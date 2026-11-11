package desktop

import (
	"nextshell.dev/v2/internal/appui"
	"os"
	"path/filepath"
	"testing"
)

func TestProductionQueueRecursiveCopyAndCut(t *testing.T) {
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
