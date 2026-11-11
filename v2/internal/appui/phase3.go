package appui

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/model"
	"strings"
	"time"
)

type phase3State struct {
	Endpoint, Workspace, Name, JoinID, Auth, Encryption, Token, Blacklist, Shell string
	ShowToken, ConfirmRemote, ConfirmRotate                                      bool
}

func (s *phase3State) init() {
	s.Endpoint = "http://127.0.0.1:41888"
	s.Name = "我的工作区"
	s.Shell = "bash"
}
func (a *App) handlePhase3(e Event) {
	if e.WorkspaceID != "" {
		a.Phase3.Workspace = e.WorkspaceID
		a.Phase3.ConfirmRemote = false
		a.Page = "cloud"
	}
	if e.FocusID != "" {
		for i, s := range a.Tabs {
			if s.ID == e.FocusID {
				a.Active = i
				a.Page = "terminal"
				a.TabName = s.Title
				break
			}
		}
	}
}
func (a *App) cloudView(c *ui.Context) {
	s := &a.Phase3
	overview := a.P.CloudOverview()
	a.phasePage(c, "云同步 · 协议 v2（本地参考服务）", func() {
		ui.Text(c, "仅回环 HTTP/HTTPS；不连接外部服务。不与 v1 工作区互通。认证 token 与 E2E 加密口令分开。").FontSize(12)
		field(c, "本地服务 URL", &s.Endpoint)
		ui.TextInput(c.Key("cloud-auth"), &s.Auth).Label("认证 token（已保存的工作区可留空）").Password().FillWidth()
		ui.TextInput(c.Key("cloud-encryption"), &s.Encryption).Label("独立加密口令（创建/加入使用，不上传）").Password().FillWidth()
		field(c, "新工作区名称", &s.Name)
		if ui.Button(c, "创建并登录工作区").Clicked() {
			a.P.CloudConnect(s.Endpoint, "", s.Name, s.Auth, s.Encryption, false)
			s.Auth = ""
			s.Encryption = ""
		}
		field(c, "加入工作区 UUID", &s.JoinID)
		if ui.Button(c, "加入远端工作区").Clicked() {
			a.P.CloudConnect(s.Endpoint, s.JoinID, "", s.Auth, s.Encryption, true)
			s.Auth = ""
			s.Encryption = ""
		}
		for i, w := range overview.Workspaces {
			ui.Row(c.Key("cloud-workspace-" + w.ID)).Gap(6).Children(func() {
				if ui.Button(c, w.Name+" · "+w.ID).Clicked() {
					s.Workspace = w.ID
					s.ConfirmRemote = false
				}
				if i < len(overview.Statuses) {
					st := overview.Statuses[i]
					ui.Text(c, st.State)
					if st.Error != "" {
						ui.Text(c, st.Error)
					}
					if st.LastSync != "" {
						ui.Text(c, st.LastSync)
					}
					if st.RecoveryID != "" {
						ui.Text(c, "加密恢复 ID："+st.RecoveryID)
					}
				}
			})
		}
		if s.Workspace == "" {
			ui.Text(c, "创建、加入或选择工作区后可推送/拉取")
			return
		}
		ui.Text(c, "当前工作区："+s.Workspace)
		ui.Row(c).Gap(6).Children(func() {
			if ui.Button(c, "登录所选工作区").Clicked() {
				a.P.CloudConnect(s.Endpoint, s.Workspace, "", s.Auth, "", false)
				s.Auth = ""
			}
			if ui.Button(c, "立即同步").Clicked() {
				a.P.CloudSync(s.Workspace, false)
			}
			if ui.Button(c, "开启后台同步").Clicked() {
				a.P.CloudBackground(s.Workspace, true)
			}
			if ui.Button(c, "暂停后台同步").Clicked() {
				a.P.CloudBackground(s.Workspace, false)
			}
		})
		ids := []string{}
		for _, v := range a.Data.Connections {
			if a.Selected[v.ID] && !v.Deleted && v.ScopeID() == model.LocalScope {
				ids = append(ids, v.ID)
			}
		}
		if ui.Button(c, fmt.Sprintf("复制所选本地连接到云工作区（%d）", len(ids))).Clicked() {
			a.status(a.P.CloudCopy(s.Workspace, ids))
			a.reload()
		}
		for _, st := range overview.Statuses {
			if st.WorkspaceID != s.Workspace {
				continue
			}
			ui.Text(c, fmt.Sprintf("状态：%s · 后台：%t · 本地 head：%s · 远端 head：%s", st.State, st.Background, st.LocalHead, st.RemoteHead))
			if st.State == "conflict" {
				ui.Text(c, "冲突已停止自动同步，不自动覆盖。本地优先自动合并尚未实现；可暂停保留本地，或明确采用远端。")
				ui.Checkbox(c.Key("confirm-cloud-remote"), &s.ConfirmRemote, "确认替换此工作区本地数据（先保存设备密钥加密的恢复快照）")
				if ui.Button(c, "采用远端版本").Clicked() {
					if s.ConfirmRemote {
						a.P.CloudSync(s.Workspace, true)
						s.ConfirmRemote = false
					} else {
						a.Message = "请先确认冲突替换"
					}
				}
				if ui.Button(c, "保留本地并暂停").Clicked() {
					a.P.CloudBackground(s.Workspace, false)
				}
			}
		}
	})
}
func (a *App) agentView(c *ui.Context) {
	s := &a.Phase3
	state := a.P.AgentOverview()
	a.phasePage(c, "Agent 网关 · 本地 MCP", func() {
		ui.Text(c, "默认关闭；每个请求必须带 Authorization: Bearer <token>。只接受回环非浏览器调用。")
		ui.Text(c, fmt.Sprintf("端点：%s · 已暂停：%t", state.URL, state.Halted))
		ui.Row(c).Gap(6).Children(func() {
			if ui.Button(c, "启用 MCP 端点").Clicked() {
				a.status(a.P.AgentEndpoint(true))
			}
			if ui.Button(c, "关闭 MCP 端点").Clicked() {
				a.status(a.P.AgentEndpoint(false))
				s.Token = ""
			}
			if ui.Button(c, "紧急停止全部 Agent").Clicked() {
				a.P.AgentHalt(true)
			}
			if ui.Button(c, "手动恢复 Agent").Clicked() {
				a.P.AgentHalt(false)
			}
		})
		if ui.Button(c, "读取本地 MCP token").Clicked() {
			token, e := a.P.AgentToken(false)
			a.status(e)
			if e == nil {
				s.Token = token
			}
		}
		ui.Checkbox(c.Key("show-mcp-token"), &s.ShowToken, "显示 token（仅此页面，勿分享）")
		entry := ui.TextInput(c.Key("mcp-token"), &s.Token).Label("MCP token").FillWidth()
		if !s.ShowToken {
			entry.Password()
		}
		ui.Checkbox(c.Key("confirm-rotate-token"), &s.ConfirmRotate, "确认撤销旧 token / 会话并暂停 Agent")
		if ui.Button(c, "轮换 MCP token").Clicked() {
			if s.ConfirmRotate {
				token, e := a.P.AgentToken(true)
				a.status(e)
				if e == nil {
					s.Token = token
				}
				s.ConfirmRotate = false
			} else {
				a.Message = "请先确认 token 轮换"
			}
		}
		ui.TextArea(c.Key("agent-blacklist"), &s.Blacklist).Label("本次运行命令黑名单（逐行子串或 re:正则）").Height(75).FillWidth()
		if ui.Button(c, "应用命令黑名单").Clicked() {
			a.P.AgentBlacklist(s.Blacklist)
		}
		ui.Text(c, "变更必须在此批准后由原会话轮询执行。仅已连接远端 SSH 独立后台通道；前台自动输入、OSC 133 命令历史不可用。").FontSize(12)
		for _, p := range state.Pending {
			ui.Column(c.Key("consent-" + p.ID)).FillWidth().Gap(3).Children(func() {
				ui.Text(c, p.Client+" · "+p.Tool+" · "+p.Target)
				ui.Text(c, p.Summary)
				ui.Text(c, "到期："+p.ExpiresAt.In(shanghai()).Format("15:04:05 UTC+8"))
				ui.Row(c).Gap(6).Children(func() {
					if ui.Button(c.Key(p.ID+"-approve"), "批准此请求").Clicked() {
						a.P.AgentRespond(p.ID, true)
					}
					if ui.Button(c.Key(p.ID+"-deny"), "拒绝此请求").Clicked() {
						a.P.AgentRespond(p.ID, false)
					}
				})
			})
		}
		for _, ev := range state.Activity {
			ui.Text(c, ev.At+" · "+ev.Tool+" · "+ev.State)
		}
	})
}
func (a *App) shellView(c *ui.Context) {
	s := &a.Phase3
	a.phasePage(c, "Shell 集成 · 官方 terminal API", func() {
		caps := a.P.Capabilities()
		ui.Text(c, fmt.Sprintf("OSC 7：%t · 屏幕文本：%t · OSC 133：%t", caps.OSC7, caps.ScreenText, caps.OSC133))
		ui.Text(c, "OSC 133：deferred, waiting on upstream；无自写流观察器。前台执行/命令标记历史继续留空。")
		id := a.activeID()
		ui.Text(c, "终端标题："+a.P.TerminalTitle(id))
		ui.Text(c, "当前目录："+a.P.SessionDir(id))
		for _, shell := range []string{"bash", "zsh"} {
			if ui.Button(c, shell+" 会话目录钩子").Clicked() {
				s.Shell = shell
			}
		}
		ui.Text(c, "只生成当前会话 OSC 7 脚本；不改 shell 配置，不自动发送。请自行审阅工作台内容后决定是否执行。")
		if ui.Button(c, "将 "+s.Shell+" OSC 7 钩子放入工作台").Clicked() {
			command, e := a.P.ShellCommand(s.Shell)
			a.status(e)
			if e == nil {
				a.Command = strings.TrimSpace(command)
				a.ShowWorkbench = true
			}
		}
	})
}

func shanghai() *time.Location { return time.FixedZone("Asia/Shanghai", 8*60*60) }
