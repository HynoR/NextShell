package text

import "math"

// blurBitmap returns the coverage of b blurred by a Gaussian of standard
// deviation sigma pixels, as a mask reaching three of them past b on each
// side: a glyph's shadow. Color glyphs blur their alpha, subpixel ones
// their most covered channel; a sigma of 0 leaves the coverage sharp.
func blurBitmap(b bitmap, sigma float32) bitmap {
	pad := 0
	if sigma > 0 {
		pad = int(math.Ceil(float64(3*sigma))) + 1
	}
	w, h := b.w+2*pad, b.h+2*pad
	cov := make([]float32, w*h)
	for y := range b.h {
		for x := range b.w {
			i := y*b.w + x
			var c byte
			switch {
			case b.color:
				c = b.pix[4*i+3]
			case b.subpixel:
				p := b.pix[4*i:]
				c = max(p[0], p[1], p[2])
			default:
				c = b.pix[i]
			}
			cov[(y+pad)*w+x+pad] = float32(c) / 255
		}
	}
	if pad > 0 {
		// The kernel, normalized, and the blur in rows, then in columns.
		kernel := make([]float32, 2*pad+1)
		var sum float32
		for i := range kernel {
			d := float64(i - pad)
			kernel[i] = float32(math.Exp(-d * d / (2 * float64(sigma) * float64(sigma))))
			sum += kernel[i]
		}
		for i := range kernel {
			kernel[i] /= sum
		}
		tmp := make([]float32, w*h)
		blur := func(dst, src []float32, n, step, lines, across int) {
			for l := range lines {
				for i := range n {
					var v float32
					for k, kv := range kernel {
						if j := i + k - pad; j >= 0 && j < n {
							v += src[l*across+j*step] * kv
						}
					}
					dst[l*across+i*step] = v
				}
			}
		}
		blur(tmp, cov, w, 1, h, w) // rows
		blur(cov, tmp, h, w, w, 1) // columns
	}
	pix := make([]byte, w*h)
	for i, c := range cov {
		pix[i] = uint8(min(max(c, 0), 1)*255 + 0.5)
	}
	return bitmap{left: b.left - pad, top: b.top - pad, w: w, h: h, pix: pix}
}
