package text

import (
	"math"
	"slices"
	"unicode"

	"github.com/go-text/typesetting/segmenter"
)

// softHyphen is a break opportunity that shows as a hyphen where a line
// breaks at it, and as nothing elsewhere.
const softHyphen = '\u00AD'

// breakLines breaks text, a paragraph without newlines, into lines of at
// most width DIPs (one line when 0), the first indent narrower, and shapes
// them with the engine, which breaks at one width. A line that breaks at
// a soft hyphen ends with a hyphen, for which only Pango leaves room: one
// that does not fit with it is broken again, narrower by the hyphen.
// So is a line that does not fit with the padding of its spans' pieces
// (Span.PaddingX), which engines know nothing of: narrower by it.
// What follows the indented line, or a line that broke elsewhere then, is
// broken anew: a paragraph without either is shaped once. hy is nil for a
// text without soft hyphens.
func (s *System) breakLines(text []rune, style Style, spans []Span, width, indent float32, rtl, wholeWords bool, hy *hyphens) []shapedLine {
	e := s.engine()
	if width <= 0 {
		return e.shape(text, style, spans, 0, rtl, wholeWords)
	}
	pads := padded(spans)
	if indent <= 0 && hy == nil && !pads {
		return e.shape(text, style, spans, width, rtl, wholeWords)
	}
	var out []shapedLine
	for pos := 0; pos < len(text); {
		limit, first := width, len(out) == 0 && indent > 0
		if first {
			limit = max(width-indent, 1)
		}
		lines := e.shape(text[pos:], style, spansIn(spans, pos, len(text)), limit, rtl, wholeWords)
		if len(lines) == 0 {
			break
		}
		if first {
			// The lines after the first are for the width.
			lines = lines[:1]
		}
		offsetLines(lines, pos)
		for _, l := range lines {
			if c := softBreak(l, text); c >= 0 && hy != nil && hy.lineWidth(l, text, c) > limit+1e-3 {
				narrow := e.shape(text[l.start:l.end], style, spansIn(spans, l.start, l.end), max(limit-hy.width(c), 1), rtl, wholeWords)
				if len(narrow) > 0 && narrow[0].end != l.end && hy.opportunity(text, l.start+narrow[0].end) {
					offsetLines(narrow[:1], l.start)
					out = append(out, narrow[0])
					break
				}
				// Nothing before fits but a part of a word: the hyphen
				// overflows, as a word too long for a line does.
			}
			if pads {
				if w, pad := paddedWidth(l, text, spans); w+pad > limit+1e-3 {
					narrow := e.shape(text[l.start:l.end], style, spansIn(spans, l.start, l.end), max(limit-pad, 1), rtl, wholeWords)
					if len(narrow) > 0 && narrow[0].end > 0 && narrow[0].end != l.end-l.start {
						offsetLines(narrow[:1], l.start)
						out = append(out, narrow[0])
						break
					}
					// Its one word overflows with its padding, as without.
				}
			}
			out = append(out, l)
		}
		end := out[len(out)-1].end
		if end <= pos {
			break
		}
		pos = end
	}
	return out
}

// padded reports whether a span pads its pieces of lines (Span.PaddingX).
func padded(spans []Span) bool {
	for _, sp := range spans {
		if sp.PaddingX > 0 {
			return true
		}
	}
	return false
}

// padOf returns the padding of span k at each end of its pieces of lines,
// 0 past the spans.
func padOf(spans []Span, k int) float32 {
	if k < 0 || k >= len(spans) {
		return 0
	}
	return max(spans[k].PaddingX, 0)
}

// paddedWidth returns the width of the glyphs of a shaped line of a
// paragraph text, the whitespace ending it left out, and the padding of
// its spans' pieces.
func paddedWidth(l shapedLine, text []rune, spans []Span) (w, pad float32) {
	trim := l.end
	for trim > l.start && hangs(text[trim-1]) {
		trim--
	}
	k := -1
	for _, r := range l.runs {
		for _, g := range r.glyphs {
			if g.Cluster >= trim && g.Cluster < l.end {
				continue
			}
			w += g.Advance
			if g.Cluster < 0 {
				continue // the ellipsis, with the glyphs before it
			}
			if j := spanIndex(spans, g.Cluster); j != k {
				pad += padOf(spans, k) + padOf(spans, j)
				k = j
			}
		}
	}
	return w, pad + padOf(spans, k)
}

// padLine moves the glyphs of a line, in visual order, apart by the
// padding of its spans' pieces, at both ends of each, as CSS pads each
// line's fragment of an inline box with box-decoration-break: clone, and
// widens the line by it. The line's runes start at rune offset of the
// spans' text.
func padLine(line *Line, spans []Span, offset int) {
	dx, k := float32(0), -1
	for i := range line.Glyphs {
		g := &line.Glyphs[i]
		// The ellipsis goes with the glyphs before it.
		if g.Runes > 0 {
			if j := spanIndex(spans, g.Cluster-offset); j != k {
				dx += padOf(spans, k) + padOf(spans, j)
				k = j
			}
		}
		g.X += dx
	}
	dx += padOf(spans, k)
	line.Width += dx
	line.padding = dx
}

// offsetLines moves lines an engine shaped from rune offset of a
// paragraph to their places in it.
func offsetLines(lines []shapedLine, offset int) {
	if offset == 0 {
		return
	}
	for i := range lines {
		l := &lines[i]
		l.start += offset
		l.end += offset
		for j := range l.runs {
			r := &l.runs[j]
			r.start += offset
			r.end += offset
			for k := range r.glyphs {
				if r.glyphs[k].Cluster >= 0 {
					r.glyphs[k].Cluster += offset
				}
			}
		}
	}
}

// softBreak returns the soft hyphen a line of a paragraph text breaks
// after, or -1 when it ends otherwise.
func softBreak(l shapedLine, text []rune) int {
	if l.end > l.start && l.end < len(text) && text[l.end-1] == softHyphen {
		return l.end - 1
	}
	return -1
}

// hyphens shapes the hyphens that soft hyphens show as where lines break,
// in the style of each, once a paragraph.
type hyphens struct {
	e     engine
	style Style
	spans []Span
	// glyphs holds the hyphen of each span, by its index, len(spans) for
	// the text's style.
	glyphs map[int][]Glyph
	// opps tells where the paragraph's lines may break, once asked.
	opps []bool
}

// opportunity reports whether a line of the paragraph text may break
// before rune i, as Unicode's line breaking algorithm says.
func (h *hyphens) opportunity(text []rune, i int) bool {
	if h.opps == nil {
		var seg segmenter.Segmenter
		h.opps = lineBreaks(&seg, nil, text)
	}
	return i >= 0 && i < len(h.opps) && h.opps[i]
}

// at returns the glyphs of the hyphen of rune i, from a pen at 0.
func (h *hyphens) at(i int) []Glyph {
	k := spanIndex(h.spans, i)
	if g, ok := h.glyphs[k]; ok {
		return g
	}
	style := h.style
	if k < len(h.spans) {
		style = h.spans[k].style(style)
	}
	// U+2010 HYPHEN would be the better glyph, but fonts lack it more
	// often than U+002D, which every one has.
	var out []Glyph
	for _, l := range h.e.shape([]rune{'-'}, style, nil, 0, false, false) {
		for _, r := range l.runs {
			out = append(out, r.glyphs...)
		}
	}
	if h.glyphs == nil {
		h.glyphs = map[int][]Glyph{}
	}
	h.glyphs[k] = out
	return out
}

// width returns the advance of the hyphen of rune i.
func (h *hyphens) width(i int) float32 {
	var w float32
	for _, g := range h.at(i) {
		w += g.Advance
	}
	return w
}

// lineWidth returns the width of a line of the paragraph text ending
// with the soft hyphen c, once its hyphen shows and its other soft
// hyphens take no room.
func (h *hyphens) lineWidth(l shapedLine, text []rune, c int) float32 {
	w := h.width(c)
	for _, r := range l.runs {
		for _, g := range r.glyphs {
			if g.Cluster < 0 || g.Cluster >= len(text) || text[g.Cluster] != softHyphen {
				w += g.Advance
			}
		}
	}
	return w
}

// place makes the soft hyphens of a shaped line of a paragraph text show
// as Unicode says, whatever the engine made of them: a hyphen for the one
// the line breaks after, when broken, and nothing, taking no room, for
// the others. The glyphs after one move by the room it takes or gives.
// rtl is the paragraph's direction, whose end a hyphen goes to when the
// engine gave its soft hyphen no glyph.
func (h *hyphens) place(l shapedLine, text []rune, broken, rtl bool) shapedLine {
	placed := false
	for ri := range l.runs {
		gs := l.runs[ri].glyphs
		for j := 0; j < len(gs); j++ {
			c := gs[j].Cluster
			if c < 0 || c >= len(text) || text[c] != softHyphen {
				continue
			}
			// Its cluster's glyphs, as the empty glyph and the hyphen Pango
			// gives one it breaks at.
			k := j + 1
			for k < len(gs) && gs[k].Cluster == c {
				k++
			}
			left, was := gs[j].X, float32(0)
			for _, g := range gs[j:k] {
				left = min(left, g.X)
				was += g.Advance
			}
			glyphRTL := gs[j].RTL
			var now float32
			var with []Glyph
			if broken && c == l.end-1 {
				with, now = h.glyphsAt(c, left, glyphRTL), h.width(c)
				placed = true
			}
			if len(with) == 0 {
				// A glyph of no font draws nothing, and keeps the caret's
				// place.
				with = []Glyph{{X: left, Cluster: c, RTL: glyphRTL}}
			}
			if d := now - was; d != 0 {
				for rk := range l.runs {
					for gk := range l.runs[rk].glyphs {
						if g := &l.runs[rk].glyphs[gk]; (rk != ri || gk < j || gk >= k) && g.X >= left+was/2 {
							g.X += d
						}
					}
				}
			}
			gs = slices.Concat(gs[:j], with, gs[k:])
			l.runs[ri].glyphs = gs
			j += len(with) - 1
		}
	}
	if broken && !placed && len(l.runs) > 0 {
		// The engine left the soft hyphen ending the line out, as a
		// character nothing shows: its hyphen ends the line.
		c := l.end - 1
		x0, x1 := float32(math.MaxFloat32), float32(-math.MaxFloat32)
		for _, r := range l.runs {
			for _, g := range r.glyphs {
				x0, x1 = min(x0, g.X), max(x1, g.X+g.Advance)
			}
		}
		x := x1
		if x0 > x1 {
			x = 0
		} else if rtl {
			x = x0 - h.width(c)
		}
		// In the run of the soft hyphen, whose clusters end at it.
		ri := len(l.runs) - 1
		for i, r := range l.runs {
			if r.start <= c && c < r.end {
				ri = i
			}
		}
		l.runs[ri].glyphs = slices.Concat(l.runs[ri].glyphs, h.glyphsAt(c, x, rtl))
		placed = true
	}
	if placed {
		inVisualOrder(&l, l.end-1)
	}
	return l
}

// inVisualOrder moves the glyphs of cluster c, a hyphen the line shows
// where the engine left its soft hyphen, among the glyphs of the line to
// where it shows, keeping them in visual order: in a right-to-left run
// or line, the hyphen, which ends it, is at its left, and the engine may
// have given its soft hyphen a run, or a place in one, of its own.
func inVisualOrder(l *shapedLine, c int) {
	var hy []Glyph
	for ri := range l.runs {
		l.runs[ri].glyphs = slices.DeleteFunc(l.runs[ri].glyphs, func(g Glyph) bool {
			if g.Cluster == c {
				hy = append(hy, g)
				return true
			}
			return false
		})
	}
	if len(hy) == 0 {
		return
	}
	x := hy[0].X
	for _, g := range hy {
		x = min(x, g.X)
	}
	// Before the first glyph right of it.
	for ri := range l.runs {
		gs := l.runs[ri].glyphs
		for j, g := range gs {
			if g.X > x {
				l.runs[ri].glyphs = slices.Concat(gs[:j], hy, gs[j:])
				return
			}
		}
	}
	last := len(l.runs) - 1
	l.runs[last].glyphs = append(l.runs[last].glyphs, hy...)
}

// glyphsAt returns the glyphs of the hyphen of rune i, standing for it,
// from a pen at x.
func (h *hyphens) glyphsAt(i int, x float32, rtl bool) []Glyph {
	var out []Glyph
	for _, g := range h.at(i) {
		g.X += x
		g.Cluster, g.RTL = i, rtl
		out = append(out, g)
	}
	return out
}

// justifyLine stretches a line of a layout of runes to width DIPs, the
// whitespace ending it left out, widening its spaces between words and
// the room after its Han characters and kana but the last, by as much
// each. It returns the left of what it stretched, relative to the line,
// which whitespace ending a right-to-left line may be left of, and
// false, leaving the line as it was, when the line has nothing to widen
// or is that wide already.
func justifyLine(line *Line, runes []rune, width float32) (left float32, ok bool) {
	end := line.End
	for end > line.Start && hangs(runes[end-1]) {
		end--
	}
	// The extent of each cluster of what stretches, by its first rune,
	// and the glyph of each that widens, its rightmost.
	type cluster struct {
		x0, x1 float32
		wide   int
	}
	clusters := make([]cluster, end-line.Start)
	seen := make([]bool, end-line.Start)
	// The padding of spans' pieces takes room as the glyphs do.
	content := line.padding
	last := -1
	left = float32(1e30)
	for i, g := range line.Glyphs {
		c := g.Cluster - line.Start
		if c < 0 || c >= len(clusters) {
			continue
		}
		content += g.Advance
		left = min(left, g.X)
		last = max(last, c)
		cl := &clusters[c]
		if !seen[c] {
			seen[c] = true
			*cl = cluster{g.X, g.X + g.Advance, i}
			continue
		}
		cl.x0 = min(cl.x0, g.X)
		if g.X+g.Advance >= cl.x1 {
			cl.x1, cl.wide = g.X+g.Advance, i
		}
	}
	extra := width - content
	if extra <= 1e-3 {
		return 0, false
	}
	var opps []float32 // the middles of the clusters that widen
	for c := range clusters {
		if seen[c] && c != last && stretches(runes[line.Start+c]) {
			opps = append(opps, (clusters[c].x0+clusters[c].x1)/2)
		}
	}
	if len(opps) == 0 {
		return 0, false
	}
	each := extra / float32(len(opps))
	// Each glyph moves by the room the clusters left of it gain: a cluster
	// right of the line's content, as whitespace ending it, moves too.
	// Lines have few of them: each glyph counts those left of it, rather
	// than a sort, which would grow every app.
	for i := range line.Glyphs {
		g := &line.Glyphs[i]
		mid := g.X + g.Advance/2
		if c := g.Cluster - line.Start; c >= 0 && c < len(clusters) && seen[c] {
			mid = (clusters[c].x0 + clusters[c].x1) / 2
		}
		n := 0
		for _, o := range opps {
			if o < mid {
				n++
			}
		}
		g.X += each * float32(n)
	}
	for c := range clusters {
		if seen[c] && c != last && stretches(runes[line.Start+c]) {
			line.Glyphs[clusters[c].wide].Advance += each
		}
	}
	// No cluster that widens is left of the content's leftmost: it stays.
	line.Width += extra
	return left, true
}

// stretches reports whether justified lines widen the room after a rune:
// a space between words, as CSS's word separators, or a Han character,
// kana or the punctuation written with them, which lines of Chinese and
// Japanese text take apart.
func stretches(r rune) bool {
	switch {
	case r == ' ' || r == ' ' || r == '፡' || r == '　':
		return true
	case r >= 0x3001 && r <= 0x303F, r >= 0xFF01 && r <= 0xFF60:
		return true
	}
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Bopomofo)
}
