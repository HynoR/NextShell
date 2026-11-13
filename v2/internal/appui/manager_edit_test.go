package appui_test

import (
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/model"
)

// TestManagerEditorShots renders the host editor with its pinned footer, and the question about dropping
// edits, at the manager window's own size, light and dark, into $NEXTSHELL_SHOTS.
func TestManagerEditorShots(t *testing.T) {
	dir := os.Getenv("NEXTSHELL_SHOTS")
	if dir == "" {
		t.Skip("set NEXTSHELL_SHOTS=<dir> to render screenshots")
	}
	if e := os.MkdirAll(dir, 0o755); e != nil {
		t.Fatal(e)
	}
	shot := func(tt *ui.Tester, name string) {
		t.Helper()
		for _, dark := range []bool{false, true} {
			tt.SetDark(dark)
			tt.Frame()
			mode := "light"
			if dark {
				mode = "dark"
			}
			f, e := os.Create(filepath.Join(dir, name+"-"+mode+".png"))
			if e != nil {
				t.Fatal(e)
			}
			if e = png.Encode(f, tt.Image()); e != nil {
				t.Fatal(e)
			}
			f.Close()
		}
		tt.SetDark(false)
	}
	p := newPlatform(t)
	demo(t, p)
	a, _ := start(p)
	mt := ui.NewTester(a.ManagerView, 960, 640) // the manager window's size
	shot(mt, "w2-01-manager-tiles")
	click(t, mt, "prod-web-02")
	if e := mt.RightClick("prod-web-02"); e != nil {
		t.Fatal(e)
	}
	if e := mt.ChooseMenuItem("编辑…"); e != nil {
		t.Fatal(e)
	}
	for _, s := range []string{"安全", "网络", "终端", "标签与备注"} {
		mt.Scroll(560, 75, 0, 2000) // the closed headers are last; wide CJK fonts push them below the fold
		click(t, mt, s)
	}
	mt.Scroll(560, 75, 0, -2000)
	mt.Advance(time.Second)
	shot(mt, "w2-02-host-editor-footer")
	mt.Scroll(560, 75, 0, 700)
	shot(mt, "w2-02b-host-editor-scrolled")
	mt.Scroll(560, 75, 0, -700)
	clickLast(t, mt, "名称")
	mt.Type("-x")
	click(t, mt, "取消")
	shot(mt, "w2-03-discard-confirm")
}

// field is the control of the form field labeled name: of the elements named so, the one furthest right
// (the label sits left of it).
func field(t *testing.T, tt *ui.Tester, name string) ui.AccessInfo {
	t.Helper()
	var out ui.AccessInfo
	found := false
	for _, v := range tt.AccessAll() {
		if v.Name == name && v.Bounds.X > 540 && (!found || v.Bounds.X > out.Bounds.X) {
			out, found = v, true
		}
	}
	if !found {
		t.Fatal("no field", name, tt.Texts())
	}
	return out
}

// editing opens the host form on host name in a manager of the standard size.
func editing(t *testing.T, a *appui.App, name string) *ui.Tester {
	t.Helper()
	mt := ui.NewTester(a.ManagerView, 960, 640)
	if e := mt.RightClick(name); e != nil {
		t.Fatal(e)
	}
	if e := mt.ChooseMenuItem("编辑…"); e != nil {
		t.Fatal(e)
	}
	has(t, mt, "编辑主机")
	return mt
}

func twoHosts(t *testing.T) (*appui.App, *fakePlatform) {
	t.Helper()
	p := newPlatform(t)
	saveHost(t, p, model.Connection{Name: "alpha", Host: "a.example", User: "root"})
	saveHost(t, p, model.Connection{Name: "beta", Host: "b.example", User: "root"})
	a, _ := start(p)
	return a, p
}

// The buttons stay below the scrolling fields: with every section open the form is longer than the window,
// and 保存 is still in it, while the last field is out of sight until scrolled to.
func TestHostEditorButtonsStayPinned(t *testing.T) {
	a, _ := twoHosts(t)
	mt := editing(t, a, "alpha")
	for _, s := range []string{"安全", "网络", "终端", "标签与备注"} {
		mt.Scroll(560, 75, 0, 2000) // the closed headers are last; wide CJK fonts push them below the fold
		click(t, mt, s)
	}
	mt.Scroll(560, 75, 0, -2000)
	mt.Advance(time.Second)
	for _, name := range []string{"取消", "保存", "保存并连接"} {
		if b, ok := mt.Access(name); !ok || b.Bounds.Y+b.Bounds.H > 640 || b.Bounds.Y < 500 {
			t.Fatalf("%s is not pinned at the foot of the window: %v", name, b.Bounds)
		}
	}
	if n := field(t, mt, "备注"); n.Bounds.Y < 600 {
		t.Fatalf("the form fits the window, so it does not test the scroll: %v", n.Bounds)
	}
	mt.Scroll(560, 75, 0, 2000)
	if n := field(t, mt, "备注"); n.Bounds.Y > 600 {
		t.Fatal("the form did not scroll", n.Bounds)
	}
	if b, _ := mt.Access("保存"); b.Bounds.Y < 500 || b.Bounds.Y+b.Bounds.H > 640 {
		t.Fatal("the buttons moved with the form", b.Bounds)
	}
	// An error shows with the buttons, not in the scrolled-away fields.
	mt.Scroll(560, 75, 0, -2000)
	click(t, mt, "端口")
	mt.Key(ui.Cmd, ui.KeyA)
	mt.Type("0")
	click(t, mt, "保存")
	e, ok := mt.Access("端口须为 1–65535")
	if !ok || e.Bounds.Y < 500 {
		t.Fatalf("the error is not by the buttons: %v", e.Bounds)
	}
}

// Every input of the form starts at one edge, with the sections closed or open, also after a credential
// takes the place of the user and the authentication above them.
func TestHostEditorInputsShareOneEdge(t *testing.T) {
	p := newPlatform(t)
	saveHost(t, p, model.Connection{Name: "alpha", Host: "a.example", User: "root"})
	if e := p.SaveCredential(model.Credential{ID: p.NewID(), Name: "运维", User: "ops", Auth: "password"}, nil); e != nil {
		t.Fatal(e)
	}
	a, _ := start(p)
	mt := editing(t, a, "alpha")
	edge := field(t, mt, "名称").Bounds.X
	for _, name := range []string{"主机", "端口", "使用凭据", "用户名", "认证方式", "目录"} {
		if x := field(t, mt, name).Bounds.X; x != edge {
			t.Fatalf("%s starts at %v, 名称 at %v", name, x, edge)
		}
	}
	for _, s := range []string{"安全", "网络", "终端", "标签与备注"} {
		mt.Scroll(560, 75, 0, 2000) // the closed headers are last; wide CJK fonts push them below the fold
		click(t, mt, s)
	}
	mt.Scroll(560, 75, 0, -2000)
	mt.Advance(time.Second)
	for _, name := range []string{"名称", "主机", "认证方式", "已知主机", "主机指纹", "代理", "心跳", "字符编码", "退格键", "删除键", "监控状态", "标签", "收藏", "颜色", "备注"} {
		if x := field(t, mt, name).Bounds.X; x != edge {
			t.Fatalf("%s starts at %v, 名称 at %v with the sections open", name, x, edge)
		}
	}
	choose(t, mt, "使用凭据", "运维")
	for _, name := range []string{"代理", "心跳", "字符编码", "监控状态", "标签", "备注"} {
		if x := field(t, mt, name).Bounds.X; x != edge {
			t.Fatalf("%s starts at %v, 名称 at %v after choosing a credential", name, x, edge)
		}
	}
}

// Edits are not dropped without asking: not for another tile, not for 取消, not for a new or another host.
func TestHostEditorAsksBeforeDroppingEdits(t *testing.T) {
	a, p := twoHosts(t)
	mt := editing(t, a, "alpha")
	// Nothing changed: another tile and 取消 just go.
	click(t, mt, "beta")
	if mt.HasText("放弃未保存的修改？") || mt.HasText("编辑主机") {
		t.Fatal("asked about a form nobody edited", mt.Texts())
	}
	mt = editing(t, a, "alpha")
	click(t, mt, "取消")
	if mt.HasText("放弃未保存的修改？") || mt.HasText("编辑主机") {
		t.Fatal("asked about a form nobody edited", mt.Texts())
	}

	// A tile: the form stays, with the edit, until the user agrees.
	mt = editing(t, a, "alpha")
	click(t, mt, "名称")
	mt.Type("-edited")
	click(t, mt, "beta")
	has(t, mt, "放弃未保存的修改？", "「alpha-edited」的修改还没有保存，放弃后无法恢复。", "继续编辑", "放弃", "编辑主机")
	click(t, mt, "继续编辑")
	has(t, mt, "编辑主机")
	if v := field(t, mt, "名称").Value; v != "alpha-edited" {
		t.Fatal("the edit is gone", v)
	}
	if mt.HasText("放弃未保存的修改？") {
		t.Fatal("the question stayed")
	}
	click(t, mt, "beta")
	click(t, mt, "放弃")
	has(t, mt, "beta", "root@b.example:22", "连接")
	if mt.HasText("编辑主机") || mt.HasText("放弃未保存的修改？") {
		t.Fatal("the form stayed after 放弃", mt.Texts())
	}
	if c := stored(t, p, "alpha"); c.Name != "alpha" {
		t.Fatal("the dropped edit was saved", c)
	}

	// 取消.
	mt = editing(t, a, "alpha")
	click(t, mt, "名称")
	mt.Type("2")
	click(t, mt, "取消")
	has(t, mt, "放弃未保存的修改？")
	click(t, mt, "继续编辑")
	has(t, mt, "编辑主机")
	click(t, mt, "取消")
	click(t, mt, "放弃")
	if mt.HasText("编辑主机") {
		t.Fatal("取消 and 放弃 left the form open")
	}

	// A new form and another host's form replace this one only after asking.
	mt = editing(t, a, "alpha")
	click(t, mt, "名称")
	mt.Type("3")
	click(t, mt, "+ 新建连接")
	has(t, mt, "放弃未保存的修改？", "编辑主机")
	click(t, mt, "放弃")
	has(t, mt, "新建主机")
	if v := field(t, mt, "名称").Value; v != "" {
		t.Fatal("the new form kept the old edit", v)
	}
	click(t, mt, "名称")
	mt.Type("new-host")
	if e := mt.RightClick("beta"); e != nil {
		t.Fatal(e)
	}
	if e := mt.ChooseMenuItem("编辑…"); e != nil {
		t.Fatal(e)
	}
	has(t, mt, "放弃未保存的修改？", "「new-host」的修改还没有保存，放弃后无法恢复。", "新建主机")
	click(t, mt, "放弃")
	has(t, mt, "编辑主机")
	if v := field(t, mt, "名称").Value; v != "beta" {
		t.Fatal("not beta's form", v)
	}

	// Another page keeps the draft, and so the question stays with the form.
	click(t, mt, "名称")
	mt.Type("!")
	click(t, mt, "密钥")
	click(t, mt, "连接")
	if v := field(t, mt, "名称").Value; v != "beta!" {
		t.Fatal("switching the page dropped the edit", v)
	}
}

// Closing the manager window asks about unsaved edits, and the window closes once they are dropped.
func TestManagerWindowAsksBeforeClosingOverEdits(t *testing.T) {
	a, p := twoHosts(t)
	hidden := 0
	a.HideManager = func() { hidden++ }
	mt := editing(t, a, "alpha")
	if !a.ManagerMayClose() {
		t.Fatal("a form nobody edited holds the window")
	}
	mt.Frame()
	if mt.HasText("编辑主机") {
		t.Fatal("the closed window kept its form")
	}

	mt = editing(t, a, "alpha")
	click(t, mt, "名称")
	mt.Type("-x")
	if a.ManagerMayClose() {
		t.Fatal("the window closes over unsaved edits")
	}
	mt.Frame()
	has(t, mt, "放弃未保存的修改？", "继续编辑", "放弃")
	click(t, mt, "继续编辑")
	if hidden != 0 || !mt.HasText("编辑主机") {
		t.Fatal("continuing editing closed the window", hidden)
	}
	if a.ManagerMayClose() {
		t.Fatal("the window closes over unsaved edits")
	}
	mt.Frame()
	click(t, mt, "放弃")
	if hidden != 1 {
		t.Fatal("the window was not closed after 放弃", hidden)
	}
	if !a.ManagerMayClose() {
		t.Fatal("the dropped edits still hold the window")
	}
	if c := stored(t, p, "alpha"); c.Name != "alpha" {
		t.Fatal("the dropped edit was saved", c)
	}
}

// Enter in a field saves, Esc cancels as 取消 does, and ⌘S (Ctrl+S elsewhere) saves.
func TestHostEditorKeyboard(t *testing.T) {
	a, p := twoHosts(t)
	mt := editing(t, a, "alpha")
	click(t, mt, "名称")
	mt.Type("-enter")
	mt.Key(0, ui.KeyEnter)
	if mt.HasText("编辑主机") || stored(t, p, "alpha-enter").Host != "a.example" {
		t.Fatal("Enter did not save", mt.Texts())
	}

	mt = editing(t, a, "alpha-enter")
	click(t, mt, "用户名")
	mt.Type("2")
	mt.Key(ui.Cmd, ui.KeyS)
	if mt.HasText("编辑主机") || stored(t, p, "alpha-enter").User != "root2" {
		t.Fatal("the save shortcut did not save", mt.Texts())
	}

	// Esc closes a form nobody edited, and asks about one that was.
	mt = editing(t, a, "beta")
	click(t, mt, "名称")
	mt.Key(0, ui.KeyEscape)
	if mt.HasText("编辑主机") {
		t.Fatal("Esc did not cancel")
	}
	mt = editing(t, a, "beta")
	click(t, mt, "名称")
	mt.Type("!")
	mt.Key(0, ui.KeyEscape)
	has(t, mt, "放弃未保存的修改？", "编辑主机")
	mt.Key(0, ui.KeyEscape) // answers the question: keep editing
	has(t, mt, "编辑主机")
	if mt.HasText("放弃未保存的修改？") {
		t.Fatal("Esc did not answer the question")
	}
	if stored(t, p, "beta").Name != "beta" {
		t.Fatal("Esc saved")
	}

	click(t, mt, "取消")
	click(t, mt, "放弃")
	if mt.HasText("编辑主机") || stored(t, p, "beta").Name != "beta" {
		t.Fatal("取消 did not drop the edit")
	}
	// An invalid form stays open with its reason beside the buttons.
	mt = editing(t, a, "beta")
	click(t, mt, "主机")
	mt.Key(ui.Cmd, ui.KeyA)
	mt.Key(0, ui.KeyBackspace)
	mt.Key(0, ui.KeyEnter)
	has(t, mt, "编辑主机", "名称、主机和用户不能为空")
}

// The credential, proxy and folder dialogs save on Enter and ⌘S too.
func TestRecordDialogsKeyboard(t *testing.T) {
	a, p := twoHosts(t)
	mt := ui.NewTester(a.ManagerView, 960, 640)
	click(t, mt, "代理")
	click(t, mt, "新建代理…")
	clickLast(t, mt, "名称")
	mt.Type("hop")
	click(t, mt, "主机")
	mt.Type("10.9.9.9")
	mt.Key(0, ui.KeyEnter)
	snap, _ := p.Snapshot()
	if len(snap.Proxies) != 1 || snap.Proxies[0].Name != "hop" || mt.HasText("socks5") {
		t.Fatal("Enter did not save the proxy", snap.Proxies, mt.Texts())
	}
	click(t, mt, "凭据")
	click(t, mt, "新建凭据…")
	clickLast(t, mt, "名称")
	mt.Type("ops")
	clickLast(t, mt, "用户名")
	mt.Type("ops")
	mt.Key(ui.Cmd, ui.KeyS)
	snap, _ = p.Snapshot()
	if len(snap.Credentials) != 1 || snap.Credentials[0].User != "ops" || mt.HasText("认证方式") {
		t.Fatal("the save shortcut did not save the credential", snap.Credentials, mt.Texts())
	}
	click(t, mt, "连接")
	click(t, mt, "新建目录")
	mt.Type("prod")
	mt.Key(0, ui.KeyEnter)
	snap, _ = p.Snapshot()
	if len(snap.Folders) != 1 || snap.Folders[0].Name != "prod" {
		t.Fatal("Enter did not save the folder", snap.Folders)
	}
}

// Trashing several hosts is one toast, and 撤销 brings them all back.
func TestBatchTrashCanBeUndone(t *testing.T) {
	p := newPlatform(t)
	for _, n := range []string{"n1", "n2", "n3"} {
		saveHost(t, p, model.Connection{Name: n, Host: n + ".example", User: "u"})
	}
	a, _ := start(p)
	mt := ui.NewTester(a.ManagerView, 960, 640)
	click(t, mt, "n1")
	mt.ClickWith(ui.Cmd, "n2")
	mt.ClickWith(ui.Cmd, "n3")
	has(t, mt, "已选 3")
	click(t, mt, "移入回收站")
	has(t, mt, "已将 3 个主机移入回收站", "撤销")
	snap, _ := p.Snapshot()
	for _, c := range snap.Connections {
		if !c.Deleted {
			t.Fatal("not trashed", c.Name)
		}
	}
	click(t, mt, "撤销")
	snap, _ = p.Snapshot()
	if len(snap.Connections) != 3 {
		t.Fatal(snap.Connections)
	}
	for _, c := range snap.Connections {
		if c.Deleted {
			t.Fatal("undo did not restore", c.Name)
		}
	}
	has(t, mt, "n1", "n2", "n3")
}

// The hint names the key the platform adds to a choice with.
func TestManagerHintNamesThePlatformModifier(t *testing.T) {
	a, _ := twoHosts(t)
	mt := ui.NewTester(a.ManagerView, 960, 640)
	mod := "Ctrl"
	if runtime.GOOS == "darwin" {
		mod = "⌘"
	}
	has(t, mt, "单击主机查看详情，双击连接；按住 "+mod+" 或 Shift 可选择多个主机批量操作。右键目录可批量绑定认证。")
}

// A host that logs in through a credential shows the credential's user, and says so, on its tile and in its
// card, whatever user the host kept for itself.
func TestTilesShowTheCredentialsUser(t *testing.T) {
	p := newPlatform(t)
	cr := model.Credential{ID: p.NewID(), Name: "运维账号", User: "ops", Auth: "password"}
	if e := p.SaveCredential(cr, nil); e != nil {
		t.Fatal(e)
	}
	saveHost(t, p, model.Connection{Name: "bound", Host: "bound.example", User: "old", CredentialID: cr.ID})
	saveHost(t, p, model.Connection{Name: "own", Host: "own.example", User: "mine"})
	a, _ := start(p)
	mt := ui.NewTester(a.ManagerView, 960, 640)
	has(t, mt, "ops@bound.example:22", "mine@own.example:22")
	if mt.HasText("old@bound.example:22") {
		t.Fatal("the tile shows the host's own user", mt.Texts())
	}
	why := "用户来自凭据「运维账号」"
	tags := 0
	for _, v := range mt.AccessAll() {
		if v.Name == why {
			tags++
		}
	}
	if tags != 1 {
		t.Fatal("only the host with a credential has the 凭据 mark", tags)
	}
	click(t, mt, "bound")
	has(t, mt, "凭据「运维账号」· 密码")
	tags = 0
	for _, v := range mt.AccessAll() {
		if v.Name == why {
			tags++
		}
	}
	if tags != 2 || mt.HasText("old@bound.example:22") {
		t.Fatal("the card shows the host's own user, or lacks the 凭据 mark", tags, mt.Texts())
	}
}
