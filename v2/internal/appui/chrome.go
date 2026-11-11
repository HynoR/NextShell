package appui

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/model"
)

type navigationItem struct{ ID, Label string }

var navigation = []navigationItem{{"terminal", "终端"}, {"connections", "连接"}, {"keys", "密钥管理"}, {"proxies", "代理管理"}, {"files", "文件"}, {"transfers", "传输"}, {"editor", "远程编辑"}, {"monitor", "监控"}, {"commands", "命令库"}, {"data", "导入导出"}, {"cloud", "云同步"}, {"agent", "Agent 网关"}, {"shell", "Shell 集成"}, {"activity", "活动面板"}}

func ghost(c *ui.Context, label, icon string) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c).Label(label).Tooltip(label).Padding(7, 10).Radius(t.Radius).Gap(7).AlignItems(ui.Center)
	if b.Hovered() {
		b.Background(t.SurfaceHover)
	}
	b.Children(func() {
		if s := appIcons[icon]; s != nil {
			ui.Icon(c, s).FontSize(17)
		}
		ui.Text(c, label).SingleLine()
	})
	return b
}
func (a *App) chrome(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().Children(func() {
		a.titlebar(c)
		ui.Row(c).FillWidth().Grow(1).MinHeight(0).Children(func() {
			ui.Split(c, &a.SidebarWidth, func() { a.sidebar(c) }, func() {
				ui.Column(c).Fill().MinWidth(0).Children(func() {
					a.tabstrip(c)
					ui.Column(c).Grow(1).FillWidth().MinHeight(0).Children(func() {
						if len(a.Prompts) > 0 {
							a.promptView(c)
						} else {
							a.pageView(c)
						}
					})
					a.workbench(c)
				})
			}).SplitReset(260).Grow(1).FillHeight()
			a.activityRail(c)
		})
		if a.ConfirmExit {
			ui.Row(c).FillWidth().Padding(12).Gap(12).Background(t.Surface).Children(func() {
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
		ui.Row(c).FillWidth().Height(28).Padding(4, 16).AlignItems(ui.Center).Background(t.Surface).BorderColor(t.Border).BorderWidth(1, 0, 0, 0).Children(func() {
			msg := a.Message
			if msg == "" {
				msg = "就绪 · 本地优先，安全连接"
			}
			ui.Text(c, msg).Grow(1).SingleLine().FontSize(12).TextColor(t.TextMuted)
			ui.Text(c, fmt.Sprintf("%d 个会话  ·  SSH / SFTP", len(a.Tabs))).FontSize(12).TextColor(t.TextMuted)
		})
	})
}
func (a *App) titlebar(c *ui.Context) {
	t := c.Theme()
	bar := c.TitleBar()
	ui.Row(c).FillWidth().Height(max(bar.Height, 60)).Padding(10, bar.Right+16, 10, bar.Left+16).Gap(12).AlignItems(ui.Center).Background(t.Surface).BorderColor(t.Border).BorderWidth(0, 0, 1, 0).DragWindow().Children(func() {
		ui.Box(c).Size(32, 32).Radius(9).Background(t.Accent).Center().Children(func() { ui.Icon(c, appIcons["terminal"]).FontSize(21).TextColor(t.AccentText) })
		ui.Text(c, "NextShell 2").FontSize(17).Bold().Margin(0, 12, 0, 0)
		ui.TextInput(c, &a.Quick).Label("快速连接地址").Placeholder("user@host:22").Grow(1).MinWidth(100)
		ui.TextInput(c, &a.QuickPassword).Placeholder("快连密码").Password().Width(130)
		if ui.PrimaryButton(c, "快速连接").Clicked() {
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
		if ghost(c, "本地终端", "terminal").Clicked() {
			a.P.Local()
		}
		ui.Select(c, &a.Appearance, []string{"跟随系统", "浅色", "深色"}).Label("外观").Width(106)
	})
}
func (a *App) sidebar(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().Background(t.Surface).Padding(12, 8).Gap(8).Children(func() {
		ui.Row(c).Padding(4, 8).Gap(8).Children(func() {
			ui.Text(c, "工作空间").FontSize(12).Bold().TextColor(t.TextMuted)
			ui.Text(c, fmt.Sprint(len(a.Data.Connections))).FontSize(12).TextColor(t.TextMuted)
		})
		ui.Row(c).FillWidth().Gap(2).Children(func() {
			if ghost(c, "连接管理", "connections").Clicked() {
				a.Page = "connections"
			}
			if ghost(c, "目录管理", "folder").Clicked() {
				a.Page = "folders"
			}
		})
		ui.SearchField(c, &a.Search).Label("搜索连接").Placeholder("搜索名称 / 主机").Margin(0, 6)
		ui.Sidebar(c.Key("connections-sidebar"), &a.TreeSelection, func() { a.tree(c, "", 0) }).Label("连接目录").Grow(1).FillWidth().Padding(2)
		ui.Checkbox(c, &a.ShowTrash, "显示回收站").Margin(4, 8)
		if a.ShowTrash {
			ui.Scroll(c).MaxHeight(180).FillWidth().Children(func() {
				for _, cc := range a.Data.Connections {
					if !cc.Deleted {
						continue
					}
					ui.Row(c).Key(cc.ID + "trash").AlignItems(ui.Center).Gap(4).Children(func() {
						ui.Text(c, cc.Name).Grow(1).SingleLine()
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
			})
		}
	})
}
func (a *App) activityRail(c *ui.Context) {
	t := c.Theme()
	ui.Scroll(c).Width(64).FillHeight().Padding(10, 8).Gap(4).Background(t.Surface).BorderColor(t.Border).BorderWidth(0, 0, 0, 1).Children(func() {
		for _, v := range navigation {
			b := ui.ButtonBase(c).Key("nav-"+v.ID).Label(v.Label).Tooltip(v.Label).Size(44, 40).Radius(9).Center().TextColor(t.TextMuted)
			if a.Page == v.ID {
				b.Background(t.Accent.Alpha(.15)).TextColor(t.Accent).BorderColor(t.Accent).BorderWidth(0, 0, 0, 2)
			} else if b.Hovered() {
				b.Background(t.SurfaceHover).TextColor(t.Text)
			}
			b.Children(func() { ui.Icon(c, appIcons[v.ID]).FontSize(22) })
			if b.Clicked() {
				a.Page = v.ID
			}
		}
	})
}
func (a *App) tabstrip(c *ui.Context) {
	t := c.Theme()
	ui.ScrollHorizontal(c).Height(48).FillWidth().Background(t.Surface).BorderColor(t.Border).BorderWidth(0, 0, 1, 0).Children(func() {
		if len(a.Tabs) == 0 {
			ui.Text(c, "工作空间 / 概览").FontSize(12).TextColor(t.TextMuted).Padding(16, 20)
			return
		}
		tabs := ui.TabsBase(c, &a.Active, len(a.Tabs))
		tabs.List.AlignItems(ui.Stretch).Gap(2).Padding(5, 8).Children(func() {
			for i, s := range a.Tabs {
				tab := tabs.Tab(i).Label(s.Title).Padding(5, 12).Gap(8).Radius(7).AlignItems(ui.Center).Drag(tabDrag(s.ID))
				if x, ok := ui.Drop[tabDrag](tab); ok {
					a.moveTab(string(x), i)
				}
				if i == a.Active {
					tab.Background(t.Background).TextColor(t.Accent).BorderColor(t.Accent).BorderWidth(0, 0, 2, 0)
				} else if tab.Hovered() {
					tab.Background(t.SurfaceHover)
				}
				tab.Children(func() {
					ui.Icon(c, appIcons["terminal"]).FontSize(15)
					title := s.Title
					if i == a.Active {
						title = "● " + title
					}
					select {
					case <-s.Terminal.Done():
						title += " [已退出]"
					default:
					}
					ui.Text(c, title).SingleLine().MaxWidth(220)
					if ui.ButtonBase(c).Label("关闭标签 "+s.Title).Tooltip("关闭标签").Size(20, 20).Center().Children(func() { ui.Text(c, "×").TextColor(t.TextMuted) }).Clicked() {
						a.closeTab(i)
					}
				})
				if tab.Clicked() {
					a.Active = i
					a.TabName = s.Title
					a.Page = "terminal"
				}
			}
		})
		if tabs.List.Changed() {
			if active := a.current(); active != nil {
				a.TabName = active.Title
				a.Page = "terminal"
			}
		}
	})
}
func (a *App) pageView(c *ui.Context) {
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
