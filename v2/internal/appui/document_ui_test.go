package appui_test

import (
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/remoteedit"
	"testing"
)

func TestOfficialDocumentTabsDirtyCloseCancelDiscard(t *testing.T) {
	a, p, tt := fixture(t)
	for _, file := range []string{"/one.txt", "/two.txt"} {
		p.events = append(p.events, appui.Event{Editor: &appui.EditorResult{Session: "remote", Document: remoteedit.Document{Path: file, Text: file}}})
	}
	tt.Frame()
	a.Phase2.Code.Insert("newer")
	tt.Frame()
	click(t, tt, "文档 remote /one.txt")
	if a.Phase2.Document.Path != "/one.txt" {
		t.Fatal("tab switch", a.Phase2.Document.Path)
	}
	click(t, tt, "文档 remote /two.txt")
	if a.DirtyDocumentCount() != 1 {
		t.Fatal("dirty state lost")
	}
	click(t, tt, "关闭文档 remote /two.txt")
	tt.Frame()
	if !tt.HasText("关闭未保存文档") {
		t.Fatal("no confirmation")
	}
	click(t, tt, "取消关闭")
	if len(a.Phase2.Documents) != 2 {
		t.Fatal("cancel closed")
	}
	click(t, tt, "关闭文档 remote /two.txt")
	tt.Frame()
	click(t, tt, "不保存关闭")
	if len(a.Phase2.Documents) != 1 || a.Phase2.Document.Path != "/one.txt" {
		t.Fatal("discard failed")
	}
}
