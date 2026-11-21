package shellintegration

import "golang.org/x/text/width"

// runeWidth is how many cells a terminal gives r: two for East Asian wide and fullwidth characters.
func runeWidth(r rune) int {
	switch width.LookupRune(r).Kind() {
	case width.EastAsianWide, width.EastAsianFullwidth:
		return 2
	}
	if r < 0x20 {
		return 0
	}
	return 1
}
