package appui

import (
	"cmp"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/model"
	"nextshell.dev/v2/internal/sftpfs"
	"nextshell.dev/v2/internal/transfer"
)

type filesState struct {
	session, path, pathInput string
	requested, followed      string
	editingPath              bool
	crumb                    int
	sort, sorted             ui.SortOrder
	all                      []sftpfs.Entry    // entries before hiding dotfiles
	hidden                   bool              // show dotfiles
	renaming, renameTo       string            // the path renamed in place
	paths                    map[string]string // last directory per session
	entries                  []sftpfs.Entry
	selection                ui.Selection[string]
	list                     ui.ListState
	row                      int
	clipboard                remoteClipboard
	follow                   bool
	dialog                   fileDialog
	trees                    map[string]*dirTree // per session
	treeHidden               bool
	treeWidth                float32
}

// dirTree is the SFTP directory tree of a session: what its listings showed, and which folders are open.
type dirTree struct {
	dirs   map[string][]string // a listed folder's subfolders, in order
	open   map[string]*bool
	asked  map[string]bool // listed, or being listed
	peeks  map[string]bool // listings asked for the tree alone, not to show in the list
	shown  string          // the folder the list shows, which the tree follows
	hidden bool            // dotfolders were kept, as the list shows them
}

type remoteClipboard struct {
	Session string
	Paths   []string
	Cut     bool
}

// fileDialog asks what a file operation needs: a name or a target server.
type fileDialog struct {
	open         bool
	kind         string // cross, mkdir, create
	entry        sftpfs.Entry
	text, target string
	overwrite    bool
}

func (s *filesState) init() {
	s.row = -1
	s.paths = map[string]string{}
	s.trees = map[string]*dirTree{}
	s.treeWidth = 200
}

func (s *filesState) tree(id string) *dirTree {
	t := s.trees[id]
	if t == nil {
		t = &dirTree{dirs: map[string][]string{}, open: map[string]*bool{}, asked: map[string]bool{}, peeks: map[string]bool{}}
		s.trees[id] = t
	}
	return t
}

// learn keeps the subfolders of a listing.
func (t *dirTree) learn(l *FileListing) {
	var dirs []string
	for _, f := range l.Entries {
		if f.Directory && !f.Symlink { // a link may point anywhere: the tree shows real folders alone
			dirs = append(dirs, f.Path)
		}
	}
	slices.SortFunc(dirs, func(x, y string) int { return naturalCompare(path.Base(x), path.Base(y)) })
	t.dirs[l.Path] = dirs
	t.asked[l.Path] = true
}

// follow opens the folders down to dir, as the list went there.
func (t *dirTree) follow(dir string) {
	t.shown = dir
	for p := dir; ; p = path.Dir(p) {
		*t.flag(p) = true
		if p == "/" || p == "." {
			return
		}
	}
}

// pathBelow is the folder inside dir on the way down to sub, or "" when sub is not inside dir.
func pathBelow(dir, sub string) string {
	prefix := strings.TrimSuffix(dir, "/") + "/"
	if !strings.HasPrefix(sub, prefix) || sub == dir {
		return ""
	}
	name, _, _ := strings.Cut(sub[len(prefix):], "/")
	return prefix + name
}

// flag is whether folder p is open, for its TreeItem.
func (t *dirTree) flag(p string) *bool {
	o := t.open[p]
	if o == nil {
		o = new(bool)
		t.open[p] = o
	}
	return o
}

func (s *filesState) handle(a *App, e Event) {
	if e.Files == nil {
		return
	}
	if tree := s.trees[e.Files.Session]; tree != nil && tree.peeks[e.Files.Path] {
		delete(tree.peeks, e.Files.Path)
		tree.learn(e.Files)
		return
	}
	if e.Files.Session != a.activeID() {
		return
	}
	tree := s.tree(e.Files.Session)
	tree.learn(e.Files)
	tree.follow(e.Files.Path)
	if s.session != e.Files.Session || s.path != e.Files.Path {
		s.selection.Clear()
		s.row = -1
	}
	s.session, s.path, s.pathInput = e.Files.Session, e.Files.Path, e.Files.Path
	s.paths[s.session] = s.path
	s.all = e.Files.Entries
	s.filterEntries()
}
func (s *filesState) filterEntries() {
	s.entries = s.entries[:0:0]
	for _, f := range s.all {
		if s.hidden || !strings.HasPrefix(f.Name, ".") {
			s.entries = append(s.entries, f)
		}
	}
	s.sortEntries()
}

// sortEntries keeps folders first, then orders by the chosen column with
// names in natural order (file2 before file10); the server's order is arbitrary.
func (s *filesState) sortEntries() {
	s.sorted = s.sort
	slices.SortStableFunc(s.entries, func(x, y sftpfs.Entry) int {
		if x.Directory != y.Directory {
			if x.Directory {
				return -1
			}
			return 1
		}
		r := 0
		switch s.sort.Column {
		case "size":
			r = cmp.Compare(x.Size, y.Size)
		case "modified":
			r = x.Modified.Compare(y.Modified)
		}
		if r == 0 {
			r = naturalCompare(x.Name, y.Name)
		}
		if s.sort.Descending {
			r = -r
		}
		return r
	})
}

// naturalCompare compares case-insensitively, digit runs by their value.
func naturalCompare(a, b string) int {
	for a != "" && b != "" {
		da, db := digits(a), digits(b)
		if da > 0 && db > 0 {
			na, nb := strings.TrimLeft(a[:da], "0"), strings.TrimLeft(b[:db], "0")
			if r := cmp.Or(cmp.Compare(len(na), len(nb)), strings.Compare(na, nb)); r != 0 {
				return r
			}
			a, b = a[da:], b[db:]
			continue
		}
		ra, sa := utf8.DecodeRuneInString(a)
		rb, sb := utf8.DecodeRuneInString(b)
		if r := cmp.Compare(unicode.ToLower(ra), unicode.ToLower(rb)); r != 0 {
			return r
		}
		a, b = a[sa:], b[sb:]
	}
	return cmp.Compare(len(a), len(b))
}
func digits(s string) int {
	n := 0
	for n < len(s) && s[n] >= '0' && s[n] <= '9' {
		n++
	}
	return n
}
func (a *App) listFiles(id, dir string) {
	a.files.requested = id
	delete(a.files.tree(id).peeks, dir)
	a.P.ListFiles(id, dir)
}

// dirTreeView is the SFTP directory tree: a click opens a folder in the list, and an opened folder
// lists its subfolders the first time.
func (a *App) dirTreeView(c *ui.Context, id string) {
	tree := a.files.tree(id)
	var node func(dir string)
	node = func(dir string) {
		name := path.Base(dir)
		subs, listed := tree.dirs[dir]
		if !listed { // until it is listed, it shows the way down to the folder shown
			if next := pathBelow(dir, tree.shown); next != "" {
				subs = []string{next}
			}
		}
		var children func()
		if !listed || len(subs) > 0 {
			children = func() {
				if !tree.asked[dir] {
					tree.asked[dir], tree.peeks[dir] = true, true
					a.P.ListFiles(id, dir)
				}
				for _, sub := range subs {
					if a.files.hidden || !strings.HasPrefix(path.Base(sub), ".") {
						node(sub)
					}
				}
			}
		}
		item := ui.TreeItem(c.Key("dir-"+dir), name, tree.flag(dir), children).Label("目录 " + dir).Selected(dir == tree.shown).Tooltip(dir)
		if item.Clicked() {
			a.later(func() { a.listFiles(id, dir) })
		}
	}
	ui.Scroll(c).Fill().Children(func() {
		ui.Tree(c, func() { node("/") }).FillWidth().Padding(4)
	})
}

func (a *App) filesPanel(c *ui.Context) {
	t := c.Theme()
	s := &a.files
	sess := a.remote()
	if sess == nil {
		emptyState(c, "SFTP 用于远程 SSH 会话", "切换到一个远程标签，或从「服务器」连接主机。")
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
		treeLabel := "隐藏目录树"
		if s.treeHidden {
			treeLabel = "显示目录树"
		}
		if iconButton(c, treeLabel, "panel-left").Clicked() {
			s.treeHidden = !s.treeHidden
			a.report(a.P.SaveUIState("sftp-tree", fmt.Sprint(s.treeHidden)), "")
		}
		if iconButton(c, "上级目录", "up").Clicked() && s.session == id {
			a.listFiles(id, path.Dir(s.path))
		}
		a.pathBar(c, id)
		if iconButton(c, "上传", "upload").Clicked() {
			a.pick(FilePick{Title: "上传文件", Multiple: true}, func(p []string) { a.upload(id, p) })
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
	if s.treeHidden {
		a.fileList(c, id)
	} else {
		ui.Split(c, &s.treeWidth, func() { a.dirTreeView(c, id) }, func() { a.fileList(c, id) }).SplitReset(200).Grow(1).FillWidth().MinHeight(0)
	}
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

// fileList is the SFTP list of the folder shown: a drop uploads to it.
func (a *App) fileList(c *ui.Context, id string) {
	t := c.Theme()
	s := &a.files
	zone := ui.Column(c).Grow(1).FillWidth().FillHeight().MinWidth(0).MinHeight(0)
	zone.ContextMenu(func(m *ui.Menu) { a.filesMenuItems(m, id) })
	if dropped := zone.DroppedFiles(); dropped != nil {
		a.upload(id, dropped)
	}
	if zone.FileDragOver() {
		zone.Border(2, t.Accent)
	}
	zone.Children(func() {
		if len(s.entries) == 0 {
			emptyState(c, "空目录", "把本地文件拖到这里即可上传。")
			return
		}
		if s.sort != s.sorted {
			s.sortEntries()
		}
		s.list.Sort = &s.sort
		s.list.Selected = &s.row
		s.list.Selection = &s.selection
		s.list.Key = func(i int) any { return s.entries[i].Path }
		s.list.Label = func(i int) string { return s.entries[i].Name }
		s.list.CellMenu = func(m *ui.Menu, row, col int) {
			if row >= 0 && row < len(s.entries) {
				a.fileMenuItems(m, id, s.entries[row])
			}
		}
		cols := []ui.TableColumn{{Title: "名称", ID: "name", Sortable: true}, {Title: "大小", ID: "size", Width: 64, Align: ui.End, Sortable: true}, {Title: "修改时间", ID: "modified", Width: 100, Sortable: true}}
		table := ui.Table(c.Key("files-"+id), &s.list, cols, len(s.entries), func(row, col int) {
			f := s.entries[row]
			faint := mutedOnRow(t, s.selection.Has(f.Path))
			switch col {
			case 0:
				if s.renaming == f.Path {
					a.renameCell(c, id, f)
					return
				}
				icon := "file"
				if f.Directory {
					icon = "folder"
				}
				label := "选择 " + f.Name
				if f.Directory {
					label = "打开 " + f.Name
				}
				ui.Row(c).Label(label).Gap(6).AlignItems(ui.Center).Tooltip(f.Mode).Children(func() {
					ui.Icon(c, appIcons[icon]).FontSize(15).TextColor(faint)
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
				ui.Text(c, size).FontSize(12).TextColor(faint)
			case 2:
				ui.Text(c, shortTime(f.Modified)).FontSize(12).TextColor(faint).SingleLine()
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
}

// pathBar shows the directory as breadcrumbs; a click jumps to a parent,
// and a double click (or the last segment) edits the path as text.
func (a *App) pathBar(c *ui.Context, id string) {
	s := &a.files
	if s.editingPath {
		in := ui.TextInput(c.Key("files-path"), &s.pathInput).Label("远程目录").Grow(1).MinWidth(0).Height(26).FontSize(12).AutoFocus().OnTextKey(func(k ui.TextKey) bool {
			if k.Key == ui.KeyEscape {
				s.editingPath = false
				return true
			}
			return false
		})
		if in.Submitted() {
			s.editingPath = false
			a.listFiles(id, strings.TrimSpace(s.pathInput))
		}
		if in.FocusLeft() {
			s.editingPath = false
		}
		return
	}
	parts := crumbs(s.path)
	s.crumb = len(parts) - 1
	box := ui.Row(c).Grow(1).MinWidth(0).Height(26).AlignItems(ui.Center).Label("远程目录 " + s.path)
	box.Children(func() {
		if ui.Breadcrumbs(c, parts, &s.crumb).Label("路径").Grow(1).MinWidth(0).Changed() {
			a.listFiles(id, crumbPath(parts, s.crumb))
		}
		if iconButton(c, "编辑路径", "edit").Clicked() {
			s.editingPath, s.pathInput = true, s.path
		}
	})
	if box.DoubleClicked() {
		s.editingPath, s.pathInput = true, s.path
	}
}
func crumbs(dir string) []string {
	if dir == "" {
		return []string{"/"}
	}
	out := []string{"/"}
	for _, p := range strings.Split(strings.Trim(dir, "/"), "/") {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
func crumbPath(parts []string, i int) string { return "/" + strings.Join(parts[1:i+1], "/") }

// pick asks the host for a native panel; without one (tests, browsers) it says so.
func (a *App) pick(o FilePick, done func([]string)) {
	if a.PickFiles == nil {
		a.toast("此环境不支持系统文件选择框")
		return
	}
	a.PickFiles(o, func(paths []string) {
		if len(paths) > 0 {
			done(paths)
		}
	})
}
func (a *App) upload(id string, locals []string) {
	for _, local := range locals {
		a.queue(TransferRequest{Session: id, Kind: "upload", Local: local, Remote: path.Join(a.files.path, filepath.Base(local))})
	}
}

// downloadTo asks where to put files: a save panel for one file, a folder otherwise.
func (a *App) downloadTo(id string, files []sftpfs.Entry) {
	if len(files) == 1 && !files[0].Directory {
		f := files[0]
		a.pick(FilePick{Title: "下载到", DefaultPath: filepath.Join(a.Prefs.DownloadDir, f.Name), Save: true}, func(p []string) { a.download(id, f, p[0], true) })
		return
	}
	a.pick(FilePick{Title: "下载到文件夹", DefaultPath: a.Prefs.DownloadDir, Directory: true}, func(p []string) {
		for _, f := range files {
			a.download(id, f, filepath.Join(p[0], f.Name), false)
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
		a.later(func() { a.pick(FilePick{Title: "上传文件", Multiple: true}, func(p []string) { a.upload(id, p) }) })
	}
	if m.Item("上传文件夹…").Chosen() {
		a.later(func() {
			a.pick(FilePick{Title: "上传文件夹", Directory: true}, func(p []string) { a.upload(id, p) })
		})
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
	if m.Item("显示隐藏文件").Checked(s.hidden).Chosen() {
		s.hidden = !s.hidden
		s.filterEntries()
	}
	if m.Item("跟随终端目录").Checked(s.follow).Chosen() {
		s.follow = !s.follow
		if s.follow && a.P.SessionDir(id) == "" {
			a.toast("终端还没有报告当前目录，可在命令面板里为会话插入目录跟踪脚本")
		}
	}
	if m.Item("在终端中 cd 到此处").Chosen() {
		a.later(func() { a.cdInTerminal(id, s.path) })
	}
	if m.Item("打开默认下载目录").Chosen() {
		a.report(a.P.OpenFolder(a.Prefs.DownloadDir), "")
	}
	if m.Item("前往默认上传目录").Chosen() {
		a.later(func() { a.listFiles(id, a.Prefs.UploadDir) })
	}
	if m.Item("刷新").Chosen() {
		a.later(func() { a.listFiles(id, s.path) })
	}
}

// renameCell edits a name in place: Return renames, Escape or leaving cancels.
func (a *App) renameCell(c *ui.Context, id string, f sftpfs.Entry) {
	s := &a.files
	in := ui.TextInput(c.Key("rename-"+f.Path), &s.renameTo).Label("重命名 " + f.Name).AutoFocus().Grow(1).Height(22).FontSize(12).OnTextKey(func(k ui.TextKey) bool {
		if k.Key == ui.KeyEscape {
			s.renaming = ""
			return true
		}
		return false
	})
	if in.Submitted() {
		s.renaming = ""
		to := strings.TrimSpace(s.renameTo)
		switch {
		case to == "" || to == f.Name:
		case strings.Contains(to, "/"):
			a.fail(fmt.Errorf("名称不能包含 /"))
		default:
			a.P.FileAction(id, "rename", f.Path, path.Join(path.Dir(f.Path), to))
		}
	} else if in.FocusLeft() {
		s.renaming = ""
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
		if m.Item("远程编辑").Chosen() {
			a.later(func() { a.externalEdit(id, f.Path) })
		}
	}
	if !f.Symlink {
		targets := a.selectedFiles(&f)
		label := ""
		if len(targets) > 1 {
			label = fmt.Sprintf("（%d 项）", len(targets))
		}
		if m.Item("下载到默认目录" + label).Chosen() {
			a.later(func() {
				for _, v := range targets {
					a.download(id, v, filepath.Join(a.Prefs.DownloadDir, v.Name), false)
				}
			})
		}
		if m.Item("下载到…" + label).Chosen() {
			a.later(func() { a.downloadTo(id, targets) })
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
	if m.Item("重命名").Chosen() {
		a.files.renaming, a.files.renameTo = f.Path, f.Name
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
	a.queue(TransferRequest{Session: id, Kind: "download", Remote: f.Path, Local: local, Overwrite: overwrite})
}
func (a *App) queue(req TransferRequest) {
	_, e := a.P.QueueTransfer(req)
	a.report(e, "已加入传输队列")
}

func (a *App) openFileDialog(kind string, f sftpfs.Entry) {
	d := fileDialog{open: true, kind: kind, entry: f}
	switch kind {
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
	titles := map[string]string{"cross": "发送到其他服务器", "mkdir": "新建文件夹", "create": "新建文件"}
	dialog(c, &d.open, func() {
		ui.Column(c).Width(460).Gap(16).Children(func() {
			ui.Text(c, titles[d.kind]).FontSize(17).Bold()
			if d.entry.Name != "" {
				muted(c, d.entry.Path)
			}
			var ok string
			ui.Form(c, func() {
				switch d.kind {
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
					ok = "创建"
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
	case "cross":
		a.queue(TransferRequest{Session: id, TargetSession: text, Kind: "cross", Remote: d.entry.Path, Destination: d.target, Overwrite: d.overwrite})
	case "mkdir", "create":
		if strings.Contains(text, "/") {
			a.fail(fmt.Errorf("名称不能包含 /"))
			return
		}
		a.P.FileAction(id, d.kind, path.Join(dir, text), "")
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

// Transfers: the queue in the activity column.

func activeTransfers(jobs []transfer.Job) int {
	n := 0
	for _, j := range jobs {
		if j.State == "queued" || j.State == "running" {
			n++
		}
	}
	return n
}

func (a *App) transferSection(c *ui.Context) {
	t := c.Theme()
	jobs := a.P.Transfers()
	states := map[string]string{"queued": "等待", "running": "进行中", "done": "完成", "failed": "失败", "cancelled": "已取消"}
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			sectionLabel(c, "传输队列").Grow(1)
			if activeTransfers(jobs) > 0 && ghost(c, "全部取消", "").Clicked() {
				a.P.CancelAllTransfers()
			}
			if len(jobs) > activeTransfers(jobs) && ghost(c, "清除已完成", "").Clicked() {
				a.toast(fmt.Sprintf("已清除 %d 个任务", a.P.ClearCompletedTransfers()))
			}
		})
		if len(jobs) == 0 {
			muted(c, "没有传输任务。把文件拖到 SFTP 即可上传。")
		}
		for _, j := range jobs {
			ui.Column(c.Key(j.ID)).FillWidth().Gap(4).Padding(8).Radius(t.Radius).Background(t.Background).Border(1, t.Border).Children(func() {
				ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, j.Label).FontSize(12).SingleLine().Grow(1).MinWidth(0).Tooltip(j.Label)
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
					ui.Progress(c, transferFraction(j)).FillWidth()
				}
				detail := states[j.State] + " · " + humanSize(j.Bytes)
				if j.Total > 0 {
					detail = fmt.Sprintf("%s · %d%% · %s / %s", states[j.State], int(transferFraction(j)*100), humanSize(j.Bytes), humanSize(j.Total))
				}
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
				muted(c, detail).FontSize(11)
				if j.Error != "" {
					ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
						title, _ := humanize(j.Error)
						ui.Text(c, title).FontSize(12).TextColor(t.Danger).SingleLine().Grow(1).MinWidth(0)
						if ghost(c, "详情", "").Clicked() {
							a.dialogs.detail, a.dialogs.detailIn = j.Error, mainWindow
						}
					})
				}
			})
		}
	})
}

// transferFraction is the share done, or -1 (indeterminate) when the size is unknown.
func transferFraction(j transfer.Job) float64 {
	if j.Total <= 0 {
		return -1
	}
	return min(1, float64(j.Bytes)/float64(j.Total))
}
