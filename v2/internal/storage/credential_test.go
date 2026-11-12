package storage

import (
	"bytes"
	"encoding/json"
	"testing"

	"nextshell.dev/v2/internal/cloudv2"
	"nextshell.dev/v2/internal/model"
)

// Deleting a credential returns its hosts to their own authentication; a host that had none keeps a
// copy of the credential's. A key a credential uses cannot be deleted.
func TestCredentialDeleteFallsBackAndHoldsItsKey(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	key := model.Key{ID: ID(), Name: "k", Private: "PRIVATE"}
	if e := s.SaveKey(key); e != nil {
		t.Fatal(e)
	}
	cr := model.Credential{ID: ID(), Name: "ops", User: "ops", Auth: "password"}
	pw := "CRED"
	if e := s.SaveCredential(cr, &pw); e != nil {
		t.Fatal(e)
	}
	own := conn(ID())
	own.CredentialID = cr.ID
	ownPw := "OWN"
	bare := conn(ID())
	bare.User, bare.CredentialID = "", cr.ID
	for _, c := range []model.Connection{own, bare} {
		if e := s.SaveConnection(c, &ownPw); e != nil {
			t.Fatal(e)
		}
	}
	keyed := cr
	keyed.ID, keyed.Auth, keyed.KeyID = ID(), "key", key.ID
	if e := s.SaveCredential(keyed, nil); e != nil {
		t.Fatal(e)
	}
	if e := s.Delete("key", key.ID); e == nil {
		t.Fatal("deleted a key a credential uses")
	}
	if e := s.Delete("credential", cr.ID); e != nil {
		t.Fatal(e)
	}
	a, _ := s.Connection(own.ID)
	b, _ := s.Connection(bare.ID)
	if a.CredentialID != "" || a.User != own.User || a.Secret != "OWN" {
		t.Fatal("own auth lost", a)
	}
	if b.CredentialID != "" || b.User != "ops" || b.Secret != "CRED" {
		t.Fatal("bare host not given the credential", b)
	}
}

// Credentials sync encrypted with the workspace, and a copy to a workspace takes the credential along.
func TestCredentialSyncsAndCopiesToWorkspace(t *testing.T) {
	a, _ := Open(":memory:")
	defer a.Close()
	b, _ := Open(":memory:")
	defer b.Close()
	id, keys := makeWorkspace(t, a)
	if e := b.SaveWorkspace(Workspace{ID: id, Name: "other"}, keys); e != nil {
		t.Fatal(e)
	}
	cr := model.Credential{ID: ID(), Name: "ops", User: "ops", Auth: "password"}
	pw := "CRED_SECRET"
	if e := a.SaveCredential(cr, &pw); e != nil {
		t.Fatal(e)
	}
	c := conn(ID())
	c.CredentialID = cr.ID
	if e := a.SaveConnection(c, nil); e != nil {
		t.Fatal(e)
	}
	if e := a.CopyLocalToWorkspace(id, []string{c.ID}); e != nil {
		t.Fatal(e)
	}
	snapshot, head, _, e := a.SyncSnapshot(id)
	if e != nil {
		t.Fatal(e)
	}
	commit, e := cloudv2.Seal(snapshot, keys.Wrapped.KeyID, head, keys.DataKey)
	if e != nil {
		t.Fatal(e)
	}
	wire, _ := json.Marshal(commit)
	if bytes.Contains(wire, []byte(pw)) {
		t.Fatal("plaintext leaked")
	}
	if e = b.ApplySyncCommit(commit); e != nil {
		t.Fatal(e)
	}
	crs, _ := b.Credentials()
	cs, _ := b.Connections()
	if len(crs) != 1 || len(cs) != 1 || cs[0].CredentialID != crs[0].ID {
		t.Fatal(crs, cs)
	}
	if got, _ := b.Credential(crs[0].ID); got.Secret != pw {
		t.Fatal("secret", got)
	}
}
