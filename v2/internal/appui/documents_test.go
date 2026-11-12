package appui

import (
	"testing"

	"nextshell.dev/v2/internal/remoteedit"
)

func opened(session, path, text string) *EditorResult {
	return &EditorResult{Session: session, Document: remoteedit.Document{Path: path, Text: text}}
}
func TestIndependentDocumentBuffersAndLateSaveAcknowledgements(t *testing.T) {
	a := &App{}
	s := &a.editor
	a.receiveDocument(opened("one", "/a", "original"))
	d1 := s.documents[0]
	first := d1.Code
	d1.Code.Insert("draft")
	d1.sync()
	draft := d1.Text
	a.receiveDocument(opened("two", "/b", "other"))
	if len(s.documents) != 2 || s.documents[0].Text != draft {
		t.Fatal("draft lost")
	}
	d2 := s.documents[1]
	second := d2.Code
	ack := opened("one", "/a", draft)
	ack.Saved = true
	a.receiveDocument(ack)
	if s.shown != d2.Key {
		t.Fatal("inactive save stole focus")
	}
	a.showDocument(d1)
	if d1.Code != first || d1.Text != draft || d1.Document.Text != draft {
		t.Fatal("state not retained")
	}
	d1.Code.Insert("later")
	d1.sync()
	newer := d1.Text
	a.receiveDocument(ack)
	if d1.Text != newer || d1.Document.Text != draft || a.DirtyDocumentCount() != 1 {
		t.Fatal("in-flight save lost newer typing")
	}
	// A remote reload is not a save ack and cannot replace a dirty local baseline.
	a.receiveDocument(opened("one", "/a", "externally changed"))
	if d1.Text != newer || d1.Document.Text != draft {
		t.Fatal("dirty reload clobbered draft")
	}
	a.requestDocumentClose(d1.Key)
	if s.closeKey == "" || len(s.documents) != 2 {
		t.Fatal("dirty close lacked confirmation")
	}
	s.closeKey = ""
	s.removeDocument(d1.Key)
	if len(s.documents) != 1 || s.documents[0].Code != second {
		t.Fatal("close lost other state")
	}
}
func TestDocumentIdentityIncludesSessionAndDeduplicatesOpen(t *testing.T) {
	a := &App{}
	a.receiveDocument(opened("a", "/same", "a"))
	a.receiveDocument(opened("b", "/same", "b"))
	a.receiveDocument(opened("a", "/same", "a"))
	if len(a.editor.documents) != 2 || a.editor.shown != a.editor.documents[0].Key {
		t.Fatal(a.editor.documents)
	}
	a.requestDocumentClose(a.editor.documents[0].Key)
	if len(a.editor.documents) != 1 || a.editor.documents[0].Text != "b" {
		t.Fatal("clean close failed")
	}
}
