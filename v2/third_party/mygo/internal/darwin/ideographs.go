//go:build darwin

package darwin

import (
	"sync"

	"github.com/ebitengine/purego"
)

var (
	coreTextOnce                 sync.Once
	ctFontCreateWithName         func(name id, size float64, matrix uintptr) uintptr
	ctFontCopyPostScriptName     func(font uintptr) id
	ctFontGetGlyphsForCharacters func(font uintptr, chars, glyphs *uint16, count int) bool
)

// ideographBlocks are where RadicalIdeographs looks for the ideograph of a
// radical's glyph, in order: CJK Unified Ideographs, then Extension A.
var ideographBlocks = [...]struct{ first, last uint16 }{{0x4E00, 0x9FFF}, {0x3400, 0x4DBF}}

// RadicalIdeographs returns the ideographs that the installed font of the
// PostScript name draws with the glyphs of the radicals, as PingFang SC
// draws ⼈ (U+2F08) with the glyph of 人: for each radical whose glyph an
// ideograph has too, the first such ideograph of CJK Unified Ideographs,
// else of Extension A. It returns nil when no font has the name. It may be
// called from any goroutine.
func RadicalIdeographs(font string, radicals []rune) map[rune]rune {
	load()
	coreTextOnce.Do(func() {
		lib := mustDlopen("/System/Library/Frameworks/CoreText.framework/CoreText")
		purego.RegisterLibFunc(&ctFontCreateWithName, lib, "CTFontCreateWithName")
		purego.RegisterLibFunc(&ctFontCopyPostScriptName, lib, "CTFontCopyPostScriptName")
		purego.RegisterLibFunc(&ctFontGetGlyphsForCharacters, lib, "CTFontGetGlyphsForCharacters")
	})
	var found map[rune]rune
	withPool(func() {
		f := ctFontCreateWithName(nsString(font), 12, 0)
		if f == 0 {
			return
		}
		defer cfRelease(f)
		// CoreText stands in another font for a name it does not know.
		name := ctFontCopyPostScriptName(f)
		if name == 0 {
			return
		}
		same := goString(name) == font
		cfRelease(uintptr(name))
		if !same {
			return
		}
		chars := make([]uint16, 0, len(radicals))
		for _, r := range radicals {
			if r > 0 && r <= 0xFFFF {
				chars = append(chars, uint16(r))
			}
		}
		if len(chars) == 0 {
			return
		}
		// The radicals of each glyph, which may be several: Songti SC draws
		// ⼝ (U+2F1D) and ⼜ (U+2F1C) as 口.
		glyphs := make([]uint16, len(chars))
		ctFontGetGlyphsForCharacters(f, &chars[0], &glyphs[0], len(chars))
		wanted := map[uint16][]rune{}
		for i, g := range glyphs {
			if g != 0 {
				wanted[g] = append(wanted[g], rune(chars[i]))
			}
		}
		found = map[rune]rune{}
		for _, b := range ideographBlocks {
			if len(wanted) == 0 {
				break
			}
			chars = make([]uint16, int(b.last-b.first)+1)
			for i := range chars {
				chars[i] = b.first + uint16(i)
			}
			glyphs = make([]uint16, len(chars))
			// It reports whether every character has a glyph, which few
			// fonts give them all; the others get glyph 0.
			ctFontGetGlyphsForCharacters(f, &chars[0], &glyphs[0], len(chars))
			for i, g := range glyphs {
				for _, r := range wanted[g] {
					found[r] = rune(chars[i])
				}
				delete(wanted, g)
			}
		}
	})
	return found
}
