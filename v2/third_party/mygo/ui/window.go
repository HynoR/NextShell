package ui

import (
	"errors"
	"log"
	"os"
	"slices"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/raster"
	"github.com/egoist/mygo/internal/scene"
	"github.com/egoist/mygo/internal/surface"
	"github.com/egoist/mygo/internal/text"
	"github.com/egoist/mygo/transfer"
)

// Content is a user interface for a window, the value of
// mygo.WindowOptions.Content. Create it with View.
type Content struct {
	view func(*context)
}

// View returns the content of a window whose user interface view builds,
// for mygo.WindowOptions.Content:
//
//	mygo.NewWindow(mygo.WindowOptions{Title: "Counter", Content: ui.View(app.View)})
//
// view runs on the main thread whenever the window needs a frame: after
// input, after Context.Invalidate or Window.Invalidate, and while
// something animates. A Content can serve several windows, each with its
// own state.
func coreView(view func(c *context)) *Content { return &Content{view: view} }

// RegisterFont adds a TrueType or OpenType font, or collection, that text
// can use with Font(family). An empty family keeps the font's own name.
// The fonts registered under a family make it up, apart from other fonts
// of their own family.
func RegisterFont(data []byte, family string) error {
	return text.Shared().RegisterFont(data, family)
}

// A FontFamily is a font family text can use, as FontFamilies lists it.
type FontFamily struct {
	// Name is the family as Font and Theme take it.
	Name string
	// Monospaced is whether its regular font gives every character the
	// same advance, as a terminal's or a code editor's font setting
	// offers.
	Monospaced bool
	// Registered is whether the app registered it with RegisterFont, under
	// Name: the name it gave, or the font's own when it gave none.
	Registered bool
}

// FontFamilies lists the font families text can use, as a font setting
// offers them: the app's registered ones first, then the system's, each
// once, sorted without regard to case. A font registered under a name the
// app gave is listed under that name alone. Browsers do not tell what
// fonts they have: there the list holds the registered families alone,
// and a family is Monospaced once its font has loaded.
//
// The first call asks the system for its families, which takes a while
// with many fonts installed (tens of milliseconds on a Mac with hundreds),
// without holding up the windows' text; later calls reuse that list, until
// RegisterFont, so fonts installed while the app runs show after a
// restart. It is safe from any goroutine.
func FontFamilies() []FontFamily {
	families := text.Shared().FontFamilies()
	out := make([]FontFamily, len(families))
	for i, f := range families {
		out[i] = FontFamily(f)
	}
	return out
}

// AttachContent connects the content to a window; package mygo calls it.
func (v *Content) AttachContent(conn *surface.Conn) {
	h := &windowHost{conn: conn}
	rt := newRuntime(v.view, h)
	h.rt = rt
	conn.Event = h.event
	conn.ThemeChanged = func() {
		// The interface font is part of the appearance.
		h.uiFont()
		rt.themeChanged()
	}
	conn.TitleBarChanged = rt.requestFrame
	conn.ActiveChanged = rt.activeChanged
	conn.Changed = rt.changed
	conn.LocaleChanged = func() {
		h.textLanguage()
		rt.changed()
	}
	conn.Capture = h.capture
	conn.Detach = h.detach
	if conn.ContentSize != nil && conn.FitWidth != nil {
		rt.fit = newFitter(conn.FitWidth, conn.ContentSize)
		conn.Fit = h.fit
	}
	rt.insp.enabled = conn.DevTools
	conn.ToggleDevTools = func() {
		if rt.insp.enabled {
			rt.toggleInspector()
		}
	}
	h.uiFont()
	h.textLanguage()
	h.automate()
	// Load the fonts while the window shows up.
	go text.Shared().Preload()
	h.requestFrame()
}

// windowHost presents frames on a window's surface, with a GPU renderer
// when the platform has one and in memory otherwise.
type windowHost struct {
	conn *surface.Conn
	rt   *engine
	gpu  gpuRenderer
	// gpuTried tells that the first frame tried the GPU, onGPU that its
	// renderer drew on a GPU, degraded that one drawing in software took
	// its place, and gpuSince when the renderer was made. After a failure,
	// retryAt is when a frame makes another, and backoff how long the next
	// failure waits.
	gpuTried bool
	onGPU    bool
	degraded bool
	gpuSince time.Time
	retryAt  time.Time
	backoff  time.Duration
	soft     raster.Renderer
	last     *scene.Scene
	// lastFrame is when the last frame was presented.
	lastFrame time.Time
	// framing tells that the surface asked for the frame being drawn, and
	// frameAsked that the content asked the surface for a frame that has
	// not come yet.
	framing, frameAsked bool
	// told is what accelerated last told the frame being painted, asked
	// that it did.
	told, asked bool
	// path is how the last frame was drawn, for MYGO_FRAME_STATS.
	path string
	// background is the scene's Background the surface was last told
	// (platform.BackgroundSurface).
	background scene.Color
	// wide tells that the GPU renderer draws the colors of scenes outside
	// the sRGB gamut (see wideHold), until wideUntil.
	wide      bool
	wideUntil time.Time
	// idleTimer runs idle once frames stop, at idleAt when idleArmed.
	idleTimer *time.Timer
	idleAt    time.Time
	idleArmed bool
	// afterT and repaintT are the timers of After and Painter.After.
	afterT, repaintT *time.Timer
	detached         bool
	// shown tells that a frame was presented, which makes the window
	// ready to show.
	shown bool
	// placed is where the surface shows the window's web views, views
	// the web views.
	placed []platform.WebViewPlacement
	views  []NativeWebView
	// glassPlaced is where the surface shows the system's glass.
	glassPlaced []platform.GlassPlacement
	// waiting tells that a frame paced by the display waits for waitingOn,
	// the GPU renderer or refresh, to be able to draw it (displayWaiter);
	// wake and displayReady draw it then, at the time the display asked
	// for it (waitedAt).
	waiting      bool
	waitingOn    displayWaiter
	waitedAt     time.Time
	wake         func()
	displayReady func()
	// refresh paces the frames drawn in memory on a surface that shows
	// them at once (platform.RefreshSurface); nil until one is.
	refresh *refreshWaiter
}

// displayWaiter is a GPU renderer that waits for the display off the main
// thread: a frame following the last one draws once the display can take
// it (Direct3D's), or once the last, built while the display could not,
// was drawn (Metal's). refreshWaiter is one for frames drawn in memory.
type displayWaiter interface {
	// Ready reports whether a frame can draw at once; otherwise wake runs,
	// on any goroutine, once it can.
	Ready(wake func()) bool
}

// refreshWaiter holds the frames drawn in memory on a surface that shows
// them at once, as Windows' GDI does, to the display's refreshes: after
// each, a goroutine waits for the next refresh
// (platform.RefreshSurface.AwaitRefresh), and paced frames that come
// meanwhile, as an animation's, wait for it (windowHost.waitDisplay).
// Otherwise they follow each other as fast as the main thread draws them,
// thousands a second. The wait lasts at least half a refresh, in case the
// surface's returns at once, and at most two, so that a refresh missed
// never stops an animation.
type refreshWaiter struct {
	surface platform.RefreshSurface
	post    func(func())
	period  time.Duration
	// waiting tells that a goroutine waits for the refresh; wake is what
	// Ready was given meanwhile.
	waiting bool
	wake    func()
	// flush closes once the surface's wait under way returns, one that
	// outlasted its bounds included, which the next wait takes up rather
	// than start another; only the waiting goroutine reaches it.
	flush chan struct{}
}

func (r *refreshWaiter) Ready(wake func()) bool {
	if !r.waiting {
		return true
	}
	r.wake = wake
	return false
}

// presented has the next paced frame wait for the display's next refresh.
func (r *refreshWaiter) presented() {
	if r.waiting {
		return
	}
	r.waiting = true
	go r.await(r.surface, r.period)
}

// await waits for the display's next refresh, within its bounds, and has
// the main thread tell Ready's caller.
func (r *refreshWaiter) await(s platform.RefreshSurface, period time.Duration) {
	start := time.Now()
	if r.flush == nil {
		done := make(chan struct{})
		r.flush = done
		go func() {
			s.AwaitRefresh()
			close(done)
		}()
	}
	t := time.NewTimer(2 * period)
	select {
	case <-r.flush:
		r.flush = nil
	case <-t.C:
	}
	t.Stop()
	if d := period/2 - time.Since(start); d > 0 {
		time.Sleep(d)
	}
	r.post(r.refreshed)
}

// refreshed runs on the main thread once the refresh came.
func (r *refreshWaiter) refreshed() {
	r.waiting = false
	if w := r.wake; w != nil {
		r.wake = nil
		w()
	}
}

// waiter returns what paced frames wait for: the GPU renderer, or the
// display's refresh after a frame drawn in memory; nil for nothing.
func (h *windowHost) waiter() displayWaiter {
	if h.gpu != nil {
		w, _ := h.gpu.(displayWaiter)
		return w
	}
	if h.refresh != nil {
		return h.refresh
	}
	return nil
}

// refreshPresented has the paced frames following one drawn in memory
// wait for the display's next refresh, where the surface shows it at once
// (refreshWaiter).
func (h *windowHost) refreshPresented() {
	s, ok := h.conn.Surface.(platform.RefreshSurface)
	if !ok || h.conn.Post == nil {
		return
	}
	if h.refresh == nil {
		h.refresh = &refreshWaiter{post: h.conn.Post}
	}
	period := time.Second / 60
	if hz := h.conn.Surface.RefreshRate(); hz > 0 {
		period = time.Duration(float64(time.Second) / hz)
	}
	h.refresh.surface, h.refresh.period = s, period
	h.refresh.presented()
}

func (h *windowHost) framePath() string {
	if h.path == "drawn on the GPU" && h.gpu != nil {
		// The stats logger calls this only while MYGO_FRAME_STATS is enabled.
		if f, ok := h.gpu.(interface{ FrameInfo() string }); ok {
			return h.path + ": " + f.FrameInfo()
		}
	}
	return h.path
}

func (h *windowHost) rightToLeft() bool { return h.conn.RightToLeft != nil && h.conn.RightToLeft() }

// newGPU makes the GPU renderer of a surface; tests replace it.
var newGPU = newGPURenderer

// event handles an event of the surface, noting when it asks for a frame:
// only then may renderers draw (OpenGL's context is current only then).
func (h *windowHost) event(ev platform.SurfaceEvent) bool {
	if h.detached {
		return false // the window closed: it draws nothing more
	}
	if ev.Kind == platform.SurfaceRenew {
		// The next frame makes the renderer again, for the surface's new
		// native objects.
		if h.gpu != nil {
			h.gpu.Release()
			h.gpu = nil
		}
		h.gpuTried, h.retryAt = false, time.Time{}
		h.requestFrame()
		return false
	}
	if ev.Kind == platform.WebViewTabOut || ev.Kind == platform.WebViewKey {
		i := slices.IndexFunc(h.placed, func(p platform.WebViewPlacement) bool { return p.WebView == ev.WebView })
		switch {
		case i < 0:
		case ev.Kind == platform.WebViewTabOut:
			h.rt.webViewTabOut(h.views[i], ev.Mods&platform.ModShift != 0)
		default:
			h.rt.webViewKey(h.views[i], Modifiers(ev.Mods), Key(ev.Key), Key(ev.PhysicalKey), ev.Repeat)
		}
		return false
	}
	if ev.Kind == platform.SurfaceResize {
		if r, ok := h.gpu.(invalidator); ok {
			r.Invalidate()
		}
	}
	if ev.Kind == platform.SurfaceFrame {
		if ev.Paced && h.waitDisplay(time.Now()) {
			return true
		}
		h.waiting, h.waitingOn = false, nil
		h.frameAsked = false
		h.framing = true
		defer func() { h.framing = false }()
	}
	return h.rt.event(ev)
}

// waitDisplay reports whether a frame paced by the display, asked for at,
// waits for the GPU renderer to be able to draw it, or for the display's
// refresh after a frame drawn in memory (waiter): rather than wait on
// the main thread, as for a drawable the display holds most of a refresh,
// the frame draws once it can, built with the input that came meanwhile,
// at the time it was asked for, so that what moves keeps its pace.
func (h *windowHost) waitDisplay(at time.Time) bool {
	w := h.waiter()
	p, paced := h.conn.Surface.(platform.PacedSurface)
	if w == nil || !paced || h.conn.Post == nil {
		return false
	}
	if h.waiting && h.waitingOn == w {
		// Refreshes passing meanwhile ask for the same frame.
		return true
	}
	if h.wake == nil {
		h.displayReady = func() {
			if !h.waiting || h.detached {
				return
			}
			h.waiting, h.waitingOn = false, nil
			h.rt.frameAt = h.waitedAt
			p.FrameNow()
		}
		h.wake = func() { h.conn.Post(h.displayReady) }
	}
	if w.Ready(h.wake) {
		return false
	}
	h.waiting, h.waitingOn, h.waitedAt = true, w, at
	return true
}

// gpuRenderer draws scenes into a surface on the GPU.
type gpuRenderer interface {
	Render(s *scene.Scene) error
	Release()
}

// invalidator is a GPU renderer drawing only what changed into a target
// that keeps what was drawn but as the surface resizes, even to the same
// size, as a GtkGLArea makes its buffers anew as GTK allocates it:
// Invalidate has the next frame draw whole.
type invalidator interface{ Invalidate() }

// wideRenderer is a GPU renderer that can draw the colors of scenes outside
// the sRGB gamut (Metal's).
type wideRenderer interface {
	// SetWide has the frames drawn from now on show those colors, into
	// drawables of a wide gamut, or their nearest sRGB ones, and reports
	// whether they do.
	SetWide(on bool) bool
}

// A window whose screen shows more than sRGB, as a Mac's Display P3
// screen, draws the colors of ui.Oklch outside the sRGB gamut once a frame
// has some, on the GPU, whose drawables then hold float16 components, and
// goes on for wideHold after the last frame with some: a blinking caret or
// a color in transition does not switch the drawables back and forth. The
// CPU draws sRGB colors: frames meanwhile are the GPU's, and those after
// it may draw on the CPU again. A window on an sRGB screen draws the
// nearest sRGB colors, as it would show anyway, with the CPU's frames.
const wideHold = 2 * time.Second

// frameIdle is how long after the last frame the host frees the frame
// drawn in memory, as large as the window, and the surface gives back what
// its frames took (platform.IdleSurface). The next frame draws whole.
const frameIdle = 2 * time.Second

func (h *windowHost) refreshRate() float32 { return float32(h.conn.Surface.RefreshRate()) }

// occluded reports whether nothing of the window shows: never before its
// first frame reached the surface, which a window made hidden waits for
// to become ready to show (OnReadyToShow), though it shows nothing yet.
func (h *windowHost) occluded() bool {
	if !h.shown {
		return false
	}
	s, ok := h.conn.Surface.(platform.OccludableSurface)
	return ok && s.Occluded()
}

func (h *windowHost) size() (float32, float32, float32) {
	w, ht, s := h.conn.Surface.Size()
	if s <= 0 {
		s = 1
	}
	return float32(w), float32(ht), float32(s)
}

func (h *windowHost) present(s *scene.Scene) {
	if h.detached {
		return // closed while the frame was built: nothing shows it
	}
	h.last, h.path = s, ""
	defer h.recheck()
	if s.Width <= 0 || s.Height <= 0 {
		return
	}
	if s.Background != h.background {
		h.background = s.Background
		if b, ok := h.conn.Surface.(platform.BackgroundSurface); ok {
			b.SetBackground(platform.Color(s.Background))
		}
	}
	if !h.framing {
		// A frame built outside the surface's, for a capture, is shown by
		// the surface's next.
		h.requestFrame()
		return
	}
	due := !h.retryAt.IsZero() && !time.Now().Before(h.retryAt)
	switch {
	case !h.gpuTried || h.gpu == nil && due:
		h.makeGPU()
	case due && h.degraded:
		// Drawing in software since the GPU went away: is it back?
		h.gpu.Release()
		h.gpu = nil
		h.makeGPU()
	}
	now := time.Now()
	h.lastFrame = now
	h.armIdle(frameIdle)
	h.useWide(s, now)
	defer h.ready()
	if h.render(s) {
		h.path = "drawn on the GPU"
		if h.degraded {
			h.path = "drawn by the GPU renderer in software"
		}
		return
	}
	h.path = "drawn in memory"
	damage := h.soft.Render(s)
	m := &h.soft.Image
	if d, ok := h.conn.Surface.(platform.DamageSurface); ok {
		d.PresentDamage(m.Pix, m.Stride, m.W, m.H, damage)
	} else {
		h.conn.Surface.PresentPixels(m.Pix, m.Stride, m.W, m.H)
	}
	h.refreshPresented()
}

// ready tells the surface, after the first frame, that the frame reached
// it, and the window that it is ready to show: once the frame returns, as
// its listeners may show the window.
func (h *windowHost) ready() {
	if h.shown {
		return
	}
	h.shown = true
	if f, ok := h.conn.Surface.(platform.FirstFrameSurface); ok {
		f.FirstFrame()
	}
	if h.conn.Ready != nil && h.conn.Post != nil {
		h.conn.Post(h.conn.Ready)
	}
}

// armIdle has idle run d from now, unless it runs sooner already.
func (h *windowHost) armIdle(d time.Duration) {
	if h.conn.Post == nil {
		return
	}
	at := time.Now().Add(d)
	if h.idleArmed && !h.idleAt.After(at) {
		return
	}
	h.idleAt, h.idleArmed = at, true
	if h.idleTimer == nil {
		h.idleTimer = time.AfterFunc(d, func() { h.conn.Post(h.idle) })
	} else {
		h.idleTimer.Reset(d)
	}
}

// idle runs on the main thread after frames stop: it frees the frame drawn
// in memory, and has the surface give back memory, once the window is idle
// for frameIdle. A frame since rearms it.
func (h *windowHost) idle() {
	h.idleArmed = false
	if h.detached {
		return
	}
	since := time.Since(h.lastFrame)
	if since < frameIdle {
		h.armIdle(frameIdle - since)
		return
	}
	h.soft.Release()
	h.rt.text.ReleaseShaping()
	if s, ok := h.conn.Surface.(platform.IdleSurface); ok {
		s.Idle()
	}
}

// useWide has the GPU renderer draw the colors of s outside the sRGB gamut
// when the window shows them (see wideHold), and reports whether it does.
func (h *windowHost) useWide(s *scene.Scene, now time.Time) bool {
	w, ok := h.gpu.(wideRenderer)
	if !ok {
		return false
	}
	if len(s.Wide) > 0 {
		h.wideUntil = now.Add(wideHold)
	}
	if on := now.Before(h.wideUntil) && h.screenWide(); on != h.wide {
		h.wide = w.SetWide(on)
	}
	return h.wide
}

// screenWide reports whether the window's screen shows more than sRGB.
func (h *windowHost) screenWide() bool {
	w, ok := h.conn.Surface.(platform.WideGamutSurface)
	return ok && w.WideGamut()
}

// render draws s with the GPU renderer and reports whether it did. One
// that fails gives way to another made at once, in case the GPU is back
// already, or another, or Windows' software rasterizer, can draw; when
// that fails too, frames are drawn in memory until a later try.
func (h *windowHost) render(s *scene.Scene) bool {
	for try := 0; h.gpu != nil; try++ {
		err := h.gpu.Render(s)
		if err == nil {
			return true
		}
		log.Printf("mygo: drawing without the GPU: %v", err)
		h.gpu.Release()
		h.gpu = nil
		if try > 0 {
			h.retryLater()
			return false
		}
		if time.Since(h.gpuSince) > time.Minute {
			h.backoff = 0 // it worked for a while: the waits start over
		}
		h.makeGPU()
	}
	return false
}

// makeGPU makes the surface's GPU renderer: for the first frame, after one
// failed, and when the wait after a failure is over. Where the first frame
// cannot have one, the window draws in memory for good, and where it has a
// GPU, a renderer that draws in software in its place tries for the GPU
// again from time to time.
func (h *windowHost) makeGPU() {
	retry := h.gpuTried
	h.gpuTried, h.retryAt = true, time.Time{}
	n := h.conn.Surface.Native()
	if os.Getenv("MYGO_GPU") == "0" || n == (platform.SurfaceNative{}) {
		return
	}
	r, err := newGPU(n)
	switch {
	case err != nil:
		log.Printf("mygo: drawing without the GPU: %v", err)
		if retry {
			h.retryLater()
		}
		return
	case r == nil:
		return
	case !retry:
		h.onGPU = !software(r)
	case h.onGPU && software(r):
		if !h.degraded {
			log.Print("mygo: drawing in software until the GPU is back")
		}
		h.degraded = true
		h.retryLater()
	default:
		if !software(r) {
			log.Print("mygo: drawing with the GPU again")
		}
		h.degraded = false
	}
	h.gpu, h.gpuSince = r, time.Now()
	h.wide = false // the renderer draws sRGB until told otherwise
	if p, ok := r.(interface{ SetPost(func(func())) }); ok && h.conn.Post != nil {
		// Frames waiting for the display draw through the main thread.
		p.SetPost(h.conn.Post)
	}
}

// accelerated reports whether the frame draws on a GPU (Painter.Accelerated).
// The first frame makes its GPU renderer as it paints rather than as it
// presents, to tell.
func (h *windowHost) accelerated() bool {
	if h.framing && !h.gpuTried {
		h.makeGPU()
	}
	h.told, h.asked = h.gpu != nil && !software(h.gpu), true
	return h.told
}

// recheck paints the window again when the frame presented draws on a GPU
// and accelerated told its painters otherwise, or the other way round: the
// GPU went away or came back as it presented.
func (h *windowHost) recheck() {
	if h.asked && h.told != (h.gpu != nil && !software(h.gpu)) && h.conn.Invalidate != nil {
		h.invalidate()
	}
	h.asked = false
}

// software reports whether a renderer draws on the CPU, as Direct3D's WARP.
func software(r gpuRenderer) bool {
	s, ok := r.(interface{ Software() bool })
	return ok && s.Software()
}

// retryLater has a frame make a GPU renderer again after a wait, which
// doubles with each failure up to half a minute: a driver that resets, a
// GPU unplugged or a machine waking up may take a while. A window that
// draws nothing meanwhile is not woken for it.
func (h *windowHost) retryLater() {
	h.backoff = min(max(2*h.backoff, time.Second), 30*time.Second)
	h.retryAt = time.Now().Add(h.backoff)
}

// capture renders the last frame in memory: built now where none was yet,
// or where one waits for the window to show (holdFrame), so that a window
// out of sight shows what it shows now.
func (h *windowHost) capture() (int, int, []byte) {
	if h.last == nil || h.rt.held {
		h.rt.runFrame()
	}
	s := h.last
	if s == nil {
		return 0, 0, nil
	}
	var img raster.Image
	img.Resize(s.Width, s.Height)
	raster.Render(&img, s)
	return img.W, img.H, img.RGBA()
}

// fit builds the frame that waits for the window to show, if one does,
// for a window that takes its content's size to measure what it shows
// now before it shows (surface.Conn.Fit), as capture does; the surface's
// next frame draws it. It builds none within a frame, nor before the
// first: the view runs as the window first draws, not within NewWindow.
func (h *windowHost) fit() bool {
	rt := h.rt
	if h.detached || rt.inFrame || h.last == nil || !h.frameAsked && !rt.held {
		return false
	}
	rt.runFrame()
	return true
}

func (h *windowHost) detach() {
	h.detached = true
	if h.idleTimer != nil {
		h.idleTimer.Stop()
	}
	h.rt.close()
	h.soft.Release()
	if h.gpu != nil {
		h.gpu.Release()
		h.gpu = nil
	}
	// The last frame, and the bitmaps it drew, go with the window (#1281).
	h.last = nil
	h.placed, h.views, h.glassPlaced = nil, nil, nil
}

func (h *windowHost) requestFrame() {
	h.frameAsked = true
	h.conn.Surface.RequestFrame()
}

func (h *windowHost) framesAtOnce() bool {
	_, ok := h.conn.Surface.(platform.ImmediateFrameSurface)
	return ok
}

func (h *windowHost) setFrameRate(fps float32) {
	if s, ok := h.conn.Surface.(platform.FrameRateSurface); ok {
		s.SetFrameRate(float64(fps))
	}
}

func (h *windowHost) afterTimer(at time.Time) {
	switch {
	case at.IsZero():
		if h.afterT != nil {
			h.afterT.Stop()
		}
	case h.afterT != nil:
		h.afterT.Reset(max(time.Until(at), time.Millisecond))
	default:
		h.afterT = time.AfterFunc(max(time.Until(at), time.Millisecond), h.invalidate)
	}
}

func (h *windowHost) repaintTimer(at time.Time) {
	if at.IsZero() {
		if h.repaintT != nil {
			h.repaintT.Stop()
		}
		return
	}
	d := max(time.Until(at), time.Millisecond)
	if h.repaintT == nil {
		h.repaintT = time.AfterFunc(d, func() { h.post(h.rt.repaintNow) })
	} else {
		h.repaintT.Reset(d)
	}
}

func (h *windowHost) startDataDrag(data transfer.Data, local any, options transfer.DragOptions, x, y float32) error {
	return h.conn.StartDataDrag(data, local, options, float64(x), float64(y))
}
func (h *windowHost) cancelDataDrag()                    { h.conn.CancelDataDrag() }
func (h *windowHost) setDropFormats(f []transfer.Format) { h.conn.Surface.SetDropFormats(f) }

func (h *windowHost) post(fn func()) {
	if h.conn.Post != nil {
		h.conn.Post(fn)
	} else {
		h.conn.Invalidate()
	}
}

// uiFont gives system-ui the desktop's interface font, and text the
// desktop's settings for rasterizing it, where the text system does not
// know them.
func (h *windowHost) uiFont() {
	if h.conn.UIFont != nil {
		text.Shared().SetUIFamily(h.conn.UIFont())
	}
	if h.conn.FontRendering != nil {
		r := h.conn.FontRendering()
		text.Shared().SetFontRendering(r.Antialias, r.Hinting, r.Subpixels)
	}
}

func (h *windowHost) setCursor(c Cursor) { h.conn.Surface.SetCursor(platform.Cursor(c)) }

func (h *windowHost) setTextInput(t platform.TextInputState) { h.conn.Surface.SetTextInput(t) }

func (h *windowHost) updateAccessibility(tree *platform.AccessTree) {
	h.conn.Surface.UpdateAccessibility(tree)
}

func (h *windowHost) readClipboard() string {
	if h.conn.Clipboard == nil {
		return ""
	}
	return h.conn.Clipboard.ReadText()
}

func (h *windowHost) clipboardFormats() []transfer.Format {
	if h.conn.Clipboard == nil {
		return nil
	}
	return h.conn.Clipboard.Formats()
}

func (h *windowHost) writeClipboard(s string) {
	if h.conn.Clipboard != nil {
		h.conn.Clipboard.WriteText(s)
	}
}

func (h *windowHost) writeClipboardThen(s string, done func(error)) {
	c := h.conn.Clipboard
	if later, ok := c.(platform.LaterClipboard); ok {
		later.WriteTextThen(s, done)
		return
	}
	err := errNoClipboard
	if c != nil {
		err = c.WriteData(transfer.TextData(s), nil)
	}
	h.post(func() { done(err) })
}

// errNoClipboard is what writing the clipboard of a window without one
// comes to.
var errNoClipboard = errors.New("ui: the window has no clipboard")

func (h *windowHost) readClipboardData(formats []transfer.Format) transfer.Data {
	if h.conn.Clipboard == nil {
		return transfer.Data{}
	}
	d, err := h.conn.Clipboard.ReadData(formats)
	if err != nil && len(d.Items()) == 0 {
		return transfer.Data{}
	}
	return d
}

func (h *windowHost) writeClipboardData(d transfer.Data) {
	if h.conn.Clipboard != nil {
		h.conn.Clipboard.WriteData(d, nil)
	}
}

func (h *windowHost) startDrag() {
	if h.conn.StartDrag != nil {
		h.conn.StartDrag()
	}
}

func (h *windowHost) titleBarDoubleClicked() {
	if h.conn.TitleBarDoubleClicked != nil {
		h.conn.TitleBarDoubleClicked()
	}
}

func (h *windowHost) setMaximizeButton(r Rect) {
	if placeMaximizeButton != nil {
		placeMaximizeButton(h, r)
	}
}

// placeMaximizeButton is sendMaximizeButton once a view marked a maximize
// button (MaximizeButton): apps that mark none don't link the backends'
// side of it.
var placeMaximizeButton func(h *windowHost, r Rect)

func sendMaximizeButton(h *windowHost, r Rect) {
	if s, ok := h.conn.Surface.(platform.MaximizeButtonSurface); ok {
		s.SetMaximizeButton(rectF(r))
	}
}

func (h *windowHost) escape() {
	if h.conn.Escape != nil {
		h.conn.Escape()
	}
}

func (h *windowHost) isDark() bool { return h.conn.IsDark != nil && h.conn.IsDark() }

func (h *windowHost) preferences() platform.Preferences {
	if h.conn.Preferences == nil {
		return platform.Preferences{}
	}
	return h.conn.Preferences()
}

func (h *windowHost) locale() string {
	if h.conn.Locale == nil {
		return ""
	}
	return h.conn.Locale()
}

func (h *windowHost) region() i18n.Region {
	if h.conn.Region == nil {
		return i18n.Region{}
	}
	return h.conn.Region()
}

// textLanguage makes the language the app shows that of the text the
// text engine lays out without one, as Painter.Shape does.
func (h *windowHost) textLanguage() {
	text.Shared().SetLanguage(h.locale())
}

func (h *windowHost) titleBar() TitleBar {
	if h.conn.TitleBar == nil {
		return TitleBar{}
	}
	t := h.conn.TitleBar()
	return TitleBar{Height: float32(t.Height), Left: float32(t.Left), Right: float32(t.Right)}
}

func (h *windowHost) vibrancy() bool {
	s, ok := h.conn.Surface.(platform.MaterialSurface)
	return ok && s.ShowsMaterial()
}

func (h *windowHost) active() bool { return h.conn.Active == nil || h.conn.Active() }

func (h *windowHost) invalidate() { h.conn.Invalidate() }

func (h *windowHost) openURL(u string, done func(error)) {
	if h.conn.OpenURL != nil {
		h.conn.OpenURL(u, done)
	}
}

// nativeGlass reports whether the surface draws the system's glass.
func (h *windowHost) nativeGlass() bool {
	s, ok := h.conn.Surface.(platform.GlassSurface)
	return ok && s.NativeGlassSupported()
}

// placeGlass gives the surface where the frame shows the system's glass,
// unless it is where the last frame showed it.
func (h *windowHost) placeGlass(places []platform.GlassPlacement) {
	s, ok := h.conn.Surface.(platform.GlassSurface)
	if !ok {
		return
	}
	if slices.EqualFunc(places, h.glassPlaced, sameGlassPlacement) {
		return
	}
	h.glassPlaced = append(h.glassPlaced[:0], places...)
	s.PlaceGlass(places)
}

// sameGlassPlacement reports whether a and b place the same glass.
func sameGlassPlacement(a, b platform.GlassPlacement) bool {
	if a.ID != b.ID || a.Frame != b.Frame || a.Clip != b.Clip || a.Radii != b.Radii ||
		a.CornerRadius != b.CornerRadius || a.Style != b.Style || (a.Tint == nil) != (b.Tint == nil) {
		return false
	}
	return a.Tint == nil || *a.Tint == *b.Tint
}

// placeWebViews gives the surface where the frame shows the window's web
// views, unless it is where the last frame showed them.
func (h *windowHost) placeWebViews(views []placedWebView) {
	list := make([]platform.WebViewPlacement, 0, len(views))
	natives := make([]NativeWebView, 0, len(views))
	for _, v := range views {
		if n := v.v.SurfaceWebView(h.conn); n != nil {
			list = append(list, v.placement(n))
			natives = append(natives, v.v)
		}
	}
	if slices.EqualFunc(list, h.placed, func(a, b platform.WebViewPlacement) bool {
		return a.WebView == b.WebView && a.Frame == b.Frame && a.Clip == b.Clip && a.Width == b.Width && a.Height == b.Height &&
			a.Radii == b.Radii && slices.Equal(a.Covers, b.Covers)
	}) {
		return
	}
	h.placed, h.views = list, natives
	if h.conn.PlaceWebViews != nil {
		h.conn.PlaceWebViews(list)
	} else {
		h.conn.Surface.PlaceWebViews(list)
	}
}

func (h *windowHost) platformWebView(v NativeWebView) platform.WebView {
	return v.SurfaceWebView(h.conn)
}

func (h *windowHost) tabIntoWebView(v NativeWebView, back bool) {
	if n := v.SurfaceWebView(h.conn); n != nil {
		n.TabInto(back)
	}
}

func (h *windowHost) popupMenu(m *platform.Menu, x, y float32, chosen func(int)) {
	if h.conn.PopupMenu != nil {
		h.conn.PopupMenu(m, float64(x), float64(y), chosen)
	}
}
