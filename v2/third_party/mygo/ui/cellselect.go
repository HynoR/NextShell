package ui

// A CellsStyle paints the block of cells a Table chooses (CellSelect),
// and the rows chosen, in its own colors rather than the accent, as a
// spreadsheet does: the chosen cell stands out over its row, and the rows
// alternate.
//
//	grid.CellsStyle = ui.CellsStyle{
//		Cell:     t.Accent,
//		CellText: t.AccentText,
//		Row:      t.Accent.Alpha(0.12),
//		Stripe:   t.Text.Alpha(0.04),
//	}
//
// A zero color paints none, leaving the theme's background, except where
// Cell is zero: the block then shows as chosen rows do, in the accent.
type CellsStyle struct {
	// Cell is the background of the cells of the block, and the border of
	// the cursor where it is out of the block.
	Cell Color
	// CellText is the color of their text and of the cursor's border in
	// the block, as AccentText is of chosen rows.
	CellText Color
	// Row is the background of the rows chosen.
	Row Color
	// Stripe is the background of every other row.
	Stripe Color
}

// ColumnRange is the columns of the block of cells a Table chooses
// (ListState.CellSelect), by column ID (TableColumn.ID, or Title).
type ColumnRange struct {
	// Cursor is the column the keys move from, where the border shows.
	Cursor string
	// Anchor is the column Shift extends the columns from.
	Anchor string
	// All chooses every column, as a click on a RowHeader cell does.
	All bool
}

// tableCol is a column of a Table choosing cells: its ID, its index in the
// columns given to Table, whether it holds row headers, and the state ID of
// its header's cell.
type tableCol struct {
	id        string
	col       int
	rowHeader bool
	head      uint64
}

// choosesCells reports whether the list holds the rows of a Table choosing
// blocks of cells (ListState.CellSelect).
func (f *listFrame) choosesCells() bool { return f.flat && f.s.choosesCells() }

// choosesCells reports whether a Table of the list chooses cells.
func (s *ListState) choosesCells() bool { return s.CellSelect && s.Selection != nil }

// chooseAllColumns makes the block every column, the rows as they are.
func (f *listFrame) chooseAllColumns() {
	c := f.s.Cells
	c.All = true
	f.setCells(c)
}

// cursorBorder draws the border of the cell at Cells.Cursor in color while
// table has the focus.
func cursorBorder(table *node, color Color) func(p *Painter, r Rect) {
	return func(p *Painter, r Rect) {
		if table.Focused() {
			p.Stroke(r, color, 0, 1)
		}
	}
}

// colIndex returns the place of the column whose ID is id among those
// shown, or -1.
func (s *ListState) colIndex(id string) int {
	for k, col := range s.tableCols {
		if col.id == id {
			return k
		}
	}
	return -1
}

// fixCells moves Cells.Cursor to the first column shown that is not a
// RowHeader column when it names none, and Cells.Anchor to the Cursor when
// it names none.
func (s *ListState) fixCells() {
	c := &s.Cells
	if k := s.colIndex(c.Cursor); k < 0 || s.tableCols[k].rowHeader {
		c.Cursor = ""
		if k := s.nextCol(-1, 1); k >= 0 {
			c.Cursor = s.tableCols[k].id
		}
	}
	if k := s.colIndex(c.Anchor); k < 0 || s.tableCols[k].rowHeader {
		c.Anchor = c.Cursor
	}
}

// nextCol returns the first column from place k on by d that is not a
// RowHeader column, or -1.
func (s *ListState) nextCol(k, d int) int {
	for k += d; k >= 0 && k < len(s.tableCols); k += d {
		if !s.tableCols[k].rowHeader {
			return k
		}
	}
	return -1
}

// block returns the places of the first and the last column of the block
// among those shown, hi below lo for none.
func (s *ListState) block() (lo, hi int) {
	if s.Cells.All {
		return 0, len(s.tableCols) - 1
	}
	a, c := s.colIndex(s.Cells.Anchor), s.colIndex(s.Cells.Cursor)
	if a < 0 || c < 0 {
		return 0, -1
	}
	return min(a, c), max(a, c)
}

// inBlock reports whether the column at place k is one of the block's.
func (s *ListState) inBlock(k int) bool {
	lo, hi := s.block()
	return k >= lo && k <= hi && !s.tableCols[k].rowHeader
}

// CellSelected reports whether the cell of row and col, as a Table's cell
// builder receives them, is chosen: its row is chosen and, in a table
// choosing cells (CellSelect), its column is in the block, or it is a
// RowHeader cell of a row chosen with every column. In a table that does
// not choose cells, every cell of a chosen row is. It is false before the
// list is first built. A cell builder can style what it holds with it:
//
//	ui.Table(c, &app.grid, cols, len(app.rows), func(row, col int) {
//		text := ui.Text(c, app.rows[row][col])
//		if app.grid.CellSelected(row, col) {
//			text.TextColor(c.Theme().AccentText)
//		}
//	})
func (s *ListState) CellSelected(row, col int) bool {
	f := &s.frame
	if s.cursor() == nil || row < 0 || row >= f.n || f.isHeader(row) || !f.chosen(row, f.key(row)) {
		return false
	}
	if !s.choosesCells() {
		return true
	}
	for k, tc := range s.tableCols {
		if tc.col == col {
			if tc.rowHeader {
				return s.Cells.All
			}
			return s.inBlock(k)
		}
	}
	return false
}

// SelectedColumns returns the IDs of the columns of the block of cells a
// Table chooses (CellSelect), in the order shown in the last frame,
// without RowHeader columns.
func (s *ListState) SelectedColumns() []string {
	lo, hi := s.block()
	var ids []string
	for k := lo; k <= hi; k++ {
		if col := s.tableCols[k]; !col.rowHeader {
			ids = append(ids, col.id)
		}
	}
	return ids
}

// SelectColumns chooses every row of a Table choosing cells (CellSelect)
// and the columns from the one whose ID is from to the one whose ID is
// to, which becomes Cells.Cursor. Call it in the view, after the table is
// built.
func (s *ListState) SelectColumns(from, to string) {
	f := &s.frame
	if f.owner == nil || !f.choosesCells() {
		return
	}
	f.chooseAll()
	f.setCells(ColumnRange{Cursor: to, Anchor: from})
}

// setCells makes c the columns of the block, reporting a new choice.
func (f *listFrame) setCells(c ColumnRange) {
	if f.s.Cells != c {
		f.s.Cells = c
		f.changed()
	}
}

// columnAt returns the place of the column at the window's point (x, y) in
// the last frame, as its header shows it, the nearest when the point is
// past them, or -1.
func (f *listFrame) columnAt(x, y float32) int {
	at, best := -1, float32(0)
	for k, col := range f.s.tableCols {
		st := f.c.rt.states[col.head]
		if st == nil {
			continue
		}
		lx, _ := st.local(x, y)
		d := max(-lx, lx-st.w, 0)
		if at < 0 || d < best {
			at, best = k, d
		}
	}
	return at
}

// clickCell sets the columns of the block for a click with mods on a row
// at the window's point (x, y): those of a RowHeader cell are all.
func (f *listFrame) clickCell(x, y float32, mods Modifiers) {
	s := f.s
	k := f.columnAt(x, y)
	if k < 0 {
		return
	}
	c, id := s.Cells, s.tableCols[k].id
	switch {
	case s.tableCols[k].rowHeader:
		c.All = true
	case mods&Shift != 0:
		c.Cursor, c.All = id, false
	case mods&Cmd != 0:
		return
	default:
		c = ColumnRange{Cursor: id, Anchor: id}
	}
	f.setCells(c)
}

// cellInBlockAt reports whether the cell at the window's point (x, y), in
// a row chosen, is in the block: a RowHeader cell, or none, counts as in it.
func (f *listFrame) cellInBlockAt(x, y float32) bool {
	k := f.columnAt(x, y)
	return k < 0 || f.s.tableCols[k].rowHeader || f.s.inBlock(k)
}

// cellKeys moves Cells.Cursor with Left and Right, extending the columns
// with Shift, as the screen shows them right to left too; without Shift
// they leave the cursor's cell alone. collapse leaves one column, as the
// keys moving the rows without Shift do, and all chooses every column, as
// Cmd+A does. In an OutlineTable, Left and Right stay the outline's, which
// open and close the rows.
func (f *listFrame) cellKeys(collapse, all bool) {
	s, o := f.s, f.owner
	d, extend := 0, false
	switch {
	case f.tree:
	case o.Shortcut(0, KeyLeft):
		d = -1
	case o.Shortcut(0, KeyRight):
		d = 1
	case o.Shortcut(Shift, KeyLeft):
		d, extend = -1, true
	case o.Shortcut(Shift, KeyRight):
		d, extend = 1, true
	}
	if o.rtl() {
		d = -d
	}
	c := s.Cells
	if d != 0 {
		if at := s.colIndex(c.Cursor); at >= 0 {
			if k := s.nextCol(at, d); k >= 0 {
				c.Cursor = s.tableCols[k].id
				f.revealColumn(k)
			}
		}
		collapse = collapse || !extend
		if cur := *s.cursor(); !extend && cur >= 0 && cur < f.n && !f.isHeader(cur) {
			f.choose(cur) // leave one cell, as the keys that move the rows do
		}
	}
	if collapse {
		c.Anchor, c.All = c.Cursor, false
	}
	if all {
		c.All = true
	}
	f.setCells(c)
}

// revealColumn scrolls the table sideways as little as shows the column at
// place k, as its header showed it in the last frame.
func (f *listFrame) revealColumn(k int) {
	h, st := f.c.rt.states[f.s.tableCols[k].head], f.e.st
	if h == nil || st == nil {
		return
	}
	var dx float32
	switch {
	case h.x < st.x:
		dx = h.x - st.x
	case h.x+h.w > st.x+st.w:
		dx = min(h.x+h.w-(st.x+st.w), h.x-st.x)
	}
	if dx != 0 && scrollBy(st, dx, 0) {
		f.c.rt.consumed = true
	}
}

// dragCells sets the columns of the block a drag over the cells chooses:
// from the column of the cell pressed to the one at the window's point
// (x, y).
func (f *listFrame) dragCells(x, y float32) {
	s := f.s
	from := s.colIndex(s.dragSel.fromCol)
	if from < 0 || s.tableCols[from].rowHeader {
		// None, or a drag from a RowHeader cell, which chooses whole rows
		// (fromCol is "", which an untitled RowHeader column's ID is too).
		return
	}
	to := f.columnAt(x, y)
	if to < 0 {
		return
	}
	// Over a RowHeader column, the nearest of the others toward the cell
	// pressed.
	if s.tableCols[to].rowHeader {
		d := 1
		if to > from {
			d = -1
		}
		to = s.nextCol(to, d)
	}
	f.setCells(ColumnRange{Cursor: s.tableCols[to].id, Anchor: s.dragSel.fromCol})
}

// startDragCells notes the column a drag over the cells extends from: that
// of the cell it started on, at the window's point (x, y), or with extend,
// as a Shift-press, Cells.Anchor. A RowHeader cell's chooses every column.
func (f *listFrame) startDragCells(x, y float32, extend bool) {
	s := f.s
	if extend && s.colIndex(s.Cells.Anchor) >= 0 {
		s.dragSel.fromCol = s.Cells.Anchor
		return
	}
	s.dragSel.fromCol = ""
	k := f.columnAt(x, y)
	if k < 0 {
		return
	}
	if s.tableCols[k].rowHeader {
		f.chooseAllColumns()
		return
	}
	s.dragSel.fromCol = s.tableCols[k].id
}
