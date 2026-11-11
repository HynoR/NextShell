package ui

// Preferences are settings of the desktop that the system's own controls
// follow. The default theme follows them, and Animate follows
// ReduceMotion; a theme of your own (Context.SetTheme) and drawing of your
// own may read them with Context.Preferences.
type Preferences struct {
	// Accent is the accent color the user chose, which the default theme
	// takes; A is 0 where the desktop has none.
	Accent Color
	// ReduceMotion asks for less motion: macOS's Reduce Motion, Windows's
	// animation effects and GNOME's animations turned off. Animate then
	// goes to its target at once; Loop, which shows that something is
	// going on, goes on.
	ReduceMotion bool
	// ReduceTransparency asks for opaque surfaces in place of translucent
	// ones: macOS's Reduce Transparency, Windows's transparency effects
	// turned off. Materials should fall back to an opaque surface, as the
	// glass plugin does; windows' own materials follow it already.
	ReduceTransparency bool
	// HighContrast asks for more contrast: macOS's Increase Contrast,
	// Windows's contrast themes, the desktop portal's higher contrast. The
	// default theme then draws borders and secondary text darker (lighter
	// in the dark), and the focus ring opaque.
	HighContrast bool
	// TextScale is how many times larger than usual text should be, as
	// Windows's and GNOME's text size settings say, 1 for usual: the
	// default theme's FontSize is that much larger.
	TextScale float32
	// Pointer is how accurately the primary pointing device points, and
	// AnyPointer the most accurate of all, as Media Queries 4's pointer
	// and any-pointer: PointerFine for a mouse, a touchpad or a pen,
	// PointerCoarse for a touch screen alone, or in Windows's tablet mode.
	// A touch-first layout, with larger targets, reads Pointer; macOS's
	// and those not known are a mouse's.
	Pointer, AnyPointer PointerAccuracy
	// NoHover tells that the primary pointing device cannot hover, as a
	// finger on a touch screen, and NoAnyHover that none can: Media
	// Queries 4's hover: none and any-hover: none. Hover and AnyHover say
	// the opposite. Tooltips and what shows on hover alone want a pointer
	// that hovers.
	NoHover, NoAnyHover bool
}

// PointerAccuracy is how accurately a pointing device points.
type PointerAccuracy uint8

const (
	// PointerFine is a mouse's, a touchpad's or a pen's: the default.
	PointerFine PointerAccuracy = iota
	// PointerCoarse is a finger's on a touch screen, which wants larger
	// targets.
	PointerCoarse
	// PointerNone is that of no pointing device, as a keyboard alone.
	PointerNone
)

// Hover reports whether the primary pointing device can hover, as Media
// Queries 4's hover: hover.
func (p Preferences) Hover() bool { return !p.NoHover }

// AnyHover reports whether any pointing device can hover, as Media
// Queries 4's any-hover: hover.
func (p Preferences) AnyHover() bool { return !p.NoAnyHover }

// Preferences returns the settings of the desktop that controls follow.
// A frame follows their changes.
func (c *context) Preferences() Preferences { return c.rt.preferences() }

// preferences returns the desktop's settings, read once until they change.
func (rt *engine) preferences() Preferences {
	if !rt.prefsKnown {
		p := rt.host.preferences()
		rt.prefs = Preferences{
			Accent:             Color{R: p.Accent.R, G: p.Accent.G, B: p.Accent.B, A: p.Accent.A},
			ReduceMotion:       p.ReduceMotion,
			ReduceTransparency: p.ReduceTransparency,
			HighContrast:       p.HighContrast,
			TextScale:          float32(p.TextScale),
			Pointer:            PointerAccuracy(p.Pointer),
			AnyPointer:         PointerAccuracy(p.AnyPointer),
			NoHover:            p.NoHover,
			NoAnyHover:         p.NoAnyHover,
		}
		if rt.prefs.TextScale <= 0 {
			rt.prefs.TextScale = 1
		}
		rt.prefsKnown = true
	}
	return rt.prefs
}

// follow makes the theme follow the desktop's settings.
func (t *Theme) follow(p Preferences) {
	if a := p.Accent; a.A > 0 {
		a.A = 255
		black, white := Color{A: 255}, Color{R: 255, G: 255, B: 255, A: 255}
		t.Accent = a
		if t.Dark {
			t.AccentHover, t.AccentPressed = a.Mix(white, 0.2), a.Mix(black, 0.15)
			t.Selection, t.Focus = a.Alpha(0.4), a.Mix(white, 0.2).Alpha(0.6)
		} else {
			t.AccentHover, t.AccentPressed = a.Mix(black, 0.12), a.Mix(black, 0.25)
			t.Selection, t.Focus = a.Alpha(0.25), a.Alpha(0.55)
		}
		// Text on the accent is black where white would not stand out, as
		// on yellow.
		t.AccentText = white
		if l := a.gray().R; l > 165 {
			t.AccentText = Color{R: 24, G: 24, B: 27, A: 255}
		}
	}
	if p.HighContrast {
		t.Border = t.Border.Mix(t.Text, 0.45)
		t.TextMuted = t.TextMuted.Mix(t.Text, 0.4)
		t.Focus.A = 255
		t.Scrollbar = t.Scrollbar.Alpha(1.6)
	}
	if s := p.TextScale; s > 0 && s != 1 {
		t.FontSize *= s
	}
}
