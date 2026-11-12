package appui

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"
)

// activityColumn is the right column: what runs beside the terminal (transfers,
// agents, live edits). Folded, it is a strip of icons with counts.
func (a *App) activityColumn(c *ui.Context) {
	t := c.Theme()
	active := activeTransfers(a.P.Transfers())
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
	if !a.layout.rightOpen {
		ui.Column(c).Width(stripWidth).FillHeight().Padding(8, 0).Gap(8).AlignItems(ui.Center).Background(t.Surface).BorderColor(t.Border).BorderWidth(0, 0, 0, 1).Children(func() {
			open := iconButton(c, "展开活动栏", "panel-right").Clicked()
			ui.Box(c).Width(20).Height(1).Background(t.Border)
			transfers := "传输队列"
			if active > 0 {
				transfers = fmt.Sprintf("%d 个传输进行中", active)
			}
			for _, v := range []struct {
				label, icon string
				count       int
				color       ui.Color
			}{{transfers, "transfer", active, t.Accent}, {agentLabel, "agent", pending, t.Warning}, {"实时编辑", "edit", 0, t.Accent}} {
				open = stripButton(c, v.label, v.icon, v.count, v.color, v.label == "Agent 活动中") || open
			}
			if open {
				a.layout.rightOpen = true
			}
		})
		return
	}
	ui.Column(c).Width(rightWidth).FillHeight().Background(t.Surface).BorderColor(t.Border).BorderWidth(0, 0, 0, 1).Children(func() {
		ui.Row(c).FillWidth().Padding(8, 8, 4, 12).AlignItems(ui.Center).Children(func() {
			ui.Spacer(c).Grow(1)
			if iconButton(c, "收起活动栏", "panel-right").Clicked() {
				a.layout.rightOpen = false
			}
		})
		ui.Scroll(c).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Padding(0, 12, 12, 12).Gap(18).Children(func() {
				a.transferSection(c)
				a.agentSection(c, agentLabel, pending)
				ui.Column(c).FillWidth().Gap(8).Children(func() {
					sectionLabel(c, "实时编辑")
					muted(c, "暂无实时编辑")
				})
			})
		})
	})
}

// agentSection says whether agents are at work and what waits for approval.
func (a *App) agentSection(c *ui.Context, label string, pending int) {
	t := c.Theme()
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		sectionLabel(c, "Agent 活动")
		if pending > 0 {
			b := ui.ButtonBase(c).Label(label).Tooltip(pendingTooltip(a.P.AgentOverview().Pending)).FillWidth().Padding(6, 8).Radius(t.Radius).Background(t.Warning.Alpha(.14)).Children(func() {
				ui.Text(c, label).FontSize(12).Bold().TextColor(t.Warning)
			})
			if b.Clicked() {
				a.openSettings("Agent")
			}
		} else if label == "Agent 活动中" {
			ui.Text(c, "Agent 活动中 · 最近有 Agent 在操作 NextShell").FontSize(12).TextColor(accentInk(t))
		} else {
			muted(c, "最近没有 Agent 操作")
		}
		if ghost(c, "设置 → Agent", "agent").Clicked() {
			a.openSettings("Agent")
		}
	})
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
