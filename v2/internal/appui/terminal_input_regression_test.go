package appui_test

import (
	"bytes"
	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
	"io"
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
	tt.Key(ui.Ctrl|ui.Shift, ui.KeyC)
	if !strings.Contains(tt.Clipboard(), "COPY_ME") {
		t.Fatal("terminal copy intercepted", tt.Clipboard())
	}
	tt.SetClipboard("PASTE_ME")
	tt.Key(ui.Ctrl|ui.Shift, ui.KeyV)
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
