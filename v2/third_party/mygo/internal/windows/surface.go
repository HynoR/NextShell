//go:build windows && (amd64 || arm64)

package windows

import (
	"log"
	"math"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/egoist/mygo/internal/platform"
)

// The surface of a window that shows content MyGo draws itself is a child
// window filling its client area, below the caption controls of a hidden
// title bar, as the webview's window is. Renderers draw into it with a
// swap chain, or the backend copies frames drawn in memory onto it.

const surfaceClass = "MyGoSurface"

var (
	imm32 = systemDLL("imm32.dll")

	procSetCapture            = user32.NewProc("SetCapture")
	procSetCursor             = user32.NewProc("SetCursor")
	procBeginPaint            = user32.NewProc("BeginPaint")
	procEndPaint              = user32.NewProc("EndPaint")
	procRedrawWindow          = user32.NewProc("RedrawWindow")
	procSystemParametersInfoW = user32.NewProc("SystemParametersInfoW")
	procGetDoubleClickTime    = user32.NewProc("GetDoubleClickTime")
	procGetMessageTime        = user32.NewProc("GetMessageTime")
	procGetSystemMetrics      = user32.NewProc("GetSystemMetrics")
	procSetDIBitsToDevice     = gdi32.NewProc("SetDIBitsToDevice")
	procImmGetContext         = imm32.NewProc("ImmGetContext")
	procImmReleaseContext     = imm32.NewProc("ImmReleaseContext")
	procImmGetCompositionStrW = imm32.NewProc("ImmGetCompositionStringW")
	procImmSetCompositionWnd  = imm32.NewProc("ImmSetCompositionWindow")
	procImmSetCandidateWindow = imm32.NewProc("ImmSetCandidateWindow")
	procImmAssociateContextEx = imm32.NewProc("ImmAssociateContextEx")
	procImmNotifyIME          = imm32.NewProc("ImmNotifyIME")
	surfaceClassRegistered    bool
	surfaceCursors            = map[platform.Cursor]uintptr{}
	surfaceWheelLines         = 0
	surfaceWheelLinesQueried  = false
)

const (
	wmPaint              = 0x000F
	wmKillFocus          = 0x0008
	wmSetCursor          = 0x0020
	wmGetDlgCode         = 0x0087
	wmKeyDown            = 0x0100
	wmKeyUp              = 0x0101
	wmChar               = 0x0102
	wmSysKeyDown         = 0x0104
	wmSysKeyUp           = 0x0105
	wmSysChar            = 0x0106
	wmImeStartComp       = 0x010D
	wmImeEndComp         = 0x010E
	wmImeComposition     = 0x010F
	wmMouseMove          = 0x0200
	wmLButtonDown        = 0x0201
	wmLButtonDblClk      = 0x0203
	wmRButtonDown        = 0x0204
	wmRButtonDblClk      = 0x0206
	wmMButtonDown        = 0x0207
	wmMButtonUp          = 0x0208
	wmMButtonDblClk      = 0x0209
	wmMouseWheel         = 0x020A
	wmMouseHWheel        = 0x020E
	wmXButtonDown        = 0x020B
	wmXButtonUp          = 0x020C
	wmXButtonDblClk      = 0x020D
	wmAppCommand         = 0x0319
	wmCaptureChanged     = 0x0215
	wmImeSetContext      = 0x0281
	wmImeChar            = 0x0286
	wmGetObject          = 0x003D
	dlgcWantAllKeys      = 0x0004
	dlgcWantChars        = 0x0080
	iscShowUICompWindow  = 0x80000000
	gcsCompStr           = 0x0008
	gcsCursorPos         = 0x0080
	gcsResultStr         = 0x0800
	cfsPoint             = 0x0002
	cfsExclude           = 0x0080
	iaceDefault          = 0x0010
	niCompositionStr     = 0x0015
	cpsComplete          = 0x0001
	cpsCancel            = 0x0004
	rdwInvalidate        = 0x0001
	rdwUpdateNow         = 0x0100
	spiGetWheelScrollLns = 0x0068
	tmeLeaveFlag         = 0x0002
)

type paintStruct struct {
	HDC       uintptr
	Erase     int32
	Paint     rect
	Restore   int32
	IncUpdate int32
	Reserved  [32]byte
}

type compositionForm struct {
	Style uint32
	Pos   point
	Area  rect
}

type candidateForm struct {
	Index uint32
	Style uint32
	Pos   point
	Area  rect
}

type surface struct {
	clientComposition platform.InputComposition
	w                 *window
	hwnd              uintptr

	paintDC uintptr
	// now tells that a frame draws at once: one a resize draws, or one
	// FrameNow draws for a frame left to draw (platform.PacedSurface).
	now      bool
	tracking bool
	buttons  int
	// pressure is the last WM_POINTER message's (mouseDevice).
	pressure float64
	clicks   platform.ClickCounter
	cursor   platform.Cursor
	high     uint16 // a high surrogate of WM_CHAR waiting for its pair
	ime      bool
	caret    platform.RectF
	input    platform.TextInputState

	// pointerAt is where on the screen the last WM_POINTER message's
	// pointer was, in pixels (pointerMessage).
	pointerAt point
	// canceledUp tells that the system took the press of the pen or the
	// finger (cancelPress): the mouse button's release that follows is
	// its, and goes nowhere.
	canceledUp bool

	// keyTaken tells that the content took the key down, whose
	// WM_SYSCHAR then opens no menu.
	keyTaken bool
	// menuRan tells that the key down ran a menu item (menuShortcut): its
	// character goes nowhere.
	menuRan bool
	// held are the keys the content got pressed and not yet released,
	// whose releases a menu's loop or an input method may take (liftKeys).
	held platform.HeldKeys

	reconvert  *[2]int // the runes a reconversion replaces
	dropTarget uintptr // IDropTarget
	access     *uiaTree
	dragSource *oleDragSource

	// comp shows the frames drawn in memory in a window without a
	// redirection bitmap, where GDI cannot (compositor.go); nil while none
	// shows.
	comp *compositor
	// gdiFailed tells that SetDIBitsToDevice refused a frame, which was
	// logged once.
	gdiFailed bool
	// pixels tells that the frames are drawn in memory and copied onto the
	// window's own pixels with GDI (PresentPixels), rather than presented
	// by a GPU renderer's swap chain.
	pixels bool
	// drawn tells that the content's first frame reached the surface
	// (FirstFrame), and cloaked that DWM keeps the window out of sight
	// until a frame shows in it (cloak). hiddenFrame tells that the first
	// frame of a window hidden, which no WM_PAINT asks for, is posted
	// (drawHidden).
	drawn, cloaked, hiddenFrame bool

	// bg is the content's background (SetBackground), and bgBrush a brush
	// of it while it is opaque, which fills what the window shows beyond
	// the surface as it grows (fillUncovered), and the window's own pixels
	// under the surface, which show as it shrinks (fillUnder).
	bg      platform.Color
	bgBrush uintptr

	// layer holds the web views under the frames, once there are some
	// (webview_embed.go): placed are where the content shows them, and
	// stacked those shown, in the order of their visuals. webHover is the
	// web view under the pointer, and webCapture the one a press went to,
	// which gets the mouse until the buttons are let go.
	layer                *webLayer
	placed               []platform.WebViewPlacement
	stacked              []*window
	webHover, webCapture *window
}

func registerSurfaceClass() {
	if surfaceClassRegistered {
		return
	}
	surfaceClassRegistered = true
	wc := wndClassEx{
		WndProc:   wndProcCallback,
		Instance:  instance(),
		ClassName: u16(surfaceClass),
	}
	wc.Size = uint32(unsafe.Sizeof(wc))
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
}

func newSurface(w *window) *surface {
	registerSurfaceClass()
	s := &surface{w: w}
	s.hwnd = createWindow(0, surfaceClass, "", wsChild|wsVisible|wsClipSiblings, 0, 0, 0, 0, w.hwnd)
	if s.hwnd == 0 {
		return nil
	}
	w.b.surfaces[s.hwnd] = s
	s.cloak()
	// Input methods only come to text inputs.
	procImmAssociateContextEx.Call(s.hwnd, 0, 0)
	s.acceptFileDrops()
	s.fit()
	return s
}

// fit makes the surface fill the client area of its window.
func (s *surface) fit() {
	var r rect
	procGetClientRect.Call(s.w.hwnd, uintptr(unsafe.Pointer(&r)))
	procSetWindowPos.Call(s.hwnd, 0, 0, 0, uintptr(r.Right-r.Left), uintptr(r.Bottom-r.Top), swpNoZOrder|swpNoActivate)
}

func (s *surface) dpi() int { return dpiOf(s.w.hwnd) }

func (s *surface) toDIP(v int32) float64 { return float64(v) * 96 / float64(s.dpi()) }

// Native returns the surface's window, which a GPU renderer is about to
// draw into: in a window without a redirection bitmap, it composes the
// window from then on, and the frames drawn in memory let go of it.
func (s *surface) Native() platform.SurfaceNative {
	s.freeComp()
	s.pixels = false
	return platform.SurfaceNative{HWND: s.hwnd, Composed: s.viaComposition()}
}

// viaComposition reports whether the frames show through
// DirectComposition: in a window without a redirection bitmap, and over
// web views.
func (s *surface) viaComposition() bool { return s.w.noRedirect || s.layer != nil }

// ShowsMaterial reports whether the window shows its material behind the
// content: it has one, no redirection bitmap to cover it, and DWM draws
// materials. A flyout has no redirection bitmap before Windows 11 22H2 as
// well, to show what is behind it, but no material there.
func (s *surface) ShowsMaterial() bool {
	return s.w.noRedirect && s.w.vibrancy != "" && systemBackdrops()
}

func (s *surface) freeComp() {
	if s.comp != nil {
		s.comp.free()
		s.comp = nil
	}
}

func (s *surface) Size() (float64, float64, float64) {
	var r rect
	procGetClientRect.Call(s.hwnd, uintptr(unsafe.Pointer(&r)))
	return s.toDIP(r.Right - r.Left), s.toDIP(r.Bottom - r.Top), float64(s.dpi()) / 96
}

func (s *surface) RequestFrame() {
	procInvalidateRectW.Call(s.hwnd, 0, 0)
	if !s.drawn && !s.hiddenFrame && !s.w.IsVisible() {
		s.hiddenFrame = true
		s.w.b.post(s.drawHidden)
	}
}

// The window of native UI is cloaked, which keeps it out of sight though
// it is visible to Windows, from its creation until a frame of its content
// shows in it: shown before, it would show what it holds until then,
// white, for as long as the GPU renderer takes to start and the first
// frame to draw (#905). Windows sends a cloaked window WM_PAINT as any
// visible one, whose frame then shows it (reveal). A window made hidden
// gets no WM_PAINT: its first frame draws all the same (drawHidden), which
// makes it ready to show (OnReadyToShow) and shows as the window does,
// from the swap chain. Should no frame show in a window shown, it shows
// once timerUncloak fires, uncloakAfter later, which needs the main thread
// to be idle meanwhile: drawing a frame never lets it fire.
const (
	dwmwaCloak   = 13 // DWMWA_CLOAK
	timerUncloak = 3  // the surface's timer that shows a window still cloaked
)

var uncloakAfter = time.Second

// cloak keeps the window out of sight until uncloak.
func (s *surface) cloak() {
	on := int32(1)
	hr, _, _ := procDwmSetWindowAttribute.Call(s.w.hwnd, dwmwaCloak, uintptr(unsafe.Pointer(&on)), unsafe.Sizeof(on))
	s.cloaked = hr == 0
}

// uncloak shows the window, as Windows shows it.
func (s *surface) uncloak() {
	if !s.cloaked {
		return
	}
	s.cloaked = false
	procKillTimer.Call(s.hwnd, timerUncloak)
	off := int32(0)
	procDwmSetWindowAttribute.Call(s.w.hwnd, dwmwaCloak, uintptr(unsafe.Pointer(&off)), unsafe.Sizeof(off))
}

// showing tells that Windows is about to show the window: while it is
// cloaked, the timer that shows it should no frame show in it starts.
func (s *surface) showing() {
	if s.cloaked {
		procSetTimer.Call(s.hwnd, timerUncloak, uintptr(uncloakAfter/time.Millisecond), 0)
	}
}

// reveal shows the window, cloaked until a frame of the content showed in
// it, after the WM_PAINT that drew one, or, for a window that drew its
// first frame while hidden, the first since it is visible. Should DWM show
// the window a refresh before the frame presented, it shows the window's
// own pixels under the surface, which hold the content's background
// (fillUnder): drawn nowhere while the window was hidden, they are drawn
// now. It does not wait for DWM (DwmFlush), which may wait on the main
// thread until something else on the screen changes.
func (s *surface) reveal() {
	s.fillUnder()
	procGdiFlush.Call()
	s.uncloak()
}

// FirstFrame tells that the content's first frame reached the surface
// (platform.FirstFrameSurface).
func (s *surface) FirstFrame() { s.drawn = true }

var _ platform.FirstFrameSurface = (*surface)(nil)

// drawHidden draws the content's first frame in a window hidden, which no
// WM_PAINT asks for: the window is then ready to show, and shows the frame
// from the swap chain as it shows. A window shown since gets WM_PAINT.
func (s *surface) drawHidden() {
	s.hiddenFrame = false
	if s.drawn || s.w.closed || s.w.b.surfaces[s.hwnd] != s || s.w.IsVisible() {
		return
	}
	s.send(platform.SurfaceEvent{Kind: platform.SurfaceFrame})
}

// FramesAtOnce marks that RequestFrame paints at once, not at the display's
// next refresh (platform.ImmediateFrameSurface).
func (s *surface) FramesAtOnce() {}

// FrameNow draws a frame at once, for one the content left to draw until
// the swap chain had room for it.
func (s *surface) FrameNow() {
	if !s.w.closed {
		s.drawNow()
	}
}

// drawNow draws a frame at once, in WM_PAINT.
func (s *surface) drawNow() {
	now := s.now
	s.now = true
	procRedrawWindow.Call(s.hwnd, 0, 0, rdwInvalidate|rdwUpdateNow)
	s.now = now
}

// Occluded reports whether nothing of the window shows on screen: while it
// is minimized. Windows covered by others, or on another virtual desktop,
// show.
func (s *surface) Occluded() bool { return s.w.state == sizeMinimized }

// RefreshRate returns the refresh rate of the monitor showing the window,
// in its current display mode.
func (s *surface) RefreshRate() float64 {
	mi := monitorInfo(s.w.monitor())
	var dm devMode
	dm.Size = uint16(unsafe.Sizeof(dm))
	if ok, _, _ := procEnumDisplaySettingsW.Call(uintptr(unsafe.Pointer(&mi.Device[0])), enumCurrentSettings, uintptr(unsafe.Pointer(&dm))); ok == 0 {
		return 0
	}
	if dm.DisplayFrequency <= 1 {
		return 0 // the hardware's default rate, unknown
	}
	return float64(dm.DisplayFrequency)
}

// AwaitRefresh returns once DWM composes its next frame (DwmFlush): the
// frames drawn in memory show at once, through GDI or DirectComposition,
// and WM_PAINT comes as soon as one is asked for, so the content holds
// them to the refreshes with it (platform.RefreshSurface). It is called
// off the main thread.
func (s *surface) AwaitRefresh() { procDwmFlush.Call() }

var _ platform.RefreshSurface = (*surface)(nil)

// SetBackground keeps a brush of the content's background, for the window
// to show where it grew before the surface's frame of its new size shows
// (platform.BackgroundSurface).
func (s *surface) SetBackground(c platform.Color) {
	if c == s.bg {
		return
	}
	s.bg = c
	if s.bgBrush != 0 {
		procDeleteObject.Call(s.bgBrush)
		s.bgBrush = 0
	}
	if c.A == 255 {
		s.bgBrush, _, _ = procCreateSolidBrush.Call(uintptr(c.R) | uintptr(c.G)<<8 | uintptr(c.B)<<16)
		s.fillUnder()
	}
}

var _ platform.BackgroundSurface = (*surface)(nil)

// fillUncovered paints the part of the window's client area that the
// surface does not cover yet, as the window has just grown, with the
// content's background. Windows resizes the window, and DWM may show it at
// its new size, before WM_SIZE fits the surface to it and the surface
// presents the frame of that size: until then, DWM would show what the
// window's redirection bitmap holds there, black, as the window paints no
// background of its own. Only that part is painted, before the frame of
// the new size: under the surface, the swap chain shows over what GDI
// draws, and the frame is not slowed (fillUnder paints there after it).
func (s *surface) fillUncovered() {
	if s.bgBrush == 0 || s.w.noRedirect {
		// A window without a redirection bitmap shows nothing GDI draws:
		// its material shows there, not black.
		return
	}
	var c, sr rect
	procGetClientRect.Call(s.w.hwnd, uintptr(unsafe.Pointer(&c)))
	procGetClientRect.Call(s.hwnd, uintptr(unsafe.Pointer(&sr))) // the surface is at the client area's origin
	if sr.Right >= c.Right && sr.Bottom >= c.Bottom {
		return // the window did not grow
	}
	dc, _, _ := procGetDC.Call(s.w.hwnd)
	if dc == 0 {
		return
	}
	procExcludeClipRect.Call(dc, 0, 0, uintptr(sr.Right), uintptr(sr.Bottom))
	procFillRect.Call(dc, uintptr(unsafe.Pointer(&c)), s.bgBrush)
	procReleaseDC.Call(s.w.hwnd, dc)
	procGdiFlush.Call()
}

// fillUnder paints the window's whole client area with the content's
// background, under the surface too, once the surface took the window's
// size, and when the background changes. As the window shrinks, WM_SIZE
// fits the surface to the new size, and the surface presents the frame of
// that size, within the window's SetWindowPos, before DWM takes the
// window's new size: for a refresh, DWM shows the window at its last size
// around the smaller surface, and there what the window's redirection
// bitmap holds where the surface was. Nothing painted it there, as the
// swap chain covered it, so it showed black, as an L along the surface's
// new right and bottom edges but where an earlier size had left the
// background. Painting it at each size keeps the background there for the
// next shrink; the swap chain shows over it meanwhile. Frames copied onto
// the window with GDI are in those pixels, which keep the last one where
// the window shrinks: they are left as they are, as in a window without a
// redirection bitmap, or one composing its frames, where GDI shows nothing.
func (s *surface) fillUnder() {
	if s.bgBrush == 0 || s.pixels || s.viaComposition() {
		return
	}
	var c rect
	procGetClientRect.Call(s.w.hwnd, uintptr(unsafe.Pointer(&c)))
	// A DC of the window's client area that does not leave out its child
	// windows, as GetDC does for a window with WS_CLIPCHILDREN.
	const dcxCache = 0x2
	dc, _, _ := procGetDCEx.Call(s.w.hwnd, 0, dcxCache)
	if dc == 0 {
		return
	}
	procFillRect.Call(dc, uintptr(unsafe.Pointer(&c)), s.bgBrush)
	procReleaseDC.Call(s.w.hwnd, dc)
	procGdiFlush.Call()
}

func (s *surface) PresentPixels(pix []byte, stride, width, height int) {
	if len(pix) < stride*height || width == 0 || height == 0 {
		return
	}
	if s.viaComposition() {
		// GDI draws nothing in a window without a redirection bitmap, and
		// would draw under the web views: the frame shows through
		// DirectComposition, with its alpha.
		if s.comp == nil {
			s.comp = newCompositor(s.w.b, s.hwnd)
		}
		px := s.comp.pixels(width * height)
		for y := range height {
			copy(px[y*width:(y+1)*width], unsafe.Slice((*uint32)(unsafe.Pointer(&pix[y*stride])), width))
		}
		s.comp.show(px, int32(width), int32(height))
		return
	}
	s.pixels = true
	dc := s.paintDC
	if dc == 0 {
		dc, _, _ = procGetDC.Call(s.hwnd)
		defer procReleaseDC.Call(s.hwnd, dc)
	}
	bi := bitmapInfoHeader{Width: int32(stride / 4), Height: -int32(height), Planes: 1, BitCount: 32}
	bi.Size = uint32(unsafe.Sizeof(bi))
	lines, _, _ := procSetDIBitsToDevice.Call(dc, 0, 0, uintptr(width), uintptr(height), 0, 0, 0, uintptr(height),
		uintptr(unsafe.Pointer(&pix[0])), uintptr(unsafe.Pointer(&bi)), 0)
	if lines == 0 && !s.gdiFailed {
		s.gdiFailed = true
		log.Printf("mygo: native UI shows nothing: SetDIBitsToDevice drew no lines of a %dx%d frame", width, height)
	}
}

func (s *surface) SetCursor(c platform.Cursor) {
	s.cursor = c
	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	procScreenToClient.Call(s.hwnd, uintptr(unsafe.Pointer(&pt)))
	var r rect
	procGetClientRect.Call(s.hwnd, uintptr(unsafe.Pointer(&r)))
	if pt.X >= r.Left && pt.Y >= r.Top && pt.X < r.Right && pt.Y < r.Bottom {
		procSetCursor.Call(cursorHandle(c))
	}
}

func cursorHandle(c platform.Cursor) uintptr {
	if h, ok := surfaceCursors[c]; ok {
		return h
	}
	ids := map[platform.Cursor]uintptr{
		platform.CursorDefault: 32512, platform.CursorPointer: 32649, platform.CursorText: 32513,
		platform.CursorMove: 32646, platform.CursorResizeEW: 32644, platform.CursorResizeNS: 32645,
		platform.CursorResizeNWSE: 32642, platform.CursorResizeNESW: 32643, platform.CursorNotAllowed: 32648,
		platform.CursorCrosshair: 32515, platform.CursorGrab: 32649, platform.CursorGrabbing: 32646,
		// Windows has no cursors of one direction, of columns and rows, or
		// for vertical text: those of both directions and the I-beam.
		platform.CursorResizeN: 32645, platform.CursorResizeS: 32645, platform.CursorResizeE: 32644,
		platform.CursorResizeW: 32644, platform.CursorResizeColumn: 32644, platform.CursorResizeRow: 32645,
		platform.CursorVerticalText: 32513,
		platform.CursorProgress:     32650, platform.CursorWait: 32514, platform.CursorHelp: 32651, // IDC_APPSTARTING, IDC_WAIT, IDC_HELP
		platform.CursorAllScroll: 32646, platform.CursorNoDrop: 32648, // IDC_SIZEALL, IDC_NO
	}
	if c == platform.CursorNone {
		surfaceCursors[c] = 0 // SetCursor(NULL) hides it
		return 0
	}
	if h := drawnCursor(c); h != 0 {
		surfaceCursors[c] = h
		return h
	}
	id, ok := ids[c]
	if !ok {
		id = 32512
	}
	h, _, _ := procLoadCursorW.Call(0, id)
	surfaceCursors[c] = h
	return h
}

func (s *surface) SetTextInput(t platform.TextInputState) {
	// Without input methods, the window has no input context: keys type
	// the layout's characters, as WM_CHAR, as winit's do.
	active, caret := t.Active && !t.NoInputMethod, t.Caret
	if s.input.Active && s.input.Client != t.Client {
		s.clientComposition.Reset()
		if himc, _, _ := procImmGetContext.Call(s.hwnd); himc != 0 {
			procImmNotifyIME.Call(himc, niCompositionStr, cpsCancel, 0)
			procImmReleaseContext.Call(s.hwnd, himc)
		}
		s.reconvert = nil
	}
	s.input = t
	if active != s.ime {
		s.ime = active
		if active {
			procImmAssociateContextEx.Call(s.hwnd, 0, iaceDefault)
		} else {
			s.reconvert = nil
			if himc, _, _ := procImmGetContext.Call(s.hwnd); himc != 0 {
				procImmNotifyIME.Call(himc, niCompositionStr, cpsComplete, 0)
				procImmReleaseContext.Call(s.hwnd, himc)
			}
			procImmAssociateContextEx.Call(s.hwnd, 0, 0)
		}
	}
	s.caret = caret
	if active {
		s.placeIME()
	}
}

// placeIME puts the input method's composition and candidate windows at
// the caret.
func (s *surface) placeIME() {
	himc, _, _ := procImmGetContext.Call(s.hwnd)
	if himc == 0 {
		return
	}
	defer procImmReleaseContext.Call(s.hwnd, himc)
	dpi := s.dpi()
	x, y := toPx(int(s.caret.X), dpi), toPx(int(s.caret.Y), dpi)
	h := toPx(int(s.caret.H+0.5), dpi)
	cf := compositionForm{Style: cfsPoint, Pos: point{x, y}}
	procImmSetCompositionWnd.Call(himc, uintptr(unsafe.Pointer(&cf)))
	cand := candidateForm{Style: cfsExclude, Pos: point{x, y + h}, Area: rect{x, y, x + 1, y + h}}
	procImmSetCandidateWindow.Call(himc, uintptr(unsafe.Pointer(&cand)))
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

// liftKeys lets go of the keys held, as a menu's loop takes the keyboard,
// and their releases with it: Alt and a letter opening a menu of the bar,
// Alt and Space the window's menu, F10 or Alt the bar. The window keeps
// the focus meanwhile, so no WM_KILLFOCUS lets go of them.
func (s *surface) liftKeys() { s.held.LiftAll(mods(), s.send) }

// modifierKey is whether the virtual key vk is Shift, Ctrl, Alt or a Windows key.
func modifierKey(vk uintptr) bool {
	switch vk {
	case vkShift, vkControl, vkMenu, vkLWin, vkRWin, 0xA0, 0xA1, 0xA2, 0xA3, 0xA4, 0xA5:
		return true
	}
	return false
}

func mods() platform.Modifiers {
	var m platform.Modifiers
	down := func(vk uintptr) bool {
		r, _, _ := procGetKeyState.Call(vk)
		return r&0x8000 != 0
	}
	if down(vkShift) {
		m |= platform.ModShift
	}
	if down(vkControl) {
		m |= platform.ModCtrl
	}
	if down(vkMenu) {
		m |= platform.ModAlt
	}
	if down(vkLWin) || down(vkRWin) {
		m |= platform.ModSuper
	}
	return m
}

// systemMetric is GetSystemMetrics' metric for the DPI of the window hwnd.
func systemMetric(metric, hwnd uintptr) uintptr {
	if has(procGetSystemMetricsForDpi) {
		if n, _, _ := procGetSystemMetricsForDpi.Call(metric, uintptr(dpiOf(hwnd))); n != 0 {
			return n
		}
	}
	n, _, _ := procGetSystemMetrics.Call(metric)
	return n
}

func (s *surface) pointer(kind platform.SurfaceEventKind, lp uintptr, button int) {
	x, y := int16(loword(lp)), int16(hiword(lp))
	clicks := 0
	if kind == platform.PointerDown {
		// A press carries no count: it follows the double-click time and
		// the rectangle centered on the last press, in the window's pixels,
		// as set.
		ms, _, _ := procGetDoubleClickTime.Call()
		at, _, _ := procGetMessageTime.Call()
		w, h := systemMetric(smCxDoubleClk, s.hwnd), systemMetric(smCyDoubleClk, s.hwnd)
		clicks = s.clicks.Press(button, float64(x), float64(y), uint32(at), platform.DoubleClick{Time: time.Duration(ms) * time.Millisecond, DX: float64(w) / 2, DY: float64(h) / 2})
	}
	pointer, pressure := s.mouseDevice()
	s.send(platform.SurfaceEvent{Kind: kind, X: s.toDIP(int32(x)), Y: s.toDIP(int32(y)), Button: button, Clicks: clicks, Mods: mods(), Pointer: pointer, Pressure: pressure})
}

func (s *surface) message(hwnd uintptr, m uint32, wp, lp uintptr) (uintptr, bool) {
	switch m {
	case wmMouseMove, wmLButtonDown, wmLButtonDblClk, wmLButtonUp, wmRButtonDown, wmRButtonDblClk, wmRButtonUp,
		wmMButtonDown, wmMButtonDblClk, wmMButtonUp, wmXButtonDown, wmXButtonDblClk, wmXButtonUp, wmMouseWheel, wmMouseHWheel:
		if s.forwardMouse(m, wp, lp) {
			return 0, true // the web view under the pointer takes it
		}
	case wmTimer:
		if wp == timerUncloak {
			s.uncloak()
			return 0, true
		}
		if wp == timerWebView {
			procKillTimer.Call(hwnd, timerWebView)
			for _, v := range s.w.webViews {
				if v.controller == 0 && v.webViewErr == nil {
					v.createWebView()
				}
			}
			return 0, true
		}
	}
	switch m {
	case wmPaint:
		var ps paintStruct
		dc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		s.paintDC = dc
		// Frames that follow each other as the content asks for them may
		// wait until the swap chain has room for one, or, drawn in memory,
		// until the display's next refresh (AwaitRefresh).
		s.send(platform.SurfaceEvent{Kind: platform.SurfaceFrame, Paced: !s.now})
		s.paintDC = 0
		procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		if s.cloaked && s.drawn {
			s.reveal()
		}
		return 0, true
	case wmEraseBkgnd:
		return 1, true
	case wmSize:
		if s.layer != nil {
			// The web views keep their place in DIPs, in other pixels after
			// a change of DPI.
			for _, v := range s.w.webViews {
				v.placeEmbedded()
			}
			s.layer.commit()
		}
		s.send(platform.SurfaceEvent{Kind: platform.SurfaceResize})
		// Draw at once, so the content follows the window while it is
		// resized.
		s.drawNow()
		return 0, true
	case wmPointerDown, wmPointerUpdate, wmPointerUp:
		// A pen or a finger, whose mouse messages DefWindowProc makes.
		s.pointerMessage(m, wp)
		if m == wmPointerUp && pointerCanceled(wp) && s.buttons != 0 {
			// The system took the pen or the finger, as for an edge's
			// gesture: the press ends with no release, and the button
			// up DefWindowProc may make of it goes nowhere. The capture
			// goes after the buttons, so that losing it cancels no more.
			s.cancelPress()
			s.canceledUp = true
			procReleaseCapture.Call()
		}
		return 0, false
	case wmMouseMove:
		if !s.tracking {
			tme := trackMouseEvent{Flags: tmeLeaveFlag, Track: hwnd}
			tme.Size = uint32(unsafe.Sizeof(tme))
			procTrackMouseEvent.Call(uintptr(unsafe.Pointer(&tme)))
			s.tracking = true
		}
		s.pointer(platform.PointerMove, lp, 0)
		return 0, true
	case wmMouseLeave:
		s.tracking = false
		if c := s.w.caption; c != nil && c.snap != 0 && cursorOver(c.snap) {
			// Over the maximize button the content draws, whose window
			// gives the content the pointer's moves.
			return 0, true
		}
		s.mouseLeft()
		s.send(platform.SurfaceEvent{Kind: platform.PointerLeave})
		return 0, true
	case wmLButtonDown, wmLButtonDblClk, wmRButtonDown, wmRButtonDblClk, wmMButtonDown, wmMButtonDblClk:
		button := 0
		switch m {
		case wmRButtonDown, wmRButtonDblClk:
			button = 1
		case wmMButtonDown, wmMButtonDblClk:
			button = 2
		}
		procSetFocus.Call(hwnd)
		if s.buttons == 0 {
			procSetCapture.Call(hwnd)
		}
		s.buttons |= 1 << button
		s.canceledUp = false
		s.pointer(platform.PointerDown, lp, button)
		return 0, true
	case wmLButtonUp, wmRButtonUp, wmMButtonUp:
		button := 0
		switch m {
		case wmRButtonUp:
			button = 1
		case wmMButtonUp:
			button = 2
		}
		if s.canceledUp && s.buttons == 0 {
			// The release of a press the system took (cancelPress),
			// which opens no context menu either.
			s.canceledUp = false
			return 0, true
		}
		s.buttons &^= 1 << button
		if s.buttons == 0 {
			procReleaseCapture.Call()
		}
		s.pointer(platform.PointerUp, lp, button)
		if m == wmRButtonUp {
			return 0, false // WM_CONTEXTMENU
		}
		return 0, true
	case wmXButtonDown, wmXButtonDblClk, wmXButtonUp:
		// The side buttons go back and forward, as keys do, as they are
		// let go: Windows's apps go there then, as DefWindowProc sends
		// WM_APPCOMMAND, which taking them leaves out.
		if m == wmXButtonUp {
			k := platform.KeyBack
			if hiword(wp) == 2 { // XBUTTON2
				k = platform.KeyForward
			}
			s.send(platform.SurfaceEvent{Kind: platform.KeyPressed, Key: k, Mods: mods()})
			s.send(platform.SurfaceEvent{Kind: platform.KeyReleased, Key: k, Mods: mods()})
		}
		return 1, true
	case wmAppCommand:
		// Devices that send commands rather than keys or buttons.
		k := platform.KeyUnknown
		switch hiword(lp) &^ 0xF000 {
		case 1: // APPCOMMAND_BROWSER_BACKWARD
			k = platform.KeyBack
		case 2: // APPCOMMAND_BROWSER_FORWARD
			k = platform.KeyForward
		}
		if k == platform.KeyUnknown {
			return 0, false
		}
		s.send(platform.SurfaceEvent{Kind: platform.KeyPressed, Key: k, Mods: mods()})
		s.send(platform.SurfaceEvent{Kind: platform.KeyReleased, Key: k, Mods: mods()})
		return 1, true
	case wmCaptureChanged:
		// Another window took the mouse while a button was down, as a
		// menu, or WM_CANCELMODE released it: the press ends with no
		// release. OLE's drag loop takes it too (StartDataDrag), and
		// the drag's end ends the press. The capture is gone already:
		// releasing it would take another window's.
		if s.buttons != 0 && lp != hwnd && s.dragSource == nil {
			s.cancelPress()
		}
		s.buttons = 0
		if lp != hwnd {
			s.webCapture = nil
		}
		return 0, true
	case wmMouseWheel, wmMouseHWheel:
		pt := point{int32(int16(loword(lp))), int32(int16(hiword(lp)))}
		procScreenToClient.Call(hwnd, uintptr(unsafe.Pointer(&pt)))
		delta := float64(int16(hiword(wp)))
		precise := int(delta)%120 != 0
		if pinching(m, wp) {
			// A touchpad's pinch, which Windows sends as scrolling with
			// Ctrl held, as it does Ctrl and two fingers: three notches'
			// worth zoom twice as large.
			s.send(platform.SurfaceEvent{Kind: platform.PointerZoom, X: s.toDIP(pt.X), Y: s.toDIP(pt.Y), Scale: math.Exp(delta / 120 * math.Ln2 / 3), Mods: mods()})
			return 0, true
		}
		// A line of the system's "lines to scroll" is 33 DIPs, as
		// browsers scroll about 100 pixels a notch at the default 3;
		// the terminal plugin scrolls a row a line (wheelRows).
		dist := delta / 120 * float64(wheelLines()) * 33
		ev := platform.SurfaceEvent{Kind: platform.PointerScroll, X: s.toDIP(pt.X), Y: s.toDIP(pt.Y), Mods: mods(), Precise: precise}
		if m == wmMouseWheel {
			ev.DY = -dist
		} else {
			ev.DX = dist
		}
		s.send(ev)
		return 0, true
	case wmSetCursor:
		if loword(lp) == htClient {
			if s.webCursor() {
				return 1, true // the page's
			}
			procSetCursor.Call(cursorHandle(s.cursor))
			return 1, true
		}
	case wmGetDlgCode:
		return dlgcWantAllKeys | dlgcWantChars, true
	case wmSetFocus:
		s.send(platform.SurfaceEvent{Kind: platform.SurfaceFocus})
		return 0, true
	case wmKillFocus:
		s.send(platform.SurfaceEvent{Kind: platform.SurfaceBlur})
		return 0, true
	case wmKeyDown, wmSysKeyDown:
		s.clientComposition.Reset()
		s.keyTaken, s.menuRan = false, false
		modifiers := mods()
		if modifierKey(wp) {
			s.send(platform.SurfaceEvent{Kind: platform.ModifiersChanged, Mods: modifiers})
		}
		if s.menuShortcut(wp, false) {
			return 0, true
		}
		rawTaken := false
		k, physical, ok := keyAndPosition(wp, lp, false, modifiers)
		if ok {
			s.keyTaken = s.send(platform.SurfaceEvent{Kind: platform.KeyPressed, Key: k, PhysicalKey: physical, Code: keyCode(wp, lp), Mods: modifiers, Repeat: lp&(1<<30) != 0})
		} else {
			rawTaken = s.rawKey(platform.RawKeyPressed, wp, lp, physical, modifiers)
			s.keyTaken = rawTaken
		}
		if !s.keyTaken && s.menuShortcut(wp, true) {
			return 0, true
		}
		// Alt+F4, Alt+Space and F10 keep working, and Alt the menu bar, unless
		// the content took it as a key of its own.
		return 0, m == wmKeyDown || rawTaken
	case wmKeyUp, wmSysKeyUp:
		modifiers := mods()
		if modifierKey(wp) {
			s.send(platform.SurfaceEvent{Kind: platform.ModifiersChanged, Mods: modifiers})
		}
		rawTaken := false
		k, physical, ok := keyAndPosition(wp, lp, true, modifiers)
		if ok {
			s.send(platform.SurfaceEvent{Kind: platform.KeyReleased, Key: k, PhysicalKey: physical, Code: keyCode(wp, lp), Mods: modifiers})
		} else if wp == vkProcessKey && lp&(1<<24) == 0 {
			// The input method took the release of a key the content got
			// pressed, as it began composing while the key was held: the
			// key goes up all the same, by its position.
			s.held.Lift(platform.KeyUnknown, platform.KeyForScancode(uint16(lp>>16&0xFF)), modifiers, s.send)
		} else {
			rawTaken = s.rawKey(platform.RawKeyReleased, wp, lp, physical, modifiers)
		}
		return 0, m == wmKeyUp || rawTaken
	case wmSysChar:
		if s.menuRan {
			return 0, true
		}
		if s.keyTaken {
			// The content took Alt and the key, as a terminal does.
			return 0, true
		}
		// Alt and a letter go to the window, which opens the menu of the
		// letter: DefWindowProc would open the window's bar without it.
		if wp > ' ' {
			procSendMessageW.Call(s.w.hwnd, wmSysCommand, scKeyMenu, wp)
			return 0, true
		}
	case wmChar:
		if s.menuRan {
			return 0, true
		}
		c := uint16(wp)
		switch {
		case utf16.IsSurrogate(rune(c)) && c < 0xDC00:
			s.high = c
			return 0, true
		case utf16.IsSurrogate(rune(c)):
			r := utf16.DecodeRune(rune(s.high), rune(c))
			s.high = 0
			s.send(platform.SurfaceEvent{Kind: platform.TextInput, Text: string(r)})
			return 0, true
		case c < 0x20 || c == 0x7F:
			return 0, true // control characters come as keys
		}
		s.send(platform.SurfaceEvent{Kind: platform.TextInput, Text: string(rune(c))})
		return 0, true
	case wmImeSetContext:
		r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(m), wp, lp&^iscShowUICompWindow)
		return r, true
	case wmImeStartComp:
		s.placeIME()
		return 0, true
	case wmImeComposition:
		himc, _, _ := procImmGetContext.Call(hwnd)
		if himc == 0 {
			return 0, true
		}
		if lp&gcsResultStr != 0 {
			if text := imeString(himc, gcsResultStr); text != "" {
				s.composed(platform.SurfaceEvent{Kind: platform.TextInput, Text: text})
			}
		}
		if lp&gcsCompStr != 0 {
			text := imeString(himc, gcsCompStr)
			pos, _, _ := procImmGetCompositionStrW.Call(himc, gcsCursorPos, 0, 0)
			units := utf16.Encode([]rune(text))
			caret := len(utf16.Decode(units[:min(int(pos), len(units))]))
			s.composed(platform.SurfaceEvent{Kind: platform.TextComposition, Text: text, Caret: caret})
		}
		if lp&(gcsResultStr|gcsCompStr) == 0 && imeString(himc, gcsCompStr) == "" {
			// Microsoft's Japanese IME gives the composition up (Esc) with
			// neither, rather than with an empty GCS_COMPSTR, before
			// WM_IME_ENDCOMPOSITION, which would keep the marked text.
			if c := s.input.Client; c != nil {
				s.clientComposition.Cancel(c)
			} else {
				s.send(platform.SurfaceEvent{Kind: platform.TextComposition})
			}
		}
		procImmReleaseContext.Call(hwnd, himc)
		s.placeIME()
		return 0, true
	case wmImeEndComp:
		s.reconvert = nil
		if c := s.input.Client; c != nil {
			s.clientComposition.End(c)
			return 0, true
		}
		s.send(platform.SurfaceEvent{Kind: platform.TextComposition})
		return 0, true
	case wmImeChar:
		return 0, true
	case wmImeRequest:
		return s.imeRequest(wp, lp), true
	case wmGetObject:
		return s.getObject(wp, lp)
	case wmDestroy:
		s.CancelDataDrag()
		s.destroyAccess()
		s.revokeFileDrops()
		s.freeComp()
		s.SetBackground(platform.Color{})
		delete(s.w.b.surfaces, hwnd)
		return 0, true
	}
	return 0, false
}

// imeString reads a composition string of an input context.
func imeString(himc, which uintptr) string {
	n, _, _ := procImmGetCompositionStrW.Call(himc, which, 0, 0)
	if int32(n) <= 0 {
		return ""
	}
	buf := make([]uint16, n/2)
	procImmGetCompositionStrW.Call(himc, which, uintptr(unsafe.Pointer(&buf[0])), n)
	return string(utf16.Decode(buf))
}

func wheelLines() int {
	if !surfaceWheelLinesQueried {
		surfaceWheelLinesQueried = true
		var n uint32
		if r, _, _ := procSystemParametersInfoW.Call(spiGetWheelScrollLns, 0, uintptr(unsafe.Pointer(&n)), 0); r != 0 && n > 0 && n < 100 {
			surfaceWheelLines = int(n)
		} else {
			surfaceWheelLines = 3
		}
	}
	return surfaceWheelLines
}

// keyCode returns the key by position of a WM_KEYDOWN or WM_KEYUP, from its
// scancode: CodeUnknown for what an input method (VK_PROCESSKEY) or injected
// Unicode text (VK_PACKET) keeps from the application, and for a key the
// system made with no scancode.
func keyCode(vk, lp uintptr) platform.KeyCode {
	if vk == vkProcessKey || vk == vkPacket {
		return platform.CodeUnknown
	}
	return platform.KeyCodeForXT(uint8(lp>>16&0xFF), lp&(1<<24) != 0)
}

// rawKey sends a key shortcuts have no name for, which keyAndPosition leaves
// out, by its position, and reports whether the content took it. kind is
// RawKeyPressed or RawKeyReleased.
func (s *surface) rawKey(kind platform.SurfaceEventKind, wp, lp uintptr, physical platform.Key, modifiers platform.Modifiers) bool {
	code := keyCode(wp, lp)
	if code == platform.CodeUnknown {
		return false
	}
	return s.send(platform.SurfaceEvent{Kind: kind, PhysicalKey: physical, Code: code, Mods: modifiers, Repeat: kind == platform.RawKeyPressed && lp&(1<<30) != 0})
}

// keyAndPosition returns the key of a WM_KEYDOWN or WM_KEYUP by virtual
// key, and by position (the scancode in lp). ok is false for a key to leave
// out: one whose virtual key is unmapped, unless a shortcut modifier is
// held and its position is a US key. Known virtual keys keep their meaning
// (Russian letters already use VK_A through VK_Z). A release
// is sent whatever the modifiers held now, so that a press sent as a chord
// is let go of even after Ctrl was.
func keyAndPosition(vk, lp uintptr, release bool, modifiers platform.Modifiers) (k, physical platform.Key, ok bool) {
	k = vkKey(vk)
	// An input method (VK_PROCESSKEY) or injected Unicode text (VK_PACKET)
	// keeps the key from the application: the scancode is not a shortcut.
	if vk == vkProcessKey || vk == vkPacket {
		return k, platform.KeyUnknown, k != platform.KeyUnknown
	}
	// Bit 24 is the extended flag: the keypad's slash shares the code of /.
	if lp&(1<<24) == 0 {
		physical = platform.KeyForScancode(uint16(lp >> 16 & 0xFF))
	}
	if k == platform.KeyUnknown && !(release && physical != platform.KeyUnknown) && !platform.KeyByPosition(k, physical, modifiers) {
		return k, physical, false
	}
	return k, physical, true
}

// vkKey maps a virtual key code to the key it stands for.
func vkKey(vk uintptr) platform.Key {
	switch {
	case vk >= 'A' && vk <= 'Z':
		return platform.KeyA + platform.Key(vk-'A')
	case vk >= '0' && vk <= '9':
		return platform.Key0 + platform.Key(vk-'0')
	case vk >= 0x60 && vk <= 0x69: // the numeric keypad
		return platform.Key0 + platform.Key(vk-0x60)
	case vk >= 0x70 && vk <= 0x7B:
		return platform.KeyF1 + platform.Key(vk-0x70)
	}
	switch vk {
	case 0x0D:
		return platform.KeyEnter
	case 0x1B:
		return platform.KeyEscape
	case 0x08:
		return platform.KeyBackspace
	case 0x09:
		return platform.KeyTab
	case 0x20:
		return platform.KeySpace
	case 0x2E:
		return platform.KeyDelete
	case 0x2D:
		return platform.KeyInsert
	case 0x24:
		return platform.KeyHome
	case 0x23:
		return platform.KeyEnd
	case 0x21:
		return platform.KeyPageUp
	case 0x22:
		return platform.KeyPageDown
	case 0x25:
		return platform.KeyLeft
	case 0x26:
		return platform.KeyUp
	case 0x27:
		return platform.KeyRight
	case 0x28:
		return platform.KeyDown
	case 0xBD, 0x6D:
		return platform.KeyMinus
	case 0xBB:
		return platform.KeyEqual
	case 0xBC:
		return platform.KeyComma
	case 0xBE, 0x6E:
		return platform.KeyPeriod
	case 0xBF, 0x6F:
		return platform.KeySlash
	case 0xBA:
		return platform.KeySemicolon
	case 0xDE:
		return platform.KeyQuote
	case 0xDB:
		return platform.KeyBracketLeft
	case 0xDD:
		return platform.KeyBracketRight
	case 0xDC:
		return platform.KeyBackslash
	case 0xC0:
		return platform.KeyBackquote
	case 0x5D:
		return platform.KeyContextMenu
	case 0xA6: // VK_BROWSER_BACK
		return platform.KeyBack
	case 0xA7: // VK_BROWSER_FORWARD
		return platform.KeyForward
	}
	return platform.KeyUnknown
}

// Surface returns the surface of a window created with
// WindowOptions.Surface.
func (w *window) Surface() platform.Surface {
	if w.surface == nil {
		return nil
	}
	return w.surface
}

// menuShortcut runs the window's menu item whose shortcut the key down vk
// is, as macOS's menus take their key equivalents, and reports whether it
// ran one. The app's own items take their keys before the content, which
// gets only what they leave; items with a role (roles), only those the
// content leaves: their keys are the content's own too, as Ctrl+C, which
// copies in a field, interrupts in a terminal and is Edit › Copy's.
func (s *surface) menuShortcut(vk uintptr, roles bool) bool {
	w := s.w.top()
	cmd, ok := w.accels[accelKey{vk: uint32(vk), mods: currentModifiers()}]
	if !ok {
		return false
	}
	e := w.b.menus.entries[cmd]
	if e == nil || (e.role != "") != roles {
		return false
	}
	if roles {
		// The items of edit roles are grayed as the content was when their
		// menu last opened, not as it is now.
		w.enableEditItems(e.menu)
	}
	if st, _, _ := procGetMenuState.Call(e.menu, uintptr(cmd), mfByCommand); uint32(st) != 0xFFFFFFFF && st&mfGrayed != 0 {
		return false
	}
	s.keyTaken, s.menuRan = true, true
	w.b.menuCommand(cmd, w)
	return true
}
