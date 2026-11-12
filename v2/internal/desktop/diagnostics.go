package desktop

import (
	"context"
	"fmt"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/diagnostics"
	"time"
)

func (p *Platform) Diagnose(kind, target string) string {
	id := p.NewID()
	ctx, cancel := context.WithTimeout(p.ctx, 30*time.Second)
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		cancel()
		return ""
	}
	if p.diagnosticCancel != nil {
		p.diagnosticCancel()
	}
	p.diagnosticCancel = cancel
	p.mu.Unlock()
	binary := p.Preferences().NexttracePath
	p.launch(func() {
		defer cancel()
		var text string
		var err error
		if target, err = diagnostics.Resolve(ctx, target); err != nil {
			p.emit(appui.Event{Diagnostic: &appui.DiagnosticResult{ID: id, Kind: kind, Text: "错误：" + err.Error()}})
			return
		}
		switch kind {
		case "ping":
			var r diagnostics.PingResult
			r, err = diagnostics.Ping(ctx, target)
			text = fmt.Sprintf("发出 %d / 收到 %d · 丢包 %.1f%%\nRTT min / avg / max：%s / %s / %s", r.Sent, r.Received, r.Loss, r.Min, r.Avg, r.Max)
		case "traceroute":
			text, err = diagnostics.Trace(ctx, binary, target)
		default:
			err = fmt.Errorf("未知诊断类型")
		}
		if err != nil {
			text += "\n错误：" + err.Error()
		}
		p.emit(appui.Event{Diagnostic: &appui.DiagnosticResult{ID: id, Kind: kind, Text: text}})
	})
	return id
}
func (p *Platform) CancelDiagnostic() {
	p.mu.Lock()
	cancel := p.diagnosticCancel
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}
