package transfer

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func waitJobs(t *testing.T, q *Queue, done func([]Job) bool) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if done(q.List()) {
			return
		}
		select {
		case <-deadline:
			t.Fatal("queue timeout", q.List())
		case <-ticker.C:
		}
	}
}
func TestQueueConcurrencyCancelRetryClose(t *testing.T) {
	q := New(2, nil)
	defer q.Close()
	var active, maxActive atomic.Int64
	entered := make(chan struct{}, 8)
	release := make(chan struct{})
	work := func(ctx context.Context, progress func(int64)) error {
		n := active.Add(1)
		defer active.Add(-1)
		for old := maxActive.Load(); n > old; old = maxActive.Load() {
			if maxActive.CompareAndSwap(old, n) {
				break
			}
		}
		entered <- struct{}{}
		progress(123)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	first, _ := q.Add("first", work)
	q.Add("second", work)
	<-entered
	<-entered
	third, _ := q.Add("queued", work)
	q.Cancel(third)
	waitJobs(t, q, func(js []Job) bool { return js[2].State == "cancelled" })
	q.Cancel(first)
	waitJobs(t, q, func(js []Job) bool { return js[0].State == "cancelled" })
	if e := q.Retry(first); e != nil {
		t.Fatal(e)
	}
	<-entered
	close(release)
	waitJobs(t, q, func(js []Job) bool { return js[0].State == "done" && js[1].State == "done" })
	jobs := q.List()
	if maxActive.Load() != 2 || jobs[0].Attempt != 2 || jobs[0].Bytes != 123 {
		t.Fatal(jobs, maxActive.Load())
	}
	if q.Retry(first) == nil || q.Retry("missing") == nil {
		t.Fatal("invalid retry accepted")
	}
	q.Close()
	if _, e := q.Add("closed", work); e == nil {
		t.Fatal("accepted job after close")
	}
}
func TestQueueFailureRetry(t *testing.T) {
	q := New(1, nil)
	defer q.Close()
	var calls atomic.Int64
	id, _ := q.Add("flaky", func(ctx context.Context, p func(int64)) error {
		if calls.Add(1) == 1 {
			return errors.New("transient")
		}
		return nil
	})
	waitJobs(t, q, func(js []Job) bool { return js[0].State == "failed" })
	if e := q.Retry(id); e != nil {
		t.Fatal(e)
	}
	waitJobs(t, q, func(js []Job) bool { return js[0].State == "done" })
	if q.List()[0].Attempt != 2 {
		t.Fatal(q.List())
	}
}

func TestClearCompletedRetainsActiveAndRetryableJobs(t *testing.T) {
	q := New(1, nil)
	defer q.Close()
	q.Add("done", func(context.Context, func(int64)) error { return nil })
	waitJobs(t, q, func(js []Job) bool { return js[0].State == "done" })
	q.Add("failed", func(context.Context, func(int64)) error { return errors.New("retryable") })
	waitJobs(t, q, func(js []Job) bool { return len(js) == 2 && js[1].State == "failed" })
	entered := make(chan struct{})
	q.Add("running", func(ctx context.Context, _ func(int64)) error { close(entered); <-ctx.Done(); return ctx.Err() })
	<-entered
	q.Add("queued", func(ctx context.Context, _ func(int64)) error { <-ctx.Done(); return ctx.Err() })
	if n := q.ClearCompleted(); n != 1 {
		t.Fatal(n)
	}
	js := q.List()
	if len(js) != 3 || js[0].State != "failed" {
		t.Fatal(js)
	}
	if n := q.ClearCompleted(); n != 0 {
		t.Fatal(n)
	}
	q.CancelAll()
	waitJobs(t, q, func(js []Job) bool { return js[1].State == "cancelled" && js[2].State == "cancelled" })
	if len(q.List()) != 3 {
		t.Fatal("failed/cancelled records removed")
	}
}

func TestSetTotalReachesTheJob(t *testing.T) {
	q := New(1, nil)
	defer q.Close()
	done := make(chan struct{})
	q.Add("x", func(ctx context.Context, progress func(int64)) error {
		SetTotal(ctx, 100)
		progress(40)
		close(done)
		return nil
	})
	<-done
	SetTotal(context.Background(), 5) // outside a job: no effect, no panic
	for range 100 {
		if j := q.List()[0]; j.State == "done" {
			if j.Total != 100 || j.Bytes != 40 {
				t.Fatal(j)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("job did not finish")
}

func TestClearFailedDropsFailedAndCancelledOnly(t *testing.T) {
	q := New(1, nil)
	defer q.Close()
	q.Add("done", func(context.Context, func(int64)) error { return nil })
	q.Add("failed", func(context.Context, func(int64)) error { return errors.New("boom") })
	q.Add("cancelled", func(context.Context, func(int64)) error { return context.Canceled })
	waitJobs(t, q, func(js []Job) bool { return len(js) == 3 && js[2].State == "cancelled" })
	entered := make(chan struct{})
	q.Add("running", func(ctx context.Context, _ func(int64)) error { close(entered); <-ctx.Done(); return ctx.Err() })
	<-entered
	if n := q.ClearFailed(); n != 2 {
		t.Fatal(n)
	}
	js := q.List()
	if len(js) != 2 || js[0].Label != "done" || js[1].Label != "running" {
		t.Fatal(js)
	}
	if n := q.ClearFailed(); n != 0 {
		t.Fatal(n)
	}
}

func TestItemsJobsStayItemsAfterRetry(t *testing.T) {
	q := New(1, nil)
	defer q.Close()
	id, _ := q.AddItems("remove", func(context.Context, func(int64)) error { return errors.New("boom") })
	waitJobs(t, q, func(js []Job) bool { return js[0].State == "failed" })
	if !q.List()[0].Items {
		t.Fatal("not an items job")
	}
	if e := q.Retry(id); e != nil {
		t.Fatal(e)
	}
	waitJobs(t, q, func(js []Job) bool { return js[0].State == "failed" && js[0].Attempt == 2 })
	if !q.List()[0].Items {
		t.Fatal("a retry counted bytes")
	}
}

func TestDismissDropsOneEndedJobAndNeverARunningOne(t *testing.T) {
	q := New(2, nil)
	defer q.Close()
	boom := func(context.Context, func(int64)) error { return errors.New("boom") }
	release := make(chan struct{})
	hold := func(ctx context.Context, _ func(int64)) error {
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil
	}
	a, _ := q.Add("a", boom)
	b, _ := q.Add("b", boom)
	running, _ := q.Add("running", hold)
	waitJobs(t, q, func(js []Job) bool {
		return js[0].State == "failed" && js[1].State == "failed" && js[2].State == "running"
	})
	q.Dismiss(running) // still running: stays
	q.Dismiss("no such job")
	if got := q.List(); len(got) != 3 {
		t.Fatal(got)
	}
	q.Dismiss(a)
	if got := q.List(); len(got) != 2 || got[0].ID != b || got[1].ID != running {
		t.Fatal("only a went:", got)
	}
	close(release)
}
