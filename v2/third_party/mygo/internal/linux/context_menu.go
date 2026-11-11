//go:build linux && (amd64 || arm64)

package linux

import (
	"strconv"
	"strings"
	"sync"

	"github.com/ebitengine/purego"

	"github.com/egoist/mygo/internal/platform"
)

// A page's context menu: the web view's context-menu signal hands over the
// WebKitContextMenu it is about to show, which MyGo changes in place as the
// core answers, and a hit test, which tells what was clicked but the
// selection. The menu's items are WebKit's own, which do what they do
// wherever MyGo puts them; the app's are items of a GAction each, which
// WebKitGTK shows without a shortcut or an icon.

var (
	webkitContextMenuNew                             func() ptr
	webkitContextMenuGetItems                        func(menu ptr) ptr
	webkitContextMenuAppend                          func(menu, item ptr)
	webkitContextMenuRemoveAll                       func(menu ptr)
	webkitContextMenuItemIsSeparator                 func(item ptr) bool
	webkitContextMenuItemGetStockAction              func(item ptr) int32
	webkitContextMenuItemGetSubmenu                  func(item ptr) ptr
	webkitContextMenuItemGetAction                   func(item ptr) ptr // GtkAction, which holds the label
	webkitContextMenuItemGetGAction                  func(item ptr) ptr
	webkitContextMenuItemNewFromStockActionWithLabel func(action int32, label *byte) ptr
	webkitContextMenuItemNewFromGAction              func(action ptr, label *byte, target ptr) ptr
	webkitContextMenuItemNewSeparator                func() ptr
	webkitContextMenuItemNewWithSubmenu              func(label *byte, submenu ptr) ptr
	webkitHitTestResultGetContext                    func(hit ptr) uint32
	webkitHitTestResultGetLinkURI                    func(hit ptr) ptr
	webkitHitTestResultGetLinkLabel                  func(hit ptr) ptr
	webkitHitTestResultGetImageURI                   func(hit ptr) ptr
	webkitHitTestResultGetMediaURI                   func(hit ptr) ptr
	webkitContextMenuActionGetType                   func() uintptr
	gtkActionGetLabel                                func(action ptr) ptr
	gActionGetEnabled                                func(action ptr) bool
	gActionGetState                                  func(action ptr) ptr
	gSimpleActionNew                                 func(name *byte, parameter ptr) ptr
	gSimpleActionNewStateful                         func(name *byte, parameter, state ptr) ptr
	gSimpleActionSetEnabled                          func(action ptr, enabled bool)
	gSimpleActionSetState                            func(action, state ptr)
	gSimpleActionGetType                             func() uintptr
	gTypeClassRef                                    func(typ uintptr) ptr
	gEnumGetValue                                    func(class ptr, value int32) ptr

	cbContextMenu, cbContextActivate ptr
)

// bindContextMenus binds what a context menu with a listener needs, the
// first time one opens, and reports whether WebKitGTK has it all
// (webkit_context_menu_item_get_gaction is from 2.18).
func bindContextMenus() bool {
	bindContextMenusOnce.Do(func() {
		w, o, i := libWebKit, libGObject, libGIO
		contextMenusBound = bind(w, &webkitContextMenuNew, "webkit_context_menu_new") &&
			bind(w, &webkitContextMenuGetItems, "webkit_context_menu_get_items") &&
			bind(w, &webkitContextMenuAppend, "webkit_context_menu_append") &&
			bind(w, &webkitContextMenuRemoveAll, "webkit_context_menu_remove_all") &&
			bind(w, &webkitContextMenuItemIsSeparator, "webkit_context_menu_item_is_separator") &&
			bind(w, &webkitContextMenuItemGetStockAction, "webkit_context_menu_item_get_stock_action") &&
			bind(w, &webkitContextMenuItemGetSubmenu, "webkit_context_menu_item_get_submenu") &&
			bind(w, &webkitContextMenuItemGetAction, "webkit_context_menu_item_get_action") &&
			bind(w, &webkitContextMenuItemGetGAction, "webkit_context_menu_item_get_gaction") &&
			bind(w, &webkitContextMenuItemNewFromStockActionWithLabel, "webkit_context_menu_item_new_from_stock_action_with_label") &&
			bind(w, &webkitContextMenuItemNewFromGAction, "webkit_context_menu_item_new_from_gaction") &&
			bind(w, &webkitContextMenuItemNewSeparator, "webkit_context_menu_item_new_separator") &&
			bind(w, &webkitContextMenuItemNewWithSubmenu, "webkit_context_menu_item_new_with_submenu") &&
			bind(w, &webkitHitTestResultGetContext, "webkit_hit_test_result_get_context") &&
			bind(w, &webkitHitTestResultGetLinkURI, "webkit_hit_test_result_get_link_uri") &&
			bind(w, &webkitHitTestResultGetLinkLabel, "webkit_hit_test_result_get_link_label") &&
			bind(w, &webkitHitTestResultGetImageURI, "webkit_hit_test_result_get_image_uri") &&
			bind(w, &webkitHitTestResultGetMediaURI, "webkit_hit_test_result_get_media_uri") &&
			bind(w, &webkitContextMenuActionGetType, "webkit_context_menu_action_get_type") &&
			bind(libGTK, &gtkActionGetLabel, "gtk_action_get_label") &&
			bind(i, &gActionGetEnabled, "g_action_get_enabled") &&
			bind(i, &gActionGetState, "g_action_get_state") &&
			bind(i, &gSimpleActionNew, "g_simple_action_new") &&
			bind(i, &gSimpleActionNewStateful, "g_simple_action_new_stateful") &&
			bind(i, &gSimpleActionSetEnabled, "g_simple_action_set_enabled") &&
			bind(i, &gSimpleActionSetState, "g_simple_action_set_state") &&
			bind(i, &gSimpleActionGetType, "g_simple_action_get_type") &&
			bind(o, &gTypeClassRef, "g_type_class_ref") &&
			bind(o, &gEnumGetValue, "g_enum_get_value")
	})
	return contextMenusBound
}

var (
	bindContextMenusOnce sync.Once
	contextMenusBound    bool
)

// contextItem is an item of the app's in the context menu shown last:
// its window, and its role.
type contextItem struct {
	window int
	role   string
}

// contextItems are the app's items of the context menu shown last, by
// their IDs, which the activation of their actions carries.
var contextItems = map[int]contextItem{}

// initContextMenuCallbacks makes the callback of the context-menu signal,
// which every web view connects: it calls the code of context menus,
// which an app links as it first listens to them (LinkContextMenus).
func initContextMenuCallbacks() {
	cbContextMenu = purego.NewCallback(func(web, menu, event, hit, data ptr) bool {
		// The listener the core heard of was added after the code was
		// linked: HandlesContextMenu, which takes the listeners' lock,
		// sees showContextMenu set.
		w := theBackend.window(data)
		if w == nil || !w.h.HandlesContextMenu() || showContextMenu == nil {
			return false
		}
		return showContextMenu(w, menu, event, hit)
	})
}

// showContextMenu is (*window).contextMenu once LinkContextMenus linked it.
var (
	showContextMenu  func(w *window, menu, event, hit ptr) bool
	linkContextMenus sync.Once
)

// LinkContextMenus links the code of pages' context menus, when the app
// first listens to them (Page.OnContextMenu).
func LinkContextMenus() {
	linkContextMenus.Do(func() {
		cbContextActivate = purego.NewCallback(func(action, parameter, data ptr) {
			id := int(data)
			c, ok := contextItems[id]
			w := theBackend.window(ptr(c.window))
			if !ok || w == nil {
				return
			}
			if cmd, ok := editCommands[c.role]; ok {
				webkitWebViewExecuteEditingCommand(w.web, cs(cmd))
				return
			}
			w.h.MenuItemClicked(id)
		})
		showContextMenu = (*window).contextMenu
	})
}

// contextMenu asks the core about the menu WebKit is about to show, and
// changes it as it answers. It returns true to show none.
func (w *window) contextMenu(menu, event, hit ptr) bool {
	if !bindContextMenus() {
		return false
	}
	var natives []ptr
	defer func() {
		for _, item := range natives {
			gObjectUnref(item)
		}
	}()
	m := &platform.ContextMenu{Items: describeContextMenu(menu, &natives)}
	// A button's event (GdkEventButton: type 0, x 24, y 32), at a point of
	// the web view's own window; the menu key's has none.
	if event != 0 {
		if t := field[int32](event, 0); t >= 4 && t <= 7 { // GDK_BUTTON_PRESS to GDK_BUTTON_RELEASE
			m.Pointer, m.X, m.Y = true, field[float64](event, 24), field[float64](event, 32)
		}
	}
	if hit != 0 {
		const editable = 1 << 5 // WEBKIT_HIT_TEST_RESULT_CONTEXT_EDITABLE
		m.Target = platform.ContextMenuTarget{
			LinkURL:  goStr(webkitHitTestResultGetLinkURI(hit)),
			LinkText: goStr(webkitHitTestResultGetLinkLabel(hit)),
			ImageURL: goStr(webkitHitTestResultGetImageURI(hit)),
			MediaURL: goStr(webkitHitTestResultGetMediaURI(hit)),
			Editable: webkitHitTestResultGetContext(hit)&editable != 0,
		}
	}
	resp := w.h.ContextMenu(m)
	switch {
	case resp.Default:
		return false
	case resp.Menu == nil:
		return true
	}
	clear(contextItems)
	w.fillContextMenu(menu, resp.Menu.Items, natives)
	return false
}

// describeContextMenu describes WebKit's items of menu and its submenus,
// numbering each, and takes a reference to it in natives, which keeps it
// while the menus are emptied and filled again.
func describeContextMenu(menu ptr, natives *[]ptr) []*platform.MenuItem {
	var items []*platform.MenuItem
	for node := webkitContextMenuGetItems(menu); node != 0; node = field[ptr](node, 8) {
		item := field[ptr](node, 0)
		*natives = append(*natives, gObjectRef(item))
		it := &platform.MenuItem{Native: len(*natives), Enabled: true, Visible: true}
		items = append(items, it)
		if webkitContextMenuItemIsSeparator(item) {
			it.Type = platform.MenuItemSeparator
			continue
		}
		it.Name = contextActionName(webkitContextMenuItemGetStockAction(item))
		if a := webkitContextMenuItemGetAction(item); a != 0 {
			it.Label = withoutMnemonic(goStr(gtkActionGetLabel(a)))
		}
		if a := webkitContextMenuItemGetGAction(item); a != 0 {
			it.Enabled = gActionGetEnabled(a)
			if state := gActionGetState(a); state != 0 {
				if goStr(gVariantGetTypeString(state)) == "b" {
					it.Type, it.Checked = platform.MenuItemCheckbox, gVariantGetBoolean(state)
				}
				gVariantUnref(state)
			}
		}
		if sub := webkitContextMenuItemGetSubmenu(item); sub != 0 {
			it.Type, it.Submenu = platform.MenuItemSubmenu, &platform.Menu{Items: describeContextMenu(sub, natives)}
		}
	}
	return items
}

// fillContextMenu empties menu and fills it with items: WebKit's (natives,
// by number), as the core changed them, and the app's.
func (w *window) fillContextMenu(menu ptr, items []*platform.MenuItem, natives []ptr) {
	webkitContextMenuRemoveAll(menu)
	for _, it := range items {
		if !it.Visible {
			continue
		}
		var item ptr
		if it.Native != 0 && it.Native <= len(natives) {
			item = w.nativeContextItem(natives[it.Native-1], it, natives)
		} else {
			item = w.appContextItem(it, natives)
		}
		webkitContextMenuAppend(menu, item)
	}
}

// nativeContextItem gives WebKit's item the state, the label and the
// submenu the core gave it, and returns it, or the item that takes its
// place.
func (w *window) nativeContextItem(item ptr, it *platform.MenuItem, natives []ptr) ptr {
	if it.Type == platform.MenuItemSeparator {
		return item
	}
	sub := webkitContextMenuItemGetSubmenu(item)
	if sub != 0 && it.Submenu != nil {
		w.fillContextMenu(sub, it.Submenu.Items, natives)
	}
	label := ""
	if a := webkitContextMenuItemGetAction(item); a != 0 {
		label = withoutMnemonic(goStr(gtkActionGetLabel(a)))
	}
	// WebKitGTK renames no item: one of the same stock action, made with
	// the label, takes its place. Its custom items and submenus keep
	// theirs.
	const noAction, custom = 0, 10000
	if action := webkitContextMenuItemGetStockAction(item); it.Label != label && sub == 0 && action != noAction && action != custom {
		item = webkitContextMenuItemNewFromStockActionWithLabel(action, cs(mnemonicLabel(it.Label)))
	}
	if a := webkitContextMenuItemGetGAction(item); a != 0 && gTypeCheckInstanceIsA(a, gSimpleActionGetType()) {
		gSimpleActionSetEnabled(a, it.Enabled)
		if state := gActionGetState(a); state != 0 {
			if goStr(gVariantGetTypeString(state)) == "b" && gVariantGetBoolean(state) != it.Checked {
				gSimpleActionSetState(a, gVariantNewBoolean(it.Checked))
			}
			gVariantUnref(state)
		}
	}
	return item
}

// appContextItem makes an item of the app's: a separator, a submenu, or
// an item of an action of its own, whose activation the app hears.
// WebKitGTK shows a checkbox's or a radio item's check from the action's
// boolean state, as a check box.
func (w *window) appContextItem(it *platform.MenuItem, natives []ptr) ptr {
	if it.Type == platform.MenuItemSeparator {
		return webkitContextMenuItemNewSeparator()
	}
	label := cs(mnemonicLabel(it.LabelWithSubtitle()))
	if it.Submenu != nil {
		sub := webkitContextMenuNew()
		w.fillContextMenu(sub, it.Submenu.Items, natives)
		return webkitContextMenuItemNewWithSubmenu(label, sub) // which takes sub
	}
	name := cs("mygo-" + strconv.Itoa(it.ID))
	var action ptr
	if it.Type == platform.MenuItemCheckbox || it.Type == platform.MenuItemRadio {
		action = gSimpleActionNewStateful(name, 0, gVariantNewBoolean(it.Checked))
	} else {
		action = gSimpleActionNew(name, 0)
	}
	gSimpleActionSetEnabled(action, it.Enabled)
	connect(action, "activate", cbContextActivate, ptr(it.ID))
	contextItems[it.ID] = contextItem{window: w.id, role: it.Role}
	item := webkitContextMenuItemNewFromGAction(action, label, 0)
	gObjectUnref(action) // the item holds it
	return item
}

// contextActionName names a stock action: MyGo's name for the items other
// platforms have, else the nick of WebKitContextMenuAction's value, as
// "spelling-guess", "" for an item of no stock action.
func contextActionName(action int32) string {
	switch action {
	case 0, 10000: // NO_ACTION, CUSTOM
		return ""
	case 1:
		return "openLink"
	case 2:
		return "openLinkInNewWindow"
	case 3:
		return "downloadLink"
	case 4:
		return "copyLink"
	case 5:
		return "openImageInNewWindow"
	case 6:
		return "downloadImage"
	case 7:
		return "copyImage"
	case 8:
		return "copyImageURL"
	case 9:
		return "openFrameInNewWindow"
	case 10:
		return "back"
	case 11:
		return "forward"
	case 13:
		return "reload"
	case 18:
		return "selectAll"
	case 21:
		return "spellingGuess"
	case 31:
		return "inspect"
	case 32, 33:
		return "openMediaInNewWindow"
	case 34, 35:
		return "copyMediaURL"
	case 36:
		return "mediaControls"
	case 37:
		return "mediaLoop"
	case 38:
		return "mediaFullScreen"
	case 42, 43:
		return "downloadMedia"
	case 44:
		return "emoji"
	case 45:
		return "pasteAndMatchStyle"
	}
	// The others' nicks are MyGo's names too where they have one: stop,
	// copy, cut, paste, delete.
	if contextActions == 0 {
		contextActions = gTypeClassRef(webkitContextMenuActionGetType()) // kept: the type is static
	}
	if v := gEnumGetValue(contextActions, action); v != 0 {
		return goStr(field[ptr](v, 16)) // GEnumValue: value, value_name, value_nick
	}
	return ""
}

// contextActions is the GEnumClass of WebKitContextMenuAction.
var contextActions ptr

// withoutMnemonic returns a GTK label without the underscore before its
// mnemonic, as "_Copy" is "Copy".
func withoutMnemonic(s string) string {
	if !strings.Contains(s, "_") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '_' && i+1 < len(s) {
			i++
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// mnemonicLabel is the label GTK shows as s, whose underscores would
// mark mnemonics.
func mnemonicLabel(s string) string { return strings.ReplaceAll(s, "_", "__") }
