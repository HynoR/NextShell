// Package appui is native presentation only. All system interaction is injected.
package appui

import (
	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
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
	FilesFailed          *FileFailure // a listing that could not be read
	Removal              *RemovalCount
	Editor               *EditorResult
	Monitor              *MonitorResult
	Import               *ImportPreview
	ShellMark            *PromptEvent // reserved: no producer while OSC 133 waits on upstream
	ShellHook            *ShellHookResult
	Session              *Session
	Failed               string // ID of the saved connection whose connect failed
	Host                 string // the name of the connection whose connect failed, for the toast; set with Message
	AuthFailed           bool   // Failed was refused its login (or has no key): the user may fix it and retry
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
	Credentials []model.Credential
	History     []string
}
type Platform interface {
	Snapshot() (Snapshot, error)
	Preferences() model.Preferences
	SavePreferences(model.Preferences) error
	// ImportBackground copies the picture at src into the app's data folder and returns the copy's file name
	// and the picture; a refusal's message is meant for the user. BackgroundPicture reads a copy back, and
	// RemoveBackground deletes one that may already be gone.
	ImportBackground(src string) (string, *ui.Bitmap, error)
	BackgroundPicture(name string) (*ui.Bitmap, error)
	RemoveBackground(name string)
	SaveConnection(model.Connection, *string) error
	SaveFolder(model.Folder) error
	SaveKey(model.Key) error
	SaveProxy(model.Proxy, *string) error
	// SaveCredential saves a shared credential; a nil secret keeps its saved password.
	SaveCredential(model.Credential, *string) error
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
	// ImportKey and SaveKey fail with *model.KeyExistsError for a key already saved. A new key is bound
	// to the imported hosts waiting for it (by fingerprint or name), which a Message event reports.
	ImportKey(path, passphrase string) (model.Key, error)
	// ReplaceKey gives key id a new private key, read from path unless pem is set; it keeps its name.
	ReplaceKey(id, path, pem, passphrase string) error
	Connect(model.Connection)
	// CancelConnect stops the connects in progress to saved connection id, reporting nothing.
	CancelConnect(id string)
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
	// CountRemoval counts, as a Removal event, what deleting paths takes (folders with all inside); the
	// returned func gives it up. A "remove" transfer then deletes one path.
	CountRemoval(session string, paths []string) (cancel func())
	Transfers() []transfer.Job
	CancelTransfer(id string)
	CancelAllTransfers()
	ClearCompletedTransfers() int
	// ClearFailedTransfers drops the failed and cancelled jobs, which stay for a retry until then.
	ClearFailedTransfers() int
	RetryTransfer(id string) error
	// DismissTransfer drops one ended job from the list; a queued or running one stays.
	DismissTransfer(id string)
	OpenRemote(session, path string)
	// ReloadRemote is OpenRemote for the editor's reload button: the same Editor event,
	// and a failure's Message is ForEditor.
	ReloadRemote(session, path string)
	SaveRemote(session string, doc remoteedit.Document, text string)
	// SaveLocalCopy writes text to the local file dest: the editor keeps unsaved edits there when the server's
	// file changed under them. The server is not touched.
	SaveLocalCopy(dest, text string) error
	// ExternalEdit opens path in an outside editor (none: the system's text editor) and keeps every save
	// in sync until StopLiveEdit, whether or not the editor keeps running.
	ExternalEdit(session, path, executable string, args []string)
	LiveEdits() []remoteedit.Live
	StopLiveEdit(id string) // "" stops all
	// ResolveLiveEdit settles a conflict: overwrite replaces the server's file, otherwise the local copy is reloaded.
	ResolveLiveEdit(id string, overwrite bool)
	SaveLiveEditCopy(id, dest string) error
	Diagnose(kind, target string) string
	CancelDiagnostic()
	RefreshMonitor(session string)
	WatchMonitor(session string, on bool)
	// WatchMonitorDetail: while on, samples for that session use the full probe (processes + network connections); otherwise the light probe.
	WatchMonitorDetail(session string, on bool)
	SignalProcess(session string, pid int, signal string)
	SaveCommand(model.Command) error
	ExecuteCommand(session, text string, appendCR ...bool) error
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

// FileFailure is a directory listing that failed. Path is what was asked for; Err is the server's own wording.
type FileFailure struct{ Session, Path, Err string }

// RemovalCount answers Platform.CountRemoval: what deleting each path takes with it, or why that is unknown.
type RemovalCount struct {
	Session string
	Targets []RemovalTally
	Err     string
}
type RemovalTally struct {
	Path        string
	Files, Dirs int
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
	// Err is set when this sample failed (Snapshot is empty then). It is not a toast: sampling repeats every
	// 2 s, so the panels count the failures and say so in place. monitor.ErrUnsupported marks a server that
	// can never be read.
	Err error
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
	Items                                                    int // kind "remove": what was counted, for the progress
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
