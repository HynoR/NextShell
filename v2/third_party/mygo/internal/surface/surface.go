// Package surface connects the content of a window that MyGo draws itself
// (package ui) to the window (package mygo), without either importing the
// other's internals.
package surface

import (
	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/transfer"
)

// Conn is a window's side of the connection. Package mygo fills it before
// calling Content.AttachContent; the content sets the hooks it handles.
// Everything runs on the main thread, except Invalidate and Post.
type Conn struct {
	Surface platform.Surface
	// Window is the *mygo.Window.
	Window any
	// Clipboard is the system clipboard.
	Clipboard platform.Clipboard
	// StartDrag moves the window with the pointer, for drag regions;
	// TitleBarDoubleClicked does what a double click on a title bar does.
	StartDrag             func()
	TitleBarDoubleClicked func()
	// Escape is called with an Escape that no element or shortcut of the
	// content handled, which closes a flyout.
	Escape func()
	// StartDataDrag connects a UI source to the core's process-local
	// registry. local never leaves the process.
	StartDataDrag  func(transfer.Data, any, transfer.DragOptions, float64, float64) error
	CancelDataDrag func()
	// IsDark reports the system's dark appearance, and Preferences the
	// settings of the desktop that controls follow, which ThemeChanged
	// tells changes of as well.
	IsDark      func() bool
	Preferences func() platform.Preferences
	// Locale returns the language the app shows (App.Locale), and Region
	// the user's regional formats, which LocaleChanged tells changes of.
	// They cost nothing: the content may read them every frame.
	Locale func() string
	Region func() i18n.Region
	// RightToLeft reports whether the content is laid out right to left,
	// as the app's language reads (i18n.Direction of App.Locale), unless
	// the window says otherwise; nil is left to right. The content reads
	// it as it builds each frame: call Changed after it changes.
	RightToLeft func() bool
	// UIFont returns the family of the desktop's interface font where the
	// system's text stack does not know it (Linux), else "".
	UIFont func() string
	// FontRendering returns how the desktop's settings say to rasterize
	// text where the system's text stack does not know them (Linux).
	FontRendering func() platform.FontRendering
	// TitleBar returns the room the window controls take in a window with
	// a hidden title bar, zero in other windows.
	TitleBar func() platform.TitleBar
	// Active reports whether the window is drawn as active, as the window
	// the user's input goes to (see ui.Context.WindowActive), which
	// ActiveChanged tells changes of; nil is active. It costs nothing.
	Active func() bool
	// OpenURL opens a link in the default browser, and gives done, unless
	// nil, what came of it on the main thread.
	OpenURL func(url string, done func(error))
	// DevTools tells that the window's developer tools are on
	// (PageOptions.DevTools): the content then opens its inspector.
	DevTools bool
	// Invalidate asks for a frame; it is safe from any goroutine.
	Invalidate func()
	// Changed asks for a frame built anew after the app changed the state
	// the content shows (Window.Update, Window.Invalidate); the content
	// sets it, else the window asks the surface for a frame.
	Changed func()
	// Post runs fn on the main thread soon, unless the window has closed;
	// it is safe from any goroutine.
	Post func(fn func())
	// Ready tells that the content presented its first frame: the window
	// is ready to show (Window.OnReadyToShow).
	Ready func()
	// PopupMenu shows m as a context menu at (x, y) in the surface, in
	// DIPs, once the event being handled returns. chosen receives the ID of
	// the item chosen, if one is.
	PopupMenu func(m *platform.Menu, x, y float64, chosen func(id int))
	// PlaceWebViews shows the window's web views where the content's last
	// frame shows them (Surface.PlaceWebViews).
	PlaceWebViews func(views []platform.WebViewPlacement)
	// ContentSize, set by a window that takes its content's size
	// (mygo.FlyoutOptions.FitContent), nil in others, hears the size
	// the content takes, in DIPs, as a frame laid out at the window's
	// size measures it and when it changed: its max-content width within
	// the bounds FitWidth returns (0 for none), and its height at that
	// width. It is called while the frame builds: the window resizes
	// after it, and the frame that follows lays the content out at the
	// size it took.
	ContentSize func(width, height float64)
	FitWidth    func() (min, max float64)
	// Fit, set by the content where ContentSize is, builds and lays out
	// at once the frame the content waits to draw, if one waits, as in a
	// hidden window, which tells ContentSize the size it takes if that
	// changed, and reports whether it did build one. The window calls
	// it before it shows, to show at that size.
	Fit func() bool

	// Event receives the surface's events, and Focus and Blur of the
	// window. It reports whether the content takes dragged files, as
	// WindowHandler.SurfaceEvent does.
	Event func(ev platform.SurfaceEvent) bool
	// ThemeChanged is called when the system appearance changes, and
	// TitleBarChanged when TitleBar does.
	ThemeChanged    func()
	TitleBarChanged func()
	// ActiveChanged is called when Active changed.
	ActiveChanged func()
	// LocaleChanged is called when Locale or Region changed, for the
	// content to build its frame anew.
	LocaleChanged func()
	// Capture renders the content as it is now into premultiplied RGBA.
	Capture func() (width, height int, rgba []byte)
	// ToggleDevTools opens or closes the content's inspector, for the
	// Toggle Developer Tools menu item, when DevTools is set.
	ToggleDevTools func()
	// Detach is called once the window is closed.
	Detach func()
}

// Content is implemented by package ui.
type Content interface {
	AttachContent(conn *Conn)
}

// WebView is a web view a window made for its content to show
// (*mygo.WebView, of Window.NewWebView), which package ui places.
type WebView interface {
	// SurfaceWebView returns the web view's native side when it is of the
	// window of conn and not destroyed, else nil. Main thread only.
	SurfaceWebView(conn *Conn) platform.WebView
}
