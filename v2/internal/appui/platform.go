// Package appui is native presentation only. All system interaction is injected.
package appui

import (
	"github.com/egoist/mygo/plugins/terminal"
	"nextshell.dev/v2/internal/model"
)

type Session struct {
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
}
type Event struct {
	Session *Session
	Message string
	Prompt  *Prompt
}
type Snapshot struct {
	Connections []model.Connection
	Folders     []model.Folder
	Keys        []model.Key
	Proxies     []model.Proxy
}
type Platform interface {
	Snapshot() (Snapshot, error)
	SaveConnection(model.Connection, *string) error
	SaveFolder(model.Folder) error
	SaveKey(model.Key) error
	SaveProxy(model.Proxy, *string) error
	Delete(kind, id string) error
	NewID() string
	GenerateKey(name string) (model.Key, error)
	ImportKey(path, passphrase string) (model.Key, error)
	Connect(model.Connection)
	Local()
	CloseSession(id string)
	Poll() []Event
	SetWake(func())
	ReadLegacyDeviceKey() (string, error)
	Shutdown()
}
