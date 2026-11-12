package desktop

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"nextshell.dev/v2/internal/agent"
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
	testutil.RequirePOSIXRemote(t)
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
	testutil.RequirePOSIXRemote(t)
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

func codeOf(e error) string {
	var coded *agent.CodedError
	if errors.As(e, &coded) {
		return coded.C
	}
	return ""
}

// waitUntil polls cond for a few seconds.
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatal("timeout waiting for", what)
}

// Closing a session (the tab, or session_close) ends the agent's command on the server first: it is
// killed while the connection is still up, and the caller hears "disconnected", not a timeout.
func TestClosingASessionKillsItsRunningExec(t *testing.T) {
	testutil.RequirePOSIXRemote(t)
	p := desktopFixture(t)
	server := testServer(t)
	s := connectAs(t, p, server, "closing", "utf-8")
	done := make(chan error, 1)
	go func() {
		_, e := agentBackend{p}.Exec(context.Background(), s.ID, "sleep 30")
		done <- e
	}()
	waitUntil(t, "the command to start", func() bool { return server.Execs.Load() == 1 })
	start := time.Now()
	p.CloseSession(s.ID)
	select {
	case e := <-done:
		if codeOf(e) != "disconnected" {
			t.Fatalf("exec answered %v (%q), want disconnected", e, codeOf(e))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("exec kept running after its session was closed")
	}
	waitUntil(t, "the server to kill the command", func() bool { return server.Killed.Load() == 1 }) // the signal went out before the connection closed
	t.Log("exec ended", time.Since(start), "after the tab closed")
}

// A connection that drops during a command is "disconnected", with a hint that the command may have run.
func TestExecOnAConnectionThatDropsIsDisconnected(t *testing.T) {
	testutil.RequirePOSIXRemote(t)
	p := desktopFixture(t)
	server := testServer(t)
	s := connectAs(t, p, server, "dropping", "utf-8")
	done := make(chan error, 1)
	go func() {
		_, e := agentBackend{p}.Exec(context.Background(), s.ID, "sleep 30")
		done <- e
	}()
	waitUntil(t, "the command to start", func() bool { return server.Execs.Load() == 1 })
	server.Drop()
	select {
	case e := <-done:
		if codeOf(e) != "disconnected" || !strings.Contains(e.Error(), "may or may not") {
			t.Fatalf("exec answered %v (%q), want disconnected", e, codeOf(e))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("exec did not notice the lost connection")
	}
	// and a later call on the dead session says the same
	if _, e := (agentBackend{p}).Exec(context.Background(), s.ID, "echo hi"); codeOf(e) != "disconnected" {
		t.Fatal("exec on a dead connection:", e)
	}
}

// Twenty tabs opened at once on one server all connect, although the server drops connections beyond
// its MaxStartups: handshakes towards one endpoint are limited.
func TestParallelOpensToOneServerStayWithinItsMaxStartups(t *testing.T) {
	p := desktopFixture(t)
	server := testServer(t)
	server.SetMaxStartups(8)
	server.SetAuthDelay(100 * time.Millisecond)
	const tabs = 20
	for i := range tabs {
		c := server.Options().Connection
		c.ID, c.Name = "", "tab"+strconv.Itoa(i)
		p.Connect(c)
	}
	opened, failed := 0, ""
	deadline := time.After(30 * time.Second)
	for opened < tabs && failed == "" {
		select {
		case <-deadline:
			t.Fatalf("only %d of %d tabs opened", opened, tabs)
		case <-time.After(10 * time.Millisecond):
		}
		for _, e := range p.Poll() {
			switch {
			case e.Session != nil:
				opened++
				id := e.Session.ID
				t.Cleanup(func() { p.CloseSession(id) })
			case e.Message != "":
				failed = e.Message
			}
		}
	}
	if failed != "" || server.Refused.Load() != 0 {
		t.Fatalf("%d opened, %d connections refused by the server; failure: %s", opened, server.Refused.Load(), failed)
	}
}

func TestHandshakeEOFIsReportedAsRefused(t *testing.T) {
	for _, msg := range []string{"ssh: handshake failed: EOF", "ssh: handshake failed: read tcp 1.2.3.4:5->6.7.8.9:22: read: connection reset by peer"} {
		e := openError(errors.New(msg))
		if codeOf(e) != "refused" || !strings.Contains(e.Error(), "服务器在握手时断开") {
			t.Fatal(msg, e)
		}
	}
	if codeOf(openError(errors.New("dial tcp 1.2.3.4:22: connect: connection refused"))) != "refused" || codeOf(openError(errors.New("ssh: handshake failed: ssh: unable to authenticate"))) != "auth_failed" {
		t.Fatal("other codes changed")
	}
}

// trackDir tells the session's terminal its directory, like a shell with directory tracking does.
func trackDir(t *testing.T, p *Platform, id, dir string) {
	t.Helper()
	u := &url.URL{Scheme: "file", Host: "localhost", Path: dir}
	if e := p.Send(id, "\x1b]7;"+u.String()+"\x07"); e != nil {
		t.Fatal(e)
	}
	waitUntil(t, "the terminal to report "+dir, func() bool { return p.SessionDir(id) == dir })
}

// The command runs as a whole in the tab's directory, whatever operators or comments it has; if the
// directory is gone nothing runs at all and the caller is told which path is missing.
func TestExecRunsInTheTabsDirectoryOrNotAtAll(t *testing.T) {
	testutil.RequirePOSIXRemote(t)
	p := desktopFixture(t)
	server := testServer(t)
	s := connectAs(t, p, server, "cwd", "utf-8")
	b := agentBackend{p}
	dir := filepath.Join(server.Root, "it's here")
	if e := os.Mkdir(dir, 0o700); e != nil {
		t.Fatal(e)
	}
	trackDir(t, p, s.ID, dir)
	for _, tc := range []struct {
		command, out string
		exit         int
	}{
		{"pwd", dir + "\n", 0},
		{"echo a; pwd; echo b", "a\n" + dir + "\nb\n", 0},
		{"false || pwd", dir + "\n", 0},
		{"echo hi # a trailing comment", "hi\n", 0},
		{"pwd & wait", dir + "\n", 0},
		{"exit 3", "", 1}, // the test server reports every failure as 1
		{"cat <<EOF\ninside\nEOF", "inside\n", 0},
	} {
		r, e := b.Exec(context.Background(), s.ID, tc.command)
		if e != nil || r.Output != tc.out || r.ExitCode != tc.exit || r.ActualCWD != dir {
			t.Errorf("%q: %+v %v", tc.command, r, e)
		}
	}
	if e := os.Remove(dir); e != nil {
		t.Fatal(e)
	}
	r, e := b.Exec(context.Background(), s.ID, "echo first; touch ran-anyway")
	if codeOf(e) != "cwd_missing" || !strings.Contains(e.Error(), dir) || r.Output != "" || r.Stderr != "" {
		t.Fatalf("%+v %v", r, e)
	}
	if _, e := os.Stat(filepath.Join(server.Root, "ran-anyway")); e == nil {
		t.Fatal("the command ran although its directory is gone")
	}
	// A tab without directory tracking runs in the login directory and reports no actualCwd.
	u := connectAs(t, p, server, "untracked", "utf-8")
	if r, e := b.Exec(context.Background(), u.ID, "echo ok"); e != nil || r.Output != "ok\n" || r.ActualCWD != "" {
		t.Fatalf("%+v %v", r, e)
	}
}

// Quitting the app does not leave an agent's command running on the server either.
func TestShuttingDownKillsRunningExecs(t *testing.T) {
	testutil.RequirePOSIXRemote(t)
	p := desktopFixture(t)
	server := testServer(t)
	s := connectAs(t, p, server, "quitting", "utf-8")
	done := make(chan error, 1)
	go func() {
		_, e := agentBackend{p}.Exec(context.Background(), s.ID, "sleep 30")
		done <- e
	}()
	waitUntil(t, "the command to start", func() bool { return server.Execs.Load() == 1 })
	p.Shutdown()
	select {
	case e := <-done:
		if codeOf(e) != "disconnected" {
			t.Fatal(e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("exec kept running after shutdown")
	}
	waitUntil(t, "the server to kill the command", func() bool { return server.Killed.Load() == 1 })
}
