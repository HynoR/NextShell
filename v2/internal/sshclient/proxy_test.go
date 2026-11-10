package sshclient

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"nextshell.dev/v2/internal/model"
	"strconv"
	"sync"
	"testing"
	"time"
)

type proxyRequest struct {
	Host, User, Password string
	Port                 int
	Version              byte
}

func testProxy(t *testing.T, version byte, auth, reject bool) (*model.Proxy, <-chan proxyRequest) {
	t.Helper()
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	h, ps, _ := net.SplitHostPort(l.Addr().String())
	port, _ := strconv.Atoi(ps)
	p := &model.Proxy{Name: "test", Kind: fmt.Sprintf("socks%d", version), Host: h, Port: port}
	if auth {
		p.User = "proxy-user"
		p.Password = "proxy-pass"
	}
	requests := make(chan proxyRequest, 1)
	var conns sync.Map
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			c, e := l.Accept()
			if e != nil {
				return
			}
			conns.Store(c, true)
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer conns.Delete(c)
				defer c.Close()
				c.SetDeadline(time.Now().Add(5 * time.Second))
				r := bufio.NewReader(c)
				v, e := r.ReadByte()
				if e != nil {
					return
				}
				got := proxyRequest{Version: v}
				if v == 4 {
					cmd, _ := r.ReadByte()
					if cmd != 1 {
						return
					}
					b := make([]byte, 6)
					if _, e = io.ReadFull(r, b); e != nil {
						return
					}
					got.Port = int(b[0])<<8 | int(b[1])
					got.Host = net.IP(b[2:]).String()
					user, e := r.ReadString(0)
					if e != nil {
						return
					}
					got.User = user[:len(user)-1]
					if b[2] == 0 && b[3] == 0 && b[4] == 0 && b[5] != 0 {
						host, e := r.ReadString(0)
						if e != nil {
							return
						}
						got.Host = host[:len(host)-1]
					}
					requests <- got
					if reject {
						c.Write([]byte{0, 91, 0, 0, 0, 0, 0, 0})
						return
					}
					c.Write([]byte{0, 90, 0, 0, 0, 0, 0, 0})
				} else if v == 5 {
					n, _ := r.ReadByte()
					methods := make([]byte, int(n))
					if _, e = io.ReadFull(r, methods); e != nil {
						return
					}
					method := byte(0)
					if auth {
						method = 2
					}
					if reject {
						c.Write([]byte{5, 255})
						return
					}
					c.Write([]byte{5, method})
					if auth {
						av, _ := r.ReadByte()
						if av != 1 {
							return
						}
						un, _ := r.ReadByte()
						ub := make([]byte, int(un))
						io.ReadFull(r, ub)
						pn, _ := r.ReadByte()
						pb := make([]byte, int(pn))
						io.ReadFull(r, pb)
						got.User = string(ub)
						got.Password = string(pb)
						if got.User != "proxy-user" || got.Password != "proxy-pass" {
							c.Write([]byte{1, 1})
							return
						}
						c.Write([]byte{1, 0})
					}
					head := make([]byte, 4)
					if _, e = io.ReadFull(r, head); e != nil {
						return
					}
					switch head[3] {
					case 1:
						b := make([]byte, 4)
						io.ReadFull(r, b)
						got.Host = net.IP(b).String()
					case 3:
						n, _ := r.ReadByte()
						b := make([]byte, int(n))
						io.ReadFull(r, b)
						got.Host = string(b)
					case 4:
						b := make([]byte, 16)
						io.ReadFull(r, b)
						got.Host = net.IP(b).String()
					default:
						return
					}
					pb := make([]byte, 2)
					io.ReadFull(r, pb)
					got.Port = int(pb[0])<<8 | int(pb[1])
					requests <- got
					c.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0})
				} else {
					return
				}
				up, e := net.Dial("tcp", net.JoinHostPort(got.Host, strconv.Itoa(got.Port)))
				if e != nil {
					return
				}
				defer up.Close()
				c.SetDeadline(time.Time{})
				done := make(chan struct{})
				go func() { io.Copy(up, r); up.Close(); close(done) }()
				io.Copy(c, up)
				c.Close()
				<-done
			}()
		}
	}()
	t.Cleanup(func() { l.Close(); conns.Range(func(k, v any) bool { k.(net.Conn).Close(); return true }); wg.Wait() })
	return p, requests
}
func TestSSHSOCKSProxies(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version byte
		auth    bool
		domain  bool
	}{{"socks4", 4, false, false}, {"socks4a", 4, false, true}, {"socks5", 5, false, false}, {"socks5-auth", 5, true, true}} {
		t.Run(tc.name, func(t *testing.T) {
			srv := newServer(t, false, false)
			p, req := testProxy(t, tc.version, tc.auth, false)
			o := srv.options()
			o.Proxy = p
			if tc.domain {
				o.Connection.Host = "localhost"
			}
			s, e := Dial(context.Background(), o)
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			echo(t, s, "through proxy")
			got := <-req
			if got.Port != o.Connection.Port || got.Host != o.Connection.Host {
				t.Fatal(got)
			}
			if tc.auth && (got.User != p.User || got.Password != p.Password) {
				t.Fatal("proxy auth missing")
			}
		})
	}
}
func TestProxyRejects(t *testing.T) {
	for _, v := range []byte{4, 5} {
		t.Run(fmt.Sprint(v), func(t *testing.T) {
			p, _ := testProxy(t, v, false, true)
			ctx, c := context.WithTimeout(context.Background(), time.Second)
			defer c()
			conn, e := dialTransport(ctx, "localhost:22", p)
			if e == nil {
				conn.Close()
				t.Fatal("rejected proxy accepted")
			}
		})
	}
}
func TestProxyInvalid(t *testing.T) {
	for _, p := range []model.Proxy{{Name: "x", Kind: "http", Host: "localhost", Port: 1}, {Name: "x", Kind: "socks4", Host: "localhost", Port: 0}} {
		if _, e := dialTransport(context.Background(), "localhost:22", &p); e == nil {
			t.Fatal("invalid accepted")
		}
	}
	if _, e := dialTransport(context.Background(), "badaddress", nil); e == nil {
		t.Fatal("invalid target accepted")
	}
}
func TestAgentUnavailable(t *testing.T) {
	srv := newServer(t, false, false)
	o := srv.options()
	o.Connection.Auth = "agent"
	o.AgentSocket = "/nonexistent-nextshell-test-agent"
	if s, e := Dial(context.Background(), o); e == nil {
		s.Close()
		t.Fatal("missing agent accepted")
	}
	o.Connection.Auth = "keyboard"
	o.Keyboard = func(string, string, []string, []bool) ([]string, error) { return nil, errors.New("cancel") }
	if s, e := Dial(context.Background(), o); e == nil {
		s.Close()
		t.Fatal("cancelled challenge accepted")
	}
}

func TestSOCKS5WrongPassword(t *testing.T) {
	p, _ := testProxy(t, 5, true, false)
	p.Password = "wrong"
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if c, e := dialTransport(ctx, "localhost:22", p); e == nil {
		c.Close()
		t.Fatal("wrong proxy password accepted")
	}
}
func TestProxyHandshakeCancellation(t *testing.T) {
	for _, kind := range []string{"socks4", "socks5"} {
		t.Run(kind, func(t *testing.T) {
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
			h, ps, _ := net.SplitHostPort(l.Addr().String())
			port, _ := strconv.Atoi(ps)
			p := &model.Proxy{Name: "silent", Kind: kind, Host: h, Port: port}
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			start := time.Now()
			c, e := dialTransport(ctx, "localhost:22", p)
			if e == nil {
				c.Close()
				t.Fatal("silent proxy succeeded")
			}
			if time.Since(start) > time.Second {
				t.Fatal("proxy cancellation hung")
			}
		})
	}
}
