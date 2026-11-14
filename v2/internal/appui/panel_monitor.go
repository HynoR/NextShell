package appui

import (
	"errors"
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

// hostMonitor is what the app knows of one host's server, kept per host (not per tab) so a reconnected or
// switched-back host shows its last data at once, until a fresh sample replaces it.
type hostMonitor struct {
	history    monitor.History
	snap, full monitor.Snapshot // the last sample, and the last one with processes and connections
	session    string           // the session that sent snap
	latency    time.Duration    // 0 when unknown
	nic        string           // the interface the left column shows
	// A failed sample counts against failSession; stopAfter in a row stop its sampling until a retry,
	// a reconnect (a new session) or a good sample.
	fails       int
	failSession string
	failErr     error
}

type monitorState struct {
	hosts                         map[string]*hostMonitor // by hostKey
	watching, detailed            map[string]bool         // sessions sampled lightly, and with the full probe
	tools                         []toolTab
	tool                          string // key of the shown tool tab; "" while a terminal shows
	trace                         traceState
	filter, netFilter             string
	processList, networkList      ui.ListState
	processRow, networkRow        int
	processSort                   ui.SortOrder
	detail                        int // PID shown in the process drawer
	historyDisk, historyInterface string
}

const (
	allNICs     = "全部网卡"
	cachedAfter = 6 * time.Second // a sample older than this is shown as 缓存数据
	stopAfter   = 3               // failed samples in a row before a server is left alone
)

func (s *monitorState) init() {
	s.hosts = map[string]*hostMonitor{}
	s.watching, s.detailed = map[string]bool{}, map[string]bool{}
	s.processRow, s.networkRow = -1, -1
	s.processSort = ui.SortOrder{Column: "CPU", Descending: true}
}

// hostKey names the host a session belongs to: its saved connection, or the session when it has none.
func hostKey(s *Session) string { return orDefault(s.Connection.ID, s.ID) }
func (a *App) hostKeyOf(session string) string {
	if i := a.tabIndex(session); i >= 0 {
		return hostKey(a.Tabs[i])
	}
	return session
}

func (s *monitorState) handle(a *App, e Event) {
	if d := e.Diagnostic; d != nil && d.ID == s.trace.id {
		s.trace.busy, s.trace.text = false, d.Text
	}
	m := e.Monitor
	if m == nil {
		return
	}
	key := a.hostKeyOf(m.Session)
	if m.Err != nil {
		h := s.entry(key)
		if h.failSession != m.Session {
			h.failSession, h.fails = m.Session, 0
		}
		if h.fails++; h.fails == stopAfter {
			a.log("无法读取服务器状态：" + m.Err.Error())
		}
		h.failErr = m.Err
		return
	}
	if h := s.record(key, m.Snapshot); h != nil {
		h.session, h.latency, h.fails = m.Session, m.Latency, 0
	}
}

// entry is the host's record, made when it has none, for at most 32 hosts, dropping the stalest.
func (s *monitorState) entry(key string) *hostMonitor {
	h := s.hosts[key]
	if h != nil {
		return h
	}
	if len(s.hosts) >= 32 {
		oldest := ""
		var at time.Time
		for key, o := range s.hosts {
			if len(o.history.Points) == 0 {
				oldest = key
				break
			}
			last := o.history.Points[len(o.history.Points)-1].At
			if at.IsZero() || last.Before(at) {
				at, oldest = last, key
			}
		}
		delete(s.hosts, oldest)
	}
	h = &hostMonitor{}
	s.hosts[key] = h
	return h
}

// record keeps the sample as the host's latest and in its history.
// It returns nil for a sample older than the last.
func (s *monitorState) record(key string, snap monitor.Snapshot) *hostMonitor {
	h := s.entry(key)
	if !h.history.Append(snap) {
		return nil
	}
	h.snap = snap
	if snap.Detail {
		h.full = snap
	}
	return h
}

// monitorOn is the host's monitoring switch as saved now, so turning it off takes effect on open tabs.
func (a *App) monitorOn(s *Session) bool {
	if cc, ok := a.host(s.Connection.ID); ok && s.Connection.ID != "" {
		return cc.Monitor
	}
	return s.Connection.Monitor
}

// sampling says whether the session's server is being sampled: connected, its switch on, and not given up on.
func (a *App) sampling(s *Session) bool {
	return !s.Local && !a.P.Exited(s.ID) && a.monitorOn(s) && !a.monitorStopped(s)
}

// monitorStopped says the connected session's server failed stopAfter samples in a row: it is left alone, and
// the panels offer to turn its monitoring off or to try again.
func (a *App) monitorStopped(s *Session) bool {
	h := a.monitor.hosts[hostKey(s)]
	return h != nil && h.fails >= stopAfter && h.failSession == s.ID && !a.P.Exited(s.ID)
}

// monitorOff turns the server's monitoring off for good: its saved connection, and this session's own copy.
func (a *App) monitorOff(s *Session) {
	s.Connection.Monitor = false
	if cc, ok := a.host(s.Connection.ID); ok && s.Connection.ID != "" {
		cc.Monitor = false
		a.report(a.P.SaveConnection(cc, nil), "已关闭此服务器的监控")
		a.reload()
	}
}

// monitorUnreadable is what the panels show in place of a server's status once it is given up on.
func (a *App) monitorUnreadable(c *ui.Context, s *Session) {
	why := "连接不稳定或服务器没有响应"
	if h := a.monitor.hosts[hostKey(s)]; h != nil && errors.Is(h.failErr, monitor.ErrUnsupported) {
		why = "目前只支持 Linux"
	}
	ui.Column(c).FillWidth().MaxWidth(280).Gap(8).Children(func() {
		ui.Text(c, "无法读取此服务器的状态（"+why+"）").FontSize(12).Wrap()
		if ui.Button(c, "关闭此服务器的监控").FillWidth().Clicked() {
			a.later(func() { a.monitorOff(s) })
		}
		if ui.Button(c, "重试").FillWidth().Clicked() {
			a.later(func() {
				if h := a.monitor.hosts[hostKey(s)]; h != nil {
					h.fails = 0
				}
			})
		}
	})
}

// cached says whether what the host shows is old: from another session, or not renewed for a while.
func (a *App) cached(s *Session, h *hostMonitor) bool {
	return h.session != s.ID || time.Since(h.snap.At) > cachedAfter
}

// watchMonitor samples the active connected SSH session while the left column or the 系统信息 tab shows it,
// and every host with a process or network tab open, with the full probe, until the tab closes.
func (a *App) watchMonitor() {
	s := &a.monitor
	light, full := map[string]bool{}, map[string]bool{}
	sysinfo := !a.layout.benchHidden && a.layout.bench == benchSysinfo
	if r := a.remote(); r != nil && a.sampling(r) && (!a.layout.leftHidden || sysinfo) {
		light[r.ID] = true
	}
	for _, t := range s.tools {
		if r := a.toolSession(t); r != nil && a.sampling(r) {
			light[r.ID], full[r.ID] = true, true
		}
	}
	for id := range s.detailed {
		if !full[id] {
			a.P.WatchMonitorDetail(id, false)
			delete(s.detailed, id)
		}
	}
	for id := range full {
		if !s.detailed[id] {
			a.P.WatchMonitorDetail(id, true)
			s.detailed[id] = true
		}
	}
	for id := range s.watching {
		if !light[id] {
			a.P.WatchMonitor(id, false)
			delete(s.watching, id)
		}
	}
	for id := range light {
		if !s.watching[id] {
			a.P.RefreshMonitor(id)
			a.P.WatchMonitor(id, true)
			s.watching[id] = true
		}
	}
}

// sysinfoPanel is the workbench's 系统信息 tab: what the server is, and its history.
func (a *App) sysinfoPanel(c *ui.Context) {
	sess := a.remote()
	if sess == nil {
		emptyState(c, "系统信息用于远程 SSH 会话", "切换到一个远程标签即可查看系统信息和历史曲线。")
		return
	}
	h := a.monitor.hosts[hostKey(sess)]
	switch {
	case !a.monitorOn(sess):
		emptyState(c, "此服务器的监控已关闭", "在连接设置里开启后，这里会显示系统信息和历史曲线。")
	case a.monitorStopped(sess):
		ui.Column(c).Fill().Padding(24).Center().Children(func() { a.monitorUnreadable(c, sess) })
	case h == nil || h.snap.At.IsZero():
		reading(c)
	default:
		ui.Scroll(c).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Padding(8).Gap(12).Children(func() {
				a.systemInfo(c, sess, h)
				a.monitorHistory(c, h)
			})
		})
	}
}

// systemInfo is the static part: the system, its sizes and its disks.
func (a *App) systemInfo(c *ui.Context, sess *Session, h *hostMonitor) {
	t := c.Theme()
	m := h.snap
	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "系统信息").Bold().Grow(1)
		if a.cached(sess, h) {
			cachedChip(c)
		}
	})
	rows := [][2]string{{"系统", strings.ReplaceAll(m.Info, "\n", " · ")}, {"负载", m.Load}, {"内存", humanSize(int64(m.MemoryTotal))}}
	if m.SwapTotal > 0 {
		rows = append(rows, [2]string{"交换", humanSize(int64(m.SwapTotal))})
	}
	for _, d := range m.Disks {
		rows = append(rows, [2]string{"磁盘 " + d.Mount, fmt.Sprintf("%s / %s（%.0f%%）", humanSize(int64(d.UsedKB)*1024), humanSize(int64(d.TotalKB)*1024), d.UsedPercent)})
	}
	for _, kv := range rows {
		if kv[1] == "" {
			continue
		}
		ui.Row(c).FillWidth().Gap(8).Children(func() {
			ui.Text(c, kv[0]).FontSize(12).TextColor(t.TextMuted).Width(96).SingleLine()
			ui.Text(c, kv[1]).Font("monospace").FontSize(12).Wrap().Selectable().Grow(1).MinWidth(0)
		})
	}
}

func reading(c *ui.Context) {
	ui.Column(c).Fill().Center().Gap(8).Children(func() {
		ui.Spinner(c)
		muted(c, "正在读取服务器状态…")
	})
}

// cachedChip marks what is shown as old data.
func cachedChip(c *ui.Context) ui.Element {
	t := c.Theme()
	return ui.Text(c, "缓存数据").FontSize(11).TextColor(t.Warning).SingleLine().Padding(1, 6).Radius(4).Background(t.Warning.Alpha(.14)).Label("缓存数据")
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

// monitorSidebar is the left column's 系统监控: a section that folds, with bars for CPU, memory, swap and
// disk, load, the network of one interface with its graph, and the buttons of the process and network tabs.
func (a *App) monitorSidebar(c *ui.Context, sess *Session) {
	t := c.Theme()
	h := a.monitor.hosts[hostKey(sess)]
	has := h != nil && !h.snap.At.IsZero()
	cached := has && a.cached(sess, h)
	if has && !cached {
		c.After(time.Until(h.snap.At.Add(cachedAfter)) + 100*time.Millisecond) // flip to 缓存数据 if samples stop
	}
	open := &a.layout.monOpen
	parts := ui.CollapsibleBase(c, open)
	parts.Trigger.FillWidth().Padding(2, 0).Gap(4).AlignItems(ui.Center).Label("系统监控")
	parts.Trigger.Children(func() {
		icon := "chevron-right"
		if *open {
			icon = "chevron-down"
		}
		ui.Icon(c, appIcons[icon]).FontSize(12).TextColor(t.TextMuted)
		sectionLabel(c, "系统监控").Grow(1)
		if cached {
			cachedChip(c)
		}
	})
	parts.Panel(func() { a.monitorBody(c, sess, h, has) }).FillWidth().Gap(10)
}

func (a *App) monitorBody(c *ui.Context, sess *Session, h *hostMonitor, has bool) {
	t := c.Theme()
	switch {
	case sess.Local:
		muted(c, "本地终端不显示服务器状态")
		return
	case !a.monitorOn(sess):
		muted(c, "此服务器的监控已关闭")
		muted(c, "可在连接设置里重新开启。")
		return
	}
	exited := a.P.Exited(sess.ID)
	if exited {
		muted(c, "连接断开后不再更新")
	}
	if a.monitorStopped(sess) {
		a.monitorUnreadable(c, sess)
		return
	}
	if !has {
		if !exited {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c)
				muted(c, "正在读取服务器状态…")
			})
		}
		return
	}
	m := h.snap
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
	a.networkRows(c, h)
	if m.Info != "" {
		muted(c, m.Info).FontSize(11).Tooltip(m.Info)
	}
	if len(m.Warnings) > 0 {
		ui.Text(c, "部分信息无法读取").FontSize(11).TextColor(t.Warning).Tooltip(strings.Join(m.Warnings, "\n"))
	}
	if exited {
		return
	}
	ui.Row(c).FillWidth().Gap(6).Padding(4, 0, 0, 0).Children(func() {
		for _, v := range []struct{ label, icon, kind string }{{"进程管理器", "cpu", toolProcess}, {"网络管理器", "globe", toolNetwork}} {
			b := ui.ButtonBase(c).Label(v.label).Grow(1).Basis(0).Height(28).Gap(4).Radius(t.Radius).Center().Border(1, t.Border)
			if b.Hovered() {
				b.Background(t.SurfaceHover)
			}
			b.Children(func() {
				ui.Icon(c, appIcons[v.icon]).FontSize(13).TextColor(t.TextMuted)
				ui.Text(c, v.label).FontSize(12).SingleLine()
			})
			if b.Clicked() {
				a.openTool(v.kind, sess)
			}
		}
	})
}

// networkRows is the interface choice, its graph and its rates.
func (a *App) networkRows(c *ui.Context, h *hostMonitor) {
	t := c.Theme()
	m := h.snap
	names := make([]string, 0, len(m.Interfaces))
	for name := range m.Interfaces {
		names = append(names, name)
	}
	sort.Strings(names)
	rx, tx, suffix := m.RXPerSecond, m.TXPerSecond, ""
	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		muted(c, "网络").Width(32)
		if len(names) == 0 {
			return
		}
		options := append([]string{allNICs}, names...)
		if !slices.Contains(options, h.nic) {
			h.nic = allNICs
		}
		ui.Select(c, &h.nic, options).Label("网卡").Grow(1).MinWidth(0)
	})
	if r, ok := m.Interfaces[h.nic]; ok {
		rx, tx, suffix = r.RXPerSecond, r.TXPerSecond, ":"+h.nic
	}
	netGraph(c, &h.history, suffix)
	ui.Row(c).FillWidth().Gap(8).Children(func() {
		ui.Text(c, "↑ "+humanSize(int64(tx))+"/s").Font("monospace").FontSize(11).TextColor(metricColor(t, "up")).SingleLine().Grow(1).Basis(0)
		ui.Text(c, "↓ "+humanSize(int64(rx))+"/s").Font("monospace").FontSize(11).TextColor(metricColor(t, "down")).SingleLine().Grow(1).Basis(0)
	})
}

// netGraph draws the last samples' upload and download of one interface (all when suffix is empty), on a
// scale that keeps an idle link flat.
func netGraph(c *ui.Context, h *monitor.History, suffix string) {
	t := c.Theme()
	pts := h.Points
	ui.Box(c).FillWidth().Height(36).Radius(4).Background(t.Background).Border(1, t.Border).Label("网络曲线").Draw(func(p *ui.Painter, r ui.Rect) {
		if len(pts) < 2 {
			return
		}
		ceiling := 16.0 // KiB/s
		for _, pt := range pts {
			ceiling = max(ceiling, nanZero(pt.Value("rx"+suffix)), nanZero(pt.Value("tx"+suffix)))
		}
		x, y := chart.NewTime(pts[0].At, pts[len(pts)-1].At), chart.NewLinear(0, ceiling)
		for _, line := range []struct {
			key   string
			color ui.Color
		}{{"rx" + suffix, metricColor(t, "down")}, {"tx" + suffix, metricColor(t, "up")}} {
			xs, ys := seriesXY(pts, line.key, x, y)
			p.StrokePath(chart.Line(ui.Rect{X: r.X + 1, Y: r.Y + 1, W: r.W - 2, H: r.H - 2}, xs, ys, chart.CurveLinear), 1.5, line.color)
		}
	})
}

func nanZero(v float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	return v
}

// seriesXY is one series of the history as chart positions, with a gap where samples are far apart.
func seriesXY(pts []monitor.Point, key string, x chart.Time, y chart.Linear) (xs, ys []float32) {
	for j, point := range pts {
		if j > 0 && point.At.Sub(pts[j-1].At) > 10*time.Second {
			xs = append(xs, x.At(point.At.Add(-time.Nanosecond)))
			ys = append(ys, float32(math.NaN()))
		}
		xs = append(xs, x.At(point.At))
		ys = append(ys, y.At(point.Value(key)))
	}
	return xs, ys
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
		ui.Meter(c, clampRatio(percent/100)*100, 0, 100, meterLevels(c.Theme(), color)).Label(name).FillWidth().Height(6).Radius(3)
	})
}

func (a *App) monitorHistory(c *ui.Context, host *hostMonitor) {
	s := &a.monitor
	h := &host.history
	if len(h.Points) == 0 {
		muted(c, "还没有历史数据")
		return
	}
	muted(c, fmt.Sprintf("最近 %d 次读数，不保存到磁盘", len(h.Points)))
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
				xs, ys := seriesXY(h.Points, s.Key, x, y)
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
						a.confirmDanger(fmt.Sprintf("向进程 %d 发送 %s", v.PID, sig), v.Command+"\n这可能会终止或暂停服务器上的程序。", "发送 "+sig, func() { a.P.SignalProcess(id, v.PID, sig) })
					})
				}
			}
		})
	}
	cols := []ui.TableColumn{{Title: "PID", Width: 80, Sortable: true}, {Title: "CPU", Width: 68, Sortable: true, Align: ui.End}, {Title: "内存", Width: 68, Sortable: true, Align: ui.End}, {Title: "命令", Sortable: true}}
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

// connState is a connection's state as ss or netstat name it, in words; one it does not know stays as it is.
func connState(s string) string {
	switch strings.ReplaceAll(strings.ToUpper(s), "_", "-") {
	case "LISTEN", "UNCONN":
		return "监听"
	case "ESTAB", "ESTABLISHED":
		return "已建立"
	case "SYN-SENT":
		return "正在连接"
	case "SYN-RECV":
		return "正在接入"
	case "FIN-WAIT-1", "FIN-WAIT-2", "FIN-WAIT1", "FIN-WAIT2", "LAST-ACK", "CLOSING":
		return "正在关闭"
	case "CLOSE-WAIT":
		return "等待关闭"
	case "TIME-WAIT":
		return "等待回收"
	case "CLOSED":
		return "已关闭"
	}
	return s
}
// hiddenProcess is why a connection has no process: ss and netstat name only the login user's own.
const hiddenProcess = "进程不可见：服务器只向 root 显示其他用户的进程"

// processLabel is a connection's "PID name", or a dash when the server did not tell.
func processLabel(v monitor.Network) string {
	if v.PID == 0 {
		return "—"
	}
	return strings.TrimSpace(fmt.Sprintf("%d %s", v.PID, v.Process))
}

func (a *App) networkTable(c *ui.Context, network []monitor.Network) {
	s := &a.monitor
	ui.SearchField(c, &s.netFilter).Label("过滤网络连接").Placeholder("端口、地址或进程").Margin(4, 8)
	var rows []monitor.Network
	q := strings.ToLower(strings.TrimSpace(s.netFilter))
	for _, v := range network {
		if strings.Contains(strings.ToLower(fmt.Sprintf("%s %s %s %s:%d %s:%d %d %s", v.Protocol, v.State, connState(v.State), v.LocalIP, v.LocalPort, v.RemoteIP, v.RemotePort, v.PID, v.Process)), q) {
			rows = append(rows, v)
		}
	}
	s.networkList.Selected = &s.networkRow
	s.networkList.Key = func(i int) any {
		v := rows[i]
		return fmt.Sprintf("%s|%s|%d|%s|%d|%d", v.Protocol, v.LocalIP, v.LocalPort, v.RemoteIP, v.RemotePort, v.PID)
	}
	cols := []ui.TableColumn{{Title: "协议", Width: 108}, {Title: "本地"}, {Title: "远端"}, {Title: "进程", Width: 90}}
	ui.Table(c.Key("network-table"), &s.networkList, cols, len(rows), func(row, col int) {
		v := rows[row]
		text := []string{v.Protocol + " " + connState(v.State), fmt.Sprintf("%s:%d", v.LocalIP, v.LocalPort), fmt.Sprintf("%s:%d", v.RemoteIP, v.RemotePort), processLabel(v)}[col]
		tip := fmt.Sprintf("%d 个连接 · %d 个对端", v.Connections, v.UniqueIPs)
		if col == 3 && v.PID == 0 {
			tip = hiddenProcess
		}
		ui.Text(c, text).SingleLine().FontSize(12).Tooltip(tip)
	}).Grow(1).FillWidth()
	count := fmt.Sprintf("%d / %d 条", len(rows), len(network))
	if slices.ContainsFunc(network, func(v monitor.Network) bool { return v.PID == 0 }) {
		count += " · 显示为 — 的进程属于其他用户，需以 root 登录才能看到"
	}
	muted(c, count).Padding(4, 10)
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
					a.confirmDanger(fmt.Sprintf("向进程 %d 发送 TERM", v.PID), v.Command+"\n这可能会终止服务器上的程序。", "发送 TERM", func() { a.P.SignalProcess(id, v.PID, "TERM") })
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
