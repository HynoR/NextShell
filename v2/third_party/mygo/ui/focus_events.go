package ui

// FocusLeft reports whether the keyboard focus left the element since the
// last frame: it was on the element or inside it, and is now outside it,
// or nowhere, as the window lost the keyboard. That is the web's focusout
// whose relatedTarget the element does not contain. The overlays attached
// to the element and to those inside it (AttachTo), as a combobox's list
// or a button's popover, count as inside it, as does the element it is
// attached to, as PressedOutside has them: a combobox closes as the focus
// leaves its field, but not as it moves into its list.
//
//	field := ui.TextInput(c, &app.query)
//	if field.FocusLeft() {
//		app.open = false
//	}
//
// Where the focus went is what the elements tell as they are built
// (Focused, FocusWithin), and a Handle's FocusWithin before its control
// is.
func (e *node) FocusLeft() bool {
	if !e.hasState() {
		return false
	}
	rt := e.c.rt
	to := rt.focusNow()
	if !rt.leftRead {
		rt.leftRead, rt.leftSaw = true, to
	}
	from := rt.focusFrom
	if from == 0 || from == to || !rt.pressedWithin(from, e) {
		return false
	}
	if s := rt.states[to]; s != nil && s.born == rt.frame {
		// The focus moved to an element this frame made, whose place the
		// frame has yet to commit: the next one tells.
		rt.leftSaw = from
		return false
	}
	if to != 0 && rt.pressedWithin(to, e) {
		return false
	}
	rt.consumed = true
	return true
}

// focusNow returns the element with the keyboard focus, 0 while the window
// does not have the keyboard, noting that the frame depends on it.
func (rt *engine) focusNow() uint64 {
	if !rt.windowFocus() {
		return 0
	}
	return rt.focused
}

// passFocus notes, as a pass ends, where the focus is, for FocusLeft in
// the next pass to tell where it moved from. Where it moved after the
// pass read it, as when an element built later took it, the next pass
// tells, from where the reads saw it.
func (rt *engine) passFocus() {
	now := rt.focused
	if !rt.windowFocused {
		now = 0
	}
	if rt.leftRead && rt.leftSaw != now {
		rt.focusFrom = rt.leftSaw
		rt.consumed, rt.animating = true, true
	} else {
		rt.focusFrom = now
	}
	rt.leftRead = false
}

// FocusTakesText reports whether the element with the keyboard focus takes
// text, typed or composed: a text input or text area, or an editor of
// rich text or code, that is not read only, or an element that takes text
// itself (HandleTextInput, TextCaret). It is the web's guard of
// single-key shortcuts (isContentEditable, an input or a textarea, the
// role textbox): a command palette's letters step aside while a field
// takes them as text.
//
//	if !c.FocusTakesText() && c.Shortcut(0, ui.KeyK) {
//		app.palette = true
//	}
//
// It is false while the window does not have the keyboard, as the
// window tells the system's input methods then.
func (c *context) FocusTakesText() bool { return c.rt.focusTakesText() }

// focusTakesText is FocusTakesText.
func (rt *engine) focusTakesText() bool {
	s := rt.states[rt.focused]
	if s == nil || !rt.windowFocus() {
		return false
	}
	// As updateTextInput has the window take text.
	if s.textClient != nil {
		return s.flags&flagDisabled == 0 && (s.editor == nil || !s.editor.readOnly)
	}
	return s.editor == nil && s.input != nil && s.takesText
}
