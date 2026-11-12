package appui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/model"
)

// Keys, proxies and the recycle bin: the records hosts refer to.

type keysState struct {
	name, algorithm, path, passphrase, pem string
	names                                  map[string]string // inline rename buffers
}

type proxiesState struct {
	open, replaceSecret bool
	proxy               model.Proxy
	port, password, err string
}

func (a *App) keyUsers(id string) int {
	n := 0
	for _, cc := range a.Data.Connections {
		if cc.KeyID == id && !cc.Deleted {
			n++
		}
	}
	return n
}

func (a *App) keySettings(c *ui.Context) {
	s := &a.settings.keys
	t := c.Theme()
	if s.names == nil {
		s.names = map[string]string{}
	}
	group(c, "已保存的密钥", func() {
		if len(a.Data.Keys) == 0 {
			muted(c, "还没有密钥。生成一个，或导入已有的私钥。")
		}
		for _, k := range a.Data.Keys {
			ui.Row(c.Key("key-"+k.ID)).FillWidth().Padding(8).Gap(8).AlignItems(ui.Center).Radius(t.Radius).Border(1, t.Border).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
					name, ok := s.names[k.ID]
					if !ok {
						name = k.Name
					}
					if ui.EditableText(c, &name).Label("密钥名称 " + k.Name).Changed() {
						a.report(a.P.RenameKey(k.ID, strings.TrimSpace(name)), "已重命名密钥")
						a.reload()
						delete(s.names, k.ID)
					} else {
						s.names[k.ID] = name
					}
					muted(c, fmt.Sprintf("%s · %d 个主机使用", keyKind(k), a.keyUsers(k.ID))).SingleLine()
				})
				if ghost(c, "复制公钥", "").Clicked() {
					a.P.CopyText(k.Public)
					a.toast("已复制公钥")
				}
				if ghost(c, "删除", "").TextColor(t.Danger).Clicked() {
					msg := "删除后无法恢复。"
					if n := a.keyUsers(k.ID); n > 0 {
						msg = fmt.Sprintf("有 %d 个主机使用这个密钥，删除后它们将无法用密钥登录。", n)
					}
					a.confirm("删除密钥「"+k.Name+"」", msg, "删除密钥", func() {
						a.report(a.P.Delete("key", k.ID), "已删除密钥")
						a.reload()
					})
				}
			})
		}
	})
	group(c, "生成新密钥", func() {
		field(c, "名称", &s.name)
		choice(c, "算法", &s.algorithm, []string{"ed25519", "rsa-2048", "rsa-4096"})
		actions(c, func() {
			if ui.Button(c, "生成密钥").Clicked() {
				_, e := a.P.GenerateKey(strings.TrimSpace(s.name), s.algorithm)
				a.report(e, "已生成密钥")
				if e == nil {
					s.name = ""
				}
				a.reload()
			}
		})
	})
	group(c, "导入私钥", func() {
		a.localPath(c, "私钥文件", "未选择", &s.path, FilePick{DefaultPath: "~/.ssh", Hidden: true})
		password(c, "口令", &s.passphrase).Description("私钥没有口令时留空")
		ui.Field(c, "或粘贴私钥", func() { ui.TextArea(c, &s.pem).Label("粘贴私钥").Font("monospace").Height(90).Grow(1) }).FillWidth()
		actions(c, func() { a.importKeyButton(c) })
	})
}
func (a *App) importKeyButton(c *ui.Context) {
	s := &a.settings.keys
	if ui.Button(c, "导入密钥").Clicked() {
		var e error
		if strings.TrimSpace(s.pem) != "" {
			name := strings.TrimSpace(s.name)
			if name == "" {
				name = "导入的密钥"
			}
			e = a.P.SaveKey(model.Key{ID: a.P.NewID(), Name: name, Private: s.pem, Passphrase: s.passphrase})
		} else {
			_, e = a.P.ImportKey(strings.TrimSpace(s.path), s.passphrase)
		}
		a.report(e, "已导入密钥")
		if e == nil {
			s.path, s.passphrase, s.pem = "", "", ""
		}
		a.reload()
	}
}
func keyKind(k model.Key) string {
	kind, _, _ := strings.Cut(k.Public, " ")
	kind = strings.TrimPrefix(kind, "ssh-")
	if kind == "" {
		kind = "私钥"
	}
	return kind
}

func (a *App) proxySettings(c *ui.Context) {
	s := &a.settings.proxies
	t := c.Theme()
	group(c, "已保存的代理", func() {
		if len(a.Data.Proxies) == 0 {
			muted(c, "还没有代理。主机可以通过 SOCKS 代理连接。")
		}
		for _, p := range a.Data.Proxies {
			ui.Row(c.Key("proxy-"+p.ID)).FillWidth().Padding(8).Gap(8).AlignItems(ui.Center).Radius(t.Radius).Border(1, t.Border).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Children(func() {
					ui.Text(c, p.Name).Bold()
					muted(c, fmt.Sprintf("%s · %s:%d", strings.ToUpper(p.Kind), p.Host, p.Port))
				})
				if ghost(c, "编辑", "").Clicked() {
					*s = proxiesState{open: true, proxy: p, port: strconv.Itoa(p.Port)}
				}
				if ghost(c, "删除", "").TextColor(t.Danger).Clicked() {
					a.confirm("删除代理「"+p.Name+"」", "使用它的主机将改为直接连接。", "删除代理", func() {
						a.report(a.P.Delete("proxy", p.ID), "已删除代理")
						a.reload()
					})
				}
			})
		}
		actions(c, func() {
			if ui.Button(c, "新建代理").Clicked() {
				*s = proxiesState{open: true, proxy: model.Proxy{Kind: "socks5"}, port: "1080"}
			}
		})
	})
	if !s.open {
		return
	}
	title := "新建代理"
	if s.proxy.ID != "" {
		title = "编辑代理"
	}
	group(c, title, func() {
		field(c, "名称", &s.proxy.Name)
		choice(c, "类型", &s.proxy.Kind, []string{"socks5", "socks4"})
		field(c, "主机", &s.proxy.Host)
		field(c, "端口", &s.port)
		field(c, "用户名", &s.proxy.User)
		if s.proxy.ID != "" && !s.replaceSecret {
			ui.Field(c, "密码", func() {
				if ui.Button(c, "修改已保存的密码").Clicked() {
					s.replaceSecret = true
				}
			}).FillWidth()
		} else {
			password(c, "密码", &s.password)
		}
		if s.err != "" {
			ui.Text(c, s.err).TextColor(t.Danger)
		}
		actions(c, func() {
			if ui.Button(c, "取消").Clicked() {
				s.open = false
			}
			if ui.PrimaryButton(c, "保存代理").Clicked() {
				a.saveProxy()
			}
		})
	})
}
func (a *App) saveProxy() {
	s := &a.settings.proxies
	p := s.proxy
	port, e := strconv.Atoi(strings.TrimSpace(s.port))
	if e != nil {
		s.err = "端口须为数字"
		return
	}
	p.Port = port
	var secret *string
	if p.ID == "" || s.replaceSecret {
		secret = &s.password
	}
	if p.ID == "" {
		p.ID = a.P.NewID()
	}
	if e := a.P.SaveProxy(p, secret); e != nil {
		s.err = e.Error()
		return
	}
	*s = proxiesState{}
	a.reload()
	a.toast("已保存代理")
}

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
