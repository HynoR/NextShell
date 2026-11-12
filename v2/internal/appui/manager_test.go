package appui_test

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/model"
)

func stored(t *testing.T, p *fakePlatform, name string) model.Connection {
	t.Helper()
	snap, _ := p.Snapshot()
	for _, c := range snap.Connections {
		if c.Name == name {
			return c
		}
	}
	t.Fatal("no host", name)
	return model.Connection{}
}

// A credential made inside the host form is chosen there, its user shows read-only, and binding it to
// many hosts at once replaces their own login; deleting it returns them to their own.
func TestCredentialFromHostFormAndBatch(t *testing.T) {
	p := newPlatform(t)
	for _, n := range []string{"n1", "n2", "n3"} {
		saveHost(t, p, model.Connection{Name: n, Host: n + ".example", User: "old"})
	}
	a, _ := start(p)
	mt := manager(a)
	click(t, mt, "+ 新建连接")
	mt.Type("with-cred")
	click(t, mt, "主机")
	mt.Type("10.0.0.9")
	click(t, mt, "新建凭据…")
	has(t, mt, "新建凭据")
	clickLast(t, mt, "名称")
	mt.Type("运维")
	clickLast(t, mt, "用户名")
	mt.Type("ops")
	clickLast(t, mt, "密码")
	mt.Type("pw")
	click(t, mt, "保存凭据")
	has(t, mt, "运维", "ops", "来自凭据，修改凭据即对所有使用它的主机生效")
	click(t, mt, "保存")
	c := stored(t, p, "with-cred")
	snap, _ := p.Snapshot()
	if len(snap.Credentials) != 1 || c.CredentialID != snap.Credentials[0].ID || c.User != "" {
		t.Fatal("credential not bound", c, snap.Credentials)
	}

	click(t, mt, "n1")
	mt.ClickWith(ui.Cmd, "n2")
	mt.ClickWith(ui.Cmd, "n3")
	has(t, mt, "已选 3")
	click(t, mt, "绑定认证…")
	choose(t, mt, "使用凭据", "运维")
	has(t, mt, "密码 → 凭据 运维")
	click(t, mt, "应用到 3 个主机")
	for _, n := range []string{"n1", "n2", "n3"} {
		if c := stored(t, p, n); c.CredentialID != snap.Credentials[0].ID || c.User != "old" {
			t.Fatal("batch credential", c)
		}
	}
	has(t, mt, "ops@n1.example:22")

	click(t, mt, "凭据")
	click(t, mt, "运维")
	has(t, mt, "被引用（4）")
	click(t, mt, "删除")
	has(t, mt, "有 4 个主机使用这个凭据，删除后它们改用各自的用户名和认证。")
	click(t, mt, "删除凭据")
	if c := stored(t, p, "n1"); c.CredentialID != "" || c.User != "old" {
		t.Fatal("fallback", c)
	}
	if c := stored(t, p, "with-cred"); c.CredentialID != "" || c.User != "ops" {
		t.Fatal("a host without its own login keeps the credential's", c)
	}
}

// A key made beside the key field is chosen at once; the same key again is the one already saved; the
// next new host starts with the same key.
func TestKeyFromHostFormDedupeAndLastAuth(t *testing.T) {
	p := newPlatform(t)
	existing, e := p.GenerateKey("existing", "ed25519")
	if e != nil {
		t.Fatal(e)
	}
	a, _ := start(p)
	mt := manager(a)
	click(t, mt, "+ 新建连接")
	mt.Type("k1")
	click(t, mt, "主机")
	mt.Type("h1")
	click(t, mt, "用户名")
	mt.Type("u")
	choose(t, mt, "认证方式", "密钥")
	click(t, mt, "新建 / 生成…")
	click(t, mt, "导入私钥")
	click(t, mt, "粘贴私钥")
	mt.Type(existing.Private)
	click(t, mt, "导入密钥")
	has(t, mt, "密钥已存在，已选用：existing")
	click(t, mt, "保存")
	if c := stored(t, p, "k1"); c.Auth != "key" || c.KeyID != existing.ID {
		t.Fatal("existing key not chosen", c)
	}
	snap, _ := p.Snapshot()
	if len(snap.Keys) != 1 {
		t.Fatal("duplicate key saved", snap.Keys)
	}

	click(t, mt, "+ 新建连接")
	mt.Type("k2")
	click(t, mt, "主机")
	mt.Type("h2")
	click(t, mt, "用户名")
	mt.Type("u")
	click(t, mt, "保存")
	if c := stored(t, p, "k2"); c.Auth != "key" || c.KeyID != existing.ID {
		t.Fatal("new host did not start with the last key", c)
	}

	click(t, mt, "+ 新建连接")
	click(t, mt, "新建 / 生成…")
	click(t, mt, "名称")
	mt.Type("fresh")
	clickLast(t, mt, "生成密钥")
	has(t, mt, "fresh")
	snap, _ = p.Snapshot()
	if len(snap.Keys) != 2 {
		t.Fatal(snap.Keys)
	}
}

// An imported key host without its key stays key auth with a red mark, and one button binds a key to
// every such host.
func TestMissingKeyMarkAndChooseKeyForAll(t *testing.T) {
	p := newPlatform(t)
	k, _ := p.GenerateKey("ops", "ed25519")
	for _, n := range []string{"legacy-1", "legacy-2"} {
		saveHost(t, p, model.Connection{Name: n, Host: n, User: "root", Auth: "key", NeedsKeyRebind: true, OriginalAuth: "key"})
	}
	a, _ := start(p)
	mt := manager(a)
	has(t, mt, "缺少密钥", "2 个主机使用密钥认证，但还没有可用的密钥")
	click(t, mt, "为 2 个主机选择密钥")
	choose(t, mt, "密钥", "ops")
	click(t, mt, "应用到 2 个主机")
	for _, n := range []string{"legacy-1", "legacy-2"} {
		if c := stored(t, p, n); c.KeyID != k.ID || c.NeedsKeyRebind {
			t.Fatal(c)
		}
	}
	if mt.HasText("缺少密钥") {
		t.Fatal("mark stays")
	}
}

// After a refused login the user may change the user name and method, which the host keeps.
func TestAuthFailureRetryWritesBack(t *testing.T) {
	p := newPlatform(t)
	h := saveHost(t, p, model.Connection{Name: "box", Host: "10.0.0.3", User: "wrong"})
	_, tt := start(p)
	p.events = append(p.events, appui.Event{Failed: h.ID, AuthFailed: true, Message: "ssh: handshake failed: ssh: unable to authenticate"})
	tt.Frame()
	has(t, tt, "登录失败：box")
	click(t, tt, "用户名")
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("root")
	choose(t, tt, "认证方式", "SSH Agent")
	click(t, tt, "保存并重试")
	if c := stored(t, p, "box"); c.User != "root" || c.Auth != "agent" {
		t.Fatal("not written back", c)
	}
	if len(p.connects) != 1 || p.connects[0].User != "root" {
		t.Fatal("no retry", p.connects)
	}
}
