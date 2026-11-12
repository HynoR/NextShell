package appui_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/model"
)

func dblclick(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	click(t, tt, label)
	click(t, tt, label)
}

// appMod mirrors the app's modifier for shortcuts that must not take a Ctrl+letter.
func appMod() ui.Modifiers {
	if ui.Cmd == ui.Super {
		return ui.Cmd
	}
	return ui.Ctrl | ui.Shift
}

func TestWelcomeQuickConnectAndLayoutShortcuts(t *testing.T) {
	p := newPlatform(t)
	saved := saveHost(t, p, model.Connection{Name: "saved-box", Host: "10.9.9.9", User: "ops"})
	a, tt := start(p)
	has(t, tt, "NextShell", "最近", "saved-box", "+ 新建主机", "导入…", "打开本地终端", "未选择服务器")
	click(t, tt, "快速连接")
	tt.Type("tester@localhost:2222")
	tt.Key(0, ui.KeyEnter)
	if len(p.connects) != 1 || p.connects[0].Port != 2222 || p.connects[0].User != "tester" || p.connects[0].Secret != "" {
		t.Fatal("quick connect", p.connects)
	}
	click(t, tt, "快速连接")
	tt.Type("SAVED-BOX")
	tt.Key(0, ui.KeyEnter)
	if len(p.connects) != 2 || p.connects[1].ID != saved.ID {
		t.Fatal("name did not connect the saved host", p.connects)
	}
	has(t, tt, "连接中", "正在连接 saved-box…") // the first connect shows its tab at once
	p.events = append(p.events, appui.Event{Message: "连接被拒绝：服务器没有在这个端口提供 SSH 服务", Failed: saved.ID})
	tt.Frame()
	click(t, tt, "打开本地终端")
	if p.local != 1 {
		t.Fatal("local terminal")
	}
	session(t, p, tt, "s1", "saved-box", saved)
	has(t, tt, "已连接", "ops@10.9.9.9:22", "UTF-8 · 刚刚连接")
	if tt.HasText("最近") {
		t.Fatal("welcome shown with a tab")
	}
	// The left column samples the server while it shows.
	if !p.watching["s1"] || p.detailed["s1"] {
		t.Fatal("left column did not sample, or sampled processes", p.watching, p.detailed)
	}
	tt.Key(appMod(), ui.KeyB)
	if tt.HasText("系统监控") || !tt.HasText("展开左栏") || p.watching["s1"] {
		t.Fatal("left column not folded, or still sampling")
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyM)
	has(t, tt, "系统信息")
	if tt.HasText("进程") || !p.watching["s1"] || p.detailed["s1"] {
		t.Fatal("系统信息 samples lightly and keeps the process and network views out")
	}
	tt.Key(appMod(), ui.KeyJ)
	if p.watching["s1"] || p.detailed["s1"] {
		t.Fatal("workbench did not fold or sampling went on", p.watching, p.detailed)
	}
	tt.Key(appMod(), ui.KeyO)
	if p.managerShown != 1 {
		t.Fatal("manager shortcut")
	}
	// The layout comes back on the next start.
	_, bt := start(p)
	bt.Frame()
	session(t, p, bt, "s2", "saved-box", saved)
	if !bt.HasText("展开左栏") || !bt.HasText("展开工作区") {
		t.Fatal("layout not restored", bt.Texts())
	}
	tt.Key(appMod(), ui.KeyB)
	tt.Key(appMod(), ui.KeyJ)
	tt.Key(ui.Cmd, ui.KeyComma)
	if p.settingsShown != 1 {
		t.Fatal("settings shortcut")
	}
	tt.Key(appMod(), ui.KeyT)
	if p.local != 2 {
		t.Fatal("new local shortcut")
	}
	tt.Key(appMod(), ui.KeyW)
	if len(a.Tabs) != 0 || len(p.closed) != 1 {
		t.Fatal("close shortcut")
	}
}

func TestNoImplementationTermsInVisibleText(t *testing.T) {
	p := newPlatform(t)
	demo(t, p)
	a, tt := start(p)
	st := ui.NewTester(a.SettingsView, 880, 1400)
	check := func(tt *ui.Tester, where string) {
		t.Helper()
		for _, s := range tt.Texts() {
			for _, bad := range []string{"v1", "OSC", "keepalive", "Keepalive", "AAD", "Platform", "TRUST"} {
				if strings.Contains(s, bad) {
					t.Errorf("%s shows %q: %q", where, bad, s)
				}
			}
		}
	}
	check(tt, "welcome")
	for _, section := range []string{"窗口行为", "文件传输", "远端编辑", "终端", "网络工具", "回收站", "Agent", "关于"} {
		click(t, st, section)
		check(st, section)
	}
	mt := manager(a)
	for _, page := range []string{"连接", "密钥", "代理", "凭据"} {
		click(t, mt, page)
		check(mt, page)
	}
}

// manager opens the connection manager window of a.
func manager(a *appui.App) *ui.Tester { return ui.NewTester(a.ManagerView, 960, 700) }

func TestHostEditorCreateEditValidateAndTrash(t *testing.T) {
	a, p, main := fixture(t)
	session(t, p, main, "local", "本地", model.Connection{})
	if main.HasText("主机列表") {
		t.Fatal("the main window lists hosts")
	}
	raised := 0
	a.RaiseWindow = func(focus bool) {
		if focus {
			raised++
		}
	}
	tt := manager(a)
	click(t, tt, "+ 新建连接")
	has(t, tt, "新建主机", "使用凭据", "认证方式", "安全", "网络", "终端")
	tt.Type("web-a")
	click(t, tt, "主机")
	tt.Type("10.0.0.7")
	click(t, tt, "用户名")
	tt.Type("root")
	clickLast(t, tt, "密码")
	tt.Type("pw")
	click(t, tt, "保存")
	snap, _ := p.Snapshot()
	if len(snap.Connections) != 1 || snap.Connections[0].Name != "web-a" || snap.Connections[0].Port != 22 {
		t.Fatal(snap.Connections)
	}
	if stored, _ := p.Platform.Snapshot(); stored.Connections[0].Secret != "" {
		t.Fatal("secret leaked into snapshot")
	}
	has(t, tt, "web-a", "已保存主机：web-a")

	if e := tt.RightClick("web-a"); e != nil {
		t.Fatal(e)
	}
	if e := tt.ChooseMenuItem("编辑…"); e != nil {
		t.Fatal(e)
	}
	has(t, tt, "编辑主机")
	click(t, tt, "端口")
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("70000")
	click(t, tt, "保存")
	has(t, tt, "端口须为 1–65535")
	click(t, tt, "端口")
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("2200")
	click(t, tt, "保存并连接")
	if len(p.connects) != 1 || p.connects[0].Port != 2200 || raised != 1 {
		t.Fatal("save and connect opens in the main window", p.connects, raised)
	}

	if e := tt.RightClick("web-a"); e != nil {
		t.Fatal(e)
	}
	if e := tt.ChooseMenuItem("移入回收站"); e != nil {
		t.Fatal(e)
	}
	snap, _ = p.Snapshot()
	if !snap.Connections[0].Deleted {
		t.Fatal("not trashed")
	}
	click(t, tt, "撤销")
	snap, _ = p.Snapshot()
	if snap.Connections[0].Deleted {
		t.Fatal("undo did not restore")
	}

	// The recycle bin restores and purges.
	if e := tt.RightClick("web-a"); e != nil {
		t.Fatal(e)
	}
	tt.ChooseMenuItem("移入回收站")
	st := ui.NewTester(a.SettingsView, 880, 640)
	click(t, st, "回收站")
	click(t, st, "恢复")
	snap, _ = p.Snapshot()
	if snap.Connections[0].Deleted {
		t.Fatal("restore")
	}
	tt.Frame()
	if e := tt.RightClick("web-a"); e != nil {
		t.Fatal(e)
	}
	tt.ChooseMenuItem("移入回收站")
	st.Frame()
	click(t, st, "永久删除")
	click(t, st, "永久删除") // the alert's button names the action
	snap, _ = p.Snapshot()
	if len(snap.Connections) != 0 {
		t.Fatal("purge", snap.Connections)
	}
}

func TestFoldersFavoritesFilterAndBatchAuth(t *testing.T) {
	p := newPlatform(t)
	prod := model.Folder{ID: p.NewID(), Name: "生产"}
	child := model.Folder{ID: p.NewID(), Name: "子目录", ParentID: prod.ID}
	p.SaveFolder(prod)
	p.SaveFolder(child)
	if e := p.SaveProxy(model.Proxy{ID: "px", Name: "跳板", Kind: "socks5", Host: "127.0.0.1", Port: 1080}, nil); e != nil {
		t.Fatal(e)
	}
	saveHost(t, p, model.Connection{Name: "alpha", Host: "a.example", User: "u", FolderID: prod.ID, ProxyID: "px"})
	saveHost(t, p, model.Connection{Name: "beta", Host: "b.example", User: "u", FolderID: child.ID})
	saveHost(t, p, model.Connection{Name: "gamma", Host: "g.example", User: "u"})
	a, main := start(p)
	tt := manager(a)
	has(t, tt, "生产  2", "子目录  1", "全部主机  3", "alpha", "beta", "gamma")
	click(t, tt, "生产  2")
	if !tt.HasText("alpha") || !tt.HasText("beta") || tt.HasText("gamma") {
		t.Fatal("folder shows its subtree", tt.Texts())
	}
	click(t, tt, "全部主机  3")
	click(t, tt, "搜索主机")
	tt.Type("b.exam")
	if tt.HasText("alpha") || !tt.HasText("beta") || tt.HasText("gamma") {
		t.Fatal("filter", tt.Texts())
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Key(0, ui.KeyBackspace)
	if e := tt.RightClick("gamma"); e != nil {
		t.Fatal(e)
	}
	if e := tt.ChooseMenuItem("收藏"); e != nil {
		t.Fatal(e)
	}
	has(t, tt, "★")
	if e := tt.RightClick("生产  2"); e != nil {
		t.Fatal(e)
	}
	if e := tt.ChooseMenuItem("批量绑定认证…"); e != nil {
		t.Fatal(e)
	}
	has(t, tt, "将应用到「生产」及子目录的 2 个主机", "alpha")
	click(t, tt, "SSH Agent")
	has(t, tt, "密码 → SSH Agent")
	click(t, tt, "应用到 2 个主机")
	snap, _ := p.Snapshot()
	for _, c := range snap.Connections {
		if (c.Name == "gamma") == (c.Auth == "agent") {
			t.Fatal("batch scope", c.Name, c.Auth)
		}
		if c.Name == "alpha" && c.ProxyID != "px" {
			t.Fatal("batch kept proxy", c.ProxyID)
		}
	}
	dblclick(t, tt, "beta")
	if len(p.connects) != 1 || p.connects[0].Name != "beta" {
		t.Fatal("double click connect", p.connects)
	}
	// A failed connect shows in the main window; Failed carries the host's ID.
	p.events = append(p.events, appui.Event{Failed: p.connects[0].ID, Message: "connection refused"})
	main.Frame()
	has(t, main, "连接被拒绝：服务器没有在这个端口提供 SSH 服务")
	click(t, main, "详情")
	has(t, main, "connection refused")
	click(t, main, "关闭")
	tt.Frame()

	if e := tt.RightClick("子目录  1"); e != nil {
		t.Fatal(e)
	}
	tt.ChooseMenuItem("重命名 / 移动…")
	has(t, tt, "编辑目录")
	click(t, tt, "目录名称")
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("改名")
	click(t, tt, "保存目录")
	has(t, tt, "改名  1")
}

func TestHandshakeEOFThroughProxyBlamesTheProxy(t *testing.T) {
	p := newPlatform(t)
	if e := p.SaveProxy(model.Proxy{ID: "px", Name: "跳板", Kind: "socks5", Host: "127.0.0.1", Port: 1080}, nil); e != nil {
		t.Fatal(e)
	}
	direct := saveHost(t, p, model.Connection{Name: "direct", Host: "d.example", Port: 22, User: "u", Auth: "agent"})
	proxied := saveHost(t, p, model.Connection{Name: "proxied", Host: "p.example", Port: 22, User: "u", Auth: "agent", ProxyID: "px"})
	_, tt := start(p)
	p.events = append(p.events, appui.Event{Failed: proxied.ID, Message: "ssh: handshake failed: EOF"})
	tt.Frame()
	has(t, tt, "代理连接被关闭")
	p.events = append(p.events, appui.Event{Failed: direct.ID, Message: "ssh: handshake failed: EOF"})
	tt.Frame()
	has(t, tt, "服务器不支持此认证方式")
}

func drag(t *testing.T, tt *ui.Tester, from, to string) {
	t.Helper()
	a, ok := tt.Find(from)
	b, ok2 := tt.Find(to)
	if !ok || !ok2 {
		t.Fatal("drag ends", from, ok, to, ok2)
	}
	x, y := a.X+a.W/2, a.Y+a.H/2
	tt.Press(x, y)
	for i := 1; i <= 8; i++ {
		tt.Move(x+(b.X+20-x)*float32(i)/8, y+(b.Y+b.H/2-y)*float32(i)/8)
		tt.Frame()
	}
	tt.Release(b.X+20, b.Y+b.H/2)
	tt.Frame()
	tt.Frame()
}

func TestDragHostIntoFolderAndMoveFoldersUp(t *testing.T) {
	p := newPlatform(t)
	for i, name := range []string{"甲", "乙", "丙"} {
		if e := p.SaveFolder(model.Folder{ID: name, Name: name, SortIndex: i}); e != nil {
			t.Fatal(e)
		}
	}
	h := saveHost(t, p, model.Connection{Name: "web-1", Host: "w", Port: 22, User: "u", Auth: "agent", FolderID: "甲"})
	a, _ := start(p)
	tt := manager(a)
	drag(t, tt, "拖动 web-1", "丙  0")
	snap, _ := p.Snapshot()
	for _, c := range snap.Connections {
		if c.ID == h.ID && c.FolderID != "丙" {
			t.Fatal("host not moved", c.FolderID)
		}
	}
	if e := tt.RightClick("丙  1"); e != nil {
		t.Fatal(e)
	}
	tt.ChooseMenuItem("上移")
	tt.RightClick("丙  1")
	tt.ChooseMenuItem("上移")
	snap, _ = p.Snapshot()
	order := map[string]int{}
	for _, f := range snap.Folders {
		order[f.ID] = f.SortIndex
	}
	if !(order["丙"] < order["甲"] && order["甲"] < order["乙"]) {
		t.Fatal("folder order", order)
	}
}

func TestStartupRestoresTabsRecentsAndCloseConfirmPreference(t *testing.T) {
	p := newPlatform(t)
	h := saveHost(t, p, model.Connection{Name: "db", Host: "db.example", Port: 22, User: "u", Auth: "agent"})
	prefs := p.Preferences()
	prefs.RestoreTabs = true
	p.SavePreferences(prefs)
	p.SaveUIState("tabs", "local\n"+h.ID+"\nmissing-host")
	a, tt := start(p)
	tt.Frame()
	if p.local != 1 || len(p.connects) != 1 || p.connects[0].ID != h.ID {
		t.Fatal("restore", p.local, p.connects)
	}
	if a.ExitNeedsConfirm() {
		t.Fatal("no tabs yet, nothing to confirm")
	}
	session(t, p, tt, "local", "local", model.Connection{})
	session(t, p, tt, "db", "db", h)
	if !a.ExitNeedsConfirm() {
		t.Fatal("open tabs confirm by default")
	}
	a.SaveOpenTabs()
	if got := p.UIState("tabs"); got != "local\n"+h.ID {
		t.Fatal("saved tabs", got)
	}
	prefs.NoCloseConfirm = true
	p.SavePreferences(prefs)
	a.Prefs = prefs
	if a.ExitNeedsConfirm() {
		t.Fatal("confirm switched off")
	}
	// Connecting records the host among the recents kept across runs.
	if got := p.UIState("recent"); got != h.ID {
		t.Fatal("recent", got)
	}
}

// remoteSession adds an SSH tab whose terminal shows text.
func remoteSession(t *testing.T, p *fakePlatform, tt *ui.Tester, id, text string) *appui.Session {
	t.Helper()
	s := &appui.Session{ID: id, Title: id, Terminal: newTerminal(t, text), Connection: remoteHost(id)}
	p.events = append(p.events, appui.Event{Session: s})
	tt.Frame()
	tt.Frame()
	return s
}

func TestShellIntegrationFailureIsLoggedQuietlyAndTheWindowNeverTypes(t *testing.T) {
	p := newPlatform(t)
	prefs := p.Preferences()
	prefs.ShellIntegration = "auto"
	p.SavePreferences(prefs)
	a, tt := start(p)
	s := remoteSession(t, p, tt, "r", "u@r:~$ ")
	p.events = append(p.events, appui.Event{ShellHook: &appui.ShellHookResult{Session: "r", Err: errors.New(`login shell "fish" is not bash or zsh`)}})
	tt.Frame()
	s.Terminal.Feed([]byte("\r\nu@r:~$ "))
	tt.Frame()
	tt.Frame()
	if len(p.sent) != 0 {
		t.Fatal("the window typed into the session", p.sent)
	}
	if tt.HasText("login shell") {
		t.Fatal("a failure is logged quietly, not toasted")
	}
	// It is in the activity list under 关于.
	st := ui.NewTester(a.SettingsView, 880, 640)
	click(t, st, "关于")
	if !st.HasText("Shell 集成未启用") {
		t.Fatal(st.Texts())
	}
}

func TestShellIntegrationIsManualByDefault(t *testing.T) {
	p := newPlatform(t)
	if v := p.Preferences().ShellIntegration; v != "" {
		t.Fatal("default preference", v)
	}
	a, tt := start(p)
	remoteSession(t, p, tt, "r", "u@r:~$ ")
	session(t, p, tt, "l", "本地", model.Connection{})
	if len(p.sent) != 0 {
		t.Fatal("manual mode typed into the terminal", p.sent)
	}
	st := ui.NewTester(a.SettingsView, 880, 1400)
	click(t, st, "终端")
	has(t, st, "默认。不写服务器文件；需要时在命令面板执行「插入目录跟踪脚本」，只对当前会话生效")
	click(t, st, "自动")
	has(t, st, "会在服务器上写入 ~/.cache/nextshell/osc7.sh，新的 bash / zsh 会话出现提示符后再执行一行 . ~/.cache/nextshell/osc7.sh；文件面板可跟随终端目录。其他 shell 不处理")
	if v := p.Preferences().ShellIntegration; v != "auto" {
		t.Fatal("not saved", v)
	}
}
