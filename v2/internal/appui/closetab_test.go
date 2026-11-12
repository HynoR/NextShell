package appui_test

import (
	"runtime"
	"testing"

	"github.com/egoist/mygo/ui"
)

// The app menu has no Cmd+W item (cmd/nextshell/menu.go), so the key reaches the window: it closes the
// current tab, the last one too, and with no tab does nothing.
func TestCmdWClosesTheCurrentTab(t *testing.T) {
	a, p, tt := fixture(t)
	for _, id := range []string{"A", "B"} {
		session(t, p, tt, id, "tab-"+id, remoteHost(id))
	}
	mod := ui.Cmd
	if runtime.GOOS != "darwin" {
		mod = ui.Ctrl | ui.Shift
	}
	tt.Key(mod, ui.KeyW)
	click(t, tt, "断开并关闭") // a connected tab asks first
	if ids(a) != "A" {
		t.Fatal("closes the active tab", ids(a))
	}
	tt.Key(mod, ui.KeyW)
	click(t, tt, "断开并关闭")
	if ids(a) != "" || a.Active != -1 {
		t.Fatal("closes the last tab", ids(a), a.Active)
	}
	tt.Key(mod, ui.KeyW)
	if ids(a) != "" || len(p.closed) != 2 {
		t.Fatal("nothing to close", p.closed)
	}
}
