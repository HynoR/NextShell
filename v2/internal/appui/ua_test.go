package appui_test

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/model"
)

// Reconnecting replaces the session: a live one loses what runs in it, so the tab menu and the palette ask
// first. A session that already ended reconnects at once.
func TestReconnectOfALiveSessionAsksFirst(t *testing.T) {
	a, p, tt := fixture(t)
	s := session(t, p, tt, "x", "live", remoteHost("live"))
	tt.RightClick("live")
	tt.ChooseMenuItem("重新连接")
	has(t, tt, "重新连接 live？", "正在运行的命令会中断", "断开并重新连接")
	if p.reconnected != 0 {
		t.Fatal("reconnected a live session without asking")
	}
	click(t, tt, "取消")
	if p.reconnected != 0 || tt.HasText("断开并重新连接") {
		t.Fatal("cancel reconnected", p.reconnected)
	}
	// The palette's action asks the same.
	tt.Key(appMod(), ui.KeyK)
	tt.Type("重新连接")
	tt.Key(0, ui.KeyEnter)
	has(t, tt, "重新连接 live？")
	click(t, tt, "断开并重新连接")
	if p.reconnected != 1 {
		t.Fatal("confirmed reconnect", p.reconnected)
	}
	// Once the session has ended there is nothing to lose.
	s.Terminal.Close()
	<-s.Terminal.Done()
	tt.Frame()
	tt.RightClick("live")
	tt.ChooseMenuItem("重新连接")
	if p.reconnected != 2 || tt.HasText("断开并重新连接") {
		t.Fatal("an ended session asked", p.reconnected)
	}
	_ = a
}

// A local shell is asked about too; 不再询问 on closing connected tabs covers SSH tabs only.
func TestReconnectConfirmLocalAndPreference(t *testing.T) {
	p := newPlatform(t)
	prefs := p.Preferences()
	prefs.NoTabConfirm = true
	if e := p.SavePreferences(prefs); e != nil {
		t.Fatal(e)
	}
	_, tt := start(p)
	session(t, p, tt, "ssh", "web", remoteHost("web"))
	session(t, p, tt, "sh", "zsh", model.Connection{})
	tt.RightClick("web")
	tt.ChooseMenuItem("重新连接")
	if p.reconnected != 1 {
		t.Fatal("the preference not to ask was ignored", p.reconnected)
	}
	tt.RightClick("zsh")
	tt.ChooseMenuItem("重新连接")
	has(t, tt, "重新打开 zsh？", "终端里正在运行的程序会结束")
	if p.reconnected != 1 {
		t.Fatal("a local shell was replaced unasked")
	}
	click(t, tt, "结束并重新打开")
	if p.reconnected != 2 {
		t.Fatal(p.reconnected)
	}
}

// Closing tabs with unsaved files asks with 取消 as the default: Enter keeps the edits; with nothing at
// stake Enter still confirms.
func TestUnsavedCloseDefaultsToCancel(t *testing.T) {
	a, p, tt, _, _ := editorFixture(t)
	open(p, tt, "one", "/etc/app.conf", "a=1\n")
	tt.Type("x")
	click(t, tt, "关闭标签 one")
	has(t, tt, "这些文件的修改还没有保存", "不保存并关闭")
	tt.Key(0, ui.KeyEnter)
	if len(a.Tabs) != 1 || a.DirtyDocumentCount() != 1 || tt.HasText("不保存并关闭") {
		t.Fatal("Enter discarded the edits", len(a.Tabs), a.DirtyDocumentCount())
	}
	click(t, tt, "关闭标签 one")
	click(t, tt, "不保存并关闭")
	if len(a.Tabs) != 0 {
		t.Fatal("the button no longer closes", len(a.Tabs))
	}
	// No unsaved file: Enter on the default 断开并关闭 closes.
	session(t, p.fakePlatform, tt, "two", "two", remoteHost("two"))
	click(t, tt, "关闭标签 two")
	tt.Key(0, ui.KeyEnter)
	if len(a.Tabs) != 0 {
		t.Fatal("Enter no longer confirms a plain close", len(a.Tabs))
	}
}
