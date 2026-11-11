package mygo

import "github.com/egoist/mygo/internal/platform"

// AuthRequest is passed to the auth handler of a window (SetAuthHandler):
// a server, or a proxy, asks for a user name and a password before it
// serves the page, or something the page loads.
type AuthRequest struct {
	// Host and Port of the server asking, or of the proxy, e.g.
	// "intranet.example.com" and 443.
	Host string
	Port int
	// Realm is the protection space the server named, "" for none: one
	// server can ask for the credentials of several.
	Realm string
	// Scheme is the authentication scheme, lowercase: "basic", "digest",
	// "ntlm" or "negotiate".
	Scheme string
	// IsProxy is true when a proxy asks rather than the server. WebView2
	// does not tell: it is false on Windows, where Host is then the
	// proxy's.
	IsProxy bool
}

// AuthResponse answers an AuthRequest with the user name and the password
// to sign in with. One with neither cancels the sign-in, as a user
// dismissing a browser's prompt does: the page gets the server's refusal,
// its 401 (or 407) page.
type AuthResponse struct {
	Username string
	Password string
}

// SetAuthHandler answers the HTTP authentication challenges (Basic,
// Digest, NTLM, Negotiate) of the window's pages and of what they load,
// the server's or a proxy's: fn returns the credentials to sign in with,
// or an empty AuthResponse to cancel, and runs on the main thread while
// the load waits. A server that refuses the credentials asks again; the
// web view keeps those it accepts for the rest of the session, for that
// server and realm, as browsers do, so fn is not asked for every request.
// MyGo never logs them.
//
//	page.SetAuthHandler(func(req mygo.AuthRequest) mygo.AuthResponse {
//		if req.Host == "router.lan" && !req.IsProxy {
//			return mygo.AuthResponse{Username: "admin", Password: password}
//		}
//		return mygo.AuthResponse{}
//	})
//
// To ask the user, fn can cancel and show the app's own prompt, then
// Reload the page once it has the credentials to answer with.
//
// nil restores the default, the platform's:
//
//   - macOS: no prompt; the page gets the server's refusal. MyGo answers
//     WKNavigationDelegate's
//     webView:didReceiveAuthenticationChallenge:completionHandler:.
//   - Linux: WebKitGTK's sign-in dialog. MyGo answers
//     WebKitWebView::authenticate.
//   - Windows: WebView2's sign-in dialog. MyGo answers ICoreWebView2_10's
//     BasicAuthenticationRequested; on a runtime without it, fn is never
//     asked.
func (p *Page) SetAuthHandler(fn func(req AuthRequest) AuthResponse) {
	p.w.mu.Lock()
	p.w.authHandler = fn
	p.w.mu.Unlock()
}

func (h *windowHandler) AuthRequested(req platform.AuthRequest) platform.AuthResponse {
	h.w.mu.Lock()
	fn := h.w.authHandler
	h.w.mu.Unlock()
	if fn == nil {
		return platform.AuthResponse{Default: true}
	}
	r := fn(AuthRequest{Host: req.Host, Port: req.Port, Realm: req.Realm, Scheme: req.Scheme, IsProxy: req.IsProxy})
	return platform.AuthResponse{Username: r.Username, Password: r.Password}
}

// CertificateError is passed to the certificate error handler of a
// window (SetCertificateErrorHandler): a server's certificate that does
// not verify.
type CertificateError struct {
	// URL is what was loading: the page, or something it loads. On macOS
	// it is the server's origin, such as "https://dev.example:8443", as
	// WebKit asks about the connection rather than a request.
	URL string
	// Host is the server's host in URL, a name or an IP address.
	Host string
	// Error tells what is wrong with the certificate, in the platform's
	// words, for logs and messages.
	Error string
	// Certificate is the server's certificate, DER-encoded, which
	// x509.ParseCertificate reads. Compare it, or its hash, with the one
	// the app expects.
	Certificate []byte
}

// SetCertificateErrorHandler decides about the server certificates that
// do not verify, for the window's pages and what they load: fn returns
// true to load them anyway, as for a dev server with a self-signed
// certificate or an internal host whose certificate the app ships. It
// runs on the main thread while the load waits. By default, and whenever
// fn returns false, the load fails as in a browser (OnDidFailLoad). nil
// restores the default.
//
// Allow only the certificate expected, never a host whatever its
// certificate, which anyone between the app and the server can present:
//
//	page.SetCertificateErrorHandler(func(e mygo.CertificateError) bool {
//		sum := sha256.Sum256(e.Certificate)
//		return e.Host == "dev.example" && sum == devCertificateSHA256
//	})
//
// What is asked, and how long a yes lasts, depends on the platform:
//
//   - macOS: each connection to the server, of the page and of what it
//     loads; a yes lasts for that connection. MyGo checks the
//     certificate itself (SecTrustEvaluateAsyncWithError), only while a
//     handler is set, from WKNavigationDelegate's
//     webView:didReceiveAuthenticationChallenge:completionHandler:.
//   - Linux: the page only, as WebKitGTK tells no more
//     (WebKitWebView::load-failed-with-tls-errors): what it loads from
//     such a server fails without asking. A yes allows that certificate
//     for its host in every window until the app quits
//     (webkit_web_context_allow_tls_certificate_for_host), and the page
//     loads again.
//   - Windows: the page and what it loads (ICoreWebView2_14's
//     ServerCertificateErrorDetected; on a runtime without it, fn is
//     never asked).
//     WebView2 reports the page's load failed (OnDidFailLoad) before it
//     asks, and a yes allows that certificate for its host in every
//     window until the app quits.
func (p *Page) SetCertificateErrorHandler(fn func(e CertificateError) bool) {
	p.w.mu.Lock()
	p.w.certificateHandler = fn
	p.w.mu.Unlock()
}

func (h *windowHandler) CertificateError(e platform.CertificateError) bool {
	h.w.mu.Lock()
	fn := h.w.certificateHandler
	h.w.mu.Unlock()
	return fn != nil && fn(CertificateError{URL: e.URL, Host: e.Host, Error: e.Error, Certificate: e.Certificate})
}

func (h *windowHandler) HandlesCertificateErrors() bool {
	h.w.mu.Lock()
	defer h.w.mu.Unlock()
	return h.w.certificateHandler != nil
}
