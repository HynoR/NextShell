package appui_test

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/appui"
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

// The command bar names its target and keeps a line for each tab: a command typed for one server is never
// sent to the next tab's, and it is still there when the tab comes back.
func TestCommandBarKeepsALinePerSessionAndNamesItsTarget(t *testing.T) {
	a, p, tt := fixture(t)
	session(t, p, tt, "A", "prod-web-01", remoteHost("a"))
	session(t, p, tt, "B", "db-master", remoteHost("b"))
	has(t, tt, "→ db-master")
	click(t, tt, "命令输入")
	tt.Type("systemctl restart nginx")
	click(t, tt, "prod-web-01")
	has(t, tt, "→ prod-web-01")
	if tt.HasText("→ db-master") {
		t.Fatal("the bar still names the previous tab")
	}
	inputIs(t, tt, "")
	click(t, tt, "命令输入")
	tt.Type("uptime")
	click(t, tt, "db-master")
	inputIs(t, tt, "systemctl restart nginx")
	click(t, tt, "命令输入")
	tt.Key(0, ui.KeyEnter)
	if len(p.sent) != 1 || p.sent[0] != "systemctl restart nginx" {
		t.Fatal("sent", p.sent)
	}
	inputIs(t, tt, "")
	click(t, tt, "prod-web-01")
	inputIs(t, tt, "uptime")
	// A reconnect keeps the line with its tab; closing the tab drops it.
	p.events = append(p.events, appui.Event{Session: &appui.Session{ID: "A2", Replaces: "A", Terminal: newTerminal(t, "$ "), Connection: remoteHost("a")}})
	tt.Frame()
	inputIs(t, tt, "uptime")
	click(t, tt, "db-master")
	click(t, tt, "关闭标签 prod-web-01")
	click(t, tt, "断开并关闭")
	session(t, p, tt, "C", "cache-01", remoteHost("c"))
	inputIs(t, tt, "")
	if len(a.Tabs) != 2 {
		t.Fatal(len(a.Tabs))
	}
}

// A multi-line snippet cannot go into the bar: clicking it says where to send it, and what was typed stays.
func TestMultiLineSnippetClickSaysWhereToSend(t *testing.T) {
	p := newPlatform(t)
	if e := p.SaveCommand(model.Command{ID: p.NewID(), Name: "部署", Group: "默认", Text: "cd /srv\n./deploy.sh"}); e != nil {
		t.Fatal(e)
	}
	_, tt := start(p)
	session(t, p, tt, "A", "prod", remoteHost("a"))
	click(t, tt, "命令输入")
	tt.Type("ls")
	click(t, tt, "命令库")
	click(t, tt, "片段 部署")
	tt.Frame()
	has(t, tt, "多行片段请点 ▶ 发送")
	inputIs(t, tt, "ls")
}

// A session that has ended shows it in the bar: Return reconnects instead of sending; a shortcut moves the
// keyboard into the bar and Esc gives it back to the terminal.
func TestCommandBarOnAnEndedSessionAndKeyboardHandOff(t *testing.T) {
	_, p, tt := fixture(t)
	s := session(t, p, tt, "x", "gone", remoteHost("gone"))
	tt.Key(appMod(), ui.KeyL)
	tt.Type("hi")
	inputIs(t, tt, "hi")
	tt.Key(0, ui.KeyEscape)
	tt.Type("there")
	inputIs(t, tt, "hi") // the terminal has the keys again
	s.Terminal.Close()
	<-s.Terminal.Done()
	tt.Frame()
	if got := barPlaceholder(tt); got != "连接已断开，回车重新连接" {
		t.Fatal("placeholder", got)
	}
	if e := tt.Click("发送"); e == nil && len(p.sent) != 0 {
		t.Fatal("sent to an ended session", p.sent)
	}
	click(t, tt, "命令输入")
	tt.Key(0, ui.KeyEnter)
	if p.reconnected != 1 || len(p.sent) != 0 {
		t.Fatal("Return on an ended session", p.reconnected, p.sent)
	}
	inputIs(t, tt, "hi") // the line waits for the new session
}

func barPlaceholder(tt *ui.Tester) string {
	for _, v := range tt.AccessAll() {
		if v.Name == "命令输入" && v.Role == ui.RoleTextField {
			return v.Placeholder
		}
	}
	return ""
}
