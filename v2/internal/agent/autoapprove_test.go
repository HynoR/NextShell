package agent

import (
	"sync/atomic"
	"testing"
	"time"
)

// clock returns a gateway whose time only moves when the test says so.
func clock(f *fake, changed func()) (*Gateway, func(time.Duration)) {
	g := New(f, changed)
	now := time.Now()
	g.now = func() time.Time { return now }
	return g, func(d time.Duration) { now = now.Add(d) }
}

var mutations = []struct {
	tool string
	a    Arguments
	call string // expected backend call prefix
}{
	{"exec", Arguments{Target: "remote", Command: "pwd"}, "exec:pwd"},
	{"session_open", Arguments{Target: "host"}, "open"},
	{"command_save", Arguments{Name: "save", Command: "pwd"}, "save:pwd"},
	{"session_send_signal", Arguments{Target: "remote", Signal: "interrupt"}, "control:"},
	{"session_focus", Arguments{Target: "remote"}, "focus:remote"},
}

func TestAutoApproveIsOffByDefaultAndPromptsAsBefore(t *testing.T) {
	f := &fake{}
	g, _ := clock(f, nil)
	if g.AutoApproval().On {
		t.Fatal("auto-approve must default to off")
	}
	for _, m := range mutations {
		pendingID(t, invoke(g, "owner", m.tool, m.a))
	}
	if len(f.calls) != 0 {
		t.Fatal("mutated without approval", f.calls)
	}
}

func TestAutoApproveRunsEveryMutationWithoutPrompt(t *testing.T) {
	f := &fake{}
	g, _ := clock(f, nil)
	if e := g.SetAutoApprove(15 * time.Minute); e != nil {
		t.Fatal(e)
	}
	for i, m := range mutations {
		r := invoke(g, "owner", m.tool, m.a)
		if !r.OK {
			t.Fatal(m.tool, r.Error)
		}
		if data, ok := r.Data.(map[string]any); ok && data["status"] == "pending" {
			t.Fatal(m.tool, "was queued for approval")
		}
		if len(f.calls) != i+1 || len(f.calls[i]) < len(m.call) || f.calls[i][:len(m.call)] != m.call {
			t.Fatal(m.tool, f.calls)
		}
	}
	if len(g.Pending()) != 0 {
		t.Fatal("approval queue not empty", g.Pending())
	}
	var audited bool
	for _, a := range g.Activity() {
		audited = audited || (a.Tool == "exec" && a.State == "auto-approved" && a.Target != "" && a.Summary != "")
	}
	if !audited {
		t.Fatal("auto-approved call missing from activity log", g.Activity())
	}
}

func TestAutoApproveExpiresAtDeadlineThenPromptsAgain(t *testing.T) {
	f := &fake{}
	g, advance := clock(f, nil)
	if e := g.SetAutoApprove(15 * time.Minute); e != nil {
		t.Fatal(e)
	}
	state := g.AutoApproval()
	if !state.On || state.UntilExit || state.Expires.IsZero() {
		t.Fatal(state)
	}
	advance(15*time.Minute - time.Nanosecond)
	if !g.AutoApproval().On || !invoke(g, "owner", "exec", Arguments{Target: "remote", Command: "pwd"}).OK || len(f.calls) != 1 {
		t.Fatal("expired before deadline")
	}
	advance(time.Nanosecond) // exactly the deadline: expired
	if g.AutoApproval().On {
		t.Fatal("still on at deadline")
	}
	pendingID(t, invoke(g, "owner", "exec", Arguments{Target: "remote", Command: "whoami"}))
	if len(f.calls) != 1 {
		t.Fatal("ran after expiry", f.calls)
	}
	if state = g.AutoApproval(); state.On || !state.Expires.IsZero() {
		t.Fatal("expired state not cleared", state)
	}
}

func TestAutoApproveExpiryIsCheckedPerCallWithoutObserver(t *testing.T) {
	g, advance := clock(&fake{}, nil)
	g.SetAutoApprove(time.Hour)
	advance(time.Hour + time.Second)
	// No AutoApproval() poll in between: the call itself must notice the expiry.
	pendingID(t, invoke(g, "owner", "exec", Arguments{Target: "remote", Command: "pwd"}))
}

func TestAutoApproveUntilExitNeverExpires(t *testing.T) {
	f := &fake{}
	g, advance := clock(f, nil)
	if e := g.SetAutoApprove(UntilExit); e != nil {
		t.Fatal(e)
	}
	advance(24 * 365 * time.Hour)
	state := g.AutoApproval()
	if !state.On || !state.UntilExit || !state.Expires.IsZero() {
		t.Fatal(state)
	}
	if !invoke(g, "owner", "exec", Arguments{Target: "remote", Command: "pwd"}).OK || len(f.calls) != 1 {
		t.Fatal("did not run", f.calls)
	}
}

func TestAutoApproveDoesNotCoverRiskyOrBlockedCalls(t *testing.T) {
	f := &fake{}
	g, _ := clock(f, nil)
	if e := g.SetBlacklist([]string{"custom-danger", "re:^echo forbidden"}); e != nil {
		t.Fatal(e)
	}
	g.SetAutoApprove(UntilExit)
	for _, tc := range []struct {
		name, code string
		a          Arguments
	}{
		{"dangerous", "forbidden", Arguments{Target: "remote", Command: "sudo rm -rf /"}},
		{"blacklist substring", "forbidden", Arguments{Target: "remote", Command: "echo CUSTOM-DANGER"}},
		{"blacklist regexp", "forbidden", Arguments{Target: "remote", Command: "echo forbidden"}},
		{"sensitive without consent", "consent_required", Arguments{Target: "remote", Command: "cat /app/.env"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := invoke(g, "owner", "exec", tc.a)
			if r.OK || r.Error.Code != tc.code {
				t.Fatal(r)
			}
		})
	}
	// With the user's consent a sensitive command is approvable, but only by hand.
	id := pendingID(t, invoke(g, "owner", "exec", Arguments{Target: "remote", Command: "cat .env", AllowSensitive: true}))
	if len(f.calls) != 0 {
		t.Fatal("risky command ran unattended", f.calls)
	}
	if !g.Respond(id, true) {
		t.Fatal("manual approval refused")
	}
	if r := invoke(g, "owner", "exec", Arguments{RequestID: id}); !r.OK || len(f.calls) != 1 || f.calls[0] != "exec:cat .env" {
		t.Fatal(r, f.calls)
	}
	// An ordinary command in the same window still runs on its own.
	if r := invoke(g, "owner", "exec", Arguments{Target: "remote", Command: "uptime"}); !r.OK || len(f.calls) != 2 {
		t.Fatal(r, f.calls)
	}
}

func TestAutoApproveDoesNotApproveRequestsQueuedEarlier(t *testing.T) {
	f := &fake{}
	g, _ := clock(f, nil)
	id := pendingID(t, invoke(g, "owner", "exec", Arguments{Target: "remote", Command: "pwd"}))
	g.SetAutoApprove(UntilExit)
	if r := invoke(g, "owner", "exec", Arguments{RequestID: id}); !r.OK || len(f.calls) != 0 {
		t.Fatal("queued request bypassed the user", r, f.calls)
	}
	if len(g.Pending()) != 1 {
		t.Fatal("request lost")
	}
}

func TestAutoApproveReadOnlyToolsUnaffected(t *testing.T) {
	f := &fake{}
	g, _ := clock(f, nil)
	g.SetAutoApprove(UntilExit)
	for _, tool := range []string{"host_list", "session_list", "command_search", "session_read", "session_history"} {
		if r := invoke(g, "owner", tool, readArgs(tool)); !r.OK {
			t.Fatal(tool, r.Error)
		}
	}
	if len(f.calls) != 0 {
		t.Fatal(f.calls)
	}
}

func TestEmergencyStopTurnsAutoApproveOffAndResumeDoesNotRestoreIt(t *testing.T) {
	f := &fake{}
	g, _ := clock(f, nil)
	g.SetAutoApprove(UntilExit)
	g.Halt(true)
	if g.AutoApproval().On {
		t.Fatal("halt left auto-approve on")
	}
	if invoke(g, "owner", "exec", Arguments{Target: "remote", Command: "pwd"}).OK {
		t.Fatal("call allowed while halted")
	}
	if g.SetAutoApprove(time.Hour) == nil {
		t.Fatal("auto-approve enabled while halted")
	}
	g.Halt(false)
	if g.AutoApproval().On {
		t.Fatal("resume restored auto-approve")
	}
	pendingID(t, invoke(g, "owner", "exec", Arguments{Target: "remote", Command: "pwd"}))
	if len(f.calls) != 0 {
		t.Fatal(f.calls)
	}
}

func TestAutoApproveExplicitOffReplaceAndValidation(t *testing.T) {
	g, advance := clock(&fake{}, nil)
	g.SetAutoApprove(UntilExit)
	if e := g.SetAutoApprove(0); e != nil || g.AutoApproval().On {
		t.Fatal("off failed", e)
	}
	pendingID(t, invoke(g, "owner", "exec", Arguments{Target: "remote", Command: "pwd"}))
	// A new choice replaces the old one: it can both extend and shorten.
	g.SetAutoApprove(time.Hour)
	g.SetAutoApprove(15 * time.Minute)
	advance(16 * time.Minute)
	if g.AutoApproval().On {
		t.Fatal("shorter replacement ignored")
	}
	g.SetAutoApprove(15 * time.Minute)
	g.SetAutoApprove(time.Hour)
	advance(16 * time.Minute)
	if !g.AutoApproval().On {
		t.Fatal("longer replacement ignored")
	}
	for _, d := range []time.Duration{-2, -time.Hour} {
		if g.SetAutoApprove(d) == nil {
			t.Fatal("accepted", d)
		}
	}
	if !g.AutoApproval().On {
		t.Fatal("invalid request changed state")
	}
}

func TestAutoApproveExpiryNotifiesObserverAndLogs(t *testing.T) {
	var changes atomic.Int32
	g := New(&fake{}, func() { changes.Add(1) }) // real clock: exercises the timer
	g.SetAutoApprove(30 * time.Millisecond)
	if !g.AutoApproval().On {
		t.Fatal("not on")
	}
	before := changes.Load()
	deadline := time.Now().Add(2 * time.Second)
	for {
		var logged bool
		for _, a := range g.Activity() {
			logged = logged || a.State == "auto-approve expired"
		}
		if logged {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("expiry never announced")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if changes.Load() == before || g.AutoApproval().On {
		t.Fatal("observer not told / still on")
	}
}

func TestAutoApproveConcurrentCallsAndToggles(t *testing.T) {
	g := New(&fake{}, nil)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			g.SetAutoApprove([]time.Duration{0, UntilExit, time.Hour}[i%3])
			g.AutoApproval()
		}
	}()
	for i := 0; i < 200; i++ {
		invoke(g, "owner", "session_focus", Arguments{Target: "remote"})
	}
	<-done
}
