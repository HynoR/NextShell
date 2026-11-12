package appui_test

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/model"
	"nextshell.dev/v2/internal/monitor"
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
	web = saveHost(t, p, model.Connection{Name: "prod-web-01", Host: "10.0.0.1", User: "root", FolderID: prod.ID, Favorite: true, Tags: []string{"web"}})
	saveHost(t, p, model.Connection{Name: "prod-web-02", Host: "10.0.0.2", User: "root", FolderID: prod.ID})
	db = saveHost(t, p, model.Connection{Name: "db-master", Host: "10.0.2.10", User: "postgres", FolderID: prod.ID})
	saveHost(t, p, model.Connection{Name: "test-01", Host: "10.1.0.5", User: "dev", FolderID: test.ID})
	saveHost(t, p, model.Connection{Name: "redis-01", Host: "10.0.3.1", User: "root", FolderID: cache.ID})
	saveHost(t, p, model.Connection{Name: "bastion", Host: "jump.example.com", User: "ops"})
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

func monitorSnapshot(at time.Time, i int) monitor.Snapshot {
	return monitor.Snapshot{At: at, Info: "Ubuntu 24.04 LTS · 4 核 · 运行 12 天", Load: "0.42 0.38 0.35", HasDelta: true, HasNetworkDelta: true, CPUPercent: float64(20 + i*3%40), MemoryTotal: 8 << 30, MemoryAvailable: uint64(3+i%2) << 30, SwapTotal: 2 << 30, SwapFree: 2 << 30, RXPerSecond: float64(120000 + i*9000), TXPerSecond: float64(40000 + i*3000),
		Disks:     []monitor.Disk{{Device: "/dev/sda1", Mount: "/", TotalKB: 80 << 20, UsedKB: 41 << 20, UsedPercent: 51}},
		Processes: []monitor.Process{{PID: 812, User: "www", CPU: 12.5, MemoryMB: 140, Command: "nginx: worker process"}, {PID: 1, User: "root", CPU: 0.1, MemoryMB: 12, Command: "/sbin/init"}, {PID: 2201, User: "postgres", CPU: 4.2, MemoryMB: 610, Command: "postgres: checkpointer"}},
		Network:   []monitor.Network{{Protocol: "tcp", State: "LISTEN", LocalIP: "0.0.0.0", LocalPort: 443, PID: 812, Process: "nginx", Connections: 32, UniqueIPs: 9}, {Protocol: "tcp", State: "ESTAB", LocalIP: "10.0.0.1", LocalPort: 22, RemoteIP: "10.0.9.4", RemotePort: 51022, PID: 990, Process: "sshd", Connections: 1, UniqueIPs: 1}}}
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
	shot(tt, "01-welcome")

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
	}}})
	for i := range 12 {
		p.events = append(p.events, appui.Event{Monitor: &appui.MonitorResult{Session: "s1", Snapshot: monitorSnapshot(now.Add(time.Duration(i-12)*2*time.Second), i)}})
	}
	tt.Frame()
	tt.Key(ui.Cmd|ui.Shift, ui.KeyF)
	tt.Frame()
	shot(tt, "02-main-files")
	click(t, tt, "监控")
	shot(tt, "03-main-monitor")
	click(t, tt, "进程")
	shot(tt, "04-main-monitor-processes")
	click(t, tt, "片段")
	shot(tt, "05-main-snippets")

	p.jobs = []transfer.Job{
		{ID: "1", Label: "上传 release.tar.gz → /srv/app", State: "running", Bytes: 48 << 20, Attempt: 1, Started: now.Add(-20 * time.Second)},
		{ID: "2", Label: "下载 /var/log/syslog", State: "done", Bytes: 2200000, Attempt: 1, Started: now.Add(-time.Minute), Finished: now.Add(-50 * time.Second)},
		{ID: "3", Label: "上传 config.yaml → /etc/app", State: "failed", Error: "权限不足", Attempt: 1},
	}
	tt.Frame()
	click(t, tt, "传输列表")
	shot(tt, "06-transfers")
	tt.Key(0, ui.KeyEscape)

	tt.Key(ui.Cmd, ui.KeyK)
	tt.Type("prod")
	shot(tt, "07-palette")
	tt.Key(0, ui.KeyEscape)

	if e := tt.RightClick("prod-web-02"); e != nil {
		t.Fatal(e)
	}
	if e := tt.ChooseMenuItem("编辑…"); e != nil {
		t.Fatal(e)
	}
	shot(tt, "08-host-general")
	click(t, tt, "认证")
	shot(tt, "09-host-auth")
	click(t, tt, "高级")
	shot(tt, "10-host-advanced")
	click(t, tt, "取消")

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

	st := ui.NewTester(a.SettingsView, 880, 640)
	for i, section := range []string{"通用", "终端", "SSH", "密钥", "代理", "同步", "Agent", "导入导出", "回收站", "网络诊断", "关于"} {
		click(t, st, section)
		shot(st, "20-settings-"+string(rune('a'+i))+"-"+section)
	}
}
