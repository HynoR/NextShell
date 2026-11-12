package appui

import (
	"strings"
	"testing"
	"time"

	"nextshell.dev/v2/internal/transfer"
)

func TestToastTimeoutGrowsWithTheTextAndIsCapped(t *testing.T) {
	for _, v := range []struct {
		text  string
		floor time.Duration
		want  time.Duration
	}{
		{"", 3 * time.Second, 3 * time.Second},
		{"已加入传输队列", 3 * time.Second, 3 * time.Second},                        // 7 runes: the floor holds
		{strings.Repeat("字", 50), 3 * time.Second, 4 * time.Second},          // 80 ms a rune, counted in runes not bytes
		{strings.Repeat("a", 200), 3 * time.Second, 10 * time.Second},        // capped
		{strings.Repeat("字", 50), 6 * time.Second, 6 * time.Second},          // an error keeps its longer floor
		{strings.Repeat("字", 120), 6 * time.Second, 9600 * time.Millisecond}, // and grows past it
	} {
		if got := toastTimeout(v.text, v.floor); got != v.want {
			t.Errorf("%d runes, floor %v: %v, want %v", len([]rune(v.text)), v.floor, got, v.want)
		}
	}
}

func TestConnectTitleNamesTheHost(t *testing.T) {
	if got := connectTitle("prod-web-01", "连接超时"); got != "连接 prod-web-01 失败：连接超时" {
		t.Fatal(got)
	}
	if got := connectTitle("", "连接超时"); got != "连接超时" {
		t.Fatal("no name, no prefix:", got)
	}
	if got := connectTitle(strings.Repeat("h", 40), "连接超时"); !strings.Contains(got, "… 失败：连接超时") {
		t.Fatal("a long name is clipped, the reason stays:", got)
	}
}

func TestTransferWatchReportsEachFinishOnce(t *testing.T) {
	var w transferWatch
	job := func(id, state string, attempt int) transfer.Job {
		return transfer.Job{ID: id, Label: "上传 " + id + ".zip → /srv", State: state, Attempt: attempt}
	}
	names := func(jobs []transfer.Job) (out []string) {
		for _, j := range jobs {
			out = append(out, j.ID)
		}
		return
	}
	eq := func(got []transfer.Job, want ...string) {
		t.Helper()
		if g := strings.Join(names(got), ","); g != strings.Join(want, ",") {
			t.Fatalf("got [%s], want %v", g, want)
		}
	}

	done, failed := w.finished(nil)
	eq(done)
	eq(failed)
	// Queued and running jobs say nothing.
	done, failed = w.finished([]transfer.Job{job("a", "queued", 1), job("b", "running", 1)})
	eq(done)
	eq(failed)
	// One ends well, one badly; a job never seen before it ended (it ran between two frames) counts too.
	done, failed = w.finished([]transfer.Job{job("a", "done", 1), job("b", "failed", 1), job("c", "done", 1)})
	eq(done, "a", "c")
	eq(failed, "b")
	// The next frame repeats nothing.
	done, failed = w.finished([]transfer.Job{job("a", "done", 1), job("b", "failed", 1), job("c", "done", 1)})
	eq(done)
	eq(failed)
	// Cancelling is the user's own doing.
	done, failed = w.finished([]transfer.Job{job("a", "done", 1), job("b", "cancelled", 1), job("c", "done", 1)})
	eq(done)
	eq(failed)
	// A retry that fails again is a new failure; one that succeeds is a success.
	_, _ = w.finished([]transfer.Job{job("a", "done", 1), job("b", "queued", 2), job("c", "done", 1)})
	done, failed = w.finished([]transfer.Job{job("a", "done", 1), job("b", "failed", 2), job("c", "done", 1)})
	eq(done)
	eq(failed, "b")
	done, _ = w.finished([]transfer.Job{job("a", "done", 1), job("b", "done", 3), job("c", "done", 1)})
	eq(done, "b")
	// Cleared jobs are forgotten: the same ID never comes back, but the map must not grow.
	_, _ = w.finished(nil)
	if len(w.seen) != 0 {
		t.Fatal("cleared jobs are still remembered", w.seen)
	}
}

func TestTransferToastsNameTheFileAndOfferRetry(t *testing.T) {
	var retried []string
	opened := 0
	retry, show := func(id string) { retried = append(retried, id) }, func() { opened++ }
	up := transfer.Job{ID: "j1", Label: "上传 报告.pdf → /srv/www", State: "done"}
	down := transfer.Job{ID: "j2", Label: "下载 syslog → /Users/me/Downloads", State: "done"}
	bad := transfer.Job{ID: "j3", Label: "上传 big.iso → /srv", State: "failed", Error: "write tcp 10.0.0.1:22: connection reset by peer"}

	if got := transferToasts([]transfer.Job{up}, nil, retry, show); len(got) != 1 || got[0].Title != "报告.pdf 上传完成" || got[0].Action != "" {
		t.Fatal(got)
	}
	if got := transferToasts([]transfer.Job{down}, nil, retry, show); got[0].Title != "syslog 下载完成" {
		t.Fatal(got)
	}
	// Several finishing together share one toast, which replaces the last one instead of stacking.
	got := transferToasts([]transfer.Job{up, down}, nil, retry, show)
	if len(got) != 1 || got[0].Title != "2 个传输已完成" || got[0].ID != transferDoneID {
		t.Fatal(got)
	}
	if one := transferToasts([]transfer.Job{up}, nil, retry, show); one[0].ID != transferDoneID {
		t.Fatal("a single success must use the same slot")
	}

	got = transferToasts(nil, []transfer.Job{bad}, retry, show)
	if len(got) != 1 || got[0].Title != "big.iso 上传失败：连接被服务器重置" || got[0].Type != "error" || got[0].Action != "重试" {
		t.Fatalf("%+v", got)
	}
	got[0].OnAction()
	if len(retried) != 1 || retried[0] != "j3" {
		t.Fatal("重试 retried", retried)
	}
	// Failures are not coalesced with successes.
	if got := transferToasts([]transfer.Job{up}, []transfer.Job{bad}, retry, show); len(got) != 2 {
		t.Fatal(got)
	}
	// Several failures at once: one toast that opens the transfer list.
	got = transferToasts(nil, []transfer.Job{bad, bad}, retry, show)
	if len(got) != 1 || got[0].Title != "2 个传输失败" || got[0].Action != "查看" {
		t.Fatalf("%+v", got)
	}
	got[0].OnAction()
	if opened != 1 {
		t.Fatal("查看 did not open the activity column")
	}
	// A label without the usual shape still gets a toast.
	if got := transferToasts([]transfer.Job{{Label: "x"}}, nil, retry, show); got[0].Title != "x 传输完成" {
		t.Fatal(got)
	}
}
