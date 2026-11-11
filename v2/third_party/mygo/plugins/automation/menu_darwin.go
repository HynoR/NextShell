//go:build darwin && !mygo_noinspector

package automation

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/internal/darwin"
)

// chooseMenuItem chooses an item of the application menu by the titles
// along its path, as the menu bar does. Main thread only.
func chooseMenuItem(_ *mygo.Window, path []string) error {
	return darwin.TestPerformMenuItem(path...)
}
