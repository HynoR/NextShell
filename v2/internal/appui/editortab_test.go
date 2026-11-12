package appui_test

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/remoteedit"
)

// The remote saves of these tests are the test's own business: the real platform would try an SSH session.
func (p *keysPlatform) SaveRemote(session string, doc remoteedit.Document, text string) {}

type savedDoc struct {
	session string
	doc     remoteedit.Document
	text    string
}

// editPlatform records what the editor saves and opens outside.
type editPlatform struct {
	*fakePlatform
	saved    []savedDoc
	external [][]string
}

func (p *editPlatform) SaveRemote(session string, doc remoteedit.Document, text string) {
	p.saved = append(p.saved, savedDoc{session, doc, text})
}
func (p *editPlatform) ExternalEdit(session, path, executable string, args []string) {
	p.external = append(p.external, args)
}

// editorFixture is the app with one connected host, "one", and an editor window that records its shows and hides.
func editorFixture(t *testing.T) (a *appui.App, p *editPlatform, tt *ui.Tester, shown, hidden *int) {
	t.Helper()
	p = &editPlatform{fakePlatform: newPlatform(t)}
	a = appui.New(p)
	shown, hidden = new(int), new(int)
	a.ShowEditor, a.HideEditor = func() { *shown++ }, func() { *hidden++ }
	tt = ui.NewTester(a.View, 1280, 800)
	session(t, p.fakePlatform, tt, "one", "one", remoteHost("one"))
	return
}

// open delivers the file as the platform would, on session id.
func open(p *editPlatform, tt *ui.Tester, id, path, text string) {
	p.events = append(p.events, appui.Event{Editor: &appui.EditorResult{Session: id, Document: remoteedit.Document{Path: path, Text: text, Hash: "h"}}})
	tt.Frame()
}

// An editor is a tab: the file's name, its host's colour, the unsaved dot (which the × replaces under the pointer).
func TestEditorTabShowsNameAndUnsavedDot(t *testing.T) {
	a, p, tt, _, _ := editorFixture(t)
	open(p, tt, "one", "/etc/nginx/nginx.conf", "worker_processes 1;\n")
	has(t, tt, "nginx.conf", "关闭标签 nginx.conf", "保存")
	if tt.HasText("未保存") || a.DirtyDocumentCount() != 0 {
		t.Fatal("clean file marked unsaved")
	}
	tt.Type("# ")
	tt.Frame()
	has(t, tt, "未保存")
	// Under the pointer the dot gives way to the ×.
	tab, _ := tt.Find("nginx.conf")
	tt.Move(tab.X+tab.W/2, tab.Y+tab.H/2)
	tt.Frame()
	if tt.HasText("未保存") {
		t.Fatal("the dot stayed under the pointer")
	}
	// The terminal comes back with its own tab.
	click(t, tt, "one")
	if tt.HasText("保存") && tt.HasText("远程文本") {
		t.Fatal("the editor stayed over the terminal")
	}
}

// Same names read apart by their folder.
func TestEditorTabsOfOneNameShowTheirFolder(t *testing.T) {
	_, p, tt, _, _ := editorFixture(t)
	open(p, tt, "one", "/etc/nginx/app.conf", "a\n")
	open(p, tt, "one", "/etc/apache/app.conf", "b\n")
	has(t, tt, "nginx/app.conf", "apache/app.conf")
}

// ⌘S saves through the host's session; a dirty tab's close asks 保存 / 不保存 / 取消.
func TestEditorSaveShortcutAndDirtyClose(t *testing.T) {
	a, p, tt, _, _ := editorFixture(t)
	open(p, tt, "one", "/etc/app.conf", "a=1\n")
	tt.Type("x")
	tt.Key(ui.Cmd, ui.KeyS)
	if len(p.saved) != 1 || p.saved[0].session != "one" || p.saved[0].doc.Path != "/etc/app.conf" || p.saved[0].text != "xa=1\n" {
		t.Fatal("⌘S", p.saved)
	}
	// 取消 keeps it, 保存 saves and closes once the save is acknowledged.
	click(t, tt, "关闭标签 app.conf")
	has(t, tt, "关闭未保存的文件", "取消", "不保存", "保存")
	click(t, tt, "取消")
	if a.DirtyDocumentCount() != 1 {
		t.Fatal("cancel")
	}
	click(t, tt, "关闭标签 app.conf")
	click(t, tt, "保存")
	if len(p.saved) != 2 || !tt.HasText("关闭标签 app.conf") {
		t.Fatal("the save, and the file stays open until it is acknowledged", len(p.saved))
	}
	p.events = append(p.events, appui.Event{Editor: &appui.EditorResult{Saved: true, Session: "one", Document: remoteedit.Document{Path: "/etc/app.conf", Text: "xa=1\n", Hash: "h2"}}, Message: "已保存远程文件", ForEditor: true})
	tt.Frame()
	if tt.HasText("关闭标签 app.conf") || a.DirtyDocumentCount() != 0 {
		t.Fatal("not closed after the save")
	}
}

// A conflict (the remote file changed) refuses the save: the tab stays open and unsaved, and says why.
func TestEditorSaveConflictKeepsTheTabOpen(t *testing.T) {
	a, p, tt, _, _ := editorFixture(t)
	open(p, tt, "one", "/etc/app.conf", "a=1\n")
	tt.Type("x")
	click(t, tt, "关闭标签 app.conf")
	click(t, tt, "保存") // asks to save, then close
	p.events = append(p.events, appui.Event{Message: "remote file changed since open; reload before saving", ForEditor: true})
	tt.Frame()
	has(t, tt, "远程文件在打开后被修改了，请重新载入后再保存", "关闭标签 app.conf")
	if a.DirtyDocumentCount() != 1 {
		t.Fatal("the unsaved file was dropped")
	}
	// A later save that works does not close the tab: nobody asked for that any more.
	p.events = append(p.events, appui.Event{Editor: &appui.EditorResult{Saved: true, Session: "one", Document: remoteedit.Document{Path: "/etc/app.conf", Text: "xa=1\n", Hash: "h2"}}})
	tt.Frame()
	has(t, tt, "关闭标签 app.conf")
}

// 在新窗口打开 moves the document, with its edits and undo history, into the editor window; 回到主窗口 moves it back.
func TestEditorPopOutKeepsStateAndPopBack(t *testing.T) {
	a, p, tt, shown, hidden := editorFixture(t)
	open(p, tt, "one", "/etc/app.conf", "a=1\n")
	tt.Type("x")
	ed := ui.NewTester(a.EditorView, 900, 640)
	click(t, tt, "在新窗口打开")
	if *shown != 1 || tt.HasText("关闭标签 app.conf") || tt.HasText("远程文本") {
		t.Fatal("pop out", *shown)
	}
	ed.Frame()
	has(t, ed, "● /etc/app.conf", "回到主窗口")
	if a.DirtyDocumentCount() != 1 {
		t.Fatal("the edit was lost in the move")
	}
	// ⌘Z in the window undoes the typing: the history came along.
	ed.Key(ui.Cmd, ui.KeyZ)
	ed.Frame()
	if a.DirtyDocumentCount() != 0 {
		t.Fatal("undo history lost", ed.Texts())
	}
	ed.Key(ui.Cmd|ui.Shift, ui.KeyZ)
	click(t, ed, "回到主窗口")
	ed.Frame()
	tt.Frame()
	if *hidden != 1 || !tt.HasText("关闭标签 app.conf") || !tt.HasText("远程文本") {
		t.Fatal("pop back", *hidden)
	}
	if a.DirtyDocumentCount() != 1 {
		t.Fatal("the edit was lost coming back")
	}
}

// The editor tab follows its host as a tool tab does: closing the host's last session asks about the
// unsaved files first; with another session of the host left, nothing is asked.
func TestEditorTabFollowsItsHost(t *testing.T) {
	a, p, tt, _, _ := editorFixture(t)
	open(p, tt, "one", "/etc/app.conf", "a=1\n")
	open(p, tt, "one", "/etc/hosts", "127.0.0.1 localhost\n")
	tt.Type("x") // /etc/hosts is the one shown
	// A second session to the host: closing one asks only because it is connected.
	session(t, p.fakePlatform, tt, "two", "one", remoteHost("one"))
	click(t, tt, "关闭标签 one")
	has(t, tt, "关闭 one？")
	if tt.HasText("这些文件的修改还没有保存") {
		t.Fatal("asked about files with a session left")
	}
	click(t, tt, "断开并关闭")
	if len(a.Tabs) != 1 || !tt.HasText("关闭标签 app.conf") {
		t.Fatal("the editor tabs went with one of two sessions", len(a.Tabs))
	}
	// The last session: the unsaved file is named, 取消 keeps everything.
	click(t, tt, "关闭标签 one")
	has(t, tt, "这些文件的修改还没有保存，关闭后会丢失：", "/etc/hosts", "连接也会断开")
	if tt.HasText("不再询问") {
		t.Fatal("unsaved files are asked about whatever 不再询问 says")
	}
	click(t, tt, "取消")
	if len(a.Tabs) != 1 || a.DirtyDocumentCount() != 1 {
		t.Fatal("cancel")
	}
	click(t, tt, "关闭标签 one")
	click(t, tt, "不保存并关闭")
	if len(a.Tabs) != 0 || a.DirtyDocumentCount() != 0 || tt.HasText("关闭标签 app.conf") || tt.HasText("关闭标签 hosts") {
		t.Fatal("the editor tabs outlived their host", len(a.Tabs), a.DirtyDocumentCount())
	}
}

// With only clean files the host closes as it did, and the clean tabs go with it.
func TestCleanEditorTabsCloseWithTheirHost(t *testing.T) {
	a, p, tt, _, _ := editorFixture(t)
	open(p, tt, "one", "/etc/app.conf", "a=1\n")
	click(t, tt, "关闭标签 one")
	has(t, tt, "关闭 one？")
	click(t, tt, "断开并关闭")
	if len(a.Tabs) != 0 || tt.HasText("关闭标签 app.conf") {
		t.Fatal("clean editor tab stayed")
	}
}

// A reconnect replaces the session: the document saves through the new one.
func TestEditorSavesThroughTheReconnectedSession(t *testing.T) {
	_, p, tt, _, _ := editorFixture(t)
	open(p, tt, "one", "/etc/app.conf", "a=1\n")
	tt.Type("x")
	next := &appui.Session{ID: "two", Replaces: "one", Title: "one", Terminal: newTerminal(t, "$ "), Connection: remoteHost("one")}
	p.events = append(p.events, appui.Event{Session: next})
	tt.Frame()
	click(t, tt, "app.conf")
	tt.Key(ui.Cmd, ui.KeyS)
	if len(p.saved) != 1 || p.saved[0].session != "two" {
		t.Fatal("saved through a closed session", p.saved)
	}
}

// An agent closing the last session never costs the user an edit: the unsaved file stays, in its tab.
func TestAgentClosingAHostKeepsUnsavedEditorTabs(t *testing.T) {
	a, p, tt, _, _ := editorFixture(t)
	open(p, tt, "one", "/etc/app.conf", "a=1\n")
	open(p, tt, "one", "/etc/clean.conf", "b\n")
	click(t, tt, "app.conf")
	tt.Type("y")
	p.events = append(p.events, appui.Event{CloseID: "one"})
	tt.Frame()
	if len(a.Tabs) != 0 || a.DirtyDocumentCount() != 1 {
		t.Fatal("agent close", len(a.Tabs), a.DirtyDocumentCount())
	}
	has(t, tt, "关闭标签 app.conf")
	if tt.HasText("关闭标签 clean.conf") {
		t.Fatal("the clean file stayed")
	}
	click(t, tt, "app.conf")
	has(t, tt, "远程文本") // still editable, and the tab strip shows without any session
	tt.Key(ui.Cmd, ui.KeyS)
	has(t, tt, "这个服务器的连接已关闭，重新连接后才能保存")
	if len(p.saved) != 0 {
		t.Fatal("saved without a session")
	}
}

// Quitting with unsaved files asks, whether or not sessions are open.
func TestQuitAsksAboutUnsavedEditorTabs(t *testing.T) {
	a, p, tt, _, _ := editorFixture(t)
	prefs := p.Preferences()
	prefs.NoCloseConfirm = true // the sessions alone would not ask
	if e := p.SavePreferences(prefs); e != nil {
		t.Fatal(e)
	}
	a.Prefs = prefs
	if a.ExitNeedsConfirm() {
		t.Fatal("asked with nothing unsaved")
	}
	open(p, tt, "one", "/etc/app.conf", "a=1\n")
	tt.Type("x")
	if !a.ExitNeedsConfirm() {
		t.Fatal("an unsaved file did not ask")
	}
	a.ConfirmExit = true
	tt.Frame()
	has(t, tt, "退出 NextShell？", "仍有 1 个会话、1 个未保存的文件")
}

// The external editor's arguments are plain text: spaces separate, quotes keep spaces together.
func TestEditorExternalArgumentsArePlainText(t *testing.T) {
	_, p, tt, _, _ := editorFixture(t)
	open(p, tt, "one", "/etc/app.conf", "a=1\n")
	click(t, tt, "外部编辑器参数")
	tt.Type(`--wait --new-window "my profile"`)
	click(t, tt, "用外部编辑器打开")
	if len(p.external) != 1 || len(p.external[0]) != 3 || p.external[0][0] != "--wait" || p.external[0][2] != "my profile" {
		t.Fatal("args", p.external)
	}
}

// The palette's 远程编辑器 brings the document forward from another tab.
func TestPaletteShowsTheEditor(t *testing.T) {
	_, p, tt, _, _ := editorFixture(t)
	open(p, tt, "one", "/etc/app.conf", "a=1\n")
	click(t, tt, "关闭标签 app.conf") // clean: closed; open again, then leave it for the terminal
	open(p, tt, "one", "/etc/app.conf", "a=1\n")
	tt.Key(appMod(), ui.KeyK)
	tt.Type("远程编辑器")
	tt.Key(0, ui.KeyEnter)
	has(t, tt, "远程文本")
}
