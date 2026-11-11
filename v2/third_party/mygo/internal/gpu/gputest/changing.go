package gputest

import (
	"math/rand/v2"

	"github.com/egoist/mygo/internal/scene"
)

// Changing makes random scenes and changes them a little at a time, as the
// frames of an app do, for the tests of renderers drawing only what
// changed: fills (with continuous corners too), shadows, glyphs, images,
// gradients, effects reading their backdrops, clips (one nested in
// another, transformed, now and then), transforms, and colors outside the
// sRGB gamut.
type Changing struct {
	rnd   *rand.Rand
	atlas *scene.Atlas
	img   *scene.Image
	// masks are rectangles of the atlas holding something.
	masks [][4]uint16
	// Clear is the color scenes start with.
	Clear scene.Color
}

// NewChanging returns a maker of scenes from seed.
func NewChanging(seed uint64) *Changing {
	m := &Changing{rnd: rand.New(rand.NewPCG(seed, 11)), atlas: scene.NewAtlas(1, 128, 128),
		Clear: scene.Color{R: 250, G: 250, B: 250, A: 255}}
	for range 6 {
		w, h := 4+m.rnd.IntN(12), 4+m.rnd.IntN(12)
		x, y, ok := m.atlas.Alloc(w, h)
		if !ok {
			break
		}
		m.atlas.Put(x, y, w, h, m.pixels(w*h), w)
		m.masks = append(m.masks, [4]uint16{uint16(x), uint16(y), uint16(w), uint16(h)})
	}
	m.img = scene.NewImageRGBA(8, 8, m.pixels(8*8*4))
	return m
}

func (m *Changing) pixels(n int) []byte {
	p := make([]byte, n)
	for i := range p {
		p[i] = byte(m.rnd.IntN(256))
	}
	return p
}

func (m *Changing) color() scene.Color {
	return scene.Color{R: uint8(m.rnd.IntN(256)), G: uint8(m.rnd.IntN(256)), B: uint8(m.rnd.IntN(256)), A: uint8(64 + m.rnd.IntN(192))}
}

func (m *Changing) rect() scene.Rect {
	return scene.Rect{X: m.rnd.Float32()*300 - 10, Y: m.rnd.Float32()*220 - 10, W: 2 + m.rnd.Float32()*90, H: 2 + m.rnd.Float32()*70}
}

// transform returns no transform mostly, else a translation, a rotation or
// a scaling.
func (m *Changing) transform() scene.Affine {
	switch m.rnd.IntN(8) {
	case 0:
		return scene.Translation(m.rnd.Float32()*20-10, m.rnd.Float32()*20-10)
	case 1:
		return scene.Translation(150, 110).Mul(scene.Rotation(m.rnd.Float32()*40 - 20)).Mul(scene.Translation(-150, -110))
	case 2:
		return scene.Scaling(0.8+m.rnd.Float32()*0.4, 0.8+m.rnd.Float32()*0.4)
	}
	return scene.Affine{}
}

// wide gives op colors outside the sRGB gamut, now and then.
func (m *Changing) wide(s *scene.Scene, op *scene.Op) {
	if m.rnd.IntN(4) != 0 {
		return
	}
	s.Wide = append(s.Wide, scene.WideColors{Color: [4]float32{-0.2 + m.rnd.Float32()*0.1, 1.1, -0.1, 1}, Set: scene.WideColor})
	op.Wide = uint16(len(s.Wide))
}

// op returns a random operation that draws.
func (m *Changing) op(s *scene.Scene) scene.Op {
	var op scene.Op
	switch m.rnd.IntN(8) {
	case 0, 1:
		r := m.rnd.Float32() * 12
		op = scene.Op{Kind: scene.OpFill, Rect: m.rect(), Radii: scene.Uniform(r), Color: m.color(),
			Border: scene.Uniform(float32(m.rnd.IntN(3))), BorderColor: m.color(), Continuous: m.rnd.IntN(3) == 0}
		if m.rnd.IntN(4) == 0 {
			op.Paint, op.Color2 = scene.PaintLinear, m.color()
			op.Gradient = [4]float32{op.Rect.X, op.Rect.Y, op.Rect.X + op.Rect.W, op.Rect.Y + op.Rect.H}
		}
		m.wide(s, &op)
	case 2:
		op = scene.Op{Kind: scene.OpShadow, Rect: m.rect(), Radii: scene.Uniform(4), Color: m.color(), Blur: m.rnd.Float32() * 16}
	case 3, 4:
		start := int32(len(s.Glyphs))
		for range 1 + m.rnd.IntN(3) {
			k := m.masks[m.rnd.IntN(len(m.masks))]
			s.Glyphs = append(s.Glyphs, scene.Glyph{X: float32(m.rnd.IntN(300)), Y: float32(m.rnd.IntN(220)),
				W: float32(k[2]), H: float32(k[3]), U: k[0], V: k[1], UW: k[2], VH: k[3], Color: m.color()})
		}
		op = scene.Op{Kind: scene.OpGlyphs, Start: start, End: int32(len(s.Glyphs))}
	case 5:
		op = scene.Op{Kind: scene.OpImage, Rect: m.rect(), Image: m.img, Src: scene.Rect{W: 8, H: 8}}
	default:
		r := m.rnd.Float32() * 20
		fx := scene.EffectOp{Effect: LensEffect, Blur: float32(m.rnd.IntN(4)) * 3}
		if m.rnd.IntN(3) == 0 {
			fx.Effect = TintEffect
		}
		fx.Params[0] = [4]float32{m.rnd.Float32() * 4}
		c := m.color()
		fx.Params[1] = [4]float32{float32(c.R) / 255, float32(c.G) / 255, float32(c.B) / 255, float32(c.A) / 255}
		s.Effects = append(s.Effects, fx)
		return scene.Op{Kind: scene.OpEffect, Rect: m.rect(), Radii: scene.Uniform(r), Start: int32(len(s.Effects) - 1)}
	}
	op.Transform = m.transform()
	return op
}

// Scene returns a scene of 320×240 pixels.
func (m *Changing) Scene() *scene.Scene {
	s := &scene.Scene{Width: 320, Height: 240, Clear: m.Clear, MaskAtlas: m.atlas,
		Text: scene.TextParams{GammaRatios: scene.GammaRatios(1.8), Contrast: 1}}
	for range 12 {
		s.Ops = append(s.Ops, m.op(s))
	}
	s.Ops = append(s.Ops, scene.Op{Kind: scene.OpPushClip, Rect: m.rect(), Radii: scene.Uniform(6)})
	for range 6 {
		s.Ops = append(s.Ops, m.op(s))
	}
	s.Ops = append(s.Ops, scene.Op{Kind: scene.OpPopClip})
	for range 6 {
		s.Ops = append(s.Ops, m.op(s))
	}
	return s
}

// Change changes s a little, as a frame of an app would, and says how.
func (m *Changing) Change(s *scene.Scene) string {
	// Drawing operations, which can change freely, unlike clips.
	var drawing []int
	for i, op := range s.Ops {
		if op.Kind != scene.OpPushClip && op.Kind != scene.OpPopClip {
			drawing = append(drawing, i)
		}
	}
	i := drawing[m.rnd.IntN(len(drawing))]
	op := &s.Ops[i]
	switch m.rnd.IntN(10) {
	case 0:
		op.Color = m.color()
		return "color"
	case 1:
		if op.Kind == scene.OpGlyphs {
			s.Glyphs[op.Start].X += 3
			return "glyph moved"
		}
		op.Rect.X += m.rnd.Float32()*20 - 10
		return "moved"
	case 2:
		s.Ops = append(s.Ops[:i+1], s.Ops[i:]...)
		s.Ops[i] = m.op(s)
		return "inserted"
	case 3:
		if len(drawing) > 4 {
			s.Ops = append(s.Ops[:i], s.Ops[i+1:]...)
			return "deleted"
		}
		return "nothing"
	case 4:
		for j := range s.Ops {
			if s.Ops[j].Kind == scene.OpPushClip {
				s.Ops[j].Rect = m.rect()
			}
		}
		return "clip moved"
	case 5:
		// New pixels where glyphs are, as a transient mask of the next
		// frame takes the place of the last one's.
		k := m.masks[m.rnd.IntN(len(m.masks))]
		m.atlas.Put(int(k[0]), int(k[1]), int(k[2]), int(k[3]), m.pixels(int(k[2])*int(k[3])), int(k[2]))
		return "atlas pixels"
	case 6:
		copy(m.img.Pix, m.pixels(len(m.img.Pix)))
		m.img.Changed()
		return "image pixels"
	case 7:
		op.Transform = m.transform()
		return "transform"
	case 8:
		if op.Wide != 0 {
			s.Wide[op.Wide-1].Color[0] = -0.3 + m.rnd.Float32()*0.2
			return "wide color"
		}
		if op.Kind == scene.OpEffect {
			s.Effects[op.Start].Params[0][0] = m.rnd.Float32() * 4
			return "effect"
		}
		return "nothing"
	case 9:
		return m.nest(s)
	default:
		return "nothing"
	}
}

// nest takes the clip nested in the scene's, moved by a translation, out
// of s, or puts one in, with a fill: the renderers then cut what the
// outer clip holds with a mask, as it holds a transformed clip, rather
// than in the shader, which draws its edge a unit in the last place
// apart, so all it holds changes.
func (m *Changing) nest(s *scene.Scene) string {
	for k := range s.Ops {
		if s.Ops[k].Kind != scene.OpPushClip || !s.Ops[k].Transform.Set {
			continue
		}
		end := k + 1
		for end < len(s.Ops) && s.Ops[end].Kind != scene.OpPopClip {
			end++
		}
		s.Ops = append(s.Ops[:k], s.Ops[min(end+1, len(s.Ops)):]...)
		return "nested clip out"
	}
	// Before the outer clip's end.
	at := -1
	for k, op := range s.Ops {
		if op.Kind == scene.OpPopClip {
			at = k
			break
		}
	}
	if at < 0 {
		return "nothing"
	}
	inner := scene.Op{Kind: scene.OpPushClip, Rect: scene.Rect{W: 60, H: 40}, Radii: scene.Uniform(4),
		Transform: scene.Translation(m.rnd.Float32()*240, m.rnd.Float32()*180)}
	fill := scene.Op{Kind: scene.OpFill, Rect: m.rect(), Color: m.color()}
	s.Ops = append(s.Ops[:at], append([]scene.Op{inner, fill, {Kind: scene.OpPopClip}}, s.Ops[at:]...)...)
	return "nested clip in"
}
