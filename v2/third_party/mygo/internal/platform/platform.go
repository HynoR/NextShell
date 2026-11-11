// Package platform defines the contract between the public mygo API and the
// native backends (WKWebView on macOS, WebKitGTK on Linux, WebView2 on
// Windows).
//
// Threading rules: unless a method says otherwise it must be called on the
// main (UI) thread, and every handler callback is invoked on the main thread.
// The public package is responsible for hopping onto the main thread.
package platform

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/egoist/mygo/transfer"
)

// ErrUnsupported is returned by features the current backend cannot provide.
// It is errors.ErrUnsupported too (errors.Is), which apps check.
var ErrUnsupported error = unsupportedError{}

type unsupportedError struct{}

func (unsupportedError) Error() string        { return "not supported on this platform" }
func (unsupportedError) Is(target error) bool { return target == errors.ErrUnsupported }

// ErrNotificationsDenied is what ShowNotification fails with when the user
// does not allow the app to show notifications.
var ErrNotificationsDenied = errors.New("mygo: the user does not allow notifications")

// Point is a position in screen coordinates (DIPs, origin top-left of the
// primary display).
type Point struct{ X, Y int }

// Size is a width and height in DIPs.
type Size struct{ Width, Height int }

// Rect is a rectangle in screen coordinates (DIPs).
type Rect struct{ X, Y, Width, Height int }

// TitleBar is the room the window controls take at the top of a window
// with a hidden title bar, in DIPs: they sit in a band of Height along the
// top edge, Left wide from the left edge and Right wide from the right.
type TitleBar struct{ Height, Left, Right int }

// Color is an 8-bit RGBA color.
type Color struct{ R, G, B, A uint8 }

// Backend is implemented by every native backend.
type Backend interface {
	// Name identifies the backend, for example "darwin/wkwebview".
	Name() string

	// Init prepares the native application. It is called once, on the main
	// thread, before Run.
	Init(h AppHandler, opts AppOptions) error
	// Run runs the native event loop until Quit is called. h.Ready is
	// invoked once the application has finished launching.
	Run() error
	// Quit makes Run return.
	Quit()

	// IsMainThread reports whether the caller runs on the UI thread. Safe
	// to call from any goroutine.
	IsMainThread() bool
	// Signal asks the backend to invoke AppHandler.Dispatch on the main
	// thread as soon as possible. Safe to call from any goroutine.
	Signal()
	// Step blocks until at least one native event has been processed or
	// Wake has been called. Main thread only; used for nested waits.
	Step()
	// Wake unblocks a pending Step. Safe to call from any goroutine.
	Wake()

	NewWindow(opts *WindowOptions, h WindowHandler) (Window, error)

	SetApplicationMenu(m *Menu)
	// PopupMenu shows a context menu. pos is relative to the window's
	// content area; nil means the current mouse location. It blocks until
	// the menu is dismissed.
	PopupMenu(m *Menu, w Window, pos *Point)
	// UpdateMenuItem refreshes label/state of every native item created
	// for item.ID.
	UpdateMenuItem(item *MenuItem)

	App() AppController
	Dialogs() Dialogs
	Clipboard() Clipboard
	Shell() Shell
	Screen() Screen
	Theme() Theme
	Power() Power
	Network() Network
	NowPlaying() NowPlaying
	// Secrets may be called before Init, from any goroutine.
	Secrets() Secrets
	// Applications lists the applications installed on the computer.
	// Unlike other methods it may be called from any goroutine once Init
	// returned: it reads the disk, which the core does off the main thread.
	Applications() ([]Application, error)

	// NewFileWatch starts watching files, with no target yet. notify is
	// called on a goroutine of the backend, one notice at a time.
	// NewFileWatch may be called before Init, from any goroutine.
	NewFileWatch(notify func(FileWatchNotice)) (FileWatch, error)
	// ReplaceFile replaces the file at target with the file at
	// replacement, in the same directory, keeping what the system keeps of
	// the target's metadata (permissions, owner, extended attributes,
	// creation date); replacement is gone afterwards. It may be called
	// before Init, from any goroutine.
	ReplaceFile(target, replacement string) error

	NewTray(h TrayHandler) (Tray, error)
	RegisterHotkey(id int, accelerator string) error
	UnregisterHotkey(id int)

	NotificationsSupported() bool
	// ShowNotification shows n; done runs on the main thread once the
	// system has it, or with why it could not.
	ShowNotification(n *Notification, done func(error))
	RemoveNotification(id string)
	// RemoveAllNotifications removes the app's notifications, on macOS
	// those of earlier runs too.
	RemoveAllNotifications()

	// WebViewAvailable reports whether the system webview is there, and so
	// whether windows showing web pages can work: WKWebView on macOS,
	// WebKitGTK on Linux, the WebView2 Runtime on Windows. A window showing
	// native UI needs none, so an app whose windows all do runs without one.
	// It may be called before Init, from any goroutine.
	WebViewAvailable() bool
}

// WindowStateWindow is a window whose bounds in DIPs change with the
// scales of the displays where they differ (Windows).
// WindowStateBounds is its rectangle in the screen's pixels, which window
// states keep with them.
type WindowStateWindow interface {
	WindowStateBounds() Rect
}

// WindowStateBackend restores windows at their pixels (Windows):
// RestoreWindowState sets opts.StateBounds and returns true when pixels,
// zero for none, would show on a connected display. Otherwise the core
// restores the state's DIPs where every display has the same scale, and
// centers the window where they have not.
type WindowStateBackend interface {
	RestoreWindowState(opts *WindowOptions, pixels Rect) bool
}

// StyleBackend styles the widgets the toolkit draws for the app with the
// app's own style sheet (Linux: GTK's CSS, Theme.SetGTKCSS). SetStyleSheet
// replaces the sheet set before, or removes it for "", and keeps that one
// when css does not parse, returning why.
type StyleBackend interface {
	SetStyleSheet(css string) error
}

// WindowButtonsBackend reports the window buttons the desktop puts at the
// start and at the end of title bars (Linux): "minimize", "maximize" and
// "close", in their order, none where the desktop wants none. Main thread
// only.
type WindowButtonsBackend interface {
	WindowButtons() (start, end []string)
}

// AppOptions configures Backend.Init.
type AppOptions struct {
	Name string
	// ActivationPolicy is "regular" (default), "accessory" or "prohibited".
	// Only meaningful on macOS.
	ActivationPolicy string
	// DMABUFRenderer is "off" (or ""), "on" or "auto": whether WebKitGTK
	// may draw with its DMA-BUF renderer. Only meaningful on Linux.
	DMABUFRenderer string
}

// AppHandler receives application level events from the backend.
type AppHandler interface {
	Ready()
	// Dispatch drains the queue of functions scheduled for the main thread.
	Dispatch()
	// QuitRequested is called when the user or the OS asks the application
	// to quit (Cmd+Q, Dock > Quit, logout). Returning true lets the quit
	// proceed.
	QuitRequested() bool
	// Terminating is called right before the process is terminated by the
	// OS (logout/shutdown); Run will not return.
	Terminating()
	// Activated is called when the application is re-activated, e.g. by
	// clicking its Dock icon.
	Activated(hasVisibleWindows bool)
	DidBecomeActive()
	DidResignActive()
	OpenURLs(urls []string)
	OpenFiles(paths []string)
	MenuItemClicked(id int)
	ThemeChanged()
	DisplaysChanged()
	// LocaleChanged is called when the languages the user prefers, or the
	// regional formats, may have changed, on the systems that report it.
	LocaleChanged()
	// PowerEvent is "suspend", "resume", "lock-screen" or "unlock-screen".
	PowerEvent(event string)
	// NetworkChanged reports the network's status, once Network.Watch was
	// called, when it may have changed.
	NetworkChanged(status NetworkStatus)
	HotkeyPressed(id int)
	NotificationClicked(id string)
	// MediaCommand reports a media key or a remote command for what
	// NowPlaying shows; position is where MediaSeek asks to go.
	MediaCommand(cmd MediaCommand, position time.Duration)
}

// AppController exposes application level native features.
type AppController interface {
	SetActivationPolicy(policy string)
	Activate()
	// AllowForeground lets the next process to ask bring its windows to
	// the front, which Windows only lets the process in front do.
	AllowForeground()
	// WithActivationToken runs fn with the startup notification or XDG
	// activation token of the launch that caused it, which the window fn
	// shows or focuses first may need to take the focus (Wayland).
	WithActivationToken(token string, fn func())
	Hide()
	Unhide()
	IsHidden() bool
	// IsActive reports whether the app is the active one: in front on
	// macOS, with a window that has the keyboard elsewhere.
	IsActive() bool
	SetBadge(label string)
	Badge() string
	// Bounce requests user attention and returns an id for CancelBounce.
	Bounce(critical bool) int
	CancelBounce(id int)
	SetDockIcon(png []byte) error
	// SetDockMenu sets the menu of the Dock icon (macOS); nil removes it.
	SetDockMenu(m *Menu)
	// NewDataStore makes a website data store other than the default one
	// (mygo.Session), which WindowOptions.DataStore and the calls below
	// take. The core makes one store per session and closes it with
	// DataStore.Close.
	NewDataStore(opts DataStoreOptions) (DataStore, error)
	// ClearBrowsingData deletes the data of types pages stored in store,
	// nil for the default one; done runs on the main thread.
	ClearBrowsingData(store DataStore, types SiteData, done func(error))
	// Cookie operations address store, nil for the default page store.
	// Calls and exactly-once completions run on main; ListCookies returns
	// independent Go values: every cookie, or with an http or https url
	// the cookies a request to it sends (CookieMatchesURL where the engine
	// cannot tell).
	ListCookies(store DataStore, url string, done func([]Cookie, error))
	SetCookie(store DataStore, cookie Cookie, done func(error))
	DeleteCookie(store DataStore, key CookieKey, done func(error))
	ClearCookies(store DataStore, done func(error))
	// CompileContentRules makes a content rule list of rules, in WebKit's
	// content blocker JSON format: it loads the list kept as identifier
	// (ContentRulesIdentifier) where the engine keeps compiled lists,
	// else compiles rules, keeps the list as identifier and removes the
	// lists kept for id under other identifiers (StaleContentRules).
	// done runs on the main thread exactly once, with a handle
	// Page.SetContentRules takes.
	CompileContentRules(id, identifier, rules string, done func(list any, err error))
	ShowAboutPanel(opts AboutPanelOptions)
	// PreferredLanguages returns the languages the user reads, most
	// preferred first, as the system's settings list them, in the forms
	// the system writes them (the core normalizes them): never what the
	// app chose for itself (SaveLocale). Safe before Init.
	PreferredLanguages() []string
	// Region returns the user's regional formats. Safe before Init.
	Region() Region
	// SavedLocale returns the language the app chose for itself, as the
	// system keeps it, and whether the system keeps one at all (macOS:
	// AppleLanguages in the app's own defaults, which System Settings
	// shows as the app's language; browsers: localStorage). Where it
	// keeps none, the core keeps it in a file. Safe before Init.
	SavedLocale() (tag string, kept bool)
	// SaveLocale keeps tag where SavedLocale reads it, "" to forget it,
	// and reports whether the system keeps one at all.
	SaveLocale(tag string) bool
	// SetLanguages tells the backend the languages the app shows, its
	// own language first, and whether it is laid out right to left, for
	// what shows text the core does not draw: the web views it creates
	// later and, where it can change, those it created, and the system's
	// own texts and widgets.
	SetLanguages(tags []string, rtl bool)
	// RegisterURLScheme makes the app, identified by id (its identifier)
	// and named name, the handler of the URLs of scheme for the user.
	RegisterURLScheme(scheme, id, name string) error
	UnregisterURLScheme(scheme, id, name string) error
	IsURLSchemeRegistered(scheme, id, name string) bool
	// SetOpenAtLogin makes the app start at login, passing arg where the
	// start command takes arguments; OpenedAtLogin reports whether the
	// system started it so without one (macOS login items).
	SetOpenAtLogin(open bool, id, name, arg string) error
	OpenAtLogin(id, name, arg string) bool
	OpenedAtLogin() bool
	// Package describes the application bundle the process runs from; ok
	// is false for a plain executable (e.g. `go run`). Safe to call from
	// any goroutine, before Init too.
	Package() (info PackageInfo, ok bool)
	// AddRecentDocument records the absolute path of a document the user
	// opened in the system's recent documents; ClearRecentDocuments
	// removes the ones the app recorded.
	AddRecentDocument(path string) error
	ClearRecentDocuments() error
	// SetJumpList commits the app's whole jump list (Windows); elsewhere
	// it does nothing.
	SetJumpList(list JumpList) error
}

// Region is the user's regional formats, as the system's settings have
// them with the user's changes.
type Region struct {
	// Locale is the locale of the formats, in the form the system writes
	// it, "" when unknown.
	Locale string
	// FirstWeekday is the first day of the week, 0 for Sunday to 6 for
	// Saturday, or -1 when the system does not say.
	FirstWeekday int
	// HourCycle is 12 or 24, or 0 when the system does not say.
	HourCycle int
}

// PackageInfo is metadata of a packaged application.
type PackageInfo struct {
	Name       string
	Version    string
	Identifier string
}

// AboutPanelOptions configures the standard about panel.
type AboutPanelOptions struct {
	ApplicationName    string
	ApplicationVersion string
	Version            string
	Copyright          string
	Credits            string
}

// JumpList is the whole jump list of the app: Windows replaces all of it
// at each commit.
type JumpList struct {
	Tasks []JumpListTask
}

// JumpListTask is a task of a jump list: a program to run with its
// arguments, unquoted, its label, tooltip and icon.
type JumpListTask struct {
	Title       string
	Description string
	Path        string
	Args        []string
	WorkingDir  string
	IconPath    string
	IconIndex   int
}

// WindowOptions configures a native window and its webview. The public
// package fills in all defaults before calling NewWindow.
type WindowOptions struct {
	Title          string
	X, Y           int
	Width, Height  int
	Center         bool
	UseContentSize bool
	MinSize        Size
	MaxSize        Size
	// StateBounds is the window's restored normal rectangle in the
	// screen's pixels, set by WindowStateBackend. It overrides X, Y,
	// Width and Height.
	StateBounds *Rect

	Resizable      bool
	Movable        bool
	Minimizable    bool
	Maximizable    bool
	Closable       bool
	Focusable      bool
	Fullscreenable bool
	AlwaysOnTop    bool
	FullScreen     bool
	Maximized      bool
	SkipTaskbar    bool
	AutoHideMenu   bool
	HasShadow      bool
	Frameless      bool
	Transparent    bool
	// BackgroundColor is painted before the page renders; nil keeps the
	// platform default.
	BackgroundColor *Color
	// TitleBarStyle is "default", "hidden" or "hiddenInset". A hidden title
	// bar gives the page the whole window, under the window controls:
	// AppKit's traffic lights on macOS, and on Linux and Windows controls
	// the backend puts over the page (see Window.TitleBar).
	TitleBarStyle string
	// TitleBarHeight is the height of a hidden title bar in DIPs (Linux,
	// Windows); 0 is the backend's own.
	TitleBarHeight int
	// HideWindowButtons hides the window controls of a hidden title bar,
	// which the page draws itself: TitleBar is then zero.
	HideWindowButtons    bool
	TrafficLightPosition *Point
	Vibrancy             string
	// VibrancyTint tints the material, where the platform's material takes
	// one (macOS 26's Liquid Glass): nil for none.
	VibrancyTint *Color
	// CornerRadius rounds a frameless window's corners, in DIPs: its
	// content and material are cut to them (macOS), or the system rounds
	// it with its own corners (Windows 11).
	CornerRadius int
	Opacity      float64
	Parent       Window
	Modal        bool
	// Flyout makes the window a flyout of its Parent, placed next to an
	// anchor (Window.PlaceFlyout), at Width and Height: borderless,
	// transparent, out of the taskbar, with Focusable telling whether it
	// takes the keyboard. Show of a flyout that is not focusable shows it
	// without the keyboard, as ShowInactive.
	Flyout *Flyout
	// Panel makes the window a panel (mygo.WindowOptions.Panel): it floats
	// above the windows of all apps on the current workspace, full-screen
	// ones included, out of the taskbar and the app switcher, and on macOS
	// takes the keyboard without activating the app.
	Panel bool

	// UserScripts are injected into every page, in order.
	UserScripts []UserScript
	// Schemes lists the custom protocols the webview must route to
	// WindowHandler.SchemeRequest.
	Schemes  []string
	DevTools bool
	// UserAgent overrides the user agent of the webview.
	UserAgent string
	// UserAgentSuffix is appended to UserAgent, or to the webview's own
	// user agent, after a space. The public package trims it.
	UserAgentSuffix string
	Zoom            float64
	// TabFocusesLinks makes Tab reach links and form controls that are not
	// text fields (macOS, WKPreferences.tabFocusesLinks); the other
	// platforms reach them already.
	TabFocusesLinks bool
	// NoBackgroundThrottling keeps the page running at full speed while
	// its window is hidden, minimized or covered
	// (mygo.PageOptions.BackgroundThrottling): its timers unthrottled, its
	// process at the priority of a visible one.
	NoBackgroundThrottling bool
	// Autoplay lets the page play audible media without a user gesture
	// (mygo.PageOptions.Autoplay).
	Autoplay bool
	// PictureInPicture lets the page's videos go into picture in picture
	// (macOS, WebKit's private _allowsPictureInPictureMediaPlayback);
	// WebView2 lets them already, and WebKitGTK has none.
	PictureInPicture bool
	// SwipeNavigation lets a swipe go back and forward through the page's
	// history (mygo.PageOptions.SwipeNavigation).
	SwipeNavigation bool
	// PinchZoom lets a pinch magnify the page; the backends turn it off
	// where their webview has it on by default (mygo.PageOptions.PinchZoom).
	PinchZoom bool
	// DataStore is the website data store of the page, one of
	// AppController.NewDataStore; nil is the default store. A window
	// opened by window.open has its opener's.
	DataStore DataStore
	// Native carries backend specific state, e.g. the WKWebViewConfiguration
	// WebKit hands us for window.open().
	Native uintptr

	// ScrollToContainer scrolls the native container around an embedded web
	// view instead of its page. Ordinary pages keep their own wheel input.
	ScrollToContainer bool
	// Surface creates the window with a Surface, which content MyGo draws
	// itself fills, instead of a webview: Window.Surface returns it, the
	// webview methods do nothing and the webview options are ignored.
	Surface bool
}

// DataStoreOptions describe a website data store (AppController.NewDataStore).
type DataStoreOptions struct {
	// ID identifies a persistent store from one run to the next: a UUID
	// in lowercase hex with dashes, which the core derives from the
	// session's name. It is empty for an ephemeral store.
	ID string
	// Ephemeral keeps the store's data in memory, where it goes with the
	// store.
	Ephemeral bool
	// DataDir and CacheDir are where a persistent store keeps its data and
	// its caches, for the backends that choose (Linux).
	DataDir, CacheDir string
}

// DataStore is a website data store a backend made: cookies, local and
// session storage, IndexedDB, caches and service workers that the pages
// using it share, apart from those of other stores.
type DataStore interface {
	// Close lets the store go once the backend no longer needs it: the
	// core calls it when no page uses an ephemeral store, whose data then
	// goes, and makes another store for the session's next page. Calls
	// under way finish.
	Close()
}

// SiteData are kinds of data pages store, for ClearBrowsingData.
type SiteData uint

const (
	SiteDataCookies SiteData = 1 << iota
	// SiteDataLocalStorage is localStorage and sessionStorage.
	SiteDataLocalStorage
	// SiteDataIndexedDB is IndexedDB, and WebSQL where the engine has it.
	SiteDataIndexedDB
	// SiteDataCaches is the HTTP caches, and Cache Storage.
	SiteDataCaches
	SiteDataServiceWorkers
	// SiteDataAll is all the data the engine keeps for sites, the kinds
	// above and the others (HSTS, media keys, …).
	SiteDataAll SiteData = 1<<iota - 1 | 1<<31
)

// All reports whether t asks for all the data, SiteDataAll.
func (t SiteData) All() bool { return t&SiteDataAll == SiteDataAll }

// UserScript is JavaScript injected into pages.
type UserScript struct {
	Source string
	// AtDocumentEnd injects after the DOM is parsed instead of before any
	// page script runs.
	AtDocumentEnd bool
	// AllFrames injects into iframes too.
	AllFrames bool
}

// Window is a native window hosting a webview.
type Window interface {
	// Handle returns the native window (NSWindow*, GtkWindow*, HWND).
	Handle() uintptr
	// Surface returns the drawing surface of a window created with
	// WindowOptions.Surface, nil for others.
	Surface() Surface

	SetTitle(title string)
	Title() string
	// SetRepresentedFile shows the file a window shows, "" for none: the
	// proxy icon of the title bar and its menu of folders (macOS). Other
	// platforms do nothing.
	SetRepresentedFile(path string)
	// SetDocumentEdited shows that the window's document has unsaved
	// changes: the dot of the close button (macOS). Other platforms do
	// nothing; the core marks the title there.
	SetDocumentEdited(edited bool)
	SetBounds(r Rect)
	Bounds() Rect
	SetContentBounds(r Rect)
	ContentBounds() Rect
	SetMinimumSize(s Size)
	SetMaximumSize(s Size)
	SetResizable(v bool)
	IsResizable() bool
	SetMovable(v bool)
	IsMovable() bool
	SetMinimizable(v bool)
	IsMinimizable() bool
	SetMaximizable(v bool)
	IsMaximizable() bool
	SetClosable(v bool)
	IsClosable() bool
	SetAlwaysOnTop(v bool)
	IsAlwaysOnTop() bool
	Show()
	ShowInactive()
	Hide()
	IsVisible() bool
	Focus()
	Blur()
	IsFocused() bool
	Minimize()
	IsMinimized() bool
	Maximize()
	Unmaximize()
	IsMaximized() bool
	Restore()
	SetFullScreen(v bool)
	IsFullScreen() bool
	Center()
	SetBackgroundColor(c Color)
	SetOpacity(v float64)
	Opacity() float64
	SetHasShadow(v bool)
	HasShadow() bool
	SetIgnoreMouseEvents(v bool)
	SetContentProtection(v bool)
	SetVibrancy(material string)
	// SetVibrancyTint tints the window's material, where it takes one
	// (macOS 26's Liquid Glass); nil removes the tint. Other materials and
	// platforms ignore it.
	SetVibrancyTint(c *Color)
	// SetActiveAppearance keeps the window drawn as active, as the key
	// window of the app in front, while it is not (macOS).
	SetActiveAppearance(v bool)
	// SetWindowButtonsTheme draws the controls of a hidden title bar for a
	// "light" or "dark" background whatever the app's appearance, or
	// follows it again with "system" (Windows).
	SetWindowButtonsTheme(source string)
	// SetProgressBar shows progress on the taskbar button, Dock icon or
	// launcher entry: state is "" (none), "normal", "indeterminate",
	// "paused" or "error", value between 0 and 1.
	SetProgressBar(state string, value float64)
	FlashFrame(flash bool)
	SetSkipTaskbar(v bool)
	SetVisibleOnAllWorkspaces(v bool)
	// SetIcon sets the window icon from a PNG image; nil restores the
	// application's.
	SetIcon(png []byte) error
	// SetMenu sets a per-window menu bar (Linux/Windows). No-op on macOS.
	SetMenu(m *Menu)
	// SetAutoHideMenu hides the menu bar until Alt or F10 brings the
	// keyboard to it (Linux/Windows). No-op on macOS.
	SetAutoHideMenu(v bool)
	// TitleBar returns the room the window controls of a window with a
	// hidden title bar take, which WindowHandler.TitleBarChanged reports
	// changes of. It is zero for other windows and while the controls are
	// hidden, in full screen.
	TitleBar() TitleBar
	// StartDrag moves the window with the mouse (frameless drag regions).
	StartDrag()
	// TitleBarDoubleClicked performs the platform action for a double click
	// on a custom title bar (zoom/minimize on macOS).
	TitleBarDoubleClicked()
	// PlaceFlyout places a flyout (WindowOptions.Flyout) of the given
	// size next to its anchor again, as Flyout.Place resolves it in the
	// work area of a display, after the anchor, the size or the parent
	// changed. It does nothing for other windows.
	PlaceFlyout(f Flyout, size Size)
	// Close destroys the window without asking WindowHandler.ShouldClose.
	Close()

	Page
}

// Page is the web page of a Window, or of a WebView.
type Page interface {
	// WebViewHandle returns the native webview object.
	WebViewHandle() uintptr

	LoadURL(url string)
	LoadHTML(html, baseURL string)
	LoadFile(path, readAccessDir string)
	Reload(ignoreCache bool)
	StopLoading()
	GoBack()
	GoForward()
	CanGoBack() bool
	CanGoForward() bool
	URL() string
	IsLoading() bool
	// Eval runs JavaScript in the main frame and ignores the result.
	Eval(js string)
	// SetUserScripts replaces the scripts of WindowOptions.UserScripts:
	// the documents the page loads from then on run the new ones, while
	// the current document keeps what it ran. Scripts a backend injects
	// itself, as the title bar's, stay.
	SetUserScripts(scripts []UserScript)
	// SetContentRules replaces the content rule lists the page's loads
	// go through from then on with lists, handles of
	// AppController.CompileContentRules; none removes them.
	SetContentRules(lists []any)
	// CallAsyncFunction runs body as the body of an async function in the
	// main frame, awaits it and reports the returned value, which the
	// caller arranges to be a string. err is non-nil when the body fails to
	// compile or the evaluation fails natively. Page CSP does not apply.
	CallAsyncFunction(body string, cb func(result string, err error))
	SetZoom(factor float64)
	Zoom() float64
	SetUserAgent(ua string)
	UserAgent() string
	// ProcessID returns the ID of the operating system's process that
	// the backend reports for the page (see Page.ProcessID in package
	// mygo for which one), 0 when it reports none.
	ProcessID() int
	// GPUProcessID returns the ID of the operating system's process that
	// does the page's graphics and media (see Page.GPUProcessID in
	// package mygo), 0 when the backend reports none.
	GPUProcessID() int
	// IsAudible reports whether the page plays sound, from any of its
	// frames, as WindowHandler.AudibleChanged reports its changes; false
	// where the backend cannot tell.
	IsAudible() bool
	// SetAudioMuted mutes the page's sound, or lets it play again. A
	// backend whose web view does not exist yet mutes it once it does;
	// one that cannot mute does nothing.
	SetAudioMuted(muted bool)
	// IsAudioMuted reports what SetAudioMuted set, false where the
	// backend cannot mute.
	IsAudioMuted() bool
	// CaptureState reports whether the page captures kind, and whether
	// it is muted, as WindowHandler.CaptureStateChanged reports its
	// changes; CaptureNone where the backend cannot tell.
	CaptureState(kind CaptureKind) CaptureState
	// SetCaptureState mutes the page's capture of kind (CaptureMuted),
	// lets it go on (CaptureActive) or stops it (CaptureNone). It does
	// nothing while the page captures no such device, and where the
	// backend cannot.
	SetCaptureState(kind CaptureKind, state CaptureState)
	// TogglePictureInPicture moves a video of the page into picture in
	// picture, or the one out back into the page (see
	// Page.TogglePictureInPicture in package mygo for which video); a
	// backend that cannot does nothing.
	TogglePictureInPicture()
	// PictureInPictureActive tells cb whether a video of the page is in
	// picture in picture, false where the backend cannot tell. cb runs on
	// the main thread exactly once, maybe before PictureInPictureActive
	// returns.
	PictureInPictureActive(cb func(active bool))
	// SessionState returns the engine's own state of the page's
	// back/forward list, with the scroll positions and form state it
	// saved of its entries, nil where the backend keeps none.
	SessionState() []byte
	// RestoreSessionState puts back a back/forward list that
	// SessionState returned and loads its current entry, as LoadURL
	// loads a page, and reports whether it did: false where the backend
	// keeps no state or can't read this one, for the core to load the
	// page's URL instead.
	RestoreSessionState(state []byte) bool
	OpenDevTools()
	CloseDevTools()
	IsDevToolsOpened() bool
	// CapturePage takes a PNG snapshot of the visible page.
	CapturePage(cb func(png []byte, err error))
	// PrintToPDF renders the page as a PDF document.
	PrintToPDF(opts PDFOptions, cb func(pdf []byte, err error))
	// DroppedFiles returns the paths of the files of the latest drop on
	// the page and forgets them. The page reports the drop itself (a
	// "drop" message); backends record the paths before the page sees it.
	DroppedFiles() []string
	Print()
}

// CaptureKind is a kind of device a page captures (Page.CaptureState).
type CaptureKind int

const (
	CaptureCamera CaptureKind = iota
	CaptureMicrophone
	// CaptureDisplay is the capture of a screen or a window,
	// getDisplayMedia().
	CaptureDisplay
)

// CaptureState is whether a page captures a kind of device. The values
// are WebKit's WKMediaCaptureState and WebKitMediaCaptureState.
type CaptureState int

const (
	CaptureNone CaptureState = iota
	CaptureActive
	CaptureMuted
)

// WebView is a web view in a window showing a Surface, which its content
// places (Surface.NewWebView, Surface.PlaceWebViews): under the surface,
// which shows it where its frames are transparent, as holes
// (scene.OpHole) make them. Its page reports to the WindowHandler it was
// made with, which hears nothing of the window.
type WebView interface {
	Page
	// Focus gives the web view the keyboard.
	Focus()
	// TabInto gives the web view the keyboard as Tab does: at its page's
	// first element taking the focus, or its last when back, as Shift+Tab
	// does. Tab past the other end comes back as WebViewTabOut.
	TabInto(back bool)
	// SetBackgroundColor paints the web view before its page does.
	SetBackgroundColor(c Color)
	// Close destroys the web view; its handler hears no more of it.
	// Backends close the web views of a window that closes before telling
	// its WindowHandler.Closed, and Close does nothing then.
	Close()
}

// WebViewPlacement is where the content of a surface shows a web view
// (Surface.PlaceWebViews), in DIPs relative to the surface.
type WebViewPlacement struct {
	WebView WebView
	// Frame is the web view's box as it shows; Clip the part of it that
	// shows, which the frames leave transparent but where the content
	// painted over it.
	Frame, Clip RectF
	// Width and Height are the size the page lays out at, in its own
	// DIPs: the element's content box before a scale of the transforms
	// around it, which shows the page in Frame scaled as a picture of it,
	// its viewport the same (Scale). Zero is Frame's size.
	Width, Height float64
	// Radii are those of the page's corners, in its DIPs, from its
	// top-left clockwise, as the hole showing it has: the page shows, and
	// takes the pointer, inside them.
	Radii [4]float64
	// Covers are the boxes, within Clip, of what the content painted over
	// the web view that takes the pointer: there the pointer is the
	// surface's, elsewhere in Clip the web view's.
	Covers []RectF
}

// Size returns the size the page lays out at, in its DIPs.
func (p *WebViewPlacement) Size() (w, h float64) {
	if p.Width <= 0 || p.Height <= 0 {
		return p.Frame.W, p.Frame.H
	}
	return p.Width, p.Height
}

// Scale returns how much Frame scales the page: 1, 1 unless a transform
// scales it.
func (p *WebViewPlacement) Scale() (x, y float64) {
	w, h := p.Size()
	if w <= 0 || h <= 0 {
		return 1, 1
	}
	return p.Frame.W / w, p.Frame.H / h
}

// ToPage converts a point of the surface to the page's DIPs.
func (p *WebViewPlacement) ToPage(x, y float64) (float64, float64) {
	sx, sy := p.Scale()
	return (x - p.Frame.X) / sx, (y - p.Frame.Y) / sy
}

// Rounded reports whether the page's corners are rounded.
func (p *WebViewPlacement) Rounded() bool { return p.Radii != [4]float64{} }

// At reports whether the pointer at x, y is the web view's.
func (p *WebViewPlacement) At(x, y float64) bool {
	if !p.Clip.Contains(x, y) {
		return false
	}
	for _, c := range p.Covers {
		if c.Contains(x, y) {
			return false
		}
	}
	if p.Rounded() {
		w, h := p.Size()
		px, py := p.ToPage(x, y)
		return InsideCorners(w, h, p.Radii, px, py)
	}
	return true
}

// InsideCorners reports whether x, y is inside the corners of radii
// (from the top-left clockwise) of a box w by h at the origin.
func InsideCorners(w, h float64, radii [4]float64, x, y float64) bool {
	// Each corner's center, and the side of it the corner is on.
	for i, c := range [4][2]float64{{0, 0}, {1, 0}, {1, 1}, {0, 1}} {
		r := radii[i]
		if r <= 0 {
			continue
		}
		cx, cy := r+c[0]*(w-2*r), r+c[1]*(h-2*r)
		dx, dy := x-cx, y-cy
		if (c[0] == 0) != (dx < 0) || (c[1] == 0) != (dy < 0) {
			continue // not in the corner's square
		}
		if dx*dx+dy*dy > r*r {
			return false
		}
	}
	return true
}

// WindowHandler receives window and webview events from the backend.
type WindowHandler interface {
	// ShouldClose is called when the user tries to close the window.
	ShouldClose() bool
	// Closed is called after the native window has been destroyed.
	Closed()
	Focused()
	Blurred()
	// MenuItemClicked reports an item chosen from this window's menu bar
	// or context menu, even while the menu has focus instead of the window.
	MenuItemClicked(id int)
	Resized()
	Moved()
	Minimized()
	Restored()
	Maximized()
	Unmaximized()
	EnteredFullScreen()
	LeftFullScreen()
	// TitleBarChanged is called when Window.TitleBar changes, e.g. when the
	// window enters full screen or the desktop's button layout changes.
	TitleBarChanged()

	// Message delivers a message posted by the renderer bridge.
	Message(msg string)
	// WillNavigate is called before a navigation; returning false cancels it.
	WillNavigate(nav Navigation) bool
	NavigationStarted(url string)
	NavigationCommitted(url string)
	LoadFinished()
	LoadFailed(url string, code int, description string)
	TitleChanged(title string)
	// NewWindow is called for window.open() and target=_blank links. It
	// returns the window that should host the new page or nil to deny it.
	NewWindow(req NewWindowRequest) Window
	// ClosedByPage is called when the page calls window.close().
	ClosedByPage()
	RenderProcessGone(reason string)
	// AudibleChanged is called when Page.IsAudible changes.
	AudibleChanged(audible bool)
	// CaptureStateChanged is called when Page.CaptureState of kind may
	// have changed, with its state.
	CaptureStateChanged(kind CaptureKind, state CaptureState)
	SchemeRequest(req *SchemeRequest)
	// DownloadStarted returns where to save a download, "" to cancel it;
	// DownloadFinished reports how it ended.
	DownloadStarted(url, suggestedName string) string
	DownloadFinished(url, path string, err error)
	// SchemeDownload downloads a URL of a custom scheme, which the engine
	// cannot turn into a download, by serving it again.
	SchemeDownload(url string)
	// PermissionRequested asks whether the page at origin may use kinds:
	// "camera", "microphone", "geolocation" or "notifications".
	PermissionRequested(kinds []string, origin string) bool
	// AuthRequested answers an HTTP authentication challenge of the page,
	// or of what it loads.
	AuthRequested(req AuthRequest) AuthResponse
	// CertificateError asks whether to load what a server sent with a
	// certificate that does not verify: false fails it, as without the
	// call. A backend that verifies certificates itself to tell (macOS)
	// does so only while HandlesCertificateErrors reports true, and
	// leaves them to the web view otherwise.
	CertificateError(e CertificateError) bool
	HandlesCertificateErrors() bool
	// ContextMenu asks what the page's context menu shows, as it is
	// about to open, and HandlesContextMenu whether to ask at all: a
	// backend describes its menu only while it does.
	ContextMenu(m *ContextMenu) ContextMenuResponse
	HandlesContextMenu() bool

	// SurfaceEvent delivers input on the window's Surface, and changes of
	// it. For FileDragOver and FileDrop it reports whether the content
	// takes the files where they are, for KeyPressed whether it took
	// the key itself, which the system then leaves alone (Windows opens no
	// menu for Alt and the key), for TextComposition whether it took the
	// composition, which the input method gives up otherwise (macOS), and
	// for SurfaceCommandEnabled whether the command would act; it returns
	// false for other events.
	SurfaceEvent(ev SurfaceEvent) bool
}

// Navigation describes a pending navigation.
type Navigation struct {
	URL           string
	IsMainFrame   bool
	UserInitiated bool
	// Unasked is true for the navigations the app is not asked about
	// (OnWillNavigate): reloads, history navigations and the loads the app
	// starts.
	Unasked bool
	// Reload is true for a reload of the main frame's document, whoever
	// started it: Reload, the page's location.reload(), the web view's own
	// menus.
	Reload bool
}

// NewWindowRequest describes a window.open() or target=_blank request.
type NewWindowRequest struct {
	URL         string
	FrameName   string
	Disposition string
	// Width/Height/X/Y come from window.open() features; zero when absent.
	X, Y, Width, Height int
	// UserInitiated is true when a user's gesture, as a click or a key
	// press, opened the window: a click on the link, or its handler
	// calling window.open().
	UserInitiated bool
	// Native must be copied into WindowOptions.Native of the window that is
	// returned.
	Native uintptr
}

// SchemeRequest is a request for a custom protocol.
type SchemeRequest struct {
	// Context is cancelled when the webview no longer needs the response.
	Context context.Context
	Method  string
	URL     string
	// Header is an http.Header, of an unnamed type so that platform does
	// not import net/http, which builds for browsers leave out: values of
	// either type assign to the other.
	Header map[string][]string
	// Body is nil for requests without a body.
	Body io.ReadCloser
	// Responder methods must be called on the main thread, Respond first.
	Responder SchemeResponder
}

// SchemeResponder delivers a custom protocol response to the webview.
type SchemeResponder interface {
	Respond(status int, header map[string][]string) // an http.Header
	Write(p []byte)
	Finish()
	Fail(err error)
}

// SchemeBodyWriter is implemented by SchemeResponders that take the body
// from the goroutine serving the request instead of through Write,
// blocking while the webview has not read enough of it: a large response
// then takes little memory however slowly the webview reads it.
type SchemeBodyWriter interface {
	// WriteBody writes p once Respond has run. It is called off the main
	// thread, never at the same time as the other methods, and fails
	// once the webview no longer reads the response.
	WriteBody(p []byte) error
}

// MenuItemType is the kind of a menu item.
type MenuItemType int

// Menu item kinds.
const (
	MenuItemNormal MenuItemType = iota
	MenuItemSeparator
	MenuItemSubmenu
	MenuItemCheckbox
	MenuItemRadio
)

// Menu is a snapshot of a menu tree.
type Menu struct {
	Items []*MenuItem
	// RightToLeft lays a context menu out right to left, as that of an
	// interface laid out so: its items from the right, opening to the
	// left of where it shows, which is its top right corner.
	RightToLeft bool
}

// MenuItem is a snapshot of a menu item. ID identifies the public item the
// native item was built from.
type MenuItem struct {
	ID          int
	Type        MenuItemType
	Label       string
	Subtitle    string
	Role        string
	Accelerator string
	Enabled     bool
	Visible     bool
	Checked     bool
	ToolTip     string
	// Icon is the image beside the label, nil for none.
	Icon *MenuIcon
	// IconColor, when set, is the color of a template Icon in the menus
	// package ui draws, in place of the text's; backends ignore it.
	IconColor *Color
	Submenu   *Menu
	// Native is set on the web view's own items of a page's context menu
	// (WindowHandler.ContextMenu): the backend's number for the item,
	// from 1, which it shows where the core puts it, doing what the web
	// view does; ID is then not the backend's business. Name is such an
	// item's name (ContextMenuReload and the other names of package mygo,
	// else the platform's own), and Accelerator the platform's own text of
	// its shortcut, shown as it is, where the backend draws the menu.
	Native int
	Name   string
}

// FileFilter restricts the files shown in file dialogs: those of its
// Extensions, and of its MIMETypes, which may be "kind/*".
type FileFilter struct {
	Name       string
	Extensions []string
	MIMETypes  []string
}

// OpenDialogOptions configures an open panel.
type OpenDialogOptions struct {
	Title                      string
	DefaultPath                string
	ButtonLabel                string
	Message                    string
	Filters                    []FileFilter
	OpenFiles                  bool
	OpenDirectories            bool
	Multiple                   bool
	ShowHiddenFiles            bool
	CreateDirectories          bool
	TreatPackagesAsDirectories bool
}

// SaveDialogOptions configures a save panel.
type SaveDialogOptions struct {
	Title                      string
	DefaultPath                string
	ButtonLabel                string
	Message                    string
	NameFieldLabel             string
	Filters                    []FileFilter
	ShowHiddenFiles            bool
	CreateDirectories          bool
	TreatPackagesAsDirectories bool
}

// MessageBoxOptions configures a message box.
type MessageBoxOptions struct {
	// Type is "none", "info", "error", "question" or "warning".
	Type            string
	Title           string
	Message         string
	Detail          string
	Buttons         []string
	DefaultID       int
	CancelID        int
	DestructiveIDs  []int
	CheckboxLabel   string
	CheckboxChecked bool
}

// MessageBoxResult is the outcome of a message box.
type MessageBoxResult struct {
	Response        int
	CheckboxChecked bool
}

// Dialogs shows native dialogs. Callbacks run on the main thread exactly
// once. parent may be nil for application modal dialogs.
type Dialogs interface {
	ShowOpenDialog(parent Window, opts *OpenDialogOptions, cb func(paths []string, err error))
	ShowSaveDialog(parent Window, opts *SaveDialogOptions, cb func(path string, err error))
	ShowMessageBox(parent Window, opts *MessageBoxOptions, cb func(res MessageBoxResult, err error))
}

// Clipboard accesses the system clipboard.
type Clipboard interface {
	// Data reads copy accepted representations, so returned data outlives
	// native ownership. Nil formats reads all; a missing explicit match is
	// transfer.ErrFormat. WriteData takes an application-owned snapshot;
	// released runs once when native providers are no longer needed, never
	// after a failed write. Neither operation depends on a source window.
	ReadData(formats []transfer.Format) (transfer.Data, error)
	WriteData(data transfer.Data, released func()) error
	Formats() []transfer.Format
	// Flush materializes owned providers and asks the system to persist the
	// data. Close drops any remaining owned providers at app shutdown.
	Flush() error
	Close()
	ReadText() string
	WriteText(text string)
	ReadHTML() string
	WriteHTML(markup string)
	ReadImage() []byte
	WriteImage(png []byte) error
	Clear()
	AvailableFormats() []string
}

// LaterClipboard is a clipboard whose writes finish later, as a
// browser's, whose clipboard answers with a promise: WriteTextThen puts
// text there as WriteText does, and gives done what came of it, on the
// main thread. Desktops' clipboards answer at once (WriteData).
type LaterClipboard interface {
	WriteTextThen(text string, done func(error))
}

// Shell integrates with the desktop environment.
type Shell interface {
	OpenExternal(url string) error
	OpenPath(path string) error
	ShowItemInFolder(path string)
	TrashItem(path string) error
	Beep()
	// FileIcon returns the icon the file manager shows for a file, a
	// folder or an application, as a PNG image of size by size pixels.
	FileIcon(path string, size int) ([]byte, error)
	// TypeIcon returns the icon the file manager shows for files with the
	// extension ext (without its dot), as FileIcon does, though no such
	// file exists.
	TypeIcon(ext string, size int) ([]byte, error)
	// OpenApplication starts the application at Application.Path, or
	// brings it to the front.
	OpenApplication(path string) error
}

// Application is an installed application (Backend.Applications): Path is
// where FileIcon finds its icon and OpenApplication starts it.
type Application struct {
	Name, ID, Path string
}

// Display describes a monitor.
type Display struct {
	ID          int64
	Label       string
	Bounds      Rect
	WorkArea    Rect
	ScaleFactor float64
	Rotation    int
	Internal    bool
	Primary     bool
}

// Screen queries monitors.
type Screen interface {
	Displays() []Display
	CursorPoint() Point
}

// Theme reads and overrides the system appearance.
type Theme interface {
	IsDark() bool
	// SetSource is "system", "light" or "dark".
	SetSource(source string)
	// UIFont returns the family of the desktop's interface font where the
	// system's text stack does not know it, as on Linux, else "".
	UIFont() string
	// FontRendering returns how the desktop's settings say to rasterize
	// text where the system's text stack does not know them, as on Linux.
	FontRendering() FontRendering
	// Preferences returns the settings of the desktop that the system's
	// controls follow. A change of any of them calls Handler.ThemeChanged.
	Preferences() Preferences
}

// Preferences are settings of the desktop that the system's own controls
// follow, and native UI with them.
type Preferences struct {
	// Accent is the accent color the user chose; A is 0 where the desktop
	// has none.
	Accent Color
	// ReduceMotion asks for less motion: macOS's Reduce Motion, Windows's
	// animation effects and GNOME's animations turned off.
	ReduceMotion bool
	// ReduceTransparency asks for opaque surfaces in place of translucent
	// ones: macOS's Reduce Transparency, Windows's transparency effects
	// turned off, prefers-reduced-transparency in browsers. Linux desktops
	// have no such setting.
	ReduceTransparency bool
	// HighContrast asks for more contrast: macOS's Increase Contrast,
	// Windows's contrast themes, the desktop portal's higher contrast.
	HighContrast bool
	// TextScale is how many times larger than usual text should be, as
	// Windows's and GNOME's text size settings say; 0 or 1 for usual.
	TextScale float64
	// Pointer is how accurate the primary pointing device is, and
	// AnyPointer the most accurate of all, as Media Queries 4's pointer
	// and any-pointer: PointerFine, a mouse's, where it is not known.
	Pointer, AnyPointer PointerAccuracy
	// NoHover tells that the primary pointing device cannot hover, as a
	// touch screen, and NoAnyHover that none can (hover and any-hover:
	// none).
	NoHover, NoAnyHover bool
}

// PointerAccuracy is how accurately a pointing device points.
type PointerAccuracy uint8

const (
	// PointerFine is a mouse's, a touchpad's or a pen's.
	PointerFine PointerAccuracy = iota
	// PointerCoarse is a finger's on a touch screen.
	PointerCoarse
	// PointerNone is that of no pointing device at all.
	PointerNone
)

// FontRendering is how the desktop's settings say to rasterize text, as
// GTK's gtk-xft settings: each field is "" for the default, or unknown.
type FontRendering struct {
	// Antialias is "none", "gray" or "subpixel".
	Antialias string
	// Hinting is "none", "slight", "medium" or "full".
	Hinting string
	// Subpixels is the order of the screen's subpixels: "rgb", "bgr",
	// "vrgb" or "vbgr".
	Subpixels string
}

// PDFOptions configures Window.PrintToPDF; lengths are in inches.
type PDFOptions struct {
	Landscape                                        bool
	PageWidth, PageHeight                            float64
	MarginTop, MarginRight, MarginBottom, MarginLeft float64
	Background                                       bool
}

// Power reports power and session events and keeps the computer awake.
type Power interface {
	// Watch starts reporting AppHandler.PowerEvent.
	Watch()
	// KeepAwake keeps the system, and with display the display, from
	// sleeping while idle until release is called.
	KeepAwake(display bool, reason string) (release func())
	OnBattery() bool
	IdleTime() time.Duration
}

// MediaCommand is a command of the system's media controls, or a set of
// them (NowPlayingInfo.Commands).
type MediaCommand uint32

const (
	MediaPlay MediaCommand = 1 << iota
	MediaPause
	MediaTogglePlayPause
	MediaStop
	MediaNext
	MediaPrevious
	MediaSeek
)

// NowPlayingInfo is what the app plays, fully defaulted by the core.
type NowPlayingInfo struct {
	Title, Artist, Album string
	Artwork              []byte // PNG or JPEG, or nil
	Duration             time.Duration
	// Position is where playback was when the core set the info; the
	// system advances it at Rate while State is "playing".
	Position time.Duration
	Rate     float64 // the speed while playing, 1 by default
	State    string  // "playing", "paused" or "stopped"
	Commands MediaCommand
}

// NowPlaying shows what the app plays where the system shows media, and
// reports AppHandler.MediaCommand for the commands the info enables.
type NowPlaying interface {
	// Set shows info; nil removes it and stops reporting commands.
	Set(info *NowPlayingInfo)
}

// NetworkStatus is the state of the computer's network connection.
type NetworkStatus struct {
	Online  bool
	Metered bool // expensive, metered or in a low data mode
}

// Network reports the state of the computer's network connection.
type Network interface {
	// Watch starts reporting AppHandler.NetworkChanged, which may also be
	// called when the status stayed the same.
	Watch()
	// Status returns the current state.
	Status() NetworkStatus
}

// ErrSecretNotFound is what Secrets.Secret fails with when the store has
// no secret for the service and account.
var ErrSecretNotFound = errors.New("mygo: secret not found")

// Secrets keeps small secrets in the system's credential store: the
// Keychain on macOS, the Secret Service on Linux (through libsecret) and
// the Credential Manager on Windows. A secret is identified by a service,
// the app, and an account, its key, as Electron's keytar stores them.
//
// Unlike other platform methods these may be called from any goroutine,
// and they may block while the system asks the user, e.g. to unlock the
// keyring: the core never calls them on the main thread of a running app.
type Secrets interface {
	SetSecret(service, account string, secret []byte) error
	// Secret fails with ErrSecretNotFound when there is none.
	Secret(service, account string) ([]byte, error)
	// DeleteSecret succeeds when there is no secret to delete.
	DeleteSecret(service, account string) error
}

// TrayHandler receives tray icon events.
type TrayHandler interface {
	Clicked()
	RightClicked()
	// AppearanceChanged tells that the bar the icon shows on turned dark
	// or light.
	AppearanceChanged()
}

// Tray is a status bar / notification area icon.
type Tray interface {
	// SetID names the icon across launches (macOS's autosave name).
	SetID(id string)
	// Dark reports whether the bar the icon shows on is dark.
	Dark() bool
	SetImage(png []byte, template bool) error
	SetTitle(title string)
	SetToolTip(tip string)
	SetMenu(m *Menu)
	PopUpMenu(m *Menu)
	// SetActivatable tells the panel whether the icon answers activation,
	// so that it can tell a single click from a double one. Platforms that
	// report clicks themselves ignore it.
	SetActivatable(on bool)
	Bounds() Rect
	Destroy()
}

// Notification is a desktop notification.
type Notification struct {
	ID       string
	Title    string
	Subtitle string
	Body     string
	Silent   bool
	// Group gathers the notifications that share it (threadIdentifier on
	// macOS).
	Group string
	// Image is shown with the notification: PNG or JPEG, or nil. The core
	// links the backend's code of images (LinkNotificationImages) before
	// it gives one.
	Image  []byte
	Sender *NotificationSender
}

// NotificationSender describes an incoming direct message's sender.
type NotificationSender struct {
	ID, Name string
	Avatar   []byte
}
