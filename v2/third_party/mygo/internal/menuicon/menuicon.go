// Package menuicon turns the PNG images of menu items into the pixels a
// backend that draws them itself needs: premultiplied BGRA, as Windows'
// 32-bit DIB sections (PARGB) take them, scaled to the menu's size.
package menuicon

import (
	"bytes"
	"image"
	"image/png"
)

// Image is a decoded image, its pixels premultiplied 0xAARRGGBB, row after
// row from the top.
type Image struct {
	W, H int
	Px   []uint32
}

// Decode decodes a PNG image.
func Decode(data []byte) (*Image, error) {
	src, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return FromImage(src), nil
}

// FromImage copies an image's pixels.
func FromImage(src image.Image) *Image {
	r := src.Bounds()
	img := &Image{W: r.Dx(), H: r.Dy(), Px: make([]uint32, r.Dx()*r.Dy())}
	i := 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			// RGBA is premultiplied, 16 bits a channel.
			cr, cg, cb, ca := src.At(x, y).RGBA()
			img.Px[i] = ca>>8<<24 | cr>>8<<16 | cg>>8<<8 | cb>>8
			i++
		}
	}
	return img
}

// Scale returns the image at w by h pixels, each pixel the average of the
// part of the image it covers, which keeps fine lines of a large image
// as a box filter does when it shrinks, and draws it in blocks when it
// grows.
func (img *Image) Scale(w, h int) *Image {
	if w <= 0 || h <= 0 || img.W == 0 || img.H == 0 {
		return &Image{}
	}
	if w == img.W && h == img.H {
		return img
	}
	xs, ys := spans(img.W, w), spans(img.H, h)
	out := &Image{W: w, H: h, Px: make([]uint32, w*h)}
	for dy, ry := range ys {
		for dx, rx := range xs {
			var a, r, g, b, total float64
			for _, sy := range ry {
				row := img.Px[sy.i*img.W:]
				for _, sx := range rx {
					k := sx.w * sy.w
					p := row[sx.i]
					a += k * float64(p>>24)
					r += k * float64(p>>16&0xFF)
					g += k * float64(p>>8&0xFF)
					b += k * float64(p&0xFF)
					total += k
				}
			}
			out.Px[dy*w+dx] = channel(a/total)<<24 | channel(r/total)<<16 | channel(g/total)<<8 | channel(b/total)
		}
	}
	return out
}

// Tint returns the image in one color, 0xRRGGBB, keeping its alpha: a
// template image in the color of the menu's text.
func (img *Image) Tint(rgb uint32) *Image {
	out := &Image{W: img.W, H: img.H, Px: make([]uint32, len(img.Px))}
	cr, cg, cb := rgb>>16&0xFF, rgb>>8&0xFF, rgb&0xFF
	for i, p := range img.Px {
		a := p >> 24
		out.Px[i] = a<<24 | (cr*a+127)/255<<16 | (cg*a+127)/255<<8 | (cb*a+127)/255
	}
	return out
}

// Over returns a premultiplied pixel drawn over an opaque one, 0x00RRGGBB,
// at an opacity from 0 to 1.
func Over(src, dst uint32, opacity float64) uint32 {
	a := float64(src>>24) / 255 * opacity
	mix := func(shift uint) uint32 {
		s, d := float64(src>>shift&0xFF)*opacity, float64(dst>>shift&0xFF)
		return channel(s+d*(1-a)) << shift
	}
	return mix(16) | mix(8) | mix(0)
}

func channel(v float64) uint32 {
	switch {
	case v <= 0:
		return 0
	case v >= 255:
		return 255
	}
	return uint32(v + 0.5)
}

// span is a source pixel and how much of it a destination pixel covers.
type span struct {
	i int
	w float64
}

// spans returns, for each of n destination pixels, the source pixels of a
// row (or column) of size pixels it covers, and by how much.
func spans(size, n int) [][]span {
	out := make([][]span, n)
	scale := float64(size) / float64(n)
	for d := range out {
		lo, hi := float64(d)*scale, float64(d+1)*scale
		for s := int(lo); s < size && float64(s) < hi; s++ {
			w := min(hi, float64(s+1)) - max(lo, float64(s))
			if w > 1e-9 {
				out[d] = append(out[d], span{s, w})
			}
		}
	}
	return out
}
