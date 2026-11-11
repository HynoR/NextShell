package ui

import (
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/internal/platform"
)

// Inline elements are the text elements built inside another text: they
// continue its paragraph, which lays out its own text and theirs as one,
// with the styles they set as spans over its own. They keep their state
// and their interaction (clicks, hovering, the focus, tooltips, context
// menus, assistive technology), with the boxes of their words, in the
// paragraph's layout, as their area.

// isInline reports whether the element is inside a text.
func (e *node) isInline() bool { return e.parent != nil && e.parent.kind == kindText }

// inlineText makes the text of a paragraph its own followed by that of the
// elements inside it, once they are built, and gives each the range of its
// text in it.
func (e *node) inlineText() {
	if !e.paragraph {
		e.paragraph, e.ownText = true, e.text
	}
	var b strings.Builder
	b.WriteString(e.ownText)
	n := utf8.RuneCountInString(e.ownText)
	for ch := e.first; ch != nil; ch = ch.next {
		if ch.hidden {
			ch.runes = [2]int{n, n} // no text in the paragraph
			continue
		}
		m := utf8.RuneCountInString(ch.text)
		ch.runes = [2]int{n, n + m}
		n += m
		b.WriteString(ch.text)
	}
	e.text = b.String()
}

// accessText returns the text that names a text element for assistive
// technology: its text without that of the elements inside it hidden from
// it (AccessHidden), as a hidden node gives nothing to a name. Without
// one, its text is returned as it is, without a copy.
func (e *node) accessText() string {
	cuts := e.accessCuts(nil, 0)
	if cuts == nil {
		return e.text
	}
	var b strings.Builder
	at := 0
	for _, r := range cuts {
		b.WriteString(e.text[at:runeOffset(e.text, r[0])])
		at = runeOffset(e.text, r[1])
	}
	b.WriteString(e.text[at:])
	return b.String()
}

// accessCuts appends to cuts the ranges of runes, in the paragraph's text,
// of the elements inside e hidden from assistive technology, e's text
// starting at rune offset of it.
func (e *node) accessCuts(cuts [][2]int, offset int) [][2]int {
	for ch := e.first; ch != nil; ch = ch.next {
		a, b := offset+ch.runes[0], offset+ch.runes[1]
		switch {
		case a == b:
		case ch.accessHidden:
			cuts = append(cuts, [2]int{a, b})
		default:
			cuts = ch.accessCuts(cuts, a)
		}
	}
	return cuts
}

// inlineSpans returns the spans of a paragraph: its own, then those of the
// elements inside it, in the styles they set over it.
func (e *node) inlineSpans() []Span {
	// The spans go into a buffer the element's state keeps, as the frame
	// is done with them once it painted.
	sc := e.st.spanCache()
	out := append(sc.inline[:0], e.spans...)
	if len(e.spans) == 0 && e.ownText != "" {
		out = append(out, Span{Text: e.ownText})
	}
	for ch := e.first; ch != nil; ch = ch.next {
		if ch.hidden {
			continue
		}
		spans := ch.spans
		switch {
		case ch.first != nil:
			spans = ch.inlineSpans()
		case spans == nil:
			spans = []Span{{Text: ch.text}}
		}
		base, alpha := ch.spanStyle()
		for _, s := range spans {
			if alpha < 1 {
				s = s.faded(alpha)
			}
			out = append(out, over(s, base))
		}
	}
	sc.inline = out
	return out
}

// spanStyle returns the text style the element sets, as a span, and the
// alpha that fades its text and that inside it.
func (e *node) spanStyle() (Span, float32) {
	t := &e.ts
	var s Span
	if t.set&setFamily != 0 {
		s.Font = t.family
	}
	if t.set&setSize != 0 {
		s.Size = t.size
	}
	if t.set&setWeight != 0 {
		s.Weight = t.weight
	}
	if t.set&setItalic != 0 {
		s.Italic = t.italic
	}
	if t.set&setColor != 0 {
		s.Color, s.colorSet = t.color, t.color.A == 0
	}
	if t.set&setUnderline != 0 {
		s.Underline = t.underline && !t.wavy && !t.dotted
		s.WavyUnderline = t.underline && t.wavy
		s.DottedUnderline = t.underline && t.dotted
		s.solid = t.underline && t.solid
	}
	if t.set&setStrike != 0 {
		s.Strikethrough = t.strike
	}
	if t.set&setSpacing != 0 {
		s.LetterSpacing, s.NoLetterSpacing = t.spacing, t.spacing == 0
	}
	if t.set&setFeatures != 0 {
		s.Features = t.features
	}
	if t.set&setLang != 0 {
		s.Lang = t.lang
	}
	if t.set&setDecoColor != 0 {
		s.DecorationColor, s.decoColorSet = t.decoColor, t.decoColor.A == 0
	}
	if t.set&setDecoThick != 0 {
		s.DecorationThickness = t.decoThick
	}
	if t.set&setDecoOffset != 0 {
		s.UnderlineOffset, s.offsetSet = t.decoOffset, true
	}
	if t.set&setStroke != 0 {
		s.strokeWidth, s.strokeColor, s.strokeSet = t.strokeWidth, t.strokeColor, true
	}
	if t.set&setShadow != 0 {
		s.Shadow, s.shadowSet = t.shadow, true
	}
	switch {
	case t.set&setBackground != 0:
		s.Background = t.background
	case e.fill == fillColor && e.bg.A > 0:
		// The background of an inline element is that of its text.
		s.Background = e.bg
	}
	// What dims the element dims its text, and what hides it keeps its
	// room in the line but paints none of it, as CSS's visibility: hidden.
	alpha := float32(1)
	switch {
	case e.flags&flagInvisible != 0:
		alpha = 0
	case e.flags&flagDisabled != 0 && !e.undimmed:
		alpha = 0.5
	}
	if e.opacitySet {
		alpha *= e.opacity
	}
	if alpha < 1 {
		// The colors it takes from the text around it, faded.
		r := e.resolvedText()
		s.Color, s.colorSet = r.color, r.color.A == 0
		if r.set&setDecoColor != 0 {
			s.DecorationColor, s.decoColorSet = r.decoColor, r.decoColor.A == 0
		}
		if r.set&setStroke != 0 {
			s.strokeWidth, s.strokeColor, s.strokeSet = r.strokeWidth, r.strokeColor, true
		}
		if r.set&setShadow != 0 {
			s.Shadow, s.shadowSet = r.shadow, true
		}
		s = s.faded(alpha)
	}
	return s, alpha
}

// faded returns the span with the colors it sets times alpha, as the
// opacity of an inline element fades them: a color faded to nothing stays
// the span's own.
func (s Span) faded(alpha float32) Span {
	if c, ok := s.color(); ok {
		s.Color = c.Alpha(alpha)
		s.colorSet = s.Color.A == 0
	}
	if c, ok := s.decorationColor(); ok {
		s.DecorationColor = c.Alpha(alpha)
		s.decoColorSet = s.DecorationColor.A == 0
	}
	if s.strokeSet {
		s.strokeColor = s.strokeColor.Alpha(alpha)
	}
	if _, ok := s.shadow(); ok {
		s.Shadow.color = s.Shadow.color.Alpha(alpha)
		s.shadowSet = true
	}
	// A background faded to nothing shows that of the text around it, as
	// it would on the web.
	s.Background = s.Background.Alpha(alpha)
	return s
}

// over returns span s over base: what s leaves zero takes base's style.
func over(s, base Span) Span {
	if s.Font == "" {
		s.Font = base.Font
	}
	if s.Size == 0 {
		s.Size = base.Size
	}
	if s.Weight == 0 {
		s.Weight = base.Weight
	}
	if _, ok := s.color(); !ok {
		s.Color, s.colorSet = base.Color, base.colorSet
	}
	if _, ok := s.decorationColor(); !ok {
		s.DecorationColor, s.decoColorSet = base.DecorationColor, base.decoColorSet
	}
	if s.DecorationThickness == 0 {
		s.DecorationThickness = base.DecorationThickness
	}
	if _, ok := s.underlineOffset(); !ok {
		s.UnderlineOffset, s.offsetSet = base.UnderlineOffset, base.offsetSet
	}
	if s.Background.A == 0 {
		s.Background = base.Background
	}
	if !s.strokeSet {
		s.strokeWidth, s.strokeColor, s.strokeSet = base.strokeWidth, base.strokeColor, base.strokeSet
	}
	if _, ok := s.shadow(); !ok {
		s.Shadow, s.shadowSet = base.Shadow, base.shadowSet
	}
	if _, ok := s.letterSpacing(); !ok {
		s.LetterSpacing, s.NoLetterSpacing = base.LetterSpacing, base.NoLetterSpacing
	}
	if s.Features == "" {
		s.Features = base.Features
	}
	if s.Lang == "" {
		s.Lang = base.Lang
	}
	s.Italic = s.Italic || base.Italic
	s.Underline = s.Underline || base.Underline
	s.WavyUnderline = s.WavyUnderline || base.WavyUnderline
	s.DottedUnderline = s.DottedUnderline || base.DottedUnderline
	s.solid = s.solid || base.solid
	s.Strikethrough = s.Strikethrough || base.Strikethrough
	return s
}

// placeInline gives the elements inside e, which starts at rune offset of
// paragraph para's text, and those inside them, the boxes of their text in
// the paragraph's layout, and as their box the box around those.
func placeInline(para, e *node, offset int) {
	ox, oy := para.x+para.contentX(), para.y+para.contentY()
	for ch := e.first; ch != nil; ch = ch.next {
		a, b := offset+ch.runes[0], offset+ch.runes[1]
		ch.frags = ch.frags[:0]
		var box Rect
		if para.tl != nil {
			rt := para.c.rt
			rt.inlineRects = para.tl.AppendSelection(rt.inlineRects[:0], a, b, false)
			for i, r := range rt.inlineRects {
				f := Rect{ox + r.X, oy + r.Y, r.W, r.H}
				ch.frags = append(ch.frags, f)
				if i == 0 {
					box = f
				} else {
					box = union(box, f)
				}
			}
		}
		ch.x, ch.y, ch.w, ch.h = box.X, box.Y, box.W, box.H
		placeInline(para, ch, a)
	}
}

func union(a, b Rect) Rect {
	x0, y0 := min(a.X, b.X), min(a.Y, b.Y)
	x1, y1 := max(a.X+a.W, b.X+b.W), max(a.Y+a.H, b.Y+b.H)
	return Rect{x0, y0, x1 - x0, y1 - y0}
}

// paintInline draws the focus ring of an inline element around its words,
// and those of the elements inside it: the paragraph draws their text.
func (p *Painter) paintInline(e *node) {
	if e.ringShown() {
		for _, r := range e.frags {
			p.focusRingOf(e, r, [4]float32{2, 2, 2, 2})
		}
	}
	for ch := e.first; ch != nil; ch = ch.next {
		if ch.flags&flagInvisible == 0 && !ch.hidden {
			p.element(ch)
		}
	}
}

// ringShown reports whether the element has the keyboard focus from the
// keyboard and MyGo rings it.
func (e *node) ringShown() bool {
	rt := e.c.rt
	if e.flags&(flagFocusable|flagOwnRing) == flagFocusable && rt.insp.isForced(e.id, forceFocusVisible) {
		return true
	}
	return e.flags&(flagFocusable|flagOwnRing) == flagFocusable && rt.focused == e.id && rt.focusVisible && rt.windowFocus()
}

// accessInline describes the elements inside text e that are more than
// text to assistive technology, such as links, as nodes inside the node
// parent: the text itself already reads theirs.
func (rt *engine) accessInline(t *platform.AccessTree, e *node, parent int) {
	for ch := e.first; ch != nil; ch = ch.next {
		if ch.flags&flagInvisible != 0 || ch.hidden || ch.accessHidden || len(ch.frags) == 0 {
			continue
		}
		role, ok := ch.accessRole()
		if !ok || role == platform.RoleText && ch.flags&(flagClickable|flagFocusable) == 0 {
			rt.accessInline(t, ch, parent)
			continue
		}
		t.Nodes = append(t.Nodes, platform.AccessNode{
			ID: ch.id, Parent: parent, Role: role, Label: ch.label,
			Bounds: accessBounds(ch),
		})
		rt.accessDetails(ch, &t.Nodes[len(t.Nodes)-1])
	}
}
