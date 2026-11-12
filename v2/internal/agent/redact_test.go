package agent

import (
	"strings"
	"testing"
)

const topsecret = "TOPSECRET9x"

func TestRedactMasksSecrets(t *testing.T) {
	jwt := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"
	for _, tc := range []struct{ name, in, keep string }{
		{"assignment", "password=" + topsecret, "password="},
		{"colon", "Password: " + topsecret, "Password:"},
		{"json", `{"password": "` + topsecret + `", "user": "bob"}`, `"user": "bob"`},
		{"yaml", "api_key: " + topsecret, "api_key:"},
		{"quoted", `export DB_PASS="` + topsecret + `"`, "export DB_PASS="},
		{"single quoted", "export DB_PASS='" + topsecret + "'", "DB_PASS="},
		{"DB_PASS", "DB_PASS=" + topsecret, "DB_PASS="},
		{"APP-SECRET", "APP-SECRET=" + topsecret, "APP-SECRET="},
		{"pass", "pass=" + topsecret, "pass="},
		{"passwd", "passwd=" + topsecret, "passwd="},
		{"pwd", "pwd=" + topsecret, "pwd="},
		{"secret", "client_secret=" + topsecret, "client_secret="},
		{"token", "GITHUB_TOKEN=" + topsecret, "GITHUB_TOKEN="},
		{"apikey", "apikey=" + topsecret, "apikey="},
		{"api_key", "OPENAI_API_KEY=" + topsecret, "OPENAI_API_KEY="},
		{"api-key", "x-api-key: " + topsecret, "x-api-key:"},
		{"access_key", "aws_access_key=" + topsecret, "aws_access_key="},
		{"private_key", "private_key=" + topsecret, "private_key="},
		{"PGPASSWORD", "PGPASSWORD=" + topsecret + " psql", "PGPASSWORD="},
		{"camelCase", "dbPassword=" + topsecret, "dbPassword="},
		{"suffix", "token_value=" + topsecret, "token_value="},
		{"flag", "mytool --password " + topsecret, "--password"},
		{"flag=", "mytool --password=" + topsecret, "--password="},
		{"flag token", "mytool --api-key=" + topsecret, "--api-key="},
		{"bearer", "curl -H 'Authorization: Bearer " + topsecret + "' https://x", "Authorization: Bearer "},
		{"bearer bare", "Bearer " + topsecret, "Bearer "},
		{"basic", "Authorization: Basic dXNlcjpwYXNzd29yZA==", "Authorization: Basic "},
		{"basic quoted", `curl -H "Authorization: Basic ` + topsecret + `"`, "Authorization: Basic "},
		{"token scheme", "Authorization: Token " + topsecret, "Authorization: Token "},
		{"digest", `Authorization: Digest username="bob", realm="x", response="` + topsecret + `"`, "Authorization: Digest "},
		{"proxy auth", "Proxy-Authorization: Basic " + topsecret, "Proxy-Authorization: Basic "},
		{"cookie", "Cookie: session=" + topsecret + "; theme=dark", "Cookie: "},
		{"set-cookie", "Set-Cookie: sid=" + topsecret + "; Path=/; HttpOnly", "Set-Cookie: "},
		{"curl -u", "curl -u bob:" + topsecret + " https://x", "curl -u bob:"},
		{"curl --user", "curl --user bob:" + topsecret + " https://x", "--user bob:"},
		{"mysql -p", "mysql -h db -u root -p" + topsecret + " app", "mysql -h db -u root -p"},
		{"mysqldump -p", "mysqldump -uroot -p" + topsecret + " db > x.sql", "mysqldump -uroot -p"},
		{"mysql --password", "mysql --password=" + topsecret, "--password="},
		{"uri", "https://bob:" + topsecret + "@host/x", "https://bob:"},
		{"aws access key id", "id AKIA" + "IOSFODNN7EXAMPLE end", "id "},
		{"aws temp key id", "ASIA" + "IOSFODNN7EXAMPLE", ""},
		{"github pat classic", "ghp_" + strings.Repeat("a1", 18), ""},
		{"github oauth", "gho_" + strings.Repeat("b2", 18), ""},
		{"github user", "ghu_" + strings.Repeat("c3", 18), ""},
		{"github server", "ghs_" + strings.Repeat("d4", 18), ""},
		{"github refresh", "ghr_" + strings.Repeat("e5", 18), ""},
		{"github fine-grained", "github_pat_11ABCDEFG0" + strings.Repeat("x", 30), ""},
		{"openai", "sk-" + strings.Repeat("Ab3", 10), ""},
		{"openai project", "sk-proj-" + strings.Repeat("Ab3_", 10), ""},
		{"anthropic", "sk-ant-api03-" + strings.Repeat("Zz9-", 10), ""},
		{"slack bot", "xox" + "b-1234567890-abcdefghij", ""},
		{"slack app", "xox" + "a-1234567890-abcdefghij", ""},
		{"slack user", "xox" + "p-1234567890-abcdefghij", ""},
		{"slack refresh", "xox" + "r-1234567890-abcdefghij", ""},
		{"gitlab", "glpat-" + strings.Repeat("q7", 10), ""},
		{"jwt", "token is " + jwt + " ok", "token is "},
		{"pem", "-----BEGIN OPENSSH PRIVATE KEY-----\n" + topsecret + "\n-----END OPENSSH PRIVATE KEY-----\nafter", "\nafter"},
		{"pem rsa", "-----BEGIN RSA PRIVATE KEY-----\nMIIE" + topsecret + "\n-----END RSA PRIVATE KEY-----", ""},
		{"pem without end", "before\n-----BEGIN PRIVATE KEY-----\nMIIE" + topsecret + "\nmore lines\nand more", "before\n"},
		{"pem truncated screen", "-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAA" + topsecret, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := Redact(tc.in)
			for _, leak := range []string{topsecret, "dXNlcjpwYXNzd29yZA", "AKIA" + "IOSFODNN7EXAMPLE", "ASIA" + "IOSFODNN7EXAMPLE", strings.Repeat("a1", 18), strings.Repeat("b2", 18), strings.Repeat("c3", 18), strings.Repeat("d4", 18), strings.Repeat("e5", 18), strings.Repeat("Ab3", 10), "1234567890-abcdefghij", strings.Repeat("q7", 10), "eyJzdWIi", "SflKxw", "11ABCDEFG0"} {
				if strings.Contains(out, leak) {
					t.Fatalf("leaked %q in %q", leak, out)
				}
			}
			if !strings.Contains(out, redacted) {
				t.Fatalf("nothing redacted: %q", out)
			}
			if tc.keep != "" && !strings.Contains(out, tc.keep) {
				t.Fatalf("lost %q: %q", tc.keep, out)
			}
		})
	}
}

func TestRedactLeavesOrdinaryText(t *testing.T) {
	for _, s := range []string{
		"/usr/local/bin/python3 /home/bob/projects/app/main.py",
		"commit 9fceb02d0ae598e95dc970b74767f19372d61af8 Merge branch 'main'",
		"sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		"6ba7b810-9dad-11d1-80b4-00c04fd430c8 550e8400-e29b-41d4-a716-446655440000",
		"drwxr-xr-x 2 bob bob 4096 Jan  1 12:00 compass",
		"compass=north bypass=true passenger=3 author=bob",
		"ssh -p 2222 bob@host mkdir -p /tmp/x",
		"mysql -h db -u root -p",
		"Last login: Tue Jan  1 12:00:00 2026 from 10.0.0.1",
		"Password:\nLast login: x",
		"risk-averse disk-usage task-runner",
		"/dev/sda1 on / type ext4 (rw,relatime)",
		"user@host:~$ ls -la",
		"error: token expired", // no key/value separator
		"HEAD is now at 4a5b6c7 fix: auth flow",
	} {
		if out := Redact(s); out != s {
			t.Errorf("changed: %q -> %q", s, out)
		}
	}
}

func TestRedactStripsTerminalEscapes(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"\x1b[31mred\x1b[0m", "red"},
		{"\x1b[1;32mok\x1b[m done", "ok done"},
		{"a\x1b]0;window title\x07b", "ab"},
		{"a\x1b]8;;https://evil.example\x1b\\link\x1b]8;;\x1b\\b", "alinkb"},
		{"a\x1b]52;c;c2VjcmV0\x07b", "ab"},
		{"a\x1b]0;unterminated title", "a"},
		{"\x1b[?2004hprompt$ \x1b[?2004l", "prompt$ "},
		{"x\x1bPq;data\x1b\\y", "xy"},
		{"\x1b(Bplain", "plain"},
	} {
		if got := Redact(tc.in); got != tc.want {
			t.Errorf("%q: got %q, want %q", tc.in, got, tc.want)
		}
	}
	// A secret split by colour codes is still found.
	if got := Redact("password=\x1b[31m" + topsecret + "\x1b[0m"); strings.Contains(got, topsecret) {
		t.Errorf("leaked through escapes: %q", got)
	}
}

func TestRedactIsIdempotentAndFast(t *testing.T) {
	in := "password=" + topsecret + "\nAuthorization: Bearer abc\n" + strings.Repeat("plain line of output\n", 20000)
	once := Redact(in)
	if Redact(once) != once {
		t.Fatal("not idempotent")
	}
}
