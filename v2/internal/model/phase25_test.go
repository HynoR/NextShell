package model

import (
	"testing"
)

func TestKeepaliveWireTriStateResolution(t *testing.T) {
	yes, no := true, false
	interval := 45
	zero := 0
	for _, test := range []struct {
		name string
		c    Connection
		want int
	}{{"inherit", Connection{}, 30}, {"legacy-explicit", Connection{Keepalive: 10}, 10}, {"disabled", Connection{KeepaliveEnabled: &no, KeepaliveIntervalSec: &interval}, 0}, {"enabled-default", Connection{KeepaliveEnabled: &yes}, 30}, {"inherit-enabled-override-interval", Connection{KeepaliveIntervalSec: &interval}, 45}, {"enabled-interval", Connection{KeepaliveEnabled: &yes, KeepaliveIntervalSec: &interval}, 45}} {
		t.Run(test.name, func(t *testing.T) {
			if got := test.c.EffectiveKeepalive(); got != test.want {
				t.Fatal(got)
			}
		})
	}
	c := Connection{Name: "n", Host: "h", Port: 22, User: "u", Auth: "password", KeepaliveEnabled: &no, KeepaliveIntervalSec: &zero}
	if e := c.Validate(); e != nil {
		t.Fatal("explicit disabled zero interval rejected", e)
	}
	c.KeepaliveEnabled = &yes
	if e := c.Validate(); e == nil {
		t.Fatal("enabled zero interval accepted")
	}
}
