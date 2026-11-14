package monitor

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestHistoryRetentionAndMissingDeltas(t *testing.T) {
	var h History
	base := time.Unix(1700000000, 0)
	s := Snapshot{At: base, MemoryTotal: 1000, MemoryAvailable: 250, Disks: []Disk{{Mount: "/", TotalKB: 100, UsedPercent: 60}}}
	if !h.Append(s) {
		t.Fatal("first rejected")
	}
	if !math.IsNaN(h.Points[0].Value("cpu")) || !math.IsNaN(h.Points[0].Value("rx")) || h.Points[0].Value("memory") != 75 || h.Points[0].Value("disk:/") != 60 {
		t.Fatal(h.Points[0])
	}
	if h.Append(s) {
		t.Fatal("duplicate accepted")
	}
	s.At = base.Add(-time.Second)
	if h.Append(s) {
		t.Fatal("old accepted")
	}
	for i := 1; i <= 60; i++ {
		s.At = base.Add(time.Duration(i) * time.Second)
		s.HasDelta = true
		s.HasNetworkDelta = true
		s.CPUPercent = float64(i)
		s.RXPerSecond = 2048
		s.Interfaces = map[string]InterfaceRate{"eth0": {HasDelta: true, RXPerSecond: 1024}}
		h.Append(s)
	}
	if len(h.Points) != 50 || h.Points[0].At != base.Add(11*time.Second) || h.Points[49].Value("rx") != 2 || h.Points[49].Value("rx:eth0") != 1 {
		t.Fatal(h.Points)
	}
	s.At = base.Add(61 * time.Second)
	s.HasDelta = false
	s.HasNetworkDelta = false
	s.Interfaces = nil
	h.Append(s)
	if !math.IsNaN(h.Points[49].Value("cpu")) || !math.IsNaN(h.Points[49].Value("rx:eth0")) {
		t.Fatal("reset created fake zero")
	}
}
func TestHistoryRejectsInvalidNumbers(t *testing.T) {
	var h History
	h.Append(Snapshot{At: time.Now(), HasDelta: true, CPUPercent: math.NaN(), HasNetworkDelta: true, RXPerSecond: math.Inf(1), TXPerSecond: -1})
	for _, v := range h.Points[0].Values {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			t.Fatal(v)
		}
	}
}
func TestPerInterfaceDeltaResetAndTopology(t *testing.T) {
	raw := strings.Replace(probe, "---NS_NETCOUNTERS---\n1000\n2000", "---NS_NETDEV---\neth0: 1000 0 0 0 0 0 0 0 2000 0 0 0 0 0 0 0\nlo: 99999 0 0 0 0 0 0 0 99999", 1)
	at := time.Unix(100, 0)
	a, err := Parse(raw, at, nil)
	if _, lo := a.Interfaces["lo"]; err != nil || a.HasNetworkDelta || len(a.Interfaces) != 2 || !lo || a.RX != 1000 || a.TX != 2000 { // lo is pickable, not counted
		t.Fatal(a, err)
	}
	b, err := Parse(strings.Replace(raw, "eth0: 1000", "eth0: 1200", 1), at.Add(2*time.Second), &a)
	if err != nil || !b.HasNetworkDelta || !b.Interfaces["eth0"].HasDelta || b.Interfaces["eth0"].RXPerSecond != 100 {
		t.Fatal(b, err)
	}
	reset, err := Parse(raw, at.Add(4*time.Second), &b)
	if err != nil || reset.HasNetworkDelta || reset.Interfaces["eth0"].HasDelta {
		t.Fatal(reset, err)
	}
	changed, err := Parse(strings.Replace(raw, "eth0:", "eth1:", 1), at.Add(6*time.Second), &reset)
	if err != nil || changed.HasNetworkDelta {
		t.Fatal(changed, err)
	}
}
