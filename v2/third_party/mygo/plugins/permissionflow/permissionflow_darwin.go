//go:build darwin

package permissionflow

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	"github.com/egoist/mygo"
)

// macOS keeps the permissions in TCC, which the frameworks ask: the
// accessibility API (ApplicationServices), CoreGraphics for the screen and
// the input, AVFoundation for the microphone and the camera, EventKit for
// calendars and reminders, and CoreBluetooth. Their frameworks are loaded
// when a permission of theirs is first checked.

const (
	applicationServicesPath = "/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices"
	coreGraphicsPath        = "/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics"
	coreFoundationPath      = "/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation"
	avFoundationPath        = "/System/Library/Frameworks/AVFoundation.framework/AVFoundation"
	eventKitPath            = "/System/Library/Frameworks/EventKit.framework/EventKit"
	coreBluetoothPath       = "/System/Library/Frameworks/CoreBluetooth.framework/CoreBluetooth"
	appKitPath              = "/System/Library/Frameworks/AppKit.framework/AppKit"
	libObjCPath             = "/usr/lib/libobjc.A.dylib"

	settingsBundleID = "com.apple.systempreferences"

	// TCC's authorization statuses, as AVFoundation, EventKit and
	// CoreBluetooth number them.
	tccNotDetermined = 0
	tccRestricted    = 1
	tccDenied        = 2
	tccAuthorized    = 3

	ekEntityTypeEvent    = 0
	ekEntityTypeReminder = 1
)

var (
	coreOnce sync.Once
	coreErr  error

	axIsProcessTrusted             func() bool
	axIsProcessTrustedWithOptions  func(options objc.ID) bool
	cgPreflightScreenCaptureAccess func() bool
	cgRequestScreenCaptureAccess   func() bool
	cgPreflightListenEventAccess   func() bool
	cgRequestListenEventAccess     func() bool
	cgWindowListCopyWindowInfo     func(option, relativeTo uint32) objc.ID
	cgEventSourceButtonState       func(state int32, button uint32) bool
	cfRelease                      func(obj objc.ID)

	// Messages of the types that Guide sends 30 times a second, which
	// objc.Send would register anew each time.
	sendUint  func(obj objc.ID, s objc.SEL) uint
	sendInt   func(obj objc.ID, s objc.SEL) int
	sendFloat func(obj objc.ID, s objc.SEL) float64

	poolPush, poolPop uintptr
	avMediaTypeAudio  objc.ID
	avMediaTypeVideo  objc.ID

	frameworks sync.Map // path → error of loading it

	macMajor int // macOS's major version
)

func sel(name string) objc.SEL { return objc.RegisterName(name) }

func class(name string) objc.ID { return objc.ID(objc.GetClass(name)) }

func loadCore() error {
	coreOnce.Do(func() {
		libs := map[string]uintptr{}
		for _, path := range []string{applicationServicesPath, coreGraphicsPath, coreFoundationPath, appKitPath, libObjCPath} {
			h, err := purego.Dlopen(path, purego.RTLD_LAZY|purego.RTLD_GLOBAL)
			if err != nil {
				coreErr = fmt.Errorf("permissionflow: %w", err)
				return
			}
			libs[path] = h
		}
		for _, f := range []struct {
			fn        any
			lib, name string
		}{
			{&axIsProcessTrusted, applicationServicesPath, "AXIsProcessTrusted"},
			{&axIsProcessTrustedWithOptions, applicationServicesPath, "AXIsProcessTrustedWithOptions"},
			{&cgPreflightScreenCaptureAccess, coreGraphicsPath, "CGPreflightScreenCaptureAccess"},
			{&cgRequestScreenCaptureAccess, coreGraphicsPath, "CGRequestScreenCaptureAccess"},
			{&cgPreflightListenEventAccess, coreGraphicsPath, "CGPreflightListenEventAccess"},
			{&cgRequestListenEventAccess, coreGraphicsPath, "CGRequestListenEventAccess"},
			{&cgWindowListCopyWindowInfo, coreGraphicsPath, "CGWindowListCopyWindowInfo"},
			{&cgEventSourceButtonState, coreGraphicsPath, "CGEventSourceButtonState"},
			{&cfRelease, coreFoundationPath, "CFRelease"},
		} {
			p, err := purego.Dlsym(libs[f.lib], f.name)
			if err != nil {
				coreErr = fmt.Errorf("permissionflow: missing %s (macOS 10.15 and later): %w", f.name, errors.ErrUnsupported)
				return
			}
			purego.RegisterFunc(f.fn, p)
		}
		msgSend, err := purego.Dlsym(libs[libObjCPath], "objc_msgSend")
		if err != nil {
			coreErr = fmt.Errorf("permissionflow: %w", err)
			return
		}
		purego.RegisterFunc(&sendUint, msgSend)
		purego.RegisterFunc(&sendInt, msgSend)
		purego.RegisterFunc(&sendFloat, msgSend)
		poolPush, _ = purego.Dlsym(libs[libObjCPath], "objc_autoreleasePoolPush")
		poolPop, _ = purego.Dlsym(libs[libObjCPath], "objc_autoreleasePoolPop")
		withPool(func() {
			type osVersion struct{ Major, Minor, Patch int }
			info := class("NSProcessInfo").Send(sel("processInfo"))
			macMajor = objc.Send[osVersion](info, sel("operatingSystemVersion")).Major
		})
	})
	return coreErr
}

// loadFramework loads the framework at path once, for its classes.
func loadFramework(path string) (uintptr, error) {
	type loaded struct {
		h   uintptr
		err error
	}
	if v, ok := frameworks.Load(path); ok {
		return v.(loaded).h, v.(loaded).err
	}
	h, err := purego.Dlopen(path, purego.RTLD_LAZY|purego.RTLD_GLOBAL)
	if err != nil {
		err = fmt.Errorf("permissionflow: %w", err)
	}
	v, _ := frameworks.LoadOrStore(path, loaded{h, err})
	return v.(loaded).h, v.(loaded).err
}

func loadAVFoundation() error {
	h, err := loadFramework(avFoundationPath)
	if err != nil {
		return err
	}
	if avMediaTypeAudio == 0 {
		audio, err1 := purego.Dlsym(h, "AVMediaTypeAudio")
		video, err2 := purego.Dlsym(h, "AVMediaTypeVideo")
		if err1 != nil || err2 != nil {
			return fmt.Errorf("permissionflow: AVFoundation lacks its media types: %w", errors.ErrUnsupported)
		}
		avMediaTypeAudio = **(**objc.ID)(unsafe.Pointer(&audio))
		avMediaTypeVideo = **(**objc.ID)(unsafe.Pointer(&video))
	}
	return nil
}

// withPool runs fn in an autorelease pool, on one thread.
func withPool(fn func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	p, _, _ := purego.SyscallN(poolPush)
	defer purego.SyscallN(poolPop, p)
	fn()
}

func cstr(s string) *byte {
	b := append([]byte(s), 0)
	return &b[0]
}

// nsString returns an autoreleased NSString.
func nsString(s string) objc.ID {
	return class("NSString").Send(sel("stringWithUTF8String:"), cstr(s))
}

func goString(s objc.ID) string {
	if s == 0 {
		return ""
	}
	p := objc.Send[unsafe.Pointer](s, sel("UTF8String"))
	if p == nil {
		return ""
	}
	n := 0
	for *(*byte)(unsafe.Add(p, n)) != 0 {
		n++
	}
	return string(unsafe.Slice((*byte)(p), n))
}

const ungated = true

func title(p Permission, t string) string {
	// macOS 27 renamed Accessibility; its anchor stays.
	if p == Accessibility && loadCore() == nil && macMajor >= 27 {
		return "Device Control and Data Access"
	}
	return t
}

// tccStatus converts a status of AVFoundation, EventKit or CoreBluetooth.
// EventKit's 4, write only, isn't the access asked for.
func tccStatus(v int) Status {
	switch v {
	case tccNotDetermined:
		return NotDetermined
	case tccAuthorized:
		return Granted
	}
	return Denied
}

func granted(ok bool) Status {
	if ok {
		return Granted
	}
	return Denied
}

func check(p Permission) (st Status, err error) {
	if err := loadCore(); err != nil {
		return Unknown, err
	}
	switch p {
	case Accessibility:
		return granted(axIsProcessTrusted()), nil
	case ScreenRecording:
		return granted(cgPreflightScreenCaptureAccess()), nil
	case InputMonitoring:
		return granted(cgPreflightListenEventAccess()), nil
	case FullDiskAccess:
		return fullDiskAccess(), nil
	case Microphone, Camera:
		if err := loadAVFoundation(); err != nil {
			return Unknown, err
		}
		withPool(func() {
			st = tccStatus(objc.Send[int](class("AVCaptureDevice"), sel("authorizationStatusForMediaType:"), mediaType(p)))
		})
		return st, nil
	case Calendars, Reminders:
		if _, err := loadFramework(eventKitPath); err != nil {
			return Unknown, err
		}
		withPool(func() {
			st = tccStatus(objc.Send[int](class("EKEventStore"), sel("authorizationStatusForEntityType:"), entityType(p)))
		})
		return st, nil
	case Bluetooth:
		if _, err := loadFramework(coreBluetoothPath); err != nil {
			return Unknown, err
		}
		cb := class("CBManager")
		if cb == 0 || !objc.Send[bool](cb, sel("respondsToSelector:"), sel("authorization")) {
			return Unknown, nil
		}
		withPool(func() { st = tccStatus(objc.Send[int](cb, sel("authorization"))) })
		return st, nil
	}
	return Unknown, nil
}

func mediaType(p Permission) objc.ID {
	if p == Camera {
		return avMediaTypeVideo
	}
	return avMediaTypeAudio
}

func entityType(p Permission) int {
	if p == Reminders {
		return ekEntityTypeReminder
	}
	return ekEntityTypeEvent
}

// fullDiskAccess tries to read files that only apps with Full Disk Access
// read: TCC's own database of the user, then Safari's files.
func fullDiskAccess() Status {
	home, err := os.UserHomeDir()
	if err != nil {
		return Unknown
	}
	f, err := os.Open(filepath.Join(home, "Library/Application Support/com.apple.TCC/TCC.db"))
	if err == nil {
		f.Close()
		return Granted
	}
	if errors.Is(err, fs.ErrPermission) {
		return Denied
	}
	if _, err := os.ReadDir(filepath.Join(home, "Library/Safari")); err == nil {
		return Granted
	} else if errors.Is(err, fs.ErrPermission) {
		return Denied
	}
	return Unknown
}

// usageDescription checks that the app's Info.plist describes why it asks,
// as macOS ends apps in bundles that ask without. A program outside a
// bundle asks for the app that started it.
func usageDescription(keys ...string) error {
	var err error
	withPool(func() {
		b := class("NSBundle").Send(sel("mainBundle"))
		if !strings.HasSuffix(goString(b.Send(sel("bundlePath"))), ".app") {
			return
		}
		for _, k := range keys {
			if b.Send(sel("objectForInfoDictionaryKey:"), nsString(k)) == 0 {
				err = fmt.Errorf("%w: add %s to macos.infoPlist", ErrUsageDescription, k)
				return
			}
		}
	})
	return err
}

var (
	eventStore     objc.ID // kept: a request dies with its store
	centralManager objc.ID // kept: Bluetooth asks while it lives
	requestMu      sync.Mutex
)

func request(ctx context.Context, p Permission) (Status, error) {
	st, err := check(p)
	if err != nil {
		return st, err
	}
	switch p {
	case Accessibility:
		if st == Granted {
			return st, nil
		}
		withPool(func() {
			opts := class("NSDictionary").Send(sel("dictionaryWithObject:forKey:"),
				class("NSNumber").Send(sel("numberWithBool:"), true), nsString("AXTrustedCheckOptionPrompt"))
			st = granted(axIsProcessTrustedWithOptions(opts))
		})
		return st, nil
	case ScreenRecording:
		if st == Granted {
			return st, nil
		}
		return granted(cgRequestScreenCaptureAccess()), nil
	case InputMonitoring:
		if st == Granted {
			return st, nil
		}
		return granted(cgRequestListenEventAccess()), nil
	}
	if st != NotDetermined {
		return st, nil
	}
	done := make(chan struct{}, 1)
	blk := objc.NewBlock(func(objc.Block, bool, objc.ID) {
		select {
		case done <- struct{}{}:
		default:
		}
	})
	defer blk.Release()
	switch p {
	case Microphone, Camera:
		key := "NSMicrophoneUsageDescription"
		if p == Camera {
			key = "NSCameraUsageDescription"
		}
		if err := usageDescription(key); err != nil {
			return st, err
		}
		withPool(func() {
			class("AVCaptureDevice").Send(sel("requestAccessForMediaType:completionHandler:"), mediaType(p), blk)
		})
	case Calendars, Reminders:
		modern := objc.Send[bool](class("EKEventStore"), sel("instancesRespondToSelector:"), sel("requestFullAccessToEventsWithCompletion:"))
		keys := []string{"NSCalendarsUsageDescription"}
		if p == Reminders {
			keys = []string{"NSRemindersUsageDescription"}
		}
		if modern {
			keys[0] = strings.Replace(keys[0], "UsageDescription", "FullAccessUsageDescription", 1)
		}
		if err := usageDescription(keys...); err != nil {
			return st, err
		}
		requestMu.Lock()
		if eventStore == 0 {
			eventStore = class("EKEventStore").Send(sel("alloc")).Send(sel("init"))
		}
		store := eventStore
		requestMu.Unlock()
		withPool(func() {
			switch {
			case modern && p == Calendars:
				store.Send(sel("requestFullAccessToEventsWithCompletion:"), blk)
			case modern:
				store.Send(sel("requestFullAccessToRemindersWithCompletion:"), blk)
			default:
				store.Send(sel("requestAccessToEntityType:completion:"), entityType(p), blk)
			}
		})
	case Bluetooth:
		if err := usageDescription("NSBluetoothAlwaysUsageDescription"); err != nil {
			return st, err
		}
		requestMu.Lock()
		if centralManager == 0 {
			// Its delegate would hear of the state of the radio:
			// the prompt shows without one.
			centralManager = class("CBCentralManager").Send(sel("alloc")).Send(sel("initWithDelegate:queue:"), objc.ID(0), objc.ID(0))
		}
		requestMu.Unlock()
		// CoreBluetooth calls no block: wait for the status to change.
		return waitDetermined(ctx, p)
	default:
		return st, nil
	}
	select {
	case <-done:
	case <-ctx.Done():
		return st, ctx.Err()
	}
	return check(p)
}

// waitDetermined waits until the user has answered for p.
func waitDetermined(ctx context.Context, p Permission) (Status, error) {
	t := time.NewTicker(pollInterval)
	defer t.Stop()
	for {
		st, err := check(p)
		if err != nil || st != NotDetermined {
			return st, err
		}
		select {
		case <-ctx.Done():
			return st, ctx.Err()
		case <-t.C:
		}
	}
}

func openSettings(p Permission) error {
	if err := loadCore(); err != nil {
		return err
	}
	ok := false
	mygo.RunOnMain(func() {
		withPool(func() {
			u := class("NSURL").Send(sel("URLWithString:"), nsString(macSettingsURL(p)))
			ws := class("NSWorkspace").Send(sel("sharedWorkspace"))
			ok = objc.Send[bool](ws, sel("openURL:"), u)
		})
	})
	if !ok {
		return fmt.Errorf("permissionflow: System Settings didn't open %s", macSettingsURL(p))
	}
	return nil
}
