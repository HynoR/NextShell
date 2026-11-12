//go:build interop

package interop

import (
	"context"
	"io"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"nextshell.dev/v2/internal/sftpfs"
	"nextshell.dev/v2/internal/sshclient"
)

// tcpProxy forwards to target and counts bytes; Blackhole makes it swallow traffic silently in both
// directions while keeping the TCP connections open (a NAT entry that expired, a dead Wi-Fi).
type tcpProxy struct {
	l          net.Listener
	target     string
	bytes      atomic.Int64
	blackholed atomic.Bool
	wg         sync.WaitGroup
}

func newTCPProxy(t *testing.T, target string) *tcpProxy {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &tcpProxy{l: l, target: target}
	t.Cleanup(func() { l.Close(); p.wg.Wait() })
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			up, err := net.Dial("tcp", target)
			if err != nil {
				c.Close()
				continue
			}
			t.Cleanup(func() { c.Close(); up.Close() })
			for _, pair := range [][2]net.Conn{{c, up}, {up, c}} {
				p.wg.Add(1)
				go func(src, dst net.Conn) {
					defer p.wg.Done()
					buf := make([]byte, 32<<10)
					for {
						n, err := src.Read(buf)
						if n > 0 {
							p.bytes.Add(int64(n))
							if !p.blackholed.Load() {
								dst.Write(buf[:n])
							}
						}
						if err != nil {
							dst.Close()
							return
						}
					}
				}(pair[0], pair[1])
			}
		}
	}()
	return p
}
func (p *tcpProxy) Port() int {
	_, s, _ := net.SplitHostPort(p.l.Addr().String())
	n, _ := strconv.Atoi(s)
	return n
}

func viaProxy(o sshclient.Options, p *tcpProxy) sshclient.Options {
	o.Connection.Port = p.Port()
	return o
}

func waitDone(s *sshclient.Session, d time.Duration) (time.Duration, bool) {
	start := time.Now()
	select {
	case <-s.Done():
		return time.Since(start), true
	case <-time.After(d):
		return d, false
	}
}

func TestKeepaliveIdle(t *testing.T) {
	c := Shared(t, Debian)
	p := newTCPProxy(t, c.Addr(22))
	o := viaProxy(c.Options(), p)
	o.Connection.Keepalive = 1
	o.KeepaliveTimeout = 2 * time.Second
	x, s := shell(t, c, func(got *sshclient.Options) { *got = o })

	before := p.bytes.Load()
	time.Sleep(6 * time.Second)
	idle := p.bytes.Load() - before
	select {
	case <-s.Done():
		t.Fatalf("session died while idle: %v", s.WaitError())
	default:
	}
	// each keepalive is one request + one reply (~100 bytes each way); 6s at 1s interval must show traffic
	if idle < 6*60 {
		t.Fatalf("only %d bytes crossed the wire in 6s idle; keepalives are not being sent", idle)
	}
	t.Logf("idle 6s with keepalive=1s: %d bytes on the wire", idle)
	x.Sync(t, 5*time.Second)
	Sh(t, s, "true")
}

// A silent network (no RST, no FIN): keepalive must notice; without it the session cannot know.
func TestBlackholeDetectedByKeepalive(t *testing.T) {
	c := Shared(t, Debian)
	p := newTCPProxy(t, c.Addr(22))
	o := viaProxy(c.Options(), p)
	o.Connection.Keepalive = 1
	o.KeepaliveTimeout = 2 * time.Second
	_, s := shell(t, c, func(got *sshclient.Options) { *got = o })
	p.blackholed.Store(true)
	d, ok := waitDone(s, 10*time.Second)
	if !ok {
		t.Fatal("blackholed session not closed within 10s despite keepalive=1s/timeout=2s")
	}
	t.Logf("blackhole detected after %v (keepalive 1s + reply timeout 2s)", d.Round(100*time.Millisecond))
	if d > 6*time.Second {
		t.Fatalf("detection took %v", d)
	}
	if _, err := s.Read(make([]byte, 1)); err == nil {
		t.Fatal("Read succeeded on a closed session")
	}
	start := time.Now()
	if _, err := s.Write([]byte("x")); err == nil || time.Since(start) > 2*time.Second {
		t.Fatalf("Write after loss: err=%v after %v", err, time.Since(start))
	}
}

func TestBlackholeBoundedWithoutKeepalive(t *testing.T) {
	c := Shared(t, Debian)
	p := newTCPProxy(t, c.Addr(22))
	o := viaProxy(c.Options(), p)
	o.Connection.Keepalive = 0
	_, s := shell(t, c, func(got *sshclient.Options) { *got = o })
	p.blackholed.Store(true)

	// the session cannot know the peer is gone (that is what keepalive is for) ...
	if _, ok := waitDone(s, 3*time.Second); ok {
		t.Fatal("unexpected: session noticed a silent peer without keepalive")
	}
	// ... but every caller-visible operation stays bounded by its context / Close
	for name, fn := range map[string]func(ctx context.Context) error{
		"Run":    func(ctx context.Context) error { return s.Run(ctx, "true", nil, io.Discard, io.Discard) },
		"Output": func(ctx context.Context) error { _, e := s.Output(ctx, "true", 1024); return e },
		"SFTP": func(ctx context.Context) error {
			_, cleanup, e := s.OpenSFTPContext(ctx)
			if cleanup != nil {
				cleanup()
			}
			return e
		},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		start := time.Now()
		err := fn(ctx)
		cancel()
		if err == nil || time.Since(start) > 4*time.Second {
			t.Errorf("%s: err=%v after %v", name, err, time.Since(start))
		}
	}
	done := make(chan struct{})
	go func() { s.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close hung on a blackholed connection")
	}
}

func TestContainerStoppedMidSession(t *testing.T) {
	for _, how := range []string{"stop", "kill"} {
		t.Run(how, func(t *testing.T) {
			c := Start(t, Spec{Variant: Debian})
			o := c.Options()
			o.Connection.Keepalive = 1
			o.KeepaliveTimeout = 2 * time.Second
			x, s := shell(t, c, func(got *sshclient.Options) { *got = o })
			sf, cleanup, err := s.OpenSFTPContext(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer cleanup()
			sc := &sftpfs.Client{Client: sf}
			if _, err = sc.List("/"); err != nil {
				t.Fatal(err)
			}

			go func() {
				time.Sleep(200 * time.Millisecond)
				if how == "stop" {
					c.Stop()
				} else {
					c.Kill()
				}
			}()
			d, ok := waitDone(s, 20*time.Second)
			if !ok {
				t.Fatalf("session still open 20s after docker %s", how)
			}
			t.Logf("docker %s: session closed after %v; WaitError=%v", how, d.Round(10*time.Millisecond), s.WaitError())
			if err := x.Err(); err == nil {
				t.Fatal("reader saw no error")
			}
			start := time.Now()
			if _, err = TrySh(s, "true", 5*time.Second); err == nil {
				t.Fatal("Run succeeded on a dead session")
			}
			if _, err = sc.List("/"); err == nil {
				t.Fatal("SFTP List succeeded on a dead session")
			}
			if _, _, err = s.OpenSFTPContext(context.Background()); err == nil {
				t.Fatal("OpenSFTPContext succeeded on a dead session")
			}
			if time.Since(start) > 5*time.Second {
				t.Fatalf("operations on a dead session took %v", time.Since(start))
			}
			if _, err = sshclient.Dial(context.Background(), withTimeout(o, 3*time.Second)); err == nil {
				t.Fatal("redial to a stopped container succeeded")
			}
		})
	}
}

func withTimeout(o sshclient.Options, d time.Duration) sshclient.Options { o.Timeout = d; return o }

// docker pause freezes sshd without closing sockets: the real-world "server hung / VM suspended" case.
func TestContainerPausedThenResumed(t *testing.T) {
	c := Start(t, Spec{Variant: Debian})
	o := c.Options()
	o.Connection.Keepalive = 1
	o.KeepaliveTimeout = 8 * time.Second // generous: a pause shorter than this must be survived
	x, s := shell(t, c, func(got *sshclient.Options) { *got = o })
	c.Pause()
	time.Sleep(3 * time.Second)
	c.Unpause()
	x.Sync(t, 10*time.Second)
	select {
	case <-s.Done():
		t.Fatal("a 3s server stall killed the session")
	default:
	}
	// and a dial against a frozen server fails by its own timeout instead of hanging
	c.Pause()
	defer c.Unpause()
	start := time.Now()
	_, err := sshclient.Dial(context.Background(), withTimeout(c.Options(), 3*time.Second))
	if err == nil || time.Since(start) > 6*time.Second {
		t.Fatalf("dial to frozen server: err=%v after %v", err, time.Since(start))
	}
}
