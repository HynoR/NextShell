package diagnostics

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The test binary doubles as the user's nexttrace, because a shell script cannot be one on Windows:
// the copy Trace runs finds the mode in the environment.
func TestMain(m *testing.M) {
	switch os.Getenv("NEXTSHELL_FAKE_NEXTTRACE") {
	case "args":
		for _, a := range os.Args[1:] {
			fmt.Println(a)
		}
		os.Exit(0)
	case "sleep":
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestDiagnosticTargetsRejectArguments(t *testing.T) {
	for _, target := range []string{"-F", "example.com", "https://example.com", "127.0.0.1; touch nope", "127.0.0.1 --tcp", "fe80::1%eth0", ""} {
		t.Run(target, func(t *testing.T) {
			if _, e := targetIP(target); e == nil {
				t.Fatal("accepted")
			}
		})
	}
	for _, s := range []string{"127.0.0.1", "::1", "2001:db8::1"} {
		if _, e := targetIP(s); e != nil {
			t.Fatal(e)
		}
	}
}
func TestPingLoopbackProBing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, e := Ping(ctx, "127.0.0.1")
	if e != nil {
		// A socket the system will not give this process: Linux without ping_group_range/CAP_NET_RAW,
		// Windows without the right to open a raw ICMP socket. Not a failed ping, which returns no error.
		var op *net.OpError
		if errors.Is(e, os.ErrPermission) || errors.As(e, &op) && op.Op == "listen" {
			t.Skipf("ICMP sockets are not available to this process (not changing system permissions): %v", e)
		}
		t.Fatal(e)
	}
	if s.Sent != 3 || s.Received != 3 || s.Loss != 0 {
		t.Fatal(s)
	}
	t.Logf("localhost ICMP: sent=%d received=%d loss=%.0f%%", s.Sent, s.Received, s.Loss)
}
func TestTraceOptionalBinaryArgumentsAndCancellation(t *testing.T) {
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	test, e := os.ReadFile(exe)
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "user nexttrace"+filepath.Ext(exe)) // a path with a space: no shell may see it
	if e := os.WriteFile(binary, test, 0700); e != nil {
		t.Fatal(e)
	}
	t.Setenv("NEXTSHELL_FAKE_NEXTTRACE", "args")
	out, e := Trace(context.Background(), binary, "127.0.0.1")
	if e != nil || out != "--traceroute\n--raw\n--no-rdns\n--data-provider\ndisable-geoip\n--map\n--no-color\n127.0.0.1\n" {
		t.Fatal(out, e)
	}
	t.Setenv("NEXTSHELL_FAKE_NEXTTRACE", "sleep")
	ctx, c := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer c()
	start := time.Now()
	_, e = Trace(ctx, binary, "::1")
	if !errors.Is(e, context.DeadlineExceeded) || time.Since(start) > 2*time.Second {
		t.Fatal(e)
	}
	if _, e = Trace(context.Background(), filepath.Join(dir, "absent"), "127.0.0.1"); e == nil {
		t.Fatal("missing binary hidden")
	}
}
func TestTraceOutputIsBounded(t *testing.T) {
	var b boundedOutput
	n, e := b.Write([]byte(strings.Repeat("x", 100000)))
	if e != nil || n != 100000 || len(b.String()) > 66000 || !strings.Contains(b.String(), "截断") {
		t.Fatal(n, e, len(b.String()))
	}
}
