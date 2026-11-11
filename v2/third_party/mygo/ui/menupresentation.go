package ui

import "sync"

// MenuPresentation is how a window shows its popup menus
// (Context.SetMenuPresentation): the system's, or drawn in the window.
type MenuPresentation uint8

const (
	// SystemMenus shows popup menus as the system's, as ContextMenu and
	// Menu do by default.
	SystemMenus MenuPresentation = iota
	// PopoverMenus draws popup menus in the window with the theme's look,
	// as PopoverMenu and PopoverMenuButton draw theirs.
	PopoverMenus
)

// SetMenuPresentation sets how the window shows its popup menus: those of
// ContextMenu and Menu, the menus MyGo's widgets build with them, as a
// table's header and cell menus, and the editing menu of text inputs and
// selectable text. With PopoverMenus they draw in the window as
// PopoverMenu draws its menu, with its keys, focus and roles, so that an
// app with a look of its own shows no menu of the desktop's beside its
// own; with SystemMenus, the default, the system shows them. It stays
// until set again. PopoverMenu and PopoverMenuButton draw theirs in the
// window whatever it is. Only the menus MyGo builds follow it: the menu
// bar, the tray's menu and the Dock's stay the system's, and a web view's
// are the web view's own.
//
//	c.SetMenuPresentation(ui.PopoverMenus)
func (c *context) SetMenuPresentation(p MenuPresentation) {
	if p == PopoverMenus {
		linkMenusInWindow.Do(func() {
			menusInWindow.contextMenu, menusInWindow.menuButton = (*node).PopoverMenu, popoverMenuButton
			menusInWindow.openEditMenu, menusInWindow.drawEditMenu = (*engine).openDrawnEditMenu, (*engine).drawEditMenu
		})
	}
	c.rt.menuPresentation = p
}

// menusInWindow are what draws the menus of a window that draws them
// (PopoverMenus): ContextMenu's, Menu's, and the editing menu of text.
// SetMenuPresentation alone sets them, so that apps that never draw their
// menus do not link the drawn menus' code through ContextMenu, Menu or the
// frame.
var (
	menusInWindow struct {
		contextMenu, menuButton func(e *node, build func(m *Menu)) *node
		openEditMenu            func(rt *engine, s *state)
		drawEditMenu            func(rt *engine)
	}
	linkMenusInWindow sync.Once
)

// popoverMenuButton is PopoverMenuButton without options, for Menu's
// buttons in a window that draws its menus.
func popoverMenuButton(e *node, build func(m *Menu)) *node { return e.PopoverMenuButton(build) }

// MenuPresentation returns how the window shows its popup menus
// (SetMenuPresentation).
func (c *context) MenuPresentation() MenuPresentation { return c.rt.menuPresentation }

// drawsMenus reports whether the window draws its popup menus
// (PopoverMenus).
func (rt *engine) drawsMenus() bool { return rt.menuPresentation == PopoverMenus }

// drawsEditMenuOf reports whether the editing menu of the text input or
// selectable text s draws in the window: while it draws its menus, but not
// for the inspector's fields, whose panel keeps its own look over the
// window's overlays.
func (rt *engine) drawsEditMenuOf(s *state) bool {
	return rt.drawsMenus() && !rt.insp.holds(rt, s.id)
}

// openDrawnEditMenu opens the editing menu of the text input or selectable
// text s drawn in the window (PopoverMenus), for the next pass to draw
// (drawEditMenu).
func (rt *engine) openDrawnEditMenu(s *state) {
	d := &drawnMenu{id: s.id, text: s.id, edit: true}
	d.menu.openAt(rt, transformRect(s.world, Rect{s.x, s.y, s.w, s.h}))
	rt.menu.drawn = d
	rt.consumed = true
}

// drawEditMenu draws the editing menu of a text input or selectable text
// that shows in the window, which no element's build draws, as an element
// with PopoverMenu and EditItems would: after the view, in the window's
// content or its overlays, with the theme of the element. The frame calls
// it while such a menu shows. One that went out of the view, or into a
// part that takes no input, as a page going away, is not drawn, which
// closes it.
func (rt *engine) drawEditMenu() {
	d := rt.menu.drawn
	e := rt.c.root.find(d.id)
	if e == nil && rt.c.overlay != nil {
		e = rt.c.overlay.find(d.id)
	}
	if e == nil || e.flags&flagInert != 0 || inert(e) {
		return
	}
	c := e.c
	saved := c.theme
	c.theme = e.theme()
	rt.drawMenu(e, editItems)
	c.theme = saved
}

// editItems builds a menu of the editing items of the text it opened on.
func editItems(m *Menu) { m.EditItems() }

// find returns the element id among e and the elements inside it, or nil.
func (e *node) find(id uint64) *node {
	if e.id == id {
		return e
	}
	for ch := e.first; ch != nil; ch = ch.next {
		if found := ch.find(id); found != nil {
			return found
		}
	}
	return nil
}
