//go:build linux && (amd64 || arm64)

package linux

import (
	"sync"
	"unsafe"
)

// WebKitGTK's session state (2.12+), bound the first time a page's is
// saved or restored.
var (
	webkitWebViewGetSessionState         func(wv ptr) ptr
	webkitWebViewRestoreSessionState     func(wv, state ptr)
	webkitWebViewSessionStateNew         func(bytes ptr) ptr
	webkitWebViewSessionStateSerialize   func(state ptr) ptr
	webkitWebViewSessionStateUnref       func(state ptr)
	webkitWebViewGetBackForwardList      func(wv ptr) ptr
	webkitBackForwardListGetCurrentItem  func(list ptr) ptr
	webkitWebViewGoToBackForwardListItem func(wv, item ptr)
	gBytesGetData                        func(bytes ptr, size *uintptr) unsafe.Pointer
	sessionStateOnce                     sync.Once
	sessionStateBound                    bool
)

func bindSessionState() bool {
	sessionStateOnce.Do(func() {
		w := libWebKit
		sessionStateBound = bind(w, &webkitWebViewGetSessionState, "webkit_web_view_get_session_state") &&
			bind(w, &webkitWebViewRestoreSessionState, "webkit_web_view_restore_session_state") &&
			bind(w, &webkitWebViewSessionStateNew, "webkit_web_view_session_state_new") &&
			bind(w, &webkitWebViewSessionStateSerialize, "webkit_web_view_session_state_serialize") &&
			bind(w, &webkitWebViewSessionStateUnref, "webkit_web_view_session_state_unref") &&
			bind(w, &webkitWebViewGetBackForwardList, "webkit_web_view_get_back_forward_list") &&
			bind(w, &webkitBackForwardListGetCurrentItem, "webkit_back_forward_list_get_current_item") &&
			bind(w, &webkitWebViewGoToBackForwardListItem, "webkit_web_view_go_to_back_forward_list_item") &&
			bind(libGLib, &gBytesNew, "g_bytes_new") && bind(libGLib, &gBytesUnref, "g_bytes_unref") &&
			bind(libGLib, &gBytesGetData, "g_bytes_get_data")
	})
	return sessionStateBound
}

// SessionState serializes webkit_web_view_get_session_state: the
// back/forward list, in WebKitGTK's own versioned encoding.
func (w *window) SessionState() []byte {
	if !bindSessionState() {
		return nil
	}
	state := webkitWebViewGetSessionState(w.web)
	data := webkitWebViewSessionStateSerialize(state)
	webkitWebViewSessionStateUnref(state)
	var n uintptr
	p := gBytesGetData(data, &n)
	b := append([]byte{}, unsafe.Slice((*byte)(p), n)...)
	gBytesUnref(data)
	return b
}

// RestoreSessionState puts back the back/forward list, which
// webkit_web_view_restore_session_state does without loading anything,
// then goes to its current item, as Epiphany does. A state WebKitGTK
// can't read makes no session state.
func (w *window) RestoreSessionState(b []byte) bool {
	if len(b) == 0 || !bindSessionState() {
		return false
	}
	data := gBytesNew(&b[0], uintptr(len(b)))
	state := webkitWebViewSessionStateNew(data)
	gBytesUnref(data)
	if state == 0 {
		return false
	}
	webkitWebViewRestoreSessionState(w.web, state)
	webkitWebViewSessionStateUnref(state)
	item := webkitBackForwardListGetCurrentItem(webkitWebViewGetBackForwardList(w.web))
	if item == 0 {
		return false
	}
	webkitWebViewGoToBackForwardListItem(w.web, item)
	return true
}
