package appui

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/model"
)

type welcomeState struct{ quick string }

// quickConnect connects to a saved host by name, or to user@host[:port].
func (a *App) quickConnect(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	for _, cc := range a.Data.Connections {
		if !cc.Deleted && strings.EqualFold(cc.Name, text) {
			a.connect(cc)
			return true
		}
	}
	cc, e := model.QuickConnect(text)
	if e != nil {
		a.fail(e)
		return false
	}
	a.connect(cc)
	return true
}

func (a *App) welcomeView(c *ui.Context) {
	t := c.Theme()
	ui.Scroll(c).Fill().Children(func() {
		ui.Column(c).FillWidth().Padding(64, 32).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Width(560).Gap(24).Children(func() {
				ui.Text(c, "NextShell").FontSize(28).Bold().AlignSelf(ui.Center)
				in := ui.TextInput(c, &a.welcome.quick).Label("快速连接").Placeholder("user@host 或主机名，回车连接").FillWidth().Height(36).AutoFocus()
				if in.Submitted() && a.quickConnect(a.welcome.quick) {
					a.welcome.quick = ""
				}
				hosts := a.hosts.recentHosts(a, 5)
				if len(hosts) == 0 {
					ui.Column(c).FillWidth().Gap(8).Children(func() {
						muted(c, "开始使用")
						guide(c, "新建主机", "填写地址和用户名，保存后双击即可连接", func() { a.newHost("") })
						guide(c, "从 NextShell 1.x 导入连接文件", "在 NextShell 1.x 中导出连接文件，然后在这里导入", func() { a.openSettings("导入导出") })
					})
				} else {
					ui.Column(c).FillWidth().Gap(8).Children(func() {
						muted(c, "最近")
						ui.Grid(c).Columns(3).FillWidth().Gap(8).Children(func() {
							for _, cc := range hosts {
								if card(c, "recent-"+cc.ID, cc.Name, cc.User+"@"+cc.Host).Clicked() {
									a.connect(cc)
								}
							}
							if card(c, "recent-local", "本地终端", "打开本机 Shell").Clicked() {
								a.P.Local()
							}
						})
					})
				}
				ui.Row(c).Gap(8).Children(func() {
					if ui.Button(c, "+ 新建主机").Clicked() {
						a.newHost("")
					}
					if ui.Button(c, "导入…").Clicked() {
						a.openSettings("导入导出")
					}
					if ui.Button(c, "打开本地终端").Clicked() {
						a.P.Local()
					}
				})
				muted(c, shortcutLabel("K")+" 搜索主机、标签和操作").AlignSelf(ui.Center).TextColor(t.TextMuted)
			})
		})
	})
}

func card(c *ui.Context, key, title, sub string) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c.Key(key)).Label(title).Padding(10, 12).Gap(2).Radius(t.Radius).Border(1, t.Border).Background(t.Surface)
	if b.Hovered() {
		b.Background(t.SurfaceHover)
	}
	b.Column().Children(func() {
		ui.Text(c, title).Bold().SingleLine()
		ui.Text(c, sub).FontSize(12).TextColor(t.TextMuted).SingleLine()
	})
	return b
}

func guide(c *ui.Context, title, sub string, run func()) {
	t := c.Theme()
	b := ui.ButtonBase(c.Key(title)).Label(title).FillWidth().Padding(12).Gap(2).Radius(t.Radius).Border(1, t.Border).Background(t.Surface)
	if b.Hovered() {
		b.Background(t.SurfaceHover)
	}
	b.Column().Children(func() {
		ui.Text(c, title).Bold()
		ui.Text(c, sub).FontSize(12).TextColor(t.TextMuted)
	})
	if b.Clicked() {
		run()
	}
}
