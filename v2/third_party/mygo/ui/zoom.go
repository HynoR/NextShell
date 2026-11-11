package ui

import (
	"math"

	"github.com/egoist/mygo/internal/platform"
)

// The zooms SetZoom takes; others are clamped to them.
const (
	minZoom = 0.25
	maxZoom = 5
)

// SetZoom zooms the window's content by z, as a browser zooms a page: the
// view is laid out in a window 1/z its size and shown z times larger,
// drawn at the screen's resolution times z, so that text, icons, vectors
// and images stay as sharp as at that size natively, not a picture of
// the window scaled up. 1 shows the content at its size; the zoom
// stays until set again, and is clamped to between 0.25 and 5. A frame
// that sets another builds again at the new size before it shows.
//
// What the view sees is zoomed: Size, TitleBar, Bounds and every length
// are in the content's DIPs, z of them to the window's. The pointer, text
// input, input methods, accessibility, context menus, drags and web views
// follow it. The window's own sizes are not zoomed: Window.SetContentSize
// and Window.ContentSize are in the window's DIPs, z times the content's.
//
//	c.SetZoom(app.zoom) // 1.25 for 125%
func (c *context) SetZoom(z float32) { c.rt.setZoom(z) }

// Zoom returns the zoom SetZoom set, 1 for none.
func (c *context) Zoom() float32 { return c.rt.zoomFactor() }

// zoomFactor is the content's zoom, 1 for none.
func (rt *engine) zoomFactor() float32 {
	if rt.contentZoom == 0 {
		return 1
	}
	return rt.contentZoom
}

// zoomed reports whether the content is zoomed.
func (rt *engine) zoomed() bool { return rt.contentZoom != 0 && rt.contentZoom != 1 }

func (rt *engine) setZoom(z float32) {
	if math.IsNaN(float64(z)) || z <= 0 {
		z = 1
	}
	z = min(max(z, minZoom), maxZoom)
	if z == rt.zoomFactor() {
		return
	}
	rt.contentZoom = z
	// The frame being built is of the old zoom: it builds again.
	rt.zoomChanged = true
	rt.changed()
}

// size returns the size of the content, in its DIPs, and how many device
// pixels one is: the window's, zoomed.
func (rt *engine) size() (w, h, scale float32) {
	w, h, scale = rt.host.size()
	if rt.zoomed() {
		z := rt.contentZoom
		w, h, scale = w/z, h/z, scale*z
	}
	return w, h, scale
}

// pixels returns the size of the frame in device pixels, the window's,
// for content w×h DIPs at scale: zoomed, its rounding is the window's.
func (rt *engine) pixels(w, h, scale float32) (int, int) {
	if rt.zoomed() {
		w, h, scale = rt.host.size()
	}
	return int(math.Ceil(float64(w * scale))), int(math.Ceil(float64(h * scale)))
}

// fromWindow maps a surface event from the window's DIPs to the
// content's: its position, and the distance a touchpad scrolls, which the
// content follows; a wheel's lines scroll as far as unzoomed.
func (rt *engine) fromWindow(ev *platform.SurfaceEvent) {
	if !rt.zoomed() {
		return
	}
	z := float64(rt.contentZoom)
	ev.X, ev.Y = ev.X/z, ev.Y/z
	if ev.Precise {
		ev.DX, ev.DY = ev.DX/z, ev.DY/z
	}
}

// toWindow maps a rectangle of the content to the window's DIPs.
func (rt *engine) toWindow(r platform.RectF) platform.RectF {
	if !rt.zoomed() {
		return r
	}
	z := float64(rt.contentZoom)
	return platform.RectF{X: r.X * z, Y: r.Y * z, W: r.W * z, H: r.H * z}
}

// titleBar returns the room the window controls take, in the content's
// DIPs.
func (rt *engine) titleBar() TitleBar {
	b := rt.host.titleBar()
	if rt.zoomed() {
		z := rt.contentZoom
		b = TitleBar{Height: b.Height / z, Left: b.Left / z, Right: b.Right / z}
	}
	return b
}

// windowAccess maps the boxes of an accessibility tree to the window's
// DIPs.
func (rt *engine) windowAccess(t *platform.AccessTree) *platform.AccessTree {
	if rt.zoomed() {
		for i := range t.Nodes {
			t.Nodes[i].Bounds = rt.toWindow(t.Nodes[i].Bounds)
		}
	}
	return t
}

// windowWebViews maps where the frame shows its web views to the window's
// DIPs, in place: views is the frame's own (webViewPlacements).
func (rt *engine) windowWebViews(views []placedWebView) []placedWebView {
	if !rt.zoomed() {
		return views
	}
	for i := range views {
		v := &views[i]
		v.frame, v.clip = rt.toWindow(v.frame), rt.toWindow(v.clip)
		// The page lays out at the window's DIPs, unscaled by the zoom.
		z := float64(rt.contentZoom)
		v.size = [2]float64{v.size[0] * z, v.size[1] * z}
		for j := range v.radii {
			v.radii[j] *= z
		}
		for j := range v.covers {
			v.covers[j] = rt.toWindow(v.covers[j])
		}
	}
	return views
}

// windowRect maps a rectangle of the content to the window's DIPs.
func (rt *engine) windowRect(r Rect) Rect {
	if !rt.zoomed() {
		return r
	}
	z := rt.contentZoom
	return Rect{r.X * z, r.Y * z, r.W * z, r.H * z}
}
