package ui

// fitter measures the content of a window that takes its content's size
// (mygo.FlyoutOptions.FitContent, surface.Conn.ContentSize) as each frame
// lays it out, and tells the window when that size changed: the window
// resizes, and the frame that follows lays the content out at its size,
// as a browser lays out a popup at the size of its page's content.
type fitter struct {
	// widths returns the bounds of the window's width, in its DIPs, 0 for
	// none; tell hears the size.
	widths func() (min, max float64)
	tell   func(width, height float64)
	// told is the size last told, and before the one told before it;
	// laid is the window's size the last frame was laid out at.
	told, before, laid [2]float32
}

func newFitter(widths func() (min, max float64), tell func(width, height float64)) *fitter {
	none := [2]float32{-1, -1}
	return &fitter{widths: widths, tell: tell, told: none, before: none}
}

// measureFit measures the size the frame's content takes, in the window's
// DIPs: its max-content width, within the window's bounds, and its height
// at that width, as an html element of width: fit-content and height:
// auto. Elements placed absolutely, as overlays and toasts, take none of
// it, nor does the inspector, which the content keeps its size beside.
//
// A frame laid out at the size it last told, which measures the size it
// told before that, holds: its view builds the content after the window's
// size (Context.Size), which would have it resize back and forth. The
// content changing in a frame at the same size as the last tells it.
func (rt *engine) measureFit(root *node) {
	f := rt.fit
	if f == nil || rt.insp.open {
		return
	}
	z := float32(1)
	if rt.zoomed() {
		z = rt.contentZoom
	}
	lo, hi := f.widths()
	w := max(contentWidth(root, true)*z, float32(lo))
	if hi > 0 {
		w = min(w, float32(hi))
	}
	h := (contentHeight(root, max(w/z-root.padX(), 0)) + root.padY()) * z
	ww, wh, _ := rt.host.size()
	resized := f.laid != [2]float32{ww, wh}
	f.laid = [2]float32{ww, wh}
	m := [2]float32{w, h}
	if m == f.told || resized && m == f.before {
		return
	}
	f.before, f.told = f.told, m
	f.tell(float64(w), float64(h))
}
