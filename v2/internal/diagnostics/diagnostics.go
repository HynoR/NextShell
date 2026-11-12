// Package diagnostics uses pro-bing and an optional user-installed nexttrace executable.
// No NTrace-core code is linked. Targets are IP literals, avoiding implicit DNS/helper lookups.
package diagnostics

import (
	"bytes"
	"context"
	"fmt"
	probing "github.com/prometheus-community/pro-bing"
	"net/netip"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

func targetIP(s string) (string, error) {
	a, e := netip.ParseAddr(strings.TrimSpace(s))
	if e != nil || a.Zone() != "" {
		return "", fmt.Errorf("目标必须为IPv4/IPv6字面地址（不接受URL、参数或zone）")
	}
	return a.String(), nil
}

type PingResult struct {
	Sent, Received int
	Loss           float64
	Min, Avg, Max  time.Duration
}

func Ping(ctx context.Context, target string) (PingResult, error) {
	var result PingResult
	ip, e := targetIP(target)
	if e != nil {
		return result, e
	}
	p := probing.New(ip)
	p.Count = 3
	p.Interval = 150 * time.Millisecond
	p.Timeout = 3 * time.Second
	// Windows has no unprivileged ICMP socket: pro-bing's "privileged" (raw) mode is the only one
	// there, and the library documents it as working without elevation.
	p.SetPrivileged(runtime.GOOS == "windows")
	if e = p.RunWithContext(ctx); e != nil {
		need := "需系统允许非特权ping socket"
		if runtime.GOOS == "windows" {
			need = "需系统允许原始ICMP socket，防火墙或安全软件可能拦截了它"
		}
		return result, fmt.Errorf("ICMP ping不可用（%s）：%w", need, e)
	}
	s := p.Statistics()
	result = PingResult{s.PacketsSent, s.PacketsRecv, s.PacketLoss, s.MinRtt, s.AvgRtt, s.MaxRtt}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	return result, nil
}

type boundedOutput struct {
	mu        sync.Mutex
	b         bytes.Buffer
	truncated bool
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	left := (64 << 10) - b.b.Len()
	if left > 0 {
		b.b.Write(p[:min(n, left)])
	}
	if n > left {
		b.truncated = true
	}
	return n, nil
}
func (b *boundedOutput) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.b.String()
	if b.truncated {
		s += "\n[输出已截断到64KiB]"
	}
	return s
}

// Trace never uses a shell. Fail closed on unsupported flags; never rerun a measurement.
// CLI flags verified against https://github.com/nxtrace/NTrace-core/blob/main/README.md.
func Trace(ctx context.Context, binary, target string) (string, error) {
	ip, e := targetIP(target)
	if e != nil {
		return "", e
	}
	if binary == "" {
		binary = "nexttrace"
	}
	command := exec.CommandContext(ctx, binary, "--traceroute", "--raw", "--no-rdns", "--data-provider", "disable-geoip", "--map", "--no-color", ip)
	command.WaitDelay = time.Second
	var output boundedOutput
	command.Stdout = &output
	command.Stderr = &output
	e = command.Run()
	if ctx.Err() != nil {
		e = ctx.Err()
	}
	return output.String(), e
}
