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
	"strconv"
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

// Output is what a session received after a cursor (the total number of bytes received so far).
type Output struct {
	Data      string // raw terminal output after the cursor
	Cursor    int64  // total bytes received
	Lost      bool   // the cursor was older than what is kept
	Supported bool   // false for local shells, which have no output stream
	Alt       bool   // the program in the session is using the alternate screen (vim, tmux, htop)
}

// UserAction is the callback Open uses when the connection needs a decision from the user
// (reason "host_key", "password" or "otp"): call it before showing the dialog and the returned
// function once the user answered.
type UserAction func(reason string) (done func())

type Backend interface {
	Hosts() ([]Host, error)
	Sessions() []Session
	// Closed reports whether id belonged to a session that was closed recently.
	Closed(id string) bool
	Commands() ([]Command, error)
	// Workspaces lists the cloud workspace ids a command may be saved into.
	Workspaces() ([]string, error)
	SaveCommand(Command) (Command, error)
	Read(string) (string, error)
	// Output returns what the session received after cursor since; since < 0 only reports the cursor.
	Output(id string, since int64) (Output, error)
	Exec(context.Context, string, string) (ExecResult, error)
	// Control types data into the session as if the user did; ErrUserTyping while the user is typing there.
	Control(string, string) error
	Focus(string) error
	Close(string) error
	// Open connects to a saved host and returns the session id. The gateway gives it a context that
	// outlives the MCP request, so a dialog the user answers later still completes the connection.
	Open(ctx context.Context, host string, userAction UserAction) (string, error)
}
type WaitFor struct {
	Pattern    string `json:"pattern"`
	TimeoutSec int    `json:"timeoutSec,omitempty"`
}
type Arguments struct {
	Target         string   `json:"target,omitempty"`
	Query          string   `json:"query,omitempty"`
	Command        string   `json:"command,omitempty"`
	Name           string   `json:"name,omitempty"`
	Group          *string  `json:"group,omitempty"`
	ID             string   `json:"id,omitempty"`
	WorkspaceID    string   `json:"workspaceId,omitempty"`
	AppendCR       *bool    `json:"appendCr,omitempty"`
	Signal         string   `json:"signal,omitempty"`
	RequestID      string   `json:"requestId,omitempty"`
	Reason         string   `json:"reason,omitempty"`
	AllowSensitive bool     `json:"allowSensitive,omitempty"`
	TimeoutSec     int      `json:"timeoutSec,omitempty"`
	Limit          int      `json:"limit,omitempty"`
	Offset         int      `json:"offset,omitempty"`
	Mode           string   `json:"mode,omitempty"`
	Lines          int      `json:"lines,omitempty"`
	StripANSI      *bool    `json:"stripAnsi,omitempty"`
	Text           string   `json:"text,omitempty"`
	Keys           []string `json:"keys,omitempty"`
	WaitFor        *WaitFor `json:"waitFor,omitempty"`
	SinceCursor    string   `json:"sinceCursor,omitempty"`
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

var messages = map[string]string{
	"invalid_argument":     "Invalid tool arguments",
	"not_found":            "No such session, host or request",
	"forbidden":            "Agent access halted or command blocked",
	"consent_required":     "Sensitive file or environment access requires explicit user consent",
	"denied":               "The user declined this request; do not retry",
	"unavailable":          "Capability unavailable",
	"timeout":              "Operation timed out",
	"internal":             "The operation failed",
	"busy":                 "Another call is already running on this session",
	"disconnected":         "The session is disconnected or was closed",
	"auth_failed":          "Authentication failed",
	"host_key":             "The host key was not trusted or has changed",
	"refused":              "The connection was refused",
	"unreachable":          "The host is unreachable",
	"user_action_required": "The user has to act in NextShell",
	"user_typing":          "The user is typing in this tab; try again shortly",
}

// CodedError carries a stable tool error code from the backend.
type CodedError struct {
	C   string
	Err error
}

func (e *CodedError) Error() string { return e.Err.Error() }
func (e *CodedError) Unwrap() error { return e.Err }

// Coded tags err with an error code from the tool error model.
func Coded(code string, err error) error { return &CodedError{C: code, Err: err} }

// ErrUserTyping is returned by Backend.Control while the user is typing in the session.
var ErrUserTyping = Coded("user_typing", errors.New("the user pressed a key in this tab within the last 3 seconds"))

// fail answers with an error code and a short, redacted cause.
func fail(code string, cause ...string) Result {
	msg := messages[code]
	if len(cause) > 0 && cause[0] != "" {
		c := Redact(strings.Join(strings.Fields(cause[0]), " "))
		if r := []rune(c); len(r) > 200 {
			c = string(r[:200]) + "…"
		}
		msg += ": " + c
	}
	return Result{Error: &ToolError{Code: code, Message: msg}}
}

// failErr maps a backend error to its code; the cause rides along.
func failErr(e error) Result {
	var coded *CodedError
	switch {
	case errors.As(e, &coded):
		return fail(coded.C, e.Error())
	case errors.Is(e, context.DeadlineExceeded), errors.Is(e, context.Canceled):
		return fail("timeout")
	}
	return fail("internal", e.Error())
}
func ok(data any) Result { return Result{OK: true, Data: data} }

// blocked answers a command the policy never lets run; unlike a halt, it says why.
func blocked(v Verdict) Result {
	r := fail("forbidden", strings.Join(v.Why, "; "))
	r.Error.Message = strings.Replace(r.Error.Message, messages["forbidden"], "Blocked by the command policy", 1)
	return r
}

type Pending struct {
	ID, Client, Tool, Target string
	TargetName               string // what Target is called in the UI: a tab title or host name
	Summary                  string // what would run, in full and redacted
	Reason                   string // the agent's stated reason, redacted
	Tier                     string // "risky", "sensitive" or "" (see Verdict)
	Why                      string // why the policy wants a manual decision
	CreatedAt, ExpiresAt     time.Time
	args                     Arguments
	owner                    string
	decision                 int
}
type Activity struct{ Tool, State, At string }

// Notice tells the app that an agent call is changing something (Pending: it is waiting for a
// decision), so it can show its one-time takeover notice and the "agent active" indicator.
type Notice struct {
	Tool    string
	Pending bool
}

// UntilExit passed to SetAutoApprove keeps auto-approval on until the app exits or is halted.
const UntilExit time.Duration = -1

// AutoApproval is the observable state of the opt-in approval bypass.
type AutoApproval struct {
	On        bool
	UntilExit bool      // On without an expiry
	Expires   time.Time // zero unless On with a deadline
}

// openOp is one connection attempt for a host; callers that ask for the same host while it runs share it.
type openOp struct {
	changed  chan struct{} // closed and replaced whenever waiting or finished changes
	waiting  string        // reason while the user has a dialog to answer
	finished bool
	id       string
	err      error
}

func (op *openOp) bump() { close(op.changed); op.changed = make(chan struct{}) }

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
	busy      map[string]bool // exec only: one at a time per session
	opens     map[string]*openOp
	typed     map[string]typedLine
	changed   func()
	notify    func(Notice)
	now       func() time.Time
}

func New(b Backend, changed func()) *Gateway {
	return &Gateway{backend: b, pending: map[string]*Pending{}, running: map[string]context.CancelFunc{}, busy: map[string]bool{}, opens: map[string]*openOp{}, typed: map[string]typedLine{}, changed: changed, now: time.Now}
}

// SetNotify registers the observer of mutating calls (see Notice).
func (g *Gateway) SetNotify(f func(Notice)) { g.mu.Lock(); g.notify = f; g.mu.Unlock() }
func (g *Gateway) tell(n Notice) {
	g.mu.Lock()
	f := g.notify
	g.mu.Unlock()
	if f != nil {
		f(n)
	}
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
// Calls that are risky (tier 2, sensitive files) or forbidden (tier 1) are never covered.
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

// HasPending reports whether owner (an MCP session) has requests waiting for a decision.
func (g *Gateway) HasPending(owner string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, p := range g.pending {
		if p.owner == owner && p.decision == 0 && p.ExpiresAt.After(g.now()) {
			return true
		}
	}
	return false
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

// resolve finds the session target names. live is set for a connected one; code is the error
// to answer otherwise ("disconnected" for a dead or recently closed session, "not_found" for an unknown id).
func (g *Gateway) resolve(target string) (live, listed *Session, code string) {
	for _, s := range g.backend.Sessions() {
		if s.ID == target {
			c := s
			if s.Connected {
				return &c, &c, ""
			}
			return nil, &c, "disconnected"
		}
	}
	if g.backend.Closed(target) {
		return nil, nil, "disconnected"
	}
	return nil, nil, "not_found"
}

func clamp(v, lo, hi, def int) int {
	if v <= 0 {
		return def
	}
	return min(max(v, lo), hi)
}
func tail(text string, lines int) (string, bool) {
	if len(text) > 512<<10 {
		text = text[len(text)-(512<<10):]
	}
	parts := strings.Split(text, "\n")
	if len(parts) > lines {
		return strings.Join(parts[len(parts)-lines:], "\n"), true
	}
	return text, false
}

// targeted tools name a session in `target`.
var targeted = map[string]bool{"session_read": true, "session_history": true, "exec": true, "session_send_text": true, "session_send_signal": true, "session_focus": true, "session_close": true}

func (g *Gateway) Call(ctx context.Context, clientID, clientName, tool string, raw json.RawMessage) (result Result) {
	defer func() {
		state := "failed"
		if result.OK {
			state = "completed"
			if p, ok := result.Data.(map[string]any); ok && (p["status"] == "pending" || p["status"] == "user_action_required") {
				state = p["status"].(string)
			}
		}
		g.record(tool, state)
	}()
	sp := spec(tool)
	g.mu.Lock()
	halted := g.halted
	g.mu.Unlock()
	if halted && tool != "agent_status" {
		return fail("forbidden")
	}
	if sp == nil || len(raw) > 32<<10 {
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
	if len(a.Command) > 8192 || len(a.Target) > 128 || len(a.Query) > 200 || len(a.Name) > 256 || len(a.Reason) > 300 || (a.Group != nil && len(*a.Group) > 256) || len(a.Text) > 8192 || len(a.Keys) > 64 || len(a.ID) > 128 || len(a.WorkspaceID) > 128 || len(a.RequestID) > 128 || a.Offset < 0 {
		return fail("invalid_argument")
	}
	switch tool {
	case "agent_status":
		return g.status(clientID)
	case "request_cancel":
		return g.cancel(clientID, a.RequestID)
	}
	approved := false
	if a.RequestID != "" {
		g.mu.Lock()
		p := g.pending[a.RequestID]
		if p == nil || p.owner != clientID || p.Tool != tool || !p.ExpiresAt.After(g.now()) {
			g.mu.Unlock()
			return fail("not_found", "no pending request of yours with that id")
		}
		if p.decision == 0 {
			res := ok(g.pendingData(p))
			g.mu.Unlock()
			return res
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
	var live, listed *Session
	if targeted[tool] || tool == "session_open" {
		if a.Target == "" {
			return fail("invalid_argument", "target is required")
		}
	}
	if targeted[tool] {
		var code string
		live, listed, code = g.resolve(a.Target)
		if live == nil && tool != "session_close" {
			return fail(code)
		}
		if tool == "session_close" && listed == nil {
			if code == "disconnected" {
				return ok(map[string]any{"sessionId": a.Target, "alreadyClosed": true})
			}
			return fail("not_found")
		}
	}
	var verdict Verdict
	var typedNext typedLine
	var data string // bytes for session_send_text
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
		return ok(map[string]any{"sessions": sessions})
	case "host_list":
		hosts, e := g.backend.Hosts()
		if e != nil {
			return failErr(e)
		}
		query := strings.ToLower(a.Query)
		out := []Host{}
		total, limit := 0, clamp(a.Limit, 1, 200, 50)
		for _, h := range hosts {
			// Match what the agent may see: redacted text only, so a query cannot probe a secret.
			h.Name, h.Username = Redact(h.Name), Redact(h.Username)
			tags := make([]string, len(h.Tags))
			for i, t := range h.Tags {
				tags[i] = Redact(t)
			}
			h.Tags = tags
			if query != "" && !strings.Contains(strings.ToLower(h.Name+" "+h.Host+" "+strings.Join(h.Tags, " ")), query) {
				continue
			}
			total++
			if total > a.Offset && len(out) < limit {
				out = append(out, h)
			}
		}
		return ok(map[string]any{"hosts": out, "total": total, "offset": a.Offset, "limit": limit})
	case "command_search":
		commands, e := g.backend.Commands()
		if e != nil {
			return failErr(e)
		}
		query := strings.ToLower(a.Query)
		out := []Command{}
		total, limit := 0, clamp(a.Limit, 1, 200, 50)
		for _, c := range commands {
			c.Command, c.Name, c.Group = Redact(c.Command), Redact(c.Name), Redact(c.Group)
			if query != "" && !strings.Contains(strings.ToLower(c.Name+" "+c.Command), query) {
				continue
			}
			total++
			if total > a.Offset && len(out) < limit {
				out = append(out, c)
			}
		}
		return ok(map[string]any{"matches": out, "total": total, "offset": a.Offset, "limit": limit})
	case "session_history":
		return ok(map[string]any{"sessionId": a.Target, "integrationAvailable": false, "entries": []any{}})
	case "session_read":
		return g.read(ctx, a)
	case "exec":
		if live.Local || (a.Mode != "" && a.Mode != "background") {
			return fail("unavailable", "exec needs a remote SSH session; local shells are not supported")
		}
		if strings.TrimSpace(a.Command) == "" {
			return fail("invalid_argument", "command is empty")
		}
		verdict = Classify(a.Command, g.blacklistCopy())
		if verdict.Forbidden {
			return blocked(verdict)
		}
		if verdict.Sensitive && !a.AllowSensitive {
			return fail("consent_required", strings.Join(verdict.Why, "; "))
		}
	case "session_send_text":
		var e error
		if data, e = Keystrokes(a.Text, a.Keys); e != nil {
			return fail("invalid_argument", e.Error())
		}
		g.mu.Lock()
		state := g.typed[a.Target]
		g.mu.Unlock()
		verdict, typedNext = scanTyping(state, data, g.blacklistCopy(), g.altScreen(a.Target))
		if verdict.Forbidden {
			return blocked(verdict)
		}
		if verdict.Sensitive && !a.AllowSensitive {
			return fail("consent_required", strings.Join(verdict.Why, "; "))
		}
	case "session_open":
		hosts, e := g.backend.Hosts()
		if e != nil {
			return failErr(e)
		}
		found := false
		for _, h := range hosts {
			found = found || h.ID == a.Target
		}
		if !found {
			return fail("not_found", "no saved host with that id")
		}
		for _, s := range g.backend.Sessions() {
			if s.ConnectionID == a.Target && s.Connected {
				return ok(map[string]any{"status": "opened", "sessionId": s.ID, "reused": true})
			}
		}
	case "command_save":
		if a.ID == "" && (strings.TrimSpace(a.Name) == "" || strings.TrimSpace(a.Command) == "") {
			return fail("invalid_argument", "name and command are required for a new command")
		}
		if a.ID != "" && a.Name == "" && a.Command == "" && a.Group == nil && a.AppendCR == nil {
			return fail("invalid_argument", "nothing to change")
		}
		if _, e := g.mergeCommand(a); e != nil {
			return failErr(e)
		}
	case "session_send_signal":
		if _, ok := controls[a.Signal]; !ok {
			return fail("invalid_argument", "signal must be interrupt, eof, suspend or quit")
		}
	case "session_focus", "session_close":
	default:
		return fail("invalid_argument")
	}
	auto := sp.Gated && !approved && !verdict.Manual() && g.AutoApproval().On
	if auto {
		g.record(tool, "auto-approved")
	}
	if sp.Gated && !approved && !auto {
		g.mu.Lock()
		for id, p := range g.pending {
			if !p.ExpiresAt.After(g.now()) {
				delete(g.pending, id)
			}
		}
		if len(g.pending) >= 100 {
			g.mu.Unlock()
			return fail("busy", "too many pending approvals")
		}
		for _, p := range g.pending {
			if p.owner == clientID && p.Tool == tool && reflect.DeepEqual(p.args, a) && p.ExpiresAt.After(g.now()) {
				if p.decision < 0 {
					g.mu.Unlock()
					return fail("denied")
				}
				if p.decision == 0 {
					res := ok(g.pendingData(p))
					g.mu.Unlock()
					return res
				}
				delete(g.pending, p.ID) // approved earlier: repeating the same call runs it
				approved = true
				break
			}
		}
		if !approved {
			id := cloudv2.NewID()
			targetName, summary := g.describe(tool, a, data)
			p := &Pending{ID: id, Client: Redact(clientName), Tool: tool, Target: a.Target, TargetName: targetName, Summary: summary, Reason: Redact(a.Reason), Tier: verdict.Tier(), Why: strings.Join(verdict.Why, "; "), CreatedAt: g.now(), ExpiresAt: g.now().Add(2 * time.Minute), args: a, owner: clientID}
			g.pending[id] = p
			res := ok(g.pendingData(p))
			g.mu.Unlock()
			if g.changed != nil {
				g.changed()
			}
			g.tell(Notice{Tool: tool, Pending: true})
			return res
		}
		g.mu.Unlock()
	}
	if sp.Gated {
		g.tell(Notice{Tool: tool})
	}
	return g.run(ctx, tool, a, data, typedNext)
}

// altScreen reports whether the session shows a full-screen program.
func (g *Gateway) altScreen(id string) bool {
	o, _ := g.backend.Output(id, -1)
	return o.Alt
}

func (g *Gateway) blacklistCopy() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string{}, g.blacklist...)
}

// pendingData is the reply that tells an agent a request waits for the user. g.mu is held.
func (g *Gateway) pendingData(p *Pending) map[string]any {
	d := map[string]any{"status": "pending", "requestId": p.ID, "expiresInSec": max(0, int(p.ExpiresAt.Sub(g.now()).Seconds()))}
	if p.Tier != "" {
		d["tier"] = p.Tier
		d["why"] = p.Why
		d["message"] = "This needs a manual approval in NextShell, even when auto-approve is on."
	}
	return d
}

// describe returns the human name of a call's target and a full, redacted summary for the approval prompt.
func (g *Gateway) describe(tool string, a Arguments, typed string) (targetName, summary string) {
	if tool == "session_open" {
		hosts, _ := g.backend.Hosts()
		for _, h := range hosts {
			if h.ID == a.Target {
				targetName = Redact(h.Name)
			}
		}
	} else {
		for _, s := range g.backend.Sessions() {
			if s.ID == a.Target {
				targetName = Redact(s.Title)
			}
		}
	}
	if targetName == "" {
		targetName = a.Target
	}
	switch tool {
	case "exec":
		summary = a.Command
	case "session_send_text":
		summary = strconv.QuoteToGraphic(typed)
	case "command_save":
		summary = a.Name + ": " + a.Command
		if a.ID != "" {
			summary = "update " + a.ID + " " + summary
		}
	case "session_send_signal":
		summary = "send " + a.Signal
	default:
		summary = tool + " " + targetName
	}
	return targetName, Redact(summary)
}

// mergeCommand validates a command_save and, for an update, fills the fields the caller left out
// from the stored command.
func (g *Gateway) mergeCommand(a Arguments) (Command, error) {
	c := Command{ID: a.ID, Name: a.Name, Command: a.Command, WorkspaceID: a.WorkspaceID, AppendCR: a.AppendCR}
	if a.Group != nil {
		c.Group = *a.Group
	}
	if a.WorkspaceID != "" {
		known, e := g.backend.Workspaces()
		if e != nil {
			return c, e
		}
		found := false
		for _, w := range known {
			found = found || w == a.WorkspaceID
		}
		if !found {
			return c, Coded("not_found", errors.New("unknown workspaceId"))
		}
	}
	if a.ID == "" {
		return c, nil
	}
	existing, e := g.backend.Commands()
	if e != nil {
		return c, e
	}
	for _, old := range existing {
		if old.ID != a.ID {
			continue
		}
		if a.WorkspaceID != "" && a.WorkspaceID != old.WorkspaceID {
			return c, Coded("invalid_argument", errors.New("a command cannot move to another workspace"))
		}
		if a.Name == "" {
			c.Name = old.Name
		}
		if a.Command == "" {
			c.Command = old.Command
		}
		if a.Group == nil {
			c.Group = old.Group
		}
		if a.AppendCR == nil {
			c.AppendCR = old.AppendCR
		}
		c.WorkspaceID = old.WorkspaceID
		return c, nil
	}
	return c, Coded("not_found", errors.New("no command with that id"))
}

func (g *Gateway) status(owner string) Result {
	auto := g.AutoApproval()
	state := map[string]any{"on": auto.On, "untilExit": auto.UntilExit}
	if !auto.Expires.IsZero() {
		state["expiresAt"] = auto.Expires.UTC().Format(time.RFC3339)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	pending := []map[string]any{}
	for _, p := range g.pending {
		if p.owner == owner && p.ExpiresAt.After(g.now()) && p.decision == 0 {
			summary := []rune(p.Summary)
			if len(summary) > 200 {
				summary = append(summary[:200], '…')
			}
			e := map[string]any{"requestId": p.ID, "tool": p.Tool, "summary": string(summary), "ageSec": int(g.now().Sub(p.CreatedAt).Seconds())}
			if p.Tier != "" {
				e["tier"] = p.Tier
			}
			pending = append(pending, e)
		}
	}
	return ok(map[string]any{"enabled": true, "halted": g.halted, "autoApprove": state, "pending": pending})
}
func (g *Gateway) cancel(owner, id string) Result {
	g.mu.Lock()
	p := g.pending[id]
	if p == nil || p.owner != owner {
		g.mu.Unlock()
		return fail("not_found", "no pending request of yours with that id")
	}
	delete(g.pending, id)
	g.mu.Unlock()
	if g.changed != nil {
		g.changed()
	}
	return ok(map[string]any{"requestId": id, "cancelled": true})
}

const readPoll = 150 * time.Millisecond

func (g *Gateway) read(ctx context.Context, a Arguments) Result {
	lines := clamp(a.Lines, 1, 2000, 200)
	if a.Mode != "" && a.Mode != "screen" {
		return fail("unavailable", "only mode screen exists")
	}
	var re *regexp.Regexp
	var plain string
	timeout := 30 * time.Second
	wait := a.WaitFor != nil
	if wait {
		p := a.WaitFor.Pattern
		if p == "" || len(p) > 200 {
			return fail("invalid_argument", "waitFor.pattern must be 1-200 characters")
		}
		if expr, isRe := strings.CutPrefix(p, "re:"); isRe {
			var e error
			if re, e = regexp.Compile("(?m)" + expr); e != nil {
				return fail("invalid_argument", e.Error())
			}
		} else {
			plain = p
		}
		if t := a.WaitFor.TimeoutSec; t != 0 {
			if t < 1 || t > 60 {
				return fail("invalid_argument", "waitFor.timeoutSec must be 1-60")
			}
			timeout = time.Duration(t) * time.Second
		}
	}
	matches := func(text string) bool {
		if re != nil {
			return re.MatchString(text)
		}
		return strings.Contains(text, plain)
	}
	since := int64(-1)
	if a.SinceCursor != "" {
		n, e := strconv.ParseInt(a.SinceCursor, 10, 64)
		if e != nil || n < 0 {
			return fail("invalid_argument", "sinceCursor must be a cursor returned by an earlier read")
		}
		since = n
	}
	probe, e := g.backend.Output(a.Target, -1)
	if e != nil {
		return failErr(e)
	}
	deadline := time.Now().Add(timeout)
	pause := func() *Result {
		select {
		case <-ctx.Done():
			r := fail("timeout")
			return &r
		case <-time.After(readPoll):
		}
		if g.Halted() {
			r := fail("forbidden")
			return &r
		}
		return nil
	}
	if (since < 0 && !wait) || !probe.Supported {
		if since >= 0 {
			return fail("unavailable", "local shells have no output stream; omit sinceCursor")
		}
		for {
			text, e := g.backend.Read(a.Target)
			if e != nil {
				return failErr(e)
			}
			text = Redact(text)
			matched := wait && matches(text)
			timedOut := wait && !matched && !time.Now().Before(deadline)
			if !wait || matched || timedOut {
				content, truncated := tail(text, lines)
				data := map[string]any{"sessionId": a.Target, "content": content, "mode": "screen", "truncated": truncated, "cursor": strconv.FormatInt(probe.Cursor, 10)}
				if wait {
					data["matched"], data["timedOut"] = matched, timedOut
				}
				return ok(data)
			}
			if r := pause(); r != nil {
				return *r
			}
		}
	}
	if since < 0 {
		since = probe.Cursor // wait for output that arrives from now on
	}
	for {
		o, e := g.backend.Output(a.Target, since)
		if e != nil {
			return failErr(e)
		}
		clean := Redact(CleanOutput(o.Data))
		matched := wait && matches(clean)
		timedOut := wait && !matched && !time.Now().Before(deadline)
		if !wait || matched || timedOut {
			content, truncated := tail(clean, lines)
			data := map[string]any{"sessionId": a.Target, "content": content, "mode": "output", "truncated": truncated, "lost": o.Lost, "cursor": strconv.FormatInt(o.Cursor, 10)}
			if wait {
				data["matched"], data["timedOut"] = matched, timedOut
			}
			return ok(data)
		}
		if r := pause(); r != nil {
			return *r
		}
	}
}

// run executes an approved (or auto-approved) call.
func (g *Gateway) run(ctx context.Context, tool string, a Arguments, data string, _ typedLine) Result {
	seconds := a.TimeoutSec
	if seconds <= 0 || seconds > 120 {
		seconds = 30
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
	defer cancel()
	if g.Halted() {
		return fail("forbidden")
	}
	switch tool {
	case "session_open":
		return g.open(ctx, a.Target)
	case "exec":
		runID := cloudv2.NewID()
		g.mu.Lock()
		if g.busy[a.Target] {
			g.mu.Unlock()
			return fail("busy", "another exec is running on this session")
		}
		g.busy[a.Target] = true
		g.running[runID] = cancel
		g.mu.Unlock()
		defer func() { g.mu.Lock(); delete(g.running, runID); delete(g.busy, a.Target); g.mu.Unlock() }()
		r, e := g.backend.Exec(ctx, a.Target, a.Command)
		if e != nil {
			return failErr(e)
		}
		r.Output, r.Stderr, r.ActualCWD = Redact(r.Output), Redact(r.Stderr), Redact(r.ActualCWD)
		return ok(r)
	case "command_save":
		merged, e := g.mergeCommand(a)
		if e != nil {
			return failErr(e)
		}
		c, e := g.backend.SaveCommand(merged)
		if e != nil {
			return failErr(e)
		}
		c.Command, c.Name, c.Group = Redact(c.Command), Redact(c.Name), Redact(c.Group)
		return ok(c)
	case "session_send_signal":
		if e := g.backend.Control(a.Target, controls[a.Signal]); e != nil {
			return failErr(e)
		}
		// Ctrl-C throws away the half-typed line the tracker still holds.
		if a.Signal == "interrupt" {
			g.mu.Lock()
			g.typed[a.Target] = typedLine{alt: g.typed[a.Target].alt}
			g.mu.Unlock()
		}
		return ok(map[string]any{"sessionId": a.Target, "signal": a.Signal})
	case "session_send_text":
		g.mu.Lock()
		state := g.typed[a.Target]
		g.mu.Unlock()
		// The line may have changed since the request was made: judge it again before typing.
		before, _ := g.backend.Output(a.Target, -1)
		v, next := scanTyping(state, data, g.blacklistCopy(), before.Alt)
		if v.Forbidden {
			return blocked(v)
		}
		if e := g.backend.Control(a.Target, data); e != nil {
			return failErr(e)
		}
		g.mu.Lock()
		if len(g.typed) > 256 {
			g.typed = map[string]typedLine{}
		}
		g.typed[a.Target] = next
		g.mu.Unlock()
		return ok(map[string]any{"sessionId": a.Target, "sent": len(data), "cursor": strconv.FormatInt(before.Cursor, 10)})
	case "session_focus":
		if e := g.backend.Focus(a.Target); e != nil {
			return failErr(e)
		}
		return ok(map[string]any{"sessionId": a.Target})
	case "session_close":
		if e := g.backend.Close(a.Target); e != nil {
			return failErr(e)
		}
		g.mu.Lock()
		delete(g.typed, a.Target)
		g.mu.Unlock()
		return ok(map[string]any{"sessionId": a.Target, "closed": true})
	}
	return fail("invalid_argument")
}

// open joins the connection attempt for host, starting it if none runs. The attempt belongs to the
// gateway, not to the caller: it goes on while the user answers a dialog, and a later call for the
// same host picks it up. A caller that would have to wait for the user is answered at once.
func (g *Gateway) open(ctx context.Context, host string) Result {
	g.mu.Lock()
	op := g.opens[host]
	if op == nil {
		op = &openOp{changed: make(chan struct{})}
		g.opens[host] = op
		opCtx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
		runID := cloudv2.NewID()
		g.running[runID] = cancel
		go func() {
			id, e := g.backend.Open(opCtx, host, func(reason string) func() {
				g.mu.Lock()
				op.waiting = reason
				op.bump()
				g.mu.Unlock()
				if g.changed != nil {
					g.changed()
				}
				return func() {
					g.mu.Lock()
					if op.waiting == reason {
						op.waiting = ""
						op.bump()
					}
					g.mu.Unlock()
				}
			})
			cancel()
			g.mu.Lock()
			op.id, op.err, op.finished, op.waiting = id, e, true, ""
			op.bump()
			delete(g.running, runID)
			g.mu.Unlock()
			// Nobody may be waiting any more: forget the outcome after a while.
			time.AfterFunc(2*time.Minute, func() {
				g.mu.Lock()
				if g.opens[host] == op {
					delete(g.opens, host)
				}
				g.mu.Unlock()
			})
		}()
	}
	g.mu.Unlock()
	for {
		g.mu.Lock()
		waiting, finished, changed := op.waiting, op.finished, op.changed
		id, err := op.id, op.err
		if finished && g.opens[host] == op {
			delete(g.opens, host) // the first caller to see the outcome consumes it
		}
		g.mu.Unlock()
		switch {
		case finished && err != nil:
			return failErr(err)
		case finished:
			return ok(map[string]any{"status": "opened", "sessionId": id})
		case waiting != "":
			return ok(map[string]any{"status": "user_action_required", "reason": waiting, "message": userActionMessage(waiting)})
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return fail("timeout", "still connecting; call session_open again with the same target to keep waiting")
		}
	}
}

func userActionMessage(reason string) string {
	what := map[string]string{"host_key": "confirm the server's host key", "password": "enter the password", "otp": "enter the one-time code"}[reason]
	if what == "" {
		what = "answer a prompt"
	}
	return "The user has to " + what + " in the NextShell window. Tell them, then call session_open again with the same target; it joins the connection in progress."
}

var controls = map[string]string{"interrupt": "\x03", "eof": "\x04", "suspend": "\x1a", "quit": "\x1c"}
