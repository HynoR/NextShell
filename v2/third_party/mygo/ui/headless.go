package ui

import (
	"fmt"
	"image"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/raster"
	"github.com/egoist/mygo/internal/scene"
	"github.com/egoist/mygo/transfer"
)

// headless renders frames in memory with the software renderer.
type headless struct {
	w, h, scale float32
	// maxButton is where the view shows the maximize button.
	maxButton Rect
	img       raster.Renderer
	// requested is set when the view asks for a frame, from any goroutine
	// for invalidate.
	requested atomic.Bool
	dark      bool
	prefs     platform.Preferences
	// lang and reg are what SetLocale and SetRegion set.
	lang string
	reg  i18n.Region
	// rtl lays the window out right to left (SetDirection).
	rtl       bool
	clipboard string
	// clipboardData is what WriteClipboardData copied, with its formats.
	clipboardData transfer.Data
	cursor        Cursor
	ime           platform.TextInputState
	opened        []string
	// escapes counts the Escapes nothing handled.
	escapes int
	openErr error
	// clipboardErr fails the writes of text to the clipboard
	// (FailClipboard).
	clipboardErr error
	// later are what the view asked to run after the frame, as a window
	// posts them to the main thread.
	later  []func()
	bar    TitleBar
	access *platform.AccessTree
	// told are the announcements of the trees, and announced those the
	// view made.
	told, announced []string
	// menu is the context menu shown, at menuAt, and chosen takes its
	// choice.
	menu   *platform.Menu
	menuAt [2]float32
	chosen func(id int)
	// hz is the display's refresh rate; hidden tells that nothing of the
	// window shows.
	hz     float32
	hidden bool
	// frameRate is what the engine last told of the frames a second that
	// come while only drawings move (setFrameRate).
	frameRate float32
	// atOnce has requestFrame stand for a surface whose frames come at
	// once rather than at the display's next refresh (framesAtOnce).
	atOnce bool
	// material tells that the window shows one (SetVibrancy), and
	// inactive that it is not drawn as active (SetWindowActive).
	material, inactive bool
	// reading runs once as the clipboard is next read, as GTK's nested
	// event loop may draw a frame then.
	reading func()
	// last is the scene of the last frame, which tests inspect.
	last        *scene.Scene
	dragData    transfer.Data
	dragLocal   any
	dragOptions transfer.DragOptions
	// webViews are where the last frame showing web views showed them;
	// tabbedInto the web view Tab gave the keyboard last, from its end
	// when tabbedBack.
	webViews   []placedWebView
	tabbedInto NativeWebView
	tabbedBack bool

	// invalidated asks for a frame built anew, as a window's Changed.
	invalidated atomic.Bool
	// afterAt and repaintAt are when the timers of After and Painter.After
	// are due, zero for none; afterFrom is when After's was armed. No
	// timer runs: the Tester's time may stop or leap.
	afterAt, afterFrom, repaintAt time.Time
	clock                         func() time.Time
}

func (h *headless) size() (float32, float32, float32) { return h.w, h.h, h.scale }
func (h *headless) refreshRate() float32              { return h.hz }
func (h *headless) setFrameRate(fps float32)          { h.frameRate = fps }
func (h *headless) framesAtOnce() bool                { return h.atOnce }
func (h *headless) occluded() bool                    { return h.hidden }
func (h *headless) present(s *scene.Scene) {
	h.last = s
	h.img.Render(s)
}
func (h *headless) afterTimer(at time.Time) {
	h.afterAt = at
	if h.clock != nil {
		h.afterFrom = h.clock()
	}
}
func (h *headless) framePath() string                          { return "drawn in memory" }
func (h *headless) requestFrame()                              { h.requested.Store(true) }
func (h *headless) repaintTimer(at time.Time)                  { h.repaintAt = at }
func (h *headless) setCursor(c Cursor)                         { h.cursor = c }
func (h *headless) setTextInput(t platform.TextInputState)     { h.ime = t }
func (h *headless) updateAccessibility(t *platform.AccessTree) { h.keepAccess(t) }
func (h *headless) startDrag()                                 {}
func (h *headless) setDropFormats([]transfer.Format)           {}
func (h *headless) readClipboard() string {
	if r := h.reading; r != nil {
		h.reading = nil
		r()
	}
	return h.clipboard
}

// readClipboardData reads the representations of formats of what was
// copied, or the text of WriteClipboard.
func (h *headless) readClipboardData(formats []transfer.Format) transfer.Data {
	d := h.clipboardData
	if len(d.Items()) == 0 && h.clipboard != "" {
		d = transfer.TextData(h.clipboard)
	}
	if formats == nil {
		return d
	}
	var reps []transfer.Representation
	for _, f := range formats {
		if b, err := d.Read(f); err == nil {
			reps = append(reps, transfer.Bytes(f, b))
		}
	}
	if len(reps) == 0 {
		return transfer.Data{}
	}
	return transfer.New(transfer.NewItem(reps...))
}

func (h *headless) writeClipboardData(d transfer.Data) {
	h.clipboardData = d.Snapshot()
	b, _ := d.Read(transfer.Text)
	h.clipboard = string(b)
}

func (h *headless) clipboardFormats() []transfer.Format { return h.readClipboardData(nil).Formats() }

func (h *headless) startDataDrag(d transfer.Data, local any, o transfer.DragOptions, x, y float32) error {
	h.dragData, h.dragLocal, h.dragOptions = d.Snapshot(), local, o
	return nil
}
func (h *headless) cancelDataDrag() { h.finishDataDrag(transfer.Result{Canceled: true}) }
func (h *headless) finishDataDrag(r transfer.Result) {
	done := h.dragOptions.Done
	h.dragData, h.dragLocal, h.dragOptions = transfer.Data{}, nil, transfer.DragOptions{}
	if done != nil {
		done(r)
	}
}
func (h *headless) titleBarDoubleClicked()            {}
func (h *headless) setMaximizeButton(r Rect)          { h.maxButton = r }
func (h *headless) escape()                           { h.escapes++ }
func (h *headless) isDark() bool                      { return h.dark }
func (h *headless) rightToLeft() bool                 { return h.rtl }
func (h *headless) preferences() platform.Preferences { return h.prefs }
func (h *headless) locale() string                    { return h.lang }
func (h *headless) region() i18n.Region               { return h.reg }
func (h *headless) titleBar() TitleBar                { return h.bar }
func (h *headless) vibrancy() bool                    { return h.material }
func (h *headless) active() bool                      { return !h.inactive }
func (h *headless) invalidate() {
	h.invalidated.Store(true)
	h.requested.Store(true)
}

// writeClipboard copies s, unless FailClipboard set an error;
// writeClipboardThen gives done that error before the next frame too, as
// a window gives it after the system wrote the clipboard.
func (h *headless) writeClipboard(s string) {
	if h.clipboardErr == nil {
		h.clipboard, h.clipboardData = s, transfer.Data{}
	}
}

func (h *headless) writeClipboardThen(s string, done func(error)) {
	h.writeClipboard(s)
	err := h.clipboardErr
	h.later = append(h.later, func() { done(err) })
}

// openURL notes the link, and gives done the error FailOpenURL set before
// the next frame, as a window gives it after the system opened the link.
func (h *headless) openURL(u string, done func(error)) {
	h.opened = append(h.opened, u)
	if done != nil {
		err := h.openErr
		h.later = append(h.later, func() { done(err) })
	}
}

// post asks for a frame rather than run fn: the Tester builds each frame
// anew.
func (h *headless) post(func()) { h.requested.Store(true) }

// keepAccess keeps the tree for assistive technology, and its
// announcements.
func (h *headless) keepAccess(t *platform.AccessTree) {
	h.access, h.told = t, append(h.told, t.Announcements...)
}

func (h *headless) popupMenu(m *platform.Menu, x, y float32, chosen func(int)) {
	h.menu, h.menuAt, h.chosen = m, [2]float32{x, y}, chosen
}
func (h *headless) placeWebViews(views []placedWebView) {
	h.webViews = append(h.webViews[:0], views...)
}
func (h *headless) nativeGlass() bool                              { return false }
func (h *headless) placeGlass([]platform.GlassPlacement)           {}
func (h *headless) tabIntoWebView(v NativeWebView, back bool)      { h.tabbedInto, h.tabbedBack = v, back }
func (h *headless) platformWebView(NativeWebView) platform.WebView { return nil }
func (h *headless) image() *image.RGBA {
	m := &h.img.Image
	return &image.RGBA{Pix: m.RGBA(), Stride: 4 * m.W, Rect: image.Rect(0, 0, m.W, m.H)}
}

// Render draws a frame of view in a window of width×height DIPs at scale
// device pixels per DIP, without a window: for snapshots and tests.
func coreRender(view func(c *context), width, height int, scale float32) *image.RGBA {
	t := coreNewTester(view, width, height)
	t.SetScale(scale)
	return t.Image()
}

// Tester runs a view without a window, as a test drives it: it renders
// frames in memory and sends the view pointer and keyboard input, between
// which it settles the frames the view asks for.
//
// Its clock stands still unless the test moves it with Advance, so what
// depends on time (animations, Context.After, the clicks that make a
// double click, typing that undoes in one step, tooltips) does the same
// however fast the machine runs the test.
type Tester struct {
	batch  int
	rt     *engine
	h      *headless
	clicks platform.ClickCounter
	// now is the Tester's time, which only Advance moves, and frameAt
	// that of the last frame.
	now     time.Time
	frameAt time.Time
	// pointer and pressure are the device of the pointer's events, and
	// how hard it presses (UsePointer).
	pointer  PointerType
	pressure float32
}

// NewTester starts testing view in a window of width×height DIPs. Two
// elements given one key under one parent make it panic where the second
// was given, as apps only log it.
func coreNewTester(view func(c *context), width, height int) *Tester {
	return coreNewTesterWith(view, width, height, TesterOptions{})
}

// TesterOptions are settings of the desktop that a Tester starts with,
// which its first frame follows, as Media Queries 5 has the user's
// preferences apply from the first time styles resolve: a view mounted
// under ReduceMotion starts no animation in its first frames either, as
// it would before a SetPreferences after NewTester.
type TesterOptions struct {
	// Preferences are the desktop's settings that controls follow
	// (SetPreferences).
	Preferences Preferences
	// Dark starts in the dark appearance (SetDark).
	Dark bool
}

// NewTesterWith starts testing view in a window of width×height DIPs, as
// NewTester does, with the desktop's settings of o from the first frame:
//
//	tt := ui.NewTesterWith(view, 400, 300, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: true}})
func coreNewTesterWith(view func(c *context), width, height int, o TesterOptions) *Tester {
	h := &headless{w: float32(width), h: float32(height), scale: 1, prefs: platformPreferences(o.Preferences), dark: o.Dark}
	t := &Tester{rt: newRuntime(view, h), h: h, now: time.Now()}
	t.rt.clock = t.Now
	h.clock = t.Now
	t.rt.collect = true
	// Duplicate keys panic, so that the test fails where the key was given.
	t.rt.strict = true
	t.rt.handleChecks = true
	t.settle()
	return t
}

// settle runs frames until the view asks for no more, or 20 of them.
func (t *Tester) settle() {
	if t.batch > 0 {
		return
	}
	t.frameAt = t.Now()
	for i := 0; i < 20; i++ {
		t.h.requested.Store(false)
		t.h.invalidated.Store(false)
		t.runLater()
		t.rt.runFrame()
		if !t.asked() {
			return
		}
	}
}

// asked reports whether the view asked for a frame.
func (t *Tester) asked() bool { return t.h.requested.Load() || len(t.h.later) > 0 }

// runLater runs what the view asked to run after the frame.
func (t *Tester) runLater() {
	for len(t.h.later) > 0 {
		fn := t.h.later[0]
		t.h.later = t.h.later[1:]
		fn()
	}
}

// Now returns the Tester's time, that of its next frame, which stands
// still until Advance moves it.
func (t *Tester) Now() time.Time { return t.now }

// Advance moves the Tester's clock on by d, running the frames due
// meanwhile at their times: one each refresh while something moves, and
// those of After and Painter.After when they come due. An animation of
// 200 ms is a quarter of the way after Advance(50 * time.Millisecond),
// and done after another Advance(150 * time.Millisecond).
func (t *Tester) Advance(d time.Duration) {
	end := t.now.Add(max(d, 0))
	for {
		at, ok := t.due()
		if !ok || at.After(end) {
			break
		}
		t.now = at
		if h := t.h; !h.afterAt.IsZero() && !at.Before(h.afterAt) {
			h.afterAt = time.Time{}
			h.invalidate()
		}
		if h := t.h; !h.repaintAt.IsZero() && !at.Before(h.repaintAt) {
			h.repaintAt = time.Time{}
			t.rt.repaintNow()
		}
		if t.asked() {
			// Built anew after invalidate, as a window's.
			if t.h.invalidated.Swap(false) {
				t.rt.redraw = false
			}
			t.h.requested.Store(false)
			t.runLater()
			t.rt.surfaceFrame()
			t.frameAt = at
		}
	}
	t.now = end
}

// due returns when the next frame is due: the next refresh if one was
// asked for, or a timer's.
func (t *Tester) due() (time.Time, bool) {
	now := t.Now()
	var at time.Time
	consider := func(due time.Time) {
		if due.Before(now) {
			due = now
		}
		if at.IsZero() || due.Before(at) {
			at = due
		}
	}
	if t.asked() {
		hz := t.h.hz
		if hz <= 0 {
			hz = 60
		}
		consider(t.frameAt.Add(time.Duration(float64(time.Second) / float64(hz))))
	}
	if due, ok := t.timerDue(t.h.afterAt, t.h.afterFrom); ok {
		consider(due)
	}
	if due, ok := t.timerDue(t.h.repaintAt, t.frameAt); ok {
		consider(due)
	}
	return at, !at.IsZero()
}

// timerDue returns when a timer for at, armed at armed, fires: at least a
// millisecond later, as a window's.
func (t *Tester) timerDue(at, armed time.Time) (time.Time, bool) {
	if at.IsZero() {
		return at, false
	}
	if soonest := armed.Add(time.Millisecond); at.Before(soonest) {
		at = soonest
	}
	return at, true
}

func (t *Tester) send(ev platform.SurfaceEvent) {
	switch ev.Kind {
	case platform.PointerMove, platform.PointerDown, platform.PointerUp:
		ev.Pointer, ev.Pressure = platform.PointerKind(t.pointer), float64(t.pressure)
	}
	if ev.Kind == platform.PointerDown && ev.Clicks == 0 {
		// Counted as a backend counts.
		ev.Clicks = t.clicks.Press(ev.Button, ev.X, ev.Y, uint32(t.now.UnixMilli()), platform.DefaultDoubleClick)
	}
	t.rt.event(ev)
	t.settle()
}

// SetSize resizes the window.
func (t *Tester) SetSize(width, height int) {
	t.h.w, t.h.h = float32(width), float32(height)
	t.settle()
}

// SetScale sets the device pixels per DIP.
func (t *Tester) SetScale(scale float32) {
	t.h.scale = scale
	t.settle()
}

// SetZoom zooms the content by z, as the view's Context.SetZoom does. The
// Tester's coordinates stay the window's: Find, ClickAt, Move, Scroll and
// TextCaret are in its DIPs, the view's z times smaller.
func (t *Tester) SetZoom(z float32) {
	t.rt.setZoom(z)
	t.settle()
}

// SetTitleBar sets the room the window controls of a window with a hidden
// title bar take, which Context.TitleBar returns.
func (t *Tester) SetTitleBar(bar TitleBar) {
	t.h.bar = bar
	t.Frame()
}

// SetVibrancy sets whether the window shows a material where the view
// draws no background, which Context.Vibrancy returns.
func (t *Tester) SetVibrancy(shows bool) {
	t.h.material = shows
	t.Frame()
}

// SetWindowActive draws the window as active, the window the user's
// input goes to, or not, as when the user switches to another window or
// app: Context.WindowActive returns it. It leaves the keyboard focus to
// SetFocused, as a window's flyout taking the keyboard leaves the window
// active.
func (t *Tester) SetWindowActive(active bool) {
	t.h.inactive = !active
	t.rt.activeChanged()
	t.settle()
}

// SetWindowHidden hides all of the window from sight, as minimizing it
// or covering it with other windows does, or shows it again:
// Context.WindowHidden returns it, and what moves waits while it is
// hidden.
func (t *Tester) SetWindowHidden(hidden bool) {
	if t.h.hidden == hidden {
		return
	}
	t.h.hidden = hidden
	kind := platform.SurfaceShown
	if hidden {
		kind = platform.SurfaceHidden
	}
	t.send(platform.SurfaceEvent{Kind: kind})
}

// SetDark switches between the light and the dark appearance.
func (t *Tester) SetDark(dark bool) {
	t.h.dark = dark
	t.rt.themeChanged()
	t.settle()
}

// SetPreferences changes the desktop's settings that controls follow, as
// the user does in the system's settings.
func (t *Tester) SetPreferences(p Preferences) {
	t.h.prefs = platformPreferences(p)
	t.rt.themeChanged()
	t.settle()
}

// platformPreferences returns p as a backend gives them.
func platformPreferences(p Preferences) platform.Preferences {
	return platform.Preferences{Accent: platform.Color{R: p.Accent.R, G: p.Accent.G, B: p.Accent.B, A: p.Accent.A}, ReduceMotion: p.ReduceMotion,
		ReduceTransparency: p.ReduceTransparency, HighContrast: p.HighContrast, TextScale: float64(p.TextScale),
		Pointer: platform.PointerAccuracy(p.Pointer), AnyPointer: platform.PointerAccuracy(p.AnyPointer), NoHover: p.NoHover, NoAnyHover: p.NoAnyHover}
}

// SetLocale makes tag the language the app shows, as mygo.App.SetLocale
// does: the view reads it with Context.Locale, and text without a
// language of its own is in it. A Tester starts with none, "", which
// draws text as the system does by default.
func (t *Tester) SetLocale(tag string) {
	t.h.lang = tag
	t.rt.changed()
	t.settle()
}

// SetRegion changes the user's regional formats, which the view reads with
// Context.Region, as the user does in the system's settings. A Tester
// starts with the zero Region.
func (t *Tester) SetRegion(r i18n.Region) {
	t.h.reg = r
	t.rt.changed()
	t.settle()
}

// SetDirection lays the window out in d, as an app whose language reads
// so: RightToLeft lays it out from the right.
func (t *Tester) SetDirection(d Direction) {
	t.h.rtl = d == RightToLeft
	t.Frame()
}

// Frame renders another frame, as when the view's state changed.
func (t *Tester) Frame() { t.h.requested.Store(true); t.settle() }

// FramePending reports whether the view still asks for a frame, as one
// that animates or never settles does after an input or Frame. The
// frames of After and Painter.After wait for Advance to reach their
// time, as the clock stands still until then.
func (t *Tester) FramePending() bool { return t.asked() }

// Image returns the last frame.
func (t *Tester) Image() *image.RGBA { return t.h.image() }

// Find returns the box of the first element showing text s, or labeled s,
// in the window's DIPs: zoomed (SetZoom), z times the content's.
func (t *Tester) Find(s string) (Rect, bool) {
	for _, n := range t.rt.labels {
		if n.text == s {
			r, z := n.r, t.rt.zoomFactor()
			return Rect{r.X * z, r.Y * z, r.W * z, r.H * z}, true
		}
	}
	return Rect{}, false
}

// Texts returns the texts of the last frame, in order.
func (t *Tester) Texts() []string {
	var out []string
	for _, n := range t.rt.labels {
		out = append(out, n.text)
	}
	return out
}

// HasText reports whether the last frame shows a text containing s.
func (t *Tester) HasText(s string) bool {
	for _, n := range t.rt.labels {
		if strings.Contains(n.text, s) {
			return true
		}
	}
	return false
}

// targetCenter checks the named element before sending any input. Labels
// include clipped boxes, and a visible box may be covered by another control.
func (t *Tester) targetCenter(s string, button int) (float32, float32, error) {
	var n *labelNode
	for i := range t.rt.labels {
		if t.rt.labels[i].text == s {
			n = &t.rt.labels[i]
			break
		}
	}
	if n == nil {
		return 0, 0, fmt.Errorf("ui: no element shows %q", s)
	}
	r, z := n.r, t.rt.zoomFactor()
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	x, y := cx*z, cy*z
	if x < 0 || y < 0 || x >= t.h.w || y >= t.h.h {
		return 0, 0, fmt.Errorf("ui: %q is at (%v, %v), outside the window", s, x, y)
	}
	if r.W <= 0 || r.H <= 0 {
		return 0, 0, fmt.Errorf("ui: %q has no visible area", s)
	}
	var named []uint64
	for st := t.rt.states[n.id]; st != nil; st = t.rt.states[st.parent] {
		named = append(named, st.id)
		if st.parent == 0 {
			break
		}
	}
	// Native pages take the pointer before UI hit testing, even inside
	// IgnoreInput: the system gives the press to their view.
	if page := t.WebViewAt(x, y); page != nil {
		for _, id := range named {
			if t.rt.states[id].webView == page {
				return x, y, nil
			}
		}
		return 0, 0, fmt.Errorf("ui: %q is covered by a web view", s)
	}
	hit := t.rt.hitChain(cx, cy)
	if button == 0 {
		for _, id := range hit {
			st := t.rt.states[id]
			if st == nil || st.flags&(flagScrollX|flagScrollY) == 0 || !st.barsShown || st.hideScrollbars {
				continue
			}
			lx, ly := st.local(cx, cy)
			lx, ly = lx+st.x, ly+st.y
			g := scrollBars(Rect{st.x, st.y, st.w, st.h}, st.barInset, float32(st.contentW), float32(st.contentH),
				float32(st.scrollX), float32(st.scrollY), st.flags, st.barWidth, st.rtl)
			if g.vertical && g.vTrack.Contains(lx, ly) || g.horizontal && g.hTrack.Contains(lx, ly) {
				return 0, 0, fmt.Errorf("ui: %q is covered by a scroll bar", s)
			}
		}
	}
	// A button's label is often a text child. Noninteractive siblings
	// inside it may take the hit too; both must still reach that button.
	flags := interactive
	if button == 1 {
		flags |= flagContextMenu
	}
	receiverOf := func(chain []uint64, f uint32) *state {
		for _, id := range chain {
			if st := t.rt.states[id]; st != nil &&
				(st.flags&f != 0 || st.input != nil || st.webView != nil) {
				return st
			}
		}
		return nil
	}
	receiver := func(chain []uint64) *state { return receiverOf(chain, flags) }
	want := receiver(named)
	// A field's label can name its outer container, which delegates focus
	// to an input nested inside. Clicking that input is the same target.
	matches := slices.Contains(hit, n.id)
	if want != nil {
		got := receiver(hit)
		matches = got == want && t.rt.handler(hit) == t.rt.handler(named) &&
			(button != 1 || t.rt.menuTarget(hit) == t.rt.menuTarget(named))
		// A control without a menu of its own, as a list's row, opens
		// the menu of the content it holds (#1414): the press reaches the
		// named control, nothing between takes a primary click or input,
		// and the menu opened is that content's. A control over the named
		// one is not inside it, and one inside with a menu of its own
		// keeps it.
		if i := slices.Index(hit, want.id); !matches && button == 1 && i > 0 &&
			want.flags&flagContextMenu == 0 && got != nil && got != want &&
			receiverOf(hit[:i], interactive) == nil && t.rt.menuTarget(hit) == got &&
			t.rt.handler(hit) == t.rt.handler(named) {
			matches = true
		}
		for i := range t.rt.c.used {
			e := t.rt.nodeAt(uint32(i))
			if e.id == want.id && e.focusTo != nil && got == e.focusTo.st && slices.Contains(hit, n.id) {
				matches = true
				break
			}
		}
	}
	if !matches || len(named) == 0 || t.rt.states[n.id].ignoreInput {
		return 0, 0, fmt.Errorf("ui: %q is not the input target at (%v, %v)", s, x, y)
	}
	return x, y, nil
}

// Click clicks the center of the element showing text or labeled s. It
// returns an error without sending input when no element shows s, it has
// no visible area, or its center would reach another control or nothing.
func (t *Tester) Click(s string) error {
	x, y, err := t.targetCenter(s, 0)
	if err != nil {
		return err
	}
	t.ClickAt(x, y)
	return nil
}

// ClickAt clicks at (x, y), in DIPs.
func (t *Tester) ClickAt(x, y float32) { t.ClickAtWith(0, x, y) }

// ClickWith clicks the center of the element showing text or labeled s
// holding the modifier keys mods, as a Shift-click. It checks the target
// before sending input, as Click does.
func (t *Tester) ClickWith(mods Modifiers, s string) error {
	x, y, err := t.targetCenter(s, 0)
	if err != nil {
		return err
	}
	t.ClickAtWith(mods, x, y)
	return nil
}

// ClickAtWith clicks at (x, y), in DIPs, holding the modifier keys mods:
// a web view that takes the pointer there (WebViewAt) takes the click.
func (t *Tester) ClickAtWith(mods Modifiers, x, y float32) {
	m := platform.Modifiers(mods)
	if t.webViewPress(x, y, 0, m) {
		return
	}
	t.send(platform.SurfaceEvent{Kind: platform.PointerDown, X: float64(x), Y: float64(y), Mods: m})
	t.send(platform.SurfaceEvent{Kind: platform.PointerUp, X: float64(x), Y: float64(y), Mods: m})
}

// RightClick clicks the center of the element showing text or labeled s
// with the secondary button. It checks the target before sending input,
// as Click does. On an element without a context menu of its own, as a
// list's row, it opens the menu of the content the element holds there.
func (t *Tester) RightClick(s string) error {
	x, y, err := t.targetCenter(s, 1)
	if err != nil {
		return err
	}
	t.RightClickAt(x, y)
	return nil
}

// RightClickAt clicks at (x, y), in DIPs, with the secondary button.
func (t *Tester) RightClickAt(x, y float32) {
	if t.webViewPress(x, y, 1, 0) {
		return
	}
	t.send(platform.SurfaceEvent{Kind: platform.PointerDown, X: float64(x), Y: float64(y), Button: 1})
	t.send(platform.SurfaceEvent{Kind: platform.PointerUp, X: float64(x), Y: float64(y), Button: 1})
}

// Menu returns the labels of the items of the context menu shown, "-" for
// separators, or nil when none is: the system's, or one drawn in the window
// (PopoverMenu, SetMenuPresentation).
func (t *Tester) Menu() []string {
	m, _ := t.currentMenu()
	if m == nil {
		return nil
	}
	out := []string{}
	for _, it := range m.Items {
		label := it.Label
		if it.Type == platform.MenuItemSeparator {
			label = "-"
		}
		out = append(out, label)
	}
	return out
}

// ChooseMenuItem chooses the item of the context menu shown labeled with
// the last of path, in the submenus the others label, and closes the menu.
func (t *Tester) ChooseMenuItem(path ...string) error {
	m, choose := t.currentMenu()
	if m == nil {
		return fmt.Errorf("ui: no context menu is shown")
	}
	if len(path) == 0 {
		return fmt.Errorf("ui: no item to choose")
	}
	var found *platform.MenuItem
	for i, label := range path {
		found = nil
		for _, it := range m.Items {
			if it.Label == label && it.Type != platform.MenuItemSeparator {
				found = it
				break
			}
		}
		switch {
		case found == nil:
			return fmt.Errorf("ui: the context menu has no item %q", strings.Join(path[:i+1], " > "))
		case !found.Enabled:
			return fmt.Errorf("ui: the item %q is disabled", strings.Join(path[:i+1], " > "))
		case i < len(path)-1:
			if found.Submenu == nil {
				return fmt.Errorf("ui: the item %q has no submenu", strings.Join(path[:i+1], " > "))
			}
			m = found.Submenu
		}
	}
	if found.Type == platform.MenuItemSubmenu {
		return fmt.Errorf("ui: %q opens a submenu", strings.Join(path, " > "))
	}
	choose(found.ID)
	t.settle()
	return nil
}

// CloseMenu closes the context menu shown without choosing an item.
func (t *Tester) CloseMenu() {
	if t.rt.menu.drawn != nil {
		t.rt.menu.drawn = nil
		t.settle()
		return
	}
	t.h.menu, t.h.chosen = nil, nil
}

// currentMenu returns the context menu shown, the one drawn in the window
// or else the system's, and what chooses its item id and closes it; nil
// when none shows.
func (t *Tester) currentMenu() (*platform.Menu, func(id int)) {
	if d := t.rt.menu.drawn; d != nil && d.items != nil {
		return d.items, func(id int) { d.choose = id }
	}
	if t.h.menu == nil {
		return nil, nil
	}
	chosen := t.h.chosen
	return t.h.menu, func(id int) {
		t.h.menu, t.h.chosen = nil, nil
		chosen(id)
	}
}

// Press presses (x, y) without releasing; Release releases it.
func (t *Tester) Press(x, y float32) { t.PressButton(0, x, y) }

// Release releases the pointer at (x, y).
func (t *Tester) Release(x, y float32) { t.ReleaseButton(0, x, y) }

// PressButton presses (x, y) with the pointer's button, 0 the primary, 1
// the secondary and 2 the middle, without releasing, as a right-button
// drag begins; ReleaseButton releases it.
func (t *Tester) PressButton(button int, x, y float32) {
	if t.webViewPress(x, y, button, 0) {
		return
	}
	t.send(platform.SurfaceEvent{Kind: platform.PointerDown, X: float64(x), Y: float64(y), Button: button})
}

// ReleaseButton releases the pointer's button at (x, y).
func (t *Tester) ReleaseButton(button int, x, y float32) {
	t.send(platform.SurfaceEvent{Kind: platform.PointerUp, X: float64(x), Y: float64(y), Button: button})
}

// CancelPress has the system take the pointer's press, as a browser's
// pointercancel does: every button held goes up without a release, which
// clicks nothing (InputEvent.Canceled, PressCanceled).
func (t *Tester) CancelPress() {
	t.send(platform.SurfaceEvent{Kind: platform.PointerCancel})
}

// WebViewAt returns the web view (WebView) that takes the pointer at
// (x, y), as the window shows it: where the last frame showed it and
// painted nothing over it that takes the pointer; nil for none.
func (t *Tester) WebViewAt(x, y float32) NativeWebView {
	for _, v := range slices.Backward(t.h.webViews) {
		if p := v.placement(nil); p.At(float64(x), float64(y)) {
			return v.v
		}
	}
	return nil
}

// webViewPress presses button at (x, y) on the web view taking the
// pointer there, as the platform would, which the content only hears of,
// and reports whether one took it.
func (t *Tester) webViewPress(x, y float32, button int, mods platform.Modifiers) bool {
	if t.WebViewAt(x, y) == nil {
		return false
	}
	t.send(platform.SurfaceEvent{Kind: platform.WebViewPress, X: float64(x), Y: float64(y), Button: button, Mods: mods})
	return true
}

// UsePointer has the pointer's presses and moves from now on come from
// the device pointer pressing with pressure, from 0 to 1, as a pen's, which
// a system tells: 0 is a mouse's, which tells none. The Tester starts with
// UsePointer(PointerMouse, 0). Calling it between moves while the pointer
// presses changes the pressure as a pen pressing harder does:
//
//	tt.UsePointer(ui.PointerPen, 0.2)
//	tt.Press(10, 10)
//	tt.UsePointer(ui.PointerPen, 0.9)
//	tt.Move(10, 10) // the pen presses harder where it stands
func (t *Tester) UsePointer(pointer PointerType, pressure float32) {
	t.pointer, t.pressure = pointer, pressure
}

// Move moves the pointer to (x, y).
func (t *Tester) Move(x, y float32) {
	t.send(platform.SurfaceEvent{Kind: platform.PointerMove, X: float64(x), Y: float64(y)})
}

// Scroll scrolls by dx, dy DIPs with the pointer at (x, y), as a touchpad
// does: at once.
func (t *Tester) Scroll(x, y, dx, dy float32) {
	t.send(platform.SurfaceEvent{Kind: platform.PointerScroll, X: float64(x), Y: float64(y), DX: float64(dx), DY: float64(dy), Precise: true})
}

// DropFiles drops files dragged from another app at (x, y), as from
// Finder, and reports whether an element took them (DroppedFiles).
func (t *Tester) DropFiles(x, y float32, paths ...string) bool {
	t.send(platform.SurfaceEvent{Kind: platform.FileDragOver, X: float64(x), Y: float64(y)})
	taken := t.rt.event(platform.SurfaceEvent{Kind: platform.FileDrop, X: float64(x), Y: float64(y), Files: paths})
	t.settle()
	return taken
}

// Wheel scrolls by dx, dy DIPs with the pointer at (x, y), as a notch of
// a mouse wheel does: a scroll container glides there over a moment,
// which advancing the clock (Advance) shows.
func (t *Tester) Wheel(x, y, dx, dy float32) {
	t.send(platform.SurfaceEvent{Kind: platform.PointerScroll, X: float64(x), Y: float64(y), DX: float64(dx), DY: float64(dy)})
}

// Pinch zooms by scale with the pointer at (x, y), as pinching a touchpad
// does (InputZoom).
func (t *Tester) Pinch(x, y, scale float32) {
	t.send(platform.SurfaceEvent{Kind: platform.PointerZoom, X: float64(x), Y: float64(y), Scale: float64(scale)})
}

// HoldModifiers presses or lets go of modifier keys on their own, as holding
// Cmd does, leaving mods held.
func (t *Tester) HoldModifiers(mods Modifiers) {
	t.send(platform.SurfaceEvent{Kind: platform.ModifiersChanged, Mods: platform.Modifiers(mods)})
}

// Key presses a key with modifiers.
func (t *Tester) Key(mods Modifiers, key Key) {
	t.send(platform.SurfaceEvent{Kind: platform.KeyPressed, Key: platform.Key(key), Mods: platform.Modifiers(mods)})
	t.send(platform.SurfaceEvent{Kind: platform.KeyReleased, Key: platform.Key(key), Mods: platform.Modifiers(mods)})
}

// KeyDown presses a key and holds it, as a player holds a key that walks,
// until KeyUp.
func (t *Tester) KeyDown(key Key) {
	t.send(platform.SurfaceEvent{Kind: platform.KeyPressed, Key: platform.Key(key)})
}

// KeyRepeat presses a key with modifiers as its auto-repeat does while
// it is held down (InputEvent.Repeat, Context.ShortcutRepeated).
func (t *Tester) KeyRepeat(mods Modifiers, key Key) {
	t.send(platform.SurfaceEvent{Kind: platform.KeyPressed, Key: platform.Key(key), Mods: platform.Modifiers(mods), Repeat: true})
}

// KeyUp releases a key KeyDown held.
func (t *Tester) KeyUp(key Key) {
	t.send(platform.SurfaceEvent{Kind: platform.KeyReleased, Key: platform.Key(key)})
}

// KeyAt presses a key that the active layout does not type as a US key
// (Key is KeyUnknown), at position physical of an ANSI US keyboard, as S
// is pressed on a Cyrillic layout.
func (t *Tester) KeyAt(mods Modifiers, physical Key) {
	t.send(platform.SurfaceEvent{Kind: platform.KeyPressed, PhysicalKey: platform.Key(physical), Mods: platform.Modifiers(mods)})
	t.send(platform.SurfaceEvent{Kind: platform.KeyReleased, PhysicalKey: platform.Key(physical), Mods: platform.Modifiers(mods)})
}

// CodeDown presses a key that only its position names, as a modifier on its
// own or Caps Lock (InputEvent.Code): mods are the modifiers held with it,
// its own among them.
func (t *Tester) CodeDown(mods Modifiers, code KeyCode) {
	t.send(platform.SurfaceEvent{Kind: platform.RawKeyPressed, Code: platform.KeyCode(code), Mods: platform.Modifiers(mods)})
}

// CodeUp lets go of a key CodeDown pressed: mods are the modifiers still
// held.
func (t *Tester) CodeUp(mods Modifiers, code KeyCode) {
	t.send(platform.SurfaceEvent{Kind: platform.RawKeyReleased, Code: platform.KeyCode(code), Mods: platform.Modifiers(mods)})
}

// TypeKey presses a key that types text, as a keyboard does: the key goes
// down, the text comes, then the key goes up.
func (t *Tester) TypeKey(mods Modifiers, key Key, text string) {
	t.send(platform.SurfaceEvent{Kind: platform.KeyPressed, Key: platform.Key(key), Mods: platform.Modifiers(mods)})
	t.send(platform.SurfaceEvent{Kind: platform.TextInput, Text: text})
	t.send(platform.SurfaceEvent{Kind: platform.KeyReleased, Key: platform.Key(key), Mods: platform.Modifiers(mods)})
}

// Type types text into the focused text input.
func (t *Tester) Type(s string) {
	t.send(platform.SurfaceEvent{Kind: platform.TextInput, Text: s})
}

// SetFocused gives the window the keyboard, or takes it away, as when the
// user switches to another window.
func (t *Tester) SetFocused(focused bool) {
	kind := platform.SurfaceBlur
	if focused {
		kind = platform.SurfaceFocus
	}
	t.send(platform.SurfaceEvent{Kind: kind})
}

// Compose shows text as an input method's composition, with its caret at
// rune caret, as while typing in Chinese or Japanese; an empty text ends
// it. Type commits the text. Nothing is composed while the focused element
// turned input methods off (InputMethod), as on the desktop.
func (t *Tester) Compose(text string, caret int) {
	if t.h.ime.Active && t.h.ime.NoInputMethod {
		return
	}
	t.send(platform.SurfaceEvent{Kind: platform.TextComposition, Text: text, Caret: caret})
}

// Command performs an edit command of the menus, as Copy of the Edit menu:
// "copy", "cut", "paste", "pasteAndMatchStyle", "selectAll", "undo",
// "redo" or "delete", or a Format menu's, as "bold" or "alignCenter".
func (t *Tester) Command(name string) {
	t.send(platform.SurfaceEvent{Kind: platform.SurfaceCommand, Text: name})
}

// TextCaret returns where input methods compose, and whether one would:
// the caret of the focused text input, or of an element's TextCaret, and
// not while it turned them off (InputMethod).
func (t *Tester) TextCaret() (Rect, bool) {
	c := t.h.ime.Caret
	return Rect{float32(c.X), float32(c.Y), float32(c.W), float32(c.H)}, t.h.ime.Active && !t.h.ime.NoInputMethod
}

// TextInputActive reports whether the focused element takes text, typed
// or composed (HandleTextInput, TextCaret), and whether it turned input
// methods off (InputMethod), as the window tells the system.
func (t *Tester) TextInputActive() (active, noInputMethod bool) {
	return t.h.ime.Active, t.h.ime.NoInputMethod
}

// Clipboard returns the text the view copied; SetClipboard sets the text
// it pastes.
func (t *Tester) Clipboard() string { return t.h.clipboard }

// SetClipboard sets the clipboard's text.
func (t *Tester) SetClipboard(s string) { t.h.clipboard, t.h.clipboardData = s, transfer.Data{} }

// ClipboardData returns what the view copied with Services.WriteClipboardData,
// or the text it copied; SetClipboardData puts data on the clipboard, as
// another app copies HTML.
func (t *Tester) ClipboardData() transfer.Data { return t.h.readClipboardData(nil) }

func (t *Tester) SetClipboardData(d transfer.Data) {
	t.h.clipboardData = d.Snapshot()
	b, _ := d.Read(transfer.Text)
	t.h.clipboard = string(b)
}

// Cursor returns the pointer shape the view shows.
func (t *Tester) Cursor() Cursor { return t.h.cursor }

// OpenedURLs returns the links the view opened.
func (t *Tester) OpenedURLs() []string { return t.h.opened }

// FailClipboard makes the view's writes of text to the clipboard from now
// on fail with err, copying nothing, as a browser refuses them without
// the user acting (Context.WriteClipboardThen); nil lets them through
// again.
func (t *Tester) FailClipboard(err error) { t.h.clipboardErr = err }

// FailOpenURL makes the links the view opens from now on fail with err, as
// those no app opens, or opens them again for nil.
func (t *Tester) FailOpenURL(err error) { t.h.openErr = err }

// Announcements returns what the view asked screen readers to read out
// (Context.Announce), as a Router the titles of the pages it showed, and
// forgets it.
func (t *Tester) Announcements() []string {
	a := t.h.announced
	t.h.announced = nil
	return a
}

// Focused reports whether the element showing text or labeled s, or its
// container, has the keyboard focus.
func (t *Tester) Focused(s string) bool {
	for _, n := range t.rt.labels {
		if n.text != s {
			continue
		}
		for id := n.id; id != 0; {
			if id == t.rt.focused {
				return true
			}
			st := t.rt.states[id]
			if st == nil {
				break
			}
			id = st.parent
		}
	}
	return false
}
