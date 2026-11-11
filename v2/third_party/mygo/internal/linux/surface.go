//go:build linux && (amd64 || arm64)

package linux

import (
	"image"
	"log"
	"math"
	"os"
	"sync"
	"unicode/utf8"
	"unsafe"

	"github.com/ebitengine/purego"

	"github.com/egoist/mygo/internal/gpu/gl"
	"github.com/egoist/mygo/internal/platform"
)

// The surface of a window that shows content MyGo draws itself is a
// GtkGLArea in place of the web view: frames are drawn with OpenGL in its
// render signal, into the framebuffer of the context GTK makes current,
// and GTK shows them. Without OpenGL, frames drawn in memory are painted
// with cairo in the draw signal, as on a GtkDrawingArea, which the surface
// is with MYGO_GPU=0 and where OpenGL would not draw on a GPU (gpuGL).
// GTK's frame clock paces both, at the refresh rate of the monitor showing
// the window (paceFrameClock); keys go through a GtkIMContext while a text
// input has the focus.
//
// Frames drawn in memory are drawn in the frame clock's update phase, by a
// tick callback, before GTK paints: the area then asks GTK to repaint what
// they changed, which GTK paints from them, keeping the rest of its buffer
// and telling the compositor only that changed. A draw GTK asks for by
// itself, as when the window is resized or uncovered, draws a frame there.
//
// GTK paints, and its frame clock ticks for, only a window that shows: the
// first frame of a window made hidden is drawn while it is hidden, from an
// idle callback (drawHidden), which makes it ready to show
// (OnReadyToShow).

var (
	surfaceOnce                 sync.Once
	gtkDrawingAreaNew           func() ptr
	gtkWidgetSetCanFocus        func(w ptr, v bool)
	gtkWidgetAddEvents          func(w ptr, mask int32)
	gtkWidgetQueueDraw          func(w ptr)
	gtkWidgetQueueDrawArea      func(w ptr, x, y, width, height int32)
	gtkWidgetGetHasWindow       func(w ptr) bool
	gtkWidgetAddTickCallback    func(w, callback, data, notify ptr) uint32
	cairoClipExtents            func(cr ptr, x1, y1, x2, y2 *float64)
	gtkWidgetGetAllocatedWidth  func(w ptr) int32
	gtkWidgetGetAllocatedHeight func(w ptr) int32
	gtkWidgetGetScaleFactor     func(w ptr) int32
	gtkWidgetGetDisplay         func(w ptr) ptr
	gtkWidgetHasFocus           func(w ptr) bool
	gtkIMMulticontextNew        func() ptr
	gtkIMContextSetClientWindow func(im, win ptr)
	gtkIMContextFilterKeypress  func(im, event ptr) bool
	gtkIMContextFocusIn         func(im ptr)
	gtkIMContextFocusOut        func(im ptr)
	gtkIMContextReset           func(im ptr)
	gtkIMContextSetCursorLoc    func(im ptr, r *gdkRectangle)
	gtkIMContextGetPreedit      func(im ptr, str *ptr, attrs *ptr, cursor *int32)
	gdkKeyvalToUnicode          func(keyval uint32) uint32
	cairoImageSurfaceForData    func(data *byte, format, width, height, stride int32) ptr
	cairoSurfaceSetDeviceScale  func(s ptr, x, y float64)
	cairoSetSourceSurface       func(cr, s ptr, x, y float64)
	cairoSetOperator            func(cr ptr, op int32)
	cairoPaint                  func(cr ptr)
	gtkGLAreaNew                func() ptr
	gtkGLAreaSetRequiredVersion func(a ptr, major, minor int32)
	gtkGLAreaSetHasAlpha        func(a ptr, alpha bool)
	gtkGLAreaGetError           func(a ptr) ptr
	gtkGLAreaSetError           func(a, gerr ptr)
	gtkGLAreaSetAutoRender      func(a ptr, auto bool)
	gtkGLAreaQueueRender        func(a ptr)
	gdkWindowPeekChildren       func(w ptr) ptr
	gdkWindowGetUserData        func(w ptr, data *ptr)
	gdkEventGetSourceDevice     func(e ptr) ptr
	gdkDeviceGetSource          func(d ptr) int32
	gdkEventGetAxis             func(e ptr, use int32, value *float64) bool
	gtkWidgetGetFrameClock      func(w ptr) ptr
	gdkFrameClockCurrentTimings func(clock ptr) ptr
	gdkFrameTimingsRefresh      func(t ptr) int64

	cbSurfaceDraw, cbSurfaceSize, cbSurfaceRealize, cbSurfaceButton ptr
	cbSurfaceMotion, cbSurfaceLeave, cbSurfaceScroll, cbSurfaceKey  ptr
	cbSurfaceFocusIn, cbSurfaceFocusOut, cbSurfaceScale, cbIMCommit ptr
	cbIMPreedit, cbIMPreeditEnd, cbSurfaceRender, cbAreaContext     ptr
	cbSurfaceUnrealize, cbSurfaceTick, cbSurfaceGesture             ptr
	cbRenderTick, cbDrawHidden, cbWindowGLDamage                    ptr
	surfaceCursors                                                  = map[platform.Cursor]ptr{}
)

func loadSurface() {
	surfaceOnce.Do(func() {
		t, d, c := libGTK, libGDK, libCairo
		mustBind(t, &gtkDrawingAreaNew, "gtk_drawing_area_new")
		mustBind(t, &gtkWidgetSetCanFocus, "gtk_widget_set_can_focus")
		mustBind(t, &gtkWidgetAddEvents, "gtk_widget_add_events")
		mustBind(t, &gtkWidgetQueueDraw, "gtk_widget_queue_draw")
		mustBind(t, &gtkWidgetQueueDrawArea, "gtk_widget_queue_draw_area")
		mustBind(t, &gtkWidgetGetHasWindow, "gtk_widget_get_has_window")
		mustBind(t, &gtkWidgetAddTickCallback, "gtk_widget_add_tick_callback")
		mustBind(c, &cairoClipExtents, "cairo_clip_extents")
		mustBind(t, &gtkWidgetGetAllocatedWidth, "gtk_widget_get_allocated_width")
		mustBind(t, &gtkWidgetGetAllocatedHeight, "gtk_widget_get_allocated_height")
		mustBind(t, &gtkWidgetGetScaleFactor, "gtk_widget_get_scale_factor")
		mustBind(t, &gtkWidgetGetDisplay, "gtk_widget_get_display")
		mustBind(t, &gtkWidgetHasFocus, "gtk_widget_has_focus")
		mustBind(t, &gtkIMMulticontextNew, "gtk_im_multicontext_new")
		mustBind(t, &gtkIMContextSetClientWindow, "gtk_im_context_set_client_window")
		mustBind(t, &gtkIMContextFilterKeypress, "gtk_im_context_filter_keypress")
		mustBind(t, &gtkIMContextFocusIn, "gtk_im_context_focus_in")
		mustBind(t, &gtkIMContextFocusOut, "gtk_im_context_focus_out")
		mustBind(t, &gtkIMContextReset, "gtk_im_context_reset")
		mustBind(t, &gtkIMContextSetCursorLoc, "gtk_im_context_set_cursor_location")
		mustBind(t, &gtkIMContextGetPreedit, "gtk_im_context_get_preedit_string")
		mustBind(d, &gdkKeyvalToUnicode, "gdk_keyval_to_unicode")
		mustBind(c, &cairoImageSurfaceForData, "cairo_image_surface_create_for_data")
		mustBind(c, &cairoSurfaceSetDeviceScale, "cairo_surface_set_device_scale")
		mustBind(c, &cairoSetSourceSurface, "cairo_set_source_surface")
		mustBind(c, &cairoSetOperator, "cairo_set_operator")
		mustBind(c, &cairoPaint, "cairo_paint")
		mustBind(d, &gdkWindowPeekChildren, "gdk_window_peek_children")
		mustBind(d, &gdkWindowGetUserData, "gdk_window_get_user_data")
		mustBind(d, &gdkEventGetSourceDevice, "gdk_event_get_source_device")
		mustBind(d, &gdkDeviceGetSource, "gdk_device_get_source")
		mustBind(d, &gdkEventGetAxis, "gdk_event_get_axis")
		mustBind(t, &gtkWidgetGetFrameClock, "gtk_widget_get_frame_clock")
		mustBind(d, &gdkFrameClockCurrentTimings, "gdk_frame_clock_get_current_timings")
		mustBind(d, &gdkFrameTimingsRefresh, "gdk_frame_timings_get_refresh_interval")
		// GtkGLArea came in GTK 3.16.
		if !bind(t, &gtkGLAreaNew, "gtk_gl_area_new") || !bind(t, &gtkGLAreaSetRequiredVersion, "gtk_gl_area_set_required_version") ||
			!bind(t, &gtkGLAreaSetHasAlpha, "gtk_gl_area_set_has_alpha") || !bind(t, &gtkGLAreaGetError, "gtk_gl_area_get_error") ||
			!bind(t, &gtkGLAreaSetError, "gtk_gl_area_set_error") ||
			!bind(t, &gtkGLAreaSetAutoRender, "gtk_gl_area_set_auto_render") || !bind(t, &gtkGLAreaQueueRender, "gtk_gl_area_queue_render") {
			gtkGLAreaNew = nil
		}
	})
}

type surface struct {
	clientComposition platform.InputComposition
	w                 *window
	area              ptr // GtkGLArea, or GtkDrawingArea
	im                ptr // GtkIMContext
	cr                ptr // the cairo context of the draw signal in progress
	cursor            platform.Cursor
	// gl tells that the area is a GtkGLArea, rendering that its render
	// signal is in progress, rendered that it ran.
	gl, rendering, rendered bool
	// present shows the frames the content draws in memory in a GtkGLArea,
	// as when its GL renderer fails; inMemory tells that it did, failed
	// that it failed too.
	present                 gl.Presenter
	inMemory, presentFailed bool
	// tick is the tick callback drawing frames in memory, while one is
	// asked for (RequestFrame); again tells that another was asked for
	// while it drew one, and ticking that it draws one.
	tick           uint32
	again, ticking bool
	// renderAgain tells that a frame was asked for during the render
	// signal.
	renderAgain bool
	// renderPending tells that a tick callback asks the area to render.
	// It is not reset on unrealize: GTK keeps tick callbacks across it.
	renderPending bool
	// drawn tells that the content's first frame reached the surface
	// (FirstFrame), and hiddenFrame that the first frame of a window
	// hidden, which GTK never paints, waits for its idle callback
	// (drawHidden).
	drawn, hiddenFrame bool
	// staged is the frame the tick callback drew, which the draw signal
	// paints in the same frame of the frame clock: the host's memory,
	// valid until it draws or frees another (see Idle).
	staged struct {
		pix                   []byte
		stride, width, height int
	}

	// held are the keys the content got pressed and not yet released,
	// whose releases the input method may take (cbSurfaceKey).
	held platform.HeldKeys
	// pinch is the scale of the touchpad's pinch in progress at its last
	// event, from 1 as it began.
	pinch float64
	// touch follows the fingers on the area; touched tells that GDK
	// gave it touches, which the mouse GTK made of them must not repeat.
	touch   touchGesture
	touched bool

	textInput bool
	caret     platform.RectF
	input     platform.TextInputState
	// lastKey is a copy of the last key press, which a context menu the
	// key opens shows for.
	lastKey ptr
	// pressing is the key press being handled, which the Tab moving the
	// focus to a web view passes on to it (TabInto).
	pressing ptr
	// down are the keys held, which tell a press's auto-repeat.
	down         keysDown
	lastPointer  ptr
	clicks       platform.ClickCounter
	dragContext  ptr
	dragRequest  *platform.DragRequest
	dragError    error
	dragCanceled bool
	dragEscaped  bool
	dataDrop     *gtkDataDrop
	// layers is the overlay of the area over web, the layout of the web
	// views under it, and placed where the content shows them
	// (webview.go), once the window has some.
	layers, web ptr
	placed      []platform.WebViewPlacement
}

// GDK event masks of the drawing area: pointer motion, button press and
// release, key press and release, enter and leave, focus, scroll, touch,
// smooth scroll and touchpad gestures.
const surfaceEvents = 1<<2 | 1<<8 | 1<<9 | 1<<10 | 1<<11 | 1<<12 | 1<<13 | 1<<14 | 1<<21 | 1<<22 | 1<<23 | 1<<24

func (w *window) createSurface() {
	loadSurface()
	data := ptr(w.id)
	s := &surface{w: w}
	s.im = gtkIMMulticontextNew()
	s.newArea(gtkGLAreaNew != nil && os.Getenv("MYGO_GPU") != "0" && gpuGL())
	connect(s.im, "commit", cbIMCommit, data)
	connect(s.im, "preedit-changed", cbIMPreedit, data)
	connect(s.im, "preedit-end", cbIMPreeditEnd, data)
	s.connectSystem(data)
	w.surface = s
	if s.gl && w.b.waylandSurface != nil {
		connect(w.win, "draw", cbWindowGLDamage, data)
	}
}

// wlSurfaceDamage is wl_surface.damage, marshalled by libwayland-client
// (wl_proxy_marshal_array), which GDK loaded.
var wlSurfaceDamage struct {
	once    sync.Once
	marshal func(proxy ptr, opcode uint32, args *[4]uint64)
}

// damageGLPaint tells the compositor what the paint of the window being
// drawn changes, in the window's coordinates, which GTK 3 tells it wrong
// at a scale of 2 or more once it composites the window with OpenGL, as
// it does from the first context of the window's GtkGLArea. GTK passes
// what a frame changed to eglSwapBuffersWithDamageEXT unscaled, flipped
// with the window's height (gdk/wayland/gdkglcontext-wayland.c, fixed in
// GTK 4 only), and EGL takes rectangles of the buffer's pixels: at a scale
// of 2, a change at the top of the window is reported in its lower half,
// at half the size. A compositor that redraws only what it was told
// changed (Mesa's llvmpipe tells it whole frames did, a GPU's buffers what
// GTK said) then shows a title button the pointer hovers flickering or
// gone, and a shadow missing on some sides (#1071). The damage of
// wl_surface.damage, in the surface's coordinates, joins EGL's in the
// commit of the swap that follows the paint: the area GTK repaints
// (cairo's clip in the toplevel's draw signal), with what the age of the
// back buffer had it repaint too.
func (w *window) damageGLPaint(cr ptr) {
	if gtkWidgetGetScaleFactor(w.win) < 2 {
		return
	}
	wl := w.b.waylandSurface(gtkWidgetGetWindow(w.win))
	d := &wlSurfaceDamage
	d.once.Do(func() {
		if lib, err := purego.Dlopen("libwayland-client.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL); err == nil {
			bind(lib, &d.marshal, "wl_proxy_marshal_array")
		}
	})
	if wl == 0 || d.marshal == nil {
		return
	}
	var x0, y0, x1, y1 float64
	cairoClipExtents(cr, &x0, &y0, &x1, &y1)
	r := image.Rect(int(math.Floor(x0)), int(math.Floor(y0)), int(math.Ceil(x1)), int(math.Ceil(y1)))
	if r.Empty() {
		return
	}
	if testGLDamage != nil {
		testGLDamage(w, r)
	}
	// wl_argument is a union of 8 bytes, whose int32 is its first 4.
	arg := func(v int) uint64 { return uint64(uint32(int32(v))) }
	const damage = 2 // wl_surface's request
	d.marshal(wl, damage, &[4]uint64{arg(r.Min.X), arg(r.Min.Y), arg(r.Dx()), arg(r.Dy())})
}

// testGLDamage, when set, sees each area damageGLPaint damages.
var testGLDamage func(w *window, r image.Rectangle)

// newArea creates the surface's widget, a GtkGLArea or a GtkDrawingArea
// that assistive technology sees the content of, and connects its input.
func (s *surface) newArea(gl bool) {
	data := ptr(s.w.id)
	s.area = newSurfaceArea(gl)
	if gl {
		gtkGLAreaSetRequiredVersion(s.area, 3, 3)
		// Only transparent windows show what is behind them. With an alpha
		// channel GTK keeps the area in a texture it blends, rather than in
		// a renderbuffer it copies, which took a small window 50 MB more of
		// the GPU's memory, and an animation half as much CPU again.
		gtkGLAreaSetHasAlpha(s.area, s.w.opts.Transparent)
		// GTK keeps the area's last frame as it paints the window, and
		// renders it again only as the content asks (queueDraw): rendering
		// on every paint, its default, built a frame for each one a web view
		// under the area painted.
		gtkGLAreaSetAutoRender(s.area, false)
		connect(s.area, "create-context", cbAreaContext, data)
		connect(s.area, "render", cbSurfaceRender, data)
	}
	s.gl = gl
	gtkWidgetSetCanFocus(s.area, true)
	gtkWidgetAddEvents(s.area, surfaceEvents)
	// The edges of a frameless window resize it, as over a page.
	connect(s.area, "button-press-event", cbButtonPress, data)
	if s.w.undecorated() {
		connect(s.area, "motion-notify-event", cbMotion, data)
	}
	connect(s.area, "draw", cbSurfaceDraw, data)
	connect(s.area, "size-allocate", cbSurfaceSize, data)
	connect(s.area, "realize", cbSurfaceRealize, data)
	connect(s.area, "unrealize", cbSurfaceUnrealize, data)
	connect(s.area, "notify::scale-factor", cbSurfaceScale, data)
	connect(s.area, "button-press-event", cbSurfaceButton, data)
	connect(s.area, "button-release-event", cbSurfaceButton, data)
	connect(s.area, "motion-notify-event", cbSurfaceMotion, data)
	connect(s.area, "leave-notify-event", cbSurfaceLeave, data)
	connect(s.area, "scroll-event", cbSurfaceScroll, data)
	connect(s.area, "event", cbSurfaceGesture, data)
	connect(s.area, "key-press-event", cbSurfaceKey, data)
	connect(s.area, "key-release-event", cbSurfaceKey, data)
	connect(s.area, "focus-in-event", cbSurfaceFocusIn, data)
	connect(s.area, "focus-out-event", cbSurfaceFocusOut, data)
}

// contentWidget returns the widget showing the window's content: the web
// view or the surface, in the overlay over its web views once it has
// some.
func (w *window) contentWidget() ptr {
	if s := w.surface; s != nil {
		if s.layers != 0 {
			return s.layers
		}
		return s.area
	}
	return w.web
}

// contentWindow returns the GdkWindow that receives the input of the
// window's content.
func (w *window) contentWindow() ptr {
	if w.surface != nil {
		return w.surface.eventWindow()
	}
	return gtkWidgetGetWindow(w.web)
}

func (s *surface) destroy() {
	s.cancelPendingDrop()
	s.CancelDataDrag()
	if s.lastPointer != 0 {
		gdkEventFree(s.lastPointer)
		s.lastPointer = 0
	}
	gtkIMContextSetClientWindow(s.im, 0)
	gObjectUnref(s.im)
	s.im = 0
	if s.lastKey != 0 {
		gdkEventFree(s.lastKey)
		s.lastKey = 0
	}
}

// popupTrigger returns the last press of a button or, in a surface, of a
// key in the window's content: the event a context menu shows for, whose
// time and serial GTK grabs the pointer with.
func (w *window) popupTrigger() ptr {
	ev := w.press.event
	if s := w.surface; s != nil && s.lastKey != 0 {
		// GdkEventButton and GdkEventKey: time 20.
		if ev == 0 || field[uint32](s.lastKey, 20)-field[uint32](ev, 20) < 1<<31 {
			ev = s.lastKey
		}
	}
	return ev
}

// Surface returns the surface of a window created with
// WindowOptions.Surface.
func (w *window) Surface() platform.Surface {
	if w.surface == nil {
		return nil
	}
	return w.surface
}

func (s *surface) Native() platform.SurfaceNative {
	n := platform.SurfaceNative{Widget: s.area}
	if s.rendering {
		n.GLArea = s.area
	}
	return n
}

// eventWindow returns the GdkWindow that receives the surface's input: the
// drawing area's own, or the input-only window a GtkGLArea, which draws in
// its parent's window, adds when realized.
func (s *surface) eventWindow() ptr {
	win := gtkWidgetGetWindow(s.area)
	if win == 0 || !s.gl {
		return win
	}
	// GList: data 0, next 8.
	for l := gdkWindowPeekChildren(win); l != 0; l = field[ptr](l, 8) {
		child := field[ptr](l, 0)
		var owner ptr
		gdkWindowGetUserData(child, &owner)
		if owner == s.area {
			return child
		}
	}
	return win
}

func (s *surface) Size() (float64, float64, float64) {
	return float64(gtkWidgetGetAllocatedWidth(s.area)), float64(gtkWidgetGetAllocatedHeight(s.area)), float64(max(gtkWidgetGetScaleFactor(s.area), 1))
}

func (s *surface) RequestFrame() {
	if !s.drawn && !s.hiddenFrame && !s.w.closed && !s.w.IsVisible() {
		s.hiddenFrame = true
		gIdleAddFull(0, cbDrawHidden, ptr(s.w.id), 0)
	}
	switch {
	case s.w.closed:
	case s.drawsGL() && s.rendering:
		// gtk_gl_area_draw clears the area's render request after it
		// emits the render signal (gtk/gtkglarea.c), so one asked for
		// within it is lost: ask at the next tick instead.
		s.renderAgain = true
	case s.drawsGL():
		s.queueDraw()
	default:
		s.again = true
		if s.tick == 0 {
			s.tick = gtkWidgetAddTickCallback(s.area, cbSurfaceTick, ptr(s.w.id), 0)
		}
	}
}

// FirstFrame tells that the content's first frame reached the surface
// (platform.FirstFrameSurface).
func (s *surface) FirstFrame() { s.drawn = true }

var _ platform.FirstFrameSurface = (*surface)(nil)

// drawHidden draws the content's first frame in a window hidden, which GTK
// neither paints nor ticks the frame clock of: the window is then ready to
// show. Realizing the window, without showing it, gives the area the size
// it shows at. The frame is drawn in memory, and shows nowhere: a
// GtkGLArea has no context before it shows, and once it does, the next
// frame, in its render signal, makes the GPU renderer, which this frame
// could not (SurfaceRenew). A window shown since draws as GTK paints it.
func (s *surface) drawHidden() {
	s.hiddenFrame = false
	if s.drawn || s.w.closed || s.w.IsVisible() {
		return
	}
	gtkWidgetRealize(s.w.win)
	s.send(platform.SurfaceEvent{Kind: platform.SurfaceFrame})
	if s.gl && s.drawn {
		s.send(platform.SurfaceEvent{Kind: platform.SurfaceRenew})
	}
}

// queueRenderLater asks the area to render at the next tick of its frame
// clock, after the render signal now running has returned.
func (s *surface) queueRenderLater() {
	if !s.renderPending && !s.w.closed {
		s.renderPending = true
		gtkWidgetAddTickCallback(s.area, cbRenderTick, ptr(s.w.id), 0)
	}
}

// queueDraw has the area paint again, a GtkGLArea rendering a frame in its
// render signal as it does.
func (s *surface) queueDraw() {
	if s.gl {
		gtkGLAreaQueueRender(s.area)
	} else {
		gtkWidgetQueueDraw(s.area)
	}
}

// drawing tells that the area draws a frame: in its tick callback, its
// draw signal or its render signal.
func (s *surface) drawing() bool { return s.ticking || s.cr != 0 || s.rendering }

// drawsGL tells that the area draws its frames with OpenGL, in its render
// signal: a GtkGLArea with a context.
func (s *surface) drawsGL() bool { return s.gl && gtkGLAreaGetError(s.area) == 0 }

// PresentDamage shows a frame drawn in memory that changed the last one
// only within damage. Drawn by the tick callback, it is painted where GTK
// repaints what changed; in the draw signal or the render signal, it is
// painted whole, and what it changed outside GTK's clip is repainted in
// the next frame.
func (s *surface) PresentDamage(pix []byte, stride, width, height int, damage []image.Rectangle) {
	scale := max(int(gtkWidgetGetScaleFactor(s.area)), 1)
	// GTK takes the area to repaint in the coordinates of the widget's
	// GdkWindow, whatever its documentation says: a GtkGLArea has none of
	// its own, and its parent's starts above and left of it, by the title
	// bar GTK draws, its shadow, or the menu bar.
	var at gdkRectangle
	if !gtkWidgetGetHasWindow(s.area) {
		gtkWidgetGetAllocation(s.area, &at)
	}
	queue := func(d image.Rectangle) {
		x0, y0 := d.Min.X/scale, d.Min.Y/scale
		gtkWidgetQueueDrawArea(s.area, at.X+int32(x0), at.Y+int32(y0), int32((d.Max.X+scale-1)/scale-x0), int32((d.Max.Y+scale-1)/scale-y0))
	}
	if !s.ticking {
		s.PresentPixels(pix, stride, width, height)
		if s.cr != 0 {
			var x0, y0, x1, y1 float64
			cairoClipExtents(s.cr, &x0, &y0, &x1, &y1)
			clip := image.Rect(int(math.Floor(x0))*scale, int(math.Floor(y0))*scale, int(math.Ceil(x1))*scale, int(math.Ceil(y1))*scale)
			for _, d := range damage {
				if !d.In(clip) {
					queue(d)
				}
			}
		}
		return
	}
	s.staged.pix, s.staged.stride, s.staged.width, s.staged.height = pix, stride, width, height
	for _, d := range damage {
		queue(d)
	}
}

// mallocTrim is glibc's malloc_trim, which other C libraries lack.
var mallocTrim, _ = purego.Dlsym(purego.RTLD_DEFAULT, "malloc_trim")

// Idle gives the system back the memory that C code freed while drawing
// frames, which glibc keeps for later: GTK paints a window it composites
// with OpenGL into an image as large as the window each frame, and glibc
// raises its threshold for giving large blocks back as they are freed.
func (s *surface) Idle() {
	s.staged.pix = nil // the host freed it
	if mallocTrim != 0 {
		purego.SyscallN(mallocTrim, 0)
	}
}

// RefreshRate returns the refresh rate of the monitor showing the area.
func (s *surface) RefreshRate() float64 { return float64(s.refreshMilliHz()) / 1000 }

// Occluded reports whether nothing of the window shows on screen: while it
// is minimized, as GDK tells on X11. Wayland tells no such thing, nor do
// windows covered by others.
func (s *surface) Occluded() bool { return s.w.state&stateIconified != 0 }

// refreshMilliHz returns the refresh rate of the monitor showing the area,
// in millihertz as GDK knows it, 0 when unknown: on Wayland the output the
// window last entered, on X11 the monitor with most of it.
func (s *surface) refreshMilliHz() int32 {
	win := gtkWidgetGetWindow(s.area)
	if s.w.closed || win == 0 {
		return 0
	}
	m := gdkDisplayGetMonitorAtWin(gdkWindowGetDisplay(win), win)
	if m == 0 {
		return 0
	}
	return max(gdkMonitorGetRefreshRate(m), 0)
}

// paceFrameClock tells GTK's frame clock how often the monitor showing the
// area refreshes, in the timings of the frame being drawn: the clock waits
// that long after a frame before the next (gdk/gdkframeclockidle.c), and
// 1/60 s while no frame's timings tell it. GDK tells it only from the
// compositor: on Wayland in the frame callback of a paint it commits
// itself, which it never asks for when it composites the window with
// OpenGL, as eglSwapBuffers commits, and on X11 from a compositor sending
// _NET_WM_FRAME_TIMINGS. Without, the GtkGLArea drew 60 frames a second
// on a 165 Hz display under Niri, as on X11 without a compositor
// (#1219). Each frame asks the monitor again, which follows the window to
// another; one whose rate is unknown keeps the clock's 60 Hz, and a
// compositor's rate arriving later replaces this one.
func (s *surface) paceFrameClock() {
	clock := gtkWidgetGetFrameClock(s.area)
	if clock == 0 {
		return
	}
	t := gdkFrameClockCurrentTimings(clock)
	if t == 0 || gdkFrameTimingsRefresh(t) != 0 {
		return
	}
	if us := refreshInterval(s.refreshMilliHz()); us > 0 {
		setRefreshInterval(t, us, gdkFrameTimingsRefresh)
	}
}

// refreshInterval returns the time between refreshes of a display of mhz
// millihertz in microseconds, as GDK computes it from a Wayland output's
// rate, 0 for an unknown rate.
func refreshInterval(mhz int32) int64 {
	if mhz <= 0 {
		return 0
	}
	return 1_000_000_000 / int64(mhz)
}

// refreshIntervalOffsets are where GdkFrameTimings, which GDK keeps
// private, may hold refresh_interval: after smoothed_frame_time, which
// GTK 3.24.24 added, else in its place.
var refreshIntervalOffsets = [...]uintptr{56, 48}

// refreshIntervalAt is the offset of the timings' refresh_interval, once
// a write there read back through its getter; -1 once none did.
var refreshIntervalAt int

// setRefreshInterval sets the refresh interval of frame timings t, which
// get (gdk_frame_timings_get_refresh_interval) reads as 0, to us, reporting
// whether it did: at the first offset whose write get reads back. A field
// holding a value is not the interval, and one that held 0 gets 0 back
// when get did not see the write.
func setRefreshInterval(t ptr, us int64, get func(t ptr) int64) bool {
	offsets := refreshIntervalOffsets[:]
	switch {
	case refreshIntervalAt < 0:
		return false
	case refreshIntervalAt > 0:
		offsets = []uintptr{uintptr(refreshIntervalAt)}
	}
	base := *(*unsafe.Pointer)(unsafe.Pointer(&t))
	for _, off := range offsets {
		f := (*int64)(unsafe.Add(base, off))
		if *f != 0 {
			continue
		}
		*f = us
		if get(t) == us {
			refreshIntervalAt = int(off)
			return true
		}
		*f = 0
	}
	if refreshIntervalAt == 0 {
		refreshIntervalAt = -1
	}
	return false
}

func (s *surface) PresentPixels(pix []byte, stride, width, height int) {
	if s.cr == 0 {
		if s.rendering {
			// The content draws in memory after all, as when its GL
			// renderer fails: GTK shows only what OpenGL draws.
			s.inMemory = true
			gl.MakeAreaCurrent(s.area)
			if err := s.present.Present(pix, stride, width, height); err != nil && !s.presentFailed {
				s.presentFailed = true
				log.Printf("mygo: native UI shows nothing: %v", err)
			}
			return
		}
		// Frames are drawn in the draw signal; outside of it, ask for one.
		s.queueDraw()
		return
	}
	if len(pix) < stride*height || width == 0 || height == 0 {
		return
	}
	const formatARGB32, operatorSource, operatorOver = 0, 1, 2 // premultiplied, native endian: BGRA
	img := cairoImageSurfaceForData(&pix[0], formatARGB32, int32(width), int32(height), int32(stride))
	scale := float64(max(gtkWidgetGetScaleFactor(s.area), 1))
	cairoSurfaceSetDeviceScale(img, scale, scale)
	cairoSetSourceSurface(s.cr, img, 0, 0)
	op := int32(operatorSource)
	if s.layers != 0 {
		op = operatorOver // over the web views, which holes show
	}
	cairoSetOperator(s.cr, op)
	cairoPaint(s.cr)
	cairoSurfaceDestroy(img)
}

var cursorNames = map[platform.Cursor]string{
	platform.CursorDefault: "default", platform.CursorPointer: "pointer", platform.CursorText: "text",
	platform.CursorMove: "move", platform.CursorResizeEW: "ew-resize", platform.CursorResizeNS: "ns-resize",
	platform.CursorResizeNWSE: "nwse-resize", platform.CursorResizeNESW: "nesw-resize",
	platform.CursorNotAllowed: "not-allowed", platform.CursorCrosshair: "crosshair",
	platform.CursorGrab: "grab", platform.CursorGrabbing: "grabbing",
	platform.CursorResizeN: "n-resize", platform.CursorResizeE: "e-resize", platform.CursorResizeS: "s-resize",
	platform.CursorResizeW: "w-resize", platform.CursorResizeColumn: "col-resize", platform.CursorResizeRow: "row-resize",
	platform.CursorVerticalText: "vertical-text", platform.CursorCopy: "copy", platform.CursorAlias: "alias",
	platform.CursorContextMenu: "context-menu", platform.CursorNone: "none",
	platform.CursorProgress: "progress", platform.CursorWait: "wait", platform.CursorHelp: "help", platform.CursorCell: "cell",
	platform.CursorZoomIn: "zoom-in", platform.CursorZoomOut: "zoom-out", platform.CursorAllScroll: "all-scroll",
	platform.CursorNoDrop: "no-drop",
}

// cursorFallbacks are the nearest cursors for those a cursor theme may
// lack, which GDK finds no X cursor for either.
var cursorFallbacks = map[platform.Cursor]string{
	platform.CursorProgress: "wait", platform.CursorCell: "crosshair",
	platform.CursorAllScroll: "move", platform.CursorNoDrop: "not-allowed",
}

func (s *surface) SetCursor(c platform.Cursor) {
	s.cursor = c
	win := s.eventWindow()
	if win == 0 || s.w.cursor.on {
		return // a resize cursor of the edges shows
	}
	cur, ok := surfaceCursors[c]
	if !ok {
		name := cursorNames[c]
		if name == "" {
			name = "default"
		}
		cur = gdkCursorNewFromName(gtkWidgetGetDisplay(s.area), cs(name))
		if fallback := cursorFallbacks[c]; cur == 0 && fallback != "" {
			cur = gdkCursorNewFromName(gtkWidgetGetDisplay(s.area), cs(fallback))
		}
		surfaceCursors[c] = cur
	}
	gdkWindowSetCursor(win, cur)
}

func (s *surface) SetTextInput(t platform.TextInputState) {
	active, caret := t.Active, t.Caret
	if s.textInput && (!active || s.input.Client != t.Client || t.NoInputMethod) {
		s.clientComposition.Reset()
		gtkIMContextReset(s.im)
	}
	// Without input methods, the input context loses the focus, as a
	// Wayland compositor's input method grabs the keys while it has it,
	// and keys type the layout's characters (typed), as winit's do.
	if was, raw := s.rawInput(), active && t.NoInputMethod; raw != was {
		if raw {
			gtkIMContextFocusOut(s.im)
		} else if gtkWidgetHasFocus(s.area) {
			gtkIMContextFocusIn(s.im)
		}
	}
	s.input = t
	s.textInput, s.caret = active, caret
	if active {
		r := gdkRectangle{X: int32(caret.X), Y: int32(caret.Y), Width: max(int32(caret.W), 1), Height: int32(caret.H + 0.5)}
		gtkIMContextSetCursorLoc(s.im, &r)
	}
}

// rawInput reports whether the focused element takes text with input
// methods off (NoInputMethod).
func (s *surface) rawInput() bool { return s.textInput && s.input.NoInputMethod }

// typed sends the text a key press types with input methods off: the
// character of its keyval, unless a modifier makes it a shortcut, as
// GtkIMContextSimple has it.
func (s *surface) typed(event ptr) {
	if gdkMods(field[uint32](event, 24))&(platform.ModCtrl|platform.ModAlt|platform.ModSuper) != 0 {
		return
	}
	r := rune(gdkKeyvalToUnicode(field[uint32](event, 28)))
	if r < 0x20 || r == 0x7F {
		return
	}
	s.commit(string(r))
}

// commit sends text typed or committed by an input method.
func (s *surface) commit(text string) {
	if c := s.input.Client; c != nil {
		s.clientComposition.Replace(c, nil, text)
		return
	}
	if text != "" {
		s.send(platform.SurfaceEvent{Kind: platform.TextInput, Text: text})
	}
}

func (s *surface) send(ev platform.SurfaceEvent) bool {
	if ev.Kind == platform.PointerDown || ev.Kind == platform.SurfaceBlur {
		s.clientComposition.Reset()
	}
	if !s.held.Track(ev) {
		return false
	}
	if s.w.closed {
		return false
	}
	return s.w.h.SurfaceEvent(ev)
}

// grabBroken handles a GdkEventGrabBroken (GDK_GRAB_BROKEN, 35: type 0,
// keyboard 20): the pointer's grab of a press, implicit or not, went to
// another window, as a menu popping up, or ended as the window hid, and
// the release will not come here. The press ends with none
// (platform.PointerCancel), unless it starts a data drag (StartDataDrag),
// whose end ends it.
func (s *surface) grabBroken(event ptr) {
	if field[int32](event, 20) != 0 || s.dragRequest != nil {
		return // the keyboard's grab, or the drag's
	}
	if s.lastPointer != 0 {
		gdkEventFree(s.lastPointer)
		s.lastPointer = 0
	}
	s.send(platform.SurfaceEvent{Kind: platform.PointerCancel})
}

// touchEvent gives a GdkEventTouch (GDK_TOUCH_BEGIN 37, UPDATE 38, END
// 39, CANCEL 40) to the surface's gesture and sends what it does.
// GdkEventTouch, as GdkEventButton up to state: type 0, time 20, x 24,
// y 32, state 48, sequence 56, emulating_pointer 64.
func (s *surface) touchEvent(typ int32, event ptr) {
	s.touched = true
	seq, x, y, t := field[ptr](event, 56), field[float64](event, 24), field[float64](event, 32), field[uint32](event, 20)
	var evs []platform.SurfaceEvent
	switch typ {
	case 37:
		if field[int32](event, 64) != 0 {
			// The first finger of new touches: no other is down, whatever
			// ended unseen, as under a grab.
			s.touch.fingers = s.touch.fingers[:0]
		}
		evs = s.touch.begin(seq, x, y, t)
	case 38:
		evs = s.touch.move(seq, x, y)
	default:
		evs = s.touch.end(seq, x, y, t, typ == 40)
	}
	mods := gdkMods(field[uint32](event, 48))
	pressure := eventPressure(event)
	for _, ev := range evs {
		ev.Mods = mods
		ev.Pointer, ev.Pressure = platform.PointerTouch, pressure
		if ev.Kind == platform.PointerDown {
			if !gtkWidgetHasFocus(s.area) {
				gtkWidgetGrabFocus(s.area)
			}
			// Taps land further apart than clicks.
			dc := doubleClick()
			dc.DX, dc.DY = max(dc.DX, touchSlop), max(dc.DY, touchSlop)
			ev.Clicks = s.clicks.Press(ev.Button, ev.X, ev.Y, t, dc)
			// A menu the tap opens shows for it.
			if s.w.press.event != 0 {
				gdkEventFree(s.w.press.event)
			}
			s.w.press.event = gdkEventCopy(event)
		}
		s.send(ev)
	}
}

// eventDevice returns the device of a button or motion event, by its
// source device, and the pressure of its axes: a tablet's pen or eraser,
// or a touchscreen's finger GTK makes a mouse of, else a mouse or a
// touchpad, whose pressure the content leaves to Pointer Events' default.
func eventDevice(event ptr) (platform.PointerKind, float64) {
	k := platform.PointerMouse
	if d := gdkEventGetSourceDevice(event); d != 0 {
		switch gdkDeviceGetSource(d) {
		case 1, 2: // GDK_SOURCE_PEN, GDK_SOURCE_ERASER
			k = platform.PointerPen
		case 5: // GDK_SOURCE_TOUCHSCREEN
			k = platform.PointerTouch
		}
	}
	if k == platform.PointerMouse {
		return k, 0
	}
	return k, eventPressure(event)
}

// eventPressure returns the pressure of an event's axes, which GDK
// scales from 0 to 1 (GDK_AXIS_PRESSURE), or 0 for none.
func eventPressure(event ptr) float64 {
	var v float64
	if !gdkEventGetAxis(event, 3, &v) {
		return 0
	}
	return v
}

// emulated tells that a button or motion event is the mouse GTK makes of
// a finger, once the area has had touches of its own.
func (s *surface) emulated(event ptr) bool {
	if !s.touched {
		return false
	}
	d := gdkEventGetSourceDevice(event)
	return d != 0 && gdkDeviceGetSource(d) == 5 // GDK_SOURCE_TOUCHSCREEN
}

// scrollEvent converts a GdkEventScroll to the content's scroll, at its
// point moved by ox, oy: from the window it came to, a web view's, to the
// area's. GdkEventScroll: x 24, y 32, state 40, direction 44, delta_x 72,
// delta_y 80.
func scrollEvent(event ptr, ox, oy float64) platform.SurfaceEvent {
	ev := platform.SurfaceEvent{Kind: platform.PointerScroll, X: ox + field[float64](event, 24), Y: oy + field[float64](event, 32), Mods: gdkMods(field[uint32](event, 40))}
	const step = 50
	switch field[int32](event, 44) {
	case 0:
		ev.DY = -step
	case 1:
		ev.DY = step
	case 2:
		ev.DX = -step
	case 3:
		ev.DX = step
	case 4: // smooth
		dx, dy := field[float64](event, 72), field[float64](event, 80)
		ev.DX, ev.DY = dx*step, dy*step
		ev.Precise = dx != float64(int64(dx)) || dy != float64(int64(dy))
	}
	return ev
}

func gdkMods(state uint32) platform.Modifiers {
	var m platform.Modifiers
	if state&1 != 0 {
		m |= platform.ModShift
	}
	if state&(1<<2) != 0 {
		m |= platform.ModCtrl
	}
	if state&(1<<3) != 0 {
		m |= platform.ModAlt
	}
	if state&(1<<26|1<<28) != 0 {
		m |= platform.ModSuper
	}
	return m
}

// modifierKeyval is the modifier a GDK keyval presses: Shift_L/R, Control_L/R,
// Alt_L/R and Meta_L/R, Super_L/R and Hyper_L/R; 0 for other keys.
func modifierKeyval(keyval uint32) platform.Modifiers {
	switch keyval {
	case 0xffe1, 0xffe2:
		return platform.ModShift
	case 0xffe3, 0xffe4:
		return platform.ModCtrl
	case 0xffe9, 0xffea, 0xffe7, 0xffe8:
		return platform.ModAlt
	case 0xffeb, 0xffec, 0xffed, 0xffee:
		return platform.ModSuper
	}
	return 0
}

var gdkKeys = map[uint32]platform.Key{
	0xff0d: platform.KeyEnter, 0xff8d: platform.KeyEnter, 0xff1b: platform.KeyEscape, 0xff08: platform.KeyBackspace,
	0xff09: platform.KeyTab, 0xfe20: platform.KeyTab, 0x20: platform.KeySpace, 0xffff: platform.KeyDelete,
	0xff9f: platform.KeyDelete, 0xff63: platform.KeyInsert, 0xff50: platform.KeyHome, 0xff95: platform.KeyHome,
	0xff57: platform.KeyEnd, 0xff9c: platform.KeyEnd, 0xff55: platform.KeyPageUp, 0xff9a: platform.KeyPageUp,
	0xff56: platform.KeyPageDown, 0xff9b: platform.KeyPageDown, 0xff51: platform.KeyLeft, 0xff96: platform.KeyLeft,
	0xff52: platform.KeyUp, 0xff97: platform.KeyUp, 0xff53: platform.KeyRight, 0xff98: platform.KeyRight,
	0xff54: platform.KeyDown, 0xff99: platform.KeyDown, 0xff67: platform.KeyContextMenu,
	0x1008ff26: platform.KeyBack, 0x1008ff27: platform.KeyForward, // XF86Back, XF86Forward
}

func keyvalKey(keyval uint32) platform.Key {
	if k, ok := gdkKeys[keyval]; ok {
		return k
	}
	if keyval >= 0xffbe && keyval <= 0xffc9 {
		return platform.KeyF1 + platform.Key(keyval-0xffbe)
	}
	if r := gdkKeyvalToUnicode(keyval); r != 0 {
		return platform.KeyForRune(rune(r))
	}
	return platform.KeyUnknown
}

// eventPhysicalKey reads GdkEventKey.hardware_keycode on the supported
// 64-bit GTK ABI. X11 and Wayland expose the evdev code plus 8 here.
func eventPhysicalKey(event ptr) platform.Key {
	if code := field[uint16](event, 48); code >= 8 {
		return platform.KeyForScancode(code - 8)
	}
	return platform.KeyUnknown
}

// eventKeyCode reads the key by position from GdkEventKey.hardware_keycode,
// as eventPhysicalKey does, on any keyboard.
func eventKeyCode(event ptr) platform.KeyCode {
	if code := field[uint16](event, 48); code >= 8 {
		return platform.KeyCodeForEvdev(code - 8)
	}
	return platform.CodeUnknown
}

// keyEvent handles a GdkEventKey (type 0, state 24, keyval 28,
// hardware_keycode 48), and reports whether it stops there.
func (s *surface) keyEvent(event ptr) bool {
	kind := platform.KeyPressed
	repeat := false
	keycode := field[uint16](event, 48)
	if field[int32](event, 0) == 9 { // GDK_KEY_RELEASE
		kind = platform.KeyReleased
		s.down.release(keycode)
	} else {
		// Before the input method sees it, which may take the press
		// or the release.
		repeat = s.down.press(keycode)
		s.clientComposition.Reset()
		if s.lastKey != 0 {
			gdkEventFree(s.lastKey)
		}
		s.lastKey = gdkEventCopy(event)
	}
	if s.rawInput() {
		if kind == platform.KeyPressed {
			defer s.typed(event)
		}
	} else if s.textInput && gtkIMContextFilterKeypress(s.im, event) {
		if kind == platform.KeyReleased {
			// The input method took the release of a key the content
			// got pressed, as the focus moved to a text input while
			// the key was held: the key goes up all the same. One it
			// hands back, as IBus and Fcitx do the keys they leave,
			// comes again, which held keeps from the content.
			s.held.Lift(keyvalKey(field[uint32](event, 28)), eventPhysicalKey(event), gdkMods(field[uint32](event, 24)), s.send)
		}
		return true
	}
	// A modifier key on its own: the state is the one before the
	// event, so the key's own bit goes in or out, unless the same
	// modifier of the other side stays held.
	if bit := modifierKeyval(field[uint32](event, 28)); bit != 0 {
		state := gdkMods(field[uint32](event, 24))
		if kind == platform.KeyPressed || s.down.otherSideHeld(keycode) {
			state |= bit
		} else {
			state &^= bit
		}
		s.send(platform.SurfaceEvent{Kind: platform.ModifiersChanged, Mods: state})
		return s.rawKey(kind, event, state, repeat)
	}
	k := keyvalKey(field[uint32](event, 28))
	mods := gdkMods(field[uint32](event, 24))
	physical := eventPhysicalKey(event)
	code := eventKeyCode(event)
	// A key the layout types no US key for, as S on a Cyrillic layout,
	// still reaches shortcuts by its position.
	// A release is sent whatever the modifiers held now, so that a press
	// sent as a chord is let go of even after Ctrl was.
	if k == platform.KeyUnknown {
		switch {
		case kind == platform.KeyReleased && physical != platform.KeyUnknown:
			s.send(platform.SurfaceEvent{Kind: kind, PhysicalKey: physical, Code: code, Mods: mods})
			return false
		case !platform.KeyByPosition(k, physical, mods):
			return s.rawKey(kind, event, mods, repeat)
		}
	}
	if kind == platform.KeyPressed {
		s.pressing = event
		defer func() { s.pressing = 0 }()
	}
	s.send(platform.SurfaceEvent{Kind: kind, Key: k, PhysicalKey: physical, Code: code, Mods: mods, Repeat: repeat})
	return true
}

// rawKey sends a key shortcuts have no name for, whose keyval tells nothing
// (a modifier, a lock key, one the layout types no US key for), by its
// position, and reports whether the content took it.
func (s *surface) rawKey(kind platform.SurfaceEventKind, event ptr, mods platform.Modifiers, repeat bool) bool {
	code := eventKeyCode(event)
	if code == platform.CodeUnknown {
		return false
	}
	raw := platform.RawKeyPressed
	if kind == platform.KeyReleased {
		raw = platform.RawKeyReleased
	}
	return s.send(platform.SurfaceEvent{Kind: raw, PhysicalKey: eventPhysicalKey(event), Code: code, Mods: mods, Repeat: repeat})
}

func (b *Backend) surfaceOf(data ptr) *surface {
	if w := b.window(data); w != nil {
		return w.surface
	}
	return nil
}

func initSurfaceCallbacks() {
	b := func() *Backend { return theBackend }
	cbSurfaceDraw = purego.NewCallback(func(widget, cr, data ptr) bool {
		s := b().surfaceOf(data)
		if s == nil {
			return true
		}
		// A GtkGLArea draws in its render signal, unless it has no context:
		// then, as a drawing area, in this one.
		if s.drawsGL() {
			return false
		}
		s.paceFrameClock()
		s.cr = cr
		// The frame the tick callback drew, unless GTK resized the area
		// since; else GTK asked for this draw by itself, and the content
		// draws a frame now.
		st := &s.staged
		w, h, scale := s.Size()
		if st.pix != nil && st.width == int(math.Ceil(w*scale)) && st.height == int(math.Ceil(h*scale)) {
			s.PresentPixels(st.pix, st.stride, st.width, st.height)
		} else {
			s.send(platform.SurfaceEvent{Kind: platform.SurfaceFrame})
		}
		st.pix = nil
		s.cr = 0
		return true
	})
	// The tick callback draws the frames asked for in memory, before GTK
	// paints, as long as they are asked for.
	cbSurfaceTick = purego.NewCallback(func(widget, clock, data ptr) bool {
		s := b().surfaceOf(data)
		if s == nil {
			return false
		}
		if s.drawsGL() {
			// The area has its context now: frames come from its render
			// signal.
			s.tick = 0
			s.queueDraw()
			return false
		}
		s.again, s.ticking, s.staged.pix = false, true, nil
		s.paceFrameClock()
		s.send(platform.SurfaceEvent{Kind: platform.SurfaceFrame})
		s.ticking = false
		if !s.again {
			s.tick = 0
		}
		return s.again
	})
	// The GtkGLArea's context: OpenGL 3.3, else OpenGL ES 3.0. One GDK
	// cannot make leaves the area its error, so that it draws with cairo,
	// without the context GTK's own handler would make.
	cbAreaContext = purego.NewCallback(func(area, data ptr) ptr {
		ctx, gerr := glContext(gtkWidgetGetWindow(area))
		if gerr != 0 {
			gtkGLAreaSetError(area, gerr)
			gErrorFree(gerr)
		}
		return ctx
	})
	// The toplevel's draw signal, before GTK draws it: the window paints.
	cbWindowGLDamage = purego.NewCallback(func(widget, cr, data ptr) bool {
		if w := b().window(data); w != nil {
			w.damageGLPaint(cr)
		}
		return false
	})
	cbDrawHidden = purego.NewCallback(func(data ptr) int32 {
		if s := b().surfaceOf(data); s != nil {
			s.drawHidden()
		}
		return 0 // G_SOURCE_REMOVE
	})
	cbRenderTick = purego.NewCallback(func(widget, clock, data ptr) bool {
		if s := b().surfaceOf(data); s != nil {
			s.renderPending = false
			s.queueDraw()
		}
		return false
	})
	cbSurfaceRender = purego.NewCallback(func(area, context, data ptr) bool {
		s := b().surfaceOf(data)
		if s == nil {
			return false
		}
		s.rendering, s.rendered, s.inMemory = true, true, false
		s.paceFrameClock()
		s.send(platform.SurfaceEvent{Kind: platform.SurfaceFrame})
		s.rendering = false
		if s.renderAgain {
			s.renderAgain = false
			s.queueRenderLater()
		}
		return true
	})
	cbSurfaceSize = purego.NewCallback(func(widget, allocation, data ptr) {
		if s := b().surfaceOf(data); s != nil {
			if s.layers != 0 {
				s.shape() // to the new size
			}
			s.send(platform.SurfaceEvent{Kind: platform.SurfaceResize})
		}
	})
	cbSurfaceScale = purego.NewCallback(func(widget, pspec, data ptr) {
		if s := b().surfaceOf(data); s != nil {
			s.send(platform.SurfaceEvent{Kind: platform.SurfaceResize})
			s.queueDraw()
		}
	})
	// Input methods let go of the GdkWindow before it goes, as GtkEntry
	// has them: fcitx5's disconnects from it.
	cbSurfaceUnrealize = purego.NewCallback(func(widget, data ptr) {
		if s := b().surfaceOf(data); s != nil {
			gtkIMContextSetClientWindow(s.im, 0)
		}
	})
	cbSurfaceRealize = purego.NewCallback(func(widget, data ptr) {
		if s := b().surfaceOf(data); s != nil {
			gtkIMContextSetClientWindow(s.im, s.eventWindow())
			if s.layers != 0 {
				s.shape() // the new window has none
			}
			if s.cursor != platform.CursorDefault {
				s.SetCursor(s.cursor) // on the area that took another's place
			}
			if s.gl {
				s.queueDraw() // its frame went with the context it unrealized
			}
		}
	})
	// GdkEventButton: type 0, time 20, x 24, y 32, state 48, button 52.
	cbSurfaceButton = purego.NewCallback(func(widget, event, data ptr) bool {
		s := b().surfaceOf(data)
		if s == nil {
			return false
		}
		typ := field[int32](event, 0)
		if typ != 4 && typ != 7 || s.emulated(event) { // GDK_BUTTON_PRESS, GDK_BUTTON_RELEASE: GTK's own double click events add nothing
			return true
		}
		n, mods := field[uint32](event, 52), gdkMods(field[uint32](event, 48))
		if n == 8 || n == 9 {
			// The side buttons go back and forward, as keys do.
			kind, k := platform.KeyPressed, platform.KeyBack
			if typ == 7 {
				kind = platform.KeyReleased
			}
			if n == 9 {
				k = platform.KeyForward
			}
			s.send(platform.SurfaceEvent{Kind: kind, Key: k, Mods: mods})
			return true
		}
		button, ok := map[uint32]int{1: 0, 2: 2, 3: 1}[n]
		if !ok {
			return true
		}
		kind := platform.PointerDown
		if typ == 7 {
			kind = platform.PointerUp
		} else if !gtkWidgetHasFocus(s.area) {
			gtkWidgetGrabFocus(s.area)
		}
		if kind == platform.PointerDown && button == 0 {
			if s.lastPointer != 0 {
				gdkEventFree(s.lastPointer)
			}
			s.lastPointer = gdkEventCopy(event)
		}
		x, y := field[float64](event, 24), field[float64](event, 32)
		clicks := 0
		if kind == platform.PointerDown {
			clicks = s.clicks.Press(button, x, y, field[uint32](event, 20), doubleClick())
		}
		pointer, pressure := eventDevice(event)
		s.send(platform.SurfaceEvent{Kind: kind, X: x, Y: y, Button: button, Clicks: clicks, Mods: mods, Pointer: pointer, Pressure: pressure})
		if kind == platform.PointerUp && button == 0 && s.dragRequest == nil && s.lastPointer != 0 {
			gdkEventFree(s.lastPointer)
			s.lastPointer = 0
		}
		return true
	})
	// GdkEventMotion: x 24, y 32, state 48.
	cbSurfaceMotion = purego.NewCallback(func(widget, event, data ptr) bool {
		if s := b().surfaceOf(data); s != nil && !s.emulated(event) {
			if field[uint32](event, 48)&(1<<8) != 0 {
				if s.lastPointer != 0 {
					gdkEventFree(s.lastPointer)
				}
				s.lastPointer = gdkEventCopy(event)
			}
			pointer, pressure := eventDevice(event)
			s.send(platform.SurfaceEvent{Kind: platform.PointerMove, X: field[float64](event, 24), Y: field[float64](event, 32), Mods: gdkMods(field[uint32](event, 48)), Pointer: pointer, Pressure: pressure})
		}
		return false
	})
	// GdkEventCrossing: mode 72; only normal crossings leave.
	cbSurfaceLeave = purego.NewCallback(func(widget, event, data ptr) bool {
		if s := b().surfaceOf(data); s != nil && field[int32](event, 72) == 0 {
			s.send(platform.SurfaceEvent{Kind: platform.PointerLeave})
		}
		return false
	})
	cbSurfaceScroll = purego.NewCallback(func(widget, event, data ptr) bool {
		s := b().surfaceOf(data)
		if s == nil {
			return false
		}
		s.send(scrollEvent(event, 0, 0))
		return true
	})
	// The touchscreen's touches, before GTK's touch-event handler, the
	// touchpad's pinches, which have no signal of their own in GTK 3, and
	// the pointer's grab broken.
	cbSurfaceGesture = purego.NewCallback(func(widget, event, data ptr) bool {
		typ := field[int32](event, 0)
		if typ == 35 {
			if s := b().surfaceOf(data); s != nil {
				s.grabBroken(event)
			}
			return false
		}
		if typ < 37 || typ > 40 && typ != 42 {
			return false
		}
		s := b().surfaceOf(data)
		if s == nil {
			return false
		}
		if typ != 42 {
			// Taken here, the touch reaches no touch-event handler: GTK's
			// own makes the first finger the mouse.
			s.touchEvent(typ, event)
			return true
		}
		// GdkEventTouchpadPinch (GDK_TOUCHPAD_PINCH, 42): type 0, phase 17,
		// x 24, y 32, scale 64 (from 1 as the pinch began), state 88. GDK
		// reports pinches on Wayland; X11 has none.
		scale := field[float64](event, 64)
		switch field[int8](event, 17) {
		case 0: // begin
			s.pinch = 1
		case 1: // update
			if s.pinch > 0 && scale > 0 {
				s.send(platform.SurfaceEvent{Kind: platform.PointerZoom, X: field[float64](event, 24), Y: field[float64](event, 32), Scale: scale / s.pinch, Mods: gdkMods(field[uint32](event, 88))})
			}
			s.pinch = scale
		default: // end, cancel
			s.pinch = 0
		}
		return true
	})
	cbSurfaceKey = purego.NewCallback(func(widget, event, data ptr) bool {
		s := b().surfaceOf(data)
		return s != nil && s.keyEvent(event)
	})
	cbSurfaceFocusIn = purego.NewCallback(func(widget, event, data ptr) bool {
		if s := b().surfaceOf(data); s != nil {
			if !s.rawInput() {
				gtkIMContextFocusIn(s.im)
			}
			s.send(platform.SurfaceEvent{Kind: platform.SurfaceFocus})
		}
		return false
	})
	cbSurfaceFocusOut = purego.NewCallback(func(widget, event, data ptr) bool {
		if s := b().surfaceOf(data); s != nil {
			gtkIMContextFocusOut(s.im)
			s.down.clear()
			s.send(platform.SurfaceEvent{Kind: platform.SurfaceBlur})
		}
		return false
	})
	cbIMCommit = purego.NewCallback(func(im, str, data ptr) {
		if s := b().surfaceOf(data); s != nil {
			s.commit(goStr(str))
		}
	})
	cbIMPreedit = purego.NewCallback(func(im, data ptr) {
		s := b().surfaceOf(data)
		if s == nil {
			return
		}
		var str ptr
		var cursor int32
		gtkIMContextGetPreedit(s.im, &str, nil, &cursor)
		text := takeStr(str)
		if c := s.input.Client; c != nil {
			s.clientComposition.Reset()
			caret := platform.UTF16Len(string([]rune(text)[:min(int(cursor), len([]rune(text)))]))
			c.SetMarkedText(nil, text, platform.TextRange{Start: caret, End: caret})
			return
		}
		s.send(platform.SurfaceEvent{Kind: platform.TextComposition, Text: text, Caret: min(int(cursor), utf8.RuneCountInString(text))})
	})
	cbIMPreeditEnd = purego.NewCallback(func(im, data ptr) {
		if s := b().surfaceOf(data); s != nil {
			if c := s.input.Client; c != nil {
				s.clientComposition.End(c)
				return
			}
			s.send(platform.SurfaceEvent{Kind: platform.TextComposition})
		}
	})
}
