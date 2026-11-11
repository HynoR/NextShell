package automationcli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// maxText is how many characters of a label or a value a line shows,
// unless full.
const maxText = 120

// line describes an element on a line, without its ref: its role, its
// label, value, key and key shortcuts, and its states, leaving out what
// it lacks. full adds what agents seldom need: placeholders under a
// label, descriptions, ranges, selections, levels, actions, bounds and
// the element's ID, and writes long text whole.
func line(n *node, focus string, full bool) string {
	var b strings.Builder
	b.WriteString("[" + n.Role + "]")
	if n.Label != "" {
		b.WriteString(" " + quote(n.Label, full))
	}
	if n.Value != "" {
		b.WriteString(" value=" + quote(n.Value, full))
	} else if n.Now != nil {
		b.WriteString(" value=" + num(*n.Now))
	}
	if n.Placeholder != "" && (full || n.Label == "") {
		b.WriteString(" placeholder=" + quote(n.Placeholder, full))
	}
	if len(n.Key) > 0 {
		b.WriteString(" key=" + keyText(n.Key))
	}
	if n.Shortcuts != "" {
		b.WriteString(" keyshortcuts=" + quote(n.Shortcuts, true))
	}
	for _, s := range n.States {
		// Every control can take the focus: saying so is noise.
		if s != "focusable" {
			b.WriteString(" " + s)
		}
	}
	if n.Current != "" {
		b.WriteString(" current=" + n.Current)
	}
	if n.Complete != "" {
		b.WriteString(" autocomplete=" + n.Complete)
	}
	if n.HasPopup != "" {
		b.WriteString(" haspopup=" + n.HasPopup)
	}
	if n.RoleDescription != "" {
		b.WriteString(" roledescription=" + quote(n.RoleDescription, full))
	}
	if n.ID == focus {
		b.WriteString(" focused")
	}
	if n.WebView {
		b.WriteString(" webview")
	}
	if !full {
		return b.String()
	}
	if n.Description != "" {
		b.WriteString(" description=" + quote(n.Description, true))
	}
	if n.Min != nil && n.Max != nil {
		b.WriteString(" range=" + num(*n.Min) + ".." + num(*n.Max))
	}
	if n.Step != nil && *n.Step != 0 {
		b.WriteString(" step=" + num(*n.Step))
	}
	if n.Selection != nil {
		fmt.Fprintf(&b, " selection=%d,%d", n.Selection[0], n.Selection[1])
	}
	if n.Level != 0 {
		fmt.Fprintf(&b, " level=%d", n.Level)
	}
	if n.SetSize != 0 {
		fmt.Fprintf(&b, " item=%d/%d", n.PosInSet, n.SetSize)
	}
	// A table's or a grid's size and the place of a row or a cell in it,
	// as the app gave them (aria-rowcount and the like), and the rows and
	// columns a cell covers (aria-rowspan, aria-colspan).
	for _, v := range [...]struct {
		name string
		n    int
	}{{"rowcount", n.RowCount}, {"colcount", n.ColCount}, {"rowindex", n.RowIndex}, {"colindex", n.ColIndex},
		{"rowspan", n.RowSpan}, {"colspan", n.ColSpan}} {
		if v.n != 0 {
			fmt.Fprintf(&b, " %s=%d", v.name, v.n)
		}
	}
	if len(n.Actions) > 0 {
		b.WriteString(" actions=" + strings.Join(n.Actions, ","))
	}
	r := n.Bounds
	fmt.Fprintf(&b, " bounds=%s,%s,%sx%s id=%s", num(r.X), num(r.Y), num(r.Width), num(r.Height), n.ID)
	return b.String()
}

// quote quotes s, cut to maxText characters unless full.
func quote(s string, full bool) string {
	if !full {
		if r := []rune(s); len(r) > maxText {
			return strconv.Quote(string(r[:maxText])+"…") + fmt.Sprintf(" (%d more)", len(r)-maxText)
		}
	}
	return strconv.Quote(s)
}

func num(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

var bareKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.:/-]*$`)

// keyText writes a key as selectors take it: a string that reads as no
// other value bare, other strings quoted, numbers and booleans as JSON
// writes them.
func keyText(k json.RawMessage) string {
	var s string
	if json.Unmarshal(k, &s) == nil && bareKey.MatchString(s) && s != "true" && s != "false" && s != "null" {
		return s
	}
	var b bytes.Buffer
	if json.Compact(&b, k) != nil {
		return string(k)
	}
	return b.String()
}

// keyJSON is the JSON a key selector names: JSON as itself, as key=5,
// key=true or key="a b", anything else as a string.
func keyJSON(s string) []byte {
	var v any
	if json.Unmarshal([]byte(s), &v) == nil {
		switch v.(type) {
		case string, float64, bool:
			var b bytes.Buffer
			json.Compact(&b, []byte(s))
			return b.Bytes()
		}
	}
	b, _ := json.Marshal(s)
	return b
}

// controls are the roles snapshot -i keeps even when they have no action
// now, as a disabled button.
var controls = map[string]bool{
	"button": true, "link": true, "checkBox": true, "radio": true, "switch": true,
	"slider": true, "textField": true, "popUpButton": true, "tab": true,
	"menuButton": true, "toggleButton": true, "comboBox": true, "disclosure": true,
	"stepper": true, "colorWell": true, "menuItem": true, "menuItemCheckBox": true,
	"menuItemRadio": true, "treeItem": true,
}

// interactive tells whether snapshot -i shows n: controls, and elements
// with actions other than scrolling into view.
func interactive(n *node) bool {
	if controls[n.Role] || n.WebView {
		return true
	}
	for _, a := range n.Actions {
		if a != "scrollIntoView" {
			return true
		}
	}
	return false
}

// writeSnapshot writes a window's elements, a line each, indented under
// the elements shown around them.
func writeSnapshot(w io.Writer, sn *seen, announcements []string, interactiveOnly, full bool) {
	// inner is the depth of an element's children.
	inner := make(map[string]int, len(sn.Nodes))
	shown := 0
	for i := range sn.Nodes {
		n := &sn.Nodes[i]
		depth := inner[n.Parent]
		show := !interactiveOnly || interactive(n)
		inner[n.ID] = depth
		if !show {
			continue
		}
		inner[n.ID] = depth + 1
		shown++
		fmt.Fprintf(w, "%s%s %s\n", strings.Repeat("  ", depth), ref(sn.Refs[n.ID]), line(n, sn.Focus, full))
	}
	switch {
	case shown > 0:
	case interactiveOnly && len(sn.Nodes) > 0:
		fmt.Fprintln(w, "(no interactive elements: run snapshot without -i)")
	default:
		fmt.Fprintln(w, "(no elements)")
	}
	writeAnnouncements(w, announcements)
}

func writeAnnouncements(w io.Writer, announcements []string) {
	for _, a := range announcements {
		fmt.Fprintf(w, "announced %s\n", quote(a, false))
	}
}

// windowLine describes a window on a line.
func windowLine(w windowInfo, selected bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "w%d [%s] %s", w.ID, w.Kind, quote(w.Title, false))
	if w.URL != "" {
		b.WriteString(" url=" + w.URL)
	}
	if w.Focused {
		b.WriteString(" focused")
	}
	if !w.Visible {
		b.WriteString(" hidden")
	}
	if selected {
		b.WriteString(" selected")
	}
	return b.String()
}

// diffLine is a line of a diff: '+' an element added, '-' one gone or as
// it was, '=' one unchanged around them.
type diffLine struct {
	op   byte
	ref  int
	text string
}

func (d diffLine) String() string { return string(d.op) + " " + ref(d.ref) + " " + d.text }

// diff compares what a window showed before with what it shows now, in
// the order of now, and keeps context unchanged elements around each
// change. An element whose line changed shows twice, as it was ('-') and
// as it is ('+'), with its ref; one gone shows with its ref, now stale.
func diff(before, now *seen, context int) []diffLine {
	if before == nil {
		before = &seen{}
	}
	at := make(map[string]int, len(before.Nodes))
	for i, n := range before.Nodes {
		if _, ok := at[n.ID]; !ok {
			at[n.ID] = i
		}
	}
	stays := make(map[string]bool, len(now.Nodes))
	for _, n := range now.Nodes {
		stays[n.ID] = true
	}
	var all []diffLine
	next := 0
	gone := func(upTo int) {
		for ; next < upTo; next++ {
			if n := &before.Nodes[next]; !stays[n.ID] {
				all = append(all, diffLine{'-', before.Refs[n.ID], line(n, before.Focus, false)})
			}
		}
	}
	for i := range now.Nodes {
		n := &now.Nodes[i]
		r, text := now.Refs[n.ID], line(n, now.Focus, false)
		p, ok := at[n.ID]
		if !ok {
			all = append(all, diffLine{'+', r, text})
			continue
		}
		if p >= next {
			gone(p)
			next = p + 1
		}
		if was := line(&before.Nodes[p], before.Focus, false); was != text {
			all = append(all, diffLine{'-', r, was}, diffLine{'+', r, text})
		} else {
			all = append(all, diffLine{'=', r, text})
		}
	}
	gone(len(before.Nodes))

	var out []diffLine
	for i, d := range all {
		keep := d.op != '='
		for j := max(i-context, 0); !keep && j <= min(i+context, len(all)-1); j++ {
			keep = all[j].op != '='
		}
		if keep {
			out = append(out, d)
		}
	}
	return out
}
