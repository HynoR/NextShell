//go:build !mygo_noinspector

package ui

import "syscall"

// createNoWindow is CREATE_NO_WINDOW.
const createNoWindow = 0x08000000

// editorProcAttr starts an editor without a console, which an editor's
// launcher script (code.cmd) would otherwise flash.
func editorProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: createNoWindow}
}
