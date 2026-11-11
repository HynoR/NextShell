package appui

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/chart"
	"math"
	"nextshell.dev/v2/internal/monitor"
	"sort"
	"strings"
	"time"
)

func (a *App) recordMonitorHistory(id string, snap monitor.Snapshot) {
	s := &a.Phase2
	if s.MonitorHistories == nil {
		s.MonitorHistories = map[string]*monitor.History{}
	}
	h := s.MonitorHistories[id]
	if h == nil {
		if len(s.MonitorHistories) >= 32 {
			oldest := ""
			var at time.Time
			for key, history := range s.MonitorHistories {
				if len(history.Points) == 0 {
					oldest = key
					break
				}
				last := history.Points[len(history.Points)-1].At
				if at.IsZero() || last.Before(at) {
					at = last
					oldest = key
				}
			}
			delete(s.MonitorHistories, oldest)
		}
		h = &monitor.History{}
		s.MonitorHistories[id] = h
	}
	h.Append(snap)
}
func (a *App) monitorHistoryView(c *ui.Context, id string) {
	s := &a.Phase2
	h := s.MonitorHistories[id]
	if h == nil || len(h.Points) == 0 {
		ui.Text(c, "还没有此会话的历史；刷新监控或启用每 2 秒刷新")
		return
	}
	ui.Text(c, fmt.Sprintf("最近 %d 个真实采样 · 每会话最多 50 点 · 超过 10 秒间隔断线 · 不持久化", len(h.Points))).FontSize(12).TextColor(c.Theme().TextMuted)
	interfaces, disks := []string{"全部网卡"}, []string{}
	knownNet, knownDisk := map[string]bool{}, map[string]bool{}
	for _, p := range h.Points {
		for key := range p.Values {
			if strings.HasPrefix(key, "rx:") {
				n := strings.TrimPrefix(key, "rx:")
				if !knownNet[n] {
					knownNet[n] = true
					interfaces = append(interfaces, n)
				}
			}
			if strings.HasPrefix(key, "disk:") {
				n := strings.TrimPrefix(key, "disk:")
				if !knownDisk[n] {
					knownDisk[n] = true
					disks = append(disks, n)
				}
			}
		}
	}
	sort.Strings(interfaces[1:])
	sort.Strings(disks)
	if s.HistoryInterface == "" || s.HistoryInterface != "全部网卡" && !knownNet[s.HistoryInterface] {
		s.HistoryInterface = "全部网卡"
	}
	if !knownDisk[s.HistoryDisk] && len(disks) > 0 {
		s.HistoryDisk = disks[0]
		if knownDisk["/"] {
			s.HistoryDisk = "/"
		}
	}
	ui.Row(c).Gap(12).Children(func() {
		ui.Select(c, &s.HistoryInterface, interfaces).Label("历史网卡").Width(190)
		if len(disks) > 0 {
			ui.Select(c, &s.HistoryDisk, disks).Label("历史磁盘挂载点").Width(260)
		}
	})
	a.historyPlot(c, "CPU · %", h, []historySeries{{"cpu", "CPU"}}, 100)
	a.historyPlot(c, "内存 / Swap · %", h, []historySeries{{"memory", "内存"}, {"swap", "Swap"}}, 100)
	suffix := ""
	if s.HistoryInterface != "全部网卡" {
		suffix = ":" + s.HistoryInterface
	}
	a.historyPlot(c, "网络 · KiB/s（不含 lo）", h, []historySeries{{"rx" + suffix, "接收"}, {"tx" + suffix, "发送"}}, 0)
	if len(disks) > 0 {
		a.historyPlot(c, "磁盘容量占用 · %（不是 I/O）", h, []historySeries{{"disk:" + s.HistoryDisk, s.HistoryDisk}}, 100)
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
		ui.Row(c).Gap(12).Children(func() {
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
		chart.Plot(c, chart.Axes{Left: y.Ticks(4), Bottom: x.Ticks(5), Label: func(_ chart.Side, tick chart.Tick) {
			ui.Text(c, tick.Label).NoWrap().FontSize(10).TextColor(t.TextMuted)
		}}, func(area ui.Element) {
			type line struct {
				xs, ys []float32
				color  ui.Color
			}
			lines := []line{}
			for i, s := range series {
				xs, ys := []float32{}, []float32{}
				for j, point := range h.Points {
					if j > 0 && point.At.Sub(h.Points[j-1].At) > 10*time.Second {
						xs = append(xs, x.At(point.At.Add(-time.Nanosecond)))
						ys = append(ys, float32(math.NaN()))
					}
					xs = append(xs, x.At(point.At))
					ys = append(ys, y.At(point.Value(s.Key)))
				}
				color := colors[i%len(colors)]
				lines = append(lines, line{xs, ys, color})
				for j, point := range h.Points {
					if v := point.Value(s.Key); !math.IsNaN(v) {
						chart.Point(c.Key(fmt.Sprintf("%s-%d", s.Key, j)), x.At(point.At), y.At(v), 5, 5).Radius(3).Background(color).Tooltip(point.At.Local().Format("15:04:05") + fmt.Sprintf(" · %s %.2f", s.Name, v))
					}
				}
			}
			area.Draw(func(p *ui.Painter, r ui.Rect) {
				for _, tick := range y.Ticks(4) {
					p.Line(r.X, chart.Y(r, tick.At), r.X+r.W, chart.Y(r, tick.At), 1, t.Border)
				}
				for _, line := range lines {
					p.StrokePath(chart.Line(r, line.xs, line.ys, chart.CurveLinear), 2, line.color)
				}
			})
		}).Label(title).Height(136).FillWidth().Gap(6)
	})
}
