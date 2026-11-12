package remoteedit

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"nextshell.dev/v2/internal/sftpfs"
)

// pokeWatch reports a change each time the test sends on it.
type pokeWatch chan struct{}

func (w pokeWatch) Next(ctx context.Context) error {
	select {
	case <-w:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (w pokeWatch) Close() error { return nil }

func TestLiveEditSyncConflictAndStop(t *testing.T) {
	c, root := fixture(t)
	remote := filepath.Join(root, "text")
	os.WriteFile(remote, []byte("initial"), 0600)
	poke := pokeWatch(make(chan struct{}))
	retained := make(chan string, 1)
	r := NewLiveEdits(func(context.Context, string) (Watcher, error) { return poke, nil },
		func(_ context.Context, _ string, fn func(*sftpfs.Client) error) error { return fn(c) }, nil, func(p string) { retained <- p })
	defer r.Close()
	l, e := r.Start(context.Background(), "s", remote)
	if e != nil || l.State != Watching {
		t.Fatal(l, e)
	}
	if again, _ := r.Start(context.Background(), "s", remote); again.ID != l.ID {
		t.Fatal("same file opened twice")
	}
	wait := func(want LiveState) {
		t.Helper()
		for range 200 {
			if got := r.List()[0]; got.State == want {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("state", r.List()[0], "want", want)
	}
	save := func(text string) { os.WriteFile(l.Local, []byte(text), 0600); poke <- struct{}{} }
	read := func(p string) string { b, _ := os.ReadFile(p); return string(b) }

	save("first")
	wait(Synced)
	if read(remote) != "first" {
		t.Fatal(read(remote))
	}
	os.WriteFile(remote, []byte("theirs"), 0600)
	save("mine")
	wait(Conflict)
	if read(remote) != "theirs" {
		t.Fatal("conflict overwrote the server")
	}
	copyTo := filepath.Join(t.TempDir(), "copy")
	if e = r.SaveCopy(l.ID, copyTo); e != nil || read(copyTo) != "mine" {
		t.Fatal(e)
	}
	r.Reload(context.Background(), l.ID)
	wait(Synced)
	if read(l.Local) != "theirs" {
		t.Fatal("reload", read(l.Local))
	}
	os.WriteFile(remote, []byte("theirs again"), 0600)
	save("mine again")
	wait(Conflict)
	r.Overwrite(context.Background(), l.ID)
	wait(Synced)
	if read(remote) != "mine again" {
		t.Fatal("overwrite", read(remote))
	}
	// A stopped edit with unsent changes keeps its copy; a clean one is removed.
	os.WriteFile(remote, []byte("changed"), 0600)
	save("unsent")
	wait(Conflict)
	r.Stop(l.ID)
	if got := <-retained; got != l.Local || read(got) != "unsent" {
		t.Fatal("unsent changes lost", got)
	}
	os.RemoveAll(filepath.Dir(l.Local))
	l, _ = r.Start(context.Background(), "s", remote)
	r.Move("s", "t")
	if r.List()[0].Session != "t" {
		t.Fatal("reconnect lost the edit")
	}
	r.StopSession("t")
	r.Close()
	if len(r.List()) != 0 {
		t.Fatal(r.List())
	}
	if _, e = os.Stat(filepath.Dir(l.Local)); !os.IsNotExist(e) {
		t.Fatal("clean copy left behind")
	}
}
