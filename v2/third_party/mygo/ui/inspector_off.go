//go:build mygo_noinspector

package ui

import (
	"time"

	"github.com/egoist/mygo/internal/platform"
)

// Production builds (mygo build, unless MYGO_INSPECTOR=1 or --debug) leave
// the inspector out, as their developer tools are off: this stands in for
// it, never open, so that it takes no room in the binary.
type inspector struct {
	enabled, open, changed bool
	selected               uint64
}

func (in *inspector) contentWidth(w float32) float32                                { return w }
func (in *inspector) lap(int)                                                       {}
func (in *inspector) repainted(time.Duration)                                       {}
func (in *inspector) noteSource()                                                   {}
func (in *inspector) noteKey(uint64, any)                                           {}
func (in *inspector) pointer(*engine, platform.SurfaceEvent, float32, float32) bool { return false }
func (in *inspector) applyEdits(*node)                                              {}
func (in *inspector) holds(*engine, uint64) bool                                    { return false }

// inspectorBuilt keeps out of the layout what an empty function would still
// cost it: gridLayout's frame and code grow by the arguments of a call that
// inlines to nothing. It is not a method, which the type would keep.
const inspectorBuilt = false

func noteGridTracks(*node, []float32, []float32, []float32, []float32, float32, float32, bool) {}

func (in *inspector) isForced(uint64, uint8) bool                     { return false }
func (in *inspector) snapshot(*engine, *node)                         {}
func (in *inspector) paintHighlight(*engine, *Painter, float32)       {}
func (rt *engine) toggleInspector()                                   {}
func (rt *engine) inspectKey(Modifiers, Key) bool                     { return false }
func (rt *engine) buildInspector(*context, float32, float32, float32) {}
