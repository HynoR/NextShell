package appui

import (
	"fmt"
	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/model"
	"strconv"
	"strings"
)

type tabDrag string
type App struct {
	Phase2                     phase2State
	Phase3                     phase3State
	ConfirmExit, ExitConfirmed bool
	KeyAlgorithm               string
	Search, ConnectionLayout   string
	Collapsed                  map[string]bool
	KeyRename                  string
	ShowWorkbench              bool

	P                         Platform
	Data                      Snapshot
	Tabs                      []*Session
	Active                    int
	Page                      string
	Quick, QuickPassword      string
	Message                   string
	Logs                      []string
	Edit                      model.Connection
	InheritKeepalive          bool
	Port, Keepalive, Password string
	ReplaceSecret             bool
	Folder                    model.Folder
	Key                       model.Key
	KeyPath                   string
	Proxy                     model.Proxy
	ProxyPort, ProxyPassword  string
	ReplaceProxySecret        bool
	Command                   string
	History                   []string
	HistoryIndex              int
	TabName                   string
	ShowTrash                 bool
	Prompts                   []*Prompt
	Answers                   []string
	DeleteKind, DeleteID      string
	Selected                  map[string]bool
}

func New(p Platform) *App {
	a := &App{KeyAlgorithm: "ed25519", P: p, Active: -1, Page: "terminal", Selected: map[string]bool{}, Collapsed: map[string]bool{}, ShowWorkbench: true, ConnectionLayout: "table"}
	a.Phase2.init()
	a.Phase3.init()
	a.newConnection()
	a.Proxy = model.Proxy{Kind: "socks5"}
	a.ProxyPort = "1080"
	a.reload()
	return a
}
func (a *App) status(e error) {
	if e != nil {
		a.Message = e.Error()
	} else {
		a.Message = "已完成"
	}
	a.Logs = append(a.Logs, a.Message)
	if len(a.Logs) > 100 {
		a.Logs = a.Logs[len(a.Logs)-100:]
	}
}
func (a *App) reload() {
	s, e := a.P.Snapshot()
	if e != nil {
		a.status(e)
		return
	}
	a.Data = s
	a.History = s.History
	a.HistoryIndex = len(a.History)
}
func (a *App) newConnection() {
	a.Edit = model.Connection{Auth: "password", Encoding: "utf-8", Port: 22, Keepalive: 30}
	a.Port = "22"
	a.Keepalive = "30"
	a.InheritKeepalive = false
	a.Password = ""
	a.ReplaceSecret = false
}
func (a *App) edit(c model.Connection) {
	a.Edit = c
	a.Port = strconv.Itoa(c.Port)
	a.InheritKeepalive = c.KeepaliveEnabled == nil && c.Keepalive == 0
	interval := c.Keepalive
	if c.KeepaliveIntervalSec != nil {
		interval = *c.KeepaliveIntervalSec
	} else if a.InheritKeepalive {
		interval = model.DefaultKeepaliveSeconds
	}
	a.Keepalive = strconv.Itoa(interval)
	a.Password = ""
	a.ReplaceSecret = false
	a.Page = "connections"
}
func (a *App) current() *Session {
	if a.Active < 0 || a.Active >= len(a.Tabs) {
		return nil
	}
	return a.Tabs[a.Active]
}
func (a *App) closeTab(i int) {
	if i < 0 || i >= len(a.Tabs) {
		return
	}
	a.P.CloseSession(a.Tabs[i].ID)
	a.Tabs = append(a.Tabs[:i], a.Tabs[i+1:]...)
	if len(a.Tabs) == 0 {
		a.Active = -1
	} else if i < a.Active {
		a.Active--
	} else if a.Active >= len(a.Tabs) {
		a.Active = len(a.Tabs) - 1
	}
}
func (a *App) View(c *ui.Context) {
	a.P.SetWake(c.Services().Invalidate)
	alive := a.Prompts[:0]
	for _, p := range a.Prompts {
		select {
		case <-p.Done:
		default:
			alive = append(alive, p)
		}
	}
	a.Prompts = alive
	for _, e := range a.P.Poll() {
		a.handlePhase2(e)
		a.handlePhase3(e)
		if e.Session != nil {
			replaced := false
			for i, old := range a.Tabs {
				if e.Session.Replaces == old.ID {
					a.P.CloseSession(old.ID)
					e.Session.Title = old.Title
					e.Session.ManualTitle = old.ManualTitle
					a.Tabs[i] = e.Session
					a.Active = i
					replaced = true
					break
				}
			}
			if !replaced {
				a.Tabs = append(a.Tabs, e.Session)
				a.Active = len(a.Tabs) - 1
			}
			a.TabName = e.Session.Title
			a.Page = "terminal"
		}
		if e.Message != "" {
			a.status(fmt.Errorf("%s", e.Message))
		}
		if e.Prompt != nil {
			a.Prompts = append(a.Prompts, e.Prompt)
		}
	}
	a.refreshTitles()
	ui.Column(c).Fill().Gap(0).Children(func() {
		bar := c.TitleBar()
		ui.Row(c).FillWidth().Height(max(bar.Height, 48)).Padding(8, bar.Right+8, 8, bar.Left+8).Gap(8).DragWindow().Children(func() {
			ui.Text(c, "NextShell 2").Bold()
			ui.Checkbox(c, &a.ShowWorkbench, "工作台")
			ui.TextInput(c, &a.Quick).Placeholder("user@host:22").Grow(1)
			ui.TextInput(c, &a.QuickPassword).Placeholder("快连密码").Password().Width(160)
			if ui.Button(c, "快速连接").Clicked() {
				cc, e := model.QuickConnect(a.Quick)
				if e != nil {
					a.status(e)
				} else {
					cc.Secret = a.QuickPassword
					a.P.Connect(cc)
					a.QuickPassword = ""
					a.Message = "正在连接…"
				}
			}
			if ui.Button(c, "本地终端").Clicked() {
				a.P.Local()
			}
		})
		ui.Row(c).FillWidth().Grow(1).Children(func() {
			ui.Scroll(c).Width(230).FillHeight().Padding(8).Children(func() {
				ui.Column(c).FillWidth().Gap(5).Children(func() {
					if ui.Button(c, "连接管理").FillWidth().Clicked() {
						a.Page = "connections"
					}
					if ui.Button(c, "目录管理").FillWidth().Clicked() {
						a.Page = "folders"
					}
					ui.Text(c, "连接目录").Bold()
					ui.TextInput(c, &a.Search).Placeholder("搜索名称 / 主机")
					a.tree(c, "", 0)
					ui.Checkbox(c, &a.ShowTrash, "显示回收站")
					if a.ShowTrash {
						for _, cc := range a.Data.Connections {
							if cc.Deleted {
								ui.Row(c).Children(func() {
									ui.Text(c, cc.Name)
									if ui.Button(c, "永久删除").Key(cc.ID + "purge").Clicked() {
										a.DeleteKind = "connection"
										a.DeleteID = cc.ID
										a.Page = "connections"
									}
									if ui.Button(c, "恢复").Key(cc.ID + "restore").Clicked() {
										cc.Deleted = false
										a.status(a.P.SaveConnection(cc, nil))
										a.reload()
									}
								})
							}
						}
					}
				})
			})
			ui.Column(c).Grow(1).FillHeight().Children(func() {
				ui.ScrollHorizontal(c).FillWidth().Height(44).Children(func() {
					ui.Row(c).Gap(4).Children(func() {
						for i, t := range a.Tabs {
							row := ui.Row(c).Key(t.ID + "tab").Drag(tabDrag(t.ID)).Gap(1)
							if dragged, ok := ui.Drop[tabDrag](row); ok {
								a.moveTab(string(dragged), i)
							}
							row.Children(func() {
								label := t.Title
								if i == a.Active {
									label = "● " + label
								}
								select {
								case <-t.Terminal.Done():
									label += " [已退出]"
								default:
								}
								if ui.Button(c, label).Clicked() {
									a.Active = i
									a.TabName = t.Title
									a.Page = "terminal"
								}
								if ui.Button(c, "×").Clicked() {
									a.closeTab(i)
								}
							})
						}
					})
				})
				if len(a.Prompts) > 0 {
					a.promptView(c)
				} else {
					switch a.Page {
					case "connections":
						a.connectionsView(c)
					case "folders":
						a.foldersView(c)
					case "keys":
						a.keysView(c)
					case "proxies":
						a.proxiesView(c)
					case "files":
						a.filesView(c)
					case "transfers":
						a.transfersView(c)
					case "editor":
						a.editorView(c)
					case "monitor":
						a.monitorView(c)
					case "commands":
						a.commandsView(c)
					case "data":
						a.dataView(c)
					case "cloud":
						a.cloudView(c)
					case "agent":
						a.agentView(c)
					case "shell":
						a.shellView(c)
					case "activity":
						a.activityView(c)
					default:
						a.terminalView(c)
					}
				}
				a.workbench(c)
			})
			ui.Column(c).Width(126).FillHeight().Gap(8).Padding(8).Children(func() {
				for _, v := range []struct{ ID, Label string }{{"terminal", "终端"}, {"connections", "连接"}, {"keys", "密钥管理"}, {"proxies", "代理管理"}, {"files", "文件"}, {"transfers", "传输"}, {"editor", "远程编辑"}, {"monitor", "监控"}, {"commands", "命令库"}, {"data", "导入导出"}, {"cloud", "云同步"}, {"agent", "Agent 网关"}, {"shell", "Shell 集成"}, {"activity", "活动面板"}} {
					if ui.Button(c, v.Label).FillWidth().Clicked() {
						a.Page = v.ID
					}
				}
			})
		})
		if a.ConfirmExit {
			ui.Row(c).FillWidth().Padding(8).Gap(8).Children(func() {
				ui.Text(c, fmt.Sprintf("仍有 %d 个会话，退出将断开连接", len(a.Tabs)))
				if ui.Button(c, "退出应用").Clicked() {
					a.ExitConfirmed = true
					a.P.Quit()
				}
				if ui.Button(c, "继续使用").Clicked() {
					a.ConfirmExit = false
				}
			})
		}
		ui.Text(c, a.Message).FillWidth().Padding(5).FontSize(12)
	})
	a.deleteView(c)
}
func (a *App) tree(c *ui.Context, parent string, depth int) {
	if depth > 30 {
		return
	}
	for _, f := range a.Data.Folders {
		if f.ParentID == parent {
			label := "▾ "
			if a.Collapsed[f.ID] {
				label = "▸ "
			}
			if ui.Button(c, strings.Repeat("  ", depth)+label+f.Name).Key(f.ID + "folder").Clicked() {
				a.Collapsed[f.ID] = !a.Collapsed[f.ID]
			}
			if !a.Collapsed[f.ID] {
				a.tree(c, f.ID, depth+1)
			}
		}
	}
	for _, cc := range a.Data.Connections {
		if cc.FolderID != parent || cc.Deleted || !a.matches(cc) {
			continue
		}
		ui.Row(c).Key(cc.ID + "row").Gap(4).Children(func() {
			if ui.Button(c, strings.Repeat("  ", depth)+cc.Name).Grow(1).Clicked() {
				a.P.Connect(cc)
				a.Message = "正在连接…"
			}
			if ui.Button(c, "编辑").Clicked() {
				a.edit(cc)
			}
		})
	}
}
func (a *App) terminalView(c *ui.Context) {
	s := a.current()
	if s == nil {
		ui.Column(c).Grow(1).FillWidth().Center().Children(func() {
			ui.Text(c, "NextShell 原生终端").FontSize(28)
			ui.Text(c, "从左侧选择连接，或打开本地终端")
			ui.Text(c, "首次 SSH 连接必须核对主机指纹")
		})
		return
	}
	ui.Row(c).FillWidth().Gap(4).Children(func() {
		ui.TextInput(c.Key(s.ID+"-tab-name"), &a.TabName).Label("标签名称").Width(160)
		if ui.Button(c.Key(s.ID+"-rename"), "重命名").Clicked() && strings.TrimSpace(a.TabName) != "" {
			s.Title = strings.TrimSpace(a.TabName)
			s.ManualTitle = true
			c.Invalidate()
		}
		if ui.Button(c, "复制会话").Clicked() {
			a.P.CloneSession(s)
		}
		if ui.Button(c, "重连").Clicked() {
			a.P.Reconnect(s)
		}
		if ui.Button(c, "断开并关闭").Clicked() {
			a.closeTab(a.Active)
		}
		if ui.Button(c, "←").Clicked() && a.Active > 0 {
			i := a.Active
			a.Tabs[i-1], a.Tabs[i] = a.Tabs[i], a.Tabs[i-1]
			a.Active--
		}
		if ui.Button(c, "→").Clicked() && a.Active < len(a.Tabs)-1 {
			i := a.Active
			a.Tabs[i+1], a.Tabs[i] = a.Tabs[i], a.Tabs[i+1]
			a.Active++
		}
	})
	if live := a.current(); live != nil {
		terminal.View(c.Key(live.ID+"terminal"), live.Terminal).Grow(1).FillWidth().AutoFocus()
	}
}
func (a *App) workbench(c *ui.Context) {
	if !a.ShowWorkbench {
		return
	}
	ui.Column(c).FillWidth().Padding(6).Gap(4).Children(func() {
		ui.Text(c, "工作台 · 命令输入 / 历史（本地明文保存，勿输入密码）").FontSize(12)
		ui.Row(c).FillWidth().Gap(6).Children(func() {
			ui.TextInput(c, &a.Command).Label("命令输入").Placeholder("向当前会话发送命令").Grow(1).OnTextKey(func(k ui.TextKey) bool {
				if k.Key == ui.KeyEnter {
					a.Command = k.Text()
					a.sendCommand()
					k.SetText("")
					return true
				}
				return false
			})
			if ui.Button(c, "发送").Clicked() {
				a.sendCommand()
			}
			if ui.Button(c, "上一条").Clicked() && len(a.History) > 0 {
				if a.HistoryIndex > 0 {
					a.HistoryIndex--
				}
				a.Command = a.History[a.HistoryIndex]
			}
		})
	})
}
func (a *App) sendCommand() {
	if s := a.current(); s != nil && a.Command != "" {
		if e := a.P.Send(s.ID, a.Command+"\r"); e != nil {
			a.status(e)
			return
		}
		a.History = append(a.History, a.Command)
		if len(a.History) > 100 {
			a.History = a.History[len(a.History)-100:]
		}
		a.HistoryIndex = len(a.History)
		a.Command = ""
		a.status(a.P.SaveHistory(a.History))
	}
}
func field(c *ui.Context, label string, value *string) {
	ui.Row(c).FillWidth().Gap(8).Children(func() { ui.Text(c, label).Width(120); ui.TextInput(c, value).Label(label).Grow(1) })
}
func password(c *ui.Context, label string, value *string) {
	ui.Row(c).FillWidth().Gap(8).Children(func() { ui.Text(c, label).Width(120); ui.TextInput(c, value).Label(label).Password().Grow(1) })
}
func choice(c *ui.Context, label string, value *string, options []string) {
	ui.Row(c).FillWidth().Gap(8).Children(func() { ui.Text(c, label).Width(120); ui.Select(c, value, options).Label(label).Grow(1) })
}
func chooseID(c *ui.Context, label string, id *string, ids, names []string) {
	name := "（无）"
	opts := []string{name}
	for i, v := range ids {
		n := names[i] + " [" + v[:min(8, len(v))] + "]"
		opts = append(opts, n)
		if *id == v {
			name = n
		}
	}
	choice(c, label, &name, opts)
	*id = ""
	for i, n := range opts[1:] {
		if n == name {
			*id = ids[i]
		}
	}
}
func (a *App) folderChoice(c *ui.Context, label string, id *string) {
	var ids, names []string
	for _, f := range a.Data.Folders {
		ids = append(ids, f.ID)
		names = append(names, f.Name)
	}
	chooseID(c, label, id, ids, names)
}
func (a *App) keyChoice(c *ui.Context, id *string) {
	var ids, names []string
	for _, f := range a.Data.Keys {
		ids = append(ids, f.ID)
		names = append(names, f.Name)
	}
	chooseID(c, "密钥", id, ids, names)
}
func (a *App) proxyChoice(c *ui.Context, id *string) {
	var ids, names []string
	for _, f := range a.Data.Proxies {
		ids = append(ids, f.ID)
		names = append(names, f.Name)
	}
	chooseID(c, "代理", id, ids, names)
}
func (a *App) connectionsView(c *ui.Context) {
	ui.Scroll(c).Key("page-" + a.Page).Grow(1).FillWidth().Padding(12).Children(func() {
		ui.Column(c).FillWidth().Gap(8).Children(func() {
			ui.Text(c, "连接管理").FontSize(22)
			choice(c, "资源视图", &a.ConnectionLayout, []string{"table", "grid"})
			ui.Row(c).Gap(5).Children(func() {
				if ui.Button(c, "新建连接").Clicked() {
					a.newConnection()
				}
				if ui.Button(c, "保存连接").Clicked() {
					a.saveConnection()
				}
				if ui.Button(c, "连接当前配置").Clicked() {
					a.connectDraft()
				}
				if a.Edit.ID != "" && ui.Button(c, "移入回收站").Clicked() {
					a.Edit.Deleted = true
					a.status(a.P.SaveConnection(a.Edit, nil))
					a.reload()
					a.newConnection()
				}
			})
			field(c, "名称", &a.Edit.Name)
			field(c, "主机", &a.Edit.Host)
			field(c, "端口", &a.Port)
			field(c, "用户", &a.Edit.User)
			choice(c, "认证", &a.Edit.Auth, []string{"password", "key", "agent", "keyboard"})
			password(c, "密码（不回显）", &a.Password)
			ui.Checkbox(c, &a.ReplaceSecret, "替换已保存密码（空值会清除）")
			a.keyChoice(c, &a.Edit.KeyID)
			a.proxyChoice(c, &a.Edit.ProxyID)
			a.folderChoice(c, "目录", &a.Edit.FolderID)
			ui.Checkbox(c.Key("use-known-hosts"), &a.Edit.UseKnownHosts, "只读 ~/.ssh/known_hosts（数据库指纹优先）")
			field(c, "固定主机指纹", &a.Edit.Fingerprint)
			choice(c, "编码", &a.Edit.Encoding, []string{"utf-8", "gb18030", "gbk", "big5", "shift_jis"})
			ui.Checkbox(c, &a.InheritKeepalive, "keepalive 启用状态继承应用默认（启用）")
			field(c, "keepalive 秒（0=显式关闭，默认30）", &a.Keepalive)
			ui.Text(c, "所有连接（勾选后可批量绑定上述认证、密钥和代理）").Bold()
			var container ui.Element
			if a.ConnectionLayout == "grid" {
				container = ui.Grid(c).Columns(2).FillWidth().Gap(8)
			} else {
				container = ui.Column(c).FillWidth().Gap(6)
			}
			container.Children(func() {
				for _, cc := range a.Data.Connections {
					if cc.Deleted {
						continue
					}
					var row ui.Element
					if a.ConnectionLayout == "grid" {
						row = ui.Column(c).Key(cc.ID + "card").Padding(10).Gap(6)
					} else {
						row = ui.Row(c).Key(cc.ID + "manage").Gap(6)
					}
					row.Children(func() {
						sel := a.Selected[cc.ID]
						ui.Checkbox(c, &sel, cc.Name+" · "+cc.User+"@"+cc.Address())
						a.Selected[cc.ID] = sel
						if ui.Button(c, "编辑").Clicked() {
							a.edit(cc)
						}
						if ui.Button(c, "复制配置").Clicked() {
							copy, e := a.P.DuplicateConnection(cc.ID)
							a.status(e)
							if e == nil {
								a.reload()
								a.edit(copy)
							}
						}
					})
				}
			})
			if ui.Button(c, "批量绑定认证").Clicked() {
				a.batchBind()
			}
		})
	})
}
func (a *App) draft() (model.Connection, error) {
	cc := a.Edit
	var e error
	cc.Port, e = strconv.Atoi(a.Port)
	if e != nil {
		return cc, fmt.Errorf("无效端口")
	}
	cc.Keepalive, e = strconv.Atoi(a.Keepalive)
	if e != nil {
		return cc, fmt.Errorf("无效 keepalive")
	}
	if a.InheritKeepalive {
		cc.KeepaliveEnabled = nil
		if cc.Keepalive != model.DefaultKeepaliveSeconds {
			interval := cc.Keepalive
			cc.KeepaliveIntervalSec = &interval
		} else if a.Edit.KeepaliveIntervalSec == nil {
			cc.KeepaliveIntervalSec = nil
		}
		cc.Keepalive = 0
	} else {
		enabled := cc.Keepalive > 0
		cc.KeepaliveEnabled = &enabled
		interval := cc.Keepalive
		cc.KeepaliveIntervalSec = &interval
	}
	return cc, cc.Validate()
}
func (a *App) saveConnection() {
	cc, e := a.draft()
	if e != nil {
		a.status(e)
		return
	}
	fresh := cc.ID == ""
	if fresh {
		cc.ID = a.P.NewID()
	}
	var sec *string
	if fresh || a.ReplaceSecret || a.Password != "" {
		sec = &a.Password
	}
	e = a.P.SaveConnection(cc, sec)
	a.status(e)
	if e == nil {
		a.Edit = cc
		a.Password = ""
		a.ReplaceSecret = false
		a.reload()
	}
}
func (a *App) connectDraft() {
	cc, e := a.draft()
	if e != nil {
		a.status(e)
		return
	}
	cc.Secret = a.Password
	a.P.Connect(cc)
	a.Password = ""
}
func (a *App) batchBind() {
	for _, cc := range a.Data.Connections {
		if !a.Selected[cc.ID] {
			continue
		}
		cc.Auth = a.Edit.Auth
		cc.KeyID = a.Edit.KeyID
		cc.ProxyID = a.Edit.ProxyID
		var secret *string
		if a.ReplaceSecret || a.Password != "" {
			secret = &a.Password
		}
		if e := a.P.SaveConnection(cc, secret); e != nil {
			a.status(e)
			return
		}
	}
	a.Password = ""
	a.reload()
	a.status(nil)
}
func (a *App) foldersView(c *ui.Context) {
	ui.Scroll(c).Key("page-" + a.Page).Grow(1).FillWidth().Padding(12).Children(func() {
		ui.Column(c).FillWidth().Gap(8).Children(func() {
			ui.Text(c, "目录管理").FontSize(22)
			field(c, "目录名称", &a.Folder.Name)
			a.folderChoice(c, "父目录", &a.Folder.ParentID)
			ui.Row(c).Gap(5).Children(func() {
				if ui.Button(c, "新目录").Clicked() {
					a.Folder = model.Folder{}
				}
				if ui.Button(c, "保存目录").Clicked() {
					f := a.Folder
					if f.ID == "" {
						f.ID = a.P.NewID()
					}
					e := a.P.SaveFolder(f)
					a.status(e)
					if e == nil {
						a.Folder = f
						a.reload()
					}
				}
			})
			for _, f := range a.Data.Folders {
				ui.Row(c).Key(f.ID).Gap(6).Children(func() {
					ui.Text(c, f.Name).Grow(1)
					if ui.Button(c, "编辑").Clicked() {
						a.Folder = f
					}
					if ui.Button(c, "删除").Clicked() {
						a.DeleteKind = "folder"
						a.DeleteID = f.ID
					}
				})
			}
		})
	})
}
func (a *App) keysView(c *ui.Context) {
	ui.Scroll(c).Key("page-" + a.Page).Grow(1).FillWidth().Padding(12).Children(func() {
		ui.Column(c).FillWidth().Gap(8).Children(func() {
			ui.Text(c, "SSH 密钥").FontSize(22)
			field(c, "名称", &a.Key.Name)
			choice(c, "算法", &a.KeyAlgorithm, []string{"ed25519", "rsa-2048", "rsa-4096"})
			field(c, "导入文件路径", &a.KeyPath)
			password(c, "密钥口令", &a.Key.Passphrase)
			ui.Text(c, "也可粘贴 PEM / OpenSSH 私钥（仅保存时读取）")
			ui.TextArea(c, &a.Key.Private).Height(130)
			ui.Row(c).Gap(6).Children(func() {
				if ui.Button(c, "生成密钥").Clicked() {
					_, e := a.P.GenerateKey(a.Key.Name, a.KeyAlgorithm)
					a.status(e)
					a.Key = model.Key{}
					a.reload()
				}
				if ui.Button(c, "导入文件").Clicked() {
					_, e := a.P.ImportKey(a.KeyPath, a.Key.Passphrase)
					a.status(e)
					a.Key = model.Key{}
					a.reload()
				}
				if ui.Button(c, "保存粘贴密钥").Clicked() {
					k := a.Key
					if k.ID == "" {
						k.ID = a.P.NewID()
					}
					a.status(a.P.SaveKey(k))
					a.Key = model.Key{}
					a.reload()
				}
			})
			for _, k := range a.Data.Keys {
				ui.Column(c).Key(k.ID).Children(func() {
					ui.Text(c, k.Name).Bold()
					ui.Text(c, k.Public).FontSize(11)
					field(c, "新密钥名称", &a.KeyRename)
					if ui.Button(c, "重命名密钥").Clicked() {
						a.status(a.P.RenameKey(k.ID, a.KeyRename))
						a.reload()
					}
					ui.Row(c).Children(func() {
						if ui.Button(c, "删除密钥").Clicked() {
							a.DeleteKind = "key"
							a.DeleteID = k.ID
						}
					})
				})
			}
		})
	})
}
func (a *App) proxiesView(c *ui.Context) {
	ui.Scroll(c).Key("page-" + a.Page).Grow(1).FillWidth().Padding(12).Children(func() {
		ui.Column(c).FillWidth().Gap(8).Children(func() {
			ui.Text(c, "SOCKS 代理").FontSize(22)
			field(c, "名称", &a.Proxy.Name)
			choice(c, "类型", &a.Proxy.Kind, []string{"socks4", "socks5"})
			field(c, "主机", &a.Proxy.Host)
			field(c, "端口", &a.ProxyPort)
			field(c, "用户", &a.Proxy.User)
			password(c, "密码", &a.ProxyPassword)
			ui.Checkbox(c, &a.ReplaceProxySecret, "替换代理密码")
			ui.Row(c).Gap(6).Children(func() {
				if ui.Button(c, "新代理").Clicked() {
					a.Proxy = model.Proxy{Kind: "socks5"}
					a.ProxyPort = "1080"
					a.ProxyPassword = ""
				}
				if ui.Button(c, "保存代理").Clicked() {
					p := a.Proxy
					port, e := strconv.Atoi(a.ProxyPort)
					if e != nil {
						a.status(e)
						return
					}
					p.Port = port
					var sec *string
					if p.ID == "" || a.ReplaceProxySecret || a.ProxyPassword != "" {
						sec = &a.ProxyPassword
					}
					if p.ID == "" {
						p.ID = a.P.NewID()
					}
					e = a.P.SaveProxy(p, sec)
					a.status(e)
					if e == nil {
						a.Proxy = p
						a.ProxyPassword = ""
						a.ReplaceProxySecret = false
						a.reload()
					}
				}
			})
			for _, p := range a.Data.Proxies {
				ui.Row(c).Key(p.ID).Gap(6).Children(func() {
					ui.Text(c, p.Name+" · "+p.Kind).Grow(1)
					if ui.Button(c, "编辑").Clicked() {
						a.Proxy = p
						a.ProxyPort = strconv.Itoa(p.Port)
						a.ProxyPassword = ""
					}
					if ui.Button(c, "删除").Clicked() {
						a.DeleteKind = "proxy"
						a.DeleteID = p.ID
					}
				})
			}
		})
	})
}
func (a *App) deleteView(c *ui.Context) {
	if a.DeleteID == "" {
		return
	}
	open := true
	choice := ui.AlertDialog(c, &open, "永久删除", "确认删除 "+a.DeleteKind+"？这会永久删除记录和凭据，不能从回收站恢复。", "取消删除", "确认删除")
	if choice == 1 {
		if e := a.P.Delete(a.DeleteKind, a.DeleteID); e != nil {
			a.status(e)
		} else {
			a.DeleteID = ""
			a.reload()
			a.Message = "已永久删除"
		}
	}
	if !open && choice != 1 {
		a.DeleteID = ""
	}
}

func (a *App) activityView(c *ui.Context) {
	ui.Scroll(c).Key("page-" + a.Page).Grow(1).FillWidth().Padding(12).Children(func() {
		ui.Column(c).FillWidth().Gap(8).Children(func() {
			ui.Text(c, "活动记录（不记录密码或终端内容）").Bold()
			for _, l := range a.Logs {
				ui.Text(c, l)
			}
			ui.Text(c, "会话切换器与预览").Bold()
			caps := a.P.Capabilities()
			ui.Text(c, fmt.Sprintf("终端 API: OSC7=%v, Text=%v, OSC133=%v（等待上游）", caps.OSC7, caps.ScreenText, caps.OSC133))
			for i, s := range a.Tabs {
				if ui.Button(c, s.Title).Key(s.ID + "switch").Clicked() {
					a.Active = i
					a.Page = "terminal"
				}
				preview := a.P.ScreenText(s.ID)
				r := []rune(preview)
				if len(r) > 1200 {
					preview = string(r[len(r)-1200:])
				}
				ui.TextArea(c.Key(s.ID+"preview"), &preview).ReadOnly(true).Height(130)
			}
		})
	})
}
func (a *App) promptView(c *ui.Context) {
	p := a.Prompts[0]
	if len(a.Answers) != len(p.Questions) {
		a.Answers = make([]string, len(p.Questions))
	}
	ui.Scroll(c).Grow(1).FillWidth().Padding(16).Children(func() {
		ui.Column(c).FillWidth().Gap(10).Children(func() {
			ui.Text(c, p.Title).Bold()
			for i, q := range p.Questions {
				ui.Text(c, q)
				e := ui.TextInput(c.Key(fmt.Sprint("answer", i)), &a.Answers[i]).Label(q)
				if i >= len(p.Echo) || !p.Echo[i] {
					e.Password()
				}
			}
			ui.Row(c).Gap(8).Children(func() {
				if ui.Button(c, "继续认证").Clicked() {
					select {
					case p.Answers <- append([]string{}, a.Answers...):
					default:
					}
					a.Prompts = a.Prompts[1:]
					a.Answers = nil
				}
				if ui.Button(c, "取消认证").Clicked() {
					select {
					case p.Answers <- nil:
					default:
					}
					a.Prompts = a.Prompts[1:]
					a.Answers = nil
				}
			})
		})
	})
}

func (a *App) matches(c model.Connection) bool {
	q := strings.ToLower(strings.TrimSpace(a.Search))
	return q == "" || strings.Contains(strings.ToLower(c.Name+" "+c.Host+" "+c.User), q)
}
func (a *App) moveTab(id string, to int) {
	from := -1
	active := ""
	if s := a.current(); s != nil {
		active = s.ID
	}
	for i, s := range a.Tabs {
		if s.ID == id {
			from = i
			break
		}
	}
	if from < 0 || to < 0 || to >= len(a.Tabs) || from == to {
		return
	}
	s := a.Tabs[from]
	a.Tabs = append(a.Tabs[:from], a.Tabs[from+1:]...)
	a.Tabs = append(a.Tabs, nil)
	copy(a.Tabs[to+1:], a.Tabs[to:])
	a.Tabs[to] = s
	for i, s := range a.Tabs {
		if s.ID == active {
			a.Active = i
		}
	}
}

func (a *App) refreshTitles() {
	for _, s := range a.Tabs {
		if s.ManualTitle {
			continue
		}
		next := a.P.TerminalTitle(s.ID)
		if next != "" && next != s.Title {
			old := s.Title
			s.Title = next
			if a.current() == s && a.TabName == old {
				a.TabName = next
			}
		}
	}
}
