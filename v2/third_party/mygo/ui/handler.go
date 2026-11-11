package ui

import (
	"time"

	"github.com/egoist/mygo/internal/platform"
)

// InputKind is the kind of an InputEvent.
type InputKind uint8

// Kinds of input.
const (
	// InputKeyDown and InputKeyUp report a key with the modifiers held;
	// Repeat marks the auto-repeat of a key held down. A key that types
	// text comes before the text, an InputText event, on macOS and
	// Windows; on Linux, only the text comes while the element takes text
	// (TextCaret) with input methods on (InputMethod). Every key the system
	// tells the window of comes: a modifier on its own, a lone tap of Super
	// among them, the lock keys, and a key whose press the layout types no
	// Key for come with Key KeyUnknown, named by Code alone, and Mods holds
	// the modifier a press brings and not the one a release takes. Taking
	// one keeps it from the system, as Alt from the menu bar.
	InputKeyDown InputKind = iota + 1
	InputKeyUp
	// InputText is text typed, or committed by an input method.
	InputText
	// InputCompose is the composition of an input method, with its caret
	// at rune Caret; an empty Text ends it. One the element leaves, the
	// input method gives up on macOS, as a terminal does the dead key ´ of
	// Option-E that it sent as Alt, so that the next key isn't composed
	// with it.
	InputCompose
	// InputCommand is an edit command of the menus, such as Copy of the
	// Edit menu: "copy", "cut", "paste", "pasteAndMatchStyle" (which
	// comes again as "paste" when the element doesn't take it),
	// "selectAll", "undo", "redo" or "delete", or one of the Format menu,
	// as Bold: "bold", "italic", "underline", "strikethrough",
	// "alignLeft", "alignCenter" or "alignRight".
	InputCommand
	// InputPointerDown and InputPointerUp report Button pressed or
	// released at X, Y, with Clicks the count of quick successive presses
	// (2 for a double click). Once the element takes an InputPointerDown,
	// the pointer's moves and its release come to it wherever they are,
	// and the release comes when the window loses the keyboard too, or
	// once a frame builds the element no more, as when the press moved it
	// into another container, or when the system cancels the press: those
	// releases are Canceled.
	InputPointerDown
	InputPointerUp
	// InputPointerMove reports the pointer at X, Y, with Button held, or
	// -1 when none is.
	InputPointerMove
	// InputScroll scrolls by DX, DY DIPs at X, Y; Precise marks
	// touchpads, which scroll by pixels rather than by lines. One the
	// element leaves scrolls the scroll containers around it. Momentum
	// marks scrolling that continues after the fingers leave a touchpad.
	InputScroll
	// InputSelect selects runes Start to End of the text an element
	// describes to assistive technology (Element.Access), as a screen
	// reader moves the selection of a text field of the app's own.
	InputSelect
	// InputZoom zooms by Scale at X, Y, as pinching a touchpad does: 2
	// shows the content twice as large as it was, 0.5 half as large.
	// macOS, Linux on Wayland and browsers report pinches; Windows reports
	// them as scrolling with Ctrl held, so there a touchpad's scrolling
	// with Ctrl held comes as InputZoom too, and a mouse wheel's notches
	// as InputScroll with Ctrl in Mods.
	InputZoom
)

// InputEvent is input an element takes as it comes (HandleInput).
type InputEvent struct {
	Kind InputKind
	Key  Key
	// PhysicalKey is Key by position on an ANSI US keyboard, whatever the
	// layout types: the S key is KeyS on a Cyrillic layout too, where Key
	// is KeyUnknown. KeyUnknown where the platform does not report it.
	PhysicalKey Key
	// Code is the key by its position on any keyboard, with the sides of
	// the modifiers told apart, as CodeShiftLeft; CodeUnknown where the
	// platform does not report it, as for a mouse's side buttons, whose
	// keys are KeyBack and KeyForward. It alone names a key whose Key is
	// KeyUnknown, as a modifier on its own or Caps Lock.
	Code   KeyCode
	Mods   Modifiers
	Repeat bool
	// Text of InputText, InputCompose and InputCommand, and Caret the
	// rune of an InputCompose's caret.
	Text  string
	Caret int
	// X and Y locate the pointer relative to the element's box.
	X, Y float32
	// Button is 0 for the primary button, 1 the secondary and 2 the
	// middle.
	Button int
	Clicks int
	// Canceled marks an InputPointerUp that ends the press without the
	// pointer letting go, as Pointer Events' pointercancel: the system
	// took the press (a browser's pointercancel, as a touch turns into
	// the page's scrolling, the window losing the mouse capture or the
	// pointer's grab, a menu taking the release), the window lost the
	// keyboard, or a frame built the element no more. The release still
	// comes, so that what the press holds lets go, but it is no
	// completed gesture: undo what the press began rather than commit it,
	// as a tap, a drop or the end of a scrub. Elements that ask about
	// their input as they are built get no click of it (Clicked,
	// RightClicked), no drop (Drop) and no end of a drag (DragEnded).
	Canceled bool
	// Pointer is the device of InputPointerDown, InputPointerUp and
	// InputPointerMove, and Pressure how hard it presses, from 0 to 1, as
	// Pointer Events' pointerType and pressure: a pen's or a finger's
	// where the system tells it, else, as for a mouse, 0.5 while a button
	// is down and 0 while none is. A pen's pressure changing where it
	// stands comes as an InputPointerMove too.
	Pointer  PointerType
	Pressure float32
	// DX and DY are what InputScroll scrolls by, in local axes; positive DY moves the view
	// down the content.
	DX, DY   float32
	Precise  bool
	Momentum bool
	// Scale is what InputZoom zooms by.
	Scale float32
	// Start and End are the runes InputSelect selects.
	Start, End int
	// Time is when the event came, by the clock of the window's frames
	// (Context.Now), which a Tester's clock is in tests.
	Time time.Time
}

// HandleInput has fn take the element's input as it comes, on the main
// thread, before the next frame is built: keys, text and edit commands
// while the element has the keyboard focus, the pointer pressed on it,
// moving over it, scrolling or pinching over it. fn reports whether it took the
// event; one it leaves goes on as it would without fn, to shortcuts, Tab,
// context menus and scroll containers. Keys that the window or an element
// around the focus handles as a Shortcut go to the shortcut first. A frame
// follows the events fn takes.
//
// It is for widgets that need every key as it is pressed, such as a
// terminal, or the scrolling and pinching over them, such as a chart that
// zooms and pans; most widgets ask about their input as they are built
// instead (Clicked, Shortcut, Dragged).
func (e *node) HandleInput(fn func(ev InputEvent) bool) *node {
	e.inputFn = fn
	return e
}

// ObserveInput has fn see the input that comes to the element or to an
// element inside it, without taking it, as an ancestor listening to the
// keydown events that bubble up to it from the focus: keys pressed and
// released (with Repeat), text typed and composed, and the edit commands
// of the menus while the element or one of its descendants has the
// keyboard focus, and the pointer pressed and released over it. fn runs on
// the main thread before the element the input goes to gets it (the
// capture phase of UI Events), the outermost of the elements observing
// first, with X and Y relative to its own box; the input then goes on as
// it would without fn, to HandleInput, the text inputs, shortcuts, Tab and
// the rest, which may move the focus. A frame follows the input fn sees.
//
// fn sees what HandleInput would see at the focus, whatever takes it,
// keys a Shortcut takes and Tab among them, but not the keys the system or
// the menus take before the window gets them (an edit command of the menus
// comes instead, InputCommand), the keys a web view takes while it has the
// focus, nor the input of the popovers, menus and dialogs the element
// opens, which are not inside it. A key that moved the focus, as Tab,
// comes up where the focus went, as with UI Events. A disabled element
// observes nothing.
//
// It is for a region that reacts to any input in it, such as a demo that
// stops playing at the first key pressed in it, or a panel that closes a
// hint as soon as one of its fields is typed in.
func (e *node) ObserveInput(fn func(ev InputEvent)) *node {
	e.observeFn = fn
	return e
}

// TextCaret has the element take text from the system's input methods
// while it has the keyboard focus, with their composition at r, the caret,
// relative to the element's box: candidate windows show there. Its
// InputText and Compose events (HandleInput) bring the text.
func (e *node) TextCaret(r Rect) *node {
	e.caret, e.takesText = r, true
	return e
}

// InputMethod sets whether the system's input methods compose text for the
// element while it has the keyboard focus, as they do by default for one
// that takes text (HandleTextInput, TextCaret). Turned off, keys type the
// characters of the keyboard's layout, which come as InputText, and
// nothing is composed: a mode that reads letters as commands, as Vim's
// normal mode, gets j as j with a Chinese input method on, as Neovide and
// winit's set_ime_allowed(false) have it. Browsers cannot turn input
// methods off, and compose still.
func (e *node) InputMethod(on bool) *node {
	e.noIME = !on
	return e
}

// rawKey gives a key no shortcut can name to the elements around the focus
// that observe their input and to the element with the focus, and reports
// whether that one took it. It goes nowhere else in the window.
func (rt *engine) rawKey(ev platform.SurfaceEvent) bool {
	kind := InputKeyDown
	if ev.Kind == platform.RawKeyReleased {
		kind = InputKeyUp
	}
	input := InputEvent{Kind: kind, Key: Key(ev.Key), PhysicalKey: Key(ev.PhysicalKey), Code: KeyCode(ev.Code), Mods: Modifiers(ev.Mods), Repeat: ev.Repeat}
	rt.observeFocus(input)
	return rt.deliver(rt.focusHandler(), input)
}

// handler returns the state of the innermost element of chain that
// handles its input.
func (rt *engine) handler(chain []uint64) *state {
	for _, id := range chain {
		if s := rt.states[id]; s != nil && s.input != nil && s.flags&flagDisabled == 0 {
			return s
		}
	}
	return nil
}

// deliver gives ev to the element of s, with the pointer's position
// relative to its box, and reports whether it took it.
func (rt *engine) deliver(s *state, ev InputEvent) bool {
	if s == nil || s.input == nil {
		return false
	}
	ev.X, ev.Y = s.local(rt.pointerX, rt.pointerY)
	ev.Time = rt.now()
	switch ev.Kind {
	case InputPointerDown, InputPointerUp, InputPointerMove:
		ev.Pointer, ev.Pressure = rt.pointerType, rt.pressure
	}
	if ev.Kind == InputScroll {
		ev.DX, ev.DY = s.vector(rt.pointerX, rt.pointerY, ev.DX, ev.DY)
	}
	if !s.input(ev) {
		return false
	}
	rt.requestFrame()
	return true
}

// observeFocus shows ev to the elements around the focus that observe
// their input (ObserveInput).
func (rt *engine) observeFocus(ev InputEvent) {
	if rt.observers == 0 || rt.focused == 0 || !rt.windowFocus() {
		return
	}
	chain := rt.appendFocusChain(rt.observeBuf[:0])
	rt.observe(chain, ev)
}

// observePointer shows ev to the elements under the pointer that observe
// their input.
func (rt *engine) observePointer(ev InputEvent) {
	if rt.observers == 0 {
		return
	}
	chain := rt.appendHitChain(rt.observeBuf[:0], rt.pointerX, rt.pointerY)
	rt.observe(chain, ev)
}

// observe shows ev to the elements of chain, an element and its
// ancestors innermost first, that observe their input: the outermost
// first, as UI Events capture, with the pointer's position relative to
// each one's box. chain is rt.observeBuf's, which it holds meanwhile, as
// fn may run a nested main loop that brings another event.
func (rt *engine) observe(chain []uint64, ev InputEvent) {
	rt.observeBuf = nil
	defer func() { rt.observeBuf = chain[:0] }()
	ev.Time = rt.now()
	seen := false
	for i := len(chain) - 1; i >= 0; i-- {
		s := rt.states[chain[i]]
		if s == nil || s.observe == nil || s.flags&flagDisabled != 0 {
			continue
		}
		ev.X, ev.Y = s.local(rt.pointerX, rt.pointerY)
		s.observe(ev)
		seen = true
	}
	if seen {
		rt.requestFrame()
	}
}

// focusHandler returns the state of the focused element when it handles
// its input.
func (rt *engine) focusHandler() *state {
	if s := rt.states[rt.focused]; s != nil && s.input != nil && rt.windowFocus() {
		return s
	}
	return nil
}
