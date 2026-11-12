//go:build interop

package interop

import (
	"context"
	"strings"
	"testing"
	"time"

	"nextshell.dev/v2/internal/model"
	"nextshell.dev/v2/internal/sshclient"
)

// The target is only resolvable inside the docker network, so the connection can only succeed if the
// proxy (not the client) resolves the hostname, which is what SOCKS5 domain addressing is for.
func TestSOCKSProxy(t *testing.T) {
	net := Network(t)
	target := Start(t, Spec{Variant: Debian, Network: net, Alias: "target.internal"})
	socks := Start(t, Spec{Variant: Socks, Network: net, Ports: []int{1080, 1081}})
	_ = target

	o := target.Options()
	o.Connection.Host, o.Connection.Port = "target.internal", 22
	proxy := func(kind string, port int, user, pass string) *model.Proxy {
		return &model.Proxy{ID: "p", Name: "p", Kind: kind, Host: "127.0.0.1", Port: port, User: user, Password: pass}
	}
	try := func(p *model.Proxy) (*sshclient.Session, error) {
		o := o
		o.Proxy = p
		o.Timeout = 8 * time.Second
		return sshclient.Dial(context.Background(), o)
	}

	t.Run("direct-cannot-resolve", func(t *testing.T) {
		if s, err := try(nil); err == nil {
			s.Close()
			t.Fatal("target.internal resolved without the proxy; test is meaningless")
		}
	})
	t.Run("socks5-no-auth", func(t *testing.T) {
		s, err := try(proxy("socks5", socks.Ports[1080], "", ""))
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		if got := whoami(t, s); got != "tester" {
			t.Fatal(got)
		}
		if got := strings.TrimSpace(Sh(t, s, "echo through-socks")); got != "through-socks" {
			t.Fatal(got)
		}
	})
	t.Run("socks5-user-pass", func(t *testing.T) {
		s, err := try(proxy("socks5", socks.Ports[1081], "proxy", "proxy-pass"))
		if err != nil {
			t.Fatal(err)
		}
		s.Close()
	})
	t.Run("socks5-wrong-proxy-password", func(t *testing.T) {
		start := time.Now()
		s, err := try(proxy("socks5", socks.Ports[1081], "proxy", "wrong"))
		if err == nil {
			s.Close()
			t.Fatal("wrong proxy password accepted")
		}
		t.Logf("wrong proxy password: %v (%v)", err, time.Since(start).Round(time.Millisecond))
	})
	t.Run("socks5-auth-required-but-none-given", func(t *testing.T) {
		if s, err := try(proxy("socks5", socks.Ports[1081], "", "")); err == nil {
			s.Close()
			t.Fatal("anonymous client passed an authenticating proxy")
		} else {
			t.Logf("auth-required proxy, no credentials: %v", err)
		}
	})
	t.Run("socks4-ip", func(t *testing.T) {
		o := o
		o.Connection.Host = target.IP(t, net)
		o.Proxy = proxy("socks4", socks.Ports[1080], "tester", "")
		o.Timeout = 8 * time.Second
		s, err := sshclient.Dial(context.Background(), o)
		if err != nil {
			t.Fatal(err)
		}
		s.Close()
	})
	t.Run("socks4a-domain", func(t *testing.T) {
		// Dante answers "granted" for a SOCKS4a domain it cannot serve and then drops the stream; this is
		// recorded, not asserted, because it is the proxy's behaviour.
		s, err := try(proxy("socks4", socks.Ports[1080], "tester", ""))
		if err == nil {
			s.Close()
		}
		t.Logf("SOCKS4a with a domain name against Dante: %v", err)
	})
	t.Run("proxy-down", func(t *testing.T) {
		start := time.Now()
		if s, err := try(proxy("socks5", 1, "", "")); err == nil {
			s.Close()
			t.Fatal("dialled through a closed port")
		} else if d := time.Since(start); d > 5*time.Second {
			t.Fatalf("took %v", d)
		}
	})
	t.Run("proxy-refuses-target", func(t *testing.T) {
		o := o
		o.Connection.Host = "no-such-host.internal"
		o.Proxy = proxy("socks5", socks.Ports[1080], "", "")
		o.Timeout = 8 * time.Second
		start := time.Now()
		if s, err := sshclient.Dial(context.Background(), o); err == nil {
			s.Close()
			t.Fatal("connected to a nonexistent host")
		} else {
			t.Logf("unresolvable target via proxy: %v (%v)", err, time.Since(start).Round(time.Millisecond))
		}
	})
}
