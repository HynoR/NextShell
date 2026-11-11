package raster

import (
	"image"
	"runtime"

	"github.com/egoist/mygo/internal/scene"
)

// Renderer draws the successive scenes of a window into Image, redrawing
// only where a scene differs from the one before (scene.Differ): a
// blinking caret, a ticking clock or a button under the pointer redraws
// only itself.
type Renderer struct {
	// Image holds the last scene drawn, in mem.
	Image Image
	mem   *pixels

	// d draws the bands of large damage.
	d      drawer
	diff   scene.Differ
	damage []image.Rectangle
}

// Render draws s, and returns the rectangles of Image it changed. s may
// change once Render returns.
func (r *Renderer) Render(s *scene.Scene) []image.Rectangle {
	drawCanvases(s)
	if r.whole(s) || r.Image.W != s.Width || r.Image.H != s.Height {
		// A new size, or Image was released (Release).
		r.resize(s.Width, s.Height)
		r.damage = append(r.damage[:0], image.Rect(0, 0, s.Width, s.Height))
	}
	bounds := r.diff.Bounds()
	for _, d := range r.damage {
		r.d.draw(&r.Image, s, d, bounds)
	}
	r.diff.Remember(s)
	return r.damage
}

// pixels is the memory of a Renderer's image (allocPixels).
type pixels struct {
	b       []byte
	mapped  bool
	cleanup runtime.Cleanup
}

// resize makes Image w×h, reusing its memory when it can.
func (r *Renderer) resize(w, h int) {
	n := 4 * w * h
	if r.mem == nil || len(r.mem.b) < n {
		r.mem.free()
		r.mem = allocPixels(n)
	}
	r.Image = Image{W: w, H: h, Stride: 4 * w, Pix: r.mem.b[:n]}
}

// Release frees the image and what Render remembers: the next Render
// draws everything. Image must not be used meanwhile.
func (r *Renderer) Release() {
	r.mem.free()
	*r = Renderer{}
}

func area(rects []image.Rectangle) int {
	n := 0
	for _, d := range rects {
		n += d.Dx() * d.Dy()
	}
	return n
}

// whole sets the damage of s against the last scene, and reports whether
// s must be drawn whole: there is none, or s changes too much of it.
func (r *Renderer) whole(s *scene.Scene) bool {
	r.diff.Reach = 1 // a pixel's antialiased edge
	damage, ok := r.diff.Diff(s)
	if !ok {
		return true
	}
	r.damage = r.diff.AddBackdrops(s, append(r.damage[:0], damage...))
	// Past half the window, drawing it whole costs about the same.
	return area(r.damage)*2 >= s.Width*s.Height
}

// Invalidate makes the next Render draw everything.
func (r *Renderer) Invalidate() { r.diff.Invalidate() }

// opBounds returns where each operation of s draws, within the clips
// around it, for a renderer without a last scene.
func (r *Renderer) opBounds(s *scene.Scene) []image.Rectangle {
	r.diff.Reach = 1
	r.diff.Diff(s)
	return r.diff.Bounds()
}
