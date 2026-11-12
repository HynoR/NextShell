//go:build !nextshell_devtools

package main

import (
	"flag"
	"io"
	"nextshell.dev/v2/internal/desktop"
)

const developmentInspector = false
const cloudUI = false

func registerDevFlags(*flag.FlagSet, *options)                 {}
func checkDevFlags(options) error                              { return nil }
func startDevMode(*desktop.Platform, options, io.Writer) error { return nil }
