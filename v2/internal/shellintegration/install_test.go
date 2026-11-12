package shellintegration

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nextshell.dev/v2/internal/sshclient"
	"nextshell.dev/v2/internal/testutil"
)

// remote starts the loopback server (its root stands in for the home directory)
// and a session whose login shell the test sets with SHELL.
func remote(t *testing.T, shell string) (*sshclient.Session, string, *testutil.Server) {
	t.Helper()
	t.Setenv("SHELL", shell)
	root := t.TempDir()
	server, e := testutil.New(root)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(server.Close)
	s, e := sshclient.Dial(context.Background(), server.Options())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return s, root, server
}

func TestInstallDetectsTheLoginShell(t *testing.T) {
	for _, v := range []struct {
		shell string
		ok    bool
	}{{"/bin/bash", true}, {"/usr/bin/zsh", true}, {"/usr/bin/fish", false}, {"/bin/tcsh", false}, {"", false}, {"/opt/zsh-5/bin/zshx", false}} {
		t.Run(v.shell, func(t *testing.T) {
			s, root, _ := remote(t, v.shell)
			e := Install(context.Background(), s)
			_, statErr := os.Stat(filepath.Join(root, ".cache/nextshell/osc7.sh"))
			if v.ok && (e != nil || statErr != nil) {
				t.Fatal("supported shell skipped", e, statErr)
			}
			if !v.ok && (e == nil || !os.IsNotExist(statErr) || !strings.Contains(e.Error(), "not bash or zsh")) {
				t.Fatal("unsupported shell proceeded or wrote a file", e, statErr)
			}
		})
	}
}

func TestInstallWritesTheFileOnceWithPrivateModes(t *testing.T) {
	s, root, server := remote(t, "/bin/bash")
	dir, file := filepath.Join(root, ".cache/nextshell"), filepath.Join(root, ".cache/nextshell/osc7.sh")
	if e := Install(context.Background(), s); e != nil {
		t.Fatal(e)
	}
	got, e := os.ReadFile(file)
	if e != nil || string(got) != File() {
		t.Fatal("content", e)
	}
	if di, fi := mustStat(t, dir), mustStat(t, file); di.Mode().Perm() != 0o700 || fi.Mode().Perm() != 0o600 {
		t.Fatalf("modes %v %v", di.Mode().Perm(), fi.Mode().Perm())
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatal("temporary files left behind", entries)
	}

	// An identical file is left alone: its modification time does not move.
	old := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
	if e := os.Chtimes(file, old, old); e != nil {
		t.Fatal(e)
	}
	if e := Install(context.Background(), s); e != nil {
		t.Fatal(e)
	}
	if !mustStat(t, file).ModTime().Equal(old) {
		t.Fatal("identical content was rewritten")
	}

	// A different one is replaced, and the directory's mode repaired.
	if e := os.WriteFile(file, []byte("stale"), 0o644); e != nil {
		t.Fatal(e)
	}
	if e := os.Chmod(dir, 0o755); e != nil {
		t.Fatal(e)
	}
	if e := Install(context.Background(), s); e != nil {
		t.Fatal(e)
	}
	got, _ = os.ReadFile(file)
	if string(got) != File() || mustStat(t, file).Mode().Perm() != 0o600 || mustStat(t, dir).Mode().Perm() != 0o700 {
		t.Fatal("stale file kept", string(got))
	}
	if server.SFTP.Load() != 3 || server.Execs.Load() != 3 {
		t.Fatal("one exec and one sftp channel per install", server.Execs.Load(), server.SFTP.Load())
	}
}

func TestInstallFailureLeavesNothingBehind(t *testing.T) {
	s, root, _ := remote(t, "/bin/zsh")
	// A directory where the file should go: the write fails, no hook is installed.
	if e := os.MkdirAll(filepath.Join(root, ".cache/nextshell/osc7.sh"), 0o700); e != nil {
		t.Fatal(e)
	}
	if e := Install(context.Background(), s); e == nil {
		t.Fatal("write over a directory succeeded")
	}
	s.Close()
	if e := Install(context.Background(), s); e == nil {
		t.Fatal("closed session succeeded")
	}
}

// The file the line sources works in each shell and, sourced again, hooks once.
func TestFileSourcesInBashAndZsh(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "osc7.sh")
	if e := os.WriteFile(file, []byte(File()), 0o600); e != nil {
		t.Fatal(e)
	}
	for _, shell := range []string{"bash", "zsh"} {
		path, e := exec.LookPath(shell)
		if e != nil {
			t.Logf("UNVERIFIED: %s is not installed", shell)
			continue
		}
		cmd := exec.Command(path, "-c", ". "+file+"; . "+file)
		cmd.Dir = dir
		if shell == "bash" {
			cmd.Args = []string{path, "--noprofile", "--norc", "-c", ". " + file + "; . " + file + `; [[ "$PROMPT_COMMAND" == *__nextshell_emit_cwd* ]] || exit 3; [[ "${PROMPT_COMMAND/__nextshell_emit_cwd/}" != *__nextshell_emit_cwd* ]] || exit 4`}
		} else {
			cmd.Args = []string{path, "-f", "-c", ". " + file + "; . " + file + `; [[ ${#precmd_functions[@]} == 1 ]] || exit 3`}
		}
		out, e := cmd.CombinedOutput()
		if e != nil || strings.Count(string(out), "\x1b]7;file://localhost") != 2 {
			t.Fatal(shell, e, string(out))
		}
	}
}

func TestSourceLineIsOneShortLine(t *testing.T) {
	if strings.Count(SourceLine, "\r") != 1 || !strings.HasPrefix(SourceLine, " ") || len(SourceLine) > 40 || strings.Contains(SourceLine, "\n") {
		t.Fatalf("%q", SourceLine)
	}
}

func mustStat(t *testing.T, p string) os.FileInfo {
	t.Helper()
	i, e := os.Stat(p)
	if e != nil {
		t.Fatal(e)
	}
	return i
}
