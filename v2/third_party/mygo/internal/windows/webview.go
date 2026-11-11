//go:build windows && (amd64 || arm64)

package windows

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"github.com/egoist/mygo/internal/accelerator"
	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/schemehost"
)

// Custom schemes are served from http://<scheme>.localhost/, and their
// other hosts from http://<host>.<scheme>.localhost/ (package schemehost),
// which WebView2 lets the app answer (WebResourceRequested). Chromium
// treats *.localhost as a secure origin, and unlike https, http blocks no
// mixed content, so pages reach ws:// and http:// URLs as on macOS and
// Linux. The rest of MyGo sees <scheme>://<host>/ URLs.

// webURL maps a custom scheme URL of the window to the URL the webview
// loads.
func (w *window) webURL(raw string) string { return schemehost.WebURL(raw, w.hasScheme) }

// appURL maps a URL loaded by the webview back to its custom scheme URL.
func (w *window) appURL(raw string) string { return schemehost.AppURL(raw, w.hasScheme) }

func (w *window) hasScheme(s string) bool {
	for _, x := range w.opts.Schemes {
		if x == s {
			return true
		}
	}
	return false
}

// pendingCall is a call waiting for the webview. fail, unless nil, runs
// instead when the webview never comes, so that the call's callback runs.
type pendingCall struct {
	run  func()
	fail func(error)
}

var errNoWebView = errors.New("mygo: the window shows native UI, not a web page")

// withWebView runs fn once the webview exists.
func (w *window) withWebView(fn func()) { w.withWebViewOr(fn, nil) }

// withWebViewOr runs fn once the webview exists, or fail, unless nil, with
// the reason it never will: the window closed, or WebView2 could not
// create the webview.
func (w *window) withWebViewOr(fn func(), fail func(error)) {
	var err error
	switch {
	case w.closed:
		err = errDestroyed
	case w.surface != nil:
		err = errNoWebView
	case w.webViewErr != nil:
		err = w.webViewErr
	case w.ready:
		fn()
		return
	default:
		w.pending = append(w.pending, pendingCall{fn, fail})
		return
	}
	if fail != nil {
		fail(err)
	}
}

// failPending fails the calls waiting for the webview.
func (w *window) failPending(err error) {
	failCookieRequests(w, err)
	pending := w.pending
	w.pending = nil
	for _, c := range pending {
		if c.fail != nil {
			c.fail(err)
		}
	}
}

// webViewFailed records why the window has no webview, which later calls
// get, and fails those waiting for it.
func (w *window) webViewFailed(err error) {
	if w.webViewErr != nil {
		return
	}
	log.Print(err)
	w.webViewErr = err
	w.failPending(err)
}

// A window asks WebView2 for its webview up to webViewAttempts times, a
// second, then two, apart: under load, creations fail with ERROR_BUSY or
// CO_E_SERVER_EXEC_FAILURE, and Microsoft advises trying again unless one
// failed with ERROR_INVALID_STATE (the environment's options differ from
// those of the browser process running for its user data).
const (
	webViewAttempts   = 3
	errorInvalidState = 0x8007139F // HRESULT_FROM_WIN32(ERROR_INVALID_STATE)
	timerWebView      = 2          // the window's timer that asks again
)

func (w *window) createWebView() {
	if w.closed {
		return
	}
	if w.b.envErr != nil {
		w.webViewFailed(w.b.envErr)
		return
	}
	if w.opts.DataStore != nil && !w.b.hasProfiles() {
		w.webViewFailed(errNoProfiles)
		return
	}
	fail := testFailWebViews > 0
	if fail {
		testFailWebViews--
	}
	hr := withHandler(func(hr, controller uintptr) {
		// A web view in a window of native UI gets a composition
		// controller, whose controller closes it.
		closeController := func() {
			if c := queryInterface(controller, &iidICoreWebView2Controller); c != 0 {
				comCall(c, ctlClose)
				release(c)
			}
		}
		if fail && controller != 0 { // TestFailWebViews
			closeController()
			hr, controller = eFail, 0
		}
		// Closing the window aborts the creation (E_ABORT).
		if w.closed || w.destroying || w.host != nil && w.host.closed {
			if controller != 0 {
				closeController()
			}
			return
		}
		if failed(hr) || controller == 0 {
			w.creationFailed(hr)
			return
		}
		addRef(controller)
		if w.host != nil {
			controller = w.setUpEmbedded(controller)
		}
		w.setUp(controller)
	}, func(h uintptr) uintptr {
		if w.host != nil {
			return w.createEmbedded(h)
		}
		return w.b.createController(storeOf(w.opts.DataStore), w.hwnd, false, h)
	})
	if failed(hr) {
		w.creationFailed(hr)
	}
}

// creationFailed has the window ask for its webview again later, or, if it
// may not, fails the calls waiting for it.
func (w *window) creationFailed(hr uintptr) {
	err := hresultError("creating the WebView2 controller", hr)
	w.webViewFails++
	if hr == errorInvalidState || w.webViewFails >= webViewAttempts {
		w.webViewFailed(err)
		return
	}
	log.Printf("%v; trying again", err)
	hwnd := w.hwnd
	if w.host != nil && w.host.surface != nil {
		hwnd = w.host.surface.hwnd // which asks again for its web views
	}
	procSetTimer.Call(hwnd, timerWebView, uintptr(backoff(w.webViewFails-1)/time.Millisecond), 0)
}

func (w *window) setUp(controller uintptr) {
	w.controller = controller
	comCall(controller, ctlGetCoreWebView2, uintptr(unsafe.Pointer(&w.webview)))
	comCall(w.webview, wvGetSettings, uintptr(unsafe.Pointer(&w.settings)))
	o := w.opts
	s := w.settings
	comCall(s, setPutIsScriptEnabled, 1)
	comCall(s, setPutIsWebMessageEnabled, 1)
	comCall(s, setPutAreDefaultScriptDialogsEnabled, 1)
	comCall(s, setPutIsStatusBarEnabled, 0)
	comCall(s, setPutAreDevToolsEnabled, boolArg(o.DevTools))
	// The browser's own keys (F5 and Ctrl+R reload, Ctrl+P prints, Ctrl+F
	// finds, Ctrl+Plus zooms) come with the inspector: WKWebView and
	// WebKitGTK have none, and a reload is not a navigation OnWillNavigate
	// is asked about. AcceleratorKeyPressed still sees every key, so the
	// menu items' keys, as the View menu's Reload and Zoom In, still work.
	if s3 := queryInterface(s, &iidICoreWebView2Settings3); s3 != 0 {
		comCall(s3, set3PutAreBrowserAcceleratorKeysEnabled, boolArg(o.DevTools))
		release(s3)
	}
	comCall(s, setPutAreDefaultContextMenusEnabled, 1)
	comCall(s, setPutIsZoomControlEnabled, 1)
	// WebView2 has a touchscreen's pinch zoom and swipe navigation on:
	// they are off unless the page asks, as in WKWebView.
	if s5 := queryInterface(s, &iidICoreWebView2Settings5); s5 != 0 {
		comCall(s5, set5PutIsPinchZoomEnabled, boolArg(o.PinchZoom))
		release(s5)
	}
	if s6 := queryInterface(s, &iidICoreWebView2Settings6); s6 != 0 {
		comCall(s6, set6PutIsSwipeNavigationEnabled, boolArg(o.SwipeNavigation))
		release(s6)
	}
	if o.UserAgent != "" || o.UserAgentSuffix != "" {
		w.setUpUserAgent()
	}
	w.applyWebViewBackground()

	w.addUserScripts()
	if w.host != nil {
		forwardKeys(w)
	}

	// The add methods write a registration token. Its address is taken in
	// each call (go:uintptrescapes), as a uintptr saved earlier goes stale
	// when the goroutine stack moves.
	var tok int64
	add := func(index int, fn func(sender, args uintptr)) {
		withHandler(fn, func(h uintptr) uintptr { return comCall(w.webview, index, h, uintptr(unsafe.Pointer(&tok))) })
	}
	add(wvAddNavigationStarting, w.navigationStarting)
	add(wvAddContentLoading, func(_, _ uintptr) { w.h.NavigationCommitted(w.URL()) })
	add(wvAddNavigationCompleted, w.navigationCompleted)
	add(wvAddDocumentTitleChanged, func(_, _ uintptr) {
		var p uintptr
		comCall(w.webview, wvGetDocumentTitle, uintptr(unsafe.Pointer(&p)))
		w.h.TitleChanged(takeWstr(p))
	})
	add(wvAddWebMessageReceived, func(_, args uintptr) {
		var p uintptr
		if failed(comCall(args, msgTryGetWebMessageAsString, uintptr(unsafe.Pointer(&p)))) {
			return // not a string
		}
		if paths := postedFiles(args); len(paths) > 0 {
			w.dropped = paths
		}
		w.h.Message(takeWstr(p))
	})
	add(wvAddNewWindowRequested, w.newWindowRequested)
	add(wvAddPermissionRequested, w.permissionRequested)
	w.addAuthHandlers()
	w.addContextMenuHandler()
	if wv4 := queryInterface(w.webview, &iidICoreWebView2_4); wv4 != 0 {
		withHandler(w.downloadStarting, func(h uintptr) uintptr { return comCall(wv4, wv4AddDownloadStarting, h, uintptr(unsafe.Pointer(&tok))) })
		release(wv4)
	}
	if wv8 := queryInterface(w.webview, &iidICoreWebView2_8); wv8 != 0 {
		withHandler(func(_, _ uintptr) { w.h.AudibleChanged(w.IsAudible()) }, func(h uintptr) uintptr {
			return comCall(wv8, wv8AddIsDocumentPlayingAudioChanged, h, uintptr(unsafe.Pointer(&tok)))
		})
		withHandler(func(_, _ uintptr) { w.mutedChanged() }, func(h uintptr) uintptr {
			return comCall(wv8, wv8AddIsMutedChanged, h, uintptr(unsafe.Pointer(&tok)))
		})
		release(wv8)
		if w.muted { // muted before the web view was made
			w.SetAudioMuted(true)
		}
	}
	add(wvAddWindowCloseRequested, func(_, _ uintptr) { w.h.ClosedByPage() })
	add(wvAddProcessFailed, func(_, args uintptr) {
		var kind int32
		comCall(args, procFailedGetKind, uintptr(unsafe.Pointer(&kind)))
		switch kind {
		case 0:
			w.h.RenderProcessGone("browser-exited")
		case 1, 3:
			w.h.RenderProcessGone("crashed")
		case 2:
			w.h.RenderProcessGone("unresponsive")
		}
	})
	add(wvAddWebResourceRequested, w.resourceRequested)
	for _, scheme := range o.Schemes {
		w.addFilter("http://" + scheme + ".localhost/*")
		w.addFilter("http://*." + scheme + ".localhost/*") // its other hosts
	}
	w.filterAllRequests()
	withHandler(w.acceleratorKeyPressed, func(h uintptr) uintptr {
		return comCall(controller, ctlAddAcceleratorKeyPressed, h, uintptr(unsafe.Pointer(&tok)))
	})

	if w.host != nil {
		w.placeEmbedded()
		if l := w.host.surface.layer; l != nil {
			l.commit()
		}
	} else {
		comCall(controller, ctlPutIsVisible, 1)
		w.resizeWebView()
	}
	w.layoutControls() // above the webview's window
	if o.Zoom > 0 && o.Zoom != 1 {
		w.SetZoom(o.Zoom)
	}
	w.b.applyWebViewTheme(w)
	if w.IsFocused() {
		w.focusWebView()
	}

	if !w.b.holdForCookies(w) {
		w.becomeReady()
	}
}

// becomeReady runs the calls that waited for the web view, its first
// navigation among them.
func (w *window) becomeReady() {
	if w.closed {
		return
	}
	w.ready = true
	w.b.closeCookieView(w.b.cookiesOf(w.opts.DataStore))
	pending := w.pending
	w.pending = nil
	for i, c := range pending {
		if w.closed {
			// A call closed the window: the others never run.
			w.pending = pending[i:]
			w.failPending(errDestroyed)
			return
		}
		c.run()
	}
}

func (w *window) addFilter(pattern string) {
	comCall(w.webview, wvAddWebResourceRequestedFilter, uintptr(unsafe.Pointer(u16(pattern))), 0) // COREWEBVIEW2_WEB_RESOURCE_CONTEXT_ALL
}

func (w *window) resizeWebView() {
	if w.controller == 0 {
		return
	}
	var r rect
	procGetClientRect.Call(w.hwnd, uintptr(unsafe.Pointer(&r)))
	putBounds(w.controller, r)
}

func (w *window) focusWebView() {
	if w.controller != 0 {
		comCall(w.controller, ctlMoveFocus, 0) // COREWEBVIEW2_MOVE_FOCUS_REASON_PROGRAMMATIC
	}
}

func (w *window) navigationStarting(_, args uintptr) {
	var p uintptr
	comCall(args, navStartingGetURI, uintptr(unsafe.Pointer(&p)))
	uri := w.appURL(takeWstr(p))
	var user int32
	comCall(args, navStartingGetIsUserInitiated, uintptr(unsafe.Pointer(&user)))
	kind := navigationKind(args)
	nav := platform.Navigation{URL: uri, IsMainFrame: true, UserInitiated: user != 0, Unasked: kind == navReload || kind == navBackOrForward, Reload: kind == navReload}
	if w.programmatic {
		w.programmatic = false
		nav.Unasked = true
	}
	if !w.h.WillNavigate(nav) {
		comCall(args, navStartingPutCancel, 1)
		return
	}
	// LoadHTML serves its document to its navigation, which may request
	// it more than once, and not to later ones.
	if w.htmlNext {
		w.htmlNext = false
	} else {
		clear(w.htmlFor)
	}
	w.loading = true
	w.navigating = uri
	w.h.NavigationStarted(uri)
}

// COREWEBVIEW2_NAVIGATION_KIND
const (
	navReload = iota
	navBackOrForward
	navNewDocument
)

// navigationKind returns the COREWEBVIEW2_NAVIGATION_KIND of a navigation,
// or -1 on runtimes older than it.
func navigationKind(args uintptr) int32 {
	args3 := queryInterface(args, &iidICoreWebView2NavigationStartingEventArgs3)
	if args3 == 0 {
		return -1
	}
	defer release(args3)
	kind := int32(-1)
	comCall(args3, navStarting3GetNavigationKind, uintptr(unsafe.Pointer(&kind)))
	return kind
}

func (w *window) navigationCompleted(_, args uintptr) {
	w.loading = false
	var ok int32
	comCall(args, navCompletedGetIsSuccess, uintptr(unsafe.Pointer(&ok)))
	if ok != 0 {
		if w.reloadAdopted {
			w.reloadAdopted = false
			comCall(w.webview, wvReload)
			return
		}
		if zoomByScript && w.zoom != 1 {
			putZoom(w, w.zoom) // a new document starts unzoomed
		}
		w.h.LoadFinished()
		return
	}
	var status int32
	comCall(args, navCompletedGetWebErrorStatus, uintptr(unsafe.Pointer(&status)))
	if status == 14 { // COREWEBVIEW2_WEB_ERROR_STATUS_OPERATION_CANCELED: another navigation
		return
	}
	w.h.LoadFailed(w.URL(), int(status), "navigation failed (COREWEBVIEW2_WEB_ERROR_STATUS "+strconv.Itoa(int(status))+")")
}

// downloadStarting asks where to save a download, instead of WebView2's own
// download UI, and reports when it ended. What custom schemes serve comes
// from the app itself, so it is downloaded from there.
func (w *window) downloadStarting(_, args uintptr) {
	var op uintptr
	if failed(comCall(args, dlStartGetOperation, uintptr(unsafe.Pointer(&op)))) || op == 0 {
		return
	}
	defer release(op)
	var p uintptr
	comCall(op, dlOpGetURI, uintptr(unsafe.Pointer(&p)))
	uri := takeWstr(p)
	if app := w.appURL(uri); app != uri {
		comCall(args, dlStartPutCancel, 1)
		w.h.SchemeDownload(app)
		return
	}
	comCall(args, dlStartGetResultFilePath, uintptr(unsafe.Pointer(&p)))
	path := w.h.DownloadStarted(uri, filepath.Base(takeWstr(p)))
	comCall(args, dlStartPutHandled, 1) // no download UI of WebView2
	if path == "" {
		comCall(args, dlStartPutCancel, 1)
		return
	}
	os.Remove(path) // a file there is replaced
	comCall(args, dlStartPutResultFilePath, uintptr(unsafe.Pointer(u16(path))))
	var tok int64
	withHandler(func(sender, _ uintptr) {
		var state, reason int32
		comCall(sender, dlOpGetState, uintptr(unsafe.Pointer(&state)))
		switch state {
		case 1: // interrupted
			comCall(sender, dlOpGetInterruptReason, uintptr(unsafe.Pointer(&reason)))
			w.h.DownloadFinished(uri, path, fmt.Errorf("mygo: download interrupted (reason %d)", reason))
		case 2: // completed
			w.h.DownloadFinished(uri, path, nil)
		}
	}, func(h uintptr) uintptr { return comCall(op, dlOpAddStateChanged, h, uintptr(unsafe.Pointer(&tok))) })
}

// permissionRequested decides camera, microphone, location and notification
// requests; WebView2 asks the user about the others.
func (w *window) permissionRequested(_, args uintptr) {
	var kind int32
	comCall(args, permGetKind, uintptr(unsafe.Pointer(&kind)))
	name := map[int32]string{1: "microphone", 2: "camera", 3: "geolocation", 4: "notifications"}[kind]
	if name == "" {
		return
	}
	var p uintptr
	comCall(args, permGetURI, uintptr(unsafe.Pointer(&p)))
	origin := takeWstr(p)
	if u, err := url.Parse(w.appURL(origin)); err == nil {
		origin = (&url.URL{Scheme: u.Scheme, Host: u.Host}).String()
	}
	const allow, deny = 1, 2 // COREWEBVIEW2_PERMISSION_STATE
	state := deny
	if w.h.PermissionRequested([]string{name}, origin) {
		state = allow
	}
	comCall(args, permPutState, uintptr(state))
}

// postedFiles returns the paths of the File objects a page posted with
// chrome.webview.postMessageWithAdditionalObjects, as the bridge does with
// dropped files.
func postedFiles(args uintptr) []string {
	args2 := queryInterface(args, &iidICoreWebView2WebMessageReceivedEventArgs2)
	if args2 == 0 {
		return nil // an older runtime
	}
	defer release(args2)
	var objects uintptr
	if failed(comCall(args2, msg2GetAdditionalObjects, uintptr(unsafe.Pointer(&objects)))) || objects == 0 {
		return nil
	}
	defer release(objects)
	var n uint32
	comCall(objects, objectsGetCount, uintptr(unsafe.Pointer(&n)))
	var paths []string
	for i := range n {
		var obj uintptr
		if failed(comCall(objects, objectsGetValueAtIndex, uintptr(i), uintptr(unsafe.Pointer(&obj)))) || obj == 0 {
			continue
		}
		if file := queryInterface(obj, &iidICoreWebView2File); file != 0 {
			var p uintptr
			if !failed(comCall(file, fileGetPath, uintptr(unsafe.Pointer(&p)))) {
				if path := takeWstr(p); path != "" {
					paths = append(paths, path)
				}
			}
			release(file)
		}
		release(obj)
	}
	return paths
}

func (w *window) DroppedFiles() []string {
	paths := w.dropped
	w.dropped = nil
	return paths
}

func (w *window) newWindowRequested(_, args uintptr) {
	// WebView2 must not open windows of its own.
	comCall(args, newWinPutHandled, 1)
	var p uintptr
	comCall(args, newWinGetURI, uintptr(unsafe.Pointer(&p)))
	raw := takeWstr(p)
	var user int32
	comCall(args, newWinGetIsUserInitiated, uintptr(unsafe.Pointer(&user)))
	req := platform.NewWindowRequest{URL: w.appURL(raw), UserInitiated: user != 0}
	var features uintptr
	if comCall(args, newWinGetWindowFeatures, uintptr(unsafe.Pointer(&features))) == sOK && features != 0 {
		var hasSize, hasPos int32
		comCall(features, featGetHasSize, uintptr(unsafe.Pointer(&hasSize)))
		comCall(features, featGetHasPosition, uintptr(unsafe.Pointer(&hasPos)))
		var v uint32
		if hasSize != 0 {
			comCall(features, featGetWidth, uintptr(unsafe.Pointer(&v)))
			req.Width = int(v)
			comCall(features, featGetHeight, uintptr(unsafe.Pointer(&v)))
			req.Height = int(v)
		}
		if hasPos != 0 {
			comCall(features, featGetLeft, uintptr(unsafe.Pointer(&v)))
			req.X = int(v)
			comCall(features, featGetTop, uintptr(unsafe.Pointer(&v)))
			req.Y = int(v)
		}
		release(features)
	}
	child, ok := w.h.NewWindow(req).(*window)
	if !ok || child == nil {
		return // denied: window.open returns null
	}
	// The child's web view becomes the popup, keeping its relation to the
	// opener (window.opener, postMessage), as on macOS: WebView2 waits
	// for it (a deferral) and navigates it itself. It is handed over once
	// set up, its scripts and handlers in place, and before it navigates;
	// it shares the opener's environment, as every window does.
	//
	// WebView2 can't load a URL of the app's schemes, which window.open
	// gives it as http://<scheme>.localhost/ (openScript), but a link
	// still may: the child loads it as an independent page.
	var deferral uintptr
	if w.webURL(raw) != raw || failed(comCall(args, newWinGetDeferral, uintptr(unsafe.Pointer(&deferral)))) || deferral == 0 {
		child.LoadURL(req.URL) // an independent page, as on Linux
		return
	}
	addRef(args)
	done := func() {
		comCall(deferral, deferralComplete)
		release(deferral)
		release(args)
	}
	child.withWebViewOr(func() {
		if hr := comCall(args, newWinPutNewWindow, child.webview); failed(hr) {
			log.Print(hresultError("adopting the new window's web view", hr))
			done()
			child.LoadURL(req.URL)
			return
		}
		done()
		// WebView2 drops the chrome.webview messages the first document of
		// an adopted popup posts once it loaded, so the bridge's calls get
		// no reply; the popup's next document posts them all. A page of
		// the app, which uses the bridge, loads again (navigationCompleted).
		child.reloadAdopted = w.appURL(raw) != raw
	}, func(error) { done() }) // the child closed, or has no web view
}

// Accelerator keys reach the webview, not the window: run the menu items
// they belong to from here.
func (w *window) acceleratorKeyPressed(_, args uintptr) {
	w = w.top() // a web view's keys are its window's
	var kind int32
	comCall(args, accelGetKeyEventKind, uintptr(unsafe.Pointer(&kind)))
	if kind != 0 && kind != 2 { // KEY_DOWN, SYSTEM_KEY_DOWN
		return
	}
	var vk uint32
	comCall(args, accelGetVirtualKey, uintptr(unsafe.Pointer(&vk)))
	cmd, ok := w.pageAccelerator(accelKey{vk: vk, mods: currentModifiers()})
	if !ok {
		return
	}
	comCall(args, accelPutHandled, 1)
	w.b.menuCommand(cmd, w)
}

// pageAccelerator returns the menu command a key pressed in a page runs,
// if any. A page keeps the keys of the edit roles, as in a browser and in
// native UI: WebView2 cuts, copies and pastes with the page's own events
// and every format of the clipboard.
func (w *window) pageAccelerator(key accelKey) (uint16, bool) {
	cmd, ok := w.accels[key]
	if !ok {
		return 0, false
	}
	if e := w.b.menus.entries[cmd]; e != nil && platform.EditCommands[e.role] && pageEditKey(key) {
		return 0, false
	}
	return cmd, true
}

// pageEditKey reports a key WebView2 edits a page with itself, as Edge
// does. An edit role's item on another key, as Copy on Ctrl+Shift+C in a
// terminal, still runs from it: the page would do nothing with the key.
func pageEditKey(key accelKey) bool {
	const ctrl, shift = accelerator.Ctrl, accelerator.Shift
	switch key.mods {
	case ctrl:
		return key.vk == 'Z' || key.vk == 'Y' || key.vk == 'X' || key.vk == 'C' || key.vk == 'V' || key.vk == 'A'
	case ctrl | shift:
		return key.vk == 'Z' || key.vk == 'V'
	case 0:
		return key.vk == 0x2E // VK_DELETE
	}
	return false
}

func (w *window) LoadURL(raw string) {
	w.withWebView(func() {
		w.programmatic = true
		comCall(w.webview, wvNavigate, uintptr(unsafe.Pointer(u16(w.webURL(raw)))))
	})
}

func (w *window) LoadHTML(html, baseURL string) {
	w.withWebView(func() {
		w.programmatic = true
		if !servedAtBase(baseURL) {
			comCall(w.webview, wvNavigateToString, uintptr(unsafe.Pointer(u16(html))))
			return
		}
		// Serve the document at its base URL, so relative URLs, the origin
		// and custom schemes work as in the other backends.
		target := w.webURL(baseURL)
		w.htmlFor[target] = html
		w.htmlNext = true
		if w.appURL(target) == target { // not covered by the filters of the schemes
			w.addFilter(target)
		}
		comCall(w.webview, wvNavigate, uintptr(unsafe.Pointer(u16(target))))
	})
}

// servedAtBase reports whether LoadHTML serves its document at baseURL,
// answering the navigation's request for it. WebView2 requests no about:
// URL, about:blank among them, so their documents, as those without a base
// URL, go through NavigateToString, which shows them at about:blank.
func servedAtBase(baseURL string) bool {
	scheme, _, _ := strings.Cut(baseURL, ":")
	return baseURL != "" && !strings.EqualFold(scheme, "about")
}

func (w *window) LoadFile(path, _ string) {
	u := url.URL{Scheme: "file", Path: "/" + strings.ReplaceAll(path, `\`, "/")}
	w.LoadURL(u.String())
}

func (w *window) Reload(ignoreCache bool) {
	w.withWebView(func() {
		if ignoreCache {
			w.devtools("Page.reload", `{"ignoreCache":true}`, nil)
			return
		}
		comCall(w.webview, wvReload)
	})
}

func (w *window) StopLoading() { w.withWebView(func() { comCall(w.webview, wvStop) }) }
func (w *window) GoBack()      { w.withWebView(func() { comCall(w.webview, wvGoBack) }) }
func (w *window) GoForward()   { w.withWebView(func() { comCall(w.webview, wvGoForward) }) }

func (w *window) CanGoBack() bool    { return w.getBool(wvGetCanGoBack) }
func (w *window) CanGoForward() bool { return w.getBool(wvGetCanGoForward) }

func (w *window) getBool(index int) bool {
	if w.webview == 0 {
		return false
	}
	var v int32
	comCall(w.webview, index, uintptr(unsafe.Pointer(&v)))
	return v != 0
}

func (w *window) URL() string {
	if w.webview == 0 {
		return ""
	}
	var p uintptr
	comCall(w.webview, wvGetSource, uintptr(unsafe.Pointer(&p)))
	return w.appURL(takeWstr(p))
}

func (w *window) IsLoading() bool { return w.loading }

// addUserScripts adds the user scripts of the options to every document,
// and the title bar's after them, keeping their ids, which arrive later.
func (w *window) addUserScripts() {
	scripts := w.opts.UserScripts
	if w.caption != nil {
		// After the bridge, which it tells.
		scripts = append(scripts[:len(scripts):len(scripts)], platform.UserScript{Source: w.titleBarScript()})
	}
	if w.hidingScript != "" {
		scripts = append(scripts[:len(scripts):len(scripts)], platform.UserScript{Source: w.hidingScript, AllFrames: true})
	}
	if len(w.opts.Schemes) > 0 {
		scripts = append(scripts[:len(scripts):len(scripts)], platform.UserScript{Source: w.openScript(), AllFrames: true})
	}
	gen := w.scriptGen
	for _, us := range scripts {
		src := us.Source
		if us.AtDocumentEnd {
			src = "document.readyState===\"loading\"?document.addEventListener(\"DOMContentLoaded\",()=>{\n" + src + "\n},{once:true}):(()=>{\n" + src + "\n})();"
		}
		if !us.AllFrames {
			// Scripts run in every frame unless told otherwise.
			src = "if(window===window.top){\n" + src + "\n}"
		}
		withHandler(func(hr, id uintptr) {
			if failed(hr) || id == 0 || w.closed || w.webview == 0 {
				return
			}
			if s := wstr(id); w.scriptGen == gen {
				w.scriptIDs = append(w.scriptIDs, s)
			} else {
				// Replaced while it was being added.
				comCall(w.webview, wvRemoveScriptToExecuteOnDocumentCreated, uintptr(unsafe.Pointer(u16(s))))
			}
		}, func(h uintptr) uintptr {
			return comCall(w.webview, wvAddScriptToExecuteOnDocumentCreated, uintptr(unsafe.Pointer(u16(src))), h)
		})
	}
}

// SetUserScripts replaces the user scripts. Scripts run in the order they
// were added, so all of them are removed and added again.
func (w *window) SetUserScripts(scripts []platform.UserScript) {
	if w.closed {
		return
	}
	w.opts.UserScripts = scripts
	if w.webview == 0 {
		return // setUp adds them
	}
	w.scriptGen++
	for _, id := range w.scriptIDs {
		comCall(w.webview, wvRemoveScriptToExecuteOnDocumentCreated, uintptr(unsafe.Pointer(u16(id))))
	}
	w.scriptIDs = nil
	w.addUserScripts()
}

func (w *window) Eval(js string) {
	w.withWebView(func() {
		withHandler(func(uintptr, uintptr) {}, func(h uintptr) uintptr {
			return comCall(w.webview, wvExecuteScript, uintptr(unsafe.Pointer(u16(js))), h)
		})
	})
}

// CallAsyncFunction evaluates through the DevTools protocol, which awaits
// promises, reports syntax errors and ignores the page's CSP.
func (w *window) CallAsyncFunction(body string, cb func(string, error)) {
	params, _ := json.Marshal(map[string]any{
		"expression":    "(async () => {\n" + body + "\n})()",
		"awaitPromise":  true,
		"returnByValue": true,
	})
	w.withWebViewOr(func() {
		w.devtools("Runtime.evaluate", string(params), func(result string, err error) {
			if err != nil {
				cb("", err)
				return
			}
			var out struct {
				Result struct {
					Value json.RawMessage `json:"value"`
				} `json:"result"`
				ExceptionDetails *struct {
					Text      string `json:"text"`
					Exception *struct {
						Description string `json:"description"`
					} `json:"exception"`
				} `json:"exceptionDetails"`
			}
			if err := json.Unmarshal([]byte(result), &out); err != nil {
				cb("", err)
				return
			}
			if d := out.ExceptionDetails; d != nil {
				msg := d.Text
				if d.Exception != nil && d.Exception.Description != "" {
					msg = d.Exception.Description
				}
				cb("", errors.New(msg))
				return
			}
			var s string
			_ = json.Unmarshal(out.Result.Value, &s)
			cb(s, nil)
		})
	}, func(err error) { cb("", err) })
}

// devtools calls a DevTools protocol method; done may be nil.
func (w *window) devtools(method, params string, done func(string, error)) {
	id := 0
	if done != nil {
		w.nextCall++
		id = w.nextCall
		w.calls[id] = done
	}
	hr := withHandler(func(hr, result uintptr) {
		cb := w.calls[id]
		delete(w.calls, id)
		if cb == nil {
			return
		}
		if failed(hr) {
			cb("", hresultError(method, hr))
			return
		}
		cb(wstr(result), nil)
	}, func(h uintptr) uintptr {
		return comCall(w.webview, wvCallDevToolsProtocolMethod, uintptr(unsafe.Pointer(u16(method))), uintptr(unsafe.Pointer(u16(params))), h)
	})
	if failed(hr) {
		if cb := w.calls[id]; cb != nil {
			delete(w.calls, id)
			cb("", hresultError(method, hr))
		}
	}
}

func (w *window) SetZoom(f float64) {
	w.zoom = f
	w.withWebView(func() { putZoom(w, f) })
}

func (w *window) Zoom() float64 { return w.zoom }

// setUpUserAgent applies UserAgent and UserAgentSuffix, which appends to
// WebView2's own user agent when it is all that is set.
func (w *window) setUpUserAgent() {
	s2 := queryInterface(w.settings, &iidICoreWebView2Settings2)
	if s2 == 0 {
		return
	}
	defer release(s2)
	ua := w.opts.UserAgent
	if suffix := w.opts.UserAgentSuffix; suffix != "" {
		if ua == "" {
			ua = userAgentOf(s2)
		}
		if ua != "" {
			ua += " " + suffix
		}
	}
	if ua != "" {
		comCall(s2, set2PutUserAgent, uintptr(unsafe.Pointer(u16(ua))))
	}
}

// userAgentOf returns the user agent Settings2 sets.
func userAgentOf(s2 uintptr) string {
	var p uintptr
	comCall(s2, set2GetUserAgent, uintptr(unsafe.Pointer(&p)))
	return takeWstr(p)
}

// setUserAgent sets the user agent the page sends, which WebView2 decides for
// an empty one. The suffix the options set stays on the end of it.
func (w *window) setUserAgent(ua string) {
	s2 := queryInterface(w.settings, &iidICoreWebView2Settings2)
	if s2 == 0 {
		return
	}
	defer release(s2)
	suffix := w.opts.UserAgentSuffix
	if ua != "" && suffix != "" {
		ua += " " + suffix
	}
	comCall(s2, set2PutUserAgent, uintptr(unsafe.Pointer(u16(ua))))
	if ua == "" && suffix != "" {
		// Whether WebView2 kept the user agent or went back to its own,
		// the suffix stays on the end of it.
		if cur := userAgentOf(s2); cur != "" && !strings.HasSuffix(cur, " "+suffix) {
			comCall(s2, set2PutUserAgent, uintptr(unsafe.Pointer(u16(cur+" "+suffix))))
		}
	}
}

func (w *window) SetUserAgent(ua string) { w.withWebView(func() { w.setUserAgent(ua) }) }

func (w *window) UserAgent() string {
	if w.settings == 0 {
		return w.opts.UserAgent
	}
	s2 := queryInterface(w.settings, &iidICoreWebView2Settings2)
	if s2 == 0 {
		return ""
	}
	defer release(s2)
	return userAgentOf(s2)
}

// ProcessID returns the browser process of the web view
// (ICoreWebView2::get_BrowserProcessId), which every web view of the
// app's environment shares, 0 before the web view is made.
func (w *window) ProcessID() int {
	if w.webview == 0 {
		return 0
	}
	var pid uint32
	if r := comCall(w.webview, wvGetBrowserProcessId, uintptr(unsafe.Pointer(&pid))); failed(r) {
		return 0
	}
	return int(pid)
}

// GPUProcessID returns the GPU process of the web view's environment,
// which its web views share, from ICoreWebView2Environment8's
// GetProcessInfos; 0 before the web view is made, on runtimes older than
// it, and while the environment runs no GPU process.
func (w *window) GPUProcessID() int {
	if w.webview == 0 {
		return 0
	}
	env8 := queryInterface(w.b.env, &iidICoreWebView2Environment8)
	if env8 == 0 {
		return 0
	}
	defer release(env8)
	var infos uintptr
	if failed(comCall(env8, env8GetProcessInfos, uintptr(unsafe.Pointer(&infos)))) || infos == 0 {
		return 0
	}
	defer release(infos)
	var n uint32
	if failed(comCall(infos, procInfosGetCount, uintptr(unsafe.Pointer(&n)))) {
		return 0
	}
	for i := range n {
		var info uintptr
		if failed(comCall(infos, procInfosGetValueAtIndex, uintptr(i), uintptr(unsafe.Pointer(&info)))) || info == 0 {
			continue
		}
		var kind, pid int32
		ok := !failed(comCall(info, procInfoGetKind, uintptr(unsafe.Pointer(&kind)))) && kind == processKindGPU &&
			!failed(comCall(info, procInfoGetProcessId, uintptr(unsafe.Pointer(&pid))))
		release(info)
		if ok {
			return int(pid)
		}
	}
	return 0
}

// webView8 returns the web view's ICoreWebView2_8, which the caller
// releases, or 0 before the web view is made and on runtimes older than
// it.
func (w *window) webView8() uintptr {
	return queryInterface(w.webview, &iidICoreWebView2_8)
}

// getBool8 returns a BOOL property of ICoreWebView2_8, false without it.
func (w *window) getBool8(index int) bool {
	wv8 := w.webView8()
	if wv8 == 0 {
		return false
	}
	defer release(wv8)
	var v int32
	if failed(comCall(wv8, index, uintptr(unsafe.Pointer(&v)))) {
		return false
	}
	return v != 0
}

// IsAudible returns ICoreWebView2_8's IsDocumentPlayingAudio, which
// IsDocumentPlayingAudioChanged (setUp) tells the changes of.
func (w *window) IsAudible() bool { return w.getBool8(wv8GetIsDocumentPlayingAudio) }

// SetAudioMuted puts ICoreWebView2_8's IsMuted, or keeps it for setUp
// until the web view is made. A runtime older than ICoreWebView2_8 can't
// mute.
//
// WebView2 may lose a put that follows another before IsMutedChanged told
// the first, or apply them out of order, and the page then plays muted or
// stays silent unmuted. So one put is in flight at a time: w.muted is the
// state wanted, and mutedChanged puts it again once the runtime told its
// change, if it differs. A put whose IsMutedChanged never comes (none
// does for no change) holds the next one back for mutePutTimeout only.
func (w *window) SetAudioMuted(v bool) {
	w.muted = v
	if w.mutePut.IsZero() || time.Since(w.mutePut) >= mutePutTimeout {
		w.putMuted()
	}
}

// mutePutTimeout is how long a put of IsMuted holds the next back while
// its IsMutedChanged doesn't come.
const mutePutTimeout = time.Second

// putMuted puts w.muted, and holds the next put back until IsMutedChanged
// tells this one landed when it changes what the web view reports. A put
// of what it reports already changes nothing, and none comes for it.
func (w *window) putMuted() {
	w.mutePut = time.Time{}
	wv8 := w.webView8()
	if wv8 == 0 {
		return
	}
	defer release(wv8)
	var cur int32
	if failed(comCall(wv8, wv8GetIsMuted, uintptr(unsafe.Pointer(&cur)))) || (cur != 0) != w.muted {
		w.mutePut = time.Now()
	}
	comCall(wv8, wv8PutIsMuted, boolArg(w.muted))
}

// mutedChanged runs on IsMutedChanged: the put in flight landed, and the
// state wanted since is put if the web view reports another.
func (w *window) mutedChanged() {
	w.mutePut = time.Time{}
	if wv8 := w.webView8(); wv8 != 0 {
		var cur int32
		bad := failed(comCall(wv8, wv8GetIsMuted, uintptr(unsafe.Pointer(&cur))))
		release(wv8)
		if bad || (cur != 0) != w.muted {
			w.putMuted()
		}
	}
}

// IsAudioMuted returns ICoreWebView2_8's IsMuted, or what SetAudioMuted
// keeps until the web view is made, unless it never will be.
func (w *window) IsAudioMuted() bool {
	if w.webview == 0 {
		return w.muted && w.webViewErr == nil
	}
	return w.getBool8(wv8GetIsMuted)
}

// CaptureState is always CaptureNone: WebView2 tells nothing of a page's
// capture of the camera, the microphone or the display.
func (w *window) CaptureState(platform.CaptureKind) platform.CaptureState {
	return platform.CaptureNone
}

// SetCaptureState does nothing: WebView2 can't mute or stop a page's
// capture.
func (w *window) SetCaptureState(platform.CaptureKind, platform.CaptureState) {}

// TogglePictureInPicture runs platform.TogglePictureInPicture through the
// DevTools protocol as a user's gesture, which
// requestPictureInPicture() needs.
func (w *window) TogglePictureInPicture() {
	params, _ := json.Marshal(map[string]any{"expression": platform.TogglePictureInPicture, "userGesture": true})
	w.withWebView(func() { w.devtools("Runtime.evaluate", string(params), nil) })
}

// PictureInPictureActive asks the page for its
// document.pictureInPictureElement; false before the web view is made.
func (w *window) PictureInPictureActive(cb func(bool)) {
	if !w.ready || w.closed {
		cb(false)
		return
	}
	w.CallAsyncFunction(platform.PictureInPictureActive, func(s string, err error) {
		cb(err == nil && s == "true")
	})
}

// SessionState is nil: WebView2 has no API for the back/forward list,
// so the core keeps the page's URL alone.
func (w *window) SessionState() []byte { return nil }

// RestoreSessionState can't, for the core to load the page's URL.
func (w *window) RestoreSessionState([]byte) bool { return false }

func (w *window) OpenDevTools() {
	if !w.opts.DevTools {
		return
	}
	w.withWebView(func() {
		comCall(w.webview, wvOpenDevToolsWindow)
		w.devTools = true
	})
}

// CloseDevTools has no WebView2 counterpart; the user closes them.
func (w *window) CloseDevTools()         {}
func (w *window) IsDevToolsOpened() bool { return w.devTools }

func (w *window) CapturePage(cb func([]byte, error)) {
	w.withWebViewOr(func() {
		var stream uintptr
		if r, _, _ := procCreateStreamOnHGlobal.Call(0, 1, uintptr(unsafe.Pointer(&stream))); failed(r) {
			cb(nil, hresultError("CreateStreamOnHGlobal", r))
			return
		}
		w.nextCall++
		id := w.nextCall
		w.calls[id] = func(string, error) { cb(nil, errDestroyed) }
		hr := withHandler(func(hr, _ uintptr) {
			if w.calls[id] == nil {
				return // the window closed
			}
			delete(w.calls, id)
			defer release(stream)
			if failed(hr) {
				cb(nil, hresultError("CapturePreview", hr))
				return
			}
			cb(hglobalBytes(stream), nil)
		}, func(h uintptr) uintptr {
			return comCall(w.webview, wvCapturePreview, 0, stream, h) // COREWEBVIEW2_CAPTURE_PREVIEW_IMAGE_FORMAT_PNG
		})
		if failed(hr) {
			delete(w.calls, id)
			release(stream)
			cb(nil, hresultError("CapturePreview", hr))
		}
	}, func(err error) { cb(nil, err) })
}

// hglobalBytes copies the memory behind a stream made by
// CreateStreamOnHGlobal.
func hglobalBytes(stream uintptr) []byte {
	var mem uintptr
	if r, _, _ := procGetHGlobalFromStream.Call(stream, uintptr(unsafe.Pointer(&mem))); failed(r) {
		return nil
	}
	size, _, _ := procGlobalSize.Call(mem)
	p, _, _ := procGlobalLock.Call(mem)
	if p == 0 {
		return nil
	}
	defer procGlobalUnlock.Call(mem)
	return bytes.Clone(unsafe.Slice((*byte)(native(p)), size))
}

func (w *window) Print() { w.Eval("window.print()") }

// PrintToPDF uses the DevTools protocol, like Chromium's headless mode.
func (w *window) PrintToPDF(o platform.PDFOptions, cb func([]byte, error)) {
	params, _ := json.Marshal(map[string]any{
		"landscape":         o.Landscape,
		"printBackground":   o.Background,
		"paperWidth":        o.PageWidth,
		"paperHeight":       o.PageHeight,
		"marginTop":         o.MarginTop,
		"marginRight":       o.MarginRight,
		"marginBottom":      o.MarginBottom,
		"marginLeft":        o.MarginLeft,
		"preferCSSPageSize": false,
	})
	w.withWebViewOr(func() {
		w.devtools("Page.printToPDF", string(params), func(res string, err error) {
			if err != nil {
				cb(nil, fmt.Errorf("mygo: printing to PDF: %w", err))
				return
			}
			var r struct {
				Data string `json:"data"`
			}
			if err := json.Unmarshal([]byte(res), &r); err != nil {
				cb(nil, err)
				return
			}
			pdf, err := base64.StdEncoding.DecodeString(r.Data)
			cb(pdf, err)
		})
	}, func(err error) { cb(nil, err) })
}

// Custom scheme requests.

func (w *window) resourceRequested(_, args uintptr) {
	var req uintptr
	if comCall(args, resReqGetRequest, uintptr(unsafe.Pointer(&req))) != sOK || req == 0 {
		return
	}
	defer release(req)
	var p uintptr
	comCall(req, reqGetURI, uintptr(unsafe.Pointer(&p)))
	uri := takeWstr(p)
	var resource int32
	comCall(args, resReqGetResourceContext, uintptr(unsafe.Pointer(&resource)))
	const document = 1 // COREWEBVIEW2_WEB_RESOURCE_CONTEXT_DOCUMENT
	if w.contentFilter != nil && w.contentFilter(args, req, uri, resource) {
		return
	}

	if html, ok := w.htmlFor[uri]; ok && resource == document {
		w.respond(args, 0, http.StatusOK, http.Header{"Content-Type": {"text/html; charset=utf-8"}}, []byte(html))
		return
	}
	target := w.appURL(uri)
	if target == uri {
		return // not ours
	}
	comCall(req, reqGetMethod, uintptr(unsafe.Pointer(&p)))
	sreq := &platform.SchemeRequest{Method: takeWstr(p), URL: target, Header: requestHeaders(req)}
	var content uintptr
	if comCall(req, reqGetContent, uintptr(unsafe.Pointer(&content))) == sOK && content != 0 {
		sreq.Body = io.NopCloser(bytes.NewReader(readStream(content)))
		release(content)
	}
	var deferral uintptr
	if comCall(args, resReqGetDeferral, uintptr(unsafe.Pointer(&deferral))) != sOK {
		return
	}
	addRef(args)
	ctx, cancel := context.WithCancel(context.Background())
	sreq.Context = ctx
	sreq.Responder = &schemeResponse{w: w, args: args, deferral: deferral, cancel: cancel, url: target, document: resource == document}
	w.h.SchemeRequest(sreq)
}

func requestHeaders(req uintptr) http.Header {
	h := http.Header{}
	var headers, iter uintptr
	if comCall(req, reqGetHeaders, uintptr(unsafe.Pointer(&headers))) != sOK || headers == 0 {
		return h
	}
	defer release(headers)
	if comCall(headers, headersGetIterator, uintptr(unsafe.Pointer(&iter))) != sOK || iter == 0 {
		return h
	}
	defer release(iter)
	for {
		var has int32
		comCall(iter, iterGetHasCurrentHeader, uintptr(unsafe.Pointer(&has)))
		if has == 0 {
			return h
		}
		var name, value uintptr
		comCall(iter, iterGetCurrentHeader, uintptr(unsafe.Pointer(&name)), uintptr(unsafe.Pointer(&value)))
		h.Add(takeWstr(name), takeWstr(value))
		var more int32
		comCall(iter, iterMoveNext, uintptr(unsafe.Pointer(&more)))
		if more == 0 {
			return h
		}
	}
}

// respond answers a WebResourceRequested event; deferral is 0 for a
// synchronous answer.
func (w *window) respond(args, deferral uintptr, status int, header http.Header, body []byte) {
	var raw strings.Builder
	for k, vs := range header {
		for _, v := range vs {
			raw.WriteString(k + ": " + v + "\r\n")
		}
	}
	stream := memStream(body)
	var resp uintptr
	hr := comCall(w.b.env, envCreateWebResourceResponse, stream, uintptr(status),
		uintptr(unsafe.Pointer(u16(http.StatusText(status)))), uintptr(unsafe.Pointer(u16(raw.String()))), uintptr(unsafe.Pointer(&resp)))
	release(stream)
	if !failed(hr) && resp != 0 {
		comCall(args, resReqPutResponse, resp)
		release(resp)
	}
	if deferral != 0 {
		comCall(deferral, deferralComplete)
	}
}

// schemeResponse buffers a response: WebView2 wants it whole.
type schemeResponse struct {
	w        *window
	args     uintptr
	deferral uintptr
	cancel   context.CancelFunc
	url      string
	document bool // a page loads, rather than a resource of one
	status   int
	header   http.Header
	body     bytes.Buffer
	done     bool
}

func (r *schemeResponse) Respond(status int, header map[string][]string) {
	r.status, r.header = status, header
}

func (r *schemeResponse) Write(p []byte) { r.body.Write(p) }

// isDownload reports whether a page response is a file to save: an
// attachment, or plain bytes.
func isDownload(h http.Header) bool {
	disposition := strings.ToLower(strings.TrimSpace(h.Get("Content-Disposition")))
	return strings.HasPrefix(disposition, "attachment") || strings.HasPrefix(h.Get("Content-Type"), "application/octet-stream")
}

func (r *schemeResponse) Finish() {
	if r.done {
		return
	}
	r.done = true
	if r.status == 0 {
		r.status = http.StatusOK
	}
	if r.document && r.status < 300 && isDownload(r.header) && !r.w.closed {
		// WebView2 does not download what it gets from here: the page
		// stays, and the app serves the URL again into a download.
		r.w.respond(r.args, r.deferral, http.StatusNoContent, nil, nil)
		r.w.h.SchemeDownload(r.url)
		r.release()
		return
	}
	if !r.w.closed {
		r.w.respond(r.args, r.deferral, r.status, r.header, r.body.Bytes())
	} else {
		comCall(r.deferral, deferralComplete)
	}
	r.release()
}

func (r *schemeResponse) Fail(err error) {
	if r.done {
		return
	}
	r.done = true
	if !r.w.closed {
		r.w.respond(r.args, r.deferral, http.StatusBadGateway, http.Header{"Content-Type": {"text/plain; charset=utf-8"}}, []byte(err.Error()))
	} else {
		comCall(r.deferral, deferralComplete)
	}
	r.release()
}

func (r *schemeResponse) release() {
	r.cancel()
	release(r.deferral)
	release(r.args)
}

// openScript makes window.open give WebView2 the URLs of the app's schemes
// as the http://[<host>.]<scheme>.localhost/ URLs that serve them (webURL), which
// the popup can load as it opens, keeping its opener (newWindowRequested).
func (w *window) openScript() string {
	schemes, _ := json.Marshal(w.opts.Schemes)
	return `(() => {
	const schemes = ` + string(schemes) + `, open = window.open;
	if (typeof open !== "function") return;
	window.open = function (url, ...rest) {
		try {
			const u = new URL(String(url), location.href), scheme = u.protocol.slice(0, -1);
			const host = u.hostname.toLowerCase();
			if (!u.port && !u.username && !u.password && schemes.includes(scheme) && (host === "localhost" || /^(?=.{1,253}$)[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$/.test(host))) {
				url = "http://" + (host === "localhost" ? "" : host + ".") + scheme + ".localhost" + u.pathname + u.search + u.hash;
			}
		} catch {}
		return open.call(this, url, ...rest);
	};
})();`
}
