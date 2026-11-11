package ui

// TypeNative uses GTK's synchronous text-input-client commit path in memory.
func (t *Tester) TypeNative(text string) {
	if c := t.h.ime.Client; c != nil {
		c.ReplaceText(nil, text)
	} else {
		t.Type(text)
	}
	t.settle()
}

// Burst groups test events before rendering, like a slow display.
func (t *Tester) Burst(fn func())                  { t.batch++; defer func() { t.batch--; t.settle() }(); fn() }
func (t *Tester) SetInspectorEnabled(enabled bool) { t.rt.insp.enabled = enabled }
