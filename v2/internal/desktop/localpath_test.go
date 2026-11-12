package desktop

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLocalPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)        // os.UserHomeDir on Unix
	t.Setenv("USERPROFILE", home) // and on Windows
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	downloads := filepath.Join(home, "Downloads")
	for in, want := range map[string]string{
		"":                       "",
		"/abs/x":                 "/abs/x",
		"~user/x":                "~user/x",
		"~":                      home,
		"~/":                     home,
		"~/work/x":               filepath.Join(home, "work", "x"),
		"~/Downloads":            downloads,
		"~/Downloads/f.txt":      filepath.Join(downloads, "f.txt"),
		`~\Downloads\f.txt`:      filepath.Join(downloads, "f.txt"), // filepath.Join on Windows turns ~/Downloads into this
		"~/Downloadsfoo":         filepath.Join(home, "Downloadsfoo"),
		"~/Downloads/sub/f.txt":  filepath.Join(downloads, "sub", "f.txt"),
		"~/work/Downloads/f.txt": filepath.Join(home, "work", "Downloads", "f.txt"),
	} {
		got, e := LocalPath(in)
		if e != nil || got != want {
			t.Errorf("LocalPath(%q) = %q, %v; want %q", in, got, e, want)
		}
	}
}

func TestLocalPathFollowsLinuxUserDirs(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("xdg-user-dirs is a Linux desktop convention")
	}
	home, cfg := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", cfg)
	if e := os.WriteFile(filepath.Join(cfg, "user-dirs.dirs"), []byte("# comment\nXDG_DESKTOP_DIR=\"$HOME/桌面\"\nXDG_DOWNLOAD_DIR=\"$HOME/下载\"\n"), 0o600); e != nil {
		t.Fatal(e)
	}
	if got, _ := LocalPath("~/Downloads/f.txt"); got != filepath.Join(home, "下载", "f.txt") {
		t.Fatal(got)
	}
}

func TestXDGDownloads(t *testing.T) {
	home, other := t.TempDir(), t.TempDir()
	for content, want := range map[string]string{
		"XDG_DOWNLOAD_DIR=\"$HOME/下载\"\n":      filepath.Join(home, "下载"),
		"XDG_DOWNLOAD_DIR=\"" + other + "\"\n": other,
		"XDG_DOWNLOAD_DIR=\"$HOME/\"\n":        "", // switched off: the file says $HOME
		"XDG_DOWNLOAD_DIR=\"relative\"\n":      "",
		"XDG_MUSIC_DIR=\"$HOME/音乐\"\n":         "",
		"":                                     "",
	} {
		file := filepath.Join(t.TempDir(), "user-dirs.dirs")
		if e := os.WriteFile(file, []byte(content), 0o600); e != nil {
			t.Fatal(e)
		}
		if got := xdgDownloads(file, home); got != want {
			t.Errorf("xdgDownloads(%q) = %q, want %q", content, got, want)
		}
	}
	if xdgDownloads(filepath.Join(home, "missing"), home) != "" {
		t.Error("missing file")
	}
}

func TestFileURL(t *testing.T) {
	for in, want := range map[string]string{
		"/home/u/下载":           "file:///home/u/%E4%B8%8B%E8%BD%BD",
		"/Users/x/My Files":    "file:///Users/x/My%20Files",
		"C:/Users/x/Downloads": "file:///C:/Users/x/Downloads", // what a Windows path becomes after ToSlash
	} {
		if got := fileURL(in); got != want {
			t.Errorf("fileURL(%q) = %q, want %q", in, got, want)
		}
	}
	if runtime.GOOS == "windows" {
		if got := fileURL(`C:\Users\x\Downloads`); got != "file:///C:/Users/x/Downloads" {
			t.Error(got)
		}
	}
}
