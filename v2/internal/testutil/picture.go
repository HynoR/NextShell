package testutil

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// WritePicture writes a procedural picture width pixels wide (and 5/8 of that high) to path, with no external
// assets: a diagonal gradient from warm to cool with a grid of bright disks, vivid enough to show through a
// translucent panel and to put text over.
func WritePicture(t testing.TB, path string, width int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, width*5/8))
	b := img.Bounds()
	for y := range b.Dy() {
		for x := range b.Dx() {
			f := float64(x+y) / float64(b.Dx()+b.Dy())
			c := color.RGBA{R: uint8(255 * (1 - f)), G: uint8(90 + 120*math.Sin(f*math.Pi)), B: uint8(60 + 195*f), A: 255}
			if cx, cy := x%320-160, y%320-160; cx*cx+cy*cy < 70*70 {
				c = color.RGBA{R: 250, G: 215, B: 60, A: 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if e := png.Encode(&buf, img); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(path, buf.Bytes(), 0o600); e != nil {
		t.Fatal(e)
	}
}

// Picture is a procedural picture file in a folder of its own.
func Picture(t testing.TB) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "wallpaper.png")
	WritePicture(t, path, 1600)
	return path
}

// Solid is a 64x40 picture of one color in a folder of its own.
func Solid(t testing.TB, c color.RGBA) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 40))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, 255
	}
	var buf bytes.Buffer
	if e := png.Encode(&buf, img); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(t.TempDir(), "solid.png")
	if e := os.WriteFile(path, buf.Bytes(), 0o600); e != nil {
		t.Fatal(e)
	}
	return path
}

// PNGHeader is a PNG that says it is w by h and holds no picture: enough for its size to be read, and for a
// decoder to find it damaged.
func PNGHeader(w, h uint32) []byte {
	ihdr := binary.BigEndian.AppendUint32(binary.BigEndian.AppendUint32(nil, w), h)
	ihdr = append(ihdr, 8, 0, 0, 0, 0) // 8 bits, gray
	chunk := append([]byte("IHDR"), ihdr...)
	out := []byte("\x89PNG\r\n\x1a\n")
	out = binary.BigEndian.AppendUint32(out, uint32(len(ihdr)))
	out = append(out, chunk...)
	return binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(chunk))
}
