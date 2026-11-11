package ui

import (
	"encoding/binary"
	"hash/maphash"
	"math"

	"github.com/egoist/mygo/internal/scene"
)

// clipping is how an element clips and masks what it paints, itself and
// what is inside it (ClipInset, MaskLinearGradient and the others, and
// the layers of Masks), made the first time.
type clipping struct {
	// inset are the insets of the clip from the box, top, right, bottom
	// and left, in DIPs or, with percent, in percent of the box's height
	// and width; radius rounds its corners.
	inset    [4]float32
	percent  bool
	radius   float32
	insetSet bool
	mask     maskKind
	linear   LinearGradient
	radial   RadialGradient
	conic    ConicGradient
	layers   []Mask
}

type maskKind uint8

const (
	maskNone maskKind = iota
	maskLinear
	maskRadial
	maskConic
	maskLayers
)

// clippings returns the element's clipping, made the first time.
func (e *node) clippings() *clipping {
	if e.clipping == nil {
		e.clipping = &clipping{}
	}
	return e.clipping
}

// ClipInset clips the element and what is inside it, its background,
// shadows, border and children, to its box inset by top, right, bottom and
// left DIPs, with corners rounded by radius, as CSS's clip-path: inset()
// does. A negative inset reaches beyond the box on that side, as a clip
// along one axis that leaves a shadow whole along the other. The insets
// may change every frame, as values an element animates do:
//
//	hidden := float32(100)
//	if app.open {
//		hidden = 0
//	}
//	card.ClipInsetPercent(0, card.AnimateTo("wipe", hidden, ui.Smooth), 0, 0, 0)
//
// What the clip hides takes no clicks.
func (e *node) ClipInset(top, right, bottom, left, radius float32) *node {
	k := e.clippings()
	k.inset, k.percent, k.radius, k.insetSet = [4]float32{top, right, bottom, left}, false, max(radius, 0), true
	return e
}

// ClipInsetPercent is ClipInset with insets in percent of the box: top
// and bottom of its height, right and left of its width, as CSS's inset()
// takes percentages. An inset going from 100 to 0 wipes the element in
// from the other side.
func (e *node) ClipInsetPercent(top, right, bottom, left, radius float32) *node {
	k := e.clippings()
	k.inset, k.percent, k.radius, k.insetSet = [4]float32{top, right, bottom, left}, true, max(radius, 0), true
	return e
}

// MaskLinearGradient masks the element and what is inside it by a linear
// gradient across its box, as CSS's mask-image: linear-gradient() does:
// each point shows as much as the gradient's alpha there, whatever its
// colors, and nothing shows outside the box (or the inset of ClipInset).
// A list whose rows fade out at its top and bottom edges:
//
//	black := ui.RGB(0, 0, 0)
//	fade := []ui.GradientStop{{ui.Transparent, 0}, {black, 0.1}, {black, 0.9}, {ui.Transparent, 1}}
//	list.MaskLinearGradient(ui.LinearGradient{Angle: 180, Stops: fade})
//
// Each thing drawn is masked on its own, rather than the element drawn
// into a layer first, as Opacity fades a group: where the mask lets a
// part through, a row's background shows what is under it too.
func (e *node) MaskLinearGradient(g LinearGradient) *node {
	k := e.clippings()
	k.mask, k.linear, k.layers = maskLinear, g, nil
	return e
}

// MaskRadialGradient masks the element and what is inside it by a radial
// gradient, as MaskLinearGradient does by a linear one: a vignette, or a
// spotlight.
func (e *node) MaskRadialGradient(g RadialGradient) *node {
	k := e.clippings()
	k.mask, k.radial, k.layers = maskRadial, g, nil
	return e
}

// MaskConicGradient masks the element and what is inside it by a conic
// gradient, as MaskLinearGradient does by a linear one: a ring revealed
// as its stops turn.
func (e *node) MaskConicGradient(g ConicGradient) *node {
	k := e.clippings()
	k.mask, k.conic, k.layers = maskConic, g, nil
	return e
}

// selfClip returns the rectangle and corner radius that the element clips
// itself to (ClipInset, or its box for a mask alone), and false when it
// clips nothing of itself.
func (e *node) selfClip() (Rect, float32, bool) {
	k := e.clipping
	if k == nil || !k.insetSet && k.mask == maskNone || e.isInline() {
		return Rect{}, 0, false
	}
	r := Rect{e.x, e.y, e.w, e.h}
	if !k.insetSet {
		return r, 0, true
	}
	in := k.inset
	if k.percent {
		in = [4]float32{in[0] * r.H / 100, in[1] * r.W / 100, in[2] * r.H / 100, in[3] * r.W / 100}
	}
	r = Rect{r.X + in[3], r.Y + in[0], max(r.W-in[1]-in[3], 0), max(r.H-in[0]-in[2], 0)}
	return r, k.radius, true
}

// clipSelf pushes the clip of e, painting, around all it paints
// (selfClip): its inset, and its mask's gradient across its box, or the
// mask of its layers (Masks).
func (p *Painter) clipSelf(e *node) {
	r, radius, _ := e.selfClip()
	if e.clipping.mask == maskLayers {
		pushMaskLayers(p, e, r, radius)
		return
	}
	op := scene.Op{Kind: scene.OpPushClip, Rect: p.snap(Rect{e.x, e.y, e.w, e.h})}
	switch k := e.clipping; k.mask {
	case maskLinear:
		p.gradient(&op, k.linear)
	case maskRadial:
		p.radial(&op, k.radial)
	case maskConic:
		p.conic(&op, k.conic)
	}
	op.Rect, op.Radii, op.Continuous = p.snap(r), p.radii([4]float32{radius, radius, radius, radius}), continuousCorners
	p.clip = intersect(p.clip, transformRect(p.transform, r))
	p.add(op)
}

// ClipPath restricts what fn draws to the inside of a path, by its
// FillRule (as SVG's clip-rule), its edges smoothed: a drawing cut to a polygon or to any
// outline, as CSS's and SVG's clip-path. Clips nest with those of Clip,
// and turn with a transform:
//
//	star := ui.MustParsePath("M50 0 L61 35 L98 35 L68 57 L79 91 L50 70 L21 91 L32 57 L2 35 L39 35 Z")
//	p.ClipPath(star, func() {
//		p.FillGradient(r, ui.LinearGradient{From: gold, To: orange, Angle: 135}, 0)
//	})
//
// The clip is a mask of the path's coverage, made once for each shape and
// size it is drawn at.
func (p *Painter) ClipPath(path *Path, fn func()) {
	saved := p.clip
	f := &p.rt.paths.flat
	path.flatten(f, p.scale)
	p.pushPathClip(f)
	fn()
	p.popClip()
	p.clip = saved
}

// pathClipMax is the largest width and height of a path's clip mask, in
// device pixels: what a transform shows of a path larger still is cut.
const pathClipMax = 8192

// pushPathClip pushes a clip to the inside of the flattened path f, with
// a mask of its coverage, cached by its shape (engine.clipMask).
func (p *Painter) pushPathClip(f *flatPath) {
	if f.polys() == 0 {
		p.clip = Rect{}
		p.add(scene.Op{Kind: scene.OpPushClip})
		return
	}
	minX, minY := float32(math.MaxFloat32), float32(math.MaxFloat32)
	maxX, maxY := float32(-math.MaxFloat32), float32(-math.MaxFloat32)
	for _, pt := range f.pts {
		minX, maxX = min(minX, pt[0]), max(maxX, pt[0])
		minY, maxY = min(minY, pt[1]), max(maxY, pt[1])
	}
	// The points move to quarters of a pixel from the pixel grid, as
	// drawPath moves them, so that a path filled where it clips meets
	// its edges.
	gx, gy := float32(math.Floor(float64(minX))), float32(math.Floor(float64(minY)))
	for i, pt := range f.pts {
		f.pts[i] = [2]float32{gx + round((pt[0]-gx)*4)/4, gy + round((pt[1]-gy)*4)/4}
	}
	// A pixel of room around the path, transparent, which sampling the
	// mask between its pixels reaches; within what may show.
	x0, y0 := float64(gx-1), float64(gy-1)
	x1, y1 := math.Ceil(float64(maxX))+1, math.Ceil(float64(maxY))+1
	s := float64(p.scale)
	if v, t := p.clip, p.transform; !t.Set {
		x0, y0 = max(x0, math.Floor(float64(v.X)*s)-1), max(y0, math.Floor(float64(v.Y)*s)-1)
		x1, y1 = min(x1, math.Ceil(float64(v.X+v.W)*s)+1), min(y1, math.Ceil(float64(v.Y+v.H)*s)+1)
	} else if m, ok := t.Pixels(p.scale).Inverse(); ok && !t.Perspective() {
		b := m.Bounds(scene.Rect{X: v.X * p.scale, Y: v.Y * p.scale, W: v.W * p.scale, H: v.H * p.scale})
		x0, y0 = max(x0, math.Floor(float64(b.X))-1), max(y0, math.Floor(float64(b.Y))-1)
		x1, y1 = min(x1, math.Ceil(float64(b.X+b.W))+1), min(y1, math.Ceil(float64(b.Y+b.H))+1)
	}
	w, h := int(min(x1-x0, pathClipMax)), int(min(y1-y0, pathClipMax))
	if w <= 0 || h <= 0 {
		p.clip = Rect{}
		p.add(scene.Op{Kind: scene.OpPushClip})
		return
	}
	ox, oy := float32(x0), float32(y0)
	img := p.rt.clipMask(f, ox, oy, w, h)
	r := scene.Rect{X: ox, Y: oy, W: float32(w), H: float32(h)}
	p.clip = intersect(p.clip, transformRect(p.transform, Rect{ox / p.scale, oy / p.scale, float32(w) / p.scale, float32(h) / p.scale}))
	p.add(scene.Op{Kind: scene.OpPushClip, Rect: r, Image: img, Src: scene.Rect{W: float32(w), H: float32(h)}})
}

// pathClip is the mask of a path a clip was cut to (Painter.ClipPath),
// and the last paint that used it.
type pathClip struct {
	img  *scene.Image
	used uint64
}

// clipMask returns the w×h mask of the coverage of the path f from (x0,
// y0), in device pixels, as an image's alpha: the one made for the same
// shape lately, or a new one (keptMask).
func (rt *engine) clipMask(f *flatPath, x0, y0 float32, w, h int) *scene.Image {
	var buf [8]byte
	var hs maphash.Hash
	hs.SetSeed(pathSeed)
	binary.LittleEndian.PutUint32(buf[:4], uint32(w))
	binary.LittleEndian.PutUint32(buf[4:], uint32(h))
	hs.Write(buf[:])
	for i := range f.polys() {
		for _, pt := range f.poly(i) {
			binary.LittleEndian.PutUint32(buf[:4], math.Float32bits(pt[0]-x0))
			binary.LittleEndian.PutUint32(buf[4:], math.Float32bits(pt[1]-y0))
			hs.Write(buf[:])
		}
		hs.WriteByte(0xff)
	}
	if f.evenOdd {
		hs.WriteByte(0xfe)
	}
	key := hs.Sum64()
	if img := rt.keptMask(key); img != nil {
		return img
	}
	z := &rt.paths.raster
	z.Reset(w, h)
	fillInto(z, f, x0, y0)
	n := w * h
	if cap(rt.paths.pix) < n {
		rt.paths.pix = make([]byte, n)
	}
	cov := rt.paths.pix[:n]
	fillMask(z, f, cov, w)
	pix := make([]byte, 4*n)
	for i, a := range cov {
		pix[4*i+3] = a
	}
	img := scene.NewImageRGBA(w, h, pix)
	rt.keepMask(key, img)
	return img
}

// keptMask returns the mask of a clip kept by key (keepMask), made
// lately, or nil, which forgets the masks no paint used since the one
// before; the scenes that drew them keep them as long as they need.
func (rt *engine) keptMask(key uint64) *scene.Image {
	if c, ok := rt.pathClips[key]; ok {
		c.used = rt.paints
		return c.img
	}
	for k, c := range rt.pathClips {
		if c.used+1 < rt.paints {
			delete(rt.pathClips, k)
		}
	}
	return nil
}

// keepMask keeps img, the mask of a clip, by key, for keptMask.
func (rt *engine) keepMask(key uint64, img *scene.Image) {
	if rt.pathClips == nil {
		rt.pathClips = map[uint64]*pathClip{}
	}
	rt.pathClips[key] = &pathClip{img: img, used: rt.paints}
}
