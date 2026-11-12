package appui_test

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/model"
	"nextshell.dev/v2/internal/monitor"
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
	click(t, tt, "SFTP")
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
	has(t, tt, "SFTP 用于远程 SSH 会话")
}

func TestRemoteEditorOpensFromFilesAndGuardsDirtyClose(t *testing.T) {
	a, p, tt := fixture(t)
	session(t, p, tt, "one", "one", remoteHost("one"))
	click(t, tt, "SFTP")
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
	tt.RightClick("片段 plain")
	tt.ChooseMenuItem("发送到全部会话…")
	click(t, tt, "发送到 1 个会话")
	if len(p.sent) != 3 {
		t.Fatal("broadcast", p.sent)
	}
}

func TestMonitorSamplesOnlyWhileVisible(t *testing.T) {
	_, p, tt := fixture(t)
	tt.SetSize(1280, 1100) // room for the process drawer
	session(t, p, tt, "one", "one", remoteHost("one"))
	has(t, tt, "系统监控", "正在读取服务器状态…")
	if !p.watching["one"] || p.detailed["one"] {
		t.Fatal("the left column samples lightly", p.watching, p.detailed)
	}
	now := time.Now()
	light := monitorSnapshot(now, 0)
	light.Detail, light.Processes, light.Network = false, nil, nil
	p.events = append(p.events, appui.Event{Monitor: &appui.MonitorResult{Session: "one", Snapshot: light}})
	tt.Frame()
	has(t, tt, "CPU", "内存", "交换", "磁盘", "0.42 0.38 0.35", "20%", "59.0G / 80.0G", "↑ 39.1K/s", "Ubuntu 24.04 LTS · 4 核 · 运行 12 天")
	if tt.HasText("缓存数据") {
		t.Fatal("a fresh sample is not cached data")
	}
	// 进程管理器 opens the host's process tab, which waits for a full sample instead of showing none.
	click(t, tt, "进程管理器")
	if !p.detailed["one"] {
		t.Fatal("process tab without the full probe")
	}
	has(t, tt, "进程 · one", "正在读取服务器状态…")
	if tt.HasText("0 / 0 个进程") {
		t.Fatal("a light sample showed as no processes")
	}
	for i := range 3 {
		p.events = append(p.events, appui.Event{Monitor: &appui.MonitorResult{Session: "one", Snapshot: monitorSnapshot(now.Add(time.Duration(i+1)*2*time.Second), i)}})
	}
	tt.Frame()
	has(t, tt, "nginx: worker process", "3 / 3 个进程")
	// A light sample after a full one keeps the last full tables.
	light.At = now.Add(10 * time.Second)
	p.events = append(p.events, appui.Event{Monitor: &appui.MonitorResult{Session: "one", Snapshot: light}})
	tt.Frame()
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
	// The network manager is a tab of its own; both keep the full probe until they close.
	click(t, tt, "网络管理器")
	has(t, tt, "网络 · one", "tcp 监听")
	tt.Key(appMod(), ui.KeyW) // closes the tool tab shown, not the host's session
	if tt.HasText("网络 · one") || !tt.HasText("进程 · one") || !p.detailed["one"] || len(p.closed) != 0 {
		t.Fatal("the process tab still needs the full probe", p.detailed, p.closed)
	}
	click(t, tt, "关闭标签 进程 · one")
	if p.detailed["one"] || !p.watching["one"] {
		t.Fatal("closing the tabs stops the full probe, not the left column", p.watching, p.detailed)
	}
	session(t, p, tt, "two", "two", remoteHost("two"))
	if p.watching["one"] || !p.watching["two"] || p.detailed["two"] {
		t.Fatal("watch did not follow the active tab", p.watching, p.detailed)
	}
	tt.Key(appMod(), ui.KeyB)
	click(t, tt, "SFTP")
	if p.watching["two"] {
		t.Fatal("still sampling with nothing showing it")
	}
}

// A host's data outlives its tab and its sampling: switching back shows it at once, labeled, until a fresh sample.
func TestMonitorKeepsCachedDataPerHost(t *testing.T) {
	_, p, tt := fixture(t)
	session(t, p, tt, "one", "one", remoteHost("one"))
	old := monitorSnapshot(time.Now().Add(-time.Minute), 0)
	p.events = append(p.events, appui.Event{Monitor: &appui.MonitorResult{Session: "one", Snapshot: old}})
	tt.Frame()
	has(t, tt, "缓存数据", "0.42 0.38 0.35")
	p.events = append(p.events, appui.Event{Monitor: &appui.MonitorResult{Session: "one", Snapshot: monitorSnapshot(time.Now(), 1)}})
	tt.Frame()
	if tt.HasText("缓存数据") {
		t.Fatal("a fresh sample is live")
	}
	session(t, p, tt, "two", "two", remoteHost("two"))
	if tt.HasText("0.42 0.38 0.35") {
		t.Fatal("another host showed this host's data")
	}
	tt.Key(ui.Cmd, ui.Key1)
	has(t, tt, "0.42 0.38 0.35") // back on the first host: its last sample, at once
}

// A server the probe cannot read (not Linux, a router) fails every sample. That is said in place, not toasted every
// 2 s; after three failures in a row the server is left alone, with a way to turn its monitoring off.
func TestMonitorUnreadableServerStopsSamplingAndCanBeTurnedOff(t *testing.T) {
	_, p, tt := fixture(t)
	host := saveHost(t, p, model.Connection{Name: "router", Host: "router.example", User: "admin"})
	p.events = append(p.events, appui.Event{Message: "saved"}) // a reload, as after any save
	session(t, p, tt, "one", "one", host)
	fail := func(err error, n int) {
		for range n {
			p.events = append(p.events, appui.Event{Monitor: &appui.MonitorResult{Session: "one", Err: err}})
		}
		tt.Frame()
	}
	unsupported := fmt.Errorf("%w: Process exited with status 44", monitor.ErrUnsupported)
	fail(unsupported, 2)
	if !p.watching["one"] || !tt.HasText("正在读取服务器状态…") || tt.HasText("无法读取此服务器的状态（目前只支持 Linux）") {
		t.Fatal("two failures already gave up", p.watching, tt.Texts())
	}
	fail(unsupported, 1)
	has(t, tt, "无法读取此服务器的状态（目前只支持 Linux）", "关闭此服务器的监控", "重试")
	if p.watching["one"] || tt.HasText("正在读取服务器状态…") || tt.HasText("Process exited") {
		t.Fatal("still sampling, or showed the raw error", p.watching, tt.Texts())
	}
	// The 系统信息 tab says the same.
	tt.Key(ui.Cmd|ui.Shift, ui.KeyM)
	has(t, tt, "无法读取此服务器的状态（目前只支持 Linux）")
	// 重试 samples again, and gives up again after three more.
	click(t, tt, "重试")
	tt.Frame()
	if !p.watching["one"] || tt.HasText("关闭此服务器的监控") {
		t.Fatal("retry did not resume", p.watching, tt.Texts())
	}
	fail(errors.New("context deadline exceeded"), 3)
	has(t, tt, "无法读取此服务器的状态（连接不稳定或服务器没有响应）")
	// 关闭此服务器的监控 is saved with the host, and the panel says it is off.
	click(t, tt, "关闭此服务器的监控")
	tt.Frame()
	has(t, tt, "此服务器的监控已关闭")
	if snap, _ := p.Snapshot(); len(snap.Connections) != 1 || snap.Connections[0].Monitor {
		t.Fatal("the switch was not saved", snap.Connections)
	}
	if p.watching["one"] {
		t.Fatal("sampling a host whose monitoring went off")
	}
}

// A good sample ends the count; so does a reconnect, which is a new session.
func TestMonitorFailureCountResets(t *testing.T) {
	_, p, tt := fixture(t)
	session(t, p, tt, "one", "one", remoteHost("one"))
	err := errors.New("timeout")
	send := func(session string, e error, n int) {
		for i := range n {
			r := &appui.MonitorResult{Session: session, Err: e}
			if e == nil {
				r.Snapshot = monitorSnapshot(time.Now().Add(time.Duration(i)*time.Second), 0)
			}
			p.events = append(p.events, appui.Event{Monitor: r})
		}
		tt.Frame()
	}
	send("one", err, 2)
	send("one", nil, 1)
	send("one", err, 2)
	if !p.watching["one"] || tt.HasText("关闭此服务器的监控") {
		t.Fatal("failures that were not in a row stopped it")
	}
	send("one", err, 1)
	has(t, tt, "关闭此服务器的监控")
	// The same host, a new session: sampled again, from a clean count.
	session(t, p, tt, "two", "two", remoteHost("one"))
	if !p.watching["two"] || tt.HasText("关闭此服务器的监控") {
		t.Fatal("a reconnected host stayed given up on", p.watching, tt.Texts())
	}
}

// The process and network tabs share the host's samples: they stop with it, and say why.
func TestMonitorToolTabsShowTheFailure(t *testing.T) {
	_, p, tt := fixture(t)
	session(t, p, tt, "one", "one", remoteHost("one"))
	p.events = append(p.events, appui.Event{Monitor: &appui.MonitorResult{Session: "one", Snapshot: monitorSnapshot(time.Now(), 0)}})
	tt.Frame()
	click(t, tt, "进程管理器")
	if !p.detailed["one"] {
		t.Fatal("no full probe for the process tab")
	}
	for range 3 {
		p.events = append(p.events, appui.Event{Monitor: &appui.MonitorResult{Session: "one", Err: monitor.ErrUnsupported}})
	}
	tt.Frame()
	tt.Frame()
	has(t, tt, "进程 · one", "无法读取此服务器的状态（目前只支持 Linux）")
	if p.detailed["one"] || p.watching["one"] || tt.HasText("个进程") {
		t.Fatal("the tab kept sampling or showed old rows", p.detailed, p.watching)
	}
}

func TestMonitorOffStopsSampling(t *testing.T) {
	_, p, tt := fixture(t)
	off := remoteHost("off")
	off.Monitor = false
	session(t, p, tt, "off", "off", off)
	has(t, tt, "此服务器的监控已关闭")
	if p.watching["off"] {
		t.Fatal("sampled a host whose monitoring is off")
	}
	// The saved record decides, so turning it off in the manager takes effect on a tab already open.
	on := saveHost(t, p, model.Connection{Name: "saved", Host: "saved.example", User: "u"})
	session(t, p, tt, "saved", "saved", on)
	if !p.watching["saved"] {
		t.Fatal("a new host is not sampled")
	}
	on.Monitor = false
	if e := p.SaveConnection(on, nil); e != nil {
		t.Fatal(e)
	}
	p.events = append(p.events, appui.Event{Message: "saved"}) // a reload
	tt.Frame()
	tt.Frame()
	if p.watching["saved"] {
		t.Fatal("still sampling after the switch went off")
	}
}

func TestMonitorSidebarFoldsAndPicksAnInterface(t *testing.T) {
	_, p, tt := fixture(t)
	session(t, p, tt, "one", "one", remoteHost("one"))
	for i := range 4 {
		snap := monitorSnapshot(time.Now().Add(time.Duration(i-4)*time.Second), i)
		snap.Interfaces = map[string]monitor.InterfaceRate{"eth0": {RXPerSecond: 2048, TXPerSecond: 1024, HasDelta: true}, "wg0": {RXPerSecond: 512, TXPerSecond: 256, HasDelta: true}}
		p.events = append(p.events, appui.Event{Monitor: &appui.MonitorResult{Session: "one", Snapshot: snap, Latency: 18 * time.Millisecond}})
	}
	tt.Frame()
	has(t, tt, "↑ 47.9K/s", "18 ms")
	click(t, tt, "网卡")
	click(t, tt, "wg0")
	has(t, tt, "↑ 256 B/s", "↓ 512 B/s")
	click(t, tt, "系统监控")
	tt.Advance(400 * time.Millisecond) // the section closes with an animation
	if tt.HasText("0.42 0.38 0.35") {
		t.Fatal("the section did not fold")
	}
	if !strings.HasSuffix(p.UIState("layout"), " true") {
		t.Fatal("the fold is not remembered", p.UIState("layout"))
	}
	click(t, tt, "系统监控")
	tt.Advance(400 * time.Millisecond)
	has(t, tt, "0.42 0.38 0.35")
}

func TestConnectingTabAppearsOnFirstConnect(t *testing.T) {
	p := newPlatform(t)
	host := saveHost(t, p, model.Connection{Name: "first", Host: "first.example", User: "u"})
	a, tt := start(p)
	click(t, tt, "快速连接")
	tt.Type("first")
	tt.Key(0, ui.KeyEnter)
	has(t, tt, "连接中", "正在连接 first…")
	session(t, p, tt, "one", "first", host)
	if tt.HasText("正在连接 first…") || len(a.Tabs) != 1 {
		t.Fatal("the connecting tab stayed after the session arrived")
	}
}

func TestConnectingTabCancels(t *testing.T) {
	p := newPlatform(t)
	host := saveHost(t, p, model.Connection{Name: "first", Host: "first.example", User: "u"})
	a, tt := start(p)
	connect := func() {
		click(t, tt, "快速连接")
		tt.Type("first")
		tt.Key(0, ui.KeyEnter)
		has(t, tt, "正在连接 first…")
	}
	connect()
	click(t, tt, "取消连接 first") // the tab's close button
	connect()
	tt.Key(0, ui.KeyEscape)
	connect()
	tt.Key(appMod(), ui.KeyW)
	if tt.HasText("正在连接 first…") || len(p.cancelled) != 3 || p.cancelled[2] != host.ID {
		t.Fatal("cancel", p.cancelled, tt.Texts())
	}
	// Closing a tab that is reconnecting stops the reconnect too.
	s := session(t, p, tt, "one", "first", host)
	s.Terminal.Close()
	<-s.Terminal.Done()
	tt.Frame()
	click(t, tt, "重新连接")
	tt.Key(appMod(), ui.KeyW)
	if len(a.Tabs) != 0 || len(p.cancelled) != 4 {
		t.Fatal("reconnect not cancelled", p.cancelled)
	}
}

func TestSendCommandDialogAndHistory(t *testing.T) {
	_, p, tt := fixture(t)
	session(t, p, tt, "one", "one", remoteHost("one"))
	tt.Key(appMod(), ui.KeyK)
	tt.Type("发送命令")
	tt.Key(0, ui.KeyEnter)
	tt.Type("echo hi")
	tt.Key(0, ui.KeyEnter)
	snap, _ := p.Snapshot()
	if len(p.sent) != 1 || p.sent[0] != "echo hi" || len(snap.History) != 1 {
		t.Fatal("send", p.sent, snap.History)
	}
	tt.Key(appMod(), ui.KeyK)
	tt.Type("发送命令")
	tt.Key(0, ui.KeyEnter)
	click(t, tt, "echo hi")
	click(t, tt, "发送到全部会话…")
	click(t, tt, "发送到 1 个会话")
	if len(p.sent) != 2 {
		t.Fatal("broadcast", p.sent)
	}
}

func TestFilesDirectoryTreeFollowsAndLists(t *testing.T) {
	_, p, tt := fixture(t)
	session(t, p, tt, "one", "one", remoteHost("one"))
	tt.Key(appMod(), ui.KeyJ) // the workbench was hidden: show it, on SFTP
	tt.Key(appMod(), ui.KeyJ)
	listing(p, tt, "one", "/var/log", sftpfs.Entry{Name: "nginx", Path: "/var/log/nginx", Directory: true}, sftpfs.Entry{Name: "syslog", Path: "/var/log/syslog"})
	tt.Frame()
	has(t, tt, "目录 /", "目录 /var", "目录 /var/log", "目录 /var/log/nginx")
	// The open folders above list their subfolders for the tree alone.
	if !slices.Contains(p.listed, "one:/") || !slices.Contains(p.listed, "one:/var") {
		t.Fatal("tree did not list the folders above", p.listed)
	}
	listing(p, tt, "one", "/var", sftpfs.Entry{Name: "lib", Path: "/var/lib", Directory: true}, sftpfs.Entry{Name: "log", Path: "/var/log", Directory: true}, sftpfs.Entry{Name: "x", Path: "/var/x"})
	has(t, tt, "目录 /var/lib", "syslog", "2 项") // the list still shows /var/log
	click(t, tt, "目录 /var/lib")
	if p.listed[len(p.listed)-1] != "one:/var/lib" {
		t.Fatal("tree click", p.listed)
	}
	click(t, tt, "隐藏目录树")
	if tt.HasText("目录 /var/lib") || p.UIState("sftp-tree") != "true" {
		t.Fatal("tree not hidden", p.UIState("sftp-tree"))
	}
	click(t, tt, "显示目录树")
	has(t, tt, "目录 /var/lib")
}

func TestFilesSortFoldersFirstNaturallyAndByHeader(t *testing.T) {
	_, p, tt := fixture(t)
	tt.SetSize(1280, 1400) // every row in view
	session(t, p, tt, "one", "one", remoteHost("one"))
	click(t, tt, "SFTP")
	listing(p, tt, "one", "/d",
		sftpfs.Entry{Name: "file10", Path: "/d/file10", Size: 1},
		sftpfs.Entry{Name: "zeta", Path: "/d/zeta", Directory: true},
		sftpfs.Entry{Name: "File2", Path: "/d/File2", Size: 9},
		sftpfs.Entry{Name: "alpha", Path: "/d/alpha", Directory: true})
	order := func() string {
		var out []string
		for _, n := range []string{"alpha", "zeta", "File2", "file10"} {
			label := "选择 " + n
			if n == "alpha" || n == "zeta" {
				label = "打开 " + n // the list's row, not the tree's
			}
			r, ok := tt.Find(label)
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
	tt.Advance(time.Second) // not a double click
	click(t, tt, "大小")      // descending
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
	tt.Key(appMod(), ui.KeyK)
	tt.Type("远程编辑器")
	has(t, tt, "没有结果")
}

// A toast shows in the window whose action raised it: editor actions in the editor
// window (even when their result arrives through the main window's events),
// settings actions in the settings window, everything else in the main window.
func TestToastsShowInTheWindowOfTheirAction(t *testing.T) {
	p := &keysPlatform{fakePlatform: newPlatform(t), keys: []model.HostKey{{Address: "db.example:22", Fingerprint: "SHA256:abc"}}}
	a := appui.New(p)
	tt := ui.NewTester(a.View, 1280, 800)
	a.ShowEditor, a.HideEditor = func() {}, func() {}
	ed := ui.NewTester(a.EditorView, 900, 640)
	st := ui.NewTester(a.SettingsView, 880, 1400)
	session(t, p.fakePlatform, tt, "one", "one", remoteHost("one"))
	p.events = append(p.events, appui.Event{Editor: &appui.EditorResult{Session: "one", Document: remoteedit.Document{Path: "/etc/a.conf", Text: "a=1\n"}}})
	tt.Frame()
	ed.Frame()
	// where says which windows show text. The editor window builds first, as a focused
	// one does, and again after the main window polled the events (the app wakes it).
	where := func(text string) (inMain, inEditor, inSettings bool) {
		ed.Frame()
		st.Frame()
		tt.Frame()
		ed.Frame()
		st.Frame()
		return tt.HasText(text), ed.HasText(text), st.HasText(text)
	}
	expect := func(what, text string, main, editor, settings bool) {
		t.Helper()
		if m, e, s := where(text); m != main || e != editor || s != settings {
			t.Fatalf("%s: shown in main=%v editor=%v settings=%v, want %v %v %v", what, m, e, s, main, editor, settings)
		}
	}

	// The editor's save arrives as an event the main window polls.
	p.events = append(p.events, appui.Event{Editor: &appui.EditorResult{Session: "one", Document: remoteedit.Document{Path: "/etc/a.conf", Text: "a=1\n"}, Saved: true}, Message: "已保存远程文件", ForEditor: true})
	expect("save result", "已保存远程文件", false, true, false)
	// Everything else, whichever window builds first.
	p.events = append(p.events, appui.Event{Message: "外部编辑器已关闭"})
	expect("other event", "外部编辑器已关闭", true, false, false)
	// A toast raised while the editor window builds.
	click(t, ed, "外部编辑器参数")
	ed.Type("--wait")
	click(t, ed, "用外部编辑器打开")
	expect("editor action", "编辑器参数须为 JSON 字符串数组，如 [\"--wait\"]", false, true, false)
	ed.Advance(7 * time.Second) // the toast times out
	// An error's details open in the window that showed the error.
	p.events = append(p.events, appui.Event{Message: "dial tcp: connection refused", ForEditor: true})
	expect("editor error", "连接被拒绝：服务器没有在这个端口提供 SSH 服务", false, true, false)
	click(t, ed, "详情")
	expect("details of an editor error", "dial tcp: connection refused", false, true, false)
	click(t, ed, "关闭")
	// A toast raised while the settings window builds.
	click(t, st, "终端")
	click(t, st, "移除 db.example:22")
	click(t, st, "移除密钥")
	expect("settings action", "已移除 db.example:22 的主机密钥", false, false, true)
	// A reload that fails belongs to the editor too.
	p.events = append(p.events, appui.Event{Message: "reload went wrong", ForEditor: true})
	expect("reload failure", "reload went wrong", false, true, false)
	// With the editor window gone its late results fall back to the main window.
	if !a.EditorMayClose() {
		t.Fatal("clean editor refused to close")
	}
	p.events = append(p.events, appui.Event{Message: "迟到的结果", ForEditor: true})
	expect("late editor result", "迟到的结果", true, false, false)
}

func TestEditorReloadAsksThePlatformToReload(t *testing.T) {
	a, p, tt := fixture(t)
	a.ShowEditor = func() {}
	ed := ui.NewTester(a.EditorView, 900, 640)
	session(t, p, tt, "one", "one", remoteHost("one"))
	p.events = append(p.events, appui.Event{Editor: &appui.EditorResult{Session: "one", Document: remoteedit.Document{Path: "/etc/a.conf", Text: "a=1\n"}}})
	tt.Frame()
	ed.Frame()
	click(t, ed, "重新载入")
	click(t, ed, "丢弃并重新载入")
	if len(p.reloaded) != 1 || p.reloaded[0] != "/etc/a.conf" || len(p.opened) != 0 {
		t.Fatal("reload", p.reloaded, p.opened)
	}
}

func TestToolTabsGoWithTheirHost(t *testing.T) {
	_, p, tt := fixture(t)
	session(t, p, tt, "one", "one", remoteHost("one"))
	p.events = append(p.events, appui.Event{Monitor: &appui.MonitorResult{Session: "one", Snapshot: monitorSnapshot(time.Now(), 0)}})
	tt.Frame()
	click(t, tt, "进程管理器")
	has(t, tt, "进程 · one")
	click(t, tt, "关闭标签 one")
	click(t, tt, "断开并关闭")
	if tt.HasText("进程 · one") || p.detailed["one"] {
		t.Fatal("the process tab outlived its host", p.detailed)
	}
}
