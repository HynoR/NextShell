package ui

import (
	"math"
	"slices"
	"time"

	"github.com/egoist/mygo/internal/scene"
)

// Shared elements: a Transition's Match moves an element appearing from
// where the element of the same Match showed in the frame before, from its
// box and in its colors, laid out at each box on the way (or scaled,
// ScaleContent). With Fly, it flies there, as Keynote's Magic Move: it
// goes from that one's look to its own too (matchLook), and from the
// transform of the elements around that one to its own's, over the rest
// of its layer, the window's content or its overlay, out of its
// ancestors' clips (matches.lifted); the other, flying too, goes with it,
// fading out under it: a copy of it once gone, rather than as its Exit
// says, or itself while it is still built, as in a page going away.

// matcher moves the elements of a Match (engine.matches): Transition gives
// the engine one the first time it is given a Match or ScaleContent, so
// that apps without one link none of it.
type matcher interface {
	// claim notes the colors of the elements gone that elements of their
	// Match appearing come from, and takes those flying with them, before
	// animateExits forgets them or copies them for their Exit; move moves
	// the elements on their way, once the frame is placed.
	claim()
	move()
	// paint paints the elements flying in layer, an overlay, or the
	// window's content for nil, over it.
	paint(p *Painter, layer *node)
	// scale shows e, as laid out, in a box w×h where it is, scaled
	// (ScaleContent).
	scale(e *node, w, h float32)
}

// matches is the engine's matcher: where each Match showed last, and the
// elements of a Match on their way, by ID (transition.matching).
type matches struct {
	rt      *engine
	matched map[any]*matchRec
	flights map[uint64]*flight
	// lifted are the elements flying, which paint over the rest of their
	// layer, in the order move moved them: parents first, each over the
	// element it came from.
	lifted []flier
	// gone are the elements that elements of their Match started from in
	// this frame and fly with, and to are where the layout put the
	// elements of the frame's uses.
	gone []uint64
	to   []Rect
	// byID maps the elements of the frame's uses with a Match, in frame
	// byIDFrame, once builtWith needs it; spare are records forgotten,
	// which records reuse.
	byID      map[uint64]*node
	byIDFrame uint64
	spare     []*matchRec
	// fresh tells that claim found an element of a Match appearing where
	// the element of its Match showed, which starts on its way, and noted
	// counts the records noted in the frame: as many as there are, the
	// next frame forgets none.
	fresh bool
	noted int
}

// flier is an element flying over layer (layerOf).
type flier struct{ e, layer *node }

// linkMatches gives the engine its matcher.
func (rt *engine) linkMatches() {
	if rt.matches == nil {
		rt.matches = &matches{rt: rt, matched: map[any]*matchRec{}, flights: map[uint64]*flight{}}
	}
}

// flight is an element of a Match on its way, of transition r, as of the
// last frame it was built: its box in the window, from the box the
// element it came from had. Flying (fly), look goes from 0 to 1: its look
// from lookFrom to its own, lookTo, the transform of the elements around
// it from worldFrom, that of those around the other, to its own, and the
// crossfade. That element goes with it, fading: src, a copy of it gone,
// at srcAlpha and turned by srcTurn, or else the element of srcID while it
// is built; morph moves the characters of its text from that one's
// instead (ContentTransition's Morph). Gone with the elements around it,
// it flies on as copy, its copy in theirs, the copy of the element of
// transition copyIn, going as its Exit says (flyOn).
type flight struct {
	r         *transition
	box       [4]channel
	fly       bool
	morph     bool
	look      channel
	lookFrom  matchLook
	lookTo    matchLook
	worldFrom scene.Affine
	src       *node
	srcID     uint64
	srcAlpha  float32
	srcTurn   Transform
	copy      *node
	copyIn    uint64
	// shift is the translation the element shows with in this frame, and
	// what is inside it (pin).
	shift Point
}

// matchRec is where the element of the Match key showed in a frame, in
// the window: node, in the frame's arena, which the next frame keeps when
// kept (engine.exitsBuilt), for its look and the transform of the
// elements around it, unless it flew (lookSet), showing look under world.
// gone is a copy of it once it went, which the element flying in its place
// takes (claim), and goneColors the colors it showed.
type matchRec struct {
	key        any
	frame, id  uint64
	box        Rect
	node       *node
	kept       bool
	lookSet    bool
	look       matchLook
	world      scene.Affine
	gone       *node
	goneAlpha  float32
	goneColors [2]Color
	// taker is the element of the Match on its way in frame takenAt.
	taker   *node
	takenAt uint64
}

// matchLook is how an element of a Match looks besides its box and its
// colors: its corners, its opacity, its text's color and its transform.
type matchLook struct {
	radius  [4]float32
	opacity float32
	text    Color
	turn    Transform
}

// lookOf returns how e looks, once its parents are built.
func lookOf(e *node) matchLook {
	l := matchLook{radius: e.radius, opacity: 1, text: e.resolvedText().color, turn: e.transform}
	if e.opacitySet {
		l.opacity = e.opacity
	}
	return l
}

// mixLook returns the look k of the way from a to b.
func mixLook(a, b matchLook, k float32, lab bool) matchLook {
	m := matchLook{opacity: max(0, min(lerp(a.opacity, b.opacity, k), 1)), text: b.text, turn: mixTransform(a.turn, b.turn, k)}
	for i := range m.radius {
		m.radius[i] = max(lerp(a.radius[i], b.radius[i], k), 0)
	}
	if a.text != b.text {
		m.text = mixPremultiplied(a.text, b.text, max(0, min(k, 1)), lab)
	}
	return m
}

// mixTransform returns the transform k of the way from a to b, as CSS
// interpolates them: in 2D, through their translations, rotations, skews
// and scales; in 3D or in perspective, through their perspectives,
// translations, rotations (as quaternions), skews and scales. Rotations
// turn the short way round. One that cannot be decomposed, flattened to
// nothing, is a's until halfway, then b's.
func mixTransform(a, b Transform, k float32) Transform {
	switch {
	case !a.set() && !b.set() || k >= 1:
		return b
	case k <= 0:
		return a
	case a.d3 != nil || b.d3 != nil || a.m.G != 0 || a.m.H != 0 || b.m.G != 0 || b.m.H != 0:
		da, okA := decompose3(a.mat4())
		db, okB := decompose3(b.mat4())
		if !okA || !okB {
			if k < 0.5 {
				return a
			}
			return b
		}
		return in3D(recompose3(mix3(da, db, float64(k))))
	}
	da, db := decompose(a.m), decompose(b.m)
	turn := db[2] - da[2]
	if turn > math.Pi {
		turn -= 2 * math.Pi
	} else if turn < -math.Pi {
		turn += 2 * math.Pi
	}
	var d [6]float64
	for i := range d {
		d[i] = da[i] + (db[i]-da[i])*float64(k)
	}
	d[2] = da[2] + turn*float64(k)
	sin, cos := math.Sincos(d[2])
	sx, sy, skew := d[3], d[4], d[5]
	return Transform{m: scene.Matrix(float32(cos*sx), float32(sin*sx), float32(sy*(cos*skew-sin)), float32(sy*(sin*skew+cos)), float32(d[0]), float32(d[1]))}
}

// decomposed3 is a transform in 3D taken apart as CSS takes it: m is the
// perspective, of the translation, of the rotation (quaternion x, y, z,
// w), of the skew (xy, xz, yz), of the scale.
type decomposed3 struct {
	perspective [4]float64
	translate   [3]float64
	quaternion  [4]float64
	skew        [3]float64
	scale       [3]float64
}

// decompose3 takes m apart, and reports whether it could: one that
// flattens space cannot be.
func decompose3(m mat4) (d decomposed3, ok bool) {
	var M [16]float64
	for i, v := range m {
		M[i] = float64(v)
	}
	if M[15] == 0 {
		return d, false
	}
	for i := range M {
		M[i] /= M[15]
	}
	// M is the perspective, its last row, of the affine part, A and t.
	A := [3][3]float64{{M[0], M[1], M[2]}, {M[4], M[5], M[6]}, {M[8], M[9], M[10]}}
	inv, ok := inverse3(A)
	if !ok {
		return d, false
	}
	d.translate = [3]float64{M[3], M[7], M[11]}
	d.perspective = [4]float64{0, 0, 0, 1}
	if M[12] != 0 || M[13] != 0 || M[14] != 0 {
		// The last row of M is the perspective's times the affine part's.
		r := [3]float64{M[12], M[13], M[14]}
		for j := range 3 {
			d.perspective[j] = r[0]*inv[0][j] + r[1]*inv[1][j] + r[2]*inv[2][j]
		}
		d.perspective[3] = M[15] - (d.perspective[0]*d.translate[0] + d.perspective[1]*d.translate[1] + d.perspective[2]*d.translate[2])
	}
	// A is the rotation of the skew of the scale: its columns, made
	// orthonormal in turn, give the scale and the skews.
	var c [3][3]float64
	for j := range 3 {
		c[j] = [3]float64{A[0][j], A[1][j], A[2][j]}
	}
	d.scale[0] = length3(c[0])
	c[0] = scale3(c[0], 1/d.scale[0])
	d.skew[0] = dot3(c[0], c[1])
	c[1] = combine3(c[1], c[0], -d.skew[0])
	d.scale[1] = length3(c[1])
	c[1] = scale3(c[1], 1/d.scale[1])
	d.skew[0] /= d.scale[1]
	d.skew[1] = dot3(c[0], c[2])
	c[2] = combine3(c[2], c[0], -d.skew[1])
	d.skew[2] = dot3(c[1], c[2])
	c[2] = combine3(c[2], c[1], -d.skew[2])
	d.scale[2] = length3(c[2])
	c[2] = scale3(c[2], 1/d.scale[2])
	d.skew[1] /= d.scale[2]
	d.skew[2] /= d.scale[2]
	if dot3(c[0], cross3(c[1], c[2])) < 0 {
		// Mirrored: negative scales.
		for i := range 3 {
			d.scale[i] = -d.scale[i]
			c[i] = scale3(c[i], -1)
		}
	}
	// The rotation's quaternion, from its largest component, which holds
	// at any angle (Shepperd's), as half a turn has no direction to sign
	// the others by.
	R := func(i, j int) float64 { return c[j][i] }
	q := &d.quaternion
	switch tr := R(0, 0) + R(1, 1) + R(2, 2); {
	case tr > 0:
		w := 0.5 * math.Sqrt(1+tr)
		*q = [4]float64{(R(2, 1) - R(1, 2)) / (4 * w), (R(0, 2) - R(2, 0)) / (4 * w), (R(1, 0) - R(0, 1)) / (4 * w), w}
	case R(0, 0) >= R(1, 1) && R(0, 0) >= R(2, 2):
		x := 0.5 * math.Sqrt(max(1+R(0, 0)-R(1, 1)-R(2, 2), 0))
		*q = [4]float64{x, (R(0, 1) + R(1, 0)) / (4 * x), (R(0, 2) + R(2, 0)) / (4 * x), (R(2, 1) - R(1, 2)) / (4 * x)}
	case R(1, 1) >= R(2, 2):
		y := 0.5 * math.Sqrt(max(1-R(0, 0)+R(1, 1)-R(2, 2), 0))
		*q = [4]float64{(R(0, 1) + R(1, 0)) / (4 * y), y, (R(1, 2) + R(2, 1)) / (4 * y), (R(0, 2) - R(2, 0)) / (4 * y)}
	default:
		z := 0.5 * math.Sqrt(max(1-R(0, 0)-R(1, 1)+R(2, 2), 0))
		*q = [4]float64{(R(0, 2) + R(2, 0)) / (4 * z), (R(1, 2) + R(2, 1)) / (4 * z), z, (R(1, 0) - R(0, 1)) / (4 * z)}
	}
	return d, true
}

// recompose3 puts d together.
func recompose3(d decomposed3) mat4 {
	x, y, z, w := d.quaternion[0], d.quaternion[1], d.quaternion[2], d.quaternion[3]
	R := [3][3]float64{
		{1 - 2*(y*y+z*z), 2 * (x*y - z*w), 2 * (x*z + y*w)},
		{2 * (x*y + z*w), 1 - 2*(x*x+z*z), 2 * (y*z - x*w)},
		{2 * (x*z - y*w), 2 * (y*z + x*w), 1 - 2*(x*x+y*y)},
	}
	K := [3][3]float64{{1, d.skew[0], d.skew[1]}, {0, 1, d.skew[2]}, {0, 0, 1}}
	var N [4][4]float64
	for i := range 3 {
		for j := range 3 {
			var v float64
			for l := range 3 {
				v += R[i][l] * K[l][j]
			}
			N[i][j] = v * d.scale[j]
		}
		N[i][3] = d.translate[i]
	}
	N[3][3] = 1
	var m mat4
	for i := range 3 {
		for j := range 4 {
			m[4*i+j] = float32(N[i][j])
		}
	}
	for j := range 4 {
		var v float64
		for i := range 4 {
			v += d.perspective[i] * N[i][j]
		}
		m[12+j] = float32(v)
	}
	return m
}

// mix3 returns the transform k of the way from a to b, its rotation
// turning the short way round.
func mix3(a, b decomposed3, k float64) decomposed3 {
	var d decomposed3
	for i := range 4 {
		d.perspective[i] = a.perspective[i] + (b.perspective[i]-a.perspective[i])*k
	}
	for i := range 3 {
		d.translate[i] = a.translate[i] + (b.translate[i]-a.translate[i])*k
		d.skew[i] = a.skew[i] + (b.skew[i]-a.skew[i])*k
		d.scale[i] = a.scale[i] + (b.scale[i]-a.scale[i])*k
	}
	qa, qb := a.quaternion, b.quaternion
	product := qa[0]*qb[0] + qa[1]*qb[1] + qa[2]*qb[2] + qa[3]*qb[3]
	if product < 0 {
		for i := range qb {
			qb[i] = -qb[i]
		}
		product = -product
	}
	wa, wb := 1-k, k
	if product < 0.9995 {
		theta := math.Acos(min(product, 1))
		sin := math.Sin(theta)
		wa, wb = math.Sin((1-k)*theta)/sin, math.Sin(k*theta)/sin
	}
	var n float64
	for i := range 4 {
		d.quaternion[i] = wa*qa[i] + wb*qb[i]
		n += d.quaternion[i] * d.quaternion[i]
	}
	n = math.Sqrt(n)
	for i := range 4 {
		d.quaternion[i] /= n
	}
	return d
}

// inverse3 returns the inverse of m, and reports whether it has one.
func inverse3(m [3][3]float64) (inv [3][3]float64, ok bool) {
	det := m[0][0]*(m[1][1]*m[2][2]-m[1][2]*m[2][1]) - m[0][1]*(m[1][0]*m[2][2]-m[1][2]*m[2][0]) + m[0][2]*(m[1][0]*m[2][1]-m[1][1]*m[2][0])
	if math.Abs(det) < 1e-12 {
		return inv, false
	}
	inv[0][0] = (m[1][1]*m[2][2] - m[1][2]*m[2][1]) / det
	inv[0][1] = (m[0][2]*m[2][1] - m[0][1]*m[2][2]) / det
	inv[0][2] = (m[0][1]*m[1][2] - m[0][2]*m[1][1]) / det
	inv[1][0] = (m[1][2]*m[2][0] - m[1][0]*m[2][2]) / det
	inv[1][1] = (m[0][0]*m[2][2] - m[0][2]*m[2][0]) / det
	inv[1][2] = (m[0][2]*m[1][0] - m[0][0]*m[1][2]) / det
	inv[2][0] = (m[1][0]*m[2][1] - m[1][1]*m[2][0]) / det
	inv[2][1] = (m[0][1]*m[2][0] - m[0][0]*m[2][1]) / det
	inv[2][2] = (m[0][0]*m[1][1] - m[0][1]*m[1][0]) / det
	return inv, true
}

func dot3(a, b [3]float64) float64 { return a[0]*b[0] + a[1]*b[1] + a[2]*b[2] }

func length3(a [3]float64) float64 { return math.Sqrt(dot3(a, a)) }

func scale3(a [3]float64, k float64) [3]float64 { return [3]float64{a[0] * k, a[1] * k, a[2] * k} }

// combine3 returns a plus k times b.
func combine3(a, b [3]float64, k float64) [3]float64 {
	return [3]float64{a[0] + k*b[0], a[1] + k*b[1], a[2] + k*b[2]}
}

func cross3(a, b [3]float64) [3]float64 {
	return [3]float64{a[1]*b[2] - a[2]*b[1], a[2]*b[0] - a[0]*b[2], a[0]*b[1] - a[1]*b[0]}
}

// decompose returns the translation, rotation (radians), scale and skew of
// m, in 2D: m is the translation of the rotation of the skew of the scale.
func decompose(m scene.Affine) [6]float64 {
	if !m.Set {
		return [6]float64{0, 0, 0, 1, 1, 0}
	}
	a, b, c, d := float64(m.A), float64(m.B), float64(m.C), float64(m.D)
	sx := math.Hypot(a, b)
	if sx == 0 {
		return [6]float64{float64(m.X), float64(m.Y), 0, 0, math.Hypot(c, d), 0}
	}
	a, b = a/sx, b/sx
	skew := a*c + b*d
	c, d = c-a*skew, d-b*skew
	sy := math.Hypot(c, d)
	if sy != 0 {
		skew /= sy
	}
	if a*d-b*c < 0 {
		// Mirrored: a negative scale along x.
		sx, a, b = -sx, -a, -b
	}
	return [6]float64{float64(m.X), float64(m.Y), math.Atan2(b, a), sx, sy, skew}
}

// worldAround returns the transform that the elements around e give it,
// from its parent's space to the window's, as commit sets it (world).
func worldAround(e *node) scene.Affine {
	var w scene.Affine
	for a := e.parent; a != nil; a = a.parent {
		w = a.transformAt(a.x, a.y).Mul(w)
		if a == a.c.overlay {
			break // which the root's does not move
		}
	}
	return w
}

// under shows e, which the elements around it transform by around, as if
// they transformed it by world: its own transform turned so, around its
// origin.
func under(e *node, world, around scene.Affine) {
	if world == around {
		return
	}
	inv, ok := around.Inverse()
	if !ok {
		return
	}
	ox, oy := float32(0.5), float32(0.5)
	if e.originSet {
		ox, oy = e.originX, e.originY
	}
	x, y := e.x+ox*e.w, e.y+oy*e.h
	d := scene.Translation(-x, -y).Mul(inv).Mul(world).Mul(scene.Translation(x, y))
	e.transform = e.transform.Then(Transform{m: d})
}

// layerOf returns the overlay e is in, or nil in the window's content.
func layerOf(e *node) *node {
	for a := e; a.parent != nil; a = a.parent {
		if a.parent == a.c.overlay {
			return a
		}
	}
	return nil
}

// scale shows e, as laid out, in a box w×h where it is: scaled, after its
// own transform, around its origin.
func (*matches) scale(e *node, w, h float32) {
	kx, ky := float32(1), float32(1)
	if e.w > 0 {
		kx = w / e.w
	}
	if e.h > 0 {
		ky = h / e.h
	}
	ox, oy := float32(0.5), float32(0.5)
	if e.originSet {
		ox, oy = e.originX, e.originY
	}
	e.transform = e.transform.Then(Transform{m: scene.Translation((kx-1)*ox*e.w, (ky-1)*oy*e.h).Mul(scene.Scaling(kx, ky))})
}

// The crossfade of an element flying, k of the way, with the one it came
// from, as Motion's: the element coming is opaque by half the way, and
// the other fades out over the second half, under it.
func fadeIn(k float32) float32 {
	t := max(0, min(k/0.5, 1)) - 1
	return float32(math.Sqrt(float64(1 - t*t)))
}

func fadeOut(k float32) float32 { return 1 - max(0, min((k-0.5)/0.45, 1)) }

// moves reports whether the elements of a Match move: not while the
// desktop asks for less motion, nor in a frame showing changes at once.
func (m *matches) moves() bool {
	return !m.rt.preferences().ReduceMotion && !m.rt.instant()
}

// use returns the transition of u, if it has a Match and is its element's
// in this frame.
func (m *matches) use(u transitionUse) *transition {
	r := u.e.st.trec
	if u.t.Match == nil || r == nil || r.elem != u.e || r.built != m.rt.frame {
		return nil
	}
	return r
}

// builtWith returns the element with id of the frame's uses, with a Match.
func (m *matches) builtWith(uses []transitionUse, id uint64) *node {
	if m.byIDFrame != m.rt.frame {
		// Mapped once a frame, as elements of a Match look up others.
		if m.byID == nil {
			m.byID = map[uint64]*node{}
		}
		clear(m.byID)
		for _, u := range uses {
			if m.use(u) != nil {
				m.byID[u.e.id] = u.e
			}
		}
		m.byIDFrame = m.rt.frame
	}
	return m.byID[id]
}

// record returns the record of the Match of u, of transition r, made if
// needed: the one r last had, while it is still its Match's.
func (m *matches) record(r *transition, u transitionUse) *matchRec {
	k := u.t.Match
	if rec, _ := r.match.(*matchRec); rec != nil && rec.key == k {
		return rec
	}
	rec := m.matched[k]
	if rec == nil {
		if n := len(m.spare); n > 0 {
			rec, m.spare = m.spare[n-1], m.spare[:n-1]
		} else {
			rec = &matchRec{}
		}
		rec.key = k
		m.matched[k] = rec
	}
	r.match = rec
	return rec
}

// shown returns the colors the element of transition r showed in the
// frame before, if it animates them, even forgotten as a route changed.
func (m *matches) shown(r *transition) [2]Color {
	if r == nil || r.colorFrame+1 != m.rt.frame {
		return [2]Color{}
	}
	return r.colorShown
}

// starts reports whether e, of transition r, appears where rec noted the
// element of its Match in the frame before.
func (m *matches) starts(r *transition, e *node, rec *matchRec) bool {
	return r.fresh && rec != nil && rec.id != e.id && rec.frame+1 == m.rt.frame
}

// morphs reports whether e moves the characters of its text from those
// of the element of its Match, from (ContentTransition's Morph).
func morphs(e, from *node) bool {
	return e.kind == kindText && e.first == nil && e.contentSet && e.content.Morph && morphText != nil &&
		from.kind == kindText && from.first == nil && from.tl != nil
}

func (m *matches) claim() {
	rt := m.rt
	if !m.moves() {
		return
	}
	for _, u := range rt.c.transitions {
		r := m.use(u)
		if r == nil || !r.fresh {
			continue
		}
		rec := m.matched[u.t.Match]
		if !m.starts(r, u.e, rec) {
			continue
		}
		m.fresh = true
		from := rt.trans[rec.id]
		if from == nil {
			continue
		}
		// Its colors, which the element appearing comes from.
		rec.goneColors = m.shown(from)
		if !u.t.Fly || !from.t.Fly || from.built == rt.frame || from.ghost != nil || from.elem == nil || from.laid+1 != rt.frame {
			continue // it goes as its Exit says, or, still built, flies itself
		}
		// Rather than go as its Exit says, it flies with the element,
		// copied from the frame before, whose arena the engine kept, as for
		// an Exit.
		delete(rt.trans, rec.id)
		e := from.elem
		if morphs(u.e, e) {
			continue // its characters move instead
		}
		g, alpha := ghostOf(e), float32(1)
		if g.opacitySet {
			alpha = g.opacity
		}
		g.leaving, g.flags = 2, g.flags|flagAbsolute
		// Painted out of the elements around it, it keeps the text style
		// and the theme they gave it.
		g.ts = e.resolvedText()
		if t := e.theme(); g.themed == nil && t != rt.c.window {
			theme := *t
			g.themed = &theme
		}
		rec.gone, rec.goneAlpha = g, alpha
	}
}

func (m *matches) move() {
	rt := m.rt
	if len(m.matched) > m.noted {
		// Those no element showed in the frame before.
		for k, rec := range m.matched {
			if rec.frame+1 < rt.frame {
				delete(m.matched, k)
				*rec = matchRec{}
				m.spare = append(m.spare, rec)
			}
		}
	}
	m.noted = 0
	clear(m.lifted)
	m.lifted = m.lifted[:0]
	uses := rt.c.transitions
	if len(m.flights) > 0 || m.fresh {
		uses = m.moveAll(uses, m.moves())
	}
	m.fresh = false
	// Each Match shows where its element on its way is, else where the
	// element that showed it last is, else the first built. Those of a
	// page going away (Router) that flew show only on the way.
	for _, u := range uses {
		r := m.use(u)
		if r == nil {
			continue
		}
		e := u.e
		rec := m.record(r, u)
		var holder *node
		switch {
		case rec.takenAt == rt.frame:
			holder = rec.taker
		case rec.id == e.id:
			holder = e
		case rec.id != 0 && rec.frame+1 >= rt.frame:
			holder = m.builtWith(uses, rec.id)
		}
		switch {
		case holder != nil && holder != e:
			if u.t.Fly && !e.lift && inert(e) {
				e.flags |= flagInvisible
			}
		case rec.takenAt != rt.frame:
			m.note(rec, e, Rect{e.x, e.y, e.w, e.h})
		}
	}
}

// inert reports whether e is in an element that shows but takes no input,
// as a page going away (Router).
func inert(e *node) bool {
	for a := e.parent; a != nil; a = a.parent {
		if a.flags&flagInert != 0 {
			return true
		}
	}
	return false
}

// moveAll starts the elements of uses appearing on their way, and moves
// those on their way, which moves tells to go on: parents first, in the
// order it returns uses, as one laid out anew places what is inside it.
func (m *matches) moveAll(uses []transitionUse, moves bool) []transitionUse {
	rt := m.rt
	c := rt.c
	uses = c.byDepth(uses)
	now := c.now
	// Each goes where the layout put it, as one on its way around it
	// moves it: on its own way, independently.
	m.to = m.to[:0]
	for _, u := range uses {
		m.to = append(m.to, Rect{u.e.x, u.e.y, u.e.w, u.e.h})
	}
	m.gone = m.gone[:0]
	for i, u := range uses {
		r := m.use(u)
		if r == nil {
			continue
		}
		e := u.e
		switch {
		case !moves:
			m.end(r, e.id)
		case r.fresh:
			if rec := m.matched[u.t.Match]; m.starts(r, e, rec) {
				m.start(r, u, rec, now)
			}
		}
		if r.matching {
			m.step(r, u, m.to[i], m.record(r, u), uses, now)
		}
	}
	if len(m.gone) > 0 {
		// Copies of the elements they came from, going away as elements
		// around them went, or flying with elements of a Match around them,
		// show them no more: they fly with those that came from them.
		for _, x := range rt.trans {
			if x.ghost != nil && x.built != rt.frame {
				hideCopies(x.ghost, m.gone)
			}
		}
		for _, f := range m.flights {
			if f.src != nil {
				hideCopies(f.src, m.gone)
			}
		}
	}
	for id, f := range m.flights {
		r := rt.trans[id]
		switch {
		case r != nil && r.built == rt.frame:
			if !r.matching {
				m.end(r, id) // there
			}
		case !m.flyOn(f, id, uses, now):
			m.end(r, id) // gone
		}
	}
	return uses
}

// note notes in rec that e showed its Match in the frame, at rest, in the
// window's box.
func (m *matches) note(rec *matchRec, e *node, box Rect) {
	rt := m.rt
	if rec.frame != rt.frame {
		m.noted++
	}
	rec.frame, rec.id, rec.box, rec.node, rec.kept, rec.lookSet = rt.frame, e.id, box, e, rt.exitsBuilt, false
}

// start sends the element of u, appearing, on its way from where rec
// showed the element of its Match.
func (m *matches) start(r *transition, u transitionUse, rec *matchRec, now time.Time) {
	rt, e := m.rt, u.e
	f := &flight{fly: u.t.Fly}
	m.flights[e.id] = f
	from := [4]float32{rec.box.X, rec.box.Y, rec.box.W, rec.box.H}
	for i := range f.box {
		f.box[i] = channel{value: float64(from[i]), target: float64(from[i]), at: now}
	}
	r.matching = true
	if c := rec.goneColors; c != ([2]Color{}) {
		// Its colors come from the other's too.
		r.colorSet, r.colorTarget, r.colorShown, r.colorFrame = true, c, c, rt.frame-1
	}
	g := rec.gone
	rec.gone, rec.goneColors = nil, [2]Color{}
	if !f.fly {
		return
	}
	// From the other's look, under the elements around it.
	switch {
	case rec.lookSet:
		f.lookFrom, f.worldFrom = rec.look, rec.world
	case rec.kept:
		f.lookFrom = lookOf(rec.node)
		if p := rec.node.parent; p != nil {
			f.worldFrom = p.world
		}
	default:
		f.lookFrom, f.worldFrom = lookOf(e), worldAround(e)
	}
	f.look = channel{at: now}
	headFor(&f.look, 1, r.animation(), now)
	if g != nil {
		f.src, f.srcAlpha, f.srcTurn = g, rec.goneAlpha, g.transform
	} else if x := rt.trans[rec.id]; x != nil && x.built == rt.frame && x.t.Fly {
		f.srcID = rec.id // still built, it flies itself
	}
	if rec.kept && morphs(e, rec.node) {
		f.morph = true
		m.morphFrom(r, e, rec, f.lookFrom.text, now)
	}
	if f.src != nil || f.srcID != 0 || f.morph {
		m.gone = append(m.gone, rec.id)
	}
}

// morphFrom moves the characters of the text of e, of transition r, from
// those of the text that rec noted, from where they were: its content
// transition, as of the frame it paints next.
func (m *matches) morphFrom(r *transition, e *node, rec *matchRec, color Color, now time.Time) {
	from := rec.node
	tx, ty := from.contentX(), from.contentY()
	pairs := &morphPairs{boxOf: from.tl, boxX: rec.box.X, boxY: rec.box.Y}
	was := from.st.crec
	if was != nil {
		if pr, _ := was.morph.(*morphPairs); pr != nil {
			// The colors of its spans, as rich text painted them last.
			pairs.spans = pr.spans
		} else {
			was = nil
		}
	}
	cr := &contentRec{frame: m.rt.paints + 1, text: e.text, tl: e.tl, oldText: from.text, oldTL: from.tl,
		oldX: rec.box.X + tx, oldY: rec.box.Y + ty, oldColor: color, morph: pairs}
	cr.k = channel{at: now}
	if rec.lookSet && was != nil && was.k.value > 0 && was.k.value < 1 {
		// The other was morphing on its way.
		color := from.resolvedText().color
		if was.oldText == e.text {
			// Back to the text the other morphed from, from where it is: a
			// morph k of the way shows as the morph back 1-k of the way.
			cr.k.value, cr.oldColor = 1-was.k.value, color
		} else {
			// From its characters as they showed, morphing.
			snapMorph(was, rec.box.X, rec.box.Y, rec.box.X+tx, rec.box.Y+ty, color, cr)
		}
	}
	headFor(&cr.k, 1, r.animation(), now)
	e.st.crec = cr
}

// hideCopies hides the copies of the elements of ids inside g, a copy
// going away, but g itself.
func hideCopies(g *node, ids []uint64) {
	for c := g.first; c != nil; c = c.next {
		if slices.Contains(ids, c.id) {
			c.flags |= flagInvisible
			continue
		}
		hideCopies(c, ids)
	}
}

// end ends the way of element id, of transition r, if any.
func (m *matches) end(r *transition, id uint64) {
	if r != nil {
		r.matching = false
	}
	delete(m.flights, id)
}

// step shows the element of u, of transition r, on its way to box, with
// the element it came from if it flies, and notes it in rec while it is.
func (m *matches) step(r *transition, u transitionUse, box Rect, rec *matchRec, uses []transitionUse, now time.Time) {
	e := u.e
	f := m.flights[e.id]
	if f == nil {
		r.matching = false
		return
	}
	f.r = r
	shown, moving := m.advance(f, box, now)
	if !moving {
		m.end(r, e.id)
		return
	}
	rec.taker, rec.takenAt = e, m.rt.frame
	if !f.fly {
		m.show(e, shown, r.t.ScaleContent)
		m.note(rec, e, shown)
		return
	}
	f.lookTo = lookOf(e)
	look, world := m.fly(f, e, shown, box, uses, 1)
	m.note(rec, e, shown)
	rec.lookSet, rec.look, rec.world = true, look, world
}

// advance moves flight f on towards box, where its element is, and
// returns the box it shows it in, and whether it is still on its way.
// Flying, it arrives with its look, its box within a hair of its own: the
// rest of a spring settling would move it by less than a pixel, which its
// text shows a pixel at a time.
func (m *matches) advance(f *flight, box Rect, now time.Time) (Rect, bool) {
	a := f.r.animation()
	to := [4]float32{box.X, box.Y, box.W, box.H}
	var v [4]float32
	moving, near := false, true
	for i := range f.box {
		ch := &f.box[i]
		headFor(ch, float64(to[i]), a, now)
		ch.advance(now)
		moving = moving || ch.moving || ch.pending
		near = near && !ch.pending && math.Abs(ch.value-ch.target) < 0.25
		v[i] = float32(ch.value)
	}
	if f.fly {
		f.look.advance(now)
		moving = !near || f.look.moving || f.look.pending
	}
	if moving {
		m.rt.animating = true
	}
	return Rect{v[0], v[1], max(v[2], 0), max(v[3], 0)}, moving
}

// fly shows e, flying, in the window's box shown on its way to box: from
// the look of the element it came from to its own (lookTo), from the
// transform of the elements around that one to that of those around it,
// over the rest of its layer, and over the element it came from, which
// fades out. fade fades both, as the copy of the elements around e going
// away fades it. It returns the look e shows, and the transform around
// it.
func (m *matches) fly(f *flight, e *node, shown, box Rect, uses []transitionUse, fade float32) (matchLook, scene.Affine) {
	rt := m.rt
	scaled := f.r.t.ScaleContent
	k := max(0, min(float32(f.look.value), 1))
	layer := layerOf(e)
	// Under the elements around the other, going to those around it.
	around := worldAround(e)
	world := mixTransform(Transform{m: f.worldFrom}, Transform{m: around}, k).m
	// The element it came from, under it, fading out.
	src := f.src
	if src == nil && f.srcID != 0 {
		src = m.builtWith(uses, f.srcID)
	}
	if src != nil && f.morph {
		// Its characters move into this one's.
		src.flags |= flagInvisible
		src = nil
	}
	if src != nil {
		alpha := float32(1)
		var srcAround scene.Affine
		if src == f.src {
			// A copy, laid out anew each frame, as those of an Exit.
			src.next = nil
			rt.c.root.add(src)
			alpha, src.transform = f.srcAlpha, f.srcTurn
			w, h := shown.W, shown.H
			if scaled {
				w, h = src.w, src.h
			}
			layoutBox(src, w, h)
			src.x, src.y = shown.X, shown.Y
			place(src, 0, 0)
			if scaled {
				m.scale(src, shown.W, shown.H)
			}
		} else {
			if src.opacitySet {
				alpha = src.opacity
			}
			srcAround = worldAround(src)
			m.show(src, shown, scaled)
		}
		m.pin(src, box)
		under(src, world, srcAround)
		src.opacity, src.opacitySet = alpha*fadeOut(k)*fade, true
		src.lift = true
		m.lifted = append(m.lifted, flier{src, layer})
	}
	// Itself, from the other's look to its own: laid out first, as its
	// look lays out nothing.
	lab := f.r.animation().Oklab
	look := mixLook(f.lookFrom, f.lookTo, k, lab)
	e.transform = look.turn
	if f.morph {
		// Its text as laid out, which its characters move to, as they
		// will show where its box goes.
		e.x, e.y, e.w, e.h = shown.X, shown.Y, shown.W, shown.H
		if cr := e.st.crec; cr != nil {
			if pr, _ := cr.morph.(*morphPairs); pr != nil {
				pr.toX, pr.toY, pr.toFrame = box.X-shown.X, box.Y-shown.Y, rt.frame
			}
			// Its characters go to its color as they move.
			look.text = mixPremultiplied(cr.oldColor, f.lookTo.text, max(0, min(float32(cr.k.value), 1)), lab)
		}
		// Its characters place themselves (morph): without the translation
		// of the elements flying around it.
		if a := m.around(e); a != (Point{}) {
			e.transform = e.transform.Then(Transform{m: scene.Translation(-a.X, -a.Y)})
		}
		f.shift = Point{}
	} else {
		m.show(e, shown, scaled)
		f.shift = m.pin(e, box)
	}
	under(e, world, around)
	shows := look
	if src != nil {
		shows.opacity *= fadeIn(k)
	}
	if look.text != f.lookTo.text && !f.morph {
		e.ts.color, e.ts.set = look.text, e.ts.set|setColor
	}
	e.opacity, e.opacitySet = shows.opacity*fade, true
	e.radius = look.radius
	if scaled && !f.morph && e.w > 0 && e.h > 0 {
		// Its corners as round as they show, scaled.
		s := 2 / (shown.W/e.w + shown.H/e.h)
		for i := range e.radius {
			e.radius[i] *= s
		}
	}
	e.lift = true
	m.lifted = append(m.lifted, flier{e, layer})
	return look, world
}

// flyOn flies on the element of flight f, id, which went with the
// elements around it: as its copy in theirs, going as their Exit says,
// from where it shows to where that copy has it, fading as it fades. It
// reports whether it does: not once there, nor once that copy is gone.
func (m *matches) flyOn(f *flight, id uint64, uses []transitionUse, now time.Time) bool {
	rt := m.rt
	if !f.fly || f.r == nil {
		return false
	}
	if f.copy == nil {
		for gid, x := range rt.trans {
			if x.ghost != nil && x.built != rt.frame {
				if c := copyIn(x.ghost, id); c != nil {
					f.copy, f.copyIn = c, gid
					break
				}
			}
		}
	}
	e := f.copy
	if x := rt.trans[f.copyIn]; e == nil || x == nil || x.ghost == nil || x.built == rt.frame || e.flags&flagInvisible != 0 {
		return false // gone, or flying with another element of its Match
	}
	r := f.r
	box := Rect{e.x, e.y, e.w, e.h}
	shown, moving := m.advance(f, box, now)
	if !moving {
		// There: it goes on as the copy around it goes, as it looks.
		l := f.lookTo
		e.transform, e.radius, e.opacity, e.opacitySet, e.lift = l.turn, l.radius, l.opacity, true, false
		if e.ts.set&setColor != 0 {
			e.ts.color = l.text
		}
		if r.colorSet {
			e.bg, e.borderC = r.colorTarget[0], r.colorTarget[1]
		}
		return false
	}
	// Its colors go on to its own too, as its transition moved them.
	if r.colorMoving {
		r.colorK.advance(now)
		k := float32(1)
		if r.colorK.moving || r.colorK.pending {
			k = max(0, min(float32(r.colorK.value), 1))
		} else {
			r.colorMoving = false
		}
		lab := r.animation().Oklab
		e.bg, e.borderC = mixPremultiplied(r.colorFrom[0], r.colorTarget[0], k, lab), mixPremultiplied(r.colorFrom[1], r.colorTarget[1], k, lab)
	}
	fade := float32(1)
	for a := e.parent; a != nil; a = a.parent {
		if a.leaving != 0 && a.opacitySet {
			fade *= a.opacity
		}
	}
	m.fly(f, e, shown, box, uses, fade)
	return true
}

// copyIn returns the copy of the element of id inside g, a copy going
// away, but g itself.
func copyIn(g *node, id uint64) *node {
	for c := g.first; c != nil; c = c.next {
		if c.id == id {
			return c
		}
		if d := copyIn(c, id); d != nil {
			return d
		}
	}
	return nil
}

// show shows e, of a Match on its way, in the window's box r: laid out at
// r's size, or scaled to it from its own.
func (m *matches) show(e *node, r Rect, scaled bool) {
	if !scaled {
		e.showAt(r)
		return
	}
	shift(e, r.X-e.x, r.Y-e.y)
	m.scale(e, r.W, r.H)
}

// pin places e, flying, so that its content sits on the device's pixels
// as it does at rest, at rest, and shows it where it is with a
// translation, which it returns with that of the elements flying around
// it, inside which it places itself: its text moves smoothly and as one,
// rather than each glyph a pixel at a time, and is the text at rest once
// there.
func (m *matches) pin(e *node, rest Rect) Point {
	_, _, s := m.rt.size()
	around := m.around(e)
	x, rx := snapTo(e.x-around.X, rest.X, s)
	y, ry := snapTo(e.y-around.Y, rest.Y, s)
	shift(e, x-e.x, y-e.y)
	if rx != 0 || ry != 0 {
		e.transform = e.transform.Then(Transform{m: scene.Translation(rx, ry)})
	}
	return Point{around.X + rx, around.Y + ry}
}

// around returns the translation that the elements flying around e give
// it.
func (m *matches) around(e *node) Point {
	for a := e.parent; a != nil; a = a.parent {
		if f := m.flights[a.id]; a.lift && f != nil {
			return f.shift
		}
	}
	return Point{}
}

// snapTo returns where to place a box at pos, s pixels a DIP, so that it
// sits on the pixels as one at rest does, and what is left of the way to
// pos.
func snapTo(pos, rest, s float32) (placed, left float32) {
	d := (pos - rest) * s
	left = (d - float32(math.Round(float64(d)))) / s
	return pos - left, left
}

// paint paints the elements flying in layer over it, as move listed them,
// as the copy of an element dragged paints: in the window's clip, as
// opaque as they are, whatever the opacity of the elements around them,
// and in the theme of the Themed around them.
func (m *matches) paint(p *Painter, layer *node) {
	if len(m.lifted) == 0 {
		return
	}
	w, h, _ := p.rt.size()
	saved, savedClip, savedTheme := p.opacity, p.clip, p.theme
	for _, l := range m.lifted {
		if l.layer == layer && parentsShow(l.e) {
			p.opacity, p.clip, p.theme, p.lifting = 1, Rect{0, 0, w, h}, l.e.theme(), l.e
			p.element(l.e)
		}
	}
	p.opacity, p.clip, p.theme, p.lifting = saved, savedClip, savedTheme, nil
}

// parentsShow reports whether the elements around e show it: none is
// hidden or invisible.
func parentsShow(e *node) bool {
	for a := e.parent; a != nil; a = a.parent {
		if a.hidden || a.flags&flagInvisible != 0 {
			return false
		}
	}
	return true
}
