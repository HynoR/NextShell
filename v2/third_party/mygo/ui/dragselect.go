package ui

import "runtime"

// dragSelect chooses the rows press p on one of them drags over
// (ListState.DragSelect), in a Table choosing cells the columns too; the
// engine scrolls the list as the pointer nears its edges
// (scrollDragSelect).
func (f *listFrame) dragSelect(p *state) {
	s, rt := f.s, f.c.rt
	d := &s.dragSel
	if !f.dragSelects() {
		d.on = false
		return
	}
	if p == nil || rt.pressButton != 0 {
		if d.on && p == nil {
			if _, built := s.rows[d.id]; !built {
				d.on = false // released while its list was not built
			}
		}
		return
	}
	b, ok := p.valueBinding.(*listRowInput)
	if !ok || b.f != f {
		return
	}
	if !d.on {
		if !p.drag.far || b.i >= f.n || f.isHeader(b.i) {
			return
		}
		d.on, d.from, d.id, d.add = true, b.i, p.id, p.pressMods&Cmd != 0
		extend := p.pressMods&Shift != 0
		switch {
		case extend:
			// The Shift-press extended the choice from where it began, and
			// so does the drag.
		case d.add:
			f.lead(d.from, true)
		default:
			f.choose(d.from)
		}
		if f.choosesCells() {
			f.startDragCells(p.drag.ox, p.drag.oy, extend)
		}
	}
	if d.id != p.id {
		return
	}
	x, y := p.pointer(rt)
	if to := f.rowAt(x, y); to >= 0 && !f.isHeader(to) {
		// Adding to the choice, the rows from the one pressed to the one
		// under the pointer take the place of those the drag chose before
		// on every platform, as on macOS: rows it leaves are no longer
		// chosen.
		f.extendBy(to, d.add, d.add || runtime.GOOS == "darwin")
	}
	if f.choosesCells() {
		f.dragCells(x, y)
	}
}

// DragSelecting reports whether a drag choosing rows (DragSelect), or the
// cells of a Table (CellSelect), goes on: the pointer holds it. An app can
// put off work that follows the choice, as a sum of the cells chosen, until
// it ends: from the frame of the release on, it reports false.
func (s *ListState) DragSelecting() bool {
	d, f := &s.dragSel, &s.frame
	if !d.on || f.c == nil {
		return false
	}
	p := f.c.rt.pressed
	return p != nil && p.id == d.id
}

// dragSelects reports whether a drag over the rows chooses them
// (ListState.DragSelect), as it does in a Table choosing cells.
func (f *listFrame) dragSelects() bool {
	return f.s.Selection != nil && (f.s.DragSelect || f.choosesCells())
}

// rowAt returns the row of the last frame at the window's point (x, y), as
// the rows show, the first or last of those built when the point is above
// or below them, or -1 for none.
func (f *listFrame) rowAt(x, y float32) int {
	rt := f.c.rt
	at := -1
	var first, last *state
	firstRow, lastRow := -1, -1
	for id, r := range f.s.rows {
		st := rt.states[id]
		if st == nil || r.row >= f.n {
			continue
		}
		if _, ly := st.local(x, y); ly >= 0 && ly < st.h {
			at = r.row
		}
		if first == nil || st.y < first.y {
			first, firstRow = st, r.row
		}
		if last == nil || st.y+st.h > last.y+last.h {
			last, lastRow = st, r.row
		}
	}
	switch {
	case at >= 0:
		return at
	case first == nil:
		return -1
	}
	if _, ly := first.local(x, y); ly < 0 {
		return firstRow
	}
	if _, ly := last.local(x, y); ly >= last.h {
		return lastRow
	}
	return -1
}

// dragScrollEdge is how near an edge of a scrolling element the pointer
// dragging in it scrolls it, faster nearer and past.
const dragScrollEdge = 32

// scrollDragSelect scrolls the list a drag choosing its rows presses in as
// the pointer nears or passes its top or bottom, and in a Table choosing
// cells its sides, at the start of a frame as text selection scrolls.
func (rt *engine) scrollDragSelect() {
	p := rt.pressed
	if p == nil || rt.pressButton != 0 {
		return
	}
	b, ok := p.valueBinding.(*listRowInput)
	if !ok || b.f == nil || !b.f.s.dragSel.on || b.f.s.dragSel.id != p.id {
		return
	}
	f := b.f
	st := f.e.st
	x, y := st.local(rt.pointerX, rt.pointerY)
	speed := func(at, size float32) float32 {
		// In a list shorter than both edges, each takes its half.
		edge := min(dragScrollEdge, size/2)
		switch {
		case at < edge:
			return -min(edge-at, 2*dragScrollEdge) / 2
		case at > size-edge:
			return min(at-(size-edge), 2*dragScrollEdge) / 2
		}
		return 0
	}
	dx, dy := float32(0), speed(y, st.h)
	if f.choosesCells() {
		dx = speed(x, st.w)
	}
	if (dx != 0 || dy != 0) && scrollBy(st, dx, dy) {
		rt.animating = true
	}
}
