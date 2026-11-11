package terminal

import (
	"os"
	"path/filepath"
	"strings"
)

// Ghostty's C API preserves prompts by default. zsh instead redraws the
// whole prompt on SIGWINCH; the final C leaves the initial screen unmarked.
const promptRedraw = "\x1b]133;A;redraw=1\a\x1b]133;C\a"

func zshEnvironment(env []string) ([]string, string, error) {
	dir, err := os.MkdirTemp("", "mygo-zsh-")
	if err != nil {
		return nil, "", err
	}
	if err := os.WriteFile(filepath.Join(dir, ".zshenv"), []byte(zshIntegration), 0o600); err != nil {
		os.RemoveAll(dir)
		return nil, "", err
	}
	clean := make([]string, 0, len(env)+1)
	var original string
	var hadOriginal bool
	for _, entry := range env {
		if value, ok := strings.CutPrefix(entry, "ZDOTDIR="); ok {
			original, hadOriginal = value, true
		} else if !strings.HasPrefix(entry, "MYGO_TERMINAL_ZDOTDIR=") {
			clean = append(clean, entry)
		}
	}
	if hadOriginal {
		clean = append(clean, "MYGO_TERMINAL_ZDOTDIR="+original)
	}
	return append(clean, "ZDOTDIR="+dir), dir, nil
}

// Restore ZDOTDIR before sourcing any user configuration. Install hooks at
// the first precmd, after startup files and prompt themes have loaded. P10k
// emits its own markers when asked, including asynchronous prompt updates.
const zshIntegration = `if (( ${+MYGO_TERMINAL_ZDOTDIR} )); then
  export ZDOTDIR=$MYGO_TERMINAL_ZDOTDIR
  unset MYGO_TERMINAL_ZDOTDIR
else
  unset ZDOTDIR
fi
[[ ! -r ${ZDOTDIR-$HOME}/.zshenv ]] || source -- "${ZDOTDIR-$HOME}/.zshenv"
[[ -o interactive ]] || return

typeset -gi __p9k_force_term_shell_integration=1

_mygo_mark_prompt() {
  local code=$?
  emulate -L zsh
  if ! zle; then
    printf '\e]133;D;%d\a' $code
  fi
  [[ -o promptpercent ]] || return
  local start=$'%{\e]133;A;cl=line;redraw=1\a%}' input=$'%{\e]133;B\a%}'
  local continuation=$'%{\e]133;P;k=s\a%}'
  if [[ $PS1 != *$'\e]133;'* ]]; then
    PS1=$start${PS1//$'\n'/$'\n'$continuation}$input
  fi
  [[ $PS2 == *$'\e]133;'* ]] || PS2=$continuation$PS2$input
}
_mygo_command_start() {
  printf '\e]133;C\a'
}
_mygo_install_marks() {
  emulate -L zsh
  autoload -Uz add-zsh-hook
  add-zsh-hook -d precmd _mygo_install_marks
  add-zsh-hook precmd _mygo_mark_prompt
  add-zsh-hook preexec _mygo_command_start
  _mygo_mark_prompt
}
typeset -ga precmd_functions
precmd_functions+=(_mygo_install_marks)
`

// Track only OSC 133, across read boundaries. Other control strings are
// opaque and OSC payloads are bounded; a TUI's text must not mark a prompt.
type promptTracker struct {
	state    byte
	body     []byte
	length   int
	atPrompt bool
}

func (p *promptTracker) feed(data []byte) {
	for _, b := range data {
		if b == 0x18 || b == 0x1a { // CAN / SUB
			p.state, p.length = 0, 0
			p.body = p.body[:0]
			continue
		}
		switch p.state {
		case 0:
			if b == 0x1b {
				p.state = 1
			}
		case 1:
			p.escape(b)
		case 2:
			p.length++
			switch b {
			case 7:
				p.finish()
				p.state = 0
			case 0x1b:
				p.state = 3
			default:
				if p.length <= 64 {
					p.body = append(p.body, b)
				}
			}
		case 3:
			if b == '\\' {
				p.finish()
				p.state = 0
			} else {
				p.escape(b) // ESC aborted the previous OSC.
			}
		case 4:
			if b == 0x1b {
				p.state = 5
			}
		case 5:
			if b == '\\' {
				p.state = 0
			} else if b != 0x1b {
				p.state = 4
			}
		}
	}
}

func (p *promptTracker) escape(b byte) {
	p.state = 0
	switch b {
	case ']':
		p.state, p.length, p.body = 2, 0, p.body[:0]
	case 'P', '_', '^', 'X': // DCS, APC, PM, SOS
		p.state = 4
	case 'c': // RIS
		p.atPrompt = false
	case 0x1b:
		p.state = 1
	}
}

func (p *promptTracker) finish() {
	if p.length > 64 {
		return
	}
	mark, ok := strings.CutPrefix(string(p.body), "133;")
	if !ok || mark == "" || len(mark) > 1 && mark[1] != ';' {
		return
	}
	switch mark[0] {
	case 'A', 'B', 'I', 'N', 'P':
		p.atPrompt = true
	case 'C', 'D':
		p.atPrompt = false
	}
}
