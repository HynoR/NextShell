package ui

import (
	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/scene"
)

// NativeGlassSpec is how a NativeGlassMaterial asks for the system's glass:
// its style (0 regular, 1 clear) and tint, A 0 for none.
type NativeGlassSpec struct {
	Style int
	Tint  Color
}

// NativeGlassMaterial is a Material drawn by the system where it has
// Liquid Glass (macOS 26 and later), instead of by the renderer: the
// material leaves a hole in the frame and the system's glass view, under
// it, shows what is under the surface, as a ui.WebView's page. The frame
// under the material is what the glass samples, not the elements the frame
// painted.
type NativeGlassMaterial interface {
	Material
	// NativeGlass asks for the system's glass, or reports false for the
	// material's own drawing.
	NativeGlass() (NativeGlassSpec, bool)
}

// noteNativeGlass notes where the frame shows the system's glass of e,
// which the surface places under it (Surface.PlaceGlass).
func (rt *engine) noteNativeGlass(e *node, clip Rect, invisible bool, spec NativeGlassSpec) {
	if invisible || rt.commitHidden {
		return
	}
	b := e.visualRect(Rect{X: e.x, Y: e.y, W: e.w, H: e.h})
	if b.W <= 0 || b.H <= 0 {
		return
	}
	v := intersect(b, clip)
	if v.W <= 0 || v.H <= 0 {
		return
	}
	var tint *platform.Color
	if spec.Tint.A > 0 {
		tint = &platform.Color{R: spec.Tint.R, G: spec.Tint.G, B: spec.Tint.B, A: spec.Tint.A}
	}
	r := e.radius
	rt.glassPlaces = append(rt.glassPlaces, platform.GlassPlacement{
		ID:           e.id,
		Frame:        rectF(b),
		Clip:         rectF(v),
		Radii:        [4]float64{float64(r[0]), float64(r[1]), float64(r[2]), float64(r[3])},
		Style:        spec.Style,
		Tint:         tint,
		CornerRadius: float64(max(r[0], r[1], r[2], r[3])),
	})
}

// placeNativeGlass tells the host where the frame shows the system's glass.
func (rt *engine) placeNativeGlass() {
	if len(rt.glassPlaces) == 0 && !rt.glassShown {
		return
	}
	rt.glassShown = len(rt.glassPlaces) > 0
	rt.host.placeGlass(rt.glassPlaces)
}

// NativeGlassSupported reports whether the window's surface draws the
// system's glass, as macOS 26's NSGlassEffectView does.
func (p *Painter) NativeGlassSupported() bool { return p.rt.host.nativeGlass() }

// Hole leaves a rounded hole in the frame, through which what is under the
// window's surface shows: the page of a ui.WebView, or a system's glass
// view that a NativeGlassMaterial places. Its own ink, as a material's,
// paints nothing there.
func (p *Painter) Hole(r Rect, radii [4]float32) {
	if r.W <= 0 || r.H <= 0 || p.opacity == 0 {
		return
	}
	p.add(scene.Op{Kind: scene.OpHole, Rect: p.snap(r), Radii: p.radii(radii), Continuous: continuousCorners, Opacity: p.opacity})
	if c := intersect(transformRect(p.transform, r), p.clip); c.W > 0 && c.H > 0 {
		p.holes = append(p.holes, c)
	}
}
