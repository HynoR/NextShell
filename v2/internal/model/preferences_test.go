package model

import (
	"math"
	"testing"
)

func TestPreferencesValidation(t *testing.T) {
	if e := DefaultPreferences().Validate(); e != nil {
		t.Fatal(e)
	}
	cases := map[string]func(*Preferences){"appearance": func(p *Preferences) { p.Appearance = "invalid" }, "sizeSmall": func(p *Preferences) { p.FontSize = 9 }, "sizeLarge": func(p *Preferences) { p.FontSize = 25 }, "lineSmall": func(p *Preferences) { p.LineHeight = .9 }, "lineNaN": func(p *Preferences) { p.LineHeight = float32(math.NaN()) }, "lineLarge": func(p *Preferences) { p.LineHeight = 2.1 }, "cursor": func(p *Preferences) { p.Cursor = "bad" }, "theme": func(p *Preferences) { p.TerminalTheme = "bad" }, "background": func(p *Preferences) { p.Background = "#fff" }, "foreground": func(p *Preferences) { p.Foreground = "red" }, "bufferSmall": func(p *Preferences) { p.ScrollbackMiB = 0 }, "bufferLarge": func(p *Preferences) { p.ScrollbackMiB = 257 }, "keepaliveSmall": func(p *Preferences) { p.KeepaliveSeconds = 4 }, "keepaliveLarge": func(p *Preferences) { p.KeepaliveSeconds = 601 }, "commandNewline": func(p *Preferences) { p.LocalShell = "sh\ncommand" }, "downloadEmpty": func(p *Preferences) { p.DownloadDir = "" }}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			p := DefaultPreferences()
			change(&p)
			if p.Validate() == nil {
				t.Fatal("invalid preferences accepted")
			}
		})
	}
}
