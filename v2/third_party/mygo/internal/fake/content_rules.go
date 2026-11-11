package fake

import (
	"encoding/json"
	"errors"

	"github.com/egoist/mygo/internal/platform"
)

// ContentRuleList is a list CompileContentRules made: the handle
// SetContentRules gets.
type ContentRuleList struct {
	ID, Identifier, Rules string
}

// CompileContentRules keeps a list by identifier, as WebKit keeps
// compiled lists, counting the compiles in ContentRuleCompiles. Rules
// that are not a JSON array fail.
func (a app) CompileContentRules(id, identifier, rules string, done func(any, error)) {
	a.b.mu.Lock()
	if l, ok := a.b.contentLists[identifier]; ok {
		a.b.mu.Unlock()
		done(l, nil)
		return
	}
	var array []json.RawMessage
	if err := json.Unmarshal([]byte(rules), &array); err != nil {
		a.b.mu.Unlock()
		done(nil, errors.New("rules are not a JSON array"))
		return
	}
	if a.b.contentLists == nil {
		a.b.contentLists = map[string]*ContentRuleList{}
	}
	for name := range a.b.contentLists {
		if platform.StaleContentRules(id, identifier, name) {
			delete(a.b.contentLists, name)
		}
	}
	l := &ContentRuleList{ID: id, Identifier: identifier, Rules: rules}
	a.b.contentLists[identifier] = l
	a.b.ContentRuleCompiles++
	a.b.mu.Unlock()
	done(l, nil)
}

// ContentRuleLists returns the identifiers of the lists kept.
func (b *Backend) ContentRuleLists() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var names []string
	for name := range b.contentLists {
		names = append(names, name)
	}
	return names
}

// SetContentRules keeps the lists, which ContentRules returns.
func (w *Window) SetContentRules(lists []any) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.contentRules = make([]*ContentRuleList, 0, len(lists))
	for _, l := range lists {
		w.contentRules = append(w.contentRules, l.(*ContentRuleList))
	}
	w.ContentRuleUpdates++
}

// ContentRules returns the lists the page's loads go through.
func (w *Window) ContentRules() []*ContentRuleList {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]*ContentRuleList(nil), w.contentRules...)
}
