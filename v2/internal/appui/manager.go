package appui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/model"
)

var authNames = map[string]string{"password": "密码", "key": "密钥", "agent": "SSH Agent", "keyboard": "键盘交互"}

// ManagerView builds the connection manager window: hosts in folders, and the keys, proxies and
// credentials they share. A connection it starts opens in the main window, which comes forward.
func (a *App) ManagerView(c *ui.Context) {
	a.view = managerWindow
	defer func() { a.view = mainWindow }()
	a.theme(c)
	a.wakeManager = c.Services().Invalidate
	t := c.Theme()
	h := &a.hosts
	root := ui.Column(c).Fill().Background(t.Background)
	if dropped := root.DroppedFiles(); len(dropped) > 0 {
		a.importDropped(dropped)
	}
	if root.FileDragOver() {
		root.Border(2, t.Accent)
	}
	root.Children(func() {
		ui.Row(c).FillWidth().Height(44).Padding(0, 12).AlignItems(ui.Center).Gap(12).BorderColor(t.Border).BorderWidth(0, 0, 1, 0).Children(func() {
			ui.Segmented(c, &h.page, "连接", "密钥", "代理", "凭据").Label("管理器页面")
		})
		switch h.page {
		case managerKeys:
			a.keysPage(c)
		case managerProxies:
			a.proxiesPage(c)
		case managerCredentials:
			a.credentialsPage(c)
		default:
			a.hostsPage(c)
		}
	})
	a.folderDialog(c)
	a.batchDialog(c)
	a.recordDialogs(c)
	a.confirmView(c, managerWindow)
	a.detailView(c, managerWindow)
	a.runActions(c)
	a.flushToasts(c, managerWindow)
	if a.wakeMain != nil {
		a.wakeMain()
	}
}

// openManagerPage opens the manager on one of its pages.
func (a *App) openManagerPage(page int) {
	a.hosts.page = page
	a.openManager()
}

// importDropped previews the first connection file dropped on the manager.
func (a *App) importDropped(paths []string) {
	for _, p := range paths {
		if strings.EqualFold(filepath.Ext(p), ".json") {
			a.openImport(p)
			return
		}
	}
	a.toast("只能导入 .json 连接文件")
}

// hostsPage is the 1.x manager: the folder tree, the hosts of the chosen folder as tiles, and the
// chosen host's card or form.
func (a *App) hostsPage(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Grow(1).FillWidth().MinHeight(0).AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Width(220).FillHeight().Background(t.Surface).BorderColor(t.Border).BorderWidth(0, 1, 0, 0).Children(func() { a.folderList(c) })
		ui.Column(c).Grow(1).MinWidth(0).FillHeight().Children(func() { a.hostTiles(c) })
		ui.Scroll(c.Key("host-side")).Width(440).FillHeight().BorderColor(t.Border).BorderWidth(0, 0, 0, 1).Children(func() {
			ui.Column(c).FillWidth().Padding(16, 18).Gap(12).Children(func() {
				if a.hosts.editor.open {
					a.hostForm(c)
				} else {
					a.hostCard(c)
				}
			})
		})
	})
}

// folderList is the folder tree: recent hosts, all hosts, then the folders with their host counts.
func (a *App) folderList(c *ui.Context) {
	h := &a.hosts
	t := c.Theme()
	ui.Row(c).FillWidth().Height(36).Padding(0, 6, 0, 12).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "目录").Bold().Grow(1)
		if iconButton(c, "新建目录", "plus").Clicked() {
			a.editFolder(model.Folder{})
		}
	})
	all := 0
	for _, cc := range a.Data.Connections {
		if !cc.Deleted {
			all++
		}
	}
	ui.Sidebar(c.Key("folders"), &h.folder, func() {
		ui.SidebarItem(c, recentFolder, appIcons["refresh"], "最近连接")
		a.hostDropTarget(c, ui.SidebarItem(c, "", appIcons["server"], fmt.Sprintf("全部主机  %d", all)), "")
		var tree func(parent string, depth int)
		tree = func(parent string, depth int) {
			for _, f := range a.Data.Folders {
				if f.ParentID != parent || depth > 30 {
					continue
				}
				item := ui.SidebarItem(c.Key("folder-"+f.ID), f.ID, appIcons["folder"], fmt.Sprintf("%s  %d", f.Name, a.folderCount(f.ID))).PaddingStart(float32(depth) * 14)
				a.folderMenu(item, f)
				a.hostDropTarget(c, item, f.ID)
				tree(f.ID, depth+1)
			}
		}
		tree("", 0)
	}).Label("目录").Grow(1).FillWidth()
	ui.Row(c).FillWidth().Padding(6, 8).BorderColor(t.Border).BorderWidth(1, 0, 0, 0).Children(func() {
		if ghost(c, "本地终端", "terminal").Grow(1).Clicked() {
			a.P.Local()
			a.raiseMain()
		}
	})
}

// shownHosts is what the tiles show: the search results across all folders, else the chosen folder's.
func (a *App) shownHosts() []model.Connection {
	h := &a.hosts
	var out []model.Connection
	switch {
	case strings.TrimSpace(h.filter) != "":
		for _, cc := range a.Data.Connections {
			if !cc.Deleted && a.matches(cc) {
				out = append(out, cc)
			}
		}
	case h.folder == recentFolder:
		for _, id := range h.recent {
			if cc, ok := a.host(id); ok && !cc.Deleted {
				out = append(out, cc)
			}
		}
	case h.folder == "":
		for _, cc := range a.Data.Connections {
			if !cc.Deleted {
				out = append(out, cc)
			}
		}
	default:
		out = a.folderHosts(h.folder)
	}
	return out
}

func (a *App) hostTiles(c *ui.Context) {
	h := &a.hosts
	t := c.Theme()
	hosts := a.shownHosts()
	ui.Row(c).FillWidth().Padding(8, 12).Gap(8).AlignItems(ui.Center).BorderColor(t.Border).BorderWidth(0, 0, 1, 0).Children(func() {
		if ui.PrimaryButton(c, "+ 新建连接").Clicked() {
			folder := h.folder
			if folder == recentFolder {
				folder = ""
			}
			a.newHost(folder)
		}
		ui.SearchField(c, &h.filter).Label("搜索主机").Placeholder("名称 / 地址 / 用户 / 标签").Grow(1).MinWidth(120)
		if ui.Button(c, "导入…").Clicked() {
			a.openImport("")
		}
		if ui.Button(c, "导出…").Clicked() {
			a.openExport(nil)
		}
	})
	var missing []string
	for _, cc := range a.Data.Connections {
		if !cc.Deleted && a.missingKey(cc) {
			missing = append(missing, cc.ID)
		}
	}
	if len(missing) > 0 {
		ui.Row(c).FillWidth().Padding(6, 12).Gap(8).AlignItems(ui.Center).Background(t.Danger.Alpha(.1)).Children(func() {
			ui.Text(c, fmt.Sprintf("%d 个主机使用密钥认证，但还没有可用的密钥", len(missing))).FontSize(12).TextColor(t.Danger).Grow(1)
			if ui.Button(c, fmt.Sprintf("为 %d 个主机选择密钥", len(missing))).Clicked() {
				a.openBatch("缺少密钥的主机", missing, "key")
			}
		})
	}
	var picked []model.Connection
	for _, cc := range hosts {
		if h.picked.Has(cc.ID) {
			picked = append(picked, cc)
		}
	}
	if len(picked) >= 2 {
		ui.Row(c).FillWidth().Padding(6, 12).Gap(8).AlignItems(ui.Center).Background(t.Accent.Alpha(.12)).Children(func() {
			ui.Text(c, fmt.Sprintf("已选 %d", len(picked))).Bold().Grow(1)
			if ui.Button(c, "绑定认证…").Clicked() {
				a.openBatch("所选", hostIDs(picked), "")
			}
			if ui.Button(c, "导出所选…").Clicked() {
				a.openExport(hostIDs(picked))
			}
			if ui.Button(c, "移入回收站").Clicked() {
				a.trashHosts(picked)
			}
			if ghost(c, "取消选择", "").Clicked() {
				h.picked.Clear()
			}
		})
	}
	if len(hosts) == 0 {
		ui.Column(c).Grow(1).FillWidth().Children(func() {
			if strings.TrimSpace(h.filter) != "" {
				emptyState(c, "没有匹配的主机", "换个关键字试试。")
			} else {
				emptyState(c, "这里还没有主机", "新建一个，或把导出的 .json 连接文件拖到这个窗口。")
			}
		})
		return
	}
	h.grid.Key = func(i int) any { return hosts[i].ID }
	h.grid.Label = func(i int) string { return hosts[i].Name }
	h.grid.Selected = &h.chosen
	h.grid.Selection = &h.picked
	if h.chosen >= len(hosts) {
		h.chosen = -1
	}
	grid := ui.GridView(c.Key("tiles"), &h.grid, len(hosts), 190, 60, func(i int) { a.hostTile(c, hosts[i]) }).Gap(8).Padding(12).Grow(1).FillWidth()
	if grid.Changed() && h.chosen >= 0 && h.chosen < len(hosts) {
		if id := hosts[h.chosen].ID; id != h.selected {
			h.selected = id
			h.editor.open = false
		}
	}
	if grid.Submitted() && h.chosen >= 0 && h.chosen < len(hosts) {
		a.connect(hosts[h.chosen])
	}
}

// hostTile is a host in the grid: its name and login, its status, and a red mark when it logs in
// with a key it does not have.
func (a *App) hostTile(c *ui.Context, cc model.Connection) {
	t := c.Theme()
	tile := ui.Row(c).Grow(1).FillWidth().Padding(8, 10).Gap(8).AlignItems(ui.Center).Radius(t.Radius).Tooltip(a.login(cc))
	if !a.hosts.picked.Has(cc.ID) { // a chosen tile shows the grid's fill
		tile.Background(t.Surface).Border(1, t.Border)
	}
	a.hostMenu(tile, cc)
	tile.Children(func() {
		ui.Box(c).Size(3, 30).Radius(1.5).Background(hostColor(cc))
		// The icon is the handle that drags the host onto a folder: the rest of the tile takes clicks.
		ui.Icon(c, appIcons["server"]).Label("拖动 " + cc.Name).Tooltip("拖到左侧目录即可移动").FontSize(18).TextColor(t.TextMuted).Drag(hostDrag(cc.ID))
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
				if cc.Favorite {
					ui.Text(c, "★").FontSize(11).TextColor(t.Warning)
				}
				ui.Text(c, cc.Name).Bold().SingleLine().MinWidth(0)
			})
			ui.Text(c, a.login(cc)).Font("monospace").FontSize(11).TextColor(t.TextMuted).SingleLine()
		})
		if a.missingKey(cc) {
			ui.Text(c, "!").Label("缺少密钥").Bold().FontSize(11).TextColor(ui.RGB(255, 255, 255)).Background(t.Danger).Padding(0, 6).Radius(8).Tooltip("使用密钥认证，但还没有可用的密钥")
		}
		if color, ok := a.hostStatus(c, cc.ID); ok {
			dot(c, color)
		}
	})
}

// hostCard shows the host chosen among the tiles.
func (a *App) hostCard(c *ui.Context) {
	t := c.Theme()
	cc, ok := a.host(a.hosts.selected)
	if !ok || cc.Deleted {
		ui.Text(c, "连接管理器").FontSize(20).Bold()
		muted(c, "单击主机查看详情，双击连接；按住 ⌘ 或 Shift 可选择多个主机批量操作。右键目录可批量绑定认证。")
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
	r := a.resolved(cc)
	ui.Text(c, cc.Name).FontSize(20).Bold()
	ui.Text(c, a.login(cc)).Font("monospace").TextColor(t.TextMuted).Selectable()
	login := authLabel(a, r.Auth, r.KeyID)
	if cr, ok := a.credential(cc.CredentialID); ok {
		login = "凭据「" + cr.Name + "」· " + login
	}
	for _, kv := range [][2]string{
		{"认证", login},
		{"代理", a.proxyName(cc.ProxyID)},
		{"目录", orDefault(a.folderPath(cc.FolderID), "（根目录）")},
		{"标签", strings.Join(cc.Tags, ", ")},
		{"备注", cc.Notes},
	} {
		if kv[1] == "" {
			continue
		}
		ui.Row(c).FillWidth().Gap(12).Children(func() {
			muted(c, kv[0]).Width(48)
			text := ui.Text(c, kv[1]).FontSize(13).Grow(1).MinWidth(0)
			if kv[0] == "认证" && a.missingKey(cc) {
				text.TextColor(t.Danger)
			}
		})
	}
	ui.Row(c).Gap(8).Padding(8, 0, 0, 0).Children(func() {
		if ui.PrimaryButton(c, "连接").Clicked() {
			a.connect(cc)
		}
		if ui.Button(c, "编辑…").Clicked() {
			a.editHost(cc)
		}
		if a.missingKey(cc) && ui.Button(c, "选择密钥…").Clicked() {
			a.openBatch("「"+cc.Name+"」", []string{cc.ID}, "key")
		}
	})
}
