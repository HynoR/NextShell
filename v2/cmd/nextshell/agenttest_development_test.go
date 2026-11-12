//go:build nextshell_devtools

package main

import (
	"bytes"
	"flag"
	"io"
	"nextshell.dev/v2/internal/desktop"
	"nextshell.dev/v2/internal/exchange"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
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

func writeConnections(t *testing.T, dir string, entries ...exchange.Entry) string {
	t.Helper()
	raw, e := exchange.Encode(entries, nil, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	file := filepath.Join(dir, "hosts.json")
	if e = os.WriteFile(file, raw, 0o600); e != nil {
		t.Fatal(e)
	}
	return file
}

func TestAgentTestImportsConnectionsIntoTheTestProfile(t *testing.T) {
	dir := t.TempDir()
	pw := "secret"
	file := writeConnections(t, dir,
		exchange.Entry{Name: "web-1", Host: "10.0.0.1", Port: 22, Username: "tester", AuthType: "password", Password: &pw, GroupPath: "/server/prod", Tags: []string{"t"}, Encoding: "utf-8"},
		exchange.Entry{Name: "web-2", Host: "10.0.0.2", Port: 2222, Username: "tester", AuthType: "password", GroupPath: "/server", Encoding: "utf-8"},
		exchange.Entry{Name: "web-3", Host: "10.0.0.1", Port: 22, Username: "tester", AuthType: "password", GroupPath: "/server", Encoding: "utf-8"}) // same endpoint as web-1
	p, e := desktop.Open(filepath.Join(dir, "t.db"))
	if e != nil {
		t.Fatal(e)
	}
	p.SetAgentPort("0")
	t.Cleanup(p.Shutdown)
	var stderr bytes.Buffer
	if e = startDevMode(p, options{db: "t.db", agentTest: true, importFile: file}, &stderr); e != nil {
		t.Fatal(e)
	}
	snap, e := p.Snapshot()
	if e != nil || len(snap.Connections) != 3 {
		t.Fatal(snap.Connections, e)
	}
	names := snap.Connections[0].Name + snap.Connections[1].Name + snap.Connections[2].Name
	if !strings.Contains(names, "web-1") || !strings.Contains(names, "web-2") || !strings.Contains(names, "web-3") {
		t.Fatal(names)
	}
	if !strings.Contains(stderr.String(), "imported 3 connection(s)") {
		t.Fatal(stderr.String())
	}
	if len(snap.Folders) == 0 {
		t.Fatal("group paths should become folders")
	}
	// Host keys are not pre-trusted: the trust flow stays real.
	if pins, e := p.HostKeys(); e != nil || len(pins) != 0 {
		t.Fatal("import must not pin host keys", pins, e)
	}
	if o := p.AgentOverview(); o.URL == "" {
		t.Fatal("MCP not enabled after the import")
	}
}

func TestImportFlagNeedsAgentTestAndAReadableFile(t *testing.T) {
	fs := flag.NewFlagSet("nextshell", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if _, e := parseFlags(fs, []string{"--db", "x.db", "--import", "hosts.json"}); e == nil {
		t.Fatal("--import without --agent-test must be refused")
	}
	p, e := desktop.Open(filepath.Join(t.TempDir(), "t.db"))
	if e != nil {
		t.Fatal(e)
	}
	p.SetAgentPort("0")
	t.Cleanup(p.Shutdown)
	if e = startDevMode(p, options{db: "t.db", agentTest: true, importFile: "/does/not/exist.json"}, io.Discard); e == nil {
		t.Fatal("a missing file must stop the start")
	}
}
