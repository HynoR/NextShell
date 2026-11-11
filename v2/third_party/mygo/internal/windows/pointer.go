//go:build windows && (amd64 || arm64)

package windows

import (
	"unsafe"

	"github.com/egoist/mygo/internal/platform"
)

// Pens and fingers come as WM_POINTER messages, which the surface leaves
// to DefWindowProc to make mouse messages of, as it does for windows that
// handle none: the surface reads their pressure from them, and the mouse
// messages that follow tell what device they come of.
var (
	procGetMessageExtraInfo = user32.NewProc("GetMessageExtraInfo")
	procGetPointerType      = user32.NewProc("GetPointerType")
	procGetPointerPenInfo   = user32.NewProc("GetPointerPenInfo")
	procGetPointerTouchInfo = user32.NewProc("GetPointerTouchInfo")
)

const (
	wmPointerUpdate = 0x0245
	wmPointerDown   = 0x0246
	wmPointerUp     = 0x0247

	// pointerMessageFlagCanceled is POINTER_MESSAGE_FLAG_CANCELED, in the
	// high word of a WM_POINTER message's wParam.
	pointerMessageFlagCanceled = 0x8000

	ptTouch = 2
	ptPen   = 3

	penMaskPressure   = 0x1
	touchMaskPressure = 0x4

	// miWpSignature marks the extra information of mouse messages that
	// Windows makes of a pen's or a finger's (MI_WP_SIGNATURE), with
	// 0x80 set for a finger's.
	miWpSignature = 0xFF515700
	signatureMask = 0xFFFFFF00
)

// pointerInfo is POINTER_INFO.
type pointerInfo struct {
	Type, ID, FrameID, Flags uint32
	SourceDevice, Target     uintptr
	Pixel, Himetric          point
	PixelRaw, HimetricRaw    point
	Time, HistoryCount       uint32
	InputData                int32
	KeyStates                uint32
	PerformanceCount         uint64
	ButtonChangeType         uint32
}

// pointerPenInfo is POINTER_PEN_INFO.
type pointerPenInfo struct {
	Info                  pointerInfo
	Flags, Mask, Pressure uint32
	Rotation              uint32
	TiltX, TiltY          int32
}

// pointerTouchInfo is POINTER_TOUCH_INFO.
type pointerTouchInfo struct {
	Info                  pointerInfo
	Flags, Mask           uint32
	Contact, ContactRaw   rect
	Orientation, Pressure uint32
}

// The pointer messages' information, read on the main thread into these
// rather than into values of each message, which would go to the heap.
var (
	pointerType uint32
	penInfo     pointerPenInfo
	touchInfo   pointerTouchInfo
)

// pointerPressure returns how hard the pen or the finger of WM_POINTER's
// pointer id presses, from 0 to 1, or 0 where it does not tell, as a
// mouse's, with its device and where it is on the screen, in pixels. ok is
// false where Windows tells none of it.
func pointerPressure(id uint32) (pressure float64, kind platform.PointerKind, at point, ok bool) {
	if !has(procGetPointerType) {
		return 0, 0, point{}, false
	}
	if r, _, _ := procGetPointerType.Call(uintptr(id), uintptr(unsafe.Pointer(&pointerType))); r == 0 {
		return 0, 0, point{}, false
	}
	switch pointerType {
	case ptPen:
		if r, _, _ := procGetPointerPenInfo.Call(uintptr(id), uintptr(unsafe.Pointer(&penInfo))); r != 0 {
			if penInfo.Mask&penMaskPressure != 0 {
				pressure = float64(penInfo.Pressure) / 1024
			}
			return pressure, platform.PointerPen, penInfo.Info.Pixel, true
		}
	case ptTouch:
		if r, _, _ := procGetPointerTouchInfo.Call(uintptr(id), uintptr(unsafe.Pointer(&touchInfo))); r != 0 {
			if touchInfo.Mask&touchMaskPressure != 0 {
				pressure = float64(touchInfo.Pressure) / 1024
			}
			return pressure, platform.PointerTouch, touchInfo.Info.Pixel, true
		}
	}
	return 0, 0, point{}, false
}

// pointerMessage notes the pressure of a WM_POINTER message, which the
// mouse messages Windows makes of it carry (mouseDevice). A pen pressing
// harder or softer where it stands makes no mouse message: the surface
// moves the pointer where it is, with the new pressure, as Pointer Events
// fire pointermove.
func (s *surface) pointerMessage(m uint32, wp uintptr) {
	pressure, kind, at, ok := pointerPressure(uint32(loword(wp)))
	if !ok {
		s.pressure = 0
		return
	}
	if m == wmPointerUpdate && s.buttons != 0 && at == s.pointerAt && pressure != s.pressure {
		pt := at
		procScreenToClient.Call(s.hwnd, uintptr(unsafe.Pointer(&pt)))
		s.send(platform.SurfaceEvent{Kind: platform.PointerMove, X: s.toDIP(pt.X), Y: s.toDIP(pt.Y), Mods: mods(), Pointer: kind, Pressure: pressure})
	}
	s.pressure, s.pointerAt = pressure, at
}

// pointerCanceled reports whether a WM_POINTER message's pointer was
// canceled (IS_POINTER_CANCELED_WPARAM): the system took it, and it ends
// with no release.
func pointerCanceled(wp uintptr) bool { return hiword(wp)&pointerMessageFlagCanceled != 0 }

// cancelPress ends the press of the buttons held with no release
// (platform.PointerCancel).
func (s *surface) cancelPress() {
	s.buttons = 0
	s.send(platform.SurfaceEvent{Kind: platform.PointerCancel, Mods: mods()})
}

// mouseDevice returns the device of the mouse message being handled, as
// Windows marks those it makes of a pen's or a finger's, with the pressure
// that the pointer message before it told.
func (s *surface) mouseDevice() (platform.PointerKind, float64) {
	extra, _, _ := procGetMessageExtraInfo.Call()
	switch {
	case uint32(extra)&signatureMask != miWpSignature:
		return platform.PointerMouse, 0
	case extra&0x80 != 0:
		return platform.PointerTouch, s.pressure
	}
	return platform.PointerPen, s.pressure
}
