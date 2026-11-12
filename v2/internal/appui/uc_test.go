package appui_test

import (
	"image/color"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/appui"
)

// pixel is the color the window shows at x, y (DIPs; the testers render at scale 1).
func pixel(tt *ui.Tester, x, y float32) color.RGBA { return tt.Image().RGBAAt(int(x), int(y)) }

// within reports whether got is hex, give or take a rounding step.
func within(got color.RGBA, hex string) bool {
	want := ui.Hex(hex)
	d := func(a, b uint8) int {
		if a > b {
			return int(a - b)
		}
		return int(b - a)
	}
	return d(got.R, want.R) <= 2 && d(got.G, want.G) <= 2 && d(got.B, want.B) <= 2
}

// buttonFill is the fill of the button named label, sampled left of its text.
func buttonFill(t *testing.T, tt *ui.Tester, label string) color.RGBA {
	t.Helper()
	info, ok := tt.AccessByRole(ui.RoleButton, label)
	if !ok {
		t.Fatal("no button", label, tt.Texts())
	}
	return pixel(tt, info.Bounds.X+6, info.Bounds.Y+info.Bounds.H/2)
}

func focused(t *testing.T, tt *ui.Tester, label string) bool {
	t.Helper()
	info, ok := tt.AccessByRole(ui.RoleButton, label)
	if !ok {
		t.Fatal("no button", label, tt.Texts())
	}
	return info.Focused
}

// Every confirmation is the same dialog with the focus on the button that backs out, so Enter never does
// the action; a destructive one has its action filled in the danger color, any other in the main color.
func TestConfirmationsAreSafeByDefaultAndDangerIsRed(t *testing.T) {
	p := newPlatform(t)
	demo(t, p)
	a := appui.New(p)
	st := ui.NewTester(a.SettingsView, 880, 640)
	deleted := func() (n int) {
		snap, _ := p.Snapshot()
		for _, c := range snap.Connections {
			if c.Deleted {
				n++
			}
		}
		return n
	}
	click(t, st, "回收站")
	click(t, st, "清空回收站（2）")
	has(t, st, "清空回收站", "永久删除 2 个主机和它们保存的凭据，无法恢复。", "删除 2 个主机")
	if !focused(t, st, "取消") || focused(t, st, "删除 2 个主机") {
		t.Fatal("the focus is not on 取消")
	}
	for dark, want := range map[bool]string{false: "#ac1c1c", true: "#ff9a9a"} {
		st.SetDark(dark)
		st.Frame()
		if got := buttonFill(t, st, "删除 2 个主机"); !within(got, want) {
			t.Fatalf("dark=%v: the destructive action is %v, want the danger color %s", dark, got, want)
		}
	}
	st.SetDark(false)
	st.Frame()
	st.Key(0, ui.KeyEnter)
	if st.HasText("永久删除 2 个主机和它们保存的凭据，无法恢复。") || deleted() != 2 {
		t.Fatal("Enter confirmed, or left the dialog open", deleted())
	}
	click(t, st, "清空回收站（2）")
	click(t, st, "删除 2 个主机")
	if deleted() != 0 {
		t.Fatal("the button did not confirm", deleted())
	}
}

// A confirmation of something that can be undone keeps the main color, and Enter backs out of it too.
func TestPlainConfirmationKeepsTheMainColorAndEnterBacksOut(t *testing.T) {
	_, p, tt := fixture(t)
	url := "https://example.invalid/real-target"
	p.events = append(p.events, appui.Event{Link: url})
	tt.Frame()
	for dark, want := range map[bool]string{false: "#1f6fd6", true: "#4d9fff"} {
		tt.SetDark(dark)
		tt.Frame()
		if got := buttonFill(t, tt, "打开链接"); !within(got, want) {
			t.Fatalf("dark=%v: %v, want the main color %s", dark, got, want)
		}
	}
	tt.SetDark(false)
	tt.Frame()
	if !focused(t, tt, "取消") {
		t.Fatal("the focus is not on 取消")
	}
	tt.Key(0, ui.KeyEnter)
	if len(p.opened) != 0 || tt.HasText("请确认完整地址可信") {
		t.Fatal("Enter opened the link", p.opened)
	}
}

// The editor's own questions look the same: dropping edits is red and does not have the focus.
func TestEditorQuestionsUseTheSameDialog(t *testing.T) {
	a, p, tt, _, _ := editorFixture(t)
	open(p, tt, "one", "/etc/app.conf", "a=1\n")
	tt.Type("x")
	click(t, tt, "关闭标签 app.conf")
	has(t, tt, "关闭未保存的文件", "取消", "不保存", "保存")
	if got := buttonFill(t, tt, "不保存"); !within(got, "#ac1c1c") {
		t.Fatal("不保存 is not red", got)
	}
	if !focused(t, tt, "保存") || focused(t, tt, "不保存") {
		t.Fatal("Enter would not save")
	}
	click(t, tt, "取消")

	ed := ui.NewTester(a.EditorView, 900, 640)
	click(t, tt, "在新窗口打开")
	ed.Frame()
	if a.EditorMayClose() {
		t.Fatal("the unsaved document did not ask")
	}
	ed.Frame()
	has(t, ed, "关闭编辑器", "1 个文件的修改尚未保存，关闭后会丢失。")
	if !focused(t, ed, "取消") || !within(buttonFill(t, ed, "全部不保存"), "#ac1c1c") {
		t.Fatal("取消 has no focus, or the action is not red")
	}
	ed.Key(0, ui.KeyEnter)
	if ed.HasText("1 个文件的修改尚未保存，关闭后会丢失。") || a.DirtyDocumentCount() != 1 {
		t.Fatal("Enter discarded the edits")
	}
}

// The bars of the system monitor turn amber at 70% and red at 90% in colors that stay visible on their
// track in both themes, not the deepened text colors of the light theme.
func TestMetersTurnAmberAndRedInBothThemes(t *testing.T) {
	_, p, tt := fixture(t)
	session(t, p, tt, "one", "one", remoteHost("one"))
	snap := monitorSnapshot(time.Now(), 0)
	snap.Disks[0].UsedPercent = 74
	snap.MemoryTotal, snap.MemoryAvailable = 8<<30, 400<<20 // 95% used
	p.events = append(p.events, appui.Event{Monitor: &appui.MonitorResult{Session: "one", Snapshot: snap}})
	tt.Frame()
	for _, v := range []struct {
		dark                   bool
		cpu, warning, critical string
	}{{false, "#2563eb", "#c26700", "#dc2626"}, {true, "#4d9fff", "#f59e0b", "#f87171"}} {
		tt.SetDark(v.dark)
		tt.Frame()
		for name, want := range map[string]string{"CPU": v.cpu, "磁盘": v.warning, "内存": v.critical} {
			info, ok := tt.AccessByRole(ui.RoleMeter, name)
			if !ok {
				t.Fatal("no meter", name, tt.Texts())
			}
			if got := pixel(tt, info.Bounds.X+4, info.Bounds.Y+info.Bounds.H/2); !within(got, want) {
				t.Errorf("dark=%v %s: fill %v, want %s", v.dark, name, got, want)
			}
		}
	}
}
