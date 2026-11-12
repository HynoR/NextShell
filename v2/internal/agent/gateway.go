// Package agent is the UI-independent policy boundary for local MCP clients.
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"nextshell.dev/v2/internal/cloudv2"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Host struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Host     string   `json:"host"`
	Port     int      `json:"port"`
	Username string   `json:"username"`
	Tags     []string `json:"tags"`
}
type Session struct {
	ID           string `json:"id"`
	ConnectionID string `json:"connectionId"`
	Name         string `json:"name"`     // the connection's name; empty for a local terminal
	Host         string `json:"host"`     // empty for a local terminal
	Encoding     string `json:"encoding"` // the host's text encoding; empty for a local terminal
	Title        string `json:"title"`
	Local        bool   `json:"local"`
	CWD          string `json:"cwd"`
	Cols         int    `json:"cols"`
	Rows         int    `json:"rows"`
	Connected    bool   `json:"connected"`
}
type Command struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name"`
	Command     string `json:"command"`
	Group       string `json:"group,omitempty"`
	WorkspaceID string `json:"workspaceId,omitempty"`
	AppendCR    *bool  `json:"appendCr,omitempty"`
}
type ExecResult struct {
	SessionID string `json:"sessionId"`
	Mode      string `json:"mode"`
	Output    string `json:"output"`
	Stderr    string `json:"stderr"`
	ExitCode  int    `json:"exitCode"`
	ActualCWD string `json:"actualCwd"`
	Truncated bool   `json:"truncated"` // output or stderr passed the size cap; the first part is kept
}
type Backend interface {
	Hosts() ([]Host, error)
	Sessions() []Session
	Commands() ([]Command, error)
	SaveCommand(Command) (Command, error)
	Read(string) (string, error)
	Exec(context.Context, string, string) (ExecResult, error)
	Control(string, string) error
	Focus(string) error
	Open(context.Context, string) (string, error)
}
type Arguments struct {
	Target         string `json:"target,omitempty"`
	Query          string `json:"query,omitempty"`
	Command        string `json:"command,omitempty"`
	Name           string `json:"name,omitempty"`
	Group          string `json:"group,omitempty"`
	ID             string `json:"id,omitempty"`
	WorkspaceID    string `json:"workspaceId,omitempty"`
	AppendCR       *bool  `json:"appendCr,omitempty"`
	Signal         string `json:"signal,omitempty"`
	RequestID      string `json:"requestId,omitempty"`
	Reason         string `json:"reason,omitempty"`
	AllowSensitive bool   `json:"allowSensitive,omitempty"`
	TimeoutSec     int    `json:"timeoutSec,omitempty"`
	Limit          int    `json:"limit,omitempty"`
	Mode           string `json:"mode,omitempty"`
	Lines          int    `json:"lines,omitempty"`
	StripANSI      *bool  `json:"stripAnsi,omitempty"`
}
type ToolError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type Result struct {
	OK    bool       `json:"ok"`
	Data  any        `json:"data,omitempty"`
	Error *ToolError `json:"error,omitempty"`
}

func fail(code string) Result {
	return Result{Error: &ToolError{Code: code, Message: map[string]string{"invalid_argument": "Invalid tool arguments", "not_found": "No active target matches", "forbidden": "Agent access halted or command blocked", "consent_required": "Sensitive file requires explicit user consent", "denied": "The user declined this request; do not retry", "unavailable": "Capability unavailable; OSC 133/foreground execution is deferred", "timeout": "Operation timed out", "internal": "The operation failed", "busy": "Session is busy"}[code]}}
}

type Pending struct {
	ID, Client, Tool, Target, Summary string
	ExpiresAt                         time.Time
	args                              Arguments
	owner                             string
	decision                          int
}
type Activity struct{ Tool, State, At string }

// UntilExit passed to SetAutoApprove keeps auto-approval on until the app exits or is halted.
const UntilExit time.Duration = -1

// AutoApproval is the observable state of the opt-in approval bypass.
type AutoApproval struct {
	On        bool
	UntilExit bool      // On without an expiry
	Expires   time.Time // zero unless On with a deadline
}
type Gateway struct {
	backend   Backend
	mu        sync.Mutex
	halted    bool
	auto      AutoApproval
	autoTimer *time.Timer
	pending   map[string]*Pending
	activity  []Activity
	blacklist []string
	running   map[string]context.CancelFunc
	busy      map[string]bool
	changed   func()
	now       func() time.Time
}

func New(b Backend, changed func()) *Gateway {
	return &Gateway{backend: b, pending: map[string]*Pending{}, running: map[string]context.CancelFunc{}, busy: map[string]bool{}, changed: changed, now: time.Now}
}
func (g *Gateway) record(tool, state string) {
	g.mu.Lock()
	g.activity = append(g.activity, Activity{Tool: tool, State: state, At: g.now().In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("15:04:05 UTC+8")})
	if len(g.activity) > 100 {
		g.activity = g.activity[len(g.activity)-100:]
	}
	g.mu.Unlock()
	if g.changed != nil {
		g.changed()
	}
}
func (g *Gateway) Activity() []Activity {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]Activity{}, g.activity...)
}
func (g *Gateway) Halted() bool { g.mu.Lock(); defer g.mu.Unlock(); return g.halted }
func (g *Gateway) Halt(h bool) {
	g.mu.Lock()
	g.halted = h
	if h {
		g.setAutoLocked(AutoApproval{}) // emergency stop: never resumes with auto-approval still on
		for _, cancel := range g.running {
			cancel()
		}
		for _, p := range g.pending {
			p.decision = -1
		}
	}
	g.mu.Unlock()
	g.record("global", map[bool]string{true: "halted", false: "resumed"}[h])
}
func (g *Gateway) setAutoLocked(a AutoApproval) {
	if g.autoTimer != nil {
		g.autoTimer.Stop()
		g.autoTimer = nil
	}
	g.auto = a
}

// SetAutoApprove skips the in-app approval prompt for mutating calls. d>0 expires after d,
// UntilExit lasts until the app exits, 0 turns it off. State is memory-only, never persisted.
// Calls that are risky (sensitive files) or blocked (dangerous, blacklisted) are never covered.
func (g *Gateway) SetAutoApprove(d time.Duration) error {
	if d < 0 && d != UntilExit {
		return errors.New("invalid auto-approve duration")
	}
	g.mu.Lock()
	if d != 0 && g.halted {
		g.mu.Unlock()
		return errors.New("agent access is halted")
	}
	switch {
	case d == 0:
		g.setAutoLocked(AutoApproval{})
	case d == UntilExit:
		g.setAutoLocked(AutoApproval{On: true, UntilExit: true})
	default:
		until := g.now().Add(d)
		g.setAutoLocked(AutoApproval{On: true, Expires: until})
		// The deadline is enforced by AutoApproval(); the timer only tells the UI to repaint.
		g.autoTimer = time.AfterFunc(d, func() {
			if !g.AutoApproval().On {
				g.record("global", "auto-approve expired")
			}
		})
	}
	g.mu.Unlock()
	g.record("global", map[bool]string{true: "auto-approve on", false: "auto-approve off"}[d != 0])
	return nil
}

// AutoApproval reports whether auto-approval is active, expiring it first if its deadline passed.
func (g *Gateway) AutoApproval() AutoApproval {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.auto.On && !g.auto.UntilExit && !g.now().Before(g.auto.Expires) {
		g.setAutoLocked(AutoApproval{})
	}
	return g.auto
}
func (g *Gateway) SetBlacklist(lines []string) error {
	for _, line := range lines {
		p := strings.TrimSpace(line)
		if strings.HasPrefix(p, "re:") {
			if _, e := regexp.Compile(strings.TrimPrefix(p, "re:")); e != nil {
				return errors.New("invalid blacklist regular expression")
			}
		}
	}
	g.mu.Lock()
	g.blacklist = append([]string{}, lines...)
	g.mu.Unlock()
	return nil
}
func (g *Gateway) Pending() []Pending {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []Pending
	for id, p := range g.pending {
		if !p.ExpiresAt.After(g.now()) {
			delete(g.pending, id)
			continue
		}
		if p.decision == 0 {
			out = append(out, *p)
		}
	}
	return out
}
func (g *Gateway) Respond(id string, approve bool) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	p := g.pending[id]
	if p == nil || p.decision != 0 || !p.ExpiresAt.After(g.now()) || g.halted {
		return false
	}
	p.decision = -1
	if approve {
		p.decision = 1
	}
	return true
}
func (g *Gateway) Call(ctx context.Context, clientID, clientName, tool string, raw json.RawMessage) (result Result) {
	defer func() {
		state := "failed"
		if result.OK {
			state = "completed"
			if p, ok := result.Data.(map[string]any); ok && p["status"] == "pending" {
				state = "pending"
			}
		}
		g.record(tool, state)
	}()
	g.mu.Lock()
	halted := g.halted
	g.mu.Unlock()
	if halted {
		return fail("forbidden")
	}
	if len(raw) > 32<<10 {
		return fail("invalid_argument")
	}
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	var a Arguments
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&a) != nil || d.Decode(new(any)) != io.EOF {
		return fail("invalid_argument")
	}
	if len(a.Command) > 8192 || len(a.Target) > 128 || len(a.Query) > 200 || len(a.Name) > 256 || len(a.Reason) > 300 || len(a.Group) > 256 {
		return fail("invalid_argument")
	}
	mutates := tool == "exec" || tool == "session_open" || tool == "session_send_signal" || tool == "session_focus" || tool == "command_save"
	approved := false
	if a.RequestID != "" {
		g.mu.Lock()
		p := g.pending[a.RequestID]
		if p == nil || p.owner != clientID || p.Tool != tool || !p.ExpiresAt.After(g.now()) {
			g.mu.Unlock()
			return fail("not_found")
		}
		if p.decision == 0 {
			g.mu.Unlock()
			return Result{OK: true, Data: map[string]any{"status": "pending", "requestId": p.ID}}
		}
		if p.decision < 0 {
			g.mu.Unlock()
			return fail("denied")
		}
		a = p.args
		delete(g.pending, p.ID)
		approved = true
		g.mu.Unlock()
	}
	var live *Session
	if a.Target != "" {
		for _, s := range g.backend.Sessions() {
			if s.ID == a.Target && s.Connected {
				copy := s
				live = &copy
				break
			}
		}
	}
	risky := false // risky calls always get a manual prompt, even under auto-approve
	switch tool {
	case "session_list":
		sessions := append([]Session{}, g.backend.Sessions()...) // [] not null for clients
		if len(sessions) > 500 {
			sessions = sessions[:500]
		}
		for i := range sessions {
			sessions[i].Title = Redact(sessions[i].Title)
			sessions[i].CWD = Redact(sessions[i].CWD)
		}
		return Result{OK: true, Data: map[string]any{"sessions": sessions}}
	case "host_list":
		hosts, e := g.backend.Hosts()
		if e != nil {
			return fail("internal")
		}
		out := []Host{}
		query := strings.ToLower(a.Query)
		for _, h := range hosts {
			if query == "" || strings.Contains(strings.ToLower(h.Name+" "+h.Host+" "+strings.Join(h.Tags, " ")), query) {
				h.Name = Redact(h.Name)
				for i := range h.Tags {
					h.Tags[i] = Redact(h.Tags[i])
				}
				out = append(out, h)
				if len(out) >= 20 {
					break
				}
			}
		}
		return Result{OK: true, Data: map[string]any{"hosts": out}}
	case "command_search":
		commands, e := g.backend.Commands()
		if e != nil {
			return fail("internal")
		}
		limit := a.Limit
		if limit <= 0 || limit > 200 {
			limit = 30
		}
		out := []Command{}
		for _, c := range commands {
			if strings.Contains(strings.ToLower(c.Name+" "+c.Command), strings.ToLower(a.Query)) {
				c.Command = Redact(c.Command)
				c.Name = Redact(c.Name)
				c.Group = Redact(c.Group)
				out = append(out, c)
				if len(out) >= limit {
					break
				}
			}
		}
		return Result{OK: true, Data: map[string]any{"matches": out}}
	case "session_history":
		if live == nil {
			return fail("not_found")
		}
		return Result{OK: true, Data: map[string]any{"sessionId": a.Target, "integrationAvailable": false, "entries": []any{}}}
	case "session_read":
		if live == nil {
			return fail("not_found")
		}
		if a.Mode != "" && a.Mode != "screen" {
			return fail("unavailable")
		}
		text, e := g.backend.Read(a.Target)
		if e != nil {
			return fail("unavailable")
		}
		text = Redact(text)
		if len(text) > 512<<10 {
			text = text[len(text)-(512<<10):]
		}
		lines := a.Lines
		if lines <= 0 || lines > 2000 {
			lines = 200
		}
		parts := strings.Split(text, "\n")
		truncated := len(parts) > lines
		if truncated {
			parts = parts[len(parts)-lines:]
		}
		return Result{OK: true, Data: map[string]any{"sessionId": a.Target, "content": strings.Join(parts, "\n"), "mode": "screen", "truncated": truncated}}
	case "exec":
		if live == nil {
			return fail("not_found")
		}
		if live.Local || (a.Mode != "" && a.Mode != "background") {
			return fail("unavailable")
		}
		if strings.TrimSpace(a.Command) == "" {
			return fail("invalid_argument")
		}
		g.mu.Lock()
		blacklist := append([]string{}, g.blacklist...)
		g.mu.Unlock()
		if Dangerous(a.Command, blacklist) {
			return fail("forbidden")
		}
		if risky = Sensitive(a.Command); risky && !a.AllowSensitive {
			return fail("consent_required")
		}
	case "session_open":
		hosts, e := g.backend.Hosts()
		if e != nil {
			return fail("internal")
		}
		found := false
		for _, h := range hosts {
			if h.ID == a.Target {
				found = true
			}
		}
		if !found {
			return fail("not_found")
		}
		for _, s := range g.backend.Sessions() {
			if s.ConnectionID == a.Target && s.Connected {
				return Result{OK: true, Data: map[string]any{"status": "opened", "sessionId": s.ID, "reused": true}}
			}
		}
	case "command_save":
		if strings.TrimSpace(a.Name) == "" || strings.TrimSpace(a.Command) == "" {
			return fail("invalid_argument")
		}
	case "session_send_signal":
		if live == nil {
			return fail("not_found")
		}
		if _, ok := controls[a.Signal]; !ok {
			return fail("invalid_argument")
		}
	case "session_focus":
		if live == nil {
			return fail("not_found")
		}
	default:
		return fail("invalid_argument")
	}
	auto := mutates && !approved && !risky && g.AutoApproval().On
	if auto {
		g.record(tool, "auto-approved")
	}
	if mutates && !approved && !auto {
		g.mu.Lock()
		for id, p := range g.pending {
			if !p.ExpiresAt.After(g.now()) {
				delete(g.pending, id)
			}
		}
		if len(g.pending) >= 100 {
			g.mu.Unlock()
			return fail("busy")
		}
		for _, p := range g.pending {
			if p.owner == clientID && p.Tool == tool && reflect.DeepEqual(p.args, a) && p.ExpiresAt.After(g.now()) {
				decision := p.decision
				g.mu.Unlock()
				if decision < 0 {
					return fail("denied")
				}
				return Result{OK: true, Data: map[string]any{"status": "pending", "requestId": p.ID}}
			}
		}
		id := cloudv2.NewID()
		summary := a.Command
		if summary == "" {
			summary = tool + " " + a.Target
		}
		g.pending[id] = &Pending{ID: id, Client: Redact(clientName), Tool: tool, Target: a.Target, Summary: Redact(summary), ExpiresAt: g.now().Add(2 * time.Minute), args: a, owner: clientID}
		g.mu.Unlock()
		if g.changed != nil {
			g.changed()
		}
		return Result{OK: true, Data: map[string]any{"status": "pending", "requestId": id}}
	}
	seconds := a.TimeoutSec
	if seconds <= 0 || seconds > 120 {
		seconds = 30
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
	defer cancel()
	runID := cloudv2.NewID()
	g.mu.Lock()
	if g.halted {
		g.mu.Unlock()
		return fail("forbidden")
	}
	if g.busy[a.Target] {
		g.mu.Unlock()
		return fail("busy")
	}
	g.busy[a.Target] = true
	g.running[runID] = cancel
	g.mu.Unlock()
	defer func() { g.mu.Lock(); delete(g.running, runID); delete(g.busy, a.Target); g.mu.Unlock() }()
	var data any
	var e error
	switch tool {
	case "exec":
		var r ExecResult
		r, e = g.backend.Exec(ctx, a.Target, a.Command)
		r.Output = Redact(r.Output)
		r.Stderr = Redact(r.Stderr)
		r.ActualCWD = Redact(r.ActualCWD)
		data = r
	case "session_open":
		var id string
		id, e = g.backend.Open(ctx, a.Target)
		data = map[string]any{"status": "opened", "sessionId": id}
	case "command_save":
		var c Command
		c, e = g.backend.SaveCommand(Command{ID: a.ID, Name: a.Name, Command: a.Command, Group: a.Group, WorkspaceID: a.WorkspaceID, AppendCR: a.AppendCR})
		c.Command = Redact(c.Command)
		c.Name = Redact(c.Name)
		c.Group = Redact(c.Group)
		data = c
	case "session_send_signal":
		e = g.backend.Control(a.Target, controls[a.Signal])
		data = map[string]any{"sessionId": a.Target, "signal": a.Signal}
	case "session_focus":
		e = g.backend.Focus(a.Target)
		data = map[string]any{"sessionId": a.Target}
	}
	if e != nil {
		if errors.Is(e, context.DeadlineExceeded) || errors.Is(e, context.Canceled) {
			return fail("timeout")
		}
		return fail("internal")
	}
	return Result{OK: true, Data: data}
}

var controls = map[string]string{"interrupt": "\x03", "eof": "\x04", "suspend": "\x1a", "quit": "\x1c"}
