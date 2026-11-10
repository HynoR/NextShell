package sshclient

import (
	"context"
	"errors"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"io"
	"sync"
)

func (s *Session) OpenSFTP() (*sftp.Client, error) {
	select {
	case <-s.done:
		return nil, errors.New("SSH session closed")
	default:
	}
	return sftp.NewClient(s.client)
}

// Run uses a separate exec channel; closing/cancelling it never writes into the interactive PTY.
func (s *Session) Run(ctx context.Context, command string, in io.Reader, out, stderr io.Writer) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	ss, e := s.newChannel(ctx)
	if e != nil {
		return e
	}
	defer ss.Close()
	ss.Stdin = in
	ss.Stdout = out
	ss.Stderr = stderr
	stop := context.AfterFunc(ctx, func() { ss.Close() })
	defer stop()
	e = ss.Run(command)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return e
}

type limitedBuffer struct {
	mu    sync.Mutex
	b     []byte
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(p) > b.limit-len(b.b) {
		return 0, errors.New("remote output exceeds limit")
	}
	b.b = append(b.b, p...)
	return len(p), nil
}
func (s *Session) Output(ctx context.Context, command string, limit int) ([]byte, error) {
	b := &limitedBuffer{limit: limit}
	e := s.Run(ctx, command, nil, b, b)
	return b.b, e
}

// newChannel bounds caller latency even if a peer never acknowledges a channel-open.
// A late channel is closed immediately; the parent SSH connection owns the protocol goroutine.
func (s *Session) newChannel(ctx context.Context) (*ssh.Session, error) {
	type result struct {
		s *ssh.Session
		e error
	}
	done := make(chan result)
	go func() {
		ss, e := s.client.NewSession()
		select {
		case done <- result{ss, e}:
		case <-ctx.Done():
			if ss != nil {
				ss.Close()
			}
		}
	}()
	select {
	case r := <-done:
		return r.s, r.e
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (s *Session) OpenSFTPContext(ctx context.Context) (*sftp.Client, func(), error) {
	ss, e := s.newChannel(ctx)
	if e != nil {
		return nil, nil, e
	}
	stop := context.AfterFunc(ctx, func() { ss.Close() })
	fail := func() { stop(); ss.Close() }
	in, e := ss.StdinPipe()
	if e != nil {
		fail()
		return nil, nil, e
	}
	out, e := ss.StdoutPipe()
	if e != nil {
		fail()
		return nil, nil, e
	}
	if e = ss.RequestSubsystem("sftp"); e != nil {
		fail()
		return nil, nil, e
	}
	c, e := sftp.NewClientPipe(out, in)
	if e != nil {
		fail()
		return nil, nil, e
	}
	var once sync.Once
	cleanup := func() { once.Do(func() { stop(); ss.Close(); c.Close() }) }
	return c, cleanup, nil
}
