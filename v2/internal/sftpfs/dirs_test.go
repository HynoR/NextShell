package sftpfs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestUploadAndDownloadTreeWithoutTar(t *testing.T) {
	c, root := client(t)
	local := t.TempDir()
	src := filepath.Join(local, "src")
	os.MkdirAll(filepath.Join(src, "sub"), 0o755)
	os.WriteFile(filepath.Join(src, "a.txt"), []byte("aaaa"), 0o644)
	os.WriteFile(filepath.Join(src, "sub", "b.txt"), []byte("bb"), 0o644)
	os.Symlink("/etc/passwd", filepath.Join(src, "link"))
	var last int64
	remote := filepath.Join(root, "up")
	if e := c.UploadTree(context.Background(), src, remote, func(n int64) { last = n }); e != nil {
		t.Fatal(e)
	}
	if b, _ := os.ReadFile(filepath.Join(remote, "sub", "b.txt")); string(b) != "bb" || last != 6 {
		t.Fatal("upload", string(b), last)
	}
	if _, e := os.Lstat(filepath.Join(remote, "link")); e == nil {
		t.Fatal("symlink uploaded")
	}
	if c.UploadTree(context.Background(), src, remote, func(int64) {}) == nil {
		t.Fatal("merged into an existing directory")
	}
	back := filepath.Join(local, "back")
	if e := c.DownloadTree(context.Background(), remote, back, func(n int64) { last = n }); e != nil {
		t.Fatal(e)
	}
	if b, _ := os.ReadFile(filepath.Join(back, "a.txt")); string(b) != "aaaa" || last != 6 {
		t.Fatal("download", string(b), last)
	}
	if c.DownloadTree(context.Background(), remote, back, func(int64) {}) == nil {
		t.Fatal("overwrote a local directory")
	}
}
