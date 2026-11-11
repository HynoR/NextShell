package appui_test

import (
	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/appui"
	"testing"
)

type diagnosticPlatform struct {
	*fakePlatform
	kind, target string
	cancelled    bool
}

func (p *diagnosticPlatform) Diagnose(kind, target string) string {
	p.kind, p.target = kind, target
	return "current"
}
func (p *diagnosticPlatform) CancelDiagnostic() { p.cancelled = true }
func TestDiagnosticUIRejectsStaleResultsAndCancels(t *testing.T) {
	_, base, _ := fixture(t)
	p := &diagnosticPlatform{fakePlatform: base}
	a := appui.New(p)
	a.Page = "diagnostics"
	tt := ui.NewTester(a.View, 1400, 1000)
	click(t, tt, "Ping（pro-bing）")
	if p.target != "127.0.0.1" || p.kind != "ping" || !a.Phase2.DiagnosticBusy {
		t.Fatal(p.target, p.kind)
	}
	base.events = []appui.Event{{Diagnostic: &appui.DiagnosticResult{ID: "stale", Text: "old"}}}
	tt.Frame()
	if !a.Phase2.DiagnosticBusy {
		t.Fatal("stale result won")
	}
	click(t, tt, "停止诊断")
	if !p.cancelled {
		t.Fatal("cancel not sent")
	}
	base.events = []appui.Event{{Diagnostic: &appui.DiagnosticResult{ID: "current", Kind: "ping", Text: "permission denied"}}}
	tt.Frame()
	if a.Phase2.DiagnosticBusy || !tt.HasText("ping\npermission denied") {
		t.Fatal(a.Phase2.DiagnosticText)
	}
}
