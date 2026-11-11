//go:build windows

package d3d11

import (
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/egoist/mygo/internal/gpu/d3d11/device"
)

// The swap chain's frame latency object keeps one frame waiting ahead of
// the screen without Present blocking the main thread: DXGI signals it as
// the screen takes a frame, and a frame presents only once a wait on it
// succeeded. A frame drawn when it is not signaled yet presents later
// (pending), as a goroutine waiting on it has the main thread present it,
// rather than block in Present until the screen takes the last; frames
// paced by the display wait meanwhile (Ready), as they would have. Every
// present takes one wait's success, its token, so the object's count
// stays that of the frames the screen can take.
//
// All that keeps a frame a refresh or more from the screen, built ahead and
// queued. So frames that the last ones show will make the refresh without
// (frameCost.fits) build once the window manager composed the last (DwmFlush) and
// the screen has room, and present at once, with sync interval 0, to show at its
// next composition: none queues, and input shows about a refresh sooner. Heavier
// frames build ahead, as the one the GPU draws meanwhile leaves the main thread
// free to build the next: they keep the display's pace, a refresh later.

// frameCost estimates what a frame costs from when it may start to when the GPU
// has drawn it, by the time the main thread took to draw it and the GPU's
// timestamps of it, which come in a frame or so later.
type frameCost struct {
	// queries holds the timestamps of the last frames: next is where the next
	// frame's go, and each is busy until it was read.
	queries [4]frameQuery
	next    int
	// start is when the frame being built could start, and estimate the costs
	// of the last frames, the highest held and fading.
	start    time.Time
	estimate time.Duration
	// period is the display's refresh period.
	period time.Duration
}

type frameQuery struct {
	disjoint, t0, t1 uintptr
	busy             bool
	// main is the main thread's part of the frame, or -1 when no Ready started
	// it, as a resize's frame drawn at once: its cost is not known.
	main time.Duration
}

// A frame that takes more of a refresh than fitShare may miss it when building
// late; its estimate fades by costFade a frame.
const (
	fitShare = 3.0 / 4
	costFade = 0.95
)

// fits reports whether frames take little enough of a refresh to build once the
// screen has room for them.
func (c *frameCost) fits() bool {
	return float64(c.estimate) < fitShare*float64(c.refreshPeriod())
}

// refreshPeriod returns the display's refresh period, a 60 Hz one when it is not
// known.
func (c *frameCost) refreshPeriod() time.Duration {
	if c.period == 0 {
		return minRefresh
	}
	return c.period
}

// makeQueries makes the queries, reporting whether it could: frames otherwise
// count as not fitting.
func (c *frameCost) makeQueries(device uintptr) bool {
	const queryTimestamp, queryTimestampDisjoint = 2, 3
	make1 := func(kind uint32, out *uintptr) bool {
		desc := [2]uint32{kind, 0}
		return !failed(call(device, devCreateQuery, uintptr(unsafe.Pointer(&desc)), uintptr(unsafe.Pointer(out))))
	}
	for i := range c.queries {
		q := &c.queries[i]
		if !make1(queryTimestampDisjoint, &q.disjoint) || !make1(queryTimestamp, &q.t0) || !make1(queryTimestamp, &q.t1) {
			c.release()
			return false
		}
	}
	return true
}

func (c *frameCost) release() {
	for i := range c.queries {
		q := &c.queries[i]
		free(&q.t1)
		free(&q.t0)
		free(&q.disjoint)
		q.busy = false
	}
}

// begin notes that the GPU starts drawing a frame, unless all queries wait to be
// read, which skips timing it. It comes after the frame's clear: the GPU reaches
// that once the window manager gave the buffer back, which is no work of the
// frame's, up to a refresh after the last.
func (c *frameCost) begin(ctx uintptr) *frameQuery {
	q := &c.queries[c.next]
	if q.disjoint == 0 || q.busy {
		return nil
	}
	call(ctx, ctxBegin, q.disjoint)
	call(ctx, ctxEnd, q.t0)
	return q
}

// end notes that the GPU finishes drawing the frame of q and, for its main
// thread's part, that it took since start, which only this frame takes.
func (c *frameCost) end(ctx uintptr, q *frameQuery) {
	call(ctx, ctxEnd, q.t1)
	call(ctx, ctxEnd, q.disjoint)
	q.busy = true
	q.main = c.took()
	c.next = (c.next + 1) % len(c.queries)
}

// took returns how long the main thread took on the frame since start, and
// forgets start: a frame no Ready started, as the ones a resize draws at once,
// would count all the time since the last that one did, seconds of a drag, and
// have frames build ahead long after. Its cost is not known (-1).
func (c *frameCost) took() time.Duration {
	if c.start.IsZero() {
		return -1
	}
	d := time.Since(c.start)
	c.start = time.Time{}
	return d
}

// read takes in the timestamps that came, oldest first, without waiting.
func (c *frameCost) read(ctx uintptr) {
	const doNotFlush = 1
	for range c.queries {
		// The oldest is the one after the last begun.
		q := &c.queries[c.next]
		for i := range c.queries {
			q = &c.queries[(c.next+i)%len(c.queries)]
			if q.busy {
				break
			}
		}
		if !q.busy {
			return
		}
		var clock struct {
			Frequency uint64
			Disjoint  int32
			_         int32
		}
		var t0, t1 uint64
		if call(ctx, ctxGetData, q.disjoint, uintptr(unsafe.Pointer(&clock)), unsafe.Sizeof(clock), doNotFlush) != 0 ||
			call(ctx, ctxGetData, q.t0, uintptr(unsafe.Pointer(&t0)), 8, doNotFlush) != 0 ||
			call(ctx, ctxGetData, q.t1, uintptr(unsafe.Pointer(&t1)), 8, doNotFlush) != 0 {
			return
		}
		q.busy = false
		if q.main >= 0 && clock.Disjoint == 0 && clock.Frequency != 0 && t1 >= t0 {
			c.add(q.main + time.Duration(float64(t1-t0)/float64(clock.Frequency)*float64(time.Second)))
		}
	}
}

// add takes in what a frame cost.
func (c *frameCost) add(cost time.Duration) {
	c.estimate = max(cost, time.Duration(float64(c.estimate)*costFade))
}

// readPeriod reads the display's refresh period from the window manager; it
// stays what it was when that fails.
func (c *frameCost) readPeriod() {
	// DWM_TIMING_INFO, packed: its size, then the refresh rate's numerator and
	// denominator.
	var info [292]byte
	*(*uint32)(unsafe.Pointer(&info[0])) = uint32(len(info))
	if hr, _, _ := procDwmGetCompositionTimingInfo.Call(0, uintptr(unsafe.Pointer(&info[0]))); failed(hr) {
		return
	}
	num, den := *(*uint32)(unsafe.Pointer(&info[4])), *(*uint32)(unsafe.Pointer(&info[8]))
	if num != 0 && den != 0 {
		c.period = time.Duration(float64(time.Second) * float64(den) / float64(num))
	}
}

var (
	kernel32          = syscall.NewLazyDLL(device.SystemDir() + `\kernel32.dll`)
	procWaitForObject = kernel32.NewProc("WaitForSingleObject")
	procCloseHandle   = kernel32.NewProc("CloseHandle")
)

// latencyTimeout is the longest a wait on the object takes, in
// milliseconds: the screen takes a frame each refresh, but takes none while
// the window shows nowhere, when a frame presents without its token.
var latencyTimeout uintptr = 1000

const (
	waitObject0 = 0
	// swapChainWaitable is DXGI_SWAP_CHAIN_FLAG_FRAME_LATENCY_WAITABLE_OBJECT.
	swapChainWaitable = 0x40
)

type latency struct {
	// handle is the object, 0 without one: frames then present at once.
	handle uintptr

	mu sync.Mutex
	// token tells that a wait succeeded that no present took yet; waiting
	// that a goroutine waits on the object, which done tells the end of,
	// and then what it calls; lapsed that the goroutine's wait timed out,
	// so the present it calls on does not wait again, on the main thread.
	token, waiting, lapsed bool
	done                   chan struct{}
	then                   func()
}

// waitObject waits on the object, at most ms milliseconds, and reports
// whether it was signaled.
func (l *latency) waitObject(ms uintptr) bool {
	r, _, _ := procWaitForObject.Call(l.handle, ms)
	return r == waitObject0
}

// ready reports whether a present can take its token at once, waiting for
// nothing.
func (l *latency) ready() bool {
	if l.handle == 0 {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.token && !l.waiting {
		l.token = l.waitObject(0)
	}
	// A wait that lapsed is a present that waits for nothing (take).
	return l.token || l.lapsed
}

// waitThen has a goroutine wait for the object and call then, on its own
// goroutine, once it is signaled (or after latencyTimeout).
func (l *latency) waitThen(then func()) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.then = then
	if l.waiting {
		return
	}
	l.waiting, l.done = true, make(chan struct{})
	go func() {
		signaled := l.waitObject(latencyTimeout)
		l.mu.Lock()
		l.token = l.token || signaled
		l.lapsed = !l.token
		l.waiting = false
		then := l.then
		l.then = nil
		close(l.done)
		l.mu.Unlock()
		if then != nil {
			then()
		}
	}()
}

// take takes the token a present needs, waiting for it on the calling
// thread (a resize's frame, one waited for): at most latencyTimeout, after
// which the frame presents without one. A present whose goroutine's wait
// timed out already waited as long, while the window shows nowhere, and
// presents without waiting again.
func (l *latency) take() {
	if l.handle == 0 {
		return
	}
	l.mu.Lock()
	done, waiting := l.done, l.waiting
	l.mu.Unlock()
	if waiting {
		<-done
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.token && !l.lapsed {
		l.waitObject(latencyTimeout)
	}
	l.token, l.lapsed = false, false
}

// release waits for a goroutine waiting on the object and closes it.
func (l *latency) release() {
	if l.handle == 0 {
		return
	}
	l.mu.Lock()
	done, waiting := l.done, l.waiting
	l.then = nil
	l.mu.Unlock()
	if waiting {
		<-done
	}
	procCloseHandle.Call(l.handle)
	l.handle, l.token = 0, false
}
