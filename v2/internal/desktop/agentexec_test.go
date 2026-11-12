package desktop

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/model"
	"nextshell.dev/v2/internal/testutil"
)

// connectAs opens a session to server under the connection's name and encoding.
func connectAs(t *testing.T, p *Platform, server *testutil.Server, name, encoding string) *appui.Session {
	t.Helper()
	c := server.Options().Connection
	c.Name, c.Encoding = name, encoding
	p.Connect(c)
	s := awaitEvent(t, p, func(e appui.Event) bool { return e.Session != nil }).Session
	t.Cleanup(func() { p.CloseSession(s.ID) })
	return s
}

func testServer(t *testing.T) *testutil.Server {
	t.Helper()
	server, e := testutil.New(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(server.Close)
	return server
}

// Output past the cap is cut and flagged, and the command still runs to its end.
func TestAgentExecKeepsTheStartOfTooMuchOutput(t *testing.T) {
	p := desktopFixture(t)
	s := connectAs(t, p, testServer(t), "big", "utf-8")
	b := agentBackend{p}
	const limit = 512 << 10
	start := time.Now()
	r, e := b.Exec(context.Background(), s.ID, "head -c 700000 /dev/zero | tr '\\0' x; echo done >&2; yes 中文 | head -c 600000 >&2")
	if e != nil || r.ExitCode != 0 {
		t.Fatal(e, r.ExitCode)
	}
	if !r.Truncated || len(r.Output) != limit || strings.Trim(r.Output, "x") != "" {
		t.Fatalf("stdout: truncated=%v len=%d", r.Truncated, len(r.Output))
	}
	if !strings.HasPrefix(r.Stderr, "done\n中文\n") || len(r.Stderr) > limit || len(r.Stderr) < limit-3 || !utf8.ValidString(r.Stderr) {
		t.Fatalf("stderr: len=%d valid=%v", len(r.Stderr), utf8.ValidString(r.Stderr))
	}
	t.Log("exec with 1.3 MB of output took", time.Since(start))

	r, e = b.Exec(context.Background(), s.ID, "echo small")
	if e != nil || r.Truncated || r.Output != "small\n" {
		t.Fatal(r, e)
	}
}

func TestBoundedOutputCutsAtACharacter(t *testing.T) {
	o := &boundedOutput{limit: 7}
	for _, chunk := range []string{"ab", "中文", "tail"} {
		if n, e := o.Write([]byte(chunk)); n != len(chunk) || e != nil {
			t.Fatal(n, e)
		}
	}
	if o.String() != "ab中" || !o.truncated { // "文" would cross byte 7
		t.Fatalf("%q %v", o.String(), o.truncated)
	}
	if o = (&boundedOutput{limit: 4}); true {
		o.Write([]byte("abcd"))
		if o.truncated || o.String() != "abcd" {
			t.Fatal("output of exactly the limit is not truncated")
		}
	}
}

// The host's encoding applies to exec like it does to the terminal: the command goes out in it,
// the output comes back decoded.
func TestAgentExecUsesTheHostEncoding(t *testing.T) {
	p := desktopFixture(t)
	server := testServer(t)
	s := connectAs(t, p, server, "gbk host", "gbk")
	r, e := agentBackend{p}.Exec(context.Background(), s.ID, `printf '\326\320\316\304'; printf '中文' | od -An -tx1 | tr -d ' \n' >&2`)
	if e != nil || r.Output != "中文" || r.Stderr != "d6d0cec4" {
		t.Fatalf("%+v %v", r, e)
	}
	u := connectAs(t, p, server, "utf8 host", "utf-8")
	r, e = agentBackend{p}.Exec(context.Background(), u.ID, `printf '中文'; printf '\377'`)
	if e != nil || r.Output != "中文\xff" { // the bytes are untouched; the JSON encoder marks the invalid one with U+FFFD
		t.Fatalf("%q %v", r.Output, e)
	}
}

// session_list is ordered by creation, a reconnect keeps its predecessor's place, and entries say what
// the title alone cannot.
func TestAgentSessionsAreOrderedAndDescribed(t *testing.T) {
	p := desktopFixture(t)
	server := testServer(t)
	var ids []string
	for _, name := range []string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta"} {
		ids = append(ids, connectAs(t, p, server, name, "gbk").ID)
	}
	p.Local()
	local := awaitEvent(t, p, func(e appui.Event) bool { return e.Session != nil }).Session
	t.Cleanup(func() { p.CloseSession(local.ID) })
	check := func(want []string) {
		t.Helper()
		for range 20 {
			var got []string
			for _, s := range (agentBackend{p}).Sessions() {
				got = append(got, s.ID)
			}
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Fatalf("order %v, want %v", got, want)
			}
		}
	}
	check(append(append([]string{}, ids...), local.ID))
	list := agentBackend{p}.Sessions()
	if s := list[1]; s.Name != "beta" || s.Host != "127.0.0.1" || s.Encoding != "gbk" || s.Cols <= 0 || s.Rows <= 0 || s.Local {
		t.Fatalf("%+v", s)
	}
	if s := list[6]; !s.Local || s.Name != "" || s.Host != "" || s.Encoding != "" || s.Cols <= 0 {
		t.Fatalf("%+v", s)
	}

	// Reconnecting the third tab puts the new session where it was; closing one drops it.
	p.Reconnect(&appui.Session{ID: ids[2], Connection: model.Connection{}})
	again := awaitEvent(t, p, func(e appui.Event) bool { return e.Session != nil }).Session
	t.Cleanup(func() { p.CloseSession(again.ID) })
	p.CloseSession(ids[2])
	p.CloseSession(ids[0])
	check([]string{ids[1], again.ID, ids[3], ids[4], ids[5], local.ID})
}

// A host-key dialog an agent's session_open raised says so and is not tied to the call: it outlives the
// call's timeout, a second request for the same host joins it instead of raising another, and answering
// it completes the connection.
func TestAgentOpenKeepsItsDialogAndJoinsLaterRequests(t *testing.T) {
	p := desktopFixture(t)
	server := testServer(t)
	c := server.Options().Connection
	c.ID, c.Fingerprint = p.NewID(), "" // unknown host: the first connection asks
	secret := "test-password"
	if e := p.SaveConnection(c, &secret); e != nil {
		t.Fatal(e)
	}
	b := agentBackend{p}

	short, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if _, e := b.Open(short, c.ID); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal("the call should have timed out waiting for the user:", e)
	}
	dialog := awaitEvent(t, p, func(e appui.Event) bool { return e.Prompt != nil }).Prompt
	if dialog.Kind != appui.PromptHostKey || !dialog.ByAgent {
		t.Fatalf("%+v", dialog)
	}
	time.Sleep(300 * time.Millisecond)
	select {
	case <-dialog.Done:
		t.Fatal("the dialog went away with the call that raised it")
	default:
	}

	// A later request for the same host joins the open attempt: no second dialog.
	opened := make(chan string, 1)
	go func() {
		id, e := b.Open(context.Background(), c.ID)
		if e != nil {
			t.Error(e)
		}
		opened <- id
	}()
	time.Sleep(100 * time.Millisecond)
	for _, e := range p.Poll() {
		if e.Prompt != nil {
			t.Fatal("a second dialog for the same host")
		}
	}
	dialog.Answers <- []string{} // trust the key
	var id string
	select {
	case id = <-opened:
	case <-time.After(5 * time.Second):
		t.Fatal("answering the dialog did not finish the connection")
	}
	if sessions := b.Sessions(); len(sessions) != 1 || sessions[0].ID != id || !sessions[0].Connected {
		t.Fatalf("%+v %q", sessions, id)
	}
	t.Cleanup(func() { p.CloseSession(id) })
}

func TestAgentOpenCancelledDialogReportsToTheUserAndAllowsANewAttempt(t *testing.T) {
	p := desktopFixture(t)
	server := testServer(t)
	c := server.Options().Connection
	c.ID, c.Fingerprint = p.NewID(), ""
	secret := "test-password"
	if e := p.SaveConnection(c, &secret); e != nil {
		t.Fatal(e)
	}
	b := agentBackend{p}
	short, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	b.Open(short, c.ID)
	dialog := awaitEvent(t, p, func(e appui.Event) bool { return e.Prompt != nil }).Prompt
	dialog.Answers <- nil // the user cancels
	for deadline := time.Now().Add(3 * time.Second); ; {
		var told bool
		for _, e := range p.Poll() {
			told = told || strings.Contains(e.Message, "Agent 发起的连接")
		}
		if told {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the user was not told that the agent's connection failed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	again, stop := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer stop()
	b.Open(again, c.ID)
	if next := awaitEvent(t, p, func(e appui.Event) bool { return e.Prompt != nil }).Prompt; !next.ByAgent {
		t.Fatal("a fresh attempt should ask again")
	}
}
