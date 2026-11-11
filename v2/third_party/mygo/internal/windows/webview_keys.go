//go:build windows && (amd64 || arm64)

package windows

import (
	_ "embed"
	"encoding/json"
	"log"
	"unsafe"

	"github.com/egoist/mygo/internal/platform"
)

// The keys a web view's page leaves unhandled go to the native UI around
// it (platform.WebViewKey), as WebKit sends them on in the other backends.
// WebView2 tells of no such key: the keyboard goes straight to the browser,
// and AcceleratorKeyPressed sees keys before the page and never plain
// letters. So keys.js, built from packages/bridge, runs in an isolated
// world of the page's documents, the top frame's and those of its frames
// in its process (Page.addScriptToEvaluateOnNewDocument with a worldName):
// it sees the page's trusted keydown events, decides after the page's
// handlers ran, and tells Go of the keys the page left through a binding
// of that world alone (Runtime.addBinding with an executionContextName),
// which WebView2 raises as Runtime.bindingCalled. The page's own scripts
// reach neither, with the bridge or without it (untrusted pages), so they
// cannot make up a key.

//go:embed keys.js
var keysScript string

const (
	// keysWorld is the isolated world of keys.js, and keysBinding the
	// binding it calls.
	keysWorld   = "mygo-keys"
	keysBinding = "mygoUnhandledKey"

	wvGetDevToolsProtocolEventReceiver   = 42 // ICoreWebView2
	cdpReceiverAddEventReceived          = 3  // ICoreWebView2DevToolsProtocolEventReceiver
	cdpEventArgsGetParameterObjectAsJSON = 3  // ICoreWebView2DevToolsProtocolEventReceivedEventArgs
)

// forwardKeys is forwardUnhandledKeys once a surface made a web view.
var forwardKeys = func(*window) {}

// forwardUnhandledKeys has the page of a web view in a window of native UI
// tell the content the keys it leaves unhandled. Runtime.enable installs
// the binding in the worlds made after it, as each document comes.
func (w *window) forwardUnhandledKeys() {
	params, err := json.Marshal(map[string]string{"source": keysScript, "worldName": keysWorld})
	if err != nil {
		return
	}
	var receiver uintptr
	if hr := comCall(w.webview, wvGetDevToolsProtocolEventReceiver, uintptr(unsafe.Pointer(u16("Runtime.bindingCalled"))), uintptr(unsafe.Pointer(&receiver))); failed(hr) || receiver == 0 {
		log.Print(hresultError("subscribing to the keys pages leave", hr))
		return
	}
	var tok int64
	withHandler(w.bindingCalled, func(h uintptr) uintptr {
		return comCall(receiver, cdpReceiverAddEventReceived, h, uintptr(unsafe.Pointer(&tok)))
	})
	release(receiver) // the web view keeps the handler
	logFailure := func(_ string, err error) {
		if err != nil {
			log.Printf("mygo: web view keys: %v", err)
		}
	}
	w.devtools("Runtime.enable", "{}", logFailure)
	w.devtools("Page.enable", "{}", logFailure)
	w.devtools("Runtime.addBinding", `{"name":"`+keysBinding+`","executionContextName":"`+keysWorld+`"}`, logFailure)
	w.devtools("Page.addScriptToEvaluateOnNewDocument", string(params), logFailure)
}

// bindingCalled handles Runtime.bindingCalled: keys.js telling of a key
// the page left unhandled, which the content takes as if the web view's
// element had the focus.
func (w *window) bindingCalled(_, args uintptr) {
	var p uintptr
	if failed(comCall(args, cdpEventArgsGetParameterObjectAsJSON, uintptr(unsafe.Pointer(&p)))) {
		return
	}
	var call struct {
		Name    string `json:"name"`
		Payload string `json:"payload"`
	}
	if json.Unmarshal([]byte(takeWstr(p)), &call) != nil || call.Name != keysBinding {
		return
	}
	var key struct {
		K uint32 `json:"k"` // the virtual key, as Chromium's keyCode is on Windows
		M uint8  `json:"m"`
		R bool   `json:"r"`
	}
	if json.Unmarshal([]byte(call.Payload), &key) != nil {
		return
	}
	if ev, ok := unhandledKey(w, uintptr(key.K), platform.Modifiers(key.M), key.R); ok && w.host != nil && !w.host.closed && w.host.surface != nil {
		w.host.surface.send(ev)
	}
}

// unhandledKey returns the event of a key a page left unhandled, by its
// virtual key: none for Tab, a modifier key or a key the content does not
// know.
func unhandledKey(w *window, vk uintptr, mods platform.Modifiers, repeat bool) (platform.SurfaceEvent, bool) {
	k := vkKey(vk)
	if k == platform.KeyUnknown || k == platform.KeyTab || modifierKey(vk) {
		return platform.SurfaceEvent{}, false
	}
	return platform.SurfaceEvent{Kind: platform.WebViewKey, Key: k, Mods: mods & (platform.ModShift | platform.ModCtrl | platform.ModAlt | platform.ModSuper), Repeat: repeat, WebView: w}, true
}
