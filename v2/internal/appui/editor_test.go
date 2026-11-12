package appui

import (
	"slices"
	"testing"

	"nextshell.dev/v2/internal/remoteedit"
)

func TestOfficialEditorBufferPreservesCRLFAndDirtyEvents(t *testing.T) {
	a := &App{}
	a.receiveDocument(&EditorResult{Session: "s", Document: remoteedit.Document{Path: "/a.go", Text: "one\r\ntwo\r\n"}})
	s := &a.editor
	d := s.documents[0]
	d.Code.Buffer.Set("changed\ntext\n")
	d.sync()
	if d.Text != "changed\r\ntext\r\n" {
		t.Fatalf("CRLF lost: %q", d.Text)
	}
	a.receiveDocument(&EditorResult{Session: "s", Document: remoteedit.Document{Path: "/b", Text: "unexpected"}})
	if len(s.documents) != 2 || s.documents[0].Document.Path != "/a.go" || s.documents[0].Text != "changed\r\ntext\r\n" {
		t.Fatal("dirty official buffer discarded")
	}
}

// The external editor's arguments are typed as plain text: spaces separate, quotes keep spaces together.
func TestSplitArgs(t *testing.T) {
	for in, want := range map[string][]string{
		"":                              nil,
		"   ":                           nil,
		"--wait":                        {"--wait"},
		"-n --wait":                     {"-n", "--wait"},
		`-g "my file.txt" -w`:           {"-g", "my file.txt", "-w"},
		`--folder='C:\Program Files\x'`: {`--folder=C:\Program Files\x`},
		`""`:                            {""},
		"a\t b":                         {"a", "b"},
		`"don't" 'say "hi"'`:            {"don't", `say "hi"`},
	} {
		got, err := splitArgs(in)
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("%q: %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := splitArgs(`--a "b`); err == nil {
		t.Error("an open quote is an error")
	}
}
