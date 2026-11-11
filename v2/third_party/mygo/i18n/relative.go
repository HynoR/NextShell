package i18n

import (
	"math"
	"strconv"
	"sync"
	"sync/atomic"
)

// RelativeTimeUnit is the unit of a relative time. The caller chooses
// the unit and computes its value; months and years are calendar units,
// not fixed durations.
type RelativeTimeUnit uint8

const (
	RelativeSecond RelativeTimeUnit = iota
	RelativeMinute
	RelativeHour
	RelativeDay
	RelativeWeek
	RelativeMonth
	RelativeQuarter
	RelativeYear
)

// RelativeTimeStyle is how a relative time names its unit.
type RelativeTimeStyle uint8

const (
	// RelativeLong writes full names: "3 minutes ago".
	RelativeLong RelativeTimeStyle = iota
	// RelativeShort abbreviates names: "3 min. ago" in English.
	RelativeShort
	// RelativeNarrow writes the shortest names: "3m ago" in English.
	RelativeNarrow
)

// RelativeTimeOptions are how FormatRelativeTime writes a relative time.
// The zero value writes full unit names and always includes a number,
// as Intl.RelativeTimeFormat does by default.
type RelativeTimeOptions struct {
	Style RelativeTimeStyle
	// Auto uses words such as "yesterday", "today", "tomorrow" and "now"
	// where the locale has them, as numeric "auto" does in Intl. Other
	// values still include a number. As ICU does, it rounds the offset
	// to hundredths before looking for a word for an integer from -2 to 2.
	Auto bool
}

// FormatRelativeTime writes value units relative to now in the way of
// locale: -3 minutes is "3 minutes ago", 2 days is "in 2 days". It uses
// CLDR's words, plural forms, patterns and digits, as Intl.RelativeTimeFormat
// does. Numbers round to at most three fraction digits, ties to even;
// negative zero means the past. It starts no timer and chooses no unit for the caller.
//
// It panics for NaN, infinities, or an unknown unit or style.
func FormatRelativeTime(locale string, value float64, unit RelativeTimeUnit, o RelativeTimeOptions) string {
	return NewRelativeTimeFormatter(locale, o).Format(value, unit)
}

// A RelativeTimeFormatter writes relative times in a locale with options.
// It is safe for concurrent use. FormatRelativeTime keeps one per locale
// and options. Only apps formatting relative times link their CLDR data.
type RelativeTimeFormatter struct {
	data   *relativeLocale
	number *NumberFormatter
	o      RelativeTimeOptions
}

type relativeFormatterKey struct {
	locale string
	o      RelativeTimeOptions
}

var relativeFormatters struct {
	mu sync.RWMutex
	m  map[relativeFormatterKey]*RelativeTimeFormatter
}

// NewRelativeTimeFormatter returns the formatter of locale and o.
// It panics for an unknown style.
func NewRelativeTimeFormatter(locale string, o RelativeTimeOptions) *RelativeTimeFormatter {
	if o.Style > RelativeNarrow {
		panic("i18n: unknown relative time style")
	}
	key := relativeFormatterKey{locale, o}
	relativeFormatters.mu.RLock()
	f := relativeFormatters.m[key]
	relativeFormatters.mu.RUnlock()
	if f != nil {
		return f
	}
	k := keyOf(locale)
	f = &RelativeTimeFormatter{data: relativeOf(k.dat()), number: NewNumberFormatter(locale, NumberOptions{HalfEven: true}), o: o}
	relativeFormatters.mu.Lock()
	if relativeFormatters.m == nil || len(relativeFormatters.m) > 512 {
		relativeFormatters.m = map[relativeFormatterKey]*RelativeTimeFormatter{}
	}
	relativeFormatters.m[key] = f
	relativeFormatters.mu.Unlock()
	return f
}

// Format writes value units relative to now (see FormatRelativeTime).
func (f *RelativeTimeFormatter) Format(value float64, unit RelativeTimeUnit) string {
	if unit > RelativeYear {
		panic("i18n: unknown relative time unit")
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		panic("i18n: relative time must be finite")
	}
	base := (int(f.o.Style)*(int(RelativeYear)+1) + int(unit)) * relativeUnitSlots
	// ICU rounds offsets to hundredths before choosing a word.
	if f.o.Auto && value > -2.1 && value < 2.1 {
		hundredths := int(math.Round(value * 100))
		if hundredths%100 == 0 {
			if word := f.data[base+hundredths/100+2]; word != "" {
				return word
			}
		}
	}
	dir := 1
	if math.Signbit(value) {
		dir = 0
	}
	// Pick the plural form from the rounded number that will be written,
	// as ICU's number formatter does, not the unrounded input.
	decimal := strconv.FormatFloat(math.Abs(value), 'f', -1, 64)
	_, integer, fraction := splitDecimal(decimal)
	integer, fraction = f.number.round(integer, fraction, f.number.max)
	form := pluralForm(f.number, integer, fraction, 0)
	pattern := f.data[base+5+dir*6+form]
	return simpleFormat(pattern, f.number.formatDecimal(decimal))
}

type relativeLocale [relativeSlotCount]string

var relativeTables struct {
	once            sync.Once
	starts, records string
}

var relativeLocales [fmtLocaleCount]atomic.Pointer[relativeLocale]

func relativeOf(d *localeData) *relativeLocale {
	if l := relativeLocales[d.index].Load(); l != nil {
		return l
	}
	relativeTables.once.Do(func() {
		t := unpack(fmtPackedRelative, packedRelativeStartsLen, packedRelativeRecordsLen)
		relativeTables.starts, relativeTables.records = t[0], t[1]
	})
	l := new(relativeLocale)
	fillRecords(d.index, relativeTables.starts, relativeTables.records, 0, l[:])
	if !relativeLocales[d.index].CompareAndSwap(nil, l) {
		return relativeLocales[d.index].Load()
	}
	return l
}
