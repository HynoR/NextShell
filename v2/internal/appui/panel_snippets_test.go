package appui_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/model"
)

// barText is what the command bar holds.
func barText(t *testing.T, tt *ui.Tester) string {
	t.Helper()
	info, ok := tt.Access("命令输入")
	if !ok {
		t.Fatal("no command bar", tt.Texts())
	}
	return info.Value
}

// hover moves the pointer over the element labeled label.
func hover(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	r, ok := tt.Find(label)
	if !ok {
		t.Fatal("nothing named", label, tt.Texts())
	}
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Advance(time.Second)
	tt.Frame()
}

func snippetFixture(t *testing.T) (*fakePlatform, *ui.Tester) {
	t.Helper()
	p := newPlatform(t)
	for _, c := range []model.Command{
		{Name: "重启 nginx", Group: "Web", Text: "sudo systemctl restart nginx"},
		{Name: "查找进程", Group: "Web", Text: "ps aux | grep [#name]"},
		{Name: "清理", Group: "Web", Text: "cd /tmp\nrm -rf cache"},
	} {
		c.ID = p.NewID()
		if e := p.SaveCommand(c); e != nil {
			t.Fatal(e)
		}
	}
	_, tt := start(p)
	tt.SetSize(1280, 1100) // every row in view, to point at
	session(t, p, tt, "one", "prod-web-01", remoteHost("one"))
	tt.Key(ui.Cmd|ui.Shift, ui.KeyS)
	return p, tt
}

func TestSnippetClickFillsTheCommandBarAndSendsNothing(t *testing.T) {
	p, tt := snippetFixture(t)
	click(t, tt, "片段 重启 nginx")
	if len(p.sent) != 0 {
		t.Fatal("a click ran a command on a server:", p.sent)
	}
	if got := barText(t, tt); got != "sudo systemctl restart nginx" {
		t.Fatal("command bar:", got)
	}
	if !tt.Focused("命令输入") {
		t.Fatal("the bar waits for Return, which the list's row would take as another click")
	}
	// Return in the bar sends it, as any typed line.
	tt.Key(0, ui.KeyEnter)
	if !slices.Equal(p.sent, []string{"sudo systemctl restart nginx"}) || barText(t, tt) != "" {
		t.Fatal("sent from the bar", p.sent)
	}
	// A double click sends, and leaves nothing behind in the bar.
	dblclick(t, tt, "片段 重启 nginx")
	if len(p.sent) != 2 || barText(t, tt) != "" {
		t.Fatal("double click", p.sent, barText(t, tt))
	}
}

func TestSnippetSendButtonNamesItsTarget(t *testing.T) {
	p, tt := snippetFixture(t)
	if tt.HasText("发送 重启 nginx") {
		t.Fatal("the button shows without the pointer")
	}
	hover(t, tt, "片段 重启 nginx")
	has(t, tt, "发送 重启 nginx")
	hover(t, tt, "发送 重启 nginx")
	has(t, tt, "发送到 prod-web-01")
	click(t, tt, "发送 重启 nginx")
	if !slices.Equal(p.sent, []string{"sudo systemctl restart nginx"}) {
		t.Fatal("the button sends once:", p.sent)
	}
	if got := barText(t, tt); got != "" {
		t.Fatal("the bar was filled by the send button:", got)
	}
}

func TestSnippetWithParametersAsksFromTheBarAndTheButton(t *testing.T) {
	p, tt := snippetFixture(t)
	click(t, tt, "片段 查找进程")
	if got := barText(t, tt); got != "ps aux | grep [#name]" || len(p.sent) != 0 {
		t.Fatal("filled as written:", got, p.sent)
	}
	tt.Key(0, ui.KeyEnter)
	has(t, tt, "参数原样替换，不做 shell 转义")
	if len(p.sent) != 0 {
		t.Fatal("the placeholder went out as typed:", p.sent)
	}
	tt.Type("nginx")
	click(t, tt, "发送")
	if !slices.Equal(p.sent, []string{"ps aux | grep nginx"}) {
		t.Fatal(p.sent)
	}
	// The button and a double click ask too.
	hover(t, tt, "片段 查找进程")
	click(t, tt, "发送 查找进程")
	has(t, tt, "参数原样替换，不做 shell 转义")
}

func TestSnippetOfSeveralLinesWaitsForItsSendButton(t *testing.T) {
	p, tt := snippetFixture(t)
	click(t, tt, "片段 清理")
	if got := barText(t, tt); got != "" || len(p.sent) != 0 {
		t.Fatal("a script does not fit the one-line bar:", got, p.sent)
	}
	click(t, tt, "发送 清理") // selected, so its button shows
	if !slices.Equal(p.sent, []string{"cd /tmp\nrm -rf cache"}) {
		t.Fatal(p.sent)
	}
}

// broadcastPlatform records what each session was sent, and refuses the sessions named in refuse.
type broadcastPlatform struct {
	*fakePlatform
	sentTo map[string][]string
	refuse map[string]bool
}

func (p *broadcastPlatform) ExecuteCommand(session, text string, all bool, appendCR ...bool) error {
	if p.refuse[session] {
		return errors.New("会话已关闭")
	}
	p.sentTo[session] = append(p.sentTo[session], text)
	return nil
}

func TestSendToAllNamesTheTabsSkipsDisconnectedOnesAndReportsByName(t *testing.T) {
	p := &broadcastPlatform{fakePlatform: newPlatform(t), sentTo: map[string][]string{}, refuse: map[string]bool{}}
	if e := p.SaveCommand(model.Command{ID: p.NewID(), Name: "重启 nginx", Group: "Web", Text: "sudo systemctl restart nginx"}); e != nil {
		t.Fatal(e)
	}
	a := appui.New(p)
	tt := ui.NewTester(a.View, 1280, 900)
	session(t, p.fakePlatform, tt, "web", "prod-web-01", remoteHost("web"))
	session(t, p.fakePlatform, tt, "db", "db-master", remoteHost("db"))
	session(t, p.fakePlatform, tt, "loc", "本地终端", model.Connection{})
	gone := session(t, p.fakePlatform, tt, "gone", "legacy-box", remoteHost("gone"))
	gone.Terminal.Close()
	<-gone.Terminal.Done()
	tt.Key(ui.Cmd|ui.Shift, ui.KeyS)
	tt.RightClick("片段 重启 nginx")
	if e := tt.ChooseMenuItem("发送到全部会话…"); e != nil {
		t.Fatal(e)
	}
	// The confirm lists who gets it, by tab title, and counts the disconnected one it leaves out.
	has(t, tt, "· prod-web-01\n· db-master\n· 本地终端", "sudo systemctl restart nginx", "已跳过 1 个已断开的会话", "发送到 3 个会话")
	for _, s := range tt.Texts() {
		if strings.Contains(s, "legacy-box") && strings.Contains(s, "sudo systemctl") {
			t.Fatal("a disconnected tab is listed as a target:", s)
		}
	}
	// A tab that closes between the confirm and the click is reported by its name, not its ID.
	p.refuse["db"] = true
	click(t, tt, "发送到 3 个会话")
	if len(p.sentTo["web"]) != 1 || len(p.sentTo["loc"]) != 1 || len(p.sentTo["gone"]) != 0 || len(p.sentTo["db"]) != 0 {
		t.Fatal("sent to", p.sentTo)
	}
	has(t, tt, "1 个会话没有收到：db-master")
	for _, s := range tt.Texts() {
		if strings.Contains(s, "db:") || strings.Contains(s, "web") && strings.Contains(s, "会话已") {
			t.Fatal("an ID reached the user:", s)
		}
	}
}

func TestSendToAllWithNoLiveSessionSaysSo(t *testing.T) {
	p := &broadcastPlatform{fakePlatform: newPlatform(t), sentTo: map[string][]string{}, refuse: map[string]bool{}}
	if e := p.SaveCommand(model.Command{ID: p.NewID(), Name: "x", Group: "Web", Text: "ls"}); e != nil {
		t.Fatal(e)
	}
	a := appui.New(p)
	tt := ui.NewTester(a.View, 1280, 900)
	gone := session(t, p.fakePlatform, tt, "gone", "legacy-box", remoteHost("gone"))
	gone.Terminal.Close()
	<-gone.Terminal.Done()
	tt.Key(ui.Cmd|ui.Shift, ui.KeyS)
	tt.RightClick("片段 x")
	tt.ChooseMenuItem("发送到全部会话…")
	has(t, tt, "没有可发送的会话")
	if tt.HasText("发送到 0 个会话") {
		t.Fatal("a confirm for nobody")
	}
}

// Two tabs of one host read 「名称 (2)」 everywhere, the send button's tooltip and the activity list included.
func TestNumberedTabTitlesInTooltipsAndLists(t *testing.T) {
	p, tt := snippetFixture(t)
	session(t, p, tt, "two", "prod-web-01", remoteHost("one"))
	hover(t, tt, "片段 重启 nginx")
	hover(t, tt, "发送 重启 nginx")
	has(t, tt, "发送到 prod-web-01 (2)")
}
