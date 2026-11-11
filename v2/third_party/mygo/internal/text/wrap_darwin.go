package text

import (
	"math"
	"strings"
	"unicode"
)

// Wrapping a text at a new width needs its line breaks, and Core Text finds
// those a line at a time, opening a line for each and shaping it into glyphs
// through a call for each of its properties. But a line of Latin text is a run
// of the glyphs of the whole text, moved back to the start of the line. So a
// text kept across widths (keptTypesetter) is shaped once into one line, whose
// glyphs its lines take, as GPUI does.
//
// Lines break where Core Text surely would: at the spaces before plain words.
// What could tell them apart goes to Core Text, line by line, or whole: all but
// plain Latin text, and a line ending where the next word does not fit and may
// break inside itself, or is too long for a line. And the first texts of a font,
// and one in every few hundred after them, are also wrapped by Core Text: a font
// where the two differ is left to it from then on.

const (
	// wrapCheckedFirst is how many wrappings in a font are checked before it
	// is trusted, and wrapCheckedEvery how often after that.
	wrapCheckedFirst = 16
	wrapCheckedEvery = 256
	// wrapTolerance is how far a position may be from Core Text's, which
	// hands them over rounded.
	wrapTolerance = 1e-3
)

// wrapFont is what wrapping keeps for a font.
type wrapFont struct {
	// space is the advance of a space with nothing after it, which a space
	// ending a line has; spaceKnown tells that it was measured.
	space      float32
	spaceKnown bool
	// lines is how many texts were wrapped, checked how many of them Core
	// Text agreed with, and disagreed that it did not with one.
	lines, checked uint64
	disagreed      bool
}

// shapedWhole is a text shaped into one line: for each rune its glyph, its
// advance, and the pen before it, with the pen at the end.
type shapedWhole struct {
	font     *Font
	ids      []uint32
	advances []float32
	pens     []float64
}

// plainLatin reports whether text is ASCII or Latin-1 letters, without tabs and
// other marks line breaking or shaping may treat apart.
func plainLatin(text []rune) bool {
	for _, r := range text {
		if r < 0x20 || r > 0x7E && (r < 0xC0 || r > 0xFF) {
			return false
		}
	}
	return true
}

// shapeWhole shapes a kept text into one line, or returns nil if it does not
// come out as one run with a glyph for each rune.
func (e *coreText) shapeWhole(typesetter uintptr, text []rune, italic bool) *shapedWhole {
	n := len(text)
	line := ct.typesetterCreateLine(typesetter, cfRange{0, n})
	if line == 0 {
		return nil
	}
	defer ct.release(line)
	runs := ct.lineGetGlyphRuns(line)
	if ct.arrayGetCount(runs) != 1 {
		return nil
	}
	run := ct.arrayGetValueAtIndex(runs, 0)
	if ct.runGetGlyphCount(run) != n || ct.runGetStatus(run)&ctRunStatusRightToLeft != 0 {
		return nil
	}
	attrs := ct.runGetAttributes(run)
	w := &shapedWhole{
		font:     e.runFont(ct.dictionaryGetValue(attrs, ct.fontAttributeName), italic || ct.dictionaryGetValue(attrs, e.italic) != 0),
		ids:      make([]uint32, n),
		advances: make([]float32, n),
		pens:     make([]float64, n+1),
	}
	e.runIDs = growTo(e.runIDs, n)
	e.runPoints = growTo(e.runPoints, n)
	e.runAdvances = growTo(e.runAdvances, n)
	e.runIndices = growTo(e.runIndices, n)
	all := cfRange{}
	ct.runGetGlyphs(run, all, &e.runIDs[0])
	ct.runGetPositions(run, all, &e.runPoints[0])
	ct.runGetAdvances(run, all, &e.runAdvances[0])
	ct.runGetStringIndices(run, all, &e.runIndices[0])
	for i := range n {
		if e.runIndices[i] != i || e.runPoints[i].y != 0 {
			return nil
		}
		w.ids[i] = uint32(e.runIDs[i])
		w.advances[i] = float32(e.runAdvances[i].w)
		w.pens[i] = e.runPoints[i].x
	}
	w.pens[n] = w.pens[n-1] + e.runAdvances[n-1].w
	return w
}

// plainWord reports whether a line can only break before or after a word:
// letters and digits, perhaps with an apostrophe, which may open with a bracket
// or a quote, and close with them or a mark of punctuation.
func plainWord(word []rune) bool {
	i, n := 0, len(word)
	for i < n && strings.ContainsRune("([{\"'", word[i]) {
		i++
	}
	for n > i && strings.ContainsRune(".,;:!?)]}\"'", word[n-1]) {
		n--
	}
	if i == n {
		return false
	}
	for _, r := range word[i:n] {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '\'' {
			return false
		}
	}
	return true
}

func growTo[T any](s []T, n int) []T {
	if cap(s) < n {
		return make([]T, n)
	}
	return s[:n]
}

// wrapKept wraps a kept text from its one line, or returns nil for Core Text to
// wrap it.
func (e *coreText) wrapKept(kept *keptTypesetter, text []rune, style Style, width float32) []shapedLine {
	f := e.wrapFonts[kept.font]
	if f == nil {
		f = &wrapFont{}
		e.wrapFonts[kept.font] = f
	}
	if f.disagreed {
		return nil
	}
	if !kept.shaped {
		kept.shaped = true
		kept.whole = e.shapeWhole(kept.typesetter, text, style.Italic)
	}
	w := kept.whole
	if w == nil {
		return nil
	}
	if !f.spaceKnown {
		f.spaceKnown = true
		f.space = -1
		if run := oneRun(e.shapeText([]rune{' '}, Style{Size: style.Size, Family: style.Family, Weight: style.Weight, Italic: style.Italic}, nil, 0, false, false)); run != nil && len(run.glyphs) == 1 {
			f.space = run.glyphs[0].Advance
		}
	}
	if f.space < 0 {
		return nil
	}
	n := len(text)
	limit := float64(width) + 1.0/1024
	var lines []shapedLine
	mark := len(e.shapeScratch.lines)
	for start := 0; start < n; {
		end, pen, plain := start, w.pens[start], true
		for i := start; i < n; {
			// A word and the spaces after it, which take no room.
			word := i
			for i < n && text[i] != ' ' {
				i++
			}
			fits := w.pens[i]-pen <= limit
			plain = plainWord(text[word:i])
			if !fits {
				break
			}
			for i < n && text[i] == ' ' {
				i++
			}
			end = i
		}
		if end == start || end < n && !plain {
			// The first word does not fit, or the next may break inside.
			count := max(ct.suggestLineBreak(kept.typesetter, start, limit), 1)
			line := ct.typesetterCreateLine(kept.typesetter, cfRange{start, count})
			if line == 0 {
				return nil
			}
			e.index = runeIndex(e.index, n)
			lines = e.addLine(mark, e.line(line, e.index, n, style.Italic))
			ct.release(line)
			start += count
			continue
		}
		glyphs := e.glyphRoom(end - start)
		for i := start; i < end; i++ {
			glyphs[i-start] = Glyph{Font: w.font, ID: w.ids[i], X: float32(w.pens[i] - pen), Advance: w.advances[i], Cluster: i}
		}
		if end < n {
			glyphs[end-start-1].Advance = f.space
		}
		line := shapedLine{start: start, end: end, runs: e.addRun(len(e.shapeScratch.runs), shapedRun{font: w.font, start: start, end: end, glyphs: glyphs})}
		lines = e.addLine(mark, line)
		start = end
	}
	f.lines++
	if f.checked < wrapCheckedFirst || f.lines%wrapCheckedEvery == 0 {
		e.index = runeIndex(e.index, n)
		shaped := e.typesetLines(kept.typesetter, n, width, nil, false, false, e.index, style.Italic)
		if !sameLines(lines, shaped) {
			f.disagreed = true
			return shaped
		}
		f.checked++
	}
	return lines
}

// runeIndex returns the identity map of a text of BMP runes, as appendUTF16
// makes it, up to its end.
func runeIndex(index []int, n int) []int {
	index = growTo(index, n+1)
	for i := range index {
		index[i] = i
	}
	return index
}

// sameLines reports whether lines wrapped from one line are those Core Text
// wrapped.
func sameLines(a, b []shapedLine) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].start != b[i].start || a[i].end != b[i].end {
			return false
		}
		x, y := oneRun(a[i:i+1]), oneRun(b[i:i+1])
		if x == nil || y == nil || x.font != y.font || x.start != y.start || x.end != y.end || len(x.glyphs) != len(y.glyphs) {
			return false
		}
		for k := range x.glyphs {
			g, h := &x.glyphs[k], &y.glyphs[k]
			if g.ID != h.ID || g.Font != h.Font || g.Cluster != h.Cluster || g.RTL != h.RTL || g.Y != h.Y ||
				math.Abs(float64(g.X-h.X)) > wrapTolerance || math.Abs(float64(g.Advance-h.Advance)) > wrapTolerance {
				return false
			}
		}
	}
	return true
}
