package mcpendpoint

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"nextshell.dev/v2/internal/agent"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// bridgeSnapshot is what @nextshell/mcp serves while the app is closed. It is generated from
// Tools(): NEXTSHELL_UPDATE_TOOLS=1 go test ./internal/mcpendpoint -run Snapshot
const bridgeSnapshot = "../../../packages/mcp/src/tools.generated.json"

func snapshot() []byte {
	raw, _ := json.MarshalIndent(map[string]any{"instructions": agent.Instructions, "tools": Tools()}, "", "  ")
	return append(raw, '\n')
}

func TestToolsMatchBridgeSnapshot(t *testing.T) {
	want := snapshot()
	if os.Getenv("NEXTSHELL_UPDATE_TOOLS") == "1" {
		if e := os.WriteFile(bridgeSnapshot, want, 0o644); e != nil {
			t.Fatal(e)
		}
		return
	}
	got, e := os.ReadFile(bridgeSnapshot)
	if os.IsNotExist(e) {
		t.Skip("packages/mcp is not part of this checkout (v2-only export)")
	}
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("packages/mcp/src/tools.generated.json is stale: run NEXTSHELL_UPDATE_TOOLS=1 go test ./internal/mcpendpoint -run Snapshot")
	}
}

func TestServedToolsCarryTheSpecsMetadata(t *testing.T) {
	_, _, _, url, token := start(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "t", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: url, HTTPClient: &http.Client{Transport: bearer{token}}, MaxRetries: -1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if init := session.InitializeResult(); init == nil || init.Instructions != agent.Instructions {
		t.Fatal("instructions not served")
	}
	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	specs := agent.Specs()
	if len(listed.Tools) != len(specs) {
		t.Fatal(len(listed.Tools))
	}
	byName := map[string]agent.ToolSpec{}
	for _, s := range specs {
		byName[s.Name] = s
	}
	for _, tool := range listed.Tools {
		s := byName[tool.Name]
		raw, _ := json.Marshal(tool.InputSchema)
		var served, defined map[string]any
		json.Unmarshal(raw, &served)
		raw, _ = json.Marshal(s.Schema)
		json.Unmarshal(raw, &defined)
		if tool.Name != s.Name || tool.Description != s.Description || tool.Title != s.Title || !reflect.DeepEqual(served, defined) {
			t.Fatal("served metadata differs from the spec for", s.Name, tool.Name == s.Name, tool.Description == s.Description, tool.Title == s.Title, reflect.DeepEqual(served, defined))
		}
		if tool.Annotations == nil || tool.Annotations.ReadOnlyHint != s.ReadOnly {
			t.Fatal("annotations", s.Name)
		}
	}
	// A tool with required fields really requires them.
	for _, tool := range listed.Tools {
		if tool.Name == "exec" {
			if req, _ := tool.InputSchema.(map[string]any)["required"].([]any); len(req) != 2 {
				t.Fatal("exec schema lost its required fields", tool.InputSchema)
			}
		}
	}
}

func rawPost(t *testing.T, url, token, host string) int {
	t.Helper()
	r, _ := http.NewRequest("POST", url, strings.NewReader(`{}`))
	r.Header.Set("Authorization", "Bearer "+token)
	if host != "" {
		r.Host = host
	}
	resp, e := http.DefaultClient.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func TestHostHeaderAcceptsLocalhostAndLoopbackLiteralsOnly(t *testing.T) {
	_, _, _, url, token := start(t)
	_, port, _ := net.SplitHostPort(strings.TrimSuffix(strings.TrimPrefix(url, "http://"), "/mcp"))
	for _, tc := range []struct {
		host string
		deny bool
	}{{"localhost:" + port, false}, {"localhost", false}, {"127.0.0.1:" + port, false}, {"127.0.0.2:" + port, false}, {"[::1]:" + port, false}, {"[::1]", false},
		{"evil.invalid:" + port, true}, {"localhost.evil.invalid:" + port, true}, {"evil.localhost.invalid", true}, {"192.168.1.5:" + port, true}, {"0.0.0.0:" + port, true}} {
		got := rawPost(t, url, token, tc.host)
		if (got == 403) != tc.deny {
			t.Errorf("Host %q: status %d", tc.host, got)
		}
	}
	// Origin is still refused whatever the host.
	r, _ := http.NewRequest("POST", url, strings.NewReader(`{}`))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Origin", "http://localhost")
	r.Host = "localhost:" + port
	resp, e := http.DefaultClient.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatal("browser origin accepted", resp.StatusCode)
	}
}

func TestEndpointAlsoListensOnIPv6Loopback(t *testing.T) {
	_, _, _, url, token := start(t)
	_, port, _ := net.SplitHostPort(strings.TrimSuffix(strings.TrimPrefix(url, "http://"), "/mcp"))
	if ln, e := net.Listen("tcp6", "[::1]:0"); e != nil {
		t.Skip("no IPv6 loopback on this machine")
	} else {
		ln.Close()
	}
	c, e := net.DialTimeout("tcp6", "[::1]:"+port, time.Second)
	if e != nil {
		t.Skip("port taken on ::1 by something else:", e)
	}
	c.Close()
	if got := rawPost(t, "http://[::1]:"+port+"/mcp", token, ""); got == 403 || got == 401 {
		t.Fatal("::1 not served, status", got)
	}
	if got := rawPost(t, "http://localhost:"+port+"/mcp", token, ""); got == 403 || got == 401 {
		t.Fatal("localhost not served, status", got)
	}
}

func initialize(t *testing.T, url, token string) (int, string) {
	t.Helper()
	raw := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"c","version":"1"}}}`
	r, _ := http.NewRequest("POST", url, strings.NewReader(raw))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	resp, e := http.DefaultClient.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	resp.Body.Close()
	return resp.StatusCode, resp.Header.Get("Mcp-Session-Id")
}
func ping(url, token, sid string) int {
	r, _ := http.NewRequest("POST", url, strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"ping"}`))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Mcp-Session-Id", sid)
	r.Header.Set("Mcp-Protocol-Version", "2025-06-18")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	resp, e := http.DefaultClient.Do(r)
	if e != nil {
		return -1
	}
	resp.Body.Close()
	return resp.StatusCode
}

func TestFullSessionTableEvictsTheLeastRecentlyUsedIdleSession(t *testing.T) {
	e, _, _, url, token := start(t)
	var ids []string
	for i := 0; i < maxSessions; i++ {
		code, sid := initialize(t, url, token)
		if code != 200 || sid == "" {
			t.Fatal("initialize", i, code)
		}
		ids = append(ids, sid)
	}
	// Use the first session so the second one becomes the stalest.
	e.mu.Lock()
	for i, id := range ids {
		e.sessions[id] = time.Now().Add(time.Duration(i) * time.Second)
	}
	e.sessions[ids[0]] = time.Now().Add(time.Hour) // most recently used
	e.mu.Unlock()
	code, fresh := initialize(t, url, token)
	if code != 200 || fresh == "" {
		t.Fatal("a full table must evict, not answer", code)
	}
	e.mu.Lock()
	_, firstKept := e.sessions[ids[0]]
	_, stalestKept := e.sessions[ids[1]]
	n := len(e.sessions)
	e.mu.Unlock()
	if !firstKept || stalestKept || n != maxSessions {
		t.Fatal("wrong victim", firstKept, stalestKept, n)
	}
	if got := ping(url, token, ids[1]); got != 404 {
		t.Fatal("evicted session still answers", got)
	}
	if got := ping(url, token, fresh); got != 200 {
		t.Fatal("new session unusable", got)
	}
}

func TestFullSessionTableNeverEvictsABusySession(t *testing.T) {
	e, g, _, url, token := start(t)
	var ids []string
	for i := 0; i < maxSessions; i++ {
		_, sid := initialize(t, url, token)
		ids = append(ids, sid)
	}
	e.mu.Lock()
	for _, id := range ids {
		e.active[id] = 1 // every session is serving a call
	}
	e.mu.Unlock()
	if code, _ := initialize(t, url, token); code != 503 {
		t.Fatal("all busy: want 503, got", code)
	}
	// One of them waits for an approval instead of running: still not evictable.
	e.mu.Lock()
	for _, id := range ids {
		delete(e.active, id)
	}
	e.mu.Unlock()
	requests := map[string]string{}
	for _, id := range ids {
		raw, _ := json.Marshal(map[string]any{"target": "session"})
		r := g.Call(context.Background(), id, "c", "session_focus", raw)
		if !r.OK {
			t.Fatal(r.Error)
		}
		requests[id] = r.Data.(map[string]any)["requestId"].(string)
	}
	if code, _ := initialize(t, url, token); code != 503 {
		t.Fatal("every session has a pending approval: want 503, got", code)
	}
	// Withdrawing one request makes exactly that session idle, and it is the one that goes.
	raw, _ := json.Marshal(map[string]any{"requestId": requests[ids[3]]})
	if r := g.Call(context.Background(), ids[3], "c", "request_cancel", raw); !r.OK {
		t.Fatal(r.Error)
	}
	if code, _ := initialize(t, url, token); code != 200 {
		t.Fatal("an idle session exists: want 200, got", code)
	}
	e.mu.Lock()
	_, kept := e.sessions[ids[3]]
	e.mu.Unlock()
	if kept {
		t.Fatal("wrong session evicted")
	}
}
