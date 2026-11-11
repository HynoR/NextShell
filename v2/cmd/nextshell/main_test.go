package main

import (
	"github.com/egoist/mygo"
	"testing"
)

func TestInspectorRequiresExplicitOptIn(t *testing.T) {
	for _, v := range []string{"", "0", "true", "1"} {
		t.Setenv("NEXTSHELL_DEVTOOLS", v)
		got := devToolsMode()
		want := mygo.DevToolsDisabled
		if developmentInspector && v == "1" {
			want = mygo.DevToolsEnabled
		}
		if got != want {
			t.Fatalf("%q mode=%v", v, got)
		}
	}
}
