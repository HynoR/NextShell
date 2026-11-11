package storage

import (
	"nextshell.dev/v2/internal/model"
	"testing"
)

func TestImportBundleValidationAtomicAndNames(t *testing.T) {
	s, e := Open(":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	good := model.Connection{ID: "one", Name: "one", Host: "h", Port: 22, User: "u", Auth: "password", Secret: "secret"}
	if e = s.ApplyBundle(Bundle{Folders: []model.Folder{{ID: "f", Name: "  prod  "}}, Connections: []model.Connection{good}}); e != nil {
		t.Fatal(e)
	}
	if e = s.ApplyBundle(Bundle{Connections: []model.Connection{good}}); e == nil {
		t.Fatal(e)
	}
	fs, _ := s.Folders()
	if fs[0].Name != "prod" {
		t.Fatal(fs)
	}
	for _, b := range []Bundle{{Folders: []model.Folder{{ID: "a", Name: "a/b"}}}, {Folders: []model.Folder{{ID: "a", Name: `a\b`}}}, {Folders: []model.Folder{{ID: "a", Name: "same"}, {ID: "b", Name: "same"}}}, {Folders: []model.Folder{{ID: "a", Name: "a", ParentID: "b"}, {ID: "b", Name: "b", ParentID: "a"}}}, {Folders: []model.Folder{{ID: "a", Name: "a", ParentID: "absent"}}}, {Connections: []model.Connection{good}, Folders: []model.Folder{{ID: "new", Name: "new"}}}} {
		if e = s.ApplyBundle(b); e == nil {
			t.Fatal("invalid bundle accepted", b)
		}
		folders, _ := s.Folders()
		if len(folders) != 1 {
			t.Fatal("partially applied bundle")
		}
	}
	for _, name := range []string{" ", "a/b", `a\b`} {
		if s.SaveFolder(model.Folder{ID: "bad", Name: name}) == nil {
			t.Fatal("invalid folder name", name)
		}
	}
}
