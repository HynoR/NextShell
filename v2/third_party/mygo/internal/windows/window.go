//go:build windows && (amd64 || arm64)

package windows

import (
	"errors"
	"time"
	"unsafe"

	"github.com/egoist/mygo/internal/platform"
)

var errDestroyed = errors.New("mygo: window has been destroyed")

type window struct {
	b      *Backend
	h      platform.WindowHandler
	opts   *platform.WindowOptions
	hwnd   uintptr
	parent *window
	// flyout is how a flyout (WindowOptions.Flyout) was placed last, at
	// flyoutSize.
	flyout     platform.Flyout
	flyoutSize platform.Size

	// The WebView2 controller is created asynchronously; calls that need
	// it wait in pending until it exists, or fail with webViewErr, why it
	// could not be created.
	controller   uintptr // ICoreWebView2Controller
	webview      uintptr // ICoreWebView2
	settings     uintptr // ICoreWebView2Settings
	ready        bool
	pending      []pendingCall
	webViewErr   error
	webViewFails int // creations that failed
	// scriptIDs are the ids of the scripts added to every document, which
	// SetUserScripts removes; scriptGen counts the replacements, so an id
	// that arrives after one is removed at once.
	scriptIDs  []string
	scriptGen  int
	closed     bool
	destroying bool

	// contentFilter applies the content rules of SetContentRules to a
	// request (content_rules.go), and reports whether it answered it; nil
	// without any. filtersAll tells that WebView2 asks about every
	// request, and hidingScript hides what the rules hide in every
	// document. navigating is the page loading, the top-level URL of its
	// own request.
	contentFilter func(args, req uintptr, uri string, resource int32) bool
	filtersAll    bool
	hidingScript  string
	navigating    string
	// reloadAdopted reloads the first document of an adopted popup
	// (newWindowRequested).
	reloadAdopted bool

	minW, minH, maxW, maxH int // DIPs
	movable, closable      bool
	frameless              bool
	hiddenTitleBar         bool        // a TitleBarStyle that hides the caption
	caption                *captionBar // the controls in its place, nil if they failed
	buttonsTheme           string      // "light" or "dark" for the controls (SetWindowButtonsTheme), "" as the app
	topEdge                uintptr     // the window over the top resize border of one without a caption (topedge.go), or 0
	noRedirect             bool        // no redirection bitmap, for a material behind the page
	fullScreen             bool
	reframing              bool     // SetFullScreen is changing the frame
	showMaximized          bool     // on the first show (WindowOptions.Maximized)
	dropped                []string // DroppedFiles
	skipTaskbar            bool
	progress               struct {
		state string
		value float64
	}
	icons [2]uintptr // small and big, from SetIcon
	// placing is the rectangle, in pixels, that place is moving the window
	// to, already in the DPI of its display: WM_DPICHANGED keeps it.
	placing *rect
	// surface shows the content MyGo draws, in place of the webview.
	surface *surface
	// webViews are the web views under the surface (webview_embed.go), and
	// host the window whose surface a web view is under, which shows it
	// through its composition controller comp (comp3 its drag and drop,
	// on runtimes having it; uia the provider of its page for UI
	// Automation), in the visual webVis inside clipVis, where placement puts
	// it while shown: scaled by pageScale from pageAt, in the surface's
	// pixels, and cut to its corners by corners, a rectangle clip of
	// webVis. focused tells that it has the keyboard, and
	// cursor is the cursor its page wants. hidden tells that a host is
	// hidden, which hides its web views too: WebView2 shows their pages
	// in windows of its own, which hiding the host alone leaves on the
	// desktop.
	webViews         []*window
	host             *window
	comp, comp3, uia uintptr
	clipVis, webVis  uintptr
	placement        platform.WebViewPlacement
	pageAt           [2]float32
	pageScale        [2]float32
	corners          uintptr
	shown, focused   bool
	hidden           bool
	cursor           uintptr
	saved            struct {
		style, exStyle uintptr
		placement      windowPlacement
	}
	state       int // last sizeRestored, sizeMinimized or sizeMaximized
	opacity     float64
	bgBrush     uintptr
	bg          *platform.Color
	vibrancy    string
	glass       margins // the frame's extension under the material
	shadow      bool
	ignoreMouse bool

	menu    *platform.Menu // menu bar
	hmenu   uintptr
	owner   int
	ownMenu bool
	accels  map[accelKey]uint16
	// autoHideMenu keeps hmenu off the window except while revealed, when
	// the keyboard is in the menu bar.
	autoHideMenu, revealed bool
	inMenu                 bool    // in a menu loop, of the bar or a popup
	menuKey                menuKey // the letter that opened popupMenuBar

	programmatic bool
	loading      bool
	zoom         float64
	muted        bool      // SetAudioMuted, which setUp applies to a web view made later
	mutePut      time.Time // when the IsMuted put in flight was made, zero for none
	devTools     bool
	htmlFor      map[string]string // LoadHTML documents by the URL they load at
	htmlNext     bool              // LoadHTML started the next navigation
	calls        map[int]func(string, error)
	nextCall     int

	// flyoutLeft tells that the activation left the window's focusable
	// flyout for another app's window, which draws the window inactive
	// though the flyout still shows (keepsActive).
	flyoutLeft bool
}

func (b *Backend) NewWindow(o *platform.WindowOptions, h platform.WindowHandler) (platform.Window, error) {
	if !o.Surface {
		if err := b.startEnvironment(o); err != nil {
			return nil, err
		}
	}
	w := &window{
		b: b, h: h, opts: o,
		movable: o.Movable, closable: o.Closable, frameless: o.Frameless,
		opacity: 1, zoom: 1, shadow: o.HasShadow, autoHideMenu: o.AutoHideMenu,
		minW: o.MinSize.Width, minH: o.MinSize.Height, maxW: o.MaxSize.Width, maxH: o.MaxSize.Height,
		htmlFor: map[string]string{}, calls: map[int]func(string, error){},
	}
	w.hiddenTitleBar = !o.Frameless && (o.TitleBarStyle == "hidden" || o.TitleBarStyle == "hiddenInset")
	// A page or native UI with a material behind it needs a window without
	// a redirection bitmap, whose opaque surface would cover the system
	// backdrop. GDI draws nothing there: the controls of a hidden title bar
	// and the frames of native UI show through DirectComposition
	// (compositor.go, and the renderer's swap chain), and the menus open in
	// a popup (barless). Where DirectComposition is unavailable, the window
	// keeps its bitmap: what it shows shows, and the material does not. The
	// style is the window's for life: Windows neither adds it nor removes it
	// later.
	w.noRedirect = o.Vibrancy != "" && systemBackdrops() && (!w.hiddenTitleBar && !o.Surface || b.composition() != nil)
	if o.Flyout != nil || o.Transparent && o.Surface {
		// What the content leaves transparent shows what is behind: native
		// UI's frames show their alpha through DirectComposition, which an
		// opaque redirection bitmap would cover.
		w.noRedirect = b.composition() != nil
	}
	var owner uintptr
	if p, ok := o.Parent.(*window); ok && p != nil && !p.closed {
		w.parent, owner = p, p.hwnd
	} else if o.Panel {
		// A tool window alone may still get a taskbar button, on the
		// taskbars of other displays: an owned one never does. The
		// application window is hidden for good.
		owner = b.appHwnd
	}
	style, ex := w.styles()
	class := windowClass
	if o.Flyout != nil {
		style, ex = w.flyoutStyles()
		if o.HasShadow {
			class = flyoutShadowClass
		}
	}
	if p := w.parent; p != nil && p.opts.Flyout != nil && p.IsAlwaysOnTop() {
		// Above a tray's flyout, which is above other apps' windows.
		ex |= wsExTopmost
	}
	initial := initialWindowBounds(o, screenSpace())
	w.hwnd = createWindow(ex, class, o.Title, style, initial.Left, initial.Top, initial.Right-initial.Left, initial.Bottom-initial.Top, owner)
	if w.hwnd == 0 {
		return nil, errors.New("mygo: cannot create a window")
	}
	b.windows[w.hwnd] = w
	if o.Panel || o.Frameless && o.CornerRadius > 0 {
		// Windows 11 gives tool windows the small corners of menus, and
		// frameless windows none: rounded, they take its own corners,
		// whatever the radius.
		w.roundFlyout()
	}
	if o.Flyout != nil {
		if o.HasShadow {
			w.roundFlyout()
		}
		w.PlaceFlyout(*o.Flyout, platform.Size{Width: o.Width, Height: o.Height})
	} else {
		w.placeInitially()
	}

	if !o.Closable {
		w.SetClosable(false)
	}
	if o.AlwaysOnTop {
		w.SetAlwaysOnTop(true)
	}
	if o.Opacity > 0 && o.Opacity < 1 {
		w.SetOpacity(o.Opacity)
	}
	if o.BackgroundColor != nil {
		w.SetBackgroundColor(*o.BackgroundColor)
	}
	if w.captionless() && w.shadow && o.Flyout == nil {
		w.SetHasShadow(true)
	}
	if w.captionless() {
		w.plainCaption()
	}
	if w.hiddenTitleBar {
		w.caption = newCaptionBar(w)
	}
	w.newTopEdge()
	if o.Surface {
		if w.surface = newSurface(w); w.surface == nil {
			procDestroyWindow.Call(w.hwnd)
			return nil, errors.New("mygo: cannot create the window's surface")
		}
	}
	b.applyWindowTheme(w)
	if o.Vibrancy != "" {
		w.SetVibrancy(o.Vibrancy)
	}
	if b.icon != 0 {
		w.setIcon(b.icon)
	}
	if b.appMenu != nil && w.hasAppMenu() {
		w.installMenu(b.appMenu)
	}
	if o.Modal && w.parent != nil {
		procEnableWindow.Call(w.parent.hwnd, 0)
	}
	if o.FullScreen {
		w.SetFullScreen(true)
	} else if o.Maximized {
		w.showMaximized = true
	}
	w.skipTaskbar = o.SkipTaskbar // applied once the taskbar button exists
	w.layoutControls()
	if !o.Surface {
		b.whenEnvironment(w.createWebView)
	}
	return w, nil
}

// initialWindowBounds creates a window on its target display, a restored
// one at its pixels and one with a position at its rectangle in s, so that
// moving it there does not send WM_DPICHANGED, whose rectangle would scale
// the size already in that display's scale a second time. A centered
// window is created on the primary display, at 0, 0, where it stays.
func initialWindowBounds(o *platform.WindowOptions, s dipSpace) rect {
	if o.Flyout != nil {
		return rect{}
	}
	r := o.StateBounds
	if r == nil {
		if o.Center {
			return rect{}
		}
		p, _ := s.rectToPixels(platform.Rect{X: o.X, Y: o.Y, Width: o.Width, Height: o.Height})
		r = &p
	}
	return rect{int32(r.X), int32(r.Y), int32(r.X + r.Width), int32(r.Y + r.Height)}
}

func (w *window) styles() (style, ex uint32) {
	o := w.opts
	style = wsOverlappedWindow | wsClipChildren
	if !o.Resizable {
		style &^= wsThickFrame | wsMaximizeBox
	}
	if !o.Minimizable {
		style &^= wsMinimizeBox
	}
	if !o.Maximizable {
		style &^= wsMaximizeBox
	}
	if !o.Focusable {
		ex |= wsExNoActivate
	}
	if o.Panel {
		// Out of the taskbar and Alt+Tab, on whichever virtual desktop
		// is current as it shows, and above other apps' windows. Without
		// a window menu: DWM draws the close button of a tool window's
		// menu where a material extends the frame, over the content.
		ex |= wsExToolWindow | wsExTopmost
		style &^= wsSysMenu
	}
	if w.noRedirect {
		ex |= wsExNoRedirect
	}
	return style, ex
}

// placeInitially sizes and positions a new window, in the DPI of the
// monitor it appears on: the primary one for a centered window, and for
// one with a position, the one its rectangle is on in the screen's DIPs
// (dipSpace).
//
// It has Windows compute the frame again (SWP_FRAMECHANGED): during
// CreateWindowEx the window is not in Backend.windows yet, so
// WM_NCCALCSIZE gives it the frame of DefWindowProc, a whole title bar
// on one without a caption (frameCalcSize). A window created at its
// rectangle (initialWindowBounds) keeps its size here, and a move alone
// sends no WM_NCCALCSIZE.
func (w *window) placeInitially() {
	o := w.opts
	if r := o.StateBounds; r != nil {
		w.place(rect{int32(r.X), int32(r.Y), int32(r.X + r.Width), int32(r.Y + r.Height)}, swpFrameChanged)
		return
	}
	var p platform.Rect // the window's, or its content's, in pixels
	var dpi int
	var work rect
	if o.Center {
		mon, _, _ := procMonitorFromPoint.Call(0, monitorDefaultToPrimary)
		dpi, work = monitorDPI(mon), monitorWorkArea(mon)
		p = platform.Rect{Width: int(toPx(o.Width, dpi)), Height: int(toPx(o.Height, dpi))}
	} else {
		s := screenSpace()
		var i int
		p, i = s.rectToPixels(platform.Rect{X: o.X, Y: o.Y, Width: o.Width, Height: o.Height})
		dpi = s.dpiOf(i)
	}
	width, height := int32(p.Width), int32(p.Height)
	if o.UseContentSize {
		width, height = w.outerSize(width, height, dpi)
	}
	x, y := int32(p.X), int32(p.Y)
	if o.Center {
		x = work.Left + (work.Right-work.Left-width)/2
		y = work.Top + (work.Bottom-work.Top-height)/2
	}
	w.place(rect{x, y, x + width, y + height}, swpFrameChanged)
	if o.UseContentSize && !w.captionless() {
		w.fitContent(width, height, int32(p.Width), int32(p.Height), false, 0, 0)
	}
}

// place moves the window to r, in pixels, computed in the DPI of the
// display r is on, with SetWindowPos's flags besides: where that DPI is
// not the window's, WM_DPICHANGED keeps r, rather than the rectangle it
// suggests, which scales r's size by the change of DPI a second time.
func (w *window) place(r rect, flags uintptr) {
	prev := w.placing
	w.placing = &r
	procSetWindowPos.Call(w.hwnd, 0, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right-r.Left), uintptr(r.Bottom-r.Top), swpNoZOrder|swpNoActivate|flags)
	w.placing = prev
}

// fitContent grows the window, outerW by outerH, whose content was to be
// wantW by wantH pixels, by what its content lacks, and with at moves the
// content to (atX, atY) on the screen: AdjustWindowRectEx counts a menu
// bar of one line as the system draws it, and MyGo draws Windows 11's
// taller (measureMenuBarItem), as a bar too wide for its window wraps.
func (w *window) fitContent(outerW, outerH, wantW, wantH int32, at bool, atX, atY int32) {
	var c, wr rect
	procGetClientRect.Call(w.hwnd, uintptr(unsafe.Pointer(&c)))
	procGetWindowRect.Call(w.hwnd, uintptr(unsafe.Pointer(&wr)))
	var origin point
	procClientToScreen.Call(w.hwnd, uintptr(unsafe.Pointer(&origin)))
	dw, dh := wantW-c.Right, wantH-c.Bottom
	dx, dy := int32(0), int32(0)
	if at {
		dx, dy = atX-origin.X, atY-origin.Y
	}
	if dw == 0 && dh == 0 && dx == 0 && dy == 0 {
		return
	}
	w.place(rect{wr.Left + dx, wr.Top + dy, wr.Left + dx + outerW + dw, wr.Top + dy + outerH + dh}, 0)
}

// outerSize returns the window size for a content size, in pixels.
func (w *window) outerSize(width, height int32, dpi int) (int32, int32) {
	r := rect{0, 0, width, height}
	menu := uintptr(0)
	if !w.autoHideMenu && !w.noRedirect && (w.hmenu != 0 || w.b.appMenu != nil && w.hasAppMenu()) {
		menu = 1
	}
	w.addFrame(&r, menu, dpi)
	return r.Right - r.Left, r.Bottom - r.Top
}

// addFrame grows r, a page area in pixels, to the window around it. A window
// without a caption has no title bar or menu bar, and keeps what
// frameCalcSize leaves it: the borders of a resizable one, less what the
// client area takes of them (frameBorder), and the pixel at the top of a
// hidden title bar on Windows 11.
func (w *window) addFrame(r *rect, menu uintptr, dpi int) {
	style, ex := windowLong(w.hwnd, gwlStyle), windowLong(w.hwnd, gwlExStyle)
	captionless := w.captionless()
	if captionless && style&wsThickFrame == 0 || !has(procAdjustWindowRectExForDpi) {
		return
	}
	top := r.Top
	if captionless {
		menu = 0
	}
	procAdjustWindowRectExForDpi.Call(uintptr(unsafe.Pointer(r)), style, menu, ex, uintptr(dpi))
	if captionless {
		r.Top = top
		if w.hiddenTitleBar && windows11() {
			r.Top--
		}
		b := w.frameBorder()
		r.Left, r.Right, r.Bottom = r.Left+b, r.Right-b, r.Bottom-b
	}
}

// monitorAt returns the monitor at x, y in pixels, or the nearest.
func monitorAt(x, y int) uintptr {
	pt := uintptr(uint32(int32(x))) | uintptr(uint32(int32(y)))<<32
	m, _, _ := procMonitorFromPoint.Call(pt, monitorDefaultToNearest)
	return m
}

func monitorDPI(mon uintptr) int {
	if mon != 0 && has(procGetDpiForMonitor) {
		var dx, dy uint32
		if r, _, _ := procGetDpiForMonitor.Call(mon, 0, uintptr(unsafe.Pointer(&dx)), uintptr(unsafe.Pointer(&dy))); r == 0 && dx != 0 {
			return int(dx)
		}
	}
	return dpiOf(0)
}

func monitorInfo(mon uintptr) monitorInfoEx {
	var mi monitorInfoEx
	mi.Size = uint32(unsafe.Sizeof(mi))
	procGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi)))
	return mi
}

func monitorWorkArea(mon uintptr) rect { return monitorInfo(mon).Work }

func (w *window) monitor() uintptr {
	m, _, _ := procMonitorFromWindow.Call(w.hwnd, monitorDefaultToNearest)
	return m
}

// message handles a window message; ok false lets DefWindowProc run.
func (w *window) message(m uint32, wp, lp uintptr) (uintptr, bool) {
	if m == w.b.taskbarButtonCreated && m != 0 {
		w.applyTaskbar()
		return 0, true
	}
	if w.opts.Flyout != nil {
		if r, ok := w.flyoutMessage(m, wp, lp); ok {
			return r, true
		}
	}
	if m == wmMouseActivate && w.ownsFocusableFlyout() {
		return maActivateAndEat, true
	}
	if m == wmNCActivate && wp == 0 && w.keepsActive() {
		// Its flyout takes the activation: the frame and the material
		// stay active, as the window's own menus and popovers keep them.
		if r, ok := w.menuBarMessage(m, 1, lp); ok {
			return r, true
		}
		r, _, _ := procDefWindowProcW.Call(w.hwnd, uintptr(m), 1, lp)
		return r, true
	}
	if m == wmInitMenuPopup {
		w.enableEditItems(wp)
	}
	if r, ok := w.b.menuItemMessage(w.hwnd, m, wp, lp); ok {
		return r, true
	}
	if r, ok := w.menuBarMessage(m, wp, lp); ok {
		return r, true
	}
	switch m {
	case wmClose:
		if w.h.ShouldClose() {
			w.destroy()
		}
		return 0, true
	case wmDestroy:
		w.cleanup()
		w.h.Closed()
		return 0, true
	case wmTimer:
		if wp != timerWebView {
			return 0, false
		}
		procKillTimer.Call(w.hwnd, timerWebView)
		w.createWebView()
		return 0, true
	case wmSize:
		if !w.reframing { // SetFullScreen lays the window out once it is done
			w.sized(wp)
		}
		return 0, true
	case wmShowWindow:
		w.hidden = wp == 0
		for _, v := range w.webViews {
			v.placeEmbedded()
		}
		return 0, false
	case wmMove:
		if w.controller != 0 {
			comCall(w.controller, ctlNotifyParentWindowPositionChanged)
		}
		for _, v := range w.webViews {
			if v.controller != 0 {
				comCall(v.controller, ctlNotifyParentWindowPositionChanged)
			}
		}
		w.h.Moved()
		return 0, true
	case wmActivate:
		if loword(wp) != waInactive {
			w.flyoutLeft = false
		}
		if w.caption != nil {
			w.caption.activate(loword(wp) != waInactive || w.keepsActive())
		}
		if loword(wp) == waInactive {
			w.h.Blurred()
		} else {
			w.h.Focused()
		}
		return 0, false
	case wmSetFocus:
		if w.surface != nil {
			procSetFocus.Call(w.surface.hwnd)
		} else {
			w.focusWebView()
		}
		return 0, true
	case wmGetMinMaxInfo:
		info := (*minMaxInfo)(native(lp))
		dpi := dpiOf(w.hwnd)
		if w.minW > 0 || w.minH > 0 {
			info.MinTrackSize = point{toPx(w.minW, dpi), toPx(w.minH, dpi)}
		}
		if w.maxW > 0 {
			info.MaxTrackSize.X = toPx(w.maxW, dpi)
		}
		if w.maxH > 0 {
			info.MaxTrackSize.Y = toPx(w.maxH, dpi)
		}
		return 0, true
	case wmDpiChanged:
		r := (*rect)(native(lp))
		if w.placing != nil {
			r = w.placing
		}
		procSetWindowPos.Call(w.hwnd, 0, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right-r.Left), uintptr(r.Bottom-r.Top), swpNoZOrder|swpNoActivate)
		return 0, true
	case wmNCCalcSize:
		if w.captionless() && wp != 0 && !w.fullScreen {
			return w.frameCalcSize(wp, lp), true
		}
	case wmEraseBkgnd:
		brush := w.bgBrush
		if w.surface != nil && w.surface.bgBrush != 0 {
			// Native UI's own background, around its surface (which the
			// window clips out) while the surface does not fill the window
			// yet: see fillUncovered.
			brush = w.surface.bgBrush
		}
		if brush != 0 && w.vibrancy == "" {
			var r rect
			procGetClientRect.Call(w.hwnd, uintptr(unsafe.Pointer(&r)))
			procFillRect.Call(wp, uintptr(unsafe.Pointer(&r)), brush)
		}
		return 1, true
	case wmWindowPosChanging:
		pos := (*windowPos)(native(lp))
		if pos.Flags&swpShowWindow != 0 && w.surface != nil {
			w.surface.showing()
		}
		w.maximizeInWorkArea(pos)
		if !w.movable {
			pos.Flags |= swpNoMove
		}
		return 0, false
	case wmCommand:
		if hiword(wp) == 0 && lp == 0 { // a menu item
			w.b.menuCommand(loword(wp), w)
			return 0, true
		}
	case wmEnterMenuLoop:
		w.inMenu = true
		if w.surface != nil {
			w.surface.liftKeys()
		}
	case wmExitMenuLoop:
		w.inMenu = false
	case wmMenuChar:
		if r, ok := w.menuChar(wp, lp); ok {
			return r, true
		}
	case wmSysCommand:
		// Alt or F10 alone (lp 0) take the keyboard to the menu bar, and Alt
		// and a letter (lp the letter) open the menu of the letter; Alt and
		// Space open the window menu. WebView2 passes no Alt+letter on: the
		// letters come from native UI only.
		if wp&0xFFF0 == scKeyMenu && lp != ' ' && w.hmenu != 0 {
			if w.barless() {
				w.popupMenuBar(lp)
				return 0, true
			}
			if lp == 0 && w.autoHideMenu && !w.revealed {
				return w.revealMenu(wp, lp), true
			}
		}
	}
	return 0, false
}

// sized lays the window out after it changed size or state (a WM_SIZE kind),
// and tells the app.
func (w *window) sized(kind uintptr) {
	w.resizeWebView()
	if w.surface != nil {
		// What the window grew by shows the content's background, not
		// black, until the surface presents the frame of its new size,
		// which fitting it draws at once. The window's own pixels under
		// the surface take it too, which show where the window next
		// shrinks, until DWM takes its new size (fillUnder).
		w.surface.fillUncovered()
		w.surface.fit()
		w.surface.fillUnder()
	}
	if kind != sizeMinimized {
		w.layoutControls()
	}
	if w.vibrancy != "" && kind != sizeMinimized {
		w.extendGlass(false)
	}
	was := w.state
	switch kind {
	case sizeMinimized:
		if w.state != sizeMinimized {
			w.state = sizeMinimized
			w.h.Minimized()
		}
	case sizeMaximized:
		w.fitWorkArea()
		if w.state != sizeMaximized {
			if w.state == sizeMinimized {
				w.h.Restored()
			}
			w.state = sizeMaximized
			w.h.Maximized()
		}
	case sizeRestored:
		switch w.state {
		case sizeMinimized:
			w.h.Restored()
		case sizeMaximized:
			w.h.Unmaximized()
		}
		w.state = sizeRestored
	}
	// Nothing of a minimized window shows (Surface.Occluded).
	if s := w.surface; s != nil && (was == sizeMinimized) != (w.state == sizeMinimized) {
		ev := platform.SurfaceEvent{Kind: platform.SurfaceShown}
		if w.state == sizeMinimized {
			ev.Kind = platform.SurfaceHidden
		}
		s.send(ev)
	}
	w.h.Resized()
}

// maximizeInWorkArea keeps a maximized window in the work area of its
// monitor, off the taskbar, where Windows would have it cover the monitor.
// Windows maximizes a top-level window into the work area only when it has
// a maximize box (WS_MAXIMIZEBOX), and over the whole monitor otherwise
// (Wine's WINPOS_GetMinMaxInfo spells the rule out): a window the user
// cannot maximize (Maximizable false, and so every panel) that the app
// maximizes anyway would cover the taskbar, and, topmost as panels are,
// hide it. The rectangle is fixed as it is about to apply, in screen
// coordinates, rather than in WM_GETMINMAXINFO, whose are given for the
// primary monitor and adjusted by Windows for the others. Full screen
// covers the monitor on purpose.
func (w *window) maximizeInWorkArea(pos *windowPos) {
	if w.fullScreen || pos.Flags&(swpNoMove|swpNoSize) != 0 || w.hasStyle(wsMaximizeBox) {
		return
	}
	if zoomed, _, _ := procIsZoomed.Call(w.hwnd); zoomed == 0 {
		return
	}
	r := rect{pos.X, pos.Y, pos.X + pos.CX, pos.Y + pos.CY}
	mon, _, _ := procMonitorFromRect.Call(uintptr(unsafe.Pointer(&r)), monitorDefaultToNull)
	if mon == 0 {
		return
	}
	mi := monitorInfo(mon)
	if fit, ok := workAreaMaximized(r, mi.Monitor, mi.Work); ok {
		pos.X, pos.Y, pos.CX, pos.CY = fit.Left, fit.Top, fit.Right-fit.Left, fit.Bottom-fit.Top
	}
}

// fitWorkArea moves a maximized window without a maximize box into the
// work area once it is maximized, where maximizeInWorkArea did not as it
// was about to be: a window not yet zoomed as the move applied, or a
// rectangle that did not cover the monitor. It overhangs the work area by
// its invisible borders, the window's rectangle beyond what DWM shows.
func (w *window) fitWorkArea() {
	if w.fullScreen || w.hasStyle(wsMaximizeBox) {
		return
	}
	var r, shown rect
	procGetWindowRect.Call(w.hwnd, uintptr(unsafe.Pointer(&r)))
	if procDwmGetWindowAttribute.Call(w.hwnd, dwmwaExtendedFrameBounds, uintptr(unsafe.Pointer(&shown)), unsafe.Sizeof(shown)); shown.Right <= shown.Left {
		shown = r
	}
	mon, _, _ := procMonitorFromWindow.Call(w.hwnd, monitorDefaultToNearest)
	if mon == 0 {
		return
	}
	mi := monitorInfo(mon)
	if mi.Work == mi.Monitor {
		return
	}
	fit := rect{
		Left:   mi.Work.Left - (shown.Left - r.Left),
		Top:    mi.Work.Top - (shown.Top - r.Top),
		Right:  mi.Work.Right + (r.Right - shown.Right),
		Bottom: mi.Work.Bottom + (r.Bottom - shown.Bottom),
	}
	if fit != r {
		procSetWindowPos.Call(w.hwnd, 0, uintptr(fit.Left), uintptr(fit.Top), uintptr(fit.Right-fit.Left), uintptr(fit.Bottom-fit.Top), swpNoZOrder|swpNoActivate)
	}
}

// workAreaMaximized returns the rectangle of a window maximized in the
// work area of a monitor, for one, r, maximized over the whole monitor:
// it overhangs the work area as r overhangs the monitor, by the invisible
// borders that a maximized window has outside the screen. ok is false
// when r does not cover the monitor, or the work area is the whole
// monitor (no taskbar, or one that hides).
func workAreaMaximized(r, monitor, work rect) (fit rect, ok bool) {
	if work == monitor || r.Left > monitor.Left || r.Top > monitor.Top || r.Right < monitor.Right || r.Bottom < monitor.Bottom {
		return r, false
	}
	return rect{
		Left:   work.Left - (monitor.Left - r.Left),
		Top:    work.Top - (monitor.Top - r.Top),
		Right:  work.Right + (r.Right - monitor.Right),
		Bottom: work.Bottom + (r.Bottom - monitor.Bottom),
	}, true
}

// layoutControls lays out what sits above the webview or the surface: the
// top edge, and above it the window controls of a hidden title bar, among
// them a maximize button the content draws (SetMaximizeButton), which
// keeps the snap layouts over its whole height.
func (w *window) layoutControls() {
	w.layoutTopEdge()
	if w.caption != nil {
		w.caption.layout()
	}
}

// captionless reports a window without a caption: frameless, or with a
// hidden title bar.
func (w *window) captionless() bool { return w.frameless || w.hiddenTitleBar }

// plainCaption keeps the accent color off the top of a window without a
// caption. With "Show accent color on title bars and window borders" on,
// Windows 11 paints the caption in the accent, and what is left of it, the
// pixel frameCalcSize keeps and the frame SetHasShadow extends, shows as a
// strip of the accent over the page. Windows 10 knows no caption color.
func (w *window) plainCaption() {
	if !windows11() {
		return
	}
	none := uint32(dwmColorNone)
	procDwmSetWindowAttribute.Call(w.hwnd, dwmwaCaptionColor, uintptr(unsafe.Pointer(&none)), 4)
}

// frameCalcSize removes the title bar of windows without a caption.
// Resizable ones keep their left, right and bottom borders, which Windows
// 10 and later draw invisible outside the window, so they still resize.
// Maximized windows would overflow the screen by their borders: fit the
// work area.
//
// A hidden title bar on Windows 11 keeps a pixel of it, which the window's
// border covers: without any non-client area at the top, Windows 11 opens no
// snap layouts over the maximize button of a window that is not maximized.
// Windows 10 has no snap layouts, and over a window that keeps any of its top
// it draws its whole title bar, above the page and the controls.
//
// A frameless window on Windows 10 takes the visible pixel of its borders
// too (frameBorder), and keeps only the invisible ones.
func (w *window) frameCalcSize(wp, lp uintptr) uintptr {
	params := (*ncCalcSizeParams)(native(lp))
	if w.IsMaximized() {
		// The monitor of the proposed rectangle, not of the window: while a
		// window is restored from minimized it is still the minimized one,
		// parked off screen, whose nearest monitor is not the one it comes
		// back to. Fitting the work area of the wrong monitor leaves the
		// client area outside the window, and the content shows nothing.
		// A rectangle on no monitor keeps the whole window as its client
		// area, as winit and tao do.
		if mon, _, _ := procMonitorFromRect.Call(uintptr(unsafe.Pointer(&params.Rgrc[0])), monitorDefaultToNull); mon != 0 {
			params.Rgrc[0] = monitorWorkArea(mon)
		}
		return 0
	}
	if windowLong(w.hwnd, gwlStyle)&wsThickFrame != 0 {
		top := params.Rgrc[0].Top
		procDefWindowProcW.Call(w.hwnd, wmNCCalcSize, wp, lp)
		params.Rgrc[0].Top = top
		if w.hiddenTitleBar && windows11() {
			params.Rgrc[0].Top++
		}
		if b := w.frameBorder(); b > 0 {
			params.Rgrc[0].Left -= b
			params.Rgrc[0].Right += b
			params.Rgrc[0].Bottom += b
		}
	}
	return 0
}

// frameBorder returns how many pixels of the left, right and bottom
// borders that DefWindowProc keeps a resizable frameless window are its
// client area's on Windows 10: the innermost, which DWM draws as the
// visible line around windows, inside the invisible ones that resize them.
// Windows 10 draws that line in the non-client area only, white or of the
// accent color, and it showed as a white frame around the content,
// whatever its color (#916). Over the client area, the content covers it:
// the window shows the content to its edges, and DWM's shadow around it.
// The line is a pixel at any scale. Windows 11 draws a border of its own
// color around the rounded corners, which is left as it is, and windows
// with a hidden title bar keep the line of other apps' windows.
func (w *window) frameBorder() int32 {
	if !w.frameless || w.opts.Flyout != nil || windows11() || !w.hasStyle(wsThickFrame) {
		return 0
	}
	return 1
}

func (w *window) destroy() {
	if w.closed || w.destroying {
		return
	}
	w.destroying = true
	// Re-enable the owner first, or Windows activates another app.
	if w.opts.Modal && w.parent != nil && !w.parent.closed {
		procEnableWindow.Call(w.parent.hwnd, 1)
		// And give it the activation while w has it, as EndDialog does:
		// Windows otherwise activates the window under w, another app's
		// once the user switched to one and back (#1055).
		if fg, _, _ := procGetForegroundWindow.Call(); fg == w.hwnd && w.parent.IsVisible() && !w.parent.IsMinimized() {
			w.parent.Focus()
		}
	}
	procDestroyWindow.Call(w.hwnd)
}

func (w *window) cleanup() {
	if w.closed {
		return
	}
	w.closeWebViews()
	w.closed = true
	if w.opts.Modal && w.parent != nil && !w.parent.closed {
		procEnableWindow.Call(w.parent.hwnd, 1)
	}
	for id, cb := range w.calls {
		delete(w.calls, id)
		cb("", errDestroyed)
	}
	w.failPending(errDestroyed)
	if w.caption != nil {
		w.caption.forget()
	}
	if w.topEdge != 0 {
		delete(w.b.topEdges, w.topEdge)
	}
	if w.controller != 0 {
		comCall(w.controller, ctlClose)
		release(w.settings)
		release(w.webview)
		release(w.controller)
		w.controller, w.webview, w.settings = 0, 0, 0
	}
	w.b.menus.drop(w.owner)
	// Windows destroys the menu bar on the window, not one hidden off it.
	if w.hmenu != 0 && !w.menuShown() {
		procDestroyMenu.Call(w.hmenu)
		w.hmenu = 0
	}
	if w.bgBrush != 0 {
		procDeleteObject.Call(w.bgBrush)
		w.bgBrush = 0
	}
	w.freeIcons()
	delete(w.b.windows, w.hwnd)
}

func (w *window) Handle() uintptr        { return w.hwnd }
func (w *window) WebViewHandle() uintptr { return w.webview }

func (w *window) SetTitle(title string) {
	procSetWindowTextW.Call(w.hwnd, uintptr(unsafe.Pointer(u16(title))))
}

func (w *window) Title() string {
	n, _, _ := procGetWindowTextLengthW.Call(w.hwnd)
	buf := make([]uint16, n+1)
	procGetWindowTextW.Call(w.hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return string(utf16Decode(buf))
}

// The core marks the title of an edited document; Windows shows no file.
func (w *window) SetRepresentedFile(string) {}
func (w *window) SetDocumentEdited(bool)    {}

func utf16Decode(b []uint16) []rune {
	for i, c := range b {
		if c == 0 {
			b = b[:i]
			break
		}
	}
	out := make([]rune, 0, len(b))
	for i := 0; i < len(b); i++ {
		c := rune(b[i])
		if c >= 0xD800 && c < 0xDC00 && i+1 < len(b) {
			c = (c-0xD800)<<10 + (rune(b[i+1]) - 0xDC00) + 0x10000
			i++
		}
		out = append(out, c)
	}
	return out
}

// SetBounds moves the window to r, in the screen's DIPs (dipSpace), at
// the scale of the display r is on.
func (w *window) SetBounds(r platform.Rect) {
	p, _ := screenSpace().rectToPixels(r)
	w.place(rect{int32(p.X), int32(p.Y), int32(p.X + p.Width), int32(p.Y + p.Height)}, 0)
}

// Bounds returns the window's rectangle in the screen's DIPs, at the
// scale of the display it is the most on, whose DPI it has.
func (w *window) Bounds() platform.Rect {
	var r rect
	procGetWindowRect.Call(w.hwnd, uintptr(unsafe.Pointer(&r)))
	return screenSpace().rectToDIP(pixelRect(r))
}

// SetContentBounds moves the window so that its client area is r, in the
// screen's DIPs, at the scale of the display r is on.
func (w *window) SetContentBounds(r platform.Rect) {
	s := screenSpace()
	p, i := s.rectToPixels(r)
	dpi := s.dpiOf(i)
	outer := rect{int32(p.X), int32(p.Y), int32(p.X + p.Width), int32(p.Y + p.Height)}
	menu := uintptr(0)
	if w.menuShown() {
		menu = 1
	}
	w.addFrame(&outer, menu, dpi)
	w.place(outer, 0)
	if !w.captionless() {
		w.fitContent(outer.Right-outer.Left, outer.Bottom-outer.Top, int32(p.Width), int32(p.Height), true, int32(p.X), int32(p.Y))
	}
}

// ContentBounds returns the client area in the screen's DIPs, at the
// scale of the display the window is the most on, as Bounds.
func (w *window) ContentBounds() platform.Rect {
	var wr, r rect
	procGetWindowRect.Call(w.hwnd, uintptr(unsafe.Pointer(&wr)))
	procGetClientRect.Call(w.hwnd, uintptr(unsafe.Pointer(&r)))
	var origin point
	procClientToScreen.Call(w.hwnd, uintptr(unsafe.Pointer(&origin)))
	s := screenSpace()
	return s.toDIPOn(s.pixelDisplay(pixelRect(wr)), platform.Rect{X: int(origin.X), Y: int(origin.Y), Width: int(r.Right), Height: int(r.Bottom)})
}

func (w *window) SetMinimumSize(s platform.Size) { w.minW, w.minH = s.Width, s.Height }
func (w *window) SetMaximumSize(s platform.Size) { w.maxW, w.maxH = s.Width, s.Height }

func (w *window) setStyle(bits uint32, on bool) {
	style := windowLong(w.hwnd, gwlStyle)
	if on {
		style |= uintptr(bits)
	} else {
		style &^= uintptr(bits)
	}
	setWindowLong(w.hwnd, gwlStyle, style)
	procSetWindowPos.Call(w.hwnd, 0, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoZOrder|swpNoActivate|swpFrameChanged)
}

func (w *window) setExStyle(bits uint32, on bool) {
	ex := windowLong(w.hwnd, gwlExStyle)
	if on {
		ex |= uintptr(bits)
	} else {
		ex &^= uintptr(bits)
	}
	setWindowLong(w.hwnd, gwlExStyle, ex)
}

func (w *window) hasStyle(bits uint32) bool { return windowLong(w.hwnd, gwlStyle)&uintptr(bits) != 0 }

func (w *window) SetResizable(v bool) {
	bits := uint32(wsThickFrame)
	if w.opts.Maximizable {
		bits |= wsMaximizeBox
	}
	w.setStyle(bits, v)
	if w.captionless() && w.shadow && w.opts.Flyout == nil {
		// Without borders, the window needs its frame extended for its
		// shadow, which with borders it may not have (frameBorder).
		w.SetHasShadow(true)
	}
	w.captionChanged()
}

func (w *window) IsResizable() bool { return w.hasStyle(wsThickFrame) }
func (w *window) SetMovable(v bool) { w.movable = v }
func (w *window) IsMovable() bool   { return w.movable }

func (w *window) SetMinimizable(v bool) {
	w.setStyle(wsMinimizeBox, v)
	w.captionChanged()
}

func (w *window) IsMinimizable() bool { return w.hasStyle(wsMinimizeBox) }

func (w *window) SetMaximizable(v bool) {
	w.setStyle(wsMaximizeBox, v)
	w.captionChanged()
}

func (w *window) IsMaximizable() bool { return w.hasStyle(wsMaximizeBox) }
func (w *window) IsClosable() bool    { return w.closable }

func (w *window) SetClosable(v bool) {
	w.closable = v
	menu, _, _ := procGetSystemMenu.Call(w.hwnd, 0)
	flags := uintptr(mfByCommand)
	if !v {
		flags |= mfGrayed
	}
	procEnableMenuItem.Call(menu, scClose, flags)
	if w.caption != nil {
		w.caption.paint()
	}
}

func (w *window) SetAlwaysOnTop(v bool) {
	after := hwndNoTopmost
	if v {
		after = hwndTopmost
	}
	procSetWindowPos.Call(w.hwnd, after, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate)
}

func (w *window) IsAlwaysOnTop() bool { return windowLong(w.hwnd, gwlExStyle)&wsExTopmost != 0 }

func (w *window) Show() {
	if f := w.opts.Flyout; f != nil && !f.Focusable {
		procShowWindow.Call(w.hwnd, swShowNoActivate)
		return
	}
	if w.showMaximized {
		w.showMaximized = false
		procShowWindow.Call(w.hwnd, swShowMaximized)
	} else {
		procShowWindow.Call(w.hwnd, swShow)
	}
	procSetForegroundWindow.Call(w.hwnd)
}

func (w *window) ShowInactive() {
	if w.showMaximized {
		// Maximizing activates the window; there is no inactive variant.
		w.showMaximized = false
		procShowWindow.Call(w.hwnd, swShowMaximized)
		return
	}
	procShowWindow.Call(w.hwnd, swShowNoActivate)
}
func (w *window) Hide() { procShowWindow.Call(w.hwnd, swHide) }

func (w *window) IsVisible() bool {
	r, _, _ := procIsWindowVisible.Call(w.hwnd)
	return r != 0
}

func (w *window) Focus() {
	if w.host != nil {
		w.focusWebView()
		return
	}
	if f := w.opts.Flyout; f != nil && !f.Focusable {
		w.Show() // never activated
		return
	}
	if w.IsMinimized() {
		procShowWindow.Call(w.hwnd, swRestore)
	}
	procSetForegroundWindow.Call(w.hwnd)
	w.focusWebView()
}

func (w *window) Blur() {
	const gwHwndNext = 2
	next, _, _ := procGetNextWindow.Call(w.hwnd, gwHwndNext)
	if next != 0 {
		procSetForegroundWindow.Call(next)
	}
}

func (w *window) IsFocused() bool {
	if w.host != nil {
		return w.focused
	}
	fg, _, _ := procGetForegroundWindow.Call()
	return fg == w.hwnd
}

func (w *window) Minimize() { procShowWindow.Call(w.hwnd, swMinimize) }

func (w *window) IsMinimized() bool {
	r, _, _ := procIsIconic.Call(w.hwnd)
	return r != 0
}

func (w *window) Maximize() {
	if !w.IsVisible() {
		w.showMaximized = true // maximizing would show it
		return
	}
	procShowWindow.Call(w.hwnd, swShowMaximized)
}

func (w *window) Unmaximize() {
	if w.showMaximized {
		w.showMaximized = false
		return
	}
	if w.IsMaximized() {
		procShowWindow.Call(w.hwnd, swRestore)
	}
}

func (w *window) IsMaximized() bool {
	if w.showMaximized {
		return true
	}
	r, _, _ := procIsZoomed.Call(w.hwnd)
	return r != 0
}

// Restore brings a minimized window back, maximized again if it was.
// SW_RESTORE would also take a visible window out of maximized, which
// Restore leaves alone, as on macOS and Linux.
func (w *window) Restore() {
	if w.IsMinimized() {
		procShowWindow.Call(w.hwnd, swRestore)
	}
}

// SetFullScreen takes the window's frame and menu bar away, or gives them
// back. Each step sizes the window again: it is laid out once, at the end.
func (w *window) SetFullScreen(v bool) {
	if v == w.fullScreen {
		return
	}
	if w.inMenu {
		procEndMenu.Call() // its menus come from a bar that comes or goes
	}
	w.reframing = true
	if v {
		w.saved.style = windowLong(w.hwnd, gwlStyle)
		w.saved.exStyle = windowLong(w.hwnd, gwlExStyle)
		w.saved.placement.Length = uint32(unsafe.Sizeof(w.saved.placement))
		procGetWindowPlacement.Call(w.hwnd, uintptr(unsafe.Pointer(&w.saved.placement)))
		w.fullScreen = true
		style := w.saved.style &^ (wsCaption | wsThickFrame)
		setWindowLong(w.hwnd, gwlStyle, style)
		w.attachMenu() // no menu bar in full screen
		m := monitorInfo(w.monitor()).Monitor
		procSetWindowPos.Call(w.hwnd, 0, uintptr(m.Left), uintptr(m.Top), uintptr(m.Right-m.Left), uintptr(m.Bottom-m.Top), swpNoZOrder|swpFrameChanged)
	} else {
		w.fullScreen = false
		setWindowLong(w.hwnd, gwlStyle, w.saved.style)
		setWindowLong(w.hwnd, gwlExStyle, w.saved.exStyle)
		w.attachMenu()
		procSetWindowPlacement.Call(w.hwnd, uintptr(unsafe.Pointer(&w.saved.placement)))
		procSetWindowPos.Call(w.hwnd, 0, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoZOrder|swpFrameChanged)
	}
	w.reframing = false
	kind := uintptr(sizeRestored)
	if w.IsMinimized() {
		kind = sizeMinimized
	} else if zoomed, _, _ := procIsZoomed.Call(w.hwnd); zoomed != 0 {
		kind = sizeMaximized
	}
	w.sized(kind)
	if v {
		w.h.EnteredFullScreen()
	} else {
		w.h.LeftFullScreen()
	}
	w.captionChanged() // hidden in full screen
}

func (w *window) IsFullScreen() bool { return w.fullScreen }

func (w *window) Center() {
	var r rect
	procGetWindowRect.Call(w.hwnd, uintptr(unsafe.Pointer(&r)))
	work := monitorWorkArea(w.monitor())
	width, height := r.Right-r.Left, r.Bottom-r.Top
	x := work.Left + (work.Right-work.Left-width)/2
	y := work.Top + (work.Bottom-work.Top-height)/2
	procSetWindowPos.Call(w.hwnd, 0, uintptr(x), uintptr(y), 0, 0, swpNoSize|swpNoZOrder|swpNoActivate)
}

func (w *window) SetBackgroundColor(c platform.Color) {
	w.bg = &c
	// A web view in native UI has no window of its own to paint: only
	// WebView2 fills it, and a brush would outlive it.
	if w.host == nil {
		if w.bgBrush != 0 {
			procDeleteObject.Call(w.bgBrush)
		}
		w.bgBrush, _, _ = procCreateSolidBrush.Call(uintptr(c.R) | uintptr(c.G)<<8 | uintptr(c.B)<<16)
	}
	w.withWebView(w.applyWebViewBackground)
	if w.hwnd != 0 {
		procInvalidateRect(w.hwnd)
	}
}

// applyWebViewBackground paints the webview before its page does:
// transparent with a material behind it, else the background color.
// WebView2 only supports fully opaque or fully transparent backgrounds.
// A transparent page shows the window's background color behind it, which
// GDI paints; in a window without a redirection bitmap, the webview does.
func (w *window) applyWebViewBackground() {
	ctl2 := queryInterface(w.controller, &iidICoreWebView2Controller2)
	if ctl2 == 0 {
		return
	}
	defer release(ctl2)
	var c uint32 = 0xFFFFFFFF // opaque white: A, R, G, B bytes
	switch {
	case w.vibrancy != "":
		c = 0
	case w.opts.Transparent && (w.bg == nil || !w.noRedirect):
		c = 0
	case w.bg != nil && w.bg.A == 0:
		c = 0
	case w.bg != nil:
		c = 0xFF | uint32(w.bg.R)<<8 | uint32(w.bg.G)<<16 | uint32(w.bg.B)<<24
	}
	comCall(ctl2, ctl2PutDefaultBackgroundColor, uintptr(c))
}

func (w *window) SetOpacity(v float64) {
	w.opacity = v
	w.updateLayered()
}

func (w *window) Opacity() float64 { return w.opacity }

// updateLayered makes the window layered while it is translucent or lets
// the mouse through.
func (w *window) updateLayered() {
	layered := w.opacity < 1 || w.ignoreMouse
	w.setExStyle(wsExLayered, layered)
	if layered {
		procSetLayeredWindowAttributes.Call(w.hwnd, 0, uintptr(byte(w.opacity*255+0.5)), lwaAlpha)
	}
}

// SetHasShadow casts DWM's shadow around a window without a caption, or
// none, by extending its frame a pixel into the client area: a window
// whose client area is all of it, with no borders, has no shadow
// otherwise. A resizable frameless window on Windows 10 keeps borders,
// invisible ones (frameBorder), which DWM casts the shadow around: it is
// left unextended, as Windows 10 draws the frame extended as a white line
// along the top of the client area.
func (w *window) SetHasShadow(v bool) {
	w.shadow = v
	if !w.captionless() || w.vibrancy != "" {
		return
	}
	m := margins{}
	if v && w.frameBorder() == 0 {
		m.Top = 1 // a frame the size of a pixel brings the shadow back
	}
	procDwmExtendFrameIntoClientArea.Call(w.hwnd, uintptr(unsafe.Pointer(&m)))
}

func (w *window) HasShadow() bool { return w.shadow }

func (w *window) SetIgnoreMouseEvents(v bool) {
	w.ignoreMouse = v
	w.setExStyle(wsExTransparent, v)
	w.updateLayered()
}

func (w *window) SetContentProtection(v bool) {
	if !has(procSetWindowDisplayAffinity) {
		return
	}
	affinity := uintptr(wdaNone)
	if v {
		affinity = wdaExcludeFromCapture
	}
	if r, _, _ := procSetWindowDisplayAffinity.Call(w.hwnd, affinity); r == 0 && v {
		procSetWindowDisplayAffinity.Call(w.hwnd, 1) // WDA_MONITOR before Windows 10 2004
	}
}

// Windows 11 system backdrops (DWM_SYSTEMBACKDROP_TYPE).
const (
	backdropNone    = 1
	backdropMica    = 2
	backdropAcrylic = 3
	backdropMicaAlt = 4
)

func backdropFor(material string) int32 {
	switch material {
	case "":
		return backdropNone
	case "mica":
		return backdropMica
	case "tabbed":
		return backdropMicaAlt
	case "acrylic", "menu", "popover", "hud", "sheet", "tooltip", "selection", "fullscreen-ui", "glass", "glass-clear":
		return backdropAcrylic
	}
	return backdropMica
}

// SetActiveAppearance does nothing: Windows has no way to draw a window
// that is not active as one that is.
func (w *window) SetActiveAppearance(bool) {}

// SetWindowButtonsTheme draws the controls of a hidden title bar for a
// "light" or "dark" background, whatever the app's appearance; "system"
// follows it again.
func (w *window) SetWindowButtonsTheme(source string) {
	if source == "system" {
		source = ""
	}
	if source == w.buttonsTheme {
		return
	}
	w.buttonsTheme = source
	if w.caption != nil {
		w.caption.paint()
	}
}

// buttonsDark reports whether the controls of a hidden title bar draw for
// a dark background: as SetWindowButtonsTheme says, else as the app.
func (w *window) buttonsDark() bool {
	switch w.buttonsTheme {
	case "dark":
		return true
	case "light":
		return false
	}
	return w.b.isDark()
}

func (w *window) SetVibrancy(material string) {
	w.vibrancy = material
	backdrop := backdropFor(material)
	w.extendGlass(true)
	procDwmSetWindowAttribute.Call(w.hwnd, dwmwaSystemBackdropType, uintptr(unsafe.Pointer(&backdrop)), 4)
	if material == "" && w.captionless() && w.shadow {
		w.SetHasShadow(true)
	}
	w.withWebView(w.applyWebViewBackground)
	procInvalidateRect(w.hwnd)
}

// SetVibrancyTint does nothing: Windows' backdrops take no tint.
func (w *window) SetVibrancyTint(*platform.Color) {}

// extendGlass extends the frame where the material shows, behind the
// page: over the whole client area, or nowhere without a material. With
// always it extends the frame even when its extension has not changed.
func (w *window) extendGlass(always bool) {
	m := margins{}
	if w.vibrancy != "" {
		var r rect
		procGetClientRect.Call(w.hwnd, uintptr(unsafe.Pointer(&r)))
		m = glassMargins(w.glassFromBottom(), r.Bottom-r.Top)
	}
	if m == w.glass && !always {
		return
	}
	w.glass = m
	procDwmExtendFrameIntoClientArea.Call(w.hwnd, uintptr(unsafe.Pointer(&m)))
}

// glassFromBottom reports a window whose frame extends from the bottom
// over its client area (glassMargins) rather than all over: a frameless
// window, and a maximized one. Flyouts are popups without a caption, whose
// frame DWM draws no buttons in.
//
// A frameless window keeps WS_CAPTION, WS_SYSMENU and the boxes of its
// styles, for snapping, Win+arrow keys, the taskbar's minimize and the
// window menu of Alt+Space; frameCalcSize leaves it no title bar. Extended
// all over, the frame covers the window's top, and DWM draws its caption
// buttons there, over the page.
//
// A maximized window's title bar is shorter than a restored one's, but in a
// frame extended all over DWM draws the system's caption buttons as tall as
// a restored window's: past the title bar, where the color of the one under
// the pointer shows through a translucent page.
func (w *window) glassFromBottom() bool {
	if w.frameless {
		return w.opts.Flyout == nil
	}
	return !w.fullScreen && w.IsMaximized()
}

// glassMargins returns the frame's extension that shows the material over
// a client area height pixels high. DWM draws the caption buttons of a
// window's styles in the frame at the top. A restored window with a hidden
// title bar draws its own over them. Maximized, its client area starts
// below the top of the window (frameCalcSize) and DWM's buttons would show
// above the title bar's; a frameless window has no buttons of its own at
// all. fromBottom extends the frame from the bottom over the whole client
// area instead: the material still shows everywhere, and the frame's top
// is only what lies above the client area: a strip off screen or nothing,
// too short for buttons, or the system's title bar, which holds them.
func glassMargins(fromBottom bool, height int32) margins {
	if fromBottom {
		return margins{Bottom: max(height, 1)}
	}
	return margins{-1, -1, -1, -1} // a sheet over the whole window
}

// hasAppMenu tells whether the window shows the application menu in its
// menu bar: flyouts and panels show none.
func (w *window) hasAppMenu() bool { return w.opts.Flyout == nil && !w.opts.Panel }

func (w *window) SetMenu(m *platform.Menu) {
	w.ownMenu = m != nil
	if m == nil && w.hasAppMenu() {
		m = w.b.appMenu
	}
	w.installMenu(m)
}

func (w *window) SetAutoHideMenu(v bool) {
	w.autoHideMenu = v
	if w.hmenu != 0 && !w.revealed {
		w.attachMenu()
	}
}

func (w *window) StartDrag() {
	procReleaseCapture.Call()
	procSendMessageW.Call(w.hwnd, wmNCLButtonDown, htCaption, 0)
}

func (w *window) TitleBarDoubleClicked() {
	if w.IsMaximized() {
		w.Unmaximize()
	} else if w.hasStyle(wsMaximizeBox) {
		w.Maximize()
	}
}

func (w *window) Close() {
	if w.host != nil {
		w.closeWebView()
		return
	}
	if !w.closed {
		w.destroy()
	}
}

func (w *window) setIcon(icon uintptr) {
	const iconSmall, iconBig = 0, 1
	procSendMessageW.Call(w.hwnd, wmSetIcon, iconSmall, icon)
	procSendMessageW.Call(w.hwnd, wmSetIcon, iconBig, icon)
}

func procInvalidateRect(hwnd uintptr) { procInvalidateRectW.Call(hwnd, 0, 1) }

func boolArg(v bool) uintptr {
	if v {
		return 1
	}
	return 0
}
