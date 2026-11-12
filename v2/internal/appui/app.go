package appui

import (
	"fmt"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/model"
)

// App is the window shell: sessions, layout and the feature states. Each
// feature keeps its own state struct and lives in its own file.
type App struct {
	P     Platform
	Data  Snapshot
	Prefs model.Preferences

	Tabs   []*Session
	Active int

	// ConfirmExit asks before quitting; the host sets it from the window's OnClose.
	ConfirmExit, ExitConfirmed bool
	// ShowSettings opens the settings window. The host sets it; SettingsView builds it.
	ShowSettings func()
	// PickFiles shows a native open/save panel and calls done on the UI
	// thread with the chosen paths (none when cancelled). Nil means no panels.
	PickFiles func(o FilePick, done func([]string))
	// RaiseWindow brings the main window forward for the agent: focus=false shows it without taking the
	// keyboard (the host also bounces the Dock icon), focus=true is the hand-over of session_focus. Optional.
	RaiseWindow func(focus bool)
	// Clock is the time source for agent activity and rate limits; nil means time.Now.
	Clock func() time.Time
	// ShowEditor opens (or raises) the remote editor window, which EditorView
	// builds; HideEditor closes it. Without ShowEditor the editor is a dialog.
	ShowEditor, HideEditor func()

	layout   layoutState
	tabs     tabsState
	hosts    hostsState
	welcome  welcomeState
	palette  paletteState
	files    filesState
	monitor  monitorState
	snippets snippetsState
	editor   editorState
	settings settingsState
	dialogs  dialogState
	agent    agentUI

	actions []func()
	// toasts waits per window until that window's view builds and shows them;
	// view is the window being built, so a toast raised in a view belongs to it.
	toasts [windows][]ui.Toast
	view   window
	logs   []string
	sync   struct {
		label string
		at    time.Time
	}
	wakeMain, wakeSettings func()
	wakeEditor             func()
}

// window is one of the app's windows. The remote editor's toasts show in it,
// and so do the settings', and everything else shows in the main window.
type window int

const (
	mainWindow window = iota
	editorWindow
	settingsWindow
	windows
)

// inWindow runs fn as if w's view were building: what it toasts belongs to w.
func (a *App) inWindow(w window, fn func()) {
	before := a.view
	a.view = w
	fn()
	a.view = before
}

// inEditorWindow is inWindow for the editor window, when it is showing; the
// toasts of an editor action that outlives its window go to the main window.
func (a *App) inEditorWindow(fn func()) {
	if a.editor.windowed {
		a.inWindow(editorWindow, fn)
		return
	}
	fn()
}

type layoutState struct {
	sidebarWidth, panelWidth float32
	sidebarHidden, panelOpen bool
	panel                    int // panelFiles, panelMonitor, panelSnippets
	transfersOpen            bool
}

const (
	panelFiles = iota
	panelMonitor
	panelSnippets
)

// appMod is the modifier of app shortcuts that would otherwise take a
// Ctrl+letter from the shell: Command on macOS, Ctrl+Shift on Windows and Linux.
var appMod = func() ui.Modifiers {
	if runtime.GOOS == "darwin" {
		return ui.Cmd
	}
	return ui.Ctrl | ui.Shift
}()

func New(p Platform) *App {
	a := &App{P: p, Active: -1}
	a.layout = layoutState{sidebarWidth: 240, panelWidth: 360}
	a.Prefs = p.Preferences()
	a.settings.load(a.Prefs)
	a.tabs.started = map[string]time.Time{}
	a.hosts.init()
	a.hosts.recent = strings.Fields(p.UIState("recent"))
	a.files.init()
	a.monitor.init()
	a.snippets.init()
	a.reload()
	return a
}

// later runs fn after the view is built, for changes that would disturb the
// loop building the current rows.
func (a *App) later(fn func()) { a.actions = append(a.actions, fn) }

func (a *App) toast(msg string) {
	a.log(msg)
	a.addToast(ui.Toast{Title: msg, Timeout: 3 * time.Second})
}

// addToast queues t for the window being built, or the one the code runs for.
func (a *App) addToast(t ui.Toast) { a.toasts[a.view] = append(a.toasts[a.view], t) }
func (a *App) fail(e error) {
	if e == nil {
		return
	}
	title, _ := humanize(e.Error())
	a.failAs(title, e.Error())
}

// failAs shows title with the original message msg in its details.
func (a *App) failAs(title, msg string) {
	a.log(msg)
	if r := []rune(title); len(r) > 60 {
		title = string(r[:60]) + "…"
	}
	w := a.view
	a.addToast(ui.Toast{Title: title, Type: "error", Timeout: 6 * time.Second, Action: "详情", OnAction: func() { a.dialogs.detail, a.dialogs.detailIn = msg, w }})
}

// humanize turns the common network, SSH and file errors into Chinese; the
// original stays in the toast's details. ok reports a known error.
func humanize(msg string) (string, bool) {
	m := strings.ToLower(msg)
	for _, v := range []struct{ match, text string }{
		{"unable to authenticate", "认证失败：用户名、密码或密钥不正确"},
		{"handshake failed: eof", "服务器不支持此认证方式"},
		{"unexpected message type 51", "服务器不支持此认证方式"},
		{"exited without exit status or exit signal", "连接已断开"},
		{"connection refused", "连接被拒绝：服务器没有在这个端口提供 SSH 服务"},
		{"no route to host", "无法访问主机"},
		{"network is unreachable", "网络不可达"},
		{"host is down", "无法访问主机"},
		{"no such host", "找不到主机：域名无法解析"},
		{"i/o timeout", "连接超时"},
		{"deadline exceeded", "操作超时"},
		{"timed out", "连接超时"},
		{"connection reset", "连接被服务器重置"},
		{"permission denied", "权限不足"},
		{"no such file", "文件或目录不存在"},
		{"file does not exist", "文件或目录不存在"},
		{"file exists", "目标已存在"},
		{"knownhosts: key mismatch", "known_hosts 里的主机密钥与服务器不一致"},
	} {
		if strings.Contains(m, v.match) {
			return v.text, true
		}
	}
	return msg, false
}

// report shows e, or ok when e is nil and ok is not empty.
func (a *App) report(e error, ok string) {
	if e != nil {
		a.fail(e)
	} else if ok != "" {
		a.toast(ok)
	}
}
func (a *App) log(msg string) {
	a.logs = append(a.logs, time.Now().Format("15:04:05")+"  "+msg)
	if len(a.logs) > 100 {
		a.logs = a.logs[len(a.logs)-100:]
	}
}
func (a *App) flushToasts(c *ui.Context, w window) {
	for _, t := range a.toasts[w] {
		c.AddToast(t)
	}
	a.toasts[w] = nil
}
func (a *App) reload() {
	s, e := a.P.Snapshot()
	if e != nil {
		a.fail(e)
		return
	}
	// Show records in a stable order: the store's order follows random IDs.
	sort.SliceStable(s.Connections, func(i, j int) bool {
		return strings.ToLower(s.Connections[i].Name) < strings.ToLower(s.Connections[j].Name)
	})
	sort.SliceStable(s.Folders, func(i, j int) bool {
		a, b := s.Folders[i], s.Folders[j]
		return a.SortIndex < b.SortIndex || a.SortIndex == b.SortIndex && a.Name < b.Name
	})
	sort.SliceStable(s.Commands, func(i, j int) bool {
		a, b := s.Commands[i], s.Commands[j]
		return a.Group < b.Group || a.Group == b.Group && a.Name < b.Name
	})
	a.Data = s
}
func (a *App) current() *Session {
	if a.Active < 0 || a.Active >= len(a.Tabs) {
		return nil
	}
	return a.Tabs[a.Active]
}
func (a *App) activeID() string {
	if s := a.current(); s != nil {
		return s.ID
	}
	return ""
}

// remote returns the active session when it is an SSH one.
func (a *App) remote() *Session {
	if s := a.current(); s != nil && !s.Local {
		return s
	}
	return nil
}
func (a *App) theme(c *ui.Context) {
	c.SetTheme(designTheme(c.Theme(), a.Prefs.Appearance, c.Preferences().HighContrast))
}
func (a *App) openSettings(section string) {
	if section != "" {
		a.settings.section = section
	}
	if a.ShowSettings != nil {
		a.ShowSettings()
	}
	if a.wakeSettings != nil {
		a.wakeSettings()
	}
}

// View builds the main window.
func (a *App) View(c *ui.Context) {
	a.theme(c)
	a.wakeMain = c.Services().Invalidate
	a.P.SetWake(c.Services().Invalidate)
	a.P.SetClipboard(c.Services().WriteClipboard)
	a.P.SetURLopener(c.Services().OpenURL)
	if a.poll() {
		for _, wake := range []func(){a.wakeSettings, a.wakeEditor} {
			if wake != nil {
				wake()
			}
		}
	}
	a.restoreTabs()
	a.injectShellHooks(c)
	a.refreshTitles()
	a.shortcuts(c)
	a.mainWindow(c)
	a.monitor.watch(a, a.layout.panelOpen && a.layout.panel == panelMonitor)
	a.dialogsView(c)
	a.runActions(c)
	if !a.editor.windowed { // its window closed with toasts pending
		a.toasts[mainWindow] = append(a.toasts[mainWindow], a.toasts[editorWindow]...)
		a.toasts[editorWindow] = nil
	}
	a.flushToasts(c, mainWindow)
}
func (a *App) runActions(c *ui.Context) {
	actions := a.actions
	a.actions = nil
	for _, fn := range actions {
		fn()
		c.Invalidate()
	}
}

// poll applies the platform's events and reports whether there were any.
func (a *App) poll() bool {
	a.dialogs.prunePrompts()
	events := a.P.Poll()
	if len(events) > 0 {
		a.sync.at = time.Time{} // sync results arrive as events
	}
	for _, e := range events {
		if e.Link != "" {
			if a.Prefs.HyperlinkConfirm {
				a.dialogs.links = append(a.dialogs.links, e.Link)
			} else {
				a.report(a.P.OpenURL(e.Link), "")
			}
		}
		a.settings.handle(a, e)
		a.files.handle(a, e)
		a.monitor.handle(a, e)
		if e.Editor != nil {
			a.receiveDocument(e.Editor)
		}
		if e.Agent != nil {
			a.agentActed(e.Agent)
		}
		if e.FocusID != "" {
			if e.Handover {
				a.handover(e.FocusID)
			} else {
				a.selectTabID(e.FocusID)
			}
		}
		if e.CloseID != "" {
			a.closeTabID(e.CloseID)
		}
		if e.Session != nil {
			a.addSession(e.Session)
		}
		if e.ShellHook != nil {
			a.shellHookReady(e.ShellHook)
		}
		if e.Failed != "" {
			delete(a.hosts.connecting, e.Failed)
			a.hosts.failed[e.Failed] = true
		}
		if e.Message != "" {
			a.reload()
			show := func() {
				if e.Failed != "" && a.viaProxy(e.Failed) && strings.Contains(strings.ToLower(e.Message), "handshake failed: eof") {
					a.failAs("代理连接被关闭", e.Message)
				} else if _, known := humanize(e.Message); e.Failed != "" || known {
					a.fail(fmt.Errorf("%s", e.Message))
				} else {
					a.toast(e.Message)
				}
			}
			if e.ForEditor {
				a.inEditorWindow(show)
			} else {
				show()
			}
		}
		if e.Prompt != nil {
			a.dialogs.prompts = append(a.dialogs.prompts, e.Prompt)
		}
	}
	return len(events) > 0
}

func (a *App) addSession(s *Session) {
	light, dark := TerminalThemes(a.Prefs)
	s.Terminal.SetTheme(light, dark)
	if s.Connection.ID != "" {
		delete(a.hosts.connecting, s.Connection.ID)
		delete(a.hosts.failed, s.Connection.ID)
	}
	a.tabs.started[s.ID] = time.Now()
	if !s.Local && s.Replaces == "" && a.Prefs.ShellIntegration == "auto" {
		if a.tabs.hooks == nil {
			a.tabs.hooks = map[string]*shellHook{}
		}
		a.tabs.hooks[s.ID] = &shellHook{until: time.Now().Add(10 * time.Second)}
		a.P.InstallShellHook(s.ID)
	}
	if s.ByAgent && s.Replaces == "" {
		a.addAgentTab(s)
		return
	}
	for i, old := range a.Tabs {
		if s.Replaces == old.ID {
			a.P.CloseSession(old.ID)
			s.Title = old.Title
			s.ManualTitle = old.ManualTitle
			delete(a.tabs.started, old.ID)
			a.Tabs[i] = s
			a.Active = i
			a.tabs.mru.Sync(a.tabIDs(), s.ID)
			return
		}
	}
	a.Tabs = append(a.Tabs, s)
	a.Active = len(a.Tabs) - 1
	a.tabs.mru.Sync(a.tabIDs(), s.ID)
}

func (a *App) shortcuts(c *ui.Context) {
	a.mruShortcuts(c)
	termFocused := a.tabs.term.FocusWithin(c)
	if c.Shortcut(appMod, ui.KeyK) || (!termFocused && c.Shortcut(ui.Cmd, ui.KeyK)) {
		a.palette.show()
	}
	if c.Shortcut(ui.Cmd, ui.KeyComma) {
		a.openSettings("")
	}
	if c.Shortcut(appMod, ui.KeyT) {
		a.P.Local()
	}
	if c.Shortcut(appMod, ui.KeyW) && a.current() != nil {
		a.closeTab(a.Active)
	}
	if c.Shortcut(appMod, ui.KeyB) {
		a.layout.sidebarHidden = !a.layout.sidebarHidden
	}
	if c.Shortcut(appMod, ui.KeyJ) {
		a.layout.panelOpen = !a.layout.panelOpen
	}
	for i, k := range []ui.Key{ui.KeyF, ui.KeyM, ui.KeyS} {
		if c.Shortcut(ui.Cmd|ui.Shift, k) {
			a.showPanel(i)
		}
	}
	if c.Shortcut(ui.Cmd|ui.Shift, ui.KeyD) {
		if s := a.current(); s != nil {
			a.P.CloneSession(s)
		}
	}
	if len(a.Tabs) == 0 {
		return
	}
	for n, k := range []ui.Key{ui.Key1, ui.Key2, ui.Key3, ui.Key4, ui.Key5, ui.Key6, ui.Key7, ui.Key8, ui.Key9} {
		if c.Shortcut(ui.Cmd, k) {
			if n == 8 {
				n = len(a.Tabs) - 1
			}
			a.activateTab(n)
		}
	}
	if c.Shortcut(ui.Ctrl, ui.KeyPageUp) || c.Shortcut(ui.Cmd|ui.Shift, ui.KeyBracketLeft) {
		a.activateTab((a.Active - 1 + len(a.Tabs)) % len(a.Tabs))
	}
	if c.Shortcut(ui.Ctrl, ui.KeyPageDown) || c.Shortcut(ui.Cmd|ui.Shift, ui.KeyBracketRight) {
		a.activateTab((a.Active + 1) % len(a.Tabs))
	}
}
func (a *App) showPanel(i int) {
	a.layout.panel = i
	a.layout.panelOpen = true
}

func (a *App) mainWindow(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().Background(t.Background).Children(func() {
		a.titlebar(c)
		ui.Row(c).FillWidth().Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
			if a.layout.sidebarHidden {
				a.workArea(c)
				return
			}
			ui.Split(c, &a.layout.sidebarWidth, func() { a.sidebar(c) }, func() { a.workArea(c) }).SplitReset(240).Grow(1).FillHeight()
		})
		a.statusBar(c)
	})
}

// workArea is the terminal (or the welcome page) and the context panel.
func (a *App) workArea(c *ui.Context) {
	if a.current() == nil { // no tabs, or only background tabs an agent opened
		ui.Column(c).Fill().Children(func() { a.welcomeView(c) })
		return
	}
	if !a.layout.panelOpen {
		ui.Column(c).Fill().Children(func() { a.terminalArea(c) })
		return
	}
	w, _ := c.Size()
	avail := w
	if !a.layout.sidebarHidden {
		avail -= a.layout.sidebarWidth
	}
	center := max(avail-a.layout.panelWidth, 200)
	split := ui.Split(c, &center, func() { a.terminalArea(c) }, func() { a.panelView(c) }).Grow(1).FillHeight()
	if split.Changed() {
		a.layout.panelWidth = max(avail-center, 260)
	}
}

func (a *App) terminalArea(c *ui.Context) {
	s := a.current()
	ui.Column(c).Fill().MinWidth(0).Children(func() {
		if a.tabs.mru.Active() {
			a.mruOverlay(c)
		}
		term := terminal.View(c.Key(s.ID+"terminal"), s.Terminal).Grow(1).FillWidth().AutoFocus().Bind(&a.tabs.term)
		if a.P.Exited(s.ID) {
			if term.Shortcut(0, ui.KeyEnter) {
				a.P.Reconnect(s)
			}
			t := c.Theme()
			ui.Row(c).FillWidth().Height(32).Padding(0, 12).Gap(8).AlignItems(ui.Center).Background(t.Surface).BorderColor(t.Border).BorderWidth(1, 0, 0, 0).Children(func() {
				msg := "连接已断开 · 按回车重新连接"
				if s.Local {
					msg = "终端已退出 · 按回车重新打开"
				}
				ui.Text(c, msg).Grow(1).TextColor(t.TextMuted)
				if ui.Button(c, "重新连接").Clicked() {
					a.P.Reconnect(s)
				}
			})
		}
	})
}

func (a *App) panelView(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().MinWidth(0).Background(t.Surface).Children(func() {
		ui.Row(c).FillWidth().Height(40).Padding(0, 8).Gap(6).AlignItems(ui.Center).BorderColor(t.Border).BorderWidth(0, 0, 1, 0).Children(func() {
			ui.Segmented(c, &a.layout.panel, "文件", "监控", "片段").FillEqually().Label("面板").Grow(1)
			if iconButton(c, "关闭面板", "close").Clicked() {
				a.layout.panelOpen = false
			}
		})
		ui.Column(c).Grow(1).FillWidth().MinHeight(0).Children(func() {
			switch a.layout.panel {
			case panelFiles:
				a.filesPanel(c)
			case panelMonitor:
				a.monitorPanel(c)
			default:
				a.snippetsPanel(c)
			}
		})
	})
}

func (a *App) titlebar(c *ui.Context) {
	t := c.Theme()
	bar := c.TitleBar()
	ui.Row(c).FillWidth().Height(max(bar.Height, 40)).Padding(0, bar.Right+8, 0, bar.Left+8).Gap(4).AlignItems(ui.Center).Background(t.Surface).BorderColor(t.Border).BorderWidth(0, 0, 1, 0).DragWindow().Children(func() {
		a.tabStrip(c)
		plus := iconButton(c, "新建本地终端", "plus")
		if plus.Clicked() {
			a.P.Local()
		}
		plus.PopoverMenu(func(m *ui.Menu) {
			if m.Item("本地终端").Chosen() {
				a.later(a.P.Local)
			}
			m.Separator()
			for _, cc := range a.hosts.recentHosts(a, 10) {
				if m.Item(cc.Name).ID(cc.ID).Subtitle(cc.User + "@" + cc.Host).Chosen() {
					a.later(func() { a.connect(cc) })
				}
			}
		})
		ui.Spacer(c).MinWidth(16)
		search := ui.ButtonBase(c).Label("搜索或连接").Width(220).Height(26).Padding(0, 10).Gap(6).Radius(t.Radius).AlignItems(ui.Center).Background(t.Background).Border(1, t.Border)
		search.Children(func() {
			ui.Icon(c, appIcons["search"]).FontSize(14).TextColor(t.TextMuted)
			ui.Text(c, "搜索或连接").FontSize(12).TextColor(t.TextMuted).Grow(1).SingleLine()
			ui.Text(c, shortcutLabel("K")).FontSize(11).TextColor(t.TextMuted)
		})
		if search.Clicked() {
			a.palette.show()
		}
		a.paletteView(c, search)
		if iconButton(c, "设置", "settings").Clicked() {
			a.openSettings("")
		}
	})
}

func shortcutLabel(key string) string {
	if runtime.GOOS == "darwin" {
		return "⌘" + key
	}
	return "Ctrl+" + key
}

func (a *App) statusBar(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).FillWidth().Height(24).Padding(0, 12).Gap(16).AlignItems(ui.Center).Background(t.Surface).BorderColor(t.Border).BorderWidth(1, 0, 0, 0).Children(func() {
		ui.Row(c).Grow(1).MinWidth(0).Gap(6).AlignItems(ui.Center).Children(func() {
			s := a.current()
			if s == nil {
				ui.Text(c, "未打开会话").FontSize(12).TextColor(t.TextMuted)
				return
			}
			label, color := a.sessionState(c, s)
			dot(c, color)
			ui.Text(c, label).FontSize(12).TextColor(t.TextMuted).SingleLine()
		})
		a.transfersStatus(c)
		if s := a.current(); s != nil && !s.Local {
			ui.Text(c, strings.ToUpper(orDefault(s.Connection.Encoding, "utf-8"))).FontSize(12).TextColor(t.TextMuted)
		}
		if auto := a.P.AgentAutoApproval(); auto.On {
			c.After(30 * time.Second)
			b := ui.ButtonBase(c).Label("自动批准中").Padding(0, 8).Radius(4).Background(t.Warning.Alpha(.18)).Children(func() {
				ui.Text(c, "自动批准中 · "+autoApproveLabel(auto)).FontSize(12).Bold().TextColor(t.Warning)
			})
			if b.Clicked() {
				a.openSettings("Agent")
			}
		}
		if pending := a.P.AgentOverview().Pending; len(pending) > 0 {
			b := ui.ButtonBase(c).Label("Agent 待批准").Tooltip(pendingTooltip(pending)).Padding(0, 6).Children(func() {
				ui.Text(c, fmt.Sprintf("Agent 待批准 %d", len(pending))).FontSize(12).TextColor(t.Warning)
			})
			if b.Clicked() {
				a.openSettings("Agent")
			}
		} else if a.agentActive() {
			c.After(a.agent.lastAt.Add(agentActiveFor).Sub(a.now()) + time.Second)
			b := ui.ButtonBase(c).Label("Agent 活动中").Padding(0, 6).Tooltip("最近有 Agent 在操作 NextShell，点击查看").Children(func() {
				ui.Text(c, "Agent 活动中").FontSize(12).TextColor(t.TextMuted)
			})
			if b.Clicked() {
				a.openSettings("Agent")
			}
		}
		if label := a.syncLabel(); label != "" {
			b := ui.ButtonBase(c).Label("同步状态").Padding(0, 6).Children(func() {
				ui.Text(c, label).FontSize(12).TextColor(t.TextMuted)
			})
			if b.Clicked() {
				a.openSettings("同步")
			}
		}
	})
}

// sessionState describes a session for the status bar.
func (a *App) sessionState(c *ui.Context, s *Session) (string, ui.Color) {
	t := c.Theme()
	who := "本地终端"
	if !s.Local {
		who = s.Connection.User + "@" + s.Connection.Host
	}
	if a.P.Exited(s.ID) {
		if s.Local {
			return who + " · 已退出", t.TextMuted
		}
		return who + " · 已断开", t.TextMuted
	}
	if s.Local {
		return who, t.Success
	}
	c.After(time.Minute)
	return who + " · 已连接 " + since(a.tabs.started[s.ID]), t.Success
}
func since(at time.Time) string {
	d := time.Since(at)
	switch {
	case at.IsZero() || d < time.Minute:
		return "不到 1 分钟"
	case d < time.Hour:
		return fmt.Sprintf("%d 分钟", int(d.Minutes()))
	default:
		return fmt.Sprintf("%d 小时 %d 分钟", int(d.Hours()), int(d.Minutes())%60)
	}
}

// syncLabel reads the profile at most every two seconds: the status bar builds every frame.
func (a *App) syncLabel() string {
	if time.Since(a.sync.at) > 2*time.Second {
		a.sync.label, a.sync.at = a.syncState(), time.Now()
	}
	return a.sync.label
}
func (a *App) syncState() string {
	o := a.P.CloudOverview()
	if len(o.Workspaces) == 0 {
		return ""
	}
	for _, s := range o.Statuses {
		switch s.State {
		case "conflict":
			return "☁ 同步冲突"
		case "error":
			return "☁ 同步出错"
		}
	}
	for _, s := range o.Statuses {
		if s.State != "disconnected" {
			return "☁ 已同步"
		}
	}
	return "☁ 未登录"
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// Shared small widgets.

func dot(c *ui.Context, color ui.Color) ui.Element {
	return ui.Box(c).Size(8, 8).Radius(4).Background(color)
}
func iconButton(c *ui.Context, label, icon string) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c).Label(label).Tooltip(label).Size(28, 28).Radius(t.Radius).Center().TextColor(t.TextMuted)
	if b.Hovered() {
		b.Background(t.SurfaceHover).TextColor(t.Text)
	}
	b.Children(func() { ui.Icon(c, appIcons[icon]).FontSize(16) })
	return b
}
func ghost(c *ui.Context, label, icon string) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c).Label(label).Padding(4, 8).Radius(t.Radius).Gap(6).AlignItems(ui.Center)
	if b.Hovered() {
		b.Background(t.SurfaceHover)
	}
	b.Children(func() {
		if s := appIcons[icon]; s != nil {
			ui.Icon(c, s).FontSize(16).TextColor(t.TextMuted)
		}
		ui.Text(c, label).SingleLine()
	})
	return b
}
func muted(c *ui.Context, s string) ui.Element {
	return ui.Text(c, s).FontSize(12).TextColor(c.Theme().TextMuted)
}
func field(c *ui.Context, label string, value *string) ui.Element {
	return ui.Field(c, label, func() { ui.TextInput(c, value).Label(label).Grow(1) }).FillWidth()
}

// localPath shows a local path chosen with a native panel; nothing is typed.
// Optional paths can be cleared back to the default.
func (a *App) localPath(c *ui.Context, label, empty string, value *string, o FilePick) ui.Element {
	t := c.Theme()
	return ui.Field(c, label, func() {
		ui.Row(c).Grow(1).MinWidth(0).Gap(6).AlignItems(ui.Center).Children(func() {
			shown, color := *value, t.Text
			if shown == "" {
				shown, color = empty, t.TextMuted
			}
			ui.Text(c, shown).Label(label + " " + *value).SingleLine().Grow(1).MinWidth(0).TextColor(color).Tooltip(*value)
			if *value != "" && empty != "" && ghost(c, "清除", "").Label("清除"+label).Clicked() {
				*value = ""
			}
			if ui.Button(c, "选择…").Label("选择" + label).Clicked() {
				if o.Title == "" {
					o.Title = label
				}
				if *value != "" {
					o.DefaultPath = *value
				}
				a.pick(o, func(p []string) { *value = p[0] })
			}
		})
	}).FillWidth()
}
func password(c *ui.Context, label string, value *string) ui.Element {
	return ui.Field(c, label, func() { ui.TextInput(c, value).Label(label).Password().Grow(1) }).FillWidth()
}
func choice(c *ui.Context, label string, value *string, options []string) ui.Element {
	return ui.Field(c, label, func() { ui.Select(c, value, options).Label(label).Grow(1) }).FillWidth()
}
func toggle(c *ui.Context, label string, on *bool) bool {
	changed := false
	ui.Field(c, label, func() { changed = ui.Switch(c, on).Label(label).Changed() }).FillWidth()
	return changed
}

// pick shows a Select of names and keeps *id in step; none is the label of the empty choice.
func pick(c *ui.Context, label, none string, id *string, ids, names []string) {
	chosen := none
	opts := []string{none}
	for i, v := range ids {
		n := names[i]
		if contains(opts, n) {
			n += " [" + v[:min(6, len(v))] + "]"
		}
		opts = append(opts, n)
		if *id == v {
			chosen = n
		}
	}
	choice(c, label, &chosen, opts)
	*id = ""
	for i, n := range opts[1:] {
		if n == chosen {
			*id = ids[i]
		}
	}
}
func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
func (a *App) folderPick(c *ui.Context, label string, id *string) {
	var ids, names []string
	for _, f := range a.Data.Folders {
		ids = append(ids, f.ID)
		names = append(names, a.folderPath(f.ID))
	}
	pick(c, label, "（根目录）", id, ids, names)
}
func (a *App) folderPath(id string) string {
	var parts []string
	for depth := 0; id != "" && depth < 30; depth++ {
		found := false
		for _, f := range a.Data.Folders {
			if f.ID == id {
				parts = append([]string{f.Name}, parts...)
				id = f.ParentID
				found = true
				break
			}
		}
		if !found {
			break
		}
	}
	return strings.Join(parts, " / ")
}

// FilePick describes a native file panel.
type FilePick struct {
	Title                             string
	DefaultPath                       string // "~/" means the home directory
	Save, Directory, Multiple, Hidden bool
}
