package appui

import (
	"fmt"
	"math"
	"nextshell.dev/v2/internal/agent"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/model"
)

// Sync, Agent, import / export and network diagnostics.

type cloudState struct {
	endpoint, workspace, name, joinID, auth, encryption string
	confirmRemote                                       bool
}
type agentState struct {
	token, blacklist string
	showToken        bool
	auto             int // the duration last chosen: 1 = 15 分钟, 2 = 1 小时
}

var autoApproveDurations = []time.Duration{0, 15 * time.Minute, time.Hour, agent.UntilExit}

// autoApproveLabel is "剩余 12 分钟" or "直到退出" while auto-approval is on.
func autoApproveLabel(v agent.AutoApproval) string {
	if v.UntilExit {
		return "直到退出"
	}
	m := max(1, int(math.Ceil(time.Until(v.Expires).Minutes())))
	if m >= 60 {
		return fmt.Sprintf("剩余 %d 小时 %d 分钟", m/60, m%60)
	}
	return fmt.Sprintf("剩余 %d 分钟", m)
}

type dataState struct {
	path, password, format, policy, folder string
	preview                                *ImportPreview
	confirmOverwrite, encrypt              bool
	exportPath, exportPassword             string
}
type diagState struct {
	id, target, text string
	busy             bool
}

// hostChecklist chooses hosts into the settings' selection.
func (a *App) hostChecklist(c *ui.Context, filter func(model.Connection) bool) []string {
	s := &a.settings
	var ids []string
	ui.Column(c).FillWidth().Gap(2).Children(func() {
		for _, cc := range a.Data.Connections {
			if cc.Deleted || !filter(cc) {
				continue
			}
			on := s.hosts[cc.ID]
			ui.Checkbox(c.Key("pick-"+cc.ID), &on, cc.Name+"  "+cc.User+"@"+cc.Host)
			s.hosts[cc.ID] = on
			if on {
				ids = append(ids, cc.ID)
			}
		}
	})
	return ids
}

func (a *App) cloudSettings(c *ui.Context) {
	s := &a.settings.cloud
	t := c.Theme()
	overview := a.P.CloudOverview()
	muted(c, "同步在本机加密后再上传；登录口令和加密口令是两个不同的口令。目前只支持本机运行的同步服务。")
	group(c, "工作区", func() {
		if len(overview.Workspaces) == 0 {
			muted(c, "还没有工作区")
		}
		states := map[string]string{"idle": "已同步", "syncing": "同步中", "conflict": "有冲突", "error": "出错", "disconnected": "未登录"}
		for i, w := range overview.Workspaces {
			row := ui.ButtonBase(c.Key("ws-"+w.ID)).Label(w.Name).FillWidth().Padding(8).Gap(8).Radius(t.Radius).Border(1, t.Border).AlignItems(ui.Center)
			if s.workspace == w.ID {
				row.Background(t.Accent.Alpha(.12))
			}
			row.Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Children(func() {
					ui.Text(c, w.Name).Bold()
					if i < len(overview.Statuses) {
						st := overview.Statuses[i]
						line := orDefault(states[st.State], st.State)
						if st.LastSync != "" {
							line += " · 上次 " + st.LastSync
						}
						muted(c, line)
						if st.Error != "" {
							ui.Text(c, st.Error).FontSize(12).TextColor(t.Danger)
						}
					}
				})
			})
			if row.Clicked() {
				s.workspace = w.ID
				s.confirmRemote = false
			}
		}
	})
	group(c, "登录", func() {
		field(c, "服务地址", &s.endpoint)
		ui.Field(c, "登录口令", func() { ui.TextInput(c.Key("cloud-auth"), &s.auth).Label("登录口令").Password().Grow(1) }).Description("已登录过的工作区可留空").FillWidth()
		ui.Field(c, "加密口令", func() {
			ui.TextInput(c.Key("cloud-encryption"), &s.encryption).Label("加密口令").Password().Grow(1)
		}).Description("只在本机使用，不会上传").FillWidth()
		field(c, "新工作区名称", &s.name)
		field(c, "加入工作区 ID", &s.joinID).Description("加入已有工作区时填写")
		actions(c, func() {
			if ui.Button(c, "加入工作区").Disabled(strings.TrimSpace(s.joinID) == "").Clicked() {
				a.P.CloudConnect(s.endpoint, s.joinID, "", s.auth, s.encryption, true)
				s.auth, s.encryption = "", ""
			}
			if ui.Button(c, "创建工作区").Clicked() {
				a.P.CloudConnect(s.endpoint, "", s.name, s.auth, s.encryption, false)
				s.auth, s.encryption = "", ""
			}
		})
	})
	if s.workspace == "" {
		return
	}
	group(c, "当前工作区", func() {
		actions(c, func() {
			if ui.Button(c, "登录").Clicked() {
				a.P.CloudConnect(s.endpoint, s.workspace, "", s.auth, "", false)
				s.auth = ""
			}
			if ui.PrimaryButton(c, "立即同步").Clicked() {
				a.P.CloudSync(s.workspace, false)
			}
		})
		for _, st := range overview.Statuses {
			if st.WorkspaceID != s.workspace {
				continue
			}
			bg := st.Background
			if toggle(c, "自动同步", &bg) {
				a.P.CloudBackground(s.workspace, bg)
			}
			if st.RecoveryID != "" {
				muted(c, "本地恢复快照："+st.RecoveryID)
			}
			if st.State == "conflict" {
				ui.Text(c, "云端和本机都有修改，自动同步已暂停。可以保留本机数据，或用云端版本替换本机（替换前会保存一份恢复快照）。").TextColor(t.Warning)
				actions(c, func() {
					if ui.Button(c, "保留本机并暂停").Clicked() {
						a.P.CloudBackground(s.workspace, false)
					}
					if ui.Button(c, "使用云端版本…").Clicked() {
						a.confirm("使用云端版本", "这个工作区在本机的数据会被云端版本替换。替换前会保存一份加密的恢复快照。", "替换本机数据", func() { a.P.CloudSync(s.workspace, true) })
					}
				})
			}
		}
	})
	group(c, "复制本机主机到工作区", func() {
		ids := a.hostChecklist(c, func(cc model.Connection) bool { return cc.ScopeID() == model.LocalScope })
		actions(c, func() {
			if ui.Button(c, fmt.Sprintf("复制 %d 个主机", len(ids))).Disabled(len(ids) == 0).Clicked() {
				a.report(a.P.CloudCopy(s.workspace, ids), "已复制到工作区")
				a.reload()
			}
		})
	})
}

func tierLabel(tier string) string {
	switch tier {
	case "risky":
		return "风险操作"
	case "sensitive":
		return "敏感文件"
	}
	return ""
}

// pendingTooltip names what waits for approval: the first request's tier, target and command.
func pendingTooltip(pending []agent.Pending) string {
	p := pending[0]
	line := orDefault(p.TargetName, p.Target) + " · " + p.Summary
	if label := tierLabel(p.Tier); label != "" {
		line = label + " · " + line
	}
	if r := []rune(line); len(r) > 160 {
		line = string(r[:160]) + "…"
	}
	return line
}

func (a *App) agentSettings(c *ui.Context) {
	s := &a.settings.agent
	t := c.Theme()
	state := a.P.AgentOverview()
	c.After(2 * time.Second)
	muted(c, "开启后，本机的 AI 助手（如 Claude Code）可以通过 MCP 读取会话并请求执行命令；每个修改都要在这里批准。")
	group(c, "服务", func() {
		on := state.URL != ""
		if toggle(c, "启用 MCP 服务", &on) {
			a.report(a.P.AgentEndpoint(on), "")
			if !on {
				s.token = ""
			}
		}
		if state.URL != "" {
			ui.Field(c, "地址", func() { ui.Text(c, state.URL).Selectable() }).FillWidth()
		}
		stopped := state.Halted
		if toggle(c, "紧急停止", &stopped) {
			a.P.AgentHalt(stopped)
		}
	})
	group(c, "自动批准", func() {
		auto := a.P.AgentAutoApproval()
		choice := 0
		if auto.On {
			choice = max(1, s.auto)
			if auto.UntilExit {
				choice = 3
			}
		}
		ui.Field(c, "自动批准", func() {
			if ui.Segmented(c, &choice, "关闭", "15 分钟", "1 小时", "直到退出").Label("自动批准").Changed() {
				if e := a.P.AgentAutoApprove(autoApproveDurations[choice]); e != nil {
					a.fail(e)
				} else {
					s.auto = choice
				}
			}
		}).Description("风险操作（sudo、rm -r、管道进 shell 等）和敏感文件仍需手动批准，黑名单与危险命令直接拒绝；重启应用后自动关闭。自动批准不是安全沙箱：只在你信任当前 Agent 任务时开启。").FillWidth()
		if auto.On {
			ui.Text(c, "自动批准中 · "+autoApproveLabel(auto)).TextColor(t.Warning)
		}
	})
	group(c, "Claude Code 插件", func() {
		path, e := a.P.MCPConfigPath()
		shown := path
		if e != nil {
			shown = e.Error()
		}
		ui.Field(c, "连接文件", func() {
			ui.Row(c).Grow(1).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, shown).Font("monospace").FontSize(12).Selectable().Grow(1).MinWidth(0)
				if ui.Button(c, "复制路径").Disabled(e != nil).Clicked() {
					a.P.CopyText(path)
					a.toast("已复制路径")
				}
			})
		}).Description("开启 MCP 服务时写入地址和令牌；Claude Code 插件会自动读取，无需手动填写").FillWidth()
	})
	group(c, "令牌", func() {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			in := ui.TextInput(c.Key("mcp-token"), &s.token).Label("令牌").Placeholder("点击「显示」读取").ReadOnly(true).Grow(1).Font("monospace")
			if !s.showToken {
				in.Password()
			}
			if ui.Button(c, "显示").Clicked() {
				token, e := a.P.AgentToken(false)
				a.report(e, "")
				if e == nil {
					s.token, s.showToken = token, true
				}
			}
			if ui.Button(c, "复制").Disabled(s.token == "").Clicked() {
				a.P.CopyText(s.token)
			}
		})
		actions(c, func() {
			if ui.Button(c, "轮换令牌…").Clicked() {
				a.confirm("轮换 MCP 令牌", "旧令牌和已连接的助手会立即失效，Agent 会暂停，需要手动恢复。", "轮换令牌", func() {
					token, e := a.P.AgentToken(true)
					a.report(e, "已轮换令牌")
					if e == nil {
						s.token = token
					}
				})
			}
		})
	})
	group(c, "待批准", func() {
		if len(state.Pending) == 0 {
			muted(c, "没有待批准的请求")
		}
		for _, p := range state.Pending {
			ui.Column(c.Key("consent-"+p.ID)).FillWidth().Padding(10).Gap(4).Radius(t.Radius).Border(1, t.Warning).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, p.Client+" · "+p.Tool).Bold()
					if label := tierLabel(p.Tier); label != "" {
						ui.Row(c).Padding(0, 6).Radius(3).Background(t.Warning.Alpha(.18)).Children(func() {
							ui.Text(c, label).FontSize(12).Bold().TextColor(t.Warning)
						})
					}
				})
				ui.Text(c, "目标 "+orDefault(p.TargetName, p.Target)).FontSize(12)
				if p.Reason != "" {
					ui.Text(c, "原因 "+p.Reason).FontSize(12)
				}
				ui.Text(c, p.Summary).Font("monospace").FontSize(12).Selectable()
				if p.Tier != "" {
					muted(c, "即使开启自动批准，这类请求也必须手动批准："+orDefault(p.Why, "需要人工确认"))
				}
				muted(c, "到期 "+p.ExpiresAt.Local().Format("15:04:05"))
				ui.Row(c).Gap(8).Children(func() {
					if ui.Button(c.Key(p.ID+"-deny"), "拒绝").Clicked() {
						a.P.AgentRespond(p.ID, false)
					}
					if ui.PrimaryButton(c.Key(p.ID+"-approve"), "批准").Clicked() {
						a.P.AgentRespond(p.ID, true)
					}
				})
			})
		}
	})
	group(c, "命令黑名单", func() {
		ui.Field(c, "规则", func() {
			ui.TextArea(c.Key("agent-blacklist"), &s.blacklist).Label("命令黑名单").Font("monospace").Height(70).Grow(1)
		}).Description("每行一条，包含即拦截；以 re: 开头为正则。本次运行有效").FillWidth()
		actions(c, func() {
			if ui.Button(c, "应用黑名单").Clicked() {
				a.P.AgentBlacklist(s.blacklist)
				a.toast("已应用黑名单")
			}
		})
	})
	group(c, "历史", func() {
		if len(state.Activity) == 0 {
			muted(c, "暂无记录")
		}
		states := map[string]string{"halted": "已停止", "resumed": "已恢复", "completed": "完成", "pending": "等待批准", "failed": "失败",
			"user_action_required": "等待用户操作", "auto-approve on": "开启自动批准", "auto-approve off": "关闭自动批准", "auto-approve expired": "自动批准已到期", "auto-approved": "已自动批准"}
		for i := len(state.Activity) - 1; i >= 0; i-- {
			ev := state.Activity[i]
			tool := ev.Tool
			if tool == "global" {
				tool = "Agent"
			}
			muted(c.Key(fmt.Sprint("activity-", i)), ev.At+" · "+tool+" · "+orDefault(states[ev.State], ev.State))
		}
	})
}

func (a *App) dataSettings(c *ui.Context) {
	s := &a.settings.data
	t := c.Theme()
	group(c, "导入", func() {
		formats := []string{"nextshell", "finalshell"}
		i := max(0, indexOf(formats, s.format))
		ui.Field(c, "来源", func() {
			if ui.Segmented(c, &i, "NextShell 1.x", "FinalShell").Label("来源").Changed() {
				s.format = formats[i]
			}
		}).FillWidth()
		if s.format == "finalshell" {
			a.localPath(c, "配置目录", "未选择", &s.path, FilePick{Directory: true, Hidden: true}).Description("FinalShell 的配置目录")
		} else {
			a.localPath(c, "连接文件", "未选择", &s.path, FilePick{}).Description("NextShell 1.x 导出的连接文件")
		}
		password(c, "解密口令", &s.password).Description("导出时没有加密就留空")
		a.folderPick(c, "导入到目录", &s.folder)
		policies := []string{"skip", "rename-copy", "overwrite"}
		j := max(0, indexOf(policies, s.policy))
		ui.Field(c, "同名主机", func() {
			if ui.Segmented(c, &j, "跳过", "另存副本", "覆盖").Label("同名主机").Changed() {
				s.policy = policies[j]
			}
		}).FillWidth()
		actions(c, func() {
			if ui.Button(c, "预览").Disabled(strings.TrimSpace(s.path) == "").Clicked() {
				a.P.PreviewImport(strings.TrimSpace(s.path), s.password, s.format)
				s.password = ""
			}
		})
		if p := s.preview; p != nil {
			ui.Column(c).FillWidth().Padding(10).Gap(4).Radius(t.Radius).Border(1, t.Border).Children(func() {
				ui.Text(c, fmt.Sprintf("将导入 %d 个主机", p.Count)).Bold()
				if p.PasswordsUnavailable > 0 {
					muted(c, fmt.Sprintf("%d 个主机的密码无法导入，连接时会询问", p.PasswordsUnavailable))
				}
				if p.KeysNeedRebind > 0 {
					muted(c, fmt.Sprintf("%d 个主机需要重新选择密钥", p.KeysNeedRebind))
				}
				muted(c, strings.Join(p.Names, "、"))
				for _, w := range p.Warnings {
					ui.Text(c, w).FontSize(12).TextColor(t.Warning)
				}
				for _, target := range p.Targets {
					muted(c, "已存在："+target)
				}
			})
			actions(c, func() { a.commitImportButton(c) })
		}
	})
	a.exportSettings(c)
}
func (a *App) commitImportButton(c *ui.Context) {
	s := &a.settings.data
	if ui.PrimaryButton(c, fmt.Sprintf("导入 %d 个主机", s.preview.Count)).Clicked() {
		run := func() { a.P.CommitImport(s.preview.Token, s.folder, s.policy) }
		if s.policy == "overwrite" && len(s.preview.Targets) > 0 {
			a.confirm("覆盖已有主机", fmt.Sprintf("%d 个已有主机会被覆盖；文件里明确为空的密码会清空已保存的密码。", len(s.preview.Targets)), fmt.Sprintf("覆盖 %d 个主机", len(s.preview.Targets)), run)
		} else {
			run()
		}
	}
}
func (a *App) exportSettings(c *ui.Context) {
	s := &a.settings.data
	group(c, "导出", func() {
		muted(c, "不勾选则导出全部主机。私钥永远不会导出。")
		ids := a.hostChecklist(c, func(model.Connection) bool { return true })
		a.localPath(c, "保存到", "未选择", &s.exportPath, FilePick{Save: true, DefaultPath: "~/nextshell-hosts.json"}).Description("不会覆盖已有文件")
		toggle(c, "加密并包含密码", &s.encrypt)
		if s.encrypt {
			password(c, "加密口令", &s.exportPassword)
		} else {
			muted(c, "不加密时不包含任何密码")
		}
		label := "导出全部主机"
		if len(ids) > 0 {
			label = fmt.Sprintf("导出 %d 个主机", len(ids))
		}
		actions(c, func() { a.exportButton(c, label, ids) })
	})
}
func (a *App) exportButton(c *ui.Context, label string, ids []string) {
	s := &a.settings.data
	if ui.Button(c, label).Disabled(strings.TrimSpace(s.exportPath) == "").Clicked() {
		var pass *string
		if s.encrypt {
			if s.exportPassword == "" {
				a.fail(fmt.Errorf("加密导出需要口令"))
				return
			}
			p := s.exportPassword
			pass = &p
		}
		a.P.ExportConnections(strings.TrimSpace(s.exportPath), pass, ids)
		s.exportPassword = ""
	}
}

func (a *App) diagnosticSettings(c *ui.Context) {
	s := &a.settings.diag
	v := &a.settings.draft
	muted(c, "从本机检测到某个 IP 的连通性。路由追踪使用你自己安装的 nexttrace。")
	group(c, "", func() {
		field(c, "目标 IP", &s.target)
		actions(c, func() {
			if ui.Button(c, "Ping").Disabled(s.busy).Clicked() {
				s.id = a.P.Diagnose("ping", s.target)
				s.busy = s.id != ""
				s.text = "正在 Ping…"
			}
			if ui.Button(c, "路由追踪").Disabled(s.busy).Clicked() {
				s.id = a.P.Diagnose("traceroute", s.target)
				s.busy = s.id != ""
				s.text = "正在追踪路由…"
			}
			if ui.Button(c, "停止").Disabled(!s.busy).Clicked() {
				a.P.CancelDiagnostic()
			}
		})
		if s.busy {
			ui.Progress(c, -1).FillWidth()
		}
		if s.text != "" {
			ui.Text(c, s.text).Font("monospace").FontSize(12).Selectable().FillWidth().Padding(8).Radius(6).Background(c.Theme().Background)
		}
	})
	group(c, "设置", func() {
		a.localPath(c, "nexttrace 程序", "在 PATH 中查找", &v.NexttracePath, FilePick{})
	})
}
