package ui

import "slices"

// Input bindings are retained by ID. Input reaches a control as the public
// constructor that built it returns, as Dear ImGui's and egui's widgets
// take theirs while they are called: a value bound to a control, a local
// copy too, holds the user's edit when the next statement reads it. A
// container's input waits for the call that built it, children included,
// and what the end of construction finds still queued, as controls the
// engine builds itself, applies then. Input follows the options of the
// frame the user acted on (a field read-only then ignores typing), which
// a control keeps from its last build: options set after its constructor
// govern the input of the frames after.
// The function and queue are reused; steady frames do not allocate for them.
func valueBinding[T any](n *node) *T {
	p, ok := n.st.valueBinding.(*T)
	if !ok {
		p = new(T)
		n.st.valueBinding = p
	}
	return p
}

// valueInput is a control whose input is queued, with the number of
// public constructors under way as it was built.
type valueInput struct {
	n        *node
	building int32
}

func (n *node) onValueInput(fn func(*node)) {
	if n.valueInput == nil {
		n.c.rt.inputs = append(n.c.rt.inputs, valueInput{n: n, building: n.c.rt.building})
	}
	n.valueInput = fn
}

// beginBuild starts a public constructor.
func (rt *engine) beginBuild() { rt.building++ }

// endBuild ends a public constructor, applying the input of the controls
// it built.
func (rt *engine) endBuild() {
	if rt.building > 0 {
		rt.building--
	}
	// Most controls, as text and rows, queue no input.
	if rt.inputAt < len(rt.inputs) || rt.afterInputAt < len(rt.afterInputs) ||
		len(rt.waitingInputs) > 0 || len(rt.waitingActions) > 0 {
		rt.applyInputs()
	}
}

func (rt *engine) beginInputs() {
	rt.inputAt, rt.afterInputAt = 0, 0
	rt.waitingInputs, rt.waitingActions = rt.waitingInputs[:0], rt.waitingActions[:0]
	// A rebuild must not report the same edit on a fresh local binding.
	// Keep typed input until this pass's composite controls have handled it.
	for _, s := range rt.notices {
		if s.typing {
			rt.typedInputs = append(rt.typedInputs, s)
		}
		s.changed, s.submitted, s.noticeQueued = false, false, false
		s.changeDelivered, s.submitDelivered = false, false
	}
	clear(rt.notices)
	rt.notices = rt.notices[:0]
}

// applyInputs finalizes the controls built so far whose constructors
// returned: those queued while more public constructors were under way
// than now. It runs as each constructor returns and as the view queries
// responses. Composite input actions may query their children without
// reentering it.
func (rt *engine) applyInputs() { rt.finalizeInputs(false) }

// applyAllInputs finalizes every control built, after construction.
func (rt *engine) applyAllInputs() { rt.finalizeInputs(true) }

func (rt *engine) finalizeInputs(all bool) {
	if rt.applyingInputs {
		return
	}
	rt.applyingInputs = true
	defer func() { rt.applyingInputs = false }()
	// Input queued outside public constructors, as by the engine's own
	// controls, is finished as it is queued.
	built := func(b int32) bool { return all || b == 0 || b > rt.building }
	// Each entry is looked at once as the cursor passes it; those of
	// containers still building wait aside, as few as the containers
	// open, until their constructors return.
	apply := func(i int) {
		n := rt.inputs[i].n
		if n.valueInput == nil {
			return
		}
		if n.st.pendingSubmit {
			n.st.pendingSubmit = false
			if !n.disabled() {
				n.st.markSubmitted()
			}
		}
		if n.st.pendingChange {
			n.st.pendingChange = false
			n.st.markChanged()
		}
		n.valueInput(n)
	}
	// Those waiting were queued before the cursor's, so they go first:
	// a list's rows take their clicks before the list its keys.
	rt.waitingInputs = slices.DeleteFunc(rt.waitingInputs, func(i int) bool {
		if !built(rt.inputs[i].building) {
			return false
		}
		apply(i)
		return true
	})
	for ; rt.inputAt < len(rt.inputs); rt.inputAt++ {
		if built(rt.inputs[rt.inputAt].building) {
			apply(rt.inputAt)
		} else {
			rt.waitingInputs = append(rt.waitingInputs, rt.inputAt)
		}
	}
	act := func(i int) {
		if a := rt.afterInputs[i]; a.node != nil && !a.node.disabled() {
			a.fn()
		}
	}
	rt.waitingActions = slices.DeleteFunc(rt.waitingActions, func(i int) bool {
		if !built(rt.afterInputs[i].building) {
			return false
		}
		act(i)
		return true
	})
	for ; rt.afterInputAt < len(rt.afterInputs); rt.afterInputAt++ {
		if built(rt.afterInputs[rt.afterInputAt].building) {
			act(rt.afterInputAt)
		} else {
			rt.waitingActions = append(rt.waitingActions, rt.afterInputAt)
		}
	}
	if len(rt.notices) > 0 {
		rt.consumed = true
	}
}

func (rt *engine) finishInputs() {
	for _, s := range rt.typedInputs {
		s.typing = false
	}
	clear(rt.typedInputs)
	rt.typedInputs = rt.typedInputs[:0]
}

func toggleInput(n *node) {
	if n.Clicked() {
		p := *valueBinding[*bool](n)
		*p = !*p
		n.st.markChanged()
	}
}

type radioValue[T comparable] struct {
	selected *T
	value    T
}

func radioInput[T comparable](n *node) {
	b := valueBinding[radioValue[T]](n)
	if n.Clicked() && *b.selected != b.value {
		*b.selected = b.value
		n.st.markChanged()
	}
}

type sliderValue struct {
	value        *float64
	lo, hi, step float64
}

func sliderInput(n *node) {
	if n.disabled() {
		return
	}
	b := valueBinding[sliderValue](n)
	settings := coreLocal(n, sliderKey{}, func() sliderSettings { return sliderSettings{} }).shown
	step := b.step
	if settings.step > 0 {
		step = settings.step
	}
	set := func(v float64) {
		v = snap(max(b.lo, min(b.hi, v)), b.lo, b.hi, step)
		if *b.value != v {
			*b.value = v
			n.st.markChanged()
			n.c.rt.consumed = true
		}
	}
	st, rt := n.st, n.c.rt
	if st.pressedPrimary() {
		x, y := st.local(st.pointer(rt))
		x += st.x
		y += st.y
		if settings.vertical && st.ch > 0 {
			set(b.lo + float64(max(0, min(1, 1-(y-st.cy)/st.ch)))*(b.hi-b.lo))
		} else if !settings.vertical && st.cw > 0 {
			f := (x - st.cx) / st.cw
			if n.rtl() {
				// Right to left, lo is at the right, as AppKit's sliders
				// and browsers' ranges go.
				f = 1 - f
			}
			set(b.lo + float64(max(0, min(1, f)))*(b.hi-b.lo))
		}
	}
	if step <= 0 {
		step = (b.hi - b.lo) / 100
	}
	// The arrows move the knob the way they point.
	back, forth := KeyLeft, KeyRight
	if n.rtl() && !settings.vertical {
		back, forth = forth, back
	}
	switch {
	case n.Shortcut(0, back), n.Shortcut(0, KeyDown):
		set(*b.value - step)
	case n.Shortcut(0, forth), n.Shortcut(0, KeyUp):
		set(*b.value + step)
	case n.Shortcut(0, KeyPageDown):
		set(*b.value - max(step, (b.hi-b.lo)/10))
	case n.Shortcut(0, KeyPageUp):
		set(*b.value + max(step, (b.hi-b.lo)/10))
	case n.Shortcut(0, KeyHome):
		set(b.lo)
	case n.Shortcut(0, KeyEnd):
		set(b.hi)
	}
}

func stringInput(n *node) { n.st.editor.withShown(func() { applyString(n) }) }

func applyString(n *node) {
	ed, st, rt := n.st.editor, n.st, n.c.rt
	value := ed.value
	if ed.nativeDirty {
		before := ed.nativeValue
		ed.nativeDirty, ed.nativeValue = false, ""
		if *value == before {
			if n.disabled() || ed.readOnly {
				ed.setText(*value)
				ed.compose = ""
			} else if *value != ed.buf.s {
				*value = ed.buf.s
				st.markChanged()
			}
		} else {
			ed.setText(*value)
			ed.compose = ""
		}
	}
	if n.disabled() {
		clear(ed.queue)
		ed.queue = ed.queue[:0]
		ed.compose = ""
		return
	}
	if rt.focused == n.id || len(ed.queue) > 0 {
		version := ed.buf.version
		ed.process(n.c, n)
		if ed.buf.version != version && ed.buf.s != *value {
			*value = ed.buf.s
			st.markChanged()
		}
	}
	if rt.focused != n.id {
		ed.compose = ""
	}
}

func bufferInput(n *node) { n.st.editor.withShown(func() { applyBuffer(n) }) }

func applyBuffer(n *node) {
	ed, st, rt := n.st.editor, n.st, n.c.rt
	if ed.bufferDirty && (n.disabled() || ed.readOnly) {
		snapshot, _ := ed.document.compareRestore(ed.published, ed.nativeBufferBefore.root)
		ed.loadBuffer(snapshot)
		ed.bufferDirty = false
	}
	if n.disabled() {
		clear(ed.queue)
		ed.queue = ed.queue[:0]
		ed.compose = ""
		return
	}
	if ed.bufferDirty {
		st.markChanged()
		ed.bufferDirty = false
	}
	if rt.focused == n.id || len(ed.queue) > 0 {
		before := ed.buf.version
		ed.process(n.c, n)
		if ed.buf.version != before {
			ed.client.publish()
			st.markChanged()
			ed.bufferDirty = false
		}
	}
	if rt.focused != n.id {
		ed.compose = ""
	}
}

type listRowInput struct {
	f *listFrame
	i int
}

func listRowValueInput(n *node) {
	b := valueBinding[listRowInput](n)
	f := b.f
	if n.disabled() || f.owner.disabled() {
		return
	}
	rt, st := n.c.rt, n.st
	if st.pressPending {
		// A pointer press began on the row. Where a drag chooses, one on a
		// row or a cell not chosen chooses it as the pointer goes down, as
		// spreadsheets do; one chosen waits for the release, which keeps a
		// drag of the choice.
		st.rowPressed, st.pressChose = true, false
		if st.pressed && rt.pressButton == 0 && f.dragSelects() && !f.chosenAt(b.i, st.drag.ox, st.drag.oy) {
			f.clickAt(b.i, st.drag.ox, st.drag.oy, st.pressMods)
			st.pressChose = true
		}
	}
	if n.Clicked() {
		switch {
		case st.pressChose:
		case st.rowPressed:
			f.clickAt(b.i, st.drag.ox, st.drag.oy, n.ClickModifiers()) // where it pressed
		default:
			// The keyboard or assistive technology: the row, and no column.
			f.click(b.i, n.ClickModifiers())
		}
		if !f.owner.FocusWithin() {
			// Leave the focus on a control inside a row, such as an editor
			// a double click opened.
			f.owner.Focus()
		}
	}
	if n.DoubleClicked() {
		f.owner.st.markSubmitted()
	}
	if !st.pressed {
		st.rowPressed, st.pressChose = false, false
	}
}
func listInput(n *node) {
	if n.disabled() || n.rowsOf == nil {
		return
	}
	f := n.rowsOf
	if p := f.c.rt.pressEnded; p != nil {
		// The pass that reads the last moves of a press sees it go on, up
		// to where the pointer let go, before the keys that came after its
		// release.
		f.dragSelect(p)
	}
	if f.s.cursor() != nil {
		f.navigate()
	}
	f.dragSelect(f.c.rt.pressed)
	f.reorder()
}

func (s *state) notice() {
	if s.rt != nil && !s.noticeQueued {
		s.rt.notices = append(s.rt.notices, s)
		s.noticeQueued = true
	}
}
func (s *state) markChanged() {
	if s.rt != nil && !s.rt.inFrame {
		// A change made between frames: the next frame's build reports
		// it.
		s.pendingChange = true
		return
	}
	s.changed, s.changeDelivered = true, false
	s.notice()
}
func (s *state) markSubmitted() {
	if s.rt != nil && !s.rt.inFrame {
		s.pendingSubmit = true
		return
	}
	s.submitted, s.submitDelivered = true, false
	s.notice()
}
func (s *state) markTyping() { s.typing = true; s.notice() }

// Composite controls schedule small input actions after their children.
// Queries apply them once, or they run after construction if not queried.
type inputAction struct {
	node     *node
	fn       func()
	building int32
}

func (n *node) afterInput(fn func()) {
	n.c.rt.afterInputs = append(n.c.rt.afterInputs, inputAction{node: n, fn: fn, building: n.c.rt.building})
}

// onBuilt runs fn once the view is built, after the input of every
// control, while typing (state.typing) still shows: for what needs all of
// a widget's parts, which the app builds, as the items of a tree.
func (c *context) onBuilt(fn func()) { c.rt.afterBuild = append(c.rt.afterBuild, fn) }

func (rt *engine) runAfterBuild() {
	for _, fn := range rt.afterBuild {
		fn()
	}
}

// SetChanged reports the element's value as changed in this build pass,
// as a widget of your own whose input changed what it edits: Changed
// returns true for it, as for the widgets' own.
func (e *node) SetChanged() *node {
	if e.hasState() {
		e.st.markChanged()
	}
	return e
}
