// Package cloudsync coordinates the independent encrypted v2 protocol. No UI dependencies.
package cloudsync

import (
	"context"
	"errors"
	"nextshell.dev/v2/internal/cloudhttp"
	"nextshell.dev/v2/internal/cloudv2"
	"nextshell.dev/v2/internal/storage"
	"sync"
	"time"
)

type Status struct {
	WorkspaceID, State, Error, LocalHead, RemoteHead, LastSync, RecoveryID string
	Background                                                             bool
}
type Service struct {
	life    sync.Mutex
	store   *storage.Store
	client  *cloudhttp.Client
	id      string
	run     chan struct{}
	mu      sync.Mutex
	status  Status
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	changed func()
}

func New(store *storage.Store, id string, client *cloudhttp.Client, changed func()) (*Service, error) {
	if _, e := store.WorkspaceSecrets(id); e != nil {
		return nil, e
	}
	return &Service{store: store, client: client, id: id, run: make(chan struct{}, 1), status: Status{WorkspaceID: id, State: "disconnected"}, changed: changed}, nil
}
func (s *Service) Status() Status { s.mu.Lock(); defer s.mu.Unlock(); return s.status }
func (s *Service) set(state string, e error, local, remote string) {
	s.mu.Lock()
	s.status.State = state
	s.status.LocalHead = local
	s.status.RemoteHead = remote
	s.status.Error = ""
	if e != nil {
		s.status.Error = "同步未完成；检查本地服务、密钥或冲突（不会自动覆盖）"
	}
	if state == "synced" {
		s.status.LastSync = time.Now().In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("2006-01-02 15:04:05 UTC+8")
	}
	s.mu.Unlock()
	if s.changed != nil {
		s.changed()
	}
}
func (s *Service) Login(ctx context.Context) error {
	e := s.client.Login(ctx)
	if e != nil {
		s.set("error", e, "", "")
	} else {
		s.set("ready", nil, "", "")
	}
	return e
}
func (s *Service) acquire(ctx context.Context) error {
	select {
	case s.run <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (s *Service) Once(ctx context.Context) error {
	if e := s.acquire(ctx); e != nil {
		return e
	}
	defer func() { <-s.run }()
	p, e := s.store.SyncPosition(s.id)
	if e != nil {
		return e
	}
	s.set("syncing", nil, p.Head, "")
	h, e := s.client.History(ctx, s.id, p.Head)
	if e != nil {
		state := "error"
		if errors.Is(e, cloudhttp.ErrConflict) {
			state = "conflict"
		}
		s.set(state, e, p.Head, "")
		return e
	}
	if len(h.Commits) > 0 {
		if p.Generation != p.Baseline {
			s.set("conflict", cloudhttp.ErrConflict, p.Head, h.Head)
			return cloudhttp.ErrConflict
		}
		for _, raw := range h.Commits {
			commit, e := cloudv2.DecodeCommit(raw)
			if e == nil {
				e = s.store.ApplySyncCommit(commit)
			}
			if e != nil {
				s.set("conflict", e, p.Head, h.Head)
				return e
			}
		}
		s.set("synced", nil, h.Head, h.Head)
		return nil
	}
	if p.Head == "" || p.Generation != p.Baseline {
		snapshot, head, g, e := s.store.SyncSnapshot(s.id)
		if e != nil {
			s.set("error", e, p.Head, h.Head)
			return e
		}
		keys, e := s.store.WorkspaceSecrets(s.id)
		if e != nil {
			return e
		}
		commit, e := cloudv2.Seal(snapshot, keys.Wrapped.KeyID, head, keys.DataKey)
		if e == nil {
			e = s.client.Push(ctx, commit, keys.Wrapped)
		}
		if e == nil {
			e = s.store.AcceptPublished(s.id, head, commit.ID, g)
		}
		if e != nil {
			state := "error"
			if errors.Is(e, cloudhttp.ErrConflict) {
				state = "conflict"
			}
			s.set(state, e, head, h.Head)
			return e
		}
		s.set("synced", nil, commit.ID, commit.ID)
		return nil
	}
	s.set("synced", nil, p.Head, h.Head)
	return nil
}

// UseRemote must be called ONLY after explicit UI confirmation. Local edits are encrypted for recovery.
func (s *Service) UseRemote(ctx context.Context) (string, error) {
	if e := s.acquire(ctx); e != nil {
		return "", e
	}
	defer func() { <-s.run }()
	p, e := s.store.SyncPosition(s.id)
	if e != nil {
		return "", e
	}
	history, e := s.client.History(ctx, s.id, p.Head)
	if e != nil {
		return "", e
	}
	if len(history.Commits) == 0 {
		return "", errors.New("no newer remote snapshot")
	}
	keys, e := s.store.WorkspaceSecrets(s.id)
	if e != nil {
		return "", e
	}
	head := p.Head
	var snapshot cloudv2.Snapshot
	for _, raw := range history.Commits {
		commit, e := cloudv2.DecodeCommit(raw)
		if e != nil {
			return "", e
		}
		snapshot, e = cloudv2.Open(commit, keys.DataKey, s.id, keys.Wrapped.KeyID, head)
		if e != nil {
			return "", e
		}
		head = commit.ID
	}
	id, e := s.store.ReplaceFromRemote(snapshot, head, p.Head, p.Generation)
	if e != nil {
		return "", e
	}
	s.mu.Lock()
	s.status.RecoveryID = id
	s.mu.Unlock()
	s.set("synced", nil, head, head)
	return id, nil
}
func (s *Service) Start(ctx context.Context, interval time.Duration) {
	s.life.Lock()
	defer s.life.Unlock()
	if interval < time.Second {
		interval = time.Second
	}
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.status.Background = true
	s.wg.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.wg.Done()
		defer func() {
			cancel()
			s.mu.Lock()
			s.cancel = nil
			s.status.Background = false
			s.mu.Unlock()
			if s.changed != nil {
				s.changed()
			}
		}()
		delay := interval
		for {
			e := s.Once(ctx)
			if ctx.Err() != nil {
				return
			}
			if errors.Is(e, cloudhttp.ErrConflict) || s.Status().State == "conflict" {
				s.mu.Lock()
				s.status.Background = false
				s.mu.Unlock()
				return
			}
			if e != nil {
				delay *= 2
				if delay > 5*time.Minute {
					delay = 5 * time.Minute
				}
			} else {
				delay = interval
			}
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
}
func (s *Service) Stop() {
	s.life.Lock()
	defer s.life.Unlock()
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.status.Background = false
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.wg.Wait()
}
