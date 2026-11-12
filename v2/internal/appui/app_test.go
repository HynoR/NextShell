package appui_test

import (
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
	has(t, tt, "NextShell", "最近", "saved-box", "+ 新建主机", "导入…", "打开本地终端", "未打开会话")
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
	click(t, tt, "打开本地终端")
	if p.local != 1 {
		t.Fatal("local terminal")
	}
	// The saved host connecting shows amber until its session arrives.
	session(t, p, tt, "s1", "saved-box", saved)
	has(t, tt, "ops@10.9.9.9 · 已连接 不到 1 分钟", "UTF-8")
	if tt.HasText("最近") {
		t.Fatal("welcome shown with a tab")
	}
	tt.Key(appMod(), ui.KeyB)
	if tt.HasText("主机") && tt.HasText("本地终端") {
		t.Fatal("sidebar not hidden")
	}
	tt.Key(appMod(), ui.KeyB)
	tt.Key(ui.Cmd|ui.Shift, ui.KeyM)
	has(t, tt, "文件", "监控", "片段")
	if !p.watching["s1"] {
		t.Fatal("monitor not sampled while visible")
	}
	tt.Key(appMod(), ui.KeyJ)
	if tt.HasText("片段") || p.watching["s1"] {
		t.Fatal("panel did not close or monitor kept sampling")
	}
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
	for _, section := range []string{"通用", "终端", "SSH", "密钥", "代理", "同步", "Agent", "导入导出", "回收站", "网络诊断", "关于"} {
		click(t, st, section)
		check(st, section)
	}
}

func TestHostEditorCreateEditValidateAndTrash(t *testing.T) {
	a, p, tt := fixture(t)
	session(t, p, tt, "local", "本地", model.Connection{}) // hide the welcome page's recent hosts
	click(t, tt, "新建")
	click(t, tt, "新建主机…")
	has(t, tt, "新建主机", "常规", "认证", "高级")
	tt.Type("web-a")
	click(t, tt, "主机")
	tt.Type("10.0.0.7")
	click(t, tt, "用户名")
	tt.Type("root")
	click(t, tt, "认证")
	click(t, tt, "密码")
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
	if len(p.connects) != 1 || p.connects[0].Port != 2200 {
		t.Fatal("save and connect", p.connects)
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
	saveHost(t, p, model.Connection{Name: "alpha", Host: "a.example", User: "u", FolderID: prod.ID})
	saveHost(t, p, model.Connection{Name: "beta", Host: "b.example", User: "u", FolderID: child.ID})
	saveHost(t, p, model.Connection{Name: "gamma", Host: "g.example", User: "u"})
	_, tt := start(p)
	session(t, p, tt, "local", "本地", model.Connection{})
	has(t, tt, "生产  2", "子目录  1", "alpha", "beta", "gamma")
	click(t, tt, "生产  2")
	if tt.HasText("alpha") || tt.HasText("beta") {
		t.Fatal("collapse")
	}
	click(t, tt, "生产  2")
	click(t, tt, "过滤主机")
	tt.Type("b.exam")
	if tt.HasText("alpha") || !tt.HasText("beta") || tt.HasText("gamma") {
		t.Fatal("filter", tt.Texts())
	}
	click(t, tt, "过滤主机")
	if e := tt.RightClick("gamma"); e != nil {
		t.Fatal(e)
	}
	if e := tt.ChooseMenuItem("收藏"); e != nil {
		t.Fatal(e)
	}
	has(t, tt, "★ 收藏")
	if e := tt.RightClick("生产  2"); e != nil {
		t.Fatal(e)
	}
	if e := tt.ChooseMenuItem("批量设置认证…"); e != nil {
		t.Fatal(e)
	}
	has(t, tt, "将应用到「生产」及子目录中的 2 个主机")
	click(t, tt, "SSH Agent")
	click(t, tt, "应用到 2 个主机")
	snap, _ := p.Snapshot()
	for _, c := range snap.Connections {
		if (c.Name == "gamma") == (c.Auth == "agent") {
			t.Fatal("batch scope", c.Name, c.Auth)
		}
	}
	dblclick(t, tt, "beta")
	if len(p.connects) != 1 || p.connects[0].Name != "beta" {
		t.Fatal("double click connect", p.connects)
	}
	// A failed connect turns the dot red; Failed carries the host's ID.
	p.events = append(p.events, appui.Event{Failed: p.connects[0].ID, Message: "connection refused"})
	tt.Frame()
	has(t, tt, "connection refused")

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
