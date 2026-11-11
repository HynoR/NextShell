//go:build windows && (amd64 || arm64)

package windows

import (
	"math"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"github.com/egoist/mygo/internal/platform"
)

// Windows 11 still draws Win32 menus as Windows 10 did, while its own menus
// (WinUI's MenuFlyout and MenuBar, File Explorer's context menu) have more
// room, highlights with rounded corners inset from the edges, shortcuts
// smaller and in a fainter color, the corners of flyouts, and Acrylic
// behind them. MyGo draws the items of its popup menus so there
// (MFT_OWNERDRAW), and those of the menu bar (darkmenu.go), and gives the
// menus' windows those corners and, from Windows 11 22H2, the material
// (menuGlass). The menus themselves stay Windows': their windows,
// animations and keyboard, and the items keep their strings, which
// mnemonics, screen readers and the tests read. Windows 10 keeps its own
// menus.

const (
	wmInitMenuPopup = 0x0117
	wmMeasureItem   = 0x002C
	wmDrawItem      = 0x002B

	odtMenu                = 1
	odsChecked             = 0x0008
	miimData               = 0x0020
	mftOwnerDraw           = 0x0100
	mftSeparator           = 0x0800
	mimBackground          = 0x0002
	dtCalcRect             = 0x400
	dtRight                = 0x002
	dtNoClip               = 0x100
	srcCopy                = 0x00CC0020
	spiGetNonClientMetrics = 0x0029
	layoutRTL              = 0x1
)

var (
	procBitBlt                     = gdi32.NewProc("BitBlt")
	procGetLayout                  = gdi32.NewProc("GetLayout")
	procExcludeClipRect            = gdi32.NewProc("ExcludeClipRect")
	procCreateFontIndirectW        = gdi32.NewProc("CreateFontIndirectW")
	procSetMenuInfo                = user32.NewProc("SetMenuInfo")
	procGetSysColor                = user32.NewProc("GetSysColor")
	procSystemParametersInfoForDpi = user32.NewProc("SystemParametersInfoForDpi")
	procGetTextMetricsW            = gdi32.NewProc("GetTextMetricsW")
)

// Popup items' metrics, in DIPs, after WinUI's MenuFlyoutItem (its
// margin of 4,2,4,2, padding of 11 at the sides, and 32 from one item to
// the next): a highlight inset from the menu's sides and from the next
// items with rounded corners, the text past its padding, shortcuts in
// WinUI's Caption, and marks (checks, the submenus' chevrons) in a column
// of their own. Separators are File Explorer's, 9 tall.
const (
	menuItemHeight      = 32
	menuSeparatorHeight = 9
	menuInset           = 4  // the highlight from the menu's sides
	menuItemMargin      = 2  // the highlight from the item's top and bottom
	menuEdge            = 2  // the menu's padding above its first item and below its last
	menuPadding         = 11 // the text from the highlight's sides
	menuRadius          = 4
	menuMarkColumn      = 16 + 12 // a check, and the gap after it
	menuAccelGap        = 24      // the least between a label and its shortcut
	menuChevronGap      = 24      // the least between a label and a submenu's chevron
	menuChevron         = 12
	menuGlyphSize       = 12
	// menuLabelSize and menuAccelSize are the sizes of WinUI's labels
	// (Body) and shortcuts (Caption), to the 12 of the system's menu font.
	menuLabelSize = 14
	menuAccelSize = 12
)

// The menu bar's, which grows to WinUI's room.
const (
	menuBarHeight = 30
	menuBarExtra  = 8 // wider than Windows' items
	menuBarInsetX = 2
	menuBarInsetY = 3
)

// fluentMenus reports whether MyGo draws the menus in Windows 11's style.
func fluentMenus() bool { return windows11() }

// menuLook is what an item MyGo draws shows.
type menuLook struct {
	owner     int
	label     string // with its mnemonic's &
	accel     string
	separator bool
	submenu   bool
	radio     bool
	// column is whether the menu has a column for the marks of checkable
	// items, which all its items leave room for.
	column bool
	// icon is the item's icon, and icons whether the menu has a column
	// for icons, after that of the marks (menuicon.go).
	icon  *platform.MenuIcon
	icons bool
	// first and last are whether the item is the menu's first or last,
	// which has the menu's padding above or below it.
	first, last bool
	// menu and pos are the menu holding the item and its position there.
	menu uintptr
	pos  int
	// width is the width the item asked for when Windows measured it last,
	// at dpi, 0 before.
	width int32
	dpi   int
}

// ftype returns the type the item has besides MFT_OWNERDRAW.
func (l *menuLook) ftype() uint32 {
	switch {
	case l.separator:
		return mftSeparator
	case l.radio:
		return mftRadioCheck
	}
	return 0
}

// padding returns the room above and below the item: the menu's padding
// above its first item and below its last, which with the highlight's
// margin puts it as far from the menu's top and bottom as from its sides.
func (l *menuLook) padding(dpi int) (top, bottom int32) {
	if l.first {
		top = toPx(menuEdge, dpi)
	}
	if l.last {
		bottom = toPx(menuEdge, dpi)
	}
	return top, bottom
}

// addLook registers the look of an item and returns its key, the item's
// data.
func (t *menuTable) addLook(l *menuLook) uintptr {
	if t.looks == nil {
		t.looks = map[uintptr]*menuLook{}
	}
	t.nextLook++
	t.looks[t.nextLook] = l
	return t.nextLook
}

// ownerDraw makes the item at pos of menu one MyGo draws, of type ftype
// (MFT_SEPARATOR, MFT_RADIOCHECK or 0) besides, with the look of key.
func ownerDraw(menu uintptr, pos int, ftype uint32, key uintptr) {
	mi := menuItemInfo{Mask: miimFType | miimData, Type: ftype | mftOwnerDraw, ItemData: key}
	mi.Size = uint32(unsafe.Sizeof(mi))
	procSetMenuItemInfoW.Call(menu, uintptr(pos), 1, uintptr(unsafe.Pointer(&mi)))
}

type menuItemInfo struct {
	Size         uint32
	Mask         uint32
	Type         uint32
	State        uint32
	ID           uint32
	SubMenu      uintptr
	BmpChecked   uintptr
	BmpUnchecked uintptr
	ItemData     uintptr
	TypeData     *uint16
	Cch          uint32
	BmpItem      uintptr
}

type measureItemStruct struct {
	CtlType, CtlID, ItemID, ItemWidth, ItemHeight uint32
	ItemData                                      uintptr
}

// menuPalette is the colors of popup menus, COLORREFs: the background of
// WinUI's menus where the material doesn't show behind them (the fallback
// of its Acrylic, AcrylicBackgroundFillColorDefault), their outline, and
// WinUI's colors opaque over them. A contrast theme's are the system's.
type menuPalette struct {
	back, plate, text, plateText, secondary, disabled, line uint32
	// border is the menu's one outline, a pixel of WinUI's
	// SurfaceStrokeColorFlyout (#00000019 in the light appearance,
	// #FFFFFF19 in the dark) over the background, which DWM draws around
	// the rounded window, DWMWA_COLOR_DEFAULT for DWM's own. A color of
	// its own also keeps the accent off it, which DWM would draw around a
	// window it frames active with "Show accent color on title bars and
	// window borders" on, as the menus' windows are (activateMenuFrame).
	border uint32
	// wash and rule are the highlight and the separators of a menu with
	// the material behind it (menuGlass): WinUI's translucent fills
	// (SubtleFillColorSecondary, DividerStrokeColorDefault) over it,
	// which the opaque plate and line are over the background.
	wash, rule tint
	// veil is WinUI's Acrylic tint over the material: the fallback color at
	// the opacity of its luminosity layer, so that a menu is as light as
	// the shell's menus and flyouts, which the desktop shows a seventh of
	// the way through, rather than as gray as the material alone is.
	veil tint
}

// tint is a color, a COLORREF, at an opacity from 0 to 1.
type tint struct {
	color uint32
	alpha float64
}

const (
	colorMenu          = 4
	colorMenuText      = 7
	colorGrayText      = 17
	colorHighlight     = 13
	colorHighlightText = 14
)

func sysColor(i uintptr) uint32 {
	c, _, _ := procGetSysColor.Call(i)
	return uint32(c)
}

func (b *Backend) menuPalette() menuPalette {
	switch {
	case highContrastOn():
		text := sysColor(colorMenuText)
		return menuPalette{back: sysColor(colorMenu), plate: sysColor(colorHighlight), text: text,
			plateText: sysColor(colorHighlightText), secondary: text, disabled: sysColor(colorGrayText), line: text,
			border: dwmColorDefault}
	}
	return fluentPalette(b.isDark())
}

// fluentPalette returns the colors of popup menus in the appearance, dark
// or light, without a contrast theme.
func fluentPalette(dark bool) menuPalette {
	if dark {
		return menuPalette{back: 0x2C2C2C, plate: 0x383838, text: 0xFFFFFF, plateText: 0xFFFFFF,
			secondary: 0xCFCFCF, disabled: 0x787878, line: 0x3D3D3D, border: 0x414141,
			wash: tint{0xFFFFFF, 0x0F / 255.0}, rule: tint{0xFFFFFF, 0x15 / 255.0},
			veil: tint{0x2C2C2C, acrylicLuminosity}}
	}
	return menuPalette{back: 0xF9F9F9, plate: 0xF0F0F0, text: 0x1A1A1A, plateText: 0x1A1A1A,
		secondary: 0x5F5F5F, disabled: 0x9F9F9F, line: 0xEAEAEA, border: 0xE0E0E0,
		wash: tint{0x000000, 0x09 / 255.0}, rule: tint{0x000000, 0x0F / 255.0},
		veil: tint{0xF9F9F9, acrylicLuminosity}}
}

// acrylicLuminosity is how much of WinUI's Acrylic is its tint, over the
// material, rather than the material's own color (AcrylicBrush's
// TintLuminosityOpacity, 0.85 in the light and the dark appearance).
const acrylicLuminosity = 0.85

// menuBrush returns a brush of a color, made once.
func (b *Backend) menuBrush(color uint32) uintptr {
	if br, ok := b.menuBrushes[color]; ok {
		return br
	}
	br, _, _ := procCreateSolidBrush.Call(uintptr(color))
	if b.menuBrushes == nil {
		b.menuBrushes = map[uint32]uintptr{}
	}
	b.menuBrushes[color] = br
	return br
}

// menuFonts are the fonts of popup items at a DPI: the labels, the
// shortcuts, and the glyphs of the marks.
type menuFonts struct {
	text, accel, glyphs uintptr
	fluent              bool // the glyphs are Segoe Fluent Icons'
}

// logFont is LOGFONTW.
type logFont struct {
	Height, Width, Escapement, Orientation, Weight       int32
	Italic, Underline, StrikeOut, CharSet                uint8
	OutPrecision, ClipPrecision, Quality, PitchAndFamily uint8
	FaceName                                             [32]uint16
}

// nonClientMetrics is NONCLIENTMETRICSW.
type nonClientMetrics struct {
	Size                                   uint32
	BorderWidth, ScrollWidth, ScrollHeight int32
	CaptionWidth, CaptionHeight            int32
	CaptionFont                            logFont
	SmCaptionWidth, SmCaptionHeight        int32
	SmCaptionFont                          logFont
	MenuWidth, MenuHeight                  int32
	MenuFont, StatusFont, MessageFont      logFont
	PaddedBorderWidth                      int32
}

// menuFace returns the fonts of popup items at dpi: WinUI's text, in the
// optical sizes it takes at their sizes (Text and Small) and at its 14
// pixels for the labels and 12 for the shortcuts, scaled by the size of
// text of Accessibility as the menu font is, where the menu font of the
// system is Segoe UI: Segoe UI Variable is what WinUI's text is on Windows
// 11, its CJK glyphs and the rest through the fonts Windows links.
//
// Where the menu font is another family, as Microsoft YaHei UI is on
// Chinese Windows, the labels keep it, at those sizes: its Latin letters
// are heavier than Segoe UI Variable's, which would leave the items lighter
// than the menus of Windows itself, which draw them in the menu font, next
// to which an app's menus show. The shortcuts keep WinUI's own anywhere,
// its Caption's optical size: they are Latin letters and digits, which the
// menu font's face draws larger at that size (Microsoft YaHei UI's do, by a
// sixth of the ink height of Segoe UI Variable Small's), and Windows itself
// draws them in WinUI's font on every language. Windows 10, without Segoe UI
// Variable, keeps the menu font's face for both.
//
// The menu font's quality comes with it, ClearType where the system smooths
// text so: the items' text is drawn over their background, which is opaque
// enough with the material behind them (paintItem) for the strokes to be as
// heavy as those of the shell's menus, rather than thinned by grayscale
// antialiasing.
func (b *Backend) menuFace(dpi int) menuFonts {
	if f, ok := b.menuFonts[dpi]; ok {
		return f
	}
	var ncm nonClientMetrics
	ncm.Size = uint32(unsafe.Sizeof(ncm))
	lf := logFont{Height: -toPx(12, dpi), Weight: 400}
	copy(lf.FaceName[:], utf16z("Segoe UI"))
	if r, _, _ := procSystemParametersInfoForDpi.Call(spiGetNonClientMetrics, uintptr(ncm.Size), uintptr(unsafe.Pointer(&ncm)), 0, uintptr(dpi)); r != 0 {
		lf = ncm.MenuFont
	}
	segoe := strings.EqualFold(string(utf16Decode(lf.FaceName[:])), "Segoe UI")
	// winui tells whether the font is WinUI's own, in the optical size
	// named, rather than the menu font's at that size.
	font := func(size float64, variable string, winui bool) uintptr {
		v := lf
		v.Height = int32(math.Round(float64(lf.Height) * size / 12))
		if winui {
			v.FaceName = [32]uint16{}
			copy(v.FaceName[:], utf16z(variable))
			f, _, _ := procCreateFontIndirectW.Call(uintptr(unsafe.Pointer(&v)))
			if fontFace(f, variable) {
				return f
			}
			procDeleteObject.Call(f)
			v.FaceName = lf.FaceName
		}
		f, _, _ := procCreateFontIndirectW.Call(uintptr(unsafe.Pointer(&v)))
		return f
	}
	f := menuFonts{text: font(menuLabelSize, "Segoe UI Variable Text", segoe),
		accel: font(menuAccelSize, "Segoe UI Variable Small", true)}
	size := -toPx(menuGlyphSize, dpi)
	f.glyphs, f.fluent = createFont(size, "Segoe Fluent Icons"), true
	if !fontFace(f.glyphs, "Segoe Fluent Icons") {
		procDeleteObject.Call(f.glyphs)
		f.glyphs, f.fluent = createFont(size, "Segoe MDL2 Assets"), false
	}
	if b.menuFonts == nil {
		b.menuFonts = map[int]menuFonts{}
	}
	b.menuFonts[dpi] = f
	return f
}

// dropMenuFonts forgets the fonts of popup items, for the next menus to
// follow a change of the system's, and how much Windows widens the items,
// which follows its menu font.
func (b *Backend) dropMenuFonts() {
	for _, f := range b.menuFonts {
		procDeleteObject.Call(f.text)
		procDeleteObject.Call(f.accel)
		procDeleteObject.Call(f.glyphs)
	}
	b.menuFonts, b.menuExtra = nil, nil
}

// The glyphs of Segoe Fluent Icons, also in Segoe MDL2 Assets: WinUI's
// check, and its submenus' chevrons.
const (
	glyphCheck       = 0xE73E // CheckMark
	glyphBullet      = 0xE915 // RadioBullet
	glyphChevron     = 0xE974 // ChevronRightMed
	glyphChevronLeft = 0xE973 // ChevronLeftMed
)

// menuItemMessage handles the messages of the popup items MyGo draws, which
// the menus send the window they show for.
func (b *Backend) menuItemMessage(hwnd uintptr, m uint32, wp, lp uintptr) (uintptr, bool) {
	switch m {
	case wmEnterMenuLoop:
		b.hookMenus(true)
		return 0, false
	case wmExitMenuLoop:
		b.hookMenus(false)
		return 0, false
	case wmInitMenuPopup:
		// The menu's background in the appearance, around its items. The
		// window menu is Windows'.
		if fluentMenus() && hiword(lp) == 0 {
			b.setMenuBackground(wp)
		}
		return 0, false
	case wmMeasureItem:
		mis := (*measureItemStruct)(native(lp))
		if mis.CtlType != odtMenu {
			return 0, false
		}
		l := b.menus.looks[mis.ItemData]
		if l == nil {
			return 0, false
		}
		b.measureItem(hwnd, l, mis)
		return 1, true
	case wmDrawItem:
		d := (*drawItemStruct)(native(lp))
		if d.CtlType != odtMenu {
			return 0, false
		}
		l := b.menus.looks[d.ItemData]
		if l == nil {
			return 0, false
		}
		b.drawItem(hwnd, l, d)
		return 1, true
	}
	return 0, false
}

// setMenuBackground paints the menu's background, around its items, in
// the appearance's color (WinUI's solid fallback for the material), or
// none where the material shows (dressMenu).
func (b *Backend) setMenuBackground(menu uintptr) {
	type menuInfo struct {
		Size, Mask, Style, CyMax uint32
		Back                     uintptr
		ContextHelpID            uint32
		MenuData                 uintptr
	}
	back := b.menuPalette().back
	if b.glassMenu(menu) {
		back = 0 // black, which DWM shows as transparent over the material
	}
	mi := menuInfo{Mask: mimBackground, Back: b.menuBrush(back)}
	mi.Size = uint32(unsafe.Sizeof(mi))
	procSetMenuInfo.Call(menu, uintptr(unsafe.Pointer(&mi)))
}

// textWidth measures text in a font, with its mnemonic's & left out.
func textWidth(dc, font uintptr, text string) int32 {
	if text == "" {
		return 0
	}
	old, _, _ := procSelectObject.Call(dc, font)
	defer procSelectObject.Call(dc, old)
	var r rect
	s := u16(text)
	procDrawTextW.Call(dc, uintptr(unsafe.Pointer(s)), ^uintptr(0), uintptr(unsafe.Pointer(&r)), dtCalcRect|dtSingleLine)
	return r.Right - r.Left
}

func (b *Backend) measureItem(hwnd uintptr, l *menuLook, mis *measureItemStruct) {
	dpi := dpiOf(hwnd)
	top, bottom := l.padding(dpi)
	l.dpi = dpi
	if l.separator {
		l.width = 0
		mis.ItemWidth, mis.ItemHeight = 0, uint32(toPx(menuSeparatorHeight, dpi)+top+bottom)
		return
	}
	// Windows widens every item past the width it asks for, by as much as
	// it doesn't tell, which isn't a check's width (SM_CXMENUCHECK): 26
	// pixels at 192 DPI on Windows 11, where a check is 31 (#922). Items
	// ask for what they need less what Windows widened those of the menus
	// shown at the DPI by, as they drew (learnMenuExtra), and for all they
	// need before one showed: the first menu is wider than it needs, none
	// narrower.
	w := b.itemWidth(l, dpi)
	if extra, ok := b.menuExtra[dpi]; ok {
		w = max(w-extra, 0)
	}
	l.width = w
	mis.ItemWidth, mis.ItemHeight = uint32(w), uint32(toPx(menuItemHeight, dpi)+top+bottom)
}

// itemWidth returns the width an item that isn't a separator needs at dpi:
// its text and marks, past the highlight's inset and padding.
func (b *Backend) itemWidth(l *menuLook, dpi int) int32 {
	f := b.menuFace(dpi)
	dc, _, _ := procGetDC.Call(0)
	defer procReleaseDC.Call(0, dc)
	w := toPx(menuInset+menuPadding, dpi)*2 + textWidth(dc, f.text, l.label)
	if l.column {
		w += toPx(menuMarkColumn, dpi)
	}
	if l.icons {
		w += toPx(menuIconColumn, dpi)
	}
	if l.accel != "" {
		w += toPx(menuAccelGap, dpi) + textWidth(dc, f.accel, l.accel)
	}
	if l.submenu {
		w += toPx(menuChevronGap+menuChevron, dpi)
	}
	return w
}

// learnMenuExtra learns how much Windows widened the items of a menu at
// dpi from one of them drawn w wide: all are as wide as the widest asked
// for, and that. It keeps the least it saw at the DPI.
func (b *Backend) learnMenuExtra(l *menuLook, dpi int, w int32) {
	widest := int32(0)
	for _, o := range b.menus.looks {
		if o.menu != l.menu {
			continue
		}
		if o.dpi != dpi { // measured at another DPI, or not yet
			return
		}
		widest = max(widest, o.width)
	}
	extra := w - widest
	if old, ok := b.menuExtra[dpi]; extra < 0 || ok && old <= extra {
		return
	}
	if b.menuExtra == nil {
		b.menuExtra = map[int]int32{}
	}
	b.menuExtra[dpi] = extra
}

func (b *Backend) drawItem(hwnd uintptr, l *menuLook, d *drawItemStruct) {
	dpi := dpiOf(hwnd)
	p := b.menuPalette()
	r := d.Item
	b.learnMenuExtra(l, dpi, r.Right-r.Left)
	state := d.ItemState
	selected := state&odsSelected != 0 && !l.separator
	inset := toPx(menuInset, dpi)
	top, bottom := l.padding(dpi)
	margin := toPx(menuItemMargin, dpi)
	var plate *rect
	if selected {
		plate = &rect{inset, top + margin, r.Right - r.Left - inset, r.Bottom - r.Top - bottom - margin}
	}
	glass := b.glassMenu(l.menu)
	fill := itemFill{back: p.back, plate: p.plate, alpha: 1}
	if glass {
		fill = itemFill{plate: p.wash.color, alpha: p.wash.alpha, glass: true, veil: p.veil}
	}
	paintItem(d.DC, r, fill, plate, float64(toPx(menuRadius, dpi)), func(img *bitmap, r rect) {
		dc := img.dc
		r.Top, r.Bottom = r.Top+top, r.Bottom-bottom
		if l.separator {
			thick := max(toPx(1, dpi), 1)
			y := r.Top + (r.Bottom-r.Top-thick)/2
			line := rect{r.Left + inset, y, r.Right - inset, y + thick}
			if glass {
				washRect(img, r.Right-r.Left, line, p.rule)
			} else {
				fillColor(dc, &line, p.line)
			}
			return
		}
		fore, secondary := p.text, p.secondary
		if selected {
			fore = p.plateText
			if highContrastOn() {
				secondary = fore
			}
		}
		if state&(odsGrayed|odsDisabled) != 0 {
			fore, secondary = p.disabled, p.disabled
		}
		f := b.menuFace(dpi)
		procSetBkMode.Call(dc, 1) // TRANSPARENT
		x := r.Left + toPx(menuInset+menuPadding, dpi)
		right := r.Right - toPx(menuInset+menuPadding, dpi)
		glyph := func(g uint16, box rect, color uint32) {
			old, _, _ := procSelectObject.Call(dc, f.glyphs)
			procSetTextColor.Call(dc, uintptr(color))
			procDrawTextW.Call(dc, uintptr(unsafe.Pointer(&g)), 1, uintptr(unsafe.Pointer(&box)), dtCenter|dtVCenter|dtSingleLine|dtNoPrefix)
			procSelectObject.Call(dc, old)
		}
		if l.column {
			if state&odsChecked != 0 {
				g := uint16(glyphCheck)
				if l.radio {
					g = glyphBullet
				}
				glyph(g, rect{x, r.Top, x + toPx(16, dpi), r.Bottom}, fore)
			}
			x += toPx(menuMarkColumn, dpi)
		}
		if l.icons {
			box := toPx(platform.MenuIconSize, dpi)
			if ic := l.icon; ic != nil && drawMenuIcon != nil {
				// A template icon in the text's color; one in color
				// faded when the item is disabled.
				opacity := 1.0
				if state&(odsGrayed|odsDisabled) != 0 && !ic.Template {
					opacity = 0.4
				}
				// In a mirrored DC, which lays the item out from the
				// right, x counts from the right edge of the bitmap.
				at := x
				if layout, _, _ := procGetLayout.Call(dc); layout&layoutRTL != 0 {
					at = r.Right - x - box
				}
				drawMenuIcon(img.px, r.Right-r.Left, ic, at, r.Top+(r.Bottom-r.Top-box)/2, box, dibColor(fore), opacity)
			}
			x += toPx(menuIconColumn, dpi)
		}
		if l.submenu {
			// A menu laid out right to left has a mirrored DC, which
			// places the glyphs from the right but draws them unmirrored:
			// its submenus open to the left.
			g := uint16(glyphChevron)
			if layout, _, _ := procGetLayout.Call(d.DC); layout&layoutRTL != 0 {
				g = glyphChevronLeft
			}
			glyph(g, rect{right - toPx(menuChevron, dpi), r.Top, right, r.Bottom}, fore)
		}
		old, _, _ := procSelectObject.Call(dc, f.text)
		defer procSelectObject.Call(dc, old)
		// The text goes as far as its advance, which the item is measured
		// by, but some glyphs' ink reaches past it: Microsoft YaHei UI's,
		// the menu font of Chinese Windows, by a pixel (#1028). DrawText
		// would cut that off at the end of the text's rectangle; the
		// padding past it has room for it.
		flags := uintptr(dtSingleLine | dtNoClip)
		if state&odsNoAccel != 0 {
			flags |= dtHidePrefix
		}
		procSetTextColor.Call(dc, uintptr(fore))
		// The label in the middle of the item, and its shortcut, smaller,
		// on its baseline.
		label := textMetricsOf(dc)
		y := r.Top + (r.Bottom-r.Top-label.Height)/2
		text := rect{x, y, right, y + label.Height}
		s := u16(l.label)
		procDrawTextW.Call(dc, uintptr(unsafe.Pointer(s)), ^uintptr(0), uintptr(unsafe.Pointer(&text)), flags)
		if l.accel != "" {
			procSelectObject.Call(dc, f.accel)
			accel := textMetricsOf(dc)
			y += label.Ascent - accel.Ascent
			text := rect{x, y, right, y + accel.Height}
			procSetTextColor.Call(dc, uintptr(secondary))
			s := u16(l.accel)
			procDrawTextW.Call(dc, uintptr(unsafe.Pointer(s)), ^uintptr(0), uintptr(unsafe.Pointer(&text)), dtSingleLine|dtRight|dtNoPrefix|dtNoClip)
		}
	})
	// Windows draws the arrow of a submenu after the item, in its colors:
	// the chevron is drawn already.
	procExcludeClipRect.Call(d.DC, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right), uintptr(r.Bottom))
}

// itemFill is how an item's background and highlight are painted, colors
// as COLORREFs: the highlight (plate) at alpha over the back, or, with
// glass, over the veil instead, which WinUI's Acrylic tint is, for the
// material behind the menu to show through both.
type itemFill struct {
	back, plate uint32
	alpha       float64
	glass       bool
	veil        tint
}

// paintItem draws an item in a bitmap its size, then copies it to the
// menu's DC at r: the background, a highlight with rounded corners
// (plate, in the item's coordinates, nil for none) antialiased over it,
// and what draw draws over them, in the bitmap's coordinates.
//
// With glass, GDI, which knows nothing of alpha, draws the background
// twice, over black and over white, and the pixels copied are
// premultiplied ones that show as both did: what shows over the material
// is as opaque as the two drawings differ little. Both begin with the veil,
// WinUI's Acrylic tint over the material. What draw draws goes over the
// veil's own color instead, opaque, so that ClearType blends its strokes
// with the background the shell's menus draw theirs over, and covers the
// material as they do: the pixels that drawing leaves no alpha to, which
// are the ones GDI and the icons paint, take the item's.
func paintItem(dc uintptr, r rect, f itemFill, plate *rect, radius float64, draw func(img *bitmap, r rect)) {
	w, h := r.Right-r.Left, r.Bottom-r.Top
	if w <= 0 || h <= 0 {
		return
	}
	// back fills a bitmap with the item's background of a color, and its
	// highlight; opaque marks the pixels the item's own, alpha and all,
	// rather than the colors of a drawing over black and over white, whose
	// alpha unblend tells.
	back := func(img *bitmap, color uint32, opaque bool) {
		bg, fg := dibColor(color), dibColor(f.plate)
		if f.glass {
			bg = blend(dibColor(f.veil.color), bg, f.veil.alpha)
		}
		if opaque {
			bg |= 0xFF000000
		}
		for i := range img.px {
			img.px[i] = bg
		}
		if plate == nil {
			return
		}
		for y := max(plate.Top, 0); y < min(plate.Bottom, h); y++ {
			for x := max(plate.Left, 0); x < min(plate.Right, w); x++ {
				img.px[y*w+x] = blend(fg, bg, f.alpha*roundedCoverage(float64(x)+0.5, float64(y)+0.5, plate, radius)) | bg&0xFF000000
			}
		}
	}
	if !f.glass {
		img := newBitmap(dc, w, h)
		if img == nil {
			return
		}
		defer img.free()
		back(img, f.back, false)
		procGdiFlush.Call()
		draw(img, rect{0, 0, w, h})
		procGdiFlush.Call()
		procBitBlt.Call(dc, uintptr(r.Left), uintptr(r.Top), uintptr(w), uintptr(h), img.dc, 0, 0, srcCopy)
		return
	}
	black := newBitmap(dc, w, h)
	if black == nil {
		return
	}
	defer black.free()
	back(black, 0x000000, false)
	white := newBitmap(dc, w, h)
	if white == nil {
		return
	}
	back(white, 0xFFFFFF, false)
	procGdiFlush.Call()
	for i, k := range black.px {
		black.px[i] = unblend(k, white.px[i])
	}
	white.free()
	card := newBitmap(dc, w, h)
	if card == nil {
		return
	}
	defer card.free()
	back(card, f.veil.color, true)
	procGdiFlush.Call()
	draw(card, rect{0, 0, w, h})
	procGdiFlush.Call()
	for i, px := range card.px {
		if px>>24 == 0 {
			black.px[i] = px | 0xFF000000
		}
	}
	procBitBlt.Call(dc, uintptr(r.Left), uintptr(r.Top), uintptr(w), uintptr(h), black.dc, 0, 0, srcCopy)
}

// unblend returns the premultiplied pixel, 0xAARRGGBB, that shows as
// onBlack over black and as onWhite over white, DIB pixels: its alpha is
// how much less than white's the black background shows through, on
// average over the channels, and its colors what shows over black, at
// most its alpha.
func unblend(onBlack, onWhite uint32) uint32 {
	sum := 0
	for shift := 0; shift < 24; shift += 8 {
		sum += 255 - int(onWhite>>shift&0xFF) + int(onBlack>>shift&0xFF)
	}
	a := min(max((sum+1)/3, 0), 255)
	px := uint32(a) << 24
	for shift := 0; shift < 24; shift += 8 {
		px |= uint32(min(int(onBlack>>shift&0xFF), a)) << shift
	}
	return px
}

// washRect blends a color at its opacity over the pixels of r in a
// bitmap w pixels wide.
func washRect(img *bitmap, w int32, r rect, t tint) {
	h := int32(len(img.px)) / w
	c := dibColor(t.color)
	for y := max(r.Top, 0); y < min(r.Bottom, h); y++ {
		for x := max(r.Left, 0); x < min(r.Right, w); x++ {
			img.px[y*w+x] = blend(c, img.px[y*w+x], t.alpha)
		}
	}
}

// textMetric is TEXTMETRICW.
type textMetric struct {
	Height, Ascent, Descent, InternalLeading, ExternalLeading int32
	AveCharWidth, MaxCharWidth, Weight, Overhang              int32
	DigitizedAspectX, DigitizedAspectY                        int32
	FirstChar, LastChar, DefaultChar, BreakChar               uint16
	Italic, Underlined, StruckOut, PitchAndFamily, CharSet    uint8
}

// textMetricsOf returns the metrics of the font selected into dc.
func textMetricsOf(dc uintptr) textMetric {
	var tm textMetric
	procGetTextMetricsW.Call(dc, uintptr(unsafe.Pointer(&tm)))
	return tm
}

// roundedCoverage returns how much of the pixel centered at (x, y) a
// rectangle with corners rounded by radius covers, from 0 to 1.
func roundedCoverage(x, y float64, r *rect, radius float64) float64 {
	cx := min(max(x, float64(r.Left)+radius), float64(r.Right)-radius)
	cy := min(max(y, float64(r.Top)+radius), float64(r.Bottom)-radius)
	d := math.Hypot(x-cx, y-cy)
	return min(max(radius-d+0.5, 0), 1)
}

// dibColor returns a COLORREF (0x00BBGGRR) as a DIB's pixel (0x00RRGGBB).
func dibColor(c uint32) uint32 { return c&0xFF<<16 | c&0xFF00 | c>>16&0xFF }

// blend mixes two DIB pixels, a over b by t from 0 to 1.
func blend(a, b uint32, t float64) uint32 {
	mix := func(shift uint) uint32 {
		x, y := float64(a>>shift&0xFF), float64(b>>shift&0xFF)
		return uint32(math.Round(y+(x-y)*t)) << shift
	}
	return mix(16) | mix(8) | mix(0)
}

// A menu with items its app draws loses Windows 11's frame: its window is
// square, under the drop shadow of its class (CS_DROPSHADOW) along two
// sides. While the menus show, a hook of the main thread rounds the windows
// of menus as they are created, with the corners of WinUI's flyouts,
// outlined in their colors, and takes the class's shadow off, for DWM's
// around the rounded window.
//
// It also subclasses them, to put the material of WinUI's menus, Acrylic
// (DWMSBT_TRANSIENTWINDOW), behind those of the menus MyGo draws, once
// they say which menu they show (dressMenu), on Windows 11 22H2 and later
// with transparency effects on, no contrast theme, energy saver off,
// outside remote sessions and with a GPU (menuGlass). The frame extended over the whole
// window, DWM shows the material where the window's pixels are
// transparent: the menu's background, its frame, and what the items leave
// so (paintItem). Neither the items nor the frame are transparent: they
// wear WinUI's Acrylic tint over the material, at the opacity of its
// luminosity layer (menuPalette.veil), which keeps a menu as light as the
// shell's flyouts over the desktop, and the menu's border a pixel of
// WinUI's (paintMenuBorder). DWM draws a system
// backdrop only in a window it frames active, and a solid grey in one it
// frames inactive, as a menu's window, which never activates, is: the
// subclass frames it active (activateMenuFrame), as Firefox does its
// popups with Acrylic. Elsewhere menus have WinUI's solid background, the
// fallback of its Acrylic, the window's border painted in it too, for
// DWM's outline to be the only one.

const (
	whCallWndProc      = 4
	wmCreate           = 0x0001
	wmNCDestroy        = 0x0082
	wmPrint            = 0x0317
	wmPrintClient      = 0x0318
	wmWindowPosChanged = 0x0047
	prfNonClient       = 0x0002
	gclStyle           = ^uintptr(25) // -26
	gcwAtom            = ^uintptr(31) // -32
	gwlpWndProc        = ^uintptr(3)  // -4
	menuClassAtom      = 0x8000       // #32768
	csDropShadow       = 0x00020000
	wsExLayoutRTL      = 0x00400000

	dwmwaBorderColor = 34
	dwmColorDefault  = 0xFFFFFFFF
	backdropAuto     = 0 // DWMSBT_AUTO, menus' own
	smRemoteSession  = 0x1000
)

var (
	procSetWindowsHookExW   = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx      = user32.NewProc("CallNextHookEx")
	procGetClassLongPtrW    = user32.NewProc("GetClassLongPtrW")
	procSetClassLongPtrW    = user32.NewProc("SetClassLongPtrW")
	procCallWindowProcW     = user32.NewProc("CallWindowProcW")
	procSaveDC              = gdi32.NewProc("SaveDC")
	procRestoreDC           = gdi32.NewProc("RestoreDC")

	// menuHookCallback and menuProcCallback are made once: callbacks are
	// never freed.
	menuHookCallback = sync.OnceValue(func() uintptr { return syscall.NewCallback(menuHook) })
	menuProcCallback = sync.OnceValue(func() uintptr { return syscall.NewCallback(menuProc) })
)

// cwpStruct is CWPSTRUCT.
type cwpStruct struct {
	LParam, WParam uintptr
	Message        uint32
	Hwnd           uintptr
}

// menuWindow is the window of a menu the subclass sees: the window
// procedure it replaced, the menu it showed last, whether the material is
// behind it, and the brush its border is painted with, 0 for Windows' own
// border, or, with the material, the veil it is painted with as its items
// are. popup tells that it shows a popup menu MyGo showed, whose loop
// created it, rather than one of a menu bar's dropdowns or Windows'
// window menu.
type menuWindow struct {
	proc, menu uintptr
	glass      bool
	border     uintptr
	veil       tint
	popup      bool
}

// glassConditions are what decide whether menus show the material.
type glassConditions struct {
	fluent       bool // MyGo draws the menus in Windows 11's style
	backdrops    bool // DWM has system backdrops (Windows 11 22H2)
	contrast     bool // a contrast theme is on
	transparency bool // transparency effects are on
	energySaver  bool // energy saver (battery saver) is on
	remote       bool // the session is a remote one
	software     bool // DWM composes without a GPU, as in a virtual machine
}

// glass reports whether menus show the material under the conditions:
// only where DWM surely draws it. Energy saver turns transparency effects
// off, which their setting doesn't say, a remote session may have none,
// and DWM draws none without a GPU (dwmSoftware); there, as wherever it
// isn't sure, a menu has WinUI's solid background rather than DWM's
// fallback for the material, a flat grey.
func (c glassConditions) glass() bool {
	return c.fluent && c.backdrops && !c.contrast && c.transparency && !c.energySaver && !c.remote && !c.software
}

// menuGlass reports whether the menus MyGo draws show the material behind
// them, as WinUI's do: from Windows 11 22H2, when transparency effects
// are on (Personalization > Colors), and neither a contrast theme nor
// energy saver is, outside remote sessions and with a GPU
// (glassConditions). Windows 11's first release and Windows 10 have no
// material for them.
func menuGlass() bool {
	c := glassConditions{fluent: fluentMenus(), backdrops: systemBackdrops()}
	if !c.fluent || !c.backdrops {
		return false
	}
	c.contrast = highContrastOn()
	v, ok := regDWORD(hkeyCurrentUser, personalizeKey, "EnableTransparency")
	c.transparency = !ok || v != 0
	c.energySaver = energySaverOn()
	r, _, _ := procGetSystemMetrics.Call(smRemoteSession)
	c.remote = r != 0
	c.software = dwmSoftware()
	return c.glass()
}

// dwmSoftware reports whether DWM composes without a GPU, and so draws no
// material, only the flat grey it falls back to: when the display's
// adapter is Microsoft's, the Basic Render Driver of a virtual machine
// without a 3D driver or of a computer without its display driver, or a
// software one. DWM's ForceEffectMode policy of 2 makes it draw the
// material anyway. It is read once: the adapter changes with a driver.
var dwmSoftware = sync.OnceValue(func() bool {
	if v, ok := regDWORD(hkeyLocalMachine, `SOFTWARE\Microsoft\Windows\Dwm`, "ForceEffectMode"); ok && v == 2 {
		return false
	}
	vendor, flags, ok := primaryAdapter()
	return ok && softwareAdapter(vendor, flags)
})

// softwareAdapter reports whether the DXGI adapter of vendor with flags
// renders on the CPU: one of Microsoft's (the Basic Render Driver), or
// one DXGI says is software.
func softwareAdapter(vendor, flags uint32) bool {
	const microsoft, flagSoftware = 0x1414, 2 // DXGI_ADAPTER_FLAG_SOFTWARE
	return vendor == microsoft || flags&flagSoftware != 0
}

// dxgiAdapterDesc1 is DXGI_ADAPTER_DESC1.
type dxgiAdapterDesc1 struct {
	description                                [128]uint16
	vendor, device, subSys, revision           uint32
	dedicatedVideo, dedicatedSystem, sharedMem uintptr
	luid                                       [2]uint32
	flags                                      uint32
}

// primaryAdapter returns the vendor ID and flags of the first DXGI
// adapter, the one with the primary display, which DWM composes it with,
// and ok false when DXGI cannot say.
func primaryAdapter() (vendor, flags uint32, ok bool) {
	if procCreateDXGIFactory1.Find() != nil {
		return 0, 0, false
	}
	var factory uintptr
	if hr, _, _ := procCreateDXGIFactory1.Call(uintptr(unsafe.Pointer(&iidIDXGIFactory1)), uintptr(unsafe.Pointer(&factory))); int32(uint32(hr)) < 0 || factory == 0 {
		return 0, 0, false
	}
	defer release(factory)
	const enumAdapters1, getDesc1 = 12, 10 // IDXGIFactory1, IDXGIAdapter1
	var adapter uintptr
	if hr := comCall(factory, enumAdapters1, 0, uintptr(unsafe.Pointer(&adapter))); int32(uint32(hr)) < 0 || adapter == 0 {
		return 0, 0, false
	}
	defer release(adapter)
	var desc dxgiAdapterDesc1
	if hr := comCall(adapter, getDesc1, uintptr(unsafe.Pointer(&desc))); int32(uint32(hr)) < 0 {
		return 0, 0, false
	}
	return desc.vendor, desc.flags, true
}

// energySaverOn reports whether energy saver (battery saver) is on.
func energySaverOn() bool {
	var status struct {
		acLineStatus, batteryFlag, batteryLifePercent, systemStatusFlag uint8
		batteryLifeTime, batteryFullLifeTime                            uint32
	}
	if r, _, _ := procGetSystemPowerStatus.Call(uintptr(unsafe.Pointer(&status))); r == 0 {
		return false
	}
	return status.systemStatusFlag&1 != 0
}

// hookMenus hooks the main thread's messages while its menus show (on), or
// unhooks them. Hooking, it reads whether its menus show the material,
// which they all do while they show, or none.
func (b *Backend) hookMenus(on bool) {
	switch {
	case !fluentMenus():
	case on && b.menuHook == 0:
		b.menuGlass = menuGlass()
		b.menuHook, _, _ = procSetWindowsHookExW.Call(whCallWndProc, menuHookCallback(), 0, uintptr(currentThreadID()))
	case !on && b.menuHook != 0:
		procUnhookWindowsHookEx.Call(b.menuHook)
		b.menuHook = 0
	}
}

func menuHook(code, wp, lp uintptr) uintptr {
	if int32(code) >= 0 {
		if m := (*cwpStruct)(native(lp)); m.Message == wmCreate {
			if atom, _, _ := procGetClassLongPtrW.Call(m.Hwnd, gcwAtom); atom == menuClassAtom {
				roundMenu(m.Hwnd, theBackend.menuPalette().border)
				theBackend.subclassMenu(m.Hwnd)
			}
		}
	}
	r, _, _ := procCallNextHookEx.Call(0, code, wp, lp)
	return r
}

// roundMenu gives a menu's window, being created, Windows 11's frame,
// with the corners of WinUI's menus (OverlayCornerRadius, 8 pixels).
func roundMenu(hwnd uintptr, border uint32) {
	pref := int32(dwmwcpRound)
	procDwmSetWindowAttribute.Call(hwnd, dwmwaWindowCornerPreference, uintptr(unsafe.Pointer(&pref)), 4)
	procDwmSetWindowAttribute.Call(hwnd, dwmwaBorderColor, uintptr(unsafe.Pointer(&border)), 4)
	if style, _, _ := procGetClassLongPtrW.Call(hwnd, gclStyle); style&csDropShadow != 0 {
		procSetClassLongPtrW.Call(hwnd, gclStyle, style&^csDropShadow)
	}
}

// subclassMenu subclasses a menu's window, being created, until it is
// destroyed.
func (b *Backend) subclassMenu(hwnd uintptr) {
	if b.menuWindows[hwnd] != nil {
		return
	}
	old, _, _ := procSetWindowLongPtrW.Call(hwnd, gwlpWndProc, menuProcCallback())
	if old == 0 {
		return
	}
	if b.menuWindows == nil {
		b.menuWindows = map[uintptr]*menuWindow{}
	}
	b.menuWindows[hwnd] = &menuWindow{proc: old, popup: b.popups > 0}
}

// menuProc is the window procedure of the menus' windows subclassMenu
// subclassed. Before a window shows or paints, it dresses it for the menu
// it shows; where MyGo draws the menu, it paints its border in the
// menu's background, transparent where the material is behind it, rather
// than as Windows does, and keeps its frame active for DWM to draw the
// material. The popups MyGo shows stay open on Alt, as WinUI's flyouts do,
// while a menu bar's dropdowns and Windows' window menu keep Win32's
// behavior and leave.
func menuProc(hwnd, m, wp, lp uintptr) uintptr {
	b := theBackend
	mw := b.menuWindows[hwnd]
	if mw == nil {
		r, _, _ := procDefWindowProcW.Call(hwnd, m, wp, lp)
		return r
	}
	call := func() uintptr {
		r, _, _ := procCallWindowProcW.Call(mw.proc, hwnd, m, wp, lp)
		return r
	}
	switch uint32(m) {
	case wmWindowPosChanging, wmEraseBkgnd, wmPaint, wmPrintClient:
		b.dressMenu(hwnd, mw)
	case wmWindowPosChanged:
		// Shown, framed active again, as Firefox frames its popups once
		// they show.
		r := call()
		if mw.glass && (*windowPos)(native(lp)).Flags&swpShowWindow != 0 {
			activateMenuFrame(hwnd, true)
		}
		return r
	case wmNCActivate:
		if mw.glass {
			// Framed active, whatever the window is told, without
			// painting the frame: DWM draws the material of a window
			// framed inactive as a solid fill.
			r, _, _ := procDefWindowProcW.Call(hwnd, m, 1, ^uintptr(0))
			return r
		}
	case wmNCPaint:
		if b.dressMenu(hwnd, mw); mw.glass || mw.border != 0 {
			dc, _, _ := procGetWindowDC.Call(hwnd)
			paintMenuBorder(hwnd, dc, mw)
			procReleaseDC.Call(hwnd, dc)
			return 0
		}
	case wmPrint:
		b.dressMenu(hwnd, mw)
		r := call()
		if (mw.glass || mw.border != 0) && lp&prfNonClient != 0 {
			paintMenuBorder(hwnd, wp, mw)
		}
		return r
	case wmKeyDown, wmKeyUp, wmSysKeyDown, wmSysKeyUp:
		// Alt alone, on which Win32's menus leave, as they leave on
		// Escape: a popup MyGo shows stays open, as the menus of Windows
		// 11 itself, WinUI's flyouts, do, Alt only moving the keyboard to
		// a window's own controls there, and the keyboard still reaching
		// the menu afterwards to choose an item. Alt comes as a system
		// key alone, WM_SYSKEYDOWN, and as a plain one while Control is
		// held, which Windows takes for AltGr. Alt and a letter, and the
		// rest of the keyboard, go on to Windows, and a menu bar's
		// dropdown, and Windows' window menu, leave on Alt as Win32 has
		// them.
		if wp == vkMenu && mw.popup {
			return 0
		}
	case wmSysChar:
		// Alt alone's character, which is none.
		if wp == 0 && mw.popup {
			return 0
		}
	case wmNCDestroy:
		procSetWindowLongPtrW.Call(hwnd, gwlpWndProc, mw.proc)
		delete(b.menuWindows, hwnd)
	}
	return call()
}

// dressMenu puts the material behind a menu's window, or takes it away,
// for the menu it shows: one MyGo draws, while its menus show it
// (menuGlass), rather than Windows' window menu. It picks what the
// window's border is painted with: the veil, over the material, as its
// items are, or the menu's solid background, for DWM's outline to be the
// menu's only border, and nothing, for Windows' own, for a menu MyGo
// doesn't draw or in a contrast theme. It does so once the window says
// which menu it shows, before it draws it, and again only for another
// menu.
func (b *Backend) dressMenu(hwnd uintptr, mw *menuWindow) {
	menu, _, _ := procSendMessageW.Call(hwnd, mnGetHMenu, 0, 0)
	if menu == 0 || menu == mw.menu {
		return
	}
	mw.menu = menu
	draws := b.drawsMenu(menu)
	if glass := b.menuGlass && draws; glass != mw.glass {
		mw.glass = glass
		setMenuGlass(hwnd, glass, b.isDark())
	}
	switch {
	case mw.glass:
		mw.veil, mw.border = b.menuPalette().veil, 0
	case draws && !highContrastOn():
		mw.border = b.menuBrush(b.menuPalette().back)
	default:
		mw.border = 0
	}
	if draws {
		b.setMenuBackground(menu)
	}
}

// glassMenu reports whether a menu shows in a window with the material
// behind it.
func (b *Backend) glassMenu(menu uintptr) bool {
	for _, mw := range b.menuWindows {
		if mw.menu == menu && mw.glass {
			return true
		}
	}
	return false
}

// drawsMenu reports whether MyGo draws the items of a menu.
func (b *Backend) drawsMenu(menu uintptr) bool {
	for _, l := range b.menus.looks {
		if l.menu == menu {
			return true
		}
	}
	return false
}

// setMenuGlass puts Acrylic behind a menu's window, in the appearance, the
// frame extended over all of it and framed active, or takes it away (on
// false).
func setMenuGlass(hwnd uintptr, on, dark bool) {
	backdrop, m, d := int32(backdropAuto), margins{}, int32(0)
	if on {
		backdrop, m = backdropAcrylic, margins{-1, -1, -1, -1}
		if dark {
			d = 1
		}
	}
	procDwmSetWindowAttribute.Call(hwnd, dwmwaUseImmersiveDarkMode, uintptr(unsafe.Pointer(&d)), 4)
	procDwmExtendFrameIntoClientArea.Call(hwnd, uintptr(unsafe.Pointer(&m)))
	procDwmSetWindowAttribute.Call(hwnd, dwmwaSystemBackdropType, uintptr(unsafe.Pointer(&backdrop)), 4)
	activateMenuFrame(hwnd, on)
}

// activateMenuFrame frames a menu's window active (on), or inactive as it
// was, without activating it, taking the focus or painting its frame:
// DWM draws a system backdrop only in a window it frames active, and its
// solid fallback, a flat grey, in one it frames inactive, which a menu's
// window, never activated, otherwise is. The subclass keeps it so
// (menuProc).
func activateMenuFrame(hwnd uintptr, on bool) {
	active := uintptr(0)
	if on {
		active = 1
	}
	procSendMessageW.Call(hwnd, wmNCActivate, active, ^uintptr(0)) // -1: no painting
}

// paintMenuBorder paints the border of a menu's window, around its client
// area, in dc, a DC of the whole window: the veil, over the material, as
// the pixels of its items are, or the menu's background, for DWM's outline
// around the window to be the only border, a pixel of WinUI's.
func paintMenuBorder(hwnd, dc uintptr, mw *menuWindow) {
	if dc == 0 {
		return
	}
	var win, client rect
	procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&win)))
	procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&client)))
	var origin point
	procClientToScreen.Call(hwnd, uintptr(unsafe.Pointer(&origin)))
	x, y := origin.X-win.Left, origin.Y-win.Top
	if windowLong(hwnd, gwlExStyle)&wsExLayoutRTL != 0 {
		// A mirrored window's DC counts from its right edge.
		x = win.Right - origin.X
	}
	saved, _, _ := procSaveDC.Call(dc)
	procExcludeClipRect.Call(dc, uintptr(x), uintptr(y), uintptr(x+client.Right), uintptr(y+client.Bottom))
	all := rect{0, 0, win.Right - win.Left, win.Bottom - win.Top}
	switch {
	case mw.glass:
		fillTint(dc, &all, mw.veil)
	case mw.border != 0:
		procFillRect.Call(dc, uintptr(unsafe.Pointer(&all)), mw.border)
	}
	procRestoreDC.Call(dc, saved)
}

// fillTint paints a rectangle of dc with a color at its opacity, for a menu
// with the material behind it: the pixels it copies are premultiplied, as
// the items' are (paintItem), for the material to show through them, which
// GDI's brushes, knowing nothing of alpha, cannot.
func fillTint(dc uintptr, r *rect, t tint) {
	w, h := r.Right-r.Left, r.Bottom-r.Top
	if w <= 0 || h <= 0 {
		return
	}
	img := newBitmap(dc, w, h)
	if img == nil {
		return
	}
	defer img.free()
	a := min(max(t.alpha, 0), 1)
	px := uint32(math.Round(255*a)) << 24
	c := dibColor(t.color)
	for shift := uint(0); shift < 24; shift += 8 {
		px |= uint32(math.Round(float64(c>>shift&0xFF)*a)) << shift
	}
	for i := range img.px {
		img.px[i] = px
	}
	procGdiFlush.Call()
	procBitBlt.Call(dc, uintptr(r.Left), uintptr(r.Top), uintptr(w), uintptr(h), img.dc, 0, 0, srcCopy)
}
