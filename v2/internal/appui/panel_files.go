package appui

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/model"
	"nextshell.dev/v2/internal/sftpfs"
)

type filesState struct {
	session, path, pathInput string
	requested, followed      string
	paths                    map[string]string // last directory per session
	entries                  []sftpfs.Entry
	selection                ui.Selection[string]
	list                     ui.ListState
	row                      int
	clipboard                remoteClipboard
	follow                   bool
	dialog                   fileDialog
}

type remoteClipboard struct {
	Session string
	Paths   []string
	Cut     bool
}

// fileDialog asks what a file operation needs: a name, a local path or a target.
type fileDialog struct {
	open              bool
	kind              string // upload, download, cross, mkdir, create, rename
	entry             sftpfs.Entry
	text, target      string
	overwrite, packed bool
}

func (s *filesState) init() {
	s.row = -1
	s.paths = map[string]string{}
}

func (s *filesState) handle(a *App, e Event) {
	if e.Files == nil || e.Files.Session != a.activeID() {
		return
	}
	if s.session != e.Files.Session || s.path != e.Files.Path {
		s.selection.Clear()
		s.row = -1
	}
	s.session, s.path, s.pathInput = e.Files.Session, e.Files.Path, e.Files.Path
	s.paths[s.session] = s.path
	s.entries = e.Files.Entries
}
func (a *App) listFiles(id, dir string) {
	a.files.requested = id
	a.P.ListFiles(id, dir)
}

func (a *App) filesPanel(c *ui.Context) {
	t := c.Theme()
	s := &a.files
	sess := a.remote()
	if sess == nil {
		emptyState(c, "文件面板用于远程 SSH 会话", "切换到一个远程标签，或从左侧连接主机。")
		return
	}
	id := sess.ID
	if s.session != id && s.requested != id {
		a.listFiles(id, orDefault(s.paths[id], "."))
	}
	if s.follow {
		if cwd := a.P.SessionDir(id); cwd != "" && cwd != s.followed {
			s.followed = cwd
			a.listFiles(id, cwd)
		}
	}
	ui.Row(c).FillWidth().Padding(6, 8).Gap(2).AlignItems(ui.Center).Children(func() {
		if iconButton(c, "上级目录", "up").Clicked() && s.session == id {
			a.listFiles(id, path.Dir(s.path))
		}
		in := ui.TextInput(c.Key("files-path"), &s.pathInput).Label("远程目录").Grow(1).MinWidth(0).Height(26).FontSize(12)
		if in.Submitted() {
			a.listFiles(id, strings.TrimSpace(s.pathInput))
		}
		if iconButton(c, "刷新", "refresh").Clicked() {
			a.listFiles(id, orDefault(s.path, "."))
		}
		iconButton(c, "更多", "more").PopoverMenuButton(func(m *ui.Menu) { a.filesMenuItems(m, id) }, ui.PopoverMenuButtonOptions{At: ui.AnchorBottomEnd, Self: ui.AnchorTopEnd})
	})
	if s.session != id {
		ui.Row(c).Fill().Center().Children(func() { ui.Spinner(c) })
		return
	}
	zone := ui.Column(c).Grow(1).FillWidth().MinHeight(0)
	zone.ContextMenu(func(m *ui.Menu) { a.filesMenuItems(m, id) })
	for _, local := range zone.DroppedFiles() {
		a.queue(TransferRequest{Session: id, Kind: "upload", Local: local, Remote: path.Join(s.path, filepath.Base(local))})
	}
	if zone.FileDragOver() {
		zone.Border(2, t.Accent)
	}
	zone.Children(func() {
		if len(s.entries) == 0 {
			emptyState(c, "空目录", "把本地文件拖到这里即可上传。")
			return
		}
		s.list.Selected = &s.row
		s.list.Selection = &s.selection
		s.list.Key = func(i int) any { return s.entries[i].Path }
		s.list.Label = func(i int) string { return s.entries[i].Name }
		s.list.CellMenu = func(m *ui.Menu, row, col int) {
			if row >= 0 && row < len(s.entries) {
				a.fileMenuItems(m, id, s.entries[row])
			}
		}
		cols := []ui.TableColumn{{Title: "名称"}, {Title: "大小", Width: 64, Align: ui.End}, {Title: "修改时间", Width: 84}}
		table := ui.Table(c.Key("files-"+id), &s.list, cols, len(s.entries), func(row, col int) {
			f := s.entries[row]
			switch col {
			case 0:
				icon := "file"
				if f.Directory {
					icon = "folder"
				}
				label := "选择 " + f.Name
				if f.Directory {
					label = "打开 " + f.Name
				}
				ui.Row(c).Label(label).Gap(6).AlignItems(ui.Center).Tooltip(f.Mode).Children(func() {
					ui.Icon(c, appIcons[icon]).FontSize(15).TextColor(t.TextMuted)
					name := f.Name
					if f.Symlink {
						name += " →"
					}
					ui.Text(c, name).SingleLine()
				})
			case 1:
				size := "—"
				if !f.Directory {
					size = humanSize(f.Size)
				}
				ui.Text(c, size).FontSize(12).TextColor(t.TextMuted)
			case 2:
				ui.Text(c, shortTime(f.Modified)).FontSize(12).TextColor(t.TextMuted).SingleLine()
			}
		}).Grow(1).FillWidth()
		a.fileClipboardKeys(table, id)
		if table.Submitted() && s.row >= 0 && s.row < len(s.entries) {
			f := s.entries[s.row]
			if f.Directory {
				a.listFiles(id, f.Path)
			} else if !f.Symlink {
				a.openEditor(id, f.Path)
			}
		}
	})
	ui.Row(c).FillWidth().Height(26).Padding(0, 10).Gap(8).AlignItems(ui.Center).BorderColor(t.Border).BorderWidth(1, 0, 0, 0).Children(func() {
		info := fmt.Sprintf("%d 项", len(s.entries))
		if n := s.selection.Len(); n > 0 {
			info = fmt.Sprintf("已选 %d / %d 项", n, len(s.entries))
		}
		muted(c, info).Grow(1)
		if len(s.clipboard.Paths) > 0 && ghost(c, "粘贴到当前目录", "").Clicked() {
			a.pasteRemoteClipboard(id)
		}
	})
}

func emptyState(c *ui.Context, title, sub string) {
	ui.Column(c).Fill().Padding(24).Gap(6).Center().Children(func() {
		ui.Text(c, title).Bold()
		muted(c, sub)
	})
}
func humanSize(n int64) string {
	units := []string{"B", "K", "M", "G", "T"}
	v := float64(n)
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.1f%s", v, units[i])
}
func shortTime(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	now := time.Now()
	if at.Year() == now.Year() && at.YearDay() == now.YearDay() {
		return at.Format("15:04")
	}
	if at.Year() == now.Year() {
		return at.Format("01-02 15:04")
	}
	return at.Format("2006-01-02")
}

// filesMenuItems is the ⋯ menu and the menu of the directory's blank area.
func (a *App) filesMenuItems(m *ui.Menu, id string) {
	s := &a.files
	if m.Item("上传文件…").Chosen() {
		a.later(func() { a.openFileDialog("upload", sftpfs.Entry{}) })
	}
	if m.Item("新建文件夹…").Chosen() {
		a.later(func() { a.openFileDialog("mkdir", sftpfs.Entry{}) })
	}
	if m.Item("新建文件…").Chosen() {
		a.later(func() { a.openFileDialog("create", sftpfs.Entry{}) })
	}
	if m.Item("粘贴").Disabled(len(s.clipboard.Paths) == 0).Chosen() {
		a.later(func() { a.pasteRemoteClipboard(id) })
	}
	m.Separator()
	if m.Item("跟随终端目录").Checked(s.follow).Chosen() {
		s.follow = !s.follow
		if s.follow && a.P.SessionDir(id) == "" {
			a.toast("终端还没有报告当前目录，可在命令面板里为会话插入目录跟踪脚本")
		}
	}
	if m.Item("在终端中 cd 到此处").Chosen() {
		a.later(func() { a.cdInTerminal(id, s.path) })
	}
	if m.Item("前往默认上传目录").Chosen() {
		a.later(func() { a.listFiles(id, a.Prefs.UploadDir) })
	}
	if m.Item("刷新").Chosen() {
		a.later(func() { a.listFiles(id, s.path) })
	}
}
func (a *App) cdInTerminal(id, dir string) {
	if e := safeTerminalPath(dir); e != nil {
		a.fail(e)
		return
	}
	a.report(a.P.Send(id, "cd -- "+shellQuote(dir)+"\r"), "")
	a.tabs.term.Focus()
}
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
func safeTerminalPath(s string) error {
	for _, r := range s {
		if r < 32 || r == 127 {
			return fmt.Errorf("路径包含控制字符，不能发送到终端")
		}
	}
	return nil
}

func (a *App) fileMenuItems(m *ui.Menu, id string, f sftpfs.Entry) {
	if f.Directory {
		if m.Item("打开").Chosen() {
			a.later(func() { a.listFiles(id, f.Path) })
		}
	} else if !f.Symlink {
		if m.Item("编辑").Chosen() {
			a.later(func() { a.openEditor(id, f.Path) })
		}
		if m.Item("用外部编辑器打开").Chosen() {
			a.later(func() { a.externalEdit(id, f.Path) })
		}
	}
	if !f.Symlink {
		if m.Item("下载到默认目录").Chosen() {
			a.later(func() { a.download(id, f, filepath.Join(a.Prefs.DownloadDir, f.Name), false) })
		}
		if m.Item("下载到…").Chosen() {
			a.later(func() { a.openFileDialog("download", f) })
		}
	}
	m.Separator()
	if m.Item("复制").Chosen() {
		a.later(func() { a.setRemoteClipboard(id, &f, false) })
	}
	if m.Item("剪切").Chosen() {
		a.later(func() { a.setRemoteClipboard(id, &f, true) })
	}
	if m.Item("粘贴").Disabled(len(a.files.clipboard.Paths) == 0).Chosen() {
		a.later(func() { a.pasteRemoteClipboard(id) })
	}
	m.Separator()
	if m.Item("重命名…").Chosen() {
		a.later(func() { a.openFileDialog("rename", f) })
	}
	if m.Item("复制路径").Chosen() {
		a.P.CopyText(f.Path)
	}
	if f.Directory && m.Item("在终端中 cd 到此处").Chosen() {
		a.later(func() { a.cdInTerminal(id, f.Path) })
	}
	if !f.Symlink && m.Item("发送到其他服务器…").Chosen() {
		a.later(func() { a.openFileDialog("cross", f) })
	}
	m.Separator()
	if m.Item("删除").Danger().Chosen() {
		a.later(func() {
			targets := a.selectedFiles(&f)
			names := make([]string, len(targets))
			for i, v := range targets {
				names[i] = v.Name
			}
			a.confirm("删除远程文件", strings.Join(names, "、")+"\n删除后无法恢复；只能删除文件、链接或空目录。", fmt.Sprintf("删除 %d 项", len(targets)), func() {
				for _, v := range targets {
					a.P.FileAction(id, "remove", v.Path, "")
				}
			})
		})
	}
}

func (a *App) download(id string, f sftpfs.Entry, local string, overwrite bool) {
	if err := model.ValidateDownloadName(f.Name); err != nil {
		a.fail(err)
		return
	}
	kind := "download"
	if f.Directory {
		kind = "download-packed"
	}
	a.queue(TransferRequest{Session: id, Kind: kind, Remote: f.Path, Local: local, Overwrite: overwrite})
}
func (a *App) queue(req TransferRequest) {
	_, e := a.P.QueueTransfer(req)
	a.report(e, "已加入传输队列")
}

func (a *App) openFileDialog(kind string, f sftpfs.Entry) {
	d := fileDialog{open: true, kind: kind, entry: f}
	switch kind {
	case "download":
		d.text = filepath.Join(a.Prefs.DownloadDir, f.Name)
	case "rename":
		d.text = f.Name
	case "cross":
		d.target = f.Path
	}
	a.files.dialog = d
}
func (a *App) fileDialogView(c *ui.Context) {
	d := &a.files.dialog
	sess := a.remote()
	if !d.open || sess == nil {
		return
	}
	id := sess.ID
	titles := map[string]string{"upload": "上传文件", "download": "下载到…", "cross": "发送到其他服务器", "mkdir": "新建文件夹", "create": "新建文件", "rename": "重命名文件"}
	ui.Modal(c, &d.open, func() {
		ui.Column(c).Width(460).Gap(16).Children(func() {
			ui.Text(c, titles[d.kind]).FontSize(17).Bold()
			if d.entry.Name != "" {
				muted(c, d.entry.Path)
			}
			var ok string
			ui.Form(c, func() {
				switch d.kind {
				case "upload":
					ui.Field(c, "本地路径", func() {
						ui.TextInput(c, &d.text).Label("本地路径").Placeholder("/Users/me/file.txt").AutoFocus().Grow(1)
					}).Description("也可以直接把文件拖到文件列表上").FillWidth()
					toggle(c, "作为文件夹打包上传", &d.packed)
					toggle(c, "覆盖同名文件", &d.overwrite)
					ok = "上传到 " + a.files.path
				case "download":
					ui.Field(c, "保存到", func() { ui.TextInput(c, &d.text).Label("本地路径").AutoFocus().Grow(1) }).FillWidth()
					toggle(c, "覆盖同名文件", &d.overwrite)
					ok = "下载"
				case "cross":
					var ids, names []string
					for _, t := range a.Tabs {
						if !t.Local && t.ID != id {
							ids = append(ids, t.ID)
							names = append(names, t.Title)
						}
					}
					pick(c, "目标会话", "（选择会话）", &d.text, ids, names)
					field(c, "目标路径", &d.target)
					toggle(c, "覆盖同名文件", &d.overwrite)
					ok = "发送"
				default:
					ui.Field(c, "名称", func() { ui.TextInput(c, &d.text).Label("名称").AutoFocus().Grow(1) }).FillWidth()
					ok = map[string]string{"mkdir": "创建", "create": "创建", "rename": "重命名"}[d.kind]
				}
			}).FillWidth().Gap(FormGap)
			ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "取消").Clicked() {
					d.open = false
				}
				if ui.PrimaryButton(c, ok).Disabled(strings.TrimSpace(d.text) == "").Clicked() {
					a.runFileDialog(id)
				}
			})
		})
	})
}
func (a *App) runFileDialog(id string) {
	d := &a.files.dialog
	dir := a.files.path
	text := strings.TrimSpace(d.text)
	switch d.kind {
	case "upload":
		kind := "upload"
		if d.packed {
			kind = "upload-packed"
		}
		a.queue(TransferRequest{Session: id, Kind: kind, Local: text, Remote: path.Join(dir, filepath.Base(text)), Overwrite: d.overwrite && !d.packed})
	case "download":
		a.download(id, d.entry, text, d.overwrite)
	case "cross":
		a.queue(TransferRequest{Session: id, TargetSession: text, Kind: "cross", Remote: d.entry.Path, Destination: d.target, Overwrite: d.overwrite})
	case "mkdir", "create":
		if strings.Contains(text, "/") {
			a.fail(fmt.Errorf("名称不能包含 /"))
			return
		}
		a.P.FileAction(id, d.kind, path.Join(dir, text), "")
	case "rename":
		if strings.Contains(text, "/") {
			a.fail(fmt.Errorf("名称不能包含 /"))
			return
		}
		a.P.FileAction(id, "rename", d.entry.Path, path.Join(path.Dir(d.entry.Path), text))
	}
	d.open = false
}

// The remote clipboard copies and moves files between directories and servers.

func (a *App) selectedFiles(fallback *sftpfs.Entry) []sftpfs.Entry {
	s := &a.files
	if fallback != nil && !s.selection.Has(fallback.Path) {
		return []sftpfs.Entry{*fallback}
	}
	var out []sftpfs.Entry
	for _, f := range s.entries {
		if s.selection.Has(f.Path) {
			out = append(out, f)
		}
	}
	if len(out) == 0 && fallback != nil {
		return []sftpfs.Entry{*fallback}
	}
	return out
}
func (a *App) setRemoteClipboard(id string, fallback *sftpfs.Entry, cut bool) {
	var paths []string
	for _, f := range a.selectedFiles(fallback) {
		if f.Symlink {
			a.toast("暂不支持复制符号链接")
			return
		}
		if err := model.ValidateDownloadName(f.Name); err != nil {
			a.fail(err)
			return
		}
		paths = append(paths, f.Path)
	}
	if len(paths) == 0 {
		a.toast("请先选择文件")
		return
	}
	a.files.clipboard = remoteClipboard{id, paths, cut}
	verb := "复制"
	if cut {
		verb = "剪切"
	}
	a.toast(fmt.Sprintf("已%s %d 项，可在其他目录或服务器粘贴", verb, len(paths)))
}
func (a *App) pasteRemoteClipboard(id string) {
	clip := a.files.clipboard
	if len(clip.Paths) == 0 {
		a.toast("剪贴板为空")
		return
	}
	for _, src := range clip.Paths {
		kind := "copy"
		if clip.Cut {
			kind = "move"
		}
		a.queue(TransferRequest{Session: clip.Session, TargetSession: id, Kind: kind, Remote: src, Destination: path.Join(a.files.path, path.Base(src))})
	}
	// Keep the clipboard on failures; a completed cut then fails safely (source absent).
}
func (a *App) fileClipboardKeys(e ui.Element, id string) {
	if e.Shortcut(ui.Cmd, ui.KeyC) {
		a.setRemoteClipboard(id, nil, false)
	}
	if e.Shortcut(ui.Cmd, ui.KeyX) {
		a.setRemoteClipboard(id, nil, true)
	}
	if e.Shortcut(ui.Cmd, ui.KeyV) {
		a.pasteRemoteClipboard(id)
	}
}

// Transfers: a count in the status bar opening the queue in a popover.

func (a *App) transfersStatus(c *ui.Context) {
	t := c.Theme()
	jobs := a.P.Transfers()
	active := 0
	for _, j := range jobs {
		if j.State == "queued" || j.State == "running" {
			active++
		}
	}
	label := "⇅ 传输"
	if active > 0 {
		label = fmt.Sprintf("⇅ %d 个传输", active)
		c.After(time.Second)
	}
	b := ui.ButtonBase(c).Label("传输列表").Padding(0, 6).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).FontSize(12).TextColor(t.TextMuted)
		if active > 0 {
			ui.Spinner(c).Size(10, 10)
		}
	})
	if b.Clicked() {
		a.layout.transfersOpen = !a.layout.transfersOpen
	}
	ui.Popover(c, b, &a.layout.transfersOpen, func() { a.transferList(c) })
}
func (a *App) transferList(c *ui.Context) {
	t := c.Theme()
	jobs := a.P.Transfers()
	states := map[string]string{"queued": "等待", "running": "进行中", "done": "完成", "failed": "失败", "cancelled": "已取消"}
	ui.Column(c).Width(380).Gap(8).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, "传输").Bold().Grow(1)
			if len(jobs) > 0 && ghost(c, "全部取消", "").Clicked() {
				a.P.CancelAllTransfers()
			}
		})
		if len(jobs) == 0 {
			muted(c, "没有传输任务。把文件拖到文件面板即可上传。")
		}
		ui.Scroll(c).MaxHeight(320).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Gap(8).Children(func() {
				for _, j := range jobs {
					ui.Column(c.Key(j.ID)).FillWidth().Gap(4).Children(func() {
						ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
							ui.Text(c, j.Label).SingleLine().Grow(1).MinWidth(0)
							switch j.State {
							case "queued", "running":
								if ghost(c, "取消", "").Clicked() {
									a.P.CancelTransfer(j.ID)
								}
							case "failed", "cancelled":
								if ghost(c, "重试", "").Clicked() {
									a.report(a.P.RetryTransfer(j.ID), "")
								}
							}
						})
						if j.State == "running" {
							ui.Progress(c, -1).FillWidth()
						}
						detail := states[j.State] + " · " + humanSize(j.Bytes)
						if elapsed := j.Finished.Sub(j.Started); !j.Started.IsZero() && !j.Finished.IsZero() && elapsed > 0 {
							detail += fmt.Sprintf(" · %s/s", humanSize(int64(float64(j.Bytes)/elapsed.Seconds())))
						} else if !j.Started.IsZero() && j.Finished.IsZero() {
							if elapsed := time.Since(j.Started); elapsed > 0 {
								detail += fmt.Sprintf(" · %s/s", humanSize(int64(float64(j.Bytes)/elapsed.Seconds())))
							}
						}
						if j.Attempt > 1 {
							detail += fmt.Sprintf(" · 第 %d 次", j.Attempt)
						}
						muted(c, detail)
						if j.Error != "" {
							ui.Text(c, j.Error).FontSize(12).TextColor(t.Danger)
						}
					})
				}
			})
		})
		if ui.Button(c, "清除已完成").Clicked() {
			a.toast(fmt.Sprintf("已清除 %d 个任务", a.P.ClearCompletedTransfers()))
		}
	})
}
