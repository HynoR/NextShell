package appui

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"math"
	"nextshell.dev/v2/internal/model"
	"nextshell.dev/v2/internal/monitor"
	"sort"
	"strconv"
	"strings"
)

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
func commandMatches(v model.Command, q, group string) bool {
	return (group == "全部" || v.Group == group) && strings.Contains(strings.ToLower(v.Name+" "+v.Group+" "+v.Description+" "+v.Text), strings.ToLower(strings.TrimSpace(q)))
}
func (a *App) commandMenu(e ui.Element, v model.Command) {
	e.ContextMenu(func(m *ui.Menu) {
		if m.Item("编辑保存命令").Chosen() {
			a.Phase2.Command = v
		}
		if m.Item("复制命令文本").Chosen() {
			a.P.CopyText(v.Text)
		}
		if m.Item("另存为命令副本").Chosen() {
			v.ID = ""
			v.Name += " 副本"
			a.Phase2.Command = v
		}
		if m.Item("删除保存命令").Chosen() {
			a.DeleteKind = "command"
			a.DeleteID = v.ID
		}
	})
}
func (a *App) commandHistory(c *ui.Context) {
	s := &a.Phase2
	ui.Checkbox(c, &s.HistoryVisible, "管理命令历史（本地明文）")
	if !s.HistoryVisible {
		return
	}
	if ui.Button(c, "清空历史").Clicked() {
		s.ClearHistoryConfirm = true
	}
	if s.ClearHistoryConfirm {
		switch ui.AlertDialog(c, &s.ClearHistoryConfirm, "清空命令历史", "删除全部本地历史？无法恢复。", "取消", "确认清空历史") {
		case 1:
			if e := a.P.SaveHistory(nil); e != nil {
				a.status(e)
			} else {
				a.reload()
			}
		}
	}
	ui.Scroll(c).Height(160).FillWidth().Children(func() {
		if len(a.History) == 0 {
			ui.Text(c, "暂无命令历史")
		}
		for i, h := range a.History {
			ui.Row(c).Key(fmt.Sprint("history-", i)).FillWidth().Gap(8).Children(func() {
				if ui.Button(c, h).Grow(1).Clicked() {
					s.Command.Text = h
				}
				if ui.Button(c, "删除历史项").Clicked() {
					index := i
					a.later(func() {
						next := append([]string{}, a.History[:index]...)
						next = append(next, a.History[index+1:]...)
						if e := a.P.SaveHistory(next); e != nil {
							a.status(e)
						} else {
							a.reload()
						}
					})
				}
			})
		}
	})
}
func clampRatio(v float64) float64 {
	if math.IsNaN(v) || v < 0 {
		return 0
	}
	return min(v, 1)
}
func memoryRatio(total, free uint64) float64 {
	if total == 0 || free > total {
		return 0
	}
	return float64(total-free) / float64(total)
}
func filteredProcesses(vs []monitor.Process, q string, order ui.SortOrder) []monitor.Process {
	q = strings.ToLower(strings.TrimSpace(q))
	out := []monitor.Process{}
	for _, v := range vs {
		if strings.Contains(strings.ToLower(fmt.Sprintf("%d %s %s", v.PID, v.User, v.Command)), q) {
			out = append(out, v)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		cmp := 0
		switch order.Column {
		case "PID":
			cmp = a.PID - b.PID
		case "内存 MiB":
			if a.MemoryMB < b.MemoryMB {
				cmp = -1
			} else if a.MemoryMB > b.MemoryMB {
				cmp = 1
			}
		case "命令":
			cmp = strings.Compare(a.Command, b.Command)
		default:
			if a.CPU < b.CPU {
				cmp = -1
			} else if a.CPU > b.CPU {
				cmp = 1
			}
		}
		if cmp == 0 {
			return a.PID < b.PID
		}
		if order.Descending {
			return cmp > 0
		}
		return cmp < 0
	})
	return out
}
func (a *App) processTable(c *ui.Context, processes []monitor.Process) {
	s := &a.Phase2
	ui.SearchField(c, &s.MonitorFilter).Label("过滤进程/网络").Placeholder("PID / 用户 / 命令").FillWidth()
	rows := filteredProcesses(processes, s.MonitorFilter, s.ProcessSort)
	s.ProcessList.Sort = &s.ProcessSort
	s.ProcessList.Selected = &s.ProcessRow
	s.ProcessList.Key = func(i int) any { return rows[i].PID }
	s.ProcessList.Label = func(i int) string { return rows[i].Command }
	columns := []ui.TableColumn{{Title: "PID", Width: 75, Sortable: true}, {Title: "用户", Width: 100}, {Title: "CPU", Width: 85, Sortable: true}, {Title: "内存 MiB", Width: 110, Sortable: true}, {Title: "命令", Sortable: true}}
	table := ui.Table(c.Key("process-table"), &s.ProcessList, columns, len(rows), func(row, col int) {
		v := rows[row]
		text := []string{strconv.Itoa(v.PID), v.User, fmt.Sprintf("%.1f%%", v.CPU), fmt.Sprintf("%.1f", v.MemoryMB), v.Command}[col]
		e := ui.Text(c, text).SingleLine()
		if col == 4 {
			e.ContextMenu(func(m *ui.Menu) {
				if m.Item("复制进程命令").Chosen() {
					a.P.CopyText(v.Command)
				}
			})
		}
	}).Height(260).FillWidth()
	if table.Changed() && s.ProcessRow >= 0 && s.ProcessRow < len(rows) {
		s.PID = strconv.Itoa(rows[s.ProcessRow].PID)
	}
	ui.Text(c, fmt.Sprintf("%d / %d 个进程；选中行填入PID，信号操作仍需明确确认", len(rows), len(processes))).TextColor(c.Theme().TextMuted)
}
func (a *App) networkTable(c *ui.Context, network []monitor.Network) {
	s := &a.Phase2
	ui.SearchField(c, &s.MonitorFilter).Label("过滤进程/网络").Placeholder("PID / 端口 / IP / 进程").FillWidth()
	rows := []monitor.Network{}
	q := strings.ToLower(strings.TrimSpace(s.MonitorFilter))
	for _, v := range network {
		if strings.Contains(strings.ToLower(fmt.Sprintf("%s %s %s:%d %s:%d %d %s", v.Protocol, v.State, v.LocalIP, v.LocalPort, v.RemoteIP, v.RemotePort, v.PID, v.Process)), q) {
			rows = append(rows, v)
		}
	}
	s.NetworkList.Selected = &s.NetworkRow
	s.NetworkList.Key = func(i int) any {
		v := rows[i]
		return fmt.Sprintf("%s|%s|%d|%s|%d|%d", v.Protocol, v.LocalIP, v.LocalPort, v.RemoteIP, v.RemotePort, v.PID)
	}
	ui.Table(c.Key("network-table"), &s.NetworkList, []ui.TableColumn{{Title: "协议/状态", Width: 130}, {Title: "本地地址"}, {Title: "远端地址"}, {Title: "PID/进程"}, {Title: "连接/IP", Width: 85}}, len(rows), func(row, col int) {
		v := rows[row]
		text := []string{v.Protocol + " " + v.State, fmt.Sprintf("%s:%d", v.LocalIP, v.LocalPort), fmt.Sprintf("%s:%d", v.RemoteIP, v.RemotePort), fmt.Sprintf("%d/%s", v.PID, v.Process), fmt.Sprintf("%d/%d", v.Connections, v.UniqueIPs)}[col]
		ui.Text(c, text).SingleLine()
	}).Height(260).FillWidth()
	ui.Text(c, fmt.Sprintf("%d / %d 条网络记录", len(rows), len(network))).TextColor(c.Theme().TextMuted)
}
