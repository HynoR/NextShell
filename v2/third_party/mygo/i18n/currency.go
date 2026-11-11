package i18n

import (
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

// A Currency is a currency of ISO 4217 that NumberOptions.Currency writes
// amounts of money in. CurrencyOf returns one; the zero Currency is none.
// Only an app that calls CurrencyOf, or has messages of its own (whose
// skeletons may name one), links the patterns and symbols of currencies
// of CLDR's locales.
type Currency struct {
	code string
	link *currencyLink
}

// currencyLink is the formatting of amounts, which only CurrencyOf
// links.
type currencyLink struct {
	init func(f *NumberFormatter, code string)
}

var linkedCurrencies = &currencyLink{init: initCurrency}

// CurrencyOf returns the currency of an ISO 4217 code, as
// Intl.NumberFormat's currency option takes it: "USD", "EUR", "JPY", in
// any case. A code CLDR has no data of is written as itself, with two
// fraction digits ("XYZ 1.00"); one that is not three letters is the
// zero Currency.
func CurrencyOf(code string) Currency {
	if len(code) != 3 {
		return Currency{}
	}
	lower := false
	for i := 0; i < 3; i++ {
		c := code[i]
		if c|0x20 < 'a' || c|0x20 > 'z' {
			return Currency{}
		}
		lower = lower || c >= 'a'
	}
	if lower {
		code = strings.ToUpper(code)
	}
	return Currency{code: code, link: linkedCurrencies}
}

// Code returns the currency's code, "USD", or "" for the zero Currency.
func (c Currency) Code() string { return c.code }

// A CurrencyDisplay is how NumberOptions.CurrencyDisplay names the
// currency of an amount, as Intl.NumberFormat's currencyDisplay.
type CurrencyDisplay struct {
	kind  byte
	names func(k *localeKey, code string) [6]string // of CurrencyName
}

var (
	// CurrencySymbol, the default, writes the currency's symbol in the
	// locale: "$1,842.00" of dollars in English, "US$1,842.00" in British
	// English, "1.842,00 €" of euros in German.
	CurrencySymbol = &CurrencyDisplay{kind: 's'}
	// CurrencyNarrowSymbol writes its narrow symbol, which other
	// currencies may share: "$1,842.00" of dollars in British English.
	CurrencyNarrowSymbol = &CurrencyDisplay{kind: 'n'}
	// CurrencyCode writes its code: "USD 1,842.00", "1.842,00 USD" in
	// German.
	CurrencyCode = &CurrencyDisplay{kind: 'c'}
	// CurrencyName writes its name in the plural form of the amount, in
	// the decimal pattern (negative amounts are never in parentheses):
	// "1,842.00 US dollars", "1,50 euro" in French. Only an app that uses
	// it links the names of currencies of CLDR's locales, about 300 KB.
	CurrencyName = &CurrencyDisplay{kind: 'l', names: currencyNames}
)

// The data of currencies, which i18n/internal/gen writes to
// fmtPackedCurrency: the patterns of amounts in slots of their own (by
// numbering system, the default then latn: the standard pattern, the
// accounting one, and the separators of amounts where they differ from
// those of numbers; then the unit patterns of amounts with the names of
// their currencies, by plural form), the symbols of each currency as
// records (its symbol and its narrow one, "" for the code and for the
// symbol), the codes and their fraction digits. The names are in
// fmtPackedCurrencyNames, records of each code's by plural form.
var currencyTables struct {
	once                  sync.Once
	pool                  pool
	symbolStarts, symbols string
	codes                 []string
	digits                string
	formatCodes, regions  []string

	namesOnce         sync.Once
	nameStarts, names string
}

func loadCurrencies() {
	loadTables()
	currencyTables.once.Do(func() {
		t := unpack(fmtPackedCurrency, packedCurrencyEntriesLen, packedCurrencyLensLen, packedCurrencyPoolLen,
			packedCurrencySymbolStartsLen, packedCurrencySymbolRecordsLen, packedCurrencyFormatCodesLen,
			packedCurrencyRegionsLen, packedCurrencyCodesLen, packedCurrencyDigitsLen)
		currencyTables.pool = newPool(t[0], t[1], t[2], fmtCurrencyStringCount)
		currencyTables.symbolStarts, currencyTables.symbols = t[3], t[4]
		currencyTables.formatCodes = strings.Fields(t[5])
		currencyTables.regions = strings.Fields(t[6])
		currencyTables.codes = strings.Fields(t[7])
		currencyTables.digits = t[8]
	})
}

// currencyFormat is how a NumberFormatter writes amounts of a currency.
type currencyFormat struct {
	// base is the locale's numbering system, and sys it with the
	// separators of amounts, which the formatter writes in.
	base *numberSystem
	sys  numberSystem
	sign string // the symbol, narrow symbol or code; "" for names
	// pat is the pattern of amounts with the sign, spaced from the number
	// as ICU spaces it, and bare is it without the spacing, for NaN and
	// the infinities. inner is the pattern that names of currencies are
	// written around.
	pat, bare, inner numberPattern
	// units, of amounts that name their currency, are the affixes of its
	// unit patterns with its name ("{0} US dollars") by plural form, and
	// forms the decimal pattern in each.
	units  *[6][2]string
	forms  *[6]numberPattern
	plural func(f *NumberFormatter, intPart, frac string, exp int) int // pluralForm
	// compact is the patterns of compact amounts with the sign (a
	// *compactStyle, which only compact notation links), and compactSys
	// sys as compact numbers group, which a compact Notation sets,
	// through compacter.
	compact    any
	compactSys numberSystem
	compacter  any       // currencyCompact, a compactAmounts
	_          [0]func() // no equality to link
}

// initCurrency makes f write amounts of the currency code.
func initCurrency(f *NumberFormatter, code string) {
	loadCurrencies()
	d := f.key.dat()
	cf := &currencyFormat{base: f.sys, sys: *f.sys, compacter: currencyCompact{}}
	var v [currencySlotCount]string
	var set [currencySlotCount]bool
	fillSlots(d.index, &currencyTables.pool, fmtCurrencyStarts[:], v[:], set[:])
	sv := v[:currencySystemSlots]
	if f.sys != d.numbers(false) {
		sv = v[currencySystemSlots : 2*currencySystemSlots]
	}
	if sv[2] != "" {
		cf.sys.decimalSep = sv[2]
	}
	if sv[3] != "" {
		cf.sys.group = sv[3]
	}
	pattern := sv[0]
	if f.o.Accounting {
		pattern = sv[1]
	}
	// A format of the currency's own, else of the currency of the region
	// the locale names, as ICU's DecimalFormatSymbols keep it for any
	// currency: its pattern, also of accounting and of names (with the
	// symbol), and its separators.
	own := currencyFormatOf(v[:], code)
	if own == "" {
		own = currencyFormatOf(v[:], regionCurrency(f.key.tagRegion))
	}
	if own != "" {
		parts := strings.SplitN(own, "\x00", 3)
		pattern, cf.sys.decimalSep, cf.sys.group = parts[0], parts[1], parts[2]
	}
	f.sys = &cf.sys
	f.o.Percent = false
	i, known := search(currencyTables.codes, code)
	display := f.o.CurrencyDisplay
	if display == nil {
		display = CurrencySymbol
	}
	cf.sign = code
	if display.kind != 'c' && known {
		var sym [2]string
		fillRecords(d.index, currencyTables.symbolStarts, currencyTables.symbols, 2*i, sym[:])
		if sym[0] != "" {
			cf.sign = sym[0]
		}
		if display.kind == 'n' && sym[1] != "" {
			cf.sign = sym[1]
		}
	}
	if display.names == nil || own != "" {
		p := parseNumberPattern(pattern, f.sys)
		cf.bare, cf.pat = p, p
		cf.bare.prefix, cf.pat.prefix = cf.place(p.prefix, true, false), cf.place(p.prefix, true, true)
		cf.bare.suffix, cf.pat.suffix = cf.place(p.suffix, false, false), cf.place(p.suffix, false, true)
		cf.bare.negPrefix, cf.pat.negPrefix = cf.place(p.negPrefix, true, false), cf.place(p.negPrefix, true, true)
		cf.bare.negSuffix, cf.pat.negSuffix = cf.place(p.negSuffix, false, false), cf.place(p.negSuffix, false, true)
		cf.inner = cf.pat
	} else {
		cf.inner = cf.sys.decimal
	}
	if display.names != nil {
		// The decimal pattern (or the currency's own), in the unit
		// pattern of the plural form with its name, the pattern of
		// "other" where the locale has none of the form, as ICU fills
		// them since CLDR 46.
		names := display.names(f.key, code)
		cf.units, cf.forms, cf.plural = new([6][2]string), new([6]numberPattern), pluralForm
		units := v[2*currencySystemSlots : 2*currencySystemSlots+len(cf.units)]
		for p := range cf.units {
			unit, name := units[p], names[p]
			if unit == "" {
				unit = units[len(units)-1]
			}
			prefix, suffix, _ := strings.Cut(unit, "{0}")
			prefix = strings.Replace(prefix, "{1}", name, 1)
			suffix = strings.Replace(suffix, "{1}", name, 1)
			cf.units[p] = [2]string{prefix, suffix}
			pat := cf.inner
			pat.prefix, pat.suffix = prefix+pat.prefix, pat.suffix+suffix
			pat.negPrefix, pat.negSuffix = prefix+pat.negPrefix, pat.negSuffix+suffix
			cf.forms[p] = pat
		}
		cf.pat = cf.forms[len(cf.forms)-1]
		cf.bare = cf.pat
	}
	f.pat = &cf.pat
	f.cur = cf
	// The fraction digits: the currency's, as CLDR's currencyData gives
	// them, unless the options give some; with them, the currency's for
	// the bound they leave out, as Intl.NumberFormat takes them. Compact
	// amounts round as compact numbers do.
	if f.o.Notation != nil || f.o.Integer {
		return
	}
	digits := 2
	if known {
		digits = int(currencyTables.digits[i] - '0')
	}
	hasMin, hasMax := f.o.MinFractionDigits > 0, f.o.MaxFractionDigits > 0
	switch {
	case hasMin && hasMax:
	case hasMin:
		f.max = max(digits, f.min)
	case hasMax:
		f.min = min(digits, f.max)
	default:
		f.min, f.max = digits, digits
	}
}

// pluralForm returns the index of the plural category of a number as
// written, in compact notation with the exponent exp when it is not 0.
func pluralForm(f *NumberFormatter, intPart, frac string, exp int) int {
	num := intPart
	if frac != "" {
		num += "." + frac
	}
	if exp != 0 {
		num += "c" + strconv.Itoa(exp)
	}
	o, _ := parseOperands(num)
	return int(PluralCategory(f.key.dat().tag, o))
}

// place writes the sign in place of ¤ in an affix, before the number or
// after it: when spaced and the sign is next to the number, with a
// no-break space between them if the sign's character next to it is
// neither a symbol nor a separator, as ICU spaces currencies (CLDR's
// currencySpacing, the same in every locale, before the number's digit).
func (cf *currencyFormat) place(affix string, before, spaced bool) string {
	i := strings.Index(affix, "¤")
	if i < 0 {
		return affix
	}
	sign := cf.sign
	if spaced && sign != "" {
		switch {
		case before && i+len("¤") == len(affix):
			if r, _ := utf8.DecodeLastRuneInString(sign); !unicode.In(r, unicode.S, unicode.Z) {
				sign += "\u00a0"
			}
		case !before && i == 0:
			if r, _ := utf8.DecodeRuneInString(sign); !unicode.In(r, unicode.S, unicode.Z) {
				sign = "\u00a0" + sign
			}
		}
	}
	return affix[:i] + sign + affix[i+len("¤"):]
}

// currencyFormatOf returns the format of its own of a currency of the
// slots of a locale: its pattern, decimal and grouping separators,
// separated by NULs, or "".
func currencyFormatOf(v []string, code string) string {
	if code == "" {
		return ""
	}
	for i, c := range currencyTables.formatCodes {
		if c == code {
			return v[currencyFormatsSlot+i]
		}
	}
	return ""
}

// regionCurrency returns the currency of a region where it has a format
// of its own in some locale, else "".
func regionCurrency(region string) string {
	for _, rc := range currencyTables.regions {
		if r, c, _ := strings.Cut(rc, ":"); r == region {
			return c
		}
	}
	return ""
}

// currencyNames returns the names of a currency in a locale by plural
// form, as ICU names them: the name of the form, else of "other", else
// its displayName, else the code.
func currencyNames(k *localeKey, code string) [6]string {
	loadCurrencies()
	currencyTables.namesOnce.Do(func() {
		t := unpack(fmtPackedCurrencyNames, packedCurrencyNameStartsLen, packedCurrencyNameRecordsLen)
		currencyTables.nameStarts, currencyTables.names = t[0], t[1]
	})
	var v [6]string
	if i, ok := search(currencyTables.codes, code); ok {
		fillRecords(k.data, currencyTables.nameStarts, currencyTables.names, i*len(v), v[:])
	}
	other := &v[len(v)-1]
	if *other == "" {
		*other = code
	}
	for p := range v {
		if v[p] == "" {
			v[p] = *other
		}
	}
	return v
}
