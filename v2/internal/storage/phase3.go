package storage

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"nextshell.dev/v2/internal/cloudv2"
	"nextshell.dev/v2/internal/model"
	"nextshell.dev/v2/internal/security"
)

type SyncPosition struct {
	Head                 string
	Baseline, Generation int64
}

func (s *Store) SyncPosition(workspace string) (SyncPosition, error) {
	var p SyncPosition
	e := s.db.QueryRow("SELECT head,baseline,coalesce((SELECT generation FROM scope_state WHERE scope=?),0) FROM sync_state WHERE workspace=?", workspace, workspace).Scan(&p.Head, &p.Baseline, &p.Generation)
	return p, e
}

// ReplaceFromRemote is ONLY the explicitly confirmed conflict choice. Local work is saved
// in a device-encrypted recovery snapshot before replacement, in the same transaction.
func (s *Store) ReplaceFromRemote(snapshot cloudv2.Snapshot, head, expectedHead string, expectedGeneration int64) (string, error) {
	if e := snapshot.Validate(); e != nil {
		return "", e
	}
	if !cloudv2.ValidID(head) {
		return "", errors.New("invalid remote head")
	}
	backup, oldHead, g, e := s.SyncSnapshot(snapshot.WorkspaceID)
	if e != nil {
		return "", e
	}
	if g != expectedGeneration || oldHead != expectedHead {
		return "", errors.New("conflict preview is stale")
	}
	raw, e := json.Marshal(backup)
	if e != nil {
		return "", e
	}
	id := ID()
	env, e := security.Seal(raw, s.key, "nextshell-sync-recovery:"+snapshot.WorkspaceID+":"+id)
	if e != nil {
		return "", e
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, e := s.db.Begin()
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	var h string
	var gen int64
	e = tx.QueryRow("SELECT head,coalesce((SELECT generation FROM scope_state WHERE scope=?),0) FROM sync_state WHERE workspace=?", snapshot.WorkspaceID, snapshot.WorkspaceID).Scan(&h, &gen)
	if e != nil {
		return "", e
	}
	if h != expectedHead || gen != expectedGeneration {
		return "", errors.New("workspace changed during conflict resolution")
	}
	for _, r := range snapshot.Records {
		var scope string
		e = tx.QueryRow("SELECT scope FROM resources WHERE kind=? AND id=? UNION SELECT scope FROM deletions WHERE kind=? AND id=?", r.Kind, r.ID, r.Kind, r.ID).Scan(&scope)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return "", e
		}
		if e == nil && scope != snapshot.WorkspaceID {
			return "", errors.New("remote resource ID collides with another scope")
		}
	}
	if _, e = tx.Exec("INSERT INTO sync_recovery(id,workspace,envelope) VALUES(?,?,?)", id, snapshot.WorkspaceID, env); e != nil {
		return "", e
	}
	if _, e = tx.Exec("DELETE FROM secrets WHERE id IN (SELECT kind||':'||id FROM resources WHERE scope=?)", snapshot.WorkspaceID); e != nil {
		return "", e
	}
	if _, e = tx.Exec("DELETE FROM resources WHERE scope=?", snapshot.WorkspaceID); e != nil {
		return "", e
	}
	if _, e = tx.Exec("DELETE FROM deletions WHERE scope=?", snapshot.WorkspaceID); e != nil {
		return "", e
	}
	for _, r := range snapshot.Records {
		if r.Tombstone {
			raw, _ := json.Marshal(r.Meta)
			if _, e = tx.Exec("INSERT INTO deletions(kind,id,scope,data) VALUES(?,?,?,?)", r.Kind, r.ID, r.Meta.Scope, raw); e != nil {
				return "", e
			}
		} else if e = s.putTx(tx, r.Kind, r.ID, r.Data, r.Secret, true); e != nil {
			return "", e
		}
	}
	if e = bumpGeneration(tx, snapshot.WorkspaceID); e != nil {
		return "", e
	}
	if _, e = tx.Exec("UPDATE workspaces SET name=? WHERE id=?", snapshot.Name, snapshot.WorkspaceID); e != nil {
		return "", e
	}
	if _, e = tx.Exec("UPDATE sync_state SET head=?,baseline=(SELECT generation FROM scope_state WHERE scope=?) WHERE workspace=?", head, snapshot.WorkspaceID, snapshot.WorkspaceID); e != nil {
		return "", e
	}
	return id, tx.Commit()
}
func (s *Store) Recovery(workspace, id string) (cloudv2.Snapshot, error) {
	var snapshot cloudv2.Snapshot
	var envelope []byte
	e := s.db.QueryRow("SELECT envelope FROM sync_recovery WHERE workspace=? AND id=?", workspace, id).Scan(&envelope)
	if e != nil {
		return snapshot, e
	}
	raw, e := security.Unseal(envelope, s.key, "nextshell-sync-recovery:"+workspace+":"+id)
	if e != nil {
		return snapshot, e
	}
	e = json.Unmarshal(raw, &snapshot)
	return snapshot, e
}

// CopyLocalToWorkspace creates independent identities and copies only explicitly selected connections.
func (s *Store) CopyLocalToWorkspace(workspace string, ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var exists int
	if e := s.db.QueryRow("SELECT count(*) FROM workspaces WHERE id=?", workspace).Scan(&exists); e != nil {
		return e
	}
	if exists != 1 || len(ids) == 0 || len(ids) > 500 {
		return errors.New("choose 1..500 local connections and an existing workspace")
	}
	var connections []model.Connection
	var folders []model.Folder
	var keys []model.Key
	var proxies []model.Proxy
	var credentials []model.Credential
	existingFolders, e := s.Folders()
	if e != nil {
		return e
	}
	names := map[string]bool{}
	for _, f := range existingFolders {
		if f.ScopeID() == workspace {
			names[f.ParentID+"\x00"+f.Name] = true
		}
	}
	folderIDs, keyIDs, proxyIDs, credentialIDs := map[string]string{}, map[string]string{}, map[string]string{}, map[string]string{}
	copyKey := func(old string) (string, error) {
		if keyIDs[old] == "" {
			k, e := s.keyUnlocked(old)
			if e != nil {
				return "", e
			}
			if k.ScopeID() != model.LocalScope {
				return "", errors.New("not a local key")
			}
			k.ID = ID()
			k.Meta = model.Meta{Scope: workspace}
			keyIDs[old] = k.ID
			keys = append(keys, k)
		}
		return keyIDs[old], nil
	}
	var folder func(string, int) (string, error)
	folder = func(old string, depth int) (string, error) {
		if old == "" {
			return "", nil
		}
		if depth > 30 {
			return "", errors.New("folder depth")
		}
		if id := folderIDs[old]; id != "" {
			return id, nil
		}
		f, e := get[model.Folder](s, "folder", old)
		if e != nil {
			return "", e
		}
		if f.ScopeID() != model.LocalScope {
			return "", errors.New("not a local folder")
		}
		parent, e := folder(f.ParentID, depth+1)
		if e != nil {
			return "", e
		}
		f.ID = ID()
		f.ParentID = parent
		base := f.Name
		for n := 2; names[parent+"\x00"+f.Name]; n++ {
			f.Name = fmt.Sprintf("%s (%d)", base, n)
		}
		if _, e = model.NormalizeFolderName(f.Name); e != nil {
			return "", e
		}
		names[parent+"\x00"+f.Name] = true
		f.Meta = model.Meta{Scope: workspace}
		folderIDs[old] = f.ID
		folders = append(folders, f)
		return f.ID, nil
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		c, e := s.connectionUnlocked(id)
		if e != nil {
			return e
		}
		if c.ScopeID() != model.LocalScope || c.Deleted {
			return errors.New("only active local connections may be copied")
		}
		c.FolderID, e = folder(c.FolderID, 0)
		if e != nil {
			return e
		}
		if c.KeyID != "" {
			if c.KeyID, e = copyKey(c.KeyID); e != nil {
				return e
			}
		}
		if old := c.CredentialID; old != "" {
			if credentialIDs[old] == "" {
				cr, e := s.credentialUnlocked(old)
				if e != nil {
					return e
				}
				if cr.ScopeID() != model.LocalScope {
					return errors.New("not a local credential")
				}
				if cr.KeyID != "" {
					if cr.KeyID, e = copyKey(cr.KeyID); e != nil {
						return e
					}
				}
				cr.ID = ID()
				cr.Meta = model.Meta{Scope: workspace}
				credentialIDs[old] = cr.ID
				credentials = append(credentials, cr)
			}
			c.CredentialID = credentialIDs[old]
		}
		if c.ProxyID != "" {
			old := c.ProxyID
			if proxyIDs[old] == "" {
				p, e := s.proxyUnlocked(old)
				if e != nil {
					return e
				}
				if p.ScopeID() != model.LocalScope {
					return errors.New("not a local proxy")
				}
				p.ID = ID()
				p.Meta = model.Meta{Scope: workspace}
				proxyIDs[old] = p.ID
				proxies = append(proxies, p)
			}
			c.ProxyID = proxyIDs[old]
		}
		c.ID = ID()
		c.Meta = model.Meta{Scope: workspace}
		c.Name += "（云副本）"
		connections = append(connections, c)
	}
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for _, f := range folders {
		if e = s.putTx(tx, "folder", f.ID, f, nil, false); e != nil {
			return e
		}
	}
	for _, k := range keys {
		raw, _ := json.Marshal(struct{ Private, Passphrase string }{k.Private, k.Passphrase})
		sec := string(raw)
		if e = s.putTx(tx, "key", k.ID, k, &sec, false); e != nil {
			return e
		}
	}
	for _, p := range proxies {
		sec := p.Password
		if e = s.putTx(tx, "proxy", p.ID, p, &sec, false); e != nil {
			return e
		}
	}
	for _, cr := range credentials {
		sec := cr.Secret
		if e = s.putTx(tx, "credential", cr.ID, cr, &sec, false); e != nil {
			return e
		}
	}
	for _, c := range connections {
		sec := c.Secret
		if e = s.putTx(tx, "connection", c.ID, c, &sec, false); e != nil {
			return e
		}
	}
	return tx.Commit()
}
