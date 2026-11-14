package appui

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/model"
	"nextshell.dev/v2/internal/monitor"
	"nextshell.dev/v2/internal/transfer"
)

func TestMonitorFilteringAndSorting(t *testing.T) {
	vs := []monitor.Process{{PID: 2, CPU: 10, MemoryMB: 3, Command: "nginx", User: "root"}, {PID: 1, CPU: 50, MemoryMB: 1, Command: "worker", User: "app"}}
	for _, v := range []struct {
		column string
		desc   bool
		want   int
	}{{"CPU", true, 1}, {"PID", true, 2}, {"内存", false, 1}, {"命令", false, 2}} {
		if got := filteredProcesses(vs, "", ui.SortOrder{Column: v.column, Descending: v.desc}); got[0].PID != v.want {
			t.Fatal(v, got)
		}
	}
	if got := filteredProcesses(vs, "ROOT", ui.SortOrder{}); len(got) != 1 || got[0].PID != 2 {
		t.Fatal(got)
	}
	if len(filteredProcesses(vs, "absent", ui.SortOrder{})) != 0 || vs[0].PID != 2 {
		t.Fatal("filter ignored or source mutated")
	}
}

type sendRecorder struct {
	Platform
	sent []string
}

func (p *sendRecorder) ExecuteCommand(session, text string, appendCR ...bool) error {
	p.sent = append(p.sent, text)
	return nil
}

func TestConnectionStatesInWords(t *testing.T) {
	for in, want := range map[string]string{"LISTEN": "监听", "UNCONN": "监听", "ESTAB": "已建立", "ESTABLISHED": "已建立", "TIME-WAIT": "等待回收", "TIME_WAIT": "等待回收", "CLOSE_WAIT": "等待关闭", "FIN_WAIT2": "正在关闭", "SYN-SENT": "正在连接", "weird": "weird"} {
		if got := connState(in); got != want {
			t.Errorf("%s: %s, want %s", in, got, want)
		}
	}
}

func TestSnippetParametersPreservedPerSnippet(t *testing.T) {
	p := &sendRecorder{}
	a := &App{P: p, Tabs: []*Session{{ID: "s"}}, Active: 0}
	one := model.Command{ID: "one", Text: "echo [#host] [#host] [#port]"}
	a.runSnippet(one, false)
	s := &a.snippets
	if !s.paramsOpen || len(s.params) != 2 {
		t.Fatal(s.params)
	}
	s.params[0].Value, s.params[1].Value = "localhost", "22"
	if got := s.render(); got != "echo localhost localhost 22" {
		t.Fatal(got)
	}
	a.runSnippet(model.Command{ID: "one", Text: "[#host]"}, false)
	if s.params[0].Value != "localhost" {
		t.Fatal("lost typing")
	}
	a.runSnippet(model.Command{ID: "two", Text: "[#host]"}, false)
	if s.params[0].Value != "" {
		t.Fatal("parameter leaked between snippets")
	}
	s.paramsOpen = false
	a.runSnippet(model.Command{ID: "plain", Text: "uptime"}, false)
	if s.paramsOpen || len(p.sent) != 1 || p.sent[0] != "uptime" {
		t.Fatal("plain snippet not sent", p.sent)
	}
}

func TestSnippetSearchAndMeterBounds(t *testing.T) {
	v := model.Command{Name: "deploy", Group: "prod", Description: "release", Text: "echo ok"}
	if !commandMatches(v, "RELEASE", "prod") || commandMatches(v, "deploy", "test") || commandMatches(v, "missing", "全部") {
		t.Fatal("search mismatch")
	}
	for _, v := range []struct {
		total, free uint64
		ratio       float64
	}{{0, 0, 0}, {10, 12, 0}, {100, 25, .75}} {
		if got := memoryRatio(v.total, v.free); got != v.ratio {
			t.Fatal(got)
		}
	}
	if !fuzzy("prod-web-01", "pw01") || fuzzy("prod", "dp") || !fuzzy("重启 nginx", "重启 ng") {
		t.Fatal("fuzzy")
	}
}

func TestFolderSubtreeAndSafeQuotedPaths(t *testing.T) {
	a := App{Data: Snapshot{Folders: []model.Folder{{ID: "child", ParentID: "root"}, {ID: "root"}}, Connections: []model.Connection{{ID: "a", FolderID: "root"}, {ID: "b", FolderID: "child"}, {ID: "deleted", FolderID: "root", Deleted: true}, {ID: "c", FolderID: "other"}}}}
	hosts := a.folderHosts("root")
	if len(hosts) != 2 || hosts[0].ID != "a" || hosts[1].ID != "b" || a.folderCount("child") != 1 {
		t.Fatal(hosts)
	}
	if got := shellQuote("/a'b; echo nope"); got != "'/a'\\''b; echo nope'" {
		t.Fatal(got)
	}
	for _, p := range []string{"/a\rcommand", "/a\ncommand", "/a\x03command", "/a\x1bcommand"} {
		if safeTerminalPath(p) == nil {
			t.Fatal("unsafe path accepted")
		}
	}
	if safeTerminalPath("/中文/a'b") != nil {
		t.Fatal("normal path rejected")
	}
}

func TestHostDraftKeepaliveModes(t *testing.T) {
	base := model.Connection{Name: "n", Host: "h", Port: 22, User: "u", Auth: "password"}
	// An imported host inheriting the global switch with its own interval keeps both while untouched.
	interval := 45
	imported := base
	imported.KeepaliveIntervalSec = &interval
	a := &App{}
	a.editHost(imported)
	c, e := a.hosts.editor.draft()
	if e != nil || a.hosts.editor.keepaliveMode != keepaliveGlobal || c.KeepaliveEnabled != nil || c.KeepaliveIntervalSec == nil || c.EffectiveKeepalive() != 45 {
		t.Fatal(c, e)
	}
	a.hosts.editor.keepaliveMode = keepaliveOff
	if c, e = a.hosts.editor.draft(); e != nil || c.KeepaliveEnabled == nil || *c.KeepaliveEnabled || c.EffectiveKeepalive() != 0 {
		t.Fatal(c, e)
	}
	a.hosts.editor.keepaliveMode, a.hosts.editor.keepalive = keepaliveCustom, "20"
	if c, e = a.hosts.editor.draft(); e != nil || c.EffectiveKeepalive() != 20 {
		t.Fatal(c, e)
	}
	a.editHost(c)
	if a.hosts.editor.keepaliveMode != keepaliveCustom || a.hosts.editor.keepalive != "20" {
		t.Fatal(a.hosts.editor)
	}
	a.hosts.editor.keepaliveMode, a.hosts.editor.inheritOrig = keepaliveGlobal, false
	if c, e = a.hosts.editor.draft(); e != nil || c.KeepaliveEnabled != nil || c.KeepaliveIntervalSec != nil || c.Keepalive != 0 {
		t.Fatal(c, e)
	}
	a.hosts.editor.port = "x"
	if _, e = a.hosts.editor.draft(); e == nil {
		t.Fatal("bad port accepted")
	}
}

func TestHumanizeInteropErrorsAndNaturalOrder(t *testing.T) {
	for msg, want := range map[string]string{
		"ssh: handshake failed: EOF": "服务器不支持此认证方式",
		"ssh: handshake failed: ssh: unexpected message type 51 (expected 60)": "服务器不支持此认证方式",
		"wait: remote command exited without exit status or exit signal":       "连接已断开",
		"dial tcp 10.0.0.1:22: connect: connection refused":                    "连接被拒绝：服务器没有在这个端口提供 SSH 服务",
		"resource is in use": "仍在使用中，无法删除：目录需先清空，密钥 / 代理需先解除主机或凭据的引用",
	} {
		if got, ok := humanize(msg); !ok || got != want {
			t.Errorf("%q → %q", msg, got)
		}
	}
	if naturalCompare("file2", "File10") >= 0 || naturalCompare("a01", "a1") != 0 && naturalCompare("a01", "a2") >= 0 || naturalCompare("b", "a") <= 0 {
		t.Fatal("natural order")
	}
}

func TestTransferDetailLeavesOutASizeNothingMovedFor(t *testing.T) {
	for _, v := range []struct {
		job   transfer.Job
		state string
		want  string
	}{
		{transfer.Job{State: "queued", Attempt: 1}, "等待", "等待"},
		{transfer.Job{State: "failed", Attempt: 1}, "失败", "失败"},
		{transfer.Job{State: "cancelled", Attempt: 2}, "已取消", "已取消 · 第 2 次"},
		{transfer.Job{State: "done", Bytes: 2200000, Attempt: 1}, "完成", "完成 · 2.1M"},
		{transfer.Job{State: "failed", Bytes: 5 << 20, Total: 20 << 20, Attempt: 1}, "失败", "失败 · 25% · 5.0M / 20.0M"},
	} {
		if got := transferDetail(v.job, v.state); got != v.want {
			t.Errorf("%+v: %q, want %q", v.job, got, v.want)
		}
	}
}
