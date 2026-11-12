package desktop

import (
	"bytes"
	"context"
	"errors"
	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/agent"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/sshclient"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// What the agent tools need to know about a terminal without the UI: the bytes it received, whether
// its user is typing, and which session ids were closed.

const (
	recordKeep     = 1 << 20 // output kept per session for session_read cursors
	typingGrace    = 3 * time.Second
	closedKeep     = 256
	maxOutputChunk = 512 << 10
)

// recorder wraps a session's connection to remember what the terminal received, so that agents
// can read "everything since cursor N" and wait for a pattern instead of polling the screen.
type recorder struct {
	*sshclient.Session
	mu    sync.Mutex
	buf   []byte
	total int64
	alt   bool   // a full-screen program is on the alternate screen
	carry []byte // the start of an escape sequence the last read ended in
}

var (
	privateMode = regexp.MustCompile(`\x1b\[\?([0-9;]+)([hl])`)
	partialCSI  = regexp.MustCompile(`\x1b(?:\[\??[0-9;]*)?$`)
)

// trackAlt follows the alternate-screen modes (47, 1047, 1049) in what the terminal receives: the
// terminal plugin does not say whether a program such as vim or tmux has the screen. Called with mu held.
func (r *recorder) trackAlt(b []byte) {
	if len(r.carry) == 0 && bytes.IndexByte(b, 0x1b) < 0 {
		return
	}
	data := append(r.carry, b...)
	for _, m := range privateMode.FindAllSubmatch(data, -1) {
		for _, mode := range bytes.Split(m[1], []byte(";")) {
			switch string(mode) {
			case "47", "1047", "1049":
				r.alt = m[2][0] == 'h'
			}
		}
	}
	r.carry = nil
	if loc := partialCSI.FindIndex(data); loc != nil && len(data)-loc[0] <= 64 {
		r.carry = append([]byte(nil), data[loc[0]:]...)
	}
}

func (r *recorder) Read(b []byte) (int, error) {
	n, e := r.Session.Read(b)
	if n > 0 {
		r.record(b[:n])
	}
	return n, e
}
func (r *recorder) record(b []byte) {
	r.mu.Lock()
	r.trackAlt(b)
	r.buf = append(r.buf, b...)
	r.total += int64(len(b))
	if len(r.buf) > 2*recordKeep {
		r.buf = append([]byte(nil), r.buf[len(r.buf)-recordKeep:]...)
	}
	r.mu.Unlock()
}

// since returns the output after cursor and the new cursor; lost means cursor predates what is kept.
func (r *recorder) since(cursor int64) (data string, total int64, lost bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	start := r.total - int64(len(r.buf))
	if cursor < start {
		cursor, lost = start, true
	}
	if cursor > r.total {
		cursor = r.total
	}
	rest := r.buf[cursor-start:]
	if len(rest) > maxOutputChunk {
		rest, lost = rest[len(rest)-maxOutputChunk:], true
	}
	return string(rest), r.total, lost
}

type agentIO struct {
	mu          sync.Mutex
	recorders   map[*sshclient.Session]*recorder
	typed       map[*terminal.Terminal]*atomic.Int64
	closed      map[string]bool
	closedOrder []string
	execs       map[string][]*execRun // commands an agent runs on a session
}

// execRun is one agent command in flight on a session.
type execRun struct {
	cancel context.CancelCauseFunc
	done   chan struct{}
}

var errSessionClosed = errors.New("the session was closed while the command ran")

// startExec registers a command on session id; the returned function ends it.
func (a *agentIO) startExec(id string, cancel context.CancelCauseFunc) (finish func()) {
	r := &execRun{cancel: cancel, done: make(chan struct{})}
	a.mu.Lock()
	if a.execs == nil {
		a.execs = map[string][]*execRun{}
	}
	a.execs[id] = append(a.execs[id], r)
	a.mu.Unlock()
	return func() {
		a.mu.Lock()
		a.execs[id] = slices.DeleteFunc(a.execs[id], func(o *execRun) bool { return o == r })
		if len(a.execs[id]) == 0 {
			delete(a.execs, id)
		}
		a.mu.Unlock()
		close(r.done)
	}
}

// stopExecs cancels the commands running on session id and waits (briefly) until each has asked its
// server to kill the command, which has to happen while the connection is still up.
func (a *agentIO) stopExecs(id string) {
	a.mu.Lock()
	running := slices.Clone(a.execs[id])
	a.mu.Unlock()
	for _, r := range running {
		r.cancel(errSessionClosed)
	}
	deadline := time.After(2 * time.Second)
	for _, r := range running {
		select {
		case <-r.done:
		case <-deadline:
			return
		}
	}
}

// recorded returns the connection to hand to the terminal for s.
func (p *Platform) recorded(s *sshclient.Session) *recorder {
	rec := &recorder{Session: s}
	p.agentIO.mu.Lock()
	if p.agentIO.recorders == nil {
		p.agentIO.recorders = map[*sshclient.Session]*recorder{}
	}
	p.agentIO.recorders[s] = rec
	p.agentIO.mu.Unlock()
	return rec
}

// newTerminal starts a terminal and notes when its user presses a key in it.
func (p *Platform) newTerminal(o terminal.Options) (*terminal.Terminal, error) {
	o = p.terminalOptions(o)
	stamp := new(atomic.Int64)
	// OnKey only fires for the terminal that has the keyboard, so a recent stamp means the user is typing here.
	o.OnKey = func(ui.Modifiers, ui.Key) bool { stamp.Store(time.Now().UnixNano()); return false }
	t, e := terminal.New(o)
	if e != nil {
		return nil, e
	}
	p.agentIO.mu.Lock()
	if p.agentIO.typed == nil {
		p.agentIO.typed = map[*terminal.Terminal]*atomic.Int64{}
	}
	p.agentIO.typed[t] = stamp
	p.agentIO.mu.Unlock()
	return t, nil
}

// forgetAgentIO drops what was kept for a session that is being closed and remembers its id.
func (p *Platform) forgetAgentIO(id string, t *terminal.Terminal, s *sshclient.Session) {
	a := &p.agentIO
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.recorders, s)
	delete(a.typed, t)
	if t == nil {
		return
	}
	if a.closed == nil {
		a.closed = map[string]bool{}
	}
	a.closed[id] = true
	a.closedOrder = append(a.closedOrder, id)
	if len(a.closedOrder) > closedKeep {
		delete(a.closed, a.closedOrder[0])
		a.closedOrder = a.closedOrder[1:]
	}
}

// UserTyping reports whether the user pressed a key in the session's terminal in the last 3 seconds.
func (p *Platform) UserTyping(id string) bool {
	p.mu.Lock()
	t := p.sessions[id]
	p.mu.Unlock()
	p.agentIO.mu.Lock()
	stamp := p.agentIO.typed[t]
	p.agentIO.mu.Unlock()
	return stamp != nil && time.Since(time.Unix(0, stamp.Load())) < typingGrace && stamp.Load() != 0
}

func (b agentBackend) Closed(id string) bool {
	b.p.agentIO.mu.Lock()
	defer b.p.agentIO.mu.Unlock()
	return b.p.agentIO.closed[id]
}
func (b agentBackend) Workspaces() ([]string, error) {
	list, e := b.p.store.Workspaces()
	var ids []string
	for _, w := range list {
		ids = append(ids, w.ID)
	}
	return ids, e
}
func (b agentBackend) Output(id string, since int64) (agent.Output, error) {
	b.p.mu.Lock()
	s := b.p.sshSessions[id]
	_, listed := b.p.sessions[id]
	b.p.mu.Unlock()
	if !listed {
		return agent.Output{}, agent.Coded("disconnected", errors.New("session closed"))
	}
	b.p.agentIO.mu.Lock()
	rec := b.p.agentIO.recorders[s]
	b.p.agentIO.mu.Unlock()
	if rec == nil {
		return agent.Output{}, nil // a local shell: no stream, only the screen
	}
	if since < 0 {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		return agent.Output{Cursor: rec.total, Supported: true, Alt: rec.alt}, nil
	}
	data, total, lost := rec.since(since)
	rec.mu.Lock()
	alt := rec.alt
	rec.mu.Unlock()
	return agent.Output{Data: data, Cursor: total, Lost: lost, Supported: true, Alt: alt}, nil
}

// Control types into the session unless its user is doing so.
func (b agentBackend) Control(id, data string) error {
	if b.p.UserTyping(id) {
		return agent.ErrUserTyping
	}
	if e := b.p.Send(id, data); e != nil {
		return agent.Coded("disconnected", e)
	}
	return nil
}

// Close closes the tab; the window drops it from its strip when it sees the event.
func (b agentBackend) Close(id string) error {
	b.p.mu.Lock()
	_, ok := b.p.sessions[id]
	b.p.mu.Unlock()
	if !ok {
		return agent.Coded("not_found", errors.New("no such session"))
	}
	b.p.CloseSession(id)
	b.p.emit(appui.Event{CloseID: id})
	return nil
}

// ---- connections opened for an agent

type agentOrigin struct{}

// fromAgent reports whether a connection attempt was started by an MCP call.
func fromAgent(ctx context.Context) bool { return ctx.Value(agentOrigin{}) != nil }

// savedPasswordRejected marks a context whose next password prompt follows the server rejecting the saved password.
type savedPasswordRejected struct{}

// userAction tells the agent gateway that the connection waits for a dialog, if an agent started it.
func userAction(ctx context.Context, req *appui.Prompt) (done func()) {
	ua, _ := ctx.Value(agentOrigin{}).(agent.UserAction)
	if ua == nil {
		return func() {}
	}
	reason := agent.ReasonOTP
	switch req.Kind {
	case appui.PromptHostKey:
		reason = agent.ReasonHostKey
	case appui.PromptHostKeyChanged:
		reason = agent.ReasonHostKeyChanged
	case appui.PromptPassword:
		reason = agent.ReasonPassword
		if ctx.Value(savedPasswordRejected{}) != nil {
			reason = agent.ReasonPasswordRejected
		}
	}
	return ua(reason)
}

// openError gives a failed connection the code an agent can act on.
func openError(e error) error {
	var hk *sshclient.HostKeyError
	m := strings.ToLower(e.Error())
	switch {
	case errors.As(e, &hk) || strings.Contains(m, "未信任主机") || strings.Contains(m, "host key"):
		return agent.Coded("host_key", e)
	case strings.Contains(m, "unable to authenticate") || strings.Contains(m, "permission denied"):
		return agent.Coded("auth_failed", e)
	case strings.Contains(m, "已取消"):
		return agent.Coded("denied", e)
	case strings.Contains(m, "等待输入超时"):
		return agent.Coded("timeout", e)
	case strings.Contains(m, "handshake failed") && (strings.HasSuffix(m, "eof") || strings.Contains(m, "connection reset")):
		return agent.Coded("refused", errors.New("服务器在握手时断开（可能是连接数限制或代理拒绝）"))
	case strings.Contains(m, "connection refused"):
		return agent.Coded("refused", e)
	case strings.Contains(m, "no route to host") || strings.Contains(m, "network is unreachable") || strings.Contains(m, "no such host") || strings.Contains(m, "i/o timeout") || strings.Contains(m, "host is down") || strings.Contains(m, "dial tcp") || strings.Contains(m, "proxy"):
		return agent.Coded("unreachable", e)
	case errors.Is(e, context.DeadlineExceeded) || errors.Is(e, context.Canceled):
		return agent.Coded("timeout", e)
	}
	return e
}
