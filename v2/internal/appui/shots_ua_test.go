package appui_test

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/appui"
)

// TestScreenshotsUA renders the screens of the shell fixes: the command bar and its target, the tab menu
// of a live session with the question reconnecting asks, and the toast of an agent request waiting.
func TestScreenshotsUA(t *testing.T) {
	shot := shooter(t)
	p := newPlatform(t)
	web, db := demo(t, p)
	a := appui.New(p)
	a.ShowSettings = func() { p.settingsShown++ }
	// The window draws its menus (the app lets the system do it), so that the tab menu is in the picture.
	tt := ui.NewTester(func(c *ui.Context) {
		c.SetMenuPresentation(ui.PopoverMenus)
		a.View(c)
	}, 1280, 800)
	tt.SetTitleBar(ui.TitleBar{Height: 44, Left: 72})
	one := session(t, p, tt, "s1", "prod-web-01", web)
	session(t, p, tt, "s2", "db-master", db)
	a.Active = 0
	one.Terminal.Feed([]byte("\r\nroot@prod-web-01:~# uptime\r\n 10:42:01 up 12 days,  3:02,  1 user,  load average: 0.42, 0.38, 0.35\r\nroot@prod-web-01:~# "))
	now := time.Now()
	for i := range 12 {
		p.events = append(p.events, appui.Event{Monitor: &appui.MonitorResult{Session: "s1", Snapshot: monitorSnapshot(now.Add(time.Duration(i-12)*2*time.Second), i), Latency: 18 * time.Millisecond}})
	}
	tt.Frame()

	// A line typed for prod-web-01 waits in its tab; db-master's bar is its own.
	click(t, tt, "命令输入")
	tt.Type("sudo systemctl restart nginx")
	shot(tt, "ua-1-command-bar-target")
	click(t, tt, "db-master")
	shot(tt, "ua-1b-command-bar-other-tab")
	click(t, tt, "prod-web-01")

	// The tab menu of a live session, and the question 重新连接 asks.
	if e := tt.RightClick("prod-web-01"); e != nil {
		t.Fatal(e)
	}
	shot(tt, "ua-2-tab-menu-live")
	if e := tt.ChooseMenuItem("重新连接"); e != nil {
		t.Fatal(e)
	}
	shot(tt, "ua-2b-reconnect-confirm")
	click(t, tt, "取消")

	// An agent request waits for approval: a toast, and nothing else moves. (The one-time takeover notice
	// comes first and is gone by then.)
	p.events = append(p.events, appui.Event{Agent: &appui.AgentNotice{Tool: "exec"}})
	tt.Frame()
	for range 20 {
		tt.Advance(time.Second)
		tt.Frame()
	}
	p.events = append(p.events, appui.Event{Agent: &appui.AgentNotice{Tool: "exec", Pending: true}})
	for range 6 {
		tt.Advance(150 * time.Millisecond)
		tt.Frame()
	}
	shot(tt, "ua-3-agent-pending-toast")
}
