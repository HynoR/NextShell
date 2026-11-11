package model

import "testing"

func TestTerminalURLSafety(t *testing.T) {
	for _, s := range []string{"file:///etc/passwd", "ssh://localhost", "javascript:alert(1)", "https://user:pass@example.com", "/remote/file", "https://example.com\ncommand", "http://"} {
		t.Run(s, func(t *testing.T) {
			if ValidateTerminalURL(s) == nil {
				t.Fatal("unsafe accepted")
			}
		})
	}
	for _, s := range []string{"https://example.com/path?q=1#x", "http://127.0.0.1:8080"} {
		if e := ValidateTerminalURL(s); e != nil {
			t.Fatal(e)
		}
	}
}

func TestAutomaticDownloadNameCannotEscapeConfiguredDirectory(t *testing.T) {
	for _, s := range []string{"", ".", "..", "../../.bashrc", "/tmp/file", "C:\\Windows\\file", "a\ncommand", "a\x00b"} {
		t.Run(s, func(t *testing.T) {
			if ValidateDownloadName(s) == nil {
				t.Fatal("unsafe accepted")
			}
		})
	}
	for _, s := range []string{"report.txt", "中文.txt", "a'b;quoted.txt"} {
		if e := ValidateDownloadName(s); e != nil {
			t.Fatal(e)
		}
	}
}
