package windows

// This file has no build constraint: the mapping between the screen's
// pixels and DIPs is plain arithmetic over the displays, tested on every
// platform.

import "github.com/egoist/mygo/internal/platform"

// screenDisplay is a display in the screen's pixels, in which displays
// never overlap, with its DPI. handle is its HMONITOR.
type screenDisplay struct {
	bounds, work platform.Rect
	dpi          int
	handle       uintptr
}

// dipSpace places the displays in one space of DIPs, the public API's
// screen coordinates, in which they never overlap, so that a point in
// DIPs names one display, and the windows, the cursor and the displays
// agree on where things are.
//
// A display's DIPs start at its top-left pixel scaled by the smallest
// scale of all the displays, and go on in its own scale from there:
//
//	DIP = origin × 96 / minDPI + (pixel − origin) × 96 / dpi
//
// Qt anchors each display at its top-left pixel too. Scaling the whole
// screen by the smallest scale keeps the displays apart, and every
// display then shrinks towards its top-left corner inside its share, by
// its own scale over the smallest. Where every display has the same scale
// the screen is scaled as one, as it always was, and the primary display,
// whose top-left pixel is 0, 0, keeps its DIPs whatever the others'
// scales. Displays of different scales may have gaps between them, which
// a window can still be in: it is placed by the display it is the most
// on, or the nearest.
type dipSpace struct {
	displays []screenDisplay
	minDPI   int
}

// newDIPSpace returns the space of displays. Without displays, it maps
// pixels at fallback DPI, from 0, 0.
func newDIPSpace(displays []screenDisplay, fallback int) dipSpace {
	if fallback <= 0 {
		fallback = 96
	}
	s := dipSpace{minDPI: fallback}
	for _, d := range displays {
		if d.dpi <= 0 {
			d.dpi = fallback
		}
		s.displays = append(s.displays, d)
	}
	for i, d := range s.displays {
		if i == 0 || d.dpi < s.minDPI {
			s.minDPI = d.dpi
		}
	}
	return s
}

// scaled returns v × num / den rounded to the nearest integer, halves up,
// for negative v too.
func scaled(v, num, den int) int { return floorDiv(2*v*num+den, 2*den) }

// origin returns where display i's top-left pixel is in DIPs, and its
// DPI. Without displays, it is 0, 0 at the fallback DPI.
func (s dipSpace) origin(i int) (px, dip platform.Point, dpi int) {
	if i < 0 || i >= len(s.displays) {
		return platform.Point{}, platform.Point{}, s.minDPI
	}
	d := s.displays[i]
	px = platform.Point{X: d.bounds.X, Y: d.bounds.Y}
	dip = platform.Point{X: scaled(px.X, 96, s.minDPI), Y: scaled(px.Y, 96, s.minDPI)}
	return px, dip, d.dpi
}

// toDIPOn converts r, in pixels, through display i's scale.
func (s dipSpace) toDIPOn(i int, r platform.Rect) platform.Rect {
	px, dip, dpi := s.origin(i)
	return platform.Rect{
		X: dip.X + scaled(r.X-px.X, 96, dpi), Y: dip.Y + scaled(r.Y-px.Y, 96, dpi),
		Width: scaled(r.Width, 96, dpi), Height: scaled(r.Height, 96, dpi),
	}
}

// toPixelsOn converts r, in DIPs, through display i's scale.
func (s dipSpace) toPixelsOn(i int, r platform.Rect) platform.Rect {
	px, dip, dpi := s.origin(i)
	return platform.Rect{
		X: px.X + scaled(r.X-dip.X, dpi, 96), Y: px.Y + scaled(r.Y-dip.Y, dpi, 96),
		Width: scaled(r.Width, dpi, 96), Height: scaled(r.Height, dpi, 96),
	}
}

// dpiOf returns display i's DPI.
func (s dipSpace) dpiOf(i int) int {
	_, _, dpi := s.origin(i)
	return dpi
}

// displayDIPs returns display i's bounds and work area in DIPs.
func (s dipSpace) displayDIPs(i int) (bounds, work platform.Rect) {
	d := s.displays[i]
	return s.toDIPOn(i, d.bounds), s.toDIPOn(i, d.work)
}

// pixelDisplay returns the display r, in pixels, is the most on, as
// MonitorFromRect finds it, whose DPI Windows gives a window there, or
// the nearest; -1 without displays.
func (s dipSpace) pixelDisplay(r platform.Rect) int {
	return mostOn(r, len(s.displays), func(i int) platform.Rect { return s.displays[i].bounds })
}

// dipDisplay returns the display r, in DIPs, is the most on, or the
// nearest; -1 without displays.
func (s dipSpace) dipDisplay(r platform.Rect) int {
	return mostOn(r, len(s.displays), func(i int) platform.Rect {
		b, _ := s.displayDIPs(i)
		return b
	})
}

// rectToDIP converts a rectangle of the screen's pixels, such as a
// window's, to DIPs, in the scale of the display it is the most on.
func (s dipSpace) rectToDIP(r platform.Rect) platform.Rect {
	return s.toDIPOn(s.pixelDisplay(r), r)
}

// rectToPixels converts a rectangle in DIPs, such as a window's bounds, to
// the screen's pixels, and returns the display whose scale it took: the
// one it is the most on in DIPs, whose rectangle in pixels is the most on
// it too, so that rectToDIP gives it back. Where none is, as for a
// rectangle across displays of very different scales, it takes the scale
// of the display it is the most on in DIPs.
func (s dipSpace) rectToPixels(r platform.Rect) (platform.Rect, int) {
	order := byOverlap(r, len(s.displays), func(i int) platform.Rect {
		b, _ := s.displayDIPs(i)
		return b
	})
	for _, i := range order {
		if px := s.toPixelsOn(i, r); s.pixelDisplay(px) == i {
			return px, i
		}
	}
	if len(order) == 0 {
		return s.toPixelsOn(-1, r), -1
	}
	return s.toPixelsOn(order[0], r), order[0]
}

// pointToDIP converts a pixel of the screen, such as the cursor's, to the
// DIP it is in, in the scale of the display it is on, or the nearest: a
// pixel of a display is in its DIPs, its last one too.
func (s dipSpace) pointToDIP(x, y int) platform.Point {
	i := s.pixelDisplay(platform.Rect{X: x, Y: y, Width: 1, Height: 1})
	px, dip, dpi := s.origin(i)
	return platform.Point{X: dip.X + floorDiv((x-px.X)*96, dpi), Y: dip.Y + floorDiv((y-px.Y)*96, dpi)}
}

// pointToPixels converts a DIP to the first pixel in it, in the scale of
// the display it is on in DIPs, or the nearest, which pointToDIP gives
// back.
func (s dipSpace) pointToPixels(x, y int) platform.Point {
	i := s.dipDisplay(platform.Rect{X: x, Y: y, Width: 1, Height: 1})
	px, dip, dpi := s.origin(i)
	return platform.Point{X: px.X - floorDiv(-(x-dip.X)*dpi, 96), Y: px.Y - floorDiv(-(y-dip.Y)*dpi, 96)}
}

// floorDiv returns a / b rounded down, for b > 0.
func floorDiv(a, b int) int {
	q := a / b
	if a%b != 0 && a < 0 {
		q--
	}
	return q
}

// mostOn returns the first of byOverlap's order; -1 for no displays.
func mostOn(r platform.Rect, n int, bounds func(int) platform.Rect) int {
	if order := byOverlap(r, n, bounds); len(order) > 0 {
		return order[0]
	}
	return -1
}

// byOverlap returns the n displays from the one r overlaps the most to
// the one it overlaps the least, and those it does not overlap from the
// nearest to the farthest. An empty r is taken as its top-left pixel.
func byOverlap(r platform.Rect, n int, bounds func(int) platform.Rect) []int {
	r.Width, r.Height = max(r.Width, 1), max(r.Height, 1)
	type rank struct {
		i              int
		area, distance int64
	}
	ranks := make([]rank, n)
	for i := range n {
		b := bounds(i)
		w := min(r.X+r.Width, b.X+b.Width) - max(r.X, b.X)
		h := min(r.Y+r.Height, b.Y+b.Height) - max(r.Y, b.Y)
		k := rank{i: i}
		if w > 0 && h > 0 {
			k.area = int64(w) * int64(h)
		} else {
			dx, dy := int64(max(0, -w)), int64(max(0, -h))
			k.distance = dx*dx + dy*dy
		}
		ranks[i] = k
	}
	// Insertion sort: there are a few displays, and ties keep the
	// system's order.
	for i := 1; i < n; i++ {
		for j := i; j > 0; j-- {
			a, b := ranks[j-1], ranks[j]
			if a.area > b.area || a.area == b.area && a.distance <= b.distance {
				break
			}
			ranks[j-1], ranks[j] = b, a
		}
	}
	order := make([]int, n)
	for i, k := range ranks {
		order[i] = k.i
	}
	return order
}
