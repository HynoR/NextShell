package ui

import (
	"math"
	"strings"
	"sync"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/scene"
	"github.com/egoist/mygo/internal/text"
	transfer "github.com/egoist/mygo/transfer"
)

// Align positions children along an axis: Justify places them along the
// main axis (the direction of a Row or Column), AlignItems and AlignSelf
// across it.
type Align uint8

const (
	// Start is the left of a row, the top of a column.
	Start Align = iota
	Center
	End
	// Stretch makes children as large as the container across its axis.
	Stretch
	// SpaceBetween, SpaceAround and SpaceEvenly spread children along the
	// main axis (Justify), or lines and grid tracks (AlignContent).
	SpaceBetween
	SpaceAround
	SpaceEvenly
	// Baseline lines up the first lines of text of the children of a row,
	// or of a grid's row, as a large price beside its small unit does
	// (AlignItems and AlignSelf). A child without text lines up its
	// bottom. Across a column it is Start.
	Baseline
	alignAuto
)

type unit uint8

const (
	unitAuto unit = iota
	unitPx
	unitPercent
)

// length is a size in DIPs, a percentage of the parent's or automatic.
type length struct {
	v float32
	u unit
}

// Auto is an automatic margin, which takes the free space on its side, as
// in CSS: Margin(0, Auto) centers an element horizontally, and a Row's
// child with Margin(0, 0, 0, Auto) goes to the end with those after it.
var Auto = float32(math.Inf(1))

func isAuto(v float32) bool { return math.IsInf(float64(v), 1) }

func px(v float32) length      { return length{v, unitPx} }
func percent(v float32) length { return length{v, unitPercent} }

// bound is a minimum or a maximum size, in DIPs, by a percentage of the
// parent's, or by both, as CSS's max-width: min(78%, 34rem): the larger
// of two minimums applies, and the smaller of two maximums.
type bound struct {
	px, pct float32
	set     uint8 // boundPx, boundPct
}

const (
	boundPx uint8 = 1 << iota
	boundPct
)

func (b *bound) setPx(v float32)  { b.px, b.set = v, b.set|boundPx }
func (b *bound) setPct(p float32) { b.pct, b.set = p, b.set|boundPct }

// lower and upper return a minimum and a maximum in DIPs against base,
// and false when none applies: a percentage applies only against a known
// base, not -1. Most elements have none, which they find inline.
func (b bound) lower(base float32) (float32, bool) {
	if b.set == 0 {
		return 0, false
	}
	return b.resolve(base, true)
}

func (b bound) upper(base float32) (float32, bool) {
	if b.set == 0 {
		return 0, false
	}
	return b.resolve(base, false)
}

func (b bound) resolve(base float32, larger bool) (float32, bool) {
	v, ok := b.px, b.set&boundPx != 0
	if b.set&boundPct != 0 && base >= 0 {
		p := b.pct / 100 * base
		switch {
		case !ok:
			v, ok = p, true
		case larger:
			v = max(v, p)
		default:
			v = min(v, p)
		}
	}
	return v, ok
}

// resolve returns the length in DIPs against base, and false for auto or a
// percentage of an unknown base.
func (l length) resolve(base float32) (float32, bool) {
	switch l.u {
	case unitPx:
		return l.v, true
	case unitPercent:
		if base >= 0 {
			return l.v / 100 * base, true
		}
	}
	return 0, false
}

type kind uint8

const (
	kindBox kind = iota
	kindText
	kindImage
	kindInput
	kindIcon
)

// flags of an element.
const (
	flagClickable uint32 = 1 << iota
	flagFocusable
	flagEditable
	flagDragWindow
	flagScrollX
	flagScrollY
	flagClipX
	flagClipY
	flagAbsolute
	flagDisabled
	flagTrackPointer
	flagPassThrough
	flagDraggable
	flagHover
	flagOwnRing
	flagDropTarget
	flagContextMenu
	flagSelectable
	flagInvisible
	flagDebug
	// flagToggle marks check boxes, switches and radio buttons, which
	// leave Enter to the window's shortcuts and take Space.
	flagToggle
	// flagChoosable marks the rows of a list that chooses them, which
	// assistive technology focusing chooses.
	flagChoosable
	// flagModal marks the backdrop of a dialog, which scopes the focus.
	flagModal
	// flagTypeSelect marks a list that typing the first letters of a
	// row chooses it in.
	flagTypeSelect
	// flagMenuButton marks an element that opens its menu when pressed.
	flagMenuButton
	// flagKeepFocus keeps the focus where it is when the element is
	// pressed, as a combobox's popup does for its input.
	flagKeepFocus
	// flagValueDrop marks an element taking values dragged within the
	// window (Drop, DragOver).
	flagValueDrop
	// flagInert marks what shows but takes neither the pointer nor the
	// focus, hidden from assistive technology, as a page going away.
	flagInert
	// flagPage marks a page of a Router: the elements inside keep their
	// state while the page is in the history, shown or not.
	flagPage
	// flagFocusTarget marks an element the focus goes to but Tab does not
	// stop at, as a Router's page, or an element of TabStop(false).
	flagFocusTarget
	// flagDividers marks an element drawing lines between its children,
	// listed in Context.dividers.
	flagDividers
	// flagUnselectable excludes text from a surrounding Selectable container.
	flagUnselectable

	// flagClip clips both ways.
	flagClip = flagClipX | flagClipY
)

type shadow struct {
	x, y, blur, spread float32
	color              Color
	inset              bool
}

// textStyle is the text styling of an element; descendants inherit what is
// set.
type textStyle struct {
	set    uint32
	family string
	size   float32
	weight int
	italic bool
	color  Color
	// lineHeight is a multiple of the font size, or DIPs when fixedLine.
	lineHeight float32
	fixedLine  bool
	align      Align
	underline  bool
	wavy       bool
	dotted     bool
	solid      bool // an underline through descenders (Link's hover)
	strike     bool
	spacing    float32 // letter spacing
	features   string
	// lang is the language of the text, a BCP 47 tag (Lang).
	lang string
	// decoColor and decoThick are those of underlines and strikethroughs,
	// decoOffset how far below the baseline underlines are, when set
	// (setDecoOffset); background is behind the text.
	decoColor  Color
	decoThick  float32
	decoOffset float32
	background Color
	// selection is the highlight of selected text, the theme's when unset.
	selection Color
	// caret is the color of the insertion caret, the theme's accent when
	// unset.
	caret Color
	// strokeWidth and strokeColor stroke the outlines of the glyphs
	// under their fill (TextStroke).
	strokeWidth float32
	strokeColor Color
	// tabSize puts tab stops every tabSize spaces (TabSize).
	tabSize float32
	// shadow is the shadow the glyphs cast (TextShadow).
	shadow TextShadow
}

const (
	setFamily uint32 = 1 << iota
	setSize
	setWeight
	setItalic
	setColor
	setLineHeight
	setAlign
	setUnderline
	setStrike
	setSpacing
	setFeatures
	setDecoColor
	setDecoThick
	setBackground
	setSelection
	setLang
	setDecoOffset
	setCaret
	setStroke
	setTabSize
	setShadow

	// setAll has every bit of textStyle.set.
	setAll = setShadow<<1 - 1
)

// layoutStyle is what lays an element out, of what the view sets on it:
// an element whose layoutStyle, text style, children and the like are
// those of the frame before lays out as it did then (layoutcache.go),
// which compares them. Whatever else of the element its layout reads goes
// into the comparison there; TestLayoutInputs keeps node's other fields
// out of layout.
type layoutStyle struct {
	// First, as what changes most, so that comparing finds it soonest.
	text string

	kind                 kind
	row                  bool
	wrap                 bool
	reverse, wrapReverse bool
	// dir is the element's direction (dirRTL, dirSet), and logical which
	// of its horizontal edges were set by their start or end
	// (direction.go).
	dir, logical           uint8
	justify, align, self   Align
	alignContent           Align
	gapX, gapY             float32
	pad                    [4]float32 // top, right, bottom, left
	border                 [4]float32 // top, right, bottom, left
	margin                 [4]float32 // Auto for auto margins
	width, height          length
	minW, minH, maxW, maxH bound
	grow, shrink           float32
	basis                  length
	inset                  [4]length
	aspect                 float32
	grid                   bool
	justifyItems           Align // of a grid
	cell                   gridCell
	justifySelf            Align
	// sticky holds the element in its scroll container by its insets
	// (Sticky), which then do not move it as relative positioning does.
	sticky bool
	// baselines is set on the row of a field in a form, which lines up
	// the first lines of its label and control.
	baselines bool

	maxLines   int
	single     bool
	noWrap     bool
	balance    bool
	pretty     bool
	keepSpaces bool
	ellipsis   string
}

// Element is a node of a frame's user interface. The functions that create
// elements return them so that their methods can style them and ask about
// their interaction, and the methods return the element for chaining:
//
//	ui.Text(c, "Hello").FontSize(20).Bold()
//
// An element only lives during the build pass that created it. Do not keep
// it in app state: a later pass may clear or reuse its storage for another
// element. For a list's focus and shortcuts, keep its ListState instead.
// Common input queries return false or zero for nil or cleared elements;
// this does not make references to reused storage safe.
type node struct {
	epoch      uint64
	key        any
	valueInput func(*node)
	c          *context
	id         uint64
	flags      uint32
	parent     *node
	first      *node
	last       *node
	next       *node
	nchild     int
	depth      int
	st         *state
	// track is the ScrollState of a scroll container (TrackScroll).
	track *ScrollState

	// layoutStyle holds what lays the element out, and cols and rows
	// the tracks of a grid.
	layoutStyle
	cols, rows []Track

	// Visual geometry, composed after layout.
	transform        Transform
	translatePct     [2]float32 // TranslatePercent's
	originX, originY float32
	originSet        bool
	// originAttached puts the origin on the element's point on its target
	// as its layout attaches it (AttachedOrigin).
	originAttached bool

	world scene.Affine
	// content is how its content changes (ContentTransition), when set.
	content    ContentTransition
	contentSet bool
	// pose is where AnimatePose moves it, as poseAnim says, when poseSet;
	// poseM and poseFade are where it shows now. reposed tells, in a frame
	// that only paints, that its pose moved.
	pose     Pose
	poseAnim Animation
	poseSet  bool
	poseM    scene.Affine
	poseFade float32
	reposed  bool
	// ink bounds, in the window, all that the element and those inside it
	// may paint, as commit found it: x0, y0, x1, y1. Unless noCull, what
	// it paints is no more than that, so that painting skips it where ink
	// is clipped out (paintable).
	ink    [4]float32
	noCull bool
	// backface hides it while its 3D transform turns its back to the
	// viewer (BackfaceHidden).
	backface bool
	// hidden takes it and the elements inside it out of the frame, as
	// CSS's display: none does (Hidden): the layout gives it no room, and
	// it paints, takes input and reads to assistive technology as an
	// invisible element does, while what is inside it keeps its state.
	hidden bool
	// ignoreInput lets the pointer and the keyboard pass it and the
	// elements inside it by, which still show and read to assistive
	// technology (IgnoreInput).
	ignoreInput bool
	// accessHidden leaves it and the elements inside it out of what
	// assistive technology reads, while they still show and take input
	// (AccessHidden).
	accessHidden bool
	// accessDisabled tells assistive technology that it and the elements
	// inside it are unavailable, while they keep the focus, the pointer
	// and their look (AccessDisabled).
	accessDisabled bool

	// Painting.
	bg          Color
	fill        fillKind
	grad        LinearGradient
	stripes     stripes
	borderC     Color
	borderStyle BorderStyle
	radius      [4]float32
	shadows     []shadow
	material    Material
	effect      Effect
	effecting   bool
	// cache keeps the element's layer while what it paints is the same
	// (Cache).
	cache layerKeeper
	// grads holds a conic or radial fill and a border's gradient, which
	// few elements have; blur is the standard deviation of the element's
	// blur, in DIPs.
	grads *gradients
	// clipping is how the element clips and masks all it paints
	// (ClipInset, MaskLinearGradient), which few elements do.
	clipping   *clipping
	blur       float32
	opacity    float32
	opacitySet bool
	undimmed   bool // kept at its opacity while disabled (DimDisabled)
	cursor     Cursor
	paintFn    func(p *Painter, r Rect)
	// hitFn tells where in its box the element takes the pointer
	// (HitTest).
	hitFn func(x, y float32, r Rect) bool
	// objectFn draws the inline objects of a text (DrawObjects).
	objectFn func(p *Painter, at int, r Rect)
	// styleFn sets the colors and opacity the element paints with, as it
	// is painted: it changes nothing else, so that an element out of view
	// can skip it.
	styleFn      func(e *node)
	paintAfterFn func(p *Painter, r Rect)

	// Input the element takes itself (HandleInput), and where the caret of
	// the text it takes is (TextCaret).
	inputFn    func(InputEvent) bool
	observeFn  func(InputEvent) // the input in it (ObserveInput)
	textClient TextInputClient
	caret      Rect
	takesText  bool
	noIME      bool // InputMethod(false)
	copyData   func(parts []SelectedText) transfer.Data

	// Content.
	spans    []Span // of a RichText
	spansKey string // their styles, for the layout
	// A text holding inline elements (inline.go) keeps its own text, and
	// merges the styles of theirs into its spans once a frame. An inline
	// element has the range of its text in its parent's, in runes, and the
	// boxes of that text in the paragraph's layout.
	ownText   string
	paragraph bool
	merged    bool
	runes     [2]int
	frags     []Rect
	ts        textStyle
	// themed is the theme the element's children build and paint with,
	// in place of the window's (Themed).
	themed  *Theme
	image   *Bitmap
	svg     *SVG // of an Icon, or an Image in its own colors
	webView NativeWebView
	// maximizeButton marks the window's maximize button (MaximizeButton).
	maximizeButton bool
	fit            Fit
	gray           bool
	// objectPos is where ObjectPosition puts the picture in its box, from
	// its middle: 0, 0 by default.
	objectPos [2]float32
	// ringOffset and ringColor are the offset and the color of the
	// element's focus ring, where ringSet says it sets them.
	ringOffset float32
	ringColor  Color
	ringSet    uint8
	// dismissed: Escape or a click outside closed the dialog or popover
	// whose panel this is, in this frame (Dismissed).
	dismissed bool
	rotate    float32 // of an Icon, in degrees
	label     string
	// widget names the widget that used the element's state as it created
	// it, which Key would then lose.
	widget string

	// What assistive technology sees: the role, whether a check box,
	// radio or switch is off (1), on (2) or mixed (3), whether a pop-up
	// shows, a value, and a range's minimum, maximum and value.
	role     Role
	checked  int8
	expanded bool
	// busy marks content about to change (Busy), current the item the
	// element is the current one of (Current), and autoComplete what the
	// suggestions of a text input complete (AutoComplete).
	busy         bool
	current      Current
	autoComplete AutoComplete
	// orient is the way the element runs for assistive technology:
	// Vertical for a slider going up (Vertical), or what Orientation
	// says; 0 to find it from the element.
	orient Orientation
	// tip marks an element with a tooltip (TooltipBase).
	tip bool
	// highlighted is set on the option of a select the pointer or the
	// arrows are on, and keepOpen on an item of a menu that leaves the
	// menu open as it is chosen (KeepOpen). focusHighlights marks an
	// option of a select, which assistive technology focusing highlights,
	// as the arrows do, rather than choosing it.
	highlighted     bool
	focusHighlights bool
	keepOpen        bool
	accValue        string
	accRange        [3]float64
	hasRange        bool
	// accFill describes the element to assistive technology further, as
	// a code editor its text and selection.
	accFill func(n *platform.AccessNode)
	// accStep is how far the keys move the value of a range.
	accStep float64
	// keyShortcuts are the keys that press or focus the element, as the
	// platform writes them (KeyShortcuts).
	keyShortcuts string
	// accGrid is the element's RowCount, ColumnCount, RowIndex and
	// ColumnIndex, and accSet its place in its set and the set's size
	// (PositionInSet), 0 for those not given.
	accGrid [4]int32
	accSet  [2]int32

	// Layout results, in DIPs relative to the window.
	x, y, w, h float32
	// contentW and contentH are the size of a scroll container's content.
	contentW, contentH float64
	// barInset moves a scroll container's scroll bars in from its edges
	// (ScrollbarInsets): top, right, bottom, left.
	barInset [4]float32
	// scrollBase is the offset of a List's content that its rows were
	// placed at: placing moves them by how far the offset moved since.
	scrollBase float64
	// list is the List the element is, while it builds and lays out, and
	// listRow is set on the elements holding its rows, rowIndex their row.
	// rowsOf is the list whose rows the element holds for assistive
	// technology, and takes the keys for: the list, or its Table.
	// popover is the anchor of the panel of a popover. hideScrollbars and
	// keepScrollbars are the element's Scrollbars and KeepScrollbars, and
	// barsFrom the element whose they are instead, as a table's for the
	// list of its rows.
	popover        *node
	list           *listFrame
	listRow        bool
	hideScrollbars bool
	keepScrollbars bool
	barsFrom       *node
	rowIndex       int
	rowsOf         *listFrame
	// snap is how the element, a scroll container, snaps (ScrollSnap), and
	// snapAlign how the element lines up with the container around it
	// that does (SnapAlign): Start, Center or End plus one, horizontally
	// in its low four bits and vertically in its high four, 0 for none.
	snap      Snap
	snapAlign uint8
	// focusGroup makes the element a focus group (FocusGroup), whose
	// arrows click the element they move to with groupSelects.
	focusGroup   Orientation
	groupSelects bool
	// placeOn is a sibling, laid out in the parent's flow, whose box an
	// absolute element's insets are measured from instead of the parent's.
	// A split uses it to keep its handle on the divider.
	placeOn *node
	// overflow is set on a toolbar, whose layout notes there the controls
	// it had no room for, and takes them out of the flow, collapsed.
	overflow  *[]toolbarItem
	collapsed bool
	// segment marks a segment of a segmented control or a group of
	// toggles.
	segment bool
	// activeDescendant is the option of a combobox's popup the arrows are
	// on, which assistive technology follows while the combobox has the
	// focus; nameFrom is the element whose Label names this one, as the
	// field around an input; setPos and setSize say which option of how
	// many it is; choosesItems marks a popup whose options are chosen, and
	// search a search field.
	activeDescendant *node
	nameFrom         *node
	// nameJoin makes the element's name nameFrom's followed by its Label,
	// as a range slider's knobs; labelledBy makes it nameFrom's in place
	// of its Label (LabelledBy).
	nameJoin, labelledBy bool
	// focusTo is the element taking the focus that Focus and AutoFocus
	// give this one, and whose focus Focused reports: the input of the box
	// around it (field).
	focusTo         *node
	setPos, setSize int
	choosesItems    bool
	search          bool
	// description tells assistive technology more than the name, and
	// invalid marks a value that is not valid (Description, Error,
	// Invalid). describedBy are the elements whose text describes it too
	// (DescribedBy), and errorMessage those holding its error, read while
	// it is invalid (ErrorMessage).
	description  string
	invalid      bool
	describedBy  []*node
	errorMessage []*node
	// field is set on a Field, form on a Form, whose layout makes its
	// labels as wide as the widest.
	field *fieldParts
	form  *formBuild
	// rowMinW is the least width of the rows of a list scrolling sideways,
	// as a table's; followX is the scroll container whose horizontal
	// offset the element's content follows, as a table's header follows
	// its rows; sort is the order a table's column header shows: 1
	// ascending, 2 descending, 3 another (SortDirection).
	rowMinW float32
	followX *node
	sort    int8
	// colFit fits a column of a table to its cells as it lays out.
	colFit *tableFit
	// level is how deep an item of a tree is, from 1, and expandable
	// marks one with children.
	level      int
	expandable bool
	// chooseMany marks a GridView choosing several items, or an element
	// whose items are (Multiselectable), and gridFit is how many columns
	// a GridView's items took.
	chooseMany bool
	gridFit    *gridFit
	// roleDescription names the element's role to assistive technology
	// (RoleDescription), controls are the elements it controls
	// (Controls), and hasPopup the kind of popup it opens (HasPopup).
	roleDescription string
	controls        []*node
	hasPopup        Popup
	tl              *text.Layout
	measured
	// lay says what the layout cache may do with the element
	// (layOwner, layReuse), and laid when layoutBox last laid it out, in
	// engine.laySeq.
	lay  uint8
	laid uint64
	// leaving marks a copy of an element gone, which its exit transition
	// animates (1 if it was in its parent's flow, 2 if absolute), and
	// ghosts an element holding such copies; lift one flying with a
	// Match, which paints over the rest of its layer (matches.lifted).
	// attach is where Attach puts the element, 0 for nowhere, and placed
	// where AttachTo put it as this frame laid it out, on whichever side
	// of its target, flips the sides it went to (flipAcross, flipDown).
	leaving       uint8
	ghosts        bool
	lift          bool
	attach        attachment
	placed        attachment
	flips         uint8
	edgeMarginSet bool
	// edgeMargin, when edgeMarginSet, is how far AttachTo keeps the
	// element from the window's edges (AttachEdgeMargin), in place of
	// windowMargin.
	edgeMargin float32
	// zIndex orders the element's painting and its hits among its
	// siblings (ZIndex); zOrder is set on an element with a child having
	// one, which paints and commits its children in that order alone
	// (zindex.go).
	zIndex int32
	zOrder bool
	// serial is the order the pass made the element in: of overlays, the
	// one made last shows on top.
	serial int32
}

// measure is the height of an element at a width (naturalHeight).
type measure struct {
	availW, h float32
}

// measured is what the layout measured of an element in a frame, for
// measuring it again.
type measured struct {
	measures [4]measure
	nmeasure int
	leaf     [2]float32 // the max-content and min-content widths of text
	leafOK   bool
	// intr holds the max-content and min-content widths of the element
	// (intrinsic), measured once a frame, intrOK which of them it holds.
	intr   [2]float32
	intrOK uint8
}

// Rect is a rectangle in DIPs.
type Rect struct{ X, Y, W, H float32 }

// Contains reports whether the point is inside r.
func (r Rect) Contains(x, y float32) bool { return x >= r.X && y >= r.Y && x < r.X+r.W && y < r.Y+r.H }

// Fit says how an image fills its element.
type Fit uint8

const (
	// Contain scales the image to fit inside the element, keeping its
	// aspect ratio.
	Contain Fit = iota
	// Cover scales the image to cover the element, cropping it.
	Cover
	// FillBox stretches the image to the element.
	FillBox
	// ScaleDown shows the image at its own size, or as Contain does where
	// that is smaller.
	ScaleDown
	// NaturalSize shows the image at its own size, centered and cropped.
	NaturalSize
)

// BorderStyle is how a border draws.
type BorderStyle uint8

const (
	BorderSolid BorderStyle = iota
	// BorderDashed draws each side in dashes about three times as long as
	// the side is wide, with a dash at both ends.
	BorderDashed
)

// LinearGradient blends colors along a line across a box, as CSS's
// linear-gradient does: two, From and To, or as many as its Stops.
//
//	e.LinearGradient(ui.LinearGradient{From: blue, To: yellow, Angle: 90, Oklab: true})
type LinearGradient struct {
	From, To Color
	// Angle is the direction of the line, in degrees clockwise from
	// upwards: 180 goes from top to bottom, 90 from left to right.
	Angle float32
	// Start and End place From and To along the line, from 0 to 1: the
	// colors are solid before Start and after End. Both 0 mean 0 and 1.
	Start, End float32
	// Oklab mixes the colors in the Oklab color space, whose midpoints keep
	// the colors' lightness and saturation, instead of in sRGB, as CSS's
	// "in oklab" does.
	Oklab bool
	// Stops, unless empty, are the gradient's colors in place of From and
	// To (and Start and End), at their places along the line from 0 to 1,
	// in order:
	//
	//	ui.LinearGradient{Angle: 90, Stops: []ui.GradientStop{{red, 0}, {yellow, 0.5}, {blue, 1}}}
	Stops []GradientStop
}

// GradientStop is a color of a gradient (LinearGradient, ConicGradient,
// RadialGradient) and its place along it, from 0 to 1, as CSS's color
// stops are. The gradient shows its first color before its first stop,
// its last after its last, and between two stops their colors blended;
// two stops at one place make a sharp edge. A gradient takes 32 stops at
// most, in sRGB.
type GradientStop struct {
	Color Color
	At    float32
}

// ConicGradient blends colors around a point, as CSS's conic-gradient()
// does (SwiftUI's AngularGradient, Flutter's SweepGradient): its stops at
// their places around a turn, clockwise from its angle.
//
//	e.ConicGradient(ui.ConicGradient{Stops: []ui.GradientStop{{red, 0}, {blue, 0.5}, {red, 1}}})
type ConicGradient struct {
	Stops []GradientStop
	// Center is the point the colors turn around, in fractions of the
	// box's width and height from its top left corner; the zero Point is
	// its middle.
	Center Point
	// Angle is where the turn starts, in degrees clockwise from upwards, as
	// CSS's "from": 90 starts at the right, where SwiftUI's and Flutter's
	// start.
	Angle float32
	// Oklab mixes the colors in the Oklab color space, as LinearGradient's
	// does.
	Oklab bool
}

// RadialGradient blends colors outwards from a point, as CSS's
// radial-gradient() does (SwiftUI's RadialGradient, Flutter's
// RadialGradient): its stops at their places from its center, at 0, to a
// circle of its radius, at 1.
//
//	e.RadialGradient(ui.RadialGradient{Stops: []ui.GradientStop{{white, 0}, {ui.Transparent, 1}}})
type RadialGradient struct {
	Stops []GradientStop
	// Center is the point the colors spread from, in fractions of the
	// box's width and height from its top left corner; the zero Point is
	// its middle, as ConicGradient's is.
	Center Point
	// Radius is how far the gradient reaches from its center, in DIPs; 0
	// reaches the corner of the box farthest from the center, as CSS's
	// farthest-corner default does. The gradient shows its first color
	// before the center and its last past the radius.
	Radius float32
	// Oklab mixes the colors in the Oklab color space, as LinearGradient's
	// does.
	Oklab bool
}

// gradients is a conic or radial fill and a border's gradient, linear or
// conic.
type gradients struct {
	conic        ConicGradient
	radial       RadialGradient
	borderLinear LinearGradient
	borderConic  ConicGradient
	border       borderPaint
}

// borderPaint is what paints a border: its color, or a gradient.
type borderPaint uint8

const (
	borderColor borderPaint = iota
	borderLinear
	borderConic
)

// grad returns the element's gradients, made the first time.
func (e *node) gradients() *gradients {
	if e.grads == nil {
		e.grads = &gradients{}
	}
	return e.grads
}

type fillKind uint8

const (
	fillColor fillKind = iota
	fillGradient
	fillStripes
	fillMaterial
	fillConic
	fillRadial
)

type stripes struct {
	c                 Color
	width, gap, angle float32
}

// Children builds the element's children: elements created while fn runs
// are added to e.
func (e *node) Children(fn func()) *node {
	c := e.c
	saved := c.parent
	c.parent = e
	if t := e.themed; t != nil {
		// As Toolbar flips c.buttons for its controls.
		outer, window := c.theme, c.window
		c.theme = t
		fn()
		if outer == window {
			// SetTheme called inside themes what follows the scope.
			outer = c.window
		}
		c.theme = outer
	} else {
		fn()
	}
	c.parent = saved
	if e.kind == kindText && e.first != nil {
		e.inlineText()
	}
	return e
}

// Key identifies the element among its siblings by k instead of by its
// position, so that its state (focus, scrolling, text being edited, …)
// follows it when the siblings before it change. Call it right after
// creating the element. Widgets that handle their input as they are
// created (Checkbox, Radio, Switch, Slider, Select, Link, List, TextInput
// and TextArea) cannot take a key, and Key panics: give it to an element
// around them instead, as Row(c).Key(k).Children(...) does. Siblings need
// keys of their own: two elements with one key share one state, which
// MyGo logs, and a Tester panics for.
func (e *node) Key(k any) *node {
	if e.widget != "" {
		panic("ui: Key on a " + e.widget + ", whose state was initialized: use Context.Key before construction")
	}
	e.c.rekey(e, k)
	return e
}

// Row lays the children out from left to right.
func (e *node) Row() *node {
	e.row, e.grid = true, false
	if e.align == alignAuto {
		e.align = Center
	}
	return e
}

// Column lays the children out from top to bottom.
func (e *node) Column() *node { e.row, e.grid = false, false; return e }

// Reverse lays the children out in the other direction: a Row from right
// to left, a Column from bottom to top, as CSS's row-reverse and
// column-reverse do. Justify's Start is then the right or the bottom.
func (e *node) Reverse() *node { e.reverse = true; return e }

// Wrap starts a new line of children when they do not fit.
func (e *node) Wrap() *node { e.wrap = true; return e }

// WrapReverse wraps the children onto lines that stack the other way: up
// in a Row, to the left in a Column.
func (e *node) WrapReverse() *node { e.wrap, e.wrapReverse = true, true; return e }

// Gap puts space between children, and between the lines of a wrapping
// container or the tracks of a grid.
func (e *node) Gap(v float32) *node { e.gapX, e.gapY = v, v; return e }

// GapX sets the horizontal space between children, lines or columns.
func (e *node) GapX(v float32) *node { e.gapX = v; return e }

// GapY sets the vertical space between children, lines or rows.
func (e *node) GapY(v float32) *node { e.gapY = v; return e }

// AlignContent places the lines of a wrapping container across its main
// axis, as CSS's align-content does: Start (the default), Center, End,
// Stretch, SpaceBetween, SpaceAround or SpaceEvenly. In a grid it places
// the rows, which stretch when it is not set.
func (e *node) AlignContent(a Align) *node { e.alignContent = a; return e }

// edges expands CSS shorthand values: all, vertical horizontal, top
// horizontal bottom, or top right bottom left.
func edges(v []float32) [4]float32 {
	switch len(v) {
	case 1:
		return [4]float32{v[0], v[0], v[0], v[0]}
	case 2:
		return [4]float32{v[0], v[1], v[0], v[1]}
	case 3:
		return [4]float32{v[0], v[1], v[2], v[1]}
	case 4:
		return [4]float32{v[0], v[1], v[2], v[3]}
	}
	return [4]float32{}
}

// Padding sets the space inside the element's edges, CSS style: all
// sides, vertical and horizontal, or top, right, bottom and left.
func (e *node) Padding(v ...float32) *node { e.pad = edges(v); e.logical &^= logPad; return e }

// PaddingX sets the left and right padding.
func (e *node) PaddingX(v float32) *node { e.pad[1], e.pad[3] = v, v; e.logical &^= logPad; return e }

// PaddingY sets the top and bottom padding.
func (e *node) PaddingY(v float32) *node { e.pad[0], e.pad[2] = v, v; return e }

// Margin sets the space around the element, as Padding does. Auto
// margins take the free space on their side.
func (e *node) Margin(v ...float32) *node { e.margin = edges(v); e.logical &^= logMargin; return e }

// MarginX sets the left and right margins.
func (e *node) MarginX(v float32) *node {
	e.margin[1], e.margin[3] = v, v
	e.logical &^= logMargin
	return e
}

// MarginY sets the top and bottom margins.
func (e *node) MarginY(v float32) *node { e.margin[0], e.margin[2] = v, v; return e }

// ScrollbarInsets moves a scroll container's scroll bars in from its
// edges, CSS style as Padding: the vertical bar runs from top DIPs below
// its top to bottom DIPs above its bottom, right DIPs in from its right,
// and the horizontal bar from left to right DIPs in, bottom DIPs up. A bar
// keeps clear so of what floats over the content, as a toolbar the
// content scrolls under, which AppKit's scrollerInsets and UIKit's scroll
// indicator insets do:
//
//	ui.Scroll(c).Fill().Padding(64, 16, 16).ScrollbarInsets(64, 0, 0)
func (e *node) ScrollbarInsets(v ...float32) *node { e.barInset = edges(v); return e }

// Scrollbars controls the overlay indicators and their pointer hit targets on
// this scroll container. They show on hover by default. With false, scrolling
// by wheel, TrackScroll, keyboard focus and accessibility remains available.
// It applies to Scroll, ScrollHorizontal, ScrollBoth, List and Table, not the
// internal indicators of custom controls such as a code editor (whose State
// has NoScrollbars), and is not inherited.
func (e *node) Scrollbars(visible bool) *node { e.hideScrollbars = !visible; return e }

// KeepScrollbars keeps this scroll container's scroll bars showing while its
// content overflows it, with the pointer over it or not, rather than only
// while it is hovered or a bar is dragged: a view whose place in its content
// matters, as a long diff filling a window, shows how far down it is. The
// bars look and take presses as they do on hover; Scrollbars(false) hides
// them still. It applies where Scrollbars does (a code editor's State has
// KeepScrollbars), and is not inherited.
func (e *node) KeepScrollbars(keep bool) *node { e.keepScrollbars = keep; return e }

// scrollbars returns whether the element hides its scroll bars
// (Scrollbars(false)) and whether it keeps them showing (KeepScrollbars),
// as it says or the element it takes them from does (barsFrom).
func (e *node) scrollbars() (hide, keep bool) {
	if e.barsFrom != nil {
		e = e.barsFrom
	}
	return e.hideScrollbars, e.keepScrollbars
}

// Width sets the width in DIPs.
func (e *node) Width(v float32) *node { e.width = px(v); return e }

// Height sets the height in DIPs.
func (e *node) Height(v float32) *node { e.height = px(v); return e }

// Size sets the width and height in DIPs.
func (e *node) Size(w, h float32) *node { e.width, e.height = px(w), px(h); return e }

// WidthPercent sets the width as a percentage of the parent's.
func (e *node) WidthPercent(p float32) *node { e.width = percent(p); return e }

// HeightPercent sets the height as a percentage of the parent's.
func (e *node) HeightPercent(p float32) *node { e.height = percent(p); return e }

// FillWidth makes the element as wide as its parent's content.
func (e *node) FillWidth() *node { e.width = percent(100); return e }

// FillHeight makes the element as tall as its parent's content.
func (e *node) FillHeight() *node { e.height = percent(100); return e }

// Fill makes the element as large as its parent's content.
func (e *node) Fill() *node { e.width, e.height = percent(100), percent(100); return e }

// MinWidth, MinHeight, MaxWidth and MaxHeight bound the size in DIPs.
func (e *node) MinWidth(v float32) *node  { e.minW.setPx(v); return e }
func (e *node) MinHeight(v float32) *node { e.minH.setPx(v); return e }
func (e *node) MaxWidth(v float32) *node  { e.maxW.setPx(v); return e }
func (e *node) MaxHeight(v float32) *node { e.maxH.setPx(v); return e }

// MinWidthPercent, MinHeightPercent, MaxWidthPercent and MaxHeightPercent
// bound the size by a percentage of the parent's. With a bound in DIPs
// too, both apply: the larger minimum and the smaller maximum, as CSS's
// max-width: min(78%, 34rem) for a chat bubble:
//
//	ui.Box(c).MaxWidthPercent(78).MaxWidth(544)
func (e *node) MinWidthPercent(p float32) *node  { e.minW.setPct(p); return e }
func (e *node) MinHeightPercent(p float32) *node { e.minH.setPct(p); return e }
func (e *node) MaxWidthPercent(p float32) *node  { e.maxW.setPct(p); return e }
func (e *node) MaxHeightPercent(p float32) *node { e.maxH.setPct(p); return e }

// Grow gives the element a share f of the free space along its parent's
// main axis: Grow(1) on one child makes it take all of it. Like CSS flex:
// f, the element then starts from no size (unless Basis says otherwise) and
// may shrink below its content in a column, which suits a list or editor
// filling the rest of a window.
func (e *node) Grow(f float32) *node {
	e.grow = f
	// As CSS's flex: 1, a basis of 0%: none in a parent of a known size,
	// the content's in one whose size the content sets, so that a child
	// growing in a column as high as its content keeps its height.
	if e.basis.u == unitAuto {
		e.basis = percent(0)
	}
	return e
}

// Shrink sets how much the element gives up when its siblings do not fit
// (1 by default, 0 never).
func (e *node) Shrink(f float32) *node { e.shrink = f; return e }

// Basis sets the size along the parent's main axis before growing or
// shrinking.
func (e *node) Basis(v float32) *node { e.basis = px(v); return e }

// BasisPercent sets the basis as a percentage of the parent's size along
// its main axis.
func (e *node) BasisPercent(p float32) *node { e.basis = percent(p); return e }

// Justify places the children along the main axis.
func (e *node) Justify(a Align) *node { e.justify = a; return e }

// AlignItems places the children across the main axis.
func (e *node) AlignItems(a Align) *node { e.align = a; return e }

// AlignSelf places the element across its parent's main axis, overriding
// the parent's AlignItems; in a grid, it places the element in its cell
// vertically.
func (e *node) AlignSelf(a Align) *node { e.self = a; return e }

// Center centers the children along and across the main axis.
func (e *node) Center() *node { e.justify, e.align = Center, Center; return e }

// Absolute takes the element out of its parent's layout and places it with
// Top, Right, Bottom and Left relative to the parent's padding box, above
// its siblings. As in CSS, that box is inside the parent's border but holds
// its padding: Top(0) puts the element just below the border, whatever the
// padding.
func (e *node) Absolute() *node { e.flags |= flagAbsolute; return e }

// Top, Right, Bottom and Left place an Absolute element. On an element in
// its parent's layout, they move it from where the layout put it, as CSS's
// relative positioning does, without moving its siblings.
func (e *node) Top(v float32) *node    { e.inset[0] = px(v); return e }
func (e *node) Right(v float32) *node  { e.inset[1] = px(v); e.unlog(1, logInset); return e }
func (e *node) Bottom(v float32) *node { e.inset[2] = px(v); return e }
func (e *node) Left(v float32) *node   { e.inset[3] = px(v); e.unlog(3, logInset); return e }

// TopPercent, RightPercent, BottomPercent and LeftPercent place the
// element as Top, Right, Bottom and Left do, by a percentage of the
// parent's height or width.
func (e *node) TopPercent(p float32) *node    { e.inset[0] = percent(p); return e }
func (e *node) RightPercent(p float32) *node  { e.inset[1] = percent(p); e.unlog(1, logInset); return e }
func (e *node) BottomPercent(p float32) *node { e.inset[2] = percent(p); return e }
func (e *node) LeftPercent(p float32) *node   { e.inset[3] = percent(p); e.unlog(3, logInset); return e }

// Anchor is a point of a box: a corner, the middle of a side, or the
// center.
type Anchor uint8

// Anchors, from left to right and top to bottom, then those on the side
// lines start and on the other: on the left and the right, or the right
// and the left of an element laid out right to left (Direction), as
// Flutter's AlignmentDirectional.
const (
	AnchorTopLeft Anchor = iota
	AnchorTop
	AnchorTopRight
	AnchorLeft
	AnchorCenter
	AnchorRight
	AnchorBottomLeft
	AnchorBottom
	AnchorBottomRight
	AnchorTopStart
	AnchorStart
	AnchorBottomStart
	AnchorTopEnd
	AnchorEnd
	AnchorBottomEnd
)

// physical returns the anchor on the left or the right that a, which may
// be on the start or the end side, is in direction rtl.
func (a Anchor) physical(rtl bool) Anchor {
	if a < AnchorTopStart {
		return a
	}
	end := a >= AnchorTopEnd
	row := (a - AnchorTopStart) % 3
	if end != rtl {
		return row*3 + 2
	}
	return row * 3
}

// fractions returns where the anchor, on the left or the right, is across
// a box and down it, from 0 to 1.
func (a Anchor) fractions() (fx, fy float32) { return float32(a%3) / 2, float32(a/3) / 2 }

// attachment is where Attach puts an element: 1 + at*15 + self, 0 for
// nowhere.
type attachment uint8

func (a attachment) anchors() (at, self Anchor) { return Anchor((a - 1) / 15), Anchor((a - 1) % 15) }

// flipped returns a with its anchors on the other side across (x) and
// down (y) their boxes.
func (a attachment) flipped(x, y bool) attachment {
	at, self := a.anchors()
	if x {
		at, self = at.acrossFlipped(), self.acrossFlipped()
	}
	if y {
		at, self = at.downFlipped(), self.downFlipped()
	}
	return attachment(1 + at*15 + self)
}

// acrossFlipped returns the anchor on the other side across a box: the
// right for the left, the end for the start.
func (a Anchor) acrossFlipped() Anchor {
	switch {
	case a < AnchorTopStart:
		return a/3*3 + 2 - a%3
	case a < AnchorTopEnd:
		return a + 3
	}
	return a - 3
}

// downFlipped returns the anchor on the other side down a box: the bottom
// for the top.
func (a Anchor) downFlipped() Anchor {
	if a < AnchorTopStart {
		return (2-a/3)*3 + a%3
	}
	base := AnchorTopStart + (a-AnchorTopStart)/3*3
	return base + 2 - (a - base)
}

// Attach takes the element out of its parent's layout, as Absolute does,
// and puts its point self on the point at of the parent's padding box:
// Attach(ui.AnchorTopRight, ui.AnchorCenter) centers a badge on the top
// right corner of its parent, and Attach(ui.AnchorBottomRight,
// ui.AnchorBottomRight) puts a button in the bottom right corner. Top,
// Right, Bottom and Left then move it from there; the element keeps its
// own size.
func (e *node) Attach(at, self Anchor) *node {
	e.flags |= flagAbsolute
	e.attach = attachment(1 + min(at, AnchorBottomEnd)*15 + min(self, AnchorBottomEnd))
	return e
}

// AttachTo takes the element out of its parent's layout, as Attach does,
// and puts its point self on the point at of target's box, wherever target
// is in the window: built in an Overlay, a panel goes below a button with
// AttachTo(button, ui.AnchorBottomLeft, ui.AnchorTopLeft), and to its
// right with AttachTo(button, ui.AnchorRight, ui.AnchorLeft). Where it
// would overflow the window, it goes to the other side of target, or the
// other way along it, if that overflows less, then moves along target into
// the window. Its margins keep it apart from target: a top margin below
// it, and above it as it goes there. Top, Right, Bottom and Left then move
// it; the element keeps its own size.
//
// The element is target's popover: its elements follow target as Tab
// moves, and a press on target is not outside it (PressedOutside). Build
// target before it, in the same frame. AttachedAnchors tells the side it
// went to.
func (e *node) AttachTo(target *node, at, self Anchor) *node {
	if target == nil {
		return e
	}
	e.flags |= flagAbsolute
	e.attach = attachment(1 + min(at, AnchorBottomEnd)*15 + min(self, AnchorBottomEnd))
	e.popover = target
	if e.dir&dirSet == 0 {
		// Built in an Overlay, it reads as what it is attached to.
		e.setDir(target.rtl(), false)
	}
	return e
}

// AttachedAnchors returns the points of its target's box and of its own
// that AttachTo put together as the layout placed the element: those
// asked for, or, where it went to the other side of target or the other
// way along it to stay in the window, those across or down from them, as
// Floating UI's placement: AnchorTop and AnchorBottom for a panel asked
// below its button, with AttachTo(button, ui.AnchorBottom, ui.AnchorTop),
// that went above it. Start and end anchors stay start and end ones. The
// view reads the last frame's, which are those asked for until the
// element is laid out, as in the frame it first shows; Draw reads this
// frame's. From that first frame, AttachedOrigin puts the element's
// transform origin on its own point, and a Motion's Flip mirrors its
// Enter and Exit, on the side the layout chose. An element not attached
// to another returns its Attach anchors, or AnchorTopLeft twice without
// them.
func (e *node) AttachedAnchors() (at, self Anchor) {
	a := e.placed
	if a == 0 && e.attach != 0 && e.hasState() && e.st.asked == e.attach {
		a = e.st.placed
	}
	if a == 0 {
		a = e.attach
	}
	if a == 0 {
		return AnchorTopLeft, AnchorTopLeft
	}
	return a.anchors()
}

// AttachEdgeMargin sets how far an element placed with AttachTo stays
// from the window's edges, v DIPs on each side, where it is 4 unless set:
// a panel that must not touch the edges of a small window keeps 12 from
// them, as the inset-modified containing block of CSS Anchor Positioning
// keeps a positioned box inside the insets given to it. It is the room
// the element flips and moves to stay in, and that one scrolling is
// limited to.
func (e *node) AttachEdgeMargin(v float32) *node {
	e.edgeMargin, e.edgeMarginSet = max(v, 0), true
	return e
}

// AttachedOrigin sets the transform origin on the element's own point
// that AttachTo puts on its target, on whichever side of it the layout
// puts the element (AttachedAnchors), from the frame it shows: a tooltip
// or a menu scaling in, or out, grows from the edge nearest its trigger,
// as Radix's popper transform origin. It replaces TransformOrigin on an
// element attached to another.
func (e *node) AttachedOrigin() *node {
	e.originAttached = true
	return e
}

// KeepFocus leaves the keyboard focus where it is while the element, or
// what is inside it, is pressed: the popups and bars of a control that
// keeps typing while the user picks in them, as a combobox's options or a
// code editor's completions and find buttons.
func (e *node) KeepFocus() *node { e.flags |= flagKeepFocus; return e }

// Access has fn describe the element further to assistive technology,
// as it builds the element's node, only while assistive technology reads
// the window: what costs too much to give every frame, as the text of a
// large document. The node is a type internal to this module, so only
// its packages describe elements so, as package ui/editor does.
func (e *node) Access(fn func(n *platform.AccessNode)) *node { e.accFill = fn; return e }

// AspectRatio makes the height the width divided by r.
func (e *node) AspectRatio(r float32) *node { e.aspect = r; return e }

// Clip hides what the children draw outside the element.
func (e *node) Clip() *node { e.flags |= flagClip; return e }

// ClipX hides what the children draw left and right of the element, and
// ClipY what they draw above and below it.
func (e *node) ClipX() *node { e.flags |= flagClipX; return e }
func (e *node) ClipY() *node { e.flags |= flagClipY; return e }

// Invisible hides the element and its children, which keep their room in
// the layout but draw nothing and take neither the pointer nor the focus,
// as CSS's visibility: hidden does. Hidden takes the room away too.
func (e *node) Invisible() *node { e.flags |= flagInvisible; return e }

// IgnoreInput makes the element and those inside it ignore the pointer
// and the keyboard when ignore is true, while they still show and read to
// assistive technology. The pointer goes through to what is under them,
// as PassThrough lets it: hovering, pressing, scrolling, dragging and the
// cursor are those of what is under them. Neither Tab nor a click gives
// them the focus, and they lose it as they start ignoring input. Assistive
// technology still finds them, with their roles and names, and presses
// them, which Clicked reports; it can neither focus them nor move a
// slider among them with its keys. A WebView inside them still takes the
// pointer, which the system gives its view.
//
// A control a row shows while it is hovered, as a button deleting it, can
// stay built and described at rest, without taking a press meant for the
// row:
//
//	row := ui.Row(c).Padding(8)
//	hovered := row.Hovered()
//	row.Children(func() {
//		ui.Text(c, item.Title).Grow(1)
//		del := ui.Button(c, "Delete").IgnoreInput(!hovered)
//		if !hovered {
//			del.Opacity(0)
//		}
//		if del.Clicked() {
//			removed = item.ID
//		}
//	})
//
// Unlike HTML's inert, it leaves them to assistive technology. Invisible
// and Hidden hide them from it too, and Disabled takes their press away;
// AccessHidden hides them from it alone.
func (e *node) IgnoreInput(ignore bool) *node { e.ignoreInput = ignore; return e }

// AccessHidden leaves the element and those inside it out of what
// assistive technology reads when hide is true, while they still show, as
// HTML's aria-hidden does: none of them is in its tree, their text names
// no element around them, and it can neither press, focus nor edit them.
// The pointer and the keyboard still reach them, unless they IgnoreInput
// too, which makes them inert, as HTML's inert does; without it, keep the
// keyboard focus out of them, where assistive technology cannot follow.
//
// What shows but is not there to read is so read once, or not at all: a
// page sliding out beside the one that replaces it, the cards peeking from
// behind the front one of a stack, a copy of a text fading into another:
//
//	if app.leaving >= 0 {
//		page(c, app.leaving).Absolute().Translate(-x, 0).IgnoreInput(true).AccessHidden(true)
//	}
//	page(c, app.tab).Translate(width-x, 0)
//
// RoleNone leaves out the element but not those inside it.
func (e *node) AccessHidden(hide bool) *node { e.accessHidden = hide; return e }

// AccessDisabled tells assistive technology that the element and those
// inside it are unavailable when off is true, as HTML's aria-disabled
// does, while they keep the keyboard focus, their place in the Tab order,
// the pointer and their look: a screen reader still reaches them and reads
// them, as dimmed or unavailable, with their Description, which can say
// why. It is for what the user must still find while it cannot be used,
// as a menu item whose action is blocked, a tab disabled but still in the
// arrows' order, or a submit button while the form is not valid:
//
//	del := ui.Button(c, "Delete project").AccessDisabled(app.locked)
//	if app.locked {
//		del.Description("Unlock the project to delete it")
//	}
//	if del.Clicked() && !app.locked {
//		app.deleteProject()
//	}
//
// It only tells: clicks and keys still reach the element, which the view
// ignores while it is unavailable, and it does not dim it, which the view
// styles itself if it wants. Assistive technology focuses it, but neither
// presses nor edits it. Disabled does all of it at once: no clicks, no
// focus, and half the opacity.
func (e *node) AccessDisabled(off bool) *node { e.accessDisabled = off; return e }

// Hidden hides the element and its children when hide is true, as CSS's
// display: none does: unlike an Invisible element, it takes no room in
// the layout, nor a gap between its siblings, and draws nothing, takes
// neither the pointer nor the focus, and is not read by assistive
// technology. It is still built, so that what is inside it keeps its
// state (scroll offsets, text being edited, animations) while it is
// hidden, and shows as it was once hide is false again:
//
//	ui.Column(c).Hidden(!app.showDetails).Children(func() { ... })
//
// The focus leaves it as it hides, and what is attached to it
// (AttachTo) hides with it. A List's rows are built as the list lays out,
// so a hidden List builds none.
func (e *node) Hidden(hide bool) *node { e.hidden = hide; return e }

// inHidden reports whether the element or one around it is Hidden.
func (e *node) inHidden() bool {
	for p := e; p != nil; p = p.parent {
		if p.hidden {
			return true
		}
	}
	return false
}

// Debug outlines the element and every element inside it, with their
// padding and margins, to see the layout.
func (e *node) Debug() *node { e.flags |= flagDebug; return e }

// Background fills the element, in place of a gradient.
func (e *node) Background(c Color) *node {
	e.bg = c
	if e.fill == fillGradient || e.fill == fillConic || e.fill == fillRadial {
		e.fill = fillColor
	}
	return e
}

// Gradient fills the element with a linear gradient from one color to
// another, at angle degrees clockwise from upwards as in CSS: 180 goes
// from top to bottom, 90 from left to right.
func (e *node) Gradient(from, to Color, angle float32) *node {
	return e.LinearGradient(LinearGradient{From: from, To: to, Angle: angle})
}

// LinearGradient fills the element with a gradient, of which Gradient sets
// only the colors and the angle.
func (e *node) LinearGradient(g LinearGradient) *node {
	e.grad, e.fill = g, fillGradient
	return e
}

// ConicGradient fills the element with a conic gradient, its colors
// turning around a point (CSS's conic-gradient()):
//
//	ui.Box(c).Size(80, 80).Radius(40).ConicGradient(ui.ConicGradient{
//		Stops: []ui.GradientStop{{red, 0}, {yellow, 1.0 / 3}, {blue, 2.0 / 3}, {red, 1}},
//	})
func (e *node) ConicGradient(g ConicGradient) *node {
	e.gradients().conic, e.fill = g, fillConic
	return e
}

// RadialGradient fills the element with a radial gradient, its colors
// spreading out from a point (CSS's radial-gradient()), as a glow:
//
//	ui.Box(c).Size(160, 100).Radius(16).RadialGradient(ui.RadialGradient{
//		Stops: []ui.GradientStop{{ui.RGBA(99, 102, 241, 0.6), 0}, {ui.Transparent, 1}},
//	})
func (e *node) RadialGradient(g RadialGradient) *node {
	e.gradients().radial, e.fill = g, fillRadial
	return e
}

// BorderLinearGradient paints the element's border (Border, BorderWidth)
// with a linear gradient across the element, in place of its color.
func (e *node) BorderLinearGradient(g LinearGradient) *node {
	gr := e.gradients()
	gr.borderLinear, gr.border = g, borderLinear
	return e
}

// BorderConicGradient paints the element's border (Border, BorderWidth)
// with a conic gradient around the element, in place of its color: a ring
// of colors, as SwiftUI's strokeBorder with an AngularGradient draws.
//
//	ui.Box(c).Size(120, 80).Radius(16).Border(4, ui.Color{}).BorderConicGradient(ui.ConicGradient{Stops: rainbow})
func (e *node) BorderConicGradient(g ConicGradient) *node {
	gr := e.gradients()
	gr.borderConic, gr.border = g, borderConic
	return e
}

// Blur blurs the element, with its children, by sigma DIPs, the standard
// deviation of a Gaussian, as CSS's filter: blur() does: what it draws
// spreads that far around, transparent beyond, on the GPU. It renders
// into a bitmap first, as Effect does, about 3 sigma larger each way, and
// an element with an Effect draws through it instead. Hit testing, focus
// and accessibility do not change: only what shows.
func (e *node) Blur(sigma float32) *node { e.blur = max(sigma, 0); return e }

// Stripes draws stripes of c over the background, width DIPs wide with
// gap DIPs between them, running at angle degrees clockwise from upwards:
// 0 draws vertical stripes, 90 horizontal ones, and 45 slanting ones like
// slashes, as to mark what is unavailable.
func (e *node) Stripes(c Color, width, gap, angle float32) *node {
	e.stripes, e.fill = stripes{c, width, gap, angle}, fillStripes
	return e
}

// Border draws a border of width DIPs inside the element's edges, on every
// side; BorderWidth sets different widths. As in CSS, the border takes room
// within the element's size: the padding and the children are inside it.
func (e *node) Border(width float32, c Color) *node {
	e.border, e.borderC = [4]float32{width, width, width, width}, c
	e.logical &^= logBorder
	return e
}

// BorderWidth sets the widths of the border on each side, CSS style: all
// sides, vertical and horizontal, or top, right, bottom and left. A line
// below a header:
//
//	ui.Row(c).BorderWidth(0, 0, 1, 0).BorderColor(t.Border)
func (e *node) BorderWidth(v ...float32) *node {
	e.border = edges(v)
	e.logical &^= logBorder
	return e
}

// BorderColor sets the color of the border.
func (e *node) BorderColor(c Color) *node { e.borderC = c; return e }

// BorderStyle sets whether the border is solid, as by default, or dashed.
func (e *node) BorderStyle(s BorderStyle) *node { e.borderStyle = s; return e }

// Dividers draws a line width DIPs thick in color c between each two
// children of a row, a column or a List (between their rows), across the
// element, inside its border: in the middle of the room between them,
// which the lines do not take, so give the element a Gap at least as
// wide. A row that wraps draws them between the children of each line;
// grids draw none.
func (e *node) Dividers(width float32, c Color) *node {
	if width > 0 && c.A > 0 {
		e.flags |= flagDividers
		e.c.dividers = append(e.c.dividers, dividers{e, width, c})
	}
	return e
}

// dividers are the lines an element draws between its children.
type dividers struct {
	e     *node
	width float32
	color Color
}

// Radius rounds the corners: one radius for all, or top-left, top-right,
// bottom-right and bottom-left.
func (e *node) Radius(r ...float32) *node {
	switch len(r) {
	case 1:
		e.radius = [4]float32{r[0], r[0], r[0], r[0]}
	case 4:
		e.radius = [4]float32{r[0], r[1], r[2], r[3]}
	}
	return e
}

// Shadow adds a box shadow, offset by x and y, blurred by blur and grown by
// spread DIPs. As CSS's box-shadow, it shows only outside the box: a
// translucent background does not show it through.
func (e *node) Shadow(x, y, blur, spread float32, c Color) *node {
	e.shadows = append(e.shadows, shadow{x, y, blur, spread, c, false})
	return e
}

// InsetShadow adds an inner box shadow, as CSS's box-shadow: inset: offset
// by x and y, blurred by blur and grown inward by spread DIPs. It shows
// only inside the box's padding edge, within its border and rounded
// corners, above the background and below the content. Inner shadows
// stack in the order they are added, as outer ones do.
func (e *node) InsetShadow(x, y, blur, spread float32, c Color) *node {
	e.shadows = append(e.shadows, shadow{x, y, blur, spread, c, true})
	return e
}

// NoShadow takes off the outer and inner shadows added so far, as CSS's
// box-shadow: none, so that the Shadow and InsetShadow calls after it
// start afresh.
func (e *node) NoShadow() *node { e.shadows = e.shadows[:0]; return e }

// Opacity makes the element and its children translucent as one group,
// as CSS's opacity does: what they paint is composited first and then
// faded, so that children overlapping one another, or the element's
// background under them, never show through each other. Where nothing
// they paint overlaps, they paint faded each on its own, which looks the
// same and costs nothing more; otherwise they render into a layer of the
// GPU first, in which text is smoothed in grayscale, as in Cache's. A web
// view or a material reading its backdrop inside the element shows
// through no layer: there each thing fades on its own. Disabled's dimming
// fades each thing on its own too.
func (e *node) Opacity(o float32) *node {
	linkGroups()
	e.opacity, e.opacitySet = max(0, min(o, 1)), true
	return e
}

// Cursor sets the pointer's shape over the element.
func (e *node) Cursor(c Cursor) *node { e.cursor = c + 1; return e }

// FontSize sets the size of text in DIPs, for the element's text and its
// descendants'.
func (e *node) FontSize(v float32) *node { e.ts.size = v; e.ts.set |= setSize; return e }

// FontWeight sets the weight of text from 100 (thin) to 900 (black).
func (e *node) FontWeight(w int) *node { e.ts.weight = w; e.ts.set |= setWeight; return e }

// Bold sets a bold font weight.
func (e *node) Bold() *node { return e.FontWeight(700) }

// Italic sets an italic font.
func (e *node) Italic() *node { e.ts.italic = true; e.ts.set |= setItalic; return e }

// Font sets the font family, a comma-separated list: the first family the
// system or the app has draws the text, and the others, in order, what it
// lacks, before the system's choice. "monospace" and "system-ui" are the
// system's own fonts.
func (e *node) Font(family string) *node { e.ts.family = family; e.ts.set |= setFamily; return e }

// TextColor sets the color of text.
func (e *node) TextColor(c Color) *node { e.ts.color = c; e.ts.set |= setColor; return e }

// SelectionColor sets the highlight of selected text in the element and
// the text inside it, as text on a colored bubble needs one that shows
// on it; the theme's Selection is the highlight elsewhere.
func (e *node) SelectionColor(c Color) *node {
	e.ts.selection = c
	e.ts.set |= setSelection
	return e
}

// selectionColor is the highlight of the style's selected text.
func (ts *textStyle) selectionColor(t *Theme) Color {
	if ts.set&setSelection != 0 {
		return ts.selection
	}
	return t.Selection
}

// CaretColor sets the color of the insertion caret of the text inputs in
// the element, as CSS's caret-color does, a field's own text color for
// one; the theme's accent is the color elsewhere.
func (e *node) CaretColor(c Color) *node {
	e.ts.caret = c
	e.ts.set |= setCaret
	return e
}

// caretColor is the color of the style's insertion caret.
func (ts *textStyle) caretColor(t *Theme) Color {
	if ts.set&setCaret != 0 {
		return ts.caret
	}
	return t.Accent
}

// LineHeight sets the height of lines of text as a multiple of the font
// size.
func (e *node) LineHeight(m float32) *node {
	e.ts.lineHeight, e.ts.fixedLine = m, false
	e.ts.set |= setLineHeight
	return e
}

// FixedLineHeight sets the height of lines of text in DIPs, whatever the
// font size, as for rows of text that line up with a grid.
func (e *node) FixedLineHeight(v float32) *node {
	e.ts.lineHeight, e.ts.fixedLine = v, true
	e.ts.set |= setLineHeight
	return e
}

// TextAlign aligns the lines of text: Start, Center or End.
func (e *node) TextAlign(a Align) *node { e.ts.align = a; e.ts.set |= setAlign; return e }

// Underline underlines text.
func (e *node) Underline() *node {
	e.ts.underline, e.ts.wavy, e.ts.dotted, e.ts.solid = true, false, false, false
	e.ts.set |= setUnderline
	return e
}

// WavyUnderline underlines text with a wave, as spell checkers mark words.
func (e *node) WavyUnderline() *node {
	e.ts.underline, e.ts.wavy, e.ts.dotted, e.ts.solid = true, true, false, false
	e.ts.set |= setUnderline
	return e
}

// DottedUnderline underlines text with a line of dots, as links mark
// hover.
func (e *node) DottedUnderline() *node {
	e.ts.underline, e.ts.wavy, e.ts.dotted, e.ts.solid = true, false, true, false
	e.ts.set |= setUnderline
	return e
}

// DecorationColor sets the color of underlines and strikethroughs, which
// is the text's otherwise.
func (e *node) DecorationColor(c Color) *node {
	e.ts.decoColor = c
	e.ts.set |= setDecoColor
	return e
}

// DecorationThickness sets the thickness of underlines and strikethroughs
// in DIPs, which follows the font size otherwise.
func (e *node) DecorationThickness(v float32) *node {
	e.ts.decoThick = v
	e.ts.set |= setDecoThick
	return e
}

// UnderlineOffset puts underlines v DIPs below the baseline, measured to
// their top, or above it when negative, as CSS's text-underline-offset:
// a link's underline a few pixels lower than the font puts it, clear of
// the descenders. Underlines skipping the ink of descenders skip it where
// they then are. The font places them otherwise.
func (e *node) UnderlineOffset(v float32) *node {
	e.ts.decoOffset = v
	e.ts.set |= setDecoOffset
	return e
}

// TextBackground fills the lines of text behind it with c, as a
// highlight.
func (e *node) TextBackground(c Color) *node {
	e.ts.background = c
	e.ts.set |= setBackground
	return e
}

// TextStroke strokes the outlines of the glyphs of text width DIPs wide
// in c, under their fill, as CSS's -webkit-text-stroke with paint-order:
// stroke fill: the half of the stroke outside the glyphs shows around
// them, a halo of c, as a chart's labels over its bars take one of the
// background's color to stand out. A text's strokes all go under all
// its glyphs, so that one glyph's stroke never covers its neighbor; the
// stroke goes around the edge of the pixels of color glyphs, as emoji.
// Underlines and strikethroughs are not stroked. A width of 0 strokes
// nothing, where the elements around it stroke their text.
func (e *node) TextStroke(width float32, c Color) *node {
	linkStrokes()
	e.ts.strokeWidth, e.ts.strokeColor = max(width, 0), c
	e.ts.set |= setStroke
	return e
}

// TextShadow casts a shadow of the element's text, as CSS's text-shadow:
// the shapes of its glyphs, offset by x and y DIPs, blurred by blur, in c,
// under the glyphs and their strokes, as a chart's labels over its bars
// or a caption over a photo take one to stand out. The text of the
// elements inside takes it too, and theirs where they set one; a
// transparent c casts none, where those around it cast one. A span's
// Shadow sets one for its text. The shadow of color glyphs, as emoji, is
// that of their pixels' shape; underlines and strikethroughs cast none.
//
//	ui.Text(c, "Golden hour").TextColor(ui.RGB(255, 255, 255)).TextShadow(0, 1, 4, ui.RGBA(0, 0, 0, 0.6))
func (e *node) TextShadow(x, y, blur float32, c Color) *node {
	e.ts.shadow = NewTextShadow(x, y, blur, c)
	e.ts.set |= setShadow
	return e
}

// TabSize puts the tab stops of the element's text every n spaces of its
// font from the start of each line, as CSS's tab-size: a tab advances
// to the next of them, as a code block indents with tabs of 2 or 4
// columns. A space is as wide as the font's, letter spacing included.
// Without it, or with n at 0, tabs advance as the system's text stack
// sets them, which differs between systems.
func (e *node) TabSize(n float32) *node {
	if n > 0 {
		linkTabs.Do(text.Shared().UseTabSize)
	}
	e.ts.tabSize = max(n, 0)
	e.ts.set |= setTabSize
	return e
}

// linkTabs lets the text system set tab stops once an element sets a tab
// size, so that apps that never do not link it.
var linkTabs sync.Once

// Strikethrough strikes text through.
func (e *node) Strikethrough() *node { e.ts.strike = true; e.ts.set |= setStrike; return e }

// LetterSpacing adds v DIPs after every character of text, or tightens it
// with a negative v, as for labels in capitals; 0 takes away that of the
// text around it, as for a number within such a label.
func (e *node) LetterSpacing(v float32) *node {
	e.ts.spacing = v
	e.ts.set |= setSpacing
	return e
}

// FontFeatures turns on OpenType features of the font, by tag, or sets
// them with tag=value:
//
//	ui.Textf(c, "%d items", n).FontFeatures("tnum")   // digits of one width
//	ui.Text(c, "office").FontFeatures("liga=0")       // no ligatures
//
// A font without a feature ignores it.
func (e *node) FontFeatures(features ...string) *node {
	e.ts.features = strings.Join(features, ",")
	e.ts.set |= setFeatures
	return e
}

// Lang sets the language of the element's text, and of the text of the
// elements inside it, as a BCP 47 tag, as HTML's lang attribute does: the
// system's text stack draws the glyphs of that language, as the Han
// characters Chinese and Japanese draw apart, and falls back to its
// fonts. Text is in the language the app shows (mygo.App.Locale) unless
// told otherwise: a Japanese quote in a Chinese app,
//
//	ui.Text(c, "直").Lang("ja")
//
// Korean text ("ko", "ko-KR") breaks its lines between words, at spaces
// and after punctuation, as Korean is written and as CSS's word-break:
// keep-all has it, rather than between any two syllables; a word longer
// than a line still breaks within it. "" takes the language of the
// elements around it. Browsers draw text in the page's language whatever
// the element's.
func (e *node) Lang(tag string) *node {
	e.ts.lang = tag
	e.ts.set |= setLang
	if tag == "" {
		e.ts.set &^= setLang
	}
	return e
}

// MaxLines shows at most n lines of the element's text, ending it with an
// ellipsis.
func (e *node) MaxLines(n int) *node { e.maxLines = n; return e }

// SingleLine keeps the element's text on one line, ending it with an
// ellipsis when it does not fit.
func (e *node) SingleLine() *node { e.single = true; e.maxLines = 1; return e }

// NoWrap keeps each line of the element's text whole, breaking it only at
// newlines, even where it overflows the element.
func (e *node) NoWrap() *node { e.noWrap = true; return e }

// Balance wraps the element's text in lines of about the same length, as
// CSS's text-wrap: balance does: a heading or a short callout that takes
// two lines takes two of half its length each, rather than a full line
// and a word alone on the next. The text takes as many lines as it would
// without it, at the width layout gives it, and keeps its alignment
// within that width. Each paragraph is balanced on its own, once a
// layout, as the text system keeps it.
func (e *node) Balance() *node { e.balance = true; return e }

// Pretty keeps the last line of each paragraph of the element's text from
// holding a single word, as CSS's text-wrap: pretty does at least: the
// line before gives it a word, and the lines before stay as they are. A
// paragraph of body text then ends on more than one word, at no cost to
// the rest of its lines; Balance balances them all instead.
func (e *node) Pretty() *node {
	linkPretty.Do(text.Shared().UsePretty)
	e.pretty = true
	return e
}

// linkPretty lets the text system wrap text pretty once an element or a
// TextShape asks for it, so that apps that never do not link it.
var linkPretty sync.Once

// PreserveSpaces keeps the spaces the element's text ends its lines with,
// and a text of only spaces, with their advance, as CSS's white-space:
// break-spaces does: a gap made of spaces between two texts in a row, or
// the padding a monospaced column is aligned with. Without it spaces
// ending a line take no room, as with white-space: normal.
func (e *node) PreserveSpaces() *node { e.keepSpaces = true; return e }

// Ellipsis sets what ends text that MaxLines or SingleLine cuts, "…" by
// default.
func (e *node) Ellipsis(s string) *node { e.ellipsis = s; return e }

// Label names the element for assistive technology and for finding it in
// tests, when its text does not.
func (e *node) Label(s string) *node { e.label = s; return e }

// Disabled disables the element and those inside it when d is true: they
// report no clicks, take no focus, and show at half their opacity, unless
// DimDisabled turns that off. AccessDisabled keeps their clicks and their
// focus, for what assistive technology must still reach.
func (e *node) Disabled(d bool) *node {
	if d {
		e.flags |= flagDisabled
	} else {
		e.flags &^= flagDisabled
	}
	return e
}

// DimDisabled sets whether the element shows at half its opacity, with
// what is inside it, while Disabled disables it, as it does by default,
// bases too; an element disabled by one around it dims with that one. A
// widget of your own that shows being disabled in a look of its own, as
// a card that keeps the reason it is unavailable readable, turns it off
// and styles itself while IsDisabled.
func (e *node) DimDisabled(dim bool) *node { e.undimmed = !dim; return e }

// IsDisabled reports whether the element or an ancestor is disabled.
func (e *node) IsDisabled() bool {
	for p := e; p != nil; p = p.parent {
		if p.flags&flagDisabled != 0 {
			return true
		}
	}
	return false
}

// disabled reports whether the element is disabled, or was in the last
// frame, which the input since acted on: an element around it may disable
// it after building it.
func (e *node) disabled() bool {
	return !e.hasState() || e.IsDisabled() || e.st.flags&flagDisabled != 0
}

// hasState lets input queries treat nil and cleared elements as absent.
// It cannot recognize an old pointer whose arena slot has been reused.
func (e *node) hasState() bool {
	return e != nil && e.c != nil && e.c.rt != nil && e.st != nil && !e.c.rt.closed
}

// Focusable lets the element take the keyboard focus, by a click or Tab.
func (e *node) Focusable() *node { e.flags |= flagFocusable; return e }

// TabStop makes the element focusable, and sets whether Tab and Shift+Tab
// stop at it, as they do at a Focusable element. One that is no tab stop
// is HTML's tabindex="-1": it takes the focus from a click, from Focus and
// AutoFocus, and from assistive technology, while Tab passes over it, on
// to what is inside it and after it: a container that takes the focus to
// hold its keys, as a card or a dialog's panel, without coming before its
// own controls.
func (e *node) TabStop(stop bool) *node {
	e.flags |= flagFocusable
	if stop {
		e.flags &^= flagFocusTarget
	} else {
		e.flags |= flagFocusTarget
	}
	return e
}

// DragWindow makes the element a handle that moves the window, such as the
// title bar of a frameless window. A double click on it maximizes the
// window, as on a title bar.
func (e *node) DragWindow() *node { e.flags |= flagDragWindow; return e }

// MaximizeButton marks the element as the window's maximize button, which
// the view draws in a window with a hidden title bar without the system's
// (mygo.WindowOptions.HideWindowButtons): on Windows 11, holding the
// pointer over it shows the snap layouts, as over the system's. It takes
// the pointer as any element does, and the view maximizes the window when
// it is clicked (mygo.Window.Maximize). The first such element of a frame
// counts; elsewhere it does nothing.
func (e *node) MaximizeButton() *node {
	e.maximizeButton = true
	placeMaximizeButton = sendMaximizeButton
	return e
}

// PassThrough lets the pointer reach what is under the element.
func (e *node) PassThrough() *node { e.flags |= flagPassThrough; return e }

// HitTest sets where in its box the element takes the pointer: where fn
// reports true for the pointer at (x, y), in the coordinates Draw paints
// in, r being its box as Draw gets it. Elsewhere the pointer reaches what
// is under it, as with PassThrough, so that a pie's segment, a map's
// region or a ring takes a press, the hover and the cursor only where it
// paints, as SVG's pointer-events: visiblePainted. The elements inside it
// take the pointer as they would; fn runs as the pointer moves and
// presses, on the window's thread, and only tests:
//
//	ring := func(r ui.Rect) *ui.Path {
//		var p ui.Path
//		cx, cy := r.X+r.W/2, r.Y+r.H/2
//		return p.Circle(cx, cy, r.W/2).Circle(cx, cy, r.W/2-12).FillRule(ui.EvenOdd)
//	}
//	ui.Box(c).Size(120, 120).
//		Draw(func(p *ui.Painter, r ui.Rect) { p.FillPath(ring(r), accent) }).
//		HitTest(func(x, y float32, r ui.Rect) bool { return ring(r).Contains(x, y) }).
//		OnClick(func() { … })
func (e *node) HitTest(fn func(x, y float32, r Rect) bool) *node { e.hitFn = fn; return e }

// FocusRing sets whether MyGo rings the element when it has the keyboard
// focus from the keyboard, as it does by default. Widgets that ring a part
// of themselves instead, as a check box its box, turn it off and draw
// their own with Painter.FocusRing while FocusVisible.
func (e *node) FocusRing(show bool) *node {
	if show {
		e.flags &^= flagOwnRing
	} else {
		e.flags |= flagOwnRing
	}
	return e
}

// FocusRingOffset sets how far outside the element's edge its focus ring
// goes, in DIPs, in place of the theme's FocusRingOffset: 0 puts it on
// the edge, and a negative offset inside it, as for an element whose
// ancestor clips what goes past it, as a header in an accordion's box.
// Its corners follow the element's, as CSS's outline does. The widgets
// that ring a part of themselves, as a check box its box, ring it so.
func (e *node) FocusRingOffset(d float32) *node {
	e.ringOffset = d
	e.ringSet |= ringOffsetSet
	return e
}

// FocusRingColor sets the color of the element's focus ring, in place of
// the theme's Focus, as for an element on a background the theme's ring
// does not stand out on.
func (e *node) FocusRingColor(color Color) *node {
	e.ringColor = color
	e.ringSet |= ringColorSet
	return e
}

// What an element sets of its focus ring (FocusRingOffset,
// FocusRingColor).
const (
	ringOffsetSet uint8 = 1 << iota
	ringColorSet
)

// Draw paints on the element with p after its background, before its
// children; r is its box. fn only paints: it may run more than once a
// frame.
func (e *node) Draw(fn func(p *Painter, r Rect)) *node { e.paintFn = fn; return e }

// DrawOver paints on the element with p after its children.
func (e *node) DrawOver(fn func(p *Painter, r Rect)) *node { e.paintAfterFn = fn; return e }

// DrawObjects paints the inline objects of a text (Span.ObjectWidth)
// with p, after its glyphs: fn gets each one that shows, its rune in
// the text, the text elements' inside it included, and its box, in
// window DIPs, ObjectHeight above the baseline and ObjectDescent below.
// It paints in the text's clip and in the transform of the element
// holding the object. fn only paints: it may run more than once a frame.
//
//	ui.RichText(c, ui.Span{Text: "area "}, ui.Span{Text: "\uFFFC", ObjectWidth: w, ObjectHeight: h, ObjectDescent: d}).
//		DrawObjects(func(p *ui.Painter, at int, r ui.Rect) { formula.Draw(p, r) })
func (e *node) DrawObjects(fn func(p *Painter, at int, r Rect)) *node {
	linkObjects.Do(func() { paintObjects = (*Painter).textObjects })
	e.objectFn = fn
	return e
}

// ID returns the element's identity, stable from frame to frame.
func (e *node) ID() uint64 { return e.id }

// Bounds returns the previous frame's transformed bounding rectangle in
// window DIPs, the content's when zoomed (Context.SetZoom); it is empty
// for an element the previous frame lacked.
// LayoutBounds returns its box before visual transforms.
func (e *node) Bounds() Rect {
	if !e.hasState() {
		return Rect{}
	}
	s := e.st
	return transformRect(s.world, Rect{s.x, s.y, s.w, s.h})
}

// add appends a child.
func (e *node) add(child *node) {
	// Lists build rows as they lay out, after the list was measured.
	e.forgetIntrinsic()
	child.parent = e
	child.depth = e.depth + 1
	if child.zIndex != 0 {
		e.zOrder = true // set before it was added, as on a copy leaving (ghostOf)
	}
	if e.last == nil {
		e.first = child
	} else {
		e.last.next = child
	}
	e.last = child
	e.nchild++
}
