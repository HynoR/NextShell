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
	Links                      []string
	ConnectionTags             string
	Actions                    []func()
	RenameConnection           model.Connection
	RenameText                 string
	RenameOpen, TrashConfirm   bool
	TrashTargets               []model.Connection
	Prefs                      model.Preferences
	Settings                   settingsState
	Appearance, TreeSelection  string
	SidebarWidth               float32
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
	a := &App{Appearance: "跟随系统", SidebarWidth: 260, KeyAlgorithm: "ed25519", P: p, Active: -1, Page: "terminal", Selected: map[string]bool{}, Collapsed: map[string]bool{}, ShowWorkbench: true, ConnectionLayout: "table"}
	a.Prefs = p.Preferences()
	a.Settings.load(a.Prefs)
	a.Appearance = a.Prefs.Appearance
	a.Phase2.init()
	a.Phase2.ExternalProgram = a.Prefs.EditorCommand
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
	a.Edit = model.Connection{Auth: "password", Encoding: "utf-8", Port: 22, Keepalive: 0}
	a.Port = "22"
	a.Keepalive = strconv.Itoa(a.Prefs.KeepaliveSeconds)
	a.InheritKeepalive = true
	a.Password = ""
	a.ConnectionTags = ""
	a.ReplaceSecret = false
}
func (a *App) edit(c model.Connection) {
	a.Edit = c
	a.ConnectionTags = strings.Join(c.Tags, ", ")
	a.Port = strconv.Itoa(c.Port)
	a.InheritKeepalive = c.KeepaliveEnabled == nil && c.Keepalive == 0
	interval := c.Keepalive
	if c.KeepaliveIntervalSec != nil {
		interval = *c.KeepaliveIntervalSec
	} else if a.InheritKeepalive {
		interval = a.Prefs.KeepaliveSeconds
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
	c.SetTheme(designTheme(c.Theme(), a.Appearance, c.Preferences().HighContrast))
	a.P.SetWake(c.Services().Invalidate)
	a.P.SetClipboard(c.Services().WriteClipboard)
	a.P.SetURLopener(c.Services().OpenURL)
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
		if e.Link != "" {
			if a.Prefs.HyperlinkConfirm {
				a.Links = append(a.Links, e.Link)
			} else {
				a.status(a.P.OpenURL(e.Link))
			}
		}
		a.handlePhase2(e)
		a.handlePhase3(e)
		if e.Session != nil {
			light, dark := TerminalThemes(a.Prefs)
			e.Session.Terminal.SetTheme(light, dark)
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
	a.chrome(c)
	a.deleteView(c)
	a.parityDialogs(c)
	actions := a.Actions
	a.Actions = nil
	for _, fn := range actions {
		fn()
		c.Invalidate()
	}
}
func (a *App) tree(c *ui.Context, parent string, depth int) {
	if depth > 30 {
		return
	}
	for _, f := range a.Data.Folders {
		if f.ParentID != parent {
			continue
		}
		open := !a.Collapsed[f.ID]
		section := ui.SidebarSection(c.Key(f.ID+"folder"), f.Name, &open, func() { a.tree(c, f.ID, depth+1) })
		a.folderMenu(section, f)
		a.Collapsed[f.ID] = !open
	}
	for _, cc := range a.Data.Connections {
		if cc.FolderID != parent || cc.Deleted || !a.matches(cc) {
			continue
		}
		item := ui.SidebarItem(c, cc.ID, appIcons["connections"], cc.Name).Padding(8, 8, 8, 8+float32(depth)*12).Tooltip(cc.User + "@" + cc.Host)
		a.connectionMenu(item, cc)
		item.Children(func() {
			b := ui.ButtonBase(c).Label("编辑").Tooltip("编辑 "+cc.Name).Size(24, 24).Center().Children(func() { ui.Icon(c, appIcons["edit"]).FontSize(14).TextColor(c.Theme().TextMuted) })
			if b.Clicked() {
				a.edit(cc)
			}
		})
		if item.Clicked() {
			a.P.Connect(cc)
			a.Message = "正在连接…"
		}
	}
}
func (a *App) terminalView(c *ui.Context) {
	s := a.current()
	if s == nil {
		ui.Column(c).Grow(1).FillWidth().Center().Children(func() {
			t := c.Theme()
			ui.Box(c).Size(76, 76).Radius(22).Background(t.Accent.Alpha(.12)).Center().Children(func() { ui.Icon(c, appIcons["terminal"]).FontSize(40).TextColor(t.Accent) })
			ui.Text(c, "NextShell 原生终端").FontSize(30).Bold().Margin(24, 0, 8, 0)
			ui.Text(c, "从左侧选择连接，或打开本地终端").TextColor(t.TextMuted)
			ui.Row(c).Gap(12).Margin(24, 0).Children(func() {
				if ui.PrimaryButton(c, "新建 SSH 连接").Clicked() {
					a.newConnection()
					a.Page = "connections"
				}
				if ui.Button(c, "打开本地终端").Clicked() {
					a.P.Local()
				}
			})
			ui.Text(c, "首次 SSH 连接必须核对主机指纹").FontSize(12).TextColor(t.TextMuted)
		})
		return
	}
	ui.Row(c).FillWidth().Padding(10, 16).Gap(8).AlignItems(ui.Center).Background(c.Theme().Surface).Children(func() {
		ui.TextInput(c.Key(s.ID+"-tab-name"), &a.TabName).Label("标签名称").Width(160)
		if ghost(c.Key(s.ID+"-rename"), "重命名", "edit").Clicked() && strings.TrimSpace(a.TabName) != "" {
			s.Title = strings.TrimSpace(a.TabName)
			s.ManualTitle = true
			c.Invalidate()
		}
		if ghost(c, "复制会话", "activity").Clicked() {
			a.P.CloneSession(s)
		}
		if ghost(c, "重连", "proxies").Clicked() {
			a.P.Reconnect(s)
		}
		if ghost(c, "断开并关闭", "terminal").TextColor(c.Theme().Danger).Clicked() {
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
		element := terminal.View(c.Key(live.ID+"terminal"), live.Terminal).Grow(1).FillWidth().AutoFocus()
		a.terminalShortcuts(element)
	}
}
func (a *App) workbench(c *ui.Context) {
	if !a.ShowWorkbench {
		ui.Checkbox(c, &a.ShowWorkbench, "工作台").Margin(8, 20)
		return
	}
	ui.Column(c).FillWidth().Padding(12, 20).Gap(8).Background(c.Theme().Surface).BorderColor(c.Theme().Border).BorderWidth(1, 0, 0, 0).Children(func() {
		ui.Checkbox(c, &a.ShowWorkbench, "工作台 · 命令输入 / 历史（本地明文保存，勿输入密码）").FontSize(12).TextColor(c.Theme().TextMuted)
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
	ui.Field(c, label, func() { ui.TextInput(c, value).Label(label).Grow(1) }).FillWidth()
}
func password(c *ui.Context, label string, value *string) {
	ui.Field(c, label, func() { ui.TextInput(c, value).Label(label).Password().Grow(1) }).FillWidth()
}
func choice(c *ui.Context, label string, value *string, options []string) {
	ui.Field(c, label, func() { ui.Select(c, value, options).Label(label).Grow(1) }).FillWidth()
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
	ui.Scroll(c).Key("page-" + a.Page).Grow(1).FillWidth().Padding(PageInset).Children(func() {
		ui.Column(c).FillWidth().MaxWidth(1120).Padding(PanelInset).Radius(PanelRadius).Background(c.Theme().Surface).Border(1, c.Theme().Border).Gap(FormGap).Children(func() {
			ui.Form(c, func() {
				ui.Text(c, "连接管理").FontSize(26).Bold()
				choice(c, "资源视图", &a.ConnectionLayout, []string{"table", "grid"})
				ui.Row(c).Gap(5).Children(func() {
					if ui.Button(c, "新建连接").Clicked() {
						a.newConnection()
					}
					if ui.PrimaryButton(c, "保存连接").Clicked() {
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
				field(c, "标签（逗号分隔）", &a.ConnectionTags)
				field(c, "连接备注", &a.Edit.Notes)
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
				ui.Checkbox(c, &a.InheritKeepalive, "keepalive 启用状态继承应用默认")
				field(c, "keepalive 秒（0=显式关闭，继承值见设置）", &a.Keepalive)
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
						a.connectionMenu(row.Label("连接行 "+cc.Name), cc)
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
			}).FillWidth().Gap(FormGap)
		})
	})
}
func (a *App) draft() (model.Connection, error) {
	cc := a.Edit
	cc.Tags = nil
	seenTags := map[string]bool{}
	for _, tag := range strings.Split(a.ConnectionTags, ",") {
		tag = strings.TrimSpace(tag)
		if tag != "" && !seenTags[tag] {
			cc.Tags = append(cc.Tags, tag)
			seenTags[tag] = true
		}
	}
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
	ui.Scroll(c).Key("page-" + a.Page).Grow(1).FillWidth().Padding(PageInset).Children(func() {
		ui.Column(c).FillWidth().MaxWidth(1120).Padding(PanelInset).Radius(PanelRadius).Background(c.Theme().Surface).Border(1, c.Theme().Border).Gap(FormGap).Children(func() {
			ui.Form(c, func() {
				ui.Text(c, "目录管理").FontSize(26).Bold()
				field(c, "目录名称", &a.Folder.Name)
				a.folderChoice(c, "父目录", &a.Folder.ParentID)
				ui.Row(c).Gap(5).Children(func() {
					if ui.Button(c, "新目录").Clicked() {
						a.Folder = model.Folder{}
					}
					if ui.PrimaryButton(c, "保存目录").Clicked() {
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
			}).FillWidth().Gap(FormGap)
		})
	})
}
func (a *App) keysView(c *ui.Context) {
	ui.Scroll(c).Key("page-" + a.Page).Grow(1).FillWidth().Padding(PageInset).Children(func() {
		ui.Column(c).FillWidth().MaxWidth(1120).Padding(PanelInset).Radius(PanelRadius).Background(c.Theme().Surface).Border(1, c.Theme().Border).Gap(FormGap).Children(func() {
			ui.Form(c, func() {
				ui.Text(c, "SSH 密钥").FontSize(26).Bold()
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
			}).FillWidth().Gap(FormGap)
		})
	})
}
func (a *App) proxiesView(c *ui.Context) {
	ui.Scroll(c).Key("page-" + a.Page).Grow(1).FillWidth().Padding(PageInset).Children(func() {
		ui.Column(c).FillWidth().MaxWidth(1120).Padding(PanelInset).Radius(PanelRadius).Background(c.Theme().Surface).Border(1, c.Theme().Border).Gap(FormGap).Children(func() {
			ui.Form(c, func() {
				ui.Text(c, "SOCKS 代理").FontSize(26).Bold()
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
					if ui.PrimaryButton(c, "保存代理").Clicked() {
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
			}).FillWidth().Gap(FormGap)
		})
	})
}
func (a *App) deleteView(c *ui.Context) {
	if a.DeleteID == "" {
		return
	}
	open := true
	title, message := "永久删除", "确认删除 "+a.DeleteKind+"？这会永久删除记录和凭据，不能从回收站恢复。"
	if a.DeleteKind == "command" {
		title = "删除命令"
		message = "删除后无法恢复。"
		for _, v := range a.Data.Commands {
			if v.ID == a.DeleteID {
				message = "删除命令「" + v.Name + "」？删除后无法恢复。"
			}
		}
	}
	choice := ui.AlertDialog(c, &open, title, message, "取消删除", "确认删除")
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
	ui.Scroll(c).Key("page-" + a.Page).Grow(1).FillWidth().Padding(PageInset).Children(func() {
		ui.Column(c).FillWidth().MaxWidth(1120).Padding(PanelInset).Radius(PanelRadius).Background(c.Theme().Surface).Border(1, c.Theme().Border).Gap(FormGap).Children(func() {
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
	return q == "" || strings.Contains(strings.ToLower(c.Name+" "+c.Host+" "+c.User+" "+c.Notes+" "+strings.Join(c.Tags, " ")+" "+a.folderSearchText(c.FolderID)), q)
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
		if s.ManualTitle || !a.Prefs.TitleUpdates {
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

func (a *App) folderSearchText(id string) string {
	for _, f := range a.Data.Folders {
		if f.ID == id {
			return f.Name
		}
	}
	return ""
}
