package desktop

import "github.com/egoist/mygo"

// askAttention flashes the window (the Dock icon bounces on macOS) without taking focus: a dialog an agent
// raised cannot be seen in a window that is hidden or behind others.
func askAttention() {
	for _, w := range mygo.Windows() {
		w.FlashFrame(true)
	}
}
