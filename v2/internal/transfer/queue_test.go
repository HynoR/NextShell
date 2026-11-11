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
