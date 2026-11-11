package appui

import (
	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/model"
	"nextshell.dev/v2/internal/monitor"
	"testing"
)

func TestMonitorFilteringAndSorting(t *testing.T) {
	vs := []monitor.Process{{PID: 2, CPU: 10, MemoryMB: 3, Command: "nginx", User: "root"}, {PID: 1, CPU: 50, MemoryMB: 1, Command: "worker", User: "app"}}
	for _, v := range []struct {
		column string
		desc   bool
		want   int
	}{{"CPU", true, 1}, {"PID", true, 2}, {"内存 MiB", false, 1}, {"命令", false, 2}} {
		got := filteredProcesses(vs, "", ui.SortOrder{Column: v.column, Descending: v.desc})
		if got[0].PID != v.want {
			t.Fatal(v, got)
		}
	}
	if got := filteredProcesses(vs, "ROOT", ui.SortOrder{}); len(got) != 1 || got[0].PID != 2 {
		t.Fatal(got)
	}
	if len(filteredProcesses(vs, "absent", ui.SortOrder{})) != 0 {
		t.Fatal("filter ignored")
	}
	if vs[0].PID != 2 {
		t.Fatal("source mutated")
	}
}
func TestCommandTemplateDraftPreservesAndResetsParameters(t *testing.T) {
	var s phase2State
	s.Command = model.Command{ID: "one", Text: "echo [#host] [#host] [#port]"}
	s.prepareParameters()
	if len(s.Parameters) != 2 {
		t.Fatal(s.Parameters)
	}
	s.Parameters[0].Value = "localhost"
	s.Parameters[1].Value = "22"
	if got := s.commandText(); got != "echo localhost localhost 22" {
		t.Fatal(got)
	}
	s.Command.Text = "[#host]"
	s.prepareParameters()
	if s.Parameters[0].Value != "localhost" {
		t.Fatal("lost typing")
	}
	s.Command.ID = "two"
	s.prepareParameters()
	if s.Parameters[0].Value != "" {
		t.Fatal("parameter leaked between commands")
	}
}
func TestCommandSearchAndMeterBounds(t *testing.T) {
	v := model.Command{Name: "deploy", Group: "prod", Description: "release", Text: "echo ok"}
	if !commandMatches(v, "RELEASE", "prod") || commandMatches(v, "deploy", "test") || commandMatches(v, "missing", "全部") {
		t.Fatal("search mismatch")
	}
	for _, v := range []struct {
		total, free uint64
		ratio       float64
	}{{0, 0, 0}, {10, 12, 0}, {100, 25, .75}} {
		if got := memoryRatio(v.total, v.free); got != v.ratio {
			t.Fatal(got)
		}
	}
}

func TestFolderBatchSelectionAndSafeQuotedPaths(t *testing.T) {
	a := App{Data: Snapshot{Folders: []model.Folder{{ID: "child", ParentID: "root"}, {ID: "root"}}, Connections: []model.Connection{{ID: "a", FolderID: "root"}, {ID: "b", FolderID: "child"}, {ID: "deleted", FolderID: "root", Deleted: true}, {ID: "c", FolderID: "other"}}}}
	a.selectFolderConnections("root")
	if len(a.Selected) != 2 || !a.Selected["a"] || !a.Selected["b"] {
		t.Fatal(a.Selected)
	}
	if got := shellQuote("/a'b; echo nope"); got != "'/a'\\''b; echo nope'" {
		t.Fatal(got)
	}
	for _, p := range []string{"/a\rcommand", "/a\ncommand", "/a\x03command", "/a\x1bcommand"} {
		if safeTerminalPath(p) == nil {
			t.Fatal("unsafe path accepted")
		}
	}
	if safeTerminalPath("/中文/a'b") != nil {
		t.Fatal("normal path rejected")
	}
}
