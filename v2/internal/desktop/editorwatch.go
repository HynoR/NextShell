package desktop

import (
	"context"
	"os/exec"
	"runtime"
	"strings"

	"github.com/egoist/mygo"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/remoteedit"
)

type editorWatch struct{ w *mygo.FileWatcher }

func (w editorWatch) Next(ctx context.Context) error { _, e := w.w.Next(ctx); return e }
func (w editorWatch) Close() error                   { return w.w.Close() }
func watchEditor(ctx context.Context, path string) (remoteedit.Watcher, error) {
	w, e := mygo.WatchFiles(ctx, path, mygo.FileWatchOptions{})
	if e != nil {
		return nil, e
	}
	return editorWatch{w}, nil
}

func newLiveEdits(p *Platform) *remoteedit.LiveEdits {
	return remoteedit.NewLiveEdits(watchEditor, p.withSFTP, p.Invalidate, func(local string) {
		p.emit(appui.Event{Message: "未上传的修改保留在 " + local})
	})
}

// ExternalEdit opens a remote file in an outside editor and keeps its saves in sync until the user stops
// it in the activity bar. No program means the system's text editor.
func (p *Platform) ExternalEdit(session, remote, program string, args []string) {
	p.launch(func() {
		l, e := p.edits.Start(p.ctx, session, remote)
		if e == nil {
			cmd := editorCommand(program, args, l.Local)
			if e = cmd.Start(); e == nil {
				go cmd.Wait() // whether the editor still runs does not matter
			}
		}
		if e != nil {
			p.emit(appui.Event{Message: e.Error()})
		}
	})
}

// editorCommand opens file in program, or in the system's text editor when program is empty.
func editorCommand(program string, args []string, file string) *exec.Cmd {
	switch {
	case program == "" && runtime.GOOS == "darwin":
		return exec.Command("/usr/bin/open", "-t", file)
	case program == "" && runtime.GOOS == "windows":
		return exec.Command("notepad.exe", file)
	case program == "":
		return exec.Command("xdg-open", file)
	case strings.HasSuffix(program, ".app"): // a macOS bundle chosen in the settings
		return exec.Command("/usr/bin/open", append(append([]string{"-a", program}, args...), file)...)
	}
	return exec.Command(program, append(append([]string{}, args...), file)...)
}

func (p *Platform) LiveEdits() []remoteedit.Live { return p.edits.List() }
func (p *Platform) StopLiveEdit(id string)       { p.edits.Stop(id) }
func (p *Platform) ResolveLiveEdit(id string, overwrite bool) {
	p.launch(func() {
		if overwrite {
			p.edits.Overwrite(p.ctx, id)
		} else {
			p.edits.Reload(p.ctx, id)
		}
	})
}
func (p *Platform) SaveLiveEditCopy(id, dest string) error { return p.edits.SaveCopy(id, dest) }
