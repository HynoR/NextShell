package desktop

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/model"
	"nextshell.dev/v2/internal/monitor"
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

// Joining a pending attempt and outliving the caller's timeout are the gateway's job (see
// agent/gateway2_test.go); here the backend marks the dialog and tells the user when it fails.
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
	go b.Open(context.Background(), c.ID, nil)
	dialog := awaitEvent(t, p, func(e appui.Event) bool { return e.Prompt != nil }).Prompt
	if !dialog.ByAgent {
		t.Fatalf("%+v", dialog)
	}
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
	again, stop := context.WithCancel(context.Background())
	defer stop()
	go b.Open(again, c.ID, nil)
	if next := awaitEvent(t, p, func(e appui.Event) bool { return e.Prompt != nil }).Prompt; !next.ByAgent {
		t.Fatal("a fresh attempt should ask again")
	}
}

// Events wait for the window's next frame; a hidden window draws none, so periodic ones must not pile up.
func TestMonitorSnapshotsDoNotPileUpWhileNoFrameDrainsThem(t *testing.T) {
	p := desktopFixture(t)
	for i := range 500 {
		p.emit(appui.Event{Monitor: &appui.MonitorResult{Session: "a", Snapshot: monitor.Snapshot{Load: strconv.Itoa(i)}}})
		p.emit(appui.Event{Monitor: &appui.MonitorResult{Session: "b"}})
	}
	p.emit(appui.Event{Message: "kept"})
	events := p.Poll()
	if len(events) != 3 || events[0].Monitor.Session != "a" || events[0].Monitor.Snapshot.Load != "499" || events[2].Message != "kept" {
		t.Fatalf("%d events, first %+v", len(events), events[0].Monitor)
	}
}
