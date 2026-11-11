//go:build windows && (amd64 || arm64)

package windows

import (
	"bytes"
	"errors"
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	"image/png"
	"unsafe"

	"github.com/egoist/mygo/internal/accelerator"
	"github.com/egoist/mygo/internal/platform"
)

// notifyIconData is NOTIFYICONDATAW.
type notifyIconData struct {
	Size            uint32
	Wnd             uintptr
	ID              uint32
	Flags           uint32
	CallbackMessage uint32
	Icon            uintptr
	Tip             [128]uint16
	State           uint32
	StateMask       uint32
	Info            [256]uint16
	Version         uint32 // union with uTimeout
	InfoTitle       [64]uint16
	InfoFlags       uint32
	GUIDItem        GUID
	BalloonIcon     uintptr
}

const (
	nimAdd    = 0
	nimModify = 1
	nimDelete = 2

	nifMessage = 0x01
	nifIcon    = 0x02
	nifTip     = 0x04
	nifInfo    = 0x10
	nifShowTip = 0x80

	niifNone      = 0x00
	niifUser      = 0x04
	niifNoSound   = 0x10
	niifLargeIcon = 0x20

	ninBalloonTimeout   = wmUser + 4
	ninBalloonUserClick = wmUser + 5
)

func copyUTF16(dst []uint16, s string) {
	src := utf16Units(s)
	n := copy(dst[:len(dst)-1], src)
	dst[n] = 0
}

type tray struct {
	b     *Backend
	h     platform.TrayHandler
	id    uint32
	icon  uintptr
	tip   string
	menu  *platform.Menu
	added bool
	// notification is the id of the notification the icon shows, if any.
	notification string
	temporary    bool // added only to show a notification
	// balloon is the icon of the notification's image, if it has one.
	balloon uintptr
	// dark is the taskbar's appearance, as last told.
	dark bool
	// image is the PNG the icon shows, and template whether it is black
	// and transparent, which the icon shows white on a dark taskbar as
	// macOS tints a template image.
	image    []byte
	template bool
}

func (b *Backend) NewTray(h platform.TrayHandler) (platform.Tray, error) {
	whitenTemplate = whiten
	b.nextTray++
	t := &tray{b: b, h: h, id: b.nextTray}
	t.dark = t.Dark()
	b.trays[t.id] = t
	// Added once the call that made it gave it its number, image and
	// tooltip, or as soon as asked where it is: the notification area keeps
	// the choices of the user for an icon by its number and names it by
	// its first tooltip, and keeps none for an icon numbered by its ID
	// that came blank (see SetID).
	postMessage(b.appHwnd, wmAppTrayAdd, 0, 0)
	return t, nil
}

// addNewTrays adds the icons made since it last ran.
func (b *Backend) addNewTrays() {
	for _, t := range b.trays {
		if !t.added && !t.temporary {
			t.add()
		}
	}
}

func (t *tray) data(flags uint32) notifyIconData {
	d := notifyIconData{Wnd: t.b.appHwnd, ID: t.id, Flags: flags | nifMessage, CallbackMessage: wmAppTray, Icon: t.icon}
	d.Size = uint32(unsafe.Sizeof(d))
	if t.icon != 0 {
		d.Flags |= nifIcon
	}
	if t.tip != "" {
		d.Flags |= nifTip | nifShowTip
		copyUTF16(d.Tip[:], t.tip)
	}
	return d
}

func (t *tray) add() {
	d := t.data(0)
	procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&d)))
	t.added = true
}

func (t *tray) update() {
	if !t.added {
		return // NewTray adds it as it is
	}
	d := t.data(0)
	procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&d)))
}

func (t *tray) SetImage(data []byte, template bool) error {
	img := data
	if template && t.dark && whitenTemplate != nil {
		var err error
		if img, err = whitenTemplate(data); err != nil {
			return err
		}
	}
	icon, err := iconFromPNG(img, 0)
	if err != nil {
		return err
	}
	if t.icon != 0 {
		procDestroyIcon.Call(t.icon)
	}
	t.icon, t.image, t.template = icon, data, template
	t.update()
	return nil
}

// whitenTemplate is whiten once NewTray links it: the taskbar's
// appearance changes reach SetImage from the window procedure of every
// app, and whiten's PNG encoder would cost those without a tray 140 KB.
var whitenTemplate func([]byte) ([]byte, error)

// whiten draws a template image white, keeping its alpha.
func whiten(data []byte) ([]byte, error) {
	src, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	r := src.Bounds()
	dst := image.NewNRGBA(r)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			_, _, _, a := src.At(x, y).RGBA()
			dst.SetNRGBA(x, y, color.NRGBA{255, 255, 255, uint8(a >> 8)})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, dst); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func (t *tray) SetTitle(string) {}

// Dark reports whether the taskbar is dark: Windows draws it in the
// system's mode, which may differ from the apps'.
func (t *tray) Dark() bool {
	light, ok := regDWORD(hkeyCurrentUser, personalizeKey, "SystemUsesLightTheme")
	return ok && light == 0
}

// SetID numbers the icon by its ID. The notification area keeps whether
// the user shows an icon on the taskbar by the executable's path and the
// icon's number, which NewTray otherwise gives as icons come: an icon
// made again, or after others in another order, would lose the user's
// choice, or take another icon's. It keeps none for an icon of such a
// number added blank, so NewTray adds icons once they show something. An
// icon already added is added again under its new number; a number
// another icon has keeps the old one.
func (t *tray) SetID(id string) {
	n := trayNumber(id)
	if id == "" || n == t.id || t.b.trays[n] != nil {
		return
	}
	if t.added {
		d := t.data(0)
		procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&d)))
	}
	delete(t.b.trays, t.id)
	t.id = n
	t.b.trays[n] = t
	if t.added {
		t.add()
	}
}

// trayNumber is the number of an icon of an ID: its FNV-1a hash, with the
// high bit set, which the numbers NewTray counts never reach.
func trayNumber(id string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(id))
	return h.Sum32() | 1<<31
}

func (t *tray) SetToolTip(tip string) {
	t.tip = tip
	t.update()
}

func (t *tray) SetMenu(m *platform.Menu) { t.menu = m }

// SetActivatable does nothing: the notification area reports its clicks
// itself.
func (t *tray) SetActivatable(bool) {}

func (t *tray) PopUpMenu(m *platform.Menu) {
	if m == nil {
		m = t.menu
	}
	if m == nil {
		return
	}
	id := t.b.menus.newOwner()
	defer t.b.menus.drop(id)
	pt, flags, exclude, ok := t.menuPlacement()
	if !ok {
		procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
		flags, exclude = 0, nil
	}
	menu := t.b.buildMenu(m, false, id, nil, monitorDPI(monitorAt(int(pt.X), int(pt.Y))))
	defer procDestroyMenu.Call(menu)
	if cmd := t.b.trackPopup(menu, t.b.appHwnd, pt, flags, exclude); cmd != 0 {
		t.b.menuCommand(cmd, nil)
	}
}

// trayMenuGap is how far from the taskbar, in DIPs, Windows 11 opens the
// menus of the notification area that it and input methods show (WinUI's
// MenuFlyout): centered on their icons, rather than at the pointer as
// Win32 menus open.
const trayMenuGap = 12

// appBarData is APPBARDATA.
type appBarData struct {
	Size            uint32
	Wnd             uintptr
	CallbackMessage uint32
	Edge            uint32
	Rect            rect
	LParam          uintptr
}

// tpmParams is TPMPARAMS.
type tpmParams struct {
	Size    uint32
	Exclude rect
}

const (
	abmGetTaskbarPos = 5
	// The edges of a taskbar (ABE_*) besides the bottom, Windows 11's.
	abeLeft  = 0
	abeTop   = 1
	abeRight = 2
)

var procSHAppBarMessage = shell32.NewProc("SHAppBarMessage")

// menuPlacement returns where the icon's menu opens on Windows 11, as
// trayMenuAt places it from the icon on the taskbar, or false for the
// pointer: on Windows 10, whose own menus open there, and for an icon
// that is not on the taskbar, in its overflow or nowhere.
func (t *tray) menuPlacement() (pt point, flags uintptr, exclude *rect, ok bool) {
	if !fluentMenus() {
		return pt, 0, nil, false
	}
	icon, shows := t.rect()
	if !shows {
		return pt, 0, nil, false
	}
	bar := appBarData{}
	bar.Size = uint32(unsafe.Sizeof(bar))
	if r, _, _ := procSHAppBarMessage.Call(abmGetTaskbarPos, uintptr(unsafe.Pointer(&bar))); r == 0 || !overlaps(icon, bar.Rect) {
		return pt, 0, nil, false
	}
	dpi := monitorDPI(monitorAt(int(icon.Left+icon.Right)/2, int(icon.Top+icon.Bottom)/2))
	pt, flags, ex := trayMenuAt(icon, bar.Rect, bar.Edge, toPx(trayMenuGap, dpi))
	return pt, flags, &ex, true
}

// trayMenuAt returns where a menu opens for an icon on a taskbar along an
// edge of its monitor (ABE_*), as Windows 11 opens its own: centered on
// the icon along the taskbar, gap pixels away from it. It returns the
// point, the flags of TrackPopupMenuEx that align the menu to it, and the
// rectangle the menu keeps out of, the taskbar and the gap, which keeps
// it on the side of the point it opens to.
func trayMenuAt(icon, bar rect, edge uint32, gap int32) (point, uintptr, rect) {
	x, y := (icon.Left+icon.Right)/2, (icon.Top+icon.Bottom)/2
	switch edge {
	case abeTop:
		return point{x, bar.Bottom + gap}, tpmCenterAlign | tpmVertical, rect{bar.Left, bar.Top, bar.Right, bar.Bottom + gap}
	case abeLeft:
		return point{bar.Right + gap, y}, tpmVCenterAlign, rect{bar.Left, bar.Top, bar.Right + gap, bar.Bottom}
	case abeRight:
		return point{bar.Left - gap, y}, tpmRightAlign | tpmVCenterAlign, rect{bar.Left - gap, bar.Top, bar.Right, bar.Bottom}
	}
	return point{x, bar.Top - gap}, tpmCenterAlign | tpmBottomAlign | tpmVertical, rect{bar.Left, bar.Top - gap, bar.Right, bar.Bottom}
}

// overlaps reports whether two rectangles share pixels.
func overlaps(a, b rect) bool {
	return a.Left < b.Right && b.Left < a.Right && a.Top < b.Bottom && b.Top < a.Bottom
}

func (t *tray) Bounds() platform.Rect {
	r, ok := t.rect()
	if !ok {
		return platform.Rect{}
	}
	return screenSpace().rectToDIP(pixelRect(r))
}

// rect returns where the icon is on the screen, in physical pixels.
func (t *tray) rect() (rect, bool) {
	if !t.added && !t.temporary {
		t.add() // it is somewhere once added
	}
	type identifier struct {
		Size uint32
		Wnd  uintptr
		ID   uint32
		GUID GUID
	}
	ident := identifier{Wnd: t.b.appHwnd, ID: t.id}
	ident.Size = uint32(unsafe.Sizeof(ident))
	var r rect
	if !has(procShellNotifyIconGetRect) {
		return r, false
	}
	if hr, _, _ := procShellNotifyIconGetRect.Call(uintptr(unsafe.Pointer(&ident)), uintptr(unsafe.Pointer(&r))); failed(hr) {
		return r, false
	}
	return r, r.Right > r.Left && r.Bottom > r.Top
}

func (t *tray) Destroy() {
	if t.added {
		d := t.data(0)
		procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&d)))
		t.added = false
	}
	if t.icon != 0 && !t.temporary {
		procDestroyIcon.Call(t.icon)
		t.icon = 0
	}
	t.destroyBalloonIcon()
	delete(t.b.trays, t.id)
}

func (t *tray) destroyBalloonIcon() {
	if t.balloon != 0 {
		procDestroyIcon.Call(t.balloon)
		t.balloon = 0
	}
}

// trayMessage handles the callback message of the notification area.
func (b *Backend) trayMessage(id uint32, event uint32) {
	t := b.trays[id]
	if t == nil {
		return
	}
	switch event {
	case wmLButtonUp:
		if t.h != nil {
			t.h.Clicked()
		}
	case wmRButtonUp:
		if t.h != nil {
			t.h.RightClicked()
		}
		if t.menu != nil {
			t.PopUpMenu(t.menu)
		}
	case ninBalloonUserClick:
		if n := t.notification; n != "" {
			b.finishNotification(t)
			b.h.NotificationClicked(n)
		}
	case ninBalloonTimeout:
		b.finishNotification(t)
	}
}

// iconFromPNG makes an icon of a PNG image (size 0: the image's size).
func iconFromPNG(data []byte, size int) (uintptr, error) {
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, err
	}
	if size == 0 {
		size = cfg.Width
	}
	icon, _, _ := procCreateIconFromResourceEx.Call(uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)), 1, 0x00030000, uintptr(size), uintptr(size), 0)
	if icon == 0 {
		return 0, errors.New("mygo: cannot create an icon from the image")
	}
	return icon, nil
}

// Notifications are notification area balloons, which Windows 10 and later
// show as toasts. They need an icon: the first tray icon, else a temporary
// one with the application's icon.

type notification struct{ tray *tray }

func (b *Backend) NotificationsSupported() bool { return true }

func (b *Backend) ShowNotification(n *platform.Notification, done func(error)) {
	done(b.showNotification(n))
}

func (b *Backend) showNotification(n *platform.Notification) error {
	if old := b.notifications[n.ID]; old != nil {
		// Shown again under its ID: the notification it replaces gives up
		// its tray, and its balloon's icon, rather than leaving them.
		b.finishNotification(old.tray)
	}
	var t *tray
	for _, x := range b.trays {
		if !x.temporary && x.notification == "" && x.added {
			t = x
			break
		}
	}
	if t == nil {
		b.nextTray++
		t = &tray{b: b, id: b.nextTray, temporary: true}
		t.icon, _, _ = procLoadIconW.Call(instance(), 1)
		if t.icon == 0 {
			t.icon, _, _ = procLoadIconW.Call(0, 32512) // IDI_APPLICATION
		}
		b.trays[t.id] = t
		t.add()
	}
	t.notification = n.ID
	b.notifications[n.ID] = &notification{tray: t}
	d := t.data(nifInfo)
	title := n.Title
	body := n.Body
	if n.Subtitle != "" {
		body = n.Subtitle + "\n" + body
	}
	copyUTF16(d.InfoTitle[:], title)
	copyUTF16(d.Info[:], body)
	d.InfoFlags = niifUser
	if n.Silent {
		d.InfoFlags |= niifNoSound
	}
	if len(n.Image) > 0 && notificationIcon != nil {
		if t.balloon = notificationIcon(n.Image, dpiOf(b.appHwnd)); t.balloon != 0 {
			d.BalloonIcon = t.balloon
			d.InfoFlags |= niifLargeIcon
		}
	}
	if r, _, _ := procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&d))); r == 0 {
		b.finishNotification(t)
		return fmt.Errorf("mygo: cannot show the notification")
	}
	return nil
}

func (b *Backend) RemoveNotification(id string) {
	if n := b.notifications[id]; n != nil {
		b.finishNotification(n.tray)
	}
}

func (b *Backend) RemoveAllNotifications() {
	for _, n := range b.notifications {
		b.finishNotification(n.tray)
	}
}

func (b *Backend) finishNotification(t *tray) {
	delete(b.notifications, t.notification)
	t.notification = ""
	if t.temporary {
		t.Destroy()
		return
	}
	d := t.data(nifInfo) // an empty balloon hides the current one
	procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&d)))
	t.destroyBalloonIcon()
}

// Global shortcuts.

func (b *Backend) RegisterHotkey(id int, acc string) error {
	a, err := accelerator.Parse(acc, "windows")
	if err != nil {
		return err
	}
	vk, ok := virtualKey(a.Key)
	if !ok {
		return fmt.Errorf("mygo: unsupported key in %q", acc)
	}
	const (
		modAlt, modControl, modShift, modWin, modNoRepeat = 0x1, 0x2, 0x4, 0x8, 0x4000
	)
	mods := uintptr(modNoRepeat)
	if a.Has(accelerator.Alt) {
		mods |= modAlt
	}
	if a.Has(accelerator.Ctrl) {
		mods |= modControl
	}
	if a.Has(accelerator.Shift) {
		mods |= modShift
	}
	if a.Has(accelerator.Super) {
		mods |= modWin
	}
	if r, _, err := procRegisterHotKey.Call(b.appHwnd, uintptr(id), mods, uintptr(vk)); r == 0 {
		return fmt.Errorf("mygo: cannot register %q: %v", acc, err)
	}
	return nil
}

func (b *Backend) UnregisterHotkey(id int) { procUnregisterHotKey.Call(b.appHwnd, uintptr(id)) }

// traysAppearance tells the icons whose taskbar turned dark or light. A
// temporary icon, shown only for a notification, has no handler to tell.
func (b *Backend) traysAppearance() {
	for _, t := range b.trays {
		if t.temporary || t.h == nil {
			continue
		}
		if dark := t.Dark(); dark != t.dark {
			t.dark = dark
			if t.template {
				t.SetImage(t.image, true)
			}
			t.h.AppearanceChanged()
		}
	}
}
