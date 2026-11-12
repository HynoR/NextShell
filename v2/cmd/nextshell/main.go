package main

import (
	"flag"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"log"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/desktop"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type options struct {
	db        string
	smoke     bool
	agentTest bool // only registered in nextshell_devtools builds
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
	if e = startDevMode(p, opts, os.Stderr); e != nil {
		log.Fatal(e)
	}
	a := appui.New(p)
	p.SetQuit(func() { mygo.App.Quit() })
	var settings *mygo.Window
	a.ShowSettings = func() {
		if settings != nil {
			settings.Show()
			settings.Focus()
			return
		}
		settings = mygo.NewWindow(mygo.WindowOptions{Title: "设置", Width: 880, Height: 640, MinWidth: 680, MinHeight: 440, StateKey: "settings", Content: ui.View(a.SettingsView)})
		settings.OnClosed(func() { settings = nil })
	}
	a.PickFiles = func(o appui.FilePick, done func([]string)) {
		if rest, ok := strings.CutPrefix(o.DefaultPath, "~/"); ok {
			if home, e := os.UserHomeDir(); e == nil {
				o.DefaultPath = filepath.Join(home, rest)
			}
		}
		parent := mygo.FocusedWindow()
		go func() {
			var paths []string
			if o.Save {
				if path, e := mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: parent, Title: o.Title, DefaultPath: o.DefaultPath, CreateDirectories: true, ShowHiddenFiles: o.Hidden}); e == nil && path != "" {
					paths = []string{path}
				}
			} else {
				paths, _ = mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: parent, Title: o.Title, DefaultPath: o.DefaultPath, Directory: o.Directory, Multiple: o.Multiple, CreateDirectories: o.Directory, ShowHiddenFiles: o.Hidden})
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
		editor = mygo.NewWindow(mygo.WindowOptions{Title: "远程编辑器", Width: 960, Height: 700, MinWidth: 560, MinHeight: 360, StateKey: "editor", Content: ui.View(a.EditorView)})
		editor.OnClose(func(e *mygo.CloseEvent) {
			if !a.EditorMayClose() {
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
	mygo.App.WhenReady(func() {
		w := mygo.NewWindow(mygo.WindowOptions{Title: "NextShell", Width: 1280, Height: 820, MinWidth: 900, MinHeight: 560, TitleBarStyle: mygo.TitleBarHidden, TrafficLightPosition: &mygo.Point{X: 14, Y: 13}, Page: mygo.PageOptions{DevTools: devToolsMode()}, Content: ui.View(a.View)})
		w.OnClose(func(e *mygo.CloseEvent) {
			if (len(a.Tabs) > 0 || a.DirtyDocumentCount() > 0) && !a.ExitConfirmed {
				e.PreventDefault()
				a.ConfirmExit = true
				p.Invalidate()
			}
		})
		w.OnClosed(func() { mygo.App.Quit() })
		if opts.smoke {
			p.Local()
			a.ShowSettings() // the smoke run also opens a second native window
			time.AfterFunc(4*time.Second, func() { mygo.RunOnMain(func() { a.ExitConfirmed = true; mygo.App.Quit() }) })
		}
	})
	if e = mygo.App.Run(); e != nil {
		log.Fatal(e)
	}
}

func devToolsMode() mygo.DevTools {
	if developmentInspector && os.Getenv("NEXTSHELL_DEVTOOLS") == "1" {
		return mygo.DevToolsEnabled
	}
	return mygo.DevToolsDisabled
}
