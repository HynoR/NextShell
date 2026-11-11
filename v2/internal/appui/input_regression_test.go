package appui_test

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/appui"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestNativeCommitRequestsFrameAndPublishes(t *testing.T) {
	value := ""
	tt := ui.NewTester(func(c *ui.Context) { ui.TextInput(c, &value).Label("typing").AutoFocus() }, 800, 200)
	tt.TypeNative("abc中文")
	if value != "abc中文" {
		t.Fatalf("native commit invisible until unrelated event: %q", value)
	}
}
func TestNativeBurstPreservesOrder(t *testing.T) {
	value := ""
	tt := ui.NewTester(func(c *ui.Context) { ui.TextInput(c, &value).AutoFocus() }, 800, 200)
	want := strings.Repeat("abc中文0123", 50)
	tt.Burst(func() {
		for _, r := range want {
			tt.TypeNative(string(r))
		}
	})
	tt.Frame()
	if value != want {
		t.Fatalf("order/drop: got %d bytes want %d", len(value), len(want))
	}
}
func TestInputTimingProbe(t *testing.T) {
	if testing.Short() {
		t.Skip("timing probe")
	}
	for _, native := range []bool{false, true} {
		value := ""
		tt := ui.NewTester(func(c *ui.Context) { ui.TextInput(c, &value).AutoFocus() }, 800, 200)
		start := time.Now()
		visible := 0
		for i := 0; i < 200; i++ {
			if native {
				tt.TypeNative("x")
			} else {
				tt.Type("x")
			}
			if len(value) == i+1 {
				visible++
			}
		}
		elapsed := time.Since(start)
		tt.Frame()
		fmt.Printf("INPUT_PROBE native=%t chars=200 published_without_extra_event=%d final=%d elapsed_ms=%.3f per_key_ms=%.4f\n", native, visible, len(value), float64(elapsed)/1e6, float64(elapsed)/200e6)
	}
}

func TestFullAppTypingTimingProbe(t *testing.T) {
	for _, open := range []bool{false, true} {
		a, p, tt := fixture(t)
		p.events = []appui.Event{{Session: &appui.Session{ID: "typing-profile", Title: "start", Local: true, Terminal: newTerminal(t, "hello")}}}
		tt.Frame()
		tt.SetInspectorEnabled(open)
		if open {
			tt.Key(ui.Ctrl|ui.Alt, ui.KeyF12)
		}
		click(t, tt, "标签名称")
		tt.Key(ui.Ctrl, ui.KeyA)
		want := strings.Repeat("Ab09", 50)
		samples := make([]float64, 0, len(want))
		start := time.Now()
		for _, r := range want {
			before := time.Now()
			tt.TypeNative(string(r))
			samples = append(samples, float64(time.Since(before))/1e6)
		}
		elapsed := time.Since(start)
		sort.Float64s(samples)
		if a.TabName != want {
			t.Fatalf("full App input lost/reordered %d chars", len(a.TabName))
		}
		fmt.Printf("FULL_APP_PROBE inspector_open=%t chars=%d exact=true elapsed_ms=%.3f p50_ms=%.3f p95_ms=%.3f max_ms=%.3f\n", open, len(want), float64(elapsed)/1e6, samples[len(samples)/2], samples[len(samples)*95/100], samples[len(samples)-1])
	}
}
