package ui

// Scrollbar is the scroll bar of a view that scrolls itself rather than
// through a scroll container, as a code editor or a terminal does, laid
// out and painted as a scroll container's are, so that every bar of an app
// looks alike and follows the theme (Theme.Scrollbar, ScrollbarWidth).
//
// The view paints the bar with Painter.Scrollbar while it shows it: a
// scroll container's show while the pointer is over the container or
// drags a thumb, and maps a drag of the thumb to an offset with Thumb and
// OffsetAt.
//
//	bar := ui.Scrollbar{Box: r, View: viewH, Content: contentH, Offset: scrollY}
//	if hovered || dragging {
//		p.Scrollbar(bar, ui.ScrollbarState{Dragging: dragging})
//	}
type Scrollbar struct {
	// Box is the view the bar runs along, at its right edge, or its
	// bottom edge for a horizontal bar, in the coordinates the bar is
	// painted or hit tested in.
	Box Rect
	// Horizontal makes the bar run along the bottom, scrolling sideways.
	Horizontal bool
	// View is the length of the content the view shows, Content the
	// length of all of it, and Offset how far it is scrolled, in any
	// unit, as rows of a terminal's scrollback: the bar shows nothing
	// unless Content is longer than View.
	View, Content, Offset float32
}

// ScrollbarState is how a Scrollbar paints: Dragging while the pointer
// drags its thumb, which widens it, as a scroll container's; Color, if
// not zero, colors it for a view whose colors are not the theme's, as a
// terminal's, in place of Theme.Scrollbar.
type ScrollbarState struct {
	Dragging bool
	Color    Color
}

// Scrolls reports whether the bar has anywhere to scroll, and so shows.
func (s Scrollbar) Scrolls() bool { return s.Content > s.View+0.5 }

// along returns where the track starts along the bar, and its length.
func (s Scrollbar) along() (pos, track float32) {
	if s.Horizontal {
		return s.Box.X, max(s.Box.W, 0)
	}
	return s.Box.Y, max(s.Box.H, 0)
}

// Thumb returns where the thumb starts along the bar, as Box's X for a
// horizontal bar, else its Y, and how long it is.
func (s Scrollbar) Thumb() (start, length float32) {
	pos, track := s.along()
	t := scrollThumb(pos, track, s.View, max(s.Content, s.View), s.Offset)
	return t.Y, t.H
}

// OffsetAt returns the offset that brings the thumb's start to start, as
// the thumb is dragged, within the content.
func (s Scrollbar) OffsetAt(start float32) float32 {
	pos, track := s.along()
	_, length := s.Thumb()
	travel := track - 4 - length
	reach := s.Content - s.View
	if travel <= 0 || reach <= 0 {
		return 0
	}
	return max(0, min((start-pos-2)/travel, 1)) * reach
}

// thumbRect returns the thumb's box, width DIPs across.
func (s Scrollbar) thumbRect(width float32) Rect {
	start, length := s.Thumb()
	if s.Horizontal {
		return Rect{start, s.Box.Y + s.Box.H - width - 3, length, width}
	}
	return Rect{s.Box.X + s.Box.W - width - 3, start, width, length}
}

// Scrollbar paints the thumb of a scroll bar, as a scroll container's, on
// no track, if it has anywhere to scroll.
func (p *Painter) Scrollbar(s Scrollbar, st ScrollbarState) {
	if !s.Scrolls() {
		return
	}
	t := p.Theme()
	c := t.Scrollbar
	if st.Color != (Color{}) {
		// As faint as the theme's, which High Contrast makes bolder.
		c = st.Color.Alpha(float32(t.Scrollbar.A) / 255)
	}
	p.scrollThumbFill(s.thumbRect(t.scrollbarWidth()), s.Horizontal, st.Dragging, c)
}

// scrollThumbFill paints a thumb at bar, widened toward the view while
// dragged.
func (p *Painter) scrollThumbFill(bar Rect, horizontal, dragging bool, c Color) {
	r := bar.W / 2
	if horizontal {
		r = bar.H / 2
	}
	if dragging {
		if horizontal {
			bar.Y, bar.H = bar.Y-2, bar.H+2
		} else {
			bar.X, bar.W = bar.X-2, bar.W+2
		}
		r++
	}
	p.fill(bar, [4]float32{r, r, r, r}, c, 0, Color{})
}
