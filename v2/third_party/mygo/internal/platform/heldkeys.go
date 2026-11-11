package platform

// HeldKeys keeps the keys whose presses a backend sent the content and
// whose releases it has not, for a backend whose system keeps some
// releases from the window: those an input method takes, those a menu's
// loop takes, and, in a Mac's browsers, those let go of while Command is
// held. The backend lets go of them itself (Lift, LiftAll), so that the
// content never holds a key the user let go of (KeyReleased).
type HeldKeys struct {
	held []heldKey
	// gone are the keys Lift and LiftAll let go of, whose own releases may
	// still come, which Track then keeps from the content.
	gone []heldKey
}

type heldKey struct {
	key, physical Key
	code          KeyCode
}

// same reports whether k is the key at physical, or key where either
// position is unknown.
func (k heldKey) same(key, physical Key) bool {
	if k.physical != KeyUnknown && physical != KeyUnknown {
		return k.physical == physical
	}
	return key != KeyUnknown && k.key == key
}

func indexKey(keys []heldKey, key, physical Key) int {
	for i, k := range keys {
		if k.same(key, physical) {
			return i
		}
	}
	return -1
}

// Track notes ev, an event the backend is about to send the content, and
// reports whether to send it: false for the release of a key Lift or
// LiftAll let go of already.
func (h *HeldKeys) Track(ev SurfaceEvent) bool {
	switch ev.Kind {
	case KeyPressed:
		if i := indexKey(h.gone, ev.Key, ev.PhysicalKey); i >= 0 {
			h.gone = append(h.gone[:i], h.gone[i+1:]...)
		}
		if indexKey(h.held, ev.Key, ev.PhysicalKey) < 0 && (ev.Key != KeyUnknown || ev.PhysicalKey != KeyUnknown) {
			h.held = append(h.held, heldKey{ev.Key, ev.PhysicalKey, ev.Code})
		}
	case KeyReleased:
		if i := indexKey(h.held, ev.Key, ev.PhysicalKey); i >= 0 {
			h.held = append(h.held[:i], h.held[i+1:]...)
			return true
		}
		if i := indexKey(h.gone, ev.Key, ev.PhysicalKey); i >= 0 {
			h.gone = append(h.gone[:i], h.gone[i+1:]...)
			return false
		}
	case SurfaceBlur:
		// The content lets go of every key itself.
		h.held, h.gone = h.held[:0], h.gone[:0]
	}
	return true
}

// Lift sends the release of the key held at physical (or key, where its
// position is unknown), whose own release the system keeps from the
// window, as an input method takes it, through send, which tracks it. It
// does nothing when no such key is held.
func (h *HeldKeys) Lift(key, physical Key, mods Modifiers, send func(SurfaceEvent) bool) {
	i := indexKey(h.held, key, physical)
	if i < 0 {
		return
	}
	k := h.held[i]
	send(SurfaceEvent{Kind: KeyReleased, Key: k.key, PhysicalKey: k.physical, Code: k.code, Mods: mods})
	// send tracked the release, unless the window closed.
	if i := indexKey(h.held, k.key, k.physical); i >= 0 {
		h.held = append(h.held[:i], h.held[i+1:]...)
	}
	h.gone = append(h.gone, k)
}

// LiftAll sends the releases of every key held through send, as a menu's
// loop takes the keyboard.
func (h *HeldKeys) LiftAll(mods Modifiers, send func(SurfaceEvent) bool) {
	for len(h.held) > 0 {
		k := h.held[0]
		h.Lift(k.key, k.physical, mods, send)
	}
}
