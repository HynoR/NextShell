//go:build darwin

package darwin

import (
	"fmt"

	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo/internal/platform"
)

// contentRules is the code of content rules, which LinkContentRules
// links: the backend's methods are linked into every app, and only call
// it.
var contentRules struct {
	compile func(b *Backend, id, identifier, rules string, done func(any, error))
	set     func(w *window, lists []any)
}

// LinkContentRules links the code of content rules, when the app first
// compiles rules.
func LinkContentRules() {
	contentRules.compile, contentRules.set = compileContentRules, setContentRules
}

func (a appController) CompileContentRules(id, identifier, rules string, done func(any, error)) {
	if contentRules.compile == nil {
		done(nil, platform.ErrUnsupported)
		return
	}
	contentRules.compile(a.b, id, identifier, rules, done)
}

func (w *window) SetContentRules(lists []any) {
	if contentRules.set != nil {
		contentRules.set(w, lists)
	}
}

// compileContentRules makes a WKContentRuleList in the default
// WKContentRuleListStore, which keeps compiled lists in the app's
// container across launches: the list kept as identifier, else one
// compiled from rules, after which the lists of id's earlier rules go.
// The handle is the list, retained for good: a page may apply it at any
// time.
func compileContentRules(b *Backend, contentID, identifier, rules string, done func(any, error)) {
	if class("WKContentRuleListStore") == 0 {
		done(nil, fmt.Errorf("content rules need macOS 10.13 or newer: %w", platform.ErrUnsupported))
		return
	}
	finish := func(list, err id) {
		switch {
		case list != 0:
			done(retain(list), nil)
		case err != 0:
			done(nil, contentRulesError(err))
		default:
			done(nil, fmt.Errorf("WebKit made no list"))
		}
	}
	store := retain(send(class("WKContentRuleListStore"), "defaultStore"))
	compile := func() {
		compiled := newBlock(func(_ objc.Block, list, err id) {
			retain(list)
			retain(err)
			b.runOnMain(func() {
				defer release(store)
				defer release(list)
				defer release(err)
				if list != 0 {
					removeStaleContentRules(store, contentID, identifier)
				}
				finish(list, err)
			})
		})
		send(store, "compileContentRuleListForIdentifier:encodedContentRuleList:completionHandler:",
			uintptr(nsString(identifier)), uintptr(nsString(rules)), uintptr(compiled))
		compiled.Release()
	}
	withPool(func() {
		found := newBlock(func(_ objc.Block, list, _ id) {
			retain(list)
			b.runOnMain(func() {
				defer release(list)
				if list == 0 {
					withPool(compile)
					return
				}
				release(store)
				finish(list, 0)
			})
		})
		send(store, "lookUpContentRuleListForIdentifier:completionHandler:", uintptr(nsString(identifier)), uintptr(found))
		found.Release()
	})
}

// contentRulesError describes an error of the store, which puts what is
// wrong with the rules in NSHelpAnchorErrorKey.
func contentRulesError(err id) error {
	info := send(err, "userInfo")
	if reason := send(info, "objectForKey:", uintptr(nsString("NSHelpAnchor"))); reason != 0 {
		return fmt.Errorf("%s", goString(reason))
	}
	return nsError(err)
}

// removeStaleContentRules removes the lists the store keeps for id but
// current.
func removeStaleContentRules(store id, contentID, current string) {
	retain(store)
	listed := newBlock(func(_ objc.Block, identifiers id) {
		var stale []string
		withPool(func() {
			for _, s := range arrayItems(identifiers) {
				if name := goString(s); platform.StaleContentRules(contentID, current, name) {
					stale = append(stale, name)
				}
			}
			for _, name := range stale {
				removed := newBlock(func(objc.Block, id) {})
				send(store, "removeContentRuleListForIdentifier:completionHandler:", uintptr(nsString(name)), uintptr(removed))
				removed.Release()
			}
		})
		release(store)
	})
	send(store, "getAvailableContentRuleListIdentifiers:", uintptr(listed))
	listed.Release()
}

// setContentRules replaces the lists of the window's content controller,
// which WebKit applies to the loads from then on.
func setContentRules(w *window, lists []any) {
	if w.ucc == 0 || w.closed {
		return
	}
	withPool(func() {
		send(w.ucc, "removeAllContentRuleLists")
		for _, l := range lists {
			if list, ok := l.(id); ok {
				send(w.ucc, "addContentRuleList:", uintptr(list))
			}
		}
	})
}
