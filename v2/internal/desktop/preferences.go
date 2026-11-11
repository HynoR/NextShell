package desktop

import (
	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/model"
	"runtime"
)

func (p *Platform) Preferences() model.Preferences {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.preferences
}
func terminalFont(v model.Preferences) terminal.Font {
	f := terminal.Font{Family: v.FontFamily, Size: float32(v.FontSize), LineHeight: v.LineHeight}
	if runtime.GOOS == "linux" && f.Family == "" {
		for _, family := range ui.FontFamilies() {
			if family.Name == "Noto Sans Mono CJK SC" {
				f.Family = family.Name
				break
			}
		}
	}
	return f
}
func (p *Platform) SavePreferences(v model.Preferences) error {
	p.featureMu.Lock()
	defer p.featureMu.Unlock()
	if err := p.store.SavePreferences(v); err != nil {
		return err
	}
	p.mu.Lock()
	p.preferences = v
	ts := make([]*terminal.Terminal, 0, len(p.sessions))
	for _, t := range p.sessions {
		ts = append(ts, t)
	}
	p.mu.Unlock()
	for _, t := range ts {
		t.SetFont(terminalFont(v))
	}
	p.Invalidate()
	return nil
}
