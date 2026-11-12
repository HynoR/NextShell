package appui_test

import (
	"fmt"
	"image"
	"image/color"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/model"
	"nextshell.dev/v2/internal/testutil"
)

func write(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if e := os.WriteFile(path, data, 0o600); e != nil {
		t.Fatal(e)
	}
	return path
}

func TestBackgroundPreferencesValidate(t *testing.T) {
	p := model.DefaultPreferences()
	if p.BackgroundImage != "" || p.BackgroundOpacity != 60 || p.MinimizeToTray || p.CloseToTray {
		t.Fatal("defaults", p)
	}
	for _, bad := range []func(*model.Preferences){
		func(p *model.Preferences) { p.BackgroundOpacity = 29 },
		func(p *model.Preferences) { p.BackgroundOpacity = 81 },
		func(p *model.Preferences) { p.BackgroundImage = "../secret.png" },
		func(p *model.Preferences) { p.BackgroundImage = "/etc/passwd" },
		func(p *model.Preferences) { p.BackgroundImage = `a\b.png` },
		func(p *model.Preferences) { p.BackgroundImage = ".." },
	} {
		p := model.DefaultPreferences()
		bad(&p)
		if p.Validate() == nil {
			t.Fatal("accepted", p.BackgroundImage, p.BackgroundOpacity)
		}
	}
	p.BackgroundImage, p.BackgroundOpacity = "background-0123456789ab.png", 30
	if e := p.Validate(); e != nil {
		t.Fatal(e)
	}
	p.BackgroundOpacity = 80
	if e := p.Validate(); e != nil {
		t.Fatal(e)
	}
}

func at(img *image.RGBA, x, y int) color.RGBA { return img.RGBAAt(x, y) }

// diff counts the pixels that differ, and says where as the box around them.
func diff(a, b *image.RGBA) (int, string) {
	box := image.Rectangle{Min: image.Pt(1<<30, 1<<30)}
	n := 0
	for y := a.Rect.Min.Y; y < a.Rect.Max.Y; y++ {
		for x := a.Rect.Min.X; x < a.Rect.Max.X; x++ {
			if a.RGBAAt(x, y) != b.RGBAAt(x, y) {
				n++
				box = box.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	if a.Rect.Size() != b.Rect.Size() {
		return -1, "different sizes"
	}
	return n, fmt.Sprintf("%d pixels in %v", n, box)
}

func same(a, b *image.RGBA) bool { n, _ := diff(a, b); return n == 0 }

// The panels are translucent only while a picture is set. Without one (never chosen, or cleared) the window
// is the one it always was, to the pixel, and clearing deletes the copy.
func TestPanelsAreTranslucentOnlyOverAPicture(t *testing.T) {
	p := newPlatform(t)
	web, _ := demo(t, p)
	dir := t.TempDir()
	p.SetDataDir(dir)
	a, tt := start(p)
	tt.SetTitleBar(ui.TitleBar{Height: 44, Left: 72})
	glassSession(t, p, tt, "s1", "prod-web-01", web, "\r\nroot@prod-web-01:~# ")
	theme := ui.LightTheme()
	if a.Glass() || a.PaneColor(theme) != theme.Surface || a.PageColor(theme) != (ui.Color{}) {
		t.Fatal("translucent without a picture")
	}
	frames := map[bool]*image.RGBA{}
	plain := map[bool]*image.RGBA{}
	for _, dark := range []bool{false, true} {
		tt.SetDark(dark)
		tt.Frame()
		plain[dark] = tt.Image()
	}

	st := ui.NewTester(a.SettingsView, 880, 900)
	red := testutil.Solid(t, color.RGBA{R: 255})
	a.PickFiles = func(o appui.FilePick, done func([]string)) { done([]string{red}) }
	click(t, st, "选择背景图")
	if !a.Glass() && !func() bool { tt.Frame(); return a.Glass() }() {
		t.Fatal("no picture after choosing one")
	}
	if a.PaneColor(theme).A == 255 || a.PageColor(theme).A == 0 {
		t.Fatal("panels stay opaque over a picture", a.PaneColor(theme), a.PageColor(theme))
	}
	for _, dark := range []bool{false, true} {
		tt.SetDark(dark)
		tt.Frame()
		frames[dark] = tt.Image()
		// Left column, the terminal and the activity strip: each shows the red picture through it.
		for _, where := range [][2]int{{120, 560}, {700, 300}, {1258, 300}, {200, 22}, {700, 700}} {
			before, after := at(plain[dark], where[0], where[1]), at(frames[dark], where[0], where[1])
			if after.R <= before.R+15 || after.R < after.G+15 {
				t.Errorf("dark=%v at %v: %v over a red picture is %v, nothing shows through", dark, where, before, after)
			}
		}
	}

	// Clearing it returns every pixel.
	click(t, st, "清除背景图")
	if p.Preferences().BackgroundImage != "" {
		t.Fatal("the preference stayed")
	}
	if entries, _ := os.ReadDir(filepath.Join(dir, "background")); len(entries) != 0 {
		t.Fatal("the copy stayed", entries)
	}
	for _, dark := range []bool{false, true} {
		tt.SetDark(dark)
		tt.Frame()
		if !same(tt.Image(), plain[dark]) {
			t.Fatalf("dark=%v: the window differs from before the picture", dark)
		}
	}
	if a.Glass() {
		t.Fatal("still translucent")
	}
}

// Moving or deleting the chosen file changes nothing, and the picture is there on the next run. A copy that
// is gone is told once, and the window goes on as without a picture.
func TestBackgroundSurvivesTheOriginalAndAMissingCopyFallsBack(t *testing.T) {
	p := newPlatform(t)
	dir := t.TempDir()
	p.SetDataDir(dir)
	a, _ := start(p)
	st := ui.NewTester(a.SettingsView, 880, 900)
	src := testutil.Picture(t)
	a.PickFiles = func(o appui.FilePick, done func([]string)) {
		if !o.Images {
			t.Error("the panel is not limited to pictures")
		}
		done([]string{src})
	}
	click(t, st, "选择背景图")
	prefs := p.Preferences()
	if prefs.BackgroundImage == "" {
		t.Fatal("not saved")
	}
	if filepath.IsAbs(prefs.BackgroundImage) || strings.Contains(prefs.BackgroundImage, string(filepath.Separator)) {
		t.Fatal("the preference holds a path:", prefs.BackgroundImage)
	}
	os.Remove(src)

	next := appui.New(p) // the next run, on the same profile and data folder
	nt := ui.NewTester(next.View, 1280, 800)
	nt.Frame()
	if !next.Glass() {
		t.Fatal("the picture did not come back")
	}

	// The copy vanishes: one message, no picture, and the preference is left for the user to change.
	os.Remove(filepath.Join(dir, "background", prefs.BackgroundImage))
	gone := appui.New(p)
	gt := ui.NewTester(gone.View, 1280, 800)
	gt.Frame()
	has(t, gt, "背景图无法读取")
	if gone.Glass() {
		t.Fatal("translucent without a picture")
	}
	gt.Frame()
	if gt.HasText("背景图无法读取") && countText(gt, "背景图无法读取") > 1 {
		t.Fatal("told more than once")
	}
}

func countText(tt *ui.Tester, s string) int {
	n := 0
	for _, v := range tt.Texts() {
		if strings.Contains(v, s) {
			n++
		}
	}
	return n
}

// What is not a picture, and what is too big, is refused with the reason, and nothing changes.
func TestBackgroundChoiceRefusesWithAMessage(t *testing.T) {
	p := newPlatform(t)
	dir := t.TempDir()
	p.SetDataDir(dir)
	a, _ := start(p)
	st := ui.NewTester(a.SettingsView, 880, 900)
	notes := write(t, t.TempDir(), "notes.txt", []byte("hello"))
	a.PickFiles = func(o appui.FilePick, done func([]string)) { done([]string{notes}) }
	click(t, st, "选择背景图")
	has(t, st, "这不是可用的图片，请选择 PNG、JPEG、GIF、WebP 或 BMP 图片")
	if p.Preferences().BackgroundImage != "" || a.Glass() {
		t.Fatal("a refused file was taken")
	}
	huge := write(t, t.TempDir(), "huge.png", testutil.PNGHeader(8000, 6000))
	a.PickFiles = func(o appui.FilePick, done func([]string)) { done([]string{huge}) }
	click(t, st, "选择背景图")
	has(t, st, "图片尺寸太大（8000×6000）")
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatal("refused files left something:", entries)
	}
	// Without a data folder there is nowhere to keep the copy.
	p.SetDataDir("")
	a.PickFiles = func(o appui.FilePick, done func([]string)) { done([]string{testutil.Picture(t)}) }
	click(t, st, "选择背景图")
	has(t, st, "没有可用的数据文件夹")
}

// Terminals are made transparent, so a picture can be set or cleared with sessions open; without a picture
// the view under them paints their theme's background, and the window is what an opaque terminal gave.
func TestTransparentTerminalLooksLikeAnOpaqueOneWithoutAPicture(t *testing.T) {
	shoot := func(theme string, transparent bool) map[bool]*image.RGBA {
		p := newPlatform(t)
		prefs := p.Preferences()
		prefs.TerminalTheme = theme
		if e := p.SavePreferences(prefs); e != nil {
			t.Fatal(e)
		}
		web, _ := demo(t, p)
		_, tt := start(p)
		tt.SetTitleBar(ui.TitleBar{Height: 44, Left: 72})
		conn, peer := net.Pipe()
		term, e := terminal.New(terminal.Options{Conn: conn, NoClipboardWrite: true, NoBlink: true, Transparent: transparent})
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { peer.Close(); term.Close() })
		term.Feed([]byte("\r\n\x1b[41m red \x1b[0m root@prod-web-01:~# ls\r\n\x1b[34mnginx\x1b[0m  syslog\r\nroot@prod-web-01:~# "))
		p.events = append(p.events, appui.Event{Session: &appui.Session{ID: "s1", Title: "prod-web-01", Terminal: term, Connection: web}})
		tt.Frame()
		out := map[bool]*image.RGBA{}
		for _, dark := range []bool{false, true} {
			tt.SetDark(dark)
			tt.Frame()
			// The terminal's own box: the rest of the window shows times, which differ between two runs.
			var b ui.Rect
			for _, v := range tt.AccessAll() {
				if v.Name == "Terminal" {
					b = v.Bounds
				}
			}
			if b.W == 0 {
				t.Fatal("no terminal")
			}
			out[dark] = tt.Image().SubImage(image.Rect(int(b.X), int(b.Y), int(b.X+b.W), int(b.Y+b.H))).(*image.RGBA)
		}
		return out
	}
	for _, theme := range []string{"跟随外观", "Dracula", "Nord"} {
		opaque, transparent := shoot(theme, false), shoot(theme, true)
		for _, dark := range []bool{false, true} {
			if !same(opaque[dark], transparent[dark]) {
				t.Fatalf("%s, dark=%v: a transparent terminal changes the window without a picture: %s", theme, dark, func() string { _, s := diff(opaque[dark], transparent[dark]); return s }())
			}
		}
	}
}
