// Package contentrules matches loads against rules in WebKit's content
// blocker format, for the backends whose engine has no content blockers
// (WebView2). Only the backends' content rules reach it, so that apps
// that never compile rules don't link it.
package contentrules

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"regexp/syntax"
	"slices"
	"strings"

	"golang.org/x/net/publicsuffix"
)

// Type is a set of the kinds of resources of WebKit's resource-type.
type Type uint16

const (
	Document Type = 1 << iota
	Image
	StyleSheet
	Script
	Font
	Raw
	SVGDocument
	Media
	Popup
	Ping
	Fetch
	WebSocket
	Other
)

// typeNamed returns the type of a name of resource-type, 0 for none.
// It and actionNamed switch rather than look up a map, which package
// initialization would make in every app of a backend importing this.
func typeNamed(name string) Type {
	switch name {
	case "document":
		return Document
	case "image":
		return Image
	case "style-sheet":
		return StyleSheet
	case "script":
		return Script
	case "font":
		return Font
	case "raw":
		return Raw
	case "svg-document":
		return SVGDocument
	case "media":
		return Media
	case "popup":
		return Popup
	case "ping":
		return Ping
	case "fetch":
		return Fetch
	case "websocket":
		return WebSocket
	case "other":
		return Other
	}
	return 0
}

type action uint8

const (
	invalid action = iota
	block
	blockCookies
	makeHTTPS
	hide
	ignorePrevious
)

func actionNamed(name string) action {
	switch name {
	case "block":
		return block
	case "block-cookies":
		return blockCookies
	case "make-https":
		return makeHTTPS
	case "css-display-none":
		return hide
	case "ignore-previous-rules":
		return ignorePrevious
	}
	return invalid
}

// List is a list of rules, parsed. Match it from one goroutine at a time:
// it compiles the rules' regular expressions as it first needs them.
type List struct {
	rules []rule
	// byDomain holds the indexes of the rules whose url-filter matches
	// only URLs of a domain and its subdomains, by that domain; byGram
	// those of the others whose literal has four bytes or more, by four
	// of them (gram), which every URL they match has; others the rest,
	// which Match tries for every load. css-display-none rules are only
	// in cosmetic.
	byDomain map[string][]int32
	byGram   map[uint32][]int32
	others   []int32
	// cosmetic is the css-display-none and ignore-previous-rules rules
	// for HidingScript, as JSON.
	cosmetic string
}

type rule struct {
	filter     pattern
	types      Type // 0 for every type
	loadType   uint8
	ifDomain   []string
	unless     bool // ifDomain lists unless-domain
	topURL     []pattern
	unlessTop  bool // topURL lists unless-top-url
	action     action
	hasDomains bool
}

const (
	anyParty uint8 = iota
	firstParty
	thirdParty
)

// pattern is a regular expression of WebKit's, compiled when first
// matched, with a literal every match contains, to skip it cheaply.
type pattern struct {
	source    string
	literal   string // lowercase unless sensitive
	sensitive bool
	re        *regexp.Regexp
}

func newPattern(source string, sensitive bool) (pattern, error) {
	if err := webKitRegexp(source); err != nil {
		return pattern{}, fmt.Errorf("invalid or unsupported regular expression %q: %w", source, err)
	}
	flags := syntax.Perl
	if !sensitive {
		flags |= syntax.FoldCase
	}
	re, err := syntax.Parse(source, flags)
	if err != nil {
		return pattern{}, fmt.Errorf("invalid regular expression %q: %w", source, err)
	}
	p := pattern{source: source, sensitive: sensitive, literal: longestLiteral(re.Simplify())}
	if !sensitive {
		p.literal = strings.ToLower(p.literal)
	}
	return p, nil
}

// webKitRegexp reports what of source WebKit's content blockers do not
// take: alternatives, counted repetitions, classes as \d, "^" but at the
// start, and other than ASCII.
func webKitRegexp(source string) error {
	class := false
	for i := 0; i < len(source); i++ {
		c := source[i]
		switch {
		case c >= 0x80:
			return errors.New("not ASCII")
		case c == '\\':
			i++
			if i < len(source) && strings.IndexByte("dDwWsSbB", source[i]) >= 0 {
				return fmt.Errorf("\\%c", source[i])
			}
		case class:
			class = c != ']'
		case c == '[':
			class = true
			if strings.HasPrefix(source[i+1:], "^") {
				i++
			}
		case c == '|', c == '{':
			return fmt.Errorf("%q", c)
		case c == '^' && i > 0:
			return errors.New(`"^" but at the start`)
		}
	}
	return nil
}

// longestLiteral returns the longest string every match of re contains.
func longestLiteral(re *syntax.Regexp) string {
	switch re.Op {
	case syntax.OpLiteral:
		return string(re.Rune)
	case syntax.OpCapture:
		return longestLiteral(re.Sub[0])
	case syntax.OpPlus:
		return longestLiteral(re.Sub[0])
	case syntax.OpConcat:
		best, run := "", ""
		for _, sub := range re.Sub {
			if sub.Op == syntax.OpLiteral {
				run += string(sub.Rune)
				continue
			}
			best = longer(best, run)
			run = ""
			best = longer(best, longestLiteral(sub))
		}
		return longer(best, run)
	}
	return ""
}

func longer(a, b string) string {
	if len(b) > len(a) {
		return b
	}
	return a
}

// match reports whether s, and lower, s in lowercase, match.
func (p *pattern) match(s, lower string) bool {
	if p.literal != "" {
		in := lower
		if p.sensitive {
			in = s
		}
		if !strings.Contains(in, p.literal) {
			return false
		}
	}
	if p.re == nil {
		src := p.source
		if !p.sensitive {
			src = "(?i)" + src
		}
		p.re = regexp.MustCompile(src) // newPattern parsed it
	}
	return p.re.MatchString(s)
}

// domainPrefix starts the url-filters that match a domain and its
// subdomains, as converters write Adblock Plus's "||".
const domainPrefix = `^[^:]+://+([^:/]+\.)?`

// filterDomain returns the domain a url-filter matches only URLs of,
// with its subdomains, if it is one written as domainPrefix writes them.
func filterDomain(filter string) string {
	rest, ok := strings.CutPrefix(filter, domainPrefix)
	if !ok {
		return ""
	}
	var domain strings.Builder
	for len(rest) > 0 {
		c := rest[0]
		switch {
		case c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_':
			domain.WriteByte(c | 0x20*boolByte(c >= 'A' && c <= 'Z'))
			rest = rest[1:]
			continue
		case strings.HasPrefix(rest, `\.`):
			domain.WriteByte('.')
			rest = rest[2:]
			continue
		}
		break
	}
	// The domain ends where the host does.
	for _, end := range []string{`[:/]`, `[/:]`, `/`, `:`} {
		if after, ok := strings.CutPrefix(rest, end); ok && !strings.HasPrefix(after, "*") && !strings.HasPrefix(after, "?") && !strings.HasPrefix(after, "+") {
			if d := domain.String(); d != "" && !strings.HasPrefix(d, ".") && !strings.HasSuffix(d, ".") {
				return d
			}
		}
	}
	return ""
}

func boolByte(b bool) byte {
	if b {
		return 1
	}
	return 0
}

// Parse parses rules in WebKit's content blocker format, failing as
// WebKit does on what it would not compile.
func Parse(rules string) (*List, error) {
	var raw []struct {
		Trigger json.RawMessage `json:"trigger"`
		Action  struct {
			Type     string `json:"type"`
			Selector string `json:"selector"`
		} `json:"action"`
	}
	if err := json.Unmarshal([]byte(rules), &raw); err != nil {
		return nil, fmt.Errorf("rules are not a JSON array of rules: %w", err)
	}
	l := &List{byDomain: map[string][]int32{}, byGram: map[uint32][]int32{}}
	var cosmetic []cosmeticEntry
	for i, r := range raw {
		var t struct {
			URLFilter     *string  `json:"url-filter"`
			CaseSensitive bool     `json:"url-filter-is-case-sensitive"`
			IfDomain      []string `json:"if-domain"`
			UnlessDomain  []string `json:"unless-domain"`
			ResourceType  []string `json:"resource-type"`
			LoadType      []string `json:"load-type"`
			IfTopURL      []string `json:"if-top-url"`
			UnlessTopURL  []string `json:"unless-top-url"`
		}
		if err := json.Unmarshal(r.Trigger, &t); err != nil || t.URLFilter == nil {
			return nil, fmt.Errorf("rule %d: a trigger needs a url-filter", i)
		}
		ru := rule{action: actionNamed(r.Action.Type)}
		if ru.action == invalid {
			return nil, fmt.Errorf("rule %d: invalid action type %q", i, r.Action.Type)
		}
		if ru.action == hide && strings.TrimSpace(r.Action.Selector) == "" {
			return nil, fmt.Errorf("rule %d: css-display-none needs a selector", i)
		}
		var err error
		if ru.filter, err = newPattern(*t.URLFilter, t.CaseSensitive); err != nil {
			return nil, fmt.Errorf("rule %d: %w", i, err)
		}
		for _, name := range t.ResourceType {
			typ := typeNamed(name)
			if typ == 0 {
				return nil, fmt.Errorf("rule %d: invalid resource-type %q", i, name)
			}
			ru.types |= typ
		}
		for _, name := range t.LoadType {
			switch name {
			case "first-party":
				ru.loadType = firstParty
			case "third-party":
				ru.loadType = thirdParty
			default:
				return nil, fmt.Errorf("rule %d: invalid load-type %q", i, name)
			}
		}
		if len(t.LoadType) > 1 {
			ru.loadType = anyParty
		}
		if conditions := min(len(t.IfDomain), 1) + min(len(t.UnlessDomain), 1) + min(len(t.IfTopURL), 1) + min(len(t.UnlessTopURL), 1); conditions > 1 {
			return nil, fmt.Errorf("rule %d: a trigger takes one of if-domain, unless-domain, if-top-url and unless-top-url", i)
		}
		ru.ifDomain, ru.unless = t.IfDomain, len(t.UnlessDomain) > 0
		if ru.unless {
			ru.ifDomain = t.UnlessDomain
		}
		ru.hasDomains = len(ru.ifDomain) > 0
		for _, d := range ru.ifDomain {
			for _, c := range []byte(d) {
				if c >= 'A' && c <= 'Z' || c >= 0x80 {
					return nil, fmt.Errorf("rule %d: domains must be lowercase ASCII (punycode)", i)
				}
			}
		}
		ru.unlessTop = len(t.UnlessTopURL) > 0
		for _, src := range slices.Concat(t.IfTopURL, t.UnlessTopURL) {
			p, err := newPattern(src, t.CaseSensitive)
			if err != nil {
				return nil, fmt.Errorf("rule %d: %w", i, err)
			}
			ru.topURL = append(ru.topURL, p)
		}
		if ru.action == hide || ru.action == ignorePrevious {
			cosmetic = appendCosmetic(cosmetic, r.Trigger, cosmeticEntry{
				URL: *t.URLFilter, Sensitive: t.CaseSensitive, IfDomain: t.IfDomain, UnlessDomain: t.UnlessDomain,
				IfTopURL: t.IfTopURL, UnlessTopURL: t.UnlessTopURL, Ignore: ru.action == ignorePrevious,
			}, r.Action.Selector)
			if ru.action == hide {
				continue
			}
		}
		l.rules = append(l.rules, ru)
		index := int32(len(l.rules) - 1)
		if d := filterDomain(*t.URLFilter); d != "" {
			l.byDomain[d] = append(l.byDomain[d], index)
		} else if g, ok := l.rarestGram(strings.ToLower(ru.filter.literal)); ok {
			l.byGram[g] = append(l.byGram[g], index)
		} else {
			l.others = append(l.others, index)
		}
	}
	if len(raw) == 0 {
		return nil, errors.New("no rules")
	}
	if hasHide(cosmetic) {
		data, err := json.Marshal(cosmetic)
		if err != nil {
			return nil, err
		}
		l.cosmetic = string(data)
	}
	return l, nil
}

func gram(s string) uint32 {
	return uint32(s[0]) | uint32(s[1])<<8 | uint32(s[2])<<16 | uint32(s[3])<<24
}

// rarestGram returns the four bytes of literal fewest rules are indexed
// by, if it has four.
func (l *List) rarestGram(literal string) (uint32, bool) {
	best, count := uint32(0), -1
	for i := 0; i+4 <= len(literal); i++ {
		g := gram(literal[i:])
		if n := len(l.byGram[g]); count < 0 || n < count {
			best, count = g, n
		}
	}
	return best, count >= 0
}

// Request is a load to match.
type Request struct {
	// URL is what loads, TopURL the page it loads for, the URL itself
	// for a page.
	URL, TopURL string
	// Type is what kind of resource it is: a type or several, any of
	// which a rule's resource-type may name.
	Type Type
}

// Result is what rules do to a load.
type Result struct {
	Block, BlockCookies, MakeHTTPS bool
}

// Match returns what the lists do to the load: each list applies its
// rules in order, and a list's ignore-previous-rules undoes only what
// its own rules before it did.
func Match(lists []*List, r Request) Result {
	var c matchContext
	c.init(r)
	var out Result
	for _, l := range lists {
		res := l.match(&c)
		out.Block = out.Block || res.Block
		out.BlockCookies = out.BlockCookies || res.BlockCookies
		out.MakeHTTPS = out.MakeHTTPS || res.MakeHTTPS
	}
	return out
}

type matchContext struct {
	r             Request
	url, top      string // lowercase
	host, topHost string
	thirdParty    bool
	candidates    []int32
}

func (c *matchContext) init(r Request) {
	c.r = r
	if c.r.TopURL == "" {
		c.r.TopURL = r.URL
	}
	c.url, c.top = strings.ToLower(r.URL), strings.ToLower(c.r.TopURL)
	c.host, c.topHost = urlHost(c.url), urlHost(c.top)
	c.thirdParty = site(c.host) != site(c.topHost)
}

// urlHost returns the host of a lowercase URL, without its port.
func urlHost(u string) string {
	_, rest, ok := strings.Cut(u, "://")
	if !ok {
		return ""
	}
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		rest = rest[:i]
	}
	if i := strings.LastIndexByte(rest, '@'); i >= 0 {
		rest = rest[i+1:]
	}
	if strings.HasPrefix(rest, "[") {
		if i := strings.IndexByte(rest, ']'); i >= 0 {
			return rest[:i+1]
		}
	}
	if i := strings.IndexByte(rest, ':'); i >= 0 {
		rest = rest[:i]
	}
	return strings.TrimSuffix(rest, ".")
}

// site returns the registrable domain of a host, which tells first-party
// loads from third-party ones.
func site(host string) string {
	if d, err := publicsuffix.EffectiveTLDPlusOne(host); err == nil {
		return d
	}
	return host
}

func (l *List) match(c *matchContext) Result {
	// The rules of the host's domains, of the URL's grams, and the
	// others, in order.
	candidates := l.others
	c.candidates = c.candidates[:0]
	for h := c.host; h != ""; {
		c.candidates = append(c.candidates, l.byDomain[h]...)
		_, parent, ok := strings.Cut(h, ".")
		if !ok {
			break
		}
		h = parent
	}
	if len(l.byGram) > 0 {
		for i := 0; i+4 <= len(c.url); i++ {
			c.candidates = append(c.candidates, l.byGram[gram(c.url[i:])]...)
		}
	}
	if len(c.candidates) > 0 {
		c.candidates = append(c.candidates, l.others...)
		slices.Sort(c.candidates)
		c.candidates = slices.Compact(c.candidates)
		candidates = c.candidates
	}
	var out Result
	for _, i := range candidates {
		ru := &l.rules[i]
		if !ru.matches(c) {
			continue
		}
		switch ru.action {
		case block:
			out.Block = true
		case blockCookies:
			out.BlockCookies = true
		case makeHTTPS:
			out.MakeHTTPS = strings.HasPrefix(c.url, "http:") || strings.HasPrefix(c.url, "ws:")
		case ignorePrevious:
			out = Result{}
		}
	}
	return out
}

func (ru *rule) matches(c *matchContext) bool {
	if ru.types != 0 && ru.types&c.r.Type == 0 {
		return false
	}
	if ru.loadType == firstParty && c.thirdParty || ru.loadType == thirdParty && !c.thirdParty {
		return false
	}
	if ru.hasDomains && domainListed(ru.ifDomain, c.topHost) == ru.unless {
		return false
	}
	if len(ru.topURL) > 0 {
		matched := false
		for i := range ru.topURL {
			if ru.topURL[i].match(c.r.TopURL, c.top) {
				matched = true
				break
			}
		}
		if matched == ru.unlessTop {
			return false
		}
	}
	return ru.filter.match(c.r.URL, c.url)
}

// domainListed reports whether host is one of domains, where "*" before
// a domain takes its subdomains too.
func domainListed(domains []string, host string) bool {
	for _, d := range domains {
		if sub, ok := strings.CutPrefix(d, "*"); ok {
			if host == sub || strings.HasSuffix(host, "."+sub) {
				return true
			}
		} else if host == d {
			return true
		}
	}
	return false
}
