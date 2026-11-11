// Package mcpendpoint wraps the official MCP Go SDK with local bearer authentication.
// JSON-RPC, Streamable HTTP, SSE and cancellation are SDK-owned, not reimplemented here.
package mcpendpoint

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net"
	"net/http"
	"nextshell.dev/v2/internal/agent"
	"nextshell.dev/v2/internal/cloudv2"
	"strings"
	"sync"
	"time"
)

type Endpoint struct {
	mu, initMu sync.Mutex
	verifier   [32]byte
	sessions   map[string]time.Time
	gateway    *agent.Gateway
	server     *http.Server
	listener   net.Listener
	sdk        *mcp.Server
	handler    *mcp.StreamableHTTPHandler
	now        func() time.Time
	inflight   chan struct{}
}

func hashToken(token string) ([32]byte, error) {
	raw, e := base64.RawURLEncoding.Strict().DecodeString(token)
	if e != nil || len(raw) != 32 {
		return [32]byte{}, errors.New("MCP token must be random 32-byte base64url")
	}
	return sha256.Sum256(append([]byte("nextshell/mcp/v2/auth\x00"), raw...)), nil
}
func New(token string, gateway *agent.Gateway) (*Endpoint, error) {
	h, err := hashToken(token)
	if err != nil {
		return nil, err
	}
	e := &Endpoint{verifier: h, sessions: map[string]time.Time{}, gateway: gateway, now: time.Now, inflight: make(chan struct{}, 32)}
	e.sdk = mcp.NewServer(&mcp.Implementation{Name: "NextShell-v2", Version: "0.3.0"}, &mcp.ServerOptions{SupportedProtocolVersions: []string{"2025-11-25", "2025-06-18", "2025-03-26"}, GetSessionID: func() string { id := cloudv2.NewID(); e.mu.Lock(); e.sessions[id] = e.now(); e.mu.Unlock(); return id }})
	for _, definition := range Tools() {
		name := definition["name"].(string)
		readOnly := definition["annotations"].(map[string]any)["readOnlyHint"].(bool)
		destructive := !readOnly
		e.sdk.AddTool(&mcp.Tool{Name: name, Description: definition["description"].(string), InputSchema: definition["inputSchema"], Annotations: &mcp.ToolAnnotations{ReadOnlyHint: readOnly, DestructiveHint: &destructive}}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			owner := req.Session.ID()
			if owner == "" {
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "Session-bound approval requires a negotiated session"}}}, nil
			}
			clientName := "MCP client"
			if init := req.Session.InitializeParams(); init != nil && init.ClientInfo != nil {
				clientName = init.ClientInfo.Name
			}
			result := gateway.Call(ctx, owner, clientName, name, req.Params.Arguments)
			raw, _ := json.Marshal(result)
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}}, StructuredContent: result, IsError: !result.OK}, nil
		})
	}
	e.handler = mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return e.sdk }, &mcp.StreamableHTTPOptions{JSONResponse: true, SessionTimeout: 10 * time.Minute, MaxRequestBodyBytes: 64 << 10, PropagateRequestCancellation: true})
	return e, nil
}
func (e *Endpoint) Rotate(token string) error {
	h, err := hashToken(token)
	if err != nil {
		return err
	}
	e.gateway.Halt(true)
	e.mu.Lock()
	e.verifier = h
	e.sessions = map[string]time.Time{}
	e.mu.Unlock()
	for session := range e.sdk.Sessions() {
		session.Close()
	}
	return nil
}
func (e *Endpoint) Start(port string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.server != nil {
		return "", errors.New("MCP already running")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		return "", err
	}
	e.listener = ln
	e.server = &http.Server{Handler: e, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 125 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	server := e.server
	go server.Serve(ln)
	return "http://" + ln.Addr().String() + "/mcp", nil
}
func (e *Endpoint) Close() error {
	e.gateway.Halt(true)
	for session := range e.sdk.Sessions() {
		session.Close()
	}
	e.mu.Lock()
	server := e.server
	e.server = nil
	e.sessions = map[string]time.Time{}
	e.mu.Unlock()
	if server == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := server.Shutdown(ctx)
	if err != nil {
		server.Close()
	}
	return err
}
func (e *Endpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	host, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		host = r.Host
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	remote, _, remoteErr := net.SplitHostPort(r.RemoteAddr)
	if ip == nil || !ip.IsLoopback() || remoteErr != nil || net.ParseIP(remote) == nil || !net.ParseIP(remote).IsLoopback() || r.Header.Get("Origin") != "" {
		http.Error(w, "Loopback non-browser clients only", 403)
		return
	}
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	h, err := hashToken(token)
	e.mu.Lock()
	allowed := err == nil && strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") && subtle.ConstantTimeCompare(h[:], e.verifier[:]) == 1
	e.mu.Unlock()
	if !allowed {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "Unauthorized", 401)
		return
	}
	if r.URL.Path != "/mcp" || r.URL.RawQuery != "" {
		http.NotFound(w, r)
		return
	}
	select {
	case e.inflight <- struct{}{}:
		defer func() { <-e.inflight }()
	default:
		http.Error(w, "Request capacity", 503)
		return
	}
	sid := r.Header.Get("Mcp-Session-Id")
	if sid == "" {
		e.initMu.Lock()
		defer e.initMu.Unlock()
	}
	e.mu.Lock()
	for id, last := range e.sessions {
		if e.now().Sub(last) > 10*time.Minute {
			delete(e.sessions, id)
		}
	}
	if sid != "" {
		if _, ok := e.sessions[sid]; !ok {
			e.mu.Unlock()
			http.Error(w, "Session revoked or expired", 404)
			return
		}
		e.sessions[sid] = e.now()
	} else if len(e.sessions) >= 32 {
		e.mu.Unlock()
		http.Error(w, "Session capacity", 503)
		return
	}
	e.mu.Unlock()
	e.handler.ServeHTTP(w, r)
	if r.Method == "DELETE" {
		e.mu.Lock()
		delete(e.sessions, sid)
		e.mu.Unlock()
	}
}
func Tools() []map[string]any {
	var out []map[string]any
	for _, name := range []string{"host_list", "session_open", "session_list", "session_history", "session_read", "command_search", "command_save", "exec", "session_send_signal", "session_focus"} {
		properties := map[string]any{}
		add := func(n, t string) { properties[n] = map[string]any{"type": t} }
		switch name {
		case "host_list", "command_search":
			add("query", "string")
			add("limit", "integer")
		case "session_list":
		case "session_open":
			add("target", "string")
			add("reason", "string")
			add("requestId", "string")
		case "command_save":
			for _, k := range []string{"id", "name", "command", "group", "workspaceId", "requestId"} {
				add(k, "string")
			}
			add("appendCr", "boolean")
		case "exec":
			add("target", "string")
			add("command", "string")
			add("timeoutSec", "integer")
			add("allowSensitive", "boolean")
			add("requestId", "string")
		case "session_send_signal":
			add("target", "string")
			properties["signal"] = map[string]any{"type": "string", "enum": []string{"interrupt", "eof", "suspend", "quit"}}
			add("requestId", "string")
		case "session_focus":
			add("target", "string")
			add("requestId", "string")
		case "session_read":
			add("target", "string")
			add("lines", "integer")
			add("mode", "string")
		case "session_history":
			add("target", "string")
			add("limit", "integer")
		}
		readOnly := name == "host_list" || name == "session_list" || name == "session_read" || name == "session_history" || name == "command_search"
		out = append(out, map[string]any{"name": name, "description": name + ": NextShell v2; mutations require in-app approval; OSC133/foreground unavailable", "inputSchema": map[string]any{"type": "object", "properties": properties, "additionalProperties": false}, "annotations": map[string]any{"readOnlyHint": readOnly, "destructiveHint": !readOnly}})
	}
	return out
}
