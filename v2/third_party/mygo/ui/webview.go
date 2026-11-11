package ui

import (
	"slices"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/scene"
	"github.com/egoist/mygo/internal/surface"
)

// NativeWebView is a web view a window made for its content to show,
// as *mygo.WebView, of mygo.Window.NewWebView.
type NativeWebView = surface.WebView

// WebView shows a web view, made with mygo.Window.NewWebView, in the
// element's content box. A web view has no size of its own: the element
// stretches across its container (AlignSelf(Stretch)), and takes the room
// along it with Grow or a size:
//
//	ui.Row(c).Fill().Children(func() {
//		sidebar(c)
//		ui.WebView(c, app.docs).Grow(1)
//	})
//
// The web view shows while a frame builds its element and hides when one
// does not, keeping its page: a view switching tabs builds the web view
// of the tab shown. The element paints a hole through the window's
// content (the web view is under it, the system's own view), as rounded
// as its Radius and cut by the scroll containers and clips around it, so
// that what paints after it shows over the page: its children, popovers,
// menus, dialogs, tooltips and toasts. The page takes the pointer where
// it shows and nothing painted over it takes it, and the keyboard once
// clicked, focused (mygo.WebView.Focus) or tabbed into: Tab stops at the
// element. The keys the page leaves unhandled come to the native UI as if
// the element had the focus, which the shortcuts around it and the
// window's see. Its background and border paint around the page, which
// its opacity fades, and assistive technology finds the page inside it.
func coreWebView(c *context, v NativeWebView) *node {
	e := c.newElement(kindBox)
	e.webView = v
	e.self = Stretch
	// Tab stops at it to give its page the keyboard, which shows its
	// own focus.
	e.flags |= flagFocusable | flagOwnRing
	return e
}

// webPlace is where a frame shows a web view: size is the size its page
// lays out at, which frame shows scaled, and radii its corners', in its
// DIPs; hit is the index of its element's own hit, inert that it shows
// but takes no pointer.
type webPlace struct {
	v     NativeWebView
	frame Rect
	clip  Rect
	size  [2]float32
	radii [4]float32
	hit   int
	inert bool
}

// noteWebView notes where the frame shows the web view of e, laid out
// within clip, unless none of it shows. Called while committing, before
// the element's own hit.
func (rt *engine) noteWebView(e *node, clip Rect, invisible bool) {
	if e.flags&flagInvisible != 0 || rt.commitHidden {
		return
	}
	// The hole the painter makes: the content box on whole pixels,
	// rounded as its border's inner edge.
	s := rt.painted[2]
	if s <= 0 {
		s = 1
	}
	box := e.contentBox()
	sx, sy, upright := scaling(e.world)
	var b, page Rect
	if upright && (sx != 1 || sy != 1) {
		// Scaled, as a panel zooming in: the page keeps its layout and
		// shows scaled, as the painter scales the hole, on whole pixels
		// before the transform.
		page = snapPixels(box, s)
		page = Rect{page.X / s, page.Y / s, page.W / s, page.H / s}
		b = Rect{sx*page.X + e.world.X, sy*page.Y + e.world.Y, sx * page.W, sy * page.H}
	} else {
		// Turned, skewed or in perspective, a native web view stays
		// upright, covering the transformed box's bounds, at their size.
		b = snapPixels(e.visualRect(box), s)
		b = Rect{b.X / s, b.Y / s, b.W / s, b.H / s}
		page = b
	}
	v := intersect(b, clip)
	if b.W <= 0 || b.H <= 0 || v.W <= 0 || v.H <= 0 {
		return
	}
	wp := webPlace{v: e.webView, frame: b, clip: v, size: [2]float32{page.W, page.H}, hit: len(rt.hits), inert: invisible}
	if upright {
		wp.radii = holeRadii(e, s)
		for i := range wp.radii {
			wp.radii[i] /= s
		}
	}
	rt.webPlaces = append(rt.webPlaces, wp)
}

// placedWebView is where a frame shows a web view, for the host.
type placedWebView struct {
	v           NativeWebView
	frame, clip platform.RectF
	// size is the size the page lays out at, and radii its corners', in
	// its DIPs (platform.WebViewPlacement).
	size   [2]float64
	radii  [4]float64
	covers []platform.RectF
}

// placement returns v as the platform takes it.
func (v *placedWebView) placement(n platform.WebView) platform.WebViewPlacement {
	return platform.WebViewPlacement{WebView: n, Frame: v.frame, Clip: v.clip, Width: v.size[0], Height: v.size[1], Radii: v.radii, Covers: v.covers}
}

// scaling returns the scale of m, which only moves and scales, by
// positive factors; ok is false for any other transform.
func scaling(m scene.Affine) (x, y float32, ok bool) {
	if !m.Set {
		return 1, 1, true
	}
	if m.B != 0 || m.C != 0 || m.G != 0 || m.H != 0 || m.A <= 0 || m.D <= 0 {
		return 0, 0, false
	}
	return m.A, m.D, true
}

// snapPixels returns r at scale pixels per DIP, on whole pixels.
func snapPixels(r Rect, scale float32) Rect {
	x0, y0 := round(r.X*scale), round(r.Y*scale)
	x1, y1 := round((r.X+r.W)*scale), round((r.Y+r.H)*scale)
	return Rect{x0, y0, x1 - x0, y1 - y0}
}

// holeRadii returns the radii of the corners of the hole showing the web
// view of e, in pixels at scale pixels per DIP: its border's inner edge
// less its padding.
func holeRadii(e *node, scale float32) [4]float32 {
	in := [4]float32{e.border[0] + e.pad[0], e.border[1] + e.pad[1], e.border[2] + e.pad[2], e.border[3] + e.pad[3]}
	box := snapPixels(Rect{e.x, e.y, e.w, e.h}, scale)
	px := func(r [4]float32) [4]float32 {
		return [4]float32{r[0] * scale, r[1] * scale, r[2] * scale, r[3] * scale}
	}
	sb := scene.Rect{X: box.X, Y: box.Y, W: box.W, H: box.H}
	_, radii := scene.InnerRadii(sb, scene.Corners(sb, px(e.radius), continuousCorners), px(in))
	for i := range radii {
		radii[i] = abs32(radii[i])
	}
	return radii
}

// webViewPlacements returns where the frame shows its web views, in the
// order it paints them, each with the boxes of what is painted over it
// and takes the pointer: the hits after its own. A web view built twice,
// as by an exit transition, shows where it takes the pointer.
func (rt *engine) webViewPlacements() []placedWebView {
	out := rt.webOut[:0]
	for i := range rt.webPlaces {
		wp := &rt.webPlaces[i]
		dup := slices.IndexFunc(rt.webPlaces[:i], func(o webPlace) bool { return o.v == wp.v })
		if dup >= 0 && (wp.inert || !rt.webPlaces[dup].inert) {
			continue
		}
		if dup >= 0 {
			// The inert one went first: this one takes its place.
			out = slices.DeleteFunc(out, func(o placedWebView) bool { return o.v == wp.v })
		}
		pv := placedWebView{v: wp.v, frame: rectF(wp.frame), clip: rectF(wp.clip), covers: wp.covers(rt.hits)}
		pv.size = [2]float64{float64(wp.size[0]), float64(wp.size[1])}
		for i, r := range wp.radii {
			pv.radii[i] = float64(r)
		}
		out = append(out, pv)
	}
	rt.webOut = out
	return out
}

// covers returns the boxes, within the web view's clip, of the hits after
// its own, as they show: those inside a box already found add nothing.
func (wp *webPlace) covers(hits []hit) []platform.RectF {
	if wp.inert {
		return []platform.RectF{rectF(wp.clip)}
	}
	var out []platform.RectF
	start := wp.hit
	if start < len(hits) && hits[start].st != nil && hits[start].st.webView == wp.v {
		start++ // its own
	}
next:
	for _, h := range hits[start:] {
		r := h.r
		if h.st != nil {
			r = transformRect(h.st.world, r)
		}
		r = intersect(r, wp.clip)
		if r.W <= 0 || r.H <= 0 {
			continue
		}
		f := rectF(r)
		for _, c := range out {
			if f.X >= c.X && f.Y >= c.Y && f.X+f.W <= c.X+c.W && f.Y+f.H <= c.Y+c.H {
				continue next
			}
		}
		out = append(out, f)
	}
	return out
}

func rectF(r Rect) platform.RectF {
	return platform.RectF{X: float64(r.X), Y: float64(r.Y), W: float64(r.W), H: float64(r.H)}
}

// placeWebViews tells the host where the frame shows its web views.
func (rt *engine) placeWebViews() {
	if len(rt.webPlaces) == 0 && !rt.webShown {
		return
	}
	rt.webShown = len(rt.webPlaces) > 0
	rt.host.placeWebViews(rt.windowWebViews(rt.webViewPlacements()))
}

// webViewPress handles a press that went to the web view at x, y: the
// elements under it hover, and the focus goes to the web view's element,
// as its page has the keyboard, but nothing is pressed; popovers see a
// press outside them (PressedOutside).
func (rt *engine) webViewPress(x, y float32, button int) {
	chain := rt.hitChain(x, y)
	rt.pointerX, rt.pointerY, rt.pointerIn = x, y, true
	rt.setHover(chain)
	if len(chain) > 0 {
		rt.downs = append(rt.downs, chain[0])
	} else {
		rt.downs = append(rt.downs, 0)
	}
	if button == 0 {
		var id uint64
		for _, i := range chain {
			if s := rt.states[i]; s != nil && s.webView != nil {
				id = i
				break
			}
		}
		if rt.focused != id {
			rt.focused = id
			rt.focusVisible = false
		}
	}
	rt.requestFrame()
}

// webViewKey handles a key pressed in the page of v that the page left
// unhandled, as the platform tells (platform.WebViewKey): the content
// takes it as if the element showing v had the focus, which it moves
// there, without showing it, as the page has the keyboard. The shortcuts
// of the elements around it and the window's see it; the modifiers the
// content knows stay as they were, as it hears none let go of.
func (rt *engine) webViewKey(v NativeWebView, mods Modifiers, key, physical Key, repeat bool) {
	if key == KeyTab || key == KeyUnknown && physical == KeyUnknown {
		return // the page's own, or no key
	}
	var id uint64
	for _, i := range rt.focusOrder {
		if s := rt.states[i]; s != nil && s.webView == v {
			id = i
			break
		}
	}
	if id == 0 {
		return // no element shows it now
	}
	rt.redraw = false
	rt.input = true
	if rt.focused != id {
		rt.focused = id
		rt.focusVisible = false
	}
	rt.keyDownPhysical(mods, key, physical, CodeUnknown, repeat)
	rt.updateTextInput()
}

// webViewTabOut moves the focus on from the element showing v, whose page
// Tab left past its last element, or Shift+Tab past its first when back.
func (rt *engine) webViewTabOut(v NativeWebView, back bool) {
	for _, id := range rt.focusOrder {
		if s := rt.states[id]; s != nil && s.webView == v {
			rt.focused = id
			break
		}
	}
	rt.moveFocus(back)
	rt.requestFrame()
}

// overWebView reports whether the element under the pointer is a web
// view, whose page has the cursor.
func (rt *engine) overWebView() bool {
	if len(rt.hover) == 0 {
		return false
	}
	s := rt.states[rt.hover[0]]
	return s != nil && s.webView != nil
}

// hole paints the hole showing the web view of e: its content box,
// rounded as its border's inner edge less its padding.
func (p *Painter) hole(e *node) {
	b := e.contentBox()
	if b.W <= 0 || b.H <= 0 {
		return
	}
	radii := holeRadii(e, p.scale)
	r := p.snap(b)
	// Translucent, the page blends over what was painted under it.
	p.add(scene.Op{Kind: scene.OpHole, Rect: r, Radii: radii, Continuous: continuousCorners, Opacity: p.opacity})
	if c := intersect(transformRect(p.transform, b), p.clip); c.W > 0 && c.H > 0 {
		p.holes = append(p.holes, c)
	}
}
