package security

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

func TestNodeEnvelopeInteroperability(t *testing.T) {
	b, e := os.ReadFile("testdata/node-envelope.json")
	if e != nil {
		t.Fatal(e)
	}
	var v struct {
		Password, Plaintext, KeyHex string
		Envelope                    Envelope
	}
	if e = json.Unmarshal(b, &v); e != nil {
		t.Fatal(e)
	}
	salt, _ := base64.StdEncoding.DecodeString(v.Envelope.Salt)
	nonce, _ := base64.StdEncoding.DecodeString(v.Envelope.IV)
	key, e := Derive(v.Password, salt)
	if e != nil || hex.EncodeToString(key) != v.KeyHex {
		t.Fatal("scrypt differs from Node", e)
	}
	env, e := EncryptFixed(v.Plaintext, v.Password, v.Envelope.AAD, salt, nonce)
	if e != nil || env != v.Envelope {
		t.Fatalf("byte incompatibility: %+v %v", env, e)
	}
	plain, e := Decrypt(v.Envelope, v.Password)
	if e != nil || plain != v.Plaintext {
		t.Fatal("cannot read Node ciphertext", e)
	}
	if _, e = Decrypt(v.Envelope, "wrong password"); e == nil {
		t.Fatal("wrong password accepted")
	}
}
func TestEnvelopeRoundTripAndRandomness(t *testing.T) {
	for _, p := range []string{"", "hello", "中文🙂"} {
		t.Run(p, func(t *testing.T) {
			a, e := Encrypt(p, "pass", "aad")
			if e != nil {
				t.Fatal(e)
			}
			b, e := Encrypt(p, "pass", "aad")
			if e != nil {
				t.Fatal(e)
			}
			if a.IV == b.IV || a.Salt == b.Salt {
				t.Fatal("nonce/salt reused")
			}
			got, e := Decrypt(a, "pass")
			if e != nil || got != p {
				t.Fatal(got, e)
			}
		})
	}
}
func TestEnvelopeRejectsTampering(t *testing.T) {
	original, e := Encrypt("private", "pass", "aad")
	if e != nil {
		t.Fatal(e)
	}
	cases := map[string]func(*Envelope){"version": func(v *Envelope) { v.V = 2 }, "algorithm": func(v *Envelope) { v.Alg = "xor" }, "kdf": func(v *Envelope) { v.KDF = "pbkdf2" }, "salt": func(v *Envelope) { v.Salt = "AA==" }, "iv": func(v *Envelope) { v.IV = "AA==" }, "tag": func(v *Envelope) { v.Tag = "AA==" }, "ciphertext": func(v *Envelope) { v.Ciphertext = "AA==" }, "aad": func(v *Envelope) { v.AAD = "swapped" }, "base64": func(v *Envelope) { v.Salt = "!!!" }}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			v := original
			change(&v)
			if _, e := Decrypt(v, "pass"); e == nil {
				t.Fatal("tamper accepted")
			}
		})
	}
}
func TestVaultEncryption(t *testing.T) {
	key := bytes.Repeat([]byte{1}, 32)
	a, e := Seal([]byte("secret"), key, "id-a")
	if e != nil {
		t.Fatal(e)
	}
	p, e := Unseal(a, key, "id-a")
	if e != nil || string(p) != "secret" {
		t.Fatal(e)
	}
	if _, e = Unseal(a, key, "id-b"); e == nil {
		t.Fatal("AAD swap accepted")
	}
	if _, e = Unseal(a, bytes.Repeat([]byte{2}, 32), "id-a"); e == nil {
		t.Fatal("wrong key accepted")
	}
	if _, e = Unseal([]byte("invalid"), key, ""); e == nil {
		t.Fatal("invalid JSON accepted")
	}
	if _, e = Seal(nil, []byte{1}, ""); e == nil {
		t.Fatal("bad key accepted")
	}
	if _, _, e = SealWithNonce(nil, key, []byte{1}, ""); e == nil {
		t.Fatal("bad nonce accepted")
	}
}
