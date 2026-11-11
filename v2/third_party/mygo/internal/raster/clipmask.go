package raster

import (
	"image"
	"math"
	"runtime"

	"github.com/egoist/mygo/internal/scene"
)

// ClipMask updates a retained coverage image for nested clips in area.
// GPU renderers cache this independently of the content within a clip, so
// moving content under a stationary clip costs no CPU pixel work.
//
// A row works out coverage only near the clips' edges: it is 0 outside a
// clip's bounds and 1 within the span every clip without a transform
// covers entirely, filled a run at a time. Large masks take several cores.
func ClipMask(dst *scene.Image, clips []scene.ClipShape, area image.Rectangle) *scene.Image {
	w, h := area.Dx(), area.Dy()
	if dst == nil {
		dst = scene.NewImageRGBA(w, h, make([]byte, 4*w*h))
	} else if dst.W != w || dst.H != h {
		dst.Resize(w, h)
	}
	m := &clipMasker{dst: dst, area: area, clips: make([]maskClip, len(clips))}
	for i := range clips {
		c := &clips[i]
		mc := &m.clips[i]
		mc.shape = newShape(c.Rect, scene.Corners(c.Rect, c.Radii, c.Continuous))
		mc.transform = c.Transform
		mc.inverse, mc.valid = c.Transform.Inverse()
		mc.mask, mc.masked = newClipMasking(c), c.Masks()
		b := c.Rect
		if c.Transform.Set {
			b = c.Transform.Bounds(c.Rect)
		}
		// Continuous corners antialias up to 2 pixels out.
		const margin = 3
		mc.x0, mc.x1 = int(math.Floor(float64(b.X)))-margin, int(math.Ceil(float64(b.X+b.W)))+margin
		mc.y0, mc.y1 = int(math.Floor(float64(b.Y)))-margin, int(math.Ceil(float64(b.Y+b.H)))+margin
		if c.Transform.Perspective() {
			// What is behind the viewer has no useful bounds.
			mc.x0, mc.x1, mc.y0, mc.y1 = math.MinInt, math.MaxInt, math.MinInt, math.MaxInt
		}
		m.transformed = m.transformed || c.Transform.Set || mc.masked
	}
	workers := max(min(runtime.GOMAXPROCS(0), w*h/workArea, maxWorkers), 1)
	if workers == 1 {
		for y := range h {
			m.row(0, y)
		}
	} else {
		var t team
		t.do = m.row
		t.run(workers, h)
	}
	dst.Changed()
	return dst
}

type clipMasker struct {
	dst   *scene.Image
	area  image.Rectangle
	clips []maskClip
	// transformed tells that a clip is transformed or masked, so no run
	// is solid.
	transformed bool
}

type maskClip struct {
	shape
	transform, inverse scene.Affine
	valid              bool
	mask               clipMasking
	masked             bool
	// x0, x1, y0 and y1 bound the frame pixels the clip covers at all.
	x0, x1, y0, y1 int
}

// row writes row y of the mask, from area.Min.Y.
func (m *clipMasker) row(_, y int) {
	w := m.area.Dx()
	pix := m.dst.Pix[4*y*w:][:4*w]
	fy := y + m.area.Min.Y
	// [lo, hi) are the frame pixels any clip covers, [slo, shi) those
	// every clip without a transform covers entirely.
	lo, hi := m.area.Min.X, m.area.Max.X
	slo, shi := lo, hi
	for i := range m.clips {
		c := &m.clips[i]
		if !c.valid || fy < c.y0 || fy >= c.y1 {
			lo, hi = 0, 0
			break
		}
		lo, hi = max(lo, c.x0), min(hi, c.x1)
		if c.transform.Set || c.masked {
			continue
		}
		a, b := solidSpan(&c.shape, float32(fy), float32(fy+1))
		slo, shi = max(slo, a), min(shi, b)
	}
	if lo >= hi {
		clear(pix)
		return
	}
	clear(pix[:4*(lo-m.area.Min.X)])
	clear(pix[4*(hi-m.area.Min.X):])
	wy := float32(fy) + 0.5
	for x := lo; x < hi; x++ {
		at := 4 * (x - m.area.Min.X)
		if x >= slo && x < shi && !m.transformed {
			// Every clip covers the rest of the run entirely.
			n := 4 * (shi - x)
			fillAlpha(pix[at:at+n], 255)
			x = shi - 1
			continue
		}
		wx := float32(x) + 0.5
		cov := float32(1)
		for i := range m.clips {
			c := &m.clips[i]
			if !c.transform.Set && !c.masked && x >= slo && x < shi {
				continue
			}
			cov *= transformedCoverage(&c.shape, c.transform, c.inverse, wx, wy)
			if c.masked && cov > 0 {
				cov *= c.mask.atFrame(c.transform, c.inverse, wx, wy)
			}
			if cov == 0 {
				break
			}
		}
		pix[at], pix[at+1], pix[at+2], pix[at+3] = 0, 0, 0, to8(cov)
	}
}

// fillAlpha sets RGBA pixels to transparent black with alpha a.
func fillAlpha(pix []byte, a byte) {
	if len(pix) == 0 {
		return
	}
	pix[0], pix[1], pix[2], pix[3] = 0, 0, 0, a
	for n := 4; n < len(pix); n *= 2 {
		copy(pix[n:], pix[:n])
	}
}

// clipMasking is what masks a clip besides its rounded rectangle
// (scene.ClipShape.Masks): the alpha of a gradient, and of an image.
type clipMasking struct {
	paint    painter
	img      *scene.Image
	src, dst scene.Rect
}

func newClipMasking(c *scene.ClipShape) clipMasking {
	var m clipMasking
	if c.Paint != scene.PaintSolid {
		op := scene.Op{Rect: c.Rect, Paint: c.Paint, Gradient: c.Gradient, Color: c.Color, Color2: c.Color2}
		if c.Paint.HasStops() {
			op.Stops = [2]int32{0, int32(len(c.Stops))}
		}
		m.paint = newPainter(&op, 1, c.Stops)
	}
	if c.Mask != nil && c.Mask.W > 0 && c.Mask.H > 0 {
		m.img, m.src, m.dst = c.Mask, c.MaskSrc, c.Rect
	}
	return m
}

// at returns how much the gradient and the image let through at (x, y),
// in the clip's coordinates: the alpha of each, the gradient's at the
// point, the image's sampled there, nothing outside its rectangle.
func (m *clipMasking) at(x, y float32) float32 {
	a := float32(1)
	if !m.paint.solid() {
		a = m.paint.at(x, y)[3]
	}
	if m.img != nil {
		d := m.dst
		if d.W <= 0 || d.H <= 0 || x < d.X || y < d.Y || x >= d.X+d.W || y >= d.Y+d.H {
			return 0
		}
		u := m.src.X + (x-d.X)*m.src.W/d.W
		v := m.src.Y + (y-d.Y)*m.src.H/d.H
		a *= sample(m.img, u, v, unflipped(m.src))[3]
	}
	return a
}

// atFrame is at for the frame's pixel centered at (x, y), which m (inv its
// inverse) maps the clip to, when set: what lies behind the viewer lets
// nothing through.
func (m *clipMasking) atFrame(t, inv scene.Affine, x, y float32) float32 {
	if !t.Set {
		return m.at(x, y)
	}
	if !inv.Set {
		return 0
	}
	lx, ly := inv.Point(x, y)
	if t.Perspective() && t.W(lx, ly) <= 0 {
		return 0
	}
	return m.at(lx, ly)
}
