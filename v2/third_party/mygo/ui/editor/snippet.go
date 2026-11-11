package editor

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Snippet is an immutable code template with numbered tab stops, parsed
// by ParseSnippet. It can be shared by completions and editor states.
// Its zero value is an empty template.
type Snippet struct {
	text   string
	fields []snippetField
	stops  []int
}

type snippetField struct {
	id, start, end int
	site           int // insertion site, of a live session
}

// Text returns the text inserted before the placeholders are edited.
func (s *Snippet) Text() string { return s.text }

// ParseSnippet parses the tab-stop subset of TextMate/LSP snippets:
// $1 and ${1}, placeholders such as ${1:name}, and $0 for the final stop.
// Numbers are non-negative 32-bit integers. Stops are visited in numeric
// order, with 0 last (the end when omitted).
// References to the same number share its default and are edited together;
// empty stops at the same byte coalesce as ordinary carets do.
//
// Escape $, } and backslash with a backslash. Defaults are plain text:
// nested placeholders, variables, choices and transformations return an
// error, as do conflicting defaults for the same number and invalid UTF-8.
// Text is inserted literally, with CRLF normalized to LF, without adjusting
// indentation. Parsing an unsupported server snippet lets the app offer a
// plain-text completion instead.
func ParseSnippet(source string) (*Snippet, error) {
	if !utf8.ValidString(source) {
		return nil, fmt.Errorf("editor: snippet is not UTF-8")
	}
	source = lf(source)
	type part struct {
		text  string
		id    int
		field bool
	}
	var parts []part
	defaults := map[int]string{}
	var literal strings.Builder
	flush := func() {
		if literal.Len() > 0 {
			parts = append(parts, part{text: literal.String()})
			literal.Reset()
		}
	}
	bad := func(at int) (*Snippet, error) {
		return nil, fmt.Errorf("editor: unsupported or invalid snippet at byte %d", at)
	}
	for i := 0; i < len(source); {
		if source[i] == '\\' && i+1 < len(source) && strings.ContainsRune("$}\\", rune(source[i+1])) {
			literal.WriteByte(source[i+1])
			i += 2
			continue
		}
		if source[i] != '$' {
			literal.WriteByte(source[i])
			i++
			continue
		}
		at := i
		i++
		braced := i < len(source) && source[i] == '{'
		if braced {
			i++
		}
		start := i
		for i < len(source) && source[i] >= '0' && source[i] <= '9' {
			i++
		}
		if start == i {
			if braced || i < len(source) && (source[i] == '_' || source[i] >= 'a' && source[i] <= 'z' || source[i] >= 'A' && source[i] <= 'Z') {
				return bad(at)
			}
			literal.WriteByte('$')
			continue
		}
		id, err := strconv.Atoi(source[start:i])
		if err != nil || id > 1<<31-1 {
			return bad(at)
		}
		if braced {
			if i < len(source) && source[i] == ':' {
				i++
				var value strings.Builder
				for i < len(source) && source[i] != '}' {
					if source[i] == '\\' && i+1 < len(source) && strings.ContainsRune("$}\\", rune(source[i+1])) {
						value.WriteByte(source[i+1])
						i += 2
						continue
					}
					if source[i] == '$' {
						return bad(i)
					}
					value.WriteByte(source[i])
					i++
				}
				if old, ok := defaults[id]; ok && old != value.String() {
					return bad(at)
				}
				defaults[id] = value.String()
			}
			if i >= len(source) || source[i] != '}' {
				return bad(at)
			}
			i++
		}
		flush()
		parts = append(parts, part{id: id, field: true})
	}
	flush()
	s := &Snippet{}
	var text strings.Builder
	seen := map[snippetField]bool{}
	ids := map[int]bool{}
	for _, p := range parts {
		if !p.field {
			text.WriteString(p.text)
			continue
		}
		start := text.Len()
		text.WriteString(defaults[p.id])
		f := snippetField{id: p.id, start: start, end: text.Len()}
		if !seen[f] {
			s.fields = append(s.fields, f)
			seen[f] = true
		}
		ids[p.id] = true
	}
	s.text = text.String()
	if !ids[0] {
		s.fields = append(s.fields, snippetField{id: 0, start: len(s.text), end: len(s.text)})
	}
	for id := range ids {
		if id != 0 {
			s.stops = append(s.stops, id)
		}
	}
	slices.Sort(s.stops)
	s.stops = append(s.stops, 0)
	return s, nil
}

// InsertSnippet replaces each selection with s and selects its first
// numbered placeholders. Tab and Shift+Tab visit stops; Escape, undo,
// an external buffer edit or moving outside the active fields ends the
// session. A new snippet replaces the session, rather than nesting it.
// Redo restores the insertion and its selections, without restarting a
// snippet session. It reports whether it inserted, and leaves a read-only
// editor unchanged. Call it on the UI thread, as other State methods.
func (st *State) InsertSnippet(s *Snippet) bool {
	if s == nil {
		return false
	}
	st.ensure()
	return st.ed.retry(func() bool {
		edits := make([]Edit, len(st.ed.sels))
		for i, sel := range st.ed.sels {
			edits[i] = Edit{Start: sel.start(), End: sel.end(), Text: s.text}
		}
		return st.ed.insertSnippet(s, edits)
	})
}

type snippetSession struct {
	fields      []snippetField // in document order, including each insertion site
	stops       []int
	at          int
	primarySite int
	local       bool // the next buffer change was made through this editor's apply
}

func (ed *editor) insertSnippet(s *Snippet, edits []Edit) bool {
	if ed.o.readOnly {
		return false
	}
	if len(s.stops) == 0 {
		s = &Snippet{stops: []int{0}, fields: []snippetField{{}}}
	}
	// Completion prefixes can overlap when two carets are in one word.
	// Merge those targets before computing each inserted template's fields.
	targets := make([]sel, len(edits))
	for i, e := range edits {
		targets[i] = sel{anchor: e.Start, head: e.End, id: ed.sels[i].id}
	}
	targets = mergeSels(targets)
	edits = make([]Edit, len(targets))
	for i, target := range targets {
		edits[i] = Edit{Start: target.start(), End: target.end(), Text: s.text}
	}
	ed.snippet = nil
	ed.endGroup()
	version := ed.view.version
	session := &snippetSession{stops: s.stops}
	for i, target := range targets {
		if target.id == ed.primary().id {
			session.primarySite = i
		}
	}
	ok := ed.apply(edits, editOther, func(func(int, bias) int) []sel {
		session.fields = nil
		delta := 0
		for site, e := range edits {
			for _, f := range s.fields {
				session.fields = append(session.fields, snippetField{id: f.id, start: e.Start + delta + f.start, end: e.Start + delta + f.end, site: site})
			}
			delta += len(s.text) - (e.End - e.Start)
		}
		return session.selections(ed)
	})
	if ok && len(s.stops) > 1 && ed.view.version <= version+1 {
		ed.snippet = session
	}
	return ok
}

func (s *snippetSession) selections(ed *editor) []sel {
	var sels []sel
	primary := -1
	for _, f := range s.fields {
		if f.id == s.stops[s.at] {
			if f.site == s.primarySite && primary < 0 {
				primary = len(sels)
			}
			sels = append(sels, ed.newSel(f.start, f.end))
		}
	}
	// Input methods and scrolling follow the first occurrence in the
	// insertion site that held the primary caret, not its last mirror.
	if primary >= 0 {
		ed.nextID++
		sels[primary].id = ed.nextID
	}
	return sels
}

func (ed *editor) stepSnippet(back bool) bool {
	s := ed.snippet
	if s == nil {
		return false
	}
	if ed.o.readOnly || !s.contains(ed.sels) {
		ed.snippet = nil
		return false
	}
	if back {
		s.at = max(0, s.at-1)
	} else {
		s.at++
	}
	ed.snippet = nil // setSels is an explicit selection change, except here
	ed.setSels(s.selections(ed), revealFit)
	ed.endGroup()
	if s.at < len(s.stops)-1 {
		ed.snippet = s
	}
	return true
}

// contains requires the same relative selection in every active field,
// so moving one caret alone does not leave a partially linked session.
func (s *snippetSession) contains(sels []sel) bool {
	i, anchor, head := 0, 0, 0
	for _, f := range s.fields {
		if f.id != s.stops[s.at] {
			continue
		}
		if i >= len(sels) || sels[i].start() < f.start || sels[i].end() > f.end {
			return false
		}
		a, h := sels[i].anchor-f.start, sels[i].head-f.start
		if i == 0 {
			anchor, head = a, h
		} else if a != anchor || h != head {
			return false
		}
		i++
	}
	return i == len(sels)
}

func (s *snippetSession) accepts(edits []Edit) bool {
	i := 0
	var first Edit
	for _, f := range s.fields {
		if f.id != s.stops[s.at] {
			continue
		}
		if i >= len(edits) || edits[i].Start < f.start || edits[i].End > f.end {
			return false
		}
		e := edits[i]
		e.Start -= f.start
		e.End -= f.start
		if i == 0 {
			first = e
		} else if e != first {
			return false
		}
		i++
	}
	return i == len(edits)
}

// moved follows only the one pending local change. Whole fields move in
// template order, so an edit at a shared boundary belongs to its field,
// rather than both of two adjacent fields.
func (s *snippetSession) moved(cs []change) bool {
	if !s.local {
		return false
	}
	s.local = false
	i, delta := 0, 0
	for n := range s.fields {
		f := &s.fields[n]
		start, end := f.start, f.end
		f.start += delta
		f.end += delta
		if f.id == s.stops[s.at] {
			if i >= len(cs) || cs[i].oldStart < start || cs[i].oldEnd > end {
				return false
			}
			c := cs[i]
			d := (c.newEnd - c.newStart) - (c.oldEnd - c.oldStart)
			f.end += d
			delta += d
			i++
		}
	}
	if i != len(cs) {
		return false
	}
	// Deleting adjacent mirrors can leave coincident empty fields,
	// which coalesce just as their carets do.
	seen := make(map[[3]int]bool, len(s.fields))
	s.fields = slices.DeleteFunc(s.fields, func(f snippetField) bool {
		key := [3]int{f.id, f.start, f.end}
		duplicate := seen[key]
		seen[key] = true
		return duplicate
	})
	return true
}

// Keep adjacent linked fields separate: merging their deletions would
// discard which portion belonged to each placeholder. Other deletions
// retain the editor's usual overlap handling.
func (ed *editor) mergeDeletes(edits []Edit) []Edit {
	if ed.snippet != nil && ed.snippet.accepts(edits) {
		return edits
	}
	return mergeEdits(edits)
}
