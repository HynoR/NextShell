package ui

import (
	"math"
	"slices"
	"time"
)

// Snap is how a scroll container comes to rest at the places the elements
// in it mark (ScrollSnap), as CSS's scroll-snap-type.
type Snap uint8

const (
	// SnapNone, the zero value, rests wherever the scrolling stops.
	SnapNone Snap = iota
	// SnapMandatory always rests at a place, as CSS's mandatory: a strip
	// of cards rests showing a card from its start, and a notch of a
	// wheel or an arrow key goes on to the next card.
	SnapMandatory
	// SnapProximity rests at a place only where the scrolling stops
	// near it, within a third of the view, as CSS's proximity, and
	// wherever it stops in between.
	SnapProximity
)

// snapSettle is how long a touchpad, or a finger on a touchscreen, leaves
// a container that snaps before its scrolling has settled: the momentum
// of a touchpad's fling comes on at once, and the platforms do not all
// tell when the fingers lift.
const snapSettle = 150 * time.Millisecond

// snapAnimation is how a container that snaps glides to rest: a spring,
// so that a notch of a wheel on the way turns it smoothly toward the next
// place.
var snapAnimation = Animation{Spring: SpringOptions{Duration: 400 * time.Millisecond}}

// placeSnapper finds the places a container that snaps rests at, as it is
// placed (placeSnaps). ScrollSnap sets it, so that apps that do not snap
// leave out what snapping takes.
var placeSnapper func(e *node)

// ScrollSnap makes the scroll container (Scroll, ScrollHorizontal,
// ScrollBoth, List) come to rest at the places the elements in it mark
// with SnapAlign, as CSS's scroll-snap-type does, on the axes it scrolls:
//
//   - a touchpad, its momentum after a fling, and a finger on a
//     touchscreen glide on to the nearest once they have settled, and so
//     does a thumb of a scroll bar once it is let go;
//   - a notch of a wheel, the arrow keys, Page Up and Page Down, Home and
//     End and a press on a scroll bar's track go to the next place that
//     way, past where it was, as far as they scroll.
//
// A place lines an element up with the container's content box, inside
// its padding, so that the first of a strip of cards rests where it
// starts. An element larger than the view rests anywhere it fills the
// view. Resting at an element, the container stays at it as the layout
// moves it, as when the window resizes, and an element of one coming into
// view (ScrollIntoView, the focus) lines it up. Where the app scrolls the
// container, with its ScrollState, ScrollTo or a List's ListState, it
// stays. While the desktop asks for less motion, it rests at once.
//
//	ui.ScrollHorizontal(c).ScrollSnap(ui.SnapMandatory).Padding(16).Gap(12).Children(func() {
//		for _, card := range app.cards {
//			cardView(c, card).SnapAlign(ui.Start)
//		}
//	})
func (e *node) ScrollSnap(s Snap) *node {
	if s > SnapProximity {
		panic("ui: ScrollSnap takes SnapNone, SnapMandatory or SnapProximity")
	}
	e.snap = s
	if s != SnapNone && placeSnapper == nil {
		placeSnapper = placeSnaps
	}
	return e
}

// SnapAlign marks a place where the nearest scroll container around the
// element rests, if it snaps (ScrollSnap): where the element's start,
// center or end, as a is Start, Center or End, lines up with the
// container's, on both axes, as CSS's scroll-snap-align. Start is the left
// of what scrolls sideways, its right right to left (Direction), and the
// top of what scrolls vertically.
func (e *node) SnapAlign(a Align) *node {
	v := snapCode(a)
	e.snapAlign = v | v<<4
	return e
}

// SnapAlignX marks a place as SnapAlign does, sideways only: a container
// that scrolls both ways rests anywhere vertically.
func (e *node) SnapAlignX(a Align) *node {
	e.snapAlign = e.snapAlign&0xf0 | snapCode(a)
	return e
}

// SnapAlignY marks a place as SnapAlign does, vertically only.
func (e *node) SnapAlignY(a Align) *node {
	e.snapAlign = e.snapAlign&0x0f | snapCode(a)<<4
	return e
}

func snapCode(a Align) uint8 {
	if a != Start && a != Center && a != End {
		panic("ui: snap alignments are Start, Center or End")
	}
	return uint8(a) + 1
}

// snapper is where a container that snaps comes to rest. Only placeSnaps
// makes one, so that what snapping takes is left out of apps that do not
// snap.
type snapper interface {
	// notch scrolls the container by dx, dy as a notch of a wheel or a
	// key does, as scrollBy takes them, to the next place that way: took
	// tells that it snaps on an axis they scroll, and moved that it goes
	// somewhere.
	notch(rt *engine, s *state, dx, dy float32) (moved, took bool)
	// scrolled tells that the user scrolled the container freely, as a
	// touchpad does: it rests at the nearest place once wait passed with
	// no more.
	scrolled(rt *engine, s *state, wait time.Duration)
	// revealed tells that the layout scrolled e into view (reveal).
	revealed(e *node)
	// halt stops it where it is: the user or the app took over.
	halt()
	// frame moves it to the frame's time, and reports whether it goes on.
	frame(rt *engine, s *state, now time.Time) bool
}

// haltSnap stops a container that snaps where it is.
func (s *state) haltSnap() {
	if s.snap != nil {
		s.snap.halt()
	}
}

// snapNotch scrolls container s by dx, dy to the next place, if it snaps
// on an axis they scroll (took), and reports whether it moved.
func (rt *engine) snapNotch(s *state, dx, dy float32) (moved, took bool) {
	if s.snap == nil {
		return false, false
	}
	moved, took = s.snap.notch(rt, s, dx, dy)
	if moved {
		rt.snapFrame(s.id)
	}
	return moved, took
}

// snapFrame has the frames move container id to rest (glideSnaps).
func (rt *engine) snapFrame(id uint64) {
	if !slices.Contains(rt.snapping, id) {
		rt.snapping = append(rt.snapping, id)
	}
	rt.requestFrame()
}

// glideSnaps moves the containers that snap on their way to rest to the
// frame's time.
func (rt *engine) glideSnaps(now time.Time) {
	kept := rt.snapping[:0]
	for _, id := range rt.snapping {
		if s := rt.states[id]; s != nil && s.snap != nil && s.snap.frame(rt, s, now) {
			kept = append(kept, id)
		}
	}
	rt.snapping = kept
}

// snapState is where a container that snaps rests: the places on each
// axis, from its last layout, and how it goes to one.
type snapState struct {
	mandatory bool
	// x and y are the places, as offsets of the container (scrollX,
	// scrollY), and view the size of its content box.
	x, y []snapArea
	view [2]float64
	// gx and gy glide the offset to rest, which done is how far they
	// moved so far: each frame moves it by what they went since, as a
	// wheel's notches do (wheelGlide), so that a List moving its offset
	// as it measures rows does not throw it off.
	gx, gy       channel
	doneX, doneY float64
	gliding      bool
	// settling waits for the user's scrolling to settle, until settleAt.
	settling bool
	settleAt time.Time
	// to are the elements whose places it glides to, and rest those it
	// rests at, horizontally and vertically, 0 for none.
	to, rest [2]uint64
	// reveal is the element whose place it lines up as it is placed, as
	// one in it scrolled into view.
	reveal uint64
}

// snapArea is the place of element id on an axis: the offsets from lo to
// hi, which are the same but for an element larger than the view.
type snapArea struct {
	lo, hi float64
	id     uint64
}

// placeSnaps finds the places container e rests at, before it places its
// content, and keeps it at the one it rests at as the layout moves it.
func placeSnaps(e *node) {
	s := e.st
	if e.snap == SnapNone {
		s.snap = nil
		return
	}
	sn, _ := s.snap.(*snapState)
	fresh := sn == nil
	if fresh {
		sn = &snapState{}
		s.snap = sn
	}
	sn.mandatory = e.snap == SnapMandatory
	sn.x, sn.y = sn.x[:0], sn.y[:0]
	sn.view = [2]float64{float64(max(e.w-e.padX(), 0)), float64(max(e.h-e.padY(), 0))}
	mx, my := e.maxScroll()
	for ch := e.first; ch != nil; ch = ch.next {
		// What is absolute in the container does not scroll.
		if ch.flags&flagAbsolute == 0 {
			sn.collect(e, ch, float64(ch.x), float64(ch.y)+e.scrollBase, mx, my)
		}
	}
	rt := e.c.rt
	x, y := s.scrollX, s.scrollY
	if id := sn.reveal; id != 0 {
		sn.reveal = 0
		sn.halt()
		x, sn.rest[0] = sn.keep(sn.x, x, placeOf(sn.x, id), false)
		y, sn.rest[1] = sn.keep(sn.y, y, placeOf(sn.y, id), false)
	} else {
		if sn.gliding || sn.settling || rt.scrollDrag.st == s {
			return
		}
		x, sn.rest[0] = sn.keep(sn.x, x, sn.rest[0], fresh && sn.mandatory)
		y, sn.rest[1] = sn.keep(sn.y, y, sn.rest[1], fresh && sn.mandatory)
	}
	if x != s.scrollX || y != s.scrollY {
		s.beginMove(rt.frame)
		s.scrollTo(x, y)
	}
}

// collect adds the places that d, at x, y in the content of container e,
// and the elements inside it mark, those of e's offsets as far as mx, my.
func (sn *snapState) collect(e, d *node, x, y, mx, my float64) {
	if d.hidden || d.leaving != 0 {
		return
	}
	if a := d.snapAlign; a != 0 {
		if ax := a & 0xf; ax != 0 && e.flags&flagScrollX != 0 {
			start := float64(e.contentX())
			lo, hi := x-start, x+float64(d.w)-start-sn.view[0]
			if e.rtl() {
				// The offset counts from the right, as does Start.
				lo, hi = mx-hi, mx-lo
			}
			sn.x = append(sn.x, snapAt(lo, hi, ax, mx, d.id))
		}
		if ay := a >> 4; ay != 0 && e.flags&flagScrollY != 0 {
			start := float64(e.contentY())
			sn.y = append(sn.y, snapAt(y-start, y+float64(d.h)-start-sn.view[1], ay, my, d.id))
		}
	}
	if d.scrolls() || d.kind == kindText {
		// What scrolls in it rests in it.
		return
	}
	for ch := d.first; ch != nil; ch = ch.next {
		sn.collect(e, ch, x+float64(ch.x), y+float64(ch.y), mx, my)
	}
}

// snapAt returns the place of element id, whose start lines up with the
// view's at offset lo and its end at hi, aligned as a (Start, Center or
// End plus one), within the offsets to mx.
func snapAt(lo, hi float64, a uint8, mx float64, id uint64) snapArea {
	if hi < lo {
		// Smaller than the view: one place.
		p := (lo + hi) / 2
		switch Align(a - 1) {
		case Start:
			p = lo
		case End:
			p = hi
		}
		lo, hi = p, p
	}
	return snapArea{lo: max(0, min(lo, mx)), hi: max(0, min(hi, mx)), id: id}
}

// keep returns where on an axis with places areas a container at offset
// at rests, at element id: within its place, or, where id is gone or 0,
// at the place it is at. Where it is at none, a container that must rest
// at one (sn.mandatory) goes to the nearest when it rested at an element
// gone, or when nearest asks.
func (sn *snapState) keep(areas []snapArea, at float64, id uint64, nearest bool) (float64, uint64) {
	if id != 0 {
		for _, a := range areas {
			if a.id == id {
				return max(a.lo, min(at, a.hi)), id
			}
		}
	}
	p, near, ok := nearestSnap(areas, at, at, false)
	switch {
	case ok && math.Abs(p-at) < snapEps:
		// At a place already, as where the app scrolled to one.
		return at, near
	case ok && sn.mandatory && (id != 0 || nearest):
		return p, near
	}
	return at, 0
}

// placeOf returns id where areas has its place, 0 otherwise.
func placeOf(areas []snapArea, id uint64) uint64 {
	if slices.ContainsFunc(areas, func(a snapArea) bool { return a.id == id }) {
		return id
	}
	return 0
}

// snapEps is how near offsets are to be the same place.
const snapEps = 0.5

// nearestSnap returns the place nearest offset at among areas, and the
// element whose it is; toward, past from on the way to at only.
func nearestSnap(areas []snapArea, from, at float64, toward bool) (p float64, id uint64, ok bool) {
	dir := 0.0
	if toward {
		dir = math.Copysign(1, at-from)
	}
	best := math.Inf(1)
	for _, a := range areas {
		q := max(a.lo, min(at, a.hi))
		if dir != 0 && (q-from)*dir <= snapEps {
			continue
		}
		if d := math.Abs(q - at); d < best {
			best, p, id, ok = d, q, a.id, true
		}
	}
	return p, id, ok
}

// land returns where on an axis with places areas a container scrolled
// from from to at rests, and the element whose place it is, 0 for none:
// was where nothing is past from toward at, of a container that must rest
// at a place.
func (sn *snapState) land(areas []snapArea, from, at, view float64, toward bool, was uint64) (float64, uint64) {
	if len(areas) == 0 {
		return at, 0
	}
	p, id, ok := nearestSnap(areas, from, at, toward)
	switch {
	case !ok && sn.mandatory:
		return from, was
	case !ok, !sn.mandatory && math.Abs(p-at) > view/3:
		return at, 0
	}
	return p, id
}

// goal returns the offset the container is on its way to.
func (sn *snapState) goal(s *state) (float64, float64) {
	if !sn.gliding {
		return s.scrollX, s.scrollY
	}
	return s.scrollX + sn.gx.target - sn.doneX, s.scrollY + sn.gy.target - sn.doneY
}

// maxOffsets returns as far as container s scrolls, on the axes it does.
func (s *state) maxOffsets() (x, y float64) {
	if s.flags&flagScrollX != 0 {
		x = max(0, s.contentW-float64(s.w))
	}
	if s.flags&flagScrollY != 0 {
		y = max(0, s.contentH-float64(s.h))
	}
	return x, y
}

func (sn *snapState) notch(rt *engine, s *state, dx, dy float32) (moved, took bool) {
	if s.rtl {
		dx = -dx
	}
	mx, my := s.maxOffsets()
	ox, oy := float64(dx), float64(dy)
	if s.flags&flagScrollX == 0 {
		ox = 0
	}
	if s.flags&flagScrollY == 0 {
		oy = 0
	}
	if !(ox != 0 && len(sn.x) > 0 || oy != 0 && len(sn.y) > 0) {
		return false, false
	}
	fx, fy := sn.goal(s)
	ids := sn.rest
	if sn.gliding {
		ids = sn.to
	}
	tx, ty := fx, fy
	if at := max(0, min(fx+ox, mx)); ox != 0 && at != fx {
		tx, ids[0] = sn.land(sn.x, fx, at, sn.view[0], true, ids[0])
	}
	if at := max(0, min(fy+oy, my)); oy != 0 && at != fy {
		ty, ids[1] = sn.land(sn.y, fy, at, sn.view[1], true, ids[1])
	}
	if math.Abs(tx-fx) < snapEps && math.Abs(ty-fy) < snapEps {
		return false, true
	}
	sn.glideTo(rt, s, tx, ty, ids, rt.now())
	return true, true
}

func (sn *snapState) scrolled(rt *engine, s *state, wait time.Duration) {
	sn.halt()
	sn.settling, sn.settleAt = true, rt.now().Add(wait)
	rt.snapFrame(s.id)
}

func (sn *snapState) revealed(e *node) {
	// The innermost element around e that marks a place of the container
	// being revealed: those inside another scroll container are its own.
	var id uint64
	for n := e; n != nil && n.st != nil && n.st.snap != sn; n = n.parent {
		if n.scrolls() {
			id = 0
		}
		if id == 0 && n.snapAlign != 0 {
			id = n.id
		}
	}
	sn.reveal = id
	sn.halt()
}

func (sn *snapState) halt() {
	sn.gliding, sn.settling = false, false
	sn.rest = [2]uint64{}
}

// glideTo sends the container to rest at x, y, the places of elements
// ids, as of now.
func (sn *snapState) glideTo(rt *engine, s *state, x, y float64, ids [2]uint64, now time.Time) {
	sn.settling = false
	if s.track != nil {
		s.track.glide = nil // the user takes over from ScrollTo
	}
	if l := s.list; l != nil {
		l.scrolled = false // and from the list's
	}
	s.wheel = nil
	if rt.preferences().ReduceMotion {
		sn.gliding, sn.rest = false, ids
		s.scrollTo(x, y)
		return
	}
	if !sn.gliding {
		sn.gx = channel{value: s.scrollX, target: s.scrollX, at: now}
		sn.gy = channel{value: s.scrollY, target: s.scrollY, at: now}
		sn.doneX, sn.doneY = s.scrollX, s.scrollY
		sn.gliding = true
	}
	sn.to, sn.rest = ids, [2]uint64{}
	headFor(&sn.gx, sn.doneX+x-s.scrollX, snapAnimation, now)
	headFor(&sn.gy, sn.doneY+y-s.scrollY, snapAnimation, now)
}

func (sn *snapState) frame(rt *engine, s *state, now time.Time) bool {
	if s.flags&(flagScrollX|flagScrollY) == 0 {
		sn.halt()
		return false
	}
	if t := s.track; t != nil && (t.X != float32(s.scrollX) || t.Y != float32(s.scrollY) || t.glide != nil) ||
		s.list != nil && s.list.scrolled {
		// The app set the offset, or a ScrollTo, of its ScrollState or
		// its ListState: it takes over.
		sn.halt()
		return false
	}
	if sn.settling {
		switch {
		case rt.scrollDrag.st == s:
			// The thumb of its scroll bar holds it until let go.
			return true
		case now.Before(sn.settleAt):
			rt.scheduleAt(sn.settleAt)
			return true
		}
		sn.settling = false
		x, y := s.scrollX, s.scrollY
		tx, idx := sn.land(sn.x, x, x, sn.view[0], false, 0)
		ty, idy := sn.land(sn.y, y, y, sn.view[1], false, 0)
		if math.Abs(tx-x) < snapEps && math.Abs(ty-y) < snapEps {
			sn.rest = [2]uint64{idx, idy}
			return false
		}
		sn.glideTo(rt, s, tx, ty, [2]uint64{idx, idy}, now)
	}
	if !sn.gliding {
		return false
	}
	sn.gx.advance(now)
	sn.gy.advance(now)
	dx, dy := sn.gx.value-sn.doneX, sn.gy.value-sn.doneY
	sn.doneX, sn.doneY = sn.gx.value, sn.gy.value
	mx, my := s.maxOffsets()
	s.scrollTo(max(0, min(s.scrollX+dx, mx)), max(0, min(s.scrollY+dy, my)))
	if !sn.gx.moving && !sn.gy.moving {
		sn.gliding, sn.rest = false, sn.to
		return false
	}
	rt.animating = true
	return true
}
