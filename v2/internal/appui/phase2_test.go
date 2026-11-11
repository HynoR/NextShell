package appui_test

import (
	"github.com/egoist/mygo/ui"
	"image/png"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/model"
	"nextshell.dev/v2/internal/monitor"
	"nextshell.dev/v2/internal/remoteedit"
	"nextshell.dev/v2/internal/sftpfs"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPhase2NativePanelsAndSavedCommands(t *testing.T) {
	a, p, tt := fixture(t)
	for _, page := range []struct{ button, title string }{{"文件", "SFTP 文件"}, {"传输", "传输队列"}, {"远程编辑", "远程文本编辑（UTF-8，最大 8 MiB）"}, {"监控", "Linux 监控 / 进程 / 网络"}, {"命令库", "保存到命令库"}, {"导入导出", "导入先预览再确认，密码不会在预览显示；XOR 旧导出按计划不支持"}} {
		click(t, tt, page.button)
		if !tt.HasText(page.title) {
			t.Fatal(page, tt.Texts())
		}
	}
	a.Page = "commands"
	a.Phase2.Command = model.Command{Name: "UI saved", Text: "pwd", Group: "daily"}
	tt.Frame()
	click(t, tt, "保存到命令库")
	snap, e := p.Snapshot()
	if e != nil || len(snap.Commands) != 1 || snap.Commands[0].Text != "pwd" {
		t.Fatal(snap, e)
	}
	click(t, tt, "在全部会话执行")
	if !strings.Contains(a.Message, "确认") {
		t.Fatal("all-session confirmation bypassed")
	}
	p.events = []appui.Event{{Editor: &appui.EditorResult{Session: "remote", Document: remoteedit.Document{Path: "/tmp/test", Text: "original"}}}}
	tt.Frame()
	if a.Page != "editor" {
		t.Fatal("editor event lost")
	}
	a.Phase2.EditorText = "unsaved"
	p.events = []appui.Event{{Editor: &appui.EditorResult{Session: "remote", Document: remoteedit.Document{Path: "/tmp/late", Text: "late"}}}}
	tt.Frame()
	if len(a.Phase2.Documents) != 2 || a.Phase2.Documents[0].Text != "unsaved" {
		t.Fatal("late editor response lost unsaved text")
	}
	if path := os.Getenv("NEXTSHELL_PHASE2_SCREENSHOT"); path != "" {
		f, e := os.Create(path)
		if e != nil {
			t.Fatal(e)
		}
		e = png.Encode(f, tt.Image())
		f.Close()
		if e != nil {
			t.Fatal(e)
		}
	}
}
func TestPhase2NativeFilledPanelsRender(t *testing.T) {
	a, p, _ := fixture(t)
	session := &appui.Session{ID: "sftp-session", Title: "Remote", Terminal: newTerminal(t, "hello"), Local: false}
	p.events = []appui.Event{{Session: session}, {Files: &appui.FileListing{Session: session.ID, Path: "/home/test", Entries: []sftpfs.Entry{{Name: "file", Path: "/home/test/file", Size: 123, Mode: "-rw-------"}}}}, {Monitor: &appui.MonitorResult{Session: session.ID, Snapshot: monitor.Snapshot{At: time.Now(), Info: "Linux fixture", MemoryTotal: 1024, MemoryAvailable: 512, Processes: []monitor.Process{{PID: 100, User: "root", Command: "sshd"}}, Network: []monitor.Network{{Protocol: "tcp", State: "LISTEN", LocalIP: "::", LocalPort: 22}}}}}, {Import: &appui.ImportPreview{Token: "token", Count: 1, Names: []string{"server"}, Warnings: []string{"fixture warning"}}}}
	tt := ui.NewTester(a.View, 1600, 1800)
	for _, page := range []string{"files", "transfers", "editor", "monitor", "commands", "data"} {
		a.Page = page
		tt.Frame()
		tt.Frame()
	}
	a.Page = "monitor"
	a.Phase2.MonitorPage = "system"
	tt.Frame()
	if !tt.HasText("Linux fixture") {
		t.Fatal("populated monitor was not rendered")
	}
	a.Phase2.MonitorPage = "process"
	tt.Frame()
	a.Phase2.MonitorPage = "network"
	tt.Frame()
}

type followPlatform struct {
	*fakePlatform
	paths []string
}

func (p *followPlatform) SessionDir(id string) string {
	if term := p.terminals[id]; term != nil {
		return term.Dir()
	}
	return ""
}
func (p *followPlatform) ListFiles(session, path string) { p.paths = append(p.paths, path) }
func TestPhase2FollowTerminalOSC7ThroughPlatform(t *testing.T) {
	a, p, _ := fixture(t)
	fp := &followPlatform{fakePlatform: p}
	a.P = fp
	term := newTerminal(t, "")
	p.events = []appui.Event{{Session: &appui.Session{ID: "remote", Title: "Remote", Terminal: term}}}
	tt := ui.NewTester(a.View, 1400, 1000)
	a.Page = "files"
	a.Phase2.FollowCWD = true
	term.Feed([]byte("\x1b]7;file://localhost/home/remote\x07"))
	tt.Frame()
	if len(fp.paths) != 1 || fp.paths[0] != "/home/remote" {
		t.Fatal("OSC7 follow was not routed through Platform", fp.paths)
	}
	tt.Frame()
	if len(fp.paths) != 1 {
		t.Fatal("same cwd triggered repeated reads", fp.paths)
	}
}
