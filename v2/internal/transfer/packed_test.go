package transfer

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"nextshell.dev/v2/internal/sshclient"
	"nextshell.dev/v2/internal/testutil"
	"os"
	"path/filepath"
	"testing"
)

func archive(t *testing.T, name string, kind byte, size int64) []byte {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	if e := tw.WriteHeader(&tar.Header{Name: name, Typeflag: kind, Mode: 0600, Size: size, Linkname: "../outside"}); e != nil {
		t.Fatal(e)
	}
	if kind == tar.TypeReg {
		tw.Write(make([]byte, size))
	}
	tw.Close()
	gz.Close()
	return b.Bytes()
}
func TestPackedArchiveSafety(t *testing.T) {
	for _, name := range []string{"../escape", "/absolute", `a\escape`} {
		t.Run(name, func(t *testing.T) {
			if e := Extract(context.Background(), bytes.NewReader(archive(t, name, tar.TypeReg, 1)), t.TempDir(), 100); e == nil {
				t.Fatal("path escape accepted")
			}
		})
	}
	for _, kind := range []byte{tar.TypeSymlink, tar.TypeLink, tar.TypeFifo} {
		t.Run(string(kind), func(t *testing.T) {
			if e := Extract(context.Background(), bytes.NewReader(archive(t, "link", kind, 0)), t.TempDir(), 100); e == nil {
				t.Fatal("special entry accepted")
			}
		})
	}
	if e := Extract(context.Background(), bytes.NewReader(archive(t, "big", tar.TypeReg, 101)), t.TempDir(), 100); e == nil {
		t.Fatal("expanded limit ignored")
	}
	valid := archive(t, "ok", tar.TypeReg, 1)
	for _, bad := range [][]byte{valid[:len(valid)-5], append(append([]byte{}, valid[:len(valid)-1]...), valid[len(valid)-1]^1)} {
		if e := Extract(context.Background(), bytes.NewReader(bad), t.TempDir(), 100); e == nil {
			t.Fatal("gzip trailer corruption accepted")
		}
	}
	root := t.TempDir()
	os.Symlink("/DO-NOT-READ", filepath.Join(root, "link"))
	if e := Pack(context.Background(), root, &bytes.Buffer{}); e == nil {
		t.Fatal("pack followed symlink")
	}
}
func TestRealSSHPackedTransferAndQuote(t *testing.T) {
	testutil.RequireLinuxRemote(t)
	root := t.TempDir()
	server, e := testutil.New(root)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(server.Close)
	s, e := sshclient.Dial(context.Background(), server.Options())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	local := t.TempDir()
	os.Mkdir(filepath.Join(local, "nested"), 0700)
	os.WriteFile(filepath.Join(local, "nested", "中文 ' file"), []byte("packed bytes"), 0600)
	remote := filepath.Join(root, "remote ' ; safe")
	var up int64
	if e = UploadPacked(context.Background(), s, local, remote, func(n int64) { up = n }); e != nil || up == 0 {
		t.Fatal(e, up)
	}
	if e = UploadPacked(context.Background(), s, local, remote, nil); e == nil {
		t.Fatal("merged existing tree")
	}
	download := filepath.Join(t.TempDir(), "copy")
	var down int64
	if e = DownloadPacked(context.Background(), s, remote, download, func(n int64) { down = n }); e != nil || down == 0 {
		t.Fatal(e, down)
	}
	got, e := os.ReadFile(filepath.Join(download, "nested", "中文 ' file"))
	if e != nil || string(got) != "packed bytes" {
		t.Fatal(string(got), e)
	}
	if e = DownloadPacked(context.Background(), s, remote, download, nil); e == nil {
		t.Fatal("replaced existing destination")
	}
	marker := filepath.Join(root, "INJECTION")
	if e = DownloadPacked(context.Background(), s, "missing'; touch "+marker+"; '", filepath.Join(t.TempDir(), "bad"), nil); e == nil {
		t.Fatal("bad path accepted")
	}
	if _, e = os.Stat(marker); !os.IsNotExist(e) {
		t.Fatal("shell injection")
	}
	entries, _ := os.ReadDir(root)
	for _, v := range entries {
		if v.Name() != filepath.Base(remote) {
			t.Fatal("remote staging left", v.Name())
		}
	}
	if server.Execs.Load() < 4 {
		t.Fatal("did not use real SSH exec")
	}
}
