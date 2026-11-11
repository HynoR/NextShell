package ui

import (
	"math"
	"runtime"
	"slices"
	"sync"
	"time"

	"github.com/egoist/mygo/internal/scene"
	"github.com/egoist/mygo/internal/text"
)

// Painter draws an element's own content in Element.Draw and DrawOver
// callbacks, in DIPs relative to the window.
type Painter struct {
	rt        *engine
	s         *scene.Scene
	scale     float32
	opacity   float32
	clip      Rect
	transform scene.Affine
	inline    *node
	// theme is that of the innermost Themed around the element painting
	// (Theme), nil outside any.
	theme *Theme
	// opaque tells that the glyphs painted now land on an opaque
	// background, which subpixel glyphs need: the root's, covering the
	// window, or that of an element around them, as a pane beside a
	// sidebar over the window's material. under holds the boxes of the
	// elements painting with an opaque background around the one painting.
	// holes are those painted for web views (WebView) since the frame
	// began, under which nothing is opaque.
	opaque bool
	under  []opaqueBox
	holes  []Rect
	// cull skips the elements whose ink is clipped out (paintable): in
	// the frame's painting, where the elements are where commit put them.
	cull bool
	// drawing tells that a Draw or DrawOver callback paints into the
	// window's scene, and asked that it asked to be painted again (see
	// paintPart).
	drawing, asked bool
	// lifting is the element flying (ElementTransition.Fly) being painted
	// over the rest of its layer (matches.paint), which elsewhere paints
	// nothing.
	lifting *node
}

// opaqueBox is the box of an element painting an opaque background, and
// how many holes were painted before it, which it covers.
type opaqueBox struct {
	r     Rect
	holes int
}

// continuousCorners curves rounded corners as Apple does, on macOS, where
// AppKit's and SwiftUI's controls have them (scene.Op.Continuous): circular
// elsewhere, as Windows and GTK draw them.
const continuousCorners = runtime.GOOS == "darwin"

func (rt *engine) paint(root *node, w, h, scale float32) {
	rt.paints++
	rt.drawn = rt.drawn[:0]
	s := &rt.scene
	// The root paints the theme's background: frames start transparent, so
	// that a transparent root shows what is behind the content, such as a
	// window's vibrancy.
	pw, ph := rt.pixels(w, h, scale)
	s.Reset(pw, ph, scene.Color{})
	s.Scale, s.Zoom = scale, rt.zoomFactor()
	s.MaskAtlas, s.ColorAtlas = rt.text.MaskAtlas, rt.text.ColorAtlas
	s.Text = rt.text.TextParams()
	opaque := root.bg.A == 255 && root.fill == fillColor && (!root.opacitySet || root.opacity >= 1)
	s.Background = scene.Color{}
	if opaque {
		// What the window shows where it grew before the frame of its new
		// size shows (platform.BackgroundSurface).
		s.Background = root.bg.scene()
	}
	// The painter is the engine's, which draw callbacks get, so that
	// painting allocates none.
	under := rt.under[:0]
	if opaque {
		// Under all the window paints, as what it drags.
		under = append(under, opaqueBox{r: Rect{0, 0, w, h}})
	}
	rt.painter = Painter{rt: rt, s: s, scale: scale, opacity: 1, clip: Rect{0, 0, w, h}, opaque: opaque, under: under, holes: rt.holes[:0]}
	p := &rt.painter
	uses := rt.text.TransientUses()
	rt.partTransient = 0
	p.cull = !rt.inkStale && !rt.noCull
	p.element(root)
	p.cull = false
	if rt.insp.open {
		rt.insp.paintHighlight(rt, p, h)
	}
	rt.paintDrag(p, w, h)
	rt.under, rt.holes = p.under[:0], p.holes[:0] // kept for the next frame's
	rt.partsBase = sceneData(s)
	rt.atlases = atlasGenerations(s)
	rt.transient = rt.text.TransientUses()-uses > rt.partTransient
}

// atlasGenerations returns the generations of the atlases s draws glyphs
// and masks from.
func atlasGenerations(s *scene.Scene) [2]uint64 {
	return [2]uint64{s.MaskAtlas.Generation(), s.ColorAtlas.Generation()}
}

// Now returns the time of the frame being painted, which drawings that
// move compute from.
func (p *Painter) Now() time.Time { return p.rt.c.now }

// Accelerated reports whether the frame draws on a GPU: false where it
// draws in memory (with MYGO_GPU=0, without a GPU MyGo can draw with, as
// in most virtual machines, and in Render and tests) and where the GPU's
// renderer draws on the CPU (Direct3D's WARP, Mesa's llvmpipe, browsers'
// SwiftShader). A drawing that costs too much there, as a shader covering
// much of the window (package shader runs one pixel by pixel on the CPU),
// can draw less, or nothing. Should the answer change, as when the GPU goes
// away or comes back, the window is painted again.
func (p *Painter) Accelerated() bool {
	a, ok := p.rt.host.(interface{ accelerated() bool })
	return ok && a.accelerated()
}

// AnimationFrame asks for the element to be painted again as soon as the
// display can show it, for a drawing that moves with Now while the layout
// stays, as a spinner's: unless something else changed meanwhile, the next
// frame paints the elements of this one again without building the view.
// Call it in every frame while the drawing moves; an element out of view
// is not painted, so it asks for none.
func (p *Painter) AnimationFrame() {
	p.rt.repainting, p.rt.repaintFull = true, true
	p.asks()
}

// AnimationFrameRate is AnimationFrame for a drawing that looks as smooth
// at fps frames a second as at the display's rate, as a record turning, a
// slow wave or a level meter: the window paints it again as often, rather
// than at each of the display's refreshes, 120 a second on ProMotion and
// many others, and has the display refresh as often where it can (macOS),
// for less power. The rate rounds up to one the display shows evenly, a
// whole number of its refreshes a frame: 60, 40 or 30 a second of a
// 120 Hz display's, 60 or 30 of a 60 Hz one's. The window draws at the
// fastest anything in it asks for: at the display's while another drawing
// asks for AnimationFrame or the view animates.
func (p *Painter) AnimationFrameRate(fps float32) {
	p.rt.repainting = true
	p.rt.repaintRate = max(p.rt.repaintRate, fps)
	p.asks()
}

// After asks for the element to be painted again after d, for a drawing
// that changes then, as a spinner's next step: unless something else
// changed meanwhile, that frame paints the elements of this one again
// without building the view. An element out of view is not painted, so
// it asks for none.
func (p *Painter) After(d time.Duration) {
	at := p.rt.c.now.Add(d)
	if p.rt.repaintAt.IsZero() || at.Before(p.rt.repaintAt) {
		p.rt.repaintAt = at
	}
	p.asks()
}

// snap converts a rectangle to device pixels, rounding its edges to whole
// pixels so that edges stay crisp.
func (p *Painter) snap(r Rect) scene.Rect {
	s := p.scale
	x0, y0 := round(r.X*s), round(r.Y*s)
	x1, y1 := round((r.X+r.W)*s), round((r.Y+r.H)*s)
	return scene.Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
}

func round(v float32) float32 { return float32(math.Round(float64(v))) }

func (p *Painter) radii(r [4]float32) [4]float32 {
	return [4]float32{r[0] * p.scale, r[1] * p.scale, r[2] * p.scale, r[3] * p.scale}
}

func (p *Painter) visible(r Rect, margin float32) bool {
	r = transformRect(p.transform, r)
	return r.X-margin < p.clip.X+p.clip.W && r.Y-margin < p.clip.Y+p.clip.H &&
		r.X+r.W+margin > p.clip.X && r.Y+r.H+margin > p.clip.Y
}

// shadowMargin returns how far beyond its box the element's shadows reach.
func (e *node) shadowMargin() float32 {
	margin := float32(0)
	for _, sh := range e.shadows {
		if !sh.inset {
			margin = max(margin, abs32(sh.x)+abs32(sh.y)+sh.blur+sh.spread)
		}
	}
	return margin
}

// ringMargin returns how far beyond its box the focus ring of the
// element may reach, and a DIP more, for its edge's antialiasing: 4 for
// the theme's ring by default.
func (e *node) ringMargin() float32 {
	t := e.c.window
	off := t.FocusRingOffset
	if e.ringSet&ringOffsetSet != 0 {
		off = e.ringOffset
	}
	return max(4, off+t.focusRingWidth()+1)
}

// setInk sets e's ink from vis, its box in the window, once commit set
// those of the elements inside it: the union of the areas within which
// element paints its own content (visible, from its box grown by its
// shadows' margin) — so that painting, where none of the union shows,
// would paint nothing of e, and skips it. As in Blink and Flutter, which
// skip painting what lies outside the cull rect by its visual overflow.
//
// Elements that paint beyond those areas, or do more than paint as they
// are painted, are noCull, and so are those around them: inline text,
// whose words paint over its paragraph (paintInline); effects and blurs,
// which draw their content on a layer reaching beyond it; dividers, laid
// across a container; the debug overlay, over margins; exit transitions'
// copies; and color and shadow transitions, which note the frames they
// paint in.
func (e *node) setInk(vis Rect) {
	m := e.shadowMargin() + e.ringMargin()
	ink := [4]float32{vis.X - m, vis.Y - m, vis.X + vis.W + m, vis.Y + vis.H + m}
	no := e.isInline() || e.kind == kindText && e.first != nil || e.effect != nil || e.blur > 0 ||
		e.flags&(flagDividers|flagDebug) != 0 || e.ghosts || e.leaving != 0 || e.st.trec != nil
	for ch := e.first; ch != nil; ch = ch.next {
		ink = [4]float32{min(ink[0], ch.ink[0]), min(ink[1], ch.ink[1]), max(ink[2], ch.ink[2]), max(ink[3], ch.ink[3])}
		no = no || ch.noCull
	}
	e.ink, e.noCull = ink, no
}

// paintable reports whether painting e may paint something: whether some
// of its ink shows in the clip, as visible tests a box.
func (p *Painter) paintable(e *node) bool {
	if !p.cull || e.noCull {
		return true
	}
	c, r := p.clip, e.ink
	if r[0] < c.X+c.W && r[1] < c.Y+c.H && r[2] > c.X && r[3] > c.Y {
		return true
	}
	p.rt.culled++
	return false
}

func (p *Painter) element(e *node) {
	if e.hidden || e.lift && p.lifting != e {
		return
	}
	savedTransform, savedInline, savedTheme := p.transform, p.inline, p.theme
	p.transform, p.inline = e.world, nil
	if e.themed != nil {
		p.theme = e.themed
	}
	defer func() { p.transform, p.inline, p.theme = savedTransform, savedInline, savedTheme }()

	if e.styleFn != nil {
		e.styleFn(e)
	}
	if r := e.st.trec; r != nil && r.built == p.rt.frame && r.elem == e {
		p.rt.animateColors(e, r)
		if animateShadows != nil {
			animateShadows(p.rt, e, r)
		}
	}
	if e.flags&flagInvisible != 0 || e.backface && e.transform.facesAway() {
		return
	}
	if e.isInline() {
		p.paintInline(e)
		return
	}
	if e.clipping != nil && !e.effecting {
		// Around its effect's or its blur's layer too, as CSS's clip-path
		// and mask go after its filter.
		savedClip := p.clip
		p.clipSelf(e)
		defer func() { p.popClip(); p.clip = savedClip }()
		if p.clip.W <= 0 || p.clip.H <= 0 {
			// Nothing of it shows, as an inset wiping it in starts.
			return
		}
	}
	if p.paintEffect(e) {
		return
	}
	// Faded with what is inside it, it paints as a group (paintGroup), or
	// flat when that shows the same, which painting tells (checkFlat).
	var flat groupMark
	grouped := false
	if paintGroup != nil && e.first != nil && !e.effecting && e.fade() < 1 {
		if p.opacity*e.fade() <= 0.001 {
			// Faded out, as an Exit or a pose's Fade ends: nothing shows,
			// and its layer drawn at an opacity of 0 would show whole, as
			// an op's 0 means 1.
			return
		}
		if flat, grouped = paintGroup(p, e); !grouped {
			return
		}
	} else if e.st.effectLayer != nil && !e.effecting {
		e.dropGroupLayer()
	}
	saved := p.opacity
	if e.flags&flagDisabled != 0 && !e.undimmed {
		p.opacity *= 0.5
	}
	// In its layer (paintEffect, paintGroup), the layer takes its fade.
	if !e.effecting || e.effect != nil {
		p.opacity *= e.fade()
	}
	if p.opacity <= 0.001 {
		p.opacity = saved
		return
	}
	box := Rect{e.x, e.y, e.w, e.h}
	savedOpaque, under := p.opaque, len(p.under)
	if e.fill == fillColor && e.bg.A == 255 && p.opacity >= 1 {
		p.under = append(p.under, opaqueBox{box, len(p.holes)})
	}
	// What the element paints lands on an opaque background when all of
	// it that shows does.
	p.opaque = p.covered(intersect(box, p.clip))
	if e.world.Set && (p.lifting == nil || !translates(e.world)) {
		// Transformed glyphs are drawn with grayscale coverage; but those
		// of an element flying, which a translation shows where it is
		// (matches.pin), as at rest, which renderers make gray while it
		// moves, so that they are the text at rest once there.
		p.opaque = false
	}
	margin := e.shadowMargin()
	clips := e.flags&(flagClipX|flagClipY|flagScrollX|flagScrollY) != 0
	// What an element clips meets its border where both are smoothed, on
	// rounded corners: painted over it there, it would thin the border. The
	// border goes over them instead.
	borderOver := clips && e.first != nil && e.hasBorder()
	own := p.visible(box, margin+4)
	if !own && e.kind == kindText && e.first != nil {
		own = p.inlineVisible(e)
	}
	if own {
		for _, sh := range e.shadows {
			if !sh.inset {
				p.shadow(box, e.radius, sh)
			}
		}
		p.background(e, box, !borderOver)
		if e.webView != nil {
			p.hole(e)
		}
		// Over a web view's page, as over a background.
		for _, sh := range e.shadows {
			if sh.inset {
				p.insetShadow(box, e.radius, e.border, sh)
			}
		}
		if e.paintFn != nil {
			p.draw(e, box, false)
		}
		switch e.kind {
		case kindText:
			ts := e.resolvedText()
			ox, oy := e.x+e.contentX(), e.y+e.contentY()
			var sp spanPaint
			p.inline = e
			if spans := e.paintSpans(&sp); !e.contentSet || !p.contentText(e, ox, oy, ts, spans) {
				p.textLayout(e.tl, ox, oy, ts.color, ts, spans)
				if e.objectFn != nil {
					paintObjects(p, e.tl, ox, oy, e.objectFn)
				}
			}
			p.inline = nil
		case kindImage:
			if !e.contentSet || !p.contentImage(e) {
				p.image(e)
			}
		case kindIcon:
			c := e.resolvedText().color
			if e.gray {
				c = c.gray()
			}
			if !e.contentSet || !p.contentIcon(e, e.contentBox(), c) {
				p.drawIcon(e.svg, e.contentBox(), c, e.rotate)
			}
		case kindInput:
			e.paintInput(p)
		}
	}
	savedClip := p.clip
	if clips {
		inner, rad := e.clipRect()
		p.pushClip(inner, rad)
	}
	if !clips || p.clip.W > 0 && p.clip.H > 0 {
		zOrder := e.zOrder && e != e.c.overlay && zPaint != nil
		if e.ghosts && !zOrder {
			// Elements leaving go under their siblings; by z-index, under
			// those of the same one (paintByZ).
			for c := e.first; c != nil; c = c.next {
				if c.leaving != 0 {
					p.element(c)
				}
			}
		}
		if zOrder {
			zPaint(p, e)
		} else {
			// Elements flying go over their layer: the window's content, under
			// its overlays, or their overlay, under those after it.
			m := p.rt.matches
			layers := m != nil && e == e.c.overlay
			for c := e.first; c != nil; c = c.next {
				if c.flags&flagAbsolute == 0 && !c.sticky && p.paintable(c) {
					p.element(c)
					if layers {
						m.paint(p, c)
					}
				}
			}
			if e.flags&flagDividers != 0 && !e.grid {
				p.dividers(e)
			}
			content := m != nil && e.parent == nil
			for c := e.first; c != nil; c = c.next {
				if content && c == e.c.overlay {
					m.paint(p, nil)
					content = false
				}
				// Sticky children paint above the flow too, in their order
				// among the absolute ones, as CSS's positioned elements do.
				if (c.flags&flagAbsolute != 0 && c.leaving == 0 || c.flags&flagAbsolute == 0 && c.sticky) && p.paintable(c) {
					p.element(c)
					if layers {
						m.paint(p, c)
					}
				}
			}
			if content {
				m.paint(p, nil)
			}
		}
	}
	if clips {
		p.popClip()
		p.clip = savedClip
	}
	if e.scrolls() && own {
		p.scrollbars(e)
	}
	if own && borderOver {
		p.border(e, box)
	}
	if own && e.paintAfterFn != nil {
		p.draw(e, box, true)
	}
	if e.flags&flagDebug != 0 && (e.parent == nil || e.parent.flags&flagDebug == 0) {
		p.debug(e)
	}
	if own && e.kind != kindInput && e.ringShown() {
		p.focusRingOf(e, box, e.radius)
	}
	p.opacity = saved
	if grouped {
		checkFlat(p, e, flat)
	}
	p.opaque, p.under = savedOpaque, p.under[:under]
}

// translates reports whether m only moves what it transforms.
func translates(m scene.Affine) bool {
	_, _, ok := m.Translation()
	return ok
}

// covered reports whether an opaque background around the element painting
// holds all of r, when r has an area.
func (p *Painter) covered(r Rect) bool {
	if r.W <= 0 || r.H <= 0 {
		return true
	}
	for _, u := range slices.Backward(p.under) {
		if b := u.r; r.X >= b.X && r.Y >= b.Y && r.X+r.W <= b.X+b.W && r.Y+r.H <= b.Y+b.H {
			// Unless the web view of a hole painted since shows there.
			for _, h := range p.holes[u.holes:] {
				if i := intersect(r, h); i.W > 0 && i.H > 0 {
					return false
				}
			}
			return true
		}
	}
	return false
}

// clipRect returns the box an element clips its children to, inside its
// border, and its radii: as far as the clip reaches along an axis it does
// not clip.
func (e *node) clipRect() (Rect, [4]float32) {
	r := Rect{e.x + e.border[3], e.y + e.border[0], e.w - e.border[1] - e.border[3], e.h - e.border[0] - e.border[2]}
	var rad [4]float32
	all := e.flags&(flagScrollX|flagScrollY) != 0 || e.flags&flagClip == flagClip
	if all {
		for i, side := range [4][2]int{{0, 3}, {0, 1}, {2, 1}, {2, 3}} {
			rad[i] = max(e.radius[i]-max(e.border[side[0]], e.border[side[1]]), 0)
		}
		return r, rad
	}
	const far = 1e6
	if e.flags&flagClipX == 0 {
		r.X, r.W = -far, 2*far
	}
	if e.flags&flagClipY == 0 {
		r.Y, r.H = -far, 2*far
	}
	return r, rad
}

// borders returns border widths in device pixels, whole ones at least one
// wide where they are not zero.
func (p *Painter) borders(w [4]float32) [4]float32 {
	for i, v := range w {
		if v > 0 {
			w[i] = max(round(v*p.scale), 1)
		}
	}
	return w
}

// fill paints a rounded rectangle with a solid border of bw DIPs.
func (p *Painter) fill(r Rect, radius [4]float32, bg Color, bw float32, bc Color) {
	op := scene.Op{Kind: scene.OpFill, Rect: p.snap(r), Radii: p.radii(radius), Continuous: continuousCorners, Color: bg.scene(), BorderColor: bc.scene(), Opacity: p.opacity}
	var drawn Color // the border's, if any
	if bw > 0 {
		op.Border = p.borders([4]float32{bw, bw, bw, bw})
		drawn = bc
	}
	op.Wide = p.wide(bg, Color{}, drawn)
	p.add(op)
}

// background paints the background of an element, and its border with it
// unless withBorder is false.
func (p *Painter) background(e *node, box Rect, withBorder bool) {
	border := withBorder && e.hasBorder()
	if e.fill == fillMaterial {
		e.material.PaintMaterial(p, box, e.radius)
		if border {
			p.border(e, box)
		}
		return
	}
	// A border of a gradient goes over the background on an op of its own,
	// as it would on the background's.
	if border && e.grads != nil && e.grads.border != borderColor {
		p.background(e, box, false)
		p.border(e, box)
		return
	}
	var visible bool
	switch e.fill {
	case fillColor:
		visible = e.bg.A > 0
	case fillGradient:
		visible = e.grad.From.A > 0 || e.grad.To.A > 0 || anyVisible(e.grad.Stops)
	case fillStripes:
		visible = e.bg.A > 0 || e.stripes.c.A > 0
	case fillConic:
		visible = anyVisible(e.grads.conic.Stops)
	case fillRadial:
		visible = anyVisible(e.grads.radial.Stops)
	}
	if !visible && !border {
		return
	}
	op := scene.Op{Kind: scene.OpFill, Rect: p.snap(box), Radii: p.radii(e.radius), Continuous: continuousCorners, Color: e.bg.scene(), Opacity: p.opacity}
	var bc Color // the border's, if op draws it
	if border {
		op.Border, op.BorderColor, op.Dashed = p.borders(e.border), e.borderC.scene(), e.borderStyle == BorderDashed
		bc = e.borderC
	}
	c, c2 := e.bg, Color{}
	switch e.fill {
	case fillGradient:
		p.gradient(&op, e.grad)
		c, c2 = e.grad.From, e.grad.To
		if len(e.grad.Stops) > 0 {
			c = Color{}
		}
	case fillConic:
		p.conic(&op, e.grads.conic)
		c = Color{}
	case fillRadial:
		p.radial(&op, e.grads.radial)
		c = Color{}
	case fillStripes:
		st := e.stripes
		c, c2 = st.c, e.bg
		a := float64(st.angle) * math.Pi / 180
		w := max(st.width*p.scale, 0.5)
		op.Paint, op.Color, op.Color2 = scene.PaintStripes, st.c.scene(), e.bg.scene()
		op.Gradient = [4]float32{float32(math.Cos(a)), float32(math.Sin(a)), w, w + max(st.gap*p.scale, 0)}
	}
	op.Wide = p.wide(c, c2, bc)
	p.add(op)
}

// hasBorder reports whether the element has a border that shows: of a
// color, or of a gradient.
func (e *node) hasBorder() bool {
	return scene.HasBorder(e.border) && (e.borderC.A > 0 || e.grads != nil && e.grads.border != borderColor)
}

// anyVisible reports whether any of the stops shows.
func anyVisible(stops []GradientStop) bool {
	for _, s := range stops {
		if s.Color.A > 0 {
			return true
		}
	}
	return false
}

// border paints e's border alone.
func (p *Painter) border(e *node, box Rect) {
	op := scene.Op{Kind: scene.OpFill, Rect: p.snap(box), Radii: p.radii(e.radius), Continuous: continuousCorners, Opacity: p.opacity,
		Border: p.borders(e.border), BorderColor: e.borderC.scene(), Dashed: e.borderStyle == BorderDashed, Wide: p.wide(Color{}, Color{}, e.borderC)}
	if g := e.grads; g != nil && g.border != borderColor {
		// The gradient paints it, times opaque white.
		op.BorderPaint, op.BorderColor, op.Wide = true, scene.Color{R: 255, G: 255, B: 255, A: 255}, 0
		if g.border == borderLinear {
			// As stops: Color, which two colors' From would be, is the
			// inside's, none.
			lg := g.borderLinear
			if len(lg.Stops) == 0 {
				start, end := lg.Start, lg.End
				if start == 0 && end == 0 {
					end = 1
				}
				lg.Stops = []GradientStop{{lg.From, start}, {lg.To, end}}
			}
			p.gradient(&op, lg)
		} else {
			p.conic(&op, g.borderConic)
		}
	}
	p.add(op)
}

// gradient sets the paint of op to g, across its rectangle; the caller
// sets op.Wide.
func (p *Painter) gradient(op *scene.Op, g LinearGradient) {
	// CSS angles: 0deg points up, 90deg right.
	a := float64(g.Angle) * math.Pi / 180
	dx, dy := float32(math.Sin(a)), float32(-math.Cos(a))
	half := (abs32(op.Rect.W*dx) + abs32(op.Rect.H*dy)) / 2
	cx, cy := op.Rect.X+op.Rect.W/2, op.Rect.Y+op.Rect.H/2
	x0, y0, x1, y1 := cx-dx*half, cy-dy*half, cx+dx*half, cy+dy*half
	if len(g.Stops) > 0 {
		op.Paint = scene.PaintLinearStops
		if g.Oklab {
			op.Paint = scene.PaintLinearStopsOklab
		}
		if half < 0.25 {
			x1, y1 = x0+dx*0.5, y0+dy*0.5
		}
		op.Gradient, op.Stops = [4]float32{x0, y0, x1, y1}, p.stops(g.Stops)
		return
	}
	start, end := g.Start, g.End
	if start == 0 && end == 0 {
		end = 1
	}
	op.Paint = scene.PaintLinear
	if g.Oklab {
		op.Paint = scene.PaintOklab
	}
	op.Color, op.Color2 = g.From.scene(), g.To.scene()
	sx, sy := x0+(x1-x0)*start, y0+(y1-y0)*start
	ex, ey := x0+(x1-x0)*end, y0+(y1-y0)*end
	if end-start < 0.5/max(2*half, 1) {
		// Stops at one place: a hard edge, half a pixel wide.
		ex, ey = sx+dx*0.5, sy+dy*0.5
	}
	op.Gradient = [4]float32{sx, sy, ex, ey}
}

// conic sets the paint of op to g, around its rectangle.
func (p *Painter) conic(op *scene.Op, g ConicGradient) {
	c := g.Center
	if c == (Point{}) {
		c = Point{0.5, 0.5}
	}
	op.Paint = scene.PaintConic
	if g.Oklab {
		op.Paint = scene.PaintConicOklab
	}
	op.Gradient = [4]float32{op.Rect.X + op.Rect.W*c.X, op.Rect.Y + op.Rect.H*c.Y, g.Angle * math.Pi / 180, 0}
	op.Stops = p.stops(g.Stops)
}

// radial sets the paint of op to g, from a point of its rectangle.
func (p *Painter) radial(op *scene.Op, g RadialGradient) {
	c := g.Center
	if c == (Point{}) {
		c = Point{0.5, 0.5}
	}
	r := op.Rect
	cx, cy := r.X+r.W*c.X, r.Y+r.H*c.Y
	radius := g.Radius * p.scale
	if radius <= 0 {
		// The farthest corner, as CSS's farthest-corner.
		dx, dy := max(abs32(cx-r.X), abs32(r.X+r.W-cx)), max(abs32(cy-r.Y), abs32(r.Y+r.H-cy))
		radius = float32(math.Sqrt(float64(dx*dx + dy*dy)))
	}
	op.Paint = scene.PaintRadial
	if g.Oklab {
		op.Paint = scene.PaintRadialOklab
	}
	// At least half a pixel, as in an empty box: its stops a hard edge.
	op.Gradient = [4]float32{cx, cy, max(radius, 0.5), 0}
	op.Stops = p.stops(g.Stops)
}

// stops adds a gradient's stops to the scene, at most scene.MaxStops of
// them, in order, and returns where they are (scene.Op.Stops).
func (p *Painter) stops(stops []GradientStop) [2]int32 {
	a := int32(len(p.s.Stops))
	last := float32(math.Inf(-1))
	for i, st := range stops {
		if i == scene.MaxStops {
			break
		}
		at := max(st.At, last) // in order, as CSS puts them
		last = at
		p.s.Stops = append(p.s.Stops, scene.Stop{Color: st.Color.scene(), At: at})
	}
	return [2]int32{a, int32(len(p.s.Stops))}
}

// dividers draws the lines between the children of e (Dividers), in the
// middle of the room between each two: after each row of a List but its
// last, else between children next to each other in the tree, on the same
// line.
func (p *Painter) dividers(e *node) {
	var d dividers
	for _, d = range e.c.dividers {
		if d.e == e {
			break
		}
	}
	if d.e != e {
		return
	}
	row := e.row
	if e.list != nil {
		row = false
	}
	// Across the element inside its border, or as far as children reach
	// beyond it, as the rows of a table scrolling sideways.
	lo, hi := e.y+e.border[0], e.y+e.h-e.border[2]
	if !row {
		lo, hi = e.x+e.border[3], e.x+e.w-e.border[1]
	}
	var prev *node
	for c := e.first; c != nil; c = c.next {
		if c.flags&flagAbsolute != 0 || c.collapsed || c.hidden {
			continue
		}
		if f := e.list; f != nil {
			// A List places its rows in any order: a line goes below each.
			if c.listRow && c.rowIndex < f.n-1 {
				p.divider(row, c.y+c.h+max(e.gapY, 0)/2, min(lo, c.x), max(hi, c.x+c.w), d.width, d.color)
			}
			continue
		}
		if a := prev; a != nil {
			// Main-axis start and end of a and c, and their cross extents.
			as, ae, cs, ce := a.y, a.y+a.h, c.y, c.y+c.h
			ax0, ax1, cx0, cx1 := a.x, a.x+a.w, c.x, c.x+c.w
			if row {
				as, ae, cs, ce = a.x, a.x+a.w, c.x, c.x+c.w
				ax0, ax1, cx0, cx1 = a.y, a.y+a.h, c.y, c.y+c.h
			}
			// On the same line of a row or a column that wraps.
			if !e.wrap || ax0 < cx1 && cx0 < ax1 {
				mid := (ae + cs) / 2
				if cs < as {
					mid = (ce + as) / 2 // a reversed row or column
				}
				p.divider(row, mid, min(lo, ax0, cx0), max(hi, ax1, cx1), d.width, d.color)
			}
		}
		prev = c
	}
}

// divider draws a line width DIPs thick centered on at, from lo to hi
// across: vertical in a row, horizontal in a column, whole pixels thick.
func (p *Painter) divider(row bool, at, lo, hi, width float32, c Color) {
	s := p.scale
	t := max(round(width*s), 1)
	a := round(at*s - t/2)
	r := scene.Rect{X: round(lo * s), Y: a, W: round(hi*s) - round(lo*s), H: t}
	box := Rect{lo, a / s, hi - lo, t / s}
	if row {
		r = scene.Rect{X: a, Y: round(lo * s), W: t, H: round(hi*s) - round(lo*s)}
		box = Rect{a / s, lo, t / s, hi - lo}
	}
	if !p.visible(box, 0) {
		return
	}
	p.add(scene.Op{Kind: scene.OpFill, Rect: r, Color: c.scene(), Wide: p.wide(c, Color{}, Color{}), Opacity: p.opacity})
}

// debug outlines an element and the elements inside it: their margins in
// orange, borders and padding in green, and content in blue.
func (p *Painter) debug(e *node) {
	saved := p.opacity
	p.opacity = 1
	var walk func(e *node)
	walk = func(e *node) {
		if e.flags&flagInvisible != 0 || e.hidden {
			return
		}
		box := Rect{e.x, e.y, e.w, e.h}
		if e.marginX() > 0 || e.marginY() > 0 {
			p.fill(Rect{box.X - e.m(3), box.Y - e.m(0), box.W + e.marginX(), box.H + e.marginY()}, [4]float32{}, Color{}, 1, RGBA(249, 115, 22, 0.8))
		}
		if e.padX() > 0 || e.padY() > 0 {
			p.fill(e.contentBox(), [4]float32{}, Color{}, 1, RGBA(34, 197, 94, 0.8))
		}
		p.fill(box, [4]float32{}, Color{}, 1, RGBA(59, 130, 246, 0.9))
		for c := e.first; c != nil; c = c.next {
			walk(c)
		}
	}
	walk(e)
	p.opacity = saved
}

func (p *Painter) pushClip(r Rect, radius [4]float32) {
	p.clip = intersect(p.clip, transformRect(p.transform, r))
	p.add(scene.Op{Kind: scene.OpPushClip, Rect: p.snap(r), Radii: p.radii(radius), Continuous: continuousCorners})
}

func (p *Painter) popClip() {
	p.add(scene.Op{Kind: scene.OpPopClip})
}

// textLayout paints a laid out text from (x, y), in color, with the
// background, underline or strikethrough of ts, and the colors, backgrounds
// and lines of the spans of sp, if any.
func (p *Painter) textLayout(l *text.Layout, x, y float32, color Color, ts textStyle, sp *spanPaint) {
	p.textBackgrounds(l, x, y, ts, sp)
	// Content transitions also paint the old layout; only the current text
	// carries its selection, in the same position, clip and opacity as its glyphs.
	if e := p.inline; e != nil && e.tl == l {
		if ed := e.st.editor; ed != nil && e.flags&flagSelectable != 0 && p.rt.textSelectionVisible(e.st) {
			if a, b := ed.selection(); a != b {
				for _, r := range l.Selection(a, b) {
					p.Fill(Rect{x + r.X, y + r.Y, r.W, r.H}, ts.selectionColor(p.Theme()), 0)
				}
			}
		}
	}
	p.textForeground(l, x, y, color, ts, sp)
}

// paintObjects is textObjects once DrawObjects is called, so that apps
// that never call it do not link it.
var (
	paintObjects func(p *Painter, l *text.Layout, x, y float32, fn func(p *Painter, at int, r Rect))
	linkObjects  sync.Once
)

// textObjects calls fn with each inline object of l, laid out from
// (x, y), that shows: its rune and its box, in the transform of the
// inline element holding it.
func (p *Painter) textObjects(l *text.Layout, x, y float32, fn func(p *Painter, at int, r Rect)) {
	if l == nil {
		return
	}
	var spans []text.Span
	saved := p.transform
	defer func() { p.transform = saved }()
	for li := range l.Lines {
		line := &l.Lines[li]
		for _, g := range line.Glyphs {
			if !g.Object {
				continue
			}
			if spans == nil {
				spans = text.DecodeSpans(l.Params.Spans)
			}
			h, d := objectMetrics(spans, g.Cluster)
			r := Rect{x + g.X, y + line.Baseline - h, g.Advance, h + d}
			p.transform = saved.Mul(p.inlineTransform(g.Cluster))
			if p.visible(r, 0) {
				fn(p, g.Cluster, r)
			}
		}
	}
}

// textBackgrounds paints all backgrounds before the foreground, so a
// selection can sit above them and below the glyphs and decorations.
func (p *Painter) textBackgrounds(l *text.Layout, x, y float32, ts textStyle, sp *spanPaint) {
	if l == nil || ts.background.A == 0 && sp == nil {
		return
	}
	// Inline transforms do not change while the text paints; look for them once.
	clips := !p.transform.Set && !p.inlineTransforms()
	for li := range l.Lines {
		line := &l.Lines[li]
		if clips && (y+line.Y > p.clip.Y+p.clip.H || y+line.Y+line.Height < p.clip.Y) {
			continue
		}
		if ts.background.A > 0 && line.Width > 0 {
			p.Fill(Rect{x + line.X, y + line.Y, line.Width, line.Height}, ts.background, 0)
		}
		if sp != nil {
			sp.backgrounds(p, line, x, y)
		}
	}
}

// textForeground paints glyphs and decorations over backgrounds and selection.
func (p *Painter) textForeground(l *text.Layout, x, y float32, color Color, ts textStyle, sp *spanPaint) {
	if l == nil {
		return
	}
	sys := p.rt.text
	s := p.scale
	deco := decoration{underline: ts.underline, wavy: ts.wavy, dotted: ts.dotted, strike: ts.strike, solid: ts.solid, color: ts.decoColor, thick: ts.decoThick,
		offset: ts.decoOffset, hasOffset: ts.set&setDecoOffset != 0}
	shade := text.ShadeOf(color.R, color.G, color.B)
	// Inline transforms do not change while the text paints; look for them once.
	moves := p.inlineTransforms()
	clips := !p.transform.Set && !moves
	if paintShadows != nil && (ts.shadow.color.A > 0 || sp != nil && sp.shadows()) {
		var spans spanPaint
		if sp != nil {
			spans = *sp
		}
		paintShadows(p, l, x, y, color, ts, spans, moves, clips)
	}
	if paintStrokes != nil && (ts.strokeWidth > 0 && ts.strokeColor.A > 0 || sp != nil && sp.strokes()) {
		// The spans by value, as a call through a variable would move a
		// pointer to the caller's to the heap.
		var spans spanPaint
		if sp != nil {
			spans = *sp
		}
		paintStrokes(p, l, x, y, color, ts, spans, moves, clips)
	}
	start := int32(len(p.s.Glyphs))
	var run *glyphRun
	if sys.JoinsGlyphs() && !moves {
		// The engine's, whose buffers each text reuses.
		run = &p.rt.glyphRun
		run.ids, run.pens, run.glyphs = run.ids[:0], run.pens[:0], run.glyphs[:0]
	}
	for li := range l.Lines {
		line := &l.Lines[li]
		if clips && (y+line.Y > p.clip.Y+p.clip.H || y+line.Y+line.Height < p.clip.Y) {
			continue
		}
		baseline := sys.Baseline((y + line.Baseline) * s)
		for _, g := range line.Glyphs {
			pen := (x + g.X) * s
			if g.Object || clips && (pen > (p.clip.X+p.clip.W)*s || pen+(g.Advance+g.Size)*s < p.clip.X*s) {
				continue
			}
			ix := float32(math.Floor(float64(pen)))
			glyphColor, glyphShade := color, shade
			if sp != nil {
				if glyphColor = sp.color(sp.at(g.Cluster), color); glyphColor != color {
					if glyphColor.A == 0 {
						continue // faded out, or hidden: its advance stays
					}
					glyphShade = text.ShadeOf(glyphColor.R, glyphColor.G, glyphColor.B)
				}
			}
			gi := sys.Glyph(g.Font, g.ID, s, pen, glyphShade, p.opaque && !moves)
			if !gi.OK {
				continue
			}
			glyphColor = glyphColor.Alpha(p.opacity)
			sg := scene.Glyph{
				X: ix + gi.Left, Y: baseline + gi.Top, W: float32(gi.W), H: float32(gi.H),
				U: gi.X, V: gi.Y, UW: gi.W, VH: gi.H,
				Transform: p.inlineTransform(g.Cluster).Pixels(s),
				Color:     glyphColor.scene(), Wide: p.glyphWide(glyphColor), Colored: gi.Colored, Subpixel: gi.Subpixel, Thin: gi.Thin,
			}
			if run != nil {
				run.add(p, g, gi, pen, sg, glyphShade, baseline)
				continue
			}
			p.s.Glyphs = append(p.s.Glyphs, sg)
		}
		if run != nil {
			run.flush(p, baseline)
		}
		if sp != nil {
			sp.lines(p, l, li, x, y, color, deco)
		}
		if deco.underline || deco.strike {
			p.decorations(l, li, 0, len(line.Glyphs), x, y, deco, color)
		}
	}
	if end := int32(len(p.s.Glyphs)); end > start {
		p.add(scene.Op{Kind: scene.OpGlyphs, Start: start, End: end})
	}
}

// paintStrokes is textStrokes, and strokeGlyph the text system's
// StrokeGlyph, once TextStroke is called, so that apps that never stroke
// text do not link them.
var (
	paintStrokes func(p *Painter, l *text.Layout, x, y float32, color Color, ts textStyle, sp spanPaint, moves, clips bool)
	strokeGlyph  func(s *text.System, f *text.Font, id uint32, scale, x float32, shade text.Shade, opaque bool, width float32) text.GlyphImage
)

func linkStrokes() {
	if paintStrokes == nil {
		paintStrokes, strokeGlyph = (*Painter).textStrokes, (*text.System).StrokeGlyph
	}
}

// textStrokes paints the strokes of the glyphs of l, laid out from (x, y),
// under them (TextStroke): those of ts, or of spans where it has any, in
// their colors, along the glyphs of color, as textForeground draws them.
// moves tells that inline transforms move glyphs, and clips that glyphs
// outside the clip are left out.
func (p *Painter) textStrokes(l *text.Layout, x, y float32, color Color, ts textStyle, spans spanPaint, moves, clips bool) {
	var sp *spanPaint
	if spans.spans != nil {
		sp = &spans
	}
	sys := p.rt.text
	s := p.scale
	shade := text.ShadeOf(color.R, color.G, color.B)
	start := int32(len(p.s.Glyphs))
	for li := range l.Lines {
		line := &l.Lines[li]
		if clips && (y+line.Y > p.clip.Y+p.clip.H || y+line.Y+line.Height < p.clip.Y) {
			continue
		}
		baseline := sys.Baseline((y + line.Baseline) * s)
		for _, g := range line.Glyphs {
			pen := (x + g.X) * s
			width, stroke := ts.strokeWidth, ts.strokeColor
			if g.Object || clips && (pen > (p.clip.X+p.clip.W+width)*s || pen+(g.Advance+g.Size+width)*s < p.clip.X*s) {
				continue
			}
			glyphShade := shade
			if sp != nil {
				k := sp.at(g.Cluster)
				if c := sp.color(k, color); c != color {
					glyphShade = text.ShadeOf(c.R, c.G, c.B)
				}
				width, stroke = sp.stroke(k, width, stroke)
			}
			if width <= 0 || stroke.A == 0 {
				continue
			}
			gi := strokeGlyph(sys, g.Font, g.ID, s, pen, glyphShade, p.opaque && !moves, width*s)
			if !gi.OK {
				continue
			}
			stroke = stroke.Alpha(p.opacity)
			p.s.Glyphs = append(p.s.Glyphs, scene.Glyph{
				X: float32(math.Floor(float64(pen))) + gi.Left, Y: baseline + gi.Top, W: float32(gi.W), H: float32(gi.H),
				U: gi.X, V: gi.Y, UW: gi.W, VH: gi.H,
				Transform: p.inlineTransform(g.Cluster).Pixels(s),
				Color:     stroke.scene(), Wide: p.glyphWide(stroke),
			})
		}
	}
	if end := int32(len(p.s.Glyphs)); end > start {
		p.add(scene.Op{Kind: scene.OpGlyphs, Start: start, End: end})
	}
}

// paintShadows is textShadows, and shadowGlyph the text system's
// ShadowGlyph, once a TextShadow is made (NewTextShadow), so that apps
// whose text casts none do not link them.
var (
	paintShadows func(p *Painter, l *text.Layout, x, y float32, color Color, ts textStyle, sp spanPaint, moves, clips bool)
	shadowGlyph  func(s *text.System, f *text.Font, id uint32, scale, x float32, shade text.Shade, opaque bool, sigma float32) text.GlyphImage
)

func linkTextShadows() {
	if paintShadows == nil {
		paintShadows, shadowGlyph = (*Painter).textShadows, (*text.System).ShadowGlyph
	}
}

// textShadows paints the shadows the glyphs of l, laid out from (x, y),
// cast under them and their strokes (TextShadow): that of ts, or of spans
// where it has any, as textForeground draws the glyphs. moves tells that
// inline transforms move glyphs, and clips that glyphs whose shadow falls
// outside the clip are left out.
func (p *Painter) textShadows(l *text.Layout, x, y float32, color Color, ts textStyle, spans spanPaint, moves, clips bool) {
	var sp *spanPaint
	if spans.spans != nil {
		sp = &spans
	}
	sys := p.rt.text
	s := p.scale
	shade := text.ShadeOf(color.R, color.G, color.B)
	start := int32(len(p.s.Glyphs))
	for li := range l.Lines {
		line := &l.Lines[li]
		for _, g := range line.Glyphs {
			if g.Object {
				continue
			}
			sh := ts.shadow
			glyphShade := shade
			if sp != nil {
				k := sp.at(g.Cluster)
				c := sp.color(k, color)
				if c.A == 0 {
					continue // faded out, or hidden, with its glyph
				}
				if c != color {
					glyphShade = text.ShadeOf(c.R, c.G, c.B)
				}
				sh = sp.shadow(k, sh)
			}
			if sh.color.A == 0 {
				continue
			}
			// The blur reaches three sigmas, of half the blur each.
			reach := sh.blur * 1.5
			gx, gy := x+g.X+sh.x, y+line.Y+sh.y
			if clips && (gy-reach > p.clip.Y+p.clip.H || gy+line.Height+reach < p.clip.Y ||
				gx-reach > p.clip.X+p.clip.W || gx+g.Advance+g.Size+reach < p.clip.X) {
				continue
			}
			pen := gx * s
			gi := shadowGlyph(sys, g.Font, g.ID, s, pen, glyphShade, p.opaque && !moves, sh.blur/2*s)
			if !gi.OK {
				continue
			}
			c := sh.color.Alpha(p.opacity)
			p.s.Glyphs = append(p.s.Glyphs, scene.Glyph{
				X: float32(math.Floor(float64(pen))) + gi.Left, Y: sys.Baseline((y+line.Baseline+sh.y)*s) + gi.Top, W: float32(gi.W), H: float32(gi.H),
				U: gi.X, V: gi.Y, UW: gi.W, VH: gi.H,
				Transform: p.inlineTransform(g.Cluster).Pixels(s),
				Color:     c.scene(), Wide: p.glyphWide(c),
			})
		}
	}
	if end := int32(len(p.s.Glyphs)); end > start {
		p.add(scene.Op{Kind: scene.OpGlyphs, Start: start, End: end})
	}
}

// glyphRun gathers glyphs of a line in one font and color whose
// antialiased edges share pixels, which the text system joins where the
// system's text stack draws them joined (System.JoinsGlyphs).
type glyphRun struct {
	font   *text.Font
	shade  text.Shade
	ids    []uint32
	pens   []float32
	glyphs []scene.Glyph
	last   text.GlyphImage
}

// add takes a glyph, its pen at pen and its scene glyph sg, into the run
// if it shares pixels with the last, else starts a run with it.
func (r *glyphRun) add(p *Painter, g text.Glyph, gi text.GlyphImage, pen float32, sg scene.Glyph, shade text.Shade, baseline float32) {
	if n := len(r.glyphs); n > 0 {
		prev := &r.glyphs[n-1]
		if g.Font == r.font && sg.Color == prev.Color && sg.Wide == prev.Wide && !gi.Colored && p.rt.text.Shares(r.last, prev.X, prev.Y, gi, sg.X, sg.Y) {
			r.ids, r.pens, r.glyphs, r.last = append(r.ids, g.ID), append(r.pens, pen), append(r.glyphs, sg), gi
			return
		}
		r.flush(p, baseline)
	}
	r.font, r.shade, r.last = g.Font, shade, gi
	r.ids, r.pens, r.glyphs = append(r.ids, g.ID), append(r.pens, pen), append(r.glyphs, sg)
}

// flush paints the run: one glyph as it is, more joined.
func (r *glyphRun) flush(p *Painter, baseline float32) {
	switch n := len(r.glyphs); {
	case n == 1:
		p.s.Glyphs = append(p.s.Glyphs, r.glyphs[0])
	case n > 1:
		gi := p.rt.text.GlyphRun(r.font, r.ids, r.pens, p.scale, r.shade, p.opaque)
		if !gi.OK {
			p.s.Glyphs = append(p.s.Glyphs, r.glyphs...)
			break
		}
		sg := r.glyphs[0]
		sg.X, sg.Y = float32(math.Floor(float64(r.pens[0])))+gi.Left, baseline+gi.Top
		sg.W, sg.H, sg.U, sg.V, sg.UW, sg.VH = float32(gi.W), float32(gi.H), gi.X, gi.Y, gi.W, gi.H
		sg.Subpixel, sg.Thin = gi.Subpixel, gi.Thin
		p.s.Glyphs = append(p.s.Glyphs, sg)
	}
	r.ids, r.pens, r.glyphs = r.ids[:0], r.pens[:0], r.glyphs[:0]
}

// decoration is how lines go through or under text.
type decoration struct {
	underline, wavy, dotted, strike bool
	// solid draws the underline through descenders.
	solid bool
	// color, if set, is the lines', and thick their thickness in DIPs;
	// offset, if hasOffset, is how far below the baseline the top of the
	// underline is, in DIPs.
	color     Color
	thick     float32
	offset    float32
	hasOffset bool
}

// lineStyle returns how text's engine draws the lines of d.
func (d decoration) lineStyle() text.LineStyle {
	return text.LineStyle{Thick: d.thick, Offset: d.offset, HasOffset: d.hasOffset}
}

// decorations draws the lines of d along glyphs [i, j) of line li of l,
// laid out from (x, y) in DIPs, where the system's text stack places
// underlines and strikethroughs, in the color of d or c. Wavy underlines
// follow the underline, at least a pixel thick.
func (p *Painter) decorations(l *text.Layout, li, i, j int, x, y float32, d decoration, c Color) {
	if !p.inlineTransforms() {
		p.decorationRun(l, li, i, j, x, y, d, c)
		return
	}
	gs := l.Lines[li].Glyphs
	saved := p.transform
	for i < j {
		m := p.inlineTransform(gs[i].Cluster)
		end := i + 1
		for end < j && p.inlineTransform(gs[end].Cluster) == m {
			end++
		}
		p.transform = saved.Mul(m)
		p.decorationRun(l, li, i, end, x, y, d, c)
		i = end
	}
	p.transform = saved
}

func (p *Painter) decorationRun(l *text.Layout, li, i, j int, x, y float32, d decoration, c Color) {
	if d.color.A > 0 {
		c = d.color
	}
	for _, k := range [2]text.Decoration{text.Underline, text.Strikethrough} {
		if k == text.Underline && !d.underline || k == text.Strikethrough && !d.strike {
			continue
		}
		kind := k
		if k == text.Underline && d.solid {
			kind = text.SolidUnderline
		}
		for _, st := range p.rt.text.Decorate(l, li, i, j, x, y, p.scale, kind, d.lineStyle()) {
			if st.X1 <= st.X0 || st.Bottom <= st.Top {
				continue
			}
			if k == text.Underline && d.wavy {
				p.wave(st.X0, st.X1, (st.Top+st.Bottom)/2, max(st.Bottom-st.Top, 1), c)
				continue
			}
			if k == text.Underline && d.dotted {
				p.dots(st.X0, st.X1, st.Bottom, st.Bottom-st.Top, c)
				continue
			}
			p.add(scene.Op{Kind: scene.OpFill, Rect: scene.Rect{X: st.X0, Y: st.Top, W: st.X1 - st.X0, H: st.Bottom - st.Top}, Color: c.scene(), Wide: p.wide(c, Color{}, Color{}), Opacity: p.opacity})
		}
	}
}

// wave strokes a wave from x0 to x1 around y, thick device pixels wide,
// one and a half times as high, as spell checkers underline words.
func (p *Painter) wave(x0, x1, y, thick float32, c Color) {
	s := p.scale
	amp := thick * 1.5 / 2
	length := thick * 6
	y += amp
	var path Path
	// Eight points a wave, the last at x1.
	n := max(int(math.Ceil(float64((x1-x0)/(length/8)))), 1)
	path.MoveTo(x0/s, y/s)
	for i := 1; i <= n; i++ {
		x := x0 + (x1-x0)*float32(i)/float32(n)
		path.LineTo(x/s, (y-amp*float32(math.Sin(float64((x-x0)/length*2*math.Pi))))/s)
	}
	p.StrokePath(&path, thick/s, c)
}

// dots fills square dots as thick as the underline's stroke, which ends
// at bottom in device pixels, every three DIPs or two dots' widths apart
// when they are thicker, as links mark hover.
func (p *Painter) dots(x0, x1, bottom, thick float32, c Color) {
	size := max(float32(math.Round(float64(thick))), 1)
	for x := x0; x < x1; x += max(3*p.scale, 2*size) {
		p.add(scene.Op{Kind: scene.OpFill, Rect: scene.Rect{X: x, Y: bottom - size, W: size, H: size}, Color: c.scene(), Wide: p.wide(c, Color{}, Color{}), Opacity: p.opacity})
	}
}

// contentBox returns the element's box inside its padding and border.
func (e *node) contentBox() Rect {
	return Rect{e.x + e.contentX(), e.y + e.contentY(), e.w - e.padX(), e.h - e.padY()}
}

func (p *Painter) image(e *node) {
	if s := e.svg; s != nil {
		p.drawSVG(s, e.contentBox(), e.fit, e.objectPos, e.radius, e.resolvedText().color, e.gray)
		return
	}
	img := e.image
	if img == nil || img.w == 0 || img.h == 0 {
		return
	}
	p.drawBitmap(img, e.contentBox(), e.fit, e.objectPos, e.radius, e.gray)
}

// fitIn returns where a picture w×h DIPs goes in box as fit says, placed
// pos from the middle of the room around it (ObjectPosition), and which
// part of it shows there, as fractions of its size.
func fitIn(box Rect, w, h float32, fit Fit, pos [2]float32) (dst, src Rect) {
	dst, src = box, Rect{0, 0, 1, 1}
	scale := float32(1)
	switch fit {
	case FillBox:
		return dst, src
	case Contain:
		scale = min(box.W/w, box.H/h)
	case Cover:
		scale = max(box.W/w, box.H/h)
	case ScaleDown:
		scale = min(box.W/w, box.H/h, 1)
	}
	dst = placeIn(box, w*scale, h*scale, pos)
	// Only what falls in the box shows.
	shown := intersect(dst, box)
	src = Rect{(shown.X - dst.X) / dst.W, (shown.Y - dst.Y) / dst.H, shown.W / dst.W, shown.H / dst.H}
	return shown, src
}

// placeIn returns the box of a picture w×h DIPs in box, placed pos from
// the middle of the room around it (ObjectPosition).
func placeIn(box Rect, w, h float32, pos [2]float32) Rect {
	return Rect{box.X + (box.W-w)*(0.5+pos[0]), box.Y + (box.H-h)*(0.5+pos[1]), w, h}
}

func (p *Painter) drawBitmap(img *Bitmap, box Rect, fit Fit, pos [2]float32, radius [4]float32, gray bool) {
	// A bitmap's own size is its pixels in DIPs, as an Image lays it out.
	iw, ih := float32(img.w), float32(img.h)
	dst, frac := fitIn(box, iw, ih, fit, pos)
	if dst.W <= 0 || dst.H <= 0 {
		return
	}
	// Far smaller than its pixels, a level of it, as smooth.
	shrink := min(frac.W*iw/(dst.W*p.scale), frac.H*ih/(dst.H*p.scale))
	shown := img.smaller(shrink)
	// The bitmap in the pixels of the level shown: all of it, but for the
	// part of a canvas an effect gets (Canvas.region).
	sw, sh := iw*float32(shown.W)/float32(img.img.W), ih*float32(shown.H)/float32(img.img.H)
	src := scene.Rect{X: frac.X * sw, Y: frac.Y * sh, W: frac.W * sw, H: frac.H * sh}
	p.add(scene.Op{Kind: scene.OpImage, Rect: p.snap(dst), Radii: p.radii(radius), Continuous: continuousCorners, Image: shown, Src: src, Opacity: p.opacity, Grayscale: gray,
		Pixelated: img.pixelated && shrink <= 1})
}

// scrollbars draws the thumbs of a scroll container whose content
// overflows it.
func (p *Painter) scrollbars(e *node) {
	st := e.st
	rt := e.c.rt
	theme := p.Theme()
	hovered := false
	for _, id := range rt.hover {
		if id == e.id {
			hovered = true
			break
		}
	}
	dragging := rt.scrollDrag.st == st
	// A press takes the bars only where the window shows them
	// (scrollbarPress). Kept, they show as on hover, the same thumb on no
	// track, so that the pointer coming over changes nothing.
	hide, keep := e.scrollbars()
	st.barsShown = !hide && (keep || hovered || dragging)
	if !st.barsShown {
		return
	}
	color := theme.Scrollbar
	w, h := e.contentW, e.contentH
	x, y := st.scrollX, st.scrollY
	if dragging {
		// The thumb keeps its length, and goes as far along the track as
		// the offset is through the content now (dragTo).
		d := &rt.scrollDrag
		w, h = d.contentW, d.contentH
		x = rescale(x, e.contentW-float64(e.w), w-float64(e.w))
		y = rescale(y, e.contentH-float64(e.h), h-float64(e.h))
	}
	g := scrollBars(Rect{e.x, e.y, e.w, e.h}, e.barInset, float32(w), float32(h), float32(x), float32(y), e.flags, theme.scrollbarWidth(), e.rtl())
	if g.vertical {
		p.scrollThumbFill(g.v, false, dragging && !rt.scrollDrag.horizontal, color)
	}
	if g.horizontal {
		p.scrollThumbFill(g.h, true, dragging && rt.scrollDrag.horizontal, color)
	}
}

// rescale returns offset off of content scrolling as far as now as far
// through content scrolling as far as then.
func rescale(off, now, then float64) float64 {
	if now <= 0 {
		return 0
	}
	return off / now * then
}

// scrollGeometry is where the scroll bars of a container go.
type scrollGeometry struct {
	vertical, horizontal bool
	// v and h are the thumbs, vTrack and hTrack the tracks they move on.
	v, h, vTrack, hTrack Rect
}

// scrollBars returns the scroll bars of a container box scrolled by
// (x, y) over content w×h, with thumbs width DIPs wide, moved in from its
// edges by inset (top, right, bottom, left, ScrollbarInsets): those of the
// directions its flags scroll that overflow, which leave each other the
// corner where both show. Right to left (rtl), the vertical bar is on the
// left, as in Flutter, browsers and GTK, the corner at the bottom left,
// and x, which counts from the right, puts the horizontal thumb at the
// right at 0.
func scrollBars(box Rect, inset [4]float32, w, h, x, y float32, flags uint32, width float32, rtl bool) scrollGeometry {
	var g scrollGeometry
	g.vertical = flags&flagScrollY != 0 && h > box.H+0.5
	g.horizontal = flags&flagScrollX != 0 && w > box.W+0.5
	corner := float32(0)
	if g.vertical && g.horizontal {
		corner = width + 3
	}
	top, right, bottom, left := inset[0], inset[1], inset[2], inset[3]
	if g.vertical {
		track := max(box.H-top-bottom-corner, 0)
		g.vTrack = Rect{box.X + box.W - right - width - 6, box.Y + top, width + 6, track}
		t := scrollThumb(box.Y+top, track, box.H, h, y)
		g.v = Rect{box.X + box.W - right - width - 3, t.Y, width, t.H}
		if rtl {
			g.vTrack.X = box.X + left
			g.v.X = box.X + left + 3
		}
	}
	if g.horizontal {
		track := max(box.W-left-right-corner, 0)
		g.hTrack = Rect{box.X + left, box.Y + box.H - bottom - width - 6, track, width + 6}
		t := scrollThumb(box.X+left, track, box.W, w, x)
		g.h = Rect{t.Y, box.Y + box.H - bottom - width - 3, t.H, width}
		if rtl {
			g.hTrack.X += corner
			// As far from the right of the track as the thumb would be
			// from its left.
			g.h.X = g.hTrack.X + g.hTrack.W - (t.Y - (box.X + left)) - t.H
		}
	}
	return g
}

// scrollThumb returns the thumb's position (Y) and length (H) along a
// track starting at pos, track long, for a view of content of size content
// scrolled by offset.
func scrollThumb(pos, track, view, content, offset float32) Rect {
	inner := track - 4
	thumb := min(max(inner*view/content, 24), inner)
	travel := inner - thumb
	at := float32(0)
	if content > view {
		at = max(0, min(travel*offset/(content-view), travel))
	}
	return Rect{Y: pos + 2 + at, H: thumb}
}

// Fill paints a rounded rectangle.
func (p *Painter) Fill(r Rect, c Color, radius float32) {
	p.fill(r, [4]float32{radius, radius, radius, radius}, c, 0, Color{})
}

// FillRadii paints a rounded rectangle whose corners have radii of their
// own, clockwise from the top left, as Element.Radius does.
func (p *Painter) FillRadii(r Rect, radii [4]float32, c Color) {
	p.fill(r, radii, c, 0, Color{})
}

// FillGradient paints a rounded rectangle with a gradient.
func (p *Painter) FillGradient(r Rect, g LinearGradient, radius float32) {
	op := scene.Op{Kind: scene.OpFill, Rect: p.snap(r), Radii: p.radii([4]float32{radius, radius, radius, radius}), Continuous: continuousCorners, Opacity: p.opacity}
	p.gradient(&op, g)
	op.Wide = p.wide(g.From, g.To, Color{})
	p.add(op)
}

// FillConicGradient fills a rounded rectangle with a conic gradient
// around it.
func (p *Painter) FillConicGradient(r Rect, g ConicGradient, radius float32) {
	op := scene.Op{Kind: scene.OpFill, Rect: p.snap(r), Radii: p.radii([4]float32{radius, radius, radius, radius}), Continuous: continuousCorners, Opacity: p.opacity}
	p.conic(&op, g)
	p.add(op)
}

// FillRadialGradient fills a rounded rectangle with a radial gradient,
// its colors spreading out from a point of it.
func (p *Painter) FillRadialGradient(r Rect, g RadialGradient, radius float32) {
	op := scene.Op{Kind: scene.OpFill, Rect: p.snap(r), Radii: p.radii([4]float32{radius, radius, radius, radius}), Continuous: continuousCorners, Opacity: p.opacity}
	p.radial(&op, g)
	p.add(op)
}

// Stroke paints the outline of a rounded rectangle, width DIPs wide inside
// its edge.
func (p *Painter) Stroke(r Rect, c Color, radius, width float32) {
	p.fill(r, [4]float32{radius, radius, radius, radius}, Color{}, width, c)
}

// StrokeDashed paints the outline of a rounded rectangle in dashes, as a
// dashed border.
func (p *Painter) StrokeDashed(r Rect, c Color, radius, width float32) {
	op := scene.Op{Kind: scene.OpFill, Rect: p.snap(r), Radii: p.radii([4]float32{radius, radius, radius, radius}), Continuous: continuousCorners,
		Border: p.borders([4]float32{width, width, width, width}), BorderColor: c.scene(), Dashed: true, Opacity: p.opacity, Wide: p.wide(Color{}, Color{}, c)}
	p.add(op)
}

// Shadow paints the box shadow of a rounded rectangle as Element.Shadow
// does: offset by x and y, blurred by blur and grown by spread DIPs, and
// only outside the rectangle, so that it does not show through what is
// drawn there.
func (p *Painter) Shadow(r Rect, radius, x, y, blur, spread float32, c Color) {
	p.shadow(r, [4]float32{radius, radius, radius, radius}, shadow{x, y, blur, spread, c, false})
}

// shadow paints the shadow sh of box, rounded by rad, outside box, as
// CSS's box-shadow does.
func (p *Painter) shadow(box Rect, rad [4]float32, sh shadow) {
	r := Rect{box.X + sh.x - sh.spread, box.Y + sh.y - sh.spread, box.W + 2*sh.spread, box.H + 2*sh.spread}
	grown := rad
	for i := range grown {
		if grown[i] > 0 {
			grown[i] = max(grown[i]+sh.spread, 0)
		}
	}
	p.add(scene.Op{Kind: scene.OpShadow, Rect: p.snap(r), Radii: p.radii(grown), Continuous: continuousCorners, Color: sh.color.scene(),
		Wide: p.wide(sh.color, Color{}, Color{}), Blur: sh.blur * p.scale, Cast: p.snap(box), CastRadii: p.radii(rad), Opacity: p.opacity})
}

// insetShadow paints the inner shadow sh of box, rounded by rad, inside
// its padding edge, within a border of widths bw, as CSS's box-shadow:
// inset does.
func (p *Painter) insetShadow(box Rect, rad, bw [4]float32, sh shadow) {
	pad := p.snap(box)
	padRadii := scene.FitRadii(pad, p.radii(rad))
	if scene.HasBorder(bw) {
		pad, padRadii = scene.InnerRadii(pad, padRadii, p.borders(bw))
	}
	if pad.Empty() {
		return
	}
	// The hole the shadow leaves: the padding box offset and shrunk by
	// the spread.
	s := p.scale
	spread := sh.spread * s
	hole := scene.Rect{X: pad.X + round(sh.x*s) + spread, Y: pad.Y + round(sh.y*s) + spread, W: max(pad.W-2*spread, 0), H: max(pad.H-2*spread, 0)}
	holeRadii := padRadii
	for i := range holeRadii {
		if holeRadii[i] > 0 {
			holeRadii[i] = max(holeRadii[i]-spread, 0)
		}
	}
	p.add(scene.Op{Kind: scene.OpShadow, Inset: true, Rect: hole, Radii: holeRadii, Continuous: continuousCorners, Color: sh.color.scene(),
		Wide: p.wide(sh.color, Color{}, Color{}), Blur: sh.blur * s, Cast: pad, CastRadii: padRadii, Opacity: p.opacity})
}

// Line paints a straight horizontal or vertical line between two points,
// width DIPs thick. It draws no other angle: between points that differ in
// both x and y, it paints the longer of the top and left edges of the box
// between them, the top if they are as long. StrokePolyline draws a
// slanted line.
func (p *Painter) Line(x0, y0, x1, y1, width float32, c Color) {
	r := Rect{min(x0, x1), min(y0, y1), abs32(x1 - x0), abs32(y1 - y0)}
	if r.W < r.H {
		r.X -= width / 2
		r.W = width
	} else {
		r.Y -= width / 2
		r.H = width
	}
	p.Fill(r, c, 0)
}

// Text draws a line of text with its top-left corner at (x, y).
func (p *Painter) Text(x, y float32, s string, size float32, c Color) {
	l := p.rt.text.Layout(text.Params{Text: s, Style: text.Style{Size: size}})
	p.textLayout(l, x, y, c, textStyle{size: size}, nil)
}

// Image draws a bitmap, or an SVG in its own colors (with the theme's text
// color for its currentColor), scaled to fit r.
func (p *Painter) Image(src ImageSource, r Rect, fit Fit) {
	switch s := src.(type) {
	case *Bitmap:
		if s != nil && s.w > 0 && s.h > 0 {
			p.drawBitmap(s, r, fit, [2]float32{}, [4]float32{}, false)
		}
	case *SVG:
		p.drawSVG(s, r, fit, [2]float32{}, [4]float32{}, p.Theme().Text, false)
	}
}

// ImageOptions are how Painter.ImageRegion draws a part of a bitmap.
type ImageOptions struct {
	// FlipX mirrors the part left to right, FlipY top to bottom.
	FlipX, FlipY bool
	// Tint multiplies its colors and alpha, as a colored filter would;
	// zero for none.
	Tint Color
}

// ImageRegion draws the part src of bitmap b, in its pixels (empty for all
// of it), stretched into dst, as Image draws a bitmap (pixelated if it
// is), unless dst is outside what the painter shows. Nothing of the bitmap
// around src shows, so that the parts of a bitmap holding many, as the
// icons of a sheet or the frames of an animation, draw on their own. Under
// a transform (Painter.Transform), dst is where it is, not snapped to
// pixels, so that a part moving through a zoomed view glides:
//
//	p.ImageRegion(icons, ui.Rect{X: 32, W: 16, H: 16}, box, ui.ImageOptions{})
func (p *Painter) ImageRegion(b *Bitmap, src, dst Rect, o ImageOptions) {
	if b == nil || b.w == 0 || b.h == 0 || dst.W <= 0 || dst.H <= 0 || !p.visible(dst, 1) {
		return
	}
	if src.W <= 0 || src.H <= 0 {
		src = Rect{W: float32(b.w), H: float32(b.h)}
	}
	// Far smaller than its pixels, a level of the bitmap, as smooth.
	shrink := min(src.W/(dst.W*p.scale), src.H/(dst.H*p.scale))
	shown := b.smaller(shrink)
	// Pixels of the bitmap to those of the level shown, whose size is its
	// image's, larger than the bitmap for the part of a canvas an effect
	// gets (Canvas.region).
	kx, ky := float32(shown.W)/float32(b.img.W), float32(shown.H)/float32(b.img.H)
	sr := scene.Rect{X: src.X * kx, Y: src.Y * ky, W: src.W * kx, H: src.H * ky}
	if o.FlipX {
		sr.X, sr.W = sr.X+sr.W, -sr.W
	}
	if o.FlipY {
		sr.Y, sr.H = sr.Y+sr.H, -sr.H
	}
	var tint scene.Color
	if o.Tint != (Color{}) {
		tint = o.Tint.scene()
	}
	// Snapped to the pixels of the screen, unless a transform places it:
	// snapping before a zoom would move a part in steps of several
	// pixels, where pixelated sampling glides.
	rect := p.snap(dst)
	if p.transform.Set {
		rect = scene.Rect{X: dst.X * p.scale, Y: dst.Y * p.scale, W: dst.W * p.scale, H: dst.H * p.scale}
	}
	p.add(scene.Op{Kind: scene.OpImage, Rect: rect, Image: shown, Src: sr, Opacity: p.opacity, Color: tint,
		Pixelated: b.pixelated && shrink <= 1})
}

// FocusRing draws the ring that shows the keyboard focus around r, whose
// corners are rounded by radius, as the theme draws it: FocusRingWidth
// wide, FocusRingOffset outside r, in Focus.
func (p *Painter) FocusRing(r Rect, radius [4]float32) {
	t := p.Theme()
	p.ring(r, radius, t.FocusRingOffset, t.focusRingWidth(), t.Focus)
}

// focusRingOf draws the focus ring of e around r, a part of e or its box,
// with the offset and the color e sets (FocusRingOffset, FocusRingColor),
// else the theme's.
func (p *Painter) focusRingOf(e *node, r Rect, radius [4]float32) {
	t := p.Theme()
	off, color := t.FocusRingOffset, t.Focus
	if e.ringSet&ringOffsetSet != 0 {
		off = e.ringOffset
	}
	if e.ringSet&ringColorSet != 0 {
		color = e.ringColor
	}
	p.ring(r, radius, off, t.focusRingWidth(), color)
}

// ring draws a ring w wide whose inner edge is off outside r, inside for a
// negative off, its corners following radius as CSS's outline does.
func (p *Painter) ring(r Rect, radius [4]float32, off, w float32, color Color) {
	d := off + w
	o := Rect{r.X - d, r.Y - d, r.W + 2*d, r.H + 2*d}
	if o.W <= 0 || o.H <= 0 {
		return
	}
	for i := range radius {
		radius[i] = max(0, radius[i]+d)
	}
	p.fill(o, radius, Color{}, w, color)
}

// Clip restricts what fn draws to r.
func (p *Painter) Clip(r Rect, radius float32, fn func()) {
	saved := p.clip
	p.pushClip(r, [4]float32{radius, radius, radius, radius})
	fn()
	p.popClip()
	p.clip = saved
}

// Transform transforms what fn draws by t around the point (ox, oy), which
// t leaves where it is, after the painter's own transforms: it nests. A
// view that pans and zooms, as a map or a diagram, draws its content
// through one, and a dial turns its needle around its center with another:
//
//	view := ui.Translation(-panX, -panY).Then(ui.Scaling(zoom, zoom))
//	p.Transform(view, r.X, r.Y, func() {
//		drawDiagram(p)
//		p.Transform(ui.Rotation(angle), cx, cy, func() { p.Line(cx, cy, cx, cy-40, 2, red) })
//	})
//
// Clips made in fn turn with what it draws, and what the transform moves
// out of view is culled as usual. Input is not transformed: the pointer
// comes in the element's coordinates, which the app maps to its content
// itself.
func (p *Painter) Transform(t Transform, ox, oy float32, fn func()) {
	saved := p.transform
	if t.set() {
		// Flattened around its own origin, then moved there in 2D, as an
		// element's (transformAt): the product keeps the divisor's sign,
		// so that a card turning over in perspective around (ox, oy) far
		// from the window's origin is in front of the viewer, though the
		// window's origin is behind it (#1406).
		p.transform = saved.Mul(scene.Translation(ox, oy).Mul(t.flat()).Mul(scene.Translation(-ox, -oy)))
	}
	fn()
	p.transform = saved
}
