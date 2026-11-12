package appui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"nextshell.dev/v2/internal/remoteedit"
)

// editorState holds the remote documents shown in the editor dialog. Each
// document owns an official editor State (undo, selections, folds, scroll);
// the active one is projected into the fields below for saving.
type editorState struct {
	open          bool
	documents     []*remoteDocument
	active        int
	closeKey      string
	code          *editor.State
	codeSource    string
	codePath      string
	codeVersion   uint64
	session       string
	document      remoteedit.Document
	text          string
	externalArgs  string
	reloadSession string
	reloadPath    string
	windowed      bool // the editor window is showing
	closeAll      bool // the window's close asks about unsaved documents
}

type remoteDocument struct {
	Key, Session, Text, Source string
	Document                   remoteedit.Document
	Code                       *editor.State
	Version                    uint64
}

func documentKey(session, path string) string { return session + "\x00" + path }

func (a *App) openEditor(session, remote string) {
	s := &a.editor
	s.storeActive()
	key := documentKey(session, remote)
	for i, d := range s.documents {
		if d.Key == key {
			s.selectDocument(i)
			s.open = true
			return
		}
	}
	a.P.OpenRemote(session, remote)
}
func (a *App) externalEdit(session, remote string) {
	if a.Prefs.EditorCommand == "" {
		a.toast("请先在设置的「通用」里选择外部编辑器")
		a.openSettings("通用")
		return
	}
	var args []string
	if s := a.editor.externalArgs; s != "" {
		if err := json.Unmarshal([]byte(s), &args); err != nil {
			a.fail(fmt.Errorf("编辑器参数须为 JSON 字符串数组，如 [\"--wait\"]"))
			return
		}
	}
	a.P.ExternalEdit(session, remote, a.Prefs.EditorCommand, args)
}

// syncText reads the buffer back; WriteTo (not String) keeps the buffer's CRLF.
func (s *editorState) syncText() {
	if s.code == nil || s.code.Buffer == nil || s.code.Buffer.Version() == s.codeVersion {
		return
	}
	var out bytes.Buffer
	s.code.Buffer.WriteTo(&out)
	s.text = out.String()
	s.codeSource = s.text
	s.codeVersion = s.code.Buffer.Version()
}
func (s *editorState) prepareBuffer() {
	key := documentKey(s.session, s.document.Path)
	if s.code == nil || s.code.Buffer == nil || s.codePath != key || s.codeSource != s.text {
		s.code = &editor.State{Buffer: editor.NewBuffer(s.text), Label: "远程文本"}
		s.code.Buffer.SetLanguage(editor.LanguageFor(s.document.Path))
		s.codePath = key
		s.codeSource = s.text
		s.codeVersion = s.code.Buffer.Version()
	}
}
func (s *editorState) storeActive() {
	if s.document.Path == "" {
		return
	}
	s.syncText()
	s.prepareBuffer()
	key := documentKey(s.session, s.document.Path)
	var d *remoteDocument
	for _, v := range s.documents {
		if v.Key == key {
			d = v
			break
		}
	}
	if d == nil {
		d = &remoteDocument{Key: key}
		s.documents = append(s.documents, d)
	}
	d.Session, d.Document, d.Text, d.Source, d.Code, d.Version = s.session, s.document, s.text, s.codeSource, s.code, s.codeVersion
}
func (s *editorState) selectDocument(i int) {
	if i < 0 || i >= len(s.documents) {
		return
	}
	s.storeActive()
	s.project(i)
}
func (s *editorState) project(i int) {
	d := s.documents[i]
	s.active = i
	s.session, s.document, s.text = d.Session, d.Document, d.Text
	s.code, s.codeVersion, s.codeSource, s.codePath = d.Code, d.Version, d.Source, d.Key
	s.prepareBuffer()
}

func (a *App) receiveDocument(r *EditorResult) {
	s := &a.editor
	s.storeActive()
	key := documentKey(r.Session, r.Document.Path)
	index := -1
	for i, d := range s.documents {
		if d.Key == key {
			index = i
			break
		}
	}
	fresh := index < 0
	if fresh {
		if len(s.documents) >= 32 {
			a.toast("最多同时打开 32 个远程文件，请先关闭一些")
			return
		}
		s.documents = append(s.documents, &remoteDocument{Key: key, Session: r.Session, Document: r.Document, Text: r.Document.Text})
		index = len(s.documents) - 1
	} else {
		d := s.documents[index]
		dirty := d.Text != d.Document.Text
		if r.Saved {
			d.Document = r.Document
			if d.Text == r.Document.Text && d.Code != nil {
				d.Code.Buffer.MarkSaved()
			}
		} else if dirty {
			a.inEditorWindow(func() { a.toast("远程文件有更新，已保留未保存的修改；保存时会检查冲突") })
		} else {
			d.Document = r.Document
			d.Text = r.Document.Text
			if d.Source != d.Text {
				d.Code = nil
			}
		}
	}
	// Save acknowledgements never steal focus from another document.
	if fresh || !r.Saved {
		s.project(index)
		s.open = true
	} else if documentKey(s.session, s.document.Path) == key {
		s.project(index)
	}
	s.storeActive()
}

func (a *App) requestDocumentClose(key string) {
	s := &a.editor
	s.storeActive()
	for _, d := range s.documents {
		if d.Key == key {
			if d.Text != d.Document.Text {
				s.closeKey = key
			} else {
				s.removeDocument(key)
			}
			return
		}
	}
}
func (s *editorState) removeDocument(key string) {
	s.storeActive()
	activeKey := documentKey(s.session, s.document.Path)
	for i, d := range s.documents {
		if d.Key == key {
			s.documents = append(s.documents[:i], s.documents[i+1:]...)
			break
		}
	}
	s.document, s.text, s.code, s.codePath, s.codeSource, s.session = remoteedit.Document{}, "", nil, "", "", ""
	if len(s.documents) == 0 {
		s.open = false
		return
	}
	next := min(s.active, len(s.documents)-1)
	for i, d := range s.documents {
		if d.Key == activeKey {
			next = i
			break
		}
	}
	s.project(next)
}

// DirtyDocumentCount is the number of remote documents with unsaved changes.
func (a *App) DirtyDocumentCount() int {
	s := &a.editor
	s.storeActive()
	n := 0
	for _, d := range s.documents {
		if d.Text != d.Document.Text {
			n++
		}
	}
	return n
}

// editorDialog shows the open remote documents: in their own window when
// the host has one, else over the main window.
func (a *App) editorDialog(c *ui.Context) {
	s := &a.editor
	if a.ShowEditor != nil {
		if s.open {
			s.open = false
			s.windowed = true
			a.ShowEditor()
		}
		return
	}
	if !s.open || len(s.documents) == 0 {
		return
	}
	w, h := c.Size()
	dialog(c, &s.open, func() {
		ui.Column(c).Width(max(w-120, 480)).Height(max(h-120, 360)).Gap(8).Children(func() { a.editorBody(c) })
	})
	a.documentCloseDialog(c)
}

// EditorView builds the remote editor window.
func (a *App) EditorView(c *ui.Context) {
	s := &a.editor
	a.view = editorWindow
	defer func() { a.view = mainWindow }()
	a.theme(c)
	a.wakeEditor = c.Services().Invalidate
	t := c.Theme()
	ui.Column(c).Fill().Padding(8, 12, 12, 12).Gap(8).Background(t.Background).Children(func() {
		if len(s.documents) == 0 {
			emptyState(c, "没有打开的远程文件", "在 SFTP 里双击文件即可编辑。")
			return
		}
		a.editorBody(c)
	})
	if len(s.documents) == 0 && s.windowed && a.HideEditor != nil {
		s.windowed = false
		a.HideEditor()
	}
	a.documentCloseDialog(c)
	if s.closeAll {
		open := true
		n := a.DirtyDocumentCount()
		switch ui.AlertDialog(c, &open, "关闭编辑器", fmt.Sprintf("%d 个文件的修改尚未保存。", n), "取消", "全部不保存") {
		case 1:
			s.discardAll()
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

// EditorMayClose guards closing the editor window: with unsaved documents
// it asks in the window and returns false; otherwise it lets the documents go.
func (a *App) EditorMayClose() bool {
	s := &a.editor
	if a.DirtyDocumentCount() > 0 {
		s.closeAll = true
		return false
	}
	s.discardAll()
	s.windowed = false
	return true
}
func (s *editorState) discardAll() {
	s.documents, s.document, s.text, s.code, s.codePath, s.codeSource, s.session, s.open = nil, remoteedit.Document{}, "", nil, "", "", "", false
}

func (a *App) editorBody(c *ui.Context) {
	s := &a.editor
	t := c.Theme()
	a.documentTabs(c)
	if s.reloadPath != "" {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "重新载入会丢弃未保存的修改").TextColor(t.Warning).Grow(1)
			if ui.Button(c, "取消").Clicked() {
				s.reloadPath = ""
			}
			if ui.Button(c, "丢弃并重新载入").Clicked() {
				s.text = s.document.Text
				a.P.ReloadRemote(s.reloadSession, s.reloadPath)
				s.reloadPath = ""
			}
		})
	}
	if s.document.Path == "" {
		return
	}
	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		label := s.document.Path
		if s.text != s.document.Text {
			label = "● " + label
		}
		ui.Text(c, label).SingleLine().Grow(1).MinWidth(0).TextColor(t.TextMuted)
		if ui.Button(c, "重新载入").Clicked() {
			s.reloadSession, s.reloadPath = s.session, s.document.Path
		}
		if ui.Button(c, "用外部编辑器打开").Clicked() {
			if s.text != s.document.Text {
				a.toast("请先保存或放弃修改")
			} else {
				a.externalEdit(s.session, s.document.Path)
			}
		}
		if ui.PrimaryButton(c, "保存").KeyShortcuts(ui.Cmd, ui.KeyS).Clicked() || c.Shortcut(ui.Cmd, ui.KeyS) {
			a.P.SaveRemote(s.session, s.document, s.text)
		}
	})
	s.prepareBuffer()
	editor.View(c.Key(documentKey(s.session, s.document.Path)), s.code).Grow(1).FillWidth().AutoFocus().Border(1, t.Border)
	s.syncText()
	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		muted(c, "UTF-8 文本，最大 8 MiB · 外部编辑器参数").SingleLine()
		ui.TextInput(c, &s.externalArgs).Label("外部编辑器参数").Placeholder(`["--wait"]`).Width(200)
	})
}

func (a *App) documentTabs(c *ui.Context) {
	s := &a.editor
	t := c.Theme()
	s.storeActive()
	selected := s.active
	ui.ScrollHorizontal(c).Height(36).FillWidth().Children(func() {
		tabs := ui.TabsBase(c.Key("remote-documents"), &selected, len(s.documents))
		tabs.List.Gap(4).Children(func() {
			for i, d := range s.documents {
				label := path.Base(d.Document.Path)
				if d.Text != d.Document.Text {
					label = "● " + label
				}
				e := tabs.Tab(i).Label("文档 "+d.Document.Path).Padding(6, 10).Gap(8).Radius(t.Radius).Tooltip(d.Document.Path).AlignItems(ui.Center)
				if i == s.active {
					e.Background(t.SurfaceHover).TextColor(accentInk(t))
				}
				e.Children(func() {
					ui.Text(c, label).SingleLine()
					if ui.ButtonBase(c).Label("关闭文档 "+d.Document.Path).Size(18, 18).Center().Children(func() { ui.Icon(c, appIcons["close"]).FontSize(12) }).Clicked() {
						key := d.Key
						a.later(func() { a.requestDocumentClose(key) })
					}
				})
			}
		})
	})
	if selected != s.active {
		a.later(func() { s.selectDocument(selected) })
	}
}
func (a *App) documentCloseDialog(c *ui.Context) {
	s := &a.editor
	if s.closeKey == "" {
		return
	}
	var d *remoteDocument
	for _, v := range s.documents {
		if v.Key == s.closeKey {
			d = v
		}
	}
	if d == nil {
		s.closeKey = ""
		return
	}
	open := true
	switch ui.AlertDialog(c, &open, "关闭未保存的文件", d.Document.Path+"\n修改尚未保存。保存失败或有冲突时文件会保持打开。", "取消", "不保存", "保存") {
	case 1:
		key := d.Key
		a.later(func() { s.removeDocument(key) })
	case 2:
		a.P.SaveRemote(d.Session, d.Document, d.Text)
	}
	if !open {
		s.closeKey = ""
	}
}
