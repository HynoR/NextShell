package gputest

import (
	"math"

	"github.com/egoist/mygo/internal/scene"
)

// MaskedClipScene has clips masked by gradients, linear of two colors and
// of stops, radial and conic, and by an image (a path's coverage),
// with fills, a shadow and an image inside, a clip within a masked one,
// rounded or not, a masked clip turned, and one only moved, by whole
// pixels and between them, which renderers fold; all of them again,
// scaled, under the reference scene's size (Scene); and part of an image
// over a rounded clip.
func MaskedClipScene() *scene.Scene {
	scene.FoldTranslations() // as package ui does
	s := &scene.Scene{Width: 320, Height: 740, Clear: scene.Color{R: 246, G: 247, B: 249, A: 255}}
	add := func(ops ...scene.Op) { s.Ops = append(s.Ops, ops...) }
	r4 := func(v float32) [4]float32 { return [4]float32{v, v, v, v} }
	red, blue := scene.Color{R: 220, G: 40, B: 40, A: 255}, scene.Color{R: 40, G: 60, B: 220, A: 255}
	black, clear := scene.Color{A: 255}, scene.Color{}
	stops := func(st ...scene.Stop) [2]int32 {
		a := int32(len(s.Stops))
		s.Stops = append(s.Stops, st...)
		return [2]int32{a, int32(len(s.Stops))}
	}
	content := func(x, y float32, m scene.Affine) {
		add(scene.Op{Kind: scene.OpFill, Rect: scene.Rect{X: x, Y: y, W: 90, H: 80}, Color: red, Transform: m},
			scene.Op{Kind: scene.OpShadow, Rect: scene.Rect{X: x + 20, Y: y + 18, W: 40, H: 30}, Radii: r4(6), Blur: 8, Color: scene.Color{A: 160}, Transform: m},
			scene.Op{Kind: scene.OpFill, Rect: scene.Rect{X: x + 20, Y: y + 15, W: 50, H: 40}, Radii: r4(10), Color: blue,
				Border: scene.Uniform(2), BorderColor: scene.Color{R: 250, G: 250, B: 250, A: 255}, Transform: m})
	}
	// Two colors along a line, faded out to the right.
	add(scene.Op{Kind: scene.OpPushClip, Rect: scene.Rect{X: 10, Y: 10, W: 90, H: 80}, Paint: scene.PaintLinear,
		Color: black, Color2: clear, Gradient: [4]float32{20, 0, 90, 0}})
	content(10, 10, scene.Affine{})
	add(scene.Op{Kind: scene.OpPopClip})
	// Stops fading the top and the bottom, in a rounded clip, with a
	// clip inside, and a rounded one inside that.
	add(scene.Op{Kind: scene.OpPushClip, Rect: scene.Rect{X: 110, Y: 10, W: 90, H: 80}, Radii: r4(14), Paint: scene.PaintLinearStops,
		Gradient: [4]float32{0, 10, 0, 90}, Stops: stops(scene.Stop{Color: clear, At: 0}, scene.Stop{Color: black, At: 0.25},
			scene.Stop{Color: black, At: 0.75}, scene.Stop{Color: clear, At: 1})})
	add(scene.Op{Kind: scene.OpPushClip, Rect: scene.Rect{X: 115, Y: 5, W: 70, H: 95}})
	content(110, 10, scene.Affine{})
	add(scene.Op{Kind: scene.OpPushClip, Rect: scene.Rect{X: 150, Y: 40, W: 40, H: 40}, Radii: r4(12)})
	add(scene.Op{Kind: scene.OpFill, Rect: scene.Rect{X: 140, Y: 30, W: 60, H: 60}, Color: scene.Color{G: 160, B: 90, A: 255}})
	add(scene.Op{Kind: scene.OpPopClip}, scene.Op{Kind: scene.OpPopClip}, scene.Op{Kind: scene.OpPopClip})
	// Radial and conic.
	add(scene.Op{Kind: scene.OpPushClip, Rect: scene.Rect{X: 210, Y: 10, W: 90, H: 80}, Paint: scene.PaintRadial,
		Gradient: [4]float32{255, 50, 45, 0}, Stops: stops(scene.Stop{Color: black, At: 0.4}, scene.Stop{Color: clear, At: 1})})
	content(210, 10, scene.Affine{})
	add(scene.Op{Kind: scene.OpPopClip})
	add(scene.Op{Kind: scene.OpPushClip, Rect: scene.Rect{X: 10, Y: 100, W: 90, H: 80}, Paint: scene.PaintConic,
		Gradient: [4]float32{55, 140, 0.3, 0}, Stops: stops(scene.Stop{Color: clear, At: 0}, scene.Stop{Color: black, At: 1})})
	content(10, 100, scene.Affine{})
	add(scene.Op{Kind: scene.OpPopClip})
	// A disc's coverage, as a path's mask, with an image inside.
	const d = 64
	pix := make([]byte, 4*d*d)
	for y := range d {
		for x := range d {
			dx, dy := float64(x)+0.5-d/2, float64(y)+0.5-d/2
			pix[4*(y*d+x)+3] = byte(255 * clamp(d/2-1-math.Sqrt(dx*dx+dy*dy)))
		}
	}
	disc := scene.NewImageRGBA(d, d, pix)
	img := make([]byte, 4*8*8)
	for i := 0; i < len(img); i += 4 {
		img[i], img[i+1], img[i+2], img[i+3] = byte(i*4), 120, byte(255-i*4), 255
	}
	add(scene.Op{Kind: scene.OpPushClip, Rect: scene.Rect{X: 120, Y: 108, W: d, H: d}, Image: disc, Src: scene.Rect{W: d, H: d}})
	content(110, 100, scene.Affine{})
	add(scene.Op{Kind: scene.OpImage, Rect: scene.Rect{X: 160, Y: 140, W: 40, H: 40}, Image: scene.NewImageRGBA(8, 8, img), Src: scene.Rect{W: 8, H: 8}})
	add(scene.Op{Kind: scene.OpPopClip})
	// Turned.
	turn := scene.Translation(255, 140).Mul(scene.Rotation(12)).Mul(scene.Translation(-255, -140))
	add(scene.Op{Kind: scene.OpPushClip, Rect: scene.Rect{X: 215, Y: 105, W: 80, H: 70}, Radii: r4(10), Paint: scene.PaintLinear,
		Color: clear, Color2: black, Gradient: [4]float32{215, 105, 295, 175}, Transform: turn})
	content(210, 100, turn)
	add(scene.Op{Kind: scene.OpPopClip, Transform: turn})
	// Moved by whole pixels, and between them, the gradient and the image
	// with the clip.
	for i, m := range []scene.Affine{scene.Translation(3, 2), scene.Translation(-4.25, 1.75)} {
		x := 10 + 110*float32(i)
		add(scene.Op{Kind: scene.OpPushClip, Rect: scene.Rect{X: x, Y: 200, W: 90, H: 80}, Paint: scene.PaintLinearStops,
			Gradient: [4]float32{x, 0, x + 90, 0}, Stops: stops(scene.Stop{Color: clear, At: 0.1}, scene.Stop{Color: black, At: 0.6}), Transform: m})
		content(x, 200, m)
		add(scene.Op{Kind: scene.OpPopClip, Transform: m})
	}
	add(scene.Op{Kind: scene.OpPushClip, Rect: scene.Rect{X: 230, Y: 208, W: d, H: d}, Image: disc, Src: scene.Rect{W: d, H: d}, Transform: scene.Translation(2.5, -1.25)})
	content(220, 200, scene.Translation(2.5, -1.25))
	add(scene.Op{Kind: scene.OpPopClip, Transform: scene.Translation(2.5, -1.25)})
	// Scaled, the masks with the clips.
	n, grow := len(s.Ops), scene.Translation(0, 300).Mul(scene.Scaling(1.2, 1.4))
	for _, op := range s.Ops[:n] {
		op.Transform = grow.Mul(op.Transform)
		add(op)
	}
	// Part of an image's alpha over a rounded clip, as an element's
	// layers of masks under its ClipInset (ui's Masks): the disc's middle,
	// stretched.
	add(scene.Op{Kind: scene.OpPushClip, Rect: scene.Rect{X: 10, Y: 696, W: 90, H: 40}, Radii: r4(8), Image: disc, Src: scene.Rect{X: 8, Y: 16, W: 48, H: 32}})
	add(scene.Op{Kind: scene.OpFill, Rect: scene.Rect{X: 0, Y: 690, W: 110, H: 50}, Color: red})
	add(scene.Op{Kind: scene.OpPopClip})
	return s
}
