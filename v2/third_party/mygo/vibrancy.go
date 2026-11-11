package mygo

// Vibrancy is a translucent material that blurs what is behind a window,
// shown behind a transparent page (see WindowOptions.Vibrancy).
//
// macOS has all of them (NSVisualEffectView materials, and from macOS 26
// Liquid Glass). Windows 11 has the Mica, Acrylic and Tabbed system
// backdrops; the other materials use the closest one: Acrylic for
// transient UI (menus, popovers, HUDs, sheets, tooltips, selection,
// full-screen UI) and Liquid Glass, Mica otherwise. On macOS, Mica and
// Tabbed use the under-window material and Acrylic the HUD one. Linux has
// none.
//
// The system draws the material with what is behind the window: other
// apps' windows, the desktop. The glass plugin
// (github.com/egoist/mygo/plugins/glass) draws Liquid Glass with what the
// window itself draws, the same on every platform.
type Vibrancy string

// Materials for window areas.
const (
	VibrancyNone        Vibrancy = ""
	VibrancyWindow      Vibrancy = "window"
	VibrancyContent     Vibrancy = "content"
	VibrancySidebar     Vibrancy = "sidebar"
	VibrancyHeader      Vibrancy = "header"
	VibrancyTitlebar    Vibrancy = "titlebar"
	VibrancyUnderWindow Vibrancy = "under-window"
	VibrancyUnderPage   Vibrancy = "under-page"
)

// Materials for transient UI.
const (
	VibrancyMenu         Vibrancy = "menu"
	VibrancyPopover      Vibrancy = "popover"
	VibrancyHUD          Vibrancy = "hud"
	VibrancySheet        Vibrancy = "sheet"
	VibrancyTooltip      Vibrancy = "tooltip"
	VibrancySelection    Vibrancy = "selection"
	VibrancyFullScreenUI Vibrancy = "fullscreen-ui"
)

// Liquid Glass, as macOS 26 draws it (NSGlassEffectView): what is behind
// the window shows through it, frosted and bent along its edges, under a
// lit rim, as through the Dock. It fills the window, rounded as
// WindowOptions.CornerRadius rounds a frameless one. Before macOS 26,
// VibrancyGlass is the popover material and VibrancyGlassClear the HUD one;
// on Windows 11 both are Acrylic.
//
// The clear glass looks clear only in a window drawn as active, the key
// window of the app in front, and frosted otherwise, as a floating panel
// over another app is: Window.SetActiveAppearance keeps it clear.
const (
	// VibrancyGlass is the regular glass, frosted so that what is on it
	// reads over anything: bars, panels, launchers.
	VibrancyGlass Vibrancy = "glass"
	// VibrancyGlassClear is the clear glass, barely frosted: over photos
	// and video, where what is on it brings its own contrast.
	VibrancyGlassClear Vibrancy = "glass-clear"
)

// Windows 11 system backdrops.
const (
	// VibrancyMica tints the window with the desktop wallpaper.
	VibrancyMica Vibrancy = "mica"
	// VibrancyAcrylic blurs what is behind the window.
	VibrancyAcrylic Vibrancy = "acrylic"
	// VibrancyTabbed is Mica for windows with tabs in the title bar.
	VibrancyTabbed Vibrancy = "tabbed"
)
