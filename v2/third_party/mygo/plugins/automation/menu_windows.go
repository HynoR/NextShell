//go:build windows && (amd64 || arm64) && !mygo_noinspector

package automation

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/internal/windows"
)

// chooseMenuItem chooses an item of the window's menu bar, which shows
// the application menu, by the labels along its path. Main thread only.
func chooseMenuItem(w *mygo.Window, path []string) error {
	return windows.TestActivateMenuItem(w.NativeHandle(), path...)
}
