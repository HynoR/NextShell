package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestCloseOnCmdW(t *testing.T) {
	afterFrame = func(f func()) { f() }
	closed, frames := 0, 0
	tt := ui.NewTester(closeOnCmdW(func(c *ui.Context) { frames++; ui.Text(c, "settings") }, func() { closed++ }), 300, 200)
	tt.Key(ui.Cmd, ui.KeyS)
	tt.Key(ui.Ctrl|ui.Shift, ui.KeyW)
	if closed != 0 {
		t.Fatal("only Cmd+W closes the window")
	}
	tt.Key(ui.Cmd, ui.KeyW)
	if closed != 1 || frames == 0 {
		t.Fatalf("closed=%d frames=%d: Cmd+W closes once and the view still draws", closed, frames)
	}
}
