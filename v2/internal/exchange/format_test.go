package exchange

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"golang.org/x/crypto/ssh"
	"nextshell.dev/v2/internal/model"
	"os"
	"strings"
	"testing"
	"time"
)

type vectors struct {
	ExportVector struct{ Password, Plain, Salt, IV, Encrypted string }
	Finals       []struct {
		Signed                           bool
		KeyHex, Head, Plaintext, Encoded string
	}
}

func load(t *testing.T) vectors {
	t.Helper()
	b, e := os.ReadFile("testdata/v1-vectors.json")
	if e != nil {
		t.Fatal(e)
	}
	var v vectors
	if e = json.Unmarshal(b, &v); e != nil {
		t.Fatal(e)
	}
	return v
}
func TestExactNodeExportBytes(t *testing.T) {
	v := load(t).ExportVector
	salt, _ := base64.StdEncoding.DecodeString(v.Salt)
	iv, _ := base64.StdEncoding.DecodeString(v.IV)
	encoded, e := EncryptPayloadFixed([]byte(v.Plain), v.Password, salt, iv)
	if e != nil || "b64##"+encoded != v.Encrypted {
		t.Fatal("export byte layout differs from 1.x Node implementation", e)
	}
	plain, e := DecryptPayload(encoded, v.Password)
	if e != nil || string(plain) != v.Plain {
		t.Fatal(e)
	}
	file, e := Decode([]byte(v.Encrypted), &v.Password)
	if e != nil || file.Connections[0].Password == nil || *file.Connections[0].Password != "中文-secret" {
		t.Fatal(file, e)
	}
	canonical, e := Pretty(file)
	if e != nil || !bytes.Equal(canonical, []byte(v.Plain)) {
		t.Fatal("fixture JSON field order/bytes differ", e)
	}
	wrong := "wrong"
	if _, e = Decode([]byte(v.Encrypted), &wrong); e == nil {
		t.Fatal("wrong export password accepted")
	}
	if _, e = Decode([]byte(v.Encrypted), nil); e == nil {
		t.Fatal("missing password accepted")
	}
}
func TestExportOmitSecretsAndRoundtrip(t *testing.T) {
	secret := "do-not-export"
	entry := Entry{Name: "x", Host: "h", Port: 22, Username: "u", AuthType: "password", Password: &secret, Tags: []string{}, Encoding: "utf-8"}
	plain, e := Encode([]Entry{entry}, nil, time.Unix(0, 0))
	if e != nil || bytes.Contains(plain, []byte(secret)) {
		t.Fatal("unencrypted credential leak", e)
	}
	f, e := Decode(plain, nil)
	if e != nil || !f.PasswordsOmitted || f.Connections[0].Password != nil {
		t.Fatal(f, e)
	}
	pass := "test"
	encrypted, e := Encode([]Entry{entry}, &pass, time.Unix(0, 0))
	if e != nil {
		t.Fatal(e)
	}
	f, e = Decode(encrypted, &pass)
	if e != nil || f.PasswordsOmitted || *f.Connections[0].Password != secret {
		t.Fatal(f, e)
	}
}
func TestExportMalformed(t *testing.T) {
	v := load(t).ExportVector
	for _, raw := range []string{"b64##", "b64##AA==", `{"format":"nextshell-connections","version":2}`, `{"format":"other","version":1}`, `{"format":"nextshell-connections","version":1,"passwordsObfuscated":true}`, `{"format":"nextshell-connections","version":1,"connections":[{"port":70000}]}`} {
		t.Run(fmt.Sprint(len(raw)), func(t *testing.T) {
			if _, e := Decode([]byte(raw), &v.Password); e == nil {
				t.Fatal("malformed accepted")
			}
		})
	}
	raw, _ := base64.StdEncoding.DecodeString(v.Encrypted[5:])
	raw[44] ^= 1
	if _, e := DecryptPayload(base64.StdEncoding.EncodeToString(raw), v.Password); e == nil {
		t.Fatal("tag tamper accepted")
	}
}
func TestFinalShellFirstPartyVectors(t *testing.T) {
	for _, v := range load(t).Finals {
		t.Run(fmt.Sprint("signed=", v.Signed), func(t *testing.T) {
			head, _ := base64.StdEncoding.DecodeString(v.Head)
			if hex.EncodeToString(finalKey(head, v.Signed)) != v.KeyHex {
				t.Fatal("Java Random/DES derivation differs from TS")
			}
			got, e := DecryptFinalShell(v.Encoded)
			if e != nil || got != v.Plaintext {
				t.Fatal(got, e)
			}
		})
	}
	for _, v := range []string{"", "not-base64", "AA==", "AAAAAAAAAAAAAA=="} {
		if _, e := DecryptFinalShell(v); e == nil {
			t.Fatal("invalid password decoded")
		}
	}
}
func TestFinalShellAuthAndEncodingSpecs(t *testing.T) {
	for _, auth := range []int{1, 2} {
		for _, key := range []string{"", "key-1"} {
			t.Run(fmt.Sprintf("%d/%s", auth, key), func(t *testing.T) {
				raw := fmt.Sprintf(`{"name":"legacy","host":"10.0.0.2","user_name":"admin","authentication_type":%d,"secret_key_id":%q,"terminal_encoding":"GB-18030","backspace_key_sequence":2}`, auth, key)
				entries, e := DecodeFinalShell([]byte(raw), "/import/legacy/prod")
				if e != nil {
					t.Fatal(e)
				}
				v := entries[0]
				if v.Encoding != "gb18030" || v.GroupPath != "/import/legacy/prod" || v.Backspace != "ascii-delete" || !v.PasswordUnavailable {
					t.Fatal(v)
				}
				if key == "" && v.AuthType != "password" {
					t.Fatal("guessed authentication_type numeric meaning")
				}
				if key != "" && (!v.NeedsKeyRebind || v.AuthType != "privateKey") {
					t.Fatal(v)
				}
			})
		}
	}
}
func TestFolderAndKeyMatchSpecs(t *testing.T) {
	cases := map[string][]string{"/server/prod": {"prod"}, "/import/finalshell/hk": {"finalshell", "hk"}, "/workspace/slug/a/b": {"a", "b"}, "/": {""}}
	for input, want := range cases {
		got := Segments(input)
		if input == "/" {
			if len(got) != 0 {
				t.Fatal(got)
			}
			continue
		}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatal(input, got)
		}
	}
	keys := []model.Key{{ID: "one", Name: "ops"}, {ID: "two", Name: "other"}}
	if MatchKey(&KeyRef{Name: "ops"}, keys) != "one" {
		t.Fatal("unique name match")
	}
	keys = append(keys, model.Key{ID: "dup", Name: "ops"})
	if MatchKey(&KeyRef{Name: "ops"}, keys) != "" {
		t.Fatal("ambiguous name guessed")
	}
	c := FromEntry(Entry{Name: "x", Host: "h", Port: 22, Username: "u", AuthType: "privateKey", KeyRef: &KeyRef{Name: "missing"}}, keys)
	if c.Auth != "key" || c.KeyID != "" || !c.NeedsKeyRebind || c.Validate() != nil {
		t.Fatal(c)
	}
}

func TestKeyFingerprintPrecedesUniqueNameSpec(t *testing.T) {
	_, private, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	signer, e := ssh.NewSignerFromKey(private)
	if e != nil {
		t.Fatal(e)
	}
	keys := []model.Key{{ID: "correct", Name: "renamed", Public: string(ssh.MarshalAuthorizedKey(signer.PublicKey()))}, {ID: "wrong", Name: "old-name"}}
	ref := &KeyRef{Name: "old-name", Fingerprint: ssh.FingerprintSHA256(signer.PublicKey())}
	if MatchKey(ref, keys) != "correct" {
		t.Fatal("fingerprint should have precedence over unique name")
	}
}

func TestImportMonitorDefaultsOn(t *testing.T) {
	for in, want := range map[string]bool{`{"name":"a","host":"h","port":22,"username":"u","authType":"agent"}`: true, `{"name":"a","host":"h","port":22,"username":"u","authType":"agent","monitorSession":false}`: false} {
		var e Entry
		if err := json.Unmarshal([]byte(in), &e); err != nil {
			t.Fatal(err)
		}
		if got := FromEntry(e, nil).Monitor; got != want {
			t.Fatalf("%s: %v", in, got)
		}
	}
}

// A host using a credential is exported without a user of its own; when the credential's key is not
// here, the host waits for that key instead of being dropped.
func TestCredentialByReference(t *testing.T) {
	raw := `{"format":"nextshell-connections","version":1,"exportedAt":"","passwordsOmitted":true,"connections":[{"name":"a","host":"h","port":22,"username":"","authType":"password","credential":{"name":"ops","username":"ops","authType":"privateKey","sshKeyRef":{"name":"deploy"}}}]}`
	f, e := Decode([]byte(raw), nil)
	if e != nil {
		t.Fatal(e)
	}
	c := FromEntry(f.Connections[0], nil)
	if c.User != "ops" || c.Auth != "key" || !c.NeedsKeyRebind || c.SourceKeyRef == nil || c.SourceKeyRef.Name != "deploy" || c.Validate() != nil {
		t.Fatal("missing key", c)
	}
	keys := []model.Key{{ID: "k", Name: "deploy"}}
	if c = FromEntry(f.Connections[0], keys); c.User != "" || c.Auth != "password" || c.NeedsKeyRebind {
		t.Fatal("credential copied into the host", c)
	}
	if _, e = Decode([]byte(strings.Replace(raw, `,"credential":{"name":"ops","username":"ops","authType":"privateKey","sshKeyRef":{"name":"deploy"}}`, "", 1)), nil); e == nil {
		t.Fatal("host without a user or credential accepted")
	}
}
