//go:build windows && (amd64 || arm64)

package windows

import (
	"unsafe"

	"github.com/egoist/mygo/internal/platform"
)

// WindowStateBounds is the window's rectangle in the screen's pixels,
// which Bounds rounds through the scale of the window's display.
func (w *window) WindowStateBounds() platform.Rect {
	var r rect
	procGetWindowRect.Call(w.hwnd, uintptr(unsafe.Pointer(&r)))
	return pixelRect(r)
}

// RestoreWindowState finds the display of a state's pixels, which its
// DIPs cannot tell when displays have different scales, and the window's
// size in that display's scale.
func (b *Backend) RestoreWindowState(o *platform.WindowOptions, pixels platform.Rect) bool {
	r, ok := restoreStateBounds(pixels, platform.Size{Width: o.Width, Height: o.Height}, screenDisplays())
	if !ok {
		return false
	}
	o.StateBounds, o.Center = &r, false
	return true
}

func pixelRect(r rect) platform.Rect {
	return platform.Rect{X: int(r.Left), Y: int(r.Top), Width: int(r.Right - r.Left), Height: int(r.Bottom - r.Top)}
}

// restoreStateBounds places a window of size, in DIPs, at saved, in
// pixels, if enough of it shows on a display to reach it, as onDisplay
// decides in the core. Its size is in the scale of the display with the
// most of saved, whose scale Windows gives a window across displays.
func restoreStateBounds(saved platform.Rect, size platform.Size, displays []screenDisplay) (platform.Rect, bool) {
	if saved.Width <= 0 || saved.Height <= 0 || size.Width <= 0 || size.Height <= 0 {
		return platform.Rect{}, false
	}
	target, largest := -1, int64(0)
	for i, d := range displays {
		w, h := stateOverlap(saved, d.bounds)
		if area := int64(w) * int64(h); area > largest {
			target, largest = i, area
		}
	}
	if target < 0 {
		return platform.Rect{}, false
	}
	dpi := displays[target].dpi
	r := platform.Rect{X: saved.X, Y: saved.Y, Width: int(toPx(size.Width, dpi)), Height: int(toPx(size.Height, dpi))}
	for _, d := range displays {
		w, h := stateOverlap(r, d.work)
		if w >= int(toPx(min(100, size.Width), d.dpi)) && h >= int(toPx(min(50, size.Height), d.dpi)) {
			return r, true
		}
	}
	return platform.Rect{}, false
}

func stateOverlap(a, b platform.Rect) (int, int) {
	return max(0, min(a.X+a.Width, b.X+b.Width)-max(a.X, b.X)),
		max(0, min(a.Y+a.Height, b.Y+b.Height)-max(a.Y, b.Y))
}
