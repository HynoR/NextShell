package agent

import (
	"regexp"
	"strings"
)

const redacted = "«redacted»"

// Terminal escape sequences. OSC (title, hyperlinks, clipboard) can carry text the model should not
// see, an unterminated one runs to the end of the text, and CSI is cursor/colour noise.
var (
	osc  = regexp.MustCompile("(?s)\x1b\\][^\x07\x1b]*(?:\x07|\x1b\\\\|$)")
	dcs  = regexp.MustCompile("(?s)\x1b[PX^_].*?(?:\x1b\\\\|$)")
	csi  = regexp.MustCompile("\x1b\\[[0-9;?<=>]*[ -/]*[@-~]")
	esc2 = regexp.MustCompile("\x1b[ -/]*[0-~]")
)

// StripEscapes removes OSC, DCS, CSI and two-byte ESC sequences.
func StripEscapes(s string) string {
	if !strings.ContainsRune(s, 0x1b) {
		return s
	}
	s = osc.ReplaceAllString(s, "")
	s = dcs.ReplaceAllString(s, "")
	s = csi.ReplaceAllString(s, "")
	return esc2.ReplaceAllString(s, "")
}

var (
	pemBlock     = regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`)
	pemOpen      = regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*`) // no END: redact to the end of the text
	authHeader   = regexp.MustCompile(`(?i)\b((?:proxy-)?authorization["']?\s*[:=]\s*["']?(?:basic|bearer|token|negotiate|ntlm|apikey)\s+)[^\s"']+`)
	authDigest   = regexp.MustCompile(`(?i)\b((?:proxy-)?authorization["']?\s*[:=]\s*["']?digest\s+)[^\r\n]+`) // Digest credentials hold quoted parts: redact the line
	bearer       = regexp.MustCompile(`(?i)\b(bearer\s+)[A-Za-z0-9._~+/=\-]+`)
	cookie       = regexp.MustCompile(`(?i)\b((?:set-)?cookie["']?\s*:\s*)[^\r\n"']+`)
	cookieFlag   = regexp.MustCompile(`(?i)(--cookie(?:\s+|=)["']?)[^\s"']+`)
	userFlag     = regexp.MustCompile(`((?:^|\s)(?:-u|-U|--user|--proxy-user)(?:\s+|=)["']?[^:\s"']*:)("[^"]*|'[^']*|[^\s"']+)`)
	mysqlPass    = regexp.MustCompile(`(?i)(\b(?:mysql|mysqldump|mysqladmin|mysqlshow|mysqlimport|mysqlcheck|mariadb|mariadb-dump)\b[^\n;|&]*?\s-p)([^\s]+)`)
	flag         = regexp.MustCompile(`(?i)(--(?:password|passwd|pass|pwd|token|secret|passphrase|api-?key|access-?key|private-?key|client-?secret)(?:[=\s]+))("[^"]*"|'[^']*'|[^\s]+)`)
	uri          = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://[^\s:/@]+:)[^\s@/]+(@)`)
	secretValue  = `("[^"]*"|'[^']*'|[^\s;"']+)`
	segments     = `\b(?:[A-Za-z0-9]+[_.\-])*(?:pass|passwd|password|passwords|pwd|passphrase|secret|secrets|token|tokens|apikey|api[_-]key|access[_-]key|secret[_-]key|private[_-]key|client[_-]secret|credential|credentials|auth)(?:[_.\-][A-Za-z0-9]+)*`
	assignSeg    = regexp.MustCompile(`(?i)(` + segments + `["']?[ \t]*[=:][ \t]*)` + secretValue)
	assignUpper  = regexp.MustCompile(`((?:[A-Z0-9_]*)(?:PASSWORD|PASSWD|PASSPHRASE|SECRET|TOKEN|APIKEY|API_KEY|ACCESS_KEY|PRIVATE_KEY)(?:[A-Z0-9_]*)["']?[ \t]*[=:][ \t]*)` + secretValue)
	assignCamel  = regexp.MustCompile(`(\b[a-z0-9]*(?:Pass|Passwd|Password|Pwd|Secret|Token|ApiKey|AccessKey|PrivateKey|ClientSecret|Passphrase)[A-Za-z0-9]*["']?[ \t]*[=:][ \t]*)` + secretValue)
	wellKnownRes = []*regexp.Regexp{
		regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`),
		regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{20,}`),
		regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}`),
		regexp.MustCompile(`\bsk-[A-Za-z0-9_\-]{20,}`), // also sk-ant-… and sk-proj-…
		regexp.MustCompile(`\bxox[abpr]-[A-Za-z0-9\-]{10,}`),
		regexp.MustCompile(`\bglpat-[A-Za-z0-9_\-]{16,}`),
		regexp.MustCompile(`\beyJ[A-Za-z0-9_\-]{5,}\.eyJ[A-Za-z0-9_\-]{5,}\.[A-Za-z0-9_\-]*`),
	}
)

// Redact removes terminal escape sequences and masks credentials: private keys, Authorization,
// Cookie and Bearer values, key=value pairs whose key names a secret, URL userinfo, common
// password flags, and well-known token formats. It is a filter for text that leaves the app, not a
// guarantee: a secret in a format it does not know survives.
func Redact(s string) string {
	s = StripEscapes(s)
	s = pemBlock.ReplaceAllString(s, redacted)
	s = pemOpen.ReplaceAllString(s, redacted)
	s = authDigest.ReplaceAllString(s, "${1}"+redacted)
	s = authHeader.ReplaceAllString(s, "${1}"+redacted)
	s = bearer.ReplaceAllString(s, "${1}"+redacted)
	s = cookie.ReplaceAllString(s, "${1}"+redacted)
	s = cookieFlag.ReplaceAllString(s, "${1}"+redacted)
	s = userFlag.ReplaceAllString(s, "${1}"+redacted)
	s = mysqlPass.ReplaceAllString(s, "${1}"+redacted)
	s = flag.ReplaceAllString(s, "${1}"+redacted)
	s = uri.ReplaceAllString(s, "${1}"+redacted+"${2}")
	for _, re := range []*regexp.Regexp{assignSeg, assignUpper, assignCamel} {
		s = re.ReplaceAllString(s, "${1}"+redacted)
	}
	for _, re := range wellKnownRes {
		s = re.ReplaceAllString(s, redacted)
	}
	return s
}
