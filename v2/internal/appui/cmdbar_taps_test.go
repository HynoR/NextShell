package appui

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// Two quick taps of Ctrl alone are a double tap; a slow tap, a held one, a key between or Ctrl+Shift is not.
func TestCtrlTaps(t *testing.T) {
	t0 := time.Unix(1000, 0)
	down := func(code ui.KeyCode, mods ui.Modifiers) ui.InputEvent {
		return ui.InputEvent{Kind: ui.InputKeyDown, Code: code, Mods: mods}
	}
	up := func(code ui.KeyCode) ui.InputEvent { return ui.InputEvent{Kind: ui.InputKeyUp, Code: code} }
	type step struct {
		ev ui.InputEvent
		at time.Duration
	}
	ctrl, right := ui.CodeControlLeft, ui.CodeControlRight
	for name, tc := range map[string]struct {
		steps []step
		want  bool
	}{
		"double tap":      {[]step{{down(ctrl, ui.Ctrl), 0}, {up(ctrl), 80 * time.Millisecond}, {down(ctrl, ui.Ctrl), 200 * time.Millisecond}, {up(ctrl), 280 * time.Millisecond}}, true},
		"left then right": {[]step{{down(ctrl, ui.Ctrl), 0}, {up(ctrl), 80 * time.Millisecond}, {down(right, ui.Ctrl), 200 * time.Millisecond}, {up(right), 280 * time.Millisecond}}, true},
		"slow taps":       {[]step{{down(ctrl, ui.Ctrl), 0}, {up(ctrl), 80 * time.Millisecond}, {down(ctrl, ui.Ctrl), 700 * time.Millisecond}, {up(ctrl), 780 * time.Millisecond}}, false},
		"held":            {[]step{{down(ctrl, ui.Ctrl), 0}, {up(ctrl), 80 * time.Millisecond}, {down(ctrl, ui.Ctrl), 100 * time.Millisecond}, {up(ctrl), 900 * time.Millisecond}}, false},
		"ctrl+c between":  {[]step{{down(ctrl, ui.Ctrl), 0}, {up(ctrl), 50 * time.Millisecond}, {down(ctrl, ui.Ctrl), 100 * time.Millisecond}, {ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyC, Code: ui.CodeKeyC, Mods: ui.Ctrl}, 120 * time.Millisecond}, {up(ctrl), 150 * time.Millisecond}}, false},
		"ctrl+shift":      {[]step{{down(ctrl, ui.Ctrl), 0}, {up(ctrl), 50 * time.Millisecond}, {down(ctrl, ui.Ctrl|ui.Shift), 100 * time.Millisecond}, {up(ctrl), 150 * time.Millisecond}}, false},
		"one tap":         {[]step{{down(ctrl, ui.Ctrl), 0}, {up(ctrl), 50 * time.Millisecond}}, false},
	} {
		var d ctrlTaps
		for _, s := range tc.steps {
			d.see(s.ev, t0.Add(s.at))
		}
		if got := d.take(); got != tc.want {
			t.Errorf("%s: %v", name, got)
		}
		if d.take() {
			t.Errorf("%s: taken twice", name)
		}
	}
}
