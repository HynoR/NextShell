package storage

import (
	"bytes"
	"encoding/json"
	"nextshell.dev/v2/internal/cloudv2"
	"nextshell.dev/v2/internal/model"
	"path/filepath"
	"testing"
)

func TestExplicitCopyClonesReferencesSecretsAndIndependentIDs(t *testing.T) {
	s, e := Open(":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	workspace, _ := makeWorkspace(t, s)
	f := model.Folder{ID: ID(), Name: "root"}
	if e = s.SaveFolder(f); e != nil {
		t.Fatal(e)
	}
	key := model.Key{ID: ID(), Name: "key", Private: "PRIVATE-TEST-MATERIAL", Passphrase: "key-pass"}
	if e = s.SaveKey(key); e != nil {
		t.Fatal(e)
	}
	proxy := model.Proxy{ID: ID(), Name: "proxy", Kind: "socks5", Host: "127.0.0.1", Port: 1080, User: "me"}
	password := "proxy-secret"
	if e = s.SaveProxy(proxy, &password); e != nil {
		t.Fatal(e)
	}
	c := conn(ID())
	c.FolderID = f.ID
	c.KeyID = key.ID
	c.ProxyID = proxy.ID
	secret := "connection-secret"
	if e = s.SaveConnection(c, &secret); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		if e = s.CopyLocalToWorkspace(workspace, []string{c.ID, c.ID}); e != nil {
			t.Fatal(e)
		}
	}
	connections, _ := s.Connections()
	if len(connections) != 3 {
		t.Fatal(len(connections))
	}
	n := 0
	for _, item := range connections {
		if item.ScopeID() == workspace {
			n++
			if item.ID == c.ID || item.FolderID == f.ID || item.KeyID == key.ID || item.ProxyID == proxy.ID {
				t.Fatal("source identity reused")
			}
			k, e := s.Key(item.KeyID)
			if e != nil || k.Private != key.Private || k.Passphrase != key.Passphrase {
				t.Fatal("key not copied", e)
			}
			p, e := s.Proxy(item.ProxyID)
			if e != nil || p.Password != password {
				t.Fatal("proxy not copied", e)
			}
			got, e := s.Connection(item.ID)
			if e != nil || got.Secret != secret {
				t.Fatal("secret not copied", e)
			}
		}
	}
	if n != 2 {
		t.Fatal(n)
	}
	snapshot, _, _, e := s.SyncSnapshot(workspace)
	if e != nil {
		t.Fatal(e)
	}
	if e = snapshot.Validate(); e != nil {
		t.Fatal("copy made unsyncable folder graph", e)
	}
	if e = s.CopyLocalToWorkspace(workspace, []string{ID()}); e == nil {
		t.Fatal("missing source accepted")
	}
	after, _ := s.Connections()
	if len(after) != 3 {
		t.Fatal("partial failed copy")
	}
}
func TestConflictReplacementCASAndEncryptedRecoveryAAD(t *testing.T) {
	s, e := Open(":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	workspace, _ := makeWorkspace(t, s)
	c := model.Command{Meta: model.Meta{Scope: workspace}, ID: ID(), Name: "original", Text: "PRIVATE-RECOVERY-COMMAND"}
	if e = s.SaveCommand(c); e != nil {
		t.Fatal(e)
	}
	snap, head, g, e := s.SyncSnapshot(workspace)
	if e != nil {
		t.Fatal(e)
	}
	c.Text = "changed"
	if e = s.SaveCommand(c); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ReplaceFromRemote(snap, ID(), head, g); e == nil {
		t.Fatal("stale replace accepted")
	}
	pos, _ := s.SyncPosition(workspace)
	id, e := s.ReplaceFromRemote(cloudv2.Snapshot{Version: 2, WorkspaceID: workspace, Name: "remote"}, ID(), pos.Head, pos.Generation)
	if e != nil {
		t.Fatal(e)
	}
	var env []byte
	if e = s.db.QueryRow("SELECT envelope FROM sync_recovery WHERE id=?", id).Scan(&env); e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(env, []byte("changed")) || json.Valid(env) && bytes.Contains(env, []byte(c.Name)) {
		t.Fatal("plaintext recovery")
	}
	recovery, e := s.Recovery(workspace, id)
	if e != nil || len(recovery.Records) != 1 {
		t.Fatal(e, recovery)
	}
	if _, e = s.Recovery(ID(), id); e == nil {
		t.Fatal("cross-workspace recovery accepted")
	}
	if _, e = s.db.Exec("UPDATE sync_recovery SET id=? WHERE id=?", ID(), id); e != nil {
		t.Fatal(e)
	}
	var renamed string
	s.db.QueryRow("SELECT id FROM sync_recovery").Scan(&renamed)
	if _, e = s.Recovery(workspace, renamed); e == nil {
		t.Fatal("AAD rename accepted")
	}
}
func TestMCPServiceTokenEncryptedPersistentAndRotates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.db")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	first, e := s.MCPToken(false)
	if e != nil {
		t.Fatal(e)
	}
	var env []byte
	if e = s.db.QueryRow("SELECT envelope FROM secrets WHERE id='service:mcp'").Scan(&env); e != nil || bytes.Contains(env, []byte(first)) {
		t.Fatal("token not encrypted", e)
	}
	s.Close()
	s, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	again, e := s.MCPToken(false)
	if e != nil || again != first {
		t.Fatal("token not persistent", e)
	}
	rotated, e := s.MCPToken(true)
	if e != nil || rotated == first {
		t.Fatal("not rotated", e)
	}
}
