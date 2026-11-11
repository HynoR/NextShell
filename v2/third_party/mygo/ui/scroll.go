package ui

// ScrollState is how far a scroll container scrolls its content, kept in
// the app's state with TrackScroll.
type ScrollState struct {
	Handle

	// X and Y are how far the content is scrolled left and up, in DIPs:
	// right to left (Direction), X is how far it is scrolled right, from
	// its start at the right. Set them to scroll: the container keeps them
	// within its content, so that 0 shows the start and math.MaxFloat32
	// the end.
	X, Y float32
	// MaxX and MaxY are as far as X and Y go, as the frame laid the content
	// out: Y is MaxY at the end. The container sets them.
	MaxX, MaxY float32

	glide *scrollGlide
}

// scrollGlide is a ScrollTo on its way: to x and y, as a says.
type scrollGlide struct {
	x, y   channel
	to     [2]float32
	a      Animation
	moved  bool // the target changed since the container last took it
	frame  uint64
	placed bool
}

// ScrollTo scrolls the container smoothly to x and y as a says, from where
// it shows its content, as SwiftUI's scrollTo in withAnimation:
// math.MaxFloat32 is the end. Called again on the way, it turns smoothly
// toward the new place; the user scrolling stops it. While the desktop
// asks for less motion, or the frame disables animations, it scrolls at
// once.
//
//	if ui.Button(c, "Back to top").Clicked() {
//		app.feed.ScrollTo(0, 0, ui.Smooth)
//	}
func (s *ScrollState) ScrollTo(x, y float32, a Animation) {
	finiteMotion("scroll", float64(x), float64(y))
	if a.Duration < 0 || a.Delay < 0 {
		panic("ui: animation durations must not be negative")
	}
	if s.glide == nil {
		s.glide = &scrollGlide{}
	}
	s.glide.to, s.glide.a, s.glide.moved = [2]float32{x, y}, a, true
}

// Scrolling reports whether a ScrollTo is on its way.
func (s *ScrollState) Scrolling() bool { return s.glide != nil }

// TrackScroll keeps the scrolling of a scroll container (Scroll,
// ScrollHorizontal, ScrollBoth, List) in *s: the container shows its
// content from s.X and s.Y, and writes there as the user scrolls it. The
// app reads where it is, scrolls it by setting them, and keeps a place
// with a ScrollState of its own for each page the container shows.
//
//	// A log that follows its end, unless the user scrolled up from it.
//	if app.log.Y >= app.log.MaxY {
//		app.log.Y = math.MaxFloat32
//	}
//	ui.Scroll(c).TrackScroll(&app.log).Children(app.lines)
//
// A List keeps its place by its rows instead, and scrolls to a row with
// its ListState.
func (e *node) TrackScroll(s *ScrollState) *node {
	if s != nil {
		wrapElement(e).Bind(&s.Handle)
	}
	e.track = s
	if e.adoptScroll() && e.first != nil {
		// What is built was built for the old place, as List's rows.
		e.c.rt.consumed = true
	}
	return e
}

// ScrollIntoView scrolls the containers around the element as little as
// shows it, once the frame is laid out: an element added in this frame
// comes into view with it. Call it in the frame it should come into view,
// as when it is added or chosen: in every frame, it would keep the user
// from scrolling it away.
//
// Its options line the element up elsewhere in the containers, or glide
// there, as the web's scrollIntoView options do: the current match of a
// search, centered smoothly.
//
//	if app.matchMoved {
//		hit.ScrollIntoView(ui.ScrollIntoViewOptions{Block: ui.ScrollCenter, Animation: &ui.Smooth})
//	}
func (e *node) ScrollIntoView(options ...ScrollIntoViewOptions) *node {
	if len(options) > 1 {
		panic("ui: ScrollIntoView takes at most one ScrollIntoViewOptions")
	}
	var o ScrollIntoViewOptions
	if len(options) > 0 {
		o = options[0]
		if o.Block > ScrollEnd || o.Inline > ScrollEnd {
			panic("ui: ScrollIntoView alignments are ScrollNearest, ScrollStart, ScrollCenter or ScrollEnd")
		}
		if a := o.Animation; a != nil && (a.Duration < 0 || a.Delay < 0) {
			panic("ui: animation durations must not be negative")
		}
	}
	if !e.hasState() {
		return e
	}
	e.c.reveal = append(e.c.reveal, nodeReveal{e, o})
	return e
}

// ScrollIntoViewOptions say where ScrollIntoView brings an element in the
// containers around it, and how, as the web's ScrollIntoViewOptions do.
// The zero options scroll as little as shows it, at once, as
// ScrollIntoView does without them.
type ScrollIntoViewOptions struct {
	// Block and Inline line the element up in each container around it,
	// vertically and sideways, as the web's block and inline do:
	// ScrollNearest scrolls as little as shows it, and ScrollStart,
	// ScrollCenter and ScrollEnd line its start, center or end up with
	// the container's, inside its border. Right to left, the inline start
	// is at the right.
	Block, Inline ScrollAlign
	// Animation, when set, glides the containers there as it says (Smooth,
	// a spring, or a curve), as the web's behavior: "smooth" does, rather
	// than at once. The user scrolling, or the app, stops it. While the
	// desktop asks for less motion, or the frame disables animations, it
	// scrolls at once.
	Animation *Animation
}

// ScrollAlign is where ScrollIntoView lines an element up in a container
// on an axis, as the web's ScrollLogicalPosition.
type ScrollAlign uint8

const (
	// ScrollNearest scrolls as little as shows the element: not at all
	// when it shows, and to the nearer edge otherwise. What is larger than
	// the view shows from its start.
	ScrollNearest ScrollAlign = iota
	// ScrollStart lines the element's start up with the container's: its
	// top, or its left (its right, right to left).
	ScrollStart
	// ScrollCenter puts the element in the middle of the container.
	ScrollCenter
	// ScrollEnd lines the element's end up with the container's.
	ScrollEnd
)

// nodeReveal is an element asked to come into view, as o says.
type nodeReveal struct {
	e *node
	o ScrollIntoViewOptions
}

// revealAsk is the element id asked to come into view, as o says.
type revealAsk struct {
	id uint64
	o  ScrollIntoViewOptions
}

// adoptScroll takes the offset the app set in the element's ScrollState,
// and reports whether it was a new one.
func (e *node) adoptScroll() bool {
	st, t := e.st, e.track
	st.track = t
	if t != nil && t.glide != nil {
		e.glide(t)
	}
	if t == nil || t.X == float32(st.scrollX) && t.Y == float32(st.scrollY) {
		return false
	}
	st.beginMove(e.c.rt.frame)
	st.wheel = nil // the app takes over from the wheel
	st.haltSnap()  // and from snapping
	if t.X != float32(st.scrollX) {
		st.scrollX = float64(t.X)
	}
	if t.Y != float32(st.scrollY) {
		st.scrollY = float64(t.Y)
	}
	return true
}

// glide moves the offset of a ScrollTo on its way to the frame's time.
func (e *node) glide(t *ScrollState) {
	g, st, rt, now := t.glide, e.st, e.c.rt, e.c.now
	if !g.placed || g.frame+1 < rt.frame {
		// Starting, or the container was not shown: from where it shows.
		g.x = channel{value: st.scrollX, target: st.scrollX, at: now}
		g.y = channel{value: st.scrollY, target: st.scrollY, at: now}
		g.placed, g.moved = true, true
	}
	g.frame = rt.frame
	if g.moved {
		g.moved = false
		to := [2]float64{max(0, min(float64(g.to[0]), float64(t.MaxX))), max(0, min(float64(g.to[1]), float64(t.MaxY)))}
		if rt.preferences().ReduceMotion || rt.instant() {
			g.x = channel{value: to[0], target: to[0], at: now}
			g.y = channel{value: to[1], target: to[1], at: now}
		} else {
			headFor(&g.x, to[0], g.a, now)
			headFor(&g.y, to[1], g.a, now)
		}
	}
	g.x.advance(now)
	g.y.advance(now)
	t.X, t.Y = float32(g.x.value), float32(g.y.value)
	if g.x.moving || g.x.pending || g.y.moving || g.y.pending {
		rt.animating = true
	} else {
		t.glide = nil
	}
}

// maxScroll returns as far as the element scrolls its content, on the axes
// it scrolls.
func (e *node) maxScroll() (x, y float64) {
	if e.flags&flagScrollX != 0 {
		x = max(0, e.contentW-float64(e.w))
	}
	if e.flags&flagScrollY != 0 {
		y = max(0, e.contentH-float64(e.h))
	}
	return x, y
}

// scrollLeft returns how far the scroll container moves its content to
// the left: its horizontal offset, which, right to left, counts from the
// right, as Flutter's do (CSS's go negative instead).
func (e *node) scrollLeft() float64 {
	if e.flags&flagScrollX != 0 && e.rtl() {
		mx, _ := e.maxScroll()
		return mx - e.st.scrollX
	}
	return e.st.scrollX
}

// followShift returns how far what follows the scroll container sideways
// (ScrollWith) moves to the left: right to left, both are laid out from
// the right, which the offset counts from.
func (e *node) followShift() float64 {
	if e.rtl() {
		return -e.st.scrollX
	}
	return e.st.scrollX
}

// scrollTo moves the content of a scroll container, and the ScrollState
// tracking it.
func (s *state) scrollTo(x, y float64) {
	s.scrollX, s.scrollY = x, y
	if t := s.track; t != nil {
		t.X, t.Y = float32(x), float32(y)
	}
}

// beginMove notes where the offset was before the frame first moves it,
// other than an event does.
func (s *state) beginMove(frame uint64) {
	if s.moveFrame != frame {
		s.moveFrame, s.startX, s.startY = frame, s.scrollX, s.scrollY
	}
}

// movedIn reports whether frame moved the offset: what was built read the
// old one, and the next frame builds with the new.
func (s *state) movedIn(frame uint64) bool {
	return s.moveFrame == frame && (s.scrollX != s.startX || s.scrollY != s.startY)
}

// reveal scrolls element id into view in the next frame, once it is laid
// out.
func (rt *engine) reveal(id uint64) {
	rt.askReveal(id, ScrollIntoViewOptions{})
	rt.requestFrame()
}

// askReveal adds element id to those to scroll into view once the frame
// is laid out, as o says: an element asked again keeps its place among
// them, and takes the options asked last, but for the zero ones.
func (rt *engine) askReveal(id uint64, o ScrollIntoViewOptions) {
	i := revealIndex(rt.revealIDs, id)
	switch {
	case i < 0:
		rt.revealIDs = append(rt.revealIDs, revealAsk{id, o})
	case o != ScrollIntoViewOptions{}:
		rt.revealIDs[i].o = o
	}
}

// revealIndex returns the index of element id among asks, -1 when it is
// not.
func revealIndex(asks []revealAsk, id uint64) int {
	for i, r := range asks {
		if r.id == id {
			return i
		}
	}
	return -1
}

// revealAll scrolls the elements of root asked for into view, in that
// order.
func revealAll(root *node, asks []revealAsk) {
	found := make([]*node, len(asks))
	var walk func(e *node)
	walk = func(e *node) {
		if e.hidden {
			return // it has no box to show
		}
		if i := revealIndex(asks, e.id); i >= 0 && found[i] == nil {
			found[i] = e
		}
		for ch := e.first; ch != nil; ch = ch.next {
			walk(ch)
		}
	}
	walk(root)
	for i, e := range found {
		if e != nil {
			reveal(e, asks[i].o)
		}
	}
}

// reveal scrolls the containers around e, innermost first, to show its
// box as o says. The layout has sized and arranged the boxes, relative to
// their parents, but not placed them yet.
func reveal(e *node, o ScrollIntoViewOptions) {
	for e.isInline() {
		e = e.parent
	}
	x, y, w, h := e.x, e.y, e.w, e.h
	if r, ok := clientCaret(e); ok {
		// An app's text field shows its caret: the text it edits may be
		// taller than the view, as a code editor as high as its lines.
		x, y, w, h = x+r.X, y+r.Y, max(r.W, 1), r.H
	}
	rt := e.c.rt
	frame := rt.frame
	smooth := o.Animation != nil && !rt.preferences().ReduceMotion && !rt.instant()
	for ch, p := e, e.parent; p != nil; ch, p = p, p.parent {
		if ch.stuck() {
			// Where it sticks, which may be in view already.
			fx, fy := flowOrigin(ch)
			dx, dy := ch.stickyShift(fx, fy, laidOutOrigin)
			x += dx
			y += dy
		}
		if p.scrolls() && ch.flags&flagAbsolute == 0 {
			p.adoptScroll()
			st := p.st
			mx, my := p.maxScroll()
			// Where the box is in the content: a List placed its rows
			// from scrollBase.
			cx, cy := float64(x), float64(y)+p.scrollBase
			rtl := p.flags&flagScrollX != 0 && p.rtl()
			left := st.scrollX
			if rtl {
				left = mx - left
			}
			inline := o.Inline
			if p.rtl() {
				// The start is at the right.
				inline = inline.flip()
			}
			lx := max(0, min(inline.offset(left, cx, w, p.border[3], p.w-p.border[1]), mx))
			sx := lx
			if rtl {
				// Right to left, offsets count from the right.
				sx = mx - sx
			}
			sy := max(0, min(o.Block.offset(st.scrollY, cy, h, p.border[0], p.h-p.border[2]), my))
			if smooth {
				// The containers around go to where it will be.
				rt.glideScroll(st, sx, sy, *o.Animation, e.c.now)
				x, y = float32(cx-lx)+p.x, float32(cy-sy)+p.y
				continue
			}
			if sx != st.scrollX || sy != st.scrollY {
				st.beginMove(frame)
				st.wheel = nil // what comes into view takes over from the wheel
				if st.snap != nil {
					// It lines up the place of what comes into view.
					st.snap.revealed(e)
				}
				if p.list != nil {
					// A List places its rows anew from there, building
					// those it lacked, which moves the row holding the box.
					was := ch.y
					p.relayoutList(ch, sy)
					cy = float64(y+ch.y-was) + p.scrollBase
				} else {
					st.scrollTo(sx, sy)
				}
			}
			x = float32(cx - p.scrollLeft())
			y = float32(cy - st.scrollY)
		}
		x += p.x
		y += p.y
	}
}

// clientCaret returns the box of the caret of an element with an app's
// TextInputClient, relative to the element, as the client places it.
// TextInput and TextArea reveal their whole box, and scroll their caret
// into view themselves.
func clientCaret(e *node) (Rect, bool) {
	if e.textClient == nil || e.st == nil || e.st.editor != nil {
		return Rect{}, false
	}
	sel := e.textClient.Selection()
	at := sel.Range.End
	if sel.Reversed {
		at = sel.Range.Start
	}
	r, _, ok := e.textClient.BoundsForRange(TextInputRange{Start: at, End: at})
	return r, ok && r.H > 0
}

// offset returns the offset that lines what spans [pos, pos+size) of a
// container's content up in its viewport [lo, hi), scrolled by off, as a
// says.
func (a ScrollAlign) offset(off, pos float64, size, lo, hi float32) float64 {
	switch a {
	case ScrollStart:
		return pos - float64(lo)
	case ScrollCenter:
		return pos + float64(size)/2 - (float64(lo)+float64(hi))/2
	case ScrollEnd:
		return pos + float64(size) - float64(hi)
	}
	return nearest(off, pos, size, lo, hi)
}

// flip returns the alignment from the other side: Start for End.
func (a ScrollAlign) flip() ScrollAlign {
	switch a {
	case ScrollStart:
		return ScrollEnd
	case ScrollEnd:
		return ScrollStart
	}
	return a
}

// nearest returns the offset that shows what spans [pos, pos+size) of a
// container's content in its viewport [lo, hi), scrolled by off, moving as
// little as it can: what is larger than the viewport shows from its start.
func nearest(off, pos float64, size, lo, hi float32) float64 {
	start, end := pos-off, pos+float64(size)-off
	switch {
	case start < float64(lo):
		return off - (float64(lo) - start)
	case end > float64(hi):
		return off + min(end-float64(hi), start-float64(lo))
	}
	return off
}

// RowWidth makes the rows of a List at least w DIPs wide, as the columns
// of a table ask: where they are wider than the list, it scrolls sideways
// too, and its rows grow to its width where they are not. A header above
// the rows goes with them through ScrollWith:
//
//	head := ui.Row(c).Clip().Padding(0, 8).Children(app.columnTitles)
//	list := ui.List(c, &app.rows, n).Padding(0, 8).RowWidth(app.columnsWidth())
//	list.Rows(app.row)
//	head.ScrollWith(list)
func (e *node) RowWidth(w float32) *node {
	e.rowMinW = max(w, 0)
	if e.rowMinW > 0 {
		e.flags |= flagScrollX
	} else {
		e.flags &^= flagScrollX
	}
	return e
}

// ScrollWith moves the element's content sideways as far as scroller, a
// scroll container, scrolls its own, as a table's header goes with its
// rows. Clip the element, so that what goes out of it does not show.
func (e *node) ScrollWith(scroller *node) *node {
	e.followX = scroller
	return e
}
