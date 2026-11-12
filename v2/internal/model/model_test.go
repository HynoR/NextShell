package model

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestQuickConnect(t *testing.T) {
	for _, tc := range []struct {
		input, host string
		port        int
	}{{"user@example.com", "example.com", 22}, {"ssh://user@example.com:2222", "example.com", 2222}, {"user@[::1]:22", "::1", 22}, {" user@host ", "host", 22}} {
		t.Run(tc.input, func(t *testing.T) {
			c, e := QuickConnect(tc.input)
			if e != nil || c.User != "user" || c.Host != tc.host || c.Port != tc.port {
				t.Fatal(c, e)
			}
		})
	}
	for _, bad := range []string{"", "host", "@host", "user@", "user@host:bad", "user@host:0", "user@host:65536", "user@::1"} {
		t.Run("bad-"+bad, func(t *testing.T) {
			if _, e := QuickConnect(bad); e == nil {
				t.Fatal("bad quick connect accepted")
			}
		})
	}
	if _, e := QuickConnect("host"); e == nil || e.Error() != "请输入 用户名@主机[:端口]" {
		t.Fatal("the format hint is for the person typing:", e)
	}
}
func TestConnectionValidation(t *testing.T) {
	base := Connection{Name: "x", Host: "localhost", Port: 22, User: "tester", Auth: "password", Encoding: "utf-8"}
	cases := []func(*Connection){func(c *Connection) { c.Name = " " }, func(c *Connection) { c.Host = "" }, func(c *Connection) { c.User = "" }, func(c *Connection) { c.Port = -1 }, func(c *Connection) { c.Auth = "invalid" }, func(c *Connection) { c.Auth = "key" }, func(c *Connection) { c.Encoding = "invalid" }, func(c *Connection) { c.Keepalive = -1 }, func(c *Connection) { c.Keepalive = 3601 }}
	for i, f := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			c := base
			f(&c)
			if e := c.Validate(); e == nil {
				t.Fatal(c)
			}
		})
	}
	if e := base.Validate(); e != nil {
		t.Fatal(e)
	}
}
func TestProxyValidation(t *testing.T) {
	for _, kind := range []string{"socks4", "socks5"} {
		if e := (Proxy{Name: "x", Kind: kind, Host: "localhost", Port: 1080}).Validate(); e != nil {
			t.Fatal(e)
		}
	}
	for _, p := range []Proxy{{}, {Name: "x", Kind: "http", Host: "host", Port: 1080}, {Name: "x", Kind: "socks5", Host: "host", Port: 65536}} {
		if e := p.Validate(); e == nil {
			t.Fatal(p)
		}
	}
}

func TestMonitorDefaultsOnUnlessSaidOff(t *testing.T) {
	for in, want := range map[string]bool{`{"id":"a"}`: true, `{"id":"a","monitorSession":true}`: true, `{"id":"a","monitorSession":false}`: false} {
		var c Connection
		if e := json.Unmarshal([]byte(in), &c); e != nil || c.Monitor != want {
			t.Fatalf("%s: monitor %v, %v", in, c.Monitor, e)
		}
	}
	b, _ := json.Marshal(Connection{})
	if !strings.Contains(string(b), `"monitorSession":false`) {
		t.Fatalf("an off switch must be written: %s", b)
	}
	if c, _ := QuickConnect("root@10.0.0.1"); !c.Monitor {
		t.Fatal("quick connect samples by default")
	}
}

func TestCredentialRules(t *testing.T) {
	c := Connection{Name: "x", Host: "h", Port: 22, Auth: "password", Encoding: "utf-8", CredentialID: "cr"}
	if c.Validate() != nil {
		t.Fatal("a host using a credential needs no user of its own")
	}
	c.CredentialID, c.User, c.Auth, c.NeedsKeyRebind = "", "u", "key", true
	if c.Validate() != nil {
		t.Fatal("an imported key host waiting for its key")
	}
	r := c.WithCredential(Credential{User: "ops", Auth: "key", KeyID: "k", Secret: "s"})
	if r.User != "ops" || r.KeyID != "k" || r.Secret != "s" || r.NeedsKeyRebind {
		t.Fatal(r)
	}
	for _, bad := range []Credential{{Name: "n", Auth: "password"}, {Name: "n", User: "u", Auth: "key"}, {Name: "n", User: "u", Auth: "x"}} {
		if bad.Validate() == nil {
			t.Fatal("accepted", bad)
		}
	}
}
