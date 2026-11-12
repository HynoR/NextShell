package sftpfs

import (
	"os"
	"path/filepath"
	"testing"
)

// links makes root/{real-dir/inner, real-file, link-dir, link-file, link-rel, broken}: links to a folder, a
// file, the same file by a relative path, and to nothing.
func links(t *testing.T, root string) {
	t.Helper()
	if e := os.MkdirAll(filepath.Join(root, "real-dir"), 0o755); e != nil {
		t.Fatal(e)
	}
	for name, text := range map[string]string{"real-dir/inner": "i", "real-file": "twelve bytes"} {
		if e := os.WriteFile(filepath.Join(root, name), []byte(text), 0o644); e != nil {
			t.Fatal(e)
		}
	}
	for name, target := range map[string]string{"link-dir": filepath.Join(root, "real-dir"), "link-file": filepath.Join(root, "real-file"), "link-rel": "real-file", "broken": filepath.Join(root, "nowhere")} {
		if e := os.Symlink(target, filepath.Join(root, name)); e != nil {
			t.Fatal(e)
		}
	}
}

func TestListTakesWhatLinksPointTo(t *testing.T) {
	c, root := client(t)
	links(t, root)
	entries, e := c.List(root)
	if e != nil {
		t.Fatal(e)
	}
	got := map[string]Entry{}
	for _, v := range entries {
		got[v.Name] = v
	}
	if v := got["link-dir"]; !v.Symlink || !v.Directory || v.Broken || v.Target != filepath.Join(root, "real-dir") {
		t.Fatal("a link to a folder is a folder:", v)
	}
	if v := got["link-file"]; !v.Symlink || v.Directory || v.Broken || v.Size != 12 {
		t.Fatal("a link to a file has the file's size:", v)
	}
	if v := got["link-rel"]; v.Target != "real-file" || v.Size != 12 {
		t.Fatal("the target is shown as written:", v)
	}
	if v := got["broken"]; !v.Symlink || !v.Broken || v.Directory || v.Target != filepath.Join(root, "nowhere") {
		t.Fatal("a link to nothing is broken, not an error:", v)
	}
	if v := got["real-dir"]; v.Symlink || !v.Directory || v.Broken || v.Target != "" {
		t.Fatal("a plain folder:", v)
	}
	if len(entries) != 6 {
		t.Fatal(len(entries), entries)
	}
}

func TestResolveFollowsLinksAndFailsOnBrokenOnes(t *testing.T) {
	c, root := client(t)
	links(t, root)
	file := filepath.Join(root, "real-file")
	for _, name := range []string{"link-file", "link-rel", "real-file"} {
		real, info, e := c.Resolve(filepath.Join(root, name))
		if e != nil || real != file || !info.Mode().IsRegular() {
			t.Fatal(name, real, info, e)
		}
	}
	if real, info, e := c.Resolve(filepath.Join(root, "link-dir")); e != nil || real != filepath.Join(root, "real-dir") || !info.IsDir() {
		t.Fatal("link-dir", real, info, e)
	}
	if _, _, e := c.Resolve(filepath.Join(root, "broken")); e == nil {
		t.Fatal("a broken link resolved")
	}
	loop := filepath.Join(root, "loop")
	if e := os.Symlink(loop, loop); e != nil {
		t.Fatal(e)
	}
	if _, _, e := c.Resolve(loop); e == nil {
		t.Fatal("a link to itself resolved")
	}
}

// Showing a link as the folder it points to must not make deleting it reach into that folder.
func TestRemovingALinkToAFolderLeavesTheFolder(t *testing.T) {
	c, root := client(t)
	links(t, root)
	link, inner := filepath.Join(root, "link-dir"), filepath.Join(root, "real-dir", "inner")
	if e := c.RemovePath(link); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Lstat(link); !os.IsNotExist(e) {
		t.Fatal("the link is still there", e)
	}
	if _, e := os.Stat(inner); e != nil {
		t.Fatal("deleted the folder behind the link:", e)
	}
	// A link in a tree being deleted is unlinked, whatever the list shows for it.
	if e := os.Symlink(filepath.Join(root, "real-dir"), filepath.Join(root, "real-dir", "back")); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(filepath.Join(root, "real-dir"), filepath.Join(root, "second")); e != nil {
		t.Fatal(e)
	}
	if e := c.RemoveTree(t.Context(), filepath.Join(root, "second"), nil); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(inner); e != nil {
		t.Fatal("RemoveTree followed a link to a folder:", e)
	}
}
