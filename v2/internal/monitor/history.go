package monitor

import (
	"math"
	"time"
)

const HistoryLimit = 50

type Point struct {
	At     time.Time
	Values map[string]float64
}

func (p Point) Value(key string) float64 {
	v, ok := p.Values[key]
	if !ok {
		return math.NaN()
	}
	return v
}

type History struct{ Points []Point }

// Append keeps real, strictly increasing samples. Missing deltas remain absent,
// never fabricated zeros. History is in-memory and independent of the UI.
func (h *History) Append(s Snapshot) bool {
	if s.At.IsZero() || (len(h.Points) > 0 && !s.At.After(h.Points[len(h.Points)-1].At)) {
		return false
	}
	values := map[string]float64{}
	if s.HasDelta {
		values["cpu"] = s.CPUPercent
	}
	if s.MemoryTotal > 0 {
		values["memory"] = 100 * float64(s.MemoryTotal-min(s.MemoryTotal, s.MemoryAvailable)) / float64(s.MemoryTotal)
	}
	if s.SwapTotal > 0 {
		values["swap"] = 100 * float64(s.SwapTotal-min(s.SwapTotal, s.SwapFree)) / float64(s.SwapTotal)
	}
	if s.HasNetworkDelta {
		values["rx"] = s.RXPerSecond / 1024
		values["tx"] = s.TXPerSecond / 1024
	}
	for name, n := range s.Interfaces {
		if n.HasDelta {
			values["rx:"+name] = n.RXPerSecond / 1024
			values["tx:"+name] = n.TXPerSecond / 1024
		}
	}
	for _, d := range s.Disks {
		if d.TotalKB > 0 {
			values["disk:"+d.Mount] = d.UsedPercent
		}
	}
	for key, v := range values {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			delete(values, key)
		}
	}
	h.Points = append(h.Points, Point{s.At, values})
	if len(h.Points) > HistoryLimit {
		copy(h.Points, h.Points[len(h.Points)-HistoryLimit:])
		h.Points = h.Points[:HistoryLimit]
	}
	return true
}
