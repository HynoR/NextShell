package glass

import (
	"fmt"
	"math"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/shader"
)

// Group is a row of elements on one pane of glass, whose corners merge
// while they are close, as macOS's NSGlassEffectContainerView draws them:
//
//	g := glass.NewGroup(c)
//	g.Radius = 22
//	g.Gap(6).Children(func() {
//		g.Pane(ui.Box(c).Padding(8, 16).Center().Children(func() { ui.Text(c, "Cut") }))
//		g.Pane(ui.Box(c).Padding(8, 16).Center().Children(func() { ui.Text(c, "Copy") }))
//	})
//
// The elements are an ordinary row's; the glass is painted behind them,
// one surface over all of them, so that it merges where they nearly touch
// instead of showing two panes. The elements keep their own backgrounds,
// borders and hit areas.
type Group struct {
	ui.Element
	// Style is the group's material, as Glass's.
	Style Style
	// Tint colors the glass, as Glass's.
	Tint ui.Color
	// Clarity makes large groups clearer, as Glass's.
	Clarity float32
	// Radius is the corners of each element's pane, in DIPs.
	Radius float32
	// Merge is how smoothly panes blend into one surface, in DIPs: 24 by
	// default, which bridges panes up to about 12 DIPs apart; 0 keeps
	// them apart.
	Merge float32
	// panes are the elements whose boxes the glass covers.
	panes []ui.Element
}

// NewGroup returns an empty Group, a row, whose Material paints the glass
// behind the elements its Pane adds.
func NewGroup(c *ui.Context) *Group {
	g := &Group{Element: ui.Row(c), Merge: 24}
	g.Element.Material(g)
	return g
}

// Pane adds e to the group's glass, and returns it.
func (g *Group) Pane(e ui.Element) ui.Element {
	g.panes = append(g.panes, e)
	return e
}

// PaintMaterial paints the group's glass over the union of its panes.
func (g *Group) PaintMaterial(p *ui.Painter, box ui.Rect, _ [4]float32) {
	if box.W <= 0 || box.H <= 0 || len(g.panes) == 0 {
		return
	}
	rects := make([]ui.Rect, 0, len(g.panes))
	minSide := float32(math.MaxFloat32)
	for i := range g.panes {
		b := g.panes[i].LayoutBounds()
		if b.W <= 0 || b.H <= 0 {
			continue
		}
		b = ui.Rect{X: b.X - box.X, Y: b.Y - box.Y, W: b.W, H: b.H}
		rects = append(rects, b)
		minSide = min(minSide, b.W, b.H)
	}
	if len(rects) == 0 {
		return
	}
	if p.Preferences().ReduceTransparency {
		c := p.Theme().Surface
		if g.Style == Clear {
			c = p.Theme().Background
		}
		if g.Tint.A > 0 {
			c = c.Mix(g.Tint, float32(g.Tint.A)/255*0.88)
		}
		for _, r := range rects {
			p.FillRadii(r, [4]float32{g.Radius, g.Radius, g.Radius, g.Radius}, c)
		}
		return
	}
	mat := paneMaterial(minSide, p.Scale(), g.Clarity, g.Style == Clear, p.Theme().Dark)
	if p.Preferences().HighContrast {
		mat.rim = min(mat.rim+0.15, 1)
		mat.rimWidth = max(mat.rimWidth, 1.5)
	}
	tint := [4]float32{0, 0, 0, 0}
	if g.Tint.A > 0 {
		tint = [4]float32{float32(g.Tint.R) / 255, float32(g.Tint.G) / 255, float32(g.Tint.B) / 255, float32(g.Tint.A) / 255 * 0.88}
	}
	groupShader.Draw(p, box, shader.Options{
		Uniforms: map[string]any{
			"Count":      len(rects),
			"Rects":      groupRects(rects),
			"Radii":      groupRadii(len(rects), g.Radius),
			"Bezel":      mat.bezel / p.Scale(),
			"Refraction": mat.refraction / p.Scale(),
			"Rim":        mat.rim,
			"RimWidth":   mat.rimWidth / p.Scale(),
			"Low":        mat.low,
			"High":       mat.high,
			"Curve":      mat.curve,
			"Saturation": mat.saturation,
			"Merge":      g.Merge,
			"Tint":       tint[:],
			"Light":      []float32{float32(math.Cos(float64(mat.light))), float32(math.Sin(float64(mat.light)))},
		},
		BackdropBlur: mat.blur / p.Scale(),
	})
}

// String describes the group, as the inspector shows it.
func (g *Group) String() string {
	return fmt.Sprintf("glass group(%d)", len(g.panes))
}

// groupRects lays out up to maxGroupPanes rects for the shader, in DIPs.
func groupRects(rects []ui.Rect) []float32 {
	out := make([]float32, maxGroupPanes*4)
	for i, r := range rects {
		if i >= maxGroupPanes {
			break
		}
		out[i*4], out[i*4+1], out[i*4+2], out[i*4+3] = r.X, r.Y, r.W, r.H
	}
	return out
}

func groupRadii(n int, radius float32) []float32 {
	out := make([]float32, maxGroupPanes)
	for i := range n {
		if i >= maxGroupPanes {
			break
		}
		out[i] = radius
	}
	return out
}

const maxGroupPanes = 8

// groupShader draws the union of the panes: one bezel around the whole
// surface, as the panes merge.
var groupShader = shader.MustCompile(`package main

var Count int
var Rects [32]float
var Radii [8]float
var Merge float
var Bezel float
var Refraction float
var Rim float
var RimWidth float
var Low float
var High float
var Curve float
var Saturation float
var Tint vec4
var Light vec2

func sdPane(p vec2, i int) float {
	r := vec4(Rects[i*4], Rects[i*4+1], Rects[i*4+2], Rects[i*4+3])
	h := r.zw / 2
	rad := min(Radii[i], min(h.x, h.y))
	q := abs(p - r.xy - h) - (h - vec2(rad, rad))
	m := max(q, vec2(0, 0))
	return length(m) + min(max(q.x, q.y), 0.0) - rad
}

func smin(a float, b float, k float) float {
	if k <= 0.0 {
		return min(a, b)
	}
	h := clamp(0.5+0.5*(b-a)/k, 0.0, 1.0)
	return mix(b, a, h) - k*h*(1.0-h)
}

func unionSDF(p vec2) float {
	d := 1.0e9
	for i := 0; i < 8; i++ {
		if i < Count {
			d = smin(d, sdPane(p, i), Merge)
		}
	}
	return d
}

func normalAt(p vec2) vec2 {
	e := 1.0 / pixelScale()
	n := vec2(unionSDF(p+vec2(e, 0))-unionSDF(p-vec2(e, 0)), unionSDF(p+vec2(0, e))-unionSDF(p-vec2(0, e)))
	l := length(n)
	if l < 0.00001 {
		return vec2(0, 1)
	}
	return n / l
}

func lens(t float, bezel float) float {
	if bezel <= 0.0 || t >= bezel {
		return 0.0
	}
	u := 1.0 - max(t, 0.0) / bezel
	v := 1.0 - u*u*u*u
	a := sqrt(v * sqrt(v))
	b := u*u*u
	n := sqrt(a*a + b*b)
	s := b / n
	c := a / n
	st := s / 1.5
	ct := sqrt(1.0 - st*st)
	return (s*ct - c*st) / (c*ct + s*st) / 1.118034
}

func tone(c vec3) vec3 {
	l := 0.2126*c.r + 0.7152*c.g + 0.0722*c.b
	t := Low + (High-Low)*(1.0-pow(max(1.0-l, 0.0), Curve))
	k := (High - Low) * Saturation
	return clamp(vec3(t, t, t) + (c - vec3(l, l, l))*k, vec3(0, 0, 0), vec3(1, 1, 1))
}

func Fragment(dst vec4, pos vec2) vec4 {
	d := unionSDF(pos)
	a := clamp(0.5-d*pixelScale(), 0.0, 1.0)
	if a <= 0.0 {
		return vec4(0, 0, 0, 0)
	}
	t := max(-d, 0.0)
	n := vec2(0, 1)
	if t < Bezel || t < 3.0*RimWidth {
		n = normalAt(pos)
	}
	q := pos
	if t < Bezel {
		q = q - n*(Refraction*lens(t, Bezel))
	}
	c := tone(backdropSample(q).rgb)
	c = c + (Tint.rgb-c)*Tint.a
	if Rim > 0.0 && t < 3.0*RimWidth {
		r := t / RimWidth
		lit := Rim * exp(-r*r)
		l := abs(dot(n, Light))
		c = c + (vec3(l, l, l) - c) * lit
	}
	return vec4(c.x*a, c.y*a, c.z*a, a)
}
`)
