package netproxy

import (
	"net"
	"net/netip"
	"net/url"
	"strings"
)

// firstSupported returns the first proxy of list, as "http://host:port",
// "socks5://host:port" or "direct://", that Go's transport can use, or
// nil for a direct connection: "direct://" comes first, or no proxy
// can be used.
func firstSupported(list []string) *url.URL {
	for _, s := range list {
		if p, direct := proxyURL(s); direct {
			return nil
		} else if p != nil {
			return p
		}
	}
	return nil
}

// proxyURL parses one proxy of a list. It returns nil for a proxy Go's
// transport cannot use, and direct for "direct://".
func proxyURL(s string) (p *url.URL, direct bool) {
	s = strings.TrimSpace(s)
	if strings.EqualFold(s, "direct://") || strings.EqualFold(s, "direct") {
		return nil, true
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" || u.Hostname() == "" {
		return nil, false
	}
	switch u.Scheme = strings.ToLower(u.Scheme); u.Scheme {
	case "http", "https", "socks5", "socks5h":
	case "socks":
		// A SOCKS proxy of no stated version.
		u.Scheme = "socks5"
	default: // socks4, socks4a, ftp...
		return nil, false
	}
	return &url.URL{Scheme: u.Scheme, User: u.User, Host: u.Host}, false
}

// winProxy returns the proxy of a Windows proxy list for URLs of scheme:
// "host:port" for every scheme, or per scheme as
// "http=host:port;https=host:port;socks=host:port", where a scheme
// without its own proxy uses the SOCKS one. Entries are separated by
// semicolons or spaces; the first one that applies wins.
func winProxy(list, scheme string) *url.URL {
	var all, socks *url.URL
	for _, e := range splitList(list) {
		k, v, ok := strings.Cut(e, "=")
		if !ok {
			if all == nil {
				all = winEntry(e, "http")
			}
			continue
		}
		switch k = strings.ToLower(strings.TrimSpace(k)); {
		case k == scheme:
			if p := winEntry(v, "http"); p != nil {
				return p
			}
		case k == "socks" && socks == nil:
			socks = winEntry(v, "socks5")
		}
	}
	if all != nil {
		return all
	}
	return socks
}

// winStatic returns the proxy of a proxy list and bypass list for target.
func winStatic(proxy, bypass string, target *url.URL) *url.URL {
	if proxy == "" || winBypassed(bypass, target.Hostname()) {
		return nil
	}
	return winProxy(proxy, target.Scheme)
}

// winEntry parses the server of a Windows proxy list, "host:port" or
// "scheme://host:port", with scheme when it names none.
func winEntry(s, scheme string) *url.URL {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if !strings.Contains(s, "://") {
		s = scheme + "://" + s
	}
	p, _ := proxyURL(s)
	return p
}

// winBypassed reports whether a Windows bypass list sends host direct:
// entries separated by semicolons or spaces, each a host name or an IP
// address with * wildcards ("*.example.com", "10.*"), a domain with a
// leading dot (".example.com"), an address prefix ("10.0.0.0/8"), or
// <local>, the host names without a dot.
func winBypassed(list, host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "" {
		return false
	}
	ip, ipErr := netip.ParseAddr(host)
	for _, e := range splitList(strings.ToLower(list)) {
		if _, rest, ok := strings.Cut(e, "://"); ok {
			e = rest
		}
		switch {
		case e == "<local>":
			if ipErr != nil && !strings.Contains(host, ".") {
				return true
			}
		case strings.HasPrefix(e, "<"): // <-loopback>; loopback hosts never reach here
		case strings.Contains(e, "/"):
			if p, err := netip.ParsePrefix(e); err == nil && ipErr == nil && p.Contains(ip.Unmap()) {
				return true
			}
		default:
			if h, _, err := net.SplitHostPort(e); err == nil {
				e = h // the port is not checked
			}
			e = strings.Trim(e, "[]")
			if strings.HasPrefix(e, ".") {
				e = "*" + e
			}
			if matchGlob(e, host) {
				return true
			}
		}
	}
	return false
}

// matchGlob reports whether s matches pattern, where * stands for any
// characters, dots included.
func matchGlob(pattern, s string) bool {
	before, after, ok := strings.Cut(pattern, "*")
	if !ok {
		return pattern == s
	}
	if !strings.HasPrefix(s, before) {
		return false
	}
	s = s[len(before):]
	for i := 0; i <= len(s); i++ {
		if matchGlob(after, s[i:]) {
			return true
		}
	}
	return false
}

func splitList(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return r == ';' || r == ' ' || r == '\t' || r == '\r' || r == '\n'
	})
}
