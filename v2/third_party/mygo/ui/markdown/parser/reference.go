package parser

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// GitHub's mentions (@octocat, @github/docs) and references to issues
// and pull requests (#123, GH-123, octocat/hello#123), found in the text
// once parsed, as email addresses are: never in code or links. Whether
// the person, team or issue exists is for a view to resolve.

const (
	maxLogin = 39  // the longest GitHub login
	maxRepo  = 100 // the longest repository name
	maxDigit = 10  // the most digits of an issue number
)

// Login returns whom a Mention mentions: a login, as octocat, or a team,
// as github/docs.
func (in Inline) Login() string {
	if in.Kind != Mention || in.Text == "" {
		return ""
	}
	return in.Text[1:]
}

// Ref returns the repository a Reference is in, as owner/name, or "" for
// the document's own, and the number of the issue or pull request.
func (in Inline) Ref() (repo string, number int) {
	if in.Kind != Reference {
		return "", 0
	}
	i := strings.LastIndexAny(in.Text, "#-")
	if i < 0 {
		return "", 0
	}
	if in.Text[i] == '#' {
		repo = in.Text[:i]
	}
	number, _ = strconv.Atoi(in.Text[i+1:])
	return repo, number
}

// references turns the mentions and references in the text of node n's
// descendants into nodes of their own, but in links and images, and in
// raw HTML's a, code, pre, style and script elements, as GitHub does.
// open counts those elements open before n's first child; it returns
// how many are open after its last.
func (ip *inlineParser) references(n int32, open int) int {
	for c := ip.nodes[n].first; c >= 0; c = ip.nodes[c].next {
		nd := &ip.nodes[c]
		switch nd.kind {
		case Link, Image:
		case HTML:
			open = max(open+skipElement(nd.text), 0)
		case Text:
			if open == 0 {
				// A text's start is a word's start unless text before it
				// ends in a word, as formatting ends none (_a_#1).
				prev := nd.prev >= 0 && ip.nodes[nd.prev].kind == Text && endsInWord(ip.nodes[nd.prev].text)
				c = ip.splitReferences(c, prev)
			}
		default:
			open = ip.references(c, open)
		}
	}
	return open
}

// skipElement returns 1 for raw HTML opening an element whose text has
// no mentions or references, -1 for HTML closing one, else 0.
func skipElement(tag string) int {
	closing := strings.HasPrefix(tag, "</")
	name := strings.TrimPrefix(strings.TrimPrefix(tag, "<"), "/")
	end := 0
	for end < len(name) && isASCIIAlnum(name[end]) {
		end++
	}
	switch strings.ToLower(name[:end]) {
	case "a", "code", "pre", "style", "script":
	default:
		return 0
	}
	switch {
	case closing:
		return -1
	case strings.HasSuffix(tag, "/>"):
		return 0
	}
	return 1
}

// splitReferences splits text node c at its mentions and references,
// which a word precedes when prevWord, and returns the last node of the
// text.
func (ip *inlineParser) splitReferences(c int32, prevWord bool) int32 {
	// The text's @, # and - are found once each, as offsets in the text
	// whose rest each split leaves: origin is where node c's text starts.
	sc := refScan{next: [3]int{-1, -1, -1}, has: ip.refBytes}
	origin := 0
	for {
		nd := &ip.nodes[c]
		data := nd.text
		a, b, kind, steps := findReference(data, origin, &sc, prevWord, ip.ext)
		ip.d.steps += steps
		if kind == Text {
			return c
		}
		source := ip.s[nd.start:min(nd.end, len(ip.s))]
		start, end := nd.start, nd.end
		la := start + literalToSource(source, data, a)
		lb := start + literalToSource(source, data, b)
		r := ip.newNode(kind, data[a:b], la, lb)
		post := ip.newNode(Text, data[b:], lb, end)
		nd = &ip.nodes[c]
		nd.text, nd.start, nd.end = data[:a], start, la
		ip.insertAfter(c, r)
		ip.insertAfter(r, post)
		c, prevWord = post, endsInWord(data[:b])
		origin += b
	}
}

// refScan is where the next @, # and - of a text are, as offsets in it,
// -1 before they are looked for, and the text's length when there is none
// or the paragraph has none (has, as referenceBytes returns).
type refScan struct {
	next [3]int
	has  uint8
}

// findReference finds the first mention or reference in data, the text
// from origin on, which a word character precedes when prevWord: its
// bytes and kind, or Text when there is none, and the bytes it scanned.
func findReference(data string, origin int, sc *refScan, prevWord bool, ext Extensions) (a, b int, kind InlineKind, steps int) {
	mentions, refs := ext&ExtMentions != 0, ext&ExtReferences != 0
	wordBefore := func(i int) bool {
		if i == 0 {
			return prevWord
		}
		return endsInWord(data[:i])
	}
	// The next @, # and - at or after i, each found with the vectorized
	// IndexByte, which IndexAny is not, and kept across the text's splits.
	find := func(k int, c byte, i int) int {
		if sc.next[k] < origin+i {
			if j := strings.IndexByte(data[i:], c); j < 0 {
				sc.next[k] = origin + len(data)
			} else {
				sc.next[k] = origin + i + j
			}
			steps += sc.next[k] - origin - i
		}
		return sc.next[k] - origin
	}
	for i := 0; i < len(data); i++ {
		j := len(data)
		if sc.has&hasAt != 0 {
			j = find(0, '@', i)
		}
		if sc.has&hasHash != 0 {
			j = min(j, find(1, '#', i))
		}
		if sc.has&hasGH != 0 {
			j = min(j, find(2, '-', i))
		}
		if j == len(data) {
			return 0, 0, Text, steps
		}
		i = j
		switch data[i] {
		case '@':
			if mentions && !wordBefore(i) {
				if e := scanMention(data, i+1); e > 0 {
					return i, e, Mention, steps + e - i
				}
			}
		case '#':
			if !refs {
				continue
			}
			from := i
			if wordBefore(i) {
				var n int
				from, n = repoBefore(data, i, prevWord)
				steps += n
				if from < 0 {
					continue
				}
			}
			if e := scanNumber(data, i+1); e > 0 {
				return from, e, Reference, steps + e - i
			}
		default: // GH-123, found at its hyphen, as letters are many
			if refs && isGH(data, i) && !wordBefore(i-2) {
				if e := scanNumber(data, i+1); e > 0 {
					return i - 2, e, Reference, steps + e - i
				}
			}
		}
	}
	return 0, 0, Text, steps
}

// scanMention returns the end of the login, or team, at data[i:], after
// an @, or 0: a login is letters, digits and hyphens, from a letter or a
// digit, and a team a login, a slash and its name. No word follows it,
// nor a dot and a word, as in @example.com.
func scanMention(data string, i int) int {
	j := i
	if j >= len(data) || !isASCIIAlnum(data[j]) {
		return 0
	}
	for j < len(data) && (isASCIIAlnum(data[j]) || data[j] == '-') {
		j++
	}
	if j-i > maxLogin {
		return 0
	}
	if j+1 < len(data) && data[j] == '/' && isASCIIAlnum(data[j+1]) {
		j += 2
		for j < len(data) && (isASCIIAlnum(data[j]) || data[j] == '-' || data[j] == '_') {
			j++
		}
	}
	if startsWithWord(data[j:]) || j+1 < len(data) && data[j] == '.' && isASCIIAlnum(data[j+1]) {
		return 0
	}
	return j
}

// scanNumber returns the end of the issue number at data[i:], or 0: no
// more than maxDigit digits, from 1 to 9, which no word follows.
func scanNumber(data string, i int) int {
	j := i
	if j >= len(data) || data[j] < '1' || data[j] > '9' {
		return 0
	}
	for j < len(data) && isASCIIDigit(data[j]) {
		j++
	}
	if j-i > maxDigit || startsWithWord(data[j:]) {
		return 0
	}
	return j
}

// repoBefore returns where owner/name ends at data[h], a #, begins, or
// -1, and the bytes it scanned: no word, slash or dot precedes it.
func repoBefore(data string, h int, prevWord bool) (int, int) {
	j := h
	for j > 0 && h-j < maxRepo && isRepoChar(data[j-1]) {
		j--
	}
	if j == h || j == 0 || data[j-1] != '/' {
		return -1, h - j
	}
	o := j - 1
	k := o
	for k > 0 && o-k < maxLogin && (isASCIIAlnum(data[k-1]) || data[k-1] == '-') {
		k--
	}
	steps := h - k
	if k == o || !isASCIIAlnum(data[k]) {
		return -1, steps
	}
	if k == 0 {
		if prevWord {
			return -1, steps
		}
		return 0, steps
	}
	if c := data[k-1]; c == '/' || c == '.' || endsInWord(data[:k]) {
		return -1, steps
	}
	return k, steps
}

func isRepoChar(c byte) bool { return isASCIIAlnum(c) || c == '-' || c == '_' || c == '.' }

// isWordRune reports whether r is of a word: a letter, a digit or _.
func isWordRune(r rune) bool {
	if r < utf8.RuneSelf {
		return isASCIIAlnum(byte(r)) || r == '_'
	}
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

func endsInWord(s string) bool {
	r, _ := utf8.DecodeLastRuneInString(s)
	return s != "" && isWordRune(r)
}

func startsWithWord(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return s != "" && isWordRune(r)
}

// isGH reports whether data[i], a hyphen, follows GH, in any case.
func isGH(data string, i int) bool {
	return i >= 2 && data[i-2]|0x20 == 'g' && data[i-1]|0x20 == 'h'
}

// Which of a text's bytes may start a mention or a reference, as
// referenceBytes finds them.
const (
	hasAt   = 1 << iota // @, of a mention
	hasHash             // #, of a reference
	hasGH               // the - of GH-
)

// referenceBytes returns which bytes that may start a mention or a
// reference s holds, of those ext reads, so that text without them is not
// scanned again, nor scanned for those it lacks.
func referenceBytes(s string, ext Extensions) uint8 {
	var has uint8
	// IndexByte is vectorized, which ContainsAny is not.
	if ext&ExtMentions != 0 && strings.IndexByte(s, '@') >= 0 {
		has |= hasAt
	}
	if ext&ExtReferences == 0 {
		return has
	}
	if strings.IndexByte(s, '#') >= 0 {
		has |= hasHash
	}
	for i := strings.IndexByte(s, '-'); i >= 0; {
		if isGH(s, i) {
			has |= hasGH
			break
		}
		j := strings.IndexByte(s[i+1:], '-')
		if j < 0 {
			break
		}
		i += 1 + j
	}
	return has
}

func isASCIIAlnum(c byte) bool { return isASCIIDigit(c) || isASCIILetter(c) }
