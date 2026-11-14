package appui

import (
	"bytes"
	"image/color"
	"image/png"
)

// TrayIcon is the tray icon, a PNG: the terminal mark in black on transparent, which the system tints to
// match its bar (a template icon). 36 pixels is 18 points on a Retina menu bar.
func TrayIcon() []byte {
	var b bytes.Buffer
	if err := png.Encode(&b, appIcons["terminal"].Image(36, 36, color.Black)); err != nil {
		return nil // an icon that cannot be encoded leaves the tray without one
	}
	return b.Bytes()
}

// What the tray icon's menu does in the app. The host builds the menu and shows the window first; these
// run on the UI thread and leave the rest to the next frame, which a hidden window gets once it is shown.

// TrayHost is a host the tray menu offers.
type TrayHost struct{ ID, Name, Login string }

// trayHostCount is how many hosts the tray menu lists.
const trayHostCount = 8

// TrayHosts are the hosts the tray menu offers: the ones connected to lately, then favorites, as the 「+」 menu.
func (a *App) TrayHosts() []TrayHost {
	var out []TrayHost
	for _, cc := range a.hosts.recentHosts(a, trayHostCount) {
		out = append(out, TrayHost{ID: cc.ID, Name: cc.Name, Login: a.login(cc)})
	}
	return out
}

// TrayPending says that a tray option was turned on but cannot work in this run (the host could not take the
// lock that makes a hidden window findable): the window stays visible, the settings say the option waits for a
// restart, and the first time a toast does too. Off clears it.
func (a *App) TrayPending(on bool) {
	s := &a.settings
	s.trayRestart = on
	if on && !s.trayTold {
		s.trayTold = true
		a.toast("托盘选项重启后生效")
	}
}

// TrayConnect connects to the host id, if it is still there.
func (a *App) TrayConnect(id string) {
	if cc, ok := a.host(id); ok && !cc.Deleted {
		a.later(func() { a.connect(cc) })
		a.wake()
	}
}

// TrayLocal opens a local terminal.
func (a *App) TrayLocal() {
	a.later(a.P.Local)
	a.wake()
}

// TrayQuit quits as ⌘Q does: with the confirmation when sessions are open or files are unsaved, which the
// window shows. The host has brought the window forward by then.
func (a *App) TrayQuit() { a.RequestQuit() }

// RequestQuit is every quit the user asks for (the tray's 退出, Ctrl+Q / Ctrl+Shift+Q outside macOS): it asks
// first when sessions are open or files are unsaved, as closing the window does.
func (a *App) RequestQuit() {
	if a.ExitNeedsConfirm() {
		a.ConfirmExit = true
		a.wake()
		return
	}
	a.P.Quit()
}

// wake draws the main window again, for a change made outside a frame.
func (a *App) wake() {
	if a.wakeMain != nil {
		a.wakeMain()
	}
}
