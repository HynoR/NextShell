package appui

import (
	"bytes"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"nextshell.dev/v2/internal/model"
	"nextshell.dev/v2/internal/remoteedit"
)

// editorState holds the open remote documents. A document is a tab of the main window; popped out,
// it shows in the editor window instead. Each owns an official editor State (undo, selections, folds, scroll).
type editorState struct {
	documents  []*remoteDocument
	shown      string          // the document the main window's tab shows; "" while a terminal or a tool does
	win        string          // the document the editor window shows
	closeKey   string          // asking before this one closes
	closeSave  map[string]bool // closes when its save is acknowledged
	reloadKey  string          // asking before this one is reloaded
	popOut     bool            // the editor window is to open, or come forward
	windowed   bool            // the editor window is showing
	closeAll   bool            // closing the editor window asks about unsaved documents
	fromWindow bool            // the last save or reload was asked for in the window: its result shows there
}

// remoteDocument follows its host, as a tool tab does: Host is the host's key, and it saves through
// whichever session of the host is connected (see docSession).
type remoteDocument struct {
	Key, Host, Session, Text, Source string
	Conn                             model.Connection // the host as opened, for its colour
	Document                         remoteedit.Document
	Code                             *editor.State
	Version                          uint64
	Popped                           bool
}

func documentKey(host, path string) string { return host + "\x00" + path }

func (s *editorState) document(key string) *remoteDocument {
	for _, d := range s.documents {
		if d.Key == key {
			return d
		}
	}
	return nil
}

// sync reads the buffer back; WriteTo (not String) keeps the buffer's CRLF.
func (d *remoteDocument) sync() {
	if d.Code == nil || d.Code.Buffer == nil || d.Code.Buffer.Version() == d.Version {
		return
	}
	var out bytes.Buffer
	d.Code.Buffer.WriteTo(&out)
	d.Text = out.String()
	d.Source = d.Text
	d.Version = d.Code.Buffer.Version()
}

// prepare makes the buffer match Text: a new one when Text changed behind it.
func (d *remoteDocument) prepare() {
	if d.Code == nil || d.Code.Buffer == nil || d.Source != d.Text {
		d.Code = &editor.State{Buffer: editor.NewBuffer(d.Text), Label: "远程文本"}
		d.Code.Buffer.SetLanguage(editor.LanguageFor(d.Document.Path))
		d.Source = d.Text
		d.Version = d.Code.Buffer.Version()
	}
}
func (d *remoteDocument) dirty() bool {
	d.sync()
	return d.Text != d.Document.Text
}

// openEditor shows the document if it is open, else asks for it; receiveDocument shows it then.
func (a *App) openEditor(session, remote string) {
	if d := a.editor.document(documentKey(a.hostKeyOf(session), remote)); d != nil {
		a.showDocument(d)
		return
	}
	a.P.OpenRemote(session, remote)
}

// externalEdit opens remote in the chosen editor, or the system's text editor, and lists it under 实时编辑.
func (a *App) externalEdit(session, remote string) {
	args, err := splitArgs(a.Prefs.EditorArgs)
	if err != nil {
		a.fail(err)
		return
	}
	a.P.ExternalEdit(session, remote, a.Prefs.EditorCommand, args)
	a.layout.rightOpen = true // its state shows in the activity bar
}

// splitArgs splits the arguments typed for the external editor at spaces. Single or double quotes
// keep spaces inside one argument; a backslash is no escape, so Windows paths stay as typed.
func splitArgs(s string) ([]string, error) {
	var args []string
	var cur strings.Builder
	var quote rune
	started := false
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote, started = r, true
		case unicode.IsSpace(r):
			if started {
				args, started = append(args, cur.String()), false
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
			started = true
		}
	}
	if quote != 0 {
		return nil, errors.New("外部编辑器参数里有没有闭合的引号")
	}
	if started {
		args = append(args, cur.String())
	}
	return args, nil
}

// saveLocalCopy writes d's text, with the edits not saved yet, to a file the user picks: the way out of a
// conflict that keeps the edits. It is no overwrite: the server's file is not touched.
func (a *App) saveLocalCopy(d *remoteDocument) {
	d.sync()
	text, popped := d.Text, d.Popped
	a.pick(FilePick{Title: "另存本地副本", DefaultPath: filepath.Join(a.Prefs.DownloadDir, path.Base(d.Document.Path)), Save: true}, func(p []string) {
		a.inEditor(popped, func() { a.report(a.P.SaveLocalCopy(p[0], text), "已另存到 "+p[0]) })
	})
}

// hostSession is the host's connected session, else any session of it.
func (a *App) hostSession(host string) *Session {
	var found *Session
	for _, s := range a.Tabs {
		if !s.Local && hostKey(s) == host {
			if !a.P.Exited(s.ID) {
				return s
			}
			if found == nil {
				found = s
			}
		}
	}
	return found
}

// docSession is the session d saves and reloads through: its host's, which a reconnect replaced.
func (a *App) docSession(d *remoteDocument) string {
	if s := a.hostSession(d.Host); s != nil {
		d.Session = s.ID
	}
	return d.Session
}

// activeEditor is the document the main window's tab shows, or nil while a terminal or a tool shows.
// Like a tool tab it shows with its host's session active; a document whose host has none is shown anyway.
func (a *App) activeEditor() *remoteDocument {
	s := &a.editor
	if d := s.document(s.shown); d != nil && !d.Popped {
		if cur := a.current(); cur == nil || hostKey(cur) == d.Host || a.hostSession(d.Host) == nil {
			return d
		}
	}
	s.shown = ""
	return nil
}

// documentsIn are the documents of the main window's strip, or of the editor window when popped.
func (a *App) documentsIn(popped bool) []*remoteDocument {
	var out []*remoteDocument
	for _, d := range a.editor.documents {
		if d.Popped == popped {
			out = append(out, d)
		}
	}
	return out
}

// showDocument brings d forward: its tab, with its host's session, or the editor window.
func (a *App) showDocument(d *remoteDocument) {
	s := &a.editor
	if d.Popped {
		s.win, s.popOut = d.Key, true
		return
	}
	if cur := a.current(); cur == nil || hostKey(cur) != d.Host {
		if sess := a.hostSession(d.Host); sess != nil {
			a.Active = a.tabIndex(sess.ID)
		}
	}
	a.monitor.tool = ""
	s.shown = d.Key
}

// focusEditor is the palette's 远程编辑器: the first document's tab, or the window when all are popped out.
func (a *App) focusEditor() {
	for _, popped := range []bool{false, true} {
		if docs := a.documentsIn(popped); len(docs) > 0 {
			a.showDocument(docs[0])
			return
		}
	}
}

// popOutDocument moves d to the editor window, keeping its state; the main window goes back to its terminal.
func (a *App) popOutDocument(d *remoteDocument) {
	d.Popped = true
	if a.editor.shown == d.Key {
		a.editor.shown = ""
	}
	a.showDocument(d)
}

// popBackDocument moves d from the editor window back to a tab.
func (a *App) popBackDocument(d *remoteDocument) {
	d.Popped = false
	if a.editor.win == d.Key {
		a.editor.win = ""
	}
	a.showDocument(d)
	a.raiseMain()
}

// inEditor runs fn as if the editor window were building, when popped and showing: what it toasts
// belongs to that window.
func (a *App) inEditor(popped bool, fn func()) {
	if popped && a.editor.windowed {
		a.inWindow(editorWindow, fn)
		return
	}
	fn()
}

func (a *App) receiveDocument(r *EditorResult) {
	s := &a.editor
	host := a.hostKeyOf(r.Session)
	key := documentKey(host, r.Document.Path)
	d := s.document(key)
	fresh := d == nil
	if fresh {
		if len(s.documents) >= 32 {
			a.toast("最多同时打开 32 个远程文件，请先关闭一些")
			return
		}
		d = &remoteDocument{Key: key, Host: host, Session: r.Session, Document: r.Document, Text: r.Document.Text}
		if i := a.tabIndex(r.Session); i >= 0 {
			d.Conn = a.Tabs[i].Connection
		}
		d.prepare()
		s.documents = append(s.documents, d)
	} else {
		d.Session = r.Session
		dirty := d.dirty()
		switch {
		case r.Saved:
			// The list shows the file's size and time: list its folder again when it is the one shown.
			if a.files.shows(fileSpot{r.Session, path.Dir(r.Document.Path)}) {
				a.listFiles(r.Session, a.files.path)
			}
			d.Document = r.Document
			if d.Text == r.Document.Text && d.Code != nil {
				d.Code.Buffer.MarkSaved()
			}
		case dirty:
			a.inEditor(d.Popped, func() { a.toast("远程文件有更新，已保留未保存的修改；保存时会检查冲突") })
		default:
			d.Document = r.Document
			d.Text = r.Document.Text
		}
	}
	if r.Saved && s.closeSave[key] {
		delete(s.closeSave, key)
		if !d.dirty() { // typing since the save asked keeps it open
			s.removeDocument(key)
			return
		}
	}
	// Save acknowledgements never steal focus from another document.
	if fresh || !r.Saved {
		a.showDocument(d)
	}
}

func (a *App) requestDocumentClose(key string) {
	s := &a.editor
	if d := s.document(key); d != nil {
		if d.dirty() {
			s.closeKey = key
		} else {
			s.removeDocument(key)
		}
	}
}
func (s *editorState) removeDocument(key string) {
	s.documents = slices.DeleteFunc(s.documents, func(d *remoteDocument) bool { return d.Key == key })
	delete(s.closeSave, key)
	for _, k := range []*string{&s.shown, &s.win, &s.closeKey, &s.reloadKey} {
		if *k == key {
			*k = ""
		}
	}
}

// dropDocuments closes the documents whose host has no session left, as the tool tabs go; unsaved ones
// stay, so an agent closing a session never costs the user an edit.
func (a *App) dropDocuments() {
	for _, d := range slices.Clone(a.editor.documents) {
		if a.hostSession(d.Host) == nil && !d.dirty() {
			a.editor.removeDocument(d.Key)
		}
	}
}

// dirtyWith lists the unsaved documents that closing the session tabs ids leaves without a session.
func (a *App) dirtyWith(ids []string) []*remoteDocument {
	gone := map[string]bool{}
	for _, id := range ids {
		if i := a.tabIndex(id); i >= 0 && !a.Tabs[i].Local {
			gone[hostKey(a.Tabs[i])] = true
		}
	}
	for _, s := range a.Tabs {
		if !s.Local && !slices.Contains(ids, s.ID) {
			delete(gone, hostKey(s))
		}
	}
	var out []*remoteDocument
	for _, d := range a.editor.documents {
		if gone[d.Host] && d.dirty() {
			out = append(out, d)
		}
	}
	return out
}

// rehome gives the documents of the session old to its replacement next: a reconnect of a host with
// no saved connection is a session with a new ID, hence a new host key.
func (a *App) rehome(old, next *Session) {
	s := &a.editor
	for _, d := range s.documents {
		if d.Host != hostKey(old) {
			continue
		}
		from := d.Key
		d.Session, d.Host = next.ID, hostKey(next)
		d.Key = documentKey(d.Host, d.Document.Path)
		for _, k := range []*string{&s.shown, &s.win, &s.closeKey, &s.reloadKey} {
			if *k == from {
				*k = d.Key
			}
		}
	}
}

// DirtyDocumentCount is the number of remote documents with unsaved changes.
func (a *App) DirtyDocumentCount() int {
	n := 0
	for _, d := range a.editor.documents {
		if d.dirty() {
			n++
		}
	}
	return n
}

// saveDocument uploads d, which checks that the remote file did not change since it was opened.
func (a *App) saveDocument(d *remoteDocument) {
	d.sync()
	session := a.docSession(d)
	if a.tabIndex(session) < 0 {
		a.toast("这个服务器的连接已关闭，重新连接后才能保存")
		return
	}
	a.editor.fromWindow = d.Popped
	a.P.SaveRemote(session, d.Document, d.Text)
}

// editorDialogs opens the editor window when a document was popped out, and asks before a document
// of the main window closes.
func (a *App) editorDialogs(c *ui.Context) {
	s := &a.editor
	if s.popOut && a.ShowEditor != nil {
		s.popOut, s.windowed = false, true
		a.ShowEditor()
	}
	a.documentCloseDialog(c, mainWindow)
}

// EditorView builds the remote editor window: the documents popped out of the main window.
func (a *App) EditorView(c *ui.Context) {
	s := &a.editor
	a.view = editorWindow
	defer func() { a.view = mainWindow }()
	a.theme(c)
	a.wakeEditor = c.Services().Invalidate
	t := c.Theme()
	docs := a.documentsIn(true)
	var shown *remoteDocument
	if len(docs) > 0 {
		shown = docs[0]
		if d := s.document(s.win); d != nil && d.Popped {
			shown = d
		}
	}
	ui.Column(c).Fill().Padding(8, 12, 12, 12).Gap(8).Background(t.Background).Children(func() {
		if shown == nil {
			emptyState(c, "没有打开的远程文件", "在 SFTP 里双击文件即可编辑。")
			return
		}
		a.documentTabs(c, docs, shown)
		a.editorBody(c, shown)
	})
	if shown == nil && s.windowed && a.HideEditor != nil {
		s.windowed = false
		a.HideEditor()
	}
	a.documentCloseDialog(c, editorWindow)
	if s.closeAll {
		open := true
		n := 0
		for _, d := range docs {
			if d.dirty() {
				n++
			}
		}
		switch alert(c, &open, "关闭编辑器", fmt.Sprintf("%d 个文件的修改尚未保存，关闭后会丢失。", n), backOut("取消"), action("全部不保存", true)) {
		case 1:
			s.discardPopped()
		}
		if !open {
			s.closeAll = false
		}
	}
	a.detailView(c, editorWindow)
	a.runActions(c)
	a.flushToasts(c, editorWindow)
	if a.wakeMain != nil {
		a.wakeMain()
	}
}

// EditorMayClose guards closing the editor window: with unsaved documents in it, it asks in the
// window and returns false; otherwise it lets them go. The documents in tabs stay.
func (a *App) EditorMayClose() bool {
	s := &a.editor
	for _, d := range a.documentsIn(true) {
		if d.dirty() {
			s.closeAll = true
			return false
		}
	}
	s.discardPopped()
	s.windowed = false
	return true
}
func (s *editorState) discardPopped() {
	for _, d := range slices.Clone(s.documents) {
		if d.Popped {
			s.removeDocument(d.Key)
		}
	}
	s.closeAll = false
}

// editorTab is the main window's centre while a document's tab shows.
func (a *App) editorTab(c *ui.Context, d *remoteDocument) {
	ui.Column(c).Fill().MinWidth(0).MinHeight(0).Padding(8, 12, 12, 12).Gap(8).Background(c.Theme().Background).Children(func() { a.editorBody(c, d) })
}

func (a *App) editorBody(c *ui.Context, d *remoteDocument) {
	s := &a.editor
	t := c.Theme()
	if s.reloadKey == d.Key {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
			ui.Text(c, "重新载入会丢弃未保存的修改").TextColor(t.Warning).Grow(1).MinWidth(0)
			if ui.Button(c, "取消").Clicked() {
				s.reloadKey = ""
			}
			if ui.Button(c, "另存本地副本…").Clicked() {
				a.saveLocalCopy(d)
			}
			if ui.Button(c, "丢弃并重新载入").Clicked() {
				d.Text, d.Code = d.Document.Text, nil
				s.fromWindow = d.Popped
				a.P.ReloadRemote(a.docSession(d), d.Document.Path)
				s.reloadKey = ""
			}
		})
	}
	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		if d.dirty() {
			dirtyDot(c).Label("") // the tab says it
		}
		ui.Text(c, d.Document.Path).SingleLine().Grow(1).MinWidth(0).TextColor(t.TextMuted)
		if ui.Button(c, "重新载入").Clicked() {
			s.reloadKey = d.Key
		}
		if ui.Button(c, "用外部编辑器打开").Clicked() {
			if d.dirty() {
				a.toast("请先保存或放弃修改")
			} else {
				a.externalEdit(a.docSession(d), d.Document.Path)
			}
		}
		switch {
		case d.Popped:
			if ui.Button(c, "回到主窗口").Clicked() {
				a.later(func() { a.popBackDocument(d) })
			}
		case a.ShowEditor != nil:
			if ui.Button(c, "在新窗口打开").Clicked() {
				a.later(func() { a.popOutDocument(d) })
			}
		}
		if ui.PrimaryButton(c, "保存").KeyShortcuts(ui.Cmd, ui.KeyS).Clicked() || c.Shortcut(ui.Cmd, ui.KeyS) {
			a.saveDocument(d)
		}
	})
	d.prepare()
	editor.View(c.Key(d.Key), d.Code).Grow(1).FillWidth().AutoFocus().Border(1, t.Border)
	d.sync()
	muted(c, "UTF-8 文本，最大 8 MiB").SingleLine()
}

// dirtyDot marks a document with edits not saved: the orange dot of its tab, in the editor window too.
func dirtyDot(c *ui.Context) ui.Element {
	return ui.Box(c).Size(8, 8).Radius(4).Shrink(0).Background(c.Theme().Warning).Label("未保存")
}

// docTitle is the name on a document's tab: the file's, with its folder when another tab has the same name.
func (a *App) docTitle(d *remoteDocument) string {
	name := path.Base(d.Document.Path)
	for _, o := range a.editor.documents {
		if o != d && path.Base(o.Document.Path) == name {
			return path.Base(path.Dir(d.Document.Path)) + "/" + name
		}
	}
	return name
}

// editorTabs are the document tabs of the strip, after the sessions' and the tools'; first is the index of the first.
func (a *App) editorTabs(c *ui.Context, tabs ui.TabsParts, first int) {
	t := c.Theme()
	shown := a.activeEditor()
	for n, d := range a.documentsIn(false) {
		on := d == shown
		title := a.docTitle(d)
		dirty := d.dirty()
		tab := tabs.Tab(first+n).Label(title).Height(28).Padding(0, 6, 0, 10).Gap(6).Radius(t.Radius).AlignItems(ui.Center).Tooltip(d.Document.Path + "\n" + orDefault(d.Conn.Name, d.Host))
		if a.tabs.reveal == "doc:"+d.Key {
			tab.ScrollIntoView()
		}
		if a.middleClicked(tab, "doc:"+d.Key) {
			a.later(func() { a.requestDocumentClose(d.Key) })
		}
		tab.ContextMenu(func(m *ui.Menu) {
			if m.Item("保存").Disabled(!dirty).Chosen() {
				a.later(func() { a.saveDocument(d) })
			}
			if a.ShowEditor != nil && m.Item("在新窗口打开").Chosen() {
				a.later(func() { a.popOutDocument(d) })
			}
			m.Separator()
			if m.Item("关闭").Chosen() {
				a.later(func() { a.requestDocumentClose(d.Key) })
			}
		})
		switch {
		case on:
			tab.Background(t.Background).Border(1, t.Border)
		case tab.Hovered():
			tab.Background(t.SurfaceHover)
		}
		tab.Children(func() {
			color := t.TextMuted
			if on {
				color = t.Text
			}
			ui.Box(c).Size(3, 14).Radius(1.5).Background(a.hostColor(d.Conn))
			ui.Icon(c, appIcons["file"]).FontSize(13).TextColor(color)
			ui.Text(c, title).SingleLine().MaxWidth(180).FontSize(13).TextColor(color)
			x := ui.ButtonBase(c).Label("关闭标签 "+title).Tooltip("关闭标签").Size(18, 18).Radius(4).Center()
			hover := tab.Hovered() || x.Hovered()
			if x.Hovered() {
				x.Background(t.SurfacePressed)
			}
			x.Children(func() {
				if dirty && !hover { // the unsaved dot gives way to the × the pointer is after
					dirtyDot(c)
				} else {
					ui.Icon(c, appIcons["close"]).FontSize(12).TextColor(t.TextMuted)
				}
			})
			if x.Clicked() {
				a.later(func() { a.requestDocumentClose(d.Key) })
			}
		})
	}
}

// documentTabs is the strip of the editor window, over the documents popped out.
func (a *App) documentTabs(c *ui.Context, docs []*remoteDocument, shown *remoteDocument) {
	t := c.Theme()
	selected := slices.Index(docs, shown)
	ui.ScrollHorizontal(c).Height(36).FillWidth().Children(func() {
		tabs := ui.TabsBase(c.Key("remote-documents"), &selected, len(docs))
		tabs.List.Gap(4).Children(func() {
			for i, d := range docs {
				label := a.docTitle(d)
				e := tabs.Tab(i).Label("文档 "+d.Document.Path).Padding(6, 10).Gap(8).Radius(t.Radius).Tooltip(d.Document.Path).AlignItems(ui.Center)
				if d == shown {
					e.Background(t.SurfaceHover).TextColor(accentInk(t))
				}
				e.Children(func() {
					if d.dirty() {
						dirtyDot(c)
					}
					ui.Text(c, label).SingleLine()
					if ui.ButtonBase(c).Label("关闭文档 "+d.Document.Path).Size(18, 18).Center().Children(func() { ui.Icon(c, appIcons["close"]).FontSize(12) }).Clicked() {
						a.later(func() { a.requestDocumentClose(d.Key) })
					}
				})
			}
		})
	})
	if docs[selected] != shown {
		a.editor.win = docs[selected].Key
	}
}

// documentCloseDialog asks before closing an unsaved document, in the window that shows it.
func (a *App) documentCloseDialog(c *ui.Context, w window) {
	s := &a.editor
	d := s.document(s.closeKey)
	if d == nil {
		s.closeKey = ""
		return
	}
	if d.Popped != (w == editorWindow) {
		return
	}
	open := true
	// 保存 has the focus: Enter saves, and a failed save or a conflict leaves the file open; nothing is lost.
	switch alert(c, &open, "关闭未保存的文件", d.Document.Path+"\n修改尚未保存。保存失败或有冲突时文件会保持打开。", alertButton{label: "取消"}, alertButton{label: "不保存", risk: true}, alertButton{label: "保存", primary: true, focus: true}) {
	case 1:
		key := d.Key
		a.later(func() { s.removeDocument(key) })
	case 2:
		if s.closeSave == nil {
			s.closeSave = map[string]bool{}
		}
		s.closeSave[d.Key] = true
		a.saveDocument(d)
	}
	if !open {
		s.closeKey = ""
	}
}
