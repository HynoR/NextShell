package appui

import (
	"github.com/egoist/mygo/ui"
	"testing"
)

func TestDesignThemesAndSystemPreferences(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, mode := range []string{"跟随系统", "浅色", "深色"} {
			t.Run(mode+map[bool]string{true: "/system-dark", false: "/system-light"}[dark], func(t *testing.T) {
				base := ui.LightTheme()
				if dark {
					base = ui.DarkTheme()
				}
				base.FontSize = 21
				got := designTheme(base, mode, false)
				want := dark
				if mode == "浅色" {
					want = false
				}
				if mode == "深色" {
					want = true
				}
				if got.Dark != want || got.FontSize != 21 || got.Spacing != Space || got.Text == got.Background || got.Accent == got.AccentText {
					t.Fatalf("bad design theme: %+v", got)
				}
				if base.Radius != 6 {
					t.Fatal("shared system theme mutated")
				}
			})
		}
	}
	base := ui.DarkTheme()
	base.TextMuted = ui.Hex("#ffffff")
	if designTheme(base, "跟随系统", true).TextMuted != base.TextMuted {
		t.Fatal("high contrast preference overwritten")
	}
}
func TestTerminalPaletteLegiblePairs(t *testing.T) {
	for _, th := range []bool{false, true} {
		p := terminalPalette(th)
		if p.Background == p.Foreground || p.Cursor == p.Background || p.Palette[0] == p.Palette[15] {
			t.Fatal("palette lost contrast")
		}
	}
	if terminalLight.Background == terminalDark.Background {
		t.Fatal("appearance palettes identical")
	}
}
