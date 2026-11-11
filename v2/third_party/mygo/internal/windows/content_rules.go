//go:build windows && (amd64 || arm64)

package windows

import (
	"net/http"
	"strings"
	"unsafe"

	"github.com/egoist/mygo/internal/contentrules"
	"github.com/egoist/mygo/internal/platform"
)

// WebView2 has no content blockers, so a window applying content rules
// sees every request (a "*" filter of WebResourceRequested) and matches
// it in Go; css-display-none rules hide elements through a script added
// to every document, with the user scripts.

// contentRules is the code of content rules, the matcher's among it,
// which LinkContentRules links: the backend's methods are linked into
// every app, and only call it.
var contentRules struct {
	compile func(b *Backend, rules string, done func(any, error))
	set     func(w *window, lists []any)
}

// LinkContentRules links the code of content rules, when the app first
// compiles rules.
func LinkContentRules() {
	contentRules.compile, contentRules.set = compileContentRules, setContentRules
}

func (a appController) CompileContentRules(_, _, rules string, done func(any, error)) {
	if contentRules.compile == nil {
		done(nil, platform.ErrUnsupported)
		return
	}
	contentRules.compile(a.b, rules, done)
}

func (w *window) SetContentRules(lists []any) {
	if contentRules.set != nil {
		contentRules.set(w, lists)
	}
}

// compileContentRules parses the rules on a goroutine of its own: there
// is nothing to keep across launches, and a long list takes a while.
func compileContentRules(b *Backend, rules string, done func(any, error)) {
	go func() {
		list, err := contentrules.Parse(rules)
		b.post(func() {
			if err != nil {
				done(nil, err)
				return
			}
			done(list, nil)
		})
	}()
}

func setContentRules(w *window, lists []any) {
	if w.closed {
		return
	}
	var rules []*contentrules.List
	for _, l := range lists {
		if list, ok := l.(*contentrules.List); ok {
			rules = append(rules, list)
		}
	}
	w.contentFilter = nil
	if len(rules) > 0 {
		w.contentFilter = func(args, req uintptr, uri string, resource int32) bool {
			return w.filterContent(rules, args, req, uri, resource)
		}
	}
	w.hidingScript = contentrules.HidingScript(rules)
	if w.webview == 0 {
		return // setUp applies them
	}
	w.filterAllRequests()
	w.SetUserScripts(w.opts.UserScripts)
}

// filterAllRequests makes WebView2 ask about every request, once the
// window applies content rules.
func (w *window) filterAllRequests() {
	if w.contentFilter != nil && !w.filtersAll {
		w.filtersAll = true
		w.addFilter("*")
	}
}

// resourceTypes maps a COREWEBVIEW2_WEB_RESOURCE_CONTEXT to the
// resource-type of content rules.
func resourceTypes(resource int32) contentrules.Type {
	switch resource {
	case 1:
		return contentrules.Document
	case 2:
		return contentrules.StyleSheet
	case 3:
		return contentrules.Image
	case 4:
		return contentrules.Media
	case 5:
		return contentrules.Font
	case 6:
		return contentrules.Script
	case 7:
		return contentrules.Raw
	case 8:
		return contentrules.Raw | contentrules.Fetch
	case 11: // which WebView2 does not raise WebResourceRequested for, so far
		return contentrules.Raw | contentrules.WebSocket
	case 14:
		return contentrules.Ping
	}
	return contentrules.Raw | contentrules.Other
}

// filterContent applies the window's content rules to a request, and
// reports whether it answered it: a request they block fails, and one
// they make secure goes to https.
//
// WebView2 cannot fail a request from WebResourceRequested, and a fetch
// or an XMLHttpRequest takes any response, a 403 too, where WebKit's
// blocked load is a network error, which pages tell apart. A redirect to
// a data: URL is one, as Chromium follows none from a load of a page. A
// page itself gets an empty 403, as a navigation would follow it.
func (w *window) filterContent(rules []*contentrules.List, args, req uintptr, uri string, resource int32) bool {
	const document = 1 // COREWEBVIEW2_WEB_RESOURCE_CONTEXT_DOCUMENT
	target := w.appURL(uri)
	top := w.URL()
	if resource == document && target == w.navigating || top == "" {
		top = target // the page itself
	}
	r := contentrules.Match(rules, contentrules.Request{URL: target, TopURL: top, Type: resourceTypes(resource)})
	switch {
	case r.Block && resource == document:
		w.respond(args, 0, http.StatusForbidden, nil, nil)
		return true
	case r.Block:
		w.respond(args, 0, http.StatusTemporaryRedirect, http.Header{"Location": {"data:,"}}, nil)
		return true
	case r.MakeHTTPS && target == uri:
		secure := "https:" + strings.TrimPrefix(uri, "http:")
		if strings.HasPrefix(uri, "ws:") {
			secure = "wss:" + strings.TrimPrefix(uri, "ws:")
		}
		w.respond(args, 0, http.StatusTemporaryRedirect, http.Header{"Location": {secure}}, nil)
		return true
	case r.BlockCookies:
		var headers uintptr
		if comCall(req, reqGetHeaders, uintptr(unsafe.Pointer(&headers))) == sOK && headers != 0 {
			comCall(headers, headersRemoveHeader, uintptr(unsafe.Pointer(u16("Cookie"))))
			release(headers)
		}
	}
	return false
}
