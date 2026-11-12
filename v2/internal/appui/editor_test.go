package appui

import (
	"nextshell.dev/v2/internal/remoteedit"
	"testing"
)

func TestOfficialEditorBufferPreservesCRLFAndDirtyEvents(t *testing.T) {
	a := &App{}
	a.editor.document = remoteedit.Document{Path: "/a.go", Text: "one\r\ntwo\r\n"}
	a.editor.text = a.editor.document.Text
	s := &a.editor
	s.prepareBuffer()
	s.code.Buffer.Set("changed\ntext\n")
	s.syncText()
	if s.text != "changed\r\ntext\r\n" {
		t.Fatalf("CRLF lost: %q", s.text)
	}
	a.receiveDocument(&EditorResult{Document: remoteedit.Document{Path: "/b", Text: "unexpected"}})
	if len(s.documents) != 2 || s.documents[0].Document.Path != "/a.go" || s.documents[0].Text != "changed\r\ntext\r\n" {
		t.Fatal("dirty official buffer discarded")
	}
}
