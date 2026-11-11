package i18n

import (
	"strings"
	"sync"
	"sync/atomic"
)

// A Notation is how NumberOptions.Notation writes numbers: CompactShort
// or CompactLong. Only an app that uses one links the patterns of compact
// numbers of CLDR's locales.
type Notation struct {
	long   bool
	format func(f *NumberFormatter, n *Notation, neg bool, intPart, frac string) string
	init   func(f *NumberFormatter) // of a NumberFormatter of it
}

var (
	// CompactShort writes numbers as Intl.NumberFormat's notation
	// "compact" does with compactDisplay "short": "1.2K", "12K", "3.4M"
	// in English, "1,2 k" in French, "1.2万" in Japanese, as a chart's axis
	// labels them.
	CompactShort = &Notation{format: formatCompact, init: initCompact}
	// CompactLong writes them with compactDisplay "long": "1.2 thousand",
	// "3.4 million" in English, "1,2 millier" in French.
	CompactLong = &Notation{long: true, format: formatCompact, init: initCompact}
)

// The patterns of compact numbers, which i18n/internal/gen writes to
// fmtPackedCompact in slots of their own: for the default numbering
// system then latn, short then long, the patterns of each magnitude
// from 10^3, each a list of the keys of fmtCompactKeys and their
// patterns, separated by NULs.
var compactTables struct {
	once sync.Once
	pool pool
}

var compactLocales [fmtLocaleCount]atomic.Pointer[compactLocale]

// compactLocale is the compact patterns of a locale, by numbering system
// (the default and latn) and display (short and long).
type compactLocale [2][2]compactStyle

// compactStyle is the patterns of compact numbers of a locale, a
// numbering system and a display.
type compactStyle struct {
	mags    [compactMagnitudes]compactMagnitude
	largest int // the index of the last of mags with patterns, or -1
	// sys is the numbering system, grouping numbers of four digits or
	// more only from five, as ICU groups compact numbers.
	sys numberSystem
}

// compactMagnitude is the patterns of a magnitude: by plural form, as
// fmtCompactKeys lists them, and the power of ten they divide by, the
// magnitude less the number of their zeros less one ("0K" of 10^3
// divides by 10^3, "00K" of 10^4 too).
type compactMagnitude struct {
	exp   int
	forms [8]compactForm
}

// compactForm is a pattern of compact numbers: its affixes, of positive
// numbers and negative ones, and whether it writes the number ("0K") or
// only them (a word for "a thousand"), or is "0", which writes the number
// in the decimal pattern, whole.
type compactForm struct {
	ok, number, whole                    bool
	prefix, suffix, negPrefix, negSuffix string
}

// The indexes of the keys of fmtCompactKeys.
const (
	compactExact0 = iota
	compactExact1
	compactOther = 7
)

// compactStyleOf returns the compact patterns of a locale in its numbering
// system sys, long or short.
func compactStyleOf(k *localeKey, sys *numberSystem, long bool) *compactStyle {
	d := k.dat()
	c := compactLocales[d.index].Load()
	if c == nil {
		c = loadCompactLocale(d)
		if !compactLocales[d.index].CompareAndSwap(nil, c) {
			c = compactLocales[d.index].Load()
		}
	}
	s := 0
	if sys != d.numbers(false) && c[1][0].largest >= 0 {
		s = 1
	}
	if long {
		return &c[s][1]
	}
	return &c[s][0]
}

func loadCompactLocale(d *localeData) *compactLocale {
	loadTables()
	compactTables.once.Do(func() {
		t := unpack(fmtPackedCompact, packedCompactEntriesLen, packedCompactLensLen, packedCompactPoolLen)
		compactTables.pool = newPool(t[0], t[1], t[2], fmtCompactStringCount)
	})
	var v [compactSlotCount]string
	var set [compactSlotCount]bool
	fillSlots(d.index, &compactTables.pool, fmtCompactStarts[:], v[:], set[:])
	c := new(compactLocale)
	for s := range c {
		sys := d.numbers(s == 1)
		for st := range c[s] {
			cs := &c[s][st]
			cs.sys = *sys
			cs.sys.minGrouping = max(sys.minGrouping, 2)
			cs.parse(v[(s*2+st)*compactMagnitudes:(s*2+st+1)*compactMagnitudes], sys)
		}
	}
	return c
}

// parse reads the patterns of a style, slots by magnitude from 10^3, in
// the symbols of sys.
func (cs *compactStyle) parse(slots []string, sys *numberSystem) {
	keys := strings.Fields(fmtCompactKeys)
	cs.largest = -1
	for m := range cs.mags {
		cm := &cs.mags[m]
		slot := slots[m]
		if slot == "" {
			continue
		}
		cs.largest = m
		parts := strings.Split(slot, "\x00")
		var raw [len(cm.forms)]string
		for i := 0; i+1 < len(parts); i += 2 {
			for j, key := range keys {
				if key == parts[i] {
					raw[j] = parts[i+1]
					cm.forms[j] = parseCompactForm(parts[i+1], sys)
				}
			}
		}
		// The power of ten of the magnitude's patterns: that of the
		// first with zeros.
		for _, p := range raw {
			if zeros := compactZeros(p); zeros > 0 {
				cm.exp = compactMinMagnitude + m - (zeros - 1)
				break
			}
		}
	}
}

// The patterns of compact amounts of money, which i18n/internal/gen
// writes to fmtPackedCurrencyCompact as compact numbers' (of short ones
// only, as CLDR has no others): ICU writes long compact amounts in them
// too, unless they name their currency.
var currencyCompactTables struct {
	once sync.Once
	pool pool
}

// compactAmounts makes the patterns of compact amounts of a formatter.
// Its method is linked only into an app that formats both amounts and
// compact numbers: one converts currencyCompact to it, the other calls
// it.
type compactAmounts interface {
	compactStyle(f *NumberFormatter) *compactStyle
}

// initCompact readies a formatter of amounts for compact notation: its
// separators grouped as compact numbers are, and its patterns of compact
// amounts.
func initCompact(f *NumberFormatter) {
	cf := f.cur
	if cf == nil {
		return
	}
	cf.compactSys = cf.sys
	cf.compactSys.minGrouping = max(cf.sys.minGrouping, 2)
	if c, ok := cf.compacter.(compactAmounts); ok && cf.units == nil {
		cf.compact = c.compactStyle(f) // names of currencies follow compact numbers
	}
}

// currencyCompact is the compactAmounts of formatters of amounts.
type currencyCompact struct{}

// compactStyle returns the patterns of compact amounts of f: its
// currency's sign in the locale's, those of latn where its numbering
// system has none, as ICU writes them.
func (currencyCompact) compactStyle(f *NumberFormatter) *compactStyle {
	cf := f.cur
	currencyCompactTables.once.Do(func() {
		t := unpack(fmtPackedCurrencyCompact, packedCurrencyCompactEntriesLen, packedCurrencyCompactLensLen, packedCurrencyCompactPoolLen)
		currencyCompactTables.pool = newPool(t[0], t[1], t[2], fmtCurrencyCompactStringCount)
	})
	d := f.key.dat()
	var v [2 * compactMagnitudes]string
	var set [len(v)]bool
	fillSlots(d.index, &currencyCompactTables.pool, fmtCurrencyCompactStarts[:], v[:], set[:])
	slots := v[:compactMagnitudes]
	if cf.base != d.numbers(false) || strings.Join(slots, "") == "" {
		slots = v[compactMagnitudes:]
	}
	cs := &compactStyle{sys: cf.compactSys}
	cs.parse(slots, &cf.sys)
	for m := range cs.mags {
		for i := range cs.mags[m].forms {
			form := &cs.mags[m].forms[i]
			if form.ok && !form.whole {
				spaced := form.number
				form.prefix, form.suffix = cf.place(form.prefix, true, spaced), cf.place(form.suffix, false, spaced)
				form.negPrefix, form.negSuffix = cf.place(form.negPrefix, true, spaced), cf.place(form.negSuffix, false, spaced)
			}
		}
	}
	return cs
}

// compactZeros returns the number of zeros of a compact pattern, 0 for
// "0", which writes the number whole.
func compactZeros(p string) int {
	if p == "0" {
		return 0
	}
	start, end := numberOf(p)
	return end - start
}

// numberOf returns where the zeros of the positive subpattern of a
// compact pattern start and end, outside quotes.
func numberOf(p string) (start, end int) {
	quoted := false
	for i := 0; i < len(p); i++ {
		switch {
		case p[i] == '\'':
			quoted = !quoted
		case p[i] == ';' && !quoted:
			return max(start, 0), end
		case p[i] == '0' && !quoted:
			if end == 0 {
				start = i
			}
			end = i + 1
		}
	}
	return start, end
}

// parseCompactForm reads a compact pattern: affixes around zeros, which
// stand for the number, with the symbols of sys, then those of negative
// numbers after a semicolon, else the positive ones after the minus. "0"
// is the locale's decimal pattern, which writes the number whole.
func parseCompactForm(p string, sys *numberSystem) compactForm {
	if p == "0" {
		return compactForm{ok: true, whole: true}
	}
	f := compactForm{ok: true}
	pos, neg, hasNeg := strings.Cut(p, ";")
	f.prefix, f.suffix, f.number = compactAffixes(pos, sys)
	f.negPrefix, f.negSuffix = sys.minus+f.prefix, f.suffix
	if hasNeg {
		f.negPrefix, f.negSuffix, _ = compactAffixes(neg, sys)
	}
	return f
}

// compactAffixes returns the affixes of a subpattern of a compact pattern,
// and whether it has a number between them.
func compactAffixes(sub string, sys *numberSystem) (prefix, suffix string, number bool) {
	start, end := numberOf(sub)
	if end == 0 {
		return affix(sub, sys), "", false
	}
	return affix(sub[:start], sys), affix(sub[end:], sys), true
}

// formatCompact writes a number in compact notation, as ICU's
// CompactHandler does: divided by the power of ten of its magnitude's
// patterns, rounded, and in the pattern of the plural form of what it
// writes.
func formatCompact(f *NumberFormatter, n *Notation, neg bool, intPart, frac string) string {
	var cs *compactStyle
	sys, base, grouping := f.sys, f.pat, (*numberPattern)(nil)
	if f.cur != nil {
		cs, _ = f.cur.compact.(*compactStyle)
		sys = f.cur.base
		base, grouping = &f.cur.pat, &f.cur.pat
		if f.cur.units != nil {
			base = &f.cur.inner
		}
	}
	if cs == nil {
		cs = compactStyleOf(f.key, sys, n.long)
	}
	digits := &cs.sys
	if f.cur != nil {
		digits = &f.cur.compactSys
	}
	if grouping == nil {
		grouping = &digits.decimal
	}
	mag, zero := decimalMagnitude(intPart, frac)
	exp := 0
	if zero {
		intPart, frac = f.compactRound(intPart, frac)
	} else {
		exp = cs.exp(mag)
		i, fr := f.compactRound(shiftRight(intPart, frac, exp))
		if m, z := decimalMagnitude(i, fr); !z && m != mag-exp {
			// Rounding carried into the next magnitude, as 999999 to
			// 1000K: in its patterns, if they divide by another power.
			if e := cs.exp(mag + 1); e != exp {
				i, fr = f.compactRound(shiftRight(intPart, frac, e))
				exp = e
			}
		}
		intPart, frac = i, fr
	}
	mag, zero = decimalMagnitude(intPart, frac)
	form := cs.form(f.key, mag+exp, zero, neg, intPart, frac)
	// The affixes: those of the form, with the sign, else of the decimal
	// pattern (or that of amounts); a percent's around them, or the name
	// of a currency, in the plural form of the number with its exponent.
	neg, plus := f.sign(neg, zero)
	var outerPrefix, outerSuffix, prefix, suffix string
	switch {
	case form != nil && neg:
		prefix, suffix = form.negPrefix, form.negSuffix
	case form != nil:
		prefix, suffix = form.prefix, form.suffix
	case f.o.Percent && neg:
		prefix = f.sys.minus
	case neg:
		prefix, suffix = base.negPrefix, base.negSuffix
	case !f.o.Percent:
		prefix, suffix = base.prefix, base.suffix
	}
	if plus {
		// Where a minus goes, as "%+50" of a percent in Turkish.
		prefix = f.sys.plus + prefix
	}
	if f.o.Percent {
		outerPrefix, outerSuffix = f.pat.prefix, f.pat.suffix
	}
	if f.cur != nil && f.cur.units != nil {
		u := &f.cur.units[f.cur.plural(f, intPart, frac, exp)]
		outerPrefix, outerSuffix = u[0], u[1]
	}
	var b strings.Builder
	b.WriteString(outerPrefix)
	b.WriteString(prefix)
	if form == nil || form.number {
		digits.writeInteger(&b, intPart, grouping, !f.o.NoGrouping, f.o.MinIntegerDigits)
		if frac != "" {
			b.WriteString(digits.decimalSep)
			digits.writeDigits(&b, frac)
		}
	}
	b.WriteString(suffix)
	b.WriteString(outerSuffix)
	return b.String()
}

// exp returns the power of ten the patterns of a magnitude divide by,
// those of the largest for larger ones.
func (cs *compactStyle) exp(mag int) int {
	m := min(mag-compactMinMagnitude, cs.largest)
	if m < 0 {
		return 0
	}
	return cs.mags[m].exp
}

// form returns the pattern of a number of a magnitude, which the pattern
// divided to intPart.frac: its exact form ("1" for "mille" in French), else
// that of its plural form, else "other", or nil for the decimal pattern.
func (cs *compactStyle) form(k *localeKey, mag int, zero, neg bool, intPart, frac string) *compactForm {
	if zero {
		mag = 0
	}
	m := min(mag-compactMinMagnitude, cs.largest)
	if m < 0 {
		return nil
	}
	forms := &cs.mags[m].forms
	var f *compactForm
	if !neg && strings.Trim(frac, "0") == "" && (intPart == "0" || intPart == "1") {
		f = &forms[compactExact0+int(intPart[0]-'0')]
	}
	if f == nil || !f.ok {
		num := intPart
		if frac != "" {
			num += "." + frac
		}
		o, _ := parseOperands(num)
		f = &forms[2+int(PluralCategory(k.dat().tag, o))]
		if !f.ok {
			f = &forms[compactOther]
		}
	}
	if !f.ok || f.whole {
		return nil
	}
	return f
}

// compactRound rounds a number in compact notation: to the fraction
// digits of the options, else to a whole number of two digits or more,
// or two significant digits, as Intl rounds compact numbers.
func (f *NumberFormatter) compactRound(intPart, frac string) (string, string) {
	if f.o.Integer || f.o.MinFractionDigits > 0 || f.o.MaxFractionDigits > 0 {
		return f.round(intPart, frac, f.max)
	}
	max := 0
	switch {
	case len(intPart) >= 2:
	case intPart != "0":
		max = 1
	default:
		max = len(frac) - len(strings.TrimLeft(frac, "0")) + 2
	}
	return f.round(intPart, frac, max)
}

// decimalMagnitude returns the power of ten of the first digit of a
// number, or that it is zero.
func decimalMagnitude(intPart, frac string) (int, bool) {
	if intPart != "0" {
		return len(intPart) - 1, false
	}
	t := strings.TrimLeft(frac, "0")
	if t == "" {
		return 0, true
	}
	return -(len(frac) - len(t) + 1), false
}

// shiftRight divides a number by 10^n.
func shiftRight(intPart, frac string, n int) (string, string) {
	if n <= 0 {
		return intPart, frac
	}
	if len(intPart) < n {
		intPart = strings.Repeat("0", n-len(intPart)) + intPart
	}
	intPart, frac = intPart[:len(intPart)-n], intPart[len(intPart)-n:]+frac
	intPart = strings.TrimLeft(intPart, "0")
	if intPart == "" {
		intPart = "0"
	}
	return intPart, frac
}
