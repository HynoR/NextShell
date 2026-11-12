package desktop

import (
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/model"
	"nextshell.dev/v2/internal/testutil"
	"os"
	"path/filepath"
	"testing"
	"time"
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

func TestRemovalIsCountedThenQueuedAndNeverFollowsLinks(t *testing.T) {
	testutil.RequirePOSIXRemote(t)
	p := desktopFixture(t)
	session, root := phase2SSH(t, p)
	dir, outside := filepath.Join(root, "doomed"), filepath.Join(root, "outside")
	os.MkdirAll(filepath.Join(dir, "sub", "deeper"), 0700)
	os.MkdirAll(outside, 0700)
	os.WriteFile(filepath.Join(dir, "a"), []byte("a"), 0600)
	os.WriteFile(filepath.Join(dir, "sub", "b"), []byte("b"), 0600)
	os.WriteFile(filepath.Join(outside, "keep"), []byte("keep"), 0600)
	os.Symlink(outside, filepath.Join(dir, "sub", "link"))
	cancel := p.CountRemoval(session, []string{dir})
	defer cancel()
	count := awaitEvent(t, p, func(e appui.Event) bool { return e.Removal != nil }).Removal
	if count.Err != "" || count.Session != session || len(count.Targets) != 1 || count.Targets[0] != (appui.RemovalTally{Path: dir, Files: 3, Dirs: 3}) {
		t.Fatal(count)
	}
	id, err := p.QueueTransfer(appui.TransferRequest{Session: session, Kind: "remove", Remote: dir, Items: 6})
	if err != nil {
		t.Fatal(err)
	}
	waitQueue(t, p, id)
	for _, j := range p.Transfers() {
		if j.ID == id && (!j.Items || j.Bytes != 6 || j.Total != 6 || j.Label != "删除 test:"+dir) {
			t.Fatal("a deletion's job counts items:", j)
		}
	}
	if _, err = os.Lstat(dir); !os.IsNotExist(err) {
		t.Fatal("folder left behind", err)
	}
	if b, err := os.ReadFile(filepath.Join(outside, "keep")); err != nil || string(b) != "keep" {
		t.Fatal("deleted through a link", err)
	}
	// A count that fails says so, as an event, instead of leaving the confirm waiting.
	p.CountRemoval(session, []string{filepath.Join(root, "never-was")})
	if failed := awaitEvent(t, p, func(e appui.Event) bool { return e.Removal != nil }).Removal; failed.Err == "" {
		t.Fatal("no reason", failed)
	}
	if _, err = p.QueueTransfer(appui.TransferRequest{Session: session, Kind: "remove", Remote: "/"}); err != nil {
		t.Fatal(err)
	}
	var job string
	for _, j := range p.Transfers() {
		job = j.ID
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, j := range p.Transfers() {
			if j.ID == job && j.State == "failed" {
				return
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("the root was not refused")
}

func TestTransferLabelsNameTheServers(t *testing.T) {
	p := desktopFixture(t)
	p.credentials = map[string]model.Connection{"web": {Name: "prod-web-01", Host: "10.0.0.1"}, "db": {Name: "db-master"}, "quick": {Host: "10.9.9.9"}}
	for _, v := range []struct {
		r    appui.TransferRequest
		want string
	}{
		{appui.TransferRequest{Session: "web", Kind: "upload", Local: "/home/me/config.yaml", Remote: "/etc/app/config.yaml"}, "上传 config.yaml → prod-web-01:/etc/app"},
		{appui.TransferRequest{Session: "db", Kind: "download", Remote: "/var/log/syslog", Local: "/tmp/x/syslog"}, "下载 syslog ← db-master:/var/log"},
		{appui.TransferRequest{Session: "web", Kind: "remove", Remote: "/srv/old"}, "删除 prod-web-01:/srv/old"},
		{appui.TransferRequest{Session: "web", TargetSession: "web", Kind: "move", Remote: "/a/f", Destination: "/b/f"}, "移动 f → prod-web-01:/b"},
		{appui.TransferRequest{Session: "web", TargetSession: "web", Kind: "copy", Remote: "/a/f", Destination: "/b/f"}, "复制 f → prod-web-01:/b"},
		{appui.TransferRequest{Session: "web", TargetSession: "db", Kind: "cross", Remote: "/a/f", Destination: "/b/f"}, "复制 f ← prod-web-01:/a → db-master:/b"},
		{appui.TransferRequest{Session: "quick", Kind: "upload", Local: "/x/f", Remote: "/srv/f"}, "上传 f → 10.9.9.9:/srv"}, // no name: the address
		{appui.TransferRequest{Session: "gone", Kind: "remove", Remote: "/srv/old"}, "删除 /srv/old"},
	} {
		if got := p.transferLabel(v.r); got != v.want {
			t.Errorf("%s %s: %q, want %q", v.r.Kind, v.r.Remote, got, v.want)
		}
	}
}

// A link the user chose to download gives what it points to; a link to nothing fails with a reason.
func TestDownloadThroughLinks(t *testing.T) {
	testutil.RequirePOSIXRemote(t)
	p := desktopFixture(t)
	session, root := phase2SSH(t, p)
	real := filepath.Join(root, "logs-real")
	os.MkdirAll(filepath.Join(real, "sub"), 0700)
	os.WriteFile(filepath.Join(real, "a.log"), []byte("alpha"), 0600)
	os.WriteFile(filepath.Join(real, "sub", "b.log"), []byte("beta"), 0600)
	os.Symlink(filepath.Join(real, "a.log"), filepath.Join(root, "link-file"))
	os.Symlink(real, filepath.Join(root, "link-dir"))
	os.Symlink(filepath.Join(root, "nowhere"), filepath.Join(root, "broken"))
	out := t.TempDir()
	id, err := p.QueueTransfer(appui.TransferRequest{Session: session, Kind: "download", Remote: filepath.Join(root, "link-file"), Local: filepath.Join(out, "a.log")})
	if err != nil {
		t.Fatal(err)
	}
	waitQueue(t, p, id)
	if b, err := os.ReadFile(filepath.Join(out, "a.log")); err != nil || string(b) != "alpha" {
		t.Fatal("link to a file:", err, string(b))
	}
	id, err = p.QueueTransfer(appui.TransferRequest{Session: session, Kind: "download", Remote: filepath.Join(root, "link-dir"), Local: filepath.Join(out, "logs")})
	if err != nil {
		t.Fatal(err)
	}
	waitQueue(t, p, id)
	if b, err := os.ReadFile(filepath.Join(out, "logs", "sub", "b.log")); err != nil || string(b) != "beta" {
		t.Fatal("link to a folder:", err, string(b))
	}
	id, err = p.QueueTransfer(appui.TransferRequest{Session: session, Kind: "download", Remote: filepath.Join(root, "broken"), Local: filepath.Join(out, "broken")})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.After(3 * time.Second)
	for failed := false; !failed; {
		for _, j := range p.Transfers() {
			failed = failed || j.ID == id && j.State == "failed" && j.Error != ""
		}
		select {
		case <-deadline:
			t.Fatal("a link to nothing did not fail")
		case <-time.After(time.Millisecond):
		}
	}
	if _, err = os.Lstat(filepath.Join(out, "broken")); err == nil {
		t.Fatal("a file came out of a broken link")
	}
	// The list shows the three links as what they are.
	p.ListFiles(session, root)
	listed := awaitEvent(t, p, func(e appui.Event) bool { return e.Files != nil }).Files
	for _, f := range listed.Entries {
		switch f.Name {
		case "link-file":
			if !f.Symlink || f.Directory || f.Broken || f.Size != 5 {
				t.Fatal(f)
			}
		case "link-dir":
			if !f.Symlink || !f.Directory || f.Broken {
				t.Fatal(f)
			}
		case "broken":
			if !f.Symlink || !f.Broken {
				t.Fatal(f)
			}
		}
	}
}
