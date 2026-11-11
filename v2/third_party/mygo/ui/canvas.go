package ui

import (
	"image"

	"github.com/egoist/mygo/internal/scene"
)

// A Canvas is a bitmap that a painter draws, on the GPU where the window
// draws there: render to texture, as SkSurface, LÖVE's Canvas and raylib's
// RenderTexture2D. A view draws what seldom changes into one once, as a
// chart's grid or a map's ground, and shows it every frame for the price
// of an image; fades or tints a whole layer at once; or draws at a
// resolution of its own and shows it scaled, as pixel art scales up
// crisp (game.IntegerFit, in the game plugin).
//
// It keeps what was last drawn into it until Draw draws it anew, and
// shows anywhere a bitmap does, through Bitmap.
type Canvas struct {
	img *scene.Image
	bm  *Bitmap
	// part is the bitmap of the canvas's top left corner that region
	// returned last.
	part *Bitmap
	// cache tells, for the layer of an element kept by Cache, whether
	// what it paints changed.
	cache *layerCache
}

// NewCanvas returns a canvas of w×h pixels, transparent until drawn.
func NewCanvas(w, h int) *Canvas {
	img := scene.NewCanvas(max(w, 1), max(h, 1))
	return &Canvas{img: img, bm: &Bitmap{img: img, w: img.W, h: img.H}}
}

// Bitmap returns the bitmap the canvas shows as, for Image, Painter.Image
// and Painter.ImageRegion; make it Pixelated to scale pixel art up crisp.
func (cv *Canvas) Bitmap() *Bitmap { return cv.bm }

// Size returns the canvas's size in pixels, the units it is drawn in.
func (cv *Canvas) Size() (w, h int) { return cv.img.W, cv.img.H }

// Draw draws the canvas anew, cleared to transparent, with what fn paints
// with cp in the canvas's pixels, during the frame p paints: a Draw
// callback's painter. What fn paints is what any painter paints: shapes,
// paths, text, images, parts of them, other canvases, under transforms and
// clips. Drawn, the canvas shows it from this frame on.
func (cv *Canvas) Draw(p *Painter, fn func(cp *Painter)) { cv.draw(p, 1, fn) }

// draw is Draw with scale pixels a DIP, the units fn paints in.
func (cv *Canvas) draw(p *Painter, scale float32, fn func(cp *Painter)) {
	cv.drawPart(p, scale, cv.img.W, cv.img.H, fn)
}

// drawPart is draw into the canvas's top left w×h pixels, as an effect's
// layer larger than its content: fn paints nothing beyond them, and
// backdrops end at their edge (scene.Scene.Drawn), as in a canvas of that
// size.
func (cv *Canvas) drawPart(p *Painter, scale float32, w, h int, fn func(cp *Painter)) {
	cv.paint(p, scale, w, h, fn)
	cv.drawn(p)
}

// paint paints fn into the canvas's scene, as drawPart does, leaving the
// canvas as it shows.
func (cv *Canvas) paint(p *Painter, scale float32, w, h int, fn func(cp *Painter)) {
	s := cv.img.Scene
	s.Reset(cv.img.W, cv.img.H, scene.Color{})
	if w != cv.img.W || h != cv.img.H {
		s.Drawn = image.Pt(w, h)
	}
	s.Scale = scale
	parent := p.s
	s.MaskAtlas, s.ColorAtlas, s.Text = parent.MaskAtlas, parent.ColorAtlas, parent.Text
	part := Rect{W: float32(w) / scale, H: float32(h) / scale}
	cp := &Painter{rt: p.rt, s: s, scale: scale, opacity: 1, clip: part, theme: p.theme}
	if s.Drawn != (image.Point{}) {
		cp.Clip(part, 0, func() { fn(cp) })
	} else {
		fn(cp)
	}
}

// drawn has the canvas show what its scene holds from the frame p paints.
func (cv *Canvas) drawn(p *Painter) {
	cv.img.Changed()
	// The frame's renderer draws it, before the frame: canvases drawn in
	// this one first, as they finish first.
	top := &p.rt.scene
	top.Canvases = append(top.Canvases, cv.img)
}

// resize makes the canvas w×h pixels, transparent until drawn, keeping
// its identity in renderers' texture caches: they replace its texture in
// place, rather than keep the old one until the canvas is collected.
func (cv *Canvas) resize(w, h int) {
	cv.img.Resize(max(w, 1), max(h, 1))
	cv.bm = &Bitmap{img: cv.img, w: cv.img.W, h: cv.img.H}
	cv.part = nil
}

// region returns the canvas's top left w×h pixels as a bitmap of that
// size, sharing the canvas's texture: what reads it, as an effect's
// shader, finds it transparent beyond them.
func (cv *Canvas) region(w, h int) *Bitmap {
	if w == cv.img.W && h == cv.img.H {
		return cv.bm
	}
	if p := cv.part; p == nil || p.w != w || p.h != h {
		cv.part = &Bitmap{img: cv.img, w: w, h: h}
	}
	return cv.part
}
