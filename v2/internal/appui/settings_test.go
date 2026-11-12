package appui_test

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/agent"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/cloudsync"
	"nextshell.dev/v2/internal/model"
	"nextshell.dev/v2/internal/storage"
)

type servicesPlatform struct {
	*fakePlatform
	cloud     appui.CloudOverview
	agent     appui.AgentOverview
	remote    []bool
	responses []bool
	connects  int
	diagnosed string
	cancelled bool
	auto      agent.AutoApproval
	autoSet   []time.Duration
}

func (p *servicesPlatform) CloudOverview() appui.CloudOverview { return p.cloud }
func (p *servicesPlatform) AgentOverview() appui.AgentOverview { return p.agent }
func (p *servicesPlatform) CloudSync(id string, remote bool)   { p.remote = append(p.remote, remote) }
func (p *servicesPlatform) CloudConnect(endpoint, id, name, auth, encryption string, join bool) {
	p.connects++
}
func (p *servicesPlatform) AgentRespond(id string, approve bool) {
	p.responses = append(p.responses, approve)
	p.agent.Pending = nil
}
func (p *servicesPlatform) Diagnose(kind, target string) string {
	p.diagnosed = kind + " " + target
	return "current"
}
func (p *servicesPlatform) CancelDiagnostic() { p.cancelled = true }
func (p *servicesPlatform) AgentAutoApprove(d time.Duration) error {
	p.autoSet = append(p.autoSet, d)
	switch {
	case d == 0:
		p.auto = agent.AutoApproval{}
	case d == agent.UntilExit:
		p.auto = agent.AutoApproval{On: true, UntilExit: true}
	default:
		p.auto = agent.AutoApproval{On: true, Expires: time.Now().Add(d)}
	}
	return nil
}
func (p *servicesPlatform) AgentAutoApproval() agent.AutoApproval { return p.auto }
func (p *servicesPlatform) MCPConfigPath() (string, error)        { return "/cfg/NextShell-v2/mcp.json", nil }

func TestAgentAutoApproveSettingAndStatusIndicator(t *testing.T) {
	a, p, tt, st := settingsFixture(t)
	shown := 0
	a.ShowSettings = func() { shown++ }
	click(t, st, "Agent")
	has(t, st, "/cfg/NextShell-v2/mcp.json", "开启 MCP 服务时写入地址和令牌；Claude Code 插件会自动读取，无需手动填写")
	click(t, st, "15 分钟")
	if len(p.autoSet) != 1 || p.autoSet[0] != 15*time.Minute {
		t.Fatal(p.autoSet)
	}
	has(t, st, "自动批准中 · 剩余 15 分钟")
	tt.Frame()
	has(t, tt, "自动批准中 · 剩余 15 分钟")
	click(t, tt, "自动批准中")
	if shown != 1 {
		t.Fatal("indicator did not open the Agent settings")
	}
	click(t, st, "直到退出")
	has(t, st, "自动批准中 · 直到退出")
	// Turning it off elsewhere (emergency stop, MCP off) shows at once.
	p.auto = agent.AutoApproval{}
	tt.Frame()
	st.Frame()
	if tt.HasText("自动批准中") || st.HasText("自动批准中") {
		t.Fatal("indicator outlived auto-approval")
	}
	click(t, st, "1 小时")
	click(t, st, "关闭")
	if p.autoSet[len(p.autoSet)-1] != 0 {
		t.Fatal(p.autoSet)
	}
}

// flip clicks the switch named label.
func flip(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	info, ok := tt.AccessByRole(ui.RoleSwitch, label)
	if !ok {
		t.Fatal("no switch", label)
	}
	b := info.Bounds
	tt.ClickAt(b.X+b.W/2, b.Y+b.H/2)
}

func settingsFixture(t *testing.T) (*appui.App, *servicesPlatform, *ui.Tester, *ui.Tester) {
	base := newPlatform(t)
	p := &servicesPlatform{fakePlatform: base}
	a := appui.New(p)
	return a, p, ui.NewTester(a.View, 1280, 800), ui.NewTester(a.SettingsView, 880, 1400)
}

func TestSettingsApplyInstantlyAndPersist(t *testing.T) {
	a, p, tt, st := settingsFixture(t)
	s := session(t, p.fakePlatform, tt, "one", "one", remoteHost("one"))
	_ = s
	click(t, st, "终端")
	click(t, st, "字号")
	st.Key(ui.Cmd, ui.KeyA)
	st.Type("18")
	if got := p.Preferences().FontSize; got != 18 {
		t.Fatal("font size not applied", got)
	}
	st.Key(ui.Cmd, ui.KeyA)
	st.Type("99")
	has(t, st, "字号需为10..24，行高需为1..2")
	if p.Preferences().FontSize != 18 {
		t.Fatal("invalid value saved")
	}
	st.Key(ui.Cmd, ui.KeyA)
	st.Type("16")
	flip(t, st, "光标闪烁")
	click(t, st, "竖线")
	click(t, st, "通用")
	click(t, st, "深色")
	got := p.Preferences()
	if got.FontSize != 16 || got.CursorBlink || got.Cursor != "bar" || got.Appearance != "深色" {
		t.Fatal("not persisted", got)
	}
	click(t, st, "SSH")
	click(t, st, "默认间隔（秒）")
	st.Key(ui.Cmd, ui.KeyA)
	st.Type("2")
	has(t, st, "心跳间隔须为 5–600 秒")
	st.Type("0")
	if p.Preferences().KeepaliveSeconds != 20 {
		t.Fatal("keepalive", p.Preferences().KeepaliveSeconds)
	}
	// The palette's appearance toggle and the settings window share one state.
	tt.Key(ui.Cmd, ui.KeyK)
	tt.Type("切换深浅色")
	tt.Key(0, ui.KeyEnter)
	st.Frame()
	if p.Preferences().Appearance != "浅色" || a.Prefs.Appearance != "浅色" {
		t.Fatal("toggle", p.Preferences().Appearance)
	}
	click(t, st, "终端")
	click(t, st, "恢复默认设置…")
	click(t, st, "恢复默认")
	st.Frame()
	if p.Preferences() != model.DefaultPreferences() {
		t.Fatal("reset", p.Preferences())
	}
	// A fresh app reads the saved preferences.
	if appui.New(p).Prefs != p.Preferences() {
		t.Fatal("reload")
	}
}

func TestSettingsKeysAndProxies(t *testing.T) {
	_, p, _, st := settingsFixture(t)
	click(t, st, "密钥")
	click(t, st, "名称")
	st.Type("ui-key")
	click(t, st, "生成密钥")
	snap, _ := p.Snapshot()
	if len(snap.Keys) != 1 || snap.Keys[0].Name != "ui-key" || snap.Keys[0].Private != "" {
		t.Fatal(snap.Keys)
	}
	has(t, st, "ed25519 · 0 个主机使用")
	dblclick(t, st, "密钥名称 ui-key")
	st.Key(ui.Cmd, ui.KeyA)
	st.Type("renamed")
	st.Key(0, ui.KeyEnter)
	snap, _ = p.Snapshot()
	if snap.Keys[0].Name != "renamed" {
		t.Fatal("rename", snap.Keys[0].Name)
	}
	click(t, st, "删除")
	click(t, st, "删除密钥")
	snap, _ = p.Snapshot()
	if len(snap.Keys) != 0 {
		t.Fatal("key not deleted")
	}

	click(t, st, "代理")
	click(t, st, "新建代理")
	click(t, st, "名称")
	st.Type("socks")
	click(t, st, "主机")
	st.Type("127.0.0.1")
	click(t, st, "密码")
	st.Type("secret")
	click(t, st, "保存代理")
	snap, _ = p.Snapshot()
	if len(snap.Proxies) != 1 || snap.Proxies[0].Port != 1080 || snap.Proxies[0].Password != "" {
		t.Fatal(snap.Proxies)
	}
	click(t, st, "删除")
	click(t, st, "删除代理")
	snap, _ = p.Snapshot()
	if len(snap.Proxies) != 0 {
		t.Fatal("proxy not deleted")
	}
}

func TestCloudConflictNeedsConfirmationAndAgentConsent(t *testing.T) {
	_, p, tt, st := settingsFixture(t)
	id := "11111111-1111-4111-8111-111111111111"
	p.cloud = appui.CloudOverview{Workspaces: []storage.Workspace{{ID: id, Name: "团队"}}, Statuses: []cloudsync.Status{{WorkspaceID: id, State: "conflict"}}}
	p.events = append(p.events, appui.Event{WorkspaceID: id})
	tt.Frame()
	has(t, tt, "☁ 同步冲突")
	click(t, st, "同步")
	click(t, st, "团队")
	click(t, st, "使用云端版本…")
	if len(p.remote) != 0 {
		t.Fatal("silent overwrite")
	}
	click(t, st, "替换本机数据")
	if len(p.remote) != 1 || !p.remote[0] {
		t.Fatal("confirmed replace not sent", p.remote)
	}
	click(t, st, "登录口令")
	st.Type("auth-token")
	click(t, st, "创建工作区")
	if p.connects != 1 || strings.Contains(strings.Join(st.Texts(), " "), "auth-token") {
		t.Fatal("sensitive input retained")
	}
	for _, approve := range []bool{true, false} {
		p.agent = appui.AgentOverview{Pending: []agent.Pending{{ID: "r", Client: "client", Tool: "exec", Target: "s", Summary: "pwd", ExpiresAt: time.Now().Add(time.Minute)}}}
		tt.Frame()
		has(t, tt, "Agent 待批准 1")
		click(t, tt, "Agent 待批准")
		click(t, st, "Agent")
		label := map[bool]string{true: "批准", false: "拒绝"}[approve]
		click(t, st, label)
		if p.responses[len(p.responses)-1] != approve || len(p.sent) != 0 {
			t.Fatal("consent", p.responses)
		}
	}
}

func TestDiagnosticsRejectStaleResultsAndCancel(t *testing.T) {
	_, p, tt, st := settingsFixture(t)
	click(t, st, "网络诊断")
	click(t, st, "Ping")
	if p.diagnosed != "ping 127.0.0.1" {
		t.Fatal(p.diagnosed)
	}
	p.events = append(p.events, appui.Event{Diagnostic: &appui.DiagnosticResult{ID: "stale", Text: "old"}})
	tt.Frame()
	st.Frame()
	if st.HasText("old") {
		t.Fatal("stale result shown")
	}
	click(t, st, "停止")
	if !p.cancelled {
		t.Fatal("cancel")
	}
	p.events = append(p.events, appui.Event{Diagnostic: &appui.DiagnosticResult{ID: "current", Kind: "ping", Text: "permission denied"}})
	tt.Frame()
	st.Frame()
	has(t, st, "permission denied")
}

func TestImportPreviewOpensSettingsAndExportUsesChosenHosts(t *testing.T) {
	_, p, tt, st := settingsFixture(t)
	shown := 0
	saveHost(t, p.fakePlatform, model.Connection{ID: "h1", Name: "export-me", Host: "e", User: "u"})
	a := appui.New(p)
	a.ShowSettings = func() { shown++ }
	tt = ui.NewTester(a.View, 1280, 800)
	st = ui.NewTester(a.SettingsView, 880, 1400)
	p.events = append(p.events, appui.Event{Import: &appui.ImportPreview{Count: 2, Names: []string{"x", "y"}, Token: "tok"}})
	tt.Frame()
	st.Frame()
	if shown != 1 {
		t.Fatal("import preview did not open settings")
	}
	has(t, st, "将导入 2 个主机", "导入 2 个主机")
	click(t, st, "export-me  u@e")
	has(t, st, "导出 1 个主机")
	// Local paths come only from native panels.
	var picked []appui.FilePick
	a.PickFiles = func(o appui.FilePick, done func([]string)) { picked = append(picked, o); done([]string{"/out/hosts.json"}) }
	click(t, st, "选择保存到")
	has(t, st, "/out/hosts.json")
	if len(picked) != 1 || !picked[0].Save || picked[0].Title != "保存到" {
		t.Fatal(picked)
	}
	click(t, st, "清除保存到")
	if st.HasText("/out/hosts.json") {
		t.Fatal("clear")
	}
}
