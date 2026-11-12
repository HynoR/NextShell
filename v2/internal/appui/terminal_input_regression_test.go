package appui_test

import (
	"bytes"
	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
	"io"
	"nextshell.dev/v2/internal/appui"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

type captureConn struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	closed chan struct{}
	once   sync.Once
}

func (c *captureConn) Read([]byte) (int, error) { <-c.closed; return 0, io.EOF }
func (c *captureConn) Write(b []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.Write(b)
}
func (c *captureConn) Close() error { c.once.Do(func() { close(c.closed) }); return nil }
func (c *captureConn) text() string { c.mu.Lock(); defer c.mu.Unlock(); return c.buf.String() }
func TestTerminalClipboardKeysAndOrderedBurst(t *testing.T) {
	conn := &captureConn{closed: make(chan struct{})}
	term, e := terminal.New(terminal.Options{Conn: conn, NoBlink: true})
	if e != nil {
		t.Fatal(e)
	}
	defer term.Close()
	term.Feed([]byte("COPY_ME"))
	tt := ui.NewTester(func(c *ui.Context) { terminal.View(c, term).Fill().AutoFocus() }, 800, 200)
	tt.Command("selectAll")
	// The terminal plugin's copy/paste chords: Cmd on macOS, Ctrl+Shift elsewhere.
	chord := ui.Ctrl | ui.Shift
	if runtime.GOOS == "darwin" {
		chord = ui.Super
	}
	tt.Key(chord, ui.KeyC)
	if !strings.Contains(tt.Clipboard(), "COPY_ME") {
		t.Fatal("terminal copy intercepted", tt.Clipboard())
	}
	tt.SetClipboard("PASTE_ME")
	tt.Key(chord, ui.KeyV)
	want := "PASTE_ME" + strings.Repeat("abc0123", 100) + "\r"
	for _, r := range strings.Repeat("abc0123", 100) {
		tt.Type(string(r))
	}
	tt.Key(0, ui.KeyEnter)
	until := time.Now().Add(2 * time.Second)
	for len(conn.text()) < len(want) && time.Now().Before(until) {
		time.Sleep(time.Millisecond)
	}
	if conn.text() != want {
		t.Fatalf("lost/reordered: got %d bytes want %d", len(conn.text()), len(want))
	}
}

// Bare Ctrl+letters belong to the shell: no app shortcut may take one.
func TestBareCtrlLettersReachTheShell(t *testing.T) {
	_, p, tt := fixture(t)
	conn := &captureConn{closed: make(chan struct{})}
	term, e := terminal.New(terminal.Options{Conn: conn, NoBlink: true})
	if e != nil {
		t.Fatal(e)
	}
	defer term.Close()
	p.events = append(p.events, appui.Event{Session: &appui.Session{ID: "t", Title: "shell", Terminal: term, Local: true}})
	tt.Frame()
	keys := []ui.Key{ui.KeyA, ui.KeyB, ui.KeyC, ui.KeyD, ui.KeyE, ui.KeyF, ui.KeyG, ui.KeyH, ui.KeyI, ui.KeyJ, ui.KeyK, ui.KeyL, ui.KeyM, ui.KeyN, ui.KeyO, ui.KeyP, ui.KeyQ, ui.KeyR, ui.KeyS, ui.KeyT, ui.KeyU, ui.KeyV, ui.KeyW, ui.KeyX, ui.KeyY, ui.KeyZ}
	for _, k := range keys {
		tt.Key(ui.Ctrl, k)
	}
	until := time.Now().Add(2 * time.Second)
	for len(conn.text()) < len(keys) && time.Now().Before(until) {
		time.Sleep(time.Millisecond)
	}
	if got := conn.text(); len(got) != len(keys) || got[0] != 1 || got[len(got)-1] != 26 {
		t.Fatalf("shell got %q", got)
	}
	if tt.HasText("搜索主机、标签、片段和操作，或输入 user@host") || p.local != 0 || len(p.closed) != 0 {
		t.Fatal("an app shortcut took a Ctrl+letter")
	}
}
