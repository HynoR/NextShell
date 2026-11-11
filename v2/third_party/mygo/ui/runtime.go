package ui

import (
	"slices"
	"sync"
	"time"
	"weak"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/scene"
	"github.com/egoist/mygo/internal/text"
	"github.com/egoist/mygo/transfer"
)

// host is where a runtime's frames go: a window's surface, or memory for
// headless rendering and tests.
type host interface {
	size() (w, h, scale float32)
	// refreshRate returns how many times a second the display refreshes,
	// 0 when unknown.
	refreshRate() float32
	// occluded reports whether nothing of the window shows, as when other
	// windows cover it.
	occluded() bool
	present(s *scene.Scene)
	requestFrame()
	// setFrameRate tells the surface how many frames a second come while
	// only drawings move, 0 for one each refresh (engine.pace).
	setFrameRate(fps float32)
	// framesAtOnce reports whether requestFrame has a frame come at once
	// rather than at the display's next refresh
	// (platform.ImmediateFrameSurface).
	framesAtOnce() bool
	// afterTimer and repaintTimer arm the timers of After and
	// Painter.After for at, or stop them for the zero time.
	afterTimer(at time.Time)
	repaintTimer(at time.Time)
	setCursor(Cursor)
	setTextInput(t platform.TextInputState)
	updateAccessibility(tree *platform.AccessTree)
	readClipboard() string
	writeClipboard(string)
	// writeClipboardThen puts text on the clipboard as writeClipboard
	// does, and done gets what came of it on the main thread, later.
	writeClipboardThen(text string, done func(error))
	// readClipboardData reads the representations of formats on the
	// clipboard, all for nil; writeClipboardData puts data there;
	// clipboardFormats lists them.
	readClipboardData(formats []transfer.Format) transfer.Data
	clipboardFormats() []transfer.Format
	writeClipboardData(transfer.Data)
	startDrag()
	startDataDrag(transfer.Data, any, transfer.DragOptions, float32, float32) error
	cancelDataDrag()
	setDropFormats([]transfer.Format)
	titleBarDoubleClicked()
	// setMaximizeButton tells where the view shows the window's maximize
	// button (MaximizeButton), the zero Rect for nowhere.
	setMaximizeButton(r Rect)
	// escape tells the window an Escape nothing handled.
	escape()
	isDark() bool
	preferences() platform.Preferences
	// locale returns the language the app shows, and region the user's
	// regional formats; they cost nothing.
	locale() string
	region() i18n.Region
	// rightToLeft reports whether the window is laid out right to left,
	// as its language reads.
	rightToLeft() bool
	titleBar() TitleBar
	// vibrancy reports whether the window shows a material where the
	// frames are transparent.
	vibrancy() bool
	// active reports whether the window is drawn as active, the window
	// the user's input goes to; it costs nothing.
	active() bool
	// invalidate asks for a frame from any goroutine.
	invalidate()
	// post runs fn on the main thread soon; it is safe from any goroutine.
	post(fn func())
	openURL(url string, done func(error))
	// popupMenu shows a context menu at (x, y) after the event being
	// handled; chosen receives the ID of the item chosen.
	popupMenu(m *platform.Menu, x, y float32, chosen func(id int))
	// placeWebViews shows the web views where the frame shows them, and
	// hides the others.
	placeWebViews(views []placedWebView)
	// nativeGlass reports whether the surface draws the system's glass.
	nativeGlass() bool
	// placeGlass shows the system's glass where the frame shows it, and
	// hides the rest.
	placeGlass(places []platform.GlassPlacement)
	// tabIntoWebView gives the page of v the keyboard as Tab does, from
	// its end when back (platform.WebView.TabInto).
	tabIntoWebView(v NativeWebView, back bool)
	// platformWebView returns the web view of the platform v is, if any.
	platformWebView(v NativeWebView) platform.WebView
}

// engine runs the user interface of one window: it builds frames with
// the view function, lays them out, paints them and routes input to the
// elements of the last frame. Main thread only, except where noted.
type engine struct {
	// inlineRects is placeInline's scratch, kept from frame to frame.
	inlineRects []text.Rect
	// byPosition holds the keys whose press stood for its physical key
	// (keyDownPhysical), until their release does the same.
	byPosition      map[Key]bool
	owner           weak.Pointer[engine]
	epoch           uint64
	parts           []any
	public          *Context
	arena           *elementOwner
	handleChecks    bool
	afterInputs     []inputAction
	afterBuild      []func() // run once the view is built (onBuilt)
	inputs          []valueInput
	inputAt         int
	afterInputAt    int
	applyingInputs  bool
	notices         []*state
	typedInputs     []*state
	focusFields     map[any]*focusField
	refs            []*refState
	actions         []action
	textInputClosed bool
	view            func(*context)
	host            host
	c               context
	text            *text.System
	scene           scene.Scene
	painter         Painter
	glyphRun        glyphRun

	// fit sizes a window that takes its content's size (fit.go), nil
	// for others.
	fit *fitter

	// contentZoom is the content's zoom (Context.SetZoom), 0 for none;
	// zoomChanged tells that it changed since the frame's size was read.
	contentZoom float32
	zoomChanged bool
	// menuPresentation is how the window shows its popup menus
	// (Context.SetMenuPresentation).
	menuPresentation MenuPresentation

	// building counts the public constructors under way: input queued
	// while more were under way than now belongs to controls built since,
	// which applyInputs finalizes (see value_input.go).
	building int32
	// waitingInputs and waitingActions are the entries of inputs and
	// afterInputs whose containers are still building.
	waitingInputs, waitingActions []int

	// measured are the last spans laid out outside elements (richParams).
	measured     [8]measuredSpans
	nextMeasured int
	paths        paths
	// pathClips are the masks of the paths clips were cut to lately, by
	// their shapes (clipMask), and of the layers of masks (Masks), and
	// maskScene the scene the layers are drawn from.
	pathClips map[uint64]*pathClip
	maskScene *scene.Scene
	svgs      svgs
	flex      flexScratch
	grid      gridScratch
	// under is the painter's stack of opaque backgrounds, and holes the
	// holes it painted, kept from one frame to the next.
	under []opaqueBox
	holes []Rect
	// culled counts the elements painting skipped (Painter.paintable),
	// unless noCull, which paints them all, as TestPaintCullMatches
	// compares.
	culled uint64
	noCull bool

	// layouts counts the frames laid out, laySeq the elements laid out
	// (layoutBox) and computed those of them not as in the frame before;
	// layGen is the text system's generation of the last layout, when
	// layGenOK. noLayoutCache lays every element out anew, measuring
	// all there is to measure and keeping nothing of their text
	// (heldText), as TestLayoutCacheMatches compares (layoutcache.go).
	layouts, laySeq uint64
	computed        uint32
	layGen          uint64
	layGenOK        bool
	noLayoutCache   bool

	states map[uint64]*state
	// free are states pruned, which new elements take: rows coming into
	// a list's view take those of rows that went out of it.
	free  []*state
	frame uint64
	// pass is the pass of the view building the frame: the last one
	// builds the elements that stay.
	pass int

	// What the last frame laid out, for input until the next one: the
	// focus order, with the scope of each element, and the dialog on top.
	hits []hit
	// liftHits gathers the hits of the elements flying as the frame
	// commits, which go over the rest of their layer, as they paint
	// (matches.paint); lifting tells that one is committing.
	liftHits []hit
	lifting  bool
	// z is what ordering children by their z-index works in (ZIndex).
	z           zScratch
	focusOrder  []uint64
	focusScopes []focusScope
	modal       uint64
	// modalLayer is the element at the top of the overlay holding the
	// dialog on top, which assistive technology sees with what is above.
	modalLayer  uint64
	commitScope focusScope
	// webPlaces are where the frame shows web views (WebView), and webOut
	// what the host got of them; webShown tells that the last frame
	// showed some.
	webPlaces []webPlace
	webOut    []placedWebView
	webShown  bool
	// glassPlaces are where the frame shows the system's glass, and
	// glassShown tells that the last frame showed some.
	glassPlaces []platform.GlassPlacement
	glassShown  bool
	// maxButton is where the frame shows the window's maximize button
	// (MaximizeButton), and maxSent what the host got of it, in the
	// window's DIPs.
	maxButton, maxSent Rect
	// The focus groups of the frame, the group of each element of the
	// focus order in one, and the element of each that had the focus
	// last.
	// clickLater are the elements the view clicked, as a toolbar's
	// overflow menu does, whose clicks the next pass sees.
	clickLater []uint64
	groups     map[uint64]groupInfo
	memberOf   map[uint64]uint64
	groupLast  map[uint64]uint64
	// focusFrom is the element that had the keyboard focus as the last
	// pass ended, 0 for none or while the window did not have the keyboard
	// (FocusLeft); leftRead tells that this pass read FocusLeft, which saw
	// the focus on leftSaw.
	focusFrom, leftSaw uint64
	leftRead           bool
	// openers are the elements that had the focus as overlays opened, by
	// overlay; downs the elements the pointer went down on since the last
	// pass (0 for none), for PressedOutside.
	openers   map[uint64]uint64
	downs     []uint64
	regs      []shortcutReg
	nextRegs  []shortcutReg
	delivered []shortcutReg
	// shortcutRepeat tells that the last key a shortcut took this frame
	// came as the auto-repeat of a key held down (ShortcutRepeated).
	shortcutRepeat bool
	// observers counts the elements the last commit found observing the
	// input in them (ObserveInput), and observeBuf is the chain observe
	// walks, kept between events.
	observers  int
	observeBuf []uint64

	// trans are the transitions of the elements given one, by ID
	// (transition.go): exitsBuilt tells that the last frame built elements
	// with an Exit or flying with a Match, whose arena the next frame
	// keeps to copy those that go, laidW and laidH are the size the last
	// frame was laid out at, and byID maps a frame's elements, for the
	// copies going. overlayExits tells that the last frame had elements
	// with an Exit in the overlay layer, or copies leaving it: the next
	// builds the layer, though nothing else does, for them to leave in.
	trans map[uint64]*transition
	// matches moves the elements of a Transition's Match, once one has
	// one (match.go).
	matches matcher
	// instantFrame is the frame that DisableAnimations shows at once.
	instantFrame uint64
	// paints counts the frames painted, those painting the last frame's
	// elements again included.
	paints uint64
	// posed are the elements of the last frame built with a pose
	// (AnimatePose), parents first.
	posed        []*node
	exitsBuilt   bool
	overlayExits bool
	laidW, laidH float32
	byID         map[uint64]*node
	// going are animateExits' copies going, in order, and holding the
	// elements appearing that wait for them (AfterExits).
	going   []exit
	holding []transitionUse
	// insp is the inspector (inspector.go); dupKeys are the duplicate keys
	// reported, and warnings what the inspector lists.
	insp inspector
	// dirOverride lays the window out left to right (1) or right to left
	// (2) whatever its language, as the inspector's toggle asks; 0 follows
	// it.
	dirOverride uint8
	dupKeys     map[uint64]bool
	warnings    []string
	// strict makes mistakes found while building panic, as duplicate keys
	// do in a Tester.
	strict bool
	// clock is a Tester's time, time.Now when nil.
	clock func() time.Time
	// frameAt is the time of the next frame, when the display asked for it
	// earlier than it draws (windowHost.waitDisplay).
	frameAt time.Time

	pointerX, pointerY float32
	pointerIn          bool
	// pointerType and pressure are the device of the last pointer event
	// and how hard it pressed (InputEvent.Pressure), buttons the buttons
	// down as the events tell, and pressureMoved that the last move
	// changed the pressure alone, which moves as the pointer moving does.
	pointerType   PointerType
	pressure      float32
	buttons       uint8
	pressureMoved bool
	hover         []uint64
	// chain is a buffer for the elements under the pointer.
	chain       []uint64
	pressed     *state
	pressButton int
	// pressClicks is the press's count of clicks, which its release
	// carries.
	pressClicks int
	// pressEnded is the element of the last press while its release, which
	// came with moves no pass read, waits for the pass that reads them
	// (releaseLater) and no press came since: that pass sees it still
	// pressed, by the button pressButton holds.
	pressEnded *state
	// pressLost is the handler of the element pressed when the frame
	// built another element without one in its place, for prune to give
	// the release to at pressLostX, pressLostY, the pointer in the axes
	// the element had.
	pressLost              func(InputEvent) bool
	pressLostX, pressLostY float32
	// downing counts the presses pointerDown is handling, and ups holds the
	// releases and cancels that came meanwhile, through a main loop a
	// handler ran nested, for it to apply once the press is recorded.
	downing int
	ups     []pendingUp
	// listMenu and listDrag are the parts of lists a right-click and a
	// drag reach, set as the first list builds, and cellClick and
	// cellInBlock the parts of tables choosing cells a click and a press
	// reach, set as the first of those builds, so apps without them do not
	// link them.
	listMenu    func(rt *engine, chain []uint64, x, y float32)
	listDrag    func(rt *engine)
	cellClick   func(f *listFrame, x, y float32, mods Modifiers)
	cellInBlock func(f *listFrame, x, y float32) bool
	// pressDragger is the element around the one pressed that drags a
	// value, which takes the press as it moves (dragMove), or 0.
	pressDragger uint64
	focused      uint64
	// texts are selectable paragraphs in build order; selection spans those
	// of one Selectable container (textselection.go).
	texts        []*state
	selection    textSelection
	focusVisible bool
	// keys are the keys pressed since the last frame, for its shortcuts;
	// retried, keys no shortcut of the frame they came to took, which the
	// shortcuts that frame built take in the next one; unrouted, those
	// the frame being built gave no shortcut, or gave one the view no
	// longer asks for (routeKeys, settleKeys).
	keys, retried []keyEvent
	unrouted      []shortcutReg
	menu          menuState
	// windowFocused tells that the window has the keyboard focus, which
	// windowFocus reads; focusRead, that the last frame read it, so that a
	// change of it draws a frame.
	windowFocused, focusRead bool
	// activeRead tells that the last frame read whether the window is
	// drawn as active (windowActive), so that a change of it draws a
	// frame.
	activeRead bool
	// keysHeld are the keys held in the window (Context.KeyDown); keysRead
	// tells that the last frame read them, so that a change of them draws
	// a frame.
	keysHeld []heldKey
	keysRead bool
	// withinRead tells that the frame read FocusWithin, which follows the
	// parents its last layout committed: the focus moving as it builds, to
	// an element it builds for the first time or after the read, draws
	// another frame.
	withinRead bool
	// toasts are the toasts the window holds, oldest first, and
	// toastList those showing, as ToastViewportBase gives them; their time
	// stops while toastsPaused, as the user holds them. A viewport of them
	// was built in the pass toastPass of the frame toastFrame.
	toasts       []toast
	nextToast    uint64
	toastList    []Toast
	toastsPaused bool
	toastFrame   uint64
	toastPass    int
	// mods are the modifiers of the last pointer event.
	mods Modifiers

	consumed bool
	// animating is set as a frame builds when something moves, for another
	// frame built anew; repainting as it paints when only drawings move
	// (Painter.AnimationFrame), for a frame painting its elements again,
	// and repaintAt to when drawings change next (Painter.After). redraw
	// tells that the next frame may paint again, as no event came and
	// nothing else asked for one since; painted is the size and scale of
	// the window, and gen the text system's Generation, as the last frame
	// was built. The host's repaintTimer asks for a frame at repaintDue,
	// unless one came or was asked for since. held tells that a frame, for
	// what moves or what changed, waits for the window to show, and shown
	// is when some of it last showed again (SurfaceShown); input tells
	// that an event other than a frame's came since the last frame built.
	animating  bool
	repainting bool
	repaintAt  time.Time
	redraw     bool
	held       bool
	shown      time.Time
	input      bool
	// hiddenAsked tells that a view asked whether the window shows
	// (WindowHidden), which builds it again as that changes.
	hiddenAsked bool
	painted     [3]float32
	gen         uint64
	// repaintRate is the most frames a second the drawings that move ask
	// for with Painter.AnimationFrameRate, repaintFull whether one asked
	// for the display's with Painter.AnimationFrame. paceEvery is how many
	// of the display's refreshes a frame that only paints again comes
	// every, 1 for each, and paceRate the frames a second the surface was
	// told of (pace); pacedAt is when the last frame began, and pacedNext
	// when the repaint timer asked for the next (framesAtOnce).
	repaintRate float32
	repaintFull bool
	// drawn are the Draw and DrawOver callbacks of the last frame painted
	// that asked to be painted again (paintPart), and untracked tells that
	// something else asked, or that painting the parts alone would not do:
	// the window is painted whole. partsBase is how much data the scene
	// held painted whole (sceneData), partOps the operations of the scene
	// painted before, and partPaints counts the frames painted in parts;
	// noParts paints every frame whole, for tests that compare. atlases
	// are the generations of the glyph atlases the scene was painted with,
	// partTransient how many masks of the atlas's transient room the parts
	// drew (text.System.TransientUses), and transient whether the rest of
	// the scene draws any.
	drawn         []paintPart
	untracked     bool
	partsBase     int
	atlases       [2]uint64
	partTransient uint64
	transient     bool
	partOps       []scene.Op
	partPaints    int
	noParts       bool
	paceEvery     int
	paceRate      float32
	pacedAt       time.Time
	pacedNext     time.Time
	// inkStale tells that elements moved since commit found their ink
	// (node.ink), as poses do in frames that only paint: painting then
	// skips none.
	inkStale   bool
	repaintDue time.Time
	// late is set when lists built elements while laying out.
	late bool
	// revealIDs are the elements to scroll into view once the frame is
	// laid out, and how.
	revealIDs []revealAsk
	wakeMu    sync.Mutex
	wakeAt    time.Time
	cursor    Cursor
	// ime is the text input state the host has, and base the rune of the
	// focused editor its text starts at.
	ime struct {
		state platform.TextInputState
		base  int
	}
	// drag is the value being dragged within the window.
	drag        *valueDrag
	incoming    *platform.DataDragEvent
	dataOver    uint64
	closed      bool
	closing     bool // the window closed during a frame, which closes the engine as it ends
	dropFormats []transfer.Format
	dropScratch []transfer.Format
	// kept are the pages of the history that Routers keep, and commitPage
	// the page around the elements being committed.
	kept       map[uint64]bool
	commitPage uint64
	// commitHidden is set while commit is inside a Hidden element.
	commitHidden bool
	// announcements are the texts for assistive technology to read out
	// (Context.Announce).
	announcements []string
	// live is what the live regions of the last frame's tree held.
	live liveState
	// stats measures frames for MYGO_FRAME_STATS, nil when it is unset.
	stats *frameStats
	// dropOver is the element files are dragged over; access is true once
	// assistive technology asked for the content, and accessNodes is how
	// many nodes the last frame's tree had.
	dropOver    uint64
	access      bool
	accessNodes int
	blinkStart  time.Time
	inFrame     bool
	dark        bool
	darkKnown   bool
	// prefs are the desktop's preferences, read once until they change.
	prefs      Preferences
	prefsKnown bool
	// lang is the language the app shows, read as each frame starts.
	lang string
	// fmtRegion and fmtLocale are the formats the controls write dates
	// in, worked out of the language and the system's formats, fmtSys
	// (context.formats); fmtCache keeps what they wrote.
	fmtKnown  bool
	fmtLang   string
	fmtSys    i18n.Region
	fmtRegion i18n.Region
	fmtLocale string
	fmtCache  map[fmtKey]string
	// theme is the default theme, which follows the appearance and the
	// preferences, made once until they change (themeOK); each pass
	// starts from a copy, passTheme, which the view may change.
	theme      Theme
	themeOK    bool
	passTheme  Theme
	collect    bool
	labels     []labelNode
	tips       tooltips
	scrollDrag scrollDrag
	// wheeling are the elements of the scroll containers that the notches
	// of a mouse wheel scroll smoothly (glideWheels).
	wheeling []uint64
	// snapping are the elements of the scroll containers that snap that
	// are on their way to rest, or wait for the user's scrolling to settle
	// (glideSnaps).
	snapping []uint64
	// dropped closes the Locals of the states the frame dropped, after it
	// (closeDropped), and exits holds those of states whose elements'
	// copies are leaving with exit transitions, until they are gone.
	dropped []func() error
	exits   []exitCloses
	// lastPress is the element of the press (id) and of the one before
	// (prev), and how many of the backend's count of clicks were before
	// the first on it.
	lastPress struct {
		id, prev uint64
		base     int
	}
}

// scrollDrag is the scroll bar thumb being dragged: where the pointer
// and the offset started, and the size of the content then, which the
// thumb keeps until it is let go, as the rows of a List measured
// meanwhile change it.
type scrollDrag struct {
	st                 *state
	start              float32
	from               float64
	contentW, contentH float64
	horizontal         bool
}

// labelNode is an element showing or labeled with text, for tests and
// assistive technology.
type labelNode struct {
	id   uint64
	text string
	r    Rect
}

type hit struct {
	st    *state
	r     Rect
	flags uint32
}

type shortcutReg struct {
	id   uint64
	mods Modifiers
	key  Key
	// overlay marks the registration of an overlay (overlayShortcut),
	// made serial-th in its pass: the overlay made last is on top.
	overlay bool
	serial  int32
	// retried marks a key delivered, or left unrouted, in the frame after
	// the one it came to (settleKeys): it is not retried again.
	retried bool
	// repeat marks a key delivered that came as the auto-repeat of a key
	// held down.
	repeat bool
}

type keyEvent struct {
	mods Modifiers
	key  Key
	// repeat marks the auto-repeat of a key held down.
	repeat bool
}

func newRuntime(view func(*context), h host) *engine {
	rt := &engine{view: view, host: h, text: textSystem(), states: map[uint64]*state{}, windowFocused: true}
	rt.c.rt = rt
	rt.owner = weak.Make(rt)
	rt.arena = &elementOwner{rt: rt}
	rt.public = &Context{rt: rt, services: Services{owner: rt.owner}}
	rt.handleChecks = developmentHandles
	if frameStatsOn {
		rt.stats = newFrameStats(frameStatsThreshold)
	}
	return rt
}

func (rt *engine) defaultTheme() *Theme {
	if !rt.darkKnown {
		rt.dark, rt.darkKnown = rt.host.isDark(), true
		rt.themeOK = false
	}
	if !rt.prefsKnown {
		rt.themeOK = false
	}
	if !rt.themeOK {
		t := LightTheme()
		if rt.dark {
			t = DarkTheme()
		}
		t.follow(rt.preferences())
		rt.theme, rt.themeOK = *t, true
	}
	rt.passTheme = rt.theme
	return &rt.passTheme
}

// themeChanged follows a change of the system appearance, or of the
// desktop's preferences.
func (rt *engine) themeChanged() {
	rt.darkKnown, rt.prefsKnown = false, false
	rt.redraw, rt.repaintDue = false, time.Time{}
	rt.host.requestFrame()
}

// runFrame builds, lays out, paints and presents a frame.
func (rt *engine) runFrame() {
	if rt.inFrame || rt.closed {
		return
	}
	rt.inFrame = true
	defer rt.endFrame()

	rt.frame++
	rt.input = false
	rt.focusRead, rt.activeRead, rt.withinRead, rt.keysRead = false, false, false, false
	rt.stats.begin(rt)
	now := rt.frameTime()
	rt.zoomChanged = false
	w, h, scale := rt.size()
	// The content takes the room the inspector leaves.
	appW := rt.insp.contentWidth(w)
	rt.insp.lap(-1)
	rt.c.titleBar = rt.titleBar()
	rt.c.vibrancy = rt.host.vibrancy()
	rt.text.BeginFrame()
	rt.gen = rt.text.Generation()
	rt.painted = [3]float32{w, h, scale}
	rt.animating, rt.repainting, rt.repaintAt = false, false, time.Time{}
	rt.repaintRate, rt.repaintFull, rt.untracked = 0, false, false
	rt.routeKeys()
	rt.glideWheels(now)
	rt.glideSnaps(now)
	if rt.drag != nil {
		// The source's element is this frame's, if it builds one.
		rt.drag.elem = nil
	}
	rt.dragScroll()
	rt.scrollTextSelection()
	if rt.listDrag != nil {
		rt.listDrag(rt)
	}

	if rt.exitsBuilt {
		// The last frame's elements stay as they are while this one builds,
		// for copies of those that go with an exit transition.
		rt.c.chunks, rt.c.spare = rt.c.spare, rt.c.chunks
		rt.c.dirty, rt.c.spareDirty = rt.c.spareDirty, rt.c.dirty
		rt.c.used = rt.c.dirty
	}

	focused := rt.focused
	// An event handled while building (a click, an edit) may change what
	// was built before it: build again, so the frame shows the outcome.
	for pass := 0; pass < 3; pass++ {
		rt.pass = pass
		rt.consumed = false
		if rt.zoomChanged {
			// The view zoomed the content: this pass builds it at its new
			// size.
			rt.zoomChanged = false
			w, h, scale = rt.size()
			appW = rt.insp.contentWidth(w)
			rt.c.titleBar = rt.titleBar()
			rt.painted = [3]float32{w, h, scale}
		}
		rt.nextRegs = rt.nextRegs[:0]
		clear(rt.kept)
		rt.c.reset(now, appW, h)
		rt.view(&rt.c)
		if rt.closing {
			// The view closed the window, as a close button does: the
			// window is gone at once, and the frame ends with the view.
			return
		}
		rt.buildToasts(&rt.c)
		if d := rt.menu.drawn; d != nil && d.edit && rt.drawsMenus() {
			menusInWindow.drawEditMenu(rt)
		}
		if rt.overlayExits {
			// Copies of the elements gone from the layer leave in it
			// (animateExits), empty as it is otherwise: it lets the
			// pointer through, and holds nothing to focus.
			rt.c.overlayRoot()
		}
		if ov := rt.c.overlay; ov != nil {
			rt.c.root.add(ov)
		}
		if rt.insp.open {
			rt.buildInspector(&rt.c, appW, w, h)
		}
		rt.applyAllInputs()
		rt.runAfterBuild()
		rt.finishInputs()
		// Commit derived values while callbacks still hold this pass's
		// bindings. Delivered callbacks do not repeat in the next pass.
		rt.runNoticeActions()
		rt.applyFocusRequests()
		rt.runActions()
		rt.prepareSelectable(rt.c.root)
		rt.resolveMenu()
		rt.endPass()
		if rt.closing {
			return // an action closed it
		}
		if !rt.consumed && !rt.zoomChanged && !rt.unhoverGone() {
			break
		}
	}
	if rt.zoomChanged {
		// Zoomed in the last pass: laid out at the new size, and built
		// at it in the next frame.
		rt.zoomChanged = false
		w, h, scale = rt.size()
		appW = rt.insp.contentWidth(w)
		rt.c.titleBar = rt.titleBar()
		rt.painted = [3]float32{w, h, scale}
		rt.host.requestFrame()
	}
	if rt.withinRead && rt.focused != focused {
		rt.animating = true
	}
	if rt.stats != nil {
		rt.stats.passes = rt.pass + 1
	}
	root := rt.c.root
	if rt.insp.open {
		// What the inspector's Styles tab changed, over what the view built.
		rt.insp.applyEdits(root)
	}
	rt.stats.lap(phaseBuild)
	rt.insp.lap(0)
	layoutTree(root, appW, h)
	rt.measureFit(root)
	rt.commit(root, w, h)
	// Scrolling can put a new native drop target under a stationary
	// pointer. Resolve it against this frame's geometry, then rebuild if
	// needed so DragOver/DataDragOver show the new target too.
	if in := rt.incoming; in != nil {
		over := uint64(0)
		if s := rt.dataTargetAt(rt.pointerX, rt.pointerY, in); s != nil {
			over = s.id
		}
		if rt.dataOver != over {
			rt.dataOver, rt.animating = over, true
		}
	}
	rt.commitFocusBindings()
	clear(rt.texts)
	rt.texts = rt.texts[:0]
	rt.collectSelectable(root, false)
	rt.syncTextSelection()
	rt.stats.lap(phaseLayout)
	rt.insp.lap(1)
	if rt.insp.open {
		rt.insp.snapshot(rt, root)
	}
	rt.paint(root, w, h, scale)
	for try := 0; try < 2 && rt.text.Full(); try++ {
		// The glyph atlas filled up and left some out: make room, keeping
		// what the frame draws, and paint it again.
		rt.text.MakeRoom()
		rt.paint(root, w, h, scale)
	}
	rt.stats.lap(phasePaint)
	rt.insp.lap(2)
	rt.host.present(&rt.scene)
	rt.placeWebViews()
	rt.placeNativeGlass()
	// The window hit-tests its maximize button in its own DIPs.
	if r := rt.windowRect(rt.maxButton); r != rt.maxSent {
		rt.maxSent = r
		rt.host.setMaximizeButton(r)
	}
	rt.stats.lap(phasePresent)
	rt.prune()
	rt.syncDropFormats()
	rt.prunePictures()
	rt.text.EndFrame()
	rt.settleKeys()
	rt.regs, rt.nextRegs = rt.nextRegs, rt.regs
	rt.updateTextInput()
	rt.updateCursor()
	if rt.access {
		t := rt.accessTree()
		rt.liveRegions(t, true)
		rt.host.updateAccessibility(rt.windowAccess(t))
	}
	if h, ok := rt.host.(*headless); ok {
		// For tests, whether assistive technology reads the window or not.
		h.announced = append(h.announced, rt.announcements...)
	}
	rt.announcements = rt.announcements[:0]
	rt.next()
	if len(rt.retried) > 0 || len(rt.keys) > 0 {
		// The keys retried, and those waiting for an element that took
		// one (routeKeys), route in a frame built anew, at once.
		rt.redraw, rt.repaintDue = false, time.Time{}
		rt.host.requestFrame()
	}
	// What the frame moved under the pointer hovers; with the element
	// pressed gone, as a button showing over a row the pointer left, what
	// the pointer is over now hovers rather than what it was over as the
	// press began.
	rt.rehover()
	rt.armTimer()
	rt.showMenu()
	rt.c.finish()
	clear(rt.byID)
	rt.stats.end(rt)
}

// now returns the time, which a Tester sets (clock).
func (rt *engine) now() time.Time {
	if rt.clock != nil {
		return rt.clock()
	}
	return time.Now()
}

// frameTime returns the time of a frame starting: when the display's
// refresh asked for it, for a frame that waited for the display to be
// able to show it, so that what moves keeps its pace; now otherwise.
func (rt *engine) frameTime() time.Time {
	if t := rt.frameAt; !t.IsZero() {
		rt.frameAt = time.Time{}
		return t
	}
	return rt.now()
}

// next asks for the frame that what moves needs: one built anew while the
// view animates, one painting the elements again while only drawings move,
// as soon as the display can show it or when they change next. While
// nothing of the window shows, what moves waits until some of it does
// (SurfaceShown) rather than draw frames nobody sees, as browsers pause
// the animation frames of windows out of sight.
func (rt *engine) next() {
	rt.redraw, rt.repaintDue, rt.held = false, time.Time{}, false
	moving := rt.animating || rt.repainting || !rt.repaintAt.IsZero()
	switch {
	case moving && rt.host.occluded():
		rt.held = true
	case rt.animating:
		rt.pace(0)
		rt.host.requestFrame()
	case rt.repainting:
		rt.redraw = true
		fps := rt.repaintRate
		if rt.repaintFull {
			fps = 0
		}
		rt.pace(fps)
		rt.host.requestFrame()
	case !rt.repaintAt.IsZero():
		rt.pace(0)
		rt.redraw, rt.repaintDue = true, rt.repaintAt
	default:
		rt.pace(0)
	}
	rt.host.repaintTimer(rt.repaintDue)
}

// pace has the frames that only paint again come at fps frames a second,
// or at each of the display's refreshes for 0: every so many refreshes,
// the most that still give fps frames a second, so that they come evenly,
// as a 120 Hz display draws 60, 40 or 30 frames a second but not 50. The
// surface is told, where it can have the display refresh as often
// (platform.FrameRateSurface), as ProMotion displays do; elsewhere, and
// until it does, the refreshes between draw nothing (surfaceFrame). With
// the display's rate unknown, as in browsers, frames come at each.
func (rt *engine) pace(fps float32) {
	every, rate := 1, float32(0)
	if hz := rt.host.refreshRate(); fps > 0 && hz > 0 {
		// A display of 59.94 Hz draws 30 frames a second every 2.
		every = max(1, int(hz/fps+0.05))
		if every > 1 {
			rate = hz / float32(every)
		}
	}
	rt.paceEvery = every
	if rate != rt.paceRate {
		rt.paceRate = rate
		rt.host.setFrameRate(rate)
	}
}

// paceSkips reports whether the frame the surface asks for now comes
// before the frames that only paint again are due (pace), half a refresh
// early at most, as refreshes come a little late or early.
func (rt *engine) paceSkips() bool {
	hz := rt.host.refreshRate()
	if !rt.redraw || rt.paceEvery <= 1 || hz <= 0 || rt.pacedAt.IsZero() {
		return false
	}
	due := time.Duration((float32(rt.paceEvery) - 0.5) * float32(time.Second) / hz)
	return rt.now().Sub(rt.pacedAt) < due
}

// repaintNow asks for the frame that Painter.After asked for, unless
// another frame came, or was asked for, since. An event since that asked
// for none, as the pointer moving over elements that do not look at it,
// leaves it due: it builds the view anew, in case the event changed what
// the view shows.
func (rt *engine) repaintNow() {
	if !rt.repaintDue.IsZero() && !rt.now().Before(rt.repaintDue.Add(-time.Millisecond)) {
		rt.repaintDue = time.Time{}
		rt.host.requestFrame()
	}
}

// surfaceFrame draws the frame the surface asked for: the last frame's
// elements painted again when only drawings moved since, else a frame
// built anew.
func (rt *engine) surfaceFrame() {
	if rt.holdFrame() {
		return
	}
	if rt.paceSkips() {
		rt.frameAt = time.Time{}
		if rt.host.framesAtOnce() {
			// A frame asked for would come at once, again and again
			// until this one is due: the repaint timer asks for it then.
			hz := rt.host.refreshRate()
			rt.repaintDue = rt.pacedAt.Add(time.Duration(float64(rt.paceEvery) * float64(time.Second) / float64(hz)))
			rt.pacedNext = rt.repaintDue
			rt.host.repaintTimer(rt.repaintDue)
			return
		}
		// The display's next refresh asks again; the frame waited for
		// it draws at its own time.
		rt.host.requestFrame()
		return
	}
	rt.pacedAt = rt.now()
	if next := rt.pacedNext; !next.IsZero() {
		// The frame the timer asked for keeps the pace's own times, as the
		// timer fires a little late: frames would come later and later.
		rt.pacedNext = time.Time{}
		if d := rt.pacedAt.Sub(next); d > -time.Millisecond && float32(d.Seconds())*rt.host.refreshRate() < 1 {
			rt.pacedAt = next
		}
	}
	if w, h, scale := rt.size(); rt.redraw && !rt.inFrame && rt.c.root != nil &&
		rt.painted == [3]float32{w, h, scale} && rt.text.Generation() == rt.gen {
		rt.repaintFrame(w, h, scale)
		return
	}
	rt.runFrame()
}

// holdFrame keeps a frame from drawing while nothing of the window shows,
// reporting whether it did: what changed draws once some of it shows
// (SurfaceShown), as browsers draw nothing for windows out of sight and
// as Apple asks of Mac apps, rather than frames nobody sees. A window
// still draws one after input, as keys typed into a key window another
// app's covers, and one as assistive technology turns on
// (accessibilityOn); and none waits before the window's first frame
// reached its surface (windowHost.occluded).
func (rt *engine) holdFrame() bool {
	if rt.input || rt.inFrame || !rt.host.occluded() {
		return false
	}
	rt.held = true
	rt.redraw, rt.repaintDue = false, time.Time{}
	// The time the display asked for this frame at, as one waiting for
	// the GPU renderer (windowHost.waitDisplay), is not that of the frame
	// drawn once the window shows.
	rt.frameAt = time.Time{}
	rt.host.repaintTimer(time.Time{})
	return true
}

// repaintFrame paints the elements of the last frame again, at the time of
// this one, for drawings that move with it (Painter.AnimationFrame) while
// nothing else changed: the view is neither built nor laid out, so what
// moves costs only its painting. Its timers stay as the last frame built
// armed them.
func (rt *engine) repaintFrame(w, h, scale float32) {
	rt.inFrame = true
	defer rt.endFrame()
	rt.stats.begin(rt)
	start := time.Now()
	rt.c.now = rt.frameTime()
	rt.text.BeginFrame()
	rt.repainting, rt.repaintAt = false, time.Time{}
	// What the last frame painted outside Draw callbacks that asked to be
	// painted again, as a content transition, moves only painted whole.
	whole := rt.untracked
	rt.repaintRate, rt.repaintFull, rt.untracked = 0, false, false
	root := rt.c.root
	rt.repose()
	if whole || !rt.repaintParts() {
		rt.paint(root, w, h, scale)
	}
	for try := 0; try < 2 && rt.text.Full(); try++ {
		rt.text.MakeRoom()
		rt.paint(root, w, h, scale)
	}
	rt.stats.lap(phasePaint)
	rt.insp.repainted(time.Since(start))
	rt.host.present(&rt.scene)
	rt.stats.lap(phasePresent)
	rt.text.EndFrame()
	rt.next()
	rt.stats.end(rt)
}

// endPass forgets the input the pass handled.
func (rt *engine) endPass() {
	rt.forgetInput()
	// Clicks the view gave its elements, which the next pass sees.
	for _, id := range rt.clickLater {
		if s := rt.states[id]; s != nil {
			s.click(0)
		}
	}
	rt.clickLater = rt.clickLater[:0]
	if mr := &rt.menu; mr.later != 0 {
		if s := rt.states[mr.later]; s != nil {
			rt.askMenu(s, mr.laterX, mr.laterY, true)
		}
		mr.later = 0
	}
	rt.menu.chosen = 0
	// A shortcut the view did not ask for again, as it changed: what
	// this frame built may take the key (settleKeys).
	rt.unrouted = append(rt.unrouted, rt.delivered...)
	rt.delivered = rt.delivered[:0]
	rt.downs = rt.downs[:0]
	rt.passFocus()
	// The next pass may not ask again, as when the view cleared what asked.
	for _, r := range rt.c.reveal {
		rt.askReveal(r.e.id, r.o)
	}
}

// forgetInput forgets the input of the elements the pass built, which
// they handled.
func (rt *engine) forgetInput() {
	for _, s := range rt.states {
		if s.seen != rt.frame || s.pass != rt.pass {
			continue
		}
		s.clicks, s.rightClicks, s.doubleClicks = 0, 0, 0
		s.pressPending = false
		s.dragX, s.dragY = 0, 0
		s.released, s.canceled = false, false
		s.dropped = nil
		s.droppedValue, s.hasDropped = nil, false
		s.dragMissed = false
		s.dataDropped = nil
		if s.releaseLater.due {
			// The pass read the moves of a press that ended: the next
			// pass sees it end, and the clicks it gave.
			rt.applyRelease(s)
			rt.consumed = true
		}
	}
}

// prune forgets the elements the frame did not build, but those of the
// pages Routers keep.
func (rt *engine) prune() {
	if d := rt.drag; d != nil && d.native {
		if s := rt.states[d.src]; s == nil || s.seen != rt.frame || s.pass != rt.pass {
			rt.host.cancelDataDrag()
		}
	}
	if p := rt.pressed; p != nil {
		// The element pressed is gone, as when its press moved it into
		// another container: not built again, or built over by one that
		// does not take its input. Its release will not come to it, so
		// its handler gets one now, as when the window loses the focus,
		// rather than taking the next press for that release.
		input, gone := p.input, p.seen != rt.frame || p.pass != rt.pass
		x, y := p.local(rt.pointerX, rt.pointerY)
		if rt.pressLost != nil {
			input, gone, x, y = rt.pressLost, true, rt.pressLostX, rt.pressLostY
		}
		if gone {
			rt.endPress(input, x, y)
			// A frame built anew for the press ended and what the handler
			// did with it, not one only painting again.
			rt.animating = true
		}
	}
	rt.pressLost = nil
	if len(rt.exits) > 0 {
		rt.exitsEnded()
	}
	for id, s := range rt.states {
		if s.seen != rt.frame || s.pass != rt.pass {
			if !rt.keptAlive(s) {
				if s.textAdapter != nil {
					s.textAdapter.release()
				}
				if s.closes != nil {
					rt.dropCloses(s)
				}
				delete(rt.states, id)
				if rt.scrollDrag.st == s {
					rt.scrollDrag.st = nil
				}
				if rt.pressEnded == s {
					rt.pressEnded = nil
				}
				// Recycle the state itself, without keeping the editor,
				// local resources or callbacks of the element that went.
				*s = state{text: s.text.emptied()}
				if len(rt.free) < maxFree {
					rt.free = append(rt.free, s)
				}
			}
		}
	}
	rt.restoreFocus()
	// The focus does not stay in a page kept out of sight.
	if s := rt.states[rt.focused]; s != nil && (s.seen != rt.frame || s.pass != rt.pass) {
		rt.focused = 0
	}
}

// maxFree is how many pruned states the engine keeps for new elements.
const maxFree = 256

// keptAlive reports whether a state the frame did not build is in a page
// that a Router keeps: one of its history, or inside one.
func (rt *engine) keptAlive(s *state) bool {
	if len(rt.kept) == 0 {
		return false
	}
	for n := 0; s != nil && n < 64; n++ {
		if rt.kept[s.id] {
			return true
		}
		if s.page == 0 {
			return false
		}
		s = rt.states[s.page]
	}
	return false
}

// requestFrame asks the host for a frame built anew, unless one is being
// built. It comes in place of one Painter.After has due.
func (rt *engine) requestFrame() {
	if rt.inFrame {
		return
	}
	rt.redraw, rt.repaintDue = false, time.Time{}
	rt.host.requestFrame()
}

// changed asks the host for a frame built anew after the app changed what
// the view shows, from outside the view (surface.Conn.Changed).
func (rt *engine) changed() {
	rt.redraw, rt.repaintDue = false, time.Time{}
	rt.host.requestFrame()
}

// scheduleAt asks for a frame at t (After).
func (rt *engine) scheduleAt(t time.Time) {
	rt.wakeMu.Lock()
	if rt.wakeAt.IsZero() || t.Before(rt.wakeAt) {
		rt.wakeAt = t
	}
	rt.wakeMu.Unlock()
}

// armTimer arms the host's timer for the earliest frame After asked for.
func (rt *engine) armTimer() {
	rt.wakeMu.Lock()
	at := rt.wakeAt
	rt.wakeAt = time.Time{}
	rt.wakeMu.Unlock()
	rt.host.afterTimer(at)
}

// close lets the window's state go when the window closes. A window closed
// during a frame, as a close button's click closes it, keeps it until the
// frame ends: the rest of the view builds with a Context that still works,
// and reads the theme, the size and its state as before.
func (rt *engine) close() {
	if rt.inFrame {
		rt.closing = true
		return
	}
	rt.closed, rt.closing = true, false
	clear(rt.focusFields)
	rt.focusFields = nil
	for _, r := range rt.refs {
		r.closed = true
		r.requested = false
	}
	clear(rt.refs)
	rt.refs = nil
	clear(rt.parts)
	rt.parts = nil
	clear(rt.typedInputs)
	rt.typedInputs = nil
	clear(rt.notices)
	rt.notices = nil
	clear(rt.afterInputs)
	rt.afterInputs = nil
	clear(rt.afterBuild)
	rt.afterBuild = nil
	clear(rt.inputs)
	rt.inputs = nil
	rt.arena.rt = nil
	rt.public.rt = nil
	clear(rt.actions)
	rt.actions = nil
	rt.textInputClosed = true
	if rt.drag != nil && rt.drag.native {
		rt.host.cancelDataDrag()
	}
	rt.drag, rt.incoming = nil, nil
	for _, s := range rt.states {
		if s.textAdapter != nil {
			s.textAdapter.release()
		}
	}
	rt.host.afterTimer(time.Time{})
	rt.host.repaintTimer(time.Time{})
	// Last, with the engine closed: a Close that asks for a frame, or reads
	// the window, finds it closed rather than half so.
	rt.closeLocals()
	rt.dropFrame()
}

// dropFrame lets go of what the closed window showed: the view, and all
// it reaches, the elements of the last frame, their states and the scene
// painted from them, with the bitmaps they drew. Whatever may still hold
// the engine of a closed window (the window, a renderer, a plugin)
// keeps none of it (#1281). The engine draws no frame once closed.
func (rt *engine) dropFrame() {
	rt.view = func(*context) {}
	rt.scene = scene.Scene{}
	rt.maskScene = nil
	clear(rt.pathClips)
	c := &rt.c
	c.parent, c.root, c.overlay = nil, nil, nil
	c.chunks, c.spare = nil, nil
	c.used, c.dirty, c.spareDirty = 0, 0, 0
	clear(rt.states)
	rt.free = nil
	rt.hits, rt.liftHits = nil, nil
}

// endFrame ends a frame, and closes the engine when its window closed
// during it.
func (rt *engine) endFrame() {
	rt.inFrame = false
	if rt.closing {
		rt.close() // which closes the Locals the frame dropped too
	} else if len(rt.dropped) > 0 {
		rt.closeDropped()
	}
}

// commit records the laid out frame in the elements' states: their
// boxes, the hit list in paint order, the focus order.
func (rt *engine) commit(root *node, w, h float32) {
	rt.hits, rt.liftHits = rt.hits[:0], rt.liftHits[:0]
	clear(rt.posed)
	rt.posed = rt.posed[:0]
	clear(rt.webPlaces)
	rt.webPlaces = rt.webPlaces[:0]
	clear(rt.glassPlaces)
	rt.glassPlaces = rt.glassPlaces[:0]
	rt.maxButton = Rect{}
	rt.focusOrder, rt.focusScopes = rt.focusOrder[:0], rt.focusScopes[:0]
	rt.modal, rt.modalLayer, rt.commitScope, rt.commitPage = 0, 0, focusScope{}, 0
	rt.observers = 0
	if rt.groups == nil {
		rt.groups = map[uint64]groupInfo{}
	}
	clear(rt.groups)
	rt.labels = rt.labels[:0]
	// The window, which the inspector shares with the root.
	full := Rect{0, 0, w, h}
	rt.commitElement(root, full, false)
	rt.liftHitsOver()
	rt.inkStale = false
	rt.arrangeFocus()
	rt.noteGroups()
}

func (rt *engine) commitElement(e *node, clip Rect, hidden bool) {
	inline := e.isInline()
	if e.kind == kindText && e.first != nil && !inline {
		placeInline(e, e, 0)
	}
	savedHidden := rt.commitHidden
	rt.commitHidden = savedHidden || e.hidden
	saved, savedPage := rt.enterScope(e), rt.commitPage
	defer func() { rt.commitScope, rt.commitPage, rt.commitHidden = saved, savedPage, savedHidden }()
	s := e.st
	if s == rt.pressed && s.input != nil && e.inputFn == nil {
		// Another element took the place of the one pressed (prune), in
		// the axes the element pressed had.
		rt.pressLost = s.input
		rt.pressLostX, rt.pressLostY = s.local(rt.pointerX, rt.pointerY)
	}
	s.page = rt.commitPage
	if e.flags&flagPage != 0 {
		rt.commitPage = e.id
	}
	if e.poseSet {
		rt.samplePose(e)
	}
	e.world = e.transformAt(e.x, e.y)
	if e.parent != nil && e != e.c.overlay {
		e.world = e.parent.world.Mul(e.world)
	}
	s.world = e.world
	s.clips = e.flags&(flagClipX|flagClipY|flagScrollX|flagScrollY) != 0
	if s.clips {
		s.clipRect, s.clipRadii = e.clipRect()
	}
	s.selfClip, s.selfRadius, s.selfClips = e.selfClip()
	s.hitFn = e.hitFn
	if len(rt.liftHits) > 0 && (e == e.c.overlay || e.parent != nil && e.parent == e.c.overlay) {
		// The elements flying in the layer before, the window's content
		// or an overlay, go over it, under this one.
		rt.liftHitsOver()
	}
	s.lifted = e.lift
	lifting := e.lift && !rt.lifting
	if lifting {
		clip = rt.lift()
	}
	s.x, s.y, s.w, s.h = e.x, e.y, e.w, e.h
	if e.parent != nil {
		s.parent = e.parent.id
	} else {
		s.parent = 0
	}
	s.flags = e.flags
	s.rtl = e.rtl()
	s.anchor = 0
	if e.popover != nil {
		s.anchor = e.popover.id
	}
	if e.parent != nil && e.parent.st.flags&flagDisabled != 0 {
		// Disabled with the element around it, which may have been
		// disabled after building it, as a Fieldset.
		s.flags |= flagDisabled
	}
	s.ignoreInput = e.ignoreInput || e.parent != nil && e.parent.st.ignoreInput
	s.accessHidden = e.accessHidden || e.flags&flagInert != 0 || e.parent != nil && e.parent.st.accessHidden
	s.accessDisabled = e.accessDisabled || e.parent != nil && e.parent.st.accessDisabled
	s.activeDescendant = 0
	if d := e.activeDescendant; d != nil {
		s.activeDescendant = d.id
	}
	s.cursor, s.tip = e.cursor, e.tip
	s.webView = e.webView
	s.role, s.focusHighlights = e.role, e.focusHighlights
	s.input, s.caret, s.takesText, s.noIME = e.inputFn, e.caret, e.takesText, e.noIME
	if s.observe = e.observeFn; s.observe != nil {
		rt.observers++
	}
	s.copyData = e.copyData
	if s.textClient != e.textClient {
		if s.textAdapter != nil {
			s.textAdapter.release()
		}
		s.textClient, s.textAdapter = e.textClient, nil
	}
	if s.textClient != nil && s.textAdapter == nil {
		s.textAdapter = &textInputAdapter{rt: rt, id: s.id}
	}
	if (e.flags&flagEditable != 0 || e.flags&flagSelectable != 0 && s.editor != nil) && s.cursor == 0 {
		s.cursor = CursorText + 1
	}
	vis := e.visualRect(Rect{e.x, e.y, e.w, e.h})
	v := intersect(vis, clip)
	s.vx, s.vy, s.vw, s.vh = v.X, v.Y, v.W, v.H
	s.cx, s.cy = e.x+e.contentX(), e.y+e.contentY()
	s.cw, s.ch = max(e.w-e.padX(), 0), max(e.h-e.padY(), 0)
	if e.flags&(flagScrollX|flagScrollY) != 0 {
		s.contentW, s.contentH = e.contentW, e.contentH
		s.barInset = e.barInset
		s.barWidth = e.theme().scrollbarWidth()
	}
	s.hideScrollbars, _ = e.scrollbars()
	// What is invisible keeps its box but takes neither the pointer nor
	// the focus, and has no text to find; so does what is inert, which
	// shows, and what is Hidden, which has no box.
	invisible := e.flags&(flagInvisible|flagInert) != 0 || hidden || e.hidden || e.backface && e.transform.facesAway()
	s.invisible = invisible
	if invisible {
		s.vw, s.vh = 0, 0
	}
	// What ignores input shows, has its text and reads to assistive
	// technology, but takes neither the pointer nor the focus either.
	noInput := invisible || s.ignoreInput
	if noInput && rt.focused == e.id {
		rt.focused = 0
	}
	if e.webView != nil {
		rt.noteWebView(e, clip, invisible)
	}
	if m, ok := e.material.(NativeGlassMaterial); ok && rt.host.nativeGlass() && !rt.preferences().ReduceTransparency {
		if spec, ok := m.NativeGlass(); ok {
			rt.noteNativeGlass(e, clip, invisible, spec)
		}
	}
	if e.maximizeButton && !noInput && rt.maxButton == (Rect{}) && v.W > 0 && v.H > 0 {
		rt.maxButton = v
	}
	switch {
	case e.flags&flagPassThrough != 0 || noInput:
	case inline:
		// Inline elements take the pointer over their words.
		for _, r := range e.frags {
			rt.hits = append(rt.hits, hit{s, r, e.flags})
		}
	default:
		rt.hits = append(rt.hits, hit{s, Rect{e.x, e.y, e.w, e.h}, e.flags})
	}
	label := e.label
	if f := e.nameFrom; f != nil && (label == "" || e.labelledBy && f.nameOf() != "") {
		label = f.nameOf() // an input, named by its field or its label
	}
	if (label != "" || e.kind == kindText) && !invisible {
		if label == "" {
			label = e.text
		}
		switch {
		case !inline:
			rt.labels = append(rt.labels, labelNode{e.id, label, v})
		case len(e.frags) > 0 && (e.label != "" || e.flags&interactive != 0):
			// Their paragraph shows the text of the others.
			rt.labels = append(rt.labels, labelNode{e.id, label, intersect(e.visualRect(e.frags[0]), clip)})
		}
	}
	if e.flags&flagFocusable != 0 && !e.IsDisabled() && !noInput {
		rt.focusOrder = append(rt.focusOrder, e.id)
		rt.focusScopes = append(rt.focusScopes, rt.commitScope)
		// Tab goes to the radio button or tab chosen; not to a toggle
		// that is on.
		if g := rt.commitScope.group; g != 0 && e.checked == 2 && (e.role == RoleRadio || e.role == RoleTab) {
			if info := rt.groups[g]; info.checked == 0 {
				info.checked = e.id
				rt.groups[g] = info
			}
		}
	}
	if e.flags&(flagClipX|flagClipY|flagScrollX|flagScrollY) != 0 {
		r, _ := e.clipRect()
		clip = intersect(clip, e.visualRect(r))
	}
	if s.selfClips {
		clip = intersect(clip, e.visualRect(s.selfClip))
	}
	// Children in flow first, absolute ones above them: the paint order.
	// Sticky ones are in the flow for the focus, and paint among the
	// absolute ones, where their hits go.
	// A child with a z-index moves its hits to where it paints.
	if e.zOrder && e != e.c.overlay && zCommit != nil {
		zCommit(rt, e, clip, invisible)
	} else {
		var stuckBuf [4][2]int
		stuck := stuckBuf[:0]
		for ch := e.first; ch != nil; ch = ch.next {
			if ch.flags&flagAbsolute == 0 {
				from := len(rt.hits)
				rt.commitElement(ch, clip, invisible)
				if ch.sticky {
					stuck = append(stuck, [2]int{from, len(rt.hits)})
				}
			}
		}
		moved := 0
		for ch := e.first; ch != nil; ch = ch.next {
			switch {
			case ch.flags&flagAbsolute != 0:
				rt.commitElement(ch, clip, invisible)
			case ch.sticky:
				moved = rt.stickyHits(stuck[0], moved)
				stuck = stuck[1:]
			}
		}
	}
	if lifting {
		rt.lift()
	}
	e.setInk(vis)
	if e.hidden {
		// It paints nothing, wherever it is.
		e.ink, e.noCull = [4]float32{inf, inf, -inf, -inf}, false
	}
}

// lift starts or ends committing an element flying, which takes the
// pointer where it paints: its hits go over the rest of its layer
// (liftHits), out of the clips around it, but the window's, which it
// returns.
func (rt *engine) lift() Rect {
	rt.lifting = !rt.lifting
	rt.hits, rt.liftHits = rt.liftHits, rt.hits
	w, h, _ := rt.size()
	return Rect{0, 0, w, h}
}

// liftHitsOver puts the hits of the elements flying over those committed
// so far, as a layer ends: the window's content or an overlay.
func (rt *engine) liftHitsOver() {
	rt.hits = append(rt.hits, rt.liftHits...)
	rt.liftHits = rt.liftHits[:0]
}

func intersect(a, b Rect) Rect {
	x0, y0 := max(a.X, b.X), max(a.Y, b.Y)
	x1, y1 := min(a.X+a.W, b.X+b.W), min(a.Y+a.H, b.Y+b.H)
	if x1 <= x0 || y1 <= y0 {
		return Rect{x0, y0, 0, 0}
	}
	return Rect{x0, y0, x1 - x0, y1 - y0}
}

// updateCursor shows the cursor of the element under the pointer: the
// innermost one's that sets a cursor, up to the innermost element that
// takes clicks, whose own cursor is the default unless it sets one, so a
// button in a text field shows no I-beam. A scroll bar's track, and the
// thumb being dragged wherever the pointer goes, show the default, not
// the cursor of the text under the bar.
func (rt *engine) updateCursor() {
	if dragging := rt.scrollDrag.st; dragging != nil && dragging.hideScrollbars {
		rt.scrollDrag.st = nil
	}
	if rt.pressed == nil && rt.scrollDrag.st == nil && rt.overWebView() {
		// The page sets the cursor over it: it is set again as the
		// pointer comes back.
		rt.cursor = cursorUnknown
		return
	}
	c := CursorDefault
	switch {
	case rt.scrollDrag.st != nil:
	case rt.pressed != nil && rt.pressed.cursor != 0:
		c = rt.pressed.cursor - 1
	case slices.ContainsFunc(rt.hover, rt.overScrollbar):
		// The bar takes presses before what is under it (scrollbarPress).
	default:
		for _, id := range rt.hover {
			s := rt.states[id]
			if s == nil {
				continue
			}
			if s.cursor != 0 {
				c = s.cursor - 1
				break
			}
			if s.flags&flagClickable != 0 {
				break
			}
		}
	}
	if c != rt.cursor {
		rt.cursor = c
		rt.host.setCursor(c)
	}
}

// overScrollbar reports whether the pointer is over the track of a scroll
// bar element id shows.
func (rt *engine) overScrollbar(id uint64) bool {
	s := rt.states[id]
	if s == nil || s.flags&(flagScrollX|flagScrollY) == 0 || s.hideScrollbars {
		return false
	}
	x, y := s.local(rt.pointerX, rt.pointerY)
	x += s.x
	y += s.y
	g := scrollBars(Rect{s.x, s.y, s.w, s.h}, s.barInset, float32(s.contentW), float32(s.contentH), float32(s.scrollX), float32(s.scrollY), s.flags, s.barWidth, s.rtl)
	return g.vertical && g.vTrack.Contains(x, y) || g.horizontal && g.hTrack.Contains(x, y)
}

// cursorUnknown is the cursor the engine last set while the pointer is
// over a web view, whose page set another.
const cursorUnknown Cursor = 255
