// Package storage owns the new schema. It never opens or mutates a 1.x database.
package storage

import (
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	_ "modernc.org/sqlite"
	"nextshell.dev/v2/internal/model"
	"nextshell.dev/v2/internal/security"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Store struct {
	db  *sql.DB
	key []byte
	mu  sync.Mutex
}

func Open(path string) (*Store, error) {
	if path != ":memory:" {
		if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
			return nil, e
		}
		f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
		if e != nil {
			return nil, e
		}
		f.Close()
	}
	db, e := sql.Open("sqlite", path)
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	// Reject an unrelated/legacy SQLite database before any schema writes.
	var appID, tables, version int
	if e = db.QueryRow("PRAGMA application_id").Scan(&appID); e != nil {
		db.Close()
		return nil, e
	}
	if e = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&tables); e != nil {
		db.Close()
		return nil, e
	}
	if e = db.QueryRow("PRAGMA user_version").Scan(&version); e != nil {
		db.Close()
		return nil, e
	}
	if (tables > 0 && appID != 0x4e534832) || (appID != 0 && appID != 0x4e534832) || version > 3 {
		db.Close()
		return nil, errors.New("not a supported NextShell v2 profile; refusing to modify legacy/foreign database")
	}
	if path != ":memory:" {
		if e = os.Chmod(path, 0600); e != nil {
			db.Close()
			return nil, e
		}
	}
	_, e = db.Exec(`PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000; PRAGMA application_id=1314080818;`)
	if e != nil {
		db.Close()
		return nil, e
	}
	if e = s.upgradeSchema(); e != nil {
		db.Close()
		return nil, e
	}
	if e = s.resolveKey(); e != nil {
		db.Close()
		return nil, e
	}
	return s, nil
}
func (s *Store) resolveKey() error {
	var h string
	e := s.db.QueryRow("SELECT value FROM app_settings WHERE key='device_key'").Scan(&h)
	if errors.Is(e, sql.ErrNoRows) {
		var count int
		if err := s.db.QueryRow("SELECT count(*) FROM secrets").Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			return errors.New("device key missing with existing secrets; refusing replacement")
		}
		k, e := security.Random(32)
		if e != nil {
			return e
		}
		h = hex.EncodeToString(k)
		_, e = s.db.Exec("INSERT INTO app_settings(key,value) VALUES('device_key',?) ON CONFLICT(key) DO NOTHING", h)
		if e != nil {
			return e
		}
	} else if e != nil {
		return e
	}
	if e = s.db.QueryRow("SELECT value FROM app_settings WHERE key='device_key'").Scan(&h); e != nil {
		return e
	}
	k, e := hex.DecodeString(h)
	if e != nil || len(k) != 32 {
		return errors.New("invalid stored device key; refusing to replace it")
	}
	s.key = k
	return nil
}
func (s *Store) Close() error { return s.db.Close() }
func ID() string              { return uuid.NewString() }
func (s *Store) put(kind, id string, v any, secret *string) error {
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = s.putTx(tx, kind, id, v, secret, false); e != nil {
		return e
	}
	return tx.Commit()
}

func list[T any](s *Store, kind string) ([]T, error) {
	r, e := s.db.Query("SELECT data FROM resources WHERE kind=? ORDER BY id", kind)
	if e != nil {
		return nil, e
	}
	defer r.Close()
	out := []T{}
	for r.Next() {
		var b []byte
		var v T
		if e = r.Scan(&b); e != nil {
			return nil, e
		}
		if e = json.Unmarshal(b, &v); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, r.Err()
}
func get[T any](s *Store, kind, id string) (T, error) {
	var v T
	var b []byte
	e := s.db.QueryRow("SELECT data FROM resources WHERE kind=? AND id=?", kind, id).Scan(&b)
	if e != nil {
		return v, e
	}
	return v, json.Unmarshal(b, &v)
}
func (s *Store) secret(kind, id string) (string, error) {
	var b []byte
	e := s.db.QueryRow("SELECT envelope FROM secrets WHERE id=?", kind+":"+id).Scan(&b)
	if errors.Is(e, sql.ErrNoRows) {
		return "", nil
	}
	if e != nil {
		return "", e
	}
	p, e := security.Unseal(b, s.key, "nextshell-secret:"+kind+":"+id)
	return string(p), e
}
func (s *Store) Connections() ([]model.Connection, error) {
	return list[model.Connection](s, "connection")
}
func (s *Store) Connection(id string) (model.Connection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.connectionUnlocked(id)
}

// connectionUnlocked requires Store.mu; used by atomic compound operations.
func (s *Store) connectionUnlocked(id string) (model.Connection, error) {
	c, e := get[model.Connection](s, "connection", id)
	if e != nil {
		return c, e
	}
	c.Secret, e = s.secret("connection", id)
	return c, e
}
func (s *Store) SaveConnection(c model.Connection, secret *string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c.ID == "" {
		return errors.New("missing id")
	}
	if e := c.Validate(); e != nil {
		return e
	}
	if c.FolderID != "" {
		if _, e := get[model.Folder](s, "folder", c.FolderID); e != nil {
			return fmt.Errorf("folder: %w", e)
		}
	}
	if c.KeyID != "" {
		if _, e := get[model.Key](s, "key", c.KeyID); e != nil {
			return e
		}
	}
	if c.ProxyID != "" {
		if _, e := get[model.Proxy](s, "proxy", c.ProxyID); e != nil {
			return e
		}
	}
	return s.put("connection", c.ID, c, secret)
}
func (s *Store) Folders() ([]model.Folder, error) { return list[model.Folder](s, "folder") }
func (s *Store) SaveFolder(f model.Folder) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	name, err := model.NormalizeFolderName(f.Name)
	if err != nil {
		return err
	}
	f.Name = name
	if f.ID == "" {
		return errors.New("invalid folder")
	}
	folders, e := s.Folders()
	if e != nil {
		return e
	}
	for _, other := range folders {
		if other.ID != f.ID && other.Name == f.Name && other.ParentID == f.ParentID && other.ScopeID() == f.ScopeID() {
			return errors.New("duplicate folder name in the same parent")
		}
	}
	seen := map[string]bool{f.ID: true}
	for p := f.ParentID; p != ""; {
		if seen[p] {
			return errors.New("folder cycle")
		}
		seen[p] = true
		if len(seen) > 31 {
			return errors.New("folder nesting exceeds 30 levels")
		}
		a, e := get[model.Folder](s, "folder", p)
		if e != nil {
			return e
		}
		p = a.ParentID
	}
	return s.put("folder", f.ID, f, nil)
}
func (s *Store) Keys() ([]model.Key, error) { return list[model.Key](s, "key") }
func (s *Store) Key(id string) (model.Key, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.keyUnlocked(id)
}

// keyUnlocked requires Store.mu; used by atomic compound operations.
func (s *Store) keyUnlocked(id string) (model.Key, error) {
	k, e := get[model.Key](s, "key", id)
	if e != nil {
		return k, e
	}
	v, e := s.secret("key", id)
	if e != nil {
		return k, e
	}
	var sec struct{ Private, Passphrase string }
	e = json.Unmarshal([]byte(v), &sec)
	k.Private = sec.Private
	k.Passphrase = sec.Passphrase
	return k, e
}
func (s *Store) SaveKey(k model.Key) error {
	keyMetadata(&k)
	s.mu.Lock()
	defer s.mu.Unlock()
	if k.ID == "" || k.Name == "" || k.Private == "" {
		return errors.New("invalid key")
	}
	b, e := json.Marshal(struct{ Private, Passphrase string }{k.Private, k.Passphrase})
	if e != nil {
		return e
	}
	secret := string(b)
	return s.put("key", k.ID, k, &secret)
}
func (s *Store) Proxies() ([]model.Proxy, error) { return list[model.Proxy](s, "proxy") }
func (s *Store) Proxy(id string) (model.Proxy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.proxyUnlocked(id)
}

// proxyUnlocked requires Store.mu; used by atomic compound operations.
func (s *Store) proxyUnlocked(id string) (model.Proxy, error) {
	p, e := get[model.Proxy](s, "proxy", id)
	if e != nil {
		return p, e
	}
	p.Password, e = s.secret("proxy", id)
	return p, e
}
func (s *Store) SaveProxy(p model.Proxy, secret *string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p.ID == "" {
		return errors.New("missing id")
	}
	if e := p.Validate(); e != nil {
		return e
	}
	return s.put("proxy", p.ID, p, secret)
}
func (s *Store) Delete(kind, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if kind != "connection" && kind != "folder" && kind != "key" && kind != "proxy" && kind != "command" {
		return errors.New("invalid resource kind")
	}
	cs, e := s.Connections()
	if e != nil {
		return e
	}
	for _, c := range cs {
		if kind == "folder" && c.FolderID == id || kind == "key" && c.KeyID == id || kind == "proxy" && c.ProxyID == id {
			return errors.New("resource is in use")
		}
	}
	if kind == "folder" {
		fs, e := s.Folders()
		if e != nil {
			return e
		}
		for _, f := range fs {
			if f.ParentID == id {
				return errors.New("folder has children")
			}
		}
	}
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = s.tombstoneTx(tx, kind, id); e != nil {
		return e
	}
	if _, e = tx.Exec("DELETE FROM resources WHERE kind=? AND id=?", kind, id); e != nil {
		return e
	}
	if _, e = tx.Exec("DELETE FROM secrets WHERE id=?", kind+":"+id); e != nil {
		return e
	}
	return tx.Commit()
}

// PinHost inserts a first-use pin atomically, or rejects a changed pin.
func (s *Store) PinHost(address, fp string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, e := s.db.Exec("INSERT INTO app_settings(key,value) VALUES(?,?) ON CONFLICT(key) DO NOTHING", "host:"+address, fp)
	if e != nil {
		return e
	}
	var got string
	e = s.db.QueryRow("SELECT value FROM app_settings WHERE key=?", "host:"+address).Scan(&got)
	if e != nil {
		return e
	}
	if got != fp {
		return errors.New("host key changed")
	}
	return nil
}

// ReplaceHostPin overwrites a pin after the user confirmed a changed host key.
func (s *Store) ReplaceHostPin(address, fp string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, e := s.db.Exec("INSERT INTO app_settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", "host:"+address, fp)
	return e
}
func (s *Store) HostPin(address string) (string, error) {
	var fp string
	e := s.db.QueryRow("SELECT value FROM app_settings WHERE key=?", "host:"+address).Scan(&fp)
	if errors.Is(e, sql.ErrNoRows) {
		return "", nil
	}
	return fp, e
}

// HostPins lists the trusted host keys, by address.
func (s *Store) HostPins() ([]model.HostKey, error) {
	rows, e := s.db.Query("SELECT key, value FROM app_settings WHERE key LIKE 'host:%' ORDER BY key")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []model.HostKey
	for rows.Next() {
		var k model.HostKey
		if e = rows.Scan(&k.Address, &k.Fingerprint); e != nil {
			return nil, e
		}
		k.Address = strings.TrimPrefix(k.Address, "host:")
		out = append(out, k)
	}
	return out, rows.Err()
}

// DeleteHostPin forgets a host key: the next connection asks to trust it again.
func (s *Store) DeleteHostPin(address string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, e := s.db.Exec("DELETE FROM app_settings WHERE key=?", "host:"+address)
	return e
}

// UIState is a small value the UI keeps between runs ("" when unset).
func (s *Store) UIState(key string) (string, error) {
	var v string
	e := s.db.QueryRow("SELECT value FROM app_settings WHERE key=?", "ui:"+key).Scan(&v)
	if errors.Is(e, sql.ErrNoRows) {
		return "", nil
	}
	return v, e
}
func (s *Store) SaveUIState(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, e := s.db.Exec("INSERT INTO app_settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", "ui:"+key, value)
	return e
}

// DuplicateConnection copies encrypted credentials via the vault, never via UI plaintext.
func (s *Store) DuplicateConnection(id string) (model.Connection, error) {
	c, e := s.Connection(id)
	if e != nil {
		return c, e
	}
	c.ID = ID()
	c.Name += " 副本"
	c.Deleted = false
	sec := c.Secret
	if e = s.SaveConnection(c, &sec); e != nil {
		return c, e
	}
	c.Secret = ""
	return c, nil
}
func (s *Store) RenameKey(id, name string) error {
	k, e := s.Key(id)
	if e != nil {
		return e
	}
	k.Name = name
	return s.SaveKey(k)
}
func (s *Store) History() ([]string, error) {
	var b string
	e := s.db.QueryRow("SELECT value FROM app_settings WHERE key='command_history'").Scan(&b)
	if errors.Is(e, sql.ErrNoRows) {
		return []string{}, nil
	}
	if e != nil {
		return nil, e
	}
	var out []string
	e = json.Unmarshal([]byte(b), &out)
	return out, e
}
func (s *Store) SaveHistory(history []string) error {
	if len(history) > 100 {
		history = history[len(history)-100:]
	}
	b, e := json.Marshal(history)
	if e != nil {
		return e
	}
	_, e = s.db.Exec("INSERT INTO app_settings(key,value) VALUES('command_history',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", string(b))
	return e
}
