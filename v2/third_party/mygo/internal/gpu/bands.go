package gpu

import (
	"math"

	"github.com/egoist/mygo/internal/scene"
)

// Bands. A border-only fill (addFill) writes nothing where the coverages of
// its outer and inner edges are both 1, nor outside its outer edge, and a
// shadow (addShadow) nothing inside the box casting it. Cut around one
// rectangle (addAround), a ring with large corners keeps most of what it
// shades for nothing: the core of a circle is less than the square inside
// it, and the shaders evaluate the ring's shape in every pixel of its box
// to draw a line a pixel wide. So such a shape is cut into bands of rows
// instead, each drawing only the columns between where its outer edge may
// reach and where its inner one certainly covers, on either side: what is
// shaded grows with the ring's length, not its area. The bands of the rows
// across its corners are as many as pay for their draw calls (bandCost);
// the pixels are the same either way. Shapes with continuous corners, whose
// curves the shaders compute otherwise, are still cut around one rectangle.

// bandCost is what a draw call of its own costs, in pixels shaded: as
// shadowSplitMin, below which a shadow is not split.
const bandCost = shadowSplitMin

// bandMargin is how far, in pixels, from the edge of a shape a pixel is
// taken to be covered by 0 or 1, beyond the pixel's half that antialiasing
// reaches (coverage is 0.5 less the distance to the edge), as shadowCore.
const bandMargin = 2

// rounded is a rectangle with circular corners of radii (top left, top
// right, bottom right, bottom left), fitted (scene.Corners), as the
// shaders' sdRoundRect takes it: each quarter of the rectangle has its
// corner's radius.
type rounded struct {
	r     scene.Rect
	radii [4]float32
}

// span returns the columns that the shape, grown by t (shrunk when
// negative), covers at row y, and false for none: grown by t, a corner's
// radius grows by t too, to 0 at least.
func (s *rounded) span(y, t float32) (l, r float32, ok bool) {
	x0, y0, x1, y1 := s.r.X-t, s.r.Y-t, s.r.X+s.r.W+t, s.r.Y+s.r.H+t
	if y < y0 || y > y1 || x1 <= x0 {
		return 0, 0, false
	}
	top := y < s.r.Y+s.r.H/2
	left, right := s.radii[3], s.radii[2]
	if top {
		left, right = s.radii[0], s.radii[1]
	}
	dy := y - y0 // from the nearer of the top and the bottom
	if !top {
		dy = y1 - y
	}
	in := func(radius float32) float32 {
		rho := max(radius+t, 0) // the corner's radius grown by t
		if dy >= rho {
			return 0
		}
		d := rho - dy
		return rho - float32(math.Sqrt(float64(max(rho*rho-d*d, 0))))
	}
	l, r = x0+in(left), x1-in(right)
	return l, r, l < r
}

// widest returns the columns the shape, grown by t, covers at some row
// between y0 and y1: those at the row nearest its middle, the widest of a
// convex shape.
func (s *rounded) widest(y0, y1, t float32) (l, r float32, ok bool) {
	return s.span(min(max(s.r.Y+s.r.H/2, y0), y1), t)
}

// narrowest returns the columns the shape, shrunk by t, covers at every
// row between y0 and y1: those it covers at both, as it is convex.
func (s *rounded) narrowest(y0, y1, t float32) (l, r float32, ok bool) {
	l0, r0, ok0 := s.span(y0, -t)
	l1, r1, ok1 := s.span(y1, -t)
	l, r = max(l0, l1), min(r0, r1)
	return l, r, ok0 && ok1 && l < r
}

// band is a band of rows from top to bottom, of which the columns from
// left to right are drawn, less those from holeL to holeR.
type band struct {
	top, bottom, left, right, holeL, holeR int32
}

// strips returns the scissor rectangles of the band, empty where none.
func (bd band) strips() (out [2]Scissor) {
	if bd.holeL >= bd.holeR {
		out[0] = Scissor{bd.left, bd.top, bd.right, bd.bottom}
		return out
	}
	out[0] = Scissor{bd.left, bd.top, bd.holeL, bd.bottom}
	out[1] = Scissor{bd.holeR, bd.top, bd.right, bd.bottom}
	return out
}

// bandsCost returns what drawing the bands costs, in pixels (see bandCost).
func bandsCost(bands []band) int64 {
	var c int64
	for _, bd := range bands {
		for _, st := range bd.strips() {
			if !st.Empty() {
				c += st.area() + bandCost
			}
		}
	}
	return c
}

// planBands plans, into b.bands, bands of the rows of whole drawing an
// instance that writes nothing within all of holes, shrunk by bandMargin
// (where they all cover by 1), nor, unless outer is nil, outside outer
// grown by it; and returns false when drawing whole costs less.
func (b *Builder) planBands(whole Scissor, outer *rounded, holes []rounded) bool {
	const m = bandMargin
	b.bands = b.bands[:0]
	if whole.area() <= bandCost || len(holes) == 0 {
		return false // two draw calls cost more than it
	}
	top, bottom := whole.Top, whole.Bottom
	if outer != nil {
		top = max(top, int32(math.Floor(float64(outer.r.Y-m))))
		bottom = min(bottom, int32(math.Ceil(float64(outer.r.Y+outer.r.H+m))))
	}
	// The rows where the holes may hide columns, and the rows across their
	// corners, at the top and the bottom: all of them, then those shown.
	hTop, hBottom := int32(math.MinInt32), int32(math.MaxInt32)
	var rTop, rBottom float32
	for i := range holes {
		h := &holes[i]
		hTop = max(hTop, int32(math.Ceil(float64(h.r.Y+m))))
		hBottom = min(hBottom, int32(math.Floor(float64(h.r.Y+h.r.H-m))))
		rTop = max(rTop, h.radii[0], h.radii[1])
		rBottom = max(rBottom, h.radii[2], h.radii[3])
	}
	if top >= bottom {
		return true // nothing to draw
	}
	// What bands could leave out at most, the holes' rectangle and what
	// lies outside outer's, near its corners: a hole takes four draw calls
	// or more, three more than drawing whole, so less than two never pays.
	hole := whole
	for i := range holes {
		h := &holes[i].r
		hole = hole.intersect(Scissor{int32(math.Ceil(float64(h.X + m))), int32(math.Ceil(float64(h.Y + m))),
			int32(math.Floor(float64(h.X + h.W - m))), int32(math.Floor(float64(h.Y + h.H - m)))})
	}
	saved := hole.area()
	if outer != nil {
		o := &outer.r
		in := whole.intersect(Scissor{int32(math.Floor(float64(o.X - m))), int32(math.Floor(float64(o.Y - m))),
			int32(math.Ceil(float64(o.X + o.W + m))), int32(math.Ceil(float64(o.Y + o.H + m)))})
		saved += whole.area() - in.area()
		for _, r := range outer.radii {
			saved += int64((r + m) * (r + m))
		}
	}
	if saved <= 2*bandCost {
		return false
	}
	mTop := hTop + int32(math.Ceil(float64(rTop)))
	mBottom := hBottom - int32(math.Ceil(float64(rBottom)))
	if mTop > mBottom {
		mTop = (mTop + mBottom) / 2
		mBottom = mTop
	}
	clamp := func(y int32) int32 { return min(max(y, top), bottom) }
	hTop, hBottom, mTop, mBottom = clamp(hTop), clamp(hBottom), clamp(mTop), clamp(mBottom)
	plan := func(out []band, k int32) []band {
		add := func(y0, y1 int32) {
			if y0 >= y1 {
				return
			}
			bd := band{top: y0, bottom: y1, left: whole.Left, right: whole.Right}
			fy0, fy1 := float32(y0), float32(y1)
			if outer != nil {
				l, r, ok := outer.widest(fy0, fy1, m)
				if !ok {
					return
				}
				bd.left = max(bd.left, int32(math.Floor(float64(l))))
				bd.right = min(bd.right, int32(math.Ceil(float64(r))))
			}
			if y0 >= hTop && y1 <= hBottom {
				hl, hr, ok := float32(math.Inf(-1)), float32(math.Inf(1)), true
				for i := range holes {
					l, r, in := holes[i].narrowest(fy0, fy1, m)
					hl, hr, ok = max(hl, l), min(hr, r), ok && in
				}
				if ok {
					bd.holeL = max(int32(math.Ceil(float64(hl))), bd.left)
					bd.holeR = min(int32(math.Floor(float64(hr))), bd.right)
				}
			}
			if bd.left < bd.right {
				out = append(out, bd)
			}
		}
		if hTop >= hBottom {
			add(top, bottom)
			return out
		}
		add(top, hTop)
		for i := range k {
			add(hTop+(mTop-hTop)*i/k, hTop+(mTop-hTop)*(i+1)/k)
		}
		add(mTop, mBottom)
		for i := range k {
			add(mBottom+(hBottom-mBottom)*i/k, mBottom+(hBottom-mBottom)*(i+1)/k)
		}
		add(hBottom, bottom)
		return out
	}
	// As many bands across the corners as pay for their draw calls: twice
	// as many while that costs less, each of minBandRows rows or more.
	best, found := whole.area()+bandCost, false
	last := int64(math.MaxInt64)
	rows := max(mTop-hTop, hBottom-mBottom)
	for k := int32(1); k <= 32 && (k == 1 || rows/k >= minBandRows); k *= 2 {
		b.bandsTry = plan(b.bandsTry[:0], k)
		c := bandsCost(b.bandsTry)
		if c < best {
			best, found = c, true
			b.bands, b.bandsTry = b.bandsTry, b.bands
		}
		if c >= last {
			break
		}
		last = c
	}
	return found
}

// minBandRows is the fewest rows a band across corners has: fewer pay for
// their draw calls only in shapes larger than windows.
const minBandRows = 8

// addBands appends in as the strips of the bands planBands planned, each
// with a scissor rectangle of its own, cut as addAround does.
func (b *Builder) addBands(in Instance, m scene.Affine) {
	defer b.scissorBatches(Scissor{}, false)
	for _, bd := range b.bands {
		for _, st := range bd.strips() {
			if st.Empty() {
				continue
			}
			b.scissorBatches(st, true)
			b.addCut(in, 0, m)
		}
	}
}
