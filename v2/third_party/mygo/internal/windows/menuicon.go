//go:build windows && (amd64 || arm64)

package windows

import (
	"unsafe"

	"github.com/egoist/mygo/internal/menuicon"
	"github.com/egoist/mygo/internal/platform"
)

// The icons of menu items. Windows 11's popup menus, which MyGo draws
// (menudraw.go), draw them in a column of their own, between the checks'
// and the labels, as WinUI's MenuFlyoutItem does; Windows 10's draw them
// as the item's bitmap (hbmpItem), a DIB section of premultiplied pixels,
// which themed menus draw with their alpha, where they draw checks.

const (
	miimBitmap     = 0x0080
	menuIconColumn = platform.MenuIconSize + 12 // the icon, and the gap after it
)

// iconPixels is what an icon is decoded to, once (MenuIcon.Native), with
// its pixels at the sizes and in the colors menus asked for.
type iconPixels struct {
	img   *menuicon.Image
	sized map[iconKey]*menuicon.Image
}

type iconKey struct {
	box  int32
	tint uint32 // 0xRRGGBB for a template icon; 1<<24 for none
}

const noTint = 1 << 24

// menuIconBitmap and drawMenuIcon are the code of the icons, which
// LinkMenuIcons links: the backend's methods are linked into every app,
// and only call them.
var (
	menuIconBitmap func(ic *platform.MenuIcon, dpi int, tint uint32) uintptr
	drawMenuIcon   func(px []uint32, stride int32, ic *platform.MenuIcon, x, y, box int32, tint uint32, opacity float64)
)

// LinkMenuIcons links the code of the icons of menu items, when the app
// first gives an item one.
func LinkMenuIcons() { menuIconBitmap, drawMenuIcon = iconBitmap, drawItemIcon }

// drawItemIcon draws an icon over an item's pixels (drawIcon), a template
// one in the color tint.
func drawItemIcon(px []uint32, stride int32, ic *platform.MenuIcon, x, y, box int32, tint uint32, opacity float64) {
	if img := iconAt(ic, box, tint); img != nil {
		drawIcon(px, stride, img, x, y, box, opacity)
	}
}

// iconAt returns the pixels of an icon fit to a box of box pixels, a
// template one in the color tint (0xRRGGBB), or nil for an image that
// cannot be decoded.
func iconAt(ic *platform.MenuIcon, box int32, tint uint32) *menuicon.Image {
	p, _ := ic.Native(func() any {
		img, err := menuicon.Decode(ic.PNG)
		if err != nil {
			return nil
		}
		return &iconPixels{img: img, sized: map[iconKey]*menuicon.Image{}}
	}).(*iconPixels)
	if p == nil || box <= 0 {
		return nil
	}
	if !ic.Template {
		tint = noTint
	}
	k := iconKey{box, tint}
	if img := p.sized[k]; img != nil {
		return img
	}
	w, h := platform.FitIcon(p.img.W, p.img.H, int(box))
	img := p.img.Scale(w, h)
	if tint != noTint {
		img = img.Tint(tint)
	}
	p.sized[k] = img
	return img
}

// drawIcon draws an icon's pixels over those of an opaque bitmap of width
// stride, centered in the box of box pixels at x, y, at an opacity.
func drawIcon(px []uint32, stride int32, img *menuicon.Image, x, y, box int32, opacity float64) {
	x += (box - int32(img.W)) / 2
	y += (box - int32(img.H)) / 2
	height := int32(len(px)) / stride
	for sy := range int32(img.H) {
		dy := y + sy
		if dy < 0 || dy >= height {
			continue
		}
		for sx := range int32(img.W) {
			dx := x + sx
			if dx < 0 || dx >= stride {
				continue
			}
			if p := img.Px[int(sy)*img.W+int(sx)]; p>>24 != 0 {
				px[dy*stride+dx] = menuicon.Over(p, px[dy*stride+dx], opacity)
			}
		}
	}
}

// iconBitmap makes the bitmap of an item of Windows 10's menus: a 32-bit
// DIB section of the icon's premultiplied pixels, top-down, which the
// menu's owner frees (menuTable.drop).
func iconBitmap(ic *platform.MenuIcon, dpi int, tint uint32) uintptr {
	img := iconAt(ic, toPx(platform.MenuIconSize, dpi), tint)
	if img == nil || img.W == 0 {
		return 0
	}
	hdr := bitmapInfoHeader{Width: int32(img.W), Height: -int32(img.H), Planes: 1, BitCount: 32}
	hdr.Size = uint32(unsafe.Sizeof(hdr))
	var bits uintptr
	bmp, _, _ := procCreateDIBSection.Call(0, uintptr(unsafe.Pointer(&hdr)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bmp == 0 || bits == 0 {
		return 0
	}
	copy(unsafe.Slice((*uint32)(native(bits)), len(img.Px)), img.Px)
	return bmp
}

// menuTextColor is the color, 0xRRGGBB, of the text of Windows 10's menus,
// which template icons take.
func (b *Backend) menuTextColor() uint32 {
	if b.isDark() && !highContrastOn() {
		return 0xFFFFFF
	}
	return dibColor(sysColor(colorMenuText))
}

// setItemBitmap gives the item at pos of menu (by position) or of command
// cmd the bitmap bmp, 0 for none.
func setItemBitmap(menu uintptr, item uint32, byPosition bool, bmp uintptr) {
	mi := menuItemInfo{Mask: miimBitmap, BmpItem: bmp}
	mi.Size = uint32(unsafe.Sizeof(mi))
	pos := uintptr(0)
	if byPosition {
		pos = 1
	}
	procSetMenuItemInfoW.Call(menu, uintptr(item), pos, uintptr(unsafe.Pointer(&mi)))
}

// addBitmap makes the bitmap of an icon for a menu of owner, which frees it
// as it drops its items.
func (t *menuTable) addBitmap(owner int, ic *platform.MenuIcon, dpi int, tint uint32) uintptr {
	if menuIconBitmap == nil {
		return 0
	}
	bmp := menuIconBitmap(ic, dpi, tint)
	if bmp != 0 {
		if t.bitmaps == nil {
			t.bitmaps = map[uintptr]int{}
		}
		t.bitmaps[bmp] = owner
	}
	return bmp
}

// freeBitmap frees a bitmap addBitmap made.
func (t *menuTable) freeBitmap(bmp uintptr) {
	if _, ok := t.bitmaps[bmp]; ok {
		delete(t.bitmaps, bmp)
		procDeleteObject.Call(bmp)
	}
}

// iconColumn gives the items MyGo draws of a menu, which had no icon, a
// column for icons, as one gets one: set again, Windows measures them
// again.
func (t *menuTable) iconColumn(menu uintptr) {
	for key, l := range t.looks {
		if l.menu == menu && !l.icons {
			l.icons = true
			ownerDraw(menu, l.pos, l.ftype(), key)
		}
	}
}
