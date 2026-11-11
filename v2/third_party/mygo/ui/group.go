package ui

import (
	"image"
	"sync"

	"github.com/egoist/mygo/internal/scene"
)

// Opacity fades an element and what is inside it as one group, as CSS's
// opacity does (CSS Color 4, Compositing and Blending's group opacity):
// they render together into a layer, which is then drawn at the opacity,
// so that children overlapping one another, or the element's own
// background, show as one, not each through the others. A layer costs a
// pass of the GPU, so an element paints flat, each thing faded on its
// own, where that looks the same: where nothing it paints overlaps. It
// paints flat first, and into its layer at once when what it painted
// overlaps, as from then on while it does, so that the frames before
// change what it costs, never what shows.

// paintGroup and checkFlat are Painter's once a view fades an element
// (Opacity, Transition, AnimatePose: linkGroups), so that apps that fade
// none do not link them.
var (
	paintGroup     func(p *Painter, e *node) (groupMark, bool)
	checkFlat      func(p *Painter, e *node, m groupMark)
	linkGroupsOnce sync.Once
)

// linkGroups has elements faded below full opacity paint as groups.
func linkGroups() {
	linkGroupsOnce.Do(func() { paintGroup, checkFlat = (*Painter).paintGroup, (*Painter).checkFlat })
}

// fade returns how opaque the element is, with what is inside it: its
// opacity (Opacity, and its transitions'), times what its pose's Fade
// leaves.
func (e *node) fade() float32 {
	a := float32(1)
	if e.opacitySet {
		a = e.opacity
	}
	if e.poseFade > 0 {
		a *= 1 - e.poseFade
	}
	return a
}

// groupMark is how much of the scene a group painted flat had when it
// began, to take back what it painted when that overlaps (checkFlat).
type groupMark struct {
	ops, glyphs, stops, effects, words, wide, canvases, drawn int
}

// paintGroup paints e, faded with what is inside it, into its state's
// canvas when what it painted the last time overlapped, draws the canvas
// at its fade and reports false. Otherwise, or when nothing it paints
// overlaps now, it returns where the scene is and true, for e to paint
// flat, each thing faded on its own, which checkFlat then checks.
func (p *Painter) paintGroup(e *node) (groupMark, bool) {
	if e.st.grouped && p.groupLayer(e, nil) {
		return groupMark{}, false
	}
	s, top := p.s, &p.rt.scene
	return groupMark{len(s.Ops), len(s.Glyphs), len(s.Stops), len(s.Effects), len(s.Words), len(s.Wide), len(top.Canvases), len(p.rt.drawn)}, true
}

// checkFlat checks what e, a group, painted flat since m: when some of it
// overlaps, it paints e into its layer and takes back what it painted
// flat, and paints e so from the next frame on. What shows thus depends
// on what e paints alone, not on the frames before.
func (p *Painter) checkFlat(e *node, m groupMark) {
	s, st := p.s, e.st
	c, sc := p.clip, p.scale
	over, flat := overlapping(s, s.Ops[m.ops:], scene.Rect{X: c.X * sc, Y: c.Y * sc, W: c.W * sc, H: c.H * sc})
	if !over || flat {
		st.grouped = false
		e.dropGroupLayer()
		return
	}
	// The subtree's Draw callbacks paint again, into the layer.
	st.grouped = p.groupLayer(e, &m)
}

// takeBack takes what was painted since m out of the scene, but for the
// canvases drawn since end of them, those of a layer painted after.
func (p *Painter) takeBack(m *groupMark, end int) {
	s, top, rt := p.s, &p.rt.scene, p.rt
	// What is taken back keeps no image alive, as Scene.Reset clears it.
	clear(s.Ops[m.ops:])
	clear(s.Effects[m.effects:])
	s.Ops, s.Glyphs, s.Stops = s.Ops[:m.ops], s.Glyphs[:m.glyphs], s.Stops[:m.stops]
	s.Effects, s.Words, s.Wide = s.Effects[:m.effects], s.Words[:m.words], s.Wide[:m.wide]
	top.Canvases = append(top.Canvases[:m.canvases], top.Canvases[end:]...)
	rt.drawn = rt.drawn[:m.drawn]
}

// everywhere is an area holding all a window paints, and more.
var everywhere = Rect{X: -1 << 24, Y: -1 << 24, W: 1 << 25, H: 1 << 25}

// clipIn returns what shows of p's clip in e's own space: everywhere
// where e's transform, turning in 3D or flattening, tells no area.
func (p *Painter) clipIn(e *node) Rect {
	w := e.world
	if !w.Set {
		return p.clip
	}
	inv, ok := w.Inverse()
	if !ok || w.Perspective() {
		return everywhere
	}
	return transformRect(inv, p.clip)
}

// inkArea returns from, holding what e painted into its layer in the
// frame before (keepInk).
func (p *Painter) inkArea(e *node, from Rect) Rect {
	st := e.st
	if st.inkFrame == p.rt.frame {
		return inkUnion(from, st.lastInk)
	}
	return inkUnion(from, st.ink)
}

// keepInk returns what the scene of e's layer, drawn from x0, y0 of its
// pixels, paints, in e's own space, and keeps it for the layers of the
// next frames (inkArea). A layer is as large as what its content paints
// rather than as their boxes: a blur, a shadow or a text's shadow inside
// it reaching out of them shows whole.
func (p *Painter) keepInk(e *node, cv *Canvas, x0, y0 float32) Rect {
	var r Rect
	if b := inkBounds(cv.img.Scene); !b.Empty() {
		s := p.scale
		r = Rect{(b.X + x0) / s, (b.Y + y0) / s, b.W / s, b.H / s}
	}
	st := e.st
	if st.inkFrame != p.rt.frame {
		st.lastInk, st.inkFrame = st.ink, p.rt.frame
	}
	st.ink = r
	return r
}

// layerHolds reports whether the w×h pixels from x0, y0, scale pixels a DIP,
// hold r, an area in DIPs, within half a pixel.
func layerHolds(x0, y0 float32, w, h int, scale float32, r Rect) bool {
	if r.W <= 0 || r.H <= 0 {
		return true
	}
	e := 0.5 / scale
	return r.X >= x0/scale-e && r.Y >= y0/scale-e &&
		r.X+r.W <= (x0+float32(w))/scale+e && r.Y+r.H <= (y0+float32(h))/scale+e
}

// inkUnion returns the area holding a and b, either of which may be empty.
func inkUnion(a, b Rect) Rect {
	switch {
	case b.W <= 0 || b.H <= 0:
		return a
	case a.W <= 0 || a.H <= 0:
		return b
	}
	return union(a, b)
}

// groupLayer paints e into its state's canvas, kept as Cache's while what
// it holds is the same, and the canvas at e's fade, in place of what e
// painted flat since flat, unless nil. Unless nothing in it overlaps, or a
// web view or a backdrop shows through what e paints, which no layer
// shows: then it reports false, and paints nothing more, for e to paint
// flat, or keep what it painted so.
func (p *Painter) groupLayer(e *node, flat *groupMark) bool {
	// What e and those inside it paint, as Cache keeps it, and as far as
	// they painted the last time (inkArea), within what shows of the clip.
	view := p.clipIn(e)
	from := intersect(p.inkArea(e, keptLayer{}.area(e)), view)
	end := len(p.rt.scene.Canvases)
	if from.W <= 0 || from.H <= 0 {
		if flat != nil {
			p.takeBack(flat, end)
		}
		return true // nothing of it shows
	}
	s := p.scale
	var cv *Canvas
	var x0, y0 float32
	var w, h int
	for again := false; ; again = true {
		if cv, x0, y0, w, h = p.layer(e, from); cv == nil {
			return false
		}
		// Drawn again only when what it holds changed, as Cache's, so
		// that a group faded still, or fading, costs the GPU one image.
		keptLayer{}.draw(cv, p, s, w, h, layerPaint(e, -x0/s, -y0/s, p.lifting, view))
		// What it painted beyond the layer, as a blur inside it, which the
		// layer cut: drawn again, holding it.
		more := intersect(inkUnion(from, p.keepInk(e, cv, x0, y0)), view)
		if again || layerHolds(x0, y0, w, h, s, more) {
			break
		}
		from = more
	}
	sc := cv.img.Scene
	if over, keep := overlapping(sc, sc.Ops, scene.Rect{W: float32(w), H: float32(h)}); !over || keep {
		return false
	}
	if flat != nil {
		p.takeBack(flat, end)
	}
	p.layerOp(cv, x0, y0, w, h, p.opacity*e.fade(), 0)
	return true
}

// dropGroupLayer lets go of the layer e painted into as a group, painting
// flat or opaque now, unless its effect, blur or Cache paints into it.
func (e *node) dropGroupLayer() {
	if e.effect == nil && e.blur <= 0 && e.cache == nil {
		e.st.effectLayer = nil
	}
}

// groupOps is how many operations painting something overlapping checks
// pairwise: more of them count as overlapping.
const groupOps = 32

// overlapping reports whether two of ops, of s, paint over one another
// within clip and the clips they push, as far as their bounds tell, or more
// than groupOps paint there; and flat, whether one is a hole (a web
// view's) or an effect reading its backdrop, which would not show through
// a layer. Within the clips, what painting skips out of them (paintable)
// counts for nothing either way.
func overlapping(s *scene.Scene, ops []scene.Op, clip scene.Rect) (over, flat bool) {
	var boxes [groupOps]scene.Rect
	n := 0
	eachPainted(s, ops, clip, func(b scene.Rect, hole bool) bool {
		if hole {
			over, flat = false, true
			return false
		}
		if over || b.Empty() {
			return true
		}
		if n == len(boxes) {
			over = true
			return true
		}
		for _, o := range boxes[:n] {
			if b.X < o.X+o.W && o.X < b.X+b.W && b.Y < o.Y+o.H && o.Y < b.Y+b.H {
				over = true
				break
			}
		}
		boxes[n] = b
		n++
		return true
	})
	return over, flat
}

// eachPainted calls fn with the box each op of ops, of s, paints in, as far
// as its bounds tell, within clip and the clips the ops push, until fn
// returns false: empty for what painting skips out of them (paintable),
// and with hole for a hole (a web view's) or an effect reading its
// backdrop, which would not show through a layer.
func eachPainted(s *scene.Scene, ops []scene.Op, clip scene.Rect, fn func(b scene.Rect, hole bool) bool) {
	var clips [8]scene.Rect
	depth := 0
	for i := range ops {
		op := &ops[i]
		var b scene.Rect
		hole := false
		switch op.Kind {
		case scene.OpPushClip:
			// Deeper than it keeps, a clip cuts nothing more.
			if depth < len(clips) {
				clips[depth] = clip
				clip = clip.Intersect(op.Transform.Bounds(op.Rect))
			}
			depth++
			continue
		case scene.OpPopClip:
			if depth--; depth >= 0 && depth < len(clips) {
				clip = clips[depth]
			}
			depth = max(depth, 0)
			continue
		case scene.OpHole:
			hole = true
			b = op.Transform.Bounds(op.Rect)
		case scene.OpEffect:
			hole = int(op.Start) < len(s.Effects) && s.Effects[op.Start].Effect != nil && s.Effects[op.Start].Effect.Backdrop
			b = op.Transform.Bounds(op.Rect)
		case scene.OpShadow:
			b = op.Cast
			if !op.Inset {
				e := 1.5*op.Blur + 1
				b = scene.Rect{X: op.Rect.X - e, Y: op.Rect.Y - e, W: op.Rect.W + 2*e, H: op.Rect.H + 2*e}
			}
			b = op.Transform.Bounds(b)
		case scene.OpGlyphs:
			b = glyphBounds(s, op)
		default:
			b = op.Transform.Bounds(op.Rect)
		}
		if !fn(b.Intersect(clip), hole) {
			return
		}
	}
}

// inkBounds returns what the ops of s, a layer's scene, paint, within the
// clips they push but for the layer's own edge (Scene.Drawn), which what
// they paint may cross: as large as the layer has to be (keepInk).
func inkBounds(s *scene.Scene) scene.Rect {
	ops := s.Ops
	if s.Drawn != (image.Point{}) && len(ops) > 0 && ops[0].Kind == scene.OpPushClip {
		ops = ops[1:] // its pop, unmatched, pops nothing
	}
	var ink scene.Rect
	all := scene.Rect{X: everywhere.X, Y: everywhere.Y, W: everywhere.W, H: everywhere.H}
	eachPainted(s, ops, all, func(b scene.Rect, _ bool) bool {
		switch {
		case b.Empty():
		case ink.Empty():
			ink = b
		default:
			x0, y0 := min(ink.X, b.X), min(ink.Y, b.Y)
			ink = scene.Rect{X: x0, Y: y0, W: max(ink.X+ink.W, b.X+b.W) - x0, H: max(ink.Y+ink.H, b.Y+b.H) - y0}
		}
		return true
	})
	return ink
}

// glyphBounds returns the box the glyphs of op cover, as the renderers draw
// them, in the scene's pixels.
func glyphBounds(s *scene.Scene, op *scene.Op) scene.Rect {
	var b scene.Rect
	if op.Start < 0 || op.End > int32(len(s.Glyphs)) {
		return b
	}
	for _, g := range s.Glyphs[op.Start:op.End] {
		// The GPU's renderers draw the bitmap's own size.
		r := scene.Rect{X: g.X, Y: g.Y, W: max(g.W, float32(g.UW)), H: max(g.H, float32(g.VH))}
		if g.Transform.Set {
			r = g.Transform.Bounds(r)
		}
		r = op.Transform.Bounds(r)
		if b.Empty() {
			b = r
			continue
		}
		x0, y0 := min(b.X, r.X), min(b.Y, r.Y)
		b = scene.Rect{X: x0, Y: y0, W: max(b.X+b.W, r.X+r.W) - x0, H: max(b.Y+b.H, r.Y+r.H) - y0}
	}
	return b
}
