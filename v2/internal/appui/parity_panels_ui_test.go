package appui_test

import (
	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/model"
	"nextshell.dev/v2/internal/monitor"
	"testing"
	"time"
)

func TestCommandMenuSearchHistoryAndDeleteConfirmation(t *testing.T) {
	a, p, _ := fixture(t)
	tt := ui.NewTester(a.View, 1400, 1400)
	v := model.Command{ID: p.NewID(), Name: "deploy", Group: "prod", Text: "echo [#host]"}
	if e := p.SaveCommand(v); e != nil {
		t.Fatal(e)
	}
	a.Data, _ = p.Snapshot()
	a.Page = "commands"
	a.ShowWorkbench = false
	tt.Frame()
	chooseMenu(t, tt, "prod / deploy", "复制命令文本")
	if tt.Clipboard() != v.Text {
		t.Fatal(tt.Clipboard())
	}
	chooseMenu(t, tt, "prod / deploy", "编辑保存命令")
	click(t, tt, "参数 host")
	tt.Type("localhost")
	tt.Frame()
	if !tt.HasText("预览：echo localhost") {
		t.Fatal("template preview missing")
	}
	chooseMenu(t, tt, "prod / deploy", "另存为命令副本")
	if a.Phase2.Command.ID != "" || a.Phase2.Command.Name != "deploy 副本" {
		t.Fatal(a.Phase2.Command)
	}
	chooseMenu(t, tt, "prod / deploy", "删除保存命令")
	if !tt.HasText("删除命令「deploy」？删除后无法恢复。") {
		t.Fatal("confirmation missing")
	}
	click(t, tt, "取消删除")
	if len(a.Data.Commands) != 1 {
		t.Fatal("cancel removed command")
	}
	click(t, tt, "搜索命令库")
	tt.Type("missing")
	if !tt.HasText("没有匹配的命令；可新建命令或清除筛选") {
		t.Fatal("filter/empty state missing")
	}
	a.Phase2.CommandSearch = ""
	a.History = []string{"echo one", "echo two"}
	if e := p.SaveHistory(a.History); e != nil {
		t.Fatal(e)
	}
	tt.Frame()
	click(t, tt, "管理命令历史（本地明文）")
	click(t, tt, "清空历史")
	click(t, tt, "确认清空历史")
	if len(a.History) != 0 {
		t.Fatal(a.History)
	}
}
func TestMonitorNativeTablesAndSearch(t *testing.T) {
	a, _, tt := fixture(t)
	a.Tabs = append(a.Tabs, &appui.Session{ID: "mon", Title: "mon", Terminal: newTerminal(t, "monitor")})
	a.Active = 0
	a.Page = "monitor"
	a.ShowWorkbench = false
	a.Phase2.MonitorSession = "mon"
	a.Phase2.Monitor = monitor.Snapshot{At: time.Now(), HasDelta: true, CPUPercent: 25, MemoryTotal: 100, MemoryAvailable: 50, Processes: []monitor.Process{{PID: 123, User: "root", Command: "nginx"}, {PID: 456, User: "app", Command: "worker"}}, Network: []monitor.Network{{Protocol: "tcp", LocalIP: "127.0.0.1", LocalPort: 8080, PID: 123, Process: "nginx"}}}
	tt.Frame()
	click(t, tt, "进程")
	click(t, tt, "过滤进程/网络")
	tt.Type("nginx")
	if !tt.HasText("1 / 2 个进程；选中行填入PID，信号操作仍需明确确认") {
		t.Fatal("process filter")
	}
	a.Phase2.MonitorFilter = ""
	tt.Frame()
	click(t, tt, "网络连接")
	click(t, tt, "过滤进程/网络")
	tt.Type("8080")
	if !tt.HasText("1 / 1 条网络记录") {
		t.Fatal("network filter")
	}
}
