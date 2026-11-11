package terminal

import (
	"math"
	"strings"

	"github.com/egoist/mygo/plugins/terminal/internal/vt"
)

// promptRow is a row of the shell's prompt and its input, as clickToMove
// counts the characters the shell's cursor moves over: the column of each
// of its runes, how many columns its text takes (blanks after it are not
// the input's), and whether it soft-wraps into the next row.
type promptRow struct {
	cols []int
	end  int
	wrap bool
}

// newPromptRow makes a promptRow of a row's text and columns, as
// vt.Terminal.RowText returns them.
func newPromptRow(text []rune, cols []int, wrap bool) promptRow {
	r := promptRow{cols: cols, wrap: wrap}
	for i, c := range text {
		if c != ' ' {
			r.end = cols[i] + 1
		}
	}
	return r
}

// clickToMove moves the shell's cursor to the cell clicked on the input of
// its prompt, as Ghostty and kitty do on Alt+click, by sending the arrow
// keys that the shell moves its cursor by, and reports whether the click
// was there. The shell marks its prompt with OSC 133 (shell integration):
// the input is on the cursor's row of the prompt and the rows the prompt
// goes on over, above and below it. It does nothing on the alternate
// screen, nor while the view shows the scrollback. t.mu is held.
func (v *view) clickToMove(col, row int) bool {
	term := v.t.term
	cx, cy := term.Cursor()
	if term.AltScreen() || !term.AtBottom() {
		return false
	}
	if p, _ := term.Row(cy); p == vt.NoPrompt {
		return false
	}
	_, n := term.Size()
	top, bottom := cy, cy
	for top > 0 {
		if p, _ := term.Row(top); p != vt.PromptContinuation {
			break
		}
		top--
	}
	for bottom+1 < n {
		if p, _ := term.Row(bottom + 1); p != vt.PromptContinuation {
			break
		}
		bottom++
	}
	if row < top || row > bottom {
		return false
	}
	rows := make([]promptRow, 0, bottom-top+1)
	for y := top; y <= bottom; y++ {
		text, cols := term.RowText(y)
		_, wrap := term.Row(y)
		rows = append(rows, newPromptRow(text, cols, wrap))
	}
	v.t.Send(promptArrows(rows, cx, cy-top, col, row-top, term.Mode(vt.ModeCursorKeys)))
	return true
}

// promptArrows returns the arrow keys that move the shell's cursor from
// column cx of rows[cy] to column col of rows[row]: Left or Right as many
// times as there are characters between, counted over the rows between as
// the shell holds them, a row's text and the line break after it unless it
// soft-wraps (its blanks then too). A click past the end of a row's text
// moves to its end and no further, as a Right arrow at the end of the input
// would accept a shell's suggestion (zsh-autosuggestions, fish). The arrows
// are the application's (ESC O) when the shell set DECCKM, the normal ones
// (CSI) otherwise.
func promptArrows(rows []promptRow, cx, cy, col, row int, application bool) []byte {
	// end returns how many columns of row y the shell's text takes: all
	// when it wraps, and up to the cursor on the cursor's row, where
	// blanks typed before it are text.
	end := func(y int) int {
		switch r := rows[y]; {
		case r.wrap:
			return math.MaxInt
		case y == cy:
			return max(r.end, cx)
		default:
			return r.end
		}
	}
	// offset counts the characters before column x of row y.
	offset := func(x, y int) int {
		n := 0
		for i := range y {
			for _, c := range rows[i].cols {
				if c < end(i) {
					n++
				}
			}
			if !rows[i].wrap {
				n++ // the line break
			}
		}
		for _, c := range rows[y].cols {
			if c < min(x, end(y)) {
				n++
			}
		}
		return n
	}
	steps := offset(col, row) - offset(cx, cy)
	key := "C"
	if steps < 0 {
		key, steps = "D", -steps
	}
	intro := "\x1b["
	if application {
		intro = "\x1bO"
	}
	return []byte(strings.Repeat(intro+key, steps))
}
