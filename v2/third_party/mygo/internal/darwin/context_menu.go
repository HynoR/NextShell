//go:build darwin

package darwin

import (
	"strings"

	"github.com/ebitengine/purego/objc"

	"github.com/egoist/mygo/internal/platform"
)

// A page's context menu: WebKit builds its NSMenu and pops it up over the
// web view, which AppKit tells the view as -[NSView willOpenMenu:withEvent:]
// before the menu shows. MyGo describes the menu there, asks the core, and
// changes it in place: WebKit's items, which carry what they do, go back
// where the core put them, and the app's are made as those of its other
// menus. WebKit tells nothing of what was clicked: the core has it from
// the page (bridge.go).

var (
	// contextWindow is the window the context menu shown last opened
	// over, whose window handler hears the app's items chosen, which
	// AppKit sends once the menu closed. contextOwner owns those items,
	// released as the next menu opens.
	contextWindow *window
	contextOwner  int
	// openContextMenu is the context menu being shown, for the tests: one
	// a listener heard of, with items.
	openContextMenu id
)

// contextMenuMethods are the overrides of MyGoWebView that hear of its
// context menus.
func contextMenuMethods() []objc.MethodDef {
	return []objc.MethodDef{
		method("willOpenMenu:withEvent:", func(self id, cmd objc.SEL, menu, ev id) {
			sendSuper(self, "MyGoWebView", cmd, uintptr(menu), uintptr(ev))
			// A listener the core heard of was added after the code was
			// linked: HandlesContextMenu, which takes the listeners' lock,
			// sees showContextMenu set.
			if w := theBackend.byWebView[self]; w != nil && w.h.HandlesContextMenu() && showContextMenu != nil {
				showContextMenu(w, menu, ev)
			}
		}),
		method("didCloseMenu:withEvent:", func(self id, cmd objc.SEL, menu, ev id) {
			sendSuper(self, "MyGoWebView", cmd, uintptr(menu), uintptr(ev))
			if menu == openContextMenu {
				openContextMenu = 0
			}
		}),
	}
}

// showContextMenu is (*window).contextMenu once LinkContextMenus linked
// it.
var showContextMenu func(w *window, menu, ev id)

// LinkContextMenus links the code of pages' context menus, when the app
// first listens to them (Page.OnContextMenu).
func LinkContextMenus() { showContextMenu = (*window).contextMenu }

// webKitMenu reports whether menu is WebKit's context menu, rather than a
// menu of the app's popped up over the web view (Menu.Popup): an item of
// it does what WebKit does, or bears one of its identifiers.
func webKitMenu(menu id) bool {
	forward := sel("forwardContextMenuAction:")
	for i := range sendInt(menu, "numberOfItems") {
		item := send(menu, "itemAtIndex:", uintptr(i))
		if objc.SEL(send(item, "action")) == forward || strings.HasPrefix(itemIdentifier(item), "WKMenuItemIdentifier") {
			return true
		}
	}
	return false
}

func itemIdentifier(item id) string {
	if !respondsTo(item, "identifier") {
		return ""
	}
	return goString(send(item, "identifier"))
}

// contextMenu asks the core about the menu WebKit is about to show, and
// changes it as the core answers.
func (w *window) contextMenu(menu, ev id) {
	if !webKitMenu(menu) {
		return
	}
	var natives []id
	defer func() {
		for _, item := range natives {
			release(item)
		}
	}()
	m := &platform.ContextMenu{Items: describeContextMenu(menu, &natives)}
	// The event that opened the menu: a right or Control-click, at a point
	// of the window, which the page's contextmenu event tells in CSS
	// pixels from the web view's top-left corner.
	if ev != 0 {
		switch sendInt(ev, "type") {
		case 1, 2, 3, 4, 25, 26: // NSEventTypeLeftMouseDown to RightMouseUp, OtherMouseDown and Up
			p := msgPointFromView(w.web, sel("convertPoint:fromView:"), msgPoint(ev, sel("locationInWindow")), 0)
			if !sendBool(w.web, "isFlipped") {
				p.Y = msgRect(w.web, sel("bounds")).Size.Height - p.Y
			}
			m.Pointer, m.X, m.Y = true, p.X, p.Y
		}
	}
	resp := w.h.ContextMenu(m)
	if resp.Default {
		openContextMenu = menu
		return
	}
	if contextOwner != 0 {
		dropOwner(contextOwner)
	}
	contextWindow, contextOwner = w, newOwner()
	c := contextFill{w: w, natives: natives, was: map[int]*platform.MenuItem{}}
	c.index(m.Items)
	send(menu, "removeAllItems")
	if resp.Menu != nil {
		c.fill(menu, resp.Menu.Items)
	}
	if sendInt(menu, "numberOfItems") > 0 {
		openContextMenu = menu
		return
	}
	// An empty menu shows nothing, but AppKit still tracks it, and WebKit
	// pops up no other until it closed: it closes once its tracking began.
	retain(menu)
	theBackend.post(func() {
		send(menu, "cancelTrackingWithoutAnimation")
		release(menu)
	})
}

// describeContextMenu describes WebKit's items of menu and its submenus,
// numbering each, and retains it in natives, which keeps it while the
// menus are emptied and filled again.
func describeContextMenu(menu id, natives *[]id) []*platform.MenuItem {
	n := sendInt(menu, "numberOfItems")
	items := make([]*platform.MenuItem, 0, n)
	for i := range n {
		item := send(menu, "itemAtIndex:", uintptr(i))
		*natives = append(*natives, retain(item))
		it := &platform.MenuItem{Native: len(*natives), Enabled: sendBool(item, "isEnabled"), Visible: !sendBool(item, "isHidden")}
		items = append(items, it)
		if sendBool(item, "isSeparatorItem") {
			it.Type = platform.MenuItemSeparator
			continue
		}
		it.Label = goString(send(item, "title"))
		it.Name = contextItemName(item)
		if sendInt(item, "state") == 1 { // NSControlStateValueOn
			it.Type, it.Checked = platform.MenuItemCheckbox, true
		}
		if sub := send(item, "submenu"); sub != 0 {
			it.Type, it.Submenu = platform.MenuItemSubmenu, &platform.Menu{Items: describeContextMenu(sub, natives)}
		}
	}
	return items
}

// contextIdentifiers are MyGo's names of the items WebKit identifies
// (WKMenuItemIdentifiers.h, macOS 11) that other platforms have too.
var contextIdentifiers = map[string]string{
	"WKMenuItemIdentifierCopy":                  "copy",
	"WKMenuItemIdentifierCopyImage":             "copyImage",
	"WKMenuItemIdentifierCopyLink":              "copyLink",
	"WKMenuItemIdentifierCopyMediaLink":         "copyMediaURL",
	"WKMenuItemIdentifierDownloadImage":         "downloadImage",
	"WKMenuItemIdentifierDownloadLinkedFile":    "downloadLink",
	"WKMenuItemIdentifierDownloadMedia":         "downloadMedia",
	"WKMenuItemIdentifierGoBack":                "back",
	"WKMenuItemIdentifierGoForward":             "forward",
	"WKMenuItemIdentifierInspectElement":        "inspect",
	"WKMenuItemIdentifierLookUp":                "lookUp",
	"WKMenuItemIdentifierOpenFrameInNewWindow":  "openFrameInNewWindow",
	"WKMenuItemIdentifierOpenImageInNewWindow":  "openImageInNewWindow",
	"WKMenuItemIdentifierOpenLink":              "openLink",
	"WKMenuItemIdentifierOpenLinkInNewWindow":   "openLinkInNewWindow",
	"WKMenuItemIdentifierOpenMediaInNewWindow":  "openMediaInNewWindow",
	"WKMenuItemIdentifierPaste":                 "paste",
	"WKMenuItemIdentifierReload":                "reload",
	"WKMenuItemIdentifierSearchWeb":             "searchWeb",
	"WKMenuItemIdentifierShowHideMediaControls": "mediaControls",
	"WKMenuItemIdentifierToggleFullScreen":      "mediaFullScreen",
	"WKMenuItemIdentifierShareMenu":             "share",
}

// contextTags name WebKit's items without an identifier by their tags,
// WebCore's ContextMenuAction values, which hold for the first of them.
var contextTags = [...]string{
	1: "openLinkInNewWindow", 2: "downloadLink", 3: "copyLink", 4: "openImageInNewWindow", 5: "downloadImage",
	6: "copyImage", 7: "openFrameInNewWindow", 8: "copy", 9: "back", 10: "forward", 11: "stop", 12: "reload",
	13: "cut", 14: "paste", 15: "spellingGuess", 21: "searchWeb", 22: "lookUp",
}

// contextSelectors name AppKit's items by their actions, as the editing
// items of a text field's menu.
var contextSelectors = map[string]string{
	"cut:": "cut", "copy:": "copy", "paste:": "paste", "pasteAsPlainText:": "pasteAndMatchStyle",
	"delete:": "delete", "selectAll:": "selectAll", "undo:": "undo", "redo:": "redo",
}

// contextItemName names an item of WebKit's menu: MyGo's name for the
// items other platforms have, else WebKit's identifier, "" for an item
// that has none.
func contextItemName(item id) string {
	ident := itemIdentifier(item)
	if name, ok := contextIdentifiers[ident]; ok {
		return name
	}
	// AppKit names an item WebKit gave no identifier by its action, as
	// "forwardContextMenuAction:", which tells nothing.
	if strings.HasPrefix(ident, "WKMenuItemIdentifier") {
		return ident
	}
	action := objc.SEL(send(item, "action"))
	if action == 0 {
		return ""
	}
	if action == sel("forwardContextMenuAction:") {
		if tag := sendInt(item, "tag"); tag > 0 && tag < len(contextTags) {
			return contextTags[tag]
		}
		return ""
	}
	for s, name := range contextSelectors {
		if action == sel(s) {
			return name
		}
	}
	return ""
}

// contextFill fills a context menu as the core answered.
type contextFill struct {
	w       *window
	natives []id
	// was describes WebKit's items as they were, by number.
	was map[int]*platform.MenuItem
}

func (c *contextFill) index(items []*platform.MenuItem) {
	for _, it := range items {
		c.was[it.Native] = it
		if it.Submenu != nil {
			c.index(it.Submenu.Items)
		}
	}
}

// fill adds items to menu, emptied: WebKit's, as the core changed them,
// and the app's.
func (c *contextFill) fill(menu id, items []*platform.MenuItem) {
	for _, it := range items {
		if !it.Visible {
			continue
		}
		if it.Native != 0 && it.Native <= len(c.natives) {
			send(menu, "addItem:", uintptr(c.native(c.natives[it.Native-1], it)))
		} else {
			send(menu, "addItem:", uintptr(c.app(it)))
		}
	}
}

// native gives WebKit's item the label, the state and the submenu the
// core gave it. A submenu left as it was is not filled again, which keeps
// those AppKit fills as they open, as Services.
func (c *contextFill) native(item id, it *platform.MenuItem) id {
	was := c.was[it.Native]
	if it.Type == platform.MenuItemSeparator || was == nil {
		return item
	}
	if it.Label != was.Label {
		send(item, "setTitle:", uintptr(nsString(it.Label)))
	}
	send(item, "setEnabled:", boolArg(it.Enabled))
	if it.Checked != was.Checked {
		send(item, "setState:", boolArg(it.Checked))
	}
	if it.Accelerator != was.Accelerator {
		key, mods := keyEquivalent(it.Accelerator)
		send(item, "setKeyEquivalent:", uintptr(nsString(key)))
		send(item, "setKeyEquivalentModifierMask:", uintptr(mods))
	}
	if sub := send(item, "submenu"); sub != 0 && it.Submenu != nil && was.Submenu != nil && !c.unchanged(it.Submenu.Items, was.Submenu.Items) {
		send(sub, "removeAllItems")
		c.fill(sub, it.Submenu.Items)
	}
	return item
}

// unchanged reports whether a submenu holds WebKit's items as they were.
func (c *contextFill) unchanged(items, was []*platform.MenuItem) bool {
	if len(items) != len(was) {
		return false
	}
	for i, it := range items {
		w := was[i]
		if it.Native == 0 || it.Native != w.Native || !it.Visible || it.Label != w.Label || it.Enabled != w.Enabled ||
			it.Checked != w.Checked || it.Accelerator != w.Accelerator || (it.Submenu == nil) != (w.Submenu == nil) {
			return false
		}
		if it.Submenu != nil && !c.unchanged(it.Submenu.Items, w.Submenu.Items) {
			return false
		}
	}
	return true
}

// app makes an item of the app's, as its other menus' (buildItem), whose
// choice the window's handler hears.
func (c *contextFill) app(it *platform.MenuItem) id {
	if it.Type == platform.MenuItemSeparator {
		item := send(class("NSMenuItem"), "separatorItem")
		track(contextOwner, it.ID, item)
		return item
	}
	plain := *it
	plain.Submenu = nil
	item := theBackend.buildItem(&plain, contextOwner)
	if it.Submenu != nil {
		sub := autorelease(send(send(class("NSMenu"), "alloc"), "initWithTitle:", uintptr(nsString(it.Label))))
		send(sub, "setAutoenablesItems:", 0)
		c.fill(sub, it.Submenu.Items)
		send(item, "setAction:", 0)
		send(item, "setTarget:", 0)
		send(item, "setSubmenu:", uintptr(sub))
	} else if objc.SEL(send(item, "action")) == sel("mygoMenuItemClicked:") {
		send(item, "setAction:", uintptr(sel("mygoContextItemClicked:")))
	}
	return item
}

// contextItemClicked reports an item of the app's chosen from a page's
// context menu to its window.
func contextItemClicked(self id, _ objc.SEL, item id) {
	if w := contextWindow; w != nil && !w.closed {
		w.h.MenuItemClicked(sendInt(item, "tag"))
	}
}
