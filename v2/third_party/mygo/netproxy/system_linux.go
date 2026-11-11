//go:build linux && (amd64 || arm64)

package netproxy

import (
	"net/url"
	"unsafe"

	"github.com/ebitengine/purego"
)

// Linux asks GIO's default proxy resolver, as WebKitGTK does: GNOME's
// settings, KDE's and PAC files through libproxy, the desktop portal in a
// sandbox. Without GIO, requests go direct.

type gioAPI struct {
	resolver  func() uintptr
	lookup    func(resolver uintptr, uri string, cancellable uintptr, err *uintptr) uintptr
	strfreev  func(v uintptr)
	errorFree func(err uintptr)
}

var gio lazy[*gioAPI]

func loadGIO() *gioAPI {
	lib, err := purego.Dlopen("libgio-2.0.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return nil
	}
	g := &gioAPI{}
	// GLib's functions are found through GIO, which links it.
	if !bind(lib, &g.resolver, "g_proxy_resolver_get_default") ||
		!bind(lib, &g.lookup, "g_proxy_resolver_lookup") ||
		!bind(lib, &g.strfreev, "g_strfreev") ||
		!bind(lib, &g.errorFree, "g_error_free") {
		return nil
	}
	return g
}

func bind(lib uintptr, fn any, name string) bool {
	p, err := purego.Dlsym(lib, name)
	if err != nil || p == 0 {
		return false
	}
	purego.RegisterFunc(fn, p)
	return true
}

func system(target *url.URL) *url.URL {
	g := gio.get(loadGIO)
	if g == nil {
		return nil
	}
	r := g.resolver() // not owned
	if r == 0 {
		return nil
	}
	var gerr uintptr
	list := g.lookup(r, target.String(), 0, &gerr)
	if list == 0 {
		if gerr != 0 {
			g.errorFree(gerr)
		}
		return nil
	}
	defer g.strfreev(list)
	var proxies []string
	for p := *(*unsafe.Pointer)(unsafe.Pointer(&list)); *(*uintptr)(p) != 0; p = unsafe.Add(p, unsafe.Sizeof(uintptr(0))) {
		proxies = append(proxies, cString(*(*unsafe.Pointer)(p)))
	}
	return firstSupported(proxies)
}

func cString(p unsafe.Pointer) string {
	n := 0
	for *(*byte)(unsafe.Add(p, n)) != 0 {
		n++
	}
	return string(unsafe.Slice((*byte)(p), n))
}
