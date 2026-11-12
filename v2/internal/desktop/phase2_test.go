package desktop

import (
	"encoding/json"
	"fmt"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/exchange"
	"nextshell.dev/v2/internal/model"
	"nextshell.dev/v2/internal/testutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func phase2SSH(t *testing.T, p *Platform) (string, string) {
	t.Helper()
	root := t.TempDir()
	server, e := testutil.New(root)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(server.Close)
	p.Connect(server.Options().Connection)
	s := awaitEvent(t, p, func(e appui.Event) bool { return e.Session != nil }).Session
	t.Cleanup(func() { p.CloseSession(s.ID) })
	return s.ID, root
}
func waitQueue(t *testing.T, p *Platform, id string) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		for _, j := range p.Transfers() {
			if j.ID == id {
				if j.State == "failed" || j.State == "cancelled" {
					t.Fatal(j)
				}
				if j.State == "done" {
					return
				}
			}
		}
		select {
		case <-deadline:
			t.Fatal("queue timeout")
		case <-tick.C:
		}
	}
}
func TestPhase2PlatformRealSSHWorkflow(t *testing.T) {
	testutil.RequireLinuxRemote(t)
	p := desktopFixture(t)
	session, root := phase2SSH(t, p)
	os.WriteFile(filepath.Join(root, "text"), []byte("initial"), 0600)
	p.ListFiles(session, root)
	ev := awaitEvent(t, p, func(e appui.Event) bool { return e.Files != nil })
	if len(ev.Files.Entries) != 1 || ev.Files.Path != root {
		t.Fatal(ev.Files)
	}
	p.OpenRemote(session, filepath.Join(root, "text"))
	doc := awaitEvent(t, p, func(e appui.Event) bool { return e.Editor != nil }).Editor.Document
	p.SaveRemote(session, doc, "edited through Platform")
	doc = awaitEvent(t, p, func(e appui.Event) bool { return e.Editor != nil }).Editor.Document
	if doc.Text != "edited through Platform" {
		t.Fatal(doc)
	}
	local := filepath.Join(t.TempDir(), "upload")
	os.WriteFile(local, []byte("queue upload"), 0600)
	job, e := p.QueueTransfer(appui.TransferRequest{Session: session, Kind: "upload", Local: local, Remote: filepath.Join(root, "upload")})
	if e != nil {
		t.Fatal(e)
	}
	waitQueue(t, p, job)
	target, targetRoot := phase2SSH(t, p)
	job, e = p.QueueTransfer(appui.TransferRequest{Session: session, TargetSession: target, Kind: "cross", Remote: filepath.Join(root, "upload"), Destination: filepath.Join(targetRoot, "cross")})
	if e != nil {
		t.Fatal(e)
	}
	waitQueue(t, p, job)
	got, _ := os.ReadFile(filepath.Join(targetRoot, "cross"))
	if string(got) != "queue upload" {
		t.Fatal(string(got))
	}
	// A download to "~/Downloads/…" lands in the home directory's folder, made when it is missing.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	job, e = p.QueueTransfer(appui.TransferRequest{Session: session, Kind: "download", Remote: filepath.Join(root, "upload"), Local: "~/Downloads/new/upload"})
	if e != nil {
		t.Fatal(e)
	}
	waitQueue(t, p, job)
	if got, _ := os.ReadFile(filepath.Join(home, "Downloads", "new", "upload")); string(got) != "queue upload" {
		t.Fatal(string(got))
	}
	// Light probe by default; the detail flag switches to the full one, and turning it on refreshes at once.
	p.RefreshMonitor(session)
	snapshot := awaitEvent(t, p, func(e appui.Event) bool { return e.Monitor != nil }).Monitor.Snapshot
	if snapshot.MemoryTotal == 0 || snapshot.Detail || snapshot.Processes != nil {
		t.Fatal("default monitor sample is not light:", snapshot)
	}
	p.WatchMonitorDetail(session, true)
	snapshot = awaitEvent(t, p, func(e appui.Event) bool { return e.Monitor != nil && e.Monitor.Snapshot.Detail }).Monitor.Snapshot
	if snapshot.MemoryTotal == 0 || len(snapshot.Processes) == 0 {
		t.Fatal(snapshot)
	}
	p.WatchMonitorDetail(session, false)
	p.WatchMonitor(session, true) // the 2 s ticker reads the flag per tick, so the next sample is light again
	awaitEvent(t, p, func(e appui.Event) bool { return e.Monitor != nil && !e.Monitor.Snapshot.Detail })
	p.WatchMonitor(session, false)
	command := model.Command{ID: p.NewID(), Name: "inspect", Group: "ops", Text: "printf hello-command"}
	if e = p.SaveCommand(command); e != nil {
		t.Fatal(e)
	}
	if e = p.ExecuteCommand(session, command.Text, true); e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(time.Second)
	for !strings.Contains(p.ScreenText(target), "hello-command") {
		if time.Now().After(deadline) {
			t.Fatal("all-session command not delivered")
		}
		time.Sleep(time.Millisecond)
	}
	snap, _ := p.Snapshot()
	if len(snap.Commands) != 1 {
		t.Fatal(snap.Commands)
	}
	if e = p.Delete("command", command.ID); e != nil {
		t.Fatal(e)
	}
	p.CloseSession(session)
	if _, e = p.QueueTransfer(appui.TransferRequest{Session: session, Kind: "upload"}); e == nil {
		t.Fatal("accepted closed SSH session")
	}
}
func TestPhase2PlatformImportPreviewExportAndFolderSpecs(t *testing.T) {
	p := desktopFixture(t)
	password := "interop-password"
	secret := "中文 confidential"
	entry := exchange.Entry{Name: "test", Host: "host.example", Port: 22, Username: "user", AuthType: "password", Password: &secret, GroupPath: "/server/prod/empty", Encoding: "utf-8"}
	raw, e := exchange.Encode([]exchange.Entry{entry}, &password, time.Unix(0, 0))
	if e != nil {
		t.Fatal(e)
	}
	file := filepath.Join(t.TempDir(), "import.json")
	os.WriteFile(file, append([]byte(" \xef\xbb\xbf"), raw...), 0600)
	preview := func() *appui.ImportPreview {
		p.PreviewImport(file, password, "nextshell")
		ev := awaitEvent(t, p, func(e appui.Event) bool { return e.Import != nil })
		return ev.Import
	}
	pv := preview()
	snap, _ := p.Snapshot()
	if len(snap.Connections) != 0 || pv.Count != 1 {
		t.Fatal("preview mutated storage", snap, pv)
	}
	p.CommitImport(pv.Token, "", "skip")
	awaitEvent(t, p, func(e appui.Event) bool { return strings.HasPrefix(e.Message, "已导入 ") })
	snap, _ = p.Snapshot()
	if len(snap.Connections) != 1 || len(snap.Folders) != 2 || snap.Connections[0].Secret != "" {
		t.Fatal("import hierarchy/redaction", snap)
	}
	stored, e := p.store.Connection(snap.Connections[0].ID)
	if e != nil || stored.Secret != secret {
		t.Fatal(stored, e)
	}
	pv = preview()
	p.CommitImport(pv.Token, "", "skip")
	awaitEvent(t, p, func(e appui.Event) bool { return strings.HasPrefix(e.Message, "已导入 ") })
	snap, _ = p.Snapshot()
	if len(snap.Connections) != 1 || len(snap.Folders) != 2 {
		t.Fatal("reimport skip/reuse failed")
	}
	pv = preview()
	p.CommitImport(pv.Token, "", "rename-copy")
	awaitEvent(t, p, func(e appui.Event) bool { return strings.HasPrefix(e.Message, "已导入 ") })
	snap, _ = p.Snapshot()
	if len(snap.Connections) != 2 || len(snap.Folders) != 2 {
		t.Fatal("duplicate/reuse failed")
	}
	export := filepath.Join(t.TempDir(), "export.json")
	p.ExportConnections(export, &password, []string{snap.Connections[0].ID})
	awaitEvent(t, p, func(e appui.Event) bool { return strings.HasPrefix(e.Message, "已导出 ") })
	encoded, _ := os.ReadFile(export)
	decoded, e := exchange.Decode(encoded, &password)
	if e != nil || len(decoded.Connections) != 1 || *decoded.Connections[0].Password != secret || decoded.Connections[0].GroupPath != "/server/prod/empty" {
		t.Fatal(decoded, e)
	}
	// Windows has no POSIX modes (Perm() reports 0666 whatever was asked for): there the export is as private
	// as the directory's ACL, and what the product can still guarantee is the O_EXCL below.
	if info, _ := os.Stat(export); runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatal("unsafe export mode")
	}
	p.ExportConnections(export, nil, nil)
	awaitEvent(t, p, func(e appui.Event) bool { return strings.Contains(e.Message, "file exists") })
	after, _ := os.ReadFile(export)
	if string(after) != string(encoded) {
		t.Fatal("existing export overwritten")
	}
}
func TestPhase2InvalidCallsAndShutdown(t *testing.T) {
	p := desktopFixture(t)
	if p.ExecuteCommand("missing", "pwd", false) == nil {
		t.Fatal("missing command target accepted")
	}
	if p.SaveCommand(model.Command{}) == nil {
		t.Fatal("invalid saved command accepted")
	}
	p.PreviewImport("unused", "", "unknown")
	awaitEvent(t, p, func(e appui.Event) bool { return strings.Contains(e.Message, "unsupported import format") })
	p.SignalProcess("missing", 1, "KILL")
	awaitEvent(t, p, func(e appui.Event) bool { return e.Message != "" })

	p.Shutdown()
	p.Shutdown()
}

func TestPhase2FinalShellDirectoryPreviewAndImport(t *testing.T) {
	p := desktopFixture(t)
	base := t.TempDir()
	dir := filepath.Join(base, "region", "hk")
	os.MkdirAll(dir, 0700)
	raw, e := os.ReadFile("../exchange/testdata/v1-vectors.json")
	if e != nil {
		t.Fatal(e)
	}
	var vectors struct {
		Finals []struct{ Encoded, Plaintext string }
	}
	json.Unmarshal(raw, &vectors)
	for i, v := range vectors.Finals {
		row := map[string]any{"name": fmt.Sprint("legacy-", i), "host": fmt.Sprint("host-", i), "user_name": "admin", "authentication_type": 2, "password": v.Encoded}
		if i == 1 {
			row["secret_key_id"] = "must-rebind"
		}
		encoded, _ := json.Marshal(row)
		os.WriteFile(filepath.Join(dir, fmt.Sprint(i, ".json")), encoded, 0600)
	}
	os.WriteFile(filepath.Join(base, "broken.json"), []byte("invalid"), 0600)
	os.Symlink(filepath.Join(dir, "0.json"), filepath.Join(base, "skip.json"))
	p.PreviewImport(base, "", "finalshell")
	pv := awaitEvent(t, p, func(e appui.Event) bool { return e.Import != nil }).Import
	if pv.Count != 2 || pv.KeysNeedRebind != 1 || len(pv.Warnings) != 2 {
		t.Fatal(pv)
	}
	p.CommitImport(pv.Token, "", "rename-copy")
	awaitEvent(t, p, func(e appui.Event) bool { return strings.HasPrefix(e.Message, "已导入 ") })
	snap, _ := p.Snapshot()
	if len(snap.Folders) != 2 || len(snap.Connections) != 2 {
		t.Fatal(snap)
	}
	for _, meta := range snap.Connections {
		c, e := p.store.Connection(meta.ID)
		if e != nil || c.Secret != vectors.Finals[0].Plaintext {
			t.Fatal("FinalShell decrypted password not restored", e)
		}
		if c.Name == "legacy-1" && (!c.NeedsKeyRebind || c.Auth != "keyboard") {
			t.Fatal(c)
		}
	}
}

// WatchMonitorDetail is per-session state read by the next sample; no SSH needed to see it set, refresh and clear.
func TestWatchMonitorDetailFlag(t *testing.T) {
	p := desktopFixture(t)
	detail := func() bool { p.mu.Lock(); defer p.mu.Unlock(); return p.monitorDetail["s"] }
	p.WatchMonitorDetail("s", true)
	if !detail() {
		t.Fatal("flag not set")
	}
	if ev := awaitEventOrMessage(t, p); ev.Message == "" { // turning it on refreshes at once; "s" has no SSH session to refresh
		t.Fatal("no immediate refresh:", ev)
	}
	p.WatchMonitorDetail("s", false)
	if detail() {
		t.Fatal("flag not cleared")
	}
	p.WatchMonitorDetail("s", true)
	p.CloseSession("s")
	if detail() {
		t.Fatal("closing the session leaves its flag behind")
	}
}
