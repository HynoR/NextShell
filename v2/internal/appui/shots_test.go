package appui_test

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/agent"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/model"
	"nextshell.dev/v2/internal/monitor"
	"nextshell.dev/v2/internal/remoteedit"
	"nextshell.dev/v2/internal/sftpfs"
	"nextshell.dev/v2/internal/transfer"
)

// demo fills a profile with the hosts, folders and snippets the screenshots show.
func demo(t *testing.T, p *fakePlatform) (web, db model.Connection) {
	t.Helper()
	prod := model.Folder{ID: p.NewID(), Name: "生产"}
	test := model.Folder{ID: p.NewID(), Name: "测试"}
	cache := model.Folder{ID: p.NewID(), Name: "缓存", ParentID: prod.ID}
	for _, f := range []model.Folder{prod, test, cache} {
		if e := p.SaveFolder(f); e != nil {
			t.Fatal(e)
		}
	}
	web = saveHost(t, p, model.Connection{Name: "prod-web-01", Host: "10.0.0.1", User: "root", FolderID: prod.ID, Favorite: true, Tags: []string{"web"}, Color: "#ef4444"})
	saveHost(t, p, model.Connection{Name: "prod-web-02", Host: "10.0.0.2", User: "root", FolderID: prod.ID})
	db = saveHost(t, p, model.Connection{Name: "db-master", Host: "10.0.2.10", User: "postgres", FolderID: prod.ID, Color: "#a855f7"})
	saveHost(t, p, model.Connection{Name: "test-01", Host: "10.1.0.5", User: "dev", FolderID: test.ID})
	saveHost(t, p, model.Connection{Name: "redis-01", Host: "10.0.3.1", User: "root", FolderID: cache.ID})
	ops := model.Credential{ID: p.NewID(), Name: "运维账号", User: "ops", Auth: "password"}
	if e := p.SaveCredential(ops, nil); e != nil {
		t.Fatal(e)
	}
	deploy, e := p.GenerateKey("deploy", "ed25519")
	if e != nil {
		t.Fatal(e)
	}
	saveHost(t, p, model.Connection{Name: "bastion", Host: "jump.example.com", CredentialID: ops.ID})
	saveHost(t, p, model.Connection{Name: "build-01", Host: "10.0.5.3", User: "ci", Auth: "key", KeyID: deploy.ID, FolderID: test.ID})
	saveHost(t, p, model.Connection{Name: "legacy-box", Host: "10.8.0.2", User: "root", Auth: "key", NeedsKeyRebind: true, OriginalAuth: "key", SourceKeyRef: &model.KeyReference{Name: "old-ops"}})
	saveHost(t, p, model.Connection{Name: "old-jump", Host: "10.9.0.1", User: "ops", Deleted: true})
	saveHost(t, p, model.Connection{Name: "staging-02", Host: "10.1.0.9", User: "dev", Deleted: true})
	for _, c := range []model.Command{
		{Name: "重启 nginx", Group: "Web", Text: "sudo systemctl restart nginx"},
		{Name: "查看日志", Group: "Web", Text: "tail -f /var/log/nginx/error.log"},
		{Name: "磁盘占用", Group: "系统", Text: "df -h"},
		{Name: "查找进程", Group: "系统", Text: "ps aux | grep [#name]"},
	} {
		c.ID = p.NewID()
		if e := p.SaveCommand(c); e != nil {
			t.Fatal(e)
		}
	}
	return web, db
}

// titled shows the tab strip with SSH tabs to similar hosts whose shells all set the same kind of title,
// and a local terminal running a program: SSH tabs read as the connections, each with its host's color,
// the program title is on the session card, and the local tab follows its program.
func titled(t *testing.T, shot func(*ui.Tester, string)) {
	t.Helper()
	p := newPlatform(t)
	web, db := demo(t, p)
	a, tt := start(p)
	for i, c := range []model.Connection{web, saveHost(t, p, model.Connection{Name: "prod-web-02", Host: "10.0.0.2", User: "root"}), db} {
		s := session(t, p, tt, "t"+string(rune('1'+i)), c.Name, c)
		s.Terminal.Feed([]byte("\x1b]0;" + c.User + "@" + c.Name + ": ~\x07\r\n" + c.User + "@" + c.Name + ":~# "))
	}
	local := session(t, p, tt, "tl", "本地终端", model.Connection{})
	local.Terminal.Feed([]byte("\x1b]0;vim notes.txt\x07"))
	a.Active = 1
	tt.Frame()
	shot(tt, "02d-tab-titles")
}

func monitorSnapshot(at time.Time, i int) monitor.Snapshot {
	return monitor.Snapshot{At: at, Info: "Ubuntu 24.04 LTS · 4 核 · 运行 12 天", Load: "0.42 0.38 0.35", HasDelta: true, HasNetworkDelta: true, CPUPercent: float64(20 + i*3%40), MemoryTotal: 8 << 30, MemoryAvailable: uint64(3+i%2) << 30, SwapTotal: 2 << 30, SwapFree: 1843 << 20, RXPerSecond: float64(120000 + i*9000), TXPerSecond: float64(40000 + i*3000),
		Interfaces: map[string]monitor.InterfaceRate{"eth0": {RXPerSecond: float64(120000 + i*9000), TXPerSecond: float64(40000 + i*3000), HasDelta: true}, "docker0": {RXPerSecond: 1500, TXPerSecond: 800, HasDelta: true}},
		Disks:      []monitor.Disk{{Device: "/dev/sda1", Mount: "/", TotalKB: 80 << 20, UsedKB: 59 << 20, UsedPercent: 74}},
		Detail:     true,
		Processes:  []monitor.Process{{PID: 812, User: "www", CPU: 12.5, MemoryMB: 140, Command: "nginx: worker process"}, {PID: 1, User: "root", CPU: 0.1, MemoryMB: 12, Command: "/sbin/init"}, {PID: 2201, User: "postgres", CPU: 4.2, MemoryMB: 610, Command: "postgres: checkpointer", Elapsed: 7260, PPID: 2190, State: "Ss", MemoryPercent: 3.8}},
		Network:    []monitor.Network{{Protocol: "tcp", State: "LISTEN", LocalIP: "0.0.0.0", LocalPort: 443, PID: 812, Process: "nginx", Connections: 32, UniqueIPs: 9}, {Protocol: "tcp", State: "ESTAB", LocalIP: "10.0.0.1", LocalPort: 22, RemoteIP: "10.0.9.4", RemotePort: 51022, PID: 990, Process: "sshd", Connections: 1, UniqueIPs: 1}}}
}

// TestScreenshots renders every main screen, light and dark, into $NEXTSHELL_SHOTS.
func TestScreenshots(t *testing.T) {
	dir := os.Getenv("NEXTSHELL_SHOTS")
	if dir == "" {
		t.Skip("set NEXTSHELL_SHOTS=<dir> to render screenshots")
	}
	if e := os.MkdirAll(dir, 0o755); e != nil {
		t.Fatal(e)
	}
	shot := func(tt *ui.Tester, name string) {
		t.Helper()
		for _, dark := range []bool{false, true} {
			tt.SetDark(dark)
			tt.Frame()
			mode := "light"
			if dark {
				mode = "dark"
			}
			f, e := os.Create(filepath.Join(dir, name+"-"+mode+".png"))
			if e != nil {
				t.Fatal(e)
			}
			if e = png.Encode(f, tt.Image()); e != nil {
				t.Fatal(e)
			}
			f.Close()
		}
		tt.SetDark(false)
	}

	p := newPlatform(t)
	web, db := demo(t, p)
	a, tt := start(p)
	tt.SetTitleBar(ui.TitleBar{Height: 44, Left: 72})
	shot(tt, "01-welcome")
	_, _, empty := fixture(t) // a profile with no hosts: the first run
	shot(empty, "01b-welcome-first-run")

	one := session(t, p, tt, "s1", "prod-web-01", web)
	session(t, p, tt, "s2", "db-master", db)
	a.Active = 0
	one.Terminal.Feed([]byte("\r\nroot@prod-web-01:~# uptime\r\n 10:42:01 up 12 days,  3:02,  1 user,  load average: 0.42, 0.38, 0.35\r\nroot@prod-web-01:~# "))
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	now := time.Now()
	p.events = append(p.events, appui.Event{Files: &appui.FileListing{Session: "s1", Path: "/var/log", Entries: []sftpfs.Entry{
		{Name: "nginx", Path: "/var/log/nginx", Directory: true, Mode: "drwxr-xr-x", Modified: now.Add(-26 * time.Hour)},
		{Name: "apt", Path: "/var/log/apt", Directory: true, Mode: "drwxr-xr-x", Modified: now.Add(-50 * time.Hour)},
		{Name: "syslog", Path: "/var/log/syslog", Size: 2200000, Mode: "-rw-r-----", Modified: now.Add(-time.Hour)},
		{Name: "auth.log", Path: "/var/log/auth.log", Size: 81000, Mode: "-rw-r-----", Modified: now.Add(-2 * time.Hour)},
		{Name: "kern.log", Path: "/var/log/kern.log", Size: 4096, Mode: "-rw-r-----", Modified: now.AddDate(0, -2, 0)},
		{Name: "dpkg.log", Path: "/var/log/dpkg.log", Size: 12400, Mode: "-rw-r--r--", Modified: now.AddDate(0, 0, -3)},
	}}})
	for i := range 12 {
		p.events = append(p.events, appui.Event{Monitor: &appui.MonitorResult{Session: "s1", Snapshot: monitorSnapshot(now.Add(time.Duration(i-12)*2*time.Second), i), Latency: 18 * time.Millisecond}})
	}
	tt.Frame()
	click(t, tt, "SFTP")
	tt.Frame()
	// The connected workspace, as the 1.x reference: session card, live status, terminal, SFTP.
	shot(tt, "02-main-connected")
	tt.SetSize(1440, 900)
	shot(tt, "02-main-connected-1440")
	tt.SetSize(1000, 650)
	shot(tt, "02-main-connected-small")
	tt.SetSize(1280, 800)
	// The same window in two other main colors.
	for _, accent := range []string{"violet", "orange"} {
		a.Prefs.Accent = accent
		shot(tt, "02h-main-accent-"+accent)
	}
	a.Prefs.Accent = ""
	titled(t, shot)
	workspaceTools(t, shot)
	p.events = append(p.events, appui.Event{Files: &appui.FileListing{Session: "s1", Path: "/var/log/nginx", Entries: []sftpfs.Entry{
		{Name: "archive", Path: "/var/log/nginx/archive", Directory: true, Mode: "drwxr-xr-x", Modified: now.Add(-26 * time.Hour)},
		{Name: "access.log.10", Path: "/var/log/nginx/access.log.10", Size: 900000, Mode: "-rw-r-----", Modified: now.Add(-240 * time.Hour)},
		{Name: "access.log.2", Path: "/var/log/nginx/access.log.2", Size: 2200000, Mode: "-rw-r-----", Modified: now.Add(-48 * time.Hour)},
		{Name: "access.log", Path: "/var/log/nginx/access.log", Size: 5100000, Mode: "-rw-r-----", Modified: now.Add(-time.Minute)},
	}}})
	tt.Frame()
	shot(tt, "02b-files-breadcrumbs-natural-order")
	// Selected rows, where the accent fill carries the text.
	click(t, tt, "选择 access.log.2")
	if e := tt.ClickWith(ui.Cmd, "选择 access.log"); e != nil {
		t.Fatal(e)
	}
	shot(tt, "02c-selected-rows")
	tt.Key(0, ui.KeyEscape)
	tt.Key(ui.Cmd|ui.Shift, ui.KeyM)
	shot(tt, "03-sysinfo-history")
	// The process manager is a tab of its own, beside the host's terminal; the network manager too.
	click(t, tt, "进程管理器")
	shot(tt, "04-process-manager-tab")
	click(t, tt, "网络管理器")
	click(t, tt, "tcp 已建立")
	shot(tt, "04a-network-manager-tab")
	click(t, tt, "进程 · prod-web-01")
	dblclick(t, tt, "postgres: checkpointer")
	shot(tt, "04b-process-drawer")
	click(t, tt, "关闭详情")
	click(t, tt, "关闭标签 进程 · prod-web-01")
	click(t, tt, "关闭标签 网络 · prod-web-01")
	// Another host that has not sent anything for minutes: its last data, labeled, until a fresh sample.
	for i := range 12 {
		p.events = append(p.events, appui.Event{Monitor: &appui.MonitorResult{Session: "s2", Snapshot: monitorSnapshot(now.Add(-6*time.Minute+time.Duration(i)*2*time.Second), i+5), Latency: 42 * time.Millisecond}})
	}
	tt.Frame()
	a.Active = 1
	tt.Frame()
	shot(tt, "02e-monitor-cached")
	a.Active = 0
	tt.Frame()
	// The route trace opens from the PING card.
	ping := &servicesPlatform{fakePlatform: p}
	pa := appui.New(ping)
	ptt := ui.NewTester(pa.View, 1280, 800)
	ptt.SetTitleBar(ui.TitleBar{Height: 44, Left: 72})
	session(t, p, ptt, "s1", "prod-web-01", web)
	for i := range 12 {
		p.events = append(p.events, appui.Event{Monitor: &appui.MonitorResult{Session: "s1", Snapshot: monitorSnapshot(time.Now().Add(time.Duration(i-12)*2*time.Second), i), Latency: 18 * time.Millisecond}})
	}
	ptt.Frame()
	shot(ptt, "02f-ping-card")
	click(t, ptt, "PING 延迟 18 ms，点击路由追踪")
	p.events = append(p.events, appui.Event{Diagnostic: &appui.DiagnosticResult{ID: "current", Kind: "traceroute", Text: "traceroute to 10.0.0.1, 30 hops max\n 1  192.168.1.1     1.21 ms\n 2  10.255.0.1      4.80 ms\n 3  203.0.113.9    12.40 ms\n 4  10.0.0.1       18.10 ms"}})
	ptt.Frame()
	shot(ptt, "02g-route-trace-drawer")
	// A server the probe cannot read (not Linux): after three failed samples it is left alone, said in the left
	// column and in the 系统信息 tab, with the way to turn its monitoring off.
	_, bp, btt := fixture(t)
	btt.SetTitleBar(ui.TitleBar{Height: 44, Left: 72})
	router := saveHost(t, bp, model.Connection{Name: "edge-router", Host: "10.0.0.254", User: "admin"})
	session(t, bp, btt, "s9", "edge-router", router)
	for range 3 {
		bp.events = append(bp.events, appui.Event{Monitor: &appui.MonitorResult{Session: "s9", Err: fmt.Errorf("%w: Process exited with status 44", monitor.ErrUnsupported)}})
	}
	btt.Key(ui.Cmd|ui.Shift, ui.KeyM)
	shot(btt, "02h-monitor-unreadable")
	tt.Key(ui.Cmd|ui.Shift, ui.KeyS)
	shot(tt, "05-snippets")
	click(t, tt, "SFTP")

	p.jobs = []transfer.Job{
		{ID: "1", Label: "上传 release.tar.gz → /srv/app", State: "running", Bytes: 48 << 20, Total: 120 << 20, Attempt: 1, Started: now.Add(-20 * time.Second)},
		{ID: "2", Label: "下载 /var/log/syslog", State: "done", Bytes: 2200000, Attempt: 1, Started: now.Add(-time.Minute), Finished: now.Add(-50 * time.Second)},
		{ID: "3", Label: "上传 config.yaml → /etc/app", State: "failed", Error: "sftp: \"permission denied\" (SSH_FX_PERMISSION_DENIED)", Attempt: 1},
		{ID: "4", Label: "下载 /var/log/journal → ~/Downloads", State: "running", Bytes: 9 << 20, Attempt: 1, Started: now.Add(-5 * time.Second)},
	}
	tt.Frame()
	shot(tt, "06a-activity-strip")
	click(t, tt, "2 个传输进行中")
	shot(tt, "06-activity-transfers")
	click(t, tt, "收起活动栏")

	tt.Key(appMod(), ui.KeyK)
	tt.Type("prod")
	shot(tt, "07-palette")
	tt.Key(0, ui.KeyEscape)

	// Folded: the left column keeps the status dot, the workbench keeps its tabs.
	tt.Key(appMod(), ui.KeyB)
	tt.Key(appMod(), ui.KeyJ)
	shot(tt, "07b-folded")
	tt.Key(appMod(), ui.KeyB)
	tt.Key(appMod(), ui.KeyJ)

	// The connection manager: folders, host tiles (legacy-box lacks its key), the card, several hosts
	// chosen, the batch binding with its preview, the host form, and the keys, proxies and credentials.
	mt := ui.NewTester(a.ManagerView, 1200, 760)
	shot(mt, "16-manager")
	click(t, mt, "prod-web-01")
	shot(mt, "16b-manager-host")
	if e := mt.ClickWith(ui.Cmd, "db-master"); e != nil {
		t.Fatal(e)
	}
	shot(mt, "16c-manager-multiselect")
	click(t, mt, "绑定认证…")
	choose(t, mt, "使用凭据", "运维账号")
	shot(mt, "16d-manager-batch-preview")
	click(t, mt, "取消")
	click(t, mt, "取消选择")
	if e := mt.RightClick("prod-web-02"); e != nil {
		t.Fatal(e)
	}
	if e := mt.ChooseMenuItem("编辑…"); e != nil {
		t.Fatal(e)
	}
	shot(mt, "08-host-editor")
	click(t, mt, "标签与备注")
	mt.Advance(time.Second)
	click(t, mt, "蓝")
	shot(mt, "08b-host-editor-color")
	click(t, mt, "标签与备注")
	mt.Advance(time.Second)
	choose(t, mt, "认证方式", "密钥")
	click(t, mt, "网络")
	click(t, mt, "终端")
	mt.Advance(time.Second)
	shot(mt, "09-host-editor-key-sections")
	choose(t, mt, "使用凭据", "运维账号")
	shot(mt, "10-host-editor-credential")
	click(t, mt, "取消")
	click(t, mt, "密钥")
	click(t, mt, "deploy")
	shot(mt, "18-manager-keys")
	click(t, mt, "凭据")
	click(t, mt, "运维账号")
	shot(mt, "19-manager-credentials")
	click(t, mt, "新建凭据…")
	shot(mt, "19b-credential-dialog")
	click(t, mt, "取消")
	click(t, mt, "连接")

	answers := make(chan []string, 1)
	done := make(chan struct{})
	defer close(done)
	p.events = append(p.events, appui.Event{Prompt: &appui.Prompt{Kind: appui.PromptHostKey, Title: "首次连接，请核对主机指纹", Host: "10.0.0.1:22", Fingerprint: "SHA256:nThbg6kXUpJWGl7E1IGOCspRomTxdCARLviKw6E5SY8", Answers: answers, Done: done}})
	tt.Frame()
	shot(tt, "11-host-key")
	click(t, tt, "信任并连接")
	p.events = append(p.events, appui.Event{Prompt: &appui.Prompt{Kind: appui.PromptHostKeyChanged, Title: "主机指纹已变化", Host: "10.0.0.1:22", OldFingerprint: "SHA256:nThbg6kXUpJWGl7E1IGOCspRomTxdCARLviKw6E5SY8", Fingerprint: "SHA256:Q2V4Zm9vYmFyYmF6cXV4MTIzNDU2Nzg5MGFiY2RlZg", Answers: make(chan []string, 1), Done: done}})
	tt.Frame()
	shot(tt, "12-host-key-changed")
	click(t, tt, "断开")
	p.events = append(p.events, appui.Event{Prompt: &appui.Prompt{Kind: appui.PromptPassword, Title: "输入密码", Host: "root@10.0.0.1:22", Questions: []string{"密码"}, CanRemember: true, Answers: make(chan []string, 1), Done: done}})
	tt.Frame()
	shot(tt, "13-password")
	click(t, tt, "取消")
	// The same dialogs when an agent's session_open raised them.
	p.events = append(p.events, appui.Event{Prompt: &appui.Prompt{Kind: appui.PromptHostKey, ByAgent: true, Title: "首次连接，请核对主机指纹", Host: "10.0.0.1:22", Fingerprint: "SHA256:nThbg6kXUpJWGl7E1IGOCspRomTxdCARLviKw6E5SY8", Answers: make(chan []string, 1), Done: done}})
	tt.Frame()
	shot(tt, "11b-host-key-by-agent")
	click(t, tt, "取消")
	p.events = append(p.events, appui.Event{Prompt: &appui.Prompt{Kind: appui.PromptPassword, ByAgent: true, Title: "输入密码", Host: "root@10.0.0.1:22", Questions: []string{"密码"}, CanRemember: true, Answers: make(chan []string, 1), Done: done}})
	tt.Frame()
	shot(tt, "13b-password-by-agent")
	click(t, tt, "取消")
	// A refused login offers another user, method or key, saved to the host.
	p.events = append(p.events, appui.Event{Failed: db.ID, AuthFailed: true, Message: "ssh: handshake failed: ssh: unable to authenticate, attempted methods [none password]"})
	tt.Frame()
	shot(tt, "13c-auth-retry")
	click(t, tt, "取消")

	shown := 0
	a.ShowEditor = func() { shown++ }
	p.events = append(p.events, appui.Event{Editor: &appui.EditorResult{Session: "s1", Document: remoteedit.Document{Path: "/etc/nginx/nginx.conf", Text: "user www-data;\nworker_processes auto;\n\nevents {\n    worker_connections 768;\n}\n"}}})
	p.events = append(p.events, appui.Event{Editor: &appui.EditorResult{Session: "s1", Document: remoteedit.Document{Path: "/etc/hosts", Text: "127.0.0.1 localhost\n"}}})
	tt.Frame()
	ed := ui.NewTester(a.EditorView, 960, 640)
	ed.Type("# edited\n")
	shot(ed, "14-editor-window")
	if a.EditorMayClose() {
		t.Fatal("dirty editor closed")
	}
	shot(ed, "14b-editor-close-guard")

	// A dropped connection: the card and the tab offer to reconnect.
	one.Terminal.Close()
	<-one.Terminal.Done()
	tt.Frame()
	shot(tt, "17-disconnected")

	sp := &servicesPlatform{fakePlatform: p, auto: agent.AutoApproval{On: true, Expires: now.Add(42 * time.Minute)}}
	sp.agent = appui.AgentOverview{Pending: []agent.Pending{{ID: "r", Client: "claude", Tool: "exec", Summary: "systemctl restart nginx", ExpiresAt: now.Add(time.Minute)}}}
	sa := appui.New(sp)
	stt := ui.NewTester(sa.View, 1280, 800)
	stt.SetTitleBar(ui.TitleBar{Height: 44, Left: 72})
	session(t, p, stt, "s9", "prod-web-01", web)
	shot(stt, "15-auto-approve-status")
	click(t, stt, "Agent 待批准 1")
	shot(stt, "15b-agent-activity")
	// The activity bar: badges when folded, the agent's feed with a request to approve, and the live edits.
	ap, att := newActivity(t)
	att.SetTitleBar(ui.TitleBar{Height: 44, Left: 72})
	shot(att, "24a-activity-strip-badges")
	click(t, att, "Agent 待批准 1")
	shot(att, "24b-activity-agent-feed")
	ap.agent = appui.AgentOverview{URL: ap.agent.URL}
	shot(att, "24c-activity-live-edits")
	sst := ui.NewTester(sa.SettingsView, 880, 900)
	click(t, sst, "Agent")
	shot(sst, "21-settings-agent-auto-approve")
	kp := &keysPlatform{fakePlatform: p, keys: []model.HostKey{{Address: "10.0.0.1:22", Fingerprint: "SHA256:nThbg6kXUpJWGl7E1IGOCspRomTxdCARLviKw6E5SY8"}, {Address: "jump.example.com:22", Fingerprint: "SHA256:Q2V4Zm9vYmFyYmF6cXV4MTIzNDU2Nzg5MGFiY2RlZg"}}}
	kst := ui.NewTester(appui.New(kp).SettingsView, 880, 1500)
	click(t, kst, "终端")
	shot(kst, "22-settings-terminal-ssh-groups")

	a.Version, a.ProfilePath = "2.0.0-rc1", "/Users/me/Library/Application Support/NextShell-v2/nextshell.db"
	st := ui.NewTester(a.SettingsView, 880, 640)
	for i, section := range []string{"窗口行为", "文件传输", "远端编辑", "终端", "网络工具", "回收站", "Agent", "关于"} {
		click(t, st, section)
		shot(st, "20-settings-"+string(rune('a'+i))+"-"+section)
	}

	// Shell 集成 below the fold of the settings window: 手动 by default, then 自动 with its plain description.
	tst := ui.NewTester(appui.New(p).SettingsView, 880, 1000)
	click(t, tst, "终端")
	shot(tst, "23-settings-terminal-shell-manual")
	click(t, tst, "自动")
	shot(tst, "23b-settings-terminal-shell-auto")
}

// workspaceTools shows the SFTP tree with a folder opened beside the one listed, the command bar
// recalling history, the find bar over the scrollback, and a 连接中 tab and page with their cancel.
func workspaceTools(t *testing.T, shot func(*ui.Tester, string)) {
	t.Helper()
	p := newPlatform(t)
	web, _ := demo(t, p)
	if e := p.SaveHistory([]string{"systemctl status nginx", "tail -n 50 /var/log/nginx/error.log", "df -h"}); e != nil {
		t.Fatal(e)
	}
	_, tt := start(p)
	tt.SetTitleBar(ui.TitleBar{Height: 44, Left: 72})
	s := session(t, p, tt, "s1", "prod-web-01", web)
	for i := range 40 {
		s.Terminal.Feed([]byte(fmt.Sprintf("\r\n[%02d] GET /api/health 200", i)))
		if i == 12 || i == 31 {
			s.Terminal.Feed([]byte("\r\n[error] upstream timed out while reading response header"))
		}
	}
	s.Terminal.Feed([]byte("\r\nroot@prod-web-01:~# "))
	click(t, tt, "SFTP")
	dir := func(name, at string) sftpfs.Entry {
		return sftpfs.Entry{Name: name, Path: at + "/" + name, Directory: true, Mode: "drwxr-xr-x"}
	}
	p.events = append(p.events,
		appui.Event{Files: &appui.FileListing{Session: "s1", Path: "/var/log", Entries: []sftpfs.Entry{dir("apt", "/var/log"), dir("nginx", "/var/log"), {Name: "syslog", Path: "/var/log/syslog", Size: 2200000, Mode: "-rw-r-----"}, {Name: "auth.log", Path: "/var/log/auth.log", Size: 81000, Mode: "-rw-r-----"}}}})
	tt.Frame() // the tree asks for the folders above
	p.events = append(p.events,
		appui.Event{Files: &appui.FileListing{Session: "s1", Path: "/", Entries: []sftpfs.Entry{dir("etc", ""), dir("home", ""), dir("opt", ""), dir("var", "")}}},
		appui.Event{Files: &appui.FileListing{Session: "s1", Path: "/var", Entries: []sftpfs.Entry{dir("cache", "/var"), dir("lib", "/var"), dir("log", "/var"), dir("www", "/var")}}})
	tt.Frame()
	tt.Frame()
	tt.SetSize(1280, 900)
	shot(tt, "24-sftp-tree")
	tt.SetSize(1280, 800)
	click(t, tt, "命令输入")
	tt.Key(0, ui.KeyUp)
	tt.Key(0, ui.KeyUp)
	shot(tt, "25-command-bar-history")
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyDown)
	tt.Key(ui.Cmd|ui.Shift, ui.KeyF)
	tt.Type("error")
	for range 5 {
		tt.Frame()
	}
	shot(tt, "26-find-bar")
	tt.Key(0, ui.KeyEscape)
	// A host connecting for the first time: its tab closes to cancel; alone, the page offers it too.
	connect := func(tt *ui.Tester) {
		click(t, tt, "快速连接服务器")
		tt.Type("db-master")
		tt.Key(0, ui.KeyEnter)
	}
	connect(tt)
	shot(tt, "27-connecting-tab-cancel")
	_, ctt := start(p)
	ctt.SetTitleBar(ui.TitleBar{Height: 44, Left: 72})
	connect(ctt)
	shot(ctt, "27b-connecting-page-cancel")
}
