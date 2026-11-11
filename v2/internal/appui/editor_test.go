package appui

import (
	"nextshell.dev/v2/internal/remoteedit"
	"testing"
)

func TestOfficialEditorBufferPreservesCRLFAndDirtyEvents(t *testing.T) {
	a := &App{}
	a.Phase2.Document = remoteedit.Document{Path: "/a.go", Text: "one\r\ntwo\r\n"}
	a.Phase2.EditorText = a.Phase2.Document.Text
	s := &a.Phase2
	s.prepareEditorBuffer()
	s.Code.Buffer.Set("changed\ntext\n")
	s.syncEditorText()
	if s.EditorText != "changed\r\ntext\r\n" {
		t.Fatalf("CRLF lost: %q", s.EditorText)
	}
	a.handlePhase2(Event{Editor: &EditorResult{Document: remoteedit.Document{Path: "/b", Text: "unexpected"}}})
	if s.Document.Path != "/a.go" || s.EditorText != "changed\r\ntext\r\n" {
		t.Fatal("dirty official buffer discarded")
	}
}
