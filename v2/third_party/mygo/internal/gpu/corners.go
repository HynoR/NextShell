package gpu

import (
	"math"

	"github.com/egoist/mygo/internal/scene"
)

// Continuous corners (negative radii) cost Metal's shader far more than
// circular ones: a fill's coverage near them is their value and its two
// derivatives, and a blurred shadow finds, for each of its four rows of
// samples, where the curve is by regula falsi. A batch holding any of them
// draws with a pipeline holding that code, which every fragment of the batch
// pays for, curved or not. Yet the curves shape only the pixels near the
// corners: elsewhere the shader computes exactly what it computes for a
// rectangle with square corners, by the same code (see cornerLines). So a
// large shape with continuous corners is drawn as a nine-patch, as
// compositors draw rounded rectangles: its corners as the shape, each
// piece with a scissor rectangle of its own, and the rest, a cross, as its
// twin with square corners, in batches of a pipeline without the curves.
// The pixels are identical.

// contExtent is how far from its corner the curve of a continuous corner of
// radius 1 leaves the edges (the shaders' contExtent).
const contExtent = 1.528665

// pieceCost is what a piece of a nine-patch costs, a draw call of its own
// (no piece shares a batch: each has a scissor rectangle of its own), in
// pixels of the twin: on an M5 a draw call costs about 1.2 µs of the CPU
// and the GPU, and a pixel drawn by the twin rather than the shape about
// 0.08 ns less, by the overhead of the curves' code in a pipeline. A pixel
// of a shadow in the rows its curve shapes, between its corners, saves
// curvedWeight times more: the curve is found there for each sample, by
// regula falsi (0.5 ns a pixel).
const (
	pieceCost    = 16384
	curvedWeight = 6
)

// SplitCorners makes the builder draw large shapes with continuous corners
// as nine-patches (see addCorners), for a renderer whose shader draws them
// (Metal's). Only through it do apps link that code, which the other
// renderers' apps never run.
func (b *Builder) SplitCorners() { b.cutCorners = (*Builder).addCorners }

// addCut appends in, a fill, a shadow or an image, within the current
// clip's or split's scissor rectangle: as a nine-patch where the builder
// splits corners (addCorners), else whole.
func (b *Builder) addCut(in Instance, image uintptr, m scene.Affine) {
	if b.cutCorners != nil && !b.NoCornerSplit {
		b.cutCorners(b, in, image, m)
		return
	}
	b.add(in, image, m)
}

// cornerLines returns where the pixels lie that the continuous corners
// of radii (positive) of the rectangle r cannot reach: the columns from
// Left to Right and the rows from Top to Bottom, each as far in, from the
// sides, as the curves reach (contExtent times the radius) and, by reachX
// and reachY, beyond. A pixel in neither is in one of the corners' boxes,
// which reach out of the rectangle to the frame's edges. The shaders compute
// a corner's curve only at a pixel within its box (contValue tests u and v
// against e), and anywhere else the area of the pixel in the rectangle, as
// for square corners.
func cornerLines(r scene.Rect, radii [4]float32, reachX, reachY float32) Scissor {
	e := func(i, j int) float32 { return contExtent*max(abs32(radii[i]), abs32(radii[j])) + 1 }
	return Scissor{
		Left:   int32(math.Ceil(float64(r.X + e(0, 3) + reachX))),
		Top:    int32(math.Ceil(float64(r.Y + e(0, 1) + reachY))),
		Right:  int32(math.Floor(float64(r.X + r.W - e(1, 2) - reachX))),
		Bottom: int32(math.Floor(float64(r.Y + r.H - e(2, 3) - reachY))),
	}
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func continuous(r [4]float32) bool { return r[0] < 0 || r[1] < 0 || r[2] < 0 || r[3] < 0 }

// cornersOf returns, for a fill, an image or a shadow with continuous
// corners, not transformed, where it and its twin with square corners (no
// radii, on its border's inner edge or its caster neither) draw the same,
// as the lines that bound those pixels (see cornerLines), the pixels its
// quad covers, as the vertex shader grows it, and how much more a pixel
// of the twin between the corners saves than one between the bands of
// rows across them; ok is false for any other instance.
//
// A fill's twin computes its border's inner edge the same way too, whose
// curves lie within the outer ones' boxes: each of its corners is a
// border's width or more in from the outer one, and its radius smaller by
// the larger of the two widths (scene.InnerRadii).
//
// A shadow's blur, of standard deviation sigma, takes all its corners as
// the largest (boxShadow), and the curve shapes the rows of its samples
// that lie within the curve's reach of the top or the bottom, the samples
// reaching 3 sigma from the pixel. At a pixel whose samples all lie outside
// those rows the shader finds the sides straight, as for square corners;
// at one in those rows but 9 sigma or more from where the curve can put the
// sides, the blurred edges are exactly 0 and 1 (erf2 rounds to ±1 from about
// 7.7 sigma), wherever the curve puts them. The box casting it, cut out of
// the shadow, is a fill's coverage.
func cornersOf(in *Instance, m scene.Affine) (free, quad Scissor, weight int64, ok bool) {
	shadow := in.Params[0] == 1
	if m.Set || !continuous(in.Radii) && !(shadow && continuous(in.Inner)) {
		return Scissor{}, Scissor{}, 0, false
	}
	r := scene.Rect{X: in.Rect[0], Y: in.Rect[1], W: in.Rect[2], H: in.Rect[3]}
	grow := func(e float32) Scissor {
		return Scissor{int32(math.Floor(float64(r.X - e))), int32(math.Floor(float64(r.Y - e))),
			int32(math.Ceil(float64(r.X + r.W + e))), int32(math.Ceil(float64(r.Y + r.H + e)))}
	}
	if !shadow {
		return cornerLines(r, in.Radii, 0, 0), grow(1), 1, true
	}
	sigma := in.Params[2]
	corner := max(abs32(in.Radii[0]), abs32(in.Radii[1]), abs32(in.Radii[2]), abs32(in.Radii[3]))
	free = cornerLines(r, [4]float32{corner, corner, corner, corner}, 9*sigma, 3*sigma)
	if in.UV[2] > 0 && in.UV[3] > 0 {
		free = free.intersect(cornerLines(scene.Rect{X: in.UV[0], Y: in.UV[1], W: in.UV[2], H: in.UV[3]}, in.Inner, 0, 0))
	}
	return free, grow(3*sigma + 1), curvedWeight, true
}

// addCorners appends in as a nine-patch within the batches' scissor
// rectangle, when the pixels its twin draws save more than the draw calls
// of the pieces cost (pieceCost): its corners as it is and the rest as its
// twin. The band of rows between the corners is all the twin's; each band
// of rows across them is the shape's, or, where enough lies between the
// corners, the shape's at its ends and the twin's between. Small shapes,
// whose pixels are near their corners, stay whole, and join their
// neighbors' batches (see joinLast).
func (b *Builder) addCorners(in Instance, image uintptr, m scene.Affine) {
	f, quad, weight, ok := cornersOf(&in, m)
	if !ok {
		b.add(in, image, m)
		return
	}
	cur := &b.stack[len(b.stack)-1]
	region := b.batchScissor(cur).intersect(quad)
	if f.Top > f.Bottom {
		// Every row is near a corner: either band can hold those between.
		f.Top = f.Bottom
	}
	top := Scissor{region.Left, region.Top, region.Right, f.Top}.intersect(region)
	mid := Scissor{region.Left, f.Top, region.Right, f.Bottom}.intersect(region)
	bottom := Scissor{region.Left, f.Bottom, region.Right, region.Bottom}.intersect(region)
	var pieces [7]Scissor // the shape's, then the twin's
	var twin [7]bool
	n := 0
	piece := func(s Scissor, t bool) {
		if !s.Empty() {
			pieces[n], twin[n] = s, t
			n++
		}
	}
	saved := mid.area()
	var between [2]Scissor
	for i, band := range [2]Scissor{top, bottom} {
		c := Scissor{f.Left, band.Top, f.Right, band.Bottom}.intersect(band)
		left := Scissor{band.Left, band.Top, c.Left, band.Bottom}
		right := Scissor{c.Right, band.Top, band.Right, band.Bottom}
		// Splitting the band adds its ends' draw calls.
		if c.Empty() || c.area()*weight <= pieceCost*int64(min(1, left.area())+min(1, right.area())) {
			piece(band, false)
			continue
		}
		piece(left, false)
		piece(right, false)
		between[i] = c
		saved += c.area() * weight
	}
	piece(mid, true)
	piece(between[0], true)
	piece(between[1], true)
	if n == 0 || saved <= pieceCost*int64(n-1) {
		b.add(in, image, m)
		return
	}
	sq := in
	sq.Radii, sq.Inner = [4]float32{}, [4]float32{}
	split, splitScissor := b.split, b.splitScissor
	defer b.scissorBatches(splitScissor, split)
	for i, s := range pieces[:n] {
		b.scissorBatches(s, true)
		if twin[i] {
			b.add(sq, image, m)
		} else {
			b.add(in, image, m)
		}
	}
}
