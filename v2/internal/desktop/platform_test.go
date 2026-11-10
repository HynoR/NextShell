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
	timer := time.NewTimer(5 * time.Second)
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
		if e != nil || copy.Public != k.Public {
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
	if e != nil || len(snap.History) != 1 || len(snap.Keys) != 4 {
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
	if _, e := p.ReadLegacyDeviceKey(); e == nil {
		t.Fatal("legacy stub must report unsupported")
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
	if !strings.Contains(e.Prompt.Title, "TRUST") {
		t.Fatal("no trust confirmation")
	}
	e.Prompt.Answers <- []string{"TRUST"}
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
