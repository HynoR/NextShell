package appui_test

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/sftpfs"
	"nextshell.dev/v2/internal/transfer"
)

// shooter renders a screen light and dark into $NEXTSHELL_SHOTS; without it the test skips.
func shooter(t *testing.T) func(*ui.Tester, string) {
	t.Helper()
	dir := os.Getenv("NEXTSHELL_SHOTS")
	if dir == "" {
		t.Skip("set NEXTSHELL_SHOTS=<dir> to render screenshots")
	}
	if e := os.MkdirAll(dir, 0o755); e != nil {
		t.Fatal(e)
	}
	return func(tt *ui.Tester, name string) {
		t.Helper()
		defer tt.SetDark(false)
		for _, mode := range []string{"light", "dark"} {
			tt.SetDark(mode == "dark")
			tt.Frame()
			f, e := os.Create(filepath.Join(dir, name+"-"+mode+".png"))
			if e != nil {
				t.Fatal(e)
			}
			if e = png.Encode(f, tt.Image()); e != nil {
				t.Fatal(e)
			}
			f.Close()
		}
	}
}

// TestScreenshotsUX7 renders the screens of the file, snippet and activity fixes.
func TestScreenshotsUX7(t *testing.T) {
	shot := shooter(t)
	p := newPlatform(t)
	_, db := demo(t, p)
	_, tt := start(p)
	tt.SetTitleBar(ui.TitleBar{Height: 44, Left: 72})
	session(t, p, tt, "s1", "db-master", db)
	click(t, tt, "SFTP")
	p.events = append(p.events, appui.Event{FilesFailed: &appui.FileFailure{Session: "s1", Path: ".", Err: `sftp: "permission denied" (SSH_FX_PERMISSION_DENIED)`}})
	tt.Frame()
	shot(tt, "ux7-1-listing-error")

	// Deleting a folder: the count it waits for, the confirm that says what goes, and the progress.
	rp, rt := removalFixture(t)
	rt.SetTitleBar(ui.TitleBar{Height: 44, Left: 72})
	now := time.Now()
	listing(rp.fakePlatform, rt, "one", "/srv/app",
		sftpfs.Entry{Name: "node_modules", Path: "/srv/app/node_modules", Directory: true, Mode: "drwxr-xr-x", Modified: now.Add(-26 * time.Hour)},
		sftpfs.Entry{Name: "releases", Path: "/srv/app/releases", Directory: true, Mode: "drwxr-xr-x", Modified: now.Add(-50 * time.Hour)},
		sftpfs.Entry{Name: "config.yaml", Path: "/srv/app/config.yaml", Size: 2048, Mode: "-rw-r--r--", Modified: now.Add(-time.Hour)})
	deleteRow(t, rt, "打开 node_modules")
	shot(rt, "ux7-2a-delete-counting")
	rp.events = append(rp.events, appui.Event{Removal: &appui.RemovalCount{Session: "one", Targets: []appui.RemovalTally{{Path: "/srv/app/node_modules", Files: 18423, Dirs: 2160}}}})
	rt.Frame()
	shot(rt, "ux7-2b-delete-confirm")
	click(t, rt, "删除 1 项")
	rp.jobs = []transfer.Job{{ID: "job", Label: "删除 /srv/app/node_modules", State: "running", Items: true, Bytes: 7210, Total: 20583, Attempt: 1, Started: now}}
	rt.Frame()
	shot(rt, "ux7-2c-delete-progress")
	click(t, rt, "展开活动栏")
	shot(rt, "ux7-2d-delete-in-queue")
}
