// Package model contains UI-independent application records.
package model

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

type Connection struct {
	Tags           []string `json:"tags,omitempty"`
	Notes          string   `json:"notes,omitempty"`
	Favorite       bool     `json:"favorite,omitempty"`
	Backspace      string   `json:"backspaceMode,omitempty"`
	DeleteMode     string   `json:"deleteMode,omitempty"`
	Monitor        bool     `json:"monitorSession,omitempty"`
	NeedsKeyRebind bool     `json:"needsKeyRebind,omitempty"`
	OriginalAuth   string   `json:"originalAuth,omitempty"`
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	FolderID       string   `json:"folderId,omitempty"`
	Host           string   `json:"host"`
	Port           int      `json:"port"`
	User           string   `json:"user"`
	Auth           string   `json:"auth"` // password, key, agent, keyboard
	Secret         string   `json:"-"`
	KeyID          string   `json:"keyId,omitempty"`
	ProxyID        string   `json:"proxyId,omitempty"`
	Fingerprint    string   `json:"fingerprint,omitempty"`
	Encoding       string   `json:"encoding"`
	Keepalive      int      `json:"keepalive"`
	Deleted        bool     `json:"deleted,omitempty"`
}

func (c Connection) Address() string { return net.JoinHostPort(c.Host, strconv.Itoa(c.Port)) }
func (c Connection) Validate() error {
	if strings.TrimSpace(c.Name) == "" || strings.TrimSpace(c.Host) == "" || strings.TrimSpace(c.User) == "" {
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
	if c.Auth == "key" && c.KeyID == "" {
		return errors.New("请选择密钥")
	}
	switch strings.ToLower(c.Encoding) {
	case "", "utf-8", "utf8", "gb18030", "gbk", "big5", "shift_jis":
	default:
		return errors.New("不支持的编码")
	}
	if c.Keepalive < 0 || c.Keepalive > 3600 {
		return errors.New("keepalive 须为 0–3600 秒")
	}
	return nil
}

type Folder struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ParentID string `json:"parentId,omitempty"`
}
type Key struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Public     string `json:"public"`
	Private    string `json:"-"`
	Passphrase string `json:"-"`
}
type Proxy struct {
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
