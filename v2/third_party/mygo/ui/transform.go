package ui

import (
	"math"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/raster"
	"github.com/egoist/mygo/internal/scene"
)

// Transform is a visual transform, in 2D, or in 3D seen in perspective.
// Its zero value is identity. It changes drawing and interaction, leaving
// the space reserved by layout unchanged. Build one with Translation,
// Scaling, Rotation and Skewing, and in 3D TranslationZ, Translation3D,
// RotationX, RotationY and Perspective, and compose them with Then.
// Elements inherit their ancestors' transforms; each flattens what its 3D
// transform makes of it into its parent's plane, as CSS's flat
// transform-style.
type Transform struct {
	m  scene.Affine
	d3 *mat4 // a transform in 3D, which m is not
}

// mat4 is a transform in 3D, by rows, of (x, y, z, 1).
type mat4 [16]float32

func identity4() mat4 { return mat4{0: 1, 5: 1, 10: 1, 15: 1} }

// mat4 returns t in 3D.
func (t Transform) mat4() mat4 {
	if t.d3 != nil {
		return *t.d3
	}
	m := identity4()
	if a := t.m; a.Set {
		m[0], m[1], m[3] = a.A, a.C, a.X
		m[4], m[5], m[7] = a.B, a.D, a.Y
		m[12], m[13] = a.G, a.H
		if a.Behind {
			m[15] = -1
		}
	}
	return m
}

// set reports whether t is not identity.
func (t Transform) set() bool { return t.m.Set || t.d3 != nil }

// flat returns what t makes of its plane, z = 0, seen from the front.
// Elements and Painter.Transform flatten their transforms around their
// own origins and move them where they act in 2D (scene.Affine.Mul).
func (t Transform) flat() scene.Affine {
	if t.d3 == nil {
		return t.m
	}
	m := t.d3
	// k is the origin's divisor, its sign kept (scene.Affine.Behind).
	// Negative, the origin is behind the viewer, though what the plane
	// shows around it may be in front: a turn in perspective moved far
	// from the window's origin puts that origin past the turned plane's
	// horizon (#1406). With all of the plane behind the viewer, where a Z
	// translation as far as Perspective's distance puts a plane not
	// turned, or with the origin at the viewer, nothing shows, as in CSS.
	k := m[15]
	if k == 0 || math.IsNaN(float64(k)) || k < 0 && m[12] == 0 && m[13] == 0 {
		return scene.Affine{Set: true} // singular: nothing shows
	}
	behind := k < 0
	k = float32(math.Abs(float64(k)))
	v := [8]float32{m[0] / k, m[4] / k, m[1] / k, m[5] / k, m[3] / k, m[7] / k, m[12] / k, m[13] / k}
	for _, n := range v {
		if math.IsInf(float64(n), 0) || math.IsNaN(float64(n)) {
			return scene.Affine{Set: true}
		}
	}
	a := scene.Projective(v[0], v[1], v[2], v[3], v[4], v[5], v[6], v[7])
	if behind {
		if !a.Perspective() {
			return scene.Affine{Set: true}
		}
		a.Behind = true
	}
	return a
}

// in3D returns m as a Transform.
func in3D(m mat4) Transform { return Transform{d3: &m} }

// Translation moves content by x and y DIPs.
func Translation(x, y float32) Transform {
	finiteTransform(x, y)
	scene.FoldTranslations() // renderers draw them as untransformed content
	return Transform{m: scene.Translation(x, y)}
}

// TranslationZ moves content z DIPs along the axis toward the viewer, as
// CSS's translateZ(): with Perspective after it, nearer for positive z,
// looking larger, and farther for negative z, looking smaller, by
// distance/(distance-z) around the transform's origin. Without
// Perspective it changes nothing that shows, as in CSS. Content moved as
// far as Perspective's distance, or past it, is behind the viewer and
// shows nothing.
//
//	// 120 DIPs behind the plane, seen from 800 DIPs: 800/920 its size.
//	ui.Box(c).Size(240, 160).Transform(ui.TranslationZ(-120).Then(ui.Perspective(800)))
func TranslationZ(z float32) Transform { return Translation3D(0, 0, z) }

// Translation3D moves content by x and y DIPs in its plane and z toward
// the viewer, as CSS's translate3d(): Translation and TranslationZ.
func Translation3D(x, y, z float32) Transform {
	finiteTransform(x, y, z)
	if z == 0 {
		return Translation(x, y)
	}
	if x != 0 || y != 0 {
		scene.FoldTranslations() // without Perspective, it only moves
	}
	m := identity4()
	m[3], m[7], m[11] = x, y, z
	return in3D(m)
}

// Scaling scales each axis; zero collapses it and negative values reflect it.
func Scaling(x, y float32) Transform {
	finiteTransform(x, y)
	return Transform{m: scene.Scaling(x, y)}
}

// Rotation rotates clockwise by degrees in the window's coordinate system.
func Rotation(degrees float32) Transform {
	finiteTransform(degrees)
	return Transform{m: scene.Rotation(degrees)}
}

// Skewing slants content by x degrees along the horizontal axis and y
// along the vertical, as CSS's skew(x, y).
func Skewing(x, y float32) Transform {
	finiteTransform(x, y)
	tx, ty := math.Tan(float64(x)*math.Pi/180), math.Tan(float64(y)*math.Pi/180)
	finiteTransform(float32(tx), float32(ty))
	return Transform{m: scene.Matrix(1, float32(ty), float32(tx), 1, 0, 0)}
}

// RotationX turns content in 3D around the horizontal axis through its
// origin by degrees, its top going away for positive degrees, as CSS's
// rotateX. Seen without Perspective after it, it only looks shorter.
func RotationX(degrees float32) Transform {
	finiteTransform(degrees)
	s, c := math.Sincos(float64(degrees) * math.Pi / 180)
	m := identity4()
	m[5], m[6], m[9], m[10] = float32(c), -float32(s), float32(s), float32(c)
	return in3D(m)
}

// RotationY turns content in 3D around the vertical axis through its
// origin by degrees, its right side going away for positive degrees, as
// CSS's rotateY.
func RotationY(degrees float32) Transform {
	finiteTransform(degrees)
	s, c := math.Sincos(float64(degrees) * math.Pi / 180)
	m := identity4()
	m[0], m[2], m[8], m[10] = float32(c), float32(s), -float32(s), float32(c)
	return in3D(m)
}

// Perspective sees what the transforms before it put in 3D from distance
// DIPs in front of the origin, as CSS's perspective(): the shorter the
// distance, the deeper it looks; what goes away looks smaller. A distance
// of 800 or so looks natural for a card turning over.
func Perspective(distance float32) Transform {
	finiteTransform(distance)
	if distance <= 0 {
		panic("ui: Perspective's distance must be positive")
	}
	m := identity4()
	m[14] = -1 / distance
	return in3D(m)
}

// Then applies t first, then next.
func (t Transform) Then(next Transform) Transform {
	if t.d3 == nil && next.d3 == nil {
		m := next.m.Mul(t.m)
		finiteTransform(m.A, m.B, m.C, m.D, m.X, m.Y, m.G, m.H)
		return Transform{m: m}
	}
	a, b := next.mat4(), t.mat4()
	var p mat4
	for i := range 4 {
		for j := range 4 {
			p[4*i+j] = a[4*i]*b[j] + a[4*i+1]*b[4+j] + a[4*i+2]*b[8+j] + a[4*i+3]*b[12+j]
		}
	}
	finiteTransform(p[:]...)
	return in3D(p)
}

// Transform sets the element's visual transform, in place of the one it
// had, TranslatePercent's included. Its origin is the center of its
// border box unless TransformOrigin changes it.
func (e *node) Transform(t Transform) *node { e.transform, e.translatePct = t, [2]float32{}; return e }

// Translate moves the element after its existing transform.
func (e *node) Translate(x, y float32) *node {
	e.transform = e.transform.Then(Translation(x, y))
	return e
}

// TranslatePercent moves the element by x and y percent of its own width
// and height, as CSS's translate(-50%, -50%) does: of its border box as
// the frame lays it out, so that it is in place from its first frame and
// follows its size as it changes. It acts after the element's other
// transforms, wherever it is called among them, and before its pose
// (AnimatePose), and adds to a TranslatePercent before it. A callout
// centered above a point:
//
//	ui.Box(c).Absolute().Left(p.X).Top(p.Y).TranslatePercent(-50, -100)
func (e *node) TranslatePercent(x, y float32) *node {
	finiteTransform(x, y)
	scene.FoldTranslations() // renderers draw them as untransformed content
	e.translatePct[0] += x
	e.translatePct[1] += y
	return e
}

// Scale scales the element after its existing transform, around its origin.
func (e *node) Scale(x, y float32) *node {
	e.transform = e.transform.Then(Scaling(x, y))
	return e
}

// TransformOrigin sets the origin as fractions of the border box:
// (0,0) is the top-left and (0.5,0.5) the center. Values outside the box
// are allowed. Translation does not depend on the origin.
func (e *node) TransformOrigin(x, y float32) *node {
	finiteTransform(x, y)
	e.originX, e.originY, e.originSet = x, y, true
	return e
}

// Skew slants the element after its existing transform, around its
// origin, by x degrees along the horizontal axis and y along the vertical.
func (e *node) Skew(x, y float32) *node {
	e.transform = e.transform.Then(Skewing(x, y))
	return e
}

// TranslateZ moves the element z DIPs toward the viewer after its
// existing transform, as TranslationZ: follow it with Perspective to see
// it nearer or farther. A card lifting toward the viewer as it turns over:
//
//	card.RotateY(turn).TranslateZ(lift).Perspective(800)
func (e *node) TranslateZ(z float32) *node {
	e.transform = e.transform.Then(TranslationZ(z))
	return e
}

// RotateX turns the element in 3D around the horizontal axis through its
// origin, after its existing transform, as RotationX: follow it with
// Perspective to see it in depth.
//
//	card.RotateX(30).Perspective(800)
func (e *node) RotateX(degrees float32) *node {
	e.transform = e.transform.Then(RotationX(degrees))
	return e
}

// RotateY turns the element in 3D around the vertical axis through its
// origin, after its existing transform, as RotationY: a card turning over
// shows its back past 90 degrees, mirrored.
//
//	card.RotateY(flip * 180).Perspective(800)
func (e *node) RotateY(degrees float32) *node {
	e.transform = e.transform.Then(RotationY(degrees))
	return e
}

// Perspective sees the element's transform in 3D from distance DIPs in
// front of its origin (Perspective).
func (e *node) Perspective(distance float32) *node {
	e.transform = e.transform.Then(Perspective(distance))
	return e
}

// BackfaceHidden hides the element, and takes it from the pointer, while
// its 3D transform turns its back to the viewer, as CSS's
// backface-visibility: hidden: the two faces of a card turning over, the
// back turned 180 degrees more than the front, show one at a time.
//
//	front.RotateY(turn).Perspective(800).BackfaceHidden()
//	back.RotateY(turn + 180).Perspective(800).BackfaceHidden()
func (e *node) BackfaceHidden() *node { e.backface = true; return e }

// facesAway reports whether t turns the front of its plane, which faces
// the viewer, away: whether its normal, by the inverse transpose of t,
// points away. The inverse holds t's perspective, so that a plane moved
// off its origin, as by TranslationZ before a turn, faces where the viewer
// sees it from, as Chromium's IsBackFaceVisible has it: the item at the
// side of a carousel turns its back as it passes the viewer's line.
func (t Transform) facesAway() bool {
	if t.d3 == nil {
		return false
	}
	var m [16]float64
	for i, v := range t.d3 {
		m[i] = float64(v)
	}
	det3 := func(r0, r1, r2, c0, c1, c2 int) float64 {
		a := func(r, c int) float64 { return m[4*r+c] }
		return a(r0, c0)*(a(r1, c1)*a(r2, c2)-a(r1, c2)*a(r2, c1)) -
			a(r0, c1)*(a(r1, c0)*a(r2, c2)-a(r1, c2)*a(r2, c0)) +
			a(r0, c2)*(a(r1, c0)*a(r2, c1)-a(r1, c1)*a(r2, c0))
	}
	// The determinant, along the last column; the inverse's entry at row
	// and column 2 is the cofactor there over it.
	det := -m[3]*det3(1, 2, 3, 0, 1, 2) + m[7]*det3(0, 2, 3, 0, 1, 2) - m[11]*det3(0, 1, 3, 0, 1, 2) + m[15]*det3(0, 1, 2, 0, 1, 2)
	if det == 0 {
		return false
	}
	return det3(0, 1, 3, 0, 1, 3)*det < 0
}

func finiteTransform(v ...float32) {
	for _, n := range v {
		if math.IsNaN(float64(n)) || math.IsInf(float64(n), 0) {
			panic("ui: transform values must be finite")
		}
	}
}

func (e *node) transformAt(x, y float32) scene.Affine {
	// Its pose (AnimatePose) acts after its own transform, which ends
	// with its TranslatePercent.
	m := e.transform.flat()
	if p := e.translatePct; p != [2]float32{} {
		m = scene.Translation(p[0]/100*e.w, p[1]/100*e.h).Mul(m)
	}
	m = e.poseM.Mul(m)
	if !m.Set {
		return scene.Affine{}
	}
	ox, oy := float32(0.5), float32(0.5)
	if e.originSet {
		ox, oy = e.originX, e.originY
	}
	x, y = x+ox*e.w, y+oy*e.h
	return scene.Translation(x, y).Mul(m).Mul(scene.Translation(-x, -y))
}

func (e *node) visualRect(r Rect) Rect { return transformRect(e.world, r) }
func transformRect(m scene.Affine, r Rect) Rect {
	b := m.Bounds(scene.Rect{X: r.X, Y: r.Y, W: r.W, H: r.H})
	return Rect{b.X, b.Y, b.W, b.H}
}

// LayoutBounds returns the previous frame's border box before transforms,
// in the layout's window coordinates. Draw callbacks use these coordinates.
func (e *node) LayoutBounds() Rect {
	if !e.hasState() {
		return Rect{}
	}
	s := e.st
	return Rect{s.x, s.y, s.w, s.h}
}

// LocalToWindow maps a point in the previous frame's border box to the window.
func (e *node) LocalToWindow(x, y float32) (float32, float32) {
	if !e.hasState() {
		return 0, 0
	}
	return e.st.world.Point(e.st.x+x, e.st.y+y)
}

// WindowToLocal maps a window point into the previous frame's border box.
// ok is false for singular transforms, which take no pointer input.
func (e *node) WindowToLocal(x, y float32) (lx, ly float32, ok bool) {
	if !e.hasState() {
		return 0, 0, false
	}
	m, ok := e.st.world.Inverse()
	if !ok {
		return 0, 0, false
	}
	x, y = m.Point(x, y)
	return x - e.st.x, y - e.st.y, true
}

func (s *state) local(x, y float32) (float32, float32) {
	m, ok := s.world.Inverse()
	if !ok {
		return 0, 0
	}
	x, y = m.Point(x, y)
	return x - s.x, y - s.y
}

// vector returns the move by (dx, dy) of the window's point (x, y) in the
// element's axes: the same everywhere, unless in perspective.
func (s *state) vector(x, y, dx, dy float32) (float32, float32) {
	m, ok := s.world.Inverse()
	if !ok {
		return 0, 0
	}
	return m.VectorAt(x, y, dx, dy)
}

// contains tests the actual transformed box, where it takes the pointer
// (HitTest) and every ancestor's clip, rather than the conservative
// rectangles used for accessibility and culling.
func (rt *engine) contains(s *state, r Rect, x, y float32) bool {
	m, ok := s.world.Inverse()
	if !ok {
		return false
	}
	lx, ly := m.Point(x, y)
	if !r.Contains(lx, ly) || s.selfClips && !roundedContains(s.selfClip, uniformRadii(s.selfRadius), lx, ly) ||
		s.hitFn != nil && !s.hitFn(lx, ly, Rect{s.x, s.y, s.w, s.h}) {
		return false
	}
	// The clips around an element flying stop at it, as it paints out of
	// them.
	for p := rt.states[s.parent]; p != nil && !s.lifted; p = rt.states[p.parent] {
		if p.clips || p.selfClips {
			m, ok := p.world.Inverse()
			if !ok {
				return false
			}
			px, py := m.Point(x, y)
			if p.clips && !roundedContains(p.clipRect, p.clipRadii, px, py) ||
				p.selfClips && !roundedContains(p.selfClip, uniformRadii(p.selfRadius), px, py) {
				return false
			}
		}
		if p.lifted || p.parent == 0 {
			break
		}
	}
	w, h, _ := rt.size()
	return x >= 0 && y >= 0 && x < w && y < h
}

func uniformRadii(r float32) [4]float32 { return [4]float32{r, r, r, r} }

func roundedContains(r Rect, radii [4]float32, x, y float32) bool {
	if !r.Contains(x, y) {
		return false
	}
	return raster.Contains(scene.Rect{X: r.X, Y: r.Y, W: r.W, H: r.H}, radii, continuousCorners, x, y)
}

func accessBounds(e *node) platform.RectF {
	r := e.visualRect(Rect{e.x, e.y, e.w, e.h})
	return platform.RectF{X: float64(r.X), Y: float64(r.Y), W: float64(r.W), H: float64(r.H)}
}

func laidOutVisualBox(e *node) Rect {
	b := laidOutBox(e)
	if e.st.seen != e.c.rt.frame {
		return e.Bounds()
	}
	var world func(e *node) scene.Affine
	world = func(e *node) scene.Affine {
		x, y := laidOutOrigin(e)
		if e.isInline() {
			r := laidOutBox(e)
			x, y = r.X, r.Y
		}
		m := e.transformAt(x, y)
		if e.parent != nil && e != e.c.overlay {
			m = world(e.parent).Mul(m)
		}
		return m
	}
	return transformRect(world(e), b)
}

func (p *Painter) add(op scene.Op) {
	op.Transform = p.transform.Pixels(p.scale).Mul(op.Transform)
	p.s.Ops = append(p.s.Ops, op)
}

func (p *Painter) inlineTransforms() bool {
	if p.inline == nil {
		return false
	}
	var walk func(*node) bool
	walk = func(e *node) bool {
		for ch := e.first; ch != nil; ch = ch.next {
			if ch.transform.set() || ch.translatePct != [2]float32{} || walk(ch) {
				return true
			}
		}
		return false
	}
	return walk(p.inline)
}

func (p *Painter) inlineTransform(runeIndex int) scene.Affine {
	if p.inline == nil {
		return scene.Affine{}
	}
	var find func(e *node, offset int) *node
	find = func(e *node, offset int) *node {
		for ch := e.first; ch != nil; ch = ch.next {
			if runeIndex >= offset+ch.runes[0] && runeIndex < offset+ch.runes[1] {
				return find(ch, offset+ch.runes[0])
			}
		}
		return e
	}
	e := find(p.inline, 0)
	inv, ok := p.inline.world.Inverse()
	if !ok {
		return scene.Scaling(0, 0)
	}
	return inv.Mul(e.world)
}

// Inline glyphs can move into view even when their paragraph's layout box
// is out of view. Their ancestors' clips still apply when they paint.
func (p *Painter) inlineVisible(e *node) bool {
	for ch := e.first; ch != nil; ch = ch.next {
		for _, f := range ch.frags {
			if v := intersect(ch.visualRect(f), p.clip); v.W > 0 && v.H > 0 {
				return true
			}
		}
		if p.inlineVisible(ch) {
			return true
		}
	}
	return false
}
