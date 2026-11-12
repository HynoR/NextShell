package appui_test

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/appui"
)

// palette runs the first result of query.
func palette(tt *ui.Tester, query string) {
	tt.Key(appMod(), ui.KeyK)
	tt.Type(query)
	tt.Key(0, ui.KeyEnter)
}

// The palette has the actions of the main window's buttons and shortcuts: find in the terminal, the command bar,
// the process and network managers; and the directory-following script is named for what it does.
func TestPaletteActionsForTheSession(t *testing.T) {
	a, p, tt := fixture(t)
	has(t, tt, "未选择服务器")
	tt.Key(appMod(), ui.KeyK)
	tt.Type("查找终端")
	if !tt.HasText("没有结果") {
		t.Fatal("session actions without a session", tt.Texts())
	}
	tt.Key(0, ui.KeyEscape)
	session(t, p, tt, "one", "one", remoteHost("one"))
	palette(tt, "查找终端")
	has(t, tt, "查找终端内容")
	if tt.HasText("1 / 1") || tt.HasText("无结果") {
		t.Fatal("unexpected count", tt.Texts())
	}
	tt.Key(0, ui.KeyEscape) // the find bar closes
	palette(tt, "输入命令")
	tt.Type("ls")
	inputIs(t, tt, "ls")
	palette(tt, "进程管理器")
	has(t, tt, "进程 · one")
	if a.Active != 0 {
		t.Fatal(a.Active)
	}
	palette(tt, "网络管理器")
	has(t, tt, "网络 · one")
	// The script: named for what it does.
	tt.Key(appMod(), ui.KeyK)
	tt.Type("跟随终端目录")
	has(t, tt, "让文件面板跟随终端目录（bash）", "让文件面板跟随终端目录（zsh）")
	if tt.HasText("插入目录跟踪脚本") {
		t.Fatal("the old name")
	}
}

type uaAgentPlatform struct {
	*fakePlatform
	agent appui.AgentOverview
	halts []bool
}

func (p *uaAgentPlatform) AgentOverview() appui.AgentOverview { return p.agent }
func (p *uaAgentPlatform) AgentHalt(h bool)                   { p.halts = append(p.halts, h); p.agent.Halted = h }

// 紧急停止 and 恢复 Agent are in the palette while the agent service is on.
func TestPaletteEmergencyStopAndResume(t *testing.T) {
	p := &uaAgentPlatform{fakePlatform: newPlatform(t)}
	a := appui.New(p)
	tt := ui.NewTester(a.View, 1280, 800)
	tt.Key(appMod(), ui.KeyK)
	tt.Type("紧急停止")
	if !tt.HasText("没有结果") {
		t.Fatal("offered with the agent service off")
	}
	tt.Key(0, ui.KeyEscape)
	p.agent.URL = "http://127.0.0.1:7801/mcp"
	palette(tt, "紧急停止")
	has(t, tt, "已紧急停止 Agent")
	if len(p.halts) != 1 || !p.halts[0] {
		t.Fatal("halt", p.halts)
	}
	tt.Key(appMod(), ui.KeyK)
	tt.Type("紧急停止")
	if !tt.HasText("没有结果") {
		t.Fatal("stop offered while stopped")
	}
	tt.Key(0, ui.KeyEscape)
	palette(tt, "恢复 Agent")
	if len(p.halts) != 2 || p.halts[1] {
		t.Fatal("resume", p.halts)
	}
}
