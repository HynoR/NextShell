package desktop

import (
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/testutil"
	"os"
	"path/filepath"
	"testing"
)

func TestProductionQueueRecursiveCopyAndCut(t *testing.T) {
	testutil.RequirePOSIXRemote(t)
	p := desktopFixture(t)
	src, root := phase2SSH(t, p)
	dst, target := phase2SSH(t, p)
	from := filepath.Join(root, "tree")
	os.MkdirAll(filepath.Join(from, "nested"), 0700)
	os.WriteFile(filepath.Join(from, "nested", "file"), []byte("cross server"), 0600)
	for _, r := range []appui.TransferRequest{{Session: src, TargetSession: dst, Kind: "copy", Remote: from, Destination: filepath.Join(target, "tree")}, {Session: src, TargetSession: src, Kind: "move", Remote: from, Destination: from + "-moved"}} {
		id, err := p.QueueTransfer(r)
		if err != nil {
			t.Fatal(err)
		}
		waitQueue(t, p, id)
	}
	if b, err := os.ReadFile(filepath.Join(target, "tree/nested/file")); err != nil || string(b) != "cross server" {
		t.Fatal(err, string(b))
	}
	if _, err := os.Stat(from); !os.IsNotExist(err) {
		t.Fatal("cut retained source", err)
	}
	if _, err := p.QueueTransfer(appui.TransferRequest{Session: src, TargetSession: dst, Kind: "move", Remote: from + "-moved", Destination: filepath.Join(target, "bad")}); err == nil {
		t.Fatal("cross-server move accepted")
	}
}
func TestCrossFileExplicitOverwriteStillWorks(t *testing.T) {
	testutil.RequirePOSIXRemote(t)
	p := desktopFixture(t)
	src, root := phase2SSH(t, p)
	dst, target := phase2SSH(t, p)
	from, to := filepath.Join(root, "from"), filepath.Join(target, "existing")
	os.WriteFile(from, []byte("new contents"), 0600)
	os.WriteFile(to, []byte("keep unless explicitly allowed"), 0600)
	id, err := p.QueueTransfer(appui.TransferRequest{Session: src, TargetSession: dst, Kind: "cross", Remote: from, Destination: to, Overwrite: true})
	if err != nil {
		t.Fatal(err)
	}
	waitQueue(t, p, id)
	if b, err := os.ReadFile(to); err != nil || string(b) != "new contents" {
		t.Fatal(err, string(b))
	}
}

func TestListFilesFailureEndsInAFailureEvent(t *testing.T) {
	testutil.RequirePOSIXRemote(t)
	p := desktopFixture(t)
	session, root := phase2SSH(t, p)
	gone := filepath.Join(root, "gone")
	p.ListFiles(session, gone)
	ev := awaitEvent(t, p, func(e appui.Event) bool { return e.FilesFailed != nil || e.Files != nil || e.Message != "" })
	if f := ev.FilesFailed; f == nil || f.Session != session || f.Path != gone || f.Err == "" || ev.Message != "" {
		t.Fatal("a listing that fails is its own event, so the pane can show why:", ev)
	}
}
