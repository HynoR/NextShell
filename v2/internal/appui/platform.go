// Package appui is native presentation only. All system interaction is injected.
package appui

import (
	"github.com/egoist/mygo/plugins/terminal"
	"nextshell.dev/v2/internal/model"
)

type Session struct {
	Replaces   string
	ID, Title  string
	Terminal   *terminal.Terminal
	Connection model.Connection
	Local      bool
}
type Prompt struct {
	Title     string
	Questions []string
	Echo      []bool
	Answers   chan []string
	Done      <-chan struct{}
}
type Event struct {
	ShellMark *PromptEvent // reserved: no producer while OSC 133 waits on upstream
	Session   *Session
	Message   string
	Prompt    *Prompt
}
type TerminalCapabilities struct{ OSC7, ScreenText, OSC133 bool }

// PromptEvent is a reserved seam: no producer until mygo exposes upstream OSC 133 events.
// TODO(upstream-mygo): connect plugin events without a custom stream observer.
type PromptEvent struct {
	SessionID, Mark string
	ExitCode        *int
}
type Snapshot struct {
	Connections []model.Connection
	Folders     []model.Folder
	Keys        []model.Key
	Proxies     []model.Proxy
	History     []string
}
type Platform interface {
	Snapshot() (Snapshot, error)
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
	Exited(id string) bool
	Poll() []Event
	SetWake(func())
	Quit()
	ReadLegacyDeviceKey() (string, error)
	Shutdown()
}
