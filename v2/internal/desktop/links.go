package desktop

import (
	"fmt"
	"nextshell.dev/v2/internal/model"
)

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
