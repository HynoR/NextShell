package sshclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"runtime"
	"strings"
)

// windowsAgentPipe is where the Windows OpenSSH ssh-agent service listens.
const windowsAgentPipe = `\\.\pipe\openssh-ssh-agent`

var errNoWindowsAgent = errors.New("未找到 OpenSSH Agent（Windows 设置 → 可选功能 → OpenSSH 客户端，并启动 ssh-agent 服务）")

// agentTarget picks the agent to reach: a "unix" socket path or, on Windows, a named "pipe".
// An explicit address (Options.AgentSocket) wins, then SSH_AUTH_SOCK. On Windows only a pipe path in
// SSH_AUTH_SOCK counts (Git Bash and Cygwin set it to a path Go cannot dial); otherwise the OpenSSH
// agent's pipe. An empty address means there is nothing to dial.
func agentTarget(goos, explicit, env string) (network, addr string) {
	isPipe := func(p string) bool {
		return goos == "windows" && strings.HasPrefix(strings.ToLower(p), `\\.\pipe\`)
	}
	switch {
	case isPipe(explicit):
		return "pipe", explicit
	case explicit != "":
		return "unix", explicit
	case isPipe(env):
		return "pipe", env
	case goos == "windows":
		return "pipe", windowsAgentPipe
	}
	return "unix", env
}

// dialAgent opens the agent. Pageant is not supported.
func dialAgent(ctx context.Context, explicit string) (io.ReadWriteCloser, error) {
	network, addr := agentTarget(runtime.GOOS, explicit, os.Getenv("SSH_AUTH_SOCK"))
	if addr == "" {
		return nil, errors.New("SSH_AUTH_SOCK 未设置")
	}
	if network == "unix" {
		return (&net.Dialer{}).DialContext(ctx, "unix", addr)
	}
	// A named pipe opens like a file. ponytail: blocking reads ignore ctx; the agent is local and answers at once.
	f, e := os.OpenFile(addr, os.O_RDWR, 0)
	switch {
	case errors.Is(e, fs.ErrNotExist):
		return nil, errNoWindowsAgent
	case e != nil:
		return nil, fmt.Errorf("连接 OpenSSH Agent: %w", e)
	}
	return f, nil
}
