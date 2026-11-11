package text

import (
	"cmp"
	"encoding/binary"
	"image"
	"math"
	"slices"
	"strings"
	"sync"
	"unicode"
	"unsafe"

	"github.com/egoist/mygo/internal/scene"
)

// Style selects the font of a text.
type Style struct {
	// Family is a comma-separated list of font families, where "system-ui"
	// and "monospace" are the system's own; "" is the system UI font.
	Family string
	// Size in DIPs; 0 is 14.
	Size float32
	// Weight from 100 (thin) to 900 (black); 0 is 400.
	Weight int
	Italic bool
	// LineHeight is the height of a line as a multiple of Size; 0 takes
	// the font's own line spacing.
	LineHeight float32
	// LetterSpacing adds DIPs after every character; negative tightens.
	LetterSpacing float32
	// Features are OpenType features, comma separated: a tag turns one
	// on, and tag=value sets it, as "tnum, liga=0, salt=2".
	Features string
	// Lang is the language of the text, a BCP 47 tag as "ja" or
	// "zh-Hant": the system's text stack picks the glyphs of that
	// language, as the Han characters Chinese and Japanese write apart,
	// and falls back to fonts for it. Korean text breaks its lines
	// between words (keepWordsWhole). "" is the language of the System
	// (SetLanguage).
	Lang string
	// TabSize puts tab stops every TabSize spaces of the style's font,
	// letter spacing included, from the start of each line, as CSS's
	// tab-size, once System.UseTabSize was called; 0 leaves them where
	// the system's text stack puts them.
	TabSize float32

	// tabStop is the distance between tab stops in DIPs that TabSize
	// makes, which the System sets for engines as it shapes.
	tabStop float32
}

func (s Style) weight() int {
	if s.Weight <= 0 {
		return 400
	}
	return min(s.Weight, 999)
}

// FontSize returns the size in DIPs.
func (s Style) FontSize() float32 {
	if s.Size <= 0 {
		return 14
	}
	return s.Size
}

// Align is the horizontal alignment of the lines of a layout.
type Align uint8

const (
	// Start aligns lines to the left, or to the right in right-to-left
	// paragraphs.
	Start Align = iota
	Center
	End
	// Left and Right align lines so whatever their direction, as the
	// start and the end of an interface laid out right to left do.
	Left
	Right
)

// Params describe a text to lay out.
type Params struct {
	Text  string
	Style Style
	// Width wraps lines to this many DIPs; 0 or less only breaks lines at
	// newlines.
	Width float32
	// MaxLines truncates the text to that many lines, ending it with
	// Ellipsis ("…" when empty); 0 is unlimited.
	MaxLines int
	Ellipsis string
	// NoWrap breaks lines only at newlines, aligning them within Width
	// all the same.
	NoWrap bool
	Align  Align
	// RTL makes paragraphs without a strongly directional character, as
	// numbers or an empty line, right to left, as in an interface laid out
	// right to left; the others read in the direction of their first
	// strongly directional character, as Unicode's bidirectional
	// algorithm finds it by default (rules P2 and P3), whichever this is.
	RTL bool
	// KeepSpaces keeps the advance of whitespace at the end of wrapped
	// lines, as text editors do.
	KeepSpaces bool
	// NoBreakWords only breaks lines between words, letting a long word
	// overflow the width instead of breaking it.
	NoBreakWords bool
	// Indent moves the first line of the text this many DIPs from the
	// start of its paragraph's direction, the left or the right, and
	// wraps it that much narrower, as CSS's text-indent; 0 or less is
	// none. The lines after a newline are not indented.
	Indent float32
	// Justify stretches every line that Width wraps to Width, the
	// whitespace ending it left out, by widening the spaces between its
	// words and the room after Han characters and kana, as browsers
	// justify text, once System.UseJustify was called. Align places the
	// last line of each paragraph, and a line with nothing to widen.
	Justify bool
	// Balance wraps each paragraph that Width wraps in lines of about the
	// same length, as CSS's text-wrap: balance does: in as many lines as
	// it takes, wrapped at the narrowest width that keeps them as many,
	// rather than full lines and a short last one. The lines are aligned
	// within Width all the same. A paragraph that MaxLines cuts wraps as
	// it would without it.
	Balance bool
	// Pretty keeps the last line of each paragraph that Width wraps from
	// holding a single word, as CSS's text-wrap: pretty does at least: the
	// line before gives it a word, wrapped as wide as that leaves them,
	// the lines before as they are. A paragraph that Balance balances or
	// MaxLines cuts wraps as it would without it. It takes effect once
	// System.UsePretty was called.
	Pretty bool
	// Spans styles runs of the text apart from Style, as EncodeSpans
	// writes them.
	Spans string
}

// Span styles the runes of a text before End, after those of the spans
// before it. What it sets replaces the Style of the layout; Italic only
// turns italics on.
type Span struct {
	End           int
	Family        string
	Size          float32
	Weight        int
	Italic        bool
	LetterSpacing float32
	// SpacingSet makes LetterSpacing the span's when zero too, rather
	// than the text's, as CSS's letter-spacing: 0 on an inline box.
	SpacingSet bool
	Features   string
	// Lang is the language of the runes, as Style.Lang; "" keeps the
	// text's.
	Lang string
	// ObjectWidth and ObjectHeight, when ObjectWidth is set, make each
	// rune of the span, an object replacement character (U+FFFC), an
	// inline object: a box that wide, standing that high on the baseline,
	// which lines break around and which draws no glyph, as an image in a
	// rich text's line (Glyph.Object). ObjectDescent extends the box that
	// far below the baseline, as math's subscripts and fractions go.
	ObjectWidth, ObjectHeight, ObjectDescent float32
	// BaselineShift raises the span's glyphs and objects that many DIPs
	// above the baseline, or lowers them when negative, as superscripts
	// and subscripts are; the line makes room for them.
	BaselineShift float32
	// PaddingX widens the span that many DIPs at both ends of each line's
	// piece of it, as the horizontal padding of CSS's inline boxes with
	// box-decoration-break: clone: the glyphs move in, and the line, which
	// breaks to fit it, is that much wider.
	PaddingX float32
}

// style returns the style of the span's runes in a text of style base.
func (sp Span) style(base Style) Style {
	s := base
	if sp.Family != "" {
		s.Family = sp.Family
	}
	if sp.Size > 0 {
		s.Size = sp.Size
	}
	if sp.Weight > 0 {
		s.Weight = sp.Weight
	}
	if sp.Italic {
		s.Italic = true
	}
	if sp.LetterSpacing != 0 || sp.SpacingSet {
		s.LetterSpacing = sp.LetterSpacing
	}
	if sp.Features != "" {
		s.Features = sp.Features
	}
	if sp.Lang != "" {
		s.Lang = sp.Lang
	}
	return s
}

// EncodeSpans writes spans for Params.Spans, which, as a string, keeps
// Params a value layouts are cached by.
func EncodeSpans(spans []Span) string {
	var b []byte
	str := func(s string) {
		b = binary.AppendUvarint(b, uint64(len(s)))
		b = append(b, s...)
	}
	for _, sp := range spans {
		b = binary.AppendUvarint(b, uint64(max(sp.End, 0)))
		str(sp.Family)
		b = binary.LittleEndian.AppendUint32(b, math.Float32bits(sp.Size))
		b = binary.AppendUvarint(b, uint64(max(sp.Weight, 0)))
		// Italics, a letter spacing set, and whether a baseline shift and
		// a padding follow: other spans' keys stay short.
		var flags byte
		if sp.Italic {
			flags |= 1
		}
		if sp.BaselineShift != 0 {
			flags |= 2
		}
		if sp.SpacingSet {
			flags |= 4
		}
		if sp.PaddingX != 0 {
			flags |= 8
		}
		b = append(b, flags)
		if sp.BaselineShift != 0 {
			b = binary.LittleEndian.AppendUint32(b, math.Float32bits(sp.BaselineShift))
		}
		if sp.PaddingX != 0 {
			b = binary.LittleEndian.AppendUint32(b, math.Float32bits(sp.PaddingX))
		}
		b = binary.LittleEndian.AppendUint32(b, math.Float32bits(sp.LetterSpacing))
		str(sp.Features)
		b = binary.LittleEndian.AppendUint32(b, math.Float32bits(sp.ObjectWidth))
		b = binary.LittleEndian.AppendUint32(b, math.Float32bits(sp.ObjectHeight))
		if sp.ObjectWidth != 0 {
			// Only objects have a descent: other spans' keys stay short.
			b = binary.LittleEndian.AppendUint32(b, math.Float32bits(sp.ObjectDescent))
		}
		str(sp.Lang)
	}
	return string(b)
}

// DecodeSpans reads what EncodeSpans wrote.
func DecodeSpans(s string) []Span {
	b := []byte(s)
	var out []Span
	uvarint := func() int {
		v, n := binary.Uvarint(b)
		if n <= 0 {
			b = nil
			return 0
		}
		b = b[n:]
		return int(v)
	}
	str := func() string {
		n := uvarint()
		if n > len(b) {
			b = nil
			return ""
		}
		s := string(b[:n])
		b = b[n:]
		return s
	}
	f32 := func() float32 {
		if len(b) < 4 {
			b = nil
			return 0
		}
		v := math.Float32frombits(binary.LittleEndian.Uint32(b))
		b = b[4:]
		return v
	}
	for len(b) > 0 {
		var sp Span
		sp.End = uvarint()
		sp.Family = str()
		sp.Size = f32()
		sp.Weight = uvarint()
		var flags byte
		if len(b) > 0 {
			flags = b[0]
			b = b[1:]
		}
		sp.Italic = flags&1 != 0
		sp.SpacingSet = flags&4 != 0
		if flags&2 != 0 {
			sp.BaselineShift = f32()
		}
		if flags&8 != 0 {
			sp.PaddingX = f32()
		}
		sp.LetterSpacing = f32()
		sp.Features = str()
		sp.ObjectWidth = f32()
		sp.ObjectHeight = f32()
		if sp.ObjectWidth != 0 {
			sp.ObjectDescent = f32()
		}
		sp.Lang = str()
		if b == nil {
			break
		}
		out = append(out, sp)
	}
	return out
}

// spansIn returns the spans of runes start to end of a text, with their
// ends relative to start.
func spansIn(spans []Span, start, end int) []Span {
	var out []Span
	from := 0
	for _, sp := range spans {
		s0 := from
		from = max(sp.End, from)
		if from <= start {
			continue
		}
		if s0 >= end {
			break
		}
		sp.End = min(from, end) - start
		out = append(out, sp)
	}
	return out
}

// Layout is shaped and wrapped text. Positions are in DIPs relative to the
// top-left corner of the text.
type Layout struct {
	Params Params
	Runes  []rune
	Lines  []Line
	// Width is the width of the longest line, the first line's indent
	// included, Height the sum of the line heights.
	Width, Height float32
	boundaries    []int // retained whole-grapheme offsets for visual navigation
	// Truncated reports that MaxLines cut the text.
	Truncated bool
}

// Line is a line of a Layout.
type Line struct {
	// X, Y, Width and Height are the line's box; X includes the alignment.
	X, Y, Width, Height float32
	// Baseline is the y of the baseline; Ascent and Descent the extent of
	// the fonts above and below it.
	Baseline, Ascent, Descent float32
	// Start and End are the line's runes; a newline ending it is not
	// included.
	Start, End int
	// RTL reports a right-to-left paragraph.
	RTL bool
	// Glyphs are in visual order, from the left, on every engine: X
	// ascends but for shaping offsets, and Cluster descends through a
	// right-to-left run (Glyph.RTL). The glyphs of a cluster are next to
	// each other.
	Glyphs []Glyph

	carets   []float32
	stops    []caretStop
	upstream []float32
	// padding is the room the padding of spans' pieces takes in the line
	// (Span.PaddingX).
	padding float32
}

// Glyph is a positioned glyph.
type Glyph struct {
	Font *Font
	ID   uint32
	// Size is the font size in DIPs.
	Size float32
	// X is the glyph's drawing origin, including shaping offsets, Y its
	// baseline. Advance moves the pen independently of those offsets.
	X, Y    float32
	Advance float32
	// Cluster is the first rune of the glyph's cluster, Runes how many
	// runes the cluster holds; glyphs of the ellipsis have Runes 0.
	Cluster, Runes int
	// RTL marks a glyph of a right-to-left run, whose cluster's first
	// rune is on its right.
	RTL bool
	// Object marks the box of an inline object (Span.ObjectWidth), which
	// draws no glyph: what shows there is the caller's.
	Object bool
}

// System lays out text and rasterizes glyphs with the system's own text
// engine. Use Shared.
type System struct {
	mu  sync.Mutex
	eng engine
	// uiFamily is the family system-ui stands for where the engine does
	// not know it (SetUIFamily), and rendering the desktop's settings for
	// rasterizing text, if the app gave them (SetFontRendering).
	uiFamily  string
	rendering *[3]string
	// families are the system's font families as FontFamilies last listed
	// them, while listed; RegisterFont forgets them, counting familiesGen.
	families    []FontFamily
	listed      bool
	familiesGen uint64
	// lang is the language of texts whose style names none (SetLanguage).
	lang string
	// fonts caches the font of each style.
	fonts map[Style]*Font

	// Layouts are kept by recency within a memory budget, not by how
	// many frames a scrolling window has drawn.
	layouts        map[Params]*cached
	oldest, newest *cached
	layoutBytes    int
	frame          uint64
	// made counts the layouts made (LayoutsMade), and gen the times the
	// system forgot its layouts (Generation).
	made, gen uint64

	glyphs map[glyphKey]*atlasEntry
	places map[placeKey]placement
	// runs holds joined glyphs (GlyphRun).
	runs  map[runKey]*atlasEntry
	masks map[uint64]*atlasEntry
	// transient holds the masks drawn for the frame being painted alone,
	// recent the frame each of them was last drawn in, and transientUses
	// counts the times Mask returned one of them (TransientUses).
	transient     map[uint64]GlyphImage
	recent        map[uint64]uint64
	transientUses uint64
	// MaskAtlas holds coverage masks, ColorAtlas color glyphs.
	MaskAtlas, ColorAtlas *scene.Atlas
	// full tells which atlases (mask, color) left out something the frame
	// draws, and want how many pixels that needed; failed counts failed
	// allocations.
	full [2]bool
	want [2]int
	// wantSize is the largest bitmap left out of each atlas, including
	// padding: area alone does not tell whether a long, thin mask fits.
	wantSize [2]image.Point
	failed   int
	// outGlyphs, outRuns and outMasks are what the atlases left out since they
	// last could have room: a glyph a frame draws a thousand times counts
	// once in want, which sizes the atlas, and is rasterized once.
	outGlyphs map[glyphKey]struct{}
	outRuns   map[runKey]struct{}
	outMasks  map[uint64]struct{}
	// subpixel tells that the system's settings ask for subpixel
	// antialiasing (TextParams).
	subpixel bool
	// rooms counts MakeRoom calls during the frame.
	rooms int
	// Buffers layouts reuse: the first runes of clusters (line), and the
	// advances and text of a line ending with an ellipsis (ellipsize).
	starts     []int
	advances   []float32
	ellipsized []rune
	// marks caches the width of the ellipsis of each style.
	marks map[markKey]float32
	// justify is justifyLine once UseJustify was called, so that apps
	// that never justify text do not link it.
	justify func(line *Line, runes []rune, width float32) (left float32, ok bool)
	// pretty is prettyLines once UsePretty was called, so that apps that
	// never ask for it do not link it.
	pretty func(s *System, text []rune, p Params, spans []Span, width, indent float32, rtl bool, hy *hyphens, shaped []shapedLine) []shapedLine
	// tabs is tabStop once UseTabSize was called, so that apps that never
	// set a tab size do not link it.
	tabs func(s *System, style Style) float32
}

type markKey struct {
	style Style
	mark  string // Params.Ellipsis
	rtl   bool
	// space keys the advance of a space of the style instead (tabStop).
	space bool
}

// cached is a layout, with room for one line, as most layouts have, and
// the frame that used it last.
type cached struct {
	layout     Layout
	line       [1]Line
	used       uint64
	bytes      int
	prev, next *cached
}

var shared = sync.OnceValue(newSystem)

func newSystem() *System {
	return &System{
		fonts:     map[Style]*Font{},
		layouts:   map[Params]*cached{},
		marks:     map[markKey]float32{},
		glyphs:    map[glyphKey]*atlasEntry{},
		places:    map[placeKey]placement{},
		runs:      map[runKey]*atlasEntry{},
		masks:     map[uint64]*atlasEntry{},
		transient: map[uint64]GlyphImage{},
		recent:    map[uint64]uint64{},
		MaskAtlas: scene.NewAtlas(1, 256, 256),
		// Most interfaces have no color or subpixel glyphs. Keep just a
		// transparent texel until one asks for room.
		ColorAtlas: scene.NewAtlas(4, 1, 1),
	}
}

// Shared returns the process-wide text system.
func Shared() *System { return shared() }

// engine starts the system's text engine on first use.
func (s *System) engine() engine {
	if s.eng == nil {
		s.eng = newEngine()
		if u, ok := s.eng.(uiFamilySetter); ok {
			u.setUIFamily(s.uiFamily)
		}
		if f, ok := s.eng.(fontRenderer); ok && s.rendering != nil {
			r := s.rendering
			f.setFontRendering(r[0], r[1], r[2])
		}
	}
	return s.eng
}

// maxFonts is how many Fonts an engine keeps before the System lets them
// go: a Font holds a font of the system at one size.
const maxFonts = 512

// fontForgetter is an engine whose Fonts the System lets go of when they
// are many. forgetFonts forgets the Fonts made so far without releasing
// their native fonts, which layouts kept elsewhere may still draw with;
// releaseFonts releases those of the Fonts nothing refers to anymore
// (nativeFonts).
type fontForgetter interface {
	fontCount() int
	forgetFonts()
	releaseFonts()
}

// shapingKeeper is an engine that keeps what shaping a text took across
// frames: endFrame runs after each frame, and releaseShaping as windows go
// idle.
type shapingKeeper interface {
	endFrame()
	releaseShaping()
}

// uiFamilySetter is an engine that takes the family of the desktop's
// interface font from the app.
type uiFamilySetter interface{ setUIFamily(family string) }

// fontRenderer is an engine that takes the desktop's settings for
// rasterizing text from the app (platform.FontRendering).
type fontRenderer interface {
	setFontRendering(antialias, hinting, subpixels string)
}

// SetFontRendering sets how text is rasterized where the system's text
// stack does not know the desktop's settings, as Pango on Linux:
// antialiasing ("none", "gray" or "subpixel"), hinting ("none", "slight",
// "medium" or "full") and the order of the screen's subpixels ("rgb",
// "bgr", "vrgb" or "vbgr"), "" for the default. Other engines ignore it.
func (s *System) SetFontRendering(antialias, hinting, subpixels string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := [3]string{antialias, hinting, subpixels}
	if s.rendering != nil && *s.rendering == r {
		return
	}
	s.rendering = &r
	// An engine started already takes them now, and forgets what it drew.
	if f, ok := s.eng.(fontRenderer); ok {
		f.setFontRendering(antialias, hinting, subpixels)
		s.gen++
		clear(s.fonts)
		s.clearLayouts()
		clear(s.marks)
		clear(s.glyphs)
		clear(s.places)
		clear(s.runs)
	}
}

// SetUIFamily sets the family "system-ui" stands for where the system's
// text stack does not know the desktop's interface font, as Pango on
// Linux, which finds fontconfig's default sans-serif. Other engines ignore
// it.
func (s *System) SetUIFamily(family string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if family == s.uiFamily {
		return
	}
	s.uiFamily = family
	// An engine started already takes it now, and forgets what it found.
	if u, ok := s.eng.(uiFamilySetter); ok {
		u.setUIFamily(family)
		s.gen++
		clear(s.fonts)
		s.clearLayouts()
		clear(s.marks)
	}
}

// SetLanguage sets the language of the texts whose Style names none, a
// BCP 47 tag, or "" for none: the system's text stack then picks glyphs
// and fallback fonts as it does by default, by the user's settings. The
// ui package sets the language the app shows. The layouts made before are
// made anew (Generation).
func (s *System) SetLanguage(tag string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if tag == s.lang {
		return
	}
	s.lang = tag
	s.gen++
	s.clearLayouts()
	clear(s.marks)
}

// Language returns the language SetLanguage set.
func (s *System) Language() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lang
}

// UseJustify lets layouts justify their lines (Params.Justify), which
// package ui calls as the first TextShape asks for it.
func (s *System) UseJustify() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.justify = justifyLine
}

// UsePretty lets layouts keep the last line of a paragraph from holding
// a single word (Params.Pretty), which package ui calls as the first
// element or TextShape asks for it.
func (s *System) UsePretty() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pretty = (*System).prettyLines
}

// UseTabSize lets layouts set their tab stops (Style.TabSize), which
// package ui calls as the first element sets a tab size.
func (s *System) UseTabSize() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tabs = (*System).tabStop
}

// RegisterFont adds a TrueType or OpenType font (or collection) to the
// fonts text can use, under family, or the font's own family name when
// family is empty.
func (s *System) RegisterFont(data []byte, family string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.engine().register(data, family); err != nil {
		return err
	}
	// The system's families may hold the app's under their own names.
	s.families, s.listed = nil, false
	s.familiesGen++
	s.gen++
	clear(s.fonts)
	s.clearLayouts()
	clear(s.marks)
	return nil
}

// A FontFamily is a font family text can use.
type FontFamily struct {
	Name string
	// Monospaced is whether its regular font gives every character the
	// same advance.
	Monospaced bool
	// Registered is whether the app registered it (RegisterFont).
	Registered bool
}

// FontFamilies lists the families text can use: the app's registered ones
// first, under the names it gave them, or their own when it gave none,
// then the system's, each once and sorted without regard to case. The
// system's are listed once, outside the lock, and kept until RegisterFont.
func (s *System) FontFamilies() []FontFamily {
	s.mu.Lock()
	app := s.engine().appFamilies()
	system, listed, gen := s.families, s.listed, s.familiesGen
	var list func() []FontFamily
	if !listed {
		list = s.eng.systemFamilies()
	}
	s.mu.Unlock()
	if !listed {
		if list != nil {
			system = list()
		}
		s.mu.Lock()
		if s.familiesGen == gen {
			s.families, s.listed = system, true
		}
		s.mu.Unlock()
	}
	seen := map[string]int{} // the index of each family, by lowercased name
	var out []FontFamily
	add := func(families []FontFamily, registered bool) {
		from := len(out)
		for _, f := range families {
			key := strings.ToLower(f.Name)
			i, ok := seen[key]
			switch {
			case f.Name == "":
			case !ok:
				f.Registered = registered
				seen[key] = len(out)
				out = append(out, f)
			case i >= from:
				// Fonts of one family, as its bold after its regular: it is
				// monospaced if one of them is.
				out[i].Monospaced = out[i].Monospaced || f.Monospaced
			}
		}
		slices.SortFunc(out[from:], func(a, b FontFamily) int {
			return cmp.Or(strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)), strings.Compare(a.Name, b.Name))
		})
	}
	add(app, true)
	add(system, false)
	return out
}

// Preload starts the text engine and finds the system UI font, which the
// first layout otherwise waits for.
func (s *System) Preload() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.font(Style{})
}

// EndFrame forgets layouts no frame used for a while. The ui package calls
// it after each frame.
func (s *System) EndFrame() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, f := range s.recent {
		// Windows take turns: a mask drawn again within a few frames is
		// probably drawn by every frame of its window.
		if s.frame-f >= 8 {
			delete(s.recent, k)
		}
	}
	s.frame++
	if k, ok := s.eng.(shapingKeeper); ok {
		k.endFrame()
	}
	f, ok := s.eng.(fontForgetter)
	if ok {
		f.releaseFonts()
	}
	if ok && f.fontCount() > maxFonts {
		// Fonts pile up with sizes, as with animated ones: let them all go,
		// with what refers to them. The atlas takes back the room of their
		// glyphs as it makes room; the next frame lays out and draws its
		// text anew. Layouts kept elsewhere keep their Fonts, which still
		// draw, and go once nothing refers to them.
		s.gen++
		s.clearLayouts()
		clear(s.marks)
		clear(s.fonts)
		clear(s.glyphs)
		clear(s.places)
		clear(s.runs)
		f.forgetFonts()
		return
	}
	for s.oldest != nil && s.overLayoutBudget(0) {
		s.removeLayout(s.oldest)
	}
	for s.oldest != nil && s.frame-s.oldest.used > 240 {
		s.removeLayout(s.oldest)
	}
}

// ReleaseShaping lets go of what the engine keeps of text shaped lately, for
// when a window goes idle: the next frames shape it again.
func (s *System) ReleaseShaping() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if k, ok := s.eng.(shapingKeeper); ok {
		k.releaseShaping()
	}
}

// Layout shapes and wraps p.Text. The result is shared and must not be
// modified.
func (s *System) Layout(p Params) *Layout { return s.LayoutFrom(nil, p) }

// LayoutFrom lays out p as Layout does, from l, if not nil, a layout of the
// same text at another width, as a box growing or shrinking lays its text
// out at each width: where l wraps none of its lines and fits p's width, p
// lays out alike, its lines aligned anew, rather than shaped again; at the
// left of their box, it shares them. So does the text's layout without a
// width, once made.
func (s *System) LayoutFrom(l *Layout, p Params) *Layout {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.layouts[p]; ok {
		s.useLayout(c)
		return &c.layout
	}
	if l == nil || !alike(l, p) {
		l = nil
		if u, ok := s.layouts[unwrappedParams(p)]; ok && p.Width > 0 && alike(&u.layout, p) {
			s.useLayout(u)
			l = &u.layout
		}
	}
	var c *cached
	if l != nil {
		c = &cached{layout: *l, bytes: int(unsafe.Sizeof(cached{}))}
		c.layout.Params.Width, c.layout.Params.Justify, c.layout.Params.Balance, c.layout.Params.Pretty = p.Width, p.Justify, p.Balance, p.Pretty
		c.bytes += realign(&c.layout, l)
	} else {
		// A short label may be a slice of a large document. Cache owned
		// strings so the memory estimate reflects what those parameters
		// keep alive.
		p.Text = strings.Clone(p.Text)
		p.Spans = strings.Clone(p.Spans)
		p.Ellipsis = strings.Clone(p.Ellipsis)
		p.Style.Family = strings.Clone(p.Style.Family)
		p.Style.Features = strings.Clone(p.Style.Features)
		p.Style.Lang = strings.Clone(p.Style.Lang)
		c = s.layout(p)
		c.bytes = layoutBytes(c)
	}
	// A large paragraph is kept only where it fits next to the layouts
	// recent frames used: evicting those for it would have the ordinary
	// labels and lines of other windows laid out again. Otherwise it still
	// lays out correctly, every time.
	if c.bytes <= maxLayoutBytes || c.bytes <= maxLargeLayoutBytes && s.roomBesideLive(c.bytes) {
		for s.oldest != nil && (s.overLayoutBudget(c.bytes) || len(s.layouts) >= maxLayouts) {
			s.removeLayout(s.oldest)
		}
		s.layouts[c.layout.Params] = c
		s.layoutBytes += c.bytes
		s.useLayout(c)
	}
	return &c.layout
}

// alike reports whether p lays out as l, of the same text, but for where
// its alignment puts its lines (realign): l wraps none of them, MaxLines
// cuts none, and they fit p's width, or p does not wrap them.
func alike(l *Layout, p Params) bool {
	q := l.Params
	if q.MaxLines > 0 || unwrappedParams(q) != unwrappedParams(p) || !p.NoWrap && p.Width > 0 && l.Width > p.Width {
		return false
	}
	for _, line := range l.Lines {
		if line.End < len(l.Runes) && l.Runes[line.End] != '\n' {
			return false // wrapped
		}
	}
	return true
}

// realign moves the lines of n, a copy of l laid out at another width, as
// their alignment puts them in their box at that width, copying those it
// moves, and returns how many bytes it copied.
func realign(n, l *Layout) int {
	box := func(p Params) float32 {
		if p.Width > 0 {
			return p.Width
		}
		return l.Width
	}
	from, to := box(l.Params), box(n.Params)
	if from == to {
		return 0
	}
	bytes := 0
	for i := range l.Lines {
		d := alignedAt(l, i, n.Params, to) - alignedAt(l, i, l.Params, from)
		if d == 0 {
			continue
		}
		if bytes == 0 {
			n.Lines = slices.Clone(l.Lines)
			bytes = len(n.Lines) * int(unsafe.Sizeof(Line{}))
		}
		line := &n.Lines[i]
		line.X += d
		line.Glyphs = slices.Clone(line.Glyphs)
		for j := range line.Glyphs {
			line.Glyphs[j].X += d
		}
		// Its carets and stops, laid out where it was, are found anew.
		line.carets, line.stops, line.upstream = nil, nil, nil
		bytes += len(line.Glyphs) * int(unsafe.Sizeof(Glyph{}))
	}
	return bytes
}

// alignedAt returns how far right of its box's left line i of l is, laid
// out with p in a box as wide as box, as layout aligns it.
func alignedAt(l *Layout, i int, p Params, box float32) float32 {
	line := &l.Lines[i]
	lo, hi := float32(0), box
	if indent := max(p.Indent, 0); i == 0 {
		if line.RTL {
			hi -= indent
		} else {
			lo += indent
		}
	}
	align := p.Align
	if line.RTL {
		switch align {
		case Start:
			align = End
		case End:
			align = Start
		}
	}
	switch align {
	case Center:
		return lo + (hi-lo-line.Width)/2
	case End, Right:
		return hi - line.Width
	}
	return lo
}

// unwrappedParams returns p without a width.
func unwrappedParams(p Params) Params {
	p.Width, p.Justify, p.Balance, p.Pretty = 0, false, false, false
	return p
}

// The memory estimate includes text, shaped glyphs and room for lazy caret
// geometry. A count limit also bounds the map for many very short labels.
// A layout above maxLayoutBytes is kept, up to maxLargeLayoutBytes, only
// within the live budget beside the layouts recent frames used.
const (
	liveLayoutFrames    = 8
	maxLiveLayoutBytes  = 64 << 20
	maxLayoutBytes      = 8 << 20
	maxLargeLayoutBytes = maxLiveLayoutBytes / 2
	maxLayouts          = 4096
)

// roomBesideLive tells whether n more bytes fit within the live budget by
// evicting only layouts no frame used within liveLayoutFrames. The list
// is in order of use, so those are the oldest.
func (s *System) roomBesideLive(n int) bool {
	free, count := maxLiveLayoutBytes-s.layoutBytes, len(s.layouts)
	for c := s.oldest; free < n || count >= maxLayouts; c = c.next {
		if c == nil || s.frame-c.used < liveLayoutFrames {
			return false
		}
		free, count = free+c.bytes, count-1
	}
	return true
}

// Above the ordinary budget, keep layouts used within a few global frames:
// otherwise sequential reuse of a larger working set evicts each layout just
// before its next use. The hard cap and entry limit still apply to live text.
// EndFrame also trims excess once later frames stop using it; no timer runs
// while all windows are idle.
func (s *System) overLayoutBudget(n int) bool {
	return s.layoutBytes+n > maxLiveLayoutBytes ||
		s.layoutBytes+n > maxLayoutBytes && s.oldest != nil && s.frame-s.oldest.used >= liveLayoutFrames
}

func layoutBytes(c *cached) int {
	l, p := &c.layout, c.layout.Params
	n := int(unsafe.Sizeof(*c)+unsafe.Sizeof(p)) + len(p.Text) + len(p.Spans) + len(p.Ellipsis) + len(p.Style.Family) + len(p.Style.Features) + len(p.Style.Lang)
	n += cap(l.Runes) * int(unsafe.Sizeof(rune(0)))
	if cap(l.Lines) > 1 {
		n += cap(l.Lines) * int(unsafe.Sizeof(Line{}))
	}
	// Selection can create boundaries, caret positions and visual stops
	// after insertion; reserve their room before caching the layout.
	n += (len(l.Runes) + 1) * int(unsafe.Sizeof(int(0)))
	for _, line := range l.Lines {
		n += cap(line.Glyphs) * int(unsafe.Sizeof(Glyph{}))
		n += (line.End - line.Start + 1) * 2 * int(unsafe.Sizeof(float32(0))+unsafe.Sizeof(caretStop{}))
	}
	return n
}

func (s *System) unlinkLayout(c *cached) {
	if c.prev != nil {
		c.prev.next = c.next
	} else {
		s.oldest = c.next
	}
	if c.next != nil {
		c.next.prev = c.prev
	} else {
		s.newest = c.prev
	}
	// Returned layouts may outlive the cache. Do not let them retain it.
	c.prev, c.next = nil, nil
}

func (s *System) useLayout(c *cached) {
	c.used = s.frame
	if s.newest == c {
		return
	}
	if c.prev != nil || c.next != nil || s.oldest == c {
		s.unlinkLayout(c)
	}
	c.prev = s.newest
	if s.newest != nil {
		s.newest.next = c
	} else {
		s.oldest = c
	}
	s.newest = c
}

func (s *System) removeLayout(c *cached) {
	s.unlinkLayout(c)
	delete(s.layouts, c.layout.Params)
	s.layoutBytes -= c.bytes
}

func (s *System) clearLayouts() {
	for s.oldest != nil {
		s.unlinkLayout(s.oldest)
	}
	clear(s.layouts)
	s.layoutBytes = 0
}

// LayoutsMade returns how many layouts the system has made, those Layout
// found in its cache left out.
func (s *System) LayoutsMade() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.made
}

// Generation returns a number that changes whenever the system forgets
// its layouts, as it lets go of its fonts (EndFrame) or takes other ones:
// the layouts it returned before are then to be made anew. Until then they
// still draw, with the fonts they were made with.
func (s *System) Generation() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gen
}

// Shape lays out p.Text as Layout does, but caches nothing, for text that
// its caller keeps or rarely draws twice. The result belongs to the
// caller.
func (s *System) Shape(p Params) *Layout {
	s.mu.Lock()
	defer s.mu.Unlock()
	return &s.layout(p).layout
}

// Metrics returns the ascent, descent and default line height of a style.
func (s *System) Metrics(style Style) (ascent, descent, lineHeight float32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := metricsOf(s.font(style), style)
	return m.ascent, m.descent, m.lineHeight
}

// font returns the font of a style.
func (s *System) font(style Style) *Font {
	key := Style{Family: style.Family, Size: style.FontSize(), Weight: style.weight(), Italic: style.Italic}
	if f, ok := s.fonts[key]; ok {
		return f
	}
	f := s.engine().font(key)
	if len(s.fonts) >= 1024 {
		clear(s.fonts)
	}
	s.fonts[key] = f
	return f
}

// lineMetrics are the vertical metrics of a style's lines.
type lineMetrics struct {
	ascent, descent, lineHeight float32
}

func metricsOf(f *Font, style Style) lineMetrics {
	size := style.FontSize()
	m := lineMetrics{ascent: size * 0.8, descent: size * 0.2}
	gap := float32(0)
	if f != nil && f.Ascent > 0 {
		m.ascent, m.descent, gap = f.Ascent, f.Descent, max(f.LineGap, 0)
	}
	m.lineHeight = m.ascent + m.descent + gap
	if style.LineHeight > 0 {
		m.lineHeight = style.LineHeight * size
	}
	return m
}

func (s *System) layout(p Params) *cached {
	s.made++
	s.engine().resetScratch()
	m := metricsOf(s.font(p.Style), p.Style)
	runes := []rune(p.Text)
	spans := DecodeSpans(p.Spans)
	c := &cached{layout: Layout{Params: p, Runes: runes}}
	l := &c.layout
	// The layout keeps its parameters as they came; the engine shapes in
	// the system's language where they name none.
	if p.Style.Lang == "" {
		p.Style.Lang = s.lang
	}
	if p.Style.TabSize > 0 && s.tabs != nil && strings.ContainsRune(p.Text, '\t') {
		p.Style.tabStop = s.tabs(s, p.Style)
	}
	l.Lines = c.line[:0]
	y := float32(0)
	for start := 0; start <= len(runes); {
		end := start
		for end < len(runes) && runes[end] != '\n' {
			end++
		}
		maxLines := 0
		if p.MaxLines > 0 {
			maxLines = p.MaxLines - len(l.Lines)
		}
		var truncated bool
		l.Lines, truncated = s.paragraph(l.Lines, p, spans, runes, start, end, maxLines, end < len(runes), m, &y)
		if truncated {
			l.Truncated = true
			break
		}
		start = end + 1
	}
	indent := max(p.Indent, 0)
	for i, line := range l.Lines {
		if i == 0 {
			line.Width += indent
		}
		l.Width = max(l.Width, line.Width)
	}
	l.Height = y
	// Align each line within the wrap width, or the longest line, the
	// first beside its indent.
	box := l.Width
	if p.Width > 0 {
		box = p.Width
	}
	justify, justified := p.Justify && p.Width > 0 && !p.NoWrap && s.justify != nil, false
	for i := range l.Lines {
		line := &l.Lines[i]
		lo, hi := float32(0), box
		if i == 0 {
			if line.RTL {
				hi -= indent
			} else {
				lo += indent
			}
		}
		align := p.Align
		if line.RTL {
			switch align {
			case Start:
				align = End
			case End:
				align = Start
			}
		}
		var dx float32
		// The last line of a paragraph, and a line an ellipsis ends, keep
		// the alignment.
		last := line.End == len(runes) || runes[line.End] == '\n' || l.Truncated && i == len(l.Lines)-1
		if justify && !last {
			if left, ok := s.justify(line, runes, hi-lo); ok {
				// Its words fill the room, whatever its whitespace's end.
				align, lo, justified = Left, lo-left, true
			}
		}
		switch align {
		case Center:
			dx = lo + (hi-lo-line.Width)/2
		case End, Right:
			dx = hi - line.Width
		default:
			dx = lo
		}
		if dx != 0 {
			line.X += dx
			for j := range line.Glyphs {
				line.Glyphs[j].X += dx
			}
		}
	}
	if justified {
		// The lines stretched are as wide as the width, or wider by the
		// whitespace they keep.
		for i, line := range l.Lines {
			if i == 0 {
				line.Width += indent
			}
			l.Width = max(l.Width, line.Width)
		}
	}
	return c
}

// tabStop returns the distance between the tab stops of a style, TabSize
// spaces of its font.
func (s *System) tabStop(style Style) float32 {
	n := style.TabSize
	style.TabSize, style.tabStop = 0, 0
	key := markKey{style: style, space: true}
	space, ok := s.marks[key]
	if !ok {
		for _, l := range s.engine().shape([]rune{' '}, style, nil, 0, false, false) {
			space += advance(l)
		}
		if len(s.marks) >= 256 {
			clear(s.marks)
		}
		s.marks[key] = space
	}
	if space <= 0 {
		space = style.FontSize() / 4
	}
	return space * n
}

// paragraph lays out runes[start:end], a paragraph without newlines, in at
// most maxLines lines (0 is unlimited) from *y down, appends them to out,
// and moves *y past it. It reports whether it cut the text, which
// continues after the paragraph when continues is set.
func (s *System) paragraph(out []Line, p Params, spans []Span, runes []rune, start, end, maxLines int, continues bool, m lineMetrics, y *float32) ([]Line, bool) {
	text := runes[start:end]
	// A carriage return before the newline is part of it.
	if n := len(text); n > 0 && text[n-1] == '\r' {
		text = text[:n-1]
	}
	rtl := isRTL(text, p.RTL)
	spans = spansIn(spans, start, start+len(text))
	var hy *hyphens
	if slices.Contains(text, softHyphen) {
		hy = &hyphens{e: s.engine(), style: p.Style, spans: spans}
	}
	var shaped []shapedLine
	indent := float32(0)
	if start == 0 {
		indent = max(p.Indent, 0)
	}
	if len(text) > 0 {
		width := max(p.Width, 0)
		if p.NoWrap {
			width = 0
		}
		shaped = s.breakLines(text, p.Style, spans, width, indent, rtl, p.NoBreakWords, hy)
		if p.Balance && width > 0 && len(shaped) > 1 && !(maxLines > 0 && len(shaped) > maxLines) {
			shaped = s.balance(text, p, spans, width, indent, rtl, hy, shaped)
		} else if p.Pretty && s.pretty != nil && width > 0 && len(shaped) > 1 && !(maxLines > 0 && len(shaped) > maxLines) {
			shaped = s.pretty(s, text, p, spans, width, indent, rtl, hy, shaped)
		}
	}
	if len(shaped) == 0 {
		shaped = []shapedLine{{end: len(text)}}
	}
	truncated := false
	if maxLines > 0 && (len(shaped) > maxLines || len(shaped) == maxLines && continues) {
		from, width := shaped[maxLines-1].start, p.Width
		if from == 0 && width > 0 {
			// The first line, beside its indent.
			width = max(width-indent, 1)
		}
		last := s.ellipsize(text, spans, from, p, width, rtl)
		shaped = append(shaped[:maxLines-1], last)
		truncated = true
	}
	if hy != nil {
		for k := range shaped {
			broken := !truncated || k < len(shaped)-1
			shaped[k] = hy.place(shaped[k], text, broken && softBreak(shaped[k], text) >= 0, rtl)
		}
	}
	// The glyphs of the lines share one allocation.
	n := 0
	for _, sl := range shaped {
		n += glyphCount(sl)
	}
	var room []Glyph
	if n > 0 {
		room = make([]Glyph, n)
	}
	first := len(out)
	pads := padded(spans)
	for _, sl := range shaped {
		k := glyphCount(sl)
		out = append(out, line(p, text, sl, start, rtl, m, y, room[:0:k], &s.starts, spans))
		if pads {
			padLine(&out[len(out)-1], spans, start)
		}
		room = room[k:]
	}
	lines := out[first:]
	// Runes between lines, if an engine left any out, belong to the line
	// before.
	for i := 0; i < len(lines)-1; i++ {
		lines[i].End = max(lines[i].End, lines[i+1].Start)
	}
	if !truncated {
		lines[len(lines)-1].End = end
	}
	return out, truncated
}

// balanceStep is how close to the narrowest width keeping a paragraph's
// lines as many balance finds it, in DIPs.
const balanceStep = 0.25

// balance returns the lines of text, which shaped holds wrapped at width,
// wrapped at the narrowest width that keeps them as many, found by
// bisection: about ten layouts of the paragraph, once, as its layout is
// kept (Params.Balance).
func (s *System) balance(text []rune, p Params, spans []Span, width, indent float32, rtl bool, hy *hyphens, shaped []shapedLine) []shapedLine {
	n := len(shaped)
	// The lines hold all the glyphs, whose advances, but for those of
	// the whitespace ending lines, take no more room than n lines do.
	total := float32(0)
	for _, sl := range shaped {
		for _, run := range sl.runs {
			for _, g := range run.glyphs {
				total += g.Advance
			}
		}
	}
	lo, hi := min(total/float32(n)/2, width), width
	for hi-lo > balanceStep {
		mid := (lo + hi) / 2
		lines := s.breakLines(text, p.Style, spans, mid, indent, rtl, p.NoBreakWords, hy)
		if len(lines) <= n {
			hi, shaped = mid, lines
		} else {
			lo = mid
		}
	}
	return shaped
}

// prettyLines returns the lines of text, which shaped holds wrapped at width,
// with the last holding more than one word where the line before can give
// it one: those two lines wrapped again at the widest width at which the
// last holds more, found by bisection, as for balance, as long as they
// stay two; the lines before them stay as they are (Params.Pretty).
func (s *System) prettyLines(text []rune, p Params, spans []Span, width, indent float32, rtl bool, hy *hyphens, shaped []shapedLine) []shapedLine {
	n := len(shaped)
	if lineWords(text, shaped[n-1]) > 1 {
		return shaped
	}
	from := shaped[n-2].start
	if n > 2 {
		indent = 0 // only the first line is indented
	}
	rest, restSpans := text[from:], spansIn(spans, from, len(text))
	var restHy *hyphens
	if hy != nil {
		restHy = &hyphens{e: hy.e, style: hy.style, spans: restSpans}
	}
	// Narrower, the line before gives the last its words, until the two
	// take three lines: the widest width at which either happened.
	var best []shapedLine
	lo, hi := float32(1), width
	for hi-lo > balanceStep {
		mid := (lo + hi) / 2
		lines := s.breakLines(rest, p.Style, restSpans, mid, indent, rtl, p.NoBreakWords, restHy)
		if len(lines) == 0 {
			break
		}
		if len(lines) > 2 || lineWords(rest, lines[len(lines)-1]) > 1 {
			lo, best = mid, lines
		} else {
			hi = mid
		}
	}
	if len(best) != 2 || best[1].start == 0 || !hangs(rest[best[1].start-1]) {
		// The line before has no word to spare, only a part of one.
		return shaped
	}
	offsetLines(best, from)
	return append(shaped[:n-2:n-2], best...)
}

// lineWords returns how many words the line sl of text holds, up to two: its
// runs of runes between whitespace, the whitespace ending it left out.
func lineWords(text []rune, sl shapedLine) int {
	n, in := 0, false
	for _, r := range text[sl.start:min(sl.end, len(text))] {
		switch {
		case hangs(r):
			in = false
		case !in:
			in, n = true, n+1
			if n > 1 {
				return n
			}
		}
	}
	return n
}

// hangs reports whether r, ending a line, takes no room in it: any
// whitespace but a no-break space, which CSS neither collapses nor hangs.
func hangs(r rune) bool { return unicode.IsSpace(r) && r != '\u00a0' }

// glyphCount returns how many glyphs a shaped line has.
func glyphCount(sl shapedLine) int {
	n := 0
	for _, run := range sl.runs {
		n += len(run.glyphs)
	}
	return n
}

// line positions a line of the paragraph text, which starts at rune
// offset of the layout, with its top at *y, and moves *y past it. Its
// glyphs go to room, which has room for those of sl; buf is a buffer.
func line(p Params, text []rune, sl shapedLine, offset int, rtl bool, m lineMetrics, y *float32, room []Glyph, buf *[]int, spans []Span) Line {
	line := Line{Start: offset + sl.start, End: offset + sl.end, RTL: rtl, Ascent: m.ascent, Descent: m.descent}
	if cap(room) > 0 {
		line.Glyphs = room
	}
	// Whitespace ending a line takes no room, unless kept.
	trim := sl.end
	if !p.KeepSpaces {
		for trim > sl.start && hangs(text[trim-1]) {
			trim--
		}
	}
	size := p.Style.FontSize()
	x0 := float32(math.MaxFloat32)
	starts := *buf
	for _, run := range sl.runs {
		runSize := size
		if f := run.font; f != nil {
			line.Ascent = max(line.Ascent, f.Ascent)
			line.Descent = max(line.Descent, f.Descent)
			if f.Size > 0 {
				runSize = f.Size // a span of another size
			}
		}
		// A cluster holds the runes up to the next one of its run: the next
		// glyph's, as for most text, whose clusters ascend one to a glyph.
		ascending := clustersAscend(run.glyphs)
		if !ascending {
			starts = starts[:0]
			for _, g := range run.glyphs {
				if g.Cluster >= 0 {
					starts = append(starts, g.Cluster)
				}
			}
			slices.Sort(starts)
			starts = slices.Compact(starts)
		}
		for k, g := range run.glyphs {
			if g.Cluster < 0 { // the ellipsis
				g.Cluster = line.End
			} else {
				if g.Cluster >= trim && g.Cluster < sl.end {
					continue
				}
				next := run.end
				if ascending {
					if k+1 < len(run.glyphs) {
						next = run.glyphs[k+1].Cluster
					}
				} else if i, _ := slices.BinarySearch(starts, g.Cluster); i+1 < len(starts) {
					next = starts[i+1]
				}
				g.Runes = max(next-g.Cluster, 1)
				if sp := spanAt(spans, g.Cluster); sp != nil {
					// A span shifted off the baseline takes its glyphs and
					// the room they need with it.
					shift := sp.BaselineShift
					switch {
					case sp.ObjectWidth > 0:
						// An inline object stands on the baseline, as high
						// as it is and as deep, whatever font its run took.
						g.Object, g.Advance, g.ID = true, sp.ObjectWidth, 0
						line.Ascent = max(line.Ascent, sp.ObjectHeight+shift)
						line.Descent = max(line.Descent, sp.ObjectDescent-shift)
					case shift != 0 && g.Font != nil:
						line.Ascent = max(line.Ascent, g.Font.Ascent+shift)
						line.Descent = max(line.Descent, g.Font.Descent-shift)
					}
					g.Y -= shift
				}
				g.Cluster += offset
			}
			g.Size = runSize
			x0 = min(x0, g.X)
			// Logical width follows the pen, not the glyphs' drawing
			// offsets. Pango may kern "11" by offsetting its second glyph
			// while keeping whole-pixel advances for line breaking.
			line.Width += g.Advance
			line.Glyphs = append(line.Glyphs, g)
		}
	}
	if len(line.Glyphs) == 0 {
		x0 = 0
	}
	line.Width = max(line.Width, 0)
	line.Height = max(m.lineHeight, line.Ascent+line.Descent)
	line.Y = *y
	line.Baseline = *y + (line.Height-line.Ascent-line.Descent)/2 + line.Ascent
	for i := range line.Glyphs {
		line.Glyphs[i].X -= x0
		line.Glyphs[i].Y += line.Baseline
	}
	*y += line.Height
	*buf = starts
	return line
}

// clustersAscend reports whether glyphs have a cluster each, ascending.
func clustersAscend(glyphs []Glyph) bool {
	last := -1
	for i := range glyphs {
		c := glyphs[i].Cluster
		if c <= last {
			return false
		}
		last = c
	}
	return true
}

// spanAt returns the span of rune i if it makes it an inline object or
// shifts it off the baseline, else nil.
func spanAt(spans []Span, i int) *Span {
	if len(spans) == 0 {
		return nil
	}
	k := spanIndex(spans, i)
	if k < len(spans) && (spans[k].ObjectWidth > 0 || spans[k].BaselineShift != 0) {
		return &spans[k]
	}
	return nil
}

// spanIndex returns the index of the span holding rune i, or len(spans)
// past them.
func spanIndex(spans []Span, i int) int {
	k, _ := slices.BinarySearchFunc(spans, i, func(sp Span, i int) int {
		if sp.End <= i {
			return -1
		}
		return 1
	})
	return k
}

// ellipsis ends text cut short, unless Params.Ellipsis gives another.
var ellipsis = []rune{'…'}

// ellipsize lays out the paragraph text from rune start on one line ending
// with an ellipsis: as many of its graphemes as fit width with it (all of
// them for 0).
func (s *System) ellipsize(text []rune, spans []Span, start int, p Params, width float32, rtl bool) shapedLine {
	e := s.engine()
	rest := text[start:]
	restSpans := spansIn(spans, start, len(text))
	mark := ellipsis
	if p.Ellipsis != "" {
		mark = []rune(p.Ellipsis)
	}
	cut := len(rest)
	if width > 0 {
		key := markKey{style: p.Style, mark: p.Ellipsis, rtl: rtl}
		ellipsis, ok := s.marks[key]
		if !ok {
			for _, l := range e.shape(mark, p.Style, nil, 0, rtl, false) {
				ellipsis = max(ellipsis, advance(l))
			}
			if len(s.marks) >= 256 {
				clear(s.marks)
			}
			s.marks[key] = ellipsis
		}
		// The advance of each cluster, at its first rune.
		s.advances = slices.Grow(s.advances[:0], len(rest))[:len(rest)]
		advances := s.advances
		clear(advances)
		for _, l := range e.shape(rest, p.Style, restSpans, 0, rtl, false) {
			for _, run := range l.runs {
				for _, g := range run.glyphs {
					if g.Cluster >= 0 && g.Cluster < len(rest) {
						advances[g.Cluster] += g.Advance
					}
				}
			}
		}
		var b Boundaries
		b.Reset(rest)
		w := float32(0)
		cut = 0
		// The padding of the spans' pieces before the cut, and the span of
		// the last, which the ellipsis ends.
		pads, k := padded(restSpans), -1
		for cut < len(rest) {
			next := b.NextGrapheme(cut)
			gw := float32(0)
			for _, a := range advances[cut:next] {
				gw += a
			}
			j := k
			if pads {
				if j = spanIndex(restSpans, cut); j != k {
					gw += padOf(restSpans, k) + padOf(restSpans, j)
				}
			}
			if w+gw+padOf(restSpans, j)+ellipsis > width {
				break
			}
			w += gw
			k = j
			cut = next
		}
	}
	for cut > 0 && unicode.IsSpace(rest[cut-1]) {
		cut--
	}
	t := append(append(s.ellipsized[:0], rest[:cut]...), mark...)
	s.ellipsized = t
	out := shapedLine{start: start, end: start + cut}
	// The ellipsis takes the style of the text it ends.
	tSpans := spansIn(restSpans, 0, cut)
	if n := len(tSpans); n > 0 && tSpans[n-1].End == cut {
		tSpans[n-1].End += len(mark)
	}
	for _, l := range e.shape(t, p.Style, tSpans, 0, rtl, false) {
		for _, run := range l.runs {
			run.start, run.end = start+min(run.start, cut), start+min(run.end, cut)
			for i := range run.glyphs {
				if g := &run.glyphs[i]; g.Cluster >= cut {
					g.Cluster = -1
				} else {
					g.Cluster += start
				}
			}
			out.runs = append(out.runs, run)
		}
	}
	return out
}

// advance returns the width a shaped line's glyphs take.
func advance(l shapedLine) float32 {
	var width float32
	for _, run := range l.runs {
		for _, g := range run.glyphs {
			width += g.Advance
		}
	}
	return max(width, 0)
}

// isRTL reports whether a paragraph is right-to-left: whether its first
// strongly directional rune is, else neutral.
func isRTL(para []rune, neutral bool) bool {
	for _, r := range para {
		switch {
		case unicode.In(r, unicode.Hebrew, unicode.Arabic, unicode.Syriac, unicode.Thaana, unicode.Nko, unicode.Samaritan, unicode.Mandaic, unicode.Adlam):
			return true
		case unicode.IsLetter(r):
			return false
		}
	}
	return neutral
}
