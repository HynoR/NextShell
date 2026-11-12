package appui

import (
	"fmt"
	"runtime"
	"sort"
	"strings"
	"time"

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
	// ShowManager opens (or raises) the connection manager window, which ManagerView builds.
	ShowSettings, ShowManager func()
	// PickFiles shows a native open/save panel and calls done on the UI
	// thread with the chosen paths (none when cancelled). Nil means no panels.
	PickFiles func(o FilePick, done func([]string))
	// RaiseWindow brings the main window forward: focus=false shows it without taking the keyboard (the
	// host also bounces the Dock icon), for an agent; focus=true, for session_focus and for a connection
	// started in the connection manager. Optional.
	RaiseWindow func(focus bool)
	// Clock is the time source for agent activity and rate limits; nil means time.Now.
	Clock func() time.Time
	// Cloud shows the cloud sync UI: the 同步 settings page, its palette action, the title bar's indicator and
	// the host menu's copy action. The host sets it for development builds only: no sync server exists yet.
	Cloud bool
	// Version is the packaged app's version for the About page; empty for a build run from source.
	// ProfilePath is the profile database the About page shows.
	Version, ProfilePath string
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
	wakeMain, wakeSettings  func()
	wakeEditor, wakeManager func()
}

// window is one of the app's windows. The remote editor's toasts show in it,
// and so do the settings' and the connection manager's, and everything else
// shows in the main window.
type window int

const (
	mainWindow window = iota
	editorWindow
	settingsWindow
	managerWindow
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
	a.layout.load(p.UIState("layout"))
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

// openManager opens (or raises) the connection manager window.
func (a *App) openManager() {
	if a.ShowManager != nil {
		a.ShowManager()
	}
	if a.wakeManager != nil {
		a.wakeManager()
	}
}

// raiseMain brings the main window forward with the keyboard, for a connection
// started in another window.
func (a *App) raiseMain() {
	if a.RaiseWindow != nil {
		a.RaiseWindow(true)
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
		for _, wake := range []func(){a.wakeSettings, a.wakeEditor, a.wakeManager} {
			if wake != nil {
				wake()
			}
		}
	}
	a.restoreTabs()
	a.refreshTitles()
	a.shortcuts(c)
	a.mainWindow(c)
	a.watchMonitor()
	a.dialogsView(c)
	a.runActions(c)
	a.saveLayout()
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
		if e.ShellHook != nil && e.ShellHook.Err != nil {
			a.log("Shell 集成未启用：" + e.ShellHook.Err.Error())
		}
		if e.Failed != "" {
			delete(a.hosts.connecting, e.Failed)
			a.hosts.failed[e.Failed] = true
		}
		if e.AuthFailed { // the retry dialog says why and offers the fix
			a.reload()
			a.log(e.Message)
			a.retryAuth(e.Failed, e.Message)
		} else if e.Message != "" {
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
		if tool := a.activeTool(); tool != nil {
			a.closeTool(*tool)
		} else {
			a.closeTab(a.Active)
		}
	}
	// The ⌘ letters a shell also uses take Ctrl only outside the terminal, as ⌘K does.
	key := func(k ui.Key) bool { return c.Shortcut(appMod, k) || (!termFocused && c.Shortcut(ui.Cmd, k)) }
	if key(ui.KeyB) {
		a.layout.leftHidden = !a.layout.leftHidden
	}
	if key(ui.KeyJ) {
		a.layout.benchHidden = !a.layout.benchHidden
	}
	if key(ui.KeyO) {
		a.openManager()
	}
	for _, v := range []struct {
		key   ui.Key
		bench int
	}{{ui.KeyF, benchFiles}, {ui.KeyM, benchSysinfo}, {ui.KeyS, benchSnippets}} {
		if c.Shortcut(ui.Cmd|ui.Shift, v.key) {
			a.showBench(v.bench)
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
func shortcutLabel(key string) string {
	if runtime.GOOS == "darwin" {
		return "⌘" + key
	}
	return "Ctrl+" + key
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
