package appui_test

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/model"
)

// Closing a connected SSH tab asks first, whichever way it is closed; 不再询问 is kept as a preference.
// Local shells, disconnected tabs and the agent's own closes never ask.
func TestClosingAConnectedTabAsksFirst(t *testing.T) {
	a, p, tt := fixture(t)
	session(t, p, tt, "A", "tab-A", remoteHost("A"))
	local := session(t, p, tt, "L", "本地终端", model.Connection{})
	gone := session(t, p, tt, "G", "tab-G", remoteHost("G"))
	gone.Terminal.Close()
	<-gone.Terminal.Done()
	tt.Frame()
	// Local and disconnected tabs close at once.
	click(t, tt, "关闭标签 本地终端")
	click(t, tt, "关闭标签 tab-G")
	if ids(a) != "A" || tt.HasText("断开并关闭") {
		t.Fatal("local and disconnected tabs ask", ids(a))
	}
	_ = local
	// The × asks; 取消 keeps the tab.
	click(t, tt, "关闭标签 tab-A")
	has(t, tt, "关闭 tab-A？", "这个连接还在使用。关闭后，正在运行的命令会中断。", "不再询问")
	click(t, tt, "取消")
	if ids(a) != "A" || len(p.closed) != 2 {
		t.Fatal("cancel closed the tab", ids(a), p.closed)
	}
	// The palette's 关闭标签 asks the same.
	tt.Key(appMod(), ui.KeyK)
	tt.Type("关闭标签")
	tt.Key(0, ui.KeyEnter)
	has(t, tt, "关闭 tab-A？")
	click(t, tt, "取消")
	// So does a middle click, which closes on release.
	tab, _ := tt.Find("tab-A")
	x, y := tab.X+tab.W/2, tab.Y+tab.H/2
	tt.PressButton(2, x, y)
	tt.ReleaseButton(2, x, y)
	has(t, tt, "关闭 tab-A？")
	if ids(a) != "A" {
		t.Fatal("closed without an answer")
	}
	// 不再询问 closes and is remembered: the next connected tab closes at once.
	click(t, tt, "不再询问")
	click(t, tt, "断开并关闭")
	if ids(a) != "" || !p.Preferences().NoTabConfirm {
		t.Fatal("not closed or not remembered", ids(a), p.Preferences().NoTabConfirm)
	}
	session(t, p, tt, "B", "tab-B", remoteHost("B"))
	click(t, tt, "关闭标签 tab-B")
	if ids(a) != "" || tt.HasText("断开并关闭") {
		t.Fatal("asked again after 不再询问", ids(a))
	}
}

// The agent closing a session is not the user closing a tab: no question, as before.
func TestAgentClosesAConnectedTabWithoutAsking(t *testing.T) {
	a, p, tt := fixture(t)
	session(t, p, tt, "A", "tab-A", remoteHost("A"))
	p.events = append(p.events, appui.Event{CloseID: "A"})
	tt.Frame()
	if ids(a) != "" || tt.HasText("断开并关闭") {
		t.Fatal("the agent's close asked", ids(a))
	}
}

func TestMiddleClickClosesALocalTabAtOnce(t *testing.T) {
	a, p, tt := fixture(t)
	session(t, p, tt, "L", "shell", model.Connection{})
	tab, _ := tt.Find("shell")
	x, y := tab.X+tab.W/2, tab.Y+tab.H/2
	tt.PressButton(2, x, y)
	if ids(a) != "L" {
		t.Fatal("closed on press")
	}
	tt.ReleaseButton(2, x, y)
	if ids(a) != "" {
		t.Fatal("middle click did not close", ids(a))
	}
}

// Two sessions to one host read 「名称」 and 「名称 (2)」; a name the user chose stays as typed.
func TestDuplicateTabNamesAreNumbered(t *testing.T) {
	a, p, tt := fixture(t)
	h := remoteHost("prod")
	session(t, p, tt, "1", "prod", h)
	session(t, p, tt, "2", "prod", h)
	session(t, p, tt, "3", "prod", h)
	has(t, tt, "prod", "prod (2)", "prod (3)")
	if a.Tabs[0].Title != "prod" || tt.HasText("prod (4)") {
		t.Fatal("numbering", a.Tabs[0].Title)
	}
	a.Tabs[1].Title, a.Tabs[1].ManualTitle = "prod", true
	tt.Frame()
	has(t, tt, "prod (2)") // the third is now the second automatic one
	if tt.HasText("prod (3)") {
		t.Fatal("a typed name counted")
	}
	// The palette lists them apart.
	tt.Key(appMod(), ui.KeyK)
	has(t, tt, "prod (2)")
}

// The 「+」 opens a menu: a local terminal, the recent hosts, the connection manager. ⌘T stays the local terminal.
func TestPlusButtonOpensAMenuOfLocalRecentAndManager(t *testing.T) {
	p := newPlatform(t)
	web := saveHost(t, p, model.Connection{Name: "prod-web", Host: "10.0.0.1", User: "root"})
	saveHost(t, p, model.Connection{Name: "db-master", Host: "10.0.2.10", User: "postgres"})
	_, tt := start(p)
	session(t, p, tt, "A", "tab-A", remoteHost("A"))
	click(t, tt, "新建标签")
	menu := tt.Menu()
	for _, want := range []string{"本地终端", "prod-web", "db-master", "打开连接管理器…"} {
		if !contains(menu, want) {
			t.Fatal("menu", want, menu)
		}
	}
	if p.local != 0 {
		t.Fatal("the button itself opened a terminal")
	}
	if e := tt.ChooseMenuItem("本地终端"); e != nil || p.local != 1 {
		t.Fatal(e, p.local)
	}
	click(t, tt, "新建标签")
	if e := tt.ChooseMenuItem("prod-web"); e != nil || len(p.connects) != 1 || p.connects[0].ID != web.ID {
		t.Fatal(e, p.connects)
	}
	click(t, tt, "新建标签")
	if e := tt.ChooseMenuItem("打开连接管理器…"); e != nil || p.managerShown != 1 {
		t.Fatal(e, p.managerShown)
	}
	tt.Key(appMod(), ui.KeyT)
	if p.local != 2 {
		t.Fatal("⌘T", p.local)
	}
}
func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

// With many tabs the one that becomes active is scrolled into view, whether it was added,
// chosen with a key, or picked from the palette.
func TestActiveTabScrollsIntoView(t *testing.T) {
	a, p, tt := fixture(t)
	const n = 12
	for i := 1; i <= n; i++ {
		session(t, p, tt, fmt.Sprint("s", i), fmt.Sprintf("server-%02d", i), remoteHost(fmt.Sprint("h", i)))
	}
	// The strip lies between the left column and the activity strip.
	visible := func(title string) bool {
		r, ok := tt.Find(title)
		return ok && r.W > 0 && r.X >= leftEdge && r.X+r.W <= 1280-rightEdge // a tab out of the strip is not laid out: no width
	}
	if !visible("server-12") {
		r, _ := tt.Find("server-12")
		t.Fatal("the new tab is out of view", r)
	}
	if visible("server-01") {
		t.Fatal("the test needs more tabs than fit")
	}
	tt.Key(ui.Cmd, ui.Key1)
	if a.Active != 0 || !visible("server-01") {
		r, _ := tt.Find("server-01")
		t.Fatal("tab 1 is out of view", a.Active, r)
	}
	tt.Key(ui.Cmd, ui.Key9) // ⌘9 is the last tab
	if !visible("server-12") {
		t.Fatal("the last tab is out of view")
	}
	// Scrolling away stays: it comes into view when the tab changes, not in every frame.
	r, _ := tt.Find("server-12")
	tt.Scroll(r.X, r.Y, 800, 0)
	tt.Frame()
	if visible("server-12") && visible("server-01") {
		t.Fatal("the strip did not scroll")
	}
	// A tab chosen in the palette comes into view.
	tt.Key(appMod(), ui.KeyK)
	tt.Type("server-03")
	tt.Key(0, ui.KeyEnter)
	if a.Tabs[a.Active].Title != "server-03" || !visible("server-03") {
		r, _ := tt.Find("server-03")
		t.Fatal("palette", a.Active, r)
	}
}

const leftEdge, rightEdge = 240, 40 // the left column, the activity strip

// A session that ended while the left column is folded says so under the terminal, with the way back.
func TestEndedSessionBannerShowsWhileTheLeftColumnIsFolded(t *testing.T) {
	a, p, tt := fixture(t)
	s := session(t, p, tt, "x", "gone", remoteHost("gone"))
	s.Terminal.Close()
	<-s.Terminal.Done()
	tt.Frame()
	if tt.HasText("连接已断开") && tt.HasText("按回车键重新连接") {
		t.Fatal("the card already says it; no banner")
	}
	tt.Key(appMod(), ui.KeyB)
	has(t, tt, "连接已断开", "按回车键重新连接")
	click(t, tt, "重新连接")
	if p.reconnected != 1 || len(a.Tabs) != 1 {
		t.Fatal("reconnect", p.reconnected)
	}
	// While reconnecting there is nothing to offer.
	if tt.HasText("按回车键重新连接") {
		t.Fatal("banner while connecting")
	}
	// A local shell that exited says so too.
	l := session(t, p, tt, "l", "本地终端", model.Connection{})
	l.Terminal.Close()
	<-l.Terminal.Done()
	tt.Frame()
	has(t, tt, "终端已退出", "重新打开")
}
