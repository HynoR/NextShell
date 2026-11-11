//go:build !mygo_noinspector

package automation

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/surface"
)

// init listens on the socket MYGO_AUTOMATION names, if any, before any
// window opens, so that the content of every window of native UI comes to
// the plugin as it attaches.
func init() {
	path := os.Getenv("MYGO_AUTOMATION")
	if path == "" {
		return
	}
	l, err := listen(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mygo: automation:", err)
		return
	}
	a := &app{ready: make(chan struct{}), targets: map[*mygo.Window]target{}}
	surface.Automate = a.attached
	mygo.App.WhenReady(func() { close(a.ready) })
	mygo.App.OnQuit(func() { l.Close() })
	go serve(l, a)
}

// app drives the running app's windows.
type app struct {
	// ready is closed once the app has launched: requests wait for it.
	ready chan struct{}
	// targets are the windows of native UI, by window. Main thread only.
	targets map[*mygo.Window]target
}

// target is a window of native UI: its connection to its content, and
// the content.
type target struct {
	conn    *surface.Conn
	content surface.Automated
}

// attached takes a window's content as it attaches (surface.Automate).
// Main thread only.
func (a *app) attached(conn *surface.Conn, content surface.Automated) {
	for w := range a.targets {
		if w.IsDestroyed() {
			delete(a.targets, w)
		}
	}
	if w, ok := conn.Window.(*mygo.Window); ok {
		a.targets[w] = target{conn, content}
		// A closed window's content goes, with what it showed (#1281).
		w.OnClosed(func() { delete(a.targets, w) })
	}
}

var errQuit = errors.New("the app quit")

// onMain runs fn on the main thread, once the app has launched.
func (a *app) onMain(fn func()) error {
	<-a.ready
	ran := false
	mygo.RunOnMain(func() {
		fn()
		ran = true
	})
	if !ran {
		return errQuit
	}
	return nil
}

// window returns the window of the ID, or for 0 the focused window, else
// the first.
func (a *app) window(id int) (*mygo.Window, error) {
	<-a.ready
	if id != 0 {
		if w := mygo.WindowByID(id); w != nil && !w.IsDestroyed() {
			return w, nil
		}
		return nil, errorf(codeNoWindow, "no window %d", id)
	}
	if w := mygo.FocusedWindow(); w != nil {
		return w, nil
	}
	if ws := mygo.Windows(); len(ws) > 0 {
		return ws[0], nil
	}
	return nil, errorf(codeNoWindow, "the app has no window")
}

// content runs fn on the main thread with the target of a window of
// native UI.
func (a *app) content(id int, fn func(w *mygo.Window, t target) error) error {
	w, err := a.window(id)
	if err != nil {
		return err
	}
	var ferr error
	if err := a.onMain(func() {
		t, ok := a.targets[w]
		switch {
		case w.IsDestroyed():
			ferr = errorf(codeNoWindow, "window %d closed", w.ID())
		case !ok:
			ferr = errorf(codeUnsupported, "window %d shows a web page: use eval", w.ID())
		default:
			ferr = fn(w, t)
		}
	}); err != nil {
		return err
	}
	return ferr
}

func (a *app) windows() ([]windowInfo, error) {
	var out []windowInfo
	err := a.onMain(func() {
		for _, w := range mygo.Windows() {
			if w.IsDestroyed() {
				continue
			}
			info := windowInfo{
				ID: w.ID(), Title: w.Title(), Kind: "page",
				Bounds: rectOf(w.Bounds()), ContentBounds: rectOf(w.ContentBounds()),
				Focused: w.IsFocused(), Visible: w.IsVisible(),
			}
			if _, ok := a.targets[w]; ok {
				info.Kind = "content"
			} else if p := w.Page(); p != nil {
				info.URL = p.URL()
			}
			out = append(out, info)
		}
	})
	return out, err
}

func rectOf(r mygo.Rectangle) rect {
	return rect{float64(r.X), float64(r.Y), float64(r.Width), float64(r.Height)}
}

func (a *app) tree(id int) (window int, tree *platform.AccessTree, keys map[uint64]any, err error) {
	err = a.content(id, func(w *mygo.Window, t target) error {
		window = w.ID()
		tree, keys = t.content.AutomationTree()
		return nil
	})
	return window, tree, keys, err
}

// act gives the content an action of assistive technology, as the
// backend gives it those of the system's.
func (a *app) act(id int, ev platform.SurfaceEvent) error {
	return a.content(id, func(w *mygo.Window, t target) error {
		tree, _ := t.content.AutomationTree()
		for _, n := range tree.Nodes {
			if n.ID == ev.ID {
				t.conn.Event(ev)
				return nil
			}
		}
		return errorf(codeNoElement, "window %d shows no element %d", w.ID(), ev.ID)
	})
}

// input gives the content events, as the backend gives it the system's.
func (a *app) input(id int, evs []platform.SurfaceEvent) error {
	return a.content(id, func(w *mygo.Window, t target) error {
		for _, ev := range evs {
			t.conn.Event(ev)
		}
		return nil
	})
}

func (a *app) menu(id int, path []string) error {
	w, err := a.window(id)
	if err != nil {
		return err
	}
	var merr error
	if err := a.onMain(func() { merr = chooseMenuItem(w, path) }); err != nil {
		return err
	}
	if merr != nil {
		var e *Error
		if !errors.As(merr, &e) {
			merr = errorf(codeFailed, "%v", merr)
		}
	}
	return merr
}

func (a *app) capture(id int) ([]byte, error) {
	w, err := a.window(id)
	if err != nil {
		return nil, err
	}
	b, err := w.CapturePage()
	if err != nil {
		return nil, errorf(codeFailed, "%v", err)
	}
	return b, nil
}

func (a *app) eval(id int, script string) (any, error) {
	w, err := a.window(id)
	if err != nil {
		return nil, err
	}
	p := w.Page()
	if p == nil {
		return nil, errorf(codeUnsupported, "window %d shows native UI, not a web page: use tree", w.ID())
	}
	v, err := p.Eval(script)
	if err != nil {
		return nil, errorf(codeFailed, "%v", err)
	}
	return v, nil
}

// pollEvery is how often idle asks whether the window settled.
const pollEvery = 5 * time.Millisecond

// idle returns once the window has drawn the frames it asked for: for a
// window of native UI, once its content asks for no frame (as
// ui.Tester.FramePending tells); for a page, once it has loaded and
// answered a script.
func (a *app) idle(id int, timeout time.Duration) error {
	w, err := a.window(id)
	if err != nil {
		return err
	}
	var deadline time.Time
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}
	for {
		pending := false
		err := a.content(id, func(_ *mygo.Window, t target) error {
			pending = t.content.FramePending()
			return nil
		})
		var e *Error
		if errors.As(err, &e) && e.Code == codeUnsupported {
			// A page.
			p := w.Page()
			if p == nil {
				return errorf(codeNoWindow, "window %d closed", w.ID())
			}
			pending = p.IsLoading()
			if !pending {
				if _, err := p.Eval("true"); err != nil {
					return errorf(codeFailed, "%v", err)
				}
			}
		} else if err != nil {
			return err
		}
		if !pending {
			return nil
		}
		if !deadline.IsZero() && time.Now().After(deadline) {
			return errorf(codeTimeout, "window %d still draws after %v", w.ID(), timeout)
		}
		time.Sleep(pollEvery)
	}
}
