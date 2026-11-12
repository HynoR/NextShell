package appui_test

import (
	"bytes"
	"fmt"
	"image/png"
	"testing"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/model"
)

// The tray options are offered only where the system has a tray, and they save with the other settings.
func TestTrayOptionsAppearOnlyWhereThereIsATray(t *testing.T) {
	p := newPlatform(t)
	a, _ := start(p)
	st := ui.NewTester(a.SettingsView, 880, 900)
	if st.HasText("最小化到托盘") || st.HasText("关闭窗口时最小化到托盘") {
		t.Fatal("tray options without a tray")
	}
	has(t, st, "关闭窗口时确认")
	changed := 0
	a.PreferencesChanged = func() { changed++ }
	a.TrayAvailable = true
	st.Frame()
	has(t, st, "最小化到托盘", "关闭窗口时最小化到托盘")
	if st.HasText("这一项管的是退出时的确认") {
		t.Fatal("the confirmation explained itself before the tray took over closing")
	}
	flip(t, st, "最小化到托盘")
	if got := p.Preferences(); !got.MinimizeToTray || got.CloseToTray {
		t.Fatal("minimize to tray", got.MinimizeToTray, got.CloseToTray)
	}
	flip(t, st, "关闭窗口时最小化到托盘")
	if got := p.Preferences(); !got.MinimizeToTray || !got.CloseToTray {
		t.Fatal("close to tray", got.MinimizeToTray, got.CloseToTray)
	}
	if changed < 2 || !a.Prefs.CloseToTray {
		t.Fatal("the host was not told", changed)
	}
	// 1.x switched the confirmation off here; it stays, because quitting from the tray or with ⌘Q still asks.
	has(t, st, "关闭窗口只会隐藏窗口；这一项管的是退出时的确认，从托盘退出也一样")
	flip(t, st, "关闭窗口时确认")
	if !p.Preferences().NoCloseConfirm {
		t.Fatal("the confirmation switch did not work with close-to-tray on")
	}
	flip(t, st, "关闭窗口时最小化到托盘")
	if p.Preferences().CloseToTray || st.HasText("这一项管的是退出时的确认") {
		t.Fatal("close to tray stayed on")
	}
}

func TestTrayMenuActionsConnectOpenATerminalAndQuit(t *testing.T) {
	p := newPlatform(t)
	var hosts []model.Connection
	for i := range 10 {
		hosts = append(hosts, saveHost(t, p, model.Connection{Name: fmt.Sprintf("host-%02d", i), Host: fmt.Sprintf("10.0.0.%d", i), User: "root"}))
	}
	gone := saveHost(t, p, model.Connection{Name: "removed", Host: "10.9.9.9", User: "root", Deleted: true})
	a, tt := start(p)
	list := a.TrayHosts()
	if len(list) != 8 {
		t.Fatal("the menu lists", len(list), "hosts")
	}
	for _, h := range list {
		if h.Name == "removed" || h.Login == "" {
			t.Fatal("entry", h)
		}
	}

	a.TrayConnect(hosts[7].ID)
	a.TrayConnect(gone.ID)
	a.TrayConnect("no-such-host")
	tt.Frame()
	if len(p.connects) != 1 || p.connects[0].ID != hosts[7].ID {
		t.Fatal("connected to", p.connects)
	}
	if got := a.TrayHosts(); got[0].ID != hosts[7].ID {
		t.Fatal("the host just used is not first:", got[0])
	}

	a.TrayLocal()
	tt.Frame()
	if p.local != 1 {
		t.Fatal("local terminals", p.local)
	}
}

// Quitting from the tray asks as ⌘Q does: with sessions open or files unsaved the window shows the question.
func TestTrayQuitGoesThroughTheQuitGuard(t *testing.T) {
	p := newPlatform(t)
	quits := 0
	p.SetQuit(func() { quits++ })
	a, tt := start(p)
	a.TrayQuit()
	if quits != 1 {
		t.Fatal("nothing open, yet the tray did not quit")
	}

	session(t, p, tt, "one", "one", remoteHost("one"))
	a.TrayQuit()
	tt.Frame()
	if quits != 1 || !a.ConfirmExit {
		t.Fatal("quit with a session open", quits, a.ConfirmExit)
	}
	has(t, tt, "退出 NextShell？", "仍有 1 个会话")
	click(t, tt, "取消")
	if quits != 1 || a.ConfirmExit {
		t.Fatal("cancel quit", quits)
	}
	a.TrayQuit()
	tt.Frame()
	click(t, tt, "退出")
	if quits != 2 || !a.ExitConfirmed {
		t.Fatal("confirmed quit", quits)
	}

	// The confirmation switched off still asks about unsaved files.
	ea, ep, et, _, _ := editorFixture(t)
	quits = 0
	ep.SetQuit(func() { quits++ })
	prefs := ep.Preferences()
	prefs.NoCloseConfirm = true
	if e := ep.SavePreferences(prefs); e != nil {
		t.Fatal(e)
	}
	ea.Prefs = prefs
	ea.TrayQuit()
	if quits != 1 {
		t.Fatal("sessions alone asked although the confirmation is off")
	}
	open(ep, et, "one", "/etc/app.conf", "a=1\n")
	et.Type("x")
	ea.TrayQuit()
	et.Frame()
	if quits != 1 {
		t.Fatal("quit with an unsaved file")
	}
	has(t, et, "退出 NextShell？", "仍有 1 个会话、1 个未保存的文件")
}

// The tray icon is the terminal mark in black on transparent: a template icon the system tints.
func TestTrayIconIsABlackTemplateImage(t *testing.T) {
	img, e := png.Decode(bytes.NewReader(appui.TrayIcon()))
	if e != nil {
		t.Fatal(e)
	}
	if b := img.Bounds(); b.Dx() != 36 || b.Dy() != 36 {
		t.Fatal("size", b)
	}
	inked := 0
	for y := range 36 {
		for x := range 36 {
			r, g, b, a := img.At(x, y).RGBA()
			if a != 0 {
				inked++
				if r != 0 || g != 0 || b != 0 {
					t.Fatalf("pixel %d,%d is not black: %v %v %v", x, y, r, g, b)
				}
			}
		}
	}
	if inked < 60 || inked > 36*36/2 {
		t.Fatal("the mark covers", inked, "pixels")
	}
}

// Where a desktop may show no tray icons (Linux), the close option says how to get the window back.
func TestCloseToTrayExplainsTheWayBackOnLinuxOnly(t *testing.T) {
	const hint = "看不到托盘图标时，再次打开 NextShell 即可显示窗口"
	for goos, want := range map[string]bool{"linux": true, "darwin": false, "windows": false} {
		restore := appui.SetOS(goos)
		p := newPlatform(t)
		a, _ := start(p)
		a.TrayAvailable = true
		st := ui.NewTester(a.SettingsView, 880, 900)
		if got := st.HasText(hint); got != want {
			t.Errorf("%s: hint shown=%v, want %v", goos, got, want)
		}
		has(t, st, "点关闭只会隐藏窗口，连接和终端继续运行；要退出请用托盘菜单里的「退出」")
		restore()
	}
}
