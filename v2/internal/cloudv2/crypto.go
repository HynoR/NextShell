package cloudv2

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/crypto/scrypt"
	"io"
	"unicode/utf8"
)

const Version = 2
const MaxPacket = 64 << 20
const MaxPlaintext = (MaxPacket-2048)*3/4 - 16
const N = 32768
const R = 8
const P = 1
const domain = "nextshell/cloud/v2/"

func random(n int) ([]byte, error) {
	b := make([]byte, n)
	_, e := io.ReadFull(rand.Reader, b)
	return b, e
}
func NewID() string {
	b, e := random(16)
	if e != nil {
		panic(e)
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}
func ValidID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// Authentication tokens have independent randomness and never enter the encryption KDF.
func NewAuthToken() (string, error) {
	b, e := random(32)
	return base64.RawURLEncoding.EncodeToString(b), e
}
func AuthVerifier(token string) ([]byte, error) {
	b, e := base64.RawURLEncoding.Strict().DecodeString(token)
	if e != nil || len(b) != 32 {
		return nil, errors.New("invalid random authentication token")
	}
	h := sha256.Sum256(append([]byte(domain+"authentication-verifier\x00"), b...))
	return h[:], nil
}
func VerifyAuth(token string, verifier []byte) bool {
	v, e := AuthVerifier(token)
	return e == nil && subtle.ConstantTimeCompare(v, verifier) == 1
}

type KDF struct {
	Name string `json:"name"`
	N    int    `json:"n"`
	R    int    `json:"r"`
	P    int    `json:"p"`
	Salt []byte `json:"salt"`
}
type WrappedKey struct {
	Version     int    `json:"version"`
	WorkspaceID string `json:"workspaceId"`
	KeyID       string `json:"keyId"`
	KDF         KDF    `json:"kdf"`
	Nonce       []byte `json:"nonce"`
	Ciphertext  []byte `json:"ciphertext"`
}

func (w WrappedKey) Validate() error { return w.validate() }
func (w WrappedKey) validate() error {
	if w.Version != Version || !ValidID(w.WorkspaceID) || !ValidID(w.KeyID) || w.KDF.Name != "scrypt" || w.KDF.N != N || w.KDF.R != R || w.KDF.P != P || len(w.KDF.Salt) != 32 || len(w.Nonce) != 12 || len(w.Ciphertext) != 48 {
		return errors.New("invalid or unsupported wrapped key")
	}
	return nil
}
func derive(pass string, w WrappedKey) ([]byte, error) {
	if pass == "" || len(pass) > 4096 || !utf8.ValidString(pass) {
		return nil, errors.New("encryption passphrase must be nonempty UTF-8, at most 4096 bytes")
	}
	salt, _ := json.Marshal([]any{domain + "kek", Version, w.WorkspaceID, w.KeyID, w.KDF.Salt})
	return scrypt.Key([]byte(pass), salt, N, R, P, 32)
}
func aead(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, errors.New("AES-256 requires 32-byte key")
	}
	b, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	return cipher.NewGCM(b)
}
func wrapAAD(w WrappedKey) []byte {
	b, _ := json.Marshal([]any{domain + "wrap-dek", Version, w.WorkspaceID, w.KeyID, w.KDF})
	return b
}
func NewKey(pass, workspace string) (WrappedKey, []byte, error) {
	key, e := random(32)
	if e != nil {
		return WrappedKey{}, nil, e
	}
	w, e := WrapKey(pass, workspace, NewID(), key)
	return w, key, e
}

// WrapKey rewraps the SAME random data key; it is not member revocation or DEK rotation.
func WrapKey(pass, workspace, keyID string, key []byte) (WrappedKey, error) {
	w := WrappedKey{Version: Version, WorkspaceID: workspace, KeyID: keyID, KDF: KDF{Name: "scrypt", N: N, R: R, P: P}}
	if !ValidID(workspace) || !ValidID(keyID) || len(key) != 32 {
		return w, errors.New("invalid workspace/key")
	}
	var e error
	if w.KDF.Salt, e = random(32); e != nil {
		return w, e
	}
	if w.Nonce, e = random(12); e != nil {
		return w, e
	}
	k, e := derive(pass, w)
	if e != nil {
		return w, e
	}
	a, e := aead(k)
	if e != nil {
		return w, e
	}
	w.Ciphertext = a.Seal(nil, w.Nonce, key, wrapAAD(w))
	return w, nil
}
func Unlock(pass string, w WrappedKey) ([]byte, error) {
	if e := w.validate(); e != nil {
		return nil, e
	}
	k, e := derive(pass, w)
	if e != nil {
		return nil, e
	}
	a, e := aead(k)
	if e != nil {
		return nil, e
	}
	return a.Open(nil, w.Nonce, w.Ciphertext, wrapAAD(w))
}

type Commit struct {
	Version     int    `json:"version"`
	WorkspaceID string `json:"workspaceId"`
	KeyID       string `json:"keyId"`
	ID          string `json:"commitId"`
	Parent      string `json:"parentId"`
	Nonce       []byte `json:"nonce"`
	Ciphertext  []byte `json:"ciphertext"`
}

func (c Commit) Validate() error {
	if c.Version != Version || !ValidID(c.WorkspaceID) || !ValidID(c.KeyID) || !ValidID(c.ID) || (c.Parent != "" && !ValidID(c.Parent)) || c.ID == c.Parent || len(c.Nonce) != 12 || len(c.Ciphertext) < 16 || len(c.Ciphertext) > MaxPlaintext+16 {
		return errors.New("invalid or unsupported encrypted commit")
	}
	return nil
}
func commitAAD(c Commit) []byte {
	b, _ := json.Marshal([]any{domain + "snapshot", c.Version, c.WorkspaceID, c.KeyID, c.ID, c.Parent})
	return b
}
func Seal(snapshot Snapshot, keyID, parent string, key []byte) (Commit, error) {
	c := Commit{Version: Version, WorkspaceID: snapshot.WorkspaceID, KeyID: keyID, ID: NewID(), Parent: parent}
	if e := snapshot.Validate(); e != nil {
		return c, e
	}
	raw, e := json.Marshal(snapshot)
	if e != nil {
		return c, e
	}
	if len(raw) > MaxPlaintext {
		return c, errors.New("snapshot too large")
	}
	a, e := aead(key)
	if e != nil {
		return c, e
	}
	c.Nonce, e = random(12)
	if e != nil {
		return c, e
	}
	c.Ciphertext = a.Seal(nil, c.Nonce, raw, commitAAD(c))
	return c, c.Validate()
}
func Open(c Commit, key []byte, workspace, keyID, expectedHead string) (Snapshot, error) {
	var s Snapshot
	if e := c.Validate(); e != nil {
		return s, e
	}
	if c.WorkspaceID != workspace || c.KeyID != keyID || c.Parent != expectedHead {
		return s, errors.New("workspace/key/head mismatch (replay or divergence)")
	}
	a, e := aead(key)
	if e != nil {
		return s, e
	}
	raw, e := a.Open(nil, c.Nonce, c.Ciphertext, commitAAD(c))
	if e != nil {
		return s, e
	}
	if e = strictJSON(raw, &s); e != nil {
		return s, e
	}
	if s.WorkspaceID != workspace {
		return s, errors.New("encrypted workspace mismatch")
	}
	return s, s.Validate()
}
func strictJSON(raw []byte, v any) error {
	if !utf8.Valid(raw) {
		return errors.New("invalid UTF-8 JSON")
	}
	if len(raw) > MaxPacket {
		return errors.New("JSON too large")
	}
	if e := rejectDuplicates(raw); e != nil {
		return e
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return e
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}

func rejectDuplicates(raw []byte) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	var walk func(int) error
	walk = func(depth int) error {
		if depth > 64 {
			return errors.New("JSON too deeply nested")
		}
		token, e := d.Token()
		if e != nil {
			return e
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				k, e := d.Token()
				if e != nil {
					return e
				}
				name, ok := k.(string)
				if !ok || seen[name] {
					return errors.New("duplicate JSON object key")
				}
				seen[name] = true
				if e = walk(depth + 1); e != nil {
					return e
				}
			}
		case '[':
			for d.More() {
				if e = walk(depth + 1); e != nil {
					return e
				}
			}
		default:
			return errors.New("invalid JSON delimiter")
		}
		_, e = d.Token()
		return e
	}
	if e := walk(0); e != nil {
		return e
	}
	if _, e := d.Token(); e != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}

func EncodeCommit(c Commit) ([]byte, error) {
	if e := c.Validate(); e != nil {
		return nil, e
	}
	raw, e := json.Marshal(c)
	if len(raw) > MaxPacket {
		return nil, errors.New("wire packet exceeds 64 MiB")
	}
	return raw, e
}
func DecodeCommit(raw []byte) (Commit, error) {
	var c Commit
	if e := strictJSON(raw, &c); e != nil {
		return c, e
	}
	return c, c.Validate()
}
func DecodeWrappedKey(raw []byte) (WrappedKey, error) {
	var w WrappedKey
	if e := strictJSON(raw, &w); e != nil {
		return w, e
	}
	return w, w.Validate()
}
