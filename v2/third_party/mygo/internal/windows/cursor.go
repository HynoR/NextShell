//go:build windows && (amd64 || arm64)

package windows

import (
	"math"
	"unsafe"

	"github.com/egoist/mygo/internal/platform"
)

var procCreateCursor = user32.NewProc("CreateCursor")

const smCxCursor = 13

// drawnCursor creates the cursors Windows has none of, as browsers bring
// their own: a magnifier with a plus or a minus for zooming, and a thick
// plus selecting cells. It returns 0 for the others.
func drawnCursor(c platform.Cursor) uintptr {
	if c != platform.CursorZoomIn && c != platform.CursorZoomOut && c != platform.CursorCell {
		return 0
	}
	n, _, _ := procGetSystemMetrics.Call(smCxCursor)
	size := int(max(32, min(n, 256))) &^ 15 // whole WORDs a row
	and, xor, hx, hy := cursorMasks(c, size)
	h, _, _ := procCreateCursor.Call(instance(), uintptr(hx), uintptr(hy), uintptr(size), uintptr(size),
		uintptr(unsafe.Pointer(&and[0])), uintptr(unsafe.Pointer(&xor[0])))
	return h
}

// cursorMasks draws c on size by size pixels, as CreateCursor takes it: a
// bit a pixel, the most significant first, AND 1 and XOR 0 where the
// screen shows through, both 0 for black and XOR 1 for white. hx, hy is
// the hot spot. The shapes are drawn on a grid of 32 scaled to size.
func cursorMasks(c platform.Cursor, size int) (and, xor []byte, hx, hy int) {
	f := float64(size) / 32
	stride := size / 8
	and, xor = make([]byte, stride*size), make([]byte, stride*size)
	black := make([]bool, size*size)
	white := make([]bool, size*size)
	at := func(i, j int) (x, y float64) { return (float64(i) + 0.5) / f, (float64(j) + 0.5) / f }
	if c == platform.CursorCell {
		// A white plus with a black edge.
		const arm, half = 10.5, 2.5
		plus := func(x, y, grow float64) bool {
			dx, dy := math.Abs(x-16), math.Abs(y-16)
			return dx <= half+grow && dy <= arm+grow || dy <= half+grow && dx <= arm+grow
		}
		for j := range size {
			for i := range size {
				x, y := at(i, j)
				white[j*size+i] = plus(x, y, 0)
				black[j*size+i] = !white[j*size+i] && plus(x, y, 1.25)
			}
		}
		hx, hy = size/2, size/2
	} else {
		// A black ring around a white lens, with a handle down to the
		// right and a plus or a minus in the lens.
		const cx, cy, r, ring = 12, 12, 9.5, 2.25
		for j := range size {
			for i := range size {
				x, y := at(i, j)
				d := math.Hypot(x-cx, y-cy)
				// The handle, along the diagonal from the ring outward.
				t := max(0, min(1, ((x-17.5)+(y-17.5))/2/9.5))
				hdx, hdy := x-(17.5+9.5*t), y-(17.5+9.5*t)
				sign := math.Abs(y-cy) <= 1.25 && math.Abs(x-cx) <= 4.75
				if c == platform.CursorZoomIn {
					sign = sign || math.Abs(x-cx) <= 1.25 && math.Abs(y-cy) <= 4.75
				}
				b := d >= r-ring && (d <= r || math.Hypot(hdx, hdy) <= 2.25) || sign
				black[j*size+i] = b
				white[j*size+i] = !b && d < r-ring
			}
		}
		// A white edge around the black, which shows on dark backgrounds.
		for j := range size {
			for i := range size {
				if black[j*size+i] || white[j*size+i] {
					continue
				}
				for k := range 9 {
					ni, nj := i+k%3-1, j+k/3-1
					if ni >= 0 && nj >= 0 && ni < size && nj < size && black[nj*size+ni] {
						white[j*size+i] = true
						break
					}
				}
			}
		}
		hx, hy = int(cx*f), int(cy*f)
	}
	for j := range size {
		for i := range size {
			bit := byte(0x80) >> (i % 8)
			switch k := j*stride + i/8; {
			case white[j*size+i]:
				xor[k] |= bit
			case !black[j*size+i]:
				and[k] |= bit
			}
		}
	}
	return and, xor, hx, hy
}
