package appui

import (
	"time"

	"github.com/egoist/mygo/plugins/terminal"
)

// Hooks for the tests of the grid (grid_test.go).

func (a *App) GridOn() bool   { return a.grid.on }
func (a *App) GridPumps() int { return a.grid.pumps }

// GridMirrors are the mirrors the grid holds now, by session.
func (a *App) GridMirrors() map[string]*terminal.Mirror {
	out := map[string]*terminal.Mirror{}
	for id, t := range a.grid.tiles {
		out[id] = t.mirror
	}
	return out
}

// GridPicture is the terminal that shows a session's picture, nil for a session with none.
func (a *App) GridPicture(id string) *terminal.Terminal {
	if t := a.grid.tiles[id]; t != nil {
		return t.view
	}
	return nil
}

func GridPlan(natural [][2]float32, w, h float32) (cols int, tw, th float32) {
	return gridPlan(natural, w, h)
}
func GridCell() [2]float32 { return gridCell() }

// SetGridBudget changes how long a frame spends on the sessions' frames, for tests.
func SetGridBudget(d time.Duration) (restore func()) {
	old := gridBudget
	gridBudget = d
	return func() { gridBudget = old }
}

func GridScale(natural [2]float32, tw, th float32) float32 { return gridScale(natural, tw, th) }
