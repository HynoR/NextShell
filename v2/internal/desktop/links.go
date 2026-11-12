package desktop

import (
	"fmt"
	"os"

	"nextshell.dev/v2/internal/model"
)

// OpenFolder shows a local folder ("~/" is the home directory) in the file manager.
func (p *Platform) OpenFolder(dir string) error {
	dir, e := LocalPath(dir)
	if e != nil {
		return e
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
	fn(fileURL(dir))
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
