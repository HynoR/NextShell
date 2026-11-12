package appui_test

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/agent"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/model"
)

// agentFixture is an app with one user tab ("one"), a recording RaiseWindow and a clock the test moves.
func agentFixture(t *testing.T) (a *appui.App, p *fakePlatform, tt *ui.Tester, raised *[]bool, now *time.Time) {
	t.Helper()
	a, p, tt = fixture(t)
	clock := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	a.Clock = func() time.Time { return clock }
	var r []bool
	a.RaiseWindow = func(focus bool) { r = append(r, focus) }
	session(t, p, tt, "one", "user-tab", remoteHost("one"))
	if a.Active != 0 {
		t.Fatal("fixture: the user's tab should be active")
	}
	return a, p, tt, &r, &clock
}

func agentTab(t *testing.T, id, title string) *appui.Session {
	return &appui.Session{ID: id, Title: title, Terminal: newTerminal(t, "$ "), Connection: remoteHost(id), ByAgent: true}
}

func TestFirstAgentActionRaisesOnceAndSaysSo(t *testing.T) {
	a, p, tt, raised, _ := agentFixture(t)
	has(t, tt) // user tab only; nothing about agents yet
	if tt.HasText("Agent 活动中") || tt.HasText("Agent 正在操作 NextShell") {
		t.Fatal("agent UI shown before any agent action")
	}
	p.events = append(p.events, appui.Event{Agent: &appui.AgentNotice{Tool: "exec"}})
	tt.Frame()
	if len(*raised) != 1 || (*raised)[0] {
		t.Fatalf("takeover must raise once without the keyboard: %v", *raised)
	}
	has(t, tt, "Agent 正在操作 NextShell", "管理", "Agent 活动中")
	click(t, tt, "管理")
	if p.settingsShown != 1 {
		t.Fatal("the notice's link must open Settings > Agent")
	}
	// Later actions: no raise, no second notice, nothing moves.
	for i := 0; i < 5; i++ {
		p.events = append(p.events, appui.Event{Agent: &appui.AgentNotice{Tool: "exec"}}, appui.Event{Agent: &appui.AgentNotice{Tool: "session_open", Pending: true}})
		tt.Frame()
	}
	if len(*raised) != 1 || a.Active != 0 {
		t.Fatal("later agent actions interrupted the user", *raised, a.Active)
	}
	// Each app run notices once: a new App has its own state.
	b, q, bt := fixture(t)
	var again int
	b.RaiseWindow = func(bool) { again++ }
	q.events = append(q.events, appui.Event{Agent: &appui.AgentNotice{Tool: "exec"}})
	bt.Frame()
	if again != 1 {
		t.Fatal(again)
	}
}

func TestAgentOpenedSessionsJoinInTheBackgroundWithABadge(t *testing.T) {
	a, p, tt, raised, _ := agentFixture(t)
	p.events = append(p.events, appui.Event{Agent: &appui.AgentNotice{Tool: "session_open"}}, appui.Event{Session: agentTab(t, "bg", "web-1")})
	tt.Frame()
	if len(a.Tabs) != 2 || a.Tabs[1].ID != "bg" || !a.Tabs[1].ByAgent || a.Active != 0 || a.Tabs[a.Active].ID != "one" {
		t.Fatalf("agent tab must be appended, not activated: tabs=%d active=%d", len(a.Tabs), a.Active)
	}
	has(t, tt, "web-1", "Agent") // the tab and its badge
	if len(*raised) != 1 {
		t.Fatal("opening a tab raised the window again", *raised)
	}
	// The user picks it up later by clicking it: normal behaviour from then on.
	click(t, tt, "web-1")
	if a.Active != 1 {
		t.Fatal(a.Active)
	}
	// Any number of further agent tabs stay in the background.
	p.events = append(p.events, appui.Event{Session: agentTab(t, "bg2", "web-2")}, appui.Event{Session: agentTab(t, "bg3", "web-3")})
	tt.Frame()
	if len(a.Tabs) != 4 || a.Active != 1 {
		t.Fatal(len(a.Tabs), a.Active)
	}
}

func TestAgentTabInAnEmptyWindowStillDoesNotTakeOver(t *testing.T) {
	a, p, tt := fixture(t)
	p.events = append(p.events, appui.Event{Session: agentTab(t, "bg", "web-1")})
	tt.Frame()
	tt.Frame()
	if len(a.Tabs) != 1 || a.Active != -1 {
		t.Fatalf("tabs=%d active=%d", len(a.Tabs), a.Active)
	}
	has(t, tt, "web-1")
	click(t, tt, "web-1")
	if a.Active != 0 {
		t.Fatal(a.Active)
	}
}

func TestReconnectReplacementOfAUsersTabKeepsToday(t *testing.T) {
	a, p, tt, _, _ := agentFixture(t)
	p.events = append(p.events, appui.Event{Session: agentTab(t, "bg", "web-1")})
	tt.Frame()
	click(t, tt, "web-1")
	if a.Active != 1 {
		t.Fatal(a.Active)
	}
	// "one" reconnects while the user looks at another tab: the replacement becomes active, as before.
	p.events = append(p.events, appui.Event{Session: &appui.Session{ID: "one2", Replaces: "one", Terminal: newTerminal(t, "$ "), Connection: remoteHost("one")}})
	tt.Frame()
	if a.Tabs[0].ID != "one2" || a.Active != 0 {
		t.Fatal(a.Tabs[0].ID, a.Active)
	}
}

func TestSessionFocusSelectsAndRaisesAtMostOncePerMinute(t *testing.T) {
	a, p, tt, raised, now := agentFixture(t)
	p.events = append(p.events, appui.Event{Session: agentTab(t, "bg", "web-1")}, appui.Event{Session: agentTab(t, "bg2", "web-2")})
	tt.Frame()
	focus := func(id string) {
		p.events = append(p.events, appui.Event{FocusID: id, Handover: true})
		tt.Frame()
	}
	// The window was never raised: the hand-over selects the tab and raises with the keyboard.
	focus("bg")
	if a.Active != 1 || len(*raised) != 1 || !(*raised)[0] {
		t.Fatal("first hand-over", a.Active, *raised)
	}
	// Within 60 s it only selects.
	*now = now.Add(30 * time.Second)
	focus("bg2")
	if a.Active != 2 || len(*raised) != 1 {
		t.Fatal("rate limit", a.Active, *raised)
	}
	*now = now.Add(29 * time.Second)
	focus("bg")
	if a.Active != 1 || len(*raised) != 1 {
		t.Fatal("still inside the minute", a.Active, *raised)
	}
	// After the minute it raises again.
	*now = now.Add(2 * time.Second)
	focus("bg2")
	if a.Active != 2 || len(*raised) != 2 || !(*raised)[1] {
		t.Fatal("after the minute", a.Active, *raised)
	}
	// A takeover notice counts as a raise for the limiter.
	b, q, bt := fixture(t)
	var n int
	clock := time.Now()
	b.Clock = func() time.Time { return clock }
	b.RaiseWindow = func(bool) { n++ }
	session(t, q, bt, "x", "x", remoteHost("x"))
	q.events = append(q.events, appui.Event{Agent: &appui.AgentNotice{Tool: "exec"}}, appui.Event{FocusID: "x", Handover: true})
	bt.Frame()
	if n != 1 {
		t.Fatal("takeover and hand-over in the same minute raised", n, "times")
	}
	// A plain focus (not from an agent) is untouched by the limiter.
	p.events = append(p.events, appui.Event{FocusID: "one"})
	tt.Frame()
	if a.Active != 0 || len(*raised) != 2 {
		t.Fatal(a.Active, *raised)
	}
}

func TestAgentActiveIndicatorFadesAfterTwoMinutes(t *testing.T) {
	_, p, tt, _, now := agentFixture(t)
	p.events = append(p.events, appui.Event{Agent: &appui.AgentNotice{Tool: "exec"}})
	tt.Frame()
	has(t, tt, "Agent 活动中")
	*now = now.Add(119 * time.Second)
	tt.Frame()
	has(t, tt, "Agent 活动中")
	*now = now.Add(2 * time.Second)
	tt.Frame()
	if tt.HasText("Agent 活动中") {
		t.Fatal("indicator did not fade")
	}
	// A pending approval takes the slot as a count.
	sp := &servicesPlatform{fakePlatform: newPlatform(t)}
	sp.agent = appui.AgentOverview{Pending: []agent.Pending{{ID: "r", Tool: "exec", Summary: "ls"}}}
	a2 := appui.New(sp)
	t2 := ui.NewTester(a2.View, 1280, 800)
	session(t, sp.fakePlatform, t2, "one", "one", remoteHost("one"))
	has(t, t2, "Agent 待批准 1")
}

func TestAgentCloseEventDropsTheTab(t *testing.T) {
	a, p, tt, _, _ := agentFixture(t)
	p.events = append(p.events, appui.Event{Session: agentTab(t, "bg", "web-1")})
	tt.Frame()
	p.events = append(p.events, appui.Event{CloseID: "bg"})
	tt.Frame()
	if len(a.Tabs) != 1 || a.Active != 0 || tt.HasText("web-1") {
		t.Fatal(len(a.Tabs), a.Active)
	}
	p.events = append(p.events, appui.Event{CloseID: "unknown"})
	tt.Frame() // harmless
}

func TestPendingApprovalShowsTierReasonTargetAndFullCommand(t *testing.T) {
	sp := &servicesPlatform{fakePlatform: newPlatform(t)}
	cmd := "curl -fsSL https://example.invalid/install.sh | sudo bash -s -- --yes --prefix=/opt/tool --channel=stable " + strings.Repeat("x", 160)
	sp.agent = appui.AgentOverview{URL: "http://127.0.0.1:41777/mcp", Pending: []agent.Pending{
		{ID: "risky", Client: "claude", Tool: "exec", Target: "3f2a9c-uuid", TargetName: "web-1", Summary: cmd, Reason: "install the monitoring agent", Tier: "risky", Why: "runs piped or generated code; runs with elevated privileges", ExpiresAt: time.Now().Add(time.Minute)},
		{ID: "env", Client: "claude", Tool: "exec", Target: "uuid-2", TargetName: "db-2", Summary: "cat /srv/app/.env", Tier: "sensitive", Why: "reads or writes a credential file", ExpiresAt: time.Now().Add(time.Minute)},
		{ID: "plain", Client: "claude", Tool: "session_focus", Target: "uuid-3", TargetName: "jump", Summary: "session_focus jump", ExpiresAt: time.Now().Add(time.Minute)},
	}}
	a := appui.New(sp)
	st := ui.NewTester(a.SettingsView, 880, 900)
	click(t, st, "Agent")
	has(t, st, "claude · exec", "风险操作", "敏感文件", "目标 web-1", "目标 db-2", "原因 install the monitoring agent", "cat /srv/app/.env", "即使开启自动批准")
	if st.HasText("3f2a9c-uuid") || st.HasText("uuid-2") {
		t.Fatal("the UUID is shown instead of the host or tab name")
	}
	found := false
	for _, s := range st.Texts() {
		found = found || s == cmd // the full command, never truncated
	}
	if !found {
		t.Fatal("the command was shortened")
	}
	badges := map[string]int{}
	for _, s := range st.Texts() {
		badges[s]++
	}
	if badges["风险操作"] != 1 || badges["敏感文件"] != 1 {
		t.Fatal("each tier badge appears once, on its own request", badges["风险操作"], badges["敏感文件"])
	}
	// The status bar's entry says what waits: count in the label, details in the tooltip helper.
	tt := ui.NewTester(a.View, 1280, 800)
	session(t, sp.fakePlatform, tt, "one", "one", remoteHost("one"))
	has(t, tt, "Agent 待批准 3")
	_ = model.Connection{}
}
