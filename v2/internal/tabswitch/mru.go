// Package tabswitch implements a UI-independent Ctrl+Tab transaction.
package tabswitch

type MRU struct {
	Order  []string
	Cycle  []string
	Index  int
	Origin string
}

func (m *MRU) Active() bool { return len(m.Cycle) > 0 }
func (m *MRU) Preview() string {
	if !m.Active() {
		return ""
	}
	return m.Cycle[m.Index]
}
func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
func (m *MRU) Touch(id string) {
	if id == "" {
		return
	}
	out := []string{id}
	for _, x := range m.Order {
		if x != id {
			out = append(out, x)
		}
	}
	m.Order = out
}

// Sync prunes closed tabs. The cycle is deliberately frozen until Ctrl release.
// Activation outside the preview (mouse/new tab) cancels a pending transaction.
func (m *MRU) Sync(live []string, current string) {
	out := m.Order[:0]
	for _, x := range m.Order {
		if contains(live, x) {
			out = append(out, x)
		}
	}
	m.Order = out
	if m.Active() && current != m.Preview() {
		m.Cycle = nil
	}
	if !m.Active() {
		m.Touch(current)
	}
	for _, x := range live {
		if !contains(m.Order, x) {
			m.Order = append(m.Order, x)
		}
	}
}
func (m *MRU) Step(live []string, current string, reverse bool) string {
	m.Sync(live, current)
	if !m.Active() {
		if len(live) < 2 {
			return current
		}
		m.Origin = current
		m.Cycle = append([]string(nil), m.Order...)
		m.Index = 0
	}
	delta := 1
	if reverse {
		delta = -1
	}
	for range len(m.Cycle) {
		m.Index = (m.Index + delta + len(m.Cycle)) % len(m.Cycle)
		if contains(live, m.Preview()) {
			return m.Preview()
		}
	}
	m.Cycle = nil
	return current
}
func (m *MRU) Commit() string { id := m.Preview(); m.Cycle = nil; m.Touch(id); return id }
func (m *MRU) Cancel(live []string) string {
	id := m.Origin
	m.Cycle = nil
	if contains(live, id) {
		return id
	}
	return ""
}
