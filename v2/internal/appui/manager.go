package appui

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/model"
)

var authNames = map[string]string{"password": "密码", "key": "密钥", "agent": "SSH Agent", "keyboard": "键盘交互"}

// ManagerView builds the connection manager window: the host tree, the chosen
// host, and the host, folder and batch dialogs. A connection it starts opens
// in the main window, which comes forward.
func (a *App) ManagerView(c *ui.Context) {
	a.view = managerWindow
	defer func() { a.view = mainWindow }()
	a.theme(c)
	a.wakeManager = c.Services().Invalidate
	t := c.Theme()
	ui.Row(c).Fill().AlignItems(ui.Stretch).Background(t.Background).Children(func() {
		ui.Column(c).Width(300).FillHeight().BorderColor(t.Border).BorderWidth(0, 1, 0, 0).Children(func() { a.hostTree(c) })
		ui.Scroll(c).Grow(1).MinWidth(0).FillHeight().Children(func() {
			ui.Column(c).FillWidth().Padding(24, 28).Gap(12).Children(func() { a.hostDetail(c) })
		})
	})
	a.hostDialog(c)
	a.folderDialog(c)
	a.batchDialog(c)
	a.confirmView(c, managerWindow)
	a.detailView(c, managerWindow)
	a.runActions(c)
	a.flushToasts(c, managerWindow)
	if a.wakeMain != nil {
		a.wakeMain()
	}
}

// hostDetail shows the host chosen in the tree.
func (a *App) hostDetail(c *ui.Context) {
	t := c.Theme()
	cc, ok := a.host(a.hosts.selected)
	if !ok || cc.Deleted {
		ui.Text(c, "连接管理器").FontSize(20).Bold()
		muted(c, "双击主机即可在主窗口连接；右键主机或目录查看更多操作。")
		ui.Row(c).Gap(8).Children(func() {
			if ui.PrimaryButton(c, "+ 新建主机").Clicked() {
				a.newHost("")
			}
			if ui.Button(c, "新建目录").Clicked() {
				a.editFolder(model.Folder{})
			}
		})
		return
	}
	ui.Text(c, cc.Name).FontSize(20).Bold()
	ui.Text(c, cc.User+"@"+cc.Address()).Font("monospace").TextColor(t.TextMuted).Selectable()
	for _, kv := range [][2]string{
		{"认证", authNames[cc.Auth]},
		{"目录", orDefault(a.folderPath(cc.FolderID), "（根目录）")},
		{"标签", strings.Join(cc.Tags, ", ")},
		{"备注", cc.Notes},
	} {
		if kv[1] == "" {
			continue
		}
		ui.Row(c).FillWidth().Gap(12).Children(func() {
			muted(c, kv[0]).Width(48)
			ui.Text(c, kv[1]).FontSize(13).Grow(1).MinWidth(0)
		})
	}
	ui.Row(c).Gap(8).Padding(8, 0, 0, 0).Children(func() {
		if ui.PrimaryButton(c, "连接").Clicked() {
			a.connect(cc)
		}
		if ui.Button(c, "编辑…").Clicked() {
			a.editHost(cc)
		}
	})
}
