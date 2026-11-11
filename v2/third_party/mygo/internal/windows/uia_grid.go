//go:build windows && (amd64 || arm64)

package windows

import (
	"syscall"

	"github.com/egoist/mygo/internal/platform"
)

// Tables, grids and tree grids have UI Automation's Grid and Table
// patterns, and their cells and headers GridItem and TableItem,
// as Chromium gives them: Narrator reads a cell's place, "row 4,121 of
// 10,000", from them. Places are counted from 0 among all the rows and
// columns, built or not: as the app gave them (aria-rowindex and the
// like, from 1), else as a list's rows say which of all they are
// (PositionInSet) after the header rows above them, else by the rows and
// cells built. A cell covers the rows and columns it spans (aria-rowspan,
// aria-colspan), the next in its row after them. They are found as a
// client asks, so frames pay nothing.

// cellOf reports whether nodes of a role are cells of a table, its
// column and row headers included.
func cellOf(r platform.AccessRole) bool {
	return r == platform.RoleCell || r == platform.RoleColumnHeader || r == platform.RoleRowHeader
}

// grid returns the table, grid or tree grid around the element.
func (e *uiaElement) grid() *uiaElement {
	for p := e.parent; p != nil && !p.root; p = p.parent {
		if p.n.Role.Tabular() {
			return p
		}
	}
	return nil
}

// row returns the row around a cell, if it is in one of its grid.
func (e *uiaElement) row() *uiaElement {
	for p := e.parent; p != nil && !p.root && !p.n.Role.Tabular(); p = p.parent {
		if p.n.Role == platform.RoleRow {
			return p
		}
	}
	return nil
}

// gridPlace is a row of a grid or a cell of a row, with its place: its
// row or its column, from 0.
type gridPlace struct {
	e  *uiaElement
	at int
}

// rows returns the rows of a grid in order with their places, leaving out
// those of grids inside it, and heads, the header rows above the first
// that says which of all it is.
func (e *uiaElement) rows() (rows []gridPlace, heads int) {
	listed := false
	stack := []*uiaElement{e}
	for len(stack) > 0 {
		p := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if p != e && p.n.Role.Tabular() {
			continue
		}
		if p == e || p.n.Role != platform.RoleRow {
			for i := len(p.children) - 1; i >= 0; i-- {
				stack = append(stack, p.children[i]) // in order, depth first
			}
			continue
		}
		at := len(rows)
		switch {
		case p.n.RowIndex > 0:
			at = p.n.RowIndex - 1
		case p.n.PosInSet > 0:
			at, listed = heads+p.n.PosInSet-1, true
		case !listed:
			heads++
		}
		rows = append(rows, gridPlace{p, at})
	}
	return rows, heads
}

// cells returns the cells of a row in order with their columns: a cell
// without one of its own is in the one after those the cell before it
// covers.
func (e *uiaElement) cells() []gridPlace {
	var cells []gridPlace
	col := 0
	stack := []*uiaElement{e}
	for len(stack) > 0 {
		p := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if cellOf(p.n.Role) {
			if p.n.ColIndex > 0 {
				col = p.n.ColIndex - 1
			}
			cells = append(cells, gridPlace{p, col})
			_, cols := p.span()
			col += cols
			continue
		}
		if p != e && (p.n.Role == platform.RoleRow || p.n.Role.Tabular()) {
			continue
		}
		for i := len(p.children) - 1; i >= 0; i-- {
			stack = append(stack, p.children[i])
		}
	}
	return cells
}

// span returns how many rows and columns a cell covers, 1 at least.
func (e *uiaElement) span() (rows, cols int) {
	return max(e.n.RowSpan, 1), max(e.n.ColSpan, 1)
}

// rowCount returns how many rows a grid has: as the app gave it, else as
// its list says (SizeOfSet) with the header rows, else those built.
func (e *uiaElement) rowCount() int {
	if e.n.RowCount > 0 {
		return e.n.RowCount
	}
	rows, heads := e.rows()
	n := 0
	if e.n.SetSize > 0 {
		n = heads + e.n.SetSize
	}
	for _, r := range rows {
		n = max(n, r.at+1)
	}
	return n
}

// colCount returns how many columns a grid has: as the app gave it, else
// those of its rows' cells.
func (e *uiaElement) colCount() int {
	if e.n.ColCount > 0 {
		return e.n.ColCount
	}
	n := 0
	rows, _ := e.rows()
	for _, r := range rows {
		for _, c := range r.e.cells() {
			_, cols := c.e.span()
			n = max(n, c.at+cols)
		}
	}
	return n
}

// place returns the row and the column of a cell in its grid.
func (e *uiaElement) place() (row, col int) {
	r, g := e.row(), e.grid()
	if r != nil && g != nil {
		rows, _ := g.rows()
		for _, p := range rows {
			if p.e == r {
				row = p.at
			}
		}
		for _, p := range r.cells() {
			if p.e == e {
				col = p.at
			}
		}
	} else if e.n.ColIndex > 0 {
		col = e.n.ColIndex - 1
	}
	if e.n.RowIndex > 0 {
		row = e.n.RowIndex - 1
	}
	return row, col
}

// linesOverlap reports whether lines [a, a+n) and [b, b+m) share one.
func linesOverlap(a, n, b, m int) bool { return a < b+m && b < a+n }

// gridCells calls f with each cell of a grid and the rows and columns it
// covers, from row and col.
func (e *uiaElement) gridCells(f func(c *uiaElement, row, col, rows, cols int)) {
	grid, _ := e.rows()
	for _, r := range grid {
		for _, c := range r.e.cells() {
			row := r.at
			if c.e.n.RowIndex > 0 {
				row = c.e.n.RowIndex - 1
			}
			rows, cols := c.e.span()
			f(c.e, row, c.at, rows, cols)
		}
	}
}

// cellAt returns the cell of a grid covering a row and a column, if
// built.
func (e *uiaElement) cellAt(row, col int) *uiaElement {
	var at *uiaElement
	e.gridCells(func(c *uiaElement, r, k, rows, cols int) {
		if at == nil && linesOverlap(r, rows, row, 1) && linesOverlap(k, cols, col, 1) {
			at = c
		}
	})
	return at
}

// columnHeaders returns the column headers of a grid, those over n
// columns from col alone when col is not negative.
func (e *uiaElement) columnHeaders(col, n int) []*uiaElement {
	var heads []*uiaElement
	e.gridCells(func(c *uiaElement, _, k, _, cols int) {
		if c.n.Role == platform.RoleColumnHeader && (col < 0 || linesOverlap(k, cols, col, n)) {
			heads = append(heads, c)
		}
	})
	return heads
}

// rowHeaders returns the row headers of a grid, those beside n rows from
// row alone when row is not negative.
func (e *uiaElement) rowHeaders(row, n int) []*uiaElement {
	var heads []*uiaElement
	e.gridCells(func(c *uiaElement, r, _, rows, _ int) {
		if c.n.Role == platform.RoleRowHeader && (row < 0 || linesOverlap(r, rows, row, n)) {
			heads = append(heads, c)
		}
	})
	return heads
}

// gridVtbls makes the vtables of the Grid, GridItem, Table and TableItem
// patterns, with vtbl adding IUnknown's methods and live finding the
// element of an interface still in the tree.
func gridVtbls(vtbl func(...uintptr) []uintptr, live func(this uintptr) (*uiaElement, uintptr)) {
	cb := syscall.NewCallback
	count := func(get func(e *uiaElement) int) uintptr {
		return cb(func(this, p uintptr) uintptr {
			e, hr := live(this)
			*(*int32)(native(p)) = 0
			if e == nil {
				return hr
			}
			*(*int32)(native(p)) = int32(get(e))
			return sOK
		})
	}
	uiaVtbls[ifaceGrid] = vtbl(
		cb(func(this, row, col, p uintptr) uintptr { // GetItem
			out(p, nil, 0)
			e, hr := live(this)
			if e == nil {
				return hr
			}
			r, c := int(int32(row)), int(int32(col))
			if r < 0 || c < 0 || r >= e.rowCount() || c >= e.colCount() {
				return eInvalidArg
			}
			// A cell not built, as in a row out of view, is none.
			return out(p, e.cellAt(r, c), ifaceSimple)
		}),
		count((*uiaElement).rowCount),
		count((*uiaElement).colCount),
	)
	uiaVtbls[ifaceGridItem] = vtbl(
		count(func(e *uiaElement) int { row, _ := e.place(); return row }),
		count(func(e *uiaElement) int { _, col := e.place(); return col }),
		count(func(e *uiaElement) int { rows, _ := e.span(); return rows }),
		count(func(e *uiaElement) int { _, cols := e.span(); return cols }),
		cb(func(this, p uintptr) uintptr { // get_ContainingGrid
			out(p, nil, 0)
			e, hr := live(this)
			if e == nil {
				return hr
			}
			return out(p, e.grid(), ifaceSimple)
		}),
	)
	headers := func(get func(e *uiaElement) []*uiaElement) uintptr {
		return cb(func(this, p uintptr) uintptr {
			e, hr := live(this)
			*(*uintptr)(native(p)) = 0
			if e == nil {
				return hr
			}
			*(*uintptr)(native(p)) = elementArray(get(e))
			return sOK
		})
	}
	uiaVtbls[ifaceTable] = vtbl(
		headers(func(e *uiaElement) []*uiaElement { return e.rowHeaders(-1, 0) }),
		headers(func(e *uiaElement) []*uiaElement { return e.columnHeaders(-1, 0) }),
		count(func(*uiaElement) int { return 0 }), // RowOrColumnMajor_RowMajor
	)
	uiaVtbls[ifaceTableItem] = vtbl(
		headers(func(e *uiaElement) []*uiaElement { // GetRowHeaderItems
			g := e.grid()
			if g == nil || e.n.Role == platform.RoleRowHeader {
				return nil
			}
			row, _ := e.place()
			rows, _ := e.span()
			return g.rowHeaders(row, rows)
		}),
		headers(func(e *uiaElement) []*uiaElement { // GetColumnHeaderItems
			g := e.grid()
			if g == nil || e.n.Role == platform.RoleColumnHeader {
				return nil
			}
			_, col := e.place()
			_, cols := e.span()
			return g.columnHeaders(col, cols)
		}),
	)
}
