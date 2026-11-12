package desktop

import (
	"encoding/json"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/cloudv2"
	"nextshell.dev/v2/internal/model"
	"nextshell.dev/v2/internal/storage"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func filePreview(t *testing.T, p *Platform, entry map[string]any) *appui.ImportPreview {
	t.Helper()
	file := filepath.Join(t.TempDir(), "import.json")
	raw, _ := json.Marshal(map[string]any{"format": "nextshell-connections", "version": 1, "connections": []any{entry}})
	if e := os.WriteFile(file, raw, 0600); e != nil {
		t.Fatal(e)
	}
	p.PreviewImport(file, "", "nextshell")
	return awaitEvent(t, p, func(e appui.Event) bool { return e.Import != nil }).Import
}
func wireEntry() map[string]any {
	return map[string]any{"name": "imported", "host": "example.test", "port": 22, "username": "user", "authType": "password"}
}
func importTarget(t *testing.T, p *Platform) model.Connection {
	t.Helper()
	c := model.Connection{ID: p.NewID(), Name: "target", Host: "example.test", Port: 22, User: "user", Auth: "password", Encoding: "big5", Notes: "retain notes", Favorite: true, Fingerprint: "SHA256:retain-trust", StrictHostKeyChecking: true}
	secret := "retain password"
	if e := p.SaveConnection(c, &secret); e != nil {
		t.Fatal(e)
	}
	got, _ := p.store.Connection(c.ID)
	return got
}
func importDone(t *testing.T, p *Platform) {
	t.Helper()
	awaitEvent(t, p, func(e appui.Event) bool { return strings.HasPrefix(e.Message, "已导入 ") })
}
func TestImportOverwritePreservesIdentityAbsentFieldsAndRouting(t *testing.T) {
	p := desktopFixture(t)
	target := importTarget(t, p)
	proxy := model.Proxy{ID: p.NewID(), Name: "proxy", Host: "localhost", Port: 1080, Kind: "socks5"}
	if e := p.SaveProxy(proxy, nil); e != nil {
		t.Fatal(e)
	}
	target.ProxyID = proxy.ID
	if e := p.SaveConnection(target, nil); e != nil {
		t.Fatal(e)
	}
	target, _ = p.store.Connection(target.ID)
	entry := wireEntry()
	entry["favorite"] = false
	preview := filePreview(t, p, entry)
	if len(preview.Targets) != 1 || !strings.Contains(preview.Targets[0], target.ID) {
		t.Fatal(preview)
	}
	p.CommitImport(preview.Token, "", "overwrite")
	importDone(t, p)
	got, e := p.store.Connection(target.ID)
	if e != nil || got.CreatedAt != target.CreatedAt || got.Name != "imported" || got.Secret != target.Secret || got.ProxyID != proxy.ID || got.Fingerprint != target.Fingerprint || !got.StrictHostKeyChecking || got.Notes != target.Notes || got.Favorite || got.Encoding != target.Encoding || got.Revision != target.Revision+1 {
		t.Fatal(got, e)
	}
	entry["password"] = ""
	preview = filePreview(t, p, entry)
	p.CommitImport(preview.Token, "", "overwrite")
	importDone(t, p)
	got, _ = p.store.Connection(target.ID)
	if got.Secret != "" {
		t.Fatal("explicit empty password wasn't cleared")
	}
}
func TestImportDefaultSkipRenameCopyAndAmbiguousOverwrite(t *testing.T) {
	p := desktopFixture(t)
	target := importTarget(t, p)
	entry := wireEntry()
	entry["name"] = target.Name
	entry["groupPath"] = "/server/not-created-on-skip"
	preview := filePreview(t, p, entry)
	p.CommitImport(preview.Token, "", "")
	importDone(t, p)
	snap, _ := p.Snapshot()
	if len(snap.Connections) != 1 || len(snap.Folders) != 0 {
		t.Fatal("skip mutated resources")
	}
	preview = filePreview(t, p, entry)
	p.CommitImport(preview.Token, "", "rename-copy")
	importDone(t, p)
	snap, _ = p.Snapshot()
	if len(snap.Connections) != 2 || snap.Connections[0].Name == snap.Connections[1].Name || snap.Connections[0].ID == snap.Connections[1].ID {
		t.Fatal("rename-copy failed", snap.Connections)
	}
	preview = filePreview(t, p, entry)
	if len(preview.Targets) != 2 {
		t.Fatal(preview)
	}
	p.CommitImport(preview.Token, "", "overwrite")
	awaitEvent(t, p, func(e appui.Event) bool { return strings.Contains(e.Message, "ambiguous overwrite") })
	snap, _ = p.Snapshot()
	if len(snap.Connections) != 2 {
		t.Fatal("ambiguous overwrite mutated DB")
	}
}
func TestImportStalePreviewAndPlainCredentialWarning(t *testing.T) {
	p := desktopFixture(t)
	entry := wireEntry()
	entry["password"] = "literal plaintext"
	preview := filePreview(t, p, entry)
	if len(preview.Warnings) == 0 {
		t.Fatal("no plaintext credential warning")
	}
	if e := p.SaveFolder(model.Folder{ID: p.NewID(), Name: "changed-after-preview"}); e != nil {
		t.Fatal(e)
	}
	p.CommitImport(preview.Token, "", "skip")
	awaitEvent(t, p, func(e appui.Event) bool { return strings.Contains(e.Message, "stale") })
	snap, _ := p.Snapshot()
	if len(snap.Connections) != 0 {
		t.Fatal("stale preview committed")
	}
}
func TestFileImportNeverConflictsWithCloudScopeAndRejectsCloudTarget(t *testing.T) {
	p := desktopFixture(t)
	id := cloudv2.NewID()
	w, key, e := cloudv2.NewKey("encryption pass", id)
	if e != nil {
		t.Fatal(e)
	}
	token, _ := cloudv2.NewAuthToken()
	if e = p.store.SaveWorkspace(storage.Workspace{ID: id, Name: "cloud"}, storage.WorkspaceSecrets{Wrapped: w, DataKey: key, AuthToken: token}); e != nil {
		t.Fatal(e)
	}
	cloud := model.Connection{Meta: model.Meta{Scope: id}, ID: p.NewID(), Name: "cloud", Host: "example.test", Port: 22, User: "user", Auth: "password", Encoding: "utf-8"}
	if e = p.SaveConnection(cloud, nil); e != nil {
		t.Fatal(e)
	}
	folder := model.Folder{Meta: model.Meta{Scope: id}, ID: p.NewID(), Name: "cloud folder"}
	if e = p.SaveFolder(folder); e != nil {
		t.Fatal(e)
	}
	preview := filePreview(t, p, wireEntry())
	if len(preview.Targets) != 0 {
		t.Fatal("cloud overwrite candidate exposed")
	}
	p.CommitImport(preview.Token, folder.ID, "skip")
	awaitEvent(t, p, func(e appui.Event) bool { return strings.Contains(e.Message, "local scope") })
	p.CommitImport(preview.Token, "", "skip")
	importDone(t, p)
	snap, _ := p.Snapshot()
	if len(snap.Connections) != 2 {
		t.Fatal("cloud endpoint prevented local import")
	}
	count := 0
	for _, c := range snap.Connections {
		if c.ScopeID() == model.LocalScope {
			count++
		}
	}
	if count != 1 {
		t.Fatal(snap.Connections)
	}
}
func TestV1CompatibleExportRejectsUnsupportedEncoding(t *testing.T) {
	p := desktopFixture(t)
	c := importTarget(t, p)
	c.Encoding = "shift_jis"
	if e := p.SaveConnection(c, nil); e != nil {
		t.Fatal(e)
	}
	file := filepath.Join(t.TempDir(), "export.json")
	p.ExportConnections(file, nil, nil)
	awaitEvent(t, p, func(e appui.Event) bool { return strings.Contains(e.Message, "shift_jis") })
	if _, e := os.Stat(file); !os.IsNotExist(e) {
		t.Fatal("unsupported output file written")
	}
}
