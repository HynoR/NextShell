package text

import "unicode"

// Word boundaries, without a dictionary: a word is a run of letters,
// digits and underscores, as in the scripts written with spaces between
// words (Latin, Cyrillic, Greek, Hangul…), except that each Han
// ideograph, Hiragana and Katakana is a word of its own. Chinese and
// Japanese are written without spaces and only a dictionary finds their
// words (UAX #29 leaves them to one); a character at a time is what
// platforms do without one, rather than a whole sentence as one word.
// Thai, also written without spaces, keeps its runs: its vowels are
// combining marks, so a rune at a time would split syllables.

// IsWordRune reports whether r is part of a word: a letter, a digit or
// an underscore.
func IsWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }

// StandsAlone reports whether r is a word on its own: a Han ideograph
// (with the iteration mark 々), a Hiragana or a Katakana (with the
// prolonged sound mark ー).
func StandsAlone(r rune) bool {
	if r < 0x2e80 {
		return false
	}
	switch r {
	case 0x30fc, 0xff70: // ー and its halfwidth form, of the Common script
		return true
	}
	return IsWordRune(r) && (unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r))
}

// WordBoundary reports whether a and b, next to each other, are in
// different words or runs of other runes: one is part of a word and the
// other is not, or either is a word on its own (StandsAlone). Script
// changes between Latin and Han or kana and kanji are boundaries so.
func WordBoundary(a, b rune) bool {
	return IsWordRune(a) != IsWordRune(b) || StandsAlone(a) || StandsAlone(b)
}
