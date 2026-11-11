package ui

import (
	"math/bits"
	"runtime"
	"slices"
	"time"

	"github.com/egoist/mygo/internal/platform"
)

// event handles a surface event on the main thread. It reports whether
// an element takes files dragged over or dropped at the event's position,
// the key pressed or the composition (platform.WindowHandler.SurfaceEvent).
func (rt *engine) event(ev platform.SurfaceEvent) (taken bool) {
	if ev.Kind == platform.SurfaceCommandEnabled {
		// A menu asking about its item changes nothing.
		return rt.commandEnabled(ev.Text)
	}
	if ev.Kind != platform.SurfaceFrame {
		// Whatever the event changes, the next frame builds anew.
		rt.redraw = false
	}
	switch ev.Kind {
	case platform.SurfaceFrame, platform.SurfaceResize, platform.SurfaceShown, platform.SurfaceHidden:
	default:
		// Input, which the next frame builds even out of sight.
		rt.input = true
	}
	// The window's DIPs, zoomed, are the content's.
	rt.fromWindow(&ev)
	x, y := float32(ev.X), float32(ev.Y)
	if ev.Kind == platform.PointerDown || ev.Kind == platform.WebViewPress {
		// A press is of no element until countPress finds it one.
		rt.lastPress.prev, rt.lastPress.id = rt.lastPress.id, 0
	}
	held := rt.buttons
	switch ev.Kind {
	case platform.PointerMove, platform.PointerUp:
		rt.pointerDevice(ev)
	case platform.PointerDown:
		// It presses once it came where it presses (pointerDevice).
		rt.pointerType = PointerType(ev.Pointer)
	case platform.SurfaceBlur, platform.PointerCancel:
		// Buttons let go of elsewhere, or whose press the system took,
		// never come back up here.
		rt.buttons, rt.pressure = 0, 0
	}
	if rt.insp.pointer(rt, ev, x, y) {
		return true
	}
	switch ev.Kind {
	case platform.PointerMove, platform.PointerDown, platform.PointerUp, platform.PointerScroll, platform.PointerZoom:
		rt.mods = Modifiers(ev.Mods)
	}
	switch ev.Kind {
	case platform.SurfaceFrame:
		rt.surfaceFrame()
	case platform.SurfaceResize:
		rt.requestFrame()
	case platform.SurfaceShown:
		// A video goes on from where it stood out of sight.
		rt.shown = rt.now()
		if rt.held || rt.hiddenAsked {
			// What moves goes on from where the time puts it.
			rt.held = false
			rt.requestFrame()
		}
	case platform.SurfaceHidden:
		if rt.hiddenAsked {
			// A view that asks builds once more, out of sight, to stop
			// what it does while none of the window shows.
			rt.input = true
			rt.requestFrame()
		}
	case platform.PointerMove:
		rt.pointerMove(x, y)
	case platform.PointerDown:
		rt.pointerMove(x, y)
		rt.pointerDevice(ev)
		rt.pointerDown(x, y, ev.Button, Modifiers(ev.Mods), ev.Clicks)
	case platform.PointerUp:
		rt.pointerMove(x, y)
		rt.pointerUp(ev.Button, ev.Clicks, false)
	case platform.PointerCancel:
		// Where the pointer went the system may not tell: it stays.
		button := -1
		if held != 0 {
			button = bits.TrailingZeros8(held)
		}
		rt.pointerCancel(button)
	case platform.WebViewPress:
		rt.webViewPress(x, y, ev.Button)
	case platform.PointerLeave:
		rt.pointerIn = false
		if rt.pressed == nil {
			rt.setHover(nil)
		} else {
			// The elements around the one pressed hover no more.
			rt.requestFrame()
		}
	case platform.PointerScroll:
		rt.pointerMove(x, y)
		rt.scroll(float32(ev.DX), float32(ev.DY), Modifiers(ev.Mods), ev.Precise, ev.Momentum)
	case platform.PointerZoom:
		rt.pointerMove(x, y)
		rt.zoom(float32(ev.Scale), Modifiers(ev.Mods))
	case platform.KeyPressed:
		rt.modsChanged(Modifiers(ev.Mods))
		rt.holdKey(Modifiers(ev.Mods), Key(ev.Key), Key(ev.PhysicalKey))
		taken = rt.keyDownPhysical(Modifiers(ev.Mods), Key(ev.Key), Key(ev.PhysicalKey), KeyCode(ev.Code), ev.Repeat)
	case platform.ModifiersChanged:
		rt.modsChanged(Modifiers(ev.Mods))
	case platform.RawKeyPressed, platform.RawKeyReleased:
		rt.modsChanged(Modifiers(ev.Mods))
		taken = rt.rawKey(ev)
	case platform.KeyReleased:
		rt.modsChanged(Modifiers(ev.Mods))
		key, physical := Key(ev.Key), Key(ev.PhysicalKey)
		if key == KeyUnknown && rt.byPosition[physical] {
			key = physical
		}
		delete(rt.byPosition, physical)
		rt.releaseKey(key, physical)
		rt.observeFocus(InputEvent{Kind: InputKeyUp, Key: key, PhysicalKey: physical, Code: KeyCode(ev.Code), Mods: Modifiers(ev.Mods)})
		if h := rt.focusHandler(); h != nil {
			rt.deliver(h, InputEvent{Kind: InputKeyUp, Key: key, PhysicalKey: physical, Code: KeyCode(ev.Code), Mods: Modifiers(ev.Mods)})
		}
	case platform.TextInput:
		rt.editEvent(rt.replaced(editEvent{kind: editInsert, text: ev.Text}, ev))
	case platform.TextComposition:
		taken = rt.editEvent(rt.replaced(editEvent{kind: editCompose, text: ev.Text, caret: ev.Caret}, ev))
	case platform.SurfaceCommand:
		rt.editEvent(editEvent{kind: editCommand, text: ev.Text, repeat: ev.Repeat})
	case platform.SurfaceFocus:
		rt.windowFocused = true
		rt.blinkStart = rt.now()
		rt.focusChanged()
	case platform.SurfaceBlur:
		rt.windowFocused = false
		rt.selection.dragging = false
		// Keys let go of elsewhere never come back up here.
		rt.modsChanged(0)
		clear(rt.byPosition)
		rt.releaseKeys()
		if p := rt.pressed; p != nil {
			x, y := p.local(rt.pointerX, rt.pointerY)
			rt.endPress(p.input, x, y)
		}
		rt.focusChanged()
	case platform.FileDragOver:
		taken = rt.fileDrag(x, y)
	case platform.FileDragLeave:
		rt.fileDrag(-1, -1)
	case platform.FileDrop:
		taken = rt.fileDrop(x, y, ev.Files)
	case platform.DataDragOver:
		taken = rt.dataDragOver(x, y, ev.Drag)
	case platform.DataDragLeave:
		rt.clearDataOver()
	case platform.DataDrop:
		taken = rt.dataDrop(x, y, ev.Drag)
	case platform.AccessibilityOn:
		rt.accessibilityOn()
	case platform.AccessAction:
		rt.accessAction(ev)
	}
	if ev.Kind != platform.SurfaceFrame {
		// A press or Tab may have moved the focus: tell the host now, not
		// after the next frame, or the text of keys typed before it would
		// never reach the input method.
		rt.updateTextInput()
	}
	return taken
}

// hitChain returns the ids of the topmost element at (x, y) and of its
// ancestors, innermost first.
func (rt *engine) hitChain(x, y float32) []uint64 { return rt.appendHitChain(nil, x, y) }

// appendHitChain appends hitChain(x, y) to chain.
func (rt *engine) appendHitChain(chain []uint64, x, y float32) []uint64 {
	for i := len(rt.hits) - 1; i >= 0; i-- {
		h := &rt.hits[i]
		if !rt.contains(h.st, h.r, x, y) {
			continue
		}
		for s := h.st; s != nil; s = rt.states[s.parent] {
			chain = append(chain, s.id)
			if s.parent == 0 {
				break
			}
		}
		return chain
	}
	return chain
}

// setHover makes chain the elements under the pointer, and reports
// whether it took chain, which then must not change.
func (rt *engine) setHover(chain []uint64) bool {
	changed := len(chain) != len(rt.hover)
	if !changed {
		for i := range chain {
			if chain[i] != rt.hover[i] {
				changed = true
				break
			}
		}
	}
	if !changed {
		return false
	}
	// Only elements that look at their hover need a frame.
	need := rt.watchHover(chain) || rt.watchHover(rt.hover)
	rt.hover = chain
	if need {
		rt.requestFrame()
	}
	rt.updateCursor()
	return true
}

// watchHover reports whether an element of chain looks at its hover.
func (rt *engine) watchHover(chain []uint64) bool {
	for _, id := range chain {
		if s := rt.states[id]; s != nil && s.flags&(flagHover|flagTrackPointer) != 0 {
			return true
		}
	}
	return false
}

// rehover makes the elements under the pointer hover after a frame built
// and laid them out anew, as the row taking the place of one removed: a
// pointer standing still sends no event that would. It asks for the frame
// that shows them hovering, which a frame being built cannot.
func (rt *engine) rehover() {
	if rt.pressed != nil || rt.scrollDrag.st != nil {
		// The hover holds the elements under the pointer as the press
		// began (Hovered).
		return
	}
	chain := rt.chain[:0]
	if rt.pointerIn {
		chain = rt.appendHitChain(chain, rt.pointerX, rt.pointerY)
	}
	old := rt.hover
	if !rt.setHover(chain) {
		rt.chain = chain[:0]
		return
	}
	rt.chain = old[:0]
	if rt.watchHover(chain) {
		rt.redraw, rt.repaintDue = false, time.Time{}
		rt.host.requestFrame()
	}
}

// unhoverGone makes what the pointer is over hover in place of the
// element it hovered, gone in the pass just built, as a menu's backdrop
// closing over a button, and reports whether that changes what an
// element looking at its hover sees, for the pass to be built again: the
// button shows hovering in the frame the menu goes, rather than not
// until the next (rehover). What it is over is told from the last
// frame's layout, leaving out what the pass did not build.
func (rt *engine) unhoverGone() bool {
	if len(rt.hover) == 0 || !rt.pointerIn || rt.pressed != nil || rt.scrollDrag.st != nil {
		return false
	}
	built := func(s *state) bool { return s.seen == rt.frame && s.pass == rt.pass }
	if s := rt.states[rt.hover[0]]; s != nil && built(s) {
		return false
	}
	chain := rt.chain[:0]
	for i := len(rt.hits) - 1; i >= 0; i-- {
		h := &rt.hits[i]
		if !built(h.st) || !rt.contains(h.st, h.r, rt.pointerX, rt.pointerY) {
			continue
		}
		for s := h.st; s != nil; s = rt.states[s.parent] {
			chain = append(chain, s.id)
			if s.parent == 0 {
				break
			}
		}
		break
	}
	old := rt.hover
	if slices.Equal(chain, old) {
		rt.chain = chain[:0]
		return false
	}
	rt.hover, rt.chain = chain, old[:0]
	return rt.watchHover(chain) || rt.watchHover(old)
}

func (rt *engine) pointerMove(x, y float32) {
	if d := &rt.scrollDrag; d.st != nil {
		s := d.st
		g := d.bars(s.barWidth)
		lx, ly := s.local(x, y)
		lx += s.x
		ly += s.y
		if d.horizontal {
			if travel := g.hTrack.W - 4 - g.h.W; travel > 0 {
				moved := lx - d.start
				if s.rtl {
					// The offset counts from the right.
					moved = -moved
				}
				s.scrollTo(dragTo(d.from, moved, travel, d.contentW-float64(s.w), s.contentW-float64(s.w)), s.scrollY)
			}
		} else if travel := g.vTrack.H - 4 - g.v.H; travel > 0 {
			s.scrollTo(s.scrollX, dragTo(d.from, ly-d.start, travel, d.contentH-float64(s.h), s.contentH-float64(s.h)))
		}
		rt.pointerX, rt.pointerY = x, y
		rt.requestFrame()
		return
	}
	if rt.pressed != nil && rt.pointerIn {
		dx, dy := rt.pressed.vector(rt.pointerX, rt.pointerY, x-rt.pointerX, y-rt.pointerY)
		rt.pressed.dragX += dx
		rt.pressed.dragY += dy
		rt.pressed.drag.move(rt.now(), dx, dy, x-rt.pointerX, y-rt.pointerY)
		if rt.pressed.flags&(flagTrackPointer|flagDraggable|flagEditable|flagSelectable) != 0 {
			rt.requestFrame()
		} else {
			rt.pressMove(rt.pointerX, rt.pointerY, x, y)
		}
		rt.dragMove(x, y)
	}
	// A pen pressing harder or softer where it stands moves too.
	moved := x != rt.pointerX || y != rt.pointerY || rt.pressureMoved
	rt.pressureMoved = false
	rt.pointerX, rt.pointerY, rt.pointerIn = x, y, true
	if rt.selection.dragging && rt.pressed != nil {
		rt.moveTextSelection(x, y)
	}
	if rt.pressed == nil {
		// The chain goes in a buffer, which the hover's last chain
		// becomes when the hover takes it.
		old, chain := rt.hover, rt.appendHitChain(rt.chain[:0], x, y)
		if rt.setHover(chain) {
			rt.chain = old[:0]
		} else {
			rt.chain = chain[:0]
			// The pointer may have gone onto a scroll bar, or off it.
			rt.updateCursor()
		}
	}
	if moved {
		// The element pressed takes the moves, else the one under the
		// pointer.
		if p := rt.pressed; p != nil && p.input != nil {
			rt.deliver(p, InputEvent{Kind: InputPointerMove, Button: rt.pressButton, Mods: rt.mods})
		} else if p == nil {
			if h := rt.handler(rt.hover); h != nil {
				rt.deliver(h, InputEvent{Kind: InputPointerMove, Button: -1, Mods: rt.mods})
			}
		}
	}
	if moved {
		for _, id := range rt.hover {
			if s := rt.states[id]; s != nil && s.flags&flagTrackPointer != 0 {
				rt.requestFrame()
				break
			}
		}
	}
}

// pointerDevice keeps the device of a pointer event, the buttons it
// leaves down and how hard it presses, as Pointer Events have it: a
// device that tells no pressure, as a mouse, presses 0.5 while a button
// is down, and none presses while no button is.
func (rt *engine) pointerDevice(ev platform.SurfaceEvent) {
	if b := ev.Button; b >= 0 && b < 8 {
		switch ev.Kind {
		case platform.PointerDown:
			rt.buttons |= 1 << b
		case platform.PointerUp:
			rt.buttons &^= 1 << b
		}
	}
	p := float32(min(max(ev.Pressure, 0), 1))
	switch {
	case rt.buttons == 0:
		p = 0
	case p == 0:
		p = 0.5
	}
	rt.pressureMoved = ev.Kind == platform.PointerMove && p != rt.pressure
	rt.pointerType, rt.pressure = PointerType(ev.Pointer), p
	if s := rt.pressed; s != nil && ev.Kind == platform.PointerMove {
		s.pressPointer, s.pressPressure = rt.pointerType, p
	}
}

// pointer returns where the pointer is in the window as the element sees
// it: where it let go of the element while the release waits
// (releaseLater), else where it is.
func (s *state) pointer(rt *engine) (x, y float32) {
	if r := &s.releaseLater; r.due {
		return r.x, r.y
	}
	return rt.pointerX, rt.pointerY
}

const interactive = flagClickable | flagFocusable | flagEditable | flagSelectable | flagDragWindow | flagDraggable | flagTrackPointer

func (rt *engine) pointerDown(x, y float32, button int, mods Modifiers, clicks int) {
	rt.downing++
	defer rt.endDown()
	rt.observePointer(InputEvent{Kind: InputPointerDown, Button: button, Mods: mods, Clicks: max(clicks, 1)})
	chain := rt.hitChain(x, y)
	rt.setHover(chain)
	rt.pressDragger = 0
	rt.pressEnded = nil
	if len(chain) > 0 {
		rt.downs = append(rt.downs, chain[0])
	} else {
		rt.downs = append(rt.downs, 0)
	}
	if button == 0 && rt.scrollbarPress(chain, x, y) {
		return
	}
	var target, focus *state
	for _, id := range chain {
		s := rt.states[id]
		if s == nil {
			continue
		}
		if target == nil && s.flags&interactive != 0 {
			target = s
		}
		if focus == nil && s.flags&(flagFocusable|flagEditable|flagSelectable) != 0 && s.flags&flagDisabled == 0 {
			focus = s
		}
	}
	keep := slices.ContainsFunc(chain, func(id uint64) bool {
		s := rt.states[id]
		return s != nil && s.flags&flagKeepFocus != 0
	})
	if button == 0 && !keep {
		newFocus := uint64(0)
		if focus != nil {
			newFocus = focus.id
		}
		if newFocus != rt.focused {
			rt.focused = newFocus
			rt.focusVisible = false
			rt.blinkStart = rt.now()
		}
	}
	h := rt.handler(chain)
	clicks = rt.countPress(chain, target, h, max(clicks, 1))
	if h != nil && rt.deliver(h, InputEvent{Kind: InputPointerDown, Button: button, Mods: mods, Clicks: clicks}) {
		// It takes the moves and the release.
		rt.pressed, rt.pressButton, rt.pressClicks = h, button, clicks
		h.pressed, h.pressButton = true, button
		h.pressPointer, h.pressPressure = rt.pointerType, rt.pressure
		h.pressPending = true
		h.pressX, h.pressY = h.local(x, y)
		h.drag.reset(rt.now(), x, y)
		return
	}
	if button == 1 && rt.menuPress(chain, x, y) {
		return
	}
	rt.requestFrame()
	if target == nil {
		return
	}
	if button == 0 && target.flags&flagMenuButton != 0 && target.flags&flagDisabled == 0 {
		// A menu button's menu opens as the button goes down, and takes
		// the release.
		rt.openMenuButton(target, false)
		return
	}
	if button == 0 && target.flags&flagDragWindow != 0 && target.flags&(interactive&^flagDragWindow) == 0 {
		if clicks == 2 {
			rt.host.titleBarDoubleClicked()
		} else {
			rt.host.startDrag()
		}
		return
	}
	rt.pressed, rt.pressButton, rt.pressClicks = target, button, clicks
	rt.pressDragger = pressDragger(rt, chain, target)
	target.pressed, target.pressButton, target.pressMods = true, button, mods
	target.pressPointer, target.pressPressure = rt.pointerType, rt.pressure
	target.pressPending = true
	target.pressX, target.pressY = target.local(x, y)
	target.drag.reset(rt.now(), x, y)
	if button == 0 && rt.pressTextSelection(target, x, y, mods, clicks) {
		return
	}
	if target.editor != nil {
		target.editor.pressMods = mods
		target.editor.press(target.pressX, target.pressY, clicks, button)
	}
	if clicks == 2 && button == 0 {
		target.doubleClicks++
	}
}

// endPress ends the press whose release will not come, as the window lost
// the focus, or the element pressed is gone: input, the handler of the
// element pressed, gets one now, at x, y in its own axes. The element
// gets no click, and the next press starts anew. The press ends before
// the handler runs, so that a release or press coming meanwhile, through
// a main loop the handler runs nested, is the next press's.
func (rt *engine) endPress(input func(InputEvent) bool, x, y float32) {
	p := rt.pressed
	p.pressed, p.canceled = false, true
	rt.pressed = nil
	rt.requestFrame()
	if input != nil {
		input(InputEvent{Kind: InputPointerUp, X: x, Y: y, Button: rt.pressButton, Mods: rt.mods, Clicks: rt.pressClicks, Canceled: true, Pointer: rt.pointerType, Time: rt.now()})
	}
}

// endDown applies the releases that came while pointerDown handled a
// press.
func (rt *engine) endDown() {
	if rt.downing--; rt.downing > 0 {
		return
	}
	for len(rt.ups) > 0 {
		up := rt.ups[0]
		rt.ups = rt.ups[1:]
		if up.canceled {
			rt.pointerCancel(up.button)
		} else {
			rt.pointerUp(up.button, up.clicks, false)
		}
	}
}

// pendingUp is a release that came while pointerDown handled a press
// (engine.ups): of button, with its count of clicks, or, canceled, the
// system taking the press, of button the lowest of those held then, -1
// for none.
type pendingUp struct {
	button, clicks int
	canceled       bool
}

// pointerCancel ends the press the system took (platform.PointerCancel)
// as a release of its button that commits nothing: no click, no drop,
// no end of a drag, no context menu. button is the lowest of the buttons
// held as it came, for observers and the menus when no element is
// pressed, or -1 when none was: then nothing is pressed, and nothing
// happens.
func (rt *engine) pointerCancel(button int) {
	if rt.downing > 0 {
		rt.ups = append(rt.ups, pendingUp{button: button, canceled: true})
		return
	}
	switch {
	case rt.scrollDrag.st != nil:
		button = 0
	case rt.pressed != nil:
		button = rt.pressButton
	}
	if button < 0 {
		return
	}
	rt.pointerUp(button, 0, true)
}

// countPress counts the backend's n-th quick press from the first on the
// element pressed: the innermost of the target and the handler, so that
// two handlers in one focusable element, or two buttons in one handler,
// count apart.
func (rt *engine) countPress(chain []uint64, target, h *state, n int) int {
	id := uint64(0)
	for _, c := range chain {
		if target != nil && c == target.id || h != nil && c == h.id {
			id = c
			break
		}
	}
	lp := &rt.lastPress
	if id != lp.prev || n <= lp.base {
		lp.base = n - 1
	}
	lp.id = id
	return n - lp.base
}

// pointerUp releases button, counting clicks; canceled, the system took
// the press (pointerCancel), which the release then ends without
// committing what it would.
func (rt *engine) pointerUp(button, clicks int, canceled bool) {
	if rt.downing > 0 {
		rt.ups = append(rt.ups, pendingUp{button: button, clicks: clicks})
		return
	}
	rt.observePointer(InputEvent{Kind: InputPointerUp, Button: button, Mods: rt.mods, Clicks: max(clicks, 1), Canceled: canceled})
	if button == rt.pressButton {
		clicks = rt.pressClicks // the engine's, which may have started over
	}
	if s := rt.scrollDrag.st; s != nil {
		rt.scrollDrag.st = nil
		if s.snap != nil {
			// Let go, it rests at the nearest place.
			s.snap.scrolled(rt, s, 0)
		}
		// The pointer may have left the bar, over what the hover missed.
		rt.setHover(rt.hitChain(rt.pointerX, rt.pointerY))
		rt.updateCursor()
		rt.requestFrame()
		return
	}
	if s := rt.pressed; s != nil && button == rt.pressButton {
		if d := rt.drag; canceled && d != nil && !d.native {
			// Given up, as with Escape: dropped nowhere, and not missed. A
			// native drag's session has the pointer, and ends it.
			d.canceled, d.over = true, 0
		}
		if rt.dragEnd() {
			// A drag ended, dropping its value: no click.
			rt.pressed = nil
			s.pressed = false
			if canceled {
				s.canceled = true
			}
			rt.setHover(rt.hitChain(rt.pointerX, rt.pointerY))
			return
		}
	}
	if p := rt.pressed; p != nil && p.input != nil {
		rt.deliver(p, InputEvent{Kind: InputPointerUp, Button: button, Mods: rt.mods, Clicks: max(clicks, 1), Canceled: canceled})
	}
	if button == 1 && canceled && rt.menu.release != 0 {
		// The menu waiting for the release opens for none.
		rt.menu.release, rt.menu.releaseText = 0, 0
		return
	}
	if button == 1 && !canceled && rt.menuRelease() {
		return
	}
	s := rt.pressed
	if s == nil || button != rt.pressButton {
		return
	}
	if rt.selection.dragging {
		rt.moveTextSelection(rt.pointerX, rt.pointerY)
		rt.selection.dragging = false
	}
	rt.pressed = nil
	r := &s.releaseLater
	r.due, r.x, r.y = true, rt.pointerX, rt.pointerY
	// A press of the primary button that moved ends a drag; a click does
	// not, nor does a press the system took.
	r.released = button == 0 && s.drag.n > 1 && !canceled
	r.canceled = canceled
	v := s.drag.velocities(rt.now())
	s.releaseVX, s.releaseVY, s.releaseWX, s.releaseWY = v[0], v[1], v[2], v[3]
	if ed := s.editor; ed != nil {
		if ed.dragging {
			// Its selection ends where the pointer let go, as the text
			// selection's does, before the keys that come after.
			ed.drag(s.local(rt.pointerX, rt.pointerY))
		}
		ed.release()
	}
	inside := rt.contains(s, Rect{s.x, s.y, s.w, s.h}, rt.pointerX, rt.pointerY)
	if inside && s.flags&flagDisabled == 0 && !canceled {
		switch button {
		case 0:
			if s.dragRec.dragged(rt, s) {
				break // it moved the element (Draggable): no click
			}
			s.click(s.pressMods)
		case 1:
			r.rightClicks++
		}
	}
	// Moves no pass read yet (Dragged, or the pointer while Pressed) come
	// with the element still pressed, and the pass that reads them applies
	// the release as it ends (forgetInput); with none waiting, it applies
	// now.
	if s.dragX == 0 && s.dragY == 0 {
		rt.applyRelease(s)
	} else {
		rt.pressEnded = s
	}
	rt.setHover(rt.hitChain(rt.pointerX, rt.pointerY))
	rt.requestFrame()
}

// applyRelease gives s what the release of its press does, as frames see
// it. A press that came since the release keeps it pressed.
func (rt *engine) applyRelease(s *state) {
	r := &s.releaseLater
	s.pressed = rt.pressed == s
	s.released, s.canceled = r.released, r.canceled
	s.clicks += r.clicks
	s.rightClicks += r.rightClicks
	if r.clicks > 0 {
		s.clickMods = r.clickMods
	}
	r.due, r.canceled, r.clicks, r.rightClicks = false, false, 0, 0
	if rt.pressEnded == s {
		rt.pressEnded = nil
	}
}

// click clicks s with mods held, for the next pass to see: after the
// release of its press while that waits (releaseLater), so that the clicks
// come in order, with the last one's modifiers.
func (s *state) click(mods Modifiers) {
	if r := &s.releaseLater; r.due {
		r.clicks++
		r.clickMods = mods
		return
	}
	s.clicks++
	s.clickMods = mods
}

func (rt *engine) scroll(dx, dy float32, mods Modifiers, precise, momentum bool) {
	chain := rt.appendHitChain(rt.chain[:0], rt.pointerX, rt.pointerY)
	rt.chain = chain[:0]
	// From the innermost element out, the first handling its input to
	// take it: one whose handler leaves it passes it on, as it would
	// without the handler, to the handlers around it, as an editor
	// scrolls under what it shows over its text. Scroll containers move
	// only when no handler takes it, so a handler around one, as a canvas
	// that zooms with Ctrl and the wheel, still sees it first.
	ev := InputEvent{Kind: InputScroll, DX: dx, DY: dy, Mods: mods, Precise: precise, Momentum: momentum}
	for _, id := range chain {
		if s := rt.states[id]; s != nil && s.input != nil && s.flags&flagDisabled == 0 && rt.deliver(s, ev) {
			return
		}
	}
	// Shift turns the wheel sideways: down is toward the end, which is
	// to the left right to left.
	sideways := mods&Shift != 0 && dx == 0
	if sideways {
		dx, dy = dy, 0
	}
	// A wheel's notches glide, as browsers scroll them; a touchpad's
	// small moves already follow the fingers.
	glide := !precise && !rt.preferences().ReduceMotion
	for _, id := range chain {
		s := rt.states[id]
		if s == nil {
			continue
		}
		sx, sy := s.vector(rt.pointerX, rt.pointerY, dx, dy)
		if sideways && s.rtl {
			sx = -sx
		}
		if !precise {
			// A notch goes to the next place of a container that snaps.
			if moved, took := rt.snapNotch(s, sx, sy); took {
				if moved {
					return
				}
				continue
			}
		}
		if glide {
			if wheelMoves(s, sx, sy) {
				rt.wheelScroll(s, sx, sy)
				return
			}
			continue
		}
		s.wheel = nil
		if scrollBy(s, sx, sy) {
			if s.snap != nil {
				// It rests at a place once the fingers, and the momentum
				// of their fling, settle.
				s.snap.scrolled(rt, s, snapSettle)
			}
			rt.requestFrame()
			return
		}
	}
}

// zoom gives a pinch to the element under the pointer that handles its
// input; no element zooms of its own.
func (rt *engine) zoom(scale float32, mods Modifiers) {
	if scale <= 0 || scale == 1 {
		return
	}
	chain := rt.appendHitChain(rt.chain[:0], rt.pointerX, rt.pointerY)
	rt.chain = chain[:0]
	rt.deliver(rt.handler(chain), InputEvent{Kind: InputZoom, Scale: scale, Mods: mods})
}

// dragTo returns the offset of content whose thumb moved by moved DIPs
// along a track with travel DIPs of room since the offset was from, the
// content scrolling as far as reach when the drag started and as far as
// now: the thumb goes as far through the content as along the track, as
// rows a List measures meanwhile change the size of its content, and the
// end of the track shows the end.
func dragTo(from float64, moved, travel float32, reach, now float64) float64 {
	if reach <= 0 || now <= 0 {
		return 0
	}
	along := from/reach + float64(moved)/float64(travel)
	return max(0, min(along, 1)) * now
}

// bars returns the scroll bars of the container being dragged, with the
// content's size as the drag started.
func (d *scrollDrag) bars(width float32) scrollGeometry {
	s := d.st
	return scrollBars(Rect{s.x, s.y, s.w, s.h}, s.barInset, float32(d.contentW), float32(d.contentH), float32(s.scrollX), float32(s.scrollY), s.flags, width, s.rtl)
}

// scrollBy scrolls a container by dx, dy within its content, and reports
// whether it moved. dx moves the content's view to the right, which,
// right to left, is back toward its start.
func scrollBy(s *state, dx, dy float32) bool {
	if s.rtl {
		dx = -dx
	}
	x, y := s.scrollX, s.scrollY
	if dy != 0 && s.flags&flagScrollY != 0 {
		y = max(0, min(y+float64(dy), s.contentH-float64(s.h)))
	}
	if dx != 0 && s.flags&flagScrollX != 0 {
		x = max(0, min(x+float64(dx), s.contentW-float64(s.w)))
	}
	if x == s.scrollX && y == s.scrollY {
		return false
	}
	if s.track != nil {
		s.track.glide = nil // the user takes over from ScrollTo
	}
	s.haltSnap()
	s.scrollTo(x, y)
	return true
}

// scrollKey scrolls with the keys that scroll pages in browsers the
// innermost container around the focus that can go that way, or under the
// pointer without a focus, and reports whether one moved.
func (rt *engine) scrollKey(mods Modifiers, key Key) bool {
	const line, far = 40, 1e9
	var dx, dy, page float32
	switch {
	case mods == 0 && key == KeyDown:
		dy = line
	case mods == 0 && key == KeyUp:
		dy = -line
	case mods == 0 && key == KeyRight:
		dx = line
	case mods == 0 && key == KeyLeft:
		dx = -line
	case mods == 0 && (key == KeyPageDown || key == KeySpace):
		page = 1
	case mods == 0 && key == KeyPageUp, mods == Shift && key == KeySpace:
		page = -1
	case (mods == 0 || mods == Cmd) && key == KeyHome, mods == Cmd && key == KeyUp && runtime.GOOS == "darwin":
		dy = -far
	case (mods == 0 || mods == Cmd) && key == KeyEnd, mods == Cmd && key == KeyDown && runtime.GOOS == "darwin":
		dy = far
	default:
		return false
	}
	scroller := func(id uint64) bool {
		s := rt.states[id]
		return s != nil && s.flags&(flagScrollX|flagScrollY) != 0
	}
	chain := rt.focusChain()
	if !slices.ContainsFunc(chain, scroller) && rt.pointerIn {
		chain = rt.hitChain(rt.pointerX, rt.pointerY)
	}
	for _, id := range chain {
		if !scroller(id) {
			continue
		}
		s := rt.states[id]
		if page != 0 {
			// A page keeps a line of the last one in view.
			dy = page * max(s.h-line, s.h/2)
		}
		if moved, took := rt.snapNotch(s, dx, dy); took {
			// To the next place of a container that snaps.
			if moved {
				return true
			}
			continue
		}
		if scrollBy(s, dx, dy) {
			rt.requestFrame()
			return true
		}
	}
	return false
}

// focusChain returns the focused element and its ancestors, innermost
// first.
func (rt *engine) focusChain() []uint64 { return rt.appendFocusChain(nil) }

// appendFocusChain appends focusChain() to chain.
func (rt *engine) appendFocusChain(chain []uint64) []uint64 {
	for s := rt.states[rt.focused]; s != nil; s = rt.states[s.parent] {
		chain = append(chain, s.id)
		if s.parent == 0 {
			break
		}
	}
	return chain
}

// claimed reports whether an element around the focus, or the window,
// handles the key as a shortcut.
func (rt *engine) claimed(k keyEvent) bool { return rt.claimedBy(k, true) }

// claimedBy reports whether an element around the focus handles the key as
// a shortcut, or, when window is set, the window.
func (rt *engine) claimedBy(k keyEvent, window bool) bool {
	var chain []uint64
	for _, r := range rt.regs {
		if r.mods != k.mods || r.key != k.key || r.overlay {
			continue // an overlay takes what the focus leaves
		}
		if r.id == 0 {
			if window {
				return true
			}
			continue
		}
		if chain == nil {
			chain = rt.focusChain()
		}
		if slices.Contains(chain, r.id) {
			return true
		}
	}
	return false
}

// keyDown handles a key pressed, and reports whether an element took it as
// it came (HandleInput).
func (rt *engine) keyDown(mods Modifiers, key Key, repeat bool) bool {
	return rt.keyDownPhysical(mods, key, KeyUnknown, CodeUnknown, repeat)
}

// keyDownPhysical is keyDown with the key by position (InputEvent.PhysicalKey).
// A chord of a key the layout does not type as a US key, as S on a Cyrillic
// layout, matches shortcuts by that position; plain typing is untouched.
func (rt *engine) keyDownPhysical(mods Modifiers, key, physical Key, code KeyCode, repeat bool) bool {
	if platform.KeyByPosition(platform.Key(key), platform.Key(physical), platform.Modifiers(mods)) {
		if rt.byPosition == nil {
			rt.byPosition = map[Key]bool{}
		}
		rt.byPosition[physical] = true
		key = physical
	} else if !repeat {
		delete(rt.byPosition, physical)
	}
	if rt.inspectKey(mods, key) {
		return true
	}
	rt.observeFocus(InputEvent{Kind: InputKeyDown, Key: key, PhysicalKey: physical, Code: code, Mods: mods, Repeat: repeat})
	k := keyEvent{mods, key, repeat}
	if key == KeyEscape && mods == 0 && rt.dragCancel() {
		return true
	}
	if h := rt.focusHandler(); h != nil && !rt.claimed(k) && rt.deliver(h, InputEvent{Kind: InputKeyDown, Key: key, PhysicalKey: physical, Code: code, Mods: mods, Repeat: repeat}) {
		rt.blinkStart = rt.now()
		return true
	}
	if s := rt.states[rt.focused]; s != nil && s.editor != nil && s.editor.onTextKey != nil && s.flags&flagEditable != 0 &&
		!typesText(k) && (s.editor.wants(k) || !rt.claimed(k)) && s.editor.textKey(k) {
		// OnTextKey took it as it came, as HandleInput does, but after the
		// keys and text before it, which it reads.
		rt.blinkStart = rt.now()
		rt.requestFrame()
		return true
	}
	if (key == KeyContextMenu && mods == 0 || key == KeyF10 && mods == Shift) && !rt.claimed(k) && rt.menuKey() {
		return false
	}
	if s := rt.states[rt.focused]; s != nil && s.editor != nil && s.flags&(flagEditable|flagSelectable) != 0 && s.editor.wants(k) {
		if rt.textSelectionKey(s, k) {
			rt.requestFrame()
			return false
		}
		s.editor.queue = append(s.editor.queue, editEvent{kind: editKey, mods: mods, key: key})
		rt.blinkStart = rt.now()
		rt.requestFrame()
		// Taken: the menu item of the key, as Edit › Paste's Ctrl+V on
		// Windows, leaves it rather than pasting a second time.
		return true
	}
	if s := rt.states[rt.focused]; s != nil && s.flags&flagTypeSelect != 0 && !rt.claimed(k) {
		// The first letters of a row of a list: they add up while typed
		// quickly enough, with spaces once the first came.
		now := rt.now()
		if now.Sub(s.typedAt) >= typePause {
			s.typed = ""
		}
		if r, ok := typedRune(mods, key); ok && (r != ' ' || s.typed != "") {
			s.typed += string(r)
			s.typedAt = now
			s.markTyping()
			rt.requestFrame()
			return false
		}
	}
	if !rt.claimedBy(k, false) && rt.groupKey(mods, key) {
		return false
	}
	if key == KeyTab && (mods == 0 || mods == Shift) && !rt.claimed(k) {
		rt.moveFocus(mods == Shift)
		rt.requestFrame()
		return false
	}
	if s := rt.states[rt.focused]; s != nil && s.flags&flagMenuButton != 0 && s.flags&flagDisabled == 0 && !rt.claimedBy(k, false) &&
		(mods == 0 && (key == KeyEnter || key == KeySpace || key == KeyDown || key == KeyUp) || mods == Alt && key == KeyDown) {
		rt.focusVisible = true
		if key == KeyUp {
			rt.openMenuButtonOnLast(s)
		} else {
			rt.openMenuButton(s, true)
		}
		return false
	}
	if (key == KeyEnter || key == KeySpace) && mods == 0 {
		// A focused button takes them before the window's shortcuts,
		// such as a dialog's default button; a toggle takes Space before
		// them, and Enter after. A link takes Enter alone, as in a web
		// page: Space scrolls as it does elsewhere.
		s := rt.states[rt.focused]
		window := s != nil && key == KeyEnter && s.flags&flagToggle != 0
		if s != nil && !rt.claimedBy(k, window) && s.flags&flagClickable != 0 && s.flags&flagDisabled == 0 &&
			(key == KeyEnter || s.role != RoleLink) {
			s.click(0)
			rt.focusVisible = true
			rt.requestFrame()
			return false
		}
	}
	// Space types a space where the focus takes text, rather than
	// paging, as in a web page's field: the text comes after the key.
	if !rt.claimed(k) && !(typesText(k) && rt.focusTakesText()) && rt.scrollKey(mods, key) {
		return false
	}
	rt.keys = append(rt.keys, k)
	rt.requestFrame()
	return false
}

// typePause is how long after a letter typed to choose a row the next
// one starts anew.
const typePause = time.Second

// typedRune returns the letter, digit or space a key types without
// modifiers but Shift, in lowercase, for choosing a row by its text.
func typedRune(mods Modifiers, key Key) (rune, bool) {
	switch {
	case mods&^Shift != 0:
		return 0, false
	case key >= KeyA && key <= KeyZ:
		return 'a' + rune(key-KeyA), true
	case key >= Key0 && key <= Key9:
		return '0' + rune(key-Key0), true
	case key == KeySpace:
		return ' ', true
	}
	return 0, false
}

// moveFocus focuses the next (or previous) element that takes the focus.
func (rt *engine) moveFocus(back bool) {
	// The dialog on top keeps the focus among its elements.
	order := rt.focusOrder
	if m := rt.modal; m != 0 {
		order = nil
		for i, sc := range rt.focusScopes {
			if sc.modal == m {
				order = append(order, rt.focusOrder[i])
			}
		}
	}
	n := len(order)
	if n == 0 {
		return
	}
	i := -1
	for j, id := range order {
		if id == rt.focused {
			i = j
			break
		}
	}
	// Tab stops once in a focus group, at its entry, and leaves it.
	g := rt.groupOf(rt.focused)
	d := 1
	if back {
		d = -1
	}
	next := -1
	for k := 1; k <= n; k++ {
		j := (i + d*k + 2*n) % n
		if i < 0 {
			j = (k - 1) % n
			if back {
				j = n - k
			}
		}
		if id := order[j]; rt.tabStop(id) && (g == 0 || rt.groupOf(id) != g) {
			next = j
			break
		}
	}
	if next < 0 {
		return // the group is all there is
	}
	rt.focused = order[next]
	rt.focusVisible = true
	rt.blinkStart = rt.now()
	s := rt.states[rt.focused]
	if s != nil && s.editor != nil {
		s.editor.selectAll()
	}
	rt.reveal(rt.focused)
	if s != nil && s.webView != nil {
		// Its page takes the keyboard, and Tab leaving it moves on from
		// here (webViewTabOut).
		rt.host.tabIntoWebView(s.webView, back)
	}
}

// routeKeys delivers the keys pressed since the last frame to the
// innermost element around the focus that handles them, else to the
// overlay on top that does, else to the window's shortcuts, as the last
// frame registered them. Those it finds no shortcut for wait for what
// this frame builds (settleKeys). An element takes one key a frame: from
// a second key for one, the keys wait for the next frame, which the
// first changed, as a Down and an Enter typed quickly into a menu, the
// Enter choosing the item the Down highlighted.
func (rt *engine) routeKeys() {
	rt.unrouted = rt.unrouted[:0]
	rt.shortcutRepeat = false
	if len(rt.keys) == 0 && len(rt.retried) == 0 {
		return
	}
	chain := rt.focusChain()
	route := func(k keyEvent, retried bool) bool {
		r := shortcutReg{mods: k.mods, key: k.key, retried: retried, repeat: k.repeat}
		if target, found := routeKey(rt.regs, chain, k); found {
			if slices.ContainsFunc(rt.delivered, func(d shortcutReg) bool { return d.id == target }) {
				return false
			}
			r.id = target
			rt.delivered = append(rt.delivered, r)
		} else {
			rt.unrouted = append(rt.unrouted, r)
		}
		return true
	}
	// Keys retried came before those pressed since.
	var later []keyEvent
	for i, k := range rt.retried {
		if !route(k, true) {
			later = append(later, rt.retried[i:]...)
			break
		}
	}
	for i, k := range rt.keys {
		if later != nil || !route(k, false) {
			later = append(later, rt.keys[i:]...)
			break
		}
	}
	rt.retried = rt.retried[:0]
	rt.keys = append(rt.keys[:0], later...)
}

// routeKey returns the element that takes key k by shortcuts regs, the
// focus in the elements of chain: the innermost of them, else the overlay
// on top, else the window (0). found is false when none does.
func routeKey(regs []shortcutReg, chain []uint64, k keyEvent) (target uint64, found bool) {
	for _, id := range chain {
		for _, r := range regs {
			if r.id == id && !r.overlay && r.mods == k.mods && r.key == k.key {
				return id, true
			}
		}
	}
	// The overlay on top: the one made last, as it paints last.
	var top int32 = -1
	for _, r := range regs {
		if r.overlay && r.mods == k.mods && r.key == k.key && r.serial >= top {
			target, found, top = r.id, true, r.serial
		}
	}
	if found {
		return target, true
	}
	for _, r := range regs {
		if r.id == 0 && r.mods == k.mods && r.key == k.key {
			return 0, true
		}
	}
	return 0, false
}

// settleKeys decides, once the frame is built, about the keys no shortcut
// took in it: those the frame found no shortcut for, or whose shortcut
// the view did not ask for again. The shortcuts the frame registered take
// one in the next frame, as the view had changed by the time it came: a
// key pressed as the view changes, before a frame shows the change (an
// Escape typed ahead into a flyout whose first frame builds a shortcut for
// it), goes where the view shows it goes. Else an Escape is the window's
// (a flyout closes on it), and other keys go nowhere.
func (rt *engine) settleKeys() {
	if len(rt.unrouted) == 0 {
		return
	}
	chain := rt.focusChain()
	for _, r := range rt.unrouted {
		k := keyEvent{r.mods, r.key, r.repeat}
		if _, found := routeKey(rt.nextRegs, chain, k); found && !r.retried {
			rt.retried = append(rt.retried, k) // runFrame asks for the frame
		} else if !found && k.mods == 0 && k.key == KeyEscape {
			rt.host.escape()
		}
	}
	rt.unrouted = rt.unrouted[:0]
}

// shortcut registers that element id (0 for the window) handles mods+key
// and reports whether such a key was delivered to it.
func (rt *engine) shortcut(id uint64, mods Modifiers, key Key) bool {
	rt.nextRegs = append(rt.nextRegs, shortcutReg{id: id, mods: mods, key: key})
	for i, d := range rt.delivered {
		if d.id == id && d.mods == mods && d.key == key {
			rt.delivered = append(rt.delivered[:i], rt.delivered[i+1:]...)
			rt.consumed, rt.shortcutRepeat = true, d.repeat
			return true
		}
	}
	return false
}

// editEvent delivers text, a composition or an edit command to the focus,
// and reports whether an element took it.
func (rt *engine) editEvent(ev editEvent) bool {
	if rt.observers > 0 {
		kind := InputText
		if ev.kind == editCompose {
			kind = InputCompose
		} else if ev.kind == editCommand {
			kind = InputCommand
		}
		rt.observeFocus(InputEvent{Kind: kind, Text: ev.text, Caret: ev.caret})
	}
	if s := rt.states[rt.focused]; s != nil && s.textClient != nil && rt.windowFocus() && s.flags&flagDisabled == 0 && (s.editor == nil || !s.editor.readOnly) {
		if ev.kind == editInsert || ev.kind == editCompose {
			var r *TextInputRange
			if ev.replace {
				v := TextInputRange{Start: ev.from, End: ev.to}
				r = &v
			}
			if ev.kind == editInsert {
				s.textAdapter.ReplaceText(r, ev.text)
			} else {
				caret := platform.UTF16Len(string([]rune(ev.text)[:max(0, min(ev.caret, len([]rune(ev.text))))]))
				s.textAdapter.SetMarkedText(r, ev.text, TextInputRange{Start: caret, End: caret})
			}
			return true
		}
	}
	if h := rt.focusHandler(); h != nil && h.editor == nil {
		kind := map[editKind]InputKind{editInsert: InputText, editCompose: InputCompose, editCommand: InputCommand}[ev.kind]
		handled := kind != 0 && rt.deliver(h, InputEvent{Kind: kind, Text: ev.text, Caret: ev.caret})
		if !handled && kind == InputCommand && ev.text == "pasteAndMatchStyle" {
			// A handler of text without styles pastes it as Paste does.
			handled = rt.deliver(h, InputEvent{Kind: kind, Text: "paste"})
		}
		if handled {
			rt.blinkStart = rt.now()
		} else {
			rt.commandKey(ev, nil)
		}
		return handled
	}
	s := rt.states[rt.focused]
	if s == nil || s.editor == nil {
		rt.commandKey(ev, nil)
		return false
	}
	// Selectable text takes the menus' commands, as Copy, not text.
	if s.flags&flagEditable == 0 && (s.flags&flagSelectable == 0 || ev.kind != editCommand) {
		rt.commandKey(ev, nil)
		return false
	}
	if s.editor.readOnly && rt.commandKey(ev, s.editor) {
		return true
	}
	if ev.kind == editCommand && rt.textSelectionCommand(s, ev.text) {
		rt.requestFrame()
		return true
	}
	s.editor.queue = append(s.editor.queue, ev)
	rt.blinkStart = rt.now()
	rt.requestFrame()
	return true
}

// commandKeys are the keys of the Edit menu's commands.
var commandKeys = map[string]keyEvent{
	"copy": {mods: Cmd, key: KeyC}, "cut": {mods: Cmd, key: KeyX}, "paste": {mods: Cmd, key: KeyV}, "selectAll": {mods: Cmd, key: KeyA},
	"undo": {mods: Cmd, key: KeyZ}, "redo": {mods: Cmd | Shift, key: KeyZ},
}

// commandKey has an edit command that the focus, or ed, doesn't take go to
// the shortcuts of its key, and reports whether it did. On macOS the Edit
// menu takes Command+V and the others before the window sees the key, so a
// window's Cmd+V would otherwise run only from a text input; elsewhere the
// key comes first, and the menu only after.
func (rt *engine) commandKey(ev editEvent, ed *editor) bool {
	k, ok := commandKeys[ev.text]
	if runtime.GOOS != "darwin" || ev.kind != editCommand || !ok || ed != nil && ed.wants(k) {
		return false
	}
	k.repeat = ev.repeat
	rt.keys = append(rt.keys, k)
	rt.requestFrame()
	return true
}

// commandEnabled reports whether the edit command name would act where
// editEvent takes it, for the Edit menu to enable its item.
func (rt *engine) commandEnabled(name string) bool {
	s := rt.states[rt.focused]
	// As focusHandler, without windowFocus, which would have the next
	// change of the window's focus draw a frame.
	if s != nil && s.input != nil && s.editor == nil && rt.windowFocused {
		// Such an element tells what it does with a command only by
		// doing it.
		return true
	}
	if s == nil || s.editor == nil || s.flags&(flagEditable|flagSelectable) == 0 {
		return rt.commandShortcut(name)
	}
	if k, ok := commandKeys[name]; ok && s.editor.readOnly && !s.editor.wants(k) {
		return rt.commandShortcut(name)
	}
	return rt.editCommandOn(s, name)
}

// commandShortcut reports whether commandKey would have a shortcut take
// the edit command name.
func (rt *engine) commandShortcut(name string) bool {
	k, ok := commandKeys[name]
	if runtime.GOOS != "darwin" || !ok {
		return false
	}
	_, found := routeKey(rt.regs, rt.focusChain(), k)
	return found
}

// imeContext is how many runes around the selection input methods see.
const imeContext = 512

// updateTextInput tells the host where text input goes, and the text
// around the caret.
func (rt *engine) updateTextInput() {
	var t platform.TextInputState
	base := 0
	if s := rt.states[rt.focused]; s != nil && s.textClient != nil && rt.windowFocus() && s.flags&flagDisabled == 0 && (s.editor == nil || !s.editor.readOnly) {
		t.Active, t.Client, t.NoInputMethod = true, s.textAdapter, s.noIME
		sel := s.textAdapter.Selection()
		caret := sel.Caret()
		if bounds, _, ok := s.textAdapter.BoundsForRange(TextInputRange{Start: caret, End: caret}); ok {
			t.Caret = bounds
		} else {
			r := transformRect(s.world, Rect{X: s.x, Y: s.y, W: 1, H: s.h})
			t.Caret = rt.toWindow(platform.RectF{X: float64(r.X), Y: float64(r.Y), W: float64(r.W), H: float64(r.H)})
		}
	} else if s != nil && s.editor == nil && s.input != nil && s.takesText && rt.windowFocus() {
		// An element taking text itself: no text around the caret.
		t.Active, t.NoInputMethod = true, s.noIME
		r := transformRect(s.world, Rect{s.x + s.caret.X, s.y + s.caret.Y, s.caret.W, s.caret.H})
		t.Caret = rt.toWindow(platform.RectF{X: float64(r.X), Y: float64(r.Y), W: float64(r.W), H: float64(r.H)})
	}
	if t != rt.ime.state {
		if t.Client != rt.ime.state.Client {
			if old, ok := rt.ime.state.Client.(*textInputAdapter); ok {
				old.release()
			}
		}
		rt.ime.state, rt.ime.base = t, base
		rt.host.setTextInput(t)
	}
}

// replaced makes an edit replace the runes an input method named, from
// the text it was last given, rather than the selection.
func (rt *engine) replaced(ev editEvent, sev platform.SurfaceEvent) editEvent {
	if rt.ime.state.Client != nil {
		if sev.Replace {
			ev.replace, ev.from, ev.to = true, sev.From, sev.To
		}
		return ev
	}
	if sev.Replace && rt.ime.state.Active {
		n := len([]rune(rt.ime.state.Text))
		from, to := max(0, min(sev.From, n)), max(0, min(sev.To, n))
		ev.replace, ev.from, ev.to = true, rt.ime.base+min(from, to), rt.ime.base+max(from, to)
	}
	return ev
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// Clicked reports whether the element was clicked, with the primary
// button or by Enter or Space while focused, since the last frame.
func (e *node) Clicked() bool {
	if !e.hasState() {
		return false
	}
	e.flags |= flagClickable
	if e.disabled() || e.st.clicks == 0 {
		return false
	}
	e.c.rt.consumed = true
	return true
}

// Clicks returns how many times the element was clicked since the last
// frame.
func (e *node) Clicks() int {
	if !e.hasState() {
		return 0
	}
	e.flags |= flagClickable
	if e.disabled() {
		return 0
	}
	if e.st.clicks > 0 {
		e.c.rt.consumed = true
	}
	return e.st.clicks
}

// ClickModifiers returns the modifier keys held as the element was last
// clicked, none for a click by the keyboard: with Clicked, a click with
// Shift or Cmd does something else, as extending a choice.
func (e *node) ClickModifiers() Modifiers {
	if !e.hasState() {
		return 0
	}
	return e.st.clickMods
}

// DoubleClicked reports a double click on the element.
func (e *node) DoubleClicked() bool {
	if !e.hasState() {
		return false
	}
	e.flags |= flagClickable
	if e.disabled() || e.st.doubleClicks == 0 {
		return false
	}
	e.c.rt.consumed = true
	return true
}

// RightClicked reports a click with the secondary button, as for a
// context menu.
func (e *node) RightClicked() bool {
	if !e.hasState() {
		return false
	}
	e.flags |= flagClickable
	if e.disabled() || e.st.rightClicks == 0 {
		return false
	}
	e.c.rt.consumed = true
	return true
}

// The states the inspector can force on an element, as Chrome's :hov does.
const (
	forceHover uint8 = 1 << iota
	forceActive
	forceFocus
	forceFocusVisible
	forceFocusWithin
)

// Hovered reports whether the pointer is over the element. While the
// pointer presses an element, the elements it was over as the press began,
// as those around the element pressed, stay hovered as long as it is over
// them, as in CSS, and the others hover no more: a button that shows over
// a row stays as it is pressed, and dragging over other elements does not
// light them up.
func (e *node) Hovered() bool {
	if !e.hasState() {
		return false
	}
	e.flags |= flagHover
	if e.IsDisabled() {
		return false
	}
	rt := e.c.rt
	if rt.insp.isForced(e.id, forceHover) {
		return true
	}
	if !slices.Contains(rt.hover, e.id) {
		return false
	}
	if p := rt.pressed; p != nil && p.id != e.id {
		// The hover holds the elements under the pointer as the press
		// began.
		s := e.st
		return rt.pointerIn && rt.contains(s, Rect{s.x, s.y, s.w, s.h}, rt.pointerX, rt.pointerY)
	}
	return true
}

// pressMove asks for a frame when the pointer, pressing an element, moves
// in or out of an element around it that looks at its hover (Hovered).
func (rt *engine) pressMove(x0, y0, x1, y1 float32) {
	for _, id := range rt.hover {
		s := rt.states[id]
		if s == nil || s == rt.pressed || s.flags&flagHover == 0 {
			continue
		}
		r := Rect{s.x, s.y, s.w, s.h}
		if rt.contains(s, r, x0, y0) != rt.contains(s, r, x1, y1) {
			rt.requestFrame()
			return
		}
	}
}

// Pressed reports whether the element is being pressed with the pointer's
// primary button, unless it is disabled. A press of another button, as a
// right-click's, is no press here, as CSS's :active and drags on the web
// have it: PressedButton tells which button presses the element.
func (e *node) Pressed() bool {
	if !e.hasState() {
		return false
	}
	e.flags |= flagClickable | flagHover
	s := e.st
	rt := e.c.rt
	if rt.insp.isForced(e.id, forceActive) && !e.disabled() {
		return true
	}
	x, y := s.pointer(rt)
	if !s.pressedPrimary() || e.disabled() || !rt.contains(s, Rect{s.x, s.y, s.w, s.h}, x, y) {
		return false
	}
	if s.pressPending {
		// A press may choose a row after the view built its selection and
		// focus colors. Show that choice in this frame, as for a click,
		// without rebuilding again while the pointer remains held.
		rt.consumed = true
	}
	return true
}

// PressedButton returns the button of the pointer pressing the element, 0
// for the primary, 1 the secondary and 2 the middle, while the press
// lasts, wherever the pointer goes, as Dragged; ok is false while nothing
// presses it, or it is disabled. Pressed, Dragged, DragVelocity and
// DragEnded follow the primary button alone; an element that pans with
// the middle button, as a canvas, reads the button here and where the
// pointer goes with PointerPosition.
func (e *node) PressedButton() (button int, ok bool) {
	if !e.hasState() {
		return 0, false
	}
	e.flags |= flagClickable
	s := e.st
	if !s.pressed || e.disabled() {
		return 0, false
	}
	return s.pressButton, true
}

// PressedPointer returns the device pressing the element and how hard it
// presses, from 0 to 1, as of the press's last move, while the press
// lasts, as PressedButton; ok is false while nothing presses it, or it is
// disabled. As InputEvent.Pressure, a device that tells no pressure, as a
// mouse, presses 0.5. A signature pad widens its stroke with a pen's
// pressure, and goes by the pointer's speed for a mouse. Elements asking
// for it get a frame whenever the pointer moves or the pressure changes.
func (e *node) PressedPointer() (pointer PointerType, pressure float32, ok bool) {
	if !e.hasState() {
		return 0, 0, false
	}
	e.flags |= flagClickable | flagTrackPointer
	s := e.st
	if !s.pressed || e.disabled() {
		return 0, 0, false
	}
	return s.pressPointer, s.pressPressure, true
}

// pressedPrimary reports whether the pointer's primary button presses s:
// what Pressed and Dragged report, and what moves sliders, drags and the
// like. Another button's press only clicks (RightClicked) or opens a
// context menu.
func (s *state) pressedPrimary() bool { return s.pressed && s.pressButton == 0 }

// Focused reports whether the element has the keyboard focus: for a field
// around an input, as SearchField's, whether its input has it.
func (e *node) Focused() bool {
	if e.hasState() && e.c.rt.insp.isForced(e.id, forceFocus|forceFocusVisible) {
		return true
	}
	if e != nil && e.focusTo != nil {
		return e.focusTo.Focused()
	}
	return e.hasState() && e.c.rt.focused == e.id && e.c.rt.windowFocus()
}

// FocusVisible reports whether the element has the keyboard focus and
// should show it, because it came from the keyboard.
func (e *node) FocusVisible() bool {
	if e.hasState() && e.c.rt.insp.isForced(e.id, forceFocusVisible) {
		return true
	}
	return e.Focused() && e.c.rt.focusVisible
}

// FocusWithin reports whether the element or one of its descendants has
// the keyboard focus.
func (e *node) FocusWithin() bool {
	if !e.hasState() {
		return false
	}
	rt := e.c.rt
	rt.withinRead = true
	if rt.insp.isForced(e.id, forceFocusWithin) {
		return true
	}
	for s := rt.states[rt.focused]; s != nil; s = rt.states[s.parent] {
		if s.id == e.id {
			return true
		}
		if s.parent == 0 {
			break
		}
	}
	return false
}

// Focus gives the element the keyboard focus. Called in every frame, it
// keeps it there; AutoFocus gives it once. A field around an input, as
// SearchField's, gives it to its input, where typing goes.
func (e *node) Focus() *node {
	if e != nil && e.focusTo != nil {
		e.focusTo.Focus()
		return e
	}
	if !e.hasState() {
		return e
	}
	e.flags |= flagFocusable
	rt := e.c.rt
	if rt.focused != e.id {
		rt.focused = e.id
		rt.blinkStart = rt.now()
	}
	return e
}

// AutoFocus gives the element the keyboard focus in the frame it appears,
// as the first field of a dialog.
func (e *node) AutoFocus() *node {
	if e.hasState() && e.st.born == e.c.rt.frame {
		e.Focus()
	}
	return e
}

// Shortcut reports whether the key with exactly the modifiers mods was
// pressed while the element or one of its descendants had the focus. The
// innermost element handling a key gets it; a disabled one handles none.
func (e *node) Shortcut(mods Modifiers, key Key) bool {
	if e.disabled() {
		return false
	}
	return e.c.rt.shortcut(e.id, mods, key)
}

// PointerPosition returns the pointer's position relative to the
// element's box and whether it is over the element. Elements asking for it
// get a frame whenever the pointer moves over them. The build that reads
// the last moves of a press on the element sees where the pointer let go.
func (e *node) PointerPosition() (x, y float32, over bool) {
	if !e.hasState() {
		return 0, 0, false
	}
	e.flags |= flagTrackPointer
	rt := e.c.rt
	s := e.st
	wx, wy := s.pointer(rt)
	x, y = s.local(wx, wy)
	return x, y, rt.pointerIn && rt.contains(s, Rect{s.x, s.y, s.w, s.h}, wx, wy)
}

// Dragged reports how far the pointer moved in the element's local axes
// since the last frame while pressing it with the primary button, as
// Pressed: a drag with another button moves nothing (PressedButton). The
// moves just before the pointer let go come while the element is still
// pressed, and the press ends in the build after.
func (e *node) Dragged() (dx, dy float32, ok bool) {
	if !e.hasState() {
		return 0, 0, false
	}
	e.flags |= flagDraggable
	s := e.st
	if !s.pressedPrimary() {
		return 0, 0, false
	}
	if s.dragX != 0 || s.dragY != 0 {
		e.c.rt.consumed = true
	}
	return s.dragX, s.dragY, true
}

// DragVelocity returns how fast, in DIPs per second along the element's
// local axes, the pointer pressing the element moves now: zero once it
// stops for a moment, or while nothing presses the element with the
// primary button (Dragged).
func (e *node) DragVelocity() (vx, vy float32) {
	if !e.hasState() {
		return 0, 0
	}
	e.flags |= flagDraggable
	s := e.st
	if !s.pressedPrimary() {
		return 0, 0
	}
	return s.drag.velocity(e.c.rt.now())
}

// DragEnded reports whether the pointer pressing the element with the
// primary button, and moving, let go since the last frame, with how fast
// it moved then, as DragVelocity: a spring taking over
// (SpringOptions.Velocity) keeps the fling going. A click, which does not
// move, ends no drag, nor does a drag with another button, nor a press
// the system canceled (PressCanceled).
func (e *node) DragEnded() (vx, vy float32, ok bool) {
	if !e.hasState() {
		return 0, 0, false
	}
	e.flags |= flagDraggable
	s := e.st
	if !s.released {
		return 0, 0, false
	}
	e.c.rt.consumed = true
	return s.releaseVX, s.releaseVY, true
}

// PressCanceled reports whether the press of the element ended since the
// last frame without the pointer letting go, as Pointer Events'
// pointercancel: the system took it (a browser turning a touch into the
// page's scrolling, the window losing the mouse capture or the pointer's
// grab, a menu taking the release), or the window lost the keyboard. Such
// a press clicks nothing (Clicked) and ends no drag (DragEnded), and
// Pressed turns false: undo what the press began, as a preview a drag
// showed, rather than commit it.
//
//	dx, _, held := handle.Dragged()
//	switch {
//	case held:
//		app.preview += dx
//	case handle.PressCanceled():
//		app.preview = 0
//	case app.preview != 0:
//		app.width, app.preview = app.width+app.preview, 0
//	}
func (e *node) PressCanceled() bool {
	if !e.hasState() || !e.st.canceled || e.st.pressed {
		return false
	}
	e.c.rt.consumed = true
	return true
}

// dragTrack keeps the last moves of a press, to tell how fast it goes: in
// the element's axes (x, y), and in the window's (wx, wy, from where the
// press began, ox and oy). far tells that the press went dragStart DIPs
// from there.
type dragTrack struct {
	x, y    float32
	ox, oy  float32
	wx, wy  float32
	far     bool
	samples [8]dragSample
	n, last int
}

type dragSample struct {
	at           time.Time
	x, y, wx, wy float32
}

// reset starts tracking a press at the window's point (x, y).
func (d *dragTrack) reset(now time.Time, x, y float32) {
	*d = dragTrack{ox: x, oy: y}
	d.add(now)
}

func (d *dragTrack) add(now time.Time) {
	d.last = (d.last + 1) % len(d.samples)
	d.samples[d.last] = dragSample{now, d.x, d.y, d.wx, d.wy}
	d.n = min(d.n+1, len(d.samples))
}

// move adds a move by dx, dy in the element's axes, wdx, wdy in the
// window's, at now.
func (d *dragTrack) move(now time.Time, dx, dy, wdx, wdy float32) {
	if wdx == 0 && wdy == 0 {
		return // as with the release's position
	}
	d.x += dx
	d.y += dy
	d.wx += wdx
	d.wy += wdy
	d.far = d.far || d.wx*d.wx+d.wy*d.wy >= dragStart*dragStart
	d.add(now)
}

// velocity returns how fast the press went over its last 100 ms in the
// element's axes, zero once it has not moved for 50 ms.
func (d *dragTrack) velocity(now time.Time) (float32, float32) {
	v := d.velocities(now)
	return v[0], v[1]
}

// windowVelocity returns how fast the press went, as velocity does, in
// the window's axes.
func (d *dragTrack) windowVelocity(now time.Time) (float32, float32) {
	v := d.velocities(now)
	return v[2], v[3]
}

// velocities returns how fast the press went in the element's axes and
// in the window's.
func (d *dragTrack) velocities(now time.Time) [4]float32 {
	if d.n < 2 {
		return [4]float32{}
	}
	newest := d.samples[d.last]
	if now.Sub(newest.at) > 50*time.Millisecond {
		return [4]float32{}
	}
	oldest := newest
	for i := 1; i < d.n; i++ {
		s := d.samples[(d.last-i+len(d.samples))%len(d.samples)]
		if newest.at.Sub(s.at) > 100*time.Millisecond {
			break
		}
		oldest = s
	}
	dt := float32(newest.at.Sub(oldest.at).Seconds())
	if dt <= 0 {
		return [4]float32{}
	}
	return [4]float32{(newest.x - oldest.x) / dt, (newest.y - oldest.y) / dt, (newest.wx - oldest.wx) / dt, (newest.wy - oldest.wy) / dt}
}

// Changed reports whether this widget's value changed in this build pass,
// by the input it took as its constructor returned.
func (e *node) Changed() bool {
	if e.hasState() {
		e.c.rt.applyInputs()
	}
	if e.hasState() && e.st.changed {
		e.c.rt.consumed = true
		return true
	}
	return false
}

// Submitted reports whether Enter was pressed in a single-line text input
// in this build pass.
func (e *node) Submitted() bool {
	if !e.hasState() {
		return false
	}
	e.c.rt.applyInputs()
	return e.st.submitted
}

// Dismissed reports whether pressing Escape or clicking outside closed the
// dialog or popover (Modal, DialogBase, Popover, PopoverBase) returning the
// element, in the frame it did, setting its *open to false: one that must
// stay open sets it back to true.
//
//	if ui.Modal(c, &app.editing, edit).Dismissed() && app.unsaved {
//		app.editing = true
//	}
func (e *node) Dismissed() bool { return e.dismissed }

// scrollbarPress starts dragging the thumb of the scroll container under
// the pointer when the press is on its scroll bar, or pages toward the
// press on the bar's track. Hidden bars take no press, as AppKit's
// overlay scrollers: it goes through to the content under them, as a
// click on a table's last row under its horizontal bar. The hover this
// press set does not show them yet, so they are those the last frame
// painted (barsShown).
func (rt *engine) scrollbarPress(chain []uint64, x, y float32) bool {
	for _, id := range chain {
		s := rt.states[id]
		if s == nil || s.flags&(flagScrollX|flagScrollY) == 0 || !s.barsShown || s.hideScrollbars {
			continue
		}
		lx, ly := s.local(x, y)
		lx += s.x
		ly += s.y
		g := scrollBars(Rect{s.x, s.y, s.w, s.h}, s.barInset, float32(s.contentW), float32(s.contentH), float32(s.scrollX), float32(s.scrollY), s.flags, s.barWidth, s.rtl)
		d := &rt.scrollDrag
		w, h := float64(s.w), float64(s.h)
		s.wheel = nil
		s.haltSnap()
		switch {
		case g.vertical && g.vTrack.Contains(lx, ly):
			switch {
			case ly >= g.v.Y && ly < g.v.Y+g.v.H:
				d.st, d.start, d.from, d.horizontal = s, ly, s.scrollY, false
				if s.track != nil {
					s.track.glide = nil
				}
				d.contentW, d.contentH = s.contentW, s.contentH
			case ly < g.v.Y:
				if _, took := rt.snapNotch(s, 0, float32(-h*0.9)); !took {
					s.scrollTo(s.scrollX, max(0, s.scrollY-h*0.9))
				}
			default:
				if _, took := rt.snapNotch(s, 0, float32(h*0.9)); !took {
					s.scrollTo(s.scrollX, min(s.contentH-h, s.scrollY+h*0.9))
				}
			}
		case g.horizontal && g.hTrack.Contains(lx, ly):
			// A page toward the end, as scrollBy takes it.
			page := float32(w * 0.9)
			if s.rtl {
				page = -page
			}
			switch {
			case lx >= g.h.X && lx < g.h.X+g.h.W:
				d.st, d.start, d.from, d.horizontal = s, lx, s.scrollX, true
				if s.track != nil {
					s.track.glide = nil
				}
				d.contentW, d.contentH = s.contentW, s.contentH
			case (lx < g.h.X) != s.rtl:
				// Toward the start.
				if _, took := rt.snapNotch(s, -page, 0); !took {
					s.scrollTo(max(0, s.scrollX-w*0.9), s.scrollY)
				}
			default:
				if _, took := rt.snapNotch(s, page, 0); !took {
					s.scrollTo(min(s.contentW-w, s.scrollX+w*0.9), s.scrollY)
				}
			}
		default:
			continue
		}
		rt.requestFrame()
		return true
	}
	return false
}

// windowFocus reports whether the window has the keyboard focus, noting
// that the frame depends on it.
func (rt *engine) windowFocus() bool {
	rt.focusRead = true
	return rt.windowFocused
}

// windowActive reports whether the window is drawn as active, the window
// the user's input goes to (Context.WindowActive), noting that the frame
// depends on it.
func (rt *engine) windowActive() bool {
	rt.activeRead = true
	return rt.host.active()
}

// activeChanged draws a frame after the window became active or ceased to
// be, when the last frame read it, as focusChanged does.
func (rt *engine) activeChanged() {
	if rt.activeRead {
		rt.requestFrame()
	}
}

// focusChanged draws a frame after the window gained or lost the keyboard
// focus, when the last frame read it (a focus ring, a caret, a selection):
// another frame would draw the same, as a window hides or shows again, and
// cost the GPU's driver tens of MB for a second or two.
func (rt *engine) focusChanged() {
	if rt.focusRead {
		rt.requestFrame()
	}
}

// modsChanged takes the modifier keys held now, drawing a frame when they
// changed, for views that show what a held key would do.
func (rt *engine) modsChanged(mods Modifiers) {
	if rt.mods != mods {
		rt.mods = mods
		rt.requestFrame()
	}
}

// Modifiers returns the modifier keys held now, as the last key, pointer
// or modifier event said: a view showing each row's shortcut while Cmd is
// held reads it, and draws again as it changes.
func (c *context) Modifiers() Modifiers { return c.rt.mods }

// heldKey is a key held in the window: the key as shortcuts take it, and
// its position, KeyUnknown where the platform tells none.
type heldKey struct{ key, physical Key }

// heldIndex returns the index in keysHeld of the key at physical, or of
// key where either position is unknown, or -1.
func (rt *engine) heldIndex(key, physical Key) int {
	for i, h := range rt.keysHeld {
		if h.physical != KeyUnknown && physical != KeyUnknown {
			if h.physical == physical {
				return i
			}
		} else if h.key == key {
			return i
		}
	}
	return -1
}

// holdKey notes a key pressed, whichever element has the focus or takes
// it, as shortcuts take it: by position for a chord of a key the layout
// types no US key for (keyDownPhysical).
func (rt *engine) holdKey(mods Modifiers, key, physical Key) {
	if platform.KeyByPosition(platform.Key(key), platform.Key(physical), platform.Modifiers(mods)) {
		key = physical
	}
	if key == KeyUnknown || rt.heldIndex(key, physical) >= 0 {
		return // a key no shortcut names, or held already: a repeat
	}
	rt.keysHeld = append(rt.keysHeld, heldKey{key, physical})
	rt.keysChanged()
}

// releaseKey notes a key let go of.
func (rt *engine) releaseKey(key, physical Key) {
	if i := rt.heldIndex(key, physical); i >= 0 {
		rt.keysHeld = slices.Delete(rt.keysHeld, i, i+1)
		rt.keysChanged()
	}
}

// releaseKeys lets go of every key held, as the window loses the
// keyboard: their releases go to the window that has it.
func (rt *engine) releaseKeys() {
	if len(rt.keysHeld) > 0 {
		rt.keysHeld = rt.keysHeld[:0]
		rt.keysChanged()
	}
}

// keysChanged draws a frame after the keys held changed, when the last
// frame read them.
func (rt *engine) keysChanged() {
	if rt.keysRead {
		rt.requestFrame()
	}
}

// KeyDown reports whether key is held down in the window, whichever
// element has the focus and whether or not one took the key, without
// taking it from them: a cheat sheet of shortcuts lights the keys held
// while its search field has the focus. A frame that reads it is drawn
// again as the keys held change. Keys count as shortcuts take them (a
// chord of a key the layout types no US key for, as S on a Cyrillic
// layout, by its position); the modifier keys are Modifiers'. A key whose
// press ran a menu item's shortcut went to the menu, not the window. The
// window lets go of every key as it loses the keyboard, whose releases go
// elsewhere.
func (c *context) KeyDown(key Key) bool {
	c.rt.keysRead = true
	return key != KeyUnknown && c.rt.heldIndex(key, KeyUnknown) >= 0
}

// KeysDown returns the keys held down in the window, as KeyDown tells
// them, in the order they were pressed; nil while none is. A cheat sheet
// fades the rows of shortcuts that use none of them:
//
//	held := c.KeysDown()
//	for _, s := range app.shortcuts {
//		row := shortcutRow(c, s)
//		if len(held) > 0 && !s.usesAny(held) {
//			row.Opacity(0.4)
//		}
//	}
func (c *context) KeysDown() []Key {
	c.rt.keysRead = true
	var keys []Key
	for _, h := range c.rt.keysHeld {
		if !slices.Contains(keys, h.key) {
			keys = append(keys, h.key)
		}
	}
	return keys
}
