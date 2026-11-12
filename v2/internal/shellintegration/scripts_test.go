package shellintegration

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"nextshell.dev/v2/internal/testutil"
)

type bashBinary struct {
	path         string
	major, minor int
}

func (b bashBinary) name() string { return fmt.Sprintf("bash%d.%d", b.major, b.minor) }

// bashes returns the bash under test: the one on PATH and /bin/bash when it is another binary. macOS
// ships bash 3.2 as /bin/bash, and remote macOS hosts run it, so the hook must work there as well as
// on the newer bash a developer or CI image has on PATH. NEXTSHELL_TEST_BASH adds one more.
func bashes(t *testing.T) []bashBinary {
	t.Helper()
	var found []bashBinary
	seen := map[string]bool{}
	for _, candidate := range []string{"bash", "/bin/bash", os.Getenv("NEXTSHELL_TEST_BASH")} {
		path, e := exec.LookPath(candidate)
		if candidate == "" || e != nil {
			continue
		}
		if real, e := filepath.EvalSymlinks(path); e == nil {
			path = real
		}
		if seen[path] {
			continue
		}
		seen[path] = true
		b := bashBinary{path: path}
		out, e := exec.Command(path, "--noprofile", "--norc", "-c", `echo "${BASH_VERSINFO[0]} ${BASH_VERSINFO[1]}"`).Output()
		if _, e2 := fmt.Sscan(string(out), &b.major, &b.minor); e != nil || e2 != nil {
			t.Fatalf("%s: cannot read its version: %v %v %q", path, e, e2, out)
		}
		found = append(found, b)
	}
	if len(found) == 0 {
		t.Skip("UNVERIFIED: bash is not installed")
	}
	return found
}

func TestBashOSC7PercentEscapingPreservesHooksAndIdempotence(t *testing.T) {
	testutil.RequirePOSIXRemote(t) // the directory name below holds ESC, which Windows file names cannot
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
	for _, bash := range bashes(t) {
		for _, kind := range []string{"string", "trailing-semicolon", "comment", "array"} {
			t.Run(bash.name()+"/"+kind, func(t *testing.T) {
				if kind == "array" && (bash.major < 5 || bash.major == 5 && bash.minor < 1) {
					t.Skipf("PROMPT_COMMAND is only an array of hooks from bash 5.1, %s is %d.%d", bash.path, bash.major, bash.minor)
				}
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
				cmd := exec.Command(bash.path, "--noprofile", "--norc", "-c", script)
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
