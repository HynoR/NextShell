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

// maxSessions is how many MCP sessions may be open at once.
const maxSessions = 32

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
	active     map[string]int // POST requests being served, per MCP session
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
	e := &Endpoint{verifier: h, sessions: map[string]time.Time{}, active: map[string]int{}, gateway: gateway, now: time.Now, inflight: make(chan struct{}, 32)}
	e.sdk = mcp.NewServer(&mcp.Implementation{Name: "NextShell-v2", Version: "0.3.0"}, &mcp.ServerOptions{Instructions: agent.Instructions, SupportedProtocolVersions: []string{"2025-11-25", "2025-06-18", "2025-03-26"}, GetSessionID: func() string { id := cloudv2.NewID(); e.mu.Lock(); e.sessions[id] = e.now(); e.mu.Unlock(); return id }})
	for _, spec := range agent.Specs() {
		name := spec.Name
		destructive, open := spec.Gated, true
		e.sdk.AddTool(&mcp.Tool{Name: name, Title: spec.Title, Description: spec.Description, InputSchema: spec.Schema, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: spec.ReadOnly, DestructiveHint: &destructive, IdempotentHint: spec.Idempotent, OpenWorldHint: &open}}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
	// Clients that resolve "localhost" to ::1 should work too; hosts without IPv6 loopback just skip it.
	if _, p, err := net.SplitHostPort(ln.Addr().String()); err == nil {
		if ln6, err := net.Listen("tcp6", "[::1]:"+p); err == nil {
			go server.Serve(ln6)
		}
	}
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
	host = strings.Trim(host, "[]")
	ip := net.ParseIP(host)
	remote, _, remoteErr := net.SplitHostPort(r.RemoteAddr)
	// Host must name this machine's loopback; other names (DNS rebinding) are refused.
	if !(strings.EqualFold(host, "localhost") || (ip != nil && ip.IsLoopback())) || remoteErr != nil || net.ParseIP(remote) == nil || !net.ParseIP(remote).IsLoopback() || r.Header.Get("Origin") != "" {
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
	evict := ""
	if sid != "" {
		if _, ok := e.sessions[sid]; !ok {
			e.mu.Unlock()
			http.Error(w, "Session revoked or expired", 404)
			return
		}
		e.sessions[sid] = e.now()
		if r.Method == "POST" {
			e.active[sid]++
			defer func() {
				e.mu.Lock()
				if e.active[sid]--; e.active[sid] <= 0 {
					delete(e.active, sid)
				}
				e.mu.Unlock()
			}()
		}
	} else if len(e.sessions) >= maxSessions {
		// Full: make room by dropping the session that has been idle the longest, never one that is
		// serving a call or waiting for an approval.
		for id, last := range e.sessions {
			if e.active[id] == 0 && !e.gateway.HasPending(id) && (evict == "" || last.Before(e.sessions[evict])) {
				evict = id
			}
		}
		if evict == "" {
			e.mu.Unlock()
			http.Error(w, "Session capacity", 503)
			return
		}
		delete(e.sessions, evict)
	}
	e.mu.Unlock()
	if evict != "" {
		for session := range e.sdk.Sessions() {
			if session.ID() == evict {
				session.Close()
			}
		}
	}
	e.handler.ServeHTTP(w, r)
	if r.Method == "DELETE" {
		e.mu.Lock()
		delete(e.sessions, sid)
		e.mu.Unlock()
	}
}

// Tools is the tools/list the app serves; the bridge's offline list is generated from it
// (see TestToolsMatchBridgeSnapshot).
func Tools() []map[string]any {
	var out []map[string]any
	for _, s := range agent.Specs() {
		out = append(out, map[string]any{"name": s.Name, "title": s.Title, "description": s.Description, "inputSchema": s.Schema, "annotations": map[string]any{"readOnlyHint": s.ReadOnly, "destructiveHint": s.Gated, "idempotentHint": s.Idempotent, "openWorldHint": true}})
	}
	return out
}
