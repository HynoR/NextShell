//go:build linux && (amd64 || arm64)

package linux

import "slices"

// keysDown are the hardware keycodes of the keys pressed in a surface and
// not let go of yet, by which a press tells an auto-repeat
// (SurfaceEvent.Repeat): GdkEventKey flags none, but the repeats of a key
// held come as presses with no release between them, on X11 with
// detectable auto-repeat, which GDK turns on, and on Wayland, where GTK
// repeats the keys itself.
type keysDown []uint16

// press notes the key of hardware keycode code pressed, and reports
// whether it was held already: an auto-repeat.
func (d *keysDown) press(code uint16) (repeat bool) {
	if code == 0 {
		return false
	}
	if slices.Contains(*d, code) {
		return true
	}
	*d = append(*d, code)
	return false
}

// release notes the key of hardware keycode code let go of.
func (d *keysDown) release(code uint16) {
	if i := slices.Index(*d, code); i >= 0 {
		*d = slices.Delete(*d, i, i+1)
	}
}

// otherSideHeld reports whether the modifier of hardware keycode code has
// its twin of the other side held: evdev's Shift, Control, Alt and Super,
// plus X's 8.
func (d keysDown) otherSideHeld(code uint16) bool {
	var other uint16
	switch code {
	case 42 + 8:
		other = 54 + 8
	case 54 + 8:
		other = 42 + 8
	case 29 + 8:
		other = 97 + 8
	case 97 + 8:
		other = 29 + 8
	case 56 + 8:
		other = 100 + 8
	case 100 + 8:
		other = 56 + 8
	case 125 + 8:
		other = 126 + 8
	case 126 + 8:
		other = 125 + 8
	default:
		return false
	}
	return slices.Contains(d, other)
}

// clear lets go of every key, as the surface loses the keyboard, whose
// releases go elsewhere.
func (d *keysDown) clear() { *d = (*d)[:0] }
