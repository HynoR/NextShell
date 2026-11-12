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

type options struct {
	db        string
	smoke     bool
	agentTest bool // only registered in nextshell_devtools builds
}

func parseFlags(fs *flag.FlagSet, args []string) (options, error) {
	var o options
	fs.StringVar(&o.db, "db", "", "v2 profile database (never point at a 1.x database)")
	fs.BoolVar(&o.smoke, "smoke", false, "open a native local terminal, then exit after 4 seconds")
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
	mygo.App.WhenReady(func() {
		w := mygo.NewWindow(mygo.WindowOptions{Title: "NextShell 2 · Native", Width: 1280, Height: 850, MinWidth: 1000, MinHeight: 650, TitleBarStyle: mygo.TitleBarHidden, Page: mygo.PageOptions{DevTools: devToolsMode()}, Content: ui.View(a.View)})
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
