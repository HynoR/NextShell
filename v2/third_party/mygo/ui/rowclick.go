package ui

// RowDoubleClicked reports, while a row of a List or a Table builds, that
// the row was double-clicked since the last frame with the pointer over the
// element: a cell sees its row's double click this way and leaves the click
// to the row, which chooses, where DoubleClicked would take it.
//
//	ui.Table(c, &app.list, cols, len(app.rows), func(row, col int) {
//		cell := ui.Box(c).FillWidth().Children(func() { ui.Text(c, app.rows[row][col]) })
//		if cell.RowDoubleClicked() {
//			app.edit(row, col)
//		}
//	})
func (e *node) RowDoubleClicked() bool {
	if !e.hasState() {
		return false
	}
	row := e.c.row
	return row != nil && row.double && e.Hovered()
}
