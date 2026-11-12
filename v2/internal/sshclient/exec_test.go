package sshclient_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"nextshell.dev/v2/internal/sshclient"
	"nextshell.dev/v2/internal/testutil"
)

func session(t *testing.T) (*sshclient.Session, *testutil.Server) {
	t.Helper()
	server, e := testutil.New(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(server.Close)
	s, e := sshclient.Dial(context.Background(), server.Options())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return s, server
}

// sshd keeps a command alive after its channel closes, so Run must not wait for the remote:
// it asks the server to kill the command and returns when the context ends.
func TestRunReturnsWhenTheContextEnds(t *testing.T) {
	s, server := session(t)
	for name, cancel := range map[string]func(context.Context) (context.Context, context.CancelFunc){
		"timeout": func(c context.Context) (context.Context, context.CancelFunc) {
			return context.WithTimeout(c, 300*time.Millisecond)
		},
		"cancel": func(c context.Context) (context.Context, context.CancelFunc) {
			c, stop := context.WithCancel(c)
			time.AfterFunc(300*time.Millisecond, stop)
			return c, stop
		},
	} {
		t.Run(name, func(t *testing.T) {
			killed := server.Killed.Load()
			ctx, stop := cancel(context.Background())
			defer stop()
			var out bytes.Buffer
			start := time.Now()
			e := s.Run(ctx, "echo started; exec sleep 100", nil, &out, &out)
			if took := time.Since(start); took > 3*time.Second {
				t.Fatal("Run waited for the remote command:", took)
			}
			if !errors.Is(e, ctx.Err()) {
				t.Fatalf("want the context's error, got %v", e)
			}
			deadline := time.Now().Add(3 * time.Second)
			for server.Killed.Load() == killed && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			if server.Killed.Load() == killed {
				t.Fatal("the remote command was not sent a KILL signal")
			}
			// The abandoned channel must not write into a buffer the caller owns now.
			if got := out.String(); got != "started\n" {
				t.Fatalf("output %q", got)
			}
		})
	}
}

func TestRunStillReturnsOutputAndExitStatus(t *testing.T) {
	s, _ := session(t)
	var out, stderr bytes.Buffer
	e := s.Run(context.Background(), "echo out; echo err >&2; exit 3", nil, &out, &stderr)
	if out.String() != "out\n" || stderr.String() != "err\n" || e == nil {
		t.Fatalf("%q %q %v", out.String(), stderr.String(), e)
	}
	if b, e := s.Output(context.Background(), "echo hi", 64); e != nil || string(b) != "hi\n" {
		t.Fatalf("%q %v", b, e)
	}
}
