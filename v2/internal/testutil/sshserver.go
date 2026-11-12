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
	"os/exec"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
)

// RequireLinuxRemote skips t unless this machine can stand in for a Linux
// remote: the server runs exec requests locally, while the product probes
// /proc and uses GNU tools (mv -T, ps --sort) on the remote.
func RequireLinuxRemote(t testing.TB) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skipf("needs a Linux host to act as the remote (/proc, GNU coreutils); running on %s", runtime.GOOS)
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
	once          sync.Once
}

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
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				defer s.conns.Delete(c)
				defer c.Close()
				conn, channels, requests, e := ssh.NewServerConn(c, config)
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
	ctx, cancel := context.WithCancel(s.ctx)
	defer cancel()
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
			s.Execs.Add(1)
			go func() {
				cmd := exec.CommandContext(ctx, "/bin/sh", "-c", v.Command)
				cmd.Dir = s.Root
				cmd.Stdin = ch
				cmd.Stdout = ch
				cmd.Stderr = ch.Stderr()
				status := uint32(0)
				if e := cmd.Run(); e != nil {
					status = 1
				}
				ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{status}))
				ch.Close()
			}()
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
func (s *Server) Close() {
	s.once.Do(func() {
		s.listener.Close()
		<-s.accepted
		s.cancel()
		s.conns.Range(func(k, v any) bool { k.(net.Conn).Close(); return true })
		s.wg.Wait()
	})
}
