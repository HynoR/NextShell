package appui_test

import (
	"fmt"
	"sort"
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
	a, p, tt := fixture(t)
	var picks []appui.FilePick
	var answer []string
	a.PickFiles = func(o appui.FilePick, done func([]string)) { picks = append(picks, o); done(answer) }
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
	if e := tt.ChooseMenuItem("重命名"); e != nil {
		t.Fatal(e)
	}
	has(t, tt, "重命名 old.log")
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("new.log")
	tt.Key(0, ui.KeyEnter)
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
	answer = []string{"/tmp/report.pdf", "/tmp/b.txt"}
	click(t, tt, "上传文件…")
	if last := p.queued[len(p.queued)-1]; len(picks) != 1 || !picks[0].Multiple || last.Kind != "upload" || last.Local != "/tmp/b.txt" || p.queued[len(p.queued)-2].Remote != "/dest/report.pdf" {
		t.Fatal("upload", picks, last)
	}
	click(t, tt, "更多")
	answer = []string{"/Users/me/site"}
	click(t, tt, "上传文件夹…")
	if last := p.queued[len(p.queued)-1]; !picks[1].Directory || last.Kind != "upload" || last.Remote != "/dest/site" {
		t.Fatal("folder upload", picks, last)
	}
	answer = []string{"/Users/me/saved.log"}
	tt.RightClick("old.log")
	tt.ChooseMenuItem("下载到…")
	if last := p.queued[len(p.queued)-1]; !picks[2].Save || last.Local != "/Users/me/saved.log" || !last.Overwrite {
		t.Fatal("download to", picks, last)
	}
	n := len(p.queued)
	answer = nil // cancelled
	tt.RightClick("old.log")
	tt.ChooseMenuItem("下载到…")
	if len(p.queued) != n {
		t.Fatal("cancelled pick queued", p.queued[n:])
	}
	r, _ := tt.Find("old.log")
	if !tt.DropFiles(r.X+20, r.Y+r.H/2, "/tmp/dropped.bin") {
		t.Fatal("drop not taken")
	}
	tt.Frame()
	if last := p.queued[len(p.queued)-1]; last.Kind != "upload" || last.Local != "/tmp/dropped.bin" || last.Remote != "/dest/dropped.bin" {
		t.Fatal("drop upload", last)
	}
	// Dotfiles show on request.
	listing(p, tt, "two", "/dest", sftpfs.Entry{Name: ".env", Path: "/dest/.env"}, sftpfs.Entry{Name: "app", Path: "/dest/app"})
	if tt.HasText(".env") {
		t.Fatal("dotfile shown")
	}
	click(t, tt, "更多")
	click(t, tt, "显示隐藏文件")
	has(t, tt, ".env", "2 项")
	// Breadcrumbs jump to a parent; the path stays editable.
	listing(p, tt, "two", "/var/log/nginx")
	has(t, tt, "var", "log", "nginx")
	click(t, tt, "log")
	if p.listed[len(p.listed)-1] != "two:/var/log" {
		t.Fatal("crumb", p.listed)
	}
	click(t, tt, "编辑路径")
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("/opt")
	tt.Key(0, ui.KeyEnter)
	if p.listed[len(p.listed)-1] != "two:/opt" {
		t.Fatal("typed path", p.listed)
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
	has(t, tt, "nginx: worker process", "3 / 3 个进程")
	click(t, tt, "过滤进程")
	tt.Type("postgres")
	has(t, tt, "1 / 3 个进程")
	dblclick(t, tt, "postgres: checkpointer")
	has(t, tt, "进程 2201", "启动时间", "已运行 2 小时 1 分钟", "610 MB（3.8%）", "2190 · Ss")
	click(t, tt, "复制命令行")
	has(t, tt, "已复制命令行")
	click(t, tt, "关闭详情")
	if tt.HasText("进程 2201") {
		t.Fatal("drawer stayed open")
	}
	tt.RightClick("postgres: checkpointer")
	tt.ChooseMenuItem("详细信息")
	has(t, tt, "进程 2201")
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

func TestFilesSortFoldersFirstNaturallyAndByHeader(t *testing.T) {
	_, p, tt := fixture(t)
	session(t, p, tt, "one", "one", remoteHost("one"))
	tt.Key(ui.Cmd|ui.Shift, ui.KeyF)
	listing(p, tt, "one", "/d",
		sftpfs.Entry{Name: "file10", Path: "/d/file10", Size: 1},
		sftpfs.Entry{Name: "zeta", Path: "/d/zeta", Directory: true},
		sftpfs.Entry{Name: "File2", Path: "/d/File2", Size: 9},
		sftpfs.Entry{Name: "alpha", Path: "/d/alpha", Directory: true})
	order := func() string {
		var out []string
		for _, n := range []string{"alpha", "zeta", "File2", "file10"} {
			r, ok := tt.Find(n)
			if !ok {
				t.Fatal("missing", n)
			}
			out = append(out, fmt.Sprintf("%s@%04.0f", n, r.Y))
		}
		sort.Slice(out, func(i, j int) bool { return out[i][strings.Index(out[i], "@"):] < out[j][strings.Index(out[j], "@"):] })
		for i := range out {
			out[i] = out[i][:strings.Index(out[i], "@")]
		}
		return strings.Join(out, " ")
	}
	if got := order(); got != "alpha zeta File2 file10" {
		t.Fatal("default order", got)
	}
	click(t, tt, "大小")
	click(t, tt, "大小") // descending
	if got := order(); got != "zeta alpha File2 file10" {
		t.Fatal("size descending", got)
	}
}

func TestSnippetGroupRenameAndDeleteWithCount(t *testing.T) {
	p := newPlatform(t)
	p.SaveCommand(model.Command{ID: p.NewID(), Name: "a", Group: "运维", Text: "uptime"})
	p.SaveCommand(model.Command{ID: p.NewID(), Name: "b", Group: "运维", Text: "df -h"})
	p.SaveCommand(model.Command{ID: p.NewID(), Name: "c", Group: "其他", Text: "id"})
	_, tt := start(p)
	session(t, p, tt, "one", "one", remoteHost("one"))
	tt.Key(ui.Cmd|ui.Shift, ui.KeyS)
	tt.RightClick("分组 运维")
	if e := tt.ChooseMenuItem("重命名分组…"); e != nil {
		t.Fatal(e)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("部署")
	tt.Key(0, ui.KeyEnter)
	snap, _ := p.Snapshot()
	for _, v := range snap.Commands {
		if (v.Group == "部署") != (v.Name != "c") {
			t.Fatal("rename", v)
		}
	}
	tt.RightClick("分组 部署")
	tt.ChooseMenuItem("删除分组…")
	has(t, tt, "分组里的 2 个片段会一起删除，无法恢复。")
	click(t, tt, "删除 2 个片段")
	snap, _ = p.Snapshot()
	if len(snap.Commands) != 1 || snap.Commands[0].Name != "c" {
		t.Fatal("delete group", snap.Commands)
	}
}

func TestRemoteEditorWindowGuardsCloseAndHidesWhenEmpty(t *testing.T) {
	a, p, tt := fixture(t)
	shown, hidden := 0, 0
	a.ShowEditor = func() { shown++ }
	a.HideEditor = func() { hidden++ }
	ed := ui.NewTester(a.EditorView, 900, 640)
	session(t, p, tt, "one", "one", remoteHost("one"))
	p.events = append(p.events, appui.Event{Editor: &appui.EditorResult{Session: "one", Document: remoteedit.Document{Path: "/etc/a.conf", Text: "a=1\n"}}})
	tt.Frame()
	if shown != 1 || tt.HasText("重新载入") {
		t.Fatal("editor should open in its own window", shown)
	}
	ed.Frame()
	has(t, ed, "/etc/a.conf", "保存", "重新载入")
	ed.Type("x")
	ed.Frame()
	if a.EditorMayClose() {
		t.Fatal("closed with unsaved changes")
	}
	ed.Frame()
	has(t, ed, "1 个文件的修改尚未保存。")
	click(t, ed, "取消")
	if a.DirtyDocumentCount() != 1 {
		t.Fatal("cancel dropped the document")
	}
	a.EditorMayClose()
	ed.Frame()
	click(t, ed, "全部不保存")
	ed.Frame()
	if a.DirtyDocumentCount() != 0 || hidden != 1 {
		t.Fatal("discard all", a.DirtyDocumentCount(), hidden)
	}
	// The palette offers the editor only while documents are open.
	tt.Key(ui.Cmd, ui.KeyK)
	tt.Type("远程编辑器")
	has(t, tt, "没有结果")
}
