//go:build windows && (amd64 || arm64)

package windows

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/transfer"
)

// Clipboard.

type clipboard struct{ b *Backend }

var (
	cfHTML = registerClipboardFormat("HTML Format")
	cfPNG  = registerClipboardFormat("PNG")
)

func registerClipboardFormat(name string) uint32 {
	r, _, _ := procRegisterClipboardFormatW.Call(uintptr(unsafe.Pointer(u16(name))))
	return uint32(r)
}

// The clipboard is a lock of the whole session: OpenClipboard fails, and
// OleSetClipboard and OleFlushClipboard with CLIPBRD_E_CANT_OPEN, while
// another program has it open, as Windows' clipboard history and clipboard
// managers do right after every change to read it. That program may be
// waiting for this one to render data it offered (WM_RENDERFORMAT), which
// only a thread handling its sent messages does: while it retries, the main
// thread handles them rather than sleeping, or neither would get on.
const clipboardPatience = time.Second

// retryClipboard calls try until it gets the clipboard or the clipboard
// stayed busy for clipboardPatience. Main thread only.
func retryClipboard(try func() (busy bool)) {
	deadline := time.Now().Add(clipboardPatience)
	for try() && time.Now().Before(deadline) {
		// Wait for a message sent by another thread, at most 10 ms, and
		// handle the messages sent.
		procMsgWaitForMultipleObjectsEx.Call(0, 0, 10, qsSendMessage, 0)
		var m msg
		procPeekMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0, pmNoRemove|pmQSSendMessage)
	}
}

// open opens the clipboard, waiting while another program holds it.
func (c clipboard) open() bool {
	opened := false
	retryClipboard(func() bool {
		r, _, _ := procOpenClipboard.Call(c.b.appHwnd)
		opened = r != 0
		return !opened
	})
	return opened
}

// oleClipboard calls OleSetClipboard or OleFlushClipboard, waiting while
// another program holds the clipboard.
func oleClipboard(call func() uintptr) (hr uintptr) {
	retryClipboard(func() bool {
		hr = call()
		return hr == clipbrdECantOpen
	})
	return hr
}

func closeClipboard() { procCloseClipboard.Call() }

// data copies the clipboard data of a format.
func clipboardData(format uint32) []byte {
	h, _, _ := procGetClipboardData.Call(uintptr(format))
	if h == 0 {
		return nil
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		return nil
	}
	defer procGlobalUnlock.Call(h)
	size, _, _ := procGlobalSize.Call(h)
	return bytes.Clone(unsafe.Slice((*byte)(native(p)), size))
}

// setData puts data on the (open, emptied) clipboard.
func setClipboardData(format uint32, data []byte) bool {
	h, _, _ := procGlobalAlloc.Call(gmemMoveable, uintptr(len(data)))
	if h == 0 {
		return false
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		procGlobalFree.Call(h)
		return false
	}
	copy(unsafe.Slice((*byte)(native(p)), len(data)), data)
	procGlobalUnlock.Call(h)
	if r, _, _ := procSetClipboardData.Call(uintptr(format), h); r == 0 {
		procGlobalFree.Call(h)
		return false
	}
	return true
}

// utf16Bytes encodes s as NUL-terminated little-endian UTF-16.
func utf16Bytes(s string) []byte {
	u := utf16z(s)
	b := make([]byte, 2*len(u))
	for i, c := range u {
		binary.LittleEndian.PutUint16(b[2*i:], c)
	}
	return b
}

func (c clipboard) ReadText() string {
	if !c.open() {
		return ""
	}
	defer closeClipboard()
	data := clipboardData(cfUnicodeText)
	u := make([]uint16, len(data)/2)
	for i := range u {
		u[i] = binary.LittleEndian.Uint16(data[2*i:])
	}
	return string(utf16Decode(u))
}

func (c clipboard) WriteText(text string) { _ = c.WriteData(transfer.TextData(text), nil) }

// CF_HTML wraps a fragment in a header of byte offsets.
func (c clipboard) ReadHTML() string {
	if !c.open() {
		return ""
	}
	defer closeClipboard()
	data := string(bytes.TrimRight(clipboardData(cfHTML), "\x00"))
	start, end := htmlOffset(data, "StartFragment:"), htmlOffset(data, "EndFragment:")
	if start < 0 || end < start || end > len(data) {
		return ""
	}
	return data[start:end]
}

func htmlOffset(data, key string) int {
	i := strings.Index(data, key)
	if i < 0 {
		return -1
	}
	rest := data[i+len(key):]
	j := strings.IndexAny(rest, "\r\n")
	if j < 0 {
		return -1
	}
	n, err := strconv.Atoi(strings.TrimSpace(rest[:j]))
	if err != nil {
		return -1
	}
	return n
}

func (c clipboard) WriteHTML(markup string) {
	_ = c.WriteData(transfer.New(transfer.NewItem(transfer.Bytes(transfer.HTML, []byte(markup)), transfer.Bytes(transfer.Text, []byte(markup)))), nil)
}

// htmlData is shared by clipboard and drag representations.
func htmlData(markup string) []byte {
	const header = "Version:0.9\r\nStartHTML:%010d\r\nEndHTML:%010d\r\nStartFragment:%010d\r\nEndFragment:%010d\r\n"
	prefix := "<html><body><!--StartFragment-->"
	suffix := "<!--EndFragment--></body></html>"
	h := len(fmt.Sprintf(header, 0, 0, 0, 0))
	startFragment := h + len(prefix)
	endFragment := startFragment + len(markup)
	doc := fmt.Sprintf(header, h, endFragment+len(suffix), startFragment, endFragment) + prefix + markup + suffix
	return append([]byte(doc), 0)
}

func (c clipboard) ReadImage() []byte {
	if !c.open() {
		return nil
	}
	defer closeClipboard()
	if data := clipboardData(cfPNG); len(data) > 0 {
		return data
	}
	img := dibToImage(clipboardData(cfDIB))
	if img == nil {
		return nil
	}
	var buf bytes.Buffer
	if png.Encode(&buf, img) != nil {
		return nil
	}
	return buf.Bytes()
}

func (c clipboard) WriteImage(data []byte) error {
	if _, err := png.Decode(bytes.NewReader(data)); err != nil {
		return err
	}
	return c.WriteData(transfer.New(transfer.NewItem(transfer.Bytes(transfer.PNG, data))), nil)
}

// dibToImage decodes an uncompressed 24 or 32-bit device independent
// bitmap.
func dibToImage(dib []byte) image.Image {
	if len(dib) < 40 {
		return nil
	}
	le := binary.LittleEndian
	headerSize := le.Uint32(dib)
	width, height := int(int32(le.Uint32(dib[4:]))), int(int32(le.Uint32(dib[8:])))
	bits, compression := le.Uint16(dib[14:]), le.Uint32(dib[16:])
	if width <= 0 || height == 0 || (bits != 24 && bits != 32) || (compression != 0 && compression != 3) {
		return nil
	}
	offset := int(headerSize)
	if offset < 40 || offset > len(dib) {
		return nil
	}
	if compression == 3 && headerSize == 40 {
		offset += 12 // BI_BITFIELDS masks
	}
	bottomUp := height > 0
	if !bottomUp {
		height = -height
	}
	// Clipboard bitmap dimensions are untrusted. Bound output and check
	// division before multiplication, including the INT_MIN height case.
	if width > (64<<20)/4 || height > (64<<20)/(4*width) {
		return nil
	}
	stride := ((width*int(bits) + 31) / 32) * 4
	if offset > len(dib) || height > (len(dib)-offset)/stride {
		return nil
	}
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	px := int(bits / 8)
	for y := 0; y < height; y++ {
		row := y
		if bottomUp {
			row = height - 1 - y
		}
		line := dib[offset+row*stride:]
		for x := 0; x < width; x++ {
			p := line[x*px:]
			a := uint8(255)
			if bits == 32 && headerSize > 40 {
				a = p[3]
			}
			img.SetNRGBA(x, y, color.NRGBA{R: p[2], G: p[1], B: p[0], A: a})
		}
	}
	return img
}

// imageToDIB encodes a top-down 32-bit BITMAPINFOHEADER bitmap.
func imageToDIB(img image.Image) []byte {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	out := make([]byte, 40+4*w*h)
	le := binary.LittleEndian
	le.PutUint32(out, 40)
	le.PutUint32(out[4:], uint32(w))
	le.PutUint32(out[8:], uint32(int32(-h)))
	le.PutUint16(out[12:], 1)
	le.PutUint16(out[14:], 32)
	le.PutUint32(out[20:], uint32(4*w*h))
	i := 40
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			out[i], out[i+1], out[i+2], out[i+3] = c.B, c.G, c.R, c.A
			i += 4
		}
	}
	return out
}

func (c clipboard) Clear() { _ = c.WriteData(transfer.Data{}, nil) }
func (c clipboard) AvailableFormats() []string {
	var out []string
	for _, f := range c.Formats() {
		out = append(out, string(f))
	}
	return out
}

// Shell.

type shell struct{}

func shellExecute(verb, file, params string) error {
	r, _, _ := procShellExecuteW.Call(0, uintptr(unsafe.Pointer(u16Opt(verb))), uintptr(unsafe.Pointer(u16(file))), uintptr(unsafe.Pointer(u16Opt(params))), 0, swShowNormal)
	if r <= 32 {
		return fmt.Errorf("mygo: cannot open %s (error %d)", file, r)
	}
	return nil
}

func (shell) OpenExternal(url string) error { return shellExecute("open", url, "") }
func (shell) OpenPath(path string) error    { return shellExecute("open", path, "") }

func (shell) ShowItemInFolder(path string) {
	_ = shellExecute("open", "explorer.exe", `/select,"`+path+`"`)
}

func (shell) Beep() { procMessageBeep.Call(0) }

// Screen.

type screen struct{}

var monitorEnumCallback uintptr

func (screen) Displays() []platform.Display {
	s := screenSpace()
	out := make([]platform.Display, 0, len(s.displays))
	for i, d := range s.displays {
		info := monitorInfo(d.handle)
		bounds, work := s.displayDIPs(i)
		out = append(out, platform.Display{
			ID:          int64(d.handle),
			Label:       string(utf16Decode(info.Device[:])),
			Bounds:      bounds,
			WorkArea:    work,
			ScaleFactor: float64(d.dpi) / 96,
			Primary:     info.Flags&monitorInfoPrimary != 0,
		})
	}
	return out
}

// screenDisplays returns the monitors, in pixels. Main thread only.
func screenDisplays() []screenDisplay {
	var displays []screenDisplay
	enumMonitors(func(m uintptr) {
		info := monitorInfo(m)
		displays = append(displays, screenDisplay{bounds: pixelRect(info.Monitor), work: pixelRect(info.Work), dpi: monitorDPI(m), handle: m})
	})
	return displays
}

// screenSpace returns the space of DIPs of the monitors as they are now,
// which every conversion of a point or a rectangle of the screen between
// pixels and DIPs goes through. Main thread only.
func screenSpace() dipSpace { return newDIPSpace(screenDisplays(), dpiOf(0)) }

var monitorSink func(uintptr)

// enumMonitors calls fn with every monitor. Main thread only.
func enumMonitors(fn func(uintptr)) {
	if monitorEnumCallback == 0 {
		monitorEnumCallback = syscall.NewCallback(func(mon, _, _, _ uintptr) uintptr {
			if monitorSink != nil {
				monitorSink(mon)
			}
			return 1
		})
	}
	monitorSink = fn
	procEnumDisplayMonitors.Call(0, 0, monitorEnumCallback, 0)
	monitorSink = nil
}

func (screen) CursorPoint() platform.Point {
	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	return screenSpace().pointToDIP(int(pt.X), int(pt.Y))
}

// Theme.

type theme struct{ b *Backend }

func (t theme) IsDark() bool { return t.b.isDark() }

// UIFont is DirectWrite's to find: Segoe UI.
func (theme) UIFont() string { return "" }

func (theme) FontRendering() platform.FontRendering { return platform.FontRendering{} }

func (t theme) SetSource(source string) {
	t.b.themeSource = source
	t.b.applyTheme()
}

const (
	spiGetHighContrast        = 0x0042
	spiSetHighContrast        = 0x0043
	spiGetClientAreaAnimation = 0x1042
	spiSetClientAreaAnimation = 0x1043
	hcfHighContrastOn         = 0x1
	accessibilityKey          = `Software\Microsoft\Accessibility`
	dwmKey                    = `Software\Microsoft\Windows\DWM`
	personalizeKey            = `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`
	textScaleFactor           = "TextScaleFactor"
	settingImmersiveColorSet  = "ImmersiveColorSet"
	settingWindowMetrics      = "WindowMetrics"
	settingSlateMode          = "ConvertibleSlateMode"
	// settingIntl is the lParam of WM_SETTINGCHANGE when the display
	// languages or the regional formats changed.
	settingIntl = "intl"
)

// highContrast is HIGHCONTRASTW.
type highContrast struct {
	size, flags   uint32
	defaultScheme uintptr
}

// highContrastOn reports whether a contrast theme is on.
func highContrastOn() bool {
	hc := highContrast{size: uint32(unsafe.Sizeof(highContrast{}))}
	r, _, _ := procSystemParametersInfoW.Call(spiGetHighContrast, uintptr(hc.size), uintptr(unsafe.Pointer(&hc)), 0)
	return r != 0 && hc.flags&hcfHighContrastOn != 0
}

// Preferences reads the accent of the title bars and the Start menu, the
// settings of animation and transparency effects and contrast themes, the
// size of text of Accessibility, and the pointing devices.
func (t theme) Preferences() platform.Preferences {
	var p platform.Preferences
	if v, ok := regDWORD(hkeyCurrentUser, dwmKey, "AccentColor"); ok { // 0xAABBGGRR
		p.Accent = platform.Color{R: uint8(v), G: uint8(v >> 8), B: uint8(v >> 16), A: 255}
	}
	var animate int32
	if r, _, _ := procSystemParametersInfoW.Call(spiGetClientAreaAnimation, 0, uintptr(unsafe.Pointer(&animate)), 0); r != 0 {
		p.ReduceMotion = animate == 0
	}
	// Transparency effects of Personalization > Colors, which Windows's
	// own surfaces (Mica, Acrylic) follow; toggling them broadcasts
	// ImmersiveColorSet.
	if v, ok := regDWORD(hkeyCurrentUser, personalizeKey, "EnableTransparency"); ok {
		p.ReduceTransparency = v == 0
	}
	p.HighContrast = highContrastOn()
	if v, ok := regDWORD(hkeyCurrentUser, accessibilityKey, textScaleFactor); ok && v >= 100 && v <= 500 {
		p.TextScale = float64(v) / 100
	}
	p.Pointer, p.AnyPointer, p.NoHover, p.NoAnyHover = pointingDevices()
	return p
}

// GetSystemMetrics' metrics of the pointing devices, and the bits of
// SM_DIGITIZER's.
const (
	smMousePresent     = 19
	smDigitizer        = 94
	smConvertibleSlate = 0x2003
	nidIntegratedTouch = 0x01
	nidExternalTouch   = 0x02
	nidIntegratedPen   = 0x04
	nidExternalPen     = 0x08
	nidReady           = 0x80
	nidTouch           = nidIntegratedTouch | nidExternalTouch
	nidPen             = nidIntegratedPen | nidExternalPen
)

// pointingDevices returns how accurate the primary pointing device is and
// the most accurate of all, and whether they cannot hover, as Chromium
// tells them on Windows: a mouse (SM_MOUSEPRESENT, which a virtual one
// sets too) and a pen point finely and hover, a touch screen coarsely,
// without hovering; with a touch screen, the primary device is the finest
// but in tablet mode (SM_CONVERTIBLESLATEMODE 0), where it is the touch
// screen.
func pointingDevices() (primary, finest platform.PointerAccuracy, noHover, noneHover bool) {
	mouse, _, _ := procGetSystemMetrics.Call(smMousePresent)
	digitizer, _, _ := procGetSystemMetrics.Call(smDigitizer)
	if digitizer&nidReady == 0 {
		digitizer = 0
	}
	fine, touch := mouse != 0 || digitizer&nidPen != 0, digitizer&nidTouch != 0
	switch {
	case fine:
		finest = platform.PointerFine
	case touch:
		finest, noneHover = platform.PointerCoarse, true
	default:
		finest, noneHover = platform.PointerNone, true
	}
	primary, noHover = finest, noneHover
	if touch && fine {
		if slate, _, _ := procGetSystemMetrics.Call(smConvertibleSlate); slate == 0 {
			primary, noHover = platform.PointerCoarse, true
		}
	}
	return primary, finest, noHover, noneHover
}

// preferencesChanged reports whether a WM_SETTINGCHANGE is about the
// preferences: the colors, animation effects, contrast themes, the size
// of text or tablet mode.
func preferencesChanged(wp, lp uintptr) bool {
	switch wp {
	case spiSetClientAreaAnimation, spiSetHighContrast:
		return true
	}
	if lp == 0 {
		return false
	}
	switch wstr(lp) {
	case settingImmersiveColorSet, settingWindowMetrics, textScaleFactor, settingSlateMode:
		return true
	}
	return false
}

func (b *Backend) isDark() bool {
	switch b.themeSource {
	case "dark":
		return true
	case "light":
		return false
	}
	light, ok := regDWORD(hkeyCurrentUser, personalizeKey, "AppsUseLightTheme")
	return ok && light == 0
}

func (b *Backend) applyTheme() {
	b.applyMenuTheme()
	for _, w := range b.windows {
		b.applyWindowTheme(w)
		w.withWebView(func() { b.applyWebViewTheme(w) })
	}
}

// applyWindowTheme matches the title bar, or the controls drawn in its
// place, and the menus to the appearance.
func (b *Backend) applyWindowTheme(w *window) {
	dark := int32(0)
	if b.isDark() {
		dark = 1
	}
	procDwmSetWindowAttribute.Call(w.hwnd, dwmwaUseImmersiveDarkMode, uintptr(unsafe.Pointer(&dark)), 4)
	if w.caption != nil {
		w.caption.paint()
	}
	// The bar is drawn again in the appearance, with the line below it.
	if w.menuShown() {
		procRedrawWindow.Call(w.hwnd, 0, 0, rdwInvalidate|rdwFrame|rdwUpdateNow)
	}
}

// applyWebViewTheme makes prefers-color-scheme follow an overridden
// appearance; "system" leaves it to WebView2. The profile's
// PreferredColorScheme holds from a document's start, where DevTools'
// emulation, for runtimes without it, reaches a new document a moment
// after it started: long enough for it to see the system's appearance.
func (b *Backend) applyWebViewTheme(w *window) {
	scheme, value := uintptr(0), "" // COREWEBVIEW2_PREFERRED_COLOR_SCHEME_AUTO
	switch b.themeSource {
	case "light":
		scheme, value = 1, "light"
	case "dark":
		scheme, value = 2, "dark"
	}
	if wv13 := queryInterface(w.webview, &iidICoreWebView2_13); wv13 != 0 {
		var profile uintptr
		comCall(wv13, wv13GetProfile, uintptr(unsafe.Pointer(&profile)))
		release(wv13)
		if profile != 0 {
			hr := comCall(profile, profilePutPreferredColorScheme, scheme)
			release(profile)
			if !failed(hr) {
				return
			}
		}
	}
	w.devtools("Emulation.setEmulatedMedia", `{"features":[{"name":"prefers-color-scheme","value":"`+value+`"}]}`, nil)
}
