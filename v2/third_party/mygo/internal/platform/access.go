package platform

// AccessTree is the content of a surface for assistive technology, such as
// screen readers: the elements a user can perceive and act on.
type AccessTree struct {
	// Nodes are the elements in tree order: a parent before its children.
	Nodes []AccessNode
	// Focus is the ID of the element with the keyboard focus, 0 for none.
	Focus uint64
	// Announcements are texts for assistive technology to read out once,
	// after what it is reading, as the title of a page shown or a toast:
	// news the focus does not bring.
	Announcements []string
	// Alerts are texts to read out at once, interrupting what is being
	// read: what an alert (RoleAlert) shows as it appears or changes, as
	// ARIA's assertive live regions.
	Alerts []string
	// Selected is the word for a chosen cell of a grid in the window's
	// language ("selected"), given while a cell says whether it is
	// chosen, for a bridge whose screen reader reads no chosen state of
	// cells to say with its name.
	Selected string
}

// AccessNode is an element of an AccessTree.
type AccessNode struct {
	// ID identifies the element from frame to frame.
	ID uint64
	// Parent is the index of the parent node in AccessTree.Nodes, -1 for
	// the elements at the top.
	Parent int
	Role   AccessRole
	// Label names the element; Value is its value, as the text of a text
	// field, for elements that have one.
	Label, Value string
	// Bounds is the element's visible box, in DIPs relative to the
	// surface.
	Bounds RectF
	States AccessStates
	// Min, Max and Now are the range and the value of a slider or a
	// progress bar; Now is below Min for progress of unknown length. Step
	// is how far an increment moves the value, 0 for none.
	Min, Max, Now, Step float64
	// SelStart and SelEnd are the selection of a text field, in runes,
	// and Placeholder what it shows while empty.
	SelStart, SelEnd int
	Placeholder      string
	// Description tells more about the element than its name, as help
	// text read after it: a field's description and its error, or a
	// tooltip.
	Description string
	// KeyShortcuts are the keys that press or focus the element, as
	// ARIA's aria-keyshortcuts, written as the platform's menus write
	// them ("⌘K" on macOS, "Ctrl+K" elsewhere), separated by spaces.
	KeyShortcuts string
	// Level is how deep an item of a tree is, or the rank of a heading,
	// from 1; 0 for other nodes.
	Level int
	// Current says which item the element is the current one of, as the
	// page the user is on.
	Current AccessCurrent
	// AutoComplete says what the suggestions of a text field or a combo
	// box complete as the user types, as ARIA's aria-autocomplete.
	AutoComplete AccessAutoComplete
	// PosInSet is the place of an item in its set, from 1, and SetSize
	// the size of the set, 0 when not given: a list's rows say which of
	// all they are, whether or not the others are built, and the list how
	// many rows it has.
	PosInSet, SetSize int
	// RowCount and ColCount are how many rows and columns a table, a grid
	// or a tree grid has, built or not, as ARIA's aria-rowcount and
	// aria-colcount; RowIndex and ColIndex are the place of a row or a
	// cell among them, from 1, header rows counted (aria-rowindex,
	// aria-colindex). 0 when the app gives none: a row's PosInSet and its
	// table's SetSize then tell its place, and the rows and cells built
	// the rest.
	RowCount, ColCount, RowIndex, ColIndex int
	// RowSpan and ColSpan are how many rows and columns of its table a
	// cell covers, as ARIA's aria-rowspan and aria-colspan: 0 for one.
	// The cell after one spanning columns in its row is in the column
	// after those it covers.
	RowSpan, ColSpan int
	// Actions are the actions the element takes in AccessAction events.
	Actions AccessActions
	// RoleDescription is what screen readers read for the element's role
	// in place of its own, as "carousel" for a group or "slide" for a tab
	// panel: ARIA's aria-roledescription. "" reads the role.
	RoleDescription string
	// Controls are the IDs of the nodes whose content or presence the
	// element controls, as the region a disclosure button opens: ARIA's
	// aria-controls.
	Controls []uint64
	// DescribedBy are the IDs of the nodes whose text describes the
	// element, as a hint below a field, which Description holds: ARIA's
	// aria-describedby. ErrorMessage are those of the nodes holding the
	// message of an element that is AccessInvalid, whose text Description
	// starts with: ARIA's aria-errormessage, given only while invalid.
	DescribedBy, ErrorMessage []uint64
	// HasPopup is the kind of popup the element opens, as ARIA's
	// aria-haspopup.
	HasPopup AccessPopup
	// WebView is the web view whose page the node stands for, if any
	// (Surface.NewWebView): assistive technology finds the page's elements
	// inside it, and it has no children of its own.
	WebView WebView
	// TextRuns are the formats of the runes of a text field's Value, as a
	// rich text editor's: runs in order, from rune Start to End, that
	// screen readers read as bold, italic, links and headings. Runes no
	// run holds take the text's default font. Empty for plain text.
	TextRuns []AccessTextRun
}

// AccessTextRun is a run of the Value of a text field with formats of its
// own (AccessNode.TextRuns).
type AccessTextRun struct {
	// Start and End are the run's runes of Value.
	Start, End int
	// Family is its font's family, "" for the system's; Size its size in
	// DIPs, 0 for the text's; Weight from 100 to 900, 0 for 400.
	Family                           string
	Size                             float32
	Weight                           int
	Italic, Underline, Strikethrough bool
	// Color and Background are RGBA, transparent where unset.
	Color, Background [4]uint8
	// Link is the URL the run links to.
	Link string
	// Heading is the level of the heading the run is in, from 1, 0 in a
	// body paragraph.
	Heading int
}

// AccessRole is the kind of an element of an AccessTree.
type AccessRole uint8

const (
	RoleGroup AccessRole = iota
	RoleText
	RoleButton
	RoleLink
	RoleCheckBox
	RoleRadio
	RoleSwitch
	RoleSlider
	RoleProgress
	RoleTextField
	RoleImage
	RoleList
	RoleScroll
	RoleDialog
	RolePopup
	RoleTooltip
	RolePopUpButton
	RoleTabList
	RoleTab
	RoleSplitter
	RoleStatus
	RoleTable
	RoleRow
	RoleCell
	RoleColumnHeader
	RoleTree
	RoleTreeItem
	RoleListItem
	RoleMenuButton
	RoleToolbar
	RoleRadioGroup
	// RoleToggleButton is pressed while AccessChecked.
	RoleToggleButton
	// RoleComboBox is a text field with a popup of options, which is
	// AccessExpanded while it shows.
	RoleComboBox
	// RoleDisclosure is a button showing or hiding content below it, which
	// is AccessExpanded while it shows: the trigger of a collapsible, or
	// the header of an accordion's section.
	RoleDisclosure
	// RoleMeter shows a value in a range, as a level indicator, and
	// RoleStepper steps one, as a spin button: both have Min, Max and Now.
	RoleMeter
	RoleStepper
	// RoleColorWell is a button showing a color, its Value, which opens a
	// picker of it, as AppKit's color well.
	RoleColorWell
	// RoleAlertDialog is a dialog asking about something important, its
	// Description the message.
	RoleAlertDialog
	// RoleMenu holds the items of a menu, RoleMenuBar the menus' titles
	// along a window, and RoleMenuItem is an item of either, which
	// RoleMenuItemCheckBox and RoleMenuItemRadio are when they show a
	// choice, AccessChecked (AccessMixed) while chosen.
	RoleMenu
	RoleMenuBar
	RoleMenuItem
	RoleMenuItemCheckBox
	RoleMenuItemRadio
	// RoleHeading is the title of a section, Level its rank: 1 for the
	// highest.
	RoleHeading
	// RoleRegion is a landmark with a name, as a panel screen readers list
	// among the window's landmarks: ARIA's region.
	RoleRegion
	// RoleLog, RoleTimer and RoleAlert are ARIA's live regions of those
	// roles: content added to a log is read out as it is added
	// (AccessTree.Announcements), a timer's changes only when the user
	// goes to it, and an alert's content at once, as it appears or
	// changes (AccessTree.Alerts).
	RoleLog
	RoleTimer
	RoleAlert
	// RoleSeparator divides groups of content or controls, as a rule
	// between a toolbar's groups of buttons, AccessVertical when it runs
	// up and down.
	RoleSeparator
	// RoleRowHeader is the cell of a table's row naming the row, as
	// RoleColumnHeader names a column.
	RoleRowHeader
	// RoleSearch is a landmark holding what searches the app or a
	// document, as a search field and its button: ARIA's search.
	RoleSearch
	// RoleArticle is a self-contained piece of content, as a post of a
	// feed or a comment of a thread: ARIA's article, no landmark.
	RoleArticle
	// RoleNavigation is a landmark holding links that move around the app
	// or a document, as a breadcrumb trail or a navigation bar: ARIA's
	// navigation.
	RoleNavigation
	// RoleGrid is a table whose cells take the focus and the arrow keys,
	// as a spreadsheet's, and RoleTreeGrid one whose rows open and close
	// as a tree's items do: ARIA's grid and treegrid. Their rows and
	// cells are RoleRow, RoleCell and RoleColumnHeader, as a table's.
	RoleGrid
	RoleTreeGrid
	// RoleDeletion and RoleInsertion hold content removed from or added
	// to a document, as the lines of a diff or a price struck out:
	// ARIA's deletion and insertion, HTML's del and ins.
	RoleDeletion
	RoleInsertion
)

// Tabular reports whether nodes of a role hold rows of cells: tables,
// grids and tree grids.
func (r AccessRole) Tabular() bool {
	return r == RoleTable || r == RoleGrid || r == RoleTreeGrid
}

// Ranged reports whether nodes of a role have a value in a range (Min,
// Max and Now).
func (r AccessRole) Ranged() bool {
	return r == RoleSlider || r == RoleProgress || r == RoleMeter || r == RoleStepper
}

// Live returns how content changing in a live region of a role is read,
// as ARIA's aria-live, "polite" or "assertive", and whether the region is
// read whole (aria-atomic): "" for roles that are no live region.
// Statuses and logs are read after what is being read, an alert at once;
// a timer is read only when the user goes to it.
func (r AccessRole) Live() (live string, atomic bool) {
	switch r {
	case RoleStatus:
		return "polite", true
	case RoleLog:
		return "polite", false
	case RoleAlert:
		return "assertive", true
	}
	return "", false
}

// AccessStates are the states of an element of an AccessTree.
// AccessChecked is the choice among a tab list's tabs, a tree's items, and
// the rows of a list or a table that are AccessSelectable.
type AccessStates uint32

const (
	AccessFocusable AccessStates = 1 << iota
	AccessDisabled
	AccessChecked
	AccessMixed
	AccessSelected
	AccessExpanded
	AccessMultiline
	AccessPassword
	AccessReadOnly
	AccessSelectable
	// AccessOffscreen is set on elements built but out of view, as the
	// rows a list builds beyond its edges.
	AccessOffscreen
	// AccessMultiselectable is set on a list, a table, a grid or a tree
	// whose user chooses several rows (aria-multiselectable).
	AccessMultiselectable
	// AccessSegment is set on the segments of a segmented control or a
	// group of toggles, as AppKit's subrole AXSegment says.
	AccessSegment
	// AccessSearch is set on a text field for searching.
	AccessSearch
	// AccessInvalid is set on a control whose value is not valid, as a
	// field with an error.
	AccessInvalid
	// AccessSortAscending and AccessSortDescending are set on the header
	// of the column a table's rows are sorted by.
	AccessSortAscending
	AccessSortDescending
	// AccessExpandable is set on an item of a tree with children, which
	// AccessExpanded shows.
	AccessExpandable
	// AccessVertical is set on a slider going up, a separator running up
	// and down, and an element the app says runs up and down, as ARIA's
	// aria-orientation="vertical".
	AccessVertical
	// AccessBusy is set on an element whose content is about to change,
	// as a region loading, which assistive technology waits for before
	// reading it: ARIA's aria-busy.
	AccessBusy
	// AccessSortOther is set on the header of the column a table's rows
	// are sorted by in an order neither ascending nor descending.
	AccessSortOther
	// AccessHorizontal is set on an element the app says runs across, as
	// ARIA's aria-orientation="horizontal". A slider or a separator that
	// is neither AccessVertical nor AccessHorizontal runs across; other
	// elements then have no orientation.
	AccessHorizontal
)

// AccessPopup is the kind of popup an element opens, as ARIA's
// aria-haspopup says.
type AccessPopup uint8

const (
	// PopupNone opens no popup.
	PopupNone AccessPopup = iota
	// PopupMenu opens a menu, aria-haspopup="menu" (or "true").
	PopupMenu
	// PopupListBox opens a list of options, PopupTree a tree, PopupGrid
	// a grid, as a calendar's, and PopupDialog a dialog.
	PopupListBox
	PopupTree
	PopupGrid
	PopupDialog
)

// String returns the value of aria-haspopup that p is, "" for PopupNone.
func (p AccessPopup) String() string {
	switch p {
	case PopupMenu:
		return "menu"
	case PopupListBox:
		return "listbox"
	case PopupTree:
		return "tree"
	case PopupGrid:
		return "grid"
	case PopupDialog:
		return "dialog"
	}
	return ""
}

// AccessCurrent is the item an element is the current one of among those
// around it, as ARIA's aria-current says.
type AccessCurrent uint8

const (
	// CurrentNone is not current.
	CurrentNone AccessCurrent = iota
	// CurrentItem is the current item of a set, aria-current="true".
	CurrentItem
	// CurrentPage is the page the user is on, as the last crumb of a
	// breadcrumb trail or the link of a navigation bar to it.
	CurrentPage
	// CurrentStep is the step of a process the user is at.
	CurrentStep
	// CurrentLocation is the place shown, as in a flow chart.
	CurrentLocation
	// CurrentDate is today in a calendar, and CurrentTime now in a
	// timetable.
	CurrentDate
	CurrentTime
)

// String returns the value of aria-current that c is, "" for CurrentNone.
func (c AccessCurrent) String() string {
	switch c {
	case CurrentItem:
		return "true"
	case CurrentPage:
		return "page"
	case CurrentStep:
		return "step"
	case CurrentLocation:
		return "location"
	case CurrentDate:
		return "date"
	case CurrentTime:
		return "time"
	}
	return ""
}

// AccessAutoComplete is what the suggestions of a text field complete as
// the user types, as ARIA's aria-autocomplete says.
type AccessAutoComplete uint8

const (
	// AutoCompleteNone suggests nothing as the user types.
	AutoCompleteNone AccessAutoComplete = iota
	// AutoCompleteInline completes the text in place, the rest of the
	// value inserted after the caret and selected.
	AutoCompleteInline
	// AutoCompleteList shows a popup of the values that complete it.
	AutoCompleteList
	// AutoCompleteBoth shows the popup and completes the text in place
	// with the option the arrows are on.
	AutoCompleteBoth
)

// String returns the value of aria-autocomplete that a is, "" for
// AutoCompleteNone.
func (a AccessAutoComplete) String() string {
	switch a {
	case AutoCompleteInline:
		return "inline"
	case AutoCompleteList:
		return "list"
	case AutoCompleteBoth:
		return "both"
	}
	return ""
}

// AccessActions are the actions an element of an AccessTree takes.
type AccessActions uint8

const (
	ActionPress AccessActions = 1 << iota
	ActionFocus
	ActionIncrement
	ActionDecrement
	ActionSetValue
	// ActionScrollIntoView scrolls the containers around the element to
	// show it, as assistive technology moving to it out of view asks.
	ActionScrollIntoView
	// ActionExpand opens and closes an item of a tree with children
	// (AccessExpand, AccessCollapse), which pressing chooses.
	ActionExpand
)

// AccessActionKind is the action of an AccessAction event.
type AccessActionKind uint8

const (
	AccessPress AccessActionKind = iota
	AccessFocus
	AccessIncrement
	AccessDecrement
	AccessSetValue
	AccessScrollIntoView
	AccessExpand
	AccessCollapse
	// AccessSelectText selects runes From to To of a text field's Value.
	AccessSelectText
	// AccessInsertText replaces the selection of a text field with Text,
	// as typing it does: at the caret when nothing is selected.
	AccessInsertText
)
