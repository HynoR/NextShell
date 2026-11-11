package sftpfs

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecursiveCopyAcrossRealSFTPServers(t *testing.T) {
	src, root := client(t)
	dst, target := client(t)
	from := filepath.Join(root, "树")
	if err := os.MkdirAll(filepath.Join(from, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"one", "nested/中文.txt"} {
		if err := os.WriteFile(filepath.Join(from, n), []byte("hello 中文"), 0640); err != nil {
			t.Fatal(err)
		}
	}
	var progress int64
	to := filepath.Join(target, "copy")
	if err := CopyTree(context.Background(), src, dst, from, to, false, func(n int64) {
		if n < progress {
			t.Error("progress regressed")
		}
		progress = n
	}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(to, "nested/中文.txt"))
	if err != nil || string(got) != "hello 中文" || progress != 2*int64(len(got)) {
		t.Fatal(err, string(got), progress)
	}
	if _, err = os.Stat(from); err != nil {
		t.Fatal("copy removed source", err)
	}
	if err = CopyTree(context.Background(), src, dst, from, to, false, nil); !errors.Is(err, fs.ErrExist) {
		t.Fatal("collision", err)
	}
	entries, _ := os.ReadDir(target)
	if len(entries) != 1 {
		t.Fatal("staging leaked", entries)
	}
}
func TestRecursiveCopyRefusesLinksSelfAndCancellation(t *testing.T) {
	c, root := client(t)
	from := filepath.Join(root, "source")
	os.Mkdir(from, 0700)
	os.WriteFile(filepath.Join(from, "good"), []byte("safe"), 0600)
	if err := CopyTree(context.Background(), c, c, from, filepath.Join(from, "nested"), true, nil); err == nil {
		t.Fatal("self copy accepted")
	}
	if err := os.Symlink("good", filepath.Join(from, "link")); err != nil {
		t.Fatal(err)
	}
	if err := CopyTree(context.Background(), c, c, from, filepath.Join(root, "fail"), true, nil); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "fail")); !os.IsNotExist(err) {
		t.Fatal("partial published", err)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 1 {
		t.Fatal("staging leaked", entries)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := CopyTree(ctx, c, c, from, filepath.Join(root, "cancel"), true, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestSameServerFileCopyAndMove(t *testing.T) {
	c, root := client(t)
	from := filepath.Join(root, "old")
	os.WriteFile(from, []byte("bytes"), 0600)
	to := filepath.Join(root, "new")
	if err := CopyTree(context.Background(), c, c, from, to, true, nil); err != nil {
		t.Fatal(err)
	}
	if err := c.RenamePath(to, to+"-moved"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(to); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(to + "-moved"); err != nil || string(b) != "bytes" {
		t.Fatal(err, string(b))
	}
}
