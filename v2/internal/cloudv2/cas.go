package cloudv2

import (
	"encoding/json"
	"errors"
	"sync"
)

// MemoryServer is an OFFLINE opaque-CAS contract fixture, NOT a network/server implementation.
// The server only sees routing IDs, wrapped keys, authentication verifier, and ciphertext.
type MemoryServer struct {
	mu       sync.Mutex
	verifier []byte
	heads    map[string]string
	commits  map[string]Commit
	keys     map[string]WrappedKey
}

func NewMemoryServer(verifier []byte) *MemoryServer {
	return &MemoryServer{verifier: append([]byte{}, verifier...), heads: map[string]string{}, commits: map[string]Commit{}, keys: map[string]WrappedKey{}}
}
func (m *MemoryServer) Accept(token string, c Commit, w WrappedKey) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !VerifyAuth(token, m.verifier) {
		return "", errors.New("unauthorized")
	}
	if e := c.Validate(); e != nil {
		return "", e
	}
	if e := w.validate(); e != nil {
		return "", e
	}
	if w.WorkspaceID != c.WorkspaceID || w.KeyID != c.KeyID {
		return "", errors.New("wrapped key mismatch")
	}
	head := m.heads[c.WorkspaceID]
	if c.Parent != head {
		return head, errors.New("diverged")
	}
	if _, exists := m.commits[c.ID]; exists {
		return head, errors.New("commit ID reused")
	}
	b, _ := json.Marshal(c)
	var copy Commit
	json.Unmarshal(b, &copy)
	m.commits[c.ID] = copy
	b, _ = json.Marshal(w)
	var wk WrappedKey
	json.Unmarshal(b, &wk)
	m.keys[c.WorkspaceID] = wk
	m.heads[c.WorkspaceID] = c.ID
	return c.ID, nil
}
func (m *MemoryServer) Read(token, workspace string) (Commit, WrappedKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !VerifyAuth(token, m.verifier) {
		return Commit{}, WrappedKey{}, errors.New("unauthorized")
	}
	id := m.heads[workspace]
	if id == "" {
		return Commit{}, WrappedKey{}, errors.New("not found")
	}
	b, _ := json.Marshal(m.commits[id])
	var c Commit
	json.Unmarshal(b, &c)
	b, _ = json.Marshal(m.keys[workspace])
	var w WrappedKey
	json.Unmarshal(b, &w)
	return c, w, nil
}
