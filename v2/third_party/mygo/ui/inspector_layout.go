//go:build !mygo_noinspector

package ui

import (
	"cmp"
	"slices"
	"strconv"
)

// The tree's flex and grid badges show the layout of a container over the
// content until they are clicked again, as Chrome's do: the lines of a
// grid's tracks with their numbers, and the room between a container's
// children, hatched.
//
// A grid's tracks exist only as it lays out, so gridLayout gives them to
// noteGridTracks: the layout cache is off while a badge is on, for it to run
// each frame. Laying the grid out again here would move what place already
// put in window coordinates.

const (
	inspNoLayout = iota
	inspFlex
	inspGrid
)

// inspectorBuilt tells the layout that this build has the inspector.
const inspectorBuilt = true

// The colors of the overlays, with those of the highlight's boxes in mind.
var (
	inspFlexColor = RGBA(216, 75, 176, 1)
	inspGridColor = RGBA(151, 85, 255, 1)
)

// inspTrack is a track of a grid, relative to its content, as it was laid
// out.
type inspTrack struct{ at, size float32 }

// inspTracks is a grid's tracks.
type inspTracks struct{ cols, rows []inspTrack }

// inspOverlay is the layout of a container to show over this frame.
type inspOverlay struct {
	e      *node
	box    Rect // its content, where the rest is
	tracks inspTracks
	room   []Rect // the room between its children, or tracks
}

// layoutKind tells which layout a container's badge shows: every box lays
// out as flexbox, which says something only of two children or more.
func layoutKind(e *node) uint8 {
	if e.kind != kindBox || e.list != nil {
		return inspNoLayout
	}
	if e.grid {
		return inspGrid
	}
	n := 0
	for c := e.first; c != nil; c = c.next {
		if c.flags&flagAbsolute == 0 && !c.hidden {
			if n++; n == 2 {
				return inspFlex
			}
		}
	}
	return inspNoLayout
}

// toggleLayout shows or hides the layout of a container.
func (in *inspector) toggleLayout(rt *engine, id uint64) {
	if in.layoutOn[id] {
		delete(in.layoutOn, id)
	} else {
		if in.layoutOn == nil {
			in.layoutOn = map[uint64]bool{}
		}
		in.layoutOn[id] = true
	}
	in.keepLaidOut(rt, len(in.layoutOn) > 0)
}

// keepLaidOut turns the layout cache off while grids are to note their
// tracks, and back to what it was.
func (in *inspector) keepLaidOut(rt *engine, on bool) {
	if on == in.uncached {
		return
	}
	if on {
		in.cacheWasOff = rt.noLayoutCache
		rt.noLayoutCache = true
	} else {
		rt.noLayoutCache = in.cacheWasOff
	}
	in.uncached = on
}

// noteGridTracks notes the tracks of a grid whose layout is shown, by where
// they start from its content's left and top and their sizes. Right to left,
// the columns go from the right of what they are laid out in.
func noteGridTracks(e *node, colAt, cols, rowAt, rows []float32, cw, usedW float32, rtl bool) {
	in := &e.c.rt.insp
	if !in.layoutOn[e.id] {
		return
	}
	right := cw
	if !finite(right) {
		right = usedW
	}
	var t inspTracks
	for i, at := range colAt {
		if rtl {
			at = right - at - cols[i]
		}
		t.cols = append(t.cols, inspTrack{at, cols[i]})
	}
	for i, at := range rowAt {
		t.rows = append(t.rows, inspTrack{at, rows[i]})
	}
	if rtl {
		slices.Reverse(t.cols)
	}
	if in.tracks == nil {
		in.tracks = map[uint64]inspTracks{}
	}
	in.tracks[e.id] = t
}

// measureLayouts works out what paintLayouts shows of the containers the
// walk of the tree found.
func (in *inspector) measureLayouts() {
	for i := range in.overlays {
		o := &in.overlays[i]
		o.box = o.e.contentBox()
		if o.e.grid {
			o.tracks = in.tracks[o.e.id]
			o.room = gridRoom(o)
		} else {
			o.room = flexRoom(o.e, o.box)
		}
	}
}

// contentOrigin returns where the content of a box starts, in the
// coordinates of what is in it: a container scrolls it (place).
func contentOrigin(e *node) (x, y float32) {
	b := e.contentBox()
	if e.scrolls() {
		b.X -= float32(e.scrollLeft())
		b.Y -= float32(e.st.scrollY - e.scrollBase)
	}
	return b.X, b.Y
}

// gridRoom returns the gaps between the tracks of a grid, in window
// coordinates, within its content.
func gridRoom(o *inspOverlay) []Rect {
	x, y := contentOrigin(o.e)
	var room []Rect
	for i := 1; i < len(o.tracks.cols); i++ {
		a, b := o.tracks.cols[i-1], o.tracks.cols[i]
		room = appendRoom(room, Rect{x + a.at + a.size, o.box.Y, b.at - a.at - a.size, o.box.H}, o.box)
	}
	for i := 1; i < len(o.tracks.rows); i++ {
		a, b := o.tracks.rows[i-1], o.tracks.rows[i]
		room = appendRoom(room, Rect{o.box.X, y + a.at + a.size, o.box.W, b.at - a.at - a.size}, o.box)
	}
	return room
}

// flexRoom returns the room a container's children leave along its lines,
// and between the lines, in window coordinates, within its content: its
// gaps and the free space, as Chrome hatches them. Children are told by
// where they are, as the layout may reverse or wrap them.
func flexRoom(e *node, box Rect) []Rect {
	type span struct {
		main, mainEnd, cross, crossEnd float32
	}
	var items []span
	for c := e.first; c != nil; c = c.next {
		if c.flags&flagAbsolute != 0 || c.hidden {
			continue
		}
		x, y, w, h := c.x-c.m(3), c.y-c.m(0), c.w+c.marginX(), c.h+c.marginY()
		if e.row {
			items = append(items, span{x, x + w, y, y + h})
		} else {
			items = append(items, span{y, y + h, x, x + w})
		}
	}
	// The rectangle of a span of the main axis and one of the cross axis.
	rect := func(main, mainEnd, cross, crossEnd float32) Rect {
		if e.row {
			return Rect{main, cross, mainEnd - main, crossEnd - cross}
		}
		return Rect{cross, main, crossEnd - cross, mainEnd - main}
	}
	mainStart, mainEnd, crossStart, crossEnd := box.Y, box.Y+box.H, box.X, box.X+box.W
	if e.row {
		mainStart, mainEnd, crossStart, crossEnd = box.X, box.X+box.W, box.Y, box.Y+box.H
	}
	// Lines, by the cross axis, where a child starts after the line ends.
	slices.SortStableFunc(items, func(a, b span) int { return cmp.Compare(a.cross, b.cross) })
	var room []Rect
	lineCrossEnd := crossStart
	for i := 0; i < len(items); {
		cross, end, j := items[i].cross, items[i].crossEnd, i+1
		if e.wrap {
			for ; j < len(items) && items[j].cross < end; j++ {
				end = max(end, items[j].crossEnd)
			}
		} else {
			for ; j < len(items); j++ {
				end = max(end, items[j].crossEnd)
			}
		}
		// A line that isn't wrapped takes the whole container's cross axis.
		lineStart, lineEnd := cross, end
		if !e.wrap {
			lineStart, lineEnd = crossStart, crossEnd
		} else if cross > lineCrossEnd {
			room = appendRoom(room, rect(mainStart, mainEnd, lineCrossEnd, cross), box)
		}
		lineCrossEnd = end
		line := items[i:j]
		slices.SortStableFunc(line, func(a, b span) int { return cmp.Compare(a.main, b.main) })
		at := mainStart
		for _, it := range line {
			room = appendRoom(room, rect(at, it.main, lineStart, lineEnd), box)
			at = max(at, it.mainEnd)
		}
		room = appendRoom(room, rect(at, mainEnd, lineStart, lineEnd), box)
		i = j
	}
	if e.wrap {
		room = appendRoom(room, rect(mainStart, mainEnd, lineCrossEnd, crossEnd), box)
	}
	return room
}

// appendRoom adds what of r is in box, unless it's too thin to see: layouts
// leave fractions of a DIP between children that touch.
func appendRoom(room []Rect, r, box Rect) []Rect {
	x0, y0 := max(r.X, box.X), max(r.Y, box.Y)
	x1, y1 := min(r.X+r.W, box.X+box.W), min(r.Y+r.H, box.Y+box.H)
	if x1-x0 < 0.5 || y1-y0 < 0.5 {
		return room
	}
	return append(room, Rect{x0, y0, x1 - x0, y1 - y0})
}

// paintLayouts paints the layouts shown over the content.
func (in *inspector) paintLayouts(p *Painter) {
	saved := p.transform
	defer func() { p.transform = saved }()
	for i := range in.overlays {
		o := &in.overlays[i]
		p.transform = o.e.world
		color := inspFlexColor
		if o.e.grid {
			color = inspGridColor
		}
		for _, r := range o.room {
			hatch(p, r, color)
		}
		p.StrokeDashed(o.box, color, 0, 1)
		if o.e.grid {
			in.paintGridLines(p, o, color)
		}
	}
}

// hatch fills a rectangle with diagonal lines over a tint of their color.
func hatch(p *Painter, r Rect, c Color) {
	p.Fill(r, c.Alpha(0.14), 0)
	const step = 6
	var path Path
	for d := float32(step); d < r.W+r.H; d += step {
		path.MoveTo(r.X+min(d, r.W), r.Y+d-min(d, r.W))
		path.LineTo(r.X+d-min(d, r.H), r.Y+min(d, r.H))
	}
	p.StrokePath(&path, 1, c.Alpha(0.6))
}

// paintGridLines paints the lines of a grid's tracks, numbered from 1 along
// its top and its start, as they count in ColumnStart and RowStart.
func (in *inspector) paintGridLines(p *Painter, o *inspOverlay, c Color) {
	x, y := contentOrigin(o.e)
	box := o.box
	rtl := o.e.rtl()
	// A number, after the point it is at, or before it right to left.
	number := func(n int, lx, ly float32) {
		s := Span{Text: strconv.Itoa(n), Color: Hex("#ffffff"), Size: 9, Weight: 600, Font: in.theme.Font}
		w, h := p.MeasureText(0, s)
		if rtl {
			lx -= w + 6
		}
		tag := Rect{lx, ly, w + 6, h + 2}
		p.Fill(tag, c, 2)
		p.RichText(tag.X+3, tag.Y+1, 0, s)
	}
	line := func(x0, y0, x1, y1 float32) {
		if x0 >= box.X-0.5 && x1 <= box.X+box.W+0.5 && y0 >= box.Y-0.5 && y1 <= box.Y+box.H+0.5 {
			p.Line(x0, y0, x1, y1, 1, c)
		}
	}
	cols, rows := o.tracks.cols, o.tracks.rows
	start := box.X
	if rtl {
		start = box.X + box.W
	}
	for i, t := range cols {
		lx, rx := x+t.at, x+t.at+t.size
		line(lx, box.Y, lx, box.Y+box.H)
		line(rx, box.Y, rx, box.Y+box.H)
		n := i + 1
		if rtl {
			n = len(cols) - i // the first column is the rightmost
			lx = rx
		}
		if lx >= box.X-0.5 && lx <= box.X+box.W+0.5 {
			number(n, lx, box.Y)
		}
	}
	for i, t := range rows {
		ty, by := y+t.at, y+t.at+t.size
		line(box.X, ty, box.X+box.W, ty)
		line(box.X, by, box.X+box.W, by)
		if ty >= box.Y-0.5 && ty <= box.Y+box.H+0.5 && i > 0 {
			number(i+1, start, ty)
		}
	}
}
