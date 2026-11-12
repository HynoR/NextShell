package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/exchange"
	"nextshell.dev/v2/internal/model"
)

// A host using a credential connects with the credential's user and password, and a changed password
// applies to it at once.
func TestCredentialResolvesOnEveryConnect(t *testing.T) {
	c, _ := desktopSSH(t)
	p := desktopFixture(t)
	p.trustNewHosts.Store(true)
	cr := model.Credential{ID: p.NewID(), Name: "运维", User: "tester", Auth: "password"}
	wrong := "wrong"
	if e := p.SaveCredential(cr, &wrong); e != nil {
		t.Fatal(e)
	}
	c.ID, c.User, c.Auth, c.Secret, c.CredentialID = p.NewID(), "", "password", "", cr.ID
	if e := p.SaveConnection(c, nil); e != nil {
		t.Fatal(e)
	}
	right := "secret"
	if e := p.SaveCredential(cr, &right); e != nil {
		t.Fatal(e)
	}
	p.Connect(c)
	s := awaitEvent(t, p, func(e appui.Event) bool { return e.Session != nil }).Session
	defer p.CloseSession(s.ID)
	if s.Connection.User != "tester" || s.Connection.Secret != "" {
		t.Fatal("credential not resolved", s.Connection)
	}
}

// A key-auth host without its key fails as a login the user can fix.
func TestKeyHostWithoutKeyFailsAsAuth(t *testing.T) {
	p := desktopFixture(t)
	c := model.Connection{ID: p.NewID(), Name: "legacy", Host: "127.0.0.1", Port: 1, User: "u", Auth: "key", NeedsKeyRebind: true, Encoding: "utf-8"}
	if e := p.SaveConnection(c, nil); e != nil {
		t.Fatal(e)
	}
	p.Connect(c)
	ev := awaitEventOrMessage(t, p)
	if !ev.AuthFailed || ev.Failed != c.ID || !strings.Contains(ev.Message, "未选择密钥") {
		t.Fatal(ev)
	}
}

// The same private key saved twice is refused with the first copy; a new key binds the imported hosts
// waiting for it.
func TestKeyDedupeAndRebind(t *testing.T) {
	p := desktopFixture(t)
	k, e := p.GenerateKey("ops", "ed25519")
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(t.TempDir(), "id_ops")
	os.WriteFile(path, []byte(k.Private), 0600)
	got, e := p.ImportKey(path, "")
	var dup *model.KeyExistsError
	if !errors.As(e, &dup) || got.ID != k.ID {
		t.Fatal("duplicate imported", got, e)
	}
	waiting := model.Connection{ID: p.NewID(), Name: "w", Host: "h", Port: 22, User: "u", Auth: "key", NeedsKeyRebind: true, OriginalAuth: "key", SourceKeyRef: &model.KeyReference{Name: "deploy"}, Encoding: "utf-8"}
	if e = p.SaveConnection(waiting, nil); e != nil {
		t.Fatal(e)
	}
	if _, e = p.GenerateKey("deploy", "ed25519"); e != nil {
		t.Fatal(e)
	}
	awaitEvent(t, p, func(e appui.Event) bool { return e.Message == "已为 1 个主机绑定密钥" })
	bound, _ := p.store.Connection(waiting.ID)
	if bound.KeyID == "" || bound.NeedsKeyRebind || bound.Auth != "key" {
		t.Fatal("not rebound", bound)
	}
}

// Exports carry the credential by reference with each host, its password only when encrypted, and never
// copy its user or auth into the host; an import creates it once and binds every host to it.
func TestCredentialExportImport(t *testing.T) {
	p := desktopFixture(t)
	cr := model.Credential{ID: p.NewID(), Name: "共享", User: "ops", Auth: "password"}
	pw := "s3cret"
	if e := p.SaveCredential(cr, &pw); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"a", "b"} {
		if e := p.SaveConnection(model.Connection{ID: p.NewID(), Name: name, Host: name + ".test", Port: 22, Auth: "password", CredentialID: cr.ID, Encoding: "utf-8"}, nil); e != nil {
			t.Fatal(e)
		}
	}
	dir := t.TempDir()
	plain := filepath.Join(dir, "plain.json")
	p.ExportConnections(plain, nil, nil)
	awaitEvent(t, p, func(e appui.Event) bool { return strings.HasPrefix(e.Message, "已导出") })
	raw, _ := os.ReadFile(plain)
	if strings.Contains(string(raw), pw) || !strings.Contains(string(raw), `"credential"`) {
		t.Fatal("plain export", string(raw))
	}
	enc := filepath.Join(dir, "enc.json")
	pass := "export-pass"
	p.ExportConnections(enc, &pass, nil)
	awaitEvent(t, p, func(e appui.Event) bool { return strings.HasPrefix(e.Message, "已导出") })
	raw, _ = os.ReadFile(enc)
	f, e := exchange.Decode(raw, &pass)
	if e != nil || f.Connections[0].Credential == nil || *f.Connections[0].Credential.Password != pw {
		t.Fatal(f, e)
	}
	for _, h := range f.Connections {
		if h.Username != "" || h.Password != nil && *h.Password == pw {
			t.Fatal("credential copied into the host", h)
		}
	}

	q := desktopFixture(t)
	q.PreviewImport(enc, pass, "nextshell")
	preview := awaitEvent(t, q, func(e appui.Event) bool { return e.Import != nil }).Import
	q.CommitImport(preview.Token, "", "skip")
	importDone(t, q)
	snap, _ := q.Snapshot()
	if len(snap.Credentials) != 1 || len(snap.Connections) != 2 {
		t.Fatal(snap.Credentials, snap.Connections)
	}
	for _, c := range snap.Connections {
		if c.CredentialID != snap.Credentials[0].ID {
			t.Fatal("not bound", c)
		}
	}
	if got, _ := q.store.Credential(snap.Credentials[0].ID); got.Secret != pw || got.User != "ops" {
		t.Fatal(got)
	}
	for _, c := range snap.Connections {
		if c.User != "" {
			t.Fatal("host got the credential's user", c)
		}
	}
}
