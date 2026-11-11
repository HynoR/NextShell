package ui

import "github.com/egoist/mygo/internal/scene"

// Frames that only paint again. A drawing that moves with the time
// (Painter.AnimationFrame, AnimationFrameRate, After) while nothing else
// changes has the next frame paint the elements of this one again, without
// building the view; yet painting them all again, every background, text
// run and Draw callback of the window, costs as much as what the window
// shows. So the frame painting the window keeps, for each Draw or DrawOver
// callback that asked to be painted again, the operations of the scene it
// painted and the painter as it was: a frame that only paints again runs
// those callbacks alone, as they were, and puts what they paint in the
// place of what they painted, in the last frame's scene. Anything else
// asking for such a frame (a pose moving, a drawing in a canvas or a
// layer, content changing) has the frame paint the window whole, as do the
// inspector and a drag.

// paintPart is a Draw (or, after, DrawOver) callback of element e, called
// with box, that painted the operations of the window's scene from start
// to end, with the painter as it was then, its opaque backgrounds and holes
// apart.
type paintPart struct {
	e          *node
	after      bool
	box        Rect
	start, end int
	painter    Painter
	under      []opaqueBox
	holes      []Rect
	// from and to are where it painted the frame being painted, at the
	// end of the scene's operations.
	from, to int
}

// draw calls e's Draw callback (DrawOver's when after) with box, noting
// what it painted when it asks to be painted again (paintPart).
func (p *Painter) draw(e *node, box Rect, after bool) {
	fn := e.paintFn
	if after {
		fn = e.paintAfterFn
	}
	if p.drawing || p.s != &p.rt.scene {
		// Into a layer or a canvas, painted whole.
		fn(p, box)
		return
	}
	rt := p.rt
	start, state, uses := len(p.s.Ops), *p, rt.text.TransientUses()
	p.drawing, p.asked = true, false
	fn(p, box)
	p.drawing = false
	if p.asked {
		rt.partTransient += rt.text.TransientUses() - uses
		rt.drawn = append(rt.drawn, paintPart{e: e, after: after, box: box, start: start, end: len(p.s.Ops), painter: state,
			under: append([]opaqueBox(nil), state.under...), holes: append([]Rect(nil), state.holes...)})
	}
}

// asks notes that the drawing painting asks to be painted again: by
// itself, as its part, from a Draw or DrawOver callback of the window's
// painting, else with the whole window.
func (p *Painter) asks() {
	if p.drawing {
		p.asked = true
	} else {
		p.rt.untracked = true
	}
}

// partsGrowth is how much more glyphs, stops, effects, colors and words a
// scene painted in parts holds than it did painted whole, past which the
// next frame paints it whole: what the parts painted before stays in them.
const partsGrowth = 4096

// sceneData returns how many glyphs, stops, effects, colors and words s
// holds.
func sceneData(s *scene.Scene) int {
	return len(s.Glyphs) + len(s.Stops) + len(s.Effects) + len(s.Wide) + len(s.Words)
}

// repaintParts paints the parts of the last frame painted again (see
// paintPart), and reports whether it could: false when the window must be
// painted whole, as when the glyph atlases, which every window shares,
// moved or forgot the glyphs the rest of the scene draws, as another
// window making room in them does, or when the rest of the scene draws
// masks drawn for its frame alone, in room of the atlas that the frames
// since drew over: painted whole again, they last.
func (rt *engine) repaintParts() bool {
	s := &rt.scene
	if rt.untracked || len(rt.drawn) == 0 || rt.insp.open || rt.drag != nil || rt.noParts ||
		sceneData(s) > 2*rt.partsBase+partsGrowth || atlasGenerations(s) != rt.atlases || rt.transient {
		return false
	}
	rt.partPaints++
	n := len(s.Ops)
	// The canvases the last frame drew are drawn: those the parts draw
	// anew are this frame's.
	clear(s.Canvases)
	s.Canvases = s.Canvases[:0]
	for i := range rt.drawn {
		pt := &rt.drawn[i]
		rt.painter = pt.painter
		p := &rt.painter
		p.under = append(rt.under[:0], pt.under...)
		p.holes = append(rt.holes[:0], pt.holes...)
		fn := pt.e.paintFn
		if pt.after {
			fn = pt.e.paintAfterFn
		}
		// A part that no longer asks to be painted again stays, as what
		// it painted last stays in the scene.
		pt.from = len(s.Ops)
		p.drawing, p.asked = true, false
		fn(p, pt.box)
		p.drawing = false
		pt.to = len(s.Ops)
		rt.under, rt.holes = p.under[:0], p.holes[:0]
	}
	ops, last := rt.partOps[:0], 0
	for i := range rt.drawn {
		pt := &rt.drawn[i]
		ops = append(ops, s.Ops[last:pt.start]...)
		start := len(ops)
		ops = append(ops, s.Ops[pt.from:pt.to]...)
		last, pt.start, pt.end = pt.end, start, len(ops)
	}
	ops = append(ops, s.Ops[last:n]...)
	clear(s.Ops)
	rt.partOps, s.Ops = s.Ops[:0], ops
	return true
}
