package gpu

import (
	"fmt"
	"image"
	"slices"

	"github.com/egoist/mygo/internal/scene"
)

// Partial redraws. A frame that changes little of a window (a caret
// blinking, a spinner turning, a button under the pointer) redraws only
// what changed (scene.Differ), into a buffer that holds an earlier frame:
// what changed since that one, in rectangles apart from each other, each
// cleared to the frame's color and drawn scissored to it, over the rest
// kept as it was (loaded, not cleared). A spinner in one corner and a
// progress bar in the other redraw only themselves, not all between. A
// surface's swap chain shows its buffers in turn, so a buffer the renderer
// gets holds the frame drawn into it some frames ago, as EGL's buffer age
// says: Ages keeps, for each buffer, what changed since it was drawn.

// maxAges is how many buffers Ages tracks, the most a swap chain has (a
// CAMetalLayer three) and one more.
const maxAges = 4

// maxRects is how many rectangles a part of a frame has at most, as
// scene.Differ's damage: each batch meeting several draws once for each.
const maxRects = 8

// Ages keeps what changed in the frames of a window since each buffer of
// its swap chain was drawn, by an identity of the buffer (the texture of a
// CAMetalLayer's drawable).
type Ages struct {
	// Forget, unless nil, is called with each buffer Ages stops tracking.
	Forget func(id uintptr)
	bufs   [maxAges]age
	n      int
	frame  uint64
	// taken is what Take returned last.
	taken []image.Rectangle
}

type age struct {
	id uintptr
	// changed is what changed since the buffer was drawn, unless whole, in
	// rectangles apart from each other (addRect).
	changed []image.Rectangle
	whole   bool
	// used is the frame the buffer was last drawn in.
	used uint64
}

// Add notes a new frame, which changed the rectangles changed of the last,
// or everything when whole.
func (a *Ages) Add(changed []image.Rectangle, whole bool) {
	a.frame++
	for i := range a.bufs[:a.n] {
		b := &a.bufs[i]
		b.whole = b.whole || whole
		if b.whole {
			b.changed = b.changed[:0]
			continue
		}
		for _, r := range changed {
			b.changed = addRect(b.changed, r)
		}
	}
}

// Take returns what changed in the frames since buffer id was drawn, the
// buffer the frame is drawn into, in rectangles apart from each other
// valid until the next Take, and false when that is unknown (the buffer
// is new, or everything may have changed): the frame draws it whole. It
// then holds the frame.
func (a *Ages) Take(id uintptr) (changed []image.Rectangle, known bool) {
	for i := range a.bufs[:a.n] {
		b := &a.bufs[i]
		if b.id == id {
			a.taken = append(a.taken[:0], b.changed...)
			known = !b.whole
			b.changed, b.whole, b.used = b.changed[:0], false, a.frame
			return a.taken, known
		}
	}
	// A new buffer takes the place of the one drawn least recently.
	i := a.n
	if a.n == maxAges {
		i = 0
		for j := range a.bufs {
			if a.bufs[j].used < a.bufs[i].used {
				i = j
			}
		}
		if a.Forget != nil {
			a.Forget(a.bufs[i].id)
		}
	} else {
		a.n++
	}
	a.bufs[i] = age{id: id, used: a.frame, changed: a.bufs[i].changed[:0]}
	return nil, false
}

// Reset forgets every buffer, as a swap chain makes new ones.
func (a *Ages) Reset() {
	if a.Forget != nil {
		for i := range a.bufs[:a.n] {
			a.Forget(a.bufs[i].id)
		}
	}
	for i := range a.bufs {
		a.bufs[i] = age{changed: a.bufs[i].changed[:0]}
	}
	a.n = 0
}

// addRect adds r to rects, rectangles apart from each other, and returns
// them: r takes in those it overlaps or nearly meets, and past maxRects,
// the two whose union grows least become it.
func addRect(rects []image.Rectangle, r image.Rectangle) []image.Rectangle {
	if r.Empty() {
		return rects
	}
	for i := 0; i < len(rects); {
		if rects[i].Inset(-8).Overlaps(r) {
			r = r.Union(rects[i])
			rects = slices.Delete(rects, i, i+1)
			i = 0
			continue
		}
		i++
	}
	rects = append(rects, r)
	if len(rects) <= maxRects {
		return rects
	}
	bi, bj, best := 0, 1, -1
	for i := range rects {
		for j := i + 1; j < len(rects); j++ {
			grows := pixels(rects[i].Union(rects[j])) - pixels(rects[i]) - pixels(rects[j])
			if best < 0 || grows < best {
				bi, bj, best = i, j, grows
			}
		}
	}
	u := rects[bi].Union(rects[bj])
	rects = slices.Delete(rects, bj, bj+1)
	rects = slices.Delete(rects, bi, bi+1)
	return addRect(rects, u)
}

func pixels(r image.Rectangle) int { return r.Dx() * r.Dy() }

// Part is the part of a frame to redraw: rectangles apart from each other,
// none when nothing changed.
type Part struct {
	Rects []Scissor
	// rects, grown and next are Partial's.
	rects, grown, next []image.Rectangle
}

// Bounds returns the rectangle around the part's.
func (p *Part) Bounds() Scissor {
	var u image.Rectangle
	for _, s := range p.Rects {
		u = u.Union(image.Rect(int(s.Left), int(s.Top), int(s.Right), int(s.Bottom)))
	}
	return Scissor{int32(u.Min.X), int32(u.Min.Y), int32(u.Max.X), int32(u.Max.Y)}
}

// String describes the part, for FrameInfo.
func (p *Part) String() string {
	b := p.Bounds()
	s := fmt.Sprintf("%d×%d at %d,%d", b.Right-b.Left, b.Bottom-b.Top, b.Left, b.Top)
	if len(p.Rects) > 1 {
		n := 0
		for _, r := range p.Rects {
			n += int(r.Right-r.Left) * int(r.Bottom-r.Top)
		}
		s = fmt.Sprintf("%d rectangles of %d pixels in %s", len(p.Rects), n, s)
	}
	return s
}

// Set makes p the part of a w×h frame to redraw, given what changed in the
// buffer drawn into (Ages.Take, or scene.Differ.Diff), grown by grow (the
// backdrops of effects, scene.Differ.AddBackdrops), and returns false when
// the frame is better drawn whole: past half the frame together, drawing
// it whole costs about the same.
func (p *Part) Set(changed []image.Rectangle, w, h int, grow func([]image.Rectangle) []image.Rectangle) bool {
	frame := image.Rect(0, 0, w, h)
	p.rects = p.rects[:0]
	for _, r := range changed {
		p.rects = addRect(p.rects, r.Intersect(frame))
	}
	// Grown until what it grows by is in it: a backdrop an effect reads
	// must be within one rectangle, drawn before the effect reads it.
	for grow != nil && len(p.rects) > 0 {
		p.grown = grow(append(p.grown[:0], p.rects...))
		p.next = p.next[:0]
		for _, r := range p.grown {
			p.next = addRect(p.next, r.Intersect(frame))
		}
		p.rects, p.next = p.next, p.rects
		if within(p.rects, p.next) {
			break
		}
	}
	n := 0
	p.Rects = p.Rects[:0]
	for _, r := range p.rects {
		n += pixels(r)
		p.Rects = append(p.Rects, Scissor{int32(r.Min.X), int32(r.Min.Y), int32(r.Max.X), int32(r.Max.Y)})
	}
	return 2*n < w*h
}

// within reports whether each rectangle of rects is within one of in.
func within(rects, in []image.Rectangle) bool {
	for _, r := range rects {
		if !slices.ContainsFunc(in, r.In) {
			return false
		}
	}
	return true
}

// Intersect returns the pixels both s and o take.
func (s Scissor) Intersect(o Scissor) Scissor { return s.intersect(o) }

// Has reports whether Ages tracks buffer id.
func (a *Ages) Has(id uintptr) bool {
	for i := range a.bufs[:a.n] {
		if a.bufs[i].id == id {
			return true
		}
	}
	return false
}

// Pipelines keeps, for each op of the frames a renderer builds, a hash of
// the pipelines (shaders, blending) its instances draw with. An op drawn
// with others than in the last frame (a batch specialized for other
// classes as batching changed around it, a pipeline made meanwhile, a
// clip cut by a mask) draws a unit in the last place apart here and
// there, which a frame drawn whole would change too: its bounds count as
// changed.
type Pipelines struct {
	keys, last []uint64
}

// Damage hashes the pipelines of the ops b built, pipe(i) being batch i's,
// and adds to damage, what changed since the last frame drawn as d found
// (unless not known), the bounds of the ops d matches with an op of the
// last frame drawn with other pipelines, and returns it.
func (p *Pipelines) Damage(b *Builder, pipe func(i int) uint64, d *scene.Differ, damage []image.Rectangle, known bool) []image.Rectangle {
	n := len(b.OpStart) - 1
	p.keys = slices.Grow(p.keys[:0], n)[:n]
	clear(p.keys)
	op := 0
	for i, bt := range b.Batches {
		lo, hi := int32(bt.Start), int32(bt.Start+bt.Count)
		for op < n && b.OpStart[op+1] <= lo {
			op++
		}
		key := pipe(i)
		for k := op; k < n && b.OpStart[k] < hi; k++ {
			if b.OpStart[k+1] > b.OpStart[k] {
				p.keys[k] = (p.keys[k] ^ key) * 1099511628211
			}
		}
	}
	if !known {
		return damage
	}
	bounds := d.Bounds()
	for j, key := range p.keys {
		if i, ok := d.Match(j); ok && i < len(p.last) && p.last[i] != key {
			damage = append(damage, bounds[j])
		}
	}
	return damage
}

// Remember keeps the hashes of the frame Damage was given last, drawn, to
// compare the next with.
func (p *Pipelines) Remember() { p.keys, p.last = p.last, p.keys }
