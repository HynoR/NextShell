package cloudv2

import (
	"bytes"
	"encoding/json"
	"nextshell.dev/v2/internal/model"
	"os"
	"strings"
	"testing"
)

func fixture() Snapshot {
	workspace := "10000000-0000-4000-8000-000000000001"
	m := model.Meta{Scope: workspace, CreatedAt: "2026-11-11T00:00:00.000Z", UpdatedAt: "2026-11-11T00:00:00.000Z", Revision: 1}
	c := model.Command{Meta: m, ID: "20000000-0000-4000-8000-000000000001", Name: "private-command-name", Text: "echo PRIVATE_COMMAND", Group: "private-group"}
	raw, _ := json.Marshal(c)
	return Snapshot{Version: Version, WorkspaceID: workspace, Name: "PRIVATE_WORKSPACE", Records: []Record{{Kind: "command", ID: c.ID, Meta: m, Data: raw}}}
}
func clone[T any](v T) T { b, _ := json.Marshal(v); var out T; json.Unmarshal(b, &out); return out }
func TestIndependentAuthenticationAndEncryption(t *testing.T) {
	token, e := NewAuthToken()
	if e != nil {
		t.Fatal(e)
	}
	v, e := AuthVerifier(token)
	if e != nil || !VerifyAuth(token, v) {
		t.Fatal(e)
	}
	other, _ := NewAuthToken()
	if VerifyAuth(other, v) || VerifyAuth("password", v) {
		t.Fatal("bad token accepted")
	}
	w, key, e := NewKey("independent-encryption-passphrase", fixture().WorkspaceID)
	if e != nil {
		t.Fatal(e)
	}
	unlocked, e := Unlock("independent-encryption-passphrase", w)
	if e != nil || !bytes.Equal(key, unlocked) {
		t.Fatal(e)
	}
	if _, e = Unlock(token, w); e == nil {
		t.Fatal("auth token unlocked encryption")
	}
	if _, e = Unlock(string(v), w); e == nil {
		t.Fatal("verifier unlocked encryption")
	}
	if strings.Contains(string(mustJSON(w)), token) {
		t.Fatal("token in wrapped key")
	}
}
func mustJSON(v any) []byte {
	b, e := json.Marshal(v)
	if e != nil {
		panic(e)
	}
	return b
}
func TestWrappedKeyRejectsTamperAndKDFDowngrade(t *testing.T) {
	w, _, e := NewKey("private", fixture().WorkspaceID)
	if e != nil {
		t.Fatal(e)
	}
	for name, change := range map[string]func(*WrappedKey){"version": func(w *WrappedKey) { w.Version = 1 }, "workspace": func(w *WrappedKey) { w.WorkspaceID = NewID() }, "key": func(w *WrappedKey) { w.KeyID = NewID() }, "salt": func(w *WrappedKey) { w.KDF.Salt[0] ^= 1 }, "n": func(w *WrappedKey) { w.KDF.N = 1 << 30 }, "r": func(w *WrappedKey) { w.KDF.R++ }, "p": func(w *WrappedKey) { w.KDF.P++ }, "algorithm": func(w *WrappedKey) { w.KDF.Name = "pbkdf2" }, "nonce": func(w *WrappedKey) { w.Nonce[0] ^= 1 }, "tag": func(w *WrappedKey) { w.Ciphertext[len(w.Ciphertext)-1] ^= 1 }, "short": func(w *WrappedKey) { w.Ciphertext = w.Ciphertext[:1] }} {
		t.Run(name, func(t *testing.T) {
			bad := clone(w)
			change(&bad)
			if _, e := Unlock("private", bad); e == nil {
				t.Fatal("tamper accepted")
			}
		})
	}
}
func TestRewrapAndAuthRotationDoNotChangeDEK(t *testing.T) {
	s := fixture()
	w, key, e := NewKey("before", s.WorkspaceID)
	if e != nil {
		t.Fatal(e)
	}
	c, e := Seal(s, w.KeyID, "", key)
	if e != nil {
		t.Fatal(e)
	}
	next, e := WrapKey("after", s.WorkspaceID, w.KeyID, key)
	if e != nil {
		t.Fatal(e)
	}
	got, e := Unlock("after", next)
	if e != nil || !bytes.Equal(got, key) {
		t.Fatal(e)
	}
	if _, e = Unlock("before", next); e == nil {
		t.Fatal("old wrapping pass accepted")
	}
	if _, e = Open(c, got, s.WorkspaceID, w.KeyID, ""); e != nil {
		t.Fatal(e)
	}
	if bytes.Equal(w.KDF.Salt, next.KDF.Salt) || bytes.Equal(w.Nonce, next.Nonce) {
		t.Fatal("wrapping randomness reused")
	}
	a, _ := NewAuthToken()
	b, _ := NewAuthToken()
	if a == b {
		t.Fatal("authentication randomness reused")
	}
}
func TestSnapshotEncryptsAllMetadataCommandsAndSecrets(t *testing.T) {
	s := fixture()
	key := bytes.Repeat([]byte{7}, 32)
	kid := NewID()
	c, e := Seal(s, kid, "", key)
	if e != nil {
		t.Fatal(e)
	}
	wire := mustJSON(c)
	for _, v := range []string{s.Name, "private-command-name", "PRIVATE_COMMAND", "private-group", s.Records[0].Meta.CreatedAt, s.Records[0].ID} {
		if bytes.Contains(wire, []byte(v)) {
			t.Fatal("plaintext leaked", v)
		}
	}
	got, e := Open(c, key, s.WorkspaceID, kid, "")
	if e != nil || string(mustJSON(got)) != string(mustJSON(s)) {
		t.Fatal(e)
	}
	other, e := Seal(s, kid, "", key)
	if e != nil || bytes.Equal(c.Nonce, other.Nonce) || bytes.Equal(c.Ciphertext, other.Ciphertext) {
		t.Fatal("nonce reused", e)
	}
}
func TestCommitAADVersionAndReplayProtection(t *testing.T) {
	s := fixture()
	key := bytes.Repeat([]byte{1}, 32)
	kid := NewID()
	c, e := Seal(s, kid, "", key)
	if e != nil {
		t.Fatal(e)
	}
	for name, f := range map[string]func(*Commit){"workspace": func(c *Commit) { c.WorkspaceID = NewID() }, "key": func(c *Commit) { c.KeyID = NewID() }, "commit": func(c *Commit) { c.ID = NewID() }, "parent": func(c *Commit) { c.Parent = NewID() }, "version": func(c *Commit) { c.Version = 1 }, "future": func(c *Commit) { c.Version = 99 }, "nonce": func(c *Commit) { c.Nonce[0] ^= 1 }, "tag": func(c *Commit) { c.Ciphertext[0] ^= 1 }, "self-parent": func(c *Commit) { c.Parent = c.ID }} {
		t.Run(name, func(t *testing.T) {
			bad := clone(c)
			f(&bad)
			if _, e := Open(bad, key, s.WorkspaceID, kid, ""); e == nil {
				t.Fatal("bad commit accepted")
			}
		})
	}
	if _, e = Open(c, key, s.WorkspaceID, kid, c.ID); e == nil {
		t.Fatal("replay accepted")
	}
	if _, e = Open(c, bytes.Repeat([]byte{2}, 32), s.WorkspaceID, kid, ""); e == nil {
		t.Fatal("wrong DEK")
	}
}
func TestSnapshotValidationRejectsBrokenGraphsAndMetadata(t *testing.T) {
	for name, f := range map[string]func(*Snapshot){"version": func(s *Snapshot) { s.Version = 1 }, "scope": func(s *Snapshot) { s.Records[0].Meta.Scope = "local" }, "id": func(s *Snapshot) { s.Records[0].ID = "x" }, "duplicate": func(s *Snapshot) { s.Records = append(s.Records, s.Records[0]) }, "revision": func(s *Snapshot) { s.Records[0].Meta.Revision = 0 }, "timestamp": func(s *Snapshot) { s.Records[0].Meta.UpdatedAt = "2020-01-01T00:00:00.000Z" }, "unknown-kind": func(s *Snapshot) { s.Records[0].Kind = "dangerous" }, "unexpected-secret": func(s *Snapshot) { secret := "x"; s.Records[0].Secret = &secret }, "tombstone-data": func(s *Snapshot) { s.Records[0].Tombstone = true }, "unknown-data": func(s *Snapshot) {
		s.Records[0].Data = bytes.Replace(s.Records[0].Data, []byte(`"command":`), []byte(`"unknown":`), 1)
	}, "dangling-parent": func(s *Snapshot) {
		m := s.Records[0].Meta
		f := model.Folder{Meta: m, ID: NewID(), Name: "folder", ParentID: NewID()}
		s.Records = []Record{{Kind: "folder", ID: f.ID, Meta: m, Data: mustJSON(f)}}
	}, "folder-cycle": func(s *Snapshot) {
		m := s.Records[0].Meta
		f := model.Folder{Meta: m, ID: NewID(), Name: "folder"}
		f.ParentID = f.ID
		s.Records = []Record{{Kind: "folder", ID: f.ID, Meta: m, Data: mustJSON(f)}}
	}} {
		t.Run(name, func(t *testing.T) {
			s := fixture()
			f(&s)
			if e := s.Validate(); e == nil {
				t.Fatal("invalid snapshot accepted")
			}
		})
	}
}
func TestStrictJSONContract(t *testing.T) {
	for _, raw := range []string{`{"version":2,"version":2}`, `{"version":2,"unknown":1}`, `{} {}`, string([]byte{0xff}), strings.Repeat("[", 66) + strings.Repeat("]", 66)} {
		if e := strictJSON([]byte(raw), new(Snapshot)); e == nil {
			t.Fatal("invalid JSON accepted", raw)
		}
	}
	for _, p := range []string{"", strings.Repeat("x", 4097), string([]byte{0xff})} {
		if _, _, e := NewKey(p, fixture().WorkspaceID); e == nil {
			t.Fatal("bad passphrase accepted")
		}
	}
}
func TestOfflineOpaqueCASStopsConcurrentWrites(t *testing.T) {
	token, _ := NewAuthToken()
	verifier, _ := AuthVerifier(token)
	server := NewMemoryServer(verifier)
	s := fixture()
	w, key, e := NewKey("encryption only", s.WorkspaceID)
	if e != nil {
		t.Fatal(e)
	}
	first, _ := Seal(s, w.KeyID, "", key)
	head, e := server.Accept(token, first, w)
	if e != nil || head != first.ID {
		t.Fatal(e)
	}
	if _, e = server.Accept(token, first, w); e == nil {
		t.Fatal("replay accepted")
	}
	if _, _, e = server.Read("wrong", s.WorkspaceID); e == nil {
		t.Fatal("unauthenticated read")
	}
	left, _ := Seal(s, w.KeyID, head, key)
	right, _ := Seal(s, w.KeyID, head, key)
	if _, e = server.Accept(token, left, w); e != nil {
		t.Fatal(e)
	}
	got, e := server.Accept(token, right, w)
	if e == nil || got != left.ID {
		t.Fatal("concurrent write not rejected")
	}
	c, wk, e := server.Read(token, s.WorkspaceID)
	if e != nil || c.ID != left.ID || wk.KeyID != w.KeyID {
		t.Fatal(e)
	}
	c.Ciphertext[0] ^= 1
	again, _, _ := server.Read(token, s.WorkspaceID)
	if bytes.Equal(c.Ciphertext, again.Ciphertext) {
		t.Fatal("fixture aliased server buffers")
	}
}

func TestIndependentNodeProtocolKnownAnswerVector(t *testing.T) {
	raw, e := os.ReadFile("testdata/node-vector.json")
	if e != nil {
		t.Fatal(e)
	}
	var v struct {
		Password string
		DEK, KEK []byte
		Wrapped  WrappedKey
		Snapshot Snapshot
		Commit   Commit
	}
	if e = json.Unmarshal(raw, &v); e != nil {
		t.Fatal(e)
	}
	kek, e := derive(v.Password, v.Wrapped)
	if e != nil || !bytes.Equal(kek, v.KEK) {
		t.Fatal("Node KEK mismatch", e)
	}
	key, e := Unlock(v.Password, v.Wrapped)
	if e != nil || !bytes.Equal(key, v.DEK) {
		t.Fatal("Node wrapped DEK mismatch", e)
	}
	got, e := Open(v.Commit, key, v.Wrapped.WorkspaceID, v.Wrapped.KeyID, "")
	if e != nil || string(mustJSON(got)) != string(mustJSON(v.Snapshot)) {
		t.Fatal("Node commit mismatch", e)
	}
	a, _ := aead(kek)
	wrapped := a.Seal(nil, v.Wrapped.Nonce, v.DEK, wrapAAD(v.Wrapped))
	if !bytes.Equal(wrapped, v.Wrapped.Ciphertext) {
		t.Fatal("Go wrapping bytes differ from Node")
	}
	a, _ = aead(key)
	ct := a.Seal(nil, v.Commit.Nonce, mustJSON(v.Snapshot), commitAAD(v.Commit))
	if !bytes.Equal(ct, v.Commit.Ciphertext) {
		t.Fatal("Go snapshot bytes differ from Node")
	}
}
func TestWireCodecRejectsUnknownFieldsAndDuplicateKeys(t *testing.T) {
	s := fixture()
	key := bytes.Repeat([]byte{1}, 32)
	c, e := Seal(s, NewID(), "", key)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := EncodeCommit(c)
	if e != nil {
		t.Fatal(e)
	}
	got, e := DecodeCommit(raw)
	if e != nil || got.ID != c.ID {
		t.Fatal(e)
	}
	for _, bad := range [][]byte{bytes.Replace(raw, []byte(`"version":2`), []byte(`"version":2,"version":2`), 1), bytes.Replace(raw, []byte(`"version":2`), []byte(`"version":2,"plaintextName":"leak"`), 1)} {
		if _, e = DecodeCommit(bad); e == nil {
			t.Fatal("bad outer packet accepted")
		}
	}
	w, _, e := NewKey("private", s.WorkspaceID)
	if e != nil {
		t.Fatal(e)
	}
	raw = mustJSON(w)
	if _, e = DecodeWrappedKey(raw); e != nil {
		t.Fatal(e)
	}
	raw = bytes.Replace(raw, []byte(`"version":2`), []byte(`"version":2,"authToken":"must not be present"`), 1)
	if _, e = DecodeWrappedKey(raw); e == nil {
		t.Fatal("auth material in encryption contract accepted")
	}
}
