package gputest

import (
	"fmt"
	"image"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/internal/raster"
	"github.com/egoist/mygo/internal/scene"
)

// BorderedCardScene returns the 756×640 frame of #531, a window of 378×320
// DIPs at scale 2: an opaque backdrop, and on it a card of an opaque white
// face with a border of 1 DIP (2 pixels) and corners of 12 DIPs (24
// pixels), continuous (as macOS draws them) or not, on whole pixels, large
// enough for the builder to draw it as an opaque core with pieces around it;
// and a button with the same border above it, too small for that. Their
// straight edges cover whole pixels, by 1 or 0, so a renderer draws them
// exactly as the CPU renderer does (see CompareStraightEdges).
func BorderedCardScene(continuous bool) *scene.Scene {
	backdrop := scene.Color{R: 0xf4, G: 0xf4, B: 0xf6, A: 255}
	face := scene.Color{R: 255, G: 255, B: 255, A: 255}
	line := scene.Color{R: 0xe3, G: 0xe3, B: 0xe7, A: 255}
	s := &scene.Scene{Width: 756, Height: 640}
	s.Ops = append(s.Ops,
		scene.Op{Kind: scene.OpFill, Rect: scene.Rect{W: 756, H: 640}, Color: backdrop},
		scene.Op{Kind: scene.OpFill, Rect: scene.Rect{X: 26, Y: 90, W: 730, H: 484}, Radii: [4]float32{24, 24, 24, 24}, Continuous: continuous,
			Color: face, Border: scene.Uniform(2), BorderColor: line},
		scene.Op{Kind: scene.OpFill, Rect: scene.Rect{X: 100, Y: 16, W: 200, H: 56}, Radii: [4]float32{16, 16, 16, 16}, Continuous: continuous,
			Color: face, Border: scene.Uniform(2), BorderColor: line})
	return s
}

// CompareStraightEdges compares pixels a renderer drew, as Compare does,
// and requires more of those away from the corners of the scene's rounded
// fills, along their straight edges and inside them, where coverage is
// exactly 1 or 0 for shapes on whole pixels: each channel within 2 levels
// of the CPU renderer's, at every pixel. It reports the rows (or columns)
// that differ, so that a stray line along an edge shows where it is.
func CompareStraightEdges(t testing.TB, name string, got []byte, stride int, s *scene.Scene) {
	t.Helper()
	Compare(t, name, got, stride, s)
	want := raster.NewImage(s.Width, s.Height)
	raster.Render(want, s)
	// The corners' boxes, as far as their curves reach (continuous ones 1.53
	// times their radius) and the antialiasing a pixel beyond.
	var corners []image.Rectangle
	for _, op := range s.Ops {
		if op.Kind != scene.OpFill || op.Radii == [4]float32{} {
			continue
		}
		r := max(op.Radii[0], op.Radii[1], op.Radii[2], op.Radii[3])
		e := int(r*1.6) + 3
		x0, y0 := int(op.Rect.X)-2, int(op.Rect.Y)-2
		x1, y1 := int(op.Rect.X+op.Rect.W)+2, int(op.Rect.Y+op.Rect.H)+2
		corners = append(corners, image.Rect(x0, y0, x0+e, y0+e), image.Rect(x1-e, y0, x1, y0+e),
			image.Rect(x1-e, y1-e, x1, y1), image.Rect(x0, y1-e, x0+e, y1))
	}
	off := 0
	rows := map[int]int{}
	var first []string
	for y := range s.Height {
		for x := range s.Width {
			p := image.Pt(x, y)
			loose := false
			for _, c := range corners {
				if p.In(c) {
					loose = true
					break
				}
			}
			if loose {
				continue
			}
			g := got[y*stride+4*x:][:4]
			w := want.Pix[y*want.Stride+4*x:][:4]
			d := 0
			for c := range 4 {
				d = max(d, absInt(int(g[c])-int(w[c])))
			}
			if d > 2 {
				off++
				rows[y]++
				if len(first) < 8 {
					first = append(first, fmt.Sprintf("%v: BGRA %x, want %x", p, g, w))
				}
			}
		}
	}
	if off > 0 {
		ys := slices.Sorted(maps.Keys(rows))
		var where []string
		for _, y := range ys[:min(len(ys), 16)] {
			where = append(where, fmt.Sprintf("row %d: %d", y, rows[y]))
		}
		t.Errorf("%s: %d pixels away from the corners differ from the CPU renderer's by more than 2, in %d rows (%s); first %s",
			name, off, len(rows), strings.Join(where, ", "), strings.Join(first, "; "))
	}
}
