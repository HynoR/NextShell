// Package sfsymbols draws Apple's SF Symbols, as the running macOS has
// them, for native UI.
//
//	sfsymbols.Symbol(c, "gearshape")
//	sfsymbols.Symbol(c, "trash").Size(20).Color(t.Accent).Weight(sfsymbols.Semibold).Fill()
//
// Symbol shows one as SwiftUI's Image(systemName:) does; Image gives the
// bitmap, for drawing with a ui.Painter.
//
// The symbols are read from the system: AppKit draws each with its own
// design and weight, and the plugin copies the pixels. Nothing of Apple's
// artwork is in the app or its build. Where the system has no symbol of a
// name, and on every other OS, Image says so and Symbol draws its
// Fallback instead.
package sfsymbols

// Options of a symbol's drawing.
type Options struct {
	// Natural draws the symbol at the size it has beside others of the
	// same point size, rather than filling its box: a cross stays smaller
	// than a wide rectangle, as SF Symbols balance them.
	Natural bool
	// Weight is the weight of the symbol's strokes, Regular by default.
	Weight Weight
}

// Weight is the weight of a symbol's strokes, as a font's.
type Weight int

// The weights of SF Symbols, from the lightest to the heaviest, in that
// order; the zero Weight is Regular.
const (
	UltraLight Weight = iota - 3
	Thin
	Light
	Regular
	Medium
	Semibold
	Bold
	Heavy
	Black
)

// fontWeight returns w as AppKit's NSFontWeight.
func (w Weight) fontWeight() float64 {
	switch w {
	case UltraLight:
		return -0.8
	case Thin:
		return -0.6
	case Light:
		return -0.4
	case Medium:
		return 0.23
	case Semibold:
		return 0.3
	case Bold:
		return 0.4
	case Heavy:
		return 0.56
	case Black:
		return 0.62
	}
	return 0
}
