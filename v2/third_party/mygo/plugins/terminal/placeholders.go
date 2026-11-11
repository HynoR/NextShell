package terminal

import (
	"slices"

	"github.com/egoist/mygo/plugins/terminal/internal/vt"
	"github.com/egoist/mygo/ui"
)

// placeholder is the character of a cell that shows part of an image
// placed virtually (Kitty graphics' Unicode placeholders), as yazi shows
// its previews: the cell's foreground color is the image's id, its
// underline color the placement's, and its combining marks the row and
// column of the image it shows.
const placeholder = 0x10eeee

// holderRun is a run of placeholder cells of a row, from column x for w
// columns, that show one row of a virtual placement of an image, from
// cell row, col of it.
type holderRun struct {
	x, w             int
	image, placement uint32
	row, col         int
	high             int // the image id's high byte, -1 when left out
}

// holderCell is what a placeholder cell says: row, col and high are -1
// when it leaves them out.
type holderCell struct {
	image, placement uint32 // image's low 24 bits
	row, col, high   int
}

// readHolder reads a placeholder cell of a style and combining marks.
// Marks not in the table count as left out, as in Kitty and Ghostty.
func readHolder(s vt.Style, marks []rune) holderCell {
	h := holderCell{image: colorID(s.FG), placement: colorID(s.UnderlineColor), row: -1, col: -1, high: -1}
	if len(marks) > 0 {
		h.row = diacritic(marks[0])
	}
	if len(marks) > 1 {
		h.col = diacritic(marks[1])
	}
	if len(marks) > 2 {
		if d := diacritic(marks[2]); d < 256 {
			h.high = d
		}
	}
	return h
}

// colorID is the id a color gives: a palette index, or 24 bits of RGB.
func colorID(c vt.Color) uint32 {
	switch c.Kind {
	case 1:
		return uint32(c.Index)
	case 2:
		return uint32(c.RGB.R)<<16 | uint32(c.RGB.G)<<8 | uint32(c.RGB.B)
	}
	return 0
}

// addHolder adds a placeholder cell at column x to the row's runs: it
// continues the run to its left when it is of the same image and
// placement and leaves out what would continue it, or says the same, and
// starts a run otherwise, at row and column 0 of what it leaves out.
func addHolder(runs []holderRun, x int, h holderCell) []holderRun {
	if n := len(runs); n > 0 {
		r := &runs[n-1]
		if r.x+r.w == x && r.image&0xffffff == h.image && r.placement == h.placement &&
			(h.row < 0 || h.row == r.row) && (h.col < 0 || h.col == r.col+r.w) && (h.high < 0 || h.high == r.high) {
			r.w++
			return runs
		}
	}
	r := holderRun{x: x, w: 1, image: h.image, placement: h.placement, row: max(h.row, 0), col: max(h.col, 0), high: h.high}
	if h.high >= 0 {
		r.image |= uint32(h.high) << 24
	}
	return append(runs, r)
}

// virtualImage is a virtual placement of an image, which placeholder
// cells show: cols and rows are the cells the image fits in, 0 for those
// its size takes.
type virtualImage struct {
	id         uint32
	bmp        *ui.Bitmap
	imgW, imgH int
	cols, rows int
	layer      int
}

// virtualFor returns the virtual placement that placeholders of an image
// and placement show: placement's, or for 0 the image's of the lowest id.
func (v *view) virtualFor(image, placement uint32) (virtualImage, bool) {
	var best virtualImage
	found := false
	for _, vi := range v.virtual[image] {
		if placement != 0 {
			if vi.id == placement {
				return vi, true
			}
			continue
		}
		if !found || vi.id < best.id {
			best, found = vi, true
		}
	}
	return best, found
}

// holderRegion returns the part of a virtual placement's image that a run
// of placeholders shows, src in the image's pixels, and where it goes, dst
// from the run's top left in device pixels: the image fits the
// placement's cells, centered, as in Ghostty. ok is false when the run
// shows none of it.
func holderRegion(h holderRun, vi virtualImage, cw, ch int) (src, dst ui.Rect, ok bool) {
	cols, rows := vi.cols, vi.rows
	if rows <= 0 {
		rows = (vi.imgH + ch - 1) / ch
	}
	if cols <= 0 {
		cols = (vi.imgW + cw - 1) / cw
	}
	if cols <= 0 || rows <= 0 || vi.imgW <= 0 || vi.imgH <= 0 {
		return src, dst, false
	}
	iw, ih := float32(vi.imgW), float32(vi.imgH)
	pw, ph := float32(cols*cw), float32(rows*ch)
	scale := min(pw/iw, ph/ih)
	img := ui.Rect{X: (pw - iw*scale) / 2, Y: (ph - ih*scale) / 2, W: iw * scale, H: ih * scale}
	run := ui.Rect{X: float32(h.col * cw), Y: float32(h.row * ch), W: float32(h.w * cw), H: float32(ch)}
	x0, y0 := max(img.X, run.X), max(img.Y, run.Y)
	x1, y1 := min(img.X+img.W, run.X+run.W), min(img.Y+img.H, run.Y+run.H)
	if x1 <= x0 || y1 <= y0 {
		return src, dst, false
	}
	src = ui.Rect{X: (x0 - img.X) / scale, Y: (y0 - img.Y) / scale, W: (x1 - x0) / scale, H: (y1 - y0) / scale}
	dst = ui.Rect{X: x0 - run.X, Y: y0 - run.Y, W: x1 - x0, H: y1 - y0}
	return src, dst, true
}

// drawHolders draws what placeholder cells show of the virtual
// placements of a layer, within grid.
func (v *view) drawHolders(p *ui.Painter, layer int, ox, oy float32, grid ui.Rect) {
	s := v.scale
	for y := range v.lines {
		for _, h := range v.lines[y].holders {
			vi, ok := v.virtualFor(h.image, h.placement)
			if !ok || vi.layer != layer {
				continue
			}
			src, dst, ok := holderRegion(h, vi, v.cellW, v.cellH)
			if !ok {
				continue
			}
			dst.X = ox + (float32(h.x*v.cellW)+dst.X)/s
			dst.Y = oy + (float32(y*v.cellH)+dst.Y)/s
			dst.W, dst.H = dst.W/s, dst.H/s
			p.Clip(grid, 0, func() { p.ImageRegion(vi.bmp, src, dst, ui.ImageOptions{}) })
		}
	}
}

// diacritic returns the number a combining mark stands for in a
// placeholder's row, column or id, -1 for none.
func diacritic(r rune) int {
	i, ok := slices.BinarySearch(diacritics, r)
	if !ok {
		return -1
	}
	return i
}

// diacritics are the combining marks of the numbers from 0, Kitty's
// rowcolumn-diacritics.txt.
var diacritics = []rune{
	0x0305, 0x030d, 0x030e, 0x0310, 0x0312, 0x033d, 0x033e, 0x033f, 0x0346, 0x034a,
	0x034b, 0x034c, 0x0350, 0x0351, 0x0352, 0x0357, 0x035b, 0x0363, 0x0364, 0x0365,
	0x0366, 0x0367, 0x0368, 0x0369, 0x036a, 0x036b, 0x036c, 0x036d, 0x036e, 0x036f,
	0x0483, 0x0484, 0x0485, 0x0486, 0x0487, 0x0592, 0x0593, 0x0594, 0x0595, 0x0597,
	0x0598, 0x0599, 0x059c, 0x059d, 0x059e, 0x059f, 0x05a0, 0x05a1, 0x05a8, 0x05a9,
	0x05ab, 0x05ac, 0x05af, 0x05c4, 0x0610, 0x0611, 0x0612, 0x0613, 0x0614, 0x0615,
	0x0616, 0x0617, 0x0657, 0x0658, 0x0659, 0x065a, 0x065b, 0x065d, 0x065e, 0x06d6,
	0x06d7, 0x06d8, 0x06d9, 0x06da, 0x06db, 0x06dc, 0x06df, 0x06e0, 0x06e1, 0x06e2,
	0x06e4, 0x06e7, 0x06e8, 0x06eb, 0x06ec, 0x0730, 0x0732, 0x0733, 0x0735, 0x0736,
	0x073a, 0x073d, 0x073f, 0x0740, 0x0741, 0x0743, 0x0745, 0x0747, 0x0749, 0x074a,
	0x07eb, 0x07ec, 0x07ed, 0x07ee, 0x07ef, 0x07f0, 0x07f1, 0x07f3, 0x0816, 0x0817,
	0x0818, 0x0819, 0x081b, 0x081c, 0x081d, 0x081e, 0x081f, 0x0820, 0x0821, 0x0822,
	0x0823, 0x0825, 0x0826, 0x0827, 0x0829, 0x082a, 0x082b, 0x082c, 0x082d, 0x0951,
	0x0953, 0x0954, 0x0f82, 0x0f83, 0x0f86, 0x0f87, 0x135d, 0x135e, 0x135f, 0x17dd,
	0x193a, 0x1a17, 0x1a75, 0x1a76, 0x1a77, 0x1a78, 0x1a79, 0x1a7a, 0x1a7b, 0x1a7c,
	0x1b6b, 0x1b6d, 0x1b6e, 0x1b6f, 0x1b70, 0x1b71, 0x1b72, 0x1b73, 0x1cd0, 0x1cd1,
	0x1cd2, 0x1cda, 0x1cdb, 0x1ce0, 0x1dc0, 0x1dc1, 0x1dc3, 0x1dc4, 0x1dc5, 0x1dc6,
	0x1dc7, 0x1dc8, 0x1dc9, 0x1dcb, 0x1dcc, 0x1dd1, 0x1dd2, 0x1dd3, 0x1dd4, 0x1dd5,
	0x1dd6, 0x1dd7, 0x1dd8, 0x1dd9, 0x1dda, 0x1ddb, 0x1ddc, 0x1ddd, 0x1dde, 0x1ddf,
	0x1de0, 0x1de1, 0x1de2, 0x1de3, 0x1de4, 0x1de5, 0x1de6, 0x1dfe, 0x20d0, 0x20d1,
	0x20d4, 0x20d5, 0x20d6, 0x20d7, 0x20db, 0x20dc, 0x20e1, 0x20e7, 0x20e9, 0x20f0,
	0x2cef, 0x2cf0, 0x2cf1, 0x2de0, 0x2de1, 0x2de2, 0x2de3, 0x2de4, 0x2de5, 0x2de6,
	0x2de7, 0x2de8, 0x2de9, 0x2dea, 0x2deb, 0x2dec, 0x2ded, 0x2dee, 0x2def, 0x2df0,
	0x2df1, 0x2df2, 0x2df3, 0x2df4, 0x2df5, 0x2df6, 0x2df7, 0x2df8, 0x2df9, 0x2dfa,
	0x2dfb, 0x2dfc, 0x2dfd, 0x2dfe, 0x2dff, 0xa66f, 0xa67c, 0xa67d, 0xa6f0, 0xa6f1,
	0xa8e0, 0xa8e1, 0xa8e2, 0xa8e3, 0xa8e4, 0xa8e5, 0xa8e6, 0xa8e7, 0xa8e8, 0xa8e9,
	0xa8ea, 0xa8eb, 0xa8ec, 0xa8ed, 0xa8ee, 0xa8ef, 0xa8f0, 0xa8f1, 0xaab0, 0xaab2,
	0xaab3, 0xaab7, 0xaab8, 0xaabe, 0xaabf, 0xaac1, 0xfe20, 0xfe21, 0xfe22, 0xfe23,
	0xfe24, 0xfe25, 0xfe26, 0x10a0f, 0x10a38, 0x1d185, 0x1d186, 0x1d187, 0x1d188, 0x1d189,
	0x1d1aa, 0x1d1ab, 0x1d1ac, 0x1d1ad, 0x1d242, 0x1d243, 0x1d244,
}
