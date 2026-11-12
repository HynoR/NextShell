package appui

import (
	"slices"
	"sort"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/model"
)

// A tool tab is the process or network manager of one host, a tab beside the host's sessions as in 1.x.
// It reads the host's current session; the full probe runs while the tab exists, and closing it stops it.
type toolTab struct{ kind, host string } // host is a hostKey

const (
	toolProcess = "process"
	toolNetwork = "network"
)

func (t toolTab) key() string { return t.kind + ":" + t.host }
func (t toolTab) icon() string {
	if t.kind == toolNetwork {
		return "globe"
	}
	return "cpu"
}

// toolSession is the session a tool tab samples: its host's connected session, else any of them.
func (a *App) toolSession(t toolTab) *Session {
	var found *Session
	for _, s := range a.Tabs {
		if !s.Local && hostKey(s) == t.host {
			if !a.P.Exited(s.ID) {
				return s
			}
			if found == nil {
				found = s
			}
		}
	}
	return found
}

func (a *App) toolTitle(t toolTab) string {
	name := t.host
	if s := a.toolSession(t); s != nil {
		name = orDefault(s.Connection.Name, s.Title)
	}
	if t.kind == toolNetwork {
		return "网络 · " + name
	}
	return "进程 · " + name
}

// openTool opens (or shows) the host's tool tab.
func (a *App) openTool(kind string, s *Session) {
	t := toolTab{kind, hostKey(s)}
	if !slices.Contains(a.monitor.tools, t) {
		a.monitor.tools = append(a.monitor.tools, t)
	}
	a.showTool(t)
}
func (a *App) showTool(t toolTab) {
	if s := a.toolSession(t); s != nil {
		a.Active = a.tabIndex(s.ID)
		a.monitor.tool = t.key()
	}
}
func (a *App) closeTool(t toolTab) {
	a.monitor.tools = slices.DeleteFunc(a.monitor.tools, func(o toolTab) bool { return o == t })
	if a.monitor.tool == t.key() {
		a.monitor.tool = ""
	}
}

// dropTools closes the tool tabs whose host has no session left.
func (a *App) dropTools() {
	for _, t := range slices.Clone(a.monitor.tools) {
		if a.toolSession(t) == nil {
			a.closeTool(t)
		}
	}
}

// activeTool is the tool tab shown, or nil while a terminal shows: it shows only with its host's session active.
func (a *App) activeTool() *toolTab {
	for _, t := range a.monitor.tools {
		if t.key() == a.monitor.tool {
			if s := a.current(); s != nil && hostKey(s) == t.host {
				return &t
			}
		}
	}
	a.monitor.tool = ""
	return nil
}

// toolTabs are the tool tabs of the strip, after the sessions'; first is the index of the first.
func (a *App) toolTabs(c *ui.Context, tabs ui.TabsParts, first int) {
	t := c.Theme()
	for n, tool := range slices.Clone(a.monitor.tools) {
		on := a.monitor.tool == tool.key()
		title := a.toolTitle(tool)
		tab := tabs.Tab(first+n).Label(title).Height(28).Padding(0, 6, 0, 10).Gap(6).Radius(t.Radius).AlignItems(ui.Center).Tooltip(title)
		tab.ContextMenu(func(m *ui.Menu) {
			if m.Item("关闭").Chosen() {
				a.later(func() { a.closeTool(tool) })
			}
		})
		switch {
		case on:
			tab.Background(t.Background).Border(1, t.Border)
		case tab.Hovered():
			tab.Background(t.SurfaceHover)
		}
		tab.Children(func() {
			color := t.TextMuted
			if on {
				color = t.Text
			}
			ui.Icon(c, appIcons[tool.icon()]).FontSize(13).TextColor(color)
			ui.Text(c, title).SingleLine().MaxWidth(180).FontSize(13).TextColor(color)
			x := ui.ButtonBase(c).Label("关闭标签 "+title).Tooltip("关闭标签").Size(18, 18).Radius(4).Center()
			if x.Hovered() {
				x.Background(t.SurfacePressed)
			}
			x.Children(func() { ui.Icon(c, appIcons["close"]).FontSize(12).TextColor(t.TextMuted) })
			if x.Clicked() {
				a.later(func() { a.closeTool(tool) })
			}
		})
	}
}

// toolView is the centre column while a tool tab shows: the host's processes or connections.
func (a *App) toolView(c *ui.Context, tool toolTab) {
	t := c.Theme()
	s := a.toolSession(tool)
	h := a.monitor.hosts[tool.host]
	ui.Column(c).Fill().MinWidth(0).MinHeight(0).Background(t.Background).Children(func() {
		ui.Row(c).FillWidth().Padding(8, 10, 2, 10).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, appIcons[tool.icon()]).FontSize(15).TextColor(t.TextMuted)
			ui.Text(c, a.toolTitle(tool)).Bold().SingleLine().Grow(1).MinWidth(0)
			if h != nil && !h.full.At.IsZero() && (s == nil || a.cached(s, h) || !a.sampling(s)) {
				cachedChip(c)
			}
		})
		switch {
		case s != nil && !a.monitorOn(s):
			emptyState(c, "此服务器的监控已关闭", "在连接设置里开启后，才能查看进程和网络连接。")
		case s != nil && a.monitorStopped(s):
			ui.Column(c).Fill().Padding(24).Center().Children(func() { a.monitorUnreadable(c, s) })
		case h == nil || h.full.At.IsZero():
			reading(c)
		case tool.kind == toolNetwork:
			a.networkTable(c, h.full.Network)
		default:
			id := ""
			if s != nil {
				id = s.ID
			}
			a.processTable(c, id, h.full)
		}
	})
}

// pendingTabs are the saved hosts connecting for the first time: no tab of theirs exists yet.
func (a *App) pendingTabs() []model.Connection {
	var out []model.Connection
	for id := range a.hosts.connecting {
		if cc, ok := a.host(id); ok && !slices.ContainsFunc(a.Tabs, func(s *Session) bool { return s.Connection.ID == id }) {
			out = append(out, cc)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// pendingStrip shows a 连接中 tab for each of them, until the session replaces it.
func (a *App) pendingStrip(c *ui.Context, pending []model.Connection) {
	t := c.Theme()
	for _, cc := range pending {
		ui.Row(c.Key("pending-"+cc.ID)).Height(28).Padding(0, 10).Gap(6).Radius(t.Radius).AlignItems(ui.Center).Background(t.Background).Border(1, t.Border).Label("连接中 " + cc.Name).Children(func() {
			ui.Box(c).Size(3, 14).Radius(1.5).Background(hostColor(&Session{Connection: cc}))
			ui.Box(c).Size(6, 6).Radius(3).Background(t.Warning)
			ui.Text(c, cc.Name).SingleLine().MaxWidth(180).FontSize(13)
			ui.Text(c, "连接中").FontSize(11).TextColor(t.Warning).SingleLine()
		})
	}
}

// connectingView fills the centre column while the first tab is still connecting.
func (a *App) connectingView(c *ui.Context, pending []model.Connection) {
	ui.Column(c).Grow(1).FillWidth().MinHeight(0).Center().Gap(10).Children(func() {
		ui.Spinner(c)
		ui.Text(c, "正在连接 "+pending[0].Name+"…").Bold()
		muted(c, pending[0].User+"@"+pending[0].Address())
	})
}
