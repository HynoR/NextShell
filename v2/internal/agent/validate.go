package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// validateArguments checks the JSON arguments of a tool call against that tool's schema, the one served
// to clients, so what is advertised and what is enforced cannot drift. It names the field and the allowed
// range of the first violation. partial skips "required": a call that only continues a pending request
// carries nothing but its requestId.
func validateArguments(schema map[string]any, raw []byte, partial bool) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if e := dec.Decode(&v); e != nil {
		return fmt.Errorf("arguments are not valid JSON")
	}
	if dec.More() {
		return fmt.Errorf("arguments are not valid JSON")
	}
	if v == nil {
		v = map[string]any{}
	}
	return validateValue(schema, v, "", partial)
}

func field(path string) string {
	if path == "" {
		return "arguments"
	}
	return path
}
func joinPath(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}
func intOf(v any) (int, bool) { n, ok := v.(int); return n, ok }

func validateValue(schema map[string]any, v any, path string, partial bool) error {
	switch schema["type"] {
	case "object":
		obj, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("%s must be an object", field(path))
		}
		props, _ := schema["properties"].(map[string]any)
		keys := make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			ps, known := props[k].(map[string]any)
			if !known {
				return fmt.Errorf("unknown argument %q", joinPath(path, k))
			}
			if e := validateValue(ps, obj[k], joinPath(path, k), partial); e != nil {
				return e
			}
		}
		required, _ := schema["required"].([]string)
		for _, k := range required {
			if _, has := obj[k]; !has && !(partial && path == "") {
				return fmt.Errorf("%s is required", joinPath(path, k))
			}
		}
	case "string":
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("%s must be a string", path)
		}
		n := utf8.RuneCountInString(s)
		if lo, ok := intOf(schema["minLength"]); ok && n < lo {
			return fmt.Errorf("%s must not be empty", path)
		}
		if hi, ok := intOf(schema["maxLength"]); ok && n > hi {
			return fmt.Errorf("%s is %d characters, the limit is %d", path, n, hi)
		}
		if values, ok := schema["enum"].([]string); ok && !slices.Contains(values, s) {
			return fmt.Errorf("%s must be one of %s, got %q", path, quoteList(values), s)
		}
		if pat, ok := schema["pattern"].(string); ok {
			if matched, _ := regexp.MatchString(pat, s); !matched {
				return fmt.Errorf("%s must match %s", path, pat)
			}
		}
	case "integer":
		num, ok := v.(json.Number)
		if !ok {
			return fmt.Errorf("%s must be an integer", path)
		}
		n, e := num.Int64()
		if e != nil {
			if f, fe := num.Float64(); fe == nil && f == float64(int64(f)) && f > -1e15 && f < 1e15 {
				n, e = int64(f), nil // 5.0 is an integer too
			}
		}
		if e != nil {
			return fmt.Errorf("%s must be an integer, got %s", path, num)
		}
		lo, hasLo := intOf(schema["minimum"])
		hi, hasHi := intOf(schema["maximum"])
		switch {
		case hasLo && hasHi && (n < int64(lo) || n > int64(hi)):
			return fmt.Errorf("%s must be between %d and %d, got %d", path, lo, hi, n)
		case hasLo && n < int64(lo):
			return fmt.Errorf("%s must be at least %d, got %d", path, lo, n)
		case hasHi && n > int64(hi):
			return fmt.Errorf("%s must be at most %d, got %d", path, hi, n)
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("%s must be true or false", path)
		}
	case "array":
		list, ok := v.([]any)
		if !ok {
			return fmt.Errorf("%s must be an array", path)
		}
		if hi, ok := intOf(schema["maxItems"]); ok && len(list) > hi {
			return fmt.Errorf("%s has %d items, the limit is %d", path, len(list), hi)
		}
		if items, ok := schema["items"].(map[string]any); ok {
			for i, item := range list {
				if e := validateValue(items, item, fmt.Sprintf("%s[%d]", path, i), partial); e != nil {
					return e
				}
			}
		}
	}
	return nil
}

func quoteList(values []string) string {
	q := make([]string, len(values))
	for i, v := range values {
		q[i] = fmt.Sprintf("%q", v)
	}
	return strings.Join(q, ", ")
}
