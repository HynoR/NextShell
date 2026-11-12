package appui_test

import (
	"image/color"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/agent"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/desktop"
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

// A save conflict keeps the edits reachable: 重新载入 asks first, and offers 另存本地副本… which writes the text
// with the edits to the chosen file, touching neither the server's file nor the open document.
func TestConflictCanSaveALocalCopy(t *testing.T) {
	a, p, tt, _, _ := editorFixture(t)
	open(p, tt, "one", "/etc/app.conf", "a=1\n")
	tt.Type("x")
	click(t, tt, "保存")
	p.events = append(p.events, appui.Event{Message: "remote file changed since open; reload before saving", ForEditor: true})
	tt.Frame()
	has(t, tt, "远程文件在打开后被修改了，请重新载入后再保存")
	click(t, tt, "重新载入")
	has(t, tt, "重新载入会丢弃未保存的修改", "另存本地副本…", "丢弃并重新载入")

	click(t, tt, "另存本地副本…")
	has(t, tt, "此环境不支持系统文件选择框") // a host without a file panel says so
	dest := filepath.Join(t.TempDir(), "app.conf.mine")
	var picked []appui.FilePick
	a.PickFiles = func(o appui.FilePick, done func([]string)) { picked = append(picked, o); done([]string{dest}) }
	click(t, tt, "另存本地副本…")
	if len(picked) != 1 || !picked[0].Save || filepath.Base(picked[0].DefaultPath) != "app.conf" {
		t.Fatal("the save panel", picked)
	}
	got, e := os.ReadFile(dest)
	if e != nil || string(got) != "xa=1\n" {
		t.Fatalf("copy %q, %v: want the text with the unsaved edit", got, e)
	}
	if info, _ := os.Stat(dest); info.Mode().Perm() != 0o600 {
		t.Fatal("the copy is readable by others", info.Mode())
	}
	has(t, tt, "已另存到 "+dest, "重新载入会丢弃未保存的修改") // the banner stays: the user still chooses
	if len(p.reloaded) != 0 || len(p.saved) != 1 || a.DirtyDocumentCount() != 1 {
		t.Fatal("a copy touched the document or the server", p.reloaded, len(p.saved))
	}
}

// profile opens the real platform on a profile file, as the app does at start.
func profile(t *testing.T, path string) *editPlatform {
	t.Helper()
	p, e := desktop.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	return &editPlatform{fakePlatform: &fakePlatform{Platform: p, terminals: map[string]*terminal.Terminal{}, watching: map[string]bool{}, detailed: map[string]bool{}}}
}

// The external editor's arguments live in the settings, are saved with them, survive a restart and are used
// by 用外部编辑器打开; the editor itself no longer has an input for them.
func TestEditorArgumentsAreASettingAndSurviveARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.db")
	first := profile(t, path)
	a := appui.New(first)
	st := ui.NewTester(a.SettingsView, 880, 640)
	click(t, st, "远端编辑")
	has(t, st, "外部编辑器参数", "例如 --wait")
	click(t, st, "外部编辑器参数")
	st.Type(`--wait "my profile"`)
	if got := first.Preferences().EditorArgs; got != `--wait "my profile"` {
		t.Fatalf("saved %q", got)
	}
	first.Shutdown()

	again := profile(t, path)
	t.Cleanup(again.Shutdown)
	if got := again.Preferences().EditorArgs; got != `--wait "my profile"` {
		t.Fatalf("after a restart: %q", got)
	}
	b := appui.New(again)
	tt := ui.NewTester(b.View, 1280, 800)
	session(t, again.fakePlatform, tt, "one", "one", remoteHost("one"))
	open(again, tt, "one", "/etc/app.conf", "a=1\n")
	if tt.HasText("外部编辑器参数") {
		t.Fatal("the editor still has its own input for the arguments")
	}
	click(t, tt, "用外部编辑器打开")
	if len(again.external) != 1 || len(again.external[0]) != 2 || again.external[0][0] != "--wait" || again.external[0][1] != "my profile" {
		t.Fatal("args", again.external)
	}
	sb := ui.NewTester(b.SettingsView, 880, 640)
	click(t, sb, "远端编辑")
	if in, ok := sb.AccessByRole(ui.RoleTextField, "外部编辑器参数"); !ok || in.Value != `--wait "my profile"` {
		t.Fatal("the settings do not show the arguments", in.Value)
	}
}

// invalid reports whether the text input named label shows an error.
func invalid(t *testing.T, tt *ui.Tester, label string) bool {
	t.Helper()
	info, ok := tt.AccessByRole(ui.RoleTextField, label)
	if !ok {
		t.Fatal("no field", label, tt.Texts())
	}
	return info.Invalid
}

// One bad value no longer holds back the rest: it is reported on its own field, and on the other pages at the
// top, while the other settings (here the appearance and the main color) are saved as before.
func TestSettingsErrorIsInlineAndTheRestStillSaves(t *testing.T) {
	_, p, _, st := settingsFixture(t)
	click(t, st, "终端")
	click(t, st, "字号")
	st.Key(ui.Cmd, ui.KeyA)
	st.Type("99")
	has(t, st, "须为 10–24 的整数")
	if !invalid(t, st, "字号") || invalid(t, st, "行高") {
		t.Fatal("the wrong field is marked")
	}
	if p.Preferences().FontSize != 14 {
		t.Fatal("the bad value was saved", p.Preferences().FontSize)
	}

	// Another page: the error is not forgotten, said at the top, and does not stop what is changed here.
	click(t, st, "窗口行为")
	has(t, st, "「终端」里的「字号」须为 10–24 的整数，这一项还没有保存")
	click(t, st, "深色")
	click(t, st, "紫")
	got := p.Preferences()
	if got.Appearance != "深色" || got.Accent != "violet" || got.FontSize != 14 {
		t.Fatalf("saved %+v", got)
	}
	// 查看 goes to the field, which still holds what was typed.
	click(t, st, "查看 字号")
	has(t, st, "须为 10–24 的整数")
	if tt, ok := st.AccessByRole(ui.RoleTextField, "字号"); !ok || tt.Value != "99" || st.HasText("「终端」里的") {
		t.Fatal("the field lost its text, or the page repeats the error", tt.Value)
	}
	click(t, st, "字号")
	st.Key(ui.Cmd, ui.KeyA)
	st.Type("16")
	if invalid(t, st, "字号") || st.HasText("须为 10–24 的整数") || p.Preferences().FontSize != 16 {
		t.Fatal("fixed value", p.Preferences().FontSize)
	}

	// Each of the others reports itself on its own page and keeps the saved value.
	click(t, st, "文件传输")
	click(t, st, "默认上传目录")
	st.Key(ui.Cmd, ui.KeyA)
	st.Key(0, ui.KeyBackspace)
	has(t, st, "不能为空")
	if !invalid(t, st, "默认上传目录") || p.Preferences().UploadDir != "~" {
		t.Fatal("empty upload directory", p.Preferences().UploadDir)
	}
	click(t, st, "远端编辑")
	has(t, st, "「文件传输」里的「默认上传目录」不能为空，这一项还没有保存")
	click(t, st, "外部编辑器参数")
	st.Type(`--wait "x`)
	has(t, st, "引号没有闭合")
	if p.Preferences().EditorArgs != "" {
		t.Fatal("an unclosed quote was saved")
	}
	st.Type(`"`)
	if invalid(t, st, "外部编辑器参数") || p.Preferences().EditorArgs != `--wait "x"` {
		t.Fatal("closed quote", p.Preferences().EditorArgs)
	}
}

// haltPlatform remembers the emergency stop the way the real one does.
type haltPlatform struct {
	*servicesPlatform
	halts []bool
}

func (p *haltPlatform) AgentHalt(on bool) { p.halts = append(p.halts, on); p.agent.Halted = on }

// Settings polish: help sits under its switch like every other field's; the group 外观 has a field 主题;
// the emergency stop is a button, with the way back, not a switch that reads the wrong way round; a request
// with no target does not print a bare 目标.
func TestSettingsPolish(t *testing.T) {
	p := &haltPlatform{servicesPlatform: &servicesPlatform{fakePlatform: newPlatform(t)}}
	p.agent.Pending = []agent.Pending{{ID: "x", Client: "claude", Tool: "exec", Summary: "uptime", ExpiresAt: time.Now().Add(time.Minute)}}
	a := appui.New(p)
	st := ui.NewTester(a.SettingsView, 880, 1400)
	has(t, st, "外观", "主题", "每次启动时折叠；关闭时保持上次退出时的样子", "关闭仍在使用的连接前询问；本地终端和已断开的标签直接关闭")
	if st.HasText("不勾选") {
		t.Fatal("the help still talks of ticking a switch")
	}
	if _, ok := st.AccessByRole(ui.RoleSwitch, "外观"); ok {
		t.Fatal("a field is named like its group")
	}
	click(t, st, "终端")
	has(t, st, "新会话生效")
	click(t, st, "Agent")
	has(t, st, "claude · exec", "uptime")
	if st.HasText("目标 ") || st.HasText("目标") {
		t.Fatal("a request without a target printed 目标", st.Texts())
	}
	if _, ok := st.AccessByRole(ui.RoleSwitch, "紧急停止"); ok {
		t.Fatal("the emergency stop is still a switch")
	}
	click(t, st, "立即停止")
	has(t, st, "已紧急停止 · Agent 的请求都会被拒绝", "恢复 Agent")
	click(t, st, "恢复 Agent")
	if len(p.halts) != 2 || !p.halts[0] || p.halts[1] {
		t.Fatal(p.halts)
	}
	has(t, st, "立即停止")
}

// At the narrowest manager window the tile column still holds a whole tile, with the host's form open or not:
// at the old 640 the side column squeezed it to nothing.
func TestManagerAtItsMinimumWidthKeepsAWholeTile(t *testing.T) {
	p := newPlatform(t)
	demo(t, p)
	a, _ := start(p)
	mt := ui.NewTester(a.ManagerView, appui.ManagerMinWidth, 420)
	for _, editing := range []bool{false, true} {
		if editing {
			click(t, mt, "+ 新建连接")
		}
		tile := 0.0
		for _, info := range mt.AccessAll() {
			if info.Name == "legacy-box" {
				tile = max(tile, float64(info.Bounds.W))
				if info.Bounds.X+info.Bounds.W > appui.ManagerMinWidth-440 {
					t.Fatalf("editing=%v: the tile reaches under the side column: %v", editing, info.Bounds)
				}
			}
		}
		if tile < 190 {
			t.Fatalf("editing=%v: the tile is %.0f wide, want a whole 190", editing, tile)
		}
	}
}

// The import preview words the platform's English notes in Chinese; what it cannot place is
// "部分内容无法导入", with the original as the tooltip.
func TestImportPreviewNotesAreInChinese(t *testing.T) {
	a, p, tt := fixture(t)
	mt := manager(a)
	p.events = append(p.events, appui.Event{Import: &appui.ImportPreview{Count: 2, Names: []string{"a", "b"}, Token: "t", Warnings: []string{
		"skipped symlink old-config", "此文件含明文密码；确认后将使用 v2 设备密钥重新加密保存", "broken.json: invalid character 'x' looking for beginning of value", "decoder exploded",
	}}})
	tt.Frame()
	mt.Frame()
	has(t, mt, "将导入 2 个主机", "已跳过链接文件「old-config」", "此文件含明文密码；导入后会加密保存在本机", "「broken.json」无法读取，已跳过", "部分内容无法导入")
	for _, raw := range []string{"skipped symlink", "v2 设备密钥", "invalid character", "decoder exploded"} {
		if mt.HasText(raw) {
			t.Fatalf("%q shows in the preview", raw)
		}
	}
	if info, ok := mt.AccessByRole(ui.RoleText, "部分内容无法导入"); !ok || info.Description != "decoder exploded" {
		t.Fatal("the original wording is not the tooltip", info.Description)
	}
}
