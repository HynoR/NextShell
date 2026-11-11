package appui

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/model"
	"nextshell.dev/v2/internal/sftpfs"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

func (a *App) later(fn func()) { a.Actions = append(a.Actions, fn) }
func (a *App) connectionTargets(cc model.Connection) []model.Connection {
	if !a.Selected[cc.ID] {
		return []model.Connection{cc}
	}
	var out []model.Connection
	for _, v := range a.Data.Connections {
		if a.Selected[v.ID] && !v.Deleted {
			out = append(out, v)
		}
	}
	return out
}
func (a *App) connectionMenu(item ui.Element, cc model.Connection) {
	item.ContextMenu(func(m *ui.Menu) {
		targets := a.connectionTargets(cc)
		if len(targets) == 1 {
			if m.Item("连接").Chosen() {
				a.later(func() { a.P.Connect(cc) })
			}
			if m.Item("编辑连接").Chosen() {
				a.later(func() { a.edit(cc) })
			}
			if m.Item("重命名连接").Chosen() {
				a.RenameConnection = cc
				a.RenameText = cc.Name
				a.RenameOpen = true
			}
			if m.Item("复制地址").Chosen() {
				a.P.CopyText(cc.User + "@" + cc.Address())
			}
			if m.Item("复制连接配置").Chosen() {
				a.later(func() {
					v, e := a.P.DuplicateConnection(cc.ID)
					a.status(e)
					if e == nil {
						a.reload()
						a.edit(v)
					}
				})
			}
		} else {
			if m.Item("批量绑定认证").Chosen() {
				a.later(func() {
					a.Page = "connections"
					a.Message = "修改认证后点批量绑定认证；保留当前选区"
				})
			}
		}
		m.Separator()
		if m.Item("导出连接文件…").Chosen() {
			a.later(func() {
				a.Selected = map[string]bool{}
				for _, v := range targets {
					a.Selected[v.ID] = true
				}
				a.Page = "data"
			})
		}
		if m.Item("复制到云工作区…").Chosen() {
			a.later(func() {
				a.Selected = map[string]bool{}
				for _, v := range targets {
					a.Selected[v.ID] = true
				}
				a.Page = "cloud"
			})
		}
		if m.Item("移入回收站").Chosen() {
			a.TrashTargets = targets
			a.TrashConfirm = true
		}
	})
}
func (a *App) parityDialogs(c *ui.Context) {
	ui.Modal(c, &a.RenameOpen, func() {
		ui.Column(c).Width(430).Padding(PanelInset).Gap(FormGap).Children(func() {
			ui.Text(c, "重命名连接").FontSize(20).Bold()
			ui.TextInput(c, &a.RenameText).Label("连接新名称").AutoFocus()
			ui.Row(c).Gap(8).Children(func() {
				if ui.PrimaryButton(c, "确认重命名").Clicked() {
					v := a.RenameConnection
					v.Name = strings.TrimSpace(a.RenameText)
					if v.Name == "" {
						a.Message = "名称不能为空"
					} else {
						e := a.P.SaveConnection(v, nil)
						a.status(e)
						if e == nil {
							a.RenameOpen = false
							a.reload()
							if a.Edit.ID == v.ID {
								a.Edit.Name = v.Name
							}
						}
					}
				}
				if ui.Button(c, "取消重命名").Clicked() {
					a.RenameOpen = false
				}
			})
		})
	})
	if a.TrashConfirm {
		switch ui.AlertDialog(c, &a.TrashConfirm, "移入回收站", fmt.Sprintf("将 %d 个连接移入回收站？可从回收站恢复。", len(a.TrashTargets)), "取消", "移入回收站") {
		case 1:
			for _, v := range a.TrashTargets {
				v.Deleted = true
				if e := a.P.SaveConnection(v, nil); e != nil {
					a.status(e)
					break
				}
			}
			a.reload()
			a.TrashTargets = nil
		}
	}
}
func (a *App) tabMenu(item ui.Element, s *Session, i int) {
	item.ContextMenu(func(m *ui.Menu) {
		if m.Item("打开连接管理").Chosen() {
			a.later(func() { a.Page = "connections" })
		}
		if m.Item("重命名标签").Chosen() {
			a.later(func() {
				a.Active = i
				a.TabName = s.Title
				a.Page = "terminal"
				a.Message = "在顶部标签名称框输入后点重命名"
			})
		}
		if m.Item("恢复自动标题").Chosen() {
			a.later(func() {
				s.ManualTitle = false
				s.Title = s.Connection.Name
				if s.Local {
					s.Title = "本地终端"
				}
				a.refreshTitles()
				if a.current() == s {
					a.TabName = s.Title
				}
			})
		}
		if m.Item("复制会话").Chosen() {
			a.later(func() { a.P.CloneSession(s) })
		}
		if m.Item("重连").Chosen() {
			a.later(func() { a.P.Reconnect(s) })
		}
		m.Separator()
		if m.Item("清屏").Chosen() {
			s.Terminal.ClearScreen()
		}
		if m.Item("清除滚动缓冲").Chosen() {
			s.Terminal.ClearScrollback()
		}
		if m.Item("关闭标签").Chosen() {
			a.later(func() { a.closeTab(i) })
		}
	})
}

// Shortcuts belong only to the terminal element: never steal Ctrl+W, input/composition keys.
func (a *App) terminalShortcuts(e ui.Element) {
	mod := ui.Ctrl
	if runtime.GOOS == "darwin" {
		mod = ui.Super
	}
	for n, k := range []ui.Key{ui.Key1, ui.Key2, ui.Key3, ui.Key4, ui.Key5, ui.Key6, ui.Key7, ui.Key8, ui.Key9} {
		if e.Shortcut(mod, k) {
			index := n
			if n == 8 {
				index = len(a.Tabs) - 1
			}
			a.later(func() { a.activateTab(index) })
		}
	}
	prev := e.Shortcut(ui.Ctrl, ui.KeyPageUp) || e.Shortcut(mod|ui.Shift, ui.KeyBracketLeft)
	next := e.Shortcut(ui.Ctrl, ui.KeyPageDown) || e.Shortcut(mod|ui.Shift, ui.KeyBracketRight)
	if prev {
		a.later(func() { a.activateTab((a.Active - 1 + len(a.Tabs)) % len(a.Tabs)) })
	}
	if next {
		a.later(func() { a.activateTab((a.Active + 1) % len(a.Tabs)) })
	}
	closemod := mod | ui.Shift
	if runtime.GOOS == "darwin" {
		closemod = mod
	}
	if e.Shortcut(closemod, ui.KeyW) {
		a.later(func() { a.closeTab(a.Active) })
	}
	if e.Shortcut(mod|ui.Shift, ui.KeyD) {
		a.later(func() {
			if s := a.current(); s != nil {
				a.P.CloneSession(s)
			}
		})
	}
}
func (a *App) activateTab(i int) {
	if i >= 0 && i < len(a.Tabs) {
		a.Active = i
		a.TabName = a.Tabs[i].Title
		a.Page = "terminal"
	}
}
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
func (a *App) fileMenu(item ui.Element, id string, f sftpfs.Entry) {
	item.ContextMenu(func(m *ui.Menu) {
		s := &a.Phase2
		if f.Directory {
			if m.Item("打开目录").Chosen() {
				s.SelectedRemote = f.Path
				s.RemotePath = f.Path
				a.P.ListFiles(id, f.Path)
			}
		} else if !f.Symlink {
			if m.Item("编辑文件").Chosen() {
				a.later(func() { a.openEditor(id, f.Path) })
			}
		}
		if !f.Symlink {
			if m.Item("下载到默认位置").Chosen() {
				kind := "download"
				if f.Directory {
					kind = "download-packed"
				}
				a.later(func() {
					a.queue(TransferRequest{Session: id, Kind: kind, Remote: f.Path, Local: filepath.Join(a.Prefs.DownloadDir, f.Name), Overwrite: s.Overwrite})
				})
			}
		}
		if m.Item("复制路径").Chosen() {
			a.P.CopyText(f.Path)
		}
		if m.Item("重命名远端路径").Chosen() {
			s.SelectedRemote = f.Path
			s.Action = "rename"
			s.ActionFrom = f.Path
			s.ActionTo = f.Path
		}
		if m.Item("删除远端路径").Chosen() {
			s.SelectedRemote = f.Path
			s.Action = "remove"
			s.ActionFrom = f.Path
		}
	})
}
func (a *App) filesBlankMenu(item ui.Element, id string) {
	item.ContextMenu(func(m *ui.Menu) {
		s := &a.Phase2
		if m.Item("刷新目录").Chosen() {
			a.P.ListFiles(id, s.RemotePath)
		}
		if m.Item("在终端打开此目录").Chosen() {
			if e := safeTerminalPath(s.RemotePath); e != nil {
				a.status(e)
			} else {
				a.status(a.P.Send(id, "cd -- "+shellQuote(s.RemotePath)+"\r"))
				a.Page = "terminal"
			}
		}
		if m.Item("上传文件（填写路径）").Chosen() {
			a.Message = "填写本地文件和远端文件路径后点上传文件"
		}
		if m.Item("新建远端目录").Chosen() {
			s.Action = "mkdir"
			s.ActionFrom = path.Join(s.RemotePath, "新建目录")
		}
		if m.Item("新建远端文件").Chosen() {
			s.Action = "create"
			s.ActionFrom = path.Join(s.RemotePath, "新建文件.txt")
		}
	})
}

func safeTerminalPath(s string) error {
	for _, r := range s {
		if r < 32 || r == 127 {
			return fmt.Errorf("路径包含控制字符，不能发送到交互终端")
		}
	}
	return nil
}
func (a *App) folderMenu(e ui.Element, f model.Folder) {
	e.ContextMenu(func(m *ui.Menu) {
		if m.Item("新建子目录").Chosen() {
			a.later(func() { a.Folder = model.Folder{ParentID: f.ID}; a.Page = "folders" })
		}
		if m.Item("重命名/编辑目录").Chosen() {
			a.later(func() { a.Folder = f; a.Page = "folders" })
		}
		if m.Item("批量绑定目录认证（含子目录）").Chosen() {
			a.later(func() {
				a.selectFolderConnections(f.ID)
				a.Page = "connections"
				a.Message = "已选择目录及子目录中的连接；修改认证后点批量绑定认证"
			})
		}
		if m.Item("删除空目录").Chosen() {
			a.DeleteKind = "folder"
			a.DeleteID = f.ID
		}
	})
}
func (a *App) selectFolderConnections(root string) {
	folders := map[string]bool{root: true}
	for i := 0; i < len(a.Data.Folders); i++ {
		changed := false
		for _, f := range a.Data.Folders {
			if folders[f.ParentID] && !folders[f.ID] {
				folders[f.ID] = true
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	a.Selected = map[string]bool{}
	for _, cc := range a.Data.Connections {
		if folders[cc.FolderID] && !cc.Deleted {
			a.Selected[cc.ID] = true
		}
	}
}
