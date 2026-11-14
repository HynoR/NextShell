package desktop

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"nextshell.dev/v2/internal/agent"
	"nextshell.dev/v2/internal/mcpendpoint"
)

func fileProfile(t *testing.T, dir string) *Platform {
	t.Helper()
	p, e := Open(filepath.Join(dir, "t.db"))
	if e != nil {
		t.Fatal(e)
	}
	p.SetAgentPort("0")
	t.Cleanup(p.Shutdown)
	return p
}
func readDiscovery(t *testing.T, path string) mcpendpoint.Discovery {
	t.Helper()
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	var d mcpendpoint.Discovery
	if e = json.Unmarshal(raw, &d); e != nil {
		t.Fatal(e)
	}
	return d
}

func TestMCPDiscoveryFileFollowsEnableRotateDisableAndShutdown(t *testing.T) {
	dir := t.TempDir()
	p := fileProfile(t, dir)
	want := filepath.Join(dir, "mcp.json") // next to the --db file
	if path, e := p.MCPConfigPath(); e != nil || path != want {
		t.Fatal(path, e)
	}
	if _, e := os.Stat(want); e == nil {
		t.Fatal("file exists while MCP is off")
	}
	if e := p.AgentEndpoint(true); e != nil {
		t.Fatal(e)
	}
	token, _ := p.AgentToken(false)
	d := readDiscovery(t, want)
	if d.Token != token || !strings.HasSuffix(p.AgentOverview().URL, ":"+strconv.Itoa(d.Port)+"/mcp") {
		t.Fatal(d, p.AgentOverview().URL)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(want); fi.Mode().Perm() != 0o600 {
			t.Fatal("mode", fi.Mode())
		}
	}
	rotated, e := p.AgentToken(true)
	if e != nil || rotated == token || readDiscovery(t, want).Token != rotated {
		t.Fatal("rotation not published", e)
	}
	if e = p.AgentEndpoint(false); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(want); !os.IsNotExist(e) {
		t.Fatal("file survived disable")
	}
	if e = p.AgentEndpoint(true); e != nil {
		t.Fatal(e)
	}
	if readDiscovery(t, want).Token != rotated {
		t.Fatal("re-enable did not publish current token")
	}
	p.Shutdown()
	if _, e = os.Stat(want); !os.IsNotExist(e) {
		t.Fatal("file survived clean exit")
	}
}

func TestMCPDiscoveryDefaultProfileLocation(t *testing.T) {
	db, e := DefaultPath()
	if e != nil {
		t.Skip(e)
	}
	p := &Platform{dbPath: db}
	path, e := p.discoveryPathLocked()
	cfg, _ := os.UserConfigDir()
	if e != nil || path != filepath.Join(cfg, "NextShell-v2", "mcp.json") {
		t.Fatal(path, e)
	}
}

func TestMCPDiscoveryInMemoryProfileUsesPrivateTempDir(t *testing.T) {
	p := desktopFixture(t)
	p.SetAgentPort("0")
	if e := p.AgentEndpoint(true); e != nil {
		t.Fatal(e)
	}
	path, _ := p.MCPConfigPath()
	if !strings.HasPrefix(path, os.TempDir()) {
		t.Fatal("in-memory profile must use a temp dir, not the real config dir", path)
	}
	if readDiscovery(t, path).Token == "" {
		t.Fatal("empty token")
	}
	p.Shutdown()
	if _, e := os.Stat(filepath.Dir(path)); !os.IsNotExist(e) {
		t.Fatal("temp dir left behind", e)
	}
}

func TestMCPEnableFailsCleanlyWhenDiscoveryPathIsASymlink(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(dir, "victim")
	os.WriteFile(victim, []byte("keep"), 0o600)
	if e := os.Symlink(victim, filepath.Join(dir, "mcp.json")); e != nil {
		t.Skip(e)
	}
	p := fileProfile(t, dir)
	if e := p.AgentEndpoint(true); e == nil {
		t.Fatal("enabled with a symlinked token file")
	}
	if raw, _ := os.ReadFile(victim); string(raw) != "keep" {
		t.Fatal("wrote through symlink")
	}
	if p.AgentOverview().URL != "" || !p.AgentOverview().Halted {
		t.Fatal("endpoint left running", p.AgentOverview())
	}
}

func TestAgentAutoApprovePlumbingNeverPersistsAndStopsWithMCP(t *testing.T) {
	dir := t.TempDir()
	p := fileProfile(t, dir)
	if p.AgentAutoApprove(time.Hour) == nil {
		t.Fatal("auto-approve while MCP is off")
	}
	if e := p.AgentEndpoint(true); e != nil {
		t.Fatal(e)
	}
	if e := p.AgentAutoApprove(agent.UntilExit); e != nil || !p.AgentAutoApproval().UntilExit {
		t.Fatal(e)
	}
	p.AgentHalt(true) // emergency stop
	if p.AgentAutoApproval().On {
		t.Fatal("halt kept auto-approve")
	}
	p.AgentHalt(false)
	if p.AgentAutoApproval().On {
		t.Fatal("resume restored auto-approve")
	}
	p.AgentAutoApprove(15 * time.Minute)
	if s := p.AgentAutoApproval(); !s.On || s.Expires.IsZero() {
		t.Fatal(s)
	}
	p.AgentEndpoint(false)
	if p.AgentAutoApproval().On {
		t.Fatal("disabling MCP kept auto-approve")
	}
	p.AgentEndpoint(true)
	p.AgentAutoApprove(agent.UntilExit)
	p.Shutdown()
	q := fileProfile(t, dir) // same profile after a restart
	if q.AgentAutoApproval().On {
		t.Fatal("auto-approve survived restart")
	}
	q.AgentEndpoint(true)
	if q.AgentAutoApproval().On {
		t.Fatal("auto-approve survived re-enable")
	}
}

type bearerTransport string

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+string(b))
	return http.DefaultTransport.RoundTrip(r)
}

// The token written to mcp.json is all a client needs; auto-approve then lets a mutating
// call through the real endpoint and backend without anyone clicking.
func TestMCPClientUsingDiscoveryFileRunsMutationUnderAutoApprove(t *testing.T) {
	p := fileProfile(t, t.TempDir())
	if e := p.AgentEndpoint(true); e != nil {
		t.Fatal(e)
	}
	path, _ := p.MCPConfigPath()
	d := readDiscovery(t, path)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	connect := func() *mcp.ClientSession {
		client := mcp.NewClient(&mcp.Implementation{Name: "discovery-test", Version: "1"}, nil)
		s, e := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: "http://127.0.0.1:" + strconv.Itoa(d.Port) + "/mcp", HTTPClient: &http.Client{Transport: bearerTransport(d.Token)}, MaxRetries: -1}, nil)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { s.Close() })
		return s
	}
	s := connect()
	save := func(name string) map[string]any {
		r, e := s.CallTool(ctx, &mcp.CallToolParams{Name: "command_save", Arguments: map[string]any{"name": name, "command": "echo " + name}})
		if e != nil || r.IsError {
			t.Fatal(r, e)
		}
		return r.StructuredContent.(map[string]any)
	}
	if out := save("manual"); out["data"].(map[string]any)["status"] != "pending" || len(p.AgentOverview().Pending) != 1 {
		t.Fatal("without auto-approve the call must wait for the user", out)
	}
	if e := p.AgentAutoApprove(agent.UntilExit); e != nil {
		t.Fatal(e)
	}
	out := save("auto")
	if out["ok"] != true || out["data"].(map[string]any)["status"] == "pending" || len(p.AgentOverview().Pending) != 1 {
		t.Fatal("auto-approve did not run the call", out)
	}
	snap, _ := p.Snapshot()
	var saved bool
	for _, c := range snap.Commands {
		saved = saved || c.Name == "auto"
	}
	if !saved {
		t.Fatal("command not persisted by the auto-approved call")
	}
	p.AgentHalt(true)
	if r, e := s.CallTool(ctx, &mcp.CallToolParams{Name: "command_save", Arguments: map[string]any{"name": "x", "command": "x"}}); e == nil && !r.IsError {
		t.Fatal("call allowed after emergency stop")
	}
}

// Turning MCP on is remembered: the next start of the same profile turns it on again, and off stays off.
func TestMCPEnabledSurvivesARestart(t *testing.T) {
	dir := t.TempDir()
	open := func() *Platform {
		p, e := Open(filepath.Join(dir, "t.db"))
		if e != nil {
			t.Fatal(e)
		}
		p.SetAgentPort("0")
		return p
	}
	p := open()
	p.RestoreAgentEndpoint()
	if p.AgentOverview().URL != "" {
		t.Fatal("MCP on in a new profile")
	}
	if e := p.AgentEndpoint(true); e != nil {
		t.Fatal(e)
	}
	p.Shutdown() // quitting is not turning it off
	p = open()
	p.RestoreAgentEndpoint()
	if p.AgentOverview().URL == "" {
		t.Fatal("MCP was on when the app quit, but is off after the restart")
	}
	if _, e := os.Stat(filepath.Join(dir, "mcp.json")); e != nil {
		t.Fatal("no discovery file after the restart:", e)
	}
	if e := p.AgentEndpoint(false); e != nil {
		t.Fatal(e)
	}
	p.Shutdown()
	p = open()
	defer p.Shutdown()
	p.RestoreAgentEndpoint()
	if p.AgentOverview().URL != "" {
		t.Fatal("MCP turned off, but on after the restart")
	}
}
