package sshclient

import (
	"context"
	"runtime"
	"testing"
)

func TestAgentTarget(t *testing.T) {
	const pipe = `\\.\pipe\openssh-ssh-agent`
	for _, c := range []struct {
		goos, explicit, env, network, addr string
	}{
		{"linux", "", "/run/user/1000/agent.sock", "unix", "/run/user/1000/agent.sock"},
		{"darwin", "", "/tmp/launchd/Listeners", "unix", "/tmp/launchd/Listeners"},
		{"linux", "/tmp/a.sock", "/other", "unix", "/tmp/a.sock"},
		{"linux", "", "", "unix", ""},
		{"linux", "", pipe, "unix", pipe}, // only Windows has pipes
		{"windows", "", "", "pipe", pipe},
		{"windows", "", `\\.\PIPE\1password-agent`, "pipe", `\\.\PIPE\1password-agent`},
		{"windows", "", "/tmp/ssh-XXXX/agent.123", "pipe", pipe}, // Git Bash / Cygwin: not dialable
		{"windows", "", `C:\Users\me\agent.sock`, "pipe", pipe},
		{"windows", `\\.\pipe\custom`, `\\.\pipe\env`, "pipe", `\\.\pipe\custom`},
		{"windows", `C:\agent.sock`, "", "unix", `C:\agent.sock`}, // Windows has AF_UNIX
	} {
		if n, a := agentTarget(c.goos, c.explicit, c.env); n != c.network || a != c.addr {
			t.Errorf("agentTarget(%q, %q, %q) = %s %q, want %s %q", c.goos, c.explicit, c.env, n, a, c.network, c.addr)
		}
	}
}

func TestDialAgentErrors(t *testing.T) {
	if runtime.GOOS == "windows" {
		if _, e := dialAgent(context.Background(), `\\.\pipe\nextshell-no-such-agent`); e != errNoWindowsAgent {
			t.Fatal(e)
		}
		return
	}
	t.Setenv("SSH_AUTH_SOCK", "")
	if _, e := dialAgent(context.Background(), ""); e == nil || e.Error() != "SSH_AUTH_SOCK 未设置" {
		t.Fatal(e)
	}
}
