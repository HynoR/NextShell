package remoteedit

import (
	"context"
	"errors"
	"nextshell.dev/v2/internal/sftpfs"
	"nextshell.dev/v2/internal/sshclient"
	"nextshell.dev/v2/internal/testutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T) (*sftpfs.Client, string) {
	t.Helper()
	testutil.RequirePOSIXRemote(t)
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
	c, cleanup, e := s.OpenSFTPContext(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(cleanup)
	return &sftpfs.Client{Client: c}, root
}
func TestRealSFTPEditConflictLimitsMode(t *testing.T) {
	c, root := fixture(t)
	p := filepath.Join(root, "text")
	os.WriteFile(p, []byte("initial 中文"), 0640)
	d, e := Open(c, p)
	if e != nil {
		t.Fatal(e)
	}
	next, e := Save(context.Background(), c, d, "saved")
	if e != nil || next.Hash == d.Hash {
		t.Fatal(next, e)
	}
	info, _ := os.Stat(p)
	if info.Mode().Perm() != 0640 {
		t.Fatal("mode lost")
	}
	if _, e = Save(context.Background(), c, d, "stale edit"); !errors.Is(e, ErrConflict) {
		t.Fatal("lost remote changes", e)
	}
	for _, text := range []string{"\x00binary", string([]byte{0xff}), strings.Repeat("x", MaxSize+1)} {
		if _, e = Save(context.Background(), c, next, text); e == nil {
			t.Fatal("invalid text saved")
		}
	}
	os.WriteFile(p, []byte{0, 1, 2}, 0600)
	if _, e = Open(c, p); e == nil {
		t.Fatal("binary opened")
	}
	f, _ := os.OpenFile(p, os.O_WRONLY, 0600)
	f.Truncate(MaxSize + 1)
	f.Close()
	if _, e = Open(c, p); e == nil {
		t.Fatal("oversize file opened")
	}
}

// Editing a link edits what it points to, and the link stays a link.
func TestEditingThroughALinkKeepsTheLink(t *testing.T) {
	c, root := fixture(t)
	real, link := filepath.Join(root, "sites-available"), filepath.Join(root, "sites-enabled")
	os.WriteFile(real, []byte("server {}"), 0640)
	if e := os.Symlink("sites-available", link); e != nil {
		t.Fatal(e)
	}
	d, e := Open(c, link)
	if e != nil || d.Text != "server {}" || d.Path != link || d.Mode.Perm() != 0640 {
		t.Fatal(d, e)
	}
	next, e := Save(context.Background(), c, d, "server { listen 80; }")
	if e != nil {
		t.Fatal(e)
	}
	if v, e := os.Lstat(link); e != nil || v.Mode()&os.ModeSymlink == 0 {
		t.Fatal("saving replaced the link with a file:", v, e)
	}
	if b, _ := os.ReadFile(real); string(b) != "server { listen 80; }" {
		t.Fatal("the target was not written:", string(b))
	}
	// Someone changes the target meanwhile: the conflict is seen through the link too.
	os.WriteFile(real, []byte("changed elsewhere"), 0640)
	if _, e = Save(context.Background(), c, next, "mine"); !errors.Is(e, ErrConflict) {
		t.Fatal("lost remote changes", e)
	}
	// A link to nothing, and a link to a folder, are not editable.
	os.Symlink(filepath.Join(root, "nowhere"), filepath.Join(root, "broken"))
	os.Symlink(root, filepath.Join(root, "dir-link"))
	for _, name := range []string{"broken", "dir-link"} {
		if _, e = Open(c, filepath.Join(root, name)); e == nil {
			t.Fatal(name, "opened")
		}
	}
}
