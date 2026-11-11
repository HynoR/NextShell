package mygo

import (
	"fmt"

	"github.com/egoist/mygo/internal/platform"
)

// ContentRules are rules that block what pages load and hide parts of
// them, as an ad or tracker blocker does, compiled by CompileContentRules
// for pages to apply (PageOptions.ContentRules, Page.SetContentRules).
// One value serves every page that applies it.
type ContentRules struct {
	id string
	// list is the backend's handle of the compiled list. Main thread
	// only.
	list any
}

// ID returns the id the rules were compiled under.
func (r *ContentRules) ID() string { return r.id }

// applyContentRules gives a page's native side the lists of rules. It is
// nil until the app compiles rules, so that apps that never do don't
// link the backends' content blocking. Main thread only.
var applyContentRules func(n platform.Page, rules []*ContentRules)

// CompileContentRules compiles rules in WebKit's content blocker format,
// a JSON array of rules, each with a trigger and an action, applied in
// order:
//
//	[{"trigger": {"url-filter": "^https?://ads\\.example\\.com/"}, "action": {"type": "block"}},
//	 {"trigger": {"url-filter": ".*", "if-domain": ["*example.com"]},
//	  "action": {"type": "css-display-none", "selector": ".banner"}}]
//
// A trigger matches a load by its url-filter, a regular expression of the
// URL (case-insensitive unless url-filter-is-case-sensitive), and narrows
// it with one of:
//   - if-domain or unless-domain: domains of the page, in lowercase
//     punycode, where "*" before one takes its subdomains too;
//   - if-top-url or unless-top-url: regular expressions of the page's URL;
//
// and with:
//   - resource-type: "document", "image", "style-sheet", "script",
//     "font", "raw" (fetches, XMLHttpRequests, WebSockets), "svg-document",
//     "media", "popup", "ping", "fetch", "websocket" or "other";
//   - load-type: "first-party" or "third-party", whether the load is from
//     the page's own site.
//
// An action is one of:
//   - block: the load does not happen;
//   - block-cookies: it goes without cookies;
//   - make-https: an http URL loads over https;
//   - css-display-none: the elements its selector selects are hidden in
//     the pages its trigger matches;
//   - ignore-previous-rules: what the rules before it did to the load is
//     undone.
//
// The regular expressions are WebKit's: ASCII, without alternatives
// ("|"), counted repetitions ("{2}") or classes such as "\d". A rule
// whose selector the engine cannot parse is skipped; any other mistake
// fails the whole list, as an empty list does.
//
// Compiling takes seconds for lists as long as EasyList, so call it off
// the main thread; on the main thread the app
// keeps handling events while it waits. The engine keeps what it compiled
// under id, with a hash of the rules: the same rules on a later launch
// load without compiling again, and compiling other rules under the same
// id removes the list of the earlier ones. An id is letters, digits, "-",
// "_" and ".", at most 64 of them. WebKit limits the rules of a list
// (to 150,000, or 50,000 in older versions): split longer lists and
// apply them together.
//
// macOS and Linux (WebKitGTK 2.24 or later) compile the rules with
// WebKit's content blockers; on Windows MyGo matches every load of a page
// applying rules against them, and hides elements with a style sheet each
// document adopts. There WebSockets go unblocked, as WebView2 does not
// show their requests, and an element's inline style with !important
// wins over the style sheet. In browsers it returns an error matching
// errors.ErrUnsupported.
func CompileContentRules(id, rules string) (*ContentRules, error) {
	needsApp("CompileContentRules")
	if !validContentRulesID(id) {
		return nil, fmt.Errorf("mygo: content rules: invalid id %q", id)
	}
	identifier := platform.ContentRulesIdentifier(id, rules)
	result := nativeCall(func(done func(nativeResult)) {
		if applyContentRules == nil {
			linkContentRules()
			applyContentRules = setContentRules
		}
		backend().App().CompileContentRules(id, identifier, rules, func(list any, err error) {
			done(nativeResult{list: list, err: err})
		})
	})
	if result.err != nil {
		return nil, fmt.Errorf("mygo: content rules %s: %w", id, result.err)
	}
	return &ContentRules{id: id, list: result.list}, nil
}

func validContentRulesID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, c := range []byte(id) {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return true
}

// setContentRules is applyContentRules once the app compiled rules.
func setContentRules(n platform.Page, rules []*ContentRules) {
	lists := make([]any, 0, len(rules))
	for _, r := range rules {
		if r != nil {
			lists = append(lists, r.list)
		}
	}
	n.SetContentRules(lists)
}

// SetContentRules replaces the content rules the page applies, which
// PageOptions.ContentRules set, with rules; none removes them. Loads
// from then on go through them: a page loaded already keeps what it
// loaded and hid until it reloads. Nil rules are ignored.
func (p *Page) SetContentRules(rules ...*ContentRules) {
	p.w.page(func(n platform.Window) {
		if apply := applyContentRules; apply != nil {
			apply(n, rules)
		}
	})
}

// startContentRules applies PageOptions.ContentRules to a page just made.
// Main thread only.
func startContentRules(n platform.Page, rules []*ContentRules) {
	if apply := applyContentRules; apply != nil && len(rules) > 0 {
		apply(n, rules)
	}
}
