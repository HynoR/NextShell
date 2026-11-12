package appui_test

import (
	"net"
	"testing"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/model"
)

// glassSession is a session whose terminal is made as the app makes them: it paints no background of
// its own, and the view under it does.
func glassSession(t *testing.T, p *fakePlatform, tt *ui.Tester, id, title string, c model.Connection, text string) *appui.Session {
	t.Helper()
	conn, peer := net.Pipe()
	term, e := terminal.New(terminal.Options{Conn: conn, NoClipboardWrite: true, NoBlink: true, Transparent: true})
	if e != nil {
		t.Fatal(e)
	}
	term.Feed([]byte(text))
	t.Cleanup(func() { peer.Close(); term.Close() })
	s := &appui.Session{ID: id, Title: title, Terminal: term, Connection: c, Local: c.Host == ""}
	p.events = append(p.events, appui.Event{Session: s})
	tt.Frame()
	return s
}
