package sshclient

import (
	"context"
	"errors"
	"fmt"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/text/transform"
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
//
// sshd does not end a PTY-less command when its channel closes, and keeps the channel half-open until
// the command exits, so waiting for the channel would hold the caller for as long as the remote runs.
// When ctx ends Run therefore asks the server to SIGKILL the command, closes its side and returns
// ctx.Err() at once; out and stderr are not written after Run returns.
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
	var guards []*detachable
	for _, w := range []struct {
		dst *io.Writer
		src io.Writer
	}{{&ss.Stdout, out}, {&ss.Stderr, stderr}} {
		if w.src == nil {
			continue
		}
		g := &detachable{w: w.src}
		guards = append(guards, g)
		*w.dst = g
	}
	defer func() {
		for _, g := range guards {
			g.detach()
		}
	}()
	if e = ss.Start(command); e != nil {
		return e
	}
	done := make(chan error, 1)
	go func() { done <- ss.Wait() }()
	select {
	case e = <-done:
		return e
	case <-ctx.Done():
		killAndClose(ss)
		return ctx.Err()
	}
}

// killAndClose ends a channel that is being abandoned: the signal goes first (the channel keeps its order),
// and a server that ignores it still sees the channel close.
func killAndClose(ss *ssh.Session) {
	ss.Signal(ssh.SIGKILL)
	ss.Close()
}

// detachable forwards to w until detach, after which writes are accepted and dropped: the SSH library's
// copying goroutines may outlive an abandoned channel, and w belongs to the caller again.
type detachable struct {
	mu sync.Mutex
	w  io.Writer
}

func (d *detachable) Write(p []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.w == nil {
		return len(p), nil
	}
	return d.w.Write(p)
}
func (d *detachable) detach() { d.mu.Lock(); d.w = nil; d.mu.Unlock() }

// RunText is Run for text a person or a model reads: the command goes out in the host's encoding and
// stdout and stderr come back decoded from it, like the interactive session. Run itself stays byte-exact
// for streams such as tar. A command the encoding cannot represent is refused rather than altered.
func (s *Session) RunText(ctx context.Context, command string, out, stderr io.Writer) error {
	if s.enc == nil {
		return s.Run(ctx, command, nil, out, stderr)
	}
	command, e := s.enc.NewEncoder().String(command)
	if e != nil {
		return fmt.Errorf("command has characters the host encoding cannot represent: %w", e)
	}
	decoded := func(w io.Writer) *transform.Writer { return transform.NewWriter(w, s.enc.NewDecoder()) }
	o, r := decoded(out), decoded(stderr)
	e = s.Run(ctx, command, nil, o, r)
	o.Close() // flush a character the output ended in the middle of
	r.Close()
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
	if e := ctx.Err(); e != nil {
		return nil, e
	}
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
	// No signal here: sshd refuses them for subsystems, and sftp-server exits when its stdin closes.
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
