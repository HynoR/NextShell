//go:build linux && (amd64 || arm64)

package linux

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/ebitengine/purego"

	"github.com/egoist/mygo/internal/accelerator"
	"github.com/egoist/mygo/internal/platform"
)

// Native menu items are tracked per owner (a window's menu bar, a popup, a
// tray) so they can be updated in place and forgotten when rebuilt.
type nativeItem struct {
	owner    int
	item     ptr
	check    bool
	windowID int // menu bar or popup's window; 0 for a tray
	// disabled keeps the app's choice when enableEditItems changes the native
	// item's state.
	disabled bool
	// icon is the item's icon; image and label are the widgets of an item
	// that shows one, or did (menuicon.go).
	icon         *platform.MenuIcon
	image, label ptr
	bar          bool // an item of a menu bar, which shows no icon
}

var (
	menuOwners int
	itemsByID  = map[int][]nativeItem{}
	itemRoles  = map[int]string{}
	// roleKeys are the shortcuts of items with a role, by accel group: the
	// keyval, lower case, and the modifiers.
	roleKeys = map[ptr]map[[2]uint32]bool{}

	cbMenuActivate   ptr
	cbMenuDeactivate ptr
	cbMenuMap        ptr
	cbTrayActivate   ptr

	// Menu bars that hide (window.menuHides), and the keys of items with a
	// role.
	cbMenuKey, cbCanActivateAccel, cbMenuBarDeactivate ptr
)

// modifierMask holds the GDK modifiers that make a key a shortcut: Shift,
// Control, Alt, Super, Hyper and Meta.
const modifierMask = 1<<0 | 1<<2 | 1<<3 | 1<<26 | 1<<27 | 1<<28

// editCommands are handled by the focused webview itself.
var editCommands = map[string]string{
	"undo": "Undo", "redo": "Redo", "cut": "Cut", "copy": "Copy", "paste": "Paste",
	"pasteAndMatchStyle": "PasteAsPlainText", "delete": "Delete", "selectAll": "SelectAll",
	// The Format menu's.
	"bold": "Bold", "italic": "Italic", "underline": "Underline", "strikethrough": "Strikethrough",
	"alignLeft": "JustifyLeft", "alignCenter": "JustifyCenter", "alignRight": "JustifyRight",
}

func initMenuCallbacks() {
	cbMenuActivate = purego.NewCallback(func(item, data ptr) {
		if settingState {
			return
		}
		b := theBackend
		id := int(data)
		// A submenu may have focus. Resolve this native item's window, as
		// the same Go item may appear in several windows' menu bars.
		var w *window
		for _, n := range itemsByID[id] {
			if n.item == item && n.windowID != 0 {
				w = b.window(ptr(n.windowID))
				if w == nil {
					return
				}
				break
			}
		}
		if cmd, ok := editCommands[itemRoles[id]]; ok {
			if w == nil {
				for _, x := range b.windows {
					if !x.closed && gtkWindowIsActive(x.win) {
						w = x
						break
					}
				}
			}
			var v *window
			if w != nil {
				v = w.focusedWebView()
			}
			if v != nil {
				// The page of a web view in the content has the keyboard.
				webkitWebViewExecuteEditingCommand(v.web, cs(cmd))
			} else if w != nil {
				if w.surface != nil {
					role := itemRoles[id]
					w.h.SurfaceEvent(platform.SurfaceEvent{Kind: platform.SurfaceCommand, Text: role})
				} else {
					webkitWebViewExecuteEditingCommand(w.web, cs(cmd))
				}
			}
			return
		}
		if w != nil {
			w.h.MenuItemClicked(id)
		} else {
			b.h.MenuItemClicked(id)
		}
	})
	cbMenuDeactivate = purego.NewCallback(func(menu, loop ptr) {
		gMainLoopQuit(loop)
	})
	cbMenuMap = purego.NewCallback(func(menu, data ptr) {
		if w := theBackend.window(data); w != nil {
			w.enableEditItems()
		}
	})
	// Alt pressed and released alone, or F10, show a menu bar that hides.
	cbMenuKey = purego.NewCallback(func(widget, event, data ptr) bool {
		w := theBackend.window(data)
		if w == nil {
			return false
		}
		// The shortcuts of items with a role are the content's keys too, as
		// Ctrl+C, Edit › Copy's, which interrupts in a terminal: GTK would
		// run its accelerators first, so the content gets them first, and
		// GTK runs those it leaves. The app's own items go first, as on macOS.
		if field[int32](event, 0) == 8 && w.surface != nil { // GDK_KEY_PRESS
			k := [2]uint32{gdkKeyvalToLower(field[uint32](event, 28)), field[uint32](event, 24) & modifierMask}
			if roleKeys[w.accel][k] {
				if gtkWindowPropagateKeyEvent(w.win, event) {
					return true
				}
				// GTK drops the shortcut of an item made insensitive as the
				// content was when its menu last opened, not as it is now.
				w.enableEditItems()
			}
		}
		if !w.menuHides() || w.menubar == 0 || gtkWidgetGetVisible(w.menubar) {
			w.altAlone = false
			return false
		}
		// GdkEventKey: type 0, state 24, keyval 28.
		press := field[int32](event, 0) == 8 // GDK_KEY_PRESS
		state, key := field[uint32](event, 24), field[uint32](event, 28)
		alt := key == 0xffe9 || key == 0xffea // Alt_L, Alt_R
		switch {
		case press && key == 0xffc7 && state&modifierMask == 0: // F10
			w.revealMenu()
			return true
		case press:
			w.altAlone = alt && state&modifierMask == 0
		case alt && w.altAlone:
			w.altAlone = false
			w.revealMenu()
		default:
			w.altAlone = false
		}
		return false
	})
	// GTK activates the shortcuts of widgets on screen only: a menu bar that
	// hides keeps its own.
	cbCanActivateAccel = purego.NewCallback(func(widget ptr, signal uint32, data ptr) bool {
		w := theBackend.window(data)
		return w != nil && w.menuHides()
	})
	cbMenuBarDeactivate = purego.NewCallback(func(shell, data ptr) {
		if w := theBackend.window(data); w != nil && w.menuHides() {
			gtkWidgetHide(shell)
		}
	})
	// A panel activates an icon it can tell a double click from: GNOME's
	// AppIndicator extension sends AppIndicator::activate when its icon is
	// double-clicked.
	cbTrayActivate = purego.NewCallback(func(ind, x, y, data ptr) {
		if t := theBackend.trays[int(data)]; t != nil && t.h != nil {
			t.h.Clicked()
		}
	})
}

func newOwner() int {
	menuOwners++
	return menuOwners
}

func dropOwner(owner int) {
	if owner == 0 {
		return
	}
	for id, list := range itemsByID {
		kept := list[:0]
		for _, n := range list {
			if n.owner != owner {
				kept = append(kept, n)
			}
		}
		if len(kept) == 0 {
			delete(itemsByID, id)
		} else {
			itemsByID[id] = kept
		}
	}
}

// buildMenu fills a GtkMenuShell with items; bar is a menu bar's, whose
// own items show no icons.
func buildMenu(shell ptr, m *platform.Menu, accel ptr, owner, windowID int, bar bool) {
	for _, it := range m.Items {
		gtkMenuShellAppend(shell, buildItem(it, accel, owner, windowID, bar))
	}
}

func buildItem(it *platform.MenuItem, accel ptr, owner, windowID int, bar bool) ptr {
	var item ptr
	check := false
	switch it.Type {
	case platform.MenuItemSeparator:
		item = gtkSeparatorMenuItemNew()
	case platform.MenuItemCheckbox, platform.MenuItemRadio:
		item = gtkCheckMenuItemNewWithLabel(cs(it.LabelWithSubtitle()))
		gtkCheckMenuItemSetDrawAsRadio(item, it.Type == platform.MenuItemRadio)
		check = true
	default:
		item = gtkMenuItemNewWithLabel(cs(it.LabelWithSubtitle()))
	}
	if it.Submenu != nil {
		sub := gtkMenuNew()
		if windowID != 0 {
			connect(sub, "map", cbMenuMap, ptr(windowID))
		}
		buildMenu(sub, it.Submenu, accel, owner, windowID, false)
		gtkMenuItemSetSubmenu(item, sub)
	} else if it.Type != platform.MenuItemSeparator {
		connect(item, "activate", cbMenuActivate, ptr(it.ID))
		itemRoles[it.ID] = it.Role
		if accel != 0 && it.Accelerator != "" {
			if key, mods, ok := gtkAccelerator(it.Accelerator); ok {
				gtkWidgetAddAccelerator(item, cs("activate"), accel, key, mods, 1) // GTK_ACCEL_VISIBLE
				if it.Role != "" {
					if roleKeys[accel] == nil {
						roleKeys[accel] = map[[2]uint32]bool{}
					}
					roleKeys[accel][[2]uint32{gdkKeyvalToLower(key), mods}] = true
				}
			}
		}
	}
	applyState(item, it, check)
	n := nativeItem{owner: owner, item: item, check: check, windowID: windowID, disabled: !it.Enabled, bar: bar}
	if it.Icon != nil && it.Type != platform.MenuItemSeparator && !bar && setMenuItemIcon != nil {
		n.icon = it.Icon
		if windowID != 0 {
			setMenuItemIcon(&n, it.Icon) // a tray's once its menu is exported
		}
	}
	itemsByID[it.ID] = append(itemsByID[it.ID], n)
	return item
}

// settingState is true while a check item is changed programmatically:
// gtk_check_menu_item_set_active emits "activate" as if the user clicked.
var settingState bool

func applyState(item ptr, it *platform.MenuItem, check bool) {
	gtkWidgetSetSensitive(item, it.Enabled)
	gtkWidgetSetNoShowAll(item, !it.Visible)
	gtkWidgetSetVisible(item, it.Visible)
	if check {
		settingState = true
		gtkCheckMenuItemSetActive(item, it.Checked)
		settingState = false
	}
	if it.ToolTip != "" {
		gtkWidgetSetTooltipText(item, cs(it.ToolTip))
	}
}

var gdkKeyNames = map[string]string{
	"Enter": "Return", "Tab": "Tab", "Space": "space", "Backspace": "BackSpace", "Delete": "Delete",
	"Insert": "Insert", "Escape": "Escape", "Up": "Up", "Down": "Down", "Left": "Left", "Right": "Right",
	"Home": "Home", "End": "End", "PageUp": "Page_Up", "PageDown": "Page_Down", "+": "plus",
	"VolumeUp": "XF86AudioRaiseVolume", "VolumeDown": "XF86AudioLowerVolume", "VolumeMute": "XF86AudioMute",
	"MediaNextTrack": "XF86AudioNext", "MediaPreviousTrack": "XF86AudioPrev", "MediaStop": "XF86AudioStop",
	"MediaPlayPause": "XF86AudioPlay", "PrintScreen": "Print", "NumDec": "KP_Decimal", "NumAdd": "KP_Add",
	"NumSub": "KP_Subtract", "NumMult": "KP_Multiply", "NumDiv": "KP_Divide", "CapsLock": "Caps_Lock",
	"NumLock": "Num_Lock", "ScrollLock": "Scroll_Lock",
}

// gtkAccelerator converts an accelerator to a GDK keyval and modifier mask.
func gtkAccelerator(acc string) (uint32, uint32, bool) {
	a, err := accelerator.Parse(acc, "linux")
	if err != nil {
		return 0, 0, false
	}
	var mods uint32
	if a.Has(accelerator.Shift) {
		mods |= 1 << 0
	}
	if a.Has(accelerator.Ctrl) {
		mods |= 1 << 2
	}
	if a.Has(accelerator.Alt) {
		mods |= 1 << 3
	}
	if a.Has(accelerator.Super) {
		mods |= 1 << 26
	}
	var key uint32
	switch {
	case gdkKeyNames[a.Key] != "":
		key = gdkKeyvalFromName(cs(gdkKeyNames[a.Key]))
	case strings.HasPrefix(a.Key, "F") && len(a.Key) > 1:
		key = gdkKeyvalFromName(cs(a.Key))
	case strings.HasPrefix(a.Key, "Num") && len(a.Key) == 4:
		key = gdkKeyvalFromName(cs("KP_" + a.Key[3:]))
	default:
		r, _ := utf8.DecodeRuneInString(a.Key)
		key = gdkUnicodeToKeyval(uint32(r))
		if mods&1 != 0 && !unicode.IsLetter(r) {
			key = shifted(key)
		}
	}
	return key, mods, key != 0
}

func (b *Backend) SetApplicationMenu(m *platform.Menu) {
	b.appMenu = m
	for _, w := range b.windows {
		if !w.ownMenu && w.hasAppMenu() {
			w.installMenu(m)
		}
	}
}

// hasAppMenu tells whether the window shows the application menu in its
// menu bar: flyouts and panels show none.
func (w *window) hasAppMenu() bool { return w.opts.Flyout == nil && !w.opts.Panel }

func (w *window) SetMenu(m *platform.Menu) {
	w.ownMenu = m != nil
	if m == nil && w.hasAppMenu() {
		m = w.b.appMenu
	}
	w.installMenu(m)
}

// installMenu replaces the menu bar at the top of the window.
func (w *window) installMenu(m *platform.Menu) {
	if w.menubar != 0 {
		gtkWidgetDestroy(w.menubar)
		w.menubar = 0
		dropOwner(w.owner)
		// Accelerators live in the group; start with a fresh one.
		gtkWindowRemoveAccelGroup(w.win, w.accel)
		gObjectUnref(w.accel)
		delete(roleKeys, w.accel)
		w.accel = gtkAccelGroupNew()
		gtkWindowAddAccelGroup(w.win, w.accel)
	}
	if m == nil || len(m.Items) == 0 {
		return
	}
	w.owner = newOwner()
	w.menubar = gtkMenuBarNew()
	buildMenu(w.menubar, m, w.accel, w.owner, w.id, true)
	gtkBoxPackStart(w.box, w.menubar, false, false, 0)
	gtkBoxReorderChild(w.box, w.menubar, 0)
	gtkWidgetShowAll(w.menubar)
	// The bar shows as menuHides says, whatever shows the window.
	gtkWidgetSetNoShowAll(w.menubar, true)
	gtkWidgetSetVisible(w.menubar, !w.menuHides())
	data := ptr(w.id)
	connect(w.menubar, "can-activate-accel", cbCanActivateAccel, data)
	connect(w.menubar, "deactivate", cbMenuBarDeactivate, data)
}

func (w *window) SetAutoHideMenu(v bool) {
	w.autoHideMenu = v
	if w.menubar != 0 {
		gtkWidgetSetVisible(w.menubar, !w.menuHides())
	}
}

// menuHides reports whether the menu bar shows only while its menus are
// open: one that hides, any in full screen, which leaves the page the
// whole screen, and any in a window with a hidden title bar, whose page
// draws the title bar at the top of the window, with the window buttons
// over it: a bar above would push both down, under the menus. Windows
// leaves such a window no menu bar either.
func (w *window) menuHides() bool {
	return w.autoHideMenu || w.state&stateFullscreen != 0 || w.hiddenTitleBar()
}

// revealMenu shows a menu bar that hides and opens its first menu, as F10
// does in GTK, while the key event is current: the menu grabs its device.
// The bar hides again when its menus close.
func (w *window) revealMenu() {
	gtkWidgetShow(w.menubar)
	// The menu opens under its item: lay the bar out now.
	gtkContainerCheckResize(w.win)
	if item := firstMenu(w.menubar); item != 0 {
		gtkWidgetMnemonicActivate(item, false) // as the item's mnemonic
	} else {
		gtkWidgetHide(w.menubar) // nothing to open, so no deactivate to come
	}
}

// firstMenu returns the first menu of a menu bar that the user can open,
// or 0.
func firstMenu(bar ptr) ptr {
	list := gtkContainerGetChildren(bar)
	defer gListFree(list)
	for node := list; node != 0; node = field[ptr](node, 8) {
		if item := field[ptr](node, 0); gtkWidgetGetVisible(item) && gtkWidgetIsSensitive(item) {
			return item
		}
	}
	return 0
}

func (b *Backend) UpdateMenuItem(it *platform.MenuItem) {
	// New windows build the application menu as it is now.
	b.appMenu.Apply(it)
	for i := range itemsByID[it.ID] {
		n := &itemsByID[it.ID][i]
		if it.Type != platform.MenuItemSeparator {
			if n.label != 0 {
				gtkLabelSetLabel(n.label, cs(it.LabelWithSubtitle()))
			} else {
				gtkMenuItemSetLabel(n.item, cs(it.LabelWithSubtitle()))
			}
			if it.Icon != n.icon && !n.bar && setMenuItemIcon != nil {
				n.icon = it.Icon
				setMenuItemIcon(n, it.Icon)
			}
		}
		applyState(n.item, it, n.check)
		n.disabled = !it.Enabled
	}
}

// enableEditItems makes the items of the edit roles in the window's menu
// bar and popups sensitive as the native UI it shows can act on them.
// Those of a page keep the app's state: what a page would do is not known
// before.
func (w *window) enableEditItems() {
	native := w.surface != nil && w.focusedWebView() == nil
	for id, role := range itemRoles {
		if !platform.EditCommands[role] {
			continue
		}
		for _, n := range itemsByID[id] {
			if n.windowID != w.id {
				continue
			}
			on := !n.disabled
			if on && native {
				on = w.h.SurfaceEvent(platform.SurfaceEvent{Kind: platform.SurfaceCommandEnabled, Text: role})
			}
			gtkWidgetSetSensitive(n.item, on)
		}
	}
}

// PopupMenu shows a context menu and blocks until it is dismissed.
func (b *Backend) PopupMenu(m *platform.Menu, pw platform.Window, pos *platform.Point) {
	// GTK positions menus relative to a window: the given one, else the
	// active one.
	w, _ := pw.(*window)
	if w == nil || w.closed {
		w = nil
		for _, x := range b.windows {
			if !x.closed && gtkWindowIsActive(x.win) {
				w = x
				break
			}
		}
	}
	if w == nil {
		return // nowhere to show it
	}
	owner := newOwner()
	defer dropOwner(owner)
	menu := gtkMenuNew()
	buildMenu(menu, m, 0, owner, w.id, false)
	w.enableEditItems()
	gtkWidgetShowAll(menu)
	event, anchor := w.popupTrigger(), w.contentWindow()
	const northWest = 1 // GDK_GRAVITY_NORTH_WEST
	corner := int32(northWest)
	if m.RightToLeft {
		// GTK_TEXT_DIR_RTL lays the menu out from the right (its items
		// take the default direction, which GTK takes from the locale),
		// and GDK_GRAVITY_NORTH_EAST opens it to the left of the point.
		gtkWidgetSetDirection(menu, 2)
		corner = 3
	}
	switch {
	case pos != nil && anchor != 0:
		gtkMenuPopupAtRect(menu, anchor, &gdkRectangle{X: int32(pos.X), Y: int32(pos.Y), Width: 1, Height: 1}, northWest, corner, event)
	case event != 0:
		gtkMenuPopupAtPointer(menu, event)
	case anchor != 0:
		// No mouse press to anchor to: at the pointer.
		var x, y int32
		pointer := gdkSeatGetPointer(gdkDisplayGetDefaultSeat(gdkDisplayGetDefault()))
		gdkWindowGetDevicePosition(anchor, pointer, &x, &y, nil)
		gtkMenuPopupAtRect(menu, anchor, &gdkRectangle{X: x, Y: y, Width: 1, Height: 1}, northWest, northWest, 0)
	}
	if !gtkWidgetGetVisible(menu) {
		// GTK could not show it: there is no dismissal to wait for.
		gtkWidgetDestroy(menu)
		return
	}
	loop := gMainLoopNew(0, false)
	connect(menu, "deactivate", cbMenuDeactivate, loop)
	b.popups = append(b.popups, menu)
	b.quitLoops = append(b.quitLoops, loop)
	gMainLoopRun(loop)
	b.quitLoops = b.quitLoops[:len(b.quitLoops)-1]
	b.popups = b.popups[:len(b.popups)-1]
	gMainLoopUnref(loop)
	gtkWidgetDestroy(menu)
}

// Tray icons use AppIndicator (StatusNotifierItem), which shows a menu on
// a click. A panel can activate the icon, a double click on GNOME's
// indicators, and the library reports that as the "activate" signal while
// a handler is connected.
type tray struct {
	b       *Backend
	h       platform.TrayHandler
	id      int
	ind     ptr
	menu    ptr
	owner   int
	iconDir string
	icons   int
	// activate is the handler of the "activate" signal, 0 while the icon
	// does not answer activation.
	activate uint64
}

func (b *Backend) NewTray(h platform.TrayHandler) (platform.Tray, error) {
	if appIndicatorNew == nil {
		return nil, fmt.Errorf("mygo: tray icons need libayatana-appindicator3: %w", platform.ErrUnsupported)
	}
	b.nextTray++
	t := &tray{b: b, h: h, id: b.nextTray}
	name := fmt.Sprintf("mygo-%d-%d", os.Getpid(), t.id)
	t.ind = appIndicatorNew(cs(name), cs("application-x-executable"), 0) // APPLICATION_STATUS
	appIndicatorSetStatus(t.ind, 1)                                      // ACTIVE
	// AppIndicator only shows up once it has a menu.
	t.SetMenu(&platform.Menu{})
	b.trays[t.id] = t
	return t, nil
}

func (t *tray) SetImage(png []byte, template bool) error {
	if t.iconDir == "" {
		dir, err := os.MkdirTemp("", "mygo-tray-")
		if err != nil {
			return err
		}
		t.iconDir = dir
	}
	// A new file name makes the indicator host reload the icon. The icon
	// goes by its absolute path, not by a name in IconThemePath: KDE looks
	// a name up in the icon theme first, falling back from "icon-1" to
	// "icon", and showed a theme's icon in place of the app's.
	t.icons++
	file := filepath.Join(t.iconDir, fmt.Sprintf("icon-%d.png", t.icons))
	if err := os.WriteFile(file, png, 0o644); err != nil {
		return err
	}
	appIndicatorSetIconFull(t.ind, cs(file), cs(""))
	return nil
}

// Dark reports false: AppIndicator tells nothing of the panel, whose
// icon theme draws the icon in its own colors.
func (t *tray) Dark() bool { return false }

// SetID does nothing: the indicator's ID is given as it is made, and
// panels place indicators themselves.
func (t *tray) SetID(string) {}

func (t *tray) SetTitle(title string) { appIndicatorSetLabel(t.ind, cs(title), cs(title)) }

// SetToolTip sets the hover text. The title is what panels show when they
// fall back to it, and setting it announces NewTitle alone: a host that
// caches the ToolTip property, such as quickshell, would go on showing the
// text it read when the icon appeared. Setting the tooltip as well announces
// NewToolTip, so both stay in step.
func (t *tray) SetToolTip(tip string) {
	if appIndicatorSetTitle != nil {
		appIndicatorSetTitle(t.ind, cs(tip))
	}
	if appIndicatorSetTooltipFull != nil {
		appIndicatorSetTooltipFull(t.ind, nil, cs(tip), nil)
	}
}

func (t *tray) SetMenu(m *platform.Menu) {
	dropOwner(t.owner)
	t.owner = newOwner()
	menu := gtkMenuNew()
	if m != nil {
		buildMenu(menu, m, 0, t.owner, 0, false)
	}
	gtkWidgetShowAll(menu)
	appIndicatorSetMenu(t.ind, menu)
	t.menu = menu
	// The items are exported now: give them their icons.
	for _, list := range itemsByID {
		for _, n := range list {
			if n.owner == t.owner && n.icon != nil {
				setMenuItemIcon(&n, n.icon)
			}
		}
	}
}

func (t *tray) PopUpMenu(*platform.Menu) {}
func (t *tray) Bounds() platform.Rect    { return platform.Rect{} }

// SetActivatable connects or drops the indicator's activate handler, which
// is also the answer a panel gets when it asks the icon whether it can be
// activated at all: the Activate method of the StatusNotifierItem
// interface fails with UNKNOWN_METHOD while no handler is connected. GNOME's
// AppIndicator extension then opens the menu on every click; with a
// handler it opens the menu on a single click after waiting for a second
// one, and the second click arrives here as TrayHandler.Clicked, as on
// Telegram's icon.
func (t *tray) SetActivatable(on bool) {
	if t.ind == 0 || on == (t.activate != 0) || !trayActivates() {
		return
	}
	if !on {
		gSignalHandlerDisconnect(t.ind, t.activate)
		t.activate = 0
		return
	}
	t.activate = connect(t.ind, "activate", cbTrayActivate, ptr(t.id))
}

// trayActivates tells whether the AppIndicator library has the
// "activate" signal, which older libayatana-appindicator3 releases lack:
// their icons answer no activation, whatever the app listens to. It is
// asked once an indicator exists, which registers the type.
var trayActivates = sync.OnceValue(func() bool {
	var lookup func(name *byte, typ uintptr) uint32
	var typeFromName func(name *byte) uintptr
	if !bind(libGObject, &lookup, "g_signal_lookup") || !bind(libGObject, &typeFromName, "g_type_from_name") {
		return false
	}
	typ := typeFromName(cs("AppIndicator"))
	return typ != 0 && lookup(cs("activate"), typ) != 0
})

func (t *tray) Destroy() {
	if t.ind == 0 {
		return
	}
	t.SetActivatable(false)
	appIndicatorSetStatus(t.ind, 0) // PASSIVE
	gObjectUnref(t.ind)
	t.ind = 0
	dropOwner(t.owner)
	delete(t.b.trays, t.id)
	if t.iconDir != "" {
		_ = os.RemoveAll(t.iconDir)
	}
}

// shifted returns the keyval that the key typing keyval types with Shift,
// in the keyboard's layout: plus for equal on a US keyboard. GTK matches a
// shortcut with Shift by what the key types, Shift consumed, so Alt+Shift+=
// is Alt+Shift+plus to it. It returns keyval when no key types it.
func shifted(keyval uint32) uint32 {
	km := gdkKeymapGetForDisplay(gdkDisplayGetDefault())
	var keys ptr
	var n int32
	if km == 0 || !gdkKeymapGetEntriesForKeyval(km, keyval, &keys, &n) || n == 0 {
		return keyval
	}
	defer gFree(keys)
	// GdkKeymapKey: keycode, group, level, each 4 bytes.
	for i := range uintptr(n) {
		code, group, level := field[uint32](keys, i*12), field[int32](keys, i*12+4), field[int32](keys, i*12+8)
		if level != 0 {
			continue
		}
		var sym, consumed uint32
		var effGroup, outLevel int32
		if gdkKeymapTranslateKeyboardState(km, code, 1, group, &sym, &effGroup, &outLevel, &consumed) && sym != 0 {
			return sym
		}
	}
	return keyval
}
