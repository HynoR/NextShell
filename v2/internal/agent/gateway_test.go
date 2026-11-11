package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

type fake struct {
	mu    sync.Mutex
	calls []string
	block chan struct{}
}

func (*fake) Hosts() ([]Host, error) {
	return []Host{{ID: "host", Name: "test", Host: "localhost", Username: "tester"}}, nil
}
func (*fake) Sessions() []Session {
	return []Session{{ID: "remote", Title: "password=TOPSECRET", Connected: true}, {ID: "local", Local: true, Connected: true}}
}
func (*fake) Commands() ([]Command, error) {
	return []Command{{Name: "demo", Command: "echo token=TOPSECRET"}}, nil
}
func (f *fake) mark(s string)                          { f.mu.Lock(); f.calls = append(f.calls, s); f.mu.Unlock() }
func (f *fake) SaveCommand(c Command) (Command, error) { f.mark("save:" + c.Command); return c, nil }
func (*fake) Read(string) (string, error)              { return "a\npassword=TOPSECRET\nlast", nil }
func (f *fake) Exec(ctx context.Context, id, command string) (ExecResult, error) {
	f.mark("exec:" + command)
	if f.block != nil {
		close(f.block)
		<-ctx.Done()
		return ExecResult{}, ctx.Err()
	}
	return ExecResult{Output: "token=TOPSECRET", Stderr: "password=TOPSECRET"}, nil
}
func (f *fake) Control(id, data string) error                { f.mark("control:" + data); return nil }
func (f *fake) Focus(id string) error                        { f.mark("focus:" + id); return nil }
func (f *fake) Open(context.Context, string) (string, error) { f.mark("open"); return "remote", nil }
func invoke(g *Gateway, owner, tool string, a Arguments) Result {
	raw, _ := json.Marshal(a)
	return g.Call(context.Background(), owner, "test client", tool, raw)
}
func pendingID(t *testing.T, r Result) string {
	t.Helper()
	if !r.OK {
		t.Fatal(r.Error)
	}
	data, ok := r.Data.(map[string]any)
	if !ok || data["status"] != "pending" {
		t.Fatal(r)
	}
	return data["requestId"].(string)
}
func TestDiscoveryScreenCommandsAreRedactedAndReadOnly(t *testing.T) {
	f := &fake{}
	g := New(f, nil)
	for _, tool := range []string{"host_list", "session_list", "command_search", "session_read", "session_history"} {
		t.Run(tool, func(t *testing.T) {
			r := invoke(g, "owner", tool, Arguments{Target: "remote"})
			raw, _ := json.Marshal(r)
			if !r.OK || strings.Contains(string(raw), "TOPSECRET") {
				t.Fatal(string(raw))
			}
		})
	}
	if len(f.calls) != 0 || len(g.Pending()) != 0 {
		t.Fatal("read mutated backend")
	}
}
func TestConsentBoundToOwnerToolOriginalArgumentsAndSingleUse(t *testing.T) {
	f := &fake{}
	g := New(f, nil)
	id := pendingID(t, invoke(g, "owner", "exec", Arguments{Target: "remote", Command: "pwd"}))
	for _, tc := range []struct{ owner, tool string }{{"other", "exec"}, {"owner", "session_focus"}} {
		r := invoke(g, tc.owner, tc.tool, Arguments{RequestID: id})
		if r.OK || r.Error.Code != "not_found" {
			t.Fatal(r)
		}
	}
	if !g.Respond(id, true) || len(f.calls) != 0 {
		t.Fatal("approval executed instead of waiting for original client poll")
	}
	r := invoke(g, "owner", "exec", Arguments{RequestID: id, Command: "ATTACK", Target: "local"})
	if !r.OK || len(f.calls) != 1 || f.calls[0] != "exec:pwd" {
		t.Fatal(r, f.calls)
	}
	raw, _ := json.Marshal(r)
	if strings.Contains(string(raw), "TOPSECRET") {
		t.Fatal("exec secret leaked")
	}
	if invoke(g, "owner", "exec", Arguments{RequestID: id}).OK {
		t.Fatal("approval replay")
	}
}
func TestDeniedExpiredAndSemanticArgumentDedupe(t *testing.T) {
	g := New(&fake{}, nil)
	now := time.Now()
	g.now = func() time.Time { return now }
	yes := true
	a := Arguments{Name: "save", Command: "pwd", AppendCR: &yes}
	id := pendingID(t, invoke(g, "owner", "command_save", a))
	yes2 := true
	a.AppendCR = &yes2
	if second := pendingID(t, invoke(g, "owner", "command_save", a)); second != id {
		t.Fatal("pointer identity defeated dedupe")
	}
	if !g.Respond(id, false) {
		t.Fatal("deny failed")
	}
	r := invoke(g, "owner", "command_save", a)
	if r.OK || r.Error.Code != "denied" {
		t.Fatal("denied retry accepted", r)
	}
	now = now.Add(3 * time.Minute)
	if g.Respond(id, true) {
		t.Fatal("expired approval accepted")
	}
	if r = invoke(g, "owner", "command_save", Arguments{RequestID: id}); r.OK {
		t.Fatal("expired request executed")
	}
	pendingID(t, invoke(g, "owner", "command_save", a))
}
func TestMutationConsentCoversOpenSaveSignalFocusAndKillCancellation(t *testing.T) {
	f := &fake{}
	g := New(f, nil)
	for _, tc := range []struct {
		tool string
		a    Arguments
	}{{"session_open", Arguments{Target: "host"}}, {"command_save", Arguments{Name: "save", Command: "pwd"}}, {"session_send_signal", Arguments{Target: "remote", Signal: "interrupt"}}, {"session_focus", Arguments{Target: "remote"}}} {
		t.Run(tc.tool, func(t *testing.T) {
			before := len(f.calls)
			id := pendingID(t, invoke(g, "owner", tc.tool, tc.a))
			if len(f.calls) != before {
				t.Fatal("mutated before approval")
			}
			g.Respond(id, true)
			r := invoke(g, "owner", tc.tool, Arguments{RequestID: id})
			if !r.OK || len(f.calls) != before+1 {
				t.Fatal(r)
			}
		})
	}
	f.block = make(chan struct{})
	id := pendingID(t, invoke(g, "owner", "exec", Arguments{Target: "remote", Command: "wait"}))
	g.Respond(id, true)
	done := make(chan Result, 1)
	go func() { done <- invoke(g, "owner", "exec", Arguments{RequestID: id}) }()
	select {
	case <-f.block:
	case <-time.After(time.Second):
		t.Fatal("exec not started")
	}
	g.Halt(true)
	select {
	case r := <-done:
		if r.OK || r.Error.Code != "timeout" {
			t.Fatal(r)
		}
	case <-time.After(time.Second):
		t.Fatal("kill did not cancel")
	}
	if invoke(g, "owner", "session_list", Arguments{}).OK {
		t.Fatal("halt bypass")
	}
	g.Halt(false)
	if !invoke(g, "owner", "session_list", Arguments{}).OK {
		t.Fatal("resume failed")
	}
}
func TestForegroundLocalSensitivePolicyAndBlacklist(t *testing.T) {
	g := New(&fake{}, nil)
	for _, tc := range []struct {
		name, code string
		a          Arguments
	}{{"foreground", "unavailable", Arguments{Target: "remote", Command: "pwd", Mode: "foreground"}}, {"local", "unavailable", Arguments{Target: "local", Command: "pwd"}}, {"missing", "not_found", Arguments{Target: "unknown", Command: "pwd"}}, {"danger", "forbidden", Arguments{Target: "remote", Command: "sudo rm -rf /"}}, {"env", "consent_required", Arguments{Target: "remote", Command: "cat /app/.env.production"}}} {
		t.Run(tc.name, func(t *testing.T) {
			r := invoke(g, "owner", "exec", tc.a)
			if r.OK || r.Error.Code != tc.code {
				t.Fatal(r)
			}
		})
	}
	pendingID(t, invoke(g, "owner", "exec", Arguments{Target: "remote", Command: "cat .env", AllowSensitive: true}))
	if e := g.SetBlacklist([]string{"custom-danger", "re:^echo forbidden"}); e != nil {
		t.Fatal(e)
	}
	for _, cmd := range []string{"echo CUSTOM-DANGER", "echo forbidden"} {
		r := invoke(g, "owner", "exec", Arguments{Target: "remote", Command: cmd})
		if r.OK || r.Error.Code != "forbidden" {
			t.Fatal(cmd, r)
		}
	}
	if e := g.SetBlacklist([]string{"re:["}); e == nil {
		t.Fatal("invalid regex silently replaced policy")
	}
}
func TestPendingAndActivityLimitsExpireWithoutUI(t *testing.T) {
	g := New(&fake{}, nil)
	now := time.Now()
	g.now = func() time.Time { return now }
	for i := 0; i < 100; i++ {
		pendingID(t, invoke(g, "owner", "command_save", Arguments{Name: fmt.Sprint(i), Command: "pwd"}))
	}
	if r := invoke(g, "owner", "command_save", Arguments{Name: "overflow", Command: "pwd"}); r.OK || r.Error.Code != "busy" {
		t.Fatal(r)
	}
	now = now.Add(3 * time.Minute)
	pendingID(t, invoke(g, "owner", "command_save", Arguments{Name: "after expiry", Command: "pwd"}))
	if len(g.Activity()) != 100 {
		t.Fatal("activity not bounded")
	}
}

// Risk cases are specifications from the first-party 1.x terminal command policy,
// not third-party SSH-client code. A blacklist is only defense in depth, NOT a shell sandbox.
func TestPortedCommandRiskAndRedactionSpecs(t *testing.T) {
	for _, cmd := range []string{"rm -rf /", "sudo /bin/rm --recursive --force /*", "mkfs.ext4 /dev/sda", "shutdown -h now", "reboot", "dd if=/tmp/x of=/dev/sda", "echo x > /dev/sdb", "kill -9 -1", "chmod -R 777 /", ":(){ :|:& };:"} {
		t.Run(cmd, func(t *testing.T) {
			if !Dangerous(cmd, nil) {
				t.Fatal("risk not detected", cmd)
			}
		})
	}
	for _, cmd := range []string{"ls -la", "rm /tmp/test", "echo ok > /dev/null", "printf hello"} {
		t.Run(cmd, func(t *testing.T) {
			if Dangerous(cmd, nil) {
				t.Fatal("safe command blocked", cmd)
			}
		})
	}
	for _, input := range []string{"password=TOPSECRET", "--password TOPSECRET", "Bearer TOPSECRET", "https://user:TOPSECRET@host", "-----BEGIN OPENSSH PRIVATE KEY-----\nTOPSECRET\n-----END OPENSSH PRIVATE KEY-----"} {
		t.Run(input[:min(20, len(input))], func(t *testing.T) {
			if strings.Contains(Redact(input), "TOPSECRET") {
				t.Fatal("secret not redacted")
			}
		})
	}
}

func TestV1CommandRiskExactVectors(t *testing.T) {
	danger := []string{"rm -rf /", "/bin/rm --recursive --force -- /", "mkfs.ext4 /dev/sda1", "dd if=/dev/zero of=/dev/sda bs=1M", "shutdown -h now", "reboot", ":(){ :|:& };:", "echo destroy > /dev/nvme0n1", "chmod -R 777 /", "kill -9 -1", "r''m -r -f /", "/usr/bin/r\\m -fr -- /*", "sudo -- rm -rf //", "sudo -u root /sbin/mkfs.xfs /dev/sdb1", "dd 'of=/dev/disk0' if=/dev/zero", `printf x 2>"/dev/sda"`, "command rm -rf /", "env LC_ALL=C rm -rf /", "busybox rm -rf /", "sh -c 'rm -rf /'"}
	safe := []string{"ls -la /var/log", "du -sh -- * | sort -h | tail -20", "systemctl restart sshd", "touch /tmp/agent-test", "grep error /var/log/app.log 2>/dev/null", "rm -rf /tmp/agent-scratch", "dd if=./disk.img of=./copy.img", "kill -9 4242"}
	for i, group := range [][]string{danger, safe} {
		for _, cmd := range group {
			t.Run(cmd, func(t *testing.T) {
				if Dangerous(cmd, nil) != (i == 0) {
					t.Fatal("differs from packages/terminal/src/command-risk.spec.ts", cmd)
				}
			})
		}
	}
	for _, cmd := range []string{"cat .env", "cat /srv/app/.env.production", "echo KEY=1 >> .env", "echo x >.env.local", "sed -i 's/a/b/' ./.env && ls", "cat .env.example.md"} {
		t.Run(cmd, func(t *testing.T) {
			if !Sensitive(cmd) {
				t.Fatal("sensitive vector missed", cmd)
			}
		})
	}
	for _, cmd := range []string{"env | grep PATH", "cat environment.txt", "ls .envrc"} {
		t.Run(cmd, func(t *testing.T) {
			if Sensitive(cmd) {
				t.Fatal("look-alike flagged", cmd)
			}
		})
	}
}
