package agent

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// CleanOutput turns raw terminal output into plain text: escape sequences go, a carriage return
// rewinds the line (progress bars keep their last state), backspace deletes, other controls vanish.
func CleanOutput(raw string) string {
	s := StripEscapes(raw)
	var out strings.Builder
	var line []rune
	crPending := false
	flush := func() { out.WriteString(string(line)); line = line[:0] }
	for _, r := range s {
		if crPending {
			crPending = false
			if r != '\n' {
				line = line[:0]
			}
		}
		switch {
		case r == '\n':
			flush()
			out.WriteByte('\n')
		case r == '\r':
			crPending = true
		case r == '\b' || r == 0x7f:
			if len(line) > 0 {
				line = line[:len(line)-1]
			}
		case r == '\t' || r >= 0x20 && r != 0x7f && r != 0xfffd:
			line = append(line, r)
		}
	}
	flush()
	return out.String()
}

var keyNames = func() map[string]string {
	m := map[string]string{
		"enter": "\r", "esc": "\x1b", "tab": "\t", "backspace": "\x7f", "delete": "\x1b[3~",
		"up": "\x1b[A", "down": "\x1b[B", "right": "\x1b[C", "left": "\x1b[D",
		"home": "\x1b[H", "end": "\x1b[F", "pageup": "\x1b[5~", "pagedown": "\x1b[6~",
		"f1": "\x1bOP", "f2": "\x1bOQ", "f3": "\x1bOR", "f4": "\x1bOS", "f5": "\x1b[15~", "f6": "\x1b[17~",
		"f7": "\x1b[18~", "f8": "\x1b[19~", "f9": "\x1b[20~", "f10": "\x1b[21~", "f11": "\x1b[23~", "f12": "\x1b[24~",
	}
	for c := 'a'; c <= 'z'; c++ {
		m["ctrl-"+string(c)] = string(rune(c - 'a' + 1))
	}
	return m
}()

// maxTyped is the most one session_send_text call may type.
const maxTyped = 4 << 10

// Keystrokes expands the escapes of text (\r \n \t \e \xHH \\) and appends the named keys.
func Keystrokes(text string, keys []string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(text); i++ {
		if text[i] != '\\' || i+1 >= len(text) {
			b.WriteByte(text[i])
			continue
		}
		i++
		switch text[i] {
		case 'r':
			b.WriteByte('\r')
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'e':
			b.WriteByte(0x1b)
		case '\\':
			b.WriteByte('\\')
		case 'x':
			var v int
			if i+2 < len(text) {
				if _, e := fmt.Sscanf(text[i+1:i+3], "%02x", &v); e == nil {
					b.WriteByte(byte(v))
					i += 2
					break
				}
			}
			b.WriteString(`\x`)
		default:
			b.WriteByte('\\')
			b.WriteByte(text[i])
		}
	}
	for _, k := range keys {
		seq, ok := keyNames[strings.ToLower(strings.TrimSpace(k))]
		if !ok {
			return "", fmt.Errorf("unknown key %q", k)
		}
		b.WriteString(seq)
	}
	if b.Len() > maxTyped {
		return "", fmt.Errorf("more than %d bytes", maxTyped)
	}
	if b.Len() == 0 {
		return "", fmt.Errorf("nothing to type")
	}
	return b.String(), nil
}

// typedLine follows what has been typed into a session since its last Enter, so that a command typed
// in several calls is judged as a whole. unknown means the shell holds text this tracker cannot see: the
// line was edited with history, completion, cursor or other readline keys.
type typedLine struct {
	buf     []rune
	unknown bool
	alt     bool // tracked on the alternate screen; a switch between screens starts a fresh line
}

var historyExpansion = regexp.MustCompile(`(?:^|[\s;&|(])!(?:!|\$|\^|\*|[A-Za-z0-9_-])`)

// scanTyping judges data as typed after state and returns the verdict of every line it completes
// together with the state after it.
//
// At a shell prompt every control character outside a small allowlist (Enter, Backspace, Ctrl-C/U/W,
// Ctrl-D on an empty line) marks the line unknown, so the next Enter needs a manual approval; Ctrl-O
// and Alt-Enter run the line like Enter does. On the alternate screen (alt: vim, tmux, htop) control
// keys and Esc belong to the program, not to a command line, so they leave the line alone, and the
// lines ended by Enter are still judged on a best-effort basis.
func scanTyping(state typedLine, data string, blacklist []string, alt bool) (Verdict, typedLine) {
	var v Verdict
	st := typedLine{buf: append([]rune{}, state.buf...), unknown: state.unknown, alt: alt}
	if state.alt != alt {
		st = typedLine{alt: alt} // a full-screen program started or ended: the old line was typed elsewhere
	}
	enter := func() {
		line := string(st.buf)
		if strings.TrimSpace(line) != "" || st.unknown {
			v.merge(Classify(line, blacklist))
			if st.unknown {
				v.risk("line built with history, completion or cursor keys")
			}
			if historyExpansion.MatchString(line) {
				v.risk("history expansion")
			}
		}
		st = typedLine{alt: alt}
	}
	for i := 0; i < len(data); {
		c := data[i]
		switch {
		case c == '\r' || c == '\n': // Enter, Ctrl-M, Ctrl-J
			enter()
			i++
		case c == 0x7f || c == 0x08:
			if n := len(st.buf); n > 0 {
				st.buf = st.buf[:n-1]
			}
			i++
		case c == 0x03: // Ctrl-C abandons the line
			st = typedLine{alt: alt}
			i++
		case c == 0x15 && !st.unknown: // Ctrl-U kills back to the start; with the cursor unknown it kills only part
			st = typedLine{alt: alt}
			i++
		case c == 0x17: // Ctrl-W
			s := strings.TrimRight(string(st.buf), " ")
			if j := strings.LastIndexByte(s, ' '); j >= 0 {
				st.buf = []rune(s[:j+1])
			} else {
				st.buf = nil
			}
			i++
		case c == 0x04 && !alt: // Ctrl-D ends the shell on an empty line and deletes a character otherwise
			st.unknown = st.unknown || len(st.buf) > 0
			i++
		case c == 0x0f && !alt: // Ctrl-O runs the line and brings back the next one from history
			enter()
			st.unknown = true
			i++
		case c == 0x1b:
			i++
			switch {
			case i < len(data) && (data[i] == '[' || data[i] == 'O'): // CSI / SS3: arrows, Home, F-keys, Delete
				i++
				for i < len(data) && (data[i] < '@' || data[i] > '~') {
					i++
				}
				i++
				st.unknown = st.unknown || !alt
			case alt: // Esc leaves a mode in vim; the key that follows is typed on its own
			case i < len(data) && (data[i] == '\r' || data[i] == '\n'): // Alt-Enter
				enter()
				i++
			default: // Alt+key, or a bare Esc that the next call may turn into Alt+key
				st.unknown = true
				if i < len(data) {
					i++
				}
			}
		case c < 0x20:
			st.unknown = st.unknown || !alt // completion, cursor movement, history, transpose, undo, quoted insert, …
			i++
		default:
			r, n := utf8.DecodeRuneInString(data[i:])
			st.buf = append(st.buf, r)
			i += n
		}
	}
	if len(st.buf) > 8<<10 {
		st = typedLine{unknown: true, alt: alt}
	}
	return v, st
}
