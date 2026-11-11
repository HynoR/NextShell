package platform

// KeyCode is a key by its position on the keyboard, whatever the layout
// types and whichever system reports it: a USB HID usage ID of the keyboard
// page (0x07), named after the code of UI Events' KeyboardEvent. Unlike Key
// it tells the sides of the modifiers and every key of a full keyboard.
type KeyCode uint16

// The keys by position.
const (
	CodeUnknown KeyCode = 0

	CodeKeyA KeyCode = iota + 3
	CodeKeyB
	CodeKeyC
	CodeKeyD
	CodeKeyE
	CodeKeyF
	CodeKeyG
	CodeKeyH
	CodeKeyI
	CodeKeyJ
	CodeKeyK
	CodeKeyL
	CodeKeyM
	CodeKeyN
	CodeKeyO
	CodeKeyP
	CodeKeyQ
	CodeKeyR
	CodeKeyS
	CodeKeyT
	CodeKeyU
	CodeKeyV
	CodeKeyW
	CodeKeyX
	CodeKeyY
	CodeKeyZ
	CodeDigit1
	CodeDigit2
	CodeDigit3
	CodeDigit4
	CodeDigit5
	CodeDigit6
	CodeDigit7
	CodeDigit8
	CodeDigit9
	CodeDigit0
	CodeEnter
	CodeEscape
	CodeBackspace
	CodeTab
	CodeSpace
	CodeMinus
	CodeEqual
	CodeBracketLeft
	CodeBracketRight
	CodeBackslash
)

const (
	CodeSemicolon KeyCode = iota + 0x33
	CodeQuote
	CodeBackquote
	CodeComma
	CodePeriod
	CodeSlash
	CodeCapsLock
	CodeF1
	CodeF2
	CodeF3
	CodeF4
	CodeF5
	CodeF6
	CodeF7
	CodeF8
	CodeF9
	CodeF10
	CodeF11
	CodeF12
	CodePrintScreen
	CodeScrollLock
	CodePause
	CodeInsert
	CodeHome
	CodePageUp
	CodeDelete
	CodeEnd
	CodePageDown
	CodeArrowRight
	CodeArrowLeft
	CodeArrowDown
	CodeArrowUp
	CodeNumLock
	CodeNumpadDivide
	CodeNumpadMultiply
	CodeNumpadSubtract
	CodeNumpadAdd
	CodeNumpadEnter
	CodeNumpad1
	CodeNumpad2
	CodeNumpad3
	CodeNumpad4
	CodeNumpad5
	CodeNumpad6
	CodeNumpad7
	CodeNumpad8
	CodeNumpad9
	CodeNumpad0
	CodeNumpadDecimal
	CodeIntlBackslash
	CodeContextMenu
	CodePower
	CodeNumpadEqual
	CodeF13
	CodeF14
	CodeF15
	CodeF16
	CodeF17
	CodeF18
	CodeF19
	CodeF20
	CodeF21
	CodeF22
	CodeF23
	CodeF24
)

const (
	CodeAudioVolumeMute KeyCode = iota + 0x7F
	CodeAudioVolumeUp
	CodeAudioVolumeDown
)

const (
	CodeNumpadComma KeyCode = 0x85
	CodeIntlRo      KeyCode = 0x87
	CodeKanaMode    KeyCode = 0x88
	CodeIntlYen     KeyCode = 0x89
	CodeConvert     KeyCode = 0x8A
	CodeNonConvert  KeyCode = 0x8B
	CodeLang1       KeyCode = 0x90
	CodeLang2       KeyCode = 0x91
	CodeLang3       KeyCode = 0x92
	CodeLang4       KeyCode = 0x93
)

const (
	CodeControlLeft KeyCode = iota + 0xE0
	CodeShiftLeft
	CodeAltLeft
	CodeSuperLeft
	CodeControlRight
	CodeShiftRight
	CodeAltRight
	CodeSuperRight
)

// xtCodes maps the scancodes shared by XT set 1, which Windows reports, and
// Linux's evdev: those of an AT keyboard's main block, its function keys and
// its keypad, up to F12.
var xtCodes = [...]KeyCode{
	0x01: CodeEscape, 0x02: CodeDigit1, 0x03: CodeDigit2, 0x04: CodeDigit3, 0x05: CodeDigit4,
	0x06: CodeDigit5, 0x07: CodeDigit6, 0x08: CodeDigit7, 0x09: CodeDigit8, 0x0A: CodeDigit9,
	0x0B: CodeDigit0, 0x0C: CodeMinus, 0x0D: CodeEqual, 0x0E: CodeBackspace, 0x0F: CodeTab,
	0x10: CodeKeyQ, 0x11: CodeKeyW, 0x12: CodeKeyE, 0x13: CodeKeyR, 0x14: CodeKeyT,
	0x15: CodeKeyY, 0x16: CodeKeyU, 0x17: CodeKeyI, 0x18: CodeKeyO, 0x19: CodeKeyP,
	0x1A: CodeBracketLeft, 0x1B: CodeBracketRight, 0x1C: CodeEnter, 0x1D: CodeControlLeft,
	0x1E: CodeKeyA, 0x1F: CodeKeyS, 0x20: CodeKeyD, 0x21: CodeKeyF, 0x22: CodeKeyG,
	0x23: CodeKeyH, 0x24: CodeKeyJ, 0x25: CodeKeyK, 0x26: CodeKeyL, 0x27: CodeSemicolon,
	0x28: CodeQuote, 0x29: CodeBackquote, 0x2A: CodeShiftLeft, 0x2B: CodeBackslash,
	0x2C: CodeKeyZ, 0x2D: CodeKeyX, 0x2E: CodeKeyC, 0x2F: CodeKeyV, 0x30: CodeKeyB,
	0x31: CodeKeyN, 0x32: CodeKeyM, 0x33: CodeComma, 0x34: CodePeriod, 0x35: CodeSlash,
	0x36: CodeShiftRight, 0x37: CodeNumpadMultiply, 0x38: CodeAltLeft, 0x39: CodeSpace,
	0x3A: CodeCapsLock, 0x3B: CodeF1, 0x3C: CodeF2, 0x3D: CodeF3, 0x3E: CodeF4, 0x3F: CodeF5,
	0x40: CodeF6, 0x41: CodeF7, 0x42: CodeF8, 0x43: CodeF9, 0x44: CodeF10, 0x45: CodeNumLock,
	0x46: CodeScrollLock, 0x47: CodeNumpad7, 0x48: CodeNumpad8, 0x49: CodeNumpad9,
	0x4A: CodeNumpadSubtract, 0x4B: CodeNumpad4, 0x4C: CodeNumpad5, 0x4D: CodeNumpad6,
	0x4E: CodeNumpadAdd, 0x4F: CodeNumpad1, 0x50: CodeNumpad2, 0x51: CodeNumpad3,
	0x52: CodeNumpad0, 0x53: CodeNumpadDecimal, 0x56: CodeIntlBackslash, 0x57: CodeF11, 0x58: CodeF12,
}

// KeyCodeForEvdev returns the key of a Linux evdev code (KEY_*; an X or
// GDK hardware keycode less 8), CodeUnknown for one it does not map.
func KeyCodeForEvdev(code uint16) KeyCode {
	if int(code) < len(xtCodes) {
		return xtCodes[code]
	}
	if code >= 183 && code <= 194 {
		return CodeF13 + KeyCode(code-183)
	}
	switch code {
	case 89:
		return CodeIntlRo
	case 90:
		return CodeLang3 // Katakana
	case 91:
		return CodeLang4 // Hiragana
	case 92:
		return CodeConvert
	case 93:
		return CodeKanaMode
	case 94:
		return CodeNonConvert
	case 95, 121:
		return CodeNumpadComma
	case 96:
		return CodeNumpadEnter
	case 97:
		return CodeControlRight
	case 98:
		return CodeNumpadDivide
	case 99:
		return CodePrintScreen
	case 100:
		return CodeAltRight
	case 102:
		return CodeHome
	case 103:
		return CodeArrowUp
	case 104:
		return CodePageUp
	case 105:
		return CodeArrowLeft
	case 106:
		return CodeArrowRight
	case 107:
		return CodeEnd
	case 108:
		return CodeArrowDown
	case 109:
		return CodePageDown
	case 110:
		return CodeInsert
	case 111:
		return CodeDelete
	case 113:
		return CodeAudioVolumeMute
	case 114:
		return CodeAudioVolumeDown
	case 115:
		return CodeAudioVolumeUp
	case 116:
		return CodePower
	case 117:
		return CodeNumpadEqual
	case 119:
		return CodePause
	case 122:
		return CodeLang1 // Hangul
	case 123:
		return CodeLang2 // Hanja
	case 124:
		return CodeIntlYen
	case 125:
		return CodeSuperLeft
	case 126:
		return CodeSuperRight
	case 127:
		return CodeContextMenu
	}
	return CodeUnknown
}

// KeyCodeForXT returns the key of an XT set 1 scancode as Windows reports it
// in a key message's lParam, extended being its bit 24, CodeUnknown for one
// it does not map. Windows's own quirks are in it: NumLock is the extended
// 0x45 and Pause the plain one, Alt+Print Screen and Ctrl+Pause have codes of
// their own, and CJK input methods set the extended bit of the right Shift.
func KeyCodeForXT(scan uint8, extended bool) KeyCode {
	if extended {
		switch scan {
		case 0x1C:
			return CodeNumpadEnter
		case 0x1D:
			return CodeControlRight
		case 0x35:
			return CodeNumpadDivide
		case 0x36:
			return CodeShiftRight
		case 0x37:
			return CodePrintScreen
		case 0x38:
			return CodeAltRight
		case 0x45:
			return CodeNumLock
		case 0x46:
			return CodePause
		case 0x47:
			return CodeHome
		case 0x48:
			return CodeArrowUp
		case 0x49:
			return CodePageUp
		case 0x4B:
			return CodeArrowLeft
		case 0x4D:
			return CodeArrowRight
		case 0x4F:
			return CodeEnd
		case 0x50:
			return CodeArrowDown
		case 0x51:
			return CodePageDown
		case 0x52:
			return CodeInsert
		case 0x53:
			return CodeDelete
		case 0x5B:
			return CodeSuperLeft
		case 0x5C:
			return CodeSuperRight
		case 0x5D:
			return CodeContextMenu
		case 0x5E:
			return CodePower
		}
		return CodeUnknown
	}
	switch scan {
	case 0x45:
		return CodePause
	case 0x54:
		return CodePrintScreen
	case 0x59:
		return CodeNumpadEqual
	case 0x70:
		return CodeKanaMode
	case 0x73:
		return CodeIntlRo
	case 0x76:
		return CodeF24
	case 0x79:
		return CodeConvert
	case 0x7B:
		return CodeNonConvert
	case 0x7D:
		return CodeIntlYen
	case 0x7E:
		return CodeNumpadComma
	case 0x71:
		return CodeLang2 // Hanja
	case 0x72:
		return CodeLang1 // Hangul
	case 0x77:
		return CodeLang4 // Hiragana
	case 0x78:
		return CodeLang3 // Katakana
	}
	if scan >= 0x64 && scan <= 0x6E {
		return CodeF13 + KeyCode(scan-0x64)
	}
	if int(scan) < len(xtCodes) {
		return xtCodes[scan]
	}
	return CodeUnknown
}

// macCodes maps macOS's virtual key codes (Carbon's kVK_*). A keyboard of
// ISO's layout swaps the codes of its section and grave keys, which the
// backend, knowing the keyboard, undoes.
var macCodes = [...]KeyCode{
	0x00: CodeKeyA, 0x01: CodeKeyS, 0x02: CodeKeyD, 0x03: CodeKeyF, 0x04: CodeKeyH, 0x05: CodeKeyG,
	0x06: CodeKeyZ, 0x07: CodeKeyX, 0x08: CodeKeyC, 0x09: CodeKeyV, 0x0A: CodeIntlBackslash, 0x0B: CodeKeyB,
	0x0C: CodeKeyQ, 0x0D: CodeKeyW, 0x0E: CodeKeyE, 0x0F: CodeKeyR, 0x10: CodeKeyY, 0x11: CodeKeyT,
	0x12: CodeDigit1, 0x13: CodeDigit2, 0x14: CodeDigit3, 0x15: CodeDigit4, 0x16: CodeDigit6, 0x17: CodeDigit5,
	0x18: CodeEqual, 0x19: CodeDigit9, 0x1A: CodeDigit7, 0x1B: CodeMinus, 0x1C: CodeDigit8, 0x1D: CodeDigit0,
	0x1E: CodeBracketRight, 0x1F: CodeKeyO, 0x20: CodeKeyU, 0x21: CodeBracketLeft, 0x22: CodeKeyI, 0x23: CodeKeyP,
	0x24: CodeEnter, 0x25: CodeKeyL, 0x26: CodeKeyJ, 0x27: CodeQuote, 0x28: CodeKeyK, 0x29: CodeSemicolon,
	0x2A: CodeBackslash, 0x2B: CodeComma, 0x2C: CodeSlash, 0x2D: CodeKeyN, 0x2E: CodeKeyM, 0x2F: CodePeriod,
	0x30: CodeTab, 0x31: CodeSpace, 0x32: CodeBackquote, 0x33: CodeBackspace, 0x35: CodeEscape,
	0x36: CodeSuperRight, 0x37: CodeSuperLeft, 0x38: CodeShiftLeft, 0x39: CodeCapsLock, 0x3A: CodeAltLeft,
	0x3B: CodeControlLeft, 0x3C: CodeShiftRight, 0x3D: CodeAltRight, 0x3E: CodeControlRight,
	0x40: CodeF17, 0x41: CodeNumpadDecimal, 0x43: CodeNumpadMultiply, 0x45: CodeNumpadAdd, 0x47: CodeNumLock,
	0x48: CodeAudioVolumeUp, 0x49: CodeAudioVolumeDown, 0x4A: CodeAudioVolumeMute, 0x4B: CodeNumpadDivide,
	0x4C: CodeNumpadEnter, 0x4E: CodeNumpadSubtract, 0x4F: CodeF18, 0x50: CodeF19, 0x51: CodeNumpadEqual,
	0x52: CodeNumpad0, 0x53: CodeNumpad1, 0x54: CodeNumpad2, 0x55: CodeNumpad3, 0x56: CodeNumpad4,
	0x57: CodeNumpad5, 0x58: CodeNumpad6, 0x59: CodeNumpad7, 0x5A: CodeF20, 0x5B: CodeNumpad8, 0x5C: CodeNumpad9,
	0x5D: CodeIntlYen, 0x5E: CodeIntlRo, 0x5F: CodeNumpadComma, 0x60: CodeF5, 0x61: CodeF6, 0x62: CodeF7,
	0x63: CodeF3, 0x64: CodeF8, 0x65: CodeF9, 0x66: CodeLang2, 0x67: CodeF11, 0x68: CodeLang1, 0x69: CodeF13,
	0x6A: CodeF16, 0x6B: CodeF14, 0x6D: CodeF10, 0x6E: CodeContextMenu, 0x6F: CodeF12, 0x71: CodeF15,
	0x72: CodeInsert, 0x73: CodeHome, 0x74: CodePageUp, 0x75: CodeDelete, 0x76: CodeF4, 0x77: CodeEnd,
	0x78: CodeF2, 0x79: CodePageDown, 0x7A: CodeF1, 0x7B: CodeArrowLeft, 0x7C: CodeArrowRight,
	0x7D: CodeArrowDown, 0x7E: CodeArrowUp,
}

// KeyCodeForMac returns the key of a macOS virtual key code, CodeUnknown for
// one it does not map, as Fn.
func KeyCodeForMac(code uint16) KeyCode {
	if int(code) < len(macCodes) {
		return macCodes[code]
	}
	return CodeUnknown
}

// MacFlagsChanged returns the key of a flagsChanged: event, of virtual key
// code keyCode and NSEvent modifierFlags flags, and whether it pressed that
// key rather than let go of it; CodeUnknown for one of no modifier (AppKit
// sends some with other codes) and for Fn, which has no code. The flags'
// device-dependent bits tell the sides; where the left and right bits are
// both clear while the side-independent bit is set, as in events another
// program made, it counts as pressed. Caps Lock counts as pressed: macOS
// tells of it once for each change of the lock, the backend gives it a
// release at once.
func MacFlagsChanged(keyCode uint16, flags uint) (code KeyCode, down bool) {
	const (
		shift   = 1 << 17
		control = 1 << 18
		option  = 1 << 19
		command = 1 << 20
	)
	var own, other, class uint
	code = KeyCodeForMac(keyCode)
	switch code {
	case CodeCapsLock:
		return code, true
	case CodeControlLeft:
		own, other, class = 0x0001, 0x2000, control
	case CodeControlRight:
		own, other, class = 0x2000, 0x0001, control
	case CodeShiftLeft:
		own, other, class = 0x0002, 0x0004, shift
	case CodeShiftRight:
		own, other, class = 0x0004, 0x0002, shift
	case CodeSuperLeft:
		own, other, class = 0x0008, 0x0010, command
	case CodeSuperRight:
		own, other, class = 0x0010, 0x0008, command
	case CodeAltLeft:
		own, other, class = 0x0020, 0x0040, option
	case CodeAltRight:
		own, other, class = 0x0040, 0x0020, option
	default:
		return CodeUnknown, false
	}
	return code, flags&own != 0 || flags&other == 0 && flags&class != 0
}

// domCodes maps the named codes of UI Events' KeyboardEvent that are not a
// letter or a digit.
var domCodes = map[string]KeyCode{
	"Enter": CodeEnter, "Escape": CodeEscape, "Backspace": CodeBackspace, "Tab": CodeTab, "Space": CodeSpace,
	"Minus": CodeMinus, "Equal": CodeEqual, "BracketLeft": CodeBracketLeft, "BracketRight": CodeBracketRight,
	"Backslash": CodeBackslash, "Semicolon": CodeSemicolon, "Quote": CodeQuote, "Backquote": CodeBackquote,
	"Comma": CodeComma, "Period": CodePeriod, "Slash": CodeSlash, "CapsLock": CodeCapsLock,
	"PrintScreen": CodePrintScreen, "ScrollLock": CodeScrollLock, "Pause": CodePause, "Insert": CodeInsert,
	"Home": CodeHome, "PageUp": CodePageUp, "Delete": CodeDelete, "End": CodeEnd, "PageDown": CodePageDown,
	"ArrowRight": CodeArrowRight, "ArrowLeft": CodeArrowLeft, "ArrowDown": CodeArrowDown, "ArrowUp": CodeArrowUp,
	"NumLock": CodeNumLock, "NumpadDivide": CodeNumpadDivide, "NumpadMultiply": CodeNumpadMultiply,
	"NumpadSubtract": CodeNumpadSubtract, "NumpadAdd": CodeNumpadAdd, "NumpadEnter": CodeNumpadEnter,
	"NumpadDecimal": CodeNumpadDecimal, "NumpadEqual": CodeNumpadEqual, "NumpadComma": CodeNumpadComma,
	"IntlBackslash": CodeIntlBackslash, "IntlRo": CodeIntlRo, "IntlYen": CodeIntlYen, "KanaMode": CodeKanaMode,
	"Convert": CodeConvert, "NonConvert": CodeNonConvert, "Lang1": CodeLang1, "Lang2": CodeLang2,
	"Lang3": CodeLang3, "Lang4": CodeLang4, "ContextMenu": CodeContextMenu, "Power": CodePower,
	"AudioVolumeMute": CodeAudioVolumeMute, "AudioVolumeUp": CodeAudioVolumeUp, "AudioVolumeDown": CodeAudioVolumeDown,
	"ControlLeft": CodeControlLeft, "ShiftLeft": CodeShiftLeft, "AltLeft": CodeAltLeft, "MetaLeft": CodeSuperLeft,
	"ControlRight": CodeControlRight, "ShiftRight": CodeShiftRight, "AltRight": CodeAltRight, "MetaRight": CodeSuperRight,
	// Older names of the same keys, which some browsers still give.
	"OSLeft": CodeSuperLeft, "OSRight": CodeSuperRight,
}

// KeyCodeForDOM returns the key of a KeyboardEvent's code, CodeUnknown for
// one it does not map.
func KeyCodeForDOM(code string) KeyCode {
	switch {
	case len(code) == 4 && code[:3] == "Key" && code[3] >= 'A' && code[3] <= 'Z':
		return CodeKeyA + KeyCode(code[3]-'A')
	case len(code) == 6 && code[:5] == "Digit" && code[5] >= '1' && code[5] <= '9':
		return CodeDigit1 + KeyCode(code[5]-'1')
	case code == "Digit0":
		return CodeDigit0
	case len(code) == 7 && code[:6] == "Numpad" && code[6] >= '1' && code[6] <= '9':
		return CodeNumpad1 + KeyCode(code[6]-'1')
	case code == "Numpad0":
		return CodeNumpad0
	case len(code) >= 2 && code[0] == 'F':
		if n := functionNumber(code[1:]); n >= 1 && n <= 12 {
			return CodeF1 + KeyCode(n-1)
		} else if n >= 13 && n <= 24 {
			return CodeF13 + KeyCode(n-13)
		}
	}
	return domCodes[code]
}

// functionNumber reads the one or two digits of F1 to F24, 0 for anything
// else.
func functionNumber(s string) int {
	if len(s) == 0 || len(s) > 2 {
		return 0
	}
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0
		}
		n = n*10 + int(s[i]-'0')
	}
	return n
}
