//go:build interop

package interop

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	mrand "math/rand/v2"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"nextshell.dev/v2/internal/sftpfs"
	"nextshell.dev/v2/internal/sshclient"
)

// sftpOn dials c and opens the SFTP subsystem exactly like the product (OpenSFTPContext).
func sftpOn(t testing.TB, c *Container) (*sftpfs.Client, *sshclient.Session) {
	t.Helper()
	s := Dial(t, c.Options())
	sf, cleanup, err := s.OpenSFTPContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	return &sftpfs.Client{Client: sf}, s
}

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func remoteSum(t testing.TB, s *sshclient.Session, p string) string {
	t.Helper()
	return strings.Fields(Sh(t, s, "sha256sum "+Quote(p)))[0]
}

// Quote is shell single-quoting for test commands.
func Quote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'" }

func writeRand(t testing.TB, dir, name string, size int) (string, string) {
	t.Helper()
	b := make([]byte, size)
	rand.Read(b)
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, b, 0640); err != nil {
		t.Fatal(err)
	}
	return p, sum(b)
}

func eachVariant(t *testing.T, vs []Variant, f func(t *testing.T, c *Container)) {
	for _, v := range vs {
		t.Run(v.Name, func(t *testing.T) { f(t, Shared(t, v)) })
	}
}

var unixVariants = []Variant{Debian, Ubuntu, Alpine}

func TestSFTPListBigDirectory(t *testing.T) {
	eachVariant(t, unixVariants, func(t *testing.T, c *Container) {
		sc, s := sftpOn(t, c)
		Sh(t, s, `rm -rf ~/big && mkdir ~/big && cd ~/big && seq 1 6000 | sed 's/^/file-/' | xargs touch && touch '中文 文件.txt' "it's here.txt" 'a b  c' && mkdir sub && ln -s sub link`)
		start := time.Now()
		entries, err := sc.List("/home/tester/big")
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("listed %d entries in %v", len(entries), time.Since(start).Round(time.Millisecond))
		if len(entries) != 6005 {
			t.Fatalf("got %d entries, want 6005", len(entries))
		}
		byName := map[string]sftpfs.Entry{}
		for _, e := range entries {
			byName[e.Name] = e
		}
		for _, n := range []string{"中文 文件.txt", "it's here.txt", "a b  c"} {
			if _, ok := byName[n]; !ok {
				t.Errorf("missing %q", n)
			}
		}
		if !byName["sub"].Directory || !byName["link"].Symlink {
			t.Errorf("sub=%+v link=%+v", byName["sub"], byName["link"])
		}
	})
}

func TestSFTPUploadDownloadRoundTrip(t *testing.T) {
	eachVariant(t, unixVariants, func(t *testing.T, c *Container) {
		sc, s := sftpOn(t, c)
		ctx := context.Background()
		dir := t.TempDir()
		for _, size := range []int{0, 1, 32768, 1 << 20, 24<<20 + 7} {
			name := fmt.Sprintf("f%d", size)
			local, want := writeRand(t, dir, name, size)
			remote := "/home/tester/" + name
			var last int64
			if err := sc.Upload(ctx, local, remote, false, func(n int64) { last = n }); err != nil {
				t.Fatalf("%d: upload: %v", size, err)
			}
			if last != int64(size) {
				t.Errorf("%d: progress ended at %d", size, last)
			}
			if got := remoteSum(t, s, remote); got != want {
				t.Fatalf("%d: remote hash %s != %s", size, got, want)
			}
			if mode := strings.TrimSpace(Sh(t, s, "stat -c %a "+remote)); mode != "640" {
				t.Errorf("%d: remote mode %s, want 640", size, mode)
			}
			back := filepath.Join(dir, name+".back")
			if err := sc.Download(ctx, remote, back, false, nil); err != nil {
				t.Fatalf("%d: download: %v", size, err)
			}
			b, _ := os.ReadFile(back)
			if sum(b) != want {
				t.Fatalf("%d: downloaded hash differs", size)
			}
			if leftovers := Sh(t, s, "ls -A /home/tester | grep nextshell || true"); strings.TrimSpace(leftovers) != "" {
				t.Fatalf("%d: staging files left behind: %s", size, leftovers)
			}
		}
		// unicode + hostile names
		for _, name := range []string{"中文 文件.txt", "it's \"quoted\" $HOME `x`.txt", "-dash.txt", "tab\tname"} {
			local, want := writeRand(t, dir, "src", 100)
			if err := sc.Upload(ctx, local, "/home/tester/"+name, true, nil); err != nil {
				t.Fatalf("%q: %v", name, err)
			}
			if got := remoteSum(t, s, "/home/tester/"+name); got != want {
				t.Fatalf("%q: hash mismatch", name)
			}
		}
	})
}

func TestSFTPOverwriteAndRenameSemantics(t *testing.T) {
	eachVariant(t, unixVariants, func(t *testing.T, c *Container) {
		sc, s := sftpOn(t, c)
		ctx := context.Background()
		home := "/home/tester/sem"
		Sh(t, s, "rm -rf "+home+" && mkdir "+home+" && echo ORIGINAL > "+home+"/f && mkdir "+home+"/d && ln -s f "+home+"/lnk && ln -s /etc/passwd "+home+"/etc-link")
		up := func(dst, content string, overwrite bool) error {
			return sc.UploadReader(ctx, strings.NewReader(content), dst, overwrite, 0600, nil)
		}
		cat := func(p string) string { return Sh(t, s, "cat "+p) }

		if err := up(home+"/f", "NEW", false); err == nil {
			t.Error("overwrite=false replaced an existing file")
		}
		if cat(home+"/f") != "ORIGINAL\n" {
			t.Error("original damaged by a refused upload")
		}
		if err := up(home+"/f", "NEW", true); err != nil {
			t.Fatal(err)
		}
		if cat(home+"/f") != "NEW" {
			t.Error("overwrite=true did not replace")
		}
		if err := up(home+"/d", "x", true); err == nil {
			t.Error("uploaded over a directory")
		}
		if err := up(home+"/etc-link", "x", true); err == nil {
			t.Error("wrote through a symlink to /etc/passwd")
		}
		if err := up(home+"/lnk", "x", true); err == nil {
			t.Error("replaced a symlink")
		}
		if got := strings.TrimSpace(Sh(t, s, "ls -A "+home+" | grep -c nextshell || true")); got != "0" {
			t.Errorf("%s leftover .part files", got)
		}
		if err := up("/nonexistent-dir/x", "x", false); err == nil {
			t.Error("upload into a missing directory succeeded")
		}
		if err := up("/proc/forbidden", "x", false); err == nil {
			t.Error("upload to a read-only location succeeded")
		}

		// rename: never clobbers
		if err := sc.RenamePath(home+"/f", home+"/lnk"); err == nil {
			t.Error("rename onto an existing name succeeded")
		}
		if err := sc.RenamePath(home+"/f", home+"/重命名 f"); err != nil {
			t.Fatal(err)
		}
		if err := sc.RenamePath(home+"/d", home+"/d2"); err != nil {
			t.Fatal(err)
		}
		if err := sc.RenamePath(home+"/missing", home+"/x"); err == nil {
			t.Error("rename of a missing file succeeded")
		}
		if err := sc.RemovePath(home + "/d2"); err != nil {
			t.Fatal(err)
		}
		if err := sc.RemovePath("/"); err == nil {
			t.Error("root removal allowed")
		}

		// downloads
		local := filepath.Join(t.TempDir(), "out")
		os.WriteFile(local, []byte("LOCAL"), 0600)
		if err := sc.Download(ctx, home+"/重命名 f", local, false, nil); err == nil {
			t.Error("download clobbered a local file")
		}
		if b, _ := os.ReadFile(local); string(b) != "LOCAL" {
			t.Error("local file damaged by refused download")
		}
		if err := sc.Download(ctx, home+"/重命名 f", local, true, nil); err != nil {
			t.Fatal(err)
		}
		if b, _ := os.ReadFile(local); string(b) != "NEW" {
			t.Errorf("overwriting download wrote %q", b)
		}
		if err := sc.Download(ctx, home+"/lnk", local+"2", false, nil); err == nil {
			t.Error("downloaded through a symlink")
		}
		if err := sc.Download(ctx, home+"/nope", local+"3", false, nil); err == nil {
			t.Error("downloaded a missing file")
		}
		if m, _ := filepath.Glob(filepath.Dir(local) + "/.nextshell-*"); len(m) != 0 {
			t.Errorf("local temp files left: %v", m)
		}
	})
}

// treeSums lists "path hash" for every regular file plus dirs, in a format the remote can also print.
func localTree(t testing.TB, root string) []string {
	t.Helper()
	var out []string
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == root {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		if d.IsDir() {
			out = append(out, "d ./"+filepath.ToSlash(rel))
			return nil
		}
		b, _ := os.ReadFile(p)
		out = append(out, "f ./"+filepath.ToSlash(rel)+" "+sum(b))
		return nil
	})
	slices.Sort(out)
	return out
}
func remoteTree(t testing.TB, s *sshclient.Session, root string) []string {
	t.Helper()
	out := Sh(t, s, `cd `+Quote(root)+` && { find . -mindepth 1 -type d | sed 's/^/d /'; find . -type f | while IFS= read -r f; do echo "f $f $(sha256sum "$f" | cut -d' ' -f1)"; done; }`)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	slices.Sort(lines)
	return lines
}

// makeTree builds a nested tree with awkward names and a few larger files.
func makeTree(t testing.TB, root string, files int) {
	t.Helper()
	r := mrand.New(mrand.NewPCG(1, 2))
	names := []string{"plain", "中文目录", "with space", "it's", "a$b", "-lead", "dot.dir"}
	for i := 0; i < files; i++ {
		dir := root
		for d := 0; d < r.IntN(4); d++ {
			dir = filepath.Join(dir, names[r.IntN(len(names))])
		}
		os.MkdirAll(dir, 0755)
		size := r.IntN(2000)
		if i%50 == 0 {
			size = 300000
		}
		b := make([]byte, size)
		for j := range b {
			b[j] = byte(r.IntN(256))
		}
		os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%03d-%s", i, names[i%len(names)])), b, 0644)
	}
	os.MkdirAll(filepath.Join(root, "empty dir", "nested empty"), 0755)
}

func TestSFTPRecursiveCopy(t *testing.T) {
	src, dst := tree3(t)
	local := t.TempDir() + "/tree"
	makeTree(t, local, 120)
	want := localTree(t, local)
	ctx := context.Background()

	// seed Debian via packed upload-free path: plain SFTP uploads
	seed := func(sc *sftpfs.Client, s *sshclient.Session, root string) {
		Sh(t, s, "rm -rf "+Quote(root)+" && mkdir -p "+Quote(root))
		filepath.WalkDir(local, func(p string, d fs.DirEntry, err error) error {
			rel, _ := filepath.Rel(local, p)
			if rel == "." {
				return nil
			}
			if d.IsDir() {
				return sc.MkdirAll(path.Join(root, filepath.ToSlash(rel)))
			}
			return sc.Upload(ctx, p, path.Join(root, filepath.ToSlash(rel)), false, nil)
		})
	}
	seed(src.sc, src.s, "/home/tester/srctree")
	if got := remoteTree(t, src.s, "/home/tester/srctree"); !slices.Equal(got, want) {
		t.Fatalf("seeding failed:\n%v\n%v", got[:min(3, len(got))], want[:min(3, len(want))])
	}

	t.Run("same-server", func(t *testing.T) {
		Sh(t, src.s, "rm -rf ~/copy1")
		var prog int64
		if err := sftpfs.CopyTree(ctx, src.sc, src.sc, "/home/tester/srctree", "/home/tester/copy1", true, func(n int64) { prog = n }); err != nil {
			t.Fatal(err)
		}
		if got := remoteTree(t, src.s, "/home/tester/copy1"); !slices.Equal(got, want) {
			t.Fatal("tree differs after same-server copy")
		}
		if prog == 0 {
			t.Error("no progress reported")
		}
		if err := sftpfs.CopyTree(ctx, src.sc, src.sc, "/home/tester/srctree", "/home/tester/copy1", true, nil); err == nil {
			t.Error("copy onto an existing destination succeeded")
		}
		if err := sftpfs.CopyTree(ctx, src.sc, src.sc, "/home/tester/srctree", "/home/tester/srctree/inside", true, nil); err == nil {
			t.Error("copy into itself succeeded")
		}
		if left := strings.TrimSpace(Sh(t, src.s, "ls -A ~ | grep nextshell || true")); left != "" {
			t.Errorf("staging left behind: %s", left)
		}
	})
	t.Run("cross-server-debian-to-alpine-and-back", func(t *testing.T) {
		Sh(t, dst.s, "rm -rf ~/copy2")
		if err := sftpfs.CopyTree(ctx, src.sc, dst.sc, "/home/tester/srctree", "/home/tester/copy2", false, nil); err != nil {
			t.Fatal(err)
		}
		if got := remoteTree(t, dst.s, "/home/tester/copy2"); !slices.Equal(got, want) {
			t.Fatal("tree differs on Alpine")
		}
		Sh(t, src.s, "rm -rf ~/copy3")
		if err := sftpfs.CopyTree(ctx, dst.sc, src.sc, "/home/tester/copy2", "/home/tester/copy3", false, nil); err != nil {
			t.Fatal(err)
		}
		if got := remoteTree(t, src.s, "/home/tester/copy3"); !slices.Equal(got, want) {
			t.Fatal("tree differs after the round trip")
		}
	})
	t.Run("symlink-in-tree-fails-clean", func(t *testing.T) {
		Sh(t, src.s, "rm -rf ~/withlink ~/copy4 && cp -r ~/srctree ~/withlink && ln -s /etc/passwd ~/withlink/evil")
		err := sftpfs.CopyTree(ctx, src.sc, dst.sc, "/home/tester/withlink", "/home/tester/copy4", false, nil)
		if err == nil {
			t.Fatal("copied a tree containing a symlink")
		}
		t.Logf("symlink in tree: %v", err)
		if out := Sh(t, dst.s, "ls -A ~ | grep -E 'copy4|nextshell' || true"); strings.TrimSpace(out) != "" {
			t.Errorf("partial copy / staging left on the target: %s", out)
		}
	})
	t.Run("cancel-leaves-nothing", func(t *testing.T) {
		Sh(t, dst.s, "rm -rf ~/copy5")
		cctx, cancel := context.WithCancel(ctx)
		err := sftpfs.CopyTree(cctx, src.sc, dst.sc, "/home/tester/srctree", "/home/tester/copy5", false, func(n int64) {
			if n > 200000 {
				cancel()
			}
		})
		if err == nil {
			t.Fatal("cancelled copy reported success")
		}
		if out := Sh(t, dst.s, "ls -A ~ | grep -E 'copy5|nextshell' || true"); strings.TrimSpace(out) != "" {
			t.Errorf("cancelled copy left: %s", out)
		}
	})
}

type side struct {
	sc *sftpfs.Client
	s  *sshclient.Session
}

func tree3(t *testing.T) (side, side) {
	a, as := sftpOn(t, Shared(t, Debian))
	b, bs := sftpOn(t, Shared(t, Alpine))
	return side{a, as}, side{b, bs}
}
