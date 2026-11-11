//go:build windows && (amd64 || arm64)

package windows

import (
	"encoding/base64"
	"net/url"
	"strconv"
	"strings"
	"unsafe"

	"github.com/egoist/mygo/internal/platform"
)

// HTTP authentication and server certificates of pages: ICoreWebView2_10's
// BasicAuthenticationRequested and ICoreWebView2_14's
// ServerCertificateErrorDetected. Vtable indices from WebView2.h, as in
// webview2.go: ICoreWebView2_9 ends with put_DefaultDownloadDialogMargin
// (96), ICoreWebView2_13 with get_Profile (105).
const (
	wv10AddBasicAuthenticationRequested   = 97
	wv14AddServerCertificateErrorDetected = 106

	// ICoreWebView2BasicAuthenticationRequestedEventArgs and its response.
	authGetURI          = 3
	authGetChallenge    = 4
	authGetResponse     = 5
	authPutCancel       = 7
	authRespPutUserName = 4
	authRespPutPassword = 6

	// ICoreWebView2ServerCertificateErrorDetectedEventArgs, and
	// ICoreWebView2Certificate's ToPemEncoding.
	certErrGetErrorStatus       = 3
	certErrGetRequestURI        = 4
	certErrGetServerCertificate = 5
	certErrPutAction            = 7
	certToPemEncoding           = 9
)

var (
	iidICoreWebView2_10 = guid("b1690564-6f5a-4983-8e48-31d1143fecdb")
	iidICoreWebView2_14 = guid("6daa4f10-4a90-4753-8898-77c5df534165")
)

// certError words COREWEBVIEW2_WEB_ERROR_STATUS's certificate errors.
func certError(status int32) string {
	switch status {
	case 1:
		return "the certificate does not match the host"
	case 2:
		return "the certificate expired"
	case 3:
		return "the client certificate contains errors"
	case 4:
		return "the certificate was revoked"
	case 5:
		return "the certificate is invalid"
	}
	return "the certificate does not verify"
}

// pemCertificate decodes the certificate that ToPemEncoding wrote, as
// encoding/pem would, which apps that never ask need not link.
func pemCertificate(s string) []byte {
	_, s, ok := strings.Cut(s, "-----BEGIN CERTIFICATE-----")
	if s, _, ok2 := strings.Cut(s, "-----END"); ok && ok2 {
		s = strings.Map(func(r rune) rune {
			if r == '\r' || r == '\n' || r == ' ' || r == '\t' {
				return -1
			}
			return r
		}, s)
		b := make([]byte, base64.StdEncoding.DecodedLen(len(s)))
		if n, err := base64.StdEncoding.Decode(b, []byte(s)); err == nil {
			return b[:n]
		}
	}
	return nil
}

// addAuthHandlers handles the web view's sign-ins and certificate errors,
// on runtimes that have them.
func (w *window) addAuthHandlers() {
	var tok int64
	if wv10 := queryInterface(w.webview, &iidICoreWebView2_10); wv10 != 0 {
		withHandler(w.basicAuthenticationRequested, func(h uintptr) uintptr {
			return comCall(wv10, wv10AddBasicAuthenticationRequested, h, uintptr(unsafe.Pointer(&tok)))
		})
		release(wv10)
	}
	if wv14 := queryInterface(w.webview, &iidICoreWebView2_14); wv14 != 0 {
		withHandler(w.serverCertificateErrorDetected, func(h uintptr) uintptr {
			return comCall(wv14, wv14AddServerCertificateErrorDetected, h, uintptr(unsafe.Pointer(&tok)))
		})
		release(wv14)
	}
}

// basicAuthenticationRequested answers a sign-in. Doing nothing leaves it
// to WebView2: its sign-in dialog.
func (w *window) basicAuthenticationRequested(_, args uintptr) {
	var p uintptr
	comCall(args, authGetURI, uintptr(unsafe.Pointer(&p)))
	u, err := url.Parse(takeWstr(p))
	if err != nil {
		return
	}
	comCall(args, authGetChallenge, uintptr(unsafe.Pointer(&p)))
	req := platform.AuthRequest{Host: u.Hostname()}
	req.Scheme, req.Realm = platform.ParseAuthChallenge(takeWstr(p))
	if req.Port, err = strconv.Atoi(u.Port()); err != nil {
		req.Port = 80
		if u.Scheme == "https" {
			req.Port = 443
		}
	}
	resp := w.h.AuthRequested(req)
	switch {
	case resp.Default:
	case resp.Username == "" && resp.Password == "":
		comCall(args, authPutCancel, 1) // the server's refusal
	default:
		var r uintptr
		if failed(comCall(args, authGetResponse, uintptr(unsafe.Pointer(&r)))) || r == 0 {
			return
		}
		comCall(r, authRespPutUserName, uintptr(unsafe.Pointer(u16(resp.Username))))
		comCall(r, authRespPutPassword, uintptr(unsafe.Pointer(u16(resp.Password))))
		release(r)
	}
}

// serverCertificateErrorDetected asks the app about a certificate that
// does not verify. Leaving the action as it is (DEFAULT) fails the
// request, with WebView2's error page for a page.
func (w *window) serverCertificateErrorDetected(_, args uintptr) {
	if !w.h.HandlesCertificateErrors() {
		return
	}
	var status int32
	comCall(args, certErrGetErrorStatus, uintptr(unsafe.Pointer(&status)))
	var p uintptr
	comCall(args, certErrGetRequestURI, uintptr(unsafe.Pointer(&p)))
	raw := takeWstr(p)
	u, err := url.Parse(raw)
	if err != nil {
		return
	}
	e := platform.CertificateError{URL: raw, Host: u.Hostname(), Error: certError(status)}
	var cert uintptr
	if !failed(comCall(args, certErrGetServerCertificate, uintptr(unsafe.Pointer(&cert)))) && cert != 0 {
		if !failed(comCall(cert, certToPemEncoding, uintptr(unsafe.Pointer(&p)))) {
			e.Certificate = pemCertificate(takeWstr(p))
		}
		release(cert)
	}
	if w.h.CertificateError(e) {
		const alwaysAllow = 0 // COREWEBVIEW2_SERVER_CERTIFICATE_ERROR_ACTION_ALWAYS_ALLOW
		comCall(args, certErrPutAction, alwaysAllow)
	}
}
