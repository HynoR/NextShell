package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// nested builds {"a": {"b": v}} for the path "a.b".
func nested(path string, v any) json.RawMessage {
	parts := strings.Split(path, ".")
	for i := len(parts) - 1; i >= 0; i-- {
		v = map[string]any{parts[i]: v}
	}
	raw, _ := json.Marshal(v)
	return raw
}

// Every number, string length, enum, pattern and array bound in every tool's schema is enforced: the
// test walks the schemas, so a new field is covered the moment it is declared.
func TestArgumentsAreValidatedAgainstTheSchemasTheyAdvertise(t *testing.T) {
	checked := 0
	var walk func(tool, path string, schema map[string]any)
	walk = func(tool, path string, schema map[string]any) {
		props, _ := schema["properties"].(map[string]any)
		for name, raw := range props {
			ps := raw.(map[string]any)
			p := name
			if path != "" {
				p = path + "." + name
			}
			sp := spec(tool)
			check := func(what string, v any, wantOK bool, want string) {
				t.Helper()
				checked++
				e := validateArguments(sp.Schema, nested(p, v), true)
				if e != nil && strings.HasSuffix(e.Error(), " is required") {
					e = nil // a sibling of a nested field; the field itself passed
				}
				if (e == nil) != wantOK {
					t.Errorf("%s %s %s = %v: err=%v", tool, p, what, v, e)
				} else if e != nil && !strings.Contains(e.Error(), want) {
					t.Errorf("%s %s %s: message %q lacks %q", tool, p, what, e, want)
				}
			}
			switch ps["type"] {
			case "integer":
				lo, hi := ps["minimum"].(int), ps["maximum"].(int)
				rng := fmt.Sprintf("between %d and %d", lo, hi)
				check("min", lo, true, "")
				check("max", hi, true, "")
				check("below", lo-1, false, p+" must be "+rng)
				check("above", hi+1, false, p+" must be "+rng)
				check("float", 1.5, false, p)
				check("string", "5", false, p)
			case "string":
				if hi, ok := ps["maxLength"].(int); ok {
					check("long", strings.Repeat("x", hi+1), false, p)
					if pat, ok := ps["pattern"].(string); !ok || pat == "" {
						check("max length", strings.Repeat("x", hi), true, "")
					}
				}
				if _, ok := ps["minLength"].(int); ok {
					check("empty", "", false, p+" must not be empty")
				}
				if values, ok := ps["enum"].([]string); ok {
					check("enum", values[0], true, "")
					check("bad enum", "nonsense", false, p+" must be one of")
				}
				check("number", 7, false, p)
			case "boolean":
				check("bool", true, true, "")
				check("string", "yes", false, p)
			case "array":
				hi := ps["maxItems"].(int)
				check("full", make([]string, hi), true, "")
				check("too many", make([]string, hi+1), false, p)
				check("not an array", "x", false, p)
			case "object":
				check("not an object", "x", false, p)
				walk(tool, p, ps)
				continue
			}
		}
	}
	for _, s := range Specs() {
		walk(s.Name, "", s.Schema)
	}
	if checked < 150 {
		t.Fatalf("only %d checks ran: did the walker stop early?", checked)
	}
}

func TestArgumentValidationMessages(t *testing.T) {
	g := New(&fake{}, nil)
	for _, tc := range []struct {
		tool string
		args string
		want string
	}{
		{"exec", `{"target":"remote","command":"ls","timeoutSec":500}`, "timeoutSec must be between 1 and 120, got 500"},
		{"exec", `{"target":"remote","command":"ls","timeoutSec":0}`, "timeoutSec must be between 1 and 120, got 0"},
		{"exec", `{"target":"remote","command":"ls","mode":"interactive"}`, `mode must be one of "background", got "interactive"`},
		{"exec", `{"target":"remote"}`, "command is required"},
		{"exec", `{"target":"remote","command":""}`, "command must not be empty"},
		{"exec", `{"target":"remote","command":"ls","cwd":"/tmp"}`, `unknown argument "cwd"`},
		{"host_list", `{"limit":0}`, "limit must be between 1 and 200"},
		{"host_list", `{"limit":201}`, "limit must be between 1 and 200, got 201"},
		{"host_list", `{"offset":-1}`, "offset must be between 0 and 1000000, got -1"},
		{"command_search", `{"limit":"ten"}`, "limit must be an integer"},
		{"session_read", `{"target":"remote","waitFor":{"pattern":"x","timeoutSec":61}}`, "waitFor.timeoutSec must be between 1 and 60, got 61"},
		{"session_read", `{"target":"remote","waitFor":{"timeoutSec":5}}`, "waitFor.pattern is required"},
		{"session_read", `{"target":"remote","waitFor":{"pattern":"x","bogus":1}}`, `unknown argument "waitFor.bogus"`},
		{"session_read", `{"target":"remote","lines":2001}`, "lines must be between 1 and 2000"},
		{"session_read", `{"target":"remote","sinceCursor":"abc"}`, "sinceCursor must match"},
		{"session_read", `{"target":"remote","mode":"stream"}`, `mode must be one of "screen"`},
		{"session_send_signal", `{"target":"remote","signal":"kill"}`, "signal must be one of"},
		{"session_send_text", `{"target":"remote","keys":["Enter",7]}`, "keys[1] must be a string"},
		{"exec", `[1]`, "arguments must be an object"},
		{"exec", `{"target":"remote","command":"ls"} {}`, "not valid JSON"},
	} {
		r := g.Call(t.Context(), "o", "c", tc.tool, json.RawMessage(tc.args))
		if r.OK || r.Error.Code != "invalid_argument" || !strings.Contains(r.Error.Message, tc.want) {
			t.Errorf("%s %s: %+v, want %q", tc.tool, tc.args, r, tc.want)
		}
	}
	// A continuation carries only its requestId; null and {} are no arguments.
	if r := g.Call(t.Context(), "o", "c", "exec", json.RawMessage(`{"requestId":"nope"}`)); r.OK || r.Error.Code != "not_found" {
		t.Errorf("requestId-only call: %+v", r)
	}
	if r := g.Call(t.Context(), "o", "c", "session_list", json.RawMessage(`null`)); !r.OK {
		t.Errorf("null arguments: %+v", r)
	}
}
