package gpu

import (
	"cmp"
	"image"
	"slices"

	"github.com/egoist/mygo/internal/raster"
	"github.com/egoist/mygo/internal/scene"
)

// clipMaskSlot is a retained clip mask. Masks outlive the frames that
// don't use them, up to idleMaskBytes of them, so a transform that comes
// and goes finds its masks again instead of rasterizing them.
type clipMaskSlot struct {
	// shapes are the clips the mask is of, their stops in stops, and area
	// the frame pixels it covers: the clip's scissor rectangle, or more
	// for a clip that grows (maskArea).
	shapes []scene.ClipShape
	stops  []scene.Stop
	area   image.Rectangle
	image  *scene.Image
	// frame is the last frame that used the mask, and slot the clip's
	// place among that frame's masked clips.
	frame uint64
	slot  int
}

// idleMaskBytes is how much memory the masks no clip of the frame uses
// keep, the least recently used dropped first.
const idleMaskBytes = 32 << 20

func (b *Builder) prepare(in *Instance, m scene.Affine, c *clip) bool {
	b.preparedMask = 0
	if c.scissor.Empty() || in.Rect[2] <= 0 || in.Rect[3] <= 0 {
		return false
	}
	if _, ok := m.Inverse(); !ok {
		return false
	}
	in.Transform0, in.Transform1, in.Transform2 = [4]float32{1, 0, 0, 0}, [4]float32{0, 1, 0, 0}, [4]float32{}
	if m.Set {
		in.Transform0 = [4]float32{m.A, m.C, m.X, 1}
		in.Transform1 = [4]float32{m.B, m.D, m.Y, 0}
		in.Transform2 = [4]float32{m.G, m.H, 0, 0}
		if m.Behind {
			in.Transform2[2] = 1 // the divisor's constant term is -1
		}
	}
	in.Clip, in.ClipRadii = rect(c.rect), c.radii
	if !c.masked {
		return true
	}
	// A hard clip sharing the drawing's coordinates is cheap to evaluate in
	// the fragment shader. Keep the rounded ancestor's mask stationary even
	// when a text input or custom clipped drawing moves within it. Drawing
	// that a translation of the clip's moved shares them too, folded into
	// the frame's (scene.TranslationFolder): the clip's rectangle moves.
	// So does drawing that nothing transforms, or that a translation only
	// moves, in a clip in the frame's coordinates (as a clip only moved
	// is, scene.MovedClip), whose rectangle moves the other way: as a
	// pane reaching into the rounded corners of the frame around it
	// (markEnclosing), which then needs no mask of its own.
	shared, dx, dy := c.shape.Transform == m && m.Set, float32(0), float32(0)
	if tx, ty, moved := m.Translation(); !c.shape.Transform.Set && (!m.Set || moved) {
		dx, dy, shared = -tx, -ty, true
	}
	if c.shape.Radii == [4]float32{} && !c.shape.Masks() && shared && len(b.stack) > 1 {
		parent := &b.stack[len(b.stack)-2]
		if parent.masked {
			b.preparedMask = b.clipTexture(parent, b.clipShapes[:len(b.clipShapes)-1])
			if b.preparedMask == 0 {
				return false
			}
			in.Clip = rect(c.rect)
			in.Clip[0], in.Clip[1] = in.Clip[0]+dx, in.Clip[1]+dy
			in.ClipRadii = areaRect(parent.maskArea)
			in.Transform1[3] = 2
			return true
		}
	}
	b.preparedMask = b.clipTexture(c, b.clipShapes)
	if b.preparedMask == 0 {
		return false
	}
	in.Clip = areaRect(c.maskArea)
	in.ClipRadii = [4]float32{}
	in.Transform1[3] = 1
	return true
}

func (b *Builder) clipTexture(c *clip, shapes []scene.ClipShape) uintptr {
	if c.maskTexture != 0 {
		return c.maskTexture
	}
	area := image.Rect(int(c.scissor.Left), int(c.scissor.Top), int(c.scissor.Right), int(c.scissor.Bottom))
	// The mask of the same clip, in this frame or an earlier one; else
	// the one the clip in its place had last frame, which it most likely
	// is, changed; else a new one.
	found, reuse := -1, -1
	for i := range b.maskSlots {
		m := &b.maskSlots[i]
		// A mask of more of the frame than the clip's scissor rectangle
		// holds the same coverage there.
		if area.In(m.area) && slices.EqualFunc(m.shapes, shapes, func(a, b scene.ClipShape) bool { return a.Same(&b) }) {
			found = i
			break
		}
		if reuse < 0 && m.frame == b.frame-1 && m.slot == c.maskSlot {
			reuse = i
		}
	}
	if found < 0 {
		found = reuse
		if found < 0 {
			found = len(b.maskSlots)
			b.maskSlots = append(b.maskSlots, clipMaskSlot{})
		}
		m := &b.maskSlots[found]
		m.area = maskArea(area, m.image, b.stack[0].scissor)
		m.image = raster.ClipMask(m.image, shapes, m.area)
		m.keep(shapes)
	}
	m := &b.maskSlots[found]
	m.frame, m.slot = b.frame, c.maskSlot
	c.maskArea = m.area
	c.maskTexture = b.imageTexture(m.image)
	return c.maskTexture
}

// maskArea returns the frame pixels to rasterize a clip's mask into,
// for a clip whose scissor rectangle is area, given had, the mask the
// clip in its place had last frame. A new mask covers area. Otherwise
// the mask keeps had's size, placed where area fits in it, so that a
// clip that changes reuses its mask's memory and texture instead of
// allocating both again on every frame of its animation. A card growing
// out of a cover to fill the window allocated 190 MB in 40 frames
// (egoist/mygo-music#55). A side that outgrows it doubles, up to the
// frame's size, and a clip that shrinks to less than a quarter of it
// takes its own size.
func maskArea(area image.Rectangle, had *scene.Image, frame Scissor) image.Rectangle {
	if had == nil || had.W <= 0 || had.H <= 0 {
		return area
	}
	w, h := area.Dx(), area.Dy()
	if 4*w*h < had.W*had.H {
		return area
	}
	grow := func(n, was, most int) int {
		if n <= was {
			return was
		}
		return max(n, min(2*was, most))
	}
	w, h = grow(w, had.W, int(frame.Right-frame.Left)), grow(h, had.H, int(frame.Bottom-frame.Top))
	// Within the frame where it fits, rather than rasterizing what is off
	// it.
	place := func(lo, hi, n int, end int32) int {
		return max(min(lo, int(end)-n), hi-n)
	}
	x, y := place(area.Min.X, area.Max.X, w, frame.Right), place(area.Min.Y, area.Max.Y, h, frame.Bottom)
	return image.Rect(x, y, x+w, y+h)
}

// areaRect returns area, a mask's frame pixels, as the shaders take it:
// its origin and size, which map frame pixels to the mask's texels.
func areaRect(area image.Rectangle) [4]float32 {
	return [4]float32{float32(area.Min.X), float32(area.Min.Y), float32(area.Dx()), float32(area.Dy())}
}

// keep keeps shapes as the mask's, with copies of their stops, which
// the scene's are not from frame to frame.
func (m *clipMaskSlot) keep(shapes []scene.ClipShape) {
	m.shapes = append(m.shapes[:0], shapes...)
	m.stops = m.stops[:0]
	for _, c := range shapes {
		m.stops = append(m.stops, c.Stops...)
	}
	at := 0
	for i := range m.shapes {
		if st := m.shapes[i].Stops; st != nil {
			m.shapes[i].Stops = m.stops[at : at+len(st) : at+len(st)]
			at += len(st)
		}
	}
}

// dropIdleMasks drops the least recently used masks this frame didn't use
// beyond idle bytes of them.
func (b *Builder) dropIdleMasks(idle int) {
	slices.SortFunc(b.maskSlots, func(x, y clipMaskSlot) int { return cmp.Compare(y.frame, x.frame) })
	for i := range b.maskSlots {
		m := &b.maskSlots[i]
		if m.frame == b.frame {
			continue
		}
		// The memory a mask keeps, which may be more than its pixels
		// take up: Resize reuses it for smaller ones.
		if idle -= cap(m.image.Pix); idle < 0 {
			clear(b.maskSlots[i:])
			b.maskSlots = b.maskSlots[:i]
			return
		}
	}
}
