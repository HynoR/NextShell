// Package appui is native presentation only. All system interaction is injected.
package appui

import (
	"github.com/egoist/mygo/plugins/terminal"
	"nextshell.dev/v2/internal/agent"
	"nextshell.dev/v2/internal/cloudsync"
	"nextshell.dev/v2/internal/model"
	"nextshell.dev/v2/internal/monitor"
	"nextshell.dev/v2/internal/remoteedit"
	"nextshell.dev/v2/internal/sftpfs"
	"nextshell.dev/v2/internal/storage"
	"nextshell.dev/v2/internal/transfer"
	"time"
)

type Session struct {
	ManualTitle bool
	Replaces    string
	ID, Title   string
	Terminal    *terminal.Terminal
	Connection  model.Connection
	Local       bool
	ByAgent     bool // an MCP client opened it: it joins the strip in the background, with a badge
}

// AgentNotice reports an agent call that changes something (Pending: it waits for the user's decision).
type AgentNotice struct {
	Tool    string
	Pending bool
}

// Prompt asks the user something during a connection. Kind selects the dialog:
// "" (server questions), PromptPassword, PromptHostKey or PromptHostKeyChanged.
// A nil answer cancels; Remember is set before answering and read after.
type Prompt struct {
	Kind, Host                  string
	Fingerprint, OldFingerprint string
	CanRemember, Remember       bool
	ByAgent                     bool // an agent's session_open caused it: the dialog says so
	Title                       string
	Questions                   []string
	Echo                        []bool
	Answers                     chan []string
	Done                        <-chan struct{}
}

const (
	PromptPassword       = "password"
	PromptHostKey        = "host-key"
	PromptHostKeyChanged = "host-key-changed"
)

type DiagnosticResult struct{ ID, Kind, Text string }
type Event struct {
	Link                 string
	Diagnostic           *DiagnosticResult
	FocusID, WorkspaceID string
	Handover             bool         // FocusID came from an agent's session_focus: select it and, rarely, raise the window
	CloseID              string       // an agent closed this session: drop its tab
	Agent                *AgentNotice // an agent call changed (or wants to change) something
	Files                *FileListing
	Editor               *EditorResult
	Monitor              *MonitorResult
	Import               *ImportPreview
	ShellMark            *PromptEvent // reserved: no producer while OSC 133 waits on upstream
	ShellHook            *ShellHookResult
	Session              *Session
	Failed               string // ID of the saved connection whose connect failed
	Message              string
	ForEditor            bool // Message answers an action of the remote editor, so it belongs in that window
	Prompt               *Prompt
}

// ShellHookResult says why the platform's automatic shell integration left a session alone
// (it has nothing to report when it types the line). The platform does the work itself: see
// Preferences.ShellIntegration.
type ShellHookResult struct {
	Session string
	Err     error
}
type TerminalCapabilities struct{ OSC7, ScreenText, OSC133 bool }

// PromptEvent is a reserved seam: no producer until mygo exposes upstream OSC 133 events.
// TODO(upstream-mygo): connect plugin events without a custom stream observer.
type PromptEvent struct {
	SessionID, Mark string
	ExitCode        *int
}
type Snapshot struct {
	Commands    []model.Command
	Connections []model.Connection
	Folders     []model.Folder
	Keys        []model.Key
	Proxies     []model.Proxy
	History     []string
}
type Platform interface {
	Snapshot() (Snapshot, error)
	Preferences() model.Preferences
	SavePreferences(model.Preferences) error
	SaveConnection(model.Connection, *string) error
	SaveFolder(model.Folder) error
	SaveKey(model.Key) error
	SaveProxy(model.Proxy, *string) error
	Delete(kind, id string) error
	NewID() string
	DuplicateConnection(id string) (model.Connection, error)
	RenameKey(id, name string) error
	SaveHistory([]string) error
	HostKeys() ([]model.HostKey, error)
	ForgetHostKey(address string) error
	UIState(key string) string
	SaveUIState(key, value string) error
	Capabilities() TerminalCapabilities
	GenerateKey(name, algorithm string) (model.Key, error)
	ImportKey(path, passphrase string) (model.Key, error)
	Connect(model.Connection)
	Local()
	Reconnect(*Session)
	CloneSession(*Session)
	CloseSession(id string)
	Send(id, text string) error
	ScreenText(id string) string
	SessionDir(id string) string
	Exited(id string) bool
	Poll() []Event
	SetWake(func())
	SetClipboard(func(string))
	CopyText(string)
	SetURLopener(func(string))
	OpenURL(string) error
	OpenFolder(dir string) error
	Quit()
	ListFiles(session, path string)
	FileAction(session, action, from, to string)
	QueueTransfer(TransferRequest) (string, error)
	Transfers() []transfer.Job
	CancelTransfer(id string)
	CancelAllTransfers()
	ClearCompletedTransfers() int
	RetryTransfer(id string) error
	OpenRemote(session, path string)
	// ReloadRemote is OpenRemote for the editor's reload button: the same Editor event,
	// and a failure's Message is ForEditor.
	ReloadRemote(session, path string)
	SaveRemote(session string, doc remoteedit.Document, text string)
	ExternalEdit(session, path, executable string, args []string)
	Diagnose(kind, target string) string
	CancelDiagnostic()
	RefreshMonitor(session string)
	WatchMonitor(session string, on bool)
	// WatchMonitorDetail: while on, samples for that session use the full probe (processes + network connections); otherwise the light probe.
	WatchMonitorDetail(session string, on bool)
	SignalProcess(session string, pid int, signal string)
	SaveCommand(model.Command) error
	ExecuteCommand(session, text string, all bool, appendCR ...bool) error
	PreviewImport(path, password, format string)
	CommitImport(token, folder, policy string)
	ExportConnections(path string, password *string, ids []string)
	CloudOverview() CloudOverview
	CloudConnect(endpoint, id, name, auth, encryption string, join bool)
	CloudSync(id string, useRemote bool)
	CloudBackground(id string, on bool)
	CloudCopy(id string, ids []string) error
	AgentOverview() AgentOverview
	AgentEndpoint(on bool) error
	AgentToken(rotate bool) (string, error)
	AgentHalt(bool)
	AgentRespond(id string, approve bool)
	AgentBlacklist(string)
	// AgentAutoApprove skips the approval prompt for mutating agent calls: d>0 for that long,
	// agent.UntilExit until the app exits, 0 off. Never persisted; errors while MCP is halted/off.
	// Risky calls (sensitive files, dangerous or blacklisted commands) still need manual approval.
	AgentAutoApprove(d time.Duration) error
	// AgentAutoApproval reports on/off and the expiry for a status indicator. Halting turns it off.
	AgentAutoApproval() agent.AutoApproval
	// MCPConfigPath is the mcp.json discovery file (port + token) written while MCP is enabled.
	MCPConfigPath() (string, error)
	TerminalTitle(string) string
	ShellCommand(shell string) (string, error)
	Shutdown()
}

type FileListing struct {
	Session, Path string
	Entries       []sftpfs.Entry
}
type EditorResult struct {
	Saved    bool
	Session  string
	Document remoteedit.Document
}
type MonitorResult struct {
	Session  string
	Snapshot monitor.Snapshot
	Latency  time.Duration // the round trip to the server; 0 when unknown
}
type ImportPreview struct {
	Targets                                     []string
	Warnings                                    []string
	Token                                       string
	Count, PasswordsUnavailable, KeysNeedRebind int
	Names                                       []string
}
type TransferRequest struct {
	Session, TargetSession, Kind, Local, Remote, Destination string
	Overwrite                                                bool
}

type CloudOverview struct {
	Workspaces []storage.Workspace
	Statuses   []cloudsync.Status
}
type AgentOverview struct {
	URL      string
	Halted   bool
	Pending  []agent.Pending
	Activity []agent.Activity
}
