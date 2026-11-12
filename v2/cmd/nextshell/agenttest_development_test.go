//go:build nextshell_devtools

package main

import (
	"bytes"
	"flag"
	"io"
	"nextshell.dev/v2/internal/desktop"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAgentTestRefusesTheDefaultProfile(t *testing.T) {
	fs := flag.NewFlagSet("nextshell", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if _, e := parseFlags(fs, []string{"--agent-test"}); e == nil {
		t.Fatal("--agent-test without --db would auto-approve against the real profile")
	}
}

func TestAgentTestEnablesMCPAndAutoApproveAndReportsPath(t *testing.T) {
	dir := t.TempDir()
	p, e := desktop.Open(filepath.Join(dir, "t.db"))
	if e != nil {
		t.Fatal(e)
	}
	p.SetAgentPort("0")
	t.Cleanup(p.Shutdown)
	var stderr bytes.Buffer
	if e = startDevMode(p, options{db: "t.db", agentTest: true}, &stderr); e != nil {
		t.Fatal(e)
	}
	want := filepath.Join(dir, "mcp.json")
	if !strings.Contains(stderr.String(), want) {
		t.Fatal("mcp.json path not printed:", stderr.String())
	}
	if o := p.AgentOverview(); o.URL == "" || o.Halted {
		t.Fatal("MCP not enabled", o)
	}
	if s := p.AgentAutoApproval(); !s.On || !s.UntilExit {
		t.Fatal("auto-approve not on until exit", s)
	}
	if fi, e := os.Stat(want); e != nil || (runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600) {
		t.Fatal(e)
	}
	p.Shutdown()
	if _, e = os.Stat(want); !os.IsNotExist(e) {
		t.Fatal("mcp.json left behind after exit")
	}
}

func TestWithoutAgentTestNothingIsEnabled(t *testing.T) {
	p, e := desktop.Open(":memory:")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(p.Shutdown)
	if e = startDevMode(p, options{}, io.Discard); e != nil || p.AgentOverview().URL != "" || p.AgentAutoApproval().On {
		t.Fatal(e)
	}
}
