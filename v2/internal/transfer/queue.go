// Package transfer coordinates bounded concurrent jobs with cancellation and explicit retry.
package transfer

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"sync"
	"time"
)

type Job struct {
	ID, Label, State, Error string
	Bytes, Total            int64 // Total is 0 when unknown
	Attempt                 int
	Started, Finished       time.Time
}
type Work func(context.Context, func(int64)) error

type totalKey struct{}

// SetTotal tells the job running with ctx how many bytes it will move, for a percentage.
func SetTotal(ctx context.Context, n int64) {
	if set, ok := ctx.Value(totalKey{}).(func(int64)); ok {
		set(n)
	}
}
type item struct {
	job    Job
	work   Work
	ctx    context.Context
	cancel context.CancelFunc
}
type Queue struct {
	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
	items  []*item
	slots  chan struct{}
	wg     sync.WaitGroup
	wake   func()
	closed bool
}

func New(concurrency int, wake func()) *Queue {
	if concurrency < 1 {
		concurrency = 1
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Queue{ctx: ctx, cancel: cancel, slots: make(chan struct{}, concurrency), wake: wake}
}
func (q *Queue) notify() {
	if q.wake != nil {
		q.wake()
	}
}
func (q *Queue) Add(label string, work Work) (string, error) {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return "", errors.New("queue closed")
	}
	ctx, cancel := context.WithCancel(q.ctx)
	it := &item{job: Job{ID: uuid.NewString(), Label: label, State: "queued", Attempt: 1}, work: work, ctx: ctx, cancel: cancel}
	q.items = append(q.items, it)
	q.wg.Add(1)
	q.mu.Unlock()
	go q.run(it)
	q.notify()
	return it.job.ID, nil
}
func (q *Queue) run(it *item) {
	defer q.wg.Done()
	select {
	case q.slots <- struct{}{}:
	case <-it.ctx.Done():
		q.finish(it, it.ctx.Err())
		return
	}
	defer func() { <-q.slots }()
	q.mu.Lock()
	it.job.State = "running"
	it.job.Started = time.Now()
	q.mu.Unlock()
	q.notify()
	err := it.ctx.Err()
	if err == nil {
		ctx := context.WithValue(it.ctx, totalKey{}, func(n int64) { q.mu.Lock(); it.job.Total = n; q.mu.Unlock() })
		err = it.work(ctx, func(n int64) { q.mu.Lock(); it.job.Bytes = n; q.mu.Unlock(); q.notify() })
	}
	if it.ctx.Err() != nil {
		err = it.ctx.Err()
	}
	q.finish(it, err)
}
func (q *Queue) finish(it *item, err error) {
	q.mu.Lock()
	it.job.State = "done"
	if err != nil {
		it.job.State = "failed"
		it.job.Error = err.Error()
		if errors.Is(err, context.Canceled) {
			it.job.State = "cancelled"
		}
	}
	it.job.Finished = time.Now()
	it.cancel()
	q.mu.Unlock()
	q.notify()
}
func (q *Queue) List() []Job {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]Job, len(q.items))
	for i, it := range q.items {
		out[i] = it.job
	}
	return out
}
func (q *Queue) Cancel(id string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, it := range q.items {
		if it.job.ID == id {
			it.cancel()
		}
	}
}
func (q *Queue) Retry(id string) error {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return errors.New("queue closed")
	}
	for _, it := range q.items {
		if it.job.ID != id {
			continue
		}
		if it.job.State != "failed" && it.job.State != "cancelled" {
			q.mu.Unlock()
			return errors.New("only failed/cancelled jobs may retry")
		}
		ctx, cancel := context.WithCancel(q.ctx)
		next := &item{job: Job{ID: it.job.ID, Label: it.job.Label, State: "queued", Attempt: it.job.Attempt + 1}, ctx: ctx, cancel: cancel, work: it.work}
		for i, v := range q.items {
			if v == it {
				q.items[i] = next
				break
			}
		}
		q.wg.Add(1)
		q.mu.Unlock()
		go q.run(next)
		q.notify()
		return nil
	}
	q.mu.Unlock()
	return errors.New("unknown job")
}
func (q *Queue) Close() { q.mu.Lock(); q.closed = true; q.cancel(); q.mu.Unlock(); q.wg.Wait() }

// ClearCompleted removes only successful jobs, retaining failures for retry and active work.
func (q *Queue) ClearCompleted() int {
	q.mu.Lock()
	original := q.items
	kept := original[:0]
	n := 0
	for _, it := range q.items {
		if it.job.State == "done" {
			n++
		} else {
			kept = append(kept, it)
		}
	}
	clear(original[len(kept):])
	q.items = kept
	q.mu.Unlock()
	if n > 0 {
		q.notify()
	}
	return n
}
func (q *Queue) CancelAll() {
	q.mu.Lock()
	for _, it := range q.items {
		if it.job.State == "running" || it.job.State == "queued" {
			it.cancel()
		}
	}
	q.mu.Unlock()
	q.notify()
}
