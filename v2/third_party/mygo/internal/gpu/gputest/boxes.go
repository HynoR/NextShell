package gputest

import "github.com/egoist/mygo/internal/scene"

// BoxesScene returns a w×h frame of n rounded boxes of 24 pixels, each
// casting a shadow, in rows of 30, as an animation of boxes moving draws
// them: under translations (Element.Translate) when translated, else at
// the same places without transforms. The places fall between pixels.
func BoxesScene(w, h, n int, translated bool) *scene.Scene {
	scene.FoldTranslations() // as package ui does
	s := &scene.Scene{Width: w, Height: h, Clear: scene.Color{R: 246, G: 247, B: 249, A: 255}}
	for i := range n {
		x, y := 8+float32(i%30)*40+float32(i%7)*0.25, 8+float32(i/30)*38+float32(i%5)*0.2
		m := scene.Translation(x, y)
		box := scene.Rect{W: 24, H: 24}
		if !translated {
			m, box.X, box.Y = scene.Affine{}, x, y
		}
		cast := box
		cast.Y += 2
		c := scene.Color{R: uint8(40 + i*7%180), G: uint8(80 + i*13%150), B: uint8(120 + i*3%120), A: 255}
		s.Ops = append(s.Ops,
			scene.Op{Kind: scene.OpShadow, Rect: cast, Radii: scene.Uniform(6), Color: scene.Color{A: 70}, Blur: 8, Transform: m},
			scene.Op{Kind: scene.OpFill, Rect: box, Radii: scene.Uniform(6), Color: c, Transform: m})
	}
	return s
}

// RowsScene returns a w×h frame of a list of n rows, as a window of a list
// draws them: each a rounded background, a label of glyphs, a rounded
// badge and a second label, so that plain fills and glyphs alternate.
func RowsScene(w, h, n int) *scene.Scene {
	s := &scene.Scene{Width: w, Height: h, Clear: scene.Color{R: 250, G: 250, B: 250, A: 255}}
	atlas, text, glyphs := glyphContent()
	s.MaskAtlas, s.Text = atlas, text
	run := func(x, y float32, k int, c scene.Color) {
		start := len(s.Glyphs)
		for _, g := range glyphs[:k] {
			g.X, g.Y, g.Color = g.X-40+x, y, c
			s.Glyphs = append(s.Glyphs, g)
		}
		s.Ops = append(s.Ops, scene.Op{Kind: scene.OpGlyphs, Start: int32(start), End: int32(len(s.Glyphs))})
	}
	rowH := float32(h-16) / float32(n)
	for i := range n {
		y := 8 + float32(i)*rowH
		bg := scene.Color{R: 236, G: 238, B: 242, A: 255}
		if i%2 == 1 {
			bg = scene.Color{R: 244, G: 245, B: 247, A: 255}
		}
		s.Ops = append(s.Ops, scene.Op{Kind: scene.OpFill, Rect: scene.Rect{X: 8, Y: y, W: float32(w) - 16, H: rowH - 2}, Radii: scene.Uniform(4), Color: bg})
		run(20, y+1.5, 8+i%5, scene.Color{R: 30, G: 30, B: 34, A: 255})
		s.Ops = append(s.Ops, scene.Op{Kind: scene.OpFill, Rect: scene.Rect{X: 260, Y: y + 2, W: 40, H: rowH - 6}, Radii: scene.Uniform(6),
			Color: scene.Color{R: uint8(60 + i*29%160), G: 120, B: 220, A: 255}})
		run(320, y+1.5, 4+i%7, scene.Color{R: 110, G: 110, B: 120, A: 255})
	}
	return s
}

// PanelsScene returns a w×h frame of n cards in rows of 8, as a page of an
// app draws them: each a shadowed white card, its content in a rounded
// clip, an icon (one of four images), a label of glyphs and a colored
// badge. Clips and images change from card to card, so the frame takes
// several batches a card, as pages of real windows do.
func PanelsScene(w, h, n int) *scene.Scene {
	s := &scene.Scene{Width: w, Height: h, Clear: scene.Color{R: 246, G: 247, B: 249, A: 255}}
	atlas, text, glyphs := glyphContent()
	s.MaskAtlas, s.Text = atlas, text
	var icons [4]*scene.Image
	for k := range icons {
		pix := make([]byte, 16*16*4)
		for i := 0; i < len(pix); i += 4 {
			pix[i], pix[i+1], pix[i+2], pix[i+3] = byte(60*k), byte(i/4%16*16), 200, 255
		}
		icons[k] = scene.NewImageRGBA(16, 16, pix)
	}
	cw, ch := (float32(w)-16)/8, float32(64)
	for i := range n {
		x, y := 8+float32(i%8)*cw, 8+float32(i/8)*(ch+8)
		card := scene.Rect{X: x + 4, Y: y, W: cw - 8, H: ch}
		cast := card
		cast.Y += 2
		s.Ops = append(s.Ops,
			scene.Op{Kind: scene.OpShadow, Rect: cast, Radii: scene.Uniform(8), Color: scene.Color{A: 40}, Blur: 6},
			scene.Op{Kind: scene.OpFill, Rect: card, Radii: scene.Uniform(8), Color: scene.Color{R: 255, G: 255, B: 255, A: 255},
				Border: scene.Uniform(1), BorderColor: scene.Color{R: 220, G: 222, B: 228, A: 255}},
			scene.Op{Kind: scene.OpPushClip, Rect: scene.Rect{X: card.X + 1, Y: card.Y + 1, W: card.W - 2, H: card.H - 2}, Radii: scene.Uniform(7)},
			scene.Op{Kind: scene.OpImage, Rect: scene.Rect{X: card.X + 10, Y: card.Y + 10, W: 20, H: 20}, Radii: scene.Uniform(4),
				Image: icons[i%len(icons)], Src: scene.Rect{W: 16, H: 16}})
		start := len(s.Glyphs)
		for _, g := range glyphs[:6+i%6] {
			g.X, g.Y, g.Color = g.X-40+card.X+40, card.Y+12, scene.Color{R: 30, G: 30, B: 34, A: 255}
			s.Glyphs = append(s.Glyphs, g)
		}
		s.Ops = append(s.Ops,
			scene.Op{Kind: scene.OpGlyphs, Start: int32(start), End: int32(len(s.Glyphs))},
			scene.Op{Kind: scene.OpFill, Rect: scene.Rect{X: card.X + 10, Y: card.Y + 40, W: 36, H: 14}, Radii: scene.Uniform(7),
				Color: scene.Color{R: uint8(60 + i*29%160), G: 120, B: 220, A: 255}},
			scene.Op{Kind: scene.OpPopClip})
	}
	return s
}
