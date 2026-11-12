package sftpfs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// tree makes root/a/{f1,f2,b/{f3,c/},link-to-dir,link-to-file} and root/outside/keep, the link's target.
func tree(t *testing.T, root string) (dir, outside string) {
	t.Helper()
	dir, outside = filepath.Join(root, "a"), filepath.Join(root, "outside")
	for _, d := range []string{filepath.Join(dir, "b", "c"), outside} {
		if e := os.MkdirAll(d, 0o755); e != nil {
			t.Fatal(e)
		}
	}
	for _, f := range []string{filepath.Join(dir, "f1"), filepath.Join(dir, "f2"), filepath.Join(dir, "b", "f3"), filepath.Join(outside, "keep")} {
		if e := os.WriteFile(f, []byte("x"), 0o644); e != nil {
			t.Fatal(e)
		}
	}
	for name, target := range map[string]string{"link-to-dir": outside, "link-to-file": filepath.Join(outside, "keep")} {
		if e := os.Symlink(target, filepath.Join(dir, name)); e != nil {
			t.Fatal(e)
		}
	}
	return
}

func TestTallyAndRemoveTreeNeverFollowLinks(t *testing.T) {
	c, root := client(t)
	dir, outside := tree(t, root)
	ctx := context.Background()
	files, dirs, e := c.Tally(ctx, dir)
	// f1 f2 f3 and the two links; a, b and c.
	if e != nil || files != 5 || dirs != 3 {
		t.Fatal("tally", files, dirs, e)
	}
	var last int
	if e = c.RemoveTree(ctx, dir, func(n int) { last = n }); e != nil {
		t.Fatal(e)
	}
	if last != files+dirs {
		t.Fatal("progress ends at what was counted:", last, files+dirs)
	}
	if _, e = os.Lstat(dir); !os.IsNotExist(e) {
		t.Fatal("tree left behind", e)
	}
	if _, e = os.Stat(filepath.Join(outside, "keep")); e != nil {
		t.Fatal("followed a link out of the tree:", e)
	}
	// A link to a folder is only unlinked, whether it is the target itself or inside one.
	link := filepath.Join(root, "link")
	if e = os.Symlink(outside, link); e != nil {
		t.Fatal(e)
	}
	if files, dirs, e = c.Tally(ctx, link); e != nil || files != 1 || dirs != 0 {
		t.Fatal("a link is one file", files, dirs, e)
	}
	if e = c.RemoveTree(ctx, link, nil); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(outside, "keep")); e != nil {
		t.Fatal("followed the link itself:", e)
	}
	if _, e = os.Lstat(link); !os.IsNotExist(e) {
		t.Fatal("link kept", e)
	}
}

func TestRemoveTreeRefusesRootsAndIsRepeatable(t *testing.T) {
	c, root := client(t)
	ctx := context.Background()
	for _, p := range []string{"/", ".", "", "..", "../x", "/a/..", "bad\x00path"} {
		if c.RemoveTree(ctx, p, nil) == nil {
			t.Fatalf("removed %q", p)
		}
		if _, _, e := c.Tally(ctx, p); e == nil {
			t.Fatalf("counted %q", p)
		}
	}
	gone := filepath.Join(root, "never-was")
	if e := c.RemoveTree(ctx, gone, nil); e != nil {
		t.Fatal("a path that is gone is done:", e)
	}
	if _, _, e := c.Tally(ctx, gone); !os.IsNotExist(e) {
		t.Fatal("counting what is not there", e)
	}
	file := filepath.Join(root, "f")
	os.WriteFile(file, []byte("x"), 0o644)
	if e := c.RemoveTree(ctx, file, nil); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Lstat(file); !os.IsNotExist(e) {
		t.Fatal("file kept", e)
	}
}

func TestRemoveTreeCancelsBetweenItemsAndResumes(t *testing.T) {
	c, root := client(t)
	dir, _ := tree(t, root)
	ctx, cancel := context.WithCancel(context.Background())
	e := c.RemoveTree(ctx, dir, func(n int) {
		if n == 2 {
			cancel()
		}
	})
	if !errors.Is(e, context.Canceled) {
		t.Fatal("cancel", e)
	}
	if _, e = os.Lstat(dir); e != nil {
		t.Fatal("a cancelled delete took the top folder", e)
	}
	// What is left is deleted by running it again, with the count of what is left.
	files, dirs, e := c.Tally(context.Background(), dir)
	if e != nil || files+dirs >= 8 || files+dirs == 0 {
		t.Fatal("tally of the rest", files, dirs, e)
	}
	if e = c.RemoveTree(context.Background(), dir, nil); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Lstat(dir); !os.IsNotExist(e) {
		t.Fatal("tree left behind", e)
	}
}
