// Package model contains UI-independent application records.
package model

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

const LocalScope = "local"

type Meta struct {
	Scope     string `json:"scope"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
	Revision  int64  `json:"revision"`
	DeletedAt string `json:"deletedAt,omitempty"`
}

func (m Meta) ScopeID() string {
	if m.Scope == "" {
		return LocalScope
	}
	return m.Scope
}

type KeyReference struct {
	Name        string `json:"name"`
	Fingerprint string `json:"fingerprint,omitempty"`
}
type Connection struct {
	UseKnownHosts bool          `json:"useKnownHosts,omitempty"`
	SourceKeyRef  *KeyReference `json:"sourceKeyRef,omitempty"`
	Meta
	StrictHostKeyChecking bool     `json:"strictHostKeyChecking"`
	KeepaliveEnabled      *bool    `json:"keepAliveEnabled,omitempty"`
	KeepaliveIntervalSec  *int     `json:"keepAliveIntervalSec,omitempty"`
	Tags                  []string `json:"tags,omitempty"`
	Notes                 string   `json:"notes,omitempty"`
	Favorite              bool     `json:"favorite,omitempty"`
	Backspace             string   `json:"backspaceMode,omitempty"`
	DeleteMode            string   `json:"deleteMode,omitempty"`
	Monitor               bool     `json:"monitorSession,omitempty"`
	NeedsKeyRebind        bool     `json:"needsKeyRebind,omitempty"`
	OriginalAuth          string   `json:"originalAuth,omitempty"`
	ID                    string   `json:"id"`
	Name                  string   `json:"name"`
	FolderID              string   `json:"folderId,omitempty"`
	Host                  string   `json:"host"`
	Port                  int      `json:"port"`
	User                  string   `json:"user"`
	Auth                  string   `json:"auth"` // password, key, agent, keyboard
	Secret                string   `json:"-"`
	KeyID                 string   `json:"keyId,omitempty"`
	ProxyID               string   `json:"proxyId,omitempty"`
	CredentialID          string   `json:"credentialId,omitempty"` // when set, user and auth come from the credential
	Fingerprint           string   `json:"fingerprint,omitempty"`
	Encoding              string   `json:"encoding"`
	Keepalive             int      `json:"keepalive"`
	Deleted               bool     `json:"deleted,omitempty"`
}

func (c Connection) Address() string { return net.JoinHostPort(c.Host, strconv.Itoa(c.Port)) }
func (c Connection) Validate() error {
	if strings.TrimSpace(c.Name) == "" || strings.TrimSpace(c.Host) == "" || c.CredentialID == "" && strings.TrimSpace(c.User) == "" {
		return errors.New("名称、主机和用户不能为空")
	}
	if c.Port < 1 || c.Port > 65535 {
		return errors.New("端口须为 1–65535")
	}
	switch c.Auth {
	case "password", "key", "agent", "keyboard":
	default:
		return errors.New("未知认证方式")
	}
	// An imported host whose key is not here yet stays key auth until one is bound (MissingKey).
	if c.Auth == "key" && c.KeyID == "" && !c.NeedsKeyRebind && c.CredentialID == "" {
		return errors.New("请选择密钥")
	}
	switch strings.ToLower(c.Encoding) {
	case "", "utf-8", "utf8", "gb18030", "gbk", "big5", "shift_jis":
	default:
		return errors.New("不支持的编码")
	}
	if c.KeepaliveIntervalSec != nil && (*c.KeepaliveIntervalSec < 0 || *c.KeepaliveIntervalSec > 3600 || (*c.KeepaliveIntervalSec == 0 && (c.KeepaliveEnabled == nil || *c.KeepaliveEnabled))) {
		return errors.New("keepalive interval 须为 1–3600 秒")
	}
	if c.Keepalive < 0 || c.Keepalive > 3600 {
		return errors.New("心跳间隔须为 0–3600 秒")
	}
	return nil
}

// WithCredential is c as it connects through cr: cr's user and authentication replace c's own.
func (c Connection) WithCredential(cr Credential) Connection {
	c.User, c.Auth, c.KeyID, c.Secret = cr.User, cr.Auth, cr.KeyID, cr.Secret
	if c.KeyID != "" {
		c.NeedsKeyRebind = false
	}
	return c
}

// BindKey makes c use key id, which ends a pending rebind.
func (c *Connection) BindKey(id string) {
	c.Auth, c.KeyID = "key", id
	c.NeedsKeyRebind, c.OriginalAuth, c.SourceKeyRef = false, "", nil
}

// ErrNoKey is a key-auth connection without a key to log in with.
var ErrNoKey = errors.New("未选择密钥：请为这个主机选择或导入密钥")

// Credential is a user name and a way to log in that hosts share: changing it changes every host using it.
type Credential struct {
	Meta
	ID     string `json:"id"`
	Name   string `json:"name"`
	User   string `json:"user"`
	Auth   string `json:"auth"` // password, key, agent, keyboard
	KeyID  string `json:"keyId,omitempty"`
	Secret string `json:"-"`
}

func (c Credential) Validate() error {
	if strings.TrimSpace(c.Name) == "" || strings.TrimSpace(c.User) == "" {
		return errors.New("凭据名称和用户名不能为空")
	}
	switch c.Auth {
	case "password", "agent", "keyboard":
	case "key":
		if c.KeyID == "" {
			return errors.New("请选择密钥")
		}
	default:
		return errors.New("未知认证方式")
	}
	return nil
}

// KeyExistsError is a key saved again: the same private key is already there as Key.
type KeyExistsError struct{ Key Key }

func (e *KeyExistsError) Error() string { return "密钥已存在：" + e.Key.Name }

type Folder struct {
	Meta
	SortIndex int    `json:"sortIndex"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	ParentID  string `json:"parentId,omitempty"`
}
type Key struct {
	Meta
	KeyType     string `json:"keyType,omitempty"`
	KeyBits     int    `json:"keyBits,omitempty"`
	KeyComment  string `json:"keyComment,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	Public      string `json:"public"`
	Private     string `json:"-"`
	Passphrase  string `json:"-"`
}
type Proxy struct {
	Meta
	ID       string `json:"id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user,omitempty"`
	Password string `json:"-"`
}

func (p Proxy) Validate() error {
	if p.Kind != "socks4" && p.Kind != "socks5" {
		return errors.New("代理类型须为 socks4 或 socks5")
	}
	if p.Name == "" || p.Host == "" || p.Port < 1 || p.Port > 65535 {
		return errors.New("代理名称、主机或端口无效")
	}
	return nil
}
func QuickConnect(s string) (Connection, error) {
	c := Connection{Name: s, Port: 22, Auth: "password", Encoding: "utf-8", Keepalive: 30}
	s = strings.TrimSpace(strings.TrimPrefix(s, "ssh://"))
	i := strings.LastIndex(s, "@")
	if i < 1 {
		return c, fmt.Errorf("快连格式为 user@host[:port]")
	}
	c.User = s[:i]
	a := s[i+1:]
	if h, p, e := net.SplitHostPort(a); e == nil {
		c.Host = h
		c.Port, e = strconv.Atoi(p)
		if e != nil {
			return c, e
		}
	} else {
		if strings.Contains(a, ":") && !strings.HasPrefix(a, "[") {
			return c, errors.New("IPv6 请写为 [地址]:端口")
		}
		c.Host = strings.Trim(a, "[]")
	}
	return c, c.Validate()
}

type Command struct {
	Meta
	AppendCR    *bool  `json:"appendCr,omitempty"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Group       string `json:"group"`
	Text        string `json:"command"`
	Template    bool   `json:"isTemplate,omitempty"`
}

// NormalizeFolderName keeps folder names representable in v1's slash-delimited export protocol.
func NormalizeFolderName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsAny(name, "/\\\x00") {
		return "", errors.New("目录名称不能为空或包含路径分隔符/NUL")
	}
	return name, nil
}

const DefaultKeepaliveSeconds = 30

// EffectiveKeepalive resolves wire tri-state at the Platform connection boundary only.
func (c Connection) EffectiveKeepalive() int {
	if c.KeepaliveEnabled != nil {
		if !*c.KeepaliveEnabled {
			return 0
		}
	} else if c.Keepalive > 0 {
		return c.Keepalive
	}
	if c.KeepaliveIntervalSec != nil && *c.KeepaliveIntervalSec > 0 {
		return *c.KeepaliveIntervalSec
	}
	return DefaultKeepaliveSeconds
}
