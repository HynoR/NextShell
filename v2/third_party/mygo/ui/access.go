package ui

import (
	"slices"
	"strings"

	"github.com/egoist/mygo/internal/l10n"
	"github.com/egoist/mygo/internal/platform"
)

// Assistive technology, such as screen readers, sees the elements of each
// frame as a tree once it asks for the window: widgets, texts, and the
// elements that take the focus or have a Label, with their states. It acts
// on them as the keyboard and the pointer would: pressing clicks, setting
// a text field's value edits it.

// Role is what an element is to assistive technology. Widgets set their
// own; other elements get one from what they do: one that is clickable and
// takes the focus is a button, a text is a text, a scroll container a
// scroll area, and one with a Label, or that takes the focus, a group.
type Role uint8

const (
	// RoleAuto finds the role from what the element does.
	RoleAuto Role = iota
	// RoleNone leaves the element out, but not its children.
	RoleNone
	RoleGroup
	RoleText
	RoleButton
	// RoleLink is a link: focused, it takes Enter but not Space, which
	// scrolls, as in a web page.
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
	// RoleListItem is an item of a list: the rows of a List are.
	RoleListItem
	// RoleMenuButton is a button opening a menu (Element.Menu).
	RoleMenuButton
	// RoleToolbar holds controls (Toolbar), RoleRadioGroup radio buttons
	// (RadioGroup, Segmented), and RoleToggleButton is a button that stays
	// pressed (Toggle).
	RoleToolbar
	RoleRadioGroup
	RoleToggleButton
	// RoleComboBox is a text input with a popup of options (ComboboxBase).
	RoleComboBox
	// RoleDisclosure is a button showing or hiding content below it, as
	// the trigger of a collapsible (CollapsibleBase), expanded while open.
	RoleDisclosure
	// RoleMeter shows a value in a range (Meter), and RoleStepper steps a
	// value in one (Stepper).
	RoleMeter
	RoleStepper
	// RoleColorWell is a button showing a color, which it opens a picker
	// of (ColorWell).
	RoleColorWell
	// RoleAlertDialog is a dialog asking about something important
	// (AlertDialog).
	RoleAlertDialog
	// RoleMenu holds the items of a menu drawn in the window, as a drop-down
	// menu's panel, and RoleMenuBar the titles of menus along it.
	// RoleMenuItem is an item of either, and RoleMenuItemCheckBox and
	// RoleMenuItemRadio items showing a choice, checked with Checked.
	RoleMenu
	RoleMenuBar
	RoleMenuItem
	RoleMenuItemCheckBox
	RoleMenuItemRadio
	// RoleHeading is the title of a section, named by its text, its rank
	// set with Level.
	RoleHeading
	// RoleRegion is a landmark with a Label, as an activity feed or a
	// panel: screen readers list it among the window's landmarks, and
	// jump to it. Without a Label it is a group, as ARIA's region.
	RoleRegion
	// RoleLog is a live region of messages added in order, as a chat's
	// transcript: screen readers read out what is added to it, once it has
	// shown, after what they are reading.
	RoleLog
	// RoleTimer counts time, as a countdown, whose changes screen readers
	// read only when the user goes to it.
	RoleTimer
	// RoleAlert is a live region of important news, as a field's error:
	// screen readers read its content out at once, interrupting what they
	// are reading, as it appears and whenever it changes.
	RoleAlert
	// RoleSeparator divides groups of content or controls, as a rule
	// between a toolbar's groups of buttons (a Divider): screen readers
	// read it as running up and down when it is taller than wide.
	RoleSeparator
	// RoleRowHeader is the cell naming its row of a table, as
	// RoleColumnHeader names a column.
	RoleRowHeader
	// RoleSearch is a landmark holding what searches the app or a
	// document, as the search field and its button at the top of a
	// window: screen readers list it among the window's landmarks, and
	// jump to it.
	RoleSearch
	// RoleArticle is a self-contained piece of content, as a post of a
	// feed or a comment of a thread, named by its Label: screen readers
	// step from one article to the next, as HTML's article.
	RoleArticle
	// RoleNavigation is a landmark holding links that move around the
	// app or a document, as a breadcrumb trail (Breadcrumbs) or a
	// navigation bar: screen readers list it among the window's
	// landmarks, and jump to it.
	RoleNavigation
	// RoleGrid is a table whose cells take the focus and the arrow keys,
	// as a spreadsheet's, where RoleTable is one to read; RoleTreeGrid is
	// a grid whose rows open and close as a tree's items do (Expanded,
	// Level). Their rows, cells and headers are RoleRow, RoleCell,
	// RoleColumnHeader and RoleRowHeader, as a table's, and the rows of a
	// List shown as one are its rows.
	RoleGrid
	RoleTreeGrid
	// RoleListBox is a list of options to choose from, as ARIA's listbox,
	// and RoleOption one of its options, selected with Checked, as ARIA's
	// aria-selected: what a list of your own offers, such as suggestions
	// at a text area's caret whose highlighted one the text area points
	// at with ActiveDescendant. The popups of SelectBase and ComboboxBase
	// are list boxes.
	RoleListBox
	RoleOption
	// RoleDeletion and RoleInsertion hold content removed from or added
	// to a document, as HTML's del and ins, ARIA's deletion and
	// insertion: the removed and added lines of a diff, or an old price
	// struck out beside the new one. Screen readers read their text with
	// "deletion" or "insertion" rather than as plain text. They take no
	// Label: the text inside them is what they hold.
	//
	//	ui.Row(c).Gap(6).Children(func() {
	//		ui.Text(c, "$40").Strikethrough().Role(ui.RoleDeletion)
	//		ui.Text(c, "$30").Role(ui.RoleInsertion)
	//	})
	RoleDeletion
	RoleInsertion
)

// tabular reports whether elements of a role hold rows of cells: tables,
// grids and tree grids.
func tabular(r Role) bool { return r == RoleTable || r == RoleGrid || r == RoleTreeGrid }

// Role sets what the element is to assistive technology, for an element
// drawn as a widget it is not built from, such as a custom toggle.
func (e *node) Role(r Role) *node {
	e.role = r
	return e
}

// The states of an element of your own, which a widget's base sets
// itself, tell assistive technology what it shows, as ARIA's states do
// on the web.

// Checked tells assistive technology whether the element, as a check box,
// switch, radio button, toggle button or menu item of your own, is on, or
// whether an option (RoleOption) is selected. Bases set it from their
// value.
func (e *node) Checked(on bool) *node {
	e.checked = 1 + int8(b2f(on))
	return e
}

// Mixed tells assistive technology that the element, a check box, is
// partly on, as one checking a group whose boxes differ.
func (e *node) Mixed() *node {
	e.checked = 3
	return e
}

// Expanded tells assistive technology whether what the element opens
// shows, as the popup of a button or the section below a header.
func (e *node) Expanded(open bool) *node {
	e.expanded = open
	return e
}

// Busy tells assistive technology whether the element's content is about
// to change, as a region loading, or one updated in several steps:
// screen readers wait until it is no longer busy to read what changed in
// it. A placeholder standing in for the content, as a skeleton or a
// spinner, says what is happening (a RoleStatus labelled "Loading").
func (e *node) Busy(on bool) *node {
	e.busy = on
	return e
}

// Current is the item an element is the current one of, among those
// around it (Element.Current), as ARIA's aria-current says.
type Current uint8

const (
	// CurrentNone is not current.
	CurrentNone Current = iota
	// CurrentItem is the current item of a set, aria-current="true".
	CurrentItem
	// CurrentPage is the page the user is on: the last crumb of a
	// breadcrumb trail, or the link of a navigation bar to the page shown.
	CurrentPage
	// CurrentStep is the step of a process the user is at, as in a
	// wizard's steps.
	CurrentStep
	// CurrentLocation is the place shown, as in a flow chart or a map.
	CurrentLocation
	// CurrentDate is today in a calendar, and CurrentTime now in a
	// timetable.
	CurrentDate
	CurrentTime
)

// Current tells assistive technology that the element is the current one
// of the items around it, as the page the user is on in a breadcrumb
// trail or a navigation bar, which screen readers read with it ("current
// page"). CurrentNone takes it back.
func (e *node) Current(kind Current) *node {
	e.current = kind
	return e
}

// AutoComplete is what the suggestions of a text input complete as the
// user types (Element.AutoComplete), as ARIA's aria-autocomplete says.
type AutoComplete uint8

const (
	// AutoCompleteNone suggests nothing as the user types: the popup of
	// a combobox, if any, shows the same options whatever is typed.
	AutoCompleteNone AutoComplete = iota
	// AutoCompleteInline completes the text in place: the rest of the
	// value the input guesses is inserted after the caret, selected.
	AutoCompleteInline
	// AutoCompleteList shows a popup of the values that complete what
	// was typed, as the options of a Combobox or an Autocomplete.
	AutoCompleteList
	// AutoCompleteBoth shows the popup, and completes the text in place
	// with the option the arrows are on.
	AutoCompleteBoth
)

// AutoComplete tells assistive technology what the suggestions of a
// text input or a combobox of your own complete as the user types, as
// ARIA's aria-autocomplete: AutoCompleteList for a popup of the values
// matching what was typed, which screen readers announce with the field
// so that the user knows to look there, AutoCompleteInline for the text
// completed in place. It only tells it: the view shows the suggestions
// itself, as in ComboboxBase's popup. Combobox, Autocomplete, and a
// TokenField with suggestions, tell AutoCompleteList themselves;
// AutoCompleteNone takes it back.
func (e *node) AutoComplete(kind AutoComplete) *node {
	e.autoComplete = kind
	return e
}

// Value sets what assistive technology reads as the element's value, as
// the choice a button opening a popup shows, or the text of a range's
// value beside its number (aria-valuetext), as "3 of 10 steps".
func (e *node) Value(s string) *node {
	e.accValue = s
	return e
}

// Range tells assistive technology the range and the value of a slider,
// progress bar, meter or stepper of your own: value, from lo to hi. Step
// tells it how far the keys move the value.
func (e *node) Range(lo, hi, value float64) *node {
	e.hasRange, e.accRange = true, [3]float64{lo, hi, value}
	return e
}

// Level tells assistive technology the rank of a heading, from 1 for the
// highest, or how deep an item of a tree is, from 1 at the top.
func (e *node) Level(n int) *node {
	e.level = max(n, 0)
	return e
}

// Orientation tells assistive technology which way the element runs, as
// ARIA's aria-orientation: Vertical for a slider, separator, splitter,
// toolbar, tab list or menu of your own going up and down, which screen
// readers then read as vertical, Horizontal for one going across. It only tells it: the view lays the element out and
// handles its keys itself (FocusGroup). A slider or a separator told
// neither runs across, a separator taller than wide up and down; other
// elements have no orientation. A SliderBase going up says so itself
// (Vertical).
//
//	ui.Box(c).Size(4, 200).Draw(drawTrack).
//		Role(ui.RoleSlider).Label("Volume").Range(0, 100, app.volume).
//		Focusable().Orientation(ui.Vertical)
//
// Any other value, as Horizontal|Vertical, takes it back.
func (e *node) Orientation(o Orientation) *node {
	switch o {
	case Horizontal, Vertical:
		e.orient = o
	default:
		e.orient = 0
	}
	return e
}

// KeyShortcuts tells assistive technology a key, with exactly the
// modifiers mods, that presses or focuses the element, as ARIA's
// aria-keyshortcuts: the window's shortcut that runs a toolbar button's
// command, or opens a command palette, which screen readers read with the
// element's name. It is written as the platform's menus write it ("⌘K"
// for Cmd+K on macOS, "Ctrl+K" elsewhere). It only tells the key: handle
// it with Shortcut on the context, or OnWindowShortcut. Call it once for
// each key that does.
func (e *node) KeyShortcuts(mods Modifiers, key Key) *node {
	s := shortcutText(accelerator(mods, key))
	if s == "" {
		return e
	}
	for k := range strings.FieldsSeq(e.keyShortcuts) {
		if k == s {
			return e
		}
	}
	if e.keyShortcuts != "" {
		s = e.keyShortcuts + " " + s
	}
	e.keyShortcuts = s
	return e
}

// RowCount tells assistive technology how many rows a table, grid or tree
// grid of your own has, built or not, header rows included, as ARIA's
// aria-rowcount: one that builds only the rows in view says how many it
// has, which screen readers read with the rows' RowIndex, as "row 4,121
// of 10,000". The rows of a List and a Table say which of all they are
// themselves.
func (e *node) RowCount(n int) *node {
	e.accGrid[0] = int32(max(n, 0))
	return e
}

// ColumnCount tells assistive technology how many columns a table, grid
// or tree grid of your own has, built or not (aria-colcount).
func (e *node) ColumnCount(n int) *node {
	e.accGrid[1] = int32(max(n, 0))
	return e
}

// RowIndex tells assistive technology the place of a row, or of a cell,
// among all the rows of its table, grid or tree grid, from 1 for the
// first, header rows included, as ARIA's aria-rowindex: for one that
// builds only the rows in view, with its RowCount. A cell takes its row's.
func (e *node) RowIndex(i int) *node {
	e.accGrid[2] = int32(max(i, 0))
	return e
}

// ColumnIndex tells assistive technology the place of a cell or a column
// header among all the columns of its table, grid or tree grid, from 1
// (aria-colindex): for one that builds only the columns in view, or
// leaves some out. Cells without one follow the one before them, after
// the columns it covers (ColumnSpan).
func (e *node) ColumnIndex(i int) *node {
	e.accGrid[3] = int32(max(i, 0))
	return e
}

// PositionInSet tells assistive technology which item of its set the
// element is, pos from 1, and how many items the set has, size, as ARIA's
// aria-posinset and aria-setsize: for an item of a list, list box, tree,
// menu or tab list of your own whose set is not what is built around it,
// as one built only where it shows, or a tree laid out flat in one
// column (with Level), so that screen readers read "3 of 50". Either at
// 0 or less is not given: the place the element's widget gives it, if
// any, stands.
//
//	for i := app.first; i < app.last; i++ { // only those in view
//		ui.Row(c).Role(ui.RoleOption).PositionInSet(i+1, len(app.results)).
//			Children(func() { ui.Text(c, app.results[i]) })
//	}
//
// It wins over the place a ComboboxBase, SelectBase, MenuBase, Tree or
// GridView gives its items, which counts those it builds, as for a
// combobox showing the first 50 of 1,000 results. A List's rows keep
// theirs, which say which of all its rows they are, built or not, and
// place the rows of its table on macOS and Windows; a row or a cell of a
// grid of your own says its place with RowIndex and ColumnIndex.
func (e *node) PositionInSet(pos, size int) *node {
	e.accSet = [2]int32{int32(max(pos, 0)), int32(max(size, 0))}
	return e
}

// AccessSelected tells assistive technology whether the element, a tab,
// an option, a row, a cell or an item of a tree of your own, is
// selected, as ARIA's aria-selected, without the accent background and
// text color Selected paints: for one that shows its choice its own way,
// as a tab underlined or a row with a check mark.
//
//	tab := ui.ButtonBase(c).Role(ui.RoleTab).Label(name).AccessSelected(i == app.tab)
//	if i == app.tab {
//		tab.Bold()
//	}
//
// It sets the state Selected sets, which Checked also sets on these
// roles, so every platform reads it as Selected's: a row or a cell saying
// whether it is selected, false or true, is one its table or grid
// chooses among others (Selectable). It only tells: the view shows the
// choice, and changes it as the element is clicked.
func (e *node) AccessSelected(on bool) *node {
	return e.Checked(on)
}

// Multiselectable tells assistive technology whether the user chooses
// several of the items of the element, a list, grid, tree or tree grid
// of your own, as ARIA's aria-multiselectable: screen readers offer to
// choose more than one. Its rows or items say which are chosen with
// Checked, or Selected, which shows it too. Lists, tables and grid views
// with a Selection tell it themselves.
func (e *node) Multiselectable(on bool) *node {
	e.chooseMany = on
	if on {
		e.choosesItems = true // and its items are chosen
	}
	return e
}

// ActiveDescendant tells assistive technology that d has the keyboard
// focus while the element does: the option the arrows are on in a list or
// a menu that keeps the focus itself, as a combobox's input does. Call it
// once d is built, inside the element or in its popup.
func (e *node) ActiveDescendant(d *node) *node {
	e.activeDescendant = d
	return e
}

// RoleDescription tells assistive technology what to call the element's
// role, in place of its own, as ARIA's aria-roledescription: "carousel"
// for a group of slides, and "slide" for each. Screen readers read it
// where they would read the role, so give it in the user's language, and
// keep the role, which says how to use the element. "" reads the role.
func (e *node) RoleDescription(s string) *node {
	e.roleDescription = s
	return e
}

// Controls tells assistive technology that the element controls the
// content or the presence of target, as ARIA's aria-controls: the
// region a disclosure button opens, or the list a filter's input
// narrows. Screen readers offer to move to it. Call it once target is
// built, once for each element it controls.
func (e *node) Controls(target *node) *node {
	if target != nil && !slices.Contains(e.controls, target) {
		e.controls = append(e.controls, target)
	}
	return e
}

// Popup is the kind of popup an element opens (Element.HasPopup), as
// ARIA's aria-haspopup says.
type Popup uint8

const (
	// PopupNone opens no popup.
	PopupNone Popup = iota
	// PopupMenu opens a menu, aria-haspopup="menu" (or "true").
	PopupMenu
	// PopupListBox opens a list of options, as a select's.
	PopupListBox
	// PopupTree opens a tree, PopupGrid a grid, as a date picker's
	// calendar, and PopupDialog a dialog.
	PopupTree
	PopupGrid
	PopupDialog
)

// HasPopup tells assistive technology the kind of popup the element
// opens, as a button opening a list of options (PopupListBox) or a menu
// (PopupMenu), which Expanded says shows: screen readers read it with the
// element ("pop-up button", "menu button") and offer to open it.
// PopupNone takes it back.
func (e *node) HasPopup(kind Popup) *node {
	e.hasPopup = kind
	return e
}

// SortDirection is the order a table's rows are sorted in by a column
// (Element.Sort), as ARIA's aria-sort says.
type SortDirection uint8

const (
	// SortNone is not sorted by the column.
	SortNone SortDirection = iota
	// SortAscending and SortDescending sort the rows up or down the
	// column's values.
	SortAscending
	SortDescending
	// SortOther sorts them in an order of another kind.
	SortOther
)

// Sort tells assistive technology the order a table's rows are sorted in
// by the column whose header the element is (RoleColumnHeader), as ARIA's
// aria-sort: for a sortable table of your own, whose headers show it.
// Table's headers tell it from ListState.Sort and SortBy.
func (e *node) Sort(dir SortDirection) *node {
	e.sort = int8(dir)
	return e
}

// accessRole returns the element's role, and false for elements assistive
// technology does not see.
func (e *node) accessRole() (platform.AccessRole, bool) {
	switch e.role {
	case RoleNone:
		return 0, false
	case RoleAuto:
	case RoleListItem:
		// The rows of a List shown as a table or a grid are its rows.
		if e.listRow && e.parent != nil && tabular(e.parent.role) {
			return platform.RoleRow, true
		}
		return platform.RoleListItem, true
	case RoleListBox:
		return platform.RoleList, true // choosing its items (accessDetails)
	case RoleOption:
		return platform.RoleListItem, true
	case RoleDeletion:
		return platform.RoleDeletion, true
	case RoleInsertion:
		return platform.RoleInsertion, true
	default:
		return platform.AccessRole(e.role - RoleGroup), true
	}
	switch {
	case e.flags&flagEditable != 0:
		return platform.RoleTextField, true
	case e.kind == kindText:
		return platform.RoleText, e.text != ""
	case e.kind == kindImage || e.kind == kindIcon:
		return platform.RoleImage, e.label != "" // others are decoration
	case e.flags&flagClickable != 0 && e.flags&flagFocusable != 0:
		return platform.RoleButton, true
	case e.flags&(flagScrollX|flagScrollY) != 0:
		return platform.RoleScroll, true
	case e.label != "" || e.flags&flagFocusable != 0 || e.busy || e.current != CurrentNone:
		// A state of its own shows the element too, as ARIA's global
		// states do a generic element.
		return platform.RoleGroup, true
	}
	return 0, false
}

// cellRole reports whether nodes of a role are cells of a table, headers
// included, which Checked chooses (aria-selected).
func cellRole(r platform.AccessRole) bool {
	return r == platform.RoleCell || r == platform.RoleColumnHeader || r == platform.RoleRowHeader
}

// leafRole reports whether elements of a role take the text inside them
// as their name, rather than showing it as elements of its own. Column
// and row headers and headings show what is inside them too, as their
// children are not presentational in ARIA: buttons in them, as a header's
// sort button, can be reached.
func leafRole(r platform.AccessRole) bool {
	switch r {
	case platform.RoleGroup, platform.RoleList, platform.RoleScroll, platform.RoleDialog, platform.RoleAlertDialog, platform.RolePopup, platform.RoleStatus,
		platform.RoleTabList, platform.RoleTable, platform.RoleRow, platform.RoleCell, platform.RoleColumnHeader, platform.RoleRowHeader, platform.RoleTree,
		platform.RoleListItem, platform.RoleToolbar, platform.RoleRadioGroup, platform.RoleMenu, platform.RoleMenuBar, platform.RoleHeading, platform.RoleNavigation,
		platform.RoleRegion, platform.RoleLog, platform.RoleTimer, platform.RoleAlert, platform.RoleSearch,
		platform.RoleArticle, platform.RoleGrid, platform.RoleTreeGrid, platform.RoleDeletion, platform.RoleInsertion:
		return false
	}
	return true
}

// innerText returns the texts of an element and those inside it, joined
// by spaces. An element inside it with a Label gives its label in place
// of its texts, as a name from content takes a descendant's aria-label on
// the web: a row reads "Branch main (checked out)" rather than the "✓
// main" drawn for it; one hidden from assistive technology
// (AccessHidden) gives nothing. Assistive technology names elements by it
// every frame, so a single text is returned as it is, without a copy.
func (e *node) innerText() string {
	if e.flags&flagInvisible != 0 || e.hidden {
		return ""
	}
	if e.kind == kindText {
		return e.accessText() // its text holds that of the elements inside it
	}
	var t innerTexts
	for ch := e.first; ch != nil; ch = ch.next {
		t.walk(ch)
	}
	if t.b.Len() == 0 {
		return t.first
	}
	return t.b.String()
}

// innerTexts joins texts: first holds the first alone, b all of them once
// there is a second.
type innerTexts struct {
	first string
	b     strings.Builder
}

func (t *innerTexts) walk(e *node) {
	if e.flags&flagInvisible != 0 || e.hidden || e.accessHidden {
		return
	}
	if e.label != "" {
		t.add(e.label)
		return
	}
	if e.kind == kindText {
		t.add(e.accessText())
		return // its text holds that of the elements inside it
	}
	for ch := e.first; ch != nil; ch = ch.next {
		t.walk(ch)
	}
}

// String returns the texts joined.
func (t *innerTexts) String() string {
	if t.b.Len() == 0 {
		return t.first
	}
	return t.b.String()
}

func (t *innerTexts) add(s string) {
	switch {
	case s == "":
	case t.first == "":
		t.first = s
	case t.b.Len() == 0:
		t.b.Grow(len(t.first) + 1 + len(s))
		t.b.WriteString(t.first)
		fallthrough
	default:
		t.b.WriteByte(' ')
		t.b.WriteString(s)
	}
}

// accessTree describes the last frame for assistive technology.
func (rt *engine) accessTree() *platform.AccessTree {
	// As many nodes as the last frame's, which the next is usually close
	// to: growing the slice from empty would copy it again and again.
	t := &platform.AccessTree{Nodes: make([]platform.AccessNode, 0, rt.accessNodes)}
	defer func() { rt.accessNodes = len(t.Nodes) }()
	var focused *node
	switch root := rt.c.root; {
	case root == nil:
	case rt.modal != 0 && rt.modalLayer != 0 && rt.c.overlay != nil:
		// What is behind a dialog is inert: the dialog on top and what
		// shows above it alone.
		on := false
		for ch := rt.c.overlay.first; ch != nil; ch = ch.next {
			if on = on || ch.id == rt.modalLayer; on {
				rt.accessElement(t, ch, -1, false, &focused)
			}
		}
	default:
		rt.accessElement(t, root, -1, false, &focused)
	}
	// A table or a grid whose cells say whether they are chosen chooses them, as
	// ARIA's grid holds its gridcells' selection.
	for i := range t.Nodes {
		if n := &t.Nodes[i]; cellRole(n.Role) && n.States&platform.AccessSelectable != 0 {
			if t.Selected == "" {
				t.Selected = l10n.In(rt.lang).Selected()
			}
			for p := n.Parent; p >= 0; p = t.Nodes[p].Parent {
				if t.Nodes[p].Role.Tabular() {
					t.Nodes[p].States |= platform.AccessSelectable
					break
				}
			}
		}
	}
	if rt.windowFocus() && rt.focused != 0 {
		focus := rt.focused
		// A list choosing rows with the arrows has the focus for them:
		// assistive technology follows the row chosen, as the arrows move
		// the choice, while it shows.
		if f := focused.rowsOfElement(); f != nil && f.s.cursor() != nil {
			for _, r := range f.rows {
				if r.i == *f.s.cursor() {
					focus = r.e.id
				}
			}
		}
		// A combobox has the focus for the option its arrows are on.
		if focused != nil && focused.activeDescendant != nil {
			focus = focused.activeDescendant.id
		}
		for _, id := range []uint64{focus, rt.focused} {
			if t.Focus == 0 && slices.ContainsFunc(t.Nodes, func(n platform.AccessNode) bool { return n.ID == id }) {
				t.Focus = id
			}
		}
	}
	if len(rt.announcements) > 0 {
		t.Announcements = slices.Clone(rt.announcements)
	}
	return t
}

// listOf returns the List the element is, if any.
func (e *node) listOf() *listFrame {
	if e == nil {
		return nil
	}
	return e.list
}

// rowsOfElement returns the list whose rows the element holds, if any.
func (e *node) rowsOfElement() *listFrame {
	if e == nil {
		return nil
	}
	return e.rowsOf
}

// accessElement adds the nodes of e and the elements inside it, below
// node parent; scrolled is set inside a scroll container, and focused gets
// the element with the keyboard focus.
func (rt *engine) accessElement(t *platform.AccessTree, e *node, parent int, scrolled bool, focused **node) {
	if e.flags&(flagInvisible|flagInert) != 0 || e.hidden || e.accessHidden {
		return
	}
	if e.id == rt.focused {
		*focused = e
	}
	if role, ok := e.accessRole(); ok {
		// Filled in where it lies in the tree: a node of its own, whose
		// address accessDetails takes, would be allocated for each.
		t.Nodes = append(t.Nodes, platform.AccessNode{
			ID: e.id, Parent: parent, Role: role, Label: e.label,
			Bounds: accessBounds(e),
		})
		n := &t.Nodes[len(t.Nodes)-1]
		if scrolled {
			n.Actions |= platform.ActionScrollIntoView
		}
		rt.accessDetails(e, n)
		if n.Role == platform.RoleRow && e.checked != 0 && n.States&platform.AccessSelectable == 0 {
			chooseRow(t, len(t.Nodes)-1)
		}
		parent = len(t.Nodes) - 1
		if e.kind == kindText {
			rt.accessInline(t, e, parent)
			return
		}
		if leafRole(role) {
			return
		}
	}
	if e.webView != nil {
		// Its page first, as it paints, in a node of its own that the
		// platform's elements of the page are inside.
		if v := rt.host.platformWebView(e.webView); v != nil {
			b := e.contentBox()
			t.Nodes = append(t.Nodes, platform.AccessNode{
				ID: e.id ^ pageNodeID, Parent: parent, Role: platform.RoleGroup, WebView: v,
				Bounds: platform.RectF{X: float64(b.X), Y: float64(b.Y), W: float64(b.W), H: float64(b.H)},
			})
		}
	}
	// Children in flow first, absolute ones above them, as they paint.
	scrolled = scrolled || e.scrolls()
	for ch := e.first; ch != nil; ch = ch.next {
		if ch.flags&flagAbsolute == 0 {
			rt.accessElement(t, ch, parent, scrolled, focused)
		}
	}
	for ch := e.first; ch != nil; ch = ch.next {
		if ch.flags&flagAbsolute != 0 {
			rt.accessElement(t, ch, parent, scrolled, focused)
		}
	}
}

// chooseRow makes node i, a row of your own that says whether it is
// chosen (Checked, Selected), one its table chooses, as ARIA's rows of a
// grid with aria-selected: the table, grid or tree grid around it holds
// the choice.
func chooseRow(t *platform.AccessTree, i int) {
	t.Nodes[i].States |= platform.AccessSelectable
	for p := t.Nodes[i].Parent; p >= 0; p = t.Nodes[p].Parent {
		if t.Nodes[p].Role.Tabular() {
			t.Nodes[p].States |= platform.AccessSelectable
			return
		}
	}
}

// pageNodeID tells the node of a web view's page from its element's.
const pageNodeID = 0x9e3779b97f4a7c15

// accessDetails fills in the name, value, states and actions of a node.
func (rt *engine) accessDetails(e *node, n *platform.AccessNode) {
	disabled := e.IsDisabled()
	if disabled || e.st.accessDisabled {
		n.States |= platform.AccessDisabled
	}
	if e.segment {
		n.States |= platform.AccessSegment
	}
	switch {
	case e.orient == Vertical, e.orient == 0 && n.Role == platform.RoleSeparator && e.h > e.w:
		// A slider going up, or a rule running up and down.
		n.States |= platform.AccessVertical
	case e.orient == Horizontal:
		n.States |= platform.AccessHorizontal
	}
	if e.search {
		n.States |= platform.AccessSearch
	}
	if e.invalid {
		n.States |= platform.AccessInvalid
	}
	switch e.sort {
	case 1:
		n.States |= platform.AccessSortAscending
	case 2:
		n.States |= platform.AccessSortDescending
	case 3:
		n.States |= platform.AccessSortOther
	}
	n.Description = e.description
	if len(e.describedBy) > 0 || len(e.errorMessage) > 0 {
		accessRelations(e, n)
	}
	n.RoleDescription, n.HasPopup = e.roleDescription, platform.AccessPopup(e.hasPopup)
	for _, c := range e.controls {
		n.Controls = append(n.Controls, c.id)
	}
	if f := e.nameFrom; f != nil {
		switch {
		case n.Label == "" || e.labelledBy && f.nameOf() != "":
			n.Label = f.nameOf()
		case e.nameJoin && f.nameOf() != "":
			n.Label = f.nameOf() + " " + n.Label
		}
	}
	if e.setSize > 0 {
		n.PosInSet, n.SetSize = e.setPos, e.setSize
	}
	// The app's own place in the set wins over its widget's, but for a
	// List's rows below.
	if pos := e.accSet[0]; pos > 0 {
		n.PosInSet = int(pos)
	}
	if size := e.accSet[1]; size > 0 {
		n.SetSize = int(size)
	}
	if e.choosesItems || e.role == RoleListBox {
		n.States |= platform.AccessSelectable
	}
	if e.chooseMany {
		n.States |= platform.AccessMultiselectable
	}
	// Rows and items of lists, column and row headers, headings and
	// statuses are named by their content, as leaves are, but show what is
	// inside them too, as a toast's button or a header's sort button.
	if n.Label == "" && (leafRole(n.Role) || n.Role == platform.RoleListItem || n.Role == platform.RoleRow || n.Role == platform.RoleColumnHeader ||
		n.Role == platform.RoleRowHeader || n.Role == platform.RoleHeading || n.Role == platform.RoleStatus) {
		n.Label = e.innerText()
	}
	if n.Label == "" && e.kind == kindText {
		// A text of another role, as an alert or a cell, whose text has no
		// node of its own.
		n.Label = e.text
	}
	if n.Role == platform.RoleRegion && n.Label == "" {
		n.Role = platform.RoleGroup // only a region with a name is a landmark
	}
	switch e.checked {
	case 2:
		n.States |= platform.AccessChecked
	case 3:
		n.States |= platform.AccessMixed
	}
	if e.checked != 0 && cellRole(n.Role) {
		// A cell telling whether it is chosen, as ARIA's gridcells with
		// aria-selected: chosen or not, it is chosen among others.
		n.States |= platform.AccessSelectable
	}
	if e.expanded {
		n.States |= platform.AccessExpanded
	}
	if e.expandable {
		n.States |= platform.AccessExpandable
	}
	if e.busy {
		n.States |= platform.AccessBusy
	}
	n.Current = platform.AccessCurrent(e.current)
	n.AutoComplete = platform.AccessAutoComplete(e.autoComplete)
	n.Level = e.level
	n.Value = e.accValue
	n.KeyShortcuts = e.keyShortcuts
	n.RowCount, n.ColCount, n.RowIndex, n.ColIndex = int(e.accGrid[0]), int(e.accGrid[1]), int(e.accGrid[2]), int(e.accGrid[3])
	if cellRole(n.Role) {
		// A cell covering several columns or rows of its table, as its
		// grid lays it out (aria-colspan, aria-rowspan).
		n.ColSpan, n.RowSpan = e.cellSpan()
	}
	if e.hasRange {
		n.Min, n.Max, n.Now, n.Step = e.accRange[0], e.accRange[1], e.accRange[2], e.accStep
	}
	// A row of a list says which of all it is, built or not, and the list
	// how many it has.
	if f := e.parent.listOf(); e.listRow && f != nil {
		n.PosInSet, n.SetSize = e.rowIndex+1, f.n
	}
	if f := e.rowsOf; f != nil {
		n.SetSize = f.n
		if f.s.cursor() != nil {
			n.States |= platform.AccessSelectable // as do its rows
		}
		if f.s.Selection != nil {
			n.States |= platform.AccessMultiselectable
		}
	}
	if e.flags&flagChoosable != 0 || e.role == RoleOption {
		n.States |= platform.AccessSelectable
	}
	// What a scroll container built out of view, as the rows of a list
	// beyond its edges.
	if st := e.st; (st.vw <= 0 || st.vh <= 0) && e.w > 0 && e.h > 0 {
		n.States |= platform.AccessOffscreen
	}
	if disabled {
		return
	}
	// What ignores input takes no focus, which moving a slider with its
	// keys needs, but is pressed as any other.
	keys := !e.st.ignoreInput
	if (e.flags&(flagFocusable|flagEditable|flagChoosable) != 0 || e.focusHighlights) && keys {
		// Focusing a row a list chooses chooses it, and an option of a
		// select highlights it.
		n.States |= platform.AccessFocusable
		n.Actions |= platform.ActionFocus
	}
	if e.flags&flagClickable != 0 {
		n.Actions |= platform.ActionPress
	}
	if (n.Role == platform.RoleSlider || n.Role == platform.RoleStepper) && keys {
		n.Actions |= platform.ActionIncrement | platform.ActionDecrement
	}
	if e.expandable {
		n.Actions |= platform.ActionExpand
	}
	if ed := e.st.editor; ed != nil && e.flags&flagEditable != 0 {
		if ed.readOnly {
			n.States |= platform.AccessReadOnly
		} else {
			n.Actions |= platform.ActionSetValue
		}
		if ed.multiline {
			n.States |= platform.AccessMultiline
		}
		n.Placeholder = ed.placeholder
		if ed.password {
			n.States |= platform.AccessPassword
		} else {
			n.Value = ed.buf.string()
			n.SelStart, n.SelEnd = ed.selection()
		}
	}
	// Custom text clients without an editor take set-value here; inputs and
	// text areas with an editor follow the editor branch above.
	if e.textClient != nil && (e.st == nil || e.st.editor == nil || e.flags&flagEditable == 0) {
		n.Actions |= platform.ActionSetValue
	}
	if e.accFill != nil {
		e.accFill(n)
	}
	// What is unavailable (AccessDisabled) is reached, read and focused,
	// but not operated, as ARIA's aria-disabled.
	if e.st.accessDisabled {
		n.Actions &= platform.ActionFocus | platform.ActionScrollIntoView
	}
}

// accessRelations fills in what describes a node: the elements whose
// text describes it (DescribedBy), after its own description, and while
// it is invalid, those holding its error (ErrorMessage), first, as Error
// puts its message.
func accessRelations(e *node, n *platform.AccessNode) {
	var desc innerTexts
	for _, d := range e.describedBy {
		n.DescribedBy = append(n.DescribedBy, d.id)
		desc.add(d.describes())
	}
	n.Description = joinDescription(n.Description, desc.String())
	if !e.invalid {
		return
	}
	var msg innerTexts
	for _, m := range e.errorMessage {
		n.ErrorMessage = append(n.ErrorMessage, m.id)
		msg.add(m.describes())
	}
	n.Description = joinDescription(msg.String(), n.Description)
}

// describes returns the text an element gives another it describes
// (DescribedBy, ErrorMessage): its Label, or the texts inside it, as
// ARIA computes a description from the elements it references, hidden
// from assistive technology or not.
func (e *node) describes() string {
	if e.label != "" {
		return e.label
	}
	if e.kind == kindText {
		return e.accessText()
	}
	var t innerTexts
	for ch := e.first; ch != nil; ch = ch.next {
		t.walk(ch)
	}
	return t.String()
}

// accessibilityOn starts describing frames for assistive technology,
// with the last one at once.
func (rt *engine) accessibilityOn() {
	if !rt.access {
		rt.access = true
		// None built yet, or one waiting for the window to show: the
		// frame builds, out of sight too (the event is input), so that
		// assistive technology reads what the window shows now.
		if rt.c.root == nil || rt.held {
			rt.requestFrame()
		}
	}
	t := rt.accessTree()
	rt.liveRegions(t, false) // what live regions show was there before
	rt.host.updateAccessibility(rt.windowAccess(t))
}

// accessAction performs an action of assistive technology on an element
// of the last frame, as the keyboard or the pointer would.
func (rt *engine) accessAction(ev platform.SurfaceEvent) {
	s := rt.states[ev.ID]
	// What is hidden from it, as a page leaving, takes none of its actions.
	if s == nil || s.flags&flagDisabled != 0 || s.accessHidden {
		return
	}
	// What is unavailable takes the focus, is scrolled to and has its text
	// selected, but is neither pressed nor edited.
	if s.accessDisabled {
		switch ev.Action {
		case platform.AccessFocus, platform.AccessScrollIntoView, platform.AccessSelectText:
		default:
			return
		}
	}
	switch ev.Action {
	case platform.AccessPress:
		if s.flags&flagMenuButton != 0 {
			rt.openMenuButton(s, true)
			break
		}
		if s.flags&flagClickable != 0 {
			s.click(0)
		} else {
			rt.focusOn(s)
		}
	case platform.AccessFocus:
		if s.ignoreInput {
			return
		}
		if s.focusHighlights {
			// An option of a select: the select highlights it as it
			// builds it, and Enter or a press chooses it.
			s.focusAsked = true
			break
		}
		if s.flags&flagChoosable != 0 {
			// A row of a list choosing rows: its list takes the focus for
			// it, choosing it.
			s.click(0)
			break
		}
		rt.focusOn(s)
	case platform.AccessScrollIntoView:
		// The rows of lists around it scroll into view by their place, as
		// the next frame may not build them, then it does.
		for row := s; row != nil; row = rt.states[row.parent] {
			if l := rt.states[row.parent]; l != nil && l.list != nil {
				if r, ok := l.list.rows[row.id]; ok {
					l.list.ScrollIntoView(r.row)
				}
			}
			if row.parent == 0 {
				break
			}
		}
		rt.reveal(s.id)
	case platform.AccessIncrement, platform.AccessDecrement:
		if s.ignoreInput {
			return // the keys would go to the element with the focus
		}
		rt.focusOn(s)
		// Sliders step right and left, spin buttons up and down, as do
		// sliders laid out right to left, whose Right goes down.
		up, down := KeyRight, KeyLeft
		if s.role == RoleStepper || s.rtl {
			up, down = KeyUp, KeyDown
		}
		k := up
		if ev.Action == platform.AccessDecrement {
			k = down
		}
		rt.keys = append(rt.keys, keyEvent{key: k})
	case platform.AccessExpand, platform.AccessCollapse:
		// The tree opens or closes the item as it builds it.
		s.expand = 1
		if ev.Action == platform.AccessCollapse {
			s.expand = -1
		}
	case platform.AccessSelectText:
		if ed := s.editor; ed != nil && s.flags&flagEditable != 0 {
			n := ed.buf.n
			ed.editorSelection = editorSelection{anchor: max(0, min(ev.From, n)), caret: max(0, min(ev.To, n))}
			if ed.area != nil {
				ed.area.reveal = true
			}
			break
		}
		if s.input != nil {
			s.input(InputEvent{Kind: InputSelect, Start: ev.From, End: ev.To, Time: rt.now()})
		}
	case platform.AccessSetValue:
		if s.textClient != nil && (s.editor == nil || s.flags&flagEditable == 0) {
			rt.focusOn(s)
			_, span := s.textClient.TextForRange(TextInputRange{End: int(^uint(0) >> 1)})
			s.textClient.ReplaceText(&span, ev.Text)
			break
		}
		if s.editor == nil || s.flags&flagEditable == 0 || s.editor.readOnly {
			return
		}
		rt.focusOn(s)
		s.editor.queue = append(s.editor.queue, editEvent{kind: editCommand, text: "selectAll"}, editEvent{kind: editInsert, text: ev.Text})
	case platform.AccessInsertText:
		if s.textClient != nil && (s.editor == nil || s.flags&flagEditable == 0) {
			rt.focusOn(s)
			s.textClient.ReplaceText(nil, ev.Text)
			break
		}
		if s.editor == nil || s.flags&flagEditable == 0 || s.editor.readOnly {
			return
		}
		rt.focusOn(s)
		s.editor.queue = append(s.editor.queue, editEvent{kind: editInsert, text: ev.Text})
	}
	rt.requestFrame()
}

// focusOn gives an element the keyboard focus, as Tab would, unless it
// ignores input.
func (rt *engine) focusOn(s *state) {
	if s.flags&(flagFocusable|flagEditable) == 0 || s.ignoreInput {
		return
	}
	if rt.focused != s.id {
		rt.focused = s.id
		rt.blinkStart = rt.now()
	}
	rt.focusVisible = true
	rt.reveal(s.id)
}
