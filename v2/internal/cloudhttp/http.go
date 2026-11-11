// Package cloudhttp implements loopback-only HTTP transport for the new cloud-v2 protocol.
// There is deliberately no legacy endpoint, password authentication, or external-service connection.
package cloudhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"nextshell.dev/v2/internal/cloudv2"
	"strings"
	"sync"
	"time"
)

var ErrConflict = errors.New("cloud head conflict")
var ErrNotFound = errors.New("cloud workspace not found")

type History struct {
	Version int               `json:"version"`
	Head    string            `json:"head"`
	Commits []json.RawMessage `json:"commits"`
}
type Client struct {
	base, bootstrap string
	http            *http.Client
	mu              sync.Mutex
	access          string
}

func New(base, token string, transport http.RoundTripper) (*Client, error) {
	u, e := url.Parse(base)
	if e != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("invalid loopback cloud URL")
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsLoopback() {
		return nil, errors.New("only literal loopback cloud addresses allowed in this phase")
	}
	if _, e = cloudv2.AuthVerifier(token); e != nil {
		return nil, e
	}
	if transport == nil {
		t := http.DefaultTransport.(*http.Transport).Clone()
		t.Proxy = nil
		transport = t
	}
	return &Client{base: strings.TrimSuffix(base, "/"), bootstrap: token, http: &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func decode(raw []byte, v any) error {
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
func (c *Client) request(ctx context.Context, method, path, token string, body []byte) ([]byte, int, error) {
	req, e := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(body))
	if e != nil {
		return nil, 0, e
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	res, e := c.http.Do(req)
	if e != nil {
		return nil, 0, errors.New("cloud transport unavailable")
	}
	defer res.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(res.Body, cloudv2.MaxPacket+1))
	if e != nil || len(raw) > cloudv2.MaxPacket {
		return nil, res.StatusCode, errors.New("cloud response exceeds limit or is incomplete")
	}
	if res.StatusCode == 409 {
		return raw, 409, ErrConflict
	}
	if res.StatusCode == 404 {
		return nil, 404, ErrNotFound
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, res.StatusCode, errors.New("cloud request rejected")
	}
	return raw, res.StatusCode, nil
}
func (c *Client) Login(ctx context.Context) error {
	raw, _, e := c.request(ctx, "POST", "/v2/login", c.bootstrap, nil)
	if e != nil {
		return e
	}
	var result struct {
		Version     int    `json:"version"`
		AccessToken string `json:"accessToken"`
		ExpiresAt   string `json:"expiresAt"`
	}
	if e = decode(raw, &result); e != nil {
		return e
	}
	if result.Version != cloudv2.Version {
		return errors.New("unsupported cloud login protocol")
	}
	if _, e = cloudv2.AuthVerifier(result.AccessToken); e != nil {
		return e
	}
	c.mu.Lock()
	c.access = result.AccessToken
	c.mu.Unlock()
	return nil
}
func (c *Client) call(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	c.mu.Lock()
	token := c.access
	c.mu.Unlock()
	if token == "" {
		if e := c.Login(ctx); e != nil {
			return nil, e
		}
		c.mu.Lock()
		token = c.access
		c.mu.Unlock()
	}
	raw, status, e := c.request(ctx, method, path, token, body)
	if status == 401 {
		if e = c.Login(ctx); e != nil {
			return nil, e
		}
		c.mu.Lock()
		token = c.access
		c.mu.Unlock()
		raw, _, e = c.request(ctx, method, path, token, body)
	}
	return raw, e
}
func (c *Client) History(ctx context.Context, id, after string) (History, error) {
	var h History
	if !cloudv2.ValidID(id) || (after != "" && !cloudv2.ValidID(after)) {
		return h, errors.New("invalid workspace/head")
	}
	raw, e := c.call(ctx, "GET", "/v2/workspaces/"+id+"?after="+url.QueryEscape(after), nil)
	if e != nil {
		return h, e
	}
	if e = decode(raw, &h); e != nil {
		return h, e
	}
	if h.Version != cloudv2.Version || (h.Head != "" && !cloudv2.ValidID(h.Head)) || len(h.Commits) > 256 {
		return h, errors.New("invalid cloud history")
	}
	parent := after
	for _, raw := range h.Commits {
		commit, e := cloudv2.DecodeCommit(raw)
		if e != nil || commit.WorkspaceID != id || commit.Parent != parent {
			return h, errors.New("broken cloud history chain")
		}
		parent = commit.ID
	}
	if parent != h.Head {
		return h, errors.New("history head mismatch")
	}
	return h, nil
}
func (c *Client) Key(ctx context.Context, id string) (cloudv2.WrappedKey, error) {
	if !cloudv2.ValidID(id) {
		return cloudv2.WrappedKey{}, errors.New("invalid workspace ID")
	}
	raw, e := c.call(ctx, "GET", "/v2/workspaces/"+id+"/key", nil)
	if e != nil {
		return cloudv2.WrappedKey{}, e
	}
	w, e := cloudv2.DecodeWrappedKey(raw)
	if e == nil && w.WorkspaceID != id {
		e = errors.New("wrapped key workspace mismatch")
	}
	return w, e
}
func (c *Client) Push(ctx context.Context, commit cloudv2.Commit, key cloudv2.WrappedKey) error {
	if e := commit.Validate(); e != nil {
		return e
	}
	if e := key.Validate(); e != nil {
		return e
	}
	if commit.KeyID != key.KeyID || commit.WorkspaceID != key.WorkspaceID {
		return errors.New("key identity mismatch")
	}
	raw, e := json.Marshal(struct {
		Version int                `json:"version"`
		Commit  cloudv2.Commit     `json:"commit"`
		Wrapped cloudv2.WrappedKey `json:"wrapped"`
	}{cloudv2.Version, commit, key})
	if e != nil || len(raw) > cloudv2.MaxPacket {
		return errors.New("publish too large")
	}
	raw, e = c.call(ctx, "POST", "/v2/workspaces/"+commit.WorkspaceID, raw)
	if e != nil {
		return e
	}
	var ack struct {
		Version int    `json:"version"`
		Head    string `json:"head"`
	}
	if e = decode(raw, &ack); e != nil {
		return e
	}
	if ack.Version != cloudv2.Version || ack.Head != commit.ID {
		return errors.New("invalid publish acknowledgement")
	}
	return nil
}
