package sshclient

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"io"
	"net"
	"nextshell.dev/v2/internal/model"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type testSSH struct {
	received    chan []byte
	addr        string
	signer      ssh.Signer
	private     ed25519.PrivateKey
	key         model.Key
	keep        chan bool
	resize      chan [2]int
	conns       sync.Map
	listener    net.Listener
	wg          sync.WaitGroup
	noKeepReply bool
	hangSetup   bool
}

func newServer(t *testing.T, noKeep, hang bool) *testSSH {
	t.Helper()
	_, priv, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	signer, e := ssh.NewSignerFromKey(priv)
	if e != nil {
		t.Fatal(e)
	}
	pr, pub, e := GenerateKey()
	if e != nil {
		t.Fatal(e)
	}
	ps, e := ssh.ParsePrivateKey([]byte(pr))
	if e != nil {
		t.Fatal(e)
	}
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	s := &testSSH{received: make(chan []byte, 16), addr: l.Addr().String(), signer: signer, private: priv, key: model.Key{ID: "test-key", Private: pr, Public: pub}, keep: make(chan bool, 8), resize: make(chan [2]int, 8), listener: l, noKeepReply: noKeep, hangSetup: hang}
	cfg := &ssh.ServerConfig{PasswordCallback: func(c ssh.ConnMetadata, p []byte) (*ssh.Permissions, error) {
		if c.User() == "tester" && string(p) == "secret" {
			return nil, nil
		}
		return nil, errors.New("denied")
	}, PublicKeyCallback: func(c ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
		if c.User() == "tester" && bytes.Equal(k.Marshal(), ps.PublicKey().Marshal()) {
			return nil, nil
		}
		return nil, errors.New("denied")
	}, KeyboardInteractiveCallback: func(c ssh.ConnMetadata, q ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
		a, e := q(c.User(), "OTP", []string{"Code:", "Name:"}, []bool{false, true})
		if e == nil && len(a) == 2 && a[0] == "123456" && a[1] == "tester" {
			return nil, nil
		}
		return nil, errors.New("denied")
	}}
	cfg.AddHostKey(signer)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			c, e := l.Accept()
			if e != nil {
				return
			}
			s.conns.Store(c, true)
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				defer s.conns.Delete(c)
				defer c.Close()
				cc, ch, req, e := ssh.NewServerConn(c, cfg)
				if e != nil {
					return
				}
				defer cc.Close()
				go func() {
					for r := range req {
						if r.Type == "keepalive@openssh.com" {
							select {
							case s.keep <- true:
							default:
							}
							if !s.noKeepReply {
								r.Reply(true, nil)
							}
						} else {
							r.Reply(false, nil)
						}
					}
				}()
				for n := range ch {
					if n.ChannelType() != "session" {
						n.Reject(ssh.UnknownChannelType, "session only")
						continue
					}
					channel, requests, e := n.Accept()
					if e != nil {
						return
					}
					go func() {
						defer channel.Close()
						for r := range requests {
							switch r.Type {
							case "pty-req":
								if !s.hangSetup {
									r.Reply(true, nil)
								}
							case "window-change":
								var v struct{ Cols, Rows, Width, Height uint32 }
								ssh.Unmarshal(r.Payload, &v)
								select {
								case s.resize <- [2]int{int(v.Cols), int(v.Rows)}:
								default:
								}
							case "shell":
								r.Reply(true, nil)
								go func() {
									b := make([]byte, 4096)
									for {
										n, e := channel.Read(b)
										if n > 0 {
											select {
											case s.received <- append([]byte{}, b[:n]...):
											default:
											}
											if _, err := channel.Write(b[:n]); err != nil {
												return
											}
										}
										if e != nil {
											return
										}
									}
								}()
							default:
								r.Reply(false, nil)
							}
						}
					}()
				}
			}()
		}
	}()
	t.Cleanup(func() {
		l.Close()
		s.conns.Range(func(k, v any) bool { k.(net.Conn).Close(); return true })
		s.wg.Wait()
	})
	return s
}
func (s *testSSH) options() Options {
	h, p, _ := net.SplitHostPort(s.addr)
	port, _ := strconv.Atoi(p)
	return Options{Connection: model.Connection{Name: "test", Host: h, Port: port, User: "tester", Auth: "password", Secret: "secret", Encoding: "utf-8", Fingerprint: Fingerprint(s.signer.PublicKey().Marshal())}, Key: s.key, Timeout: 2 * time.Second}
}
func echo(t *testing.T, s *Session, text string) {
	t.Helper()
	result := make(chan error, 1)
	go func() {
		_, e := s.Write([]byte(text))
		if e == nil {
			b := make([]byte, len(text))
			_, e = io.ReadFull(s, b)
			if e == nil && string(b) != text {
				e = fmt.Errorf("echo=%q", b)
			}
		}
		result <- e
	}()
	select {
	case e := <-result:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(4 * time.Second):
		s.Close()
		t.Fatal("echo timeout")
	}
}
func TestSSHAuthentication(t *testing.T) {
	for _, mode := range []string{"password", "key", "agent", "keyboard"} {
		t.Run(mode, func(t *testing.T) {
			srv := newServer(t, false, false)
			o := srv.options()
			o.Connection.Auth = mode
			if mode == "key" {
				o.Connection.KeyID = srv.key.ID
			}
			if mode == "agent" {
				key, e := ssh.ParseRawPrivateKey([]byte(srv.key.Private))
				if e != nil {
					t.Fatal(e)
				}
				ring := agent.NewKeyring()
				if e = ring.Add(agent.AddedKey{PrivateKey: key}); e != nil {
					t.Fatal(e)
				}
				sock := filepath.Join(t.TempDir(), "agent.sock")
				l, e := net.Listen("unix", sock)
				if e != nil {
					t.Fatal(e)
				}
				t.Cleanup(func() { l.Close() })
				go func() {
					c, e := l.Accept()
					if e == nil {
						defer c.Close()
						agent.ServeAgent(ring, c)
					}
				}()
				o.AgentSocket = sock
			}
			o.Keyboard = func(user, instruction string, q []string, echo []bool) ([]string, error) {
				if user != "tester" || len(q) != 2 || echo[0] || !echo[1] {
					return nil, errors.New("bad prompt")
				}
				return []string{"123456", "tester"}, nil
			}
			s, e := Dial(context.Background(), o)
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			echo(t, s, "hello 中文\r\n")
			if e = s.Resize(120, 40); e != nil {
				t.Fatal(e)
			}
			select {
			case got := <-srv.resize:
				if got != [2]int{120, 40} {
					t.Fatal(got)
				}
			case <-time.After(time.Second):
				t.Fatal("resize missing")
			}
			s.Close()
			s.Close()
			select {
			case <-s.Done():
			case <-time.After(time.Second):
				t.Fatal("not closed")
			}
		})
	}
}
func TestSSHRejectsBadCredentials(t *testing.T) {
	s := newServer(t, false, false)
	o := s.options()
	o.Connection.Secret = "wrong"
	if c, e := Dial(context.Background(), o); e == nil {
		c.Close()
		t.Fatal("accepted bad password")
	}
}
func TestSSHHostVerification(t *testing.T) {
	for _, mode := range []string{"unknown", "mismatch", "callback-reject", "callback-accept"} {
		t.Run(mode, func(t *testing.T) {
			srv := newServer(t, false, false)
			o := srv.options()
			o.Connection.Fingerprint = ""
			switch mode {
			case "mismatch":
				o.Connection.Fingerprint = "SHA256:wrong"
			case "callback-reject":
				o.HostKey = func(a, f string) error { return errors.New("rejected") }
			case "callback-accept":
				o.HostKey = func(a, f string) error {
					if a != srv.addr || f != Fingerprint(srv.signer.PublicKey().Marshal()) {
						return errors.New("wrong host")
					}
					return nil
				}
			}
			s, e := Dial(context.Background(), o)
			if mode == "callback-accept" {
				if e != nil {
					t.Fatal(e)
				}
				s.Close()
			} else if e == nil {
				s.Close()
				t.Fatal("untrusted host accepted")
			}
		})
	}
}
func TestSSHCancelledHandshake(t *testing.T) {
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	go func() {
		c, e := l.Accept()
		if e == nil {
			defer c.Close()
			io.Copy(io.Discard, c)
		}
	}()
	h, p, _ := net.SplitHostPort(l.Addr().String())
	port, _ := strconv.Atoi(p)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(40*time.Millisecond, cancel)
	start := time.Now()
	_, e = Dial(ctx, Options{Connection: model.Connection{Name: "x", Host: h, Port: port, User: "x", Auth: "password"}, Timeout: time.Second})
	if e == nil || time.Since(start) > 500*time.Millisecond {
		t.Fatalf("cancel failed: %v", e)
	}
}
func TestSSHSetupTimeout(t *testing.T) {
	srv := newServer(t, false, true)
	o := srv.options()
	o.Timeout = 100 * time.Millisecond
	start := time.Now()
	s, e := Dial(context.Background(), o)
	if e == nil {
		s.Close()
		t.Fatal("expected PTY timeout")
	}
	if time.Since(start) > time.Second {
		t.Fatal("setup hung")
	}
}

// Time a person spends answering a host-key or one-time-code dialog is not the network's: it must not eat
// the connection timeout, or the handshake dies under a dialog that is still open.
func TestSSHDialTimeoutDoesNotCountPrompts(t *testing.T) {
	for _, mode := range []string{"host key", "keyboard-interactive"} {
		t.Run(mode, func(t *testing.T) {
			srv := newServer(t, false, false)
			o := srv.options()
			o.Timeout = 300 * time.Millisecond
			answers := 0
			think := func() { answers++; time.Sleep(900 * time.Millisecond) }
			if mode == "host key" {
				o.Connection.Fingerprint = ""
				o.HostKey = func(address, fp string) error { think(); return nil }
			} else {
				o.Connection.Auth = "keyboard"
				o.Keyboard = func(user, instruction string, q []string, echo []bool) ([]string, error) {
					think()
					return []string{"123456", "tester"}, nil
				}
			}
			s, e := Dial(context.Background(), o)
			if e != nil || answers != 1 {
				t.Fatal(e, answers)
			}
			s.Close()
		})
	}
	// The budget still bounds everything else, and the error says so the way it always did.
	srv := newServer(t, false, true)
	o := srv.options()
	o.Timeout = 150 * time.Millisecond
	o.Connection.Fingerprint = ""
	o.HostKey = func(string, string) error { time.Sleep(400 * time.Millisecond); return nil }
	start := time.Now()
	_, e := Dial(context.Background(), o)
	if e == nil || !strings.Contains(e.Error(), "i/o timeout") || !errors.Is(e, context.DeadlineExceeded) || time.Since(start) > 2*time.Second {
		t.Fatal(e, time.Since(start))
	}
}
func TestSSHKeepalive(t *testing.T) {
	for _, noReply := range []bool{false, true} {
		t.Run(fmt.Sprint("silent=", noReply), func(t *testing.T) {
			srv := newServer(t, noReply, false)
			o := srv.options()
			o.Connection.Keepalive = 1
			o.Timeout = 250 * time.Millisecond
			s, e := Dial(context.Background(), o)
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			select {
			case <-srv.keep:
			case <-time.After(2 * time.Second):
				t.Fatal("no keepalive")
			}
			if noReply {
				select {
				case <-s.Done():
				case <-time.After(time.Second):
					t.Fatal("silent server did not time out")
				}
			} else {
				echo(t, s, "still alive")
			}
		})
	}
}
func TestSSHEncoding(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		raw        []byte
	}{{"gb18030", "中文", []byte{0xd6, 0xd0, 0xce, 0xc4}}, {"gbk", "中文", []byte{0xd6, 0xd0, 0xce, 0xc4}}, {"big5", "中文", []byte{0xa4, 0xa4, 0xa4, 0xe5}}, {"shift_jis", "日本", []byte{0x93, 0xfa, 0x96, 0x7b}}} {
		t.Run(tc.name, func(t *testing.T) {
			srv := newServer(t, false, false)
			o := srv.options()
			o.Connection.Encoding = tc.name
			s, e := Dial(context.Background(), o)
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			echo(t, s, tc.text)
			var got []byte
			for len(got) < len(tc.raw) {
				select {
				case b := <-srv.received:
					got = append(got, b...)
				case <-time.After(time.Second):
					t.Fatal("no wire bytes")
				}
			}
			if !bytes.Equal(got, tc.raw) {
				t.Fatalf("wire encoding %x != %x", got, tc.raw)
			}
		})
	}
}

// Fingerprint cases are ported from packages/ssh/src/host-fingerprint.spec.ts.
func TestFingerprintCompatibility(t *testing.T) {
	key := []byte("ssh-ed25519 AAAA-fake-host-key-bytes")
	sha := sha256.Sum256(key)
	md := md5.Sum(key)
	m := hex.EncodeToString(md[:])
	var colon []string
	for i := 0; i < len(m); i += 2 {
		colon = append(colon, m[i:i+2])
	}
	cases := []string{Fingerprint(key), "sha256:" + base64.StdEncoding.EncodeToString(sha[:]), hex.EncodeToString(sha[:]), m, strings.Join(colon, ":"), "  " + Fingerprint(key) + "  "}
	for i, fp := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			if !MatchFingerprint(fp, key) {
				t.Fatal(fp)
			}
			if MatchFingerprint(fp, []byte("other key")) {
				t.Fatal("MITM accepted")
			}
		})
	}
	for _, fp := range []string{"", "   ", "SHA256:bad"} {
		if MatchFingerprint(fp, key) {
			t.Fatal(fp)
		}
	}
}
func TestKeyMaterial(t *testing.T) {
	private, public, e := GenerateKey()
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(private, "BEGIN OPENSSH PRIVATE KEY") {
		t.Fatal("not OpenSSH")
	}
	got, e := InspectKey(private, "")
	if e != nil || got != public {
		t.Fatalf("%v %q", e, got)
	}
	p2, pub2, e := GenerateKey()
	if e != nil || p2 == private || pub2 == public {
		t.Fatal("not unique")
	}
	for i, v := range []string{public, private[:120], "hello world", ""} {
		t.Run(fmt.Sprint("reject", i), func(t *testing.T) {
			if _, e := InspectKey(v, ""); e == nil {
				t.Fatal("accepted invalid key")
			}
		})
	}
	signer, e := ssh.ParseRawPrivateKey([]byte(private))
	if e != nil {
		t.Fatal(e)
	}
	block, e := ssh.MarshalPrivateKeyWithPassphrase(signer, "enc@test", []byte("correct-horse"))
	if e != nil {
		t.Fatal(e)
	}
	enc := string(pem.EncodeToMemory(block))
	if _, e = InspectKey(enc, "correct-horse"); e != nil {
		t.Fatal(e)
	}
	for _, pass := range []string{"wrong", ""} {
		if _, e = InspectKey(enc, pass); e == nil {
			t.Fatal("accepted wrong passphrase")
		}
	}
}

func TestRSAKeyGeneration(t *testing.T) {
	pr, pub, e := GenerateKeyWithType("rsa-2048", "nextshell@test")
	if e != nil {
		t.Fatal(e)
	}
	k, e := ssh.ParseRawPrivateKey([]byte(pr))
	if e != nil {
		t.Fatal(e)
	}
	rsaKey, ok := k.(*rsa.PrivateKey)
	if !ok || rsaKey.N.BitLen() != 2048 {
		t.Fatal("wrong RSA modulus")
	}
	if !strings.HasSuffix(strings.TrimSpace(pub), "nextshell@test") {
		t.Fatal("comment lost")
	}
	if _, _, e = GenerateKeyWithType("unsupported", ""); e == nil {
		t.Fatal("unsupported key accepted")
	}
}
