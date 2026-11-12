package exchange

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"nextshell.dev/v2/internal/model"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestActualFirstPartyTypeScriptVectorByteCompatibility(t *testing.T) {
	raw, e := os.ReadFile("testdata/v1-actual-vector.json")
	if e != nil {
		t.Fatal(e)
	}
	var v struct{ Password, Plain, Payload string }
	if e = json.Unmarshal(raw, &v); e != nil {
		t.Fatal(e)
	}
	f, e := Decode([]byte("b64##"+v.Payload), &v.Password)
	if e != nil || len(f.Connections) != 1 || *f.Connections[0].Password != "密码 🔑" {
		t.Fatal(e)
	}
	plain, e := Pretty(f)
	if e != nil || string(plain) != v.Plain {
		t.Fatal("actual TS JSON bytes differ", e)
	}
	b, _ := base64.StdEncoding.DecodeString(v.Payload)
	got, e := EncryptPayloadFixed(plain, v.Password, b[:32], b[32:44])
	if e != nil || got != v.Payload {
		t.Fatal("actual TS AES envelope bytes differ", e)
	}
	c := FromEntry(f.Connections[0], nil)
	if c.Keepalive != 0 || c.KeepaliveEnabled == nil || *c.KeepaliveEnabled || c.KeepaliveIntervalSec == nil || *c.KeepaliveIntervalSec != 45 {
		t.Fatal("keepalive tri-state lost")
	}
	entry := ToEntry(c, f.Connections[0].GroupPath, nil)
	if entry.KeepAliveEnabled == nil || *entry.KeepAliveEnabled || *entry.KeepAliveIntervalSec != 45 {
		t.Fatal(entry)
	}
}
func TestGoExportsAcceptedByActualV1TypeScriptParserAndDecryptor(t *testing.T) {
	// CI installs Node; a machine without it cannot run the real v1 parser, and must not pretend to.
	if _, e := exec.LookPath("node"); e != nil {
		t.Skip("node is not on PATH: cannot run the actual v1 TypeScript parser (CI installs Node 24)")
	}
	pass := "interop🔒"
	secret := "secret🔑"
	entry := Entry{Name: "go", Host: "localhost", Port: 22, Username: "u", AuthType: "password", Password: &secret, GroupPath: "/server/prod", Tags: []string{"tag"}, Encoding: "utf-8"}
	for _, encrypted := range []bool{false, true} {
		name := "plain"
		var pw *string
		if encrypted {
			name = "encrypted"
			pw = &pass
		}
		t.Run(name, func(t *testing.T) {
			raw, e := Encode([]Entry{entry}, pw, time.Unix(0, 0))
			if e != nil {
				t.Fatal(e)
			}
			input, _ := json.Marshal(map[string]string{"raw": string(raw), "password": pass})
			cmd := exec.Command("node", "--disable-warning=ExperimentalWarning", "testdata/v1-actual-bridge.mjs")
			cmd.Stdin = bytes.NewReader(input)
			out, e := cmd.CombinedOutput()
			if e != nil {
				t.Fatalf("actual v1 TS invocation: %v %s", e, out)
			}
			var result struct {
				Plain   string
				Entries []Entry
			}
			if e = json.Unmarshal(out, &result); e != nil {
				t.Fatal(e, string(out))
			}
			if len(result.Entries) != 1 || result.Entries[0].Name != "go" || result.Entries[0].GroupPath != "/server/prod" {
				t.Fatal(string(out))
			}
			if encrypted {
				if result.Entries[0].Password == nil || *result.Entries[0].Password != secret {
					t.Fatal("v1 decryptor lost secret")
				}
			} else if result.Entries[0].Password != nil {
				t.Fatal("plain export leaked secret")
			}
		})
	}
}
func TestImportPresenceMergeAndPasswordStates(t *testing.T) {
	base := model.Connection{ID: "stable", Name: "old", Host: "h", Port: 22, User: "u", Auth: "password", Secret: "keep", Notes: "keep notes", Favorite: true, ProxyID: "proxy", Fingerprint: "SHA256:trust", StrictHostKeyChecking: true, Encoding: "big5", Meta: model.Meta{Scope: model.LocalScope, CreatedAt: "original", Revision: 4}}
	for _, test := range []struct {
		name, password string
		want           string
	}{{"missing", "", "keep"}, {"empty", `,"password":""`, ""}, {"present", `,"password":"new"`, "new"}} {
		t.Run(test.name, func(t *testing.T) {
			raw := []byte(`{"format":"nextshell-connections","version":1,"connections":[{"name":"new","host":"h","port":22,"username":"u","authType":"password"` + test.password + `}]}`)
			f, e := Decode(raw, nil)
			if e != nil {
				t.Fatal(e)
			}
			got := MergeEntry(base, f.Connections[0], nil)
			if got.ID != base.ID || got.CreatedAt != base.CreatedAt || got.Secret != test.want || got.ProxyID != base.ProxyID || got.Fingerprint != base.Fingerprint || !got.StrictHostKeyChecking || got.Notes != base.Notes || !got.Favorite || got.Encoding != base.Encoding {
				t.Fatal(got)
			}
		})
	}
	for _, field := range []string{`"password":null`, `"keepAliveEnabled":null`, `"keepAliveIntervalSec":null`} {
		if _, e := Decode([]byte(`{"format":"nextshell-connections","version":1,"connections":[{"name":"n","host":"h","port":22,"username":"u","authType":"password",`+field+`}]}`), nil); e == nil {
			t.Fatal("ambiguous null accepted")
		}
	}
	if _, e := Decode([]byte(`{"format":"nextshell-connections","version":1,"passwordsOmitted":true,"connections":[{"name":"n","host":"h","port":22,"username":"u","authType":"password","password":"leak"}]}`), nil); e == nil {
		t.Fatal("contradictory password metadata accepted")
	}
}
func TestUnmatchedSuppliedKeyFingerprintNeverFallsBackToName(t *testing.T) {
	keys := []model.Key{{ID: "wrong", Name: "same-name", Public: "invalid"}}
	ref := &KeyRef{Name: "same-name", Fingerprint: "SHA256:unmatched"}
	if MatchKey(ref, keys) != "" {
		t.Fatal("mismatched fingerprint rebound")
	}
	c := FromEntry(Entry{Name: "key", Host: "h", Port: 22, Username: "u", AuthType: "privateKey", KeyRef: ref}, keys)
	if !c.NeedsKeyRebind || c.SourceKeyRef == nil {
		t.Fatal(c)
	}
	out := ToEntry(c, "/server", nil)
	if out.AuthType != "privateKey" || out.KeyRef == nil || *out.KeyRef != *ref {
		t.Fatal("pending reference lost", out)
	}
}

func TestOverwritePrivateKeyWithoutReferencePreservesExistingBinding(t *testing.T) {
	old := model.Connection{ID: "existing", Auth: "key", KeyID: "bound", Name: "old", Host: "h", Port: 22, User: "u"}
	entry := Entry{Name: "new", Host: "h", Port: 22, Username: "u", AuthType: "privateKey"}
	got := MergeEntry(old, entry, nil)
	if got.KeyID != old.KeyID || got.Auth != "key" || got.NeedsKeyRebind {
		t.Fatal(got)
	}
}
