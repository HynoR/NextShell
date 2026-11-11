package ui

import (
	"bytes"
	"image"
	"image/gif"
	"io"
	"time"
)

// gifDecoder decodes an animated GIF's frames, each drawn over what the
// ones before it left, as their disposal says, into one picture.
type gifDecoder struct {
	g    *gif.GIF
	next int
	pic  *image.RGBA
	// saved is the picture before the last frame drew, for a frame that
	// disposes to the previous one; nil for one that does not.
	saved []byte
	// palette is the frame's colors, premultiplied.
	palette [256][4]byte
}

// NewGIFDecoder returns a decoder of an animated GIF's frames, for
// NewVideoPlayer, as browsers show them: a frame of 10 ms or less shows
// for 100 ms. The GIF decodes into memory at once, its frames' pixels a
// byte each, and each frame is drawn as it plays. The player's SetLoop,
// not the GIF's loop count, says whether it plays again.
func NewGIFDecoder(data []byte) (VideoDecoder, error) {
	g, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	w, h := g.Config.Width, g.Config.Height
	if w == 0 || h == 0 {
		// No logical screen: the frames' bounds are the picture's.
		var r image.Rectangle
		for _, f := range g.Image {
			r = r.Union(f.Bounds())
		}
		w, h = r.Max.X, r.Max.Y
	}
	return &gifDecoder{g: g, pic: image.NewRGBA(image.Rect(0, 0, w, h))}, nil
}

func (d *gifDecoder) Next() (VideoFrame, error) {
	g := d.g
	i := d.next
	if i >= len(g.Image) {
		return VideoFrame{}, io.EOF
	}
	if i > 0 {
		d.dispose(i - 1)
	}
	if gifDisposal(g, i) == gif.DisposalPrevious {
		d.saved = append(d.saved[:0], d.pic.Pix...)
	}
	d.draw(g.Image[i])
	d.next++
	delay := 100 * time.Millisecond
	if i < len(g.Delay) && g.Delay[i] > 1 {
		delay = time.Duration(g.Delay[i]) * 10 * time.Millisecond
	}
	return VideoFrame{Image: d.pic, Duration: delay}, nil
}

func (d *gifDecoder) Rewind() error {
	d.next = 0
	clear(d.pic.Pix)
	return nil
}

// gifDisposal returns how frame i goes before the next draws.
func gifDisposal(g *gif.GIF, i int) byte {
	if i < len(g.Disposal) {
		return g.Disposal[i]
	}
	return 0
}

// dispose undoes frame i as its disposal says, before the next draws: it
// clears its bounds (DisposalBackground; browsers clear them to
// transparent rather than the background color), restores what was under
// it (DisposalPrevious), or leaves it.
func (d *gifDecoder) dispose(i int) {
	switch gifDisposal(d.g, i) {
	case gif.DisposalBackground:
		r := d.g.Image[i].Bounds().Intersect(d.pic.Rect)
		for y := r.Min.Y; y < r.Max.Y; y++ {
			clear(d.pic.Pix[d.pic.PixOffset(r.Min.X, y):d.pic.PixOffset(r.Max.X, y)])
		}
	case gif.DisposalPrevious:
		if d.saved != nil {
			copy(d.pic.Pix, d.saved)
		}
	}
}

// draw draws a frame over the picture: its transparent pixels leave what
// is under them.
func (d *gifDecoder) draw(f *image.Paletted) {
	for i, c := range f.Palette {
		r, g, b, a := c.RGBA()
		d.palette[i] = [4]byte{byte(r >> 8), byte(g >> 8), byte(b >> 8), byte(a >> 8)}
	}
	for i := len(f.Palette); i < len(d.palette); i++ {
		d.palette[i] = [4]byte{} // out of the palette: transparent
	}
	r := f.Rect.Intersect(d.pic.Rect)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		src := f.Pix[f.PixOffset(r.Min.X, y):f.PixOffset(r.Max.X, y)]
		dst := d.pic.Pix[d.pic.PixOffset(r.Min.X, y):]
		for x, idx := range src {
			c := d.palette[idx]
			if c[3] == 0 {
				continue
			}
			copy(dst[4*x:4*x+4], c[:])
		}
	}
}
