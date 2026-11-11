package terminal

import (
	"image"
	"slices"

	"github.com/egoist/mygo/plugins/terminal/internal/vt"
	"github.com/egoist/mygo/ui"
)

// Layers of images (Kitty graphics), as Ghostty draws them, by their z.
const (
	belowBackground = iota // z below -2^30
	belowText              // negative z
	aboveText
)

// shownImage is a placement of an image as the view draws it: the part of
// its bitmap, where it goes from the grid's origin in device pixels, and
// its layer.
type shownImage struct {
	bmp        *ui.Bitmap
	src        ui.Rect
	x, y, w, h int
	z          int32
	layer      int
}

// cachedImage is an image's bitmap, as of its generation.
type cachedImage struct {
	gen  uint64
	bmp  *ui.Bitmap
	seen bool
}

// readImages reads the placements that show, the virtual ones that
// placeholder cells may show, and the bitmaps of their images, which it
// keeps while placements show them; t.mu is held.
func (v *view) readImages(term *vt.Terminal) {
	v.images = v.images[:0]
	clear(v.virtual)
	for _, c := range v.bitmaps {
		c.seen = false
	}
	term.Placements(func(pl vt.Placement) {
		if pl.Virtual {
			bmp := v.bitmap(term, pl.ImageID)
			if bmp == nil {
				return
			}
			if v.virtual == nil {
				v.virtual = map[uint32][]virtualImage{}
			}
			w, h := bmp.Size()
			v.virtual[pl.ImageID] = append(v.virtual[pl.ImageID], virtualImage{
				id: pl.ID, bmp: bmp, imgW: w, imgH: h,
				cols: pl.ParamCols, rows: pl.ParamRows, layer: layerOf(pl.Z),
			})
			return
		}
		if !pl.Visible || pl.Width <= 0 || pl.Height <= 0 || pl.SrcW <= 0 || pl.SrcH <= 0 {
			return
		}
		bmp := v.bitmap(term, pl.ImageID)
		if bmp == nil {
			return
		}
		v.images = append(v.images, shownImage{
			bmp: bmp,
			src: ui.Rect{X: float32(pl.SrcX), Y: float32(pl.SrcY), W: float32(pl.SrcW), H: float32(pl.SrcH)},
			x:   pl.Col*v.cellW + pl.XOffset, y: pl.Row*v.cellH + pl.YOffset, w: pl.Width, h: pl.Height,
			z: pl.Z, layer: layerOf(pl.Z),
		})
	})
	for id, c := range v.bitmaps {
		if !c.seen {
			delete(v.bitmaps, id)
		}
	}
	slices.SortStableFunc(v.images, func(a, b shownImage) int { return int(a.z) - int(b.z) })
}

// bitmap returns the bitmap of an image, made again when its pixels
// change, nil while they have yet to arrive; it keeps it for this frame.
func (v *view) bitmap(term *vt.Terminal, id uint32) *ui.Bitmap {
	gen := term.ImageGeneration(id)
	if gen == 0 {
		return nil
	}
	c := v.bitmaps[id]
	if c == nil || c.gen != gen {
		img, ok := term.Image(id)
		if !ok {
			return nil
		}
		if c == nil {
			if v.bitmaps == nil {
				v.bitmaps = map[uint32]*cachedImage{}
			}
			c = &cachedImage{}
			v.bitmaps[id] = c
		}
		c.gen, c.bmp = gen, ui.NewBitmap(decoded(img))
	}
	c.seen = true
	return c.bmp
}

// layerOf returns the layer of a placement's z.
func layerOf(z int32) int {
	switch {
	case z < -(1 << 30):
		return belowBackground
	case z < 0:
		return belowText
	}
	return aboveText
}

// drawImages draws the images of a layer, within the grid.
func (v *view) drawImages(p *ui.Painter, layer int, ox, oy float32) {
	s := v.scale
	grid := ui.Rect{X: ox, Y: oy, W: float32(v.cols*v.cellW) / s, H: float32(v.rows*v.cellH) / s}
	for _, im := range v.images {
		if im.layer != layer {
			continue
		}
		dst := ui.Rect{X: ox + float32(im.x)/s, Y: oy + float32(im.y)/s, W: float32(im.w) / s, H: float32(im.h) / s}
		p.Clip(grid, 0, func() { p.ImageRegion(im.bmp, im.src, dst, ui.ImageOptions{}) })
	}
	v.drawHolders(p, layer, ox, oy, grid)
}

// decoded returns an image's pixels as an image.
func decoded(img vt.Image) *image.NRGBA {
	w, h := img.Width, img.Height
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	px := img.Pixels
	switch img.Format {
	case vt.ImageRGBA:
		copy(out.Pix, px)
	case vt.ImageRGB:
		for i := 0; i < w*h && 3*i+2 < len(px); i++ {
			copy(out.Pix[4*i:], px[3*i:3*i+3])
			out.Pix[4*i+3] = 0xff
		}
	case vt.ImageGrayAlpha:
		for i := 0; i < w*h && 2*i+1 < len(px); i++ {
			g := px[2*i]
			out.Pix[4*i], out.Pix[4*i+1], out.Pix[4*i+2], out.Pix[4*i+3] = g, g, g, px[2*i+1]
		}
	case vt.ImageGray:
		for i := 0; i < w*h && i < len(px); i++ {
			g := px[i]
			out.Pix[4*i], out.Pix[4*i+1], out.Pix[4*i+2], out.Pix[4*i+3] = g, g, g, 0xff
		}
	}
	return out
}
