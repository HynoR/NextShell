package editor

import (
	"fmt"
	"math"
	"sort"
	"strconv"

	"github.com/egoist/mygo/ui"
)

// diffSide shows a side of a diff in an editor.
type diffSide struct {
	d       *DiffState
	old     bool
	unified bool
	hide    []lineSpan
	blks    []block
}

func (s *diffSide) hidden() []lineSpan { return s.hide }

func (s *diffSide) blocks() []block { return s.blks }

// hunkOf returns the hunk holding line of the side, or -1.
func (s *diffSide) hunkOf(line int) int {
	hs := s.d.model.hunks
	if s.old {
		i := sort.Search(len(hs), func(i int) bool { return hs[i].A1 > line })
		if i < len(hs) && hs[i].A0 <= line {
			return i
		}
		return -1
	}
	i := sort.Search(len(hs), func(i int) bool { return hs[i].B1 > line })
	if i < len(hs) && hs[i].B0 <= line {
		return i
	}
	return -1
}

func (s *diffSide) colors(ed *editor) diffColors { return ed.theme.diff(isDark(ed.theme.Background)) }

func (s *diffSide) lineStyle(line int) lineLook {
	d := s.d
	var st lineLook
	ed := &d.eds[1]
	if s.old {
		ed = &d.eds[0]
	}
	if s.hunkOf(line) >= 0 {
		dc := s.colors(ed)
		if s.old {
			st.bg, st.gutter, st.number = dc.deletedLine, dc.deletedGutter, dc.deleted
			st.bar, st.stripes, st.sign = dc.deleted, true, "-"
		} else {
			st.bg, st.gutter, st.number = dc.addedLine, dc.addedGutter, dc.added
			st.bar, st.sign = dc.added, "+"
		}
		if d.NoBackground {
			st.bg, st.gutter = ui.Color{}, ui.Color{}
		}
		if d.Indicators != DiffBars {
			st.bar = ui.Color{}
		}
	}
	side := DiffNew
	if s.old {
		side = DiffOld
	}
	if d.hasSel && d.selected.Side == side && line >= d.selected.Start && line <= d.selected.End {
		m := ed.theme.Modified
		bg := st.bg
		if bg.A == 0 {
			bg = ed.theme.Background
		}
		st.bg = mixLab(bg, m, 0.18)
		g := st.gutter
		if g.A == 0 {
			g = ed.theme.Background
		}
		st.gutter = mixLab(g, m, 0.3)
		st.number = m
	}
	return st
}

func (s *diffSide) words(line int) []diffRange {
	d := s.d
	if d.Inline == DiffInlineNone {
		return nil
	}
	i := s.hunkOf(line)
	if i < 0 {
		return nil
	}
	w := d.model.wordsOf(i, d.Inline)
	if s.old {
		return w.oldWords[line]
	}
	return w.newWords[line]
}

func (s *diffSide) number(line int) int {
	if pt := s.d.partial; pt != nil {
		return pt.fileLine(s.old || false, line) + 1
	}
	return line + 1
}

// paintBlock paints a block in r, whose top is at top in the content: a
// filler of rows a side lacks, hatched, or the deleted lines of a unified
// view. Separators leave their room to the elements over it
// (buildSeparators).
func (s *diffSide) paintBlock(p *ui.Painter, ed *editor, b *block, r ui.Rect, top float64) {
	ct := ed.theme
	switch b.kind {
	case blockSpacer:
		if b.from == 0 {
			return
		}
		// A filler: hatched at -45°, every 8 DIPs from its bottom-left
		// corner, as Pierre's. The corner is in float64: r's float32 is
		// a DIP off, or more, millions of DIPs down a file, which would
		// shake the stripes as the view scrolls.
		text := ui.Rect{X: r.X + ed.gutterW, Y: r.Y, W: r.W - ed.gutterW, H: r.H}
		corner := [2]float64{float64(text.X), float64(ed.r.Y) + top + float64(b.height) - ed.scrollY}
		c := ct.Text.Alpha(0.07)
		p.Clip(text, 0, func() {
			fillerStripes(p.Visible(), corner, func(from, to [2]float32) {
				p.StrokePolyline([][2]float32{from, to}, 1, c)
			})
		})
	case blockDeleted:
		s.paintDeleted(p, ed, b, r)
	}
}

// fillerStripes calls fn with the stripes of a filler whose bottom-left
// corner is at corner, from a at v's bottom to b at its top, v being what
// shows of it: lines at -45°, up to the right, every 8 DIPs along its
// bottom from the corner. They are those that cross v alone, and their
// pieces from v's bottom to its top, all of a size so that they share a
// mask without a GPU: a filler across a whole file added is millions of
// DIPs high. Stroke each alone: on a GPU, a translucent line of one
// segment takes no mask. That one would be as wide and as high as v is
// high, rasterized again as its height changes, and drawn that large for
// each stripe.
func fillerStripes(v ui.Rect, corner [2]float64, fn func(a, b [2]float32)) {
	if v.W <= 0 || v.H <= 0 {
		return
	}
	// A stripe is where x+y is the corner's, give or take 8s: s, crossing
	// v when s-y1 < x1 and s-y0 > x0.
	x0, y0 := float64(v.X), float64(v.Y)
	x1, y1 := x0+float64(v.W), y0+float64(v.H)
	phase := corner[0] + corner[1]
	lo, hi := x0+y0, x1+y1
	for s := phase + 8*(math.Floor((lo-phase)/8)+1); s < hi; s += 8 {
		fn([2]float32{float32(s - y1), float32(y1)}, [2]float32{float32(s - y0), float32(y0)})
	}
}

// paintDeleted paints the lines a unified view shows deleted: the old
// text's, tinted, with their numbers in the gutter.
func (s *diffSide) paintDeleted(p *ui.Painter, ed *editor, b *block, r ui.Rect) {
	d := s.d
	ov := &d.oldView
	ov.setTheme(ed.theme)
	ov.advanceSyntax(b.to, 0)
	dc := s.colors(ed)
	lh := ed.view.lineHeight
	ox := r.X + ed.textX - ed.scrollX
	text := ui.Rect{X: r.X + ed.gutterW, Y: r.Y, W: r.W - ed.gutterW, H: r.H}
	var cc runColors
	var rects []ui.Rect
	words := d.model.wordsOf(b.hunk, d.Inline)
	// Each line's top, and the lines in the clip.
	type placed struct {
		line int
		y, h float32
		l    *lineLayout
	}
	var lines []placed
	y := r.Y
	for line := b.from; line < b.to && y <= p.Visible().Y+p.Visible().H; line++ {
		l := ov.line(line)
		h := float32(len(l.rows)) * lh
		if y+h >= p.Visible().Y {
			lines = append(lines, placed{line, y, h, l})
		}
		y += h + d.annotationHeight(DiffOld, line)
	}
	for _, pl := range lines {
		if !d.NoBackground {
			p.Fill(ui.Rect{X: text.X, Y: pl.y, W: text.W, H: pl.h}, dc.deletedLine, 0)
			p.Fill(ui.Rect{X: r.X, Y: pl.y, W: ed.gutterW, H: pl.h}, dc.deletedGutter, 0)
		}
		if !d.NoLineNumbers {
			n := pl.line + 1
			if pt := d.partial; pt != nil {
				n = pt.fileLine(true, pl.line) + 1
			}
			num := strconv.Itoa(n)
			lay := ed.view.shaped.get(num, ed.view.style)
			nr := rowLayout{pieces: []rowPiece{{to: len(num), l: lay, ascii: true, width: lay.Width}}, to: len(num)}
			var nc runColors
			col := dc.deleted
			ed.view.paintRow(p, &lineLayout{disp: num}, &nr, r.X+ed.gutterW-4-ed.view.charWidth*0.5-lay.Width, pl.y, &nc, func(int, ui.Color) ui.Color { return col })
		}
		if d.Indicators == DiffBars {
			ed.paintBar(p, ui.Rect{X: r.X, Y: pl.y, W: 4, H: pl.h}, lineLook{bar: dc.deleted, stripes: true})
		}
	}
	p.Clip(text, 0, func() {
		for _, pl := range lines {
			l := pl.l
			ov.colorsOf(l, &cc)
			if d.Indicators == DiffClassic {
				s.paintSign(p, ed, "-", r.X+ed.textX-ed.view.charWidth*1.5, pl.y, dc.deleted)
			}
			for ri := range l.rows {
				row := &l.rows[ri]
				ry := pl.y + float32(ri)*lh
				// Only the rows in view: a long line wraps into
				// thousands, each shaped and painted otherwise.
				if ry > p.Visible().Y+p.Visible().H || ry+lh < p.Visible().Y {
					continue
				}
				if d.Inline != DiffInlineNone {
					for _, w := range words.oldWords[pl.line] {
						rects = mergeRects(ov.rangeRects(l, row, l.toDisp(w.start, false), l.toDisp(w.end, false), rects[:0]))
						for _, rc := range rects {
							p.Fill(ui.Rect{X: ox + rc.X, Y: ry, W: rc.W, H: lh}, dc.deletedWord, 3)
						}
					}
				}
				ov.paintRow(p, l, row, ox, ry, &cc, nil)
			}
		}
	})
}

// paintSign paints a classic + or - before a line.
func (s *diffSide) paintSign(p *ui.Painter, ed *editor, sign string, x, y float32, c ui.Color) {
	v := &ed.view
	lay := v.shaped.get(sign, v.style)
	row := rowLayout{pieces: []rowPiece{{to: 1, l: lay, ascii: true, width: lay.Width}}, to: 1}
	var cc runColors
	v.paintRow(p, &lineLayout{disp: sign}, &row, x, y, &cc, func(int, ui.Color) ui.Color { return c })
}

// blockInput takes presses on the numbers of lines, which select them,
// and on the deleted lines of a unified view. Separators and buttons are
// elements of their own (buildSeparators, buildBlocks).
func (s *diffSide) blockInput(ed *editor, b *block, ev ui.InputEvent, r ui.Rect) bool {
	d := s.d
	side := DiffNew
	if s.old {
		side = DiffOld
	}
	if b != nil {
		switch b.kind {
		case blockDeleted:
			// Lines of the old text: their numbers select them.
			if ev.X < ed.gutterW && d.LineSelection && ev.Kind == ui.InputPointerDown {
				// The line by the heights of its rows and annotations.
				lh := ed.view.lineHeight
				y, line := r.Y, b.to-1
				for l := b.from; l < b.to; l++ {
					y += float32(d.oldView.heights.wraps.rows(l))*lh + d.annotationHeight(DiffOld, l)
					if ev.Y < y {
						line = l
						break
					}
				}
				d.selectLine(DiffOld, line, ev.Mods&ui.Shift != 0)
				return true
			}
			return true
		}
		return false
	}
	// A press on the gutter of a line, in r.
	if ev.Kind != ui.InputPointerDown {
		return false
	}
	_, cy := ed.toContent(ev.X, ev.Y)
	line := ed.view.heights.lineAt(cy)
	if d.LineSelection {
		d.selectLine(side, line, ev.Mods&ui.Shift != 0)
		return true
	}
	return false
}

// buildBlocks builds what blocks hold of elements in view: annotations,
// the actions of hunks and the gutter's button.
func (s *diffSide) buildBlocks(c *ui.Context, ed *editor) {
	d := s.d
	v := &ed.view
	h := &v.heights
	if ed.r.W <= 0 {
		return
	}
	y0, y1 := ed.buildRange()
	first, last := h.lineAt(y0), h.lineAt(y1)
	side := DiffNew
	if s.old {
		side = DiffOld
	}
	// Annotations under their lines, after one another on a line.
	if d.RenderAnnotation != nil && len(d.Annotations) > 0 {
		stacked := map[[2]int]float64{} // by where they go: 0 a line, 1 a deleted line
		for _, a := range d.Annotations {
			var top float64
			var at [2]int
			line := a.Line
			if s.unified && a.Side == DiffOld {
				if nl, ok := d.model.newLineOf(a.Line); ok {
					line = nl
				} else {
					line = -1
				}
			}
			switch {
			case a.Side == side || s.unified && a.Side == DiffOld && line >= 0:
				if line < first || line > last {
					continue
				}
				g := h.geometry(line)
				if g.hidden {
					continue
				}
				top = g.below
				// After the blocks below it that come first.
				_, below := h.blocksOf(line)
				for _, b := range below {
					if b.kind == blockAnnotation {
						break
					}
					top += float64(b.height)
				}
				at = [2]int{0, line}
			case s.unified && a.Side == DiffOld:
				// In the block of deleted lines it is in.
				found := false
				for line := first; line <= last && !found; line++ {
					above, below := h.blocksOf(line)
					g := h.geometry(line)
					for _, blks := range [][]block{above, below} {
						bt := g.top
						if len(blks) > 0 && blks[0].below {
							bt = g.below
						}
						for _, b := range blks {
							if b.kind == blockDeleted && a.Line >= b.from && a.Line < b.to {
								off := bt
								for l := b.from; l <= a.Line; l++ {
									off += float64(float32(d.oldView.heights.wraps.rows(l)) * v.lineHeight)
									if l < a.Line {
										off += float64(d.annotationHeight(DiffOld, l))
									}
								}
								top, found = off, true
								break
							}
							bt += float64(b.height)
						}
						if found {
							break
						}
					}
				}
				if !found {
					continue
				}
				at = [2]int{1, a.Line}
			default:
				continue
			}
			top += stacked[at]
			n := stacked[at]
			stacked[at] += float64(a.Height)
			a := a
			box := ui.Box(c).Key(fmt.Sprint("annotation ", a.Side, " ", a.Line, " ", a.Key, " ", n)).Absolute().
				Left(ed.gutterW).Top(float32(top - ed.scrollY)).Width(ed.r.W - ed.gutterW).Height(a.Height).HandleInput(shield)
			box.Children(func() { d.RenderAnnotation(c, a) })
		}
	}
	// Revert and Keep on the hunk under the pointer, or the caret's.
	if d.HunkActions && !s.old {
		active := d.activeHunk()
		for i, hk := range d.model.hunks {
			if i != active {
				continue
			}
			line := hk.B0
			if line > last+1 || hk.B1 < first && hk.B0 < first {
				continue
			}
			var top float64
			if s.unified && hk.A1 > hk.A0 {
				if line >= v.text.LineCount() {
					line = v.text.LineCount() - 1
					top = h.geometry(line).below
				} else {
					top = h.geometry(line).top
					above, _ := h.blocksOf(line)
					for _, b := range above {
						if b.kind == blockDeleted {
							break
						}
						top += float64(b.height)
					}
				}
			} else {
				line = min(line, v.text.LineCount()-1)
				top = h.geometry(line).rows
			}
			if top < y0-float64(v.lineHeight) || top > y1 {
				continue
			}
			bar := ui.Row(c).Key(fmt.Sprint("hunk actions ", i)).Absolute().Right(ed.trackWidth() + 6).Top(float32(top-ed.scrollY) + 2).
				Cursor(ui.CursorDefault).KeepFocus().HandleInput(shield)
			render := d.RenderHunkActions
			if render == nil {
				render = DefaultHunkActions
			}
			render(c, bar, &DiffHunkActions{Index: i, Hunk: DiffHunk{hk.A0, hk.A1, hk.B0, hk.B1}, d: d})
		}
	}
	// The gutter's button, on the line under the pointer.
	if d.OnGutterButton != nil && ed.pointerIn && ed.hoverLine >= 0 && ed.hoverLine < v.text.LineCount() && ed.pointer.X < ed.gutterW+40 {
		g := h.geometry(ed.hoverLine)
		if !g.hidden {
			line := DiffLineRange{Side: side, Start: ed.hoverLine, End: ed.hoverLine}
			ui.Box(c).Key("gutter button").Absolute().Left(2).Top(float32(g.rows-ed.scrollY) + 2).HandleInput(shield).Children(func() {
				btn := ui.ButtonBase(c).Size(16, v.lineHeight-4).Cursor(ui.CursorPointer).KeepFocus()
				render := d.RenderGutterButton
				if render == nil {
					render = DefaultGutterButton
				}
				render(c, btn, line)
				if btn.Clicked() {
					d.OnGutterButton(line)
				}
			})
		}
	}
}
