package appui

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/model"
	"nextshell.dev/v2/internal/sftpfs"
	"path"
	"runtime"
)

type remoteClipboard struct {
	Session string
	Paths   []string
	Cut     bool
}

func (a *App) selectedFiles(fallback *sftpfs.Entry) []sftpfs.Entry {
	s := &a.Phase2
	if fallback != nil && !s.FileSelection.Has(fallback.Path) {
		return []sftpfs.Entry{*fallback}
	}
	var out []sftpfs.Entry
	for _, f := range s.Files {
		if s.FileSelection.Has(f.Path) {
			out = append(out, f)
		}
	}
	if len(out) == 0 && fallback != nil {
		return []sftpfs.Entry{*fallback}
	}
	return out
}
func (a *App) setRemoteClipboard(id string, fallback *sftpfs.Entry, cut bool) {
	paths := []string{}
	for _, f := range a.selectedFiles(fallback) {
		if f.Symlink {
			a.Message = "暂不复制符号链接"
			return
		}
		if err := model.ValidateDownloadName(f.Name); err != nil {
			a.status(err)
			return
		}
		paths = append(paths, f.Path)
	}
	if len(paths) == 0 {
		a.Message = "请先选择文件或目录"
		return
	}
	a.Phase2.Clipboard = remoteClipboard{id, paths, cut}
	a.Message = fmt.Sprintf("远端剪贴板：%d 项（跨服务器仅复制，粘贴不覆盖）", len(paths))
}
func (a *App) pasteRemoteClipboard(id string) {
	s := &a.Phase2
	clip := s.Clipboard
	if len(clip.Paths) == 0 {
		a.Message = "远端剪贴板为空"
		return
	}
	for _, src := range clip.Paths {
		kind := "copy"
		if clip.Cut {
			kind = "move"
		}
		a.queue(TransferRequest{Session: clip.Session, TargetSession: id, Kind: kind, Remote: src, Destination: path.Join(s.RemotePath, path.Base(src))})
	}
	// Keep the clipboard on failures; a completed cut subsequently fails safely
	// (source absent), never re-creates or deletes an unrelated destination.
}
func (a *App) fileClipboardToolbar(c *ui.Context, id string) {
	ui.Row(c).Gap(8).Children(func() {
		ui.Text(c, fmt.Sprintf("已选 %d 项", a.Phase2.FileSelection.Len())).TextColor(c.Theme().TextMuted)
		if ghost(c, "复制所选", "files").Clicked() {
			a.setRemoteClipboard(id, nil, false)
		}
		if ghost(c, "剪切所选", "edit").Clicked() {
			a.setRemoteClipboard(id, nil, true)
		}
		if ghost(c, "粘贴到当前目录", "folder").Clicked() {
			a.pasteRemoteClipboard(id)
		}
	})
}
func (a *App) fileClipboardKeys(e ui.Element, id string) {
	mod := ui.Ctrl
	if runtime.GOOS == "darwin" {
		mod = ui.Super
	}
	if e.Shortcut(mod, ui.KeyC) {
		a.setRemoteClipboard(id, nil, false)
	}
	if e.Shortcut(mod, ui.KeyX) {
		a.setRemoteClipboard(id, nil, true)
	}
	if e.Shortcut(mod, ui.KeyV) {
		a.pasteRemoteClipboard(id)
	}
}
