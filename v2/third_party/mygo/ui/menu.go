package ui

import (
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/internal/l10n"
	"github.com/egoist/mygo/internal/platform"
)

// ContextMenu gives the element a context menu, which build fills with
// items. The system shows it where the element is clicked with the
// secondary button, or Control-clicked on macOS, and, while the element or
// one inside it has the focus, below it when the menu key or Shift+F10 is
// pressed. build runs when the menu opens, and again in the frame after an
// item was chosen, where the item's Chosen reports it:
//
//	row.ContextMenu(func(m *ui.Menu) {
//		if m.Item("Rename").Chosen() {
//			app.renaming = i
//		}
//		m.Separator()
//		if m.Item("Delete").Disabled(app.locked).Chosen() {
//			app.delete(i)
//		}
//	})
//
// The innermost element with a context menu gets the click. Text inputs
// and selectable text have one with their editing commands, which
// ContextMenu replaces: on a text input, on a selectable text, or on the
// Selectable container whose text was clicked. EditItems puts those
// commands in a menu of one's own:
//
//	message.Selectable().ContextMenu(func(m *ui.Menu) {
//		if m.Item("Reply").Chosen() {
//			app.reply(msg)
//		}
//		m.Separator()
//		m.EditItems()
//	})
//
// PopoverMenu draws the same menu in the window, with the theme's look, as
// ContextMenu does while the window draws its menus (SetMenuPresentation).
func (e *node) ContextMenu(build func(m *Menu)) *node {
	if e.c.rt.drawsMenus() {
		return menusInWindow.contextMenu(e, build)
	}
	e.flags |= flagContextMenu
	e.buildMenu(false, false, build)
	return e
}

// Menu makes the element a menu button: it opens a menu below it, which
// build fills with items, as the pointer goes down on it, as a pop-up
// button does, and for Enter, Space or Down while it has the focus. It
// takes the focus. build runs when the menu opens, and again in the frame
// after an item was chosen, where the item's Chosen reports it, as for
// ContextMenu. MenuButton is a button with a menu:
//
//	more := ui.ButtonBase(c).Label("More").Padding(4).Radius(6)
//	more.Children(func() { ui.Icon(c, moreIcon).Size(16, 16) })
//	more.Menu(func(m *ui.Menu) {
//		if m.Item("Duplicate").Chosen() {
//			app.duplicate()
//		}
//	})
//
// PopoverMenuButton draws the same menu in the window, with the theme's
// look, as Menu does while the window draws its menus
// (SetMenuPresentation).
func (e *node) Menu(build func(m *Menu)) *node {
	if e.c.rt.drawsMenus() {
		return menusInWindow.menuButton(e, build)
	}
	e.flags |= flagMenuButton | flagClickable | flagFocusable
	if e.role == RoleAuto || e.role == RoleButton {
		e.role = RoleMenuButton
	}
	e.buildMenu(true, false, build)
	return e
}

// buildMenu builds the element's menu, its menu button's or its context
// menu, as it opens, and again for the item chosen from it. A menu drawn
// in the window (PopoverMenu, PopoverMenuButton) only opens here, or
// closes as its button is pressed again: drawMenu builds it.
func (e *node) buildMenu(button, drawn bool, build func(m *Menu)) {
	rt := e.c.rt
	mr := &rt.menu
	switch {
	case mr.asked == e.id && mr.button == button:
		mr.asked = 0
		if e.disabled() {
			break
		}
		if d := mr.drawn; drawn && mr.below && d != nil && d.id == e.id && d.menu.below {
			// The button pressed again while its menu shows.
			mr.drawn = nil
			break
		}
		if drawn {
			rt.openDrawnMenu(e)
			break
		}
		run := &menuRun{collect: true}
		pm := &platform.Menu{}
		build(&Menu{run: run, menu: pm, rt: rt, text: rt.states[mr.text]})
		var commands []string
		if slices.ContainsFunc(run.commands, func(c string) bool { return c != "" }) {
			commands = run.commands
		}
		rt.openMenu(e.id, button, pm, run.labels, run.keys, commands)
	case mr.chosen == e.id && mr.chosenButton == button:
		mr.chosen = 0
		rt.menuChoice(build, mr.choice, mr.label, mr.chosenKey, mr.chosenText)
	}
}

// menuChoice runs build for the item chosen from its menu, at place
// choice and labeled label, or with the ID key when it has one
// (MenuItem.ID), whose Chosen then reports it; text is the text the menu
// opened on.
func (rt *engine) menuChoice(build func(m *Menu), choice int, label, key string, text uint64) {
	run := &menuRun{choice: choice, label: label, key: key}
	build(&Menu{run: run, rt: rt, text: rt.states[text]})
	if run.chosen {
		// The choice may change what was built before: build again.
		rt.consumed = true
	}
}

// Menu is a menu being built, by the function given to ContextMenu,
// PopoverMenu, Element.Menu or PopoverMenuButton. Its items show in the
// order they are added.
type Menu struct {
	run  *menuRun
	menu *platform.Menu // what the menu shows, when it opens
	rt   *engine
	// text is the text input or selectable text the menu opened on, whose
	// editing items EditItems adds.
	text *state
}

// menuRun is a run of a context menu's function: the one collecting what
// the menu shows, or the one after an item was chosen, which finds it by
// its ID (MenuItem.ID), or else by its place among the items and its
// label.
type menuRun struct {
	collect bool
	seq     int
	labels  []string // the items' labels by place, when collecting
	// keys are the items' IDs by place, "" for those without one, and
	// items the items, when collecting.
	keys  []string
	items []*MenuItem
	// commands are the editing commands of EditItems' items by place, ""
	// for the others, when collecting.
	commands []string
	choice   int
	label    string
	key      string
	chosen   bool // an item reported the choice
	// highlighted are the items of a menu drawn in the window that are
	// highlighted, as its last pass built them (Highlighted).
	highlighted []menuRef
	// widths are the widths MaxWidth gave the menu and its submenus, when
	// collecting.
	widths map[*platform.Menu]float32
}

// menuRef identifies an item of a menu across runs of its function: by
// its ID when it has one, else by its place and its label.
type menuRef struct {
	seq   int
	label string
	key   string
}

// is reports whether ref is the item it.
func (ref menuRef) is(it *MenuItem) bool {
	if ref.key != "" || it.key != "" {
		return ref.key == it.key
	}
	return ref.seq == it.seq && ref.label == it.label
}

// MenuItem is an item of a Menu. Its methods set it up and return it, for
// chaining, and Chosen reports the choice.
type MenuItem struct {
	run      *menuRun
	seq      int
	label    string
	key      string
	disabled bool
	sub      bool
	p        *platform.MenuItem // when the menu opens
	// What a menu drawn in the window shows of it beyond p (Danger,
	// Trailing, Content).
	danger   bool
	trailing string
	content  func(MenuItemContent)
	// drawn is set while a drawn menu builds its Content, with hi its
	// highlight then.
	drawn, hi bool
}

func (m *Menu) add(label string, typ platform.MenuItemType) *MenuItem {
	r := m.run
	r.seq++
	it := &MenuItem{run: r, seq: r.seq, label: label}
	if r.collect {
		it.p = &platform.MenuItem{ID: r.seq, Label: label, Type: typ, Enabled: true, Visible: true}
		m.menu.Items = append(m.menu.Items, it.p)
		r.labels = append(r.labels, label)
		r.keys = append(r.keys, "")
		r.items = append(r.items, it)
		r.commands = append(r.commands, "")
	}
	return it
}

// EditItems adds the editing items of the text the menu opened on, as its
// menu has them without ContextMenu: Copy and Select All for selectable
// text, the platform's text field items for a text input. Choosing one does
// its command on the text, without reaching the menu's function. An element
// within text, as a link in a paragraph, opens its menu on that text. It
// adds nothing to a menu that opened on no text.
func (m *Menu) EditItems() {
	if m.text == nil || m.text.editor == nil {
		return
	}
	for _, c := range m.rt.editCommands(m.text) {
		if c.name == "" {
			m.Separator()
			continue
		}
		it := m.Item(c.label).Disabled(!c.on)
		if m.run.collect {
			m.run.commands[it.seq-1] = c.name
		}
	}
}

// Item adds an item showing label.
func (m *Menu) Item(label string) *MenuItem { return m.add(label, platform.MenuItemNormal) }

// Separator adds a line between groups of items.
func (m *Menu) Separator() { m.add("", platform.MenuItemSeparator) }

// Submenu adds an item showing label that opens a submenu, which build
// fills as ContextMenu's function fills the menu.
func (m *Menu) Submenu(label string, build func(m *Menu)) *MenuItem {
	it := m.add(label, platform.MenuItemSubmenu)
	it.sub = true
	sub := &Menu{run: m.run}
	if it.p != nil {
		it.p.Submenu = &platform.Menu{}
		sub.menu = it.p.Submenu
	}
	build(sub)
	return it
}

// MaxWidth keeps the menu, as PopoverMenu and PopoverMenuButton draw it,
// at most w DIPs wide: labels, subtitles and Trailing texts too long for
// it end with an ellipsis, while shortcuts, icons and marks keep their
// room. Its submenus take the same width unless their function sets one
// of its own; 0 lifts it, and a drawn menu is never narrower than its
// least width, 45 units of the theme's Spacing. Assistive technology,
// Tester.Menu and the letters typed to go to an item still see each
// label whole, and a Tooltip can show it to the pointer. The system's
// menus take no width from it.
//
//	ui.PopoverMenuButton(c, "Recent", func(m *ui.Menu) {
//		m.MaxWidth(320)
//		for _, f := range app.recent {
//			if m.Item(f.Path).ID(f.Path).Tooltip(f.Path).Chosen() {
//				app.open(f)
//			}
//		}
//	})
func (m *Menu) MaxWidth(w float32) *Menu {
	if r := m.run; r.collect && m.menu != nil {
		if r.widths == nil {
			r.widths = map[*platform.Menu]float32{}
		}
		r.widths[m.menu] = max(w, 0)
	}
	return m
}

// Disabled shows the item grayed out when d is true: it cannot be chosen.
func (it *MenuItem) Disabled(d bool) *MenuItem {
	it.disabled = d
	if it.p != nil {
		it.p.Enabled = !d
	}
	return it
}

// Checked shows a check mark by the item when on is true, as for a setting
// that choosing the item toggles.
func (it *MenuItem) Checked(on bool) *MenuItem {
	if it.p != nil && !it.sub {
		it.p.Type, it.p.Checked = platform.MenuItemCheckbox, on
	}
	return it
}

// Radio shows the item as one of a group choosing one setting, as the
// one chosen when on is true:
//
//	for _, by := range []string{"Name", "Date", "Size"} {
//		if m.Item(by).Radio(app.sort == by).Chosen() {
//			app.sort = by
//		}
//	}
//
// Chosen reports the choice; the view marks the item chosen as it builds
// the menu again.
func (it *MenuItem) Radio(on bool) *MenuItem {
	if it.p != nil && !it.sub {
		it.p.Type, it.p.Checked = platform.MenuItemRadio, on
	}
	return it
}

// MenuIcon is the image of a menu item, as mygo.NewMenuIcon makes it of a
// PNG image, and mygo.NewMenuIconSVG of an SVG document.
type MenuIcon interface {
	PlatformMenuIcon() *platform.MenuIcon
}

// Icon shows an image beside the item's label, one mygo.NewMenuIcon or
// mygo.NewMenuIconSVG made: make each once, not as the menu builds, and
// give it to the items that show it. A template image takes the color of
// the item's text, or its IconColor in the menus MyGo draws. nil shows
// none. The system's menus show it as package mygo's items do (see
// NewMenuIcon), and browsers show none; PopoverMenu draws it on every
// platform.
//
//	if m.Item("Archive").Icon(archiveIcon).Chosen() {
//		app.archive(msg)
//	}
func (it *MenuItem) Icon(icon MenuIcon) *MenuItem {
	if it.p != nil && icon != nil {
		it.p.Icon = icon.PlatformMenuIcon()
	}
	return it
}

// IconColor draws the item's template icon (Icon) in c rather than in the
// color of its text, as a red trash can by a Delete item, or a status's
// color by its name. An icon in its own colors keeps them. While the item
// is highlighted, its icon takes the highlight's text color, as its label
// does, and a disabled item's is dimmed as its label is.
//
// Only the menus MyGo draws show it: PopoverMenu's and PopoverMenuButton's,
// and ContextMenu's and Menu's in a window that draws its menus
// (PopoverMenus). The system's menus tint template icons themselves, in
// the color of their text, and leave it out.
//
//	if m.Item("Delete").Icon(trashIcon).IconColor(t.Danger).Chosen() {
//		app.delete(msg)
//	}
func (it *MenuItem) IconColor(c Color) *MenuItem {
	if it.p != nil {
		s := c.SRGB()
		it.p.IconColor = &platform.Color{R: s.R, G: s.G, B: s.B, A: s.A}
	}
	return it
}

// Tooltip shows s as the pointer rests on the item, as Element.Tooltip
// does, to say what it does. The system's menus show it on macOS and Linux,
// as package mygo's items' ToolTip, and those of Windows and browsers show
// none; PopoverMenu shows it on every platform.
//
//	m.Item("Run").Shortcut(ui.Cmd, ui.KeyEnter).Tooltip("Runs the query under the caret")
func (it *MenuItem) Tooltip(s string) *MenuItem {
	if it.p != nil {
		it.p.ToolTip = s
	}
	return it
}

// Shortcut shows a key with modifiers by the item, as the shortcut that
// does the same; the view handles the key itself, as with
// Context.Shortcut.
func (it *MenuItem) Shortcut(mods Modifiers, key Key) *MenuItem {
	if it.p != nil {
		it.p.Accelerator = accelerator(mods, key)
	}
	return it
}

// Subtitle shows s on a second, smaller line under the item's label, as
// package mygo's MenuItem.Subtitle does: PopoverMenu draws it so on every
// platform, in the color of the shortcuts, and the system's menus show it
// as they show the subtitles of package mygo's items, under the label on
// macOS 14 and in browsers, after it elsewhere ("Label — Subtitle").
// Assistive technology reads it as the item's description, before its
// Tooltip.
//
//	m.Item("Back Up Now").Subtitle("Last backup: 3 minutes ago")
func (it *MenuItem) Subtitle(s string) *MenuItem {
	if it.p != nil {
		it.p.Subtitle = s
	}
	return it
}

// ID gives the item an identity of its own, which Chosen and Highlighted
// match instead of the item's place and label: a label that changes while
// the menu shows, as a time or a count, or items before it that come and
// go, then keep the choice on the item. Give the items of a menu and its
// submenus different IDs; "" is none. In a drawn menu, ID also keys the
// item's row, whose highlight then stays on it as items around it come
// and go.
//
//	if m.Item(fmt.Sprintf("Sync %d Files", app.pending)).ID("sync").Chosen() {
//		app.sync()
//	}
func (it *MenuItem) ID(id string) *MenuItem {
	it.key = id
	if r := it.run; r.collect {
		r.keys[it.seq-1] = id
	}
	return it
}

// Danger shows the item in the theme's Danger color, as one that deletes
// or discards: its label, and its mark and template icon. Highlighted, it
// shows the theme's highlight, as the other items do, with its label in
// the Danger color where that is legible on the highlight, else in the
// highlight's text color (Theme.AccentText). The color alone tells no one
// using assistive technology: let the label say what the item does, as
// "Delete…". PopoverMenu draws it so; the system's menus show the item as
// the others.
//
//	if m.Item("Delete…").Danger().Chosen() {
//		app.confirmDelete()
//	}
func (it *MenuItem) Danger() *MenuItem {
	it.danger = true
	return it
}

// Trailing shows s at the end of the item, before its shortcut, in the
// color of the shortcuts, as a value beside a name: a file's size, a
// count. Assistive technology reads it as part of the item's description.
// PopoverMenu draws it; the system's menus show the label alone.
//
//	m.Item("Image.png").Trailing("2.4 MB")
func (it *MenuItem) Trailing(s string) *MenuItem {
	it.trailing = s
	return it
}

// Content builds what the item shows in place of its label and subtitle,
// as PopoverMenu and PopoverMenuButton draw it: fn builds the children of
// a column between the item's mark and icon and its Trailing text and
// shortcut, as the item shows, highlighted or not (MenuItemContent). Text
// in it takes the color of the item's label. The item keeps its label:
// assistive technology reads it as the item's name, the letters typed go
// to it, and the system's menus show it, without the content.
//
//	m.Item("Download").Trailing("40%").Content(func(x ui.MenuItemContent) {
//		ui.Text(x.Context, "Download")
//		ui.Progress(x.Context, app.progress)
//	})
//
// The content is part of the item, which keeps the pointer and the keys
// of the menu: a press on it chooses the item, closing the menu, and the
// pointer highlights it. A control in it takes its own presses, as a text
// input that a click gives the focus to, as a filter of the items after
// it: it keeps the focus as the pointer moves over its item, and has the
// keys it handles, the text typed with Space, and Enter, Home and End for
// a text input. Up and Down still move the highlight, giving the menu the
// focus back, the pointer moving onto another item does too, and Escape
// closes the menu. The item stays one the keys and the pointer stop at,
// and Enter on it, or a press beside the control, chooses it; a menu of
// one's own (MenuBase, ContextMenuBase) can put such a field outside its
// items.
func (it *MenuItem) Content(fn func(content MenuItemContent)) *MenuItem {
	it.content = fn
	return it
}

// MenuItemContent is what an item's Content builds with: the context, and
// how the item shows.
type MenuItemContent struct {
	Context *Context
	// Highlighted is set while the pointer or the keys are on the item,
	// Disabled while it cannot be chosen.
	Highlighted, Disabled bool
	// Text is the color of the item's label as it shows, Muted that of its
	// shortcut and Trailing text.
	Text, Muted Color
}

// Highlighted reports whether the item is highlighted in the menu drawn
// in the window (PopoverMenu, PopoverMenuButton), as the pointer or the
// keys are on it or on an item of the submenu it opens: the menu's
// function can show, as it builds, what goes with the item the user is
// on, as a preview of a theme:
//
//	for _, th := range themes {
//		item := m.Item(th.Name).ID(th.Name)
//		if item.Highlighted() {
//			app.preview = th
//		}
//		if item.Chosen() {
//			app.theme = th
//		}
//	}
//
// The function sees the highlight of the menu's last pass, and builds
// again when it moves; in Content, it is the item's own as it builds.
// Call ID first, and give the items IDs when the items the function
// builds change with the highlight. It reports false for the system's
// menus, which tell no highlight, and as an item is chosen.
func (it *MenuItem) Highlighted() bool {
	if it.drawn {
		return it.hi
	}
	for _, ref := range it.run.highlighted {
		if ref.is(it) {
			return true
		}
	}
	return false
}

// Chosen reports whether the item was chosen, in the frame after the user
// chose it: the item with the ID of the one chosen, when it had one (ID),
// else the one at its place with its label.
func (it *MenuItem) Chosen() bool {
	r := it.run
	if r.collect || it.disabled || it.sub || !(menuRef{seq: r.choice, label: r.label, key: r.key}).is(it) {
		return false
	}
	r.chosen = true
	return true
}

// menuState is what the engine knows of context menus: the one asked for,
// which the next frame builds, the one built, which shows after the frame,
// and the item chosen from the one shown.
type menuState struct {
	asked uint64 // the element whose menu opens, at (x, y)
	x, y  float32
	// text is the text input or selectable text the menu opens on, which
	// may be inside the element with the menu, and chosenText the one the
	// item chosen was from.
	text, chosenText uint64
	// button is set when the menu asked for is a menu button's, and
	// chosenButton when the item chosen is from one; key when it opens for
	// the menu key or Shift+F10, or a menu button's for its keys; below
	// when a menu button's opens below the button itself, rather than
	// from a toolbar's overflow menu.
	button, chosenButton, key, last, below bool
	// release is the element whose menu opens when the secondary button
	// goes up, as on Windows, on the text releaseText.
	release, releaseText uint64
	pending              *shownMenu
	chosen               uint64
	choice               int
	label, chosenKey     string
	// later is the menu button whose menu the view asked for, as a
	// toolbar's overflow menu does, to show at (laterX, laterY): the next
	// pass builds it, as one asked for before the pass.
	later          uint64
	laterX, laterY float32
	// drawn is the menu drawn in the window that shows (PopoverMenu,
	// PopoverMenuButton), nil while none does.
	drawn *drawnMenu
}

// shownMenu is a context menu built for the element id. commands are the
// editing commands of a text input's items, nil for a view's menu.
type shownMenu struct {
	id       uint64
	button   bool
	menu     *platform.Menu
	labels   []string
	keys     []string // the items' IDs by place (MenuItem.ID), nil for a text's own menu
	commands []string
	x, y     float32
	// text is the text the menu opened on, which takes its commands.
	text uint64
}

// textMenu returns the element whose context menu opens on the text input
// or selectable text s: s, or the Selectable container whose text it is
// when the container has a ContextMenu and s has none.
func (rt *engine) textMenu(s *state) *state {
	if s.flags&flagContextMenu == 0 && s.textScope != 0 {
		if c := rt.states[s.textScope]; c != nil && c.flags&flagContextMenu != 0 && c.flags&flagDisabled == 0 {
			return c
		}
	}
	return s
}

// menuTarget returns the innermost element of chain with a context menu, a
// text input's or the view's, or nil.
func (rt *engine) menuTarget(chain []uint64) *state {
	for _, id := range chain {
		s := rt.states[id]
		if s == nil || s.flags&flagDisabled != 0 {
			continue
		}
		if s.flags&(flagContextMenu|flagEditable|flagSelectable) != 0 {
			return s
		}
		if s.flags&flagUnselectable != 0 {
			return nil
		}
	}
	return nil
}

// textAround returns the selectable text or text input around s in chain,
// whose context menu s replaces, as a paragraph's for a link in it, or nil.
func (rt *engine) textAround(chain []uint64, s *state) *state {
	for _, id := range chain[slices.Index(chain, s.id)+1:] {
		if t := rt.states[id]; t != nil && t.flags&(flagEditable|flagSelectable) != 0 && t.editor != nil {
			return t
		}
	}
	return nil
}

// askMenu has the next frame build the context menu of s, or with button
// its menu button's, to show at (x, y): where it was clicked, or below the
// focus.
func (rt *engine) askMenu(s *state, x, y float32, button bool) {
	mr := &rt.menu
	mr.asked, mr.x, mr.y, mr.button, mr.text, mr.key, mr.last, mr.below = s.id, x, y, button, 0, false, false, false
	// The menu takes the press: the release goes to it.
	if rt.pressed != nil {
		rt.pressed.pressed = false
		rt.pressed = nil
	}
	rt.requestFrame()
}

// menuPress handles a press of the secondary button over chain: when an
// element there has a context menu, the menu opens, as the button goes
// down on macOS and Linux and up on Windows, and menuPress reports true.
// A text input takes the focus, and the caret unless the press is in the
// selection.
func (rt *engine) menuPress(chain []uint64, x, y float32) bool {
	s := rt.menuTarget(chain)
	if s == nil {
		return false
	}
	if rt.listMenu != nil {
		rt.listMenu(rt, chain, x, y)
	}
	if s.flags&flagSelectable != 0 && s.editor == nil {
		if p := rt.textSelectionAt(s.id, x, y); p.id != 0 {
			s = rt.states[p.id]
		}
	}
	// A right-click in any selected paragraph keeps the whole selection.
	if !rt.textSelectionMenu(s, x, y) && s.flags&(flagEditable|flagSelectable) != 0 && s.editor != nil {
		if rt.focused != s.id {
			rt.focused = s.id
			rt.focusVisible = false
		}
		rt.blinkStart = rt.now()
		if ed := s.editor; ed.laidOut() {
			lx, ly := s.local(x, y)
			i := ed.hit(lx, ly)
			if a, b := ed.selection(); a == b || i < a || i > b {
				ed.commitCompose()
				ed.move(i, false)
			}
		}
	}
	text := uint64(0)
	if s.editor != nil {
		text = s.id
		s = rt.textMenu(s)
	} else if t := rt.textAround(chain, s); t != nil {
		text = t.id
	}
	if runtime.GOOS == "windows" {
		rt.menu.release, rt.menu.releaseText = s.id, text
		rt.requestFrame()
		return true
	}
	rt.askMenu(s, x, y, false)
	rt.menu.text = text
	return true
}

// menuRelease opens the menu menuPress left for the release of the
// button, where it is released, and reports whether there was one.
func (rt *engine) menuRelease() bool {
	id := rt.menu.release
	if id == 0 {
		return false
	}
	text := rt.menu.releaseText
	rt.menu.release, rt.menu.releaseText = 0, 0
	if s := rt.states[id]; s != nil {
		rt.askMenu(s, rt.pointerX, rt.pointerY, false)
		rt.menu.text = text
	}
	return true
}

// menuKey opens the context menu of the focused element, or of the
// innermost around it with one, for the menu key or Shift+F10: below a
// text input's caret, or the element's top. It reports whether there was
// one.
func (rt *engine) menuKey() bool {
	s := rt.menuTarget(rt.focusChain())
	if s == nil {
		return false
	}
	x, y := s.vx, s.vy+min(s.vh, 32)
	if s.rtl {
		x += s.vw
	}
	text := uint64(0)
	if s.flags&(flagEditable|flagSelectable) != 0 && s.editor != nil {
		r := transformRect(s.world, s.editor.caretRect(s))
		x, y = r.X, r.Y+r.H
		text = s.id
		s = rt.textMenu(s)
	} else if t := rt.textAround(rt.focusChain(), s); t != nil {
		text = t.id
	}
	rt.askMenu(s, x, y, false)
	rt.menu.text, rt.menu.key = text, true
	return true
}

// askMenuLater has the next pass build the menu of menu button s, to show
// at (x, y), for the view asking for it after s was built.
func (rt *engine) askMenuLater(s *state, x, y float32) {
	mr := &rt.menu
	mr.later, mr.laterX, mr.laterY = s.id, x, y
	rt.consumed = true
}

// openMenuButton has the next frame build the menu of menu button s, to
// show below it; key is set when the keyboard opens it, or assistive
// technology.
func (rt *engine) openMenuButton(s *state, key bool) {
	x, y := s.menuCorner()
	rt.askMenu(s, x, y, true)
	rt.menu.key, rt.menu.below = key, true
}

// openMenuButtonOnLast opens the menu of menu button s from the keyboard
// on its last item that can be chosen, as the Up key does.
func (rt *engine) openMenuButtonOnLast(s *state) {
	rt.openMenuButton(s, true)
	rt.menu.last = true
}

// menuCorner returns where the menu of a button goes: at its bottom left
// corner, or its bottom right one right to left, where the menu opens to
// the left (platform.Menu.RightToLeft).
func (s *state) menuCorner() (x, y float32) {
	if s.rtl {
		return s.vx + s.vw, s.vy + s.vh
	}
	return s.vx, s.vy + s.vh
}

// openMenu shows the context menu built for the element id once the frame
// is done.
func (rt *engine) openMenu(id uint64, button bool, pm *platform.Menu, labels, keys, commands []string) {
	if len(pm.Items) == 0 {
		return
	}
	mr := &rt.menu
	if s := rt.states[id]; s != nil && s.rtl {
		pm.RightToLeft = true
	}
	mr.pending = &shownMenu{id: id, button: button, menu: pm, labels: labels, keys: keys, commands: commands, x: mr.x, y: mr.y, text: mr.text}
}

// resolveMenu opens the menu asked for that no ContextMenu built, after a
// pass of the view: a text input's, with the editing commands.
func (rt *engine) resolveMenu() {
	mr := &rt.menu
	if d := mr.drawn; d != nil && (d.built != rt.frame || d.pass != rt.pass) {
		// The element whose menu it is was not built: the menu goes with it.
		mr.drawn = nil
	}
	id := mr.asked
	if id == 0 {
		return
	}
	mr.asked = 0
	if s := rt.states[id]; s != nil && s.seen == rt.frame && s.editor != nil && !mr.button {
		if rt.drawsEditMenuOf(s) {
			menusInWindow.openEditMenu(rt, s)
		} else {
			rt.editMenu(s)
		}
	}
}

// showMenu hands the menu built in the frame to the host, which shows it
// after the event being handled.
func (rt *engine) showMenu() {
	p := rt.menu.pending
	if p == nil {
		return
	}
	rt.menu.pending = nil
	z := rt.zoomFactor()
	rt.host.popupMenu(p.menu, p.x*z, p.y*z, func(id int) { rt.menuChosen(p, id) })
}

// menuChosen takes the item id chosen from the menu p: the next frame
// reports it to ContextMenu's function, or the text input does the
// command.
func (rt *engine) menuChosen(p *shownMenu, id int) {
	if id < 1 || id > len(p.labels) {
		return
	}
	if p.commands != nil && p.commands[id-1] != "" {
		// An editing command, of the text's own menu or of EditItems.
		target := p.text
		if target == 0 {
			target = p.id
		}
		rt.menuCommand(target, p.commands[id-1])
	} else {
		mr := &rt.menu
		mr.chosen, mr.chosenButton, mr.choice, mr.label, mr.chosenKey, mr.chosenText = p.id, p.button, id, p.labels[id-1], "", p.text
		if p.keys != nil {
			mr.chosenKey = p.keys[id-1]
		}
	}
	rt.requestFrame()
}

// menuCommand does the editing command name, chosen from a menu, on the
// text input or selectable text id.
func (rt *engine) menuCommand(id uint64, name string) {
	if s := rt.states[id]; s != nil && s.editor != nil {
		if !rt.textSelectionCommand(s, name) {
			s.editor.queue = append(s.editor.queue, editEvent{kind: editCommand, text: name})
		}
		rt.blinkStart = rt.now()
	}
}

// editItem is an item of a text's editing menu: a separator without a
// name.
type editItem struct {
	label, name string
	on          bool
}

// editMenu opens the context menu of a text input or selectable text, with
// the editing commands its platform's text fields have in theirs.
func (rt *engine) editMenu(s *state) {
	commands := rt.editCommands(s)
	pm := &platform.Menu{}
	labels := make([]string, len(commands))
	names := make([]string, len(commands))
	for i, c := range commands {
		it := &platform.MenuItem{ID: i + 1, Label: c.label, Enabled: c.on, Visible: true}
		if c.name == "" {
			it.Type = platform.MenuItemSeparator
		}
		pm.Items = append(pm.Items, it)
		labels[i], names[i] = c.label, c.name
	}
	rt.openMenu(s.id, false, pm, labels, nil, names)
}

// editCommands are the items of the editing menu of s: Copy and Select All
// for selectable text, the platform's text field items for a text input.
func (rt *engine) editCommands(s *state) []editItem {
	type command = editItem
	texts := l10n.In(rt.lang)
	var (
		undo      = command{texts.Undo(), "undo", rt.editCommandOn(s, "undo")}
		cut       = command{texts.Cut(), "cut", rt.editCommandOn(s, "cut")}
		copyText  = command{texts.Copy(), "copy", rt.editCommandOn(s, "copy")}
		paste     = command{texts.Paste(), "paste", rt.editCommandOn(s, "paste")}
		del       = command{texts.Delete(), "delete", rt.editCommandOn(s, "delete")}
		selectAll = command{texts.SelectAll(), "selectAll", rt.editCommandOn(s, "selectAll")}
		separator = command{}
		commands  []command
	)
	switch {
	case s.editor.readOnly:
		commands = []command{copyText, separator, selectAll}
	case runtime.GOOS == "darwin":
		commands = []command{cut, copyText, paste, separator, selectAll}
	case runtime.GOOS == "windows":
		commands = []command{undo, separator, cut, copyText, paste, del, separator, selectAll}
	default:
		commands = []command{cut, copyText, paste, del, separator, selectAll}
	}
	return commands
}

// editCommandOn reports whether the editing command name would act on the
// text input or selectable text s, as its editor does it.
func (rt *engine) editCommandOn(s *state, name string) bool {
	ed := s.editor
	switch {
	case s.flags&flagDisabled != 0 && (s.textScope != 0 || s.flags&flagEditable != 0),
		ed.readOnly && name != "copy" && name != "selectAll",
		ed.password && (name == "cut" || name == "copy"):
		return false
	}
	if len(ed.queue) > 0 {
		// The command goes behind the input the next frame applies, which
		// changes what it acts on.
		return true
	}
	a, b := ed.selection()
	selected := a != b
	if s.textScope != 0 && rt.selection.scope == s.textScope {
		_, _, selected = rt.textSelectionBounds()
	}
	switch name {
	case "undo":
		return len(ed.undo) > 0
	case "redo":
		return len(ed.redo) > 0
	case "cut", "copy":
		return selected
	case "paste", "pasteAndMatchStyle":
		return true
	case "delete":
		return selected
	case "selectAll":
		if s.textScope != 0 {
			// The selection holds the newlines between paragraphs too.
			paragraphs := 0
			for _, p := range rt.texts {
				if p.textScope == s.textScope {
					if p.editor.buf.n > 0 {
						return true
					}
					paragraphs++
				}
			}
			return paragraphs > 1
		}
		return ed.buf.n > 0
	}
	return false
}

// accelerator writes a key with modifiers as menus take it, as in
// "Super+Shift+Z".
func accelerator(mods Modifiers, key Key) string {
	name := keyName(key)
	if name == "" {
		return ""
	}
	var b strings.Builder
	for _, m := range []struct {
		mod  Modifiers
		name string
	}{{Super, "Super"}, {Ctrl, "Ctrl"}, {Alt, "Alt"}, {Shift, "Shift"}} {
		if mods&m.mod != 0 {
			b.WriteString(m.name)
			b.WriteByte('+')
		}
	}
	b.WriteString(name)
	return b.String()
}

func keyName(k Key) string {
	switch {
	case k >= KeyA && k <= KeyZ:
		return string(rune('a' + k - KeyA))
	case k >= Key0 && k <= Key9:
		return string(rune('0' + k - Key0))
	case k >= KeyF1 && k <= KeyF12:
		return "F" + strconv.Itoa(int(k-KeyF1)+1)
	}
	switch k {
	case KeyEnter:
		return "Enter"
	case KeyEscape:
		return "Escape"
	case KeyBackspace:
		return "Backspace"
	case KeyTab:
		return "Tab"
	case KeySpace:
		return "Space"
	case KeyDelete:
		return "Delete"
	case KeyInsert:
		return "Insert"
	case KeyHome:
		return "Home"
	case KeyEnd:
		return "End"
	case KeyPageUp:
		return "PageUp"
	case KeyPageDown:
		return "PageDown"
	case KeyLeft:
		return "Left"
	case KeyRight:
		return "Right"
	case KeyUp:
		return "Up"
	case KeyDown:
		return "Down"
	case KeyMinus:
		return "-"
	case KeyEqual:
		return "="
	case KeyComma:
		return ","
	case KeyPeriod:
		return "."
	case KeySlash:
		return "/"
	case KeySemicolon:
		return ";"
	case KeyQuote:
		return "'"
	case KeyBracketLeft:
		return "["
	case KeyBracketRight:
		return "]"
	case KeyBackslash:
		return "\\"
	case KeyBackquote:
		return "`"
	}
	return ""
}
