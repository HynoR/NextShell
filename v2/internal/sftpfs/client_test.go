package sftpfs

import (
	"bytes"
	"context"
	"errors"
	"io"
	"nextshell.dev/v2/internal/sshclient"
	"nextshell.dev/v2/internal/testutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func client(t *testing.T) (*Client, string) {
	t.Helper()
	root := t.TempDir()
	server, e := testutil.New(root)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(server.Close)
	session, e := sshclient.Dial(context.Background(), server.Options())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { session.Close() })
	c, cleanup, e := session.OpenSFTPContext(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(cleanup)
	return &Client{c}, root
}
func TestRealSFTPAtomicTransferCRUD(t *testing.T) {
	c, root := client(t)
	ctx := context.Background()
	local := filepath.Join(t.TempDir(), "source")
	data := bytes.Repeat([]byte("UTF8 中文\n"), 20000)
	if e := os.WriteFile(local, data, 0600); e != nil {
		t.Fatal(e)
	}
	remote := filepath.Join(root, "remote")
	var progress int64
	if e := c.Upload(ctx, local, remote, false, func(n int64) { progress = n }); e != nil || progress != int64(len(data)) {
		t.Fatal(e, progress)
	}
	if e := c.UploadReader(ctx, strings.NewReader("bad"), remote, false, 0600, nil); e == nil {
		t.Fatal("overwrote existing file")
	}
	got, _ := os.ReadFile(remote)
	if !bytes.Equal(got, data) {
		t.Fatal("changed original")
	}
	entries, e := c.List(root)
	if e != nil || len(entries) != 1 || entries[0].Size != int64(len(data)) {
		t.Fatal(entries, e)
	}
	dest := filepath.Join(t.TempDir(), "download")
	if e = c.Download(ctx, remote, dest, false, nil); e != nil {
		t.Fatal(e)
	}
	got, _ = os.ReadFile(dest)
	if !bytes.Equal(got, data) {
		t.Fatal("corrupt download")
	}
	info, _ := os.Stat(dest)
	if info.Mode().Perm() != 0600 {
		t.Fatal("download mode", info.Mode())
	}
	if e = c.Download(ctx, remote, dest, false, nil); e == nil {
		t.Fatal("overwrote local target")
	}
	if e = c.UploadReader(ctx, strings.NewReader("replacement"), remote, true, 0640, nil); e != nil {
		t.Fatal(e)
	}
	got, _ = os.ReadFile(remote)
	if string(got) != "replacement" {
		t.Fatal(string(got))
	}
	if e = c.RenamePath(remote, remote+"2"); e != nil {
		t.Fatal(e)
	}
	if e = c.RemovePath(remote + "2"); e != nil {
		t.Fatal(e)
	}
	if e = c.Mkdir(filepath.Join(root, "dir")); e != nil {
		t.Fatal(e)
	}
	if e = c.RemovePath(filepath.Join(root, "dir")); e != nil {
		t.Fatal(e)
	}
	entries, _ = c.List(root)
	if len(entries) != 0 {
		t.Fatal("left staging", entries)
	}
}
func TestRealSFTPCancelCrossServerAndSymlinkSafety(t *testing.T) {
	c, root := client(t)
	ctx, cancel := context.WithCancel(context.Background())
	e := c.UploadReader(ctx, bytes.NewReader(bytes.Repeat([]byte{1}, 1<<20)), filepath.Join(root, "cancelled"), false, 0600, func(int64) { cancel() })
	if !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	entries, _ := c.List(root)
	if len(entries) != 0 {
		t.Fatal("partial not cleaned", entries)
	}
	source := filepath.Join(root, "source")
	if e = c.UploadReader(context.Background(), strings.NewReader("cross server bytes"), source, false, 0600, nil); e != nil {
		t.Fatal(e)
	}
	other, destRoot := client(t)
	dest := filepath.Join(destRoot, "dest")
	if e = Cross(context.Background(), c, other, source, dest, false, nil); e != nil {
		t.Fatal(e)
	}
	got, _ := os.ReadFile(dest)
	if string(got) != "cross server bytes" {
		t.Fatal(string(got))
	}
	link := filepath.Join(root, "link")
	if e = os.Symlink(source, link); e != nil {
		t.Fatal(e)
	}
	if e = c.Download(context.Background(), link, filepath.Join(t.TempDir(), "bad"), false, nil); e == nil {
		t.Fatal("followed remote symlink")
	}
	if e = c.Upload(context.Background(), link, filepath.Join(root, "bad"), false, nil); e == nil {
		t.Fatal("followed local symlink")
	}
	for _, p := range []string{"/", ".", "a/.."} {
		if e = c.RemovePath(p); e == nil {
			t.Fatal("allowed root removal")
		}
	}
	if e = c.RenamePath(source, link); e == nil {
		t.Fatal("rename overwrote link")
	}
}

type shortWriter struct{}

func (shortWriter) Write(b []byte) (int, error) { return len(b) - 1, nil }
func TestCopyShortWriterAndBadPath(t *testing.T) {
	if e := Copy(context.Background(), shortWriter{}, strings.NewReader("abc"), nil); !errors.Is(e, io.ErrShortWrite) {
		t.Fatal(e)
	}
	for _, v := range []string{"", "  ", "a\x00b"} {
		if CheckPath(v) == nil {
			t.Fatal("invalid path accepted")
		}
	}
}
