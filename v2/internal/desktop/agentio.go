package desktop

import (
	"context"
	"errors"
	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/agent"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/sshclient"
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
		return agent.Output{Cursor: rec.total, Supported: true}, nil
	}
	data, total, lost := rec.since(since)
	return agent.Output{Data: data, Cursor: total, Lost: lost, Supported: true}, nil
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

// userAction tells the agent gateway that the connection waits for a dialog, if an agent started it.
func userAction(ctx context.Context, req *appui.Prompt) (done func()) {
	ua, _ := ctx.Value(agentOrigin{}).(agent.UserAction)
	if ua == nil {
		return func() {}
	}
	reason := "otp"
	switch req.Kind {
	case appui.PromptHostKey, appui.PromptHostKeyChanged:
		reason = "host_key"
	case appui.PromptPassword:
		reason = "password"
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
	case strings.Contains(m, "connection refused"):
		return agent.Coded("refused", e)
	case strings.Contains(m, "no route to host") || strings.Contains(m, "network is unreachable") || strings.Contains(m, "no such host") || strings.Contains(m, "i/o timeout") || strings.Contains(m, "host is down") || strings.Contains(m, "dial tcp") || strings.Contains(m, "proxy"):
		return agent.Coded("unreachable", e)
	case errors.Is(e, context.DeadlineExceeded) || errors.Is(e, context.Canceled):
		return agent.Coded("timeout", e)
	}
	return e
}
