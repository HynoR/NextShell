package mygo

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"math"

	"github.com/egoist/mygo/internal/platform"
)

// SVGImage is a vector image that NewMenuIconSVG draws: a *ui.SVG, which
// package ui parses (ui.ParseSVG).
type SVGImage interface {
	// Size returns the size the image says it has.
	Size() (w, h float32)
	// Image draws it to an image of w by h pixels, with current for
	// currentColor.
	Image(w, h int, current color.Color) *image.RGBA
}

// menuIconSVGPixels is the size, in pixels, of the longer side of the image
// NewMenuIconSVG draws an SVG to: four times the box of a menu's icons
// (platform.MenuIconSize), sharp on displays of up to four pixels a point,
// which the backends scale down from, as they do a PNG image.
const menuIconSVGPixels = 4 * platform.MenuIconSize

// NewMenuIconSVG makes an icon for menu items of an SVG, as NewMenuIcon
// does of a PNG image: the *ui.SVG an app shows with ui.Icon, drawn by
// native UI's renderer, with the shapes and paint it draws (see ui.SVG).
// It draws it once, to an image of 64 pixels on its longer side, keeping
// its aspect, which menus fit in their icons' box of 16 by 16 points.
//
// With template, only the shapes' coverage counts, whatever their colors,
// as for ui.Icon: the menu shows them in the color of its text, as
// NewMenuIcon's template images, or in an item's ui.MenuItem.IconColor in
// the menus package ui draws. Without it, the SVG shows in its own colors,
// with black for currentColor. It returns an error for a nil SVG or one
// without a size.
//
//	//go:embed icons/trash.svg
//	var trashSVG []byte
//
//	var trash = ui.MustParseSVG(trashSVG) // also for ui.Icon(c, trash)
//
//	trashIcon, err := mygo.NewMenuIconSVG(trash, true)
func NewMenuIconSVG(s SVGImage, template bool) (*MenuIcon, error) {
	if s == nil {
		return nil, errors.New("mygo: NewMenuIconSVG of a nil SVG")
	}
	w, h := s.Size()
	if !(w > 0 && h > 0) || math.IsInf(float64(w), 0) || math.IsInf(float64(h), 0) {
		return nil, errors.New("mygo: the SVG has no size")
	}
	pw, ph := menuIconSVGPixels, menuIconSVGPixels
	if w >= h {
		ph = max(1, int(math.Round(float64(h*menuIconSVGPixels/w))))
	} else {
		pw = max(1, int(math.Round(float64(w*menuIconSVGPixels/h))))
	}
	var b bytes.Buffer
	if err := png.Encode(&b, s.Image(pw, ph, color.Black)); err != nil {
		return nil, err
	}
	linkMenuIcons()
	menuMu.Lock()
	defer menuMu.Unlock()
	return &MenuIcon{menuIcon(b.Bytes(), template)}, nil
}
