package loading

import (
	"math"

	"github.com/egoist/mygo/ui"
)

// drawing is an indicator's frame being painted: its square of side s at
// (x, y), how many cycles it has run, and its options.
type drawing struct {
	p       *ui.Painter
	sh      *shape
	x, y, s float32
	cycles  float64
	color   ui.Color
	o       Options
	// still draws the indicator at rest, where the desktop asks for less
	// motion.
	still bool
}

// at returns how far into its cycle the part step of count is, from 0 to
// 1, the parts staggered as loading.dev's: each a count-th of a cycle
// behind the one before, the first a whole cycle ahead, as a negative
// animation-delay. A count of 0 is the indicator's own cycle.
func (d *drawing) at(step, count int) float64 {
	t := d.cycles
	if count > 0 {
		t += float64(count-step) / float64(count)
	}
	return t - math.Floor(t)
}

// turn returns how much of a turn the indicator has turned at p of its
// cycle, the other way if reverse, as Options.Easing says.
func (d *drawing) turn(p float64, reverse bool) float64 {
	if reverse {
		p = 1 - p
	}
	switch d.o.Easing {
	case EaseInOut:
		return easeInOut(p)
	case Stacked:
		return p + easeInOut(p)
	}
	return p
}

// alpha returns the indicator's color at opacity a.
func (d *drawing) alpha(a float64) ui.Color { return d.color.Alpha(float32(a)) }

// view returns the transform from a viewBox of side n, as loading.dev's
// SVGs draw in, to the indicator's square.
func (d *drawing) view(n float32) affine {
	k := d.s / n
	return affine{a: k, d: k, e: d.x, f: d.y}
}

// begin empties the shape and has it draw through m.
func (d *drawing) begin(m affine) *shape {
	d.sh.Reset()
	d.sh.m = m
	return d.sh
}

// fill fills the shape in the indicator's color at opacity a.
func (d *drawing) fill(a float64) { d.p.FillPath(&d.sh.Path, d.alpha(a)) }

// fade returns the opacity of part step of count of the indicators whose
// parts fade in turn (loading.dev's fadeCss): from 1 down to dim through
// each cycle, at rest at rest.
func (d *drawing) fade(step, count int, dim, rest float64) float64 {
	if d.still {
		return rest
	}
	return 1 + (dim-1)*d.at(step, count)
}

// The timing functions of CSS.
var (
	easeInOut  = bezier(0.42, 0, 0.58, 1)
	easeOut    = bezier(0, 0, 0.58, 1)
	overshoot  = bezier(0.68, -0.75, 0.265, 1.75) // Cascade's
	linearEase = func(t float64) float64 { return t }
)

// bezier returns CSS's cubic-bezier(x1, y1, x2, y2).
func bezier(x1, y1, x2, y2 float64) func(float64) float64 {
	cx := 3 * x1
	bx := 3*(x2-x1) - cx
	ax := 1 - cx - bx
	cy := 3 * y1
	by := 3*(y2-y1) - cy
	ay := 1 - cy - by
	x := func(s float64) float64 { return ((ax*s+bx)*s + cx) * s }
	return func(t float64) float64 {
		if t <= 0 || t >= 1 {
			return t
		}
		// Newton's method, then bisection where the slope is flat.
		s := t
		for range 8 {
			e := x(s) - t
			if math.Abs(e) < 1e-7 {
				return ((ay*s+by)*s + cy) * s
			}
			dx := (3*ax*s+2*bx)*s + cx
			if math.Abs(dx) < 1e-6 {
				break
			}
			s -= e / dx
		}
		lo, hi := 0.0, 1.0
		s = t
		for range 40 {
			if x(s) < t {
				lo = s
			} else {
				hi = s
			}
			s = (lo + hi) / 2
		}
		return ((ay*s+by)*s + cy) * s
	}
}

// key is a keyframe: a value at a point of a cycle.
type key struct{ at, v float64 }

// frames returns the value of keyframes ks at p of a cycle, easing each
// stretch between two keyframes, as CSS's timing functions do.
func frames(p float64, ease func(float64) float64, ks ...key) float64 {
	for i := 1; i < len(ks); i++ {
		a, b := ks[i-1], ks[i]
		if p <= b.at {
			if b.at <= a.at {
				return b.v
			}
			return a.v + (b.v-a.v)*ease((p-a.at)/(b.at-a.at))
		}
	}
	return ks[len(ks)-1].v
}

// affine maps (x, y) to (a*x + c*y + e, b*x + d*y + f).
type affine struct{ a, b, c, d, e, f float32 }

// apply maps a point.
func (m affine) apply(x, y float32) (float32, float32) {
	return m.a*x + m.c*y + m.e, m.b*x + m.d*y + m.f
}

// then returns m after n: n maps first.
func (m affine) then(n affine) affine {
	return affine{
		a: m.a*n.a + m.c*n.b, b: m.b*n.a + m.d*n.b,
		c: m.a*n.c + m.c*n.d, d: m.b*n.c + m.d*n.d,
		e: m.a*n.e + m.c*n.f + m.e, f: m.b*n.e + m.d*n.f + m.f,
	}
}

// rotate returns m turning what it maps by deg degrees clockwise around
// (cx, cy), as CSS's rotate().
func (m affine) rotate(deg float64, cx, cy float32) affine {
	s, c := math.Sincos(deg * math.Pi / 180)
	sf, cf := float32(s), float32(c)
	return m.then(affine{a: cf, b: sf, c: -sf, d: cf, e: cx - cf*cx + sf*cy, f: cy - sf*cx - cf*cy})
}

// scale returns m scaling what it maps by sx and sy around (cx, cy).
func (m affine) scale(sx, sy, cx, cy float32) affine {
	return m.then(affine{a: sx, d: sy, e: cx - sx*cx, f: cy - sy*cy})
}

// move returns m moving what it maps by (dx, dy).
func (m affine) move(dx, dy float32) affine { return m.then(affine{a: 1, d: 1, e: dx, f: dy}) }

// unit returns how many DIPs a length of 1 is through m, for strokes.
func (m affine) unit() float32 {
	return float32(math.Sqrt(math.Abs(float64(m.a*m.d - m.b*m.c))))
}

// transform returns m as a ui.Transform, false if it collapses what it
// maps: a scaling, a skew along x, a rotation, then a translation.
func (m affine) transform() (ui.Transform, bool) {
	sx := float32(math.Hypot(float64(m.a), float64(m.b)))
	det := m.a*m.d - m.b*m.c
	if sx == 0 || det == 0 {
		return ui.Transform{}, false
	}
	sy := det / sx
	t := ui.Scaling(sx, sy)
	if k := (m.a*m.c + m.b*m.d) / sx; k != 0 {
		t = t.Then(ui.Skewing(float32(deg(math.Atan(float64(k/sy)))), 0))
	}
	if m.b != 0 || m.a < 0 {
		t = t.Then(ui.Rotation(float32(deg(math.Atan2(float64(m.b), float64(m.a))))))
	}
	return t.Then(ui.Translation(m.e, m.f)), true
}

// shape is a path drawn through a transform, in the coordinates of a
// viewBox or of the indicator's own.
type shape struct {
	ui.Path
	m affine
}

func (s *shape) move(x, y float32) *shape { s.MoveTo(s.m.apply(x, y)); return s }

func (s *shape) line(x, y float32) *shape { s.LineTo(s.m.apply(x, y)); return s }

func (s *shape) cube(x1, y1, x2, y2, x, y float32) *shape {
	ax, ay := s.m.apply(x1, y1)
	bx, by := s.m.apply(x2, y2)
	cx, cy := s.m.apply(x, y)
	s.CubeTo(ax, ay, bx, by, cx, cy)
	return s
}

func (s *shape) close() *shape { s.Close(); return s }

// arc adds the arc of the circle around (cx, cy) of radius r from angle
// a0 to a1, in radians clockwise from the right, as SVG's circles start
// and go: from its start if first, or a line to it.
func (s *shape) arc(cx, cy, r float32, a0, a1 float64, first bool) *shape {
	at := func(a float64) (float32, float32) {
		sin, cos := math.Sincos(a)
		return cx + r*float32(cos), cy + r*float32(sin)
	}
	if first {
		s.move(at(a0))
	} else {
		s.line(at(a0))
	}
	n := int(math.Ceil(math.Abs(a1-a0) / (math.Pi / 2)))
	h := (a1 - a0) / float64(n)
	k := float32(4.0 / 3 * math.Tan(h/4))
	for i := range n {
		b0, b1 := a0+float64(i)*h, a0+float64(i+1)*h
		s0, c0 := math.Sincos(b0)
		s1, c1 := math.Sincos(b1)
		x0, y0 := cx+r*float32(c0), cy+r*float32(s0)
		x1, y1 := cx+r*float32(c1), cy+r*float32(s1)
		s.cube(x0-k*r*float32(s0), y0+k*r*float32(c0), x1+k*r*float32(s1), y1-k*r*float32(c1), x1, y1)
	}
	return s
}

// circle adds a circle, counterclockwise if ccw, as a hole in one.
func (s *shape) circle(cx, cy, r float32, ccw bool) *shape {
	if ccw {
		return s.arc(cx, cy, r, 0, -2*math.Pi, true).close()
	}
	return s.arc(cx, cy, r, 0, 2*math.Pi, true).close()
}

// ring adds the ring between radii in and out, as a circle's border.
func (s *shape) ring(cx, cy, out, in float32) *shape {
	s.circle(cx, cy, out, false)
	if in > 0 {
		s.circle(cx, cy, in, true)
	}
	return s
}

// stroke draws an arc of the circle of radius r around (cx, cy) in the
// shape's coordinates, w wide, from angle a0 to a1 (as arc's), ended as
// Options.Cap says, at opacity a. m maps without turning: the arc is
// drawn from angle 0 and turned to a0 by the renderer, so that an arc of
// the same length is the same mask from frame to frame.
func (d *drawing) stroke(m affine, cx, cy, r, w float32, a0, a1 float64, a float64) {
	if a1 <= a0 {
		return
	}
	d.turned(m, cx, cy, a0, func(m affine) {
		s := d.begin(m)
		if d.o.Cap == FlatCap {
			s.arc(cx, cy, r+w/2, 0, a1-a0, true)
			s.arc(cx, cy, r-w/2, a1-a0, 0, false).close()
			d.fill(a)
			return
		}
		s.arc(cx, cy, r, 0, a1-a0, true)
		d.p.StrokePath(&s.Path, w*m.unit(), d.alpha(a))
	})
}

// turned draws what fn draws through m turned by angle radians clockwise
// around (cx, cy) in m's coordinates. The renderer turns it: what fn
// draws at rest is the same path from frame to frame, whose mask it
// rasterizes once, where drawing it turned would rasterize another every
// frame. fn draws through the m it is given, which rasterizes the mask
// twice as fine, for the renderer to sample it turned as sharp.
func (d *drawing) turned(m affine, cx, cy float32, angle float64, fn func(m affine)) {
	px, py := m.apply(cx, cy)
	d.p.Transform(ui.Scaling(0.5, 0.5).Then(ui.Rotation(float32(deg(angle)))), px, py, func() {
		fn(identity.scale(2, 2, px, py).then(m))
	})
}

// rounded draws the rectangle (x, y, w, h) with corners of radius r in
// m's coordinates, filled, or its border bw wide if bw > 0, at opacity
// a. The renderers draw it themselves, through any transform, as an
// element's background: no path to rasterize into a mask, as moving
// paths need every frame. Its corners are the platform's: circles and
// pills are round, smaller corners continuous on macOS.
func (d *drawing) rounded(m affine, x, y, w, h, r, bw float32, a float64) {
	if t, ok := m.move(x, y).transform(); ok {
		d.roundedIn(t, w, h, r, bw, a)
	}
}

// roundedIn is rounded through t, which maps the rectangle w×h at the
// origin to the indicator.
func (d *drawing) roundedIn(t ui.Transform, w, h, r, bw float32, a float64) {
	if w <= 0 || h <= 0 || a <= 0 {
		return
	}
	// Fill and Stroke put a rectangle and its border on whole pixels:
	// drawn zoom times as large and scaled back, they keep their sizes to
	// a zoom-th of a pixel, as they move and grow.
	const zoom = 8
	box := ui.Rect{W: w * zoom, H: h * zoom}
	c := d.alpha(a)
	d.p.Transform(ui.Scaling(1.0/zoom, 1.0/zoom).Then(t), 0, 0, func() {
		if bw > 0 {
			d.p.Stroke(box, c, r*zoom, bw*zoom)
		} else {
			d.p.Fill(box, c, r*zoom)
		}
	})
}

// dot draws the circle of radius r around (cx, cy) in m's coordinates at
// opacity a.
func (d *drawing) dot(m affine, cx, cy, r float32, a float64) {
	d.rounded(m, cx-r, cy-r, 2*r, 2*r, r, 0, a)
}

// hoop draws the ring between radii out and in around (cx, cy) in m's
// coordinates at opacity a, as a circle's border.
func (d *drawing) hoop(m affine, cx, cy, out, in float32, a float64) {
	d.rounded(m, cx-out, cy-out, 2*out, 2*out, out, max(0, out-in), a)
}

// dashes calls fn with each dash of a pattern of dashes dash long and
// gaps gap long, shifted by offset as SVG's stroke-dashoffset shifts it,
// on a path length long: from and to along it.
func dashes(length, dash, gap, offset float64, fn func(from, to float64)) {
	period := dash + gap
	if dash <= 0 || period <= 0 {
		return
	}
	// From the last dash starting before the path.
	for at := math.Mod(-offset, period) - period; at < length; at += period {
		from, to := max(at, 0), min(at+dash, length)
		if to > from {
			fn(from, to)
		}
	}
}

// capsule draws the line from (x0, y0) to (x1, y1) in m's coordinates, w
// wide, with round ends, at opacity a.
func (d *drawing) capsule(m affine, x0, y0, x1, y1, w float32, a float64) {
	dx, dy := float64(x1-x0), float64(y1-y0)
	m = m.rotate(deg(math.Atan2(dy, dx)), x0, y0)
	l := float32(math.Hypot(dx, dy))
	d.rounded(m, x0-w/2, y0-w/2, l+w, w, w/2, 0, a)
}

// polar returns the point at radius r and angle a (radians clockwise
// from the right) around (cx, cy).
func polar(cx, cy, r float32, a float64) (float32, float32) {
	s, c := math.Sincos(a)
	return cx + r*float32(c), cy + r*float32(s)
}

// rad converts degrees to radians.
func rad(deg float64) float64 { return deg * math.Pi / 180 }
