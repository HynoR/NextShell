package main

import (
	"flag"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"log"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/desktop"
	"os"
	"time"
)

func main() {
	db := flag.String("db", "", "v2 profile database (never point at a 1.x database)")
	smoke := flag.Bool("smoke", false, "open a local terminal and the settings window, then exit after 4 seconds")
	flag.Parse()
	path := *db
	if path == "" {
		var e error
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
	mygo.App.WhenReady(func() {
		w := mygo.NewWindow(mygo.WindowOptions{Title: "NextShell", Width: 1280, Height: 820, MinWidth: 900, MinHeight: 560, TitleBarStyle: mygo.TitleBarHidden, Page: mygo.PageOptions{DevTools: devToolsMode()}, Content: ui.View(a.View)})
		w.OnClose(func(e *mygo.CloseEvent) {
			if (len(a.Tabs) > 0 || a.DirtyDocumentCount() > 0) && !a.ExitConfirmed {
				e.PreventDefault()
				a.ConfirmExit = true
				p.Invalidate()
			}
		})
		w.OnClosed(func() { mygo.App.Quit() })
		if *smoke {
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
