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
