package mygo

import (
	"bytes"
	"runtime"
)

// SessionState saves the page's back/forward list, with the scroll
// positions and form fields the web engine saved, which
// PageOptions.SessionState brings back in a new window or web view: at
// the next launch, as browsers and document viewers bring back their
// windows, or after the app closed a page it wasn't showing, as browsers
// put tabs to sleep.
//
//	state, err := win.Page().SessionState()
//	// Later, or at the next launch:
//	win := mygo.NewWindow(mygo.WindowOptions{Page: mygo.PageOptions{SessionState: state}})
//
// WebKit saves them as it goes: the scroll position of the page it shows
// a moment after the user stops scrolling (WebKitGTK 2.38 and Safari 16's
// WebKit or later), its form fields only as it goes out of view (its
// window hidden, for one), and the other entries' scroll positions as
// the page leaves them. A state saved since has them.
//
// The state is bytes for the app to keep, in a file or with its own data:
// the page's URL and the web engine's own state of the page, in a format
// MyGo versions. It may hold what the user typed into forms, so keep it as
// the user's data; the HTML that LoadHTML loaded is not in it. It fails
// for a window showing native UI and after the window closed.
//
//   - macOS: WKWebView's interactionState, macOS 12 or later; the URL
//     alone before.
//   - Linux: webkit_web_view_get_session_state, serialized.
//   - Windows: the URL alone: WebView2 has no API for the back/forward
//     list.
func (p *Page) SessionState() ([]byte, error) {
	if p.w.content {
		return nil, errNoPage
	}
	var state []byte
	err := errDestroyed
	onMain(func() {
		if n := p.w.native; n != nil {
			state, err = encodeSessionState(runtime.GOOS, n.URL(), n.SessionState()), nil
		}
	})
	return state, err
}

// loadPage loads the first page of a new window or web view: the one of
// its options' session state, else url. Main thread only.
func (w *Window) loadPage(state []byte, url string) error {
	if state != nil {
		goos, u, native, ok := decodeSessionState(state)
		if ok && goos == runtime.GOOS && len(native) > 0 && w.native.RestoreSessionState(native) {
			return nil
		}
		if ok && u != "" {
			url = u
		}
	}
	if url == "" {
		return nil
	}
	return w.pg.LoadURL(url)
}

// The format of session states, version 1: sessionStateMagic, the
// version byte, the GOOS that saved it and the page's URL, each ended by
// a NUL byte, which neither holds, then the backend's own state, if any,
// which only a backend of the same GOOS reads.
const (
	sessionStateMagic   = "mygo-session"
	sessionStateVersion = 1
)

func encodeSessionState(goos, url string, native []byte) []byte {
	b := make([]byte, 0, len(sessionStateMagic)+3+len(goos)+len(url)+len(native))
	b = append(b, sessionStateMagic...)
	b = append(b, sessionStateVersion)
	b = append(append(b, goos...), 0)
	b = append(append(b, url...), 0)
	return append(b, native...)
}

// decodeSessionState reads a state encodeSessionState made; ok is false
// for anything else, a later version's states included.
func decodeSessionState(b []byte) (goos, url string, native []byte, ok bool) {
	rest, ok := bytes.CutPrefix(b, []byte(sessionStateMagic+string(rune(sessionStateVersion))))
	if !ok {
		return "", "", nil, false
	}
	g, rest, ok := bytes.Cut(rest, []byte{0})
	if !ok {
		return "", "", nil, false
	}
	u, native, ok := bytes.Cut(rest, []byte{0})
	if !ok {
		return "", "", nil, false
	}
	return string(g), string(u), native, true
}
