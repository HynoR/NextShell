package netproxy

import (
	"net/url"
	"syscall"
	"unsafe"
)

// Windows reads the user's settings (Settings > Network & internet >
// Proxy, the Internet Options of old) with WinHTTP, as Chromium does:
// automatic detection (WPAD) and setup scripts (PAC files) first, then the
// manual proxy and its bypass list.

type winHTTP struct {
	ieConfig, open, setTimeouts, proxyForURL, globalFree *syscall.LazyProc
	session                                              uintptr
}

var winhttp lazy[*winHTTP]

// loadWinHTTP loads WinHTTP from the system directory, never from the
// app's directory or the search path, and opens a synchronous session for
// setup scripts, kept for the process.
func loadWinHTTP() *winHTTP {
	kernel32 := syscall.NewLazyDLL("kernel32.dll") // a KnownDLL
	buf := make([]uint16, syscall.MAX_PATH)
	n, _, _ := kernel32.NewProc("GetSystemDirectoryW").Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 || int(n) > len(buf) {
		return nil
	}
	dll := syscall.NewLazyDLL(syscall.UTF16ToString(buf[:n]) + `\winhttp.dll`)
	w := &winHTTP{
		ieConfig:    dll.NewProc("WinHttpGetIEProxyConfigForCurrentUser"),
		open:        dll.NewProc("WinHttpOpen"),
		setTimeouts: dll.NewProc("WinHttpSetTimeouts"),
		proxyForURL: dll.NewProc("WinHttpGetProxyForUrl"),
		globalFree:  kernel32.NewProc("GlobalFree"),
	}
	for _, p := range []*syscall.LazyProc{w.ieConfig, w.open, w.setTimeouts, w.proxyForURL, w.globalFree} {
		if p.Find() != nil {
			return nil
		}
	}
	agent, _ := syscall.UTF16PtrFromString("mygo")
	w.session, _, _ = w.open.Call(uintptr(unsafe.Pointer(agent)), winhttpAccessTypeNoProxy, 0, 0, 0)
	if w.session != 0 {
		// Bounds fetching a setup script, which may not answer.
		w.setTimeouts.Call(w.session, 5000, 5000, 5000, 5000)
	}
	return w
}

const (
	winhttpAccessTypeNoProxy    = 1
	winhttpAccessTypeNamedProxy = 3

	winhttpAutoproxyAutoDetect = 0x1
	winhttpAutoproxyConfigURL  = 0x2
	winhttpAutoDetectTypeDHCP  = 0x1
	winhttpAutoDetectTypeDNSA  = 0x2
)

// WINHTTP_CURRENT_USER_IE_PROXY_CONFIG. Its strings are the caller's, freed
// with GlobalFree.
type ieProxyConfig struct {
	autoDetect    int32 // BOOL
	autoConfigURL *uint16
	proxy         *uint16
	proxyBypass   *uint16
}

// WINHTTP_AUTOPROXY_OPTIONS.
type autoProxyOptions struct {
	flags                 uint32
	autoDetectFlags       uint32
	autoConfigURL         *uint16
	reserved1             uintptr
	reserved2             uint32
	autoLogonIfChallenged int32 // BOOL
}

// WINHTTP_PROXY_INFO. Its strings are the caller's, freed with GlobalFree.
type proxyInfo struct {
	accessType  uint32
	proxy       *uint16
	proxyBypass *uint16
}

func system(target *url.URL) *url.URL {
	w := winhttp.get(loadWinHTTP)
	if w == nil {
		return nil
	}
	var cfg ieProxyConfig
	if ok, _, _ := w.ieConfig.Call(uintptr(unsafe.Pointer(&cfg))); ok == 0 {
		return nil
	}
	pac, proxy, bypass := w.take(cfg.autoConfigURL), w.take(cfg.proxy), w.take(cfg.proxyBypass)
	if cfg.autoDetect != 0 || pac != "" {
		if p, ok := w.auto(target, cfg.autoDetect != 0, pac); ok {
			return p
		}
	}
	return winStatic(proxy, bypass, target)
}

// auto runs the setup script, the one detection finds or the one at pac.
// It reports false when there is none or it fails.
func (w *winHTTP) auto(target *url.URL, detect bool, pac string) (*url.URL, bool) {
	if w.session == 0 {
		return nil, false
	}
	opts := autoProxyOptions{autoLogonIfChallenged: 1}
	if detect {
		opts.flags |= winhttpAutoproxyAutoDetect
		opts.autoDetectFlags = winhttpAutoDetectTypeDHCP | winhttpAutoDetectTypeDNSA
	}
	if pac != "" {
		u, err := syscall.UTF16PtrFromString(pac)
		if err != nil {
			return nil, false
		}
		opts.flags |= winhttpAutoproxyConfigURL
		opts.autoConfigURL = u
	}
	u, err := syscall.UTF16PtrFromString(target.String())
	if err != nil {
		return nil, false
	}
	var info proxyInfo
	if ok, _, _ := w.proxyForURL.Call(w.session, uintptr(unsafe.Pointer(u)), uintptr(unsafe.Pointer(&opts)), uintptr(unsafe.Pointer(&info))); ok == 0 {
		return nil, false
	}
	proxy, bypass := w.take(info.proxy), w.take(info.proxyBypass)
	if info.accessType != winhttpAccessTypeNamedProxy {
		return nil, true
	}
	return winStatic(proxy, bypass, target), true
}

// take returns the string WinHTTP allocated and frees it.
func (w *winHTTP) take(p *uint16) string {
	if p == nil {
		return ""
	}
	n := 0
	for *(*uint16)(unsafe.Add(unsafe.Pointer(p), 2*n)) != 0 {
		n++
	}
	s := syscall.UTF16ToString(unsafe.Slice(p, n))
	w.globalFree.Call(uintptr(unsafe.Pointer(p)))
	return s
}
