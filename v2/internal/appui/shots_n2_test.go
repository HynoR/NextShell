package appui_test

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/sftpfs"
	"nextshell.dev/v2/internal/testutil"
)

// TestScreenshotsN2 renders the main window over a background picture, and the 窗口行为 settings with the
// picture and tray options, light and dark.
func TestScreenshotsN2(t *testing.T) {
	shot := shooter(t)
	p := newPlatform(t)
	web, _ := demo(t, p)
	p.SetDataDir(t.TempDir())
	a, tt := start(p)
	a.TrayAvailable = true
	tt.SetTitleBar(ui.TitleBar{Height: 44, Left: 72})
	s := glassSession(t, p, tt, "s1", "prod-web-01", web, "\r\nroot@prod-web-01:~# uptime\r\n 10:42:01 up 12 days,  3:02,  1 user,  load average: 0.42, 0.38, 0.35\r\nroot@prod-web-01:~# \x1b[32mls\x1b[0m --color\r\n\x1b[34mnginx\x1b[0m  \x1b[34mapt\x1b[0m  syslog  auth.log\r\nroot@prod-web-01:~# ")
	_ = s
	now := time.Now()
	p.events = append(p.events, appui.Event{Files: &appui.FileListing{Session: "s1", Path: "/var/log", Entries: []sftpfs.Entry{
		{Name: "nginx", Path: "/var/log/nginx", Directory: true, Mode: "drwxr-xr-x", Modified: now.Add(-26 * time.Hour)},
		{Name: "apt", Path: "/var/log/apt", Directory: true, Mode: "drwxr-xr-x", Modified: now.Add(-50 * time.Hour)},
		{Name: "syslog", Path: "/var/log/syslog", Size: 2200000, Mode: "-rw-r-----", Modified: now.Add(-time.Hour)},
		{Name: "auth.log", Path: "/var/log/auth.log", Size: 81000, Mode: "-rw-r-----", Modified: now.Add(-2 * time.Hour)},
	}}})
	for i := range 12 {
		p.events = append(p.events, appui.Event{Monitor: &appui.MonitorResult{Session: "s1", Snapshot: monitorSnapshot(now.Add(time.Duration(i-12)*2*time.Second), i), Latency: 18 * time.Millisecond}})
	}
	tt.Frame()
	click(t, tt, "SFTP")
	shot(tt, "n2-1-main-plain")

	st := ui.NewTester(a.SettingsView, 880, 760)
	a.TrayAvailable = false
	shot(st, "n2-3a-settings-no-tray") // where the system has no tray, its options are not offered
	a.TrayAvailable = true
	a.PickFiles = func(o appui.FilePick, done func([]string)) { done([]string{testutil.Picture(t)}) }
	click(t, st, "选择背景图")
	tt.Frame()
	shot(tt, "n2-2-main-picture")
	shot(st, "n2-3-settings-window")
	flip(t, st, "关闭窗口时最小化到托盘")
	st.Advance(time.Second) // the switch finishes sliding
	shot(st, "n2-3b-settings-close-to-tray")
	flip(t, st, "关闭窗口时最小化到托盘")

	// The same picture with the panels as clear as the slider lets them, and as dense.
	for _, opacity := range []int{30, 80} {
		prefs := p.Preferences()
		prefs.BackgroundOpacity = opacity
		a.Prefs = prefs
		tt.Frame()
		shot(tt, "n2-4-main-opacity-"+string(rune('0'+opacity/10)))
	}
}
