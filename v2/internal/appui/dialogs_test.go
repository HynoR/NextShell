package appui_test

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/model"
)

func prompt(p *fakePlatform, tt *ui.Tester, pr appui.Prompt) (*appui.Prompt, chan struct{}) {
	pr.Answers = make(chan []string, 1)
	done := make(chan struct{})
	pr.Done = done
	p.events = append(p.events, appui.Event{Prompt: &pr})
	tt.Frame()
	return &pr, done
}

func answer(t *testing.T, pr *appui.Prompt) []string {
	t.Helper()
	select {
	case a := <-pr.Answers:
		return a
	case <-time.After(2 * time.Second):
		t.Fatal("no answer")
		return nil
	}
}

func TestHostKeyTrustCancelAndChangedNeedsSecondConfirmation(t *testing.T) {
	_, p, tt := fixture(t)
	pr, _ := prompt(p, tt, appui.Prompt{Kind: appui.PromptHostKey, Host: "h:22", Fingerprint: "SHA256:abc"})
	has(t, tt, "首次连接，请核对主机指纹", "信任并连接")
	click(t, tt, "信任并连接")
	if got := answer(t, pr); got == nil {
		t.Fatal("trust not sent")
	}
	pr, _ = prompt(p, tt, appui.Prompt{Kind: appui.PromptHostKey, Host: "h:22", Fingerprint: "SHA256:abc"})
	tt.Key(0, ui.KeyEscape)
	if got := answer(t, pr); got != nil {
		t.Fatal("escape trusted the key")
	}

	pr, _ = prompt(p, tt, appui.Prompt{Kind: appui.PromptHostKeyChanged, Host: "h:22", Fingerprint: "SHA256:new", OldFingerprint: "SHA256:old"})
	has(t, tt, "主机指纹已变化", "SHA256:old", "SHA256:new")
	tt.Key(0, ui.KeyEnter) // the default button disconnects
	if got := answer(t, pr); got != nil {
		t.Fatal("enter accepted a changed key")
	}
	pr, _ = prompt(p, tt, appui.Prompt{Kind: appui.PromptHostKeyChanged, Host: "h:22", Fingerprint: "SHA256:new", OldFingerprint: "SHA256:old"})
	click(t, tt, "更新指纹并连接…")
	if len(pr.Answers) != 0 {
		t.Fatal("updated without the second confirmation")
	}
	has(t, tt, "确定更新主机指纹？")
	click(t, tt, "更新指纹并连接")
	if got := answer(t, pr); got == nil {
		t.Fatal("update not sent")
	}
}

func TestPasswordPromptRememberCancelAndExpiry(t *testing.T) {
	a, p, tt := fixture(t)
	pr, _ := prompt(p, tt, appui.Prompt{Kind: appui.PromptPassword, Title: "输入密码", Host: "root@h:22", Questions: []string{"密码"}, Echo: []bool{false}, CanRemember: true})
	has(t, tt, "输入密码", "root@h:22", "记住密码")
	click(t, tt, "记住密码")
	click(t, tt, "密码")
	tt.Type("private")
	tt.Key(0, ui.KeyEnter)
	got := answer(t, pr)
	if len(got) != 1 || got[0] != "private" || !pr.Remember {
		t.Fatal("answer", got, pr.Remember)
	}
	pr, _ = prompt(p, tt, appui.Prompt{Title: "服务器验证", Questions: []string{"验证码"}, Echo: []bool{true}})
	if tt.HasText("记住密码") {
		t.Fatal("remember offered without a saved host")
	}
	click(t, tt, "取消")
	if got := answer(t, pr); got != nil {
		t.Fatal("cancel answered")
	}
	_, done := prompt(p, tt, appui.Prompt{Title: "过期", Questions: []string{"q"}})
	close(done)
	tt.Frame()
	if tt.HasText("过期") {
		t.Fatal("expired prompt kept")
	}
	_ = a
}

func TestLinksExitAndErrorDetails(t *testing.T) {
	a, p, tt := fixture(t)
	url := "https://example.invalid/real-target"
	p.events = append(p.events, appui.Event{Link: url})
	tt.Frame()
	has(t, tt, "请确认完整地址可信：\n"+url)
	click(t, tt, "取消")
	if len(p.opened) != 0 {
		t.Fatal("cancel opened")
	}
	p.events = append(p.events, appui.Event{Link: url})
	tt.Frame()
	click(t, tt, "打开链接")
	if len(p.opened) != 1 {
		t.Fatal(p.opened)
	}
	quit := false
	p.SetQuit(func() { quit = true })
	a.ConfirmExit = true
	tt.Frame()
	has(t, tt, "退出 NextShell？")
	click(t, tt, "取消")
	if a.ConfirmExit || quit {
		t.Fatal("cancel quit")
	}
	a.ConfirmExit = true
	tt.Frame()
	click(t, tt, "退出")
	if !a.ExitConfirmed || !quit {
		t.Fatal("quit not delegated")
	}
	p.events = append(p.events, appui.Event{Failed: "x", Message: "dial tcp: connection refused by the remote host after several attempts, giving up now"})
	tt.Frame()
	click(t, tt, "详情")
	has(t, tt, "详情", "dial tcp: connection refused by the remote host after several attempts, giving up now")
}

func TestPaletteConnectsSwitchesRunsAndActs(t *testing.T) {
	p := newPlatform(t)
	saveHost(t, p, model.Connection{Name: "web-palette", Host: "10.1.1.1", User: "root"})
	cmd := model.Command{ID: p.NewID(), Name: "uptime-snippet", Group: "默认", Text: "uptime"}
	p.SaveCommand(cmd)
	a, tt := start(p)
	tt.Key(appMod(), ui.KeyK)
	has(t, tt, "主机", "操作", "新建主机")
	tt.Type("ops@10.2.2.2:2201")
	tt.Key(0, ui.KeyEnter)
	if len(p.connects) != 1 || p.connects[0].Port != 2201 || p.connects[0].User != "ops" {
		t.Fatal("quick connect", p.connects)
	}
	tt.Key(appMod(), ui.KeyK)
	tt.Type("webpal")
	tt.Key(0, ui.KeyEnter)
	if len(p.connects) != 2 || p.connects[1].Name != "web-palette" {
		t.Fatal("fuzzy host", p.connects)
	}
	session(t, p, tt, "s1", "first", remoteHost("first"))
	session(t, p, tt, "s2", "second", remoteHost("second"))
	tt.Key(appMod(), ui.KeyK)
	tt.Type("first")
	tt.Key(0, ui.KeyEnter)
	if a.Active != 0 {
		t.Fatal("tab switch")
	}
	tt.Key(appMod(), ui.KeyK)
	tt.Type("uptime-sn")
	tt.Key(0, ui.KeyEnter)
	if len(p.sent) != 1 || p.sent[0] != "uptime" {
		t.Fatal("snippet", p.sent)
	}
	tt.Key(appMod(), ui.KeyK)
	tt.Type("uptime-sn")
	tt.Key(ui.Alt, ui.KeyEnter)
	has(t, tt, "发送到全部会话", "发送到 2 个会话")
	click(t, tt, "发送到 2 个会话")
	if len(p.sent) != 3 { // one before, then one per session
		t.Fatal("broadcast", p.sent)
	}
	tt.Key(appMod(), ui.KeyK)
	tt.Type("新建主机")
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyUp)
	tt.Key(0, ui.KeyEnter)
	mt := ui.NewTester(a.ManagerView, 960, 700)
	if p.managerShown != 1 {
		t.Fatal("新建主机 did not open the connection manager")
	}
	has(t, mt, "新建主机", "认证方式")
	mt.Key(0, ui.KeyEscape)
	tt.Key(appMod(), ui.KeyK)
	tt.Type("打开设置")
	tt.Key(0, ui.KeyEnter)
	if p.settingsShown != 1 {
		t.Fatal("settings action")
	}
}

// A dialog that an agent's session_open raised says so, in every kind of dialog; one the user asked
// for does not.
func TestDialogsSayWhenAnAgentStartedTheConnection(t *testing.T) {
	_, p, tt := fixture(t)
	for _, pr := range []appui.Prompt{
		{Kind: appui.PromptHostKey, Host: "h:22", Fingerprint: "SHA256:abc"},
		{Kind: appui.PromptHostKeyChanged, Host: "h:22", Fingerprint: "SHA256:new", OldFingerprint: "SHA256:old"},
		{Kind: appui.PromptPassword, Title: "输入密码", Host: "root@h:22", Questions: []string{"密码"}, Echo: []bool{false}},
		{Title: "服务器验证", Questions: []string{"验证码"}, Echo: []bool{true}},
	} {
		own, _ := prompt(p, tt, pr)
		if tt.HasText("由 Agent 发起") {
			t.Fatalf("kind %q: a dialog the user asked for says an agent did", pr.Kind)
		}
		tt.Key(0, ui.KeyEscape)
		answer(t, own)
		pr.ByAgent = true
		agent, _ := prompt(p, tt, pr)
		has(t, tt, "由 Agent 发起")
		tt.Key(0, ui.KeyEscape)
		if got := answer(t, agent); got != nil {
			t.Fatal("escape answered", got)
		}
	}
}
