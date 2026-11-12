// Package desktop is the composition boundary between UI and the operating system.
package desktop

import (
	"context"
	"errors"
	"github.com/egoist/mygo/plugins/terminal"
	"io"
	"nextshell.dev/v2/internal/agent"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/cloudsync"
	"nextshell.dev/v2/internal/mcpendpoint"
	"nextshell.dev/v2/internal/model"
	"nextshell.dev/v2/internal/monitor"
	"nextshell.dev/v2/internal/sshclient"
	"nextshell.dev/v2/internal/storage"
	"nextshell.dev/v2/internal/transfer"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

type Platform struct {
	urlOpener        func(string)
	diagnosticCancel context.CancelFunc
	clipboard        func(string)
	preferences      model.Preferences
	featureMu        sync.Mutex
	clouds           map[string]*cloudsync.Service
	gateway          *agent.Gateway
	endpoint         *mcpendpoint.Endpoint
	endpointURL      string
	endpointPort     int
	agentPort        string // listen port; tests use "0"
	dbPath           string
	mcpPath          string // discovery file; see discoveryPathLocked
	mcpTemp          string // per-process directory for an in-memory profile
	sshSessions      map[string]*sshclient.Session
	agentConnects    map[string]*attempt // by connection id; see connectForAgent
	tar              map[string]bool     // whether each session's server has tar
	queue            *transfer.Queue
	imports          map[string]importPlan
	monitorPrevious  map[string]monitor.Snapshot
	monitorBusy      map[string]bool
	monitorWatches   map[string]context.CancelFunc
	quit             func()
	credentials      map[string]model.Connection
	store            *storage.Store
	ctx              context.Context
	cancel           context.CancelFunc
	mu               sync.Mutex
	events           []appui.Event
	sessions         map[string]*terminal.Terminal
	order            []string // session ids in creation order; see agentBackend.Sessions
	wake             func()
	closed           bool
	wg               sync.WaitGroup
}

func Open(path string) (*Platform, error) {
	s, e := storage.Open(path)
	if e != nil {
		return nil, e
	}
	prefs, e := s.Preferences()
	if e != nil {
		s.Close()
		return nil, e
	}
	ctx, c := context.WithCancel(context.Background())
	p := &Platform{dbPath: path, agentPort: mcpendpoint.DefaultPort, preferences: prefs, store: s, ctx: ctx, cancel: c, credentials: map[string]model.Connection{}, sessions: map[string]*terminal.Terminal{}}
	p.sshSessions = map[string]*sshclient.Session{}
	p.tar = map[string]bool{}
	p.imports = map[string]importPlan{}
	p.monitorPrevious = map[string]monitor.Snapshot{}
	p.monitorBusy = map[string]bool{}
	p.monitorWatches = map[string]context.CancelFunc{}
	p.clouds = map[string]*cloudsync.Service{}
	p.gateway = agent.New(agentBackend{p}, p.Invalidate)
	p.gateway.Halt(true)
	p.queue = transfer.New(2, p.Invalidate)
	return p, nil
}
func DefaultPath() (string, error) {
	d, e := os.UserConfigDir()
	if e != nil {
		return "", e
	}
	return filepath.Join(d, "NextShell-v2", "nextshell.db"), nil
}
func (p *Platform) SetWake(f func()) { p.mu.Lock(); p.wake = f; p.mu.Unlock() }
func (p *Platform) emit(e appui.Event) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.events = append(p.events, e)
	f := p.wake
	p.mu.Unlock()
	if f != nil {
		f()
	}
}
func (p *Platform) Poll() []appui.Event {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := p.events
	p.events = nil
	return out
}
func (p *Platform) Snapshot() (appui.Snapshot, error) {
	var s appui.Snapshot
	var e error
	if s.Connections, e = p.store.Connections(); e != nil {
		return s, e
	}
	if s.Folders, e = p.store.Folders(); e != nil {
		return s, e
	}
	if s.Keys, e = p.store.Keys(); e != nil {
		return s, e
	}
	s.Proxies, e = p.store.Proxies()
	if e != nil {
		return s, e
	}
	s.History, e = p.store.History()
	if e != nil {
		return s, e
	}
	s.Commands, e = p.store.Commands()
	return s, e
}
func (p *Platform) SaveConnection(c model.Connection, secret *string) error {
	return p.store.SaveConnection(c, secret)
}
func (p *Platform) SaveFolder(f model.Folder) error     { return p.store.SaveFolder(f) }
func (p *Platform) HostKeys() ([]model.HostKey, error)  { return p.store.HostPins() }
func (p *Platform) ForgetHostKey(address string) error  { return p.store.DeleteHostPin(address) }
func (p *Platform) SaveUIState(key, value string) error { return p.store.SaveUIState(key, value) }
func (p *Platform) UIState(key string) string {
	v, _ := p.store.UIState(key)
	return v
}
func (p *Platform) SaveProxy(a model.Proxy, s *string) error { return p.store.SaveProxy(a, s) }
func (p *Platform) SaveKey(k model.Key) error {
	pub, e := sshclient.InspectKey(k.Private, k.Passphrase)
	if e != nil {
		return e
	}
	k.Public = pub
	return p.store.SaveKey(k)
}
func (p *Platform) Delete(kind, id string) error { return p.store.Delete(kind, id) }
func (p *Platform) NewID() string                { return storage.ID() }
func (p *Platform) GenerateKey(name, algorithm string) (model.Key, error) {
	k := model.Key{ID: p.NewID(), Name: name}
	var e error
	k.Private, k.Public, e = sshclient.GenerateKeyWithType(algorithm, "")
	if e != nil {
		return k, e
	}
	return k, p.SaveKey(k)
}
func (p *Platform) ImportKey(path, pass string) (model.Key, error) {
	f, e := os.Open(path)
	if e != nil {
		return model.Key{}, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, 1024*1024+1))
	if e != nil {
		return model.Key{}, e
	}
	if len(b) > 1024*1024 {
		return model.Key{}, errors.New("key file too large")
	}
	k := model.Key{ID: p.NewID(), Name: filepath.Base(path), Private: string(b), Passphrase: pass}
	k.Public, e = sshclient.InspectKey(k.Private, pass)
	if e != nil {
		return k, e
	}
	return k, p.SaveKey(k)
}
func (p *Platform) ask(title string, questions []string, echo []bool) ([]string, error) {
	return p.askContext(p.ctx, title, questions, echo)
}
func (p *Platform) askContext(ctx context.Context, title string, questions []string, echo []bool) ([]string, error) {
	return p.prompt(ctx, &appui.Prompt{Title: title, Questions: questions, Echo: echo})
}

// prompt shows req and waits for the answer; a nil answer cancels.
func (p *Platform) prompt(ctx context.Context, req *appui.Prompt) ([]string, error) {
	done := make(chan struct{})
	defer func() { close(done); p.emit(appui.Event{}) }()
	req.Done = done
	req.Answers = make(chan []string, 1)
	req.ByAgent = fromAgent(ctx)
	p.emit(appui.Event{Prompt: req})
	// A dialog an agent caused stays until the user answers or cancels (or the app closes); one the user
	// asked for gives up after two minutes.
	var timeout <-chan time.Time
	if req.ByAgent {
		askAttention()
	} else {
		t := time.NewTimer(2 * time.Minute)
		defer t.Stop()
		timeout = t.C
	}
	select {
	case a := <-req.Answers:
		if a == nil || len(a) != len(req.Questions) {
			return nil, errors.New("已取消")
		}
		return a, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timeout:
		return nil, errors.New("等待输入超时")
	}
}
func (p *Platform) launch(fn func()) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.wg.Add(1)
	p.mu.Unlock()
	go func() { defer p.wg.Done(); fn() }()
}
func (p *Platform) Connect(c model.Connection) {
	p.launch(func() {
		if e := p.connect(c, ""); e != nil {
			p.emit(appui.Event{Message: e.Error(), Failed: c.ID})
		}
	})
}
func (p *Platform) connect(c model.Connection, replace string) error {
	return p.connectContext(p.ctx, c, replace)
}
func (p *Platform) connectContext(ctx context.Context, c model.Connection, replace string) error {
	var stored model.Connection
	if c.ID != "" {
		var e error
		if stored, e = p.store.Connection(c.ID); e != nil {
			return e
		}
		if c.Secret == "" {
			c.Secret = stored.Secret
		}
	}
	if c.Auth == "password" && c.Secret == "" {
		if e := p.askPassword(ctx, &c, stored, "输入密码"); e != nil {
			return e
		}
	}
	var k model.Key
	var pr *model.Proxy
	var e error
	if c.KeyID != "" {
		k, e = p.store.Key(c.KeyID)
		if e != nil {
			return e
		}
	}
	if c.ProxyID != "" {
		a, err := p.store.Proxy(c.ProxyID)
		if err != nil {
			return err
		}
		pr = &a
	}
	keepalive := effectiveKeepalive(c, p.Preferences())
	explicitPin := c.Fingerprint != ""
	if c.Fingerprint == "" {
		pin, err := p.store.HostPin(c.Address())
		if err != nil {
			return err
		}
		c.Fingerprint = pin
	}
	dialConnection := c
	dialConnection.Keepalive = keepalive
	o := sshclient.Options{Connection: dialConnection, Key: k, Proxy: pr, Timeout: 2 * time.Minute, KeepaliveTimeout: 15 * time.Second, HostKey: func(addr, fp string) error {
		pin, e := p.store.HostPin(addr)
		if e != nil {
			return e
		}
		if pin != "" {
			if pin != fp {
				return &sshclient.HostKeyError{Address: addr, Fingerprint: fp, Changed: true}
			}
			return nil
		}
		if _, e := p.prompt(ctx, &appui.Prompt{Kind: appui.PromptHostKey, Title: "首次连接，请核对主机指纹", Host: addr, Fingerprint: fp}); e != nil {
			return errors.New("未信任主机")
		}
		return p.store.PinHost(addr, fp)
	}, Keyboard: func(user, instruction string, questions []string, echo []bool) ([]string, error) {
		return p.askContext(ctx, strings.TrimSpace("服务器验证 "+user+" "+instruction), questions, echo)
	}}
	if c.UseKnownHosts {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		o.KnownHostsFile = filepath.Join(home, ".ssh", "known_hosts")
	}
	s, e := sshclient.Dial(ctx, o)
	var changed *sshclient.HostKeyError
	if errors.As(e, &changed) && changed.Changed {
		// The user compares both keys; only an explicit confirmation replaces the pin.
		if _, err := p.prompt(ctx, &appui.Prompt{Kind: appui.PromptHostKeyChanged, Title: "主机指纹已变化", Host: changed.Address, Fingerprint: changed.Fingerprint, OldFingerprint: c.Fingerprint}); err != nil {
			return e
		}
		if err := p.store.ReplaceHostPin(changed.Address, changed.Fingerprint); err != nil {
			return err
		}
		if explicitPin && c.ID != "" {
			stored.Fingerprint = changed.Fingerprint
			if err := p.store.SaveConnection(stored, nil); err != nil {
				return err
			}
		}
		o.Connection.Fingerprint = changed.Fingerprint
		s, e = sshclient.Dial(ctx, o)
	}
	// A rejected password asks again, at most three times in all.
	for tries := 1; tries < 3 && c.Auth == "password" && isAuthFailure(e); tries++ {
		if err := p.askPassword(ctx, &c, stored, "密码错误，请重新输入"); err != nil {
			return err
		}
		o.Connection.Secret = c.Secret
		s, e = sshclient.Dial(ctx, o)
	}
	if e != nil {
		return e
	}
	if e = ctx.Err(); e != nil {
		s.Close()
		return e
	}
	t, e := terminal.New(p.terminalOptions(terminal.Options{Conn: s, NoClipboardWrite: true, ClipboardRead: terminal.ClipboardDeny}))
	if e != nil {
		s.Close()
		return e
	}
	p.register(t, c, false, replace, s)
	return nil
}
func isAuthFailure(e error) bool {
	return e != nil && strings.Contains(e.Error(), "unable to authenticate")
}

// askPassword asks for c's password; a remembered one replaces the saved secret.
func (p *Platform) askPassword(ctx context.Context, c *model.Connection, stored model.Connection, title string) error {
	req := &appui.Prompt{Kind: appui.PromptPassword, Title: title, Host: c.User + "@" + c.Address(), Questions: []string{"密码"}, Echo: []bool{false}, CanRemember: c.ID != ""}
	a, e := p.prompt(ctx, req)
	if e != nil {
		return e
	}
	c.Secret = a[0]
	if req.Remember && c.ID != "" {
		return p.store.SaveConnection(stored, &c.Secret)
	}
	return nil
}
func (p *Platform) register(t *terminal.Terminal, c model.Connection, local bool, replace string, sshConn ...*sshclient.Session) {
	id := p.NewID()
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		t.Close()
		return
	}
	p.sessions[id] = t
	if i := slices.Index(p.order, replace); i >= 0 {
		p.order[i] = id
	} else {
		p.order = append(p.order, id)
	}
	if len(sshConn) > 0 {
		p.sshSessions[id] = sshConn[0]
	}
	p.credentials[id] = c
	c.Secret = ""
	p.mu.Unlock()
	title := c.Name
	if local {
		title = "本地终端"
	}
	p.emit(appui.Event{Session: &appui.Session{Replaces: replace, ID: id, Title: title, Terminal: t, Connection: c, Local: local}})
}
func (p *Platform) Local() {
	p.launch(func() {
		t, e := terminal.New(p.terminalOptions(terminal.Options{NoClipboardWrite: true, ClipboardRead: terminal.ClipboardDeny}))
		if e != nil {
			p.emit(appui.Event{Message: e.Error()})
			return
		}
		p.register(t, model.Connection{}, true, "")
	})
}
func (p *Platform) CloseSession(id string) {
	p.mu.Lock()
	t := p.sessions[id]
	delete(p.sessions, id)
	p.order = slices.DeleteFunc(p.order, func(o string) bool { return o == id })
	delete(p.sshSessions, id)
	delete(p.tar, id)
	if cancel := p.monitorWatches[id]; cancel != nil {
		cancel()
		delete(p.monitorWatches, id)
	}
	delete(p.monitorPrevious, id)
	delete(p.credentials, id)
	p.mu.Unlock()
	if t != nil {
		t.Close()
	}
}

func (p *Platform) Shutdown() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	p.cancel()
	ts := p.sessions
	p.sessions = map[string]*terminal.Terminal{}
	p.order = nil
	p.credentials = map[string]model.Connection{}
	p.mu.Unlock()
	for _, t := range ts {
		t.Close()
	}
	p.closeFeatures()
	p.queue.Close()
	p.wg.Wait()
	p.store.Close()
}

var _ appui.Platform = (*Platform)(nil)

func (p *Platform) DuplicateConnection(id string) (model.Connection, error) {
	return p.store.DuplicateConnection(id)
}
func (p *Platform) RenameKey(id, name string) error { return p.store.RenameKey(id, name) }
func (p *Platform) SaveHistory(h []string) error    { return p.store.SaveHistory(h) }
func (p *Platform) Capabilities() appui.TerminalCapabilities {
	return appui.TerminalCapabilities{OSC7: true, ScreenText: true, OSC133: false}
}

func (p *Platform) Reconnect(old *appui.Session) {
	p.launch(func() {
		if !old.Local {
			p.mu.Lock()
			c, ok := p.credentials[old.ID]
			p.mu.Unlock()
			if !ok {
				c = old.Connection
			}
			if e := p.connect(c, old.ID); e != nil {
				p.emit(appui.Event{Message: e.Error()})
			}
			return
		}
		t, e := terminal.New(p.terminalOptions(terminal.Options{NoClipboardWrite: true, ClipboardRead: terminal.ClipboardDeny}))
		if e != nil {
			p.emit(appui.Event{Message: e.Error()})
			return
		}
		p.register(t, model.Connection{}, true, old.ID)
	})
}

func (p *Platform) CloneSession(old *appui.Session) {
	if old.Local {
		p.Local()
		return
	}
	p.mu.Lock()
	c, ok := p.credentials[old.ID]
	p.mu.Unlock()
	if !ok {
		c = old.Connection
	}
	p.Connect(c)
}

func (p *Platform) SetQuit(fn func()) { p.mu.Lock(); p.quit = fn; p.mu.Unlock() }
func (p *Platform) Quit() {
	p.mu.Lock()
	fn := p.quit
	p.mu.Unlock()
	if fn != nil {
		fn()
	}
}
func (p *Platform) Invalidate() {
	p.mu.Lock()
	fn := p.wake
	p.mu.Unlock()
	if fn != nil {
		fn()
	}
}

func (p *Platform) Send(id, text string) error {
	p.mu.Lock()
	t := p.sessions[id]
	p.mu.Unlock()
	if t == nil {
		return errors.New("会话已关闭")
	}
	select {
	case <-t.Done():
		return errors.New("会话已退出")
	default:
	}
	t.Send([]byte(text))
	return nil
}
func (p *Platform) ScreenText(id string) string {
	p.mu.Lock()
	t := p.sessions[id]
	p.mu.Unlock()
	if t == nil {
		return ""
	}
	return t.Text()
}
func (p *Platform) Exited(id string) bool {
	p.mu.Lock()
	t := p.sessions[id]
	p.mu.Unlock()
	if t == nil {
		return true
	}
	select {
	case <-t.Done():
		return true
	default:
		return false
	}
}

func (p *Platform) SetClipboard(fn func(string)) { p.mu.Lock(); p.clipboard = fn; p.mu.Unlock() }
func (p *Platform) CopyText(s string) {
	p.mu.Lock()
	fn := p.clipboard
	p.mu.Unlock()
	if fn != nil {
		fn(s)
	}
}
