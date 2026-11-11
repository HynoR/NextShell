package mygo

import "slices"

// A window is drawn as active while the user's input goes to it, as native
// apps tell which of their windows has it: macOS fades a sidebar's
// material and the accent of selections and controls to gray in the
// others, Windows draws their Mica as a solid fill, GTK styles them
// :backdrop. A window is active while it has the keyboard, or one of its
// flyouts does (a menu or popover of a window belongs to it), and always
// when SetActiveAppearance keeps it so. Panels and flyouts without a
// parent float over whatever app is in front, never active as the system
// tells it, and are drawn active as their materials are. A window that
// never had the keyboard, and was not shown without it, counts as active,
// so that the first frame of a window about to take it does not show it
// otherwise.
//
// Its native UI reads it with ui.Context.WindowActive, and its pages from
// the attribute data-mygo-window-inactive of their <html>, which the bridge
// sets while it is not. The system's materials follow the system's own
// state, which agrees: macOS keeps a window whose flyout has the keyboard
// drawn as its main window, and Windows its owner (flyoutActivate).

// activeScheduled tells that updateActive is posted. Main thread only.
var activeScheduled bool

// scheduleActive has the windows' active states worked out once the
// event being handled returns: a window's flyout taking the keyboard
// blurs the window first, which would draw it inactive for a frame. Main
// thread only.
func scheduleActive() {
	if !activeScheduled {
		activeScheduled = true
		postMain(updateActive)
	}
}

// updateActive works out which windows are drawn as active, and tells
// those that changed. Main thread only.
func updateActive() {
	activeScheduled = false
	windows.Lock()
	list, focused := slices.Clone(windows.list), windows.focused
	windows.Unlock()
	for _, w := range list {
		if w.native != nil {
			w.setActive(w.drawnActive(focused))
		}
	}
}

// drawnActive reports whether w is drawn as active while focused has the
// keyboard. Main thread only.
func (w *Window) drawnActive(focused *Window) bool {
	switch {
	case w.keepActive:
		return true
	case w.flyout != nil && w.parent != nil:
		return w.parent.drawnActive(focused)
	case w.flyout != nil, w.panel, !w.focusKnown:
		return true
	}
	for x := focused; x != nil; x = x.parent {
		if x == w {
			return true
		}
		if x.flyout == nil {
			break
		}
	}
	return false
}

// setActive draws w as active or not: its native UI, or its page and those
// of its web views. Main thread only.
func (w *Window) setActive(active bool) {
	if w.active == active {
		return
	}
	w.active = active
	if c := w.conn; c != nil && c.ActiveChanged != nil {
		c.ActiveChanged()
	}
	w.sendActive()
	windows.Lock()
	views := slices.Clone(w.webViews)
	windows.Unlock()
	for _, v := range views {
		v.sendActive()
	}
}
