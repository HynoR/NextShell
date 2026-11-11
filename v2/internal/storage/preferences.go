package storage

import (
	"database/sql"
	"encoding/json"
	"errors"
	"nextshell.dev/v2/internal/model"
)

// Preferences overlays newly added defaults and never reads v1 databases/keychains.
func (s *Store) Preferences() (model.Preferences, error) {
	p := model.DefaultPreferences()
	var raw string
	err := s.db.QueryRow(`SELECT value FROM app_settings WHERE key='preferences_v2'`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	if err = json.Unmarshal([]byte(raw), &p); err != nil {
		return p, err
	}
	return p, p.Validate()
}
func (s *Store) SavePreferences(p model.Preferences) error {
	if err := p.Validate(); err != nil {
		return err
	}
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO app_settings(key,value) VALUES('preferences_v2',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, string(b))
	return err
}
