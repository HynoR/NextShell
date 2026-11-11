//go:build darwin

package mygo

import (
	"github.com/egoist/mygo/internal/darwin"
	"github.com/egoist/mygo/internal/platform"
)

func newBackend() platform.Backend { return darwin.New() }

func enableNotificationSenders() { darwin.EnableNotificationSenders() }

func linkNotificationImages() { darwin.LinkNotificationImages() }

func linkContentRules() { darwin.LinkContentRules() }

func linkNowPlaying() { darwin.LinkNowPlaying() }

func linkMenuIcons() { darwin.LinkMenuIcons() }

func linkContextMenus() { darwin.LinkContextMenus() }
