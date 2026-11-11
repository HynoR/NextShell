//go:build !mygo_noinspector

package ui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Copying what the Styles tab changed as Go, for changes that outlast the
// inspector: a line of calls for each element, to paste into the view.

// changesAsGo returns the changes as Go, by element in the order of the
// tree: where the element is built, what to add to it, and what to remove.
func (in *inspector) changesAsGo() string {
	var ids []uint64
	for i := range in.nodes {
		id := in.nodes[i].id
		if len(in.edits[id]) > 0 && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	var b strings.Builder
	for _, id := range ids {
		n := &in.nodes[in.nodeOf(id)]
		fmt.Fprintf(&b, "// %s", n.name)
		if n.keyText != "" {
			fmt.Fprintf(&b, "#%s", n.keyText)
		}
		if s := in.sources[id]; s != "" {
			fmt.Fprintf(&b, ", %s", shortSource(s))
		}
		b.WriteByte('\n')
		var calls []string
		for _, ed := range in.edits[id] {
			switch {
			case ed.off:
				fmt.Fprintf(&b, "// Remove %s: %s\n", ed.name, ed.value)
			case !ed.valid || ed.value == "":
				fmt.Fprintf(&b, "// Not understood, %s: %s\n", ed.name, ed.value)
			default:
				code := goCalls(ed.name, ed.value)
				if code == nil {
					fmt.Fprintf(&b, "// No call for %s: %s\n", ed.name, ed.value)
				}
				calls = append(calls, code...)
			}
		}
		if len(calls) > 0 {
			b.WriteString(strings.Join(calls, ""))
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

// goCalls returns the calls of package ui that set a property to a value,
// nil for one that has none.
func goCalls(name, value string) []string {
	// The parsers read the value, on an element of its own.
	e := &node{shrink: 1}
	if !setStyle(e, name, value) {
		return nil
	}
	lengths := map[string]struct {
		l      length
		method string
	}{
		"width": {e.width, "Width"}, "height": {e.height, "Height"}, "flex-basis": {e.basis, "Basis"},
		"top": {e.inset[0], "Top"}, "right": {e.inset[1], "Right"}, "bottom": {e.inset[2], "Bottom"}, "left": {e.inset[3], "Left"},
	}
	if l, ok := lengths[name]; ok {
		switch l.l.u {
		case unitPx:
			return []string{call(l.method, goNum(l.l.v))}
		case unitPercent:
			return []string{call(l.method+"Percent", goNum(l.l.v))}
		}
		return nil // auto, as without a call
	}
	bounds := map[string]struct {
		b      bound
		method string
	}{"min-width": {e.minW, "MinWidth"}, "min-height": {e.minH, "MinHeight"}, "max-width": {e.maxW, "MaxWidth"}, "max-height": {e.maxH, "MaxHeight"}}
	if b, ok := bounds[name]; ok {
		var calls []string // none for auto, as without a call
		if b.b.set&boundPct != 0 {
			calls = append(calls, call(b.method+"Percent", goNum(b.b.pct)))
		}
		if b.b.set&boundPx != 0 {
			calls = append(calls, call(b.method, goNum(b.b.px)))
		}
		return calls
	}
	lower := strings.ToLower(strings.TrimSpace(value))
	switch name {
	case "flex-direction":
		calls := []string{call("Column")}
		if e.row {
			calls = []string{call("Row")}
		}
		if e.reverse {
			calls = append(calls, call("Reverse"))
		}
		return calls
	case "flex-wrap":
		switch {
		case e.wrapReverse:
			return []string{call("WrapReverse")}
		case e.wrap:
			return []string{call("Wrap")}
		}
	case "gap":
		if e.gapX == e.gapY {
			return []string{call("Gap", goNum(e.gapX))}
		}
		return []string{call("GapY", goNum(e.gapY)), call("GapX", goNum(e.gapX))}
	case "row-gap":
		return []string{call("GapY", goNum(e.gapY))}
	case "column-gap":
		return []string{call("GapX", goNum(e.gapX))}
	case "justify-content":
		return goAlign("Justify", e.justify)
	case "align-items":
		return goAlign("AlignItems", e.align)
	case "align-content":
		return goAlign("AlignContent", e.alignContent)
	case "align-self":
		return goAlign("AlignSelf", e.self)
	case "aspect-ratio":
		return []string{call("AspectRatio", goNum(e.aspect))}
	case "flex-grow":
		return []string{call("Grow", goNum(e.grow))}
	case "flex-shrink":
		return []string{call("Shrink", goNum(e.shrink))}
	case "padding":
		return []string{call("Padding", goEdges(e.pad)...)}
	case "margin":
		return []string{call("Margin", goEdges(e.margin)...)}
	case "border":
		var calls []string
		switch {
		case e.border[0] == e.border[1] && e.border[1] == e.border[2] && e.border[2] == e.border[3]:
			calls = append(calls, call("Border", goNum(e.border[0]), goColor(e.borderC)))
		default:
			calls = append(calls, call("BorderWidth", goEdges(e.border)...), call("BorderColor", goColor(e.borderC)))
		}
		if e.borderStyle == BorderDashed {
			calls = append(calls, call("BorderStyle", "ui.BorderDashed"))
		}
		return calls
	case "border-width":
		return []string{call("BorderWidth", goEdges(e.border)...)}
	case "border-color":
		return []string{call("BorderColor", goColor(e.borderC))}
	case "border-style":
		if e.borderStyle == BorderDashed {
			return []string{call("BorderStyle", "ui.BorderDashed")}
		}
		return []string{call("BorderStyle", "ui.BorderSolid")}
	case "border-radius":
		// Radius takes one corner for all, or the four.
		r := e.radius
		if r[0] == r[1] && r[1] == r[2] && r[2] == r[3] {
			return []string{call("Radius", goNum(r[0]))}
		}
		return []string{call("Radius", goNum(r[0]), goNum(r[1]), goNum(r[2]), goNum(r[3]))}
	case "filter":
		if e.blur > 0 {
			return []string{call("Blur", goNum(e.blur))}
		}
	case "background", "background-color":
		return []string{call("Background", goColor(e.bg))}
	case "box-shadow":
		if len(e.shadows) == 0 {
			return []string{call("NoShadow")}
		}
		var calls []string
		for _, sh := range e.shadows {
			method := "Shadow"
			if sh.inset {
				method = "InsetShadow"
			}
			calls = append(calls, call(method, goNum(sh.x), goNum(sh.y), goNum(sh.blur), goNum(sh.spread), goColor(sh.color)))
		}
		return calls
	case "opacity":
		return []string{call("Opacity", goNum(e.opacity))}
	case "visibility":
		if e.flags&flagInvisible != 0 {
			return []string{call("Invisible")}
		}
	case "display":
		if e.hidden {
			return []string{call("Hidden", "true")}
		}
	case "color":
		return []string{call("TextColor", goColor(e.ts.color))}
	case "font-family":
		return []string{call("Font", strconv.Quote(e.ts.family))}
	case "font-size":
		return []string{call("FontSize", goNum(e.ts.size))}
	case "font-weight":
		return []string{call("FontWeight", strconv.Itoa(e.ts.weight))}
	case "font-style":
		if e.ts.italic {
			return []string{call("Italic")}
		}
	case "line-height":
		if e.ts.fixedLine {
			return []string{call("FixedLineHeight", goNum(e.ts.lineHeight))}
		}
		return []string{call("LineHeight", goNum(e.ts.lineHeight))}
	case "letter-spacing":
		return []string{call("LetterSpacing", goNum(e.ts.spacing))}
	case "text-align":
		return goAlign("TextAlign", e.ts.align)
	case "text-decoration":
		var calls []string
		switch {
		case e.ts.underline && e.ts.wavy:
			calls = append(calls, call("WavyUnderline"))
		case e.ts.underline && e.ts.dotted:
			calls = append(calls, call("DottedUnderline"))
		case e.ts.underline:
			calls = append(calls, call("Underline"))
		}
		if e.ts.strike {
			calls = append(calls, call("Strikethrough"))
		}
		return calls
	case "font-feature-settings":
		return []string{call("FontFeatures", strconv.Quote(e.ts.features))}
	case "selection-color":
		return []string{call("SelectionColor", goColor(e.ts.selection))}
	case "caret-color":
		return []string{call("CaretColor", goColor(e.ts.caret))}
	case "line-clamp":
		return []string{call("MaxLines", strconv.Itoa(e.maxLines))}
	case "z-index":
		return []string{call("ZIndex", strconv.Itoa(int(e.zIndex)))}
	case "white-space":
		if lower == "nowrap" {
			return []string{call("NoWrap")}
		}
	}
	return nil
}

// call formats a call of an element's method.
func call(method string, args ...string) string {
	return "." + method + "(" + strings.Join(args, ", ") + ")"
}

// goNum formats a number as an argument.
func goNum(v float32) string {
	if isAuto(v) {
		return "ui.Auto"
	}
	return strconv.FormatFloat(float64(v), 'f', -1, 32)
}

// goEdges formats the four sides as the fewest arguments that say them.
func goEdges(v [4]float32) []string {
	switch {
	case v[0] == v[1] && v[1] == v[2] && v[2] == v[3]:
		return []string{goNum(v[0])}
	case v[0] == v[2] && v[1] == v[3]:
		return []string{goNum(v[0]), goNum(v[1])}
	}
	return []string{goNum(v[0]), goNum(v[1]), goNum(v[2]), goNum(v[3])}
}

// goAlign returns the call of method with a, none for the automatic value.
func goAlign(method string, a Align) []string {
	names := map[Align]string{Start: "Start", Center: "Center", End: "End", Stretch: "Stretch",
		SpaceBetween: "SpaceBetween", SpaceAround: "SpaceAround", SpaceEvenly: "SpaceEvenly", Baseline: "Baseline"}
	if n, ok := names[a]; ok {
		return []string{call(method, "ui."+n)}
	}
	return nil
}

// goColor formats a color as Go, with the values colorText tells: a wide
// color as Oklch's arguments.
func goColor(c Color) string {
	if c == Transparent {
		return "ui.Transparent"
	}
	s := colorText(c)
	if !strings.HasPrefix(s, "oklch(") {
		return `ui.Hex("` + s + `")`
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(s, "oklch("), ")")
	lch, alpha, hasAlpha := strings.Cut(inner, " / ")
	out := "ui.Oklch(" + strings.Join(strings.Fields(lch), ", ") + ")"
	if hasAlpha {
		out += ".Alpha(" + strings.TrimSpace(alpha) + ")"
	}
	return out
}
