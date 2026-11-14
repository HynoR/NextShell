package diagnostics

import (
	"strconv"
	"strings"
)

// Hop is one TTL of a route trace: the addresses that answered and the round trip of each probe,
// in milliseconds, -1 for a probe that got no answer.
type Hop struct {
	TTL  int
	IPs  []string
	RTTs []float64
	Note string // what the address is, as nexttrace knows without geo data: RFC1918 and the like
}

// Timeout is whether no probe of the hop was answered.
func (h Hop) Timeout() bool { return len(h.IPs) == 0 }

// ParseTrace reads what Trace prints: nexttrace's --raw lines, ttl|ip|ptr|rtt|asn|country|prov|city|
// district|owner|lat|lng, one per probe, become hops in TTL order; the lines around them (the version,
// the route header, warnings and errors) are notes, kept in order.
func ParseTrace(raw string) (hops []Hop, notes []string) {
	at := map[int]int{} // TTL -> index in hops
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r", ""), "\n") {
		f := strings.Split(line, "|")
		ttl, e := strconv.Atoi(strings.TrimSpace(f[0]))
		if len(f) < 4 || e != nil || ttl <= 0 || ttl > 255 {
			if s := strings.TrimSpace(line); s != "" {
				notes = append(notes, s)
			}
			continue
		}
		i, ok := at[ttl]
		if !ok {
			i = len(hops)
			at[ttl] = i
			hops = append(hops, Hop{TTL: ttl})
		}
		h := &hops[i]
		ip := strings.TrimSpace(f[1])
		rtt, e := strconv.ParseFloat(strings.TrimSpace(f[3]), 64)
		if ip == "" || ip == "*" || e != nil {
			h.RTTs = append(h.RTTs, -1)
			continue
		}
		h.RTTs = append(h.RTTs, rtt)
		if !contains(h.IPs, ip) {
			h.IPs = append(h.IPs, ip)
		}
		if h.Note == "" && len(f) > 5 {
			h.Note = noteOf(f[4:])
		}
	}
	for i := 1; i < len(hops); i++ { // nexttrace prints them in order; a merged file might not
		for j := i; j > 0 && hops[j].TTL < hops[j-1].TTL; j-- {
			hops[j], hops[j-1] = hops[j-1], hops[j]
		}
	}
	return hops, notes
}

// noteOf is the AS, the place and the owner nexttrace gave, minus its placeholder for no geo data.
func noteOf(f []string) string {
	var parts []string
	for i, v := range f {
		v = strings.TrimSpace(v)
		if i >= 6 || v == "" || v == "网络故障" || contains(parts, v) { // lat, lng: never shown
			continue
		}
		if i == 0 && !strings.HasPrefix(v, "AS") {
			v = "AS" + v
		}
		parts = append(parts, v)
	}
	return strings.Join(parts, " · ")
}

func contains(vs []string, v string) bool {
	for _, x := range vs {
		if x == v {
			return true
		}
	}
	return false
}
