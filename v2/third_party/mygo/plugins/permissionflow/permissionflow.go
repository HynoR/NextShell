// Package permissionflow checks and asks for the permissions the system
// keeps for the user to give, such as Accessibility, Screen Recording and
// the microphone, and guides the user to where they give them:
//
//	st, err := permissionflow.Check(permissionflow.Accessibility)
//	if st != permissionflow.Granted {
//		st, err = permissionflow.Guide(ctx, permissionflow.Accessibility, permissionflow.GuideOptions{})
//	}
//
// On macOS, Guide opens the page of System Settings (Privacy & Security)
// and, on the pages whose list takes apps dragged into it, shows a panel
// under the window of System Settings, which follows it, with the app to
// drag into the list, as PermissionFlow does for Swift apps. On Windows,
// it opens the page of Settings (Privacy & security). Linux and browsers
// keep no such permissions for apps, and return errors.ErrUnsupported.
//
// Every function is safe to call from any goroutine. OpenSettings and
// Guide need App.Run started, and those that wait (Request, Wait and
// Guide) must not be called from the main thread, which they would block.
package permissionflow

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// A Permission is something the system lets the user give apps.
type Permission string

// Permissions. Which of them a system has, and which of those it can tell
// about without asking, is in the documentation of each.
const (
	// Accessibility lets the app control the computer through the
	// accessibility API: read other apps' windows, move them, and send
	// them input. macOS only; macOS 27 names it Device Control and Data
	// Access.
	Accessibility Permission = "accessibility"
	// ScreenRecording lets the app capture the screen and its audio.
	// macOS only: Windows lets desktop apps capture it. macOS reports
	// a change only after the app starts again.
	ScreenRecording Permission = "screen-recording"
	// InputMonitoring lets the app read the keyboard and the mouse while
	// other apps are in front. macOS only.
	InputMonitoring Permission = "input-monitoring"
	// FullDiskAccess lets the app read the files that macOS protects, such
	// as Mail's and Safari's. macOS only. macOS has no API for it: Check
	// tries to read a protected file.
	FullDiskAccess Permission = "full-disk-access"
	// AppManagement lets the app update and delete other apps. macOS only,
	// and Check can't tell.
	AppManagement Permission = "app-management"
	// DeveloperTools lets the app run software that doesn't meet the
	// system's security policy, as debuggers do. macOS only, and Check
	// can't tell.
	DeveloperTools Permission = "developer-tools"
	// Microphone lets the app record audio: macOS and Windows. Requesting
	// it on macOS needs NSMicrophoneUsageDescription in the Info.plist
	// (macos.infoPlist in mygo.config.ts).
	Microphone Permission = "microphone"
	// Camera lets the app record video: macOS and Windows. Requesting it
	// on macOS needs NSCameraUsageDescription in the Info.plist.
	Camera Permission = "camera"
	// Calendars lets the app read and change the user's calendars. macOS
	// only. Requesting it needs NSCalendarsUsageDescription in the
	// Info.plist, and NSCalendarsFullAccessUsageDescription on macOS 14
	// and later.
	Calendars Permission = "calendars"
	// Reminders lets the app read and change the user's reminders. macOS
	// only. Requesting it needs NSRemindersUsageDescription in the
	// Info.plist, and NSRemindersFullAccessUsageDescription on macOS 14
	// and later.
	Reminders Permission = "reminders"
	// Bluetooth lets the app use Bluetooth devices. macOS only. Requesting
	// it needs NSBluetoothAlwaysUsageDescription in the Info.plist.
	Bluetooth Permission = "bluetooth"
	// MediaAppleMusic lets the app use the user's Apple Music library.
	// macOS only, and Check can't tell.
	MediaAppleMusic Permission = "media-apple-music"
)

// All returns every permission, in the order of System Settings.
func All() []Permission {
	return []Permission{
		Accessibility, ScreenRecording, InputMonitoring, FullDiskAccess,
		AppManagement, DeveloperTools, Microphone, Camera, Calendars,
		Reminders, Bluetooth, MediaAppleMusic,
	}
}

var titles = map[Permission]string{
	Accessibility:   "Accessibility",
	ScreenRecording: "Screen Recording",
	InputMonitoring: "Input Monitoring",
	FullDiskAccess:  "Full Disk Access",
	AppManagement:   "App Management",
	DeveloperTools:  "Developer Tools",
	Microphone:      "Microphone",
	Camera:          "Camera",
	Calendars:       "Calendars",
	Reminders:       "Reminders",
	Bluetooth:       "Bluetooth",
	MediaAppleMusic: "Media & Apple Music",
}

// Title returns the name of the permission as the system's settings show
// it, in English: "Screen Recording".
func (p Permission) Title() string {
	if t, ok := titles[p]; ok {
		return title(p, t)
	}
	return string(p)
}

func (p Permission) String() string { return string(p) }

func (p Permission) valid() bool {
	_, ok := titles[p]
	return ok
}

// Status is whether the app has a permission.
type Status int

// Statuses.
const (
	// Unknown is when the system can't tell without asking, or has no API
	// to tell.
	Unknown Status = iota
	// NotDetermined is when the user hasn't been asked yet: Request asks.
	// macOS doesn't tell for Accessibility, Screen Recording and Input
	// Monitoring, which are Denied until granted.
	NotDetermined
	// Denied is when the user, or the administrator, said no: only the
	// system's settings change it (see OpenSettings and Guide).
	Denied
	// Granted is when the app has the permission.
	Granted
)

func (s Status) String() string {
	switch s {
	case NotDetermined:
		return "not determined"
	case Denied:
		return "denied"
	case Granted:
		return "granted"
	}
	return "unknown"
}

// ErrUsageDescription is returned by Request on macOS when the app's
// Info.plist lacks the description of why it asks, without which macOS
// would end the app.
var ErrUsageDescription = errors.New("permissionflow: the Info.plist lacks the usage description")

// Check reports whether the app has p, without asking the user. It
// returns errors.ErrUnsupported when the system keeps no such permission
// for apps, and Unknown when it does but can't tell.
func Check(p Permission) (Status, error) {
	if !p.valid() {
		return Unknown, fmt.Errorf("permissionflow: unknown permission %q", p)
	}
	return check(p)
}

// Allowed reports whether the app may use what p guards: when it has p,
// or when the system doesn't ask for it, as Linux doesn't for the
// microphone. It is false in browsers.
func Allowed(p Permission) bool {
	st, err := Check(p)
	if errors.Is(err, errors.ErrUnsupported) {
		return ungated
	}
	return st == Granted
}

// Request asks the user for p with the system's own prompt, when it has
// one and hasn't asked yet, and returns the status it then has. macOS asks
// once for each app: after that Request only reports, and the user
// changes their mind in System Settings (see OpenSettings and Guide). For
// the permissions without a prompt (Full Disk Access, App Management,
// Developer Tools, Media & Apple Music, and all of them on Windows), it
// reports what Check does.
//
// macOS adds the app to the list of Accessibility, Screen Recording and
// Input Monitoring when it asks, and opens System Settings itself from
// its prompt, with the switch off: Request returns before the user turns
// it on (see Wait).
func Request(ctx context.Context, p Permission) (Status, error) {
	if !p.valid() {
		return Unknown, fmt.Errorf("permissionflow: unknown permission %q", p)
	}
	return request(ctx, p)
}

// OpenSettings opens the page of the system's settings where the user
// gives p: Privacy & Security in System Settings on macOS, Privacy &
// security in Settings on Windows. It returns errors.ErrUnsupported when
// the system has no such page.
func OpenSettings(p Permission) error {
	if !p.valid() {
		return fmt.Errorf("permissionflow: unknown permission %q", p)
	}
	return openSettings(p)
}

// pollInterval is how often Wait and Guide check the permission.
const pollInterval = 250 * time.Millisecond

// Wait waits until the app has p, and returns Granted, or until ctx is
// done. It returns at once, with the status, when Check can't tell
// (Unknown) or returns an error.
func Wait(ctx context.Context, p Permission) (Status, error) {
	return wait(ctx, func() (Status, error) { return Check(p) }, pollInterval)
}

func wait(ctx context.Context, check func() (Status, error), every time.Duration) (Status, error) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		st, err := check()
		if err != nil || st == Granted || st == Unknown {
			return st, err
		}
		select {
		case <-ctx.Done():
			return st, ctx.Err()
		case <-t.C:
		}
	}
}

// GuideOptions configures Guide.
type GuideOptions struct {
	// App is the path of the app the user drags into the list of System
	// Settings on macOS: the .app bundle the program runs from when it is
	// empty, or the program itself when it isn't in one, as with go run
	// (macOS gives the permission to the app that started it, such as
	// the terminal, which is the one to drag then).
	App string
	// Name is the name the panel gives the app: App's without its
	// extension when it is empty.
	Name string
}

// Guide opens the page of the system's settings where the user gives p,
// as OpenSettings does, and waits for the user to give it.
//
// On macOS, for Accessibility, Screen Recording, Input Monitoring, Full
// Disk Access, App Management, Developer Tools, Bluetooth and Media &
// Apple Music, whose list takes apps dragged into it, it also shows a
// panel under the window of System Settings, which follows the window,
// with the app to drag ("Drag MyApp to the list above to allow
// Accessibility."). The panel stays out of the way of System Settings:
// it never takes the focus. One panel shows at a time: another Guide
// closes the one before, whose Guide returns.
//
// Guide returns when the app has p (when Check can tell), when the user
// closes the panel or System Settings, or when ctx is done, with ctx's
// error. It returns the status of p then, which is Unknown when Check
// can't tell. On Windows it returns when the app has p or when ctx is
// done.
func Guide(ctx context.Context, p Permission, opts GuideOptions) (Status, error) {
	if !p.valid() {
		return Unknown, fmt.Errorf("permissionflow: unknown permission %q", p)
	}
	return guide(ctx, p, opts)
}
