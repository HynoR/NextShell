//go:build windows && (amd64 || arm64)

package windows

import (
	"errors"
	"os"
	"syscall"
	"time"
	"unsafe"
)

var procReplaceFileW = kernel32.NewProc("ReplaceFileW")

const (
	replacefileIgnoreMergeErrors = 0x2
	replacefileIgnoreACLErrors   = 0x4

	errorUnableToRemoveReplaced   = syscall.Errno(1175)
	errorUnableToMoveReplacement  = syscall.Errno(1176)
	errorUnableToMoveReplacement2 = syscall.Errno(1177)
	errorSharingViolation         = syscall.Errno(32)
	errorLockViolation            = syscall.Errno(33)
)

// replaceFileW calls ReplaceFileW without a backup file. Tests replace it
// to fail as the system does.
var replaceFileW = func(target, replacement *uint16) error {
	ok, _, err := procReplaceFileW.Call(uintptr(unsafe.Pointer(target)), uintptr(unsafe.Pointer(replacement)), 0,
		replacefileIgnoreMergeErrors|replacefileIgnoreACLErrors, 0, 0)
	if ok != 0 {
		return nil
	}
	return err
}

// ReplaceFile replaces target with ReplaceFileW, which keeps the target's
// security descriptor, attributes, alternate data streams and creation
// time, as Office and NSDocument's replacement do. Another program holding
// the target open without sharing deletes (an indexer, an antivirus, a
// cloud client) gets a moment to let go.
func (*Backend) ReplaceFile(target, replacement string) error {
	if _, err := os.Lstat(target); os.IsNotExist(err) {
		return os.Rename(replacement, target)
	}
	t, err := syscall.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	r, err := syscall.UTF16PtrFromString(replacement)
	if err != nil {
		return err
	}
	for attempt := 0; ; attempt++ {
		callErr := replaceFileW(t, r)
		if callErr == nil {
			return nil
		}
		errno, _ := callErr.(syscall.Errno)
		if errno == errorUnableToMoveReplacement || errno == errorUnableToMoveReplacement2 {
			// Without a backup file, ReplaceFileW has deleted the target
			// (1176) or moved it to a name of its own (1177), and the
			// replacement, which holds the new contents, keeps its name.
			return moveReplacement(replacement, target)
		}
		if !busy(errno) || attempt == 9 {
			return &os.LinkError{Op: "replace", Old: replacement, New: target, Err: callErr}
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// moveReplacement renames replacement to target once ReplaceFileW has
// stopped halfway, waiting, as ReplaceFile does, for a program holding the
// replacement open, which is what usually stops it.
func moveReplacement(replacement, target string) error {
	for attempt := 0; ; attempt++ {
		err := os.Rename(replacement, target)
		var errno syscall.Errno
		if err == nil || !errors.As(err, &errno) || !busy(errno) || attempt == 9 {
			return err
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// busy reports whether another program holding a file open may be why an
// operation on it failed.
func busy(errno syscall.Errno) bool {
	return errno == errorSharingViolation || errno == errorLockViolation ||
		errno == syscall.ERROR_ACCESS_DENIED || errno == errorUnableToRemoveReplaced
}
