//go:build darwin

package metal

import (
	"image"
	"runtime"
	"sync"
	"time"

	"github.com/ebitengine/purego"

	"github.com/egoist/mygo/internal/scene"
)

// A presenter fetches the layer's next drawable on a thread of its own,
// after each frame presented as soon as drawn, so that the main thread
// never waits for the display. nextDrawable blocks while the layer's
// drawables are all in use, as the display holds them until its next
// refreshes: a frame following another would wait most of a refresh,
// with the main thread able to do nothing else. Instead, a frame that
// comes while the presenter fetches is built and waits (pendingFrame),
// and the main thread draws it into the drawable once the presenter has
// it; frames paced by the display wait for that (Ready).
//
// The main thread waits for a fetch in flight before changing the layer
// (its drawables' size or format, presenting with the transaction,
// trimming), so a fetch never races those, and frames presented with the
// transaction take their drawable themselves, as a live resize's must.
// Waiting on a channel, the thread costs nothing while frames stop.
type presenter struct {
	layer id
	fetch chan struct{}

	mu   sync.Mutex
	done sync.Cond
	// drawable is the one fetched, retained, until a frame takes it;
	// fetching tells that the thread is fetching one, and wake what it
	// calls once it has (Ready).
	drawable id
	fetching bool
	wake     func()
	started  bool
}

// qosUserInteractive is QOS_CLASS_USER_INTERACTIVE, the main thread's.
const qosUserInteractive = 0x21

var (
	qosOnce sync.Once
	setQoS  func(class uint32, priority int32) int32
	// nextDrawable returns the layer's next drawable, autoreleased; tests
	// replace it.
	nextDrawable = func(layer id) id { return send(layer, "nextDrawable") }
)

// start has the thread fetch the next drawable, unless it is or has one.
func (p *presenter) start(layer id) {
	p.mu.Lock()
	if p.fetching || p.drawable != 0 {
		p.mu.Unlock()
		return
	}
	p.fetching = true
	if !p.started {
		p.started = true
		p.layer = layer
		p.done.L = &p.mu
		p.fetch = make(chan struct{}, 1)
		go p.run()
	}
	p.mu.Unlock()
	p.fetch <- struct{}{}
}

func (p *presenter) run() {
	// The autorelease pools belong to the thread.
	runtime.LockOSThread()
	qosOnce.Do(func() {
		if h, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_GLOBAL|purego.RTLD_NOW); err == nil {
			purego.RegisterLibFunc(&setQoS, h, "pthread_set_qos_class_self_np")
		}
	})
	if setQoS != nil {
		setQoS(qosUserInteractive, 0)
	}
	for range p.fetch {
		pool, _, _ := purego.SyscallN(poolPush)
		d := nextDrawable(p.layer)
		if d != 0 {
			send(d, "retain")
		}
		purego.SyscallN(poolPop, pool)
		p.mu.Lock()
		p.drawable, p.fetching = d, false
		wake := p.wake
		p.wake = nil
		p.done.Broadcast()
		p.mu.Unlock()
		if wake != nil {
			wake()
		}
	}
}

// ready reports whether a frame can take a drawable at once, as none is
// being fetched; otherwise wake runs once one is, on the presenter's
// thread.
func (p *presenter) ready(wake func()) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.fetching {
		return true
	}
	p.wake = wake
	return false
}

// wait waits for a fetch in flight.
func (p *presenter) wait() {
	p.mu.Lock()
	for p.fetching {
		p.done.Wait()
	}
	p.wake = nil
	p.mu.Unlock()
}

// take returns the drawable fetched, retained, or 0, once no fetch is in
// flight.
func (p *presenter) take() id {
	p.wait()
	p.mu.Lock()
	d := p.drawable
	p.drawable = 0
	p.mu.Unlock()
	return d
}

// drain lets go of the drawable fetched, once no fetch is in flight.
func (p *presenter) drain() {
	if d := p.take(); d != 0 {
		send(d, "release")
	}
}

// stop drains the presenter and ends its thread.
func (p *presenter) stop() {
	p.drain()
	p.mu.Lock()
	if p.started {
		close(p.fetch)
		p.started = false
	}
	p.mu.Unlock()
}

// A pendingFrame is a frame built while its drawable was being fetched,
// to draw into it once the presenter has it: its command buffer, retained,
// holding what the frame drew before the drawable (canvases, blurred
// images), and what drawing the rest takes of its scene, which the
// content may build anew meanwhile (a capture).
type pendingFrame struct {
	cb id
	// size and clear are the scene's, and whole tells that its effects
	// read their backdrops, which drawing in part would add to the area
	// drawn: it is drawn whole.
	size  scene.Scene
	whole bool
}

// SetPost gives the renderer post, which runs a function on the main
// thread from any goroutine: a frame following another builds while its
// drawable is being fetched, and draws once the presenter has it, through
// post. Without it, frames wait for their drawable.
func (r *Renderer) SetPost(post func(func())) {
	r.post = post
	finish := func() {
		pool(func() {
			// A frame not paced may have drawn the frame pending this was
			// posted for, and another may be pending, whose drawable is
			// being fetched: its fetch posts again once done, rather than
			// the main thread waiting for it.
			if r.pending.cb == 0 || !r.pre.ready(r.fetched) {
				return
			}
			if err := r.finishPending(true); err != nil {
				r.err = err
				r.wakeContent() // the next frame finds the error
			}
		})
	}
	r.fetched = func() { post(finish) }
}

// layerChanges reports whether drawing s at scale changes the layer, its
// drawables' size or format.
func (r *Renderer) layerChanges(s *scene.Scene, scale float64) bool {
	return r.layerWide != r.wide || s.Width != r.w || s.Height != r.h || scale != r.scale || r.trimmed ||
		msgRect(r.superlayer, sel("bounds")) != r.bounds
}

// stash builds the frame s, which changed damage since the last one, to
// draw once its drawable comes (finishPending).
func (r *Renderer) stash(s *scene.Scene, damage []image.Rectangle, known bool) error {
	cb, _, err := r.prepare(s)
	if err != nil {
		r.forgetFrames()
		return err
	}
	damage = r.pipelineDamage(damage, known)
	r.ages.Add(damage, !known)
	r.pending = pendingFrame{cb: send(cb, "retain"), whole: len(s.Effects) > 0}
	r.pending.size.Width, r.pending.size.Height, r.pending.size.Clear = s.Width, s.Height, s.Clear
	r.diff.Remember(s)
	r.pipelines.Remember()
	return nil
}

// finishPending draws the frame pending into its drawable, waiting for it
// when called before the presenter has it, and presents it, then has the
// presenter fetch the next frame's drawable if prefetch.
func (r *Renderer) finishPending(prefetch bool) error {
	p := r.pending
	r.pending = pendingFrame{}
	defer r.wakeContent()
	drawable := r.prefetched()
	if drawable == 0 {
		drawable = send(r.layer, "nextDrawable")
	}
	if drawable == 0 {
		// None came within a second: the frame is skipped, what it drew
		// before its drawable committed.
		r.forgetFrames()
		send(p.cb, "commit")
		r.track(p.cb)
		release(&p.cb)
		return nil
	}
	tex := send(drawable, "texture")
	backdrops := func(d []image.Rectangle) []image.Rectangle { return d }
	if p.whole {
		backdrops = nil
	}
	if err := r.drawBuilt(p.cb, &p.size, tex, &r.drawState, r.partIn(p.size.Width, p.size.Height, tex, backdrops)); err != nil {
		r.forgetFrames()
		release(&p.cb)
		return err
	}
	send(p.cb, "presentDrawable:", drawable)
	send(p.cb, "commit")
	if prefetch {
		r.pre.start(r.layer)
	}
	r.track(p.cb)
	release(&p.cb)
	r.lastRender = time.Now()
	r.armTrim()
	r.sweepIfDue()
	return nil
}

// dropPending drops the frame pending, committing what it drew before its
// drawable: the next draws whole. What Ready was given meanwhile runs, as
// the frame waiting for this one, which never comes, can draw now.
func (r *Renderer) dropPending() {
	if r.pending.cb == 0 {
		return
	}
	r.forgetFrames()
	send(r.pending.cb, "commit")
	r.track(r.pending.cb)
	release(&r.pending.cb)
	r.pending = pendingFrame{}
	r.wakeContent()
}

// wakeContent runs what Ready was given while a frame was pending.
func (r *Renderer) wakeContent() {
	if w := r.wake; w != nil {
		r.wake = nil
		w()
	}
}
