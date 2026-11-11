//go:build darwin

package darwin

import (
	"slices"
	"sync"
	"unicode/utf16"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	"github.com/egoist/mygo/internal/platform"
)

// VoiceOver and other assistive technology see the content of a surface
// as NSAccessibilityElements, one per node of the content's accessibility
// tree, which the surface view contains. Elements are kept from update to
// update by node ID, so that VoiceOver's cursor stays on them, and only
// what changed is set again. The content describes frames once AppKit
// first asks the view for its children. Lists are tables of rows, as AppKit's
// own lists, and VoiceOver reads their rows "row 5 of 10,000" from their
// index among all of their table's (AXIndex) and how many it has
// (AXRowCount), rather than from the rows built, as a list builds only those
// in view; the table says which rows show (AXVisibleRows) and which are
// chosen (AXSelectedRows). What is in a scroll container takes
// AXScrollToVisible.

// accessElement is the element of a node.
type accessElement struct {
	obj      id // MyGoAccessibilityElement, owned
	s        *surface
	node     platform.AccessNode
	parent   id
	children []uint64
	// webView is the web view whose page is the first child.
	webView *window
	// chosen are the rows of a list or table that are chosen, and count
	// its rows, built or not.
	chosen []uint64
	count  int
	// chosenCells are the cells of a table that are chosen, and linked
	// the elements the element controls (AXLinkedUIElements).
	chosenCells []uint64
	linked      []id
	// cellRow says a row is chosen as it holds a chosen cell, and at
	// where a cell is in its table, its row and column from 1, 0 where
	// unknown, and how many rows and columns it covers.
	cellRow bool
	at      [4]int
	// headers are the column headers of a table, and header that of a
	// cell's column (AXColumnHeaderUIElements).
	headers []uint64
	header  uint64
}

var (
	accessOnce     sync.Once
	postAccessNote uintptr // NSAccessibilityPostNotification
	// postAccessInfo is NSAccessibilityPostNotificationWithUserInfo.
	postAccessInfo uintptr
	// actionDescription is NSAccessibilityActionDescription.
	actionDescription uintptr
	msgPointToPoint   func(obj id, sel objc.SEL, p NSPoint) NSPoint
)

func loadAccess() {
	accessOnce.Do(func() {
		postAccessNote = mustDlsym(libAppKit, "NSAccessibilityPostNotification")
		postAccessInfo = mustDlsym(libAppKit, "NSAccessibilityPostNotificationWithUserInfo")
		actionDescription = mustDlsym(libAppKit, "NSAccessibilityActionDescription")
		purego.RegisterFunc(&msgPointToPoint, msgSendAddr)
	})
}

// postNote posts an accessibility notification about obj.
func postNote(obj id, name string) {
	if obj != 0 {
		purego.SyscallN(postAccessNote, uintptr(obj), uintptr(nsString(name)))
	}
}

// announce asks VoiceOver to read text out, after what it is reading, or
// at once for an alert (NSAccessibilityAnnouncementRequestedNotification).
func announce(app id, text string, alert bool) {
	info := send(class("NSMutableDictionary"), "dictionary")
	send(info, "setObject:forKey:", uintptr(nsString(text)), uintptr(nsString("AXAnnouncementKey")))
	priority := 50 // NSAccessibilityPriorityMedium
	if alert {
		priority = 90 // NSAccessibilityPriorityHigh, which interrupts
	}
	send(info, "setObject:forKey:", uintptr(nsNumberInt(priority)), uintptr(nsString("AXPriorityKey")))
	purego.SyscallN(postAccessInfo, uintptr(app), uintptr(nsString("AXAnnouncementRequested")), uintptr(info))
}

// accessRoles are the roles and subroles of node roles.
var accessRoles = map[platform.AccessRole][2]string{
	platform.RoleGroup:        {"AXGroup", ""},
	platform.RoleText:         {"AXStaticText", ""},
	platform.RoleButton:       {"AXButton", ""},
	platform.RoleLink:         {"AXLink", ""},
	platform.RoleCheckBox:     {"AXCheckBox", ""},
	platform.RoleRadio:        {"AXRadioButton", ""},
	platform.RoleSwitch:       {"AXCheckBox", "AXSwitch"},
	platform.RoleSlider:       {"AXSlider", ""},
	platform.RoleProgress:     {"AXProgressIndicator", ""},
	platform.RoleTextField:    {"AXTextField", ""},
	platform.RoleImage:        {"AXImage", ""},
	platform.RoleList:         {"AXTable", ""},
	platform.RoleScroll:       {"AXScrollArea", ""},
	platform.RoleDialog:       {"AXGroup", "AXDialog"},
	platform.RolePopup:        {"AXPopover", ""},
	platform.RoleTooltip:      {"AXHelpTag", ""},
	platform.RolePopUpButton:  {"AXPopUpButton", ""},
	platform.RoleTabList:      {"AXTabGroup", ""},
	platform.RoleTab:          {"AXRadioButton", "AXTabButton"},
	platform.RoleSplitter:     {"AXSplitter", ""},
	platform.RoleStatus:       {"AXGroup", "AXApplicationStatus"},
	platform.RoleTable:        {"AXTable", ""},
	platform.RoleRow:          {"AXRow", "AXTableRow"},
	platform.RoleCell:         {"AXCell", ""},
	platform.RoleColumnHeader: {"AXCell", ""},
	platform.RoleTree:         {"AXOutline", ""},
	platform.RoleTreeItem:     {"AXRow", "AXOutlineRow"},
	platform.RoleListItem:     {"AXRow", "AXTableRow"},
	platform.RoleMenuButton:   {"AXMenuButton", ""},
	platform.RoleToolbar:      {"AXToolbar", ""},
	platform.RoleRadioGroup:   {"AXRadioGroup", ""},
	platform.RoleToggleButton: {"AXCheckBox", "AXToggle"},
	platform.RoleComboBox:     {"AXComboBox", ""},
	platform.RoleDisclosure:   {"AXDisclosureTriangle", ""},
	platform.RoleMeter:        {"AXLevelIndicator", ""},
	platform.RoleStepper:      {"AXIncrementor", ""},
	platform.RoleColorWell:    {"AXColorWell", ""},
	platform.RoleAlertDialog:  {"AXGroup", "AXApplicationAlertDialog"},
	platform.RoleMenu:         {"AXMenu", ""},
	platform.RoleMenuBar:      {"AXMenuBar", ""},
	// AppKit's items show a choice with a mark (AXMenuItemMarkChar).
	platform.RoleMenuItem:         {"AXMenuItem", ""},
	platform.RoleMenuItemCheckBox: {"AXMenuItem", ""},
	platform.RoleMenuItemRadio:    {"AXMenuItem", ""},
	// WebKit's headings, whose value is their level, and landmarks.
	platform.RoleHeading:    {"AXHeading", ""},
	platform.RoleNavigation: {"AXGroup", "AXLandmarkNavigation"},
	platform.RoleRegion:     {"AXGroup", "AXLandmarkRegion"},
	platform.RoleSearch:     {"AXGroup", "AXLandmarkSearch"},
	// An article, as WebKit's.
	platform.RoleArticle: {"AXGroup", "AXDocumentArticle"},
	// Live regions, as WebKit's, and a rule, as WebKit's separator.
	platform.RoleLog:       {"AXGroup", "AXApplicationLog"},
	platform.RoleTimer:     {"AXGroup", "AXApplicationTimer"},
	platform.RoleAlert:     {"AXGroup", "AXApplicationAlert"},
	platform.RoleSeparator: {"AXSplitter", ""},
	platform.RoleRowHeader: {"AXCell", ""},
	// Grids and tree grids are tables, as WebKit's.
	platform.RoleGrid:     {"AXTable", ""},
	platform.RoleTreeGrid: {"AXTable", ""},
	// Content removed and added, as WebKit's del and ins.
	platform.RoleDeletion:  {"AXGroup", "AXDeleteStyleGroup"},
	platform.RoleInsertion: {"AXGroup", "AXInsertStyleGroup"},
}

// orientation returns the NSAccessibilityOrientation of a node, as
// aria-orientation: Vertical (1) or Horizontal (2) where the node says,
// else Horizontal for a slider or a rule, Unknown (0) for others.
func orientation(n platform.AccessNode) int {
	switch {
	case n.States&platform.AccessVertical != 0:
		return 1
	case n.States&platform.AccessHorizontal != 0, n.Role == platform.RoleSlider || n.Role == platform.RoleSeparator:
		return 2
	}
	return 0
}

// errorMessages reports whether a node points at the elements holding its
// error (AXErrorMessageElements, aria-errormessage): only while invalid.
func errorMessages(n platform.AccessNode) bool {
	return len(n.ErrorMessage) > 0 && n.States&platform.AccessInvalid != 0
}

// menuItem reports whether nodes of a role are items of a menu.
func menuItem(r platform.AccessRole) bool {
	return r == platform.RoleMenuItem || r == platform.RoleMenuItemCheckBox || r == platform.RoleMenuItemRadio
}

// markChar returns the mark an item of a menu shows for its choice, as
// AppKit's: a check mark when on, a dash when mixed, none otherwise.
func markChar(n platform.AccessNode) string {
	switch {
	case !menuItem(n.Role):
	case n.States&platform.AccessMixed != 0:
		return "-"
	case n.States&platform.AccessChecked != 0:
		return "✓"
	}
	return ""
}

// chooses reports whether a node is the row of a list, a table or an
// outline that its list chooses, or a cell of a table chosen or not, as
// ARIA's gridcells with aria-selected.
func chooses(n platform.AccessNode) bool {
	return (n.Role == platform.RoleListItem || n.Role == platform.RoleRow || n.Role == platform.RoleTreeItem || cell(n.Role)) && n.States&platform.AccessSelectable != 0
}

// sortStates are the orders a column's header says its table's rows are
// sorted in.
const sortStates = platform.AccessSortAscending | platform.AccessSortDescending | platform.AccessSortOther

// showsMenu reports whether a node opens a popup as it is pressed, which
// AXShowMenu does, as WebKit's elements with aria-haspopup.
func showsMenu(n platform.AccessNode) bool {
	return n.HasPopup != platform.PopupNone && n.Actions&platform.ActionPress != 0
}

// cell reports whether nodes of a role are cells of a table, headers
// included.
func cell(r platform.AccessRole) bool {
	return r == platform.RoleCell || r == platform.RoleColumnHeader || r == platform.RoleRowHeader
}

// placeCells tells the cells of a table's row where they are, the row
// from 1 or 0 for its header, as NSTableView's cells say
// (AXRowIndexRange, AXColumnIndexRange), the ranges as long as the rows
// and columns they cover (aria-rowspan, aria-colspan), and the header of
// their column among headers, and reports whether one of them is chosen.
// A cell is in the column the app gave it (aria-colindex), else in the
// one after those the cell before it covers.
func (s *surface) placeCells(row *accessElement, index int, headers []uint64) (chosen bool) {
	col := 1
	for _, cid := range row.children {
		c := s.elements[cid]
		if c == nil || !cell(c.node.Role) {
			continue
		}
		if c.node.ColIndex > 0 {
			col = c.node.ColIndex
		}
		r := index
		if c.node.RowIndex > 0 && index > 0 {
			r = c.node.RowIndex
		}
		c.header = 0
		if c.node.Role != platform.RoleColumnHeader {
			for _, h := range headers {
				if hc := s.elements[h]; hc != nil && hc.at[1] > 0 && hc.at[1] <= col && col < hc.at[1]+hc.at[3] {
					c.header = h
					break
				}
			}
		}
		if at := [4]int{r, col, max(c.node.RowSpan, 1), max(c.node.ColSpan, 1)}; c.at != at {
			if r > 0 && (c.at[0] != r || c.at[2] != at[2]) {
				send(c.obj, "setAccessibilityRowIndexRange:", uintptr(r-1), uintptr(at[2]))
			}
			if c.at[1] != col || c.at[3] != at[3] {
				send(c.obj, "setAccessibilityColumnIndexRange:", uintptr(col-1), uintptr(at[3]))
			}
			c.at = at
		}
		col += c.at[3]
		chosen = chosen || chooses(c.node) && c.node.States&platform.AccessChecked != 0
	}
	return chosen
}

// rowRole reports whether nodes of a role are rows of a list, a table or
// an outline.
func rowRole(r platform.AccessRole) bool {
	return r == platform.RoleRow || r == platform.RoleListItem || r == platform.RoleTreeItem
}

// ariaGrid returns the value of an attribute of a table's size or a row's
// or a cell's place in it that the app gave (aria-rowcount and the like),
// as WebKit gives them, 0 for none: a cell's row index is its row's unless
// it has one of its own.
func (el *accessElement) ariaGrid(attr string) int {
	n := &el.node
	switch attr {
	case "AXARIARowCount":
		if n.Role.Tabular() {
			return n.RowCount
		}
	case "AXARIAColumnCount":
		if n.Role.Tabular() {
			return n.ColCount
		}
	case "AXARIARowIndex":
		switch {
		case n.Role == platform.RoleRow:
			return n.RowIndex
		case cell(n.Role):
			if n.RowIndex > 0 {
				return n.RowIndex
			}
			if row := el.s.w.b.byAccess[el.parent]; row != nil && row.node.Role == platform.RoleRow {
				return row.node.RowIndex
			}
		}
	case "AXARIAColumnIndex":
		if cell(n.Role) {
			return n.ColIndex
		}
	}
	return 0
}

// ariaGridAttrs are the attributes ariaGrid answers.
var ariaGridAttrs = [...]string{"AXARIARowCount", "AXARIAColumnCount", "AXARIARowIndex", "AXARIAColumnIndex"}

func roleOf(n platform.AccessNode) (role, subrole string) {
	r := accessRoles[n.Role]
	role, subrole = r[0], r[1]
	if n.States&platform.AccessSegment != 0 {
		subrole = "AXSegment" // as NSSegmentedControl's
	}
	// The header of a column that sorts the rows, as NSTableHeaderView's
	// are: VoiceOver reads its sort direction.
	if n.Role == platform.RoleColumnHeader && (n.States&sortStates != 0 || n.Actions&platform.ActionPress != 0) {
		role, subrole = "AXButton", "AXSortButton"
	}
	if n.Role == platform.RoleTextField {
		switch {
		case n.States&platform.AccessSearch != 0:
			subrole = "AXSearchField"
		case n.States&platform.AccessPassword != 0:
			subrole = "AXSecureTextField"
		case n.States&platform.AccessMultiline != 0:
			role = "AXTextArea"
		}
	}
	return role, subrole
}

// expands reports whether a node opens and closes what it shows: the
// triggers of popups and disclosures, an item of a tree with children, or
// anything open.
func expands(n platform.AccessNode) bool {
	switch n.Role {
	case platform.RolePopUpButton, platform.RoleComboBox, platform.RoleDisclosure, platform.RoleMenuButton:
		return true
	}
	return n.States&(platform.AccessExpanded|platform.AccessExpandable) != 0
}

// titled reports whether elements of a role show their name as a title.
func titled(r platform.AccessRole) bool {
	switch r {
	case platform.RoleButton, platform.RoleLink, platform.RoleCheckBox, platform.RoleRadio, platform.RoleSwitch, platform.RolePopUpButton, platform.RoleTab,
		platform.RoleMenuButton, platform.RoleToggleButton, platform.RoleMenuItem, platform.RoleMenuItemCheckBox, platform.RoleMenuItemRadio:
		return true
	}
	return false
}

// textual reports whether elements of a role edit text: text fields and
// combo boxes.
func textual(r platform.AccessRole) bool {
	return r == platform.RoleTextField || r == platform.RoleComboBox
}

// toggle reports whether elements of a role have their state as their
// value.
func toggle(r platform.AccessRole) bool {
	switch r {
	case platform.RoleCheckBox, platform.RoleRadio, platform.RoleSwitch, platform.RoleTab, platform.RoleToggleButton:
		return true
	}
	return false
}

// valueText reports whether a node of a range has a text for its value, as
// aria-valuetext, which Core-AAM maps to AXValueDescription beside the
// number.
func valueText(n platform.AccessNode) bool { return n.Role.Ranged() && n.Value != "" }

// valueOf returns the value of a node as AppKit wants it: a number for
// toggles, disclosures and ranges, a string for texts.
func valueOf(n platform.AccessNode) id {
	switch {
	case n.Role == platform.RoleDisclosure:
		// 1 while open, as AppKit's disclosure triangles.
		return nsNumberInt(int(n.States&platform.AccessExpanded) / int(platform.AccessExpanded))
	case toggle(n.Role):
		v := 0
		switch {
		case n.States&platform.AccessMixed != 0:
			v = 2
		case n.States&platform.AccessChecked != 0:
			v = 1
		}
		return nsNumberInt(v)
	case n.Role.Ranged():
		if n.Now < n.Min {
			return 0 // a progress of unknown length
		}
		return msgFloatID(class("NSNumber"), sel("numberWithDouble:"), n.Now)
	case n.Role == platform.RoleText:
		return nsString(n.Label)
	case n.Role == platform.RoleHeading && n.Level > 0:
		return nsNumberInt(n.Level)
	}
	if n.Value != "" || textual(n.Role) {
		return nsString(n.Value)
	}
	return 0
}

// apply sets the properties of a node that changed, all of them for a new
// element, and reports whether its value changed.
func (el *accessElement) apply(n platform.AccessNode, fresh bool) (valueChanged bool) {
	o := el.node
	el.node = n
	obj := el.obj
	r, sr := roleOf(n)
	if or, osr := roleOf(o); fresh || r != or || sr != osr {
		send(obj, "setAccessibilityRole:", uintptr(nsString(r)))
		var sub id
		if sr != "" {
			sub = nsString(sr)
		}
		send(obj, "setAccessibilitySubrole:", uintptr(sub))
	}
	if fresh || n.Label != o.Label || n.Role != o.Role {
		// Controls show their name, which AppKit's own give as their
		// title; other elements are described by it.
		if titled(n.Role) {
			send(obj, "setAccessibilityTitle:", uintptr(nsString(n.Label)))
			send(obj, "setAccessibilityLabel:", 0)
		} else {
			send(obj, "setAccessibilityTitle:", 0)
			send(obj, "setAccessibilityLabel:", uintptr(nsString(n.Label)))
		}
	}
	if fresh || n.States&platform.AccessDisabled != o.States&platform.AccessDisabled {
		send(obj, "setAccessibilityEnabled:", boolArg(n.States&platform.AccessDisabled == 0))
	}
	// Only what opens says whether it is open, and what was open says it
	// closed: AppKit reports AXExpanded once it is set, and VoiceOver
	// then reads "collapsed" on lists and rows that never open.
	if was := !fresh && expands(o); expands(n) || was {
		if !was || n.States&platform.AccessExpanded != o.States&platform.AccessExpanded {
			send(obj, "setAccessibilityExpanded:", boolArg(n.States&platform.AccessExpanded != 0))
		}
	}
	if orient := orientation(n); orient != 0 && fresh || !fresh && orient != orientation(o) {
		send(obj, "setAccessibilityOrientation:", uintptr(orient))
	}
	if n.Role.Ranged() {
		if fresh || n.Min != o.Min || n.Max != o.Max {
			send(obj, "setAccessibilityMinValue:", uintptr(msgFloatID(class("NSNumber"), sel("numberWithDouble:"), n.Min)))
			send(obj, "setAccessibilityMaxValue:", uintptr(msgFloatID(class("NSNumber"), sel("numberWithDouble:"), n.Max)))
		}
	}
	// The state of a toggle is its value; a row's choice is not, which its
	// table tells (AXSelectedRowsChanged).
	toggled := toggle(n.Role) && n.States&(platform.AccessChecked|platform.AccessMixed) != o.States&(platform.AccessChecked|platform.AccessMixed) ||
		n.Role == platform.RoleDisclosure && n.States&platform.AccessExpanded != o.States&platform.AccessExpanded
	if fresh || n.Value != o.Value || n.Now != o.Now || toggled || n.Role == platform.RoleText && n.Label != o.Label ||
		n.Role == platform.RoleHeading && n.Level != o.Level {
		send(obj, "setAccessibilityValue:", uintptr(valueOf(n)))
		valueChanged = !fresh
	}
	if n.Level > 0 && n.Role != platform.RoleHeading && (fresh || n.Level != o.Level) {
		// How deep the row of an outline is, from 0.
		send(obj, "setAccessibilityDisclosureLevel:", uintptr(n.Level-1))
	}
	if n.Role == platform.RoleTreeItem && (fresh || n.States&platform.AccessExpanded != o.States&platform.AccessExpanded) {
		send(obj, "setAccessibilityDisclosed:", boolArg(n.States&platform.AccessExpanded != 0))
	}
	if n.PosInSet > 0 && (fresh || n.PosInSet != o.PosInSet) {
		send(obj, "setAccessibilityIndex:", uintptr(n.PosInSet-1))
	} else if n.PosInSet == 0 && n.RowIndex > 0 && rowRole(n.Role) && (fresh || n.RowIndex != o.RowIndex || o.PosInSet != 0) {
		// The row of a grid built only where it shows, which says which
		// of all it is (aria-rowindex).
		send(obj, "setAccessibilityIndex:", uintptr(n.RowIndex-1))
	}
	if chooses(n) && (fresh || !chooses(o) || n.States&platform.AccessChecked != o.States&platform.AccessChecked) {
		send(obj, "setAccessibilitySelected:", boolArg(n.States&platform.AccessChecked != 0))
	}
	if fresh || n.Placeholder != o.Placeholder {
		var p id
		if n.Placeholder != "" {
			p = nsString(n.Placeholder)
		}
		send(obj, "setAccessibilityPlaceholderValue:", uintptr(p))
	}
	if (n.Role == platform.RoleColumnHeader || n.Role == platform.RoleRowHeader) && (fresh || n.States&sortStates != o.States&sortStates) {
		// NSAccessibilitySortDirection, Unknown for another order.
		dir := 0
		switch {
		case n.States&platform.AccessSortAscending != 0:
			dir = 1
		case n.States&platform.AccessSortDescending != 0:
			dir = 2
		}
		send(obj, "setAccessibilitySortDirection:", uintptr(dir))
	}
	if fresh || n.Description != o.Description {
		// AppKit's help, which it gives tooltips as.
		var d id
		if n.Description != "" {
			d = nsString(n.Description)
		}
		send(obj, "setAccessibilityHelp:", uintptr(d))
	}
	if textual(n.Role) && (fresh || n.Value != o.Value || n.SelStart != o.SelStart || n.SelEnd != o.SelEnd) {
		text := []rune(n.Value)
		a, b := max(0, min(n.SelStart, len(text))), max(0, min(n.SelEnd, len(text)))
		send(obj, "setAccessibilityNumberOfCharacters:", uintptr(units(text)))
		send(obj, "setAccessibilitySelectedTextRange:", uintptr(units(text[:a])), uintptr(units(text[a:max(a, b)])))
	}
	return valueChanged
}

// accessRequested starts the content describing its frames, the first
// time AppKit asks about the surface.
func (s *surface) accessRequested() {
	if !s.accessOn {
		s.accessOn = true
		s.send(platform.SurfaceEvent{Kind: platform.AccessibilityOn})
	}
}

func (s *surface) UpdateAccessibility(tree *platform.AccessTree) {
	if tree == nil || s.w.closed {
		return
	}
	loadAccess()
	withPool(func() {
		s.updating = true
		defer func() { s.updating = false }()
		if s.elements == nil {
			s.elements = map[uint64]*accessElement{}
		}
		changed := false
		objs := make([]id, len(tree.Nodes))
		var valueChanged, busyChanged []id
		fresh := map[uint64]bool{}
		for i, n := range tree.Nodes {
			// VoiceOver reads no chosen state of a cell (AXSelected), nor
			// its change: a chosen cell says it after its name, as
			// VoiceOver's own "selected" follows a row's.
			if tree.Selected != "" && cell(n.Role) && chooses(n) && n.States&platform.AccessChecked != 0 {
				if n.Label == "" {
					n.Label = tree.Selected
				} else {
					n.Label += ", " + tree.Selected
				}
			}
			el := s.elements[n.ID]
			if el == nil {
				fresh[n.ID] = true
				el = &accessElement{obj: send(send(class("MyGoAccessibilityElement"), "alloc"), "init"), s: s}
				s.elements[n.ID] = el
				s.w.b.byAccess[el.obj] = el
				el.apply(n, true)
				changed = true
			} else {
				wasBusy := el.node.States & platform.AccessBusy
				if el.apply(n, false) {
					valueChanged = append(valueChanged, el.obj)
				}
				if n.States&platform.AccessBusy != wasBusy {
					busyChanged = append(busyChanged, el.obj)
				}
			}
			objs[i] = el.obj
		}
		children := make([][]uint64, len(tree.Nodes))
		childObjs := make([][]id, len(tree.Nodes))
		var top []id
		for i, n := range tree.Nodes {
			if n.Parent < 0 || n.Parent >= len(tree.Nodes) {
				top = append(top, objs[i])
				continue
			}
			children[n.Parent] = append(children[n.Parent], n.ID)
			childObjs[n.Parent] = append(childObjs[n.Parent], objs[i])
		}
		grafted := map[*window]bool{}
		for i, n := range tree.Nodes {
			el := s.elements[n.ID]
			parent := s.view
			if n.Parent >= 0 && n.Parent < len(tree.Nodes) {
				parent = objs[n.Parent]
			}
			if el.parent != parent {
				el.parent = parent
				send(el.obj, "setAccessibilityParent:", uintptr(parent))
			}
			// The page of a web view is inside the element showing it.
			var web *window
			if v, ok := n.WebView.(*window); ok && v.host == s.w && !v.closed && !grafted[v] {
				web, grafted[v] = v, true
				v.accessParent = el.obj
			}
			if !slices.Equal(el.children, children[i]) || el.webView != web {
				el.children, el.webView = children[i], web
				kids := childObjs[i]
				if web != nil {
					kids = append([]id{web.web}, kids...)
				}
				send(el.obj, "setAccessibilityChildren:", uintptr(nsArray(kids...)))
				changed = true
			}
		}
		for _, v := range s.w.webViews {
			if !grafted[v] {
				v.accessParent = 0
			}
		}
		// Lists, tables and grids: their rows, those that show, those
		// chosen, and how many they have, built or not. A list's own rows
		// say where they are among all, as do those of a grid the app
		// says (aria-rowindex); the rows of other tables, where among
		// those.
		var selectionChanged, cellsChanged []*accessElement
		var chosenNews []string
		for i, n := range tree.Nodes {
			if n.Role != platform.RoleList && n.Role != platform.RoleTree && !n.Role.Tabular() {
				continue
			}
			el := s.elements[n.ID]
			// The column headers, in the first row that has them, which
			// VoiceOver reads with the cells of their columns.
			var headers []uint64
			for _, rid := range children[i] {
				for _, cid := range s.elements[rid].children {
					if s.elements[cid].node.Role == platform.RoleColumnHeader {
						headers = append(headers, cid)
					}
				}
				if headers != nil {
					break
				}
			}
			el.headers = headers
			var rows, shown, chosen []id
			var chosenIDs []uint64
			itemsSet := 0
			for _, cid := range children[i] {
				c := s.elements[cid]
				if !rowRole(c.node.Role) || n.SetSize > 0 && c.node.PosInSet == 0 && c.node.RowIndex == 0 {
					if c.node.Role == platform.RoleRow {
						s.placeCells(c, 0, nil)
					}
					continue // a table's header
				}
				index := c.node.PosInSet - 1
				switch {
				case c.node.PosInSet > 0:
				case c.node.RowIndex > 0:
					index = c.node.RowIndex - 1 // set with the row (aria-rowindex)
				default:
					index = len(rows)
					send(c.obj, "setAccessibilityIndex:", uintptr(index))
				}
				rows = append(rows, c.obj)
				if n.Role == platform.RoleList {
					// The set the items of a list of the app's own say
					// they are in (aria-setsize).
					itemsSet = max(itemsSet, c.node.SetSize)
				}
				if c.node.States&platform.AccessOffscreen == 0 {
					shown = append(shown, c.obj)
				}
				// A row holding a chosen cell is chosen, as NSTableView
				// tells the cells chosen by their rows: VoiceOver reads a
				// table's choice by them.
				byCells := c.node.Role == platform.RoleRow && s.placeCells(c, index+1, headers) && !chooses(c.node)
				if byCells != c.cellRow {
					c.cellRow = byCells
					send(c.obj, "setAccessibilitySelected:", boolArg(byCells || chooses(c.node) && c.node.States&platform.AccessChecked != 0))
				}
				if chooses(c.node) && c.node.States&platform.AccessChecked != 0 || byCells {
					chosen = append(chosen, c.obj)
					chosenIDs = append(chosenIDs, cid)
				}
			}
			count := n.SetSize
			if count == 0 {
				count = n.RowCount
			}
			if count == 0 {
				count = max(itemsSet, len(rows))
			}
			if count != el.count {
				el.count = count
				send(el.obj, "setAccessibilityRowCount:", uintptr(count))
			}
			send(el.obj, "setAccessibilityRows:", uintptr(nsArray(rows...)))
			send(el.obj, "setAccessibilityVisibleRows:", uintptr(nsArray(shown...)))
			send(el.obj, "setAccessibilitySelectedRows:", uintptr(nsArray(chosen...)))
			if !slices.Equal(el.chosen, chosenIDs) {
				el.chosen = chosenIDs
				if !fresh[n.ID] { // a new list's choice did not change
					selectionChanged = append(selectionChanged, el)
				}
			}
			// The cells chosen, as a grid's selected gridcells, in its rows.
			var cells []id
			var cellIDs []uint64
			for _, rid := range children[i] {
				for _, cid := range s.elements[rid].children {
					if c := s.elements[cid]; cell(c.node.Role) && chooses(c.node) && c.node.States&platform.AccessChecked != 0 {
						cells, cellIDs = append(cells, c.obj), append(cellIDs, cid)
					}
				}
			}
			if !slices.Equal(el.chosenCells, cellIDs) {
				if !fresh[n.ID] {
					cellsChanged = append(cellsChanged, el)
					// The cells whose choice changed tell it too, as
					// WebKit's gridcells do; VoiceOver reads it of
					// neither, so a cell chosen, not new, where the focus
					// is not is read out, as the focus would read it.
					for _, cid := range cellIDs {
						if !slices.Contains(el.chosenCells, cid) {
							cellsChanged = append(cellsChanged, s.elements[cid])
							if cid != tree.Focus && !fresh[cid] && tree.Selected != "" {
								chosenNews = append(chosenNews, s.elements[cid].node.Label)
							}
						}
					}
					for _, cid := range el.chosenCells {
						if c := s.elements[cid]; c != nil && !slices.Contains(cellIDs, cid) {
							cellsChanged = append(cellsChanged, c)
						}
					}
				}
				el.chosenCells = cellIDs
				send(el.obj, "setAccessibilitySelectedCells:", uintptr(nsArray(cells...)))
			}
		}
		release(s.topLevel)
		s.topLevel = retain(nsArray(top...))
		live := make(map[uint64]bool, len(tree.Nodes))
		for _, n := range tree.Nodes {
			live[n.ID] = true
		}
		for nid, el := range s.elements {
			if !live[nid] {
				postNote(el.obj, "AXUIElementDestroyed")
				delete(s.w.b.byAccess, el.obj)
				release(el.obj)
				delete(s.elements, nid)
				changed = true
			}
		}
		// What the elements control (aria-controls), among those left.
		for _, n := range tree.Nodes {
			el := s.elements[n.ID]
			if len(n.Controls) == 0 && el.linked == nil {
				continue
			}
			var linked []id
			for _, cid := range n.Controls {
				if c := s.elements[cid]; c != nil {
					linked = append(linked, c.obj)
				}
			}
			if !slices.Equal(el.linked, linked) {
				el.linked = linked
				send(el.obj, "setAccessibilityLinkedUIElements:", uintptr(nsArray(linked...)))
			}
		}
		if changed {
			postNote(s.view, "AXLayoutChanged")
		}
		for _, obj := range valueChanged {
			postNote(obj, "AXValueChanged")
		}
		for _, el := range selectionChanged {
			postNote(el.obj, "AXSelectedRowsChanged")
		}
		for _, el := range cellsChanged {
			postNote(el.obj, "AXSelectedCellsChanged")
		}
		for _, obj := range busyChanged {
			postNote(obj, "AXElementBusyChanged")
		}
		if tree.Focus != s.access.Focus {
			if el := s.elements[tree.Focus]; el != nil {
				postNote(el.obj, "AXFocusedUIElementChanged")
			}
		}
		for _, text := range slices.Concat(chosenNews, tree.Announcements) {
			announce(s.w.b.app, text, false)
		}
		for _, text := range tree.Alerts {
			announce(s.w.b.app, text, true)
		}
		s.access = *tree
		s.access.Announcements, s.access.Alerts = nil, nil
	})
}

// destroyAccess releases the elements of a surface.
func (s *surface) destroyAccess() {
	for _, v := range s.w.webViews {
		v.accessParent = 0
	}
	for nid, el := range s.elements {
		postNote(el.obj, "AXUIElementDestroyed")
		delete(s.w.b.byAccess, el.obj)
		release(el.obj)
		delete(s.elements, nid)
	}
	release(s.topLevel)
	s.topLevel = 0
}

// accessAt returns the innermost element at a point of the view.
func (s *surface) accessAt(x, y float64) id {
	found := id(0)
	for _, n := range s.access.Nodes {
		b := n.Bounds
		if x >= b.X && y >= b.Y && x < b.X+b.W && y < b.Y+b.H {
			if el := s.elements[n.ID]; el != nil {
				found = el.obj // later nodes are deeper or above
			}
		}
	}
	return found
}

// screenRect converts a rectangle of the view to the screen.
func (s *surface) screenRect(b platform.RectF) NSRect {
	inWindow := msgConvertRectView(s.view, sel("convertRect:toView:"), NSRect{Origin: NSPoint{b.X, b.Y}, Size: NSSize{b.W, b.H}}, 0)
	return msgRectToRect(s.w.win, sel("convertRectToScreen:"), inWindow)
}

func (b *Backend) accessElementOf(obj id) *accessElement {
	el := b.byAccess[obj]
	if el == nil || el.s.w.closed {
		return nil
	}
	return el
}

// act sends an action of assistive technology to the content.
func (el *accessElement) act(a platform.AccessActionKind, text string) bool {
	if el.s.updating {
		return false
	}
	el.s.send(platform.SurfaceEvent{Kind: platform.AccessAction, ID: el.node.ID, Action: a, Text: text})
	return true
}

// legacyActions are the actions of nodes by their names in AppKit's older
// API: AXScrollToVisible is NSAccessibilityScrollToVisibleAction, which
// AppKit exports only since macOS 26.
var legacyActions = []struct {
	name   string
	action platform.AccessActions
	kind   platform.AccessActionKind
}{
	{"AXPress", platform.ActionPress, platform.AccessPress},
	{"AXIncrement", platform.ActionIncrement, platform.AccessIncrement},
	{"AXDecrement", platform.ActionDecrement, platform.AccessDecrement},
	{"AXScrollToVisible", platform.ActionScrollIntoView, platform.AccessScrollIntoView},
}

func registerAccessClass() {
	b := func() *Backend { return theBackend }
	allowed := map[string]platform.AccessActions{
		"accessibilityPerformPress":     platform.ActionPress,
		"accessibilityPerformIncrement": platform.ActionIncrement,
		"accessibilityPerformDecrement": platform.ActionDecrement,
		"setAccessibilityValue:":        platform.ActionSetValue,
		"setAccessibilitySelectedText:": platform.ActionSetValue,
		"setAccessibilityFocused:":      platform.ActionFocus,
		"setAccessibilityDisclosed:":    platform.ActionExpand,
	}
	classDef("MyGoAccessibilityElement", "NSAccessibilityElement", nil, []objc.MethodDef{
		method("accessibilityFrame", func(self id, _ objc.SEL) NSRect {
			if el := b().accessElementOf(self); el != nil {
				return el.s.screenRect(el.node.Bounds)
			}
			return NSRect{}
		}),
		// What the app calls the element's role, as WebKit says
		// aria-roledescription, else AppKit's name of it.
		method("accessibilityRoleDescription", func(self id, cmd objc.SEL) id {
			if el := b().accessElementOf(self); el != nil && el.node.RoleDescription != "" {
				return nsString(el.node.RoleDescription)
			}
			return sendSuper(self, "MyGoAccessibilityElement", cmd)
		}),
		method("isAccessibilityFocused", func(self id, _ objc.SEL) bool {
			el := b().accessElementOf(self)
			return el != nil && el.s.access.Focus == el.node.ID
		}),
		// AXInvalid, which the newer API lacks, says a value is not valid,
		// as WebKit's fields do, AXMenuItemMarkChar what an item of a
		// menu shows of its choice, AXElementBusy that the content is
		// about to change, AXARIACurrent which item of those around it the
		// element is the current one of, and AXAutocompleteValue what a
		// text field's suggestions complete, as WebKit's say aria-busy,
		// aria-current and aria-autocomplete.
		method("accessibilityAttributeNames", func(self id, cmd objc.SEL) id {
			names := sendSuper(self, "MyGoAccessibilityElement", cmd)
			el := b().accessElementOf(self)
			if el == nil {
				return names
			}
			if el.node.States&platform.AccessInvalid != 0 {
				names = send(names, "arrayByAddingObject:", uintptr(nsString("AXInvalid")))
			}
			// The elements holding its error, as WebKit's say
			// aria-errormessage.
			if errorMessages(el.node) {
				names = send(names, "arrayByAddingObject:", uintptr(nsString("AXErrorMessageElements")))
			}
			if menuItem(el.node.Role) {
				names = send(names, "arrayByAddingObject:", uintptr(nsString("AXMenuItemMarkChar")))
			}
			// The kind of popup the element opens, as WebKit's say
			// aria-haspopup.
			if el.node.HasPopup != platform.PopupNone {
				names = send(names, "arrayByAddingObject:", uintptr(nsString("AXPopupValue")))
			}
			names = send(names, "arrayByAddingObject:", uintptr(nsString("AXElementBusy")))
			// Whether it can be used: the older API does not list or answer
			// AXEnabled for what setAccessibilityEnabled: set, and VoiceOver
			// would read every disabled control as available.
			names = send(names, "arrayByAddingObject:", uintptr(nsString("AXEnabled")))
			// Where a cell is and whether it is chosen, and the cells a
			// table has chosen, which AppKit's names of an AXCell and an
			// AXTable leave out though their setters are called.
			var more []string
			switch r := el.node.Role; {
			case cell(r):
				more = []string{"AXRowIndexRange", "AXColumnIndexRange"}
				if chooses(el.node) {
					more = append(more, "AXSelected")
				}
				if el.header != 0 {
					more = append(more, "AXColumnHeaderUIElements")
				}
				if r != platform.RoleCell {
					more = append(more, "AXSortDirection") // as WebKit's headers
				}
			case r.Tabular():
				more = []string{"AXSelectedRows", "AXSelectedCells"}
				if el.headers != nil {
					more = append(more, "AXColumnHeaderUIElements")
				}
			}
			if el.node.RoleDescription != "" {
				more = append(more, "AXRoleDescription")
			}
			// What describes it (Description, DescribedBy, ErrorMessage),
			// which the older API neither lists nor answers for what
			// setAccessibilityHelp: set, as it does AXEnabled.
			if el.node.Description != "" {
				more = append(more, "AXHelp")
			}
			for _, name := range more {
				if a := nsString(name); !sendBool(names, "containsObject:", uintptr(a)) {
					names = send(names, "arrayByAddingObject:", uintptr(a))
				}
			}
			if el.node.Current != platform.CurrentNone {
				names = send(names, "arrayByAddingObject:", uintptr(nsString("AXARIACurrent")))
			}
			if el.node.AutoComplete != platform.AutoCompleteNone {
				names = send(names, "arrayByAddingObject:", uintptr(nsString("AXAutocompleteValue")))
			}
			// How a live region is read, as WebKit's say aria-live,
			// aria-atomic and aria-relevant.
			if live, _ := el.node.Role.Live(); live != "" {
				for _, name := range []string{"AXARIALive", "AXARIAAtomic", "AXARIARelevant"} {
					names = send(names, "arrayByAddingObject:", uintptr(nsString(name)))
				}
			}
			if valueText(el.node) {
				names = send(names, "arrayByAddingObject:", uintptr(nsString("AXValueDescription")))
			}
			// The keys that press or focus it, as WebKit's elements say
			// aria-keyshortcuts.
			if el.node.KeyShortcuts != "" {
				names = send(names, "arrayByAddingObject:", uintptr(nsString("AXKeyShortcutsValue")))
			}
			// The size of a grid, and the place of its rows and cells, as
			// WebKit's say aria-rowcount and the like.
			for _, attr := range ariaGridAttrs {
				if el.ariaGrid(attr) > 0 {
					names = send(names, "arrayByAddingObject:", uintptr(nsString(attr)))
				}
			}
			return names
		}),
		method("accessibilityAttributeValue:", func(self id, cmd objc.SEL, attr id) id {
			if el := b().accessElementOf(self); el != nil {
				switch stringOf(attr) {
				case "AXInvalid":
					if el.node.States&platform.AccessInvalid != 0 {
						return nsString("true")
					}
				case "AXErrorMessageElements":
					if errorMessages(el.node) {
						objs := make([]id, 0, len(el.node.ErrorMessage))
						for _, mid := range el.node.ErrorMessage {
							if m := el.s.elements[mid]; m != nil {
								objs = append(objs, m.obj)
							}
						}
						return nsArray(objs...)
					}
				case "AXMenuItemMarkChar":
					if m := markChar(el.node); m != "" {
						return nsString(m)
					}
					if menuItem(el.node.Role) {
						return 0
					}
				case "AXPopupValue":
					if p := el.node.HasPopup; p != platform.PopupNone {
						return nsString(p.String())
					}
				case "AXElementBusy":
					return nsBool(el.node.States&platform.AccessBusy != 0)
				case "AXEnabled":
					return nsBool(el.node.States&platform.AccessDisabled == 0)
				// What describes it, as the newer API's accessibilityHelp:
				// its error first while invalid, then its description, a
				// line each.
				case "AXHelp":
					if el.node.Description != "" {
						return nsString(el.node.Description)
					}
					return 0
				// The role and subrole the newer API was given, so that
				// both APIs agree: AppKit's answer here missed a header
				// made a sort button, and a grid's table.
				case "AXRole", "AXSubrole":
					role, subrole := roleOf(el.node)
					if stringOf(attr) == "AXRole" {
						return nsString(role)
					}
					if subrole != "" {
						return nsString(subrole)
					}
					return 0
				// The role description the app gives, which AppKit's
				// answer here leaves out though accessibilityRoleDescription
				// gives it.
				case "AXRoleDescription":
					if el.node.RoleDescription != "" {
						return nsString(el.node.RoleDescription)
					}
				case "AXSelected":
					if cell(el.node.Role) && chooses(el.node) {
						return nsBool(el.node.States&platform.AccessChecked != 0)
					}
					if el.cellRow {
						return nsBool(true)
					}
				case "AXRowIndexRange", "AXColumnIndexRange":
					i, n := el.at[0], el.at[2]
					if stringOf(attr) == "AXColumnIndexRange" {
						i, n = el.at[1], el.at[3]
					}
					if cell(el.node.Role) && i > 0 {
						return send(class("NSValue"), "valueWithRange:", uintptr(i-1), uintptr(n))
					}
				case "AXSelectedCells", "AXSelectedRows", "AXColumnHeaderUIElements":
					var ids []uint64
					switch {
					case el.node.Role.Tabular() && stringOf(attr) == "AXSelectedCells":
						ids = el.chosenCells
					case el.node.Role.Tabular() && stringOf(attr) == "AXSelectedRows":
						ids = el.chosen
					case el.node.Role.Tabular():
						ids = el.headers
					case cell(el.node.Role) && el.header != 0:
						ids = []uint64{el.header}
					default:
						return sendSuper(self, "MyGoAccessibilityElement", cmd, uintptr(attr))
					}
					objs := make([]id, 0, len(ids))
					for _, cid := range ids {
						if c := el.s.elements[cid]; c != nil {
							objs = append(objs, c.obj)
						}
					}
					return nsArray(objs...)
				case "AXSortDirection":
					if r := el.node.Role; r == platform.RoleColumnHeader || r == platform.RoleRowHeader {
						switch {
						case el.node.States&platform.AccessSortAscending != 0:
							return nsString("AXAscendingSortDirection")
						case el.node.States&platform.AccessSortDescending != 0:
							return nsString("AXDescendingSortDirection")
						}
						return nsString("AXUnknownSortDirection")
					}
				case "AXARIACurrent":
					if c := el.node.Current; c != platform.CurrentNone {
						return nsString(c.String())
					}
				case "AXAutocompleteValue":
					if a := el.node.AutoComplete; a != platform.AutoCompleteNone {
						return nsString(a.String())
					}
				case "AXARIALive", "AXARIAAtomic", "AXARIARelevant":
					if live, atomic := el.node.Role.Live(); live != "" {
						switch stringOf(attr) {
						case "AXARIALive":
							return nsString(live)
						case "AXARIAAtomic":
							return nsBool(atomic)
						}
						return nsString("additions text") // ARIA's default
					}
				case "AXValueDescription":
					if valueText(el.node) {
						return nsString(el.node.Value)
					}
				case "AXKeyShortcutsValue":
					if el.node.KeyShortcuts != "" {
						return nsString(el.node.KeyShortcuts)
					}
				case "AXARIARowCount", "AXARIAColumnCount", "AXARIARowIndex", "AXARIAColumnIndex":
					if v := el.ariaGrid(stringOf(attr)); v > 0 {
						return nsNumberInt(v)
					}
				}
			}
			return sendSuper(self, "MyGoAccessibilityElement", cmd, uintptr(attr))
		}),
		// The older API asks which attributes are settable, which
		// NSAccessibilityElement does not answer (calling super throws):
		// AppKit's answer without it is the attributes of the setters the
		// class overrides, AXValue, AXFocused and a row's AXDisclosing,
		// kept but for the value of a read-only text field, which is not
		// settable, as WebKit's.
		method("accessibilityIsAttributeSettable:", func(self id, _ objc.SEL, attr id) bool {
			el := b().accessElementOf(self)
			if el == nil {
				return false
			}
			switch stringOf(attr) {
			case "AXValue":
				return !textual(el.node.Role) || el.node.Actions&platform.ActionSetValue != 0
			case "AXSelectedText":
				return textual(el.node.Role) && el.node.Actions&platform.ActionSetValue != 0
			case "AXFocused":
				return true
			case "AXDisclosing":
				return el.node.Role == platform.RoleTreeItem
			}
			return false
		}),
		method("isAccessibilitySelectorAllowed:", func(self id, cmd objc.SEL, selector objc.SEL) bool {
			if el := b().accessElementOf(self); el != nil {
				if selector == sel("accessibilityPerformShowMenu") {
					return showsMenu(el.node)
				}
				for name, action := range allowed {
					if sel(name) == selector {
						return el.node.Actions&action != 0
					}
				}
			}
			return byte(sendSuper(self, "MyGoAccessibilityElement", cmd, uintptr(selector))) != 0
		}),
		// Scrolling into view is an action of the older API alone, which
		// VoiceOver asks for as it moves to what is out of view: answering
		// it, an element answers for all its actions there.
		method("accessibilityActionNames", func(self id, _ objc.SEL) id {
			var names []id
			if el := b().accessElementOf(self); el != nil {
				for _, a := range legacyActions {
					if el.node.Actions&a.action != 0 {
						names = append(names, nsString(a.name))
					}
				}
				if showsMenu(el.node) {
					names = append(names, nsString("AXShowMenu"))
				}
			}
			return nsArray(names...)
		}),
		method("accessibilityActionDescription:", func(self id, _ objc.SEL, action id) id {
			r, _, _ := purego.SyscallN(actionDescription, uintptr(action))
			return id(r)
		}),
		method("accessibilityPerformAction:", func(self id, _ objc.SEL, action id) {
			el := b().accessElementOf(self)
			if el == nil {
				return
			}
			name := stringOf(action)
			for _, a := range legacyActions {
				if a.name == name && el.node.Actions&a.action != 0 {
					el.act(a.kind, "")
				}
			}
			if name == "AXShowMenu" && showsMenu(el.node) {
				el.act(platform.AccessPress, "")
			}
		}),
		method("accessibilityPerformPress", func(self id, _ objc.SEL) bool {
			el := b().accessElementOf(self)
			return el != nil && el.act(platform.AccessPress, "")
		}),
		// Opening the popup of an element having one (aria-haspopup),
		// which pressing it does.
		method("accessibilityPerformShowMenu", func(self id, _ objc.SEL) bool {
			el := b().accessElementOf(self)
			return el != nil && showsMenu(el.node) && el.act(platform.AccessPress, "")
		}),
		method("accessibilityPerformIncrement", func(self id, _ objc.SEL) bool {
			el := b().accessElementOf(self)
			return el != nil && el.act(platform.AccessIncrement, "")
		}),
		method("accessibilityPerformDecrement", func(self id, _ objc.SEL) bool {
			el := b().accessElementOf(self)
			return el != nil && el.act(platform.AccessDecrement, "")
		}),
		// Opening and closing a row of an outline, as VoiceOver does.
		method("setAccessibilityDisclosed:", func(self id, cmd objc.SEL, open bool) {
			sendSuper(self, "MyGoAccessibilityElement", cmd, boolArg(open))
			if el := b().accessElementOf(self); el != nil && el.node.Actions&platform.ActionExpand != 0 {
				kind := platform.AccessCollapse
				if open {
					kind = platform.AccessExpand
				}
				el.act(kind, "")
			}
		}),
		method("setAccessibilityFocused:", func(self id, _ objc.SEL, focused bool) {
			if el := b().accessElementOf(self); el != nil && focused {
				el.act(platform.AccessFocus, "")
			}
		}),
		method("setAccessibilityValue:", func(self id, cmd objc.SEL, value id) {
			sendSuper(self, "MyGoAccessibilityElement", cmd, uintptr(value))
			if el := b().accessElementOf(self); el != nil && textual(el.node.Role) {
				el.act(platform.AccessSetValue, stringOf(value))
			}
		}),
		// Typing into a text field over its selection, as Voice Control,
		// dictation and automation insert text (kAXSelectedTextAttribute).
		method("setAccessibilitySelectedText:", func(self id, cmd objc.SEL, text id) {
			sendSuper(self, "MyGoAccessibilityElement", cmd, uintptr(text))
			if el := b().accessElementOf(self); el != nil && textual(el.node.Role) && el.node.Actions&platform.ActionSetValue != 0 {
				el.act(platform.AccessInsertText, stringOf(text))
			}
		}),
		// Moving the selection of a text field, as VoiceOver does.
		method("setAccessibilitySelectedTextRange:", func(self id, cmd objc.SEL, r nsRange) {
			sendSuper(self, "MyGoAccessibilityElement", cmd, uintptr(r.Location), uintptr(r.Length))
			el := b().accessElementOf(self)
			if el == nil || !textual(el.node.Role) || el.s.updating {
				return
			}
			u := utf16.Encode([]rune(el.node.Value))
			lo := int(min(r.Location, uint(len(u))))
			hi := int(min(uint(lo)+r.Length, uint(len(u))))
			start := len(utf16.Decode(u[:lo]))
			end := start + len(utf16.Decode(u[lo:hi]))
			el.s.send(platform.SurfaceEvent{Kind: platform.AccessAction, ID: el.node.ID, Action: platform.AccessSelectText, From: start, To: end})
		}),
		// The text of text fields, for reading by characters and lines.
		method("accessibilityStringForRange:", func(self id, _ objc.SEL, r nsRange) id {
			el := b().accessElementOf(self)
			if el == nil {
				return 0
			}
			u := utf16.Encode([]rune(el.node.Value))
			lo := int(min(r.Location, uint(len(u))))
			hi := int(min(uint(lo)+r.Length, uint(len(u))))
			return nsString(string(utf16.Decode(u[lo:hi])))
		}),
		// The formats of a rich text's runs, as NSTextView gives them, in
		// accessibility's own keys: VoiceOver reads them as bold, italic,
		// links.
		method("accessibilityAttributedStringForRange:", func(self id, _ objc.SEL, r nsRange) id {
			el := b().accessElementOf(self)
			if el == nil {
				return 0
			}
			return attributedAccessString(&el.node, r)
		}),
		method("accessibilityLineForIndex:", func(self id, _ objc.SEL, i int) int { return 0 }),
		method("accessibilityRangeForLine:", func(self id, _ objc.SEL, line int) nsRange {
			if el := b().accessElementOf(self); el != nil {
				return nsRange{Length: uint(units([]rune(el.node.Value)))}
			}
			return nsRange{}
		}),
		method("accessibilityFrameForRange:", func(self id, _ objc.SEL, r nsRange) NSRect {
			if el := b().accessElementOf(self); el != nil {
				return el.s.screenRect(el.node.Bounds)
			}
			return NSRect{}
		}),
	})
}

// accessViewMethods are the methods of the surface view that make it the
// container of the elements.
func accessViewMethods() []objc.MethodDef {
	b := func() *Backend { return theBackend }
	return []objc.MethodDef{
		method("isAccessibilityElement", func(self id, _ objc.SEL) bool { return false }),
		method("accessibilityChildren", func(self id, _ objc.SEL) id {
			s := b().surfaceOf(self)
			if s == nil {
				return 0
			}
			s.accessRequested()
			if s.topLevel == 0 {
				return nsArray()
			}
			return s.topLevel
		}),
		method("accessibilityHitTest:", func(self id, _ objc.SEL, p NSPoint) id {
			s := b().surfaceOf(self)
			if s == nil {
				return self
			}
			loadAccess()
			s.accessRequested()
			inWindow := msgPointToPoint(s.w.win, sel("convertPointFromScreen:"), p)
			v := msgPointFromView(self, sel("convertPoint:fromView:"), inWindow, 0)
			if obj := s.accessAt(v.X, v.Y); obj != 0 {
				return obj
			}
			return self
		}),
		method("accessibilityFocusedUIElement", func(self id, _ objc.SEL) id {
			s := b().surfaceOf(self)
			if s == nil {
				return self
			}
			s.accessRequested()
			if el := s.elements[s.access.Focus]; el != nil {
				return el.obj
			}
			return self
		}),
	}
}
