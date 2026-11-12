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
func designTheme(base *ui.Theme, mode, accent string, highContrast bool) *ui.Theme {
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
	} else {
		t.Background = ui.Hex("#f4f7fb")
		t.Surface = ui.Hex("#ffffff")
		t.SurfaceHover = ui.Hex("#edf4f8")
		t.SurfacePressed = ui.Hex("#deedf1")
		t.Border = ui.Hex("#dce5ee")
		t.Text = ui.Hex("#203249")
		t.TextMuted = ui.Hex("#5d7087")
		// Deeper than mygo's, so status text reads at 4.5:1 or better on white and on its own tint.
		t.Warning = ui.Hex("#924606")
		t.Success = ui.Hex("#166534")
		t.Danger = ui.Hex("#ac1c1c")
	}
	v := accentFor(accent).light
	if t.Dark {
		v = accentFor(accent).dark
	}
	t.Accent, t.AccentHover, t.AccentPressed, t.AccentText = ui.Hex(v[0]), ui.Hex(v[1]), ui.Hex(v[2]), ui.Hex(v[3])
	t.Selection = t.Accent.Alpha(.24)
	t.Focus = t.Accent.Alpha(.65)
	return &t
}

// accentPreset is a main color the user picks in the settings: Accent, AccentHover, AccentPressed and
// AccentText in the light theme and in the dark one. Every preset keeps the contrast described below.
type accentPreset struct {
	id, name    string
	light, dark [4]string
}

// accentPresets start with the default, the 1.x blue; cyan is the color of 2.0.0-rc1.
var accentPresets = []accentPreset{
	{"blue", "蓝", [4]string{"#1f6fd6", "#1a5fb8", "#164f9c", "#ffffff"}, [4]string{"#4d9fff", "#7ab8ff", "#3388f0", "#04162e"}},
	{"cyan", "青", [4]string{"#087f83", "#08696e", "#06575d", "#ffffff"}, [4]string{"#5ce1c7", "#8bead8", "#35c7ad", "#092522"}},
	{"green", "绿", [4]string{"#18794e", "#146640", "#105535", "#ffffff"}, [4]string{"#3ecf8e", "#6ddca9", "#2bb877", "#04241a"}},
	{"violet", "紫", [4]string{"#7046d4", "#5f3ab8", "#4f309a", "#ffffff"}, [4]string{"#a78bfa", "#c4b5fd", "#9171f0", "#1e0f45"}},
	{"orange", "橙", [4]string{"#b4530a", "#9a4708", "#803b06", "#ffffff"}, [4]string{"#fb923c", "#fdba74", "#f07a1c", "#2e1403"}},
	{"rose", "玫红", [4]string{"#c4245b", "#a81e4e", "#8c1941", "#ffffff"}, [4]string{"#fb7185", "#fda4af", "#f2546b", "#330a12"}},
}

// accentFor is the preset id names, or the default one.
func accentFor(id string) accentPreset {
	for _, p := range accentPresets {
		if p.id == id {
			return p
		}
	}
	return accentPresets[0]
}

// Selected rows of tables, lists and sidebars are filled with Accent and their
// text is AccentText: at least 4.5:1 with every preset, in both themes, also
// in a window that is not active (design_test.go checks all of these). Text
// that colors itself no longer follows the row, so it asks for its color here.

// mutedOnRow is the color of secondary text (icons, sizes, dates) in a row that
// may be selected. TextMuted on the accent fill is 1.1:1 in the light theme.
func mutedOnRow(t *ui.Theme, selected bool) ui.Color {
	if selected {
		return t.AccentText
	}
	return t.TextMuted
}

// accentInk is accent-colored text on the pale accent tint of a chosen button
// or on SurfaceHover. Accent itself can fall below 4.5:1 there in the light theme,
// so that theme uses the deeper AccentPressed (4.5:1 or better with every preset).
func accentInk(t *ui.Theme) ui.Color {
	if t.Dark {
		return t.Accent
	}
	return t.AccentPressed
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

// swatches chooses one of the colors (#rrggbb; "" is a hollow swatch) into *id; names label them.
func swatches(c *ui.Context, label string, id *string, ids, names, colors []string) {
	t := c.Theme()
	ui.Field(c, label, func() {
		ui.RadioGroup(c, func() {
			for i, v := range ids {
				sw := ui.RadioBase(c, id, v).Size(22, 22).Radius(11).Label(names[i]).Tooltip(names[i])
				if colors[i] == "" {
					sw.Border(1, t.TextMuted)
				} else {
					sw.Background(ui.Hex(colors[i]))
				}
				if *id == v {
					sw.Border(2, t.Text)
				}
			}
		}).Row().Gap(8).Label(label)
	}).FillWidth()
}
