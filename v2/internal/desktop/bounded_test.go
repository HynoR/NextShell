package desktop

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// The kept prefix stays valid UTF-8 however the writes are split, including a write that ends exactly
// at the limit inside a character and the next one that only sets the flag.
func TestBoundedOutputCutsOnCharacterBoundaries(t *testing.T) {
	text := []byte(strings.Repeat("中文x", 50))
	for limit := 1; limit < 40; limit++ {
		for chunk := 1; chunk <= 7; chunk++ {
			b := &boundedOutput{limit: limit}
			for i := 0; i < len(text); i += chunk {
				b.Write(text[i:min(i+chunk, len(text))])
			}
			if !b.truncated || len(b.data) > limit || len(b.data) < limit-3 || !utf8.Valid(b.data) || !strings.HasPrefix(string(text), string(b.data)) {
				t.Fatalf("limit=%d chunk=%d: %q truncated=%v", limit, chunk, b.data, b.truncated)
			}
		}
	}
}
