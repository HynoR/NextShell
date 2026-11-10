package main

import (
	"fmt"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
	"os"
	"time"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "terminal" {
		t, e := terminal.New(terminal.Options{Command: []string{"/bin/sh", "-c", "printf '\033]7;file://localhost/tmp\007probe-ok\n'"}})
		if e != nil {
			panic(e)
		}
		defer t.Close()
		<-t.Done()
		fmt.Printf("dir=%q text=%q\n", t.Dir(), t.Text())
		return
	}
	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{Title: "NextShell phase0 probe", Content: ui.View(func(c *ui.Context) { ui.Text(c, "NextShell native probe") })})
		time.AfterFunc(3*time.Second, func() { mygo.App.Quit() })
	})
	if e := mygo.App.Run(); e != nil {
		panic(e)
	}
	fmt.Println("native window lifecycle OK")
}
