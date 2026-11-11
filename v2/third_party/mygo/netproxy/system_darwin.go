//go:build darwin && (amd64 || arm64)

package netproxy

import (
	"net"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

// macOS asks CFNetwork, as Safari does: CFNetworkCopyProxiesForURL applies
// the settings of System Settings > Network > Proxies to the URL, with
// their exceptions, and names the setup script (PAC file) to run when
// there is one, which CFNetworkExecuteProxyAutoConfigurationURL runs on
// this thread's run loop.

// pacTimeout bounds fetching and running a setup script.
const pacTimeout = 5 * time.Second

const (
	cfStringEncodingUTF8 = 0x08000100
	cfNumberSInt32Type   = 3
	cfRunLoopRunFinished = 1
)

// CFStreamClientContext.
type streamClientContext struct {
	version                          int
	info                             uintptr
	retain, release, copyDescription uintptr
}

type cfAPI struct {
	release        func(cf uintptr)
	typeID         func(cf uintptr) uint
	equal          func(a, b uintptr) bool
	stringTypeID   func() uint
	numberTypeID   func() uint
	dictTypeID     func() uint
	stringCreate   func(alloc uintptr, s string, encoding uint32) uintptr
	stringLength   func(s uintptr) int
	stringMaxSize  func(length int, encoding uint32) int
	stringGet      func(s uintptr, buf *byte, size int, encoding uint32) bool
	numberGet      func(n uintptr, typ int, value *int32) bool
	urlCreate      func(alloc, s, base uintptr) uintptr
	arrayCount     func(a uintptr) int
	arrayAt        func(a uintptr, i int) uintptr
	dictGet        func(d, key uintptr) uintptr
	runLoopCurrent func() uintptr
	addSource      func(rl, source, mode uintptr)
	removeSource   func(rl, source, mode uintptr)
	invalidate     func(source uintptr)
	runInMode      func(mode uintptr, seconds float64, returnAfterSourceHandled bool) int32
	stop           func(rl uintptr)

	copySettings func() uintptr
	copyProxies  func(url, settings uintptr) uintptr
	runPACURL    func(script, url, callback uintptr, ctx *streamClientContext) uintptr
	runPACScript func(script, url, callback uintptr, ctx *streamClientContext) uintptr

	poolPush, poolPop uintptr

	keyType, keyHost, keyPort, keyPACURL, keyPACScript                  uintptr
	typeNone, typeHTTP, typeHTTPS, typeSOCKS, typePACURL, typePACScript uintptr

	// mode is the private run loop mode setup scripts run in, and
	// callback their completion callback, made once.
	mode, callback uintptr
}

var cfLazy lazy[*cfAPI]

// cf returns CoreFoundation's and CFNetwork's functions, or nil.
func cf() *cfAPI { return cfLazy.get(loadCF) }

func loadCF() *cfAPI {
	open := func(path string) uintptr {
		h, _ := purego.Dlopen(path, purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		return h
	}
	core := open("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation")
	network := open("/System/Library/Frameworks/CFNetwork.framework/CFNetwork")
	objc := open("/usr/lib/libobjc.A.dylib")
	if core == 0 || network == 0 || objc == 0 {
		return nil
	}
	a := &cfAPI{}
	for _, f := range []struct {
		lib  uintptr
		fn   any
		name string
	}{
		{core, &a.release, "CFRelease"},
		{core, &a.typeID, "CFGetTypeID"},
		{core, &a.equal, "CFEqual"},
		{core, &a.stringTypeID, "CFStringGetTypeID"},
		{core, &a.numberTypeID, "CFNumberGetTypeID"},
		{core, &a.dictTypeID, "CFDictionaryGetTypeID"},
		{core, &a.stringCreate, "CFStringCreateWithCString"},
		{core, &a.stringLength, "CFStringGetLength"},
		{core, &a.stringMaxSize, "CFStringGetMaximumSizeForEncoding"},
		{core, &a.stringGet, "CFStringGetCString"},
		{core, &a.numberGet, "CFNumberGetValue"},
		{core, &a.urlCreate, "CFURLCreateWithString"},
		{core, &a.arrayCount, "CFArrayGetCount"},
		{core, &a.arrayAt, "CFArrayGetValueAtIndex"},
		{core, &a.dictGet, "CFDictionaryGetValue"},
		{core, &a.runLoopCurrent, "CFRunLoopGetCurrent"},
		{core, &a.addSource, "CFRunLoopAddSource"},
		{core, &a.removeSource, "CFRunLoopRemoveSource"},
		{core, &a.invalidate, "CFRunLoopSourceInvalidate"},
		{core, &a.runInMode, "CFRunLoopRunInMode"},
		{core, &a.stop, "CFRunLoopStop"},
		{network, &a.copySettings, "CFNetworkCopySystemProxySettings"},
		{network, &a.copyProxies, "CFNetworkCopyProxiesForURL"},
		{network, &a.runPACURL, "CFNetworkExecuteProxyAutoConfigurationURL"},
		{network, &a.runPACScript, "CFNetworkExecuteProxyAutoConfigurationScript"},
	} {
		p, err := purego.Dlsym(f.lib, f.name)
		if err != nil || p == 0 {
			return nil
		}
		purego.RegisterFunc(f.fn, p)
	}
	var err error
	if a.poolPush, err = purego.Dlsym(objc, "objc_autoreleasePoolPush"); err != nil {
		return nil
	}
	if a.poolPop, err = purego.Dlsym(objc, "objc_autoreleasePoolPop"); err != nil {
		return nil
	}
	for _, c := range []struct {
		v    *uintptr
		name string
	}{
		{&a.keyType, "kCFProxyTypeKey"},
		{&a.keyHost, "kCFProxyHostNameKey"},
		{&a.keyPort, "kCFProxyPortNumberKey"},
		{&a.keyPACURL, "kCFProxyAutoConfigurationURLKey"},
		{&a.keyPACScript, "kCFProxyAutoConfigurationJavaScriptKey"},
		{&a.typeNone, "kCFProxyTypeNone"},
		{&a.typeHTTP, "kCFProxyTypeHTTP"},
		{&a.typeHTTPS, "kCFProxyTypeHTTPS"},
		{&a.typeSOCKS, "kCFProxyTypeSOCKS"},
		{&a.typePACURL, "kCFProxyTypeAutoConfigurationURL"},
		{&a.typePACScript, "kCFProxyTypeAutoConfigurationJavaScript"},
	} {
		p, err := purego.Dlsym(network, c.name)
		if err != nil || p == 0 {
			return nil
		}
		*c.v = **(**uintptr)(unsafe.Pointer(&p)) // a CFStringRef constant
	}
	if a.mode = a.stringCreate(0, "github.com/egoist/mygo/netproxy", cfStringEncodingUTF8); a.mode == 0 {
		return nil
	}
	a.callback = purego.NewCallback(pacDone)
	return a
}

func system(target *url.URL) *url.URL {
	a := cf()
	if a == nil {
		return nil
	}
	// The autorelease pool and the run loop belong to this thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool, _, _ := purego.SyscallN(a.poolPush)
	defer purego.SyscallN(a.poolPop, pool)

	s := a.stringCreate(0, target.String(), cfStringEncodingUTF8)
	if s == 0 {
		return nil
	}
	u := a.urlCreate(0, s, 0)
	a.release(s)
	if u == 0 {
		return nil
	}
	defer a.release(u)
	settings := a.copySettings()
	if settings == 0 {
		return nil
	}
	defer a.release(settings)
	list := a.copyProxies(u, settings)
	if list == 0 {
		return nil
	}
	defer a.release(list)
	return firstSupported(a.proxies(list, u))
}

// proxies converts CFNetwork's list of proxies, an array of dictionaries,
// to URLs, running the setup scripts it names for target; with target 0,
// as for the list a script returns, it skips them.
func (a *cfAPI) proxies(list, target uintptr) []string {
	var out []string
	for i := range a.arrayCount(list) {
		d := a.arrayAt(list, i)
		if d == 0 || a.typeID(d) != a.dictTypeID() {
			continue
		}
		typ := a.dictGet(d, a.keyType)
		switch {
		case typ == 0:
		case a.equal(typ, a.typeNone):
			out = append(out, "direct://")
		case a.equal(typ, a.typeHTTP), a.equal(typ, a.typeHTTPS):
			// An HTTPS proxy is the proxy of https URLs, spoken to in HTTP.
			out = append(out, a.server("http", d))
		case a.equal(typ, a.typeSOCKS):
			out = append(out, a.server("socks5", d))
		case target != 0 && a.equal(typ, a.typePACURL):
			out = append(out, a.runPAC(a.dictGet(d, a.keyPACURL), target, false)...)
		case target != 0 && a.equal(typ, a.typePACScript):
			out = append(out, a.runPAC(a.dictGet(d, a.keyPACScript), target, true)...)
		}
	}
	return out
}

// server returns the proxy of dictionary d as scheme://host:port, or ""
// without a host.
func (a *cfAPI) server(scheme string, d uintptr) string {
	host := a.goString(a.dictGet(d, a.keyHost))
	if host == "" {
		return ""
	}
	var port int32
	if n := a.dictGet(d, a.keyPort); n != 0 && a.typeID(n) == a.numberTypeID() {
		a.numberGet(n, cfNumberSInt32Type, &port)
	}
	if port > 0 {
		return scheme + "://" + net.JoinHostPort(host, strconv.Itoa(int(port)))
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return scheme + "://" + host
}

func (a *cfAPI) goString(s uintptr) string {
	if s == 0 || a.typeID(s) != a.stringTypeID() {
		return ""
	}
	n := a.stringMaxSize(a.stringLength(s), cfStringEncodingUTF8) + 1
	buf := make([]byte, n)
	if !a.stringGet(s, &buf[0], n, cfStringEncodingUTF8) {
		return ""
	}
	for i, c := range buf {
		if c == 0 {
			return string(buf[:i])
		}
	}
	return string(buf)
}

// Setup scripts' results, by the info of their callback's context.
var pacs struct {
	sync.Mutex
	next    uintptr
	results map[uintptr]*pacResult
}

type pacResult struct {
	done    bool
	proxies []string
}

// runPAC runs the setup script at the URL script, or the script itself,
// for target and returns the proxies it chose. A script that fails or
// takes longer than pacTimeout chooses none.
func (a *cfAPI) runPAC(script, target uintptr, isScript bool) []string {
	if script == 0 {
		return nil
	}
	res := &pacResult{}
	pacs.Lock()
	if pacs.results == nil {
		pacs.results = map[uintptr]*pacResult{}
	}
	pacs.next++
	id := pacs.next
	pacs.results[id] = res
	pacs.Unlock()
	defer func() {
		pacs.Lock()
		delete(pacs.results, id)
		pacs.Unlock()
	}()

	ctx := &streamClientContext{info: id}
	var source uintptr // owned, though its name says neither Create nor Copy
	if isScript {
		source = a.runPACScript(script, target, a.callback, ctx)
	} else {
		source = a.runPACURL(script, target, a.callback, ctx)
	}
	if source == 0 {
		return nil
	}
	rl := a.runLoopCurrent()
	a.addSource(rl, source, a.mode)
	deadline := time.Now().Add(pacTimeout)
	for {
		pacs.Lock()
		done := res.done
		pacs.Unlock()
		left := time.Until(deadline)
		if done || left <= 0 {
			break
		}
		if a.runInMode(a.mode, left.Seconds(), false) == cfRunLoopRunFinished {
			break
		}
	}
	// Cancels a script that is still running.
	a.invalidate(source)
	a.removeSource(rl, source, a.mode)
	a.release(source)
	runtime.KeepAlive(ctx)
	pacs.Lock()
	defer pacs.Unlock()
	return res.proxies
}

// pacDone is the callback of setup scripts, on the run loop of the thread
// that runs them. It owns neither the list nor the error.
func pacDone(info, list, cfErr uintptr) {
	a := cf()
	var proxies []string
	if cfErr == 0 && list != 0 {
		proxies = a.proxies(list, 0)
	}
	pacs.Lock()
	if res := pacs.results[info]; res != nil {
		res.done, res.proxies = true, proxies
	}
	pacs.Unlock()
	a.stop(a.runLoopCurrent())
}
