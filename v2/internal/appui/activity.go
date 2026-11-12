package appui

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/remoteedit"
)

// activityColumn is the right column: what runs beside the terminal (transfers,
// agents, live edits). Folded, it is a strip of icons with counts.
func (a *App) activityColumn(c *ui.Context) {
	t := c.Theme()
	jobs := a.P.Transfers()
	active, failed := activeTransfers(jobs), failedTransfers(jobs)
	pending := len(a.P.AgentOverview().Pending)
	agentLabel := "Agent 活动"
	switch {
	case pending > 0:
		agentLabel = fmt.Sprintf("Agent 待批准 %d", pending)
	case a.agentActive():
		c.After(a.agent.lastAt.Add(agentActiveFor).Sub(a.now()) + time.Second)
		agentLabel = "Agent 活动中"
	}
	if active > 0 {
		c.After(time.Second)
	}
	edits := a.P.LiveEdits()
	editColor, editsBusy := t.Accent, false
	for _, l := range edits {
		editsBusy = editsBusy || l.State == remoteedit.Syncing
		if l.State == remoteedit.Conflict || l.State == remoteedit.Failed {
			editColor = t.Danger
		}
	}
	editLabel := "实时编辑"
	if len(edits) > 0 {
		editLabel = fmt.Sprintf("实时编辑 %d 个文件", len(edits))
	}
	if !a.layout.rightOpen {
		ui.Column(c).Width(stripWidth).FillHeight().Padding(8, 0).Gap(8).AlignItems(ui.Center).Background(a.pane(t)).BorderColor(t.Border).BorderWidth(0, 0, 0, 1).Children(func() {
			open := iconButton(c, "展开活动栏", "panel-right").Clicked()
			ui.Box(c).Width(20).Height(1).Background(t.Border)
			// A failure is what to look at: it takes the badge, in red, until it is retried or cleared.
			transfers, badge, badgeColor := "传输队列", active, t.Accent
			switch {
			case failed > 0 && active > 0:
				transfers = fmt.Sprintf("%d 个传输失败，%d 个进行中", failed, active)
			case failed > 0:
				transfers = fmt.Sprintf("%d 个传输失败", failed)
			case active > 0:
				transfers = fmt.Sprintf("%d 个传输进行中", active)
			}
			if failed > 0 {
				badge, badgeColor = failed, t.Danger
			}
			for _, v := range []struct {
				label, icon string
				count       int
				color       ui.Color
				lit         bool
			}{{transfers, "transfer", badge, badgeColor, active > 0}, {agentLabel, "agent", pending, t.Warning, agentLabel == "Agent 活动中"}, {editLabel, "edit", len(edits), editColor, editsBusy}} {
				open = stripButton(c, v.label, v.icon, v.count, v.color, v.lit) || open
			}
			if open {
				a.layout.rightOpen = true
			}
		})
		return
	}
	ui.Column(c).Width(rightWidth).FillHeight().Background(a.pane(t)).BorderColor(t.Border).BorderWidth(0, 0, 0, 1).Children(func() {
		ui.Row(c).FillWidth().Padding(8, 8, 4, 12).AlignItems(ui.Center).Children(func() {
			ui.Spacer(c).Grow(1)
			if iconButton(c, "收起活动栏", "panel-right").Clicked() {
				a.layout.rightOpen = false
			}
		})
		ui.Scroll(c).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Padding(0, 12, 12, 12).Gap(18).Children(func() {
				a.transferSection(c)
				a.agentSection(c)
				a.liveEditSection(c, edits)
			})
		})
	})
}

// agentSection is the agent's live log: what waits for approval, an emergency stop and the recent calls.
func (a *App) agentSection(c *ui.Context) {
	t := c.Theme()
	state := a.P.AgentOverview()
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			sectionLabel(c, "Agent 活动").Grow(1)
			switch {
			case state.URL == "":
			case state.Halted:
				if ghost(c, "恢复 Agent", "").Clicked() {
					a.P.AgentHalt(false)
				}
			default:
				stop := ui.ButtonBase(c).Label("紧急停止").Tooltip("拒绝所有待批准请求、关闭自动批准并中止正在运行的命令").Padding(4, 8).Radius(t.Radius).Background(t.Danger.Alpha(.12))
				stop.Children(func() { ui.Text(c, "紧急停止").FontSize(12).Bold().TextColor(t.Danger) })
				if stop.Clicked() {
					a.P.AgentHalt(true)
					a.toast("已紧急停止 Agent")
				}
			}
		})
		switch {
		case state.URL == "":
			muted(c, "MCP 服务未开启")
		case state.Halted:
			ui.Text(c, "已紧急停止 · Agent 的请求都会被拒绝").FontSize(12).TextColor(t.Danger)
		}
		for _, p := range state.Pending {
			a.pendingCard(c, p)
		}
		shown := 0
		for i := len(state.Activity) - 1; i >= 0 && shown < 10; i-- {
			ev := state.Activity[i]
			if ev.Tool == "global" || ev.State == "pending" { // pending requests are the cards above
				continue
			}
			shown++
			color := t.TextMuted
			switch ev.State {
			case "failed":
				color = t.Danger
			case "user_action_required":
				color = t.Warning
			}
			what := orDefault(agentTools[ev.Tool], ev.Tool)
			switch ev.Tool {
			case "exec":
				what = orDefault(ev.Summary, what)
			case "session_send_text":
				what += " " + ev.Summary
			}
			ui.Column(c.Key(fmt.Sprint("agent-call-", i))).FillWidth().Gap(2).Tooltip(what).Children(func() {
				ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Text(c, strings.Fields(ev.At)[0]).Font("monospace").FontSize(11).TextColor(t.TextMuted)
					ui.Text(c, orDefault(ev.Target, "—")).FontSize(12).SingleLine().Grow(1).MinWidth(0)
					ui.Text(c, orDefault(agentStates[ev.State], ev.State)).FontSize(11).TextColor(color)
				})
				ui.Text(c, what).Font("monospace").FontSize(11).SingleLine().FillWidth().MinWidth(0)
			})
		}
		if shown == 0 && len(state.Pending) == 0 && state.URL != "" {
			muted(c, "最近没有 Agent 操作")
		}
		if ghost(c, "设置 → Agent", "agent").Clicked() {
			a.openSettings("Agent")
		}
	})
}

// agentTools names what each agent tool does; exec shows its command instead.
var agentTools = map[string]string{"session_list": "列出会话", "host_list": "列出主机", "command_search": "搜索命令库", "session_history": "读取历史",
	"session_read": "读取终端", "session_send_text": "输入", "session_send_signal": "发送中断等信号", "session_open": "打开连接", "session_close": "关闭会话",
	"session_focus": "交还会话", "command_save": "保存到命令库", "agent_status": "查询状态", "request_cancel": "撤回请求", "exec": "执行命令"}

var liveStates = map[remoteedit.LiveState]string{remoteedit.Watching: "监听中", remoteedit.Syncing: "同步中", remoteedit.Synced: "已同步", remoteedit.Conflict: "冲突", remoteedit.Failed: "出错"}

// liveEditSection lists the files open in an outside editor; each save goes to the server until stopped.
func (a *App) liveEditSection(c *ui.Context, edits []remoteedit.Live) {
	t := c.Theme()
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			sectionLabel(c, "实时编辑").Grow(1)
			if len(edits) > 0 && ghost(c, "全部断开", "").Clicked() {
				a.P.StopLiveEdit("")
			}
		})
		if len(edits) == 0 {
			muted(c, "暂无实时编辑。在 SFTP 里右键文件，选「远程编辑」。")
		}
		for _, l := range edits {
			color := map[remoteedit.LiveState]ui.Color{remoteedit.Watching: t.TextMuted, remoteedit.Syncing: accentInk(t), remoteedit.Synced: t.Success, remoteedit.Conflict: t.Warning, remoteedit.Failed: t.Danger}[l.State]
			ui.Column(c.Key(l.ID)).FillWidth().Gap(4).Padding(8).Radius(t.Radius).Background(t.Background).Border(1, t.Border).Children(func() {
				ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, path.Base(l.Path)).FontSize(12).Bold().SingleLine().Grow(1).MinWidth(0).Tooltip(l.Path)
					ui.Text(c, liveStates[l.State]).FontSize(11).Bold().TextColor(color)
					if iconButton(c, "断开 "+path.Base(l.Path), "close").Clicked() {
						a.P.StopLiveEdit(l.ID)
					}
				})
				muted(c, a.sessionTitle(l.Session)+" · "+path.Dir(l.Path)).FontSize(11).SingleLine()
				switch l.State {
				case remoteedit.Conflict:
					ui.Text(c, "服务器上的文件已被别处修改，本次保存未上传").FontSize(12).TextColor(t.Warning)
					ui.Row(c).FillWidth().Gap(4).Children(func() {
						if ghost(c, "重新载入", "").Clicked() {
							a.P.ResolveLiveEdit(l.ID, false)
						}
						if ghost(c, "另存本地", "").Clicked() {
							id := l.ID
							a.pick(FilePick{Title: "另存本地", DefaultPath: filepath.Join(a.Prefs.DownloadDir, path.Base(l.Path)), Save: true}, func(p []string) {
								a.report(a.P.SaveLiveEditCopy(id, p[0]), "已另存到 "+p[0])
							})
						}
						// The one way out that loses what someone else wrote: red, and asked first.
						if ghost(c, "覆盖远端", "").TextColor(t.Danger).Clicked() {
							id, host := l.ID, a.sessionTitle(l.Session)
							a.later(func() {
								a.confirm("覆盖 "+host+" 上的 "+path.Base(l.Path), fmt.Sprintf("%s\n%s 上的这个文件在你编辑期间被别处修改过。覆盖后，本地副本会替换它，别处的修改将丢失。", l.Path, host), "覆盖", func() { // danger
									a.P.ResolveLiveEdit(id, true)
								})
							})
						}
					})
				case remoteedit.Failed:
					ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
						title, _ := humanize(l.Err)
						ui.Text(c, title).FontSize(12).TextColor(t.Danger).SingleLine().Grow(1).MinWidth(0)
						if ghost(c, "详情", "").Clicked() {
							a.dialogs.detail, a.dialogs.detailIn = l.Err, mainWindow
						}
					})
				}
			})
		}
	})
}

// sessionTitle is the tab title of session id, for lists that name a host.
func (a *App) sessionTitle(id string) string {
	if i := a.tabIndex(id); i >= 0 {
		return a.tabTitle(a.Tabs[i])
	}
	return "已关闭的会话"
}

// stripButton is an icon of the folded column with its count; lit marks activity.
func stripButton(c *ui.Context, label, icon string, count int, color ui.Color, lit bool) bool {
	t := c.Theme()
	b := ui.ButtonBase(c).Label(label).Tooltip(label).Size(28, 28).Radius(t.Radius).Center().TextColor(t.TextMuted)
	if lit {
		b.TextColor(accentInk(t))
	}
	if b.Hovered() {
		b.Background(t.SurfaceHover)
	}
	b.Children(func() {
		ui.Icon(c, appIcons[icon]).FontSize(16)
		if count > 0 {
			ui.Text(c, fmt.Sprint(count)).FontSize(9).Bold().TextColor(t.AccentText).Padding(0, 4).Radius(7).Background(color).Absolute().Top(-3).Right(-5)
		}
	})
	return b.Clicked()
}
