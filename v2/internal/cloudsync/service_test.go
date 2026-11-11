package cloudsync

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"nextshell.dev/v2/internal/cloudhttp"
	"nextshell.dev/v2/internal/cloudv2"
	"nextshell.dev/v2/internal/model"
	"nextshell.dev/v2/internal/storage"
	"sync"
	"testing"
	"time"
)

type pair struct {
	a, b   *Service
	sa, sb *storage.Store
	id     string
	keys   storage.WorkspaceSecrets
}

func setup(t *testing.T) pair {
	t.Helper()
	token, _ := cloudv2.NewAuthToken()
	ref, e := cloudhttp.NewReference(token)
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(ref)
	t.Cleanup(server.Close)
	sa, e := storage.Open(":memory:")
	if e != nil {
		t.Fatal(e)
	}
	sb, e := storage.Open(":memory:")
	if e != nil {
		t.Fatal(e)
	}
	id := cloudv2.NewID()
	wrapped, key, e := cloudv2.NewKey("E2E password not related to authentication", id)
	if e != nil {
		t.Fatal(e)
	}
	keys := storage.WorkspaceSecrets{Wrapped: wrapped, DataKey: key, AuthToken: token}
	for _, s := range []*storage.Store{sa, sb} {
		if e = s.SaveWorkspace(storage.Workspace{ID: id, Name: "encrypted workspace"}, keys); e != nil {
			t.Fatal(e)
		}
	}
	ca, _ := cloudhttp.New(server.URL, token, nil)
	cb, _ := cloudhttp.New(server.URL, token, nil)
	a, e := New(sa, id, ca, nil)
	if e != nil {
		t.Fatal(e)
	}
	b, e := New(sb, id, cb, nil)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { a.Stop(); b.Stop(); sa.Close(); sb.Close() })
	return pair{a, b, sa, sb, id, keys}
}
func must(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func TestTwoClientPushPullConflictEncryptedRecoveryAndTombstones(t *testing.T) {
	p := setup(t)
	ctx := context.Background()
	must(t, p.a.Login(ctx))
	must(t, p.a.Once(ctx))
	must(t, p.b.Once(ctx))
	id := cloudv2.NewID()
	command := model.Command{Meta: model.Meta{Scope: p.id}, ID: id, Name: "private command", Text: "echo secret-command", Group: "private"}
	must(t, p.sa.SaveCommand(command))
	must(t, p.a.Once(ctx))
	must(t, p.b.Once(ctx))
	commands, e := p.sb.Commands()
	must(t, e)
	if len(commands) != 1 || commands[0].ID != id || commands[0].Text != command.Text || commands[0].Revision != 1 {
		t.Fatal(commands)
	}
	local := model.Connection{ID: cloudv2.NewID(), Name: "local unrelated", Host: "127.0.0.1", Port: 22, User: "me", Auth: "password", Encoding: "utf-8"}
	must(t, p.sb.SaveConnection(local, nil))
	must(t, p.b.Once(ctx))
	commands[0].Text = "B local dirty command"
	must(t, p.sb.SaveCommand(commands[0]))
	aCommands, _ := p.sa.Commands()
	aCommands[0].Text = "A remote newer command"
	must(t, p.sa.SaveCommand(aCommands[0]))
	must(t, p.a.Once(ctx))
	if e = p.b.Once(ctx); !errors.Is(e, cloudhttp.ErrConflict) || p.b.Status().State != "conflict" {
		t.Fatal(e, p.b.Status())
	}
	preserved, _ := p.sb.Commands()
	if preserved[0].Text != "B local dirty command" {
		t.Fatal("conflict overwritten")
	}
	recovery, e := p.b.UseRemote(ctx)
	must(t, e)
	backup, e := p.sb.Recovery(p.id, recovery)
	must(t, e)
	var backed model.Command
	if len(backup.Records) == 1 {
		json.Unmarshal(backup.Records[0].Data, &backed)
	}
	if len(backup.Records) != 1 || backed.Text != "B local dirty command" {
		t.Fatal(backup)
	}
	after, _ := p.sb.Commands()
	if len(after) != 1 || after[0].Text != "A remote newer command" || after[0].Revision != 2 {
		t.Fatal(after)
	}
	if _, e = p.sb.Connection(local.ID); e != nil {
		t.Fatal("other scope lost", e)
	}
	must(t, p.sa.Delete("command", id))
	must(t, p.a.Once(ctx))
	must(t, p.b.Once(ctx))
	after, _ = p.sb.Commands()
	if len(after) != 0 {
		t.Fatal("tombstone not applied", after)
	}
	if _, e = p.b.UseRemote(ctx); e == nil {
		t.Fatal("replaced without newer remote")
	}
}
func TestSchedulerStartStopRestartAndConcurrentLifecycle(t *testing.T) {
	p := setup(t)
	changes := make(chan struct{}, 20)
	p.a.changed = func() {
		select {
		case changes <- struct{}{}:
		default:
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p.a.Start(ctx, time.Second)
	for p.a.Status().State != "synced" {
		select {
		case <-changes:
		case <-ctx.Done():
			t.Fatal("scheduler did not synchronize")
		}
	}
	p.a.Stop()
	if p.a.Status().Background {
		t.Fatal("background survived Stop")
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 4; j++ {
				p.a.Start(ctx, time.Second)
				p.a.Stop()
			}
		}()
	}
	wg.Wait()
	p.a.Start(ctx, time.Second)
	p.a.Stop()
	if p.a.Status().Background {
		t.Fatal("restart not stopped")
	}
	canceled, c := context.WithCancel(ctx)
	c()
	if e := p.a.Once(canceled); e == nil {
		t.Fatal("canceled operation succeeded")
	}
}
func TestSyncKeepsDirtyLocalWhenServerHistoryDiverges(t *testing.T) {
	p := setup(t)
	ctx := context.Background()
	must(t, p.a.Once(ctx))
	must(t, p.b.Once(ctx))
	c := model.Command{Meta: model.Meta{Scope: p.id}, ID: cloudv2.NewID(), Name: "B", Text: "pwd"}
	must(t, p.sb.SaveCommand(c))
	other := model.Command{Meta: model.Meta{Scope: p.id}, ID: cloudv2.NewID(), Name: "A", Text: "date"}
	must(t, p.sa.SaveCommand(other))
	must(t, p.a.Once(ctx))
	if e := p.b.Once(ctx); !errors.Is(e, cloudhttp.ErrConflict) {
		t.Fatal(e)
	}
	commands, _ := p.sb.Commands()
	if len(commands) != 1 || commands[0].ID != c.ID {
		t.Fatal("local data changed", commands)
	}
}
