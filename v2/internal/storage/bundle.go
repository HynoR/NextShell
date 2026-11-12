package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"nextshell.dev/v2/internal/model"
	"strings"
)

type Bundle struct {
	Folders            []model.Folder
	Connections        []model.Connection
	Overwrite          map[string]bool
	ExpectedGeneration *int64
	Keys               []model.Key
	Proxies            []model.Proxy
	Commands           []model.Command
	Credentials        []model.Credential
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

// ApplyBundle atomically inserts or explicitly overwrites local file imports.
func (s *Store) ApplyBundle(b Bundle) error {
	s.mu.Lock()
	defer s.mu.Unlock()
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
	existingCredentials, e := s.Credentials()
	if e != nil {
		return e
	}
	folders := map[string]model.Folder{}
	keys, proxies, credentials := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, cr := range existingCredentials {
		credentials[cr.ID] = true
	}
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
		if f.ScopeID() != model.LocalScope {
			return errors.New("file imports only target local scope")
		}
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
			if f.ID != other.ID && f.ParentID == other.ParentID && f.Name == other.Name && f.ScopeID() == other.ScopeID() {
				return errors.New("import sibling folder collision")
			}
		}
	}
	for _, k := range b.Keys {
		if k.ScopeID() != model.LocalScope {
			return errors.New("file imports only target local scope")
		}
		if k.ID == "" || k.Name == "" || k.Private == "" {
			return errors.New("invalid imported key")
		}
		if keys[k.ID] {
			return errors.New("import key id collision")
		}
		keys[k.ID] = true
	}
	for _, p := range b.Proxies {
		if p.ScopeID() != model.LocalScope {
			return errors.New("file imports only target local scope")
		}
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
	for _, cr := range b.Credentials {
		if cr.ScopeID() != model.LocalScope {
			return errors.New("file imports only target local scope")
		}
		if cr.ID == "" || credentials[cr.ID] {
			return errors.New("import credential id collision")
		}
		if e = cr.Validate(); e != nil {
			return e
		}
		if cr.KeyID != "" && !keys[cr.KeyID] {
			return errors.New("missing imported key")
		}
		credentials[cr.ID] = true
	}
	for _, c := range b.Connections {
		if c.ScopeID() != model.LocalScope {
			return errors.New("file imports only target local scope")
		}
		if c.CredentialID != "" && !credentials[c.CredentialID] {
			return errors.New("missing imported credential")
		}
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
		if c.ScopeID() != model.LocalScope {
			return errors.New("file imports only target local scope")
		}
		if c.ID == "" || c.Name == "" || strings.TrimSpace(c.Text) == "" {
			return errors.New("invalid imported command")
		}
	}

	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if b.ExpectedGeneration != nil {
		var generation int64
		if e = tx.QueryRow("SELECT generation FROM profile_state WHERE singleton=1").Scan(&generation); e != nil {
			return e
		}
		if generation != *b.ExpectedGeneration {
			return errors.New("import preview is stale; preview again")
		}
	}
	for _, f := range b.Folders {
		if e = s.putTx(tx, "folder", f.ID, f, nil, false); e != nil {
			return e
		}
	}
	for _, k := range b.Keys {
		if k.ScopeID() != model.LocalScope {
			return errors.New("file imports only target local scope")
		}
		sec, _ := json.Marshal(struct{ Private, Passphrase string }{k.Private, k.Passphrase})
		v := string(sec)
		if e = s.putTx(tx, "key", k.ID, k, &v, false); e != nil {
			return e
		}
	}
	for _, p := range b.Proxies {
		if p.ScopeID() != model.LocalScope {
			return errors.New("file imports only target local scope")
		}
		v := p.Password
		if e = s.putTx(tx, "proxy", p.ID, p, &v, false); e != nil {
			return e
		}
	}
	for _, cr := range b.Credentials {
		v := cr.Secret
		if e = s.putTx(tx, "credential", cr.ID, cr, &v, false); e != nil {
			return e
		}
	}
	for _, c := range b.Connections {
		if c.ScopeID() != model.LocalScope {
			return errors.New("file imports only target local scope")
		}
		var n int
		var scope string
		e = tx.QueryRow("SELECT count(*),coalesce(scope,'local') FROM resources WHERE kind='connection' AND id=?", c.ID).Scan(&n, &scope)
		if e != nil {
			return e
		}
		if n > 0 && (!b.Overwrite[c.ID] || scope != model.LocalScope) {
			return errors.New("connection ID collision or non-local overwrite")
		}
		v := c.Secret
		if e = s.putTx(tx, "connection", c.ID, c, &v, false); e != nil {
			return e
		}
	}
	for _, c := range b.Commands {
		var n int
		if e = tx.QueryRow("SELECT count(*) FROM resources WHERE kind='command' AND id=?", c.ID).Scan(&n); e != nil {
			return e
		}
		if n > 0 {
			return errors.New("import command ID collision")
		}
		if e = s.putTx(tx, "command", c.ID, c, nil, false); e != nil {
			return e
		}
	}
	return tx.Commit()
}
