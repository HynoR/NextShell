package appui

import "github.com/egoist/mygo/ui"

func (a *App) tabIDs() []string {
	ids := make([]string, 0, len(a.Tabs))
	for _, s := range a.Tabs {
		ids = append(ids, s.ID)
	}
	return ids
}
func (a *App) selectTabID(id string) {
	for i, s := range a.Tabs {
		if s.ID == id {
			a.activateTab(i)
			return
		}
	}
}
func (a *App) mruShortcuts(c *ui.Context) {
	ids := a.tabIDs()
	current := ""
	if s := a.current(); s != nil {
		current = s.ID
	}
	a.TabMRU.Sync(ids, current)
	if a.TabMRU.Active() && !c.WindowActive() {
		a.TabMRU.Cancel(ids)
		return
	}
	if a.TabMRU.Active() && (c.Shortcut(0, ui.KeyEscape) || c.Shortcut(ui.Ctrl, ui.KeyEscape)) {
		a.TabMRU.Cancel(ids)
		return
	}
	forward := c.Shortcut(ui.Ctrl, ui.KeyTab)
	reverse := c.Shortcut(ui.Ctrl|ui.Shift, ui.KeyTab)
	if a.TabMRU.Active() {
		forward = forward || c.Shortcut(ui.Ctrl, ui.KeyDown)
		reverse = reverse || c.Shortcut(ui.Ctrl, ui.KeyUp)
	}
	if forward || reverse {
		a.TabMRU.Step(ids, current, reverse)
	}
	if a.TabMRU.Active() && c.Modifiers()&ui.Ctrl == 0 {
		a.selectTabID(a.TabMRU.Commit())
	}
}
