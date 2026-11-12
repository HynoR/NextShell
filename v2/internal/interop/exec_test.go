//go:build interop

package interop

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// sshd keeps a command running after its channel closes, so Run used to wait for the remote to finish.
// A context that ends must return in about its own time and take the remote process with it.
func TestExecEndsWithItsContext(t *testing.T) {
	for _, v := range []Variant{Debian, Ubuntu, Alpine} {
		t.Run(v.Name, func(t *testing.T) {
			c := Shared(t, v)
			s := Dial(t, c.Options())
			for i, command := range []string{
				"exec sleep 1000%d",                                                     // the shell is the command
				"cd /tmp && sleep 1000%d; echo not-reached",                             // the shell forks it: only its group gets the signal
				"echo out; echo err >&2; yes | head -c 100000000 | (sleep 1000%d; cat)", // output and children
			} {
				marker := "sleep 1000" + string(rune('0'+i))
				command = strings.ReplaceAll(command, "sleep 1000%d", marker)
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				var out bytes.Buffer
				start := time.Now()
				err := s.Run(ctx, command, nil, &out, &out)
				took := time.Since(start)
				cancel()
				if !errors.Is(err, context.DeadlineExceeded) || took < 1900*time.Millisecond || took > 4*time.Second {
					t.Fatalf("%q: err=%v after %v", command, err, took)
				}
				// pgrep is not in BusyBox: look at /proc, in a way that does not match its own command line
				probe := `for f in /proc/[0-9]*/cmdline; do tr '\0' ' ' < $f 2>/dev/null; echo; done | grep -c '[s]leep 1000` + string(rune('0'+i)) + `' || true`
				var left string
				for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
					if left = strings.TrimSpace(Sh(t, s, probe)); left == "0" {
						break
					}
				}
				if left != "0" {
					t.Errorf("%q: %s remote process(es) still running after the context ended\n%s", command, left, Sh(t, s, "ps -ef 2>/dev/null || ps"))
				}
			}
		})
	}
}
