package text

import "math"

// strokeBitmap returns the coverage of a stroke along the edge of the
// coverage of b, reaching r pixels to each side of it, round at corners:
// what b dilated by r covers and b eroded by r does not. An edge pixel's
// coverage places the edge within it, so that the stroke's edges are as
// smooth as the glyph's. Color glyphs stroke the edge of their alpha.
func strokeBitmap(b bitmap, r float32) bitmap {
	pad := int(math.Ceil(float64(r))) + 1
	w, h := b.w+2*pad, b.h+2*pad
	cov := make([]float32, b.w*b.h)
	for i := range cov {
		switch {
		case b.color:
			cov[i] = float32(b.pix[4*i+3]) / 255
		case b.subpixel:
			p := b.pix[4*i:]
			cov[i] = float32(max(p[0], p[1], p[2])) / 255
		default:
			cov[i] = float32(b.pix[i]) / 255
		}
	}
	// The pixels a pixel's coverage reaches: within r of its edge, which
	// is up to a pixel from its center.
	type offset struct {
		dx, dy int
		d      float32
	}
	// Nearest first, as the coverage of a pixel saturates soonest: by
	// their squared distances, counted.
	reach := r + 1
	maxSq := 2 * pad * pad
	counts := make([]int, maxSq+2)
	for dy := -pad; dy <= pad; dy++ {
		for dx := -pad; dx <= pad; dx++ {
			counts[dx*dx+dy*dy+1]++
		}
	}
	for i := 1; i < len(counts); i++ {
		counts[i] += counts[i-1]
	}
	kernel := make([]offset, counts[len(counts)-1])
	for dy := -pad; dy <= pad; dy++ {
		for dx := -pad; dx <= pad; dx++ {
			sq := dx*dx + dy*dy
			kernel[counts[sq]] = offset{dx, dy, float32(math.Sqrt(float64(sq)))}
			counts[sq]++
		}
	}
	for len(kernel) > 0 && kernel[len(kernel)-1].d >= reach {
		kernel = kernel[:len(kernel)-1]
	}
	at := func(x, y int) float32 {
		if x < 0 || y < 0 || x >= b.w || y >= b.h {
			return 0
		}
		return cov[y*b.w+x]
	}
	pix := make([]byte, w*h)
	for y := range h {
		for x := range w {
			// out is what the dilated glyph covers, in what the eroded
			// background does.
			var out, in float32
			for _, k := range kernel {
				c := at(x-pad+k.dx, y-pad+k.dy)
				if c > 0 {
					out = max(out, min(r+c-k.d, 1))
				}
				if c < 1 {
					in = max(in, min(r+1-c-k.d, 1))
				}
				if out >= 1 && in >= 1 {
					break
				}
			}
			if v := out + in - 1; v > 0 {
				pix[y*w+x] = uint8(v*255 + 0.5)
			}
		}
	}
	return bitmap{left: b.left - pad, top: b.top - pad, w: w, h: h, pix: pix}
}
