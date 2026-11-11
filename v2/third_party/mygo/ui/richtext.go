package ui

import (
	"math"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/internal/text"
)

// Span is a run of a RichText with a style of its own. What it leaves
// zero takes the style of the text around it, and Italic, Underline,
// WavyUnderline, DottedUnderline and Strikethrough only turn those on.
type Span struct {
	Text            string
	Font            string // a family, as for Element.Font
	Size            float32
	Weight          int
	Italic          bool
	Color           Color
	Underline       bool
	WavyUnderline   bool
	DottedUnderline bool
	Strikethrough   bool
	// DecorationColor and DecorationThickness are those of the span's
	// underline and strikethrough, as for Element.DecorationColor.
	DecorationColor     Color
	DecorationThickness float32
	// UnderlineOffset puts the span's underline that many DIPs below the
	// baseline, or above it when negative, as for Element.UnderlineOffset;
	// zero leaves it where the text around it has it.
	UnderlineOffset float32
	// Background highlights the span, as search results are.
	Background Color
	// Radius rounds the corners of the Background, which then hugs the
	// span's glyphs, a little wider than them, rather than filling the
	// line, as code within text shows.
	Radius float32
	// PaddingX and PaddingY pad the Background, which then hugs the
	// span's glyphs, as CSS pads an inline box: a key cap, a mention chip.
	// PaddingX, which replaces the little that Radius widens it by, also
	// takes room in the line, moving the span's glyphs in and the text
	// after them on; PaddingY only paints, over the lines above and below,
	// as the background of an inline box does on the web. Each
	// line's piece of a span that wraps gets both, and its corners, as
	// with CSS's box-decoration-break: clone.
	PaddingX, PaddingY float32
	LetterSpacing      float32
	// NoLetterSpacing makes a zero LetterSpacing the span's own, rather
	// than the text's around it, as CSS's letter-spacing: 0 on an inline
	// box: digits or code within tracked capitals.
	NoLetterSpacing bool
	Features        string // as for Element.FontFeatures, comma separated
	Lang            string // the language, as for Element.Lang
	// ObjectWidth and ObjectHeight, when ObjectWidth is set, make each
	// rune of the span, an object replacement character (U+FFFC), an
	// inline object: a box of that size in DIPs standing on the baseline,
	// which lines break around and which draws nothing, where a rich text
	// editor draws an image (TextLayout.Objects) and a text what
	// Element.DrawObjects draws. ObjectDescent extends the box that far
	// below the baseline, as math's subscripts and fractions go: the line
	// makes room for it.
	ObjectWidth, ObjectHeight, ObjectDescent float32
	// BaselineShift raises the span's text, or its objects, that many
	// DIPs above the baseline, or lowers it when negative, as superscripts
	// and subscripts with a smaller Size: footnote references, exponents,
	// chemical formulas. The line makes room for it, and its underline
	// and strikethrough go with it.
	BaselineShift float32
	// Shadow is the shadow the span's text casts (NewTextShadow), as
	// Element.TextShadow casts one: zero casts that of the text around it.
	Shadow TextShadow

	// solid draws Underline through descenders, as a link's hover.
	solid bool
	// offsetSet marks an UnderlineOffset set, zero included, by the
	// style of an inline element.
	offsetSet bool
	// strokeWidth and strokeColor are the stroke of the text of an inline
	// element (TextStroke), if strokeSet.
	strokeWidth float32
	strokeColor Color
	strokeSet   bool
	// shadowSet marks a Shadow set, though transparent, by the style of
	// an inline element (TextShadow).
	shadowSet bool
	// colorSet and decoColorSet mark a Color and a DecorationColor
	// set though transparent, by the style of an inline element: its
	// TextColor, or its Opacity or Invisible, which fade its text.
	colorSet, decoColorSet bool
}

// TextShadow is a shadow that text casts, as CSS's text-shadow, which
// NewTextShadow makes. The zero TextShadow casts none.
type TextShadow struct {
	x, y, blur float32
	color      Color
}

// NewTextShadow returns the shadow of the shapes of glyphs, offset by x
// and y DIPs and blurred by blur, in c, for a Span's Shadow:
//
//	ui.Span{Text: "Sale", Shadow: ui.NewTextShadow(0, 2, 3, t.Accent)}
//
// Text casts the shadows that are made, so that apps that make none do
// not link their painting.
func NewTextShadow(x, y, blur float32, c Color) TextShadow {
	linkTextShadows()
	return TextShadow{x: x, y: y, blur: max(blur, 0), color: c}
}

// shadow returns the span's Shadow, and whether it sets one.
func (s *Span) shadow() (TextShadow, bool) { return s.Shadow, s.shadowSet || s.Shadow.color.A > 0 }

// color returns the span's Color, and whether it sets one.
func (s *Span) color() (Color, bool) { return s.Color, s.colorSet || s.Color.A > 0 }

// decorationColor returns the span's DecorationColor, and whether it sets
// one.
func (s *Span) decorationColor() (Color, bool) {
	return s.DecorationColor, s.decoColorSet || s.DecorationColor.A > 0
}

// underlineOffset returns the span's UnderlineOffset, and whether it sets
// one.
func (s *Span) underlineOffset() (float32, bool) {
	return s.UnderlineOffset, s.offsetSet || s.UnderlineOffset != 0
}

// letterSpacing returns the span's LetterSpacing, and whether it sets one.
func (s *Span) letterSpacing() (float32, bool) {
	return s.LetterSpacing, s.NoLetterSpacing || s.LetterSpacing != 0
}

// RichText creates a text whose spans differ in style, over the style of
// the text, which its methods and the elements around it set as for Text:
//
//	ui.RichText(c,
//		ui.Span{Text: "Saved "},
//		ui.Span{Text: "report.pdf", Weight: 600},
//		ui.Span{Text: " to "},
//		ui.Span{Text: "Documents", Color: t.Accent, Underline: true},
//	)
//
// The spans wrap together as one paragraph, or several at newlines.
//
// Text elements built in its Children (Text, Link, RichText) continue the
// paragraph after its spans, as HTML's inline elements do: each styles its
// own text over the paragraph's, and keeps its interaction (Clicked,
// Hovered, the focus, a Tooltip, assistive technology) with its words as
// its area. A link in a sentence:
//
//	ui.RichText(c).Children(func() {
//		ui.Text(c, "Read ")
//		ui.Link(c, "the guide", url)
//		ui.Text(c, " to get started.")
//	})
//
// Only text elements go inside a text, and their sizes, padding, borders
// and corners do not apply; a background highlights their text.
func coreRichText(c *context, spans ...Span) *node {
	e := c.newElement(kindText)
	e.text = e.st.spanCache().join(spans)
	e.spans = spans
	return e
}

// textSpans encodes the styles of an element's spans for its layout, once
// a frame, with those of the elements inside it, once it is built.
func (e *node) textSpans() string {
	if e.first != nil && !e.merged {
		e.spans, e.merged = e.inlineSpans(), true
	}
	if e.spans == nil || e.spansKey != "" {
		return e.spansKey
	}
	sc := e.st.spanCache()
	sc.update(e.spans)
	e.spansKey = sc.key
	return e.spansKey
}

// spanCache keeps what a text element made of its spans in the frames that
// built it, for the next frame, which usually builds the same spans: their
// text, and the styles of their layout and the runes each ends at. It
// compares rather than copies the spans, so that it keeps no strings of
// theirs alive but those it made.
type spanCache struct {
	// text is the text of the spans RichText was given.
	text string
	// styles and key are the styles of the spans laid out, the elements'
	// inside the text's included, and those encoded for the layout (see
	// text.EncodeSpans); paints tells whether a span has a color, lines
	// or a background, which paint the text in more than its color.
	styles []text.Span
	key    string
	paints bool
	// inline holds the spans of a paragraph with elements inside it, its
	// own and theirs, made again each frame (inlineSpans).
	inline []Span
}

// spanCache returns the element's spanCache, making it the first time.
func (s *state) spanCache() *spanCache {
	if s.spans == nil {
		s.spans = &spanCache{}
	}
	return s.spans
}

// join returns the text of spans, as the last call made it when that
// text is the same.
func (sc *spanCache) join(spans []Span) string {
	n := 0
	same := true
	for _, s := range spans {
		end := n + len(s.Text)
		same = same && end <= len(sc.text) && sc.text[n:end] == s.Text
		n = end
	}
	if same && n == len(sc.text) {
		return sc.text
	}
	var b strings.Builder
	b.Grow(n)
	for _, s := range spans {
		b.WriteString(s.Text)
	}
	sc.text = b.String()
	return sc.text
}

// update makes the styles of the cache those of spans, unless they are
// already.
func (sc *spanCache) update(spans []Span) {
	same := len(spans) == len(sc.styles) && sc.key != ""
	end := 0
	paints := false
	for i, s := range spans {
		end += utf8.RuneCountInString(s.Text)
		paints = paints || s.Color.A > 0 || s.colorSet || s.Underline || s.WavyUnderline || s.DottedUnderline || s.Strikethrough || s.Background.A > 0 || s.strokeSet || s.shadowSet || s.Shadow.color.A > 0
		same = same && sc.styles[i] == spanStyle(s, end)
	}
	sc.paints = paints
	if same {
		return
	}
	sc.styles = sc.styles[:0]
	end = 0
	for _, s := range spans {
		end += utf8.RuneCountInString(s.Text)
		sc.styles = append(sc.styles, spanStyle(s, end))
	}
	sc.key = text.EncodeSpans(sc.styles)
}

// spanStyle returns the style of a span ending at rune end, for its
// layout.
func spanStyle(s Span, end int) text.Span {
	return text.Span{End: end, Family: s.Font, Size: s.Size, Weight: s.Weight, Italic: s.Italic, LetterSpacing: s.LetterSpacing, SpacingSet: s.NoLetterSpacing, Features: s.Features, Lang: s.Lang,
		ObjectWidth: s.ObjectWidth, ObjectHeight: s.ObjectHeight, ObjectDescent: s.ObjectDescent, BaselineShift: s.BaselineShift, PaddingX: s.PaddingX}
}

// encodeSpans encodes the styles of spans for a layout.
func encodeSpans(spans []Span) string {
	ts := make([]text.Span, len(spans))
	end := 0
	for i, s := range spans {
		end += utf8.RuneCountInString(s.Text)
		ts[i] = spanStyle(s, end)
	}
	return text.EncodeSpans(ts)
}

// richParams returns how to lay out spans over the text style of theme t,
// wrapping lines at width (none for 0).
func (rt *engine) richParams(t *Theme, width float32, spans []Span) text.Params {
	style := text.Style{Family: t.Font, Size: t.FontSize, Lang: rt.lang}
	// Views measure the same spans frame after frame.
	for i := range rt.measured {
		m := &rt.measured[i]
		if m.p.Width == width && m.p.Style == style && m.spans != nil && slices.Equal(m.spans, spans) {
			return m.p
		}
	}
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(s.Text)
	}
	p := text.Params{Text: b.String(), Width: width, Style: style, Spans: encodeSpans(spans)}
	m := &rt.measured[rt.nextMeasured]
	rt.nextMeasured = (rt.nextMeasured + 1) % len(rt.measured)
	// Strings do not change: a copy of the spans keeps them.
	m.spans, m.p = append(m.spans[:0], spans...), p
	if m.spans == nil {
		m.spans = []Span{}
	}
	return p
}

// measuredSpans are spans Painter.RichText or MeasureText laid out, and
// how.
type measuredSpans struct {
	spans []Span
	p     text.Params
}

// RichText draws spans of text as RichText shows them, with its top-left
// corner at (x, y), wrapping lines at width DIPs (none for 0), and returns
// the size it takes. What the spans leave zero takes the theme's font,
// FontSize and Text color:
//
//	w, _ := p.MeasureText(0, label)
//	p.RichText(r.X+(r.W-w)/2, r.Y, 0, label)
func (p *Painter) RichText(x, y, width float32, spans ...Span) (w, h float32) {
	t := p.Theme()
	l := p.rt.text.Layout(p.rt.richParams(t, width, spans))
	p.textLayout(l, x, y, t.Text, textStyle{size: t.FontSize}, newSpanPaint(spans))
	return l.Width, l.Height
}

// MeasureText returns the size spans of text take as Painter.RichText draws
// them, wrapping lines at width DIPs (none for 0).
func (p *Painter) MeasureText(width float32, spans ...Span) (w, h float32) {
	l := p.rt.text.Layout(p.rt.richParams(p.Theme(), width, spans))
	return l.Width, l.Height
}

// MeasureText returns the size spans of text take as Painter.RichText draws
// them, wrapping lines at width DIPs (none for 0), to lay out what depends
// on it while building.
func (c *context) MeasureText(width float32, spans ...Span) (w, h float32) {
	l := c.rt.text.Layout(c.rt.richParams(c.theme, width, spans))
	return l.Width, l.Height
}

// spanPaint paints the colors and lines of a rich text's spans: styles
// hold the runes ending each span.
type spanPaint struct {
	spans  []Span
	styles []text.Span
}

func newSpanPaint(spans []Span) *spanPaint {
	sc := &spanCache{}
	sc.update(spans)
	if !sc.paints {
		return nil
	}
	return &spanPaint{spans: spans, styles: sc.styles}
}

// paintSpans returns how to paint the spans of the element's text, which
// textSpans cached as it laid it out, or nil when they paint nothing but
// its color.
func (e *node) paintSpans(sp *spanPaint) *spanPaint {
	if e.spans == nil {
		return nil
	}
	sc := e.st.spanCache()
	if e.spansKey == "" {
		// Not laid out by the frame, so not cached yet.
		sc.update(e.spans)
	}
	if !sc.paints {
		return nil
	}
	*sp = spanPaint{spans: e.spans, styles: sc.styles}
	return sp
}

// at returns the index of the span holding rune r, or -1.
func (sp *spanPaint) at(r int) int {
	lo, hi := 0, len(sp.styles)
	for lo < hi {
		m := (lo + hi) / 2
		if sp.styles[m].End <= r {
			lo = m + 1
		} else {
			hi = m
		}
	}
	if lo == len(sp.styles) {
		return -1
	}
	return lo
}

// color returns the color of span i, or c.
func (sp *spanPaint) color(i int, c Color) Color {
	if i >= 0 {
		if sc, ok := sp.spans[i].color(); ok {
			return sc
		}
	}
	return c
}

// stroke returns the width and color of the stroke of span i, or w and c.
func (sp *spanPaint) stroke(i int, w float32, c Color) (float32, Color) {
	if i >= 0 && sp.spans[i].strokeSet {
		return sp.spans[i].strokeWidth, sp.spans[i].strokeColor
	}
	return w, c
}

// shadow returns the shadow of span i, or sh.
func (sp *spanPaint) shadow(i int, sh TextShadow) TextShadow {
	if i >= 0 {
		if s, ok := sp.spans[i].shadow(); ok {
			return s
		}
	}
	return sh
}

// shadows reports whether a span casts a shadow.
func (sp *spanPaint) shadows() bool {
	for i := range sp.spans {
		if sp.spans[i].Shadow.color.A > 0 {
			return true
		}
	}
	return false
}

// strokes reports whether a span strokes its glyphs.
func (sp *spanPaint) strokes() bool {
	for i := range sp.spans {
		if s := &sp.spans[i]; s.strokeSet && s.strokeWidth > 0 && s.strokeColor.A > 0 {
			return true
		}
	}
	return false
}

// runs calls fn with each run of the glyphs of a line in one span: the
// span's index (-1 past the spans), the run's glyphs [i, j), and its left
// and right, in DIPs from the left of the text.
func (sp *spanPaint) runs(line *text.Line, fn func(k, i, j int, x0, x1 float32)) {
	gs := line.Glyphs
	for i := 0; i < len(gs); {
		k := sp.at(gs[i].Cluster)
		x0, x1 := float32(math.MaxFloat32), float32(-math.MaxFloat32)
		j := i
		for ; j < len(gs) && sp.at(gs[j].Cluster) == k; j++ {
			x0, x1 = min(x0, gs[j].X), max(x1, gs[j].X+gs[j].Advance)
		}
		fn(k, i, j, x0, x1)
		i = j
	}
}

// backgrounds fills behind the spans of a line that have a background,
// from the top-left of the text at (x, y), in DIPs.
func (sp *spanPaint) backgrounds(p *Painter, line *text.Line, x, y float32) {
	sp.runs(line, func(k, i, _ int, x0, x1 float32) {
		saved := p.transform
		p.transform = saved.Mul(p.inlineTransform(line.Glyphs[i].Cluster))
		defer func() { p.transform = saved }()
		if k < 0 || sp.spans[k].Background.A == 0 {
			return
		}
		if s := &sp.spans[k]; s.Radius > 0 || s.PaddingX > 0 || s.PaddingY > 0 {
			size := line.Glyphs[i].Size
			px := min(size*0.22, s.Radius)
			if s.PaddingX > 0 || s.PaddingY > 0 {
				px = max(s.PaddingX, 0) // the room the layout left
			}
			py := max(s.PaddingY, 0)
			base := line.Baseline - s.BaselineShift
			p.Fill(Rect{x + x0 - px, y + base - size*0.98 - py, x1 - x0 + 2*px, size*1.32 + 2*py}, s.Background, s.Radius)
			return
		}
		p.Fill(Rect{x + x0, y + line.Y, x1 - x0, line.Height}, sp.spans[k].Background, 0)
	})
}

// lines draws the underlines and strikethroughs of the spans of line li
// of l, laid out from (x, y) in DIPs, with the color and thickness of base
// where the spans set none.
func (sp *spanPaint) lines(p *Painter, l *text.Layout, li int, x, y float32, color Color, base decoration) {
	sp.runs(&l.Lines[li], func(k, i, j int, _, _ float32) {
		if k < 0 {
			return
		}
		span := &sp.spans[k]
		d := decoration{underline: span.Underline || span.WavyUnderline || span.DottedUnderline, wavy: span.WavyUnderline, dotted: span.DottedUnderline, strike: span.Strikethrough, solid: span.solid, color: base.color, thick: base.thick,
			offset: base.offset, hasOffset: base.hasOffset}
		if !d.underline && !d.strike {
			return
		}
		if c, ok := span.decorationColor(); ok {
			d.color = c
		}
		if span.DecorationThickness > 0 {
			d.thick = span.DecorationThickness
		}
		if v, ok := span.underlineOffset(); ok {
			d.offset, d.hasOffset = v, true
		}
		c := sp.color(k, color)
		if d.color.A == 0 && (span.decoColorSet || c.A == 0) {
			return // transparent, or faded out
		}
		saved := p.transform
		p.transform = saved.Mul(p.inlineTransform(l.Lines[li].Glyphs[i].Cluster))
		p.decorationRun(l, li, i, j, x, y, d, c)
		p.transform = saved
	})
}
