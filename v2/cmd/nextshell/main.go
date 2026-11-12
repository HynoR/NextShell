package main

import (
	"flag"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"log"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/desktop"
	"os"
	"runtime"
	"time"
)

type options struct {
	db         string
	smoke      bool
	agentTest  bool   // only registered in nextshell_devtools builds
	importFile string // likewise: --import with --agent-test

	trustNewHosts bool // likewise: --trust-new-hosts with --agent-test
}

func parseFlags(fs *flag.FlagSet, args []string) (options, error) {
	var o options
	fs.StringVar(&o.db, "db", "", "v2 profile database (never point at a 1.x database)")
	fs.BoolVar(&o.smoke, "smoke", false, "open a local terminal and the settings window, then exit after 4 seconds")
	registerDevFlags(fs, &o)
	if e := fs.Parse(args); e != nil {
		return o, e
	}
	return o, checkDevFlags(o)
}

func main() {
	opts, e := parseFlags(flag.CommandLine, os.Args[1:])
	if e != nil {
		log.Fatal(e)
	}
	path := opts.db
	if path == "" {
		path, e = desktop.DefaultPath()
		if e != nil {
			log.Fatal(e)
		}
	}
	p, e := desktop.Open(path)
	if e != nil {
		log.Fatal(e)
	}
	defer p.Shutdown()
	// With a tray option on the window can hide where no icon shows (a Linux desktop without a tray host):
	// launching the app again then brings it back. A trial profile (--db) runs beside the real one.
	tr := &tray{create: newTrayIcon, show: func() {}}
	if wantsSingleInstance(p.Preferences(), opts) {
		mygo.App.OnSecondInstance(tr.secondInstance(mygo.RunOnMain))
		if !mygo.App.RequestSingleInstanceLock() {
			return // handed over to the running instance, which shows its window
		}
	}
	if e = startDevMode(p, opts, os.Stderr); e != nil {
		log.Fatal(e)
	}
	a := appui.New(p)
	a.Cloud = cloudUI
	a.ProfilePath = path
	if !mygo.IsDev() { // go run, go build and mygo dev builds are 开发版
		a.Version = mygo.App.Version()
	}
	end := &shutdown{cleanup: func() {
		if !opts.smoke {
			a.SaveOpenTabs() // before the store closes
		}
		p.Shutdown()
	}}
	p.SetQuit(func() { mygo.App.Quit() })
	var settings *mygo.Window
	a.ShowSettings = func() {
		if settings != nil {
			settings.Show()
			settings.Focus()
			return
		}
		settings = mygo.NewWindow(mygo.WindowOptions{Title: "设置", Width: 880, Height: 640, MinWidth: 680, MinHeight: 440, StateKey: "settings", Content: ui.View(closeOnCmdW(a.SettingsView, func() {
			if settings != nil {
				settings.Close()
			}
		}))})
		settings.OnClosed(func() { settings = nil })
	}
	var manager *mygo.Window
	a.ShowManager = func() {
		if manager != nil {
			manager.Show()
			manager.Focus()
			return
		}
		manager = mygo.NewWindow(mygo.WindowOptions{Title: "连接管理器", Width: 960, Height: 640, MinWidth: appui.ManagerMinWidth, MinHeight: 420, StateKey: "manager", Content: ui.View(closeOnCmdW(a.ManagerView, func() {
			if manager != nil {
				manager.Close()
			}
		}))})
		manager.OnClose(func(e *mygo.CloseEvent) {
			if !end.Terminating() && !a.ManagerMayClose() {
				e.PreventDefault()
			}
		})
		manager.OnClosed(func() { manager = nil })
	}
	// HideManager runs inside the manager's own frame: close it after that frame.
	a.HideManager = func() {
		go mygo.RunOnMain(func() {
			if manager != nil {
				manager.Close()
			}
		})
	}
	a.PickFiles = func(o appui.FilePick, done func([]string)) {
		if dir, e := desktop.LocalPath(o.DefaultPath); e == nil {
			o.DefaultPath = dir
		}
		parent := mygo.FocusedWindow()
		go func() {
			var paths []string
			if o.Save {
				if path, e := mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: parent, Title: o.Title, DefaultPath: o.DefaultPath, CreateDirectories: true, ShowHiddenFiles: o.Hidden}); e == nil && path != "" {
					paths = []string{path}
				}
			} else {
				var filters []mygo.FileFilter
				if o.Images {
					filters = []mygo.FileFilter{{Name: "图片", Extensions: []string{"png", "jpg", "jpeg", "gif", "webp", "bmp"}}}
				}
				paths, _ = mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: parent, Title: o.Title, DefaultPath: o.DefaultPath, Directory: o.Directory, Multiple: o.Multiple, CreateDirectories: o.Directory, ShowHiddenFiles: o.Hidden, Filters: filters})
			}
			mygo.RunOnMain(func() { done(paths); p.Invalidate() })
		}()
	}
	var editor *mygo.Window
	a.ShowEditor = func() {
		if editor != nil {
			editor.Show()
			editor.Focus()
			return
		}
		editor = mygo.NewWindow(mygo.WindowOptions{Title: "远程编辑器", Width: 960, Height: 700, MinWidth: 560, MinHeight: 360, StateKey: "editor", Content: ui.View(closeOnCmdW(a.EditorView, func() {
			if editor != nil {
				editor.Close()
			}
		}))})
		editor.OnClose(func(e *mygo.CloseEvent) {
			if !end.Terminating() && !a.EditorMayClose() {
				e.PreventDefault()
				p.Invalidate()
			}
		})
		editor.OnClosed(func() { editor = nil })
	}
	// HideEditor runs inside the editor's own frame: close it after that frame.
	a.HideEditor = func() {
		go mygo.RunOnMain(func() {
			if editor != nil {
				editor.Close()
			}
		})
	}
	// mygo turns SIGINT/SIGTERM into App.Quit, which asks for confirmation when sessions are open and,
	// answered by nobody, never exits. This process hears the signal too, and its own quit does not ask.
	end.Watch(notify(), func() {
		mygo.RunOnMain(func() {
			a.ExitConfirmed, a.ConfirmExit = true, false
			mygo.App.Quit()
		})
	}, 10*time.Second, os.Exit)
	tr.app, tr.terminating = a, end.Terminating
	a.PreferencesChanged = tr.Sync
	// Every way of quitting closes the windows one by one: until the quit is cancelled, the main window's
	// closing is part of it, and must not hide the window into the tray.
	mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) {
		tr.quitting.Store(true)
		if e.SessionEnding { // logout, restart, shutdown: cannot be cancelled and cannot wait for a dialog
			end.terminating.Store(true)
		}
	})
	// OnQuit is the last hook the OS session's end leaves time for; Run may never return then.
	mygo.App.OnQuit(end.Finish)
	mygo.App.OnQuitCanceled(func() { tr.quitting.Store(false) })
	mygo.App.WhenReady(func() {
		a.TrayAvailable = true
		tr.Probe(runtime.GOOS)
		mygo.App.SetMenu(appMenu(func() { a.ShowSettings() }))
		w := mygo.NewWindow(mygo.WindowOptions{Title: "NextShell", Width: 1280, Height: 820, MinWidth: 900, MinHeight: 560, TitleBarStyle: mygo.TitleBarHidden, TrafficLightPosition: &mygo.Point{X: 14, Y: 13}, Page: mygo.PageOptions{DevTools: devToolsMode()}, Content: ui.View(a.View)})
		// An agent never takes the keyboard: the first action shows the window without focus (and bounces
		// the Dock icon); only session_focus, the hand-over, focuses it.
		a.RaiseWindow = func(focus bool) {
			if focus {
				w.Show()
				w.Focus()
				return
			}
			w.ShowInactive()
			if runtime.GOOS == "darwin" {
				go mygo.App.Dock.Bounce(false)
			}
		}
		tr.show = func() { bringForward(w) }
		w.OnClose(func(e *mygo.CloseEvent) {
			if tr.closeHides() {
				e.PreventDefault()
				go mygo.RunOnMain(w.Hide)
				return
			}
			if !end.Terminating() && a.ExitNeedsConfirm() {
				e.PreventDefault()
				a.ConfirmExit = true
				go mygo.RunOnMain(func() { bringForward(w) })
				p.Invalidate()
			}
		})
		w.OnClosed(func() { mygo.App.Quit() })
		w.OnMinimize(func() {
			if tr.Hides(a.Prefs.MinimizeToTray) {
				go mygo.RunOnMain(w.Hide)
			}
		})
		// A system whose panel shows the icon's own menu is told what it lists when the window goes away.
		w.OnHide(tr.Refresh)
		w.OnBlur(tr.Refresh)
		// The Dock icon of a window hidden into the tray brings it back.
		mygo.App.OnActivate(func(visible bool) {
			if !visible && !w.IsVisible() && tr.icon != nil {
				bringForward(w)
			}
		})
		tr.Sync()
		if opts.smoke {
			p.Local()
			a.ShowSettings() // the smoke run also opens a second native window
			time.AfterFunc(4*time.Second, func() { mygo.RunOnMain(func() { a.ExitConfirmed = true; mygo.App.Quit() }) })
		}
	})
	e = mygo.App.Run()
	end.Finish()
	if e != nil {
		log.Fatal(e)
	}
}

func devToolsMode() mygo.DevTools {
	if developmentInspector && os.Getenv("NEXTSHELL_DEVTOOLS") == "1" {
		return mygo.DevToolsEnabled
	}
	return mygo.DevToolsDisabled
}
