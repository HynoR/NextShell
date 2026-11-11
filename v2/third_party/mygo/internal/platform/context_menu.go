package platform

// ContextMenu is a page's context menu about to open
// (WindowHandler.ContextMenu): what was clicked, as far as the backend
// tells, and the web view's own items.
type ContextMenu struct {
	// Pointer is set for a menu the pointer opened, at X, Y in DIPs from
	// the top-left corner of the web view; a menu the keyboard opened has
	// none.
	Pointer bool
	X, Y    float64
	// Target is what the backend tells of what was clicked. The core takes
	// what it leaves empty from the page's own contextmenu event, which
	// the bridge records just before, unless Complete says it told all:
	// the event of another frame than the page's own never reaches it.
	Target   ContextMenuTarget
	Complete bool
	// Items are the web view's items, each with its Native number.
	Items []*MenuItem
}

// ContextMenuTarget is what a context menu was opened on.
type ContextMenuTarget struct {
	// LinkURL and LinkText are those of the link clicked; ImageURL of
	// the image, MediaURL of the video or audio.
	LinkURL, LinkText string
	ImageURL          string
	MediaURL          string
	// SelectionText is the text selected where the menu opened.
	SelectionText string
	// Editable is set over a text field or editable content.
	Editable bool
	// FrameURL is the URL of the frame clicked, when it is not the
	// page's own document but a frame in it.
	FrameURL string
}

// ContextMenuResponse answers a ContextMenu. Default shows the web view's
// menu as it is, as if nobody had been asked; otherwise Menu is the menu
// to show, of the web view's items (by Native, each once) and the app's,
// or nil to show none.
type ContextMenuResponse struct {
	Default bool
	Menu    *Menu
}

// TidySeparators drops the separators of a menu shown that would start or
// end it, or follow another, as the items removed between them leave:
// Remove(ContextMenuBack, ContextMenuForward, ContextMenuReload) leaves the
// one after them first.
func (m *Menu) TidySeparators() {
	kept := m.Items[:0]
	afterItem := false // a visible item other than a separator came last
	for _, it := range m.Items {
		if it.Submenu != nil {
			it.Submenu.TidySeparators()
		}
		if it.Visible {
			sep := it.Type == MenuItemSeparator
			if sep && !afterItem {
				continue
			}
			afterItem = !sep
		}
		kept = append(kept, it)
	}
	if !afterItem { // a separator came last
		for i := len(kept) - 1; i >= 0; i-- {
			if kept[i].Visible {
				kept = append(kept[:i], kept[i+1:]...)
				break
			}
		}
	}
	m.Items = kept
}
