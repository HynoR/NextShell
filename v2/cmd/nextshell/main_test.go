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

// Nothing that auto-trusts or imports exists in a release build.
func TestTestingFlagsExistOnlyInDevtoolsBuilds(t *testing.T) {
	for _, name := range []string{"agent-test", "import", "trust-new-hosts"} {
		fs := flag.NewFlagSet("nextshell", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		if _, e := parseFlags(fs, nil); e != nil {
			t.Fatal(e)
		}
		if (fs.Lookup(name) != nil) != developmentInspector {
			t.Fatalf("--%s registered=%v, devtools build=%v", name, fs.Lookup(name) != nil, developmentInspector)
		}
	}
	fs := flag.NewFlagSet("nextshell", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	_, e := parseFlags(fs, []string{"--db", ":memory:", "--agent-test", "--trust-new-hosts"})
	if developmentInspector == (e != nil) {
		t.Fatal("--trust-new-hosts accepted=", e == nil, "devtools build=", developmentInspector)
	}
}
