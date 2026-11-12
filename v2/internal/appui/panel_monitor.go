package appui

import (
	"fmt"
	"math"
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
	session, watching             string
	snap                          monitor.Snapshot
	page                          int // 概览, 历史, 进程, 网络
	filter                        string
	processList, networkList      ui.ListState
	processRow, networkRow        int
	processSort                   ui.SortOrder
	historyDisk, historyInterface string
}

func (s *monitorState) init() {
	s.histories = map[string]*monitor.History{}
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
}
func (s *monitorState) forget(id string) { delete(s.histories, id) }

// watch samples the active remote session while the monitor panel shows, and nothing else.
func (s *monitorState) watch(a *App, visible bool) {
	id := ""
	if r := a.remote(); visible && r != nil {
		id = r.ID
	}
	if id == s.watching {
		return
	}
	if s.watching != "" {
		a.P.WatchMonitor(s.watching, false)
	}
	if id != "" {
		a.P.RefreshMonitor(id)
		a.P.WatchMonitor(id, true)
	}
	s.watching = id
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

func (a *App) monitorPanel(c *ui.Context) {
	s := &a.monitor
	sess := a.remote()
	if sess == nil {
		emptyState(c, "监控用于远程 SSH 会话", "切换到一个远程标签即可查看 CPU、内存、网络和进程。")
		return
	}
	id := sess.ID
	if s.session != id || s.snap.At.IsZero() {
		ui.Column(c).Fill().Center().Gap(8).Children(func() {
			ui.Spinner(c)
			muted(c, "正在采样…（仅支持 Linux 服务器）")
		})
		return
	}
	m := s.snap
	ui.Row(c).FillWidth().Padding(8, 8, 4, 8).Children(func() {
		ui.Segmented(c, &s.page, "概览", "历史", "进程", "网络").FillEqually().Label("监控视图").Grow(1)
	})
	switch s.page {
	case 0:
		ui.Scroll(c).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Padding(8).Gap(8).Children(func() { a.monitorOverview(c, id, m) })
		})
	case 1:
		ui.Scroll(c).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Padding(8).Gap(12).Children(func() { a.monitorHistory(c, id) })
		})
	case 2:
		a.processTable(c, id, m.Processes)
	default:
		a.networkTable(c, m.Network)
	}
}

func (a *App) monitorOverview(c *ui.Context, id string, m monitor.Snapshot) {
	h := a.monitor.histories[id]
	cpu := "—"
	if m.HasDelta {
		cpu = fmt.Sprintf("%.0f%%", m.CPUPercent)
	}
	ui.Grid(c).Columns(2).FillWidth().Gap(8).Children(func() {
		statCard(c, "CPU", cpu, "", h, "cpu", 100)
		mem := memoryRatio(m.MemoryTotal, m.MemoryAvailable)
		statCard(c, "内存", fmt.Sprintf("%.0f%%", mem*100), fmt.Sprintf("%s / %s", humanSize(int64(m.MemoryTotal-min(m.MemoryAvailable, m.MemoryTotal))), humanSize(int64(m.MemoryTotal))), h, "memory", 100)
		statCard(c, "网络", "↓ "+humanSize(int64(m.RXPerSecond))+"/s", "↑ "+humanSize(int64(m.TXPerSecond))+"/s", h, "rx", 0)
		disk, diskSub := "—", ""
		for _, d := range m.Disks {
			if d.Mount == "/" || disk == "—" {
				disk = fmt.Sprintf("%.0f%%", d.UsedPercent)
				diskSub = d.Mount + " · " + humanSize(int64(d.TotalKB)*1024)
			}
		}
		statCard(c, "磁盘", disk, diskSub, h, "disk:/", 100)
	})
	t := c.Theme()
	ui.Column(c).FillWidth().Gap(4).Padding(4, 2).Children(func() {
		if m.Info != "" {
			ui.Text(c, m.Info).FontSize(12).TextColor(t.TextMuted)
		}
		if m.Load != "" {
			muted(c, "负载 "+m.Load)
		}
		if m.SwapTotal > 0 {
			muted(c, fmt.Sprintf("交换 %s / %s", humanSize(int64(m.SwapTotal-min(m.SwapFree, m.SwapTotal))), humanSize(int64(m.SwapTotal))))
		}
		for _, d := range m.Disks {
			muted(c, fmt.Sprintf("%s  %.0f%%  %s", d.Mount, d.UsedPercent, d.Device))
		}
		for _, w := range m.Warnings {
			ui.Text(c, w).FontSize(12).TextColor(t.Warning)
		}
	})
}

// statCard is a number with a sparkline of its history.
func statCard(c *ui.Context, title, value, sub string, h *monitor.History, key string, ceiling float64) {
	t := c.Theme()
	ui.Column(c.Key("card-"+title)).Padding(10).Gap(2).Radius(t.Radius).Background(t.Background).Border(1, t.Border).Children(func() {
		muted(c, title)
		ui.Text(c, value).FontSize(18).Bold().SingleLine()
		if sub != "" {
			muted(c, sub).SingleLine()
		}
		if h == nil || len(h.Points) < 2 {
			return
		}
		var xs, ys []float32
		top := ceiling
		if top == 0 {
			top = 1
			for _, p := range h.Points {
				if v := p.Value(key); !math.IsNaN(v) && v > top {
					top = v
				}
			}
		}
		x, y := chart.NewLinear(0, float64(len(h.Points)-1)), chart.NewLinear(0, top)
		for i, p := range h.Points {
			xs = append(xs, x.At(float64(i)))
			ys = append(ys, y.At(p.Value(key)))
		}
		ui.Box(c).FillWidth().Height(24).Margin(4, 0, 0, 0).Draw(func(p *ui.Painter, r ui.Rect) {
			p.StrokePath(chart.Line(r, xs, ys, chart.CurveLinear), 1.5, t.Accent)
		})
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

func (a *App) processTable(c *ui.Context, id string, processes []monitor.Process) {
	s := &a.monitor
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
	ui.Table(c.Key("process-table"), &s.processList, cols, len(rows), func(row, col int) {
		v := rows[row]
		text := []string{strconv.Itoa(v.PID), fmt.Sprintf("%.1f%%", v.CPU), fmt.Sprintf("%.0fM", v.MemoryMB), v.Command}[col]
		ui.Text(c, text).SingleLine().FontSize(12).Tooltip(v.User)
	}).Grow(1).FillWidth()
	muted(c, fmt.Sprintf("%d / %d 个进程 · 右键发送信号", len(rows), len(processes))).Padding(4, 10)
}
func (a *App) networkTable(c *ui.Context, network []monitor.Network) {
	s := &a.monitor
	ui.SearchField(c, &s.filter).Label("过滤网络连接").Placeholder("端口、地址或进程").Margin(4, 8)
	var rows []monitor.Network
	q := strings.ToLower(strings.TrimSpace(s.filter))
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
