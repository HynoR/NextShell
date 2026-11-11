//go:build darwin

package darwin

import (
	"math"

	"github.com/ebitengine/purego/objc"
)

// Drag regions move the window with a press (StartDrag): the press of a
// page's `--app-region: drag`, which the page tells a few milliseconds
// after the web view got it, or of native UI's DragWindow, which asks while
// the surface handles the press.
//
// The window server moves the window best (performWindowDragWithEvent:):
// it takes the window to other Spaces and tiles it. But it may not begin
// when the pointer has moved since the press by the time it is asked: AppKit
// logs "Window move completed without beginning" and the window stays where
// it was, which a quick press and drag hits now and then (issue #528). So
// MyGo watches the press. When the pointer has not moved yet, it asks the
// window server; when it has, or when the press's drags still reach the app
// past a few points, a few drags after the ask, and the window server has
// not begun, MyGo moves the window itself, keeping the point pressed under
// the pointer, as the window server would. Either way the press's drags are
// the window's, not the content's.
//
// The window server tells the window as it begins (windowWillMove:), within
// a drag or two of being asked; MyGo's own moves (setFrameOrigin:) never
// do. The window's frame says so late: AppKit learns where the window is
// about every tenth of a second while the window server moves it. Should
// MyGo take a frame that has not moved for a window server that did not
// begin, both would move the window, the window server to where the pointer
// is and MyGo to where it was an event before, and the window shakes. So
// MyGo waits for a few drags before taking over, however far the first
// goes, and leaves the window alone as soon as the window server says it
// begins, even once MyGo has taken over: the window not being where MyGo put
// it tells MyGo only late, and not at all for a slow drag, whose every move
// puts the frame back where MyGo wants it.

// dragPhase is where a press stands in moving its window.
type dragPhase uint8

const (
	dragIdle    dragPhase = iota // no press, or one that did not ask
	dragPressed                  // the button is down: a drag may be asked
	dragNative                   // the window server was asked to move it
	dragManual                   // MyGo moves it with the pointer
	dragServer                   // the window server moves it
)

func (p dragPhase) String() string {
	return [...]string{"idle", "pressed", "native", "manual", "server"}[p]
}

const (
	// dragStill is how far, in points, the pointer may have gone when
	// the drag is asked, for the window server to be asked to move the
	// window: a hand on a touchpad trembles.
	dragStill = 1
	// dragSlop is how far the pointer may go, with the press's drags
	// still reaching the app and the window not moving, before MyGo
	// moves the window itself: the window server did not begin.
	dragSlop = 3
	// dragWait is how many drags the window server is given after the
	// ask to say it begins, before MyGo moves the window itself: a
	// quick hand is past dragSlop with the first.
	dragWait = 3
	// dragAway is how far, in points, the window may be from where MyGo
	// put it, which AppKit rounds, before MyGo takes it that the window
	// server moves it.
	dragAway = 2
)

// windowDrag follows the press of the left button, for StartDrag. Points
// are in the display space of Core Graphics (top-left origin of the
// primary display, y down), where events keep their place when the window
// moves; window origins are AppKit's (bottom-left, y up).
type windowDrag struct {
	phase dragPhase
	win   id      // the window pressed
	press NSPoint // where the press was
	at    NSPoint // where the pointer was last, with the button down
	// origin is the window's frame origin when the drag was asked.
	origin NSPoint
	// put is the origin MyGo last moved the window to, while manual.
	put NSPoint
	// drags counts the drags since the window server was asked.
	drags int
}

// down notes a press of the left button in win at the point at.
func (d *windowDrag) down(win id, at NSPoint) {
	*d = windowDrag{phase: dragPressed, win: win, press: at, at: at}
}

// willMove notes that the window server begins to move win, which it
// does only when asked, though it may be after MyGo took over.
func (d *windowDrag) willMove(win id) {
	if (d.phase == dragNative || d.phase == dragManual) && d.win == win {
		d.phase = dragServer
	}
}

// up notes the release of the button, or a press elsewhere.
func (d *windowDrag) up() { *d = windowDrag{} }

// dragStart is what StartDrag does.
type dragStart uint8

const (
	startNone   dragStart = iota // nothing: no press of win is down
	startNative                  // ask the window server
	startManual                  // move the window to target, and with the pointer
)

// start asks to move win, whose frame origin is origin, with the press.
// A press asks once.
func (d *windowDrag) start(win id, origin NSPoint) dragStart {
	if d.phase != dragPressed || d.win != win {
		return startNone
	}
	d.origin = origin
	if distance(d.at, d.press) >= dragStill {
		d.phase = dragManual
		return startManual
	}
	d.phase = dragNative
	return startNative
}

// dragged notes the pointer at the point at with the button down, the
// pressed window's frame origin being origin. It reports whether the event
// is the window's move rather than the content's, and whether to move the
// window (to target).
func (d *windowDrag) dragged(at, origin NSPoint) (eat, move bool) {
	switch d.phase {
	case dragPressed:
		d.at = at
	case dragNative:
		d.at = at
		if origin != d.origin {
			d.phase = dragServer
			return true, false
		}
		d.drags++
		if distance(at, d.press) < dragSlop || d.drags < dragWait {
			// The pointer has hardly moved yet, or the window
			// server may still say it begins.
			return true, false
		}
		d.phase = dragManual
		return true, true
	case dragManual:
		d.at = at
		if distance(origin, d.put) > dragAway {
			// The window server began after all.
			d.phase = dragServer
			return true, false
		}
		return true, true
	case dragServer:
		d.at = at
		return true, false
	}
	return false, false
}

// placed notes that MyGo moved the window to origin.
func (d *windowDrag) placed(origin NSPoint) { d.put = origin }

// target is the window's origin that keeps the point pressed under the
// pointer.
func (d *windowDrag) target() NSPoint {
	return NSPoint{d.origin.X + d.at.X - d.press.X, d.origin.Y - (d.at.Y - d.press.Y)}
}

func distance(a, b NSPoint) float64 { return math.Hypot(a.X-b.X, a.Y-b.Y) }

// belowMenuBar returns the origin of a window as tall as height, moved down
// so that its top edge stays at or below top, as AppKit keeps a title bar
// out of the menu bar.
func belowMenuBar(origin NSPoint, height, top float64) NSPoint {
	if origin.Y+height > top {
		origin.Y = top - height
	}
	return origin
}

// theDrag is the press that may move a window. Main thread only.
var theDrag windowDrag

// dragMonitor sees the drags and the release of a press before the
// window does. It is made once, with the first press of a web view or a
// surface.
var dragMonitor id

// pressedLeft notes a press of the left button in w's web view or
// surface, which a drag region may ask to move w with.
func (w *window) pressedLeft(ev id) {
	watchWindowDrags()
	release(w.lastMouseDown)
	w.lastMouseDown = retain(ev)
	theDrag.down(w.win, eventPoint(ev))
}

func watchWindowDrags() {
	if dragMonitor != 0 {
		return
	}
	const mask = 1<<1 | 1<<2 | 1<<6 // left mouse down, up and dragged
	blk := newBlock(func(_ objc.Block, ev id) id {
		if theDrag.phase == dragIdle {
			return ev
		}
		switch sendInt(ev, "type") {
		case 6: // NSEventTypeLeftMouseDragged
			if send(ev, "window") != theDrag.win {
				return ev
			}
			eat, move := theDrag.dragged(eventPoint(ev), msgRect(theDrag.win, sel("frame")).Origin)
			if move {
				moveDraggedWindow()
			}
			if eat {
				return 0
			}
		default:
			// A release, or a press the window has yet to see, which
			// notes itself if it may drag.
			theDrag.up()
		}
		return ev
	})
	dragMonitor = retain(send(class("NSEvent"), "addLocalMonitorForEventsMatchingMask:handler:", mask, uintptr(blk)))
}

// eventPoint returns where a mouse event happened, in the display space
// of Core Graphics.
func eventPoint(ev id) NSPoint {
	if cg := send(ev, "CGEvent"); cg != 0 {
		return cgEventGetLocation(uintptr(cg))
	}
	p := msgPoint(class("NSEvent"), sel("mouseLocation"))
	return NSPoint{p.X, primaryScreenHeight() - p.Y}
}

// moveDraggedWindow moves the window of theDrag to its target, below the
// menu bar of the screen with the pointer.
func moveDraggedWindow() {
	d := &theDrag
	origin := d.target()
	screens := arrayItems(send(class("NSScreen"), "screens"))
	if len(screens) > 0 {
		primary := msgRect(screens[0], sel("frame"))
		p := NSPoint{d.at.X, primary.Origin.Y + primary.Size.Height - d.at.Y}
		for _, s := range screens {
			if f := msgRect(s, sel("frame")); p.X >= f.Origin.X && p.X < f.Origin.X+f.Size.Width && p.Y >= f.Origin.Y && p.Y < f.Origin.Y+f.Size.Height {
				v := msgRect(s, sel("visibleFrame"))
				origin = belowMenuBar(origin, msgRect(d.win, sel("frame")).Size.Height, v.Origin.Y+v.Size.Height)
				break
			}
		}
	}
	d.placed(origin)
	msgSetPoint(d.win, sel("setFrameOrigin:"), origin)
}

// StartDrag moves the window with the press of a drag region, while the
// button is down.
func (w *window) StartDrag() {
	if w.closed || w.lastMouseDown == 0 || !sendBool(w.win, "isMovable") || w.hasStyle(styleFullScreen) {
		return
	}
	switch theDrag.start(w.win, msgRect(w.win, sel("frame")).Origin) {
	case startNative:
		send(w.win, "performWindowDragWithEvent:", uintptr(w.lastMouseDown))
	case startManual:
		moveDraggedWindow()
	}
}

// forgetDrag forgets the press of a window that closes.
func (w *window) forgetDrag() {
	if theDrag.win == w.win {
		theDrag.up()
	}
	release(w.lastMouseDown)
	w.lastMouseDown = 0
}
