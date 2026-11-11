package ui

import (
	"io"
	"reflect"
)

// closeWithState has st close v, a Local it just made, as it goes, when v
// is an io.Closer or may hold one: T or *T has Close, or T is an
// interface. Other types cost nothing, at their making or as they go.
func closeWithState[T any](st *state, v *T) {
	var zero T
	_, byPtr := any(v).(io.Closer)
	_, byValue := any(zero).(io.Closer)
	if !byPtr && !byValue && any(zero) != nil {
		return // neither a Closer nor an interface that may hold one
	}
	st.closes = append(st.closes, func() error {
		if c, ok := any(v).(io.Closer); ok {
			return c.Close()
		}
		if c, ok := any(*v).(io.Closer); ok && !isNil(c) {
			return c.Close()
		}
		return nil
	})
}

// isNil reports whether c holds a nil pointer, map, channel, function or
// slice, which is not closed.
func isNil(c io.Closer) bool {
	switch v := reflect.ValueOf(c); v.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Chan, reflect.Func, reflect.Slice, reflect.Interface, reflect.UnsafePointer:
		return v.IsNil()
	}
	return false
}

// exitCloses are the Closers of a state dropped while a copy of its element
// leaves with an exit transition (the element's own, or an ancestor's):
// they close once rec's copy is gone.
type exitCloses struct {
	id     uint64 // the ID of the element whose transition rec is
	rec    *transition
	ghost  *node
	closes []func() error
}

// dropCloses takes the Closers of s, whose state prune drops: they close
// after the frame (closeDropped), or once the copy of the element leaving
// with an exit transition is gone.
func (rt *engine) dropCloses(s *state) {
	closes := s.closes
	s.closes = nil
	for id, r := range rt.trans {
		if g := r.ghost; g != nil && r.built != rt.frame && holds(g, s.id) {
			rt.exits = append(rt.exits, exitCloses{id, r, g, closes})
			return
		}
	}
	rt.dropped = append(rt.dropped, closes...)
}

// holds reports whether the copy g of an element going, with its
// children's, has one of the element id.
func holds(g *node, id uint64) bool {
	if g.id == id {
		return true
	}
	for c := g.first; c != nil; c = c.next {
		if holds(c, id) {
			return true
		}
	}
	return false
}

// exitsEnded moves the Closers of the copies whose exits ended, or whose
// elements came back, to those closing after the frame.
func (rt *engine) exitsEnded() {
	n := 0
	for _, x := range rt.exits {
		if r := rt.trans[x.id]; r == x.rec && r.ghost == x.ghost {
			rt.exits[n] = x
			n++
			continue
		}
		rt.dropped = append(rt.dropped, x.closes...)
	}
	clear(rt.exits[n:])
	rt.exits = rt.exits[:n]
}

// closeDropped closes the Locals of the states the frame dropped, on the
// main thread once the frame ended, the last made of a state first, as
// deferred calls run. A Close that panics leaves those after it to the end
// of the next frame.
func (rt *engine) closeDropped() {
	for n := len(rt.dropped); n > 0; n = len(rt.dropped) {
		f := rt.dropped[n-1]
		rt.dropped[n-1] = nil
		rt.dropped = rt.dropped[:n-1]
		f()
	}
}

// closeLocals closes the Locals of every state as the window closes, with
// those of copies still leaving.
func (rt *engine) closeLocals() {
	for _, s := range rt.states {
		if s.closes != nil {
			rt.dropped = append(rt.dropped, s.closes...)
			s.closes = nil
		}
	}
	for _, x := range rt.exits {
		rt.dropped = append(rt.dropped, x.closes...)
	}
	rt.exits = nil
	rt.closeDropped()
}
