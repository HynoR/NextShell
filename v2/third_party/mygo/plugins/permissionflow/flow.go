package permissionflow

import (
	"path/filepath"
	"strings"
)

// The parts of the systems' implementations that are plain computation,
// here so that every platform tests them.

// macAnchors are the anchors of System Settings' Privacy & Security pages,
// which open with x-apple.systempreferences:com.apple.preference.security?anchor.
var macAnchors = map[Permission]string{
	Accessibility:   "Privacy_Accessibility",
	ScreenRecording: "Privacy_ScreenCapture",
	InputMonitoring: "Privacy_ListenEvent",
	FullDiskAccess:  "Privacy_AllFiles",
	AppManagement:   "Privacy_AppBundles",
	DeveloperTools:  "Privacy_DevTools",
	Microphone:      "Privacy_Microphone",
	Camera:          "Privacy_Camera",
	Calendars:       "Privacy_Calendars",
	Reminders:       "Privacy_Reminders",
	Bluetooth:       "Privacy_Bluetooth",
	MediaAppleMusic: "Privacy_Media",
}

func macSettingsURL(p Permission) string {
	return "x-apple.systempreferences:com.apple.preference.security?" + macAnchors[p]
}

// macDraggable reports whether the list of p's page in System Settings
// takes apps dragged into it. The others list the apps that asked, with
// the system's prompt (see Request).
func macDraggable(p Permission) bool {
	switch p {
	case Microphone, Camera, Calendars, Reminders:
		return false
	}
	return true
}

// windowsCapabilities are the capabilities of Windows' consent store
// (HKCU\Software\Microsoft\Windows\CurrentVersion\CapabilityAccessManager\ConsentStore)
// that apps outside packages ("desktop apps") are kept from, and the pages
// of Settings that switch them.
var windowsCapabilities = map[Permission]struct{ capability, uri string }{
	Microphone: {"microphone", "ms-settings:privacy-microphone"},
	Camera:     {"webcam", "ms-settings:privacy-webcam"},
}

// windowsConsent is the status of a capability from the values of its
// switches in the consent store: the device's (HKLM), the user's, and
// the user's for desktop apps (NonPackaged). Any of them off denies it;
// a switch that isn't stored is on.
func windowsConsent(values ...string) Status {
	for _, v := range values {
		if strings.EqualFold(v, "Deny") {
			return Denied
		}
	}
	return Granted
}

// appName is the name the panel gives the app at path.
func appName(path string) string {
	name := filepath.Base(filepath.Clean(path))
	return strings.TrimSuffix(name, filepath.Ext(name))
}

// A run is a part of the panel's text, in bold or not.
type run struct {
	text string
	bold bool
}

// panelText is what the panel says, as PermissionFlow's does: "Drag
// **MyApp** to the list above to allow **Accessibility**."
func panelText(app, permission string) []run {
	return []run{
		{text: "Drag "},
		{text: app, bold: true},
		{text: " to the list above to allow "},
		{text: permission, bold: true},
		{text: "."},
	}
}

// rect is a rectangle in AppKit's screen coordinates: points, from the
// bottom left of the main display, y going up.
type rect struct{ X, Y, W, H float64 }

func (r rect) maxX() float64 { return r.X + r.W }
func (r rect) maxY() float64 { return r.Y + r.H }

func (r rect) intersects(o rect) bool {
	return r.X < o.maxX() && o.X < r.maxX() && r.Y < o.maxY() && o.Y < r.maxY()
}

// flipRect converts r from the window server's coordinates, from the top
// left of the main display with y going down, to AppKit's, mainHeight
// being the height of the main display.
func flipRect(r rect, mainHeight float64) rect {
	return rect{X: r.X, Y: mainHeight - r.Y - r.H, W: r.W, H: r.H}
}

// The panel's geometry, in points.
const (
	// settingsSidebar is the width of System Settings' sidebar: the panel
	// lines up with the page on its right, where the list is.
	settingsSidebar = 230
	panelMinWidth   = 240
	screenInset     = 12
)

// panelWidth is the panel's width under a window of System Settings at
// settings, on a screen whose visible part is screen.
func panelWidth(settings, screen rect) float64 {
	return min(max(panelMinWidth, settings.W-settingsSidebar), screen.W-2*screenInset)
}

// panelFrame places a panel of the height under the window of System
// Settings at settings: aligned with its page, right under it, and kept
// inside the screen, over the bottom of the window when there's no room
// under it.
func panelFrame(settings, screen rect, height float64) rect {
	w := panelWidth(settings, screen)
	x := settings.X + settingsSidebar
	y := settings.Y - height
	x = max(screen.X+screenInset, min(x, screen.maxX()-w-screenInset))
	y = max(screen.Y+screenInset, min(y, screen.maxY()-height-screenInset))
	return rect{X: x, Y: y, W: w, H: height}
}

// screenOf returns the visible part of the screen that shows most of r,
// or the first one when none does.
func screenOf(r rect, screens []rect) rect {
	best, area := rect{}, -1.0
	for i, s := range screens {
		if i == 0 {
			best = s
		}
		if !r.intersects(s) {
			continue
		}
		w := min(r.maxX(), s.maxX()) - max(r.X, s.X)
		h := min(r.maxY(), s.maxY()) - max(r.Y, s.Y)
		if w*h > area {
			best, area = s, w*h
		}
	}
	return best
}

// A settingsWindow is what the window server says of a window of System
// Settings: its bounds in its coordinates, its layer and its alpha.
type settingsWindow struct {
	bounds rect
	layer  int
	alpha  float64
}

// mainSettingsWindow picks System Settings' window among its windows: the
// largest one of the normal layer that shows, as PermissionFlow does,
// leaving out its small helper windows.
func mainSettingsWindow(windows []settingsWindow) (rect, bool) {
	var best rect
	found := false
	for _, w := range windows {
		if w.layer != 0 || w.alpha <= 0 || w.bounds.W <= 320 || w.bounds.H <= 240 {
			continue
		}
		if !found || w.bounds.W*w.bounds.H > best.W*best.H {
			best, found = w.bounds, true
		}
	}
	return best, found
}
