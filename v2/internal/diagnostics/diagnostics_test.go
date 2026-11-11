package diagnostics

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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
		if errors.Is(e, os.ErrPermission) {
			t.Skip("box 禁止非特权ICMP：ping_group_range=1 0，进程无CAP_NET_RAW；不改系统权限")
		}
		t.Fatal(e)
	}
	if s.Sent != 3 || s.Received != 3 || s.Loss != 0 {
		t.Fatal(s)
	}
	t.Logf("localhost ICMP: sent=%d received=%d loss=%.0f%%", s.Sent, s.Received, s.Loss)
}
func TestTraceOptionalBinaryArgumentsAndCancellation(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "user nexttrace")
	if e := os.WriteFile(binary, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\"\n"), 0700); e != nil {
		t.Fatal(e)
	}
	out, e := Trace(context.Background(), binary, "127.0.0.1")
	if e != nil || out != "--traceroute\n--raw\n--no-rdns\n--data-provider\ndisable-geoip\n--map\n--no-color\n127.0.0.1\n" {
		t.Fatal(out, e)
	}
	if e = os.WriteFile(binary, []byte("#!/bin/sh\nexec sleep 30\n"), 0700); e != nil {
		t.Fatal(e)
	}
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
