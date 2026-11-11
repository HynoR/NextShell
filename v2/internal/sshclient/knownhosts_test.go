package sshclient

import (
	"context"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"net"
	"nextshell.dev/v2/internal/model"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestOptionalKnownHostsReadOnlyPinPriority(t *testing.T) {
	s := newServer(t, false, false)
	host, portText, e := net.SplitHostPort(s.addr)
	if e != nil {
		t.Fatal(e)
	}
	port, _ := strconv.Atoi(portText)
	file := filepath.Join(t.TempDir(), "known_hosts")
	line := knownhosts.Line([]string{knownhosts.Normalize(s.addr)}, s.signer.PublicKey()) + "\n"
	if e = os.WriteFile(file, []byte(line), 0400); e != nil {
		t.Fatal(e)
	}
	conn := model.Connection{Name: "known", Host: host, Port: port, User: "tester", Auth: "password", Secret: "secret", Encoding: "utf-8"}
	for _, tc := range []struct {
		name, pin string
		want      bool
	}{{"file-match", "", true}, {"bad-db-pin-wins", "SHA256:bad", false}, {"good-db-pin", ssh.FingerprintSHA256(s.signer.PublicKey()), true}} {
		t.Run(tc.name, func(t *testing.T) {
			conn.Fingerprint = tc.pin
			c, e := Dial(context.Background(), Options{Connection: conn, KnownHostsFile: file, Timeout: time.Second})
			if (e == nil) != tc.want {
				t.Fatalf("success=%v error=%v", e == nil, e)
			}
			if c != nil {
				c.Close()
			}
		})
	}
	after, _ := os.ReadFile(file)
	if string(after) != line {
		t.Fatal("known_hosts was modified")
	}
	absentSession, e := Dial(context.Background(), Options{Connection: conn, KnownHostsFile: file + "-absent", Timeout: time.Second})
	if absentSession != nil {
		absentSession.Close()
	}
	if e != nil {
		t.Fatal("pin should not depend on existing known_hosts:", e)
	}
	conn.Fingerprint = ""
	other := newServer(t, false, false)
	wrong := knownhosts.Line([]string{knownhosts.Normalize(s.addr)}, other.signer.PublicKey()) + "\n"
	os.Chmod(file, 0600)
	os.WriteFile(file, []byte(wrong), 0400)
	_, e = Dial(context.Background(), Options{Connection: conn, KnownHostsFile: file, HostKey: func(string, string) error { t.Error("must not prompt on changed known key"); return nil }, Timeout: time.Second})
	if e == nil || !strings.Contains(e.Error(), "key mismatch") {
		t.Fatalf("mismatch accepted: %v", e)
	}
}
