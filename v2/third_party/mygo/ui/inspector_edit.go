//go:build !mygo_noinspector

package ui

import (
	"math"
	"slices"
	"strconv"
	"strings"
)

// Editing styles in the inspector, as in Chrome's Styles tab: a view builds
// its elements anew each frame, so the edits are applied to them after they
// are built and before they are laid out (applyEdits), by the element's ID,
// which is why they outlast the frame and go when the inspector closes.
// Nothing changes the view's code, or its state.

// inspEdit is a change to a declaration of an element: a new value, or the
// declaration turned off.
type inspEdit struct {
	name, value string
	// off turns the declaration off, which leaves the element as though the
	// view had not set it.
	off bool
	// changed tells that the value is the user's, not the view's; added
	// that the view has no such declaration.
	changed, added bool
	// valid tells that the last element it was applied to understood it.
	valid bool
	// index is where the declaration was in its rule, to stay there while
	// off, with the swatch it had.
	index  int
	color  Color
	swatch bool
}

// inspEditing is the value being typed in the Styles tab.
type inspEditing struct {
	active bool
	// id and name are the element and the declaration; an empty name adds
	// declarations, from text as "name: value; name: value".
	id   uint64
	name string
	text string
	// start is the text as editing began, and last the text previewed.
	start, last string
	// index is the declaration's place in its rule, with its swatch.
	index  int
	color  Color
	swatch bool
	// before is the element's edit as editing began, restored by Escape.
	before    inspEdit
	hadBefore bool
	// focus asks for the focus for the input, and held tells that it had
	// it, to end editing as it goes.
	focus, held bool
}

// applyEdits applies the edits to the elements built under root.
func (in *inspector) applyEdits(root *node) {
	if len(in.edits) > 0 {
		in.applyEditsTo(root)
	}
}

func (in *inspector) applyEditsTo(e *node) {
	if e.id == in.panel && e.parent != nil && e.parent.parent == nil {
		return
	}
	if edits := in.edits[e.id]; len(edits) > 0 {
		for i := range edits {
			ed := &edits[i]
			switch {
			case ed.off:
				unsetStyle(e, ed.name)
			case ed.value != "":
				ed.valid = setStyle(e, ed.name, ed.value)
			}
		}
	}
	for c := e.first; c != nil; c = c.next {
		in.applyEditsTo(c)
	}
}

// changes returns how many declarations the user changed.
func (in *inspector) changes() int {
	n := 0
	for _, edits := range in.edits {
		n += len(edits)
	}
	return n
}

// resetEdits drops every edit: the elements are what the view builds again.
func (in *inspector) resetEdits() {
	in.edits, in.sources = nil, nil
	in.editing = inspEditing{}
	in.changed = true
}

// editOf returns the edit of a declaration, nil for none.
func (in *inspector) editOf(id uint64, name string) *inspEdit {
	edits := in.edits[id]
	for i := range edits {
		if edits[i].name == name {
			return &edits[i]
		}
	}
	return nil
}

// putEdit adds or replaces an edit.
func (in *inspector) putEdit(id uint64, ed inspEdit) {
	if have := in.editOf(id, ed.name); have != nil {
		*have = ed
	} else {
		if in.edits == nil {
			in.edits = map[uint64][]inspEdit{}
		}
		in.edits[id] = append(in.edits[id], ed)
	}
	in.changed = true
}

// dropEdit removes the edit of a declaration.
func (in *inspector) dropEdit(id uint64, name string) {
	edits := slices.DeleteFunc(in.edits[id], func(ed inspEdit) bool { return ed.name == name })
	if len(edits) == 0 {
		delete(in.edits, id)
	} else {
		in.edits[id] = edits
	}
	in.changed = true
}

// toggle turns declaration d, at index in the rule of element id, off, or
// on again.
func (in *inspector) toggle(id uint64, d inspDecl, index int) {
	ed := in.editOf(id, d.name)
	switch {
	case ed == nil:
		in.putEdit(id, inspEdit{name: d.name, value: d.value, off: true, valid: true, index: index, color: d.color, swatch: d.swatch})
	case ed.off && !ed.changed:
		// As the view sets it, so that the view's changes show again.
		in.dropEdit(id, d.name)
	default:
		ed.off = !ed.off
		ed.index, ed.color, ed.swatch = index, d.color, d.swatch
		in.changed = true
	}
}

// startEdit starts typing the value of declaration d, at index in the rule
// of element id; without a name, declarations to add.
func (in *inspector) startEdit(id uint64, d inspDecl, index int) {
	in.finishEdit(true)
	ed := inspEditing{active: true, id: id, name: d.name, text: d.value, start: d.value, last: d.value,
		index: index, color: d.color, swatch: d.swatch, focus: true}
	if have := in.editOf(id, d.name); have != nil {
		ed.before, ed.hadBefore = *have, true
	}
	in.editing = ed
}

// previewEdit applies the text typed so far, as Chrome does.
func (in *inspector) previewEdit() {
	ed := &in.editing
	if !ed.active || ed.text == ed.last {
		return
	}
	ed.last = ed.text
	if ed.name == "" {
		return // whole declarations apply once typed
	}
	in.putEdit(ed.id, inspEdit{name: ed.name, value: strings.TrimSpace(ed.text), changed: true, valid: true,
		added: ed.hadBefore && ed.before.added, index: ed.index, color: ed.color, swatch: ed.swatch})
}

// finishEdit ends typing, keeping what was typed or going back to what was
// there.
func (in *inspector) finishEdit(keep bool) {
	ed := in.editing
	if !ed.active {
		return
	}
	in.editing = inspEditing{}
	in.changed = true
	text := strings.TrimSpace(ed.text)
	switch {
	case ed.name == "":
		if keep {
			in.addDeclarations(ed.id, text)
		}
	case !keep || ed.text == ed.start:
		if ed.hadBefore {
			in.putEdit(ed.id, ed.before)
		} else {
			in.dropEdit(ed.id, ed.name)
		}
	case text == "" && ed.hadBefore && ed.before.added:
		// Cleared: one the user added goes, and the view's turns off.
		in.dropEdit(ed.id, ed.name)
	case text == "":
		in.putEdit(ed.id, inspEdit{name: ed.name, value: ed.start, off: true, valid: true, index: ed.index, color: ed.color, swatch: ed.swatch})
	}
}

// addDeclarations adds declarations typed as "name: value; name: value",
// or changes those the element has.
func (in *inspector) addDeclarations(id uint64, text string) {
	for _, decl := range cssSplit(text, ';') {
		name, value, ok := strings.Cut(decl, ":")
		name, value = strings.ToLower(strings.TrimSpace(name)), strings.TrimSpace(value)
		if !ok || name == "" || value == "" {
			continue
		}
		added := true
		if have := in.editOf(id, name); have != nil {
			added = have.added
		}
		in.putEdit(id, inspEdit{name: name, value: value, changed: true, added: added, valid: true, index: math.MaxInt32})
	}
}

// overlayEdits shows the edits of the element in rule r: values as typed,
// and what is off or not understood, which the elements do not show.
// inherits, for a rule the element inherits, tells which of them it shows.
func (in *inspector) overlayEdits(r *inspRule, inherits func(name string) bool) {
	for _, ed := range in.edits[r.id] {
		if inherits != nil && !inherits(ed.name) {
			continue
		}
		at := slices.IndexFunc(r.decls, func(d inspDecl) bool { return d.name == ed.name })
		d := inspDecl{name: ed.name, value: ed.value, off: ed.off, edited: ed.changed, invalid: !ed.off && !ed.valid, added: ed.added}
		if at >= 0 {
			if have := r.decls[at]; d.invalid {
				d.value = ed.value
			} else {
				d.color, d.swatch = have.color, have.swatch
			}
			r.decls[at] = d
			// One value stands for all, as for box shadows.
			r.decls = slices.DeleteFunc(r.decls, func(o inspDecl) bool { return o.name == ed.name && o.value != ed.value })
			continue
		}
		if ed.off {
			d.color, d.swatch = ed.color, ed.swatch
		} else if c, ok := parseCSSColor(ed.value); ok && !d.invalid {
			d.color, d.swatch = c, true
		}
		r.decls = slices.Insert(r.decls, min(ed.index, len(r.decls)), d)
	}
}

// editable reports whether the Styles tab can change declaration d.
func editable(d inspDecl) bool {
	if d.name == "display" && d.value == "grid" {
		return false
	}
	return d.added || styleKnown(d.name)
}

// styleKnown reports whether setStyle and unsetStyle know the property.
func styleKnown(name string) bool {
	switch name {
	case "flex-direction", "flex-wrap", "gap", "row-gap", "column-gap", "justify-content", "align-items", "align-content", "align-self",
		"top", "right", "bottom", "left", "width", "height", "min-width", "min-height", "max-width", "max-height",
		"aspect-ratio", "flex-grow", "flex-shrink", "flex-basis", "padding", "margin", "border", "border-width", "border-color", "border-style",
		"border-radius", "filter", "background", "background-color", "box-shadow", "opacity", "visibility", "display",
		"color", "font-family", "font-size", "font-weight", "font-style", "line-height", "letter-spacing", "text-align",
		"text-decoration", "font-feature-settings", "selection-color", "caret-color", "line-clamp", "white-space", "z-index":
		return true
	}
	return false
}

// setStyle sets a property of e as the view's calls would, and reports
// whether it knows the property and understands the value.
func setStyle(e *node, name, value string) bool {
	value = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(value), ";"))
	lower := strings.ToLower(value)
	lengths := map[string]*length{"width": &e.width, "height": &e.height, "flex-basis": &e.basis}
	if l, ok := lengths[name]; ok {
		v, ok := parseCSSLength(lower)
		if ok {
			*l = v
		}
		return ok
	}
	bounds := map[string]*bound{"min-width": &e.minW, "min-height": &e.minH, "max-width": &e.maxW, "max-height": &e.maxH}
	if b, ok := bounds[name]; ok {
		v, ok := parseCSSBound(lower, strings.HasPrefix(name, "max-"))
		if ok {
			*b = v
		}
		return ok
	}
	switch name {
	case "flex-direction":
		if e.grid {
			return false
		}
		switch lower {
		case "row", "row-reverse", "column", "column-reverse":
			e.row = strings.HasPrefix(lower, "row")
			e.reverse = strings.HasSuffix(lower, "-reverse")
			return true
		}
	case "flex-wrap":
		switch lower {
		case "nowrap", "wrap", "wrap-reverse":
			e.wrap, e.wrapReverse = lower != "nowrap", lower == "wrap-reverse"
			return true
		}
	case "gap":
		v, ok := parseCSSNumbers(lower)
		switch {
		case ok && len(v) == 1:
			e.gapX, e.gapY = v[0], v[0]
			return true
		case ok && len(v) == 2:
			e.gapY, e.gapX = v[0], v[1]
			return true
		}
	case "row-gap":
		return setCSSNumber(lower, &e.gapY)
	case "column-gap":
		return setCSSNumber(lower, &e.gapX)
	case "justify-content":
		return setCSSAlign(lower, &e.justify)
	case "align-items":
		return setCSSAlign(lower, &e.align)
	case "align-content":
		return setCSSAlign(lower, &e.alignContent)
	case "align-self":
		return setCSSAlign(lower, &e.self)
	case "top", "right", "bottom", "left":
		side := map[string]int{"top": 0, "right": 1, "bottom": 2, "left": 3}[name]
		v, ok := parseCSSLength(lower)
		if ok {
			e.inset[side] = v
			if side%2 == 1 {
				e.unlog(side, logInset)
			}
		}
		return ok
	case "aspect-ratio":
		if a, b, ok := strings.Cut(lower, "/"); ok {
			x, okX := parseCSSNumber(a)
			y, okY := parseCSSNumber(b)
			if okX && okY && y > 0 {
				e.aspect = x / y
				return true
			}
			return false
		}
		return setCSSNumber(lower, &e.aspect)
	case "flex-grow":
		v, ok := parseCSSNumber(lower)
		if ok {
			e.Grow(v)
		}
		return ok
	case "flex-shrink":
		return setCSSNumber(lower, &e.shrink)
	case "padding":
		v, ok := parseCSSEdges(lower, false)
		if ok {
			e.pad = v
			e.logical &^= logPad
		}
		return ok
	case "margin":
		v, ok := parseCSSEdges(lower, true)
		if ok {
			e.margin = v
			e.logical &^= logMargin
		}
		return ok
	case "border":
		return setCSSBorder(e, value)
	case "border-width":
		v, ok := parseCSSEdges(lower, false)
		if ok {
			e.border = v
			e.logical &^= logBorder
		}
		return ok
	case "border-color":
		c, ok := parseCSSColor(value)
		if ok {
			e.borderC = c
		}
		return ok
	case "border-style":
		switch lower {
		case "solid":
			e.borderStyle = BorderSolid
			return true
		case "dashed":
			e.borderStyle = BorderDashed
			return true
		}
	case "border-radius":
		v, ok := parseCSSEdges(lower, false)
		if ok {
			e.radius = v
		}
		return ok
	case "filter":
		if lower == "none" {
			e.blur = 0
			return true
		}
		if inner, ok := strings.CutPrefix(lower, "blur("); ok && strings.HasSuffix(inner, ")") {
			v, ok := parseCSSNumber(strings.TrimSuffix(inner, ")"))
			if ok && v >= 0 {
				e.blur = v
				return true
			}
		}
	case "background", "background-color":
		c, ok := parseCSSColor(value)
		if ok {
			if name == "background-color" && e.ts.set&setBackground != 0 {
				e.ts.background = c
			} else {
				e.bg, e.fill = c, fillColor
			}
		}
		return ok
	case "box-shadow":
		return setCSSShadows(e, value)
	case "opacity":
		v, ok := parseCSSNumber(lower)
		if p, isPct := strings.CutSuffix(lower, "%"); isPct {
			v, ok = parseCSSNumber(p)
			v /= 100
		}
		if ok {
			e.Opacity(v)
		}
		return ok
	case "visibility":
		switch lower {
		case "hidden":
			e.flags |= flagInvisible
			return true
		case "visible":
			e.flags &^= flagInvisible
			return true
		}
	case "display":
		switch lower {
		case "none":
			e.hidden = true
			return true
		case "flex":
			if !e.grid {
				e.hidden = false
				return true
			}
		case "grid":
			if e.grid {
				e.hidden = false
				return true
			}
		}
	case "color":
		c, ok := parseCSSColor(value)
		if ok {
			e.TextColor(c)
		}
		return ok
	case "font-family":
		if value = strings.Trim(value, `"'`); value != "" {
			e.Font(value)
			return true
		}
	case "font-size":
		v, ok := parseCSSNumber(lower)
		if ok && v > 0 {
			e.FontSize(v)
			return true
		}
	case "font-weight":
		switch lower {
		case "normal":
			e.FontWeight(400)
			return true
		case "bold":
			e.FontWeight(700)
			return true
		}
		if w, err := strconv.Atoi(lower); err == nil && w >= 1 && w <= 1000 {
			e.FontWeight(w)
			return true
		}
	case "font-style":
		switch lower {
		case "italic":
			e.Italic()
			return true
		case "normal":
			e.ts.italic = false
			e.ts.set |= setItalic
			return true
		}
	case "line-height":
		if p, ok := strings.CutSuffix(lower, "px"); ok {
			v, ok := parseCSSNumber(p)
			if ok && v > 0 {
				e.FixedLineHeight(v)
			}
			return ok && v > 0
		}
		v, ok := parseCSSNumber(lower)
		if ok && v > 0 {
			e.LineHeight(v)
			return true
		}
	case "letter-spacing":
		v, ok := parseCSSNumber(lower)
		if ok {
			e.LetterSpacing(v)
		}
		return ok
	case "text-align":
		switch lower {
		case "start":
			e.TextAlign(Start)
			return true
		case "center":
			e.TextAlign(Center)
			return true
		case "end":
			e.TextAlign(End)
			return true
		}
	case "text-decoration":
		return setCSSDecoration(e, lower)
	case "font-feature-settings":
		e.ts.features = value
		e.ts.set |= setFeatures
		return true
	case "selection-color":
		c, ok := parseCSSColor(value)
		if ok {
			e.ts.selection = c
			e.ts.set |= setSelection
		}
		return ok
	case "caret-color":
		c, ok := parseCSSColor(value)
		if ok {
			e.ts.caret = c
			e.ts.set |= setCaret
		}
		return ok
	case "line-clamp":
		if n, err := strconv.Atoi(lower); err == nil && n >= 0 {
			e.maxLines = n
			return true
		}
	case "z-index":
		if lower == "auto" {
			e.ZIndex(0)
			return true
		}
		if n, err := strconv.Atoi(lower); err == nil {
			e.ZIndex(n)
			return true
		}
	case "white-space":
		switch lower {
		case "nowrap":
			e.noWrap = true
			return true
		case "normal":
			e.noWrap = false
			if e.single {
				e.single, e.maxLines = false, 0
			}
			return true
		}
	}
	return false
}

// unsetStyle leaves e as though the view had not set a property.
func unsetStyle(e *node, name string) {
	switch name {
	case "flex-direction":
		if !e.grid {
			e.row, e.reverse = false, false
		}
	case "flex-wrap":
		e.wrap, e.wrapReverse = false, false
	case "gap":
		e.gapX, e.gapY = 0, 0
	case "row-gap":
		e.gapY = 0
	case "column-gap":
		e.gapX = 0
	case "justify-content":
		e.justify = alignAuto
	case "align-items":
		e.align = alignAuto
	case "align-content":
		e.alignContent = alignAuto
	case "align-self":
		e.self = alignAuto
	case "top":
		e.inset[0] = length{}
	case "right":
		e.inset[1] = length{}
		e.unlog(1, logInset)
	case "bottom":
		e.inset[2] = length{}
	case "left":
		e.inset[3] = length{}
		e.unlog(3, logInset)
	case "width":
		e.width = length{}
	case "height":
		e.height = length{}
	case "min-width":
		e.minW = bound{}
	case "min-height":
		e.minH = bound{}
	case "max-width":
		e.maxW = bound{}
	case "max-height":
		e.maxH = bound{}
	case "aspect-ratio":
		e.aspect = 0
	case "flex-grow":
		e.grow = 0
		// The basis Grow gave it.
		if e.basis == percent(0) {
			e.basis = length{}
		}
	case "flex-shrink":
		e.shrink = 1
	case "flex-basis":
		e.basis = length{}
	case "padding":
		e.pad = [4]float32{}
		e.logical &^= logPad
	case "margin":
		e.margin = [4]float32{}
		e.logical &^= logMargin
	case "border", "border-width":
		e.border = [4]float32{}
		e.logical &^= logBorder
	case "border-color":
		e.borderC = Color{}
	case "border-style":
		e.borderStyle = BorderSolid
	case "border-radius":
		e.radius = [4]float32{}
	case "filter":
		e.blur = 0
	case "background", "background-color":
		e.bg, e.fill = Transparent, fillColor
		e.ts.set &^= setBackground
	case "box-shadow":
		e.shadows = nil
	case "opacity":
		e.opacity, e.opacitySet = 1, false
	case "visibility":
		e.flags &^= flagInvisible
	case "display":
		e.hidden = false
	case "color":
		e.ts.set &^= setColor
	case "font-family":
		e.ts.set &^= setFamily
	case "font-size":
		e.ts.set &^= setSize
	case "font-weight":
		e.ts.set &^= setWeight
	case "font-style":
		e.ts.set &^= setItalic
	case "line-height":
		e.ts.set &^= setLineHeight
	case "letter-spacing":
		e.ts.set &^= setSpacing
	case "text-align":
		e.ts.set &^= setAlign
	case "text-decoration":
		e.ts.set &^= setUnderline | setStrike
	case "font-feature-settings":
		e.ts.set &^= setFeatures
	case "selection-color":
		e.ts.set &^= setSelection
	case "caret-color":
		e.ts.set &^= setCaret
	case "line-clamp":
		e.maxLines = 0
	case "z-index":
		e.zIndex = 0
	case "white-space":
		e.noWrap = false
		if e.single {
			e.single, e.maxLines = false, 0
		}
	}
}

// textStyleBit returns the bit of textStyle.set of an inherited property,
// 0 for a property elements do not inherit.
func textStyleBit(name string) uint32 {
	switch name {
	case "color":
		return setColor
	case "font-family":
		return setFamily
	case "font-size":
		return setSize
	case "font-weight":
		return setWeight
	case "font-style":
		return setItalic
	case "line-height":
		return setLineHeight
	case "letter-spacing":
		return setSpacing
	case "text-align":
		return setAlign
	case "text-decoration":
		return setUnderline | setStrike
	case "font-feature-settings":
		return setFeatures
	case "selection-color":
		return setSelection
	case "caret-color":
		return setCaret
	case "background-color":
		return setBackground
	}
	return 0
}

// parseCSSNumber parses a number of DIPs, with or without "px".
func parseCSSNumber(s string) (float32, bool) {
	s = strings.TrimSuffix(strings.TrimSpace(s), "px")
	v, err := strconv.ParseFloat(s, 32)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return float32(v), true
}

func setCSSNumber(s string, to *float32) bool {
	v, ok := parseCSSNumber(s)
	if ok {
		*to = v
	}
	return ok
}

// parseCSSNumbers parses numbers separated by spaces.
func parseCSSNumbers(s string) ([]float32, bool) {
	var out []float32
	for _, f := range strings.Fields(s) {
		v, ok := parseCSSNumber(f)
		if !ok {
			return nil, false
		}
		out = append(out, v)
	}
	return out, len(out) > 0
}

// parseCSSLength parses a length: DIPs, a percentage, or auto.
func parseCSSLength(s string) (length, bool) {
	if s == "auto" {
		return length{}, true
	}
	if p, ok := strings.CutSuffix(s, "%"); ok {
		v, ok := parseCSSNumber(p)
		return percent(v), ok
	}
	v, ok := parseCSSNumber(s)
	return px(v), ok
}

// parseCSSBound parses a minimum or a maximum size: a length, auto, none
// for a maximum, or a percentage and DIPs in max() for a minimum and in
// min() for a maximum, as cssBound writes them.
func parseCSSBound(s string, upper bool) (bound, bool) {
	var b bound
	add := func(l length) {
		switch l.u {
		case unitPx:
			b.setPx(l.v)
		case unitPercent:
			b.setPct(l.v)
		}
	}
	fn := "max("
	if upper {
		fn = "min("
	}
	if args, ok := strings.CutPrefix(s, fn); ok {
		args, ok = strings.CutSuffix(args, ")")
		x, y, two := strings.Cut(args, ",")
		if !ok || !two {
			return bound{}, false
		}
		lx, okx := parseCSSLength(strings.TrimSpace(x))
		ly, oky := parseCSSLength(strings.TrimSpace(y))
		if !okx || !oky || lx.u == unitAuto || ly.u == unitAuto || lx.u == ly.u {
			// An element holds one percentage and one size in DIPs.
			return bound{}, false
		}
		add(lx)
		add(ly)
		return b, true
	}
	if upper && s == "none" {
		return b, true
	}
	l, ok := parseCSSLength(s)
	add(l)
	return b, ok
}

// parseCSSEdges parses one to four sides, as CSS writes them.
func parseCSSEdges(s string, auto bool) ([4]float32, bool) {
	fields := strings.Fields(s)
	if len(fields) == 0 || len(fields) > 4 {
		return [4]float32{}, false
	}
	vs := make([]float32, len(fields))
	for i, f := range fields {
		switch v, ok := parseCSSNumber(f); {
		case ok:
			vs[i] = v
		case auto && f == "auto":
			vs[i] = float32(math.Inf(1))
		default:
			return [4]float32{}, false
		}
	}
	return edges(vs), true
}

func setCSSAlign(s string, to *Align) bool {
	switch s {
	case "auto", "normal":
		*to = alignAuto
	case "flex-start", "start":
		*to = Start
	case "center":
		*to = Center
	case "flex-end", "end":
		*to = End
	case "stretch":
		*to = Stretch
	case "space-between":
		*to = SpaceBetween
	case "space-around":
		*to = SpaceAround
	case "space-evenly":
		*to = SpaceEvenly
	case "baseline":
		*to = Baseline
	default:
		return false
	}
	return true
}

// setCSSBorder sets the border from its widths, style and color, in any
// order, as "1px solid #ccc" or "0 0 1px 0 dashed red"; those left out
// stay.
func setCSSBorder(e *node, value string) bool {
	if strings.EqualFold(value, "none") {
		e.border = [4]float32{}
		e.logical &^= logBorder
		return true
	}
	var widths []float32
	style, c := e.borderStyle, e.borderC
	for _, f := range cssFields(value) {
		lower := strings.ToLower(f)
		if v, ok := parseCSSNumber(lower); ok {
			widths = append(widths, v)
		} else if lower == "solid" {
			style = BorderSolid
		} else if lower == "dashed" {
			style = BorderDashed
		} else if parsed, ok := parseCSSColor(f); ok {
			c = parsed
		} else {
			return false
		}
	}
	if len(widths) > 4 {
		return false
	}
	if len(widths) > 0 {
		e.border = edges(widths)
		e.logical &^= logBorder
	}
	e.borderStyle, e.borderC = style, c
	return true
}

// setCSSShadows sets the box shadows, separated by commas: "inset", two to
// four lengths and a color, as "0 2px 8px 0 #0004", or none.
func setCSSShadows(e *node, value string) bool {
	if strings.EqualFold(value, "none") {
		e.shadows = nil
		return true
	}
	var shadows []shadow
	for _, one := range cssSplit(value, ',') {
		var sh shadow
		var lens []float32
		hasColor := false
		for _, f := range cssFields(one) {
			lower := strings.ToLower(f)
			if lower == "inset" {
				sh.inset = true
			} else if v, ok := parseCSSNumber(lower); ok {
				lens = append(lens, v)
			} else if c, ok := parseCSSColor(f); ok {
				sh.color, hasColor = c, true
			} else {
				return false
			}
		}
		if len(lens) < 2 || len(lens) > 4 {
			return false
		}
		if !hasColor {
			sh.color = RGB(0, 0, 0)
		}
		sh.x, sh.y = lens[0], lens[1]
		if len(lens) > 2 {
			sh.blur = lens[2]
		}
		if len(lens) > 3 {
			sh.spread = lens[3]
		}
		shadows = append(shadows, sh)
	}
	if len(shadows) == 0 {
		return false
	}
	e.shadows = shadows
	return true
}

func setCSSDecoration(e *node, value string) bool {
	if value == "none" {
		e.ts.underline, e.ts.strike = false, false
		e.ts.set |= setUnderline | setStrike
		return true
	}
	underline, wavy, dotted, strike := false, false, false, false
	for _, f := range strings.Fields(value) {
		switch f {
		case "underline":
			underline = true
		case "wavy":
			wavy = true
		case "dotted":
			dotted = true
		case "line-through":
			strike = true
		default:
			return false
		}
	}
	if !underline && !strike {
		return false
	}
	e.ts.underline, e.ts.wavy, e.ts.dotted, e.ts.solid = underline, wavy, dotted, false
	e.ts.strike = strike
	e.ts.set |= setUnderline | setStrike
	return true
}

// parseCSSColor parses the colors colorText writes (hex and oklch()), and
// rgb() and some names, as one types them.
func parseCSSColor(s string) (Color, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	switch {
	case strings.HasPrefix(s, "#"):
		c, err := parseHex(s)
		return c, err == nil
	case strings.HasPrefix(s, "rgb(") || strings.HasPrefix(s, "rgba("):
		v, ok := cssFunctionArgs(s)
		if !ok || len(v) < 3 || len(v) > 4 {
			return Color{}, false
		}
		byteOf := func(x float32) uint8 { return uint8(max(0, min(x, 255)) + 0.5) }
		c := RGB(byteOf(v[0]), byteOf(v[1]), byteOf(v[2]))
		if len(v) == 4 {
			c.A = alphaByte(v[3])
		}
		return c, true
	case strings.HasPrefix(s, "oklch("):
		v, ok := cssFunctionArgs(s)
		if !ok || len(v) < 3 || len(v) > 4 {
			return Color{}, false
		}
		c := Oklch(v[0], v[1], v[2])
		if len(v) == 4 {
			c.A = alphaByte(v[3])
		}
		return c, true
	}
	switch s {
	case "transparent":
		return Transparent, true
	case "black":
		return RGB(0, 0, 0), true
	case "white":
		return RGB(255, 255, 255), true
	case "red":
		return RGB(255, 0, 0), true
	case "green":
		return RGB(0, 128, 0), true
	case "blue":
		return RGB(0, 0, 255), true
	case "yellow":
		return RGB(255, 255, 0), true
	case "orange":
		return RGB(255, 165, 0), true
	case "purple":
		return RGB(128, 0, 128), true
	case "gray", "grey":
		return RGB(128, 128, 128), true
	}
	return Color{}, false
}

// cssFunctionArgs parses the numbers of "name(a b c / d)" or "name(a, b, c,
// d)".
func cssFunctionArgs(s string) ([]float32, bool) {
	open, end := strings.IndexByte(s, '('), strings.LastIndexByte(s, ')')
	if open < 0 || end != len(s)-1 {
		return nil, false
	}
	inner := strings.NewReplacer(",", " ", "/", " ").Replace(s[open+1 : end])
	return parseCSSNumbers(inner)
}

// cssFields splits s at spaces outside parentheses.
func cssFields(s string) []string {
	var out []string
	for _, f := range cssSplit(s, ' ') {
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

// cssSplit splits s at sep outside parentheses, trimming the parts and
// leaving out empty ones.
func cssSplit(s string, sep byte) []string {
	var out []string
	depth, from := 0, 0
	flush := func(to int) {
		if p := strings.TrimSpace(s[from:to]); p != "" {
			out = append(out, p)
		}
	}
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		case sep:
			if depth == 0 {
				flush(i)
				from = i + 1
			}
		}
	}
	flush(len(s))
	return out
}

// nudge returns text with its first number moved by d, as the arrow keys
// do in Chrome's Styles tab, keeping its unit. Hex colors have no number
// to move.
func nudge(text string, d float64) string {
	for i := 0; i < len(text); {
		if text[i] == ' ' {
			i++
			continue
		}
		end := i
		for end < len(text) && text[end] != ' ' {
			end++
		}
		token := text[i:end]
		num := 0
		for num < len(token) && (token[num] >= '0' && token[num] <= '9' || token[num] == '.' || num == 0 && token[num] == '-') {
			num++
		}
		if v, err := strconv.ParseFloat(token[:num], 64); err == nil {
			next := strconv.FormatFloat(math.Round((v+d)*1000)/1000, 'f', -1, 64)
			return text[:i] + next + text[i+num:]
		}
		i = end
	}
	return text
}
