package mygo

import (
	"image/color"

	"github.com/egoist/mygo/internal/platform"
)

// Page is the web page a window shows: what it loads, the scripts it runs,
// its developer tools and its events. A window showing native UI
// (WindowOptions.Content) has none.
//
//	win := mygo.NewWindow(mygo.WindowOptions{Title: "Editor", URL: "/"})
//	win.Page().OnDOMReady(func() { log.Println("ready") })
//
// Like the window's, its methods are safe from any goroutine.
type Page struct{ w *Window }

// Page returns the web page the window shows, or nil for a window that
// shows native UI.
func (w *Window) Page() *Page {
	if w.content {
		return nil
	}
	return w.pg
}

// Window returns the window that shows the page: for the page of a web
// view, the window the web view is in.
func (p *Page) Window() *Window { return p.w.top() }

// WebView returns the web view whose page this is, nil for the page of a
// window.
func (p *Page) WebView() *WebView { return p.w.webView }

// ThemeColor returns the page's theme color, and false when it has none:
// the content of the first <meta name="theme-color"> in the page's head
// whose media attribute is absent or matches, and whose content is a CSS
// color, as browsers pick it. A page with one for light and one for dark
// appearance,
//
//	<meta name="theme-color" content="#fff" media="(prefers-color-scheme: light)">
//	<meta name="theme-color" content="#111" media="(prefers-color-scheme: dark)">
//
// has the one of the app's appearance (Theme). The color is 8-bit sRGB
// with straight alpha, as the page's engine computes it, whatever the CSS
// syntax: ui.RGB(c.R, c.G, c.B) makes it a ui.Color.
//
// MyGo's runtime in the page tells it, on every platform alike, in pages of
// any origin, trusted or not; a page it doesn't run in, such as an image
// or a PDF, has none. A page that just started loading still has the color
// of the one before until its DOM is ready, so that going between pages of
// the same color changes nothing.
func (p *Page) ThemeColor() (c color.NRGBA, ok bool) {
	onMain(func() {
		if p.w.native != nil && p.w.hasThemeColor {
			c, ok = nrgba(p.w.themeColor), true
		}
	})
	return c, ok
}

// OnThemeColorChanged is called when the page's theme color (ThemeColor)
// changed, with ok false when it has none any more: its meta was added,
// removed or changed, the app's appearance changed and another meta's
// media matches, or another page has another color. It is called on the
// main thread, as other events are, and only when the color changed.
//
//	page.OnThemeColorChanged(func(c color.NRGBA, ok bool) {
//		if ok {
//			toolbar.SetBackground(ui.RGB(c.R, c.G, c.B))
//		}
//	})
func (p *Page) OnThemeColorChanged(fn func(c color.NRGBA, ok bool)) (off func()) {
	return p.w.onThemeColorChanged.add(fn, false)
}

// setThemeColor sets the page's theme color, as its bridge told it, and
// tells the listeners if it changed. Main thread only.
func (w *Window) setThemeColor(c platform.Color, ok bool) {
	w.themeColorPending = false
	if !ok {
		c = platform.Color{}
	}
	if ok == w.hasThemeColor && c == w.themeColor {
		return
	}
	w.themeColor, w.hasThemeColor = c, ok
	for _, fn := range w.onThemeColorChanged.snapshot() {
		fn(nrgba(c), ok)
	}
}

// themeColorSettled runs once the page loaded, or failed to: a page whose
// bridge did not tell its theme color since its navigation committed has
// none. Main thread only.
func (w *Window) themeColorSettled() {
	if w.themeColorPending {
		w.setThemeColor(platform.Color{}, false)
	}
}

// IsAudible reports whether the page is making sound: a video or audio
// element playing, Web Audio, from any of its frames, those of other
// sites included. Like a browser tab's speaker icon, it can stay true
// while the page is muted (SetAudioMuted); the platform decides when a
// paused or silent source stops counting. It is false for a window
// showing native UI and after the window closed, and where the platform
// can't tell:
//
//   - macOS: WebKit's private -[WKWebView _isPlayingAudio], looked up with
//     respondsToSelector:; false when WebKit doesn't answer it.
//   - Linux: webkit_web_view_is_playing_audio.
//   - Windows: ICoreWebView2_8's IsDocumentPlayingAudio; false on WebView2
//     runtimes older than it.
func (p *Page) IsAudible() bool { return pageGet(p.w, platform.Window.IsAudible) }

// OnAudibleChanged is called when the page starts or stops making sound
// (IsAudible), on the main thread, as other events are: to show a speaker
// on a tab or offer to mute a window. It is never called where IsAudible
// is always false.
//
//	page.OnAudibleChanged(func(audible bool) {
//		tab.SetSpeakerVisible(audible)
//	})
func (p *Page) OnAudibleChanged(fn func(audible bool)) (off func()) {
	return p.w.onAudibleChanged.add(fn, false)
}

// SetAudioMuted mutes all the page's sound, or lets it play again, as a
// browser mutes a tab: the page's own media elements, Web Audio and its
// frames of other sites, without the page seeing its elements muted. It
// lasts as the page navigates, until it is set again, and a page muted
// before its web view is ready starts muted. It does nothing for a window
// showing native UI and where the platform can't mute:
//
//   - macOS: WebKit's private -[WKWebView _setPageMuted:], as Safari mutes a
//     tab, looked up with respondsToSelector:; nothing when WebKit doesn't
//     answer it.
//   - Linux: webkit_web_view_set_is_muted, WebKitGTK 2.30 or later.
//   - Windows: ICoreWebView2_8's IsMuted; nothing on WebView2 runtimes
//     older than it.
func (p *Page) SetAudioMuted(muted bool) {
	p.w.page(func(n platform.Window) { n.SetAudioMuted(muted) })
}

// IsAudioMuted reports whether the page is muted (SetAudioMuted): false
// where the platform can't mute, for a window showing native UI and after
// the window closed.
func (p *Page) IsAudioMuted() bool { return pageGet(p.w, platform.Window.IsAudioMuted) }

// CaptureKind is a kind of device a page captures with the camera and
// microphone APIs of the web: getUserMedia() and getDisplayMedia().
type CaptureKind int

const (
	CaptureCamera     = CaptureKind(platform.CaptureCamera)
	CaptureMicrophone = CaptureKind(platform.CaptureMicrophone)
	// CaptureDisplay is the capture of a screen or a window, which a page
	// starts with getDisplayMedia().
	CaptureDisplay = CaptureKind(platform.CaptureDisplay)
)

// CaptureState is whether a page captures a kind of device (CaptureKind).
type CaptureState int

const (
	// CaptureNone is the zero value: the page captures no such device.
	CaptureNone = CaptureState(platform.CaptureNone)
	// CaptureActive is a capture going on: the page gets the device's
	// sound or picture.
	CaptureActive = CaptureState(platform.CaptureActive)
	// CaptureMuted is a capture that is muted, by SetCaptureState or the
	// system: the page keeps its tracks and gets silence or black frames.
	CaptureMuted = CaptureState(platform.CaptureMuted)
)

// CaptureState reports whether the page captures the camera, the
// microphone or the display (kind), from any of its frames, and whether
// the capture is muted, as Safari's recording indicators show: to show
// one in the app's window or menu bar. The page asks for the devices
// first, which SetPermissionHandler decides. It is CaptureNone for an
// unknown kind, for a window showing native UI, after the window closed
// and where the platform can't tell:
//
//   - macOS: WKWebView's cameraCaptureState and microphoneCaptureState
//     (macOS 12+), and WebKit's private _displayCaptureState (macOS 13+),
//     looked up with respondsToSelector:; CaptureNone where WebKit doesn't
//     answer them.
//   - Linux: webkit_web_view_get_camera_capture_state and its microphone
//     and display siblings, WebKitGTK 2.34 or later.
//   - Windows: always CaptureNone, WebView2 tells no capture.
func (p *Page) CaptureState(kind CaptureKind) CaptureState {
	if !kind.valid() {
		return CaptureNone
	}
	return CaptureState(pageGet(p.w, func(n platform.Window) platform.CaptureState {
		return n.CaptureState(platform.CaptureKind(kind))
	}))
}

// OnCaptureStateChanged is called when the page starts or stops capturing
// a kind of device, or its capture is muted or unmuted (CaptureState), on
// the main thread, as other events are. It is never called where
// CaptureState is always CaptureNone.
//
//	page.OnCaptureStateChanged(func(kind mygo.CaptureKind, s mygo.CaptureState) {
//		if kind == mygo.CaptureMicrophone {
//			micItem.SetChecked(s == mygo.CaptureMuted)
//		}
//	})
func (p *Page) OnCaptureStateChanged(fn func(kind CaptureKind, state CaptureState)) (off func()) {
	return p.w.onCaptureStateChanged.add(fn, false)
}

// SetCaptureState mutes the page's capture of a kind of device
// (CaptureMuted), unmutes it (CaptureActive), or stops it (CaptureNone),
// as the mute button of a call app, without the page's help: a muted
// capture keeps the page's tracks, which get silence or black frames, and
// a stopped one ends them. The page can't unmute a capture itself: asking
// for the device again goes to SetPermissionHandler, as a new capture
// does. It does nothing while the page captures no such device, for an
// unknown kind or state, for a window showing native UI and where the
// platform can't. The change shows in CaptureState and
// OnCaptureStateChanged once the page took it, a moment later:
//
//   - macOS: WKWebView's setCameraCaptureState:completionHandler: and
//     setMicrophoneCaptureState:completionHandler: (macOS 12+), and
//     WebKit's private _setDisplayCaptureState:completionHandler:
//     (macOS 13+), looked up with respondsToSelector:; nothing where
//     WebKit doesn't answer them. WebKit's mute of the page's sound
//     (SetAudioMuted) mutes the camera and the microphone together, so
//     while only one of them is muted, it mutes the other for a moment.
//   - Linux: webkit_web_view_set_camera_capture_state and its microphone
//     and display siblings, WebKitGTK 2.34 or later. WebKitGTK's mute of
//     the page's sound (SetAudioMuted) unmutes every capture, which MyGo
//     mutes again at once: a muted capture may be live for a moment as
//     the page's sound is muted or unmuted.
//   - Windows: nothing, WebView2 can't.
func (p *Page) SetCaptureState(kind CaptureKind, state CaptureState) {
	if !kind.valid() || state < CaptureNone || state > CaptureMuted {
		return
	}
	p.w.page(func(n platform.Window) {
		n.SetCaptureState(platform.CaptureKind(kind), platform.CaptureState(state))
	})
}

func (k CaptureKind) valid() bool { return k >= CaptureCamera && k <= CaptureDisplay }

// TogglePictureInPicture moves a video of the page into picture in
// picture, a small window floating over every app's, or back into the
// page when one is out, as the button of a video's controls does, for a
// menu item or a shortcut of the app; the page needs no click. Which video
// is the platform's choice, and it does nothing when the page has none it
// can move, for a window showing native UI and after the window closed:
//
//   - macOS: WebKit's private -[WKWebView _togglePictureInPicture], for
//     the video WebKit picks for its media controls, when
//     _canTogglePictureInPicture says it can (a Mac with a Touch Bar),
//     both looked up with respondsToSelector:, else the page's script,
//     as on Windows; nothing in a page without
//     PageOptions.PictureInPicture.
//   - Windows: requestPictureInPicture() of the video playing, else of the
//     largest, in the page's document, not its frames, as a user's
//     gesture; document.exitPictureInPicture() for one out.
//   - Linux: nothing, WebKitGTK has no picture in picture.
func (p *Page) TogglePictureInPicture() { p.w.page(platform.Window.TogglePictureInPicture) }

// IsPictureInPictureActive reports whether a video of the page is in
// picture in picture, whoever put it there: TogglePictureInPicture, the
// video's controls or the page's script. It is false for a window showing
// native UI, after the window closed and where the platform can't tell:
//
//   - macOS: WebKit's private -[WKWebView _isPictureInPictureActive],
//     looked up with respondsToSelector:, which knows of the media
//     controls' picture in picture, else, in a page with
//     PageOptions.PictureInPicture, its document.pictureInPictureElement,
//     as on Windows.
//   - Windows: the page's document.pictureInPictureElement, which a video
//     of one of its frames leaves null; it asks the page, and waits for
//     its answer.
//   - Linux: always false.
func (p *Page) IsPictureInPictureActive() bool {
	ch := make(chan bool, 1)
	asked := pageGet(p.w, func(n platform.Window) bool {
		n.PictureInPictureActive(func(active bool) { deliver(ch, active) })
		return true
	})
	return asked && await(ch)
}

func nrgba(c platform.Color) color.NRGBA { return color.NRGBA{R: c.R, G: c.G, B: c.B, A: c.A} }

// PageOptions configure the web page of a window.
type PageOptions struct {
	// PreloadScript is JavaScript injected into every page before the
	// page's own scripts, after window.mygo is available.
	PreloadScript string
	// PreloadAllFrames runs PreloadScript in the page's iframes too, of
	// every origin, as they start; window.mygo isn't there. A browser
	// sees with it what a player embedded from another site loads.
	PreloadAllFrames bool
	// TrustedOrigins lists extra origins, such as "https://example.com",
	// whose pages may call bound Go methods. By default only the app's own
	// content can: custom schemes registered with Protocol, file: and
	// about: pages, and loopback dev servers during development. "*"
	// trusts every origin.
	TrustedOrigins []string
	// DevTools controls the web inspector, and for a window showing
	// Content, the inspector of its native UI.
	DevTools DevTools
	// ZoomFactor of the page (default 1).
	ZoomFactor float64
	// UserAgent overrides the user agent string.
	UserAgent string
	// SafariTabFocus leaves Tab to text fields and elements with a
	// tabindex, as Safari does while the system's keyboard navigation
	// (System Settings › Keyboard › Keyboard navigation) is off (macOS).
	// By default Tab reaches links, buttons, checkboxes and selects too,
	// as it does in Chrome, Electron and WebView2.
	SafariTabFocus bool
	// PictureInPicture lets the page's videos go into picture in picture,
	// a small window floating over every app's, as they do in Safari: the
	// button of a video's controls, the item of its context menu, and
	// requestPictureInPicture() in the page's script (macOS, where
	// WKWebView keeps it to Safari; MyGo turns it on with WebKit's private
	// preference). WebView2 lets them whatever this says, and WebKitGTK
	// has no picture in picture. Page.TogglePictureInPicture moves one
	// from Go.
	PictureInPicture bool
	// SwipeNavigation lets a horizontal swipe go back and forward through
	// the page's history, as browsers do. It is off by default, as an
	// app's pages have their own way back:
	//
	//   - macOS: a trackpad's or a Magic Mouse's swipe between pages, as
	//     System Settings sets it up (WKWebView's
	//     allowsBackForwardNavigationGestures).
	//   - Linux: two fingers across a touchpad, or one across a
	//     touchscreen (WebKitSettings' enable-back-forward-navigation-gestures).
	//   - Windows: a swipe across a touchscreen (WebView2's
	//     IsSwipeNavigationEnabled, which MyGo turns off unless asked).
	SwipeNavigation bool
	// PinchZoom lets a pinch magnify the page, as browsers do: it scales
	// what the page shows without laying it out again, which ZoomFactor
	// does, and leaves ZoomFactor as it is. It is off by default, as in
	// desktop apps, and MyGo turns off what the platform has on:
	//
	//   - macOS: a trackpad's pinch, and its smart zoom (WKWebView's
	//     allowsMagnification).
	//   - Linux: a touchscreen's pinch, and a touchpad's, which GTK reports
	//     on Wayland only. WebKitGTK has no setting for it and zooms: MyGo
	//     turns its zoom gesture off from WebKitGTK 2.38, and older
	//     versions always zoom.
	//   - Windows: a touchscreen's pinch (WebView2's IsPinchZoomEnabled); a
	//     WebView2 Runtime without ICoreWebView2Settings5 always zooms.
	PinchZoom bool
	// UserAgentSuffix appends to the page's user agent instead of
	// replacing it, as UserAgent does: the app's name and version, such as
	// "MyApp/1.2", for the sites that read them. It appends to UserAgent
	// too, which SetUserAgent keeps as well; leading and trailing spaces
	// are trimmed. Some sites serve a bare web view, whose user agent
	// names no browser, older pages: "Version/17.0 Safari/605.1.15" makes the
	// page look like Safari.
	UserAgentSuffix string
	// ContentRules block what the page loads and hide parts of it, as
	// Page.SetContentRules does, from its first load.
	ContentRules []*ContentRules
	// Session is the website data store of the page: its cookies, storage
	// and caches, which the pages of other sessions don't see. nil is the
	// default session. A window opened by window.open keeps its opener's
	// session, whatever the Session of the options its open handler
	// returns.
	Session *Session
	// Untrusted makes the page someone else's, whatever its URL: a
	// plugin's UI or a preview of a user's HTML served from a custom
	// scheme. TrustedOrigins no longer count, and the page:
	//
	//   - calls no Go method, bound or MyGo's own (window.mygo.window),
	//     and its window's drag regions do nothing; MyGo drops whatever
	//     else the page posts above 4 KiB without decoding it;
	//   - hears no Broadcast, nor the Emit of the window of native UI its
	//     web view is in: send to it with EmitPage;
	//   - gets no permission, opens no window, downloads nothing and
	//     navigates to no other origin than its page's, unless
	//     SetPermissionHandler, SetWindowOpenHandler, OnWillDownload or
	//     OnWillNavigate decide.
	//
	// Its requests to custom schemes reach their handlers, which tell it
	// with CallerPage(r.Context()), but it cannot read the responses of
	// other origins than its own.
	Untrusted bool
	// BackgroundThrottling decides whether the page slows down while its
	// window is hidden, minimized or covered, as web views do to save
	// power. ThrottlingOff keeps it running, for a page that plays, syncs
	// or computes in the background, as Electron's backgroundThrottling:
	// false does:
	//
	//	Page: mygo.PageOptions{BackgroundThrottling: mygo.ThrottlingOff}
	BackgroundThrottling BackgroundThrottling
	// Autoplay decides whether the page may play audible media before the
	// user clicked or typed in it. AutoplayAllow lets it, for a page the
	// user never touches, such as that of a hidden window playing the
	// sound of an app whose interface is native UI:
	//
	//	Page: mygo.PageOptions{Autoplay: mygo.AutoplayAllow}
	Autoplay Autoplay

	// SessionState brings back a page as Page.SessionState saved it, in
	// place of the window's or web view's URL: its back/forward list,
	// with the scroll positions and form fields saved, at the entry it
	// was at. Where the platform keeps no such state (Windows), or can't
	// read this one (it comes from another platform, or a version of the
	// web engine that encodes it otherwise), the page loads the URL it
	// was at alone; a state that isn't one loads URL.
	SessionState []byte
}

// BackgroundThrottling tells whether a page slows down while it can't be
// seen (PageOptions.BackgroundThrottling).
type BackgroundThrottling int

const (
	// ThrottlingOn, the default, leaves the page to its web view, which
	// slows down a page it can't see: its timers fire once a second at
	// most, requestAnimationFrame waits, and macOS may suspend the page of
	// a window behind others.
	ThrottlingOn BackgroundThrottling = iota
	// ThrottlingOff keeps the page's timers at their rate while its window
	// is hidden, minimized or covered, and its process at the priority of
	// a visible one:
	//
	//   - macOS: WKPreferences' inactiveSchedulingPolicy none (macOS 14 or
	//     later) and WebKit's private preferences that throttle a hidden
	//     page's timers, suspend its CSS animations and let App Nap slow
	//     its process turned off; the private
	//     -[WKWebView _setWindowOcclusionDetectionEnabled:NO] makes a
	//     window behind others count as visible, so that its page keeps
	//     requestAnimationFrame too. Private ones are looked up with
	//     respondsToSelector:.
	//   - Linux: WebKitGTK's HiddenPageDOMTimerThrottling and
	//     HiddenPageCSSAnimationSuspension features turned off (WebKitGTK
	//     2.42 or later; nothing before).
	//   - Windows: the browser runs with
	//     --disable-background-timer-throttling,
	//     --disable-renderer-backgrounding and
	//     --disable-backgrounding-occluded-windows. Those are the WebView2
	//     environment's, which all the app's pages share and the first
	//     window or web view showing a page creates: give that one the
	//     option, and then no page of the app has its timers throttled.
	//     A page that asks after it is too late, which the log tells.
	//
	// The page of a hidden or minimized window still goes hidden
	// (document.visibilityState) on macOS and Linux, and so does a web
	// view of a hidden window on Windows: its web view draws nothing and
	// requestAnimationFrame waits. Run what must go on with timers.
	ThrottlingOff
)

// Autoplay tells whether a page may play audible media without a user
// gesture (PageOptions.Autoplay).
type Autoplay int

const (
	// AutoplayUserGesture, the default, leaves the page to its web view's
	// autoplay policy. WebKitGTK's and WebView2's start an unmuted <audio>
	// or <video> only once the user clicked or typed in the page: play()
	// rejects with NotAllowedError before, and muted media that plays is
	// paused again when it is unmuted. A hidden window gets no gesture, so
	// its page can't play sound there.
	AutoplayUserGesture Autoplay = iota
	// AutoplayAllow lets the page start audible media on its own: play()
	// of an unmuted element resolves, and autoplay elements start with
	// their sound, in a hidden window too.
	//
	//   - macOS: WKWebViewConfiguration's
	//     mediaTypesRequiringUserActionForPlayback set to none.
	//   - Linux: the website policies of the page's main-frame
	//     navigations allow autoplay (WEBKIT_AUTOPLAY_ALLOW; WebKitGTK
	//     2.30 or later, nothing before).
	//   - Windows: the browser runs with
	//     --autoplay-policy=no-user-gesture-required. That is the WebView2
	//     environment's, which all the app's pages share and the first
	//     window or web view showing a page creates: give that one the
	//     option, and then every page of the app may play without a
	//     gesture, those that keep AutoplayUserGesture too. A page that
	//     asks after it is too late, which the log tells.
	AutoplayAllow
)
