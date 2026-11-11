//go:build darwin

package sfsymbols

import (
	"image"
	"math"
	"runtime"
	"structs"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	"github.com/egoist/mygo/ui"
)

var (
	selSymbolImage = objc.RegisterName("imageWithSystemSymbolName:accessibilityDescription:")
	selSymbolConfg = objc.RegisterName("configurationWithPointSize:weight:")
	selSymbolApply = objc.RegisterName("imageWithSymbolConfiguration:")
	selCGImage     = objc.RegisterName("CGImageForProposedRect:context:hints:")
	selSize        = objc.RegisterName("size")
	selAlloc       = objc.RegisterName("alloc")
	selInitBytes   = objc.RegisterName("initWithBytes:length:encoding:")
	selRelease     = objc.RegisterName("release")
)

// cgRect is CGRect, which CGContextDrawImage takes by value.
type cgRect struct {
	_          structs.HostLayout
	X, Y, W, H float64
}

// cg holds the CoreGraphics functions the rasterizer calls, once resolved.
var cg struct {
	once         sync.Once
	ok           bool
	space        func() uintptr
	releaseSpace func(space uintptr)
	bitmap       func(data unsafe.Pointer, width, height, bitsPerComponent, bytesPerRow, space uintptr, info uint32) uintptr
	releaseCtx   func(ctx uintptr)
	draw         func(ctx uintptr, rect cgRect, img uintptr)
	width        func(img uintptr) uintptr
	height       func(img uintptr) uintptr

	// poolPush and poolPop are libobjc's autorelease pools; size sends a
	// message that returns an NSSize.
	poolPush, poolPop uintptr
	size              func(obj objc.ID, sel objc.SEL) nsSize
}

// nsSize is NSSize, which -[NSImage size] returns by value.
type nsSize struct {
	_    structs.HostLayout
	W, H float64
}

// cgPremultipliedLast and cgByteOrder32Big name the pixel format of the
// canvases: 8 bits of red, green, blue and alpha each, in that order.
const (
	cgPremultipliedLast = 1
	cgByteOrder32Big    = 4 << 12
)

const nsUTF8StringEncoding = 4

// maxPx is the most pixels square Image draws a symbol.
const maxPx = 512

// naturalSpan is how many times its point size the ink of
// rectangle.on.rectangle, of the widest symbols, spans (its image, with
// its margins, spans 1.47).
const naturalSpan = 1.25

func loadCG() bool {
	cg.once.Do(func() {
		if _, err := purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", purego.RTLD_NOW|purego.RTLD_GLOBAL); err != nil {
			return
		}
		lib, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			return
		}
		purego.RegisterLibFunc(&cg.space, lib, "CGColorSpaceCreateDeviceRGB")
		purego.RegisterLibFunc(&cg.releaseSpace, lib, "CGColorSpaceRelease")
		purego.RegisterLibFunc(&cg.bitmap, lib, "CGBitmapContextCreate")
		purego.RegisterLibFunc(&cg.releaseCtx, lib, "CGContextRelease")
		purego.RegisterLibFunc(&cg.draw, lib, "CGContextDrawImage")
		purego.RegisterLibFunc(&cg.width, lib, "CGImageGetWidth")
		purego.RegisterLibFunc(&cg.height, lib, "CGImageGetHeight")
		libobjc, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			return
		}
		if cg.poolPush, err = purego.Dlsym(libobjc, "objc_autoreleasePoolPush"); err != nil {
			return
		}
		if cg.poolPop, err = purego.Dlsym(libobjc, "objc_autoreleasePoolPop"); err != nil {
			return
		}
		msgSend, err := purego.Dlsym(libobjc, "objc_msgSend")
		if err != nil {
			return
		}
		purego.RegisterFunc(&cg.size, msgSend)
		cg.ok = true
	})
	return cg.ok
}

// Available reports whether this system draws SF Symbols at all: whether
// AppKit and CoreGraphics load here. A particular symbol may still be
// missing from an older macOS, and Image says so for it.
func Available() bool { return loadCG() }

// shapeKey names a symbol's shape: its name, its pixels square and its
// options. tintKey adds the color it is drawn in.
type shapeKey struct {
	name    string
	px      int
	natural bool
	weight  Weight
}

type tintKey struct {
	shapeKey
	r, g, b, a uint8
}

// The caches hold the shapes AppKit drew and the bitmaps tinted from them,
// each up to a limit, past which the least recently used goes: an app
// that animates a symbol's color or size does not grow without end.
const (
	shapeLimit = 256
	tintLimit  = 512
)

var (
	mu     sync.Mutex
	shapes = newLRU[shapeKey, *image.RGBA](shapeLimit)
	// missing holds the names this macOS has no symbol for, so that a
	// Symbol trying a variant (Fill) does not ask AppKit every frame.
	missing = newLRU[string, struct{}](shapeLimit)
	tinted  = newLRU[tintKey, *ui.Bitmap](tintLimit)
)

// Image returns the SF Symbol named name (as "gearshape"), drawn px pixels
// square in col, or false where the system has no such symbol: on another
// OS, or a macOS without it. px is limited to 8 through 512. The result is
// cached, and Image is safe to call from any goroutine.
func Image(name string, px int, col ui.Color, o Options) (ui.ImageSource, bool) {
	if !loadCG() {
		return nil, false
	}
	px = min(max(px, 8), maxPx)
	sk := shapeKey{name: name, px: px, natural: o.Natural, weight: o.Weight}
	tk := tintKey{shapeKey: sk, r: col.R, g: col.G, b: col.B, a: col.A}
	mu.Lock()
	b, ok := tinted.get(tk)
	coverage, drawn := shapes.get(sk)
	_, absent := missing.get(name)
	mu.Unlock()
	if ok {
		return b, true
	}
	if absent {
		return nil, false
	}
	if !drawn {
		var absent bool
		if coverage, drawn, absent = rasterize(name, px, o.Natural, o.Weight); !drawn {
			// Only a name AppKit has no symbol for is remembered: another
			// failure may not happen at another size or weight.
			if absent {
				mu.Lock()
				missing.put(name, struct{}{})
				mu.Unlock()
			}
			return nil, false
		}
	}
	b = ui.NewBitmap(tint(coverage, col))
	mu.Lock()
	shapes.put(sk, coverage)
	tinted.put(tk, b)
	mu.Unlock()
	return b, true
}

// canvas makes a bitmap context over pix, w by h pixels, and returns it
// with the function that releases it.
func canvas(pix []byte, w, h int) (uintptr, func()) {
	space := cg.space()
	ctx := cg.bitmap(unsafe.Pointer(&pix[0]), uintptr(w), uintptr(h), 8, uintptr(w*4), space, cgPremultipliedLast|cgByteOrder32Big)
	cg.releaseSpace(space)
	if ctx == 0 {
		return 0, func() {}
	}
	return ctx, func() {
		cg.releaseCtx(ctx)
		runtime.KeepAlive(pix)
	}
}

// rasterize draws the symbol's outline into a px×px canvas, centered and
// inset a little. With natural, every symbol takes one scale, as AppKit
// draws them at one point size: a cross stays smaller than a wide
// rectangle, rather than each filling the box.
func rasterize(name string, px int, natural bool, weight Weight) (out *image.RGBA, ok, absent bool) {
	if !loadCG() {
		return nil, false, false
	}
	withPool(func() { out, ok, absent = draw(name, px, natural, weight) })
	return out, ok, absent
}

// withPool runs fn in an autorelease pool, for AppKit's temporary objects.
// A pool belongs to its thread: the goroutine stays on one until it pops.
func withPool(fn func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	p, _, _ := purego.SyscallN(cg.poolPush)
	defer purego.SyscallN(cg.poolPop, p)
	fn()
}

// draw is rasterize inside the pool. absent says AppKit has no symbol of
// the name; other failures leave it false.
func draw(name string, px int, natural bool, weight Weight) (out *image.RGBA, ok, absent bool) {
	str := nsString(name)
	defer objc.Send[objc.ID](str, selRelease)
	img := objc.Send[objc.ID](objc.ID(objc.GetClass("NSImage")), selSymbolImage, str, objc.ID(0))
	if img == 0 {
		return nil, false, true
	}
	// A point size of px renders at the main screen's scale, 2× on a
	// Retina screen: the canvas below scales it down, never up.
	cfg := objc.Send[objc.ID](objc.ID(objc.GetClass("NSImageSymbolConfiguration")), selSymbolConfg, float64(px), weight.fontWeight())
	img = objc.Send[objc.ID](img, selSymbolApply, cfg)
	if img == 0 {
		return nil, false, false
	}
	points := cg.size(img, selSize)
	cgImage := objc.Send[objc.ID](img, selCGImage, uintptr(0), uintptr(0), uintptr(0))
	if cgImage == 0 {
		return nil, false, false
	}
	w, h := int(cg.width(uintptr(cgImage))), int(cg.height(uintptr(cgImage)))
	if w <= 0 || h <= 0 || points.W <= 0 {
		return nil, false, false
	}
	// pixelsPerPoint is the scale AppKit chose for the image.
	pixelsPerPoint := float64(w) / points.W

	// The glyph inside its alignment margins, in a canvas of its own size.
	src := make([]byte, w*h*4)
	ctx, done := canvas(src, w, h)
	if ctx == 0 {
		return nil, false, false
	}
	cg.draw(ctx, cgRect{W: float64(w), H: float64(h)}, uintptr(cgImage))
	done()
	minX, minY, maxX, maxY := w, h, -1, -1
	for y := range h {
		for x := range w {
			if src[y*w*4+x*4+3] != 0 {
				minX, maxX = min(minX, x), max(maxX, x)
				minY, maxY = min(minY, y), max(maxY, y)
			}
		}
	}
	if maxX < 0 {
		return nil, false, false
	}
	gw, gh := maxX-minX+1, maxY-minY+1

	// The glyph, scaled into the box and centered.
	dst := make([]byte, px*px*4)
	dctx, ddone := canvas(dst, px, px)
	if dctx == 0 {
		return nil, false, false
	}
	const inset = 0.85
	scale := math.Min(inset*float64(px)/float64(gw), inset*float64(px)/float64(gh))
	if natural {
		// Every symbol at one scale of points to pixels, whatever the
		// screen's: rectangle.on.rectangle, of the widest, spans
		// naturalSpan times the point size, and it, so every other, fits.
		scale = math.Min(scale, 0.92/(naturalSpan*pixelsPerPoint))
	}
	dw, dh := float64(gw)*scale, float64(gh)*scale
	// The image holds the glyph inside alignment margins: scale the whole
	// image so that the glyph, and not the margins, fills the box.
	cg.draw(dctx, cgRect{
		X: (float64(px)-dw)/2 - float64(minX)*scale,
		// CoreGraphics counts y up from the bottom, the rows down from the
		// top: the margin below the glyph is the rows after maxY.
		Y: (float64(px)-dh)/2 - float64(h-1-maxY)*scale,
		W: float64(w) * scale,
		H: float64(h) * scale,
	}, uintptr(cgImage))
	ddone()

	out = image.NewRGBA(image.Rect(0, 0, px, px))
	for i := range px * px {
		a := dst[i*4+3]
		out.Pix[i*4+0], out.Pix[i*4+1], out.Pix[i*4+2], out.Pix[i*4+3] = a, a, a, a
	}
	return out, true, false
}

// tint colors coverage, whose alpha holds the shape, in col.
func tint(coverage *image.RGBA, col ui.Color) *image.RGBA {
	out := image.NewRGBA(coverage.Rect)
	for i := range len(coverage.Pix) / 4 {
		a := uint16(coverage.Pix[i*4+3]) * uint16(col.A) / 255
		out.Pix[i*4+0] = uint8(uint16(col.R) * a / 255)
		out.Pix[i*4+1] = uint8(uint16(col.G) * a / 255)
		out.Pix[i*4+2] = uint8(uint16(col.B) * a / 255)
		out.Pix[i*4+3] = uint8(a)
	}
	return out
}

// nsString makes an owned NSString for s, which the caller releases. It
// copies the bytes, which stay alive until the message returns.
func nsString(s string) objc.ID {
	b := append([]byte(s), 0)
	str := objc.Send[objc.ID](objc.Send[objc.ID](objc.ID(objc.GetClass("NSString")), selAlloc), selInitBytes,
		unsafe.Pointer(&b[0]), uintptr(len(s)), uintptr(nsUTF8StringEncoding))
	runtime.KeepAlive(b)
	return str
}
