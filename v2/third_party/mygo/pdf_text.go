package mygo

import (
	"bytes"
	"compress/zlib"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"
)

// The PDFs that WebKit prints on macOS name radicals in their text in place
// of many Chinese characters, so copying "人工智能" from one gives "⼈⼯智能"
// and searching it for "人工" finds nothing. The system's Chinese fonts
// draw many radicals with the glyphs of the ideographs they look like, ⼈
// (U+2F08) with that of 人, and in the ToUnicode maps, which give the
// characters of a font's glyphs, CoreGraphics names the radical. Some
// radicals have the glyph of another ideograph: Songti SC draws ⼜, the
// radical of 又, as 口, so its PDFs name ⼜ for every 口, and no table of
// the ideographs that radicals stand for would put 口 back.
//
// fixPDFText rewrites those maps: a radical becomes the ideograph its font
// draws with the same glyph, which ideographs looks up in the installed
// font of that PostScript name. The new maps are appended to the PDF as
// an incremental update, so the original bytes stay and the pages look as
// they did. It returns pdf as it is when no map has such a radical, or
// when pdf is not a PDF it reads, as one CoreGraphics did not write may
// be.
func fixPDFText(pdf []byte, ideographs func(font string, radicals []rune) map[rune]rune) []byte {
	if fixed, err := fixPDFTextMaps(pdf, ideographs); err == nil {
		return fixed
	}
	return pdf
}

// errPDF stops fixPDFText at what it does not read: damage, or syntax or
// features that CoreGraphics does not write.
var errPDF = errors.New("mygo: unsupported PDF")

// isRadical reports whether r is in CJK Radicals Supplement or Kangxi
// Radicals.
func isRadical(r rune) bool { return r >= 0x2E80 && r <= 0x2FDF }

func fixPDFTextMaps(data []byte, ideographs func(string, []rune) map[rune]rune) ([]byte, error) {
	d, err := readPDF(data)
	if err != nil {
		return nil, err
	}
	// The ToUnicode maps by object number, with the fonts that use them.
	type textMap struct {
		gen  int
		font string // the PostScript name, "" if unknown or if fonts of other names share the map
		cmap *pdfCMap
		dict pdfValue
	}
	texts := map[int]*textMap{}
	for n, x := range d.xref {
		// CoreGraphics lists unused numbers as in use at offset 0.
		if !x.used || x.offset == 0 {
			continue
		}
		obj, _, err := d.object(n, x.gen)
		if err != nil {
			return nil, err
		}
		// The objects of an object stream are out of this reader's reach.
		if t, ok := obj.get("Type"); ok && t.named("ObjStm") {
			return nil, errPDF
		}
		ref, ok := obj.get("ToUnicode")
		if !ok {
			continue
		}
		if ref.kind != pdfRef {
			return nil, errPDF
		}
		font := ""
		if base, ok := obj.get("BaseFont"); ok {
			font = postScriptName(base)
		}
		if m := texts[ref.n]; m != nil {
			if m.gen != ref.gen {
				return nil, errPDF
			}
			if m.font != font {
				m.font = ""
			}
			continue
		}
		texts[ref.n] = &textMap{gen: ref.gen, font: font}
	}
	numbers := make([]int, 0, len(texts))
	for n := range texts {
		numbers = append(numbers, n)
	}
	slices.Sort(numbers)
	radicals := map[string][]rune{}
	for _, n := range numbers {
		m := texts[n]
		if m.font == "" {
			continue
		}
		dict, end, err := d.object(n, m.gen)
		if err != nil || dict.kind != pdfDict {
			return nil, errPDF
		}
		if filter, ok := dict.get("Filter"); ok {
			if filter.kind == pdfArray && len(filter.list) == 1 {
				filter = filter.list[0]
			}
			if !filter.named("FlateDecode") {
				continue
			}
		}
		// Decode parameters would change what the stream holds.
		if params, ok := dict.get("DecodeParms"); ok && params.raw != "null" {
			continue
		}
		stream, err := d.stream(dict, end)
		if err != nil {
			return nil, err
		}
		if m.cmap, err = readToUnicode(stream); err != nil {
			return nil, err
		}
		m.dict = dict
		radicals[m.font] = append(radicals[m.font], m.cmap.radicals()...)
	}
	fonts := map[string]map[rune]rune{}
	for font, rs := range radicals {
		slices.Sort(rs)
		if rs = slices.Compact(rs); len(rs) > 0 {
			fonts[font] = ideographs(font, rs)
		}
	}
	var update bytes.Buffer
	update.WriteByte('\n')
	offsets := map[int]int{}
	for _, n := range numbers {
		m := texts[n]
		if m.cmap == nil {
			continue
		}
		cmap, changed := m.cmap.rewrite(fonts[m.font])
		if !changed {
			continue
		}
		var stream bytes.Buffer
		w := zlib.NewWriter(&stream)
		w.Write(cmap)
		if err := w.Close(); err != nil {
			return nil, err
		}
		offsets[n] = len(data) + update.Len()
		if int64(offsets[n]) > 9999999999 { // the 10 digits of an xref entry
			return nil, errPDF
		}
		fmt.Fprintf(&update, "%d %d obj\n%s /Length %d /Filter /FlateDecode >>\nstream\n", n, m.gen, m.dict.dictionaryWithout("Length", "Filter", "DecodeParms"), stream.Len())
		update.Write(stream.Bytes())
		update.WriteString("\nendstream\nendobj\n")
	}
	if len(offsets) == 0 {
		return data, nil
	}
	xref := len(data) + update.Len()
	update.WriteString("xref\n")
	for _, n := range numbers {
		if at, ok := offsets[n]; ok {
			fmt.Fprintf(&update, "%d 1\n%010d %05d n \n", n, at, texts[n].gen)
		}
	}
	fmt.Fprintf(&update, "trailer\n%s /Prev %d >>\nstartxref\n%d\n%%%%EOF\n", d.trailer.dictionaryWithout("Prev", "XRefStm"), d.start, xref)
	return append(slices.Clip(data), update.Bytes()...), nil
}

// postScriptName returns the PostScript name of a font from its BaseFont,
// without the tag that marks a subset ("ABCDEF+").
func postScriptName(base pdfValue) string {
	if base.kind != pdfName {
		return ""
	}
	name, err := pdfNameText(base.raw)
	if err != nil {
		return ""
	}
	if len(name) > 7 && name[6] == '+' && strings.Trim(name[:6], "ABCDEFGHIJKLMNOPQRSTUVWXYZ") == "" {
		name = name[7:]
	}
	return name
}

// pdfCMap is a ToUnicode map: its mappings in order, and the span of its
// bfchar and bfrange sections, which a rewrite replaces with bfchar
// sections of the same mappings.
type pdfCMap struct {
	data        []byte
	mappings    []pdfMapping
	first, last int
}

// pdfMapping maps a character code, in hexadecimal as the map writes it,
// to text.
type pdfMapping struct {
	code string
	text []rune
}

const maxPDFMappings = 1 << 20

func readToUnicode(data []byte) (*pdfCMap, error) {
	c := &pdfCMap{data: data, first: -1}
	p := pdfParser{data: data}
	add := func(code, text string) error {
		if len(c.mappings) >= maxPDFMappings {
			return errPDF
		}
		runes, err := pdfUTF16(text)
		if err != nil {
			return err
		}
		c.mappings = append(c.mappings, pdfMapping{code, runes})
		return nil
	}
	hexToken := func() (string, error) {
		t, err := p.token()
		if err != nil {
			return "", err
		}
		return pdfHex(t.text)
	}
	var previous pdfToken
	for {
		t, err := p.token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if t.text != "beginbfchar" && t.text != "beginbfrange" {
			if t.text == "endbfchar" || t.text == "endbfrange" {
				return nil, errPDF
			}
			previous = t
			continue
		}
		count, err := strconv.Atoi(previous.text)
		if err != nil || count < 0 || count > maxPDFMappings {
			return nil, errPDF
		}
		// A rewrite replaces all from the first section to the last, so
		// nothing else may be between them.
		if c.first < 0 {
			c.first = previous.start
		} else if len(bytes.TrimFunc(data[c.last:previous.start], func(r rune) bool { return r < 0x80 && pdfSpace(byte(r)) })) > 0 {
			return nil, errPDF
		}
		for range count {
			code, err := hexToken()
			if err != nil {
				return nil, err
			}
			if t.text == "beginbfchar" {
				text, err := hexToken()
				if err != nil {
					return nil, err
				}
				if err := add(code, text); err != nil {
					return nil, err
				}
				continue
			}
			high, err := hexToken()
			if err != nil || len(high) != len(code) || len(code) > 8 {
				return nil, errPDF
			}
			lo, _ := strconv.ParseUint(code, 16, 32)
			hi, _ := strconv.ParseUint(high, 16, 32)
			if hi < lo || hi-lo >= uint64(maxPDFMappings-len(c.mappings)) {
				return nil, errPDF
			}
			next, err := p.token()
			if err != nil {
				return nil, err
			}
			if next.text == "[" {
				for i := lo; i <= hi; i++ {
					text, err := hexToken()
					if err != nil {
						return nil, err
					}
					if err := add(fmt.Sprintf("%0*X", len(code), i), text); err != nil {
						return nil, err
					}
				}
				if p.expect("]") != nil {
					return nil, errPDF
				}
				continue
			}
			// The codes of the range map to the text with its last byte
			// incremented, which may not pass 255 (ISO 32000-1, 9.10.3).
			text, err := pdfHex(next.text)
			if err != nil || len(text) < 2 || len(text)%2 != 0 {
				return nil, errPDF
			}
			last, _ := strconv.ParseUint(text[len(text)-2:], 16, 8)
			if last+hi-lo > 0xFF {
				return nil, errPDF
			}
			for i := lo; i <= hi; i++ {
				if err := add(fmt.Sprintf("%0*X", len(code), i), fmt.Sprintf("%s%02X", text[:len(text)-2], last+i-lo)); err != nil {
					return nil, err
				}
			}
		}
		end, err := p.token()
		want := "endbfchar"
		if t.text == "beginbfrange" {
			want = "endbfrange"
		}
		if err != nil || end.text != want {
			return nil, errPDF
		}
		c.last = end.end
		previous = end
	}
	return c, nil
}

// radicals returns the radicals the map's texts have.
func (c *pdfCMap) radicals() []rune {
	var rs []rune
	for _, m := range c.mappings {
		for _, r := range m.text {
			if isRadical(r) {
				rs = append(rs, r)
			}
		}
	}
	return rs
}

// rewrite returns the map with the radicals of its texts replaced by their
// ideographs, and whether there were any.
func (c *pdfCMap) rewrite(ideographs map[rune]rune) ([]byte, bool) {
	changed := false
	for _, m := range c.mappings {
		for i, r := range m.text {
			if to := ideographs[r]; to != 0 && isRadical(r) {
				m.text[i] = to
				changed = true
			}
		}
	}
	if !changed {
		return nil, false
	}
	var b bytes.Buffer
	b.Write(c.data[:c.first])
	// A section has at most 100 mappings.
	for start := 0; start < len(c.mappings); start += 100 {
		chunk := c.mappings[start:min(start+100, len(c.mappings))]
		fmt.Fprintf(&b, "%d beginbfchar\n", len(chunk))
		for _, m := range chunk {
			units := utf16.Encode(m.text)
			text := make([]byte, 2*len(units))
			for i, u := range units {
				text[2*i], text[2*i+1] = byte(u>>8), byte(u)
			}
			fmt.Fprintf(&b, "<%s> <%s>\n", m.code, strings.ToUpper(hex.EncodeToString(text)))
		}
		b.WriteString("endbfchar\n")
	}
	b.Write(c.data[c.last:])
	return b.Bytes(), true
}

// pdfUTF16 decodes the text of a mapping, UTF-16BE in hexadecimal.
func pdfUTF16(h string) ([]rune, error) {
	if len(h)%4 != 0 {
		return nil, errPDF
	}
	b, err := hex.DecodeString(h)
	if err != nil {
		return nil, errPDF
	}
	units := make([]uint16, len(b)/2)
	for i := range units {
		units[i] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
	}
	// An unpaired surrogate would come back as U+FFFD.
	for i := 0; i < len(units); i++ {
		switch u := units[i]; {
		case u >= 0xD800 && u <= 0xDBFF:
			if i+1 == len(units) || units[i+1] < 0xDC00 || units[i+1] > 0xDFFF {
				return nil, errPDF
			}
			i++
		case u >= 0xDC00 && u <= 0xDFFF:
			return nil, errPDF
		}
	}
	return utf16.Decode(units), nil
}

// pdfHex returns the digits of a hexadecimal string in upper case.
func pdfHex(s string) (string, error) {
	if len(s) < 2 || s[0] != '<' || s[1] == '<' || s[len(s)-1] != '>' {
		return "", errPDF
	}
	var b strings.Builder
	for i := 1; i < len(s)-1; i++ {
		c := s[i]
		switch {
		case pdfSpace(c):
		case c >= '0' && c <= '9', c >= 'A' && c <= 'F':
			b.WriteByte(c)
		case c >= 'a' && c <= 'f':
			b.WriteByte(c - 'a' + 'A')
		default:
			return "", errPDF
		}
	}
	if b.Len() == 0 {
		return "", errPDF
	}
	return b.String(), nil
}

// A minimal reader of the PDF syntax that CoreGraphics writes (ISO 32000-1,
// 7.2 to 7.5): objects listed by cross-reference tables, not streams, and
// no encryption.

// pdfToken is a token, at data[start:end].
type pdfToken struct {
	text       string
	start, end int
}

type pdfParser struct {
	data []byte
	pos  int
}

func pdfSpace(c byte) bool {
	return c == 0 || c == '\t' || c == '\n' || c == '\f' || c == '\r' || c == ' '
}

func pdfDelimiter(c byte) bool { return pdfSpace(c) || strings.IndexByte("()<>[]{}/%", c) >= 0 }

func (p *pdfParser) token() (pdfToken, error) {
	for p.pos < len(p.data) {
		if pdfSpace(p.data[p.pos]) {
			p.pos++
			continue
		}
		if p.data[p.pos] == '%' {
			for p.pos < len(p.data) && p.data[p.pos] != '\r' && p.data[p.pos] != '\n' {
				p.pos++
			}
			continue
		}
		break
	}
	start := p.pos
	if start == len(p.data) {
		return pdfToken{}, io.EOF
	}
	c := p.data[p.pos]
	p.pos++
	switch c {
	case '(':
		depth := 1
		for p.pos < len(p.data) && depth > 0 {
			c = p.data[p.pos]
			p.pos++
			switch c {
			case '\\':
				if p.pos < len(p.data) {
					p.pos++
				}
			case '(':
				depth++
			case ')':
				depth--
			}
		}
		if depth != 0 {
			return pdfToken{}, errPDF
		}
	case '<':
		if p.pos < len(p.data) && p.data[p.pos] == '<' {
			p.pos++
			break
		}
		for p.pos < len(p.data) && p.data[p.pos] != '>' {
			p.pos++
		}
		if p.pos == len(p.data) {
			return pdfToken{}, errPDF
		}
		p.pos++
	case '>':
		if p.pos == len(p.data) || p.data[p.pos] != '>' {
			return pdfToken{}, errPDF
		}
		p.pos++
	case '[', ']', '{', '}':
	default:
		if c != '/' && pdfDelimiter(c) {
			return pdfToken{}, errPDF
		}
		for p.pos < len(p.data) && !pdfDelimiter(p.data[p.pos]) {
			p.pos++
		}
	}
	return pdfToken{string(p.data[start:p.pos]), start, p.pos}, nil
}

func (p *pdfParser) expect(s string) error {
	if t, err := p.token(); err != nil || t.text != s {
		return errPDF
	}
	return nil
}

func (p *pdfParser) integer() (int, error) {
	t, err := p.token()
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(t.text)
	if err != nil {
		return 0, errPDF
	}
	return n, nil
}

// pdfKind is the type of a pdfValue, and tells which fields hold it.
type pdfKind uint8

const (
	pdfAtom      pdfKind = iota // a real number, a boolean or null
	pdfInt                      // n
	pdfRef                      // n and gen, of the object it refers to
	pdfName                     // raw, with its #xx escapes
	pdfString                   // raw, a literal string
	pdfHexString                // raw
	pdfArray                    // list
	pdfDict                     // entries
)

// pdfValue is an object of the PDF syntax. raw is its source, which the
// dictionaries of an update copy as they were.
type pdfValue struct {
	kind    pdfKind
	raw     string
	n, gen  int
	entries []pdfEntry
	list    []pdfValue
}

type pdfEntry struct {
	key, raw string // the name, and as it was written
	value    pdfValue
}

// pdfNameText returns the name a name token is for, without its slash.
func pdfNameText(raw string) (string, error) {
	if !strings.HasPrefix(raw, "/") {
		return "", errPDF
	}
	var b strings.Builder
	for i := 1; i < len(raw); i++ {
		if raw[i] != '#' {
			b.WriteByte(raw[i])
			continue
		}
		if i+2 >= len(raw) {
			return "", errPDF
		}
		v, err := strconv.ParseUint(raw[i+1:i+3], 16, 8)
		if err != nil {
			return "", errPDF
		}
		b.WriteByte(byte(v))
		i += 2
	}
	return b.String(), nil
}

func (p *pdfParser) value(depth int) (pdfValue, error) {
	if depth > 64 {
		return pdfValue{}, errPDF
	}
	t, err := p.token()
	if err != nil {
		return pdfValue{}, err
	}
	v := pdfValue{kind: pdfAtom}
	switch {
	case t.text == "<<":
		v.kind = pdfDict
		keys := map[string]bool{}
		for {
			key, err := p.token()
			if err != nil {
				return v, err
			}
			if key.text == ">>" {
				break
			}
			name, err := pdfNameText(key.text)
			if err != nil || keys[name] {
				return v, errPDF
			}
			keys[name] = true
			value, err := p.value(depth + 1)
			if err != nil {
				return v, err
			}
			v.entries = append(v.entries, pdfEntry{name, key.text, value})
		}
	case t.text == "[":
		v.kind = pdfArray
		for {
			at := p.pos
			next, err := p.token()
			if err != nil {
				return v, err
			}
			if next.text == "]" {
				break
			}
			p.pos = at
			item, err := p.value(depth + 1)
			if err != nil {
				return v, err
			}
			v.list = append(v.list, item)
		}
	case t.text[0] == '/':
		v.kind = pdfName
		if _, err := pdfNameText(t.text); err != nil {
			return v, err
		}
	case t.text[0] == '(':
		v.kind = pdfString
	case t.text[0] == '<':
		v.kind = pdfHexString
		if _, err := pdfHex(t.text); err != nil {
			return v, err
		}
	default:
		n, err := strconv.Atoi(t.text)
		if err != nil {
			if t.text == "true" || t.text == "false" || t.text == "null" {
				break
			}
			// Only plain reals, as PDF writes no exponents, NaN or infinities.
			if strings.ContainsAny(t.text, "eENaInf") {
				return v, errPDF
			}
			if _, err := strconv.ParseFloat(t.text, 64); err != nil {
				return v, errPDF
			}
			break
		}
		v.kind, v.n = pdfInt, n
		at := p.pos
		if second, err := p.token(); err == nil {
			if gen, err := strconv.Atoi(second.text); err == nil {
				if third, err := p.token(); err == nil && third.text == "R" {
					if n < 0 || gen < 0 || gen > 65535 {
						return v, errPDF
					}
					v.kind, v.gen = pdfRef, gen
					break
				}
			}
		}
		p.pos = at
	}
	v.raw = string(p.data[t.start:p.pos])
	return v, nil
}

func (v pdfValue) get(key string) (pdfValue, bool) {
	for _, e := range v.entries {
		if e.key == key {
			return e.value, true
		}
	}
	return pdfValue{}, false
}

func (v pdfValue) named(name string) bool {
	if v.kind != pdfName {
		return false
	}
	n, err := pdfNameText(v.raw)
	return err == nil && n == name
}

// dictionaryWithout writes the dictionary without the keys and without its
// closing ">>", which the caller writes after the entries it adds.
func (v pdfValue) dictionaryWithout(keys ...string) string {
	var b strings.Builder
	b.WriteString("<<")
	for _, e := range v.entries {
		if !slices.Contains(keys, e.key) {
			fmt.Fprintf(&b, " %s %s", e.raw, e.value.raw)
		}
	}
	return b.String()
}

type pdfXref struct {
	offset, gen int
	used        bool
}

type pdfFile struct {
	data    []byte
	xref    map[int]pdfXref // the newest entry of every object
	trailer pdfValue        // the newest trailer
	start   int             // the offset of the newest cross-reference table
}

func readPDF(data []byte) (*pdfFile, error) {
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		return nil, errPDF
	}
	end := bytes.TrimRightFunc(data, func(r rune) bool { return r < 0x80 && pdfSpace(byte(r)) })
	if !bytes.HasSuffix(end, []byte("%%EOF")) {
		return nil, errPDF
	}
	footer := bytes.LastIndex(end, []byte("startxref"))
	if footer < 0 {
		return nil, errPDF
	}
	p := pdfParser{data: end, pos: footer + len("startxref")}
	start, err := p.integer()
	if err != nil {
		return nil, err
	}
	// The %%EOF that token skips as a comment.
	if string(bytes.TrimSpace(end[p.pos:])) != "%%EOF" {
		return nil, errPDF
	}
	f := &pdfFile{data: data, xref: map[int]pdfXref{}, start: start}
	seen := map[int]bool{}
	for offset := start; ; {
		if offset < 0 || offset >= len(data) || seen[offset] || !bytes.HasPrefix(data[offset:], []byte("xref")) {
			return nil, errPDF
		}
		seen[offset] = true
		p := pdfParser{data: data, pos: offset}
		if p.expect("xref") != nil {
			return nil, errPDF
		}
		for {
			at := p.pos
			t, err := p.token()
			if err != nil {
				return nil, err
			}
			if t.text == "trailer" {
				break
			}
			p.pos = at
			first, err := p.integer()
			if err != nil || first < 0 {
				return nil, errPDF
			}
			count, err := p.integer()
			if err != nil || count < 0 || count > len(data)/20 || first > int(^uint(0)>>1)-count {
				return nil, errPDF
			}
			for i := range count {
				at, err := p.integer()
				if err != nil || at < 0 {
					return nil, errPDF
				}
				gen, err := p.integer()
				if err != nil || gen < 0 || gen > 65535 {
					return nil, errPDF
				}
				flag, err := p.token()
				if err != nil || flag.text != "n" && flag.text != "f" {
					return nil, errPDF
				}
				if flag.text == "n" && at >= len(data) {
					return nil, errPDF
				}
				// A newer section, read first, overrides an older one.
				if _, ok := f.xref[first+i]; !ok {
					f.xref[first+i] = pdfXref{at, gen, flag.text == "n"}
				}
			}
		}
		trailer, err := p.value(0)
		if err != nil || trailer.kind != pdfDict {
			return nil, errPDF
		}
		if offset == start {
			f.trailer = trailer
		}
		prev, ok := trailer.get("Prev")
		if !ok {
			break
		}
		if prev.kind != pdfInt || prev.n < 0 {
			return nil, errPDF
		}
		offset = prev.n
	}
	size, ok := f.trailer.get("Size")
	if !ok || size.kind != pdfInt || size.n < 1 {
		return nil, errPDF
	}
	for n := range f.xref {
		if n >= size.n {
			return nil, errPDF
		}
	}
	if _, ok := f.trailer.get("Encrypt"); ok {
		return nil, errPDF
	}
	return f, nil
}

// object reads object n of generation gen, and returns where it ends,
// before "endobj" or a dictionary's "stream".
func (f *pdfFile) object(n, gen int) (pdfValue, int, error) {
	x, ok := f.xref[n]
	if !ok || !x.used || x.offset == 0 || x.gen != gen {
		return pdfValue{}, 0, errPDF
	}
	p := pdfParser{data: f.data, pos: x.offset}
	if number, err := p.integer(); err != nil || number != n {
		return pdfValue{}, 0, errPDF
	}
	if generation, err := p.integer(); err != nil || generation != gen || p.expect("obj") != nil {
		return pdfValue{}, 0, errPDF
	}
	v, err := p.value(0)
	if err != nil {
		return v, 0, err
	}
	end := p.pos
	t, err := p.token()
	if err != nil || t.text != "endobj" && (t.text != "stream" || v.kind != pdfDict) {
		return v, 0, errPDF
	}
	return v, end, nil
}

// stream returns the data of the stream of dict, which ends at end,
// inflated if it has a filter: the caller allows FlateDecode alone.
func (f *pdfFile) stream(dict pdfValue, end int) ([]byte, error) {
	length, ok := dict.get("Length")
	if !ok {
		return nil, errPDF
	}
	if length.kind == pdfRef {
		v, _, err := f.object(length.n, length.gen)
		if err != nil {
			return nil, err
		}
		length = v
	}
	if length.kind != pdfInt || length.n < 0 {
		return nil, errPDF
	}
	p := pdfParser{data: f.data, pos: end}
	if p.expect("stream") != nil {
		return nil, errPDF
	}
	// The keyword ends with CR LF or LF.
	switch {
	case bytes.HasPrefix(f.data[p.pos:], []byte("\r\n")):
		p.pos += 2
	case bytes.HasPrefix(f.data[p.pos:], []byte("\n")):
		p.pos++
	default:
		return nil, errPDF
	}
	if length.n > len(f.data)-p.pos {
		return nil, errPDF
	}
	data := f.data[p.pos : p.pos+length.n]
	p.pos += length.n
	if p.expect("endstream") != nil || p.expect("endobj") != nil {
		return nil, errPDF
	}
	if _, ok := dict.get("Filter"); !ok {
		return data, nil
	}
	r, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, errPDF
	}
	// A map is a few kilobytes; a damaged stream may inflate without end.
	const limit = 64 << 20
	data, err = io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil || r.Close() != nil || len(data) > limit {
		return nil, errPDF
	}
	return data, nil
}
