//go:build !darwin && !windows

package permissionflow

import (
	"context"
	"errors"
	"runtime"
)

// Linux keeps none of these permissions for apps outside sandboxes, and
// browsers keep theirs for pages.

const ungated = runtime.GOOS != "js"

func title(_ Permission, t string) string { return t }

func check(Permission) (Status, error) { return Unknown, errors.ErrUnsupported }

func request(context.Context, Permission) (Status, error) { return Unknown, errors.ErrUnsupported }

func openSettings(Permission) error { return errors.ErrUnsupported }

func guide(context.Context, Permission, GuideOptions) (Status, error) {
	return Unknown, errors.ErrUnsupported
}
