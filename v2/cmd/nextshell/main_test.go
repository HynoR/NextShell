package main

import (
	"flag"
	"github.com/egoist/mygo"
	"io"
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

func TestAgentTestFlagExistsOnlyInDevtoolsBuilds(t *testing.T) {
	fs := flag.NewFlagSet("nextshell", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	o, e := parseFlags(fs, []string{"--db", ":memory:"})
	if e != nil || o.db != ":memory:" || o.agentTest {
		t.Fatal(o, e)
	}
	if (fs.Lookup("agent-test") != nil) != developmentInspector {
		t.Fatal("agent-test flag registered=", fs.Lookup("agent-test") != nil, "devtools build=", developmentInspector)
	}
	fs = flag.NewFlagSet("nextshell", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	_, e = parseFlags(fs, []string{"--agent-test", "--db", ":memory:"})
	if developmentInspector == (e != nil) {
		t.Fatal("--agent-test accepted=", e == nil, "devtools build=", developmentInspector)
	}
}
