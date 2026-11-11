package scene

import (
	"math"
	"sync/atomic"
)

// Translation returns how far m moves, when it only translates.
func (m Affine) Translation() (x, y float32, ok bool) {
	if !m.Set || m.A != 1 || m.B != 0 || m.C != 0 || m.D != 1 || m.G != 0 || m.H != 0 {
		return 0, 0, false
	}
	return m.X, m.Y, true
}

// TranslationFolder folds the transforms of a scene's ops that only
// translate (an element's Translate, which animations move) into their
// geometry, reusing its memory from scene to scene: renderers then draw
// them as they draw ops without transforms, the GPU's with the shaders of
// those (gpu.Class), not the general one, and the CPU's without sampling
// pixels on a grid. Both renderers fold, so that they keep drawing the
// same pixels.
//
// Renderers fold once FoldTranslations was called, which package ui does
// where it makes translations: apps that never translate carry none of
// the code.
type TranslationFolder struct {
	s      Scene
	ops    []Op
	glyphs []Glyph
}

// foldFunc is TranslationFolder.fold.
type foldFunc func(f *TranslationFolder, s *Scene) *Scene

// fold is TranslationFolder.fold once FoldTranslations was called. It is
// atomic, as ui.Translation calls FoldTranslations on any goroutine while
// renderers read it on theirs.
var fold atomic.Pointer[foldFunc]

// FoldTranslations makes renderers fold the ops that only translate
// (TranslationFolder), from the next scene they draw. Package ui calls it
// as it builds a view, before drawing it, each frame, and wherever it makes
// a translation, on any goroutine: it sets fold only the first time, and
// costs a load after.
func FoldTranslations() {
	if fold.Load() == nil {
		f := foldFunc((*TranslationFolder).fold)
		fold.CompareAndSwap(nil, &f)
	}
}

// Fold returns s, or, when any of its ops only translates, a copy of it
// with those folded: fills and shadows, and images and glyphs moved by
// whole pixels, glyphs without transforms of their own (renderers round
// their places, a glyph's bitmap staying on whole pixels without a
// transform), and clips: their edges are computed where they fall, as a
// fill's, rather than rasterized into a mask each frame the clip moves.
// Effects and holes keep their transforms. The copy holds until the next
// Fold. Before FoldTranslations, it returns s.
func (f *TranslationFolder) Fold(s *Scene) *Scene {
	fn := fold.Load()
	if fn == nil {
		return s
	}
	return (*fn)(f, s)
}

func (f *TranslationFolder) fold(s *Scene) *Scene {
	first := -1
	for i := range s.Ops {
		if folds(s, &s.Ops[i]) {
			first = i
			break
		}
	}
	if first < 0 {
		return s
	}
	f.ops = append(f.ops[:0], s.Ops...)
	f.s = *s
	f.s.Ops = f.ops
	copied := false
	for i := first; i < len(f.ops); i++ {
		op := &f.ops[i]
		if !folds(s, op) {
			continue
		}
		x, y := op.Transform.X, op.Transform.Y
		op.Transform = Affine{}
		op.Rect.X, op.Rect.Y = op.Rect.X+x, op.Rect.Y+y
		switch op.Kind {
		case OpShadow:
			if !op.Cast.Empty() {
				op.Cast.X, op.Cast.Y = op.Cast.X+x, op.Cast.Y+y
			}
		case OpGlyphs:
			if !copied {
				f.glyphs = append(f.glyphs[:0], s.Glyphs...)
				f.s.Glyphs, copied = f.glyphs, true
			}
			for j := op.Start; j < op.End; j++ {
				g := &f.glyphs[j]
				g.X = float32(math.Round(float64(g.X))) + x
				g.Y = float32(math.Round(float64(g.Y))) + y
			}
		}
		if op.Kind == OpFill || op.Kind == OpGlyphs || op.Kind == OpPushClip {
			// The points of gradients, a clip's masking it among them.
			op.moveGradient(x, y)
		}
	}
	return &f.s
}

// folds reports whether Fold folds op of s.
func folds(s *Scene, op *Op) bool {
	x, y, ok := op.Transform.Translation()
	if !ok {
		return false
	}
	whole := x == float32(math.Trunc(float64(x))) && y == float32(math.Trunc(float64(y)))
	switch op.Kind {
	case OpFill, OpShadow, OpPushClip:
		return true
	case OpImage:
		// Images sample their texels where they fall, which the CPU's
		// renderer and the GPU's place apart between pixels.
		return whole
	case OpGlyphs:
		if !whole || op.Start < 0 || op.Start > op.End || op.End > int32(len(s.Glyphs)) {
			return false
		}
		for _, g := range s.Glyphs[op.Start:op.End] {
			if g.Transform.Set {
				return false
			}
		}
		return true
	}
	return false
}
