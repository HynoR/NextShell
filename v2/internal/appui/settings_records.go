package appui

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/model"
)

// The recycle bin.

func (a *App) trashSettings(c *ui.Context) {
	t := c.Theme()
	var deleted []model.Connection
	for _, cc := range a.Data.Connections {
		if cc.Deleted {
			deleted = append(deleted, cc)
		}
	}
	group(c, "", func() {
		if len(deleted) == 0 {
			muted(c, "回收站是空的。删除的主机会先放在这里。")
			return
		}
		if a.settings.trash == nil {
			a.settings.trash = map[string]bool{}
		}
		var picked []model.Connection
		for _, cc := range deleted {
			if a.settings.trash[cc.ID] {
				picked = append(picked, cc)
			}
		}
		for _, cc := range deleted {
			ui.Row(c.Key("trash-"+cc.ID)).FillWidth().Padding(8).Gap(8).AlignItems(ui.Center).Radius(t.Radius).Border(1, t.Border).Children(func() {
				on := a.settings.trash[cc.ID]
				if ui.Checkbox(c, &on, "").Label("选择 " + cc.Name).Changed() {
					a.settings.trash[cc.ID] = on
				}
				ui.Column(c).Grow(1).MinWidth(0).Children(func() {
					ui.Text(c, cc.Name).Bold()
					muted(c, cc.User+"@"+cc.Address())
				})
				if ghost(c, "恢复", "").Clicked() {
					cc.Deleted = false
					a.report(a.P.SaveConnection(cc, nil), "已恢复："+cc.Name)
					a.reload()
				}
				if ghost(c, "永久删除", "").TextColor(t.Danger).Clicked() {
					a.confirm("永久删除「"+cc.Name+"」", "主机和保存的凭据都会被删除，无法恢复。", "永久删除", func() {
						a.report(a.P.Delete("connection", cc.ID), "已永久删除")
						a.reload()
					})
				}
			})
		}
		actions(c, func() {
			if ui.Button(c, fmt.Sprintf("恢复所选 %d 个", len(picked))).Disabled(len(picked) == 0).Clicked() {
				for _, cc := range picked {
					cc.Deleted = false
					if e := a.P.SaveConnection(cc, nil); e != nil {
						a.fail(e)
						break
					}
					delete(a.settings.trash, cc.ID)
				}
				a.toast(fmt.Sprintf("已恢复 %d 个主机", len(picked)))
				a.reload()
			}
			a.emptyTrashButton(c, deleted)
		})
	})
}
func (a *App) emptyTrashButton(c *ui.Context, deleted []model.Connection) {
	if ui.Button(c, fmt.Sprintf("清空回收站（%d）", len(deleted))).Clicked() {
		a.confirm("清空回收站", fmt.Sprintf("永久删除 %d 个主机和它们保存的凭据，无法恢复。", len(deleted)), fmt.Sprintf("删除 %d 个主机", len(deleted)), func() {
			for _, cc := range deleted {
				if e := a.P.Delete("connection", cc.ID); e != nil {
					a.fail(e)
					break
				}
			}
			a.reload()
		})
	}
}
