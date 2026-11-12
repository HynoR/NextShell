package appui

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/transfer"
)

// toastTimeout is how long a toast shows: at least floor, long enough to read it (about 80 ms a rune),
// and never more than 10 seconds.
func toastTimeout(msg string, floor time.Duration) time.Duration {
	return min(max(floor, time.Duration(utf8.RuneCountInString(msg))*80*time.Millisecond), 10*time.Second)
}

// clip shortens s to n runes with an ellipsis.
func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// connectTitle names the connection in the toast of a failed connect; without a name it is the reason alone.
func connectTitle(host, reason string) string {
	if host == "" {
		return reason
	}
	return "连接 " + clip(host, 24) + " 失败：" + reason
}

// transferWatch remembers what each transfer was doing when last seen, to tell which ones finished since.
type transferWatch struct{ seen map[string]watched }
type watched struct {
	state   string
	attempt int
}

// finished returns the jobs that completed or failed since the last call: also those that ran their whole
// course between two frames, and a retry that failed again. A cancelled job is the user's own doing.
func (w *transferWatch) finished(jobs []transfer.Job) (done, failed []transfer.Job) {
	next := make(map[string]watched, len(jobs))
	for _, j := range jobs {
		now := watched{j.State, j.Attempt}
		next[j.ID] = now
		if w.seen[j.ID] == now {
			continue
		}
		switch j.State {
		case "done":
			done = append(done, j)
		case "failed":
			failed = append(failed, j)
		}
	}
	w.seen = next
	return
}

// transferName splits a job's label "上传 name → folder" into its verb and the name.
func transferName(label string) (verb, name string) {
	verb, rest, ok := strings.Cut(label, " ")
	if !ok {
		return "传输", label
	}
	if i := strings.LastIndex(rest, " → "); i >= 0 {
		rest = rest[:i]
	}
	return verb, clip(rest, 30)
}

// transferDoneID is the one slot the 完成 toast takes, so a batch of uploads replaces it instead of stacking.
const transferDoneID = "transfer-done"

// transferToasts says what finished: one slot for the successes (the name, or how many when several end
// together) and a toast per failure with 重试, or one with 查看 when several fail together.
func transferToasts(done, failed []transfer.Job, retry func(id string), show func()) []ui.Toast {
	var out []ui.Toast
	switch len(done) {
	case 0:
	case 1:
		verb, name := transferName(done[0].Label)
		out = append(out, ui.Toast{ID: transferDoneID, Title: name + " " + verb + "完成", Type: "success", Timeout: toastTimeout(name, 3*time.Second)})
	default:
		out = append(out, ui.Toast{ID: transferDoneID, Title: fmt.Sprintf("%d 个传输已完成", len(done)), Type: "success", Timeout: 3 * time.Second})
	}
	switch len(failed) {
	case 0:
	case 1:
		j := failed[0]
		verb, name := transferName(j.Label)
		reason, _ := humanize(j.Error)
		title := name + " " + verb + "失败：" + clip(reason, 80)
		out = append(out, ui.Toast{ID: "transfer-" + j.ID, Title: title, Type: "error", Timeout: toastTimeout(title, 6*time.Second), Action: "重试", OnAction: func() { retry(j.ID) }})
	default:
		out = append(out, ui.Toast{ID: "transfer-failed", Title: fmt.Sprintf("%d 个传输失败", len(failed)), Type: "error", Timeout: 6 * time.Second, Action: "查看", OnAction: show})
	}
	return out
}

// notifyTransfers toasts the transfers that finished since the last frame: the activity column is folded
// by default, and a failed upload would otherwise be noticed only as a badge that goes away.
func (a *App) notifyTransfers() {
	done, failed := a.transfers.finished(a.P.Transfers())
	for _, t := range transferToasts(done, failed, func(id string) { a.report(a.P.RetryTransfer(id), "") }, func() { a.layout.rightOpen = true }) {
		a.log(t.Title)
		a.addToast(t)
	}
}
