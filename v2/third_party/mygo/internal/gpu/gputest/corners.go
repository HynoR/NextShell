package gputest

import "github.com/egoist/mygo/internal/scene"

// CornerClipScene has clips that reach into the rounded corners of a clip
// around them, whose corners must cut what they hold (#1418), at the size
// of the reference scene (Scene): first a card as package ui draws it at
// scale 2, rounded and clipped, holding a column that clips too and fills
// the card's bottom; then a card holding a pane with a clip nested in it
// at its bottom, a rounded clip in its corner, and one in its middle, away
// from the corners.
func CornerClipScene() *scene.Scene {
	s := &scene.Scene{Width: 320, Height: 740, Clear: scene.Color{R: 246, G: 247, B: 249, A: 255}}
	add := func(ops ...scene.Op) { s.Ops = append(s.Ops, ops...) }
	r4 := func(v float32) [4]float32 { return [4]float32{v, v, v, v} }
	white := scene.Color{R: 255, G: 255, B: 255, A: 255}
	pop := scene.Op{Kind: scene.OpPopClip}

	card := scene.Rect{X: 10, Y: 20, W: 300, H: 240}
	add(scene.Op{Kind: scene.OpFill, Rect: card, Radii: r4(20), Color: white},
		scene.Op{Kind: scene.OpPushClip, Rect: scene.Rect{X: 12, Y: 22, W: 296, H: 236}, Radii: r4(18)},
		scene.Op{Kind: scene.OpPushClip, Rect: scene.Rect{X: 12, Y: 102, W: 296, H: 156}},
		scene.Op{Kind: scene.OpFill, Rect: scene.Rect{X: 12, Y: 102, W: 296, H: 156}, Color: scene.Color{G: 122, B: 255, A: 255}},
		pop, pop,
		scene.Op{Kind: scene.OpFill, Rect: card, Radii: r4(20), Border: scene.Uniform(2), BorderColor: scene.Color{R: 180, G: 180, B: 180, A: 255}})

	card = scene.Rect{X: 10, Y: 300, W: 300, H: 280}
	add(scene.Op{Kind: scene.OpFill, Rect: card, Radii: r4(24), Color: white},
		scene.Op{Kind: scene.OpPushClip, Rect: card, Radii: r4(24)},
		scene.Op{Kind: scene.OpPushClip, Rect: scene.Rect{X: 10, Y: 300, W: 110, H: 280}},
		scene.Op{Kind: scene.OpFill, Rect: scene.Rect{X: 10, Y: 300, W: 110, H: 280}, Color: scene.Color{R: 210, G: 214, B: 224, A: 255}},
		scene.Op{Kind: scene.OpPushClip, Rect: scene.Rect{X: 10, Y: 500, W: 110, H: 80}},
		scene.Op{Kind: scene.OpFill, Rect: scene.Rect{X: 0, Y: 490, W: 130, H: 100}, Color: scene.Color{R: 220, G: 40, B: 40, A: 255}},
		pop, pop,
		scene.Op{Kind: scene.OpPushClip, Rect: scene.Rect{X: 230, Y: 420, W: 80, H: 160}, Radii: r4(10)},
		scene.Op{Kind: scene.OpFill, Rect: scene.Rect{X: 220, Y: 410, W: 100, H: 180}, Color: scene.Color{R: 30, G: 160, B: 90, A: 255}},
		pop,
		scene.Op{Kind: scene.OpPushClip, Rect: scene.Rect{X: 140, Y: 340, W: 80, H: 160}},
		scene.Op{Kind: scene.OpFill, Rect: scene.Rect{X: 130, Y: 330, W: 100, H: 180}, Color: scene.Color{R: 250, G: 204, B: 21, A: 255}},
		pop, pop)
	return s
}
