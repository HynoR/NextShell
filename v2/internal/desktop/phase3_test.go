package desktop

import (
	"context"
	"encoding/json"
	"github.com/egoist/mygo/plugins/terminal"
	"net/http"
	"net/http/httptest"
	"net/url"
	"nextshell.dev/v2/internal/agent"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/cloudhttp"
	"nextshell.dev/v2/internal/cloudv2"
	"nextshell.dev/v2/internal/remoteedit"
	"nextshell.dev/v2/internal/testutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAgentPlatformRealSSHExecCWDRedactionAndFocus(t *testing.T) {
	testutil.RequirePOSIXRemote(t)
	p := desktopFixture(t)
	session, root := phase2SSH(t, p)
	p.AgentHalt(false)
	dir := filepath.Join(root, "space ' quote")
	if e := os.Mkdir(dir, 0700); e != nil {
		t.Fatal(e)
	}
	cwd := (&url.URL{Scheme: "file", Host: "localhost", Path: dir}).String()
	if e := p.Send(session, "\x1b]7;"+cwd+"\x07\x1b]0;agent test title\x07"); e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(2 * time.Second)
	for p.SessionDir(session) != dir || p.TerminalTitle(session) != "agent test title" {
		if time.Now().After(deadline) {
			t.Fatal("terminal API did not report OSC7/title", p.SessionDir(session), p.TerminalTitle(session))
		}
		time.Sleep(time.Millisecond)
	}
	command := "printf 'token=PRIVATE-SSH-SECRET\\n'; pwd; printf warning >&2"
	raw, _ := json.Marshal(agent.Arguments{Target: session, Command: command})
	r := p.gateway.Call(context.Background(), "test-owner", "test-client", "exec", raw)
	if !r.OK || len(p.AgentOverview().Pending) != 1 {
		t.Fatal(r)
	}
	pending := p.AgentOverview().Pending[0]
	p.AgentRespond(pending.ID, true)
	poll, _ := json.Marshal(agent.Arguments{RequestID: pending.ID})
	r = p.gateway.Call(context.Background(), "test-owner", "test-client", "exec", poll)
	if !r.OK {
		t.Fatal(r)
	}
	result, ok := r.Data.(agent.ExecResult)
	if !ok || result.ExitCode != 0 || result.ActualCWD != dir || !strings.Contains(result.Output, dir) || strings.Contains(result.Output, "PRIVATE-SSH-SECRET") || result.Stderr != "warning" {
		t.Fatal(result)
	}
	b := agentBackend{p}
	if e := b.Focus(session); e != nil {
		t.Fatal(e)
	}
	awaitEvent(t, p, func(e appui.Event) bool { return e.FocusID == session })
	if _, e := p.ShellCommand("bash"); e != nil {
		t.Fatal(e)
	}
	if _, e := p.ShellCommand("fish"); e == nil {
		t.Fatal("unsupported shell")
	}
	if e := b.Control(session, "\x03"); e != nil {
		t.Fatal(e)
	}
}
func TestCloudPlatformCreateLoginAndManualSyncReferenceOnly(t *testing.T) {
	p := desktopFixture(t)
	token, _ := cloudv2.NewAuthToken()
	ref, e := cloudhttp.NewReference(token)
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(ref)
	defer server.Close()
	p.CloudConnect(server.URL, "", "private test workspace", token, "different strong E2E passphrase", false)
	created := awaitEvent(t, p, func(e appui.Event) bool { return e.WorkspaceID != "" })
	overview := p.CloudOverview()
	if len(overview.Workspaces) != 1 || overview.Workspaces[0].ID != created.WorkspaceID {
		t.Fatal(overview)
	}
	p.CloudSync(created.WorkspaceID, false)
	awaitEvent(t, p, func(e appui.Event) bool { return e.Message == "云同步完成" })
	overview = p.CloudOverview()
	if len(overview.Statuses) != 1 || overview.Statuses[0].State != "synced" {
		t.Fatal(overview)
	}
	p.CloudBackground(created.WorkspaceID, true)
	p.CloudBackground(created.WorkspaceID, false)
	if p.CloudOverview().Statuses[0].Background {
		t.Fatal("scheduler not stopped")
	}
}
func TestAgentDisabledByDefaultAndOfficialTerminalCallbacks(t *testing.T) {
	p := desktopFixture(t)
	overview := p.AgentOverview()
	if overview.URL != "" || !overview.Halted {
		t.Fatal("MCP not default deny", overview)
	}
	first, e := p.AgentToken(false)
	if e != nil || first == "" {
		t.Fatal(e)
	}
	rotated, e := p.AgentToken(true)
	if e != nil || rotated == first {
		t.Fatal(e)
	}
	callbacks := p.terminalOptions(terminal.Options{})
	callbacks.OnNotify("hello\x1b\n", "world")
	event := awaitEvent(t, p, func(e appui.Event) bool { return strings.HasPrefix(e.Message, "终端通知：") })
	if strings.ContainsAny(event.Message, "\x1b\n") {
		t.Fatal("notification controls not stripped")
	}
	callbacks.OnBell()
	awaitEvent(t, p, func(e appui.Event) bool { return strings.Contains(e.Message, "BEL") })
}

func TestCloudLoginDoesNotBlockStatusUIAndShutdownCancels(t *testing.T) {
	p := desktopFixture(t)
	entered := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done() }))
	defer server.Close()
	defer p.Shutdown()
	token, _ := cloudv2.NewAuthToken()
	p.CloudConnect(server.URL, "", "test", token, "independent password", false)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("login not started")
	}
	done := make(chan struct{})
	go func() { p.CloudOverview(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("network login held UI status lock")
	}
}
func TestCloudAuthenticationTokenCannotBeReusedAsEncryptionPassword(t *testing.T) {
	p := desktopFixture(t)
	token, _ := cloudv2.NewAuthToken()
	p.CloudConnect("http://127.0.0.1:1", "", "test", token, token, false)
	awaitEvent(t, p, func(e appui.Event) bool { return e.Message == "认证 token 不得复用为加密口令" })
	if len(p.CloudOverview().Workspaces) != 0 {
		t.Fatal("reused authentication for E2E")
	}
}

// What the editor window does answers with ForEditor, so the UI shows it there;
// opening a file from the main window's panel does not.
func TestEditorActionsAreTaggedForTheEditorWindow(t *testing.T) {
	testutil.RequirePOSIXRemote(t)
	p := desktopFixture(t)
	session, root := phase2SSH(t, p)
	text := filepath.Join(root, "text")
	os.WriteFile(text, []byte("initial"), 0600)
	message := func() appui.Event { return awaitEvent(t, p, func(e appui.Event) bool { return e.Message != "" }) }

	p.OpenRemote(session, filepath.Join(root, "missing"))
	if e := message(); e.ForEditor {
		t.Fatal("a failed open from the file panel is the main window's", e)
	}
	p.ReloadRemote(session, filepath.Join(root, "missing"))
	if e := message(); !e.ForEditor {
		t.Fatal("a failed reload is the editor's", e)
	}
	p.OpenRemote(session, text)
	doc := awaitEvent(t, p, func(e appui.Event) bool { return e.Editor != nil }).Editor.Document
	p.SaveRemote(session, doc, "edited")
	if e := message(); !e.ForEditor || e.Editor == nil || !e.Editor.Saved {
		t.Fatal("a save acknowledgement is the editor's", e)
	}
	p.SaveRemote(session, remoteedit.Document{Path: filepath.Join(root, "stale"), Hash: doc.Hash}, "x")
	if e := message(); !e.ForEditor {
		t.Fatal("a failed save is the editor's", e)
	}
}
