//go:build !js

package mygo

import (
	"context"
	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/mygo/internal/bridge"
	"github.com/egoist/mygo/internal/platform"
)

// What windows say to the bridge in their pages, and what they hear from
// it. Builds for browsers, whose windows show no pages, leave it out with
// the JSON it takes (bridge_js.go).

// bridgeScript returns the script that installs the bridge in the
// window's pages.
func (w *Window) bridgeScript() string {
	return bridge.Script(bridge.Config{
		Platform: jsPlatform(),
		WindowID: w.top().id, // the window a web view is in
		Version:  Version,
		Secret:   w.secret,
		Locale:   App.Locale(),
		Dir:      appDir(),
		Fit:      w.fitLimits(),
	})
}

// fitLimits returns the limits of the size of a window that takes its
// page's (FitContent), in the CSS pixels of its page, or nil. Main thread
// only.
func (w *Window) fitLimits() *bridge.FitContent {
	if w.fit == nil || w.content {
		return nil
	}
	l := bridge.NewFitContent(w.fit.min, w.fit.max, w.fit.zoom)
	return &l
}

// zoomChanged tells the page of a window that its zoom changed, and with
// it the CSS pixels of the room the window controls take and of the
// limits of FitContent: the page that shows through an event, and the
// pages it loads next through its scripts. Main thread only.
func (w *Window) zoomChanged() {
	w.sendTitleBar()
	if w.fit == nil || w.native == nil || w.content {
		return
	}
	w.fit.zoom = w.native.Zoom()
	w.userScripts = w.pageScripts()
	w.native.SetUserScripts(w.userScripts)
	if msg, err := encodeEvent(bridge.FitContentEvent, w.fitLimits()); err == nil {
		w.enqueue(msg, true)
	}
	// The content keeps its size in CSS pixels, which are other DIPs now.
	if f := w.fit; f.told {
		w.fitContentSize(f.content)
	}
}

// preferencesScriptIndex is the place of preferencesScript in a window's
// user scripts, right after the bridge, which it tells.
const preferencesScriptIndex = 1

// pageScripts are the scripts a page of the window gets at document start:
// the bridge, the preferences of the page and the preload script.
func (w *Window) pageScripts() []platform.UserScript {
	scripts := []platform.UserScript{{Source: w.bridgeScript()}, {Source: preferencesScript()}}
	if w.preload != "" {
		scripts = append(scripts, platform.UserScript{Source: w.preload, AllFrames: w.preloadAllFrames})
	}
	return scripts
}

// localeEvent returns the message of LocaleEvent for locale.
func localeEvent(locale string) (message, error) {
	return encodeEvent(bridge.LocaleEvent, bridge.Locale{Locale: locale, Dir: appDir()})
}

// sendLocale tells the page of a window the language the app shows, which
// the bridge configuration told it at document start: a page that started
// loading as the language changed may have had the one before. The bridge
// tells its listeners only of a language it did not have. Main thread
// only.
func (w *Window) sendLocale() {
	if w.content || w.native == nil {
		return
	}
	if msg, err := localeEvent(App.Locale()); err == nil {
		w.enqueue(msg, true)
	}
}

// pagesLocaleChanged tells the pages of every window the language the app
// shows, and makes it the one of the pages they load later. Main thread
// only.
func pagesLocaleChanged(locale string) {
	msg, err := localeEvent(locale)
	if err != nil {
		return
	}
	for _, w := range pageWindows() {
		if w.content || w.native == nil {
			continue
		}
		w.userScripts = w.pageScripts()
		w.native.SetUserScripts(w.userScripts)
		w.enqueue(msg, true)
	}
}

// preferencesScript tells a page the desktop's preferences at document
// start. Main thread only.
func preferencesScript() string {
	b, _ := json.Marshal(Theme.preferences())
	return bridge.PreferencesScript(b)
}

// sendPreferences tells the pages that the desktop's preferences changed.
// Main thread only.
func sendPreferences() {
	prefs, script := Theme.preferences(), preferencesScript()
	for _, w := range pageWindows() {
		w.sendPreferences(prefs, script)
	}
}

// sendPreferences tells the page of w that the preferences changed, if
// they did: the documents it loads next through its user script, which
// the backend replaces, so that their first paint has them, and the
// current one through the event. Main thread only.
func (w *Window) sendPreferences(prefs ThemePreferences, script string) {
	if w.content || w.native == nil || len(w.userScripts) <= preferencesScriptIndex ||
		w.userScripts[preferencesScriptIndex].Source == script {
		return
	}
	scripts := slices.Clone(w.userScripts)
	scripts[preferencesScriptIndex].Source = script
	w.userScripts = scripts
	w.native.SetUserScripts(scripts)
	if msg, err := encodeEvent(bridge.PreferencesEvent, prefs); err == nil {
		w.enqueue(msg, true)
	}
}

// decodeResult decodes the JSON result of eval into v.
func decodeResult(raw []byte, v any) error { return json.Unmarshal(raw, v) }

const (
	// The expression form: compile errors mean code is not an expression.
	evalExpr = "try{return JSON.stringify({ok:true,v:await(\n%s\n)})}catch(e){return JSON.stringify({ok:false,e:String(e&&e.message||e)})}"
	// The statement form runs code as a function body.
	evalBody = "try{return JSON.stringify({ok:true,v:await(async()=>{\n%s\n})()})}catch(e){return JSON.stringify({ok:false,e:String(e&&e.message||e)})}"
)

// evalCanceled, a hook of tests, runs after eval delivered the
// cancellation of its context.
var evalCanceled func()

// eval returns the JSON result of code, which shares the memory of the
// page's answer.
func (w *Window) eval(ctx context.Context, code string) (jsontext.Value, error) {
	if w.content {
		return nil, errNoPage
	}
	type result struct {
		raw string
		err error
	}
	// The cancellation, the page and the window closing may all answer; the
	// first one counts and the others must not block.
	ch := make(chan result, 3)
	stop := context.AfterFunc(ctx, func() {
		deliver(ch, result{err: ctx.Err()})
		if evalCanceled != nil {
			evalCanceled()
		}
	})
	defer stop()

	ran := false
	var off func()
	w.do(func(n platform.Window) {
		ran = true
		// A window opened with the quit under way: no one will close it, and
		// the event loop stops before the page answers.
		if nativeCalls.stopped {
			deliver(ch, result{err: errLoopStopped})
			return
		}
		// The page of a closed window never answers.
		off = w.onClosed.add(func() { deliver(ch, result{err: errDestroyed}) }, false)
		expr := strings.TrimRight(code, " \t\r\n;")
		n.CallAsyncFunction(fmt.Sprintf(evalExpr, expr), func(res string, err error) {
			if err == nil {
				deliver(ch, result{raw: res})
				return
			}
			if !strings.HasPrefix(err.Error(), "SyntaxError") {
				// It may have run (and then the page navigated or
				// crashed): running it again could run it twice.
				deliver(ch, result{err: err})
				return
			}
			// Not an expression: run it as a function body instead. The
			// first attempt failed to compile, so nothing ran twice.
			onMain(func() {
				if w.native == nil {
					deliver(ch, result{err: errDestroyed})
					return
				}
				w.native.CallAsyncFunction(fmt.Sprintf(evalBody, code), func(res string, err error) {
					deliver(ch, result{raw: res, err: err})
				})
			})
		})
	})
	if !ran {
		return nil, errDestroyed
	}
	r := await(ch)
	if off != nil {
		off()
	}
	if r.err != nil {
		return nil, r.err
	}
	var out struct {
		OK bool     `json:"ok"`
		V  rawValue `json:"v"`
		E  string   `json:"e"`
	}
	if err := json.Unmarshal(stringBytes(r.raw), &out); err != nil {
		return nil, fmt.Errorf("mygo: unexpected eval result %q: %w", r.raw, err)
	}
	if !out.OK {
		return nil, &EvalError{Message: out.E}
	}
	return jsontext.Value(out.V), nil
}

// sendTitleBar tells the page of a window with a hidden title bar the room
// its controls take. The backend's script tells the first page at document
// start; this keeps the current page, and later ones, up to date. Main
// thread only.
func (w *Window) sendTitleBar() {
	if w.content || !w.hiddenTitleBar || w.native == nil {
		return
	}
	if msg, err := encodeEvent(bridge.TitleBarEvent, bridge.NewTitleBar(w.native.TitleBar(), w.native.Zoom())); err == nil {
		w.enqueue(msg, true)
	}
}

// sendActive tells the page of a window, or of a web view in one, whether
// the window is drawn as active (active.go). Pages start active, and hear
// otherwise as their DOM is ready. Main thread only.
func (w *Window) sendActive() {
	if w.content || w.native == nil {
		return
	}
	if msg, err := encodeEvent(bridge.WindowActiveEvent, w.top().active); err == nil {
		w.enqueue(msg, true)
	}
}

// maxUntrustedMessage is the size of the largest message an untrusted
// page (PageOptions.Untrusted) may post: what MyGo's bridge posts in pages
// that call no Go method is much smaller.
const maxUntrustedMessage = 4 << 10

// handleMessage routes a message posted by the page bridge. It runs on the
// main thread.
func (w *Window) handleMessage(msg string) {
	if w.untrusted && len(msg) > maxUntrustedMessage {
		return // what the bridge posts is smaller
	}
	msg, ok := strings.CutPrefix(msg, w.secret)
	if !ok {
		return // not from the bridge
	}
	// Calls are decoded and executed off the main thread. The page context
	// is captured here so a navigation cannot slip in between.
	if len(msg) > 12 && msg[:12] == `{"t":"call",` {
		if w.untrusted {
			// Rejected right away, without a goroutine per message.
			handleCall(w, nil, msg, false)
			return
		}
		go handleCall(w, w.pageContext(), msg, w.trusted)
		return
	}
	var m struct {
		T  string  `json:"t"`
		X  float64 `json:"x"` // drop
		Y  float64 `json:"y"`
		C  int64   `json:"c"` // channels
		K  string  `json:"k"`
		N  int64   `json:"n"`
		V  string  `json:"v"` // theme-color
		W  float64 `json:"w"` // content-size
		H  float64 `json:"h"`
		SW float64 `json:"sw"`
		SH float64 `json:"sh"`
		// context-menu: the link, its text, the image, the media, the
		// selection, and whether it is editable.
		L  string `json:"l"`
		LT string `json:"lt"`
		I  string `json:"i"`
		M  string `json:"m"`
		S  string `json:"s"`
		E  bool   `json:"e"`
	}
	if err := json.Unmarshal(stringBytes(msg), &m); err != nil {
		return
	}
	switch m.T {
	case "dom-ready":
		w.outMu.Lock()
		w.domReady = true
		w.outbox = append(w.outbox, w.held...)
		w.held = nil
		w.outMu.Unlock()
		// The script at document start may tell a later page the room of
		// the controls before a change.
		w.sendTitleBar()
		w.sendLocale()
		if !w.top().active {
			w.sendActive()
		}
		w.flush()
		fire(&w.onDOMReady)
		w.readyToShow()
	case "theme-color":
		c, err := parseColor(m.V)
		w.setThemeColor(c, m.V != "" && err == nil)
	case "content-size":
		w.fitContentSize(contentSize{m.W, m.H, m.SW, m.SH})
	case "drag":
		if w.native != nil && !w.untrusted {
			w.native.StartDrag()
		}
	case "dblclick":
		if w.native != nil && !w.untrusted {
			w.native.TitleBarDoubleClicked()
		}
	case "escape":
		w.escape()
	case "context-menu":
		// What a context menu about to open was opened on, which the
		// backend may not tell (context_menu.go). An untrusted page's
		// record over maxUntrustedMessage, as of a long selection, is
		// dropped with the rest.
		if w.onContextMenu.len() > 0 {
			w.contextTarget = &contextTarget{platform.ContextMenuTarget{
				LinkURL: m.L, LinkText: m.LT, ImageURL: m.I, MediaURL: m.M, SelectionText: m.S, Editable: m.E,
			}, m.X, m.Y}
		}
	case "drop":
		if w.native != nil {
			w.filesDropped(w.native.DroppedFiles(), int(m.X), int(m.Y))
		}
	case "chan-ack":
		if c := w.channel(m.C, m.K); c != nil {
			c.ack(m.N)
		}
	case "chan-close":
		w.pageClosedChannel(m.C, m.K)
	}
}

// findScript returns the script with which FindInPage asks the bridge.
func findScript(text string, opts FindOptions) string {
	t, _ := jsonv1.Marshal(text)
	o, _ := jsonv1.Marshal(map[string]bool{"matchCase": opts.MatchCase, "backward": opts.Backward, "next": opts.FindNext})
	return fmt.Sprintf("window.__mygo.find(%s, %s)", t, o)
}
