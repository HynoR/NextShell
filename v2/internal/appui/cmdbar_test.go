package appui_test

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestCommandBarSendsAndKeepsHistoryAcrossRestarts(t *testing.T) {
	_, p, tt := fixture(t)
	session(t, p, tt, "one", "one", remoteHost("one"))
	click(t, tt, "命令输入")
	for _, line := range []string{"uptime", "df -h", "uptime"} {
		tt.Type(line)
		tt.Key(0, ui.KeyEnter)
	}
	if len(p.sent) != 3 || p.sent[1] != "df -h" {
		t.Fatal("sent", p.sent)
	}
	// A new run on the same profile: ↑ walks back from the newest, ↓ returns to the line being typed.
	a, tt := start(p)
	session(t, p, tt, "two", "two", remoteHost("two"))
	click(t, tt, "命令输入")
	tt.Type("ls")
	tt.Key(0, ui.KeyUp)
	inputIs(t, tt, "uptime")
	tt.Key(0, ui.KeyUp)
	inputIs(t, tt, "df -h")
	tt.Key(0, ui.KeyUp) // a repeat was kept once
	inputIs(t, tt, "df -h")
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyDown)
	inputIs(t, tt, "ls")
	tt.Key(0, ui.KeyEnter)
	if p.sent[len(p.sent)-1] != "ls" || len(a.Data.History) != 3 {
		t.Fatal("history", p.sent, a.Data.History)
	}
}

func TestFindBarSearchesScrollback(t *testing.T) {
	_, p, tt := fixture(t)
	s := session(t, p, tt, "one", "one", remoteHost("one"))
	for range 60 {
		s.Terminal.Feed([]byte("noise\r\n"))
	}
	s.Terminal.Feed([]byte("error: disk full\r\nok\r\nerror: retry\r\n"))
	for range 60 {
		s.Terminal.Feed([]byte("noise\r\n"))
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyF)
	has(t, tt, "查找终端内容")
	tt.Type("error")
	for range 5 {
		tt.Frame()
	}
	has(t, tt, "1 / 2")
	tt.Key(0, ui.KeyEnter)
	has(t, tt, "2 / 2")
	click(t, tt, "上一个")
	has(t, tt, "1 / 2")
	tt.Key(0, ui.KeyEscape)
	if tt.HasText("1 / 2") || s.Terminal.SearchStatus().Text != "" {
		t.Fatal("find bar stayed")
	}
}

func inputIs(t *testing.T, tt *ui.Tester, want string) {
	t.Helper()
	for _, v := range tt.AccessAll() {
		if v.Name == "命令输入" && v.Role == ui.RoleTextField {
			if v.Value != want {
				t.Fatalf("command bar shows %q, want %q", v.Value, want)
			}
			return
		}
	}
	t.Fatal("no command bar")
}

// The bar's hint names its real keys; a double tap of Ctrl (as in FinalShell) or the app's L shortcut moves
// the keyboard between the terminal and the bar, and Ctrl used with a key does not.
func TestCommandBarHintAndCtrlDoubleTapSwapsFocus(t *testing.T) {
	_, p, tt := fixture(t)
	session(t, p, tt, "one", "one", remoteHost("one"))
	if got := barPlaceholder(tt); got != "回车发送 · ↑/↓ 历史 · 双击 Ctrl 或 "+paletteKey("L")+" 切换终端/命令栏" {
		t.Fatal("placeholder", got)
	}
	tap := func() {
		tt.CodeDown(ui.Ctrl, ui.CodeControlLeft)
		tt.CodeUp(0, ui.CodeControlLeft)
	}
	tap()
	tap()
	tt.Frame()
	tt.Type("ab")
	inputIs(t, tt, "ab") // the bar has the keys
	tap()
	tap()
	tt.Frame()
	tt.Type("x")
	inputIs(t, tt, "ab") // the terminal has them again
	// Ctrl+C then a tap: one tap, not two.
	tt.CodeDown(ui.Ctrl, ui.CodeControlLeft)
	tt.Key(ui.Ctrl, ui.KeyC)
	tt.CodeUp(0, ui.CodeControlLeft)
	tap()
	tt.Frame()
	tt.Type("y")
	inputIs(t, tt, "ab")
	// The shortcut goes in and, pressed again, out.
	tt.Key(appMod(), ui.KeyL)
	tt.Type("c")
	inputIs(t, tt, "abc")
	tt.Key(appMod(), ui.KeyL)
	tt.Type("z")
	inputIs(t, tt, "abc")
}
