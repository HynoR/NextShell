package terminal

import (
	"math"
	"runtime"
	"strings"

	"github.com/egoist/mygo/ui"
)

// Font is the font of a terminal's text, with what Ghostty's font options
// set.
type Font struct {
	// Family is the font family, a comma-separated list as
	// ui.Element.Font takes it, to which ui.RegisterFont adds the app's
	// own fonts: "monospace", the system's, when empty.
	Family string
	// Size is the size in DIPs: 13 when zero.
	Size float32
	// Weight is how heavy text is, from 100 (thin) to 900 (black), of the
	// weights the family has: 400 when zero. Bold text is 300 heavier, and
	// at least 700.
	Weight int
	// Features turn OpenType features of the font on and off by tag, as
	// Ghostty's font-feature: "ss01" or "+ss01" turns one on, "calt=0" or
	// "-calt" off, and "cv05=2" picks an alternate. Coding fonts make
	// most of their ligatures with calt, and the others with liga.
	// On macOS, kerning is off unless they name kern: each character
	// keeps its place in its cells.
	Features []string
	// CellWidth is the width of columns, as a multiple of the font's
	// advance: 1 when zero or less. It changes the grid, not the font's
	// size or the height of rows: glyphs keep their size, centered in
	// their cells, and below 1 they overlap. Widths round up to whole
	// display pixels.
	CellWidth float32
	// LineHeight is the height of rows, as a multiple of the font's own
	// line height: 1 when zero, and 1.2 rows 20% taller, as Ghostty's
	// adjust-cell-height = 20%. Text is centered in its row.
	LineHeight float32
	// Thicken draws text with a thicker stroke, as Ghostty's font-thicken:
	// on macOS, with Core Text's font smoothing at its strongest, whatever
	// the text's color and the system's setting. It changes nothing on
	// Linux and Windows.
	Thicken bool
}

// fontKey is a Font at a scale, with the defaults, comparable.
type fontKey struct {
	family     string
	size       float32
	weight     int
	features   string
	cellWidth  float32
	lineHeight float32
	thicken    bool
	scale      float32
}

func (f *Font) key(scale float32) fontKey {
	k := fontKey{family: f.Family, size: f.Size, weight: f.Weight, features: featureList(f.Features), cellWidth: f.CellWidth, lineHeight: f.LineHeight, thicken: f.Thicken, scale: scale}
	if runtime.GOOS == "darwin" {
		k.features = unkerned(k.features)
	}
	if strings.TrimSpace(k.family) == "" {
		k.family = "monospace"
	}
	if k.size <= 0 {
		k.size = 13
	}
	if k.weight <= 0 {
		k.weight = 400
	}
	k.weight = min(max(k.weight, 100), 900)
	if !(k.cellWidth > 0) || math.IsInf(float64(k.cellWidth), 0) {
		k.cellWidth = 1
	}
	if k.lineHeight <= 0 {
		k.lineHeight = 1
	}
	return k
}

// variant returns the font of a variant of text: bold (1) and italic (2).
func (k fontKey) variant(i int) ui.Font {
	w := k.weight
	if i&1 != 0 {
		w = min(max(w+300, 700), 900)
	}
	return ui.Font{Family: k.family, Size: k.size, Weight: w, Italic: i&2 != 0, Features: k.features, Thicken: k.thicken}
}

// featureList converts features as Font.Features has them into the list
// ui.Font.Features takes.
func featureList(features []string) string {
	var out []string
	for _, f := range features {
		f = strings.TrimSpace(f)
		if tag, ok := strings.CutPrefix(f, "-"); ok {
			f = strings.TrimSpace(tag) + "=0"
		} else if tag, ok := strings.CutPrefix(f, "+"); ok {
			f = strings.TrimSpace(tag)
		}
		if f != "" {
			out = append(out, f)
		}
	}
	return strings.Join(out, ",")
}

// unkerned adds kern=0 to a list of features that does not name kern.
// Kerning moves characters within their cells, and Core Text shrinks the
// CJK punctuation of the system's fonts that ends a run, as a 。 being
// typed does, to half its width: centered in its two cells, it jumps
// when text follows. Only macOS needs it: elsewhere, a list of features
// could cost the font its default ones, as DirectWrite's typography
// might.
func unkerned(list string) string {
	for _, f := range strings.Split(list, ",") {
		tag, _, _ := strings.Cut(f, "=")
		if strings.TrimSpace(tag) == "kern" {
			return list
		}
	}
	if list == "" {
		return "kern=0"
	}
	return "kern=0," + list
}
