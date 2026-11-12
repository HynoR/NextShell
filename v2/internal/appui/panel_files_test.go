package appui_test

import (
	"slices"
	"testing"

	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/sftpfs"
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
