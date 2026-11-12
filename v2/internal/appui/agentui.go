package appui

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// An agent works beside the user, so its actions must not interrupt: after one notice per run it never
// raises the window, switches tabs or takes the keyboard. The exception is session_focus, the
// explicit hand-over, which selects the tab and raises the window at most once a minute.
const (
	agentActiveFor = 2 * time.Minute
	raiseEvery     = 60 * time.Second
)

type agentUI struct {
	noticed    bool      // the one-time takeover notice has been shown
	lastAt     time.Time // last agent call that changed something
	lastRaise  time.Time // last time the window was raised for the agent
	badgedOpen int       // tabs opened by agents so far (for tests and the tooltip)
}

func (a *App) now() time.Time {
	if a.Clock != nil {
		return a.Clock()
	}
	return time.Now()
}

// raise brings the window forward; focus asks for the keyboard too, which only a hand-over does.
func (a *App) raise(focus bool) {
	a.agent.lastRaise = a.now()
	if a.RaiseWindow != nil {
		a.RaiseWindow(focus)
	}
}

// agentActed records an agent call and, the first time in a run, tells the user once.
func (a *App) agentActed(n *AgentNotice) {
	a.agent.lastAt = a.now()
	if n.Pending {
		// The agent waits for a decision: say so, passively (no raise, no tab switch, no keyboard). 查看 opens
		// the activity column, where the request can be approved; a later request replaces this toast.
		a.addToast(ui.Toast{ID: "agent-pending", Title: "Agent 请求等待批准", Description: "在右侧活动栏的「Agent 活动」里批准或拒绝", Timeout: 10 * time.Second, Action: "查看", OnAction: func() { a.layout.rightOpen = true }})
	}
	if a.agent.noticed {
		return
	}
	a.agent.noticed = true
	a.raise(false) // front, never the keyboard
	a.addToast(ui.Toast{ID: "agent-takeover", Title: "Agent 正在操作 NextShell", Description: "之后不会再打断你的操作；可随时在设置里关闭或紧急停止", Timeout: 10 * time.Second, Action: "管理", OnAction: func() { a.openSettings("Agent") }})
}

// agentActive reports recent agent activity, for the status bar.
func (a *App) agentActive() bool {
	return !a.agent.lastAt.IsZero() && a.now().Sub(a.agent.lastAt) < agentActiveFor
}

// handover is session_focus: the tab is selected and, at most once a minute, the window comes forward.
func (a *App) handover(id string) {
	i := a.tabIndex(id)
	if i < 0 {
		return
	}
	if a.agent.lastRaise.IsZero() || a.now().Sub(a.agent.lastRaise) >= raiseEvery {
		a.activateTab(i)
		a.raise(true)
		return
	}
	a.Active = i
}

// addAgentTab adds a tab an agent opened: appended in the background, the active tab and keyboard untouched.
func (a *App) addAgentTab(s *Session) {
	a.Tabs = append(a.Tabs, s)
	a.agent.badgedOpen++
	a.tabs.mru.Sync(a.tabIDs(), a.activeID())
}

// agentBadge is the small mark on a tab an agent opened.
func (a *App) agentBadge(c *ui.Context, s *Session) {
	if !s.ByAgent {
		return
	}
	t := c.Theme()
	ui.Row(c).Padding(0, 4).Radius(3).Background(t.Warning.Alpha(.18)).Tooltip("由 Agent 打开").Children(func() {
		ui.Text(c, "Agent").FontSize(10).Bold().TextColor(t.Warning)
	})
}
