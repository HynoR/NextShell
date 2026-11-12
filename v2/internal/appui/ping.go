package appui

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

// traceState is the route-trace drawer: the host traced and what the trace printed.
type traceState struct {
	open, busy       bool
	name, host, text string
	id               string // the diagnostic the text answers
}

// pingCard is the left column's 延迟 line: the round trip to the server, which opens the route trace.
func (a *App) pingCard(c *ui.Context, s *Session) {
	if s.Local {
		return
	}
	t := c.Theme()
	value, color := "—", t.TextMuted
	if h := a.monitor.hosts[hostKey(s)]; h != nil && h.latency > 0 && a.sampling(s) && !a.cached(s, h) {
		ms := float64(h.latency) / float64(time.Millisecond)
		value = fmt.Sprintf("%.0f ms", max(ms, 1))
		switch {
		case ms < 100:
			color = t.Success
		case ms < 300:
			color = t.Warning
		default:
			color = t.Danger
		}
	}
	b := ui.ButtonBase(c).Label("PING 延迟 "+value+"，点击路由追踪").Tooltip("点击对这台服务器做路由追踪").FillWidth().Padding(10, 12).Gap(8).AlignItems(ui.Center).BorderColor(t.Border).BorderWidth(1, 0, 0, 0)
	if b.Hovered() {
		b.Background(t.SurfaceHover)
	}
	b.Children(func() {
		ui.Box(c).Size(8, 8).Radius(4).Background(color)
		sectionLabel(c, "PING 延迟").Grow(1)
		ui.Text(c, value).Font("monospace").FontSize(12).Bold().TextColor(color).SingleLine()
		ui.Icon(c, appIcons["chevron-right"]).FontSize(12).TextColor(t.TextMuted)
	})
	if b.Clicked() {
		a.openTrace(s)
	}
}

func (a *App) openTrace(s *Session) {
	tr := &a.monitor.trace
	tr.open, tr.name, tr.host = true, orDefault(s.Connection.Name, s.Title), s.Connection.Host
	a.startTrace()
}
func (a *App) startTrace() {
	tr := &a.monitor.trace
	tr.id = a.P.Diagnose("traceroute", tr.host)
	tr.busy, tr.text = tr.id != "", ""
}

// traceDrawer slides over the main window beside the left column while a route trace is open.
func (a *App) traceDrawer(c *ui.Context) {
	tr := &a.monitor.trace
	if !tr.open {
		return
	}
	t := c.Theme()
	if c.Shortcut(0, ui.KeyEscape) {
		tr.open = false
		a.P.CancelDiagnostic()
		return
	}
	ui.Overlay(c, func() {
		ui.Column(c).Absolute().Top(max(c.TitleBar().Height, 44)).Bottom(0).Left(leftWidth).Width(440).Padding(14).Gap(10).Background(t.Surface).BorderColor(t.Border).BorderWidth(0, 1, 0, 1).Shadow(8, 0, 24, 0, ui.RGBA(0, 0, 0, .25)).Label("路由追踪").Children(func() {
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "路由追踪 · "+tr.name).Bold().SingleLine().Grow(1).MinWidth(0)
				if iconButton(c, "关闭路由追踪", "close").Clicked() {
					tr.open = false
					a.P.CancelDiagnostic()
				}
			})
			muted(c, "从本机到 "+tr.host+" 经过的节点。")
			if tr.busy {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Spinner(c)
					muted(c, "正在追踪路由…")
				})
			}
			missing := !tr.busy && (strings.Contains(tr.text, "executable file not found") || strings.Contains(tr.text, "no such file or directory"))
			if missing {
				ui.Text(c, "未找到 nexttrace。请先安装 nexttrace，再到「设置 → 网络工具」指定它的路径。").FillWidth()
			}
			ui.Scroll(c).Grow(1).FillWidth().Children(func() {
				if tr.text != "" && !missing {
					ui.Text(c, tr.text).Font("monospace").FontSize(12).Wrap().Selectable().FillWidth().Padding(8).Radius(6).Background(t.Background).Label("路由追踪结果")
				}
			})
			ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
				if missing && ui.Button(c, "打开设置").Clicked() {
					a.later(func() { a.openSettings("网络工具") })
				}
				if tr.busy && ui.Button(c, "停止").Clicked() {
					a.P.CancelDiagnostic()
				}
				if !tr.busy && ui.PrimaryButton(c, "重新追踪").Clicked() {
					a.startTrace()
				}
			})
		})
	})
}
