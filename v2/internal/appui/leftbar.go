package appui

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

// leftColumn is the active session's card and its server's live status; ⌘B
// folds it to a strip that keeps the status dot.
func (a *App) leftColumn(c *ui.Context) {
	t := c.Theme()
	s := a.current()
	a.traceDrawer(c)
	if a.layout.leftHidden {
		ui.Column(c).Width(stripWidth).FillHeight().Padding(8, 0).Gap(10).AlignItems(ui.Center).Background(a.pane(t)).BorderColor(t.Border).BorderWidth(0, 1, 0, 0).Children(func() {
			if iconButtonTip(c, "展开左栏", "展开左栏 "+appShortcutLabel("B"), "panel-left").Clicked() {
				a.layout.leftHidden = false
			}
			if s != nil {
				label, color := a.sessionStatus(c, s)
				ui.Box(c).Size(10, 10).Radius(5).Background(color).Label(label).Tooltip(label)
			}
		})
		return
	}
	ui.Column(c).Width(leftWidth).FillHeight().Background(a.pane(t)).BorderColor(t.Border).BorderWidth(0, 1, 0, 0).Children(func() {
		if s == nil {
			ui.Row(c).FillWidth().Padding(8, 8, 0, 12).AlignItems(ui.Center).Children(func() {
				ui.Spacer(c).Grow(1)
				a.foldLeft(c)
			})
			ui.Column(c).Grow(1).FillWidth().Padding(0, 16).Center().Gap(4).Children(func() {
				muted(c, "未选择服务器")
				muted(c, "连接服务器后，这里显示实时状态").FontSize(11).Wrap()
			})
			return
		}
		a.sessionCard(c, s)
		ui.Scroll(c).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Padding(12).Gap(10).Children(func() { a.monitorSidebar(c, s) })
		})
		a.pingCard(c, s)
	})
}

func (a *App) foldLeft(c *ui.Context) {
	if iconButtonTip(c, "收起左栏", "收起左栏 "+appShortcutLabel("B"), "panel-left").Clicked() {
		a.layout.leftHidden = true
	}
}

// sessionStatus is the state the session card and the tab show.
func (a *App) sessionStatus(c *ui.Context, s *Session) (string, ui.Color) {
	t := c.Theme()
	id := s.Connection.ID
	switch {
	case !a.P.Exited(s.ID):
		if s.Local {
			return "运行中", t.Success
		}
		return "已连接", t.Success
	case s.Local:
		return "已退出", t.TextMuted
	case id != "" && a.hosts.connecting[id]:
		return "连接中", t.Warning
	case id != "" && a.hosts.failed[id]:
		return "失败", t.Danger
	}
	return "已断开", t.TextMuted
}

// reconnect reopens a session's tab; its card says 连接中 until the new session or a failure arrives.
func (a *App) reconnect(s *Session) {
	if id := s.Connection.ID; id != "" && !s.Local {
		a.hosts.connecting[id] = true
		delete(a.hosts.failed, id)
	}
	a.P.Reconnect(s)
}

// requestReconnect is a reconnect the user asked for in a menu: a session still alive is replaced, and what
// runs in it ends, so that asks first (a connected SSH tab as a close does, the 不再询问 preference
// included). A session that has ended reconnects at once.
func (a *App) requestReconnect(s *Session) {
	if a.P.Exited(s.ID) || !s.Local && a.Prefs.NoTabConfirm {
		a.reconnect(s)
		return
	}
	title, msg, action := "重新连接 "+a.tabTitle(s)+"？", "当前连接会断开，正在运行的命令会中断。", "断开并重新连接"
	if s.Local {
		title, msg, action = "重新打开 "+a.tabTitle(s)+"？", "终端里正在运行的程序会结束。", "结束并重新打开"
	}
	a.confirm(title, msg, action, func() {
		if a.tabIndex(s.ID) >= 0 { // the tab may have closed while the question was open
			a.reconnect(s)
		}
	})
}

func (a *App) sessionCard(c *ui.Context, s *Session) {
	t := c.Theme()
	label, color := a.sessionStatus(c, s)
	exited := a.P.Exited(s.ID)
	ui.Column(c).FillWidth().Padding(10, 8, 12, 12).Gap(6).BorderColor(t.Border).BorderWidth(0, 0, 1, 0).Children(func() {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(10, 10).Radius(5).Background(color)
			ui.Text(c, label).Bold().TextColor(color).Grow(1).SingleLine()
			a.foldLeft(c)
		})
		if s.Local {
			muted(c, "本地终端")
		} else {
			addr := s.Connection.User + "@" + s.Connection.Address()
			copied := time.Since(a.layout.copied) < 1500*time.Millisecond
			b := ui.ButtonBase(c).Label("复制地址 "+addr).Tooltip("点击复制").FillWidth().Padding(2, 0).Gap(6).Radius(4).AlignItems(ui.Center)
			if b.Hovered() {
				b.Background(t.SurfaceHover)
			}
			b.Children(func() {
				ui.Text(c, addr).Font("monospace").FontSize(12).SingleLine().Grow(1).MinWidth(0)
				if copied {
					c.After(1500 * time.Millisecond)
					ui.Text(c, "已复制").FontSize(11).TextColor(accentInk(t)).SingleLine()
				}
			})
			if b.Clicked() {
				a.P.CopyText(addr)
				a.layout.copied = time.Now()
			}
			info := strings.ToUpper(orDefault(s.Connection.Encoding, "utf-8"))
			if !exited {
				c.After(time.Minute)
				info += " · " + connectedFor(a.tabs.started[s.ID])
			}
			muted(c, info).SingleLine()
		}
		if title := a.P.TerminalTitle(s.ID); title != "" && !s.Local {
			muted(c, title).SingleLine().Tooltip(title) // the program's title, which the tab does not show
		}
		if exited && label != "连接中" {
			hint, action := "连接已断开 · 按回车重新连接", "重新连接"
			if s.Local {
				hint, action = "终端已退出 · 按回车重新打开", "重新打开"
			}
			muted(c, hint)
			if ui.Button(c, action).FillWidth().Clicked() {
				a.reconnect(s)
			}
		}
	})
}

// connectedFor is how long the session has been up: 刚刚连接, 已连接 5 分钟, 已连接 2 小时 3 分钟.
func connectedFor(at time.Time) string {
	d := time.Since(at)
	switch {
	case at.IsZero() || d < time.Minute:
		return "刚刚连接"
	case d < time.Hour:
		return fmt.Sprintf("已连接 %d 分钟", int(d.Minutes()))
	default:
		return fmt.Sprintf("已连接 %d 小时 %d 分钟", int(d.Hours()), int(d.Minutes())%60)
	}
}
