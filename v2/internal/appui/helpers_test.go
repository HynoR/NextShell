package appui_test

import (
	"net"
	"testing"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/desktop"
	"nextshell.dev/v2/internal/model"
	"nextshell.dev/v2/internal/transfer"
)

// fakePlatform is the real desktop platform on an in-memory profile, with
// sessions, SFTP and transfers recorded instead of touching the network.
type fakePlatform struct {
	*desktop.Platform
	terminals                   map[string]*terminal.Terminal
	events                      []appui.Event
	connects                    []model.Connection
	local, cloned, reconnected  int
	closed, listed, sent        []string
	cancelled                   []string
	reloaded                    []string
	queued                      []appui.TransferRequest
	actions                     [][3]string
	jobs                        []transfer.Job
	watching, detailed          map[string]bool
	opened                      []string
	settingsShown, managerShown int
}

func (p *fakePlatform) Poll() []appui.Event {
	e := p.events
	for _, ev := range e {
		if ev.Session != nil {
			p.terminals[ev.Session.ID] = ev.Session.Terminal
		}
	}
	p.events = nil
	return append(e, p.Platform.Poll()...)
}
func (p *fakePlatform) Connect(c model.Connection)    { p.connects = append(p.connects, c) }
func (p *fakePlatform) Local()                        { p.local++ }
func (p *fakePlatform) CancelConnect(id string)       { p.cancelled = append(p.cancelled, id) }
func (p *fakePlatform) CloneSession(s *appui.Session) { p.cloned++ }
func (p *fakePlatform) Reconnect(s *appui.Session)    { p.reconnected++ }
func (p *fakePlatform) CloseSession(id string)        { p.closed = append(p.closed, id) }
func (p *fakePlatform) ListFiles(session, path string) {
	p.listed = append(p.listed, session+":"+path)
}
func (p *fakePlatform) FileAction(session, action, from, to string) {
	p.actions = append(p.actions, [3]string{action, from, to})
}
func (p *fakePlatform) QueueTransfer(r appui.TransferRequest) (string, error) {
	p.queued = append(p.queued, r)
	return "job", nil
}
func (p *fakePlatform) Transfers() []transfer.Job             { return p.jobs }
func (p *fakePlatform) RefreshMonitor(string)                 {}
func (p *fakePlatform) WatchMonitor(id string, on bool)       { p.watching[id] = on }
func (p *fakePlatform) WatchMonitorDetail(id string, on bool) { p.detailed[id] = on }
func (p *fakePlatform) OpenRemote(session, path string)       { p.opened = append(p.opened, path) }
func (p *fakePlatform) ReloadRemote(session, path string) {
	p.reloaded = append(p.reloaded, path)
}
func (p *fakePlatform) OpenURL(s string) error      { p.opened = append(p.opened, s); return nil }
func (p *fakePlatform) SessionDir(id string) string { return "" }
func (p *fakePlatform) ExecuteCommand(session, text string, appendCR ...bool) error {
	p.sent = append(p.sent, text)
	return nil
}
func (p *fakePlatform) Send(id, text string) error { p.sent = append(p.sent, text); return nil }
func (p *fakePlatform) ScreenText(id string) string {
	if t := p.terminals[id]; t != nil {
		return t.Text()
	}
	return ""
}
func (p *fakePlatform) Exited(id string) bool {
	t := p.terminals[id]
	if t == nil {
		return true
	}
	select {
	case <-t.Done():
		return true
	default:
		return false
	}
}
func (p *fakePlatform) TerminalTitle(id string) string {
	if t := p.terminals[id]; t != nil {
		return t.Title()
	}
	return ""
}

func newPlatform(t *testing.T) *fakePlatform {
	t.Helper()
	p, e := desktop.Open(":memory:")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(p.Shutdown)
	return &fakePlatform{Platform: p, terminals: map[string]*terminal.Terminal{}, watching: map[string]bool{}, detailed: map[string]bool{}}
}
func fixture(t *testing.T) (*appui.App, *fakePlatform, *ui.Tester) {
	t.Helper()
	p := newPlatform(t)
	a, tt := start(p)
	return a, p, tt
}

// start opens the app on p, after the test saved what it needs.
func start(p *fakePlatform) (*appui.App, *ui.Tester) {
	a := appui.New(p)
	a.ShowSettings = func() { p.settingsShown++ }
	a.ShowManager = func() { p.managerShown++ }
	return a, ui.NewTester(a.View, 1280, 800)
}
func click(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	if e := tt.Click(label); e != nil {
		t.Fatal(label, e, tt.Texts())
	}
}
func has(t *testing.T, tt *ui.Tester, texts ...string) {
	t.Helper()
	for _, s := range texts {
		if !tt.HasText(s) {
			t.Fatalf("missing %q in %q", s, tt.Texts())
		}
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

// session opens a remote-looking session through a platform event.
func session(t *testing.T, p *fakePlatform, tt *ui.Tester, id, title string, c model.Connection) *appui.Session {
	t.Helper()
	s := &appui.Session{ID: id, Title: title, Terminal: newTerminal(t, "$ "), Connection: c, Local: c.Host == ""}
	p.events = append(p.events, appui.Event{Session: s})
	tt.Frame()
	return s
}
func saveHost(t *testing.T, p *fakePlatform, c model.Connection) model.Connection {
	t.Helper()
	if c.ID == "" {
		c.ID = p.NewID()
		c.Monitor = true // a new host samples
	}
	if c.Port == 0 {
		c.Port = 22
	}
	if c.Auth == "" {
		c.Auth = "password"
	}
	if c.Encoding == "" {
		c.Encoding = "utf-8"
	}
	if e := p.SaveConnection(c, nil); e != nil {
		t.Fatal(e)
	}
	return c
}

// choose picks option in the drop-down labeled label.
func choose(t *testing.T, tt *ui.Tester, label, option string) {
	t.Helper()
	info, ok := tt.AccessByRole(ui.RolePopUpButton, label)
	if !ok {
		t.Fatal("no drop-down", label, tt.Texts())
	}
	tt.ClickAt(info.Bounds.X+info.Bounds.W/2, info.Bounds.Y+info.Bounds.H/2)
	click(t, tt, option)
}

// clickLast clicks the last element named label, as the 密钥 of a form below the 密钥 page tab.
func clickLast(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	var last *ui.AccessInfo
	for _, v := range tt.AccessAll() {
		if v.Name == label {
			last = &v
		}
	}
	if last == nil {
		t.Fatal("no", label, tt.Texts())
	}
	tt.ClickAt(last.Bounds.X+last.Bounds.W/2, last.Bounds.Y+last.Bounds.H/2)
}
