package appui

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// titlebar is the window's hidden title bar: quick connect in the middle, the
// auto-approve warning, sync, and the connection manager and settings.
func (a *App) titlebar(c *ui.Context) {
	t := c.Theme()
	bar := c.TitleBar()
	ui.Row(c).FillWidth().Height(max(bar.Height, 44)).Padding(0, bar.Right+10, 0, bar.Left+10).Gap(8).AlignItems(ui.Center).Background(t.Surface).BorderColor(t.Border).BorderWidth(0, 0, 1, 0).DragWindow().Children(func() {
		ui.Spacer(c).Grow(1).Basis(0).MinWidth(0)
		search := ui.ButtonBase(c).Label("快速连接服务器").Width(380).MinWidth(180).Shrink(1).Height(28).Padding(0, 10).Gap(8).Radius(t.Radius).AlignItems(ui.Center).Background(t.Background).Border(1, t.Border)
		if search.Hovered() {
			search.Border(1, t.TextMuted)
		}
		search.Children(func() {
			ui.Icon(c, appIcons["search"]).FontSize(14).TextColor(t.TextMuted)
			ui.Text(c, "快速连接服务器…").FontSize(12).TextColor(t.TextMuted).Grow(1).MinWidth(0).SingleLine()
			ui.Text(c, shortcutLabel("K")).FontSize(11).TextColor(t.TextMuted).Padding(1, 5).Radius(4).Border(1, t.Border)
		})
		if search.Clicked() {
			a.palette.show()
		}
		a.paletteView(c, search)
		ui.Row(c).Grow(1).Basis(0).MinWidth(0).Gap(6).Justify(ui.End).AlignItems(ui.Center).Children(func() {
			a.autoApprovePill(c)
			a.syncIcon(c)
			if titleButton(c, "服务器", "server").Clicked() {
				a.openManager()
			}
			if titleButton(c, "设置", "settings").Clicked() {
				a.openSettings("")
			}
		})
	})
}

func titleButton(c *ui.Context, label, icon string) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c).Label(label).Height(28).Padding(0, 10).Gap(6).Radius(t.Radius).AlignItems(ui.Center).Border(1, t.Border).Shrink(0)
	if b.Hovered() {
		b.Background(t.SurfaceHover)
	}
	b.Children(func() {
		ui.Icon(c, appIcons[icon]).FontSize(14).TextColor(t.TextMuted)
		ui.Text(c, label).FontSize(12).SingleLine()
	})
	return b
}

// autoApprovePill warns while agent calls run without approval; it never hides then.
func (a *App) autoApprovePill(c *ui.Context) {
	auto := a.P.AgentAutoApproval()
	if !auto.On {
		return
	}
	t := c.Theme()
	c.After(30 * time.Second)
	b := ui.ButtonBase(c).Label("自动批准中").Height(24).Padding(0, 8).Radius(4).AlignItems(ui.Center).Background(t.Warning.Alpha(.18)).Shrink(0).Children(func() {
		ui.Text(c, "⚠ 自动批准中 · "+autoApproveLabel(auto)).FontSize(12).Bold().TextColor(t.Warning).SingleLine()
	})
	if b.Clicked() {
		a.openSettings("Agent")
	}
}

// syncIcon shows the cloud sync state as an icon; its label says the state.
func (a *App) syncIcon(c *ui.Context) {
	label, bad := a.syncLabel()
	if label == "" {
		return
	}
	t := c.Theme()
	b := iconButton(c, label, "cloud")
	if bad {
		b.TextColor(t.Danger)
	}
	if b.Clicked() {
		a.openSettings("同步")
	}
}

// syncLabel reads the profile at most every two seconds: the title bar builds every frame.
func (a *App) syncLabel() (string, bool) {
	if !a.Cloud {
		return "", false
	}
	if time.Since(a.sync.at) > 2*time.Second {
		a.sync.label, a.sync.at = a.syncState(), time.Now()
	}
	return a.sync.label, a.sync.label == "同步冲突" || a.sync.label == "同步出错"
}
func (a *App) syncState() string {
	o := a.P.CloudOverview()
	if len(o.Workspaces) == 0 {
		return ""
	}
	for _, s := range o.Statuses {
		switch s.State {
		case "conflict":
			return "同步冲突"
		case "error":
			return "同步出错"
		}
	}
	for _, s := range o.Statuses {
		if s.State != "disconnected" {
			return "已同步"
		}
	}
	return "同步：未登录"
}
