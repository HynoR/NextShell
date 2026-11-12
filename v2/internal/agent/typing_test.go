package agent

import "testing"

// typeAll feeds the calls one after the other and returns the verdict of the last one.
func typeAll(alt bool, calls ...string) (Verdict, typedLine) {
	var st typedLine
	var v Verdict
	for _, c := range calls {
		v, st = scanTyping(st, c, nil, alt)
	}
	return v, st
}

// At a shell prompt everything that edits the line in a way the tracker cannot follow ends in a manual
// approval at Enter, and the keys that run a line (Ctrl-J, Ctrl-M, Ctrl-O, Alt-Enter) are Enter.
func TestTypingAtAPromptDoesNotTrustEditingKeys(t *testing.T) {
	for name, calls := range map[string][]string{
		"ctrl-t transpose":      {"echo hi\x14\r"},
		"ctrl-_ undo":           {"echo hi\x1f\r"},
		"ctrl-l":                {"echo hi\x0c\r"},
		"ctrl-v quoted insert":  {"echo hi\x16\r"},
		"ctrl-x chord":          {"echo hi\x18\x05\r"},
		"ctrl-a home":           {"echo hi\x01\r"},
		"ctrl-r search":         {"\x12\r"},
		"ctrl-y yank":           {"\x19\r"},
		"tab completion":        {"cat /etc/pa\t\r"},
		"nul":                   {"echo hi\x00\r"},
		"ctrl-k keeps the line": {"echo hi\x0b\r"},
		"arrow keys":            {"echo hi\x1b[D\r"},
		"alt+key":               {"echo hi\x1bd\r"},
		"bare esc then a key":   {"echo hi\x1b", "\r"},
		"ctrl-d on a line":      {"echo hi\x04\r"},
		"ctrl-o leaves history": {"echo hi\x0f\r"},
		"cursor key, ctrl-u":    {"echo hi\x1b[D\x15\r"},
		"split over calls":      {"echo hi", "\x14", "\r"},
	} {
		v, _ := typeAll(false, calls...)
		if !v.Manual() {
			t.Errorf("%s: no manual approval (%+v)", name, v)
		}
	}
	// Ctrl-K at the end of a typed line is not a way to hide it from the tracker.
	if v, _ := typeAll(false, "reboot\x0b\r"); !v.Forbidden {
		t.Error("ctrl-k hid a forbidden line", v)
	}
}

func TestTypingEnterKeysRunTheLine(t *testing.T) {
	for name, line := range map[string]string{"ctrl-j": "reboot\n", "ctrl-m": "reboot\r", "ctrl-o": "reboot\x0f", "alt-enter": "reboot\x1b\r", "alt-ctrl-j": "reboot\x1b\n"} {
		if v, _ := typeAll(false, line); !v.Forbidden {
			t.Errorf("%s did not judge the line: %+v", name, v)
		}
	}
	// Ctrl-O leaves the next history line in the buffer.
	if v, st := typeAll(false, "echo hi\x0f"); v.Manual() || !st.unknown {
		t.Errorf("ctrl-o: %+v %+v", v, st)
	}
	if v, _ := typeAll(false, "echo hi\x0f", "\r"); !v.Risky {
		t.Error("the recalled line must need an approval")
	}
}

func TestTypingKnownEditingStaysKnown(t *testing.T) {
	for name, calls := range map[string][]string{
		"plain":              {"ls -la\r"},
		"backspace":          {"lss\x7f -la\r"},
		"ctrl-c":             {"reboot", "\x03", "ls\r"},
		"ctrl-u":             {"sudo reboot\x15ls\r"},
		"ctrl-w":             {"ls -la /tmp\x17\r"},
		"ctrl-d on an empty": {"\x04"},
		"split":              {"ls ", "-la", "\r"},
		"utf-8":              {"echo 中文\r"},
	} {
		if v, st := typeAll(false, calls...); v.Manual() || v.Forbidden || st.unknown {
			t.Errorf("%s: %+v %+v", name, v, st)
		}
	}
	// Ctrl-U after a cursor key kills only part of the line.
	if v, _ := typeAll(false, "reboot\x1b[D\x0b", "t\r"); !v.Manual() {
		t.Error("cursor keys then kill must stay unknown")
	}
}

// Full-screen programs own the control keys: tmux prefixes, vim's Esc and ex commands are ordinary input,
// while a line ended by Enter is still judged.
func TestTypingOnTheAlternateScreenLeavesControlKeysAlone(t *testing.T) {
	for name, calls := range map[string][]string{
		"tmux split":         {"\x02%"},
		"tmux prefix then c": {"\x02", "c"},
		"tmux detach":        {"\x02d"},
		"vim write quit":     {"\x1b:wq\r"},
		"vim insert":         {"ihello\x1b", ":w\r"},
		"vim arrows":         {"\x1b[A\x1b[B\x1b[D\x1b[C", "jjj"},
		"htop keys":          {"\x1b[5~", "\x1bOP", "q"},
		"ctrl keys":          {"\x01\x05\x06\x0c\x12\x14\x16\x18\x1f"},
		"ctrl-o jump":        {"\x0f"},
		"tab":                {"\t"},
		"enter alone":        {"\r"},
		"alt key":            {"\x1bd"},
	} {
		if v, st := typeAll(true, calls...); v.Manual() || v.Forbidden || st.unknown {
			t.Errorf("%s: %+v %+v", name, v, st)
		}
	}
	for name, line := range map[string]string{"forbidden": "rm -rf /\r", "forbidden after esc": "\x1breboot\r", "risky": "sudo ls\r", "sensitive": "cat .env\r"} {
		v, _ := typeAll(true, line)
		if !v.Forbidden && !v.Risky && !v.Sensitive {
			t.Errorf("alternate screen line %s not judged: %+v", name, v)
		}
	}
}

// What was typed on one screen does not prefix a line on the other.
func TestTypingStartsAFreshLineWhenTheScreenChanges(t *testing.T) {
	_, st := scanTyping(typedLine{}, "x", nil, true)
	if v, _ := scanTyping(st, "rm -rf /\r", nil, false); !v.Forbidden {
		t.Fatal("a stale prefix from the alternate screen hid the command", v)
	}
	_, st = scanTyping(typedLine{}, "echo hi\x14", nil, false)
	if v, st := scanTyping(st, "\r", nil, true); v.Manual() || st.unknown {
		t.Fatal("unknown state leaked into the full-screen program", v)
	}
}

func TestSendTextThroughTheGatewayOnTheAlternateScreen(t *testing.T) {
	b := newRich()
	g := New(b, nil)
	g.SetAutoApprove(UntilExit)
	// At the prompt Ctrl-b is an editing key: the next Enter is a manual approval.
	data(t, call(g, "o", "session_send_text", m{"target": "s1", "keys": []string{"Ctrl-b"}}))
	if d := data(t, call(g, "o", "session_send_text", m{"target": "s1", "text": "ls\\r"})); d["status"] != "pending" || d["tier"] != "risky" {
		t.Fatal("Ctrl-b at a prompt must make the line unknown", d)
	}
	g.Respond(g.Pending()[0].ID, false)
	data(t, call(g, "o", "session_send_signal", m{"target": "s1", "signal": "interrupt"}))
	b.alt.Store(true)
	n := len(b.Calls())
	data(t, call(g, "o", "session_send_text", m{"target": "s1", "text": "\\x02%"}))
	data(t, call(g, "o", "session_send_text", m{"target": "s1", "text": "\\e:wq\\r"}))
	if got := b.Calls()[n:]; len(got) != 2 || got[0] != `control:"\x02%"` {
		t.Fatal("tmux and vim input needs no approval:", got)
	}
	if c := code(t, call(g, "o", "session_send_text", m{"target": "s1", "text": "reboot\\r"})); c != "forbidden" {
		t.Fatal("lines are still judged on the alternate screen:", c)
	}
}
