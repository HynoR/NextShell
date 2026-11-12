package appui_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/sftpfs"
	"nextshell.dev/v2/internal/transfer"
)

func TestFirstListingFailureShowsReasonAndRetries(t *testing.T) {
	_, p, tt := fixture(t)
	session(t, p, tt, "one", "one", remoteHost("one"))
	click(t, tt, "SFTP")
	if tt.HasText("无法读取目录") {
		t.Fatal("an error before any failure")
	}
	p.events = append(p.events, appui.Event{FilesFailed: &appui.FileFailure{Session: "one", Path: ".", Err: `sftp: "permission denied" (SSH_FX_PERMISSION_DENIED)`}})
	tt.Frame()
	has(t, tt, "无法读取目录", "权限不足", "重试")
	if tt.HasText("回到主目录") {
		t.Fatal("the home directory itself failed: there is nowhere else to go back to")
	}
	n := len(p.listed)
	click(t, tt, "重试")
	if len(p.listed) != n+1 || p.listed[n] != "one:." {
		t.Fatal("retry did not list again", p.listed)
	}
	if tt.HasText("无法读取目录") {
		t.Fatal("the error stays while the retry is on its way")
	}
	// The remembered folder is gone: the home directory is one click away.
	p.events = append(p.events, appui.Event{FilesFailed: &appui.FileFailure{Session: "one", Path: ".", Err: "ssh: subsystem request failed"}})
	tt.Frame()
	has(t, tt, "这台服务器没有开启文件传输（SFTP）")
	listing(p, tt, "one", "/home/u", sftpfs.Entry{Name: "a.txt", Path: "/home/u/a.txt"})
	has(t, tt, "a.txt")
	if tt.HasText("无法读取目录") {
		t.Fatal("a listing that arrives clears the error")
	}
	// A folder opened from a list that shows is a toast; the list stays.
	p.events = append(p.events, appui.Event{FilesFailed: &appui.FileFailure{Session: "one", Path: "/root", Err: "permission denied"}})
	tt.Frame()
	has(t, tt, "a.txt", "权限不足")
	if tt.HasText("无法读取目录") {
		t.Fatal("a failed folder replaced a list that works")
	}
}

func TestTreeFolderThatFailsOffersRetryInPlace(t *testing.T) {
	_, p, tt := fixture(t)
	session(t, p, tt, "one", "one", remoteHost("one"))
	click(t, tt, "SFTP")
	listing(p, tt, "one", "/var/log", sftpfs.Entry{Name: "nginx", Path: "/var/log/nginx", Directory: true})
	// The folders above /var/log were asked for the tree alone; /var is refused.
	if !slices.Contains(p.listed, "one:/var") {
		t.Fatal("the tree did not ask for /var", p.listed)
	}
	p.events = append(p.events, appui.Event{FilesFailed: &appui.FileFailure{Session: "one", Path: "/var", Err: `sftp: "permission denied"`}})
	tt.Frame()
	has(t, tt, "无法读取，点击重试", "/var/log") // the list is untouched
	if tt.HasText("无法读取目录") {
		t.Fatal("a tree folder took the pane")
	}
	n := len(p.listed)
	click(t, tt, "重试读取 /var")
	tt.Frame()
	if p.listed[len(p.listed)-1] != "one:/var" || len(p.listed) != n+1 {
		t.Fatal("retry", p.listed)
	}
	if tt.HasText("无法读取，点击重试") {
		t.Fatal("the failure stays while the retry is on its way")
	}
}

// removalPlatform counts and cancels without a server.
type removalPlatform struct {
	*fakePlatform
	counts    [][]string
	gaveUp    int
	cancelled []string
}

func (p *removalPlatform) CountRemoval(session string, paths []string) func() {
	p.counts = append(p.counts, paths)
	return func() { p.gaveUp++ }
}
func (p *removalPlatform) CancelTransfer(id string) { p.cancelled = append(p.cancelled, id) }

func removalFixture(t *testing.T) (*removalPlatform, *ui.Tester) {
	t.Helper()
	p := &removalPlatform{fakePlatform: newPlatform(t)}
	a := appui.New(p)
	tt := ui.NewTester(a.View, 1280, 800)
	session(t, p.fakePlatform, tt, "one", "one", remoteHost("one"))
	click(t, tt, "SFTP")
	listing(p.fakePlatform, tt, "one", "/d", sftpfs.Entry{Name: "src", Path: "/d/src", Directory: true}, sftpfs.Entry{Name: "a.txt", Path: "/d/a.txt"})
	return p, tt
}
func deleteRow(t *testing.T, tt *ui.Tester, row string) {
	t.Helper()
	if e := tt.RightClick(row); e != nil {
		t.Fatal(e)
	}
	if e := tt.ChooseMenuItem("删除"); e != nil {
		t.Fatal(e)
	}
}

func TestDeletingAFolderCountsFirstThenRemovesWithProgress(t *testing.T) {
	p, tt := removalFixture(t)
	deleteRow(t, tt, "打开 src")
	has(t, tt, "正在统计要删除的内容…")
	if len(p.counts) != 1 || !slices.Equal(p.counts[0], []string{"/d/src"}) || len(p.actions) != 0 {
		t.Fatal("the folder is counted before anything is deleted", p.counts, p.actions)
	}
	if tt.HasText("删除后无法恢复") {
		t.Fatal("the confirm came before the count")
	}
	p.events = append(p.events, appui.Event{Removal: &appui.RemovalCount{Session: "one", Targets: []appui.RemovalTally{{Path: "/d/src", Files: 12, Dirs: 3}}}})
	tt.Frame()
	has(t, tt, "删除远程文件", "src\n将删除 12 个文件、3 个文件夹，删除后无法恢复。")
	if tt.HasText("只能删除") {
		t.Fatal("the old limit is still in the copy")
	}
	click(t, tt, "删除 1 项")
	want := appui.TransferRequest{Session: "one", Kind: "remove", Remote: "/d/src", Items: 15}
	if len(p.queued) != 1 || p.queued[0] != want || len(p.actions) != 0 {
		t.Fatal("the folder is deleted by a job that knows what to expect", p.queued, p.actions)
	}
	// Progress in the footer, and a way to stop it.
	p.jobs = []transfer.Job{{ID: "job", State: "running", Items: true, Bytes: 5, Total: 15, Attempt: 1}}
	tt.Frame()
	has(t, tt, "正在删除 5 / 15 项")
	click(t, tt, "取消删除")
	if !slices.Equal(p.cancelled, []string{"job"}) {
		t.Fatal("cancel", p.cancelled)
	}
	// However it ends, the folder is listed again.
	n := len(p.listed)
	p.jobs[0].State = "cancelled"
	tt.Frame()
	if len(p.listed) != n+1 || p.listed[n] != "one:/d" || tt.HasText("正在删除") {
		t.Fatal("not refreshed when the delete ended", p.listed[n:])
	}
}

func TestDeleteCountCanBeGivenUpAndFailuresShow(t *testing.T) {
	p, tt := removalFixture(t)
	deleteRow(t, tt, "打开 src")
	click(t, tt, "取消统计")
	if p.gaveUp != 1 || tt.HasText("正在统计") {
		t.Fatal("not given up", p.gaveUp)
	}
	p.events = append(p.events, appui.Event{Removal: &appui.RemovalCount{Session: "one", Targets: []appui.RemovalTally{{Path: "/d/src", Files: 1, Dirs: 1}}}})
	tt.Frame()
	if tt.HasText("删除后无法恢复") {
		t.Fatal("a count nobody waits for opened a confirm")
	}
	deleteRow(t, tt, "打开 src")
	p.events = append(p.events, appui.Event{Removal: &appui.RemovalCount{Session: "one", Err: `sftp: "permission denied" (SSH_FX_PERMISSION_DENIED)`}})
	tt.Frame()
	has(t, tt, "权限不足")
	if tt.HasText("正在统计") || tt.HasText("删除后无法恢复") {
		t.Fatal("a failed count leaves the footer or a confirm", tt.Texts())
	}
	// A selection of plain files needs no count.
	deleteRow(t, tt, "选择 a.txt")
	has(t, tt, "a.txt\n删除后无法恢复。")
	if len(p.counts) != 2 {
		t.Fatal("files were counted", p.counts)
	}
}

func TestFileKeysDeleteRenameAndParent(t *testing.T) {
	_, p, tt := fixture(t)
	session(t, p, tt, "one", "one", remoteHost("one"))
	click(t, tt, "SFTP")
	listing(p, tt, "one", "/srv/app", sftpfs.Entry{Name: "a.txt", Path: "/srv/app/a.txt"}, sftpfs.Entry{Name: "b.txt", Path: "/srv/app/b.txt"})
	click(t, tt, "选择 a.txt")
	tt.Key(0, ui.KeyDelete)
	has(t, tt, "删除远程文件", "a.txt\n删除后无法恢复。")
	click(t, tt, "删除 1 项")
	if len(p.actions) != 1 || p.actions[0] != [3]string{"remove", "/srv/app/a.txt", ""} {
		t.Fatal("delete key", p.actions)
	}
	// Two rows chosen: both are asked about.
	click(t, tt, "选择 a.txt")
	if e := tt.ClickWith(ui.Cmd, "选择 b.txt"); e != nil {
		t.Fatal(e)
	}
	tt.Key(ui.Cmd, ui.KeyBackspace) // the Mac's delete key
	has(t, tt, "a.txt、b.txt\n删除后无法恢复。")
	click(t, tt, "取消")
	// F2 renames the row in place; the name has the keys while it is typed.
	click(t, tt, "选择 b.txt")
	tt.Key(0, ui.KeyF2)
	has(t, tt, "重命名 b.txt")
	n := len(p.listed)
	tt.Key(0, ui.KeyBackspace)
	tt.Key(0, ui.KeyDelete)
	if len(p.listed) != n || tt.HasText("删除远程文件") {
		t.Fatal("Backspace and Delete in the name acted on the list", p.listed[n:])
	}
	tt.Key(0, ui.KeyEscape)
	if tt.HasText("重命名 b.txt") {
		t.Fatal("Escape keeps the name open")
	}
	// Backspace goes up, and stops at the root.
	click(t, tt, "选择 b.txt")
	tt.Key(0, ui.KeyBackspace)
	if p.listed[len(p.listed)-1] != "one:/srv" {
		t.Fatal("backspace", p.listed)
	}
	listing(p, tt, "one", "/srv", sftpfs.Entry{Name: "app", Path: "/srv/app", Directory: true}, sftpfs.Entry{Name: "c.txt", Path: "/srv/c.txt"})
	click(t, tt, "选择 c.txt")
	tt.Key(0, ui.KeyBackspace)
	if p.listed[len(p.listed)-1] != "one:/" {
		t.Fatal("backspace again", p.listed)
	}
	listing(p, tt, "one", "/", sftpfs.Entry{Name: "srv", Path: "/srv", Directory: true}, sftpfs.Entry{Name: "d.txt", Path: "/d.txt"})
	n = len(p.listed)
	click(t, tt, "选择 d.txt")
	tt.Key(0, ui.KeyBackspace)
	if len(p.listed) != n {
		t.Fatal("there is nothing above the root", p.listed[n:])
	}
}

// dirPlatform is a shell that tells its folder.
type dirPlatform struct {
	*fakePlatform
	dir string
}

func (p *dirPlatform) SessionDir(string) string { return p.dir }

func TestFollowToggleIsInTheToolbarAndHintsWhatToDo(t *testing.T) {
	p := &dirPlatform{fakePlatform: newPlatform(t)}
	a := appui.New(p)
	tt := ui.NewTester(a.View, 1280, 800)
	session(t, p.fakePlatform, tt, "one", "one", remoteHost("one"))
	click(t, tt, "SFTP")
	listing(p.fakePlatform, tt, "one", "/srv/app", sftpfs.Entry{Name: "a.txt", Path: "/srv/app/a.txt"})
	click(t, tt, "更多")
	if slices.Contains(tt.Menu(), "跟随终端目录") {
		t.Fatal("the toggle is still in the menu", tt.Menu())
	}
	tt.CloseMenu()
	if tt.HasText("还没有收到终端的当前目录") {
		t.Fatal("a hint before the toggle is on")
	}
	click(t, tt, "跟随终端目录")
	has(t, tt, "还没有收到终端的当前目录，暂时无法跟随。", "插入目录跟踪脚本")
	for _, s := range tt.Texts() {
		if strings.Contains(s, "osc7") || strings.Contains(s, ".sh") {
			t.Fatal("the hint names a file:", s)
		}
	}
	click(t, tt, "跟随终端目录")
	if tt.HasText("还没有收到终端的当前目录") {
		t.Fatal("the hint outlives the toggle")
	}
	// Tracking off in the settings: the way to turn it on.
	a.Prefs.ShellIntegration = "off"
	click(t, tt, "跟随终端目录")
	has(t, tt, "目录跟踪在设置里是关闭的", "Shell 集成")
	click(t, tt, "跟随终端目录")
	// The shell reports its folder: the list goes there, and no hint.
	p.dir = "/var/www"
	click(t, tt, "跟随终端目录")
	if p.listed[len(p.listed)-1] != "one:/var/www" || tt.HasText("暂时无法跟随") {
		t.Fatal("follow", p.listed)
	}
}
