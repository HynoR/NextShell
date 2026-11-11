package text

import (
	"math"
	"slices"
	"unicode"
)

// Decoration is a line drawn along text.
type Decoration uint8

const (
	Underline Decoration = iota
	Strikethrough
	// SolidUnderline is an Underline drawn through descenders, as links
	// mark hover, instead of skipping the ink of glyphs as Core Text does.
	SolidUnderline
)

// LineStyle is how a decoration's line is drawn where the font leaves it
// otherwise.
type LineStyle struct {
	// Thick, if positive, is the line's thickness in DIPs, around its
	// middle, rounded to whole pixels (at least one) and on whole pixels,
	// so that it is as sharp as browsers draw text-decoration-thickness.
	Thick float32
	// Offset, if HasOffset, puts the top of an underline Offset DIPs below
	// the baseline, or above it if negative, rounded to whole pixels, as
	// CSS's text-underline-offset does: an underline skipping ink skips
	// the glyphs where it is then. Strikethroughs stay where they are.
	Offset    float32
	HasOffset bool
}

// A Stroke is a stretch of a decoration's line, in device pixels: from X0
// to X1 and from Top to Bottom, which renderers antialias where they fall
// within pixels.
type Stroke struct{ X0, X1, Top, Bottom float32 }

// decoRange is what an engine decorates: glyphs [i, j) of a line of a
// layout whose origin is at (x, y) DIPs, runes of one style whose font is
// font, shift DIPs above the line's baseline (Span.BaselineShift), at
// scale pixels per DIP, with decoration d.
type decoRange struct {
	l     *Layout
	line  *Line
	i, j  int
	font  *Font
	x, y  float32
	shift float32
	scale float32
	d     Decoration
	// solid keeps an underline whole, through the ink of glyphs.
	solid bool
	// offset, if hasOffset, is how far below the baseline the top of an
	// underline is, in DIPs (LineStyle.Offset).
	offset    float32
	hasOffset bool
}

// Decorate returns the strokes the system's text stack draws for
// decoration d of glyphs [i, j) of line li of l, runes of one style, with
// the layout's origin at (x, y) DIPs, at scale pixels per DIP: placed as
// AppKit, Direct2D or GTK place underlines and strikethroughs, with their
// thickness, but for what ls sets.
func (s *System) Decorate(l *Layout, li, i, j int, x, y, scale float32, d Decoration, ls LineStyle) []Stroke {
	s.mu.Lock()
	defer s.mu.Unlock()
	solid := d == SolidUnderline
	if solid {
		d = Underline
	}
	if l == nil || li < 0 || li >= len(l.Lines) {
		return nil
	}
	line := &l.Lines[li]
	i, j = max(i, 0), min(j, len(line.Glyphs))
	if i >= j {
		return nil
	}
	style := l.Params.Style
	var shift float32
	if l.Params.Spans != "" {
		r := line.Glyphs[i].Cluster
		for _, sp := range DecodeSpans(l.Params.Spans) {
			if r < sp.End {
				style, shift = sp.style(style), sp.BaselineShift
				break
			}
		}
	}
	f := s.font(style)
	if f == nil {
		return nil
	}
	r := decoRange{l: l, line: line, i: i, j: j, font: f, x: x, y: y, shift: shift, scale: scale, d: d, solid: solid,
		offset: ls.Offset, hasOffset: ls.HasOffset && d == Underline}
	strokes := s.engine().decorate(r)
	if ls.Thick > 0 {
		h := max(float32(math.Round(float64(ls.Thick*scale))), 1)
		for k := range strokes {
			st := &strokes[k]
			st.Top = float32(math.Round(float64(st.Top+st.Bottom-h) / 2))
			st.Bottom = st.Top + h
		}
	}
	if r.hasOffset {
		// From the baseline the engine draws the glyphs on, a whole pixel.
		top := s.engine().baseline(r.baseline()*scale) + float32(math.Round(float64(ls.Offset*scale)))
		for k := range strokes {
			st := &strokes[k]
			st.Top, st.Bottom = top, top+st.Bottom-st.Top
		}
	}
	return strokes
}

// runs calls fn for each run of glyphs of one font among [i, j).
func (r *decoRange) runs(fn func(f *Font, i, j int)) {
	gs := r.line.Glyphs
	for i := r.i; i < r.j; {
		j := i + 1
		for j < r.j && gs[j].Font == gs[i].Font {
			j++
		}
		if gs[i].Font != nil {
			fn(gs[i].Font, i, j)
		}
		i = j
	}
}

// span returns the left and right of the advances of glyphs [i, j), in
// DIPs from the layout's origin.
func (r *decoRange) span(i, j int) (x0, x1 float32) {
	x0, x1 = float32(math.MaxFloat32), float32(-math.MaxFloat32)
	for _, g := range r.line.Glyphs[i:j] {
		x0, x1 = min(x0, g.X), max(x1, g.X+g.Advance)
	}
	return x0, x1
}

// rune returns the rune glyph k starts, or 0.
func (r *decoRange) rune(k int) rune {
	if c := r.line.Glyphs[k].Cluster; c >= 0 && c < len(r.l.Runes) {
		return r.l.Runes[c]
	}
	return 0
}

// baseline returns the baseline of the range's glyphs, in DIPs from the
// top, as laid out: the line's, unless their span shifts them.
func (r *decoRange) baseline() float32 { return r.y + r.line.Baseline - r.shift }

// cut returns [x0, x1] less the intervals of cuts, leaving out pieces no
// longer than min.
func cut(x0, x1 float32, cuts [][2]float32, minLen float32) [][2]float32 {
	slices.SortFunc(cuts, func(a, b [2]float32) int {
		switch {
		case a[0] < b[0]:
			return -1
		case a[0] > b[0]:
			return 1
		}
		return 0
	})
	var out [][2]float32
	at := x0
	for _, c := range cuts {
		if c[0] > at {
			if end := min(c[0], x1); end-at > minLen {
				out = append(out, [2]float32{at, end})
			}
		}
		at = max(at, c[1])
		if at >= x1 {
			break
		}
	}
	if x1-at > minLen {
		out = append(out, [2]float32{at, x1})
	}
	return out
}

// skipsInk reports whether a rune's glyph cuts the underlines skipping
// ink: Han, Kana and Hangul do not, as in AppKit.
func skipsInk(r rune) bool {
	return !unicode.IsSpace(r) && !unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul)
}

// bandX returns the left and right of the parts of segments (x0, y0, x1,
// y1 each) within lo ≤ y ≤ hi.
func bandX(segments [][4]float64, lo, hi float64) (a, b float64, ok bool) {
	a, b = math.Inf(1), math.Inf(-1)
	for _, s := range segments {
		px, py, qx, qy := s[0], s[1], s[2], s[3]
		t0, t1 := 0.0, 1.0
		if dy := qy - py; dy == 0 {
			if py < lo || py > hi {
				continue
			}
		} else {
			ta, tb := (lo-py)/dy, (hi-py)/dy
			if ta > tb {
				ta, tb = tb, ta
			}
			t0, t1 = max(t0, ta), min(t1, tb)
			if t0 > t1 {
				continue
			}
		}
		xa, xb := px+(qx-px)*t0, px+(qx-px)*t1
		a, b = min(a, xa, xb), max(b, xa, xb)
	}
	return a, b, a <= b
}
