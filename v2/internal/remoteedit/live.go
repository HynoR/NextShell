package remoteedit

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"nextshell.dev/v2/internal/sftpfs"
)

// LiveState is where a live edit stands.
type LiveState int

const (
	Watching LiveState = iota // the local copy has not been saved yet
	Syncing                   // a save is being uploaded
	Synced                    // the last save is on the server
	Conflict                  // the server's file changed since it was opened: the user decides
	Failed                    // the last upload failed; the next save retries
)

// Live is one remote file open in an outside editor. Its local copy is watched until the user stops
// it, its session closes or the app quits; whether the editor is still running does not matter.
type Live struct {
	ID, Session, Path, Local string
	State                    LiveState
	Err                      string
}

// WithSFTP runs fn with an SFTP client of session.
type WithSFTP func(ctx context.Context, session string, fn func(*sftpfs.Client) error) error

// LiveEdits is the registry of live edits.
type LiveEdits struct {
	watch    WatchFactory
	with     WithSFTP
	changed  func()             // the list changed
	retained func(local string) // a stopped edit kept unsent changes at local
	mu       sync.Mutex
	edits    []*liveEdit
	wg       sync.WaitGroup
	next     int
}

type liveEdit struct {
	Live
	doc    Document
	cancel context.CancelFunc
	busy   sync.Mutex // one upload or resolution at a time
}

func NewLiveEdits(watch WatchFactory, with WithSFTP, changed func(), retained func(string)) *LiveEdits {
	return &LiveEdits{watch: watch, with: with, changed: changed, retained: retained}
}

// Start opens remote into a private local copy and watches it; the caller opens the copy in an editor.
// A file already being edited returns its entry.
func (r *LiveEdits) Start(ctx context.Context, session, remote string) (Live, error) {
	r.mu.Lock()
	for _, l := range r.edits {
		if l.Session == session && l.Path == remote {
			r.mu.Unlock()
			return l.Live, nil
		}
	}
	r.mu.Unlock()
	var doc Document
	open, cancelOpen := context.WithTimeout(ctx, 30*time.Second)
	e := r.with(open, session, func(c *sftpfs.Client) (e error) { doc, e = Open(c, remote); return })
	cancelOpen()
	if e != nil {
		return Live{}, e
	}
	dir, e := os.MkdirTemp("", "nextshell-edit-*")
	if e != nil {
		return Live{}, e
	}
	local := filepath.Join(dir, filepath.Base(remote))
	if e = os.WriteFile(local, []byte(doc.Text), 0600); e != nil {
		os.RemoveAll(dir)
		return Live{}, e
	}
	ctx, cancel := context.WithCancel(ctx)
	w, e := r.watch(ctx, local)
	if e != nil {
		cancel()
		os.RemoveAll(dir)
		return Live{}, e
	}
	r.mu.Lock()
	r.next++
	l := &liveEdit{Live: Live{ID: "edit-" + strconv.Itoa(r.next), Session: session, Path: remote, Local: local}, doc: doc, cancel: cancel}
	r.edits = append(r.edits, l)
	r.wg.Add(1)
	started := l.Live
	r.mu.Unlock()
	go r.run(ctx, l, w)
	r.notify()
	return started, nil
}

// run syncs each save until the edit stops, then removes the local copy unless it holds unsent changes.
func (r *LiveEdits) run(ctx context.Context, l *liveEdit, w Watcher) {
	defer r.wg.Done()
	for {
		e := w.Next(ctx)
		if ctx.Err() != nil {
			break
		}
		if e != nil {
			r.set(l, Failed, e) // the copy itself went away: nothing more to watch
			<-ctx.Done()
			break
		}
		r.sync(ctx, l)
	}
	w.Close()
	l.busy.Lock() // a resolution still running finishes first
	defer l.busy.Unlock()
	r.mu.Lock()
	st := l.State
	r.mu.Unlock()
	if st == Conflict || st == Failed {
		if r.retained != nil {
			r.retained(l.Local)
		}
		return
	}
	os.RemoveAll(filepath.Dir(l.Local))
}

func (r *LiveEdits) notify() {
	if r.changed != nil {
		r.changed()
	}
}

func (r *LiveEdits) set(l *liveEdit, s LiveState, e error) {
	r.mu.Lock()
	l.State, l.Err = s, ""
	if e != nil {
		l.Err = e.Error()
	}
	r.mu.Unlock()
	r.notify()
}

func readLocal(local string) (string, error) {
	info, e := os.Lstat(local)
	if e != nil {
		return "", e
	}
	if !info.Mode().IsRegular() || info.Size() > MaxSize {
		return "", errors.New("the local copy is not a text file up to 8 MiB")
	}
	f, e := os.Open(local)
	if e != nil {
		return "", e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, MaxSize+1))
	return string(b), e
}

// sync uploads the local copy when it differs from what the server had.
func (r *LiveEdits) sync(ctx context.Context, l *liveEdit) {
	l.busy.Lock()
	defer l.busy.Unlock()
	text, e := readLocal(l.Local)
	if e != nil {
		r.set(l, Failed, e)
		return
	}
	r.mu.Lock()
	doc, st := l.doc, l.State
	r.mu.Unlock()
	if hash([]byte(text)) == doc.Hash {
		if st != Conflict {
			r.set(l, Synced, nil)
		}
		return
	}
	r.set(l, Syncing, nil)
	r.upload(ctx, l, func(c *sftpfs.Client) (Document, error) { return Save(ctx, c, doc, text) })
}

// upload runs save against the server and records the outcome.
func (r *LiveEdits) upload(ctx context.Context, l *liveEdit, save func(*sftpfs.Client) (Document, error)) {
	var next Document
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	r.mu.Lock()
	session := l.Session // Move may change it
	r.mu.Unlock()
	e := r.with(ctx, session, func(c *sftpfs.Client) (e error) { next, e = save(c); return })
	switch {
	case errors.Is(e, ErrConflict):
		r.set(l, Conflict, nil)
	case e != nil:
		r.set(l, Failed, e)
	default:
		r.mu.Lock()
		l.doc = next
		r.mu.Unlock()
		r.set(l, Synced, nil)
	}
}

func (r *LiveEdits) find(id string) *liveEdit {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, l := range r.edits {
		if l.ID == id {
			return l
		}
	}
	return nil
}

// Overwrite resolves a conflict with the local copy: it replaces whatever the server has now.
func (r *LiveEdits) Overwrite(ctx context.Context, id string) {
	l := r.find(id)
	if l == nil {
		return
	}
	l.busy.Lock()
	defer l.busy.Unlock()
	text, e := readLocal(l.Local)
	if e != nil {
		r.set(l, Failed, e)
		return
	}
	r.set(l, Syncing, nil)
	r.upload(ctx, l, func(c *sftpfs.Client) (Document, error) {
		current, e := Open(c, l.Path)
		if e != nil {
			return current, e
		}
		return Save(ctx, c, current, text)
	})
}

// Reload resolves a conflict with the server's file: the local copy is replaced by it.
func (r *LiveEdits) Reload(ctx context.Context, id string) {
	l := r.find(id)
	if l == nil {
		return
	}
	l.busy.Lock()
	defer l.busy.Unlock()
	r.set(l, Syncing, nil)
	r.upload(ctx, l, func(c *sftpfs.Client) (Document, error) {
		d, e := Open(c, l.Path)
		if e == nil {
			e = os.WriteFile(l.Local, []byte(d.Text), 0600)
		}
		return d, e
	})
}

// SaveCopy writes the local copy to dest, keeping the user's version outside the private folder.
func (r *LiveEdits) SaveCopy(id, dest string) error {
	l := r.find(id)
	if l == nil {
		return errors.New("the live edit has stopped")
	}
	text, e := readLocal(l.Local)
	if e != nil {
		return e
	}
	return os.WriteFile(dest, []byte(text), 0600)
}

// List returns the edits in the order they started.
func (r *LiveEdits) List() []Live {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Live, len(r.edits))
	for i, l := range r.edits {
		out[i] = l.Live
	}
	return out
}

// Stop ends the edit id; "" ends all of them.
func (r *LiveEdits) Stop(id string) { r.stop(func(l *liveEdit) bool { return id == "" || l.ID == id }) }

// StopSession ends the edits of a session that closed.
func (r *LiveEdits) StopSession(session string) {
	r.stop(func(l *liveEdit) bool { return l.Session == session })
}

// Move hands the edits of a session to the one that replaced it on reconnect.
func (r *LiveEdits) Move(from, to string) {
	r.mu.Lock()
	for _, l := range r.edits {
		if l.Session == from {
			l.Session = to
		}
	}
	r.mu.Unlock()
}

func (r *LiveEdits) stop(match func(*liveEdit) bool) {
	r.mu.Lock()
	kept := r.edits[:0]
	var stopped []*liveEdit
	for _, l := range r.edits {
		if match(l) {
			stopped = append(stopped, l)
		} else {
			kept = append(kept, l)
		}
	}
	r.edits = kept
	r.mu.Unlock()
	for _, l := range stopped {
		l.cancel()
	}
	if len(stopped) > 0 {
		r.notify()
	}
}

// Close stops every edit and waits until their copies are cleaned up.
func (r *LiveEdits) Close() {
	r.Stop("")
	r.wg.Wait()
}
