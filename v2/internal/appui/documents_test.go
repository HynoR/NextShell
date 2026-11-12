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
	s := &a.editor
	a.receiveDocument(opened("one", "/a", "original"))
	first := s.code
	s.code.Insert("draft")
	s.syncText()
	draft := s.text
	a.receiveDocument(opened("two", "/b", "other"))
	if len(s.documents) != 2 || s.documents[0].Text != draft {
		t.Fatal("draft lost")
	}
	second := s.code
	ack := opened("one", "/a", draft)
	ack.Saved = true
	a.receiveDocument(ack)
	if s.code != second || s.document.Path != "/b" {
		t.Fatal("inactive save stole focus")
	}
	s.selectDocument(0)
	if s.code != first || s.text != draft || s.document.Text != draft {
		t.Fatal("state not retained")
	}
	s.code.Insert("later")
	s.syncText()
	newer := s.text
	a.receiveDocument(ack)
	if s.text != newer || s.document.Text != draft || a.DirtyDocumentCount() != 1 {
		t.Fatal("in-flight save lost newer typing")
	}
	// A remote reload is not a save ack and cannot replace a dirty local baseline.
	a.receiveDocument(opened("one", "/a", "externally changed"))
	if s.text != newer || s.document.Text != draft {
		t.Fatal("dirty reload clobbered draft")
	}
	a.requestDocumentClose(s.documents[0].Key)
	if s.closeKey == "" || len(s.documents) != 2 {
		t.Fatal("dirty close lacked confirmation")
	}
	s.closeKey = ""
	s.removeDocument(s.documents[0].Key)
	if len(s.documents) != 1 || s.code != second {
		t.Fatal("close lost other state")
	}
}
func TestDocumentIdentityIncludesSessionAndDeduplicatesOpen(t *testing.T) {
	a := &App{}
	a.receiveDocument(opened("a", "/same", "a"))
	a.receiveDocument(opened("b", "/same", "b"))
	a.receiveDocument(opened("a", "/same", "a"))
	if len(a.editor.documents) != 2 || a.editor.session != "a" {
		t.Fatal(a.editor.documents)
	}
	a.requestDocumentClose(a.editor.documents[0].Key)
	if len(a.editor.documents) != 1 || a.editor.text != "b" {
		t.Fatal("clean close failed")
	}
}
