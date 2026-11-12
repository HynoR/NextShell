package appui_test

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/model"
	"nextshell.dev/v2/internal/sftpfs"
	"nextshell.dev/v2/internal/transfer"
)

// TestScreenshotsUB renders the screens of the server-naming confirms, the transfer list with hosts and a link row.
func TestScreenshotsUB(t *testing.T) {
	shot := shooter(t)
	now := time.Now()

	// Send to all: the tabs it goes to, by title, and the disconnected one left out.
	p := newPlatform(t)
	web, db := demo(t, p)
	_, tt := start(p)
	tt.SetTitleBar(ui.TitleBar{Height: 44, Left: 72})
	tt.SetSize(1280, 900)
	gone := session(t, p, tt, "s4", "legacy-box", model.Connection{Name: "legacy-box", Host: "10.8.0.2", User: "root", Port: 22})
	gone.Terminal.Close()
	<-gone.Terminal.Done()
	session(t, p, tt, "s1", "prod-web-01", web)
	session(t, p, tt, "s2", "db-master", db)
	session(t, p, tt, "s3", "本地终端", model.Connection{})
	click(t, tt, "prod-web-01")
	tt.Key(ui.Cmd|ui.Shift, ui.KeyS)
	tt.RightClick("片段 重启 nginx")
	if e := tt.ChooseMenuItem("发送到全部会话…"); e != nil {
		t.Fatal(e)
	}
	shot(tt, "ub-1-send-to-all-confirm")

	// Delete: the confirm names the server the files are on.
	rp := &removalPlatform{fakePlatform: newPlatform(t)}
	rweb, rdb := demo(t, rp.fakePlatform)
	ra := appui.New(rp)
	rt := ui.NewTester(ra.View, 1280, 800)
	rt.SetTitleBar(ui.TitleBar{Height: 44, Left: 72})
	session(t, rp.fakePlatform, rt, "s1", "prod-web-01", rweb)
	session(t, rp.fakePlatform, rt, "s2", "db-master", rdb)
	click(t, rt, "SFTP")
	listing(rp.fakePlatform, rt, "s2", "/var/lib/postgresql/backups",
		sftpfs.Entry{Name: "2026-11-01", Path: "/var/lib/postgresql/backups/2026-11-01", Directory: true, Mode: "drwxr-x---", Modified: now.Add(-11 * 24 * time.Hour)},
		sftpfs.Entry{Name: "base.tar.gz", Path: "/var/lib/postgresql/backups/base.tar.gz", Size: 3 << 30, Mode: "-rw-r-----", Modified: now.Add(-26 * time.Hour)})
	deleteRow(t, rt, "打开 2026-11-01")
	rp.events = append(rp.events, appui.Event{Removal: &appui.RemovalCount{Session: "s2", Targets: []appui.RemovalTally{{Path: "/var/lib/postgresql/backups/2026-11-01", Files: 640, Dirs: 12}}}})
	rt.Frame()
	shot(rt, "ub-2-delete-confirm-host")

	// The transfer list: every row says which server it is about.
	tp := &transferPlatform{fakePlatform: newPlatform(t)}
	tweb, _ := demo(t, tp.fakePlatform)
	ta := appui.New(tp)
	tr := ui.NewTester(ta.View, 1280, 900)
	tr.SetTitleBar(ui.TitleBar{Height: 44, Left: 72})
	session(t, tp.fakePlatform, tr, "s1", "prod-web-01", tweb)
	tp.jobs = []transfer.Job{
		{ID: "1", Label: "上传 release.tar.gz → prod-web-01:/srv/app", State: "running", Bytes: 48 << 20, Total: 120 << 20, Attempt: 1, Started: now.Add(-20 * time.Second)},
		{ID: "2", Label: "下载 syslog ← db-master:/var/log", State: "done", Bytes: 2200000, Attempt: 1, Started: now.Add(-time.Minute), Finished: now.Add(-50 * time.Second)},
		{ID: "3", Label: "上传 config.yaml → prod-web-01:/etc/app", State: "failed", Error: "sftp: \"permission denied\" (SSH_FX_PERMISSION_DENIED)", Attempt: 1},
		{ID: "4", Label: "上传 config.yaml → db-master:/etc/app", State: "failed", Error: "sftp: \"permission denied\" (SSH_FX_PERMISSION_DENIED)", Attempt: 1},
		{ID: "5", Label: "删除 build-01:/srv/cache", State: "cancelled", Items: true, Attempt: 1},
		{ID: "6", Label: "复制 dump.sql ← db-master:/srv → prod-web-01:/tmp", State: "queued", Attempt: 1},
	}
	tr.Frame()
	click(t, tr, "2 个传输失败，2 个进行中")
	shot(tr, "ub-3-transfer-list-hosts")

	// A link row: where it leads, and one that leads nowhere.
	lp := newPlatform(t)
	lweb, _ := demo(t, lp)
	_, lt := start(lp)
	lt.SetTitleBar(ui.TitleBar{Height: 44, Left: 72})
	lt.SetSize(1280, 1100)
	session(t, lp, lt, "s1", "prod-web-01", lweb)
	click(t, lt, "SFTP")
	listing(lp, lt, "s1", "/etc/nginx",
		sftpfs.Entry{Name: "conf.d", Path: "/etc/nginx/conf.d", Directory: true, Mode: "drwxr-xr-x", Modified: now.Add(-26 * time.Hour)},
		sftpfs.Entry{Name: "sites-available", Path: "/etc/nginx/sites-available", Directory: true, Mode: "drwxr-xr-x", Modified: now.Add(-50 * time.Hour)},
		sftpfs.Entry{Name: "logs", Path: "/etc/nginx/logs", Directory: true, Symlink: true, Target: "/var/log/nginx", Mode: "Lrwxrwxrwx", Modified: now.Add(-72 * time.Hour)},
		sftpfs.Entry{Name: "nginx.conf", Path: "/etc/nginx/nginx.conf", Size: 1536, Mode: "-rw-r--r--", Modified: now.Add(-time.Hour)},
		sftpfs.Entry{Name: "default.conf", Path: "/etc/nginx/default.conf", Symlink: true, Target: "sites-available/default", Size: 2048, Mode: "Lrwxrwxrwx", Modified: now.Add(-5 * time.Hour)},
		sftpfs.Entry{Name: "old-site", Path: "/etc/nginx/old-site", Symlink: true, Broken: true, Target: "/srv/removed", Mode: "Lrwxrwxrwx", Modified: now.Add(-90 * time.Hour)})
	click(t, lt, "选择 default.conf")
	hover(t, lt, "选择 old-site")
	shot(lt, "ub-4-symlink-rows")
}
