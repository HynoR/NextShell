//go:build linux && (amd64 || arm64)

package linux

import (
	"net/url"
	"unsafe"

	"github.com/ebitengine/purego"

	"github.com/egoist/mygo/internal/platform"
)

// HTTP authentication and server certificates of pages: the web view's
// authenticate and load-failed-with-tls-errors signals.

var (
	webkitAuthenticationRequestGetHost      func(req ptr) ptr
	webkitAuthenticationRequestGetPort      func(req ptr) uint32
	webkitAuthenticationRequestGetRealm     func(req ptr) ptr
	webkitAuthenticationRequestGetScheme    func(req ptr) int32
	webkitAuthenticationRequestIsForProxy   func(req ptr) bool
	webkitAuthenticationRequestAuthenticate func(req, credential ptr)
	webkitCredentialNew                     func(user, password *byte, persistence int32) ptr
	webkitCredentialFree                    func(credential ptr)
	webkitWebViewGetContext                 func(wv ptr) ptr
	webkitWebContextAllowTLSCertificate     func(ctx, cert ptr, host *byte)
	gByteArrayUnref                         func(a ptr)

	cbAuthenticate, cbTLSErrors, cbTLSRetry ptr
)

func bindAuth() {
	w := libWebKit
	mustBind(w, &webkitAuthenticationRequestGetHost, "webkit_authentication_request_get_host")
	mustBind(w, &webkitAuthenticationRequestGetPort, "webkit_authentication_request_get_port")
	mustBind(w, &webkitAuthenticationRequestGetRealm, "webkit_authentication_request_get_realm")
	mustBind(w, &webkitAuthenticationRequestGetScheme, "webkit_authentication_request_get_scheme")
	mustBind(w, &webkitAuthenticationRequestIsForProxy, "webkit_authentication_request_is_for_proxy")
	mustBind(w, &webkitAuthenticationRequestAuthenticate, "webkit_authentication_request_authenticate")
	mustBind(w, &webkitCredentialNew, "webkit_credential_new")
	mustBind(w, &webkitCredentialFree, "webkit_credential_free")
	mustBind(w, &webkitWebViewGetContext, "webkit_web_view_get_context")
	mustBind(w, &webkitWebContextAllowTLSCertificate, "webkit_web_context_allow_tls_certificate_for_host")
	mustBind(libGLib, &gByteArrayUnref, "g_byte_array_unref")
}

// authScheme names WebKitAuthenticationScheme's HTTP schemes, "" the
// others, client certificates among them, which get WebKit's default
// handling.
func authScheme(scheme int32) string {
	switch scheme {
	case 1, 2: // DEFAULT, HTTP_BASIC
		return "basic"
	case 3:
		return "digest"
	case 5:
		return "ntlm"
	case 6:
		return "negotiate"
	}
	return ""
}

// tlsErrors words the GTlsCertificateFlags a certificate failed with.
var tlsErrors = []string{
	"the certificate's issuer is not known",
	"the certificate does not match the host",
	"the certificate is not valid yet",
	"the certificate expired",
	"the certificate was revoked",
	"the certificate's algorithm is insecure",
	"the certificate is invalid",
}

func initAuthCallbacks() {
	b := func() *Backend { return theBackend }
	// Returning false leaves the request to WebKit: its sign-in dialog.
	cbAuthenticate = purego.NewCallback(func(web, req, data ptr) bool {
		w := b().window(data)
		scheme := authScheme(webkitAuthenticationRequestGetScheme(req))
		if w == nil || scheme == "" {
			return false
		}
		resp := w.h.AuthRequested(platform.AuthRequest{
			Host:    goStr(webkitAuthenticationRequestGetHost(req)),
			Port:    int(webkitAuthenticationRequestGetPort(req)),
			Realm:   goStr(webkitAuthenticationRequestGetRealm(req)),
			Scheme:  scheme,
			IsProxy: webkitAuthenticationRequestIsForProxy(req),
		})
		switch {
		case resp.Default:
			return false
		case resp.Username == "" && resp.Password == "":
			// Without credentials: the server's refusal.
			webkitAuthenticationRequestAuthenticate(req, 0)
		default:
			const forSession = 1 // WEBKIT_CREDENTIAL_PERSISTENCE_FOR_SESSION
			c := webkitCredentialNew(cs(resp.Username), cs(resp.Password), forSession)
			webkitAuthenticationRequestAuthenticate(req, c)
			webkitCredentialFree(c)
		}
		return true
	})
	// Returning false fails the load (load-failed), as without a handler.
	cbTLSErrors = purego.NewCallback(func(web, uri, cert ptr, flags uint32, data ptr) bool {
		w := b().window(data)
		if w == nil || cert == 0 || !w.h.HandlesCertificateErrors() {
			return false
		}
		failing := goStr(uri)
		u, err := url.Parse(failing)
		if err != nil || u.Hostname() == "" {
			return false
		}
		e := platform.CertificateError{URL: failing, Host: u.Hostname(), Error: "the certificate does not verify"}
		for i, text := range tlsErrors {
			if flags&(1<<i) != 0 {
				e.Error = text
				break
			}
		}
		// GTlsCertificate's certificate property, a GByteArray: guint8
		// *data, guint len.
		var der ptr
		gObjectGetPtr(cert, cs("certificate"), unsafe.Pointer(&der), 0)
		if der != 0 {
			p := field[unsafe.Pointer](der, 0)
			e.Certificate = append([]byte(nil), unsafe.Slice((*byte)(p), field[uint32](der, 8))...)
			gByteArrayUnref(der)
		}
		if !w.h.CertificateError(e) {
			return false
		}
		webkitWebContextAllowTLSCertificate(webkitWebViewGetContext(web), cert, cs(e.Host))
		// WebKit finishes the failed load after the signal: load the page
		// again after that.
		w.tlsRetry = failing
		gIdleAddFull(0, cbTLSRetry, data, 0)
		return true
	})
	cbTLSRetry = purego.NewCallback(func(data ptr) int32 {
		if w := b().window(data); w != nil && w.tlsRetry != "" {
			uri := w.tlsRetry
			w.tlsRetry = ""
			w.LoadURL(uri)
		}
		return 0 // G_SOURCE_REMOVE
	})
}

// connectAuth connects the signals of the web view's authentication.
func (w *window) connectAuth(data ptr) {
	connect(w.web, "authenticate", cbAuthenticate, data)
	connect(w.web, "load-failed-with-tls-errors", cbTLSErrors, data)
}
