package appui_test

import (
	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/model"
	"nextshell.dev/v2/internal/sftpfs"
	"testing"
	"time"
)

func TestRefreshedNavigationBothAppearances(t *testing.T) {
	for _, dark := range []bool{false, true} {
		t.Run(map[bool]string{false: "light", true: "dark"}[dark], func(t *testing.T) {
			a, p, _ := fixture(t)
			observed := false
			tt := ui.NewTesterWith(func(c *ui.Context) { a.View(c); observed = c.Theme().Dark }, 1400, 1000, ui.TesterOptions{Dark: dark})
			if observed != dark {
				t.Fatal("system appearance ignored")
			}
			for _, label := range []string{"连接", "文件", "命令库", "云同步", "Agent 网关", "终端"} {
				click(t, tt, label)
			}
			if a.Page != "terminal" {
				t.Fatal(a.Page)
			}
			a.Appearance = "深色"
			tt.Frame()
			if !observed {
				t.Fatal("explicit dark ignored")
			}
			a.Appearance = "浅色"
			tt.Frame()
			if observed {
				t.Fatal("explicit light ignored")
			}
			a.Appearance = "跟随系统"
			tt.SetDark(!dark)
			tt.Frame()
			if observed == dark {
				t.Fatal("system change ignored")
			}
			p.events = []appui.Event{{Session: &appui.Session{ID: "refresh-1", Title: "One", Terminal: newTerminal(t, "one")}}, {Session: &appui.Session{ID: "refresh-2", Title: "Two", Terminal: newTerminal(t, "two")}}}
			tt.Frame()
			click(t, tt, "One")
			if a.Active != 0 || a.TabName != "One" {
				t.Fatal("tab selection lost")
			}
			click(t, tt, "Two")
			click(t, tt, "关闭标签 Two")
			if len(a.Tabs) != 1 {
				t.Fatal("tab close failed")
			}
		})
	}
}
func TestRefreshedSidebarSelectionEditAndFold(t *testing.T) {
	a, p, tt := fixture(t)
	f := model.Folder{ID: p.NewID(), Name: "Lab"}
	if e := p.SaveFolder(f); e != nil {
		t.Fatal(e)
	}
	c := model.Connection{ID: p.NewID(), Name: "node", Host: "localhost", Port: 22, User: "tester", Auth: "agent", FolderID: f.ID}
	if e := p.SaveConnection(c, nil); e != nil {
		t.Fatal(e)
	}
	a.Data, _ = p.Snapshot()
	tt.Frame()
	click(t, tt, "node")
	if a.TreeSelection != c.ID || len(p.connects) != 1 {
		t.Fatal("sidebar selection/connect failed", a.TreeSelection, len(p.connects))
	}
	click(t, tt, "编辑")
	if a.Edit.ID != c.ID {
		t.Fatal("edit action lost")
	}
	a.Page = "terminal"
	tt.Frame()
	click(t, tt, "Lab")
	if tt.HasText("node") {
		t.Fatal("folder did not fold")
	}
	click(t, tt, "Lab")
	if !tt.HasText("node") {
		t.Fatal("folder did not expand")
	}
}

type tablePlatform struct {
	*fakePlatform
	listed, opened string
}

func (p *tablePlatform) ListFiles(_, path string)  { p.listed = path }
func (p *tablePlatform) OpenRemote(_, path string) { p.opened = path }
func TestRefreshedSFTPTableActions(t *testing.T) {
	_, base, _ := fixture(t)
	p := &tablePlatform{fakePlatform: base}
	a := appui.New(p)
	base.events = []appui.Event{{Session: &appui.Session{ID: "table-session", Title: "Lab", Terminal: newTerminal(t, "hello")}}}
	tt := ui.NewTester(a.View, 1400, 1000)
	a.Phase2.FilesSession = "table-session"
	a.Phase2.RemotePath = "/lab"
	a.Phase2.Files = []sftpfs.Entry{{Name: "sample.txt", Path: "/lab/sample.txt", Mode: "-rw-------", Size: 42}, {Name: "dir", Path: "/lab/dir", Directory: true}}
	a.Page = "files"
	tt.Frame()
	click(t, tt, "sample.txt")
	if a.Phase2.SelectedRemote != "/lab/sample.txt" {
		t.Fatal("file selection lost")
	}
	click(t, tt, "编辑文件")
	if p.opened != "/lab/sample.txt" || a.Page != "editor" {
		t.Fatal("editor action lost")
	}
	a.Page = "files"
	tt.Frame()
	click(t, tt, "dir")
	tt.Key(0, ui.KeyEnter)
	if p.listed != "/lab/dir" {
		t.Fatal("directory Enter action lost", p.listed)
	}
	p.listed = ""
	tt.Advance(time.Second)
	click(t, tt, "打开 dir")
	click(t, tt, "打开 dir")
	if p.listed != "/lab/dir" {
		t.Fatal("directory navigation lost", p.listed)
	}
}
