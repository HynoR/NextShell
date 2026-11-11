package desktop

import (
	"context"
	"errors"
	"github.com/egoist/mygo/plugins/terminal"
	"golang.org/x/crypto/ssh"
	"nextshell.dev/v2/internal/agent"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/cloudhttp"
	"nextshell.dev/v2/internal/cloudsync"
	"nextshell.dev/v2/internal/cloudv2"
	"nextshell.dev/v2/internal/mcpendpoint"
	"nextshell.dev/v2/internal/model"
	"nextshell.dev/v2/internal/shellintegration"
	"nextshell.dev/v2/internal/storage"
	"strings"
	"time"
	"unicode"
)

func (p *Platform) CloudOverview() appui.CloudOverview {
	p.featureMu.Lock()
	defer p.featureMu.Unlock()
	workspaces, e := p.store.Workspaces()
	out := appui.CloudOverview{Workspaces: workspaces}
	if e != nil {
		return out
	}
	for _, w := range workspaces {
		if service := p.clouds[w.ID]; service != nil {
			out.Statuses = append(out.Statuses, service.Status())
		} else {
			out.Statuses = append(out.Statuses, cloudsync.Status{WorkspaceID: w.ID, State: "disconnected"})
		}
	}
	return out
}
func (p *Platform) CloudConnect(endpoint, id, name, auth, encryption string, join bool) {
	p.operation(func(ctx context.Context) error {
		if encryption != "" && auth == encryption {
			return errors.New("认证 token 不得复用为加密口令")
		}
		var keys storage.WorkspaceSecrets
		var e error
		isNew := id == "" || join
		if !isNew {
			keys, e = p.store.WorkspaceSecrets(id)
			if e != nil {
				return e
			}
			if auth == "" {
				auth = keys.AuthToken
			}
		}
		client, e := cloudhttp.New(endpoint, auth, nil)
		if e != nil {
			return e
		}
		if e = client.Login(ctx); e != nil {
			return e
		}
		if join {
			known, e := p.store.Workspaces()
			if e != nil {
				return e
			}
			for _, w := range known {
				if w.ID == id {
					return errors.New("工作区已存在，请用连接现有工作区")
				}
			}
			wrapped, e := client.Key(ctx, id)
			if e != nil {
				return e
			}
			key, e := cloudv2.Unlock(encryption, wrapped)
			if e != nil {
				return errors.New("云加密口令不能解锁工作区")
			}
			keys = storage.WorkspaceSecrets{Wrapped: wrapped, DataKey: key, AuthToken: auth}
			name = "待同步工作区"
		} else if id == "" {
			id = cloudv2.NewID()
			wrapped, key, e := cloudv2.NewKey(encryption, id)
			if e != nil {
				return e
			}
			keys = storage.WorkspaceSecrets{Wrapped: wrapped, DataKey: key, AuthToken: auth}
		}
		keys.AuthToken = auth
		if isNew {
			if strings.TrimSpace(name) == "" {
				return errors.New("工作区名称不能为空")
			}
		} else {
			ws, e := p.store.Workspaces()
			if e != nil {
				return e
			}
			for _, w := range ws {
				if w.ID == id {
					name = w.Name
				}
			}
		}
		if e = p.store.SaveWorkspace(storage.Workspace{ID: id, Name: name}, keys); e != nil {
			return e
		}
		service, e := cloudsync.New(p.store, id, client, p.Invalidate)
		if e != nil {
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		p.featureMu.Lock()
		defer p.featureMu.Unlock()
		if e = ctx.Err(); e != nil {
			return e
		}
		if old := p.clouds[id]; old != nil {
			old.Stop()
		}
		p.clouds[id] = service
		p.emit(appui.Event{Message: "云登录完成（仅本地参考协议）", WorkspaceID: id})
		return nil
	})
}
func (p *Platform) CloudSync(id string, remote bool) {
	p.operation(func(ctx context.Context) error {
		p.featureMu.Lock()
		service := p.clouds[id]
		p.featureMu.Unlock()
		if service == nil {
			return errors.New("先登录本地参考云服务")
		}
		var e error
		if remote {
			service.Stop()
			var recovery string
			recovery, e = service.UseRemote(ctx)
			if e == nil {
				p.emit(appui.Event{Message: "已采用远端版本；本地加密恢复快照 ID：" + recovery})
			}
		} else {
			e = service.Once(ctx)
		}
		if e == nil {
			p.emit(appui.Event{Message: "云同步完成"})
		}
		return e
	})
}
func (p *Platform) CloudBackground(id string, on bool) {
	p.featureMu.Lock()
	service := p.clouds[id]
	p.featureMu.Unlock()
	if service != nil {
		if on {
			service.Start(p.ctx, 30*time.Second)
		} else {
			service.Stop()
		}
	}
}
func (p *Platform) CloudCopy(id string, ids []string) error {
	return p.store.CopyLocalToWorkspace(id, ids)
}
func (p *Platform) AgentOverview() appui.AgentOverview {
	p.featureMu.Lock()
	url := p.endpointURL
	p.featureMu.Unlock()
	return appui.AgentOverview{URL: url, Halted: p.gateway.Halted(), Pending: p.gateway.Pending(), Activity: p.gateway.Activity()}
}
func (p *Platform) AgentEndpoint(on bool) error {
	p.featureMu.Lock()
	defer p.featureMu.Unlock()
	if !on {
		if p.endpoint != nil {
			p.endpoint.Close()
		}
		p.endpoint = nil
		p.endpointURL = ""
		return nil
	}
	if p.endpoint != nil {
		return nil
	}
	token, e := p.store.MCPToken(false)
	if e != nil {
		return e
	}
	endpoint, e := mcpendpoint.New(token, p.gateway)
	if e != nil {
		return e
	}
	url, e := endpoint.Start("41777")
	if e != nil {
		return e
	}
	p.endpoint = endpoint
	p.endpointURL = url
	p.gateway.Halt(false)
	return nil
}
func (p *Platform) AgentToken(rotate bool) (string, error) {
	p.featureMu.Lock()
	defer p.featureMu.Unlock()
	token, e := p.store.MCPToken(rotate)
	if e != nil {
		return "", e
	}
	if rotate && p.endpoint != nil {
		e = p.endpoint.Rotate(token)
	}
	return token, e
}
func (p *Platform) AgentHalt(h bool) { p.gateway.Halt(h) }
func (p *Platform) AgentRespond(id string, approve bool) {
	p.gateway.Respond(id, approve)
	p.Invalidate()
}
func (p *Platform) AgentBlacklist(text string) {
	lines := strings.Split(text, "\n")
	if len(lines) > 100 {
		lines = lines[:100]
	}
	for i := range lines {
		if len(lines[i]) > 512 {
			lines[i] = lines[i][:512]
		}
	}
	if e := p.gateway.SetBlacklist(lines); e != nil {
		p.emit(appui.Event{Message: "黑名单正则无效，已保留原规则"})
	}
}
func (p *Platform) closeFeatures() {
	p.featureMu.Lock()
	services := p.clouds
	p.clouds = map[string]*cloudsync.Service{}
	endpoint := p.endpoint
	p.endpoint = nil
	p.endpointURL = ""
	p.featureMu.Unlock()
	for _, service := range services {
		service.Stop()
	}
	if endpoint != nil {
		endpoint.Close()
	}
	p.gateway.Halt(true)
}
func cleanTerminalText(s string, limit int) string {
	s = agent.Redact(s)
	var out strings.Builder
	for _, r := range s {
		if unicode.IsControl(r) {
			continue
		}
		if out.Len()+4 > limit {
			break
		}
		out.WriteRune(r)
	}
	return out.String()
}
func (p *Platform) TerminalTitle(id string) string {
	p.mu.Lock()
	t := p.sessions[id]
	p.mu.Unlock()
	if t == nil {
		return ""
	}
	return cleanTerminalText(t.Title(), 120)
}
func (p *Platform) ShellCommand(shell string) (string, error) { return shellintegration.Command(shell) }
func (p *Platform) terminalOptions(o terminal.Options) terminal.Options {
	prefs := p.Preferences()
	o.Font = terminalFont(prefs)
	o.Scrollback = prefs.ScrollbackMiB << 20
	o.NoBlink = !prefs.CursorBlink
	switch prefs.Cursor {
	case "bar":
		o.Cursor = terminal.CursorBar
	case "underline":
		o.Cursor = terminal.CursorUnderline
	default:
		o.Cursor = terminal.CursorBlock
	}
	o.NoClipboardWrite = !prefs.ClipboardWrite
	o.ClipboardRead = terminal.ClipboardDeny
	if prefs.ClipboardReadAsk {
		o.ClipboardRead = terminal.ClipboardAsk
	}
	if o.Conn == nil && prefs.LocalShell != "" {
		o.Command = []string{prefs.LocalShell}
	}
	o.OpenLink = func(link string) bool {
		if err := model.ValidateTerminalURL(link); err != nil {
			p.emit(appui.Event{Message: err.Error()})
		} else {
			p.emit(appui.Event{Link: link})
		}
		return true
	}
	o.OnTitle = func(string) { p.Invalidate() }
	o.OnBell = func() {
		if p.Preferences().Notifications {
			p.emit(appui.Event{Message: "终端响铃（BEL）"})
		}
	}
	o.OnNotify = func(title, body string) {
		if p.Preferences().Notifications {
			p.emit(appui.Event{Message: "终端通知：" + cleanTerminalText(title+" · "+body, 512)})
		}
	}
	return o
}

type agentBackend struct{ p *Platform }

func (b agentBackend) Hosts() ([]agent.Host, error) {
	connections, e := b.p.store.Connections()
	if e != nil {
		return nil, e
	}
	var out []agent.Host
	for _, c := range connections {
		if !c.Deleted {
			out = append(out, agent.Host{ID: c.ID, Name: c.Name, Host: c.Host, Port: c.Port, Username: c.User, Tags: append([]string{}, c.Tags...)})
		}
	}
	return out, nil
}
func (b agentBackend) Sessions() []agent.Session {
	b.p.mu.Lock()
	type item struct {
		id     string
		t      *terminal.Terminal
		c      model.Connection
		remote bool
	}
	var items []item
	for id, t := range b.p.sessions {
		items = append(items, item{id, t, b.p.credentials[id], b.p.sshSessions[id] != nil})
	}
	b.p.mu.Unlock()
	var out []agent.Session
	for _, i := range items {
		connected := true
		select {
		case <-i.t.Done():
			connected = false
		default:
		}
		title := cleanTerminalText(i.t.Title(), 120)
		if title == "" {
			title = i.c.Name
		}
		out = append(out, agent.Session{ID: i.id, ConnectionID: i.c.ID, Title: title, Local: !i.remote, CWD: i.t.Dir(), Connected: connected})
	}
	return out
}
func (b agentBackend) Commands() ([]agent.Command, error) {
	commands, e := b.p.store.Commands()
	if e != nil {
		return nil, e
	}
	var out []agent.Command
	for _, c := range commands {
		ws := ""
		if c.ScopeID() != model.LocalScope {
			ws = c.ScopeID()
		}
		out = append(out, agent.Command{ID: c.ID, Name: c.Name, Command: c.Text, Group: c.Group, WorkspaceID: ws, AppendCR: c.AppendCR})
	}
	return out, nil
}
func (b agentBackend) SaveCommand(c agent.Command) (agent.Command, error) {
	scope := c.WorkspaceID
	if scope == "" {
		scope = model.LocalScope
	}
	if c.ID == "" {
		c.ID = b.p.NewID()
	}
	e := b.p.store.SaveCommand(model.Command{Meta: model.Meta{Scope: scope}, ID: c.ID, Name: c.Name, Group: c.Group, Text: c.Command, AppendCR: c.AppendCR})
	return c, e
}
func (b agentBackend) Read(id string) (string, error) {
	if b.p.Exited(id) {
		return "", errors.New("session closed")
	}
	return b.p.ScreenText(id), nil
}
func (b agentBackend) Exec(ctx context.Context, id, command string) (agent.ExecResult, error) {
	result := agent.ExecResult{SessionID: id, Mode: "background"}
	session, e := b.p.ssh(id)
	if e != nil {
		return result, e
	}
	cwd := b.p.SessionDir(id)
	if cwd != "" {
		if !strings.HasPrefix(cwd, "/") || strings.ContainsRune(cwd, 0) {
			return result, errors.New("invalid cwd")
		}
		command = "cd '" + strings.ReplaceAll(cwd, "'", "'\"'\"'") + "' && " + command
	}
	result.ActualCWD = cwd
	out, stderr := &boundedOutput{limit: 512 << 10}, &boundedOutput{limit: 512 << 10}
	e = session.Run(ctx, command, nil, out, stderr)
	result.Output = out.String()
	result.Stderr = stderr.String()
	var exit *ssh.ExitError
	if errors.As(e, &exit) {
		result.ExitCode = exit.ExitStatus()
		return result, nil
	}
	return result, e
}

type boundedOutput struct {
	limit int
	data  []byte
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	if len(p) > b.limit-len(b.data) {
		return 0, errors.New("output limit")
	}
	b.data = append(b.data, p...)
	return len(p), nil
}
func (b *boundedOutput) String() string              { return string(b.data) }
func (b agentBackend) Control(id, data string) error { return b.p.Send(id, data) }
func (b agentBackend) Focus(id string) error {
	if b.p.Exited(id) {
		return errors.New("session closed")
	}
	b.p.emit(appui.Event{FocusID: id})
	return nil
}
func (b agentBackend) Open(ctx context.Context, id string) (string, error) {
	connection, e := b.p.store.Connection(id)
	if e != nil || connection.Deleted {
		return "", errors.New("not available")
	}
	if e = b.p.connectContext(ctx, connection, ""); e != nil {
		return "", e
	}
	for _, s := range b.Sessions() {
		if s.ConnectionID == id && s.Connected {
			return s.ID, nil
		}
	}
	return "", errors.New("connection did not open")
}
