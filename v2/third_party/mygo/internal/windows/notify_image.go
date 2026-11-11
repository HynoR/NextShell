//go:build windows && (amd64 || arm64)

package windows

import (
	"sync"
	"syscall"
	"unsafe"

	"github.com/egoist/mygo/internal/platform"
)

// notificationIcon is balloonIcon once LinkNotificationImages links it:
// the backend's methods are linked into every app, and only call it.
var notificationIcon func(image []byte, dpi int) uintptr

// LinkNotificationImages links the code of notifications' images, when
// the app first makes a notification with one.
func LinkNotificationImages() { notificationIcon = balloonIcon }

// balloonIcon makes the icon a balloon shows of a notification's image in
// place of the tray's: a large one (NIIF_LARGE_ICON), which a balloon
// draws the size of an icon (SM_CXICON), but the toast Windows 10 and
// later show for it at 40 DIPs on Windows 11, blurred when it grows: the
// icon is twice SM_CXICON, which the toast shrinks.
func balloonIcon(image []byte, dpi int) uintptr {
	const smCxIcon = 11
	size := 32 * dpi / 96
	if has(procGetSystemMetricsForDpi) {
		if n, _, _ := procGetSystemMetricsForDpi.Call(smCxIcon, uintptr(dpi)); n != 0 {
			size = int(n)
		}
	}
	return imageIcon(image, 2*size)
}

// WIC's function and identifiers, made on first use: what package
// variables hold is linked into every app.
var (
	wicOnce sync.Once
	wicFns  *wicAPI
)

type wicAPI struct {
	convertBitmapSource *syscall.LazyProc
	clsidFactory        GUID // CLSID_WICImagingFactory, of Windows 7
	iidFactory          GUID
	formatBGRA          GUID // GUID_WICPixelFormat32bppBGRA
}

func wic() *wicAPI {
	wicOnce.Do(func() {
		wicFns = &wicAPI{
			convertBitmapSource: systemDLL("windowscodecs.dll").NewProc("WICConvertBitmapSource"),
			clsidFactory:        guid("cacaf262-9370-4615-a13b-9f5539da4c0a"),
			iidFactory:          guid("ec5ec8a9-c395-4314-9c77-54d7a935ff70"),
			formatBGRA:          guid("6fddc324-4e03-4bfe-b185-3d77768dc90f"),
		}
	})
	return wicFns
}

// imageIcon makes an icon of size by size pixels of an encoded image, in
// any format WIC decodes (PNG, JPEG, ...): the image fit to the icon,
// centered on transparent pixels. It returns 0 for an image WIC cannot
// decode. The caller destroys the icon.
func imageIcon(image []byte, size int) uintptr {
	api := wic()
	if size <= 0 || api.convertBitmapSource.Find() != nil {
		return 0
	}
	var factory uintptr
	if hr, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&api.clsidFactory)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&api.iidFactory)), uintptr(unsafe.Pointer(&factory))); failed(hr) || factory == 0 {
		return 0
	}
	defer release(factory)
	stream := memStream(image)
	if stream == 0 {
		return 0
	}
	defer release(stream)
	// IWICImagingFactory::CreateDecoderFromStream, metadata on demand.
	var decoder uintptr
	if failed(comCall(factory, 4, stream, 0, 0, uintptr(unsafe.Pointer(&decoder)))) || decoder == 0 {
		return 0
	}
	defer release(decoder)
	var frame uintptr
	if failed(comCall(decoder, 13, 0, uintptr(unsafe.Pointer(&frame)))) || frame == 0 { // GetFrame
		return 0
	}
	defer release(frame)
	var fw, fh uint32
	if failed(comCall(frame, 3, uintptr(unsafe.Pointer(&fw)), uintptr(unsafe.Pointer(&fh)))) { // GetSize
		return 0
	}
	w, h := platform.FitIcon(int(fw), int(fh), size)
	if w == 0 {
		return 0
	}
	var scaler uintptr
	if failed(comCall(factory, 11, uintptr(unsafe.Pointer(&scaler)))) || scaler == 0 { // CreateBitmapScaler
		return 0
	}
	defer release(scaler)
	// IWICBitmapScaler::Initialize, with WICBitmapInterpolationModeFant,
	// for shrinking.
	const interpolationFant = 3
	if failed(comCall(scaler, 8, frame, uintptr(w), uintptr(h), interpolationFant)) {
		return 0
	}
	var bgra uintptr
	if hr, _, _ := api.convertBitmapSource.Call(uintptr(unsafe.Pointer(&api.formatBGRA)), scaler, uintptr(unsafe.Pointer(&bgra))); failed(hr) || bgra == 0 {
		return 0
	}
	defer release(bgra)
	pixels := make([]byte, w*h*4)
	if failed(comCall(bgra, 7, 0, uintptr(w*4), uintptr(len(pixels)), uintptr(unsafe.Pointer(&pixels[0])))) { // CopyPixels
		return 0
	}
	// An icon resource, as CreateIconFromResourceEx takes it: a header of
	// twice the height, the pixels bottom-up with straight alpha, then the
	// mask, which the alpha makes moot.
	hdr := bitmapInfoHeader{Width: int32(size), Height: int32(2 * size), Planes: 1, BitCount: 32}
	hdr.Size = uint32(unsafe.Sizeof(hdr))
	maskStride := (size + 31) / 32 * 4
	res := make([]byte, int(hdr.Size)+size*size*4+maskStride*size)
	copy(res, unsafe.Slice((*byte)(unsafe.Pointer(&hdr)), hdr.Size))
	px := res[hdr.Size:]
	left, top := (size-w)/2, (size-h)/2
	for y := range h {
		row := size - 1 - (top + y)
		copy(px[(row*size+left)*4:], pixels[y*w*4:(y+1)*w*4])
	}
	icon, _, _ := procCreateIconFromResourceEx.Call(uintptr(unsafe.Pointer(&res[0])), uintptr(len(res)), 1, 0x00030000, uintptr(size), uintptr(size), 0)
	return icon
}
