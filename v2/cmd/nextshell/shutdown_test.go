package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"nextshell.dev/v2/internal/desktop"
)

func TestSignalQuitsWithoutAskingAndCleansUpOnce(t *testing.T) {
	var cleaned, quits atomic.Int32
	s := &shutdown{cleanup: func() { cleaned.Add(1) }}
	sigs := make(chan os.Signal, 1)
	quit := make(chan struct{})
	exit := make(chan int, 1)
	s.Watch(sigs, func() { quits.Add(1); close(quit) }, 50*time.Millisecond, func(code int) { exit <- code })
	if s.Terminating() {
		t.Fatal("terminating before any signal")
	}
	sigs <- syscall.SIGTERM
	select {
	case <-quit:
	case <-time.After(2 * time.Second):
		t.Fatal("no quit")
	}
	if !s.Terminating() {
		t.Fatal("window close handlers would still ask")
	}
	// The UI thread never finished (stuck): the watchdog cleans up itself and exits with 128+SIGTERM.
	select {
	case code := <-exit:
		if code != 128+int(syscall.SIGTERM) || cleaned.Load() != 1 {
			t.Fatal(code, cleaned.Load())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no forced exit")
	}
	s.Finish() // OnQuit and the end of main run it too
	if cleaned.Load() != 1 || quits.Load() != 1 {
		t.Fatal("cleanup ran", cleaned.Load(), "times")
	}
}

// The real app with two restored sessions open: SIGTERM must end it (no confirmation dialog) in a few
// seconds and leave the restore-tabs list saved. It opens a window, so it only runs when asked:
//
//	NEXTSHELL_GUI_TESTS=1 go test ./cmd/nextshell -run SIGTERM
func TestSIGTERMEndsTheRunningAppWithSessionsOpen(t *testing.T) {
	if os.Getenv("NEXTSHELL_GUI_TESTS") == "" || runtime.GOOS == "windows" {
		t.Skip("opens a window; set NEXTSHELL_GUI_TESTS=1 (Unix)")
	}
	dir := t.TempDir()
	bin, db := filepath.Join(dir, "nextshell"), filepath.Join(dir, "t.db")
	if out, e := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); e != nil {
		t.Fatal(e, string(out))
	}
	p, e := desktop.Open(db)
	if e != nil {
		t.Fatal(e)
	}
	prefs := p.Preferences()
	prefs.RestoreTabs = true
	if e = p.SavePreferences(prefs); e != nil {
		t.Fatal(e)
	}
	// Two local shells and an id that no longer exists: the saved list must come back without the stale one.
	if e = p.SaveUIState("tabs", "local\nstale-id\nlocal"); e != nil {
		t.Fatal(e)
	}
	p.Shutdown()

	cmd := exec.Command(bin, "--db", db)
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	time.Sleep(4 * time.Second) // window up, tabs restored
	select {
	case e := <-exited:
		t.Fatal("the app ended by itself:", e)
	default:
	}
	start := time.Now()
	cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-exited:
		t.Logf("exited %v after SIGTERM", time.Since(start))
	case <-time.After(8 * time.Second):
		cmd.Process.Kill()
		t.Fatal("still running 8 s after SIGTERM: the quit confirmation is waiting for nobody")
	}
	p, e = desktop.Open(db)
	if e != nil {
		t.Fatal(e)
	}
	defer p.Shutdown()
	if got := p.UIState("tabs"); got != "local\nlocal" {
		t.Fatalf("restore-tabs list not saved on exit: %q", got)
	}
}
