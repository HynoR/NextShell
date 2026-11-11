//go:build linux && (amd64 || arm64)

package linux

import (
	"runtime"
	"sync"
	"unsafe"

	"github.com/egoist/mygo/internal/platform"
)

// The icons of menu items. A GTK menu item shows its icon in a box with
// its label, the image first, as GTK 3 advises in place of the deprecated
// GtkImageMenuItem: GTK shows it whatever gtk-menu-images says, and a
// check item draws its check before the box. A tray's menu is the panel's
// to draw, which libdbusmenu exports: its items get their icon as the
// "icon-data" property of the items exported (a PNG), which the parser of
// GTK menus would not give a check item.

// setMenuItemIcon is setItemIcon once LinkMenuIcons links it: the
// backend's methods are linked into every app, and only call it.
var setMenuItemIcon func(n *nativeItem, ic *platform.MenuIcon)

// LinkMenuIcons links the code of the icons of menu items, when the app
// first gives an item one.
func LinkMenuIcons() { setMenuItemIcon = setItemIcon }

var (
	menuIconOnce sync.Once

	gtkBinGetChild                       func(w ptr) ptr
	gtkImageNew                          func() ptr
	gtkImageSetFromSurface               func(img, surface ptr)
	gtkLabelSetLabel                     func(label ptr, s *byte)
	gtkMenuItemScale                     func(w ptr) int32
	gdkCairoSurfaceCreateFromPixbuf      func(pixbuf ptr, scale int32, win ptr) ptr
	gdkPixbufScaleSimple                 func(src ptr, w, h int32, interp int32) ptr
	gdkPixbufGetWidth                    func(p ptr) int32
	gdkPixbufGetHeight                   func(p ptr) int32
	dbusmenuGtkParseGetCachedItem        func(widget ptr) ptr
	dbusmenuMenuitemPropertySetByteArray func(mi ptr, prop *byte, data unsafe.Pointer, n uintptr) bool
	dbusmenuMenuitemPropertyRemove       func(mi ptr, prop *byte)
)

func loadMenuIcons() {
	menuIconOnce.Do(func() {
		mustBind(libGTK, &gtkBinGetChild, "gtk_bin_get_child")
		mustBind(libGTK, &gtkImageNew, "gtk_image_new")
		mustBind(libGTK, &gtkImageSetFromSurface, "gtk_image_set_from_surface")
		mustBind(libGTK, &gtkLabelSetLabel, "gtk_label_set_label")
		mustBind(libGTK, &gtkMenuItemScale, "gtk_widget_get_scale_factor")
		mustBind(libGDK, &gdkCairoSurfaceCreateFromPixbuf, "gdk_cairo_surface_create_from_pixbuf")
		mustBind(libPixbuf, &gdkPixbufScaleSimple, "gdk_pixbuf_scale_simple")
		mustBind(libPixbuf, &gdkPixbufGetWidth, "gdk_pixbuf_get_width")
		mustBind(libPixbuf, &gdkPixbufGetHeight, "gdk_pixbuf_get_height")
		// AppIndicator loaded libdbusmenu: these find it.
		if lib, err := open("libdbusmenu-gtk3.so.4"); err == nil {
			bind(lib, &dbusmenuGtkParseGetCachedItem, "dbusmenu_gtk_parse_get_cached_item")
		}
		if lib, err := open("libdbusmenu-glib.so.4"); err == nil {
			bind(lib, &dbusmenuMenuitemPropertySetByteArray, "dbusmenu_menuitem_property_set_byte_array")
			bind(lib, &dbusmenuMenuitemPropertyRemove, "dbusmenu_menuitem_property_remove")
		}
	})
}

// iconPixbufs is what an icon is decoded to, once (MenuIcon.Native): the
// image, its sizes asked for, and the PNG a tray's menu exports.
type iconPixbufs struct {
	image ptr
	sized map[int32]ptr
	tray  []byte
}

func (p *iconPixbufs) free() {
	gObjectUnref(p.image)
	for _, s := range p.sized {
		if s != 0 {
			gObjectUnref(s)
		}
	}
}

func iconPixbufsOf(ic *platform.MenuIcon) *iconPixbufs {
	p, _ := ic.Native(func() any {
		pix, err := pixbufFromPNG(ic.PNG)
		if err != nil {
			return nil
		}
		p := &iconPixbufs{image: pix, sized: map[int32]ptr{}}
		// The pixbufs' references may be dropped on any thread.
		runtime.AddCleanup(ic, (*iconPixbufs).free, p)
		return p
	}).(*iconPixbufs)
	return p
}

// iconPixbuf returns an icon's image fit to a box of box pixels, or 0.
func iconPixbuf(ic *platform.MenuIcon, box int32) ptr {
	p := iconPixbufsOf(ic)
	if p == nil {
		return 0
	}
	if s, ok := p.sized[box]; ok {
		return s
	}
	w, h := platform.FitIcon(int(gdkPixbufGetWidth(p.image)), int(gdkPixbufGetHeight(p.image)), int(box))
	const interpHyper = 3 // GDK_INTERP_HYPER, the finest when shrinking
	s := gdkPixbufScaleSimple(p.image, int32(w), int32(h), interpHyper)
	p.sized[box] = s
	return s
}

// trayIconPNG returns the PNG a tray's menu exports of an icon: the image,
// at most twice the size the panel draws it at.
func trayIconPNG(ic *platform.MenuIcon) []byte {
	p := iconPixbufsOf(ic)
	if p == nil {
		return nil
	}
	if p.tray == nil {
		p.tray = ic.PNG
		const box = 2 * platform.MenuIconSize
		if gdkPixbufGetWidth(p.image) > box || gdkPixbufGetHeight(p.image) > box {
			if s := iconPixbuf(ic, box); s != 0 {
				if png := pngFromPixbuf(s); png != nil {
					p.tray = png
				}
			}
		}
	}
	return p.tray
}

// menuItemImage returns the image of a GTK menu item, which it gets with a
// box holding it and the item's label, and the label.
func menuItemImage(n *nativeItem) (image, label ptr) {
	if n.image != 0 {
		return n.image, n.label
	}
	label = gtkBinGetChild(n.item)
	if label == 0 {
		return 0, 0
	}
	gObjectRef(label)
	gtkContainerRemove(n.item, label)
	box := gtkBoxNew(0, 6) // GTK_ORIENTATION_HORIZONTAL
	n.image = gtkImageNew()
	gtkWidgetSetNoShowAll(n.image, true)
	gtkBoxPackStart(box, n.image, false, false, 0)
	gtkBoxPackStart(box, label, true, true, 0)
	gObjectUnref(label)
	gtkContainerAdd(n.item, box)
	gtkWidgetShow(box)
	gtkWidgetShow(label)
	n.label = label
	return n.image, n.label
}

// setItemIcon shows an icon in a GTK menu item, nil for none.
func setItemIcon(n *nativeItem, ic *platform.MenuIcon) {
	loadMenuIcons()
	if n.windowID == 0 {
		setTrayItemIcon(n.item, ic)
		return
	}
	if ic == nil && n.image == 0 {
		return
	}
	image, _ := menuItemImage(n)
	if image == 0 {
		return
	}
	var surface ptr
	if ic != nil {
		scale := max(gtkMenuItemScale(n.item), 1)
		if pix := iconPixbuf(ic, platform.MenuIconSize*scale); pix != 0 {
			surface = gdkCairoSurfaceCreateFromPixbuf(pix, scale, 0)
		}
	}
	gtkImageSetFromSurface(image, surface)
	gtkWidgetSetVisible(image, surface != 0)
	if surface != 0 {
		cairoSurfaceDestroy(surface)
	}
}

// setTrayItemIcon gives the item a tray's menu exports for a GTK menu item
// an icon, nil for none. A tray's items are exported once its menu is set.
func setTrayItemIcon(item ptr, ic *platform.MenuIcon) {
	if dbusmenuGtkParseGetCachedItem == nil || dbusmenuMenuitemPropertySetByteArray == nil || dbusmenuMenuitemPropertyRemove == nil {
		return
	}
	mi := dbusmenuGtkParseGetCachedItem(item)
	if mi == 0 {
		return
	}
	var png []byte
	if ic != nil {
		png = trayIconPNG(ic)
	}
	if len(png) == 0 {
		dbusmenuMenuitemPropertyRemove(mi, cs("icon-data"))
		return
	}
	dbusmenuMenuitemPropertySetByteArray(mi, cs("icon-data"), unsafe.Pointer(&png[0]), uintptr(len(png)))
	runtime.KeepAlive(png)
}
