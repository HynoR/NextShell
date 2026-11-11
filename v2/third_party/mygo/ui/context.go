package ui

import (
	"fmt"
	"hash/maphash"
	"reflect"
	"time"

	"github.com/egoist/mygo/internal/scene"
	"github.com/egoist/mygo/internal/text"
	"github.com/egoist/mygo/transfer"
)

// Context builds a window's user interface for one frame. The window's
// view function receives it on the main thread; it is only valid during
// that call. A frame may rebuild the view in several passes; neither a
// Context nor its Elements may be saved for a later call.
type context struct {
	rt     *engine
	parent *node
	root   *node
	chunks [][]node
	used   int
	// dirty is how many elements in the arena have held references since
	// finish last cleared the unused ones, including earlier build passes.
	dirty int
	// theme is the theme the widgets being built take: the window's, or
	// that of the Themed whose children are building. window is the
	// window's, which the root has (SetTheme).
	theme    *Theme
	window   *Theme
	now      time.Time
	w, h     float32
	titleBar TitleBar
	vibrancy bool
	overlay  *node
	nextKey  any
	keySet   bool
	// tree is the Tree being built, for its items.
	tree *treeBuild
	// buttons is how the buttons being built look, in a toolbar or a
	// group of toggles.
	buttons buttonStyle
	// form and accordion are the Form and the Accordion being built, for
	// their fields and sections.
	form      *formBuild
	accordion *accordionParts
	// checks is the CheckboxGroup being built.
	checks *checkGroup
	// row is the row of a List or a Table being built, and sidebar the
	// Sidebar.
	row     *rowBuild
	sidebar *sidebarBuild
	// reveal lists the elements to scroll into view (ScrollIntoView).
	reveal []nodeReveal
	// router is the Router whose page is being built, for Links; routers
	// counts the Routers built, the first of which takes the window's
	// keys for going back and forward. inert is set while a page going
	// away is built, whose shortcuts wait.
	router  *Router
	routers int
	inert   bool
	// spare are the chunks of the frame before, which the engine keeps
	// while elements of that frame may leave with an exit transition, to
	// copy them (engine.exitsBuilt).
	spare      [][]node
	spareDirty int
	// transitions are the elements given a Transition, in the order asked,
	// and dividers those drawing lines between their children.
	transitions []transitionUse
	dividers    []dividers
	// childExits are the Exits that elements gave their children going
	// in this frame (ChildExit).
	childExits []childExit
	// sortedUses and depthStarts are byDepth's.
	sortedUses  []transitionUse
	depthStarts []int
}

const chunkSize = 32

// alloc returns a zeroed element from the frame's arena.
func (c *context) alloc() *node {
	ci, ei := c.used/chunkSize, c.used%chunkSize
	if ci == len(c.chunks) {
		c.chunks = append(c.chunks, make([]node, chunkSize))
	}
	c.used++
	e := &c.chunks[ci][ei]
	shadows, cols, rows, frags := e.shadows[:0], e.cols[:0], e.rows[:0], e.frags[:0]
	*e = node{}
	e.shadows, e.cols, e.rows, e.frags = shadows, cols, rows, frags
	e.serial = int32(c.used)
	e.epoch = c.rt.epoch
	e.shrink = 1
	e.justify, e.align, e.self, e.alignContent = alignAuto, alignAuto, alignAuto, alignAuto
	e.justifyItems, e.justifySelf = alignAuto, alignAuto
	return e
}

func (c *context) reset(now time.Time, w, h float32) {
	for _, f := range c.rt.focusFields {
		clear(f.members)
		f.members = f.members[:0]
	}
	c.rt.epoch++
	c.rt.beginInputs()
	if uint32(c.rt.epoch) == 0 {
		c.rt.arena.rt = nil
		c.rt.arena = &elementOwner{rt: c.rt}
	}
	c.rt.arena.generation = uint32(c.rt.epoch)
	c.keySet, c.nextKey = false, nil
	clear(c.rt.parts)
	c.rt.parts = c.rt.parts[:0]
	clear(c.rt.afterInputs)
	c.rt.afterInputs = c.rt.afterInputs[:0]
	clear(c.rt.afterBuild)
	c.rt.afterBuild = c.rt.afterBuild[:0]
	clear(c.rt.inputs)
	c.rt.inputs = c.rt.inputs[:0]
	c.rt.building = 0
	clear(c.rt.actions)
	c.rt.actions = c.rt.actions[:0]
	c.dirty = max(c.dirty, c.used)
	c.used = 0
	c.now = now
	c.w, c.h = w, h
	c.theme = c.rt.defaultTheme()
	c.window = c.theme
	c.rt.lang = c.rt.host.locale()
	c.tree = nil
	clear(c.reveal)
	c.reveal = c.reveal[:0]
	clear(c.transitions)
	c.transitions = c.transitions[:0]
	clear(c.sortedUses)
	c.sortedUses = c.sortedUses[:0]
	clear(c.dividers)
	c.dividers = c.dividers[:0]
	clear(c.childExits)
	c.childExits = c.childExits[:0]
	c.router, c.routers, c.inert = nil, 0, false
	root := c.alloc()
	root.c = c
	root.id = 1
	root.kind = kindBox
	root.width, root.height = px(w), px(h)
	root.st = c.rt.stateFor(root.id)
	root.bg = c.theme.Background
	root.ts = textStyle{set: setColor | setSize | setFamily, color: c.theme.Text, size: c.theme.FontSize, family: c.theme.Font}
	if c.rt.lang != "" {
		// Text is in the language the app shows unless told otherwise.
		root.ts.lang, root.ts.set = c.rt.lang, root.ts.set|setLang
	}
	if c.rt.direction() {
		root.dir = dirRTL
	}
	c.root = root
	c.parent = root
	c.overlay = nil
}

// finish frees references to elements left out of the frame, including
// those from an earlier pass. Exit transitions have copied what they
// need from the spare arena by now. Keep one empty chunk for growth, so
// that a few rows entering and leaving a list do not allocate every frame.
func (c *context) finish() {
	c.chunks = trimArena(c.chunks, c.used, max(c.dirty, c.used))
	// While this frame built an exit transition, the spare chunks are the
	// arena the next frame builds into: keep as many as this frame used,
	// only cleared.
	keep := 0
	if c.rt.exitsBuilt {
		keep = c.used
	}
	c.spare = clearArena(c.spare, c.spareDirty, keep)
	c.dirty, c.spareDirty = c.used, 0
}

// clearArena drops what the first dirty elements reference, and keeps the
// storage of keep elements and one chunk more.
func clearArena(chunks [][]node, dirty, keep int) [][]node {
	n := min(len(chunks), (keep+chunkSize-1)/chunkSize+1)
	for ci := 0; ci < n && ci*chunkSize < dirty; ci++ {
		clear(chunks[ci][:min(chunkSize, dirty-ci*chunkSize)])
	}
	clear(chunks[n:])
	return chunks[:n]
}

func trimArena(chunks [][]node, used, dirty int) [][]node {
	n := (used + chunkSize - 1) / chunkSize
	// Only elements used since the last cleanup can retain anything.
	// Clear at most the two chunks kept; the rest give up their storage.
	for i, end := used, min(dirty, (n+1)*chunkSize); i < end; {
		ci, ei := i/chunkSize, i%chunkSize
		to := min(chunkSize, end-ci*chunkSize)
		clear(chunks[ci][ei:to])
		i = ci*chunkSize + to
	}
	if n < len(chunks) {
		clear(chunks[n+1:])
		chunks = chunks[:n+1]
	}
	return chunks
}

// overlayID is the ID of the layer of overlays.
var overlayID = mix(1, 0x6f7665726c6179)

// overlayRoot returns the layer above the window's content that Overlay
// builds into; the frame adds it to the root once the view returns.
func (c *context) overlayRoot() *node {
	if c.overlay == nil {
		o := c.alloc()
		o.c = c
		o.id = overlayID
		o.kind = kindBox
		o.flags = flagAbsolute | flagPassThrough
		o.dir = c.root.dir
		o.inset = [4]length{px(0), px(0), px(0), px(0)}
		o.depth = 1
		o.st = c.rt.stateFor(o.id)
		c.overlay = o
	}
	return c.overlay
}

// newElement creates an element of kind as the last child of the current
// parent.
func (c *context) newElement(k kind) *node {
	if c.parent == nil {
		panic("ui: element created outside of a view function")
	}
	if c.parent.kind == kindText && k != kindText {
		panic("ui: only text elements (Text, Link, RichText) go inside a text")
	}

	e := c.alloc()
	e.c = c
	e.kind = k
	p := c.parent
	e.id = mix(p.id, uint64(p.nchild)+uint64(k)<<56)
	e.dir = p.dir &^ dirSet
	keyed := c.keySet
	if c.keySet {
		e.id = keyedID(p.id, c.nextKey)
		e.key = c.nextKey
		c.keySet, c.nextKey = false, nil
	}
	p.add(e)
	var built bool
	e.st, built = c.rt.lookState(e.id)
	if keyed && built {
		c.rt.duplicateKey(e.id, e.key)
	}
	if c.rt.insp.open && e.id == c.rt.insp.selected {
		c.rt.insp.noteSource()
	}
	return e
}

var keySeed = maphash.MakeSeed()

func (c *context) rekey(e *node, k any) {
	parent := uint64(0)
	if e.parent != nil {
		parent = e.parent.id
	}
	id := keyedID(parent, k)
	rt := c.rt
	if id != e.id {
		rt.unlook(e.st)
	}
	st, built := rt.lookState(id)
	if built && id != e.id {
		// Another element of the pass has the key under the same parent.
		rt.duplicateKey(id, k)
	}
	e.id, e.st, e.key = id, st, k
	if rt.insp.open {
		rt.insp.noteKey(id, k)
		if id == rt.insp.selected {
			rt.insp.noteSource()
		}
	}
}

// keyedID returns the ID of the child of parent keyed by k.
func keyedID(parent uint64, k any) uint64 {
	var h uint64
	switch k := k.(type) {
	case string:
		h = maphash.String(keySeed, k)
	// Integers of different types are different keys, as in a map.
	case int:
		h = uint64(k)*0x9E3779B97F4A7C15 + 1
	case int64:
		h = uint64(k)*0x9E3779B97F4A7C15 + 2
	case uint64:
		h = k*0x9E3779B97F4A7C15 + 3
	default:
		h = keyHash(k)
	}
	return mix(parent, h^0xA5A5A5A5A5A5A5A5)
}

// keyHash hashes a key as Go compares it, as a map's key: a pointer by the
// object it points to, whatever the object holds, a struct or an array by
// its fields or elements, and keys of different types apart. A key Go
// cannot compare, such as a slice, is hashed by its value printed, and so
// is a key not equal to itself, as a NaN, which a map would hash anew
// each time.
func keyHash(k any) (h uint64) {
	printed := func() uint64 { return maphash.String(keySeed, fmt.Sprintf("%T:%v", k, k)) }
	defer func() {
		if recover() != nil {
			h = printed()
		}
	}()
	if k != k { // panics for a key Go cannot compare
		return printed()
	}
	// One instance of Comparable for both: another, for reflect.Type,
	// would grow every native app by 16 KB.
	return maphash.Comparable[any](keySeed, k) ^ maphash.Comparable[any](keySeed, reflect.TypeOf(k))*0x9E3779B97F4A7C15
}

// mix combines two hashes (the finalizer of splitmix64).
func mix(a, b uint64) uint64 {
	h := a*0x9E3779B97F4A7C15 ^ (b + 0x632BE59BD9B4E019)
	h ^= h >> 30
	h *= 0xBF58476D1CE4E5B9
	h ^= h >> 27
	h *= 0x94D049BB133111EB
	h ^= h >> 31
	return h
}

// Theme returns the theme of the frame: the light or dark theme following
// the system's appearance, unless SetTheme replaced it. While the children
// of a Themed build, it is the Themed's theme.
func (c *context) Theme() *Theme { return c.theme }

// SetTheme makes the frame use t, for the window's root and the widgets
// created after the call. Inside the children of a Themed, which keep its
// theme, t is for the root and the widgets after the Themed.
func (c *context) SetTheme(t *Theme) {
	if c.theme == c.window {
		c.theme = t
	}
	c.window = t
	c.root.bg = t.Background
	c.root.ts.color, c.root.ts.size, c.root.ts.family = t.Text, t.FontSize, t.Font
}

// Themed creates a column whose children build with t, as SetTheme has
// the whole window build: the widgets inside take their colors and metrics
// (Spacing, Radius, FontSize) from t, Context.Theme returns t while they
// build and Painter.Theme while they paint, and the popovers, menus,
// tooltips and dialogs they open take t too. The column paints t's
// Background, and the text inside takes t's Text color, FontSize and Font,
// as the root has the window's theme's; Background(ui.Transparent) keeps
// what is behind it instead. The root, and what is built after the column,
// keep the window's theme. Light beside dark, as a theme picker previews
// them:
//
//	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
//		for _, t := range []*ui.Theme{ui.LightTheme(), ui.DarkTheme()} {
//			ui.Themed(c, t).Grow(1).Padding(16).Gap(8).Children(func() {
//				ui.Text(c, "Preview")
//				ui.Button(c, "OK")
//			})
//		}
//	})
//
// A nil t makes it a Column.
func coreThemed(c *context, t *Theme) *node {
	e := coreColumn(c)
	if t != nil {
		e.Background(t.Background)
		e.themeWith(t)
	}
	return e
}

// themeWith has e build its children (Children) and paint them
// (Painter.Theme) with t, and the text inside take t's color, size and
// font where e sets none, as the root takes the window's theme's.
func (e *node) themeWith(t *Theme) {
	e.themed = t
	ts := &e.ts
	if ts.set&setColor == 0 {
		ts.color = t.Text
	}
	if ts.set&setSize == 0 {
		ts.size = t.FontSize
	}
	if ts.set&setFamily == 0 {
		ts.family = t.Font
	}
	ts.set |= setColor | setSize | setFamily
}

// theme returns the theme e was built with: that of the innermost Themed
// around it, else the window's.
func (e *node) theme() *Theme {
	for n := e; n != nil; n = n.parent {
		if n.themed != nil {
			return n.themed
		}
	}
	return e.c.theme
}

// Size returns the size of the window's content in DIPs.
func (c *context) Size() (width, height float32) { return c.w, c.h }

// TitleBar is the room the window controls take at the top of a window
// with a hidden title bar, in DIPs: they sit in a band of Height along the
// top edge, Left wide from the left edge and Right wide from the right.
type TitleBar struct{ Height, Left, Right float32 }

// TitleBar returns the room the window controls take in a window with a
// hidden title bar (mygo.WindowOptions.TitleBarStyle), whose view draws
// the title bar under them: the traffic lights on macOS, the window
// buttons on Linux and Windows. It is zero in other windows and in full
// screen, where the controls hide. Keep clear of the controls, and let the
// title bar drag the window:
//
//	bar := c.TitleBar()
//	ui.Row(c).Height(max(bar.Height, 32)).Padding(0, bar.Right, 0, bar.Left).DragWindow()
func (c *context) TitleBar() TitleBar { return c.titleBar }

// Vibrancy reports whether the window shows its material
// (mygo.WindowOptions.Vibrancy) wherever the view draws no background: on
// macOS, and on Windows 11 22H2 and later in a window created with one.
// Elsewhere, what the view leaves transparent shows no material: draw a
// background there instead. The root draws the theme's background unless
// told otherwise; a sidebar over the material, beside opaque content:
//
//	if c.Vibrancy() {
//		c.Root().Background(ui.Transparent)
//	}
//	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
//		sidebar := ui.Column(c).Width(240)
//		if !c.Vibrancy() {
//			sidebar.Background(c.Theme().Surface)
//		}
//		sidebar.Children(func() { app.sidebar(c) })
//		ui.Column(c).Grow(1).Background(c.Theme().Background).Children(func() { app.content(c) })
//	})
func (c *context) Vibrancy() bool { return c.vibrancy }

// WindowActive reports whether the window is drawn as active: the window
// the user's input goes to, as native apps draw it, while it or one of its
// flyouts has the keyboard (and always as Window.SetActiveAppearance keeps
// it). The built-in widgets fade their accent to gray in a window that is
// not, as AppKit's do (Theme.InactiveAccent), and macOS fades the
// window's material. A frame that reads it is drawn again as it changes;
// draw what shows which window has the user's input in a view of your own
// with it:
//
//	icon := c.Theme().Accent
//	if !c.WindowActive() {
//		icon = c.Theme().TextMuted
//	}
func (c *context) WindowActive() bool { return c.rt.windowActive() }

// emphasized reports whether the widgets show what is selected and the
// primary action in t's accent: in a window drawn as active, and in every
// window when t keeps it (Theme.InactiveAccent).
func (c *context) emphasized(t *Theme) bool {
	return t.InactiveAccent == t.Accent || c.rt.windowActive()
}

// accent returns the color the widgets fill what is checked or filled
// with: t's accent in a window drawn as active, else what it fades to.
func (c *context) accent(t *Theme) Color {
	if c.emphasized(t) {
		return t.Accent
	}
	return t.inactiveAccent()
}

// Now returns the time the frame started, for animations.
func (c *context) Now() time.Time { return c.now }

// Root returns the element holding the window's content: a column the
// size of the window.
func (c *context) Root() *node { return c.root }

// Invalidate asks for another frame. It is safe from any goroutine, for
// state that changed outside of the window's events.
func (c *context) Invalidate() { c.rt.host.invalidate() }

// AnimationFrame asks for another frame as soon as the display can show
// it, for something moving. Call it in every frame while it moves. A
// drawing that moves while the layout stays asks with
// Painter.AnimationFrame instead, whose frames do not build the view.
func (c *context) AnimationFrame() { c.rt.animating = true }

// DisableAnimations makes the values that animate in this frame
// (AnimateTo, Animate, Spring), the element transitions and the pages of
// routers go to their targets at once, as SwiftUI's disablesAnimations:
// for a change that should not show as motion, as restoring a window's
// state or jumping to a search result. Call it before building what
// changes. Keyframes and timelines, which play on their own rather than
// towards a target, go on as they were: Seek a Timeline to jump it.
func (c *context) DisableAnimations() { c.rt.instantFrame = c.rt.frame }

// instant reports whether this frame shows changes at once
// (DisableAnimations).
func (rt *engine) instant() bool { return rt.instantFrame == rt.frame && rt.frame != 0 }

// Instant reports whether this frame shows animated values at once
// (DisableAnimations). Views read it to render a change settled in the same
// frames where its animations are disabled.
func (c *context) Instant() bool { return c.rt.instant() }

// After asks for another frame after d, for something that changes with
// time, such as a clock.
func (c *context) After(d time.Duration) { c.rt.scheduleAt(c.now.Add(d)) }

// ReadClipboard returns the text on the clipboard, and WriteClipboard
// puts text there, for widgets that copy and paste themselves. Call them
// on the main thread: in the view, or in an input handler.
// WriteClipboardThen tells whether the text got there.
func (c *context) ReadClipboard() string   { return c.rt.host.readClipboard() }
func (c *context) WriteClipboard(s string) { c.rt.host.writeClipboard(s) }

// WriteClipboardThen puts text on the clipboard as WriteClipboard does,
// and done, unless nil, gets what came of it a moment later, before a
// frame builds anew with what it changed, as navigator.clipboard's
// writeText resolves or rejects: an error when the text did not get
// there, as when another app holds the clipboard, or a browser refuses
// a write the user did not act for (a press or a key, a moment before).
// A copy button shows "Copied" or "Failed" with it:
//
//	if ui.Button(c, "Copy").Clicked() {
//		c.WriteClipboardThen(link, func(err error) {
//			app.copied, app.copyFailed = err == nil, err != nil
//		})
//	}
func (c *context) WriteClipboardThen(s string, done func(err error)) {
	rt := c.rt
	if done == nil {
		rt.host.writeClipboard(s)
		return
	}
	rt.host.writeClipboardThen(s, func(err error) {
		done(err)
		rt.requestFrame()
	})
}

// Announce asks screen readers to read text out once, after what they
// are reading, for news the keyboard focus does not bring, as a search
// done or a file saved: a Router announces the title of a page it shows,
// and a toast its text. It does nothing while no assistive technology
// reads the window.
func (c *context) Announce(text string) {
	if text != "" && !c.inert {
		c.rt.announcements = append(c.rt.announcements, text)
	}
}

// OpenURL opens a URL in the default browser, or the app registered for
// its scheme, as a Link does. It returns at once; OpenURLThen tells what
// came of it.
func (c *context) OpenURL(url string) { c.rt.host.openURL(url, nil) }

// OpenURLThen opens a URL as OpenURL does, and done, unless nil, gets what
// came of it in a while, before a frame builds anew with what it changed:
// an error when no app could open the URL.
//
//	c.OpenURLThen(url, func(err error) {
//		if err != nil {
//			app.failed = url
//		}
//	})
func (c *context) OpenURLThen(url string, done func(err error)) {
	rt := c.rt
	var then func(error)
	if done != nil {
		then = func(err error) {
			done(err)
			rt.requestFrame()
		}
	}
	rt.host.openURL(url, then)
}

// Shortcut reports whether the key with exactly the modifiers mods was
// pressed, wherever the keyboard focus is, unless a focused element
// handled it first: a focused button or link takes Enter and Space, and a
// check box, switch or radio button Space, so that Enter can press a
// dialog's default button.
func (c *context) Shortcut(mods Modifiers, key Key) bool {
	if !c.insideModal() || c.inert {
		return false // behind a dialog, or in a page going away
	}
	return c.rt.shortcut(0, mods, key)
}

// ShortcutRepeated reports whether the key that the last Shortcut to
// report one took came as the auto-repeat of a key held down, as the web's
// KeyboardEvent.repeat: Context's, an Element's, a list's or an overlay's,
// and those that run OnShortcut's actions, which can ask it as they run.
// An action that steps while the key is held takes every key; a toggle
// takes the press alone, and lets the repeats go by:
//
//	if c.Shortcut(0, ui.KeyT) && !c.ShortcutRepeated() {
//		app.panel = !app.panel
//	}
//
// It is false in a frame no shortcut took a key in yet.
func (c *context) ShortcutRepeated() bool { return c.rt.shortcutRepeat }

// Local returns state of type T that element e keeps from frame to frame,
// keyed by key among its states; init creates it the first time. Custom
// widgets keep what they need with it, on the element they create.
//
// The element owns what init made. When a frame no longer builds it, a
// value that is an io.Closer (T or *T has Close() error) is closed once,
// on the main thread, after that frame, as the web aborts an
// AbortController as an element is removed: a component cancels in Close
// the work it started, as a streaming request, a ticker or a subscription:
//
//	s := ui.Local(e, "stream", func() *stream { return startStream(url) })
//
// An element leaving with an exit transition closes them once its exit
// ended, and every element as its window closes. A nil value is not
// closed, and Close's error is ignored. Keep a value the element must not
// close, as one the app shares, in the app's state.
func coreLocal[T any](e *node, key any, init func() T) *T {
	v, made := keepLocal(e.st, key, init)
	if made {
		closeWithState(e.st, v)
	}
	return v
}

// keepLocal is Local without the element owning what init made, for
// values of the app the toolkit keeps a copy of (a select's value shown),
// which it must not close. made tells that init made it.
func keepLocal[T any](st *state, key any, init func() T) (v *T, made bool) {
	if st.locals == nil {
		st.locals = map[any]any{}
	}
	if v, ok := st.locals[key]; ok {
		if p, ok := v.(*T); ok {
			return p, false
		}
	}
	v = new(T)
	*v = init()
	st.locals[key] = v
	return v, true
}

// state is what the runtime keeps about an element from frame to frame.
type state struct {
	rt            *engine
	pendingSubmit bool
	pendingChange bool
	noticeQueued  bool

	// rowPressed marks a row of a list the pointer pressed, until the
	// release; pressChose, that the press chose it, so that its click does
	// not choose again.
	rowPressed, pressChose bool

	// hitFn is where in its box the element takes the pointer (HitTest).
	hitFn func(x, y float32, r Rect) bool

	world scene.Affine
	clips bool
	// lifted tells that it was an element flying with a Match, which
	// takes the pointer where it paints: over the rest of its layer, out
	// of its ancestors' clips (matches.lifted).
	lifted bool
	// invisible tells that the element showed nothing in the last frame,
	// wherever it was: invisible, inert or hidden (InViewMargin).
	invisible bool
	// rtl tells that the element was laid out right to left: a scroll
	// container's horizontal offset counts from the right, and its
	// vertical scroll bar is on the left.
	rtl bool
	// placed is where AttachTo put the element in the last frame
	// (AttachedAnchors), asked where it was asked to.
	placed, asked attachment

	clipRect  Rect
	clipRadii [4]float32
	// selfClips tells that the element clips itself and what is inside it
	// to selfClip, rounded by selfRadius (ClipInset).
	selfClips  bool
	selfClip   Rect
	selfRadius float32
	id         uint64
	seen       uint64
	pass       int    // the pass of the frame that built it last
	born       uint64 // the frame that first built the element
	// lookedSeen and lookedPass are seen and pass before the last
	// lookup (lookState), 0 for a state it created.
	lookedSeen uint64
	lookedPass int
	// lay is what the element's last layout was (layoutcache.go), and
	// text what it measured and laid out of its text (heldText).
	lay  layoutRecord
	text *heldText
	// The element's box and its visible part in the last frame.
	x, y, w, h     float32
	vx, vy, vw, vh float32
	parent         uint64
	flags          uint32
	// ignoreInput tells that the element, or one around it, ignores the
	// pointer and the keyboard (IgnoreInput); accessHidden, that it is
	// out of what assistive technology reads (AccessHidden, or inert);
	// accessDisabled, that assistive technology reads it as unavailable
	// (AccessDisabled).
	ignoreInput, accessHidden, accessDisabled bool
	// activeDescendant is the element that has the keyboard focus while
	// this one does (ActiveDescendant), 0 for none.
	activeDescendant uint64
	cursor           Cursor
	// tip marks an element with a tooltip (TooltipBase), which shows once
	// the pointer has rested on it for tipDelay, or at once within tipWarm
	// of another going: its theme's TooltipDelay.
	tip               bool
	tipDelay, tipWarm time.Duration
	// effectLayer is what the element and its children render into for
	// its Effect, its blur, its Cache or its opacity as a group
	// (paintGroup), kept from frame to frame. grouped tells that, faded
	// with what is inside it, what it painted last overlapped, so that it
	// paints into the layer at once rather than flat first (checkFlat).
	effectLayer *Canvas
	grouped     bool
	// ink is what the element and those inside it painted into its layer
	// in frame inkFrame, in its own space, and lastInk what they painted
	// in a frame before (keepInk): the layer of a frame holds lastInk, as
	// far as a blur or a shadow inside it reached, the same however many
	// times the frame paints.
	ink, lastInk Rect
	inkFrame     uint64
	// webView is the web view the element shows (WebView).
	webView NativeWebView

	clicks, rightClicks, doubleClicks int
	// pressMods are the modifiers held as the pointer went down on the
	// element, and clickMods those of its last click.
	pressMods, clickMods Modifiers
	// typed is what was typed to choose a row of a list taking the focus
	// for them since typedAt, and typing reports more since the last
	// frame.
	typed   string
	typedAt time.Time
	typing  bool
	// press is where the pointer went down in the element; dragX and dragY
	// add up its moves since the last frame.
	pressX, pressY float32
	dragX, dragY   float32
	pressed        bool
	// pressButton is the button pressing the element: 0 the primary, 1
	// the secondary, 2 the middle (pressedPrimary).
	pressButton int
	// pressPointer and pressPressure are the device pressing the element
	// and its pressure as of the press's last move (PressedPointer).
	pressPointer  PointerType
	pressPressure float32
	// drag follows the press's moves; released tells that it ended since
	// the last frame, going at releaseVX and releaseVY in the element's
	// axes, releaseWX and releaseWY in the window's, and canceled that it
	// ended without the pointer letting go (PressCanceled).
	drag                 dragTrack
	released, canceled   bool
	releaseVX, releaseVY float32
	releaseWX, releaseWY float32
	pressPending         bool // the press began since the last build pass
	// releaseLater is what the release of the press gives the element,
	// kept when it came with moves no pass had read for the pass that
	// reads them to apply as it ends (forgetInput): where the pointer let
	// go, which the element sees as the pointer meanwhile (pointer),
	// whether it ended a drag or was canceled, and the clicks since, the
	// release's own first (click).
	releaseLater struct {
		due, released       bool
		canceled            bool
		x, y                float32
		clicks, rightClicks int
		clickMods           Modifiers
	}
	// dragRec is what Draggable keeps of the element from frame to frame.
	dragRec *dragRec

	// scrollX and scrollY are float64, as the content a List scrolls
	// may be taller than float32 counts to a fraction of a DIP.
	scrollX, scrollY float64
	contentW         float64
	contentH         float64
	// barInset is the element's ScrollbarInsets, and barWidth the width
	// of its scroll bars, its theme's ScrollbarWidth.
	barInset [4]float32
	barWidth float32
	// barsShown reports whether the last frame painted the element's
	// scroll bars: they show only while it is hovered or a bar is
	// dragged, unless it keeps them (KeepScrollbars).
	barsShown, hideScrollbars bool
	// track is the ScrollState of the last frame's element, which events
	// that scroll it update (scrollTo).
	track *ScrollState
	// wheel is the notches of a mouse wheel on their way through the
	// content (wheelScroll).
	wheel *wheelGlide
	// snap is where a container that snaps comes to rest (ScrollSnap),
	// which its layout sets.
	snap snapper
	// startX and startY are the offset before frame moveFrame moved it.
	moveFrame      uint64
	startX, startY float64
	// list is the ListState that placed the rows of a List last.
	list *ListState
	// cx, cy, cw and ch are the element's content box, inside its
	// padding, in the last frame.
	cx, cy, cw, ch float32

	changed, submitted bool
	// Queries and callbacks observe input in the pass that applied it.
	changeDelivered, submitDelivered bool
	// submitMods are the modifiers held with the Enter submitting a text
	// input, as Shift going back in a find bar.
	submitMods Modifiers
	// expand is what assistive technology asked of an item of a tree: 1
	// to open it, -1 to close it. focusAsked is set as it focuses an
	// option of a select (focusHighlights, of the last frame), which
	// the select highlights as it builds it.
	expand                      int8
	focusHighlights, focusAsked bool
	// role is the element's Role in the last frame.
	role Role
	// holding is set while a stepper's arrow is held, since holdStart, and
	// holdSteps counts the steps it took.
	holding   bool
	holdStart time.Time
	holdSteps int
	dropped   []string
	// dragValue is the value the element drags (Drag), or dragFn returns
	// it as the drag starts; accepts reports whether it takes a value
	// dragged (Drop), and droppedValue is the value dropped on it, while
	// hasDropped, at (dropX, dropY). dragMissed is set on the source when
	// its value was let go over no element taking it, at (missX, missY).
	dragValue    any
	dragFn       func() any
	accepts      func(any) bool
	droppedValue any
	hasDropped   bool
	dropX, dropY float32
	dragMissed   bool
	missX, missY float32
	dataSource   *dataSource
	dataTarget   *transfer.DropOptions
	dataDropped  *transfer.Drop
	editor       *editor
	// textScope is the Selectable container this paragraph belongs to.
	textScope uint64
	// spans keeps what a text made of its spans in the last frame.
	spans        *spanCache
	valueBinding any
	locals       map[any]any
	anims        map[any]*anim
	motions      map[any]*channel
	colorMotions map[any]*colorMotion
	crec         *contentRec
	pose         *poseRec
	sequences    map[any]*sequence
	timelines    map[any]*timelineState
	shortcuts    []shortcut
	delivered    []shortcut

	// input, caret and takesText are those of the last frame's element
	// (HandleInput, TextCaret).
	input       func(InputEvent) bool
	observe     func(InputEvent) // ObserveInput
	textClient  TextInputClient
	textAdapter *textInputAdapter
	caret       Rect
	takesText   bool
	noIME       bool
	// copyData is what a Selectable container's selection copies as
	// (CopyData).
	copyData func(parts []SelectedText) transfer.Data
	// scope is the dialog the element was in, 0 for none, and anchor the
	// element it was a popover of (AttachTo, PopoverBase).
	scope, anchor uint64
	// page is the Router's page the element was in, 0 for none.
	page uint64
	// trec is what the element's Transition keeps, by the engine's
	// transitions, when it has one.
	trec *transition
	// closes closes the values of locals that are io.Closers (Local), in
	// the order they were made; nil when none can be.
	closes []func() error
}

type shortcut struct {
	mods Modifiers
	key  Key
}

func (rt *engine) stateFor(id uint64) *state {
	s, _ := rt.lookState(id)
	return s
}

// lookState returns the state of the element id as stateFor does, and
// whether the pass built an element with that ID already.
func (rt *engine) lookState(id uint64) (s *state, built bool) {
	s = rt.states[id]
	if s == nil {
		if n := len(rt.free); n > 0 {
			// A state pruned, which nothing refers to any more.
			s = rt.free[n-1]
			rt.free[n-1] = nil
			rt.free = rt.free[:n-1]
			*s = state{id: id, born: rt.frame, text: s.text}
		} else {
			s = &state{id: id, born: rt.frame}
		}
		rt.states[id] = s
	} else {
		built = s.seen == rt.frame && s.pass == rt.pass
	}
	s.rt = rt
	s.lookedSeen, s.lookedPass = s.seen, s.pass
	s.seen, s.pass = rt.frame, rt.pass
	// What the element drags and takes, as this pass asks.
	s.dragValue, s.dragFn, s.accepts = nil, nil, nil
	s.dataSource, s.dataTarget = nil, nil
	return s, built
}

// unlook undoes the lookup of s by an element that Key gave another
// identity: creation looked up its place among its siblings, which is not
// its own. A state the lookup created goes at once, so that keyed elements
// keep none behind them, nor allocate one every frame. One that was there
// before, as that of a popover that just closed in the place, is left as
// the lookup found it: pruning retires it, giving the focus back for an
// overlay, unless another element of the pass built it.
func (rt *engine) unlook(s *state) {
	if s.lookedSeen != 0 {
		s.seen, s.pass = s.lookedSeen, s.lookedPass
		return
	}
	delete(rt.states, s.id)
	// Locals made on the element's place before Key gave it another
	// identity are the element's no more.
	rt.dropped = append(rt.dropped, s.closes...)
	*s = state{text: s.text.emptied()}
	if len(rt.free) < maxFree {
		rt.free = append(rt.free, s)
	}
}

// textSystem is the shared text system.
func textSystem() *text.System { return text.Shared() }

// sceneColor converts a color for the scene.
func sceneColor(c Color) scene.Color { return c.scene() }
