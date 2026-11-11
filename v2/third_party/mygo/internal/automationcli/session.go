package automationcli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// session is what a session keeps between commands, in a file of its own:
// the socket, the window commands target, and the elements of each window
// as the last snapshot or diff showed them, with their refs.
type session struct {
	Socket string `json:"socket"`
	// Window is the window commands target unless they name one, 0 for
	// the focused window.
	Window int `json:"window,omitempty"`
	// Next is the number of the next ref. Numbers are never given twice
	// in a session, so that a stale ref can't name another element.
	Next int `json:"next"`
	// Seen are the windows' elements as last shown, by window ID.
	Seen map[string]*seen `json:"seen,omitempty"`

	name, path string
}

// seen is a window's elements as a snapshot or a diff last showed them.
type seen struct {
	Focus string `json:"focus,omitempty"`
	Nodes []node `json:"nodes"`
	// Refs are the elements' refs, by element ID.
	Refs map[string]int `json:"refs"`
}

var sessionName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// sessionPath returns the file of the session name in dir.
func sessionPath(dir, name string) (string, error) {
	if !sessionName.MatchString(name) {
		return "", fmt.Errorf("session name %q: use letters, digits, '.', '_' and '-'", name)
	}
	return filepath.Join(dir, name+".json"), nil
}

// loadSession reads the session name, which open made.
func loadSession(dir, name string) (*session, error) {
	path, err := sessionPath(dir, name)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("no session %q: run mygo-automation open --socket <path> first", name)
	}
	if err != nil {
		return nil, err
	}
	s := &session{}
	if err := json.Unmarshal(b, s); err != nil {
		return nil, fmt.Errorf("session %q is damaged (%v): run mygo-automation open again", name, err)
	}
	s.name, s.path = name, path
	return s, nil
}

// save writes the session, replacing its file at once so that a command
// killed halfway leaves the one before.
func (s *session) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), s.name+".*.tmp")
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), s.path)
	}
	if err != nil {
		os.Remove(f.Name())
	}
	return err
}

// ref is how outputs name an element: @e and its number.
func ref(n int) string { return "@e" + strconv.Itoa(n) }

// parseRef returns the number of a ref, written @e7 or e7.
func parseRef(s string) (int, bool) {
	s = strings.TrimPrefix(s, "@")
	if !strings.HasPrefix(s, "e") {
		return 0, false
	}
	n, err := strconv.Atoi(s[1:])
	if err != nil || n <= 0 || strconv.Itoa(n) != s[1:] {
		return 0, false
	}
	return n, true
}

// lookup returns the window and the element ID of ref number n. A ref
// lasts while its element is in what the last snapshot or diff of its
// window saw; it is stale once that showed the element gone.
func (s *session) lookup(n int) (window int, id string, err error) {
	for w, sn := range s.Seen {
		for id, r := range sn.Refs {
			if r == n {
				window, _ := strconv.Atoi(w)
				return window, id, nil
			}
		}
	}
	if n < s.Next {
		return 0, "", &staleError{ref(n), "its element is gone"}
	}
	return 0, "", &staleError{ref(n), "no snapshot or diff of this session gave it"}
}

// staleError rejects a ref that names no element now. The CLI never
// takes it for another element.
type staleError struct{ ref, why string }

func (e *staleError) Error() string {
	return fmt.Sprintf("stale ref %s: %s; use a ref from the latest snapshot or diff (run snapshot)", e.ref, e.why)
}

// observe takes the elements of a window as they are now: elements seen
// before keep their refs, new ones get new refs and the refs of elements
// gone become stale. It returns what the window showed before, nil if
// nothing.
func (s *session) observe(t *tree) *seen {
	if s.Seen == nil {
		s.Seen = map[string]*seen{}
	}
	key := strconv.Itoa(t.Window)
	old := s.Seen[key]
	if s.Next < 1 {
		s.Next = 1
	}
	now := &seen{Focus: t.Focus, Nodes: t.Nodes, Refs: make(map[string]int, len(t.Nodes))}
	for _, n := range t.Nodes {
		if _, ok := now.Refs[n.ID]; ok {
			continue
		}
		if old != nil {
			if r, ok := old.Refs[n.ID]; ok {
				now.Refs[n.ID] = r
				continue
			}
		}
		now.Refs[n.ID] = s.Next
		s.Next++
	}
	s.Seen[key] = now
	return old
}

// forget drops what a window showed, as when it closed.
func (s *session) forget(window int) {
	delete(s.Seen, strconv.Itoa(window))
}
