//go:build !mygo_noinspector && (unix || windows) && !ios && !android

package ui

import "os/exec"

// editorsRun reports whether this system starts programs.
const editorsRun = true

// startEditor starts an editor without the app's terminal, which a program
// reading it would fight over with mygo dev, and on its own, so that it
// stays open as mygo dev restarts the app. done gets how it exited, on
// another goroutine.
func startEditor(name string, args []string, done func(error)) error {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = editorProcAttr()
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { done(cmd.Wait()) }()
	return nil
}
