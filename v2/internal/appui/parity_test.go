package appui_test

import (
	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/model"
	"nextshell.dev/v2/internal/sftpfs"
	"strings"
	"testing"
)

func chooseMenu(t *testing.T, tt *ui.Tester, label, item string) {
	t.Helper()
	if e := tt.RightClick(label); e != nil {
		t.Fatal(e)
	}
	if e := tt.ChooseMenuItem(item); e != nil {
		t.Fatal(item, tt.Menu(), e)
	}
	tt.Frame()
}
func TestParitySettingsSaveAndReload(t *testing.T) {
	a, p, tt := fixture(t)
	a.Page = "settings"
	a.ShowWorkbench = false
	tt.Frame()
	click(t, tt, "终端字号（10..24）")
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("18")
	click(t, tt, "保存设置")
	if p.Preferences().FontSize != 18 || a.Prefs.FontSize != 18 {
		t.Fatal(a.Message, p.Preferences())
	}
	b := appui.New(p)
	if b.Prefs.FontSize != 18 {
		t.Fatal("not restored")
	}
	click(t, tt, "终端字号（10..24）")
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("99")
	click(t, tt, "保存设置")
	if p.Preferences().FontSize != 18 || !strings.Contains(a.Message, "10..24") {
		t.Fatal("invalid committed", a.Message)
	}
}
func TestParityConnectionContextScopeRenameAndTrash(t *testing.T) {
	a, p, tt := fixture(t)
	ids := []string{p.NewID(), p.NewID()}
	for i, n := range []string{"alpha", "beta"} {
		c := model.Connection{ID: ids[i], Name: n, Host: "::1", Port: 2222, User: "tester", Auth: "agent", Tags: []string{"project-x"}, Notes: "auditnote"}
		if e := p.SaveConnection(c, nil); e != nil {
			t.Fatal(e)
		}
	}
	a.Data, _ = p.Snapshot()
	tt.Frame()
	a.Selected[ids[1]] = true
	chooseMenu(t, tt, "alpha", "复制地址")
	if tt.Clipboard() != "tester@[::1]:2222" {
		t.Fatal(tt.Clipboard())
	}
	chooseMenu(t, tt, "alpha", "导出连接文件…")
	if !a.Selected[ids[0]] || a.Selected[ids[1]] || a.Page != "data" {
		t.Fatal(a.Selected)
	}
	a.Page = "terminal"
	tt.Frame()
	chooseMenu(t, tt, "alpha", "重命名连接")
	click(t, tt, "连接新名称")
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("renamed")
	click(t, tt, "确认重命名")
	s, _ := p.Snapshot()
	if s.Connections[0].Name != "renamed" && s.Connections[1].Name != "renamed" {
		t.Fatal(s.Connections)
	}
	a.Page = "terminal"
	tt.Frame()
	chooseMenu(t, tt, "renamed", "移入回收站")
	if len(a.TrashTargets) != 1 || a.TrashTargets[0].ID != ids[0] {
		t.Fatal(a.TrashTargets)
	}
	click(t, tt, "取消")
	a.Search = "auditnote"
	tt.Frame()
	if !tt.HasText("renamed") {
		t.Fatal("notes not searched")
	}
	a.Search = "project-x"
	tt.Frame()
	if !tt.HasText("renamed") {
		t.Fatal("tags not searched")
	}
}
func TestParityTerminalScopedTabShortcuts(t *testing.T) {
	a, p, tt := fixture(t)
	p.events = []appui.Event{{Session: &appui.Session{ID: "a", Title: "one", Terminal: newTerminal(t, "one")}}, {Session: &appui.Session{ID: "b", Title: "two", Terminal: newTerminal(t, "two")}}}
	tt.Frame()
	tt.Key(ui.Cmd, ui.Key1)
	tt.Frame()
	if a.Active != 0 {
		t.Fatal("Cmd1", a.Active)
	}
	tt.Key(ui.Ctrl, ui.KeyPageDown)
	tt.Frame()
	if a.Active != 1 {
		t.Fatal("next", a.Active)
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyD)
	tt.Frame()
	if p.cloned != 1 {
		t.Fatal("duplicate", p.cloned)
	}
	tt.Key(ui.Ctrl, ui.KeyW)
	tt.Frame()
	if len(a.Tabs) != 2 {
		t.Fatal("shell CtrlW intercepted")
	}
	click(t, tt, "标签名称")
	tt.Key(ui.Cmd, ui.Key1)
	tt.Frame()
	if a.Active != 1 {
		t.Fatal("input shortcut stolen")
	}
	chooseMenu(t, tt, "two", "关闭标签")
	if len(a.Tabs) != 1 {
		t.Fatal("context close")
	}
}
func TestParityFileMenuTargetsClickedRow(t *testing.T) {
	_, base, _ := fixture(t)
	p := &tablePlatform{fakePlatform: base}
	a := appui.New(p)
	base.events = []appui.Event{{Session: &appui.Session{ID: "files", Title: "test", Terminal: newTerminal(t, "ok")}}}
	tt := ui.NewTester(a.View, 1400, 1000)
	a.Phase2.FilesSession = "files"
	a.Phase2.Files = []sftpfs.Entry{{Name: "one.txt", Path: "/one.txt"}, {Name: "two.txt", Path: "/two.txt"}}
	a.Page = "files"
	tt.Frame()
	a.Phase2.SelectedRemote = "/one.txt"
	chooseMenu(t, tt, "选择 two.txt", "复制路径")
	if tt.Clipboard() != "/two.txt" {
		t.Fatal(tt.Clipboard())
	}
	chooseMenu(t, tt, "选择 two.txt", "重命名远端路径")
	if a.Phase2.ActionFrom != "/two.txt" {
		t.Fatal(a.Phase2.ActionFrom)
	}
}

func TestFolderContextAndConnectionMetadataEntry(t *testing.T) {
	a, p, tt := fixture(t)
	root := model.Folder{ID: p.NewID(), Name: "parent"}
	child := model.Folder{ID: p.NewID(), Name: "child", ParentID: root.ID}
	for _, v := range []model.Folder{root, child} {
		if e := p.SaveFolder(v); e != nil {
			t.Fatal(e)
		}
	}
	c := model.Connection{ID: p.NewID(), Name: "node", Host: "localhost", Port: 22, User: "tester", Auth: "agent", FolderID: child.ID}
	if e := p.SaveConnection(c, nil); e != nil {
		t.Fatal(e)
	}
	a.Data, _ = p.Snapshot()
	tt.Frame()
	chooseMenu(t, tt, "parent", "批量绑定目录认证（含子目录）")
	if !a.Selected[c.ID] || a.Page != "connections" {
		t.Fatal(a.Selected)
	}
	a.Edit = c
	a.Port = "22"
	a.Keepalive = "15"
	a.ConnectionTags = "prod, prod, linux"
	a.Edit.Notes = "my note"
	tt.Frame()
	click(t, tt, "保存连接")
	snapshot, _ := p.Snapshot()
	if len(snapshot.Connections[0].Tags) != 2 || snapshot.Connections[0].Notes != "my note" {
		t.Fatal(snapshot.Connections)
	}
}
