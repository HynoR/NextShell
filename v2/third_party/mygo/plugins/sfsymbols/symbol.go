package sfsymbols

import (
	"math"

	"github.com/egoist/mygo/ui"
)

// SymbolElement is the element Symbol makes: a ui.Element, with the
// symbol's own modifiers. Each returns the element, so they chain as
// SwiftUI's do, and the ui.Element's own methods (Margin, Tooltip, ...)
// are there too.
type SymbolElement struct {
	ui.Element
	s *symbol
}

// symbol is what a SymbolElement draws, read when it paints.
type symbol struct {
	name     string
	color    ui.Color
	weight   Weight
	fill     bool
	natural  bool
	fallback *ui.SVG
}

// Symbol shows the SF Symbol named name, as SwiftUI's
// Image(systemName:): at the theme's font size and in its text color,
// drawn sharp at the display's scale, and named for assistive technology.
//
//	sfsymbols.Symbol(c, "gearshape")
//	sfsymbols.Symbol(c, "trash").Size(20).Color(t.Accent).Weight(sfsymbols.Semibold).Fill()
//	sfsymbols.Symbol(c, "sparkles").Fallback(sparkles) // an SVG where the system has none
//
// Where the system has no symbol of that name, on another OS or a macOS
// without it, it draws its Fallback, or nothing, at the same size.
func Symbol(c *ui.Context, name string) SymbolElement {
	t := c.Theme()
	s := &symbol{name: name, color: t.Text}
	size := t.FontSize
	if size <= 0 {
		size = 13
	}
	e := ui.Box(c).Size(size, size).Shrink(0).Role(ui.RoleImage).Label(name)
	e.Draw(s.paint)
	return SymbolElement{Element: e, s: s}
}

// Size sets the symbol's box, size DIPs square.
func (e SymbolElement) Size(size float32) SymbolElement {
	e.Element.Size(size, size)
	return e
}

// Color draws the symbol in col rather than the theme's text color, as
// SwiftUI's foregroundStyle.
func (e SymbolElement) Color(col ui.Color) SymbolElement {
	if e.s != nil {
		e.s.color = col
	}
	return e
}

// Weight draws the symbol's strokes at w, as SwiftUI's fontWeight.
func (e SymbolElement) Weight(w Weight) SymbolElement {
	if e.s != nil {
		e.s.weight = w
	}
	return e
}

// Fill draws the symbol's filled variant where it has one ("trash.fill"
// for "trash"), and the symbol itself where it has none, as SwiftUI's
// symbolVariant(.fill).
func (e SymbolElement) Fill() SymbolElement {
	if e.s != nil {
		e.s.fill = true
	}
	return e
}

// Natural draws the symbol at the size it has beside others of the same
// point size, rather than filling its box: see Options.Natural.
func (e SymbolElement) Natural() SymbolElement {
	if e.s != nil {
		e.s.natural = true
	}
	return e
}

// Fallback draws svg, in the symbol's color, where the system has no such
// symbol.
func (e SymbolElement) Fallback(svg *ui.SVG) SymbolElement {
	if e.s != nil {
		e.s.fallback = svg
	}
	return e
}

func (s *symbol) paint(p *ui.Painter, r ui.Rect) {
	side := min(r.W, r.H)
	if side <= 0 {
		return
	}
	box := ui.Rect{X: r.X + (r.W-side)/2, Y: r.Y + (r.H-side)/2, W: side, H: side}
	scale := p.Scale()
	if scale <= 0 {
		scale = 1
	}
	px := int(math.Ceil(float64(side * scale)))
	o := Options{Natural: s.natural, Weight: s.weight}
	if s.fill {
		if src, ok := Image(s.name+".fill", px, s.color, o); ok {
			p.Image(src, box, ui.Contain)
			return
		}
	}
	if src, ok := Image(s.name, px, s.color, o); ok {
		p.Image(src, box, ui.Contain)
		return
	}
	if s.fallback != nil {
		p.Icon(s.fallback, box, s.color)
	}
}
