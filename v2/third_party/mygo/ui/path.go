package ui

import (
	"encoding/binary"
	"hash/maphash"
	"math"
	"slices"
	"sync"

	"github.com/egoist/mygo/internal/scene"
	"github.com/egoist/mygo/internal/vec"
)

// Path is a vector shape, in DIPs, for Painter.FillPath and StrokePath:
// icons, check marks, charts.
type Path struct {
	cmds []pathCmd
	// trim is the part of the path's length it keeps (Trim), when trimmed.
	trim    [2]float32
	trimmed bool
	// How a stroke ends and turns, and its dashes, in DIPs (Cap, Join,
	// MiterLimit, Dash); a miter of 0 is 4.
	cap        LineCap
	join       LineJoin
	miter      float32
	dash       []float32
	dashOffset float32
	// rule is which points a fill covers (FillRule).
	rule FillRule
}

// FillRule is which points inside a path's subpaths a Painter fills, as
// SVG's fill-rule and clip-rule say, where they overlap or one is inside
// another.
type FillRule uint8

const (
	// NonZero fills a point that the subpaths wind around more times in
	// one direction than in the other: a subpath inside another one,
	// drawn the same way round, fills too, and one drawn the other way
	// round leaves a hole. The default, as SVG's nonzero.
	NonZero FillRule = iota
	// EvenOdd fills a point that the subpaths go around an odd number of
	// times, whichever way: a subpath inside another one leaves a hole,
	// as a ring of two circles, as SVG's evenodd.
	EvenOdd
)

// LineCap is how a stroke ends where its subpath or a dash of it does.
type LineCap uint8

const (
	// RoundCap ends a stroke in a half disc as wide as the stroke: the
	// default.
	RoundCap LineCap = iota
	// ButtCap ends a stroke square, where its line ends.
	ButtCap
	// SquareCap ends a stroke square, half its width past where its line
	// ends.
	SquareCap
)

// LineJoin is how a stroke turns at a corner of its path.
type LineJoin uint8

const (
	// RoundJoin turns a corner in an arc: the default.
	RoundJoin LineJoin = iota
	// MiterJoin turns a corner in a point, where the stroke's sides meet,
	// and bevels it where the point would be farther from the corner than
	// the miter limit (MiterLimit).
	MiterJoin
	// BevelJoin cuts a corner straight across.
	BevelJoin
)

type pathCmd struct {
	op  uint8 // 0 move, 1 line, 2 quad, 3 cubic, 4 close
	pts [3][2]float32
}

// Reset empties the path and removes Trim, its FillRule and the stroke's
// caps, joins and dashes, retaining its storage.
func (p *Path) Reset() *Path {
	p.cmds = p.cmds[:0]
	p.trim, p.trimmed, p.rule = [2]float32{}, false, NonZero
	p.cap, p.join, p.miter, p.dash, p.dashOffset = RoundCap, RoundJoin, 0, p.dash[:0], 0
	return p
}

// MoveTo starts a new subpath at (x, y).
func (p *Path) MoveTo(x, y float32) *Path {
	p.cmds = append(p.cmds, pathCmd{op: 0, pts: [3][2]float32{{x, y}}})
	return p
}

// LineTo draws a line to (x, y).
func (p *Path) LineTo(x, y float32) *Path {
	p.cmds = append(p.cmds, pathCmd{op: 1, pts: [3][2]float32{{x, y}}})
	return p
}

// QuadTo draws a quadratic Bézier curve to (x, y) with control point
// (cx, cy).
func (p *Path) QuadTo(cx, cy, x, y float32) *Path {
	p.cmds = append(p.cmds, pathCmd{op: 2, pts: [3][2]float32{{cx, cy}, {x, y}}})
	return p
}

// CubeTo draws a cubic Bézier curve to (x, y) with control points (c1x,
// c1y) and (c2x, c2y).
func (p *Path) CubeTo(c1x, c1y, c2x, c2y, x, y float32) *Path {
	p.cmds = append(p.cmds, pathCmd{op: 3, pts: [3][2]float32{{c1x, c1y}, {c2x, c2y}, {x, y}}})
	return p
}

// Close closes the subpath.
func (p *Path) Close() *Path {
	p.cmds = append(p.cmds, pathCmd{op: 4})
	return p
}

// Cap sets how a Painter strokes the ends of the path's open subpaths and
// of its dashes, round by default, as SVG's stroke-linecap, and returns
// the path.
func (p *Path) Cap(c LineCap) *Path {
	p.cap = c
	return p
}

// Join sets how a Painter strokes the corners of the path, round by
// default, as SVG's stroke-linejoin, and returns the path.
func (p *Path) Join(j LineJoin) *Path {
	if j == MiterJoin {
		linkMiters.Do(func() { miterReach = mitredReach })
	}
	p.join = j
	return p
}

// MiterLimit sets how far the point of a MiterJoin may reach from its
// corner, in half the stroke's width, as SVG's stroke-miterlimit, and
// returns the path: a corner sharper than that is bevelled. It is 4 by
// default, which bevels corners sharper than about 29 degrees, and at
// least 1.
func (p *Path) MiterLimit(limit float32) *Path {
	finiteMotion("miter limit", float64(limit))
	p.miter = max(limit, 1)
	return p
}

// Dash sets a Painter to stroke the path in dashes, as SVG's
// stroke-dasharray and stroke-dashoffset, and returns the path. lengths
// are those of the dashes and the gaps between them in turn, in DIPs,
// repeated along the path, and twice over when there is an odd number of
// them; each subpath starts them anew, offset DIPs into them, and a dash
// through the start of a closed one turns there as at its other corners.
// A dash of no length shows its caps alone, as a dot with RoundCap. No
// lengths, a negative one, or lengths that add up to none stroke the path
// whole. As Trim trims the path, its dashes stay where they are on it.
func (p *Path) Dash(offset float32, lengths ...float32) *Path {
	finiteMotion("dash", float64(offset))
	p.dash, p.dashOffset = p.dash[:0], offset
	var total float32
	for _, l := range lengths {
		finiteMotion("dash", float64(l))
		if l < 0 {
			p.dash = p.dash[:0]
			return p
		}
		total += l
	}
	if total > 0 {
		linkDashes.Do(func() { dashLine, snapPhases = strokeDashes, snapDashPhases })
		p.dash = append(p.dash, lengths...)
		if len(lengths)%2 == 1 {
			p.dash = append(p.dash, lengths...)
		}
	}
	return p
}

// FillRule sets which points of the path a Painter fills, and clips to
// (FillPath, ClipPath), and Contains finds inside it, and returns the
// path: NonZero, the default, or EvenOdd, which makes a ring of two
// circles drawn the same way round:
//
//	var ring ui.Path
//	ring.Circle(50, 50, 40).Circle(50, 50, 25).FillRule(ui.EvenOdd)
//	p.FillPath(&ring, c.Theme().Accent)
func (p *Path) FillRule(r FillRule) *Path {
	p.rule = r
	return p
}

// Contains reports whether the point (x, y) is inside the path, in the
// coordinates of its points, as a Painter fills it (FillRule): Canvas's
// isPointInPath. Its subpaths close, as filling closes them, and Trim
// keeps what it fills; its curves are flattened as a Painter flattens
// them at a scale of 4. A view tells with it which of a pie's segments, a
// map's regions or a ring's band a press is in, in its own handler or in
// an element's HitTest.
func (p *Path) Contains(x, y float32) bool {
	if len(p.cmds) == 0 {
		return false
	}
	f := containsFlat.Get().(*flatPath)
	defer containsFlat.Put(f)
	const scale = 4
	p.flatten(f, scale)
	return f.contains(x*scale, y*scale)
}

// containsFlat holds the flattened paths Contains reuses.
var containsFlat = sync.Pool{New: func() any { return new(flatPath) }}

// contains reports whether f, filled, covers the point (x, y): its
// polylines, each closed, wind around it by its rule.
func (f *flatPath) contains(x, y float32) bool {
	winding := 0
	for i := range f.polys() {
		poly := f.poly(i)
		for k := range poly {
			a, b := poly[k], poly[(k+1)%len(poly)]
			if (a[1] <= y) == (b[1] <= y) {
				continue
			}
			if cx := a[0] + (y-a[1])*(b[0]-a[0])/(b[1]-a[1]); cx > x {
				if b[1] > a[1] {
					winding++
				} else {
					winding--
				}
			}
		}
	}
	return f.inside(winding)
}

// inside reports whether a point the path winds around winding times
// is filled, by its rule.
func (f *flatPath) inside(winding int) bool {
	if f.evenOdd {
		return winding%2 != 0
	}
	return winding != 0
}

// Trim keeps of the path, as a Painter fills or strokes it, the part of
// its length from from to to, as fractions of it, its subpaths one after
// another, and returns the path: 0 to 0.5 draws its first half, and a check
// mark draws itself as to animates from 0 to 1, as SwiftUI's trim(from:to:)
// and Motion's pathLength. Trimmed, closed subpaths draw open, and a
// stroke trimmed shorter than its width fades with its length, rather than
// its round or square caps showing a dot that comes or goes at once.
func (p *Path) Trim(from, to float32) *Path {
	finiteMotion("trim", float64(from), float64(to))
	p.trim, p.trimmed = [2]float32{max(0, min(from, 1)), max(0, min(to, 1))}, true
	return p
}

// Bounds returns the smallest box holding the path's points and the
// control points of its curves, in DIPs: the box of a circle (Circle), of
// lines, and of curves whose control points lie on it; others may fit in
// less. Empty paths have an empty box.
func (p *Path) Bounds() Rect {
	first := true
	var x0, y0, x1, y1 float32
	for _, c := range p.cmds {
		n := [5]int{1, 1, 2, 3, 0}[c.op]
		for _, pt := range c.pts[:n] {
			if first {
				x0, y0, x1, y1, first = pt[0], pt[1], pt[0], pt[1], false
				continue
			}
			x0, y0, x1, y1 = min(x0, pt[0]), min(y0, pt[1]), max(x1, pt[0]), max(y1, pt[1])
		}
	}
	return Rect{x0, y0, x1 - x0, y1 - y0}
}

// Circle adds a circle as a subpath.
func (p *Path) Circle(cx, cy, r float32) *Path {
	const k = 0.5522847498
	p.MoveTo(cx+r, cy)
	p.CubeTo(cx+r, cy+r*k, cx+r*k, cy+r, cx, cy+r)
	p.CubeTo(cx-r*k, cy+r, cx-r, cy+r*k, cx-r, cy)
	p.CubeTo(cx-r, cy-r*k, cx-r*k, cy-r, cx, cy-r)
	p.CubeTo(cx+r*k, cy-r, cx+r, cy-r*k, cx+r, cy)
	return p.Close()
}

// flatPath is a path flattened to polylines in device pixels: polyline i
// holds pts[starts[i]:starts[i+1]], the last up to the end of pts. The
// engine keeps one for the paths it paints, reusing its memory.
type flatPath struct {
	pts    [][2]float32
	starts []int
	closed []bool
	// evenOdd fills it by the even-odd rule (FillRule).
	evenOdd bool
	// trimmed tells that Trim kept kept pixels of the path's length.
	trimmed bool
	kept    float32
	// The polylines that trimming keeps, swapped with the others.
	keptPts    [][2]float32
	keptStarts []int
	keptClosed []bool
	// The path's stroke in pixels: its caps, joins and miter limit, and
	// its dashes (none strokes it whole), the length of their pattern, and
	// where in the pattern each polyline starts (phase).
	cap    LineCap
	join   LineJoin
	miter  float32
	dash   []float32
	period float32
	phase  []float32
}

func (f *flatPath) polys() int { return len(f.starts) }

func (f *flatPath) poly(i int) [][2]float32 {
	end := len(f.pts)
	if i+1 < len(f.starts) {
		end = f.starts[i+1]
	}
	return f.pts[f.starts[i]:end]
}

// reset empties f, untrimmed, to be stroked round and whole, retaining
// its memory: StrokePolyline fills it in as it is, flatten as its path
// says.
func (f *flatPath) reset() {
	f.pts, f.starts, f.closed = f.pts[:0], f.starts[:0], f.closed[:0]
	f.trimmed, f.kept, f.evenOdd = false, 0, false
	f.cap, f.join, f.miter = RoundCap, RoundJoin, 4
	f.dash, f.period, f.phase = f.dash[:0], 0, f.phase[:0]
}

// flatten sets f to the path's subpaths as polylines in device pixels.
func (p *Path) flatten(f *flatPath, scale float32) {
	f.reset()
	f.cap, f.join, f.evenOdd = p.cap, p.join, p.rule == EvenOdd
	if p.miter != 0 {
		f.miter = p.miter
	}
	for _, l := range p.dash {
		f.dash = append(f.dash, l*scale)
		f.period += l * scale
	}
	if !(f.period > 0) {
		f.dash = f.dash[:0]
	}
	begin, isClosed := 0, false
	var origin, pen [2]float32
	flush := func() {
		if len(f.pts)-begin > 1 {
			f.starts = append(f.starts, begin)
			f.closed = append(f.closed, isClosed)
		} else {
			f.pts = f.pts[:begin]
		}
		begin, isClosed = len(f.pts), false
	}
	for _, c := range p.cmds {
		pts := c.pts
		for i := range pts {
			pts[i][0] *= scale
			pts[i][1] *= scale
		}
		switch c.op {
		case 0:
			flush()
			origin, pen = pts[0], pts[0]
			f.pts = append(f.pts, pen)
		case 1:
			pen = pts[0]
			f.pts = append(f.pts, pen)
		case 2:
			n := curveSteps(pen, pts[0], pts[1], pts[1])
			for i := 1; i <= n; i++ {
				t := float32(i) / float32(n)
				u := 1 - t
				f.pts = append(f.pts, [2]float32{u*u*pen[0] + 2*u*t*pts[0][0] + t*t*pts[1][0], u*u*pen[1] + 2*u*t*pts[0][1] + t*t*pts[1][1]})
			}
			pen = pts[1]
		case 3:
			n := curveSteps(pen, pts[0], pts[1], pts[2])
			for i := 1; i <= n; i++ {
				t := float32(i) / float32(n)
				u := 1 - t
				a, b, cc, d := u*u*u, 3*u*u*t, 3*u*t*t, t*t*t
				f.pts = append(f.pts, [2]float32{a*pen[0] + b*pts[0][0] + cc*pts[1][0] + d*pts[2][0], a*pen[1] + b*pts[0][1] + cc*pts[1][1] + d*pts[2][1]})
			}
			pen = pts[2]
		case 4:
			if len(f.pts) > begin {
				isClosed = true
				flush()
				pen = origin
			}
		}
	}
	flush()
	offset := p.dashOffset * scale
	if len(f.dash) > 0 {
		for range f.polys() {
			f.phase = append(f.phase, offset)
		}
	}
	if p.trimmed {
		f.trim(p.trim[0], p.trim[1], offset)
	}
}

// trim keeps the part of f's length from from to to, as fractions of it,
// as open polylines, each dashed from where it starts on its subpath, its
// dashes offset by offset.
func (f *flatPath) trim(from, to, offset float32) {
	// segments returns how many segments polyline i has, a closed one's
	// back to its start included.
	segments := func(i int) int {
		n := len(f.poly(i)) - 1
		if f.closed[i] {
			n++
		}
		return n
	}
	var total float32
	for i := range f.polys() {
		poly := f.poly(i)
		for j := range segments(i) {
			total += dist(poly[j], poly[(j+1)%len(poly)])
		}
	}
	lo, hi := from*total, to*total
	f.trimmed, f.kept = true, max(hi-lo, 0)
	pts, starts, closed := f.keptPts[:0], f.keptStarts[:0], f.keptClosed[:0]
	f.phase = f.phase[:0]
	var at float32
	for i := range f.polys() {
		poly := f.poly(i)
		sub := at     // where the subpath starts
		open := false // a kept polyline goes on to the next segment
		for j := range segments(i) {
			a, b := poly[j], poly[(j+1)%len(poly)]
			l := dist(a, b)
			s, e := max(lo, at), min(hi, at+l)
			at += l
			if l == 0 || s >= e {
				open = open && l == 0
				continue
			}
			k0, k1 := (s-(at-l))/l, (e-(at-l))/l
			if !open {
				starts, closed = append(starts, len(pts)), append(closed, false)
				pts = append(pts, [2]float32{a[0] + (b[0]-a[0])*k0, a[1] + (b[1]-a[1])*k0})
				if len(f.dash) > 0 {
					f.phase = append(f.phase, offset+s-sub)
				}
			}
			pts = append(pts, [2]float32{a[0] + (b[0]-a[0])*k1, a[1] + (b[1]-a[1])*k1})
			open = k1 >= 1
		}
	}
	f.keptPts, f.pts = f.pts, pts
	f.keptStarts, f.starts = f.starts, starts
	f.keptClosed, f.closed = f.closed, closed
}

func curveSteps(a, b, c, d [2]float32) int {
	l := dist(a, b) + dist(b, c) + dist(c, d)
	return max(2, min(int(math.Sqrt(float64(l))*2), 64))
}

func dist(a, b [2]float32) float32 {
	return float32(math.Hypot(float64(a[0]-b[0]), float64(a[1]-b[1])))
}

var pathSeed = maphash.MakeSeed()

// paths holds what the engine reuses to paint paths, frame after frame:
// the flattened path, the job of drawing its mask, and the rasterizer
// and pixels that draw it.
type paths struct {
	flat   flatPath
	job    pathJob
	raster vec.Rasterizer
	pix    []byte
	stroke strokeBufs
	// What pathTiles finds of a path drawn in tiles: the tiles its edges
	// reach, and where its edges cross a row of tiles.
	reached []bool
	cross   []pathCrossing
	// crowded is the last paint (engine.paints) that draws as tiles the
	// paths larger than a tile whose masks the atlas would hold: it
	// could not hold a path's mask lately (pathCrowdedPaints).
	crowded uint64
	// large holds the keys of the masks of paths larger than a tile that
	// paint largePaint drew, which its repaint draws as tiles if the
	// atlas fills.
	large      []uint64
	largePaint uint64
}

// pathJob is the mask drawPath asks the text system for, which draws it
// with the engine's rasterizer and buffer when it is not cached.
type pathJob struct {
	f      *flatPath
	w, h   int
	hw     float32 // half the stroke's width; 0 fills
	x0, y0 float32
	// full draws a mask covered whole, as a tile inside a filled path.
	full      bool
	rasterize func() (w, h int, pix []byte)
}

// A path's mask is a bitmap in the mask atlas, which is at most
// scene.MaxAtlasSize pixels a side, keeps a pixel of padding beside each
// bitmap and holds the glyphs of text too. A path whose mask would be
// wider or taller than pathMaskMax device pixels, the largest the atlas
// holds, draws as tiles of its mask, pathTile pixels a side (pathTiles):
// 512 with the padding, eight to a row of the largest atlas. They are
// variables so that tests can tile small paths.
var (
	pathMaskMax = scene.MaxAtlasSize - 1
	pathTile    = 511
)

// pathCrowdedPaints is how many paints draw paths larger than a tile as
// tiles after the atlas could not hold a path's mask. A mask that large
// takes a shelf of the atlas of its own, mostly empty for a long thin
// stroke, and a few of them, as wires redrawn every frame, can leave no
// room for the rest of the frame's paths. Tiles pack eight to a row of
// the largest atlas and skip what the path does not reach, so the frame
// painted again holds them; on the pixel grid they draw the same pixels.
// After those paints, paths try one mask again, which draws faster and
// shows no line between tiles under a transform.
const pathCrowdedPaints = 120

// pathSpanMax is the largest width and height of a path's mask, in device
// pixels: beyond it, float32 no longer holds the quarters of a pixel that
// its points move to.
const pathSpanMax = 1 << 22

// pathCrossing is where an edge of a filled path crosses a line across a
// row of tiles, and which way it goes: +1 down, -1 up.
type pathCrossing struct {
	x   float32
	dir int
}

// drawPath paints a flattened path with c, from a mask cached by the shape:
// filled by its rule, or with width > 0 stroked that many pixels
// wide with its caps, joins and dashes.
func (p *Painter) drawPath(f *flatPath, width float32, c Color, g *LinearGradient) {
	p.drawPathPaint(f, width, c, g, nil)
}

// drawPathPaint is drawPath in a linear gradient (g) or a conic one (cg).
func (p *Painter) drawPathPaint(f *flatPath, width float32, c Color, g *LinearGradient, cg *ConicGradient) {
	if f.polys() == 0 {
		return
	}
	hw := max(width, 0) / 2
	minX, minY := float32(math.MaxFloat32), float32(math.MaxFloat32)
	maxX, maxY := float32(-math.MaxFloat32), float32(-math.MaxFloat32)
	for _, pt := range f.pts {
		minX, maxX = min(minX, pt[0]), max(maxX, pt[0])
		minY, maxY = min(minY, pt[1]), max(maxY, pt[1])
	}
	// bounds returns the corner and size of the mask of a stroke that
	// reaches reach pixels from its center line, and whether float32
	// holds its points (pathSpanMax).
	bounds := func(reach float32) (x0, y0 float32, w, h int, ok bool) {
		x0, y0 = float32(math.Floor(float64(minX-reach))), float32(math.Floor(float64(minY-reach)))
		wf, hf := math.Ceil(float64(maxX+reach-x0))+1, math.Ceil(float64(maxY+reach-y0))+1
		return x0, y0, int(wf), int(hf), wf > 0 && hf > 0 && wf <= pathSpanMax && hf <= pathSpanMax
	}
	x0, y0, _, _, ok := bounds(hw)
	if !ok {
		return
	}
	// The key covers the shape relative to the pixel grid, in quarters of
	// a pixel; a stroke's is its center line's, its width, caps, joins and
	// dashes, and where each polyline starts in its dashes. The points
	// move to those quarters, so that the mask of a key is the same
	// whichever path drew it first: a path looks the same wherever it is
	// on the grid, whatever drew before it.
	for i, pt := range f.pts {
		f.pts[i] = [2]float32{x0 + round((pt[0]-x0)*4)/4, y0 + round((pt[1]-y0)*4)/4}
	}
	if hw > 0 && len(f.dash) > 0 {
		snapPhases(f)
	}
	// Square caps and miter joins reach farther, as far as the points
	// show that the mask draws.
	x0, y0, w, h, ok := bounds(f.reach(hw))
	if !ok {
		return
	}
	key := uint64(1)
	if hw > 0 {
		key = 2 + uint64(math.Float32bits(hw))<<8
	} else if f.evenOdd {
		key = 3
	}
	var buf [8]byte
	var hs maphash.Hash
	hs.SetSeed(pathSeed)
	binary.LittleEndian.PutUint64(buf[:], key)
	hs.Write(buf[:])
	if hw > 0 {
		hs.WriteByte(byte(f.cap))
		hs.WriteByte(byte(f.join))
		// The miter limit shapes miter joins alone.
		miter := float32(0)
		if f.join == MiterJoin {
			miter = f.miter
		}
		binary.LittleEndian.PutUint32(buf[:4], math.Float32bits(miter))
		binary.LittleEndian.PutUint32(buf[4:], uint32(len(f.dash)))
		hs.Write(buf[:])
		for _, l := range f.dash {
			binary.LittleEndian.PutUint32(buf[:4], math.Float32bits(l))
			hs.Write(buf[:4])
		}
	}
	for i := range f.polys() {
		for _, pt := range f.poly(i) {
			binary.LittleEndian.PutUint32(buf[:4], math.Float32bits(round((pt[0]-x0)*4)))
			binary.LittleEndian.PutUint32(buf[4:], math.Float32bits(round((pt[1]-y0)*4)))
			hs.Write(buf[:])
		}
		if hw > 0 && len(f.dash) > 0 {
			binary.LittleEndian.PutUint32(buf[:4], math.Float32bits(f.phase[i]))
			hs.Write(buf[:4])
		}
		end := byte(0xff)
		if f.closed[i] {
			end = 0xfe
		}
		hs.WriteByte(end)
	}
	start := int32(len(p.s.Glyphs))
	c = c.Alpha(p.opacity)
	tint := scene.Glyph{Color: c.scene(), Wide: p.glyphWide(c)}
	sum, large := hs.Sum64(), w > pathTile || h > pathTile
	tiled := w > pathMaskMax || h > pathMaskMax || large && p.rt.paths.crowded >= p.rt.paints
	ps := &p.rt.paths
	if ps.largePaint != p.rt.paints {
		ps.large, ps.largePaint = ps.large[:0], p.rt.paints
	}
	if !tiled {
		if p.pathMask(sum, f, hw, x0, y0, w, h, false, tint) {
			if large {
				ps.large = append(ps.large, sum)
			}
		} else {
			// The atlas is full: the runtime makes room and paints the
			// frame again, in which paths larger than a tile draw as
			// tiles. Try them now too, in what room is left, rather than
			// show nothing if room is not made in time. The masks of
			// those this paint drew before go as room is made: kept, as
			// the frame drew them, they could leave the tiles no room,
			// and the frame would show none of them.
			ps.crowded = p.rt.paints + pathCrowdedPaints
			p.rt.text.Unkeep(ps.large)
			tiled = large
		}
	}
	if tiled && !p.pathTiles(sum, f, hw, x0, y0, w, h, tint) && w <= pathMaskMax && h <= pathMaskMax {
		// Too many tiles of it show to hold, which one mask may not be.
		p.pathMask(sum, f, hw, x0, y0, w, h, false, tint)
	}
	if int32(len(p.s.Glyphs)) == start {
		return
	}
	op := scene.Op{Kind: scene.OpGlyphs, Start: start, End: int32(len(p.s.Glyphs))}
	if g != nil || cg != nil {
		// The gradient spans the path's bounds, and half a stroke's width
		// beyond: what square caps and miter joins reach past it takes
		// the color of its edge, so that they do not move the gradient.
		op.Rect = scene.Rect{X: minX - hw, Y: minY - hw, W: maxX - minX + 2*hw, H: maxY - minY + 2*hw}
		op.Opacity = p.opacity
		if g != nil {
			p.gradient(&op, *g)
			op.Wide = p.wide(g.From, g.To, Color{})
		} else {
			p.conic(&op, *cg)
		}
	}
	p.add(op)
}

// pathMask adds the glyph of the w×h mask of the path f at (x0, y0), in
// device pixels, cached under key, with tint's color: of all of the path,
// or of a tile, the whole path clipped to it, or, if full, covered whole.
// It reports false when the atlas has no room for the mask.
func (p *Painter) pathMask(key uint64, f *flatPath, hw, x0, y0 float32, w, h int, full bool, tint scene.Glyph) bool {
	j := &p.rt.paths.job
	if j.rasterize == nil {
		j.rasterize = p.rt.rasterizePath
	}
	j.f, j.w, j.h, j.hw, j.x0, j.y0, j.full = f, w, h, hw, x0, y0, full
	gi := p.rt.text.Mask(key, j.rasterize)
	j.f = nil
	if !gi.OK {
		return false
	}
	g := tint
	g.X, g.Y, g.W, g.H, g.U, g.V, g.UW, g.VH = x0, y0, float32(gi.W), float32(gi.H), gi.X, gi.Y, gi.W, gi.H
	p.s.Glyphs = append(p.s.Glyphs, g)
	return true
}

// pathTiles draws the w×h mask of the path f at (x0, y0), too large for
// one, as tiles of pathTile pixels a side from its corner: each a mask of
// its own, cached under the path's key and its place, of the whole path
// clipped to it, so that tiles meet as the pixels of one mask do. It draws
// the tiles that show, within a pixel, and that the path reaches: those
// within a pixel of its edges, and those inside a filled path, covered
// whole, which share one mask of their size. Under a transform off the
// pixel grid, renderers smooth each mask's edges, so a faint line shows
// where tiles meet. It reports false, drawing nothing, when the atlas
// could not hold the tiles that show.
func (p *Painter) pathTiles(key uint64, f *flatPath, hw, x0, y0 float32, w, h int, tint scene.Glyph) bool {
	t := pathTile
	// The columns and rows of tiles that show.
	v, s := p.Visible(), float64(p.scale)
	span := func(from, to float32, origin float32, n int) (int, int) {
		a := math.Floor((float64(from)*s - float64(origin) - 1) / float64(t))
		b := math.Ceil((float64(to)*s - float64(origin) + 1) / float64(t))
		return int(max(a, 0)), int(min(b, float64(n)))
	}
	c0, c1 := span(v.X, v.X+v.W, x0, (w+t-1)/t)
	r0, r1 := span(v.Y, v.Y+v.H, y0, (h+t-1)/t)
	cols, rows := c1-c0, r1-r0
	if cols <= 0 || rows <= 0 {
		return true
	}
	if cols*rows > pathTilesMax {
		return false
	}
	reached := p.rt.paths.reached
	if cap(reached) < cols*rows {
		reached = make([]bool, cols*rows)
	}
	reached = reached[:cols*rows]
	clear(reached)
	p.rt.paths.reached = reached
	// segments calls fn with each edge of the path, from the mask's
	// corner: a filled polyline's, back to its start too.
	segments := func(fn func(ax, ay, bx, by float32)) {
		for i := range f.polys() {
			poly := f.poly(i)
			for k := 1; k < len(poly); k++ {
				fn(poly[k-1][0]-x0, poly[k-1][1]-y0, poly[k][0]-x0, poly[k][1]-y0)
			}
			if hw == 0 || f.closed[i] {
				last := poly[len(poly)-1]
				fn(last[0]-x0, last[1]-y0, poly[0][0]-x0, poly[0][1]-y0)
			}
		}
	}
	// Each edge reaches the tiles within m of it: as far as a stroke
	// reaches (flatPath.reach), and a pixel of antialiasing and one of
	// rounding.
	m, size := f.reach(hw)+2, float32(t)
	segments(func(ax, ay, bx, by float32) {
		lo := max(int(math.Floor(float64((min(ay, by)-m)/size))), r0)
		hi := min(int(math.Floor(float64((max(ay, by)+m)/size))), r1-1)
		for r := lo; r <= hi; r++ {
			// The part of the edge within m of the row.
			xl, xh := min(ax, bx), max(ax, bx)
			if ay != by {
				k0 := (float32(r*t) - m - ay) / (by - ay)
				k1 := (float32((r+1)*t) + m - ay) / (by - ay)
				k0, k1 = max(min(k0, k1), 0), min(max(k0, k1), 1)
				xa, xb := ax+(bx-ax)*k0, ax+(bx-ax)*k1
				xl, xh = min(xa, xb), max(xa, xb)
			}
			cl := max(int(math.Floor(float64((xl-m)/size))), c0)
			ch := min(int(math.Floor(float64((xh+m)/size))), c1-1)
			for c := cl; c <= ch; c++ {
				reached[(r-r0)*cols+c-c0] = true
			}
		}
	})
	// The tiles the path reaches are masks of their own: more than half
	// the largest atlas of them would not fit beside the frame's glyphs.
	// Such a path, as one of which more than pathTilesMax tiles show, is
	// not drawn, as paths too large for a mask were not before tiles.
	room := 0
	for i, ok := range reached {
		if ok {
			tw, th := min(t, w-(c0+i%cols)*t), min(t, h-(r0+i/cols)*t)
			room += (tw + 1) * (th + 1)
		}
	}
	if room > scene.MaxAtlasSize*scene.MaxAtlasSize/2 {
		return false
	}
	for r := r0; r < r1; r++ {
		ty, th := r*t, min(t, h-r*t)
		// Inside a filled path, a tile no edge reaches is covered whole,
		// where its edges wind around a point of it by its rule: count
		// those crossing a line across the row, left of the point.
		cross := p.rt.paths.cross[:0]
		if hw == 0 && slices.Contains(reached[(r-r0)*cols:(r-r0+1)*cols], false) {
			y := float32(ty) + 0.5
			segments(func(ax, ay, bx, by float32) {
				if (ay <= y) != (by <= y) {
					dir := 1
					if by < ay {
						dir = -1
					}
					cross = append(cross, pathCrossing{ax + (y-ay)*(bx-ax)/(by-ay), dir})
				}
			})
			p.rt.paths.cross = cross
		}
		for c := c0; c < c1; c++ {
			tx, tw := c*t, min(t, w-c*t)
			if reached[(r-r0)*cols+c-c0] {
				p.pathMask(tileKey(key, tx, ty, tw, th), f, hw, x0+float32(tx), y0+float32(ty), tw, th, false, tint)
				continue
			}
			winding, x := 0, float32(tx)+0.5
			for _, k := range cross {
				if k.x < x {
					winding += k.dir
				}
			}
			if f.inside(winding) {
				// A mask covered whole, which tiles of its size share: the
				// key of no path, at no place.
				p.pathMask(tileKey(0, -1, -1, tw, th), f, 0, x0+float32(tx), y0+float32(ty), tw, th, true, tint)
			}
		}
	}
	return true
}

// pathTilesMax is how many tiles that show pathTiles looks at, at most:
// 64 by 64, 32704 pixels a side, more than a window shows of a path unless
// a transform shrinks it several times over.
const pathTilesMax = 64 * 64

// tileKey returns the key of the w×h tile at (x, y) of the mask of key.
func tileKey(key uint64, x, y, w, h int) uint64 {
	var buf [24]byte
	binary.LittleEndian.PutUint64(buf[:], key)
	binary.LittleEndian.PutUint32(buf[8:], uint32(x))
	binary.LittleEndian.PutUint32(buf[12:], uint32(y))
	binary.LittleEndian.PutUint32(buf[16:], uint32(w))
	binary.LittleEndian.PutUint32(buf[20:], uint32(h))
	return maphash.Bytes(pathSeed, buf[:])
}

// rasterizePath draws the mask of the path job, into memory the engine
// reuses: the text system copies it into its atlas.
func (rt *engine) rasterizePath() (int, int, []byte) {
	j := &rt.paths.job
	z := &rt.paths.raster
	n := j.w * j.h
	if cap(rt.paths.pix) < n {
		rt.paths.pix = make([]byte, n)
	}
	pix := rt.paths.pix[:n]
	if j.full {
		for i := range pix {
			pix[i] = 255
		}
		return j.w, j.h, pix
	}
	z.Reset(j.w, j.h)
	if j.hw > 0 {
		strokeInto(z, &rt.paths.stroke, j.f, j.hw, j.x0, j.y0)
		z.Mask(pix, j.w)
	} else {
		fillInto(z, j.f, j.x0, j.y0)
		fillMask(z, j.f, pix, j.w)
	}
	return j.w, j.h, pix
}

// fillMask writes the coverage of the filled path f, which z holds, by
// its rule.
func fillMask(z *vec.Rasterizer, f *flatPath, dst []byte, stride int) {
	if f.evenOdd {
		z.MaskEvenOdd(dst, stride)
	} else {
		z.Mask(dst, stride)
	}
}

// FillPath fills a path, by its FillRule.
func (p *Painter) FillPath(path *Path, c Color) {
	f := &p.rt.paths.flat
	path.flatten(f, p.scale)
	p.drawPath(f, 0, c, nil)
}

// FillPathGradient fills a path with a gradient across its bounds, as the
// area under a chart's line.
func (p *Painter) FillPathGradient(path *Path, g LinearGradient) {
	f := &p.rt.paths.flat
	path.flatten(f, p.scale)
	p.drawPath(f, 0, g.From, &g)
}

// StrokePath draws the outline of a path, width DIPs wide, with the
// path's caps, joins and dashes (Path.Cap, Path.Join, Path.Dash): round
// and whole unless it sets them.
func (p *Painter) StrokePath(path *Path, width float32, c Color) {
	f := &p.rt.paths.flat
	path.flatten(f, p.scale)
	p.drawPath(f, width*p.scale, c.Alpha(f.capFade(width*p.scale)), nil)
}

// capFade returns how opaque a stroke width pixels wide of f shows:
// trimmed shorter than its width, which its round or square caps would
// show as a dot appearing or going at once, it fades with its length.
// With butt caps, it shows only what it keeps, and does not fade.
func (f *flatPath) capFade(width float32) float32 {
	if !f.trimmed || f.cap == ButtCap || width <= 0 || f.kept >= width {
		return 1
	}
	return f.kept / width
}

// reach returns how far a stroke hw pixels to each side of f reaches from
// its center line: hw, √2 hw to the corners of square caps, and to the
// points of miter joins (mitredReach).
func (f *flatPath) reach(hw float32) float32 {
	k := float32(1)
	if f.cap == SquareCap {
		k = math.Sqrt2
	}
	if f.join != MiterJoin || hw == 0 {
		return hw * k
	}
	return hw * max(k, miterReach(f))
}

// mitredReach returns how far the points of f's miter joins reach from
// their corners, in half the stroke's width: 1/cos of half their turn,
// √(2/(1+dot)) for the dot product of the directions they turn between,
// where that is within the miter limit. It finds them as offsetLoop does,
// and a thousandth farther, for dashes, whose corners are those of the
// polylines but for the rounding of the points that cut them.
func mitredReach(f *flatPath) float32 {
	limit := 2 / (f.miter * f.miter) * (1 - 1e-3)
	least := float32(2) // the least 1+dot of a mitred corner
	turn := func(d, e [2]float32) {
		if dot := float32(d[0]*e[0]) + float32(d[1]*e[1]); 1+dot >= limit {
			least = min(least, 1+dot)
		}
	}
	for i := range f.polys() {
		poly := f.poly(i)
		a := poly[0]
		var first, last [2]float32 // the first direction and the last
		segs := 0
		to := func(b [2]float32) {
			if b == a {
				return
			}
			l := dist(a, b)
			d := [2]float32{(b[0] - a[0]) / l, (b[1] - a[1]) / l}
			if segs == 0 {
				first = d
			} else {
				turn(last, d)
			}
			a, last = b, d
			segs++
		}
		for _, b := range poly[1:] {
			to(b)
		}
		if f.closed[i] && segs > 1 {
			to(poly[0])
			turn(last, first)
		}
	}
	return float32(math.Sqrt(float64(2/least))) * 1.001
}

// snapDashPhases moves where f's polylines start in their dashes to
// quarters of a pixel, within one pattern of them, as drawPathPaint moves
// their points, so that an offset that animates around the pattern draws
// again what it drew.
func snapDashPhases(f *flatPath) {
	for i, ph := range f.phase {
		ph = round(float32(math.Mod(float64(ph), float64(f.period)))*4) / 4
		if ph < 0 {
			ph += f.period
		}
		if ph == 0 {
			ph = 0 // not -0, whose bits differ
		}
		f.phase[i] = ph
	}
}

// dashAt returns where at pixels along f's dashes, repeated, falls: in
// dash or gap k, with left pixels of it to go.
func (f *flatPath) dashAt(at float32) (k int, left float32) {
	o := float32(math.Mod(float64(at), float64(f.period)))
	if o < 0 {
		o += f.period
	}
	// A point between a dash and a gap is in the gap, as Skia has it, but
	// one at a dash of no length is in that dash. Rounded, o may reach the
	// pattern's end: it goes round it once.
	for range f.dash {
		if o < f.dash[k] || o == 0 && f.dash[k] == 0 {
			break
		}
		o -= f.dash[k]
		k = (k + 1) % len(f.dash)
	}
	return k, max(f.dash[k]-o, 0)
}

// FillPathConicGradient fills a path with a conic gradient around its
// bounds.
func (p *Painter) FillPathConicGradient(path *Path, g ConicGradient) {
	f := &p.rt.paths.flat
	path.flatten(f, p.scale)
	p.drawPathPaint(f, 0, Color{R: 255, G: 255, B: 255, A: 255}, nil, &g)
}

// StrokePathConicGradient draws the outline of a path as StrokePath does,
// in a conic gradient around its bounds.
func (p *Painter) StrokePathConicGradient(path *Path, width float32, g ConicGradient) {
	f := &p.rt.paths.flat
	path.flatten(f, p.scale)
	// Trimmed short, it fades as its caps would (capFade).
	saved := p.opacity
	p.opacity *= f.capFade(width * p.scale)
	p.drawPathPaint(f, width*p.scale, Color{R: 255, G: 255, B: 255, A: 255}, nil, &g)
	p.opacity = saved
}

// StrokePathGradient draws the outline of a path as StrokePath does, in a
// gradient across its bounds.
func (p *Painter) StrokePathGradient(path *Path, width float32, g LinearGradient) {
	f := &p.rt.paths.flat
	path.flatten(f, p.scale)
	k := f.capFade(width * p.scale)
	g.From, g.To = g.From.Alpha(k), g.To.Alpha(k)
	p.drawPath(f, width*p.scale, g.From, &g)
}

func fillInto(z *vec.Rasterizer, f *flatPath, x0, y0 float32) {
	for i := range f.polys() {
		poly := f.poly(i)
		z.MoveTo(poly[0][0]-x0, poly[0][1]-y0)
		for _, pt := range poly[1:] {
			z.LineTo(pt[0]-x0, pt[1]-y0)
		}
		z.ClosePath()
	}
}

// strokeBufs is the memory strokeInto reuses: a polyline moved to the
// mask, and a dash of it, each grown to go there and back, and the first
// dash of a closed polyline, which waits for its last; and how many dashes
// and gaps the path is cut into so far (strokeCutsMax), here rather than
// on the stack, which dashLine, a function value, would move to the heap.
type strokeBufs struct {
	line, dash, head [][2]float32
	cuts             int
}

// strokeCutsMax is how many dashes and gaps strokeInto cuts a path into,
// at most, as SVG images are cut: dashes far shorter than a pixel would
// otherwise take as long as they like. Past them, it strokes the rest of
// the path whole, as such dashes look. It is a variable so that tests can
// reach it.
var strokeCutsMax = 1 << 16

// strokeInto draws a stroke hw pixels to each side of a flattened path,
// with its caps, joins and dashes, as an outline that the rasterizer
// fills: the side of a polyline, a turn around each outer corner and one
// through the corner on the inside, and back along the other side. Each
// edge of the stroke is drawn once, so that its antialiasing is exact,
// as rasterizing a piece per segment and per corner and adding them up is
// not: where their edges meet, as along the outside of curves, their
// coverage would add up and thicken the stroke.
func strokeInto(z *vec.Rasterizer, b *strokeBufs, f *flatPath, hw, x0, y0 float32) {
	b.cuts = 0
	for i := range f.polys() {
		// The polyline without repeated points, moved to the mask.
		pts := b.line[:0]
		for _, pt := range f.poly(i) {
			pts = appendPoint(pts, [2]float32{pt[0] - x0, pt[1] - y0})
		}
		closed := f.closed[i]
		if closed && len(pts) > 2 && pts[0] == pts[len(pts)-1] {
			pts = pts[:len(pts)-1]
		}
		if len(f.dash) == 0 || b.cuts >= strokeCutsMax {
			b.line = strokeLine(z, pts, closed, f, hw, [2]float32{1, 0})
			continue
		}
		b.line = pts
		dashLine(z, b, pts, closed, f, hw, f.phase[i])
	}
}

// dashLine and snapPhases are strokeDashes and snapDashPhases once a path
// is dashed (Path.Dash), and miterReach is mitredReach once a path has
// miter joins (Path.Join): apps that use neither don't link them. Only
// those methods set them, once, and only a path they set up calls them,
// after them.
var (
	dashLine   func(z *vec.Rasterizer, b *strokeBufs, pts [][2]float32, closed bool, f *flatPath, hw, phase float32)
	snapPhases func(f *flatPath)
	miterReach func(f *flatPath) float32
	linkDashes sync.Once
	linkMiters sync.Once
)

// strokeDashes draws the stroke of the dashes of a polyline without
// repeated points, phase pixels into their pattern where it starts, each
// an open polyline that goes on around the corners it reaches. A closed
// polyline's dash that runs through its first point is one dash, which
// turns there as at its other corners, and one that runs all around it is
// the polyline whole. It counts the dashes and gaps it cuts in b.cuts, and
// past strokeCutsMax of them strokes the rest of the polyline whole.
func strokeDashes(z *vec.Rasterizer, b *strokeBufs, pts [][2]float32, closed bool, f *flatPath, hw, phase float32) {
	k, left := f.dashAt(phase)
	on := k%2 == 0
	n := len(pts)
	segs := n - 1
	if closed && n > 1 {
		segs = n
	}
	// first is whether the next dash to end is the first, which a closed
	// polyline keeps in b.head if it starts on its first point.
	first, waiting := true, false
	dash := b.dash[:0]
	if on {
		dash = append(dash, pts[0])
	}
	dir := [2]float32{1, 0} // the way the line goes, for square caps of dashes of no length
	for s := range segs {
		a, c := pts[s], pts[(s+1)%n]
		l := dist(a, c)
		dir = [2]float32{(c[0] - a[0]) / l, (c[1] - a[1]) / l}
		pos := float32(0)
		for l-pos > left {
			pos += left
			t := pos / l
			pt := [2]float32{a[0] + (c[0]-a[0])*t, a[1] + (c[1]-a[1])*t}
			if on {
				dash = appendApart(dash, pt)
				if first && closed && len(dash) > 0 && dash[0] == pts[0] {
					b.head, dash = dash, b.head[:0]
					waiting = true
				} else {
					dash = strokeLine(z, dash, false, f, hw, dir)[:0]
				}
			} else {
				dash = append(dash, pt)
			}
			first = false
			if b.cuts++; b.cuts >= strokeCutsMax {
				// The rest, from here, whole.
				dash = append(dash[:0], pt)
				for j := s + 1; j <= segs; j++ {
					dash = appendApart(dash, pts[j%n])
				}
				dash = strokeLine(z, dash, false, f, hw, dir)[:0]
				if waiting {
					b.head = strokeLine(z, b.head, false, f, hw, dir)
				}
				b.dash = dash
				return
			}
			on, k = !on, (k+1)%len(f.dash)
			left = f.dash[k]
		}
		left -= l - pos
		if on {
			dash = appendApart(dash, c)
		}
	}
	switch {
	case on && first && closed:
		// No cut: the polyline whole.
		b.line = strokeLine(z, pts, true, f, hw, dir)
	case on && waiting:
		// The last dash ends on the first point, where the first starts.
		dash = strokeLine(z, append(dash, b.head[1:]...), false, f, hw, dir)
	default:
		if on {
			dash = strokeLine(z, dash, false, f, hw, dir)
		}
		if waiting {
			b.head = strokeLine(z, b.head, false, f, hw, dir)
		}
	}
	b.dash = dash[:0]
}

// appendApart appends pt to a dash unless it lies within a 64th of a
// pixel of the dash's last point: a line that short, where a dash was cut
// next to a corner, would point its cap or its join whichever way the
// rounding of the cut does.
func appendApart(dash [][2]float32, pt [2]float32) [][2]float32 {
	if n := len(dash); n > 0 && dist(dash[n-1], pt) < 1.0/64 {
		return dash
	}
	return append(dash, pt)
}

// appendPoint appends pt to pts unless pts ends with it.
func appendPoint(pts [][2]float32, pt [2]float32) [][2]float32 {
	if n := len(pts); n > 0 && pts[n-1] == pt {
		return pts
	}
	return append(pts, pt)
}

// strokeLine draws the stroke of a polyline without repeated points, hw
// pixels to each side, with f's caps and joins, and returns pts, which it
// grows. A polyline of one point shows its caps alone, square ones turned
// along dir, around it the way the outline of an open polyline goes, so
// that where they overlap, as dots on a line do, they add up rather than
// cancel.
func strokeLine(z *vec.Rasterizer, pts [][2]float32, closed bool, f *flatPath, hw float32, dir [2]float32) [][2]float32 {
	n := len(pts)
	switch {
	case n == 0:
	case n == 1:
		c := pts[0]
		switch f.cap {
		case RoundCap:
			disc(z, c[0], c[1], hw)
		case SquareCap:
			ux, uy, nx, ny := dir[0]*hw, dir[1]*hw, -dir[1]*hw, dir[0]*hw
			z.MoveTo(c[0]-ux+nx, c[1]-uy+ny)
			z.LineTo(c[0]+ux+nx, c[1]+uy+ny)
			z.LineTo(c[0]+ux-nx, c[1]+uy-ny)
			z.LineTo(c[0]-ux-nx, c[1]-uy-ny)
			z.ClosePath()
		}
	case closed && n > 2:
		// Each side of a closed polyline is a loop of its own, the other
		// side going the other way, so that the inside is not filled.
		offsetLoop(z, pts, hw, f, -1)
		for k := range n / 2 {
			pts[k], pts[n-1-k] = pts[n-1-k], pts[k]
		}
		offsetLoop(z, pts, hw, f, -1)
	default:
		// An open polyline is outlined as the loop that goes there and
		// back, whose turns at the ends are its caps. A closed one of two
		// points turns at both as at corners.
		end := n - 1
		if closed {
			end = -1
		}
		for k := n - 2; k > 0; k-- {
			pts = append(pts, pts[k])
		}
		offsetLoop(z, pts, hw, f, end)
	}
	return pts
}

// offsetLoop draws the side of a closed polyline hw pixels to its left
// (a segment's normal), turning around the outside of its corners as f's
// joins do and through the corners on the inside. With end ≥ 0, the
// polyline goes there and back along an open one, and turns straight back
// at its points 0 and end, the open one's ends, as f's caps do.
func offsetLoop(z *vec.Rasterizer, pts [][2]float32, hw float32, f *flatPath, end int) {
	n := len(pts)
	normal := func(k int) (float32, float32, float32, float32) {
		a, b := pts[k], pts[(k+1)%n]
		dx, dy := b[0]-a[0], b[1]-a[1]
		l := float32(math.Hypot(float64(dx), float64(dy)))
		dx, dy = dx/l, dy/l
		return dx, dy, -dy * hw, dx * hw
	}
	dx, dy, nx, ny := normal(0)
	z.MoveTo(pts[0][0]+nx, pts[0][1]+ny)
	for k := range n {
		c := (k + 1) % n
		b := pts[c]
		z.LineTo(b[0]+nx, b[1]+ny)
		ex, ey, mx, my := normal(c)
		// Rounded apart, as fused multiply-adds would leave a polyline
		// turning straight back, a cap's turn, a hair off zero.
		cross, dot := float32(dx*ey)-float32(dy*ex), float32(dx*ex)+float32(dy*ey)
		switch {
		case end >= 0 && (c == 0 || c == end):
			// An end, where the loop turns straight back: its cap.
			switch f.cap {
			case RoundCap:
				arc(z, b, hw, nx, ny, -math.Pi)
			case SquareCap:
				z.LineTo(b[0]+nx+dx*hw, b[1]+ny+dy*hw)
				z.LineTo(b[0]+mx+dx*hw, b[1]+my+dy*hw)
				z.LineTo(b[0]+mx, b[1]+my)
			default:
				z.LineTo(b[0]+mx, b[1]+my)
			}
		case cross > 0:
			// The inside of the corner: through it, where the stroke
			// covers itself.
			z.LineTo(b[0], b[1])
			z.LineTo(b[0]+mx, b[1]+my)
		case cross < 0 || dot < 0:
			// The outside, or the polyline turning back.
			switch {
			case f.join == RoundJoin:
				// An arc around the corner, the shorter way, or the way
				// the segment went.
				turn := math.Atan2(float64(cross), float64(dot))
				if cross == 0 {
					turn = -math.Pi
				}
				arc(z, b, hw, nx, ny, turn)
			case f.join == MiterJoin && 1+dot >= 2/(f.miter*f.miter):
				// The point where the sides meet, (n+m)/(1+dot) from the
				// corner for its normals n and m, 1/cos of half the turn
				// times hw from it: within the miter limit.
				q := 1 / (1 + dot)
				z.LineTo(b[0]+(nx+mx)*q, b[1]+(ny+my)*q)
				z.LineTo(b[0]+mx, b[1]+my)
			default:
				// Bevelled, as a miter too long is.
				z.LineTo(b[0]+mx, b[1]+my)
			}
		}
		dx, dy, nx, ny = ex, ey, mx, my
	}
	z.ClosePath()
}

// arc draws an arc of radius r around c, from (c[0]+x, c[1]+y) turning
// turn radians, in lines that stray from it by no more than 0.02 pixels.
func arc(z *vec.Rasterizer, c [2]float32, r, x, y float32, turn float64) {
	from := math.Atan2(float64(y), float64(x))
	steps := int(math.Ceil(math.Abs(turn) / (2 * math.Acos(max(1-0.02/float64(r), -1)))))
	for s := 1; s <= steps; s++ {
		a := from + turn*float64(s)/float64(steps)
		sin, cos := math.Sincos(a)
		z.LineTo(c[0]+r*float32(cos), c[1]+r*float32(sin))
	}
}

// disc draws a circle of radius r, in four cubic curves, around it the
// way the outline of an open polyline goes.
func disc(z *vec.Rasterizer, cx, cy, r float32) {
	const k = 0.5522847498
	z.MoveTo(cx+r, cy)
	z.CubeTo(cx+r, cy-r*k, cx+r*k, cy-r, cx, cy-r)
	z.CubeTo(cx-r*k, cy-r, cx-r, cy-r*k, cx-r, cy)
	z.CubeTo(cx-r, cy+r*k, cx-r*k, cy+r, cx, cy+r)
	z.CubeTo(cx+r*k, cy+r, cx+r, cy+r*k, cx+r, cy)
	z.ClosePath()
}
