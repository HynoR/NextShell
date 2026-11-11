//go:build windows && (amd64 || arm64)

package mygo

import (
	"github.com/egoist/mygo/internal/platform"
	win "github.com/egoist/mygo/internal/windows"
)

func newBackend() platform.Backend { return win.New() }

func linkContentRules() { win.LinkContentRules() }

func linkNowPlaying() { win.LinkNowPlaying() }

func linkNotificationImages() { win.LinkNotificationImages() }

func linkMenuIcons() { win.LinkMenuIcons() }

func linkContextMenus() { win.LinkContextMenus() }
