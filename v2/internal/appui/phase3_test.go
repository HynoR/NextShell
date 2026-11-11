package appui_test

import (
	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/agent"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/cloudsync"
	"nextshell.dev/v2/internal/storage"
	"strings"
	"testing"
	"time"
)

type phase3Platform struct {
	*fakePlatform
	cloud     appui.CloudOverview
	agent     appui.AgentOverview
	remote    []bool
	responses []bool
	sends     int
	connects  int
}

func (p *phase3Platform) CloudOverview() appui.CloudOverview { return p.cloud }
func (p *phase3Platform) AgentOverview() appui.AgentOverview { return p.agent }
func (p *phase3Platform) CloudSync(id string, remote bool)   { p.remote = append(p.remote, remote) }
func (p *phase3Platform) CloudConnect(endpoint, id, name, auth, encryption string, join bool) {
	p.connects++
}
func (p *phase3Platform) AgentRespond(id string, approve bool) {
	p.responses = append(p.responses, approve)
	p.agent.Pending = nil
}
func (p *phase3Platform) Send(id, text string) error { p.sends++; return nil }
func phase3Fixture(t *testing.T) (*appui.App, *phase3Platform, *ui.Tester) {
	a, p, _ := fixture(t)
	fp := &phase3Platform{fakePlatform: p}
	a.P = fp
	tt := ui.NewTester(a.View, 1600, 1800)
	return a, fp, tt
}
func TestPhase3NativeNavigationShellPreparationNoAutomaticInput(t *testing.T) {
	a, p, tt := phase3Fixture(t)
	for _, page := range []struct{ button, title string }{{"云同步", "云同步 · 协议 v2（本地参考服务）"}, {"Agent 网关", "Agent 网关 · 本地 MCP"}, {"Shell 集成", "Shell 集成 · 官方 terminal API"}} {
		click(t, tt, page.button)
		if !tt.HasText(page.title) {
			t.Fatal(page, tt.Texts())
		}
	}
	click(t, tt, "将 bash OSC 7 钩子放入工作台")
	if !strings.Contains(a.Command, "__nextshell_emit_cwd") || p.sends != 0 {
		t.Fatal("hook not prepared, or auto-executed", p.sends)
	}
	if !tt.HasText("OSC 133：deferred, waiting on upstream；无自写流观察器。前台执行/命令标记历史继续留空。") {
		t.Fatal("deferred capability hidden")
	}
}
func TestCloudConflictUIRequiresExplicitConfirmation(t *testing.T) {
	a, p, tt := phase3Fixture(t)
	id := "11111111-1111-4111-8111-111111111111"
	p.cloud = appui.CloudOverview{Workspaces: []storage.Workspace{{ID: id, Name: "test"}}, Statuses: []cloudsync.Status{{WorkspaceID: id, State: "conflict"}}}
	a.Phase3.Workspace = id
	a.Page = "cloud"
	a.ShowWorkbench = false
	tt.Frame()
	click(t, tt, "采用远端版本")
	if len(p.remote) != 0 {
		t.Fatal("silent conflict overwrite")
	}
	a.Phase3.ConfirmRemote = true
	tt.Frame()
	click(t, tt, "采用远端版本")
	if len(p.remote) != 1 || !p.remote[0] || a.Phase3.ConfirmRemote {
		t.Fatal("confirmed conflict was not sent", p.remote)
	}
	a.Phase3.Auth = "authentication-token"
	a.Phase3.Encryption = "different encryption password"
	tt.Frame()
	click(t, tt, "创建并登录工作区")
	if p.connects != 1 || a.Phase3.Auth != "" || a.Phase3.Encryption != "" {
		t.Fatal("sensitive inputs retained")
	}
}
func TestAgentConsentUIApprovesOrDeniesOnlySelectedRequest(t *testing.T) {
	for _, approve := range []bool{true, false} {
		t.Run(map[bool]string{true: "approve", false: "deny"}[approve], func(t *testing.T) {
			a, p, tt := phase3Fixture(t)
			a.Page = "agent"
			a.ShowWorkbench = false
			p.agent = appui.AgentOverview{Pending: []agent.Pending{{ID: "request", Client: "test client", Tool: "exec", Target: "session", Summary: "pwd", ExpiresAt: time.Now().Add(time.Minute)}}}
			tt.Frame()
			label := "拒绝此请求"
			if approve {
				label = "批准此请求"
			}
			click(t, tt, label)
			if len(p.responses) != 1 || p.responses[0] != approve {
				t.Fatal("wrong approval", p.responses)
			}
			if p.sends != 0 {
				t.Fatal("UI approval executed terminal input")
			}
		})
	}
}
