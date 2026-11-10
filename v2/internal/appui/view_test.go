package appui_test

import (
	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
	"image/png"
	"net"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/desktop"
	"nextshell.dev/v2/internal/model"
	"os"
	"strings"
	"testing"
	"time"
)

type fakePlatform struct {
	*desktop.Platform
	events                     []appui.Event
	connects                   []model.Connection
	local, cloned, reconnected int
	closed                     []string
}

func (p *fakePlatform) Poll() []appui.Event {
	e := p.events
	p.events = nil
	return append(e, p.Platform.Poll()...)
}
func (p *fakePlatform) Connect(c model.Connection)    { p.connects = append(p.connects, c) }
func (p *fakePlatform) Local()                        { p.local++ }
func (p *fakePlatform) CloneSession(s *appui.Session) { p.cloned++ }
func (p *fakePlatform) Reconnect(s *appui.Session)    { p.reconnected++ }
func (p *fakePlatform) CloseSession(id string)        { p.closed = append(p.closed, id) }
func fixture(t *testing.T) (*appui.App, *fakePlatform, *ui.Tester) {
	t.Helper()
	p, e := desktop.Open(":memory:")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(p.Shutdown)
	fp := &fakePlatform{Platform: p}
	a := appui.New(fp)
	tt := ui.NewTester(a.View, 1400, 1000)
	return a, fp, tt
}
func click(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	if e := tt.Click(label); e != nil {
		t.Fatal(label, e)
	}
}
func TestNativeLayoutAndNavigation(t *testing.T) {
	a, _, tt := fixture(t)
	for _, text := range []string{"NextShell 2", "本地终端", "连接管理", "目录管理", "工作台 · 命令输入 / 历史（本地明文保存，勿输入密码）"} {
		if !tt.HasText(text) {
			t.Fatal("missing", text)
		}
	}
	for _, page := range []struct{ button, page, title string }{{"连接", "connections", "连接管理"}, {"密钥管理", "keys", "SSH 密钥"}, {"代理管理", "proxies", "SOCKS 代理"}, {"活动面板", "activity", "会话切换器与预览"}, {"目录管理", "folders", "目录管理"}, {"终端", "terminal", "NextShell 原生终端"}} {
		click(t, tt, page.button)
		if a.Page != page.page || !tt.HasText(page.title) {
			t.Fatal(page, a.Page, tt.Texts())
		}
	}
	if path := os.Getenv("NEXTSHELL_SCREENSHOT"); path != "" {
		f, e := os.Create(path)
		if e != nil {
			t.Fatal(e)
		}
		e = png.Encode(f, tt.Image())
		f.Close()
		if e != nil {
			t.Fatal(e)
		}
	}
}
func TestUIConnectionCRUDAndBatch(t *testing.T) {
	a, p, tt := fixture(t)
	a.Page = "connections"
	a.Edit.Name = "测试服务器"
	a.Edit.Host = "localhost"
	a.Edit.User = "tester"
	a.Password = "keep-private"
	tt.Frame()
	click(t, tt, "保存连接")
	if a.Edit.ID == "" || a.Password != "" {
		t.Fatal("not saved/cleared")
	}
	id := a.Edit.ID
	snap, e := p.Snapshot()
	if e != nil || len(snap.Connections) != 1 || snap.Connections[0].Secret != "" {
		t.Fatal(snap, e)
	}
	a.Edit.Name = "改名"
	tt.Frame()
	click(t, tt, "保存连接")
	snap, _ = p.Snapshot()
	if snap.Connections[0].Name != "改名" {
		t.Fatal(snap)
	}
	a.Selected[id] = true
	a.Edit.Auth = "agent"
	a.Page = "connections"
	tt.Frame() // direct batch action below uses UI button after scrolling into view
	a.Port = "bad"
	tt.Frame()
	click(t, tt, "保存连接")
	if !strings.Contains(a.Message, "端口") {
		t.Fatal(a.Message)
	}
	a.Port = "22"
	a.Edit.Auth = "password"
	tt.Frame()
	click(t, tt, "移入回收站")
	snap, _ = p.Snapshot()
	if !snap.Connections[0].Deleted {
		t.Fatal("not recycled")
	}
	a.ShowTrash = true
	tt.Frame()
	click(t, tt, "恢复")
	snap, _ = p.Snapshot()
	if snap.Connections[0].Deleted {
		t.Fatal("not restored")
	}
}
func TestQuickConnectAndLocalActions(t *testing.T) {
	a, p, tt := fixture(t)
	a.Quick = "tester@localhost:2222"
	a.QuickPassword = "secret"
	tt.Frame()
	click(t, tt, "快速连接")
	if len(p.connects) != 1 || p.connects[0].Port != 2222 || p.connects[0].Secret != "secret" || a.QuickPassword != "" {
		t.Fatal("quick connect failed")
	}
	click(t, tt, "本地终端")
	if p.local != 1 {
		t.Fatal("local not routed through Platform")
	}
	a.Quick = "bad"
	tt.Frame()
	click(t, tt, "快速连接")
	if len(p.connects) != 1 || a.Message == "" {
		t.Fatal("invalid quick connect forwarded")
	}
}
func TestFolderUIAndSearch(t *testing.T) {
	a, p, tt := fixture(t)
	a.Page = "folders"
	a.Folder = model.Folder{Name: "生产"}
	tt.Frame()
	click(t, tt, "保存目录")
	if a.Folder.ID == "" {
		t.Fatal(a.Message)
	}
	f := a.Folder
	c := model.Connection{ID: p.NewID(), Name: "production", Host: "host.example", Port: 22, User: "root", Auth: "agent", FolderID: f.ID}
	if e := p.SaveConnection(c, nil); e != nil {
		t.Fatal(e)
	} // reconstruct to obtain refreshed directory snapshot
	a2 := appui.New(p)
	tt2 := ui.NewTester(a2.View, 1400, 1000)
	if !tt2.HasText("  production") {
		t.Fatal(tt2.Texts())
	}
	a2.Search = "nomatch"
	tt2.Frame()
	if tt2.HasText("  production") {
		t.Fatal("filter ignored")
	}
	a2.Search = "host.example"
	tt2.Frame()
	if !tt2.HasText("  production") {
		t.Fatal("host search failed")
	}
	click(t, tt2, "▾ 生产")
	if tt2.HasText("  production") {
		t.Fatal("collapse failed")
	}
}
func newTerminal(t *testing.T, text string) *terminal.Terminal {
	t.Helper()
	a, b := net.Pipe()
	term, e := terminal.New(terminal.Options{Conn: a, NoClipboardWrite: true})
	if e != nil {
		t.Fatal(e)
	}
	term.Feed([]byte(text))
	t.Cleanup(func() { b.Close(); term.Close() })
	return term
}
func TestTabsCommandsPreviewAndReconnect(t *testing.T) {
	a, p, tt := fixture(t)
	one := &appui.Session{ID: "one", Title: "One", Terminal: newTerminal(t, "hello-one"), Local: true}
	two := &appui.Session{ID: "two", Title: "Two", Terminal: newTerminal(t, "hello-two"), Local: true}
	p.events = []appui.Event{{Session: one}, {Session: two}}
	tt.Frame()
	if a.Active != 1 || len(a.Tabs) != 2 {
		t.Fatal("session events lost")
	}
	click(t, tt, "One")
	a.TabName = "Renamed"
	tt.Frame()
	click(t, tt, "重命名")
	if one.Title != "Renamed" {
		t.Fatal("rename failed")
	}
	click(t, tt, "复制会话")
	click(t, tt, "重连")
	if p.cloned != 1 || p.reconnected != 1 || len(a.Tabs) != 2 {
		t.Fatal("reconnect removed original before success")
	}
	replacement := &appui.Session{ID: "new-one", Replaces: "one", Title: "new", Terminal: newTerminal(t, "replacement"), Local: true}
	p.events = []appui.Event{{Session: replacement}}
	tt.Frame()
	if a.Tabs[0].ID != "new-one" || len(a.Tabs) != 2 || replacement.Title != "Renamed" {
		t.Fatal("replacement not in-place")
	}
	a.Page = "activity"
	tt.Frame()
	if !tt.HasText("会话切换器与预览") {
		t.Fatal("no previews")
	}
	a.Page = "terminal"
	tt.Frame()
	click(t, tt, "→")
	if a.Active != 1 || a.Tabs[1].ID != "new-one" {
		t.Fatal("move lost selected tab")
	}
	click(t, tt, "断开并关闭")
	if len(a.Tabs) != 1 || a.Tabs[0].ID != "two" {
		t.Fatal("close failed")
	}
}

func TestTabDragReordering(t *testing.T) {
	a, p, tt := fixture(t)
	for _, id := range []string{"A", "B", "C"} {
		p.events = append(p.events, appui.Event{Session: &appui.Session{ID: id, Title: id, Terminal: newTerminal(t, ""), Local: true}})
	}
	tt.Frame()
	from, ok := tt.Find("A")
	if !ok {
		t.Fatal("A absent")
	}
	to, ok := tt.Find("● C")
	if !ok {
		t.Fatal("C absent")
	}
	x, y := from.X+from.W/2, from.Y+from.H/2
	tx, ty := to.X+to.W/2, to.Y+to.H/2
	tt.Press(x, y)
	tt.Move(x+12, y)
	tt.Move(tx, ty)
	tt.Release(tx, ty)
	tt.Frame()
	if a.Tabs[2].ID != "A" || a.Tabs[a.Active].ID != "C" {
		t.Fatalf("drag order=%s%s%s active=%d", a.Tabs[0].ID, a.Tabs[1].ID, a.Tabs[2].ID, a.Active)
	}
}
func TestPromptResponseCancelAndExpiry(t *testing.T) {
	a, p, tt := fixture(t)
	done := make(chan struct{})
	prompt := &appui.Prompt{Title: "SSH test", Questions: []string{"Password:"}, Echo: []bool{false}, Answers: make(chan []string, 1), Done: done}
	p.events = []appui.Event{{Prompt: prompt}}
	tt.Frame()
	a.Answers[0] = "private"
	tt.Frame()
	click(t, tt, "继续认证")
	answer := <-prompt.Answers
	if len(answer) != 1 || answer[0] != "private" || len(a.Prompts) != 0 {
		t.Fatal("bad response")
	}
	p.events = []appui.Event{{Prompt: prompt}}
	tt.Frame()
	click(t, tt, "取消认证")
	if answer = <-prompt.Answers; answer != nil {
		t.Fatal("cancel not nil")
	}
	p.events = []appui.Event{{Prompt: prompt}}
	tt.Frame()
	close(done)
	tt.Frame()
	if len(a.Prompts) != 0 {
		t.Fatal("expired prompt retained")
	}
}

func TestRealLocalWorkbenchAndHistory(t *testing.T) {
	p, e := desktop.Open(":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer p.Shutdown()
	a := appui.New(p)
	tt := ui.NewTester(a.View, 1400, 1000)
	p.Local()
	until := time.Now().Add(5 * time.Second)
	for len(a.Tabs) == 0 {
		if time.Now().After(until) {
			t.Fatal("local event missing", a.Message)
		}
		time.Sleep(10 * time.Millisecond)
		tt.Frame()
	}
	a.Command = "printf '\\nnextshell-ui-ok\\n'"
	tt.Frame()
	click(t, tt, "发送")
	for !strings.Contains(a.Tabs[0].Terminal.Text(), "\nnextshell-ui-ok") {
		if time.Now().After(until) {
			t.Fatal(a.Tabs[0].Terminal.Text())
		}
		time.Sleep(10 * time.Millisecond)
		tt.Frame()
	}
	snap, e := p.Snapshot()
	if e != nil || len(snap.History) != 1 || a.Command != "" {
		t.Fatal("command history not persisted", snap, e)
	}
	click(t, tt, "上一条")
	if a.Command != snap.History[0] {
		t.Fatal("history recall failed")
	}
	if path := os.Getenv("NEXTSHELL_TERMINAL_SCREENSHOT"); path != "" {
		f, e := os.Create(path)
		if e != nil {
			t.Fatal(e)
		}
		e = png.Encode(f, tt.Image())
		f.Close()
		if e != nil {
			t.Fatal(e)
		}
	}
	info, ok := tt.AccessByRole(ui.RoleTextField, "命令输入")
	if !ok {
		t.Fatal("command accessibility missing")
	}
	tt.ClickAt(info.Bounds.X+info.Bounds.W/2, info.Bounds.Y+info.Bounds.H/2)
	tt.Key(ui.Ctrl, ui.KeyA)
	tt.Type("printf '\\nnextshell-enter-ok\\n'")
	tt.Key(0, ui.KeyEnter)
	until = time.Now().Add(3 * time.Second)
	for !strings.Contains(a.Tabs[0].Terminal.Text(), "\nnextshell-enter-ok") {
		if time.Now().After(until) {
			t.Fatal("enter command failed", a.Tabs[0].Terminal.Text())
		}
		time.Sleep(10 * time.Millisecond)
		tt.Frame()
	}
}
func TestGridAndBatchBinding(t *testing.T) {
	p, e := desktop.Open(":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer p.Shutdown()
	for _, id := range []string{"a", "b"} {
		c := model.Connection{ID: id, Name: "server-" + id, Host: "localhost", Port: 22, User: "tester", Auth: "password"}
		if e = p.SaveConnection(c, nil); e != nil {
			t.Fatal(e)
		}
	}
	a := appui.New(p)
	a.Page = "connections"
	a.ConnectionLayout = "grid"
	a.Selected["a"] = true
	a.Selected["b"] = true
	a.Edit.Auth = "agent"
	tt := ui.NewTester(a.View, 1500, 1800)
	ra, ok := tt.Find("server-a · tester@localhost:22")
	if !ok {
		t.Fatal(tt.Texts())
	}
	rb, ok := tt.Find("server-b · tester@localhost:22")
	if !ok || ra.Y != rb.Y || ra.X == rb.X {
		t.Fatal("not two-column grid", ra, rb)
	}
	click(t, tt, "批量绑定认证")
	snap, e := p.Snapshot()
	if e != nil {
		t.Fatal(e)
	}
	for _, c := range snap.Connections {
		if c.Auth != "agent" {
			t.Fatal("batch not applied")
		}
	}
}
func TestExitConfirmation(t *testing.T) {
	a, p, tt := fixture(t)
	quit := false
	p.SetQuit(func() { quit = true })
	a.ConfirmExit = true
	tt.Frame()
	click(t, tt, "继续使用")
	if a.ConfirmExit || quit {
		t.Fatal("cancel quit failed")
	}
	a.ConfirmExit = true
	tt.Frame()
	click(t, tt, "退出应用")
	if !a.ExitConfirmed || !quit {
		t.Fatal("quit not delegated")
	}
}
