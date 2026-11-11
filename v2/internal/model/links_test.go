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
