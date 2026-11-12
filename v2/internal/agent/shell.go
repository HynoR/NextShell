package agent

import (
	"regexp"
	"strings"
)

// segment is one simple command of a shell line: the words after quote removal and the operator
// that preceded it. The tokenizer is deliberately small. It understands quotes, backslashes,
// ; & && || | newlines, ( ), $(…), `…`, <(…), >(…) and heredocs, which is enough to know what stands in
// command position. It does not expand variables, globs or aliases, so it is a policy aid, not a sandbox.
type segment struct {
	sep     string   // operator before the segment: "", ";", "&&", "||", "|", "&", "\n", "(", ")"
	words   []string // quote-stripped words, redirections included
	subs    []string // bodies of $(…), `…`, <(…), >(…): analysed on their own
	subst   bool     // has $(…) or `…`
	procsub bool     // has <(…) or >(…)
	stdin   []string // heredoc bodies
}

type heredoc struct {
	idx   int // index the segment will have in the output
	delim string
	strip bool // <<-: leading tabs are ignored
}

type lexer struct {
	s       string
	i       int
	segs    []segment
	cur     segment
	word    strings.Builder
	inWord  bool
	pending []heredoc
}

const maxSubDepth = 4

func tokenize(s string) []segment {
	l := &lexer{s: s}
	l.run()
	l.endSeg("")
	return l.segs
}

func (l *lexer) endWord() {
	if l.inWord {
		l.cur.words = append(l.cur.words, l.word.String())
		l.word.Reset()
		l.inWord = false
	}
}
func (l *lexer) endSeg(next string) {
	l.endWord()
	if len(l.cur.words) > 0 || len(l.cur.subs) > 0 {
		l.segs = append(l.segs, l.cur)
		l.cur = segment{sep: next}
		return
	}
	if next != "" {
		l.cur.sep = next // an operator after an empty segment still orders what follows
	}
}
func (l *lexer) add(b byte) { l.word.WriteByte(b); l.inWord = true }

// matchClose returns the index of the closer that matches an opener whose body starts at i, or -1.
func matchClose(s string, i int, open, closer byte) int {
	depth := 1
	for ; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '\'':
			if open == '`' {
				continue
			}
			j := strings.IndexByte(s[i+1:], '\'')
			if j < 0 {
				return -1
			}
			i += j + 1
		case '"':
			for i++; i < len(s) && s[i] != '"'; i++ {
				if s[i] == '\\' {
					i++
				}
			}
		case closer:
			depth--
			if depth == 0 {
				return i
			}
		case open:
			if open != closer {
				depth++
			}
		}
	}
	return -1
}

// substitution consumes $(…), `…`, <(…) or >(…) whose body starts at l.i+skip, keeping its source in the word.
func (l *lexer) substitution(skip int, open, closer byte) {
	start := l.i + skip
	end := matchClose(l.s, start, open, closer)
	body := ""
	if end < 0 {
		body, end = l.s[start:], len(l.s)-1
	} else {
		body = l.s[start:end]
	}
	l.cur.subs = append(l.cur.subs, body)
	l.word.WriteString(l.s[l.i : end+1])
	l.inWord = true
	l.i = end + 1
}

func (l *lexer) run() {
	s := l.s
	for l.i < len(s) {
		c := s[l.i]
		var next byte
		if l.i+1 < len(s) {
			next = s[l.i+1]
		}
		switch {
		case c == '\\':
			if next == '\n' {
				l.i += 2
			} else if next != 0 {
				l.add(next)
				l.i += 2
			} else {
				l.i++
			}
		case c == '\'':
			j := strings.IndexByte(s[l.i+1:], '\'')
			if j < 0 {
				l.word.WriteString(s[l.i+1:])
				l.inWord = true
				l.i = len(s)
			} else {
				l.word.WriteString(s[l.i+1 : l.i+1+j])
				l.inWord = true
				l.i += j + 2
			}
		case c == '"':
			l.inWord = true
			l.i++
			for l.i < len(s) && s[l.i] != '"' {
				switch {
				case s[l.i] == '\\' && l.i+1 < len(s):
					if n := s[l.i+1]; n == '$' || n == '`' || n == '"' || n == '\\' {
						l.add(n)
					} else if n != '\n' {
						l.add('\\')
						l.add(n)
					}
					l.i += 2
				case s[l.i] == '$' && l.i+1 < len(s) && s[l.i+1] == '(':
					l.cur.subst = true
					l.substitution(2, '(', ')')
				case s[l.i] == '`':
					l.cur.subst = true
					l.substitution(1, '`', '`')
				default:
					l.add(s[l.i])
					l.i++
				}
			}
			l.i++
		case c == '$' && next == '(':
			l.cur.subst = true
			l.substitution(2, '(', ')')
		case c == '`':
			l.cur.subst = true
			l.substitution(1, '`', '`')
		case (c == '<' || c == '>') && next == '(':
			l.cur.procsub = true
			l.word.WriteByte(c)
			l.i++
			l.substitution(1, '(', ')')
		case c == ' ' || c == '\t' || c == '\r':
			l.endWord()
			l.i++
		case c == '\n':
			l.endSeg("\n")
			l.i++
			l.consumeHeredocs()
		case c == ';':
			l.endSeg(";")
			l.i++
		case c == '&':
			switch {
			case next == '&':
				l.endSeg("&&")
				l.i += 2
			case next == '>' || (l.inWord && (strings.HasSuffix(l.word.String(), ">") || strings.HasSuffix(l.word.String(), "<"))):
				l.add('&') // &>file, 2>&1
				l.i++
			default:
				l.endSeg("&")
				l.i++
			}
		case c == '|':
			switch next {
			case '|':
				l.endSeg("||")
				l.i += 2
			case '&':
				l.endSeg("|")
				l.i += 2
			default:
				l.endSeg("|")
				l.i++
			}
		case c == '(' || c == ')':
			l.endSeg(string(c))
			l.i++
		case c == '<' && next == '<' && !strings.HasPrefix(s[l.i:], "<<<"):
			l.heredocStart()
		default:
			l.add(c)
			l.i++
		}
	}
}

// heredocStart reads "<<[-]WORD" and queues its body, which starts on the next line.
func (l *lexer) heredocStart() {
	s := l.s
	j := l.i + 2
	strip := j < len(s) && s[j] == '-'
	if strip {
		j++
	}
	for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
		j++
	}
	var delim strings.Builder
	for j < len(s) && !strings.ContainsRune(" \t\n;&|<>()", rune(s[j])) {
		if s[j] != '\'' && s[j] != '"' && s[j] != '\\' {
			delim.WriteByte(s[j])
		}
		j++
	}
	l.endWord()
	l.cur.words = append(l.cur.words, "<<"+delim.String())
	l.pending = append(l.pending, heredoc{idx: len(l.segs), delim: delim.String(), strip: strip})
	l.i = j
}

func (l *lexer) consumeHeredocs() {
	for _, h := range l.pending {
		var body []string
		for l.i < len(l.s) {
			end := strings.IndexByte(l.s[l.i:], '\n')
			line := l.s[l.i:]
			if end >= 0 {
				line = l.s[l.i : l.i+end]
				l.i += end + 1
			} else {
				l.i = len(l.s)
			}
			check := line
			if h.strip {
				check = strings.TrimLeft(line, "\t")
			}
			if check == h.delim {
				break
			}
			body = append(body, line)
		}
		if h.idx < len(l.segs) {
			l.segs[h.idx].stdin = append(l.segs[h.idx].stdin, strings.Join(body, "\n"))
		}
	}
	l.pending = nil
}

type redirect struct {
	op     string // ">", ">>", ">|", "&>", "<", "<<<", ...
	target string
}

func (r redirect) writes() bool { return strings.HasPrefix(r.op, ">") || strings.HasPrefix(r.op, "&>") }

var redirWord = regexp.MustCompile(`^(?:[0-9]+|&)?(&>>|&>|>>|>\||>&|>|<<<|<<-?|<&|<>|<)(.*)$`)

// splitRedirects separates redirection operators and their targets from the arguments.
func splitRedirects(words []string) (args []string, redirs []redirect) {
	for i := 0; i < len(words); i++ {
		m := redirWord.FindStringSubmatch(words[i])
		if m == nil {
			args = append(args, words[i])
			continue
		}
		op, target := m[1], m[2]
		if target == "" && i+1 < len(words) {
			i++
			target = words[i]
		}
		if (op == ">&" || op == "<&") && (target == "-" || strings.Trim(target, "0123456789") == "") {
			continue // fd duplication such as 2>&1
		}
		redirs = append(redirs, redirect{op: op, target: target})
	}
	return args, redirs
}
