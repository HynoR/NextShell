package desktop

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"golang.org/x/crypto/ssh"
	"io"
	"net"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/model"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func desktopFixture(t *testing.T) *Platform {
	t.Helper()
	p, e := Open(":memory:")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(p.Shutdown)
	return p
}
func awaitEvent(t *testing.T, p *Platform, pick func(appui.Event) bool) appui.Event {
	t.Helper()
	// The first terminal of a fresh Linux container downloads libghostty-vt: seconds, depending on the network.
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		for _, e := range p.Poll() {
			if pick(e) {
				return e
			}
			if e.Message != "" {
				t.Fatalf("platform error: %s", e.Message)
			}
		}
		select {
		case <-tick.C:
		case <-timer.C:
			t.Fatal("event timeout")
		}
	}
}
func TestNativeLocalTerminal(t *testing.T) {
	// A plain shell: rc files (e.g. oh-my-zsh) re-report $PWD via OSC 7 at each
	// prompt and race with the OSC 7 this test sends.
	t.Setenv("SHELL", "/bin/sh")
	p := desktopFixture(t)
	p.Local()
	e := awaitEvent(t, p, func(e appui.Event) bool { return e.Session != nil })
	s := e.Session
	if !s.Local {
		t.Fatal("not local")
	}

	s.Terminal.Send([]byte("printf '\\033]7;file://localhost/tmp\\007'; printf '\\nnextshell-local-ok\\n'\r"))
	deadline := time.Now().Add(4 * time.Second)
	for !strings.Contains(s.Terminal.Text(), "\nnextshell-local-ok") {
		if time.Now().After(deadline) {
			t.Fatal(s.Terminal.Text())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if s.Terminal.Dir() != "/tmp" {
		t.Fatal(s.Terminal.Dir())
	}
	p.CloseSession(s.ID)
	select {
	case <-s.Terminal.Done():
	case <-time.After(time.Second):
		t.Fatal("local did not exit")
	}
	p.Shutdown()
	p.Shutdown()
}
func TestDesktopKeysProxiesAndHistory(t *testing.T) {
	p := desktopFixture(t)
	for _, kind := range []string{"ed25519", "rsa-2048"} {
		k, e := p.GenerateKey(kind, kind)
		if e != nil {
			t.Fatal(e)
		}
		if !strings.Contains(k.Private, "BEGIN OPENSSH PRIVATE KEY") {
			t.Fatal("not OpenSSH")
		}
		path := filepath.Join(t.TempDir(), "id_test")
		if e = os.WriteFile(path, []byte(k.Private), 0600); e != nil {
			t.Fatal(e)
		}
		copy, e := p.ImportKey(path, "")
		var dup *model.KeyExistsError
		if !errors.As(e, &dup) || copy.ID != k.ID { // the same key again is the one already saved
			t.Fatal(copy, e)
		}
		if e = p.RenameKey(k.ID, "renamed"); e != nil {
			t.Fatal(e)
		}
	}
	bad := filepath.Join(t.TempDir(), "bad-key")
	os.WriteFile(bad, []byte("public/not a key"), 0600)
	if _, e := p.ImportKey(bad, ""); e == nil {
		t.Fatal("bad key imported")
	}
	if e := p.SaveHistory([]string{"uptime"}); e != nil {
		t.Fatal(e)
	}
	snap, e := p.Snapshot()
	if e != nil || len(snap.History) != 1 || len(snap.Keys) != 2 {
		t.Fatal(snap, e)
	}
	for _, k := range snap.Keys {
		if k.Private != "" || k.Passphrase != "" {
			t.Fatal("secret leaked")
		}
	}
	if c := p.Capabilities(); !c.OSC7 || !c.ScreenText || c.OSC133 {
		t.Fatal("OSC capabilities fabricated")
	}
}
func TestPromptCancellationAndShutdown(t *testing.T) {
	p := desktopFixture(t)
	done := make(chan error, 1)
	p.launch(func() { _, e := p.ask("test", []string{"q"}, []bool{false}); done <- e })
	e := awaitEvent(t, p, func(e appui.Event) bool { return e.Prompt != nil })
	e.Prompt.Answers <- nil
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancel accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("cancel blocked")
	}
	select {
	case <-e.Prompt.Done:
	case <-time.After(time.Second):
		t.Fatal("prompt not expired")
	}
	p.launch(func() { _, e := p.ask("test", []string{"q"}, nil); done <- e })
	awaitEvent(t, p, func(e appui.Event) bool { return e.Prompt != nil })
	p.Shutdown()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown blocked")
	}
}
func desktopSSH(t *testing.T) (model.Connection, ssh.PublicKey) {
	t.Helper()
	_, priv, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	signer, e := ssh.NewSignerFromKey(priv)
	if e != nil {
		t.Fatal(e)
	}
	cfg := &ssh.ServerConfig{PasswordCallback: func(c ssh.ConnMetadata, p []byte) (*ssh.Permissions, error) {
		if c.User() == "tester" && bytes.Equal(p, []byte("secret")) {
			return nil, nil
		}
		return nil, errors.New("denied")
	}}
	cfg.AddHostKey(signer)
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	var all sync.Map
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			c, e := l.Accept()
			if e != nil {
				return
			}
			all.Store(c, true)
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer all.Delete(c)
				defer c.Close()
				conn, ch, req, e := ssh.NewServerConn(c, cfg)
				if e != nil {
					return
				}
				defer conn.Close()
				go ssh.DiscardRequests(req)
				for n := range ch {
					channel, requests, e := n.Accept()
					if e != nil {
						return
					}
					go func() {
						defer channel.Close()
						for r := range requests {
							if r.Type == "pty-req" {
								r.Reply(true, nil)
							} else if r.Type == "shell" {
								r.Reply(true, nil)
								go io.Copy(channel, channel)
							} else {
								r.Reply(false, nil)
							}
						}
					}()
				}
			}()
		}
	}()
	t.Cleanup(func() { l.Close(); all.Range(func(k, v any) bool { k.(net.Conn).Close(); return true }); wg.Wait() })
	h, ps, _ := net.SplitHostPort(l.Addr().String())
	port, _ := strconv.Atoi(ps)
	return model.Connection{Name: "test", Host: h, Port: port, User: "tester", Auth: "password", Secret: "secret", Encoding: "utf-8"}, signer.PublicKey()
}
func TestDesktopSSHTrustCloneAndReconnect(t *testing.T) {
	c, _ := desktopSSH(t)
	p := desktopFixture(t)
	p.Connect(c)
	e := awaitEvent(t, p, func(e appui.Event) bool { return e.Prompt != nil })
	if e.Prompt.Kind != appui.PromptHostKey || !strings.HasPrefix(e.Prompt.Fingerprint, "SHA256:") || e.Prompt.Host == "" {
		t.Fatal("no trust confirmation", e.Prompt)
	}
	e.Prompt.Answers <- []string{}
	e = awaitEvent(t, p, func(e appui.Event) bool { return e.Session != nil })
	s := e.Session
	if s.Connection.Secret != "" {
		t.Fatal("session UI metadata leaked stored credentials")
	}
	p.CloneSession(s)
	clone := awaitEvent(t, p, func(e appui.Event) bool { return e.Session != nil }).Session
	if clone.ID == s.ID {
		t.Fatal("clone reused session")
	}
	p.Reconnect(s)
	replacement := awaitEvent(t, p, func(e appui.Event) bool { return e.Session != nil }).Session
	if replacement.Replaces != s.ID {
		t.Fatal("reconnect not targeted")
	}
	p.CloseSession(s.ID)
	p.CloseSession(clone.ID)
	p.CloseSession(replacement.ID)
}

func TestTerminalCapabilityRejectsClosedSession(t *testing.T) {
	p := desktopFixture(t)
	if e := p.Send("missing", "uptime\r"); e == nil {
		t.Fatal("closed send accepted")
	}
	if p.ScreenText("missing") != "" || !p.Exited("missing") {
		t.Fatal("closed capability incorrect")
	}
}

func TestDesktopSSHPasswordPromptRememberAndChangedHostKey(t *testing.T) {
	c, _ := desktopSSH(t)
	p := desktopFixture(t)
	c.ID = p.NewID()
	c.Secret = ""
	if e := p.SaveConnection(c, nil); e != nil {
		t.Fatal(e)
	}
	if e := p.store.PinHost(c.Address(), "SHA256:old"); e != nil {
		t.Fatal(e)
	}
	p.Connect(c)
	e := awaitEvent(t, p, func(e appui.Event) bool { return e.Prompt != nil })
	if e.Prompt.Kind != appui.PromptPassword || !e.Prompt.CanRemember {
		t.Fatal("password prompt", e.Prompt)
	}
	e.Prompt.Remember = true
	e.Prompt.Answers <- []string{"secret"}
	e = awaitEvent(t, p, func(e appui.Event) bool { return e.Prompt != nil })
	if e.Prompt.Kind != appui.PromptHostKeyChanged || e.Prompt.OldFingerprint != "SHA256:old" || e.Prompt.Fingerprint == "SHA256:old" {
		t.Fatal("changed key prompt", e.Prompt)
	}
	e.Prompt.Answers <- []string{}
	s := awaitEvent(t, p, func(e appui.Event) bool { return e.Session != nil }).Session
	defer p.CloseSession(s.ID)
	if pin, _ := p.store.HostPin(c.Address()); pin == "SHA256:old" {
		t.Fatal("pin not replaced")
	}
	if stored, _ := p.store.Connection(c.ID); stored.Secret != "secret" {
		t.Fatal("password not remembered")
	}
}
func TestDesktopSSHChangedHostKeyDeclined(t *testing.T) {
	c, _ := desktopSSH(t)
	p := desktopFixture(t)
	if e := p.store.PinHost(c.Address(), "SHA256:old"); e != nil {
		t.Fatal(e)
	}
	p.Connect(c)
	e := awaitEvent(t, p, func(e appui.Event) bool { return e.Prompt != nil })
	e.Prompt.Answers <- nil
	for {
		ev := awaitEventOrMessage(t, p)
		if ev.Message != "" {
			break
		}
	}
	if pin, _ := p.store.HostPin(c.Address()); pin != "SHA256:old" {
		t.Fatal("declined change replaced pin")
	}
}
func awaitEventOrMessage(t *testing.T, p *Platform) appui.Event {
	t.Helper()
	until := time.Now().Add(5 * time.Second)
	for time.Now().Before(until) {
		for _, e := range p.Poll() {
			if e.Message != "" || e.Session != nil {
				return e
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("event timeout")
	return appui.Event{}
}
func TestDesktopSSHWrongPasswordAsksAgainAtMostThreeTimes(t *testing.T) {
	c, _ := desktopSSH(t)
	p := desktopFixture(t)
	c.Secret = "wrong"
	p.Connect(c)
	e := awaitEvent(t, p, func(e appui.Event) bool { return e.Prompt != nil })
	e.Prompt.Answers <- []string{} // trust the host key
	e = awaitEvent(t, p, func(e appui.Event) bool { return e.Prompt != nil })
	if e.Prompt.Kind != appui.PromptPassword || !strings.Contains(e.Prompt.Title, "密码错误") {
		t.Fatal("no retry prompt", e.Prompt)
	}
	e.Prompt.Answers <- []string{"secret"}
	s := awaitEvent(t, p, func(e appui.Event) bool { return e.Session != nil }).Session
	p.CloseSession(s.ID)

	p.Connect(c)
	for range 2 {
		e = awaitEvent(t, p, func(e appui.Event) bool { return e.Prompt != nil })
		e.Prompt.Answers <- []string{"still wrong"}
	}
	if ev := awaitEventOrMessage(t, p); ev.Message == "" || !strings.Contains(ev.Message, "unable to authenticate") {
		t.Fatal("third failure did not end the attempt", ev)
	}
}

// TestCancelConnect stops a connect in its handshake and at its host key dialog: the network connection
// closes, the dialog goes, no failure is reported and nothing is left running.
func TestCancelConnect(t *testing.T) {
	p := desktopFixture(t)
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	h, ps, _ := net.SplitHostPort(l.Addr().String())
	port, _ := strconv.Atoi(ps)
	silent := model.Connection{ID: p.NewID(), Name: "silent", Host: h, Port: port, User: "u", Auth: "password", Encoding: "utf-8"}
	secret := "x"
	if e := p.SaveConnection(silent, &secret); e != nil {
		t.Fatal(e)
	}
	p.Connect(silent)
	c, e := l.Accept() // the server never answers: the handshake waits
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	p.CancelConnect(silent.ID)
	c.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, e := io.Copy(io.Discard, c); e != nil {
		t.Fatal("the connection stayed open", e)
	}

	conn, _ := desktopSSH(t)
	conn.ID = p.NewID()
	if e := p.SaveConnection(conn, &conn.Secret); e != nil {
		t.Fatal(e)
	}
	p.Connect(conn)
	ask := awaitEvent(t, p, func(e appui.Event) bool { return e.Prompt != nil })
	p.CancelConnect(conn.ID)
	select {
	case <-ask.Prompt.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("the host key dialog stayed")
	}
	waitIdle(t, p)
	for _, e := range p.Poll() {
		if e.Failed != "" || e.Message != "" || e.Session != nil {
			t.Fatalf("cancelled connect reported %+v", e)
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.connects) != 0 || len(p.sessions) != 0 {
		t.Fatal("left behind", p.connects, p.sessions)
	}
}

// waitIdle waits for the platform's background work to end.
func waitIdle(t *testing.T, p *Platform) {
	t.Helper()
	done := make(chan struct{})
	go func() { p.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a connect is still running")
	}
}
