//go:build windows

package permissionflow

import (
	"context"
	"errors"
	"fmt"
	"syscall"
	"unsafe"

	"github.com/egoist/mygo"
)

// Windows keeps the microphone and the camera from desktop apps with the
// switches of its consent store, in the registry, which Settings' pages
// of Privacy & security turn on and off. It has no prompt for them, and
// keeps nothing else from desktop apps.

const consentStore = `Software\Microsoft\Windows\CurrentVersion\CapabilityAccessManager\ConsentStore\`

var procShellExecuteW = syscall.NewLazyDLL("shell32.dll").NewProc("ShellExecuteW")

const ungated = true

func title(_ Permission, t string) string { return t }

// registryString returns a string value of the registry, "" when it
// isn't there.
func registryString(root syscall.Handle, path, name string) string {
	var key syscall.Handle
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return ""
	}
	if syscall.RegOpenKeyEx(root, p, 0, syscall.KEY_READ, &key) != nil {
		return ""
	}
	defer syscall.RegCloseKey(key)
	n, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return ""
	}
	var typ, size uint32
	if syscall.RegQueryValueEx(key, n, nil, &typ, nil, &size) != nil || typ != syscall.REG_SZ || size == 0 {
		return ""
	}
	buf := make([]uint16, (size+1)/2)
	if syscall.RegQueryValueEx(key, n, nil, &typ, (*byte)(unsafe.Pointer(&buf[0])), &size) != nil {
		return ""
	}
	return syscall.UTF16ToString(buf)
}

func check(p Permission) (Status, error) {
	c, ok := windowsCapabilities[p]
	if !ok {
		return Unknown, fmt.Errorf("permissionflow: Windows has no %s permission: %w", p.Title(), errors.ErrUnsupported)
	}
	path := consentStore + c.capability
	return windowsConsent(
		registryString(syscall.HKEY_LOCAL_MACHINE, path, "Value"),
		registryString(syscall.HKEY_CURRENT_USER, path, "Value"),
		registryString(syscall.HKEY_CURRENT_USER, path+`\NonPackaged`, "Value"),
	), nil
}

func request(_ context.Context, p Permission) (Status, error) { return check(p) }

func openSettings(p Permission) error {
	c, ok := windowsCapabilities[p]
	if !ok {
		return fmt.Errorf("permissionflow: Windows has no %s permission: %w", p.Title(), errors.ErrUnsupported)
	}
	verb, _ := syscall.UTF16PtrFromString("open")
	uri, _ := syscall.UTF16PtrFromString(c.uri)
	var r uintptr
	// The shell runs in the apartment of the main thread.
	mygo.RunOnMain(func() {
		r, _, _ = procShellExecuteW.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(uri)), 0, 0, 1)
	})
	if r <= 32 {
		return fmt.Errorf("permissionflow: Settings didn't open %s (%d)", c.uri, r)
	}
	return nil
}

func guide(ctx context.Context, p Permission, _ GuideOptions) (Status, error) {
	if err := openSettings(p); err != nil {
		return Unknown, err
	}
	return Wait(ctx, p)
}
