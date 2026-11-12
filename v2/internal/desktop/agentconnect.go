package desktop

import (
	"context"
	"fmt"
	"github.com/egoist/mygo"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/model"
)

// byAgent marks a context whose connection attempt an agent asked for: its dialogs say so, and wait for
// the user instead of for a call's timeout.
type byAgent struct{}

func fromAgent(ctx context.Context) bool { return ctx.Value(byAgent{}) != nil }

// attempt is one in-flight connection an agent asked for.
type attempt struct {
	done chan struct{}
	err  error
}

// connectForAgent connects c on behalf of an agent and waits for the result or for ctx, whichever comes
// first. The attempt does not belong to ctx: when the call gives up (an agent's call has a timeout of
// seconds, a person needs longer to read a host key), the attempt and its dialog stay until the user
// answers or cancels, and a later request for the same connection joins that attempt instead of
// opening a second dialog.
func (p *Platform) connectForAgent(ctx context.Context, c model.Connection) error {
	p.mu.Lock()
	a := p.agentConnects[c.ID]
	if a == nil {
		a = &attempt{done: make(chan struct{})}
		if p.agentConnects == nil {
			p.agentConnects = map[string]*attempt{}
		}
		p.agentConnects[c.ID] = a
		p.mu.Unlock()
		p.launch(func() {
			a.err = p.connectContext(context.WithValue(p.ctx, byAgent{}, true), c, "")
			p.mu.Lock()
			delete(p.agentConnects, c.ID)
			p.mu.Unlock()
			close(a.done)
			if a.err != nil && p.ctx.Err() == nil {
				// The call may be long gone; the user is the one who can see this.
				p.emit(appui.Event{Message: fmt.Sprintf("Agent 发起的连接「%s」失败：%v", c.Name, a.err)})
			}
		})
	} else {
		p.mu.Unlock()
	}
	select {
	case <-a.done:
		return a.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// askAttention flashes the window (the Dock icon bounces on macOS) without taking focus: a dialog an agent
// raised cannot be seen in a window that is hidden or behind others.
func askAttention() {
	for _, w := range mygo.Windows() {
		w.FlashFrame(true)
	}
}
