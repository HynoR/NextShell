package main

import (
	"flag"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"log"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/desktop"
	"time"
)

func main() {
	db := flag.String("db", "", "v2 profile database (never point at a 1.x database)")
	smoke := flag.Bool("smoke", false, "open a native local terminal, then exit after 4 seconds")
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
	mygo.App.WhenReady(func() {
		w := mygo.NewWindow(mygo.WindowOptions{Title: "NextShell 2 · Native", Width: 1280, Height: 850, MinWidth: 1000, MinHeight: 650, TitleBarStyle: mygo.TitleBarHidden, Content: ui.View(a.View)})
		w.OnClose(func(e *mygo.CloseEvent) {
			if len(a.Tabs) > 0 && !a.ExitConfirmed {
				e.PreventDefault()
				a.ConfirmExit = true
				p.Invalidate()
			}
		})
		w.OnClosed(func() { mygo.App.Quit() })
		if *smoke {
			p.Local()
			time.AfterFunc(4*time.Second, func() { mygo.RunOnMain(func() { a.ExitConfirmed = true; mygo.App.Quit() }) })
		}
	})
	if e = mygo.App.Run(); e != nil {
		log.Fatal(e)
	}
}
