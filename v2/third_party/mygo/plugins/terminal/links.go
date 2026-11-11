package terminal

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// urlPattern finds the URLs programs print as text, which a Command+click
// opens as it does hyperlinks.
var urlPattern = regexp.MustCompile(`(?:https?|ftp|file)://[^\s"'<>` + "`" + `\x00-\x1f]+|mailto:[^\s"'<>` + "`" + `]+`)

// proseDelimiters end a path printed in Chinese or Japanese prose, which
// puts no space around it; a quoted path may hold them.
const proseDelimiters = "，。；：！？、（）【】《》「」『』"

// pathToken finds what may be a file path programs print as text, unquoted:
// a run of anything but spaces, quotes and proseDelimiters.
var pathToken = regexp.MustCompile(`[^\s"'<>` + "`" + proseDelimiters + `]+`)

// pathInToken finds a path inside a token that is not one as a whole, as
// main.go:12 in grep's main.go:12:code, build/app.js in --out=build/app.js
// or /usr/bin in PATH=/usr/bin:/bin.
var pathInToken = regexp.MustCompile(`(?:(?:~|\.{1,2})?/[^\s:]+|[\p{L}\p{N}_.@+-]+(?:/[\p{L}\p{N}_.@+-]+)+/?)(?::\d+(?::\d+)?)?|[\p{L}\p{N}_@+-][\p{L}\p{N}_.@+-]*\.[\p{L}\p{N}_]+(?::\d+(?::\d+)?)?`)

// quotedPlace finds the place after a quoted path, as "main.py":12.
var quotedPlace = regexp.MustCompile(`^:\d+(?::\d+)?`)

// pathPlace finds the place in the file a path ends with: :line or
// :line:col (compilers, grep), (line,col) (TypeScript, MSBuild), or #Lline
// or #LlineCcol (GitHub).
var pathPlace = regexp.MustCompile(`:(\d+)(?::(\d+))?:?$|\((\d+)(?:,(\d+))?\)$|#L(\d+)(?:C(\d+))?$`)

// pathAt returns the file path of text at column col, as urlAt, or "":
// absolute, from ~, ./ or ../, with a slash, or a file name with an
// extension or a line; the place in the file is written path:line:col
// however it was printed, and a quoted path loses its quotes.
func pathAt(text []rune, cols []int, col int) (path string, x0, x1 int) {
	s := string(text)
	span := func(a, b int) (int, int, bool) {
		start := utf8.RuneCountInString(s[:a])
		end := start + utf8.RuneCountInString(s[a:b])
		if end > start && end <= len(cols) && cols[start] <= col && col <= cols[end-1] {
			return cols[start], cols[end-1] + 1, true
		}
		return 0, 0, false
	}
	// A quoted path first, as it may hold spaces and what the tokens below
	// end at.
	for i := 0; i < len(s); i++ {
		a, b := quotedAt(s, i)
		if b < 0 {
			continue
		}
		p, pa, pb := pathOf(s[a:b])
		if p == "" {
			continue
		}
		if x0, x1, ok := span(a+pa, a+pb); ok {
			return p, x0, x1
		}
		i = b - 1
	}
	for _, m := range pathToken.FindAllStringIndex(s, -1) {
		tok := s[m[0]:m[1]]
		if p, a, b := pathOf(tok); p != "" {
			if x0, x1, ok := span(m[0]+a, m[0]+b); ok {
				return p, x0, x1
			}
			continue
		}
		for _, n := range pathInToken.FindAllStringIndex(tok, -1) {
			if p, a, b := pathOf(tok[n[0]:n[1]]); p != "" {
				if x0, x1, ok := span(m[0]+n[0]+a, m[0]+n[0]+b); ok {
					return p, x0, x1
				}
			}
		}
	}
	return "", 0, 0
}

// quotedAt returns the bytes of s, a up to b, of a quoted run that opens at
// byte i and the place after it, or b < 0. A quote between letters or digits,
// as in don't, is an apostrophe and opens or closes nothing, and the inside
// of a quoted path neither starts nor ends with a space.
func quotedAt(s string, i int) (a, b int) {
	q := s[i]
	if q != '"' && q != '\'' || i > 0 && isAlnum(s[i-1]) {
		return 0, -1
	}
	j := strings.IndexByte(s[i+1:], q)
	if j < 0 {
		return 0, -1
	}
	j += i + 1
	inner := s[i+1 : j]
	if inner == "" || strings.ContainsAny(inner, "\r\n") || strings.TrimSpace(inner) != inner {
		return 0, -1
	}
	b = j + 1
	b += len(quotedPlace.FindString(s[b:]))
	if b < len(s) && isAlnum(s[b]) {
		return 0, -1
	}
	return i, b
}

// isAlnum reports whether c is an ASCII letter or digit.
func isAlnum(c byte) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9'
}

// pathOf returns the path a token of pathToken, or a quoted run, prints,
// and the bytes of the token it shows in, a up to b, or "".
func pathOf(tok string) (path string, a, b int) {
	a, b = 0, len(tok)
	quoted := tok[0] == '"' || tok[0] == '\''
	if !quoted {
		// Brackets and emphasis around a path are not part of it, nor is
		// the call a path is the argument of, as write(notes.md); a bracket
		// the path opens is, as icon_(copy).png.
		a = len(tok) - len(strings.TrimLeft(tok, "([{*"))
		// The name of an option or a variable the path is the value of,
		// as --out=build/app.js or PATH=/usr/bin, is not part of it.
		if i := strings.IndexByte(tok[a:], '='); i >= 0 && !strings.Contains(tok[a:a+i], "/") {
			a += i + 1
		}
		b = a + len(balanced(tok[a:]))
		if strings.HasSuffix(tok[a:b], ")") && !pathPlace.MatchString(tok[a:b]) {
			if i := opening(tok[a:b]); i > 0 {
				a, b = a+i+1, b-1
			}
		}
		for b > a && strings.IndexByte(",;.!?]", tok[b-1]) >= 0 {
			b--
		}
		if !pathPlace.MatchString(tok[a:b]) {
			b = a + len(trimURL(tok[a:b]))
		}
		if b <= a {
			return "", 0, 0
		}
	}
	name, line, col := tok[a:b], 0, 0
	if loc := pathPlace.FindStringSubmatchIndex(name); loc != nil {
		place := name
		name = place[:loc[0]]
		for i := 2; i < len(loc); i += 4 {
			if loc[i] >= 0 {
				line, _ = strconv.Atoi(place[loc[i]:loc[i+1]])
				if loc[i+2] >= 0 {
					col, _ = strconv.Atoi(place[loc[i+2]:loc[i+3]])
				}
				break
			}
		}
		if strings.HasSuffix(place, ":") {
			b-- // the colon after main.go:12:3: ends a compiler's location
		}
	}
	if quoted {
		name = name[1 : len(name)-1]
	}
	if name == "" || strings.ContainsAny(name, ":\x00\r\n") ||
		strings.IndexFunc(name, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) < 0 {
		return "", 0, 0
	}
	// Words are not paths: a name without a slash takes a letter, and an
	// extension or a line, so that version 1.2.3 and 12:30 are neither.
	if !strings.Contains(name, "/") {
		dot := strings.LastIndexByte(name, '.')
		if strings.IndexFunc(name, unicode.IsLetter) < 0 || line == 0 && (dot < 0 || dot == len(name)-1) {
			return "", 0, 0
		}
	}
	path = name
	if line > 0 {
		path += ":" + strconv.Itoa(line)
		if col > 0 {
			path += ":" + strconv.Itoa(col)
		}
	}
	return path, a, b
}

// balanced returns s up to a closing bracket it did not open, as the ) of
// "(image.png)已保存".
func balanced(s string) string {
	var depth [3]int
	for i, r := range s {
		if k := strings.IndexRune("([{", r); k >= 0 {
			depth[k]++
		} else if k := strings.IndexRune(")]}", r); k >= 0 {
			if depth[k] == 0 {
				return s[:i]
			}
			depth[k]--
		}
	}
	return s
}

// opening returns the index of the ( that opens the ) s ends with, or -1.
func opening(s string) int {
	depth := 0
	for i := len(s) - 1; i >= 0; i-- {
		switch s[i] {
		case ')':
			depth++
		case '(':
			if depth--; depth == 0 {
				return i
			}
		}
	}
	return -1
}

// urlAt returns the URL of text, a row whose runes are in columns cols, at
// column col, and the columns it spans, x0 up to x1, or "".
func urlAt(text []rune, cols []int, col int) (url string, x0, x1 int) {
	return matchAt(urlPattern, text, cols, col)
}

func matchAt(re *regexp.Regexp, text []rune, cols []int, col int) (string, int, int) {
	s := string(text)
	for _, m := range re.FindAllStringIndex(s, -1) {
		u := trimURL(s[m[0]:m[1]])
		if u == "" {
			continue
		}
		start := len([]rune(s[:m[0]]))
		end := start + len([]rune(u))
		if start < len(cols) && cols[start] <= col && col <= cols[end-1] {
			return u, cols[start], cols[end-1] + 1
		}
	}
	return "", 0, 0
}

// trimURL trims what likely ends the sentence around a URL rather than the
// URL: punctuation, and closing brackets that the URL did not open.
func trimURL(u string) string {
	for len(u) > 0 {
		switch last := u[len(u)-1]; last {
		case '.', ',', ';', ':', '!', '?':
			u = u[:len(u)-1]
			continue
		case ')', ']', '}':
			open := map[byte]string{')': "(", ']': "[", '}': "{"}[last]
			if strings.Count(u, open) < strings.Count(u, string(last)) {
				u = u[:len(u)-1]
				continue
			}
		}
		return u
	}
	return u
}
