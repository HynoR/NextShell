package i18n

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Ranges of dates, as Intl.DateTimeFormat's formatRange writes them: what
// ICU's DateIntervalFormat makes of CLDR's intervalFormats, which follows
// its code (dtitvfmt.cpp, dtitvinf.cpp), as V8 calls it.

// FormatDateRange writes the range of times from start to end, in start's
// location, in the way of locale and options, as Intl.DateTimeFormat's
// formatRange does: once what they share and both what they do not, "Jan
// 5 – 9, 2026", "Jan 5 – Feb 9, 2026", "10:00 – 11:30 AM", or one date
// when they differ in none of the fields the options write. A date-range
// picker's trigger labels its range so.
//
// As V8 does, a range keeps the spaces of CLDR's patterns, thin ones
// around the dash in English (U+2009) and narrow no-break ones before AM
// and PM (U+202F), which Format writes as spaces.
func FormatDateRange(locale string, start, end time.Time, o DateOptions) string {
	return NewDateFormatter(locale, o).FormatRange(start, end)
}

// The fields of the patterns of intervals, ICU's largest first: the
// field where two times first differ picks the pattern.
const (
	ivEra = iota
	ivYear
	ivMonth
	ivDate
	ivAmPm
	ivHour
	ivMinute
	ivSecond
	ivMillisecond
	ivCount
)

// ivLetters are the pattern letters of the fields, and ivLevels their
// levels, ICU's fgCalendarFieldToLevel.
const ivLetters = "GyMdahms"

var ivLevels = [ivCount]int8{0, 10, 20, 30, 40, 50, 60, 70, 80}

// rangeFormat is how a DateFormatter writes ranges: a pattern per field
// the times first differ in, in two parts, the first for one time and the
// second for the other, or only a second, the pattern of the fallback,
// and the patterns of the fallback.
type rangeFormat struct {
	parts [ivCount]rangePart
	// full writes a time whole, date and time separately; dateTime puts
	// a date and a range of times together, fallback two times.
	full, date, time *DateFormatter
	dateTime         string
	fallback         string
	laterFirst       bool // the order of the fallback, {1} before {0}
}

type rangePart struct {
	first, second *DateFormatter
	laterFirst    bool
}

// intervalTables are the patterns of intervals of the locales, inflated
// on first use.
var intervalTables struct {
	once      sync.Once
	pool      pool
	skeletons []string
}

var intervalLocales [fmtLocaleCount]atomic.Pointer[intervalLocale]

// intervalLocale is the patterns of intervals of a locale: the fallback,
// then those of the skeletons of fmtIntervalSkeletons by the fields of
// fmtIntervalFields, and which skeletons it has patterns of.
type intervalLocale struct {
	v   [intervalSlotCount]string
	has []bool
}

func intervalsOf(d *localeData) *intervalLocale {
	if l := intervalLocales[d.index].Load(); l != nil {
		return l
	}
	loadTables()
	intervalTables.once.Do(func() {
		t := unpack(fmtPackedInterval, packedIntervalEntriesLen, packedIntervalLensLen, packedIntervalPoolLen)
		intervalTables.pool = newPool(t[0], t[1], t[2], fmtIntervalStringCount)
		intervalTables.skeletons = strings.Fields(fmtIntervalSkeletons)
	})
	l := &intervalLocale{has: make([]bool, len(intervalTables.skeletons))}
	var set [intervalSlotCount]bool
	fillSlots(d.index, &intervalTables.pool, fmtIntervalStarts[:], l.v[:], set[:])
	for i := range l.has {
		for j := range len(fmtIntervalFields) {
			if l.v[1+i*len(fmtIntervalFields)+j] != "" {
				l.has[i] = true
			}
		}
	}
	if !intervalLocales[d.index].CompareAndSwap(nil, l) {
		return intervalLocales[d.index].Load()
	}
	return l
}

// pattern returns the locale's pattern of a skeleton, by its index, for
// a field, or "": ICU's getIntervalPattern. Day periods are "B" or "a",
// hours "H" or "h": the first in ICU's order of keys, which it keeps.
func (l *intervalLocale) pattern(skel, field int) string {
	if skel < 0 {
		return ""
	}
	at := func(c byte) string {
		return l.v[1+skel*len(fmtIntervalFields)+strings.IndexByte(fmtIntervalFields, c)]
	}
	switch field {
	case ivAmPm:
		if p := at('B'); p != "" {
			return p
		}
		return at('a')
	case ivHour:
		if p := at('H'); p != "" {
			return p
		}
		return at('h')
	case ivSecond, ivMillisecond:
		return ""
	}
	return at(ivLetters[field])
}

// FormatRange writes the range from start to end (see FormatDateRange).
func (f *DateFormatter) FormatRange(start, end time.Time) string {
	r := f.ranges.Load()
	if r == nil {
		r = newRangeFormat(f)
		if !f.ranges.CompareAndSwap(nil, r) {
			r = f.ranges.Load()
		}
	}
	end = end.In(start.Location())
	field := ivCount
	switch {
	case (start.Year() > 0) != (end.Year() > 0):
		field = ivEra
	case start.Year() != end.Year():
		field = ivYear
	case start.Month() != end.Month():
		field = ivMonth
	case start.Day() != end.Day():
		field = ivDate
	case (start.Hour() >= 12) != (end.Hour() >= 12):
		field = ivAmPm
	case start.Hour()%12 != end.Hour()%12:
		field = ivHour
	case start.Minute() != end.Minute():
		field = ivMinute
	case start.Second() != end.Second():
		field = ivSecond
	case start.Nanosecond()/1e6 != end.Nanosecond()/1e6:
		field = ivMillisecond
	}
	if field == ivCount {
		// One date, as V8 writes it when ICU's has no range.
		return f.Format(start)
	}
	sameDay := field >= ivAmPm
	p := &r.parts[field]
	switch {
	case p.first == nil && p.second == nil:
		if fieldIgnored(r.full.pattern, field) {
			return f.Format(start)
		}
		return r.fallbackFormat(r.full, start, end, sameDay)
	case p.first == nil:
		return r.fallbackFormat(p.second, start, end, sameDay)
	}
	first, second := start, end
	if p.laterFirst {
		first, second = end, start
	}
	var b strings.Builder
	p.first.write(&b, first)
	if p.second != nil {
		p.second.write(&b, second)
	}
	return b.String()
}

// fallbackFormat writes a range in the fallback pattern: a date and the
// range of times when they are on the same day, else the two times,
// written with full.
func (r *rangeFormat) fallbackFormat(full *DateFormatter, start, end time.Time, sameDay bool) string {
	if sameDay && r.date != nil && r.time != nil {
		var date strings.Builder
		r.date.write(&date, start)
		return simpleFormat(r.dateTime, r.fallbackRange(r.time, start, end), date.String())
	}
	return r.fallbackRange(full, start, end)
}

func (r *rangeFormat) fallbackRange(f *DateFormatter, start, end time.Time) string {
	var a, b strings.Builder
	f.write(&a, start)
	f.write(&b, end)
	return simpleFormat(r.fallback, a.String(), b.String())
}

// simpleFormat puts its arguments for {0} and {1} in a pattern of ICU's
// SimpleFormatter, whose apostrophes quote only braces.
func simpleFormat(pattern string, args ...string) string {
	var b strings.Builder
	quoted := false
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		switch {
		case c == '\'' && i+1 < len(pattern) && pattern[i+1] == '\'':
			b.WriteByte('\'')
			i++
		case c == '\'' && quoted:
			quoted = false
		case c == '\'' && i+1 < len(pattern) && (pattern[i+1] == '{' || pattern[i+1] == '}'):
			quoted = true
		case c == '{' && !quoted && i+2 < len(pattern) && pattern[i+2] == '}' && pattern[i+1] >= '0' && int(pattern[i+1]-'0') < len(args):
			b.WriteString(args[pattern[i+1]-'0'])
			i += 2
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// newRangeFormat makes the patterns of ranges of f, as ICU's
// DateIntervalFormat::initializePattern does for the skeleton of its
// pattern, in the hour cycle of its pattern.
func newRangeFormat(f *DateFormatter) *rangeFormat {
	b := &rangeBuilder{f: f, l: intervalsOf(f.data)}
	b.gen = *f.data.generator()
	b.gen.patternHours = true
	b.gen.defaultHour = patternHour(f.pattern)
	if b.gen.defaultHour == 0 {
		b.gen.defaultHour = f.key.defaultHour()
	}
	r := &b.r
	r.fallback = b.l.v[0]
	r.laterFirst = strings.Index(r.fallback, "{1}") < strings.Index(r.fallback, "{0}")
	b.skeleton = patternSkeleton(f.pattern)
	r.full = b.formatter(b.gen.bestPattern(b.skeleton))
	b.normalizeHours()
	date, normDate, tm, normTime := dateTimeSkeletons(b.skeleton)
	if tm != "" && date != "" {
		r.dateTime = f.data.v[slotDateTimeFormats+MediumStyle.index()]
	}
	found := b.separateDateTime(normDate, normTime)
	switch {
	case tm == "":
	case date == "" || !found:
		if date == "" {
			// Times of different days in a pattern with their date.
			p := b.formatter(b.gen.bestPattern("yMd" + tm))
			r.parts[ivDate] = rangePart{second: p, laterFirst: r.laterFirst}
			r.parts[ivMonth] = r.parts[ivDate]
			r.parts[ivYear] = r.parts[ivDate]
			r.parts[ivEra] = rangePart{second: b.formatter(b.gen.bestPattern("GyMd" + tm)), laterFirst: r.laterFirst}
		}
	default:
		// Dates that differ in the fields of the date: both whole; times
		// of a day: the date and their range.
		skel := b.skeleton
		for _, field := range [...]int{ivDate, ivMonth, ivYear, ivEra} {
			if !strings.ContainsRune(date, rune(ivLetters[field])) {
				skel = ivLetters[field:field+1] + skel
				r.parts[field] = rangePart{second: b.formatter(b.gen.bestPattern(skel)), laterFirst: r.laterFirst}
			}
		}
		datePattern := b.gen.bestPattern(date)
		for _, field := range [...]int{ivAmPm, ivHour, ivMinute} {
			b.concatDate(datePattern, field)
		}
	}
	return r
}

// rangeBuilder makes a rangeFormat.
type rangeBuilder struct {
	r        rangeFormat
	f        *DateFormatter
	l        *intervalLocale
	gen      patternGenerator
	skeleton string
}

// formatter returns a formatter of a pattern in f's locale.
func (b *rangeBuilder) formatter(pattern string) *DateFormatter {
	g := &DateFormatter{key: b.f.key, data: b.f.data, sys: b.f.sys}
	g.setPattern(pattern)
	return g
}

// normalizeHours gives the skeleton the hour letter of the locale's
// patterns of its hour cycle, and the day period of a 12-hour one, as
// ICU's normalizeHourMetacharacters does.
func (b *rangeBuilder) normalizeHours() {
	s := b.skeleton
	var hourChar, periodChar byte
	hourStart, hourLen, periodStart, periodLen := 0, 0, 0, 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case 'j', 'J', 'C', 'h', 'H', 'k', 'K':
			if hourChar == 0 {
				hourChar, hourStart = c, i
			}
			hourLen++
		case 'a', 'b', 'B':
			if periodChar == 0 {
				periodChar, periodStart = c, i
			}
			periodLen++
		default:
			if hourChar != 0 && periodChar != 0 {
				i = len(s)
			}
		}
	}
	if hourChar == 0 {
		return
	}
	hour := byte('H')
	quoted := false
	p := b.gen.bestPattern(string(hourChar))
	for i := 0; i < len(p); i++ {
		if c := p[i]; c == '\'' {
			quoted = !quoted
		} else if !quoted && (c == 'h' || c == 'H' || c == 'k' || c == 'K') {
			hour = c
			break
		}
	}
	field := string(hour)
	if hour != 'H' && hour != 'k' {
		if periodChar == 0 {
			periodChar = 'a'
		}
		n := 1
		switch {
		case periodLen >= 5 || hourLen >= 5:
			n = 5
		case periodLen >= 3 || hourLen >= 3:
			n = 3
		}
		field += strings.Repeat(string(periodChar), n)
	}
	s = s[:hourStart] + field + s[hourStart+hourLen:]
	if periodLen > 0 {
		if periodStart > hourStart {
			periodStart += len(field) - hourLen
		}
		s = s[:periodStart] + s[periodStart+periodLen:]
	}
	b.skeleton = s
}

// dateTimeSkeletons splits a skeleton into its date and its time, and
// their forms of CLDR's skeletons of intervals, as ICU's
// getDateTimeSkeleton does.
func dateTimeSkeletons(skel string) (date, normDate, tm, normTime string) {
	var d, nd, t, nt strings.Builder
	var e, dd, mo, y, mi, v, z int
	var hour byte
	for i := 0; i < len(skel); i++ {
		c := skel[i]
		switch c {
		case 'E':
			d.WriteByte(c)
			e++
		case 'd':
			d.WriteByte(c)
			dd++
		case 'M':
			d.WriteByte(c)
			mo++
		case 'y':
			d.WriteByte(c)
			y++
		case 'G', 'Y', 'u', 'Q', 'q', 'L', 'l', 'W', 'w', 'D', 'F', 'g', 'e', 'c', 'U', 'r':
			nd.WriteByte(c)
			d.WriteByte(c)
		case 'a':
			t.WriteByte(c)
		case 'h', 'H', 'k', 'K':
			t.WriteByte(c)
			if hour == 0 {
				hour = c
			}
		case 'm':
			t.WriteByte(c)
			mi++
		case 'z':
			z++
			t.WriteByte(c)
		case 'v':
			v++
			t.WriteByte(c)
		case 'V', 'Z', 'j', 's', 'S', 'A', 'b', 'B':
			t.WriteByte(c)
			nt.WriteByte(c)
		}
	}
	nd.WriteString(strings.Repeat("y", y))
	switch {
	case mo >= 3:
		nd.WriteString(strings.Repeat("M", min(mo, 5)))
	case mo > 0:
		nd.WriteByte('M')
	}
	switch {
	case e > 3:
		nd.WriteString(strings.Repeat("E", min(e, 5)))
	case e > 0:
		nd.WriteByte('E')
	}
	if dd > 0 {
		nd.WriteByte('d')
	}
	if hour != 0 {
		nt.WriteByte(hour)
	}
	if mi > 0 {
		nt.WriteByte('m')
	}
	if z > 0 {
		nt.WriteByte('z')
	}
	if v > 0 {
		nt.WriteByte('v')
	}
	return d.String(), nd.String(), t.String(), nt.String()
}

// separateDateTime is ICU's setSeparateDateTimePtn: it sets the patterns
// of the fields of a date, or of a time, from those of the skeleton of
// CLDR nearest it, and reports whether there is one with its fields.
func (b *rangeBuilder) separateDateTime(date, tm string) bool {
	skel := tm
	if skel == "" {
		skel = date
	}
	best, diff := b.bestSkeleton(skel)
	if best < 0 {
		return false
	}
	if date != "" {
		b.r.date = b.formatter(b.gen.bestPattern(date))
	}
	if tm != "" {
		b.r.time = b.formatter(b.gen.bestPattern(tm))
	}
	if diff == -1 {
		return false
	}
	bestSkel := intervalTables.skeletons[best]
	if tm == "" {
		var ext extended
		b.intervalPattern(ivDate, skel, bestSkel, best, diff, &ext)
		if b.intervalPattern(ivMonth, skel, bestSkel, best, diff, &ext) {
			skel, bestSkel, best = ext.skel, ext.best, ext.bestIndex
		}
		b.intervalPattern(ivYear, skel, bestSkel, best, diff, &ext)
		b.intervalPattern(ivEra, skel, bestSkel, best, diff, &ext)
	} else {
		b.intervalPattern(ivMinute, skel, bestSkel, best, diff, nil)
		b.intervalPattern(ivHour, skel, bestSkel, best, diff, nil)
		b.intervalPattern(ivAmPm, skel, bestSkel, best, diff, nil)
	}
	return true
}

// extended is the skeleton and the best one with a larger field, which
// ICU's setIntervalPattern looks for a pattern of a field in.
type extended struct {
	skel, best string
	bestIndex  int
}

// intervalPattern is ICU's setIntervalPattern: it sets the pattern of a
// field from that of the best skeleton, adjusted to the widths of skel,
// or of the best skeleton with the field (yMMMM for a year of MMMM). It
// reports whether it extended the skeleton.
func (b *rangeBuilder) intervalPattern(field int, skel, bestSkel string, best, diff int, ext *extended) bool {
	pattern := b.l.pattern(best, field)
	if pattern == "" {
		if fieldIgnored(bestSkel, field) {
			return false
		}
		if field == ivAmPm {
			// 24-hour skeletons have no pattern of day periods: that of
			// hours is theirs.
			if pattern = b.l.pattern(best, ivHour); pattern != "" {
				b.setPart(field, adjustFieldWidth(skel, bestSkel, pattern, diff))
			}
			return false
		}
		if ext != nil {
			letter := ivLetters[field : field+1]
			ext.skel, ext.best = letter+skel, letter+bestSkel
			ext.bestIndex = b.l.skeletonIndex(ext.best)
			pattern = b.l.pattern(ext.bestIndex, field)
			if pattern == "" && diff == 0 {
				if tmp, d := b.bestSkeleton(ext.best); tmp >= 0 && d != -1 {
					diff = d
					pattern = b.l.pattern(tmp, field)
					bestSkel = intervalTables.skeletons[tmp]
				}
			}
		}
	}
	if pattern == "" {
		return false
	}
	if diff != 0 {
		pattern = adjustFieldWidth(skel, bestSkel, pattern, diff)
	}
	b.setPart(field, pattern)
	return ext != nil && ext.skel != ""
}

// skeletonIndex returns the index of a skeleton the locale has patterns
// of, or -1.
func (l *intervalLocale) skeletonIndex(skel string) int {
	for i, s := range intervalTables.skeletons {
		if s == skel && l.has[i] {
			return i
		}
	}
	return -1
}

// setPart sets the pattern of a field, split into its two parts, in the
// order its prefix names or the fallback's.
func (b *rangeBuilder) setPart(field int, pattern string) {
	later := b.r.laterFirst
	if p, ok := strings.CutPrefix(pattern, "latestFirst:"); ok {
		pattern, later = p, true
	} else if p, ok := strings.CutPrefix(pattern, "earliestFirst:"); ok {
		pattern, later = p, false
	}
	i := splitInterval(pattern)
	part := rangePart{first: b.formatter(pattern[:i]), laterFirst: later}
	if i < len(pattern) {
		part.second = b.formatter(pattern[i:])
	}
	b.r.parts[field] = part
}

// concatDate is ICU's concatSingleDate2TimeInterval: the pattern of a
// field of times of a day becomes the date with their range.
func (b *rangeBuilder) concatDate(datePattern string, field int) {
	p := &b.r.parts[field]
	if p.first == nil {
		return
	}
	tm := p.first.pattern
	if p.second != nil {
		tm += p.second.pattern
	}
	later := p.laterFirst
	b.setPart(field, simpleFormat(b.r.dateTime, tm, datePattern))
	b.r.parts[field].laterFirst = later
}

// splitInterval returns where the second part of a pattern of an interval
// starts: at its first field that repeats one before it, ICU's
// splitPatternInto2Part.
func splitInterval(p string) int {
	var seen [128]bool
	var prev byte
	count := 0
	quoted := false
	i := 0
	repeated := false
	for ; i < len(p); i++ {
		c := p[i]
		if c != prev && count > 0 {
			if seen[prev] {
				repeated = true
				break
			}
			seen[prev] = true
			count = 0
		}
		switch {
		case c == '\'':
			if i+1 < len(p) && p[i+1] == '\'' {
				i++
			} else {
				quoted = !quoted
			}
		case !quoted && (c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'):
			prev = c
			count++
		}
	}
	if count > 0 && !repeated && !seen[prev] {
		count = 0
	}
	return i - count
}

// skeletonWidths counts the letters of a skeleton, ICU's parseSkeleton.
func skeletonWidths(s string) (w [58]int) {
	for i := 0; i < len(s); i++ {
		if c := s[i]; c >= 'A' && c <= 'z' {
			w[c-'A']++
		}
	}
	return w
}

// bestSkeleton returns the index of the locale's skeleton of intervals
// nearest skel, or -1, and how they differ: 0 not at all, 1 in the
// widths of fields, 2 in hour letters, day periods or zones only, -1 in
// fields; ICU's getBestSkeleton.
func (b *rangeBuilder) bestSkeleton(skel string) (best, diff int) {
	replaced := false
	if strings.ContainsAny(skel, "zkKab") {
		skel = strings.NewReplacer("z", "v", "k", "H", "K", "h", "a", "", "b", "B").Replace(skel)
		replaced = true
	}
	in := skeletonWidths(skel)
	best, bestDist := -1, 1<<31-1
	for i, s := range intervalTables.skeletons {
		if !b.l.has[i] {
			continue
		}
		w := skeletonWidths(s)
		dist, d := 0, 1
		for j := range in {
			a, c := in[j], w[j]
			switch {
			case a == c:
			case a == 0 || c == 0:
				d = -1
				dist += 0x1000
			case j == 'M'-'A' && (a <= 2) != (c <= 2):
				dist += 0x100
			case a > c:
				dist += a - c
			default:
				dist += c - a
			}
		}
		if dist < bestDist {
			best, bestDist, diff = i, dist, d
		}
		if dist == 0 {
			diff = 0
			break
		}
	}
	if replaced && diff != -1 {
		diff = 2
	}
	return best, diff
}

// adjustFieldWidth is ICU's adjustFieldWidth: the fields of a pattern of
// the best skeleton as wide as skel asks, its hour letters, day period
// and zone those of skel.
func adjustFieldWidth(skel, bestSkel, pattern string, diff int) string {
	in := skeletonWidths(skel)
	best := skeletonWidths(bestSkel)
	if diff == 2 {
		r := map[byte]byte{}
		if strings.IndexByte(skel, 'z') >= 0 {
			best['z'-'A'] = best['v'-'A']
			r['v'] = 'z'
		}
		if strings.IndexByte(skel, 'K') >= 0 {
			r['h'] = 'K'
		}
		if strings.IndexByte(skel, 'k') >= 0 {
			r['H'] = 'k'
		}
		if strings.IndexByte(skel, 'b') >= 0 {
			r['a'] = 'b'
		}
		pattern = replaceLetters(pattern, r)
	}
	if strings.IndexByte(pattern, 'a') >= 0 && best['a'-'A'] == 0 {
		best['a'-'A'] = 1
	}
	if strings.IndexByte(pattern, 'b') >= 0 && best['b'-'A'] == 0 {
		best['b'-'A'] = 1
	}
	var out strings.Builder
	var prev byte
	count := 0
	quoted := false
	widen := func() {
		c := prev
		if c == 'L' {
			c = 'M'
		}
		if n, want := best[c-'A'], in[c-'A']; n == count && want > n {
			out.WriteString(strings.Repeat(string(prev), want-n))
		}
	}
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		if c != prev && count > 0 {
			widen()
			count = 0
		}
		switch {
		case c == '\'':
			if i+1 < len(pattern) && pattern[i+1] == '\'' {
				out.WriteByte(c)
				i++
			} else {
				quoted = !quoted
			}
		case !quoted && (c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'):
			prev = c
			count++
		}
		out.WriteByte(c)
	}
	if count > 0 {
		widen()
	}
	return out.String()
}

// replaceLetters replaces the letters of a pattern's fields, outside its
// quoted text.
func replaceLetters(p string, r map[byte]byte) string {
	if len(r) == 0 {
		return p
	}
	b := []byte(p)
	quoted := false
	for i, c := range b {
		if c == '\'' {
			quoted = !quoted
		} else if to, ok := r[c]; ok && !quoted {
			b[i] = to
		}
	}
	return string(b)
}

// fieldIgnored reports whether a pattern or skeleton writes no field as
// small as field or smaller, so that times differing in it write as one:
// ICU's isFieldUnitIgnored.
func fieldIgnored(p string, field int) bool {
	level := ivLevels[field]
	quoted := false
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch {
		case c == '\'':
			if i+1 < len(p) && p[i+1] == '\'' {
				i++
			} else {
				quoted = !quoted
			}
		case !quoted && (c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'):
			if level <= letterLevel(c) {
				return false
			}
		}
	}
	return true
}

// letterLevel is ICU's level of a pattern letter: the larger, the smaller
// its unit.
func letterLevel(c byte) int8 {
	switch c {
	case 'G':
		return 0
	case 'y', 'Y', 'u', 'U', 'r':
		return 10
	case 'M', 'L', 'Q', 'q', 'w':
		return 20
	case 'd', 'D', 'E', 'e', 'c', 'F', 'g', 'W':
		return 30
	case 'a', 'b', 'B':
		return 40
	case 'h', 'H', 'k', 'K':
		return 50
	case 'm':
		return 60
	case 's':
		return 70
	case 'S', 'A':
		return 80
	case 'z', 'Z', 'v', 'V', 'O', 'X', 'x':
		return 0
	}
	return -1
}
