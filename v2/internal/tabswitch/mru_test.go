package tabswitch

import "testing"

func TestHeldControlFreezesOrderAndCommits(t *testing.T) {
	var m MRU
	ids := []string{"a", "b", "c", "d"}
	for _, id := range ids {
		m.Sync(ids, id)
	}
	if v := m.Step(ids, "d", false); v != "c" {
		t.Fatal(v)
	}
	if v := m.Step(ids, "d", false); v != "b" {
		t.Fatal(v)
	}
	if v := m.Step(ids, "d", true); v != "c" {
		t.Fatal(v)
	}
	if v := m.Commit(); v != "c" {
		t.Fatal(v)
	}
	if v := m.Step(ids, "c", false); v != "d" {
		t.Fatal("MRU not committed", v)
	}
	if v := m.Cancel(ids); v != "c" {
		t.Fatal(v)
	}
}
func TestClosedAddedAndExternalActivation(t *testing.T) {
	var m MRU
	ids := []string{"a", "b", "c"}
	for _, id := range ids {
		m.Sync(ids, id)
	}
	if v := m.Step(ids, "c", true); v != "a" {
		t.Fatal(v)
	}
	if v := m.Step([]string{"a", "c", "new"}, "c", false); v != "c" {
		t.Fatal(v)
	}
	if v := m.Step([]string{"a", "c", "new"}, "c", false); v != "a" {
		t.Fatal("snapshot changed", v)
	}
	m.Sync([]string{"a", "c", "new"}, "new")
	if m.Active() || m.Order[0] != "new" {
		t.Fatal(m)
	}
}
func TestZeroOneAndRemovalOfOrigin(t *testing.T) {
	var m MRU
	if m.Step(nil, "", false) != "" || m.Step([]string{"x"}, "x", false) != "x" {
		t.Fatal(m)
	}
	m.Step([]string{"x", "y"}, "x", false)
	if m.Cancel([]string{"y"}) != "" {
		t.Fatal(m)
	}
}
