// Package security implements NextShell's 1.x-compatible authenticated envelope.
package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"golang.org/x/crypto/scrypt"
	"io"
)

type Envelope struct {
	V          int    `json:"v"`
	Alg        string `json:"alg"`
	KDF        string `json:"kdf"`
	Salt       string `json:"salt"`
	IV         string `json:"iv"`
	AAD        string `json:"aad,omitempty"`
	Ciphertext string `json:"ciphertext"`
	Tag        string `json:"tag"`
}

func Derive(password string, salt []byte) ([]byte, error) {
	return scrypt.Key([]byte(password), salt, 16384, 8, 1, 32)
}
func Random(n int) ([]byte, error) {
	b := make([]byte, n)
	_, e := io.ReadFull(rand.Reader, b)
	return b, e
}
func gcm(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, errors.New("AES-256 key must be 32 bytes")
	}
	b, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	return cipher.NewGCM(b)
}
func SealWithNonce(plain, key, nonce []byte, aad string) (ct, tag []byte, err error) {
	g, e := gcm(key)
	if e != nil {
		return nil, nil, e
	}
	if len(nonce) != g.NonceSize() {
		return nil, nil, errors.New("invalid nonce length")
	}
	b := g.Seal(nil, nonce, plain, []byte(aad))
	return b[:len(b)-16], b[len(b)-16:], nil
}
func Open(ct, tag, key, nonce []byte, aad string) ([]byte, error) {
	g, e := gcm(key)
	if e != nil {
		return nil, e
	}
	if len(nonce) != g.NonceSize() || len(tag) != g.Overhead() {
		return nil, errors.New("invalid GCM envelope")
	}
	return g.Open(nil, nonce, append(append([]byte{}, ct...), tag...), []byte(aad))
}
func Seal(plain, key []byte, aad string) ([]byte, error) {
	n, e := Random(12)
	if e != nil {
		return nil, e
	}
	c, t, e := SealWithNonce(plain, key, n, aad)
	if e != nil {
		return nil, e
	}
	return json.Marshal(struct{ IV, Ciphertext, Tag string }{b64(n), b64(c), b64(t)})
}
func Unseal(data, key []byte, aad string) ([]byte, error) {
	var v struct{ IV, Ciphertext, Tag string }
	if e := json.Unmarshal(data, &v); e != nil {
		return nil, e
	}
	n, e := un64(v.IV)
	if e != nil {
		return nil, e
	}
	c, e := un64(v.Ciphertext)
	if e != nil {
		return nil, e
	}
	t, e := un64(v.Tag)
	if e != nil {
		return nil, e
	}
	return Open(c, t, key, n, aad)
}
func Encrypt(plain, password, aad string) (Envelope, error) {
	s, e := Random(32)
	if e != nil {
		return Envelope{}, e
	}
	n, e := Random(12)
	if e != nil {
		return Envelope{}, e
	}
	return EncryptFixed(plain, password, aad, s, n)
}

// EncryptFixed is deterministic for interoperability vectors; callers must never reuse a nonce in production.
func EncryptFixed(plain, password, aad string, salt, nonce []byte) (Envelope, error) {
	if len(salt) != 32 {
		return Envelope{}, errors.New("invalid salt")
	}
	k, e := Derive(password, salt)
	if e != nil {
		return Envelope{}, e
	}
	c, t, e := SealWithNonce([]byte(plain), k, nonce, aad)
	return Envelope{1, "aes-256-gcm", "scrypt", b64(salt), b64(nonce), aad, b64(c), b64(t)}, e
}
func Decrypt(v Envelope, password string) (string, error) {
	if v.V != 1 || v.Alg != "aes-256-gcm" || v.KDF != "scrypt" {
		return "", errors.New("unsupported envelope")
	}
	s, e := un64(v.Salt)
	if e != nil || len(s) != 32 {
		return "", errors.New("invalid salt")
	}
	k, e := Derive(password, s)
	if e != nil {
		return "", e
	}
	n, e := un64(v.IV)
	if e != nil {
		return "", e
	}
	c, e := un64(v.Ciphertext)
	if e != nil {
		return "", e
	}
	t, e := un64(v.Tag)
	if e != nil {
		return "", e
	}
	p, e := Open(c, t, k, n, v.AAD)
	return string(p), e
}
func b64(b []byte) string           { return base64.StdEncoding.EncodeToString(b) }
func un64(s string) ([]byte, error) { return base64.StdEncoding.Strict().DecodeString(s) }
