package ui

import "github.com/egoist/mygo/internal/scene"

// Cache keeps what the element paints, with its children, in a bitmap of
// the GPU that frames draw as one image, as Flutter's RepaintBoundary and
// SwiftUI's drawingGroup do: for a still subtree that costs much to draw,
// as a card of gradients, blurred images and large shadows, under or
// around a drawing that moves. A frame redrawing where something above or
// next to it changed then draws the bitmap there, not each of its layers,
// and moving, scaling or rotating the element (Translate, Scale, Rotate,
// AnimatePose) moves the bitmap, not what it holds.
//
// The bitmap is the size of the element's box at the window's scale, and
// of what it and those inside it paint around it: shadows, focus rings,
// and children beyond the box that it does not clip, as Draw callbacks
// paint (within the box and the shadows' reach). It is drawn again when
// what the subtree paints changes: built anew with other content, laid
// out at another size, at another scale or in another theme, or as
// something inside it moves on its own (Painter.AnimationFrame, a pose,
// a transition), as then it is every frame. Painting the window still
// paints the subtree, to tell; frames that only paint the drawings that
// asked for them (Painter.AnimationFrame, Painter.After) do not.
//
// As in an effect's bitmap (Effect), its text is smoothed in grayscale
// where the subtree paints no opaque background under it, web views
// inside it do not show, and a subtree larger than 8192 pixels across is
// drawn as it is. The bitmap is drawn at the window's scale, so that
// turned, or scaled up by a transform, it shows a little softer than the
// shapes it holds would.
func (e *node) Cache() *node { e.cache = keptLayer{}; return e }

// layerKeeper renders an element's layer only when what it paints
// changed (Cache): the methods are linked into apps that use it alone.
type layerKeeper interface {
	// area returns the area of e's layer, in its own space: its box and
	// what it and those inside it paint around it.
	area(e *node) Rect
	// draw draws cv as Canvas.drawPart does, unless what fn paints is what
	// it painted last.
	draw(cv *Canvas, p *Painter, scale float32, w, h int, fn func(cp *Painter))
}

type keptLayer struct{}

func (keptLayer) area(e *node) Rect {
	box := Rect{e.x, e.y, e.w, e.h}
	grown := func(r Rect, m float32) Rect { return Rect{r.X - m, r.Y - m, r.W + 2*m, r.H + 2*m} }
	// As setInk: the shadows' margin, and as much again as a focus ring.
	area := grown(box, e.shadowMargin()+4)
	if e.flags&(flagClipX|flagClipY|flagScrollX|flagScrollY) != 0 {
		return area // all inside within the box
	}
	inv, ok := e.world.Inverse()
	if !ok {
		return area
	}
	var walk func(n *node, clip Rect, clipped bool)
	walk = func(n *node, clip Rect, clipped bool) {
		for ch := n.first; ch != nil; ch = ch.next {
			if ch.flags&flagInvisible != 0 || ch.hidden || ch.isInline() {
				continue
			}
			// In e's space: e's transform taken off theirs.
			m := inv.Mul(ch.world)
			r := transformRect(m, grown(Rect{ch.x, ch.y, ch.w, ch.h}, ch.shadowMargin()+4))
			if clipped {
				r = intersect(r, clip)
			}
			if r.W > 0 && r.H > 0 {
				area = union(area, r)
			}
			if ch.flags&(flagClipX|flagClipY|flagScrollX|flagScrollY) != 0 {
				cr, _ := ch.clipRect()
				c := transformRect(m, cr)
				if clipped {
					c = intersect(c, clip)
				}
				walk(ch, c, true)
			} else {
				walk(ch, clip, clipped)
			}
		}
	}
	walk(e, Rect{}, false)
	return area
}

// layerCache is what a canvas kept by Cache holds to tell whether what it
// is drawn with changed: the last scene, which differ remembers, and
// another to paint the next into.
type layerCache struct {
	spare  *scene.Scene
	differ scene.Differ
}

func (keptLayer) draw(cv *Canvas, p *Painter, scale float32, w, h int, fn func(cp *Painter)) {
	c := cv.cache
	if c == nil {
		c = &layerCache{spare: &scene.Scene{}}
		cv.cache = c
	}
	last := cv.img.Scene
	cv.img.Scene = c.spare
	cv.paint(p, scale, w, h, fn)
	s := cv.img.Scene
	c.spare = last
	// Kept, the scene is that of the frame all the same, whose glyphs the
	// atlases hold, for a renderer drawing the canvas anew.
	damage, known := c.differ.Diff(s)
	c.differ.Remember(s)
	if !known || len(damage) > 0 || s.Scale != last.Scale || s.Drawn != last.Drawn {
		cv.drawn(p)
	}
}
