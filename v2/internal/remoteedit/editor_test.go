package remoteedit

import (
	"context"
	"errors"
	"nextshell.dev/v2/internal/sftpfs"
	"nextshell.dev/v2/internal/sshclient"
	"nextshell.dev/v2/internal/testutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func fixture(t *testing.T) (*sftpfs.Client, string) {
	t.Helper()
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
func TestExternalEditorAutoReturnArgvAndCleanup(t *testing.T) {
	c, root := fixture(t)
	p := filepath.Join(root, "text")
	os.WriteFile(p, []byte("initial"), 0600)
	d, _ := Open(c, p)
	record := filepath.Join(t.TempDir(), "staging-path")
	program := filepath.Join(t.TempDir(), "editor")
	script := "#!/bin/sh\n[ \"$1\" = '--wait' ] || exit 3\nprintf '%s' \"$2\" > " + strconv.Quote(record) + "\nprintf first > \"$2\"\nsleep 0.35\nprintf second > \"$2\"\nsleep 0.35\n"
	os.WriteFile(program, []byte(script), 0700)
	saved := 0
	e := External(context.Background(), c, d, program, func(doc Document) {
		saved++
		if doc.Text != "first" && doc.Text != "second" {
			t.Error(doc)
		}
	}, "--wait")
	if e != nil || saved < 2 {
		t.Fatal(e, saved)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "second" {
		t.Fatal(string(got))
	}
	staged, _ := os.ReadFile(record)
	if _, e = os.Stat(string(staged)); !os.IsNotExist(e) {
		t.Fatal("successful editor staging left")
	}
}
func TestExternalEditorConflictRetainsPrivateRecovery(t *testing.T) {
	c, root := fixture(t)
	p := filepath.Join(root, "text")
	os.WriteFile(p, []byte("initial"), 0600)
	d, _ := Open(c, p)
	record := filepath.Join(t.TempDir(), "staging-path")
	program := filepath.Join(t.TempDir(), "editor")
	script := "#!/bin/sh\nprintf '%s' \"$1\" > " + strconv.Quote(record) + "\nprintf local-edit > \"$1\"\nprintf remote-edit > " + strconv.Quote(p) + "\n"
	os.WriteFile(program, []byte(script), 0700)
	e := External(context.Background(), c, d, program, nil)
	if !errors.Is(e, ErrConflict) || !strings.Contains(e.Error(), "recovery file retained") {
		t.Fatal(e)
	}
	staged, _ := os.ReadFile(record)
	recovery := string(staged)
	if !strings.HasPrefix(filepath.Base(filepath.Dir(recovery)), "nextshell-edit-") {
		t.Fatal("unexpected staging path")
	}
	t.Cleanup(func() { os.RemoveAll(filepath.Dir(recovery)) })
	got, _ := os.ReadFile(recovery)
	if string(got) != "local-edit" {
		t.Fatal("lost recovery", string(got))
	}
	info, _ := os.Stat(recovery)
	dirInfo, _ := os.Stat(filepath.Dir(recovery))
	if info.Mode().Perm() != 0600 || dirInfo.Mode().Perm() != 0700 {
		t.Fatal("unsafe recovery permissions")
	}
	got, _ = os.ReadFile(p)
	if string(got) != "remote-edit" {
		t.Fatal("remote conflict overwritten")
	}
}
