// Package schemehost maps URLs of the app's custom schemes to the
// http://*.localhost URLs that serve them where the webview can't load a
// custom scheme itself (WebView2), and back.
//
// <scheme>://localhost/… is served at http://<scheme>.localhost/…, and
// <scheme>://<host>/… at http://<host>.<scheme>.localhost/…. Chromium
// treats every *.localhost host as a secure origin on the loopback
// interface, so each host stays its own origin, as with the custom schemes
// of the other webviews.
//
// Hosts are names of DNS labels (letters, digits and hyphens, at most 63
// each, not starting or ending with a hyphen) separated by dots, as
// Chromium needs them in an http URL. They are lower-cased: hosts are
// case-insensitive, and the webview reports them in lower case. URLs with
// another host, a port, user info or no host aren't mapped, so the webview
// loads them as they are.
package schemehost

import (
	"net/url"
	"strings"
)

const suffix = ".localhost"

// WebURL maps raw, a URL of a scheme that has reports as the app's, to
// the http URL that serves it. Other URLs are returned as they are.
func WebURL(raw string, has func(scheme string) bool) string {
	u, err := url.Parse(raw)
	if err != nil || u.Opaque != "" || u.User != nil || !has(u.Scheme) {
		return raw
	}
	host := strings.ToLower(u.Host)
	if host != "localhost" && !validHost(host) {
		return raw // a port, no host, or a host that isn't a DNS name
	}
	web := u.Scheme + suffix
	if host != "localhost" {
		web = host + "." + web
	}
	// The mapping must come back: with schemes a and b.a, b://x/ and
	// a://x.b/ would both be http://x.b.a.localhost/.
	if s, h, ok := AppHost(web, has); !ok || s != u.Scheme || h != host {
		return raw
	}
	u.Scheme, u.Host = "http", web
	return u.String()
}

// AppURL maps raw, an http URL that serves a scheme that has reports as
// the app's, back to the URL of that scheme. Other URLs are returned as
// they are.
func AppURL(raw string, has func(scheme string) bool) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Port() != "" {
		return raw
	}
	scheme, host, ok := AppHost(u.Hostname(), has)
	if !ok {
		return raw
	}
	u.Scheme, u.Host = scheme, host
	return u.String()
}

// AppOrigin maps origin, the origin of a page that an http URL serving a
// scheme of the app loaded, back to the origin of that scheme, such as
// http://abc.myapp.localhost to myapp://abc. Other origins are returned
// as they are.
func AppOrigin(origin string, has func(scheme string) bool) string {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Port() != "" || (u.Path != "" && u.Path != "/") {
		return origin
	}
	scheme, host, ok := AppHost(u.Hostname(), has)
	if !ok {
		return origin
	}
	return scheme + "://" + host
}

// AppHost splits webHost, the host of an http URL, into the scheme and
// host of the app's URL it serves: <scheme>.localhost is <scheme>://localhost,
// and <host>.<scheme>.localhost is <scheme>://<host>. The longest scheme
// that has reports wins.
func AppHost(webHost string, has func(scheme string) bool) (scheme, host string, ok bool) {
	rest, found := strings.CutSuffix(strings.ToLower(webHost), suffix)
	if !found || rest == "" {
		return "", "", false
	}
	for i := 0; ; {
		scheme = rest[i:]
		if has(scheme) {
			if i == 0 {
				return scheme, "localhost", true
			}
			host = rest[:i-1]
			return scheme, host, validHost(host)
		}
		j := strings.IndexByte(scheme, '.')
		if j < 0 {
			return "", "", false
		}
		i += j + 1
	}
}

// validHost reports whether host is lower-case DNS labels separated by
// dots.
func validHost(host string) bool {
	if host == "" || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			if !('a' <= c && c <= 'z' || '0' <= c && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}
