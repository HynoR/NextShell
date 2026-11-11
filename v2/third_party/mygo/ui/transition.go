package ui

import (
	"math"
	"slices"
	"sync"
	"time"

	"github.com/egoist/mygo/internal/scene"
)

// ElementTransition animates an element (Element.Transition) from what the
// last frame showed to what the layout makes of it in this one: where it
// is in its parent, its size, its background and border colors, and its
// shadows. Enter and Exit animate it as it appears and as it goes:
//
//	ui.Row(c).Key(item.ID).Transition(ui.ElementTransition{
//		Enter: &ui.Motion{Collapse: true},
//		Exit:  &ui.Motion{Collapse: true},
//	})
type ElementTransition struct {
	// Duration is how long a change takes: 200 ms when zero, and none
	// when negative, which shows every change, the Enter and the Exit at
	// once, as for an app asking for less motion of its own.
	Duration time.Duration
	// Ease is how the element goes, as for AnimateWith: EaseOut when nil.
	Ease Easing
	// Animation, if set, is how changes go instead of Duration and Ease:
	// with a spring (Snappy, …), a change on the way keeps the element
	// going as it went; with a Delay, changes wait, as the rows of a list
	// entering one after another.
	Animation *Animation
	// ExitDuration and ExitEase, if set, are how long the Exit takes and
	// how it goes, in place of Duration and Ease, or of Animation's spring
	// (whose Delay still holds it): a popup that comes in over 150 ms and
	// goes over 100. When zero, ExitDuration is Duration's, or about as
	// long as Animation's spring; when negative, the element goes at once,
	// without a copy. ExitEase is Ease's, or that of Animation's curve,
	// when nil.
	ExitDuration time.Duration
	ExitEase     Easing
	// Position, Size, Colors and Shadows choose what animates: where the
	// element is in its parent, its width and height (its content laid out
	// at each size on the way), its background and border colors, and its
	// shadows (Shadow, InsetShadow), as a resting shadow fading on hover.
	// None set animates them all.
	Position, Size, Colors, Shadows bool
	// Width and Height, in place of Size, animate the element's width or
	// its height alone, as CSS's transition-property lists one of them:
	// the other follows the layout at once, as a panel springing open
	// while the split view holding it is dragged wider.
	Width, Height bool
	// Enter, if set, is where the element comes from as it appears in a
	// parent that the last frame showed: it starts there, and goes to its
	// place.
	Enter *Motion
	// Exit, if set, is where the element goes once it is no longer built,
	// while its parent still is: a copy of it, as the last frame showed
	// it, goes there under its siblings of the same z-index (ZIndex),
	// taking neither room, the pointer nor the focus, and is then gone. The parent's ChildExit, in the
	// frame it goes, says where it goes instead.
	Exit *Motion
	// AfterExits holds the element appearing while copies of its siblings
	// leave with their Exit: it takes its place unseen, and comes in with
	// its Enter once they are gone, so that its parent shows one of them
	// at a time, as a panel that finishes leaving before the next one
	// appears.
	AfterExits bool
	// Match, if set, is an identity the element shares with one that the
	// frame before showed elsewhere, even in another parent, as a photo's
	// thumbnail in a grid and the photo in a detail view: the element
	// appearing comes from where that one was, at its size and in its
	// colors, as SwiftUI's matchedGeometryEffect and Motion's layoutId.
	// Use a value of a type of your own, as a struct of the photo's ID.
	Match any
	// Fly, with a Match, flies the element appearing from where the other
	// was, as Keynote's Magic Move and Flutter's Hero do: from its
	// corners, opacity, text color and transform (in 3D too), and from the
	// transforms of the elements around it, drawn over the rest of the
	// window (or of its overlay) on the way, out of the clips around it,
	// where it takes the pointer. The other, flying too, goes with it,
	// fading out under it, rather than as its Exit says.
	Fly bool
	// ScaleContent shows the element's size changing, as it resizes or
	// comes from the element of its Match, by scaling it from its own size
	// rather than laying its content out at each size on the way: its text
	// keeps its lines, growing or shrinking with it, as a title into a
	// heading, and a picture stretches with its box.
	ScaleContent bool
}

// Motion is where an element appearing comes from, or where one going
// goes, from its place in the layout.
type Motion struct {
	// X and Y move it by as many DIPs, and XPercent and YPercent by as
	// many percent of its own width and height, as TranslatePercent does.
	X, Y               float32
	XPercent, YPercent float32
	// Opacity is how opaque it is there: 0, as by default, is
	// transparent, and 1 opaque.
	Opacity float32
	// Collapse makes it nothing along its parent's direction: no height in
	// a column, no width in a row. Its content keeps its layout, clipped.
	Collapse bool
	// Scale is how large it is there, around its TransformOrigin: 0.9 a
	// tenth smaller; 0, as by default, keeps it at its size, as 1 does.
	// Rotate turns it by as many degrees clockwise.
	Scale, Rotate float32
	// Blur blurs it there by as many DIPs, as Element.Blur does: text or
	// an icon crossing over to another through a blur of 4 or so.
	Blur float32
	// Flip, for an element attached to another (AttachTo), mirrors X and
	// XPercent where the layout put it across its target from where it
	// was asked to go, and Y and YPercent where it put it the other way
	// down (AttachedAnchors), from the frame it appears: a menu asked
	// below its button, coming down from Y: -8, comes up from 8 where it
	// opens above it, as its Exit goes.
	Flip bool
}

// The sides AttachTo put an element on, across and down from those asked
// (node.flips).
const (
	flipAcross uint8 = 1 << iota
	flipDown
)

// flippedFor returns m, mirrored where the layout flipped e (Flip).
func (m *Motion) flippedFor(e *node) *Motion {
	if !m.Flip || e.flips == 0 {
		return m
	}
	f := *m
	if e.flips&flipAcross != 0 {
		f.X, f.XPercent = -f.X, -f.XPercent
	}
	if e.flips&flipDown != 0 {
		f.Y, f.YPercent = -f.Y, -f.YPercent
	}
	return &f
}

// defaultDuration is the Duration of a Transition without one.
const defaultDuration = 200 * time.Millisecond

// Transition animates the element as t asks. The element needs the same
// ID from frame to frame: a Key, or the same place among its siblings.
// Elements that the change moves as well need a Transition of their own to
// move smoothly, the same one to move in step with it, as the rows of a
// list:
//
//	for _, it := range app.items {
//		ui.Row(c).Key(it.ID).Transition(rowTransition).Children(func() {
//			ui.Text(c, it.Title)
//		})
//	}
//
// Changes made by resizing the window do not animate. While the desktop
// asks for less motion (Preferences.ReduceMotion), nothing moves: colors
// still change smoothly, and an Enter or Exit that fades or blurs without
// collapsing fades and blurs in place.
func (e *node) Transition(t ElementTransition) *node {
	e.c.transitions = append(e.c.transitions, transitionUse{e: e, t: t})
	linkShadows.Do(func() { animateShadows = (*engine).animateShadows })
	linkGroups() // its fades
	if t.Match != nil || t.ScaleContent {
		e.c.rt.linkMatches()
	}
	scene.FoldTranslations() // renderers draw the poses that only move as untransformed content
	return e
}

// Transitioning reports whether the element's Transition was moving it,
// or changing its colors, or its pose (AnimatePose) or Draggable was
// moving it, in the frame before: false once it is where it goes.
func (e *node) Transitioning() bool {
	if !e.hasState() {
		return false
	}
	frame := e.c.rt.frame
	if p := e.st.pose; p != nil && p.frame+1 >= frame && p.moving() {
		return true
	}
	if d := e.st.dragRec; d != nil && d.frame+1 >= frame && d.moving() {
		return true
	}
	r := e.st.trec
	return r != nil && r.built+1 >= frame && (r.moving != 0 || r.colorMoving || r.matching)
}

// ChildExit says where the children of the element that leave in this
// frame go, in place of the Exit of their Transitions: those whose
// Transition has an Exit, which are copied as they go. It suits an exit
// decided as they leave, as a calendar, a wizard or a carousel stepping
// forward or back, whose page going goes the way of the step that sent
// it, even when the step turned around in the frame it went:
//
//	dir := float32(app.dir) // 1 forward, -1 back
//	ui.Box(c).Fill().ChildExit(ui.Motion{X: -40 * dir}).Children(func() {
//		ui.Column(c.Key(app.page)).Absolute().Fill().Transition(ui.ElementTransition{
//			Enter: &ui.Motion{X: 40 * dir},
//			Exit:  &ui.Motion{X: -40 * dir},
//		})
//	})
//
// Copies already on their way go on as they went.
func (e *node) ChildExit(m Motion) *node {
	c := e.c
	for i := range c.childExits {
		if c.childExits[i].e == e {
			c.childExits[i].m = m
			return e
		}
	}
	c.childExits = append(c.childExits, childExit{e, m})
	return e
}

// childExit is a call of ChildExit in the frame being built.
type childExit struct {
	e *node
	m Motion
}

// childExit returns the Exit that p gave its children going in this
// frame, if it gave one.
func (c *context) childExit(p *node) (Motion, bool) {
	for _, x := range c.childExits {
		if x.e == p {
			return x.m, true
		}
	}
	return Motion{}, false
}

// transitionUse is a call of Transition in the frame being built.
type transitionUse struct {
	e *node
	t ElementTransition
	r *transition
}

// tbox is what a transition moves: an element's place in its parent's
// box, its size, its opacity, and the scale, rotation and blur of a
// Motion.
type tbox struct{ x, y, w, h, alpha, scale, rot, blur float32 }

// vals returns the components of b, in the order of the channels.
func (b tbox) vals() [8]float32 {
	return [8]float32{b.x, b.y, b.w, b.h, b.alpha, b.scale, b.rot, b.blur}
}

func boxOf(v [8]float32) tbox { return tbox{v[0], v[1], v[2], v[3], v[4], v[5], v[6], v[7]} }

// What a transition moves now.
const (
	movePos uint8 = 1 << iota
	moveWidth
	moveHeight
	moveAlpha
	moveTurn
	moveSize = moveWidth | moveHeight
	moveAll  = movePos | moveSize | moveAlpha | moveTurn
)

// groupOf is what moves each channel of a transition: a blur goes with
// the opacity, as it moves nothing.
var groupOf = [8]uint8{movePos, movePos, moveWidth, moveHeight, moveAlpha, moveTurn, moveTurn, moveAlpha}

// The axes a Motion collapses an element along.
const (
	collapseWidth  = 1
	collapseHeight = 2
)

// transition is what an element's Transition keeps from frame to frame,
// by the element's ID. It outlives the element while a copy of it leaves.
type transition struct {
	t ElementTransition
	// built is the last frame that built the element, and laid the last
	// that laid it out: elem is its element then, and parent the ID of
	// its parent.
	built, laid uint64
	elem        *node
	parent      uint64
	// target is where the layout put the element in frame laid, next
	// where it puts it in the frame being laid out, and shown where it
	// shows: ch move the components of moving (movePos, …) there.
	target, next, shown tbox
	ch                  [8]channel
	moving              uint8
	// entering is the components of moving that the Enter set going, not
	// a move of the layout: what an AttachTo panel adds to its place.
	entering uint8
	fresh    bool
	// held keeps the element appearing unseen while copies of its
	// siblings leave (AfterExits), and focus the keyboard focus given to
	// it meanwhile, which it takes as it shows.
	held, focus bool
	// collapse is the axis a Motion collapses the element along, while it
	// does.
	collapse uint8
	// The background and border colors, which move as the element
	// paints, after its styleFn set them, k of the way from colorFrom.
	colorTarget, colorFrom, colorShown [2]Color
	colorK                             channel
	colorMoving, colorSet              bool
	colorFrame                         uint64
	// The shadows, which move as the colors do, k of the way from
	// shadowFrom.
	shadowTarget, shadowFrom, shadowShown []shadow
	shadowK                               channel
	shadowMoving, shadowSet               bool
	shadowFrame                           uint64
	// ghost is the copy of the element going, which moves from gone,
	// where it showed as it went; opacity and blur are its own, and turn
	// its transform then.
	ghost         *node
	gone          tbox
	opacity, blur float32
	turn          Transform
	// index is where the element was among its siblings in the frame it
	// went from, which orders the copies going under one parent.
	index int
	// matching moves an element appearing from where the one it Matches
	// showed, and match is the record of its Match it had last
	// (*matchRec, match.go), which apps without one do not link.
	matching bool
	match    any
}

// instantly is the Animation of a transition with a negative Duration,
// which headFor shows at its target at once.
var instantly = Animation{Duration: -1}

// animation returns how the transition's changes go.
func (r *transition) animation() Animation {
	if a := r.t.Animation; a != nil {
		return *a
	}
	if r.t.Duration < 0 {
		return instantly
	}
	ease := r.t.Ease
	if ease == nil {
		ease = EaseOut
	}
	return Animation{Duration: r.duration(), Ease: ease}
}

// exitAnimation returns how the transition's Exit goes: as its changes do
// but for its ExitDuration and ExitEase.
func (r *transition) exitAnimation() Animation {
	t := &r.t
	if t.ExitDuration == 0 && t.ExitEase == nil {
		return r.animation()
	}
	d := t.ExitDuration
	if d == 0 {
		d = r.duration()
	}
	if d <= 0 {
		return instantly
	}
	a := Animation{Duration: d, Ease: t.ExitEase}
	if b := t.Animation; b != nil {
		a.Delay = b.Delay
		if a.Ease == nil && b.Duration > 0 {
			a.Ease = b.Ease
		}
	} else if a.Ease == nil {
		a.Ease = t.Ease
	}
	return a
}

// exitsAtOnce reports whether the transition's Exit takes no time: the
// element goes without a copy.
func (r *transition) exitsAtOnce() bool { return r.exitAnimation().Duration < 0 }

// duration returns how long the transition's changes take, about as long
// for a spring, and 0 for none.
func (r *transition) duration() time.Duration {
	if a := r.t.Animation; a != nil {
		if a.Duration > 0 {
			return a.Duration
		}
		if a.Spring.Duration > 0 {
			return a.Spring.Duration
		}
		if a.Spring.Stiffness == 0 && a.Spring.Damping == 0 {
			return defaultSpringDuration
		}
		m, k, _, _ := a.Spring.physics()
		return time.Duration(2 * math.Pi * math.Sqrt(m/k) * float64(time.Second))
	}
	switch {
	case r.t.Duration > 0:
		return r.t.Duration
	case r.t.Duration < 0:
		return 0
	}
	return defaultDuration
}

// head sends the channels of move from where they are to target, as the
// transition's animation says, from now.
func (r *transition) head(target tbox, move uint8, now time.Time) {
	r.headWith(target, move, r.animation(), now)
}

// headWith sends the channels of move from where they are to target, as a
// says, from now.
func (r *transition) headWith(target tbox, move uint8, a Animation, now time.Time) {
	to := target.vals()
	for i := range r.ch {
		if move&groupOf[i] != 0 {
			headFor(&r.ch[i], float64(to[i]), a, now)
		}
	}
	r.moving |= move
}

// headFor sends ch from where it is now to to, as a says: there at once
// with a negative Duration (instantly).
func headFor(ch *channel, to float64, a Animation, now time.Time) {
	if a.Duration < 0 {
		*ch = channel{value: to, target: to, at: now}
		return
	}
	ch.advance(now)
	if to == ch.goal() && (ch.moving || ch.pending || ch.value == to) {
		return
	}
	switch {
	case a.Delay > 0:
		ch.pending, ch.pendingTo, ch.pendingAt, ch.pendingAnim = true, to, now.Add(a.Delay), a
		ch.pendingAnim.Delay = 0
	default:
		ch.pending = false
		ch.retarget(to, a)
	}
}

// follow shifts what moves that the Transition does not animate (own
// does not hold), as an Enter's place, by as much as the layout moved it
// since the frame before: that follows the layout at once, as it would
// without the Enter. A size scales instead, so that one collapsed opens
// on in proportion, never below nothing as the layout makes it smaller.
func (r *transition) follow(next tbox, own uint8) {
	was, to := r.target.vals(), next.vals()
	for i := range r.ch {
		g := groupOf[i]
		if g&(movePos|moveSize) == 0 || own&g != 0 || r.moving&g == 0 {
			continue
		}
		switch d := to[i] - was[i]; {
		case d == 0:
		case g&moveSize != 0 && was[i] > 0:
			r.ch[i].scale(float64(was[i]), float64(to[i]))
		default:
			r.ch[i].shift(float64(d))
		}
	}
}

// rest puts every channel at rest at b.
func (r *transition) rest(b tbox, now time.Time) {
	for i, v := range b.vals() {
		r.ch[i] = channel{value: float64(v), target: float64(v), at: now}
	}
	r.moving, r.entering, r.collapse = 0, 0, 0
}

// sample moves the channels to now, and returns where they show the
// element going to target: the components not moving there at once.
// The moves over, the transition no longer moves.
func (r *transition) sample(target tbox, now time.Time) tbox {
	to := target.vals()
	var v [8]float32
	var still uint8 = moveAll
	for i := range r.ch {
		ch := &r.ch[i]
		if r.moving&groupOf[i] == 0 {
			*ch = channel{value: float64(to[i]), target: float64(to[i]), at: now}
		} else {
			ch.advance(now)
			if ch.moving || ch.pending {
				still &^= groupOf[i]
			}
		}
		v[i] = float32(ch.value)
	}
	r.moving &^= still
	r.entering &= r.moving
	if r.moving == 0 {
		r.collapse = 0
		return target
	}
	return boxOf(v)
}

// What a transition animates.
const (
	propPosition uint8 = 1 << iota
	propWidth
	propHeight
	propColors
	propShadows
	propSize = propWidth | propHeight
)

func (r *transition) properties() uint8 {
	t := &r.t
	if !t.Position && !t.Size && !t.Colors && !t.Shadows && !t.Width && !t.Height {
		return propPosition | propSize | propColors | propShadows
	}
	var p uint8
	if t.Position {
		p |= propPosition
	}
	if t.Size {
		p |= propSize
	}
	if t.Width {
		p |= propWidth
	}
	if t.Height {
		p |= propHeight
	}
	if t.Colors {
		p |= propColors
	}
	if t.Shadows {
		p |= propShadows
	}
	return p
}

func lerp(a, b, k float32) float32 { return a + (b-a)*k }

// moved returns b moved as m says, collapsed along axis, or, without
// motion (still), only faded and blurred.
func (b tbox) moved(m *Motion, axis uint8, still bool) tbox {
	b.alpha = max(0, min(m.Opacity, 1))
	b.blur = max(m.Blur, 0)
	if still {
		return b
	}
	b.x += m.X + m.XPercent/100*b.w
	b.y += m.Y + m.YPercent/100*b.h
	if m.Scale != 0 {
		b.scale = m.Scale
	}
	b.rot = m.Rotate
	switch axis {
	case collapseWidth:
		b.w = 0
	case collapseHeight:
		b.h = 0
	}
	return b
}

// fades reports whether m makes an element appear or go even without
// motion: it fades or blurs, and collapses nothing, which would move its
// siblings.
func (m *Motion) fades() bool { return (m.Opacity < 1 || m.Blur > 0) && !m.Collapse }

// collapseAxis returns the axis a Motion collapses a child of parent along.
func collapseAxis(m *Motion, parent *node) uint8 {
	switch {
	case !m.Collapse:
		return 0
	case parent.row && parent.list == nil && !parent.grid:
		return collapseWidth
	}
	return collapseHeight
}

// animateLayout moves the elements given a Transition from where the last
// frame showed them to where the layout put them, and the copies of those
// gone with an Exit: after the layout of a frame in a window w×h, while
// boxes are relative to their parents.
func (rt *engine) animateLayout(root *node, w, h float32) {
	c := root.c
	// Changes that resizing the window makes do not animate.
	resized := w != rt.laidW || h != rt.laidH || rt.instant()
	rt.laidW, rt.laidH = w, h
	uses := c.transitions
	rt.exitsBuilt, rt.overlayExits = false, false
	if len(uses) == 0 && len(rt.trans) == 0 {
		return
	}
	if rt.trans == nil {
		rt.trans = map[uint64]*transition{}
	}
	now := c.now
	still := rt.preferences().ReduceMotion
	// Parents first: one resizing lays out its children anew, which then
	// move themselves.
	uses = c.byDepth(uses)
	n := 0
	for _, u := range uses {
		e := u.e
		if e.parent == nil {
			continue // the root's
		}
		if e.inHidden() {
			// Hidden, it has no box to go from or to, and shows again
			// where it is then, as it hides, at once.
			delete(rt.trans, e.id)
			e.st.trec = nil
			continue
		}
		r := rt.trans[e.id]
		if r == nil {
			r = &transition{}
			rt.trans[e.id] = r
		} else if r.built == rt.frame {
			if r.elem == e {
				r.t = u.t // the element's last Transition counts
			}
			continue
		}
		r.fresh = r.laid+1 != rt.frame
		r.t, r.elem, r.parent, r.built = u.t, e, e.parent.id, rt.frame
		r.next = tbox{e.x, e.y, e.w, e.h, 1, 1, 0, 0}
		if g := r.ghost; g != nil {
			// Built again as a copy of it was going: it comes back from
			// where the copy shows, as fast as it went.
			r.ghost, r.fresh, r.collapse = nil, false, 0
			r.head(r.next, moveAll, now)
		}
		e.st.trec = r
		if u.t.Exit != nil || u.t.Fly && u.t.Match != nil {
			// The next frame copies it if it goes: to go as its Exit
			// says, or to fly with the element taking its Match.
			rt.exitsBuilt = true
			if u.t.Exit != nil && e.parent == c.overlay {
				rt.overlayExits = true
			}
		}
		u.r = r
		uses[n] = u
		n++
	}
	holding := rt.holding[:0]
	for _, u := range uses[:n] {
		e, r := u.e, u.r
		next := r.next
		// An element appearing in a parent that was there comes in;
		// whole new parts of the interface, as a page, appear at once.
		// The overlay layer is the window's, though built only while it
		// holds something: what appears in it comes in as in the root.
		parent := e.parent
		if parent == c.overlay {
			parent = root
		}
		appears := r.fresh && !resized && e.st.born == rt.frame && parent.st.born != rt.frame
		if r.held || appears && r.t.AfterExits {
			// Unseen until the copies of its siblings are gone, which
			// animateExits makes: then it comes in.
			r.held = true
			r.rest(next, now)
			r.target, r.laid, r.shown = next, rt.frame, next
			holding = append(holding, u)
			continue
		}
		switch {
		case r.fresh:
			r.rest(next, now)
			if appears {
				r.enter(e, next, still, now)
			}
		case resized || still && r.moving&^moveAlpha != 0:
			r.rest(next, now)
		default:
			props := r.properties()
			// The groups of channels the Transition animates: an axis of
			// the size it does not choose follows the layout at once.
			var own, changed uint8
			if props&propPosition != 0 {
				own |= movePos
				if next.x != r.target.x || next.y != r.target.y {
					changed |= movePos
				}
			}
			if props&propWidth != 0 {
				own |= moveWidth
			}
			if props&propHeight != 0 {
				own |= moveHeight
			}
			if own&moveWidth != 0 && next.w != r.target.w || own&moveHeight != 0 && next.h != r.target.h {
				// Both axes chosen (Size), either changing heads both.
				changed |= own & moveSize
			}
			if still {
				changed = 0
			}
			if r.laid+1 == rt.frame {
				// Not back as its copy went: that heads for next already.
				r.follow(next, own)
			}
			if changed != 0 {
				r.head(next, changed, now)
				r.entering &^= changed
			}
		}
		r.target, r.laid = next, rt.frame
		rt.show(e, r, now)
	}
	if rt.matches != nil {
		rt.matches.claim()
	}
	rt.animateExits(root, now, still, resized)
	for _, u := range holding {
		e, r := u.e, u.r
		if e.parent.ghosts {
			e.flags |= flagInvisible
			if rt.focused == e.id {
				// Unseen, it takes no focus yet, but keeps it for later.
				rt.focused, r.focus = 0, true
			}
			continue
		}
		r.held = false
		if r.focus {
			r.focus = false
			if rt.focused == 0 {
				// Unless the focus went elsewhere meanwhile; the next
				// frame builds it focused.
				e.Focus()
				rt.animating = true
			}
		}
		if !resized {
			r.enter(e, r.next, still, now)
		}
		rt.show(e, r, now)
	}
	clear(holding)
	rt.holding = holding[:0]
}

// enter brings e in from where its Enter says, to next, unless it has
// none or, without motion (still), it would only move.
func (r *transition) enter(e *node, next tbox, still bool, now time.Time) {
	m := r.t.Enter
	if m == nil || still && !m.fades() {
		return
	}
	m = m.flippedFor(e)
	var axis uint8
	if !still {
		axis = collapseAxis(m, e.parent)
	}
	r.rest(next.moved(m, axis, still), now)
	r.collapse = axis
	r.head(next, moveAll, now)
	r.entering = moveAll
}

// show shows e where r's channels have it now, on its way to r.target.
func (rt *engine) show(e *node, r *transition, now time.Time) {
	next := r.target
	shown := next
	if r.moving != 0 {
		shown = r.sample(next, now)
	}
	if r.moving != 0 {
		rt.animating = true
	} else {
		r.rest(next, now)
	}
	r.shown = shown
	r.apply(e, shown)
}

// showAt shows e in the window's box r, its content laid out at r's size
// as a Transition's Size lays it out on the way: once the frame's boxes are
// in the window.
func (e *node) showAt(r Rect) {
	if r.W == e.w && r.H == e.h {
		shift(e, r.X-e.x, r.Y-e.y)
		return
	}
	layoutBox(e, r.W, r.H)
	e.x, e.y = r.X, r.Y
	place(e, 0, 0)
}

// byDepth returns the transitions asked for in the order of their
// elements' depth, those of one depth in the order asked: a counting sort,
// into a slice the context reuses.
func (c *context) byDepth(uses []transitionUse) []transitionUse {
	deepest := 0
	for i := range uses {
		deepest = max(deepest, uses[i].e.depth)
	}
	starts := c.depthStarts
	if cap(starts) < deepest+2 {
		starts = make([]int, deepest+2)
	} else {
		starts = starts[:deepest+2]
		clear(starts)
	}
	for i := range uses {
		starts[uses[i].e.depth+1]++
	}
	for d := 1; d < len(starts); d++ {
		starts[d] += starts[d-1]
	}
	sorted := slices.Grow(c.sortedUses[:0], len(uses))[:len(uses)]
	for _, u := range uses {
		sorted[starts[u.e.depth]] = u
		starts[u.e.depth]++
	}
	c.depthStarts, c.sortedUses = starts, sorted
	return sorted
}

// apply shows e at s.
func (r *transition) apply(e *node, s tbox) {
	if s.w != e.w || s.h != e.h {
		switch {
		case r.collapse != 0:
			// Collapsing: the content keeps its layout, clipped.
			e.w, e.h = max(s.w, 0), max(s.h, 0)
			e.flags |= flagClip
		case r.t.ScaleContent && e.c.rt.matches != nil:
			// Laid out at its size, and scaled to s.
			e.c.rt.matches.scale(e, max(s.w, 0), max(s.h, 0))
		default:
			layoutBox(e, max(s.w, 0), max(s.h, 0))
		}
	}
	e.x, e.y = s.x, s.y
	if s.alpha < 1 {
		o := float32(1)
		if e.opacitySet {
			o = e.opacity
		}
		e.opacity, e.opacitySet = o*max(s.alpha, 0), true
	}
	if t, ok := s.turn(); ok {
		e.transform = e.transform.Then(t)
	}
	if s.blur > 0 {
		e.blur = addBlur(e.blur, s.blur)
	}
}

// addBlur returns the sigma of a blur by a after one by b, as Gaussians
// compose.
func addBlur(a, b float32) float32 { return float32(math.Hypot(float64(a), float64(b))) }

// turn returns the transform of s's scale and rotation, unless it has
// none.
func (s tbox) turn() (Transform, bool) {
	if s.scale == 1 && s.rot == 0 {
		return Transform{}, false
	}
	return Scaling(s.scale, s.scale).Then(Rotation(s.rot)), true
}

// animateExits moves the copies of the elements gone with an Exit, under
// their parents, and copies those that just went, from the frame before,
// which the engine kept for them.
func (rt *engine) animateExits(root *node, now time.Time, still, instant bool) {
	var byID map[uint64]*node
	going := rt.going[:0]
	for id, r := range rt.trans {
		if r.built == rt.frame {
			continue
		}
		if r.ghost == nil && (r.t.Exit == nil || r.held || instant && rt.instant() || r.laid+1 != rt.frame) {
			delete(rt.trans, id)
			continue
		}
		if byID == nil {
			byID = rt.elementsByID(root)
		}
		p := byID[r.parent]
		if p == nil || p.leaving != 0 {
			// Its parent went too.
			delete(rt.trans, id)
			continue
		}
		if r.ghost == nil {
			if m, ok := rt.c.childExit(p); ok {
				// Its parent says where the children going now go.
				r.t.Exit = &m
			}
			if still && !r.t.Exit.fades() || r.exitsAtOnce() {
				delete(rt.trans, id)
				continue
			}
			e := r.elem
			r.t.Exit = r.t.Exit.flippedFor(e)
			r.ghost = ghostOf(e)
			r.elem = nil
			r.ghost.leaving = 1
			if e.flags&flagAbsolute != 0 {
				r.ghost.leaving = 2
			}
			r.ghost.flags |= flagAbsolute
			r.opacity = 1
			if e.opacitySet {
				r.opacity = e.opacity / max(r.shown.alpha, 0.001)
			}
			r.turn = r.ghost.transform
			// Its own blur, without its transition's.
			r.blur = float32(math.Sqrt(max(0, float64(e.blur)*float64(e.blur)-float64(r.shown.blur)*float64(r.shown.blur))))
			r.gone, r.collapse = r.shown, 0
			if !still {
				r.collapse = collapseAxis(r.t.Exit, p)
			}
			// From where it showed, as fast as it went, or as its
			// ExitDuration says.
			r.headWith(r.gone.moved(r.t.Exit, r.collapse, still), moveAll, r.exitAnimation(), now)
			// The frame it went from is still whole.
			r.index = 0
			for c := e.parent.first; c != nil && c != e; c = c.next {
				r.index++
			}
		}
		going = append(going, exit{id, r, p})
	}
	// The copies go under their parents in the order the elements had,
	// not the map's, which changes from run to run: an insertion sort, as
	// few go at once.
	for i := 1; i < len(going); i++ {
		for j := i; j > 0 && going[j].before(going[j-1]); j-- {
			going[j], going[j-1] = going[j-1], going[j]
		}
	}
	for _, x := range going {
		id, r, p := x.id, x.r, x.p
		s := r.sample(r.gone.moved(r.t.Exit, r.collapse, still), now)
		if r.moving == 0 {
			delete(rt.trans, id)
			continue
		}
		g := r.ghost
		g.next = nil
		p.add(g)
		p.ghosts = true
		if p == rt.c.overlay {
			rt.overlayExits = true
		}
		layoutBox(g, r.gone.w, r.gone.h)
		if s.w != r.gone.w || s.h != r.gone.h {
			g.w, g.h = max(s.w, 0), max(s.h, 0)
			g.flags |= flagClip
		}
		g.x, g.y = s.x, s.y
		g.opacity, g.opacitySet = r.opacity*max(s.alpha, 0), true
		g.transform = r.turn
		if t, ok := s.turn(); ok {
			g.transform = g.transform.Then(t)
		}
		g.blur = r.blur
		if s.blur > 0 {
			g.blur = addBlur(r.blur, s.blur)
		}
		r.shown = s
		rt.animating = true
	}
	clear(going)
	rt.going = going[:0]
}

// exit is a transition whose element's copy goes under p.
type exit struct {
	id uint64
	r  *transition
	p  *node
}

// before tells whether x's copy goes before y's under a parent.
func (x exit) before(y exit) bool {
	if x.r.index != y.r.index {
		return x.r.index < y.r.index
	}
	return x.id < y.id
}

// elementsByID returns the elements of the frame by ID, in a map the
// engine reuses.
func (rt *engine) elementsByID(root *node) map[uint64]*node {
	if rt.byID == nil {
		rt.byID = map[uint64]*node{}
	}
	clear(rt.byID)
	var walk func(e *node)
	walk = func(e *node) {
		rt.byID[e.id] = e
		for c := e.first; c != nil; c = c.next {
			walk(c)
		}
	}
	walk(root)
	return rt.byID
}

// ghostOf copies e, an element of the frame before, and the elements inside
// it, for its exit transition. The copy only shows: it refers to no other
// element nor to the app's state, has states of its own, and is inert.
func ghostOf(e *node) *node {
	g := new(node)
	*g = *e
	st := *e.st
	st.trec = nil
	// Its layer too (effectLayer), as an element taking the state in its
	// place paints into its own: a canvas shows one content a frame.
	st.effectLayer = nil
	// Its spans, and the styles they paint with, are its own: another
	// element taking its state in its place, in this frame, made the
	// state's its own already. A paragraph's spans are in a buffer of the
	// state (inlineSpans).
	g.spans, st.spans = slices.Clone(e.spans), nil
	if g.spans != nil {
		st.spans = &spanCache{}
		st.spans.update(g.spans)
	}
	g.st = &st
	g.parent, g.first, g.last, g.next, g.nchild = nil, nil, nil, nil, 0
	g.shadows, g.cols, g.rows, g.frags = slices.Clone(e.shadows), slices.Clone(e.cols), slices.Clone(e.rows), slices.Clone(e.frags)
	g.track, g.popover, g.list, g.rowsOf, g.overflow = nil, nil, nil, nil, nil
	g.activeDescendant, g.nameFrom, g.focusTo, g.field, g.form = nil, nil, nil, nil, nil
	g.describedBy, g.errorMessage = nil, nil
	g.followX, g.colFit, g.gridFit = nil, nil, nil
	// It no longer flies (Fly), if it did: it goes as its parent's copy.
	g.inputFn, g.observeFn, g.ghosts, g.lift = nil, nil, false, false
	if g.themed != nil {
		// The app may change its theme while the copy leaves.
		t := *g.themed
		g.themed = &t
	}
	g.poseSet = false // it keeps the pose it had
	g.tl, g.nmeasure, g.leafOK, g.intrOK, g.lay = nil, 0, false, 0, 0
	g.flags = g.flags&^(flagModal|flagPage|flagFocusTarget) | flagInert
	for c := e.first; c != nil; c = c.next {
		if c.leaving == 0 {
			g.add(ghostOf(c))
		}
	}
	return g
}

// animateColors moves e's background and border colors, once its styleFn
// set them, as its Transition asks: even while the desktop asks for less
// motion, as changing colors moves nothing.
func (rt *engine) animateColors(e *node, r *transition) {
	if r.properties()&propColors == 0 {
		return
	}
	now := rt.c.now
	if r.colorFrame != rt.frame {
		target := [2]Color{e.bg, e.borderC}
		switch {
		case !r.colorSet || r.colorFrame+1 != rt.frame:
			// Not painted in the frame before: its colors show at once.
			r.colorMoving = false
		case target != r.colorTarget && rt.instant():
			r.colorMoving = false
		case target != r.colorTarget:
			r.colorFrom, r.colorMoving = r.colorShown, true
			r.colorK = channel{at: now}
			headFor(&r.colorK, 1, r.animation(), now)
		}
		r.colorSet, r.colorTarget, r.colorFrame = true, target, rt.frame
		r.colorShown = target
		if r.colorMoving {
			r.colorK.advance(now)
			if !r.colorK.moving && !r.colorK.pending {
				r.colorMoving = false
			} else {
				k := max(0, min(float32(r.colorK.value), 1))
				lab := r.animation().Oklab
				r.colorShown = [2]Color{mixPremultiplied(r.colorFrom[0], target[0], k, lab), mixPremultiplied(r.colorFrom[1], target[1], k, lab)}
				rt.animating = true
			}
		}
	}
	e.bg, e.borderC = r.colorShown[0], r.colorShown[1]
}

// animateShadows is engine.animateShadows once a view asks for a
// Transition, so that apps without one do not link it.
var (
	animateShadows func(rt *engine, e *node, r *transition)
	linkShadows    sync.Once
)

// animateShadows moves e's shadows, once its styleFn ran, as its
// Transition asks and as animateColors moves its colors: each shadow
// from the one in its place in the frame before, a missing one from a
// transparent one at no offset, as CSS interpolates box-shadow lists.
// Lists with an inner shadow where the other has an outer one change at
// once.
func (rt *engine) animateShadows(e *node, r *transition) {
	if r.properties()&propShadows == 0 {
		return
	}
	now := rt.c.now
	if r.shadowFrame != rt.frame {
		switch {
		case !r.shadowSet || r.shadowFrame+1 != rt.frame:
			r.shadowMoving = false
		case slices.Equal(e.shadows, r.shadowTarget):
		case rt.instant() || !shadowsMix(r.shadowShown, e.shadows):
			r.shadowMoving = false
		default:
			r.shadowFrom, r.shadowMoving = append(r.shadowFrom[:0], r.shadowShown...), true
			r.shadowK = channel{at: now}
			headFor(&r.shadowK, 1, r.animation(), now)
		}
		r.shadowSet, r.shadowFrame = true, rt.frame
		r.shadowTarget = append(r.shadowTarget[:0], e.shadows...)
		r.shadowShown = append(r.shadowShown[:0], e.shadows...)
		if r.shadowMoving {
			r.shadowK.advance(now)
			if !r.shadowK.moving && !r.shadowK.pending {
				r.shadowMoving = false
			} else {
				k := max(0, min(float32(r.shadowK.value), 1))
				r.shadowShown = mixShadows(r.shadowShown[:0], r.shadowFrom, r.shadowTarget, k, r.animation().Oklab)
				rt.animating = true
			}
		}
	}
	if r.shadowMoving {
		e.shadows = append(e.shadows[:0], r.shadowShown...)
	}
}

// shadowsMix reports whether the shadows a and b move from one to the
// other: those in the same place in both are inner or outer alike.
func shadowsMix(a, b []shadow) bool {
	for i := range min(len(a), len(b)) {
		if a[i].inset != b[i].inset {
			return false
		}
	}
	return true
}

// mixShadows appends to dst the shadows k of the way from a to b, the
// shorter list taken on with transparent shadows at no offset.
func mixShadows(dst, a, b []shadow, k float32, lab bool) []shadow {
	for i := range max(len(a), len(b)) {
		var x, y shadow
		switch {
		case i >= len(a):
			y = b[i]
			x.inset = y.inset
		case i >= len(b):
			x = a[i]
			y.inset = x.inset
		default:
			x, y = a[i], b[i]
		}
		dst = append(dst, shadow{
			x:      lerp(x.x, y.x, k),
			y:      lerp(x.y, y.y, k),
			blur:   max(0, lerp(x.blur, y.blur, k)),
			spread: lerp(x.spread, y.spread, k),
			color:  mixPremultiplied(x.color, y.color, k, lab),
			inset:  x.inset,
		})
	}
	return dst
}

// forgetColors shows the colors of the elements at once: a route change is
// where an element that comes back shows its own colors, not the color of
// the element that took its place while it was away.
func (rt *engine) forgetColors() {
	for _, r := range rt.trans {
		r.colorSet, r.shadowSet = false, false
	}
}

// mixPremultiplied returns the color k of the way from a to b, mixed with
// premultiplied alpha, so that a color fading from transparent keeps its
// hue, in Oklab if lab (Animation.Oklab), else in sRGB.
func mixPremultiplied(a, b Color, k float32, lab bool) Color {
	aa, ba := float32(a.A)/255, float32(b.A)/255
	alpha := lerp(aa, ba, k)
	if alpha <= 0 {
		return Color{}
	}
	if lab || a.wide.ok() || b.wide.ok() {
		// Premultiplied, b weighs this much of the straight colors.
		var c Color
		if lab {
			c = mixLab(a, b, k*ba/alpha)
		} else {
			c = mixWide(a, b, k*ba/alpha)
		}
		c.A = alphaByte(alpha)
		return c
	}
	ch := func(x, y uint8) uint8 {
		v := lerp(float32(x)*aa, float32(y)*ba, k) / alpha
		return uint8(max(0, min(v+0.5, 255)))
	}
	return Color{R: ch(a.R, b.R), G: ch(a.G, b.G), B: ch(a.B, b.B), A: alphaByte(alpha)}
}
