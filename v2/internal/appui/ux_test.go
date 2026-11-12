package appui_test

import (
	"runtime"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/model"
	"nextshell.dev/v2/internal/transfer"
)

type retryPlatform struct {
	*fakePlatform
	retried []string
}

func (p *retryPlatform) RetryTransfer(id string) error { p.retried = append(p.retried, id); return nil }

// With the activity column folded, a transfer that ends must still say so: done, or failed with a retry.
func TestFinishedTransfersToastEvenWithTheColumnFolded(t *testing.T) {
	p := &retryPlatform{fakePlatform: newPlatform(t)}
	a := appui.New(p)
	tt := ui.NewTester(a.View, 1280, 800)
	p.jobs = []transfer.Job{{ID: "j1", Label: "上传 a.zip → /srv", State: "running", Attempt: 1}}
	tt.Frame()
	if tt.HasText("上传完成") || tt.HasText("上传失败") {
		t.Fatal("a running transfer toasted", tt.Texts())
	}
	p.jobs[0].State = "done"
	tt.Frame()
	has(t, tt, "a.zip 上传完成")
	p.jobs = append(p.jobs, transfer.Job{ID: "j2", Label: "下载 b.iso → /tmp", State: "failed", Error: "open /tmp/b.iso: permission denied", Attempt: 1})
	tt.Frame()
	has(t, tt, "b.iso 下载失败：权限不足", "重试")
	click(t, tt, "重试")
	if !slices.Equal(p.retried, []string{"j2"}) {
		t.Fatal("重试 did not retry the job", p.retried)
	}
}

func TestConnectFailureToastNamesTheHostAndADeclinedPasswordIsSilent(t *testing.T) {
	p := newPlatform(t)
	web := saveHost(t, p, model.Connection{Name: "prod-web-01", Host: "10.0.0.1", User: "root"})
	_, tt := start(p)
	p.events = append(p.events, appui.Event{Message: "dial tcp 10.0.0.1:22: connect: connection refused", Failed: web.ID, Host: web.Name})
	tt.Frame()
	has(t, tt, "连接 prod-web-01 失败：连接被拒绝：服务器没有在这个端口提供 SSH 服务", "详情")
	click(t, tt, "详情")
	has(t, tt, "dial tcp 10.0.0.1:22: connect: connection refused") // the original stays in the details
	click(t, tt, "关闭")

	// Quick connect has no saved host, only the name it was typed as.
	p.events = append(p.events, appui.Event{Message: "dial tcp: lookup nowhere.example: no such host", Host: "root@nowhere.example"})
	tt.Frame()
	has(t, tt, "连接 root@nowhere.example 失败：找不到主机：域名无法解析")

	// Cancelling the password dialog is no failure: no toast, and no red mark on the host.
	other := saveHost(t, p, model.Connection{Name: "db-master", Host: "10.0.2.10", User: "postgres"})
	p.events = append(p.events, appui.Event{Message: "已取消", Failed: other.ID, Host: other.Name})
	tt.Frame()
	if tt.HasText("已取消") || tt.HasText("db-master 失败") {
		t.Fatal("a declined password toasted", tt.Texts())
	}
	// The same message from a source that is not a connect stays visible.
	p.events = append(p.events, appui.Event{Message: "已取消"})
	tt.Frame()
	has(t, tt, "已取消")
}

func TestHostKeyDialogShowsTheFingerprintOnOneSelectableLine(t *testing.T) {
	_, p, tt := fixture(t)
	const fp = "SHA256:nThbg6kXUpJWGl7E1IGOCspRomTxdCARLviKw6E5SY8"
	pr, _ := prompt(p, tt, appui.Prompt{Kind: appui.PromptHostKey, Host: "10.0.0.1:22", Fingerprint: fp})
	has(t, tt, "首次连接，请核对主机指纹", "10.0.0.1:22", fp, "信任并连接", "取消")
	r, ok := tt.Find(fp)
	if !ok || r.H > 20 {
		t.Fatalf("the fingerprint wraps: %+v", r)
	}
	click(t, tt, "信任并连接")
	if got := answer(t, pr); got == nil {
		t.Fatal("trust not sent")
	}
	pr, _ = prompt(p, tt, appui.Prompt{Kind: appui.PromptHostKey, ByAgent: true, Host: "10.0.0.1:22", Fingerprint: fp})
	has(t, tt, "由 Agent 发起")
	click(t, tt, "取消")
	if got := answer(t, pr); got != nil {
		t.Fatal("cancel trusted the key")
	}
}

func paletteKey(letter string) string {
	if runtime.GOOS == "darwin" {
		return "⌘" + letter
	}
	return "Ctrl+Shift+" + letter
}

func TestPaletteListsOpenTabsBeforeHostsAndShowsShortcuts(t *testing.T) {
	p := newPlatform(t)
	web := saveHost(t, p, model.Connection{Name: "prod-web-01", Host: "10.0.0.1", User: "root"})
	saveHost(t, p, model.Connection{Name: "prod-web-02", Host: "10.0.0.2", User: "root"})
	a, tt := start(p)
	session(t, p, tt, "s1", "prod-web-01", web)
	session(t, p, tt, "s2", "scratch", model.Connection{})
	if a.Active != 1 {
		t.Fatal("setup")
	}
	tt.Key(appMod(), ui.KeyK)
	tt.Type("prod")
	texts := tt.Texts()
	if tab, host := slices.Index(texts, "标签"), slices.Index(texts, "主机"); tab < 0 || host < 0 || tab > host {
		t.Fatal("标签 must come before 主机", tab, host, texts)
	}
	tt.Key(0, ui.KeyEnter)
	if a.Active != 0 || len(p.connects) != 0 {
		t.Fatal("Enter opened a second session instead of switching", a.Active, p.connects)
	}

	for query, want := range map[string]string{"本地终端": paletteKey("T"), "连接管理器": paletteKey("O"), "左栏": paletteKey("B"), "底部工作区": paletteKey("J")} {
		tt.Key(appMod(), ui.KeyK)
		tt.Type(query)
		has(t, tt, want)
		tt.Key(0, ui.KeyEscape)
	}
	comma := "Ctrl+,"
	if runtime.GOOS == "darwin" {
		comma = "⌘,"
	}
	tt.Key(appMod(), ui.KeyK)
	tt.Type("打开设置")
	has(t, tt, comma)
}

// The welcome page's box promises 主机名: the text need not be a whole saved name or user@host.
func TestWelcomeQuickConnectFindsSavedHostsByPartOfNameOrAddress(t *testing.T) {
	p := newPlatform(t)
	demo(t, p)
	// quick types text into a fresh welcome page (a connect replaces it with a tab).
	quick := func(text string) *ui.Tester {
		t.Helper()
		_, tt := start(p)
		click(t, tt, "快速连接")
		tt.Type(text)
		tt.Key(0, ui.KeyEnter)
		return tt
	}
	connected := func(want ...string) {
		t.Helper()
		var got []string
		for _, c := range p.connects {
			got = append(got, c.Name)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("connected to %v, want %v", got, want)
		}
	}
	quick("db-mas") // one host has it in its name
	connected("db-master")
	quick("10.0.0.1") // one has it as its address; prod-web-02's 10.0.0.2 and db-master's 10.0.2.10 do not
	connected("db-master", "prod-web-01")
	tt := quick("prod") // two match: the palette opens on the text, nothing connects
	connected("db-master", "prod-web-01")
	has(t, tt, "root@10.0.0.1:22", "root@10.0.0.2:22")
	if tt.HasText("postgres@10.0.2.10:22") || tt.HasText("没有结果") {
		t.Fatal("the palette is not filtered by the text", tt.Texts())
	}
	tt = quick("zzz") // none: the palette says so
	has(t, tt, "没有结果")
	tt = quick("old-jump") // a deleted host is not offered
	has(t, tt, "没有结果")
	connected("db-master", "prod-web-01")
	quick("ops@new.example") // user@host still goes straight out
	if len(p.connects) != 3 || p.connects[2].Host != "new.example" {
		t.Fatal("user@host", p.connects)
	}
}

func TestFirstRunWelcomeOffersEachActionOnce(t *testing.T) {
	_, p, tt := fixture(t)
	has(t, tt, "新建主机", "从 NextShell 1.x 导入连接文件", "打开本地终端")
	if tt.HasText("+ 新建主机") || tt.HasText("导入…") {
		t.Fatal("the buttons repeat the guide cards", tt.Texts())
	}
	click(t, tt, "打开本地终端")
	if p.local != 1 {
		t.Fatal("local terminal")
	}
	// With hosts the cards show them and the row keeps all three buttons.
	saveHost(t, p, model.Connection{Name: "box", Host: "10.9.9.9", User: "ops"})
	_, bt := start(p)
	has(t, bt, "+ 新建主机", "导入…", "打开本地终端")
}
