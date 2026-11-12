package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/egoist/mygo/plugins/terminal"
	"net"
	"nextshell.dev/v2/internal/agent"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/model"
	"nextshell.dev/v2/internal/sshclient"
	"strings"
	"testing"
	"time"
)

func TestRecorderCursorsKeepABoundedWindow(t *testing.T) {
	r := &recorder{}
	r.record([]byte("hello "))
	r.record([]byte("world"))
	if d, total, lost := r.since(0); d != "hello world" || total != 11 || lost {
		t.Fatal(d, total, lost)
	}
	if d, total, _ := r.since(6); d != "world" || total != 11 {
		t.Fatal(d, total)
	}
	if d, total, lost := r.since(99); d != "" || total != 11 || lost {
		t.Fatal("a cursor from the future", d, total, lost)
	}
	chunk := []byte(strings.Repeat("x", 1<<20))
	for i := 0; i < 5; i++ {
		r.record(chunk)
	}
	d, total, lost := r.since(0)
	if !lost || total != 11+5<<20 || len(d) > maxOutputChunk {
		t.Fatal("old output must be reported lost and bounded", len(d), total, lost)
	}
	r.mu.Lock()
	kept := len(r.buf)
	r.mu.Unlock()
	if kept > 2*recordKeep {
		t.Fatal("recorder grew without bound", kept)
	}
	if d, _, lost := r.since(total - 5); d != "xxxxx" || lost {
		t.Fatal(d, lost)
	}
}

func TestOpenErrorsMapToAgentCodes(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code string
	}{
		{&sshclient.HostKeyError{Address: "h:22", Changed: true}, "host_key"},
		{errors.New("未信任主机"), "host_key"},
		{errors.New("ssh: handshake failed: ssh: unable to authenticate, attempted methods [none password], no supported methods remain"), "auth_failed"},
		{errors.New("dial tcp 127.0.0.1:1: connect: connection refused"), "refused"},
		{errors.New("dial tcp 10.255.255.1:22: i/o timeout"), "unreachable"},
		{errors.New("dial tcp: lookup nohost.invalid: no such host"), "unreachable"},
		{errors.New("connect: network is unreachable"), "unreachable"},
		{errors.New("已取消"), "denied"},
		{errors.New("等待输入超时"), "timeout"},
		{context.DeadlineExceeded, "timeout"},
	} {
		var coded *agent.CodedError
		if e := openError(tc.err); !errors.As(e, &coded) || coded.C != tc.code {
			t.Errorf("%v -> %v, want %s", tc.err, e, tc.code)
		}
	}
	plain := errors.New("something odd")
	if openError(plain) != plain {
		t.Fatal("unknown errors pass through")
	}
}

func localTab(t *testing.T, p *Platform) (string, <-chan string) {
	t.Helper()
	a, b := net.Pipe()
	term, e := p.newTerminal(terminal.Options{Conn: a, NoClipboardWrite: true})
	if e != nil {
		t.Fatal(e)
	}
	got := make(chan string, 8)
	go func() {
		buf := make([]byte, 256)
		for {
			n, e := b.Read(buf)
			if e != nil {
				return
			}
			got <- string(buf[:n])
		}
	}()
	t.Cleanup(func() { b.Close() })
	p.register(term, model.Connection{}, true, "")
	return awaitEvent(t, p, func(e appui.Event) bool { return e.Session != nil }).Session.ID, got
}

func TestAgentBackendOnALocalTab(t *testing.T) {
	p := desktopFixture(t)
	id, typed := localTab(t, p)
	b := agentBackend{p}
	if o, e := b.Output(id, -1); e != nil || o.Supported {
		t.Fatal("a local shell has no output stream", o, e)
	}
	if e := b.Control(id, "ls\r"); e != nil {
		t.Fatal(e)
	}
	select {
	case s := <-typed:
		if s != "ls\r" {
			t.Fatal(s)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("nothing typed")
	}
	// The user's keystrokes (the terminal's OnKey) block the agent for three seconds.
	p.mu.Lock()
	term := p.sessions[id]
	p.mu.Unlock()
	p.agentIO.mu.Lock()
	stamp := p.agentIO.typed[term]
	p.agentIO.mu.Unlock()
	stamp.Store(time.Now().UnixNano())
	if !p.UserTyping(id) {
		t.Fatal("recent key not noticed")
	}
	if e := b.Control(id, "x"); !errors.Is(e, agent.ErrUserTyping) {
		t.Fatal("agent typed over the user:", e)
	}
	p.gateway.Halt(false)
	p.gateway.SetAutoApprove(agent.UntilExit)
	r := p.gateway.Call(context.Background(), "o", "c", "session_send_text", json.RawMessage(fmt.Sprintf(`{"target":%q,"text":"x"}`, id)))
	if r.OK || r.Error.Code != "user_typing" {
		t.Fatal("want user_typing, got", r)
	}
	stamp.Store(time.Now().Add(-4 * time.Second).UnixNano())
	if p.UserTyping(id) {
		t.Fatal("still typing after the grace period")
	}
	r = p.gateway.Call(context.Background(), "o", "c", "session_send_text", json.RawMessage(fmt.Sprintf(`{"target":%q,"text":"y"}`, id)))
	if !r.OK {
		t.Fatal(r.Error)
	}
	// Close: the session is gone at once, its id is remembered, the window is told.
	if e := b.Close(id); e != nil {
		t.Fatal(e)
	}
	awaitEvent(t, p, func(e appui.Event) bool { return e.CloseID == id })
	if !b.Closed(id) || len(b.Sessions()) != 0 {
		t.Fatal("not closed")
	}
	if _, e := b.Output(id, -1); e == nil {
		t.Fatal("output of a closed session")
	}
	r = p.gateway.Call(context.Background(), "o", "c", "session_read", json.RawMessage(fmt.Sprintf(`{"target":%q}`, id)))
	if r.OK || r.Error.Code != "disconnected" {
		t.Fatal("a closed session's id must answer disconnected:", r)
	}
	if e := b.Close(id); e == nil {
		t.Fatal("double close")
	}
}

func TestClosedSessionIdsAreRememberedBoundedly(t *testing.T) {
	p := desktopFixture(t)
	for i := 0; i < closedKeep+50; i++ {
		p.forgetAgentIO(fmt.Sprint("s", i), &terminal.Terminal{}, nil)
	}
	b := agentBackend{p}
	if b.Closed("s0") || b.Closed("s49") || !b.Closed("s50") || !b.Closed(fmt.Sprint("s", closedKeep+49)) {
		t.Fatal("tombstones are not a bounded FIFO")
	}
	p.agentIO.mu.Lock()
	n := len(p.agentIO.closed)
	p.agentIO.mu.Unlock()
	if n != closedKeep {
		t.Fatal(n)
	}
}

func TestAgentOutputOfARealSSHSessionAndFocusHandover(t *testing.T) {
	p := desktopFixture(t)
	id, _ := phase2SSH(t, p)
	b := agentBackend{p}
	o, e := b.Output(id, -1)
	if e != nil || !o.Supported {
		t.Fatal(o, e)
	}
	if e := b.Control(id, "hello-from-agent\r"); e != nil { // the test server's shell echoes
		t.Fatal(e)
	}
	var got agent.Output
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if got, _ = b.Output(id, o.Cursor); strings.Contains(got.Data, "hello-from-agent") {
			break
		}
	}
	if !strings.Contains(got.Data, "hello-from-agent") || got.Lost || got.Cursor <= o.Cursor {
		t.Fatal("typed text did not come back through the recorder", got)
	}
	if again, _ := b.Output(id, got.Cursor); again.Data != "" {
		t.Fatal("cursor did not advance", again)
	}
	if e := b.Focus(id); e != nil {
		t.Fatal(e)
	}
	ev := awaitEvent(t, p, func(e appui.Event) bool { return e.FocusID == id })
	if !ev.Handover {
		t.Fatal("session_focus must be marked as a hand-over so the window can rate-limit raising it")
	}
}

// The user decision flow end to end on the platform: session_open returns at once while the host-key
// dialog is up, a retry joins the attempt, and the tab opens in the background.
func TestAgentSessionOpenWaitsForTheDialogWithoutBlocking(t *testing.T) {
	c, _ := desktopSSH(t)
	p := desktopFixture(t)
	c.ID = p.NewID()
	secret := c.Secret
	if e := p.SaveConnection(c, &secret); e != nil {
		t.Fatal(e)
	}
	p.gateway.Halt(false)
	p.gateway.SetAutoApprove(agent.UntilExit)
	open := func() map[string]any {
		r := p.gateway.Call(context.Background(), "o", "c", "session_open", json.RawMessage(fmt.Sprintf(`{"target":%q}`, c.ID)))
		if !r.OK {
			t.Fatal(r.Error)
		}
		return r.Data.(map[string]any)
	}
	started := time.Now()
	d := open()
	if d["status"] != "user_action_required" || d["reason"] != "host_key" || time.Since(started) > 3*time.Second {
		t.Fatal(d)
	}
	prompt := awaitEvent(t, p, func(e appui.Event) bool { return e.Prompt != nil }).Prompt
	if prompt.Kind != appui.PromptHostKey {
		t.Fatal(prompt.Kind)
	}
	if d = open(); d["status"] != "user_action_required" {
		t.Fatal("the dialog is still open:", d)
	}
	prompt.Answers <- []string{}
	var sessionID any
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if d = open(); d["status"] == "opened" {
			sessionID = d["sessionId"]
			break
		}
	}
	if sessionID == nil {
		t.Fatal("never opened", d)
	}
	ev := awaitEvent(t, p, func(e appui.Event) bool { return e.Session != nil })
	if !ev.Session.ByAgent || ev.Session.ID != sessionID {
		t.Fatal("the session event must say an agent opened it", ev.Session)
	}
	// The next call reuses the connected tab and never asks again.
	if d = open(); d["reused"] != true {
		t.Fatal(d)
	}
	p.CloseSession(ev.Session.ID)
}

func TestAgentOpenWithAWrongPasswordFailsWithAuthFailed(t *testing.T) {
	c, _ := desktopSSH(t)
	p := desktopFixture(t)
	c.ID, c.Secret = p.NewID(), "wrong"
	wrong := "wrong"
	if e := p.SaveConnection(c, &wrong); e != nil {
		t.Fatal(e)
	}
	// The host key is trusted by the poller below; the stored password is then rejected.
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-time.After(10 * time.Millisecond):
			}
			for _, e := range p.Poll() {
				if e.Prompt != nil && e.Prompt.Kind == appui.PromptHostKey {
					e.Prompt.Answers <- []string{}
				} else if e.Prompt != nil && e.Prompt.Kind == appui.PromptPassword {
					e.Prompt.Answers <- nil // cancel the re-ask
				}
			}
		}
	}()
	p.gateway.Halt(false)
	p.gateway.SetAutoApprove(agent.UntilExit)
	var last agent.Result
	for deadline := time.Now().Add(8 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		last = p.gateway.Call(context.Background(), "o", "c", "session_open", json.RawMessage(fmt.Sprintf(`{"target":%q}`, c.ID)))
		if !last.OK {
			break
		}
	}
	if last.OK || last.Error.Code != "auth_failed" && last.Error.Code != "denied" {
		t.Fatal("want auth_failed (or denied when the re-ask was cancelled), got", last)
	}
}

// The recorder knows when a full-screen program has the screen, also when a sequence is split over reads.
func TestRecorderTracksTheAlternateScreen(t *testing.T) {
	r := &recorder{}
	alt := func() bool { r.mu.Lock(); defer r.mu.Unlock(); return r.alt }
	for _, step := range []struct {
		chunk string
		want  bool
	}{
		{"user@host:~$ vim x\r\n", false},
		{"\x1b[?1049h\x1b[22;0;0t\x1b[1;24r", true},
		{"-- INSERT --", true},
		{"\x1b[?1049l", false},
		{"\x1b[?47h", true}, {"\x1b[?47l", false},
		{"\x1b[?1047h", true}, {"\x1b[?1047l", false},
		{"\x1b[?25l\x1b[?1049;2004h", true}, // combined parameters
		{"\x1b[?1049", true}, {"l", false},  // split across reads
		{"abc\x1b", false}, {"[?10", false}, {"49h", true},
		{"\x1b[?2004h\x1b[?1h", true}, // other private modes do not matter
		{"\x1b[?1049l\x1b[?1049h", true},
	} {
		r.record([]byte(step.chunk))
		if alt() != step.want {
			t.Fatalf("after %q: alt=%v, want %v", step.chunk, alt(), step.want)
		}
	}
}
