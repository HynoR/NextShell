package storage

import (
	"bytes"
	"database/sql"
	"encoding/hex"
	"nextshell.dev/v2/internal/model"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, e := Open(":memory:")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func conn(id string) model.Connection {
	return model.Connection{ID: id, Name: "server", Host: "localhost", Port: 22, User: "tester", Auth: "password", Encoding: "utf-8", Keepalive: 30}
}
func TestDeviceKeyDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "profile.db")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	var stored string
	if e = s.db.QueryRow("SELECT value FROM app_settings WHERE key='device_key'").Scan(&stored); e != nil {
		t.Fatal(e)
	}
	key, e := hex.DecodeString(stored)
	if e != nil || len(key) != 32 || !bytes.Equal(key, s.key) {
		t.Fatal("bad key")
	}
	sec := "sensitive password"
	if e = s.SaveConnection(conn("a"), &sec); e != nil {
		t.Fatal(e)
	}
	s.Close()
	s, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if !bytes.Equal(key, s.key) {
		t.Fatal("key changed")
	}
	c, e := s.Connection("a")
	if e != nil || c.Secret != sec {
		t.Fatal(e, c)
	}
	stat, e := os.Stat(path)
	if e != nil {
		t.Fatal(e)
	}
	// Windows has no POSIX modes (Perm() is 0666 there): the profile directory's inherited ACL is what keeps
	// the database private, so only the plaintext check below applies.
	if runtime.GOOS != "windows" && stat.Mode().Perm() != 0600 {
		t.Fatalf("mode=%o", stat.Mode().Perm())
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(raw, []byte(sec)) {
		t.Fatal("plaintext secret on disk")
	}
}
func TestForeignDatabaseUnmodified(t *testing.T) {
	for _, app := range []int{0, 123} {
		t.Run(string(rune('A'+app%20)), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "legacy.db")
			db, e := sql.Open("sqlite", path)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = db.Exec("CREATE TABLE app_settings(key TEXT PRIMARY KEY,value TEXT); INSERT INTO app_settings VALUES('device_key','old'); PRAGMA user_version=77"); e != nil {
				t.Fatal(e)
			}
			if app != 0 {
				db.Exec("PRAGMA application_id=123")
			}
			db.Close()
			before, _ := os.ReadFile(path)
			if s, e := Open(path); e == nil {
				s.Close()
				t.Fatal("foreign DB accepted")
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(before, after) {
				t.Fatal("foreign database modified")
			}
		})
	}
}
func TestCorruptDeviceKeyNotReplaced(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v2.db")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	s.db.Exec("UPDATE app_settings SET value='bad' WHERE key='device_key'")
	s.Close()
	if s, e = Open(path); e == nil {
		s.Close()
		t.Fatal("corrupt key silently replaced")
	}
	db, e := sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	var got string
	db.QueryRow("SELECT value FROM app_settings WHERE key='device_key'").Scan(&got)
	if got != "bad" {
		t.Fatal("key replaced")
	}
}
func TestConnectionLifecycle(t *testing.T) {
	s := testStore(t)
	c := conn("a")
	sec := "secret"
	if e := s.SaveConnection(c, &sec); e != nil {
		t.Fatal(e)
	}
	all, e := s.Connections()
	if e != nil || len(all) != 1 || all[0].Secret != "" {
		t.Fatal("list leaked credentials", e)
	}
	c.Name = "renamed"
	if e = s.SaveConnection(c, nil); e != nil {
		t.Fatal(e)
	}
	got, e := s.Connection("a")
	if e != nil || got.Secret != sec || got.Name != "renamed" {
		t.Fatal(got, e)
	}
	dup, e := s.DuplicateConnection("a")
	if e != nil || dup.Secret != "" || dup.ID == c.ID {
		t.Fatal(dup, e)
	}
	got, e = s.Connection(dup.ID)
	if e != nil || got.Secret != sec {
		t.Fatal(got, e)
	}
	c.Deleted = true
	s.SaveConnection(c, nil)
	got, _ = s.Connection("a")
	if !got.Deleted {
		t.Fatal("not recycled")
	}
	c.Deleted = false
	empty := ""
	s.SaveConnection(c, &empty)
	got, _ = s.Connection("a")
	if got.Secret != "" || got.Deleted {
		t.Fatal("not restored/cleared")
	}
	if e = s.Delete("connection", "a"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Connection("a"); e == nil {
		t.Fatal("not deleted")
	}
}
func TestSecretsTamperAndAAD(t *testing.T) {
	s := testStore(t)
	sec := "do not leak"
	s.SaveConnection(conn("a"), &sec)
	s.SaveConnection(conn("b"), &sec)
	if _, e := s.db.Exec("UPDATE secrets SET envelope=(SELECT envelope FROM secrets WHERE id='connection:a') WHERE id='connection:b'"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Connection("b"); e == nil {
		t.Fatal("credential swap accepted")
	}
	s.db.Exec("UPDATE secrets SET envelope='{}' WHERE id='connection:a'")
	if _, e := s.Connection("a"); e == nil {
		t.Fatal("corrupt credential accepted")
	}
}
func TestFoldersReferencesAndCycle(t *testing.T) {
	s := testStore(t)
	if e := s.SaveFolder(model.Folder{ID: "a", Name: "root"}); e != nil {
		t.Fatal(e)
	}
	if e := s.SaveFolder(model.Folder{ID: "b", Name: "child", ParentID: "a"}); e != nil {
		t.Fatal(e)
	}
	if e := s.SaveFolder(model.Folder{ID: "a", Name: "root", ParentID: "b"}); e == nil {
		t.Fatal("cycle accepted")
	}
	if e := s.Delete("folder", "a"); e == nil {
		t.Fatal("parent deleted")
	}
	c := conn("x")
	c.FolderID = "b"
	if e := s.SaveConnection(c, nil); e != nil {
		t.Fatal(e)
	}
	if e := s.Delete("folder", "b"); e == nil {
		t.Fatal("used folder deleted")
	}
	s.Delete("connection", "x")
	s.Delete("folder", "b")
	if e := s.Delete("folder", "a"); e != nil {
		t.Fatal(e)
	}
	c.FolderID = "missing"
	if e := s.SaveConnection(c, nil); e == nil {
		t.Fatal("missing folder accepted")
	}
}
func TestKeysAndProxies(t *testing.T) {
	s := testStore(t)
	k := model.Key{ID: "k", Name: "key", Public: "pub", Private: "PRIVATE CONTENT", Passphrase: "KEY PASSPHRASE"}
	if e := s.SaveKey(k); e != nil {
		t.Fatal(e)
	}
	keys, e := s.Keys()
	if e != nil || len(keys) != 1 || keys[0].Private != "" || keys[0].Passphrase != "" {
		t.Fatal("keys leaked")
	}
	got, e := s.Key("k")
	if e != nil || got.Private != k.Private || got.Passphrase != k.Passphrase {
		t.Fatal(e)
	}
	if e = s.RenameKey("k", "renamed"); e != nil {
		t.Fatal(e)
	}
	got, _ = s.Key("k")
	if got.Name != "renamed" || got.Private != k.Private {
		t.Fatal(got)
	}
	p := model.Proxy{ID: "p", Name: "proxy", Kind: "socks5", Host: "localhost", Port: 1080}
	pass := "PROXY PASSWORD"
	if e = s.SaveProxy(p, &pass); e != nil {
		t.Fatal(e)
	}
	ps, _ := s.Proxies()
	if ps[0].Password != "" {
		t.Fatal("proxy password leak")
	}
	pg, e := s.Proxy("p")
	if e != nil || pg.Password != pass {
		t.Fatal(pg, e)
	}
	c := conn("a")
	c.Auth = "key"
	c.KeyID = "k"
	c.ProxyID = "p"
	s.SaveConnection(c, nil)
	for _, kind := range []string{"key", "proxy"} {
		id := "k"
		if kind == "proxy" {
			id = "p"
		}
		if e = s.Delete(kind, id); e == nil {
			t.Fatal("referenced resource deleted")
		}
	}
	s.Delete("connection", "a")
	if e = s.Delete("key", "k"); e != nil {
		t.Fatal(e)
	}
	if e = s.Delete("proxy", "p"); e != nil {
		t.Fatal(e)
	}
}
func TestHostPins(t *testing.T) {
	s := testStore(t)
	if got, e := s.HostPin("host:22"); e != nil || got != "" {
		t.Fatal(got, e)
	}
	if e := s.PinHost("host:22", "SHA256:first"); e != nil {
		t.Fatal(e)
	}
	if e := s.PinHost("host:22", "SHA256:first"); e != nil {
		t.Fatal(e)
	}
	if e := s.PinHost("host:22", "SHA256:changed"); e == nil {
		t.Fatal("changed key accepted")
	}
	got, _ := s.HostPin("host:22")
	if got != "SHA256:first" {
		t.Fatal(got)
	}
}
func TestHistoryBoundedPersistence(t *testing.T) {
	s := testStore(t)
	h, e := s.History()
	if e != nil || len(h) != 0 {
		t.Fatal(h, e)
	}
	h = make([]string, 123)
	for i := range h {
		h[i] = strings.Repeat("x", i+1)
	}
	if e = s.SaveHistory(h); e != nil {
		t.Fatal(e)
	}
	got, e := s.History()
	if e != nil || len(got) != 100 || got[0] != h[23] {
		t.Fatal("history cap")
	}
}
func TestStorageRejectInvalid(t *testing.T) {
	s := testStore(t)
	for _, c := range []model.Connection{{}, conn("")} {
		if e := s.SaveConnection(c, nil); e == nil {
			t.Fatal("bad connection accepted")
		}
	}
	if e := s.SaveFolder(model.Folder{ID: "x"}); e == nil {
		t.Fatal("bad folder")
	}
	if e := s.SaveKey(model.Key{ID: "x"}); e == nil {
		t.Fatal("bad key")
	}
	if e := s.SaveProxy(model.Proxy{ID: "x"}, nil); e == nil {
		t.Fatal("bad proxy")
	}
	if e := s.Delete("bad", "x"); e == nil {
		t.Fatal("bad kind")
	}
}

func TestConcurrentFolderMovesCannotCycle(t *testing.T) {
	s := testStore(t)
	for i := 0; i < 30; i++ {
		s.SaveFolder(model.Folder{ID: "a", Name: "A"})
		s.SaveFolder(model.Folder{ID: "b", Name: "B"})
		start := make(chan struct{})
		results := make(chan error, 2)
		var wg sync.WaitGroup
		for _, f := range []model.Folder{{ID: "a", Name: "A", ParentID: "b"}, {ID: "b", Name: "B", ParentID: "a"}} {
			wg.Add(1)
			go func(f model.Folder) { defer wg.Done(); <-start; results <- s.SaveFolder(f) }(f)
		}
		close(start)
		wg.Wait()
		a, b := <-results, <-results
		if a == nil && b == nil {
			t.Fatal("concurrent cycle accepted")
		}
	}
}
func TestFolderNamesAndDepth(t *testing.T) {
	s := testStore(t)
	if e := s.SaveFolder(model.Folder{ID: "blank", Name: " "}); e == nil {
		t.Fatal("blank accepted")
	}
	s.SaveFolder(model.Folder{ID: "a", Name: "A"})
	if e := s.SaveFolder(model.Folder{ID: "b", Name: "A"}); e == nil {
		t.Fatal("sibling name collision accepted")
	}
	parent := "a"
	for i := 0; i < 30; i++ {
		id := strings.Repeat("x", i+1)
		if e := s.SaveFolder(model.Folder{ID: id, Name: id, ParentID: parent}); e != nil {
			t.Fatal(i, e)
		}
		parent = id
	}
	if e := s.SaveFolder(model.Folder{ID: "deep", Name: "deep", ParentID: parent}); e == nil {
		t.Fatal("invisible depth accepted")
	}
}

func TestHostPinsListForgetAndUIState(t *testing.T) {
	s, e := Open(":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	s.PinHost("b.example:22", "SHA256:b")
	s.PinHost("a.example:22", "SHA256:a")
	s.SaveUIState("tabs", `["local"]`)
	keys, e := s.HostPins()
	if e != nil || len(keys) != 2 || keys[0].Address != "a.example:22" || keys[0].Fingerprint != "SHA256:a" {
		t.Fatal(keys, e)
	}
	if e = s.DeleteHostPin("a.example:22"); e != nil {
		t.Fatal(e)
	}
	if pin, _ := s.HostPin("a.example:22"); pin != "" {
		t.Fatal("not forgotten")
	}
	if v, _ := s.UIState("tabs"); v != `["local"]` {
		t.Fatal(v)
	}
	if v, e := s.UIState("missing"); v != "" || e != nil {
		t.Fatal(v, e)
	}
}
