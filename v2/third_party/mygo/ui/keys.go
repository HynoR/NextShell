package ui

import (
	"runtime"

	"github.com/egoist/mygo/internal/platform"
)

// Key identifies a key. Letter and digit keys follow the keyboard layout:
// KeyA is the key that types an a.
type Key uint16

// Keys.
const (
	KeyUnknown      = Key(platform.KeyUnknown)
	KeyEnter        = Key(platform.KeyEnter)
	KeyEscape       = Key(platform.KeyEscape)
	KeyBackspace    = Key(platform.KeyBackspace)
	KeyTab          = Key(platform.KeyTab)
	KeySpace        = Key(platform.KeySpace)
	KeyDelete       = Key(platform.KeyDelete)
	KeyInsert       = Key(platform.KeyInsert)
	KeyHome         = Key(platform.KeyHome)
	KeyEnd          = Key(platform.KeyEnd)
	KeyPageUp       = Key(platform.KeyPageUp)
	KeyPageDown     = Key(platform.KeyPageDown)
	KeyLeft         = Key(platform.KeyLeft)
	KeyRight        = Key(platform.KeyRight)
	KeyUp           = Key(platform.KeyUp)
	KeyDown         = Key(platform.KeyDown)
	KeyF1           = Key(platform.KeyF1)
	KeyF2           = Key(platform.KeyF2)
	KeyF3           = Key(platform.KeyF3)
	KeyF4           = Key(platform.KeyF4)
	KeyF5           = Key(platform.KeyF5)
	KeyF6           = Key(platform.KeyF6)
	KeyF7           = Key(platform.KeyF7)
	KeyF8           = Key(platform.KeyF8)
	KeyF9           = Key(platform.KeyF9)
	KeyF10          = Key(platform.KeyF10)
	KeyF11          = Key(platform.KeyF11)
	KeyF12          = Key(platform.KeyF12)
	KeyMinus        = Key(platform.KeyMinus)
	KeyEqual        = Key(platform.KeyEqual)
	KeyComma        = Key(platform.KeyComma)
	KeyPeriod       = Key(platform.KeyPeriod)
	KeySlash        = Key(platform.KeySlash)
	KeySemicolon    = Key(platform.KeySemicolon)
	KeyQuote        = Key(platform.KeyQuote)
	KeyBracketLeft  = Key(platform.KeyBracketLeft)
	KeyBracketRight = Key(platform.KeyBracketRight)
	KeyBackslash    = Key(platform.KeyBackslash)
	KeyBackquote    = Key(platform.KeyBackquote)
	KeyContextMenu  = Key(platform.KeyContextMenu)
	Key0            = Key(platform.Key0)
	Key1            = Key(platform.Key1)
	Key2            = Key(platform.Key2)
	Key3            = Key(platform.Key3)
	Key4            = Key(platform.Key4)
	Key5            = Key(platform.Key5)
	Key6            = Key(platform.Key6)
	Key7            = Key(platform.Key7)
	Key8            = Key(platform.Key8)
	Key9            = Key(platform.Key9)
	KeyA            = Key(platform.KeyA)
	KeyB            = Key(platform.KeyB)
	KeyC            = Key(platform.KeyC)
	KeyD            = Key(platform.KeyD)
	KeyE            = Key(platform.KeyE)
	KeyF            = Key(platform.KeyF)
	KeyG            = Key(platform.KeyG)
	KeyH            = Key(platform.KeyH)
	KeyI            = Key(platform.KeyI)
	KeyJ            = Key(platform.KeyJ)
	KeyK            = Key(platform.KeyK)
	KeyL            = Key(platform.KeyL)
	KeyM            = Key(platform.KeyM)
	KeyN            = Key(platform.KeyN)
	KeyO            = Key(platform.KeyO)
	KeyP            = Key(platform.KeyP)
	KeyQ            = Key(platform.KeyQ)
	KeyR            = Key(platform.KeyR)
	KeyS            = Key(platform.KeyS)
	KeyT            = Key(platform.KeyT)
	KeyU            = Key(platform.KeyU)
	KeyV            = Key(platform.KeyV)
	KeyW            = Key(platform.KeyW)
	KeyX            = Key(platform.KeyX)
	KeyY            = Key(platform.KeyY)
	KeyZ            = Key(platform.KeyZ)
	// KeyBack and KeyForward go back and forward in a history: the side
	// buttons of a mouse, the keys of keyboards that have them, and on
	// macOS swipes between pages. A Router takes them.
	KeyBack    = Key(platform.KeyBack)
	KeyForward = Key(platform.KeyForward)
)

// KeyCode is a key by its position on the keyboard, whatever the layout
// types and whichever system reports it (InputEvent.Code), with the sides
// of the modifiers told apart and every key of a full keyboard, those that
// Key has no name for too. Its values are USB HID usage IDs of the keyboard
// page (0x07), and its names the codes of UI Events' KeyboardEvent, Meta
// being Super here.
type KeyCode uint16

// Keys by position.
const (
	CodeUnknown         = KeyCode(platform.CodeUnknown)
	CodeKeyA            = KeyCode(platform.CodeKeyA)
	CodeKeyB            = KeyCode(platform.CodeKeyB)
	CodeKeyC            = KeyCode(platform.CodeKeyC)
	CodeKeyD            = KeyCode(platform.CodeKeyD)
	CodeKeyE            = KeyCode(platform.CodeKeyE)
	CodeKeyF            = KeyCode(platform.CodeKeyF)
	CodeKeyG            = KeyCode(platform.CodeKeyG)
	CodeKeyH            = KeyCode(platform.CodeKeyH)
	CodeKeyI            = KeyCode(platform.CodeKeyI)
	CodeKeyJ            = KeyCode(platform.CodeKeyJ)
	CodeKeyK            = KeyCode(platform.CodeKeyK)
	CodeKeyL            = KeyCode(platform.CodeKeyL)
	CodeKeyM            = KeyCode(platform.CodeKeyM)
	CodeKeyN            = KeyCode(platform.CodeKeyN)
	CodeKeyO            = KeyCode(platform.CodeKeyO)
	CodeKeyP            = KeyCode(platform.CodeKeyP)
	CodeKeyQ            = KeyCode(platform.CodeKeyQ)
	CodeKeyR            = KeyCode(platform.CodeKeyR)
	CodeKeyS            = KeyCode(platform.CodeKeyS)
	CodeKeyT            = KeyCode(platform.CodeKeyT)
	CodeKeyU            = KeyCode(platform.CodeKeyU)
	CodeKeyV            = KeyCode(platform.CodeKeyV)
	CodeKeyW            = KeyCode(platform.CodeKeyW)
	CodeKeyX            = KeyCode(platform.CodeKeyX)
	CodeKeyY            = KeyCode(platform.CodeKeyY)
	CodeKeyZ            = KeyCode(platform.CodeKeyZ)
	CodeDigit1          = KeyCode(platform.CodeDigit1)
	CodeDigit2          = KeyCode(platform.CodeDigit2)
	CodeDigit3          = KeyCode(platform.CodeDigit3)
	CodeDigit4          = KeyCode(platform.CodeDigit4)
	CodeDigit5          = KeyCode(platform.CodeDigit5)
	CodeDigit6          = KeyCode(platform.CodeDigit6)
	CodeDigit7          = KeyCode(platform.CodeDigit7)
	CodeDigit8          = KeyCode(platform.CodeDigit8)
	CodeDigit9          = KeyCode(platform.CodeDigit9)
	CodeDigit0          = KeyCode(platform.CodeDigit0)
	CodeEnter           = KeyCode(platform.CodeEnter)
	CodeEscape          = KeyCode(platform.CodeEscape)
	CodeBackspace       = KeyCode(platform.CodeBackspace)
	CodeTab             = KeyCode(platform.CodeTab)
	CodeSpace           = KeyCode(platform.CodeSpace)
	CodeMinus           = KeyCode(platform.CodeMinus)
	CodeEqual           = KeyCode(platform.CodeEqual)
	CodeBracketLeft     = KeyCode(platform.CodeBracketLeft)
	CodeBracketRight    = KeyCode(platform.CodeBracketRight)
	CodeBackslash       = KeyCode(platform.CodeBackslash)
	CodeSemicolon       = KeyCode(platform.CodeSemicolon)
	CodeQuote           = KeyCode(platform.CodeQuote)
	CodeBackquote       = KeyCode(platform.CodeBackquote)
	CodeComma           = KeyCode(platform.CodeComma)
	CodePeriod          = KeyCode(platform.CodePeriod)
	CodeSlash           = KeyCode(platform.CodeSlash)
	CodeCapsLock        = KeyCode(platform.CodeCapsLock)
	CodeF1              = KeyCode(platform.CodeF1)
	CodeF2              = KeyCode(platform.CodeF2)
	CodeF3              = KeyCode(platform.CodeF3)
	CodeF4              = KeyCode(platform.CodeF4)
	CodeF5              = KeyCode(platform.CodeF5)
	CodeF6              = KeyCode(platform.CodeF6)
	CodeF7              = KeyCode(platform.CodeF7)
	CodeF8              = KeyCode(platform.CodeF8)
	CodeF9              = KeyCode(platform.CodeF9)
	CodeF10             = KeyCode(platform.CodeF10)
	CodeF11             = KeyCode(platform.CodeF11)
	CodeF12             = KeyCode(platform.CodeF12)
	CodePrintScreen     = KeyCode(platform.CodePrintScreen)
	CodeScrollLock      = KeyCode(platform.CodeScrollLock)
	CodePause           = KeyCode(platform.CodePause)
	CodeInsert          = KeyCode(platform.CodeInsert)
	CodeHome            = KeyCode(platform.CodeHome)
	CodePageUp          = KeyCode(platform.CodePageUp)
	CodeDelete          = KeyCode(platform.CodeDelete)
	CodeEnd             = KeyCode(platform.CodeEnd)
	CodePageDown        = KeyCode(platform.CodePageDown)
	CodeArrowRight      = KeyCode(platform.CodeArrowRight)
	CodeArrowLeft       = KeyCode(platform.CodeArrowLeft)
	CodeArrowDown       = KeyCode(platform.CodeArrowDown)
	CodeArrowUp         = KeyCode(platform.CodeArrowUp)
	CodeNumLock         = KeyCode(platform.CodeNumLock)
	CodeNumpadDivide    = KeyCode(platform.CodeNumpadDivide)
	CodeNumpadMultiply  = KeyCode(platform.CodeNumpadMultiply)
	CodeNumpadSubtract  = KeyCode(platform.CodeNumpadSubtract)
	CodeNumpadAdd       = KeyCode(platform.CodeNumpadAdd)
	CodeNumpadEnter     = KeyCode(platform.CodeNumpadEnter)
	CodeNumpad1         = KeyCode(platform.CodeNumpad1)
	CodeNumpad2         = KeyCode(platform.CodeNumpad2)
	CodeNumpad3         = KeyCode(platform.CodeNumpad3)
	CodeNumpad4         = KeyCode(platform.CodeNumpad4)
	CodeNumpad5         = KeyCode(platform.CodeNumpad5)
	CodeNumpad6         = KeyCode(platform.CodeNumpad6)
	CodeNumpad7         = KeyCode(platform.CodeNumpad7)
	CodeNumpad8         = KeyCode(platform.CodeNumpad8)
	CodeNumpad9         = KeyCode(platform.CodeNumpad9)
	CodeNumpad0         = KeyCode(platform.CodeNumpad0)
	CodeNumpadDecimal   = KeyCode(platform.CodeNumpadDecimal)
	CodeIntlBackslash   = KeyCode(platform.CodeIntlBackslash)
	CodeContextMenu     = KeyCode(platform.CodeContextMenu)
	CodePower           = KeyCode(platform.CodePower)
	CodeNumpadEqual     = KeyCode(platform.CodeNumpadEqual)
	CodeF13             = KeyCode(platform.CodeF13)
	CodeF14             = KeyCode(platform.CodeF14)
	CodeF15             = KeyCode(platform.CodeF15)
	CodeF16             = KeyCode(platform.CodeF16)
	CodeF17             = KeyCode(platform.CodeF17)
	CodeF18             = KeyCode(platform.CodeF18)
	CodeF19             = KeyCode(platform.CodeF19)
	CodeF20             = KeyCode(platform.CodeF20)
	CodeF21             = KeyCode(platform.CodeF21)
	CodeF22             = KeyCode(platform.CodeF22)
	CodeF23             = KeyCode(platform.CodeF23)
	CodeF24             = KeyCode(platform.CodeF24)
	CodeAudioVolumeMute = KeyCode(platform.CodeAudioVolumeMute)
	CodeAudioVolumeUp   = KeyCode(platform.CodeAudioVolumeUp)
	CodeAudioVolumeDown = KeyCode(platform.CodeAudioVolumeDown)
	CodeNumpadComma     = KeyCode(platform.CodeNumpadComma)
	CodeIntlRo          = KeyCode(platform.CodeIntlRo)
	CodeKanaMode        = KeyCode(platform.CodeKanaMode)
	CodeIntlYen         = KeyCode(platform.CodeIntlYen)
	CodeConvert         = KeyCode(platform.CodeConvert)
	CodeNonConvert      = KeyCode(platform.CodeNonConvert)
	CodeLang1           = KeyCode(platform.CodeLang1)
	CodeLang2           = KeyCode(platform.CodeLang2)
	CodeLang3           = KeyCode(platform.CodeLang3)
	CodeLang4           = KeyCode(platform.CodeLang4)
	CodeControlLeft     = KeyCode(platform.CodeControlLeft)
	CodeShiftLeft       = KeyCode(platform.CodeShiftLeft)
	CodeAltLeft         = KeyCode(platform.CodeAltLeft)
	CodeSuperLeft       = KeyCode(platform.CodeSuperLeft)
	CodeControlRight    = KeyCode(platform.CodeControlRight)
	CodeShiftRight      = KeyCode(platform.CodeShiftRight)
	CodeAltRight        = KeyCode(platform.CodeAltRight)
	CodeSuperRight      = KeyCode(platform.CodeSuperRight)
)

// Modifiers are modifier keys.
type Modifiers uint8

// Modifier keys.
const (
	Shift = Modifiers(platform.ModShift)
	Ctrl  = Modifiers(platform.ModCtrl)
	Alt   = Modifiers(platform.ModAlt)
	// Super is Command on macOS and the Windows key elsewhere.
	Super = Modifiers(platform.ModSuper)
)

// Cmd is the modifier of the platform's shortcuts: Command on macOS,
// Control elsewhere, as in Cmd+C.
var Cmd = func() Modifiers {
	if runtime.GOOS == "darwin" {
		return Super
	}
	return Ctrl
}()

// PointerType is the kind of device moving the pointer, as Pointer
// Events' pointerType.
type PointerType uint8

// Pointer devices.
const (
	// PointerMouse is a mouse, or a touchpad, which moves the pointer as
	// one does.
	PointerMouse = PointerType(platform.PointerMouse)
	// PointerPen is a pen on a tablet or a screen, its eraser too.
	PointerPen = PointerType(platform.PointerPen)
	// PointerTouch is a finger on a touchscreen.
	PointerTouch = PointerType(platform.PointerTouch)
)

// Cursor is a pointer shape.
type Cursor uint8

// Pointer shapes.
const (
	CursorDefault    = Cursor(platform.CursorDefault)
	CursorPointer    = Cursor(platform.CursorPointer)
	CursorText       = Cursor(platform.CursorText)
	CursorMove       = Cursor(platform.CursorMove)
	CursorResizeEW   = Cursor(platform.CursorResizeEW)
	CursorResizeNS   = Cursor(platform.CursorResizeNS)
	CursorResizeNWSE = Cursor(platform.CursorResizeNWSE)
	CursorResizeNESW = Cursor(platform.CursorResizeNESW)
	CursorNotAllowed = Cursor(platform.CursorNotAllowed)
	CursorCrosshair  = Cursor(platform.CursorCrosshair)
	CursorGrab       = Cursor(platform.CursorGrab)
	CursorGrabbing   = Cursor(platform.CursorGrabbing)
	// CursorResizeN, E, S and W resize toward one side only, as at the
	// limit of a resize.
	CursorResizeN = Cursor(platform.CursorResizeN)
	CursorResizeE = Cursor(platform.CursorResizeE)
	CursorResizeS = Cursor(platform.CursorResizeS)
	CursorResizeW = Cursor(platform.CursorResizeW)
	// CursorResizeColumn and CursorResizeRow move the line between columns
	// or rows, as of a table or a split.
	CursorResizeColumn = Cursor(platform.CursorResizeColumn)
	CursorResizeRow    = Cursor(platform.CursorResizeRow)
	CursorVerticalText = Cursor(platform.CursorVerticalText)
	// CursorCopy and CursorAlias show that a drop copies or links what is
	// dragged.
	CursorCopy        = Cursor(platform.CursorCopy)
	CursorAlias       = Cursor(platform.CursorAlias)
	CursorContextMenu = Cursor(platform.CursorContextMenu)
	// CursorNone hides the pointer over the element, as over a video.
	CursorNone = Cursor(platform.CursorNone)
	// CursorProgress shows that the app is busy while it can still be
	// used, as over an element marked busy (aria-busy), and CursorWait
	// that it cannot be.
	CursorProgress = Cursor(platform.CursorProgress)
	CursorWait     = Cursor(platform.CursorWait)
	CursorHelp     = Cursor(platform.CursorHelp)
	// CursorCell selects cells, as of a spreadsheet.
	CursorCell = Cursor(platform.CursorCell)
	// CursorZoomIn and CursorZoomOut show that a click zooms, as over an
	// image.
	CursorZoomIn  = Cursor(platform.CursorZoomIn)
	CursorZoomOut = Cursor(platform.CursorZoomOut)
	// CursorAllScroll scrolls in every direction, as a middle-button
	// autoscroll does.
	CursorAllScroll = Cursor(platform.CursorAllScroll)
	// CursorNoDrop shows that what is dragged cannot be dropped here.
	CursorNoDrop = Cursor(platform.CursorNoDrop)
)
