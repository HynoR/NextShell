package mygo

import (
	"math"
	"sync"

	"github.com/egoist/mygo/internal/platform"
)

// The IDs of the web view's own items in a page's context menu
// (ContextMenuEvent.Menu), the same on every platform that has the item.
// Items without one of these keep the platform's own name as their ID:
// WebKit's identifier on macOS ("WKMenuItemIdentifierTranslateMenu"), the
// nick of the stock action on Linux ("spelling-guess"), WebView2's name on
// Windows ("createQrCode"); items the platform names none of have "".
const (
	ContextMenuBack                 = "back"
	ContextMenuForward              = "forward"
	ContextMenuReload               = "reload"
	ContextMenuStop                 = "stop"
	ContextMenuInspect              = "inspect" // Inspect Element, with DevTools
	ContextMenuUndo                 = "undo"
	ContextMenuRedo                 = "redo"
	ContextMenuCut                  = "cut"
	ContextMenuCopy                 = "copy"
	ContextMenuPaste                = "paste"
	ContextMenuPasteAndMatchStyle   = "pasteAndMatchStyle"
	ContextMenuDelete               = "delete"
	ContextMenuSelectAll            = "selectAll"
	ContextMenuSpellingGuess        = "spellingGuess" // each of the words suggested for a misspelled one
	ContextMenuOpenLink             = "openLink"
	ContextMenuOpenLinkInNewWindow  = "openLinkInNewWindow"
	ContextMenuDownloadLink         = "downloadLink"
	ContextMenuCopyLink             = "copyLink"
	ContextMenuOpenImageInNewWindow = "openImageInNewWindow"
	ContextMenuDownloadImage        = "downloadImage"
	ContextMenuCopyImage            = "copyImage"
	ContextMenuCopyImageURL         = "copyImageURL"
	ContextMenuOpenFrameInNewWindow = "openFrameInNewWindow"
	ContextMenuOpenMediaInNewWindow = "openMediaInNewWindow"
	ContextMenuDownloadMedia        = "downloadMedia"
	ContextMenuCopyMediaURL         = "copyMediaURL"
	ContextMenuMediaControls        = "mediaControls" // show or hide a video's controls
	ContextMenuMediaLoop            = "mediaLoop"
	ContextMenuMediaFullScreen      = "mediaFullScreen"
	ContextMenuPictureInPicture     = "pictureInPicture"
	ContextMenuLookUp               = "lookUp"
	ContextMenuSearchWeb            = "searchWeb"
	ContextMenuShare                = "share"
	ContextMenuEmoji                = "emoji"
	ContextMenuPrint                = "print"
)

// ContextMenuEvent is passed to Page.OnContextMenu listeners as the page's
// context menu is about to open: what was right-clicked, and Menu, the
// menu the web view is about to show, which listeners change or replace.
// Preventing the event shows no menu.
type ContextMenuEvent struct {
	Preventable
	// LinkURL and LinkText are those of the link clicked, ImageURL that
	// of the image, MediaURL that of the video or audio.
	LinkURL  string
	LinkText string
	ImageURL string
	MediaURL string
	// SelectionText is the text selected where the menu opened.
	SelectionText string
	// Editable is true over a text field or editable content, whose menu
	// has the web view's editing items.
	Editable bool
	// FrameURL is the URL of the frame (an iframe) clicked, "" in the
	// page's own document. Windows tells it; macOS and Linux leave it "".
	FrameURL string
	// Menu holds the web view's own items, each with its ID (ContextMenu*
	// names), its label and its state, as the platform made them: hide,
	// rename, disable or move them, remove them with Menu.Remove, and add
	// the app's own with Append and Insert. The web view's items do what
	// the web view does, unless the app gives one a Click of its own, which
	// runs in its place. Setting Menu to a menu of the app's own shows that
	// instead, and nil shows none, as PreventDefault does.
	Menu *Menu
}

// OnContextMenu is called as the page's context menu is about to open,
// on a right-click, a Control-click on macOS, or the menu key: to change
// the menu the web view shows, as in Electron's context-menu event. With
// no listener, the page shows the web view's menu as it is: Reload, Back
// and Forward, Inspect Element in builds with DevTools, and what a link,
// an image, a selection or a text field adds, as Look Up, Translate,
// spelling suggestions and Services on macOS.
//
//	page.OnContextMenu(func(e *mygo.ContextMenuEvent) {
//		e.Menu.Remove(mygo.ContextMenuReload, mygo.ContextMenuBack, mygo.ContextMenuForward)
//		if e.LinkURL != "" {
//			e.Menu.Append(mygo.Separator(), &mygo.MenuItem{Label: "Open in Browser", Click: func(*mygo.MenuItem, *mygo.Window) {
//				go mygo.Shell.OpenExternal(e.LinkURL)
//			}})
//		}
//	})
//
// A page's own contextmenu handler that calls preventDefault shows none
// either: the listener is not called. Listeners run on the main thread
// while the menu waits, so they must not block; the app's items' Click
// runs once one is chosen, with the window.
//
// What each platform tells and does:
//
//   - macOS: what was clicked comes from the page's contextmenu event,
//     which MyGo's runtime in the page reports just before, in the page's
//     own document: a click in a frame tells nothing. MyGo changes WebKit's
//     menu in place (-[NSView willOpenMenu:withEvent:]), whose items keep
//     Look Up, Translate, Services and the spelling suggestions working.
//   - Linux: WebKitGTK's hit test tells the link, image, media and
//     whether it is editable, and the page's contextmenu event the
//     selection. MyGo changes WebKitGTK's menu in place
//     (WebKitWebView::context-menu); WebKitGTK shows no shortcut or icon
//     for the app's items, renames only items of its stock actions, and
//     shows radio items as check boxes.
//   - Windows: WebView2 tells all of it (ICoreWebView2_11's
//     ContextMenuRequested). A menu the app changed is drawn by MyGo, as
//     its other menus, with WebView2's items doing what they do; one the
//     app left as it was is WebView2's own. A WebView2 Runtime without it
//     never calls fn.
func (p *Page) OnContextMenu(fn func(e *ContextMenuEvent)) (off func()) {
	contextMenusOnce.Do(func() {
		contextMenus = contextMenu
		linkContextMenus()
	})
	return p.w.onContextMenu.add(fn, false)
}

var (
	contextMenusOnce sync.Once
	// contextMenus is contextMenu once an app listens to context menus,
	// which leaves its code, and the backend's, out of apps that don't. It
	// is set before the first listener is added, which
	// windowHandler.ContextMenu checks first.
	contextMenus func(w *Window, m *platform.ContextMenu) platform.ContextMenuResponse
)

func (h *windowHandler) HandlesContextMenu() bool { return h.w.onContextMenu.len() > 0 }

func (h *windowHandler) ContextMenu(m *platform.ContextMenu) platform.ContextMenuResponse {
	if h.w.onContextMenu.len() == 0 {
		h.w.contextTarget = nil
		return platform.ContextMenuResponse{Default: true}
	}
	return contextMenus(h.w, m)
}

// contextTarget is what the bridge recorded of the page's latest
// contextmenu event (bridge.go): at X, Y in CSS pixels of the viewport.
type contextTarget struct {
	platform.ContextMenuTarget
	X, Y float64
}

// contextMenu tells the listeners of a page's context menu about it, and
// returns what they made of it. Main thread only.
func contextMenu(w *Window, m *platform.ContextMenu) platform.ContextMenuResponse {
	t := m.Target
	if r := w.contextTarget; r != nil && !m.Complete && r.of(m, w) {
		fill(&t.LinkURL, r.LinkURL)
		fill(&t.LinkText, r.LinkText)
		fill(&t.ImageURL, r.ImageURL)
		fill(&t.MediaURL, r.MediaURL)
		fill(&t.SelectionText, r.SelectionText)
		t.Editable = t.Editable || r.Editable
	}
	w.contextTarget = nil
	menu := NewMenu(contextItems(m.Items))
	menu.transient = true
	e := &ContextMenuEvent{
		LinkURL: t.LinkURL, LinkText: t.LinkText, ImageURL: t.ImageURL, MediaURL: t.MediaURL,
		SelectionText: t.SelectionText, Editable: t.Editable, FrameURL: t.FrameURL, Menu: menu,
	}
	fire1(&w.onContextMenu, e)
	if e.prevented || e.Menu == nil {
		return platform.ContextMenuResponse{}
	}
	// The app's items are found by their weak IDs as they are chosen,
	// after the menu closed: the window keeps them until its next menu.
	w.shownContextMenu = e.Menu
	snap := e.Menu.snapshot()
	shown := map[int]bool{}
	onceEach(snap, shown)
	if sameItems(snap.Items, m.Items) {
		return platform.ContextMenuResponse{Default: true}
	}
	snap.TidySeparators()
	return platform.ContextMenuResponse{Menu: snap}
}

func fill(s *string, v string) {
	if *s == "" {
		*s = v
	}
}

// of reports whether the bridge recorded the event of the menu m opens
// for: the page's own document hears none of a click in a frame, and one
// it cancelled opened no menu, so its record stays until the next. The
// record of a menu the keyboard opened is that of the latest event.
func (r *contextTarget) of(m *platform.ContextMenu, w *Window) bool {
	if !m.Pointer {
		return true
	}
	zoom := 1.0
	if w.native != nil {
		if z := w.native.Zoom(); z > 0 {
			zoom = z
		}
	}
	// The event is in CSS pixels, which the page's zoom makes DIPs, and
	// may be rounded to whole ones.
	near := 2 * max(zoom, 1)
	return math.Abs(r.X*zoom-m.X) <= near && math.Abs(r.Y*zoom-m.Y) <= near
}

// contextItems makes the templates of the web view's items.
func contextItems(items []*platform.MenuItem) []*MenuItem {
	out := make([]*MenuItem, 0, len(items))
	for _, it := range items {
		m := &MenuItem{
			ID: it.Name, Label: it.Label, Accelerator: it.Accelerator, Type: MenuItemNormal,
			Disabled: !it.Enabled, Hidden: !it.Visible, Checked: it.Checked, native: it.Native,
		}
		switch it.Type {
		case platform.MenuItemSeparator:
			m.Type = MenuItemSeparator
		case platform.MenuItemCheckbox:
			m.Type = MenuItemCheckbox
		case platform.MenuItemRadio:
			m.Type = MenuItemRadio
		}
		if it.Submenu != nil {
			m.Type, m.Submenu = MenuItemSubmenu, contextItems(it.Submenu.Items)
		}
		out = append(out, m)
	}
	return out
}

// onceEach drops the web view's items shown already from m and its
// submenus: a native item is in one place at most.
func onceEach(m *platform.Menu, shown map[int]bool) {
	kept := m.Items[:0]
	for _, it := range m.Items {
		if it.Native != 0 {
			if shown[it.Native] {
				continue
			}
			shown[it.Native] = true
		}
		if it.Submenu != nil {
			onceEach(it.Submenu, shown)
		}
		kept = append(kept, it)
	}
	m.Items = kept
}

// sameItems reports whether a menu shows the web view's items as they
// were, which the web view then shows itself.
func sameItems(a, b []*platform.MenuItem) bool {
	if len(a) != len(b) {
		return false
	}
	for i, x := range a {
		y := b[i]
		if x.Native == 0 || x.Native != y.Native || !x.Visible || x.Label != y.Label || x.Enabled != y.Enabled ||
			x.Checked != y.Checked || x.Accelerator != y.Accelerator || (x.Submenu == nil) != (y.Submenu == nil) {
			return false
		}
		if x.Submenu != nil && !sameItems(x.Submenu.Items, y.Submenu.Items) {
			return false
		}
	}
	return true
}
