package appui

import (
	"nextshell.dev/v2/internal/remoteedit"
	"testing"
)

func opened(session, path, text string) *EditorResult {
	return &EditorResult{Session: session, Document: remoteedit.Document{Path: path, Text: text}}
}
func TestIndependentDocumentBuffersAndLateSaveAcknowledgements(t *testing.T) {
	a := &App{}
	s := &a.Phase2
	a.receiveDocument(opened("one", "/a", "original"))
	first := s.Code
	s.Code.Insert("draft")
	s.syncEditorText()
	draft := s.EditorText
	a.receiveDocument(opened("two", "/b", "other"))
	if len(s.Documents) != 2 || s.Documents[0].Text != draft {
		t.Fatal("draft lost")
	}
	second := s.Code
	ack := opened("one", "/a", draft)
	ack.Saved = true
	a.receiveDocument(ack)
	if s.Code != second || s.Document.Path != "/b" {
		t.Fatal("inactive save stole focus")
	}
	s.selectDocument(0)
	if s.Code != first || s.EditorText != draft || s.Document.Text != draft {
		t.Fatal("state not retained")
	}
	s.Code.Insert("later")
	s.syncEditorText()
	newer := s.EditorText
	a.receiveDocument(ack)
	if s.EditorText != newer || s.Document.Text != draft || a.DirtyDocumentCount() != 1 {
		t.Fatal("in-flight save lost newer typing")
	}
	// A remote reload is not a save ack and cannot replace a dirty local baseline.
	a.receiveDocument(opened("one", "/a", "externally changed"))
	if s.EditorText != newer || s.Document.Text != draft {
		t.Fatal("dirty reload clobbered draft")
	}
	a.requestDocumentClose(s.Documents[0].Key)
	if s.CloseDocumentKey == "" || len(s.Documents) != 2 {
		t.Fatal("dirty close lacked confirmation")
	}
	s.CloseDocumentKey = ""
	s.removeDocument(s.Documents[0].Key)
	if len(s.Documents) != 1 || s.Code != second {
		t.Fatal("close lost other state")
	}
}
func TestDocumentIdentityIncludesSessionAndDeduplicatesOpen(t *testing.T) {
	a := &App{}
	a.receiveDocument(opened("a", "/same", "a"))
	a.receiveDocument(opened("b", "/same", "b"))
	a.receiveDocument(opened("a", "/same", "a"))
	if len(a.Phase2.Documents) != 2 || a.Phase2.EditorSession != "a" {
		t.Fatal(a.Phase2.Documents)
	}
	a.requestDocumentClose(a.Phase2.Documents[0].Key)
	if len(a.Phase2.Documents) != 1 || a.Phase2.EditorText != "b" {
		t.Fatal("clean close failed")
	}
}
