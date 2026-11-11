package appui

import (
	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// Shared layout tokens (DIPs); individual controls may use multiples of Space.
const (
	Space           float32 = 4
	PageInset               = 6 * Space
	PanelInset              = 6 * Space
	FormGap                 = 3 * Space
	RowGap                  = 2 * Space
	FieldLabelWidth         = 36 * Space
	PanelRadius             = 3 * Space
)

// All chrome uses the official Theme's colors and spacing, not per-page palettes.
func designTheme(base *ui.Theme, mode string, highContrast bool) *ui.Theme {
	t := *base
	if mode == "浅色" {
		t = *ui.LightTheme()
		t.FontSize = base.FontSize
	}
	if mode == "深色" {
		t = *ui.DarkTheme()
		t.FontSize = base.FontSize
	}
	t.Radius = 8
	t.Spacing = Space
	if highContrast {
		return &t
	}
	if t.Dark {
		t.Background = ui.Hex("#0b1220")
		t.Surface = ui.Hex("#142033")
		t.SurfaceHover = ui.Hex("#1d3048")
		t.SurfacePressed = ui.Hex("#233e53")
		t.Border = ui.Hex("#293a50")
		t.Text = ui.Hex("#e5edf6")
		t.TextMuted = ui.Hex("#a0b2c8")
		t.Accent = ui.Hex("#5ce1c7")
		t.AccentHover = ui.Hex("#8bead8")
		t.AccentPressed = ui.Hex("#35c7ad")
		t.AccentText = ui.Hex("#092522")
	} else {
		t.Background = ui.Hex("#f4f7fb")
		t.Surface = ui.Hex("#ffffff")
		t.SurfaceHover = ui.Hex("#edf4f8")
		t.SurfacePressed = ui.Hex("#deedf1")
		t.Border = ui.Hex("#dce5ee")
		t.Text = ui.Hex("#203249")
		t.TextMuted = ui.Hex("#5d7087")
		t.Accent = ui.Hex("#087f83")
		t.AccentHover = ui.Hex("#08696e")
		t.AccentPressed = ui.Hex("#06575d")
		t.AccentText = ui.Hex("#ffffff")
	}
	t.Selection = t.Accent.Alpha(.24)
	t.Focus = t.Accent.Alpha(.65)
	return &t
}

var terminalLight, terminalDark = terminalPalette(false), terminalPalette(true)

func terminalPalette(dark bool) *terminal.Theme {
	t := terminal.LightTheme()
	colors := []string{"#253449", "#b8324b", "#167044", "#87650b", "#285ac7", "#8648ac", "#087f83", "#d3dee9", "#657a94", "#cd3e57", "#208454", "#997713", "#396ee0", "#985dc1", "#118c96", "#f4f7fb"}
	t.Background = ui.Hex("#f8fafc")
	t.Foreground = ui.Hex("#203249")
	t.Cursor = ui.Hex("#087f83")
	t.Selection = ui.Hex("#d4e9ec")
	if dark {
		t.Background = ui.Hex("#0b1220")
		t.Foreground = ui.Hex("#dce7f5")
		t.Cursor = ui.Hex("#5ce1c7")
		t.Selection = ui.Hex("#29475b")
		colors = []string{"#17263b", "#f4788a", "#7bd6a5", "#efcb7c", "#82aaff", "#c7a1ef", "#6bdad4", "#d7e2f1", "#6c819b", "#ff91a1", "#99e4bd", "#ffdb96", "#9abcff", "#dfb8ff", "#91e5e0", "#f4f8ff"}
	}
	for i, s := range colors {
		t.Palette[i] = ui.Hex(s)
	}
	return t
}
