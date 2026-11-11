package appui_test

import (
	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/appui"
	"testing"
)

func TestMRUOfficialKeysHeldReverseReleaseAndEscape(t *testing.T) {
	a, p, tt := fixture(t)
	for _, id := range []string{"a", "b", "c"} {
		p.events = append(p.events, appui.Event{Session: &appui.Session{ID: id, Title: id, Terminal: newTerminal(t, id)}})
	}
	tt.Frame()
	tt.Key(ui.Ctrl, ui.KeyTab)
	if a.Tabs[a.Active].ID != "c" || a.TabMRU.Preview() != "b" || !a.TabMRU.Active() {
		t.Fatal(a.Active, a.TabMRU)
	}
	tt.Key(ui.Ctrl, ui.KeyTab)
	if a.Tabs[a.Active].ID != "c" || a.TabMRU.Preview() != "a" {
		t.Fatal(a.Active, a.TabMRU)
	}
	tt.Key(ui.Ctrl|ui.Shift, ui.KeyTab)
	if a.Tabs[a.Active].ID != "c" || a.TabMRU.Preview() != "b" {
		t.Fatal(a.Active, a.TabMRU)
	}
	tt.CodeUp(0, ui.CodeControlLeft)
	tt.Frame()
	if a.TabMRU.Active() || a.TabMRU.Order[0] != "b" {
		t.Fatal(a.TabMRU)
	}
	tt.Key(ui.Ctrl, ui.KeyTab)
	if a.Tabs[a.Active].ID != "b" || a.TabMRU.Preview() != "c" {
		t.Fatal(a.Active, a.TabMRU)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.Tabs[a.Active].ID != "b" || a.TabMRU.Active() {
		t.Fatal(a.Active, a.TabMRU)
	}
	tt.Key(ui.Ctrl, ui.KeyTab)
	tt.Key(ui.Ctrl, ui.KeyDown)
	if a.Tabs[a.Active].ID != "b" || a.TabMRU.Preview() != "a" {
		t.Fatal("arrow preview", a.Active, a.TabMRU)
	}
	tt.SetWindowActive(false)
	tt.Frame()
	if a.TabMRU.Active() || a.Tabs[a.Active].ID != "b" {
		t.Fatal("blur committed", a.TabMRU)
	}

}
