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
	// SourceLine is the one line the automatic mode types into a session. The
	// leading space keeps it out of the history of shells that ignore such lines.
	SourceLine = " . ~/.cache/nextshell/osc7.sh\r"

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

func Command(shell string) (string, error) {
	script, e := Script(shell)
	if e != nil {
		return "", e
	}
	return "eval " + quote(script), nil
}

// File is the content of osc7.sh: both hooks behind a shell check, so one file serves bash and zsh.
func File() string {
	return "# NextShell directory tracking (OSC 7). Written by NextShell; safe to delete.\n" +
		"if [ -n \"$ZSH_VERSION\" ]; then\n  eval " + quote(zsh) + "\n" +
		"elif [ -n \"$BASH_VERSION\" ]; then\n  eval " + quote(bash) + "\nfi\n"
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
