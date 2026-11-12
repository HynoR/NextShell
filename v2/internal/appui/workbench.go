package appui

import "github.com/egoist/mygo/ui"

// workbench is the bottom of the centre column: SFTP, 命令库 and 系统信息 for
// the active session. ⌘J folds it to a strip of its tabs.
func (a *App) workbench(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().MinHeight(0).Background(t.Surface).Children(func() {
		a.benchHeader(c, "收起工作区", "chevron-down")
		ui.Column(c).Grow(1).FillWidth().MinHeight(0).Children(func() {
			switch a.layout.bench {
			case benchFiles:
				a.filesPanel(c)
			case benchSnippets:
				a.snippetsPanel(c)
			default:
				a.sysinfoPanel(c)
			}
		})
	})
}

// benchStrip is the folded workbench: a tab opens it on that tab.
func (a *App) benchStrip(c *ui.Context) {
	ui.Column(c).FillWidth().Background(c.Theme().Surface).Children(func() { a.benchHeader(c, "展开工作区", "chevron-up") })
}

func (a *App) benchHeader(c *ui.Context, fold, icon string) {
	t := c.Theme()
	ui.Row(c).FillWidth().Height(34).Padding(0, 6, 0, 8).Gap(2).AlignItems(ui.Stretch).BorderColor(t.Border).BorderWidth(1, 0, 1, 0).Children(func() {
		for i, label := range benchTabs {
			on := i == a.layout.bench && !a.layout.benchHidden
			b := ui.ButtonBase(c).Label(label).Padding(0, 10).Center().BorderColor(t.Accent)
			switch {
			case on:
				b.BorderWidth(0, 0, 2, 0).TextColor(accentInk(t))
			case b.Hovered():
				b.TextColor(t.Text)
			default:
				b.TextColor(t.TextMuted)
			}
			b.Children(func() { ui.Text(c, label).FontSize(13).SingleLine() })
			if b.Clicked() {
				a.showBench(i)
			}
		}
		ui.Spacer(c).Grow(1)
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			if iconButtonTip(c, fold, fold+" "+appShortcutLabel("J"), icon).Clicked() {
				a.layout.benchHidden = !a.layout.benchHidden
			}
		})
	})
}
