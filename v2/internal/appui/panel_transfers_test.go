package appui_test

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/transfer"
)

// transferPlatform forgets the failed jobs it is told to clear.
type transferPlatform struct{ *fakePlatform }

func (p *transferPlatform) ClearFailedTransfers() int {
	var kept []transfer.Job
	for _, j := range p.jobs {
		if j.State != "failed" && j.State != "cancelled" {
			kept = append(kept, j)
		}
	}
	n := len(p.jobs) - len(kept)
	p.jobs = kept
	return n
}

func TestFailedTransfersTakeTheStripBadgeAndLeadTheList(t *testing.T) {
	p := &transferPlatform{fakePlatform: newPlatform(t)}
	a := appui.New(p)
	tt := ui.NewTester(a.View, 1280, 1000)
	session(t, p.fakePlatform, tt, "one", "one", remoteHost("one"))
	now := time.Now()
	p.jobs = []transfer.Job{
		{ID: "1", Label: "上传 release.tar.gz → /srv/app", State: "running", Bytes: 1 << 20, Total: 4 << 20, Attempt: 1, Started: now},
		{ID: "2", Label: "下载 /var/log/syslog", State: "done", Bytes: 2200000, Attempt: 1},
		{ID: "3", Label: "上传 config.yaml → /etc/app", State: "failed", Error: `sftp: "permission denied" (SSH_FX_PERMISSION_DENIED)`, Attempt: 1},
		{ID: "4", Label: "上传 old.bin → /srv", State: "cancelled", Attempt: 1},
	}
	tt.Frame()
	// Folded: one failure and one running transfer, and the badge is the failure's.
	has(t, tt, "1 个传输失败，1 个进行中")
	if tt.HasText("1 个传输进行中") {
		t.Fatal("the failure is hidden behind the running count")
	}
	click(t, tt, "1 个传输失败，1 个进行中")
	has(t, tt, "1 个传输失败，1 个已取消", "上传 config.yaml → /etc/app", "权限不足", "清除失败项", "清除已完成")
	// What failed comes before what runs.
	failed, _ := tt.Find("上传 config.yaml → /etc/app")
	running, _ := tt.Find("上传 release.tar.gz → /srv/app")
	if failed.Y >= running.Y {
		t.Fatal("the failed entry is below the running one", failed, running)
	}
	click(t, tt, "清除失败项")
	has(t, tt, "已清除 2 个任务")
	// The failure toast may still be up and name the reason; the list entry itself must be gone.
	if tt.HasText("上传 config.yaml → /etc/app") || tt.HasText("清除失败项") {
		t.Fatal("cleared entries are still listed", tt.Texts())
	}
	click(t, tt, "收起活动栏")
	has(t, tt, "1 个传输进行中")
	if tt.HasText("个传输失败") {
		t.Fatal("the failure badge outlives the failure")
	}
	// A failure alone, with nothing running.
	p.jobs = []transfer.Job{{ID: "3", Label: "下载 x", State: "failed", Error: "boom", Attempt: 1}, {ID: "5", Label: "下载 y", State: "failed", Error: "boom", Attempt: 1}}
	tt.Frame()
	has(t, tt, "2 个传输失败")
}
