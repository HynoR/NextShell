//go:build !mygo_noinspector

package ui

import (
	"fmt"
	"hash/maphash"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/internal/gamut"
	"github.com/egoist/mygo/internal/platform"
)

// The inspector shows the elements of a window's frames in a panel docked
// beside the content, as Chrome's developer tools show a page's: the tree
// of elements as markup, the styles, box model and properties of the one
// chosen in the tree or picked in the window, which it outlines over the
// content with a tooltip, how long frames take, and issues, as duplicate
// keys. Windows whose DevTools are on open it with the Toggle Developer
// Tools menu item or Ctrl+Alt+F12. Terminal copy/paste chords are reserved.
//
// The panel is built with the view's elements, as the last child of the
// root, which the content leaves room for: the content's root is narrower
// by the panel's width. It shows the elements of the frame before, which
// the inspector notes after each frame is laid out, asking for another
// frame when they changed (inspector_panel.go builds it,
// inspector_overlay.go paints over the content).

// inspectorWidth is the width the inspector's panel starts at.
const inspectorWidth = 420

// inspectorKey keys the panel under the root.
const inspectorKey = "mygo.inspector"

// The tabs of the panel, and of the sidebar of its Elements tab.
const (
	inspElements = iota
	inspPerformance
	inspIssues
)

const (
	inspStyles = iota
	inspComputed
	inspProperties
)

// inspHistory is how many frames the Performance tab shows.
const inspHistory = 120

type inspector struct {
	// enabled tells that the window's DevTools let the inspector open.
	enabled bool
	open    bool
	// picking is set while the pointer picks an element in the content,
	// and swallow while the release of the press that picked it goes
	// nowhere.
	picking, swallow bool
	// selected is the element chosen, hovered the element under the
	// pointer in the tree, or in the content while picking; reveal shows
	// the element chosen in the tree, opening the nodes around it.
	selected, hovered uint64
	reveal            bool
	// source is where the app built the element chosen.
	source string
	// panel is the ID of the panel, width its width, and appW the width
	// it leaves the content. tab is the panel's tab, side the tab of the
	// sidebar below the tree, split the height of the tree.
	panel       uint64
	width, appW float32
	tab, side   int
	split       float32
	// nodes are the elements of the last frame, in the order of the tree,
	// with the keys the frame gave them; sel describes the element chosen.
	// sum hashes them, and shown is the sum the panel was built from.
	// changed asks for a panel built anew, as warnings come.
	nodes   []inspNode
	keys    map[uint64]any
	seen    map[uint64]bool
	sel     inspDetails
	sum     uint64
	shown   uint64
	changed bool
	// rows are the rows of the tree: the nodes inside no node closed, and
	// the closing tags of the nodes open. opened holds the nodes the user
	// opened or closed, by ID. list is the tree's, listID its ID in the
	// last frame, and row the row chosen.
	rows   []inspRow
	opened map[uint64]bool
	list   ListState
	listID uint64
	row    int
	// The find bar of the tree, and the nodes matching its query.
	finding bool
	query   string
	match   int
	matches []int32
	// edits are what the Styles tab changed, by element, until the
	// inspector closes (inspector_edit.go), and editing the value being
	// typed.
	edits   map[uint64][]inspEdit
	editing inspEditing
	// sources are where the app builds the elements chosen that have
	// edits, as changesAsGo tells.
	sources map[uint64]string
	// forced are the states forced on elements, by element, until the
	// inspector closes, and statesOpen shows the Styles tab's bar of them.
	forced     map[uint64]uint8
	statesOpen bool
	// openable notes whether the file of the element's source can open.
	openable openableSource
	// layoutOn holds the containers whose layout shows over the content,
	// tracks what their grids noted as they laid out, and overlays what
	// the walk of this frame's elements found of them. uncached tells that
	// they turned the layout cache off, which cacheWasOff says it was.
	layoutOn              map[uint64]bool
	tracks                map[uint64]inspTracks
	overlays              []inspOverlay
	uncached, cacheWasOff bool
	// Scroll offsets of the breadcrumbs, the sidebar and the issues.
	crumbs       ScrollState
	sideScroll   ScrollState
	issuesScroll ScrollState
	// selElem and hoverElem are the elements of the frame being painted
	// with those IDs.
	selElem, hoverElem *node
	// times are how long the last frame took to build, lay out and paint,
	// counted from lapAt; elements is how many it had, and history the
	// times of the last frames, frames of them in all.
	times    [3]time.Duration
	lapAt    time.Time
	elements int
	history  [inspHistory][3]time.Duration
	frames   int
	// hz is the refresh rate of the window's display.
	hz float32
	// theme is the panel's, of colors pal.
	theme Theme
	pal   inspPalette
}

// inspNode is an element of the tree: its tag (name), the attributes it
// shows (its key and label), and the text of a text. key keys its row:
// the element's ID, unless another element had it (a duplicate key).
type inspNode struct {
	id, key  uint64
	depth    int32
	parent   int32
	kids     bool
	name     string
	layout   uint8 // what its badge shows
	keyText  string
	label    string
	text     string
	w, h     float32
	leaving  bool
	hasTrans bool
}

// inspRow is a row of the tree: a node's opening tag, or the closing tag
// of a node open.
type inspRow struct {
	node  int32
	close bool
}

// contentWidth returns the width the window w wide leaves the content.
func (in *inspector) contentWidth(w float32) float32 {
	if !in.open {
		in.appW = w
		return w
	}
	if in.width == 0 {
		in.width = inspectorWidth
	}
	pw := min(max(in.width, 260), w-160)
	if w < 520 {
		pw = w / 2
	}
	in.appW = max(w-pw, 0)
	return in.appW
}

// lap notes how long the frame's phase took since the last lap (-1 starts
// the frame), and the frame's times once it painted.
func (in *inspector) lap(phase int) {
	if !in.open {
		return
	}
	now := time.Now()
	if phase < 0 {
		clear(in.keys)
	} else {
		in.times[phase] = now.Sub(in.lapAt)
	}
	if phase == 2 {
		in.history[in.frames%inspHistory] = in.times
		in.frames++
	}
	in.lapAt = now
}

// repainted notes a frame that painted the elements of the last one again
// (engine.repaintFrame), which took d.
func (in *inspector) repainted(d time.Duration) {
	if !in.open {
		return
	}
	in.times = [3]time.Duration{0, 0, d}
	in.history[in.frames%inspHistory] = in.times
	in.frames++
}

// toggleInspector opens or closes the inspector.
func (rt *engine) toggleInspector() {
	in := &rt.insp
	in.open = !in.open
	in.picking, in.hovered = false, 0
	if !in.open {
		in.nodes, in.rows, in.sel = nil, nil, inspDetails{}
		// What the Styles tab changed goes with the panel: the window shows
		// what the view builds again.
		in.edits, in.editing, in.sources, in.forced = nil, inspEditing{}, nil, nil
		in.layoutOn, in.tracks, in.overlays = nil, nil, nil
		in.keepLaidOut(rt, false)
	} else if in.keys == nil {
		in.keys = map[uint64]any{}
	}
	rt.requestFrame()
}

// inspectKey toggles the inspector only for Ctrl+Alt+F12,
// and stops picking for Escape.
func (rt *engine) inspectKey(mods Modifiers, key Key) bool {
	in := &rt.insp
	if in.picking && key == KeyEscape && mods == 0 {
		in.picking, in.hovered = false, 0
		rt.requestFrame()
		return true
	}
	if !in.enabled {
		return false
	}
	switch {
	case key == KeyF12 && mods == Ctrl|Alt:
		rt.toggleInspector()
		return true
	}
	return false
}

// isForced reports whether the inspector forces one of the states on the
// element id.
func (in *inspector) isForced(id uint64, states uint8) bool {
	return in.forced != nil && in.forced[id]&states != 0
}

// toggleForced forces a state on the element id, or stops forcing it.
func (in *inspector) toggleForced(id uint64, state uint8) {
	if in.forced == nil {
		in.forced = map[uint64]uint8{}
	}
	if in.forced[id] ^= state; in.forced[id] == 0 {
		delete(in.forced, id)
	}
	in.changed = true
}

// forcedNames lists the states forced on the element id, as CSS names them.
func (in *inspector) forcedNames(id uint64) string {
	var names []string
	for _, s := range inspStates {
		if in.isForced(id, s.state) {
			names = append(names, s.name)
		}
	}
	return strings.Join(names, " ")
}

// inspStates are the states the inspector can force, as CSS names them.
var inspStates = [...]struct {
	name  string
	state uint8
}{
	{":hover", forceHover},
	{":active", forceActive},
	{":focus", forceFocus},
	{":focus-visible", forceFocusVisible},
	{":focus-within", forceFocusWithin},
}

// noteSource notes where the app builds the element chosen.
func (in *inspector) noteSource() { in.source = callSite() }

// noteKey notes the key of an element, which the tree shows.
func (in *inspector) noteKey(id uint64, k any) {
	if in.keys != nil {
		in.keys[id] = k
	}
}

// holds reports whether the element id is in the panel while it shows.
func (in *inspector) holds(rt *engine, id uint64) bool {
	return in.open && in.panel != 0 && rt.within(id, in.panel)
}

// pointer takes the pointer over the content while picking: moving
// outlines the element under it, and a press chooses it. It reports
// whether it took ev.
func (in *inspector) pointer(rt *engine, ev platform.SurfaceEvent, x, y float32) bool {
	switch {
	case in.swallow && (ev.Kind == platform.PointerUp || ev.Kind == platform.PointerCancel):
		in.swallow = false
		return true
	case !in.picking || x >= in.appW:
		return false
	}
	switch ev.Kind {
	case platform.PointerMove:
		var id uint64
		if chain := rt.hitChain(x, y); len(chain) > 0 {
			id = chain[0]
		}
		if id != in.hovered {
			in.hovered = id
			rt.requestFrame()
		}
		return true
	case platform.PointerDown:
		if chain := rt.hitChain(x, y); len(chain) > 0 {
			in.choose(chain[0])
			in.reveal = true
		}
		in.picking, in.hovered, in.swallow = false, 0, true
		rt.requestFrame()
		return true
	case platform.PointerUp, platform.PointerCancel:
		return true
	}
	return false
}

// choose chooses the element id.
func (in *inspector) choose(id uint64) {
	if id != in.selected {
		in.finishEdit(true)
		in.selected, in.source = id, ""
		// The breadcrumbs show the element chosen, at their end.
		in.crumbs.X = math.MaxFloat32
		in.sideScroll.Y = 0
	}
}

// snapshot notes the elements of the frame laid out under root, and asks
// for another frame when the panel showed others.
func (in *inspector) snapshot(rt *engine, root *node) {
	in.nodes = in.nodes[:0]
	in.selElem, in.hoverElem = nil, nil
	in.overlays = in.overlays[:0]
	if in.seen == nil {
		in.seen = map[uint64]bool{}
	}
	clear(in.seen)
	var sum uint64
	in.walk(rt, root, 0, -1, &sum)
	in.measureLayouts()
	in.elements = len(in.nodes)
	in.sel = inspDetails{}
	if e := in.selElem; e != nil {
		in.sel = describe(rt, e)
		if in.source != "" && len(in.edits[e.id]) > 0 {
			if in.sources == nil {
				in.sources = map[uint64]string{}
			}
			in.sources[e.id] = in.source
		}
	}
	sum = mix(sum, in.sel.hash())
	sum = mix(sum, uint64(len(rt.warnings)))
	in.sum = sum
	if in.sum != in.shown || in.changed {
		rt.animating = true
	}
}

func (in *inspector) walk(rt *engine, e *node, depth, parent int32, sum *uint64) {
	if e.id == in.panel && e.parent != nil && e.parent.parent == nil {
		return
	}
	i := len(in.nodes)
	key := e.id
	if in.seen[key] {
		key = mix(key, uint64(i))
	}
	in.seen[key] = true
	n := inspNode{id: e.id, key: key, depth: depth, parent: parent, name: elementName(e), label: e.label,
		w: e.w, h: e.h, leaving: e.leaving != 0, layout: layoutKind(e)}
	if k, ok := in.keys[e.id]; ok {
		n.keyText = fmt.Sprint(k)
	}
	if e.kind == kindText {
		n.text = e.text
	}
	if r := e.st.trec; r != nil && r.built == rt.frame && r.elem == e {
		n.hasTrans = true
	}
	in.nodes = append(in.nodes, n)
	*sum = mix(*sum, e.id^uint64(depth)<<56^uint64(n.layout)<<48)
	*sum = mix(*sum, uint64(math.Float32bits(e.x))<<32|uint64(math.Float32bits(e.y)))
	*sum = mix(*sum, uint64(math.Float32bits(e.w))<<32|uint64(math.Float32bits(e.h)))
	if n.text != "" || n.label != "" || n.keyText != "" {
		*sum = mix(*sum, maphash.String(keySeed, n.text+"\x00"+n.label+"\x00"+n.keyText))
	}
	if e.id == in.selected {
		in.selElem = e
	}
	if e.id == in.hovered {
		in.hoverElem = e
	}
	if n.layout != inspNoLayout && !e.hidden && in.layoutOn[e.id] {
		in.overlays = append(in.overlays, inspOverlay{e: e})
	}
	for c := e.first; c != nil; c = c.next {
		in.walk(rt, c, depth+1, int32(i), sum)
	}
	in.nodes[i].kids = len(in.nodes)-1 > i
}

// isOpen reports whether the tree shows what is inside node n: what the
// user chose, else whether it is near the root.
func (in *inspector) isOpen(n *inspNode) bool {
	if v, ok := in.opened[n.id]; ok {
		return v
	}
	return n.depth < 3
}

// expandTo opens the nodes around the node id, and reports whether it
// opened one.
func (in *inspector) expandTo(id uint64) bool {
	i := in.nodeOf(id)
	if i < 0 {
		return false
	}
	opened := false
	for p := in.nodes[i].parent; p >= 0; p = in.nodes[p].parent {
		if n := &in.nodes[p]; !in.isOpen(n) {
			in.opened[n.id] = true
			opened = true
		}
	}
	return opened
}

// nodeOf returns the index of the node id, -1 for none.
func (in *inspector) nodeOf(id uint64) int32 {
	for i := range in.nodes {
		if in.nodes[i].id == id {
			return int32(i)
		}
	}
	return -1
}

// setOpen opens or closes node i, and with all, the nodes inside it.
func (in *inspector) setOpen(i int32, open, all bool) {
	n := &in.nodes[i]
	in.opened[n.id] = open
	if !all {
		return
	}
	for j := i + 1; j < int32(len(in.nodes)) && in.nodes[j].depth > n.depth; j++ {
		if in.nodes[j].kids {
			in.opened[in.nodes[j].id] = open
		}
	}
}

// elementName returns the widget an element is, else its role, else its
// kind: its tag in the tree.
func elementName(e *node) string {
	if e.widget != "" {
		return e.widget
	}
	if n := roleName(e.role); n != "" {
		return n
	}
	switch {
	case e.flags&flagEditable != 0:
		return "TextInput"
	case e.role == RoleAuto && e.flags&flagClickable != 0 && e.flags&flagFocusable != 0:
		return "Button" // as assistive technology sees it
	}
	switch e.kind {
	case kindText:
		if e.spans != nil {
			return "RichText"
		}
		return "Text"
	case kindImage:
		return "Image"
	case kindIcon:
		return "Icon"
	case kindInput:
		return "Input"
	}
	switch {
	case e.parent == nil:
		return "Root"
	case e.id == overlayID:
		return "Overlay"
	case e.grid:
		return "Grid"
	case e.flags&(flagScrollX|flagScrollY) != 0:
		return "Scroll"
	case e.row:
		return "Row"
	}
	return "Column"
}

func roleName(r Role) string {
	switch r {
	case RoleButton:
		return "Button"
	case RoleLink:
		return "Link"
	case RoleCheckBox:
		return "Checkbox"
	case RoleRadio:
		return "Radio"
	case RoleSwitch:
		return "Switch"
	case RoleSlider:
		return "Slider"
	case RoleProgress:
		return "Progress"
	case RoleTextField:
		return "TextField"
	case RoleList:
		return "List"
	case RoleDialog:
		return "Dialog"
	case RoleAlertDialog:
		return "AlertDialog"
	case RolePopup:
		return "Popup"
	case RoleTooltip:
		return "Tooltip"
	case RolePopUpButton:
		return "Select"
	case RoleTabList:
		return "Tabs"
	case RoleTab:
		return "Tab"
	case RoleSplitter:
		return "Splitter"
	case RoleStatus:
		return "Status"
	case RoleTable:
		return "Table"
	case RoleRow:
		return "TableRow"
	case RoleCell:
		return "Cell"
	case RoleColumnHeader:
		return "ColumnHeader"
	case RoleTree:
		return "Tree"
	case RoleTreeItem:
		return "TreeItem"
	case RoleListItem:
		return "ListItem"
	case RoleMenuButton:
		return "MenuButton"
	case RoleToolbar:
		return "Toolbar"
	case RoleRadioGroup:
		return "RadioGroup"
	case RoleToggleButton:
		return "Toggle"
	case RoleComboBox:
		return "Combobox"
	case RoleDisclosure:
		return "Disclosure"
	case RoleMeter:
		return "Meter"
	case RoleStepper:
		return "Stepper"
	case RoleColorWell:
		return "ColorWell"
	case RoleMenu:
		return "Menu"
	case RoleMenuBar:
		return "MenuBar"
	case RoleMenuItem:
		return "MenuItem"
	case RoleMenuItemCheckBox:
		return "MenuItemCheckbox"
	case RoleMenuItemRadio:
		return "MenuItemRadio"
	case RoleRegion:
		return "Region"
	case RoleLog:
		return "Log"
	case RoleTimer:
		return "Timer"
	case RoleAlert:
		return "Alert"
	case RoleSeparator:
		return "Separator"
	case RoleRowHeader:
		return "RowHeader"
	case RoleSearch:
		return "Search"
	case RoleArticle:
		return "Article"
	case RoleHeading:
		return "Heading"
	case RoleNavigation:
		return "Navigation"
	case RoleGrid:
		return "Grid"
	case RoleTreeGrid:
		return "TreeGrid"
	case RoleListBox:
		return "ListBox"
	case RoleOption:
		return "Option"
	case RoleDeletion:
		return "Deletion"
	case RoleInsertion:
		return "Insertion"
	}
	return ""
}

// accessibleRole returns the role assistive technology sees, named as
// Chrome names roles, and whether it sees the element.
func accessibleRole(e *node) (string, bool) {
	r, ok := e.accessRole()
	if !ok {
		return "none", false
	}
	switch e.role {
	case RoleListBox:
		return "listbox", true
	case RoleOption:
		return "option", true
	case RoleDeletion:
		return "deletion", true
	case RoleInsertion:
		return "insertion", true
	}
	switch role := Role(r) + RoleGroup; role {
	case RoleGroup:
		return "group", true
	case RoleText:
		return "text", true
	case RoleImage:
		return "image", true
	case RoleScroll:
		return "scroll area", true
	case RoleTextField:
		return "textbox", true
	case RolePopUpButton:
		return "combobox", true
	default:
		return strings.ToLower(roleName(role)), true
	}
}

// clip returns s cut to n runes, on one line.
func clip(s string, n int) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i] + "…"
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "…"
}

// num formats a size: whole, or with one decimal.
func num(v float32) string {
	if v == float32(math.Round(float64(v))) {
		return strconv.Itoa(int(v))
	}
	return strconv.FormatFloat(float64(v), 'f', 1, 32)
}

// pxText formats a length in DIPs as CSS writes pixels.
func pxText(v float32) string {
	if v == 0 {
		return "0"
	}
	return num(v) + "px"
}

// dec formats v with at most n decimals, without trailing zeros.
func dec(v float64, n int) string {
	s := strconv.FormatFloat(v, 'f', n, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return s
}

func ms(d time.Duration) string { return strconv.FormatFloat(float64(d)/1e6, 'f', 1, 64) }

// colorText formats a color as CSS writes it: hex, or oklch() for one
// outside the sRGB gamut, whose hex would be its nearest sRGB color and not
// the wide color a window drawing a wide gamut shows.
func colorText(c Color) string {
	if c.wide.ok() {
		r, g, b := c.wide.rgb()
		l, ch, h := gamut.Oklch(gamut.Oklab(gamut.Decode(float64(r)), gamut.Decode(float64(g)), gamut.Decode(float64(b))))
		s := "oklch(" + dec(l, 3) + " " + dec(ch, 3) + " " + dec(h, 1)
		if c.A != 255 {
			s += " / " + dec(float64(c.A)/255, 2)
		}
		return s + ")"
	}
	if c.A == 255 {
		return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
	}
	return fmt.Sprintf("#%02x%02x%02x%02x", c.R, c.G, c.B, c.A)
}
