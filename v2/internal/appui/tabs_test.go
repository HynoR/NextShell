package appui_test

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/model"
)

func remoteHost(name string) model.Connection {
	return model.Connection{ID: "id-" + name, Name: name, Host: name + ".example", User: "u", Port: 22, Monitor: true}
}

func TestTabRenameTitlesAndReconnect(t *testing.T) {
	a, p, tt := fixture(t)
	s := session(t, p, tt, "one", "connection", remoteHost("one"))
	s.Terminal.Feed([]byte("\x1b]0;remote-title\x07"))
	tt.Frame()
	if s.Title != "connection" {
		t.Fatal("an SSH tab shows the connection name, not the program title", s.Title)
	}
	has(t, tt, "remote-title") // the program title is on the session card
	dblclick(t, tt, "connection")
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("manual")
	tt.Key(0, ui.KeyEnter)
	if s.Title != "manual" || !s.ManualTitle {
		t.Fatal("inline rename", s.Title)
	}
	s.Terminal.Feed([]byte("\x1b]2;later\x07"))
	tt.Frame()
	if s.Title != "manual" {
		t.Fatal("program title replaced the user's name")
	}
	// Escape cancels an inline rename.
	if e := tt.RightClick("manual"); e != nil {
		t.Fatal(e)
	}
	if e := tt.ChooseMenuItem("重命名"); e != nil {
		t.Fatal(e)
	}
	tt.Type("discarded")
	tt.Key(0, ui.KeyEscape)
	if s.Title != "manual" {
		t.Fatal("escape kept the rename", s.Title)
	}
	next := &appui.Session{ID: "two", Replaces: "one", Title: "fresh", Terminal: newTerminal(t, ""), Connection: s.Connection}
	p.events = append(p.events, appui.Event{Session: next})
	tt.Frame()
	if len(a.Tabs) != 1 || a.Tabs[0] != next || next.Title != "manual" || !next.ManualTitle {
		t.Fatal("reconnect replaced the tab wrongly")
	}
	tt.RightClick("manual")
	if e := tt.ChooseMenuItem("恢复自动标题"); e != nil {
		t.Fatal(e)
	}
	if next.ManualTitle || next.Title != "one" {
		t.Fatal("automatic title", next.Title)
	}
}

// Local terminals keep following the program's title (while the preference allows it); SSH tabs never do.
func TestLocalTabFollowsTheProgramTitleAndSSHTabDoesNot(t *testing.T) {
	_, p, tt := fixture(t)
	local := session(t, p, tt, "L", "本地终端", model.Connection{})
	remote := session(t, p, tt, "R", "prod-db", remoteHost("prod-db"))
	for _, s := range []*appui.Session{local, remote} {
		s.Terminal.Feed([]byte("\x1b]0;vim notes.txt\x07"))
	}
	tt.Frame()
	if local.Title != "vim notes.txt" || remote.Title != "prod-db" {
		t.Fatal("titles", local.Title, remote.Title)
	}
}
func TestLocalTabKeepsItsNameWhenProgramsMayNotRetitle(t *testing.T) {
	p := newPlatform(t)
	prefs := p.Preferences()
	prefs.TitleUpdates = false
	if e := p.SavePreferences(prefs); e != nil {
		t.Fatal(e)
	}
	_, tt := start(p)
	local := session(t, p, tt, "L", "本地终端", model.Connection{})
	local.Terminal.Feed([]byte("\x1b]0;vim notes.txt\x07"))
	tt.Frame()
	if local.Title != "本地终端" {
		t.Fatal(local.Title)
	}
}

func TestTabMenuCloseVariantsAndDuplicate(t *testing.T) {
	a, p, tt := fixture(t)
	for _, id := range []string{"A", "B", "C", "D"} {
		session(t, p, tt, id, "tab-"+id, remoteHost(id))
	}
	tt.RightClick("tab-B")
	tt.ChooseMenuItem("复制会话")
	tt.RightClick("tab-B")
	tt.ChooseMenuItem("重新连接")
	if p.cloned != 1 || p.reconnected != 1 {
		t.Fatal("duplicate / reconnect")
	}
	tt.RightClick("tab-B")
	tt.ChooseMenuItem("关闭右侧")
	has(t, tt, "关闭 2 个标签？", "其中 2 个连接还在使用")
	click(t, tt, "断开并关闭")
	if ids(a) != "AB" {
		t.Fatal("close to the right", ids(a))
	}
	tt.RightClick("tab-B")
	tt.ChooseMenuItem("关闭其他")
	click(t, tt, "断开并关闭")
	if ids(a) != "B" || a.Active != 0 {
		t.Fatal("close others", ids(a), a.Active)
	}
	click(t, tt, "关闭标签 tab-B")
	click(t, tt, "断开并关闭")
	if len(a.Tabs) != 0 || a.Active != -1 || len(p.closed) != 4 {
		t.Fatal("close", p.closed)
	}
	has(t, tt, "开始使用", "从 NextShell 1.x 导入连接文件")
}
func ids(a *appui.App) string {
	s := ""
	for _, t := range a.Tabs {
		s += t.ID
	}
	return s
}

func TestTabDragReorderAndShortcuts(t *testing.T) {
	a, p, tt := fixture(t)
	for _, id := range []string{"A", "B", "C"} {
		session(t, p, tt, id, "tab-"+id, remoteHost(id))
	}
	from, _ := tt.Find("tab-A")
	to, _ := tt.Find("tab-C")
	x, y := from.X+from.W/2, from.Y+from.H/2
	tx, ty := to.X+to.W/2, to.Y+to.H/2
	tt.Press(x, y)
	tt.Move(x+12, y)
	tt.Move(tx, ty)
	tt.Release(tx, ty)
	tt.Frame()
	if ids(a) != "BCA" || a.Tabs[a.Active].ID != "C" {
		t.Fatal("drag", ids(a), a.Active)
	}
	tt.Key(ui.Cmd, ui.Key1)
	if a.Active != 0 {
		t.Fatal("Cmd+1")
	}
	tt.Key(ui.Cmd, ui.Key9)
	if a.Active != 2 {
		t.Fatal("Cmd+9 picks the last tab")
	}
	tt.Key(ui.Ctrl, ui.KeyPageDown)
	if a.Active != 0 {
		t.Fatal("next wraps")
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyBracketLeft)
	if a.Active != 2 {
		t.Fatal("previous wraps")
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyD)
	if p.cloned != 1 {
		t.Fatal("duplicate shortcut")
	}
}

func TestMRUKeysHeldReverseReleaseAndEscape(t *testing.T) {
	a, p, tt := fixture(t)
	for _, id := range []string{"a", "b", "c"} {
		session(t, p, tt, id, "tab-"+id, remoteHost(id))
	}
	tt.Key(ui.Ctrl, ui.KeyTab)
	has(t, tt, "最近使用 · 松开 Ctrl 切换 · Esc 取消")
	tt.Key(ui.Ctrl, ui.KeyTab)
	tt.Key(ui.Ctrl|ui.Shift, ui.KeyTab)
	if a.Tabs[a.Active].ID != "c" {
		t.Fatal("switched before release")
	}
	tt.CodeUp(0, ui.CodeControlLeft)
	tt.Frame()
	if a.Tabs[a.Active].ID != "b" {
		t.Fatal("release did not commit", a.Active)
	}
	tt.Key(ui.Ctrl, ui.KeyTab)
	tt.Key(0, ui.KeyEscape)
	tt.CodeUp(0, ui.CodeControlLeft)
	tt.Frame()
	if a.Tabs[a.Active].ID != "b" {
		t.Fatal("escape did not cancel")
	}
}

func TestDisconnectedTabOffersReconnect(t *testing.T) {
	a, p, tt := fixture(t)
	s := session(t, p, tt, "x", "gone", remoteHost("gone"))
	s.Terminal.Close()
	<-s.Terminal.Done()
	tt.Frame()
	has(t, tt, "已断开", "状态 已断开", "连接已断开 · 按回车重新连接", "u@gone.example:22")
	click(t, tt, "重新连接")
	if p.reconnected != 1 || len(a.Tabs) != 1 {
		t.Fatal("reconnect")
	}
	// Until the new session arrives the card and the tab say so; a failure turns them red.
	has(t, tt, "连接中", "状态 连接中")
	if tt.HasText("重新连接 gone") {
		t.Fatal("reconnect offered while connecting")
	}
	p.events = append(p.events, appui.Event{Failed: s.Connection.ID, Message: "connection refused"})
	tt.Frame()
	has(t, tt, "失败", "状态 失败")
	click(t, tt, "重新连接 gone") // the tab's own button
	if p.reconnected != 2 {
		t.Fatal("tab reconnect", p.reconnected)
	}
	next := &appui.Session{ID: "y", Replaces: "x", Title: "gone", Terminal: newTerminal(t, "$ "), Connection: s.Connection}
	p.events = append(p.events, appui.Event{Session: next})
	tt.Frame()
	has(t, tt, "已连接", "状态 已连接")
	// The address copies with a click.
	click(t, tt, "复制地址 u@gone.example:22")
	has(t, tt, "已复制")
	if tt.Clipboard() != "u@gone.example:22" {
		t.Fatal("copy", tt.Clipboard())
	}
}
