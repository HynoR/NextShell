package appui_test

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/appui"
)

// TestScreenshotsUC renders the dialogs, editor, settings, meters and manager of batch C.
func TestScreenshotsUC(t *testing.T) {
	shot := shooter(t)

	// A destructive confirmation (the action red, 取消 focused) and a plain one (the main color).
	p := newPlatform(t)
	web, _ := demo(t, p)
	a, tt := start(p)
	tt.SetTitleBar(ui.TitleBar{Height: 44, Left: 72})
	session(t, p, tt, "s1", "prod-web-01", web)
	st := ui.NewTester(a.SettingsView, 880, 640)
	click(t, st, "回收站")
	click(t, st, "清空回收站（2）")
	shot(st, "uc-1a-confirm-danger")
	st.Key(0, ui.KeyEscape)
	p.events = append(p.events, appui.Event{Link: "https://example.invalid/real-target"})
	tt.Frame()
	shot(tt, "uc-1b-confirm-plain")

	// The editor after a save conflict: the reload banner with 另存本地副本….
	ep, et, ed := func() (*editPlatform, *ui.Tester, *appui.App) {
		a, p, tt, _, _ := editorFixture(t)
		tt.SetTitleBar(ui.TitleBar{Height: 44, Left: 72})
		return p, tt, a
	}()
	_ = ed
	open(ep, et, "one", "/etc/nginx/nginx.conf", "worker_processes 1;\nevents {\n    worker_connections 1024;\n}\n")
	et.Type("# edited\n")
	click(t, et, "保存")
	ep.events = append(ep.events, appui.Event{Message: "remote file changed since open; reload before saving", ForEditor: true})
	et.Frame()
	click(t, et, "重新载入")
	shot(et, "uc-2-editor-conflict-banner")

	// 设置 → 远端编辑 with the arguments field filled, and a validation error on its page and on another.
	sp := newPlatform(t)
	sa := appui.New(sp)
	ss := ui.NewTester(sa.SettingsView, 880, 520)
	click(t, ss, "远端编辑")
	click(t, ss, "外部编辑器参数")
	ss.Type(`--wait --new-window "my profile"`)
	shot(ss, "uc-3-settings-editor-args")
	click(t, ss, "终端")
	click(t, ss, "字号")
	ss.Key(ui.Cmd, ui.KeyA)
	ss.Type("99")
	shot(ss, "uc-4a-settings-error-inline")
	click(t, ss, "窗口行为")
	shot(ss, "uc-4b-settings-error-banner")

	// The system monitor's bars at 74% (amber) and 95% (red), over the normal ones.
	mp := newPlatform(t)
	mweb, _ := demo(t, mp)
	ma, mt := start(mp)
	_ = ma
	mt.SetTitleBar(ui.TitleBar{Height: 44, Left: 72})
	session(t, mp, mt, "s1", "prod-web-01", mweb)
	snap := monitorSnapshot(time.Now(), 3)
	snap.Disks[0].UsedPercent = 74
	snap.Disks[0].UsedKB = 59 << 20
	snap.MemoryTotal, snap.MemoryAvailable = 8<<30, 400<<20
	snap.SwapTotal, snap.SwapFree = 2<<30, 700<<20
	mp.events = append(mp.events, appui.Event{Monitor: &appui.MonitorResult{Session: "s1", Snapshot: snap, Latency: 18 * time.Millisecond}})
	mt.Frame()
	shot(mt, "uc-5-meters-74-95")

	// The manager at its narrowest, with a host chosen, and with the form open.
	gp := newPlatform(t)
	demo(t, gp)
	ga, _ := start(gp)
	gm := ui.NewTester(ga.ManagerView, appui.ManagerMinWidth, 560)
	click(t, gm, "build-01")
	shot(gm, "uc-6a-manager-min-width")
	click(t, gm, "+ 新建连接")
	shot(gm, "uc-6b-manager-min-width-form")
}
