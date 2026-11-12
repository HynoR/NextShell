package desktop

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"nextshell.dev/v2/internal/remoteedit"
	"nextshell.dev/v2/internal/testutil"
)

// The editor quits right after opening the file (as `open -t` does): later saves to the copy still go up.
func TestLiveEditOutlivesTheEditor(t *testing.T) {
	testutil.RequirePOSIXRemote(t)
	p := desktopFixture(t)
	session, root := phase2SSH(t, p)
	remote := filepath.Join(root, "app.conf")
	os.WriteFile(remote, []byte("port=80\n"), 0600)
	editor := filepath.Join(t.TempDir(), "editor")
	os.WriteFile(editor, []byte("#!/bin/sh\nexit 0\n"), 0700)
	p.ExternalEdit(session, remote, editor, nil)
	var l remoteedit.Live
	await := func(ok func() bool) {
		t.Helper()
		for deadline := time.Now().Add(5 * time.Second); !ok(); time.Sleep(10 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatal("timeout", p.LiveEdits())
			}
		}
	}
	await(func() bool {
		if edits := p.LiveEdits(); len(edits) == 1 {
			l = edits[0]
		}
		return l.Local != ""
	})
	time.Sleep(200 * time.Millisecond) // the editor has exited
	os.WriteFile(l.Local, []byte("port=8080\n"), 0600)
	await(func() bool { b, _ := os.ReadFile(remote); return string(b) == "port=8080\n" })
	await(func() bool { return p.LiveEdits()[0].State == remoteedit.Synced })
	p.CloseSession(session)
	if len(p.LiveEdits()) != 0 {
		t.Fatal("closed session kept its edits")
	}
}
