package ui

import (
	"errors"
	"image"
	"image/draw"
	"io"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"weak"
)

// A VideoDecoder decodes a video's frames one after another, for a
// VideoPlayer to show. NewGIFDecoder returns one for animated GIFs;
// implement one for another format. A player calls it from one goroutine
// at a time.
type VideoDecoder interface {
	// Next decodes the next frame, with io.EOF after the last.
	Next() (VideoFrame, error)
	// Rewind goes back to before the first frame, for the video to play
	// again.
	Rewind() error
}

// A VideoFrame is a picture of a video and how long it shows.
type VideoFrame struct {
	// Image is the picture, of any image type: the player converts it as
	// Bitmap.Update does, and copies it, so the decoder may draw the next
	// frame into it.
	Image image.Image
	// Duration is how long the frame shows; a frame of none shows for
	// 100 ms, as browsers show GIFs'.
	Duration time.Duration
}

// videoAhead is the frames a player keeps ready, ahead of its playhead.
const videoAhead = 2

// A VideoPlayer plays a video, for Video to show: its frames decode on a
// goroutine of its own a few ahead, as the playhead nears them, and each
// reaches the screen through a Bitmap's Update as it comes due, a frame
// passed over when the next one is due already. A new player plays as
// soon as a Video shows it; show it in one Video at a time. Call its
// methods on the main thread, in the view or an input handler, as drawing
// does.
type VideoPlayer struct {
	dec    *videoDecode
	bitmap *Bitmap
	// first is the first frame, kept for less motion, which shows it, and
	// for playing again; firstDur is how long it shows. cur is the frame
	// the playhead, pos, is in, which ends at end; shows is the one the
	// bitmap shows: cur, or first.
	first    *image.RGBA
	firstDur time.Duration
	cur      *image.RGBA
	shows    *image.RGBA
	pos, end time.Duration
	// at is when the frame that last built the video began, rt and frame
	// which frame of which window that was, and moved whether the
	// playhead moved on in it.
	at    time.Time
	rt    *engine
	frame uint64
	moved bool
	// frames are what the goroutine decoding ahead sends, done closed as
	// it returns; nil before it starts, and after the video ended.
	frames chan videoFrame
	done   chan struct{}
	paused bool
	ended  bool
	err    error
	closed bool
}

// videoDecode is what a player shares with the goroutine decoding ahead of
// it, which must not keep the player alive: a player dropped stops it.
type videoDecode struct {
	d    VideoDecoder
	loop atomic.Bool
	// free are frames shown or passed over, for the goroutine to decode
	// into again.
	free chan *image.RGBA
	// waiting tells that the playhead reached a frame not decoded yet:
	// sending it wakes the window that last built the video, wake, which
	// the goroutine does not keep alive either.
	waiting atomic.Bool
	mu      sync.Mutex
	wake    weak.Pointer[engine]
	quit    chan struct{}
	stop    sync.Once
}

// videoFrame is a frame decoded ahead, or with no image the end of the
// video, with what ended it.
type videoFrame struct {
	img *image.RGBA
	dur time.Duration
	err error
}

// NewVideoPlayer returns a player of d's frames, playing. It decodes the
// first frame at once, which Video shows from its first frame, and which
// reports a video that does not decode. Make players once, not in the
// view, as bitmaps. Closing the player closes d if d is an io.Closer.
func NewVideoPlayer(d VideoDecoder) (*VideoPlayer, error) {
	f, err := d.Next()
	if err == io.EOF {
		err = errors.New("ui: a video of no frames")
	}
	if err != nil {
		return nil, err
	}
	first := videoRGBA(nil, f.Image)
	dec := &videoDecode{d: d, free: make(chan *image.RGBA, videoAhead+2), quit: make(chan struct{})}
	p := &VideoPlayer{dec: dec, bitmap: NewBitmap(first), first: first, firstDur: videoDuration(f.Duration)}
	p.cur, p.shows, p.end = first, first, p.firstDur
	runtime.AddCleanup(p, func(dec *videoDecode) { dec.halt() }, dec)
	return p, nil
}

// SetLoop plays the video again from its start each time it ends,
// without a gap. It returns the player.
func (p *VideoPlayer) SetLoop(loop bool) *VideoPlayer {
	p.dec.loop.Store(loop)
	return p
}

// Loop reports whether the video plays again as it ends.
func (p *VideoPlayer) Loop() bool { return p.dec.loop.Load() }

// Play goes on from where the video was paused, or plays it again from its
// start once it ended.
func (p *VideoPlayer) Play() {
	p.paused = false
	if p.ended && !p.closed {
		p.ended, p.err = false, nil
		p.release(p.cur)
		p.cur, p.pos, p.end = p.first, 0, p.firstDur
	}
}

// Pause holds the video at the frame it shows.
func (p *VideoPlayer) Pause() { p.paused = true }

// Playing reports whether the video plays: neither paused nor ended. It
// plays while out of sight too, though it does not move on then.
func (p *VideoPlayer) Playing() bool { return !p.paused && !p.ended }

// Size returns the size of the video's frames in pixels, which Video
// shows as DIPs.
func (p *VideoPlayer) Size() (w, h int) { return p.bitmap.Size() }

// Err returns what stopped the video before its end, as a frame that did
// not decode: the video shows the frame before it.
func (p *VideoPlayer) Err() error { return p.err }

// Close stops decoding, and closes the decoder if it is an io.Closer, on
// a goroutine of its own once the frame it may be decoding is done. The
// video shows the frame it was at.
func (p *VideoPlayer) Close() {
	if p.closed {
		return
	}
	p.closed, p.ended = true, true
	p.dec.halt()
	done := p.done
	p.frames, p.done = nil, nil
	if c, ok := p.dec.d.(io.Closer); ok {
		go func() {
			if done != nil {
				<-done
			}
			c.Close()
		}()
	}
}

// Video creates an element showing a player's video, by default at its
// size as DIPs, scaled to fit when given another size, as Image shows a
// bitmap:
//
//	//go:embed waves.gif
//	var wavesGIF []byte
//
//	d, err := ui.NewGIFDecoder(wavesGIF)
//	...
//	app.waves, err = ui.NewVideoPlayer(d)
//	app.waves.SetLoop(true)
//
//	ui.Video(c, app.waves).Fill().Fit(ui.Cover)
//
// The video moves on only while the view builds it and some of the window
// shows: one that went out of the view, or a window out of sight, goes on
// from where it stood. Each frame of the video is a frame of the window,
// which After asks for as it comes due. While the desktop asks for less
// motion (Preferences.ReduceMotion), it shows its first frame and does
// not play.
func coreVideo(c *context, p *VideoPlayer) *node {
	if p == nil {
		return coreImage(c, nil)
	}
	p.advance(c)
	return coreImage(c, p.bitmap)
}

// advance moves the playhead on by the time the video played since the
// frame that last built it, once a frame, and shows the frame due: the
// last one of those that came due, the others passed over, so that a video
// decoded late catches up rather than lags.
func (p *VideoPlayer) advance(c *context) {
	rt, now := c.rt, c.now
	if p.rt == rt && p.frame == rt.frame {
		return // another pass of the frame, which moved it already
	}
	// Only time the video showed moves it: since the frame before, which
	// built it playing too, and since some of the window showed again
	// (SurfaceShown), as no frame builds while none of it shows. What it
	// showed after its last frame before the window went out of sight,
	// less than a frame of the video, does not count.
	from := p.at
	if rt.shown.After(from) {
		from = rt.shown
	}
	if p.moved && p.rt == rt && p.frame+1 == rt.frame && now.After(from) {
		p.pos += now.Sub(from)
	}
	p.rt, p.frame, p.at = rt, rt.frame, now
	if rt.preferences().ReduceMotion {
		p.moved = false
		p.show(p.first)
		return
	}
	p.moved = !p.paused && !p.ended && !rt.host.occluded()
	if !p.moved {
		if !p.paused && !p.ended {
			// Out of sight, as keys typed into a window that another
			// covers build a frame: the frame After asks for waits for
			// the window to show (holdFrame), and goes on from there.
			c.After(max(p.end-p.pos, 0))
		}
		p.show(p.cur)
		return
	}
	if p.frames == nil {
		p.start()
	}
	p.dec.mu.Lock()
	p.dec.wake = weak.Make(rt)
	p.dec.mu.Unlock()
	for p.pos >= p.end {
		f, ok := p.receive()
		if !ok {
			break // not decoded yet: sending it wakes the window
		}
		if f.img == nil {
			p.ended, p.moved, p.err = true, false, f.err
			p.frames = nil
			break
		}
		p.release(p.cur)
		p.cur = f.img
		p.end += f.dur
	}
	p.show(p.cur)
	if p.moved && p.pos < p.end {
		c.After(p.end - p.pos)
	}
}

// start has a goroutine decode the frames ahead of the playhead: those
// after the first, which the player decoded, or once the video ended
// those from its start again, after the goroutine that ended it returned.
func (p *VideoPlayer) start() {
	again := p.done != nil
	if again {
		<-p.done
	}
	frames, done := make(chan videoFrame, videoAhead), make(chan struct{})
	p.frames, p.done = frames, done
	go p.dec.run(frames, done, again)
}

// receive takes the next frame decoded, reporting false when there is none
// yet, which the goroutine is to wake the window for as it sends one.
func (p *VideoPlayer) receive() (videoFrame, bool) {
	select {
	case f := <-p.frames:
		return f, true
	default:
	}
	p.dec.waiting.Store(true)
	select {
	case f := <-p.frames:
		// Sent meanwhile: a wake may come still, for a frame that
		// changes nothing.
		p.dec.waiting.Store(false)
		return f, true
	default:
		return videoFrame{}, false
	}
}

// release gives a frame no longer shown back to the goroutine, to decode
// into again.
func (p *VideoPlayer) release(img *image.RGBA) {
	if img == p.first || img == p.shows {
		return
	}
	select {
	case p.dec.free <- img:
	default:
	}
}

// show has the bitmap show img, which stays as it is until the next show,
// as Bitmap.Update asks.
func (p *VideoPlayer) show(img *image.RGBA) {
	if img == p.shows {
		return
	}
	old := p.shows
	p.shows = img
	p.bitmap.Update(img)
	if old != p.cur {
		p.release(old)
	}
}

// run decodes frames into frames until the video ends, or the player
// stops (halt): the first frame of a video that plays again too, which the
// player keeps, after rewinding. It sends what ended the video as a frame
// with no image, nil at its end.
func (dec *videoDecode) run(frames chan<- videoFrame, done chan<- struct{}, rewind bool) {
	defer close(done)
	send := func(f videoFrame) bool {
		select {
		case frames <- f:
		case <-dec.quit:
			return false
		}
		if dec.waiting.Swap(false) {
			dec.mu.Lock()
			rt := dec.wake.Value()
			dec.mu.Unlock()
			if rt != nil {
				rt.host.invalidate()
			}
		}
		return true
	}
	if rewind {
		err := dec.d.Rewind()
		if err == nil {
			_, err = dec.d.Next()
		}
		if err != nil {
			send(videoFrame{err: err})
			return
		}
	}
	for {
		select {
		case <-dec.quit:
			return
		default:
		}
		f, err := dec.d.Next()
		if err == io.EOF && dec.loop.Load() {
			if err = dec.d.Rewind(); err == nil {
				f, err = dec.d.Next()
			}
		}
		if err != nil {
			if err == io.EOF {
				err = nil
			}
			send(videoFrame{err: err})
			return
		}
		var buf *image.RGBA
		select {
		case buf = <-dec.free:
		default:
		}
		if !send(videoFrame{img: videoRGBA(buf, f.Image), dur: videoDuration(f.Duration)}) {
			return
		}
	}
}

// halt stops the goroutine decoding ahead.
func (dec *videoDecode) halt() { dec.stop.Do(func() { close(dec.quit) }) }

// videoRGBA copies img into dst, or into a new image when dst is nil or of
// another size, converting it as Bitmap.Update does.
func videoRGBA(dst *image.RGBA, img image.Image) *image.RGBA {
	b := img.Bounds()
	if dst == nil || dst.Rect.Dx() != b.Dx() || dst.Rect.Dy() != b.Dy() {
		dst = image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	}
	draw.Draw(dst, dst.Rect, img, b.Min, draw.Src)
	return dst
}

// videoDuration is how long a frame shows: 100 ms for none.
func videoDuration(d time.Duration) time.Duration {
	if d <= 0 {
		return 100 * time.Millisecond
	}
	return d
}
