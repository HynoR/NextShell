package appui

import (
	"nextshell.dev/v2/internal/model"
	"testing"
)

func TestConnectionDraftPreservesKeepaliveInheritanceAndDisable(t *testing.T) {
	a := &App{Edit: model.Connection{Name: "n", Host: "h", Port: 22, User: "u", Auth: "password"}, Port: "22", Keepalive: "45", InheritKeepalive: true}
	c, e := a.draft()
	if e != nil || c.KeepaliveEnabled != nil || c.Keepalive != 0 || c.KeepaliveIntervalSec == nil || *c.KeepaliveIntervalSec != 45 || c.EffectiveKeepalive() != 45 {
		t.Fatal(c, e)
	}
	a.InheritKeepalive = false
	a.Keepalive = "0"
	c, e = a.draft()
	if e != nil || c.KeepaliveEnabled == nil || *c.KeepaliveEnabled || c.EffectiveKeepalive() != 0 {
		t.Fatal(c, e)
	}
}
