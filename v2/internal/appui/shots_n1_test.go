package appui_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/model"
)

// n1Screens are what the sessions of the grid's screenshots show: logs, a database prompt, full-screen
// programs, a build, a table.
var n1Screens = map[string]struct {
	cols, rows int
	text       string
}{
	"log": {110, 32, func() string {
		var b strings.Builder
		b.WriteString("\x1b[1;32mroot@prod-web-01\x1b[0m:\x1b[1;34m~\x1b[0m# tail -f /var/log/nginx/access.log\r\n")
		for i := 0; i < 24; i++ {
			code, color := 200, 32
			if i%7 == 3 {
				code, color = 502, 31
			} else if i%5 == 2 {
				code, color = 304, 36
			}
			fmt.Fprintf(&b, "10.0.%d.%d - - [12/Nov/2026:10:4%d:%02d +0000] \"GET /api/v1/items/%d HTTP/1.1\" \x1b[%dm%d\x1b[0m %d\r\n", i%4, 20+i, i/10, 10+i, 4000+i*13, color, code, 300+i*17)
		}
		return b.String()
	}()},
	"psql": {100, 30, "\x1b[1;32mpostgres@db-master\x1b[0m:~$ psql shop\r\npsql (16.2)\r\nType \"help\" for help.\r\n\r\nshop=# select status, count(*) from orders group by 1;\r\n  status   | count\r\n-----------+--------\r\n paid      | 128433\r\n pending   |   2114\r\n refunded  |    731\r\n(3 rows)\r\n\r\nshop=# \x1b[1mexplain analyze\x1b[0m select * from orders where id = 4711;\r\n Index Scan using orders_pkey on orders  (cost=0.43..8.45 rows=1)\r\n Planning Time: 0.121 ms\r\n Execution Time: 0.043 ms\r\nshop=# "},
	"top": {120, 34, func() string {
		var b strings.Builder
		b.WriteString("\x1b[?1049h\x1b[H\x1b[2J")
		b.WriteString("\x1b[1;1H  1  [\x1b[32m||||||||||||\x1b[0m                    32.1%]   Tasks: \x1b[1m214\x1b[0m, 612 thr; \x1b[32m3\x1b[0m running")
		b.WriteString("\x1b[2;1H  2  [\x1b[32m|||||||\x1b[0m                         18.4%]   Load average: 0.42 0.38 0.35")
		b.WriteString("\x1b[3;1H  3  [\x1b[31m|||||||||||||||||||||\x1b[0m           61.0%]   Uptime: 12 days, 03:41:07")
		b.WriteString("\x1b[4;1H Mem[\x1b[32m||||||||\x1b[34m||\x1b[33m|||\x1b[0m                 3.12G/7.77G]")
		b.WriteString("\x1b[6;1H\x1b[30;46m  PID USER      PRI  NI  VIRT   RES   SHR S CPU% MEM%   TIME+  Command                         \x1b[0m")
		for i, p := range []string{"nginx: worker process", "postgres: checkpointer", "node server.js --port 3000", "redis-server *:6379", "/usr/sbin/sshd -D", "containerd", "systemd-journald", "cron -f", "php-fpm: pool www", "dockerd -H fd://"} {
			fmt.Fprintf(&b, "\x1b[%d;1H%5d root       20   0  1.2G  310M  12M S %4.1f  %3.1f  1:%02d.%02d %s", 7+i, 812+i*37, 40.0-float64(i)*3.7, 4.0-float64(i)*.3, 10+i, 20+i, p)
		}
		b.WriteString("\x1b[34;1H\x1b[30;46mF1\x1b[0mHelp \x1b[30;46mF2\x1b[0mSetup \x1b[30;46mF3\x1b[0mSearch \x1b[30;46mF9\x1b[0mKill \x1b[30;46mF10\x1b[0mQuit")
		return b.String()
	}()},
	"build":  {90, 28, "\x1b[32m~/code/nextshell\x1b[0m $ go test ./...\r\n\x1b[32mok\x1b[0m  \tnextshell.dev/v2/internal/agent\t1.204s\r\n\x1b[32mok\x1b[0m  \tnextshell.dev/v2/internal/appui\t4.215s\r\n\x1b[32mok\x1b[0m  \tnextshell.dev/v2/internal/desktop\t2.871s\r\n\x1b[31mFAIL\x1b[0m\tnextshell.dev/v2/internal/sftpfs\t0.412s\r\n--- \x1b[31mFAIL\x1b[0m: TestListLarge (0.31s)\r\n    sftp_test.go:88: got 9998 entries, want 10000\r\n\x1b[32mok\x1b[0m  \tnextshell.dev/v2/internal/storage\t0.903s\r\n\x1b[1;31mFAIL\x1b[0m\r\n\x1b[32m~/code/nextshell\x1b[0m $ "},
	"docker": {120, 30, "ubuntu@staging-02:~$ docker ps\r\n\x1b[1mCONTAINER ID   IMAGE                 STATUS          PORTS                  NAMES\x1b[0m\r\n3f2a91c0e1b4   shop-api:2.14.1       Up 3 hours      0.0.0.0:8080->8080     shop-api\r\n9b7d00a3c2f8   postgres:16           Up 12 days      5432/tcp               shop-db\r\n51ce8e7714aa   redis:7-alpine        Up 12 days      6379/tcp               shop-cache\r\n0c44b2f9d3e1   nginx:1.27            Up 12 days      0.0.0.0:443->443       edge\r\nubuntu@staging-02:~$ docker logs -f shop-api --tail 4\r\n\x1b[36m2026-11-12T10:41:02Z\x1b[0m INFO  request id=8841 path=/healthz 200 1ms\r\n\x1b[36m2026-11-12T10:41:04Z\x1b[0m INFO  request id=8842 path=/api/orders 200 14ms\r\n\x1b[36m2026-11-12T10:41:05Z\x1b[0m \x1b[33mWARN\x1b[0m  slow query orders.list 412ms\r\n"},
	"vim": {100, 30, func() string {
		var b strings.Builder
		b.WriteString("\x1b[?1049h\x1b[H\x1b[2J")
		for i, l := range []string{"server {", "    listen 443 ssl http2;", "    server_name shop.example.com;", "", "    location /api/ {", "        proxy_pass http://127.0.0.1:8080;", "        proxy_set_header Host $host;", "        proxy_read_timeout 30s;", "    }", "}"} {
			fmt.Fprintf(&b, "\x1b[%d;1H\x1b[33m%2d\x1b[0m %s", i+1, i+1, l)
		}
		for r := 11; r <= 28; r++ {
			fmt.Fprintf(&b, "\x1b[%d;1H\x1b[34m~\x1b[0m", r)
		}
		b.WriteString("\x1b[29;1H\x1b[30;47m /etc/nginx/sites-enabled/shop.conf [+]                                          6,35      All \x1b[0m\x1b[6;37H")
		return b.String()
	}()},
	"shell":   {90, 26, "\x1b[1;32mdev@test-01\x1b[0m:\x1b[1;34m~/app\x1b[0m$ git status -sb\r\n## main...origin/main [ahead 2]\r\n \x1b[31mM\x1b[0m src/server.go\r\n\x1b[32mA\x1b[0m  src/health.go\r\n?? notes.txt\r\n\x1b[1;32mdev@test-01\x1b[0m:\x1b[1;34m~/app\x1b[0m$ make test\r\ngo test ./... \r\nok  \tapp/src\t0.311s\r\n\x1b[1;32mdev@test-01\x1b[0m:\x1b[1;34m~/app\x1b[0m$ "},
	"journal": {110, 30, "root@redis-01:~# journalctl -u redis -n 6 --no-pager\r\nNov 12 10:38:41 redis-01 redis[771]: \x1b[32m* Background saving started\x1b[0m\r\nNov 12 10:38:41 redis-01 redis[771]: * DB saved on disk\r\nNov 12 10:39:12 redis-01 redis[771]: \x1b[33m# WARNING overcommit_memory is set to 0\x1b[0m\r\nNov 12 10:40:03 redis-01 redis[771]: * 100 changes in 300 seconds. Saving...\r\nroot@redis-01:~# redis-cli info memory | head -3\r\n# Memory\r\nused_memory:1048576\r\nused_memory_human:1.00M\r\nroot@redis-01:~# "},
	"cron":    {100, 28, "root@bastion:~# crontab -l\r\n# m h  dom mon dow   command\r\n0 3 * * * /usr/local/bin/backup.sh >> /var/log/backup.log 2>&1\r\n*/5 * * * * /usr/local/bin/healthcheck\r\nroot@bastion:~# tail -n 3 /var/log/backup.log\r\n[10:00:01] backup started\r\n[10:02:47] \x1b[32mcompleted: 14.2 GiB in 166s\x1b[0m\r\nroot@bastion:~# "},
}

// gridScenes opens one session for each of the named hosts, showing the screens in order.
func gridScenes(t *testing.T, names []string, screens []string, gone int) (*ui.Tester, func()) {
	t.Helper()
	p := newPlatform(t)
	demo(t, p)
	a, tt := start(p)
	tt.SetTitleBar(ui.TitleBar{Height: 44, Left: 72})
	var conns []*captureConn
	for i, name := range names {
		c := model.Connection{}
		if name != "本地终端" {
			c = saveHost(t, p, model.Connection{Name: name, Host: fmt.Sprintf("10.0.%d.%d", i, 10+i), User: "root", Color: []string{"#ef4444", "#a855f7", "", "#f59e0b", "#22c55e", "#06b6d4", "", "#ec4899", ""}[i%9]})
		}
		s, conn := gridSession(t, p, tt, fmt.Sprintf("n1-%d", i), name, c, "")
		scr := n1Screens[screens[i]]
		s.Terminal.Resize(scr.cols, scr.rows)
		s.Terminal.Feed([]byte(scr.text))
		conns = append(conns, conn)
	}
	a.Active = 0
	tt.Frame()
	if gone >= 0 {
		conns[gone].Close()
		<-a.Tabs[gone].Terminal.Done()
	}
	tt.Frame()
	return tt, func() { openGrid(t, tt); tt.Advance(time.Second) }
}

// TestScreenshotsN1 renders the grid (监视网格) with four sessions, one of them disconnected, and with
// nine, and a dozen and a half that scroll.
func TestScreenshotsN1(t *testing.T) {
	shot := shooter(t)
	tt, open := gridScenes(t, []string{"prod-web-01", "db-master", "监控机", "bastion"}, []string{"log", "psql", "top", "cron"}, 3)
	open()
	shot(tt, "n1-1-grid-4")

	tt, open = gridScenes(t,
		[]string{"prod-web-01", "db-master", "prod-web-02", "build-01", "staging-02", "redis-01", "本地终端", "test-01", "bastion"},
		[]string{"log", "psql", "top", "build", "docker", "journal", "vim", "shell", "cron"}, 8)
	open()
	shot(tt, "n1-2-grid-9")

	var names, screens []string
	keys := []string{"log", "psql", "top", "build", "docker", "journal", "vim", "shell", "cron"}
	for i := 0; i < 14; i++ {
		names = append(names, fmt.Sprintf("host-%02d", i+1))
		screens = append(screens, keys[i%len(keys)])
	}
	tt, open = gridScenes(t, names, screens, -1)
	open()
	shot(tt, "n1-3-grid-14")
}
