//go:build darwin

package darwin

import (
	"fmt"
	"image"
	"math"
	"os"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/transfer"
)

// The functions in this file drive native UI the way a user would, for the
// GUI tests in internal/e2e. They must run on the main thread.

// TestPerformMenuItem activates an item of the application menu, found by
// the titles along its path, e.g. TestPerformMenuItem("File", "New").
// plugins/automation's menu request chooses items with it too.
func TestPerformMenuItem(path ...string) error {
	var err error
	withPool(func() {
		var menu id
		var index int
		if menu, index, err = menuItemAt(path); err == nil {
			send(menu, "performActionForItemAtIndex:", uintptr(index))
		}
	})
	return err
}

// TestMenuItemEnabled reports whether an item of the application menu,
// found as TestPerformMenuItem finds it, is enabled once its menu
// validates its items, as before it opens.
func TestMenuItemEnabled(path ...string) (enabled bool, err error) {
	withPool(func() {
		var menu id
		var index int
		if menu, index, err = menuItemAt(path); err == nil {
			send(menu, "update")
			enabled = sendBool(send(menu, "itemAtIndex:", uintptr(index)), "isEnabled")
		}
	})
	return enabled, err
}

// TestMenuItemSubtitle returns the title and the subtitle of an item of the
// application menu, found by its title as TestPerformMenuItem finds it.
func TestMenuItemSubtitle(path ...string) (title, subtitle string, err error) {
	withPool(func() {
		var menu id
		var index int
		if menu, index, err = menuItemAt(path); err == nil {
			item := send(menu, "itemAtIndex:", uintptr(index))
			title = goString(send(item, "title"))
			if respondsTo(item, "subtitle") {
				subtitle = goString(send(item, "subtitle"))
			}
		}
	})
	return title, subtitle, err
}

// menuItemAt finds an item of the application menu by the titles along
// its path, and returns its menu and its index there.
func menuItemAt(path []string) (menu id, index int, err error) {
	menu = send(theBackend.app, "mainMenu")
	for i, title := range path {
		if menu == 0 {
			return 0, 0, fmt.Errorf("mygo: menu %q has no submenu", path[i-1])
		}
		index = sendInt(menu, "indexOfItemWithTitle:", uintptr(nsString(title)))
		if index < 0 {
			return 0, 0, fmt.Errorf("mygo: no menu item %q", title)
		}
		if i == len(path)-1 {
			return menu, index, nil
		}
		menu = send(send(menu, "itemAtIndex:", uintptr(index)), "submenu")
	}
	return 0, 0, fmt.Errorf("mygo: no menu item")
}

// TestPerformKeyEquivalent sends a key press with the Command key (and
// shift when requested) to the application menu and reports whether an item
// handled it.
func TestPerformKeyEquivalent(key string, shift bool) bool {
	handled := false
	withPool(func() {
		flags := uint(1 << 20)
		if shift {
			flags |= 1 << 17
		}
		ev := msgKeyEvent(class("NSEvent"), sel("keyEventWithType:location:modifierFlags:timestamp:windowNumber:context:characters:charactersIgnoringModifiers:isARepeat:keyCode:"),
			10 /* NSEventTypeKeyDown */, NSPoint{}, flags, 0, 0, 0, nsString(key), nsString(key), false, 0)
		handled = sendBool(send(theBackend.app, "mainMenu"), "performKeyEquivalent:", uintptr(ev))
	})
	return handled
}

// TestOnScreen reports whether some of a window shows on screen, as the
// window server reports once it put the window there.
func TestOnScreen(handle uintptr) bool {
	const occlusionStateVisible = 1 << 1
	return sendInt(id(handle), "occlusionState")&occlusionStateVisible != 0
}

// TestWindowOrder returns the app's windows on screen, panels too, front
// to back.
func TestWindowOrder() []uintptr {
	var order []uintptr
	withPool(func() {
		for _, w := range theBackend.windowsFrontToBack() {
			order = append(order, uintptr(w))
		}
	})
	return order
}

// TestOnActiveSpace reports whether a window is on the Space that shows,
// a full-screen window's own when it is in front.
func TestOnActiveSpace(handle uintptr) bool {
	return sendBool(id(handle), "isOnActiveSpace")
}

// TestWindowLevel returns a window's level.
func TestWindowLevel(handle uintptr) int {
	return sendInt(id(handle), "level")
}

// TestEndSheet ends the sheet attached to a window as if its first button
// was clicked, and reports whether there was one.
func TestEndSheet(handle uintptr) bool {
	sheet := send(id(handle), "attachedSheet")
	if sheet == 0 {
		return false
	}
	send(id(handle), "endSheet:returnCode:", uintptr(sheet), nsAlertFirstButtonReturn)
	return true
}

// TestClick sends a left mouse down and up at a point of a window's content
// (top-left origin, in points), like a user click.
func TestClick(handle uintptr, x, y float64) { testClick(handle, x, y, false) }

// TestAppClick is TestClick through the application's event queue, where
// event monitors see the click as they see the user's, as those of
// transient popovers that close on clicks elsewhere. The application has
// handled the click when it returns (handlePosted).
func TestAppClick(handle uintptr, x, y float64) { testClick(handle, x, y, true) }

// TestHitView returns the class of the view a window hit-tests at (x, y)
// of its content, in DIPs from its top left.
func TestHitView(handle uintptr, x, y float64) (cls string) {
	w := theBackend.byNSWindow[id(handle)]
	if w == nil {
		return ""
	}
	withPool(func() {
		content := send(w.win, "contentView")
		b := msgRect(content, sel("bounds"))
		hit := msgHitTest(content, sel("hitTest:"), NSPoint{X: x, Y: b.Size.Height - y})
		if hit != 0 {
			cls = goString(send(hit, "className"))
		}
	})
	return cls
}

// TestAppPress is half of TestAppClick: the left mouse down, or the up,
// which a user's click handles apart, a frame or more later.
func TestAppPress(handle uintptr, x, y float64, down bool) {
	typ := uint(2) // NSEventTypeLeftMouseUp
	if down {
		typ = 1 // NSEventTypeLeftMouseDown
	}
	testMouse(handle, x, y, true, typ)
}

// TestPostMouse presses (down) or releases the primary button at (x, y)
// on screen, in MyGo's coordinates (top-left origin), as the user's mouse
// does: through the window server, to whatever shows there, the menu
// bar's items too. It reports false where the process may not post events
// (Accessibility).
func TestPostMouse(x, y float64, down bool) bool {
	testPostOnce.Do(func() {
		purego.RegisterLibFunc(&cgPreflightPostEventAccess, libCG, "CGPreflightPostEventAccess")
		purego.RegisterLibFunc(&cgEventCreateMouseEvent, libCG, "CGEventCreateMouseEvent")
		purego.RegisterLibFunc(&cgEventPost, libCG, "CGEventPost")
	})
	if !cgPreflightPostEventAccess() {
		return false
	}
	typ := uint32(2) // kCGEventLeftMouseUp
	if down {
		typ = 1 // kCGEventLeftMouseDown
	}
	ev := cgEventCreateMouseEvent(0, typ, NSPoint{x, y}, 0) // kCGMouseButtonLeft
	if ev == 0 {
		return false
	}
	cgEventPost(0, ev) // kCGHIDEventTap
	cfRelease(ev)
	return true
}

var (
	testPostOnce               sync.Once
	cgPreflightPostEventAccess func() bool
	cgEventCreateMouseEvent    func(source uintptr, typ uint32, at NSPoint, button uint32) uintptr
	cgEventPost                func(tap uint32, ev uintptr)
)

// TestMouseButtons returns how many presses and releases of the primary
// button the application took from the window server since the first
// call, which starts counting them: a test that posts them with
// TestPostMouse waits for them so.
func TestMouseButtons() (downs, ups int) {
	if testButtons == 0 {
		blk := newBlock(func(_ objc.Block, ev id) id {
			if sendInt(ev, "type") == 1 { // NSEventTypeLeftMouseDown
				testDowns++
			} else {
				testUps++
			}
			return ev
		})
		testButtons = retain(send(class("NSEvent"), "addLocalMonitorForEventsMatchingMask:handler:", 1<<1|1<<2, uintptr(blk)))
	}
	return testDowns, testUps
}

// testButtons is TestMouseButtons's monitor, counting into testDowns and
// testUps. Main thread only.
var (
	testButtons        id
	testDowns, testUps int
)

// TestClosePopover asks a flyout's popover to close as AppKit asks a
// transient one as the user presses outside it (performClose:), and
// reports whether it has one.
func TestClosePopover(handle uintptr) bool {
	w := theBackend.byNSWindow[id(handle)]
	if w == nil || w.popover == 0 {
		return false
	}
	send(w.popover, "performClose:", 0)
	return true
}

func testClick(handle uintptr, x, y float64, viaApp bool) {
	testMouse(handle, x, y, viaApp, 1, 2) // NSEventTypeLeftMouseDown, LeftMouseUp
}

func testMouse(handle uintptr, x, y float64, viaApp bool, types ...uint) {
	withPool(func() {
		win := id(handle)
		content := msgRect(send(win, "contentView"), sel("frame"))
		loc := NSPoint{x, content.Size.Height - y}
		number := sendInt(win, "windowNumber")
		for _, typ := range types {
			ev := msgMouseEvent(class("NSEvent"), sel("mouseEventWithType:location:modifierFlags:timestamp:windowNumber:context:eventNumber:clickCount:pressure:"),
				typ, loc, 0, 0, number, 0, 0, 1, 1)
			if viaApp {
				send(theBackend.app, "postEvent:atStart:", uintptr(ev), 0)
			} else {
				send(win, "sendEvent:", uintptr(ev))
			}
		}
	})
	if viaApp {
		handlePosted()
	}
}

// testHandledSubtype marks the application-defined event handlePosted
// posts after the events a test posted.
const testHandledSubtype = 0x6d74 // 'mt'

// testHandledMark tells handlePosted's events apart. Main thread only.
var testHandledMark int

// handlePosted handles the events posted to the application's queue, up to
// an event it posts after them, before it returns: it takes them from the
// queue and sends them as the run loop does, so that event monitors and
// transient popovers see them as they see the user's. A test that posts
// input so knows that the application handled it before it checks what
// the input did, rather than waiting a while. It gives up after 10
// seconds, which only a hung application would take.
func handlePosted() {
	testHandledMark++
	mark := testHandledMark
	withPool(func() {
		ev := msgOtherEvent(class("NSEvent"), sel("otherEventWithType:location:modifierFlags:timestamp:windowNumber:context:subtype:data1:data2:"),
			nsEventTypeApplicationDefined, NSPoint{}, 0, 0, 0, 0, testHandledSubtype, mark, 0)
		send(theBackend.app, "postEvent:atStart:", uintptr(ev), 0)
	})
	mode := nsString("kCFRunLoopDefaultMode")
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		done := false
		withPool(func() {
			until := msgDateSince(class("NSDate"), sel("dateWithTimeIntervalSinceNow:"), 0.5)
			ev := msgNextEvent(theBackend.app, sel("nextEventMatchingMask:untilDate:inMode:dequeue:"), ^uint64(0), until, mode, true)
			if ev == 0 {
				return
			}
			if sendInt(ev, "type") == nsEventTypeApplicationDefined && int16(sendInt(ev, "subtype")) == testHandledSubtype && sendInt(ev, "data1") == mark {
				done = true
				return
			}
			send(theBackend.app, "sendEvent:", uintptr(ev))
		})
		if done {
			return
		}
	}
}

// TestDrag presses the primary button at the first of points, in points
// from the top-left corner of a window's content, drags the pointer through
// the others, and releases it at the last.
func TestDrag(handle uintptr, points [][2]float64) {
	withPool(func() {
		win := id(handle)
		content := msgRect(send(win, "contentView"), sel("frame"))
		number := sendInt(win, "windowNumber")
		for i, p := range points {
			typ := uint(6) // NSEventTypeLeftMouseDragged
			switch i {
			case 0:
				typ = 1 // NSEventTypeLeftMouseDown
			case len(points) - 1:
				typ = 2 // NSEventTypeLeftMouseUp
			}
			ev := msgMouseEvent(class("NSEvent"), sel("mouseEventWithType:location:modifierFlags:timestamp:windowNumber:context:eventNumber:clickCount:pressure:"),
				typ, NSPoint{p[0], content.Size.Height - p[1]}, 0, 0, number, 0, 0, 1, 1)
			send(win, "sendEvent:", uintptr(ev))
		}
	})
}

// TestMouseStep is a press (Down), drag or release of the left button at
// a point of the screen, in MyGo's coordinates (top-left origin).
type TestMouseStep struct {
	Down, Up bool // neither is a drag
	X, Y     float64
}

// TestAppMouseAt posts steps of the left button in a window through the
// application's queue, where event monitors see them as they see the
// user's, and handles them before it returns. Each step is placed in the
// window where it is when the step is made, so a step keeps its place on
// the screen when the window moves.
func TestAppMouseAt(handle uintptr, steps ...TestMouseStep) {
	withPool(func() {
		win := id(handle)
		number := sendInt(win, "windowNumber")
		screen := primaryScreenHeight()
		for _, st := range steps {
			typ := uint(6) // NSEventTypeLeftMouseDragged
			switch {
			case st.Down:
				typ = 1 // NSEventTypeLeftMouseDown
			case st.Up:
				typ = 2 // NSEventTypeLeftMouseUp
			}
			f := msgRect(win, sel("frame"))
			loc := NSPoint{st.X - f.Origin.X, screen - st.Y - f.Origin.Y}
			ev := msgMouseEvent(class("NSEvent"), sel("mouseEventWithType:location:modifierFlags:timestamp:windowNumber:context:eventNumber:clickCount:pressure:"),
				typ, loc, 0, 0, number, 0, 0, 1, 1)
			send(theBackend.app, "postEvent:atStart:", uintptr(ev), 0)
		}
	})
	handlePosted()
}

// TestWindowDrag tells where the press stands in moving a window
// (StartDrag): "idle", "pressed", "native", "manual" or "server"; "idle"
// for a window no press is down in.
func TestWindowDrag(handle uintptr) string {
	if theDrag.win != id(handle) {
		return dragIdle.String()
	}
	return theDrag.phase.String()
}

// TestControlClick Control-clicks (x, y), in points from the top-left
// corner of a window's content.
func TestControlClick(handle uintptr, x, y float64) {
	withPool(func() {
		win := id(handle)
		content := msgRect(send(win, "contentView"), sel("frame"))
		loc := NSPoint{x, content.Size.Height - y}
		number := sendInt(win, "windowNumber")
		for _, typ := range []uint{1, 2} { // NSEventTypeLeftMouseDown, LeftMouseUp
			ev := msgMouseEvent(class("NSEvent"), sel("mouseEventWithType:location:modifierFlags:timestamp:windowNumber:context:eventNumber:clickCount:pressure:"),
				typ, loc, 1<<18, 0, number, 0, 0, 1, 1) // NSEventModifierFlagControl
			send(win, "sendEvent:", uintptr(ev))
		}
	})
}

// TestSecondaryClickAsPrimary clicks (x, y), in points from the top-left
// corner of a window's content, with the secondary button as data loss
// prevention agents post its events again: as the primary button's press
// and release, which keep the secondary button's number.
func TestSecondaryClickAsPrimary(handle uintptr, x, y float64) (ok bool) {
	w := theBackend.byNSWindow[id(handle)]
	if w == nil || w.surface == nil {
		return false
	}
	testSideOnce.Do(func() {
		purego.RegisterLibFunc(&cgEventSetIntegerValueField, libCG, "CGEventSetIntegerValueField")
	})
	testPinchOnce.Do(func() {
		purego.RegisterLibFunc(&cgEventSetType, libCG, "CGEventSetType")
		purego.RegisterLibFunc(&cgEventSetDoubleValueField, libCG, "CGEventSetDoubleValueField")
		purego.RegisterLibFunc(&cgEventSetLocation, libCG, "CGEventSetLocation")
	})
	withPool(func() {
		content := msgRect(send(w.win, "contentView"), sel("frame"))
		want := NSPoint{x, content.Size.Height - y}
		near := func(p NSPoint) bool { return math.Abs(p.X-want.X) < 0.5 && math.Abs(p.Y-want.Y) < 0.5 }
		number := sendInt(w.win, "windowNumber")
		for _, typ := range []uint{1, 2} { // NSEventTypeLeftMouseDown, LeftMouseUp
			ev := msgMouseEvent(class("NSEvent"), sel("mouseEventWithType:location:modifierFlags:timestamp:windowNumber:context:eventNumber:clickCount:pressure:"),
				typ, want, 0, 0, number, 0, 0, 1, 1)
			cg := uintptr(send(ev, "CGEvent"))
			cgEventSetIntegerValueField(cg, 3, 1) // kCGMouseEventButtonNumber: the secondary button
			ev = send(class("NSEvent"), "eventWithCGEvent:", cg)
			// The way through CGEvent moves the point by the window's origin
			// once more: move it back (CoreGraphics' y goes down).
			if got := msgPoint(ev, sel("locationInWindow")); ev != 0 && !near(got) {
				at := cgEventGetLocation(cg)
				cgEventSetLocation(cg, NSPoint{at.X - (got.X - want.X), at.Y + (got.Y - want.Y)})
				ev = send(class("NSEvent"), "eventWithCGEvent:", cg)
			}
			if ev == 0 || sendInt(ev, "type") != int(typ) || sendInt(ev, "buttonNumber") != 1 || !near(msgPoint(ev, sel("locationInWindow"))) {
				return
			}
			name := "mouseDown:"
			if typ == 2 {
				name = "mouseUp:"
			}
			send(w.surface.view, name, uintptr(ev))
		}
		ok = true
	})
	return ok
}

var (
	testSideOnce sync.Once
	// cgEventSetIntegerValueField is CGEventSetIntegerValueField.
	cgEventSetIntegerValueField func(event uintptr, field uint32, value int64)
)

// TestSideButton presses and releases a mouse's back or forward button
// (buttons 3 and 4) in a window showing native UI.
func TestSideButton(handle uintptr, back bool) bool {
	w := theBackend.byNSWindow[id(handle)]
	if w == nil || w.surface == nil {
		return false
	}
	testSideOnce.Do(func() {
		purego.RegisterLibFunc(&cgEventSetIntegerValueField, libCG, "CGEventSetIntegerValueField")
	})
	button := int64(4)
	if back {
		button = 3
	}
	withPool(func() {
		number := sendInt(w.win, "windowNumber")
		for _, typ := range []uint{25, 26} { // NSEventTypeOtherMouseDown, OtherMouseUp
			ev := msgMouseEvent(class("NSEvent"), sel("mouseEventWithType:location:modifierFlags:timestamp:windowNumber:context:eventNumber:clickCount:pressure:"),
				typ, NSPoint{10, 10}, 0, 0, number, 0, 0, 1, 1)
			// NSEvent makes other buttons the middle one: number it.
			cg := send(ev, "CGEvent")
			cgEventSetIntegerValueField(uintptr(cg), 3, button) // kCGMouseEventButtonNumber
			ev = send(class("NSEvent"), "eventWithCGEvent:", uintptr(cg))
			name := "otherMouseDown:"
			if typ == 26 {
				name = "otherMouseUp:"
			}
			send(w.surface.view, name, uintptr(ev))
		}
	})
	return true
}

var (
	testPinchOnce              sync.Once
	cgEventSetType             func(event uintptr, typ uint32)
	cgEventSetDoubleValueField func(event uintptr, field uint32, value float64)
	cgEventSetLocation         func(event uintptr, p NSPoint)
)

// TestPinch pinches a touchpad at a point of a window showing native UI
// (top-left origin, in points), growing the content by magnification, as
// a magnify event of macOS says it. It reports false when the event it
// makes is not one.
func TestPinch(handle uintptr, x, y, magnification float64) (ok bool) {
	w := theBackend.byNSWindow[id(handle)]
	if w == nil || w.surface == nil {
		return false
	}
	testSideOnce.Do(func() {
		purego.RegisterLibFunc(&cgEventSetIntegerValueField, libCG, "CGEventSetIntegerValueField")
	})
	testPinchOnce.Do(func() {
		purego.RegisterLibFunc(&cgEventSetType, libCG, "CGEventSetType")
		purego.RegisterLibFunc(&cgEventSetDoubleValueField, libCG, "CGEventSetDoubleValueField")
		purego.RegisterLibFunc(&cgEventSetLocation, libCG, "CGEventSetLocation")
	})
	withPool(func() {
		content := msgRect(send(w.win, "contentView"), sel("frame"))
		want := NSPoint{x, content.Size.Height - y}
		ev := msgMouseEvent(class("NSEvent"), sel("mouseEventWithType:location:modifierFlags:timestamp:windowNumber:context:eventNumber:clickCount:pressure:"),
			5, want, 0, 0, sendInt(w.win, "windowNumber"), 0, 0, 0, 0) // NSEventTypeMouseMoved
		// A gesture event of the window server, of the HID system's zoom
		// type, which AppKit makes a magnify event of.
		cg := uintptr(send(ev, "CGEvent"))
		cgEventSetType(cg, 29)                             // kCGSEventGesture
		cgEventSetIntegerValueField(cg, 110, 8)            // gesture type: kIOHIDEventTypeZoom
		cgEventSetDoubleValueField(cg, 113, magnification) // zoom value
		ev = send(class("NSEvent"), "eventWithCGEvent:", cg)
		if ev == 0 || sendInt(ev, "type") != 30 { // NSEventTypeMagnify
			return
		}
		// The way through CGEvent moves the point by the window's origin
		// once more: move it back (CoreGraphics' y goes down).
		near := func(p NSPoint) bool { return math.Abs(p.X-want.X) < 0.5 && math.Abs(p.Y-want.Y) < 0.5 }
		if got := msgPoint(ev, sel("locationInWindow")); !near(got) {
			at := cgEventGetLocation(cg)
			cgEventSetLocation(cg, NSPoint{at.X - (got.X - want.X), at.Y + (got.Y - want.Y)})
			ev = send(class("NSEvent"), "eventWithCGEvent:", cg)
			if ev == 0 || !near(msgPoint(ev, sel("locationInWindow"))) {
				return
			}
		}
		send(w.surface.view, "magnifyWithEvent:", uintptr(ev))
		ok = true
	})
	return ok
}

// TestSwipe swipes left (back) or right (forward) in a window showing
// native UI, as a touchpad swiping between pages and the side buttons of
// mice through their makers' software do. It reports false when the event
// it makes is not a swipe.
func TestSwipe(handle uintptr, back bool) (ok bool) {
	w := theBackend.byNSWindow[id(handle)]
	if w == nil || w.surface == nil {
		return false
	}
	testSideOnce.Do(func() {
		purego.RegisterLibFunc(&cgEventSetIntegerValueField, libCG, "CGEventSetIntegerValueField")
	})
	testPinchOnce.Do(func() {
		purego.RegisterLibFunc(&cgEventSetType, libCG, "CGEventSetType")
		purego.RegisterLibFunc(&cgEventSetDoubleValueField, libCG, "CGEventSetDoubleValueField")
		purego.RegisterLibFunc(&cgEventSetLocation, libCG, "CGEventSetLocation")
	})
	withPool(func() {
		content := msgRect(send(w.win, "contentView"), sel("frame"))
		ev := msgMouseEvent(class("NSEvent"), sel("mouseEventWithType:location:modifierFlags:timestamp:windowNumber:context:eventNumber:clickCount:pressure:"),
			5, NSPoint{10, content.Size.Height - 10}, 0, 0, sendInt(w.win, "windowNumber"), 0, 0, 0, 0) // NSEventTypeMouseMoved
		// A gesture event of the window server, of the HID system's swipe
		// type, which AppKit makes a swipe event of: the direction a mask
		// of kIOHIDSwipeLeft and kIOHIDSwipeRight.
		cg := uintptr(send(ev, "CGEvent"))
		direction := int64(1 << 3) // kIOHIDSwipeRight
		if back {
			direction = 1 << 2 // kIOHIDSwipeLeft
		}
		cgEventSetType(cg, 29)                          // kCGSEventGesture
		cgEventSetIntegerValueField(cg, 110, 16)        // gesture type: kIOHIDEventTypeSwipe
		cgEventSetIntegerValueField(cg, 115, direction) // swipe direction
		ev = send(class("NSEvent"), "eventWithCGEvent:", cg)
		if ev == 0 || sendInt(ev, "type") != 31 { // NSEventTypeSwipe
			return
		}
		if got, _ := swipeBack(msgFloat(ev, sel("deltaX"))); got != back {
			return
		}
		send(w.surface.view, "swipeWithEvent:", uintptr(ev))
		ok = true
	})
	return ok
}

// TestTrafficLights returns where the top-left corner of a window's close
// button is, in points from the top-left corner of the window.
func TestTrafficLights(handle uintptr) (x, y float64) {
	win := id(handle)
	btn := send(win, "standardWindowButton:", 0)
	f := msgRect(btn, sel("frame"))
	parent := send(btn, "superview")
	a := msgPointFromView(parent, sel("convertPoint:toView:"), f.Origin, 0)
	b := msgPointFromView(parent, sel("convertPoint:toView:"), NSPoint{f.Origin.X + f.Size.Width, f.Origin.Y + f.Size.Height}, 0)
	return math.Min(a.X, b.X), msgRect(win, sel("frame")).Size.Height - math.Max(a.Y, b.Y)
}

// TestSetWindowAppearance gives a window its own dark or light appearance,
// apart from the application's.
func TestSetWindowAppearance(handle uintptr, dark bool) {
	name := "NSAppearanceNameAqua"
	if dark {
		name = "NSAppearanceNameDarkAqua"
	}
	withPool(func() {
		send(id(handle), "setAppearance:", uintptr(send(class("NSAppearance"), "appearanceNamed:", uintptr(nsString(name)))))
	})
}

// TestWebViewAttached reports whether a window's web view is in its view
// hierarchy.
func TestWebViewAttached(handle uintptr) bool {
	w := theBackend.byNSWindow[id(handle)]
	return w != nil && send(w.web, "window") == w.win
}

// TestTabFocusesLinks reports whether Tab reaches links and form controls
// that are not text fields in a window's page.
func TestTabFocusesLinks(handle uintptr) bool {
	w := theBackend.byNSWindow[id(handle)]
	return w != nil && w.web != 0 && sendBool(send(send(w.web, "configuration"), "preferences"), "tabFocusesLinks")
}

// TestBackgroundThrottlingOff reports whether the page of a window, or of
// its first web view, keeps running while it can't be seen: every one of
// throttlingPrefs WebKit has at its value for that, and the web view's
// window occlusion detection off where WebKit has it; and how many of
// those settings WebKit has.
func TestBackgroundThrottlingOff(handle uintptr, webView bool) (off bool, settings int) {
	w := theBackend.byNSWindow[id(handle)]
	if w != nil && webView {
		if len(w.webViews) == 0 {
			return false, 0
		}
		w = w.webViews[0]
	}
	if w == nil || w.web == 0 {
		return false, 0
	}
	off = true
	withPool(func() {
		prefs := send(send(w.web, "configuration"), "preferences")
		for _, p := range throttlingPrefs {
			if respondsTo(prefs, p.get) {
				settings++
				v := uintptr(send(prefs, p.get))
				if p.boolean {
					v = boolArg(byte(v) != 0)
				}
				off = off && v == p.off
			}
		}
		if respondsTo(w.web, "_windowOcclusionDetectionEnabled") {
			settings++
			off = off && !sendBool(w.web, "_windowOcclusionDetectionEnabled")
		}
	})
	return off, settings
}

// TestMockCaptureDevices gives a window's page WebKit's mock camera and
// microphone, which getUserMedia() captures without devices or the
// system's permission (private -[WKPreferences
// _setMockCaptureDevicesEnabled:]), even while the page is not focused
// (_setGetUserMediaRequiresFocus:), for the pages it loads from then on;
// false when WebKit doesn't answer them.
func TestMockCaptureDevices(handle uintptr) bool {
	w := theBackend.byNSWindow[id(handle)]
	if w == nil || w.web == 0 {
		return false
	}
	prefs := send(send(w.web, "configuration"), "preferences")
	if !respondsTo(prefs, "_setMockCaptureDevicesEnabled:") {
		return false
	}
	send(prefs, "_setMockCaptureDevicesEnabled:", boolArg(true))
	if respondsTo(prefs, "_setMediaDevicesEnabled:") {
		send(prefs, "_setMediaDevicesEnabled:", boolArg(true))
	}
	if respondsTo(prefs, "_setMockCaptureDevicesPromptEnabled:") {
		send(prefs, "_setMockCaptureDevicesPromptEnabled:", boolArg(false))
	}
	if respondsTo(prefs, "_setGetUserMediaRequiresFocus:") {
		send(prefs, "_setGetUserMediaRequiresFocus:", boolArg(false))
	}
	return true
}

// TestAllowsPictureInPicture reports whether a window's page lets its
// videos go into picture in picture, and false for supported when WebKit
// doesn't answer its private _allowsPictureInPictureMediaPlayback.
func TestAllowsPictureInPicture(handle uintptr) (allowed, supported bool) {
	w := theBackend.byNSWindow[id(handle)]
	if w == nil || w.web == 0 {
		return false, false
	}
	prefs := send(send(w.web, "configuration"), "preferences")
	if !respondsTo(prefs, "_allowsPictureInPictureMediaPlayback") {
		return false, false
	}
	return sendBool(prefs, "_allowsPictureInPictureMediaPlayback"), true
}

// TestPageGestures reports whether a swipe navigates and a pinch magnifies
// the page of a window, or with webView 0 or more, of its web view of that
// index, as WKWebView's properties say. ok is false without that page.
func TestPageGestures(handle uintptr, webView int) (swipe, pinch, ok bool) {
	w := theBackend.byNSWindow[id(handle)]
	if w != nil && webView >= 0 {
		if webView >= len(w.webViews) {
			return false, false, false
		}
		w = w.webViews[webView]
	}
	if w == nil || w.web == 0 {
		return false, false, false
	}
	return sendBool(w.web, "allowsBackForwardNavigationGestures"), sendBool(w.web, "allowsMagnification"), true
}

// TestActivate brings the application in front, as clicking its Dock icon
// does.
func TestActivate() {
	withPool(func() { send(theBackend.app, "activateIgnoringOtherApps:", 1) })
}

// TestIsFrontmost reports whether the application is the frontmost one,
// which NSApp's isActive does not tell while a nonactivating panel is the
// key window: it reports the app active then.
func TestIsFrontmost() bool {
	var front bool
	withPool(func() {
		app := send(send(class("NSWorkspace"), "sharedWorkspace"), "frontmostApplication")
		front = app != 0 && sendInt(app, "processIdentifier") == os.Getpid()
	})
	return front
}

// TestCollectionBehavior returns a window's NSWindowCollectionBehavior.
func TestCollectionBehavior(handle uintptr) uint {
	return uint(sendInt(id(handle), "collectionBehavior"))
}

// TestMaterial returns the class of a window's material (Vibrancy), its
// style when glass, the corners its content view is cut to, and whether
// the window draws as active.
func TestMaterial(handle uintptr) (class string, style int, radius float64, active bool) {
	w := theBackend.byNSWindow[id(handle)]
	if w == nil {
		return "", 0, 0, false
	}
	if w.effect != 0 {
		class = goString(send(w.effect, "className"))
		if w.glass {
			style = sendInt(w.effect, "style")
		}
	}
	if layer := send(w.view, "layer"); layer != 0 {
		radius = msgFloat(layer, sel("cornerRadius"))
	}
	if respondsTo(w.win, "_hasActiveAppearance") {
		active = sendBool(w.win, "_hasActiveAppearance")
	}
	return class, style, radius, active
}

// TestMaterialState returns the NSVisualEffectState of a window's
// material, -1 for none or glass.
func TestMaterialState(handle uintptr) int {
	w := theBackend.byNSWindow[id(handle)]
	if w == nil || w.effect == 0 || w.glass {
		return -1
	}
	return sendInt(w.effect, "state")
}

// TestGlassTint returns the glass's tint, in sRGB, and 0, 0, 0, 0 when it
// has none or the window has no glass.
func TestGlassTint(handle uintptr) (red, green, blue, alpha float64) {
	w := theBackend.byNSWindow[id(handle)]
	if w == nil || w.effect == 0 || !w.glass {
		return 0, 0, 0, 0
	}
	withPool(func() {
		c := send(w.effect, "tintColor")
		if c == 0 {
			return
		}
		c = send(c, "colorUsingColorSpace:", uintptr(send(class("NSColorSpace"), "sRGBColorSpace")))
		if c == 0 {
			return
		}
		red, green, blue, alpha = msgFloat(c, sel("redComponent")), msgFloat(c, sel("greenComponent")),
			msgFloat(c, sel("blueComponent")), msgFloat(c, sel("alphaComponent"))
	})
	return red, green, blue, alpha
}

// TestNativeGlassSupported reports whether windows show the system's
// glass: the system has Liquid Glass and does not reduce transparency, as
// GitHub's macOS runners do.
func TestNativeGlassSupported() (ok bool) {
	if !hasClass("NSGlassEffectView") {
		return false
	}
	withPool(func() { ok = !sendBool(workspace(), "accessibilityDisplayShouldReduceTransparency") })
	return ok
}

// TestNativeGlass returns the first system's glass view of a window's
// native content: its class, frame in its view's coordinates, and style.
func TestNativeGlass(handle uintptr) (class string, frame [4]float64, style int, ok bool) {
	w := theBackend.byNSWindow[id(handle)]
	if w == nil || w.surface == nil {
		return "", frame, 0, false
	}
	for _, v := range w.surface.glass {
		r := msgRect(v, sel("frame"))
		return goString(send(v, "className")), [4]float64{r.Origin.X, r.Origin.Y, r.Size.Width, r.Size.Height}, sendInt(v, "style"), true
	}
	return "", frame, 0, false
}

// TestNativeGlassTint returns the first system's glass view of a window's
// native content tint, in sRGB.
func TestNativeGlassTint(handle uintptr) (red, green, blue, alpha float64) {
	w := theBackend.byNSWindow[id(handle)]
	if w == nil || w.surface == nil {
		return 0, 0, 0, 0
	}
	for _, v := range w.surface.glass {
		withPool(func() {
			c := send(v, "tintColor")
			if c == 0 {
				return
			}
			c = send(c, "colorUsingColorSpace:", uintptr(send(class("NSColorSpace"), "sRGBColorSpace")))
			if c == 0 {
				return
			}
			red, green, blue, alpha = msgFloat(c, sel("redComponent")), msgFloat(c, sel("greenComponent")),
				msgFloat(c, sel("blueComponent")), msgFloat(c, sel("alphaComponent"))
		})
		return red, green, blue, alpha
	}
	return 0, 0, 0, 0
}

// TestRoundedBacking reports whether a window is opaque, and the alpha and
// red, in sRGB, of its content view's background, which fills its rounded
// corners' inside.
func TestRoundedBacking(handle uintptr) (opaque bool, alpha, red float64) {
	w := theBackend.byNSWindow[id(handle)]
	if w == nil {
		return false, 0, 0
	}
	opaque = sendBool(w.win, "isOpaque")
	withPool(func() {
		if layer := send(w.view, "layer"); layer != 0 {
			if c := send(layer, "backgroundColor"); c != 0 {
				color := send(send(class("NSColor"), "colorWithCGColor:", uintptr(c)), "colorUsingColorSpace:", uintptr(send(class("NSColorSpace"), "sRGBColorSpace")))
				if color != 0 {
					alpha = msgFloat(color, sel("alphaComponent"))
					red = msgFloat(color, sel("redComponent"))
				}
			}
		}
	})
	return opaque, alpha, red
}

// TestWindowBacking reports whether a window is opaque, and the alpha of
// its background color, in sRGB.
func TestWindowBacking(handle uintptr) (opaque bool, alpha float64) {
	w := theBackend.byNSWindow[id(handle)]
	if w == nil {
		return false, 0
	}
	opaque = sendBool(w.win, "isOpaque")
	withPool(func() {
		c := send(send(w.win, "backgroundColor"), "colorUsingColorSpace:", uintptr(send(class("NSColorSpace"), "sRGBColorSpace")))
		if c != 0 {
			alpha = msgFloat(c, sel("alphaComponent"))
		}
	})
	return opaque, alpha
}

// TestDockInspector opens the web inspector of a window docked to it, and
// reports whether the window has one.
func TestDockInspector(handle uintptr) bool {
	w := theBackend.byNSWindow[id(handle)]
	if w == nil {
		return false
	}
	in := w.inspector()
	if in == 0 {
		return false
	}
	send(in, "show")
	send(in, "attach")
	return true
}

// TestInspectorPlace reports where the docked inspector of a window is:
// "content" in its content view, "frame" elsewhere in the window, "" when
// the window has none.
func TestInspectorPlace(handle uintptr) string {
	w := theBackend.byNSWindow[id(handle)]
	if w == nil {
		return ""
	}
	var find func(view id) id
	find = func(view id) id {
		if view != w.web && sendBool(view, "isKindOfClass:", uintptr(class("WKWebView"))) {
			return view
		}
		for _, sub := range arrayItems(send(view, "subviews")) {
			if found := find(sub); found != 0 {
				return found
			}
		}
		return 0
	}
	content := send(w.win, "contentView")
	switch in := find(send(content, "superview")); {
	case in == 0:
		return ""
	case sendBool(in, "isDescendantOf:", uintptr(content)):
		return "content"
	default:
		return "frame"
	}
}

// TestSetDroppedFiles makes paths the files of the next drop on a window's
// page, as if they had been dragged there.
func TestSetDroppedFiles(handle uintptr, paths []string) {
	if w := theBackend.byNSWindow[id(handle)]; w != nil {
		w.dropped = paths
	}
}

// TestDockTileImage renders the content of the Dock tile offscreen, as the
// Dock does, to PNG; nil when the tile shows the plain application icon.
func TestDockTileImage() []byte {
	var data []byte
	withPool(func() {
		view := send(send(theBackend.app, "dockTile"), "contentView")
		if view == 0 {
			return
		}
		bounds := msgRect(view, sel("bounds"))
		rep := msgInitRect(view, sel("bitmapImageRepForCachingDisplayInRect:"), bounds)
		msgInitRectID(view, sel("cacheDisplayInRect:toBitmapImageRep:"), bounds, rep)
		data = goBytes(send(rep, "representationUsingType:properties:", 4, uintptr(send(class("NSDictionary"), "dictionary"))))
	})
	return data
}

// TestDockMenu returns the titles of the Dock menu the app delegate
// returns, and clicks the item at click unless it is -1.
func TestDockMenu(click int) []string {
	var titles []string
	withPool(func() {
		menu := send(send(theBackend.app, "delegate"), "applicationDockMenu:", uintptr(theBackend.app))
		if menu == 0 {
			return
		}
		for _, it := range arrayItems(send(menu, "itemArray")) {
			titles = append(titles, goString(send(it, "title")))
		}
		if click >= 0 {
			send(menu, "performActionForItemAtIndex:", uintptr(click))
		}
	})
	return titles
}

// TestRecentDocuments returns the paths of the documents the app recorded
// with App.AddRecentDocument and did not clear, in the order the system
// keeps them.
func TestRecentDocuments() []string {
	var paths []string
	withPool(func() {
		for _, u := range arrayItems(send(send(class("NSDocumentController"), "sharedDocumentController"), "recentDocumentURLs")) {
			paths = append(paths, goString(send(u, "path")))
		}
	})
	return paths
}

// TestFullScreenHidesToolbar reports whether a window asks to hide its
// toolbar with the menu bar when it enters full screen.
func TestFullScreenHidesToolbar(handle uintptr) bool {
	win := id(handle)
	delegate := send(win, "delegate")
	if !respondsTo(delegate, "window:willUseFullScreenPresentationOptions:") {
		return false
	}
	options := send(delegate, "window:willUseFullScreenPresentationOptions:", uintptr(win), presentationFullScreen|presentationAutoHideMenuBar)
	return options&presentationAutoHideToolbar != 0
}

var (
	testInputOnce         sync.Once
	msgSetMarkedText      func(obj id, sel objc.SEL, text id, selected, replacement nsRange)
	msgInsertText         func(obj id, sel objc.SEL, text id, replacement nsRange)
	tisCopyCurrentSource  uintptr
	tisGetSourceProperty  uintptr
	cfEqual               uintptr
	tisPropertySourceType uintptr
	tisTypeKeyboardLayout uintptr
)

func loadTestInput() {
	testInputOnce.Do(func() {
		purego.RegisterFunc(&msgSetMarkedText, msgSendAddr)
		purego.RegisterFunc(&msgInsertText, msgSendAddr)
		tisCopyCurrentSource, _ = purego.Dlsym(libCarbon, "TISCopyCurrentKeyboardInputSource")
		tisGetSourceProperty, _ = purego.Dlsym(libCarbon, "TISGetInputSourceProperty")
		cfEqual, _ = purego.Dlsym(libCF, "CFEqual")
		for _, c := range []struct {
			name string
			to   *uintptr
		}{{"kTISPropertyInputSourceType", &tisPropertySourceType}, {"kTISTypeKeyboardLayout", &tisTypeKeyboardLayout}} {
			if p, err := purego.Dlsym(libCarbon, c.name); err == nil {
				*c.to = **(**uintptr)(unsafe.Pointer(&p))
			}
		}
	})
}

// keyboardLayoutSelected reports whether the input source is a keyboard
// layout, which inserts what keys type, rather than an input method,
// which composes it.
func keyboardLayoutSelected() bool {
	loadTestInput()
	if tisCopyCurrentSource == 0 || tisGetSourceProperty == 0 || tisPropertySourceType == 0 || tisTypeKeyboardLayout == 0 {
		return false
	}
	src, _, _ := purego.SyscallN(tisCopyCurrentSource)
	if src == 0 {
		return false
	}
	defer cfRelease(src)
	typ, _, _ := purego.SyscallN(tisGetSourceProperty, src, tisPropertySourceType)
	eq, _, _ := purego.SyscallN(cfEqual, typ, tisTypeKeyboardLayout)
	return typ != 0 && byte(eq) != 0
}

// TestClickAndType clicks (x, y) in a window showing native UI and types
// text there at once, before the window draws another frame, with key
// events that go through the input method as a keyboard's do. It reports
// false for a window showing a web page, and while the input source is an
// input method, which would compose the keys rather than insert them.
func TestClickAndType(handle uintptr, x, y float64, text string) bool {
	w := theBackend.byNSWindow[id(handle)]
	if w == nil || w.surface == nil || !keyboardLayoutSelected() {
		return false
	}
	TestClick(handle, x, y)
	withPool(func() {
		number := sendInt(w.win, "windowNumber")
		for _, r := range text {
			chars := nsString(string(r))
			for _, typ := range []uint{10, 11} { // NSEventTypeKeyDown, KeyUp
				ev := msgKeyEvent(class("NSEvent"), sel("keyEventWithType:location:modifierFlags:timestamp:windowNumber:context:characters:charactersIgnoringModifiers:isARepeat:keyCode:"),
					typ, NSPoint{}, 0, 0, number, 0, chars, chars, false, 0)
				send(w.win, "sendEvent:", uintptr(ev))
			}
		}
	})
	return true
}

// TestKey presses and releases the key of virtual key code code, typing
// chars, in a window showing native UI, as the keyboard does: the events
// go through the window to its first responder, the surface and its input
// method or a web view.
func TestKey(handle uintptr, code uint16, chars string) bool {
	return TestKeyShift(handle, code, chars, false)
}

// TestAppKey posts a key down and up to the application's event queue,
// which sends it to the key window as it does the user's keys, to a page
// or native UI. The application has handled the key when it returns
// (handlePosted).
func TestAppKey(code uint16, chars string) {
	withPool(func() {
		number := sendInt(send(theBackend.app, "keyWindow"), "windowNumber")
		s := nsString(chars)
		for _, typ := range []uint{10, 11} { // NSEventTypeKeyDown, KeyUp
			ev := msgKeyEvent(class("NSEvent"), sel("keyEventWithType:location:modifierFlags:timestamp:windowNumber:context:characters:charactersIgnoringModifiers:isARepeat:keyCode:"),
				typ, NSPoint{}, 0, 0, number, 0, s, s, false, code)
			send(theBackend.app, "postEvent:atStart:", uintptr(ev), 0)
		}
	})
	handlePosted()
}

// TestKeyShift is TestKey with Shift held when shift is set.
func TestKeyShift(handle uintptr, code uint16, chars string, shift bool) bool {
	w := theBackend.byNSWindow[id(handle)]
	if w == nil || w.surface == nil {
		return false
	}
	sendKeyToWindow(w, code, chars, shift)
	return true
}

// TestPageKeyShift presses a key in a window showing a web page, which the
// window sends to its first responder as it does the keyboard's, with Shift
// held when shift is set. It reports false for a window without a page.
func TestPageKeyShift(handle uintptr, code uint16, chars string, shift bool) bool {
	w := theBackend.byNSWindow[id(handle)]
	if w == nil || w.web == 0 {
		return false
	}
	sendKeyToWindow(w, code, chars, shift)
	return true
}

// TestKeyCmd presses a key with Command held in a window showing native
// UI, which the window sends to its first responder as it does the
// keyboard's: the content, or the page of a web view.
func TestKeyCmd(handle uintptr, code uint16, chars string) bool {
	w := theBackend.byNSWindow[id(handle)]
	if w == nil || w.surface == nil {
		return false
	}
	sendKeyFlags(w, code, chars, 1<<20) // NSEventModifierFlagCommand
	return true
}

// sendKeyToWindow sends a key down and up to a window, as the keyboard does.
func sendKeyToWindow(w *window, code uint16, chars string, shift bool) {
	var flags uint
	if shift {
		flags = 1 << 17 // NSEventModifierFlagShift
	}
	sendKeyFlags(w, code, chars, flags)
}

// sendKeyFlags sends a key down and up to a window with the modifier
// flags held, as the keyboard does.
func sendKeyFlags(w *window, code uint16, chars string, flags uint) {
	withPool(func() {
		number := sendInt(w.win, "windowNumber")
		s := nsString(chars)
		for _, typ := range []uint{10, 11} { // NSEventTypeKeyDown, KeyUp
			ev := msgKeyEvent(class("NSEvent"), sel("keyEventWithType:location:modifierFlags:timestamp:windowNumber:context:characters:charactersIgnoringModifiers:isARepeat:keyCode:"),
				typ, NSPoint{}, flags, 0, number, 0, s, s, false, code)
			send(w.win, "sendEvent:", uintptr(ev))
		}
	})
}

// TestCompose does what an input method does to a window showing native
// UI: it shows text as the composition, its caret at rune caret, or with
// commit, inserts it. It reports false for a window showing a web page.
func TestCompose(handle uintptr, text string, caret int, commit bool) bool {
	return TestComposeOver(handle, text, caret, commit, -1, 0)
}

// TestComposeOver is TestCompose replacing the length UTF-16 units of the
// input method's document at from, unless from is negative, as macOS's
// press and hold does with the letter it accents.
func TestComposeOver(handle uintptr, text string, caret int, commit bool, from, length int) bool {
	w := theBackend.byNSWindow[id(handle)]
	if w == nil || w.surface == nil {
		return false
	}
	loadTestInput()
	withPool(func() {
		replace := nsRange{Location: nsNotFound}
		if from >= 0 {
			replace = nsRange{Location: uint(from), Length: uint(length)}
		}
		if commit {
			msgInsertText(w.surface.view, sel("insertText:replacementRange:"), nsString(text), replace)
			return
		}
		caretAt := nsRange{Location: uint(utf16Len(string([]rune(text)[:caret])))}
		msgSetMarkedText(w.surface.view, sel("setMarkedText:selectedRange:replacementRange:"), nsString(text), caretAt, replace)
	})
	return true
}

// TestKeyComposing presses the key of virtual key code code, typing chars,
// in a window showing native UI, with the test standing in for the input
// method that interprets it: the input method inserts commit, unless it
// is empty, then asks for the key's command (doCommandBySelector:), unless
// command is empty. The Korean input method takes Return so while it
// composes: it commits the syllable and asks for insertNewline:. The
// Japanese one commits the candidate and asks for nothing. It reports
// false for a window showing a web page.
func TestKeyComposing(handle uintptr, code uint16, chars, commit, command string) bool {
	w := theBackend.byNSWindow[id(handle)]
	if w == nil || w.surface == nil {
		return false
	}
	loadTestInput()
	interpret := interpretKeys
	defer func() { interpretKeys = interpret }()
	interpretKeys = func(view, ev id) {
		if commit != "" {
			msgInsertText(view, sel("insertText:replacementRange:"), nsString(commit), nsRange{Location: nsNotFound})
		}
		if command != "" {
			send(view, "doCommandBySelector:", uintptr(sel(command)))
		}
	}
	sendKeyToWindow(w, code, chars, false)
	return true
}

// TestInputClient returns what a window's surface tells input methods: the
// selected range and the whole text of their document.
func TestInputClient(handle uintptr) (selected [2]int, document string) {
	w := theBackend.byNSWindow[id(handle)]
	if w == nil || w.surface == nil {
		return [2]int{-1, 0}, ""
	}
	r := w.surface.selectedRange()
	withPool(func() {
		doc, _, _ := w.surface.document()
		var actual nsRange
		length := uint(units(doc))
		if w.surface.input.Client != nil {
			length = 1<<31 - 1
		}
		document = stringOf(w.surface.substring(nsRange{Length: length}, &actual))
	})
	return [2]int{int(r.Location), int(r.Length)}, document
}

var (
	testDragOnce sync.Once
	testDragInfo id
	testDragAt   NSPoint
	testDragPB   id
	testDragMask uint = 1
)

// TestDropFiles drags files over (x, y), in points from the top-left
// corner of a window's content, and drops them there, as Finder would:
// it calls the surface's dragging methods with an NSDraggingInfo of its
// own, and puts on the pasteboard the file reference URLs of the files
// that exist at those paths, with no symbolic links. It reports whether
// the surface took the files over that point and whether it took the
// drop.
func TestDropFiles(handle uintptr, x, y float64, paths []string) (over, dropped bool) {
	w := theBackend.byNSWindow[id(handle)]
	if w == nil || w.surface == nil {
		return false, false
	}
	testDragOnce.Do(func() {
		classDef("MyGoTestDraggingInfo", "NSObject", nil, []objc.MethodDef{
			method("draggingLocation", func(self id, _ objc.SEL) NSPoint { return testDragAt }),
			method("draggingPasteboard", func(self id, _ objc.SEL) id { return testDragPB }),
			method("draggingSourceOperationMask", func(self id, _ objc.SEL) uint { return testDragMask }),
		})
		testDragInfo = send(send(class("MyGoTestDraggingInfo"), "alloc"), "init")
		testDragPB = retain(send(class("NSPasteboard"), "pasteboardWithUniqueName"))
	})
	withPool(func() {
		content := msgRect(send(w.win, "contentView"), sel("frame"))
		testDragMask = 1
		testDragAt = NSPoint{x, content.Size.Height - y}
		send(testDragPB, "clearContents")
		var urls []id
		for _, p := range paths {
			u := fileURL(p)
			if ref := send(u, "fileReferenceURL"); ref != 0 && goString(send(send(ref, "filePathURL"), "path")) == p {
				u = ref
			}
			urls = append(urls, u)
		}
		send(testDragPB, "writeObjects:", uintptr(nsArray(urls...)))
		v := w.surface.view
		over = send(v, "draggingEntered:", uintptr(testDragInfo)) != 0
		over = send(v, "draggingUpdated:", uintptr(testDragInfo)) != 0 && over
		if !over {
			send(v, "draggingExited:", uintptr(testDragInfo))
			return
		}
		dropped = sendBool(v, "prepareForDragOperation:", uintptr(testDragInfo)) && sendBool(v, "performDragOperation:", uintptr(testDragInfo))
	})
	return over, dropped
}

// TestDropData exercises native source pasteboard providers and the
// destination protocol with serialized data (no core session token).
func TestDropData(handle uintptr, x, y float64, data transfer.Data, operations transfer.Operation) (operation transfer.Operation, dropped bool) {
	w := theBackend.byNSWindow[id(handle)]
	if w == nil || w.surface == nil {
		return
	}
	TestDropFiles(handle, -1, -1, nil)
	withPool(func() {
		testDragMask = macOperations(operations)
		content := msgRect(send(w.win, "contentView"), sel("frame"))
		testDragAt = NSPoint{x, content.Size.Height - y}
		src := &macDataSource{r: platform.DragRequest{Data: data.Snapshot()}}
		defer func() {
			send(testDragPB, "clearContents")
			for _, p := range src.providers {
				delete(macDragProviders, p)
				release(p)
			}
		}()
		send(testDragPB, "clearContents")
		send(testDragPB, "writeObjects:", uintptr(nsArray(src.pasteboardItems()...)))
		v := w.surface.view
		operation = goOperations(uint(send(v, "draggingEntered:", uintptr(testDragInfo))))
		if operation != transfer.None {
			dropped = sendBool(v, "performDragOperation:", uintptr(testDragInfo))
		}
		send(v, "draggingExited:", uintptr(testDragInfo))
	})
	return
}

// TestAccessNode is an element of native UI as assistive technology reads
// it through NSAccessibility.
type TestAccessNode struct {
	Role, Subrole, Label, Value string
	// Frame is in points of the screen, its origin at the bottom left.
	Frame    [4]float64
	Focused  bool
	Depth    int
	Children int
	// Index is a row's place among its table's rows, from 0, and
	// RowCount how many rows a table has, through the newer API.
	Index, RowCount int
	obj             id
}

// TestAccessibility reads the elements of a window showing native UI as
// assistive technology does, depth first.
func TestAccessibility(handle uintptr) []TestAccessNode {
	w := theBackend.byNSWindow[id(handle)]
	if w == nil || w.surface == nil {
		return nil
	}
	var out []TestAccessNode
	withPool(func() {
		var walk func(obj id, depth int)
		walk = func(obj id, depth int) {
			kids := arrayItems(send(obj, "accessibilityChildren"))
			if depth > 0 {
				n := TestAccessNode{
					Role:     goString(send(obj, "accessibilityRole")),
					Subrole:  goString(send(obj, "accessibilitySubrole")),
					Label:    goString(send(obj, "accessibilityLabel")) + goString(send(obj, "accessibilityTitle")),
					Focused:  sendBool(obj, "isAccessibilityFocused"),
					Depth:    depth,
					Children: len(kids),
					obj:      obj,
				}
				// Ours, not a page's elements, which may not answer them.
				if theBackend.byAccess[obj] != nil {
					n.Index, n.RowCount = sendInt(obj, "accessibilityIndex"), sendInt(obj, "accessibilityRowCount")
				}
				if v := send(obj, "accessibilityValue"); v != 0 {
					n.Value = goString(send(v, "description"))
				}
				f := msgRect(obj, sel("accessibilityFrame"))
				n.Frame = [4]float64{f.Origin.X, f.Origin.Y, f.Size.Width, f.Size.Height}
				out = append(out, n)
			}
			for _, k := range kids {
				walk(k, depth+1)
			}
		}
		walk(w.surface.view, 0)
	})
	return out
}

// TestAccessibilityPerform performs action ("press", "increment",
// "decrement", "focus" or "value" with value) on the element of a window
// showing native UI labeled label, as assistive technology does, and
// reports whether there was such an element that allowed it.
func TestAccessibilityPerform(handle uintptr, label, action, value string) bool {
	for _, n := range TestAccessibility(handle) {
		if n.Label != label {
			continue
		}
		ok := false
		withPool(func() {
			selector := map[string]string{"press": "accessibilityPerformPress", "increment": "accessibilityPerformIncrement",
				"decrement": "accessibilityPerformDecrement", "focus": "setAccessibilityFocused:", "value": "setAccessibilityValue:",
				"insert": "setAccessibilitySelectedText:"}[action]
			if !sendBool(n.obj, "isAccessibilitySelectorAllowed:", uintptr(sel(selector))) {
				return
			}
			switch action {
			case "focus":
				send(n.obj, selector, 1)
			case "value", "insert":
				send(n.obj, selector, uintptr(nsString(value)))
			default:
				sendBool(n.obj, selector)
			}
			ok = true
		})
		return ok
	}
	return false
}

// TestAccessibilityAttribute reads an attribute of the element labeled
// label through AppKit's older API, as assistive technology does: its
// value, whether it is settable, and whether the element names it.
func TestAccessibilityAttribute(handle uintptr, label, attr string) (value string, settable, named, ok bool) {
	for _, n := range TestAccessibility(handle) {
		if n.Label != label {
			continue
		}
		withPool(func() {
			name := nsString(attr)
			if v := send(n.obj, "accessibilityAttributeValue:", uintptr(name)); v != 0 {
				value = goString(send(v, "description"))
			}
			settable = sendBool(n.obj, "accessibilityIsAttributeSettable:", uintptr(name))
			for _, a := range arrayItems(send(n.obj, "accessibilityAttributeNames")) {
				named = named || goString(a) == attr
			}
		})
		return value, settable, named, true
	}
	return "", false, false, false
}

// TestObserve has key-value observing watch the view of a window showing
// native UI and the elements of it that assistive technology reads, as
// other code may: the runtime gives each a generated subclass of its
// class, whose methods reach the overrides of MyGo's classes. stop ends it.
func TestObserve(handle uintptr) (stop func()) {
	w := theBackend.byNSWindow[id(handle)]
	if w == nil || w.surface == nil {
		return func() {}
	}
	type observed struct {
		obj id
		key string
	}
	list := []observed{{w.surface.view, "frame"}}
	for _, n := range TestAccessibility(handle) {
		list = append(list, observed{n.obj, "accessibilityValue"})
	}
	withPool(func() {
		for _, o := range list {
			// Kept until stop: an object observed must not go away.
			retain(o.obj)
			send(o.obj, "addObserver:forKeyPath:options:context:", uintptr(w.delegate), uintptr(nsString(o.key)), 0, 0)
		}
	})
	return func() {
		withPool(func() {
			for _, o := range list {
				send(o.obj, "removeObserver:forKeyPath:", uintptr(w.delegate), uintptr(nsString(o.key)))
				release(o.obj)
			}
		})
	}
}

// TestDocumentChrome describes what a window's title bar shows of its
// document: the title, the file of the proxy icon, whether the icon
// shows, and the dot of an edited document.
func TestDocumentChrome(handle uintptr) (title, file string, icon, edited bool) {
	withPool(func() {
		w := id(handle)
		title = goString(send(w, "title"))
		if u := send(w, "representedURL"); u != 0 {
			file = goString(send(u, "path"))
		}
		const documentIconButton = 4 // NSWindowDocumentIconButton
		if b := send(w, "standardWindowButton:", documentIconButton); b != 0 {
			icon = !sendBool(b, "isHidden")
		}
		edited = sendBool(w, "isDocumentEdited")
	})
	return
}

// TestSheet returns the texts of the alert attached to a window as a
// sheet, and its button titles in the order the alert was given them
// (their tags), which TestEndSheetButton takes.
func TestSheet(handle uintptr) (texts, buttons []string, ok bool) {
	withPool(func() {
		sheet := send(id(handle), "attachedSheet")
		if sheet == 0 {
			return
		}
		ok = true
		tagged := map[int]string{}
		var walk func(v id)
		walk = func(v id) {
			switch {
			case sendBool(v, "isKindOfClass:", uintptr(class("NSButton"))):
				if t := goString(send(v, "title")); t != "" {
					tagged[sendInt(v, "tag")-nsAlertFirstButtonReturn] = t
				}
			case sendBool(v, "isKindOfClass:", uintptr(class("NSTextField"))):
				if t := goString(send(v, "stringValue")); t != "" {
					texts = append(texts, t)
				}
			}
			for _, sub := range arrayItems(send(v, "subviews")) {
				walk(sub)
			}
		}
		walk(send(sheet, "contentView"))
		for i := 0; i < len(tagged); i++ {
			buttons = append(buttons, tagged[i])
		}
	})
	return
}

// TestEndSheetButton ends the alert attached to a window as a sheet as if
// its button i, in the order the alert was given them, was clicked.
func TestEndSheetButton(handle uintptr, i int) bool {
	sheet := send(id(handle), "attachedSheet")
	if sheet == 0 {
		return false
	}
	send(id(handle), "endSheet:returnCode:", uintptr(sheet), uintptr(nsAlertFirstButtonReturn+i))
	return true
}

// TestSheetDestructive reports whether each button of the alert attached
// to a window as a sheet, in the order the alert was given them, has a
// destructive action.
func TestSheetDestructive(handle uintptr) (destructive []bool, ok bool) {
	withPool(func() {
		sheet := send(id(handle), "attachedSheet")
		if sheet == 0 {
			return
		}
		ok = true
		tagged := map[int]id{}
		var walk func(v id)
		walk = func(v id) {
			if sendBool(v, "isKindOfClass:", uintptr(class("NSButton"))) {
				if i := sendInt(v, "tag") - nsAlertFirstButtonReturn; i >= 0 {
					tagged[i] = v
				}
			}
			for _, sub := range arrayItems(send(v, "subviews")) {
				walk(sub)
			}
		}
		walk(send(sheet, "contentView"))
		for i := 0; i < len(tagged); i++ {
			destructive = append(destructive, respondsTo(tagged[i], "hasDestructiveAction") && sendBool(tagged[i], "hasDestructiveAction"))
		}
	})
	return
}

// TestSheetKey presses and releases a key in the sheet attached to a
// window, with Command held when cmd is set, through the application's
// event queue as the keyboard's keys go. It reports whether there was a
// sheet.
func TestSheetKey(handle uintptr, code uint16, chars string, cmd bool) bool {
	sheet := send(id(handle), "attachedSheet")
	if sheet == 0 {
		return false
	}
	var flags uint
	if cmd {
		flags = 1 << 20 // NSEventModifierFlagCommand
	}
	withPool(func() {
		number := sendInt(sheet, "windowNumber")
		s := nsString(chars)
		for _, typ := range []uint{10, 11} { // NSEventTypeKeyDown, KeyUp
			ev := msgKeyEvent(class("NSEvent"), sel("keyEventWithType:location:modifierFlags:timestamp:windowNumber:context:characters:charactersIgnoringModifiers:isARepeat:keyCode:"),
				typ, NSPoint{}, flags, 0, number, 0, s, s, false, code)
			send(theBackend.app, "postEvent:atStart:", uintptr(ev), 0)
		}
	})
	handlePosted()
	return true
}

// TestModal reports whether a dialog runs for a window, as its sheet or
// application modal (runModal), and whether it shows: AppKit ordered it
// in, and the window server put it on screen.
func TestModal(handle uintptr) (running, shows bool) {
	panel := send(id(handle), "attachedSheet")
	if panel == 0 {
		panel = send(theBackend.app, "modalWindow")
	}
	if panel == 0 {
		return false, false
	}
	return true, sendBool(panel, "isVisible") && TestOnScreen(uintptr(panel))
}

// TestCancelModal dismisses the dialog TestModal finds, as its Cancel
// button or Escape would, and reports whether there was one.
func TestCancelModal(handle uintptr) bool {
	sheet := send(id(handle), "attachedSheet")
	panel := sheet
	if panel == 0 {
		panel = send(theBackend.app, "modalWindow")
	}
	switch {
	case panel == 0:
		return false
	case sendBool(panel, "isKindOfClass:", uintptr(class("NSSavePanel"))):
		send(panel, "cancel:", 0)
	case sheet != 0:
		send(id(handle), "endSheet:returnCode:", uintptr(sheet), 0) // NSModalResponseCancel
	default:
		send(theBackend.app, "abortModal")
	}
	return true
}

// TestRightClickPage right-clicks (x, y), in points from the top-left
// corner of a window's content, as TestClick clicks. WebKit opens the
// page's context menu once the page heard the click, later.
func TestRightClickPage(handle uintptr, x, y float64) {
	testMouse(handle, x, y, false, 3, 4) // NSEventTypeRightMouseDown, RightMouseUp
}

// TestPageMenu returns the titles of the items of the page's context menu
// being shown, "-" for separators, and false when none is.
func TestPageMenu() ([]string, bool) {
	menu := openContextMenu
	if menu == 0 {
		return nil, false
	}
	labels := []string{}
	for i := range sendInt(menu, "numberOfItems") {
		item := send(menu, "itemAtIndex:", uintptr(i))
		switch {
		case sendBool(item, "isHidden"):
		case sendBool(item, "isSeparatorItem"):
			labels = append(labels, "-")
		default:
			labels = append(labels, goString(send(item, "title")))
		}
	}
	return labels, true
}

// TestChoosePageMenuItem chooses the item titled label of the page's
// context menu being shown, as a click on it does, closes the menu, and
// reports whether it has one.
func TestChoosePageMenuItem(label string) bool {
	menu := openContextMenu
	if menu == 0 {
		return false
	}
	for i := range sendInt(menu, "numberOfItems") {
		item := send(menu, "itemAtIndex:", uintptr(i))
		if !sendBool(item, "isHidden") && !sendBool(item, "isSeparatorItem") && goString(send(item, "title")) == label {
			send(menu, "performActionForItemAtIndex:", uintptr(i))
			send(menu, "cancelTracking")
			return true
		}
	}
	return false
}

// TestDismissPageMenu closes the page's context menu being shown, if one
// is.
func TestDismissPageMenu() {
	if openContextMenu != 0 {
		send(openContextMenu, "cancelTracking")
	}
}

var (
	testPixelsOnce                   sync.Once
	cgWindowListCreateImage          func(bounds NSRect, list, window, options uint32) uintptr
	cgBitmapContextCreate            func(data unsafe.Pointer, width, height, bitsPerComponent, bytesPerRow, space uintptr, info uint32) uintptr
	cgContextSetInterpolationQuality func(ctx uintptr, quality int32)
	cgContextDrawImage               func(ctx uintptr, rect NSRect, img uintptr)
	cgContextRelease                 func(ctx uintptr)
)

// TestWindowPixels returns the content of a window as the window server
// shows it on screen, not as its views would draw it now: what Core
// Animation committed and the screen took. One pixel a point, in sRGB;
// ok is false where the window server gives no image of the window. An
// app gets the images of its own windows without the permission to record
// the screen.
func TestWindowPixels(handle uintptr) (img *image.RGBA, ok bool) {
	w := theBackend.byNSWindow[id(handle)]
	if w == nil {
		return nil, false
	}
	loadSurface() // the sRGB color space
	testPixelsOnce.Do(func() {
		create, err := purego.Dlsym(libCG, "CGWindowListCreateImage")
		if err != nil {
			return // gone from the system
		}
		purego.RegisterFunc(&cgWindowListCreateImage, create)
		purego.RegisterLibFunc(&cgBitmapContextCreate, libCG, "CGBitmapContextCreate")
		purego.RegisterLibFunc(&cgContextSetInterpolationQuality, libCG, "CGContextSetInterpolationQuality")
		purego.RegisterLibFunc(&cgContextDrawImage, libCG, "CGContextDrawImage")
		purego.RegisterLibFunc(&cgContextRelease, libCG, "CGContextRelease")
	})
	if cgWindowListCreateImage == nil {
		return nil, false
	}
	const (
		includingWindow   = 1 << 3 // kCGWindowListOptionIncludingWindow
		ignoreFraming     = 1 << 0 // kCGWindowImageBoundsIgnoreFraming: no shadow
		nominalResolution = 1 << 4 // kCGWindowImageNominalResolution
	)
	null := NSRect{Origin: NSPoint{math.Inf(1), math.Inf(1)}} // CGRectNull: the window's bounds
	shot := cgWindowListCreateImage(null, includingWindow, uint32(sendInt(w.win, "windowNumber")), ignoreFraming|nominalResolution)
	if shot == 0 {
		return nil, false
	}
	defer purego.SyscallN(cgImageRelease, shot)
	// Drawn into a bitmap of the window's size in points, whatever the
	// image's resolution, with the rows top to bottom.
	frame := msgRect(w.win, sel("frame"))
	fw, fh := int(math.Round(frame.Size.Width)), int(math.Round(frame.Size.Height))
	if fw <= 0 || fh <= 0 {
		return nil, false
	}
	full := image.NewRGBA(image.Rect(0, 0, fw, fh))
	const premultipliedLast, byteOrder32Big = 1, 4 << 12
	ctx := cgBitmapContextCreate(unsafe.Pointer(&full.Pix[0]), uintptr(fw), uintptr(fh), 8, uintptr(full.Stride), cgColorSpaceSRGB, premultipliedLast|byteOrder32Big)
	if ctx == 0 {
		return nil, false
	}
	cgContextSetInterpolationQuality(ctx, 1) // kCGInterpolationNone
	cgContextDrawImage(ctx, NSRect{Size: NSSize{float64(fw), float64(fh)}}, shot)
	cgContextRelease(ctx)
	// The content view, from the window's bottom-left corner.
	content := msgRect(send(w.win, "contentView"), sel("frame"))
	x := int(math.Round(content.Origin.X))
	y := fh - int(math.Round(content.Origin.Y+content.Size.Height))
	r := image.Rect(x, y, x+int(math.Round(content.Size.Width)), y+int(math.Round(content.Size.Height))).Intersect(full.Rect)
	img = image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	for row := range r.Dy() {
		copy(img.Pix[row*img.Stride:], full.Pix[full.PixOffset(r.Min.X, r.Min.Y+row):full.PixOffset(r.Max.X, r.Min.Y+row)])
	}
	return img, true
}

// TestDrawablesTrimmed reports whether the GPU renderer of a window's
// native UI gave back its drawables, as it does once the window has drawn
// nothing for a while (internal/gpu/metal's trim), and whether the window
// draws with one.
func TestDrawablesTrimmed(handle uintptr) (trimmed, ok bool) {
	w := theBackend.byNSWindow[id(handle)]
	if w == nil || w.surface == nil {
		return false, false
	}
	l := w.surface.metalLayer()
	if l == 0 {
		return false, false
	}
	size := msgSize(l, sel("drawableSize"))
	return size.Width <= 1 && size.Height <= 1, true
}
