package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// rich is a scriptable backend for the behaviours the plain fake cannot express.
type rich struct {
	mu       sync.Mutex
	calls    []string
	sessions []Session
	hosts    []Host
	commands []Command
	out      []byte // everything the session "received"
	execGate chan struct{}
	execIn   chan struct{}
	open     func(ctx context.Context, host string, ua UserAction) (string, error)
	opens    atomic.Int32
	typing   bool
	alt      atomic.Bool // the session shows a full-screen program
	gated    atomic.Bool
}

func newRich() *rich {
	return &rich{sessions: []Session{{ID: "s1", ConnectionID: "h1", Title: "web-1", Connected: true}, {ID: "dead", Title: "old", Connected: false}, {ID: "local", Local: true, Connected: true, Title: "本地终端"}},
		hosts: []Host{{ID: "h1", Name: "web-1", Host: "10.0.0.1"}, {ID: "h2", Name: "db password=HIDDENx", Host: "10.0.0.2", Tags: []string{"prod"}}}}
}
func (r *rich) mark(s string) { r.mu.Lock(); r.calls = append(r.calls, s); r.mu.Unlock() }
func (r *rich) Calls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string{}, r.calls...)
}
func (r *rich) Hosts() ([]Host, error) { return r.hosts, nil }
func (r *rich) Sessions() []Session    { return r.sessions }
func (r *rich) Closed(id string) bool  { return id == "gone" }
func (r *rich) Commands() ([]Command, error) {
	return append([]Command{}, r.commands...), nil
}
func (r *rich) Workspaces() ([]string, error) { return []string{"ws1"}, nil }
func (r *rich) SaveCommand(c Command) (Command, error) {
	r.mark(fmt.Sprintf("save:%s|%s|%s|%s", c.ID, c.Name, c.Command, c.Group))
	return c, nil
}
func (r *rich) Read(string) (string, error) { return "screen", nil }
func (r *rich) Output(id string, since int64) (Output, error) {
	if id == "local" {
		return Output{}, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	o := Output{Supported: true, Cursor: int64(len(r.out)), Alt: r.alt.Load()}
	if since >= 0 && since <= o.Cursor {
		o.Data = string(r.out[since:])
	}
	return o, nil
}
func (r *rich) feed(s string) { r.mu.Lock(); r.out = append(r.out, s...); r.mu.Unlock() }
func (r *rich) Exec(ctx context.Context, id, command string) (ExecResult, error) {
	r.mark("exec:" + command)
	if r.execGate != nil && r.gated.CompareAndSwap(false, true) {
		close(r.execIn)
		select {
		case <-r.execGate:
		case <-ctx.Done():
			return ExecResult{}, ctx.Err()
		}
	}
	return ExecResult{Output: "ok"}, nil
}
func (r *rich) Control(id, data string) error {
	if r.typing {
		return ErrUserTyping
	}
	r.mark("control:" + strconv.Quote(data))
	return nil
}
func (r *rich) Focus(id string) error { r.mark("focus:" + id); return nil }
func (r *rich) Close(id string) error { r.mark("close:" + id); return nil }
func (r *rich) Open(ctx context.Context, host string, ua UserAction) (string, error) {
	r.opens.Add(1)
	r.mark("open:" + host)
	if r.open != nil {
		return r.open(ctx, host, ua)
	}
	return "s1", nil
}

func call(g *Gateway, owner, tool string, args any) Result {
	raw, _ := json.Marshal(args)
	return g.Call(context.Background(), owner, "test client", tool, raw)
}
func data(t *testing.T, r Result) map[string]any {
	t.Helper()
	if !r.OK {
		t.Fatal(r.Error)
	}
	d, _ := r.Data.(map[string]any) // exec and command_save answer with structs
	return d
}
func code(t *testing.T, r Result) string {
	t.Helper()
	if r.OK {
		t.Fatalf("expected an error, got %+v", r.Data)
	}
	return r.Error.Code
}

func execOut(r Result) string {
	if e, ok := r.Data.(ExecResult); ok {
		return e.Output
	}
	return fmt.Sprint(r.Error, r.Data)
}

type m = map[string]any

func TestOnlyExecIsSerialisedPerSession(t *testing.T) {
	b := newRich()
	b.execGate, b.execIn = make(chan struct{}), make(chan struct{})
	g := New(b, nil)
	g.SetAutoApprove(UntilExit)
	done := make(chan Result, 1)
	go func() { done <- call(g, "o", "exec", m{"target": "s1", "command": "sleep 5"}) }()
	<-b.execIn
	if c := code(t, call(g, "o", "exec", m{"target": "s1", "command": "uptime"})); c != "busy" {
		t.Fatal("second exec on the same session must be busy, got", c)
	}
	// Everything else goes through while the exec runs.
	for _, tc := range []struct {
		tool string
		a    m
	}{{"session_send_signal", m{"target": "s1", "signal": "interrupt"}}, {"session_focus", m{"target": "s1"}}, {"session_read", m{"target": "s1"}}, {"session_send_text", m{"target": "s1", "text": "x"}}, {"session_close", m{"target": "s1"}}, {"session_list", m{}}} {
		if r := call(g, "o", tc.tool, tc.a); !r.OK {
			t.Fatal(tc.tool, "waited behind exec:", r.Error)
		}
	}
	if r := call(g, "o", "exec", m{"target": "local", "command": "x"}); r.OK {
		t.Fatal("local exec")
	}
	close(b.execGate)
	if r := <-done; !r.OK {
		t.Fatal(r.Error)
	}
	if r := call(g, "o", "exec", m{"target": "s1", "command": "uptime"}); !r.OK {
		t.Fatal("busy flag leaked", r.Error)
	}
}

func TestConcurrentOpenOfOneHostCoalesces(t *testing.T) {
	b := newRich()
	b.sessions = nil
	release := make(chan struct{})
	b.open = func(ctx context.Context, host string, ua UserAction) (string, error) {
		<-release
		return "new-session", nil
	}
	g := New(b, nil)
	g.SetAutoApprove(UntilExit)
	var wg sync.WaitGroup
	results := make([]Result, 5)
	for i := range results {
		wg.Add(1)
		go func() { defer wg.Done(); results[i] = call(g, "o", "session_open", m{"target": "h1"}) }()
	}
	time.Sleep(100 * time.Millisecond)
	close(release)
	wg.Wait()
	for _, r := range results {
		d := data(t, r)
		if d["status"] != "opened" || d["sessionId"] != "new-session" {
			t.Fatal(r)
		}
	}
	if n := b.opens.Load(); n != 1 {
		t.Fatal("connected", n, "times")
	}
}

func TestOpenNeedingTheUserReturnsAtOnceAndLaterCallsJoin(t *testing.T) {
	b := newRich()
	b.sessions = nil
	answered := make(chan struct{})
	prompted := make(chan struct{})
	b.open = func(ctx context.Context, host string, ua UserAction) (string, error) {
		done := ua("host_key")
		close(prompted)
		<-answered
		done()
		time.Sleep(50 * time.Millisecond) // connecting after the answer
		return "joined", nil
	}
	g := New(b, nil)
	g.SetAutoApprove(UntilExit)
	started := time.Now()
	r := call(g, "o", "session_open", m{"target": "h1"})
	if d := data(t, r); d["status"] != "user_action_required" || d["reason"] != "host_key" || d["message"] == "" {
		t.Fatal(r)
	}
	if time.Since(started) > 2*time.Second {
		t.Fatal("waited for the user")
	}
	<-prompted
	// The dialog is still open: a retry joins the same attempt and says so again.
	if d := data(t, call(g, "o", "session_open", m{"target": "h1"})); d["status"] != "user_action_required" {
		t.Fatal(d)
	}
	if b.opens.Load() != 1 {
		t.Fatal("second connection started")
	}
	close(answered)
	var d map[string]any
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
		if d = data(t, call(g, "o", "session_open", m{"target": "h1"})); d["status"] != "user_action_required" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if d["status"] != "opened" || d["sessionId"] != "joined" {
		t.Fatal(d)
	}
	if b.opens.Load() != 1 {
		t.Fatal("reconnected")
	}
	// No slot stays occupied: exec on another session is unaffected and the open op is gone.
	g.mu.Lock()
	left := len(g.opens) + len(g.busy)
	g.mu.Unlock()
	if left != 0 {
		t.Fatal("bookkeeping leaked", left)
	}
}

func TestOpenFailureIsDeliveredOnceWithItsCode(t *testing.T) {
	b := newRich()
	b.sessions = nil
	b.open = func(ctx context.Context, host string, ua UserAction) (string, error) {
		done := ua("password")
		defer done()
		time.Sleep(30 * time.Millisecond)
		return "", Coded("auth_failed", errors.New("ssh: unable to authenticate password=TOPSECRET"))
	}
	g := New(b, nil)
	g.SetAutoApprove(UntilExit)
	if d := data(t, call(g, "o", "session_open", m{"target": "h1"})); d["reason"] != "password" {
		t.Fatal(d)
	}
	time.Sleep(100 * time.Millisecond)
	r := call(g, "o", "session_open", m{"target": "h1"})
	if c := code(t, r); c != "auth_failed" || strings.Contains(r.Error.Message, "TOPSECRET") || !strings.Contains(r.Error.Message, "unable to authenticate") {
		t.Fatal(r.Error)
	}
	// Consumed: the next call is a fresh attempt.
	data(t, call(g, "o", "session_open", m{"target": "h1"}))
	if b.opens.Load() != 2 {
		t.Fatal(b.opens.Load())
	}
}

func TestErrorModelCodesAndCauses(t *testing.T) {
	b := newRich()
	g := New(b, nil)
	g.SetAutoApprove(UntilExit)
	if c := code(t, call(g, "o", "session_read", m{"target": "dead"})); c != "disconnected" {
		t.Fatal(c)
	}
	if c := code(t, call(g, "o", "exec", m{"target": "gone", "command": "ls"})); c != "disconnected" {
		t.Fatal("a closed session's id must not be not_found:", c)
	}
	if c := code(t, call(g, "o", "session_read", m{"target": "never-existed"})); c != "not_found" {
		t.Fatal(c)
	}
	if c := code(t, call(g, "o", "session_read", m{})); c != "invalid_argument" {
		t.Fatal(c)
	}
	for _, c := range []string{"disconnected", "auth_failed", "host_key", "refused", "unreachable", "user_action_required", "user_typing", "timeout", "busy", "cwd_missing"} {
		if messages[c] == "" {
			t.Fatal("no message for", c)
		}
	}
	r := fail("internal", "boom: password=TOPSECRET\n and a very long tail "+strings.Repeat("x", 500))
	if strings.Contains(r.Error.Message, "TOPSECRET") || strings.Contains(r.Error.Message, "\n") || len([]rune(r.Error.Message)) > 300 {
		t.Fatal(r.Error.Message)
	}
	if r := failErr(errors.New("dial: token=TOPSECRET")); r.Error.Code != "internal" || strings.Contains(r.Error.Message, "TOPSECRET") || !strings.Contains(r.Error.Message, "dial") {
		t.Fatal(r.Error)
	}
	if r := failErr(fmt.Errorf("wrapped: %w", context.DeadlineExceeded)); r.Error.Code != "timeout" {
		t.Fatal(r.Error)
	}
}

func TestSearchesMatchRedactedTextOnlyAndPage(t *testing.T) {
	b := newRich()
	for i := 0; i < 120; i++ {
		b.hosts = append(b.hosts, Host{ID: fmt.Sprint("x", i), Name: fmt.Sprint("bulk-", i)})
		b.commands = append(b.commands, Command{ID: fmt.Sprint("c", i), Name: fmt.Sprint("cmd-", i), Command: "echo " + fmt.Sprint(i)})
	}
	b.commands = append(b.commands, Command{ID: "sec", Name: "deploy", Command: "mysql -pHUNTER2secret app"})
	g := New(b, nil)
	// A query for the secret part finds nothing: the redacted text is what is searched.
	for _, tc := range []struct {
		tool string
		key  string
		q    string
	}{{"host_list", "hosts", "HIDDENx"}, {"command_search", "matches", "HUNTER2"}} {
		d := data(t, call(g, "o", tc.tool, m{"query": tc.q}))
		if d["total"] != 0 {
			t.Fatal(tc.tool, "answered a query for hidden text", d)
		}
	}
	d := data(t, call(g, "o", "host_list", m{"query": "db password"}))
	if d["total"] != 1 || strings.Contains(fmt.Sprint(d["hosts"]), "HIDDENx") {
		t.Fatal(d)
	}
	// Paging and bounds.
	d = data(t, call(g, "o", "host_list", m{}))
	if len(d["hosts"].([]Host)) != 50 || d["total"] != 122 || d["limit"] != 50 {
		t.Fatal("default page", len(d["hosts"].([]Host)), d["total"])
	}
	d = data(t, call(g, "o", "host_list", m{"limit": 200, "offset": 100}))
	names := []string{}
	for _, h := range b.hosts {
		names = append(names, strings.ToLower(Redact(h.Name)))
	}
	slices.Sort(names)
	if hosts := d["hosts"].([]Host); len(hosts) != 22 || strings.ToLower(hosts[0].Name) != names[100] || strings.ToLower(hosts[21].Name) != names[121] {
		t.Fatal("the page is not the 101st..122nd host by name", len(hosts), d["offset"])
	}
	if r := call(g, "o", "host_list", m{"limit": 5000}); code(t, r) != "invalid_argument" || !strings.Contains(r.Error.Message, "limit must be between 1 and 200") {
		t.Fatal("limit above the range must be refused, naming the range:", r.Error)
	}
	if d = data(t, call(g, "o", "command_search", m{"limit": 90})); len(d["matches"].([]Command)) != 90 || d["total"] != 121 {
		t.Fatal("command_search limit ignored", len(d["matches"].([]Command)))
	}
	if c := code(t, call(g, "o", "command_search", m{"limit": 9999})); c != "invalid_argument" {
		t.Fatal("limit above 200 is out of range:", c)
	}
	if c := code(t, call(g, "o", "host_list", m{"offset": -1})); c != "invalid_argument" {
		t.Fatal(c)
	}
}

func TestCommandSaveMergesAndValidates(t *testing.T) {
	b := newRich()
	yes := true
	b.commands = []Command{{ID: "c1", Name: "old", Command: "ls", Group: "g", AppendCR: &yes, WorkspaceID: ""}, {ID: "c2", Name: "cloud", Command: "pwd", WorkspaceID: "ws1"}}
	g := New(b, nil)
	g.SetAutoApprove(UntilExit)
	data(t, call(g, "o", "command_save", m{"id": "c1", "command": "ls -la"}))
	if last := b.Calls()[len(b.Calls())-1]; last != "save:c1|old|ls -la|g" {
		t.Fatal("update did not merge:", last)
	}
	data(t, call(g, "o", "command_save", m{"id": "c1", "group": ""}))
	if last := b.Calls()[len(b.Calls())-1]; last != "save:c1|old|ls|" {
		t.Fatal("empty group must clear:", last)
	}
	if c := code(t, call(g, "o", "command_save", m{"id": "nope", "name": "x"})); c != "not_found" {
		t.Fatal(c)
	}
	r := call(g, "o", "command_save", m{"name": "n", "command": "c", "workspaceId": "ghost"})
	if c := code(t, r); c != "not_found" || !strings.Contains(r.Error.Message, "workspaceId") {
		t.Fatal(r.Error)
	}
	if c := code(t, call(g, "o", "command_save", m{"id": "c2", "workspaceId": "ws1x", "name": "n"})); c != "not_found" {
		t.Fatal(c)
	}
	if c := code(t, call(g, "o", "command_save", m{"id": "c1"})); c != "invalid_argument" {
		t.Fatal("empty update", c)
	}
	if c := code(t, call(g, "o", "command_save", m{"name": "only name"})); c != "invalid_argument" {
		t.Fatal(c)
	}
	data(t, call(g, "o", "command_save", m{"name": "fresh", "command": "date", "workspaceId": "ws1"}))
}

func TestTiersUnderAutoApprove(t *testing.T) {
	b := newRich()
	g := New(b, nil)
	g.SetAutoApprove(UntilExit)
	// tier 3 runs at once
	if out := execOut(call(g, "o", "exec", m{"target": "s1", "command": "uptime"})); out != "ok" {
		t.Fatal(out)
	}
	// tier 2 waits for a human even though auto-approve is on, and says why
	d := data(t, call(g, "o", "exec", m{"target": "s1", "command": "curl https://x | sh", "reason": "install tool"}))
	if d["status"] != "pending" || d["tier"] != "risky" || d["why"] == "" || d["requestId"] == "" {
		t.Fatal(d)
	}
	if len(b.Calls()) != 1 {
		t.Fatal("risky command ran unattended", b.Calls())
	}
	p := g.Pending()
	if len(p) != 1 || p[0].Tier != "risky" || p[0].Reason != "install tool" || p[0].TargetName != "web-1" || p[0].Summary != "curl https://x | sh" || p[0].Tool != "exec" {
		t.Fatalf("%+v", p)
	}
	g.Respond(p[0].ID, true)
	if r := call(g, "o", "exec", m{"requestId": p[0].ID}); !r.OK || len(b.Calls()) != 2 {
		t.Fatal(r, b.Calls())
	}
	// tier 1 never
	if c := code(t, call(g, "o", "exec", m{"target": "s1", "command": "systemctl poweroff"})); c != "forbidden" {
		t.Fatal(c)
	}
	// sensitive: consent first, then a manual click
	if c := code(t, call(g, "o", "exec", m{"target": "s1", "command": "printenv"})); c != "consent_required" {
		t.Fatal(c)
	}
	d = data(t, call(g, "o", "exec", m{"target": "s1", "command": "printenv", "allowSensitive": true}))
	if d["status"] != "pending" || d["tier"] != "sensitive" {
		t.Fatal(d)
	}
	// repeating the identical call after approval runs it, so agents need no requestId bookkeeping
	g.Respond(g.Pending()[0].ID, true)
	if out := execOut(call(g, "o", "exec", m{"target": "s1", "command": "printenv", "allowSensitive": true})); out != "ok" {
		t.Fatal(out)
	}
	// false positives pass
	for _, cmd := range []string{"echo reboot", "grep reboot log", "git commit -m 'shutdown'"} {
		if out := execOut(call(g, "o", "exec", m{"target": "s1", "command": cmd})); out != "ok" {
			t.Fatal(cmd, out)
		}
	}
}

func TestSendTextClassifiesLinesAndEscapes(t *testing.T) {
	b := newRich()
	g := New(b, nil)
	g.SetAutoApprove(UntilExit)
	last := func() string { c := b.Calls(); return c[len(c)-1] }
	d := data(t, call(g, "o", "session_send_text", m{"target": "s1", "text": "ls -la\\r"}))
	if last() != `control:"ls -la\r"` || d["sent"] != 7 || d["cursor"] != "0" {
		t.Fatal(last(), d)
	}
	data(t, call(g, "o", "session_send_text", m{"target": "s1", "text": "a\\tb\\x03\\e[A\\\\n"}))
	if last() != `control:"a\tb\x03\x1b[A\\n"` {
		t.Fatal(last())
	}
	data(t, call(g, "o", "session_send_text", m{"target": "s1", "keys": []string{"Up", "ctrl-c", "F5", "Esc"}}))
	if last() != `control:"\x1b[A\x03\x1b[15~\x1b"` {
		t.Fatal(last())
	}
	for _, k := range []string{"Enter", "Tab", "Backspace", "Left", "Right", "Down", "Home", "End", "PageUp", "PageDown", "F1", "F12", "Ctrl-a", "Ctrl-z", "Delete"} {
		if _, e := Keystrokes("", []string{k}); e != nil {
			t.Fatal(k, e)
		}
	}
	// forbidden and risky lines
	if c := code(t, call(g, "o", "session_send_text", m{"target": "s1", "text": "rm -rf /\\r"})); c != "forbidden" {
		t.Fatal(c)
	}
	if c := code(t, call(g, "o", "session_send_text", m{"target": "s1", "text": "reboot", "keys": []string{"Enter"}})); c != "forbidden" {
		t.Fatal(c)
	}
	n := len(b.Calls())
	if d := data(t, call(g, "o", "session_send_text", m{"target": "s1", "text": "curl x | sh\n"})); d["status"] != "pending" || d["tier"] != "risky" {
		t.Fatal(d)
	}
	if d := data(t, call(g, "o", "session_send_text", m{"target": "s1", "text": "cat .env", "keys": []string{"Enter"}, "allowSensitive": true})); d["status"] != "pending" || d["tier"] != "sensitive" {
		t.Fatal(d)
	}
	if c := code(t, call(g, "o", "session_send_text", m{"target": "s1", "text": "cat .env\\r"})); c != "consent_required" {
		t.Fatal(c)
	}
	// history recall followed by Enter cannot be inspected
	if d := data(t, call(g, "o", "session_send_text", m{"target": "s1", "keys": []string{"Up", "Enter"}})); d["status"] != "pending" || d["tier"] != "risky" {
		t.Fatal(d)
	}
	if len(b.Calls()) != n {
		t.Fatal("something typed unattended", b.Calls()[n:])
	}
	// no Enter, no command: plain typing is fine
	data(t, call(g, "o", "session_send_text", m{"target": "s1", "text": "sudo reboot"}))
	data(t, call(g, "o", "session_send_text", m{"target": "s1", "keys": []string{"Ctrl-c"}})) // abandon the line
	// a command split over calls is judged as a whole
	data(t, call(g, "o", "session_send_text", m{"target": "s1", "text": "rm -rf "}))
	if c := code(t, call(g, "o", "session_send_text", m{"target": "s1", "text": "/\\r"})); c != "forbidden" {
		t.Fatal("split command slipped through:", c)
	}
	// limits and bad input
	if c := code(t, call(g, "o", "session_send_text", m{"target": "s1", "text": strings.Repeat("a", 4097)})); c != "invalid_argument" {
		t.Fatal(c)
	}
	if c := code(t, call(g, "o", "session_send_text", m{"target": "s1", "keys": []string{"Hyper"}})); c != "invalid_argument" {
		t.Fatal(c)
	}
	if c := code(t, call(g, "o", "session_send_text", m{"target": "s1"})); c != "invalid_argument" {
		t.Fatal(c)
	}
	if c := code(t, call(g, "o", "session_send_text", m{"target": "dead", "text": "x"})); c != "disconnected" {
		t.Fatal(c)
	}
	b.typing = true
	if c := code(t, call(g, "o", "session_send_text", m{"target": "s1", "text": "x"})); c != "user_typing" {
		t.Fatal(c)
	}
	if c := code(t, call(g, "o", "session_send_signal", m{"target": "s1", "signal": "interrupt"})); c != "user_typing" {
		t.Fatal(c)
	}
}

func TestInterruptForgetsTheHalfTypedLine(t *testing.T) {
	b := newRich()
	g := New(b, nil)
	g.SetAutoApprove(UntilExit)
	data(t, call(g, "o", "session_send_text", m{"target": "s1", "text": "sudo ls"}))
	data(t, call(g, "o", "session_send_signal", m{"target": "s1", "signal": "interrupt"}))
	if d := data(t, call(g, "o", "session_send_text", m{"target": "s1", "text": "echo hi\\r"})); d["sent"] != 8 {
		t.Fatal("the cancelled sudo must not taint the next line", d)
	}
	r := call(g, "o", "session_send_text", m{"target": "s1", "text": "reboot\\r"})
	if r.Error == nil || r.Error.Code != "forbidden" || !strings.HasPrefix(r.Error.Message, "Blocked by the command policy: ") {
		t.Fatalf("%+v", r.Error)
	}
}

func TestSendTextNeedsApprovalWithoutAutoApprove(t *testing.T) {
	b := newRich()
	g := New(b, nil)
	d := data(t, call(g, "o", "session_send_text", m{"target": "s1", "text": "ls\\r", "reason": "look around"}))
	if d["status"] != "pending" || len(b.Calls()) != 0 {
		t.Fatal(d)
	}
	p := g.Pending()[0]
	if p.Summary != `"ls\r"` || p.Reason != "look around" {
		t.Fatalf("%+v", p)
	}
	g.Respond(p.ID, true)
	data(t, call(g, "o", "session_send_text", m{"requestId": p.ID}))
	if len(b.Calls()) != 1 {
		t.Fatal(b.Calls())
	}
}

func TestSessionCloseAndAlreadyClosed(t *testing.T) {
	b := newRich()
	g := New(b, nil)
	if d := data(t, call(g, "o", "session_close", m{"target": "dead"})); d["status"] != "pending" {
		t.Fatal(d)
	}
	g.SetAutoApprove(UntilExit)
	if d := data(t, call(g, "o", "session_close", m{"target": "dead"})); d["closed"] != true {
		t.Fatal("a dropped tab can be closed", d)
	}
	if d := data(t, call(g, "o", "session_close", m{"target": "gone"})); d["alreadyClosed"] != true {
		t.Fatal(d)
	}
	if c := code(t, call(g, "o", "session_close", m{"target": "never"})); c != "not_found" {
		t.Fatal(c)
	}
}

func TestReadSinceCursorAndWaitFor(t *testing.T) {
	b := newRich()
	g := New(b, nil)
	d := data(t, call(g, "o", "session_read", m{"target": "s1"}))
	if d["mode"] != "screen" || d["cursor"] != "0" {
		t.Fatal(d)
	}
	b.feed("\x1b[32mhello\x1b[0m password=TOPSECRET\r\nline2\r\n")
	d = data(t, call(g, "o", "session_read", m{"target": "s1", "sinceCursor": "0"}))
	c := d["content"].(string)
	if d["mode"] != "output" || strings.Contains(c, "TOPSECRET") || strings.Contains(c, "\x1b") || !strings.HasPrefix(c, "hello password=") || d["cursor"] != strconv.Itoa(len(b.out)) {
		t.Fatal(d)
	}
	cursor := d["cursor"].(string)
	if d = data(t, call(g, "o", "session_read", m{"target": "s1", "sinceCursor": cursor})); d["content"] != "" {
		t.Fatal("nothing new expected", d)
	}
	// waitFor returns as soon as the text shows up
	go func() { time.Sleep(200 * time.Millisecond); b.feed("building...\ndone: OK 42\n") }()
	started := time.Now()
	d = data(t, call(g, "o", "session_read", m{"target": "s1", "sinceCursor": cursor, "waitFor": m{"pattern": "done: OK", "timeoutSec": 10}}))
	if d["matched"] != true || d["timedOut"] != false || time.Since(started) > 5*time.Second || !strings.Contains(d["content"].(string), "done: OK 42") {
		t.Fatal(d)
	}
	// regex
	d = data(t, call(g, "o", "session_read", m{"target": "s1", "sinceCursor": cursor, "waitFor": m{"pattern": `re:OK \d+$`}}))
	if d["matched"] != true {
		t.Fatal(d)
	}
	// a pattern cannot probe redacted text
	d = data(t, call(g, "o", "session_read", m{"target": "s1", "sinceCursor": "0", "waitFor": m{"pattern": "TOPSECRET", "timeoutSec": 1}}))
	if d["matched"] != false || d["timedOut"] != true {
		t.Fatal("matched against unredacted text", d)
	}
	// without sinceCursor waitFor sees only output that arrives from now on
	d = data(t, call(g, "o", "session_read", m{"target": "s1", "waitFor": m{"pattern": "done: OK", "timeoutSec": 1}}))
	if d["matched"] != false {
		t.Fatal("matched old output", d)
	}
	for _, bad := range []m{{"target": "s1", "sinceCursor": "abc"}, {"target": "s1", "waitFor": m{"pattern": ""}}, {"target": "s1", "waitFor": m{"pattern": "re:["}}, {"target": "s1", "waitFor": m{"pattern": "x", "timeoutSec": 61}}} {
		if c := code(t, call(g, "o", "session_read", bad)); c != "invalid_argument" {
			t.Fatal(bad, c)
		}
	}
	// a local shell has no stream
	if c := code(t, call(g, "o", "session_read", m{"target": "local", "sinceCursor": "5"})); c != "unavailable" {
		t.Fatal(c)
	}
	if d = data(t, call(g, "o", "session_read", m{"target": "local", "waitFor": m{"pattern": "screen"}})); d["matched"] != true {
		t.Fatal(d)
	}
}

func TestReadWaitStopsWhenHalted(t *testing.T) {
	b := newRich()
	g := New(b, nil)
	done := make(chan Result, 1)
	go func() {
		done <- call(g, "o", "session_read", m{"target": "s1", "waitFor": m{"pattern": "never", "timeoutSec": 60}})
	}()
	time.Sleep(200 * time.Millisecond)
	g.Halt(true)
	select {
	case r := <-done:
		if code(t, r) != "forbidden" {
			t.Fatal(r)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("wait ignored the emergency stop")
	}
}

func TestAgentStatusAndRequestCancel(t *testing.T) {
	b := newRich()
	g := New(b, nil)
	d := data(t, call(g, "me", "agent_status", m{}))
	if d["enabled"] != true || d["halted"] != false || d["autoApprove"].(map[string]any)["on"] != false || len(d["pending"].([]map[string]any)) != 0 {
		t.Fatal(d)
	}
	id := data(t, call(g, "me", "exec", m{"target": "s1", "command": "sudo ls"}))["requestId"].(string)
	data(t, call(g, "other", "exec", m{"target": "s1", "command": "uptime"}))
	st := data(t, call(g, "me", "agent_status", m{}))["pending"].([]map[string]any)
	if len(st) != 1 || st[0]["requestId"] != id || st[0]["tool"] != "exec" || st[0]["summary"] != "sudo ls" || st[0]["tier"] != "risky" {
		t.Fatal("only my own requests, with tool/summary/tier:", st)
	}
	g.SetAutoApprove(time.Hour)
	auto := data(t, call(g, "me", "agent_status", m{}))["autoApprove"].(map[string]any)
	if auto["on"] != true || auto["expiresAt"] == nil {
		t.Fatal(auto)
	}
	if c := code(t, call(g, "other", "request_cancel", m{"requestId": id})); c != "not_found" {
		t.Fatal("cancelled someone else's request:", c)
	}
	if d := data(t, call(g, "me", "request_cancel", m{"requestId": id})); d["cancelled"] != true {
		t.Fatal(d)
	}
	if len(g.Pending()) != 1 { // only the other client's remains
		t.Fatal(g.Pending())
	}
	if c := code(t, call(g, "me", "request_cancel", m{"requestId": id})); c != "not_found" {
		t.Fatal(c)
	}
	if c := code(t, call(g, "me", "exec", m{"requestId": id})); c != "not_found" {
		t.Fatal("cancelled request still runnable", c)
	}
	g.Halt(true)
	if d := data(t, call(g, "me", "agent_status", m{})); d["halted"] != true {
		t.Fatal("status must work while halted", d)
	}
}

func TestNotifyFiresForMutatingCallsOnly(t *testing.T) {
	b := newRich()
	g := New(b, nil)
	var mu sync.Mutex
	var got []Notice
	g.SetNotify(func(n Notice) { mu.Lock(); got = append(got, n); mu.Unlock() })
	call(g, "o", "session_list", m{})
	call(g, "o", "session_read", m{"target": "s1"})
	if len(got) != 0 {
		t.Fatal("reads must stay silent", got)
	}
	id := data(t, call(g, "o", "session_focus", m{"target": "s1"}))["requestId"].(string)
	g.Respond(id, true)
	call(g, "o", "session_focus", m{"requestId": id})
	if len(got) != 2 || !got[0].Pending || got[1].Pending || got[1].Tool != "session_focus" {
		t.Fatal(got)
	}
}

func TestSpecsMatchTheGateway(t *testing.T) {
	want := []string{"host_list", "session_list", "session_read", "session_history", "command_search", "session_open", "command_save", "exec", "session_send_text", "session_send_signal", "session_focus", "session_close", "agent_status", "request_cancel"}
	var got []string
	for _, s := range Specs() {
		got = append(got, s.Name)
		if len(s.Description) < 80 || s.Schema["type"] != "object" || s.Schema["additionalProperties"] != false {
			t.Fatal("thin spec", s.Name)
		}
		props := s.Schema["properties"].(map[string]any)
		for _, r := range func() []string { r, _ := s.Schema["required"].([]string); return r }() {
			if props[r] == nil {
				t.Fatal(s.Name, "requires an undeclared property", r)
			}
		}
		for n, p := range props {
			if p.(map[string]any)["description"] == nil {
				t.Fatal(s.Name, n, "has no description")
			}
		}
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatal(got)
	}
	// every property a schema offers is accepted by the decoder, and the schemas' gated set is what the gateway gates
	g := New(newRich(), nil)
	for _, s := range Specs() {
		if s.Gated != (s.Name == "exec" || s.Name == "session_open" || s.Name == "command_save" || strings.HasPrefix(s.Name, "session_send") || s.Name == "session_focus" || s.Name == "session_close") {
			t.Fatal("gated set wrong for", s.Name)
		}
		for n := range s.Schema["properties"].(map[string]any) {
			var a Arguments
			raw, _ := json.Marshal(m{n: nil})
			d := json.NewDecoder(strings.NewReader(string(raw)))
			d.DisallowUnknownFields()
			if e := d.Decode(&a); e != nil && strings.Contains(e.Error(), "unknown field") {
				t.Fatal(s.Name, "documents", n, "but the gateway rejects it")
			}
		}
	}
	_ = g
}

// A secret that begins in one read and ends in the next is still masked: redaction sees the output
// before the cursor too.
func TestIncrementalReadsRedactSecretsSplitAcrossReads(t *testing.T) {
	b := newRich()
	g := New(b, nil)
	read := func(since string) (string, string) {
		d := data(t, call(g, "o", "session_read", m{"target": "s1", "sinceCursor": since}))
		return d["content"].(string), d["cursor"].(string)
	}
	body := "b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtzc2gtZWQyNTUxOQ"
	b.feed("$ cat key\r\n-----BEGIN OPENSSH PRIVATE KEY-----\r\n" + body[:30])
	first, cursor := read("0")
	b.feed(body[30:] + "\r\n" + body + "\r\n-----END OPENSSH PRIVATE KEY-----\r\nafter the key\r\n")
	second, _ := read(cursor)
	for _, got := range []string{first, second} {
		if strings.Contains(got, body[:8]) || strings.Contains(got, body[30:50]) || strings.Contains(got, "END OPENSSH") {
			t.Fatalf("private key leaked: %q", got)
		}
	}
	if !strings.Contains(first, redacted) || !strings.Contains(second, "after the key") {
		t.Fatalf("the key is masked once and what follows it still comes back: %q %q", first, second)
	}

	b2 := newRich()
	g = New(b2, nil)
	b2.feed("token is ghp_AbCdEf")
	first, cursor = read("0")
	if !strings.Contains(first, "token is") {
		t.Fatal(first)
	}
	b2.feed("0123456789abcdefghijKLMN done\r\n")
	second, _ = read(cursor)
	if strings.Contains(second, "0123456789") || strings.Contains(second, "AbCdEf") || !strings.Contains(second, redacted+" done") {
		t.Fatalf("token split across reads leaked: %q", second)
	}
	// a secret that never reaches the second read changes nothing
	b2.feed("plain line\r\n")
	if got, _ := read(cursor); !strings.HasSuffix(got, "plain line\n") {
		t.Fatal(got)
	}
}

func TestFreshTextSlicesAtTheCursor(t *testing.T) {
	for _, tc := range []struct {
		o     Output
		since int64
		want  string
		lost  bool
	}{
		{Output{Data: "hello world", Cursor: 11}, 6, "world", false},
		{Output{Data: "hello world", Cursor: 11}, 0, "hello world", false},
		{Output{Data: "hello", Cursor: 105}, 3, "hello", true}, // the cursor is older than what is kept
		{Output{Data: "abc", Cursor: 3}, 99, "", false},        // a cursor from the future
		{Output{Data: "中文字", Cursor: 9}, 4, "文字", false},
		{Output{Data: "\x1b[31mred\x1b[0m text", Cursor: 17}, 8, "text", false},        // the escape before the cursor is gone
		{Output{Data: "a password=hunter2\n", Cursor: 19}, 11, redacted + "\n", false}, // the value is cut off from its key by the cursor
	} {
		got, lost := freshText(tc.o, tc.since)
		if strings.TrimSpace(got) != strings.TrimSpace(tc.want) || lost != tc.lost {
			t.Errorf("%+v since %d: got %q lost=%v, want %q lost=%v", tc.o, tc.since, got, lost, tc.want, tc.lost)
		}
	}
}

// The user_action_required answer tells a changed host key from a first one, and a rejected saved
// password from a password that was never saved.
func TestUserActionRequiredSaysWhy(t *testing.T) {
	for _, tc := range []struct {
		reason, wantReason string
		saved              bool
		message            string
	}{
		{ReasonHostKey, "host_key", false, "host key"},
		{ReasonHostKeyChanged, "host_key_changed", false, "CHANGED host key"},
		{ReasonPassword, "password", false, "enter the password"},
		{ReasonPasswordRejected, "password", true, "saved for this host"},
		{ReasonOTP, "otp", false, "one-time code"},
	} {
		b := newRich()
		b.sessions = nil
		b.open = func(ctx context.Context, host string, ua UserAction) (string, error) {
			defer ua(tc.reason)()
			<-ctx.Done()
			return "", ctx.Err()
		}
		g := New(b, nil)
		g.SetAutoApprove(UntilExit)
		d := data(t, call(g, "o", "session_open", m{"target": "h1"}))
		_, saved := d["savedPasswordRejected"]
		if d["status"] != "user_action_required" || d["reason"] != tc.wantReason || saved != tc.saved || !strings.Contains(d["message"].(string), tc.message) {
			t.Errorf("%s: %v", tc.reason, d)
		}
		if tc.saved && d["savedPasswordRejected"] != true {
			t.Errorf("%s: %v", tc.reason, d)
		}
		g.Halt(true) // ends the attempt
	}
}

// A saved command is judged like exec: forbidden never lands in the library, risky needs a manual
// approval even under auto-approve.
func TestCommandSaveClassifiesTheCommandText(t *testing.T) {
	b := newRich()
	yes := true
	b.commands = []Command{{ID: "c1", Name: "old", Command: "ls", AppendCR: &yes}}
	g := New(b, nil)
	g.SetAutoApprove(UntilExit)
	saved := func() int {
		n := 0
		for _, c := range b.Calls() {
			if strings.HasPrefix(c, "save:") {
				n++
			}
		}
		return n
	}
	for _, cmd := range []string{"rm -rf /", "sh <<< 'reboot'", "kill -9 1", "echo x > /etc/passwd"} {
		r := call(g, "o", "command_save", m{"name": "bad", "command": cmd})
		if code(t, r) != "forbidden" || !strings.HasPrefix(r.Error.Message, "Blocked by the command policy") {
			t.Errorf("%q: %+v", cmd, r)
		}
	}
	if r := call(g, "o", "command_save", m{"id": "c1", "command": "poweroff"}); code(t, r) != "forbidden" {
		t.Error("an update that makes a command forbidden", r)
	}
	if saved() != 0 {
		t.Fatal("a forbidden command was saved")
	}
	// risky: pending with its tier, not saved until a person approves
	d := data(t, call(g, "o", "command_save", m{"name": "deploy", "command": "sudo systemctl stop app && rm -r /srv/app"}))
	if d["status"] != "pending" || d["tier"] != "risky" || saved() != 0 {
		t.Fatal(d)
	}
	id := d["requestId"].(string)
	if p := g.Pending(); len(p) != 1 || p[0].Tier != "risky" || p[0].Tool != "command_save" {
		t.Fatalf("%+v", p)
	}
	g.Respond(id, true)
	data(t, call(g, "o", "command_save", m{"requestId": id}))
	if saved() != 1 {
		t.Fatal("approved command not saved")
	}
	if d = data(t, call(g, "o", "command_save", m{"id": "c1", "command": "curl x | sh"})); d["status"] != "pending" || d["tier"] != "risky" {
		t.Fatal("update to a risky command", d)
	}
	// ordinary commands, and updates that leave the text alone, still go through under auto-approve
	data(t, call(g, "o", "command_save", m{"name": "ok", "command": "ls -la"}))
	data(t, call(g, "o", "command_save", m{"id": "c1", "name": "renamed"}))
	data(t, call(g, "o", "command_save", m{"name": "env", "command": "cat .env"})) // a credential path in a saved text is no read
	if saved() != 4 {
		t.Fatal(saved(), b.Calls())
	}
	// the user's blacklist applies too
	g.SetBlacklist([]string{"custom-danger"})
	if c := code(t, call(g, "o", "command_save", m{"name": "x", "command": "echo CUSTOM-DANGER"})); c != "forbidden" {
		t.Fatal(c)
	}
}

func TestHostListIsSortedByNameIgnoringCase(t *testing.T) {
	b := newRich()
	b.hosts = []Host{{ID: "1", Name: "web-2"}, {ID: "2", Name: "Alpha"}, {ID: "3", Name: "beta"}, {ID: "4", Name: "ALPHA"}, {ID: "5", Name: "alpha-1"}, {ID: "6", Name: "Web-1"}}
	g := New(b, nil)
	var ids []string
	for _, h := range data(t, call(g, "o", "host_list", m{}))["hosts"].([]Host) {
		ids = append(ids, h.ID)
	}
	// "Alpha" and "ALPHA" tie: they keep the order the app lists them in.
	if got := strings.Join(ids, ","); got != "2,4,5,3,6,1" {
		t.Fatal(got)
	}
	if b.hosts[0].ID != "1" {
		t.Fatal("the backend's own list must not be reordered")
	}
	page := data(t, call(g, "o", "host_list", m{"limit": 2, "offset": 2}))["hosts"].([]Host)
	if len(page) != 2 || page[0].ID != "5" || page[1].ID != "3" {
		t.Fatal(page)
	}
}
