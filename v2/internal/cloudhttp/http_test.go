package cloudhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"nextshell.dev/v2/internal/cloudv2"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fixtureHTTP(t *testing.T) (*Client, *ReferenceServer, string) {
	t.Helper()
	token, _ := cloudv2.NewAuthToken()
	ref, e := NewReference(token)
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(ref)
	t.Cleanup(server.Close)
	client, e := New(server.URL, token, nil)
	if e != nil {
		t.Fatal(e)
	}
	return client, ref, token
}
func TestLoopbackOnlyCloudAddresses(t *testing.T) {
	token, _ := cloudv2.NewAuthToken()
	for _, u := range []string{"http://example.com", "https://192.0.2.1", "http://localhost:80", "http://127.0.0.1/x", "http://user@127.0.0.1", "http://127.0.0.1?x=1", "file:///tmp/x", "http://127.0.0.1/#x"} {
		t.Run(u, func(t *testing.T) {
			if _, e := New(u, token, nil); e == nil {
				t.Fatal("accepted", u)
			}
		})
	}
	if _, e := New("http://[::1]:1234", token, nil); e != nil {
		t.Fatal(e)
	}
}
func TestHTTPLoginCASRefreshAndOpaqueHistory(t *testing.T) {
	client, ref, token := fixtureHTTP(t)
	ctx := context.Background()
	id := cloudv2.NewID()
	wrapped, key, e := cloudv2.NewKey("independent encryption password", id)
	if e != nil {
		t.Fatal(e)
	}
	snapshot := cloudv2.Snapshot{Version: 2, WorkspaceID: id, Name: "private-workspace-name"}
	first, e := cloudv2.Seal(snapshot, wrapped.KeyID, "", key)
	if e != nil {
		t.Fatal(e)
	}
	if e = client.Push(ctx, first, wrapped); e != nil {
		t.Fatal(e)
	}
	history, e := client.History(ctx, id, "")
	if e != nil || len(history.Commits) != 1 || history.Head != first.ID {
		t.Fatal(history, e)
	}
	raw, _ := json.Marshal(history)
	if bytes.Contains(raw, []byte(snapshot.Name)) || bytes.Contains(raw, []byte(token)) || bytes.Contains(raw, key) {
		t.Fatal("reference server saw plaintext material")
	}
	received, e := client.Key(ctx, id)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = cloudv2.Unlock("wrong password", received); e == nil {
		t.Fatal("authentication grants decryption")
	}
	if e = client.Push(ctx, first, wrapped); !errors.Is(e, ErrConflict) {
		t.Fatal("replay accepted", e)
	}
	other, _ := cloudv2.Seal(snapshot, wrapped.KeyID, "", key)
	if e = client.Push(ctx, other, wrapped); !errors.Is(e, ErrConflict) {
		t.Fatal("CAS accepted stale head", e)
	}
	ref.mu.Lock()
	for token := range ref.sessions {
		ref.sessions[token] = time.Now().Add(-time.Hour)
	}
	ref.mu.Unlock()
	h, e := client.History(ctx, id, first.ID)
	if e != nil || len(h.Commits) != 0 {
		t.Fatal("expired session not refreshed", e)
	}
	if _, e = client.History(ctx, id, cloudv2.NewID()); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	if _, e = client.Key(ctx, cloudv2.NewID()); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	wrong, _ := cloudv2.NewAuthToken()
	bad, _ := New(client.base, wrong, nil)
	if e = bad.Login(ctx); e == nil {
		t.Fatal("wrong authentication accepted")
	}
}
func TestCloudRejectsRedirectsVersionsAndBadAcknowledgement(t *testing.T) {
	token, _ := cloudv2.NewAuthToken()
	var hits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer redirect.Close()
	client, _ := New(redirect.URL, token, nil)
	if e := client.Login(context.Background()); e == nil || hits.Load() != 0 {
		t.Fatal("redirect followed", e)
	}
	for _, kind := range []string{"login-version", "history-chain", "ack-head", "origin"} {
		t.Run(kind, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v2/login" {
					version := 2
					if kind == "login-version" {
						version = 1
					}
					json.NewEncoder(w).Encode(map[string]any{"version": version, "accessToken": token, "expiresAt": "unused"})
					return
				}
				if r.Method == "GET" {
					json.NewEncoder(w).Encode(History{Version: 2, Head: cloudv2.NewID()})
					return
				}
				json.NewEncoder(w).Encode(map[string]any{"version": 2, "head": cloudv2.NewID()})
			}))
			defer server.Close()
			c, _ := New(server.URL, token, nil)
			ctx := context.Background()
			if kind == "login-version" {
				if e := c.Login(ctx); e == nil {
					t.Fatal("old protocol accepted")
				}
				return
			}
			if kind == "history-chain" {
				if _, e := c.History(ctx, cloudv2.NewID(), ""); e == nil {
					t.Fatal("bad chain accepted")
				}
				return
			}
			if kind == "origin" {
				_, ref, tok := fixtureHTTP(t)
				request := httptest.NewRequest("POST", "http://127.0.0.1/v2/login", nil)
				request.RemoteAddr = "127.0.0.1:1"
				request.Header.Set("Authorization", "Bearer "+tok)
				request.Header.Set("Origin", "http://127.0.0.1")
				rec := httptest.NewRecorder()
				ref.ServeHTTP(rec, request)
				if rec.Code != 403 {
					t.Fatal(rec.Code)
				}
				return
			}
			id := cloudv2.NewID()
			w, k, _ := cloudv2.NewKey("strong independent encryption password", id)
			commit, _ := cloudv2.Seal(cloudv2.Snapshot{Version: 2, WorkspaceID: id, Name: "test"}, w.KeyID, "", k)
			if e := c.Push(ctx, commit, w); e == nil {
				t.Fatal("bad ack accepted")
			}
		})
	}
}
func TestCloudRequestCancellationAndStrictWire(t *testing.T) {
	token, _ := cloudv2.NewAuthToken()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.Copy(io.Discard, r.Body); <-r.Context().Done() }))
	defer server.Close()
	client, _ := New(server.URL, token, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if e := client.Login(ctx); e == nil {
		t.Fatal("canceled login succeeded")
	}
	var out struct {
		Version int `json:"version"`
	}
	for _, raw := range []string{`{"version":2,"unknown":1}`, `{"version":2} {}`, `not-json`} {
		if e := decode([]byte(raw), &out); e == nil {
			t.Fatal("invalid wire accepted", raw)
		}
	}
	if _, e := New("http://127.0.0.1", strings.Repeat("bad", 10), nil); e == nil {
		t.Fatal("invalid token accepted")
	}
}
