package mcpendpoint

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"nextshell.dev/v2/internal/agent"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type backend struct{ focus atomic.Int32 }

func (*backend) Hosts() ([]agent.Host, error) {
	return []agent.Host{{ID: "host", Name: "test host"}}, nil
}
func (*backend) Sessions() []agent.Session                          { return []agent.Session{{ID: "session", Connected: true}} }
func (*backend) Commands() ([]agent.Command, error)                 { return nil, nil }
func (*backend) SaveCommand(c agent.Command) (agent.Command, error) { return c, nil }
func (*backend) Read(string) (string, error)                        { return "screen", nil }
func (*backend) Exec(context.Context, string, string) (agent.ExecResult, error) {
	return agent.ExecResult{}, nil
}
func (*backend) Control(string, string) error                 { return nil }
func (b *backend) Focus(string) error                         { b.focus.Add(1); return nil }
func (*backend) Open(context.Context, string) (string, error) { return "session", nil }

type bearer struct{ token string }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	c := r.Clone(r.Context())
	c.Header = r.Header.Clone()
	c.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(c)
}
func testToken(b byte) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat(string(b), 32)))
}
func start(t *testing.T) (*Endpoint, *agent.Gateway, *backend, string, string) {
	t.Helper()
	b := &backend{}
	g := agent.New(b, nil)
	token := testToken('a')
	e, err := New(token, g)
	if err != nil {
		t.Fatal(err)
	}
	url, err := e.Start("0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.Close() })
	return e, g, b, url, token
}
func TestOfficialSDKNegotiationToolsConsentAndRotation(t *testing.T) {
	e, g, b, url, token := start(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "GoSDK-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: url, HTTPClient: &http.Client{Transport: bearer{token}}, MaxRetries: -1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tools, err := session.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) != 10 {
		t.Fatal(tools, err)
	}
	read, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "host_list", Arguments: map[string]any{}})
	if err != nil || read.IsError {
		t.Fatal(read, err)
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "session_focus", Arguments: map[string]any{"target": "session"}})
	if err != nil || result.IsError || b.focus.Load() != 0 {
		t.Fatal(result, err)
	}
	pending := g.Pending()
	if len(pending) != 1 || pending[0].Client != "GoSDK-test" {
		t.Fatal(pending)
	}
	if !g.Respond(pending[0].ID, true) {
		t.Fatal("approval failed")
	}
	result, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "session_focus", Arguments: map[string]any{"requestId": pending[0].ID}})
	if err != nil || result.IsError || b.focus.Load() != 1 {
		t.Fatal(result, err, b.focus.Load())
	}
	if err = e.Rotate(testToken('b')); err != nil || !g.Halted() {
		t.Fatal(err)
	}
	r, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatal(resp.Status)
	}
}
func TestMCPAuthenticationOriginHostBodyAndLegacyProtocol(t *testing.T) {
	_, _, _, url, token := start(t)
	for _, tc := range []struct {
		name, token, origin, host string
		want                      int
	}{{"missing", "", "", "", 401}, {"wrong", testToken('z'), "", "", 401}, {"browser", token, "http://127.0.0.1", "", 403}, {"dns-rebinding", token, "", "evil.invalid", 403}} {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := http.NewRequest("POST", url, strings.NewReader(`{}`))
			if tc.token != "" {
				r.Header.Set("Authorization", "Bearer "+tc.token)
			}
			r.Header.Set("Origin", tc.origin)
			if tc.host != "" {
				r.Host = tc.host
			}
			resp, e := http.DefaultClient.Do(r)
			if e != nil {
				t.Fatal(e)
			}
			resp.Body.Close()
			if resp.StatusCode != tc.want {
				t.Fatal(resp.Status)
			}
		})
	}
	raw := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"v1-transport-spec","version":"1"}}}`
	r, _ := http.NewRequest("POST", url, strings.NewReader(raw))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var result struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
		} `json:"result"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&result); err != nil || result.Result.ProtocolVersion != "2025-06-18" || resp.Header.Get("Mcp-Session-Id") == "" {
		t.Fatal(result, err, resp.Status)
	}

	sid := resp.Header.Get("Mcp-Session-Id")
	for _, method := range []string{"GET", "DELETE"} {
		request, _ := http.NewRequest(method, url, nil)
		request.Header.Set("Mcp-Session-Id", sid)
		unauthorized, e := http.DefaultClient.Do(request)
		if e != nil {
			t.Fatal(e)
		}
		unauthorized.Body.Close()
		if unauthorized.StatusCode != 401 {
			t.Fatal("method bypassed authentication", method, unauthorized.StatusCode)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Mcp-Session-Id", sid)
	request.Header.Set("Mcp-Protocol-Version", "2025-06-18")
	request.Header.Set("Accept", "text/event-stream")
	stream, e := http.DefaultClient.Do(request)
	if e != nil {
		t.Fatal("SDK SSE not established", e)
	}
	stream.Body.Close()
	if stream.StatusCode != 200 || !strings.Contains(stream.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatal("SSE not supported", stream.Status)
	}
}
