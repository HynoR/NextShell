package appui

import (
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/tabswitch"
)

type tabsState struct {
	mru                  tabswitch.MRU
	started              map[string]time.Time
	renaming, renameText string
	term                 ui.Handle
}

type tabDrag string

func (a *App) tabStrip(c *ui.Context) {
	t := c.Theme()
	ui.ScrollHorizontal(c).Height(38).MinWidth(0).Shrink(1).Children(func() {
		if len(a.Tabs) == 0 {
			return
		}
		tabs := ui.TabsBase(c, &a.Active, len(a.Tabs))
		tabs.List.Height(38).AlignItems(ui.Center).Gap(2).Padding(5, 0).Children(func() {
			for i, s := range a.Tabs {
				exited := a.P.Exited(s.ID)
				tab := tabs.Tab(i).Label(s.Title).Height(28).Padding(0, 6, 0, 10).Gap(6).Radius(t.Radius).AlignItems(ui.Center).Drag(tabDrag(s.ID)).Tooltip(a.tabTooltip(s))
				a.tabMenu(tab, s)
				if x, ok := ui.Drop[tabDrag](tab); ok {
					a.moveTab(string(x), i)
				}
				if tab.DoubleClicked() {
					a.startRename(s)
				}
				switch {
				case i == a.Active:
					tab.Background(t.Background).Border(1, t.Border)
				case tab.Hovered():
					tab.Background(t.SurfaceHover)
				}
				tab.Children(func() {
					color := t.Text
					if exited || i != a.Active {
						color = t.TextMuted
					}
					if exited {
						ui.Text(c, "⟳").FontSize(12).TextColor(t.TextMuted)
					}
					if a.tabs.renaming == s.ID {
						in := ui.TextInput(c.Key("rename-"+s.ID), &a.tabs.renameText).Label("标签名称").Width(140).AutoFocus().OnTextKey(func(k ui.TextKey) bool {
							if k.Key == ui.KeyEscape {
								a.tabs.renaming = ""
								return true
							}
							return false
						})
						if in.Submitted() || in.FocusLeft() {
							a.finishRename(s)
						}
					} else {
						ui.Text(c, s.Title).SingleLine().MaxWidth(180).FontSize(13).TextColor(color)
					}
					x := ui.ButtonBase(c).Label("关闭标签 "+s.Title).Tooltip("关闭标签").Size(18, 18).Radius(4).Center()
					if x.Hovered() {
						x.Background(t.SurfacePressed)
					}
					x.Children(func() { ui.Icon(c, appIcons["close"]).FontSize(12).TextColor(t.TextMuted) })
					if x.Clicked() {
						index := i
						a.later(func() { a.closeTab(index) })
					}
				})
			}
		})
		if tabs.List.Changed() {
			a.activateTab(a.Active)
		}
	})
}
func (a *App) tabTooltip(s *Session) string {
	if s.Local {
		return "本地终端"
	}
	return s.Connection.User + "@" + s.Connection.Address()
}

func (a *App) tabMenu(item ui.Element, s *Session) {
	item.ContextMenu(func(m *ui.Menu) {
		if m.Item("重命名").Chosen() {
			a.later(func() { a.startRename(s) })
		}
		if s.ManualTitle && m.Item("恢复自动标题").Chosen() {
			a.later(func() {
				s.ManualTitle = false
				s.Title = s.Connection.Name
				if s.Local {
					s.Title = "本地终端"
				}
				a.refreshTitles()
			})
		}
		if m.Item("复制会话").Chosen() {
			a.later(func() { a.P.CloneSession(s) })
		}
		if m.Item("重新连接").Chosen() {
			a.later(func() { a.P.Reconnect(s) })
		}
		m.Separator()
		if m.Item("清屏").Chosen() {
			s.Terminal.ClearScreen()
		}
		if m.Item("清除滚动缓冲").Chosen() {
			s.Terminal.ClearScrollback()
		}
		m.Separator()
		if m.Item("关闭").Chosen() {
			a.later(func() { a.closeTabID(s.ID) })
		}
		if m.Item("关闭其他").Disabled(len(a.Tabs) < 2).Chosen() {
			a.later(func() { a.closeWhere(func(i int, o *Session) bool { return o != s }) })
		}
		idx := a.tabIndex(s.ID)
		if m.Item("关闭右侧").Disabled(idx == len(a.Tabs)-1).Chosen() {
			a.later(func() { a.closeWhere(func(i int, _ *Session) bool { return i > a.tabIndex(s.ID) }) })
		}
	})
}
func (a *App) startRename(s *Session) {
	a.tabs.renaming = s.ID
	a.tabs.renameText = s.Title
}
func (a *App) finishRename(s *Session) {
	if a.tabs.renaming != s.ID {
		return
	}
	a.tabs.renaming = ""
	if name := strings.TrimSpace(a.tabs.renameText); name != "" && name != s.Title {
		s.Title = name
		s.ManualTitle = true
	}
}

func (a *App) tabIDs() []string {
	ids := make([]string, 0, len(a.Tabs))
	for _, s := range a.Tabs {
		ids = append(ids, s.ID)
	}
	return ids
}
func (a *App) tabIndex(id string) int {
	for i, s := range a.Tabs {
		if s.ID == id {
			return i
		}
	}
	return -1
}
func (a *App) selectTabID(id string) {
	if i := a.tabIndex(id); i >= 0 {
		a.activateTab(i)
	}
}
func (a *App) activateTab(i int) {
	if i >= 0 && i < len(a.Tabs) {
		a.Active = i
		a.tabs.term.Focus()
	}
}
func (a *App) closeTabID(id string) { a.closeTab(a.tabIndex(id)) }
func (a *App) closeTab(i int) {
	if i < 0 || i >= len(a.Tabs) {
		return
	}
	id := a.Tabs[i].ID
	a.monitor.forget(id)
	delete(a.tabs.started, id)
	a.P.CloseSession(id)
	a.Tabs = append(a.Tabs[:i], a.Tabs[i+1:]...)
	switch {
	case len(a.Tabs) == 0:
		a.Active = -1
	case i < a.Active:
		a.Active--
	case a.Active >= len(a.Tabs):
		a.Active = len(a.Tabs) - 1
	}
}

// closeWhere closes the tabs for which close reports true, from the right.
func (a *App) closeWhere(close func(i int, s *Session) bool) {
	for i := len(a.Tabs) - 1; i >= 0; i-- {
		if close(i, a.Tabs[i]) {
			a.closeTab(i)
		}
	}
}
func (a *App) moveTab(id string, to int) {
	from := a.tabIndex(id)
	if from < 0 || to < 0 || to >= len(a.Tabs) || from == to {
		return
	}
	active := a.activeID()
	s := a.Tabs[from]
	a.Tabs = append(a.Tabs[:from], a.Tabs[from+1:]...)
	a.Tabs = append(a.Tabs, nil)
	copy(a.Tabs[to+1:], a.Tabs[to:])
	a.Tabs[to] = s
	a.Active = a.tabIndex(active)
}

// refreshTitles follows the titles programs set, unless the user named the tab.
func (a *App) refreshTitles() {
	for _, s := range a.Tabs {
		if s.ManualTitle || !a.Prefs.TitleUpdates {
			continue
		}
		if next := a.P.TerminalTitle(s.ID); next != "" && next != s.Title {
			s.Title = next
		}
	}
}

func (a *App) mruShortcuts(c *ui.Context) {
	ids := a.tabIDs()
	current := a.activeID()
	m := &a.tabs.mru
	m.Sync(ids, current)
	if m.Active() && !c.WindowActive() {
		m.Cancel(ids)
		return
	}
	if m.Active() && (c.Shortcut(0, ui.KeyEscape) || c.Shortcut(ui.Ctrl, ui.KeyEscape)) {
		m.Cancel(ids)
		return
	}
	forward := c.Shortcut(ui.Ctrl, ui.KeyTab)
	reverse := c.Shortcut(ui.Ctrl|ui.Shift, ui.KeyTab)
	if m.Active() {
		forward = forward || c.Shortcut(ui.Ctrl, ui.KeyDown)
		reverse = reverse || c.Shortcut(ui.Ctrl, ui.KeyUp)
	}
	if forward || reverse {
		m.Step(ids, current, reverse)
	}
	if m.Active() && c.Modifiers()&ui.Ctrl == 0 {
		a.selectTabID(m.Commit())
	}
}

// mruOverlay lists the recent tabs while Ctrl is held, with the last lines of the one chosen.
func (a *App) mruOverlay(c *ui.Context) {
	t := c.Theme()
	ui.Overlay(c, func() {
		w, _ := c.Size()
		ui.Column(c).Absolute().Top(60).Left(max(w/2-260, 16)).Width(520).Padding(8).Gap(2).Radius(10).Background(t.Surface).Border(1, t.Border).Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, .25)).Children(func() {
			muted(c, "最近使用 · 松开 Ctrl 切换 · Esc 取消").Padding(4, 8)
			for _, id := range a.tabs.mru.Cycle {
				i := a.tabIndex(id)
				if i < 0 {
					continue
				}
				row := ui.Row(c).Key("mru-"+id).Padding(6, 8).Radius(6)
				if id == a.tabs.mru.Preview() {
					row.Background(t.Accent.Alpha(.15))
				}
				row.Children(func() { ui.Text(c, a.Tabs[i].Title).SingleLine() })
			}
			if lines := lastLines(a.P.ScreenText(a.tabs.mru.Preview()), 6); lines != "" {
				ui.Text(c, lines).Font("monospace").FontSize(11).TextColor(t.TextMuted).Padding(8).FillWidth().Background(t.Background).Radius(6)
			}
		})
	})
}
func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n "), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
