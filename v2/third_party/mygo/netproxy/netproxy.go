// Package netproxy sends Go's HTTP requests through the proxy the user set
// in the system's settings, as browsers and the app's web pages do, with
// its bypass list and automatic configuration (PAC files, WPAD):
//
//	client := &http.Client{Transport: netproxy.Transport()}
//
// MyGo's own requests go this way: the updater's, and those of the fetch
// and websocket plugins unless their options name a client.
//
// Proxy decides, for each request:
//
//  1. Loopback hosts (localhost, *.localhost, 127.0.0.0/8, ::1) are
//     reached directly.
//  2. When any of HTTP_PROXY, HTTPS_PROXY, ALL_PROXY or NO_PROXY (or its
//     lowercase name) is set, the environment decides, as
//     http.ProxyFromEnvironment does: an explicit setting wins over the
//     system's.
//  3. Otherwise the system's settings for the URL do: WinHTTP's reading
//     of the user's settings on Windows, with automatic detection and PAC
//     files; CFNetwork's on macOS, with PAC files; GIO's default proxy
//     resolver on Linux, which reads GNOME's settings, or KDE's and PAC
//     files through libproxy. Where none answers, requests go direct.
//
// The system's answer for a scheme and host is kept for 5 seconds, so a
// change of the settings applies to the requests that follow without
// restarting the app. Requests under way keep their connections, and Go's
// transport keys idle connections by their proxy, so none opened through
// the previous proxy is reused for the new one.
//
// Go's transport speaks to HTTP and HTTPS proxies and to SOCKS5: a SOCKS
// proxy of unknown version is taken for SOCKS5, and SOCKS4 proxies are
// skipped. A proxy's password is sent only when the system's answer
// holds it, as GNOME's settings can. In browsers (js/wasm) the browser
// makes the requests, with its own proxy.
package netproxy

import (
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// ttl is how long the system's answer for a scheme and host is kept.
const ttl = 5 * time.Second

// envNames are the variables that hand the decision to the environment.
var envNames = [...]string{
	"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy",
	"ALL_PROXY", "all_proxy", "NO_PROXY", "no_proxy",
}

// Replaced by tests.
var (
	envProxy    = http.ProxyFromEnvironment
	systemProxy = system
	now         = time.Now
)

// Proxy returns the proxy for req, or nil to connect directly. It is an
// http.Transport's Proxy, safe from any goroutine; it may block while the
// system runs a PAC file.
func Proxy(req *http.Request) (*url.URL, error) {
	if isLoopback(req.URL.Hostname()) {
		return nil, nil
	}
	for _, name := range envNames {
		if os.Getenv(name) != "" {
			return envProxy(req)
		}
	}
	return cached(req.URL.Scheme, req.URL.Host), nil
}

var transport lazy[*http.Transport]

// Transport returns a transport like http.DefaultTransport whose Proxy is
// Proxy. It is shared: clone it to change its settings.
func Transport() *http.Transport {
	return transport.get(func() *http.Transport {
		t := http.DefaultTransport.(*http.Transport).Clone()
		t.Proxy = Proxy
		return t
	})
}

// lazy is a value made on first use. Unlike sync.OnceValue's, a package
// variable of it runs nothing at init, so that apps that never make a
// request through Proxy link none of it.
type lazy[T any] struct {
	once sync.Once
	v    T
}

func (l *lazy[T]) get(f func() T) T {
	l.once.Do(func() { l.v = f() })
	return l.v
}

var cache struct {
	sync.Mutex
	m map[string]cacheEntry
}

type cacheEntry struct {
	proxy *url.URL
	until time.Time
}

// cached returns the system's proxy for scheme and host (with its port),
// asking the system when the last answer is older than ttl.
func cached(scheme, host string) *url.URL {
	key := scheme + "://" + host
	t := now()
	cache.Lock()
	e, ok := cache.m[key]
	cache.Unlock()
	if !ok || !t.Before(e.until) {
		e = cacheEntry{systemProxy(&url.URL{Scheme: scheme, Host: host, Path: "/"}), t.Add(ttl)}
		cache.Lock()
		if cache.m == nil || len(cache.m) >= 256 {
			cache.m = map[string]cacheEntry{}
		}
		cache.m[key] = e
		cache.Unlock()
	}
	if e.proxy == nil {
		return nil
	}
	p := *e.proxy
	return &p
}

func isLoopback(host string) bool {
	host = strings.ToLower(host)
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
