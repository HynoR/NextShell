// Package commandtemplate implements NextShell 1.x's [#word] placeholders, not shell expansion.
package commandtemplate

import "regexp"

var placeholder = regexp.MustCompile(`\[#([A-Za-z0-9_]+)\]`)

func Keys(text string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, m := range placeholder.FindAllStringSubmatch(text, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}

// Render is a single pass, like v1 commandTemplate.ts: absent values become empty,
// parameters are literal (not recursively expanded, shell-quoted, or persisted).
func Render(text string, params map[string]string) string {
	return placeholder.ReplaceAllStringFunc(text, func(m string) string { return params[m[2:len(m)-1]] })
}
