//go:build interop

package interop

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"nextshell.dev/v2/internal/sshclient"
	"nextshell.dev/v2/internal/storage"
)

func dial(o sshclient.Options) (*sshclient.Session, error) {
	return sshclient.Dial(context.Background(), o)
}

func asHostKeyError(t *testing.T, err error) *sshclient.HostKeyError {
	t.Helper()
	var hk *sshclient.HostKeyError
	if !errors.As(err, &hk) {
		t.Fatalf("want *HostKeyError, got %T: %v", err, err)
	}
	return hk
}

// The desktop Platform flow: no pin -> prompt -> PinHost; later dials carry the pin as Connection.Fingerprint.
func TestHostKeyFirstUsePinAndChangedKey(t *testing.T) {
	c := Start(t, Spec{Variant: Debian})
	port := c.HostPort()
	store, err := storage.Open(filepath.Join(t.TempDir(), "profile.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// 1. first use, no callback: refused with the fingerprint, which must be one the server really has.
	o := c.Options()
	o.HostKey = nil
	_, err = dial(o)
	hk := asHostKeyError(t, err)
	if hk.Changed {
		t.Fatal("first use reported as changed")
	}
	if !slices.Contains(c.HostFingerprints(t), hk.Fingerprint) {
		t.Fatalf("fingerprint %s is not one of the server's %v", hk.Fingerprint, c.HostFingerprints(t))
	}
	addr := o.Connection.Address()

	// 2. user trusts it: pin, then connect with the pin.
	o.HostKey = func(a, fp string) error { return store.PinHost(a, fp) }
	Dial(t, o).Close()
	pin, _ := store.HostPin(addr)
	if pin != hk.Fingerprint {
		t.Fatalf("stored pin %q, want %q", pin, hk.Fingerprint)
	}
	o.Connection.Fingerprint = pin
	o.HostKey = nil
	whoami(t, Dial(t, o))

	// 3. same address, new container => new host keys. Both paths must refuse.
	c.Remove()
	c2 := Start(t, Spec{Variant: Debian, HostPort: port})
	if slices.Contains(c2.HostFingerprints(t), pin) {
		t.Fatal("recreated container kept its host key; test is meaningless")
	}
	if _, err = dial(o); !asHostKeyError(t, err).Changed {
		t.Fatalf("pinned dial: want Changed host key error, got %v", err)
	}
	o.Connection.Fingerprint = ""
	o.HostKey = func(a, fp string) error { return store.PinHost(a, fp) }
	if _, err = dial(o); err == nil || !strings.Contains(err.Error(), "host key changed") {
		t.Fatalf("callback path: want 'host key changed', got %v", err)
	}
	if after, _ := store.HostPin(addr); after != pin {
		t.Fatal("pin was overwritten by a changed key")
	}
}

func scanKnownHosts(t *testing.T, c *Container, filterType string) string {
	t.Helper()
	cmd := "ssh-keyscan -p 22 127.0.0.1 2>/dev/null"
	if filterType != "" {
		cmd = "ssh-keyscan -t " + filterType + " -p 22 127.0.0.1 2>/dev/null"
	}
	var sb strings.Builder
	for _, line := range strings.Split(c.Exec(t, cmd), "\n") {
		if f := strings.Fields(line); len(f) == 3 {
			sb.WriteString("[127.0.0.1]:" + strconv.Itoa(c.HostPort()) + " " + f[1] + " " + f[2] + "\n")
		}
	}
	return sb.String()
}

// UseKnownHosts: a known_hosts written by OpenSSH holds one key type (usually ed25519) per host.
func TestKnownHostsFile(t *testing.T) {
	c := Shared(t, Debian)
	for _, typ := range []string{"", "ed25519", "ecdsa", "rsa"} {
		name := typ
		if name == "" {
			name = "all-types"
		}
		t.Run(name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "known_hosts")
			if err := os.WriteFile(file, []byte(scanKnownHosts(t, c, typ)), 0600); err != nil {
				t.Fatal(err)
			}
			o := c.Options()
			o.HostKey = func(string, string) error { t.Error("prompted although the host is in known_hosts"); return nil }
			o.KnownHostsFile = file
			s, err := dial(o)
			if err != nil {
				t.Fatalf("known_hosts with %q key rejected a genuine server: %v", typ, err)
			}
			s.Close()
		})
	}
}

func TestKnownHostsChangedKeyRefused(t *testing.T) {
	a := Start(t, Spec{Variant: Debian})
	b := Start(t, Spec{Variant: Debian})
	file := filepath.Join(t.TempDir(), "known_hosts")
	// a's keys recorded under b's address
	lines := strings.ReplaceAll(scanKnownHosts(t, a, ""), "["+"127.0.0.1]:"+strconv.Itoa(a.HostPort()), "[127.0.0.1]:"+strconv.Itoa(b.HostPort()))
	if err := os.WriteFile(file, []byte(lines), 0600); err != nil {
		t.Fatal(err)
	}
	o := b.Options()
	o.HostKey = func(string, string) error { t.Error("must not prompt on a changed key"); return nil }
	o.KnownHostsFile = file
	_, err := dial(o)
	if err == nil || !strings.Contains(err.Error(), "key mismatch") {
		t.Fatalf("want key mismatch, got %v", err)
	}
	// unknown host in an existing file falls back to first-use handling
	o.KnownHostsFile = filepath.Join(t.TempDir(), "empty")
	os.WriteFile(o.KnownHostsFile, nil, 0600)
	o.HostKey = func(string, string) error { return nil }
	Dial(t, o).Close()
}
