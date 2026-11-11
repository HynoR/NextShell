// Package glass draws Liquid Glass in native UI, as macOS 26 and later
// draw it (AppKit's NSGlassEffectView), on every platform: what the
// elements under it painted shows through, blurred, bent near its edges as
// through the rim of a lens, and lit along its hairline rim.
// MyGo's renderers draw it with shaders of this package, on the GPU or the
// CPU, so it looks the same everywhere.
//
//	ui.Row(c).Padding(8, 16).Radius(22).Material(glass.Glass{})
//
// ScrollEdge is macOS's scroll edge effect, which content scrolling under
// a bar fades or frosts under, and Blur blurs what is under an element
// without the glass, evenly or fading along a gradient.
package glass

//go:generate go run ./internal/gen

import (
	"fmt"
	"math"
	"time"

	"github.com/egoist/mygo/ui"
)

// Glass is Liquid Glass, a ui.Material: Element.Material fills an element
// with it, in place of a background, shaped by its Radius. What was
// painted under the element shows through it, as content that scrolls
// under a bar placed over it with Absolute, and other glass.
type Glass struct {
	// Style is the glass's material.
	Style Style
	// Tint colors the glass, by its alpha, as for a prominent button: an
	// opaque tint lets a little of the glass show, as AppKit's does.
	Tint ui.Color
	// Interactive makes the glass grow a little while the element is
	// pressed, as AppKit's interactive glass does.
	Interactive bool
	// Native asks for the system's glass where the window has it (macOS
	// 26 and later), drawn under the content: it samples what is under the
	// window's surface, as a ui.WebView's page, not the elements the frame
	// painted. Elsewhere, and with Reduce Transparency, the shader draws
	// it.
	Native bool
	// Clarity, from 0 to 1, makes panes as large as sidebars clearer,
	// the content under them reading through: 0, the default, is
	// macOS's material, as frosty as a control's; 0.5 blurs half as much
	// and lightens (or darkens) less; 1 is nearly clear. Panes up to 100
	// DIPs keep their material whatever it is; it takes full effect from
	// 200. On Clear glass, which barely tones already, it only lights the
	// rim and curves the bezel.
	Clarity float32
	// hover follows the pointer over an interactive glass, from 0 to 1.
	hover float32
}

// pressKey is the key of an interactive glass's animation.
type pressKey struct{}

// hoverKey is the key of an interactive glass's hover animation.
type hoverKey struct{}

// pressed is an interactive glass as its element is built: how far it
// has grown, from 0 to 1, and how far the pointer hovers it.
type pressed struct {
	Glass
	grow, hover float32
}

// BuildMaterial follows the press and hover of an interactive glass's
// element: the glass grows as it is pressed, and lights its rim as the
// pointer enters it, within 150 ms, as AppKit's does.
func (g Glass) BuildMaterial(e ui.Element) ui.Material {
	if !g.Interactive {
		return g
	}
	target := float32(0)
	if e.Pressed() {
		target = 1
	}
	hover := float32(0)
	if e.Hovered() {
		hover = 1
	}
	return pressed{g, e.Animate(pressKey{}, target, 150*time.Millisecond),
		e.Animate(hoverKey{}, hover, 120*time.Millisecond)}
}

// Style is the material of Glass.
type Style uint8

const (
	// Regular frosts what shows through and lightens it, or darkens it in
	// dark mode, so that what is on the glass reads over anything: for
	// bars, buttons and panels. Larger panes are frostier.
	Regular Style = iota
	// Clear barely blurs nor tones what shows through, for glass over
	// photos and video, where what is on it brings its own contrast.
	Clear
)

// NativeGlass asks for the system's glass, which ui places under the
// surface, where it has Liquid Glass.
func (g Glass) NativeGlass() (ui.NativeGlassSpec, bool) {
	if !g.Native {
		return ui.NativeGlassSpec{}, false
	}
	style := 0
	if g.Style == Clear {
		style = 1
	}
	return ui.NativeGlassSpec{Style: style, Tint: g.Tint}, true
}

// PaintMaterial paints the glass over box, rounded by radii.
func (g Glass) PaintMaterial(p *ui.Painter, box ui.Rect, radii [4]float32) {
	if g.Native && p.NativeGlassSupported() && !p.Preferences().ReduceTransparency {
		// The system's glass, under the surface, shows through this hole.
		p.Hole(box, radii)
		return
	}
	paint(p, box, radii, g)
}

// PaintMaterial paints the glass grown by how far it is pressed: as
// macOS 27's, measured from NSGlassEffectView, by about 1.1 DIPs left and
// right and 0.45 DIPs above and below, whatever its size. AppKit stretches
// the content too, a pixel or so, which the children here are not.
func (pressed) NativeGlass() (ui.NativeGlassSpec, bool) {
	return ui.NativeGlassSpec{}, false
}

func (g pressed) PaintMaterial(p *ui.Painter, box ui.Rect, radii [4]float32) {
	dx, dy := 1.1*g.grow, 0.45*g.grow
	box = ui.Rect{X: box.X - dx, Y: box.Y - dy, W: box.W + 2*dx, H: box.H + 2*dy}
	for i := range radii {
		if radii[i] > 0 {
			radii[i] += dy
		}
	}
	g.Glass.hover = g.hover
	paint(p, box, radii, g.Glass)
}

// String describes the glass, as the inspector shows it.
func (g Glass) String() string {
	s := "glass"
	if g.Style == Clear {
		s += " clear"
	}
	if g.Tint.A > 0 {
		s += fmt.Sprintf(" tint(#%02x%02x%02x%02x)", g.Tint.R, g.Tint.G, g.Tint.B, g.Tint.A)
	}
	if g.Native {
		s += " native"
	}
	if g.Interactive {
		s += " interactive"
	}
	if g.Clarity > 0 {
		s += fmt.Sprintf(" clarity(%g)", g.Clarity)
	}
	return s
}

// Paint paints a pane of glass shaped as r rounded by radius, in a
// drawing (Element.Draw), over what the painter painted before it.
func Paint(p *ui.Painter, r ui.Rect, radius float32, g Glass) {
	paint(p, r, [4]float32{radius, radius, radius, radius}, g)
}

// paint paints a pane of glass over box, rounded by radii.
//
// Its material follows macOS 27's, measured from NSGlassEffectView: the
// regular one maps black to 54% and white to 100% in light mode, and to
// 15% and 51% in dark mode, the clear one adds an eighth; larger panes
// blur more, up to 10 DIPs; the bezel curves the last 36 DIPs of the
// pane, at most half of it, and bends what shows through by up to 1.6
// times that, mirroring what is inside it, as AppKit's do; light comes
// from above and below, along a rim a pixel wide, and the pane casts no
// shadow, as NSGlassEffectView's and the glass bezel's do not. Panes
// large as sidebars and inspectors are clearer by the glass's Clarity
// (paneMaterial).
func paint(p *ui.Painter, box ui.Rect, radii [4]float32, g Glass) {
	if box.W <= 0 || box.H <= 0 {
		return
	}
	if p.Preferences().ReduceTransparency {
		paintOpaque(p, box, radii, g)
		return
	}
	mat := paneMaterial(min(box.W, box.H), p.Scale(), g.Clarity, g.Style == Clear, p.Theme().Dark)
	if p.Preferences().HighContrast {
		mat.rim = min(mat.rim+0.15, 1)
		mat.rimWidth = max(mat.rimWidth, 1.5)
	}
	if g.hover > 0 {
		// As the pointer enters an interactive glass, its rim lights and
		// its surface lifts a little.
		mat.rim = min(mat.rim+0.18*g.hover, 1)
		mat.low = min(mat.low+0.02*g.hover, 1)
		mat.high = min(mat.high+0.02*g.hover, 1)
	}
	if g.Tint.A > 0 {
		mat.tint, mat.wideTint = p.EffectColor(g.Tint)
		a := 0.88 * (1 + 0.1*g.hover)
		mat.tint[3] = min(mat.tint[3]*a, 1)
		mat.wideTint[3] = min(mat.wideTint[3]*a, 1)
	}
	p.Effect(Effect, box, radii, mat.blur, mat.params())
}

// paintOpaque paints g as the opaque surface that the desktop asks for
// with Reduce Transparency: the theme's surface, or its background for
// clear glass, tinted as g is, without blur, refraction or a rim.
func paintOpaque(p *ui.Painter, box ui.Rect, radii [4]float32, g Glass) {
	t := p.Theme()
	c := t.Surface
	if g.Style == Clear {
		c = t.Background
	}
	if g.Tint.A > 0 {
		c = c.Mix(g.Tint, float32(g.Tint.A)/255*0.88)
	}
	p.FillRadii(box, radii, c)
	if p.Preferences().HighContrast {
		p.Stroke(box, t.Border, max(radii[0], radii[1], radii[2], radii[3]), 1)
	}
}

// paneMaterial is the material of a pane whose shorter side is m DIPs,
// at scale s, of clarity k from 0 to 1, clear or regular, in dark mode or
// light. Panes up to 100 DIPs, as controls, keep the measured material;
// from 200, as sidebars and inspectors, theirs is clarity's; between, it
// eases from one to the other. Clarity goes in two straight steps, each
// half of it, from the measured material (frost) to a clearer one (mid),
// the content under the pane reading through, and on to one nearly clear
// (clear): see paneTone.
func paneMaterial(m, s, k float32, clear, dark bool) material {
	f := min(max((m-100)/100, 0), 1)
	f = f * f * (3 - 2*f) // smoothstep
	k = min(max(k, 0), 1)
	if k != k { // NaN
		k = 0
	}
	at := func(v paneTone) float32 { return v.frost + (v.at(k)-v.frost)*f }
	bezel := min(at(paneBezel), m/2)
	mat := material{
		bezel:      bezel * s,
		refraction: 1.6 * bezel * s,
		rim:        at(paneRim),
		rimWidth:   1,
		light:      math.Pi / 2,
	}
	switch {
	case clear:
		mat.blur = s
		mat.low, mat.high, mat.curve, mat.saturation = 0.125, 1.082, 1, 1
	case dark:
		mat.blur = min(max(m*0.035, 1), at(paneBlur)) * s
		mat.low, mat.high, mat.curve, mat.saturation = at(paneDarkLow), at(paneDarkHigh), 2, 2.2
	default:
		mat.blur = min(max(m*0.035, 1), at(paneBlur)) * s
		mat.low, mat.high, mat.curve, mat.saturation = at(paneLightLow), 1, 1.2, 1
	}
	return mat
}

// paneTone is a parameter of a large pane's material at clarity 0
// (frost, the measured material's), 0.5 (mid) and 1 (clear).
type paneTone struct{ frost, mid, clear float32 }

// at is the parameter at clarity k, from 0 to 1.
func (v paneTone) at(k float32) float32 {
	if k <= 0.5 {
		return v.frost + (v.mid-v.frost)*k*2
	}
	return v.mid + (v.clear-v.mid)*(k-0.5)*2
}

var (
	// paneBlur is the most the pane blurs, in DIPs.
	paneBlur = paneTone{10, 5, 2}
	// paneLightLow is what black maps to in light mode, white to 1.
	paneLightLow = paneTone{0.541, 0.35, 0.2}
	// paneDarkLow and paneDarkHigh are what black and white map to in
	// dark mode.
	paneDarkLow  = paneTone{0.15, 0.15, 0.1}
	paneDarkHigh = paneTone{0.51, 0.6, 0.85}
	// paneRim is how bright the rim is lit.
	paneRim = paneTone{0.35, 0.5, 0.5}
	// paneBezel is how wide the bezel curves, in DIPs.
	paneBezel = paneTone{36, 48, 48}
)
