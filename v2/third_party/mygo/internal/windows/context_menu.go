//go:build windows && (amd64 || arm64)

package windows

import (
	"strings"
	"unsafe"

	"github.com/egoist/mygo/internal/platform"
)

// A page's context menu: ICoreWebView2_11's ContextMenuRequested tells
// what was clicked and the items WebView2 is about to show. A menu the
// core leaves as it was stays WebView2's own. One it changed, MyGo draws
// as its other popup menus, and tells WebView2 which of its items was
// chosen (SelectedCommandId), which WebView2 then does. Vtable indices
// from WebView2.h, as in webview2.go: ICoreWebView2_10 ends with
// remove_BasicAuthenticationRequested (98), and ICoreWebView2_11 adds
// CallDevToolsProtocolMethodForSession (99) first.
const (
	wv11AddContextMenuRequested = 100
	ctlGetBounds                = 5

	// ICoreWebView2ContextMenuRequestedEventArgs
	cmReqGetMenuItems         = 3
	cmReqGetContextMenuTarget = 4
	cmReqGetLocation          = 5
	cmReqPutSelectedCommandID = 6
	cmReqPutHandled           = 8
	cmReqGetDeferral          = 10

	// ICoreWebView2ContextMenuItemCollection
	cmItemsGetCount        = 3
	cmItemsGetValueAtIndex = 4

	// ICoreWebView2ContextMenuItem
	cmItemGetName                   = 3
	cmItemGetLabel                  = 4
	cmItemGetCommandID              = 5
	cmItemGetShortcutKeyDescription = 6
	cmItemGetKind                   = 8
	cmItemGetIsEnabled              = 10
	cmItemGetIsChecked              = 12
	cmItemGetChildren               = 13

	// ICoreWebView2ContextMenuTarget
	cmTargetGetKind                    = 3
	cmTargetGetIsEditable              = 4
	cmTargetGetIsRequestedForMainFrame = 5
	cmTargetGetPageURI                 = 6
	cmTargetGetFrameURI                = 7
	cmTargetGetHasLinkURI              = 8
	cmTargetGetLinkURI                 = 9
	cmTargetGetHasLinkText             = 10
	cmTargetGetLinkText                = 11
	cmTargetGetHasSourceURI            = 12
	cmTargetGetSourceURI               = 13
	cmTargetGetHasSelection            = 14
	cmTargetGetSelectionText           = 15
)

var iidICoreWebView2_11 = guid("0be78e56-c193-4051-b943-23b460c08bdb")

// addContextMenuHandler listens to the page's context menus, on WebView2
// Runtimes that tell them (ICoreWebView2_11, 1.0.1185). The handler calls
// the code of context menus, which an app links as it first listens to
// them (LinkContextMenus); until then WebView2 shows its own.
func (w *window) addContextMenuHandler() {
	wv11 := queryInterface(w.webview, &iidICoreWebView2_11)
	if wv11 == 0 {
		return
	}
	var tok int64
	withHandler(func(_, args uintptr) {
		// A listener the core heard of was added after the code was
		// linked: HandlesContextMenu, which takes the listeners' lock,
		// sees contextMenuRequested set.
		if w.h.HandlesContextMenu() && contextMenuRequested != nil {
			contextMenuRequested(w, args)
		}
	}, func(h uintptr) uintptr {
		return comCall(wv11, wv11AddContextMenuRequested, h, uintptr(unsafe.Pointer(&tok)))
	})
	release(wv11)
}

// contextMenuRequested is (*window).contextMenuRequested once
// LinkContextMenus linked it.
var contextMenuRequested func(w *window, args uintptr)

// LinkContextMenus links the code of pages' context menus, when the app
// first listens to them (Page.OnContextMenu).
func LinkContextMenus() { contextMenuRequested = (*window).contextMenuRequested }

// contextNatives holds WebView2's items of a context menu, by number from
// 1, with their labels as WebView2 words them, mnemonics and all.
type contextNatives struct {
	items  []uintptr
	labels []string
}

func (c *contextNatives) release() {
	for _, item := range c.items {
		release(item)
	}
}

// contextMenuRequested asks the core about the menu WebView2 is about to
// show. Doing nothing leaves the menu to WebView2.
func (w *window) contextMenuRequested(args uintptr) {
	var list uintptr
	if failed(comCall(args, cmReqGetMenuItems, uintptr(unsafe.Pointer(&list)))) || list == 0 {
		return
	}
	c := &contextNatives{}
	// In DIPs, whatever the display's scale, unlike the web view's bounds.
	var at point
	comCall(args, cmReqGetLocation, uintptr(unsafe.Pointer(&at)))
	m := &platform.ContextMenu{Pointer: true, X: float64(at.X), Y: float64(at.Y), Items: c.describe(list)}
	// What WebView2 leaves out of a click in the page's own document, as
	// the URL of a data: image, the page's contextmenu event tells; one in
	// a frame never reaches it.
	m.Target, m.Complete = w.contextMenuTarget(args)
	release(list)
	resp := w.h.ContextMenu(m)
	if resp.Default {
		c.release()
		return
	}
	comCall(args, cmReqPutHandled, 1)
	var deferral uintptr
	if resp.Menu == nil || failed(comCall(args, cmReqGetDeferral, uintptr(unsafe.Pointer(&deferral)))) || deferral == 0 {
		c.release()
		return
	}
	addRef(args)
	// The menu shows once WebView2 got its answer, as the sample of its
	// documentation does: its modal loop must not run in WebView2's call.
	w.b.post(func() {
		defer c.release()
		done := func() {
			comCall(deferral, deferralComplete)
			release(deferral)
			release(args)
		}
		if w.closed || w.controller == 0 {
			done()
			return
		}
		w.showContextMenu(resp.Menu, c, at, args, done)
	})
}

// showContextMenu shows a context menu the core changed at a point of the
// web view, and runs the item chosen: WebView2's, through args, once done
// tells WebView2 the menu closed, or the app's, after.
func (w *window) showContextMenu(m *platform.Menu, c *contextNatives, at point, args uintptr, done func()) {
	relabel(m.Items, c)
	m.TidySeparators() // around the items relabel hid
	var bounds rect
	comCall(w.controller, ctlGetBounds, uintptr(unsafe.Pointer(&bounds)))
	dpi := int32(dpiOf(w.hwnd))
	pt := point{bounds.Left + (at.X*dpi+48)/96, bounds.Top + (at.Y*dpi+48)/96}
	parent := w.hwnd
	if w.host != nil && w.host.surface != nil {
		parent = w.host.surface.hwnd
	}
	procClientToScreen.Call(parent, uintptr(unsafe.Pointer(&pt)))
	b := w.b
	owner := b.menus.newOwner()
	defer b.menus.drop(owner)
	menu := b.buildMenu(m, false, owner, nil, monitorDPI(monitorAt(int(pt.X), int(pt.Y))))
	defer procDestroyMenu.Call(menu)
	cmd := b.trackPopup(menu, w.top().hwnd, pt, 0, nil)
	e := b.menus.entries[cmd]
	if cmd != 0 && e != nil && e.native > 0 && e.native <= len(c.items) {
		var id int32
		comCall(c.items[e.native-1], cmItemGetCommandID, uintptr(unsafe.Pointer(&id)))
		comCall(args, cmReqPutSelectedCommandID, uintptr(id))
		cmd = 0
	}
	done()
	if cmd != 0 {
		b.menuCommand(cmd, w)
	}
}

// relabel gives WebView2's items whose labels the core left as they were
// WebView2's own, with their mnemonics, and hides those still without a
// label: WebView2 tells the spelling suggestions with none.
func relabel(items []*platform.MenuItem, c *contextNatives) {
	for _, it := range items {
		if it.Native > 0 && it.Native <= len(c.labels) && it.Label == withoutAmpersands(c.labels[it.Native-1]) {
			it.Label = c.labels[it.Native-1]
		}
		if it.Native > 0 && it.Label == "" && it.Type != platform.MenuItemSeparator {
			it.Visible = false
		}
		if it.Submenu != nil {
			relabel(it.Submenu.Items, c)
		}
	}
}

// describe describes WebView2's items of a menu and its submenus,
// numbering each, and keeps it in c.
func (c *contextNatives) describe(list uintptr) []*platform.MenuItem {
	var n uint32
	comCall(list, cmItemsGetCount, uintptr(unsafe.Pointer(&n)))
	items := make([]*platform.MenuItem, 0, n)
	for i := range n {
		var item, p uintptr
		if failed(comCall(list, cmItemsGetValueAtIndex, uintptr(i), uintptr(unsafe.Pointer(&item)))) || item == 0 {
			continue
		}
		comCall(item, cmItemGetLabel, uintptr(unsafe.Pointer(&p)))
		label := takeWstr(p)
		c.items, c.labels = append(c.items, item), append(c.labels, label)
		it := &platform.MenuItem{Native: len(c.items), Label: withoutAmpersands(label), Visible: true}
		items = append(items, it)
		var kind, v int32
		comCall(item, cmItemGetKind, uintptr(unsafe.Pointer(&kind)))
		if kind == 3 { // COREWEBVIEW2_CONTEXT_MENU_ITEM_KIND_SEPARATOR
			it.Type, it.Label, it.Enabled = platform.MenuItemSeparator, "", true
			continue
		}
		comCall(item, cmItemGetName, uintptr(unsafe.Pointer(&p)))
		it.Name = contextItemName(takeWstr(p))
		comCall(item, cmItemGetShortcutKeyDescription, uintptr(unsafe.Pointer(&p)))
		it.Accelerator = takeWstr(p)
		comCall(item, cmItemGetIsEnabled, uintptr(unsafe.Pointer(&v)))
		it.Enabled = v != 0
		switch kind {
		case 1, 2: // CHECK_BOX, RADIO
			it.Type = platform.MenuItemCheckbox
			if kind == 2 {
				it.Type = platform.MenuItemRadio
			}
			comCall(item, cmItemGetIsChecked, uintptr(unsafe.Pointer(&v)))
			it.Checked = v != 0
		case 4: // SUBMENU
			var children uintptr
			if comCall(item, cmItemGetChildren, uintptr(unsafe.Pointer(&children))); children != 0 {
				it.Type, it.Submenu = platform.MenuItemSubmenu, &platform.Menu{Items: c.describe(children)}
				release(children)
			}
		}
	}
	return items
}

// contextNames are MyGo's names of WebView2's items whose names differ.
var contextNames = map[string]string{
	"inspectElement":    "inspect",
	"saveLinkAs":        "downloadLink",
	"copyLinkLocation":  "copyLink",
	"saveImageAs":       "downloadImage",
	"copyImageLocation": "copyImageURL",
	"saveMediaAs":       "downloadMedia",
	"copyVideoLocation": "copyMediaURL",
	"copyAudioLocation": "copyMediaURL",
	"loop":              "mediaLoop",
	"showAllControls":   "mediaControls",
	"spellcheck":        "spellingGuess",
}

// contextItemName names an item of WebView2's: MyGo's name for the items
// other platforms have, else WebView2's own, as "createQrCode".
func contextItemName(name string) string {
	if n, ok := contextNames[name]; ok {
		return n
	}
	return name
}

// withoutAmpersands returns a menu label without the ampersand before its
// mnemonic, as "&Copy" is "Copy", and "&&" is "&".
func withoutAmpersands(s string) string {
	if !strings.Contains(s, "&") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '&' && i+1 < len(s) {
			i++
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// contextMenuTarget tells what a context menu was opened on, with the
// URLs of the app's schemes as the app knows them, and whether it was
// opened in a frame, whose target WebView2 tells all of.
func (w *window) contextMenuTarget(args uintptr) (t platform.ContextMenuTarget, inFrame bool) {
	var target uintptr
	if failed(comCall(args, cmReqGetContextMenuTarget, uintptr(unsafe.Pointer(&target)))) || target == 0 {
		return t, false
	}
	defer release(target)
	flag := func(i int) bool {
		var v int32
		comCall(target, i, uintptr(unsafe.Pointer(&v)))
		return v != 0
	}
	str := func(i int) string {
		var p uintptr
		comCall(target, i, uintptr(unsafe.Pointer(&p)))
		return takeWstr(p)
	}
	t.Editable = flag(cmTargetGetIsEditable)
	if flag(cmTargetGetHasLinkURI) {
		t.LinkURL = w.appURL(str(cmTargetGetLinkURI))
	}
	if flag(cmTargetGetHasLinkText) {
		t.LinkText = str(cmTargetGetLinkText)
	}
	if flag(cmTargetGetHasSourceURI) {
		var kind int32
		comCall(target, cmTargetGetKind, uintptr(unsafe.Pointer(&kind)))
		switch src := w.appURL(str(cmTargetGetSourceURI)); kind {
		case 1: // COREWEBVIEW2_CONTEXT_MENU_TARGET_KIND_IMAGE
			t.ImageURL = src
		case 3, 4: // AUDIO, VIDEO
			t.MediaURL = src
		}
	}
	if flag(cmTargetGetHasSelection) {
		t.SelectionText = str(cmTargetGetSelectionText)
	}
	// IsRequestedForMainFrame says false for the page's own document too,
	// whose frame is the page.
	if !flag(cmTargetGetIsRequestedForMainFrame) {
		if frame := str(cmTargetGetFrameURI); frame != str(cmTargetGetPageURI) {
			t.FrameURL, inFrame = w.appURL(frame), true
		}
	}
	return t, inFrame
}
