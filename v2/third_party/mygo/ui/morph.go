package ui

import (
	"math"
	"slices"

	"github.com/egoist/mygo/internal/scene"
	"github.com/egoist/mygo/internal/text"
)

// morphText paints a text changing as ContentTransition's Morph moves it,
// and reports whether it does, noting how one at rest paints otherwise;
// snapMorph has a text morph from another as it shows morphing (as
// matches.start takes it over). ContentTransition links them, so that
// apps without a Morph link none of it.
var (
	morphText func(p *Painter, e *node, r *contentRec, x, y float32, ts textStyle, sp spanPaint, k float32, changing bool) bool
	snapMorph func(src *contentRec, bx, by, x, y float32, color Color, dst *contentRec)
)

func linkMorph() {
	if morphText == nil {
		morphText = (*Painter).morphContent
		snapMorph = snapMorphFrom
	}
}

// morphContent morphs the text of e, as morphText does.
func (p *Painter) morphContent(e *node, r *contentRec, x, y float32, ts textStyle, sp spanPaint, k float32, changing bool) bool {
	if !changing || r.oldTL == nil || e.tl == nil {
		restMorph(r, sp.spans)
		return false
	}
	p.morph(e, r, x, y, ts, sp, k)
	return true
}

// Morphs pair the texts' clusters, of a character or of characters drawn
// as one (as a ligature or a letter with its accents), whose glyphs move
// together, at most this many pairs of them compared to keep their order.
const maxMorphPairs = 1 << 16

// morphPairs pairs the glyphs a text morphs from, from, with the clusters
// of the glyphs of what it is now, cur: pairs is, for each cluster of cur,
// the glyph of from it comes from, -1 for none, and taken marks the glyphs
// of from that one comes from; curAt is the cluster of each rune of cur,
// -1 for none. from are the glyphs of old, laid out as the text was, or,
// snapped, those of another text as it showed morphing.
//
// The old text moves with the element's box, from boxX, boxY, where it
// was when the text was boxOf; the new goes on to where the box goes, toX,
// toY from where it shows, while it is an element of a Match on its way,
// in frame toFrame. spans are those the text painted last, its rich
// text's, which color its glyphs and draw lines and highlights, and
// oldSpans paint the old text's.
type morphPairs struct {
	old, cur   *text.Layout
	from       []morphGlyph
	snapped    bool
	spans      []Span
	oldSpans   *spanPaint
	curC       []morphCluster
	curAt      []int32
	pairs      []int32
	taken      []bool
	lcs        []int32
	boxOf      *text.Layout
	boxX, boxY float32
	toX, toY   float32
	toFrame    uint64
	// lab mixes the glyphs' colors in Oklab (Animation.Oklab).
	lab bool
}

// morphGlyph is a glyph a text morphs from, as it shows: the runes of its
// cluster, for the first glyph of one, its font and glyph, of fontSize, as
// large as size shows, with its pen and baseline at (x, y) in the window,
// in its color, alpha opaque.
type morphGlyph struct {
	runes          []rune
	font           *text.Font
	id             uint32
	fontSize, size float32
	x, y           float32
	color          Color
	alpha          float32
}

// morphCluster is a cluster of a layout's glyphs: its runes, and the pen
// of its first glyph, its baseline and its size.
type morphCluster struct {
	at, n int
	x, y  float32
	size  float32
}

// clustersOf appends the clusters of l to cs, and returns them with the
// cluster of each rune of l, in at.
func clustersOf(l *text.Layout, cs []morphCluster, at []int32) ([]morphCluster, []int32) {
	at = slices.Grow(at[:0], len(l.Runes))[:len(l.Runes)]
	for i := range at {
		at[i] = -1
	}
	for li := range l.Lines {
		line := &l.Lines[li]
		for _, g := range line.Glyphs {
			if g.Object || g.Runes == 0 || g.Cluster < 0 || g.Cluster >= len(at) || at[g.Cluster] >= 0 {
				continue
			}
			at[g.Cluster] = int32(len(cs))
			cs = append(cs, morphCluster{at: g.Cluster, n: min(g.Runes, len(l.Runes)-g.Cluster), x: g.X, y: line.Baseline, size: g.Size})
		}
	}
	return cs, at
}

// glyphsOf appends the glyphs of l, laid out from (x, y) in color, but
// those its spans, sp if not nil, color, to gs.
func glyphsOf(l *text.Layout, x, y float32, color Color, sp *spanPaint, gs []morphGlyph) []morphGlyph {
	seen := -1
	for li := range l.Lines {
		line := &l.Lines[li]
		for _, g := range line.Glyphs {
			if g.Object {
				continue
			}
			c := color
			if sp != nil {
				c = sp.color(sp.at(g.Cluster), color)
			}
			mg := morphGlyph{font: g.Font, id: g.ID, fontSize: g.Size, size: g.Size, x: x + g.X, y: y + line.Baseline, color: c, alpha: 1}
			// The glyphs of a cluster are next to each other, in visual
			// order: clusters descend through right-to-left text.
			if g.Runes > 0 && g.Cluster != seen && g.Cluster >= 0 && g.Cluster < len(l.Runes) {
				mg.runes, seen = l.Runes[g.Cluster:min(g.Cluster+g.Runes, len(l.Runes))], g.Cluster
			}
			gs = append(gs, mg)
		}
	}
	return gs
}

// pair pairs the glyphs from and the clusters of cur: those of the
// longest sequence of the same characters in both keep their order, and
// the others take the nearest of the same characters left, as letters
// rearranged.
func (pr *morphPairs) pair(cur *text.Layout) {
	pr.cur = cur
	pr.curC, pr.curAt = clustersOf(cur, pr.curC[:0], pr.curAt)
	n, m := len(pr.from), len(pr.curC)
	pr.pairs = slices.Grow(pr.pairs[:0], m)[:m]
	for j := range pr.pairs {
		pr.pairs[j] = -1
	}
	pr.taken = slices.Grow(pr.taken[:0], n)[:n]
	clear(pr.taken)
	same := func(i, j int) bool {
		a, b := pr.from[i].runes, pr.curC[j]
		return a != nil && slices.Equal(a, cur.Runes[b.at:b.at+b.n])
	}
	if n*m > maxMorphPairs*16 {
		return // too long to pair: it fades
	}
	if n*m <= maxMorphPairs {
		// lcs[i*w+j] is the length of the longest common sequence of the
		// glyphs from i and the clusters of cur from j.
		w := m + 1
		pr.lcs = slices.Grow(pr.lcs[:0], (n+1)*w)[:(n+1)*w]
		clear(pr.lcs)
		for i := n - 1; i >= 0; i-- {
			for j := m - 1; j >= 0; j-- {
				if same(i, j) {
					pr.lcs[i*w+j] = pr.lcs[(i+1)*w+j+1] + 1
				} else {
					pr.lcs[i*w+j] = max(pr.lcs[(i+1)*w+j], pr.lcs[i*w+j+1])
				}
			}
		}
		for i, j := 0, 0; i < n && j < m; {
			switch {
			case same(i, j):
				pr.pairs[j], pr.taken[i] = int32(i), true
				i++
				j++
			case pr.lcs[(i+1)*w+j] >= pr.lcs[i*w+j+1]:
				i++
			default:
				j++
			}
		}
	}
	for j := range m {
		if pr.pairs[j] >= 0 {
			continue
		}
		best, dist := -1, 0
		for i := range n {
			if pr.taken[i] || !same(i, j) {
				continue
			}
			// As far from it in from as it is in cur, relatively.
			d := i*m - j*n
			if d < 0 {
				d = -d
			}
			if best < 0 || d < dist {
				best, dist = i, d
			}
		}
		if best >= 0 {
			pr.pairs[j], pr.taken[best] = int32(best), true
		}
	}
}

// clusterOf returns the cluster of glyph g in at, -1 for none.
func clusterOf(at []int32, g text.Glyph) int32 {
	if g.Runes == 0 || g.Cluster < 0 || g.Cluster >= len(at) {
		return -1
	}
	return at[g.Cluster]
}

// prepare pairs the glyphs of r's old text, or those it snapped, with its
// new text's, painted with the spans of sp, and notes where its box was as
// it changed in place, at (bx, by).
func (pr *morphPairs) prepare(r *contentRec, bx, by float32, sp *spanPaint) {
	old, cur := r.oldTL, r.tl
	if pr.boxOf != old {
		// Changed in place, in the frame painting this: the text of a
		// Match's element comes from the box matches.start noted.
		pr.boxOf, pr.boxX, pr.boxY = old, bx, by
	}
	if pr.old != old {
		// Changed anew: from its old text, laid out and painted with the
		// spans it painted last.
		pr.old, pr.snapped, pr.cur, pr.from = old, false, nil, nil
		pr.oldSpans = nil
		if len(pr.spans) > 0 {
			pr.oldSpans = newSpanPaint(pr.spans)
		}
		var spans []Span
		if sp != nil {
			spans = sp.spans
		}
		pr.noteSpans(spans)
	}
	if !pr.snapped && pr.from == nil || pr.cur != cur {
		if !pr.snapped {
			pr.from = glyphsOf(old, r.oldX, r.oldY, r.oldColor, pr.oldSpans, pr.from[:0])
		}
		pr.pair(cur)
	}
}

// noteSpans notes spans, those a text paints with.
func (pr *morphPairs) noteSpans(spans []Span) {
	if !slices.Equal(pr.spans, spans) {
		pr.spans = slices.Clone(spans)
	}
}

// restMorph notes, for the change to come, how the text of r paints at
// rest, with spans, forgetting what it paired.
func restMorph(r *contentRec, spans []Span) {
	pr, _ := r.morph.(*morphPairs)
	if pr == nil {
		if spans == nil {
			return
		}
		pr = &morphPairs{}
		r.morph = pr
	}
	if pr.old != nil || pr.boxOf != nil {
		*pr = morphPairs{spans: pr.spans}
	}
	pr.noteSpans(spans)
}

// each calls show with each glyph of r's text changing k of the way, its
// box at (bx, by) and its text from (x, y), in color, its box going on to
// (tx, ty) from there: the glyphs it comes from that the new text has
// none of, fading out, moving with the box; then the new text's, those it
// has from the others moving from them, going to their size, color and
// opacity, the others fading in. Each shows its pen and baseline at (gx,
// gy), size times as large, in c with the weight of shade, alpha opaque,
// where text at rest has it at (rx, ry); runes are those of its cluster,
// for the first glyph of one.
func (pr *morphPairs) each(r *contentRec, bx, by, x, y, tx, ty float32, color Color, sp *spanPaint, k float32, show func(g text.Glyph, runes []rune, rx, ry, gx, gy, size float32, c Color, shade text.Shade, alpha float32)) {
	mx, my := bx-pr.boxX, by-pr.boxY
	if fade := 1 - k; fade > 0 {
		for i, a := range pr.from {
			if !pr.taken[i] {
				g := text.Glyph{Font: a.font, ID: a.id, Size: a.fontSize}
				show(g, a.runes, a.x, a.y, a.x+mx, a.y+my, a.size/a.fontSize, a.color, text.ShadeOf(a.color.R, a.color.G, a.color.B), a.alpha*fade)
			}
		}
	}
	cur := r.tl
	for li := range cur.Lines {
		line := &cur.Lines[li]
		for _, g := range line.Glyphs {
			if g.Object {
				continue
			}
			color := color
			if sp != nil {
				color = sp.color(sp.at(g.Cluster), color)
			}
			shade := text.ShadeOf(color.R, color.G, color.B)
			gx, gy := x+g.X, y+line.Baseline
			ci := clusterOf(pr.curAt, g)
			var runes []rune
			if ci >= 0 && g.Cluster == pr.curC[ci].at && g.X == pr.curC[ci].x {
				b := pr.curC[ci]
				runes = cur.Runes[b.at : b.at+b.n]
			}
			if ci < 0 || pr.pairs[ci] < 0 {
				show(g, runes, gx+tx, gy+ty, gx, gy, 1, color, shade, k)
				continue
			}
			a, b := pr.from[pr.pairs[ci]], pr.curC[ci]
			dx, dy := a.x+mx-(x+b.x), a.y+my-(y+b.y)
			size := float32(1)
			if b.size > 0 {
				size = lerp(a.size/b.size, 1, k)
			}
			// The shade sets a glyph's weight: those that move change it
			// once, halfway, rather than at each step of their color.
			sh := shade
			if k < 0.5 {
				sh = text.ShadeOf(a.color.R, a.color.G, a.color.B)
			}
			show(g, runes, gx+tx, gy+ty, gx+dx*(1-k), gy+dy*(1-k), size, mixPremultiplied(a.color, color, k, pr.lab), sh, lerp(a.alpha, 1, k))
		}
	}
}

// morph paints the text of e changing, as r notes it, k of the way, from
// (x, y), in color (each): the old text moves with e's box, from where it
// was, and the new shows each glyph as text at rest where e's box goes
// would, moved from there: smoothly, together, and as text at rest once
// there.
func (p *Painter) morph(e *node, r *contentRec, x, y float32, ts textStyle, spans spanPaint, k float32) {
	var sp *spanPaint
	if spans.spans != nil {
		sp = &spans
	}
	pr, _ := r.morph.(*morphPairs)
	if pr == nil {
		pr = &morphPairs{}
		r.morph = pr
	}
	pr.prepare(r, e.x, e.y, sp)
	pr.lab = e.content.Animation != nil && e.content.Animation.Oklab
	color := ts.color
	// What its spans and its style paint besides glyphs, highlights under
	// them and lines over them, fades: the old text's out, going with the
	// box, the new's in.
	ox, oy := r.oldX+e.x-pr.boxX, r.oldY+e.y-pr.boxY
	saved := p.opacity
	decor := func(paint func(l *text.Layout, x, y float32, color Color, sp *spanPaint)) {
		if !pr.snapped {
			p.opacity = saved * (1 - k)
			paint(r.oldTL, ox, oy, r.oldColor, pr.oldSpans)
		}
		p.opacity = saved * k
		paint(r.tl, x, y, color, sp)
		p.opacity = saved
	}
	decor(func(l *text.Layout, x, y float32, _ Color, sp *spanPaint) { p.textBackgrounds(l, x, y, ts, sp) })
	defer decor(func(l *text.Layout, x, y float32, color Color, sp *spanPaint) { p.textLines(l, x, y, color, ts, sp) })
	sys := p.rt.text
	s := p.scale
	var tx, ty float32
	if pr.toFrame == p.rt.frame {
		tx, ty = pr.toX, pr.toY
	}
	// stroke, while the strokes of the glyphs paint under them all, is
	// the width of the text's (TextStroke), and shadow tells that the
	// shadows they cast paint under those (TextShadow).
	var stroke float32
	var shadow bool
	show := func(g text.Glyph, _ []rune, rx, ry, gx, gy, size float32, c Color, shade text.Shade, alpha float32) {
		if alpha <= 0.001 || size <= 0 {
			return
		}
		pen, base := rx*s, ry*s
		origin, baseline := sys.Origin(g.Font, s, pen, p.opaque), sys.Baseline(base)
		// Drawn near the size it shows, a quarter of an octave apart, so
		// that it stretches by 9% at most and the atlas holds few sizes of
		// it, from a pen on a whole pixel; at rest, as text at rest is.
		q, at, o := float32(1), pen, origin
		if size != 1 {
			q = float32(math.Exp2(math.Round(4*math.Log2(float64(size))) / 4))
		}
		if q != 1 {
			at = float32(math.Floor(float64(pen)))
			o = at
		}
		// At its size, as text at rest draws it: with the coverage of each
		// subpixel on an opaque background where the system asks for it,
		// which renderers make gray while it is transformed; at another,
		// gray.
		var gi text.GlyphImage
		switch {
		case shadow:
			gi, c = shadowGlyph(sys, g.Font, g.ID, s*q, at, shade, false, ts.shadow.blur/2*s*q), ts.shadow.color
		case stroke > 0:
			gi, c = strokeGlyph(sys, g.Font, g.ID, s*q, at, shade, p.opaque && q == 1, stroke*s*q), ts.strokeColor
		default:
			gi = sys.Glyph(g.Font, g.ID, s*q, at, shade, p.opaque && q == 1)
		}
		if !gi.OK {
			return
		}
		c = c.Alpha(p.opacity * alpha)
		sg := scene.Glyph{
			X: float32(math.Floor(float64(at))) + gi.Left, Y: baseline + gi.Top, W: float32(gi.W), H: float32(gi.H),
			U: gi.X, V: gi.Y, UW: gi.W, VH: gi.H,
			Color: c.scene(), Wide: p.glyphWide(c), Colored: gi.Colored, Subpixel: gi.Subpixel, Thin: gi.Thin,
		}
		// Its origin goes to its pen, as far from it as text at rest has
		// it, and it scales around it.
		ox, oy := gx*s+origin-pen, gy*s+baseline-base
		if shadow {
			ox, oy = ox+ts.shadow.x*s, oy+ts.shadow.y*s
		}
		if q != 1 || size != 1 || abs32(ox-o) > 1e-3 || abs32(oy-baseline) > 1e-3 {
			sg.Transform = scene.Translation(ox, oy).Mul(scene.Scaling(size/q, size/q)).Mul(scene.Translation(-o, -baseline))
		}
		p.s.Glyphs = append(p.s.Glyphs, sg)
	}
	if shadowGlyph != nil && ts.shadow.color.A > 0 {
		shadow = true
		start := int32(len(p.s.Glyphs))
		pr.each(r, e.x, e.y, x, y, tx, ty, color, sp, k, show)
		if end := int32(len(p.s.Glyphs)); end > start {
			p.add(scene.Op{Kind: scene.OpGlyphs, Start: start, End: end})
		}
		shadow = false
	}
	if strokeGlyph != nil && ts.strokeWidth > 0 && ts.strokeColor.A > 0 {
		stroke = ts.strokeWidth
		start := int32(len(p.s.Glyphs))
		pr.each(r, e.x, e.y, x, y, tx, ty, color, sp, k, show)
		if end := int32(len(p.s.Glyphs)); end > start {
			p.add(scene.Op{Kind: scene.OpGlyphs, Start: start, End: end})
		}
		stroke = 0
	}
	start := int32(len(p.s.Glyphs))
	pr.each(r, e.x, e.y, x, y, tx, ty, color, sp, k, show)
	if end := int32(len(p.s.Glyphs)); end > start {
		p.add(scene.Op{Kind: scene.OpGlyphs, Start: start, End: end})
	}
}

// textLines paints the lines over the text of l, laid out from (x, y), in
// color: those of its style, ts, and those of its spans, sp if not nil.
func (p *Painter) textLines(l *text.Layout, x, y float32, color Color, ts textStyle, sp *spanPaint) {
	deco := decoration{underline: ts.underline, wavy: ts.wavy, strike: ts.strike, solid: ts.solid, color: ts.decoColor, thick: ts.decoThick,
		offset: ts.decoOffset, hasOffset: ts.set&setDecoOffset != 0}
	if !deco.underline && !deco.strike && sp == nil {
		return
	}
	for li := range l.Lines {
		if sp != nil {
			sp.lines(p, l, li, x, y, color, deco)
		}
		if deco.underline || deco.strike {
			p.decorations(l, li, 0, len(l.Lines[li].Glyphs), x, y, deco, color)
		}
	}
}

// snapMorphFrom has dst, of a text taking the place of the element of
// src, morph from src's glyphs as they showed, morphing: its box at (bx,
// by) and its text from (x, y), in color.
func snapMorphFrom(src *contentRec, bx, by, x, y float32, color Color, dst *contentRec) {
	pr, _ := src.morph.(*morphPairs)
	dpr, _ := dst.morph.(*morphPairs)
	if pr == nil || dpr == nil || pr.cur != src.tl || src.oldTL == nil {
		return
	}
	k := max(0, min(float32(src.k.value), 1))
	var sp *spanPaint
	if len(pr.spans) > 0 {
		sp = newSpanPaint(pr.spans)
	}
	var from []morphGlyph
	pr.each(src, bx, by, x, y, 0, 0, color, sp, k, func(g text.Glyph, runes []rune, _, _, gx, gy, size float32, c Color, _ text.Shade, alpha float32) {
		if alpha > 0.001 {
			from = append(from, morphGlyph{runes: runes, font: g.Font, id: g.ID, fontSize: g.Size, size: g.Size * size, x: gx, y: gy, color: c, alpha: alpha})
		}
	})
	// In the order they show, along their lines, as layouts order glyphs.
	slices.SortStableFunc(from, func(a, b morphGlyph) int {
		if d := a.y - b.y; abs32(d) > min(a.size, b.size)/2 {
			return cmpFloat(d)
		}
		return cmpFloat(a.x - b.x)
	})
	dpr.from, dpr.snapped, dpr.old, dpr.cur = from, true, dst.oldTL, nil
}

// cmpFloat returns -1, 0 or 1 as d is below, at or above 0.
func cmpFloat(d float32) int {
	switch {
	case d < 0:
		return -1
	case d > 0:
		return 1
	}
	return 0
}
