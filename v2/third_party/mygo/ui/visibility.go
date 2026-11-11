package ui

// WindowHidden reports whether nothing of the window shows on screen, as
// the web's document.hidden: it is minimized, hidden, on another space or
// desktop, or covered by other windows, as far as the system tells (see
// the platform differences). A view that shows a clock, a price or what
// plays stops working while it is hidden, and refreshes as it shows
// again: the view builds once as it hides, and again as it shows, from
// the first time it asks. Frames that only move what shows wait while it
// is hidden anyway.
//
//	app.ticker.SetPaused(c.WindowHidden())
func (c *context) WindowHidden() bool {
	c.rt.hiddenAsked = true
	return c.rt.host.occluded()
}
