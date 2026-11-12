package appui_test

import (
	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/sftpfs"
	"testing"
)

func TestOfficialFileMultiselectionAndClipboardAcrossSessions(t *testing.T) {
	a, base, tt := fixture(t)
	p := &clipboardPlatform{fakePlatform: base}
	a.P = p
	a.ShowWorkbench = false
	p.events = []appui.Event{{Session: &appui.Session{ID: "one", Title: "one", Terminal: newTerminal(t, "")}}}
	tt.Frame()
	a.Page = "files"
	a.Phase2.FilesSession = "one"
	a.Phase2.RemotePath = "/src"
	a.Phase2.Files = []sftpfs.Entry{{Name: "a", Path: "/src/a"}, {Name: "folder", Path: "/src/folder", Directory: true}}
	tt.Frame()
	if err := tt.ClickWith(ui.Cmd, "选择 a"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ClickWith(ui.Cmd, "打开 folder"); err != nil {
		t.Fatal(err)
	}
	if a.Phase2.FileSelection.Len() != 2 {
		t.Fatal("official multiselect", a.Phase2.FileSelection.Len())
	}
	tt.Key(ui.Cmd, ui.KeyC)
	p.events = []appui.Event{{Session: &appui.Session{ID: "two", Title: "two", Terminal: newTerminal(t, "")}}}
	tt.Frame()
	a.Page = "files"
	a.Phase2.FilesSession = "two"
	a.Phase2.RemotePath = "/dest"
	a.Phase2.Files = nil
	tt.Frame()
	click(t, tt, "粘贴到当前目录")
	if len(p.queued) != 2 {
		t.Fatal("queue count", p.queued)
	}
	for _, r := range p.queued {
		if r.Kind != "copy" || r.Session != "one" || r.TargetSession != "two" {
			t.Fatal(r)
		}
	}
}

type clipboardPlatform struct {
	*fakePlatform
	queued []appui.TransferRequest
}

func (p *clipboardPlatform) QueueTransfer(r appui.TransferRequest) (string, error) {
	p.queued = append(p.queued, r)
	return "queued", nil
}
