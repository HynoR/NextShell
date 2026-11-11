package storage

import (
	"nextshell.dev/v2/internal/model"
	"path/filepath"
	"testing"
)

func TestLocalPreferencesPersistence(t *testing.T) {
	file := filepath.Join(t.TempDir(), "prefs.db")
	s, e := Open(file)
	if e != nil {
		t.Fatal(e)
	}
	p, e := s.Preferences()
	if e != nil || p != model.DefaultPreferences() {
		t.Fatal(p, e)
	}
	p.Appearance = "深色"
	p.FontSize = 18
	p.FontFamily = "monospace"
	p.ClipboardReadAsk = true
	p.Cursor = "bar"
	p.DownloadDir = "~/Documents"
	p.KeepaliveSeconds = 22
	if e = s.SavePreferences(p); e != nil {
		t.Fatal(e)
	}
	s.Close()
	s, e = Open(file)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	actual, e := s.Preferences()
	if e != nil || actual != p {
		t.Fatal(actual, e)
	}
	bad := p
	bad.FontSize = 100
	if s.SavePreferences(bad) == nil {
		t.Fatal("invalid saved")
	}
	actual, _ = s.Preferences()
	if actual != p {
		t.Fatal("invalid overwrote valid")
	}
	if _, e = s.db.Exec(`UPDATE app_settings SET value='{"fontSize":19}' WHERE key='preferences_v2'`); e != nil {
		t.Fatal(e)
	}
	actual, e = s.Preferences()
	if e != nil || actual.FontSize != 19 || actual.LineHeight != 1.2 || actual.Appearance != "跟随系统" {
		t.Fatal("new defaults not overlaid", actual, e)
	}
	if _, e = s.db.Exec(`UPDATE app_settings SET value='invalid' WHERE key='preferences_v2'`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Preferences(); e == nil {
		t.Fatal("corrupt preferences hidden")
	}
}
