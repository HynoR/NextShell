package desktop

import (
	"context"
	"errors"
	"github.com/egoist/mygo/plugins/terminal"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/shellintegration"
	"nextshell.dev/v2/internal/sshclient"
	"strings"
	"time"
)

// hookBudget is the wall-clock time a new session has to get its server ready and show a prompt.
var hookBudget = 10 * time.Second

// integrateShell is Shell 集成 "自动": it prepares the directory hook on the server and, once the shell
// shows a prompt, types the one short line that sources it. It runs in the platform from the moment the
// session exists, on its own clock: a window that is hidden or behind others draws no frames, and
// nothing here waits for one. A session that cannot get there in hookBudget is left alone.
func (p *Platform) integrateShell(session string, t *terminal.Terminal, s *sshclient.Session) {
	p.launch(func() {
		ctx, cancel := context.WithTimeout(p.ctx, hookBudget)
		defer cancel()
		e := shellintegration.Install(ctx, s)
		if e == nil {
			e = waitForPrompt(ctx, t)
		}
		if e == nil {
			t.Send([]byte(shellintegration.SourceLine))
			return
		}
		if errors.Is(e, context.DeadlineExceeded) {
			e = errors.New("十秒内没有等到提示符或服务器没有准备好，已跳过")
		}
		p.emit(appui.Event{ShellHook: &appui.ShellHookResult{Session: session, Err: e}})
	})
}

// waitForPrompt polls the terminal's text (which its own goroutine keeps current, drawn or not).
func waitForPrompt(ctx context.Context, t *terminal.Terminal) error {
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for !promptShown(t.Text()) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.Done():
			return errors.New("会话已结束")
		case <-tick.C:
		}
	}
	return nil
}

// promptShown is true when the screen's last line looks like a shell prompt.
func promptShown(screen string) bool {
	lines := strings.Split(strings.TrimRight(screen, " \n"), "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	return last != "" && strings.ContainsAny(last[len(last)-1:], "$#%>")
}
