package storage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"nextshell.dev/v2/internal/cloudv2"
	"nextshell.dev/v2/internal/model"
	"path/filepath"
	"strings"
	"testing"
)

func makeWorkspace(t *testing.T, s *Store) (string, WorkspaceSecrets) {
	t.Helper()
	id := cloudv2.NewID()
	w, key, e := cloudv2.NewKey("private encryption phrase", id)
	if e != nil {
		t.Fatal(e)
	}
	token, e := cloudv2.NewAuthToken()
	if e != nil {
		t.Fatal(e)
	}
	keys := WorkspaceSecrets{Wrapped: w, DataKey: key, AuthToken: token}
	if e = s.SaveWorkspace(Workspace{ID: id, Name: "workspace private name"}, keys); e != nil {
		t.Fatal(e)
	}
	return id, keys
}
func TestWorkspaceVaultRedactionAndIndependentRotation(t *testing.T) {
	s, e := Open(":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	id, keys := makeWorkspace(t, s)
	list, e := s.Workspaces()
	if e != nil || len(list) != 1 {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(list)
	if bytes.Contains(raw, keys.DataKey) || strings.Contains(string(raw), keys.AuthToken) {
		t.Fatal("workspace secrets leaked in listing")
	}
	var envelope []byte
	s.db.QueryRow("SELECT envelope FROM secrets WHERE id=?", "workspace:"+id).Scan(&envelope)
	if bytes.Contains(envelope, keys.DataKey) || bytes.Contains(envelope, []byte(keys.AuthToken)) {
		t.Fatal("unencrypted workspace vault")
	}
	keys.AuthToken, _ = cloudv2.NewAuthToken()
	keys.Wrapped, e = cloudv2.WrapKey("changed encryption phrase", id, keys.Wrapped.KeyID, keys.DataKey)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.SaveWorkspace(list[0], keys); e != nil {
		t.Fatal(e)
	}
	got, e := s.WorkspaceSecrets(id)
	if e != nil || got.AuthToken != keys.AuthToken || !bytes.Equal(got.DataKey, keys.DataKey) {
		t.Fatal(e)
	}
	g, _ := s.Generation()
	if g != 0 {
		t.Fatal("rewrap created resource edits")
	}
	list[0].Name = "renamed"
	if e = s.SaveWorkspace(list[0], keys); e != nil {
		t.Fatal(e)
	}
	g, _ = s.Generation()
	if g != 1 {
		t.Fatal("workspace rename not dirty")
	}
}
func TestOfflineSyncRoundTripAllResourcesSecretsAndTombstones(t *testing.T) {
	a, _ := Open(":memory:")
	defer a.Close()
	b, _ := Open(":memory:")
	defer b.Close()
	id, keys := makeWorkspace(t, a)
	if e := b.SaveWorkspace(Workspace{ID: id, Name: "new device"}, keys); e != nil {
		t.Fatal(e)
	}
	m := model.Meta{Scope: id}
	folder := model.Folder{Meta: m, ID: ID(), Name: "secret folder", SortIndex: 4}
	key := model.Key{Meta: m, ID: ID(), Name: "private key", Private: "PRIVATE_KEY", Passphrase: "PRIVATE_PASSPHRASE"}
	proxy := model.Proxy{Meta: m, ID: ID(), Name: "private proxy", Kind: "socks5", Host: "localhost", Port: 1080, Password: "PROXY_SECRET"}
	if e := a.SaveFolder(folder); e != nil {
		t.Fatal(e)
	}
	if e := a.SaveKey(key); e != nil {
		t.Fatal(e)
	}
	if e := a.SaveProxy(proxy, &proxy.Password); e != nil {
		t.Fatal(e)
	}
	c := conn(ID())
	c.Meta = m
	c.FolderID = folder.ID
	c.KeyID = key.ID
	c.ProxyID = proxy.ID
	c.StrictHostKeyChecking = true
	c.Fingerprint = "SHA256:LOCAL_TRUST"
	secret := "CONNECTION_SECRET"
	if e := a.SaveConnection(c, &secret); e != nil {
		t.Fatal(e)
	}
	no := false
	command := model.Command{Meta: m, ID: ID(), Name: "private command", Text: "echo COMMAND_SECRET", AppendCR: &no}
	if e := a.SaveCommand(command); e != nil {
		t.Fatal(e)
	}
	snapshot, head, g, e := a.SyncSnapshot(id)
	if e != nil || len(snapshot.Records) != 5 {
		t.Fatal(e, len(snapshot.Records))
	}
	commit, e := cloudv2.Seal(snapshot, keys.Wrapped.KeyID, head, keys.DataKey)
	if e != nil {
		t.Fatal(e)
	}
	wire, _ := json.Marshal(commit)
	for _, secret := range []string{"PRIVATE_KEY", "PROXY_SECRET", "CONNECTION_SECRET", "COMMAND_SECRET", "secret folder", c.Fingerprint} {
		if bytes.Contains(wire, []byte(secret)) {
			t.Fatal("plaintext leaked", secret)
		}
	}
	if e = b.ApplySyncCommit(commit); e != nil {
		t.Fatal(e)
	}
	if e = a.AcceptPublished(id, head, commit.ID, g); e != nil {
		t.Fatal(e)
	}
	got, e := b.Connection(c.ID)
	found := false
	for _, r := range snapshot.Records {
		if r.ID == c.ID && r.Meta == got.Meta {
			found = true
		}
	}
	if e != nil || got.Secret != secret || !found {
		t.Fatal(got, e)
	}
	gotkey, e := b.Key(key.ID)
	if e != nil || gotkey.Private != key.Private || gotkey.Passphrase != key.Passphrase {
		t.Fatal(e)
	}
	gotproxy, e := b.Proxy(proxy.ID)
	if e != nil || gotproxy.Password != proxy.Password {
		t.Fatal(e)
	}
	commands, _ := b.Commands()
	if len(commands) != 1 || commands[0].AppendCR == nil || *commands[0].AppendCR {
		t.Fatal(commands)
	}
	if e = b.ApplySyncCommit(commit); e == nil {
		t.Fatal("replayed commit accepted")
	}
	for _, r := range []struct{ k, id string }{{"connection", c.ID}, {"key", key.ID}, {"proxy", proxy.ID}, {"folder", folder.ID}} {
		if e = a.Delete(r.k, r.id); e != nil {
			t.Fatal(e)
		}
	}
	next, head, g, e := a.SyncSnapshot(id)
	if e != nil {
		t.Fatal(e)
	}
	tombstones := 0
	for _, r := range next.Records {
		if r.Tombstone {
			tombstones++
		}
	}
	if tombstones != 4 {
		t.Fatal(tombstones)
	}
	child, e := cloudv2.Seal(next, keys.Wrapped.KeyID, head, keys.DataKey)
	if e != nil {
		t.Fatal(e)
	}
	if e = b.ApplySyncCommit(child); e != nil {
		t.Fatal(e)
	}
	if _, e = b.Connection(c.ID); e == nil {
		t.Fatal("deleted connection retained")
	}
	result, head, _, e := b.SyncSnapshot(id)
	if e != nil || head != child.ID || len(result.Records) != 5 {
		t.Fatal(e)
	}
	if e = a.AcceptPublished(id, commit.ID, child.ID, g); e != nil {
		t.Fatal(e)
	}
}
func TestSyncRejectsLocalDirtyAndTamperWithoutAdvancingHead(t *testing.T) {
	a, _ := Open(":memory:")
	defer a.Close()
	id, keys := makeWorkspace(t, a)
	snapshot, head, _, e := a.SyncSnapshot(id)
	if e != nil {
		t.Fatal(e)
	}
	c, e := cloudv2.Seal(snapshot, keys.Wrapped.KeyID, head, keys.DataKey)
	if e != nil {
		t.Fatal(e)
	}
	bad := c
	bad.Ciphertext = append([]byte{}, c.Ciphertext...)
	bad.Ciphertext[0] ^= 1
	if e = a.ApplySyncCommit(bad); e == nil {
		t.Fatal("tamper accepted")
	}
	_, unchanged, _, _ := a.SyncSnapshot(id)
	if unchanged != head {
		t.Fatal("failed decryption advanced head")
	}
	command := model.Command{Meta: model.Meta{Scope: id}, ID: ID(), Name: "local edit", Text: "echo dirty"}
	if e = a.SaveCommand(command); e != nil {
		t.Fatal(e)
	}
	if e = a.ApplySyncCommit(c); e == nil {
		t.Fatal("dirty local edits overwritten")
	}
	got, _ := a.Commands()
	if len(got) != 1 {
		t.Fatal("local edit lost")
	}
	if e = a.AcceptPublished(id, "", c.ID, 0); e == nil {
		t.Fatal("stale published snapshot accepted")
	}
}
func TestSyncAtomicCollisionMissingTombstoneAndRevisionGuard(t *testing.T) {
	for _, scenario := range []string{"cross-scope-collision", "omitted-resource", "same-revision-content", "rollback-revision"} {
		t.Run(scenario, func(t *testing.T) {
			s, _ := Open(":memory:")
			defer s.Close()
			id, keys := makeWorkspace(t, s)
			local := model.Command{ID: ID(), Name: "local", Text: "echo local"}
			s.SaveCommand(local)
			cloud := model.Command{Meta: model.Meta{Scope: id}, ID: ID(), Name: "cloud", Text: "echo cloud"}
			s.SaveCommand(cloud)
			snap, head, g, e := s.SyncSnapshot(id)
			if e != nil {
				t.Fatal(e)
			}
			first, _ := cloudv2.Seal(snap, keys.Wrapped.KeyID, head, keys.DataKey)
			if e = s.AcceptPublished(id, head, first.ID, g); e != nil {
				t.Fatal(e)
			}
			snap, head, _, _ = s.SyncSnapshot(id)
			switch scenario {
			case "cross-scope-collision":
				r := snap.Records[0]
				r.ID = local.ID
				var c model.Command
				json.Unmarshal(r.Data, &c)
				c.ID = local.ID
				r.Data, _ = json.Marshal(c)
				snap.Records = append(snap.Records, r)
			case "omitted-resource":
				snap.Records = nil
			case "same-revision-content":
				var c model.Command
				json.Unmarshal(snap.Records[0].Data, &c)
				c.Text = "changed without revision"
				snap.Records[0].Data, _ = json.Marshal(c)
			case "rollback-revision":
				snap.Records[0].Meta.Revision = 0
			}
			next, e := cloudv2.Seal(snap, keys.Wrapped.KeyID, head, keys.DataKey)
			if e == nil {
				e = s.ApplySyncCommit(next)
			}
			if e == nil {
				t.Fatal("invalid snapshot accepted")
			}
			after, h, _, e := s.SyncSnapshot(id)
			if e != nil || h != head || len(after.Records) != 1 {
				t.Fatal("partial apply", e)
			}
			commands, _ := s.Commands()
			if len(commands) != 2 {
				t.Fatal("local resources altered")
			}
		})
	}
}

func TestSyncSnapshotIsConsistentAcrossIndependentStoreHandles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "own-v2.db")
	a, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	id, _ := makeWorkspace(t, a)
	c := conn(ID())
	c.Meta = model.Meta{Scope: id}
	c.Name = "version-0"
	secret := c.Name
	if e = a.SaveConnection(c, &secret); e != nil {
		t.Fatal(e)
	}
	b, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	done := make(chan error, 1)
	go func() {
		for i := 1; i <= 30; i++ {
			c.Name = fmt.Sprintf("version-%d", i)
			secret := c.Name
			if e := b.SaveConnection(c, &secret); e != nil {
				done <- e
				return
			}
		}
		done <- nil
	}()
	for i := 0; i < 30; i++ {
		snap, _, _, e := a.SyncSnapshot(id)
		if e != nil {
			t.Fatal(e)
		}
		if len(snap.Records) != 1 {
			t.Fatal(len(snap.Records))
		}
		var got model.Connection
		if e = json.Unmarshal(snap.Records[0].Data, &got); e != nil {
			t.Fatal(e)
		}
		if snap.Records[0].Secret == nil || got.Name != *snap.Records[0].Secret {
			t.Fatal("mixed revisions from a non-atomic snapshot")
		}
	}
	if e = <-done; e != nil {
		t.Fatal(e)
	}
}
