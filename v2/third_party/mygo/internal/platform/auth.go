package platform

import "strings"

// AuthRequest is an HTTP authentication challenge of a page, or of what
// it loads (WindowHandler.AuthRequested).
type AuthRequest struct {
	// Host and Port of the server asking, or of the proxy.
	Host string
	Port int
	// Realm is the protection space the server named, "" for none.
	Realm string
	// Scheme is the authentication scheme, lowercase: "basic", "digest",
	// "ntlm" or "negotiate".
	Scheme  string
	IsProxy bool
}

// AuthResponse answers an AuthRequest. Default leaves the challenge to
// the web view, as if nobody had been asked; otherwise the request goes on
// with Username and Password, or, when both are empty, without
// credentials: the page gets the server's refusal (401 or 407).
type AuthResponse struct {
	Default            bool
	Username, Password string
}

// CertificateError is a server certificate that does not verify
// (WindowHandler.CertificateError).
type CertificateError struct {
	// URL is what was loading; Host the server's host in it.
	URL, Host string
	// Error tells what is wrong, in the platform's words.
	Error string
	// Certificate is the server's certificate, DER-encoded.
	Certificate []byte
}

// ParseAuthChallenge returns the scheme, lowercase, and the realm of an
// HTTP authentication challenge, as a WWW-Authenticate header gives it:
// `Basic realm="intranet", charset="UTF-8"` is "basic" and "intranet".
func ParseAuthChallenge(challenge string) (scheme, realm string) {
	s := strings.TrimSpace(challenge)
	end := strings.IndexAny(s, " \t,")
	if end < 0 {
		return strings.ToLower(s), ""
	}
	scheme, s = strings.ToLower(s[:end]), s[end:]
	for s != "" {
		s = strings.TrimLeft(s, " \t,")
		eq := strings.IndexByte(s, '=')
		if eq < 0 {
			break
		}
		name := strings.TrimSpace(s[:eq])
		s = strings.TrimLeft(s[eq+1:], " \t")
		var value string
		if strings.HasPrefix(s, `"`) {
			// A quoted string, with backslash escapes.
			var b strings.Builder
			i := 1
			for ; i < len(s) && s[i] != '"'; i++ {
				if s[i] == '\\' && i+1 < len(s) {
					i++
				}
				b.WriteByte(s[i])
			}
			value, s = b.String(), s[min(i+1, len(s)):]
		} else {
			end := strings.IndexAny(s, ", \t")
			if end < 0 {
				end = len(s)
			}
			value, s = s[:end], s[end:]
		}
		if strings.EqualFold(name, "realm") {
			return scheme, value
		}
	}
	return scheme, ""
}
