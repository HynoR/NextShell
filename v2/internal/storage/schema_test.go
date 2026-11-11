package storage

import (
	"bytes"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"nextshell.dev/v2/internal/model"
	"nextshell.dev/v2/internal/security"
	"path/filepath"
	"testing"
)

func TestSchema3OnlyUpgradesOwnV2Profile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v2.db")
	db, e := sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.Exec(`PRAGMA application_id=1314080818;PRAGMA user_version=2;CREATE TABLE app_settings(key TEXT PRIMARY KEY,value TEXT NOT NULL);CREATE TABLE resources(kind TEXT,id TEXT,data BLOB,PRIMARY KEY(kind,id));CREATE TABLE secrets(id TEXT PRIMARY KEY,envelope BLOB NOT NULL);`)
	if e != nil {
		t.Fatal(e)
	}
	key := bytes.Repeat([]byte{3}, 32)
	db.Exec("INSERT INTO app_settings VALUES('device_key',?)", hex.EncodeToString(key))
	db.Exec("INSERT INTO app_settings VALUES('legacy-archive:historical','leave me alone')")
	c := conn("stable-v2-id")
	raw, _ := json.Marshal(c)
	db.Exec("INSERT INTO resources VALUES('connection',?,?)", c.ID, raw)
	env, e := security.Seal([]byte("preserved-secret"), key, "nextshell-secret:connection:"+c.ID)
	if e != nil {
		t.Fatal(e)
	}
	db.Exec("INSERT INTO secrets VALUES(?,?)", "connection:"+c.ID, env)
	db.Close()
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	got, e := s.Connection(c.ID)
	if e != nil || got.Secret != "preserved-secret" || got.Scope != model.LocalScope || got.Revision != 1 || got.CreatedAt == "" {
		t.Fatal(got, e)
	}
	var version int
	s.db.QueryRow("PRAGMA user_version").Scan(&version)
	if version != 3 {
		t.Fatal(version)
	}
	var untouched string
	s.db.QueryRow("SELECT value FROM app_settings WHERE key='legacy-archive:historical'").Scan(&untouched)
	if untouched != "leave me alone" {
		t.Fatal("historical user data removed")
	}
	var after []byte
	s.db.QueryRow("SELECT envelope FROM secrets WHERE id=?", "connection:"+c.ID).Scan(&after)
	if !bytes.Equal(env, after) {
		t.Fatal("schema upgrade re-encrypted secrets")
	}
}
func TestMissingDeviceKeyWithSecretsFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v2.db")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	c := conn("one")
	secret := "secret"
	if e = s.SaveConnection(c, &secret); e != nil {
		t.Fatal(e)
	}
	s.db.Exec("DELETE FROM app_settings WHERE key='device_key'")
	s.Close()
	if s, e = Open(path); e == nil {
		s.Close()
		t.Fatal("missing key silently regenerated")
	}
	db, _ := sql.Open("sqlite", path)
	defer db.Close()
	var n int
	db.QueryRow("SELECT count(*) FROM app_settings WHERE key='device_key'").Scan(&n)
	if n != 0 {
		t.Fatal("replacement key written")
	}
}
func TestResourceMetadataNoOpAndAtomicStaleGuard(t *testing.T) {
	s, e := Open(":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	c := conn("one")
	secret := "secret"
	if e = s.SaveConnection(c, &secret); e != nil {
		t.Fatal(e)
	}
	before, _ := s.Connection(c.ID)
	g, _ := s.Generation()
	if e = s.SaveConnection(before, &secret); e != nil {
		t.Fatal(e)
	}
	after, _ := s.Connection(c.ID)
	next, _ := s.Generation()
	if before.Meta != after.Meta || next != g {
		t.Fatal("no-op created revision")
	}
	before.Name = "changed"
	if e = s.SaveConnection(before, nil); e != nil {
		t.Fatal(e)
	}
	after, _ = s.Connection(c.ID)
	if after.Revision != before.Revision+1 || after.CreatedAt != before.CreatedAt || after.UpdatedAt <= before.UpdatedAt || after.Secret != secret {
		t.Fatal(after)
	}
	b := Bundle{Folders: []model.Folder{{ID: "not-written", Name: "not-written"}}, ExpectedGeneration: &g}
	if e = s.ApplyBundle(b); e == nil {
		t.Fatal("stale import accepted")
	}
	folders, _ := s.Folders()
	if len(folders) != 0 {
		t.Fatal("partial stale import")
	}
}
func TestScopeIsolationAndFolderNames(t *testing.T) {
	s, e := Open(":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	w, _ := makeWorkspace(t, s)
	cloudFolder := model.Folder{Meta: model.Meta{Scope: w}, ID: ID(), Name: "same"}
	if e = s.SaveFolder(cloudFolder); e != nil {
		t.Fatal(e)
	}
	localFolder := model.Folder{ID: ID(), Name: "same"}
	if e = s.SaveFolder(localFolder); e != nil {
		t.Fatal(e)
	}
	c := conn(ID())
	c.FolderID = cloudFolder.ID
	if e = s.SaveConnection(c, nil); e == nil {
		t.Fatal("cross-scope folder accepted")
	}
	if e = s.ApplyBundle(Bundle{Folders: []model.Folder{{ID: ID(), Name: "other"}}}); e != nil {
		t.Fatal("unrelated equal folder names poisoned import", e)
	}
	unknown := model.Folder{Meta: model.Meta{Scope: ID()}, ID: ID(), Name: "unknown"}
	if e = s.SaveFolder(unknown); e == nil {
		t.Fatal("unknown workspace accepted")
	}
}

func TestDeletionTombstonePreventsImplicitIDReuse(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	c := conn(ID())
	if e := s.SaveConnection(c, nil); e != nil {
		t.Fatal(e)
	}
	if e := s.Delete("connection", c.ID); e != nil {
		t.Fatal(e)
	}
	if e := s.SaveConnection(c, nil); e == nil {
		t.Fatal("deleted UUID reused")
	}
	var n int
	s.db.QueryRow("SELECT count(*) FROM deletions WHERE kind='connection' AND id=?", c.ID).Scan(&n)
	if n != 1 {
		t.Fatal("deletion ledger lost")
	}
}
