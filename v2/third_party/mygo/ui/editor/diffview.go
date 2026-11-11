package editor

import (
	"sort"
	"strconv"
	"time"

	"github.com/egoist/mygo/internal/l10n"
	"github.com/egoist/mygo/ui"
)

// DiffState is a diff viewer's state, kept by the app across
// frames: the texts it compares, its options, and what it keeps of its
// own, as the lines expanded and its scroll position. Zero options are
// the defaults, those of Pierre's diffs.
type DiffState struct {
	// Old and New are the texts compared; empty buffers are made when
	// nil. New is the one edited when Editable, and can be the buffer of
	// an editor's too: an edit in either shows in both.
	Old, New *Buffer
	// Layout shows the texts side by side, or one over the other.
	Layout DiffLayout
	// SplitPercent is the percentage of a split view's width the old
	// side takes, from 0 to 100, half when 0: the user drags the line
	// between the sides to change it, or moves it with the arrows once it
	// has the keyboard focus, as ui.SplitPercent's divider, and the app
	// keeps it to show the sides as the user left them. Each side keeps
	// the room of its gutter and 12 columns of text, where the view is
	// wide enough for both, and the share is kept for when it is again.
	SplitPercent float32
	// Editable lets the new text be edited where it shows, with all an
	// editor does; the diff follows as it is typed.
	Editable bool
	// Inline highlights what changed within changed lines.
	Inline DiffInline
	// Indicators mark changed lines: with bars, + and -, or nothing.
	Indicators DiffIndicators
	// ContextLines is how many unchanged lines show around changes, 3
	// when 0, none when negative; the others collapse into separators
	// that expand them, ExpansionLines (100 when 0) a click, all of them
	// with Shift. ExpandUnchanged shows them all.
	ContextLines    int
	ExpansionLines  int
	ExpandUnchanged bool
	// NoBackground leaves changed lines without their tint, and
	// NoLineNumbers the gutter without numbers.
	NoBackground  bool
	NoLineNumbers bool
	// Wrap wraps long lines rather than scrolling them sideways.
	Wrap bool
	// FitContent makes the viewer as high as its lines, as a file of a
	// list of them, rather than scrolling them itself.
	FitContent bool
	// KeepScrollbars and NoScrollbars are those of State: the scroll
	// bars stay with the pointer away, or are taken away with their room.
	KeepScrollbars bool
	NoScrollbars   bool
	// Font, LineHeight, TabSize and Theme are those of State.
	Font       ui.Font
	LineHeight float32
	TabSize    int
	Theme      *Theme
	// Bindings add key bindings to the editors of the sides.
	Bindings []Binding
	// HunkActions shows Revert and Keep on the change under the pointer,
	// or the caret's, which put the old lines back in New, or make the
	// new lines Old's, as an agent's changes are reviewed.
	HunkActions bool
	// LineSelection lets the numbers of lines be clicked and dragged to
	// select lines, which OnLineSelection reports.
	LineSelection   bool
	OnLineSelection func(DiffLineRange)
	// OnGutterButton shows a + on the gutter of the line under the
	// pointer, as to comment on it, and is called with the line when it
	// is clicked.
	OnGutterButton func(DiffLineRange)
	// Annotations show what RenderAnnotation builds under lines, as
	// comments of a review, as high as each asks; the other side of a
	// split view leaves the room empty.
	Annotations      []DiffAnnotation
	RenderAnnotation func(c *ui.Context, a DiffAnnotation)

	// RenderSeparator, RenderHunkActions and RenderGutterButton build the
	// viewer's separators, the actions on a change and the gutter's
	// button in place of its own (DefaultSeparator, DefaultHunkActions
	// and DefaultGutterButton, which they can call to wrap them): each is
	// given the element the viewer places, to style and fill, and what
	// the part shows and does. A separator is as wide as the viewer, less
	// the room of the scroll bar's track while the viewer scrolls, and 32
	// DIPs high.
	RenderSeparator    func(c *ui.Context, sep ui.Element, s *DiffSeparator)
	RenderHunkActions  func(c *ui.Context, bar ui.Element, a *DiffHunkActions)
	RenderGutterButton func(c *ui.Context, btn ui.Element, line DiffLineRange)
	// RenderFindBar and RenderGoToLine build the bars of the sides'
	// editors, as State's.
	RenderFindBar  func(c *ui.Context, bar ui.Element, f *FindBar)
	RenderGoToLine func(c *ui.Context, bar ui.Element, g *GoToLineBar)

	model   diffModel
	eds     [2]editor // the old side's and the new side's
	sides   [2]diffSide
	oldView view // the old text, for its lines in a unified view
	scroll  sharedScroll
	split   diffSplit
	// revealed are the unchanged lines expanded, in the old text, which
	// edits of the new text leave where they are; revealedVer is the old
	// text's version they are of.
	revealed    []lineSpan
	revealedVer uint64
	revealedOf  *Buffer
	expandV     int
	selected    DiffLineRange
	hasSel      bool
	// oldActive is set while the old side of a split view has the
	// keyboard, and stays after it goes, until the new side has it.
	oldActive bool
	prepared  diffPrepared
	// blocksV counts the blocks prepare made, and built is the geometry
	// the separators and buttons were built for, which a frame changing
	// it builds again (diffGeom).
	blocksV int
	built   diffGeom
	gapList []diffGap
	partial *diffPartial
}

// DiffLayout is how a diff viewer lays out its two texts.
type DiffLayout uint8

const (
	// DiffSplit shows the old text on the left and the new on the right,
	// their lines aligned.
	DiffSplit DiffLayout = iota
	// DiffUnified shows the new text, with the lines deleted from the
	// old above where they were.
	DiffUnified
)

// DiffInline is how a diff viewer highlights what changed within lines.
type DiffInline uint8

const (
	// DiffInlineWordAlt highlights words, joining those one character
	// apart, as "a b c" changed reads as one highlight.
	DiffInlineWordAlt DiffInline = iota
	// DiffInlineWord highlights each word changed.
	DiffInlineWord
	// DiffInlineWordLine highlights from the first word changed to the
	// last.
	DiffInlineWordLine
	// DiffInlineChar highlights characters.
	DiffInlineChar
	// DiffInlineNone highlights nothing within lines.
	DiffInlineNone
)

// DiffIndicators is how a diff viewer marks changed lines.
type DiffIndicators uint8

const (
	// DiffBars draws a bar at the gutter's edge, striped for deletions.
	DiffBars DiffIndicators = iota
	// DiffClassic shows + and - before the lines.
	DiffClassic
	// DiffNoIndicators marks lines by their background alone.
	DiffNoIndicators
)

// DiffSide is a side of a diff: the old text or the new.
type DiffSide uint8

const (
	DiffOld DiffSide = iota
	DiffNew
)

// DiffLineRange is lines Start to End, inclusive, from 0, of a side.
type DiffLineRange struct {
	Side       DiffSide
	Start, End int
}

// DiffAnnotation is room under a line of a side for what
// RenderAnnotation builds there, Height DIPs high. Key identifies it
// among the others.
type DiffAnnotation struct {
	Side   DiffSide
	Line   int
	Height float32
	Key    any
}

// DiffHunk is a change: the old lines OldStart to OldEnd, exclusive,
// replaced by the new lines NewStart to NewEnd.
type DiffHunk struct {
	OldStart, OldEnd, NewStart, NewEnd int
}

func (d *DiffState) context() int {
	switch {
	case d.ContextLines < 0:
		return 0
	case d.ContextLines == 0:
		return 3
	}
	return d.ContextLines
}

func (d *DiffState) expansionLines() int {
	if d.ExpansionLines <= 0 {
		return 100
	}
	return d.ExpansionLines
}

func (d *DiffState) ensure() {
	if d.Old == nil {
		d.Old = NewBuffer("")
	}
	if d.New == nil {
		d.New = NewBuffer("")
	}
	if pt := d.partial; pt != nil && (pt.old != d.Old || pt.new != d.New) {
		// Texts of the app's in place of a patch's.
		d.partial = nil
		d.prepared = diffPrepared{}
	}
	d.model.set(d.Old, d.New)
	d.model.update()
	// The lines revealed move with edits of the old text, as Keep makes,
	// and go with it.
	if d.revealedOf != d.Old {
		d.revealed, d.revealedOf, d.revealedVer = nil, d.Old, d.Old.Version()
		d.expandV++
	}
	if ver := d.Old.Version(); ver != d.revealedVer {
		if changes, _, ok := d.Old.changesSince(d.revealedVer); ok && len(d.revealed) > 0 {
			for _, cs := range changes {
				for i := range d.revealed {
					d.revealed[i] = lineSpan{mapLine(d.revealed[i].from, cs), mapLine(d.revealed[i].to, cs)}
				}
			}
		} else if !ok {
			d.revealed = nil
		}
		d.revealedVer = ver
		d.expandV++
	}
}

// Hunks returns the changes between the texts.
func (d *DiffState) Hunks() []DiffHunk {
	d.ensure()
	out := make([]DiffHunk, len(d.model.hunks))
	for i, h := range d.model.hunks {
		out[i] = DiffHunk{h.A0, h.A1, h.B0, h.B1}
	}
	return out
}

// Stats returns how many lines the changes add and delete.
func (d *DiffState) Stats() (added, deleted int) {
	d.ensure()
	return d.model.stats()
}

// Revert puts the old lines of hunk i back in New.
func (d *DiffState) Revert(i int) {
	d.ensure()
	d.model.revert(i)
}

// Accept makes the new lines of hunk i Old's, so it is no longer a
// change.
func (d *DiffState) Accept(i int) {
	d.ensure()
	d.model.accept(i)
}

// ExpandAll shows every unchanged line; CollapseAll collapses them again
// into separators.
func (d *DiffState) ExpandAll() {
	d.ensure()
	d.ExpandUnchanged = true
}

func (d *DiffState) CollapseAll() {
	d.ExpandUnchanged = false
	d.revealed = nil
	d.expandV++
}

// SelectedLines returns the lines selected by their numbers, and whether
// there are.
func (d *DiffState) SelectedLines() (DiffLineRange, bool) { return d.selected, d.hasSel }

// SetSelectedLines selects lines, as clicking their numbers does; ok
// false clears the selection.
func (d *DiffState) SetSelectedLines(r DiffLineRange, ok bool) {
	if r.End < r.Start {
		r.Start, r.End = r.End, r.Start
	}
	d.selected, d.hasSel = r, ok
}

// GoToHunk puts the caret of the active side (ActiveSide) at the start
// of hunk i and scrolls it to the middle of the view.
func (d *DiffState) GoToHunk(i int) {
	d.ensure()
	if i < 0 || i >= len(d.model.hunks) {
		return
	}
	h := d.model.hunks[i]
	side := d.ActiveSide()
	ed := d.editorOf(side)
	line := h.B0
	if side == DiffOld {
		line = h.A0
	}
	r := &ed.view.text
	off := r.LineStart(min(line, r.LineCount()-1))
	ed.setSels([]sel{ed.newSel(off, off)}, revealCenter)
}

// ScrollY returns how far the diff's rows are scrolled up, in DIPs, as the
// last frame laid them out, however they were scrolled: by the wheel, the
// keys, the scroll bar, GoToHunk or SetScrollY. It is the new side's, which
// the old side of a split view shares, 0 for a viewer as high as its lines
// (FitContent). SetScrollY scrolls them, as far as they scroll, as a
// minimap beside the diff does when it is clicked or dragged.
func (d *DiffState) ScrollY() float64 { return d.eds[1].scrollY }

func (d *DiffState) SetScrollY(y float64) {
	ed := d.editorOf(DiffNew)
	if d.Layout == DiffSplit && !d.FitContent {
		// Both sides, which the frame keeps together (sharedScroll).
		d.scroll.set(y)
		return
	}
	ed.setScroll(y)
}

// ViewportHeight returns the height of the part of the diff the viewer
// shows, in DIPs, as the last frame laid it out: from ScrollY to ScrollY
// plus ViewportHeight, against LineTop, is what is in view. A viewer as
// high as its lines (FitContent) is that high.
func (d *DiffState) ViewportHeight() float64 { return float64(d.eds[1].viewH) }

// LineTop returns the top of a line of a side, from 0, in DIPs below the
// top of the diff's first row, as the last frame laid the diff out: less
// ScrollY, it is where the line shows in the viewer, as State's LineTop.
// The separators, the annotations and the room a split view leaves across
// from lines of the other side take their heights; unchanged lines
// collapsed into a separator take none, and are at its top. In a unified
// view, a line the new text deleted is among the deleted lines above its
// change, and an unchanged line of the old text where it is in the new.
// LineTop of a side's count of lines is the height of the whole diff, as
// a minimap scales it.
func (d *DiffState) LineTop(side DiffSide, line int) float64 {
	d.ensure()
	line = max(line, 0)
	h := &d.eds[1].view.heights
	switch {
	case side == DiffOld && d.Layout == DiffSplit:
		h = &d.eds[0].view.heights
	case side == DiffOld:
		if nl, ok := d.model.newLineOf(line); ok || line >= d.Old.LineCount() {
			if !ok {
				nl = h.n
			}
			return rowsTop(h, nl)
		}
		return d.deletedTop(line)
	}
	return rowsTop(h, line)
}

// rowsTop returns the top of the rows of line, below its blocks above;
// for a line collapsed into a separator, the separator's top; past the
// last line, the height of them all.
func rowsTop(h *heights, line int) float64 {
	if line >= h.n {
		return h.total()
	}
	if h.isHidden(line) {
		// The separator of the run is above the line after it, or, for
		// the run after the last change, below the line before it.
		i := sort.Search(len(h.hidden), func(i int) bool { return h.hidden[i].to >= line })
		span := h.hidden[i]
		sep := func(b *block) bool { return b.kind == blockSeparator && line >= b.from && line < b.to }
		if span.to+1 < h.n {
			if top, ok := blockTop(h, span.to+1, false, sep); ok {
				return top
			}
		}
		if span.from > 0 {
			if top, ok := blockTop(h, span.from-1, true, sep); ok {
				return top
			}
		}
	}
	return h.geometry(line).rows
}

// blockTop returns the top of the first block of line that is is, among
// those above it or those below, and whether there is one.
func blockTop(h *heights, line int, below bool, is func(b *block) bool) (float64, bool) {
	g := h.geometry(line)
	above, under := h.blocksOf(line)
	blks, top := above, g.top
	if below {
		blks, top = under, g.below
	}
	for i := range blks {
		if is(&blks[i]) {
			return top, true
		}
		top += float64(blks[i].height)
	}
	return 0, false
}

// deletedTop returns the top of a deleted line of the old text in a
// unified view, in the block of the deleted lines of its change.
func (d *DiffState) deletedTop(line int) float64 {
	ed := &d.eds[1]
	h := &ed.view.heights
	n := h.n
	for i, hk := range d.model.hunks {
		if line < hk.A0 || line >= hk.A1 {
			continue
		}
		// Where prepare put the block: above the change's first new line,
		// or below the last line of the text.
		at, below := hk.B0, false
		if at >= n {
			at, below = n-1, true
		}
		if at < 0 {
			return 0
		}
		top, ok := blockTop(h, at, below, func(b *block) bool { return b.kind == blockDeleted && b.hunk == i })
		if !ok {
			return h.geometry(at).rows
		}
		for l := hk.A0; l < line; l++ {
			top += float64(float32(d.oldView.heights.wraps.rows(l))*ed.view.lineHeight + d.annotationHeight(DiffOld, l))
		}
		return top
	}
	return h.total()
}

// Run runs a command in the editor of the active side (ActiveSide), as
// its key would, and reports whether it did something: Find opens its
// find bar, as an app's menu item does.
func (d *DiffState) Run(cmd Command) bool {
	ed := d.editorOf(d.ActiveSide())
	return ed.retry(func() bool { return ed.run(cmd) })
}

// Find opens the find bar of the active side's editor (ActiveSide) with
// query, and selects its first match after the caret.
func (d *DiffState) Find(query string) {
	ed := d.editorOf(d.ActiveSide())
	f := &ed.find
	f.open, f.query, f.stale = true, query, true
	f.step(ed, 1)
}

// ActiveSide returns the side that has the keyboard, or had it last: the
// new side before either had it, and in a unified view. Cursor,
// CursorPos, CurrentHunk, GoToHunk, Run and Find act in its editor.
func (d *DiffState) ActiveSide() DiffSide {
	if d.oldActive && d.Layout == DiffSplit {
		return DiffOld
	}
	return DiffNew
}

// Cursor returns the offset of the newest caret of the active side, in
// its text.
func (d *DiffState) Cursor() int {
	return d.editorOf(d.ActiveSide()).primary().head
}

// CursorPos returns the line and column of the newest caret of the
// active side, in its text.
func (d *DiffState) CursorPos() Pos {
	ed := d.editorOf(d.ActiveSide())
	return posOf(ed.view.text, ed.primary().head)
}

// CurrentHunk returns the hunk the caret of the active side is in, with
// inside true, as for Revert; else the last hunk before the caret, as
// "2 of 5" counts changes and Next Change steps from (GoToHunk(i+1)), or
// -1 before the first. On a side the hunk has no lines of, as the new
// side of a deletion, the caret is in it on the line where its lines
// were.
func (d *DiffState) CurrentHunk() (i int, inside bool) {
	side := d.ActiveSide()
	ed := d.editorOf(side)
	line := ed.view.text.LineOf(ed.primary().head)
	if j := d.model.hunkAt(line, side == DiffOld); j >= 0 {
		return j, true
	}
	i = -1
	for j, h := range d.model.hunks {
		start := h.B0
		if side == DiffOld {
			start = h.A0
		}
		if start > line {
			break
		}
		i = j
	}
	return i, false
}

// editorOf returns the editor of a side, made ready to act in before the
// viewer is first built.
func (d *DiffState) editorOf(side DiffSide) *editor {
	d.ensure()
	ed, buf := &d.eds[side], d.New
	if side == DiffOld {
		buf = d.Old
	}
	if ed.view.buf != buf || ed.sels == nil {
		ed.init(buf, d.sideOptions(side == DiffOld), ui.LightTheme())
	}
	ed.sync()
	return ed
}

// sideOptions are the options of the editor of the old side, or the new.
func (d *DiffState) sideOptions(old bool) options {
	// Both gutters as wide, so that both sides wrap alike.
	digits := max(3, len(strconv.Itoa(max(d.Old.LineCount(), d.New.LineCount()))))
	o := options{
		readOnly: old || !d.Editable, font: d.Font, lineHeight: d.LineHeight, tabSize: d.TabSize,
		wrap: d.Wrap, lineNumbers: LineNumbersOn, whitespace: WhitespaceNone, autoClose: true,
		theme: d.Theme, bindings: d.Bindings, fitContent: d.FitContent, minDigits: digits,
		keepScrollbars: d.KeepScrollbars, hideScrollbars: d.NoScrollbars,
		renderFindBar: d.RenderFindBar, renderGoToLine: d.RenderGoToLine,
	}
	if d.NoLineNumbers {
		o.lineNumbers = LineNumbersOff
	}
	o.diffSide = diffNewSide
	if old {
		o.diffSide = diffOldSide
	}
	return o
}

// DiffView builds the diff viewer.
func DiffView(c *ui.Context, d *DiffState) ui.Element {
	d.ensure()
	// The new side's editor scrolls as it is built, as to a caret
	// GoToHunk moved, where a split view's scrolls as it is drawn
	// (rebuildIfMoved): either builds the view again, for what read
	// ScrollY before.
	scrolled := d.eds[1].scrollY
	defer func() {
		if d.eds[1].scrollY != scrolled {
			c.Invalidate()
		}
	}()
	t := c.Theme()
	ct := d.Theme.resolve(t)
	opts := d.sideOptions
	d.sides[0] = diffSide{d: d, old: true, hide: d.sides[0].hide, blks: d.sides[0].blks}
	d.sides[1] = diffSide{d: d, unified: d.Layout == DiffUnified, hide: d.sides[1].hide, blks: d.sides[1].blks}
	if d.Layout == DiffUnified {
		d.oldView.setTheme(ct)
		d.oldView.setBuffer(d.Old)
		d.oldView.sync(nil)
		o := opts(false)
		o.decor = &d.sides[1]
		if d.Indicators == DiffClassic {
			o.signs = true
		}
		root := ui.Box(c).Direction(ui.LeftToRight).Clip()
		root.Draw(func(p *ui.Painter, r ui.Rect) {
			ed := &d.eds[1]
			ed.layout(r.W, r.H, p.Scale())
			d.oldView.setStyle(ed.view.font, ed.o.lineHeight, p.Scale(), ed.tabSize())
			d.oldView.setWrap(ed.view.wrap, ed.view.wrapWidth)
			d.prepare()
			ed.layout(r.W, r.H, p.Scale())
			d.rebuildIfMoved()
		})
		root.Children(func() {
			ed := d.eds[1].build(c, d.New, o)
			ed.Fill()
			if d.FitContent {
				ed.FillWidth()
			}
			d.buildSeparators(c, ct)
		})
		if d.FitContent {
			d.fit()
			root.Height(float32(d.eds[1].view.heights.total()))
		}
		return root
	}
	// Side by side: two editors sharing their scroll.
	d.scroll.eds = []*editor{&d.eds[0], &d.eds[1]}
	// The old text on the left, as code reads left to right.
	row := ui.Row(c).Direction(ui.LeftToRight).Clip()
	row.Draw(func(p *ui.Painter, r ui.Rect) {
		if r.W != d.split.width {
			// Resized: the build shares the new width between the sides.
			d.split.width = r.W
			d.eds[1].services.Invalidate()
		}
		w := d.split.widths(r.W)
		for i := range d.eds {
			d.eds[i].layout(w[i], r.H, p.Scale())
		}
		if d.Wrap && d.eds[0].view.wrapWidth != d.eds[1].view.wrapWidth {
			// Sides wrapping at unlike widths are aligned line by line
			// (prepare): the lines in view measured first, which the
			// editors would measure as they paint, after.
			for i := range d.eds {
				d.eds[i].view.measureWraps(d.eds[i].anchorLine, 0)
			}
		}
		d.prepare()
		for i := range d.eds {
			d.eds[i].layout(w[i], r.H, p.Scale())
		}
		if !d.FitContent {
			d.scroll.set(d.scroll.y)
			// The new side reveals its carets for both.
			ed := &d.eds[1]
			if ed.reveal != revealNone {
				ed.doReveal()
				d.scroll.set(ed.scrollY)
			}
			if ed0 := &d.eds[0]; ed0.reveal != revealNone {
				ed0.doReveal()
				d.scroll.set(ed0.scrollY)
			}
		}
		d.rebuildIfMoved()
	})
	row.Children(func() {
		var sides [2]ui.Element
		var div ui.Element
		for i, b := range []*Buffer{d.Old, d.New} {
			o := opts(i == 0)
			o.decor = &d.sides[i]
			o.sharedScroll = &d.scroll
			if d.FitContent {
				o.sharedScroll = nil
			}
			o.noScrollbar = i == 0
			if d.Indicators == DiffClassic {
				o.signs = true
			}
			sides[i] = d.eds[i].build(c, b, o).Basis(0).FillHeight()
			if i == 0 {
				div = ui.Box(c).Key("diff divider").Width(1).Shrink(0).FillHeight()
			}
			if d.eds[i].focused {
				d.oldActive = i == 0
			}
		}
		if d.buildDivider(c, ct, div) {
			row.SetChanged()
		}
		for i := range sides {
			sides[i].Grow(d.split.grow[i])
		}
		d.buildSeparators(c, ct)
	})
	if d.FitContent {
		d.fit()
		row.Height(float32(max(d.eds[0].view.heights.total(), d.eds[1].view.heights.total())))
	}
	return row
}

// diffMinColumns is how many columns of text a side of a split view
// keeps at least, besides its gutter, wherever the line between the
// sides is dragged.
const diffMinColumns = 12

// diffSplit is how a split view shares its width between its sides.
type diffSplit struct {
	// width is the view's as the last frame drew it, which the build
	// shares out and the divider moves within.
	width float32
	// grow are the sides' factors of growth as built, by which the frame
	// lays them out (widths).
	grow [2]float32
}

// widths returns the widths the layout gives the sides in a view w wide,
// the divider taking 1 DIP: its arithmetic, that of a row whose items
// grow from nothing, so that the sides are laid out for painting at the
// widths they are drawn at to the bit.
func (s *diffSplit) widths(w float32) [2]float32 {
	free := w - 1
	var sum float32
	sum += s.grow[0]
	sum += s.grow[1]
	if sum == 0 {
		return [2]float32{}
	}
	return [2]float32{free * s.grow[0] / sum, free * s.grow[1] / sum}
}

// sideMin returns the least width of a side of a split view: its gutter
// and diffMinColumns columns of text, as its editor was last laid out,
// and at least what ui.Split keeps of a pane.
func (d *DiffState) sideMin(t *ui.Theme) float32 {
	m := t.Space(10)
	for i := range d.eds {
		ed := &d.eds[i]
		m = max(m, ed.textX+diffMinColumns*ed.view.charWidth+ed.trackWidth()+4)
	}
	return m
}

// buildDivider builds the line between the sides of a split view, div,
// as ui.Split's divider: the pointer drags it, from a handle a few DIPs
// wide over either side, and it takes the focus between the sides, in
// their order, and moves with the arrows. It shares the view's width
// between the sides by SplitPercent (split.grow), and reports whether a
// drag or a key moved it.
func (d *DiffState) buildDivider(c *ui.Context, ct *Theme, div ui.Element) bool {
	t := c.Theme()
	minSide, grip, step := d.sideMin(t), t.Space(1.5), t.Space(2.5)
	// The room to share, as the last frame drew it: none before.
	room := max(d.split.width-1, 0)
	fits := d.split.width > 0 && room >= 2*minSide
	fit := func(v float32) float32 { return max(minSide, min(v, room-minSide)) }
	share := min(d.SplitPercent, 100) / 100
	// shown is the old side's width as the last frame laid it out, which
	// drags and keys move the line from: half the room with no share, or
	// where the sides do not fit beside each other.
	shown := room / 2
	if d.SplitPercent > 0 && fits {
		shown = fit(share * room)
	}
	moved := false
	set := func(v float32) {
		if !fits {
			return
		}
		if v = fit(v); v != shown {
			d.SplitPercent, shown, moved = v*100/room, v, true
		}
	}
	div.Focusable().FocusRing(false).Role(ui.RoleSplitter).Label(l10n.In(c.Locale()).Divider())
	// The arrows move the line the way they point: the view is laid out
	// left to right in either direction, as code reads.
	if div.Shortcut(0, ui.KeyLeft) {
		set(shown - step)
	}
	if div.Shortcut(0, ui.KeyRight) {
		set(shown + step)
	}
	// The handle the pointer drags spans a few DIPs over either side,
	// above them. The wheel over it scrolls the diff, as over the lines.
	handle := ui.Box(c).Key("diff divider handle").Absolute().Top(0).Bottom(0).Width(grip).
		Cursor(ui.CursorResizeEW).HandleInput(d.eds[1].wheelOnly)
	// A press there focuses the line, as a press on it would; only a move
	// moves it.
	dx, _, held := handle.Dragged()
	if held {
		div.Focus()
		if dx != 0 {
			set(shown + dx)
		}
	}
	handle.Left(shown - (grip-1)/2)
	lo, hi := minSide, max(room-minSide, minSide)
	if !fits {
		lo, hi = shown, shown
	}
	div.Range(float64(lo), float64(hi), float64(shown)).Step(float64(step))
	line := ct.Background.Mix(t.Border, 0.6)
	if held || handle.Hovered() {
		line = t.Accent
	}
	div.Background(line)
	handle.Draw(func(p *ui.Painter, r ui.Rect) {
		if div.FocusVisible() {
			p.FocusRing(r, [4]float32{})
		}
	})
	// The layout shares this frame's width as the last frame's: the old
	// side's width, as shown, and the rest; halves, as two items growing
	// alike are laid out, where the share is not set or the sides do not
	// fit beside each other.
	switch {
	case d.SplitPercent > 0 && fits:
		d.split.grow = [2]float32{shown, room - shown}
	case d.SplitPercent > 0 && d.split.width == 0:
		d.split.grow = [2]float32{share, 1 - share}
	default:
		d.split.grow = [2]float32{1, 1}
	}
	return moved
}

// fit lays out a diff that fits its content as its Draw does, its
// unchanged lines folded, at the size its editors were built at. A diff
// not drawn yet, as one below the view of a scroll container, is then as
// high as it shows, not as all its lines: else it would shrink as it came
// into view, and the container's end with it, under the user scrolling.
func (d *DiffState) fit() {
	eds := d.eds[:]
	if d.Layout == DiffUnified {
		eds = d.eds[1:]
		ed := &d.eds[1]
		d.oldView.setStyle(ed.view.font, ed.o.lineHeight, max(1, ed.view.scale), ed.tabSize())
		d.oldView.setWrap(ed.view.wrap, ed.view.wrapWidth)
	}
	layout := func() {
		d.prepare()
		for i := range eds {
			ed := &eds[i]
			ed.layout(ed.fitWidth(), ed.viewH, max(1, ed.view.scale))
		}
	}
	layout()
	// The wraps of the lines shown, those folded away left, and the
	// layout again for the rows they found.
	measured := false
	for i := range eds {
		v := &eds[i].view
		before := v.heights.wraps.version
		v.measureWraps(0, time.Second)
		measured = measured || v.heights.wraps.version != before
	}
	if measured {
		layout()
	}
}

// expand reveals lines of the gap starting at old line a: at its top,
// below the change before it (dir 1), at its bottom (dir -1), or all.
func (d *DiffState) expand(a int, dir int, all bool) {
	for _, g := range d.gapList {
		if g.a != a || g.hidden == 0 {
			continue
		}
		from, to := g.hideA, g.hideA+g.hidden-1
		n := d.expansionLines()
		switch {
		case all:
		case dir > 0:
			to = min(to, from+n-1)
		default:
			from = max(from, to-n+1)
		}
		d.revealed = mergeSpans(append(d.revealed, lineSpan{from, to}))
		d.expandV++
		return
	}
}

// revealedAt returns the last line of the lines revealed holding old line
// a, or -1.
func (d *DiffState) revealedAt(a int) int {
	for _, sp := range d.revealed {
		if a >= sp.from && a <= sp.to {
			return sp.to
		}
	}
	return -1
}

// revealedBefore returns the first line of the lines revealed holding old
// line a, or -1.
func (d *DiffState) revealedBefore(a int) int {
	for _, sp := range d.revealed {
		if a >= sp.from && a <= sp.to {
			return sp.from
		}
	}
	return -1
}

// selectLine selects a line by its number, or extends the selection to
// it from its nearer end.
func (d *DiffState) selectLine(side DiffSide, line int, extend bool) {
	switch {
	case extend && d.hasSel && d.selected.Side == side:
		if line < d.selected.Start {
			d.selected.Start = line
		} else {
			d.selected.End = line
		}
	case d.hasSel && d.selected.Side == side && d.selected.Start == line && d.selected.End == line:
		d.hasSel = false
	default:
		d.selected, d.hasSel = DiffLineRange{Side: side, Start: line, End: line}, true
	}
	if d.OnLineSelection != nil && d.hasSel {
		d.OnLineSelection(d.selected)
	}
}

// activeHunk returns the hunk under the pointer on either side, or else
// the one holding the new side's newest caret, or -1.
func (d *DiffState) activeHunk() int {
	for i := range d.eds {
		ed := &d.eds[i]
		if !ed.pointerIn || ed.hoverLine < 0 {
			continue
		}
		side := &d.sides[i]
		if i == 1 && side.unified {
			if j := d.model.hunkAtNew(ed.hoverLine); j >= 0 {
				return j
			}
			continue
		}
		if j := side.hunkOf(ed.hoverLine); j >= 0 {
			return j
		}
		if i == 1 {
			if j := d.model.hunkAtNew(ed.hoverLine); j >= 0 {
				return j
			}
		}
	}
	ed := &d.eds[1]
	if ed.focused && len(ed.sels) > 0 {
		line := ed.view.text.LineOf(ed.primary().head)
		return d.model.hunkAtNew(line)
	}
	return -1
}

// isDark reports whether a background is dark.
func isDark(c ui.Color) bool {
	l := 0.2126*float64(c.R) + 0.7152*float64(c.G) + 0.0722*float64(c.B)
	return l < 128
}

// diffGeom is what places the elements a diff view builds over its lines,
// as its separators, which the view builds from the last frame's layout:
// the blocks, the wraps of the lines, the height and the scroll.
type diffGeom struct {
	blocks         int
	wrapsA, wrapsB uint64
	total, scroll  float64
}

func (d *DiffState) geom() diffGeom {
	a := &d.eds[0].view
	if d.Layout == DiffUnified {
		a = &d.oldView
	}
	b := &d.eds[1]
	return diffGeom{d.blocksV, a.heights.wraps.version, b.view.heights.wraps.version, b.view.heights.total(), b.scrollY}
}

// rebuildIfMoved asks for the view to be built again when laying it out
// for painting moved what its elements were built for, as expanding
// lines from a separator's button does: else the separator stays until
// something else builds the view, as the pointer moving.
func (d *DiffState) rebuildIfMoved() {
	if d.geom() != d.built {
		d.eds[1].services.Invalidate()
	}
}
