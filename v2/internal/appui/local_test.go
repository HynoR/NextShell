package appui_test

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/desktop"
)

// A real local shell: open it, send a command from the palette's dialog, and keep the history.
func TestRealLocalTerminalSendCommand(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	p, e := desktop.Open(":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer p.Shutdown()
	a := appui.New(p)
	tt := ui.NewTester(a.View, 1280, 800)
	click(t, tt, "打开本地终端")
	wait := func(what string, ok func() bool) {
		t.Helper()
		until := time.Now().Add(5 * time.Second)
		for !ok() {
			if time.Now().After(until) {
				t.Fatal(what)
			}
			time.Sleep(10 * time.Millisecond)
			tt.Frame()
		}
	}
	wait("local session", func() bool { return len(a.Tabs) == 1 })
	has(t, tt, "本地终端")
	tt.Key(ui.Cmd, ui.KeyK)
	tt.Type("发送命令")
	tt.Key(0, ui.KeyEnter)
	tt.Type("printf '\\nnextshell-ui-ok\\n'")
	tt.Key(0, ui.KeyEnter)
	wait("command output", func() bool { return strings.Contains(a.Tabs[0].Terminal.Text(), "\nnextshell-ui-ok") })
	if snap, _ := p.Snapshot(); len(snap.History) != 1 {
		t.Fatal("history", snap.History)
	}
}
