package appui_test

import (
	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/model"
	"testing"
)

func TestGUIRealTypingRename(t *testing.T) {
	a, p, tt := fixture(t)
	s := &appui.Session{ID: "typed", Title: "Before", Terminal: newTerminal(t, "hi"), Local: true}
	p.events = []appui.Event{{Session: s}}
	tt.Frame()
	click(t, tt, "标签名称")
	tt.Key(ui.Ctrl, ui.KeyA)
	for _, r := range "typed-name" {
		tt.TypeNative(string(r))
	}
	click(t, tt, "重命名")
	if s.Title != "typed-name" {
		t.Fatalf("name=%q input=%q", s.Title, a.TabName)
	}
}
func TestGUIPermanentDeleteFromTrash(t *testing.T) {
	a, p, tt := fixture(t)
	c := model.Connection{ID: "trash", Name: "trash", Host: "127.0.0.1", Port: 22, User: "test", Auth: "password", Encoding: "utf-8", Deleted: true}
	if e := p.SaveConnection(c, nil); e != nil {
		t.Fatal(e)
	}
	a.Data, _ = p.Snapshot()
	a.ShowTrash = true
	tt.Frame()
	click(t, tt, "永久删除")
	click(t, tt, "确认删除")
	snap, _ := p.Snapshot()
	if len(snap.Connections) != 0 {
		t.Fatalf("still present: %+v", snap.Connections)
	}
}
func TestGUIRotateCheckboxRealClick(t *testing.T) {
	a, _, tt := fixture(t)
	a.Page = "agent"
	tt.Frame()
	click(t, tt, "确认撤销旧 token / 会话并暂停 Agent")
	if !a.Phase3.ConfirmRotate {
		t.Fatal("checkbox did not check")
	}
	click(t, tt, "轮换 MCP token")
	if a.Phase3.ConfirmRotate || a.Phase3.Token == "" {
		t.Fatal("rotation/confirmation state failed")
	}
}

func (p *fakePlatform) TerminalTitle(id string) string {
	if term := p.terminals[id]; term != nil {
		return term.Title()
	}
	return p.Platform.TerminalTitle(id)
}
func TestGUITitleOSCManualPriorityAndReconnect(t *testing.T) {
	a, p, tt := fixture(t)
	s := &appui.Session{ID: "title", Title: "connection", Terminal: newTerminal(t, "hi")}
	p.events = []appui.Event{{Session: s}}
	tt.Frame()
	s.Terminal.Feed([]byte("\x1b]0;remote-title\x07"))
	tt.Frame()
	if s.Title != "remote-title" || !tt.HasText("● remote-title") {
		t.Fatal("OSC title not displayed", s.Title)
	}
	click(t, tt, "标签名称")
	tt.Key(ui.Ctrl, ui.KeyA)
	tt.TypeNative("manual-title")
	click(t, tt, "重命名")
	s.Terminal.Feed([]byte("\x1b]2;later-remote-title\x07"))
	tt.Frame()
	if s.Title != "manual-title" || !s.ManualTitle {
		t.Fatal("remote title replaced manual name")
	}
	next := &appui.Session{ID: "replacement", Replaces: s.ID, Title: "new", Terminal: newTerminal(t, "hello")}
	p.events = []appui.Event{{Session: next}}
	tt.Frame()
	if !next.ManualTitle || next.Title != "manual-title" {
		t.Fatal("manual name lost on reconnect")
	}
	_ = a
}
