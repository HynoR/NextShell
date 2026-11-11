package audio

import (
	"errors"
	"runtime"
	"sync"
	"time"
)

// A device is the system's audio output, opened by openDevice to hold
// about ahead of what is heard.
type device interface {
	// write plays interleaved stereo frames after those written before,
	// waiting while the device holds enough already. An error closes the
	// device, and the output opens it again.
	write(samples []float32) error
	close()
}

// idleClose is how long the output stays open with nothing playing.
const idleClose = 5 * time.Second

var (
	openOutput = openDevice // tests replace it

	wake     = make(chan struct{}, 1)
	runOnce  sync.Once
	testMode bool // tests mix by hand
)

// wakeOutput starts the output, or tells it a voice started. The caller
// holds mu.
func wakeOutput() {
	if testMode {
		return
	}
	runOnce.Do(func() { go run() })
	select {
	case wake <- struct{}{}:
	default:
	}
}

func active() bool {
	mu.Lock()
	defer mu.Unlock()
	return len(voices) > 0 && !testMode
}

// mixFor mixes into buf for an output open at latency lat, and reports
// whether anything plays; ok is false, and it mixes nothing, once tests
// mix by hand or the latency changed.
func mixFor(buf []float32, lat Latency) (busy, ok bool) {
	mu.Lock()
	defer mu.Unlock()
	if testMode || latency != lat {
		return false, false
	}
	return mixLocked(buf), true
}

// run opens the device while anything plays, and mixes into it.
func run() {
	// Devices that need it, as COM's, stay on one thread.
	runtime.LockOSThread()
	for {
		for !active() {
			<-wake
		}
		lat := outputLatency()
		dev, n, err := openOutput(lat.ahead())
		if err != nil {
			setErr(err)
			dev, n = newNullDevice(err != errUnsupported), nullFrames
			// It holds nothing: what it takes is heard as it takes it.
			setHeld(0)
		} else {
			setErr(nil)
			setHeld(int64(lat.ahead() * sampleRate / time.Second))
		}
		buf := make([]float32, n*channels)
		idle := time.Duration(0)
		per := frames(int64(n), sampleRate)
		for {
			busy, ok := mixFor(buf, lat)
			if !ok {
				break
			}
			if err := dev.write(buf); err != nil {
				// Lost, or a null device trying the real one again.
				break
			}
			if busy {
				idle = 0
			} else if idle += per; idle >= idleClose {
				break
			}
		}
		dev.close()
	}
}

var errUnsupported = errors.New("audio: no audio output on this platform")

// nullFrames is how many frames a null device takes at a time: 10 ms.
const nullFrames = sampleRate / 100

// A nullDevice plays nothing, on time: players still end when they would
// have. One standing in for a device that failed to open fails after a
// while, so that the output tries the device again.
type nullDevice struct {
	next  time.Time
	retry time.Time
	// now and sleep are its clock, which tests replace.
	now   func() time.Time
	sleep func(time.Duration)
}

func newNullDevice(retry bool) *nullDevice {
	d := &nullDevice{next: time.Now(), now: time.Now, sleep: time.Sleep}
	if retry {
		d.retry = d.next.Add(2 * time.Second)
	}
	return d
}

var errRetry = errors.New("audio: try the device again")

func (d *nullDevice) write(samples []float32) error {
	now := d.now()
	if !d.retry.IsZero() && now.After(d.retry) {
		return errRetry
	}
	if now.Sub(d.next) > 100*time.Millisecond {
		// Behind, as after a sleep: don't hurry to catch up.
		d.next = now
	}
	d.next = d.next.Add(frames(int64(len(samples)/channels), sampleRate))
	d.sleep(d.next.Sub(d.now()))
	return nil
}

func (d *nullDevice) close() {}
