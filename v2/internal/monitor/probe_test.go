package monitor

import (
	"context"
	"fmt"
	"math"
	"nextshell.dev/v2/internal/sshclient"
	"nextshell.dev/v2/internal/testutil"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const probe = `---NS_LOADAVG---
0.10 0.20 0.30
---NS_CPUSTAT---
cpu 100 10 20 300 40 0 0 0 0 0
---NS_MEMINFO---
MemTotal: 1024000 kB
MemAvailable: 512000 kB
SwapTotal: 1024 kB
SwapFree: 1024 kB
---NS_DISK---
/dev/vda1 102400 20480 81920 20% /
---NS_NETCOUNTERS---
1000
2000
---NS_PROBE_END---`

func TestFirstPartySystemProbeSpecsAndDeltas(t *testing.T) {
	at := time.Unix(100, 0)
	a, e := Parse(probe, at, nil)
	if e != nil || a.CPUTotal != 470 || a.CPUIdle != 340 || a.RX != 1000 || a.TX != 2000 || a.MemoryAvailable != 512000*1024 || a.Disks[0].UsedKB != 20480 {
		t.Fatal(a, e)
	}
	b, e := Parse(strings.ReplaceAll(strings.ReplaceAll(probe, "cpu 100 10 20 300 40", "cpu 150 10 20 350 40"), "1000\n2000", "1200\n2400"), at.Add(2*time.Second), &a)
	if e != nil || !b.HasDelta || math.Abs(b.CPUPercent-50) > 0.01 || b.RXPerSecond != 100 || b.TXPerSecond != 200 {
		t.Fatal(b, e)
	}
	reset, e := Parse(probe, at.Add(4*time.Second), &b)
	if e != nil || reset.HasDelta || reset.RXPerSecond != 0 {
		t.Fatal(reset, e)
	}
	s := Sections("---NS_INFO---\nrow\r\x1b[Kline\n---NS_PROBE_END---")
	if strings.Contains(s["INFO"], "\x1b") || !strings.Contains(s["INFO"], "row") {
		t.Fatal(s)
	}
}
func TestProbeMalformedAndMemoryFallback(t *testing.T) {
	for _, raw := range []string{"", strings.Replace(probe, "---NS_NETCOUNTERS---", "---NS_NOCOUNTERS---", 1), strings.Replace(probe, "1000\n2000", "invalid\n2000", 1), strings.Replace(probe, "cpu 100", "cpu invalid", 1), strings.Replace(probe, "MemTotal: 1024000", "MemTotal: 0", 1)} {
		if _, e := Parse(raw, time.Now(), nil); e == nil {
			t.Fatal("invalid probe accepted")
		}
	}
	raw := strings.Replace(probe, "MemAvailable: 512000 kB", "MemFree: 100 kB\nCached: 200 kB\nBuffers: 50 kB\nSReclaimable: 50 kB\nShmem: 10 kB", 1)
	v, e := Parse(raw, time.Now(), nil)
	if e != nil || v.MemoryAvailable != 390*1024 {
		t.Fatal(v, e)
	}
}
func TestFirstPartyNetworkAndProcessSpecs(t *testing.T) {
	ss := `Netid State Recv-Q Send-Q Local Address:Port Peer Address:Port Process
tcp ESTAB 0 0 10.0.0.1:22 10.0.0.2:50123 users:(("sshd",pid=100,fd=4))
tcp LISTEN 0 128 0.0.0.0:22 0.0.0.0:* users:(("sshd",pid=100,fd=3))
udp UNCONN 0 0 0.0.0.0:22 0.0.0.0:*
tcp6 LISTEN 0 128 [::]:443 [::]:*`
	rows := ParseSS(ss)
	if len(rows) != 4 || rows[1].PID != 100 || rows[1].Connections != 1 || rows[1].UniqueIPs != 1 || rows[1].Process != "sshd" || rows[3].LocalIP != "::" {
		t.Fatal(rows)
	}
	netstat := `garbage line
tcp 0 0 0.0.0.0:80 0.0.0.0:* LISTEN 99/nginx
tcp 0 0 10.0.0.1:80 10.0.0.3:53000 ESTABLISHED 99/nginx
tcp6 0 0 :::443 :::* LISTEN -
udp 0 0 0.0.0.0:68 0.0.0.0:* -`
	rows = ParseNetstat(netstat)
	if len(rows) != 4 || rows[0].Process != "nginx" || rows[0].Connections != 1 || rows[1].RemotePort != 53000 || rows[2].PID != 0 || rows[2].Process != "unknown" {
		t.Fatal(rows)
	}
	ps := ParseProcesses("100 1 root S 0 20 1.5 2.5 2048 4096 60 command name\ngarbage\n0 1 root S 0 20 1 1 1 1 1 no")
	if len(ps) != 1 || ps[0].MemoryMB != 2 || ps[0].VirtualMB != 4 || ps[0].Command != "command name" {
		t.Fatal(ps)
	}
}
func TestRealSSHProbeExecCancelAndProcessSignal(t *testing.T) {
	testutil.RequireLinuxRemote(t)
	server, e := testutil.New(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(server.Close)
	s, e := sshclient.Dial(context.Background(), server.Options())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	snapshot, e := Collect(context.Background(), s, nil)
	if e != nil || snapshot.MemoryTotal == 0 || len(snapshot.Processes) == 0 || !strings.Contains(snapshot.Info, "Linux") {
		t.Fatal(snapshot, e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, e = s.Output(ctx, "exec sleep 10", 1024); e == nil || time.Since(start) > time.Second {
		t.Fatal("exec cancellation failed", e)
	}
	child := exec.Command("sleep", "5")
	if e = child.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { child.Process.Kill(); child.Wait() }()
	command, e := SignalCommand(child.Process.Pid, "TERM")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Output(context.Background(), command, 1024); e != nil {
		t.Fatal("signal failed", fmt.Sprint(command), e)
	}
	if e = child.Wait(); e == nil {
		t.Fatal("test-owned process not signalled")
	}
	for _, signal := range []string{"TERM;echo x", "", "9"} {
		if _, e = SignalCommand(100, signal); e == nil {
			t.Fatal("signal injection accepted")
		}
	}
	if _, e = SignalCommand(1, "KILL"); e == nil {
		t.Fatal("pid 1 allowed")
	}
	if _, e = s.Output(context.Background(), "printf after-cancel", 1024); e != nil {
		t.Fatal("exec cancel broke terminal SSH connection", e)
	}
}

func TestUnsupportedPsExplainsEmptyProcessList(t *testing.T) {
	raw := strings.Replace(probe, "---NS_DISK---", "---NS_PROCESSES---\nps: unrecognized option: no-headers\nBusyBox v1.37.0\n---NS_DISK---", 1)
	v, e := Parse(raw, time.Now(), nil)
	if e != nil || len(v.Processes) != 0 || len(v.Warnings) == 0 || !strings.Contains(v.Warnings[0], "unrecognized option: no-headers") {
		t.Fatal(v.Warnings, e)
	}
	if v, e = Parse(probe, time.Now(), nil); e != nil || strings.Contains(strings.Join(v.Warnings, "|"), "process list") {
		t.Fatal("a probe without a process section must not warn about it:", v.Warnings, e)
	}
}
