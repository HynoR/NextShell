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
)

type Session struct {
	ManualTitle bool
	Replaces    string
	ID, Title   string
	Terminal    *terminal.Terminal
	Connection  model.Connection
	Local       bool
}
type Prompt struct {
	Title     string
	Questions []string
	Echo      []bool
	Answers   chan []string
	Done      <-chan struct{}
}
type DiagnosticResult struct{ ID, Kind, Text string }
type Event struct {
	Diagnostic           *DiagnosticResult
	FocusID, WorkspaceID string
	Files                *FileListing
	Editor               *EditorResult
	Monitor              *MonitorResult
	Import               *ImportPreview
	ShellMark            *PromptEvent // reserved: no producer while OSC 133 waits on upstream
	Session              *Session
	Message              string
	Prompt               *Prompt
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
	SaveRemote(session string, doc remoteedit.Document, text string)
	ExternalEdit(session, path, executable string, args []string)
	Diagnose(kind, target string) string
	CancelDiagnostic()
	RefreshMonitor(session string)
	WatchMonitor(session string, on bool)
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
	TerminalTitle(string) string
	ShellCommand(shell string) (string, error)
	Shutdown()
}

type FileListing struct {
	Session, Path string
	Entries       []sftpfs.Entry
}
type EditorResult struct {
	Session  string
	Document remoteedit.Document
}
type MonitorResult struct {
	Session  string
	Snapshot monitor.Snapshot
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
