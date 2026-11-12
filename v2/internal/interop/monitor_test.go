//go:build interop

package interop

import (
	"context"
	"strings"
	"testing"
	"time"

	"nextshell.dev/v2/internal/monitor"
)

func TestMonitorProbeGNU(t *testing.T) {
	for _, v := range []Variant{Debian, Ubuntu} {
		t.Run(v.Name, func(t *testing.T) {
			s := Dial(t, Shared(t, v).Options())
			ctx := context.Background()
			first, err := monitor.Collect(ctx, s, nil)
			if err != nil {
				t.Fatal(err)
			}
			Sh(t, s, "i=0; while [ $i -lt 300000 ]; do i=$((i+1)); done") // burn CPU so the delta is non-zero
			time.Sleep(time.Second)
			snap, err := monitor.Collect(ctx, s, &first)
			if err != nil {
				t.Fatal(err)
			}
			if snap.MemoryTotal == 0 || snap.MemoryAvailable == 0 || len(snap.Disks) == 0 || len(snap.Processes) < 3 {
				t.Fatalf("thin snapshot: mem=%d disks=%d procs=%d", snap.MemoryTotal, len(snap.Disks), len(snap.Processes))
			}
			if !snap.HasDelta || !snap.HasNetworkDelta {
				t.Errorf("deltas missing: cpu=%v net=%v", snap.HasDelta, snap.HasNetworkDelta)
			}
			if !strings.Contains(snap.Info, "Linux") || snap.Load == "" {
				t.Errorf("info=%q load=%q", snap.Info, snap.Load)
			}
			if _, ok := snap.Interfaces["eth0"]; !ok {
				t.Errorf("no eth0 in %v", snap.Interfaces)
			}
			var sshd bool
			for _, n := range snap.Network {
				if n.Listener && n.LocalPort == 22 {
					sshd = true
				}
			}
			if !sshd {
				t.Errorf("no listener on :22 in %d sockets", len(snap.Network))
			}
			var haveSSHD bool
			for _, p := range snap.Processes {
				if strings.Contains(p.Command, "sshd") && p.PID > 0 {
					haveSSHD = true
				}
			}
			if !haveSSHD || len(snap.Warnings) != 0 {
				t.Errorf("sshd process=%v warnings=%v", haveSSHD, snap.Warnings)
			}
			t.Logf("%s: cpu=%.1f%% mem=%d/%d disks=%d procs=%d sockets=%d", v.Name, snap.CPUPercent, snap.MemoryAvailable>>20, snap.MemoryTotal>>20, len(snap.Disks), len(snap.Processes), len(snap.Network))
		})
	}
}

// BusyBox has no `ps --sort`/`etimes` and no ss. Records which parts of the probe survive.
func TestMonitorProbeAlpineBusyBox(t *testing.T) {
	s := Dial(t, Shared(t, Alpine).Options())
	start := time.Now()
	raw, runErr := TrySh(s, monitor.ProbeCommand, 15*time.Second)
	snap, err := monitor.Collect(context.Background(), s, nil)
	t.Logf("probe on BusyBox: run err=%v, Collect err=%v, took %v", runErr, err, time.Since(start).Round(time.Millisecond))
	if err != nil {
		t.Logf("raw probe output:\n%s", raw)
		t.Fatalf("Collect failed on BusyBox: %v", err)
	}
	t.Logf("cpu/mem ok: mem=%dMiB disks=%d interfaces=%d", snap.MemoryTotal>>20, len(snap.Disks), len(snap.Interfaces))
	t.Logf("processes=%d (ps --sort/etimes unsupported by BusyBox)", len(snap.Processes))
	t.Logf("network sockets=%d warnings=%v", len(snap.Network), snap.Warnings)
	if snap.MemoryTotal == 0 || len(snap.Disks) == 0 {
		t.Error("core metrics missing")
	}
	if len(snap.Processes) == 0 && !strings.Contains(strings.Join(snap.Warnings, "|"), "process list unavailable") {
		t.Error("empty process table without an explanation")
	}
}
