package ui

// Split lays out first and second side by side, with a divider between
// them that the user drags to resize them, or moves with the arrows once
// it has the keyboard focus: *size is the width of first, which the
// divider keeps 40 DIPs from either edge. With less room than that, first
// gives way, keeping *size for when there is room again. Changed reports a
// move. Right to left (Direction), first is on the right. SplitReset makes
// a double click on the divider put first back at a size the app chooses.
//
//	ui.Split(c, &app.sidebar, app.files, app.editor).Fill()
func coreSplit(c *context, size *float32, first, second func()) *node {
	return split(c, size, first, second, false, false)
}

// SplitVertical is Split with first above second, *size its height.
func coreSplitVertical(c *context, size *float32, first, second func()) *node {
	return split(c, size, first, second, true, false)
}

// SplitPercent is Split with *p the percentage of the room first takes,
// from 0 to 100, so that both panes grow and shrink with the room, in
// proportion.
//
//	ui.SplitPercent(c, &app.sidebarPercent, app.files, app.editor).Fill()
func coreSplitPercent(c *context, p *float32, first, second func()) *node {
	return split(c, p, first, second, false, true)
}

// SplitVerticalPercent is SplitPercent with first above second.
func coreSplitVerticalPercent(c *context, p *float32, first, second func()) *node {
	return split(c, p, first, second, true, true)
}

// splitResetKey is the local that keeps a split's SplitReset settings.
type splitResetKey struct{}

// splitResetSettings are what SplitReset says of a split in this build,
// and what it said in the build before, which the input since applies by:
// the split the user double-clicked.
type splitResetSettings struct {
	now, shown       float32
	nowSet, shownSet bool
}

// SplitReset makes a double click on the divider of a Split put the first
// pane at v, in the split's own unit: DIPs for Split and SplitVertical, a
// percentage for SplitPercent and SplitVerticalPercent. A split without it
// keeps its size on a double click. The value is kept even when the window
// is too small to show it, so the pane takes it back when there is room
// again.
//
//	ui.Split(c, &app.sidebar, app.files, app.editor).SplitReset(220)
func (e *node) SplitReset(v float32) *node {
	if e.widget != "Split" {
		return e
	}
	s := coreLocal(e, splitResetKey{}, func() splitResetSettings { return splitResetSettings{} })
	s.now, s.nowSet = v, true
	return e
}

// splitResetOf returns the SplitReset settings of a split, or nil when it
// has none.
func splitResetOf(e *node) *splitResetSettings {
	if v, ok := e.st.locals[splitResetKey{}]; ok {
		if s, ok := v.(*splitResetSettings); ok {
			return s
		}
	}
	return nil
}

// split shares the room between first and second: *size is first's size
// in DIPs, or with inPercent its percentage of the room.
func split(c *context, size *float32, first, second func(), vertical, inPercent bool) *node {
	t := c.theme
	minPane, grip := t.Space(10), t.Space(1.5)
	stack := func() *node {
		// One element or the other: creating one adds it to the parent.
		if vertical {
			return coreColumn(c)
		}
		return coreRow(c)
	}
	length := func(n *node, v float32) {
		if vertical {
			n.Height(v)
		} else {
			n.Width(v)
		}
	}
	minLength := func(n *node, v float32) {
		if vertical {
			n.MinHeight(v)
		} else {
			n.MinWidth(v)
		}
	}
	e := stack().AlignItems(Stretch)
	e.widget = "Split"
	// reset is what SplitReset said of the split in the build before, which
	// the input since applies by; this build's SplitReset says what the
	// next build's input applies by.
	reset := splitResetOf(e)
	if reset != nil {
		reset.shown, reset.shownSet = reset.now, reset.nowSet
		reset.now, reset.nowSet = 0, false
	}
	e.Children(func() {
		// The panes meet at a line one DIP thick, with nothing between them
		// and it. The handle the pointer drags spans a few DIPs over either
		// pane, above them: it is placed in this box, which has no padding,
		// whatever the split's.
		in := stack().Grow(1).AlignItems(Stretch)
		minLength(in, 0)
		// The room to share, as the last frame laid it out.
		total := in.st.w
		if vertical {
			total = in.st.h
		}
		share := min(max(*size, 0), 100) / 100
		// shown is first's size as the last frame laid it out. Drags and
		// keys move the divider from there, not from *size, which a split
		// too small for it does not show.
		shown := *size
		switch {
		case inPercent && total > 0:
			shown = fitSplit(share*(total-1), total, minPane)
		case !inPercent && total > 0 && *size > minPane:
			shown = fitSplit(*size, total, minPane)
		}
		set := func(v float32) {
			v = fitSplit(v, total, minPane)
			if inPercent {
				if total <= 1 {
					return
				}
				v = v * 100 / (total - 1)
			}
			if v != *size {
				*size = v
				e.st.markChanged()
				c.rt.consumed = true
			}
		}
		// resetTo puts first at the size SplitReset chose, in the split's
		// own unit. Unlike set, it keeps a size the room cannot show, as
		// the divider's press does, so that first takes it back when there
		// is room again.
		resetTo := func(v float32) {
			if inPercent {
				v = min(max(v, 0), 100)
			} else {
				v = max(v, minPane)
			}
			if v != *size {
				*size = v
				e.st.markChanged()
				c.rt.consumed = true
			}
		}
		in.Children(func() {
			// The layout sizes the panes, not shown: shown uses the last
			// frame's room, and the layout uses this frame's.
			a := coreBox(c).Shrink(1).Clip()
			if inPercent {
				a.Grow(share)
				minLength(a, minPane)
			} else {
				length(a, *size)
				minLength(a, min(*size, minPane))
			}
			a.Children(first)

			// The line takes the focus between the panes, in their order.
			div := coreBox(c).Shrink(0).Focusable().Role(RoleSplitter).Label(c.texts().Divider())
			div.widget = "Divider"
			div.flags |= flagOwnRing
			length(div, 1)
			// The arrows move the line the way they point.
			back, forth := div.arrows()
			if vertical {
				back, forth = KeyUp, KeyDown
			}
			div.afterInput(func() {
				if div.Shortcut(0, back) {
					set(shown - t.Space(2.5))
				}
				if div.Shortcut(0, forth) {
					set(shown + t.Space(2.5))
				}
			})
			div.hasRange, div.accRange = true, [3]float64{float64(minPane), float64(max(total-1-minPane, minPane)), float64(shown)}
			div.accStep = float64(t.Space(2.5))

			b := coreBox(c).Grow(1).Shrink(1).Clip()
			if inPercent {
				b.Grow(1 - share)
			}
			minLength(b, minPane)
			b.Children(second)

			// placeOn keeps the handle on the divider where this frame lays
			// it out, also when the first pane gives way.
			handle := coreBox(c).Absolute()
			handle.placeOn = div
			handle.flags |= flagDraggable | flagHover
			if vertical {
				handle.Left(0).Right(0).Top(-(grip - 1) / 2).Height(grip).Cursor(CursorResizeNS)
			} else {
				handle.Top(0).Bottom(0).Start(-(grip - 1) / 2).Width(grip).Cursor(CursorResizeEW)
			}
			// A press there focuses the line, as a press on it would. Only a
			// move changes the size, so a press keeps *size for when there is
			// room again. A double click restores the size SplitReset chose.
			dx, dy, held := handle.Dragged()
			handle.afterInput(func() {
				if reset != nil && reset.shownSet && handle.DoubleClicked() {
					div.Focus()
					resetTo(reset.shown)
					return
				}
				if !held {
					return
				}
				div.Focus()
				if dx == 0 && dy == 0 {
					return
				}
				switch {
				case vertical:
					set(shown + dy)
				case handle.rtl():
					set(shown - dx)
				default:
					set(shown + dx)
				}
			})
			div.Draw(func(p *Painter, r Rect) {
				line := t.Border
				if held || handle.Hovered() {
					line = t.Accent
				}
				p.Fill(r, line, 0)
			})
			handle.Draw(func(p *Painter, r Rect) {
				if div.FocusVisible() {
					p.focusRingOf(div, r, [4]float32{})
				}
			})
		})
	})
	return e
}

// fitSplit clamps v, the first pane's size, to leave minPane on either side
// of the divider in total, the room as last laid out (0 before the first
// layout).
func fitSplit(v, total, minPane float32) float32 {
	if total > 0 {
		v = min(v, total-1-minPane)
	}
	return max(v, minPane)
}
