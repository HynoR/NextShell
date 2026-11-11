// Command inputlatency measures how long input takes to show on screen in
// native UI on macOS: from when an event arrives to when the frame showing
// it reaches the display. It needs a desktop session.
//
//	go run ./internal/inputlatency [-mode type,scroll,drag,anim] [-runs 8] [-events 300]
//
// Each run is a process of its own with a window of native UI, shown
// without activating the app, so it takes neither the keyboard nor the
// front: typing into a text area (a key every 60 to 140 ms, each frame
// after a pause), scrolling a list of a million rows with the trackpad
// (an event every 4 to 12 ms), dragging a box (the same), or animating a
// box with no input (anim), or boxes casting wide shadows (30, -boxes),
// which keep the GPU busy (heavy). Events come
// from another goroutine through the main run loop, as the window
// server's do, at any time within the display's refresh, and go through
// the window as AppKit's (sendEvent:, scrollWheel:); an event is taken to
// arrive when it is posted, so the time the main thread was busy counts.
//
// The frame showing an event is the next one the main thread presents
// after handling it: the main thread handles events and builds frames in
// turn. Its drawable's presented handler gives when it reached the screen
// (presentedTime). The tool swizzles the presents (the command buffer's
// presentDrawable:, the drawable's present), -[CAMetalLayer nextDrawable]
// for the main thread's waits, and the surface view's mygoTick: and
// updateLayer (internal/darwin) for when frames start, and fails if they
// are gone.
//
// Each run prints the latency's mean and percentiles in milliseconds, and
// the means of what it is made of: waiting for the main thread (queue),
// handling the event (handle), from then to the frame's present (waiting
// for the display link's next tick, or the end of the run loop's pass
// after a pause, building and painting, waiting for a drawable), and from
// the present to the screen (the GPU, the window server's composite and
// the display). And of the frames: how long the main thread waited for
// drawables a frame, and how often more than 4 ms; what share of the
// refreshes showed a new frame (all of them for -mode anim, an animation
// with no input, but for frames not drawn in time); and what share of the
// frames presented never showed, replaced by the next. The summary gives the median of the runs' figures: a run
// keeps the state the window server settles in, which changes from run
// to run, so compare medians of several runs, alternating builds.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
)

func main() {
	modes := flag.String("mode", "type,scroll,drag,anim", "what the runs do: type, scroll, drag, anim, heavy, separated by commas")
	runs := flag.Int("runs", 6, "how many runs of each mode")
	events := flag.Int("events", 300, "events measured in a run, after warmup (refreshes for anim and heavy)")
	warmup := flag.Int("warmup", 30, "events not measured at the start of a run (refreshes for anim and heavy)")
	x := flag.Int("x", 0, "where the window opens, as WindowOptions.X")
	y := flag.Int("y", 0, "where the window opens, as WindowOptions.Y")
	boxes := flag.Int("boxes", 30, "how many boxes heavy draws")
	child := flag.String("run", "", "run one measurement of this mode in this process, printing its figures")
	flag.Parse()
	if *child != "" {
		if err := measure(*child, *events, *warmup, *x, *y, *boxes); err != nil {
			fmt.Fprintln(os.Stderr, "inputlatency:", err)
			os.Exit(1)
		}
		return
	}
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "inputlatency:", err)
		os.Exit(1)
	}
	for mode := range strings.SplitSeq(*modes, ",") {
		var all []figures
		for i := range *runs {
			out, err := exec.Command(self, "-run", mode, "-events", strconv.Itoa(*events), "-warmup", strconv.Itoa(*warmup),
				"-x", strconv.Itoa(*x), "-y", strconv.Itoa(*y), "-boxes", strconv.Itoa(*boxes)).Output()
			if err != nil {
				fmt.Fprintf(os.Stderr, "inputlatency: %s run %d: %v\n", mode, i+1, err)
				os.Exit(1)
			}
			f, err := parseFigures(string(out))
			if err != nil {
				fmt.Fprintf(os.Stderr, "inputlatency: %s run %d: %v\n", mode, i+1, err)
				os.Exit(1)
			}
			fmt.Printf("%-6s run %d: %s\n", mode, i+1, f)
			all = append(all, f)
		}
		fmt.Printf("%-6s median of %d runs: %s\n", mode, len(all), medianFigures(all))
	}
}

// figures are a run's, in milliseconds but for refresh (Hz) and events.
type figures struct {
	refresh, events                    float64
	mean, p50, p90, p99                float64
	queue, handle, toPresent, toScreen float64 // means
	// The frames': the main thread's wait for drawables a frame, the
	// percent of frames it waited over 4 ms, the percent of refreshes
	// showing a new frame, and of frames presented and never shown.
	mainWait, blocked, newFrames, notShown float64
}

var figureNames = []string{"refresh", "events", "mean", "p50", "p90", "p99", "queue", "handle", "toPresent", "toScreen", "mainWait", "blocked", "newFrames", "notShown"}

func (f *figures) values() []*float64 {
	return []*float64{&f.refresh, &f.events, &f.mean, &f.p50, &f.p90, &f.p99, &f.queue, &f.handle, &f.toPresent, &f.toScreen,
		&f.mainWait, &f.blocked, &f.newFrames, &f.notShown}
}

func (f figures) String() string {
	return fmt.Sprintf("mean %5.1f  p50 %5.1f  p90 %5.1f  p99 %5.1f ms | queue %3.1f  handle %3.1f  to present %4.1f  to screen %4.1f | main waits %4.2f ms a frame, %4.1f%% over 4 ms | %5.1f%% of refreshes new, %3.1f%% not shown | %.0f Hz, %.0f events",
		f.mean, f.p50, f.p90, f.p99, f.queue, f.handle, f.toPresent, f.toScreen, f.mainWait, f.blocked, f.newFrames, f.notShown, f.refresh, f.events)
}

// line is how a run hands its figures to the parent process.
func (f figures) line() string {
	var b strings.Builder
	b.WriteString("figures")
	for i, v := range f.values() {
		fmt.Fprintf(&b, " %s=%.3f", figureNames[i], *v)
	}
	return b.String()
}

func parseFigures(out string) (figures, error) {
	var f figures
	for line := range strings.SplitSeq(out, "\n") {
		rest, ok := strings.CutPrefix(line, "figures ")
		if !ok {
			continue
		}
		vals := f.values()
		for field := range strings.FieldsSeq(rest) {
			name, v, _ := strings.Cut(field, "=")
			i := slices.Index(figureNames, name)
			n, err := strconv.ParseFloat(v, 64)
			if i < 0 || err != nil {
				return f, fmt.Errorf("bad figure %q", field)
			}
			*vals[i] = n
		}
		return f, nil
	}
	return f, fmt.Errorf("no figures in %q", out)
}

func medianFigures(all []figures) figures {
	var m figures
	for i, v := range m.values() {
		var xs []float64
		for _, f := range all {
			xs = append(xs, *f.values()[i])
		}
		*v = median(xs)
	}
	return m
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	xs = slices.Clone(xs)
	slices.Sort(xs)
	if n := len(xs); n%2 == 0 {
		return (xs[n/2-1] + xs[n/2]) / 2
	}
	return xs[len(xs)/2]
}

// percentile returns the p-th of sorted xs, 0 ≤ p ≤ 1.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	return sorted[int(p*float64(len(sorted)-1))]
}
