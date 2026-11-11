//go:build darwin

package darwin

import (
	"github.com/ebitengine/purego/objc"

	"github.com/egoist/mygo/internal/platform"
)

// A flyout with Flyout.Popover shows its web view or surface in an
// NSPopover, which draws the system's material and arrow, and animates.
// AppKit places it against the anchor: the arrow points at the anchor's
// middle, on the side the flyout asks for, which AppKit flips where the
// screen has no room. The popover makes its window as it shows: w.win is
// that window while it shows. AppKit gives a popover that shows the
// keyboard, focusable or not, while its parent stays the key window, which
// the popover's window says it is too. A focusable flyout's popover is
// transient: it asks the delegate (popoverShouldClose:) before it closes on
// a click elsewhere or on its anchor, as WindowHandler.ShouldClose decides.

const (
	popoverBehaviorApplicationDefined = 0
	popoverBehaviorTransient          = 1
)

// holdPopover keeps a transient popover open as a window is modal to it,
// as an application-defined one, until it hides: refusing AppKit's close
// instead left it open for good, as AppKit then no longer closed it on
// clicks elsewhere, and ate the clicks on its tray icon (#1055, as #1050).
// Making it transient again once it shows does not have AppKit close it
// on clicks elsewhere either: held (popoverHeld), the clicks elsewhere
// close it as they close other focusable flyouts (closeHeldPopovers).
func (w *window) holdPopover() {
	if w.popover == 0 || w.closed || !w.flyout.Focusable || w.popoverHeld {
		return
	}
	w.popoverHeld = true
	send(w.popover, "setBehavior:", popoverBehaviorApplicationDefined)
	w.b.watchOutsideClicks()
}

// closeHeldPopovers closes the popovers held open (holdPopover) that no
// window is modal to anymore, for a press in another window, in the app
// (win), or outside it (0), unless on a popover's tray icon, whose click
// toggles it.
func (b *Backend) closeHeldPopovers(win id) {
	var open []*window
	for _, w := range b.byNSWindow {
		if !w.popoverHeld || w.closed || w.win == win || !sendBool(w.popover, "isShown") || w.modalChild() != nil {
			continue
		}
		if t, ok := w.flyout.Tray.(*tray); ok && (t.pressed() || win != 0 && t.button != 0 && send(t.button, "window") == win) {
			continue
		}
		open = append(open, w)
	}
	for _, w := range open {
		if !w.closed && w.h.ShouldClose() {
			w.Close()
		}
	}
}

func (w *window) createPopover(content NSRect) {
	w.delegate = alloc("MyGoWindowDelegate")
	w.createContent(content)
	send(w.view, "setAutoresizingMask:", nsViewWidthHeightSizable)
	w.popoverController = alloc("NSViewController")
	send(w.popoverController, "setView:", uintptr(w.view))
	w.popover = alloc("NSPopover")
	send(w.popover, "setContentViewController:", uintptr(w.popoverController))
	msgSetSize(w.popover, sel("setContentSize:"), content.Size)
	behavior := uintptr(popoverBehaviorApplicationDefined)
	if w.flyout.Focusable {
		behavior = popoverBehaviorTransient
	}
	send(w.popover, "setBehavior:", behavior)
	send(w.popover, "setDelegate:", uintptr(w.delegate))
	w.register()
}

// popoverAnchor returns the view the popover shows from, the anchor in it
// and the edge of it the popover goes to: the parent's content view, which
// is not flipped, or the button of a tray's status item. view is 0 when
// there is none.
func (w *window) popoverAnchor() (view id, r NSRect, edge uint) {
	// NSRectEdge
	edge = map[platform.Side]uint{platform.SideBottom: 1, platform.SideTop: 3, platform.SideRight: 2, platform.SideLeft: 0}[w.flyout.Side]
	if t, ok := w.flyout.Tray.(*tray); ok {
		if t.item == 0 {
			return 0, r, edge
		}
		return t.button, msgRect(t.button, sel("bounds")), edge
	}
	a, p := w.flyout.Anchor, w.parent
	if p == nil || p.closed || p.view == 0 {
		return 0, r, edge
	}
	h := msgRect(p.view, sel("bounds")).Size.Height
	r = NSRect{
		Origin: NSPoint{float64(a.X), h - float64(a.Y) - float64(max(a.Height, 1))},
		Size:   NSSize{float64(max(a.Width, 1)), float64(max(a.Height, 1))},
	}
	return p.view, r, edge
}

func (w *window) showPopover() {
	view, r, edge := w.popoverAnchor()
	if view == 0 || sendBool(w.popover, "isShown") {
		return
	}
	w.popoverClosing = false
	if w.popoverHeld {
		w.popoverHeld = false
		send(w.popover, "setBehavior:", popoverBehaviorTransient)
	}
	if w.flyout.Tray != nil {
		// The popover of a menu bar app takes the keyboard of an app the
		// user did not activate, as AppKit's own status items' do.
		send(w.b.app, "activateIgnoringOtherApps:", 1)
	}
	msgSetSize(w.popover, sel("setContentSize:"), NSSize{float64(w.flyoutSize.Width), float64(w.flyoutSize.Height)})
	msgRectIDUint(w.popover, sel("showRelativeToRect:ofView:preferredEdge:"), r, view, edge)
	w.adoptPopoverWindow()
	w.highlightTray(true)
}

// highlightTray highlights the tray icon a popover shows from while it
// shows, as AppKit's status items are while their menus show.
func (w *window) highlightTray(on bool) {
	if t, ok := w.flyout.Tray.(*tray); ok && t.item != 0 {
		send(t.button, "highlight:", boolArg(on))
	}
}

// adoptPopoverWindow makes the window the popover shows in the flyout's.
func (w *window) adoptPopoverWindow() {
	win := send(w.view, "window")
	if win == 0 || win == w.win {
		return
	}
	w.forgetPopoverWindow()
	w.win = win
	w.b.byNSWindow[win] = w
	if w.surface != nil {
		send(win, "makeFirstResponder:", uintptr(w.surface.view))
	}
}

func (w *window) forgetPopoverWindow() {
	if w.win == 0 {
		return
	}
	delete(w.b.byNSWindow, w.win)
	w.win = 0
}

// placePopover moves a popover that shows to the anchor, at the size.
func (w *window) placePopover() {
	if !sendBool(w.popover, "isShown") {
		return
	}
	msgSetSize(w.popover, sel("setContentSize:"), NSSize{float64(w.flyoutSize.Width), float64(w.flyoutSize.Height)})
	if _, r, _ := w.popoverAnchor(); w.flyout.Tray == nil {
		msgSetRect(w.popover, sel("setPositioningRect:"), r)
	}
}

// hidePopover closes the popover, which shows again with Show, and gives
// the keyboard it had back to the parent.
func (w *window) hidePopover() {
	if !sendBool(w.popover, "isShown") {
		return
	}
	key := w.win != 0 && sendBool(w.win, "isKeyWindow")
	w.popoverClosing = true
	send(w.popover, "close")
	if p := w.parent; key && p != nil && !p.closed && sendBool(p.win, "isVisible") {
		send(p.win, "makeKeyWindow")
	}
}

// closePopover destroys the flyout, at once.
func (w *window) closePopover() {
	send(w.popover, "setAnimates:", 0)
	w.hidePopover()
	w.cleanup()
	w.h.Closed()
}

func (w *window) cleanupPopover() {
	w.forgetPopoverWindow()
	send(w.popover, "setDelegate:", 0)
	if sendBool(w.popover, "isShown") {
		w.highlightTray(false)
		send(w.popover, "setAnimates:", 0)
		send(w.popover, "close")
	}
	send(w.popover, "setContentViewController:", 0)
	release(w.popover)
	release(w.popoverController)
}

func popoverDelegateMethods() []objc.MethodDef {
	b := func() *Backend { return theBackend }
	return []objc.MethodDef{
		// The user clicked elsewhere, in a transient popover, or on its
		// tray icon, the positioning view, whose click AppKit eats: the
		// icon's action never comes, so the click only closes it. AppKit
		// asks on the release (macOS 26) or on the press, and keeping it
		// open on a press on the icon kept it open for good, as it then no
		// longer closed on clicks elsewhere either (#1050).
		method("popoverShouldClose:", func(self id, _ objc.SEL, popover id) bool {
			if w := b().windowFor(self); w != nil {
				return w.h.ShouldClose()
			}
			return true
		}),
		method("popoverDidClose:", func(self id, _ objc.SEL, n id) {
			w := b().windowFor(self)
			if w == nil {
				return
			}
			w.highlightTray(false)
			w.forgetPopoverWindow()
			if w.popoverClosing {
				w.popoverClosing = false
				return
			}
			w.cleanup()
			w.h.Closed()
		}),
	}
}

// pressed reports whether the user is pressing the icon's button: the
// event being handled is a press on it, or a mouse button is down with the
// pointer over it, or over the copy of it the user last clicked (frame),
// as when the app hears of the press only after it made the app resign,
// or in a monitor of other apps' events, which the menu bar's are on
// recent macOS.
func (t *tray) pressed() bool {
	if t.item == 0 {
		return false
	}
	win := send(t.button, "window")
	if win == 0 {
		return false
	}
	if ev := send(t.b.app, "currentEvent"); ev != 0 && send(ev, "window") == win {
		switch sendInt(ev, "type") {
		case 1, 3: // NSEventTypeLeftMouseDown, RightMouseDown
			p := msgPointFromView(t.button, sel("convertPoint:fromView:"), msgPoint(ev, sel("locationInWindow")), 0)
			return inRect(p, msgRect(t.button, sel("bounds")))
		}
	}
	if send(class("NSEvent"), "pressedMouseButtons") == 0 {
		return false
	}
	return inRect(msgPoint(class("NSEvent"), sel("mouseLocation")), t.frame())
}

// inRect reports whether p is in r, its far edges excluded.
func inRect(p NSPoint, r NSRect) bool {
	return p.X >= r.Origin.X && p.Y >= r.Origin.Y && p.X < r.Origin.X+r.Size.Width && p.Y < r.Origin.Y+r.Size.Height
}
