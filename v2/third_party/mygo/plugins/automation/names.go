//go:build !mygo_noinspector

package automation

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"

	"github.com/egoist/mygo/internal/accelerator"
	"github.com/egoist/mygo/internal/platform"
)

// roleNames are the names of platform.AccessRole's roles, in their order.
var roleNames = [...]string{
	"group", "text", "button", "link", "checkBox", "radio", "switch", "slider",
	"progress", "textField", "image", "list", "scroll", "dialog", "popup",
	"tooltip", "popUpButton", "tabList", "tab", "splitter", "status", "table",
	"row", "cell", "columnHeader", "tree", "treeItem", "listItem", "menuButton",
	"toolbar", "radioGroup", "toggleButton", "comboBox", "disclosure", "meter",
	"stepper", "colorWell", "alertDialog", "menu", "menuBar", "menuItem",
	"menuItemCheckBox", "menuItemRadio", "heading", "region", "log", "timer",
	"alert", "separator", "rowHeader", "search", "article",
	"navigation", "grid", "treeGrid", "deletion", "insertion",
}

func roleName(r platform.AccessRole) string {
	if int(r) < len(roleNames) {
		return roleNames[r]
	}
	return "role" + strconv.Itoa(int(r))
}

// stateNames are the names of platform.AccessStates' bits, from the
// lowest.
var stateNames = [...]string{
	"focusable", "disabled", "checked", "mixed", "selected", "expanded",
	"multiline", "password", "readOnly", "selectable", "offscreen",
	"multiselectable", "segment", "search", "invalid", "sortAscending",
	"sortDescending", "expandable", "vertical", "busy", "sortOther",
	"horizontal",
}

// actionNames are the names of platform.AccessActions' bits, from the
// lowest.
var actionNames = [...]string{
	"press", "focus", "increment", "decrement", "setValue", "scrollIntoView", "expand",
}

// names returns the names of the bits set in bits.
func names(bits uint64, of []string) []string {
	var out []string
	for i, name := range of {
		if bits&(1<<i) != 0 {
			out = append(out, name)
		}
	}
	return out
}

// actionKinds are the actions act takes, by name.
var actionKinds = map[string]platform.AccessActionKind{
	"press":          platform.AccessPress,
	"focus":          platform.AccessFocus,
	"increment":      platform.AccessIncrement,
	"decrement":      platform.AccessDecrement,
	"setValue":       platform.AccessSetValue,
	"scrollIntoView": platform.AccessScrollIntoView,
	"expand":         platform.AccessExpand,
	"collapse":       platform.AccessCollapse,
	"selectText":     platform.AccessSelectText,
	"insertText":     platform.AccessInsertText,
}

// inputEvent is an event of an input request.
type inputEvent struct {
	// Type is pointerMove, pointerDown, pointerUp, click, wheel, keyDown,
	// keyUp, key or text.
	Type string  `json:"type"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	// Button is left (the default), right or middle; Clicks counts quick
	// successive presses, 1 unless set.
	Button string `json:"button"`
	Clicks int    `json:"clicks"`
	// DX and DY are a wheel's distances in DIPs, Precise marks a
	// touchpad's.
	DX      float64 `json:"dx"`
	DY      float64 `json:"dy"`
	Precise bool    `json:"precise"`
	// Key is a key with its modifiers as menus' accelerators write them,
	// as "Enter" or "CmdOrCtrl+Shift+Z".
	Key string `json:"key"`
	// Mods are the modifiers held during a pointer or wheel event: shift,
	// ctrl, alt, super or cmdOrCtrl.
	Mods []string `json:"mods"`
	Text string   `json:"text"`
}

// surfaceEvents returns the events of the surface that evs make, as the
// backend's would.
func surfaceEvents(evs []inputEvent) ([]platform.SurfaceEvent, error) {
	var out []platform.SurfaceEvent
	for i, e := range evs {
		mods, err := modifiers(e.Mods)
		if err != nil {
			return nil, errorf(codeInvalid, "event %d: %v", i, err)
		}
		button := 0
		switch e.Button {
		case "", "left":
		case "right":
			button = 1
		case "middle":
			button = 2
		default:
			return nil, errorf(codeInvalid, "event %d: no button %q: left, right or middle", i, e.Button)
		}
		clicks := max(e.Clicks, 1)
		at := platform.SurfaceEvent{X: e.X, Y: e.Y, Mods: mods}
		pointer := func(kind platform.SurfaceEventKind, clicks int) platform.SurfaceEvent {
			ev := at
			ev.Kind, ev.Button, ev.Clicks = kind, button, clicks
			return ev
		}
		switch e.Type {
		case "pointerMove":
			out = append(out, pointer(platform.PointerMove, 0))
		case "pointerDown":
			out = append(out, pointer(platform.PointerDown, clicks))
		case "pointerUp":
			out = append(out, pointer(platform.PointerUp, clicks))
		case "click":
			// A double click is two presses, the second counting 2.
			out = append(out, pointer(platform.PointerMove, 0))
			for n := 1; n <= clicks; n++ {
				out = append(out, pointer(platform.PointerDown, n), pointer(platform.PointerUp, n))
			}
		case "wheel":
			ev := pointer(platform.PointerScroll, 0)
			ev.DX, ev.DY, ev.Precise = e.DX, e.DY, e.Precise
			out = append(out, ev)
		case "keyDown", "keyUp", "key":
			k, mods, err := parseKey(e.Key)
			if err != nil {
				return nil, errorf(codeInvalid, "event %d: %v", i, err)
			}
			ev := platform.SurfaceEvent{Key: k, PhysicalKey: k, Mods: mods}
			if e.Type != "keyUp" {
				ev.Kind = platform.KeyPressed
				out = append(out, ev)
			}
			if e.Type != "keyDown" {
				ev.Kind = platform.KeyReleased
				out = append(out, ev)
			}
		case "text":
			if e.Text == "" {
				return nil, errorf(codeInvalid, "event %d: text has no text", i)
			}
			out = append(out, platform.SurfaceEvent{Kind: platform.TextInput, Text: e.Text})
		default:
			return nil, errorf(codeInvalid, "event %d: no event type %q", i, e.Type)
		}
	}
	return out, nil
}

// modifiers returns the modifiers of names.
func modifiers(list []string) (platform.Modifiers, error) {
	var mods platform.Modifiers
	for _, name := range list {
		switch strings.ToLower(name) {
		case "shift":
			mods |= platform.ModShift
		case "ctrl", "control":
			mods |= platform.ModCtrl
		case "alt", "option":
			mods |= platform.ModAlt
		case "super", "cmd", "command", "meta":
			mods |= platform.ModSuper
		case "cmdorctrl":
			if runtime.GOOS == "darwin" {
				mods |= platform.ModSuper
			} else {
				mods |= platform.ModCtrl
			}
		default:
			return 0, fmt.Errorf("no modifier %q", name)
		}
	}
	return mods, nil
}

// namedKeys are the keys of accelerators that have a key of native UI,
// by accelerator.Accelerator's names.
var namedKeys = map[string]platform.Key{
	"Enter": platform.KeyEnter, "Escape": platform.KeyEscape, "Backspace": platform.KeyBackspace,
	"Tab": platform.KeyTab, "Space": platform.KeySpace, "Delete": platform.KeyDelete,
	"Insert": platform.KeyInsert, "Home": platform.KeyHome, "End": platform.KeyEnd,
	"PageUp": platform.KeyPageUp, "PageDown": platform.KeyPageDown, "Left": platform.KeyLeft,
	"Right": platform.KeyRight, "Up": platform.KeyUp, "Down": platform.KeyDown,
}

// parseKey returns the key and the modifiers of an accelerator.
func parseKey(s string) (platform.Key, platform.Modifiers, error) {
	a, err := accelerator.Parse(s, runtime.GOOS)
	if err != nil {
		return 0, 0, err
	}
	var mods platform.Modifiers
	for _, m := range []struct {
		a accelerator.Modifiers
		p platform.Modifiers
	}{{accelerator.Super, platform.ModSuper}, {accelerator.Ctrl, platform.ModCtrl}, {accelerator.Alt, platform.ModAlt}, {accelerator.Shift, platform.ModShift}} {
		if a.Has(m.a) {
			mods |= m.p
		}
	}
	k := namedKeys[a.Key]
	if r := []rune(a.Key); len(r) == 1 {
		k = platform.KeyForRune(r[0])
	} else if n, err := strconv.Atoi(strings.TrimPrefix(a.Key, "F")); err == nil && a.Key[0] == 'F' && n >= 1 && n <= 12 {
		k = platform.KeyF1 + platform.Key(n-1)
	}
	if k == platform.KeyUnknown {
		return 0, 0, fmt.Errorf("native UI has no key %q", a.Key)
	}
	return k, mods, nil
}
