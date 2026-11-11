package ui

import (
	"github.com/egoist/mygo/internal/scene"
	"github.com/egoist/mygo/transfer"
)

// Elements drag values to others within a window: Drag makes an element
// the source of a value, which a press moving a few DIPs starts dragging,
// with a copy of the element following the pointer; Drop and DragOver make
// an element take values of a type, the innermost under the pointer taking
// them. Escape gives up a drag. Lists and grids reorder their rows by
// dragging them (ListState.Reorder, GridState.Reorder), and lists drag
// them out for other elements to take (ListState.DragRows).

// dragStart is how far the pointer moves pressing a source before it
// drags it.
const dragStart = 4

// valueDrag is a value being dragged: the element dragging it, the value,
// where the pointer pressed the element, the element taking it under the
// pointer, and the source's element in the frame, for its copy.
type valueDrag struct {
	src      uint64
	value    any
	offX     float32
	offY     float32
	over     uint64
	elem     *node
	canceled bool
	native   bool
	// count is how many things the value stands for, as rows dragged
	// (countedDrag), for the badge beside the pointer.
	count int
}

// Drag makes the element the source of value, dragged within the window
// once the pointer pressing the element moves a few DIPs: a copy of the
// element follows the pointer, and the element taking the value under it
// gets it when the pointer is released (Drop). A press dragging is no
// click. Dragging reports the drag.
func (e *node) Drag(value any) *node {
	e.flags |= flagDraggable | flagHover
	e.st.dragValue = value
	if d := e.c.rt.drag; d != nil && d.src == e.id {
		d.elem = e
		// The value as the frame has it, as of rows that moved.
		if !d.native {
			d.value = value
		}
	}
	return e
}

// dragFrom makes the element the source of a value that value returns as
// the drag starts, as the rows chosen in a list.
func (e *node) dragFrom(value func() any) *node {
	e.flags |= flagDraggable | flagHover
	e.st.dragFn = value
	if d := e.c.rt.drag; d != nil && d.src == e.id {
		d.elem = e
	}
	return e
}

// DragMissed reports, in the frame after, that the value the element
// drags (Drag) was let go over no element taking it, and where, in the
// window's coordinates: outside the window when it was let go past its
// edge, as to tear a tab out into a window of its own. A drag given up,
// with Escape, is not missed.
func (e *node) DragMissed() (Point, bool) {
	if !e.hasState() || !e.st.dragMissed {
		return Point{}, false
	}
	e.c.rt.consumed = true
	return Point{e.st.missX, e.st.missY}, true
}

// Dragging reports whether the element is dragging its value (Drag), or
// the pointer is dragging the element (Draggable).
func (e *node) Dragging() bool {
	if !e.hasState() {
		return false
	}
	if d := e.c.rt.drag; d != nil && d.src == e.id && !d.canceled {
		return true
	}
	return e.st.pressed && !e.disabled() && e.st.dragRec.dragged(e.c.rt, e.st)
}

// Drop makes e take values of type T dragged within the window (Element.Drag)
// and returns the value dropped on it since the last frame.
//
//	if f, ok := ui.Drop[*File](folder); ok {
//		app.move(f, dir)
//	}
func coreDrop[T any](e *node) (T, bool) {
	var zero T
	e.flags |= flagValueDrop
	e.st.accepts = func(v any) bool { _, ok := v.(T); return ok }
	if v, ok := e.st.droppedValue.(T); ok && e.st.hasDropped {
		e.c.rt.consumed = true
		return v, true
	}
	return zero, false
}

// DragOver makes e take values of type T dragged within the window, as
// Drop does, and returns the value dragged over it, for showing it would
// take it.
func coreDragOver[T any](e *node) (T, bool) {
	var zero T
	e.flags |= flagValueDrop
	e.st.accepts = func(v any) bool { _, ok := v.(T); return ok }
	if in := e.c.rt.incoming; in != nil && e.c.rt.dataOver == e.id {
		v, ok := in.Local.(T)
		return v, ok
	}
	d := e.c.rt.drag
	if d == nil || d.canceled || d.over != e.id {
		return zero, false
	}
	v, ok := d.value.(T)
	return v, ok
}

// valueTarget returns the innermost element at (x, y) taking value.
func (rt *engine) valueTarget(x, y float32, value any) uint64 {
	for _, id := range rt.hitChain(x, y) {
		if s := rt.states[id]; s != nil && s.flags&flagValueDrop != 0 && s.flags&flagDisabled == 0 && s.accepts != nil && s.accepts(value) {
			return id
		}
	}
	return 0
}

// dragMove starts dragging the source pressed once the pointer moved
// far enough, and follows the element under it.
func (rt *engine) dragMove(x, y float32) {
	s := rt.pressed
	if s == nil || rt.pressButton != 0 {
		return
	}
	if !s.drags() {
		if s = rt.handPress(x, y); s == nil {
			return
		}
	}
	d := rt.drag
	if d == nil {
		px, py := s.world.Point(s.x+s.pressX, s.y+s.pressY)
		dx, dy := x-px, y-py
		if dx*dx+dy*dy < dragStart*dragStart {
			return
		}
		value, count := s.dragValue, 0
		if s.dragFn != nil {
			value = s.dragFn()
		}
		if c, ok := value.(countedDrag); ok {
			value, count = c.value, c.n
		}
		if value == nil && s.dataSource == nil {
			return
		}
		d = &valueDrag{src: s.id, value: value, offX: s.pressX, offY: s.pressY, count: count}
		rt.drag = d
		if src := s.dataSource; src != nil {
			d.native = true
			options := src.options
			if options.Preview == nil {
				options.Preview, options.Hotspot = rt.dataPreview(s)
			}
			done := options.Done
			options.Done = func(result transfer.Result) {
				rt.drag = nil
				if p := rt.pressed; p != nil {
					p.pressed = false
					rt.pressed = nil
				}
				rt.clearDataOver()
				if !rt.closed {
					rt.requestFrame()
				}
				if done != nil {
					done(result)
				}
			}
			if err := rt.host.startDataDrag(src.data(), value, options, x*rt.zoomFactor(), y*rt.zoomFactor()); err != nil {
				options.Done(transfer.Result{Err: err})
			}
			return
		}
	}
	if d.native {
		return
	}
	if d.canceled {
		return
	}
	d.over = rt.valueTarget(x, y, d.value)
	rt.requestFrame()
}

// countedDrag is a value a source drags with how many things it stands
// for, as a list's rows or a grid's items: the drag carries the value
// alone, and shows the number.
type countedDrag struct {
	value any
	n     int
}

// drags reports whether the element drags a value (Drag, a list's rows,
// DragData).
func (s *state) drags() bool {
	return s.dragValue != nil || s.dragFn != nil || s.dataSource != nil
}

// pressDragger returns the element around target, the one pressed, that
// drags a value, as a list's row around a button, when target only takes
// clicks: one taking the pointer's moves (a text, a slider, a value of its
// own to drag) keeps them.
func pressDragger(rt *engine, chain []uint64, target *state) uint64 {
	if target.drags() || target.flags&(flagEditable|flagSelectable|flagDraggable|flagTrackPointer) != 0 {
		return 0
	}
	inside := false
	for _, id := range chain {
		s := rt.states[id]
		switch {
		case s == nil:
		case s == target:
			inside = true
		case inside && s.flags&flagDisabled == 0 && s.drags():
			return id
		}
	}
	return 0
}

// handPress gives the press to the element around the one pressed that
// drags a value, once the pointer moves as far as starts a drag: the
// element pressed has no click then. It returns that element, or nil.
func (rt *engine) handPress(x, y float32) *state {
	s, a := rt.pressed, rt.states[rt.pressDragger]
	if a == nil || rt.drag != nil || !a.drags() {
		return nil
	}
	px, py := s.world.Point(s.x+s.pressX, s.y+s.pressY)
	if dx, dy := x-px, y-py; dx*dx+dy*dy < dragStart*dragStart {
		return nil
	}
	rt.pressed, rt.pressDragger = a, 0
	s.pressed = false
	a.pressed, a.pressButton, a.pressMods, a.pressPending = true, s.pressButton, s.pressMods, true
	a.pressX, a.pressY = a.local(px, py)
	a.drag.reset(rt.now(), px, py)
	return a
}

// dragEnd drops the value dragged on the element taking it under the
// pointer, and reports whether a drag ended, which is no click.
func (rt *engine) dragEnd() bool {
	d := rt.drag
	if d == nil {
		return false
	}
	if d.native {
		return true
	}
	rt.drag = nil
	if !d.canceled {
		if t := rt.states[rt.valueTarget(rt.pointerX, rt.pointerY, d.value)]; t != nil {
			t.droppedValue, t.hasDropped = d.value, true
			t.dropX, t.dropY = rt.pointerX, rt.pointerY
		} else if s := rt.states[d.src]; s != nil {
			s.dragMissed, s.missX, s.missY = true, rt.pointerX, rt.pointerY
		}
	}
	rt.requestFrame()
	return true
}

// dragCancel gives up the drag, as Escape does.
func (rt *engine) dragCancel() bool {
	if d := rt.drag; d != nil && !d.canceled {
		if d.native {
			rt.host.cancelDataDrag()
			return true
		}
		d.canceled, d.over = true, 0
		rt.requestFrame()
		return true
	}
	return false
}

// dragScroll scrolls the scroll container under the pointer while a value
// or native transfer is dragged near one of its edges, faster nearer to it.
func (rt *engine) dragScroll() {
	d := rt.drag
	// A native source may still be alive after leaving this window. Only
	// its destination events (incoming) tell us it is over this window.
	if rt.incoming == nil && (d == nil || d.canceled || d.native) {
		return
	}
	const edge = 32
	for _, id := range rt.hitChain(rt.pointerX, rt.pointerY) {
		s := rt.states[id]
		if s == nil || s.flags&flagScrollY == 0 {
			continue
		}
		var dy float32
		_, y := s.local(rt.pointerX, rt.pointerY)
		switch top, bottom := y, s.h-y; {
		case top < edge:
			dy = -(edge - top) / 2
		case bottom < edge:
			dy = (edge - bottom) / 2
		}
		if dy != 0 && scrollBy(s, 0, dy) {
			rt.animating = true
			// The element under the pointer changes as the content moves.
			if d != nil && !d.native && !d.canceled {
				d.over = rt.valueTarget(rt.pointerX, rt.pointerY, d.value)
			}
		}
		return
	}
}

// paintDrag paints a copy of the element dragging its value under the
// pointer, above the rest.
func (rt *engine) paintDrag(p *Painter, w, h float32) {
	d := rt.drag
	if d == nil || d.canceled || d.native || d.elem == nil || d.elem.c == nil {
		return
	}
	e := d.elem
	px, py := e.world.Point(e.x+d.offX, e.y+d.offY)
	dx, dy := rt.pointerX-px, rt.pointerY-py
	affine := hasVisualTransform(e)
	if affine {
		shiftVisual(e, dx, dy)
	} else {
		shift(e, dx, dy)
	}
	saved, savedClip, savedTheme, dimmed := p.opacity, p.clip, p.theme, e.opacitySet
	// Painted alone, it takes the theme of the Themed around it.
	p.opacity, p.clip, p.theme, e.opacitySet = 0.75, Rect{0, 0, w, h}, e.theme(), false
	p.element(e)
	p.opacity = 1
	// How many go with it, as several rows dragged.
	if n := d.count; n > 1 {
		t := p.Theme()
		label := Span{Text: itoa(n), Size: t.FontSize - 2, Weight: 600, Color: t.AccentText}
		tw, th := p.MeasureText(0, label)
		bw := max(tw+10, th+4)
		// Beside the pointer, as the copy may be wider than the window.
		x, y := rt.pointerX+10, rt.pointerY-th-14
		p.Fill(Rect{x, y, bw, th + 4}, t.Accent, (th+4)/2)
		p.RichText(x+(bw-tw)/2, y+2, 0, label)
	}
	p.opacity, p.clip, p.theme, e.opacitySet = saved, savedClip, savedTheme, dimmed
	if affine {
		restoreVisual(e)
	} else {
		shift(e, -dx, -dy)
	}
}

func hasVisualTransform(e *node) bool {
	if e.world.Set {
		return true
	}
	for ch := e.first; ch != nil; ch = ch.next {
		if hasVisualTransform(ch) {
			return true
		}
	}
	return false
}

func restoreVisual(e *node) {
	e.world = e.st.world
	for ch := e.first; ch != nil; ch = ch.next {
		restoreVisual(ch)
	}
}

func shiftVisual(e *node, dx, dy float32) {
	e.world = scene.Translation(dx, dy).Mul(e.world)
	for ch := e.first; ch != nil; ch = ch.next {
		shiftVisual(ch, dx, dy)
	}
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}

// shift moves an element and those inside it by (dx, dy).
func shift(e *node, dx, dy float32) {
	e.x, e.y = e.x+dx, e.y+dy
	for i := range e.frags {
		e.frags[i].X += dx
		e.frags[i].Y += dy
	}
	for ch := e.first; ch != nil; ch = ch.next {
		shift(ch, dx, dy)
	}
}
