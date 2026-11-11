package loading

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"
)

// The indicators follow loading.dev's, drawn in their SVG's viewBox or in
// fractions of their size as its CSS places them, with its keyframes and
// timing functions.

// identity is the transform that maps nothing elsewhere.
var identity = affine{a: 1, d: 1}

// spin returns the angle, in radians, that an indicator turning once a
// cycle has turned, as Options.Easing says, the other way if reverse; at
// rest, rest.
func (d *drawing) spin(reverse bool, rest float64) float64 {
	if d.still {
		return rest
	}
	return d.turn(d.at(0, 0), reverse) * 2 * math.Pi
}

// deg converts radians to degrees.
func deg(r float64) float64 { return r * 180 / math.Pi }

// center returns the middle of the indicator's square.
func (d *drawing) center() (float32, float32) { return d.x + d.s/2, d.y + d.s/2 }

var arc = spec{cycle: 800 * time.Millisecond, aspect: 1, draw: func(d *drawing) {
	a := d.spin(false, 0)
	d.stroke(d.view(24), 12, 12, 10, 2.5, a, a+18.0/10, 1)
}}

// Arc creates a single open stroke turning in a circle.
func Arc(c *ui.Context, o ...Options) ui.Element { return show(c, &arc, o) }

var atom = spec{cycle: time.Second, aspect: 1, draw: func(d *drawing) {
	cx, cy := d.center()
	r := d.s / 2
	d.hoop(identity, cx, cy, r, r-0.055*d.s, 1)
	// Each orbit, tilted a third of a half turn more than the one before,
	// is a ring turning in 3D around its diameter, seen from the front: an
	// ellipse growing and shrinking across it.
	for i := range 3 {
		a := rad(60)
		if !d.still {
			a = d.turn(d.at(i, 3), false) * 2 * math.Pi
		}
		k := float32(math.Cos(a))
		if math.Abs(float64(k)) < 1e-4 {
			continue
		}
		d.hoop(identity.rotate(float64(i)*60, cx, cy).scale(k, 1, cx, cy), cx, cy, r, r-0.045*d.s, 1)
	}
}}

// Atom creates three rings tumbling inside a circle.
func Atom(c *ui.Context, o ...Options) ui.Element { return show(c, &atom, o) }

var blocks = spec{cycle: 1300 * time.Millisecond, aspect: 1, draw: func(d *drawing) {
	cell, gap := d.s*0.8/3, d.s*0.1
	for row := range 3 {
		for col := range 3 {
			place, count := row+col, 5
			switch d.o.Sweep {
			case Rows:
				place, count = row, 3
			case Columns:
				place, count = col, 3
			}
			k := 0.8
			if !d.still {
				k = frames(d.at(place, count), easeInOut, key{0, 1}, key{0.35, 0}, key{0.7, 1}, key{1, 1})
			}
			if k <= 0 {
				continue
			}
			x, y := d.x+float32(col)*(cell+gap), d.y+float32(row)*(cell+gap)
			sk := float32(k)
			d.rounded(identity.scale(sk, sk, x+cell/2, y+cell/2), x, y, cell, cell, 0.0625*d.s, 0, 1)
		}
	}
}}

// Blocks creates nine blocks shrinking and growing in a sweep across a
// grid, in the order Options.Sweep says.
func Blocks(c *ui.Context, o ...Options) ui.Element { return show(c, &blocks, o) }

var bouncingDots = spec{cycle: 500 * time.Millisecond, aspect: 1.15, draw: func(d *drawing) {
	dot := 0.25 * d.s
	for i := range 3 {
		// Up and down on alternate cycles.
		y := 0.0
		if !d.still {
			t := d.cycles + float64(3-i)/3
			n := math.Floor(t)
			p := t - n
			if int64(n)%2 == 1 {
				p = 1 - p
			}
			y = 0.28 - easeInOut(p)
		}
		cx := d.x + dot/2 + float32(i)*0.45*d.s
		d.dot(identity, cx, d.y+d.s/2+float32(y)*dot, dot/2, 1)
	}
}}

// BouncingDots creates three staggered dots bouncing up and down, in a
// row 1.15 times as wide as it is high.
func BouncingDots(c *ui.Context, o ...Options) ui.Element { return show(c, &bouncingDots, o) }

var cascade = spec{cycle: 1500 * time.Millisecond, aspect: 1, draw: func(d *drawing) {
	for i, r := range [3]float32{10.5, 7, 3.5} {
		a := 0.0
		if !d.still {
			a = overshoot(d.at(i, 24)) * 2 * math.Pi
		}
		d.stroke(d.view(24), 12, 12, r, 2, a, a+math.Pi/2, 1)
	}
}}

// Cascade creates three nested arcs fanning into a spiral, then snapping
// back in line.
func Cascade(c *ui.Context, o ...Options) ui.Element { return show(c, &cascade, o) }

// eightDots are Circular dots' dots, in a viewBox of 16.
var eightDots = [8][2]float32{
	{8, 1.5}, {12.5962, 3.4038}, {14.5, 8}, {12.5962, 12.5962},
	{8, 14.5}, {3.4038, 12.5962}, {1.5, 8}, {3.4038, 3.4038},
}

var circularDots = spec{cycle: 800 * time.Millisecond, aspect: 1, draw: func(d *drawing) {
	for i, at := range eightDots {
		d.dot(d.view(16), at[0], at[1], 1.5, d.fade(i, 8, 0.2, 0.6))
	}
}}

// CircularDots creates eight dots in a ring, the brightest going around.
func CircularDots(c *ui.Context, o ...Options) ui.Element { return show(c, &circularDots, o) }

var classic = spec{cycle: 1200 * time.Millisecond, aspect: 1, draw: func(d *drawing) {
	// The bars are placed as loading.dev's CSS places them: around a point
	// a little right of the middle.
	cx, cy := d.x+0.52*d.s, d.y+0.501*d.s
	w, h := 0.24*d.s, 0.08*d.s
	for i := range 12 {
		m := identity.rotate(float64(i)*30, cx, cy).move(1.46*w, 0)
		d.rounded(m, cx-w/2, cy-h/2, w, h, min(6, h/2), 0, d.fade(i, 12, 0.15, 0.5))
	}
}}

// Classic creates twelve fading bars in a radial pattern.
func Classic(c *ui.Context, o ...Options) ui.Element { return show(c, &classic, o) }

var classicV2 = spec{cycle: 800 * time.Millisecond, aspect: 1, draw: func(d *drawing) {
	for i := range 8 {
		d.capsule(d.view(16).rotate(float64(i)*45, 8, 8), 8, 2.0/3, 8, 10.0/3, 4.0/3, d.fade(i, 8, 0.4, 0.5))
	}
}}

// ClassicV2 creates eight ticks in a ring, lit in turn.
func ClassicV2(c *ui.Context, o ...Options) ui.Element { return show(c, &classicV2, o) }

var clock = spec{cycle: 1200 * time.Millisecond, aspect: 1, draw: func(d *drawing) {
	v := d.view(16)
	d.dot(v, 8, 8, 8, 0.1)
	d.turned(v, 8, 8, d.spin(false, 0), d.hand)
}}

// hand draws Clock's hand through v, in its viewBox of 16, pointing up.
func (d *drawing) hand(v affine) {
	s := d.begin(v)
	s.move(11.1937, 2.92061)
	s.cube(10.5206, 2.49739, 9.77304, 2.21397, 8.9954, 2.08314)
	s.cube(8.45076, 1.99151, 8, 2.44772, 8, 3)
	s.line(8, 7.20324)
	s.cube(8, 7.49211, 8.12492, 7.76686, 8.34259, 7.95677)
	s.line(11.8999, 11.0603)
	s.cube(12.3287, 11.4344, 12.9913, 11.378, 13.2643, 10.8787)
	s.cube(13.6356, 10.1998, 13.8736, 9.45248, 13.9617, 8.67713)
	s.cube(14.0892, 7.55433, 13.8971, 6.41834, 13.4074, 5.39993)
	s.cube(12.9177, 4.38152, 12.1503, 3.5221, 11.1937, 2.92061)
	s.close()
	d.fill(1)
}

// Clock creates a clock hand sweeping around a faint face.
func Clock(c *ui.Context, o ...Options) ui.Element { return show(c, &clock, o) }

var comet = spec{cycle: 700 * time.Millisecond, aspect: 1, draw: func(d *drawing) {
	cx, cy := d.center()
	r, w := d.s/2, 0.12*d.s
	a := deg(d.spin(false, 0))
	// The tail, a ring fading in from transparent around a turn, and the
	// head at its end.
	s := d.begin(identity).ring(cx, cy, r, r-w)
	d.p.FillPathConicGradient(&s.Path, ui.ConicGradient{Angle: float32(a), Stops: []ui.GradientStop{
		{Color: d.alpha(0), At: 0}, {Color: d.color, At: 1},
	}})
	d.dot(identity.rotate(a, cx, cy), cx, d.y+w/2, w/2, 1)
}}

// Comet creates a full ring fading into its tail.
func Comet(c *ui.Context, o ...Options) ui.Element { return show(c, &comet, o) }

var compass = spec{cycle: 500 * time.Millisecond, aspect: 1, draw: func(d *drawing) {
	a := 0.0
	if !d.still {
		a = 90 * easeInOut(d.at(0, 0))
	}
	for i := range 4 {
		d.capsule(d.view(16).rotate(a+float64(i)*90, 8, 8), 8, 2.0/3, 8, 10.0/3, 4.0/3, 1)
	}
}}

// Compass creates four ticks turning a quarter turn at a time.
func Compass(c *ui.Context, o ...Options) ui.Element { return show(c, &compass, o) }

var dual = spec{cycle: time.Second, aspect: 1, draw: func(d *drawing) {
	v := d.view(24)
	a := d.spin(false, 0)
	d.stroke(v, 12, 12, 10, 2.5, a, a+18.0/10, 1)
	b := d.spin(true, math.Pi)
	d.stroke(v, 12, 12, 5.5, 2.5, b, b+10/5.5, 1)
}}

// Dual creates two arcs turning in opposite directions.
func Dual(c *ui.Context, o ...Options) ui.Element { return show(c, &dual, o) }

var eclipse = spec{cycle: 1200 * time.Millisecond, aspect: 1, draw: func(d *drawing) {
	cx, cy := d.center()
	dot := 0.4 * d.s
	for i := range 2 {
		// Across and back, nearer (larger) on the way there and farther
		// on the way back.
		x, a, k := float64(i)*1.5-0.75, 1.0, 1.0
		if !d.still {
			p := d.at(i, 2)
			x = frames(p, easeInOut, key{0, 0.75}, key{0.5, -0.75}, key{1, 0.75})
			a = frames(p, easeInOut, key{0, 0.75}, key{0.25, 1}, key{0.5, 0.75}, key{0.75, 0.5}, key{1, 0.75})
			k = frames(p, easeInOut, key{0, 1}, key{0.25, 1.3}, key{0.5, 1}, key{0.75, 0.7}, key{1, 1})
		}
		d.dot(identity, cx+float32(x)*dot, cy, dot/2*float32(k), a)
	}
}}

// Eclipse creates two dots trading places, one passing behind the other.
func Eclipse(c *ui.Context, o ...Options) ui.Element { return show(c, &eclipse, o) }

var flip = spec{cycle: 1200 * time.Millisecond, aspect: 1, draw: func(d *drawing) {
	cx, cy := d.center()
	half, r := 0.32*d.s, 0.06*d.s
	ax, ay := 0.0, 0.0
	if !d.still {
		p := d.at(0, 0)
		ax = frames(p, easeInOut, key{0, 0}, key{0.5, -180}, key{1, -180})
		ay = frames(p, easeInOut, key{0, 0}, key{0.5, 0}, key{1, -180})
	}
	// The square turned in 3D, around its horizontal axis and then its
	// vertical one (CSS's rotateX(ax) rotateY(ay)), seen in perspective
	// from three times the indicator's size.
	t := ui.Translation(-half, -half).Then(ui.RotationY(float32(ay))).Then(ui.RotationX(float32(ax))).
		Then(ui.Perspective(3 * d.s)).Then(ui.Translation(cx, cy))
	d.roundedIn(t, 2*half, 2*half, r, 0, 1)
}}

// Flip creates a square flipping over one axis, then the other.
func Flip(c *ui.Context, o ...Options) ui.Element { return show(c, &flip, o) }

var gather = spec{cycle: 1600 * time.Millisecond, aspect: 1, draw: func(d *drawing) {
	cx, cy := d.center()
	b, gap := 0.38*d.s, 0.24*d.s
	pull, turn := 0.0, 0.0
	if !d.still {
		p := d.at(0, 0)
		pull = frames(p, easeInOut, key{0, 0}, key{0.3, 1}, key{0.6, 1}, key{1, 0})
		turn = frames(p, easeInOut, key{0, 0}, key{0.3, 0}, key{0.6, 90}, key{1, 90})
	}
	// Each block pulls in toward the middle by a fifth of its size.
	in := float32(pull) * 0.211 * b
	m := identity.rotate(turn, cx, cy)
	for i := range 4 {
		col, row := float32(i%2), float32(i/2)
		x := d.x + col*(b+gap) + (1-2*col)*in
		y := d.y + row*(b+gap) + (1-2*row)*in
		d.rounded(m, x, y, b, b, 0.14*d.s, 0, 1)
	}
}}

// Gather creates four blocks pulling together, turning, and pushing
// apart.
func Gather(c *ui.Context, o ...Options) ui.Element { return show(c, &gather, o) }

var leap = spec{cycle: 1800 * time.Millisecond, aspect: 1, draw: func(d *drawing) {
	// Whole DIPs, as loading.dev rounds them to pixels.
	round := func(v float32) float32 { return float32(math.Round(float64(v))) }
	dot := round(0.22 * d.s)
	gap := float32(math.Floor(float64(d.s-dot) / 2))
	top := round((d.s - dot) / 2)
	left := d.s - dot - 2*gap
	// The middle of the stretch a dot leaps over.
	mx, my := d.x+left+gap+dot/2, d.y+top+dot/2
	for i := range 3 {
		// The last dot leaps over the others to the front, as they slide
		// back a place each.
		x, a := float64(i)*float64(gap), 0.0
		if !d.still {
			p := d.at(i, 3)
			g := float64(gap)
			x = frames(p, easeInOut, key{0, 0}, key{0.3333, 0}, key{0.6666, -g}, key{1, -2 * g})
			a = frames(p, easeInOut, key{0, 0}, key{0.3333, 180}, key{0.6666, 180}, key{1, 180})
		}
		px, py := polar(mx+float32(x), my, gap, rad(a+180))
		d.dot(identity, px, py, dot/2, 1)
	}
}}

// Leap creates three dots in a row, the last one leaping to the front.
func Leap(c *ui.Context, o ...Options) ui.Element { return show(c, &leap, o) }

var linearDots = spec{cycle: 900 * time.Millisecond, aspect: 0.9375, draw: func(d *drawing) {
	dot := 0.1875 * d.s
	for i := range 3 {
		a := 0.75
		if !d.still {
			a = frames(d.at(i, 3), linearEase, key{0, 1}, key{0.6667, 0.5}, key{1, 1})
		}
		d.dot(identity, d.x+dot/2+float32(i)*2*dot, d.y+d.s/2, dot/2, a)
	}
}}

// LinearDots creates three dots lighting up in turn from left to right,
// in a row nearly as wide as it is high.
func LinearDots(c *ui.Context, o ...Options) ui.Element { return show(c, &linearDots, o) }

// segments are where the Loading mark's segments are, in a viewBox of 15.
var segments = [8][2]float32{{12, 6}, {10, 10}, {6, 12}, {2, 10}, {0, 6}, {2, 2}, {6, 0}, {10, 2}}

var loadingMark = spec{cycle: time.Second, aspect: 1, draw: func(d *drawing) {
	v := d.view(15)
	for i, at := range segments {
		a := d.fade(i, 8, 0.2, 0.6)
		for _, b := range [4][2]float32{{0, 0}, {2, 0}, {0, 2}, {2, 2}} {
			d.rounded(v, at[0]+b[0], at[1]+b[1], 1, 1, 0, 0, a)
		}
	}
}}

// Loading creates the loading.dev mark, its brightest segment circling
// the ring.
func Loading(c *ui.Context, o ...Options) ui.Element { return show(c, &loadingMark, o) }

var morph = spec{cycle: 1200 * time.Millisecond, aspect: 1, draw: func(d *drawing) {
	cx, cy := d.center()
	side := 0.64 * d.s
	r, a := 0.08*float64(d.s), 0.0
	if !d.still {
		p := d.at(0, 0)
		r = frames(p, easeInOut, key{0, r}, key{0.5, float64(side) / 2}, key{1, r})
		a = frames(p, easeInOut, key{0, 0}, key{0.5, 45}, key{1, 90})
	}
	d.rounded(identity.rotate(a, cx, cy), cx-side/2, cy-side/2, side, side, float32(r), 0, 1)
}}

// Morph creates a square rounding into a circle and back as it turns.
func Morph(c *ui.Context, o ...Options) ui.Element { return show(c, &morph, o) }

var orbit = spec{cycle: 750 * time.Millisecond, aspect: 1, draw: func(d *drawing) {
	cx, cy := d.center()
	d.dot(identity, cx, cy, 0.125*d.s, 1)
	// Half a ring, from transparent at its start to opaque at its end.
	r, w := 0.42*d.s, 0.0625*d.s
	d.turned(identity, cx, cy, d.spin(false, 0), func(m affine) {
		a0, a1 := math.Pi/2, 3*math.Pi/2
		s := d.begin(m).arc(cx, cy, r, a0, a1, true).arc(cx, cy, r-w, a1, a0, false).close()
		// Empty subpaths at the corners of the whole ring's box, which the
		// gradient turns around the middle of.
		s.move(cx-r, cy-r).line(cx-r, cy-r).close().move(cx+r, cy+r).line(cx+r, cy+r).close()
		d.p.FillPathConicGradient(&s.Path, ui.ConicGradient{Angle: 180, Stops: []ui.GradientStop{
			{Color: d.alpha(0), At: 0}, {Color: d.color, At: 0.5}, {Color: d.color, At: 1},
		}})
	})
}}

// Orbit creates a fading half ring turning around a dot.
func Orbit(c *ui.Context, o ...Options) ui.Element { return show(c, &orbit, o) }

var pulse = spec{cycle: 1200 * time.Millisecond, aspect: 1, draw: func(d *drawing) {
	v := d.view(16)
	k, a := 1.0, 0.2
	if !d.still {
		e := easeOut(d.at(0, 0))
		k, a = 0.25+0.75*e, 0.4*(1-e)
	}
	d.dot(v, 8, 8, 8*float32(k), a)
	d.dot(v, 8, 8, 2, 1)
}}

// Pulse creates a ring rippling outward from a dot.
func Pulse(c *ui.Context, o ...Options) ui.Element { return show(c, &pulse, o) }

var radar = spec{cycle: 1500 * time.Millisecond, aspect: 1, draw: func(d *drawing) {
	v := d.view(16)
	d.dot(v, 8, 8, 8, 0.2)
	d.hoop(v, 8, 8, 6, 5, 0.2)
	d.turned(v, 8, 8, d.spin(false, 0), d.beam)
	d.dot(v, 8, 8, 2, 1)
}}

// beam draws Radar's beam through m, in its viewBox of 16: a sector of
// the ring between radii 3 and 8, from the top to its leading edge,
// opaque inside a third of the radius and fading to transparent at its
// rim, as loading.dev's radial gradient.
func (d *drawing) beam(m affine) {
	const in, out, solid = 3, 8, 8 * 0.3334
	// The leading edge, from the inner circle to the outer one.
	ix, iy, ox, oy := 10.4707-8, 9.69922-8, 14.249-8, 12.9951-8
	opacity := func(r float64) float64 { return max(0, min(1, 1-(r-solid)/(out-solid))) }
	// Sectors of growing radii over each other, each as opaque as makes
	// the ring between its radius and the one before the gradient's
	// opacity there: their edges inside the beam meet no other edge, as
	// bands side by side would.
	n := int(max(6, min(40, math.Ceil(float64(out-in)*float64(m.unit())/1.5))))
	band := func(k int) float64 { return in + (out-in)*(float64(k)-0.5)/float64(n) }
	for k := 1; k <= n; k++ {
		inside, beyond := opacity(band(k)), 0.0
		if k < n {
			beyond = opacity(band(k + 1))
		}
		a := 1 - (1-inside)/(1-beyond)
		r := in + (out-in)*float64(k)/float64(n)
		// Where the leading edge crosses the circle of radius r.
		dx, dy := ox-ix, oy-iy
		qa, qb, qc := dx*dx+dy*dy, 2*(ix*dx+iy*dy), ix*ix+iy*iy-r*r
		u := max(0, min(1, (-qb+math.Sqrt(max(0, qb*qb-4*qa*qc)))/(2*qa)))
		end := math.Atan2(iy+u*dy, ix+u*dx)
		s := d.begin(m)
		s.move(8, 8-in).line(8, 8-float32(r))
		s.arc(8, 8, float32(r), -math.Pi/2, end, false)
		s.line(float32(8+ix), float32(8+iy))
		s.arc(8, 8, in, math.Atan2(iy, ix), -math.Pi/2, false).close()
		d.fill(a)
	}
}

// Radar creates a fading beam sweeping around a faint dish.
func Radar(c *ui.Context, o ...Options) ui.Element { return show(c, &radar, o) }

var ring = spec{cycle: 800 * time.Millisecond, aspect: 1, draw: func(d *drawing) {
	v := d.view(24)
	d.hoop(v, 12, 12, 11.25, 8.75, 0.2)
	a := d.spin(false, 0)
	d.stroke(v, 12, 12, 10, 2.5, a, a+16.0/10, 1)
}}

// Ring creates an arc turning in a faint circle.
func Ring(c *ui.Context, o ...Options) ui.Element { return show(c, &ring, o) }

var ripple = spec{cycle: 1200 * time.Millisecond, aspect: 1, draw: func(d *drawing) {
	cx, cy := d.center()
	for i := range 3 {
		k, a := float64(i+1)/3, 0.4
		if !d.still {
			p := d.at(i, 3)
			if d.o.Direction == RippleIn {
				p = 1 - p
			}
			e := easeOut(p)
			k, a = e, 1-e
		}
		if k <= 0 {
			continue
		}
		r := d.s / 2 * float32(k)
		d.hoop(identity, cx, cy, r, r-0.08*d.s*float32(k), a)
	}
}}

// Ripple creates three rings spreading out from the center, or drawing in
// to it, as Options.Direction says.
func Ripple(c *ui.Context, o ...Options) ui.Element { return show(c, &ripple, o) }

var slide = spec{cycle: 2400 * time.Millisecond, aspect: 1, draw: func(d *drawing) {
	dot := 0.34 * d.s
	far := 1.588 * float64(dot)
	rest := [3][2]float64{{far, 0}, {0, 0}, {0, far}}
	for i := range 3 {
		// Around the square's corners, to the one empty, and waiting
		// there for the others.
		x, y := rest[i][0], rest[i][1]
		if !d.still {
			p := d.at(i, 3)
			x = frames(p, easeInOut, key{0, far}, key{0.0833, far}, key{0.25, far}, key{0.3333, 0}, key{0.5, 0},
				key{0.5833, 0}, key{0.75, 0}, key{0.8333, far}, key{1, far})
			y = frames(p, easeInOut, key{0, 0}, key{0.0833, far}, key{0.25, far}, key{0.3333, far}, key{0.5, far},
				key{0.5833, 0}, key{0.75, 0}, key{0.8333, 0}, key{1, 0})
		}
		d.dot(identity, d.x+0.06*d.s+dot/2+float32(x), d.y+0.06*d.s+dot/2+float32(y), dot/2, 1)
	}
}}

// Slide creates three dots sliding into the empty corner of a square.
func Slide(c *ui.Context, o ...Options) ui.Element { return show(c, &slide, o) }

var snake = spec{cycle: 1400 * time.Millisecond, aspect: 1, draw: func(d *drawing) {
	v := d.view(24)
	a := d.spin(false, 0)
	// A dash stretching to 45 long, then going on at that length, as its
	// start catches up with its end.
	dash, offset := 18.0, 0.0
	if !d.still {
		p := d.at(0, 0)
		dash = frames(p, easeInOut, key{0, 1}, key{0.5, 45}, key{1, 45})
		offset = frames(p, easeInOut, key{0, 0}, key{0.5, -17}, key{1, -62})
	}
	dashes(2*math.Pi*10, dash, 100, offset, func(from, to float64) {
		d.stroke(v, 12, 12, 10, 2.5, a+from/10, a+to/10, 1)
	})
}}

// Snake creates an arc stretching and shrinking as it circles.
func Snake(c *ui.Context, o ...Options) ui.Element { return show(c, &snake, o) }

// swirlSteps are the order of Swirl's cells, by rows, around the grid,
// -1 for its empty middle.
var swirlSteps = [9]int{0, 1, 2, 7, -1, 3, 6, 5, 4}

var swirl = spec{cycle: 1200 * time.Millisecond, aspect: 1, draw: func(d *drawing) {
	pad, gap := 0.0625*d.s, 0.15625*d.s
	cell := (d.s - 2*pad - 2*gap) / 3
	for i, step := range swirlSteps {
		if step < 0 {
			continue
		}
		x, y := d.x+pad+float32(i%3)*(cell+gap), d.y+pad+float32(i/3)*(cell+gap)
		d.rounded(identity, x, y, cell, cell, 0.0625*d.s, 0, d.fade(step, 8, 0.2, 0.6))
	}
}}

// Swirl creates a bright cell chasing its trail around a square.
func Swirl(c *ui.Context, o ...Options) ui.Element { return show(c, &swirl, o) }

// Trace's rounded square, in its viewBox of 20, and its dash.
const (
	traceAt, traceSide, traceRadius, traceWidth = 1.25, 17.5, 4, 2.5
	traceDash                                   = 16
)

// tracePerimeter is the length of Trace's rounded square.
var tracePerimeter = 4*(traceSide-2*traceRadius) + 2*math.Pi*traceRadius

var trace = spec{cycle: 1200 * time.Millisecond, aspect: 1, draw: func(d *drawing) {
	v := d.view(20)
	const out = traceWidth / 2
	d.rounded(v, traceAt-out, traceAt-out, traceSide+2*out, traceSide+2*out, traceRadius+out, traceWidth, 0.2)
	at := 0.0
	if !d.still {
		at = d.turn(d.at(0, 0), false) * tracePerimeter
	}
	dashes(tracePerimeter, traceDash, tracePerimeter-traceDash, -at, func(from, to float64) {
		d.traceDash(v, from, to)
	})
}}

// traceDash draws Trace's dash from from to to along its rounded square,
// through v.
func (d *drawing) traceDash(v affine, from, to float64) {
	// along returns the point at length u along the square, from the
	// start of its top side, as SVG's rect starts, and the direction it
	// goes there, in radians.
	const straight = traceSide - 2*traceRadius
	corner := math.Pi / 2 * traceRadius
	along := func(u float64) (x, y, dir float64) {
		side := min(3, int(u/(straight+corner)))
		u -= float64(side) * (straight + corner)
		dir = float64(side) * math.Pi / 2
		sin, cos := math.Sincos(dir)
		// Where the side starts: the top's after the top left corner.
		starts := [4][2]float64{
			{traceAt + traceRadius, traceAt}, {traceAt + traceSide, traceAt + traceRadius},
			{traceAt + traceSide - traceRadius, traceAt + traceSide}, {traceAt, traceAt + traceSide - traceRadius},
		}
		x, y = starts[side][0], starts[side][1]
		if u <= straight {
			return x + u*cos, y + u*sin, dir
		}
		// Around the corner, whose center is inward of the side's end.
		x, y = x+straight*cos-traceRadius*sin, y+straight*sin+traceRadius*cos
		a := dir - math.Pi/2 + (u-straight)/traceRadius
		sa, ca := math.Sincos(a)
		return x + traceRadius*ca, y + traceRadius*sa, a + math.Pi/2
	}
	n := int(math.Ceil((to-from)/0.4)) + 1
	s := d.begin(v)
	if d.o.Cap == FlatCap {
		// The dash's outline: one edge there, the other back.
		for i := range 2 * n {
			j, side := i, -1.0
			if i >= n {
				j, side = 2*n-1-i, 1
			}
			x, y, dir := along(from + (to-from)*float64(j)/float64(n-1))
			sin, cos := math.Sincos(dir)
			px, py := float32(x-side*traceWidth/2*sin), float32(y+side*traceWidth/2*cos)
			if i == 0 {
				s.move(px, py)
			} else {
				s.line(px, py)
			}
		}
		s.close()
		d.fill(1)
		return
	}
	for i := range n {
		x, y, _ := along(from + (to-from)*float64(i)/float64(n-1))
		if i == 0 {
			s.move(float32(x), float32(y))
		} else {
			s.line(float32(x), float32(y))
		}
	}
	d.p.StrokePath(&s.Path, traceWidth*v.unit(), d.color)
}

// Trace creates a dash tracing the outline of a rounded square.
func Trace(c *ui.Context, o ...Options) ui.Element { return show(c, &trace, o) }

var wave = spec{cycle: 900 * time.Millisecond, aspect: 1, draw: func(d *drawing) {
	w := 0.12 * d.s
	for i := range 5 {
		h := 0.4 + 0.15*float64(i)
		if !d.still {
			h = frames(d.at(i, 5), easeInOut, key{0, 0.3}, key{0.5, 1}, key{1, 0.3})
		}
		bh := float32(h) * d.s
		y := d.y + (d.s-bh)/2
		if d.o.Origin == WaveBottom {
			y = d.y + d.s - bh
		}
		d.rounded(identity, d.x+float32(i)*(d.s-w)/4, y, w, bh, w/2, 0, 1)
	}
}}

// Wave creates five bars rising and falling in a wave, from their middle
// or their bottom as Options.Origin says.
func Wave(c *ui.Context, o ...Options) ui.Element { return show(c, &wave, o) }
