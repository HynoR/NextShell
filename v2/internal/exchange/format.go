// Package exchange implements NextShell v1 and FinalShell wire formats without UI dependencies.
package exchange

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/crypto/ssh"
	"nextshell.dev/v2/internal/model"
	"nextshell.dev/v2/internal/security"
	"strings"
	"time"
)

const Format = "nextshell-connections"
const ExportAAD = "nextshell-connection-export"

type KeyRef struct {
	Name        string `json:"name"`
	Fingerprint string `json:"fingerprint,omitempty"`
}
type Entry struct {
	Present              map[string]bool `json:"-"`
	Name                 string          `json:"name"`
	Host                 string          `json:"host"`
	Port                 int             `json:"port"`
	Username             string          `json:"username"`
	AuthType             string          `json:"authType"`
	Password             *string         `json:"password,omitempty"`
	KeepAliveEnabled     *bool           `json:"keepAliveEnabled,omitempty"`
	KeepAliveIntervalSec *int            `json:"keepAliveIntervalSec,omitempty"`
	GroupPath            string          `json:"groupPath"`
	Tags                 []string        `json:"tags"`
	Notes                string          `json:"notes,omitempty"`
	Favorite             bool            `json:"favorite"`
	Encoding             string          `json:"terminalEncoding"`
	Backspace            string          `json:"backspaceMode"`
	Delete               string          `json:"deleteMode"`
	Monitor              bool            `json:"monitorSession"`
	KeyRef               *KeyRef         `json:"sshKeyRef,omitempty"`
	Credential           *CredentialRef  `json:"credential,omitempty"` // the shared credential the user and auth above come from
	NeedsKeyRebind       bool            `json:"-"`
	PasswordUnavailable  bool            `json:"-"`
}

// CredentialRef carries a shared credential with each host using it; its password follows the
// same rule as a host's (encrypted exports only).
type CredentialRef struct {
	Name     string  `json:"name"`
	Username string  `json:"username"`
	AuthType string  `json:"authType"`
	Password *string `json:"password,omitempty"`
	KeyRef   *KeyRef `json:"sshKeyRef,omitempty"`
}
type File struct {
	PlainCredentials    bool    `json:"-"`
	Format              string  `json:"format"`
	Version             int     `json:"version"`
	ExportedAt          string  `json:"exportedAt"`
	PasswordsOmitted    bool    `json:"passwordsOmitted,omitempty"`
	PasswordsObfuscated bool    `json:"passwordsObfuscated,omitempty"`
	Connections         []Entry `json:"connections"`
}

func Pretty(v any) ([]byte, error) {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	e.SetIndent("", "  ")
	if err := e.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(b.Bytes(), []byte("\n")), nil
}
func EncryptPayload(plain []byte, password string) (string, error) {
	salt, e := security.Random(32)
	if e != nil {
		return "", e
	}
	nonce, e := security.Random(12)
	if e != nil {
		return "", e
	}
	return EncryptPayloadFixed(plain, password, salt, nonce)
}

// EncryptPayloadFixed exists for first-party interoperability vectors. Never reuse nonces in production.
func EncryptPayloadFixed(plain []byte, password string, salt, nonce []byte) (string, error) {
	if len(salt) != 32 || len(nonce) != 12 {
		return "", errors.New("invalid export salt/nonce")
	}
	key, e := security.Derive(password, salt)
	if e != nil {
		return "", e
	}
	ct, tag, e := security.SealWithNonce(plain, key, nonce, ExportAAD)
	if e != nil {
		return "", e
	}
	out := append(append(append(append([]byte{}, salt...), nonce...), tag...), ct...)
	return base64.StdEncoding.EncodeToString(out), nil
}
func DecryptPayload(payload, password string) ([]byte, error) {
	b, e := base64.StdEncoding.Strict().DecodeString(payload)
	if e != nil || len(b) < 60 {
		return nil, errors.New("encrypted payload too short or invalid base64")
	}
	key, e := security.Derive(password, b[:32])
	if e != nil {
		return nil, e
	}
	return security.Open(b[60:], b[44:60], key, b[32:44], ExportAAD)
}
func Encode(entries []Entry, password *string, at time.Time) ([]byte, error) {
	copyEntries := append([]Entry{}, entries...)
	for i := range copyEntries {
		if password == nil {
			copyEntries[i].Password = nil
		}
		if cr := copyEntries[i].Credential; cr != nil && password == nil {
			v := *cr
			v.Password = nil
			copyEntries[i].Credential = &v
		}
		if copyEntries[i].Tags == nil {
			copyEntries[i].Tags = []string{}
		}
	}
	f := File{Format: Format, Version: 1, ExportedAt: at.UTC().Format("2006-01-02T15:04:05.000Z"), PasswordsOmitted: password == nil, Connections: copyEntries}
	plain, e := Pretty(f)
	if e != nil || password == nil {
		return plain, e
	}
	v, e := EncryptPayload(plain, *password)
	return []byte("b64##" + v), e
}
func Decode(raw []byte, password *string) (File, error) {
	var out File
	raw = bytes.TrimSpace(bytes.TrimPrefix(bytes.TrimSpace(raw), []byte{0xef, 0xbb, 0xbf}))
	if len(raw) > 64<<20 {
		return out, errors.New("import exceeds 64 MiB")
	}
	encrypted := bytes.HasPrefix(raw, []byte("b64##"))
	if encrypted {
		if password == nil {
			return out, errors.New("encrypted export requires password")
		}
		plain, e := DecryptPayload(string(raw[5:]), *password)
		if e != nil {
			return out, fmt.Errorf("export decrypt: %w", e)
		}
		raw = plain
	}
	if e := json.Unmarshal(raw, &out); e != nil {
		return out, e
	}
	if out.Format != Format || out.Version != 1 {
		return out, errors.New("unsupported NextShell export format/version")
	}
	if out.PasswordsObfuscated {
		return out, errors.New("legacy XOR export intentionally unsupported; re-export encrypted using 1.x")
	}
	if len(out.Connections) > 100000 {
		return out, errors.New("too many connections")
	}
	for i, v := range out.Connections {
		if !encrypted && (v.Password != nil || v.Credential != nil && v.Credential.Password != nil) {
			out.PlainCredentials = true
		}
		if cr := v.Credential; cr != nil {
			if cr.Name == "" || cr.Username == "" || out.PasswordsOmitted && cr.Password != nil {
				return out, errors.New("invalid credential in export")
			}
			switch cr.AuthType {
			case "password", "privateKey", "agent", "interactive":
			default:
				return out, errors.New("invalid credential authType")
			}
		}
		if out.PasswordsOmitted && v.Password != nil {
			return out, errors.New("passwordsOmitted contradicts a supplied password")
		}
		if (v.AuthType == "password" || v.AuthType == "interactive") && v.Password == nil {
			out.Connections[i].PasswordUnavailable = true
		}
		if v.Name == "" || v.Host == "" || v.Username == "" || v.Port < 1 || v.Port > 65535 {
			return out, errors.New("invalid connection in export")
		}
		switch v.AuthType {
		case "password", "privateKey", "agent", "interactive":
		default:
			return out, errors.New("invalid authType")
		}
	}
	if len(out.Connections) == 0 {
		return out, errors.New("empty connection export")
	}
	return out, nil
}
func ToEntry(c model.Connection, folder string, key *model.Key) Entry {
	auth := map[string]string{"key": "privateKey", "keyboard": "interactive"}[c.Auth]
	if auth == "" {
		auth = c.Auth
	}
	e := Entry{Name: c.Name, Host: c.Host, Port: c.Port, Username: c.User, AuthType: auth, GroupPath: folder, Tags: c.Tags, Notes: c.Notes, Favorite: c.Favorite, Encoding: c.Encoding, Backspace: c.Backspace, Delete: c.DeleteMode, Monitor: c.Monitor}
	if e.GroupPath == "" {
		e.GroupPath = "/server"
	}
	e.Encoding = strings.ToLower(e.Encoding)
	if e.Encoding == "utf8" {
		e.Encoding = "utf-8"
	}
	if e.Encoding == "" {
		e.Encoding = "utf-8"
	}
	if e.Backspace == "" {
		e.Backspace = "ascii-backspace"
	}
	if e.Delete == "" {
		e.Delete = "vt220-delete"
	}
	e.KeepAliveEnabled = c.KeepaliveEnabled
	e.KeepAliveIntervalSec = c.KeepaliveIntervalSec
	if e.KeepAliveEnabled == nil && c.Keepalive > 0 {
		enabled := true
		interval := c.Keepalive
		e.KeepAliveEnabled = &enabled
		e.KeepAliveIntervalSec = &interval
	}
	if c.NeedsKeyRebind && c.OriginalAuth == "key" {
		e.AuthType = "privateKey"
		if c.SourceKeyRef != nil {
			e.KeyRef = &KeyRef{Name: c.SourceKeyRef.Name, Fingerprint: c.SourceKeyRef.Fingerprint}
		}
	}
	if e.AuthType == "password" || e.AuthType == "interactive" {
		pass := c.Secret
		e.Password = &pass
	}
	if key != nil {
		e.KeyRef = keyRef(*key)
	}
	return e
}
func keyRef(key model.Key) *KeyRef {
	ref := &KeyRef{Name: key.Name}
	if pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(key.Public)); err == nil {
		ref.Fingerprint = ssh.FingerprintSHA256(pub)
	}
	return ref
}

// ToCredential is cr as an export carries it; key is the key it refers to, if any.
func ToCredential(cr model.Credential, key *model.Key) *CredentialRef {
	auth := map[string]string{"key": "privateKey", "keyboard": "interactive"}[cr.Auth]
	if auth == "" {
		auth = cr.Auth
	}
	out := &CredentialRef{Name: cr.Name, Username: cr.User, AuthType: auth}
	if auth == "password" || auth == "interactive" {
		pass := cr.Secret
		out.Password = &pass
	}
	if key != nil {
		out.KeyRef = keyRef(*key)
	}
	return out
}

// FromCredential is the credential an import creates from r, with the key matched among keys; ok is
// false when it names a key that is not there.
func FromCredential(r CredentialRef, keys []model.Key) (cr model.Credential, ok bool) {
	cr = model.Credential{Name: r.Name, User: r.Username, Auth: r.AuthType}
	switch cr.Auth {
	case "privateKey":
		cr.Auth = "key"
	case "interactive":
		cr.Auth = "keyboard"
	}
	if r.Password != nil {
		cr.Secret = *r.Password
	}
	if cr.Auth == "key" {
		cr.KeyID = MatchKey(r.KeyRef, keys)
	}
	return cr, cr.Validate() == nil
}
func FromEntry(e Entry, keys []model.Key) model.Connection {
	c := model.Connection{KeepaliveEnabled: e.KeepAliveEnabled, KeepaliveIntervalSec: e.KeepAliveIntervalSec, Name: e.Name, Host: e.Host, Port: e.Port, User: e.Username, Auth: e.AuthType, Tags: e.Tags, Notes: e.Notes, Favorite: e.Favorite, Encoding: e.Encoding, Backspace: e.Backspace, DeleteMode: e.Delete, Monitor: e.Monitor || !e.Has("monitorSession"), NeedsKeyRebind: e.NeedsKeyRebind}
	if e.Password != nil {
		c.Secret = *e.Password
	}
	if e.KeepAliveEnabled != nil && *e.KeepAliveEnabled {
		c.Keepalive = 30
		if e.KeepAliveIntervalSec != nil {
			c.Keepalive = *e.KeepAliveIntervalSec
		}
	}
	switch c.Auth {
	case "privateKey":
		c.Auth = "key"
	case "interactive":
		c.Auth = "keyboard"
	}
	if c.Auth == "key" || e.KeyRef != nil {
		c.KeyID = MatchKey(e.KeyRef, keys)
		if c.KeyID == "" {
			// Stays key auth, marked as missing its key, until a key is imported or chosen.
			c.Auth = "key"
			c.NeedsKeyRebind = true
			c.OriginalAuth = "key"
			if e.KeyRef != nil {
				c.SourceKeyRef = &model.KeyReference{Name: e.KeyRef.Name, Fingerprint: e.KeyRef.Fingerprint}
			}
		} else {
			c.Auth = "key"
		}
	}
	if c.Encoding == "" {
		c.Encoding = "utf-8"
	}
	return c
}
func MatchKey(ref *KeyRef, keys []model.Key) string {
	if ref == nil {
		return ""
	}
	if ref.Fingerprint != "" {
		for _, k := range keys {
			pub, _, _, _, e := ssh.ParseAuthorizedKey([]byte(k.Public))
			if e == nil && ssh.FingerprintSHA256(pub) == ref.Fingerprint {
				return k.ID
			}
		}
		return ""
	}
	var ids []string
	for _, k := range keys {
		if k.Name == ref.Name {
			ids = append(ids, k.ID)
		}
	}
	if len(ids) == 1 {
		return ids[0]
	}
	return ""
}
func Segments(legacy string) []string {
	var out []string
	for _, s := range strings.Split(legacy, "/") {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	if len(out) > 0 {
		if out[0] == "server" || out[0] == "import" {
			return out[1:]
		}
		if out[0] == "workspace" {
			return out[min(2, len(out)):]
		}
	}
	return out
}

func (e *Entry) UnmarshalJSON(raw []byte) error {
	type wire Entry
	var w wire
	if err := json.Unmarshal(raw, &w); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	w.Present = map[string]bool{}
	for k, v := range fields {
		if bytes.Equal(bytes.TrimSpace(v), []byte("null")) && (k == "password" || k == "keepAliveEnabled" || k == "keepAliveIntervalSec") {
			return fmt.Errorf("null %s is not an omitted field", k)
		}
		w.Present[k] = true
	}
	*e = Entry(w)
	return nil
}
func (e Entry) Has(field string) bool {
	if e.Present == nil {
		return true
	}
	return e.Present[field]
}

// MergeEntry preserves target identity and fields absent from the interchange contract.
func MergeEntry(old model.Connection, e Entry, keys []model.Key) model.Connection {
	previousKey := old.KeyID
	previousAuth := old.Auth
	fresh := FromEntry(e, keys)
	old.Name = fresh.Name
	old.Host = fresh.Host
	old.Port = fresh.Port
	old.User = fresh.User
	old.Auth = fresh.Auth
	old.KeyID = fresh.KeyID
	old.NeedsKeyRebind = fresh.NeedsKeyRebind
	old.OriginalAuth = fresh.OriginalAuth
	old.SourceKeyRef = fresh.SourceKeyRef
	if e.AuthType == "privateKey" && e.KeyRef == nil && previousAuth == "key" && previousKey != "" {
		old.Auth = "key"
		old.KeyID = previousKey
		old.NeedsKeyRebind = false
		old.OriginalAuth = ""
		old.SourceKeyRef = nil
	}
	if e.Password != nil {
		old.Secret = *e.Password
	}
	if e.Has("tags") {
		old.Tags = fresh.Tags
	}
	if e.Has("notes") {
		old.Notes = fresh.Notes
	}
	if e.Has("favorite") {
		old.Favorite = fresh.Favorite
	}
	if e.Has("terminalEncoding") {
		old.Encoding = fresh.Encoding
	}
	if e.Has("backspaceMode") {
		old.Backspace = fresh.Backspace
	}
	if e.Has("deleteMode") {
		old.DeleteMode = fresh.DeleteMode
	}
	if e.Has("monitorSession") {
		old.Monitor = fresh.Monitor
	}
	if e.KeepAliveEnabled != nil {
		old.KeepaliveEnabled = e.KeepAliveEnabled
	}
	if e.KeepAliveIntervalSec != nil {
		old.KeepaliveIntervalSec = e.KeepAliveIntervalSec
	}
	if old.KeepaliveEnabled != nil {
		old.Keepalive = 0
		if *old.KeepaliveEnabled {
			old.Keepalive = 30
			if old.KeepaliveIntervalSec != nil {
				old.Keepalive = *old.KeepaliveIntervalSec
			}
		}
	}
	return old
}
