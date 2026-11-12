package appui_test

import (
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/agent"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/model"
	"nextshell.dev/v2/internal/remoteedit"
)

// activityPlatform has an agent at work and a live edit in each state.
type activityPlatform struct {
	*servicesPlatform
	live     []remoteedit.Live
	stopped  []string
	resolved []string
	halts    []bool
}

func (p *activityPlatform) LiveEdits() []remoteedit.Live { return p.live }
func (p *activityPlatform) StopLiveEdit(id string)       { p.stopped = append(p.stopped, id) }
func (p *activityPlatform) ResolveLiveEdit(id string, overwrite bool) {
	p.resolved = append(p.resolved, map[bool]string{true: "overwrite ", false: "reload "}[overwrite]+id)
}
func (p *activityPlatform) AgentHalt(h bool) { p.halts = append(p.halts, h); p.agent.Halted = h }

func newActivity(t *testing.T) (*activityPlatform, *ui.Tester) {
	t.Helper()
	p := &activityPlatform{servicesPlatform: &servicesPlatform{fakePlatform: newPlatform(t)}}
	now := time.Now()
	p.agent = appui.AgentOverview{URL: "http://127.0.0.1:7801/mcp",
		Pending: []agent.Pending{{ID: "r", Client: "claude", Tool: "exec", TargetName: "prod-web-01", Summary: "sudo systemctl restart nginx", Tier: "risky", Why: "sudo", ExpiresAt: now.Add(time.Minute)}},
		Activity: []agent.Activity{
			{Tool: "session_list", State: "completed", At: "10:41:58 UTC+8"},
			{Tool: "session_read", State: "completed", At: "10:42:01 UTC+8", Target: "prod-web-01", Summary: "session_read prod-web-01"},
			{Tool: "exec", State: "auto-approved", At: "10:42:03 UTC+8", Target: "prod-web-01", Summary: "df -h"},
			{Tool: "exec", State: "completed", At: "10:42:04 UTC+8", Target: "prod-web-01", Summary: "df -h"},
			{Tool: "exec", State: "failed", At: "10:42:09 UTC+8", Target: "db-master", Summary: "cat /var/log/postgresql/postgresql-16-main.log | tail -n 50"},
			{Tool: "exec", State: "pending", At: "10:42:12 UTC+8", Target: "prod-web-01", Summary: "sudo systemctl restart nginx"},
		}}
	p.live = []remoteedit.Live{
		{ID: "e1", Session: "s1", Path: "/etc/nginx/nginx.conf", State: remoteedit.Watching},
		{ID: "e2", Session: "s1", Path: "/etc/nginx/sites-enabled/default", State: remoteedit.Syncing},
		{ID: "e3", Session: "s1", Path: "/srv/app/config.yaml", State: remoteedit.Synced},
		{ID: "e4", Session: "s1", Path: "/etc/hosts", State: remoteedit.Conflict},
		{ID: "e5", Session: "s1", Path: "/root/.bashrc", State: remoteedit.Failed, Err: "sftp: \"permission denied\" (SSH_FX_PERMISSION_DENIED)"},
	}
	a := appui.New(p)
	tt := ui.NewTester(a.View, 1280, 800)
	session(t, p.fakePlatform, tt, "s1", "prod-web-01", model.Connection{Name: "prod-web-01", Host: "10.0.0.1", User: "root"})
	return p, tt
}

func TestActivityBarAgentFeedAndLiveEdits(t *testing.T) {
	p, tt := newActivity(t)
	has(t, tt, "Agent 待批准 1", "实时编辑 5 个文件")
	click(t, tt, "实时编辑 5 个文件")
	tt.SetSize(1280, 1300)
	has(t, tt, "监听中", "同步中", "已同步", "冲突", "出错", "权限不足", "df -h", "完成", "已自动批准", "失败", "sudo systemctl restart nginx")
	// Overwriting the server's file asks first, naming the server and the file; backing out changes nothing.
	click(t, tt, "覆盖远端")
	has(t, tt, "覆盖 prod-web-01 上的 hosts", "/etc/hosts")
	if len(p.resolved) != 0 {
		t.Fatal("overwrote on one click", p.resolved)
	}
	click(t, tt, "取消")
	if len(p.resolved) != 0 || tt.HasText("覆盖 prod-web-01 上的 hosts") {
		t.Fatal("cancel", p.resolved)
	}
	click(t, tt, "覆盖远端")
	click(t, tt, "覆盖")
	click(t, tt, "重新载入")
	click(t, tt, "断开 hosts")
	click(t, tt, "全部断开")
	if !slices.Equal(p.resolved, []string{"overwrite e4", "reload e4"}) || !slices.Equal(p.stopped, []string{"e4", ""}) {
		t.Fatal(p.resolved, p.stopped)
	}
	click(t, tt, "批准")
	if !slices.Equal(p.responses, []bool{true}) {
		t.Fatal("approval", p.responses)
	}
	click(t, tt, "紧急停止")
	has(t, tt, "恢复 Agent")
	click(t, tt, "恢复 Agent")
	if !slices.Equal(p.halts, []bool{true, false}) {
		t.Fatal("halt", p.halts)
	}
}
