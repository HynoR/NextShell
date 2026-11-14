package instance

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A second launch on the same profile wakes the first and is told to exit; another profile runs beside it.
func TestOneProcessPerProfile(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	dir := t.TempDir()
	db := filepath.Join(dir, "trial.db")
	woke := make(chan bool, 4)
	first := New(db, func() { woke <- true })
	if !first.Take() || !first.Take() {
		t.Fatal("the first launch did not get the lock")
	}
	defer first.Release()
	// The same file named another way is the same profile.
	if second := New(filepath.Join(dir, ".", "x", "..", "trial.db"), nil); second.Take() {
		t.Fatal("a second launch on the same profile was let in")
	}
	select {
	case <-woke:
	case <-time.After(2 * time.Second):
		t.Fatal("the first launch was not asked to show its window")
	}
	other := New(filepath.Join(dir, "other.db"), nil)
	if !other.Take() {
		t.Fatal("another profile was refused")
	}
	other.Release()

	// Once the first quits, the next launch holds the lock.
	first.Release()
	if _, e := os.Stat(first.path); !os.IsNotExist(e) {
		t.Fatal("the socket stayed", e)
	}
	third := New(db, nil)
	if !third.Take() {
		t.Fatal("the lock outlived its process")
	}
	third.Release()
}

// A socket left by a crash does not keep the profile locked.
func TestStaleSocketIsTakenOver(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	db := filepath.Join(t.TempDir(), "crash.db")
	l := New(db, nil)
	ln, e := net.Listen("unix", l.path)
	if e != nil {
		t.Fatal(e)
	}
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	ln.Close() // the file stays, nobody listens
	if _, e := os.Stat(l.path); e != nil {
		t.Fatal("no stale socket to test with", e)
	}
	if !l.Take() {
		t.Fatal("a stale socket kept the profile locked")
	}
	l.Release()
}
