package appui

import (
	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/monitor"
	"testing"
	"time"
)

func TestHistoryNativeChartRenderingAndSessionIsolation(t *testing.T) {
	a := &App{}
	base := time.Unix(1700000000, 0)
	for i := range 6 {
		a.recordMonitorHistory("one", monitor.Snapshot{At: base.Add(time.Duration(i) * 2 * time.Second), HasDelta: i > 0, HasNetworkDelta: i > 0, CPUPercent: float64(10 + i), MemoryTotal: 1024, MemoryAvailable: 512, RXPerSecond: float64(512 * i), TXPerSecond: float64(1024 * i), Interfaces: map[string]monitor.InterfaceRate{"eth0": {HasDelta: i > 0, RXPerSecond: float64(512 * i)}}, Disks: []monitor.Disk{{Mount: "/", TotalKB: 100, UsedPercent: 50}}})
	}
	a.recordMonitorHistory("two", monitor.Snapshot{At: base, MemoryTotal: 100, MemoryAvailable: 80})
	if len(a.Phase2.MonitorHistories["one"].Points) != 6 || a.Phase2.MonitorHistories["two"].Points[0].Value("memory") != 20 {
		t.Fatal("mixed session histories")
	}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Fill().Children(func() { a.monitorHistoryView(c, "one") }) }, 1100, 1000)
	tt.Frame()
	if !tt.HasText("CPU · %") || !tt.HasText("磁盘容量占用 · %（不是 I/O）") {
		t.Fatal(tt.Texts())
	}
	if len(tt.Image().Pix) == 0 {
		t.Fatal("no image")
	}
}
func TestMonitorHistorySessionRetentionBound(t *testing.T) {
	a := &App{}
	for i := range 50 {
		a.recordMonitorHistory(time.Unix(int64(i), 0).String(), monitor.Snapshot{At: time.Unix(int64(i), 0)})
	}
	if len(a.Phase2.MonitorHistories) != 32 {
		t.Fatal(len(a.Phase2.MonitorHistories))
	}
}
