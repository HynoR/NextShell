package automationcli

import (
	"fmt"
	"strings"
)

// usageError is a command line the command does not take.
type usageError struct{ msg string }

func (e *usageError) Error() string { return e.msg }

func usagef(format string, args ...any) error {
	return &usageError{fmt.Sprintf(format, args...)}
}

// args are a command's arguments: its positional ones and its flags.
type args struct {
	pos   []string
	bools map[string]bool
	vals  map[string]string
}

// flagSpec lists the flags a command takes: true for a flag with a
// value, false for a switch.
type flagSpec map[string]bool

// aliases are the short spellings of flags.
var aliases = map[string]string{"i": "interactive"}

// parseArgs parses a command's arguments, with flags anywhere among them
// (press @e2 --settle), written -f, --f, --f=v or --f v. "--" ends the
// flags; a negative number, as wheel's -120, and "-" alone are
// positional.
func parseArgs(in []string, spec flagSpec) (*args, error) {
	a := &args{bools: map[string]bool{}, vals: map[string]string{}}
	for i := 0; i < len(in); i++ {
		s := in[i]
		if s == "--" {
			a.pos = append(a.pos, in[i+1:]...)
			break
		}
		if len(s) < 2 || s[0] != '-' || (s[1] >= '0' && s[1] <= '9') || s[1] == '.' {
			a.pos = append(a.pos, s)
			continue
		}
		name := strings.TrimLeft(s, "-")
		val, hasVal := "", false
		if k, v, ok := strings.Cut(name, "="); ok {
			name, val, hasVal = k, v, true
		}
		if long, ok := aliases[name]; ok {
			name = long
		}
		takesValue, ok := spec[name]
		if !ok {
			return nil, usagef("unknown flag %s", s)
		}
		switch {
		case takesValue && !hasVal:
			if i+1 >= len(in) {
				return nil, usagef("flag --%s needs a value", name)
			}
			i++
			a.vals[name] = in[i]
		case takesValue:
			a.vals[name] = val
		case hasVal:
			return nil, usagef("flag --%s takes no value", name)
		default:
			a.bools[name] = true
		}
	}
	return a, nil
}

// want checks that the command got between min and max positional
// arguments, max -1 for any number.
func (a *args) want(min, max int, usage string) error {
	if len(a.pos) < min || (max >= 0 && len(a.pos) > max) {
		return usagef("usage: mygo-automation %s", usage)
	}
	return nil
}
