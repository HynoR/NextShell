// Package testutil hosts loopback-only SSH/SFTP fixtures. Paths/commands are supplied only by tests.
package testutil

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"io"
	"net"
	"nextshell.dev/v2/internal/model"
	"nextshell.dev/v2/internal/sshclient"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// linuxTestCmd runs the whole suite on Linux from a non-Linux dev machine (run inside v2/).
const linuxTestCmd = `docker run --rm -v "$PWD":/src -w /src -v nextshell-gomod:/go/pkg/mod -e GOTOOLCHAIN=auto golang:1.27 go test ./... -count=1`

// RequirePOSIXRemote skips t where this machine cannot stand in for a POSIX
// remote. The server runs exec requests with /bin/sh and serves SFTP from a
// directory of the local file system, so on Windows commands exit 127, a
// `C:\...` path joined onto the SFTP root is invalid and POSIX file modes do
// not exist. Tests of remote-side behaviour call it; tests of client-side
// behaviour must work on Windows instead.
func RequirePOSIXRemote(t testing.TB) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("needs a POSIX host to act as the remote (/bin/sh, slash-separated absolute paths, file modes); the in-process server cannot be one on Windows")
	}
}

// RequireLinuxRemote skips t unless this machine can stand in for a Linux
// remote: the server runs exec requests locally, while the product probes
// /proc and uses GNU tools (mv -T, ps --sort) on the remote.
func RequireLinuxRemote(t testing.TB) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skipf("needs a Linux host to act as the remote (/proc, GNU coreutils); running on %s. Run in Docker: %s", runtime.GOOS, linuxTestCmd)
	}
}

type Server struct {
	Root, Address string
	Signer        ssh.Signer
	listener      net.Listener
	conns         sync.Map
	wg            sync.WaitGroup
	accepted      chan struct{}
	ctx           context.Context
	cancel        context.CancelFunc
	Execs, SFTP   atomic.Int64
	// Killed counts exec commands ended by a "signal" request, like sshd's session_signal_req.
	Killed atomic.Int64
	once   sync.Once
	// maxStartups drops a connection at once while that many others have not finished authenticating, like
	// sshd's MaxStartups; authDelay makes the authentication take a while so that they overlap.
	maxStartups atomic.Int32
	authDelay   atomic.Int64
	unauth      atomic.Int32
	Refused     atomic.Int64 // connections dropped because of maxStartups
}

// SetMaxStartups limits the connections that may be authenticating at once (0: no limit).
func (s *Server) SetMaxStartups(n int) { s.maxStartups.Store(int32(n)) }

// SetAuthDelay makes every password check take d.
func (s *Server) SetAuthDelay(d time.Duration) { s.authDelay.Store(int64(d)) }

func New(root string) (*Server, error) {
	_, key, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		return nil, e
	}
	signer, e := ssh.NewSignerFromKey(key)
	if e != nil {
		return nil, e
	}
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		return nil, e
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{Root: root, Address: l.Addr().String(), Signer: signer, listener: l, accepted: make(chan struct{}), ctx: ctx, cancel: cancel}
	config := &ssh.ServerConfig{PasswordCallback: func(meta ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
		time.Sleep(time.Duration(s.authDelay.Load()))
		if meta.User() == "tester" && string(password) == "test-password" {
			return nil, nil
		}
		return nil, errors.New("denied")
	}}
	config.AddHostKey(signer)
	go func() {
		defer close(s.accepted)
		for {
			c, e := l.Accept()
			if e != nil {
				return
			}
			s.conns.Store(c, true)
			if limit := s.maxStartups.Load(); limit > 0 && s.unauth.Load() >= limit {
				s.Refused.Add(1)
				s.conns.Delete(c)
				c.Close() // the client sees EOF in its handshake
				continue
			}
			s.unauth.Add(1)
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				defer s.conns.Delete(c)
				defer c.Close()
				conn, channels, requests, e := ssh.NewServerConn(c, config)
				s.unauth.Add(-1)
				if e != nil {
					return
				}
				defer conn.Close()
				go func() {
					for r := range requests {
						r.Reply(r.Type == "keepalive@openssh.com", nil)
					}
				}()
				for n := range channels {
					if n.ChannelType() != "session" {
						n.Reject(ssh.UnknownChannelType, "session only")
						continue
					}
					ch, req, e := n.Accept()
					if e != nil {
						continue
					}
					s.wg.Add(1)
					go s.serve(ch, req)
				}
			}()
		}
	}()
	return s, nil
}
func (s *Server) serve(ch ssh.Channel, requests <-chan *ssh.Request) {
	defer s.wg.Done()
	defer ch.Close()
	var running atomic.Pointer[os.Process]
	for r := range requests {
		switch r.Type {
		case "pty-req", "window-change":
			if r.WantReply {
				r.Reply(true, nil)
			}
		case "shell":
			r.Reply(true, nil)
			go io.Copy(ch, ch)
		case "subsystem":
			var v struct{ Name string }
			ssh.Unmarshal(r.Payload, &v)
			if v.Name != "sftp" {
				r.Reply(false, nil)
				continue
			}
			r.Reply(true, nil)
			s.SFTP.Add(1)
			server, e := sftp.NewServer(ch, sftp.WithServerWorkingDirectory(s.Root))
			if e != nil {
				return
			}
			go func() { server.Serve(); server.Close(); ch.Close() }()
		case "exec":
			var v struct{ Command string }
			ssh.Unmarshal(r.Payload, &v)
			r.Reply(true, nil)
			// Like sshd, a command outlives its channel: only a "signal" request or Close ends it early.
			cmd := exec.CommandContext(s.ctx, "/bin/sh", "-c", v.Command)
			cmd.Dir = s.Root
			cmd.Stdin = ch
			cmd.Stdout = ch
			cmd.Stderr = ch.Stderr()
			cmd.WaitDelay = time.Second
			// Started before the next request is read, as sshd forks: a "signal" right behind it finds the command.
			started := cmd.Start()
			if started == nil {
				running.Store(cmd.Process)
			}
			s.Execs.Add(1)
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				status := uint32(0)
				if started != nil {
					status = 127
				} else if e := cmd.Wait(); e != nil {
					status = 1
				}
				ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{status}))
				ch.Close()
			}()
		case "signal":
			// Any signal ends the command (the tests only send KILL); like sshd, closing the channel does not.
			if p := running.Load(); p != nil && p.Kill() == nil {
				s.Killed.Add(1)
			}
		default:
			r.Reply(false, nil)
		}
	}
}
func (s *Server) Options() sshclient.Options {
	h, p, _ := net.SplitHostPort(s.Address)
	port, _ := strconv.Atoi(p)
	return sshclient.Options{Connection: model.Connection{Name: "test", Host: h, Port: port, User: "tester", Auth: "password", Secret: "test-password", Fingerprint: ssh.FingerprintSHA256(s.Signer.PublicKey()), Encoding: "utf-8"}}
}

// Drop cuts every connection without a word, like a network failure: commands keep running on the
// "remote" side and no exit status is ever sent.
func (s *Server) Drop() { s.conns.Range(func(k, v any) bool { k.(net.Conn).Close(); return true }) }

func (s *Server) Close() {
	s.once.Do(func() {
		s.listener.Close()
		<-s.accepted
		s.cancel()
		s.conns.Range(func(k, v any) bool { k.(net.Conn).Close(); return true })
		s.wg.Wait()
	})
}
