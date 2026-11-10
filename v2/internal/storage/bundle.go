package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"nextshell.dev/v2/internal/model"
	"nextshell.dev/v2/internal/security"
	"strings"
)

var ErrAlreadyImported = errors.New("snapshot already imported")

type Bundle struct {
	Folders     []model.Folder
	Connections []model.Connection
	Keys        []model.Key
	Proxies     []model.Proxy
	Commands    []model.Command
	History     []string
	Archive     []byte
}

func (s *Store) Commands() ([]model.Command, error) { return list[model.Command](s, "command") }
func (s *Store) SaveCommand(c model.Command) error {
	if c.ID == "" || strings.TrimSpace(c.Name) == "" || strings.TrimSpace(c.Text) == "" || len(c.Text) > 1<<20 {
		return errors.New("invalid saved command")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.put("command", c.ID, c, nil)
}

// ApplyBundle commits an entire import or migration atomically. It never overwrites an existing id.
// The idempotence marker is committed in the same transaction as every row and credential.
func (s *Store) ApplyBundle(b Bundle, marker string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if marker != "" {
		if done, e := s.Imported(marker); e != nil {
			return e
		} else if done {
			return ErrAlreadyImported
		}
	}
	existingFolders, e := s.Folders()
	if e != nil {
		return e
	}
	existingKeys, e := s.Keys()
	if e != nil {
		return e
	}
	existingProxies, e := s.Proxies()
	if e != nil {
		return e
	}
	folders := map[string]model.Folder{}
	keys, proxies := map[string]bool{}, map[string]bool{}
	for _, f := range existingFolders {
		folders[f.ID] = f
	}
	for _, k := range existingKeys {
		keys[k.ID] = true
	}
	for _, p := range existingProxies {
		proxies[p.ID] = true
	}
	for i, f := range b.Folders {
		name, err := model.NormalizeFolderName(f.Name)
		if err != nil {
			return err
		}
		f.Name = name
		b.Folders[i] = f
		if f.ID == "" || strings.TrimSpace(f.Name) == "" {
			return errors.New("invalid imported folder")
		}
		if _, exists := folders[f.ID]; exists {
			return errors.New("import folder id collision")
		}
		folders[f.ID] = f
	}
	for _, f := range folders {
		seen := map[string]bool{}
		for id := f.ID; id != ""; {
			if seen[id] || len(seen) > 30 {
				return errors.New("import folder cycle/depth")
			}
			seen[id] = true
			parent, ok := folders[id]
			if !ok {
				return errors.New("missing import parent")
			}
			id = parent.ParentID
		}
		for _, other := range folders {
			if f.ID != other.ID && f.ParentID == other.ParentID && f.Name == other.Name {
				return errors.New("import sibling folder collision")
			}
		}
	}
	for _, k := range b.Keys {
		if k.ID == "" || k.Name == "" || k.Private == "" {
			return errors.New("invalid imported key")
		}
		if keys[k.ID] {
			return errors.New("import key id collision")
		}
		keys[k.ID] = true
	}
	for _, p := range b.Proxies {
		if p.ID == "" {
			return errors.New("missing proxy id")
		}
		if e = p.Validate(); e != nil {
			return e
		}
		if proxies[p.ID] {
			return errors.New("import proxy id collision")
		}
		proxies[p.ID] = true
	}
	for _, c := range b.Connections {
		if e = c.Validate(); e != nil {
			return fmt.Errorf("%s: %w", c.Name, e)
		}
		if c.ID == "" {
			return errors.New("missing connection id")
		}
		if c.FolderID != "" {
			if _, ok := folders[c.FolderID]; !ok {
				return errors.New("missing connection folder")
			}
		}
		if c.KeyID != "" && !keys[c.KeyID] {
			return errors.New("missing imported key")
		}
		if c.ProxyID != "" && !proxies[c.ProxyID] {
			return errors.New("missing imported proxy")
		}
	}
	for _, c := range b.Commands {
		if c.ID == "" || c.Name == "" || strings.TrimSpace(c.Text) == "" {
			return errors.New("invalid imported command")
		}
	}
	var mergedHistory []string
	if b.History != nil {
		existing, err := s.History()
		if err != nil {
			return err
		}
		history := append(append([]string{}, b.History...), existing...)
		seen := map[string]bool{}
		var reversed []string
		for i := len(history) - 1; i >= 0; i-- {
			if !seen[history[i]] {
				seen[history[i]] = true
				reversed = append(reversed, history[i])
			}
		}
		history = history[:0]
		for i := len(reversed) - 1; i >= 0; i-- {
			history = append(history, reversed[i])
		}
		mergedHistory = history
	}
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if marker != "" {
		var n int
		if e = tx.QueryRow("SELECT count(*) FROM app_settings WHERE key=?", "import:"+marker).Scan(&n); e != nil {
			return e
		}
		if n > 0 {
			return ErrAlreadyImported
		}
		if _, e = tx.Exec("INSERT INTO app_settings(key,value) VALUES(?,?)", "import:"+marker, "complete"); e != nil {
			return e
		}
	}
	put := func(kind, id string, v any, secret *string) error {
		data, e := json.Marshal(v)
		if e != nil {
			return e
		}
		if _, e = tx.Exec("INSERT INTO resources(kind,id,data) VALUES(?,?,?)", kind, id, data); e != nil {
			return e
		}
		if secret != nil {
			envelope, e := security.Seal([]byte(*secret), s.key, "nextshell-secret:"+kind+":"+id)
			if e != nil {
				return e
			}
			_, e = tx.Exec("INSERT INTO secrets(id,envelope) VALUES(?,?)", kind+":"+id, envelope)
			return e
		}
		return nil
	}
	for _, f := range b.Folders {
		if e = put("folder", f.ID, f, nil); e != nil {
			return e
		}
	}
	for _, k := range b.Keys {
		sec, _ := json.Marshal(struct{ Private, Passphrase string }{k.Private, k.Passphrase})
		v := string(sec)
		if e = put("key", k.ID, k, &v); e != nil {
			return e
		}
	}
	for _, p := range b.Proxies {
		v := p.Password
		if e = put("proxy", p.ID, p, &v); e != nil {
			return e
		}
	}
	for _, c := range b.Connections {
		v := c.Secret
		if e = put("connection", c.ID, c, &v); e != nil {
			return e
		}
	}
	for _, c := range b.Commands {
		if e = put("command", c.ID, c, nil); e != nil {
			return e
		}
	}
	if b.History != nil {
		history := mergedHistory
		if len(history) > 100 {
			history = history[len(history)-100:]
		}
		raw, _ := json.Marshal(history)
		if _, e = tx.Exec("INSERT INTO app_settings(key,value) VALUES('command_history',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", string(raw)); e != nil {
			return e
		}
	}
	if len(b.Archive) > 0 {
		envelope, e := security.Seal(b.Archive, s.key, "nextshell-legacy-archive:"+marker)
		if e != nil {
			return e
		}
		if _, e = tx.Exec("INSERT INTO secrets(id,envelope) VALUES(?,?)", "legacy-archive:"+marker, envelope); e != nil {
			return e
		}
	}
	return tx.Commit()
}
func (s *Store) Imported(marker string) (bool, error) {
	var n int
	e := s.db.QueryRow("SELECT count(*) FROM app_settings WHERE key=?", "import:"+marker).Scan(&n)
	return n > 0, e
}

// LegacyArchive returns the decrypted selected-table snapshot, not a byte-for-byte database backup.
// No UI or implicit file export exposes this potentially secret-bearing recovery material.
func (s *Store) LegacyArchive(marker string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var raw []byte
	if e := s.db.QueryRow("SELECT envelope FROM secrets WHERE id=?", "legacy-archive:"+marker).Scan(&raw); e != nil {
		return nil, e
	}
	return security.Unseal(raw, s.key, "nextshell-legacy-archive:"+marker)
}
