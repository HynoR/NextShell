package agent

import (
	"path"
	"regexp"
	"strings"
)

const redacted = "«redacted»"

var private = regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`)
var bearer = regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._+/=\-]+`)
var assignment = regexp.MustCompile(`(?i)([A-Za-z0-9_.-]*(?:password|passwd|pwd|secret|token|api[_-]?key|access[_-]?key|passphrase|credential|auth)[A-Za-z0-9_.-]*\s*[=:]\s*)("[^"]*"|'[^']*'|[^\s;]+)`)
var flag = regexp.MustCompile(`(?i)(--(?:password|passwd|token|secret|passphrase)[=\s]+)("[^"]*"|'[^']*'|[^\s]+)`)
var uri = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://[^\s:/@]+:)[^\s@/]+(@)`)
var ansi = regexp.MustCompile("\x1b\\[[0-9;?]*[ -/]*[@-~]")

func Redact(s string) string {
	s = private.ReplaceAllString(s, redacted)
	s = bearer.ReplaceAllString(s, "${1}"+redacted)
	s = assignment.ReplaceAllString(s, "${1}"+redacted)
	s = flag.ReplaceAllString(s, "${1}"+redacted)
	s = uri.ReplaceAllString(s, "${1}"+redacted+"${2}")
	return ansi.ReplaceAllString(s, "")
}

var dangerousExe = regexp.MustCompile(`(?:^|[\s;&|()])(?:[A-Za-z0-9_./-]*/)?(?:mkfs(?:\.[a-z0-9]+)?|shutdown|reboot)(?:[\s;&|]|$)`)
var device = regexp.MustCompile(`(?:of=|>{1,2}\s*)(/dev/[^\s;&|]+)`)

func Dangerous(command string, blacklist []string) bool {
	lower := strings.ToLower(strings.NewReplacer("'", "", "\"", "", "\\", "").Replace(command))
	if dangerousExe.MatchString(lower) || strings.Contains(lower, ":(){") || strings.Contains(lower, ": () {") {
		return true
	}
	for _, m := range device.FindAllStringSubmatch(lower, -1) {
		if m[1] != "/dev/null" && m[1] != "/dev/stdout" && m[1] != "/dev/stderr" && !strings.HasPrefix(m[1], "/dev/fd/") {
			return true
		}
	}
	parts := strings.Fields(strings.NewReplacer(";", " ", "&", " ", "|", " ", "(", " ", ")", " ").Replace(lower))
	for i, p := range parts {
		if path.Base(p) == "rm" {
			recursive, force, root := false, false, false
			for _, arg := range parts[i+1:] {
				if arg == "--recursive" || strings.HasPrefix(arg, "-") && strings.Contains(arg, "r") {
					recursive = true
				}
				if arg == "--force" || strings.HasPrefix(arg, "-") && strings.Contains(arg, "f") {
					force = true
				}
				if path.Clean(arg) == "/" || arg == "/*" {
					root = true
				}
			}
			if recursive && force && root {
				return true
			}
		}
		if path.Base(p) == "kill" && strings.Contains(lower, "-1") && (strings.Contains(lower, "-9") || strings.Contains(lower, "-kill")) {
			return true
		}
		if path.Base(p) == "chmod" && strings.Contains(lower, "777") && strings.Contains(lower, "-r") {
			for _, arg := range parts[i+1:] {
				if arg == "/" || arg == "/*" {
					return true
				}
			}
		}
	}
	for _, p := range blacklist {
		p = strings.TrimSpace(p)
		if p != "" {
			if strings.Contains(command, p) {
				return true
			}
			if re, e := regexp.Compile(p); e == nil && re.MatchString(command) {
				return true
			}
		}
	}
	return false
}

var sensitive = regexp.MustCompile(`(?:^|[\s/'"<>])\.env(?:\.[A-Za-z0-9._-]+)?(?:$|[\s;'"&|<>])`)

func Sensitive(s string) bool { return sensitive.MatchString(s) }
