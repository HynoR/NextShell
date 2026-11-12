package desktop

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nextshell.dev/v2/internal/testutil"
)

func writeFile(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if e := os.WriteFile(path, data, 0o600); e != nil {
		t.Fatal(e)
	}
	return path
}

// A picture is copied under a name of its own, and the copy outlives the original. What is not a picture, or is
// too big, is refused with a message and leaves nothing behind.
func TestImportBackgroundCopiesAPictureAndRefusesTheRest(t *testing.T) {
	p := desktopFixture(t)
	data := t.TempDir()
	p.SetDataDir(data)
	folder := filepath.Join(data, "background")
	src := testutil.Picture(t)
	name, bmp, e := p.ImportBackground(src)
	if e != nil || bmp == nil {
		t.Fatal(name, bmp, e)
	}
	if !strings.HasPrefix(name, "background-") || filepath.Ext(name) != ".png" || name != filepath.Base(name) {
		t.Fatal("name", name)
	}
	original, _ := os.ReadFile(src)
	if copied, e := os.ReadFile(filepath.Join(folder, name)); e != nil || !bytes.Equal(copied, original) {
		t.Fatal("the copy differs from the original", e)
	}
	// The copy stands on its own: the original can go, and the copy reads back.
	if e := os.Remove(src); e != nil {
		t.Fatal(e)
	}
	if back, e := p.BackgroundPicture(name); e != nil || back == nil {
		t.Fatal(back, e)
	}
	again := testutil.Picture(t) // the same picture from elsewhere is the same file, not a second copy
	if n2, _, e := p.ImportBackground(again); e != nil || n2 != name {
		t.Fatal(n2, e)
	}

	scratch := t.TempDir()
	good, _ := os.ReadFile(again)
	cases := []struct {
		what, path, message string
	}{
		{"missing", filepath.Join(scratch, "nope.png"), "无法打开"},
		{"folder", scratch, "不是一个图片文件"},
		{"text", writeFile(t, scratch, "notes.png", []byte("not a picture at all")), "不是可用的图片"},
		{"empty", writeFile(t, scratch, "empty.png", nil), "不是可用的图片"},
		{"svg", writeFile(t, scratch, "logo.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"/>`)), "不是可用的图片"},
		{"truncated", writeFile(t, scratch, "cut.png", good[:len(good)/2]), "已损坏"},
		{"many pixels", writeFile(t, scratch, "huge.png", testutil.PNGHeader(6000, 5000)), "图片尺寸太大（6000×5000）"},
		{"many bytes", writeFile(t, scratch, "heavy.png", append(testutil.PNGHeader(10, 10), make([]byte, 21<<20)...)), "图片太大了（超过 20 MB）"},
	}
	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			n, b, e := p.ImportBackground(c.path)
			if e == nil || !strings.Contains(e.Error(), c.message) || n != "" || b != nil {
				t.Fatalf("%q %v %v, want an error with %q", n, b, e, c.message)
			}
		})
	}
	if entries, _ := os.ReadDir(folder); len(entries) != 1 {
		t.Fatal("a refused file left something behind:", entries)
	}

	// Only a plain file name is read or removed: a preference cannot point elsewhere.
	outside := writeFile(t, data, "secret.png", good)
	if _, e := p.BackgroundPicture("../secret.png"); e == nil {
		t.Fatal("read outside the folder")
	}
	p.RemoveBackground("../secret.png")
	if _, e := os.Stat(outside); e != nil {
		t.Fatal("removed outside the folder")
	}
	p.RemoveBackground(name)
	p.RemoveBackground(name) // already gone: no matter
	if _, e := os.Stat(filepath.Join(folder, name)); e == nil {
		t.Fatal("the copy stayed")
	}

	// A profile in memory has no folder to keep it in.
	p.SetDataDir("")
	if _, _, e := p.ImportBackground(again); e == nil || !strings.Contains(e.Error(), "没有可用的数据文件夹") {
		t.Fatal(e)
	}
}

// The data folder is the profile's own, unless the profile is in memory.
func TestDataFolderFollowsTheProfile(t *testing.T) {
	for path, want := range map[string]string{"": "", ":memory:": "", filepath.Join("a", "b", "x.db"): filepath.Join("a", "b")} {
		if got := dataFolder(path); got != want {
			t.Fatalf("%q: %q, want %q", path, got, want)
		}
	}
}
