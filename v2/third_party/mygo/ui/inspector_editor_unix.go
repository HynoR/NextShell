//go:build !mygo_noinspector && unix && !ios && !android

package ui

import "syscall"

// editorProcAttr starts an editor in a session of its own, out of the
// process group mygo dev stops the app with.
func editorProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
