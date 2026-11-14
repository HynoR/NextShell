package appui

import (
	"cmp"
	"errors"
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
	failed                   map[string]string   // per session: why its first listing could not be read
	counting                 *removalCount       // the delete confirm waits for this count
	removing                 map[string]string   // deletions started here: transfer job -> session
	landing                  map[string]fileSpot // transfers that put files on a server: job -> where they land
	treeHidden               bool
	treeWidth                float32
}

// removalCount is a delete waiting for its count: what the confirm says it takes.
type removalCount struct {
	session string
	targets []sftpfs.Entry
	cancel  func()
}

// dirTree is the SFTP directory tree of a session: what its listings showed, and which folders are open.
type dirTree struct {
	dirs   map[string][]string // a listed folder's subfolders, in order
	open   map[string]*bool
	asked  map[string]bool   // listed, or being listed
	peeks  map[string]bool   // listings asked for the tree alone, not to show in the list
	failed map[string]string // a folder whose listing failed, and why
	shown  string            // the folder the list shows, which the tree follows
	seen   string            // the folder shown that the tree last scrolled to: it scrolls once per change, then the user's
	hidden bool              // dotfolders were kept, as the list shows them
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
	s.failed = map[string]string{}
	s.removing = map[string]string{}
	s.landing = map[string]fileSpot{}
	s.treeWidth = 200
}

// fileSpot is a folder of a session's server.
type fileSpot struct{ session, dir string }

// landsIn is the folder a transfer writes into on a server, if it writes on one.
func landsIn(r TransferRequest) (fileSpot, bool) {
	switch r.Kind {
	case "upload", "upload-packed":
		return fileSpot{r.Session, path.Dir(r.Remote)}, r.Remote != ""
	case "copy", "move", "cross":
		target := r.TargetSession
		if target == "" {
			target = r.Session
		}
		return fileSpot{target, path.Dir(r.Destination)}, r.Destination != ""
	}
	return fileSpot{}, false
}

// refreshLanded lists the folder shown again when a transfer into it has ended, so an upload or a copy
// shows up without pressing refresh. It runs every frame.
func (a *App) refreshLanded() {
	s := &a.files
	if len(s.landing) == 0 {
		return
	}
	refresh := false
	for _, j := range a.P.Transfers() {
		spot, ok := s.landing[j.ID]
		if !ok || j.State == "queued" || j.State == "running" {
			continue
		}
		delete(s.landing, j.ID)
		refresh = refresh || s.shows(spot)
	}
	if refresh {
		a.listFiles(s.session, s.path)
	}
}

// shows is whether the list shows the folder spot.
func (s *filesState) shows(spot fileSpot) bool {
	return s.session != "" && s.session == spot.session && s.path == spot.dir
}

func (s *filesState) tree(id string) *dirTree {
	t := s.trees[id]
	if t == nil {
		t = &dirTree{dirs: map[string][]string{}, open: map[string]*bool{}, asked: map[string]bool{}, peeks: map[string]bool{}, failed: map[string]string{}}
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
	if f := e.FilesFailed; f != nil {
		s.listFailed(a, f)
		return
	}
	if r := e.Removal; r != nil {
		s.counted(a, r)
		return
	}
	if e.Files == nil {
		return
	}
	if tree := s.trees[e.Files.Session]; tree != nil && tree.peeks[e.Files.Path] {
		delete(tree.peeks, e.Files.Path)
		tree.learn(e.Files)
		if pathBelow(e.Files.Path, tree.shown) != "" { // a folder above the one shown arrived: its rows moved the folder
			tree.seen = ""
		}
		return
	}
	if e.Files.Session != a.activeID() {
		return
	}
	tree := s.tree(e.Files.Session)
	tree.learn(e.Files)
	tree.follow(e.Files.Path)
	delete(s.failed, e.Files.Session)
	if s.session != e.Files.Session || s.path != e.Files.Path {
		s.selection.Clear()
		s.row = -1
	}
	s.session, s.path, s.pathInput = e.Files.Session, e.Files.Path, e.Files.Path
	s.paths[s.session] = s.path
	s.all = e.Files.Entries
	s.filterEntries()
}

// counted opens the delete confirm with what the count found.
func (s *filesState) counted(a *App, r *RemovalCount) {
	n := s.counting
	if n == nil || n.session != r.Session || len(n.targets) != len(r.Targets) && r.Err == "" { // given up, or another delete's
		return
	}
	s.counting = nil
	if r.Err != "" {
		a.fail(errors.New(r.Err))
		return
	}
	files, dirs := 0, 0
	for _, v := range r.Targets {
		files, dirs = files+v.Files, dirs+v.Dirs
	}
	// The count may end after the tab was switched: the title names the server the files are on.
	a.confirmDanger("删除 "+a.sessionTitle(r.Session)+" 上的文件", fmt.Sprintf("%s\n将删除 %d 个文件、%d 个文件夹，删除后无法恢复。", fileNames(n.targets), files, dirs)+linksNote(n.targets), fmt.Sprintf("删除 %d 项", len(n.targets)), func() {
		for _, v := range r.Targets {
			id, e := a.P.QueueTransfer(TransferRequest{Session: r.Session, Kind: "remove", Remote: v.Path, Items: v.Files + v.Dirs})
			if e != nil {
				a.fail(e)
				return
			}
			s.removing[id] = r.Session
		}
	})
}

// linksNote is what a delete confirm adds when links are among the targets: they go, what they lead to stays.
func linksNote(files []sftpfs.Entry) string {
	if slices.ContainsFunc(files, func(f sftpfs.Entry) bool { return f.Symlink }) {
		return "\n链接只会删除链接本身，不会动它指向的内容。"
	}
	return ""
}

// fileNames lists up to five names for a confirm.
func fileNames(files []sftpfs.Entry) string {
	var names []string
	for _, f := range files[:min(len(files), 5)] {
		names = append(names, f.Name)
	}
	if len(files) > 5 {
		return strings.Join(names, "、") + fmt.Sprintf(" 等 %d 项", len(files))
	}
	return strings.Join(names, "、")
}

// askRemove confirms deleting targets. A folder takes all inside it, so the server is asked to count first:
// the confirm says how much goes. Plain files and links need no count.
func (a *App) askRemove(id string, targets []sftpfs.Entry) {
	s := &a.files
	if !slices.ContainsFunc(targets, func(f sftpfs.Entry) bool { return f.Directory && !f.Symlink }) {
		a.confirmDanger("删除 "+a.sessionTitle(id)+" 上的文件", fileNames(targets)+"\n删除后无法恢复。"+linksNote(targets), fmt.Sprintf("删除 %d 项", len(targets)), func() {
			for _, v := range targets {
				a.P.FileAction(id, "remove", v.Path, "")
			}
		})
		return
	}
	s.giveUpCount()
	paths := make([]string, len(targets))
	for i, v := range targets {
		paths[i] = v.Path
	}
	s.counting = &removalCount{session: id, targets: targets}
	s.counting.cancel = a.P.CountRemoval(id, paths)
}
func (s *filesState) giveUpCount() {
	if n := s.counting; n != nil {
		n.cancel()
		s.counting = nil
	}
}

// removalStatus is the footer's word on a delete: the count it waits for, or its progress, and the way
// to stop it. A finished delete refreshes the folder shown.
func (a *App) removalStatus(c *ui.Context, id string) {
	s := &a.files
	if n := s.counting; n != nil && n.session == id {
		muted(c, "正在统计要删除的内容…")
		if ghost(c, "取消", "").Label("取消统计").Clicked() {
			s.giveUpCount()
		}
		return
	}
	var done, total int64
	var running []string
	for _, j := range a.P.Transfers() {
		if s.removing[j.ID] != id {
			continue
		}
		if j.State == "queued" || j.State == "running" {
			done, total = done+j.Bytes, total+j.Total
			running = append(running, j.ID)
		} else {
			delete(s.removing, j.ID)
			if s.session == id {
				a.listFiles(id, s.path)
			}
		}
	}
	if len(running) == 0 {
		return
	}
	text := fmt.Sprintf("正在删除，已删除 %d 项", done)
	if total > 0 {
		text = fmt.Sprintf("正在删除 %d / %d 项", min(done, total), total)
	}
	muted(c, text)
	if ghost(c, "取消", "").Label("取消删除").Clicked() {
		for _, j := range running {
			a.P.CancelTransfer(j)
		}
	}
}

// listFailed answers a listing that failed: a folder of the tree shows it in place, a list not shown yet
// takes the pane (its spinner would never end), and a folder opened from a list shown is a toast.
func (s *filesState) listFailed(a *App, f *FileFailure) {
	reason, detail := listReason(f.Err)
	if tree := s.trees[f.Session]; tree != nil && tree.peeks[f.Path] {
		delete(tree.peeks, f.Path)
		tree.failed[f.Path] = reason
		return
	}
	if f.Session == s.session {
		a.failAs(reason, f.Err)
		return
	}
	s.failed[f.Session] = reason
	if detail != "" {
		s.failed[f.Session] += "\n" + detail
	}
}

// listReason says why a listing failed, in words for the user; detail is the server's own wording
// when it is not one of the known causes.
func listReason(msg string) (reason, detail string) {
	if strings.Contains(strings.ToLower(msg), "subsystem") {
		return "这台服务器没有开启文件传输（SFTP）", ""
	}
	if title, known := humanize(msg); known {
		return title, ""
	}
	return "服务器没有返回目录内容", msg
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
	delete(a.files.failed, id)
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
				if reason, bad := tree.failed[dir]; bad {
					if ui.TreeItem(c.Key("retry-"+dir), "无法读取，点击重试", nil, nil).Label("重试读取 " + dir).Tooltip(reason).TextColor(c.Theme().Danger).Clicked() {
						delete(tree.failed, dir)
						tree.peeks[dir] = true
						a.later(func() { a.P.ListFiles(id, dir) })
					}
				}
			}
		}
		item := ui.TreeItem(c.Key("dir-"+dir), name, tree.flag(dir), children).Label("目录 " + dir).Selected(dir == tree.shown).Tooltip(dir)
		if dir == tree.shown && tree.seen != dir {
			item.ScrollIntoView()
			tree.seen = dir
		}
		if item.Clicked() {
			a.later(func() { a.listFiles(id, dir) })
		}
	}
	ui.Scroll(c.Key("tree-" + id)).Fill().Children(func() {
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
	exited := a.P.Exited(id) // the list is what it was when the connection ended
	ui.Row(c).FillWidth().Padding(6, 8).Gap(2).AlignItems(ui.Center).Children(func() {
		treeLabel := "隐藏目录树"
		if s.treeHidden {
			treeLabel = "显示目录树"
		}
		if iconButton(c, treeLabel, "panel-left").Clicked() {
			s.treeHidden = !s.treeHidden
			a.report(a.P.SaveUIState("sftp-tree", fmt.Sprint(s.treeHidden)), "")
		}
		if iconButton(c, "上级目录", "up").Disabled(exited).Clicked() && s.session == id {
			a.listFiles(id, path.Dir(s.path))
		}
		a.pathBar(c, id)
		follow := iconButton(c, "跟随终端目录", "link")
		if s.follow {
			follow.Background(t.SurfacePressed).TextColor(accentInk(t))
		}
		if follow.Clicked() {
			s.follow, s.followed = !s.follow, ""
		}
		if iconButton(c, "上传", "upload").Disabled(exited).Clicked() {
			a.pick(FilePick{Title: "上传文件", Multiple: true}, func(p []string) { a.upload(id, p) })
		}
		if iconButton(c, "刷新", "refresh").Disabled(exited).Clicked() {
			a.listFiles(id, orDefault(s.path, "."))
		}
		iconButton(c, "更多", "more").Disabled(exited).PopoverMenuButton(func(m *ui.Menu) { a.filesMenuItems(m, id) }, ui.PopoverMenuButtonOptions{At: ui.AnchorBottomEnd, Self: ui.AnchorTopEnd})
	})
	if exited {
		ui.Row(c).FillWidth().Padding(5, 10).Gap(8).AlignItems(ui.Center).Label("连接已断开的提示").Children(func() {
			ui.Icon(c, appIcons["info"]).FontSize(14).TextColor(t.TextMuted)
			muted(c, "连接已断开，文件操作不可用").FontSize(12).Grow(1).MinWidth(0)
		})
	} else if s.follow && a.P.SessionDir(id) == "" {
		a.followHint(c)
	}
	if s.session != id {
		if why, bad := s.failed[id]; bad {
			a.listError(c, id, why)
			return
		}
		ui.Row(c).Fill().Center().Children(func() { ui.Spinner(c) })
		return
	}
	if s.treeHidden {
		s.tree(id).seen = "" // shown again, it scrolls to the folder again
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
		a.removalStatus(c, id)
		if len(s.clipboard.Paths) > 0 && ghost(c, "粘贴到当前目录", "").Clicked() {
			a.pasteRemoteClipboard(id)
		}
	})
}

// followHint says what to do when the list is to follow the terminal, which has not told its folder.
func (a *App) followHint(c *ui.Context) {
	t := c.Theme()
	text := "还没有收到终端的当前目录，暂时无法跟随。需要服务器使用 bash 或 zsh：按 " + appShortcutLabel("K") + "，选「让文件面板跟随终端目录」，检查后回车执行即可。"
	if a.Prefs.ShellIntegration == "off" {
		text = "目录跟踪在设置里是关闭的。到设置的「Shell 集成」打开后，文件面板才能跟随终端目录。"
	}
	ui.Row(c).FillWidth().Padding(5, 10).Gap(8).Background(t.Warning.Alpha(.12)).Label("跟随终端目录的提示").Children(func() {
		ui.Icon(c, appIcons["info"]).FontSize(14).TextColor(t.Warning)
		ui.Text(c, text).FontSize(12).Grow(1).MinWidth(0)
	})
}

// listError takes the pane when the first listing failed: why, and a way to ask again.
func (a *App) listError(c *ui.Context, id, why string) {
	reason, detail, _ := strings.Cut(why, "\n")
	retry := orDefault(a.files.paths[id], ".")
	ui.Column(c).Fill().Padding(24).Gap(6).Center().Children(func() {
		ui.Text(c, "无法读取目录").Bold()
		ui.Text(c, reason).FontSize(12).TextColor(c.Theme().Danger)
		if detail != "" {
			muted(c, detail).FontSize(11).Selectable()
		}
		ui.Row(c).Gap(8).Padding(8, 0, 0, 0).Children(func() {
			if ui.Button(c, "重试").Clicked() {
				a.listFiles(id, retry)
			}
			if retry != "." && ui.Button(c, "回到主目录").Clicked() {
				a.listFiles(id, ".")
			}
		})
	})
}

// fileList is the SFTP list of the folder shown: a drop uploads to it.
func (a *App) fileList(c *ui.Context, id string) {
	t := c.Theme()
	s := &a.files
	dead := a.P.Exited(id) // nothing can be done to a connection that ended: no menus, no drops
	zone := ui.Column(c).Grow(1).FillWidth().FillHeight().MinWidth(0).MinHeight(0)
	if !dead {
		zone.ContextMenu(func(m *ui.Menu) { a.filesMenuItems(m, id) })
		if dropped := zone.DroppedFiles(); dropped != nil {
			a.upload(id, dropped)
		}
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
		s.list.CellMenu = nil
		if !dead {
			s.list.CellMenu = func(m *ui.Menu, row, col int) {
				if row >= 0 && row < len(s.entries) {
					a.fileMenuItems(m, id, s.entries[row])
				}
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
				switch {
				case f.Broken:
					icon = "link"
				case f.Directory:
					icon = "folder"
				}
				label := "选择 " + f.Name
				if f.Directory {
					label = "打开 " + f.Name
				}
				ui.Row(c).Label(label).Gap(6).AlignItems(ui.Center).Tooltip(linkTooltip(f)).Children(func() {
					ui.Icon(c, appIcons[icon]).FontSize(15).TextColor(faint)
					ui.Text(c, f.Name).SingleLine()
					if f.Symlink { // where it leads, as the folder or file it is
						ui.Text(c, linkTail(f)).FontSize(12).TextColor(faint).SingleLine()
					}
				})
			case 1:
				size := "—"
				if !f.Directory && !f.Broken {
					size = humanSize(f.Size)
				}
				ui.Text(c, size).FontSize(12).TextColor(faint)
			case 2:
				ui.Text(c, shortTime(f.Modified)).FontSize(12).TextColor(faint).SingleLine()
			}
		}).Grow(1).FillWidth()
		if !dead {
			a.fileClipboardKeys(table, id)
			a.fileKeys(table, id)
		}
		if table.Submitted() && !dead && s.row >= 0 && s.row < len(s.entries) {
			f := s.entries[s.row]
			switch {
			case f.Broken:
				a.toast("「" + f.Name + "」是失效的链接：它指向的位置不存在或无法访问")
			case f.Directory:
				a.listFiles(id, f.Path)
			default:
				a.openEditor(id, f.Path)
			}
		}
	})
}

// linkTail is what follows a link's name in its row: where it points, and 失效 when nothing is there.
func linkTail(f sftpfs.Entry) string {
	tail := "→"
	if f.Target != "" {
		tail += " " + f.Target
	}
	if f.Broken {
		tail += "（失效）"
	}
	return tail
}

// linkTooltip is the tooltip of a row: its permissions, and for a link where it leads.
func linkTooltip(f sftpfs.Entry) string {
	if !f.Symlink {
		return f.Mode
	}
	tip := f.Mode + "\n链接指向 " + orDefault(f.Target, "未知位置")
	if f.Broken {
		tip += "，但那里不存在或无法访问"
	}
	return tip
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
	var reqs []TransferRequest
	for _, local := range locals {
		reqs = append(reqs, TransferRequest{Session: id, Kind: "upload", Local: local, Remote: path.Join(a.files.path, filepath.Base(local))})
	}
	a.queue(reqs...)
}

// downloadTo asks where to put files: a save panel for one file, a folder otherwise.
func (a *App) downloadTo(id string, files []sftpfs.Entry) {
	if len(files) == 1 && !files[0].Directory {
		f := files[0]
		a.pick(FilePick{Title: "下载到", DefaultPath: filepath.Join(a.Prefs.DownloadDir, f.Name), Save: true}, func(p []string) {
			a.download(id, files, func(sftpfs.Entry) string { return p[0] }, true)
		})
		return
	}
	a.pick(FilePick{Title: "下载到文件夹", DefaultPath: a.Prefs.DownloadDir, Directory: true}, func(p []string) {
		a.download(id, files, func(f sftpfs.Entry) string { return filepath.Join(p[0], f.Name) }, false)
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
	switch {
	case f.Broken: // nothing to open or download: it can still be renamed and deleted
	case f.Directory:
		if m.Item("打开").Chosen() {
			a.later(func() { a.listFiles(id, f.Path) })
		}
	default:
		if m.Item("编辑").Chosen() {
			a.later(func() { a.openEditor(id, f.Path) })
		}
		if m.Item("远程编辑").Chosen() {
			a.later(func() { a.externalEdit(id, f.Path) })
		}
	}
	if !f.Broken {
		targets := a.selectedFiles(&f)
		label := ""
		if len(targets) > 1 {
			label = fmt.Sprintf("（%d 项）", len(targets))
		}
		if m.Item("下载到默认目录" + label).Chosen() {
			a.later(func() {
				a.download(id, targets, func(f sftpfs.Entry) string { return filepath.Join(a.Prefs.DownloadDir, f.Name) }, false)
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
		a.later(func() { a.askRemove(id, a.selectedFiles(&f)) })
	}
}

// download queues files to the local path to gives each; a name that is not safe to write, and a link that
// leads nowhere, are skipped with a word.
func (a *App) download(id string, files []sftpfs.Entry, to func(sftpfs.Entry) string, overwrite bool) {
	var reqs []TransferRequest
	for _, f := range files {
		if err := model.ValidateDownloadName(f.Name); err != nil {
			a.fail(err)
			continue
		}
		if f.Broken {
			a.toast("「" + f.Name + "」是失效的链接，没有可下载的内容")
			continue
		}
		reqs = append(reqs, TransferRequest{Session: id, Kind: "download", Remote: f.Path, Local: to(f), Overwrite: overwrite})
	}
	a.queue(reqs...)
}

// transferQueuedID is the one slot of the 已加入 toast: another batch replaces it.
const transferQueuedID = "transfer-queued"

// queue queues the transfers and says so once: a hundred files are one toast, not a hundred. A failure is
// shown once however many requests met it.
func (a *App) queue(reqs ...TransferRequest) {
	n, failed := 0, map[string]bool{}
	for _, req := range reqs {
		if id, e := a.P.QueueTransfer(req); e == nil {
			n++
			if spot, ok := landsIn(req); ok {
				a.files.landing[id] = spot
			}
		} else if !failed[e.Error()] {
			failed[e.Error()] = true
			a.fail(e)
		}
	}
	if n > 0 {
		msg := fmt.Sprintf("已加入 %d 个传输", n)
		a.log(msg)
		a.addToast(ui.Toast{ID: transferQueuedID, Title: msg, Timeout: 3 * time.Second})
	}
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
			submit := false
			ui.Form(c, func() {
				switch d.kind {
				case "cross":
					var ids, names []string
					for _, t := range a.Tabs {
						if !t.Local && t.ID != id {
							ids = append(ids, t.ID)
							names = append(names, a.tabTitle(t))
						}
					}
					pick(c, "目标会话", "（选择会话）", &d.text, ids, names)
					field(c, "目标路径", &d.target)
					toggle(c, "覆盖同名文件", &d.overwrite)
					ok = "发送"
				default:
					ui.Field(c, "名称", func() { submit = ui.TextInput(c, &d.text).Label("名称").AutoFocus().Grow(1).Submitted() }).FillWidth()
					ok = "创建"
				}
			}).FillWidth().Gap(FormGap)
			ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "取消").Clicked() {
					d.open = false
				}
				// Enter in the name field creates, as the button does.
				if ui.PrimaryButton(c, ok).Disabled(strings.TrimSpace(d.text) == "").Clicked() || submit && strings.TrimSpace(d.text) != "" {
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
	kind := "copy"
	if clip.Cut {
		kind = "move"
	}
	var reqs []TransferRequest
	for _, src := range clip.Paths {
		reqs = append(reqs, TransferRequest{Session: clip.Session, TargetSession: id, Kind: kind, Remote: src, Destination: path.Join(a.files.path, path.Base(src))})
	}
	a.queue(reqs...)
	// Keep the clipboard on failures; a completed cut then fails safely (source absent).
}

// fileKeys are the keys of the list: Delete (⌘⌫ on a Mac) asks to delete what is selected, F2 renames the
// row, Backspace goes up to the folder above. A name being typed keeps its keys.
func (a *App) fileKeys(e ui.Element, id string) {
	s := &a.files
	if s.renaming != "" {
		return
	}
	row := func() []sftpfs.Entry { // what is selected, or else the row the keyboard is on
		if got := a.selectedFiles(nil); len(got) > 0 {
			return got
		}
		if s.row >= 0 && s.row < len(s.entries) {
			return s.entries[s.row : s.row+1]
		}
		return nil
	}
	switch {
	case e.Shortcut(0, ui.KeyDelete), e.Shortcut(ui.Cmd, ui.KeyBackspace):
		if targets := slices.Clone(row()); len(targets) > 0 {
			a.later(func() { a.askRemove(id, targets) })
		}
	case e.Shortcut(0, ui.KeyF2):
		if s.row >= 0 && s.row < len(s.entries) {
			f := s.entries[s.row]
			s.renaming, s.renameTo = f.Path, f.Name
		} else if got := row(); len(got) == 1 {
			s.renaming, s.renameTo = got[0].Path, got[0].Name
		}
	case e.Shortcut(0, ui.KeyBackspace):
		if s.path != "/" {
			a.listFiles(id, path.Dir(s.path))
		}
	}
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

func transferCount(jobs []transfer.Job, states ...string) int {
	n := 0
	for _, j := range jobs {
		if slices.Contains(states, j.State) {
			n++
		}
	}
	return n
}
func activeTransfers(jobs []transfer.Job) int { return transferCount(jobs, "queued", "running") }
func failedTransfers(jobs []transfer.Job) int { return transferCount(jobs, "failed") }

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
			if transferCount(jobs, "done") > 0 && ghost(c, "清除已完成", "").Clicked() {
				a.toast(fmt.Sprintf("已清除 %d 个任务", a.P.ClearCompletedTransfers()))
			}
		})
		if failed, cancelled := failedTransfers(jobs), transferCount(jobs, "cancelled"); failed+cancelled > 0 {
			// What went wrong comes first, with its reason; it stays until retried or cleared.
			jobs = slices.SortedStableFunc(slices.Values(jobs), func(x, y transfer.Job) int {
				return cmp.Compare(btoi(y.State == "failed"), btoi(x.State == "failed"))
			})
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				var parts []string
				if failed > 0 {
					parts = append(parts, fmt.Sprintf("%d 个传输失败", failed))
				}
				if cancelled > 0 {
					parts = append(parts, fmt.Sprintf("%d 个已取消", cancelled))
				}
				color := t.TextMuted
				if failed > 0 {
					color = t.Danger
				}
				ui.Text(c, strings.Join(parts, "，")).FontSize(12).Bold().TextColor(color).Grow(1).MinWidth(0)
				if ghost(c, "清除失败项", "").Clicked() {
					a.toast(fmt.Sprintf("已清除 %d 个任务", a.P.ClearFailedTransfers()))
				}
			})
		}
		if len(jobs) == 0 {
			muted(c, "没有传输任务。把文件拖到 SFTP 即可上传。")
		}
		for _, j := range jobs {
			border := t.Border
			if j.State == "failed" {
				border = t.Danger.Alpha(.5)
			}
			ui.Column(c.Key(j.ID)).FillWidth().Gap(4).Padding(8).Radius(t.Radius).Background(t.Background).Border(1, border).Children(func() {
				ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, j.Label).FontSize(12).Grow(1).MinWidth(0).Tooltip(j.Label) // wraps: the server is the end of it
					switch j.State {
					case "queued", "running":
						if ghost(c, "取消", "").Clicked() {
							a.P.CancelTransfer(j.ID)
						}
					case "failed", "cancelled":
						if ghost(c, "重试", "").Clicked() {
							a.report(a.P.RetryTransfer(j.ID), "")
						}
						if iconButton(c, "移除 "+j.Label, "close").Clicked() { // one row, not all the failed ones
							id := j.ID
							a.later(func() { a.P.DismissTransfer(id) })
						}
					}
				})
				if j.State == "running" {
					ui.Progress(c, transferFraction(j)).FillWidth()
				}
				detail := transferDetail(j, states[j.State])
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

// transferDetail is the grey line of a job: its state, how far it is, the speed and the attempt.
func transferDetail(j transfer.Job, state string) string {
	detail := state
	if j.Bytes > 0 { // nothing moved yet: 「失败 · 0 B」 says nothing
		detail += " · " + humanSize(j.Bytes)
	}
	switch {
	case j.Items: // a deletion counts items, and has no speed
		detail = state
		if j.Total > 0 && j.State == "running" {
			detail += fmt.Sprintf(" · 已删除 %d / %d 项 · %d%%", j.Bytes, j.Total, int(transferFraction(j)*100))
		} else if j.Bytes > 0 {
			detail += fmt.Sprintf(" · 已删除 %d 项", j.Bytes)
		}
	case j.Total > 0:
		detail = fmt.Sprintf("%s · %d%% · %s / %s", state, int(transferFraction(j)*100), humanSize(j.Bytes), humanSize(j.Total))
	}
	if elapsed := transferElapsed(j); !j.Items && elapsed > 0 {
		detail += fmt.Sprintf(" · %s/s", humanSize(int64(float64(j.Bytes)/elapsed.Seconds())))
	}
	if j.Attempt > 1 {
		detail += fmt.Sprintf(" · 第 %d 次", j.Attempt)
	}
	return detail
}

// transferElapsed is how long j ran, or has been running; zero before it starts.
func transferElapsed(j transfer.Job) time.Duration {
	switch {
	case j.Started.IsZero():
		return 0
	case j.Finished.IsZero():
		return time.Since(j.Started)
	}
	return j.Finished.Sub(j.Started)
}

// transferFraction is the share done, or -1 (indeterminate) when the size is unknown.
func transferFraction(j transfer.Job) float64 {
	if j.Total <= 0 {
		return -1
	}
	return min(1, float64(j.Bytes)/float64(j.Total))
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}
