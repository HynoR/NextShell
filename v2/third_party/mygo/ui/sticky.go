package ui

// Sticky keeps the element in its parent's flow, where it takes its room,
// and holds it inside the scroll container around it while its parent is
// in view, as CSS's position: sticky does: Top, Right, Bottom and Left (or
// Start and End, and their percentages of the container's size) say how far
// in from the container's edges, inside its border, it stays. It moves no
// further than its parent's content box, so the parent leaving the view
// takes it along, and the next one's comes up in its place: a day's heading
// at the top of a timeline until the next day pushes it out, or the check
// box choosing all the rows at the left of a table scrolling sideways.
//
//	ui.Scroll(c).Fill().Children(func() {
//		for _, day := range app.days {
//			ui.Column(c).Children(func() {
//				ui.Text(c, day.Title).Bold().Background(t.Surface).Sticky().Top(0)
//				for _, ev := range day.Events {
//					eventRow(c, ev)
//				}
//			})
//		}
//	})
//
// Each axis follows the nearest container scrolling that way, or one
// following another sideways (ScrollWith), or else the window: a heading
// in a table scrolling sideways in a page scrolling down sticks to the
// page's top and to the table's start. Where both insets of an axis are
// set and the container is too small for both, Top wins, and Left, or
// Right right to left. Without insets, the element stays where the flow
// puts it. It paints above its siblings, as Absolute elements do, and the
// pointer, assistive technology and the focus ring find it where it shows;
// the focus and assistive technology still take it in its place among
// them. Sticky does nothing to an Absolute element, nor to one inside a
// text.
func (e *node) Sticky() *node { e.sticky = true; return e }

// stuck reports whether the element is sticky and in its parent's flow.
func (e *node) stuck() bool {
	return e.sticky && e.flags&flagAbsolute == 0 && e.parent != nil && !e.isInline()
}

// placedOrigin returns where place put an element, in the window.
func placedOrigin(e *node) (x, y float32) { return e.x, e.y }

// stickyShift returns how far the sticky element e moves from (x, y), where
// the flow puts it in the window, as at says the elements around it are:
// where place put them, or will (laidOutOrigin).
func (e *node) stickyShift(x, y float32, at func(*node) (float32, float32)) (dx, dy float32) {
	if e.inset == [4]length{} {
		return 0, 0
	}
	// The containing block, the parent's content box; of a container that
	// scrolls, or follows another, the content it moves.
	p := e.parent
	px, py := at(p)
	cx, cy := px+p.contentX(), py+p.contentY()
	cw, ch := max(p.w-p.padX(), 0), max(p.h-p.padY(), 0)
	if f := p.followX; f != nil {
		cx -= float32(f.followShift())
		for c := p.first; c != nil; c = c.next {
			if c.flags&flagAbsolute == 0 && !c.hidden {
				cw = max(cw, c.x+c.w+c.m(1)-p.contentX())
			}
		}
	}
	if p.scrolls() {
		cx -= float32(p.scrollLeft())
		cy -= float32(p.st.scrollY)
		cw = max(cw, float32(p.contentW)-p.padX())
		ch = max(ch, float32(p.contentH)-p.padY())
	}

	// The views it sticks in: the padding boxes of the containers around it
	// scrolling each way, or the window's.
	var vx, vy *node
	a := p
	for ; vx == nil || vy == nil; a = a.parent {
		if vx == nil && (a.flags&flagScrollX != 0 || a.followX != nil) {
			vx = a
		}
		if vy == nil && a.flags&flagScrollY != 0 {
			vy = a
		}
		if a.parent == nil {
			break
		}
	}
	if vx == nil {
		vx = a
	}
	if vy == nil {
		vy = a
	}
	ax, _ := at(vx)
	_, ay := at(vy)
	v0x, v1x := ax+vx.border[3], ax+vx.w-vx.border[1]
	v0y, v1y := ay+vy.border[0], ay+vy.h-vy.border[2]

	top, tok := e.inset[0].resolve(max(v1y-v0y, 0))
	right, rok := e.inset[1].resolve(max(v1x-v0x, 0))
	bottom, bok := e.inset[2].resolve(max(v1y-v0y, 0))
	left, lok := e.inset[3].resolve(max(v1x-v0x, 0))
	dy = stick(y, y+e.h, e.m(0), e.m(2), cy, cy+ch, v0y+top, v1y-bottom, tok, bok, false)
	dx = stick(x, x+e.w, e.m(3), e.m(1), cx, cx+cw, v0x+left, v1x-right, lok, rok, e.rtl())
	return dx, dy
}

// stick returns how far a box from lo to hi along an axis, with margins mlo
// and mhi, moves to keep it after from (when fromOK) and before to (when
// toOK), within its containing block from clo to chi: where both cannot
// hold, from wins, or to with toWins. As CSS says, its margins count only
// as far as they fit in the containing block.
func stick(lo, hi, mlo, mhi, clo, chi, from, to float32, fromOK, toOK, toWins bool) float32 {
	lo -= min(mlo, max(lo-clo, 0))
	hi += min(mhi, max(chi-hi, 0))
	var d float32
	toStart := func() {
		if fromOK && lo+d < from {
			d = min(from-lo, max(d, chi-hi))
		}
	}
	toEnd := func() {
		if toOK && hi+d > to {
			d = max(to-hi, min(d, clo-lo))
		}
	}
	if toWins {
		toStart()
		toEnd()
	} else {
		toEnd()
		toStart()
	}
	return d
}

// stickyHits moves the hits a sticky child took, r of rt.hits as it was
// committed in the flow, to the end, over those of the children committed
// before, as they paint: commit calls it as it reaches the child among the
// absolute ones. moved is how far hits moved back so far, which it returns
// with these.
func (rt *engine) stickyHits(r [2]int, moved int) int {
	a, b := r[0]-moved, r[1]-moved
	if b > a {
		h := rt.hits[a:]
		k := b - a
		reverseHits(h[:k])
		reverseHits(h[k:])
		reverseHits(h)
	}
	return moved + b - a
}

func reverseHits(h []hit) {
	for i, j := 0, len(h)-1; i < j; i, j = i+1, j-1 {
		h[i], h[j] = h[j], h[i]
	}
}
