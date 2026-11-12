package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// An app started from Finder or the Dock has PATH=/usr/bin:/bin:/usr/sbin:/sbin, so tools looked up by
// name (nexttrace) are missed when installed with Homebrew. Terminals run a login shell, which sets its own PATH.
func init() {
	path := filepath.SplitList(os.Getenv("PATH"))
	for _, dir := range []string{"/opt/homebrew/bin", "/usr/local/bin"} {
		if !slices.Contains(path, dir) {
			path = append(path, dir)
		}
	}
	os.Setenv("PATH", strings.Join(path, string(filepath.ListSeparator)))
}
