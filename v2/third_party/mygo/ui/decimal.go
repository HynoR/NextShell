package ui

import (
	"math"
	"math/big"
	"strconv"
	"strings"
)

// DecimalInput creates a text input editing *value as an exact decimal
// between lo and hi, which the arrows Up and Down, and the buttons beside
// it, step by step: a NumberInput for amounts of money, which it never
// rounds. *value is written with exactly the decimals of step, as
// "1234.50"; lo and hi are written the same way, "" for no limit. What is
// typed applies as soon as it is a number in range, with '.' before its
// decimals and ',' between groups of three digits: "1,234.5" sets
// "1234.50", and shows so once the input loses the focus. Text with more
// decimals than step's (other than zeros), out of range or not a number
// leaves *value as it was and marks the input with Error, whose message
// an enclosing Field shows, until it is corrected. A value the app sets
// replaces what was typed. Changed reports a new value.
//
//	ui.DecimalInput(c, &app.amount, "0", "", "0.01")
func coreDecimalInput(c *context, value *string, lo, hi, step string) *node {
	t := c.theme
	row := coreRow(c).Gap(t.Space(1)).Shrink(0).AlignItems(Center)
	row.widget = "DecimalInput"
	d := coreLocal(row, "decimal", func() decimalInput { return decimalInput{} })
	if d.limits(lo, hi, step) || *value != d.shown {
		// A value the app set replaces what was typed.
		d.show(*value)
	}
	set := func(v *big.Int) {
		if d.min != nil && v.Cmp(d.min) < 0 {
			v = d.min
		}
		if d.max != nil && v.Cmp(d.max) > 0 {
			v = d.max
		}
		if s := formatDecimal(v, d.scale); s != *value {
			*value = s
			row.st.markChanged()
			c.rt.consumed = true
		}
		d.show(*value)
	}
	// stepBy steps the value, from 0 for none, and shows it in full.
	stepBy := func(sign int) {
		v := new(big.Int)
		if d.v != nil {
			v.Set(d.v)
		}
		if sign > 0 {
			v.Add(v, d.unit)
		} else {
			v.Sub(v, d.unit)
		}
		set(v)
		c.rt.consumed = true
	}
	row.Children(func() {
		in := coreTextInput(c, &d.text).Width(t.Space(20)).Shrink(1).MinWidth(0)
		in.widget = "DecimalInput"
		if d.fault != decimalFine {
			in.fieldError(d.message(c))
		}
		in.afterInput(func() {
			fault := d.fault
			if in.Changed() {
				v, f := parseDecimal(d.text, d.scale)
				if f == decimalFine {
					f = d.inRange(v)
				}
				switch {
				case strings.TrimSpace(d.text) == "":
					// Nothing yet: the value stays, and shows again once
					// the focus goes.
					d.fault = decimalFine
				case f == decimalFine:
					text := d.text
					set(v)
					d.text = text // as typed, until the focus goes
				default:
					d.fault = f
				}
			}
			if in.Shortcut(0, KeyUp) {
				stepBy(1)
			}
			if in.Shortcut(0, KeyDown) {
				stepBy(-1)
			}
			if !in.Focused() && d.fault == decimalFine && d.text != d.full() {
				// What the app set, or what was typed, shown in full;
				// what is wrong stays until it is corrected.
				d.text = d.full()
				c.rt.consumed = true
			}
			if d.fault != fault {
				c.rt.consumed = true // shown in the next pass
			}
		})
		// Assistive technology reads the value as the text of the input,
		// exactly; the range in floating point.
		in.hasRange, in.accRange, in.accStep = true, [3]float64{d.minF, d.maxF, d.now}, d.unitF
		for _, b := range []struct {
			label string
			sign  int
		}{{"−", -1}, {"+", 1}} {
			btn := coreButton(c, b.label).Padding(t.Space(1), t.Space(2)).Label(map[bool]string{true: "Increase", false: "Decrease"}[b.sign > 0])
			btn.Disabled(d.v != nil && (b.sign < 0 && d.min != nil && d.v.Cmp(d.min) <= 0 || b.sign > 0 && d.max != nil && d.v.Cmp(d.max) >= 0))
			btn.afterInput(func() {
				if btn.Clicked() {
					stepBy(b.sign)
				}
			})
			btn.TextColor(t.Text)
		}
	})
	return row
}

// decimalInput is what a DecimalInput keeps from frame to frame.
type decimalInput struct {
	// text is what the input shows; shown is the *value it was last made
	// from, or that typing set, and v that value in units of 10^-scale,
	// nil for none ("") or one that is not a number of the scale.
	text, shown string
	v           *big.Int
	// fault is what is wrong with the text, or with the value the app
	// set.
	fault decimalFault
	// lo, hi and step are the arguments that the limits were made from:
	// min and max, nil for none, and unit, the step, in units of
	// 10^-scale, where scale is the number of decimals of step.
	lo, hi, step   string
	min, max, unit *big.Int
	scale          int
	ready          bool
	// minF, maxF, unitF and now are the range, the step and the value for
	// assistive technology.
	minF, maxF, unitF, now float64
}

// decimalFault is what is wrong with a decimal typed.
type decimalFault uint8

const (
	decimalFine     decimalFault = iota
	decimalInvalid               // not a number
	decimalTooExact              // more decimals than the step's, not zeros
	decimalBelow                 // below lo
	decimalAbove                 // above hi
)

// limits parses the bounds and the step as they change, and reports
// whether they did. It panics for one that is not a decimal, and for lo
// or hi with more decimals than step.
func (d *decimalInput) limits(lo, hi, step string) bool {
	if d.ready && lo == d.lo && hi == d.hi && step == d.step {
		return false
	}
	d.ready, d.lo, d.hi, d.step = true, lo, hi, step
	_, frac, _ := strings.Cut(strings.TrimSpace(step), ".")
	d.scale = len(frac)
	u, f := parseDecimal(step, d.scale)
	if f != decimalFine || u.Sign() <= 0 {
		panic("ui: DecimalInput's step " + strconv.Quote(step) + " is not a positive decimal")
	}
	d.unit, d.unitF = u, decimalFloat(u, d.scale)
	bound := func(s string, inf float64) (*big.Int, float64) {
		if s == "" {
			return nil, inf
		}
		v, f := parseDecimal(s, d.scale)
		if f != decimalFine {
			panic("ui: DecimalInput's bound " + strconv.Quote(s) + " is not a decimal with at most the decimals of its step " + strconv.Quote(step))
		}
		return v, decimalFloat(v, d.scale)
	}
	d.min, d.minF = bound(lo, math.Inf(-1))
	d.max, d.maxF = bound(hi, math.Inf(1))
	return true
}

// show shows s, a value the app set or typing did: written in full, or
// as it is when it is not a number of the scale, which is then wrong.
func (d *decimalInput) show(s string) {
	d.shown, d.text, d.v, d.fault = s, s, nil, decimalFine
	d.now = max(d.minF, min(d.maxF, 0))
	if strings.TrimSpace(s) == "" {
		return
	}
	v, f := parseDecimal(s, d.scale)
	if f != decimalFine {
		d.fault = f
		return
	}
	d.v, d.text, d.fault = v, formatDecimal(v, d.scale), d.inRange(v)
	d.now = decimalFloat(v, d.scale)
}

// full returns the value written in full.
func (d *decimalInput) full() string {
	if d.v == nil {
		return d.shown
	}
	return formatDecimal(d.v, d.scale)
}

// inRange reports whether v is below lo or above hi.
func (d *decimalInput) inRange(v *big.Int) decimalFault {
	switch {
	case d.min != nil && v.Cmp(d.min) < 0:
		return decimalBelow
	case d.max != nil && v.Cmp(d.max) > 0:
		return decimalAbove
	}
	return decimalFine
}

// message returns what the input says of its fault, in the language the
// window shows.
func (d *decimalInput) message(c *context) string {
	texts := c.texts()
	switch d.fault {
	case decimalTooExact:
		if d.scale == 0 {
			return texts.DecimalWhole()
		}
		return texts.DecimalPlaces(d.scale)
	case decimalBelow:
		return texts.DecimalAtLeast(formatDecimal(d.min, d.scale))
	case decimalAbove:
		return texts.DecimalAtMost(formatDecimal(d.max, d.scale))
	}
	return texts.DecimalInvalid()
}

// parseDecimal parses s, a decimal as "-1,234.50", into units of
// 10^-scale: digits, with '.' before the decimals and ',' between groups
// of three digits before it, after a sign. Decimals past scale must be
// zeros, which it never rounds.
func parseDecimal(s string, scale int) (*big.Int, decimalFault) {
	s = strings.TrimSpace(s)
	neg := strings.HasPrefix(s, "-")
	if neg || strings.HasPrefix(s, "+") {
		s = s[1:]
	}
	whole, frac, _ := strings.Cut(s, ".")
	if whole == "" && frac == "" {
		return nil, decimalInvalid
	}
	if strings.Contains(whole, ",") {
		for i, g := range strings.Split(whole, ",") {
			if g == "" || len(g) > 3 || i > 0 && len(g) != 3 {
				return nil, decimalInvalid
			}
		}
		whole = strings.ReplaceAll(whole, ",", "")
	}
	for _, r := range whole + frac {
		if r < '0' || r > '9' {
			return nil, decimalInvalid
		}
	}
	if len(frac) > scale {
		if strings.TrimRight(frac[scale:], "0") != "" {
			return nil, decimalTooExact
		}
		frac = frac[:scale]
	}
	v, _ := new(big.Int).SetString("0"+whole+frac+strings.Repeat("0", scale-len(frac)), 10)
	if neg {
		v.Neg(v)
	}
	return v, decimalFine
}

// formatDecimal writes v, in units of 10^-scale, with scale decimals.
func formatDecimal(v *big.Int, scale int) string {
	s := v.Text(10)
	sign := ""
	if v.Sign() < 0 {
		sign, s = "-", s[1:]
	}
	if scale == 0 {
		return sign + s
	}
	if len(s) <= scale {
		s = strings.Repeat("0", scale-len(s)+1) + s
	}
	return sign + s[:len(s)-scale] + "." + s[len(s)-scale:]
}

// decimalFloat returns v, in units of 10^-scale, as the nearest float.
func decimalFloat(v *big.Int, scale int) float64 {
	f, _ := strconv.ParseFloat(formatDecimal(v, scale), 64)
	return f
}
