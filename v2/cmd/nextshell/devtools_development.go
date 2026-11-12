//go:build nextshell_devtools

package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"nextshell.dev/v2/internal/agent"
	"nextshell.dev/v2/internal/desktop"
)

const developmentInspector = true

func registerDevFlags(fs *flag.FlagSet, o *options) {
	fs.BoolVar(&o.agentTest, "agent-test", false, "enable MCP with auto-approve until exit (requires --db; development builds only)")
}

// checkDevFlags runs before the profile is opened: auto-approve must never touch the user's real profile.
func checkDevFlags(o options) error {
	if o.agentTest && o.db == "" {
		return errors.New("--agent-test requires --db <path> so it never runs against your real profile")
	}
	return nil
}

// startDevMode turns on MCP and auto-approve for agent-driven testing and reports where mcp.json is.
func startDevMode(p *desktop.Platform, o options, stderr io.Writer) error {
	if !o.agentTest {
		return nil
	}
	if e := p.AgentEndpoint(true); e != nil {
		return e
	}
	if e := p.AgentAutoApprove(agent.UntilExit); e != nil {
		return e
	}
	path, e := p.MCPConfigPath()
	if e != nil {
		return e
	}
	_, e = fmt.Fprintln(stderr, "agent-test: MCP enabled, auto-approve until exit; mcp.json:", path)
	return e
}
