package platform

// MenuIconSize is the box, in points (DIPs), that a menu item's icon is
// drawn to fit, keeping its aspect: the size of the icons of the
// system's own menus on macOS, Windows and Linux.
const MenuIconSize = 16

// MenuIcon is the image a menu item shows beside its label. Package mygo
// shares one between the snapshots of the items showing the same image,
// and makes a new one when an item's image changes, so a backend tells a
// change by the pointer and decodes each image once (Native).
type MenuIcon struct {
	// PNG is the image, its bytes never changed once made.
	PNG []byte
	// Template is a black and transparent image, which takes the color of
	// the menu's text where the backend can tint it.
	Template bool

	native any
	made   bool
}

// Native returns what make made of the icon the first time it was asked:
// a backend's decoding of it, kept for as long as the icon is, which
// make may free with runtime.AddCleanup. A nil result is kept too, as an
// image that cannot be decoded. Backends call it on the main thread.
func (ic *MenuIcon) Native(make func() any) any {
	if !ic.made {
		ic.native, ic.made = make(), true
	}
	return ic.native
}

// FitIcon returns the size of an image of w by h pixels drawn to fit a box
// of box by box pixels, keeping its aspect, at least a pixel each way.
func FitIcon(w, h, box int) (int, int) {
	if w <= 0 || h <= 0 || box <= 0 {
		return 0, 0
	}
	if w >= h {
		return box, max(1, (h*box+w/2)/w)
	}
	return max(1, (w*box+h/2)/h), box
}

// LabelWithSubtitle is the label of a backend that can't show the subtitle
// on a line of its own.
func (it *MenuItem) LabelWithSubtitle() string {
	if it.Subtitle == "" {
		return it.Label
	}
	return it.Label + " — " + it.Subtitle
}

// Apply gives the items of the menu that it is a snapshot of, by ID, its
// state: a backend that keeps a snapshot to build menus from later (a
// tray's, the application menu for new windows) keeps it current with the
// updates it is given.
func (m *Menu) Apply(it *MenuItem) {
	if m == nil {
		return
	}
	for i, x := range m.Items {
		if x.ID == it.ID {
			c := *it
			m.Items[i] = &c
			continue
		}
		x.Submenu.Apply(it)
	}
}
