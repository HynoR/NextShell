package ui

import (
	"slices"

	"github.com/egoist/mygo/internal/text"
)

// formBuild is a Form being built: the boxes of its fields' labels, which
// its layout makes as wide as the widest.
type formBuild struct {
	labels  []*node
	aligned bool
}

// fieldParts are the parts of a Field: its label, the column holding its
// control and the texts below it, and the control, which the label names
// and the texts describe.
type fieldParts struct {
	label, content, control *node
	// errors are those of controls inside that check what is typed, as
	// DecimalInput, which show below the control (fieldError).
	errors []string
}

// Form creates a column of the fields that fn builds (Field, Fieldset),
// whose labels sit in a column of their own, beside the controls, as in
// macOS's forms: aligned to the right, as wide as the widest, and level
// with the first line of their control.
//
//	ui.Form(c, func() {
//		ui.Field(c, "Name", func() { ui.TextInput(c, &app.name) })
//		ui.Field(c, "Size", func() { ui.Select(c, &app.size, sizes) })
//	})
func coreForm(c *context, fn func()) *node {
	f := coreColumn(c).Gap(c.theme.Space(3))
	f.widget = "Form"
	fb := &formBuild{}
	f.form = fb
	saved := c.form
	c.form = fb
	f.Children(fn)
	c.form = saved
	return f
}

// alignLabels makes the labels of a form as wide as the widest, once a
// frame, before the form is measured or laid out.
func (fb *formBuild) alignLabels() {
	if fb.aligned {
		return
	}
	fb.aligned = true
	var w float32
	for _, l := range fb.labels {
		if !l.inHidden() {
			// A Hidden field's label sets no width.
			w = max(w, intrinsic(l, true))
		}
	}
	for _, l := range fb.labels {
		l.width = px(w)
		l.parent.forgetIntrinsic()
	}
}

// Field creates a field: label, above the control that fn builds, or
// beside it in a Form. The label names the control for assistive
// technology, unless the control names itself, as a check box with its own
// text does, when it names the field around it; a click on the label
// focuses the control, or clicks a check box, a switch or a radio button.
// Description and Error add texts below the control, which describe it:
//
//	ui.Field(c, "Email", func() {
//		ui.TextInput(c, &app.email)
//	}).Description("For receipts.").Error(app.emailError)
//
// The control is the first element fn builds that takes the focus, or a
// group of them, such as a RadioGroup.
func coreField(c *context, label string, fn func()) *node {
	t := c.theme
	fp := &fieldParts{}
	var f *node
	if c.form != nil {
		f = coreRow(c).Gap(t.Space(3)).AlignItems(Start)
		f.baselines = true
	} else {
		f = coreColumn(c).Gap(t.Space(1.5))
	}
	f.widget, f.field = "Field", fp
	f.Children(func() {
		if c.form != nil {
			box := coreRow(c).Justify(End).Shrink(0)
			c.form.labels = append(c.form.labels, box)
			box.Children(func() { fp.label = coreText(c, label) })
		} else {
			fp.label = coreText(c, label).FontWeight(500)
		}
		fp.content = coreColumn(c).Gap(t.Space(1.5))
		if c.form != nil {
			fp.content.Grow(1).MinWidth(0) // the rest of the row
		}
		fp.content.Children(fn)
	})
	ctrl := fieldControl(fp.content)
	fp.control = ctrl
	for _, msg := range fp.errors {
		f.fieldText(msg, t.Danger)
	}
	l := fp.label
	if ctrl == nil {
		return f
	}
	if namesItself(ctrl) {
		f.Role(RoleGroup).Label(label)
	} else if ctrl.nameFrom == nil || ctrl.nameFrom.label == "" {
		ctrl.nameFrom = l
	}
	l.labels(ctrl)
	l.styleFn = func(l *node) {
		if ctrl.IsDisabled() && !l.IsDisabled() {
			l.Opacity(0.5)
		}
	}
	return f
}

// LabelledBy has label, a text the view draws as the element's caption,
// name the element for assistive technology, in place of its Label or the
// text inside it, as ARIA's aria-labelledby does, and gives a click on
// label to the element, as HTML's <label for> does: the click focuses it,
// as a text input, or in a group of controls, as a RadioGroup, the one
// chosen, else the first, and clicks a check box, a switch or a radio
// button, as a Field's label does. Call it once both are built, before or
// after the element:
//
//	label := ui.Text(c, "Card number").TextColor(t.TextMuted)
//	ui.TextInput(c, &app.card).LabelledBy(label)
//
// A label that is no text names the element by its Label.
func (e *node) LabelledBy(label *node) *node {
	if e == nil || label == nil || label == e {
		return e
	}
	ctrl := e
	if e.focusTo != nil {
		ctrl = e.focusTo // the input of the box around it, as a SearchField's
	}
	ctrl.nameFrom, ctrl.labelledBy = label, true
	label.labels(ctrl)
	return e
}

// labels gives a click on e, a label of ctrl (Field, LabelledBy), to
// ctrl, as HTML's label activation behavior does: it focuses ctrl, or
// what is chosen in it, and clicks it when it toggles.
func (e *node) labels(ctrl *node) {
	e.flags |= flagClickable
	if e.Clicked() && !ctrl.disabled() {
		if to := focusIn(ctrl); to != nil {
			to.Focus()
		}
		if ctrl.flags&flagToggle != 0 {
			e.c.rt.clickLater = append(e.c.rt.clickLater, ctrl.id)
		}
		e.c.rt.consumed = true
	}
}

// fieldControl returns the control of a field: the first element inside e
// that takes the focus, or a group of them.
func fieldControl(e *node) *node {
	for ch := e.first; ch != nil; ch = ch.next {
		switch ch.role {
		case RoleRadioGroup, RoleToolbar, RoleList, RoleListBox, RoleTable, RoleTree, RoleTabList, RoleGrid, RoleTreeGrid:
			return ch
		}
		if ch.flags&(flagFocusable|flagEditable) != 0 {
			return ch
		}
		if ctrl := fieldControl(ch); ctrl != nil {
			return ctrl
		}
	}
	return nil
}

// focusIn returns what a click on the label of a field focuses: its
// control, or in a group of them, the one chosen, else the first.
func focusIn(e *node) *node {
	if e.flags&(flagFocusable|flagEditable) != 0 {
		return e
	}
	var first, chosen *node
	var walk func(e *node)
	walk = func(e *node) {
		for ch := e.first; ch != nil && chosen == nil; ch = ch.next {
			if ch.flags&flagFocusable != 0 && !ch.disabled() {
				if first == nil {
					first = ch
				}
				if ch.checked == 2 {
					chosen = ch
				}
			}
			walk(ch)
		}
	}
	walk(e)
	if chosen != nil {
		return chosen
	}
	return first
}

// namesItself reports whether a control has a name of its own, as a
// check box with its own text has, rather than a value.
func namesItself(e *node) bool {
	if e.label != "" || e.labelledBy {
		return true
	}
	switch e.role {
	case RoleButton, RoleLink, RoleCheckBox, RoleRadio, RoleSwitch, RoleToggleButton, RoleMenuButton, RoleTab, RoleDisclosure:
		return e.innerText() != ""
	case RoleAuto:
		return e.flags&flagClickable != 0 && e.flags&flagEditable == 0 && e.innerText() != ""
	}
	return false
}

// nameOf returns the name an element gives another (nameFrom): its Label,
// or the text it is.
func (e *node) nameOf() string {
	if e.label == "" && e.kind == kindText {
		return e.accessText()
	}
	return e.label
}

// Fieldset creates a group of the fields that fn builds, under legend,
// which names the group for assistive technology. Disabled disables them
// all. In a Form, their labels line up with those of the other fields.
//
//	ui.Fieldset(c, "Shipping", func() {
//		ui.Field(c, "Address", func() { ui.TextInput(c, &app.address) })
//	})
func coreFieldset(c *context, legend string, fn func()) *node {
	t := c.theme
	above := c.parent != nil && c.parent.nchild > 0
	g := coreColumn(c).Gap(t.Space(3)).Shrink(0)
	g.widget, g.role, g.label = "Fieldset", RoleGroup, legend
	if above {
		// Apart from what is above, as a section.
		g.Margin(t.Space(3), 0, 0, 0)
	}
	g.Children(func() {
		coreText(c, legend).FontWeight(600)
		fn()
	})
	return g
}

// Description tells assistive technology more about the element than its
// name, as help text it reads after it. On a Field, the text shows below
// the control, and describes it.
func (e *node) Description(s string) *node {
	if s == "" {
		return e
	}
	target := e.fieldText(s, e.theme().TextMuted)
	target.description = joinDescription(target.description, s)
	return e
}

// Error marks the element's value as not valid, for msg, which assistive
// technology reads with it; an empty msg leaves it valid. On a Field, the
// message shows below the control in the theme's Danger color, and marks
// the control, whose border text inputs draw in that color.
func (e *node) Error(msg string) *node {
	if msg == "" {
		return e
	}
	target := e.fieldText(msg, e.theme().Danger)
	target.invalid = true
	// The error first, as what to read first.
	target.description = joinDescription(msg, target.description)
	return e
}

// Invalid marks the element's value as not valid, as ARIA's aria-invalid,
// without a message: assistive technology reads the element as invalid,
// and a text input draws its border in the theme's Danger color, but its
// description stays as it is. It suits a required field marked as the
// user leaves it empty, whose message waits for the form to be submitted,
// or a control whose message is an element of its own (ErrorMessage). On
// a Field, it marks the field's control. false leaves the element as it
// is: valid, unless Error, or the control's own check of what is typed
// into it, marks it.
//
//	ui.TextInput(c, &app.card).Label("Card number").
//		Invalid(app.cardTouched && app.card == "")
func (e *node) Invalid(on bool) *node {
	if on {
		e.describedNode().invalid = true
	}
	return e
}

// DescribedBy has desc, an element the view draws, as a hint below a
// field or a help text beside it, describe the element to assistive
// technology, as ARIA's aria-describedby does: screen readers read desc's
// text after the element's name and value, as a Description, which it
// follows. The text is desc's Label, else the texts inside it, as a
// sentence with a link in it, read as one; it counts even when desc is
// hidden from assistive technology (AccessHidden). Call it once both are
// built, before or after the element, once for each element describing
// it, in the order to read them. On a Field, it describes the field's
// control.
//
//	hint := ui.Text(c, "16 digits, no spaces").TextColor(t.TextMuted)
//	ui.TextInput(c, &app.card).Label("Card number").DescribedBy(hint)
func (e *node) DescribedBy(desc *node) *node {
	if target := e.describedNode(); desc != nil && desc != target && !slices.Contains(target.describedBy, desc) {
		target.describedBy = append(target.describedBy, desc)
	}
	return e
}

// ErrorMessage has msg, an element the view draws holding the message of
// the element's error, tell assistive technology why its value is not
// valid, as ARIA's aria-errormessage does: while the element is invalid
// (Invalid, Error), screen readers read msg's text with it, first in its
// description, and those that follow the relation go to msg. While it is
// valid, msg says nothing of it, so the view may keep msg built, empty or
// hidden. Call it once both are built, once for each element holding a
// message. On a Field, it is the field's control's message.
//
//	in := ui.TextInput(c, &app.email).Label("Email").Invalid(app.emailError != "")
//	if app.emailError != "" {
//		in.ErrorMessage(ui.Text(c, app.emailError).TextColor(t.Danger))
//	}
func (e *node) ErrorMessage(msg *node) *node {
	if target := e.describedNode(); msg != nil && msg != target && !slices.Contains(target.errorMessage, msg) {
		target.errorMessage = append(target.errorMessage, msg)
	}
	return e
}

// describedNode returns the element that the description, the validity
// and the relations given to e are of: the control of a Field, or the
// input of a field around one, as a SearchField's, else e.
func (e *node) describedNode() *node {
	if f := e.field; f != nil && f.control != nil {
		return f.control
	}
	if e.focusTo != nil {
		return e.focusTo
	}
	return e
}

// fieldError marks e, a control checking what is typed into it, as not
// valid for msg, as Error does, and shows msg below the control of the
// Field around it, as the field's Error would.
func (e *node) fieldError(msg string) {
	e.invalid = true
	e.description = joinDescription(msg, e.description)
	for p := e.parent; p != nil; p = p.parent {
		if fp := p.field; fp != nil {
			if fp.control != nil {
				p.fieldText(msg, e.theme().Danger) // built once the field was
			} else {
				fp.errors = append(fp.errors, msg)
			}
			return
		}
	}
}

// fieldText shows s below the control of a field, and returns the element
// it describes: the control, or e.
func (e *node) fieldText(s string, color Color) *node {
	f := e.field
	if f == nil {
		return e
	}
	c := e.c
	f.content.Children(func() {
		coreText(c, s).TextColor(color).FontSize(c.theme.FontSize - 1)
	})
	if f.control != nil {
		return f.control
	}
	return e
}

func joinDescription(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + "\n" + b
}

// inputBorder is the border of the box of a text input: the theme's
// Danger while the input is not valid, its Accent while it has the focus,
// darker while the pointer is over the box.
func inputBorder(t *Theme, box, in *node) {
	switch {
	case in.invalid:
		box.borderC = t.Danger
	case in.Focused():
		box.borderC = t.Accent
	case box.Hovered():
		box.borderC = t.Border.Mix(t.Text, 0.25)
	}
}

// alignBaselines lines up the first lines of text of a row's children,
// as a field in a form does its label's and its control's, moving down
// those whose first line is higher. A child without text, as a switch,
// takes the baseline a line of the others' text centered on its first
// control would have.
func alignBaselines(e *node) {
	var ascent, descent float32
	for ch := e.first; ch != nil && ascent == 0; ch = ch.next {
		if l := firstLine(ch); l != nil && ch.flags&flagAbsolute == 0 {
			ascent, descent = l.Ascent, l.Descent
		}
	}
	if ascent == 0 {
		return
	}
	at := func(ch *node) (float32, bool) {
		b, _, ok := anchor(ch, ascent, descent)
		return b, ok
	}
	var top float32
	for ch := e.first; ch != nil; ch = ch.next {
		if ch.flags&flagAbsolute == 0 {
			if b, ok := at(ch); ok {
				top = max(top, ch.y+b)
			}
		}
	}
	for ch := e.first; ch != nil; ch = ch.next {
		if ch.flags&flagAbsolute == 0 {
			if b, ok := at(ch); ok {
				ch.y = top - b
			}
		}
	}
}

// anchor returns how far below e's top the baseline its row lines up on
// is: that of its first line of text, or, where a control comes first, a
// line of the row's text centered on it, so that a control without text,
// as a row of swatches, lines up with its label rather than with a
// description under it. In a row, whose children sit side by side, text
// comes before what is drawn beside it, as a status dot or an icon. text
// reports whether the baseline is that of a text.
func anchor(e *node, ascent, descent float32) (b float32, text, ok bool) {
	if e.flags&flagInvisible != 0 || e.hidden {
		return 0, false, false
	}
	switch {
	case e.kind == kindText || e.kind == kindInput:
		b, ok := firstBaseline(e)
		return b, ok, ok
	case e.flags&(flagFocusable|flagEditable|flagClickable) != 0 || e.first == nil:
		if b, ok := firstBaseline(e); ok {
			return b, true, true
		}
		if e.h > 0 {
			return e.h/2 + (ascent-descent)/2, false, true
		}
		return 0, false, false
	}
	var first float32
	found := false
	for ch := e.first; ch != nil; ch = ch.next {
		if ch.flags&(flagAbsolute|flagInvisible) != 0 {
			continue
		}
		b, text, ok := anchor(ch, ascent, descent)
		if !ok {
			continue
		}
		if text || !e.row {
			return ch.y + b, text, true
		}
		if !found {
			first, found = ch.y+b, true
		}
	}
	return first, false, found
}

// firstLine returns the first line of text in the element, as last laid
// out, or nil.
func firstLine(e *node) *text.Line {
	if e.flags&flagInvisible != 0 || e.hidden {
		return nil
	}
	switch e.kind {
	case kindText:
		if e.tl != nil && len(e.tl.Lines) > 0 {
			return &e.tl.Lines[0]
		}
		return nil
	case kindInput:
		if ed := e.st.editor; ed != nil {
			return ed.firstLine()
		}
		return nil
	}
	for ch := e.first; ch != nil; ch = ch.next {
		if ch.flags&flagAbsolute == 0 {
			if l := firstLine(ch); l != nil {
				return l
			}
		}
	}
	return nil
}

// firstBaseline returns how far below the element's top the baseline of
// its first line of text is, as last laid out, and false for an element
// without text.
func firstBaseline(e *node) (float32, bool) {
	if e.flags&flagInvisible != 0 || e.hidden {
		return 0, false
	}
	switch e.kind {
	case kindText:
		if e.tl != nil && len(e.tl.Lines) > 0 {
			return e.contentY() + e.tl.Lines[0].Baseline, true
		}
		return 0, false
	case kindInput:
		if ed := e.st.editor; ed != nil {
			if l := ed.firstLine(); l != nil {
				return ed.originY + l.Baseline, true
			}
		}
		return 0, false
	}
	for ch := e.first; ch != nil; ch = ch.next {
		if ch.flags&(flagAbsolute|flagInvisible) != 0 {
			continue
		}
		if b, ok := firstBaseline(ch); ok {
			return ch.y + b, true
		}
	}
	return 0, false
}
