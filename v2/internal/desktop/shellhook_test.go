package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nextshell.dev/v2/internal/appui"
)

// inbox is awaitEvent that keeps the events a Poll returned beyond the one asked for: on a fast machine the
// session and the hook's verdict arrive in the same batch.
type inbox struct {
	t    *testing.T
	p    *Platform
	held []appui.Event
}

func (b *inbox) await(pick func(appui.Event) bool) appui.Event {
	b.t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		b.held = append(b.held, b.p.Poll()...)
		for i, e := range b.held {
			if pick(e) {
				b.held = append(b.held[:i], b.held[i+1:]...)
				return e
			}
		}
	}
	b.t.Fatal("event timeout")
	return appui.Event{}
}

func autoIntegration(t *testing.T, mode string) *Platform {
	t.Helper()
	p := desktopFixture(t)
	prefs := p.Preferences()
	prefs.ShellIntegration = mode
	if e := p.SavePreferences(prefs); e != nil {
		t.Fatal(e)
	}
	return p
}

// The hook is installed and typed by the platform alone: this test has no window, so no frame ever runs.
// (The old code did it from the window's frame loop, which a hidden window does not run.)
func TestAutoShellIntegrationTypesTheLineAtThePromptWithoutAWindow(t *testing.T) {
	t.Setenv("SHELL", "/bin/bash")
	p := autoIntegration(t, "auto")
	server := testServer(t)
	p.Connect(server.Options().Connection)
	s := awaitEvent(t, p, func(e appui.Event) bool { return e.Session != nil }).Session
	t.Cleanup(func() { p.CloseSession(s.ID) })
	file := filepath.Join(server.Root, ".cache/nextshell/osc7.sh")
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if _, e := os.Stat(file); e == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the server never got the hook file")
		}
	}
	time.Sleep(300 * time.Millisecond) // the file is there, the prompt is not: nothing may be typed
	if strings.Contains(s.Terminal.Text(), "osc7.sh") {
		t.Fatal("typed before the prompt:", s.Terminal.Text())
	}
	s.Terminal.Feed([]byte("tester@host:~$ "))
	for deadline := time.Now().Add(5 * time.Second); !strings.Contains(s.Terminal.Text(), ". ~/.cache/nextshell/osc7.sh"); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the line was not typed at the prompt:", s.Terminal.Text())
		}
	}
	// once only
	time.Sleep(300 * time.Millisecond)
	if n := strings.Count(s.Terminal.Text(), ". ~/.cache/nextshell/osc7.sh"); n != 1 {
		t.Fatal("typed", n, "times")
	}
}

// A reconnect starts a new shell, so it gets the line again; the file is on the server already and is not rewritten.
func TestAutoShellIntegrationRunsForReconnectedSessionsToo(t *testing.T) {
	t.Setenv("SHELL", "/bin/bash")
	p := autoIntegration(t, "auto")
	server := testServer(t)
	events := &inbox{t: t, p: p}
	p.Connect(server.Options().Connection)
	first := events.await(func(e appui.Event) bool { return e.Session != nil }).Session
	t.Cleanup(func() { p.CloseSession(first.ID) })
	line := ". ~/.cache/nextshell/osc7.sh"
	first.Terminal.Feed([]byte("tester@host:~$ "))
	for deadline := time.Now().Add(5 * time.Second); !strings.Contains(first.Terminal.Text(), line); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the first session did not get the line:", first.Terminal.Text())
		}
	}
	file := filepath.Join(server.Root, ".cache/nextshell/osc7.sh")
	old := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
	if e := os.Chtimes(file, old, old); e != nil {
		t.Fatal(e)
	}

	p.Reconnect(first)
	next := events.await(func(e appui.Event) bool { return e.Session != nil && e.Session.Replaces == first.ID }).Session
	t.Cleanup(func() { p.CloseSession(next.ID) })
	time.Sleep(300 * time.Millisecond) // no prompt yet: nothing typed
	if strings.Contains(next.Terminal.Text(), "osc7.sh") {
		t.Fatal("typed before the prompt:", next.Terminal.Text())
	}
	next.Terminal.Feed([]byte("tester@host:~$ "))
	for deadline := time.Now().Add(5 * time.Second); !strings.Contains(next.Terminal.Text(), line); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the reconnected session did not get the line:", next.Terminal.Text())
		}
	}
	if fi, e := os.Stat(file); e != nil || !fi.ModTime().Equal(old) {
		t.Fatal("the hook file was rewritten although its content is the same", e)
	}
}

func TestAutoShellIntegrationReportsAndTypesNothingWhenTheServerCannotOrNoPromptShows(t *testing.T) {
	for name, tc := range map[string]struct {
		shell, want string
		prompt      bool
	}{
		"fish":      {"/usr/bin/fish", "not bash or zsh", true},
		"no prompt": {"/bin/bash", "没有等到提示符", false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("SHELL", tc.shell)
			defer func(d time.Duration) { hookBudget = d }(hookBudget)
			hookBudget = 400 * time.Millisecond
			p := autoIntegration(t, "auto")
			events := &inbox{t: t, p: p}
			p.Connect(testServer(t).Options().Connection)
			s := events.await(func(e appui.Event) bool { return e.Session != nil }).Session
			t.Cleanup(func() { p.CloseSession(s.ID) })
			if tc.prompt {
				s.Terminal.Feed([]byte("tester@host:~$ "))
			}
			r := events.await(func(e appui.Event) bool { return e.ShellHook != nil }).ShellHook
			if r.Session != s.ID || r.Err == nil || !strings.Contains(r.Err.Error(), tc.want) {
				t.Fatalf("%+v", r)
			}
			time.Sleep(100 * time.Millisecond)
			if strings.Contains(s.Terminal.Text(), "osc7.sh") {
				t.Fatal("typed into the session:", s.Terminal.Text())
			}
		})
	}
}

func TestShellIntegrationManualOffAndLocalLeaveTheSessionAlone(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	for _, mode := range []string{"", "off"} {
		p := autoIntegration(t, mode)
		server := testServer(t)
		p.Connect(server.Options().Connection)
		s := awaitEvent(t, p, func(e appui.Event) bool { return e.Session != nil }).Session
		s.Terminal.Feed([]byte("tester@host:~$ "))
		time.Sleep(300 * time.Millisecond)
		if server.Execs.Load() != 0 || strings.Contains(s.Terminal.Text(), "osc7.sh") {
			t.Fatalf("mode %q touched the server or the terminal", mode)
		}
		p.CloseSession(s.ID)
	}
	p := autoIntegration(t, "auto")
	p.Local()
	l := awaitEvent(t, p, func(e appui.Event) bool { return e.Session != nil }).Session
	time.Sleep(300 * time.Millisecond)
	if strings.Contains(l.Terminal.Text(), "osc7.sh") {
		t.Fatal("a local shell got the hook")
	}
}
