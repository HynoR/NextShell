package sshclient_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"nextshell.dev/v2/internal/sshclient"
	"nextshell.dev/v2/internal/testutil"
)

func session(t *testing.T) (*sshclient.Session, *testutil.Server) { return sessionWith(t, "utf-8") }
func sessionWith(t *testing.T, encoding string) (*sshclient.Session, *testutil.Server) {
	t.Helper()
	server, e := testutil.New(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(server.Close)
	o := server.Options()
	o.Connection.Encoding = encoding
	s, e := sshclient.Dial(context.Background(), o)
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

func TestRunTextUsesTheHostEncoding(t *testing.T) {
	for _, tc := range []struct{ encoding, text, wire string }{
		{"gbk", "中文", "d6d0cec4"},
		{"big5", "中文", "a4a4a4e5"},
		{"utf-8", "中文", "e4b8ade69687"},
	} {
		t.Run(tc.encoding, func(t *testing.T) {
			server, e := testutil.New(t.TempDir())
			if e != nil {
				t.Fatal(e)
			}
			defer server.Close()
			o := server.Options()
			o.Connection.Encoding = tc.encoding
			s, e := sshclient.Dial(context.Background(), o)
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			// The command carries a non-ASCII argument: the server must receive it in the host encoding
			// (od shows the bytes), and what printf writes back must be decoded to UTF-8.
			var out, stderr bytes.Buffer
			command := "printf '" + tc.text + "' | od -An -tx1 | tr -d ' \\n'; echo; printf '" + tc.text + "'; printf '" + tc.text + "' >&2"
			if e := s.RunText(context.Background(), command, &out, &stderr); e != nil {
				t.Fatal(e)
			}
			if want := tc.wire + "\n" + tc.text; out.String() != want || stderr.String() != tc.text {
				t.Fatalf("stdout %q stderr %q, want %q and %q", out.String(), stderr.String(), want, tc.text)
			}
		})
	}
}

func TestRunTextDecodesAcrossWritesAndMarksInvalidBytes(t *testing.T) {
	s, _ := sessionWith(t, "gbk")
	var out bytes.Buffer
	// the first byte of a two-byte character is flushed alone, then 0xff (invalid) ends the output
	command := `printf '\326'; sleep 0.2; printf '\320\377'`
	if e := s.RunText(context.Background(), command, &out, io.Discard); e != nil {
		t.Fatal(e)
	}
	if out.String() != "中�" {
		t.Fatalf("%q", out.String())
	}
	if e := s.RunText(context.Background(), "echo '\U0001F600'", io.Discard, io.Discard); e == nil {
		t.Fatal("a command the host encoding cannot represent was sent")
	}
}
