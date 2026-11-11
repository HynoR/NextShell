package appui

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"nextshell.dev/v2/internal/remoteedit"
	"path"
)

// Each document owns an official editor State (undo, selections, folds, scroll).
// Active projection fields remain for the external-editor and save interfaces.
type remoteDocument struct {
	Key, Session, Text, Source string
	Document                   remoteedit.Document
	Code                       *editor.State
	Version                    uint64
}

func documentKey(session, path string) string { return session + "\x00" + path }
func (s *phase2State) storeActiveDocument() {
	if s.Document.Path == "" {
		return
	}
	s.syncEditorText()
	s.prepareEditorBuffer()
	key := documentKey(s.EditorSession, s.Document.Path)
	var d *remoteDocument
	for _, v := range s.Documents {
		if v.Key == key {
			d = v
			break
		}
	}
	if d == nil {
		d = &remoteDocument{Key: key}
		s.Documents = append(s.Documents, d)
	}
	d.Session = s.EditorSession
	d.Document = s.Document
	d.Text = s.EditorText
	d.Source = s.CodeSource
	d.Code = s.Code
	d.Version = s.CodeVersion
}
func (s *phase2State) selectDocument(i int) {
	if i < 0 || i >= len(s.Documents) {
		return
	}
	s.storeActiveDocument()
	s.projectDocument(i)
}
func (s *phase2State) projectDocument(i int) {
	d := s.Documents[i]
	s.ActiveDocument = i
	s.EditorSession = d.Session
	s.Document = d.Document
	s.EditorText = d.Text
	s.Code = d.Code
	s.CodeVersion = d.Version
	s.CodeSource = d.Source
	s.CodePath = d.Key
	s.prepareEditorBuffer()
}
func (a *App) receiveDocument(r *EditorResult) {
	s := &a.Phase2
	s.storeActiveDocument()
	key := documentKey(r.Session, r.Document.Path)
	index := -1
	for i, d := range s.Documents {
		if d.Key == key {
			index = i
			break
		}
	}
	fresh := index < 0
	if fresh {
		if len(s.Documents) >= 32 {
			a.Message = "最多同时打开 32 个远程文档，请先关闭部分文档"
			return
		}
		s.Documents = append(s.Documents, &remoteDocument{Key: key, Session: r.Session, Document: r.Document, Text: r.Document.Text})
		index = len(s.Documents) - 1
	} else {
		d := s.Documents[index]
		dirty := d.Text != d.Document.Text
		if r.Saved {
			d.Document = r.Document
			if d.Text == r.Document.Text && d.Code != nil {
				d.Code.Buffer.MarkSaved()
			}
		} else if dirty {
			a.Message = "远端更新到达，已保留该文档的未保存文本；保存会核对远端哈希"
		} else {
			d.Document = r.Document
			d.Text = r.Document.Text
			if d.Source != d.Text {
				d.Code = nil
			}
		}
	}
	// Save acknowledgements never steal focus from another document/page.
	if fresh || !r.Saved {
		s.projectDocument(index)
		a.Page = "editor"
	} else if documentKey(s.EditorSession, s.Document.Path) == key {
		s.projectDocument(index)
	}
	s.storeActiveDocument()
}
func (a *App) documentTabs(c *ui.Context) {
	s := &a.Phase2
	s.storeActiveDocument()
	if len(s.Documents) == 0 {
		return
	}
	selected := s.ActiveDocument
	ui.ScrollHorizontal(c).Height(44).FillWidth().Children(func() {
		tabs := ui.TabsBase(c.Key("remote-documents"), &selected, len(s.Documents))
		tabs.List.Gap(4).Children(func() {
			for i, d := range s.Documents {
				label := path.Base(d.Document.Path)
				if d.Text != d.Document.Text {
					label = "● " + label
				}
				e := tabs.Tab(i).Label("文档 "+d.Session+" "+d.Document.Path).Padding(8, 10).Gap(8).Radius(PanelRadius).Tooltip(d.Session + " · " + d.Document.Path)
				if i == s.ActiveDocument {
					e.Background(c.Theme().SurfaceHover).TextColor(c.Theme().Accent)
				}
				e.Children(func() {
					ui.Icon(c, appIcons["edit"]).FontSize(15)
					ui.Text(c, label).SingleLine()
					if ui.ButtonBase(c).Label("关闭文档 "+d.Session+" "+d.Document.Path).Size(20, 20).Center().Children(func() { ui.Text(c, "×") }).Clicked() {
						key := d.Key
						a.later(func() { a.requestDocumentClose(key) })
					}
				})
			}
		})
	})
	if selected != s.ActiveDocument {
		a.later(func() { s.selectDocument(selected) })
	}
}
func (a *App) requestDocumentClose(key string) {
	s := &a.Phase2
	s.storeActiveDocument()
	for _, d := range s.Documents {
		if d.Key == key {
			if d.Text != d.Document.Text {
				s.CloseDocumentKey = key
			} else {
				s.removeDocument(key)
			}
			return
		}
	}
}
func (s *phase2State) removeDocument(key string) {
	s.storeActiveDocument()
	activeKey := documentKey(s.EditorSession, s.Document.Path)
	for i, d := range s.Documents {
		if d.Key != key {
			continue
		}
		s.Documents = append(s.Documents[:i], s.Documents[i+1:]...)
		break
	}
	s.Document = remoteedit.Document{}
	s.EditorText = ""
	s.Code = nil
	s.CodePath = ""
	s.CodeSource = ""
	s.EditorSession = ""
	if len(s.Documents) > 0 {
		next := min(s.ActiveDocument, len(s.Documents)-1)
		for i, d := range s.Documents {
			if d.Key == activeKey {
				next = i
				break
			}
		}
		s.projectDocument(next)
	}
}
func (a *App) documentCloseDialog(c *ui.Context) {
	s := &a.Phase2
	if s.CloseDocumentKey == "" {
		return
	}
	var d *remoteDocument
	for _, v := range s.Documents {
		if v.Key == s.CloseDocumentKey {
			d = v
			break
		}
	}
	if d == nil {
		s.CloseDocumentKey = ""
		return
	}
	open := true
	choice := ui.AlertDialog(c, &open, "关闭未保存文档", fmt.Sprintf("%s\n修改尚未保存。保存失败或发生远端冲突时，文档会保持打开。", d.Document.Path), "取消关闭", "不保存关闭", "保存并保留文档")
	switch choice {
	case 1:
		key := d.Key
		a.later(func() { s.removeDocument(key) })
	case 2:
		a.P.SaveRemote(d.Session, d.Document, d.Text)
	}
	if !open {
		s.CloseDocumentKey = ""
	}
}
func (a *App) DirtyDocumentCount() int {
	s := &a.Phase2
	s.storeActiveDocument()
	n := 0
	for _, d := range s.Documents {
		if d.Text != d.Document.Text {
			n++
		}
	}
	return n
}
