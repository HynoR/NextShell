// Package shellintegration supplies opt-in session-only OSC 7 scripts. OSC 133 remains deferred.
package shellintegration

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"nextshell.dev/v2/internal/sftpfs"
	"nextshell.dev/v2/internal/sshclient"
)

//go:embed scripts/cwd.bash
var bash string

//go:embed scripts/cwd.zsh
var zsh string

const (
	// sourceCommand is what the automatic mode types into a session, as SourceLine makes it. The
	// leading space keeps it out of the history of shells that ignore such lines; the file drops it
	// from bash's history either way.
	sourceCommand = " . ~/.cache/nextshell/osc7.sh"

	// Where the hook lives, relative to the home directory an SFTP session starts in.
	remoteDir  = ".cache/nextshell"
	remoteFile = remoteDir + "/osc7.sh"
)

func Script(shell string) (string, error) {
	switch shell {
	case "bash":
		return bash, nil
	case "zsh":
		return zsh, nil
	default:
		return "", errors.New("only bash/zsh cwd hooks are available; OSC 133 remains deferred")
	}
}

// quote single-quotes a script for eval, so a shell that does not match never parses it.
func quote(script string) string { return "'" + strings.ReplaceAll(script, "'", "'\"'\"'") + "'" }

// lineQuote quotes a script as one $'...' word, which bash and zsh both read: the whole script stays on
// one line, so typing it leaves no continuation prompts ("> ") on the screen.
func lineQuote(script string) string {
	r := strings.NewReplacer(`\`, `\\`, `'`, `\'`, "\n", `\n`, "\t", `\t`, "\r", `\r`, "\x1b", `\e`)
	return "$'" + r.Replace(strings.TrimRight(script, "\n")) + "'"
}

// Command is the manual mode's hook: one line that evals the shell's script.
func Command(shell string) (string, error) {
	script, e := Script(shell)
	if e != nil {
		return "", e
	}
	return " eval " + lineQuote(script), nil
}

// SourceLine is the line the automatic mode types at a prompt. rows is how many screen rows the prompt and
// the typed line take, which the file moves back over and clears once the hook is in: the screen shows the
// prompt as if nothing had been typed. 0 clears nothing.
func SourceLine(rows int) string {
	if rows <= 0 {
		return sourceCommand + "\r"
	}
	return fmt.Sprintf("%s %d\r", sourceCommand, rows)
}

// EchoRows is how many rows the prompt line (the screen's last line, as Terminal.Text joins wrapped lines)
// and SourceLine take on a screen cols wide: what SourceLine(rows) is to clear.
func EchoRows(prompt string, cols int) int {
	if cols <= 0 {
		return 0
	}
	w := 0
	for _, r := range prompt {
		w += runeWidth(r)
	}
	// Terminal text drops trailing blanks: a prompt ends with a space after its $, # or %.
	w += 1 + len(SourceLine(9)) - 1 // the line with a one-digit count, without its \r
	return (w + cols - 1) / cols
}

// bashForget drops the line that sourced the file from bash's history, when it is the last entry there (a
// HISTCONTROL with ignorespace has already left it out).
const bashForget = `__nextshell_h=$(HISTTIMEFORMAT= builtin history 1 2>/dev/null)
if [[ $__nextshell_h == *nextshell/osc7.sh* && $__nextshell_h =~ ^[[:space:]]*([0-9]+) ]]; then builtin history -d "${BASH_REMATCH[1]}" 2>/dev/null; fi
unset __nextshell_h`

// File is the content of osc7.sh: both hooks behind a shell check, so one file serves bash and zsh. Sourced
// with a row count ($1), it then erases that many rows above the cursor: the prompt and the line that
// sourced it, which the shell draws again.
func File() string {
	return "# NextShell directory tracking (OSC 7). Written by NextShell; safe to delete.\n" +
		"if [ -n \"$ZSH_VERSION\" ]; then\n  eval " + quote(zsh) + "\n" +
		"elif [ -n \"$BASH_VERSION\" ]; then\n  eval " + quote(bash) + "\n  eval " + quote(bashForget) + "\nfi\n" +
		"case \"${1:-}\" in ''|*[!0-9]*) ;; *) printf '\\033[%sA\\r\\033[J' \"$1\" ;; esac\n"
}

// Install detects the login shell over a separate exec channel and, for bash
// and zsh, writes osc7.sh into the server's ~/.cache/nextshell over SFTP: the
// directory 0700, the file 0600, rewritten only when its content differs.
// It types nothing; on success the caller sends SourceLine.
func Install(ctx context.Context, s *sshclient.Session) error {
	out, e := s.Output(ctx, `basename "$SHELL"`, 256)
	if e != nil {
		return fmt.Errorf("detect login shell: %w", e)
	}
	if shell := strings.TrimSpace(string(out)); shell != "bash" && shell != "zsh" {
		return fmt.Errorf("login shell %q is not bash or zsh", shell)
	}
	c, cleanup, e := s.OpenSFTPContext(ctx)
	if e != nil {
		return fmt.Errorf("open sftp: %w", e)
	}
	defer cleanup()
	fs := &sftpfs.Client{Client: c}
	info, e := c.Stat(remoteDir)
	switch {
	case os.IsNotExist(e):
		if e = c.MkdirAll(remoteDir); e == nil {
			e = c.Chmod(remoteDir, 0o700)
		}
	case e == nil && info.Mode().Perm() != 0o700:
		e = c.Chmod(remoteDir, 0o700)
	}
	if e != nil {
		return fmt.Errorf("prepare ~/%s: %w", remoteDir, e)
	}
	content := File()
	_, e = c.Lstat(remoteFile)
	exists := e == nil
	if e != nil && !os.IsNotExist(e) {
		return fmt.Errorf("check ~/%s: %w", remoteFile, e)
	}
	if exists {
		if f, e := c.Open(remoteFile); e == nil {
			have, e := io.ReadAll(io.LimitReader(f, int64(len(content))+1))
			f.Close()
			if e == nil && bytes.Equal(have, []byte(content)) {
				return nil
			}
		}
	}
	if e = fs.UploadReader(ctx, strings.NewReader(content), remoteFile, exists, 0o600, nil); e != nil {
		return fmt.Errorf("write ~/%s: %w", remoteFile, e)
	}
	return nil
}
