//go:build windows && (amd64 || arm64)

package windows

import (
	"cmp"
	"math"
	"slices"
	"unsafe"

	"github.com/egoist/mygo/internal/platform"
)

// A web view in a window of native UI (platform.Surface.NewWebView) is a
// window of this package without a window of its own (hwnd is 0): its
// host's surface hosts a WebView2 composition controller, whose visuals
// are in a DirectComposition target of the surface's window that is not
// topmost, under the target of its frames (the renderer's swap chain, or
// the frames drawn in memory, which the surface then composes as a window
// without a redirection bitmap does). Its frames are transparent where the
// content shows a web view (holes), so that what the content paints after
// it shows over the page. The surface gets the mouse everywhere and sends
// it on to the web view where it shows and nothing painted over it takes
// the pointer, as visual hosting has the host do; the web view takes the
// keyboard as it gets the focus, and tells the cursor its page wants.

var (
	procDCompositionCreateDevice2 = systemDLL("dcomp.dll").NewProc("DCompositionCreateDevice2")

	iidICoreWebView2Environment3           = guid("80a22ae3-be7c-4ce2-afe1-5a50056cdeeb")
	iidICoreWebView2Controller             = guid("4d00c0d1-9434-4eb6-8078-8697a560334f")
	iidICoreWebView2CompositionController2 = guid("0b6a3d24-49cb-4806-ba20-b5e0734a7b26")
	iidICoreWebView2CompositionController3 = guid("9570570e-4d76-4361-9ee1-f04d0dbdfb1e")
	iidICoreWebView2Environment4           = guid("20944379-6dcf-41d6-a0a0-abc0fc50de0d")
)

// Vtable indices, from WebView2.h and dcomp.h.
const (
	env3CreateCompositionController = 9 // ICoreWebView2Environment3

	// ICoreWebView2CompositionController
	compPutRootVisualTarget = 4
	compSendMouseInput      = 5
	compGetCursor           = 7
	compAddCursorChanged    = 9

	comp2GetAutomationProvider = 11 // ICoreWebView2CompositionController2

	env4GetAutomationProviderForWindow = 11 // ICoreWebView2Environment4

	// ICoreWebView2CompositionController3
	comp3DragEnter = 12
	comp3DragLeave = 13
	comp3DragOver  = 14
	comp3Drop      = 15

	// ICoreWebView2Controller, and the arguments of MoveFocusRequested
	ctlAddMoveFocusRequested = 13
	ctlAddGotFocus           = 15
	ctlAddLostFocus          = 17
	moveFocusGetReason       = 3
	moveFocusPutHandled      = 5

	// COREWEBVIEW2_MOVE_FOCUS_REASON
	moveFocusNext     = 1
	moveFocusPrevious = 2

	dcompVisualSetTransform     = 8  // IDCompositionVisual: the overload taking a D2D_MATRIX_3X2_F
	dcompVisualSetClipObject    = 13 // the overload taking an IDCompositionClip
	dcompVisualSetClip          = 14 // the overload taking a D2D_RECT_F
	dcompVisualAddVisual        = 16
	dcompVisualRemoveVisual     = 17
	dcompDevCreateRectangleClip = 24 // IDCompositionDevice

	// IDCompositionRectangleClip: the overloads taking a float, each after
	// the one taking an animation; the corners' radii, x then y, from the
	// top-left, then the top-right, the bottom-left and the bottom-right.
	rectClipSetLeft          = 4
	rectClipSetTop           = 6
	rectClipSetRight         = 8
	rectClipSetBottom        = 10
	rectClipSetTopLeftRadius = 12

	mouseLeave = 0x02A3 // COREWEBVIEW2_MOUSE_EVENT_KIND_LEAVE, WM_MOUSELEAVE
)

// webLayer is the tree of a surface's web views: a DirectComposition
// target on the surface's window that is not topmost, on a device of its
// own without a rendering device, which no GPU reset takes away.
type webLayer struct {
	dcomp, target, root uintptr
}

func newWebLayer(hwnd uintptr) (*webLayer, error) {
	if err := procDCompositionCreateDevice2.Find(); err != nil {
		return nil, err
	}
	l := &webLayer{}
	hr, _, _ := procDCompositionCreateDevice2.Call(0, uintptr(unsafe.Pointer(&iidIDCompositionDevice)), uintptr(unsafe.Pointer(&l.dcomp)))
	if failed(hr) {
		return nil, hresultError("DCompositionCreateDevice2", hr)
	}
	if hr := comCall(l.dcomp, dcompDevCreateTargetForHwnd, hwnd, 0, uintptr(unsafe.Pointer(&l.target))); failed(hr) {
		l.free()
		return nil, hresultError("IDCompositionDevice::CreateTargetForHwnd", hr)
	}
	if hr := comCall(l.dcomp, dcompDevCreateVisual, uintptr(unsafe.Pointer(&l.root))); failed(hr) {
		l.free()
		return nil, hresultError("IDCompositionDevice::CreateVisual", hr)
	}
	if hr := comCall(l.target, dcompTargetSetRoot, l.root); failed(hr) {
		l.free()
		return nil, hresultError("IDCompositionTarget::SetRoot", hr)
	}
	return l, nil
}

func (l *webLayer) visual() uintptr {
	var v uintptr
	comCall(l.dcomp, dcompDevCreateVisual, uintptr(unsafe.Pointer(&v)))
	return v
}

func (l *webLayer) commit() { comCall(l.dcomp, dcompDevCommit) }

func (l *webLayer) free() {
	for _, p := range []*uintptr{&l.root, &l.target, &l.dcomp} {
		release(*p)
		*p = 0
	}
}

// translate moves a visual by x, y pixels.
func translate(visual uintptr, x, y float32) { place(visual, x, y, 1, 1) }

// place scales a visual by sx, sy, then moves it by x, y pixels.
func place(visual uintptr, x, y, sx, sy float32) {
	m := [6]float32{sx, 0, 0, sy, x, y} // D2D_MATRIX_3X2_F
	comCall(visual, dcompVisualSetTransform, uintptr(unsafe.Pointer(&m)))
}

// roundCorners cuts the web view w's visual to the rectangle from 0, 0 to
// width, height pixels, its corners of radii from the top-left clockwise,
// or takes the cut away without radii.
func (w *window) roundCorners(l *webLayer, width, height float32, radii [4]float32) {
	if radii == [4]float32{} {
		if w.corners != 0 {
			comCall(w.webVis, dcompVisualSetClipObject, 0)
			release(w.corners)
			w.corners = 0
		}
		return
	}
	if w.corners == 0 {
		if failed(comCall(l.dcomp, dcompDevCreateRectangleClip, uintptr(unsafe.Pointer(&w.corners)))) {
			w.corners = 0
			return
		}
		comCall(w.webVis, dcompVisualSetClipObject, w.corners)
	}
	c := w.corners
	comCallFloat32(c, rectClipSetLeft, 0)
	comCallFloat32(c, rectClipSetTop, 0)
	comCallFloat32(c, rectClipSetRight, width)
	comCallFloat32(c, rectClipSetBottom, height)
	// The setters' order goes around by the bottom-left first.
	for i, r := range [4]float32{radii[0], radii[1], radii[3], radii[2]} {
		comCallFloat32(c, rectClipSetTopLeftRadius+4*i, r)   // x
		comCallFloat32(c, rectClipSetTopLeftRadius+4*i+2, r) // y
	}
}

// toPage converts a point of the surface, in pixels, to the web view's,
// as its composition controller takes them.
func (w *window) toPage(x, y int32) (int32, int32) {
	sx, sy := cmp.Or(w.pageScale[0], 1), cmp.Or(w.pageScale[1], 1)
	return int32(math.Round(float64((float32(x) - w.pageAt[0]) / sx))), int32(math.Round(float64((float32(y) - w.pageAt[1]) / sy)))
}

// clipVisual clips a visual to its rectangle from 0, 0 to w, h pixels.
func clipVisual(visual uintptr, w, h float32) {
	r := [4]float32{0, 0, w, h} // D2D_RECT_F: left, top, right, bottom
	comCall(visual, dcompVisualSetClip, uintptr(unsafe.Pointer(&r)))
}

func (s *surface) NewWebView(o *platform.WindowOptions, h platform.WindowHandler) (platform.WebView, error) {
	b := s.w.b
	if err := b.startEnvironment(o); err != nil {
		return nil, err
	}
	if s.layer == nil {
		l, err := newWebLayer(s.hwnd)
		if err != nil {
			return nil, err
		}
		s.layer = l
		if !s.w.noRedirect {
			// The frames show through DirectComposition from now on, over
			// the web views, with their alpha: the content makes its
			// renderer again (Native), and frames drawn in memory go
			// through the compositor.
			s.send(platform.SurfaceEvent{Kind: platform.SurfaceRenew})
			procInvalidateRectW.Call(s.hwnd, 0, 0)
		}
	}
	v := &window{
		b: b, h: h, opts: o, host: s.w, zoom: 1,
		htmlFor: map[string]string{}, calls: map[int]func(string, error){},
	}
	if o.BackgroundColor != nil {
		c := *o.BackgroundColor
		v.bg = &c
	}
	// Only apps with web views in native UI link the script of their keys.
	forwardKeys = (*window).forwardUnhandledKeys
	v.clipVis, v.webVis = s.layer.visual(), s.layer.visual()
	comCall(v.clipVis, dcompVisualAddVisual, v.webVis, 1, 0)
	s.w.webViews = append(s.w.webViews, v)
	b.whenEnvironment(v.createWebView)
	return v, nil
}

// createEmbedded asks WebView2 for the composition controller of a web
// view in a window of native UI, through handler.
func (w *window) createEmbedded(handler uintptr) uintptr {
	return w.b.createController(storeOf(w.opts.DataStore), w.host.surface.hwnd, true, handler)
}

// setUpEmbedded connects a web view's composition controller to its
// visual and to its host's surface, before setUp.
func (w *window) setUpEmbedded(comp uintptr) uintptr {
	w.comp = comp
	controller := queryInterface(comp, &iidICoreWebView2Controller)
	comCall(comp, compPutRootVisualTarget, w.webVis)
	// The add methods write a registration token. Its address is taken in
	// each call (go:uintptrescapes), as a uintptr saved earlier goes stale
	// when the goroutine stack moves.
	var tok int64
	withHandler(func(_, _ uintptr) {
		var c uintptr
		comCall(w.comp, compGetCursor, uintptr(unsafe.Pointer(&c)))
		w.cursor = c
		if s := w.host.surface; s != nil && s.webHover == w {
			procSetCursor.Call(c)
		}
	}, func(h uintptr) uintptr { return comCall(comp, compAddCursorChanged, h, uintptr(unsafe.Pointer(&tok))) })
	withHandler(func(_, _ uintptr) { w.focused = true }, func(h uintptr) uintptr { return comCall(controller, ctlAddGotFocus, h, uintptr(unsafe.Pointer(&tok))) })
	withHandler(func(_, _ uintptr) { w.focused = false }, func(h uintptr) uintptr { return comCall(controller, ctlAddLostFocus, h, uintptr(unsafe.Pointer(&tok))) })
	// Tab past the page's ends gives the content the keyboard, not the
	// next window WebView2 would find.
	withHandler(func(_, args uintptr) {
		var reason int32
		comCall(args, moveFocusGetReason, uintptr(unsafe.Pointer(&reason)))
		comCall(args, moveFocusPutHandled, 1)
		w.tabOut(reason == moveFocusPrevious)
	}, func(h uintptr) uintptr {
		return comCall(controller, ctlAddMoveFocusRequested, h, uintptr(unsafe.Pointer(&tok)))
	})
	w.comp3 = queryInterface(comp, &iidICoreWebView2CompositionController3)
	return controller
}

// TabInto gives the page the keyboard at its first element, or its last
// going back.
func (w *window) TabInto(back bool) {
	if w.closed || w.controller == 0 || !w.shown {
		return
	}
	reason := uintptr(moveFocusNext)
	if back {
		reason = moveFocusPrevious
	}
	comCall(w.controller, ctlMoveFocus, reason)
}

// tabOut gives the content the keyboard back as Tab leaves the page of the
// web view w past its last element, or Shift+Tab past its first.
func (w *window) tabOut(back bool) {
	s := w.host.surface
	if s == nil || w.host.closed {
		return
	}
	procSetFocus.Call(s.hwnd)
	ev := platform.SurfaceEvent{Kind: platform.WebViewTabOut, Key: platform.KeyTab, WebView: w}
	if back {
		ev.Mods = platform.ModShift
	}
	s.send(ev)
}

// automationProvider returns WebView2's UI Automation provider of the
// page, which the window keeps a reference to, or 0.
func (w *window) automationProvider() uintptr {
	if w.uia == 0 && w.comp != 0 {
		if comp2 := queryInterface(w.comp, &iidICoreWebView2CompositionController2); comp2 != 0 {
			comCall(comp2, comp2GetAutomationProvider, uintptr(unsafe.Pointer(&w.uia)))
			release(comp2)
		}
	}
	return w.uia
}

// pagePoint converts a point of the screen, a POINTL as OLE passes it, to
// the web view's pixels, as its composition controller takes them.
func (w *window) pagePoint(pt uintptr) uintptr {
	s := w.host.surface
	p := point{int32(uint32(pt)), int32(uint32(pt >> 32))}
	procScreenToClient.Call(s.hwnd, uintptr(unsafe.Pointer(&p)))
	p.X, p.Y = w.toPage(p.X, p.Y)
	return uintptr(uint32(p.X)) | uintptr(uint32(p.Y))<<32
}

// PlaceWebViews shows the window's web views where the content shows
// them, in its paint order under the surface's frames, and hides the
// others.
func (s *surface) PlaceWebViews(views []platform.WebViewPlacement) {
	if s.w.closed || s.layer == nil {
		return
	}
	s.placed = append(s.placed[:0], views...)
	var shown []*window
	for _, p := range views {
		if v, ok := p.WebView.(*window); ok && v.host == s.w && !v.closed {
			v.placement, v.shown = p, true
			v.placement.Covers = nil
			shown = append(shown, v)
		}
	}
	for _, v := range s.w.webViews {
		if !slices.Contains(shown, v) && v.shown {
			v.shown = false
			if v.focused {
				// A hidden view keeps no keyboard: it goes back to the
				// content.
				procSetFocus.Call(s.hwnd)
			}
		}
	}
	if !slices.Equal(shown, s.stacked) {
		// The visuals of those shown, in the order painted.
		comCall(s.layer.root, dcompVisualRemoveAllVisuals)
		prev := uintptr(0)
		for _, v := range shown {
			comCall(s.layer.root, dcompVisualAddVisual, v.clipVis, 1, prev)
			prev = v.clipVis
		}
		s.stacked = shown
	}
	for _, v := range s.w.webViews {
		v.placeEmbedded()
	}
	s.layer.commit()
}

const dcompVisualRemoveAllVisuals = 18

// placeEmbedded shows a web view where the content last showed it, or
// hides it, once it has its controller. It stays hidden while its host
// is.
func (w *window) placeEmbedded() {
	s := w.host.surface
	if s == nil {
		return
	}
	k := float64(s.dpi()) / 96
	px := func(v float64) float32 { return float32(math.Round(v * k)) }
	f, c := w.placement.Frame, w.placement.Clip
	cx, cy := px(c.X), px(c.Y)
	fx, fy := px(f.X), px(f.Y)
	clipVisual(w.clipVis, px(c.X+c.W)-cx, px(c.Y+c.H)-cy)
	translate(w.clipVis, cx, cy)
	// The page lays out at its size, which its visual shows scaled to its
	// frame, if a transform of the content scales it.
	width, height := px(f.X+f.W)-fx, px(f.Y+f.H)-fy
	at, scale := [2]float32{fx, fy}, [2]float32{1, 1}
	if sx, sy := w.placement.Scale(); sx != 1 || sy != 1 {
		pw, ph := w.placement.Size()
		width, height = max(px(pw), 1), max(px(ph), 1)
		at = [2]float32{float32(f.X * k), float32(f.Y * k)}
		scale = [2]float32{float32(f.W*k) / width, float32(f.H*k) / height}
	}
	w.pageAt, w.pageScale = at, scale
	// The page draws from its visual's origin.
	place(w.webVis, at[0]-cx, at[1]-cy, scale[0], scale[1])
	var radii [4]float32
	for i, r := range w.placement.Radii {
		radii[i] = float32(r * k)
	}
	w.roundCorners(s.layer, width, height, radii)
	if w.controller == 0 {
		return
	}
	if w.shown {
		// Its bounds, in the surface's pixels, place what WebView2 shows
		// in windows of its own, as a select's options.
		putBounds(w.controller, rect{int32(fx), int32(fy), int32(fx + width), int32(fy + height)})
	}
	comCall(w.controller, ctlPutIsVisible, boolArg(w.shown && !w.host.hidden))
}

// webViewAt returns the web view that takes the pointer at x, y in the
// surface, in DIPs.
func (s *surface) webViewAt(x, y float64) *window {
	for i := len(s.placed) - 1; i >= 0; i-- {
		if p := &s.placed[i]; p.At(x, y) {
			if v, ok := p.WebView.(*window); ok && !v.closed && v.comp != 0 {
				return v
			}
		}
	}
	return nil
}

// forwardMouse sends a mouse message of the surface on to the web view
// under the pointer, or that took the press, and reports whether it did:
// the content then hears only that the pointer moved over it, or that a
// button went down on it. The wheel over a web view with
// ScrollToContainer stays the content's, which scrolls as it does over
// its own elements; a pinch still goes to the page, as on the other
// platforms.
func (s *surface) forwardMouse(m uint32, wp, lp uintptr) bool {
	if s.layer == nil {
		return false
	}
	pt := point{int32(int16(loword(lp))), int32(int16(hiword(lp)))}
	if m == wmMouseWheel || m == wmMouseHWheel {
		procScreenToClient.Call(s.hwnd, uintptr(unsafe.Pointer(&pt)))
	}
	x, y := s.toDIP(pt.X), s.toDIP(pt.Y)
	v := s.webCapture
	if v == nil && s.buttons == 0 {
		v = s.webViewAt(x, y)
	}
	if v != s.webHover && m != wmMouseLeave {
		if h := s.webHover; h != nil && h.comp != 0 && !h.closed {
			comCall(h.comp, compSendMouseInput, mouseLeave, 0, 0, 0)
		}
		s.webHover = v
	}
	if v == nil || v.closed {
		return false
	}
	if (m == wmMouseWheel || m == wmMouseHWheel) && v.opts.ScrollToContainer && !pinching(m, wp) {
		return false // the content around the web view scrolls
	}
	var data uint32
	keys := uintptr(loword(wp))
	switch m {
	case wmMouseWheel, wmMouseHWheel:
		data = uint32(int32(int16(hiword(wp))))
	case wmXButtonDown, wmXButtonUp, wmXButtonDblClk:
		data = uint32(hiword(wp))
	}
	switch m {
	case wmMouseMove:
		// The content hears of the pointer over the page: what it
		// hovered goes, and it sets no cursor there.
		if !s.tracking {
			tme := trackMouseEvent{Flags: tmeLeaveFlag, Track: s.hwnd}
			tme.Size = uint32(unsafe.Sizeof(tme))
			procTrackMouseEvent.Call(uintptr(unsafe.Pointer(&tme)))
			s.tracking = true
		}
		s.send(platform.SurfaceEvent{Kind: platform.PointerMove, X: x, Y: y, Mods: mods()})
	case wmLButtonDown, wmLButtonDblClk, wmRButtonDown, wmRButtonDblClk, wmMButtonDown, wmMButtonDblClk, wmXButtonDown, wmXButtonDblClk:
		if s.webCapture == nil {
			s.webCapture = v
			procSetCapture.Call(s.hwnd)
		}
		button := 0
		switch m {
		case wmRButtonDown, wmRButtonDblClk:
			button = 1
		case wmMButtonDown, wmMButtonDblClk:
			button = 2
		}
		if m != wmXButtonDown && m != wmXButtonDblClk {
			s.send(platform.SurfaceEvent{Kind: platform.WebViewPress, X: x, Y: y, Button: button, Mods: mods()})
		}
		if !v.focused {
			v.focusWebView()
		}
	}
	lx, ly := v.toPage(pt.X, pt.Y)
	local := uintptr(uint32(lx)) | uintptr(uint32(ly))<<32
	comCall(v.comp, compSendMouseInput, uintptr(m), keys, uintptr(data), local)
	switch m {
	case wmLButtonUp, wmRButtonUp, wmMButtonUp, wmXButtonUp:
		if keys&(0x01|0x02|0x10|0x20|0x40) == 0 { // MK_LBUTTON, MK_RBUTTON, MK_MBUTTON, MK_XBUTTON1, MK_XBUTTON2
			s.webCapture = nil
			procReleaseCapture.Call()
		}
	}
	return true
}

// pinching reports whether a wheel message is a touchpad's pinch, which
// Windows sends as scrolling by less than a notch with Ctrl held.
func pinching(m uint32, wp uintptr) bool {
	return m == wmMouseWheel && int16(hiword(wp))%120 != 0 && loword(wp)&0x0008 != 0 // MK_CONTROL
}

// mouseLeft tells the web view the pointer was over that it left.
func (s *surface) mouseLeft() {
	if h := s.webHover; h != nil {
		s.webHover = nil
		if h.comp != 0 && !h.closed {
			comCall(h.comp, compSendMouseInput, mouseLeave, 0, 0, 0)
		}
	}
}

// webCursor sets the cursor of the page under the pointer, and reports
// whether there is one: WM_SETCURSOR comes before the move that tells.
func (s *surface) webCursor() bool {
	v := s.webCapture
	if v == nil && s.layer != nil && s.buttons == 0 {
		var pt point
		procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
		procScreenToClient.Call(s.hwnd, uintptr(unsafe.Pointer(&pt)))
		v = s.webViewAt(s.toDIP(pt.X), s.toDIP(pt.Y))
	}
	if v != nil && v.cursor != 0 {
		procSetCursor.Call(v.cursor)
		return true
	}
	return false
}

// focusedWebView returns the web view of the window that has the
// keyboard, if one does.
func (w *window) focusedWebView() *window {
	for _, v := range w.webViews {
		if !v.closed && v.focused {
			return v
		}
	}
	return nil
}

// top returns the window that shows w: its host for a web view.
func (w *window) top() *window {
	if w.host != nil {
		return w.host
	}
	return w
}

// closeWebView closes a web view: by Close, or as its window closes.
func (w *window) closeWebView() {
	if w.closed {
		return
	}
	w.closed = true
	for id, cb := range w.calls {
		delete(w.calls, id)
		cb("", errDestroyed)
	}
	w.failPending(errDestroyed)
	h := w.host
	s := h.surface
	if w.focused && s != nil {
		procSetFocus.Call(s.hwnd)
	}
	if w.controller != 0 {
		comCall(w.controller, ctlClose)
		release(w.settings)
		release(w.webview)
		release(w.controller)
		w.controller, w.webview, w.settings = 0, 0, 0
	}
	release(w.comp)
	release(w.comp3)
	release(w.uia)
	w.comp, w.comp3, w.uia = 0, 0, 0
	if s != nil {
		if s.webHover == w {
			s.webHover = nil
		}
		if s.webCapture == w {
			s.webCapture = nil
			procReleaseCapture.Call()
		}
		s.placed = slices.DeleteFunc(s.placed, func(p platform.WebViewPlacement) bool { return p.WebView == platform.WebView(w) })
		if i := slices.Index(s.stacked, w); i >= 0 && s.layer != nil {
			comCall(s.layer.root, dcompVisualRemoveVisual, w.clipVis)
			s.stacked = slices.Delete(slices.Clone(s.stacked), i, i+1)
			s.layer.commit()
		}
	}
	release(w.corners)
	release(w.webVis)
	release(w.clipVis)
	w.corners, w.webVis, w.clipVis = 0, 0, 0
	h.webViews = slices.DeleteFunc(h.webViews, func(v *window) bool { return v == w })
}

// closeWebViews closes the web views of a window that closes, and frees
// the tree that held them.
func (w *window) closeWebViews() {
	for len(w.webViews) > 0 {
		w.webViews[len(w.webViews)-1].closeWebView()
	}
	if s := w.surface; s != nil && s.layer != nil {
		s.layer.free()
		s.layer = nil
	}
}
