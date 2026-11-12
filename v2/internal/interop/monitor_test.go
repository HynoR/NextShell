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
			first, err := monitor.Collect(ctx, s, nil, false)
			if err != nil {
				t.Fatal(err)
			}
			if first.Detail || first.Processes != nil || first.Network != nil || len(first.Warnings) != 0 || first.MemoryTotal == 0 || len(first.Disks) == 0 || first.Load == "" || !strings.Contains(first.Info, "Linux") {
				t.Fatalf("light sample: %+v", first)
			}
			Sh(t, s, "i=0; while [ $i -lt 300000 ]; do i=$((i+1)); done") // burn CPU so the delta is non-zero
			time.Sleep(time.Second)
			snap, err := monitor.Collect(ctx, s, &first, true) // light -> full: the rates must still come out
			if err != nil {
				t.Fatal(err)
			}
			if !snap.Detail {
				t.Fatal("full probe returned a light sample")
			}
			if back, err := monitor.Collect(ctx, s, &snap, false); err != nil || back.Detail || back.Processes != nil || back.Network != nil || !back.HasNetworkDelta {
				t.Errorf("full -> light: err=%v %+v", err, back)
			}
			lightRaw, _ := TrySh(s, monitor.LightProbeCommand, 15*time.Second)
			fullRaw, _ := TrySh(s, monitor.ProbeCommand, 15*time.Second)
			t.Logf("%s: light probe %d bytes, full probe %d bytes", v.Name, len(lightRaw), len(fullRaw))
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
	ctx := context.Background()
	// The light probe touches only /proc, df and uname: BusyBox has all of those.
	start := time.Now()
	light, err := monitor.Collect(ctx, s, nil, false)
	t.Logf("light probe on BusyBox: err=%v, took %v", err, time.Since(start).Round(time.Millisecond))
	if err != nil {
		t.Fatalf("light Collect failed on BusyBox: %v", err)
	}
	if light.Detail || light.Processes != nil || light.Network != nil || len(light.Warnings) != 0 || light.MemoryTotal == 0 || len(light.Disks) == 0 || len(light.Interfaces) == 0 {
		t.Errorf("light sample on BusyBox: %+v", light)
	}
	start = time.Now()
	raw, runErr := TrySh(s, monitor.ProbeCommand, 15*time.Second)
	snap, err := monitor.Collect(ctx, s, &light, true)
	t.Logf("full probe on BusyBox: run err=%v, Collect err=%v, took %v", runErr, err, time.Since(start).Round(time.Millisecond))
	if err != nil {
		t.Logf("raw probe output:\n%s", raw)
		t.Fatalf("Collect failed on BusyBox: %v", err)
	}
	t.Logf("cpu/mem ok: mem=%dMiB disks=%d interfaces=%d", snap.MemoryTotal>>20, len(snap.Disks), len(snap.Interfaces))
	t.Logf("processes=%d (ps --sort/etimes unsupported by BusyBox)", len(snap.Processes))
	t.Logf("network sockets=%d warnings=%v", len(snap.Network), snap.Warnings)
	if !snap.Detail || snap.MemoryTotal == 0 || len(snap.Disks) == 0 {
		t.Error("core metrics missing")
	}
	if len(snap.Processes) == 0 && !strings.Contains(strings.Join(snap.Warnings, "|"), "process list unavailable") {
		t.Error("empty process table without an explanation")
	}
}
