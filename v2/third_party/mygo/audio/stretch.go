package audio

import "math"

// A stretcher time-stretches a voice's source for SetPreservesPitch, by
// WSOLA (waveform similarity overlap-add): it plays segments of the
// source, two hops long, half overlapping, crossfaded with a Hann window,
// one hop of the output each. Where the speed puts the next segment, it
// looks within tolerance for the start most like the natural continuation
// of the last, so that the waveform goes on without a seam, skipping or
// repeating whole periods of it; the pitch stays. The voice then
// resamples what it gives at its rate alone, without its speed.
//
// A voice gets one the first time its player preserves pitch and keeps
// it: with it off, it passes the source through as it is, which a voice
// without one reads itself.
type stretcher struct {
	ch    int   // channels of the source: 1 or 2
	hs    int   // a hop: half a segment, about 16 ms
	delta int64 // how far a segment may move to match, about 10 ms
	dec   int   // the stride of the coarse search: about 16 kHz
	win   []float32

	// Frames of the source from frame base, n of them. rd is the frame
	// read next: base+n, or before base while skipping up to it.
	buf  []float32
	base int64
	n    int
	rd   int64
	end  int64 // the frame after the source's last, once it ended, or -1
	held int   // frames of buf a pass-through read gave, dropped at the next

	mono []float32 // the frames searched, mixed to mono
	tm   []float32 // tail, mixed to mono
	tail []float32 // a hop of the natural continuation of the last segment
	nat  int64     // the frame tail starts at
	out  []float32 // a hop: what a step gives
	own  []float32 // the source's frames, passed through

	on    bool    // stretching, rather than passing the source through
	first bool    // no segment yet since it started stretching
	done  bool    // nothing more to give
	ana   float64 // the frame the speed puts the next segment at

	// The source frames, counted on past its loop's end, that the frames
	// of the voice's hist are at; those of what it read last, from tag0 by
	// tagStep; and where the frames after them are.
	tags          [4]float64
	tag0, tagStep float64
	next          float64
}

func newStretcher(rate, ch int) *stretcher {
	hs := max(rate/64, 16)
	delta := max(rate/96, 8)
	// The frames a step reads at most, and what a voice hands over.
	size := max(2*delta+2*hs+1, 256)
	st := &stretcher{
		ch: ch, hs: hs, delta: int64(delta), dec: max(rate/16000, 1),
		win:  make([]float32, hs),
		buf:  make([]float32, size*ch),
		mono: make([]float32, 2*delta+hs+1),
		tm:   make([]float32, hs),
		tail: make([]float32, hs*ch),
		out:  make([]float32, hs*ch),
		own:  make([]float32, 256*ch),
	}
	for j := range st.win {
		s := math.Sin(math.Pi / 2 * (float64(j) + 0.5) / float64(hs))
		st.win[j] = float32(s * s)
	}
	return st
}

// reset starts over at frame f, from which the source reads next, and
// sets the voice's frames before it, which it has not read yet, at the
// frames before f.
func (st *stretcher) reset(f int64, on bool) {
	st.base, st.rd, st.n, st.held = f, f, 0, 0
	st.end = -1
	st.on, st.first, st.done = on, true, false
	st.ana, st.next = float64(f), float64(f)
	// The voice's second frame of hist is at idx: f-3 after a reset.
	for k := range st.tags {
		st.tags[k] = float64(f - 4 + int64(k))
	}
}

// push notes the frame that comes into the voice's hist at tag.
func (st *stretcher) push(tag float64) {
	st.tags[0], st.tags[1], st.tags[2] = st.tags[1], st.tags[2], st.tags[3]
	st.tags[3] = tag
}

// drop forgets the frames before to; those not read yet are skipped.
func (st *stretcher) drop(to int64) {
	if to <= st.base {
		return
	}
	if k := to - st.base; k < int64(st.n) {
		st.n -= int(k)
		copy(st.buf, st.buf[int(k)*st.ch:(int(k)+st.n)*st.ch])
	} else {
		st.n = 0
	}
	st.base = to
}

// fill reads the source up to frame upto, or its end, and reports false
// while a stream has not decoded that far.
func (st *stretcher) fill(src source, upto int64) bool {
	ch := st.ch
	for st.end < 0 && st.rd < upto {
		skip := st.rd < st.base
		var dst []float32
		if skip {
			dst = st.buf[:min(st.base-st.rd, int64(len(st.buf)/ch))*int64(ch)]
		} else {
			dst = st.buf[st.n*ch : (st.n+int(min(upto-st.rd, int64(len(st.buf)/ch-st.n))))*ch]
		}
		got, ended := src.read(dst)
		if !skip {
			st.n += got
		}
		st.rd += int64(got)
		if ended {
			st.end = st.rd
		} else if got == 0 {
			return false
		}
	}
	if st.end >= 0 && st.base+int64(st.n) < upto {
		// Silence after the end.
		k := int(upto - st.base)
		clear(st.buf[st.n*ch : k*ch])
		st.n = k
	}
	return true
}

// readStretched gives the voice its next frames, in v.in, as a source does:
// time-stretched while it preserves pitch, else as the source has them.
func (v *voice) readStretched() (int, bool) {
	st := v.st
	if st.held > 0 {
		st.drop(st.base + int64(st.held))
		st.held = 0
	}
	if st.done {
		return 0, true
	}
	if v.pitch != st.on {
		// From what is held, which goes on from the last segment.
		if v.pitch {
			st.first, st.ana = true, float64(st.base)
		} else if !st.first && st.nat >= st.base {
			st.drop(st.nat)
		}
		if st.end >= 0 {
			// Not the silence after the end.
			st.n = int(max(min(int64(st.n), st.end-st.base), 0))
		}
		st.on = v.pitch
	}
	if st.on {
		n, ended := st.stretch(v.src, v.speed)
		v.in = st.out
		return n, ended
	}
	st.tagStep = 1
	if st.n > 0 {
		st.tag0, st.held = float64(st.base), st.n
		st.next = float64(st.base + int64(st.n))
		v.in = st.buf
		return st.n, false
	}
	n, ended := v.src.read(st.own)
	st.tag0 = float64(st.rd)
	st.rd += int64(n)
	st.base = st.rd
	st.next = float64(st.rd)
	v.in = st.own
	return n, ended
}

// stretch puts the next frames into st.out and returns how many, with
// ended true once there are none, and none while a stream is late.
func (st *stretcher) stretch(src source, speed float64) (int, bool) {
	hs, ch := st.hs, st.ch
	a := int64(math.Round(st.ana))
	lo, hi := a, a
	if !st.first {
		lo = max(a-st.delta, st.base)
		hi = max(a+st.delta, lo)
	}
	st.drop(lo)
	if !st.fill(src, hi+int64(2*hs)) {
		return 0, false
	}
	if e := st.end; e >= 0 && a >= e {
		// The speed is past the end: what was playing fades out.
		st.done, st.next = true, float64(e)
		if st.first {
			return 0, true
		}
		for j := range hs {
			w := 1 - st.win[j]
			for k := j * ch; k < (j+1)*ch; k++ {
				st.out[k] = st.tail[k] * w
			}
		}
		st.tag0, st.tagStep = float64(e), 0
		return hs, false
	}
	s := lo
	if !st.first {
		s = st.best(lo, hi)
	}
	in := st.buf[int(s-st.base)*ch:]
	o := st.out[:hs*ch]
	if st.first || s == st.nat {
		// No seam: the source as it is.
		copy(o, in)
	} else {
		for j := range hs {
			w := st.win[j]
			for k := j * ch; k < (j+1)*ch; k++ {
				o[k] = st.tail[k] + (in[k]-st.tail[k])*w
			}
		}
	}
	copy(st.tail, in[hs*ch:2*hs*ch])
	st.first = false
	st.nat = s + int64(hs)
	st.tag0, st.tagStep = st.ana, speed
	st.ana += float64(hs) * speed
	st.next = st.ana
	if e := st.end; e >= 0 && st.next > float64(e) {
		// Where the source ends, the frames after are at its end.
		st.tagStep = max(float64(e)-st.tag0, 0) / float64(hs)
		st.next = float64(e)
	}
	return hs, false
}

// best returns the start between lo and hi of the segment whose first hop
// is most like tail, by normalized cross-correlation in mono: every dec
// frames, then each frame around the best.
func (st *stretcher) best(lo, hi int64) int64 {
	if st.nat >= lo && st.nat <= hi {
		// The natural continuation itself: a perfect match.
		return st.nat
	}
	hs, ch := st.hs, st.ch
	span := int(hi - lo)
	off := int(lo - st.base)
	m, t := st.mono[:span+hs], st.tm
	if ch == 1 {
		copy(m, st.buf[off:])
		copy(t, st.tail)
	} else {
		for i := range m {
			m[i] = st.buf[2*(off+i)] + st.buf[2*(off+i)+1]
		}
		for i := range t {
			t[i] = st.tail[2*i] + st.tail[2*i+1]
		}
	}
	score := func(s, stride int) float32 {
		var xy, yy float32
		y := m[s : s+hs]
		for j := 0; j < hs; j += stride {
			xy += t[j] * y[j]
			yy += y[j] * y[j]
		}
		if yy == 0 {
			return 0
		}
		return xy / float32(math.Sqrt(float64(yy)))
	}
	dec := st.dec
	b, bs := 0, float32(math.Inf(-1))
	for s := 0; s <= span; s += dec {
		if x := score(s, dec); x > bs {
			b, bs = s, x
		}
	}
	if dec > 1 {
		c := b
		bs = float32(math.Inf(-1))
		for s := max(c-dec+1, 0); s <= min(c+dec-1, span); s++ {
			if x := score(s, 1); x > bs {
				b, bs = s, x
			}
		}
	}
	return lo + int64(b)
}
