package ui

import (
	"encoding/binary"
	"hash/maphash"
	"math"
	"slices"
	"sync"

	"github.com/egoist/mygo/internal/raster"
	"github.com/egoist/mygo/internal/scene"
)

// Mask is a layer of an element's mask (Element.Masks), as one image of
// CSS's mask-image, with its mask-composite and its mask-origin and
// mask-clip.
type Mask struct {
	// Image is what the layer masks by: the alpha of a LinearGradient, a
	// RadialGradient or a ConicGradient across Box, whatever its colors.
	// A gradient of one color, as LinearGradient{From: black, To: black},
	// shows all of Box.
	Image MaskImage
	// Composite is how the layer goes with those after it in the list,
	// which are under it: MaskAdd, by default, shows what either shows.
	// The last layer's is not used.
	Composite MaskComposite
	// Box is the box the image lies across, nothing of the layer showing
	// outside it: the element's border box by default, or its padding box
	// or its content box.
	Box MaskBox
}

// MaskImage is what a layer of a mask masks by: a LinearGradient, a
// RadialGradient or a ConicGradient.
type MaskImage interface{ maskImage() }

func (LinearGradient) maskImage() {}
func (RadialGradient) maskImage() {}
func (ConicGradient) maskImage()  {}

// MaskComposite is how a layer of a mask goes with the layers under it,
// as CSS's mask-composite: each point shows as much as the result there.
type MaskComposite uint8

const (
	// MaskAdd shows the layer over those under it: what either shows.
	MaskAdd MaskComposite = iota
	// MaskSubtract shows the layer where those under it do not.
	MaskSubtract
	// MaskIntersect shows the layer where those under it show too.
	MaskIntersect
	// MaskExclude shows the layer and those under it where the other does
	// not: what one of them shows, but not both.
	MaskExclude
)

// MaskBox is the box a layer of a mask lies across (Mask.Box), as CSS's
// mask-origin and mask-clip.
type MaskBox uint8

const (
	// MaskBorderBox is the element's box, its border included.
	MaskBorderBox MaskBox = iota
	// MaskPaddingBox is the box inside its border.
	MaskPaddingBox
	// MaskContentBox is the box inside its border and padding.
	MaskContentBox
)

// Masks masks the element and what is inside it by layers of masks, the
// first on top, each composited with those after it as its Composite
// says, as CSS's mask-image with mask-composite does: each point shows as
// much as the result's alpha there, and nothing shows outside the box (or
// the inset of ClipInset). It replaces MaskLinearGradient and the others,
// which make one layer, and they replace it. A ring 1.5 DIPs wide, of a
// gradient, its middle cut out by a layer over the content box:
//
//	black := ui.RGB(0, 0, 0)
//	solid := ui.LinearGradient{From: black, To: black}
//	ui.Box(c).Size(120, 40).Radius(20).Padding(1.5).Gradient(pink, violet, 90).Masks(
//		ui.Mask{Image: solid, Box: ui.MaskContentBox, Composite: ui.MaskExclude},
//		ui.Mask{Image: solid},
//	)
//
// and a crescent, a disc less one beside it:
//
//	disc := func(x, y float32) ui.RadialGradient {
//		return ui.RadialGradient{Center: ui.Point{X: x, Y: y}, Radius: 40,
//			Stops: []ui.GradientStop{{black, 0.98}, {ui.Transparent, 1}}}
//	}
//	moon.Masks(ui.Mask{Image: disc(0.5, 0.5), Composite: ui.MaskSubtract}, ui.Mask{Image: disc(0.7, 0.35)})
//
// Layers that are not one gradient across the border box make a mask of
// their alpha, drawn once for each size the element paints at.
func (e *node) Masks(layers ...Mask) *node {
	k := e.clippings()
	k.mask, k.layers = maskNone, nil
	switch {
	case len(layers) == 0:
	case len(layers) == 1 && layers[0].Box == MaskBorderBox:
		// One gradient across the box masks as one does.
		switch g := layers[0].Image.(type) {
		case LinearGradient:
			k.mask, k.linear = maskLinear, g
		case RadialGradient:
			k.mask, k.radial = maskRadial, g
		case ConicGradient:
			k.mask, k.conic = maskConic, g
		}
	default:
		linkMaskLayers.Do(func() { pushMaskLayers = (*Painter).pushMaskLayers })
		k.mask, k.layers = maskLayers, slices.Clone(layers)
	}
	return e
}

// pushMaskLayers is Painter.pushMaskLayers once Masks makes layers, so
// that apps that never do do not link it.
var (
	pushMaskLayers func(p *Painter, e *node, r Rect, radius float32)
	linkMaskLayers sync.Once
)

// maskLayersMax is the largest width and height of the mask of layers,
// in pixels: larger elements are masked by one drawn smaller, stretched.
const maskLayersMax = 4096

// pushMaskLayers pushes the clip of e to r, its corners rounded by
// radius, within its box, masked by the alpha of its layers composited
// (Masks).
func (p *Painter) pushMaskLayers(e *node, r Rect, radius float32) {
	box := Rect{e.x, e.y, e.w, e.h}
	bp, rp := p.snap(box), p.snap(intersect(r, box))
	w, h := int(min(bp.W, maskLayersMax)), int(min(bp.H, maskLayersMax))
	if w <= 0 || h <= 0 || rp.W <= 0 || rp.H <= 0 {
		p.clip = Rect{}
		p.add(scene.Op{Kind: scene.OpPushClip})
		return
	}
	kx, ky := float32(w)/bp.W, float32(h)/bp.H
	// The layers' ops in the pixels of the mask, as the painter makes them
	// at that scale, into a scene of their own.
	s := p.rt.maskScene
	if s == nil {
		s = &scene.Scene{}
		p.rt.maskScene = s
	}
	*s = scene.Scene{Width: w, Height: h, Ops: s.Ops[:0], Stops: s.Stops[:0]}
	savedScene, savedScale := p.s, p.scale
	p.s, p.scale = s, p.scale*kx
	layers := e.clipping.layers
	for _, l := range layers {
		lb, radii := e.maskBox(l.Box)
		op := scene.Op{Kind: scene.OpFill, Opacity: 1, Rect: scene.Rect{
			X: (lb.X*savedScale - bp.X) * kx, Y: (lb.Y*savedScale - bp.Y) * ky,
			W: lb.W * savedScale * kx, H: lb.H * savedScale * ky}, Continuous: continuousCorners}
		for i, r := range radii {
			op.Radii[i] = r * savedScale * kx
		}
		switch g := l.Image.(type) {
		case LinearGradient:
			p.gradient(&op, g)
		case RadialGradient:
			p.radial(&op, g)
		case ConicGradient:
			p.conic(&op, g)
		default:
			op.Rect = scene.Rect{} // nothing
		}
		s.Ops = append(s.Ops, op)
	}
	p.s, p.scale = savedScene, savedScale
	key := maskLayersKey(s, layers)
	img := p.rt.keptMask(key)
	if img == nil {
		img = drawMaskLayers(s, layers)
		p.rt.keepMask(key, img)
	}
	// The part of the mask under the clip.
	src := scene.Rect{X: (rp.X - bp.X) * kx, Y: (rp.Y - bp.Y) * ky, W: rp.W * kx, H: rp.H * ky}
	p.clip = intersect(p.clip, transformRect(p.transform, intersect(r, box)))
	p.add(scene.Op{Kind: scene.OpPushClip, Rect: rp, Radii: p.radii([4]float32{radius, radius, radius, radius}),
		Continuous: continuousCorners, Image: img, Src: src})
}

// maskBox returns the box b of the element, and the radii of its corners,
// as CSS rounds the boxes inside a border and padding: each corner's
// radius less the wider of its two sides, as the inner edge of a border.
func (e *node) maskBox(b MaskBox) (Rect, [4]float32) {
	r := scene.Rect{X: e.x, Y: e.y, W: e.w, H: e.h}
	radii := scene.Corners(r, e.radius, false)
	if b >= MaskPaddingBox {
		r, radii = scene.InnerRadii(r, radii, e.border)
	}
	if b >= MaskContentBox {
		r, radii = scene.InnerRadii(r, radii, e.pad)
	}
	return Rect{r.X, r.Y, max(r.W, 0), max(r.H, 0)}, radii
}

// maskLayersKey returns the key of the mask of the ops of s, one for each
// of layers, by which keepMask keeps it.
func maskLayersKey(s *scene.Scene, layers []Mask) uint64 {
	var hs maphash.Hash
	hs.SetSeed(pathSeed)
	var buf [4]byte
	f := func(v float32) {
		binary.LittleEndian.PutUint32(buf[:], math.Float32bits(v))
		hs.Write(buf[:])
	}
	color := func(c scene.Color) { hs.Write([]byte{c.R, c.G, c.B, c.A}) }
	hs.WriteString("mask layers")
	f(float32(s.Width))
	f(float32(s.Height))
	for i, op := range s.Ops {
		hs.WriteByte(byte(layers[i].Composite))
		hs.WriteByte(byte(op.Paint))
		for _, v := range op.Radii {
			f(v)
		}
		f(op.Rect.X)
		f(op.Rect.Y)
		f(op.Rect.W)
		f(op.Rect.H)
		for _, v := range op.Gradient {
			f(v)
		}
		color(op.Color)
		color(op.Color2)
		for _, st := range s.Stops[op.Stops[0]:op.Stops[1]] {
			color(st.Color)
			f(st.At)
		}
	}
	return hs.Sum64()
}

// drawMaskLayers draws the ops of s, one for each of layers, each on its
// own, and returns their alphas composited from the last up as their
// Composite says, as an image's alpha.
func drawMaskLayers(s *scene.Scene, layers []Mask) *scene.Image {
	w, h := s.Width, s.Height
	n := w * h
	acc := make([]float32, n)
	dst := raster.NewImage(w, h)
	ops := s.Ops
	defer func() { s.Ops = ops }()
	for i := len(ops) - 1; i >= 0; i-- {
		clear(dst.Pix)
		s.Ops = ops[i : i+1]
		raster.Render(dst, s)
		bottom := i == len(ops)-1
		op := layers[i].Composite
		for j := range acc {
			src, d := float32(dst.Pix[4*j+3])/255, acc[j]
			switch {
			case bottom:
				d = src
			case op == MaskSubtract:
				d = src * (1 - d)
			case op == MaskIntersect:
				d = src * d
			case op == MaskExclude:
				d = src*(1-d) + d*(1-src)
			default:
				d = src + d*(1-src)
			}
			acc[j] = d
		}
	}
	pix := make([]byte, 4*n)
	for j, a := range acc {
		pix[4*j+3] = byte(max(0, min(a*255+0.5, 255)))
	}
	return scene.NewImageRGBA(w, h, pix)
}
