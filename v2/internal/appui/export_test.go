package appui

import "github.com/egoist/mygo/ui"

// SetOS makes the shortcut labels read as written for another system ("darwin", "linux", "windows"), for
// tests. The returned func puts the real one back.
func SetOS(goos string) (restore func()) {
	old := osName
	osName = goos
	return func() { osName = old }
}

// Glass says whether the main window shows a picture, and PaneColor and PageColor are what its bars and
// pages paint for the theme t: the plain colors without one, translucent ones with one.
func (a *App) Glass() bool                    { return a.glass() }
func (a *App) PaneColor(t *ui.Theme) ui.Color { return a.pane(t) }
func (a *App) PageColor(t *ui.Theme) ui.Color { return a.page(t) }
