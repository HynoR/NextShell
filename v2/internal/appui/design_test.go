package appui

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

// luminance and contrast follow WCAG 2.x.
func luminance(c ui.Color) float64 {
	channel := func(v uint8) float64 {
		x := float64(v) / 255
		if x <= 0.03928 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(c.R) + 0.7152*channel(c.G) + 0.0722*channel(c.B)
}
func contrast(a, b ui.Color) float64 {
	x, y := luminance(a), luminance(b)
	if x < y {
		x, y = y, x
	}
	return (x + 0.05) / (y + 0.05)
}

// Text that sits on the accent fill or tint, in the places that use them: selected rows
// of tables and the sidebar, the palette, the host dialog's sections and the editor's tabs,
// with every accent preset.
func TestSelectedTextContrastInBothThemes(t *testing.T) {
	for _, preset := range accentPresets {
		for _, dark := range []bool{false, true} {
			accent := preset.id
			base := ui.LightTheme()
			if dark {
				base = ui.DarkTheme()
			}
			th := designTheme(base, "跟随系统", accent, false)
			// What mygo fades the fill to in a window that is not active (Theme.inactiveAccent).
			far := ui.RGB(0, 0, 0)
			if luminance(th.AccentText) < 0.5 {
				far = ui.RGB(255, 255, 255)
			}
			inactive := th.AccentText.Mix(far, 0.55)
			check := func(what string, fg, bg ui.Color) {
				t.Helper()
				if r := contrast(fg, bg); r < 4.5 {
					t.Errorf("%s dark=%v %s: %.2f:1, want at least 4.5:1", accent, dark, what, r)
				}
			}
			for name, fill := range map[string]ui.Color{"accent": th.Accent, "hovered": th.AccentHover, "pressed": th.AccentPressed, "inactive window": inactive} {
				check("selected row text on "+name, th.AccentText, fill)
				check("selected row secondary text on "+name, mutedOnRow(th, true), fill)
			}
			for name, bg := range map[string]ui.Color{"surface": th.Surface, "background": th.Background} {
				check("secondary text on "+name, mutedOnRow(th, false), bg)
				for _, alpha := range []float32{.12, .15} {
					tint := th.Accent.Alpha(alpha).Over(bg)
					check("text on the accent tint over "+name, th.Text, tint)
					if alpha == .15 {
						check("accent ink on the accent tint over "+name, accentInk(th), tint)
					}
				}
			}
			check("accent ink on SurfaceHover", accentInk(th), th.SurfaceHover)
		}
	}
}

func TestDesignThemesAndSystemPreferences(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, mode := range []string{"跟随系统", "浅色", "深色"} {
			t.Run(mode+map[bool]string{true: "/system-dark", false: "/system-light"}[dark], func(t *testing.T) {
				base := ui.LightTheme()
				if dark {
					base = ui.DarkTheme()
				}
				base.FontSize = 21
				got := designTheme(base, mode, "", false)
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
	if got := designTheme(ui.DarkTheme(), "深色", "", false).Accent; got != ui.Hex("#4d9fff") {
		t.Fatalf("default accent %v, want the 1.x blue", got)
	}
	if designTheme(ui.DarkTheme(), "深色", "gone", false).Accent != designTheme(ui.DarkTheme(), "深色", "", false).Accent {
		t.Fatal("an unknown preset is not the default")
	}
	base := ui.DarkTheme()
	base.TextMuted = ui.Hex("#ffffff")
	if designTheme(base, "跟随系统", "", true).TextMuted != base.TextMuted {
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
