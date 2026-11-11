package text

import (
	"slices"
	"strings"
	"unicode"

	"github.com/go-text/typesetting/segmenter"
)

// Rect is a rectangle in DIPs relative to the layout.
type Rect struct{ X, Y, W, H float32 }

// lineCarets returns the x of every caret position of a line, from Start
// to End: inside a cluster of several runes (a ligature) the positions
// split the cluster evenly. It caches them in the line, so a layout's
// caret methods must be used from one goroutine at a time.
func (line *Line) lineCarets() []float32 {
	if line.carets != nil {
		return line.carets
	}
	type cluster struct {
		x0, x1       float32
		start, runes int
		rtl          bool
	}
	var clusters []cluster
	for g := 0; g < len(line.Glyphs); {
		first := line.Glyphs[g]
		c := cluster{first.X, first.X + first.Advance, first.Cluster, first.Runes, first.RTL}
		g++
		for g < len(line.Glyphs) && line.Glyphs[g].Cluster == first.Cluster && line.Glyphs[g].Runes == first.Runes {
			c.x0 = min(c.x0, line.Glyphs[g].X)
			c.x1 = max(c.x1, line.Glyphs[g].X+line.Glyphs[g].Advance)
			g++
		}
		if c.runes > 0 {
			clusters = append(clusters, c)
		}
	}
	pos := func(c cluster, k int) float32 {
		f := float32(k) / float32(c.runes)
		if c.rtl {
			return c.x1 - (c.x1-c.x0)*f
		}
		return c.x0 + (c.x1-c.x0)*f
	}
	n := line.End - line.Start + 1
	carets := make([]float32, n)
	set := make([]bool, n)
	// A cluster's start takes precedence over the end of the cluster
	// before it, which differ in bidirectional text.
	for _, c := range clusters {
		for k := 0; k < c.runes; k++ {
			if i := c.start + k - line.Start; i >= 0 && i < n {
				carets[i], set[i] = pos(c, k), true
			}
		}
	}
	for _, c := range clusters {
		if i := c.start + c.runes - line.Start; i >= 0 && i < n && !set[i] {
			carets[i], set[i] = pos(c, c.runes), true
		}
	}
	end := line.X + line.Width
	if line.RTL {
		end = line.X
	}
	for i := n - 1; i >= 0; i-- {
		if !set[i] {
			carets[i] = end
			if i+1 < n && set[i+1] {
				// A trimmed space before the next cluster.
				carets[i] = carets[i+1]
			}
			set[i] = true
		}
	}
	line.carets = carets
	return carets
}

// LineAt returns the index of the line holding rune index, the start of
// the next line at a soft line break.
func (l *Layout) LineAt(index int) int {
	for i := range l.Lines {
		line := &l.Lines[i]
		if index < line.End || (index == line.End && (i == len(l.Lines)-1 || l.Lines[i+1].Start > index)) {
			return i
		}
	}
	return len(l.Lines) - 1
}

// Caret returns the position of the caret before rune index: its x, the
// top of its line and the line's height.
func (l *Layout) Caret(index int) (x, y, h float32) {
	if len(l.Lines) == 0 {
		return 0, 0, 0
	}
	index = max(0, min(index, len(l.Runes)))
	line := &l.Lines[l.LineAt(index)]
	carets := line.lineCarets()
	i := max(0, min(index-line.Start, len(carets)-1))
	return carets[i], line.Y, line.Height
}

// IndexAt returns the rune index of the caret position closest to (x, y).
func (l *Layout) IndexAt(x, y float32) int {
	if len(l.Lines) == 0 {
		return 0
	}
	li := len(l.Lines) - 1
	for i := range l.Lines {
		if y < l.Lines[i].Y+l.Lines[i].Height {
			li = i
			break
		}
	}
	line := &l.Lines[li]
	carets := line.lineCarets()
	best, bestDist := 0, float32(-1)
	for i, cx := range carets {
		d := cx - x
		if d < 0 {
			d = -d
		}
		if bestDist < 0 || d < bestDist {
			best, bestDist = i, d
		}
	}
	return line.Start + best
}

// Selection returns the rectangles covering the runes from start to end.
func (l *Layout) Selection(start, end int) []Rect { return l.SelectionOn(start, end, false) }

// SelectionOn is Selection for a layout of a paragraph of a longer text,
// whose selection goes on past the newline after the paragraph when on
// is set: the newline shows, as between the lines of a layout.
func (l *Layout) SelectionOn(start, end int, on bool) []Rect {
	return l.SelectionVisual(start, end, on)
}

// SelectionVisual returns separate rectangles for selected visual runs. At
// bidi boundaries a logical range may cover disjoint areas on a line; taking
// the minimum and maximum caret would highlight intervening unselected text.
// on includes the paragraph's following newline, as SelectionOn does.
func (l *Layout) SelectionVisual(start, end int, on bool) []Rect {
	return l.AppendSelection(nil, start, end, on)
}

// AppendSelection appends the rectangles of SelectionVisual to dst, for
// callers that keep them frame after frame.
func (l *Layout) AppendSelection(dst []Rect, start, end int, on bool) []Rect {
	if start > end {
		start, end = end, start
	}
	if on {
		end = len(l.Runes) + 1
	}
	out := dst
	var buf [16]Rect
	for li := range l.Lines {
		line := &l.Lines[li]
		// A range can include the newline at End, so keep that boundary's
		// line even when none of its glyphs are selected.
		if line.End < start || line.Start >= end {
			continue
		}
		pieces := buf[:0]
		for i := 0; i < len(line.Glyphs); {
			first := line.Glyphs[i]
			x0, x1 := first.X, first.X+first.Advance
			i++
			for i < len(line.Glyphs) && line.Glyphs[i].Cluster == first.Cluster && line.Glyphs[i].Runes == first.Runes {
				x0, x1 = min(x0, line.Glyphs[i].X), max(x1, line.Glyphs[i].X+line.Glyphs[i].Advance)
				i++
			}
			a, z := max(start, first.Cluster), min(end, first.Cluster+first.Runes)
			if a >= z || first.Runes <= 0 {
				continue
			}
			width := x1 - x0
			left, right := float32(a-first.Cluster)/float32(first.Runes), float32(z-first.Cluster)/float32(first.Runes)
			if first.RTL {
				left, right = 1-right, 1-left
			}
			r := Rect{x0 + left*width, line.Y, (right - left) * width, line.Height}
			// Clusters following each other join as they come, as most do.
			if n := len(pieces); n > 0 && pieces[n-1].X+pieces[n-1].W >= r.X-0.01 && pieces[n-1].X <= r.X+0.01 {
				pieces[n-1].W = max(pieces[n-1].X+pieces[n-1].W, r.X+r.W) - pieces[n-1].X
				continue
			}
			pieces = append(pieces, r)
		}
		if start <= line.End && end > line.End && (li < len(l.Lines)-1 && l.Lines[li+1].Start > line.End || li == len(l.Lines)-1 && on) {
			x, width := line.X+line.Width, l.Params.Style.FontSize()/3
			if line.RTL {
				x = line.X - width
			}
			pieces = append(pieces, Rect{x, line.Y, width, line.Height})
		}
		// By x, insertion sorted: pieces are mostly in order already.
		for i := 1; i < len(pieces); i++ {
			for j := i; j > 0 && pieces[j-1].X > pieces[j].X; j-- {
				pieces[j-1], pieces[j] = pieces[j], pieces[j-1]
			}
		}
		for _, r := range pieces {
			if r.W <= 0 {
				continue
			}
			if n := len(out); n > len(dst) && out[n-1].Y == r.Y && out[n-1].X+out[n-1].W >= r.X-0.01 {
				out[n-1].W = max(out[n-1].X+out[n-1].W, r.X+r.W) - out[n-1].X
			} else {
				out = append(out, r)
			}
		}
	}
	return out
}

// Boundaries finds grapheme and word boundaries in text.
type Boundaries struct {
	seg   segmenter.Segmenter
	runes []rune
	// graphemes holds the start of every grapheme, then len(runes).
	graphemes []int
}

// Reset prepares b for runes.
func (b *Boundaries) Reset(runes []rune) {
	b.runes = runes
	b.graphemes = b.graphemes[:0]
	b.seg.Init(runes)
	it := b.seg.GraphemeIterator()
	for it.Next() {
		b.graphemes = append(b.graphemes, it.Grapheme().Offset)
	}
	b.graphemes = append(b.graphemes, len(runes))
}

// GraphemeOffsets returns a copy of the grapheme starts and text's end for
// a retained shaped-text layout.
func (b *Boundaries) GraphemeOffsets() []int { return slices.Clone(b.graphemes) }

// NextGrapheme returns the end of the grapheme starting at or containing i.
func (b *Boundaries) NextGrapheme(i int) int {
	for _, g := range b.graphemes {
		if g > i {
			return g
		}
	}
	return len(b.runes)
}

// PrevGrapheme returns the start of the grapheme before i.
func (b *Boundaries) PrevGrapheme(i int) int {
	prev := 0
	for _, g := range b.graphemes {
		if g >= i {
			return prev
		}
		prev = g
	}
	return prev
}

// NextWord returns the end of the word after i, skipping what is between.
// Each ideograph and kana is a word (see WordBoundary).
func (b *Boundaries) NextWord(i int) int {
	n := len(b.runes)
	for i < n && !IsWordRune(b.runes[i]) {
		i++
	}
	if i < n {
		i++
		for i < n && !WordBoundary(b.runes[i-1], b.runes[i]) {
			i++
		}
	}
	return i
}

// PrevWord returns the start of the word before i.
func (b *Boundaries) PrevWord(i int) int {
	for i > 0 && !IsWordRune(b.runes[i-1]) {
		i--
	}
	if i > 0 {
		i--
		for i > 0 && !WordBoundary(b.runes[i-1], b.runes[i]) {
			i--
		}
	}
	return i
}

// WordAt returns the word, or the run of other runes, around i, within
// its line: an ideograph or a kana is a word alone.
func (b *Boundaries) WordAt(i int) (start, end int) {
	n := len(b.runes)
	if n == 0 {
		return 0, 0
	}
	i = min(i, n-1)
	start, end = i, i
	for start > 0 && b.runes[start-1] != '\n' && !WordBoundary(b.runes[start-1], b.runes[start]) {
		start--
	}
	if b.runes[i] != '\n' {
		end = i + 1
		for end < n && b.runes[end] != '\n' && !WordBoundary(b.runes[end-1], b.runes[end]) {
			end++
		}
	}
	return start, end
}

// lineBreaks reports, for each rune of text and for its end, whether a
// line may break before it, in breaks, with seg; both are reused.
func lineBreaks(seg *segmenter.Segmenter, breaks []bool, text []rune) []bool {
	breaks = slices.Grow(breaks[:0], len(text)+1)[:len(text)+1]
	clear(breaks)
	seg.Init(text)
	it := seg.LineIterator()
	for it.Next() {
		l := it.Line()
		breaks[l.Offset+len(l.Text)] = true
	}
	return breaks
}

// Korean is written with spaces between words, and its lines break at
// them: a word of Hangul syllables is not broken across lines, as CSS's
// word-break: keep-all has it. Unicode's line breaking algorithm, which
// the text engines follow, allows a break between any two syllables, as
// between Han characters. So in the languages that keep their words whole
// (keepsWords), the engines take the breaks between two letters or digits
// out of those it allows (keepWordsWhole), and break lines at the others,
// as at spaces and after punctuation; a word longer than a line still
// breaks where the engine breaks it, as CSS's overflow-wrap does, unless
// the text keeps words whole anyway (Params.NoBreakWords).

// keepsWords reports whether the lines of text in language lang, a BCP 47
// tag, break between words only: Korean's.
func keepsWords(lang string) bool {
	base, _, _ := strings.Cut(lang, "-")
	base, _, _ = strings.Cut(base, "_")
	return strings.EqualFold(base, "ko")
}

// keepsAnyWords reports whether a text of language lang, with spans of
// their own languages, has runes in a language that keeps words whole.
func keepsAnyWords(lang string, spans []Span) bool {
	if keepsWords(lang) {
		return true
	}
	for _, sp := range spans {
		if sp.Lang != "" && keepsWords(sp.Lang) {
			return true
		}
	}
	return false
}

// keepWordsWhole takes out of breaks, where lines of text may break
// (lineBreaks), the breaks between two letters or digits of the runes in a
// language that keeps words whole: lang, the text's, or a span's. It
// reports whether it took any out.
func keepWordsWhole(breaks []bool, text []rune, lang string, spans []Span) bool {
	taken := false
	// The span of the rune last asked, and where it starts.
	span, from := 0, 0
	keeps := func(i int) bool {
		for span < len(spans) && max(spans[span].End, from) <= i {
			from = max(spans[span].End, from)
			span++
		}
		if span < len(spans) && spans[span].Lang != "" {
			return keepsWords(spans[span].Lang)
		}
		return keepsWords(lang)
	}
	for i := 1; i < len(text) && i < len(breaks); i++ {
		if !breaks[i] || !wordInside(text[i]) {
			continue
		}
		// A mark goes with the letter it is on.
		j := i - 1
		for j > 0 && unicode.Is(unicode.M, text[j]) {
			j--
		}
		if wordInside(text[j]) && keeps(j) && keeps(i) {
			breaks[i] = false
			taken = true
		}
	}
	return taken
}

// breakAt reports whether a line may break before UTF-16 unit u, 0 < u < n,
// of a text, given breaks of its runes and the rune of each unit, index: not
// within a surrogate pair.
func breakAt(breaks []bool, index []int, u int) bool {
	return index[u] != index[u-1] && breaks[index[u]]
}

// wordInside reports whether r is a letter or digit that a word keeping
// whole holds, as Chromium takes them for keep-all: but those of the
// scripts written without spaces between words, as Thai, whose breaks
// come from a dictionary.
func wordInside(r rune) bool {
	if !unicode.IsLetter(r) && !unicode.IsNumber(r) {
		return false
	}
	switch {
	case r < 0x0E00:
		return true
	case r <= 0x0EFF, // Thai, Lao
		r >= 0x1000 && r <= 0x109F,   // Myanmar
		r >= 0x1780 && r <= 0x17FF,   // Khmer
		r >= 0x1950 && r <= 0x1AAF,   // Tai Le, New Tai Lue, Khmer's symbols, Tai Tham
		r >= 0xA9E0 && r <= 0xA9FF,   // Myanmar Extended-B
		r >= 0xAA60 && r <= 0xAADF,   // Myanmar Extended-A, Tai Viet
		r >= 0x11700 && r <= 0x1174F: // Ahom
		return false
	}
	return true
}
