package platform

import (
	"github.com/egoist/mygo/transfer"
	"image"
)

// Surface is the drawing area of a window created with
// WindowOptions.Surface, which shows content MyGo draws itself (package
// ui) instead of a webview. Its methods run on the main thread.
type Surface interface {
	// Native returns the native objects a GPU renderer draws into, which it
	// calls before making one: a surface that shows frames drawn in memory
	// through those objects (Windows' Composed) lets go of them then.
	Native() SurfaceNative
	// Size returns the size of the drawing area in DIPs and how many
	// device pixels a DIP is.
	Size() (width, height, scale float64)
	// RequestFrame asks for a SurfaceFrame event at the next opportunity
	// to draw: the next display refresh, WM_PAINT or GTK draw. Requests
	// made before it comes coalesce.
	RequestFrame()
	// RefreshRate returns how many times a second the display showing the
	// surface refreshes, at most, 0 when unknown.
	RefreshRate() float64
	// PresentPixels shows a frame drawn in memory: premultiplied BGRA rows
	// of stride bytes, width×height device pixels. It is called while
	// handling a SurfaceFrame event.
	PresentPixels(pix []byte, stride, width, height int)
	// SetCursor sets the pointer's shape over the surface.
	SetCursor(c Cursor)
	// SetTextInput turns text input (IME composition) on for an editable
	// element, telling input methods where its caret is and the text
	// around it, or off.
	SetTextInput(t TextInputState)
	// UpdateAccessibility gives assistive technology the content's
	// elements. The content calls it after every frame once the surface
	// sent AccessibilityOn.
	UpdateAccessibility(tree *AccessTree)
	// StartDataDrag starts a native transfer; CancelDataDrag cancels the
	// source owned by this surface. Both run on the main thread.
	StartDataDrag(DragRequest)
	CancelDataDrag()
	// SetDropFormats registers the formats the content can receive.
	SetDropFormats([]transfer.Format)
	// NewWebView creates a web view under the surface, hidden until
	// PlaceWebViews shows it. Of opts, the options of a webview apply
	// (UserScripts, Schemes, DevTools, UserAgent, Zoom, BackgroundColor,
	// Transparent); h hears of its page.
	NewWebView(opts *WindowOptions, h WindowHandler) (WebView, error)
	// PlaceWebViews shows the web views of the surface where the content's
	// last frame shows them, in the order it painted them, and hides the
	// others, which keep their pages. A press on one, which takes it,
	// comes as WebViewPress.
	PlaceWebViews(views []WebViewPlacement)
}

// DamageSurface is a Surface that shows a frame drawn in memory by what
// changed since the last one: Linux's, whose toolkit then repaints, and
// the compositor takes, only that.
type DamageSurface interface {
	// PresentDamage is PresentPixels for a frame that differs from the
	// last one presented only within damage, in device pixels.
	PresentDamage(pix []byte, stride, width, height int, damage []image.Rectangle)
}

// OccludableSurface is a Surface that tells when nothing of it shows, as
// when other windows cover its window, while the system may still give it
// frames: the content then draws nothing that moves until it shows again.
type OccludableSurface interface {
	// Occluded reports whether nothing of the surface shows on screen. The
	// surface sends SurfaceHidden as none of it shows any more, and
	// SurfaceShown once some of it shows again.
	Occluded() bool
}

// WideGamutSurface is a Surface that tells whether its window's screen
// shows colors outside the sRGB gamut (macOS's).
type WideGamutSurface interface {
	// WideGamut reports whether the screen showing most of the window
	// shows colors outside the sRGB gamut, as Display P3 screens do.
	WideGamut() bool
}

// GlassPlacement is where a surface shows the system's glass
// (Surface.PlaceGlass), under its content, in DIPs from its top left.
type GlassPlacement struct {
	// ID identifies the element showing the glass from one frame to the
	// next.
	ID uint64
	// Frame is the glass's box; Clip the part of it that shows.
	Frame, Clip RectF
	// Radii are the corners of the hole the content leaves, clockwise
	// from the top left, and CornerRadius the one the glass view takes,
	// where it takes one.
	Radii        [4]float64
	CornerRadius float64
	// Style is NSGlassEffectView's style: 0 regular, 1 clear.
	Style int
	// Tint colors the glass, nil for none.
	Tint *Color
}

// GlassSurface is a Surface that draws the system's glass under its
// content, as macOS 26's NSGlassEffectView does, where the content leaves a
// hole.
type GlassSurface interface {
	// NativeGlassSupported reports whether the system's glass is available.
	NativeGlassSupported() bool
	// PlaceGlass shows the glass where the content's last frame shows it,
	// and hides the rest.
	PlaceGlass(places []GlassPlacement)
}

// MaterialSurface is a Surface whose window can show a material
// (vibrancy) behind the content, wherever its frames are transparent.
type MaterialSurface interface {
	// ShowsMaterial reports whether the window shows a material behind the
	// content now: what its frames leave transparent shows it, rather than
	// nothing.
	ShowsMaterial() bool
}

// PacedSurface is a Surface whose frames following each other come at the
// display's refreshes, marked Paced: a renderer that waits for the display
// lets such a frame draw once the display can take it, with FrameNow.
type PacedSurface interface {
	// FrameNow sends a SurfaceFrame at once, as a refresh would, for a
	// frame a Paced one left to draw.
	FrameNow()
}

// RefreshSurface is a PacedSurface whose frames drawn in memory
// (PresentPixels) show at once, and whose frames asked for (RequestFrame)
// come at once, not at the display's refreshes, as Windows' do: after
// each frame drawn in memory, the content waits for the next refresh off
// the main thread, and Paced frames that come meanwhile wait for it.
type RefreshSurface interface {
	// AwaitRefresh returns once the display shows its next refresh, or
	// at once when nothing is to show. It blocks, and is called from a
	// goroutine of its own, never the main thread.
	AwaitRefresh()
}

// FrameRateSurface is a Surface that can have its display refresh less
// often than it can while frames come at a lower rate, as macOS's display
// links do on ProMotion displays.
type FrameRateSurface interface {
	// SetFrameRate tells how many frames a second come from now on while
	// they follow each other, a whole fraction of RefreshRate, or 0 for
	// one each refresh.
	SetFrameRate(fps float64)
}

// ImmediateFrameSurface is a Surface whose RequestFrame sends a
// SurfaceFrame at once, as Windows' does, rather than at the display's next
// refresh: a frame due at a later refresh waits on a timer instead.
type ImmediateFrameSurface interface {
	// FramesAtOnce marks the surface.
	FramesAtOnce()
}

// MaximizeButtonSurface is a Surface whose window makes the maximize
// button its content draws (in a window without its own, as
// WindowOptions.HideWindowButtons) the system's for the pointer: Windows
// shows the snap layouts over it. Package ui reaches it only once a view
// marks such a button (ui.Element.MaximizeButton), for apps that mark none
// not to link it.
type MaximizeButtonSurface interface {
	// SetMaximizeButton tells where the content shows the window's
	// maximize button, in DIPs from the top-left corner of the content.
	// The zero RectF is nowhere.
	SetMaximizeButton(r RectF)
}

// IdleSurface is a Surface that can give back memory once frames stop.
type IdleSurface interface {
	// Idle tells that no frame came for a while: the surface may give
	// back memory that drawing frames took and freed.
	Idle()
}

// BackgroundSurface is a Surface told the color the content's frames fill
// the window with, which its window shows where no frame of its size
// shows yet: on Windows, DWM can show a window at its new size, as the
// user drags its border, before the frame of that size, and would show
// black where the window grew.
type BackgroundSurface interface {
	// SetBackground sets the color, opaque, or transparent for none.
	SetBackground(c Color)
}

// FirstFrameSurface is a Surface told when the content's first frame
// reached it: a window made hidden draws that frame while hidden, which
// no paint or refresh of the display asks for, and which makes it ready
// to show. Windows' window also stays out of sight until then, rather
// than show what it holds before, white.
type FirstFrameSurface interface {
	// FirstFrame tells that the content presented its first frame. It is
	// called once, within the frame, before the window is told it is
	// ready to show.
	FirstFrame()
}

// TextInputState is the state of the text input that has the keyboard,
// for input methods.
type TextInputState struct {
	// Active is true while an editable element has the keyboard.
	Active bool
	// Caret is the caret's rectangle, in DIPs relative to the surface.
	Caret RectF
	// Text is the input's text around the caret, and Start and End are the
	// selection in it, in runes: both the caret when nothing is selected.
	// Input methods read it to edit what was typed before, as the accents
	// of macOS's press and hold replace the letter they decorate.
	Text       string
	Start, End int
	// Client supplies full text and geometry for a custom text element. The
	// Text/Start/End snapshot remains the plain-widget and TextCaret fallback.
	Client TextInputClient
	// NoInputMethod keeps input methods out while Active: keys type the
	// characters of the keyboard's layout as TextInput, and nothing is
	// composed, as winit's set_ime_allowed(false). Browsers cannot turn
	// input methods off, and ignore it.
	NoInputMethod bool
}

// SurfaceNative holds the native objects of a Surface.
type SurfaceNative struct {
	// HWND is the surface's child window (Windows).
	HWND uintptr
	// Composed tells that the window has no redirection bitmap, which
	// would hide its material (Windows): a GPU renderer presents through
	// DirectComposition, with the frames' alpha, which the material shows
	// through.
	Composed bool
	// View is the surface's NSView and Layer its layer (macOS).
	View, Layer uintptr
	// Widget is the surface's GtkGLArea or GtkDrawingArea, and GLArea the
	// GtkGLArea while its render signal draws a frame with OpenGL, in the
	// context it made current (Linux).
	Widget, GLArea uintptr
	// Canvas is the number of the canvas's WebGL 2 context (package gl's
	// Context.ID), current while SurfaceFrame events are handled
	// (browsers).
	Canvas uintptr
}

// RectF is a rectangle in DIPs with fractional coordinates.
type RectF struct{ X, Y, W, H float64 }

// Contains reports whether the point is inside r.
func (r RectF) Contains(x, y float64) bool {
	return x >= r.X && y >= r.Y && x < r.X+r.W && y < r.Y+r.H
}

// SurfaceEventKind is the kind of a SurfaceEvent.
type SurfaceEventKind uint8

const (
	// SurfaceFrame asks the content to draw a frame now.
	SurfaceFrame SurfaceEventKind = iota
	// SurfaceResize reports a new Size; a frame follows.
	SurfaceResize
	// PointerMove reports the pointer at X, Y.
	PointerMove
	// PointerDown and PointerUp report Button pressed or released at X,
	// Y; Clicks counts quick successive presses (2 for a double click),
	// by the system's double-click settings. A release may leave it 0, for
	// its press's.
	PointerDown
	PointerUp
	// PointerCancel reports that the system ended the pointer's press
	// without a release, as Pointer Events' pointercancel: every button
	// held is let go of, and no PointerUp follows for them. The content
	// commits nothing a release would. Backends send it when they lose the
	// press to the system (a browser's pointercancel, a window losing the
	// mouse capture or a pointer grab while a button is down, a menu's
	// loop taking the release), and the content ignores one while nothing
	// is pressed.
	PointerCancel
	// PointerLeave reports the pointer left the surface.
	PointerLeave
	// PointerScroll scrolls by DX, DY DIPs at X, Y: positive DY moves the
	// view down the content, as dragging a scroll bar's thumb down does.
	PointerScroll
	// PointerZoom zooms by Scale at X, Y, as pinching a touchpad does: 2
	// shows the content twice as large as it was.
	PointerZoom
	// KeyPressed and KeyReleased report Key with Mods; Repeat marks
	// auto-repeat.
	KeyPressed
	KeyReleased
	// ModifiersChanged reports the modifier keys held, Mods, as one of
	// them goes down or up on its own.
	ModifiersChanged
	// RawKeyPressed and RawKeyReleased report a key by its position, Code,
	// that KeyPressed and KeyReleased leave out: a modifier, a lock key, or
	// one the layout types no Key for. PhysicalKey is set where it can be,
	// Key never is. The content reports whether it took the press or the
	// release, which then goes no further in the system, as Alt opening
	// the window's menu bar.
	RawKeyPressed
	RawKeyReleased
	// TextInput inserts Text, typed or committed by an input method.
	TextInput
	// TextComposition shows Text as the input method's composition, its
	// caret at rune Caret; an empty Text ends the composition.
	TextComposition
	// SurfaceFocus and SurfaceBlur report the surface gaining or losing
	// the keyboard.
	SurfaceFocus
	SurfaceBlur
	// SurfaceCommand performs the edit command Text: "copy", "cut",
	// "paste", "pasteAndMatchStyle", "selectAll", "undo", "redo" or
	// "delete" (Edit menu roles), or a Format menu role's: "bold",
	// "italic", "underline", "strikethrough", "alignLeft", "alignCenter"
	// or "alignRight".
	SurfaceCommand
	// SurfaceCommandEnabled asks whether SurfaceCommand with Text would
	// act now, for a menu to enable its item: WindowHandler.SurfaceEvent
	// reports it.
	SurfaceCommandEnabled
	// FileDragOver reports files of another app dragged over X, Y, and
	// FileDragLeave that they left. FileDrop drops Files at X, Y.
	// WindowHandler.SurfaceEvent returns whether the content takes them
	// there.
	FileDragOver
	FileDragLeave
	FileDrop
	// AccessibilityOn reports that assistive technology asked about the
	// surface: the content calls UpdateAccessibility at once and after
	// every frame from then on.
	AccessibilityOn
	// AccessAction performs Action on the element ID of the accessibility
	// tree, with Text the value of AccessSetValue.
	AccessAction
	// SurfaceShown reports that some of an OccludableSurface shows again
	// after none did, and SurfaceHidden that none of it shows any more.
	SurfaceShown
	SurfaceHidden
	// DataDragOver queries a destination; DataDrop commits a transfer.
	// DataDragLeave clears its hover state. Drag carries the query/result.
	DataDragOver
	DataDragLeave
	DataDrop
	// SurfaceRenew reports that the native objects a GPU renderer draws
	// into changed (Surface.Native), as a window of Windows composes its
	// frames once it has web views: the content makes its renderer again.
	SurfaceRenew
	// WebViewPress reports that Button went down at X, Y on a web view
	// the content shows there (Surface.PlaceWebViews), which takes the
	// press and what follows: the content notes it, as popovers close for
	// a press outside them, but handles none of it.
	WebViewPress
	// WebViewTabOut reports that Tab moved the keyboard out of WebView
	// past its page's last element, or Shift+Tab (Mods) past its first:
	// the surface has the keyboard again, and the content moves the focus
	// on from the web view's element.
	WebViewTabOut
	// WebViewKey reports Key with Mods, pressed in the page of WebView,
	// which has the keyboard, and which the page left unhandled: it did
	// not cancel it, type it into an editable element or compose with it,
	// and it is neither Tab nor a modifier key on its own. The content
	// takes it as if the web view's element had the focus, and the
	// keyboard stays with the page.
	WebViewKey
)

// EditCommands are the menu roles of edit commands, which native UI takes as
// a SurfaceCommand and answers SurfaceCommandEnabled for.
var EditCommands = map[string]bool{
	"undo": true, "redo": true, "cut": true, "copy": true, "paste": true, "pasteAndMatchStyle": true,
	"delete": true, "selectAll": true,
}

// SurfaceEvent is input on a Surface, or a change of it.
type SurfaceEvent struct {
	Kind SurfaceEventKind
	// X and Y locate the pointer in DIPs relative to the surface.
	X, Y float64
	// Button is 0 for the primary button, 1 the secondary, 2 the middle.
	Button int
	Clicks int
	// DX and DY are scroll distances in DIPs; Precise marks touchpads and
	// other devices that scroll by pixels rather than by lines. Momentum
	// marks scrolling that continues after the fingers leave a touchpad.
	DX, DY   float64
	Precise  bool
	Momentum bool
	// Scale is PointerZoom's.
	Scale float64
	// Pointer is the device of PointerMove, PointerDown and PointerUp, as
	// Pointer Events' pointerType, and Pressure how hard a pen or a finger
	// presses, from 0 to 1, where the device tells: 0 where it does not,
	// as a mouse, which the content takes for Pointer Events' default.
	Pointer  PointerKind
	Pressure float64
	Key      Key
	// PhysicalKey is the key by its position on an ANSI US keyboard,
	// whatever the active layout types; KeyUnknown where the backend does
	// not report it. Key and PhysicalKey agree on a US layout.
	PhysicalKey Key
	// Code is the key by its position, on any keyboard and with the sides
	// of the modifiers told apart; CodeUnknown where the backend does not
	// report it. KeyPressed, KeyReleased and the raw keys have it.
	Code   KeyCode
	Mods   Modifiers
	Repeat bool
	Text   string
	Caret  int
	// Replace makes TextInput replace, and TextComposition compose over,
	// the runes From to To of the last TextInputState.Text, instead of the
	// selection.
	Replace  bool
	From, To int
	// Files are the paths of FileDrop's files, or original native paths
	// for DataDrop's legacy file-listener fallback.
	Files []string
	Drag  *DataDragEvent
	// ID and Action are AccessAction's.
	ID     uint64
	Action AccessActionKind
	// WebView is WebViewTabOut's and WebViewKey's.
	WebView WebView
	// Paced marks a SurfaceFrame of frames following each other at the
	// display's refreshes (PacedSurface): the content may draw it later,
	// once the display can take it, which it tells by returning true from
	// SurfaceEvent; the refreshes go on meanwhile.
	Paced bool
}

// PointerKind is the kind of device of a pointer event.
type PointerKind uint8

const (
	// PointerMouse is a mouse, or a touchpad, which moves the pointer as
	// one does.
	PointerMouse PointerKind = iota
	// PointerPen is a pen on a tablet or a screen, its eraser too.
	PointerPen
	// PointerTouch is a finger on a touchscreen.
	PointerTouch
)

// Modifiers are the modifier keys held during an event.
type Modifiers uint8

const (
	ModShift Modifiers = 1 << iota
	ModCtrl
	ModAlt
	// ModSuper is Command on macOS and the Windows key elsewhere.
	ModSuper
)

// Key identifies a key. Letter and digit keys follow the keyboard layout:
// KeyA is the key that types an a.
type Key uint16

const (
	KeyUnknown Key = iota
	KeyEnter
	KeyEscape
	KeyBackspace
	KeyTab
	KeySpace
	KeyDelete
	KeyInsert
	KeyHome
	KeyEnd
	KeyPageUp
	KeyPageDown
	KeyLeft
	KeyRight
	KeyUp
	KeyDown
	KeyF1
	KeyF2
	KeyF3
	KeyF4
	KeyF5
	KeyF6
	KeyF7
	KeyF8
	KeyF9
	KeyF10
	KeyF11
	KeyF12
	KeyMinus
	KeyEqual
	KeyComma
	KeyPeriod
	KeySlash
	KeySemicolon
	KeyQuote
	KeyBracketLeft
	KeyBracketRight
	KeyBackslash
	KeyBackquote
	KeyContextMenu
	Key0
	Key1
	Key2
	Key3
	Key4
	Key5
	Key6
	Key7
	Key8
	Key9
	KeyA
	KeyB
	KeyC
	KeyD
	KeyE
	KeyF
	KeyG
	KeyH
	KeyI
	KeyJ
	KeyK
	KeyL
	KeyM
	KeyN
	KeyO
	KeyP
	KeyQ
	KeyR
	KeyS
	KeyT
	KeyU
	KeyV
	KeyW
	KeyX
	KeyY
	KeyZ
	// KeyBack and KeyForward go back and forward in a history: the side
	// buttons of a mouse, the keys of keyboards that have them, and on
	// macOS swipes between pages.
	KeyBack
	KeyForward
)

// KeyForRune returns the key typing r unshifted, KeyUnknown for others.
func KeyForRune(r rune) Key {
	switch {
	case r >= 'a' && r <= 'z':
		return KeyA + Key(r-'a')
	case r >= 'A' && r <= 'Z':
		return KeyA + Key(r-'A')
	case r >= '0' && r <= '9':
		return Key0 + Key(r-'0')
	}
	switch r {
	case '-':
		return KeyMinus
	case '=':
		return KeyEqual
	case ',':
		return KeyComma
	case '.':
		return KeyPeriod
	case '/':
		return KeySlash
	case ';':
		return KeySemicolon
	case '\'':
		return KeyQuote
	case '[':
		return KeyBracketLeft
	case ']':
		return KeyBracketRight
	case '\\':
		return KeyBackslash
	case '`':
		return KeyBackquote
	case ' ':
		return KeySpace
	}
	return KeyUnknown
}

// scancodeKeys maps the scancodes of the letter, digit and punctuation keys
// of an ANSI US keyboard to the key at that position. The main block's
// codes are the same in Linux's evdev and in XT set 1, which Windows
// reports.
var scancodeKeys = map[uint16]Key{
	2: Key1, 3: Key2, 4: Key3, 5: Key4, 6: Key5, 7: Key6, 8: Key7, 9: Key8, 10: Key9, 11: Key0,
	12: KeyMinus, 13: KeyEqual,
	16: KeyQ, 17: KeyW, 18: KeyE, 19: KeyR, 20: KeyT, 21: KeyY, 22: KeyU, 23: KeyI, 24: KeyO, 25: KeyP,
	26: KeyBracketLeft, 27: KeyBracketRight,
	30: KeyA, 31: KeyS, 32: KeyD, 33: KeyF, 34: KeyG, 35: KeyH, 36: KeyJ, 37: KeyK, 38: KeyL,
	39: KeySemicolon, 40: KeyQuote, 41: KeyBackquote, 43: KeyBackslash,
	44: KeyZ, 45: KeyX, 46: KeyC, 47: KeyV, 48: KeyB, 49: KeyN, 50: KeyM,
	51: KeyComma, 52: KeyPeriod, 53: KeySlash,
}

// KeyForScancode returns the key at a scancode's position on an ANSI US
// keyboard, whatever the layout types (SurfaceEvent.PhysicalKey), or
// KeyUnknown for a key it does not map. A scancode is Linux's evdev code
// (the X keycode less 8) or Windows's XT set 1 code without the extended
// flag.
func KeyForScancode(code uint16) Key { return scancodeKeys[code] }

// KeyByPosition reports whether a key press whose layout key is key may
// stand for its physical key: only when the layout types no US key, and a
// shortcut modifier is held, so that typing stays on the layout.
func KeyByPosition(key, physical Key, mods Modifiers) bool {
	return key == KeyUnknown && physical != KeyUnknown && mods&(ModCtrl|ModAlt|ModSuper) != 0
}

// Cursor is a pointer shape.
type Cursor uint8

const (
	CursorDefault Cursor = iota
	CursorPointer
	CursorText
	CursorMove
	CursorResizeEW
	CursorResizeNS
	CursorResizeNWSE
	CursorResizeNESW
	CursorNotAllowed
	CursorCrosshair
	CursorGrab
	CursorGrabbing
	CursorResizeN
	CursorResizeE
	CursorResizeS
	CursorResizeW
	CursorResizeColumn
	CursorResizeRow
	CursorVerticalText
	CursorCopy
	CursorAlias
	CursorContextMenu
	CursorNone
	CursorProgress
	CursorWait
	CursorHelp
	CursorCell
	CursorZoomIn
	CursorZoomOut
	CursorAllScroll
	CursorNoDrop
)
