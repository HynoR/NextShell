//go:build windows && (amd64 || arm64)

package windows

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/egoist/mygo/internal/platform"
)

// Backend is the Windows (Win32 + WebView2) implementation of
// platform.Backend.
type Backend struct {
	h       platform.AppHandler
	name    string
	appHwnd uintptr // hidden window receiving app level messages
	running bool

	signaled atomic.Bool
	// posted are the functions post runs on the main thread.
	postedMu sync.Mutex
	posted   []func()

	windows map[uintptr]*window // by HWND
	// languages are those SetLanguages told: the first is the language
	// of the WebView2 environment, once made.
	languages []string
	appMenu   *platform.Menu

	// The WebView2 environment is created asynchronously, when the first
	// window that shows a web page needs it; windows wait.
	env        uintptr
	envErr     error
	envStarted bool
	envWaiters []func()
	// envUnthrottled tells that the environment's browser runs, or is
	// starting, with unthrottledArguments, which the page that started it
	// asked for; envWarned that a page that asked later was told it was
	// too late.
	envUnthrottled, envWarned bool
	// envAutoplay and envAutoplayWarned are the same for autoplayArguments
	// (PageOptions.Autoplay).
	envAutoplay, envAutoplayWarned bool
	// envResult is whether the environment was created (1) or failed (2),
	// 0 before, for WebViewAvailable, which any goroutine may call.
	envResult atomic.Int32

	// cookies gives cookie calls a cookie manager (cookies.go).
	cookies cookieStore
	// stores are the profiles of sessions (session.go), privateStores
	// the number of InPrivate ones made.
	stores        []*dataStore
	privateStores int

	menus menuTable

	trays          map[uint32]*tray
	nextTray       uint32
	taskbarCreated uint32

	// ITaskbarList3 (taskbar.go) and the message telling a window its
	// taskbar button exists.
	taskbarList          uintptr
	taskbarFailed        bool
	taskbarButtonCreated uint32

	watchingPower bool
	notifications map[string]*notification

	themeSource string
	badge       string
	hidden      []*window
	icon        uintptr // default window icon (SetDockIcon)

	// captions are the windows of the controls of windows with a hidden
	// title bar (titlebar.go), captionFonts their glyphs by DPI.
	captions map[uintptr]*captionBar
	// topEdges are the windows over the top resize borders of windows
	// without a caption (topedge.go), and their windows.
	topEdges     map[uintptr]*window
	captionFonts map[int]captionFont
	// menuFonts are the fonts of the menu items MyGo draws by DPI,
	// menuExtra how much Windows widens them by DPI, and menuBrushes the
	// brushes of menus' backgrounds by color (menudraw.go).
	menuFonts   map[int]menuFonts
	menuExtra   map[int]int32
	menuBrushes map[uint32]uintptr
	// menuHook hooks the main thread while menus show, and menuGlass is
	// whether they show the material behind them. menuWindows are the
	// windows of menus it subclassed, and popups counts the popup menus
	// MyGo is showing: TrackPopupMenuEx runs the menu loop that creates
	// their windows, and their submenus', unlike the loops of a menu bar's
	// dropdowns and of Windows' window menu, which are not MyGo's
	// (menuProc).
	menuHook    uintptr
	menuGlass   bool
	menuWindows map[uintptr]*menuWindow
	popups      int
	// comp shows the controls of windows with a material behind them
	// (compositor.go). compRetry is when to try making it again after
	// compFails failures in a row, and composeAt when timerCompose draws
	// again the controls that could not draw, zero when it is not set.
	comp                 *composition
	compRetry, composeAt time.Time
	compFails            int

	// surfaces are the windows of the content MyGo draws (surface.go).
	surfaces map[uintptr]*surface
}

var (
	theBackend   *Backend
	mainThreadID = currentThreadID() // package initialization runs on the main thread
)

// New creates the Windows backend.
func New() *Backend {
	return &Backend{
		windows:       map[uintptr]*window{},
		trays:         map[uint32]*tray{},
		notifications: map[string]*notification{},
		menus:         newMenuTable(),
		themeSource:   "system",
		captions:      map[uintptr]*captionBar{},
		topEdges:      map[uintptr]*window{},
		captionFonts:  map[int]captionFont{},
		surfaces:      map[uintptr]*surface{},
	}
}

func (b *Backend) Name() string { return "windows/webview2" }

func (b *Backend) IsMainThread() bool { return currentThreadID() == mainThreadID }

func (b *Backend) Init(h platform.AppHandler, opts platform.AppOptions) error {
	b.h = h
	b.name = opts.Name
	theBackend = b
	procCoInitializeEx.Call(0, coinitApartmentThreaded)
	if has(procSetProcessDpiAwarenessContext) {
		procSetProcessDpiAwarenessContext.Call(dpiAwarenessContextPerMonitorAwareV2)
	}
	activateCommonControls()
	if err := registerClasses(); err != nil {
		return err
	}
	b.appHwnd = createWindow(0, appClass, "", wsOverlapped, 0, 0, 0, 0, 0)
	if b.appHwnd == 0 {
		return fmt.Errorf("mygo: cannot create the application window")
	}
	b.taskbarCreated = registerWindowMessage("TaskbarCreated")
	b.taskbarButtonCreated = registerWindowMessage("TaskbarButtonCreated")
	b.applyMenuTheme() // for the menus of trays too, before any window
	// Clipboard and drag/drop share startup-created OLE callback tables.
	dropOnce.Do(initDropTarget)
	return nil
}

// startEnvironment starts creating the WebView2 environment, the first time
// a window that shows a web page is created, and returns why it cannot,
// such as a missing WebView2 Runtime. Windows that show native UI need
// none: an app whose windows all do runs without the runtime. o are the
// options of the page that needs it, nil for none.
//
// The environment's browser runs with unthrottledArguments when that first
// page asked not to be throttled, and with autoplayArguments when it asked
// to play without a user gesture: the arguments are the browser's, which
// all the app's pages share, and a page that asks once it runs is too
// late, which the log tells.
func (b *Backend) startEnvironment(o *platform.WindowOptions) error {
	unthrottled := o != nil && o.NoBackgroundThrottling
	autoplay := o != nil && o.Autoplay
	if b.envStarted {
		if unthrottled && !b.envUnthrottled && !b.envWarned {
			b.envWarned = true
			log.Print("mygo: ThrottlingOff of a page shown after the first one: WebView2's browser already runs without the arguments that keep hidden pages running; give the option to the first window or web view that shows a page")
		}
		if autoplay && !b.envAutoplay && !b.envAutoplayWarned {
			b.envAutoplayWarned = true
			log.Print("mygo: AutoplayAllow of a page shown after the first one: WebView2's browser already runs without the argument that lets pages play sound without a user gesture; give the option to the first window or web view that shows a page")
		}
		return nil
	}
	dir := os.Getenv("LOCALAPPDATA")
	if dir == "" {
		dir = os.TempDir()
	}
	userData := filepath.Join(dir, sanitize(b.name), "WebView2")
	_ = os.MkdirAll(userData, 0o755)
	language := ""
	if len(b.languages) > 0 {
		language = b.languages[0]
	}
	// What the first page asked for counts from now, so that a page shown
	// while the environment is being made isn't told it is too late.
	b.envUnthrottled, b.envAutoplay = unthrottled, autoplay
	err := createEnvironment(userData, language, browserArguments(unthrottled, autoplay), func(env uintptr, withArgs bool, err error) {
		b.env, b.envErr = env, err
		if err != nil || !withArgs {
			b.envUnthrottled, b.envAutoplay = false, false
		}
		if err != nil {
			log.Print(err)
			b.envResult.Store(2)
		} else {
			b.envResult.Store(1)
		}
		waiters := b.envWaiters
		b.envWaiters = nil
		for _, fn := range waiters {
			fn()
		}
	})
	// A runtime installed later is found by the next window.
	b.envStarted = err == nil
	return err
}

// whenEnvironment runs fn once the WebView2 environment, which
// startEnvironment started, exists (or failed).
func (b *Backend) whenEnvironment(fn func()) {
	if b.env != 0 || b.envErr != nil {
		fn()
		return
	}
	b.envWaiters = append(b.envWaiters, fn)
}

func sanitize(name string) string {
	out := []rune{}
	for _, r := range name {
		if r < ' ' || r == '<' || r == '>' || r == ':' || r == '"' || r == '/' || r == '\\' || r == '|' || r == '?' || r == '*' {
			r = '_'
		}
		out = append(out, r)
	}
	if len(out) == 0 {
		return "MyGo"
	}
	return string(out)
}

func (b *Backend) Run() error {
	b.running = true
	postMessage(b.appHwnd, wmAppReady, 0, 0)
	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	b.running = false
	return nil
}

// Quit ends the event loop. Modal loops (dialogs, menus) end too: they
// forward WM_QUIT to the loop they run in.
func (b *Backend) Quit() { procPostQuitMessage.Call(0) }

func (b *Backend) Signal() {
	if b.appHwnd == 0 || !b.signaled.CompareAndSwap(false, true) {
		return
	}
	postMessage(b.appHwnd, wmAppDispatch, 0, 0)
}

// post runs fn on the main thread, for the backend's own goroutines.
func (b *Backend) post(fn func()) {
	b.postedMu.Lock()
	b.posted = append(b.posted, fn)
	b.postedMu.Unlock()
	postMessage(b.appHwnd, wmAppRun, 0, 0)
}

func (b *Backend) runPosted() {
	b.postedMu.Lock()
	posted := b.posted
	b.posted = nil
	b.postedMu.Unlock()
	for _, fn := range posted {
		fn()
	}
}

func (b *Backend) Step() {
	procMsgWaitForMultipleObjectsEx.Call(0, 0, 500, qsAllInput, mwmoInputAvailable)
	var m msg
	for {
		r, _, _ := procPeekMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0, pmRemove)
		if r == 0 {
			return
		}
		if m.Message == wmQuit {
			procPostQuitMessage.Call(m.WParam) // for the loop running outside
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func (b *Backend) Wake() {
	if b.appHwnd != 0 {
		postMessage(b.appHwnd, wmAppWake, 0, 0)
	}
}

func (b *Backend) App() platform.AppController   { return appController{b} }
func (b *Backend) Dialogs() platform.Dialogs     { return dialogs{b} }
func (b *Backend) Clipboard() platform.Clipboard { return clipboard{b} }
func (b *Backend) Shell() platform.Shell         { return shell{} }
func (b *Backend) Screen() platform.Screen       { return screen{} }
func (b *Backend) Theme() platform.Theme         { return theme{b} }

// Window classes and the window procedure, created once.
const (
	appClass    = "MyGoApp"
	windowClass = "MyGoWindow"
)

var wndProcCallback uintptr

func registerClasses() error {
	if wndProcCallback != 0 {
		return nil
	}
	wndProcCallback = syscall.NewCallback(wndProc)
	cursor, _, _ := procLoadCursorW.Call(0, idcArrow)
	icon, _, _ := procLoadIconW.Call(instance(), 1) // the icon resource `mygo build` embeds
	for _, name := range []string{appClass, windowClass, captionClass, flyoutShadowClass} {
		style := uint32(0x0003) // CS_HREDRAW | CS_VREDRAW
		switch name {
		case captionClass:
			style = 0x0008 // CS_DBLCLKS: a double click on the top edge
		case flyoutShadowClass:
			style |= 0x00020000 // CS_DROPSHADOW
		}
		wc := wndClassEx{
			Style:     style,
			WndProc:   wndProcCallback,
			Instance:  instance(),
			Cursor:    cursor,
			Icon:      icon,
			IconSm:    icon,
			ClassName: u16(name),
		}
		wc.Size = uint32(unsafe.Sizeof(wc))
		if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
			return fmt.Errorf("mygo: cannot register the window class: %v", err)
		}
	}
	return nil
}

func createWindow(exStyle uint32, class, title string, style uint32, x, y, w, h int32, parent uintptr) uintptr {
	hwnd, _, _ := procCreateWindowExW.Call(uintptr(exStyle),
		uintptr(unsafe.Pointer(u16(class))), uintptr(unsafe.Pointer(u16(title))), uintptr(style),
		uintptr(x), uintptr(y), uintptr(w), uintptr(h), parent, 0, instance(), 0)
	return hwnd
}

func registerWindowMessage(name string) uint32 {
	r, _, _ := procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(u16(name))))
	return uint32(r)
}

func wndProc(hwnd, m, wp, lp uintptr) uintptr {
	b := theBackend
	if b != nil {
		if hwnd == b.appHwnd {
			if r, ok := b.appMessage(uint32(m), wp, lp); ok {
				return r
			}
		} else if w := b.windows[hwnd]; w != nil {
			if r, ok := w.message(uint32(m), wp, lp); ok {
				return r
			}
		} else if c := b.captions[hwnd]; c != nil {
			if r, ok := c.message(hwnd, uint32(m), wp, lp); ok {
				return r
			}
		} else if w := b.topEdges[hwnd]; w != nil {
			if r, ok := w.topEdgeMessage(uint32(m), wp, lp); ok {
				return r
			}
		} else if s := b.surfaces[hwnd]; s != nil {
			if r, ok := s.message(hwnd, uint32(m), wp, lp); ok {
				return r
			}
		}
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, m, wp, lp)
	return r
}

// appMessage handles the messages of the application window.
func (b *Backend) appMessage(m uint32, wp, lp uintptr) (uintptr, bool) {
	if r, ok := b.menuItemMessage(b.appHwnd, m, wp, lp); ok {
		return r, true
	}
	switch m {
	case wmAppReady:
		b.h.Ready()
		return 0, true
	case wmAppDispatch:
		b.signaled.Store(false)
		b.h.Dispatch()
		return 0, true
	case wmAppWake:
		return 0, true
	case wmAppRun:
		b.runPosted()
		return 0, true
	case wmAppCookieView:
		b.closeCookieViewNow()
		return 0, true
	case wmAppTray:
		b.trayMessage(uint32(wp), uint32(loword(lp)))
		return 0, true
	case wmAppTrayAdd:
		b.addNewTrays()
		return 0, true
	case wmHotkey:
		b.h.HotkeyPressed(int(wp))
		return 0, true
	case wmPowerBroadcast, wmWTSSessionChange:
		b.powerMessage(m, wp)
		return 1, false // TRUE for power broadcasts, and default handling
	case wmSettingChange:
		if preferencesChanged(wp, lp) {
			b.dropMenuFonts() // the size of text, or the menu font
		}
		switch {
		case lp != 0 && wstr(lp) == settingImmersiveColorSet:
			b.applyTheme()
			b.h.ThemeChanged()
			b.traysAppearance()
		case lp != 0 && wstr(lp) == settingIntl:
			// The display languages or the regional formats.
			b.h.LocaleChanged()
		case preferencesChanged(wp, lp):
			b.h.ThemeChanged()
		}
		return 0, false
	case wmDisplayChange:
		b.h.DisplaysChanged()
		return 0, false
	case wmTimer:
		if wp == timerCompose {
			b.recompose()
			return 0, true
		}
		if wp == timerCookieView && checkCookieViews != nil {
			checkCookieViews(b)
			return 0, true
		}
	case wmAppDeviceRemoved:
		b.deviceRemoved(wp)
		return 0, true
	case wmAppNetwork:
		b.h.NetworkChanged(b.Network().Status())
		return 0, true
	case wmQueryEndSession:
		return 1, true
	case wmEndSession:
		if wp != 0 {
			b.h.Terminating()
		}
		return 0, true
	}
	if m == b.taskbarCreated && m != 0 {
		// Explorer restarted: the notification area forgot our icons.
		for _, t := range b.trays {
			if t.added {
				t.add()
			}
		}
		return 0, true
	}
	return 0, false
}

// activateCommonControls activates version 6 of the common controls
// (themed controls, task dialogs) for executables without a manifest
// asking for it, like `go run` builds: shell32.dll carries such a
// manifest as resource 124.
func activateCommonControls() {
	if !has(procCreateActCtxW) {
		return
	}
	type actCtx struct {
		Size                  uint32
		Flags                 uint32
		Source                *uint16
		ProcessorArchitecture uint16
		LangID                uint16
		AssemblyDirectory     *uint16
		ResourceName          uintptr
		ApplicationName       *uint16
		Module                uintptr
	}
	const (
		actCtxFlagResourceNameValid      = 0x008
		actCtxFlagAssemblyDirectoryValid = 0x004
	)
	dir := u16(sysDir)
	ctx := actCtx{
		Flags:             actCtxFlagResourceNameValid | actCtxFlagAssemblyDirectoryValid,
		Source:            u16(sysDir + `\shell32.dll`),
		AssemblyDirectory: dir,
		ResourceName:      124,
	}
	ctx.Size = uint32(unsafe.Sizeof(ctx))
	h, _, _ := procCreateActCtxW.Call(uintptr(unsafe.Pointer(&ctx)))
	if h == ^uintptr(0) { // INVALID_HANDLE_VALUE
		return
	}
	var cookie uintptr
	procActivateActCtx.Call(h, uintptr(unsafe.Pointer(&cookie)))
}

type appController struct{ b *Backend }

func (a appController) SetActivationPolicy(string) {}

// AllowForeground passes the app's right to the foreground on to the next
// process to ask (ASFW_ANY). Only the process in front has it to pass on,
// so an app passes it before it closes its windows: a process it starts
// once they closed could no longer take it from the app.
func (a appController) AllowForeground() {
	const asfwAny = ^uintptr(0)
	procAllowSetForegroundWindow.Call(asfwAny)
}

func (a appController) WithActivationToken(_ string, fn func()) { fn() }

// Activate brings the app forward as macOS does, with its windows in their
// order: a window of the app already in front stays there, else the one
// the user had last, the highest of them in the z-order, comes forward.
// Flyouts that never take the keyboard bring nothing forward.
func (a appController) Activate() {
	if fg, _, _ := procGetForegroundWindow.Call(); a.b.windows[fg] != nil {
		return
	}
	const gwHwndNext = 2
	h, _, _ := procGetTopWindow.Call(0)
	for ; h != 0; h, _, _ = procGetNextWindow.Call(h, gwHwndNext) {
		w := a.b.windows[h]
		if w == nil || !w.IsVisible() {
			continue
		}
		if f := w.opts.Flyout; f != nil && !f.Focusable {
			continue
		}
		w.Focus()
		return
	}
}

func (a appController) Hide() {
	a.b.hidden = nil
	for _, w := range a.b.windows {
		if w.IsVisible() {
			a.b.hidden = append(a.b.hidden, w)
			w.Hide()
		}
	}
}

func (a appController) Unhide() {
	for _, w := range a.b.hidden {
		if !w.closed {
			w.ShowInactive()
		}
	}
	a.b.hidden = nil
}

func (a appController) IsHidden() bool        { return len(a.b.hidden) > 0 }
func (a appController) SetBadge(label string) { a.b.badge = label }
func (a appController) Badge() string         { return a.b.badge }

// IsActive reports whether the window in front is one of the app's.
func (a appController) IsActive() bool {
	fg, _, _ := procGetForegroundWindow.Call()
	return a.b.windows[fg] != nil
}

const (
	flashwStop      = 0
	flashwTray      = 0x2
	flashwAll       = 0x3
	flashwTimerNoFG = 0xC
)

func (a appController) Bounce(critical bool) int {
	flags := uint32(flashwTray | flashwTimerNoFG)
	if critical {
		flags = flashwAll | flashwTimerNoFG
	}
	for _, w := range a.b.windows {
		fi := flashWInfo{HWnd: w.hwnd, Flags: flags}
		fi.Size = uint32(unsafe.Sizeof(fi))
		procFlashWindowEx.Call(uintptr(unsafe.Pointer(&fi)))
	}
	return 1
}

func (a appController) CancelBounce(int) {
	for _, w := range a.b.windows {
		fi := flashWInfo{HWnd: w.hwnd, Flags: flashwStop}
		fi.Size = uint32(unsafe.Sizeof(fi))
		procFlashWindowEx.Call(uintptr(unsafe.Pointer(&fi)))
	}
}

// SetDockMenu does nothing: there is no Dock.
func (a appController) SetDockMenu(*platform.Menu) {}

func (a appController) SetDockIcon(png []byte) error {
	icon, err := iconFromPNG(png, 0)
	if err != nil {
		return err
	}
	if a.b.icon != 0 {
		procDestroyIcon.Call(a.b.icon)
	}
	a.b.icon = icon
	for _, w := range a.b.windows {
		w.setIcon(icon)
	}
	return nil
}

func (a appController) ShowAboutPanel(o platform.AboutPanelOptions) {
	detail := o.ApplicationVersion
	if o.Version != "" {
		detail += " (" + o.Version + ")"
	}
	for _, s := range []string{o.Copyright, o.Credits} {
		if s != "" {
			detail += "\n" + s
		}
	}
	tx := platform.CurrentTexts()
	title := strings.Replace(tx.About, "\x00", o.ApplicationName, 1)
	a.b.messageBox(0, &platform.MessageBoxOptions{Type: "info", Title: title, Message: o.ApplicationName, Detail: detail, Buttons: []string{tx.OK}})
}

// Package reads the version resource `mygo build` embeds in the
// executable.
func (a appController) Package() (platform.PackageInfo, bool) {
	exe, err := os.Executable()
	if err != nil {
		return platform.PackageInfo{}, false
	}
	path := u16(exe)
	size, _, _ := procGetFileVersionInfoSizeW.Call(uintptr(unsafe.Pointer(path)), 0)
	if size == 0 {
		return platform.PackageInfo{}, false
	}
	data := make([]byte, size)
	if r, _, _ := procGetFileVersionInfoW.Call(uintptr(unsafe.Pointer(path)), 0, size, uintptr(unsafe.Pointer(&data[0]))); r == 0 {
		return platform.PackageInfo{}, false
	}
	value := func(path string) (uintptr, uint32) {
		var p uintptr
		var n uint32
		if r, _, _ := procVerQueryValueW.Call(uintptr(unsafe.Pointer(&data[0])), uintptr(unsafe.Pointer(u16(path))), uintptr(unsafe.Pointer(&p)), uintptr(unsafe.Pointer(&n))); r == 0 || n == 0 {
			return 0, 0
		}
		return p, n
	}
	// The strings of the app's own language, the first of its
	// translations: en-US and Unicode (040904b0) before it shipped others.
	table := "040904b0"
	if p, n := value(`\VarFileInfo\Translation`); n >= 4 {
		lang, codePage := *(*uint16)(native(p)), *(*uint16)(native(p + 2))
		table = fmt.Sprintf("%04x%04x", lang, codePage)
	}
	query := func(key string) string {
		if p, _ := value(`\StringFileInfo\` + table + `\` + key); p != 0 {
			return wstr(p)
		}
		return ""
	}
	info := platform.PackageInfo{Name: query("ProductName"), Version: query("ProductVersion"), Identifier: query("MyGoIdentifier")}
	if info.Identifier == "" {
		return platform.PackageInfo{}, false // not built by mygo build
	}
	return info, true
}

// ClearBrowsingData clears the profile of store, the one the app's web
// views share for nil, through the web view cookie calls use: a window's,
// or a hidden one before a window shows a page (cookies.go). SiteDataAll
// is ClearBrowsingDataAll, other types ClearBrowsingData of their kinds.
func (a appController) ClearBrowsingData(store platform.DataStore, types platform.SiteData, done func(error)) {
	a.withCookieManager(store, func(_ []platform.Cookie, err error) { done(err) }, func(r *cookieRequest) {
		wv13 := queryInterface(r.webview, &iidICoreWebView2_13)
		if wv13 == 0 {
			r.finish(nil, errors.New("mygo: clearing browsing data needs a newer WebView2 Runtime"))
			return
		}
		var profile uintptr
		comCall(wv13, wv13GetProfile, uintptr(unsafe.Pointer(&profile)))
		release(wv13)
		p2 := queryInterface(profile, &iidICoreWebView2Profile2)
		release(profile)
		if p2 == 0 {
			r.finish(nil, errors.New("mygo: clearing browsing data needs a newer WebView2 Runtime"))
			return
		}
		defer release(p2)
		hr := withHandler(func(result, _ uintptr) {
			if failed(result) {
				r.finish(nil, hresultError("clearing browsing data", result))
				return
			}
			r.finish(nil, nil)
		}, func(h uintptr) uintptr {
			if types.All() {
				return comCall(p2, profile2ClearBrowsingDataAll, h)
			}
			return comCall(p2, profile2ClearBrowsingData, browsingDataKinds(types), h)
		})
		if failed(hr) {
			r.finish(nil, hresultError("clearing browsing data", hr))
		}
	})
}
