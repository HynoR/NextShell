//go:build windows

package raster

import (
	"runtime"
	"syscall"
	"unsafe"
)

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll") // a KnownDLL
	procVirtualAlloc = kernel32.NewProc("VirtualAlloc")
	procVirtualFree  = kernel32.NewProc("VirtualFree")
)

const (
	memCommit     = 0x1000
	memReserve    = 0x2000
	memRelease    = 0x8000
	pageReadWrite = 0x04
)

// allocPixels returns n bytes for the image of a Renderer, in a
// VirtualAlloc of their own outside the Go heap: as on Unix, the GC would
// keep about as much again beside a frame, and GDI's SetDIBitsToDevice
// draws nothing of a frame whose memory spans two of the heap's 4 MB
// reservations (arenas), which a frame of a few MB nearly always does.
func allocPixels(n int) *pixels {
	if n <= 0 {
		return &pixels{b: make([]byte, n)}
	}
	a, _, _ := procVirtualAlloc.Call(0, uintptr(n), memCommit|memReserve, pageReadWrite)
	if a == 0 {
		return &pixels{b: make([]byte, n)}
	}
	// Memory outside the Go heap: converted as vet allows.
	b := unsafe.Slice((*byte)(*(*unsafe.Pointer)(unsafe.Pointer(&a))), n)
	p := &pixels{b: b, mapped: true}
	p.cleanup = runtime.AddCleanup(p, func(a uintptr) { procVirtualFree.Call(a, 0, memRelease) }, a)
	return p
}

// free gives the memory back at once.
func (p *pixels) free() {
	if p == nil {
		return
	}
	if p.mapped {
		p.cleanup.Stop()
		procVirtualFree.Call(uintptr(unsafe.Pointer(unsafe.SliceData(p.b))), 0, memRelease)
	}
	p.b = nil
}
