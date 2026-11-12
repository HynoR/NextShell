//go:build interop

package interop

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"nextshell.dev/v2/internal/sshclient"
)

// serveAgent runs an in-process ssh-agent holding keys on a unix socket (a stand-in for SSH_AUTH_SOCK).
func serveAgent(t *testing.T, keys ...string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "nsa") // short path: unix sockets are limited to ~100 bytes
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	keyring := agent.NewKeyring()
	for _, k := range keys {
		raw, err := ssh.ParseRawPrivateKey([]byte(k))
		if err != nil {
			t.Fatal(err)
		}
		if err = keyring.Add(agent.AddedKey{PrivateKey: raw}); err != nil {
			t.Fatal(err)
		}
	}
	sock := filepath.Join(dir, "agent.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	t.Cleanup(func() { l.Close(); wg.Wait() })
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); agent.ServeAgent(keyring, c) }()
		}
	}()
	return sock
}

func whoami(t *testing.T, s *sshclient.Session) string {
	t.Helper()
	return strings.TrimSpace(Sh(t, s, "id -un"))
}

func TestAuthMatrix(t *testing.T) {
	for _, v := range []Variant{Debian, Ubuntu, Alpine} {
		t.Run(v.Name, func(t *testing.T) {
			c := Shared(t, v)
			ok := func(t *testing.T, o sshclient.Options) {
				t.Helper()
				if got := whoami(t, Dial(t, o)); got != o.Connection.User {
					t.Fatalf("logged in as %q, want %q", got, o.Connection.User)
				}
			}
			t.Run("password", func(t *testing.T) { ok(t, c.Options()) })
			t.Run("ed25519", func(t *testing.T) { ok(t, c.KeyOptions(Keys.Ed25519)) })
			t.Run("rsa", func(t *testing.T) { ok(t, c.KeyOptions(Keys.RSA)) })
			t.Run("encrypted", func(t *testing.T) { ok(t, c.KeyOptions(Keys.Encrypted)) })
			t.Run("encrypted-wrong-passphrase", func(t *testing.T) {
				o := c.KeyOptions(Keys.Encrypted)
				o.Key.Passphrase = "nope"
				_, err := sshclient.Dial(context.Background(), o)
				if err == nil || !strings.Contains(err.Error(), "passphrase") && !strings.Contains(err.Error(), "password") {
					t.Fatalf("want a passphrase error, got %v", err)
				}
				o.Key.Passphrase = ""
				if _, err = sshclient.Dial(context.Background(), o); err == nil || !strings.Contains(err.Error(), "passphrase") {
					t.Fatalf("missing passphrase: want a passphrase error, got %v", err)
				}
			})
			t.Run("agent", func(t *testing.T) {
				o := c.Options()
				o.Connection.Auth, o.Connection.Secret = "agent", ""
				o.AgentSocket = serveAgent(t, Keys.RSA.Private, Keys.Ed25519.Private)
				ok(t, o)
			})
			t.Run("agent-without-matching-key", func(t *testing.T) {
				priv, _, _ := sshclient.GenerateKey()
				o := c.Options()
				o.Connection.Auth, o.Connection.Secret = "agent", ""
				o.AgentSocket = serveAgent(t, priv)
				if _, err := sshclient.Dial(context.Background(), o); err == nil {
					t.Fatal("agent with an unknown key logged in")
				}
			})
			t.Run("wrong-password", func(t *testing.T) {
				o := c.Options()
				o.Connection.Secret = "not-the-password"
				start := time.Now()
				_, err := sshclient.Dial(context.Background(), o)
				if err == nil || !strings.Contains(err.Error(), "unable to authenticate") {
					t.Fatalf("want auth failure, got %v", err)
				}
				if d := time.Since(start); d > 10*time.Second {
					t.Fatalf("auth failure took %v", d)
				}
			})
			t.Run("unknown-user", func(t *testing.T) {
				o := c.Options()
				o.Connection.User = "nobody-here"
				if _, err := sshclient.Dial(context.Background(), o); err == nil {
					t.Fatal("unknown user logged in")
				}
			})
			t.Run("untrusted-key-for-other-user", func(t *testing.T) {
				o := c.KeyOptions(Keys.Ed25519)
				o.Connection.User = "root"
				if _, err := sshclient.Dial(context.Background(), o); err == nil {
					t.Fatal("root login accepted")
				}
			})
		})
	}
}

// prompter answers keyboard-interactive prompts by substring and records what the server asked.
type prompter struct {
	mu      sync.Mutex
	asked   []string
	answers map[string]string
}

func (p *prompter) challenge(user, instruction string, questions []string, echo []bool) ([]string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, len(questions))
	for i, q := range questions {
		p.asked = append(p.asked, q)
		for k, a := range p.answers {
			if strings.Contains(strings.ToLower(q), k) {
				out[i] = a
			}
		}
	}
	return out, nil
}

func TestKeyboardInteractivePAM(t *testing.T) {
	for _, v := range []Variant{Debian, Ubuntu} {
		t.Run(v.Name, func(t *testing.T) {
			c := Shared(t, v)
			t.Run("password-via-pam", func(t *testing.T) {
				p := &prompter{answers: map[string]string{"password": Password}}
				o := c.Options()
				o.Connection.Auth, o.Connection.Secret, o.Keyboard = "keyboard", "", p.challenge
				if got := whoami(t, Dial(t, o)); got != "tester" {
					t.Fatal(got)
				}
				if len(p.asked) != 1 || !strings.Contains(p.asked[0], "assword") {
					t.Fatalf("prompts: %q", p.asked)
				}
			})
			t.Run("wrong-password-via-pam", func(t *testing.T) {
				p := &prompter{answers: map[string]string{"password": "bad"}}
				o := c.Options()
				o.Connection.Auth, o.Connection.Secret, o.Keyboard = "keyboard", "", p.challenge
				if _, err := sshclient.Dial(context.Background(), o); err == nil {
					t.Fatal("wrong PAM password accepted")
				}
			})
			t.Run("2fa-password-plus-code", func(t *testing.T) {
				p := &prompter{answers: map[string]string{"password": Password, "verification": "11112222"}}
				o := c.Options()
				o.Connection.User = "otpuser"
				o.Connection.Auth, o.Connection.Secret, o.Keyboard = "keyboard", "", p.challenge
				if got := whoami(t, Dial(t, o)); got != "otpuser" {
					t.Fatal(got)
				}
				if len(p.asked) != 2 {
					t.Fatalf("want password + code prompts, got %q", p.asked)
				}
				t.Logf("2FA prompts: %q", p.asked)
				// scratch codes are single use
				if _, err := sshclient.Dial(context.Background(), o); err == nil {
					t.Fatal("reused one-time code accepted")
				}
			})
			t.Run("2fa-wrong-code", func(t *testing.T) {
				p := &prompter{answers: map[string]string{"password": Password, "verification": "00000000"}}
				o := c.Options()
				o.Connection.User = "otpuser"
				o.Connection.Auth, o.Connection.Secret, o.Keyboard = "keyboard", "", p.challenge
				if _, err := sshclient.Dial(context.Background(), o); err == nil {
					t.Fatal("wrong OTP accepted")
				}
			})
			t.Run("2fa-password-auth-refused", func(t *testing.T) {
				o := c.Options()
				o.Connection.User = "otpuser"
				if _, err := sshclient.Dial(context.Background(), o); err == nil {
					t.Fatal("password-only login accepted for an AuthenticationMethods=keyboard-interactive user")
				}
			})
			t.Run("keyboard-handler-cancel", func(t *testing.T) {
				// the user dismisses the prompt: Dial must fail promptly, not hang
				o := c.Options()
				o.Connection.Auth, o.Connection.Secret = "keyboard", ""
				o.Keyboard = func(string, string, []string, []bool) ([]string, error) { return nil, context.Canceled }
				start := time.Now()
				if _, err := sshclient.Dial(context.Background(), o); err == nil || time.Since(start) > 5*time.Second {
					t.Fatalf("err=%v after %v", err, time.Since(start))
				}
			})
		})
	}
	t.Run("alpine-has-no-pam", func(t *testing.T) {
		c := Shared(t, Alpine)
		p := &prompter{answers: map[string]string{"password": Password}}
		o := c.Options()
		o.Connection.Auth, o.Connection.Secret, o.Keyboard = "keyboard", "", p.challenge
		_, err := sshclient.Dial(context.Background(), o)
		t.Logf("keyboard-interactive on a non-PAM sshd: %v (prompts %q)", err, p.asked)
		if err == nil {
			t.Fatal("expected failure without PAM")
		}
	})
}

func TestAgentForwardingNotImplemented(t *testing.T) {
	// sshclient only authenticates *with* an agent; it never requests agent forwarding (auth-agent-req),
	// so `ssh-add -l` on the remote cannot see the local agent. This documents the gap.
	c := Shared(t, Debian)
	o := c.Options()
	o.Connection.Auth, o.Connection.Secret = "agent", ""
	o.AgentSocket = serveAgent(t, Keys.Ed25519.Private)
	s := Dial(t, o)
	out, _ := TrySh(s, `echo "SSH_AUTH_SOCK=[$SSH_AUTH_SOCK]"`, 10*time.Second)
	if !strings.Contains(out, "SSH_AUTH_SOCK=[]") {
		t.Fatalf("remote unexpectedly has an agent socket: %q (forwarding was implemented; replace this test)", out)
	}
}
