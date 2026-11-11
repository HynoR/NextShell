//go:build !darwin

package sfsymbols

import "github.com/egoist/mygo/ui"

// Image returns the SF Symbol named name, drawn px pixels square in col,
// or false where the system has no such symbol: on another OS, or a macOS
// without it. Here it never has one.
func Image(name string, px int, col ui.Color, o Options) (ui.ImageSource, bool) {
	return nil, false
}

// Available reports whether this system draws SF Symbols at all. It does
// not.
func Available() bool { return false }

// Names returns the names of the SF Symbols this system has. It has none.
func Names() []string { return nil }
