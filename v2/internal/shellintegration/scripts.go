// Package shellintegration supplies opt-in session-only OSC 7 scripts. OSC 133 remains deferred.
package shellintegration

import (
	_ "embed"
	"errors"
	"strings"
)

//go:embed scripts/cwd.bash
var bash string

//go:embed scripts/cwd.zsh
var zsh string

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
func Command(shell string) (string, error) {
	script, e := Script(shell)
	if e != nil {
		return "", e
	}
	return "eval '" + strings.ReplaceAll(script, "'", "'\"'\"'") + "'", nil
}
