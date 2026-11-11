//go:build linux && (amd64 || arm64)

package linux

import (
	"image"

	"github.com/egoist/mygo/internal/gpu/gl"
)

// TestSurfaceGL tells how the native UI of a window draws: "opengl" when
// its renderer draws in the GtkGLArea, "memory" when the GtkGLArea shows
// frames drawn on the CPU, "cairo" when a drawing area, or a GtkGLArea
// without a context, paints them with cairo, and "" before the first frame.
// For a GtkGLArea it returns its framebuffer's pixels too, premultiplied
// BGRA rows from the top, and their size.
func TestSurfaceGL(handle uintptr) (how string, pix []byte, width, height int) {
	s := surfaceByHandle(handle)
	switch {
	case s == nil:
		return "", nil, 0, 0
	case !s.gl || gtkGLAreaGetError(s.area) != 0:
		return "cairo", nil, 0, 0
	case !s.rendered:
		return "", nil, 0, 0
	}
	how = "opengl"
	if s.inMemory {
		how = "memory"
	}
	var makeCurrent, attachBuffers func(area ptr)
	if !bind(libGTK, &makeCurrent, "gtk_gl_area_make_current") || !bind(libGTK, &attachBuffers, "gtk_gl_area_attach_buffers") {
		return how, nil, 0, 0
	}
	// The area's framebuffer keeps the last frame it rendered.
	makeCurrent(s.area)
	attachBuffers(s.area)
	scale := int(max(gtkWidgetGetScaleFactor(s.area), 1))
	width, height = int(gtkWidgetGetAllocatedWidth(s.area))*scale, int(gtkWidgetGetAllocatedHeight(s.area))*scale
	pix, err := gl.ReadFramebuffer(width, height)
	if err != nil {
		return how, nil, 0, 0
	}
	return how, pix, width, height
}

// TestSurfaceOnScreen returns, as a PNG, what the display shows of the
// surface of a window: GTK repaints only what frames drawn in memory
// changed, and keeps the rest.
func TestSurfaceOnScreen(handle uintptr) []byte {
	s := surfaceByHandle(handle)
	var getFromWindow func(win ptr, x, y, width, height int32) ptr
	if s == nil || !bind(libGDK, &getFromWindow, "gdk_pixbuf_get_from_window") {
		return nil
	}
	// The area's GdkWindow is its parent's when it has none of its own.
	var at gdkRectangle
	if !gtkWidgetGetHasWindow(s.area) {
		gtkWidgetGetAllocation(s.area, &at)
	}
	pix := getFromWindow(gtkWidgetGetWindow(s.area), at.X, at.Y, gtkWidgetGetAllocatedWidth(s.area), gtkWidgetGetAllocatedHeight(s.area))
	if pix == 0 {
		return nil
	}
	defer gObjectUnref(pix)
	return pngFromPixbuf(pix)
}

// TestProbeGL probes OpenGL as the first window of native UI does, and
// returns what it found and a function that reports whether GDK freed the
// probe's window; nil where GDK keeps it: GTK 3 on Wayland never drops a
// reference of its own.
func TestProbeGL() (renderer, failure string, freed func() bool) {
	var malloc0 func(n uintptr) ptr
	var addWeakPointer func(obj, location ptr)
	if !theBackend.onX11 || !bind(libGLib, &malloc0, "g_malloc0") || !bind(libGObject, &addWeakPointer, "g_object_add_weak_pointer") {
		return "", "", nil
	}
	// GObject clears the location as it finalizes the window. It stays
	// allocated: a window GDK still had when the test ended may go later.
	var location ptr
	renderer, failure = probeGLWith(func(win ptr) {
		location = malloc0(8)
		*slot(location, 0) = win
		addWeakPointer(win, location)
	})
	if location == 0 {
		return renderer, failure, nil
	}
	return renderer, failure, func() bool { return *slot(location, 0) == 0 }
}

// TestGLDamage returns what the paints of a window of native UI drawing
// with OpenGL told a Wayland compositor they changed since the last call,
// beside GTK's own damage (damageGLPaint), in the window's coordinates;
// where its GtkGLArea is in them; and the window's scale. The first call
// starts recording. It returns ok false but on Wayland, for a GtkGLArea.
func TestGLDamage(handle uintptr) (damage []image.Rectangle, area image.Point, scale int, ok bool) {
	w := windowByHandle(handle)
	if w == nil || w.surface == nil || !w.surface.gl || w.b.waylandSurface == nil {
		return nil, image.Point{}, 0, false
	}
	if testGLDamage == nil {
		testGLDamaged = map[*window][]image.Rectangle{}
		testGLDamage = func(w *window, r image.Rectangle) { testGLDamaged[w] = append(testGLDamaged[w], r) }
	}
	damage, testGLDamaged[w] = testGLDamaged[w], nil
	// The area has no GdkWindow of its own: its allocation is in the
	// window's.
	var at gdkRectangle
	gtkWidgetGetAllocation(w.surface.area, &at)
	return damage, image.Pt(int(at.X), int(at.Y)), int(gtkWidgetGetScaleFactor(w.win)), true
}

// testGLDamaged holds what TestGLDamage recorded, by window.
var testGLDamaged map[*window][]image.Rectangle

// TestFrameClockInterval returns the refresh interval, in microseconds, in
// the timings of the last frame of the frame clock of a window showing
// native UI, which the clock waits after it for the next (paceFrameClock),
// and the refresh rate of the monitor showing the window in millihertz.
func TestFrameClockInterval(handle uintptr) (interval int64, mhz int32) {
	s := surfaceByHandle(handle)
	if s == nil {
		return 0, 0
	}
	if clock := gtkWidgetGetFrameClock(s.area); clock != 0 {
		if t := gdkFrameClockCurrentTimings(clock); t != 0 {
			interval = gdkFrameTimingsRefresh(t)
		}
	}
	return interval, s.refreshMilliHz()
}
