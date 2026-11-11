// Package automation lets another process drive a running app, as an
// end-to-end test runner or an agent does: list its windows, read what
// their native UI shows as elements, act on them, send pointer, wheel,
// key and text input, choose items of the application menu, evaluate
// scripts in pages, capture windows as PNG images, and wait for them to
// settle.
//
// An app opts in by importing the package, as a file built for tests
// does:
//
//	//go:build e2e
//
//	package main
//
//	import _ "github.com/egoist/mygo/plugins/automation"
//
// Then it listens only when the environment variable MYGO_AUTOMATION names
// the path of a Unix domain socket, which only the user can open, and
// answers JSON requests on it, one per line:
//
//	MYGO_AUTOMATION=/tmp/app.sock ./app
//
// Builds without the inspector of native UI (-tags mygo_noinspector, as
// mygo build makes for release) leave the plugin out: it never listens
// there. See docs/plugins/automation.md for the protocol, and for
// mygo-automation (cmd/mygo-automation), its command line client for
// agents.
package automation
