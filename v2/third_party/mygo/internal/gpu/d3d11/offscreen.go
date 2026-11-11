//go:build windows

package d3d11

import (
	"fmt"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"github.com/egoist/mygo/internal/scene"
)

// RenderOffscreen draws s with a renderer of its own into a window that
// never shows, and returns the premultiplied BGRA rows of its back buffer
// and their stride: for tests, as packages drawing effects have.
func RenderOffscreen(s *scene.Scene) ([]byte, int, error) {
	hwnd, err := newHiddenWindow(s.Width, s.Height)
	if err != nil {
		return nil, 0, err
	}
	defer destroyWindow(hwnd)
	r, err := New(hwnd)
	if err != nil {
		return nil, 0, err
	}
	defer r.Release()
	if err := r.draw(s); err != nil {
		return nil, 0, err
	}
	return r.read()
}

// newHiddenWindow creates a window that never shows, for a swap chain.
func newHiddenWindow(w, h int) (uintptr, error) {
	class, _ := syscall.UTF16PtrFromString("STATIC")
	const wsPopup = 0x80000000
	hwnd, _, err := user32.NewProc("CreateWindowExW").Call(0, uintptr(unsafe.Pointer(class)), 0, wsPopup, 0, 0, uintptr(w), uintptr(h), 0, 0, 0, 0)
	if hwnd == 0 {
		return 0, fmt.Errorf("d3d11: cannot create a window: %w", err)
	}
	return hwnd, nil
}

func destroyWindow(hwnd uintptr) { user32.NewProc("DestroyWindow").Call(hwnd) }

// read copies the back buffer to memory: BGRA rows and their stride.
func (r *Renderer) read() ([]byte, int, error) { return r.readPart(r.w, r.h) }

// readPart copies the w×h pixels at the top left of the back buffer to
// memory, as read.
func (r *Renderer) readPart(w, h int) ([]byte, int, error) {
	var back, staging uintptr
	if hr := call(r.swapChain, scGetBuffer, 0, uintptr(unsafe.Pointer(&iidID3D11Texture2D)), uintptr(unsafe.Pointer(&back))); failed(hr) {
		return nil, 0, fmt.Errorf("d3d11: no back buffer: %#x", uint32(hr))
	}
	defer free(&back)
	const usageStaging, cpuAccessRead, mapRead, ctxCopyResource = 3, 0x20000, 1, 47
	desc := texture2DDesc{Width: uint32(r.bw), Height: uint32(r.bh), MipLevels: 1, ArraySize: 1, Format: formatB8G8R8A8Unorm,
		SampleCount: 1, Usage: usageStaging, CPUAccessFlags: cpuAccessRead}
	if hr := call(r.device, devCreateTexture2D, uintptr(unsafe.Pointer(&desc)), 0, uintptr(unsafe.Pointer(&staging))); failed(hr) {
		return nil, 0, fmt.Errorf("d3d11: no staging texture: %#x", uint32(hr))
	}
	defer free(&staging)
	call(r.ctx, ctxCopyResource, staging, back)
	var m mapped
	if hr := call(r.ctx, ctxMap, staging, 0, mapRead, 0, uintptr(unsafe.Pointer(&m))); failed(hr) {
		return nil, 0, fmt.Errorf("d3d11: cannot map: %#x", uint32(hr))
	}
	defer call(r.ctx, ctxUnmap, staging, 0)
	pix := make([]byte, w*h*4)
	for y := range h {
		copy(pix[y*w*4:], unsafe.Slice((*byte)(ptr(m.Data+uintptr(y)*uintptr(m.RowPitch))), w*4))
	}
	return pix, w * 4, nil
}

// Offscreen draws scenes into a window that never shows, timing each on
// the GPU, for measurements (BenchmarkStressFrameGPU).
type Offscreen struct {
	r                *Renderer
	hwnd             uintptr
	disjoint, t0, t1 uintptr
}

// NewOffscreen returns an Offscreen drawing frames of w×h pixels.
func NewOffscreen(w, h int) (*Offscreen, error) {
	hwnd, err := newHiddenWindow(w, h)
	if err != nil {
		return nil, err
	}
	r, err := New(hwnd)
	if err != nil {
		destroyWindow(hwnd)
		return nil, err
	}
	o := &Offscreen{r: r, hwnd: hwnd}
	const queryTimestamp, queryTimestampDisjoint = 2, 3
	for _, q := range []struct {
		kind uint32
		out  *uintptr
	}{{queryTimestampDisjoint, &o.disjoint}, {queryTimestamp, &o.t0}, {queryTimestamp, &o.t1}} {
		desc := [2]uint32{q.kind, 0}
		if hr := call(r.device, devCreateQuery, uintptr(unsafe.Pointer(&desc)), uintptr(unsafe.Pointer(q.out))); failed(hr) {
			o.Release()
			return nil, fmt.Errorf("d3d11: cannot create a query: %#x", uint32(hr))
		}
	}
	return o, nil
}

// Render draws s whole and returns the time the GPU spent drawing it, once
// it is done.
func (o *Offscreen) Render(s *scene.Scene) (time.Duration, error) {
	ctx := o.r.ctx
	call(ctx, ctxBegin, o.disjoint)
	call(ctx, ctxEnd, o.t0)
	if err := o.r.draw(s); err != nil {
		return 0, err
	}
	call(ctx, ctxEnd, o.t1)
	call(ctx, ctxEnd, o.disjoint)
	call(ctx, ctxFlush)
	var clock struct {
		Frequency uint64
		Disjoint  int32
		_         int32
	}
	var start, end uint64
	get := func(q uintptr, out unsafe.Pointer, size uintptr) {
		// S_FALSE, 1, until the GPU gets there.
		for call(ctx, ctxGetData, q, uintptr(out), size, 0) == 1 {
			runtime.Gosched()
		}
	}
	get(o.disjoint, unsafe.Pointer(&clock), unsafe.Sizeof(clock))
	get(o.t0, unsafe.Pointer(&start), 8)
	get(o.t1, unsafe.Pointer(&end), 8)
	if clock.Disjoint != 0 || clock.Frequency == 0 {
		return 0, nil
	}
	return time.Duration(float64(end-start) / float64(clock.Frequency) * float64(time.Second)), nil
}

// Release frees what the Offscreen holds.
func (o *Offscreen) Release() {
	free(&o.t1)
	free(&o.t0)
	free(&o.disjoint)
	o.r.Release()
	destroyWindow(o.hwnd)
}
