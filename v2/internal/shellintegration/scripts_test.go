package shellintegration

import (
	"bytes"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBashOSC7PercentEscapingPreservesHooksAndIdempotence(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "中 文%\x1b;dir")
	if e := os.Mkdir(directory, 0700); e != nil {
		t.Fatal(e)
	}
	hook, e := Command("bash")
	if e != nil {
		t.Fatal(e)
	}
	encoded := (&url.URL{Scheme: "file", Host: "localhost", Path: directory}).String()
	encoded = strings.ReplaceAll(encoded, ";", "%3B")
	osc := "\x1b]7;" + encoded + "\x07"
	for _, kind := range []string{"string", "trailing-semicolon", "comment", "array"} {
		t.Run(kind, func(t *testing.T) {
			pre := "PROMPT_COMMAND='printf previous-hook'"
			if kind == "trailing-semicolon" {
				pre = "PROMPT_COMMAND='printf previous-hook; '"
			}
			if kind == "comment" {
				pre = "PROMPT_COMMAND='printf previous-hook # existing comment'"
			}
			tail := `[[ $PROMPT_COMMAND == *'printf previous-hook'* ]] || exit 3; [[ $PROMPT_COMMAND == *'__nextshell_emit_cwd'* ]] || exit 4; remainder="${PROMPT_COMMAND/__nextshell_emit_cwd/}"; [[ $remainder != *'__nextshell_emit_cwd'* ]] || exit 5; eval "$PROMPT_COMMAND"`
			if kind == "array" {
				pre = "PROMPT_COMMAND=('printf previous-hook')"
				tail = `[[ ${PROMPT_COMMAND[0]} == 'printf previous-hook' && ${PROMPT_COMMAND[1]} == '__nextshell_emit_cwd' && ${#PROMPT_COMMAND[@]} == 2 ]] || exit 6; for hook in "${PROMPT_COMMAND[@]}"; do eval "$hook"; done`
			}
			script := pre + "\n" + hook + "\n" + hook + "\n" + tail
			cmd := exec.Command("bash", "--noprofile", "--norc", "-c", script)
			cmd.Dir = directory
			out, e := cmd.CombinedOutput()
			if e != nil || !bytes.Equal(out, []byte(osc+osc+"previous-hook"+osc)) {
				t.Fatalf("bash hook: %v\n%q wanted %q", e, out, osc+osc+"previous-hook"+osc)
			}
			if bytes.Contains(out, []byte("133;")) {
				t.Fatal("OSC133 should remain deferred")
			}
		})
	}
}
func TestShellIntegrationRejectsUnsupportedShellAndDoesNotEditRC(t *testing.T) {
	for _, s := range []string{"fish", "bash; echo unexpected", "", "/bin/bash"} {
		if _, e := Command(s); e == nil {
			t.Fatal("unsupported shell accepted", s)
		}
	}
	for _, shell := range []string{"bash", "zsh"} {
		script, e := Script(shell)
		if e != nil || strings.Contains(script, ".bashrc") || strings.Contains(script, ".zshrc") || strings.Contains(script, "133;") {
			t.Fatal("persistent changes or OSC133 observer", shell, e)
		}
	}
}
func TestZshOSC7HookWhenAvailable(t *testing.T) {
	zsh, e := exec.LookPath("zsh")
	if e != nil {
		t.Skip("UNVERIFIED: zsh is not installed on this Linux box")
	}
	hook, e := Command("zsh")
	if e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command(zsh, "-f", "-c", hook+"\n"+hook+"\n"+`[[ ${#precmd_functions[@]} == 1 ]] || exit 7`)
	cmd.Dir = t.TempDir()
	out, e := cmd.CombinedOutput()
	if e != nil || bytes.Count(out, []byte("\x1b]7;")) != 2 {
		t.Fatal(e, string(out))
	}
}
