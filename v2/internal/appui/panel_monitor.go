package appui

import (
	"fmt"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/chart"
	"nextshell.dev/v2/internal/monitor"
)

type monitorState struct {
	histories                     map[string]*monitor.History
	details                       map[string]monitor.Snapshot // the last full sample per session, for the process and network pages
	session, watching, detailed   string
	snap                          monitor.Snapshot
	page                          int // pageHistory, pageProcesses, pageNetwork
	filter, netFilter             string
	processList, networkList      ui.ListState
	processRow, networkRow        int
	processSort                   ui.SortOrder
	detail                        int // PID shown in the process drawer
	historyDisk, historyInterface string
}

// The pages of the workbench's 系统信息 tab.
const (
	pageHistory = iota
	pageProcesses
	pageNetwork
)

func (s *monitorState) init() {
	s.histories = map[string]*monitor.History{}
	s.details = map[string]monitor.Snapshot{}
	s.processRow, s.networkRow = -1, -1
	s.processSort = ui.SortOrder{Column: "CPU", Descending: true}
}
func (s *monitorState) handle(a *App, e Event) {
	if e.Monitor == nil {
		return
	}
	s.record(e.Monitor.Session, e.Monitor.Snapshot)
	s.session = e.Monitor.Session
	s.snap = e.Monitor.Snapshot
	if s.snap.Detail {
		s.details[s.session] = s.snap
	}
}
func (s *monitorState) forget(id string) {
	delete(s.histories, id)
	delete(s.details, id)
}

// watchMonitor samples the active connected SSH session while the left column
// or the 系统信息 tab shows it, and adds processes and connections while one
// of their pages shows.
func (a *App) watchMonitor() {
	s := &a.monitor
	id := ""
	if r := a.remote(); r != nil && !a.P.Exited(r.ID) {
		id = r.ID
	}
	sysinfo := !a.layout.benchHidden && a.layout.bench == benchSysinfo
	watch, detailed := "", ""
	if id != "" && (!a.layout.leftHidden || sysinfo) {
		watch = id
	}
	if id != "" && sysinfo && s.page != pageHistory {
		detailed = id
	}
	if detailed != s.detailed {
		if s.detailed != "" {
			a.P.WatchMonitorDetail(s.detailed, false)
		}
		if detailed != "" {
			a.P.WatchMonitorDetail(detailed, true)
		}
		s.detailed = detailed
	}
	if watch != s.watching {
		if s.watching != "" {
			a.P.WatchMonitor(s.watching, false)
		}
		if watch != "" {
			a.P.RefreshMonitor(watch)
			a.P.WatchMonitor(watch, true)
		}
		s.watching = watch
	}
}

// record keeps at most 32 sessions' histories, dropping the stalest.
func (s *monitorState) record(id string, snap monitor.Snapshot) {
	h := s.histories[id]
	if h == nil {
		if len(s.histories) >= 32 {
			oldest := ""
			var at time.Time
			for key, history := range s.histories {
				if len(history.Points) == 0 {
					oldest = key
					break
				}
				last := history.Points[len(history.Points)-1].At
				if at.IsZero() || last.Before(at) {
					at, oldest = last, key
				}
			}
			delete(s.histories, oldest)
		}
		h = &monitor.History{}
		s.histories[id] = h
	}
	h.Append(snap)
}

// sysinfoPanel is the workbench's 系统信息 tab: the history, process and network pages.
func (a *App) sysinfoPanel(c *ui.Context) {
	s := &a.monitor
	sess := a.remote()
	if sess == nil {
		emptyState(c, "系统信息用于远程 SSH 会话", "切换到一个远程标签即可查看历史曲线、进程和网络连接。")
		return
	}
	id := sess.ID
	ui.Row(c).FillWidth().Padding(6, 8, 4, 8).Children(func() {
		ui.Segmented(c, &s.page, "历史", "进程", "网络").FillEqually().Label("系统信息视图").Width(300)
	})
	full, ok := s.details[id]
	switch {
	case s.page == pageHistory:
		ui.Scroll(c).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Padding(8).Gap(12).Children(func() { a.monitorHistory(c, id) })
		})
	case !ok: // light samples leave the tables out: nothing yet is not "none"
		sampling(c)
	case s.page == pageProcesses:
		a.processTable(c, id, full)
	default:
		a.networkTable(c, full.Network)
	}
}

func sampling(c *ui.Context) {
	ui.Column(c).Fill().Center().Gap(8).Children(func() {
		ui.Spinner(c)
		muted(c, "正在采样…（仅支持 Linux 服务器）")
	})
}

// Metric colors: CPU blue, memory green, swap indigo, disk cyan; upload orange, download green.
func metricColor(t *ui.Theme, name string) ui.Color {
	dark := map[string]string{"cpu": "#4d9fff", "mem": "#34d399", "swap": "#818cf8", "disk": "#22d3ee", "up": "#fb923c", "down": "#34d399"}
	light := map[string]string{"cpu": "#2563eb", "mem": "#059669", "swap": "#4f46e5", "disk": "#0e7490", "up": "#c2410c", "down": "#047857"}
	if t.Dark {
		return ui.Hex(dark[name])
	}
	return ui.Hex(light[name])
}

// monitorSidebar is the left column's 系统监控: bars for CPU, memory, swap and
// disk, load, network rates and a line about the system.
func (a *App) monitorSidebar(c *ui.Context, sess *Session) {
	t := c.Theme()
	sectionLabel(c, "系统监控")
	m := a.monitor.snap
	switch {
	case sess.Local:
		muted(c, "本地终端不采样")
		return
	case a.P.Exited(sess.ID):
		muted(c, "连接断开后不再采样")
		return
	case a.monitor.session != sess.ID || m.At.IsZero():
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Spinner(c)
			muted(c, "正在采样…（仅支持 Linux 服务器）")
		})
		return
	}
	if m.Load != "" {
		ui.Row(c).FillWidth().Gap(8).Padding(6, 8).Radius(6).Background(t.Background).Border(1, t.Border).Children(func() {
			muted(c, "负载")
			ui.Text(c, m.Load).Font("monospace").FontSize(12).SingleLine()
		})
	}
	used := func(total, free uint64) string {
		return humanSize(int64(total-min(free, total))) + " / " + humanSize(int64(total))
	}
	if m.HasDelta {
		meterRow(c, "CPU", m.CPUPercent, "", metricColor(t, "cpu"))
	} else {
		meterRow(c, "CPU", 0, "—", metricColor(t, "cpu"))
	}
	meterRow(c, "内存", memoryRatio(m.MemoryTotal, m.MemoryAvailable)*100, used(m.MemoryTotal, m.MemoryAvailable), metricColor(t, "mem"))
	if m.SwapTotal > 0 {
		meterRow(c, "交换", memoryRatio(m.SwapTotal, m.SwapFree)*100, used(m.SwapTotal, m.SwapFree), metricColor(t, "swap"))
	} else {
		meterRow(c, "交换", 0, "未启用", metricColor(t, "swap"))
	}
	for i, d := range m.Disks {
		if d.Mount == "/" || i == len(m.Disks)-1 && !slices.ContainsFunc(m.Disks, func(d monitor.Disk) bool { return d.Mount == "/" }) {
			meterRow(c, "磁盘", d.UsedPercent, humanSize(int64(d.UsedKB)*1024)+" / "+humanSize(int64(d.TotalKB)*1024), metricColor(t, "disk")).Tooltip(d.Mount + "  " + d.Device)
			break
		}
	}
	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		muted(c, "网络").Label("网络速率").Width(32)
		ui.Text(c, "↑ "+humanSize(int64(m.TXPerSecond))+"/s").Font("monospace").FontSize(11).TextColor(metricColor(t, "up")).SingleLine().Grow(1).Basis(0)
		ui.Text(c, "↓ "+humanSize(int64(m.RXPerSecond))+"/s").Font("monospace").FontSize(11).TextColor(metricColor(t, "down")).SingleLine().Grow(1).Basis(0)
	})
	if m.Info != "" {
		muted(c, m.Info).FontSize(11).Tooltip(m.Info)
	}
	for _, w := range m.Warnings {
		ui.Text(c, w).FontSize(11).TextColor(t.Warning)
	}
	ui.Row(c).FillWidth().Gap(6).Padding(4, 0, 0, 0).Children(func() {
		for _, v := range []struct {
			label, icon string
			page        int
		}{{"进程管理器", "cpu", pageProcesses}, {"网络管理器", "globe", pageNetwork}} {
			b := ui.ButtonBase(c).Label(v.label).Grow(1).Basis(0).Height(28).Gap(4).Radius(t.Radius).Center().Border(1, t.Border)
			if b.Hovered() {
				b.Background(t.SurfaceHover)
			}
			b.Children(func() {
				ui.Icon(c, appIcons[v.icon]).FontSize(13).TextColor(t.TextMuted)
				ui.Text(c, v.label).FontSize(12).SingleLine()
			})
			if b.Clicked() {
				a.monitor.page = v.page
				a.showBench(benchSysinfo)
			}
		}
	})
}

// meterRow is a metric's name and value over its bar, amber from 70% and red from 90%.
func meterRow(c *ui.Context, name string, percent float64, detail string, color ui.Color) ui.Element {
	return ui.Column(c.Key("meter-" + name)).FillWidth().Gap(3).Children(func() {
		ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
			muted(c, name).Grow(1)
			if detail != "" {
				ui.Text(c, detail).Font("monospace").FontSize(11).TextColor(c.Theme().TextMuted).SingleLine()
			}
			if detail != "—" {
				ui.Text(c, fmt.Sprintf("%.0f%%", percent)).Font("monospace").FontSize(12).Bold().SingleLine().Width(38).TextAlign(ui.End)
			}
		})
		ui.Meter(c, clampRatio(percent/100)*100, 0, 100, &ui.MeterLevels{Warning: 70, Critical: 90, NormalColor: color}).Label(name).FillWidth().Height(6).Radius(3)
	})
}

func (a *App) monitorHistory(c *ui.Context, id string) {
	s := &a.monitor
	h := s.histories[id]
	if h == nil || len(h.Points) == 0 {
		muted(c, "还没有历史数据")
		return
	}
	muted(c, fmt.Sprintf("最近 %d 次采样，不保存到磁盘", len(h.Points)))
	interfaces, disks := []string{"全部网卡"}, []string{}
	knownNet, knownDisk := map[string]bool{}, map[string]bool{}
	for _, p := range h.Points {
		for key := range p.Values {
			if n, ok := strings.CutPrefix(key, "rx:"); ok && !knownNet[n] {
				knownNet[n] = true
				interfaces = append(interfaces, n)
			}
			if n, ok := strings.CutPrefix(key, "disk:"); ok && !knownDisk[n] {
				knownDisk[n] = true
				disks = append(disks, n)
			}
		}
	}
	sort.Strings(interfaces[1:])
	sort.Strings(disks)
	if s.historyInterface == "" || s.historyInterface != "全部网卡" && !knownNet[s.historyInterface] {
		s.historyInterface = "全部网卡"
	}
	if !knownDisk[s.historyDisk] && len(disks) > 0 {
		s.historyDisk = disks[0]
		if knownDisk["/"] {
			s.historyDisk = "/"
		}
	}
	ui.Row(c).FillWidth().Gap(8).Children(func() {
		ui.Select(c, &s.historyInterface, interfaces).Label("历史网卡").Grow(1)
		if len(disks) > 0 {
			ui.Select(c, &s.historyDisk, disks).Label("历史磁盘").Grow(1)
		}
	})
	a.historyPlot(c, "CPU · %", h, []historySeries{{"cpu", "CPU"}}, 100)
	a.historyPlot(c, "内存 / 交换 · %", h, []historySeries{{"memory", "内存"}, {"swap", "交换"}}, 100)
	suffix := ""
	if s.historyInterface != "全部网卡" {
		suffix = ":" + s.historyInterface
	}
	a.historyPlot(c, "网络 · KiB/s", h, []historySeries{{"rx" + suffix, "接收"}, {"tx" + suffix, "发送"}}, 0)
	if len(disks) > 0 {
		a.historyPlot(c, "磁盘占用 · %", h, []historySeries{{"disk:" + s.historyDisk, s.historyDisk}}, 100)
	}
}

type historySeries struct{ Key, Name string }

func (a *App) historyPlot(c *ui.Context, title string, h *monitor.History, series []historySeries, ceiling float64) {
	t := c.Theme()
	first, last := h.Points[0].At, h.Points[len(h.Points)-1].At
	if first.Equal(last) {
		last = last.Add(time.Second)
	}
	x := chart.NewTime(first, last)
	if ceiling == 0 {
		ceiling = 1
		for _, p := range h.Points {
			for _, s := range series {
				if v := p.Value(s.Key); !math.IsNaN(v) && v > ceiling {
					ceiling = v
				}
			}
		}
	}
	y := chart.NewLinear(0, ceiling).Nice(4)
	colors := []ui.Color{t.Accent, t.TextMuted}
	ui.Column(c.Key(title)).FillWidth().Gap(6).Children(func() {
		ui.Row(c).FillWidth().Gap(12).Children(func() {
			ui.Text(c, title).Bold().Grow(1)
			for i, s := range series {
				latest := h.Points[len(h.Points)-1].Value(s.Key)
				value := "—"
				if !math.IsNaN(latest) {
					value = fmt.Sprintf("%.1f", latest)
				}
				ui.Text(c, s.Name+" "+value).TextColor(colors[i%len(colors)]).FontSize(12)
			}
		})
		chart.Plot(c, chart.Axes{Left: y.Ticks(4), Bottom: x.Ticks(3), Label: func(_ chart.Side, tick chart.Tick) {
			ui.Text(c, tick.Label).NoWrap().FontSize(10).TextColor(t.TextMuted)
		}}, func(area ui.Element) {
			type line struct {
				xs, ys []float32
				color  ui.Color
			}
			var lines []line
			for i, s := range series {
				var xs, ys []float32
				for j, point := range h.Points {
					if j > 0 && point.At.Sub(h.Points[j-1].At) > 10*time.Second {
						xs = append(xs, x.At(point.At.Add(-time.Nanosecond)))
						ys = append(ys, float32(math.NaN()))
					}
					xs = append(xs, x.At(point.At))
					ys = append(ys, y.At(point.Value(s.Key)))
				}
				lines = append(lines, line{xs, ys, colors[i%len(colors)]})
			}
			area.Draw(func(p *ui.Painter, r ui.Rect) {
				for _, tick := range y.Ticks(4) {
					p.Line(r.X, chart.Y(r, tick.At), r.X+r.W, chart.Y(r, tick.At), 1, t.Border)
				}
				for _, line := range lines {
					p.StrokePath(chart.Line(r, line.xs, line.ys, chart.CurveLinear), 2, line.color)
				}
			})
		}).Label(title).Height(120).FillWidth().Gap(6)
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
		case "内存":
			cmp = compare(a.MemoryMB, b.MemoryMB)
		case "命令":
			cmp = strings.Compare(a.Command, b.Command)
		default:
			cmp = compare(a.CPU, b.CPU)
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
func compare(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func (a *App) processTable(c *ui.Context, id string, snap monitor.Snapshot) {
	s := &a.monitor
	processes := snap.Processes
	ui.SearchField(c, &s.filter).Label("过滤进程").Placeholder("PID、用户或命令").Margin(4, 8)
	rows := filteredProcesses(processes, s.filter, s.processSort)
	s.processList.Sort = &s.processSort
	s.processList.Selected = &s.processRow
	s.processList.KeepOffset = true
	s.processList.Key = func(i int) any { return rows[i].PID }
	s.processList.Label = func(i int) string { return rows[i].Command }
	s.processList.CellMenu = func(m *ui.Menu, row, col int) {
		if row < 0 || row >= len(rows) {
			return
		}
		v := rows[row]
		if m.Item("详细信息").Chosen() {
			s.detail = v.PID
		}
		if m.Item("复制命令").Chosen() {
			a.P.CopyText(v.Command)
		}
		m.Submenu("发送信号", func(m *ui.Menu) {
			for _, sig := range []string{"TERM", "KILL", "STOP", "CONT"} {
				if m.Item(sig).Chosen() {
					a.later(func() {
						a.confirm(fmt.Sprintf("向进程 %d 发送 %s", v.PID, sig), v.Command+"\n这可能会终止或暂停服务器上的程序。", "发送 "+sig, func() { a.P.SignalProcess(id, v.PID, sig) })
					})
				}
			}
		})
	}
	cols := []ui.TableColumn{{Title: "PID", Width: 60, Sortable: true}, {Title: "CPU", Width: 68, Sortable: true, Align: ui.End}, {Title: "内存", Width: 68, Sortable: true, Align: ui.End}, {Title: "命令", Sortable: true}}
	table := ui.Table(c.Key("process-table"), &s.processList, cols, len(rows), func(row, col int) {
		v := rows[row]
		text := []string{strconv.Itoa(v.PID), fmt.Sprintf("%.1f%%", v.CPU), fmt.Sprintf("%.0fM", v.MemoryMB), v.Command}[col]
		ui.Text(c, text).SingleLine().FontSize(12).Tooltip(v.User)
	}).Grow(1).FillWidth()
	if table.Submitted() && s.processRow >= 0 && s.processRow < len(rows) {
		s.detail = rows[s.processRow].PID
	}
	if s.detail != 0 {
		a.processDrawer(c, id, snap)
	}
	muted(c, fmt.Sprintf("%d / %d 个进程 · 双击查看详情，右键发送信号", len(rows), len(processes))).Padding(4, 10)
}
func (a *App) networkTable(c *ui.Context, network []monitor.Network) {
	s := &a.monitor
	ui.SearchField(c, &s.netFilter).Label("过滤网络连接").Placeholder("端口、地址或进程").Margin(4, 8)
	var rows []monitor.Network
	q := strings.ToLower(strings.TrimSpace(s.netFilter))
	for _, v := range network {
		if strings.Contains(strings.ToLower(fmt.Sprintf("%s %s %s:%d %s:%d %d %s", v.Protocol, v.State, v.LocalIP, v.LocalPort, v.RemoteIP, v.RemotePort, v.PID, v.Process)), q) {
			rows = append(rows, v)
		}
	}
	s.networkList.Selected = &s.networkRow
	s.networkList.Key = func(i int) any {
		v := rows[i]
		return fmt.Sprintf("%s|%s|%d|%s|%d|%d", v.Protocol, v.LocalIP, v.LocalPort, v.RemoteIP, v.RemotePort, v.PID)
	}
	cols := []ui.TableColumn{{Title: "协议", Width: 84}, {Title: "本地"}, {Title: "远端"}, {Title: "进程", Width: 90}}
	ui.Table(c.Key("network-table"), &s.networkList, cols, len(rows), func(row, col int) {
		v := rows[row]
		text := []string{v.Protocol + " " + v.State, fmt.Sprintf("%s:%d", v.LocalIP, v.LocalPort), fmt.Sprintf("%s:%d", v.RemoteIP, v.RemotePort), fmt.Sprintf("%d %s", v.PID, v.Process)}[col]
		ui.Text(c, text).SingleLine().FontSize(12).Tooltip(fmt.Sprintf("%d 个连接 · %d 个对端", v.Connections, v.UniqueIPs))
	}).Grow(1).FillWidth()
	muted(c, fmt.Sprintf("%d / %d 条", len(rows), len(network))).Padding(4, 10)
}

// processDrawer details one process under the table; it follows the PID
// across samples and says so when the process is gone.
func (a *App) processDrawer(c *ui.Context, id string, snap monitor.Snapshot) {
	s := &a.monitor
	processes := snap.Processes
	t := c.Theme()
	i := slices.IndexFunc(processes, func(v monitor.Process) bool { return v.PID == s.detail })
	// The workbench can be low: the drawer scrolls rather than push its buttons out of the window.
	ui.Scroll(c).FillWidth().MaxHeight(260).Shrink(1).MinHeight(0).Background(t.Surface).BorderColor(t.Border).BorderWidth(1, 0, 0, 0).Children(func() {
		ui.Column(c).FillWidth().Padding(10).Gap(6).Label("进程详情").Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Text(c, fmt.Sprintf("进程 %d", s.detail)).Bold().Grow(1)
				if iconButton(c, "关闭详情", "close").Clicked() {
					s.detail = 0
				}
			})
			if i < 0 {
				muted(c, "进程已退出")
				return
			}
			v := processes[i]
			ui.Scroll(c).FillWidth().MaxHeight(72).Children(func() {
				ui.Text(c, v.Command).Font("monospace").FontSize(12).Wrap().Selectable().Label("命令行")
			})
			started := snap.At.Add(-time.Duration(v.Elapsed) * time.Second)
			for _, kv := range [][2]string{
				{"用户", v.User},
				{"启动时间", started.Format("2006-01-02 15:04:05") + "（已运行 " + uptime(time.Duration(v.Elapsed)*time.Second) + "）"},
				{"CPU / 内存", fmt.Sprintf("%.1f%% / %.0f MB（%.1f%%）", v.CPU, v.MemoryMB, v.MemoryPercent)},
				{"父进程 · 状态", fmt.Sprintf("%d · %s · nice %d", v.PPID, v.State, v.Nice)},
			} {
				ui.Row(c).FillWidth().Gap(8).Children(func() {
					ui.Text(c, kv[0]).FontSize(12).TextColor(t.TextMuted).Width(84)
					ui.Text(c, kv[1]).FontSize(12).SingleLine().Grow(1).MinWidth(0)
				})
			}
			ui.Row(c).Gap(8).Children(func() {
				if ui.Button(c, "复制命令行").Clicked() {
					a.P.CopyText(v.Command)
					a.toast("已复制命令行")
				}
				if ui.Button(c, "结束进程…").Clicked() {
					a.confirm(fmt.Sprintf("向进程 %d 发送 TERM", v.PID), v.Command+"\n这可能会终止服务器上的程序。", "发送 TERM", func() { a.P.SignalProcess(id, v.PID, "TERM") })
				}
			})
		})
	})
}
func uptime(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d 秒", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%d 分钟", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d 小时 %d 分钟", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%d 天", int(d.Hours()/24))
}
