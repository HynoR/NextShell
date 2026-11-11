package storage

import (
	"database/sql"
	"encoding/json"
	"errors"
	"nextshell.dev/v2/internal/cloudv2"
	"nextshell.dev/v2/internal/model"
	"nextshell.dev/v2/internal/security"
)

type Workspace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type WorkspaceSecrets struct {
	Wrapped   cloudv2.WrappedKey `json:"wrapped"`
	DataKey   []byte             `json:"dataKey"`
	AuthToken string             `json:"authToken"`
}

func (s *Store) SaveWorkspace(w Workspace, keys WorkspaceSecrets) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !cloudv2.ValidID(w.ID) || w.Name == "" || len(w.Name) > 1024 || keys.Wrapped.WorkspaceID != w.ID || len(keys.DataKey) != 32 {
		return errors.New("invalid workspace")
	}
	if _, e := cloudv2.AuthVerifier(keys.AuthToken); e != nil {
		return e
	}
	if e := keys.Wrapped.Validate(); e != nil {
		return e
	}
	raw, e := json.Marshal(keys)
	if e != nil {
		return e
	}
	env, e := security.Seal(raw, s.key, "nextshell-workspace:"+w.ID)
	if e != nil {
		return e
	}
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var old []byte
	e = tx.QueryRow("SELECT envelope FROM secrets WHERE id=?", "workspace:"+w.ID).Scan(&old)
	if e == nil {
		plain, e := security.Unseal(old, s.key, "nextshell-workspace:"+w.ID)
		if e != nil {
			return e
		}
		var prior WorkspaceSecrets
		if e = json.Unmarshal(plain, &prior); e != nil {
			return e
		}
		if string(prior.DataKey) != string(keys.DataKey) || prior.Wrapped.KeyID != keys.Wrapped.KeyID {
			return errors.New("DEK rotation needs a future explicit rotation protocol")
		}
	} else if !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	var oldName string
	nameErr := tx.QueryRow("SELECT name FROM workspaces WHERE id=?", w.ID).Scan(&oldName)
	if nameErr == nil && oldName != w.Name {
		if e = bumpGeneration(tx, w.ID); e != nil {
			return e
		}
	} else if nameErr != nil && !errors.Is(nameErr, sql.ErrNoRows) {
		return nameErr
	}
	if _, e = tx.Exec("INSERT INTO workspaces(id,name) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name", w.ID, w.Name); e != nil {
		return e
	}
	if _, e = tx.Exec("INSERT INTO secrets(id,envelope) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET envelope=excluded.envelope", "workspace:"+w.ID, env); e != nil {
		return e
	}
	if _, e = tx.Exec("INSERT OR IGNORE INTO sync_state(workspace,head,baseline) VALUES(?,'',0)", w.ID); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) Workspaces() ([]Workspace, error) {
	rows, e := s.db.Query("SELECT id,name FROM workspaces ORDER BY id")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var result []Workspace
	for rows.Next() {
		var w Workspace
		if e = rows.Scan(&w.ID, &w.Name); e != nil {
			return nil, e
		}
		result = append(result, w)
	}
	return result, rows.Err()
}
func (s *Store) WorkspaceSecrets(id string) (WorkspaceSecrets, error) {
	var out WorkspaceSecrets
	var env []byte
	e := s.db.QueryRow("SELECT envelope FROM secrets WHERE id=?", "workspace:"+id).Scan(&env)
	if e != nil {
		return out, e
	}
	raw, e := security.Unseal(env, s.key, "nextshell-workspace:"+id)
	if e != nil {
		return out, e
	}
	e = json.Unmarshal(raw, &out)
	return out, e
}
func (s *Store) SyncSnapshot(id string) (cloudv2.Snapshot, string, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return cloudv2.Snapshot{}, "", 0, err
	}
	defer tx.Rollback()
	snapshot := cloudv2.Snapshot{Version: cloudv2.Version, WorkspaceID: id}
	var head string
	var generation int64
	if e := tx.QueryRow("SELECT name FROM workspaces WHERE id=?", id).Scan(&snapshot.Name); e != nil {
		return snapshot, head, generation, e
	}
	if e := tx.QueryRow("SELECT head FROM sync_state WHERE workspace=?", id).Scan(&head); e != nil {
		return snapshot, head, generation, e
	}
	if e := tx.QueryRow("SELECT coalesce((SELECT generation FROM scope_state WHERE scope=?),0)", id).Scan(&generation); e != nil {
		return snapshot, head, generation, e
	}
	rows, e := tx.Query("SELECT kind,id,data FROM resources WHERE scope=? ORDER BY kind,id", id)
	if e != nil {
		return snapshot, head, generation, e
	}
	for rows.Next() {
		var r cloudv2.Record
		if e = rows.Scan(&r.Kind, &r.ID, &r.Data); e != nil {
			rows.Close()
			return snapshot, head, generation, e
		}
		if e = json.Unmarshal(r.Data, &r.Meta); e != nil {
			rows.Close()
			return snapshot, head, generation, e
		}
		snapshot.Records = append(snapshot.Records, r)
	}
	if e = rows.Err(); e != nil {
		rows.Close()
		return snapshot, head, generation, e
	}
	rows.Close()
	for i := range snapshot.Records {
		r := &snapshot.Records[i]
		if r.Kind == "connection" || r.Kind == "key" || r.Kind == "proxy" {
			var envelope []byte
			e := tx.QueryRow("SELECT envelope FROM secrets WHERE id=?", r.Kind+":"+r.ID).Scan(&envelope)
			if e != nil {
				return snapshot, head, generation, e
			}
			plain, e := security.Unseal(envelope, s.key, "nextshell-secret:"+r.Kind+":"+r.ID)
			v := string(plain)
			if e != nil {
				return snapshot, head, generation, e
			}
			r.Secret = &v
		}
	}
	rows, e = tx.Query("SELECT kind,id,data FROM deletions WHERE scope=? ORDER BY kind,id", id)
	if e != nil {
		return snapshot, head, generation, e
	}
	for rows.Next() {
		r := cloudv2.Record{Tombstone: true}
		var raw []byte
		if e = rows.Scan(&r.Kind, &r.ID, &raw); e != nil {
			rows.Close()
			return snapshot, head, generation, e
		}
		if e = json.Unmarshal(raw, &r.Meta); e != nil {
			rows.Close()
			return snapshot, head, generation, e
		}
		snapshot.Records = append(snapshot.Records, r)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return snapshot, head, generation, e
	}
	return snapshot, head, generation, snapshot.Validate()
}
func (s *Store) AcceptPublished(workspace, expectedHead, newHead string, generation int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !cloudv2.ValidID(newHead) || newHead == expectedHead {
		return errors.New("invalid head")
	}
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var current int64
	e = tx.QueryRow("SELECT coalesce((SELECT generation FROM scope_state WHERE scope=?),0)", workspace).Scan(&current)
	if e != nil {
		return e
	}
	if current != generation {
		return errors.New("workspace edited while publishing; reconcile before advancing head")
	}
	res, e := tx.Exec("UPDATE sync_state SET head=?,baseline=? WHERE workspace=? AND head=?", newHead, generation, workspace, expectedHead)
	if e != nil {
		return e
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return errors.New("trusted head changed")
	}
	return tx.Commit()
}

// ApplySyncCommit never downloads or trusts arbitrary JSON; authenticate/decrypt BEFORE atomic writes.
func (s *Store) ApplySyncCommit(c cloudv2.Commit) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys, e := s.WorkspaceSecrets(c.WorkspaceID)
	if e != nil {
		return e
	}
	var head string
	var baseline, current int64
	if e = s.db.QueryRow("SELECT head,baseline FROM sync_state WHERE workspace=?", c.WorkspaceID).Scan(&head, &baseline); e != nil {
		return e
	}
	if e = s.db.QueryRow("SELECT coalesce((SELECT generation FROM scope_state WHERE scope=?),0)", c.WorkspaceID).Scan(&current); e != nil {
		return e
	}
	if baseline != current {
		return errors.New("unsynced local edits; refusing implicit overwrite")
	}
	snapshot, e := cloudv2.Open(c, keys.DataKey, c.WorkspaceID, keys.Wrapped.KeyID, head)
	if e != nil {
		return e
	}
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var checkedHead string
	var checkedBaseline, checkedGeneration int64
	if e = tx.QueryRow("SELECT head,baseline,coalesce((SELECT generation FROM scope_state WHERE scope=?),0) FROM sync_state WHERE workspace=?", c.WorkspaceID, c.WorkspaceID).Scan(&checkedHead, &checkedBaseline, &checkedGeneration); e != nil {
		return e
	}
	if checkedHead != head || checkedBaseline != baseline || checkedGeneration != baseline {
		return errors.New("workspace changed before atomic apply")
	}

	incoming := map[string]cloudv2.Record{}
	for _, r := range snapshot.Records {
		incoming[r.Kind+":"+r.ID] = r
	}
	rows, e := tx.Query("SELECT kind,id FROM resources WHERE scope=? UNION SELECT kind,id FROM deletions WHERE scope=?", c.WorkspaceID, c.WorkspaceID)
	if e != nil {
		return e
	}
	for rows.Next() {
		var kind, id string
		if e = rows.Scan(&kind, &id); e != nil {
			rows.Close()
			return e
		}
		if _, ok := incoming[kind+":"+id]; !ok {
			rows.Close()
			return errors.New("full snapshot omitted known resource/tombstone")
		}
	}
	rows.Close()
	for _, r := range snapshot.Records {
		var oldMeta model.Meta
		var oldData []byte
		e = tx.QueryRow("SELECT scope,created_at,updated_at,deleted_at,revision,data FROM resources WHERE kind=? AND id=?", r.Kind, r.ID).Scan(&oldMeta.Scope, &oldMeta.CreatedAt, &oldMeta.UpdatedAt, &oldMeta.DeletedAt, &oldMeta.Revision, &oldData)
		exists := e == nil
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if !exists {
			var raw []byte
			e = tx.QueryRow("SELECT data FROM deletions WHERE kind=? AND id=?", r.Kind, r.ID).Scan(&raw)
			if e == nil {
				json.Unmarshal(raw, &oldMeta)
				exists = true
			} else if !errors.Is(e, sql.ErrNoRows) {
				return e
			}
		}
		if exists {
			if oldMeta.Scope != c.WorkspaceID || r.Meta.Revision < oldMeta.Revision || r.Meta.CreatedAt != oldMeta.CreatedAt || r.Meta.UpdatedAt < oldMeta.UpdatedAt {
				return errors.New("resource scope/revision/timestamp rollback")
			}
			if r.Meta.Revision == oldMeta.Revision {
				if oldMeta != r.Meta {
					return errors.New("metadata changed without revision")
				}
				if r.Tombstone {
					if len(oldData) != 0 {
						return errors.New("deletion without revision")
					}
				} else {
					var a, b map[string]any
					json.Unmarshal(oldData, &a)
					json.Unmarshal(r.Data, &b)
					if content(a) != content(b) {
						return errors.New("content changed without revision")
					}
					if r.Secret != nil {
						var env []byte
						if e = tx.QueryRow("SELECT envelope FROM secrets WHERE id=?", r.Kind+":"+r.ID).Scan(&env); e != nil {
							return e
						}
						plain, e := security.Unseal(env, s.key, "nextshell-secret:"+r.Kind+":"+r.ID)
						if e != nil {
							return e
						}
						if string(plain) != *r.Secret {
							return errors.New("secret changed without revision")
						}
					}
				}
			}
		}
		if r.Tombstone {
			raw, _ := json.Marshal(r.Meta)
			if _, e = tx.Exec("DELETE FROM resources WHERE kind=? AND id=?", r.Kind, r.ID); e != nil {
				return e
			}
			if _, e = tx.Exec("DELETE FROM secrets WHERE id=?", r.Kind+":"+r.ID); e != nil {
				return e
			}
			if _, e = tx.Exec("INSERT INTO deletions(kind,id,scope,data) VALUES(?,?,?,?) ON CONFLICT(kind,id) DO UPDATE SET data=excluded.data", r.Kind, r.ID, r.Meta.Scope, raw); e != nil {
				return e
			}
			if e = bumpGeneration(tx, r.Meta.Scope); e != nil {
				return e
			}
		} else {
			if e = s.putTx(tx, r.Kind, r.ID, json.RawMessage(r.Data), r.Secret, true); e != nil {
				return e
			}
		}
	}
	if _, e = tx.Exec("UPDATE workspaces SET name=? WHERE id=?", snapshot.Name, c.WorkspaceID); e != nil {
		return e
	}
	if e = tx.QueryRow("SELECT coalesce((SELECT generation FROM scope_state WHERE scope=?),0)", c.WorkspaceID).Scan(&current); e != nil {
		return e
	}
	res, e := tx.Exec("UPDATE sync_state SET head=?,baseline=? WHERE workspace=? AND head=? AND baseline=?", c.ID, current, c.WorkspaceID, head, baseline)
	if e != nil {
		return e
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return errors.New("head changed")
	}
	return tx.Commit()
}
