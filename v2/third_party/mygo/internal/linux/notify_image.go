//go:build linux && (amd64 || arm64)

package linux

import (
	"sync"

	"github.com/egoist/mygo/internal/platform"
)

// notificationImageHint is imageHint once LinkNotificationImages links
// it: the backend's methods are linked into every app, and only call it.
var notificationImageHint func(image []byte) ptr

// LinkNotificationImages links the code of notifications' images, when
// the app first makes a notification with one.
func LinkNotificationImages() { notificationImageHint = imageHint }

var (
	notifyImageOnce sync.Once

	gVariantNewFixedArray     func(typ *byte, elements ptr, n, size uintptr) ptr
	gdkPixbufGetRowstride     func(p ptr) int32
	gdkPixbufGetHasAlpha      func(p ptr) bool
	gdkPixbufGetBitsPerSample func(p ptr) int32
	gdkPixbufGetNChannels     func(p ptr) int32
	gdkPixbufGetPixels        func(p ptr) ptr
)

func loadNotifyImages() {
	notifyImageOnce.Do(func() {
		mustBind(libGLib, &gVariantNewFixedArray, "g_variant_new_fixed_array")
		// Bound with the icons of menu items too, to the same functions.
		mustBind(libPixbuf, &gdkPixbufScaleSimple, "gdk_pixbuf_scale_simple")
		mustBind(libPixbuf, &gdkPixbufGetWidth, "gdk_pixbuf_get_width")
		mustBind(libPixbuf, &gdkPixbufGetHeight, "gdk_pixbuf_get_height")
		mustBind(libPixbuf, &gdkPixbufGetRowstride, "gdk_pixbuf_get_rowstride")
		mustBind(libPixbuf, &gdkPixbufGetHasAlpha, "gdk_pixbuf_get_has_alpha")
		mustBind(libPixbuf, &gdkPixbufGetBitsPerSample, "gdk_pixbuf_get_bits_per_sample")
		mustBind(libPixbuf, &gdkPixbufGetNChannels, "gdk_pixbuf_get_n_channels")
		mustBind(libPixbuf, &gdkPixbufGetPixels, "gdk_pixbuf_get_pixels")
	})
}

// notifyImageBox is the most pixels across of a notification's image:
// servers draw it a few times the size of an icon at most (dunst at 128
// pixels, mako at 64), and the bus copies every byte of it.
const notifyImageBox = 256

// imageHint returns the hint "image-data" of an image, which the
// notification spec prefers to a file the server would read later: the
// pixels GdkPixbuf decodes, fit to notifyImageBox, given as libnotify
// gives a pixbuf (width, height, row stride, alpha, bits per sample,
// channels, then the bytes, the last row without its padding). It
// returns 0 for an image GdkPixbuf cannot decode.
func imageHint(image []byte) ptr {
	loadNotifyImages()
	pix, err := pixbufFromPNG(image) // any format GdkPixbuf has a loader for
	if err != nil {
		return 0
	}
	defer gObjectUnref(pix)
	w, h := gdkPixbufGetWidth(pix), gdkPixbufGetHeight(pix)
	if w > notifyImageBox || h > notifyImageBox {
		fw, fh := platform.FitIcon(int(w), int(h), notifyImageBox)
		const interpHyper = 3 // GDK_INTERP_HYPER, the finest when shrinking
		scaled := gdkPixbufScaleSimple(pix, int32(fw), int32(fh), interpHyper)
		if scaled == 0 {
			return 0
		}
		defer gObjectUnref(scaled)
		pix, w, h = scaled, int32(fw), int32(fh)
	}
	stride, bits, channels := gdkPixbufGetRowstride(pix), gdkPixbufGetBitsPerSample(pix), gdkPixbufGetNChannels(pix)
	n := (h-1)*stride + w*((channels*bits+7)/8)
	data := gVariantNewFixedArray(cs("y"), gdkPixbufGetPixels(pix), uintptr(n), 1)
	value := tuple(
		gVariantNewInt32(w),
		gVariantNewInt32(h),
		gVariantNewInt32(stride),
		gVariantNewBoolean(gdkPixbufGetHasAlpha(pix)),
		gVariantNewInt32(bits),
		gVariantNewInt32(channels),
		data,
	)
	return gVariantNewDictEntry(gVariantNewString(cs("image-data")), gVariantNewVariant(value))
}
