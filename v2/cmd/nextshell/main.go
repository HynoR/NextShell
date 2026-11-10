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
	mygo.App.WhenReady(func() {
		w := mygo.NewWindow(mygo.WindowOptions{Title: "NextShell 2 · Native", Width: 1280, Height: 850, Content: ui.View(a.View)})
		w.OnClosed(func() { mygo.App.Quit() })
		if *smoke {
			p.Local()
			time.AfterFunc(4*time.Second, func() { mygo.App.Quit() })
		}
	})
	if e = mygo.App.Run(); e != nil {
		log.Fatal(e)
	}
}
