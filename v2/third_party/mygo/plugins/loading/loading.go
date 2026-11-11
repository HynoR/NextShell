// Package loading has the 29 loading indicators of loading.dev
// (https://loading.dev, Jakub Krehel's loading-dev for React) for native
// UI: arcs, dots, bars and blocks that MyGo draws itself, each a ui
// element that moves as long as it shows.
//
//	loading.Arc(c)
//	loading.Ring(c, loading.Options{Size: 32, Color: t.Accent, Easing: loading.EaseInOut})
//	ui.Row(c).Gap(8).Children(func() {
//		loading.BouncingDots(c, loading.Options{Size: 12})
//		ui.Text(c, "Thinking")
//	})
//
// An indicator is Options.Size DIPs high, 20 unless set, and as wide, or
// wider for a row of dots; the element's own size, as Size or Fill set it,
// scales it. It draws in the theme's text color unless Options.Color says
// otherwise, and runs at loading.dev's pace unless Options.Duration says
// otherwise. Where the desktop asks for less motion
// (ui.Preferences.ReduceMotion) it stands still, in loading.dev's pose for
// reduced motion; Options.Paused stops it where it is.
//
// Assistive technology sees a progress indicator of unknown length; name
// it with Label.
package loading

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// Options are the settings of an indicator, each meaning loading.dev's
// default when zero.
type Options struct {
	// Size is the indicator's height in DIPs, 20 if 0. Its width is the
	// same, except for BouncingDots's and LinearDots's rows of dots.
	Size float32
	// Color is what the indicator draws in, the theme's Text if zero.
	Color ui.Color
	// Duration is the time of one cycle of the indicator's motion, its
	// own (as loading.dev's) if 0.
	Duration time.Duration
	// Paused stops the indicator where it is, and it goes on from there
	// once false again, as CSS's paused animation-play-state.
	Paused bool
	// Easing is how the indicators that turn (Arc, Atom, Clock, Comet,
	// Dual, Orbit, Radar, Ring, Snake and Trace) turn: Linear by default.
	Easing Easing
	// Cap is the end of the strokes of Arc, Cascade, Dual, Ring, Snake
	// and Trace: RoundCap by default.
	Cap Cap
	// Sweep is the order in which Blocks's blocks shrink: Diagonal by
	// default.
	Sweep Sweep
	// Direction is where Ripple's rings go: RippleOut by default.
	Direction RippleDirection
	// Origin is where Wave's bars grow from: WaveCenter by default.
	Origin WaveOrigin
}

// Easing is how an indicator that turns moves through a turn.
type Easing uint8

const (
	// Linear turns at one speed.
	Linear Easing = iota
	// EaseInOut starts and ends each turn slowly, as CSS's ease-in-out.
	EaseInOut
	// Stacked adds a turn at one speed and one easing in and out: two
	// turns a cycle, fast in its middle.
	Stacked
)

// Cap is how a stroke ends.
type Cap uint8

const (
	// RoundCap ends a stroke in a half circle.
	RoundCap Cap = iota
	// FlatCap ends a stroke square, where it ends.
	FlatCap
)

// Sweep is the order of Blocks's blocks.
type Sweep uint8

const (
	// Diagonal sweeps from the top left corner to the bottom right one.
	Diagonal Sweep = iota
	// Rows sweeps from the top row down.
	Rows
	// Columns sweeps from the left column right.
	Columns
)

// RippleDirection is where Ripple's rings go.
type RippleDirection uint8

const (
	// RippleOut spreads the rings out from the center.
	RippleOut RippleDirection = iota
	// RippleIn draws the rings in to the center.
	RippleIn
)

// WaveOrigin is where Wave's bars grow from.
type WaveOrigin uint8

const (
	// WaveCenter grows the bars up and down from their middle.
	WaveCenter WaveOrigin = iota
	// WaveBottom grows the bars up from the bottom.
	WaveBottom
)

// DefaultSize is an indicator's height when Options.Size is 0.
const DefaultSize = 20

// A Spinner is one of the indicators, by name, as a gallery lists them.
type Spinner struct {
	// Name is the indicator's name, as loading.dev's: "Arc", "Bouncing dots".
	Name string
	// New creates the indicator, as the function of its name does.
	New func(c *ui.Context, o ...Options) ui.Element
}

// All returns the indicators, by name.
func All() []Spinner {
	return []Spinner{
		{"Arc", Arc}, {"Atom", Atom}, {"Blocks", Blocks}, {"Bouncing dots", BouncingDots},
		{"Cascade", Cascade}, {"Circular dots", CircularDots}, {"Classic", Classic},
		{"Classic v2", ClassicV2}, {"Clock", Clock}, {"Comet", Comet}, {"Compass", Compass},
		{"Dual", Dual}, {"Eclipse", Eclipse}, {"Flip", Flip}, {"Gather", Gather}, {"Leap", Leap},
		{"Linear dots", LinearDots}, {"Loading", Loading}, {"Morph", Morph}, {"Orbit", Orbit},
		{"Pulse", Pulse}, {"Radar", Radar}, {"Ring", Ring}, {"Ripple", Ripple}, {"Slide", Slide},
		{"Snake", Snake}, {"Swirl", Swirl}, {"Trace", Trace}, {"Wave", Wave},
	}
}

// spec is what an indicator is: how long its cycle is, how wide it is for
// its height, and how it draws.
type spec struct {
	cycle  time.Duration
	aspect float32
	draw   func(d *drawing)
}

// timerKey keys an indicator's timer in its element.
type timerKey struct{}

// timer is how long an indicator has run: since start, or held while
// paused.
type timer struct {
	start  time.Time
	held   time.Duration
	paused bool
	// sh is the shape the indicator draws with, its storage kept from
	// frame to frame.
	sh shape
}

// show creates an indicator of s with the options given, at most one.
func show(c *ui.Context, s *spec, opts []Options) ui.Element {
	if len(opts) > 1 {
		panic("loading: at most one Options")
	}
	var o Options
	if len(opts) == 1 {
		o = opts[0]
	}
	size := o.Size
	if size <= 0 {
		size = DefaultSize
	}
	color := o.Color
	if color == (ui.Color{}) {
		color = c.Theme().Text
	}
	cycle := s.cycle
	if o.Duration > 0 {
		cycle = o.Duration
	}
	still := c.Preferences().ReduceMotion
	// A progress indicator of unknown length, as ui.Spinner.
	e := ui.Box(c).Size(size*s.aspect, size).Shrink(0).Role(ui.RoleProgress).Range(0, 1, -1)
	now := c.Now()
	k := ui.Local(e, timerKey{}, func() timer { return timer{start: now} })
	switch {
	case o.Paused && !k.paused:
		k.held, k.paused = now.Sub(k.start), true
	case !o.Paused && k.paused:
		k.start, k.paused = now.Add(-k.held), false
	}
	e.Draw(func(p *ui.Painter, r ui.Rect) {
		d := drawing{p: p, sh: &k.sh, color: color, o: o, still: still}
		d.s = min(r.H, r.W/s.aspect)
		d.x, d.y = r.X+(r.W-d.s*s.aspect)/2, r.Y+(r.H-d.s)/2
		if !still {
			ran := k.held
			if !k.paused {
				ran = p.Now().Sub(k.start)
				p.AnimationFrame()
			}
			d.cycles = float64(ran) / float64(cycle)
		}
		s.draw(&d)
	})
	return e
}
