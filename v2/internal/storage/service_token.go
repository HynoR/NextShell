package storage

import (
	"database/sql"
	"errors"
	"nextshell.dev/v2/internal/cloudv2"
	"nextshell.dev/v2/internal/security"
)

// MCPToken is generated only on explicit enable. It is separate from all cloud credentials.
func (s *Store) MCPToken(rotate bool) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var env []byte
	e := s.db.QueryRow("SELECT envelope FROM secrets WHERE id='service:mcp'").Scan(&env)
	if e == nil && !rotate {
		raw, e := security.Unseal(env, s.key, "nextshell-service:mcp")
		return string(raw), e
	}
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return "", e
	}
	token, e := cloudv2.NewAuthToken()
	if e != nil {
		return "", e
	}
	env, e = security.Seal([]byte(token), s.key, "nextshell-service:mcp")
	if e != nil {
		return "", e
	}
	_, e = s.db.Exec("INSERT INTO secrets(id,envelope) VALUES('service:mcp',?) ON CONFLICT(id) DO UPDATE SET envelope=excluded.envelope", env)
	return token, e
}
