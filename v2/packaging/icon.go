//go:build ignore

// Generates icon.png, the 1024x1024 app icon: a ">_" prompt on the brand teal (appui/design.go).
// Regenerate with: go run packaging/icon.go   (from v2/; mygo turns the PNG into .icns / .ico)
package main

import (
	"image"
	"image/png"
	"log"
	"math"
	"os"
)

const size = 1024

type rgb struct{ r, g, b float64 }

func hex(r, g, b uint8) rgb { return rgb{float64(r) / 255, float64(g) / 255, float64(b) / 255} }

func mix(a, b rgb, t float64) rgb {
	return rgb{a.r + (b.r-a.r)*t, a.g + (b.g-a.g)*t, a.b + (b.b-a.b)*t}
}

func clamp(x float64) float64 { return math.Max(0, math.Min(1, x)) }

// roundRect is the signed distance to a rounded square of half-size h centred at (512, 512+dy).
func roundRect(x, y, h, r, dy float64) float64 {
	qx, qy := math.Abs(x-512)-(h-r), math.Abs(y-512-dy)-(h-r)
	return math.Hypot(math.Max(qx, 0), math.Max(qy, 0)) + math.Min(math.Max(qx, qy), 0) - r
}

// segment is the distance to the line a-b.
func segment(x, y, ax, ay, bx, by float64) float64 {
	px, py, dx, dy := x-ax, y-ay, bx-ax, by-ay
	t := clamp((px*dx + py*dy) / (dx*dx + dy*dy))
	return math.Hypot(px-t*dx, py-t*dy)
}

func main() {
	top, bottom, glyph := hex(0x0b, 0x9c, 0x9f), hex(0x06, 0x66, 0x6a), hex(0x5c, 0xe1, 0xc7)
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for py := range size {
		for px := range size {
			x, y := float64(px)+.5, float64(py)+.5
			body := clamp(.5 - roundRect(x, y, 412, 185, 0)) // the 824px squircle-ish body, as macOS icons have
			base := mix(top, bottom, clamp((y-100)/824))
			// Glyph: chevron plus underscore, 84px round-capped strokes.
			d := math.Min(segment(x, y, 318, 372, 488, 512), segment(x, y, 488, 512, 318, 652))
			d = math.Min(d, segment(x, y, 548, 652, 704, 652)) - 42
			col := mix(base, glyph, clamp(.5-d)*body)
			// Soft black shadow below the body, composited under it.
			shadow := clamp(1 - math.Max(roundRect(x, y, 412, 185, 14), 0)/28)
			shadow *= .30 * shadow
			alpha := body + (1-body)*shadow
			var r, g, b float64
			if alpha > 0 {
				r, g, b = col.r*body/alpha, col.g*body/alpha, col.b*body/alpha
			}
			o := img.PixOffset(px, py)
			img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3] = uint8(r*255+.5), uint8(g*255+.5), uint8(b*255+.5), uint8(alpha*255+.5)
		}
	}
	f, err := os.Create("packaging/icon.png")
	if err != nil {
		log.Fatal(err)
	}
	if err = png.Encode(f, img); err != nil {
		log.Fatal(err)
	}
	if err = f.Close(); err != nil {
		log.Fatal(err)
	}
}
