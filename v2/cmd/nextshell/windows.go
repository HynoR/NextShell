package main

import (
	"runtime"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

var afterFrame = func(f func()) { go mygo.RunOnMain(f) } // a variable so a test can run it inline

// closeOnCmdW wraps the view of a secondary window (settings, editor) so that Cmd+W closes it. The app menu
// has no Close Window item, as a menu key would shadow the main window's Cmd+W, which closes a tab.
// close runs after the frame, as the editor's own HideEditor does.
func closeOnCmdW(view func(*ui.Context), close func()) func(*ui.Context) {
	return func(c *ui.Context) {
		if c.Shortcut(ui.Cmd, ui.KeyW) {
			afterFrame(close)
		}
		view(c)
	}
}

// bringForward makes the app active with w in front, for a quit confirmation that is drawn in w: from the Dock
// or another app the window is behind and unredrawn until activated, so the request would look ignored.
// macOS 14+ refuses an activation the user did not start, so then the Dock icon bounces until clicked.
func bringForward(w *mygo.Window) {
	if w.IsMinimized() {
		w.Restore()
	}
	mygo.App.Focus()
	w.Show()
	w.Focus()
	if runtime.GOOS == "darwin" && !mygo.App.IsActive() {
		go mygo.App.Dock.Bounce(true)
	}
}
