//go:build windows && (amd64 || arm64)

package windows

import (
	"cmp"
	"log"
	"math"
	"strings"
	"unsafe"

	"github.com/egoist/mygo/internal/bridge"
	"github.com/egoist/mygo/internal/platform"
)

// A window with a hidden title bar has no caption: the page fills the
// window, and the backend puts minimize, maximize or restore, and close over
// the page's top-right corner, drawn as Windows 11 draws its own. Two child
// windows sit above the webview:
//
//   - The buttons: a layered window whose pixels carry their alpha
//     (UpdateLayeredWindow), so the page shows around the glyphs; in a
//     window with a material behind its page, which has no redirection
//     bitmap, the pixels show through DirectComposition (compositor.go)
//     instead. It answers WM_NCHITTEST with the buttons' hit-test codes,
//     which brings Windows 11's snap layouts over the maximize button, and
//     runs the buttons from the non-client mouse messages that follow.
//   - The top edge (newTopEdge), which every resizable window without a
//     caption has, frameless ones too, along the rest of the top.
//
// Pages keep clear of the buttons with the --mygo-titlebar-* CSS variables
// (Window.TitleBar).

const (
	captionClass = "MyGoCaption"
	// Windows 11's caption buttons are 46 DIPs wide, as tall as the title
	// bar (32 by default) and have glyphs of 10.
	captionButtonWidth = 46
	captionHeight      = 32
	captionGlyphSize   = 10
)

type captionButton int

const (
	buttonMinimize captionButton = iota
	buttonMaximize
	buttonClose
)

// glyph returns the button's glyph in Segoe Fluent Icons and Segoe MDL2
// Assets, which have them at the same code points.
func (b captionButton) glyph(maximized bool) uint16 {
	switch b {
	case buttonMinimize:
		return 0xE921 // ChromeMinimize
	case buttonMaximize:
		if maximized {
			return 0xE923 // ChromeRestore
		}
		return 0xE922 // ChromeMaximize
	}
	return 0xE8BB // ChromeClose
}

// hitTest is the code WM_NCHITTEST answers over the button.
func (b captionButton) hitTest() uintptr {
	switch b {
	case buttonMinimize:
		return htMinButton
	case buttonMaximize:
		return htMaxButton
	}
	return htClose
}

// captionBar holds the window controls of a window with a hidden title bar.
type captionBar struct {
	w       *window
	buttons uintptr // the buttons' window
	shown   []captionButton
	// hot is the index of the button under the pointer and pressed that of
	// the button pressed, or -1.
	hot, pressed int
	tracking     bool // TrackMouseEvent asked for WM_NCMOUSELEAVE
	active       bool // the window is the active window
	// snap is a window over the maximize button the content draws itself
	// (SetMaximizeButton), 0 until it says where; snapRect is where, in
	// DIPs, and snapPressed tells that the content has a press from it.
	snap        uintptr
	snapRect    platform.RectF
	snapTrack   bool
	snapPressed bool
	// comp shows the buttons in a window without a redirection bitmap,
	// where UpdateLayeredWindow cannot (compositor.go); nil otherwise.
	comp *compositor
}

func newCaptionBar(w *window) *captionBar {
	c := &captionBar{w: w, hot: -1, pressed: -1}
	if w.noRedirect {
		// Layered, so that it blends over the webview instead of cutting
		// its rectangle out of it, and shown through DirectComposition.
		c.buttons = createWindow(wsExLayered|wsExNoRedirect, captionClass, "", wsChild|wsClipSiblings, 0, 0, 0, 0, w.hwnd)
		if c.buttons != 0 {
			procSetLayeredWindowAttributes.Call(c.buttons, 0, 255, lwaAlpha)
			c.comp = newCompositor(w.b, c.buttons)
		}
	} else {
		c.buttons = createWindow(wsExLayered, captionClass, "", wsChild|wsClipSiblings, 0, 0, 0, 0, w.hwnd)
	}
	if c.buttons == 0 {
		log.Print("mygo: cannot create the window controls of a window with a hidden title bar")
		return nil
	}
	w.b.captions[c.buttons] = c
	c.shown = w.shownButtons()
	return c
}

// forget unregisters the bar's windows, which Windows destroys with the
// window.
func (c *captionBar) forget() {
	if c.comp != nil {
		c.comp.free()
	}
	delete(c.w.b.captions, c.buttons)
	if c.snap != 0 {
		delete(c.w.b.captions, c.snap)
	}
}

// shownButtons returns the buttons of the window, left to right: close alone
// when it can be neither minimized nor maximized, as Windows does, and none
// when the page draws its own (HideWindowButtons).
func (w *window) shownButtons() []captionButton {
	if w.opts.HideWindowButtons {
		return nil
	}
	if !w.hasStyle(wsMinimizeBox) && !w.hasStyle(wsMaximizeBox) {
		return []captionButton{buttonClose}
	}
	return []captionButton{buttonMinimize, buttonMaximize, buttonClose}
}

func (w *window) buttonEnabled(b captionButton) bool {
	switch b {
	case buttonMinimize:
		return w.hasStyle(wsMinimizeBox)
	case buttonMaximize:
		return w.hasStyle(wsMaximizeBox)
	}
	return w.closable
}

func (w *window) titleBarHeight() int { return cmp.Or(w.opts.TitleBarHeight, captionHeight) }

func (w *window) TitleBar() platform.TitleBar {
	if w.caption == nil || w.fullScreen || len(w.shownButtons()) == 0 {
		return platform.TitleBar{}
	}
	return platform.TitleBar{Height: w.titleBarHeight(), Right: captionButtonWidth * len(w.shownButtons())}
}

// titleBarScript tells the first page the room of the window controls, at
// document start.
func (w *window) titleBarScript() string { return bridge.TitleBarScript(w.TitleBar(), w.opts.Zoom) }

// captionChanged lays the controls out again after the window gained or lost
// a button, or entered or left full screen, and tells the page.
func (w *window) captionChanged() {
	w.layoutTopEdge()
	if w.caption == nil {
		return
	}
	w.caption.layout()
	w.h.TitleBarChanged()
}

// layout puts the buttons at the top-right corner, above the webview, and
// paints them. It runs when the window changes size, state or DPI, when the
// webview appears, and when the buttons change.
func (c *captionBar) layout() {
	w := c.w
	if w.IsMinimized() {
		return // no room to lay out in
	}
	c.shown = w.shownButtons()
	if c.hot >= len(c.shown) || c.pressed >= len(c.shown) {
		c.hot, c.pressed = -1, -1
	}
	dpi := dpiOf(w.hwnd)
	var r rect
	procGetClientRect.Call(w.hwnd, uintptr(unsafe.Pointer(&r)))
	width := toPx(captionButtonWidth, dpi) * int32(len(c.shown))
	height := toPx(w.titleBarHeight(), dpi)
	x := r.Right - width
	show := uintptr(swpShowWindow)
	if w.fullScreen || width == 0 {
		show = swpHideWindow
	}
	// HWND_TOP (0): above the webview's window, created after them.
	procSetWindowPos.Call(c.buttons, 0, uintptr(x), 0, uintptr(width), uintptr(height), swpNoActivate|show)
	if c.snap != 0 {
		placeSnap(c, dpi)
	}
	c.paint()
}

func (c *captionBar) activate(active bool) {
	if c.active != active {
		c.active = active
		c.paint()
	}
}

func (c *captionBar) setHot(i int) {
	if i >= 0 && !c.w.buttonEnabled(c.shown[i]) {
		i = -1
	}
	if i != c.hot {
		c.hot = i
		c.paint()
	}
}

// indexAt returns the index of the button at the screen position of a
// non-client mouse message, or -1.
func (c *captionBar) indexAt(lp uintptr) int {
	pt := point{getXParam(lp), getYParam(lp)}
	procScreenToClient.Call(c.buttons, uintptr(unsafe.Pointer(&pt)))
	dpi := dpiOf(c.w.hwnd)
	bw := toPx(captionButtonWidth, dpi)
	if pt.X < 0 || pt.Y < 0 || pt.Y >= toPx(c.w.titleBarHeight(), dpi) || bw <= 0 {
		return -1
	}
	if i := int(pt.X / bw); i < len(c.shown) {
		return i
	}
	return -1
}

func (c *captionBar) click(b captionButton) {
	w := c.w
	if !w.buttonEnabled(b) {
		return
	}
	cmd := uintptr(scClose)
	switch b {
	case buttonMinimize:
		cmd = scMinimize
	case buttonMaximize:
		cmd = scMaximize
		if w.IsMaximized() {
			cmd = scRestore
		}
	}
	// Sent, as the system's buttons do: one posted right after a maximize
	// never reached the window. OnClose may keep the window open, or it
	// closes during the call.
	procSendMessageW.Call(w.hwnd, wmSysCommand, cmd, 0)
}

// message handles a message of the buttons' window; ok false lets
// DefWindowProc run.
func (c *captionBar) message(hwnd uintptr, m uint32, wp, lp uintptr) (uintptr, bool) {
	w := c.w
	if hwnd == c.snap {
		return handleSnap(c, m, lp)
	}
	switch m {
	case wmNCHitTest:
		if i := c.indexAt(lp); i >= 0 && w.buttonEnabled(c.shown[i]) {
			return c.shown[i].hitTest(), true
		}
		return htClient, true
	case wmNCMouseMove:
		c.setHot(c.indexAt(lp))
		if !c.tracking {
			tme := trackMouseEvent{Flags: tmeLeave | tmeNonClient, Track: hwnd}
			tme.Size = uint32(unsafe.Sizeof(tme))
			procTrackMouseEvent.Call(uintptr(unsafe.Pointer(&tme)))
			c.tracking = true
		}
		// DefWindowProc brings the snap layouts over HTMAXBUTTON.
		return 0, false
	case wmNCMouseLeave, wmMouseLeave:
		c.tracking = false
		c.pressed = -1
		c.hot = -1
		c.paint()
		return 0, true
	case wmNCLButtonDown, wmNCLButtonDblClk:
		// Not DefWindowProc: it would run the system's buttons on this
		// child window.
		if i := c.indexAt(lp); i >= 0 && w.buttonEnabled(c.shown[i]) {
			c.pressed = i
			c.paint()
		}
		return 0, true
	case wmNCLButtonUp:
		i, pressed := c.indexAt(lp), c.pressed
		c.pressed = -1
		c.paint()
		if i >= 0 && i == pressed {
			c.click(c.shown[i])
		}
		return 0, true
	case wmNCRButtonDown, wmNCRButtonUp, wmNCRButtonDblClk:
		return 0, true
	}
	return 0, false
}

// rgba is a color with straight alpha.
type rgba struct{ r, g, b, a uint8 }

// Windows 11's close button is red on hover, and Windows 10's a brighter red.
var (
	closeRed11 = rgba{0xC4, 0x2B, 0x1C, 0xFF}
	closeRed10 = rgba{0xE8, 0x11, 0x23, 0xFF}
)

// colors returns the backplate and glyph colors of the i-th button, after
// Windows' own: the glyph in the appearance's text color, fainter while the
// window is inactive or the button disabled; on hover and press a backplate
// of the glyph's color at 10% and 20%, or red behind a white glyph for
// close. At rest the backplate is all but transparent, which keeps the
// buttons' window taking the mouse there.
func (c *captionBar) colors(i int, b captionButton, dark, fluent bool) (back, fore rgba) {
	fore = rgba{0, 0, 0, 0xE4}
	if dark {
		fore = rgba{0xFF, 0xFF, 0xFF, 0xFF}
	}
	back = rgba{0, 0, 0, 1}
	red := closeRed10
	if fluent {
		red = closeRed11
	}
	white := rgba{0xFF, 0xFF, 0xFF, 0xFF}
	hot := i == c.hot && (c.pressed < 0 || c.pressed == i)
	switch {
	case !c.w.buttonEnabled(b):
		fore.a = 0x5C
	case hot && c.pressed == i && b == buttonClose:
		back, fore = rgba{red.r, red.g, red.b, 0xCC}, white
	case hot && b == buttonClose:
		back, fore = red, white
	case hot && c.pressed == i:
		back = rgba{fore.r, fore.g, fore.b, 0x33}
	case hot:
		back = rgba{fore.r, fore.g, fore.b, 0x1A}
	case !c.active:
		fore.a = 0x72
	}
	return back, fore
}

// over lays a glyph color of coverage cov (0 to 255) over a backplate, and
// returns the premultiplied BGRA pixel that UpdateLayeredWindow takes.
func over(fore rgba, cov uint32, back rgba) uint32 {
	fa := uint32(fore.a) * cov / 255
	ba := uint32(back.a) * (255 - fa) / 255
	channel := func(f, b uint8) uint32 { return (uint32(f)*fa + uint32(b)*ba) / 255 }
	return (fa+ba)<<24 | channel(fore.r, back.r)<<16 | channel(fore.g, back.g)<<8 | channel(fore.b, back.b)
}

// paint draws the buttons for the window's state and appearance into their
// layered window.
func (c *captionBar) paint() {
	w := c.w
	dpi := dpiOf(w.hwnd)
	bw, height := toPx(captionButtonWidth, dpi), toPx(w.titleBarHeight(), dpi)
	width := bw * int32(len(c.shown))
	if width <= 0 || height <= 0 {
		return
	}
	screen, _, _ := procGetDC.Call(0)
	defer procReleaseDC.Call(0, screen)
	// UpdateLayeredWindow takes the pixels in a bitmap, DirectComposition
	// alone.
	var img *bitmap
	var px []uint32
	if c.comp != nil {
		px = c.comp.pixels(int(width) * int(height))
	} else {
		if img = newBitmap(screen, width, height); img == nil {
			return
		}
		defer img.free()
		px = img.px
	}
	mask := newBitmap(screen, width, height)
	if mask == nil {
		return
	}
	defer mask.free()

	// The glyphs white on black: their coverage.
	face := w.b.captionFace(dpi)
	old, _, _ := procSelectObject.Call(mask.dc, face.font)
	procSetBkMode.Call(mask.dc, 1) // TRANSPARENT
	procSetTextColor.Call(mask.dc, 0xFFFFFF)
	maximized := w.IsMaximized()
	for i, b := range c.shown {
		g := b.glyph(maximized)
		r := rect{int32(i) * bw, 0, int32(i+1) * bw, height}
		procDrawTextW.Call(mask.dc, uintptr(unsafe.Pointer(&g)), 1, uintptr(unsafe.Pointer(&r)), dtCenter|dtVCenter|dtSingleLine|dtNoPrefix)
	}
	procSelectObject.Call(mask.dc, old)
	procGdiFlush.Call()

	dark := w.buttonsDark()
	for i, b := range c.shown {
		back, fore := c.colors(i, b, dark, face.fluent)
		for y := int32(0); y < height; y++ {
			row := y * width
			for x := int32(i) * bw; x < int32(i+1)*bw; x++ {
				px[row+x] = over(fore, mask.px[row+x]&0xFF, back)
			}
		}
	}
	if c.comp != nil {
		c.comp.show(px, width, height)
		return
	}
	size := [2]int32{width, height}
	var origin point
	blend := uint32(255<<16 | 1<<24) // AC_SRC_OVER, SourceConstantAlpha 255, AC_SRC_ALPHA
	procUpdateLayeredWindow.Call(c.buttons, screen, 0, uintptr(unsafe.Pointer(&size)), img.dc,
		uintptr(unsafe.Pointer(&origin)), 0, uintptr(unsafe.Pointer(&blend)), ulwAlpha)
}

// captionFont is the font of the caption glyphs at one DPI.
type captionFont struct {
	font uintptr
	// fluent is Segoe Fluent Icons, Windows 11's; Windows 10 has Segoe
	// MDL2 Assets.
	fluent bool
}

func (b *Backend) captionFace(dpi int) captionFont {
	if f, ok := b.captionFonts[dpi]; ok {
		return f
	}
	size := -toPx(captionGlyphSize, dpi) // the em height
	f := captionFont{font: createFont(size, "Segoe Fluent Icons"), fluent: true}
	if !fontFace(f.font, "Segoe Fluent Icons") {
		procDeleteObject.Call(f.font)
		f = captionFont{font: createFont(size, "Segoe MDL2 Assets")}
	}
	b.captionFonts[dpi] = f
	return f
}

func createFont(height int32, face string) uintptr {
	const (
		fwNormal           = 400
		defaultCharset     = 1
		antialiasedQuality = 4
	)
	f, _, _ := procCreateFontW.Call(uintptr(height), 0, 0, 0, fwNormal, 0, 0, 0, defaultCharset, 0, 0,
		antialiasedQuality, 0, uintptr(unsafe.Pointer(u16(face))))
	return f
}

// fontFace reports whether font is the named face, not one Windows
// substituted for a missing one.
func fontFace(font uintptr, name string) bool {
	dc, _, _ := procCreateCompatibleDC.Call(0)
	defer procDeleteDC.Call(dc)
	old, _, _ := procSelectObject.Call(dc, font)
	defer procSelectObject.Call(dc, old)
	buf := make([]uint16, 64)
	procGetTextFaceW.Call(dc, uintptr(len(buf)), uintptr(unsafe.Pointer(&buf[0])))
	return strings.EqualFold(string(utf16Decode(buf)), name)
}

// bitmap is a 32-bit top-down DIB section selected into a memory DC.
type bitmap struct {
	dc, bmp, old uintptr
	px           []uint32
}

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

func newBitmap(screen uintptr, width, height int32) *bitmap {
	hdr := bitmapInfoHeader{Width: width, Height: -height, Planes: 1, BitCount: 32}
	hdr.Size = uint32(unsafe.Sizeof(hdr))
	var bits unsafe.Pointer
	bmp, _, _ := procCreateDIBSection.Call(screen, uintptr(unsafe.Pointer(&hdr)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bmp == 0 || bits == nil {
		return nil
	}
	dc, _, _ := procCreateCompatibleDC.Call(screen)
	old, _, _ := procSelectObject.Call(dc, bmp)
	return &bitmap{dc: dc, bmp: bmp, old: old, px: unsafe.Slice((*uint32)(bits), int(width)*int(height))}
}

func (b *bitmap) free() {
	procSelectObject.Call(b.dc, b.old)
	procDeleteDC.Call(b.dc)
	procDeleteObject.Call(b.bmp)
}

type trackMouseEvent struct {
	Size      uint32
	Flags     uint32
	Track     uintptr
	HoverTime uint32
}

// The maximize button that the content draws, in a window without the
// system's buttons (HideWindowButtons), gets Windows 11's snap layouts as
// the system's does: a window over it, layered without pixels as the top
// edge's, answers HTMAXBUTTON, and DefWindowProc then shows them as the
// pointer rests there. The pointer's moves and presses over it, which come
// as non-client messages, go on to the content, whose button hovers and
// clicks as any other.

// handleSnap handles the messages of the window over the maximize button,
// and placeSnap lays it out: SetMaximizeButton sets them as it makes the
// window, which package ui calls once a view marks such a button, so that
// apps that mark none don't link them.
var (
	handleSnap func(c *captionBar, m uint32, lp uintptr) (uintptr, bool)
	placeSnap  func(c *captionBar, dpi int)
)

// SetMaximizeButton puts the window that brings the snap layouts over the
// content's maximize button, or hides it for the zero RectF
// (platform.MaximizeButtonSurface).
func (s *surface) SetMaximizeButton(r platform.RectF) {
	w := s.w
	c := w.caption
	if c == nil || c.snapRect == r {
		return
	}
	c.snapRect = r
	if c.snap == 0 && r != (platform.RectF{}) {
		c.snap = createWindow(wsExLayered|wsExNoRedirect, captionClass, "", wsChild|wsClipSiblings, 0, 0, 0, 0, w.hwnd)
		if c.snap == 0 {
			return
		}
		procSetLayeredWindowAttributes.Call(c.snap, 0, 255, lwaAlpha)
		w.b.captions[c.snap] = c
		handleSnap, placeSnap = (*captionBar).snapMessage, (*captionBar).layoutSnap
	}
	if c.snap != 0 {
		c.layoutSnap(dpiOf(w.hwnd))
	}
}

func (c *captionBar) layoutSnap(dpi int) {
	w := c.w
	r := c.snapRect
	px := func(v float64) int32 { return int32(math.Round(v * float64(dpi) / 96)) }
	x0, y0, x1, y1 := px(r.X), px(r.Y), px(r.X+r.W), px(r.Y+r.H)
	show := uintptr(swpShowWindow)
	if w.fullScreen || w.surface == nil || x1 <= x0 || y1 <= y0 || !w.hasStyle(wsMaximizeBox) {
		show = swpHideWindow
	}
	// HWND_TOP (0): above the content's window.
	procSetWindowPos.Call(c.snap, 0, uintptr(x0), uintptr(y0), uintptr(x1-x0), uintptr(y1-y0), swpNoActivate|show)
}

// toContent gives the content a pointer event at the screen position of a
// non-client mouse message.
func (c *captionBar) toContent(kind platform.SurfaceEventKind, lp uintptr) {
	s := c.w.surface
	if s == nil {
		return
	}
	pt := point{getXParam(lp), getYParam(lp)}
	procScreenToClient.Call(s.hwnd, uintptr(unsafe.Pointer(&pt)))
	s.pointer(kind, uintptr(uint16(pt.X))|uintptr(uint16(pt.Y))<<16, 0)
}

// cursorOver reports whether the window under the pointer is hwnd.
func cursorOver(hwnd uintptr) bool {
	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	h, _, _ := procWindowFromPoint.Call(uintptr(uint32(pt.X)) | uintptr(uint32(pt.Y))<<32)
	return h == hwnd
}

func (c *captionBar) snapMessage(m uint32, lp uintptr) (uintptr, bool) {
	switch m {
	case wmNCHitTest:
		return htMaxButton, true
	case wmNCMouseMove:
		if !c.snapTrack {
			tme := trackMouseEvent{Flags: tmeLeave | tmeNonClient, Track: c.snap}
			tme.Size = uint32(unsafe.Sizeof(tme))
			procTrackMouseEvent.Call(uintptr(unsafe.Pointer(&tme)))
			c.snapTrack = true
		}
		c.toContent(platform.PointerMove, lp)
		// DefWindowProc brings the snap layouts over HTMAXBUTTON.
		return 0, false
	case wmNCMouseLeave, wmMouseLeave:
		c.snapTrack = false
		s := c.w.surface
		if s == nil {
			return 0, true
		}
		var pt point
		procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
		if c.snapPressed {
			// The press ends where the pointer went, as a captured one
			// would: no click outside the button.
			c.snapPressed = false
			c.toContent(platform.PointerUp, uintptr(uint16(pt.X))|uintptr(uint16(pt.Y))<<16)
		}
		// The content tells its own pointer's moves.
		if !cursorOver(s.hwnd) {
			s.send(platform.SurfaceEvent{Kind: platform.PointerLeave})
		}
		return 0, true
	case wmNCLButtonDown, wmNCLButtonDblClk:
		// Not DefWindowProc, which would maximize the window itself.
		c.snapPressed = true
		c.toContent(platform.PointerDown, lp)
		return 0, true
	case wmNCLButtonUp:
		if c.snapPressed {
			c.snapPressed = false
			c.toContent(platform.PointerUp, lp)
		}
		return 0, true
	case wmNCRButtonDown, wmNCRButtonUp, wmNCRButtonDblClk:
		return 0, true
	}
	return 0, false
}
