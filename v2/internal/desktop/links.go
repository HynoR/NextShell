package desktop

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"nextshell.dev/v2/internal/model"
)

// OpenFolder shows a local folder ("~/" is the home directory) in the file manager.
func (p *Platform) OpenFolder(dir string) error {
	if rest, ok := strings.CutPrefix(dir, "~"); ok {
		home, e := os.UserHomeDir()
		if e != nil {
			return e
		}
		dir = filepath.Join(home, rest)
	}
	if fi, e := os.Stat(dir); e != nil {
		return e
	} else if !fi.IsDir() {
		return fmt.Errorf("%s 不是目录", dir)
	}
	p.mu.Lock()
	fn := p.urlOpener
	p.mu.Unlock()
	if fn == nil {
		return fmt.Errorf("无可用桌面链接处理器")
	}
	fn((&url.URL{Scheme: "file", Path: dir}).String())
	return nil
}

func (p *Platform) SetURLopener(fn func(string)) { p.mu.Lock(); p.urlOpener = fn; p.mu.Unlock() }
func (p *Platform) OpenURL(raw string) error {
	if e := model.ValidateTerminalURL(raw); e != nil {
		return e
	}
	p.mu.Lock()
	fn := p.urlOpener
	p.mu.Unlock()
	if fn == nil {
		return fmt.Errorf("无可用桌面链接处理器")
	}
	fn(raw)
	return nil
}
