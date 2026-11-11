// Package mcpendpoint implements the bounded JSON-response subset of MCP Streamable HTTP.
// It binds loopback, requires a bearer token on EVERY request, and never enables SSE.
package mcpendpoint

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"nextshell.dev/v2/internal/agent"
	"nextshell.dev/v2/internal/cloudv2"
	"strings"
	"sync"
	"time"
)

const ProtocolVersion = "2025-06-18"

type client struct {
	Name string
	Last time.Time
}
type Endpoint struct {
	mu       sync.Mutex
	verifier [32]byte
	sessions map[string]client
	gateway  *agent.Gateway
	server   *http.Server
	listener net.Listener
	now      func() time.Time
}

func hashToken(token string) ([32]byte, error) {
	raw, e := base64.RawURLEncoding.Strict().DecodeString(token)
	if e != nil || len(raw) != 32 {
		return [32]byte{}, errors.New("MCP token must be random 32-byte base64url")
	}
	return sha256.Sum256(append([]byte("nextshell/mcp/v2/auth\x00"), raw...)), nil
}
func New(token string, gateway *agent.Gateway) (*Endpoint, error) {
	h, e := hashToken(token)
	if e != nil {
		return nil, e
	}
	return &Endpoint{verifier: h, sessions: map[string]client{}, gateway: gateway, now: time.Now}, nil
}
func (e *Endpoint) Rotate(token string) error {
	h, err := hashToken(token)
	if err != nil {
		return err
	}
	e.mu.Lock()
	e.verifier = h
	e.sessions = map[string]client{}
	e.mu.Unlock()
	e.gateway.Halt(true)
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
	e.mu.Lock()
	server := e.server
	e.server = nil
	e.sessions = map[string]client{}
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

type rpc struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func write(w http.ResponseWriter, id json.RawMessage, result any) {
	json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}
func rpcError(w http.ResponseWriter, status, code int, id json.RawMessage, message string) {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}})
}
func (e *Endpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	host, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		host = r.Host
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	remote, _, remoteErr := net.SplitHostPort(r.RemoteAddr)
	if ip == nil || !ip.IsLoopback() || remoteErr != nil || net.ParseIP(remote) == nil || !net.ParseIP(remote).IsLoopback() || r.Header.Get("Origin") != "" {
		rpcError(w, 403, -32000, nil, "Loopback non-browser clients only")
		return
	}
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	h, err := hashToken(token)
	e.mu.Lock()
	allowed := err == nil && strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") && subtle.ConstantTimeCompare(h[:], e.verifier[:]) == 1
	e.mu.Unlock()
	if !allowed {
		w.Header().Set("WWW-Authenticate", "Bearer")
		rpcError(w, 401, -32000, nil, "Unauthorized")
		return
	}
	if r.URL.Path != "/mcp" || r.URL.RawQuery != "" {
		rpcError(w, 404, -32601, nil, "Unknown endpoint")
		return
	}
	sid := r.Header.Get("Mcp-Session-Id")
	e.mu.Lock()
	for id, c := range e.sessions {
		if e.now().Sub(c.Last) > 10*time.Minute {
			delete(e.sessions, id)
		}
	}
	identity, known := e.sessions[sid]
	if known {
		identity.Last = e.now()
		e.sessions[sid] = identity
	}
	e.mu.Unlock()
	if r.Method == "DELETE" {
		if !known {
			rpcError(w, 404, -32000, nil, "Unknown session")
			return
		}
		e.mu.Lock()
		delete(e.sessions, sid)
		e.mu.Unlock()
		w.WriteHeader(204)
		return
	}
	if r.Method != "POST" {
		w.Header().Set("Allow", "POST, DELETE")
		rpcError(w, 405, -32600, nil, "SSE not supported; use JSON POST")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, (64<<10)+1))
	if err != nil || len(raw) > 64<<10 {
		rpcError(w, 413, -32600, nil, "Request too large")
		return
	}
	var request rpc
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if d.Decode(&request) != nil || d.Decode(new(any)) != io.EOF || request.JSONRPC != "2.0" {
		rpcError(w, 400, -32700, nil, "Invalid JSON-RPC")
		return
	}
	if len(request.ID) > 0 && string(request.ID) != "null" {
		var scalar any
		if json.Unmarshal(request.ID, &scalar) != nil {
			rpcError(w, 400, -32600, nil, "Invalid request id")
			return
		}
		switch scalar.(type) {
		case string, float64:
		default:
			rpcError(w, 400, -32600, nil, "Invalid request id")
			return
		}
	}
	if request.Method == "initialize" {
		if len(request.ID) == 0 {
			rpcError(w, 400, -32600, nil, "initialize needs id")
			return
		}
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
			ClientInfo      struct {
				Name    string `json:"name"`
				Version string `json:"version"`
			} `json:"clientInfo"`
			Capabilities map[string]any `json:"capabilities"`
		}
		if json.Unmarshal(request.Params, &params) != nil || len(params.ClientInfo.Name) > 128 {
			rpcError(w, 400, -32602, request.ID, "Invalid initialize")
			return
		}
		e.mu.Lock()
		if len(e.sessions) >= 32 {
			e.mu.Unlock()
			rpcError(w, 503, -32000, request.ID, "Session capacity")
			return
		}
		id := cloudv2.NewID()
		e.sessions[id] = client{Name: params.ClientInfo.Name, Last: e.now()}
		e.mu.Unlock()
		w.Header().Set("Mcp-Session-Id", id)
		write(w, request.ID, map[string]any{"protocolVersion": ProtocolVersion, "capabilities": map[string]any{"tools": map[string]any{"listChanged": false}}, "serverInfo": map[string]any{"name": "NextShell-v2", "version": "0.3.0"}})
		return
	}
	if !known {
		rpcError(w, 404, -32000, request.ID, "Initialize first")
		return
	}
	if protocol := r.Header.Get("Mcp-Protocol-Version"); protocol != "" && protocol != ProtocolVersion && protocol != "2025-03-26" {
		rpcError(w, 400, -32600, request.ID, "Unsupported protocol version")
		return
	}
	if request.Method == "notifications/initialized" && len(request.ID) == 0 {
		w.WriteHeader(202)
		return
	}
	if len(request.ID) == 0 {
		rpcError(w, 400, -32600, nil, "Unsupported notification")
		return
	}
	switch request.Method {
	case "ping":
		write(w, request.ID, map[string]any{})
	case "tools/list":
		write(w, request.ID, map[string]any{"tools": Tools()})
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
			Meta      json.RawMessage `json:"_meta,omitempty"`
		}
		decoder := json.NewDecoder(strings.NewReader(string(request.Params)))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&p) != nil {
			rpcError(w, 400, -32602, request.ID, "Invalid tool call")
			return
		}
		result := e.gateway.Call(r.Context(), sid, identity.Name, p.Name, p.Arguments)
		text, _ := json.Marshal(result)
		write(w, request.ID, map[string]any{"content": []any{map[string]any{"type": "text", "text": string(text)}}, "structuredContent": result, "isError": !result.OK})
	default:
		rpcError(w, 200, -32601, request.ID, "Method not found")
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
