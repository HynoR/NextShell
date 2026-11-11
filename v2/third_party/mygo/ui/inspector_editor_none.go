//go:build !mygo_noinspector && ((!unix && !windows) || ios || android)

package ui

import "errors"

// editorsRun reports whether this system starts programs: a browser and a
// phone don't, and don't have the app's source either.
const editorsRun = false

func startEditor(string, []string, func(error)) error { return errors.ErrUnsupported }
