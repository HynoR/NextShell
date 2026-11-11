package desktop

import (
	"github.com/egoist/mygo/plugins/terminal"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/model"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreferencesConfigureOfficialTerminal(t *testing.T) {
	p := desktopFixture(t)
	v := p.Preferences()
	v.FontSize = 18
	v.FontFamily = "monospace"
	v.LineHeight = 1.3
	v.Cursor = "bar"
	v.CursorBlink = false
	v.ScrollbackMiB = 20
	v.ClipboardReadAsk = true
	v.ClipboardWrite = true
	v.LocalShell = "/bin/sh"
	v.Notifications = false
	if e := p.SavePreferences(v); e != nil {
		t.Fatal(e)
	}
	o := p.terminalOptions(terminal.Options{})
	if o.Font.Size != 18 || o.Font.LineHeight != 1.3 || o.Font.Family != "monospace" || o.Cursor != terminal.CursorBar || !o.NoBlink || o.Scrollback != 20<<20 || o.NoClipboardWrite || o.ClipboardRead != terminal.ClipboardAsk || len(o.Command) != 1 || o.Command[0] != "/bin/sh" {
		t.Fatal(o)
	}
	o.OnBell()
	o.OnNotify("title", "text")
	for _, e := range p.Poll() {
		if e.Message != "" {
			t.Fatal("disabled notification emitted", e.Message)
		}
	}
	v.Notifications = true
	if e := p.SavePreferences(v); e != nil {
		t.Fatal(e)
	}
	o.OnBell()
	if len(p.Poll()) != 1 {
		t.Fatal("live notify preference not applied")
	}
}
func TestSFTPCreateDoesNotOverwrite(t *testing.T) {
	p := desktopFixture(t)
	id, root := phase2SSH(t, p)
	file := filepath.Join(root, "new file.txt")
	p.FileAction(id, "create", file, "")
	awaitEvent(t, p, func(e appui.Event) bool { return strings.Contains(e.Message, "create completed") })
	if b, e := os.ReadFile(file); e != nil || len(b) != 0 {
		t.Fatal(e, string(b))
	}
	if e := os.WriteFile(file, []byte("retain"), 0600); e != nil {
		t.Fatal(e)
	}
	p.FileAction(id, "create", file, "")
	awaitEvent(t, p, func(e appui.Event) bool { return e.Message != "" })
	if b, e := os.ReadFile(file); e != nil || string(b) != "retain" {
		t.Fatal("overwritten", e, string(b))
	}
}

func TestDiagnosticPlatformCancellationAndResultIdentity(t *testing.T) {
	p := desktopFixture(t)
	file := filepath.Join(t.TempDir(), "nexttrace")
	if e := os.WriteFile(file, []byte("#!/bin/sh\nexec sleep 30\n"), 0700); e != nil {
		t.Fatal(e)
	}
	prefs := p.Preferences()
	prefs.NexttracePath = file
	if e := p.SavePreferences(prefs); e != nil {
		t.Fatal(e)
	}
	id := p.Diagnose("traceroute", "127.0.0.1")
	p.CancelDiagnostic()
	ev := awaitEvent(t, p, func(e appui.Event) bool { return e.Diagnostic != nil })
	if ev.Diagnostic.ID != id || !strings.Contains(ev.Diagnostic.Text, "canceled") {
		t.Fatal(ev.Diagnostic)
	}
	id = p.Diagnose("unknown", "127.0.0.1")
	ev = awaitEvent(t, p, func(e appui.Event) bool { return e.Diagnostic != nil })
	if ev.Diagnostic.ID != id || !strings.Contains(ev.Diagnostic.Text, "未知诊断类型") {
		t.Fatal(ev.Diagnostic)
	}
}

func TestKeepalivePreferenceInheritance(t *testing.T) {
	v := model.DefaultPreferences()
	off, on := false, true
	interval := 45
	for _, tc := range []struct {
		name   string
		c      model.Connection
		global bool
		want   int
	}{{"global", model.Connection{}, true, 15}, {"globalOff", model.Connection{}, false, 0}, {"explicitOff", model.Connection{KeepaliveEnabled: &off}, true, 0}, {"explicitOn", model.Connection{KeepaliveEnabled: &on, KeepaliveIntervalSec: &interval}, false, 45}, {"intervalOverride", model.Connection{KeepaliveIntervalSec: &interval}, true, 45}, {"legacy", model.Connection{Keepalive: 30}, false, 30}} {
		t.Run(tc.name, func(t *testing.T) {
			v.KeepaliveEnabled = tc.global
			if got := effectiveKeepalive(tc.c, v); got != tc.want {
				t.Fatal(got)
			}
			if tc.name == "global" {
				v.KeepaliveSeconds = 20
				if got := effectiveKeepalive(tc.c, v); got != 20 {
					t.Fatal("reconnect froze policy", got)
				}
				v.KeepaliveSeconds = 15
			}
		})
	}
}

func TestTerminalLinksConsumeDefaultAndUseInjectedOpener(t *testing.T) {
	p := desktopFixture(t)
	o := p.terminalOptions(terminal.Options{})
	if !o.OpenLink("https://example.invalid/path") {
		t.Fatal("default opener not consumed")
	}
	e := awaitEvent(t, p, func(e appui.Event) bool { return e.Link != "" })
	if e.Link != "https://example.invalid/path" {
		t.Fatal(e.Link)
	}
	calls := 0
	p.SetURLopener(func(string) { calls++ })
	if err := p.OpenURL("file:///etc/passwd"); err == nil || calls != 0 {
		t.Fatal("unsafe opened")
	}
	if err := p.OpenURL(e.Link); err != nil || calls != 1 {
		t.Fatal(err, calls)
	}
}
