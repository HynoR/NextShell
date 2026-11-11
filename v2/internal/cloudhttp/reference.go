package cloudhttp

import (
	"crypto/sha256"
	"encoding/json"
	"io"
	"net/http"
	"nextshell.dev/v2/internal/cloudv2"
	"strings"
	"sync"
	"time"
)

// ReferenceServer is a bounded, single-account, IN-MEMORY TEST/REFERENCE implementation.
// It is NOT a production service and has no persisted accounts, memberships or recovery.
type ReferenceServer struct {
	mu       sync.Mutex
	verifier []byte
	sessions map[[32]byte]time.Time
	heads    map[string]string
	commits  map[string]cloudv2.Commit
	keys     map[string]cloudv2.WrappedKey
	now      func() time.Time
}

func NewReference(token string) (*ReferenceServer, error) {
	verifier, e := cloudv2.AuthVerifier(token)
	if e != nil {
		return nil, e
	}
	return &ReferenceServer{verifier: verifier, sessions: map[[32]byte]time.Time{}, heads: map[string]string{}, commits: map[string]cloudv2.Commit{}, keys: map[string]cloudv2.WrappedKey{}, now: time.Now}, nil
}
func (s *ReferenceServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if r.Header.Get("Origin") != "" {
		http.Error(w, "browser origins not supported", 403)
		return
	}
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		http.Error(w, "unauthorized", 401)
		return
	}
	if r.URL.Path == "/v2/login" && r.Method == "POST" {
		if !cloudv2.VerifyAuth(token, s.verifier) {
			http.Error(w, "unauthorized", 401)
			return
		}
		for k, expiry := range s.sessions {
			if !expiry.After(s.now()) {
				delete(s.sessions, k)
			}
		}
		if len(s.sessions) >= 128 {
			http.Error(w, "session capacity", 503)
			return
		}
		access, e := cloudv2.NewAuthToken()
		if e != nil {
			http.Error(w, "unavailable", 500)
			return
		}
		expiry := s.now().Add(15 * time.Minute)
		s.sessions[sha256.Sum256([]byte(access))] = expiry
		json.NewEncoder(w).Encode(map[string]any{"version": 2, "accessToken": access, "expiresAt": expiry.UTC().Format(time.RFC3339)})
		return
	}
	if expiry, ok := s.sessions[sha256.Sum256([]byte(token))]; !ok || !expiry.After(s.now()) {
		http.Error(w, "unauthorized", 401)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/v2/workspaces/")
	if path == r.URL.Path {
		http.NotFound(w, r)
		return
	}
	parts := strings.Split(path, "/")
	id := parts[0]
	if !cloudv2.ValidID(id) || len(parts) > 2 {
		http.NotFound(w, r)
		return
	}
	if len(parts) == 2 {
		if parts[1] != "key" || r.Method != "GET" {
			http.NotFound(w, r)
			return
		}
		key, ok := s.keys[id]
		if !ok {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(key)
		return
	}
	switch r.Method {
	case "GET":
		head := s.heads[id]
		after := r.URL.Query().Get("after")
		if after != "" && !cloudv2.ValidID(after) {
			http.Error(w, "invalid head", 400)
			return
		}
		if head == "" {
			if after != "" {
				http.Error(w, "diverged", 409)
				return
			}
			json.NewEncoder(w).Encode(History{Version: 2})
			return
		}
		var reversed []cloudv2.Commit
		for cursor := head; cursor != after; {
			commit, ok := s.commits[cursor]
			if !ok || commit.WorkspaceID != id {
				http.Error(w, "diverged", 409)
				return
			}
			reversed = append(reversed, commit)
			if len(reversed) > 256 {
				http.Error(w, "history capacity", 413)
				return
			}
			cursor = commit.Parent
			if cursor == "" && after != "" {
				http.Error(w, "diverged", 409)
				return
			}
		}
		h := History{Version: 2, Head: head}
		for i := len(reversed) - 1; i >= 0; i-- {
			raw, _ := cloudv2.EncodeCommit(reversed[i])
			h.Commits = append(h.Commits, raw)
		}
		raw, _ := json.Marshal(h)
		if len(raw) > cloudv2.MaxPacket {
			http.Error(w, "history too large", 413)
			return
		}
		w.Write(raw)
	case "POST":
		body, e := io.ReadAll(io.LimitReader(r.Body, cloudv2.MaxPacket+1))
		if e != nil || len(body) > cloudv2.MaxPacket {
			http.Error(w, "body too large", 413)
			return
		}
		var publish struct {
			Version int             `json:"version"`
			Commit  json.RawMessage `json:"commit"`
			Wrapped json.RawMessage `json:"wrapped"`
		}
		if decode(body, &publish) != nil || publish.Version != 2 {
			http.Error(w, "invalid protocol", 400)
			return
		}
		commit, e := cloudv2.DecodeCommit(publish.Commit)
		if e != nil {
			http.Error(w, "invalid commit", 400)
			return
		}
		key, e := cloudv2.DecodeWrappedKey(publish.Wrapped)
		if e != nil || commit.WorkspaceID != id || key.WorkspaceID != id || key.KeyID != commit.KeyID {
			http.Error(w, "invalid key", 400)
			return
		}
		if commit.Parent != s.heads[id] {
			http.Error(w, "diverged", 409)
			return
		}
		if _, ok := s.commits[commit.ID]; ok {
			http.Error(w, "reused commit", 409)
			return
		}
		if len(s.commits) >= 256 {
			http.Error(w, "reference history capacity", 507)
			return
		}
		s.commits[commit.ID] = commit
		s.heads[id] = commit.ID
		s.keys[id] = key
		json.NewEncoder(w).Encode(map[string]any{"version": 2, "head": commit.ID})
	default:
		w.WriteHeader(405)
	}
}
