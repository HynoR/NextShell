package appui_test

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/appui"
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
}
