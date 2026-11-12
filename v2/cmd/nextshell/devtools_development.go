//go:build nextshell_devtools

package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"nextshell.dev/v2/internal/agent"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/desktop"
	"strings"
	"time"
)

const developmentInspector = true

func registerDevFlags(fs *flag.FlagSet, o *options) {
	fs.BoolVar(&o.agentTest, "agent-test", false, "enable MCP with auto-approve until exit (requires --db; development builds only)")
	fs.StringVar(&o.importFile, "import", "", "with --agent-test: import a plain nextshell-connections v1 JSON file into the test profile at startup (development builds only)")
}

// checkDevFlags runs before the profile is opened: auto-approve must never touch the user's real profile.
func checkDevFlags(o options) error {
	if o.agentTest && o.db == "" {
		return errors.New("--agent-test requires --db <path> so it never runs against your real profile")
	}
	if o.importFile != "" && !o.agentTest {
		return errors.New("--import is only for the --agent-test profile")
	}
	return nil
}

// startDevMode turns on MCP and auto-approve for agent-driven testing and reports where mcp.json is.
func startDevMode(p *desktop.Platform, o options, stderr io.Writer) error {
	if !o.agentTest {
		return nil
	}
	if o.importFile != "" {
		n, e := importFile(p, o.importFile)
		if e != nil {
			return fmt.Errorf("--import %s: %w", o.importFile, e)
		}
		fmt.Fprintf(stderr, "agent-test: imported %d connection(s) from %s\n", n, o.importFile)
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

// importFile runs the app's own import (preview, then commit with "skip" on duplicates) and waits for it,
// so test agents get saved hosts without a private seeder. Host keys stay unpinned: trust is still asked.
func importFile(p *desktop.Platform, file string) (int, error) {
	p.PreviewImport(file, "", "nextshell")
	var preview *appui.ImportPreview
	if e := awaitEvent(p, func(ev appui.Event) bool { preview = ev.Import; return preview != nil }); e != nil {
		return 0, e
	}
	p.CommitImport(preview.Token, "", "skip")
	if e := awaitEvent(p, func(ev appui.Event) bool { return strings.HasPrefix(ev.Message, "已导入") }); e != nil {
		return 0, e
	}
	return preview.Count, nil
}

// awaitEvent consumes the platform's events (the window is not up yet) until pick accepts one;
// a message that arrives first is the failure.
func awaitEvent(p *desktop.Platform, pick func(appui.Event) bool) error {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		for _, ev := range p.Poll() {
			if pick(ev) {
				return nil
			}
			if ev.Message != "" {
				return errors.New(ev.Message)
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	return errors.New("timed out")
}
