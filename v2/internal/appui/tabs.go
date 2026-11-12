package appui

import (
	"fmt"
	"slices"
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
	restored             bool
	ask                  *closeAsk // a close waiting for the user's answer
	shown, reveal        string    // the tab last scrolled to; the one to scroll to in this frame, if it changed
	middle, held         string    // the tab the middle button was held on in the last frame, and in this one
}

// closeAsk is a close of session tabs that asks first: some are connected, or leave unsaved files.
type closeAsk struct {
	ids   []string
	live  int               // connected SSH tabs among them
	dirty []*remoteDocument // the unsaved files that go with them
	never bool              // 不再询问 ticked
}

// ExitNeedsConfirm says whether closing the main window should ask first.
func (a *App) ExitNeedsConfirm() bool {
	return !a.ExitConfirmed && (a.DirtyDocumentCount() > 0 || len(a.Tabs) > 0 && !a.Prefs.NoCloseConfirm)
}

// SaveOpenTabs remembers the open hosts and local shells for the next start.
func (a *App) SaveOpenTabs() {
	var ids []string
	for _, s := range a.Tabs {
		switch {
		case s.Local:
			ids = append(ids, "local")
		case s.Connection.ID != "":
			ids = append(ids, s.Connection.ID)
		}
	}
	a.report(a.P.SaveUIState("tabs", strings.Join(ids, "\n")), "")
}

// restoreTabs reopens the last run's tabs once, when the preference asks.
func (a *App) restoreTabs() {
	if a.tabs.restored {
		return
	}
	a.tabs.restored = true
	if !a.Prefs.RestoreTabs {
		return
	}
	for _, id := range strings.Fields(a.P.UIState("tabs")) {
		if id == "local" {
			a.P.Local()
		} else if cc, ok := a.host(id); ok && !cc.Deleted {
			a.connect(cc)
		}
	}
}

type tabDrag string

// tabBar heads the centre column: the tabs, and a button for a new local terminal or a recent host.
func (a *App) tabBar(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).FillWidth().Height(38).Padding(0, 8).Gap(4).AlignItems(ui.Center).Background(a.pane(t)).BorderColor(t.Border).BorderWidth(0, 0, 1, 0).Children(func() {
		a.tabStrip(c)
		a.pendingStrip(c, a.pendingTabs())
		a.plusMenu(c)
		ui.Spacer(c).Grow(1)
	})
}

// plusMenu is the 「+」: a new local terminal, a recent host, or the connection manager.
func (a *App) plusMenu(c *ui.Context) {
	iconButton(c, "新建标签", "plus").PopoverMenuButton(func(m *ui.Menu) {
		if m.Item("本地终端").Shortcut(appMod, ui.KeyT).Chosen() {
			a.later(a.P.Local)
		}
		if recent := a.hosts.recentHosts(a, 10); len(recent) > 0 {
			m.Separator()
			m.Item("最近连接").Disabled(true) // a heading for the hosts below
			for _, cc := range recent {
				if m.Item(cc.Name).ID(cc.ID).Subtitle(a.login(cc)).Chosen() {
					a.later(func() { a.connect(cc) })
				}
			}
		}
		m.Separator()
		if m.Item("打开连接管理器…").Chosen() {
			a.later(a.openManager)
		}
	})
}

func (a *App) tabStrip(c *ui.Context) {
	t := c.Theme()
	ui.ScrollHorizontal(c).Height(38).MinWidth(0).Shrink(1).Children(func() {
		docs := a.documentsIn(false)
		if len(a.Tabs) == 0 && len(docs) == 0 {
			return
		}
		// The widget keeps a selection inside the strip; a window with tabs but none active (an agent opened
		// the only one in the background) must stay that way, so it gets a copy and a.Active only follows a click.
		selected := a.Active
		tool := a.activeTool() // the tool tabs follow the sessions' tabs, and the documents' the tools'
		doc := a.activeEditor()
		switch {
		case doc != nil:
			selected = len(a.Tabs) + len(a.monitor.tools) + slices.Index(docs, doc)
		case tool != nil:
			selected = len(a.Tabs) + slices.Index(a.monitor.tools, *tool)
		}
		other := tool != nil || doc != nil
		a.tabs.reveal = ""
		if key := a.activeTabKey(); key != a.tabs.shown { // scrolled to once, when it changes: not again, to leave the scrolling to the user
			a.tabs.reveal, a.tabs.shown = key, key
		}
		defer func() { a.tabs.middle, a.tabs.held = a.tabs.held, "" }()
		tabs := ui.TabsBase(c, &selected, len(a.Tabs)+len(a.monitor.tools)+len(docs))
		tabs.List.Height(38).AlignItems(ui.Center).Gap(2).Padding(5, 0).Children(func() {
			for i, s := range a.Tabs {
				exited := a.P.Exited(s.ID)
				title := a.tabTitle(s)
				tab := tabs.Tab(i).Label(title).Height(28).Padding(0, 6, 0, 10).Gap(6).Radius(t.Radius).AlignItems(ui.Center).Drag(tabDrag(s.ID)).Tooltip(a.tabTooltip(s))
				if a.tabs.reveal == s.ID {
					tab.ScrollIntoView()
				}
				if a.middleClicked(tab, s.ID) {
					a.later(func() { a.requestClose(s.ID) })
				}
				a.tabMenu(tab, s)
				if x, ok := ui.Drop[tabDrag](tab); ok {
					a.moveTab(string(x), i)
				}
				if a.Active < 0 && tab.Clicked() { // the strip's own selection cannot be "none": the click picks the tab
					a.later(func() { a.activateTab(i) })
				}
				if tab.DoubleClicked() {
					a.startRename(s)
				}
				switch {
				case i == a.Active && !other:
					tab.Background(t.Background).Border(1, t.Border)
				case tab.Hovered():
					tab.Background(t.SurfaceHover)
				}
				tab.Children(func() {
					color := t.Text
					if exited || i != a.Active || other {
						color = t.TextMuted
					}
					if !s.Local {
						ui.Box(c).Size(3, 14).Radius(1.5).Background(a.hostColor(s.Connection))
					}
					label, status := a.sessionStatus(c, s)
					ui.Box(c).Size(6, 6).Radius(3).Background(status).Label("状态 " + label)
					a.agentBadge(c, s)
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
						ui.Text(c, title).SingleLine().MaxWidth(180).FontSize(13).TextColor(color)
					}
					if exited && label != "连接中" {
						r := ui.ButtonBase(c).Label("重新连接 "+title).Tooltip("重新连接").Size(18, 18).Radius(4).Center()
						if r.Hovered() {
							r.Background(t.SurfacePressed)
						}
						r.Children(func() { ui.Icon(c, appIcons["refresh"]).FontSize(12).TextColor(t.Warning) })
						if r.Clicked() {
							a.later(func() { a.reconnect(s) })
						}
					}
					x := ui.ButtonBase(c).Label("关闭标签 "+title).Tooltip("关闭标签").Size(18, 18).Radius(4).Center()
					if x.Hovered() {
						x.Background(t.SurfacePressed)
					}
					x.Children(func() { ui.Icon(c, appIcons["close"]).FontSize(12).TextColor(t.TextMuted) })
					if x.Clicked() {
						a.later(func() { a.requestClose(s.ID) })
					}
				})
			}
		})
		a.toolTabs(c, tabs, len(a.Tabs))
		a.editorTabs(c, tabs, len(a.Tabs)+len(a.monitor.tools))
		if tabs.List.Changed() {
			switch tools := len(a.Tabs) + len(a.monitor.tools); {
			case selected >= tools:
				a.showDocument(docs[selected-tools])
			case selected >= len(a.Tabs):
				a.showTool(a.monitor.tools[selected-len(a.Tabs)])
			default:
				a.activateTab(selected)
			}
		}
	})
}

// activeTabKey names the tab that shows, to scroll it into view when it changes.
func (a *App) activeTabKey() string {
	if doc := a.activeEditor(); doc != nil {
		return "doc:" + doc.Key
	}
	if tool := a.activeTool(); tool != nil {
		return "tool:" + tool.key()
	}
	return a.activeID()
}

// middleClicked reports a click of the middle button on tab, released on it.
func (a *App) middleClicked(tab ui.Element, key string) bool {
	if b, ok := tab.PressedButton(); ok && b == 2 {
		a.tabs.held = key
		return false
	}
	return a.tabs.middle == key && tab.Hovered()
}

// tabTitle is the title of s's tab: a duplicate of an earlier tab's automatic title reads 「名称 (2)」.
func (a *App) tabTitle(s *Session) string {
	if s.ManualTitle {
		return s.Title
	}
	n := 1
	for _, o := range a.Tabs {
		if o == s {
			break
		}
		if !o.ManualTitle && o.Title == s.Title {
			n++
		}
	}
	if n > 1 {
		return fmt.Sprintf("%s (%d)", s.Title, n)
	}
	return s.Title
}

// tabTooltip names what the tab is. An SSH tab shows its connection name, so the title the remote
// program set (often the same user@host for every tab) is shown here instead.
func (a *App) tabTooltip(s *Session) string {
	if s.Local {
		return "本地终端"
	}
	tip := s.Connection.User + "@" + s.Connection.Address()
	if title := a.P.TerminalTitle(s.ID); title != "" {
		tip += "\n" + title
	}
	return tip
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
			a.later(func() { a.requestReconnect(s) })
		}
		if m.Item("打开连接管理器").Chosen() {
			a.later(a.openManager)
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
			a.later(func() { a.requestClose(s.ID) })
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
		a.monitor.tool, a.editor.shown = "", ""
		a.tabs.term.Focus()
	}
}
func (a *App) closeTabID(id string) { a.closeTab(a.tabIndex(id)) }

// requestClose is the user closing session tabs: it asks first when one is connected, or when the
// host's last session goes with unsaved files. An agent's close, and what the user confirmed, go
// straight to closeTab.
func (a *App) requestClose(ids ...string) {
	ask := &closeAsk{ids: ids}
	for _, id := range ids {
		if i := a.tabIndex(id); i >= 0 && !a.Tabs[i].Local && !a.P.Exited(id) && !a.Prefs.NoTabConfirm {
			ask.live++
		}
	}
	ask.dirty = a.dirtyWith(ids)
	if ask.live == 0 && len(ask.dirty) == 0 {
		a.closeTabs(ids)
		return
	}
	a.tabs.ask = ask
}
func (a *App) closeTabs(ids []string) {
	for _, id := range ids {
		a.closeTabID(id)
	}
}
func (a *App) closeTab(i int) {
	if i < 0 || i >= len(a.Tabs) {
		return
	}
	id := a.Tabs[i].ID
	if host := a.Tabs[i].Connection.ID; host != "" && a.hosts.connecting[host] && a.P.Exited(id) {
		a.cancelConnect(host) // its reconnect would otherwise open a new tab
	}
	delete(a.tabs.started, id)
	a.cmdbar.forget(id)
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
	a.dropTools()
	a.dropDocuments()
}

// closeWhere closes the tabs for which close reports true, from the right.
func (a *App) closeWhere(close func(i int, s *Session) bool) {
	var ids []string
	for i := len(a.Tabs) - 1; i >= 0; i-- {
		if close(i, a.Tabs[i]) {
			ids = append(ids, a.Tabs[i].ID)
		}
	}
	a.requestClose(ids...)
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

// refreshTitles follows the titles programs set in local terminals, unless the user named the tab.
// An SSH tab keeps its connection name: ten tabs to similar hosts would otherwise all read user@host.
func (a *App) refreshTitles() {
	for _, s := range a.Tabs {
		if s.ManualTitle || !s.Local || !a.Prefs.TitleUpdates {
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
				row.Children(func() { ui.Text(c, a.tabTitle(a.Tabs[i])).SingleLine() })
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

// closeDialog asks before the tabs of a.tabs.ask close.
func (a *App) closeDialog(c *ui.Context) {
	k := a.tabs.ask
	if k == nil {
		return
	}
	title, msg, action := "关闭标签？", "这个连接还在使用。关闭后，正在运行的命令会中断。", "断开并关闭"
	if i := a.tabIndex(k.ids[0]); len(k.ids) == 1 && i >= 0 {
		title = "关闭 " + a.tabTitle(a.Tabs[i]) + "？"
	} else if len(k.ids) > 1 {
		title, msg = fmt.Sprintf("关闭 %d 个标签？", len(k.ids)), fmt.Sprintf("其中 %d 个连接还在使用。关闭后，正在运行的命令会中断。", k.live)
	}
	if len(k.dirty) > 0 { // unsaved files are asked about whatever 不再询问 says
		action = "不保存并关闭"
		var names []string
		for _, d := range k.dirty[:min(len(k.dirty), 6)] {
			names = append(names, d.Document.Path)
		}
		msg = "这些文件的修改还没有保存，关闭后会丢失：\n" + strings.Join(names, "\n")
		if len(k.dirty) > 6 {
			msg += fmt.Sprintf("\n……共 %d 个", len(k.dirty))
		}
		if k.live > 0 {
			msg += "\n\n连接也会断开，正在运行的命令会中断。"
		}
	}
	open := true
	dialog(c, &open, func() {
		ui.Column(c).Width(420).Gap(12).Children(func() {
			ui.Text(c, title).FontSize(17).Bold()
			ui.Text(c, msg).FillWidth()
			if len(k.dirty) == 0 {
				ui.Checkbox(c, &k.never, "不再询问")
			}
			ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
				// Enter confirms the focused button: with edits at stake that is 取消, never the loss.
				cancel := ui.Button(c, "取消")
				if len(k.dirty) > 0 {
					cancel.AutoFocus()
				}
				if cancel.Clicked() {
					open = false
				}
				confirm := ui.PrimaryButton(c, action)
				if len(k.dirty) == 0 {
					confirm.AutoFocus()
				}
				if confirm.Clicked() {
					open = false
					a.later(func() {
						if k.never && len(k.dirty) == 0 {
							p := a.Prefs
							p.NoTabConfirm = true
							if e := a.P.SavePreferences(p); e != nil {
								a.fail(e)
							} else {
								a.Prefs, a.settings.draft.NoTabConfirm = p, true
							}
						}
						for _, d := range k.dirty {
							a.editor.removeDocument(d.Key)
						}
						a.closeTabs(k.ids)
					})
				}
			})
		})
	})
	if !open {
		a.tabs.ask = nil
	}
}
