// Package desktop is the composition boundary between UI and the operating system.
package desktop

import (
	"context"
	"errors"
	"fmt"
	"github.com/egoist/mygo/plugins/terminal"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/model"
	"nextshell.dev/v2/internal/sshclient"
	"nextshell.dev/v2/internal/storage"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Platform struct {
	store    *storage.Store
	ctx      context.Context
	cancel   context.CancelFunc
	mu       sync.Mutex
	events   []appui.Event
	sessions map[string]*terminal.Terminal
	wake     func()
	closed   bool
	wg       sync.WaitGroup
}

func Open(path string) (*Platform, error) {
	s, e := storage.Open(path)
	if e != nil {
		return nil, e
	}
	ctx, c := context.WithCancel(context.Background())
	return &Platform{store: s, ctx: ctx, cancel: c, sessions: map[string]*terminal.Terminal{}}, nil
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
	return s, e
}
func (p *Platform) SaveConnection(c model.Connection, secret *string) error {
	return p.store.SaveConnection(c, secret)
}
func (p *Platform) SaveFolder(f model.Folder) error          { return p.store.SaveFolder(f) }
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
func (p *Platform) GenerateKey(name string) (model.Key, error) {
	k := model.Key{ID: p.NewID(), Name: name}
	var e error
	k.Private, k.Public, e = sshclient.GenerateKey()
	if e != nil {
		return k, e
	}
	return k, p.SaveKey(k)
}
func (p *Platform) ImportKey(path, pass string) (model.Key, error) {
	b, e := os.ReadFile(path)
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
	req := &appui.Prompt{Title: title, Questions: questions, Echo: echo, Answers: make(chan []string, 1)}
	p.emit(appui.Event{Prompt: req})
	t := time.NewTimer(2 * time.Minute)
	defer t.Stop()
	select {
	case a := <-req.Answers:
		if len(a) != len(questions) {
			return nil, errors.New("用户取消认证")
		}
		return a, nil
	case <-p.ctx.Done():
		return nil, p.ctx.Err()
	case <-t.C:
		return nil, errors.New("认证提示超时")
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
		if e := p.connect(c); e != nil {
			p.emit(appui.Event{Message: e.Error()})
		}
	})
}
func (p *Platform) connect(c model.Connection) error {
	if c.ID != "" {
		stored, e := p.store.Connection(c.ID)
		if e != nil {
			return e
		}
		if c.Secret == "" {
			c.Secret = stored.Secret
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
	o := sshclient.Options{Connection: c, Key: k, Proxy: pr, HostKey: func(addr, fp string) error {
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
		a, e := p.ask("核对服务器指纹后输入 TRUST", []string{addr + "\n" + fp}, []bool{true})
		if e != nil {
			return e
		}
		if a[0] != "TRUST" {
			return errors.New("未信任主机")
		}
		return p.store.PinHost(addr, fp)
	}, Keyboard: func(user, instruction string, questions []string, echo []bool) ([]string, error) {
		return p.ask("SSH "+user+" "+instruction, questions, echo)
	}}
	s, e := sshclient.Dial(p.ctx, o)
	if e != nil {
		return e
	}
	t, e := terminal.New(terminal.Options{Conn: s, NoClipboardWrite: true, ClipboardRead: terminal.ClipboardDeny})
	if e != nil {
		s.Close()
		return e
	}
	p.register(t, c, false)
	return nil
}
func (p *Platform) register(t *terminal.Terminal, c model.Connection, local bool) {
	id := p.NewID()
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		t.Close()
		return
	}
	p.sessions[id] = t
	p.mu.Unlock()
	title := c.Name
	if local {
		title = "本地终端"
	}
	p.emit(appui.Event{Session: &appui.Session{ID: id, Title: title, Terminal: t, Connection: c, Local: local}})
}
func (p *Platform) Local() {
	p.launch(func() {
		t, e := terminal.New(terminal.Options{NoClipboardWrite: true, ClipboardRead: terminal.ClipboardDeny})
		if e != nil {
			p.emit(appui.Event{Message: e.Error()})
			return
		}
		p.register(t, model.Connection{}, true)
	})
}
func (p *Platform) CloseSession(id string) {
	p.mu.Lock()
	t := p.sessions[id]
	delete(p.sessions, id)
	p.mu.Unlock()
	if t != nil {
		t.Close()
	}
}

// Legacy keychain fallback is deliberately not invoked on fresh v2 startup.
// Phase 2 migrator may call this explicitly; no replacement key is silently minted.
func (p *Platform) ReadLegacyDeviceKey() (string, error) {
	return "", fmt.Errorf("UNVERIFIED: legacy keytar service NextShell / account device-key reader is not implemented; import the DB-resident device_key or re-enter credentials")
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
	p.mu.Unlock()
	for _, t := range ts {
		t.Close()
	}
	p.wg.Wait()
	p.store.Close()
}

var _ appui.Platform = (*Platform)(nil)
