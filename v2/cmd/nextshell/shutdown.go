package main

import (
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// shutdown ends the process in an orderly way. A quit the user asks for (Cmd+Q, closing the window)
// still goes through the confirmation; one the system asks for does not: SIGTERM/SIGINT (a service
// manager, a logout that stops the user's processes, Ctrl+C in a terminal) and the end of the OS
// session cannot wait for an answer nobody will give.
type shutdown struct {
	terminating atomic.Bool
	once        sync.Once
	cleanup     func()
}

// Terminating is true once the system asked the process to end; window close handlers let go then.
func (s *shutdown) Terminating() bool { return s.terminating.Load() }

// Finish closes sessions, removes mcp.json and flushes state, once, whichever path gets here first.
func (s *shutdown) Finish() { s.once.Do(s.cleanup) }

// notify is signal.Notify for the signals that mean "end now".
func notify() <-chan os.Signal {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	return ch
}

// Watch waits for the first signal, marks the shutdown as the system's and asks the app to quit
// (quit runs on the UI thread, where the window closes without a dialog). If the process is still
// here after grace, because the UI thread is stuck, it finishes the cleanup itself and exits.
// A second signal is left to the framework, which exits at once.
func (s *shutdown) Watch(sigs <-chan os.Signal, quit func(), grace time.Duration, exit func(int)) {
	go func() {
		sig := <-sigs
		s.terminating.Store(true)
		quit()
		time.AfterFunc(grace, func() {
			s.Finish()
			code := 0
			if n, ok := sig.(syscall.Signal); ok {
				code = 128 + int(n)
			}
			exit(code)
		})
	}()
}
