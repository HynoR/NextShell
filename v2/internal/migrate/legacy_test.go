package migrate

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"nextshell.dev/v2/internal/model"
	"nextshell.dev/v2/internal/security"
	"nextshell.dev/v2/internal/storage"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const oldKey = "0101010101010101010101010101010101010101010101010101010101010101"

func fixture(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "nextshell.sqlite")
	db, e := sql.Open("sqlite", p)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := db.Exec(q, args...); e != nil {
			t.Fatal(e, q)
		}
	}
	exec(`CREATE TABLE connections(id TEXT PRIMARY KEY,name TEXT,host TEXT,port INTEGER,username TEXT,auth_type TEXT,credential_ref TEXT,private_key_path TEXT,private_key_ref TEXT,host_fingerprint TEXT,ssh_key_id TEXT,proxy_id TEXT,proxy_type TEXT,proxy_host TEXT,proxy_port INTEGER,proxy_username TEXT,proxy_credential_ref TEXT,group_path TEXT,folder_id TEXT,terminal_encoding TEXT,tags TEXT,notes TEXT,favorite INTEGER,keepalive_enabled INTEGER,keepalive_interval_sec INTEGER,monitor_session INTEGER,origin_scope_key TEXT)`)
	exec(`CREATE TABLE app_settings(key TEXT PRIMARY KEY,value_json TEXT,updated_at INTEGER)`)
	exec(`CREATE TABLE secret_store(id TEXT PRIMARY KEY,purpose TEXT,ciphertext_b64 TEXT,iv_b64 TEXT,tag_b64 TEXT,aad TEXT)`)
	exec(`CREATE TABLE ssh_keys(id TEXT PRIMARY KEY,name TEXT,key_content_ref TEXT,passphrase_ref TEXT,origin_scope_key TEXT)`)
	exec(`CREATE TABLE proxies(id TEXT PRIMARY KEY,name TEXT,proxy_type TEXT,host TEXT,port INTEGER,username TEXT,credential_ref TEXT,origin_scope_key TEXT)`)
	exec(`CREATE TABLE connection_folders(id TEXT PRIMARY KEY,scope_key TEXT,parent_id TEXT,name TEXT,sort_index INTEGER)`)
	exec(`CREATE TABLE saved_commands(id TEXT PRIMARY KEY,name TEXT,description TEXT,group_name TEXT,command TEXT,is_template INTEGER)`)
	exec(`CREATE TABLE command_history(command TEXT,use_count INTEGER,last_used_at INTEGER)`)
	exec(`CREATE TABLE schema_migrations(version INTEGER)`)
	exec(`CREATE TABLE recycle_bin_entries(id TEXT,payload_json TEXT)`)
	exec(`INSERT INTO schema_migrations VALUES(29)`)
	v, _ := json.Marshal(oldKey)
	exec(`INSERT INTO app_settings VALUES('device_key',?,0)`, string(v))
	exec(`INSERT INTO app_settings VALUES('theme','"dark"',0)`)
	key, _ := hex.DecodeString(oldKey)
	addSecret := func(id, plain string) {
		aad := "nextshell-" + id
		ct, tag, e := security.SealWithNonce([]byte(plain), key, bytes.Repeat([]byte{2}, 12), aad)
		if e != nil {
			t.Fatal(e)
		}
		b64 := base64.StdEncoding.EncodeToString
		exec(`INSERT INTO secret_store VALUES(?,?,?,?,?,?)`, id, "test", b64(ct), b64(bytes.Repeat([]byte{2}, 12)), b64(tag), aad)
	}
	addSecret("password", "password-from-v1")
	addSecret("proxy-password", "proxy-from-v1")
	_, private, _ := ed25519.GenerateKey(rand.Reader)
	der, _ := x509.MarshalPKCS8PrivateKey(private)
	addSecret("private", string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})))
	exec(`INSERT INTO ssh_keys VALUES('key','ops','secret://private','','local-default')`)
	exec(`INSERT INTO proxies VALUES('proxy','jump','socks5','127.0.0.1',1080,'pu','secret://proxy-password','local-default')`)
	exec(`INSERT INTO connection_folders VALUES('prod','local-default',NULL,'prod',0),('empty','local-default','prod','empty',0),('cloud','cloud:workspace',NULL,'cloud',0)`)
	exec(`INSERT INTO connections(id,name,host,port,username,auth_type,credential_ref,group_path,folder_id,terminal_encoding,tags,notes,favorite,keepalive_enabled,keepalive_interval_sec,monitor_session,origin_scope_key) VALUES('password','prod-a','10.0.0.1',22,'root','password','secret://password','/server/prod','prod','gb18030','["a","b"]','notes',1,1,25,1,'local-default')`)
	exec(`INSERT INTO connections(id,name,host,port,username,auth_type,credential_ref,group_path,ssh_key_id,proxy_id,origin_scope_key) VALUES('key','prod-key','10.0.0.2',2222,'admin','privateKey','','/workspace/slug/ops','key','proxy','local-default')`)
	exec(`INSERT INTO connections(id,name,host,port,username,auth_type,credential_ref,group_path,private_key_path) VALUES('path','old-path','10.0.0.3',22,'root','privateKey','','/import/legacy','/DO-NOT-READ/private-key')`)
	exec(`INSERT INTO connections(id,name,host,port,username,auth_type,group_path,origin_scope_key) VALUES('cloud','cloud','10.0.0.4',22,'root','agent','/server/cloud','cloud:workspace')`)
	exec(`INSERT INTO saved_commands VALUES('cmd','listing','desc','daily','ls -la',0)`)
	exec(`INSERT INTO command_history VALUES('pwd',2,1),('whoami',1,2)`)
	exec(`INSERT INTO recycle_bin_entries VALUES('trashed','{}')`)
	return p
}
func change(t *testing.T, path, q string) {
	t.Helper()
	db, e := sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if _, e = db.Exec(q); e != nil {
		t.Fatal(e)
	}
}
func target(t *testing.T) *storage.Store {
	t.Helper()
	s, e := storage.Open(filepath.Join(t.TempDir(), "v2.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func TestMigrationRealSchemaReadOnlyAtomicIdempotent(t *testing.T) {
	p := fixture(t)
	before, _ := os.ReadFile(p)
	s := target(t)
	r, e := Run(context.Background(), p, s, func(context.Context) (string, error) { t.Error("DB key should take precedence"); return "", nil })
	if e != nil {
		t.Fatal(e)
	}
	if r.Connections != 3 || r.Keys != 1 || r.Proxies != 1 || r.Commands != 1 || r.Folders != 5 || r.Already {
		t.Fatal(r)
	}
	for id, want := range map[string]string{"password": "password-from-v1", "key": ""} {
		c, e := s.Connection("v1-connection:" + id)
		if e != nil || c.Secret != want {
			t.Fatal(c, e)
		}
	}
	c, _ := s.Connection("v1-connection:password")
	if c.Encoding != "gb18030" || c.Keepalive != 25 || !c.Favorite || !c.Monitor || len(c.Tags) != 2 || c.FolderID != "v1-folder:prod" {
		t.Fatal(c)
	}
	c, _ = s.Connection("v1-connection:key")
	if c.Auth != "key" || c.KeyID != "v1-key:key" || c.ProxyID != "v1-proxy:proxy" {
		t.Fatal(c)
	}
	key, e := s.Key(c.KeyID)
	if e != nil || !strings.Contains(key.Private, "PRIVATE KEY") {
		t.Fatal(e)
	}
	proxy, e := s.Proxy(c.ProxyID)
	if e != nil || proxy.Password != "proxy-from-v1" {
		t.Fatal(proxy, e)
	}
	c, _ = s.Connection("v1-connection:path")
	if !c.NeedsKeyRebind || c.Auth != "keyboard" {
		t.Fatal(c)
	}
	commands, e := s.Commands()
	if e != nil || len(commands) != 1 || commands[0].Text != "ls -la" {
		t.Fatal(commands, e)
	}
	history, e := s.History()
	if e != nil || strings.Join(history, ",") != "pwd,whoami" {
		t.Fatal(history, e)
	}
	archive, err := s.LegacyArchive(r.Marker)
	if err != nil || !bytes.Contains(archive, []byte("recycle_bin_entries")) || !bytes.Contains(archive, []byte("theme")) {
		t.Fatal("selected legacy tables were not archived", err)
	}
	r, e = Run(context.Background(), p, s, nil)
	if e != nil || !r.Already {
		t.Fatal(r, e)
	}
	after, _ := os.ReadFile(p)
	if !bytes.Equal(before, after) {
		t.Fatal("source modified")
	}
	for _, ext := range []string{"-wal", "-shm"} {
		if _, e := os.Stat(p + ext); !os.IsNotExist(e) {
			t.Fatal("source sidecar created", ext)
		}
	}
}
func TestMigrationFallbackAndFailureRollback(t *testing.T) {
	cases := map[string]string{"missing-key": `DELETE FROM app_settings WHERE key='device_key'`, "wrong-key": `UPDATE app_settings SET value_json='"ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"' WHERE key='device_key'`, "bad-ref": `UPDATE connections SET credential_ref='secret://absent' WHERE id='password'`, "corrupt": `UPDATE secret_store SET ciphertext_b64='AAAA' WHERE id='password'`, "future": `UPDATE schema_migrations SET version=30`, "cycle": `UPDATE connection_folders SET parent_id='empty' WHERE id='prod'`, "invalid-proxy": `UPDATE proxies SET port=70000`, "unresolved-proxy": `UPDATE connections SET proxy_id='absent' WHERE id='key'`,
		"invalid-port": `UPDATE connections SET port=0 WHERE id='password'`}
	for name, q := range cases {
		t.Run(name, func(t *testing.T) {
			p := fixture(t)
			change(t, p, q)
			before, _ := os.ReadFile(p)
			s := target(t)
			r, e := Run(context.Background(), p, s, nil)
			if e == nil {
				t.Fatal("invalid migration accepted")
			}
			cs, _ := s.Connections()
			fs, _ := s.Folders()
			ks, _ := s.Keys()
			if len(cs)+len(fs)+len(ks) != 0 {
				t.Fatal("partial migration")
			}
			if yes, _ := s.Imported(r.Marker); yes {
				t.Fatal("failed marker committed")
			}
			after, _ := os.ReadFile(p)
			if !bytes.Equal(before, after) {
				t.Fatal("source modified on failure")
			}
		})
	}
	t.Run("fallback-success", func(t *testing.T) {
		p := fixture(t)
		change(t, p, cases["missing-key"])
		s := target(t)
		calls := 0
		r, e := Run(context.Background(), p, s, func(context.Context) (string, error) { calls++; return oldKey, nil })
		if e != nil || r.Connections != 3 || calls != 1 {
			t.Fatal(r, e, calls)
		}
	})
	t.Run("fallback-stub", func(t *testing.T) {
		p := fixture(t)
		change(t, p, cases["missing-key"])
		_, e := Run(context.Background(), p, target(t), func(context.Context) (string, error) {
			return "", errors.New("keytar NextShell/device-key UNVERIFIED stub")
		})
		if e == nil || !strings.Contains(e.Error(), "keychain fallback") {
			t.Fatal(e)
		}
	})
}
func TestMigrationTransactionIDCollisionAndForeignDB(t *testing.T) {
	p := fixture(t)
	s := target(t)
	if e := s.SaveConnection(model.Connection{ID: "v1-connection:password", Name: "do-not-overwrite", Host: "h", Port: 22, User: "u", Auth: "agent"}, nil); e != nil {
		t.Fatal(e)
	}
	r, e := Run(context.Background(), p, s, nil)
	if e == nil {
		t.Fatal("collision accepted")
	}
	folders, _ := s.Folders()
	keys, _ := s.Keys()
	cs, _ := s.Connections()
	if len(folders)+len(keys) != 0 || len(cs) != 1 || cs[0].Name != "do-not-overwrite" {
		t.Fatal("transaction did not roll back")
	}
	if yes, _ := s.Imported(r.Marker); yes {
		t.Fatal("marker survived rollback")
	}
	blank := filepath.Join(t.TempDir(), "foreign.db")
	db, _ := sql.Open("sqlite", blank)
	db.Exec(`CREATE TABLE other(x)`)
	db.Close()
	if _, e = Run(context.Background(), blank, s, nil); e == nil {
		t.Fatal("foreign DB accepted")
	}
}

func TestMigrationPreservesExistingHistoryAndFolderBackfillSpec(t *testing.T) {
	p := fixture(t)
	change(t, p, `INSERT INTO connections(id,name,host,port,username,auth_type,group_path) VALUES('same-zone','same-zone','host',22,'user','agent','/import/prod')`)
	s := target(t)
	s.SaveHistory([]string{"v2 recent"})
	if _, e := Run(context.Background(), p, s, nil); e != nil {
		t.Fatal(e)
	}
	h, _ := s.History()
	if strings.Join(h, ",") != "pwd,whoami,v2 recent" {
		t.Fatal("existing v2 history replaced", h)
	}
	a, _ := s.Connection("v1-connection:password")
	b, _ := s.Connection("v1-connection:same-zone")
	if a.FolderID != b.FolderID {
		t.Fatal("legacy server/import zones did not reuse the same folder")
	}
}
