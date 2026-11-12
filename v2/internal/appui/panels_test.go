package appui_test

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/model"
	"nextshell.dev/v2/internal/remoteedit"
	"nextshell.dev/v2/internal/sftpfs"
)

func listing(p *fakePlatform, tt *ui.Tester, id, dir string, entries ...sftpfs.Entry) {
	p.events = append(p.events, appui.Event{Files: &appui.FileListing{Session: id, Path: dir, Entries: entries}})
	tt.Frame()
}

func TestFilesPanelBrowseClipboardAndOperations(t *testing.T) {
	_, p, tt := fixture(t)
	session(t, p, tt, "one", "one", remoteHost("one"))
	tt.Key(ui.Cmd|ui.Shift, ui.KeyF)
	if len(p.listed) != 1 || p.listed[0] != "one:." {
		t.Fatal("panel did not list the session's home", p.listed)
	}
	listing(p, tt, "one", "/src", sftpfs.Entry{Name: "a.txt", Path: "/src/a.txt", Size: 2048}, sftpfs.Entry{Name: "folder", Path: "/src/folder", Directory: true})
	has(t, tt, "a.txt", "2.0K", "2 项")
	dblclick(t, tt, "打开 folder")
	if p.listed[len(p.listed)-1] != "one:/src/folder" {
		t.Fatal("directory not opened", p.listed)
	}
	click(t, tt, "上级目录")
	if p.listed[len(p.listed)-1] != "one:/" {
		t.Fatal("up", p.listed)
	}
	click(t, tt, "选择 a.txt")
	if e := tt.ClickWith(ui.Cmd, "打开 folder"); e != nil {
		t.Fatal(e)
	}
	has(t, tt, "已选 2 / 2 项")
	tt.Key(ui.Cmd, ui.KeyC)
	has(t, tt, "已复制 2 项，可在其他目录或服务器粘贴")

	session(t, p, tt, "two", "two", remoteHost("two"))
	listing(p, tt, "two", "/dest")
	click(t, tt, "粘贴到当前目录")
	if len(p.queued) != 2 {
		t.Fatal("paste", p.queued)
	}
	for _, r := range p.queued {
		if r.Kind != "copy" || r.Session != "one" || r.TargetSession != "two" || !strings.HasPrefix(r.Destination, "/dest/") {
			t.Fatal(r)
		}
	}

	listing(p, tt, "two", "/dest", sftpfs.Entry{Name: "old.log", Path: "/dest/old.log"})
	if e := tt.RightClick("old.log"); e != nil {
		t.Fatal(e)
	}
	if e := tt.ChooseMenuItem("删除"); e != nil {
		t.Fatal(e)
	}
	click(t, tt, "删除 1 项")
	if len(p.actions) != 1 || p.actions[0] != [3]string{"remove", "/dest/old.log", ""} {
		t.Fatal("delete", p.actions)
	}
	tt.RightClick("old.log")
	tt.ChooseMenuItem("重命名…")
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("new.log")
	click(t, tt, "重命名")
	if p.actions[1] != [3]string{"rename", "/dest/old.log", "/dest/new.log"} {
		t.Fatal("rename", p.actions)
	}
	tt.RightClick("old.log")
	tt.ChooseMenuItem("下载到默认目录")
	if last := p.queued[len(p.queued)-1]; last.Kind != "download" || last.Remote != "/dest/old.log" || !strings.HasSuffix(last.Local, "old.log") {
		t.Fatal("download", last)
	}
	click(t, tt, "更多")
	click(t, tt, "新建文件夹…")
	tt.Type("logs")
	click(t, tt, "创建")
	if p.actions[2] != [3]string{"mkdir", "/dest/logs", ""} {
		t.Fatal("mkdir", p.actions)
	}
	click(t, tt, "更多")
	click(t, tt, "上传文件…")
	tt.Type("/tmp/report.pdf")
	click(t, tt, "上传到 /dest")
	if last := p.queued[len(p.queued)-1]; last.Kind != "upload" || last.Remote != "/dest/report.pdf" {
		t.Fatal("upload", last)
	}
	r, _ := tt.Find("old.log")
	if !tt.DropFiles(r.X+20, r.Y+r.H/2, "/tmp/dropped.bin") {
		t.Fatal("drop not taken")
	}
	tt.Frame()
	if last := p.queued[len(p.queued)-1]; last.Kind != "upload" || last.Local != "/tmp/dropped.bin" || last.Remote != "/dest/dropped.bin" {
		t.Fatal("drop upload", last)
	}
	// A local tab has no file panel.
	session(t, p, tt, "local", "local", model.Connection{})
	has(t, tt, "文件面板用于远程 SSH 会话")
}

func TestRemoteEditorOpensFromFilesAndGuardsDirtyClose(t *testing.T) {
	a, p, tt := fixture(t)
	session(t, p, tt, "one", "one", remoteHost("one"))
	tt.Key(ui.Cmd|ui.Shift, ui.KeyF)
	listing(p, tt, "one", "/etc", sftpfs.Entry{Name: "app.conf", Path: "/etc/app.conf"})
	dblclick(t, tt, "选择 app.conf")
	if len(p.opened) != 1 || p.opened[0] != "/etc/app.conf" {
		t.Fatal("open", p.opened)
	}
	p.events = append(p.events, appui.Event{Editor: &appui.EditorResult{Session: "one", Document: remoteedit.Document{Path: "/etc/app.conf", Text: "a=1\n"}}})
	tt.Frame()
	has(t, tt, "/etc/app.conf", "保存", "重新载入")
	if a.DirtyDocumentCount() != 0 {
		t.Fatal("clean document counted dirty")
	}
	tt.Type("x")
	tt.Frame()
	if a.DirtyDocumentCount() != 1 {
		t.Fatal("typing not tracked")
	}
	click(t, tt, "关闭文档 /etc/app.conf")
	has(t, tt, "关闭未保存的文件")
	click(t, tt, "取消")
	if a.DirtyDocumentCount() != 1 {
		t.Fatal("cancel closed")
	}
	click(t, tt, "关闭文档 /etc/app.conf")
	click(t, tt, "不保存")
	if a.DirtyDocumentCount() != 0 || tt.HasText("重新载入") {
		t.Fatal("discard")
	}
}

func TestSnippetsPanelSendParamsCreateAndDelete(t *testing.T) {
	p := newPlatform(t)
	p.SaveCommand(model.Command{ID: p.NewID(), Name: "plain", Group: "默认", Text: "uptime"})
	p.SaveCommand(model.Command{ID: p.NewID(), Name: "grep", Group: "默认", Text: "ps aux | grep [#name]"})
	_, tt := start(p)
	session(t, p, tt, "one", "one", remoteHost("one"))
	tt.Key(ui.Cmd|ui.Shift, ui.KeyS)
	click(t, tt, "片段 plain")
	if len(p.sent) != 1 || p.sent[0] != "uptime" {
		t.Fatal(p.sent)
	}
	click(t, tt, "片段 grep")
	has(t, tt, "参数原样替换，不做 shell 转义")
	tt.Type("nginx")
	has(t, tt, "ps aux | grep nginx")
	click(t, tt, "发送")
	if p.sent[1] != "ps aux | grep nginx" {
		t.Fatal(p.sent)
	}
	click(t, tt, "新建片段")
	tt.Type("df")
	click(t, tt, "命令")
	tt.Type("df -h")
	click(t, tt, "保存片段")
	snap, _ := p.Snapshot()
	if len(snap.Commands) != 3 {
		t.Fatal("snippet not saved", snap.Commands)
	}
	tt.RightClick("片段 df")
	tt.ChooseMenuItem("删除")
	click(t, tt, "删除片段")
	snap, _ = p.Snapshot()
	if len(snap.Commands) != 2 {
		t.Fatal("snippet not deleted")
	}
	click(t, tt, "片段")
	tt.RightClick("片段 plain")
	tt.ChooseMenuItem("发送到全部会话…")
	click(t, tt, "发送到 1 个会话")
	if len(p.sent) != 3 {
		t.Fatal("broadcast", p.sent)
	}
}

func TestMonitorPanelSamplesOnlyWhileVisible(t *testing.T) {
	_, p, tt := fixture(t)
	session(t, p, tt, "one", "one", remoteHost("one"))
	tt.Key(ui.Cmd|ui.Shift, ui.KeyM)
	has(t, tt, "正在采样…（仅支持 Linux 服务器）")
	now := time.Now()
	for i := range 3 {
		p.events = append(p.events, appui.Event{Monitor: &appui.MonitorResult{Session: "one", Snapshot: monitorSnapshot(now.Add(time.Duration(i)*2*time.Second), i)}})
	}
	tt.Frame()
	has(t, tt, "CPU", "内存", "网络", "磁盘", "负载 0.42 0.38 0.35")
	click(t, tt, "进程")
	has(t, tt, "nginx: worker process", "3 / 3 个进程 · 右键发送信号")
	click(t, tt, "过滤进程")
	tt.Type("postgres")
	has(t, tt, "1 / 3 个进程 · 右键发送信号")
	click(t, tt, "历史")
	has(t, tt, "CPU · %")
	session(t, p, tt, "two", "two", remoteHost("two"))
	if p.watching["one"] || !p.watching["two"] {
		t.Fatal("watch did not follow the active tab", p.watching)
	}
	click(t, tt, "文件")
	if p.watching["two"] {
		t.Fatal("still sampling with the panel hidden")
	}
}

func TestSendCommandDialogAndHistory(t *testing.T) {
	_, p, tt := fixture(t)
	session(t, p, tt, "one", "one", remoteHost("one"))
	tt.Key(ui.Cmd, ui.KeyK)
	tt.Type("发送命令")
	tt.Key(0, ui.KeyEnter)
	tt.Type("echo hi")
	tt.Key(0, ui.KeyEnter)
	snap, _ := p.Snapshot()
	if len(p.sent) != 1 || p.sent[0] != "echo hi" || len(snap.History) != 1 {
		t.Fatal("send", p.sent, snap.History)
	}
	tt.Key(ui.Cmd, ui.KeyK)
	tt.Type("发送命令")
	tt.Key(0, ui.KeyEnter)
	click(t, tt, "echo hi")
	click(t, tt, "发送到全部会话…")
	click(t, tt, "发送到 1 个会话")
	if len(p.sent) != 2 {
		t.Fatal("broadcast", p.sent)
	}
}
