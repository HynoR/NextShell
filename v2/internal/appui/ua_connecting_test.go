package appui_test

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/model"
)

// Connecting shows at once: an unsaved host (quick connect) says so in a toast, and a further connection to a
// host that already has a live tab gets the 连接中 chip a first connection has, with its cancel button.
func TestConnectingIsShownForQuickConnectAndASecondConnection(t *testing.T) {
	p := newPlatform(t)
	host := saveHost(t, p, model.Connection{Name: "one", Host: "one.example", User: "u"})
	a, tt := start(p)
	click(t, tt, "快速连接")
	tt.Type("tester@localhost:2222")
	tt.Key(0, ui.KeyEnter)
	has(t, tt, "正在连接 tester@localhost:2222…")
	if len(p.connects) != 1 {
		t.Fatal("connects", p.connects)
	}
	session(t, p, tt, "A", "one", host)
	if tt.HasText("取消连接 one") {
		t.Fatal("a chip without a connection")
	}
	tt.Key(appMod(), ui.KeyK)
	tt.Type("one")
	tt.Key(0, ui.KeyDown) // the open tab comes first; the host below it connects again
	tt.Key(0, ui.KeyEnter)
	if len(p.connects) != 2 || p.connects[1].ID != host.ID {
		t.Fatal("palette connect", p.connects)
	}
	has(t, tt, "连接中 one", "取消连接 one")
	click(t, tt, "取消连接 one")
	if len(p.cancelled) != 1 || tt.HasText("取消连接 one") {
		t.Fatal("cancel", p.cancelled)
	}
	// The new session replaces the chip.
	tt.Key(appMod(), ui.KeyK)
	tt.Type("one")
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyEnter)
	has(t, tt, "取消连接 one")
	p.events = append(p.events, appui.Event{Session: &appui.Session{ID: "B", Title: "one", Terminal: newTerminal(t, "$ "), Connection: host}})
	tt.Frame()
	if tt.HasText("取消连接 one") || len(a.Tabs) != 2 {
		t.Fatal("the chip stayed", len(a.Tabs))
	}
}
