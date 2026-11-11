//go:build darwin

package darwin

import (
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/ebitengine/purego/objc"

	"github.com/egoist/mygo/internal/bridge"
	"github.com/egoist/mygo/internal/platform"
)

const (
	styleTitled              = 1 << 0
	styleClosable            = 1 << 1
	styleMiniaturizable      = 1 << 2
	styleResizable           = 1 << 3
	styleFullScreen          = 1 << 14
	styleFullSizeContentView = 1 << 15
	styleNonactivatingPanel  = 1 << 7

	nsBackingStoreBuffered   = 2
	nsFloatingWindowLevel    = 3
	nsPopUpMenuWindowLevel   = 101
	nsNormalWindowLevel      = 0
	nsViewWidthHeightSizable = 2 | 16

	collectionCanJoinAllSpaces    = 1 << 0
	collectionTransient           = 1 << 3
	collectionIgnoresCycle        = 1 << 6
	collectionFullScreenPrimary   = 1 << 7
	collectionFullScreenAuxiliary = 1 << 8
	collectionFullScreenNone      = 1 << 9

	// NSApplicationPresentationOptions
	presentationAutoHideMenuBar = 1 << 2
	presentationFullScreen      = 1 << 10
	presentationAutoHideToolbar = 1 << 11
)

var (
	errInvalidImage = errors.New("mygo: invalid image data")
	errClosed       = errors.New("mygo: window has been closed")
)

type window struct {
	b        *Backend
	h        platform.WindowHandler
	opts     *platform.WindowOptions
	win      id
	view     id // the content view: holds the web view and a docked inspector
	web      id
	delegate id
	ucc      id
	// effect is the material behind the content (Vibrancy): an
	// NSVisualEffectView, or an NSGlassEffectView when glass is.
	effect id
	glass  bool
	// keepActive is SetActiveAppearance's.
	keepActive bool
	// tint is the material's tint, which only NSGlassEffectView takes.
	tint *platform.Color
	// provisionalURL is the URL of the navigation started last, which a
	// load failed before it committed is of when its error doesn't tell.
	provisionalURL string
	// radius is the corners the content view is cut to (Flyout.Radius,
	// WindowOptions.CornerRadius), which the material follows.
	radius int
	// systemBacking tells that the content view of a rounded window has
	// the system's window color, which a layer cannot follow: it is
	// resolved again when the appearance changes (fillSystemBacking).
	systemBacking bool
	// glassClear tells that the glass made the window clear
	// (clearForGlass), an opaque one or the content view of a rounded one,
	// whose color waits in backing, retained, for the glass to go: 0 for
	// the system's (systemBacking).
	glassClear bool
	backing    id
	parent     *window
	// hiddenWithParent tells that a child window was ordered out as its
	// parent hid or miniaturized (hideChildren), at hiddenRank among its
	// siblings, front to back, to show again with it; orderPending that
	// orderChildrenLater posted the ordering of the window's children.
	hiddenWithParent bool
	hiddenRank       int
	orderPending     bool
	// onTop is what AlwaysOnTop asked for, which the window's level
	// follows, or its parent's when higher (applyLevel). attached tells
	// that a child window is an AppKit child window of its parent, which
	// is in full screen, and childrenAttached that the window attaches its
	// child windows, from entering full screen to leaving it
	// (attachChildren).
	onTop            bool
	attached         bool
	childrenAttached bool
	// flyout is how a flyout (WindowOptions.Flyout) was placed last, at
	// flyoutSize.
	flyout     platform.Flyout
	flyoutSize platform.Size
	// popover shows a flyout with the look of AppKit's popovers
	// (Flyout.Popover), in its window, win while it shows; popoverClosing
	// tells that the backend closes it, not the user; popoverHeld that
	// a window was modal to it as it showed (holdPopover).
	popover, popoverController  id
	popoverClosing, popoverHeld bool

	lastMouseDown id
	dropped       []string         // DroppedFiles
	downloads     map[id][2]string // WKDownload: URL and path
	attention     int              // the request of FlashFrame
	maximized     bool
	// auxiliary tells that SetVisibleOnAllWorkspaces made a window that
	// cannot go full screen an auxiliary of full-screen Spaces, to undo.
	auxiliary     bool
	programmatic  bool
	closed        bool
	sheet         bool
	trafficLights *platform.Point
	// surface shows the content MyGo draws, in place of the web view.
	surface *surface
	// webViews are the web views under the surface (webview.go), and host
	// the window whose surface a web view is under, in the clip view clip,
	// in pane, which shows it scaled and cut to its corners; mask is the
	// layer cutting pane to corners of different radii.
	webViews []*window
	host     *window
	clip     id
	pane     id
	mask     id
	// pointerIn tells that the page of a web view has the pointer, where
	// nothing the content painted covers it.
	pointerIn bool
	// accessParent is the element of the content showing a web view,
	// which assistive technology finds its page in.
	accessParent id
	// observesAudio tells that the window observes its web view's
	// _isPlayingAudio, which WebKit answers (IsAudible).
	observesAudio bool
	// observesCapture tells, by platform.CaptureKind, that the window
	// observes its web view's capture state (captureKeys), which WebKit
	// answers.
	observesCapture [3]bool
	// captureMuted tells, by platform.CaptureKind, the capture that
	// SetCaptureState muted, until it stops, which SetAudioMuted keeps
	// apart for the camera and the microphone. WebKit's own changes, which
	// may be SetAudioMuted's on their way, don't change it.
	captureMuted [3]bool
	// pageCacheOff tells that the window turned WebKit's back/forward
	// cache off as its page left with a video in picture in picture
	// (leavingPage).
	pageCacheOff bool
	// restoredScroll is the scroll position of the page RestoreSessionState
	// brought back before its window showed on screen, until the page
	// loaded (keepRestoredScroll).
	restoredScroll NSPoint
	// pipActive tells that the page's document has a video in picture in
	// picture, as its pipScript last said.
	pipActive bool
}

func (b *Backend) NewWindow(o *platform.WindowOptions, h platform.WindowHandler) (platform.Window, error) {
	w := &window{b: b, h: h, opts: o, trafficLights: o.TrafficLightPosition, downloads: map[id][2]string{}}
	if p, ok := o.Parent.(*window); ok && p != nil {
		w.parent = p
	}
	withPool(w.create)
	return w, nil
}

func (w *window) create() {
	o := w.opts

	if o.Flyout != nil {
		w.createFlyout()
		return
	}
	style := uint(styleTitled)
	if o.Closable {
		style |= styleClosable
	}
	if o.Minimizable {
		style |= styleMiniaturizable
	}
	if o.Resizable {
		style |= styleResizable
	}
	hiddenTitleBar := o.Frameless || o.TitleBarStyle == "hidden" || o.TitleBarStyle == "hiddenInset"
	if hiddenTitleBar {
		style |= styleFullSizeContentView
	}

	size := NSRect{Size: NSSize{float64(o.Width), float64(o.Height)}}
	content := size
	if !o.UseContentSize {
		content = msgRectForRect(class("NSWindow"), sel("contentRectForFrameRect:styleMask:"), size, style)
		content.Origin = NSPoint{}
	}
	cls := "MyGoWindow"
	if o.Panel {
		// The nonactivating style must be there from the start: the window
		// server only learns that the window never activates its app from
		// the style it is created with.
		cls = "MyGoPanel"
		style |= styleNonactivatingPanel
	}
	w.win = msgInitWindow(send(class(cls), "alloc"), sel("initWithContentRect:styleMask:backing:defer:"),
		content, style, nsBackingStoreBuffered, false)
	send(w.win, "setReleasedWhenClosed:", 0)
	w.delegate = alloc("MyGoWindowDelegate")
	send(w.win, "setDelegate:", uintptr(w.delegate))
	send(w.win, "setTitle:", uintptr(nsString(o.Title)))
	send(w.win, "setTabbingMode:", 2) // NSWindowTabbingModeDisallowed

	if hiddenTitleBar {
		send(w.win, "setTitlebarAppearsTransparent:", 1)
		send(w.win, "setTitleVisibility:", 1) // NSWindowTitleHidden
	}
	if o.Frameless || hiddenTitleBar && o.HideWindowButtons {
		for i := uintptr(0); i < 3; i++ {
			send(send(w.win, "standardWindowButton:", i), "setHidden:", 1)
		}
	}
	if o.TitleBarStyle == "hiddenInset" && !o.Frameless {
		tb := send(send(class("NSToolbar"), "alloc"), "initWithIdentifier:", uintptr(nsString("mygo.titlebar")))
		send(tb, "setShowsBaselineSeparator:", 0)
		send(w.win, "setToolbar:", uintptr(tb))
		release(tb)
	}
	if o.MinSize.Width > 0 || o.MinSize.Height > 0 {
		w.SetMinimumSize(o.MinSize)
	}
	if o.MaxSize.Width > 0 || o.MaxSize.Height > 0 {
		w.SetMaximumSize(o.MaxSize)
	}
	send(w.win, "setMovable:", boolArg(o.Movable))
	if !o.Maximizable {
		w.SetMaximizable(false)
	}
	behavior := uint(collectionFullScreenPrimary)
	if !o.Fullscreenable {
		behavior = collectionFullScreenNone
	}
	if o.Panel {
		// On the Space the user is on, full-screen ones included, as they
		// switch, and out of Mission Control and the window cycle.
		behavior = collectionCanJoinAllSpaces | collectionFullScreenAuxiliary | collectionIgnoresCycle | collectionTransient
		// A panel stays as the app is deactivated: the app hides it.
		send(w.win, "setHidesOnDeactivate:", 0)
	} else if w.modalToFlyout() {
		// Where its flyout is, over another app in full screen too.
		behavior = collectionFullScreenAuxiliary | collectionTransient
	}
	send(w.win, "setCollectionBehavior:", uintptr(behavior))
	// A child window takes its parent's level when that is higher.
	w.onTop = o.AlwaysOnTop
	w.applyLevel()
	radius := 0
	if o.Frameless {
		radius = o.CornerRadius
	}
	w.fillWindow(content, radius)

	if o.Center {
		send(w.win, "center")
	} else {
		frame := msgRect(w.win, sel("frame"))
		r := rectFromMac(frame)
		r.X, r.Y = o.X, o.Y
		msgSetRectBool(w.win, sel("setFrame:display:"), rectToMac(r), false)
	}
	if w.trafficLights != nil {
		w.layoutTrafficLights()
		// The window's appearance changes with the application's, or when
		// it is given its own; either lays the title bar out again.
		send(w.win, "addObserver:forKeyPath:options:context:", uintptr(w.delegate), uintptr(nsString("effectiveAppearance")), 0, 0)
	}
	w.register()

	if o.FullScreen {
		send(w.win, "toggleFullScreen:", 0)
	} else if o.Maximized {
		send(w.win, "zoom:", 0)
	}
}

// createFlyout creates the panel of a flyout (WindowOptions.Flyout):
// borderless and nonactivating, so that it never activates the app,
// and the key window only when it is focusable, while its parent stays
// the main window. It is a child window of its parent while it shows.
func (w *window) createFlyout() {
	o := w.opts
	content := NSRect{Size: NSSize{float64(o.Width), float64(o.Height)}}
	w.flyout = *o.Flyout
	w.flyoutSize = platform.Size{Width: o.Width, Height: o.Height}
	if w.flyout.Popover {
		w.createPopover(content)
		return
	}
	w.win = msgInitWindow(send(class("MyGoFlyout"), "alloc"), sel("initWithContentRect:styleMask:backing:defer:"),
		content, styleNonactivatingPanel, nsBackingStoreBuffered, false)
	send(w.win, "setReleasedWhenClosed:", 0)
	w.delegate = alloc("MyGoWindowDelegate")
	send(w.win, "setDelegate:", uintptr(w.delegate))
	send(w.win, "setTitle:", uintptr(nsString(o.Title)))
	// NSPanel hides when the app is deactivated: a flyout stays with its
	// parent instead, and one that takes the keyboard closes.
	send(w.win, "setHidesOnDeactivate:", 0)
	send(w.win, "setCollectionBehavior:", collectionTransient|collectionIgnoresCycle|collectionFullScreenAuxiliary)
	if w.flyout.Tray != nil {
		// Over the windows of other apps, as the menus of the menu bar.
		send(w.win, "setLevel:", nsPopUpMenuWindowLevel)
	}
	w.fillWindow(content, w.flyout.Radius)
	w.register()
	w.PlaceFlyout(w.flyout, w.flyoutSize)
}

// roundContent cuts the content view, and the material in it, to corners
// of radius, a flyout's (Flyout.Radius) or a frameless window's
// (WindowOptions.CornerRadius). The window is made clear, its color moved
// to the content view, which is cut to them, so that nothing fills the
// corners, and its shadow, made from what it draws, follows them.
func (w *window) roundContent(radius int) {
	if radius <= 0 {
		return
	}
	w.radius = radius
	send(w.view, "setWantsLayer:", 1)
	layer := send(w.view, "layer")
	msgSetFloat(layer, sel("setCornerRadius:"), float64(radius))
	send(layer, "setCornerCurve:", uintptr(nsString("continuous")))
	send(layer, "setMasksToBounds:", 1)
	if sendBool(w.win, "isOpaque") {
		if w.opts.BackgroundColor == nil {
			w.systemBacking = true
			w.fillSystemBacking()
		} else {
			send(layer, "setBackgroundColor:", uintptr(send(send(w.win, "backgroundColor"), "CGColor")))
		}
		send(w.win, "setOpaque:", 0)
		send(w.win, "setBackgroundColor:", uintptr(send(class("NSColor"), "clearColor")))
	}
	w.roundEffect()
	send(w.win, "invalidateShadow")
}

// fillSystemBacking gives the content view of a rounded window without a
// BackgroundColor the window color of the system, as the app's appearance
// draws it. A layer's color is fixed once set, where the window's followed
// the appearance: refreshSystemBackings sets it again when that changes.
func (w *window) fillSystemBacking() {
	withPool(func() {
		appearance := class("NSAppearance")
		prev := send(appearance, "currentAppearance")
		send(appearance, "setCurrentAppearance:", uintptr(send(w.b.app, "effectiveAppearance")))
		c := send(send(class("NSColor"), "windowBackgroundColor"), "CGColor")
		send(send(w.view, "layer"), "setBackgroundColor:", uintptr(c))
		send(appearance, "setCurrentAppearance:", uintptr(prev))
	})
}

// refreshSystemBackings resolves the window color of rounded windows
// anew, once the appearance or the system's colors changed.
func (b *Backend) refreshSystemBackings() {
	for _, w := range b.byNSWindow {
		if w.systemBacking && !w.glassClear {
			w.fillSystemBacking()
		}
	}
}

// roundEffect cuts the material to the content view's corners. The glass
// draws its rim along its own corners, which it rounds itself.
func (w *window) roundEffect() {
	if w.effect == 0 || w.radius <= 0 {
		return
	}
	if w.glass {
		msgSetFloat(w.effect, sel("setCornerRadius:"), float64(w.radius))
		return
	}
	send(w.effect, "setWantsLayer:", 1)
	el := send(w.effect, "layer")
	msgSetFloat(el, sel("setCornerRadius:"), float64(w.radius))
	send(el, "setCornerCurve:", uintptr(nsString("continuous")))
	send(el, "setMasksToBounds:", 1)
}

// fillWindow gives a new window its look, its corners of radius
// (roundContent) and its web view or surface.
func (w *window) fillWindow(content NSRect, radius int) {
	o := w.opts
	send(w.win, "setHasShadow:", boolArg(o.HasShadow))
	if o.Opacity < 1 {
		msgSetFloat(w.win, sel("setAlphaValue:"), o.Opacity)
	}
	if o.Transparent {
		send(w.win, "setOpaque:", 0)
		send(w.win, "setBackgroundColor:", uintptr(send(class("NSColor"), "clearColor")))
	} else if o.BackgroundColor != nil {
		send(w.win, "setBackgroundColor:", uintptr(nsColor(*o.BackgroundColor)))
	}

	w.createContent(content)
	if w.surface != nil {
		send(w.win, "makeFirstResponder:", uintptr(w.surface.view))
	}
	send(w.win, "setContentView:", uintptr(w.view))
	// Before the material: the glass clears the color roundContent moves.
	w.roundContent(radius)
	if o.Vibrancy != "" {
		w.SetVibrancy(o.Vibrancy)
	}
	if o.VibrancyTint != nil {
		w.SetVibrancyTint(o.VibrancyTint)
	}
}

// createContent creates the web view or the surface, in w.view.
func (w *window) createContent(content NSRect) {
	if w.opts.Surface {
		w.createSurface(content)
	} else {
		w.createWebView(content)
	}
	// WebKit docks the inspector next to the web view, in its superview.
	// That must not be the window's frame view: AppKit would draw a broken
	// legacy title bar from then on.
	w.view = msgInitRect(send(class("NSView"), "alloc"), sel("initWithFrame:"), NSRect{Size: content.Size})
	if w.surface != nil {
		send(w.view, "addSubview:", uintptr(w.surface.view))
	} else {
		send(w.view, "addSubview:", uintptr(w.web))
	}
}

// register lets the delegate, the web view and the window find w.
func (w *window) register() {
	b := w.b
	b.byDelegate[w.delegate] = w
	b.byWebView[w.web] = w
	if w.win != 0 {
		b.byNSWindow[w.win] = w
	}
}

// PlaceFlyout places a flyout next to its anchor, in the parent's content
// area, as Flyout.Place resolves it in the visible frame of a screen.
func (w *window) PlaceFlyout(f platform.Flyout, size platform.Size) {
	if w.opts.Flyout == nil || w.closed {
		return
	}
	w.flyout, w.flyoutSize = f, size
	if w.popover != 0 {
		w.placePopover()
		return
	}
	displays := screen{}.Displays()
	anchor := f.Anchor
	if t, ok := f.Tray.(*tray); ok {
		if anchor = t.Bounds(); anchor.Width == 0 || anchor.Height == 0 {
			r := platform.Centered(size, platform.PrimaryWorkArea(displays))
			msgSetRectBool(w.win, sel("setFrame:display:"), rectToMac(r), true)
			return
		}
	} else if p := w.parent; p != nil && !p.closed {
		c := p.ContentBounds()
		anchor.X += c.X
		anchor.Y += c.Y
	}
	r, _, _ := f.Place(anchor, size, platform.WorkAreaFor(anchor, displays))
	msgSetRectBool(w.win, sel("setFrame:display:"), rectToMac(r), true)
}

func (w *window) createWebView(content NSRect) {
	o := w.opts
	var cfg id
	if o.Native != 0 {
		// window.open(): WebKit hands us the configuration to use.
		cfg = retain(id(o.Native))
	} else {
		cfg = alloc("WKWebViewConfiguration")
		for _, s := range o.Schemes {
			send(cfg, "setURLSchemeHandler:forURLScheme:", uintptr(schemeHandler()), uintptr(nsString(s)))
		}
		if o.DataStore != nil { // a session's (session.go)
			send(cfg, "setWebsiteDataStore:", uintptr(websiteDataStore(o.DataStore)))
		}
	}
	prefs := send(cfg, "preferences")
	send(prefs, "setValue:forKey:", uintptr(nsBool(o.DevTools)), uintptr(nsString("developerExtrasEnabled")))
	send(prefs, "setValue:forKey:", uintptr(nsBool(true)), uintptr(nsString("allowFileAccessFromFileURLs")))
	if respondsTo(prefs, "setElementFullscreenEnabled:") {
		send(prefs, "setElementFullscreenEnabled:", 1)
	}
	// Tab reaches buttons, links and the other controls too, as it does in
	// Chrome, Electron and WebView2, whatever the system's keyboard
	// navigation setting, unless the app asked for Safari's behavior.
	if respondsTo(prefs, "setTabFocusesLinks:") {
		send(prefs, "setTabFocusesLinks:", boolArg(o.TabFocusesLinks))
	}
	if o.NoBackgroundThrottling || o.Native != 0 {
		setBackgroundThrottling(cfg, prefs, !o.NoBackgroundThrottling, o.Native != 0)
		// It may have given a window.open window a copy of its opener's.
		prefs = send(cfg, "preferences")
	}
	// Picture in picture, which WKWebView keeps to Safari: the button of
	// videos' controls, their context menu's item and
	// requestPictureInPicture().
	if respondsTo(prefs, "_setAllowsPictureInPictureMediaPlayback:") {
		send(prefs, "_setAllowsPictureInPictureMediaPlayback:", boolArg(o.PictureInPicture))
	}
	setAutoplay(cfg, o.Autoplay, o.Native != 0)

	// Every window gets its own content controller so messages, scripts and
	// the bridge configuration never leak between windows.
	w.ucc = alloc("WKUserContentController")
	send(w.ucc, "addScriptMessageHandler:name:", uintptr(w.delegate), uintptr(nsString("mygo")))
	if o.PictureInPicture {
		send(w.ucc, "addScriptMessageHandler:name:", uintptr(w.delegate), uintptr(nsString(pipHandler)))
	}
	w.addUserScripts(o.Zoom)
	send(cfg, "setUserContentController:", uintptr(w.ucc))

	// WebKit appends the application name to the user agent of the web view,
	// and window.open() hands us the configuration of the window that opens
	// it: the options of the new window decide, as they do on Linux and
	// Windows, where a window never inherits a suffix.
	var appName id
	if o.UserAgentSuffix != "" {
		appName = nsString(o.UserAgentSuffix)
	}
	if o.UserAgentSuffix != "" || o.Native != 0 {
		send(cfg, "setApplicationNameForUserAgent:", uintptr(appName))
	}

	w.web = msgInitRectID(send(class("MyGoWebView"), "alloc"), sel("initWithFrame:configuration:"), NSRect{Size: content.Size}, cfg)
	release(cfg)
	send(w.web, "setNavigationDelegate:", uintptr(w.delegate))
	send(w.web, "setUIDelegate:", uintptr(w.delegate))
	send(w.web, "setAutoresizingMask:", nsViewWidthHeightSizable)
	if o.Transparent || o.BackgroundColor != nil || o.Vibrancy != "" {
		// Let the window color show until the page paints.
		send(w.web, "setValue:forKey:", uintptr(nsBool(false)), uintptr(nsString("drawsBackground")))
		if o.BackgroundColor != nil && respondsTo(w.web, "setUnderPageBackgroundColor:") {
			send(w.web, "setUnderPageBackgroundColor:", uintptr(nsColor(*o.BackgroundColor)))
		}
	}
	if respondsTo(w.web, "setInspectable:") {
		send(w.web, "setInspectable:", boolArg(o.DevTools))
	}
	if o.NoBackgroundThrottling && respondsTo(w.web, "_setWindowOcclusionDetectionEnabled:") {
		// A window behind others counts as visible.
		send(w.web, "_setWindowOcclusionDetectionEnabled:", 0)
	}
	// Both are off in a new WKWebView.
	if o.SwipeNavigation {
		send(w.web, "setAllowsBackForwardNavigationGestures:", 1)
	}
	if o.PinchZoom {
		send(w.web, "setAllowsMagnification:", 1)
	}
	if o.UserAgent != "" {
		ua := o.UserAgent
		if o.UserAgentSuffix != "" {
			ua += " " + o.UserAgentSuffix
		}
		send(w.web, "setCustomUserAgent:", uintptr(nsString(ua)))
	}
	if o.Zoom > 0 && o.Zoom != 1 {
		msgSetFloat(w.web, sel("setPageZoom:"), o.Zoom)
	}
	send(w.web, "addObserver:forKeyPath:options:context:", uintptr(w.delegate), uintptr(nsString("title")), 1, 0)
	if respondsTo(w.web, "_isPlayingAudio") {
		// WebKit tells its changes with willChangeValueForKey: and
		// didChangeValueForKey:, as Safari's tabs' speaker icons hear.
		w.observesAudio = true
		send(w.web, "addObserver:forKeyPath:options:context:", uintptr(w.delegate), uintptr(nsString("_isPlayingAudio")), 0, 0)
	}
	// WebKit tells their changes as for _isPlayingAudio.
	for kind, key := range captureKeys {
		if respondsTo(w.web, key) {
			w.observesCapture[kind] = true
			send(w.web, "addObserver:forKeyPath:options:context:", uintptr(w.delegate), uintptr(nsString(key)), 0, 0)
		}
	}
}

// throttlingPrefs are the preferences with which WebKit slows down a page
// it can't see, and the value of each that keeps the page running
// (PageOptions.BackgroundThrottling): the public inactiveSchedulingPolicy,
// whose WKInactiveSchedulingPolicyNone keeps the web content process of a
// window behind others from being suspended (macOS 14+), and private
// ones, looked up with respondsToSelector:, that throttle a hidden page's
// timers to once a second, suspend its CSS animations and let App Nap
// slow down its process.
var throttlingPrefs = [...]struct {
	get, set string
	off      uintptr
	boolean  bool
}{
	{"inactiveSchedulingPolicy", "setInactiveSchedulingPolicy:", 2, false},
	{"_hiddenPageDOMTimerThrottlingEnabled", "_setHiddenPageDOMTimerThrottlingEnabled:", 0, true},
	{"_hiddenPageCSSAnimationSuspensionEnabled", "_setHiddenPageCSSAnimationSuspensionEnabled:", 0, true},
	{"_pageVisibilityBasedProcessSuppressionEnabled", "_setPageVisibilityBasedProcessSuppressionEnabled:", 0, true},
}

// setBackgroundThrottling sets throttlingPrefs in prefs, the preferences
// of cfg: to WebKit's defaults when throttle, else to the values that keep
// a hidden page running. The preferences of a configuration window.open()
// hands over (shared) are the opener's: a window that wants them
// otherwise gets a copy, which leaves the opener's as they are.
func setBackgroundThrottling(cfg, prefs id, throttle, shared bool) {
	var defaults id
	if throttle {
		defaults = alloc("WKPreferences")
		defer release(defaults)
	}
	get := func(o id, p int) uintptr {
		v := uintptr(send(o, throttlingPrefs[p].get))
		if throttlingPrefs[p].boolean {
			v = boolArg(byte(v) != 0)
		}
		return v
	}
	for i, p := range throttlingPrefs {
		if !respondsTo(prefs, p.set) || !respondsTo(prefs, p.get) {
			continue
		}
		v := p.off
		if throttle {
			v = get(defaults, i)
		}
		if get(prefs, i) == v {
			continue
		}
		if shared && respondsTo(prefs, "copyWithZone:") {
			shared = false
			prefs = send(prefs, "copy")
			send(cfg, "setPreferences:", uintptr(prefs))
			release(prefs) // the configuration keeps it
		}
		send(prefs, p.set, v)
	}
}

// setAutoplay lets the page of cfg play audible media without a user
// gesture (PageOptions.Autoplay): no media type requires the user's action
// (WKAudiovisualMediaTypeNone). A configuration window.open() handed over
// (opened) is a copy of the opener's, which a window that doesn't ask gets
// WebKit's default back from. The property is macOS 10.12's, and still
// looked up with respondsToSelector:.
func setAutoplay(cfg id, allow, opened bool) {
	const set = "setMediaTypesRequiringUserActionForPlayback:"
	switch {
	case !respondsTo(cfg, set):
	case allow:
		send(cfg, set, 0)
	case opened:
		defaults := alloc("WKWebViewConfiguration")
		send(cfg, set, uintptr(send(defaults, "mediaTypesRequiringUserActionForPlayback")))
		release(defaults)
	}
}

// stopObserving removes the key-value observers createWebView added.
func (w *window) stopObserving() {
	send(w.web, "removeObserver:forKeyPath:", uintptr(w.delegate), uintptr(nsString("title")))
	if w.observesAudio {
		w.observesAudio = false
		send(w.web, "removeObserver:forKeyPath:", uintptr(w.delegate), uintptr(nsString("_isPlayingAudio")))
	}
	for kind, key := range captureKeys {
		if w.observesCapture[kind] {
			w.observesCapture[kind] = false
			send(w.web, "removeObserver:forKeyPath:", uintptr(w.delegate), uintptr(nsString(key)))
		}
	}
}

func nsColor(c platform.Color) id {
	return msgColor(class("NSColor"), sel("colorWithSRGBRed:green:blue:alpha:"),
		float64(c.R)/255, float64(c.G)/255, float64(c.B)/255, float64(c.A)/255)
}

// cleanup releases native resources; called from windowWillClose:.
func (w *window) cleanup() {
	if w.closed {
		return
	}
	w.closeWebViews()
	w.closed = true
	if contextWindow == w {
		contextWindow = nil
	}
	w.releaseChildren()
	b := w.b
	w.stopObserving()
	if w.trafficLights != nil {
		send(w.win, "removeObserver:forKeyPath:", uintptr(w.delegate), uintptr(nsString("effectiveAppearance")))
	}
	send(w.ucc, "removeScriptMessageHandlerForName:", uintptr(nsString("mygo")))
	if w.opts.PictureInPicture {
		send(w.ucc, "removeScriptMessageHandlerForName:", uintptr(nsString(pipHandler)))
	}
	send(w.ucc, "removeAllUserScripts")
	send(w.web, "stopLoading")
	send(w.web, "setNavigationDelegate:", 0)
	send(w.web, "setUIDelegate:", 0)
	if w.popover != 0 {
		w.cleanupPopover()
	} else {
		send(w.win, "setDelegate:", 0)
		if w.modalToFlyout() {
			w.leaveFlyout()
		} else if w.parent != nil && !w.parent.closed {
			send(w.parent.win, "removeChildWindow:", uintptr(w.win))
		}
	}
	delete(b.byDelegate, w.delegate)
	delete(b.byWebView, w.web)
	delete(b.byNSWindow, w.win)
	w.forgetDrag()
	release(w.ucc)
	for _, j := range printJobs {
		if j.w == w && j.cb != nil {
			cb := j.cb
			j.cb = nil
			cb(nil, errDestroyed)
		}
	}
	if w.surface != nil {
		w.surface.destroy()
	}
	release(w.effect)
	release(w.backing)
	release(w.web)
	release(w.view)
	// AppKit is still closing the window; let the pool release it.
	autorelease(w.delegate)
	if w.popover == 0 {
		autorelease(w.win)
	}
}

func (w *window) Handle() uintptr        { return uintptr(w.win) }
func (w *window) WebViewHandle() uintptr { return uintptr(w.web) }

func (w *window) SetTitle(title string) {
	withPool(func() { send(w.win, "setTitle:", uintptr(nsString(title))) })
	w.layoutTrafficLights()
}

// SetRepresentedFile shows the file's proxy icon in the title bar, which
// the user drags, and whose ⌘-click menu lists its folders.
func (w *window) SetRepresentedFile(path string) {
	withPool(func() {
		var url id
		if path != "" {
			url = fileURL(path)
		}
		send(w.win, "setRepresentedURL:", uintptr(url))
	})
	// AppKit lays the title bar's buttons out again.
	w.layoutTrafficLights()
}

// SetDocumentEdited shows the dot in the close button, and dims the proxy
// icon.
func (w *window) SetDocumentEdited(edited bool) {
	send(w.win, "setDocumentEdited:", boolArg(edited))
	w.layoutTrafficLights()
}

func (w *window) Title() string {
	var s string
	withPool(func() { s = goString(send(w.win, "title")) })
	return s
}

func (w *window) SetBounds(r platform.Rect) {
	msgSetRectBool(w.win, sel("setFrame:display:"), rectToMac(r), true)
}

func (w *window) Bounds() platform.Rect { return rectFromMac(msgRect(w.win, sel("frame"))) }

func (w *window) SetContentBounds(r platform.Rect) {
	frame := msgRectToRect(w.win, sel("frameRectForContentRect:"), rectToMac(r))
	msgSetRectBool(w.win, sel("setFrame:display:"), frame, true)
}

func (w *window) ContentBounds() platform.Rect {
	frame := msgRect(w.win, sel("frame"))
	return rectFromMac(msgRectToRect(w.win, sel("contentRectForFrameRect:"), frame))
}

func (w *window) SetMinimumSize(s platform.Size) {
	msgSetSize(w.win, sel("setMinSize:"), NSSize{float64(s.Width), float64(s.Height)})
}

func (w *window) SetMaximumSize(s platform.Size) {
	max := NSSize{math.MaxFloat32, math.MaxFloat32}
	if s.Width > 0 {
		max.Width = float64(s.Width)
	}
	if s.Height > 0 {
		max.Height = float64(s.Height)
	}
	msgSetSize(w.win, sel("setMaxSize:"), max)
}

func (w *window) setStyle(bit uint, on bool) {
	mask := uint(send(w.win, "styleMask"))
	if on {
		mask |= bit
	} else {
		mask &^= bit
	}
	send(w.win, "setStyleMask:", uintptr(mask))
}

func (w *window) hasStyle(bit uint) bool { return uint(send(w.win, "styleMask"))&bit != 0 }

func (w *window) SetResizable(v bool)   { w.setStyle(styleResizable, v) }
func (w *window) IsResizable() bool     { return w.hasStyle(styleResizable) }
func (w *window) SetMovable(v bool)     { send(w.win, "setMovable:", boolArg(v)) }
func (w *window) IsMovable() bool       { return sendBool(w.win, "isMovable") }
func (w *window) SetMinimizable(v bool) { w.setStyle(styleMiniaturizable, v) }
func (w *window) IsMinimizable() bool   { return w.hasStyle(styleMiniaturizable) }
func (w *window) SetClosable(v bool)    { w.setStyle(styleClosable, v) }
func (w *window) IsClosable() bool      { return w.hasStyle(styleClosable) }

func (w *window) SetMaximizable(v bool) {
	send(send(w.win, "standardWindowButton:", 2), "setEnabled:", boolArg(v))
}

func (w *window) IsMaximizable() bool {
	return sendBool(send(w.win, "standardWindowButton:", 2), "isEnabled")
}

func (w *window) SetAlwaysOnTop(v bool) {
	w.onTop = v
	if w.host != nil || w.opts.Flyout != nil {
		level := uintptr(nsNormalWindowLevel)
		if v {
			level = nsFloatingWindowLevel
		}
		send(w.win, "setLevel:", level)
		return
	}
	// Its child windows follow it to its level (applyLevel).
	w.applyLevel()
	if w.onScreen() {
		w.orderChildrenLater()
	}
}

func (w *window) IsAlwaysOnTop() bool {
	if w.isChild() {
		return w.onTop // its level is its parent's when that is higher
	}
	return sendInt(w.win, "level") != nsNormalWindowLevel
}

func (w *window) Show() {
	if w.popover != 0 {
		w.showPopover()
		return
	}
	if w.opts.Flyout != nil {
		w.showFlyout(w.flyout.Focusable)
		return
	}
	if w.modalToFlyout() {
		w.showOverFlyout(true)
		return
	}
	if w.parent != nil && w.opts.Modal && !w.sheet {
		w.sheet = true
		send(w.parent.win, "beginSheet:completionHandler:", uintptr(w.win), 0)
		return
	}
	w.showAboveParent(true)
	if !w.opts.Panel {
		send(w.b.app, "activateIgnoringOtherApps:", 1)
	}
}

// A window modal to a flyout (modalToFlyout) is no sheet of it, which
// hangs from its top edge, no wider than a flyout is: it shows where the
// core put it, over the flyout, attached as its AppKit child window at
// its level, as a sheet is, which keeps it above the flyout. The flyout
// takes no input while it shows (watchModalInput), and keeps open
// (modalChild): it neither closes as it resigns key, nor on a click
// outside the app, and its popover is held open (holdPopover).

// modalToFlyout tells that w is a window modal to a flyout.
func (w *window) modalToFlyout() bool {
	return w.opts.Modal && w.parent != nil && w.parent.opts.Flyout != nil
}

// modalChild returns the window modal to w that shows, or nil.
func (w *window) modalChild() *window {
	for _, c := range w.b.byNSWindow {
		if c.parent == w && c.modalToFlyout() && !c.closed && sendBool(c.win, "isVisible") {
			return c
		}
	}
	return nil
}

// showOverFlyout shows a window modal to a flyout above it, as the key
// window for Show (key).
func (w *window) showOverFlyout(key bool) {
	w.b.watchModalInput()
	w.applyLevel() // a popover's window is made as it shows
	p := w.parent
	p.holdPopover()
	if key {
		send(w.b.app, "activateIgnoringOtherApps:", 1)
		send(w.win, "makeKeyAndOrderFront:", 0)
	} else {
		send(w.win, "orderFrontRegardless")
	}
	if !p.closed && p.win != 0 && sendBool(p.win, "isVisible") && send(w.win, "parentWindow") == 0 {
		send(p.win, "addChildWindow:ordered:", uintptr(w.win), 1) // NSWindowAbove
	}
}

// leaveFlyout detaches a window modal to a flyout from it as it hides or
// closes, and gives the keyboard it had back to the flyout when that
// takes it, else to the flyout's parent.
func (w *window) leaveFlyout() {
	p := w.parent
	key := sendBool(w.win, "isKeyWindow")
	if p.closed || p.win == 0 {
		return
	}
	send(p.win, "removeChildWindow:", uintptr(w.win))
	if !key || !p.IsVisible() {
		return
	}
	if p.flyout.Focusable {
		send(p.win, "makeKeyWindow")
	} else if pp := p.parent; pp != nil && !pp.closed && sendBool(pp.win, "isVisible") {
		send(pp.win, "makeKeyWindow")
	}
}

// modalInput is the monitor of the app's events that eats the mouse's and
// the keyboard's in a flyout a window is modal to, as AppKit's in a
// window under its sheet: a press brings the modal window in front, with
// the keyboard. It is made once, as the first such window shows.
var modalInput id

func (b *Backend) watchModalInput() {
	if modalInput != 0 {
		return
	}
	// left, right and other mouse down, up and dragged, mouse moved, key
	// down and up, scroll wheel
	const mask = 1<<1 | 1<<3 | 1<<25 | 1<<2 | 1<<4 | 1<<26 | 1<<6 | 1<<7 | 1<<27 | 1<<5 | 1<<10 | 1<<11 | 1<<22
	blk := newBlock(func(_ objc.Block, ev id) id {
		f := b.byNSWindow[send(ev, "window")]
		if f == nil || f.closed || f.opts.Flyout == nil {
			return ev
		}
		m := f.modalChild()
		if m == nil {
			return ev
		}
		switch sendInt(ev, "type") {
		case 1, 3, 25: // NSEventTypeLeftMouseDown, RightMouseDown, OtherMouseDown
			send(b.app, "activateIgnoringOtherApps:", 1)
			send(m.win, "makeKeyAndOrderFront:", 0)
		}
		return 0
	})
	modalInput = retain(send(class("NSEvent"), "addLocalMonitorForEventsMatchingMask:handler:", mask, uintptr(blk)))
}

// showFlyout shows a flyout above its parent, as its child window, and
// gives it the keyboard when it takes it. Showing it does not activate
// the app: it shows over a parent the user is using.
func (w *window) showFlyout(key bool) {
	if key {
		w.b.watchOutsideClicks()
		send(w.win, "makeKeyAndOrderFront:", 0)
	} else {
		send(w.win, "orderFront:", 0)
	}
	if p := w.parent; p != nil && !p.closed && send(w.win, "parentWindow") == 0 {
		send(p.win, "addChildWindow:ordered:", uintptr(w.win), 1) // NSWindowAbove
	}
}

// hideFlyout hides a flyout. It stops being a child window, which AppKit
// would order in again with its parent, and gives the keyboard it had
// back to the parent.
func (w *window) hideFlyout() {
	key := sendBool(w.win, "isKeyWindow")
	if p := w.parent; p != nil && !p.closed {
		send(p.win, "removeChildWindow:", uintptr(w.win))
		if key && sendBool(p.win, "isVisible") {
			send(p.win, "makeKeyWindow")
		}
	}
	send(w.win, "orderOut:", 0)
}

// outsideClicks is the monitor of clicks outside the app, in other apps or
// the menu bar, which close focusable flyouts as AppKit's transient
// popovers close: their panels may take the keyboard without the app
// being activated, and a click in the menu bar activates no other app, so
// they would not resign the key window. It is made once, as the first
// focusable flyout shows.
var outsideClicks id

func (b *Backend) watchOutsideClicks() {
	if outsideClicks != 0 {
		return
	}
	const mask = 1<<1 | 1<<3 | 1<<25 // left, right and other mouse down
	blk := newBlock(func(_ objc.Block, ev id) {
		var open []*window
		for _, w := range b.byNSWindow {
			if w.opts.Flyout != nil && w.popover == 0 && w.flyout.Focusable && !w.closed && sendBool(w.win, "isVisible") && w.modalChild() == nil {
				// A press on its tray icon is left to the icon's click,
				// which toggles it, rather than closing it for the click
				// to open it again.
				if t, ok := w.flyout.Tray.(*tray); ok && t.pressed() {
					continue
				}
				open = append(open, w)
			}
		}
		for _, w := range open {
			if !w.closed && w.h.ShouldClose() {
				w.Close()
			}
		}
		b.closeHeldPopovers(0)
	})
	outsideClicks = retain(send(class("NSEvent"), "addGlobalMonitorForEventsMatchingMask:handler:", mask, uintptr(blk)))
	b.watchAnchorPresses()
}

// anchorPresses is the monitor of presses in the app that eats a press on
// the anchor of a focusable flyout, which only closes the flyout, as
// AppKit's transient popovers eat a press on their positioning rect: the
// press would make the parent key, which closes the flyout before the
// release clicks what opened it, which would open it again. The press's
// drags and release are eaten too. Presses elsewhere go on to what is
// under the pointer. It is made once, with outsideClicks.
var anchorPresses id

// eatingPress is the type of the release that ends a press anchorPresses
// ate, or 0. Main thread only.
var eatingPress int

func (b *Backend) watchAnchorPresses() {
	// left, right and other mouse down, up and dragged
	const mask = 1<<1 | 1<<3 | 1<<25 | 1<<2 | 1<<4 | 1<<26 | 1<<6 | 1<<7 | 1<<27
	blk := newBlock(func(_ objc.Block, ev id) id {
		switch typ := sendInt(ev, "type"); typ {
		case 2, 4, 26: // NSEventTypeLeftMouseUp, RightMouseUp, OtherMouseUp
			if typ == eatingPress {
				eatingPress = 0
				return 0
			}
		case 6, 7, 27: // NSEventTypeLeftMouseDragged, RightMouseDragged, OtherMouseDragged
			if eatingPress != 0 {
				return 0
			}
		default: // a press, which ends any other
			eatingPress = 0
			b.closeHeldPopovers(send(ev, "window"))
			if w := b.flyoutAnchoredAt(ev); w != nil {
				eatingPress = typ + 1
				if w.h.ShouldClose() {
					w.Close()
				}
				return 0
			}
		}
		return ev
	})
	anchorPresses = retain(send(class("NSEvent"), "addLocalMonitorForEventsMatchingMask:handler:", mask, uintptr(blk)))
}

// flyoutAnchoredAt returns the focusable flyout that shows in a panel
// whose anchor is under the press ev in its parent, or nil.
func (b *Backend) flyoutAnchoredAt(ev id) *window {
	p := b.byNSWindow[send(ev, "window")]
	if p == nil || p.closed {
		return nil
	}
	frame := msgRect(p.win, sel("frame"))
	c := msgRectToRect(p.win, sel("contentRectForFrameRect:"), frame)
	loc := msgPoint(ev, sel("locationInWindow"))
	x := loc.X - (c.Origin.X - frame.Origin.X)
	y := c.Origin.Y - frame.Origin.Y + c.Size.Height - loc.Y
	for _, w := range b.byNSWindow {
		if w.parent != p || w.opts.Flyout == nil || w.popover != 0 || !w.flyout.Focusable || w.closed || !sendBool(w.win, "isVisible") {
			continue
		}
		if a := w.flyout.Anchor; x >= float64(a.X) && y >= float64(a.Y) && x < float64(a.X+a.Width) && y < float64(a.Y+a.Height) {
			return w
		}
	}
	return nil
}

// dismissFlyout closes a focusable flyout that lost the keyboard, as the
// user closing it, unless it went to a flyout or a window of its own, or
// a window is modal to it.
func (w *window) dismissFlyout() {
	if w.closed || !w.flyout.Focusable || !sendBool(w.win, "isVisible") || w.modalChild() != nil {
		return
	}
	for k := w.b.byNSWindow[send(w.b.app, "keyWindow")]; k != nil; k = k.parent {
		if k == w {
			return
		}
	}
	if w.h.ShouldClose() {
		w.Close()
	}
}

func (w *window) ShowInactive() {
	if w.popover != 0 {
		w.showPopover()
		return
	}
	if w.opts.Flyout != nil {
		w.showFlyout(false)
		return
	}
	if w.modalToFlyout() {
		w.showOverFlyout(false)
		return
	}
	w.showAboveParent(false)
}

func (w *window) Hide() {
	if w.popover != 0 {
		w.hidePopover()
		return
	}
	if w.opts.Flyout != nil {
		w.hideFlyout()
		return
	}
	if w.sheet {
		w.sheet = false
		send(w.parent.win, "endSheet:", uintptr(w.win))
		return
	}
	if w.modalToFlyout() {
		w.leaveFlyout()
		send(w.win, "orderOut:", 0)
		return
	}
	w.hiddenWithParent = false
	// Attached, AppKit would show it again with its parent.
	w.detach()
	send(w.win, "orderOut:", 0)
	w.hideChildren()
}

func (w *window) IsVisible() bool {
	if w.popover != 0 {
		return sendBool(w.popover, "isShown")
	}
	return sendBool(w.win, "isVisible")
}

func (w *window) Focus() {
	if w.host != nil {
		if !w.closed {
			send(w.win, "makeFirstResponder:", uintptr(w.web))
		}
		return
	}
	if w.opts.Flyout != nil {
		w.Show() // the key window only when it takes the keyboard
		return
	}
	w.hiddenWithParent = false
	send(w.win, "makeKeyAndOrderFront:", 0)
	if !w.opts.Panel {
		send(w.b.app, "activateIgnoringOtherApps:", 1)
	}
}

func (w *window) Blur() { send(w.win, "orderBack:", 0) }
func (w *window) IsFocused() bool {
	return sendBool(w.win, "isKeyWindow")
}
func (w *window) Minimize()         { send(w.win, "miniaturize:", 0) }
func (w *window) IsMinimized() bool { return sendBool(w.win, "isMiniaturized") }

func (w *window) Maximize() {
	if !w.IsMaximized() {
		send(w.win, "zoom:", 0)
	}
}

func (w *window) Unmaximize() {
	if w.IsMaximized() {
		send(w.win, "zoom:", 0)
	}
}

func (w *window) IsMaximized() bool { return sendBool(w.win, "isZoomed") && !w.IsFullScreen() }

func (w *window) Restore() {
	if w.IsMinimized() {
		send(w.win, "deminiaturize:", 0)
	}
}

func (w *window) SetFullScreen(v bool) {
	if v != w.IsFullScreen() {
		send(w.win, "toggleFullScreen:", 0)
	}
}

func (w *window) IsFullScreen() bool { return w.hasStyle(styleFullScreen) }
func (w *window) Center()            { send(w.win, "center") }

func (w *window) SetBackgroundColor(c platform.Color) {
	withPool(func() {
		switch {
		case w.host != nil:
			// A web view's window is its host's.
			w.clipBackground(c)
		case w.glassClear:
			// The glass shows what is behind the window, the color once it goes.
			w.systemBacking = false
			release(w.backing)
			w.backing = retain(nsColor(c))
		case w.radius > 0:
			// The window stays clear, its corners cut off (roundContent).
			w.systemBacking = false
			send(send(w.view, "layer"), "setBackgroundColor:", uintptr(send(nsColor(c), "CGColor")))
		default:
			send(w.win, "setBackgroundColor:", uintptr(nsColor(c)))
		}
		send(w.web, "setValue:forKey:", uintptr(nsBool(false)), uintptr(nsString("drawsBackground")))
		if respondsTo(w.web, "setUnderPageBackgroundColor:") {
			send(w.web, "setUnderPageBackgroundColor:", uintptr(nsColor(c)))
		}
	})
}

func (w *window) SetOpacity(v float64) { msgSetFloat(w.win, sel("setAlphaValue:"), v) }

func (w *window) SetProgressBar(state string, value float64) { w.b.setDockProgress(state, value) }

func (w *window) FlashFrame(flash bool) {
	if w.attention != 0 {
		send(w.b.app, "cancelUserAttentionRequest:", uintptr(w.attention))
		w.attention = 0
	}
	if flash {
		w.attention = sendInt(w.b.app, "requestUserAttention:", 10) // NSInformationalRequest
	}
}

// SetSkipTaskbar does nothing: the Dock shows applications, not windows.
func (w *window) SetSkipTaskbar(bool) {}

func (w *window) SetVisibleOnAllWorkspaces(v bool) {
	const canJoinAllSpaces = 1 << 0
	behavior := uint(sendInt(w.win, "collectionBehavior"))
	if v {
		behavior |= canJoinAllSpaces
		// FullScreenNone (DisableFullScreen) also keeps a window out of
		// other apps' full-screen Spaces. As an auxiliary, the window shows
		// there too (an accessory app's: macOS shows no regular app's
		// windows there), and still cannot go full screen itself.
		if behavior&collectionFullScreenNone != 0 {
			behavior = behavior&^collectionFullScreenNone | collectionFullScreenAuxiliary
			w.auxiliary = true
		}
	} else {
		behavior &^= canJoinAllSpaces
		if w.auxiliary {
			behavior = behavior&^collectionFullScreenAuxiliary | collectionFullScreenNone
			w.auxiliary = false
		}
	}
	send(w.win, "setCollectionBehavior:", uintptr(behavior))
}

// SetIcon does nothing: macOS windows show no icon of their own.
func (w *window) SetIcon([]byte) error        { return nil }
func (w *window) Opacity() float64            { return msgFloat(w.win, sel("alphaValue")) }
func (w *window) SetHasShadow(v bool)         { send(w.win, "setHasShadow:", boolArg(v)) }
func (w *window) HasShadow() bool             { return sendBool(w.win, "hasShadow") }
func (w *window) SetIgnoreMouseEvents(v bool) { send(w.win, "setIgnoresMouseEvents:", boolArg(v)) }

func (w *window) SetContentProtection(v bool) {
	sharing := uintptr(1) // NSWindowSharingReadOnly
	if v {
		sharing = 0 // NSWindowSharingNone
	}
	send(w.win, "setSharingType:", sharing)
}

// glassStyles maps the Liquid Glass vibrancies to NSGlassEffectViewStyle,
// and to the NSVisualEffectMaterial closest to each before macOS 26, which
// has no NSGlassEffectView.
var glassStyles = map[string]struct{ style, material uintptr }{
	"glass":       {0, 6},  // regular; popover
	"glass-clear": {1, 13}, // clear; HUD
}

// vibrancyMaterials maps mygo.Vibrancy values to NSVisualEffectMaterial.
var vibrancyMaterials = map[string]uintptr{
	"titlebar": 3, "selection": 4, "menu": 5, "popover": 6, "sidebar": 7, "header": 10,
	"sheet": 11, "window": 12, "hud": 13, "fullscreen-ui": 15, "tooltip": 17, "content": 18,
	"under-window": 21, "under-page": 22,
	// Windows 11 backdrops.
	"mica": 21, "tabbed": 21, "acrylic": 13,
}

func (w *window) SetVibrancy(material string) {
	withPool(func() {
		m, ok := vibrancyMaterials[material]
		g, isGlass := glassStyles[material]
		glass := isGlass && hasClass("NSGlassEffectView")
		if isGlass && !glass {
			m, ok = g.material, true
		}
		if !ok && !glass {
			if material != "" {
				log.Printf("mygo: unknown vibrancy %q", material)
			}
			w.removeEffect()
			return
		}
		if w.effect != 0 && w.glass != glass {
			w.removeEffect()
		}
		if w.effect == 0 {
			cls := "NSVisualEffectView"
			if glass {
				cls = "NSGlassEffectView"
			}
			w.effect = msgInitRect(send(class(cls), "alloc"), sel("initWithFrame:"), msgRect(w.view, sel("bounds")))
			w.glass = glass
			if !glass {
				send(w.effect, "setBlendingMode:", 0) // NSVisualEffectBlendingModeBehindWindow
				send(w.effect, "setState:", w.effectState())
			}
			send(w.effect, "setAutoresizingMask:", nsViewWidthHeightSizable)
			// Behind the page and a docked inspector.
			send(w.view, "addSubview:positioned:relativeTo:", uintptr(w.effect), ^uintptr(0), 0) // NSWindowBelow
			send(w.web, "setValue:forKey:", uintptr(nsBool(false)), uintptr(nsString("drawsBackground")))
			w.roundEffect()
			if glass {
				w.clearForGlass()
			}
		}
		if glass {
			send(w.effect, "setStyle:", g.style)
			w.applyTint()
		} else {
			send(w.effect, "setMaterial:", m)
		}
	})
}

// SetActiveAppearance has the window drawn as the main window of the app
// in front, whichever is, through AppKit's own, private
// _setForceMainAppearance:. Without it, a window that is not draws its glass
// frosted, the clear style too, as a panel floating over another app always
// is. NSVisualEffectView has a public setting for it (its state, which
// SetVibrancy sets); NSGlassEffectView has none.
func (w *window) SetActiveAppearance(v bool) {
	w.keepActive = v
	if respondsTo(w.win, "_setForceMainAppearance:") {
		send(w.win, "_setForceMainAppearance:", boolArg(v))
	}
	if w.effect != 0 && !w.glass {
		send(w.effect, "setState:", w.effectState())
	}
}

// effectState returns the NSVisualEffectState of the window's material: a
// window's follows its active state, as AppKit's sidebars do, and fades to
// an opaque gray while the window is not the one the user's input goes to.
// Panels and flyouts float over whatever app is in front, never active as
// AppKit tells it, and keep theirs active, as SetActiveAppearance keeps a
// window's.
func (w *window) effectState() uintptr {
	if w.keepActive || w.opts.Panel || w.opts.Flyout != nil {
		return 1 // NSVisualEffectStateActive
	}
	return 0 // NSVisualEffectStateFollowsWindowActiveState
}

// SetWindowButtonsTheme does nothing: the traffic lights look the same in
// both appearances, and an appearance of the window's own would change its
// page's too.
func (w *window) SetWindowButtonsTheme(string) {}

// SetVibrancyTint tints the glass, nil to remove the tint. Other
// materials take none.
func (w *window) SetVibrancyTint(c *platform.Color) {
	w.tint = c
	if w.effect != 0 && w.glass {
		withPool(w.applyTint)
	}
}

// applyTint sets the glass's tintColor from w.tint. It runs in a pool.
func (w *window) applyTint() {
	if w.effect == 0 || !w.glass {
		return
	}
	var color uintptr
	if w.tint != nil {
		color = uintptr(nsColor(*w.tint))
	}
	send(w.effect, "setTintColor:", color)
}

// removeEffect takes the material out of the window.
func (w *window) removeEffect() {
	if w.effect != 0 {
		send(w.effect, "removeFromSuperview")
		release(w.effect)
		w.effect, w.glass = 0, false
		w.restoreBacking()
	}
}

// clearForGlass makes a window that is not transparent clear while it
// shows the glass. NSGlassEffectView draws what is behind it in the
// window, which is the window's own color in an opaque one, where
// NSVisualEffectView blends with what is behind the window. The color of
// a rounded window is on its content view (roundContent). A popover's
// window is AppKit's.
func (w *window) clearForGlass() {
	if w.glassClear || w.opts.Transparent || w.popover != 0 {
		return
	}
	w.glassClear = true
	if w.radius > 0 {
		layer := send(w.view, "layer")
		if c := send(layer, "backgroundColor"); c != 0 && !w.systemBacking {
			w.backing = retain(send(class("NSColor"), "colorWithCGColor:", uintptr(c)))
		}
		send(layer, "setBackgroundColor:", 0)
	} else {
		w.backing = retain(send(w.win, "backgroundColor"))
		send(w.win, "setOpaque:", 0)
		send(w.win, "setBackgroundColor:", uintptr(send(class("NSColor"), "clearColor")))
	}
	send(w.win, "invalidateShadow")
}

// restoreBacking gives the window back the color clearForGlass took.
func (w *window) restoreBacking() {
	if !w.glassClear {
		return
	}
	w.glassClear = false
	switch {
	case w.radius > 0 && w.systemBacking:
		w.fillSystemBacking()
	case w.radius > 0:
		send(send(w.view, "layer"), "setBackgroundColor:", uintptr(send(w.backing, "CGColor")))
	default:
		send(w.win, "setBackgroundColor:", uintptr(w.backing))
		send(w.win, "setOpaque:", 1)
	}
	release(w.backing)
	w.backing = 0
	send(w.win, "invalidateShadow")
}

func (w *window) SetMenu(*platform.Menu) {}

// SetAutoHideMenu does nothing: the menu bar belongs to the application.
func (w *window) SetAutoHideMenu(bool) {}

// hiddenTitleBar reports a TitleBarStyle that hides the title bar but keeps
// the traffic lights.
func (w *window) hiddenTitleBar() bool {
	o := w.opts
	return !o.Frameless && (o.TitleBarStyle == "hidden" || o.TitleBarStyle == "hiddenInset")
}

// TitleBar returns the room the traffic lights take, from the left edge to
// the right edge of the zoom button, in the title bar, which is as tall as
// the window's own or, with TrafficLightPosition, leaves as much room below
// them as above. They hide in full screen, and with HideWindowButtons.
func (w *window) TitleBar() platform.TitleBar {
	if !w.hiddenTitleBar() || w.closed || w.IsFullScreen() || w.opts.HideWindowButtons {
		return platform.TitleBar{}
	}
	closeBtn := send(w.win, "standardWindowButton:", 0)
	zoom := send(w.win, "standardWindowButton:", 2)
	if closeBtn == 0 || zoom == 0 {
		return platform.TitleBar{}
	}
	c, z := msgRect(closeBtn, sel("frame")), msgRect(zoom, sel("frame"))
	if p := w.trafficLights; p != nil {
		// Where layoutTrafficLights puts them, which may not have run yet.
		right := float64(p.X) + z.Origin.X - c.Origin.X + z.Size.Width
		return platform.TitleBar{Height: int(math.Round(c.Size.Height + 2*float64(p.Y))), Left: int(math.Ceil(right))}
	}
	frame := msgRect(w.win, sel("frame"))
	content := msgRect(w.win, sel("contentLayoutRect"))
	height := frame.Size.Height - content.Origin.Y - content.Size.Height
	return platform.TitleBar{Height: int(math.Round(height)), Left: int(math.Ceil(z.Origin.X + z.Size.Width))}
}

func (w *window) TitleBarDoubleClicked() {
	var action string
	withPool(func() {
		defaults := send(class("NSUserDefaults"), "standardUserDefaults")
		action = goString(send(defaults, "stringForKey:", uintptr(nsString("AppleActionOnDoubleClick"))))
	})
	switch action {
	case "None":
	case "Minimize":
		send(w.win, "performMiniaturize:", 0)
	default:
		send(w.win, "performZoom:", 0)
	}
}

func (w *window) Close() {
	if w.host != nil {
		w.closeWebView()
		return
	}
	if w.closed {
		return
	}
	if w.popover != 0 {
		w.closePopover()
		return
	}
	if w.sheet {
		w.sheet = false
		send(w.parent.win, "endSheet:", uintptr(w.win))
	}
	if w.opts.Flyout != nil {
		w.hideFlyout()
	}
	send(w.win, "close")
}

// layoutTrafficLights puts the top-left corner of the close button at the
// requested position, and the other buttons after it, in a title bar that
// leaves as much room below them as above. AppKit lays the title bar out
// again when the window resizes, its title changes or the appearance
// changes, so this runs after each of them.
func (w *window) layoutTrafficLights() {
	p := w.trafficLights
	if p == nil || w.closed || w.IsFullScreen() {
		return
	}
	closeBtn := send(w.win, "standardWindowButton:", 0)
	if closeBtn == 0 {
		return
	}
	container := send(send(closeBtn, "superview"), "superview")
	if container == 0 {
		return
	}
	btnFrame := msgRect(closeBtn, sel("frame"))
	height := btnFrame.Size.Height + 2*float64(p.Y)
	winFrame := msgRect(w.win, sel("frame"))
	cf := msgRect(container, sel("frame"))
	cf.Size.Height = height
	cf.Origin.Y = winFrame.Size.Height - height
	msgSetRect(container, sel("setFrame:"), cf)

	mini := send(w.win, "standardWindowButton:", 1)
	gap := msgRect(mini, sel("frame")).Origin.X - btnFrame.Origin.X
	for i := uintptr(0); i < 3; i++ {
		btn := send(w.win, "standardWindowButton:", i)
		msgSetPoint(btn, sel("setFrameOrigin:"), NSPoint{float64(p.X) + float64(i)*gap, float64(p.Y)})
	}
}

// Navigation.

func (w *window) LoadURL(url string) {
	w.programmatic = true
	withPool(func() {
		req := send(class("NSURLRequest"), "requestWithURL:", uintptr(nsURL(url)))
		send(w.web, "loadRequest:", uintptr(req))
	})
}

func (w *window) LoadHTML(html, baseURL string) {
	w.programmatic = true
	withPool(func() {
		var base id
		if baseURL != "" {
			base = nsURL(baseURL)
		}
		send(w.web, "loadHTMLString:baseURL:", uintptr(nsString(html)), uintptr(base))
	})
}

func (w *window) LoadFile(path, readAccessDir string) {
	w.programmatic = true
	withPool(func() {
		send(w.web, "loadFileURL:allowingReadAccessToURL:", uintptr(fileURL(path)), uintptr(fileURL(readAccessDir)))
	})
}

func (w *window) Reload(ignoreCache bool) {
	if ignoreCache {
		send(w.web, "reloadFromOrigin")
	} else {
		send(w.web, "reload")
	}
}

func (w *window) StopLoading()       { send(w.web, "stopLoading") }
func (w *window) GoBack()            { send(w.web, "goBack") }
func (w *window) GoForward()         { send(w.web, "goForward") }
func (w *window) CanGoBack() bool    { return sendBool(w.web, "canGoBack") }
func (w *window) CanGoForward() bool { return sendBool(w.web, "canGoForward") }
func (w *window) IsLoading() bool    { return sendBool(w.web, "isLoading") }

func (w *window) URL() string {
	var s string
	withPool(func() { s = goString(send(send(w.web, "URL"), "absoluteString")) })
	return s
}

// addUserScripts adds the user scripts of the options to the content
// controller, and the title bar's after them. Pool required.
func (w *window) addUserScripts(zoom float64) {
	add := func(src string, injection uintptr, mainFrameOnly bool) {
		script := send(send(class("WKUserScript"), "alloc"), "initWithSource:injectionTime:forMainFrameOnly:",
			uintptr(nsString(src)), injection, boolArg(mainFrameOnly))
		send(w.ucc, "addUserScript:", uintptr(script))
		release(script)
	}
	for _, s := range w.opts.UserScripts {
		injection := uintptr(0) // WKUserScriptInjectionTimeAtDocumentStart
		if s.AtDocumentEnd {
			injection = 1
		}
		add(s.Source, injection, !s.AllFrames)
	}
	if w.opts.PictureInPicture {
		add(pipScript, 0, true)
	}
	if w.hiddenTitleBar() {
		// After the bridge, which it tells: the room of the traffic lights.
		add(bridge.TitleBarScript(w.TitleBar(), zoom), 0, true)
	}
}

// SetUserScripts replaces the user scripts: a content controller can only
// remove all of its scripts, so they are all added again.
func (w *window) SetUserScripts(scripts []platform.UserScript) {
	if w.ucc == 0 || w.closed {
		return
	}
	w.opts.UserScripts = scripts
	withPool(func() {
		send(w.ucc, "removeAllUserScripts")
		w.addUserScripts(w.Zoom())
	})
}

func (w *window) Eval(js string) {
	withPool(func() {
		send(w.web, "evaluateJavaScript:completionHandler:", uintptr(nsString(js)), 0)
	})
}

func (w *window) CallAsyncFunction(body string, cb func(string, error)) {
	if !respondsTo(w.web, "callAsyncJavaScript:arguments:inFrame:inContentWorld:completionHandler:") {
		cb("", errors.New("mygo: Eval requires macOS 11 or later"))
		return
	}
	withPool(func() {
		blk := newBlock(func(_ objc.Block, result id, err id) {
			if err != 0 {
				cb("", nsError(err))
				return
			}
			cb(goString(result), nil)
		})
		defer blk.Release()
		send(w.web, "callAsyncJavaScript:arguments:inFrame:inContentWorld:completionHandler:",
			uintptr(nsString(body)), 0, 0, uintptr(send(class("WKContentWorld"), "pageWorld")), uintptr(blk))
	})
}

func (w *window) SetZoom(f float64) { msgSetFloat(w.web, sel("setPageZoom:"), f) }
func (w *window) Zoom() float64     { return msgFloat(w.web, sel("pageZoom")) }

func (w *window) SetUserAgent(ua string) {
	withPool(func() {
		var s id
		if ua != "" {
			if suffix := w.opts.UserAgentSuffix; suffix != "" {
				ua += " " + suffix
			}
			s = nsString(ua)
		}
		// An empty one restores the default user agent, which the
		// configuration still appends its suffix to.
		send(w.web, "setCustomUserAgent:", uintptr(s))
	})
}

func (w *window) UserAgent() string {
	var s string
	withPool(func() {
		s = goString(send(w.web, "customUserAgent"))
		if s == "" && respondsTo(w.web, "_userAgent") {
			s = goString(send(w.web, "_userAgent"))
		}
	})
	return s
}

// ProcessID returns the web content process of the page from WebKit's
// private -[WKWebView _webProcessIdentifier] (a pid_t), 0 when WebKit
// doesn't answer it or runs no process for the page yet.
func (w *window) ProcessID() int {
	if !respondsTo(w.web, "_webProcessIdentifier") {
		return 0
	}
	return int(int32(send(w.web, "_webProcessIdentifier")))
}

// GPUProcessID returns WebKit's GPU process from its private
// -[WKWebView _gpuProcessIdentifier] (a pid_t), 0 when WebKit doesn't
// answer it or hasn't started the process yet.
func (w *window) GPUProcessID() int {
	if !respondsTo(w.web, "_gpuProcessIdentifier") {
		return 0
	}
	return int(int32(send(w.web, "_gpuProcessIdentifier")))
}

// IsAudible reports WebKit's private -[WKWebView _isPlayingAudio]
// (macOS 10.13.4+), false when WebKit doesn't answer it.
func (w *window) IsAudible() bool {
	return respondsTo(w.web, "_isPlayingAudio") && sendBool(w.web, "_isPlayingAudio")
}

// wkMediaAudioMuted is _WKMediaAudioMuted, the bit of _WKMediaMutedState
// (WKWebViewPrivate.h) that mutes the page's sound; the others mute its
// capture devices, and are kept as they are.
const wkMediaAudioMuted = 1 << 0

// canMute reports whether WebKit answers the private selectors that mute
// a page, as Safari mutes a tab (macOS 10.13+).
func (w *window) canMute() bool {
	return respondsTo(w.web, "_mediaMutedState") && respondsTo(w.web, "_setPageMuted:")
}

// wkMediaCaptureDevicesMuted is _WKMediaCaptureDevicesMuted, the bit of
// _WKMediaMutedState that _mediaMutedState sets when the camera or the
// microphone is muted, and with which _setPageMuted: mutes both.
const wkMediaCaptureDevicesMuted = 1 << 1

// SetAudioMuted sets the _WKMediaAudioMuted bit of the page's
// _mediaMutedState with _setPageMuted:, or does nothing when WebKit
// doesn't answer them. The other bits are kept, but one bit stands for
// the camera and the microphone: when only one of them is muted
// (captureMuted), the other is unmuted again after _setPageMuted: muted
// both, so that the muted one is never live in between.
func (w *window) SetAudioMuted(v bool) {
	if !w.canMute() {
		return
	}
	state := uintptr(send(w.web, "_mediaMutedState"))
	if v {
		state |= wkMediaAudioMuted
	} else {
		state &^= wkMediaAudioMuted
	}
	send(w.web, "_setPageMuted:", state)
	camera, microphone := w.captureMuted[platform.CaptureCamera], w.captureMuted[platform.CaptureMicrophone]
	if state&wkMediaCaptureDevicesMuted != 0 && camera != microphone {
		live := platform.CaptureCamera
		if camera {
			live = platform.CaptureMicrophone
		}
		w.setCaptureState(live, platform.CaptureActive)
	}
}

func (w *window) IsAudioMuted() bool {
	return w.canMute() && uintptr(send(w.web, "_mediaMutedState"))&wkMediaAudioMuted != 0
}

// captureKeys are the properties of WKWebView that tell the page's
// capture, by platform.CaptureKind: the camera's and the microphone's
// (macOS 12+), and WebKit's private _displayCaptureState (macOS 13+), all
// key-value observable. Their values are WKMediaCaptureState's and
// WKDisplayCaptureState's, none, active and muted, as
// platform.CaptureState's.
var captureKeys = [...]string{"cameraCaptureState", "microphoneCaptureState", "_displayCaptureState"}

// captureSetters change the capture of captureKeys: their completion
// handler may be nil.
var captureSetters = [...]string{
	"setCameraCaptureState:completionHandler:",
	"setMicrophoneCaptureState:completionHandler:",
	"_setDisplayCaptureState:completionHandler:",
}

// CaptureState reports the captureKeys property of kind, CaptureNone when
// WebKit doesn't answer it.
func (w *window) CaptureState(kind platform.CaptureKind) platform.CaptureState {
	if !respondsTo(w.web, captureKeys[kind]) {
		return platform.CaptureNone
	}
	return platform.CaptureState(send(w.web, captureKeys[kind]))
}

// SetCaptureState changes the page's capture of kind with its
// captureSetters method, while the page captures such a device, as
// WebKitGTK's setters do: WebKit would otherwise keep a mute set before
// the page captures. It does nothing when WebKit doesn't answer it.
func (w *window) SetCaptureState(kind platform.CaptureKind, state platform.CaptureState) {
	if w.CaptureState(kind) == platform.CaptureNone {
		return
	}
	w.captureMuted[kind] = state == platform.CaptureMuted
	w.setCaptureState(kind, state)
}

func (w *window) setCaptureState(kind platform.CaptureKind, state platform.CaptureState) {
	if respondsTo(w.web, captureSetters[kind]) {
		send(w.web, captureSetters[kind], uintptr(state), 0)
	}
}

// captureChanged tells the handler of the page's capture of kind, which
// WebKit observed changing.
func (w *window) captureChanged(kind platform.CaptureKind) {
	state := w.CaptureState(kind)
	if state == platform.CaptureNone {
		w.captureMuted[kind] = false
	}
	w.h.CaptureStateChanged(kind, state)
}

// TogglePictureInPicture moves the page's video into picture in picture,
// or back, with WebKit's private -[WKWebView _togglePictureInPicture]
// when _canTogglePictureInPicture says it can, else with the page's
// script, which evaluateJavaScript runs as a user's gesture: it says no on
// a Mac without a Touch Bar (a MacBook Air on macOS 27), where the script
// works. It does nothing in a page without the option, where WebKit
// refuses picture in picture.
func (w *window) TogglePictureInPicture() {
	if !w.opts.PictureInPicture {
		return
	}
	if respondsTo(w.web, "_canTogglePictureInPicture") && respondsTo(w.web, "_togglePictureInPicture") &&
		sendBool(w.web, "_canTogglePictureInPicture") {
		send(w.web, "_togglePictureInPicture")
		return
	}
	w.Eval(platform.TogglePictureInPicture)
}

// isPictureInPictureActive reports WebKit's private
// -[WKWebView _isPictureInPictureActive], false when WebKit doesn't
// answer it.
func (w *window) isPictureInPictureActive() bool {
	return respondsTo(w.web, "_isPictureInPictureActive") && sendBool(w.web, "_isPictureInPictureActive")
}

// PictureInPictureActive tells WebKit's _isPictureInPictureActive, which
// knows of picture in picture through its media controls only, else the
// page's document.pictureInPictureElement, which the script's sets.
func (w *window) PictureInPictureActive(cb func(bool)) {
	if w.isPictureInPictureActive() || !w.opts.PictureInPicture || w.closed {
		cb(w.isPictureInPictureActive())
		return
	}
	w.CallAsyncFunction(platform.PictureInPictureActive, func(s string, err error) {
		cb(err == nil && s == "true")
	})
}

// pipHandler is the name of the script message handler that pipScript
// tells whether the page's document has a video in picture in picture.
const pipHandler = "mygoPictureInPicture"

// pipScript tells pipHandler, as the page shows and as a video of its
// document goes into picture in picture or out of it, whether one is in.
// WebKit's _isPictureInPictureActive knows only of its media controls'
// picture in picture, not of requestPictureInPicture().
const pipScript = `(() => {
	const h = window.webkit.messageHandlers.` + pipHandler + `;
	const tell = () => h.postMessage(String(!!document.pictureInPictureElement));
	addEventListener("enterpictureinpicture", tell, true);
	addEventListener("leavepictureinpicture", tell, true);
	addEventListener("pageshow", tell);
})()`

// leavingPage runs as the page is about to navigate. WebKit restores a
// page from its back/forward cache believing that the video it had in
// picture in picture still is, so a page leaving with one is not kept
// there: WebKit's private -[WKPreferences _setUsesPageCache:] turns the
// cache off until a page leaves without one. Pages of windows without
// PictureInPicture are cached as usual.
func (w *window) leavingPage() {
	if !w.opts.PictureInPicture || w.closed {
		return
	}
	off := w.pipActive || w.isPictureInPictureActive()
	if off == w.pageCacheOff {
		return
	}
	prefs := send(send(w.web, "configuration"), "preferences")
	if !respondsTo(prefs, "_setUsesPageCache:") {
		return
	}
	w.pageCacheOff = off
	send(prefs, "_setUsesPageCache:", boolArg(!off))
}

// SessionState returns WKWebView's interactionState (macOS 12+), which
// is NSData of WebKit's own encoding of the back/forward list (its
// _sessionStateData, which Safari keeps), nil before macOS 12 or when
// WebKit gives something else.
func (w *window) SessionState() []byte {
	if !respondsTo(w.web, "interactionState") {
		return nil
	}
	var b []byte
	withPool(func() {
		s := send(w.web, "interactionState")
		if s != 0 && sendBool(s, "isKindOfClass:", uintptr(class("NSData"))) {
			b = goBytes(s)
		}
	})
	return b
}

// RestoreSessionState sets WKWebView's interactionState, which restores
// the back/forward list and goes to its current item. WebKit ignores data
// it can't decode, which leaves the list without a current item.
func (w *window) RestoreSessionState(state []byte) bool {
	if !respondsTo(w.web, "setInteractionState:") {
		return false
	}
	var ok bool
	withPool(func() {
		send(w.web, "setInteractionState:", uintptr(nsData(state)))
		item := send(send(w.web, "backForwardList"), "currentItem")
		ok = item != 0
		if ok && !occlusionVisible(w.win) && respondsTo(item, "_scrollPosition") {
			w.restoredScroll = msgPoint(item, sel("_scrollPosition"))
		}
	})
	return ok
}

// keepRestoredScroll keeps the scroll position WebKit restored as the
// page RestoreSessionState brought back loaded, which WebKit puts back at
// the top when the page's window first shows on screen after that (macOS
// 26): in a window made hidden, and in one shown as it opens when the
// page loads before the window server puts the window on screen. A
// script in a world of its own puts the page back at its position when
// the page next scrolls, to the top, before the user did anything; any
// other scroll, the user's included, ends it. The position is the
// private -[WKBackForwardListItem _scrollPosition] of the current item
// RestoreSessionState left, looked up with respondsToSelector:; a page
// whose history.scrollRestoration is "manual" keeps where it is.
func (w *window) keepRestoredScroll() {
	p := w.restoredScroll
	w.restoredScroll = NSPoint{}
	if p == (NSPoint{}) || w.closed {
		return
	}
	js := fmt.Sprintf(keepScrollScript, int(p.X), int(p.Y))
	withPool(func() {
		send(w.web, "evaluateJavaScript:inFrame:inContentWorld:completionHandler:",
			uintptr(nsString(js)), 0, uintptr(send(class("WKContentWorld"), "defaultClientWorld")), 0)
	})
}

// keepScrollScript is keepRestoredScroll's, of the position x, y.
const keepScrollScript = `(() => {
	const x = %d, y = %d, inputs = ["keydown", "pointerdown", "wheel", "touchstart"];
	if (history.scrollRestoration !== "auto") return;
	const back = () => scrollX === 0 && scrollY === 0 && scrollTo(x, y);
	const done = () => {
		removeEventListener("scroll", scrolled);
		for (const t of inputs) removeEventListener(t, done, true);
	};
	const scrolled = () => {
		if (scrollX !== x || scrollY !== y) {
			back();
			done();
		}
	};
	back();
	addEventListener("scroll", scrolled);
	for (const t of inputs) addEventListener(t, done, true);
})()`

// occlusionVisible tells that some of a window shows on screen, as the
// window server last said.
func occlusionVisible(win id) bool {
	const occlusionStateVisible = 1 << 1
	return sendInt(win, "occlusionState")&occlusionStateVisible != 0
}

func (w *window) inspector() id {
	if !w.opts.DevTools || !respondsTo(w.web, "_inspector") {
		return 0
	}
	return send(w.web, "_inspector")
}

func (w *window) OpenDevTools() {
	if in := w.inspector(); in != 0 {
		send(in, "show")
	}
}

func (w *window) CloseDevTools() {
	if in := w.inspector(); in != 0 {
		send(in, "close")
	}
}

func (w *window) IsDevToolsOpened() bool {
	in := w.inspector()
	return in != 0 && respondsTo(in, "isVisible") && sendBool(in, "isVisible")
}

func (w *window) CapturePage(cb func([]byte, error)) {
	withPool(func() {
		blk := newBlock(func(_ objc.Block, img id, err id) {
			if err != 0 || img == 0 {
				cb(nil, nsError(err))
				return
			}
			withPool(func() { cb(pngFromImage(img), nil) })
		})
		defer blk.Release()
		send(w.web, "takeSnapshotWithConfiguration:completionHandler:", 0, uintptr(blk))
	})
}

// pngFromImage encodes an NSImage as PNG.
func pngFromImage(img id) []byte {
	tiff := send(img, "TIFFRepresentation")
	rep := send(class("NSBitmapImageRep"), "imageRepWithData:", uintptr(tiff))
	if rep == 0 {
		return nil
	}
	data := send(rep, "representationUsingType:properties:", 4, uintptr(send(class("NSDictionary"), "dictionary")))
	return goBytes(data)
}

// securityOrigin renders a WKSecurityOrigin like a URL origin.
func securityOrigin(o id) string {
	origin := goString(send(o, "protocol")) + "://" + goString(send(o, "host"))
	if port := sendInt(o, "port"); port != 0 {
		origin += ":" + strconv.Itoa(port)
	}
	return origin
}

// printJob is a print operation of PrintToPDF, by its context in
// printJobs until it ends. One whose window closes may never end: WebKit
// leaves it waiting for the page, so the window's cleanup gives cb
// errDestroyed, and done releases what the job holds if it ends after all.
type printJob struct {
	w    *window
	cb   func(pdf []byte, err error) // nil once it got its result
	done func(success bool)
}

var printJobs = map[uintptr]*printJob{}

var errDestroyed = errors.New("mygo: window has been destroyed")

func (w *window) PrintToPDF(o platform.PDFOptions, cb func([]byte, error)) {
	if !respondsTo(w.web, "printOperationWithPrintInfo:") {
		cb(nil, errors.New("mygo: printing to PDF needs macOS 11 or later"))
		return
	}
	f, err := os.CreateTemp("", "mygo-*.pdf")
	if err != nil {
		cb(nil, err)
		return
	}
	path := f.Name()
	f.Close()
	withPool(func() {
		info := autorelease(send(send(class("NSPrintInfo"), "sharedPrintInfo"), "copy"))
		send(info, "setJobDisposition:", uintptr(appKitString("NSPrintSaveJob")))
		url := send(class("NSURL"), "fileURLWithPath:", uintptr(nsString(path)))
		send(send(info, "dictionary"), "setObject:forKey:", uintptr(url), uintptr(appKitString("NSPrintJobSavingURL")))
		const points = 72
		width, height := o.PageWidth, o.PageHeight
		orientation := 0 // NSPaperOrientationPortrait
		if o.Landscape {
			width, height, orientation = height, width, 1
		}
		msgSetSize(info, sel("setPaperSize:"), NSSize{width * points, height * points})
		send(info, "setOrientation:", uintptr(orientation))
		msgSetFloat(info, sel("setTopMargin:"), o.MarginTop*points)
		msgSetFloat(info, sel("setRightMargin:"), o.MarginRight*points)
		msgSetFloat(info, sel("setBottomMargin:"), o.MarginBottom*points)
		msgSetFloat(info, sel("setLeftMargin:"), o.MarginLeft*points)
		send(info, "setHorizontalPagination:", 1) // fit the width, like browsers
		send(info, "setVerticalPagination:", 0)   // as many pages as it takes
		send(info, "setHorizontallyCentered:", 0)
		send(info, "setVerticallyCentered:", 0)
		// The preferences are shared with the configuration's copy. They are
		// kept for the job, which may end after the window closed.
		prefs := retain(send(send(w.web, "configuration"), "preferences"))
		backgrounds := respondsTo(prefs, "setShouldPrintBackgrounds:") // macOS 13.3
		printedBackgrounds := false
		if backgrounds {
			printedBackgrounds = sendBool(prefs, "shouldPrintBackgrounds")
			send(prefs, "setShouldPrintBackgrounds:", boolArg(o.Background))
		}
		// Kept until it ran: nothing else holds on to it.
		op := retain(send(w.web, "printOperationWithPrintInfo:", uintptr(info)))
		send(op, "setShowsPrintPanel:", 0)
		send(op, "setShowsProgressPanel:", 0)
		msgSetRect(send(op, "view"), sel("setFrame:"), msgRect(w.web, sel("bounds")))
		job := uintptr(len(printJobs) + 1)
		for printJobs[job] != nil {
			job++
		}
		j := &printJob{w: w, cb: cb}
		j.done = func(success bool) {
			release(op)
			if backgrounds {
				send(prefs, "setShouldPrintBackgrounds:", boolArg(printedBackgrounds))
			}
			release(prefs)
			data, err := os.ReadFile(path)
			os.Remove(path)
			if j.cb == nil {
				return
			}
			if err == nil && (!success || len(data) == 0) {
				err = errors.New("mygo: printing to PDF failed")
			}
			j.cb(data, err)
		}
		printJobs[job] = j
		// The app's delegate hears of the end, as the window's may be gone.
		send(op, "runOperationModalForWindow:delegate:didRunSelector:contextInfo:", uintptr(w.win), uintptr(w.b.delegate),
			uintptr(sel("mygoPrintOperationDidRun:success:contextInfo:")), job)
	})
}

func (w *window) Print() {
	if !respondsTo(w.web, "printOperationWithPrintInfo:") {
		return
	}
	withPool(func() {
		info := send(class("NSPrintInfo"), "sharedPrintInfo")
		// Kept until it ran: nothing else holds on to it.
		op := retain(send(w.web, "printOperationWithPrintInfo:", uintptr(info)))
		view := send(op, "view")
		msgSetRect(view, sel("setFrame:"), msgRect(w.web, sel("bounds")))
		send(op, "runOperationModalForWindow:delegate:didRunSelector:contextInfo:", uintptr(w.win), 0, 0, 0)
	})
}

// Coordinates: AppKit puts the origin at the bottom-left of the primary
// screen, MyGo at its top-left.

func primaryScreenHeight() float64 {
	screens := send(class("NSScreen"), "screens")
	if sendInt(screens, "count") == 0 {
		return 0
	}
	return msgRect(send(screens, "objectAtIndex:", 0), sel("frame")).Size.Height
}

func rectFromMac(r NSRect) platform.Rect {
	h := primaryScreenHeight()
	return platform.Rect{
		X:      int(math.Round(r.Origin.X)),
		Y:      int(math.Round(h - r.Origin.Y - r.Size.Height)),
		Width:  int(math.Round(r.Size.Width)),
		Height: int(math.Round(r.Size.Height)),
	}
}

func rectToMac(r platform.Rect) NSRect {
	h := primaryScreenHeight()
	return NSRect{
		Origin: NSPoint{float64(r.X), h - float64(r.Y) - float64(r.Height)},
		Size:   NSSize{float64(r.Width), float64(r.Height)},
	}
}

// draggedFiles returns the paths of the files on the pasteboard of a
// drag.
func draggedFiles(info id) []string {
	var paths []string
	withPool(func() {
		opts := send(class("NSDictionary"), "dictionaryWithObject:forKey:", uintptr(nsBool(true)), uintptr(nsString("NSPasteboardURLReadingFileURLsOnlyKey")))
		urls := send(send(info, "draggingPasteboard"), "readObjectsForClasses:options:", uintptr(nsArray(class("NSURL"))), uintptr(opts))
		for _, u := range arrayItems(urls) {
			if p := goString(send(send(u, "filePathURL"), "path")); p != "" {
				paths = append(paths, p)
			}
		}
	})
	return paths
}

func (w *window) DroppedFiles() []string {
	paths := w.dropped
	w.dropped = nil
	return paths
}

func (b *Backend) windowFor(delegate id) *window {
	w := b.byDelegate[delegate]
	if w == nil || w.closed {
		return nil
	}
	return w
}

func registerWindowClasses() {
	// WebKit moves the keyboard out of a page past its ends through the
	// window's key view loop: a web view in native UI gives it back to
	// the content.
	selectKeyView := func(class string, back bool) func(id, objc.SEL, id) {
		return func(self id, cmd objc.SEL, view id) {
			if w := webViewOf(view); w != nil && w.host != nil {
				w.tabOut(back)
				return
			}
			sendSuper(self, class, cmd, uintptr(view))
		}
	}
	classDef("MyGoWindow", "NSWindow", nil, []objc.MethodDef{
		method("orderWindow:relativeTo:", orderWindowRelativeTo("MyGoWindow")),
		method("canBecomeKeyWindow", func(self id, _ objc.SEL) bool { return true }),
		method("canBecomeMainWindow", func(self id, _ objc.SEL) bool { return true }),
		method("selectKeyViewFollowingView:", selectKeyView("MyGoWindow", false)),
		method("selectKeyViewPrecedingView:", selectKeyView("MyGoWindow", true)),
	})

	// A panel (WindowOptions.Panel) takes the keyboard, without
	// activating the app, and is never the main window.
	classDef("MyGoPanel", "NSPanel", nil, []objc.MethodDef{
		method("orderWindow:relativeTo:", orderWindowRelativeTo("MyGoPanel")),
		method("canBecomeKeyWindow", func(self id, _ objc.SEL) bool { return true }),
		method("canBecomeMainWindow", func(self id, _ objc.SEL) bool { return false }),
		// NSPanel closes on an Escape its content leaves (cancelOperation:),
		// past windowShouldClose: MyGo closes it then, as the user closing
		// it, which OnClose listeners may prevent.
		method("cancelOperation:", func(self id, _ objc.SEL, sender id) {}),
		method("selectKeyViewFollowingView:", selectKeyView("MyGoPanel", false)),
		method("selectKeyViewPrecedingView:", selectKeyView("MyGoPanel", true)),
	})

	// A flyout's panel becomes the key window only when it takes the
	// keyboard, and never the main window: its parent stays it.
	classDef("MyGoFlyout", "NSPanel", nil, []objc.MethodDef{
		method("canBecomeKeyWindow", func(self id, _ objc.SEL) bool {
			w := theBackend.byNSWindow[self]
			return w != nil && w.flyout.Focusable
		}),
		method("canBecomeMainWindow", func(self id, _ objc.SEL) bool { return false }),
		method("selectKeyViewFollowingView:", selectKeyView("MyGoFlyout", false)),
		method("selectKeyViewPrecedingView:", selectKeyView("MyGoFlyout", true)),
	})

	registerClipViewClass()
	classDef("MyGoWebView", "WKWebView", nil, append(append(webViewMethods(), contextMenuMethods()...), []objc.MethodDef{
		method("mouseDown:", func(self id, cmd objc.SEL, ev id) {
			if w := theBackend.byWebView[self]; w != nil {
				if w.host != nil {
					w.pressed(ev, 0)
				}
				// A web view's page drags the window it is in (StartDrag).
				w.top().pressedLeft(ev)
			}
			sendSuper(self, "MyGoWebView", cmd, uintptr(ev))
		}),
		// Files dropped on the page: the page gets File objects, the app
		// their paths.
		method("performDragOperation:", func(self id, cmd objc.SEL, info id) bool {
			if w := theBackend.byWebView[self]; w != nil {
				w.dropped = draggedFiles(info)
			}
			return byte(sendSuper(self, "MyGoWebView", cmd, uintptr(info))) != 0
		}),
	}...))

	b := func() *Backend { return theBackend }
	classDef("MyGoWindowDelegate", "NSObject",
		[]string{"NSWindowDelegate", "WKNavigationDelegate", "WKUIDelegate", "WKScriptMessageHandler", "WKDownloadDelegate", "NSPopoverDelegate"},
		append(popoverDelegateMethods(), []objc.MethodDef{
			// NSWindowDelegate
			method("windowShouldClose:", func(self id, _ objc.SEL, sender id) bool {
				if w := b().windowFor(self); w != nil {
					return w.h.ShouldClose()
				}
				return true
			}),
			// WKUIDelegate (macOS 12): camera and microphone requests.
			method("webView:requestMediaCapturePermissionForOrigin:initiatedByFrame:type:decisionHandler:", func(self id, _ objc.SEL, web, origin, frame id, typ int, handler uintptr) {
				const grant, deny = 1, 2 // WKPermissionDecision
				decision := deny
				if w := b().windowFor(self); w != nil {
					kinds := map[int][]string{0: {"camera"}, 1: {"microphone"}, 2: {"camera", "microphone"}}[typ]
					if w.h.PermissionRequested(kinds, securityOrigin(origin)) {
						decision = grant
					}
				}
				callBlock(handler, uintptr(decision))
			}),
			method("windowWillClose:", func(self id, _ objc.SEL, n id) {
				if w := b().windowFor(self); w != nil {
					w.cleanup()
					w.h.Closed()
				}
			}),
			method("windowDidBecomeKey:", func(self id, _ objc.SEL, n id) {
				if w := b().windowFor(self); w != nil {
					w.orderChildrenLater()
					w.h.Focused()
					w.surfaceKeyChanged(true)
				}
			}),
			method("windowDidResignKey:", func(self id, _ objc.SEL, n id) {
				if w := b().windowFor(self); w != nil {
					w.h.Blurred()
					w.surfaceKeyChanged(false)
					if w.opts.Flyout != nil && w.popover == 0 {
						// Once AppKit made another window key, or none,
						// unless the press on its tray icon toggles it.
						if t, ok := w.flyout.Tray.(*tray); !ok || !t.pressed() {
							b().post(w.dismissFlyout)
						}
					}
				}
			}),
			method("windowDidResize:", func(self id, _ objc.SEL, n id) {
				w := b().windowFor(self)
				if w == nil {
					return
				}
				w.layoutTrafficLights()
				if z := w.IsMaximized(); z != w.maximized {
					w.maximized = z
					if z {
						w.h.Maximized()
					} else {
						w.h.Unmaximized()
					}
				}
				w.h.Resized()
			}),
			method("windowWillMove:", func(self id, _ objc.SEL, n id) {
				// The window server begins to move the window: a drag
				// region's press is its (window_drag.go).
				if w := b().windowFor(self); w != nil {
					theDrag.willMove(w.win)
				}
			}),
			method("windowDidMove:", func(self id, _ objc.SEL, n id) {
				if w := b().windowFor(self); w != nil {
					w.h.Moved()
				}
			}),
			method("windowDidChangeOcclusionState:", func(self id, _ objc.SEL, n id) {
				if w := b().windowFor(self); w != nil && w.surface != nil {
					kind := platform.SurfaceShown
					if !w.surface.visible() {
						kind = platform.SurfaceHidden
					}
					w.surface.send(platform.SurfaceEvent{Kind: kind})
				}
			}),
			method("windowDidMiniaturize:", func(self id, _ objc.SEL, n id) {
				if w := b().windowFor(self); w != nil {
					w.hideChildren()
					w.h.Minimized()
				}
			}),
			method("windowDidDeminiaturize:", func(self id, _ objc.SEL, n id) {
				if w := b().windowFor(self); w != nil {
					w.orderChildrenLater()
					w.h.Restored()
				}
			}),
			// The toolbar that insets the traffic lights holds no items: in
			// full screen it hides with the menu bar instead of covering the
			// top of the page.
			method("window:willUseFullScreenPresentationOptions:", func(self id, _ objc.SEL, win id, proposed uint) uint {
				const autoHide = presentationFullScreen | presentationAutoHideMenuBar
				if send(win, "toolbar") != 0 && proposed&autoHide == autoHide {
					return proposed | presentationAutoHideToolbar
				}
				return proposed
			}),
			// The child windows follow their parent into its full-screen
			// Space, and are left where they are as it comes out of it.
			method("windowWillEnterFullScreen:", func(self id, _ objc.SEL, n id) {
				if w := b().windowFor(self); w != nil {
					w.childrenAttached = true
					w.attachChildren()
				}
			}),
			method("windowDidFailToEnterFullScreen:", func(self id, _ objc.SEL, win id) {
				if w := b().windowFor(self); w != nil {
					w.leftFullScreen()
				}
			}),
			method("windowDidEnterFullScreen:", func(self id, _ objc.SEL, n id) {
				if w := b().windowFor(self); w != nil {
					w.h.EnteredFullScreen()
					if w.hiddenTitleBar() {
						w.h.TitleBarChanged() // the traffic lights hid
					}
				}
			}),
			method("windowDidExitFullScreen:", func(self id, _ objc.SEL, n id) {
				if w := b().windowFor(self); w != nil {
					w.leftFullScreen()
					w.layoutTrafficLights()
					w.h.LeftFullScreen()
					if w.hiddenTitleBar() {
						w.h.TitleBarChanged()
					}
				}
			}),

			// WKScriptMessageHandler
			method("userContentController:didReceiveScriptMessage:", func(self id, _ objc.SEL, ucc, msg id) {
				w := b().windowFor(self)
				if w == nil {
					return
				}
				// The handler is reachable from every frame; only the
				// page's own bridge may talk to Go.
				if frame := send(msg, "frameInfo"); frame == 0 || !sendBool(frame, "isMainFrame") {
					return
				}
				body := send(msg, "body")
				if body == 0 || !sendBool(body, "isKindOfClass:", uintptr(class("NSString"))) {
					return
				}
				if goString(send(msg, "name")) == pipHandler {
					w.pipActive = goString(body) == "true"
					return
				}
				w.h.Message(goString(body))
			}),

			// Key-value observing of the page title, its sound and
			// capture, and the window's appearance.
			method("observeValueForKeyPath:ofObject:change:context:", func(self id, _ objc.SEL, keyPath, object, change id, ctx uintptr) {
				w := b().windowFor(self)
				if w == nil {
					return
				}
				switch goString(keyPath) {
				case "title":
					w.h.TitleChanged(goString(send(w.web, "title")))
				case "_isPlayingAudio":
					w.h.AudibleChanged(w.IsAudible())
				case "cameraCaptureState":
					w.captureChanged(platform.CaptureCamera)
				case "microphoneCaptureState":
					w.captureChanged(platform.CaptureMicrophone)
				case "_displayCaptureState":
					w.captureChanged(platform.CaptureDisplay)
				case "effectiveAppearance":
					// AppKit lays the title bar out again after telling us.
					b().post(w.layoutTrafficLights)
				}
			}),

			// WKNavigationDelegate
			method("webView:decidePolicyForNavigationAction:decisionHandler:", func(self id, _ objc.SEL, web, action id, handler uintptr) {
				// A link with the download attribute (macOS 11.3).
				if respondsTo(action, "shouldPerformDownload") && sendBool(action, "shouldPerformDownload") {
					callBlock(handler, 2) // WKNavigationActionPolicyDownload
					return
				}
				allow := true
				if w := b().windowFor(self); w != nil {
					frame := send(action, "targetFrame")
					navType := sendInt(action, "navigationType")
					// WKNavigationType: link, form, back/forward, reload, …
					nav := platform.Navigation{
						URL:           goString(send(send(send(action, "request"), "URL"), "absoluteString")),
						IsMainFrame:   frame != 0 && sendBool(frame, "isMainFrame"),
						UserInitiated: userInitiated(action, navType),
						Unasked:       navType == 2 || navType == 3,
						Reload:        navType == 3,
					}
					if nav.IsMainFrame && w.programmatic {
						w.programmatic = false
						nav.Unasked = true
					}
					allow = w.h.WillNavigate(nav)
					if allow && nav.IsMainFrame {
						w.leavingPage()
					}
				}
				callBlock(handler, boolArg(allow))
			}),
			// Responses the page cannot show, or sent as attachments, are
			// downloads (macOS 11.3).
			method("webView:decidePolicyForNavigationResponse:decisionHandler:", func(self id, _ objc.SEL, web, nav id, handler uintptr) {
				const cancel, allow, download = 0, 1, 2 // WKNavigationResponsePolicy
				resp := send(nav, "response")
				url := goString(send(send(resp, "URL"), "absoluteString"))
				disposition, custom := schemeDispositions[url]
				if !custom && respondsTo(resp, "allHeaderFields") {
					disposition = goString(send(send(resp, "allHeaderFields"), "objectForKey:", uintptr(nsString("Content-Disposition"))))
				}
				attachment := strings.HasPrefix(strings.ToLower(strings.TrimSpace(disposition)), "attachment")
				switch {
				case !attachment && sendBool(nav, "canShowMIMEType"):
					callBlock(handler, allow)
				case custom:
					// WebKit cannot turn what a scheme handler serves into a
					// download: serve it again into one.
					callBlock(handler, cancel)
					if w := b().windowFor(self); w != nil {
						w.h.SchemeDownload(url)
					}
				case hasClass("WKDownload"):
					callBlock(handler, download)
				default:
					callBlock(handler, cancel)
				}
			}),
			method("webView:navigationAction:didBecomeDownload:", func(self id, _ objc.SEL, web, action, download id) {
				send(download, "setDelegate:", uintptr(self))
			}),
			method("webView:navigationResponse:didBecomeDownload:", func(self id, _ objc.SEL, web, nav, download id) {
				send(download, "setDelegate:", uintptr(self))
			}),
			// WKDownloadDelegate.
			method("download:decideDestinationUsingResponse:suggestedFilename:completionHandler:", func(self id, _ objc.SEL, download, resp, suggested id, handler uintptr) {
				w := b().windowFor(self)
				if w == nil {
					callBlock(handler, 0)
					return
				}
				url := goString(send(send(send(download, "originalRequest"), "URL"), "absoluteString"))
				path := w.h.DownloadStarted(url, goString(suggested))
				if path == "" {
					callBlock(handler, 0) // cancels the download
					return
				}
				os.Remove(path) // WebKit refuses to replace a file
				w.downloads[download] = [2]string{url, path}
				withPool(func() { callBlock(handler, uintptr(send(class("NSURL"), "fileURLWithPath:", uintptr(nsString(path))))) })
			}),
			method("downloadDidFinish:", func(self id, _ objc.SEL, download id) {
				if w := b().windowFor(self); w != nil {
					if d, ok := w.downloads[download]; ok {
						delete(w.downloads, download)
						w.h.DownloadFinished(d[0], d[1], nil)
					}
				}
			}),
			method("download:didFailWithError:resumeData:", func(self id, _ objc.SEL, download, nserr, resume id) {
				if w := b().windowFor(self); w != nil {
					if d, ok := w.downloads[download]; ok {
						delete(w.downloads, download)
						w.h.DownloadFinished(d[0], d[1], errors.New(goString(send(nserr, "localizedDescription"))))
					}
				}
			}),
			method("webView:didStartProvisionalNavigation:", func(self id, _ objc.SEL, web, nav id) {
				if w := b().windowFor(self); w != nil {
					w.provisionalURL = w.URL()
					w.h.NavigationStarted(w.provisionalURL)
				}
			}),
			method("webView:didCommitNavigation:", func(self id, _ objc.SEL, web, nav id) {
				if w := b().windowFor(self); w != nil {
					w.h.NavigationCommitted(w.URL())
				}
			}),
			method("webView:didFinishNavigation:", func(self id, _ objc.SEL, web, nav id) {
				if w := b().windowFor(self); w != nil {
					w.keepRestoredScroll()
					w.h.LoadFinished()
				}
			}),
			method("webView:didFailProvisionalNavigation:withError:", func(self id, _ objc.SEL, web, nav, err id) {
				if w := b().windowFor(self); w != nil {
					w.restoredScroll = NSPoint{}
					w.loadFailed(err, w.provisionalURL)
				}
			}),
			method("webView:didFailNavigation:withError:", func(self id, _ objc.SEL, web, nav, err id) {
				if w := b().windowFor(self); w != nil {
					w.restoredScroll = NSPoint{}
					w.loadFailed(err, w.URL())
				}
			}),
			// HTTP sign-in and server certificates (auth.go).
			method("webView:didReceiveAuthenticationChallenge:completionHandler:", func(self id, _ objc.SEL, web, challenge id, handler uintptr) {
				if w := b().windowFor(self); w != nil {
					w.authChallenge(challenge, handler)
				} else {
					callBlock(handler, authDefault, 0)
				}
			}),
			method("webViewWebContentProcessDidTerminate:", func(self id, _ objc.SEL, web id) {
				if w := b().windowFor(self); w != nil {
					w.h.RenderProcessGone("terminated")
				}
			}),

			// WKUIDelegate
			method("webView:createWebViewWithConfiguration:forNavigationAction:windowFeatures:", func(self id, _ objc.SEL, web, cfg, action, features id) id {
				w := b().windowFor(self)
				if w == nil {
					return 0
				}
				num := func(key string) int {
					if n := send(features, key); n != 0 {
						return sendInt(n, "integerValue")
					}
					return 0
				}
				req := platform.NewWindowRequest{
					URL:    goString(send(send(send(action, "request"), "URL"), "absoluteString")),
					X:      num("x"),
					Y:      num("y"),
					Width:  num("width"),
					Height: num("height"),
					// The action of the link, or of window.open(), whose
					// WKNavigationType is "other".
					UserInitiated: userInitiated(action, sendInt(action, "navigationType")),
					Native:        uintptr(cfg),
				}
				child, ok := w.h.NewWindow(req).(*window)
				if !ok || child == nil {
					return 0
				}
				return child.web
			}),
			method("webViewDidClose:", func(self id, _ objc.SEL, web id) {
				if w := b().windowFor(self); w != nil {
					w.h.ClosedByPage()
				}
			}),
			method("webView:runJavaScriptAlertPanelWithMessage:initiatedByFrame:completionHandler:", func(self id, _ objc.SEL, web, message, frame id, handler uintptr) {
				w := b().windowFor(self)
				jsAlert(w, goString(message), handler)
			}),
			method("webView:runJavaScriptConfirmPanelWithMessage:initiatedByFrame:completionHandler:", func(self id, _ objc.SEL, web, message, frame id, handler uintptr) {
				w := b().windowFor(self)
				jsConfirm(w, goString(message), handler)
			}),
			method("webView:runJavaScriptTextInputPanelWithPrompt:defaultText:initiatedByFrame:completionHandler:", func(self id, _ objc.SEL, web, prompt, defaultText, frame id, handler uintptr) {
				w := b().windowFor(self)
				jsPrompt(w, goString(prompt), goString(defaultText), handler)
			}),
			method("webView:runOpenPanelWithParameters:initiatedByFrame:completionHandler:", func(self id, _ objc.SEL, web, params, frame id, handler uintptr) {
				w := b().windowFor(self)
				jsFileInput(w, params, handler)
			}),
		}...))
}

// userInitiated reports whether a user's gesture started a navigation
// action. WebKit's private -[WKNavigationAction _isUserInitiated] knows,
// a click whose handler navigates or calls window.open() included;
// without it, the action's WKNavigationType says, a link or a form,
// which misses those.
func userInitiated(action id, navType int) bool {
	if respondsTo(action, "_isUserInitiated") {
		return sendBool(action, "_isUserInitiated")
	}
	return navType == 0 || navType == 1 // WKNavigationTypeLinkActivated, FormSubmitted
}

// loadFailed reports a failed load of the URL its error names: the
// string or the NSURL, which is all WebKit keeps of a certificate error;
// else of fallback.
func (w *window) loadFailed(err id, fallback string) {
	code := sendInt(err, "code")
	// Cancelled (-999) and "frame load interrupted" (102) happen when a
	// navigation is replaced or denied; they are not failures.
	if code == -999 || code == 102 {
		return
	}
	var url, desc string
	withPool(func() {
		info := send(err, "userInfo")
		url = goString(send(info, "objectForKey:", uintptr(nsString("NSErrorFailingURLStringKey"))))
		if u := send(info, "objectForKey:", uintptr(nsString("NSErrorFailingURLKey"))); url == "" && u != 0 && sendBool(u, "isKindOfClass:", uintptr(class("NSURL"))) {
			url = goString(send(u, "absoluteString"))
		}
		desc = goString(send(err, "localizedDescription"))
	})
	if url == "" {
		url = fallback
	}
	w.h.LoadFailed(url, code, desc)
}
