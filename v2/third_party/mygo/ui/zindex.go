package ui

import (
	"cmp"
	"math"
	"slices"
)

// ZIndex orders the element's painting among its siblings, as CSS's
// z-index does: those with a higher value paint later, above those with a
// lower one, whatever their order in the tree, and take the pointer first
// where they overlap. Siblings with the same value paint as they would
// without one: those in the flow in the order they were built, then the
// absolute and sticky ones in theirs. The default is 0; a negative value
// paints the element under its siblings, still above its parent's
// background. The copy of an element leaving with its Exit
// (ElementTransition) keeps the z-index the element had, under its
// siblings of the same one: a dialog fading out stays above the page.
//
// It moves the element with all that is inside it, as one, among its
// siblings alone: it does not reach past its parent, which still clips it
// where it clips, nor mix the elements inside it with those of other
// parents, as if each element with a value other than 0 made a stacking
// context, as in CSS. Nothing else changes: the layout, the order Tab
// moves the focus in and what assistive technology reads follow the tree.
//
// A stack of overlapping avatars, the first on top:
//
//	ui.Row(c).Children(func() {
//		for i, u := range users {
//			a := ui.Avatar(c, u.Name, u.Photo).ZIndex(len(users) - i)
//			if i > 0 {
//				a.Margin(0, 0, 0, -8)
//			}
//		}
//	})
//
// A cell of a grid growing as the pointer is over it, above the cells
// after it:
//
//	cell := ui.Box(c).Size(80, 80)
//	if cell.Hovered() {
//		cell.Scale(1.2, 1.2).ZIndex(1)
//	}
//
// The window's content stays under its overlays (Overlay) whatever its
// z-index, and overlays keep the order they open in; an element inside a
// text (inline) paints with its text.
func (e *node) ZIndex(z int) *node {
	e.zIndex = int32(max(math.MinInt32, min(z, math.MaxInt32)))
	if z != 0 {
		zPaint, zCommit = (*Painter).paintByZ, (*engine).commitByZ
		if e.parent != nil {
			e.parent.zOrder = true
		}
	}
	return e
}

// zPaint and zCommit paint and commit the children of an element with
// zOrder: ZIndex sets them, so that apps that never call it do not link
// what orders children (paintByZ, commitByZ).
var (
	zPaint  func(p *Painter, e *node)
	zCommit func(rt *engine, e *node, clip Rect, invisible bool)
)

// zScratch holds what ordering children by their z-index works in, kept
// by the engine so that it allocates once: the children of the elements
// painting (paintByZ), and what the children committed (commitByZ), in
// segs, sorted through sorted, as pointers, which sort with the code of
// every other slice of pointers.
type zScratch struct {
	order  []*node
	segs   []zSeg
	sorted []*zSeg
	hits   []hit
	webs   []webPlace
}

// zSeg is what a child committed: rt.hits[hit0:hit1] and
// rt.webPlaces[web0:web1]; the rest is where it paints: rank 1, or 2 for a
// child pinned last (zPinned), then its z-index, phase 0 for a copy
// leaving, which paints first, 1 for a child in the flow, 2 for one
// painting above it (absolute or sticky), and index, its place among its
// siblings.
type zSeg struct {
	rank, phase uint8
	z           int32
	index       int
	hit0, hit1  int
	web0, web1  int
}

// zPinned returns the first child of e that, with those after it, stays
// above the rest whatever their z-index: of the root, the layer of
// overlays, with the inspector's panel after it.
func (e *node) zPinned() *node {
	if e.parent == nil {
		return e.c.overlay
	}
	return nil
}

// aboveFlow reports whether e paints above the flow of its parent: an
// absolute or a sticky element.
func (e *node) aboveFlow() bool { return e.flags&flagAbsolute != 0 || e.sticky }

// paintOrder appends the children of e, which has some with a z-index
// (zOrder), to buf in the order they paint: by z-index, and those with the
// same in the flow first, then the absolute and sticky ones, each in tree
// order, as without one. Those leaving (ghosts) go first, keeping their
// z-index, under the others of the same one, and the pinned ones
// (zPinned) stay last.
func (e *node) paintOrder(buf []*node) []*node {
	pinned := e.zPinned()
	phases := func(buf []*node, from, to *node) []*node {
		for c := from; c != to; c = c.next {
			if !c.aboveFlow() {
				buf = append(buf, c)
			}
		}
		for c := from; c != to; c = c.next {
			if c.aboveFlow() && c.leaving == 0 {
				buf = append(buf, c)
			}
		}
		return buf
	}
	base := len(buf)
	if e.ghosts {
		// Copies leaving, added last, are never pinned.
		for c := e.first; c != nil; c = c.next {
			if c.leaving != 0 {
				buf = append(buf, c)
			}
		}
	}
	buf = phases(buf, e.first, pinned)
	slices.SortStableFunc(buf[base:], func(a, b *node) int { return cmp.Compare(a.zIndex, b.zIndex) })
	if pinned != nil {
		buf = phases(buf, pinned, nil)
	}
	return buf
}

// paintByZ paints the children of e in their order by z-index
// (paintOrder), as Painter.element paints them in tree order otherwise:
// the dividers between those in the flow go over those under them and
// those of z-index 0 in the flow, and the elements flying over the
// window's content go under its overlays.
func (p *Painter) paintByZ(e *node) {
	z := &p.rt.z
	base := len(z.order)
	z.order = e.paintOrder(z.order)
	end := len(z.order)
	m := p.rt.matches
	content := m != nil && e.parent == nil
	dividers := e.flags&flagDividers != 0 && !e.grid
	for i := base; i < end; i++ {
		// Read again each time: painting c may grow the slice.
		c := z.order[i]
		if dividers && (c.zIndex > 0 || c.zIndex == 0 && c.aboveFlow() && c.leaving == 0) {
			p.dividers(e)
			dividers = false
		}
		if content && c == e.c.overlay {
			m.paint(p, nil)
			content = false
		}
		if p.paintable(c) {
			p.element(c)
		}
	}
	if dividers {
		p.dividers(e)
	}
	if content {
		m.paint(p, nil)
	}
	clear(z.order[base:end])
	z.order = z.order[:base]
}

// commitByZ commits the children of e as commitElement does, in the same
// order, which the focus follows, and then puts their hits, and the web
// views they hold, in the order they paint (paintOrder), each child's
// together: a child raised above its siblings takes the pointer first
// where they overlap.
func (rt *engine) commitByZ(e *node, clip Rect, invisible bool) {
	z := &rt.z
	base := len(z.segs)
	hit0, web0 := len(rt.hits), len(rt.webPlaces)
	pinned := e.zPinned()
	commit := func(ch *node, index int, pin bool) {
		k := len(z.segs)
		z.segs = append(z.segs, zSeg{rank: 1, z: ch.zIndex, phase: 1, index: index, hit0: len(rt.hits), web0: len(rt.webPlaces)})
		switch {
		case ch.leaving != 0:
			z.segs[k].phase = 0
		case pin:
			z.segs[k].rank, z.segs[k].z = 2, 0
		}
		if ch.aboveFlow() && ch.leaving == 0 {
			z.segs[k].phase = 2
		}
		rt.commitElement(ch, clip, invisible)
		// Committing ch used and gave back the segments after k.
		z.segs[k].hit1, z.segs[k].web1 = len(rt.hits), len(rt.webPlaces)
	}
	pin, i := false, 0
	for ch := e.first; ch != nil; ch, i = ch.next, i+1 {
		pin = pin || ch == pinned
		if ch.flags&flagAbsolute == 0 {
			commit(ch, i, pin)
		}
	}
	pin, i = false, 0
	for ch := e.first; ch != nil; ch, i = ch.next, i+1 {
		pin = pin || ch == pinned
		if ch.flags&flagAbsolute != 0 {
			commit(ch, i, pin)
		}
	}
	segs := z.segs[base:]
	sorted := z.sorted[:0]
	for i := range segs {
		sorted = append(sorted, &segs[i])
	}
	slices.SortStableFunc(sorted, func(a, b *zSeg) int {
		return cmp.Or(cmp.Compare(a.rank, b.rank), cmp.Compare(a.z, b.z), cmp.Compare(a.phase, b.phase), cmp.Compare(a.index, b.index))
	})
	z.hits = append(z.hits[:0], rt.hits[hit0:]...)
	z.webs = append(z.webs[:0], rt.webPlaces[web0:]...)
	hits, webs := rt.hits[:hit0], rt.webPlaces[:web0]
	for _, s := range sorted {
		moved := len(hits) - s.hit0
		hits = append(hits, z.hits[s.hit0-hit0:s.hit1-hit0]...)
		for _, wp := range z.webs[s.web0-web0 : s.web1-web0] {
			wp.hit += moved
			webs = append(webs, wp)
		}
	}
	rt.hits, rt.webPlaces = hits, webs
	clear(z.hits)
	clear(z.webs)
	clear(sorted)
	z.sorted = sorted[:0]
	z.segs = z.segs[:base]
}
