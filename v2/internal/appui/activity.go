package appui

import (
	"cmp"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/remoteedit"
	"nextshell.dev/v2/internal/transfer"
)

// activityColumn is the right column: what runs beside the terminal (agents,
// transfers, live edits). Folded, it is a strip of icons with counts.
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
			}{
				{agentLabel, "agent", pending, t.Warning, agentLabel == "Agent 活动中"},
				{transfers, "transfer", badge, badgeColor, active > 0},
				{editLabel, "edit", len(edits), editColor, editsBusy},
			} {
				open = stripButton(c, v.label, v.icon, v.count, v.color, v.lit) || open
			}
			if open {
				a.layout.rightOpen = true
			}
		})
		return
	}
	ui.Column(c).Width(rightWidth).FillHeight().Background(a.pane(t)).BorderColor(t.Border).BorderWidth(0, 0, 0, 1).Children(func() {
		ui.Row(c).FillWidth().Height(36).Padding(0, 8, 0, 12).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "活动").Bold().Grow(1)
			if iconButton(c, "收起活动栏", "panel-right").Clicked() {
				a.layout.rightOpen = false
			}
		})
		ui.Scroll(c).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Padding(4, 8, 12, 12).Children(func() {
				a.agentSection(c)
				activityDivider(c)
				a.transferSection(c)
				activityDivider(c)
				a.liveEditSection(c, edits)
			})
		})
	})
}

// activityDivider is the hairline between activity sections.
func activityDivider(c *ui.Context) {
	ui.Box(c).FillWidth().Height(1).Margin(8, 0).Background(c.Theme().Border)
}

// activitySection is one block of the activity column. The header (chevron, title, count, actions)
// collapses the body; open lasts for this run only. count is hidden at 0.
func (a *App) activitySection(c *ui.Context, title string, open *bool, count int, countColor ui.Color, actions func(), body func()) {
	t := c.Theme()
	ui.Column(c.Key("activity-" + title)).FillWidth().Gap(8).Children(func() {
		var parts ui.CollapsibleParts
		ui.Row(c).FillWidth().MinHeight(28).AlignItems(ui.Stretch).Gap(2).Children(func() {
			parts = ui.CollapsibleBase(c, open)
			tr := parts.Trigger.Grow(1).MinWidth(0).Gap(4).AlignItems(ui.Center).Padding(2, 4).Radius(t.Radius).Label(title)
			if tr.Hovered() {
				tr.Background(t.SurfaceHover)
			}
			tr.Children(func() {
				icon := "chevron-right"
				if *open {
					icon = "chevron-down"
				}
				ui.Icon(c, appIcons[icon]).FontSize(12).TextColor(t.TextMuted).Shrink(0)
				sectionLabel(c, title).MinWidth(0)
				if count > 0 {
					if countColor == (ui.Color{}) {
						countColor = t.TextMuted
					}
					ui.Text(c, fmt.Sprint(count)).FontSize(11).Bold().TextColor(countColor).Shrink(0)
				}
				ui.Spacer(c).Grow(1)
			})
			if actions != nil {
				actions()
			}
		})
		parts.Panel(body).FillWidth().Gap(8)
	})
}

// moreMenu is a section's ⋯. label names the button; build adds the actions that apply.
func moreMenu(c *ui.Context, label string, build func(*ui.Menu)) {
	iconButtonTip(c, label, "更多操作", "more").PopoverMenuButton(build, ui.PopoverMenuButtonOptions{At: ui.AnchorBottomEnd, Self: ui.AnchorTopEnd})
}

// agentSection is the agent's live log: what waits for approval, an emergency stop and the recent calls.
func (a *App) agentSection(c *ui.Context) {
	t := c.Theme()
	state := a.P.AgentOverview()
	var actions func()
	if state.URL != "" {
		actions = func() {
			switch {
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
		}
	}
	a.activitySection(c, "Agent 活动", &a.activityOpen.agent, len(state.Pending), t.Warning, actions, func() {
		quiet := len(state.Pending) == 0
		for i := len(state.Activity) - 1; quiet && i >= 0; i-- {
			ev := state.Activity[i]
			quiet = ev.Tool == "global" || ev.State == "pending"
		}
		// Nothing else to show: the off-state and the way to the settings share one line.
		if state.URL == "" && quiet {
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				muted(c, "MCP 服务未开启").SingleLine().Grow(1).MinWidth(0)
				if ghost(c, "设置 → Agent", "agent").Clicked() {
					a.openSettings("Agent")
				}
			})
			return
		}
		switch {
		case state.URL == "":
			muted(c, "MCP 服务未开启")
		case state.Halted:
			ui.Text(c, "已紧急停止 · 拒绝所有请求").FontSize(12).TextColor(t.Danger)
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
		if quiet && state.URL != "" {
			muted(c, "暂无操作记录")
		}
		if ghost(c, "设置 → Agent", "agent").Clicked() {
			a.openSettings("Agent")
		}
	})
}

// agentTools names what each agent tool does; exec shows its command instead.
var agentTools = map[string]string{"session_list": "列出会话", "host_list": "列出主机", "command_search": "搜索命令库", "session_history": "读取历史",
	"session_read": "读取终端", "session_send_text": "输入", "session_send_signal": "发送控制信号", "session_open": "打开连接", "session_close": "关闭会话",
	"session_focus": "交还会话", "command_save": "保存到命令库", "agent_status": "查询状态", "request_cancel": "撤回请求", "exec": "执行命令"}

var liveStates = map[remoteedit.LiveState]string{remoteedit.Watching: "监听中", remoteedit.Syncing: "同步中", remoteedit.Synced: "已同步", remoteedit.Conflict: "冲突", remoteedit.Failed: "出错"}

// liveEditSection lists the files open in an outside editor; each save goes to the server until stopped.
func (a *App) liveEditSection(c *ui.Context, edits []remoteedit.Live) {
	t := c.Theme()
	color := t.TextMuted
	for _, l := range edits {
		if l.State == remoteedit.Conflict || l.State == remoteedit.Failed {
			color = t.Danger
			break
		}
	}
	var actions func()
	if len(edits) > 0 {
		actions = func() {
			moreMenu(c, "编辑操作", func(m *ui.Menu) {
				if m.Item("全部断开").Chosen() {
					a.P.StopLiveEdit("")
				}
			})
		}
	}
	a.activitySection(c, "实时编辑", &a.activityOpen.edits, len(edits), color, actions, func() {
		if len(edits) == 0 {
			muted(c, "暂无实时编辑文件")
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
					ui.Text(c, "远端文件已变更，本次修改未上传").FontSize(12).TextColor(t.Warning)
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
								a.confirmDanger("覆盖 "+host+" 上的 "+path.Base(l.Path), fmt.Sprintf("%s\n%s 上的文件已被其他进程修改，覆盖将丢失远端更改。", l.Path, host), "覆盖", func() {
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

func transferCount(jobs []transfer.Job, states ...string) int {
	n := 0
	for _, j := range jobs {
		if slices.Contains(states, j.State) {
			n++
		}
	}
	return n
}
func activeTransfers(jobs []transfer.Job) int { return transferCount(jobs, "queued", "running") }
func failedTransfers(jobs []transfer.Job) int { return transferCount(jobs, "failed") }

// transferSection is the queue: a failure stays, with its reason, until it is retried or cleared.
func (a *App) transferSection(c *ui.Context) {
	t := c.Theme()
	jobs := a.P.Transfers()
	active, failed := activeTransfers(jobs), failedTransfers(jobs)
	done, cancelled := transferCount(jobs, "done"), transferCount(jobs, "cancelled")
	// The count is the queue, unless something failed: then it is the failures, in red, as on the strip.
	n, color := len(jobs), t.TextMuted
	if failed > 0 {
		n, color = failed, t.Danger
	}
	var actions func()
	if active > 0 || done > 0 || failed+cancelled > 0 {
		actions = func() {
			moreMenu(c, "传输操作", func(m *ui.Menu) {
				if active > 0 && m.Item("全部取消").Chosen() {
					a.P.CancelAllTransfers()
				}
				if done > 0 && m.Item("清除已完成").Chosen() {
					a.toast(fmt.Sprintf("已清除 %d 个任务", a.P.ClearCompletedTransfers()))
				}
				if failed+cancelled > 0 && m.Item("清除失败项").Chosen() {
					a.toast(fmt.Sprintf("已清除 %d 个任务", a.P.ClearFailedTransfers()))
				}
			})
		}
	}
	states := map[string]string{"queued": "等待", "running": "进行中", "done": "完成", "failed": "失败", "cancelled": "已取消"}
	a.activitySection(c, "传输队列", &a.activityOpen.transfers, n, color, actions, func() {
		if failed+cancelled > 0 {
			// What went wrong comes first, with its reason; it stays until retried or cleared.
			jobs = slices.SortedStableFunc(slices.Values(jobs), func(x, y transfer.Job) int {
				return cmp.Compare(btoi(y.State == "failed"), btoi(x.State == "failed"))
			})
			var parts []string
			if failed > 0 {
				parts = append(parts, fmt.Sprintf("%d 个传输失败", failed))
			}
			if cancelled > 0 {
				parts = append(parts, fmt.Sprintf("%d 个已取消", cancelled))
			}
			line := t.TextMuted
			if failed > 0 {
				line = t.Danger
			}
			ui.Text(c, strings.Join(parts, "，")).FontSize(12).Bold().TextColor(line).FillWidth().MinWidth(0)
		}
		if len(jobs) == 0 {
			muted(c, "暂无传输任务")
		}
		for _, j := range jobs {
			border := t.Border
			if j.State == "failed" {
				border = t.Danger.Alpha(.5)
			}
			ui.Column(c.Key(j.ID)).FillWidth().Gap(4).Padding(8).Radius(t.Radius).Background(t.Background).Border(1, border).Children(func() {
				ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, j.Label).FontSize(12).Grow(1).MinWidth(0).Tooltip(j.Label) // wraps: the server is the end of it
					switch j.State {
					case "queued", "running":
						if ghost(c, "取消", "").Clicked() {
							a.P.CancelTransfer(j.ID)
						}
					case "failed", "cancelled":
						if ghost(c, "重试", "").Clicked() {
							a.report(a.P.RetryTransfer(j.ID), "")
						}
						if iconButton(c, "移除 "+j.Label, "close").Clicked() { // one row, not all the failed ones
							id := j.ID
							a.later(func() { a.P.DismissTransfer(id) })
						}
					}
				})
				if j.State == "running" {
					ui.Progress(c, transferFraction(j)).FillWidth()
				}
				detail := transferDetail(j, states[j.State])
				muted(c, detail).FontSize(11)
				if j.Error != "" {
					ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
						title, _ := humanize(j.Error)
						ui.Text(c, title).FontSize(12).TextColor(t.Danger).SingleLine().Grow(1).MinWidth(0)
						if ghost(c, "详情", "").Clicked() {
							a.dialogs.detail, a.dialogs.detailIn = j.Error, mainWindow
						}
					})
				}
			})
		}
	})
}

// transferDetail is the grey line of a job: its state, how far it is, the speed and the attempt.
func transferDetail(j transfer.Job, state string) string {
	detail := state
	if j.Bytes > 0 { // nothing moved yet: 「失败 · 0 B」 says nothing
		detail += " · " + humanSize(j.Bytes)
	}
	switch {
	case j.Items: // a deletion counts items, and has no speed
		detail = state
		if j.Total > 0 && j.State == "running" {
			detail += fmt.Sprintf(" · 已删除 %d / %d 项 · %d%%", j.Bytes, j.Total, int(transferFraction(j)*100))
		} else if j.Bytes > 0 {
			detail += fmt.Sprintf(" · 已删除 %d 项", j.Bytes)
		}
	case j.Total > 0:
		detail = fmt.Sprintf("%s · %d%% · %s / %s", state, int(transferFraction(j)*100), humanSize(j.Bytes), humanSize(j.Total))
	}
	if elapsed := transferElapsed(j); !j.Items && elapsed > 0 {
		detail += fmt.Sprintf(" · %s/s", humanSize(int64(float64(j.Bytes)/elapsed.Seconds())))
	}
	if j.Attempt > 1 {
		detail += fmt.Sprintf(" · 第 %d 次", j.Attempt)
	}
	return detail
}

// transferElapsed is how long j ran, or has been running; zero before it starts.
func transferElapsed(j transfer.Job) time.Duration {
	switch {
	case j.Started.IsZero():
		return 0
	case j.Finished.IsZero():
		return time.Since(j.Started)
	}
	return j.Finished.Sub(j.Started)
}

// transferFraction is the share done, or -1 (indeterminate) when the size is unknown.
func transferFraction(j transfer.Job) float64 {
	if j.Total <= 0 {
		return -1
	}
	return min(1, float64(j.Bytes)/float64(j.Total))
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}
