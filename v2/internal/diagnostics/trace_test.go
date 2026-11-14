package diagnostics

import (
	"reflect"
	"testing"
)

// nexttrace v1.7.6's --raw output, as Trace returns it, becomes one row per TTL.
func TestParseTrace(t *testing.T) {
	raw := "NextTrace v1.7.6 2026-11-02T15:55:57Z c93eabd\r\nIP Geo Data Provider: disable-geoip\n172.30.0.2 -> 1.1.1.1, 8 hops max, 28 byte packets, ICMP mode\n" +
		"1|*||||||\n1|*||||||\n1|*||||||\n" +
		"2|10.1.3.39||0.68||RFC1918|||||0.0000|0.0000\n2|10.1.3.39||1.14||RFC1918|||||0.0000|0.0000\n2|10.1.3.39||1.24||RFC1918|||||0.0000|0.0000\n" +
		"3|52.46.166.151||1.47||网络故障|||||0.0000|0.0000\n3|*||||||\n3|52.46.166.152||3.14||网络故障|||||0.0000|0.0000\n" +
		"4|1.1.1.1|one.one.one.one|2.69|13335|US|||CLOUDFLARENET|Cloudflare|0.0000|0.0000\n"
	hops, notes := ParseTrace(raw)
	if !reflect.DeepEqual(notes, []string{"NextTrace v1.7.6 2026-11-02T15:55:57Z c93eabd", "IP Geo Data Provider: disable-geoip", "172.30.0.2 -> 1.1.1.1, 8 hops max, 28 byte packets, ICMP mode"}) {
		t.Fatal(notes)
	}
	want := []Hop{
		{TTL: 1, RTTs: []float64{-1, -1, -1}},
		{TTL: 2, IPs: []string{"10.1.3.39"}, RTTs: []float64{0.68, 1.14, 1.24}, Note: "RFC1918"},
		{TTL: 3, IPs: []string{"52.46.166.151", "52.46.166.152"}, RTTs: []float64{1.47, -1, 3.14}},
		{TTL: 4, IPs: []string{"1.1.1.1"}, RTTs: []float64{2.69}, Note: "AS13335 · US · CLOUDFLARENET · Cloudflare"},
	}
	if !reflect.DeepEqual(hops, want) {
		t.Fatalf("%+v", hops)
	}
	if !hops[0].Timeout() || hops[1].Timeout() {
		t.Fatal("timeout")
	}
	// Without nexttrace, or with an error, there are no hops: only what it said.
	hops, notes = ParseTrace("exec: \"nexttrace\": executable file not found in $PATH")
	if len(hops) != 0 || len(notes) != 1 {
		t.Fatal(hops, notes)
	}
}
