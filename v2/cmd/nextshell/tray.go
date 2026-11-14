package main

import (
	"runtime"
	"sync/atomic"

	"github.com/egoist/mygo"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/instance"
	"nextshell.dev/v2/internal/model"
)

// trayIcon is what the host asks of a tray icon; *mygo.Tray is one, and a test uses another.
type trayIcon interface {
	SetMenu(*mygo.Menu)
	PopUpMenu()
	OnClick(func()) func()
	OnRightClick(func()) func()
	Destroy()
}

// tray is the app's tray icon. It exists only while a preference needs it (the window hides into it when
// minimized or closed), and the window hides only while it exists, so a window never ends up without a way back.
type tray struct {
	app    *appui.App
	create func() (trayIcon, error) // makes the icon; fails where the system has no tray
	show   func()                   // brings the main window forward
	icon   trayIcon
	failed bool

	// shared is a run on a profile file, the real one or a --db one: it may be launched again, and only
	// while it holds that profile's single-instance lock does a second launch show its window instead of
	// opening the file again, so only then may it hide the window. lock takes the lock: mygo's
	// RequestSingleInstanceLock for the real profile, one keyed by the file for --db (profileLock).
	shared, locked bool
	lock           func() bool

	quitting    atomic.Bool // the app is quitting: closing the window then means quitting, never hiding
	terminating func() bool // the system is ending the process, which nothing may hold back
}

// newTrayIcon is the real icon: the terminal mark as a template icon, which the system tints to the bar.
func newTrayIcon() (trayIcon, error) {
	t, e := mygo.NewTray(mygo.TrayOptions{ID: "nextshell", Icon: appui.TrayIcon(), IconIsTemplate: true, ToolTip: "NextShell"})
	if e != nil {
		return nil, e
	}
	return t, nil
}

// menuIsStatic is whether the system draws the tray's menu itself, from the one set on the icon: the Linux
// panels do and report no right click. Elsewhere the menu is built when it is asked for, so it is current,
// and a click on the icon is free to bring the window forward.
var menuIsStatic = runtime.GOOS == "linux"

// Probe tells whether the system has a tray at all, where that can fail (Linux without an indicator
// library, other systems without a tray); macOS and Windows always have one. Call it once the app is ready.
func (t *tray) Probe(goos string) {
	if goos == "darwin" || goos == "windows" {
		return
	}
	if icon, e := t.create(); e != nil {
		t.failed = true
		t.app.TrayAvailable = false
	} else {
		icon.Destroy()
	}
}

// takeLock holds the single-instance lock where a run needs it to hide its window. A profile in memory
// cannot be opened again and needs none. mygo allows the call at any time and answers true again once the lock is
// held; it answers false when another instance of this app holds it, which no run can change.
func (t *tray) takeLock() bool {
	if !t.shared || t.locked {
		return true
	}
	t.locked = t.lock()
	return t.locked
}

// Sync makes the icon match the preferences: made when one of them needs it, removed when none does.
func (t *tray) Sync() {
	wanted := !t.failed && (t.app.Prefs.MinimizeToTray || t.app.Prefs.CloseToTray)
	switch {
	case wanted && t.icon == nil:
		// An option turned on in this run needs the lock too; without it the window stays visible, and the
		// settings say that the option works after a restart.
		if !t.takeLock() {
			t.app.TrayPending(true)
			return
		}
		icon, e := t.create()
		if e != nil {
			t.failed = true
			t.app.TrayAvailable = false
			return
		}
		t.app.TrayPending(false)
		t.icon = icon
		icon.OnClick(func() { t.show() })
		if !menuIsStatic {
			icon.OnRightClick(t.popUp)
		}
		t.Refresh()
	case !wanted && t.icon != nil:
		t.icon.Destroy()
		t.icon = nil
	}
	if !wanted {
		t.app.TrayPending(false)
	}
}

// wantsSingleInstance is whether this launch takes the single-instance lock: a tray option is on, so the
// window may be hidden, and the profile is a file that may be opened again. A --db profile has a lock of
// its own and runs beside the real one; where no tray icon shows (a Linux desktop without a tray host),
// launching it again is what brings its window back.
func wantsSingleInstance(prefs model.Preferences, o options) bool {
	return (prefs.MinimizeToTray || prefs.CloseToTray) && sharedProfile(o)
}

// sharedProfile is whether the run's profile may be opened by another launch: any file, not :memory:.
func sharedProfile(o options) bool { return o.db != ":memory:" }

// profileLock is the single-instance lock of the run's profile, and how it is let go: mygo's app-wide
// lock for the real profile, whose second launches reach OnSecondInstance; for a --db profile one keyed
// by its file, whose second launches call wake.
func profileLock(o options, wake func()) (take func() bool, release func()) {
	if o.db == "" {
		return mygo.App.RequestSingleInstanceLock, func() {}
	}
	l := instance.New(o.db, wake)
	return l.Take, l.Release
}

// secondInstance is what a later launch does to this one, which holds the lock: show the window as the tray's
// 显示窗口 does. The launch calls from another goroutine, so run puts it on the main thread.
func (t *tray) secondInstance(run func(func())) func([]string, string) {
	return func([]string, string) { run(func() { t.show() }) }
}

// Hides is whether the window may hide for the preference on: there is an icon to bring it back.
func (t *tray) Hides(on bool) bool { return on && t.icon != nil }

// closeHides is whether closing the main window hides it. A quit, ⌘Q or the tray's, goes through closing
// the windows, so it must not be taken for the user closing this one.
func (t *tray) closeHides() bool {
	return !t.quitting.Load() && !t.terminating() && t.Hides(t.app.Prefs.CloseToTray)
}

// Refresh puts the current menu on a system that shows the icon's own; the others build it per click.
func (t *tray) Refresh() {
	if t.icon != nil && menuIsStatic {
		t.icon.SetMenu(t.menu())
	}
}

// popUp shows the menu under the icon: attached for the moment the menu is open, then taken off, so
// that a click on the icon goes on reaching OnClick (an icon with a menu shows it for every click).
func (t *tray) popUp() {
	if t.icon == nil {
		return
	}
	t.icon.SetMenu(t.menu())
	t.icon.PopUpMenu()
	if t.icon != nil {
		t.icon.SetMenu(nil)
	}
}

// menu is 显示窗口, the recent hosts, 新建本地终端 and 退出. Each shows the window first: what it asks of the
// app, the connection, the terminal and the confirmation of a quit, is drawn there.
func (t *tray) menu() *mygo.Menu {
	item := func(label string, do func()) *mygo.MenuItem {
		return &mygo.MenuItem{Label: label, Click: func(*mygo.MenuItem, *mygo.Window) { t.show(); do() }}
	}
	items := []*mygo.MenuItem{item("显示窗口", func() {})}
	if hosts := t.app.TrayHosts(); len(hosts) > 0 {
		items = append(items, mygo.Separator(), &mygo.MenuItem{Label: "最近连接", Disabled: true})
		for _, h := range hosts {
			m := item(h.Name, func() { t.app.TrayConnect(h.ID) })
			m.Subtitle = h.Login
			items = append(items, m)
		}
	}
	items = append(items, mygo.Separator(), item("新建本地终端", t.app.TrayLocal), mygo.Separator(), item("退出", t.app.TrayQuit))
	return mygo.NewMenu(items)
}
