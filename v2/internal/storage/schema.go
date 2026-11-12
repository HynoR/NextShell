package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/pressly/goose/v3"
	"nextshell.dev/v2/internal/model"
	"nextshell.dev/v2/internal/security"
	"time"
)

// Goose owns ordering, version records and transaction boundaries. JSON backfill
// remains a Go migration because resource payloads are application-specific.
func (s *Store) upgradeSchema() error {
	provider, e := goose.NewProvider(goose.DialectSQLite3, s.db, nil, goose.WithDisableGlobalRegistry(true), goose.WithGoMigrations(
		goose.NewGoMigration(1, &goose.GoFunc{RunTx: func(ctx context.Context, tx *sql.Tx) error {
			_, e := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS app_settings(key TEXT PRIMARY KEY,value TEXT NOT NULL);CREATE TABLE IF NOT EXISTS resources(kind TEXT NOT NULL,id TEXT NOT NULL,data BLOB NOT NULL,PRIMARY KEY(kind,id));CREATE TABLE IF NOT EXISTS secrets(id TEXT PRIMARY KEY,envelope BLOB NOT NULL);`)
			return e
		}}, nil),
		goose.NewGoMigration(2, &goose.GoFunc{RunTx: s.schemaV3}, nil),
		goose.NewGoMigration(3, &goose.GoFunc{RunTx: func(ctx context.Context, tx *sql.Tx) error {
			_, e := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS sync_recovery(id TEXT PRIMARY KEY,workspace TEXT NOT NULL,envelope BLOB NOT NULL);`)
			return e
		}}, nil),
	))
	if e != nil {
		return e
	}
	_, e = provider.Up(context.Background())
	return e
}
func (s *Store) schemaV3(_ context.Context, tx *sql.Tx) error {
	rows, e := tx.Query("PRAGMA table_info(resources)")
	if e != nil {
		return e
	}
	have := map[string]bool{}
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var def any
		if e = rows.Scan(&cid, &name, &typ, &notnull, &def, &pk); e != nil {
			rows.Close()
			return e
		}
		have[name] = true
	}
	rows.Close()
	for _, col := range []struct{ name, typ string }{{"scope", "TEXT NOT NULL DEFAULT 'local'"}, {"created_at", "TEXT NOT NULL DEFAULT ''"}, {"updated_at", "TEXT NOT NULL DEFAULT ''"}, {"deleted_at", "TEXT NOT NULL DEFAULT ''"}, {"revision", "INTEGER NOT NULL DEFAULT 0"}} {
		if !have[col.name] {
			if _, e = tx.Exec("ALTER TABLE resources ADD COLUMN " + col.name + " " + col.typ); e != nil {
				return e
			}
		}
	}
	_, e = tx.Exec(`CREATE INDEX IF NOT EXISTS resources_scope ON resources(scope,kind);
CREATE TABLE IF NOT EXISTS profile_state(singleton INTEGER PRIMARY KEY CHECK(singleton=1),generation INTEGER NOT NULL); INSERT OR IGNORE INTO profile_state VALUES(1,0);
CREATE TABLE IF NOT EXISTS deletions(kind TEXT NOT NULL,id TEXT NOT NULL,scope TEXT NOT NULL,data BLOB NOT NULL,PRIMARY KEY(kind,id));
CREATE TABLE IF NOT EXISTS workspaces(id TEXT PRIMARY KEY,name TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS scope_state(scope TEXT PRIMARY KEY,generation INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS sync_state(workspace TEXT PRIMARY KEY,head TEXT NOT NULL,baseline INTEGER NOT NULL);
PRAGMA user_version=3;`)
	if e != nil {
		return e
	}
	rows, e = tx.Query("SELECT kind,id,data FROM resources WHERE created_at=''")
	if e != nil {
		return e
	}
	type row struct {
		kind, id string
		data     []byte
	}
	var old []row
	for rows.Next() {
		var r row
		if e = rows.Scan(&r.kind, &r.id, &r.data); e != nil {
			rows.Close()
			return e
		}
		old = append(old, r)
	}
	rows.Close()
	now := timestamp()
	for _, r := range old {
		var value map[string]any
		if e = json.Unmarshal(r.data, &value); e != nil {
			return e
		}
		if r.kind == "connection" {
			if _, present := value["keepAliveEnabled"]; !present {
				if interval, ok := value["keepalive"].(float64); ok {
					value["keepAliveEnabled"] = interval > 0
					if interval > 0 {
						value["keepAliveIntervalSec"] = interval
					}
				}
			}
			if _, ok := value["strictHostKeyChecking"]; !ok {
				value["strictHostKeyChecking"] = false
			}
		}
		m := model.Meta{Scope: model.LocalScope, CreatedAt: now, UpdatedAt: now, Revision: 1}
		raw, e := withMeta(value, m)
		if e != nil {
			return e
		}
		if _, e = tx.Exec("UPDATE resources SET data=?,scope=?,created_at=?,updated_at=?,revision=1 WHERE kind=? AND id=?", raw, m.Scope, m.CreatedAt, m.UpdatedAt, r.kind, r.id); e != nil {
			return e
		}
	}
	return nil
}
func timestamp() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000Z") }
func withMeta(v map[string]any, m model.Meta) ([]byte, error) {
	v["scope"] = m.Scope
	v["createdAt"] = m.CreatedAt
	v["updatedAt"] = m.UpdatedAt
	v["revision"] = m.Revision
	if m.DeletedAt != "" {
		v["deletedAt"] = m.DeletedAt
	} else {
		delete(v, "deletedAt")
	}
	return json.Marshal(v)
}
func content(v map[string]any) string {
	copy := map[string]any{}
	for k, x := range v {
		if k != "createdAt" && k != "updatedAt" && k != "revision" && k != "deletedAt" {
			copy[k] = x
		}
	}
	b, _ := json.Marshal(copy)
	return string(b)
}
func (s *Store) Generation() (int64, error) {
	var n int64
	e := s.db.QueryRow("SELECT generation FROM profile_state WHERE singleton=1").Scan(&n)
	return n, e
}
func (s *Store) putTx(tx *sql.Tx, kind, id string, value any, secret *string, remote bool) error {
	raw, e := json.Marshal(value)
	if e != nil {
		return e
	}
	var v map[string]any
	if e = json.Unmarshal(raw, &v); e != nil {
		return e
	}
	var incoming model.Meta
	json.Unmarshal(raw, &incoming)
	var old []byte
	var m model.Meta
	e = tx.QueryRow("SELECT data,scope,created_at,updated_at,deleted_at,revision FROM resources WHERE kind=? AND id=?", kind, id).Scan(&old, &m.Scope, &m.CreatedAt, &m.UpdatedAt, &m.DeletedAt, &m.Revision)
	exists := e == nil
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	if incoming.Scope == "" {
		incoming.Scope = m.Scope
	}
	if incoming.Scope == "" {
		incoming.Scope = model.LocalScope
	}
	if exists && incoming.Scope != m.Scope {
		return errors.New("scope move requires an explicit new copy")
	}
	if incoming.Scope != model.LocalScope {
		var n int
		if e = tx.QueryRow("SELECT count(*) FROM workspaces WHERE id=?", incoming.Scope).Scan(&n); e != nil {
			return e
		}
		if n != 1 {
			return errors.New("unknown workspace scope")
		}
	}
	v["scope"] = incoming.Scope
	if !remote && !exists {
		var deleted int
		if e = tx.QueryRow("SELECT count(*) FROM deletions WHERE kind=? AND id=?", kind, id).Scan(&deleted); e != nil {
			return e
		}
		if deleted > 0 {
			return errors.New("deleted ID cannot be reused; create a new UUID")
		}
	}
	if !remote {
		for field, target := range map[string]string{"folderId": "folder", "parentId": "folder", "keyId": "key", "proxyId": "proxy", "credentialId": "credential"} {
			if ref, ok := v[field].(string); ok && ref != "" {
				var scope string
				if e = tx.QueryRow("SELECT scope FROM resources WHERE kind=? AND id=?", target, ref).Scan(&scope); e != nil {
					return fmt.Errorf("missing %s reference: %w", field, e)
				}
				if scope != incoming.Scope {
					return errors.New("cross-scope reference forbidden")
				}
			}
		}
	}
	if exists && !remote {
		var before map[string]any
		json.Unmarshal(old, &before)
		sameSecret := secret == nil
		if secret != nil {
			var env []byte
			e = tx.QueryRow("SELECT envelope FROM secrets WHERE id=?", kind+":"+id).Scan(&env)
			if e == nil {
				plain, err := security.Unseal(env, s.key, "nextshell-secret:"+kind+":"+id)
				if err != nil {
					return err
				}
				sameSecret = string(plain) == *secret
			} else if !errors.Is(e, sql.ErrNoRows) {
				return e
			}
		}
		if content(before) == content(v) && sameSecret {
			return nil
		}
	}
	if remote {
		m = incoming
	} else {
		now := timestamp()
		if exists && now <= m.UpdatedAt {
			if prev, e := time.Parse(time.RFC3339Nano, m.UpdatedAt); e == nil {
				now = prev.Add(time.Millisecond).UTC().Format("2006-01-02T15:04:05.000Z")
			}
		}
		if !exists {
			m.CreatedAt = now
		}
		m.Scope = incoming.Scope
		m.UpdatedAt = now
		m.Revision++
		if deleted, _ := v["deleted"].(bool); deleted {
			if m.DeletedAt == "" {
				m.DeletedAt = now
			}
		} else {
			m.DeletedAt = ""
		}
	}
	raw, e = withMeta(v, m)
	if e != nil {
		return e
	}
	if _, e = tx.Exec(`INSERT INTO resources(kind,id,data,scope,created_at,updated_at,deleted_at,revision) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(kind,id) DO UPDATE SET data=excluded.data,scope=excluded.scope,created_at=excluded.created_at,updated_at=excluded.updated_at,deleted_at=excluded.deleted_at,revision=excluded.revision`, kind, id, raw, m.Scope, m.CreatedAt, m.UpdatedAt, m.DeletedAt, m.Revision); e != nil {
		return e
	}
	if secret != nil {
		env, e := security.Seal([]byte(*secret), s.key, "nextshell-secret:"+kind+":"+id)
		if e != nil {
			return e
		}
		if _, e = tx.Exec("INSERT INTO secrets(id,envelope) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET envelope=excluded.envelope", kind+":"+id, env); e != nil {
			return e
		}
	}
	if _, e = tx.Exec("DELETE FROM deletions WHERE kind=? AND id=?", kind, id); e != nil {
		return e
	}
	return bumpGeneration(tx, m.Scope)
}
func (s *Store) tombstoneTx(tx *sql.Tx, kind, id string) error {
	var m model.Meta
	e := tx.QueryRow("SELECT scope,created_at,updated_at,revision FROM resources WHERE kind=? AND id=?", kind, id).Scan(&m.Scope, &m.CreatedAt, &m.UpdatedAt, &m.Revision)
	if errors.Is(e, sql.ErrNoRows) {
		return nil
	}
	if e != nil {
		return e
	}
	now := timestamp()
	if now <= m.UpdatedAt {
		prev, e := time.Parse(time.RFC3339Nano, m.UpdatedAt)
		if e != nil {
			return e
		}
		now = prev.Add(time.Millisecond).UTC().Format("2006-01-02T15:04:05.000Z")
	}
	m.UpdatedAt = now
	m.DeletedAt = m.UpdatedAt
	m.Revision++
	b, _ := json.Marshal(m)
	if _, e = tx.Exec("INSERT INTO deletions(kind,id,scope,data) VALUES(?,?,?,?) ON CONFLICT(kind,id) DO UPDATE SET data=excluded.data", kind, id, m.Scope, b); e != nil {
		return e
	}
	return bumpGeneration(tx, m.Scope)
}

func bumpGeneration(tx *sql.Tx, scope string) error {
	if _, e := tx.Exec("UPDATE profile_state SET generation=generation+1 WHERE singleton=1"); e != nil {
		return e
	}
	_, e := tx.Exec("INSERT INTO scope_state(scope,generation) VALUES(?,1) ON CONFLICT(scope) DO UPDATE SET generation=generation+1", scope)
	return e
}
