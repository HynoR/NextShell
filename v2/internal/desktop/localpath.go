package desktop

import (
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// LocalPath expands a leading "~" (alone, or before / or \) to the home directory. "~/Downloads" is the
// system's Downloads folder, which a desktop may name otherwise (Linux: ~/下载 in Chinese locales).
func LocalPath(p string) (string, error) {
	rest, ok := strings.CutPrefix(p, "~")
	if !ok || rest != "" && rest[0] != '/' && rest[0] != '\\' {
		return p, nil // "~user" is a name, not the home directory
	}
	home, e := os.UserHomeDir()
	if e != nil {
		return "", e
	}
	rest = strings.TrimLeft(rest, `/\`)
	if first, tail, _ := strings.Cut(strings.ReplaceAll(rest, `\`, "/"), "/"); first == "Downloads" {
		return filepath.Join(downloadsDir(home), filepath.FromSlash(tail)), nil
	}
	return filepath.Join(home, rest), nil
}

func downloadsDir(home string) string {
	if runtime.GOOS == "linux" {
		if cfg, e := os.UserConfigDir(); e == nil {
			if d := xdgDownloads(filepath.Join(cfg, "user-dirs.dirs"), home); d != "" {
				return d
			}
		}
	}
	return filepath.Join(home, "Downloads")
}

// xdgDownloads reads XDG_DOWNLOAD_DIR from an xdg-user-dirs file; "" when it names no folder of its own
// (the file says $HOME when the user turned the folder off).
func xdgDownloads(file, home string) string {
	b, e := os.ReadFile(file)
	if e != nil {
		return ""
	}
	for line := range strings.SplitSeq(string(b), "\n") {
		v, ok := strings.CutPrefix(strings.TrimSpace(line), "XDG_DOWNLOAD_DIR=")
		if !ok {
			continue
		}
		v = strings.Trim(v, `"`)
		if rest, ok := strings.CutPrefix(v, "$HOME"); ok {
			v = home + rest
		}
		if v = filepath.Clean(v); filepath.IsAbs(v) && v != filepath.Clean(home) {
			return v
		}
	}
	return ""
}

// fileURL is dir as a file: URL; a Windows drive path gets its leading slash (file:///C:/Users/me).
func fileURL(dir string) string {
	p := filepath.ToSlash(dir)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
}
