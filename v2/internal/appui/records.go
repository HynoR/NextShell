package appui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/model"
)

// The connection manager's keys, proxies and credentials pages, the dialogs that create them (also
// from the host form, which then uses what they made), and import / export.

type recordsState struct {
	keyList, proxyList, credList ui.ListState
	keyRow, proxyRow, credRow    int
	rename                       map[string]string // key names being edited

	key   keyDialog
	proxy proxyDialog
	cred  credentialDialog
	data  dataState
}

// keyDialog generates or imports a key, or replaces the private key of one (replace).
type keyDialog struct {
	open, importing                        bool
	replace                                string
	name, algorithm, path, passphrase, pem string
	err                                    string
	target                                 *string // the field that takes the new key
}

type proxyDialog struct {
	open, replaceSecret bool
	proxy               model.Proxy
	port, password, err string
	target              *string
}

type credentialDialog struct {
	open, replaceSecret bool
	cred                model.Credential
	password, err       string
	target              *string
}

type dataState struct {
	importOpen, exportOpen                 bool
	path, password, format, policy, folder string
	preview                                *ImportPreview
	exportIDs                              []string
	encrypt                                bool
	exportPath, exportPassword             string
}

func (r *recordsState) init() {
	r.keyRow, r.proxyRow, r.credRow = -1, -1, -1
	r.rename = map[string]string{}
	r.key.algorithm = "ed25519"
	r.data.format = "nextshell"
	r.data.policy = "skip"
	r.data.encrypt = true
}

func (a *App) recordDialogs(c *ui.Context) {
	a.importDialog(c)
	a.exportDialog(c)
	a.credentialDialog(c)
	a.proxyDialog(c)
	a.keyDialog(c) // last: the credential dialog opens it on top of itself
}

// References.

func (a *App) keyUsers(id string) (hosts []string, creds []string) {
	for _, cc := range a.Data.Connections {
		if cc.KeyID == id && !cc.Deleted {
			hosts = append(hosts, cc.Name)
		}
	}
	for _, cr := range a.Data.Credentials {
		if cr.KeyID == id {
			creds = append(creds, cr.Name)
		}
	}
	return hosts, creds
}
func (a *App) hostsUsing(match func(model.Connection) bool) []string {
	var out []string
	for _, cc := range a.Data.Connections {
		if !cc.Deleted && match(cc) {
			out = append(out, cc.Name)
		}
	}
	return out
}

// usedBy lists what refers to a record, under the detail of its page.
func usedBy(c *ui.Context, hosts, creds []string) {
	ui.Text(c, fmt.Sprintf("被引用（%d）", len(hosts)+len(creds))).Bold().FontSize(13).Margin(8, 0, 0, 0)
	if len(hosts)+len(creds) == 0 {
		muted(c, "没有主机使用")
	}
	for _, n := range hosts {
		ui.Text(c.Key("used-"+n), "主机 · "+n).FontSize(12)
	}
	for _, n := range creds {
		ui.Text(c.Key("used-cred-"+n), "凭据 · "+n).FontSize(12)
	}
}

// faint is a table cell's secondary text, readable on the chosen row too.
func faint(c *ui.Context, s string, chosen bool) ui.Element {
	return ui.Text(c, s).FontSize(12).TextColor(mutedOnRow(c.Theme(), chosen)).SingleLine()
}

// recordPage lays out a page: a toolbar and a table, and the chosen row's detail on the right.
func recordPage(c *ui.Context, toolbar, table, detail func()) {
	t := c.Theme()
	ui.Row(c).Grow(1).FillWidth().MinHeight(0).AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).FillHeight().Children(func() {
			ui.Row(c).FillWidth().Padding(8, 12).Gap(8).AlignItems(ui.Center).BorderColor(t.Border).BorderWidth(0, 0, 1, 0).Children(toolbar)
			table()
		})
		ui.Scroll(c).Width(360).FillHeight().BorderColor(t.Border).BorderWidth(0, 0, 0, 1).Children(func() {
			ui.Column(c).FillWidth().Padding(16, 18).Gap(8).Children(detail)
		})
	})
}

// Keys.

func (a *App) keysPage(c *ui.Context) {
	r := &a.hosts.records
	recordPage(c, func() {
		if ui.PrimaryButton(c, "生成密钥…").Clicked() {
			a.openKeyDialog(nil)
		}
		if ui.Button(c, "导入私钥…").Clicked() {
			a.openKeyDialog(nil)
			r.key.importing = true
		}
	}, func() {
		if len(a.Data.Keys) == 0 {
			emptyState(c, "还没有密钥", "生成一个，或导入已有的私钥。同一把私钥只保存一次，所有主机共用。")
			return
		}
		r.keyList.Selected = &r.keyRow
		r.keyList.Key = func(i int) any { return a.Data.Keys[i].ID }
		r.keyList.Label = func(i int) string { return a.Data.Keys[i].Name }
		cols := []ui.TableColumn{{Title: "名称"}, {Title: "类型", Width: 90}, {Title: "指纹"}, {Title: "被引用", Width: 64, Align: ui.End}}
		ui.Table(c.Key("keys"), &r.keyList, cols, len(a.Data.Keys), func(row, col int) {
			k := a.Data.Keys[row]
			hosts, creds := a.keyUsers(k.ID)
			switch col {
			case 0:
				ui.Text(c, k.Name).SingleLine()
			case 1:
				faint(c, keyKind(k), row == r.keyRow)
			case 2:
				faint(c, k.Fingerprint, row == r.keyRow).Font("monospace").FontSize(11)
			default:
				faint(c, strconv.Itoa(len(hosts)+len(creds)), row == r.keyRow)
			}
		}).Grow(1).FillWidth()
	}, func() {
		if r.keyRow < 0 || r.keyRow >= len(a.Data.Keys) {
			muted(c, "选择一把密钥查看公钥和使用它的主机。")
			return
		}
		k := a.Data.Keys[r.keyRow]
		name, ok := r.rename[k.ID]
		if !ok {
			name = k.Name
		}
		if ui.EditableText(c, &name).Label("密钥名称 " + k.Name).FontSize(18).Bold().Changed() {
			a.report(a.P.RenameKey(k.ID, strings.TrimSpace(name)), "已重命名密钥")
			a.reload()
			delete(r.rename, k.ID)
		} else {
			r.rename[k.ID] = name
		}
		muted(c, keyKind(k)+" · "+k.Fingerprint).Selectable()
		ui.Text(c, k.Public).Font("monospace").FontSize(11).Wrap().Selectable()
		hosts, creds := a.keyUsers(k.ID)
		ui.Row(c).Gap(8).Children(func() {
			if ui.Button(c, "复制公钥").Clicked() {
				a.P.CopyText(k.Public)
				a.toast("已复制公钥")
			}
			if ui.Button(c, "替换私钥…").Clicked() {
				a.openKeyDialog(nil)
				r.key.importing, r.key.replace, r.key.name = true, k.ID, k.Name
			}
			del := ui.Button(c, "删除").Disabled(len(hosts)+len(creds) > 0)
			if len(hosts)+len(creds) > 0 {
				del.Tooltip("有主机或凭据在使用，不能删除")
			}
			if del.Clicked() {
				a.confirm("删除密钥「"+k.Name+"」", "删除后无法恢复。", "删除密钥", func() {
					a.report(a.P.Delete("key", k.ID), "已删除密钥")
					a.reload()
				})
			}
		})
		usedBy(c, hosts, creds)
	})
}
func keyKind(k model.Key) string {
	kind, _, _ := strings.Cut(k.Public, " ")
	kind = strings.TrimPrefix(kind, "ssh-")
	if kind == "" {
		kind = "私钥"
	}
	return kind
}

// openKeyDialog opens the key dialog; the key it makes goes into *target.
func (a *App) openKeyDialog(target *string) {
	a.openManager()
	d := &a.hosts.records.key
	*d = keyDialog{open: true, algorithm: orDefault(d.algorithm, "ed25519"), target: target}
}

func (a *App) keyDialog(c *ui.Context) {
	d := &a.hosts.records.key
	if !d.open {
		return
	}
	enter := false // Enter in a field, or ⌘S, saves
	dialog(c, &d.open, func() {
		ui.Column(c).Width(500).Gap(14).Children(func() {
			title := "新建密钥"
			if d.replace != "" {
				title = "替换「" + d.name + "」的私钥"
			}
			ui.Text(c, title).FontSize(17).Bold()
			if d.replace == "" {
				mode := 0
				if d.importing {
					mode = 1
				}
				if ui.Segmented(c, &mode, "生成", "导入私钥").Label("方式").Changed() {
					d.importing = mode == 1
				}
			} else {
				muted(c, "使用这把密钥的主机和凭据会改用新的私钥。")
			}
			ui.Form(c, func() {
				if d.replace == "" {
					field(c, "名称", &d.name, &enter)
				}
				if !d.importing {
					choice(c, "算法", &d.algorithm, []string{"ed25519", "rsa-2048", "rsa-4096"})
					return
				}
				a.localPath(c, "私钥文件", "未选择", &d.path, FilePick{DefaultPath: "~/.ssh", Hidden: true})
				ui.Field(c, "或粘贴私钥", func() { ui.TextArea(c, &d.pem).Label("粘贴私钥").Font("monospace").Height(90).Grow(1) }).FillWidth()
				password(c, "口令", &d.passphrase, &enter).Description("私钥没有口令时留空")
			}).FillWidth().Gap(FormGap)
			if d.err != "" {
				ui.Text(c, d.err).TextColor(c.Theme().Danger)
			}
			ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "取消").Clicked() {
					d.open = false
				}
				label := "生成密钥"
				if d.importing {
					label = "导入密钥"
				}
				if d.replace != "" {
					label = "替换私钥"
				}
				if ui.PrimaryButton(c, label).Clicked() || enter || c.Shortcut(ui.Cmd, ui.KeyS) {
					a.saveKeyDialog()
				}
			})
		})
	})
}
func (a *App) saveKeyDialog() {
	d := &a.hosts.records.key
	name := strings.TrimSpace(d.name)
	var k model.Key
	var e error
	switch {
	case d.replace != "":
		e = a.P.ReplaceKey(d.replace, strings.TrimSpace(d.path), d.pem, d.passphrase)
		k.ID = d.replace
	case !d.importing:
		k, e = a.P.GenerateKey(name, d.algorithm)
	case strings.TrimSpace(d.pem) != "":
		k = model.Key{ID: a.P.NewID(), Name: orDefault(name, "导入的密钥"), Private: d.pem, Passphrase: d.passphrase}
		e = a.P.SaveKey(k)
	default:
		k, e = a.P.ImportKey(strings.TrimSpace(d.path), d.passphrase)
	}
	var dup *model.KeyExistsError
	if errors.As(e, &dup) {
		k, e = dup.Key, nil
		if d.target != nil {
			a.toast("密钥已存在，已选用：" + dup.Key.Name)
		} else {
			a.toast("密钥已存在：" + dup.Key.Name)
		}
	} else if e == nil {
		a.toast("已保存密钥")
	}
	if e != nil {
		d.err = e.Error()
		return
	}
	a.reload()
	if d.target != nil {
		*d.target = k.ID
	} else {
		for i, v := range a.Data.Keys {
			if v.ID == k.ID {
				a.hosts.records.keyRow = i
			}
		}
	}
	*d = keyDialog{algorithm: d.algorithm}
}

// Proxies.

func (a *App) proxiesPage(c *ui.Context) {
	r := &a.hosts.records
	recordPage(c, func() {
		if ui.PrimaryButton(c, "新建代理…").Clicked() {
			a.openProxyDialog(model.Proxy{Kind: "socks5"}, nil)
		}
	}, func() {
		if len(a.Data.Proxies) == 0 {
			emptyState(c, "还没有代理", "主机可以通过 SOCKS 代理连接。")
			return
		}
		r.proxyList.Selected = &r.proxyRow
		r.proxyList.Key = func(i int) any { return a.Data.Proxies[i].ID }
		r.proxyList.Label = func(i int) string { return a.Data.Proxies[i].Name }
		cols := []ui.TableColumn{{Title: "名称"}, {Title: "类型", Width: 70}, {Title: "地址"}, {Title: "被引用", Width: 64, Align: ui.End}}
		ui.Table(c.Key("proxies"), &r.proxyList, cols, len(a.Data.Proxies), func(row, col int) {
			p := a.Data.Proxies[row]
			switch col {
			case 0:
				ui.Text(c, p.Name).SingleLine()
			case 1:
				faint(c, strings.ToUpper(p.Kind), row == r.proxyRow)
			case 2:
				ui.Text(c, fmt.Sprintf("%s:%d", p.Host, p.Port)).Font("monospace").FontSize(12).SingleLine()
			default:
				faint(c, strconv.Itoa(len(a.hostsUsing(func(cc model.Connection) bool { return cc.ProxyID == p.ID }))), row == r.proxyRow)
			}
		}).Grow(1).FillWidth()
	}, func() {
		if r.proxyRow < 0 || r.proxyRow >= len(a.Data.Proxies) {
			muted(c, "选择一个代理查看使用它的主机。")
			return
		}
		p := a.Data.Proxies[r.proxyRow]
		users := a.hostsUsing(func(cc model.Connection) bool { return cc.ProxyID == p.ID })
		ui.Text(c, p.Name).FontSize(18).Bold()
		muted(c, fmt.Sprintf("%s · %s:%d", strings.ToUpper(p.Kind), p.Host, p.Port))
		ui.Row(c).Gap(8).Children(func() {
			if ui.Button(c, "编辑…").Clicked() {
				a.openProxyDialog(p, nil)
			}
			del := ui.Button(c, "删除").Disabled(len(users) > 0)
			if len(users) > 0 {
				del.Tooltip("有主机在使用，不能删除")
			}
			if del.Clicked() {
				a.confirm("删除代理「"+p.Name+"」", "删除后无法恢复。", "删除代理", func() {
					a.report(a.P.Delete("proxy", p.ID), "已删除代理")
					a.reload()
				})
			}
		})
		usedBy(c, users, nil)
	})
}

// openProxyDialog edits p; a new proxy's ID goes into *target.
func (a *App) openProxyDialog(p model.Proxy, target *string) {
	a.openManager()
	port := "1080"
	if p.Port > 0 {
		port = strconv.Itoa(p.Port)
	}
	a.hosts.records.proxy = proxyDialog{open: true, proxy: p, port: port, target: target}
}

func (a *App) proxyDialog(c *ui.Context) {
	s := &a.hosts.records.proxy
	if !s.open {
		return
	}
	enter := false // Enter in a field, or ⌘S, saves
	dialog(c, &s.open, func() {
		ui.Column(c).Width(440).Gap(14).Children(func() {
			title := "新建代理"
			if s.proxy.ID != "" {
				title = "编辑代理"
			}
			ui.Text(c, title).FontSize(17).Bold()
			ui.Form(c, func() {
				field(c, "名称", &s.proxy.Name, &enter)
				choice(c, "类型", &s.proxy.Kind, []string{"socks5", "socks4"})
				field(c, "主机", &s.proxy.Host, &enter)
				field(c, "端口", &s.port, &enter)
				field(c, "用户名", &s.proxy.User, &enter)
				if s.proxy.ID != "" && !s.replaceSecret {
					ui.Field(c, "密码", func() {
						if ui.Button(c, "修改已保存的密码").Clicked() {
							s.replaceSecret = true
						}
					}).FillWidth()
				} else {
					password(c, "密码", &s.password, &enter)
				}
			}).FillWidth().Gap(FormGap)
			if s.err != "" {
				ui.Text(c, s.err).TextColor(c.Theme().Danger)
			}
			ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "取消").Clicked() {
					s.open = false
				}
				if ui.PrimaryButton(c, "保存代理").Clicked() || enter || c.Shortcut(ui.Cmd, ui.KeyS) {
					a.saveProxy()
				}
			})
		})
	})
}
func (a *App) saveProxy() {
	s := &a.hosts.records.proxy
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
	if s.target != nil {
		*s.target = p.ID
	}
	*s = proxyDialog{}
	a.reload()
	a.toast("已保存代理")
}

// Credentials.

func (a *App) credentialsPage(c *ui.Context) {
	r := &a.hosts.records
	recordPage(c, func() {
		if ui.PrimaryButton(c, "新建凭据…").Clicked() {
			a.openCredentialDialog(model.Credential{Auth: "password"}, nil)
		}
		muted(c, "一组用户名和认证，多台主机共用；修改一次，所有使用它的主机都生效。").Grow(1).MinWidth(0)
	}, func() {
		if len(a.Data.Credentials) == 0 {
			emptyState(c, "还没有凭据", "把常用的用户名和密码（或密钥）存成凭据，主机里选「使用凭据」即可。")
			return
		}
		r.credList.Selected = &r.credRow
		r.credList.Key = func(i int) any { return a.Data.Credentials[i].ID }
		r.credList.Label = func(i int) string { return a.Data.Credentials[i].Name }
		cols := []ui.TableColumn{{Title: "名称"}, {Title: "用户名"}, {Title: "认证"}, {Title: "被引用", Width: 64, Align: ui.End}}
		ui.Table(c.Key("credentials"), &r.credList, cols, len(a.Data.Credentials), func(row, col int) {
			cr := a.Data.Credentials[row]
			switch col {
			case 0:
				ui.Text(c, cr.Name).SingleLine()
			case 1:
				ui.Text(c, cr.User).Font("monospace").FontSize(12).SingleLine()
			case 2:
				faint(c, authLabel(a, cr.Auth, cr.KeyID), row == r.credRow)
			default:
				faint(c, strconv.Itoa(len(a.hostsUsing(func(cc model.Connection) bool { return cc.CredentialID == cr.ID }))), row == r.credRow)
			}
		}).Grow(1).FillWidth()
	}, func() {
		if r.credRow < 0 || r.credRow >= len(a.Data.Credentials) {
			muted(c, "选择一个凭据查看使用它的主机。")
			return
		}
		cr := a.Data.Credentials[r.credRow]
		users := a.hostsUsing(func(cc model.Connection) bool { return cc.CredentialID == cr.ID })
		ui.Text(c, cr.Name).FontSize(18).Bold()
		muted(c, cr.User+" · "+authLabel(a, cr.Auth, cr.KeyID))
		ui.Row(c).Gap(8).Children(func() {
			if ui.Button(c, "编辑…").Clicked() {
				a.openCredentialDialog(cr, nil)
			}
			if ui.Button(c, "删除").Clicked() {
				msg := "删除后无法恢复。"
				if len(users) > 0 {
					msg = fmt.Sprintf("有 %d 个主机使用这个凭据，删除后它们改用各自的用户名和认证。", len(users))
				}
				a.confirm("删除凭据「"+cr.Name+"」", msg, "删除凭据", func() {
					a.report(a.P.Delete("credential", cr.ID), "已删除凭据")
					a.reload()
				})
			}
		})
		usedBy(c, users, nil)
	})
}

// openCredentialDialog edits cr; a new credential's ID goes into *target.
func (a *App) openCredentialDialog(cr model.Credential, target *string) {
	a.openManager()
	a.hosts.records.cred = credentialDialog{open: true, cred: cr, target: target}
}

func (a *App) credentialDialog(c *ui.Context) {
	s := &a.hosts.records.cred
	if !s.open {
		return
	}
	enter := false // Enter in a field, or ⌘S, saves
	dialog(c, &s.open, func() {
		ui.Column(c).Width(500).Gap(14).Children(func() {
			title := "新建凭据"
			if s.cred.ID != "" {
				title = "编辑凭据"
			}
			ui.Text(c, title).FontSize(17).Bold()
			ui.Form(c, func() {
				field(c, "名称", &s.cred.Name, &enter)
				field(c, "用户名", &s.cred.User, &enter)
				authKind(c, &s.cred.Auth)
				switch s.cred.Auth {
				case "password":
					if s.cred.ID != "" && !s.replaceSecret {
						ui.Field(c, "密码", func() {
							if ui.Button(c, "修改已保存的密码").Clicked() {
								s.replaceSecret = true
							}
						}).FillWidth()
					} else {
						password(c, "密码", &s.password, &enter).Description("留空则在连接时询问")
					}
				case "key":
					a.keyPick(c, &s.cred.KeyID)
				}
			}).FillWidth().Gap(FormGap)
			if s.err != "" {
				ui.Text(c, s.err).TextColor(c.Theme().Danger)
			}
			ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "取消").Clicked() {
					s.open = false
				}
				if ui.PrimaryButton(c, "保存凭据").Clicked() || enter || c.Shortcut(ui.Cmd, ui.KeyS) {
					a.saveCredential()
				}
			})
		})
	})
}
func (a *App) saveCredential() {
	s := &a.hosts.records.cred
	cr := s.cred
	cr.Name, cr.User = strings.TrimSpace(cr.Name), strings.TrimSpace(cr.User)
	if e := cr.Validate(); e != nil {
		s.err = e.Error()
		return
	}
	var secret *string
	if cr.ID == "" || s.replaceSecret {
		secret = &s.password
	}
	if cr.ID == "" {
		cr.ID = a.P.NewID()
	}
	if e := a.P.SaveCredential(cr, secret); e != nil {
		s.err = e.Error()
		return
	}
	if s.target != nil {
		*s.target = cr.ID
	}
	*s = credentialDialog{}
	a.reload()
	a.toast("已保存凭据：" + cr.Name)
}

// Import and export.

// openImport opens the import dialog; a path (a dropped file) is previewed at once.
func (a *App) openImport(path string) {
	a.openManager()
	d := &a.hosts.records.data
	d.importOpen, d.preview = true, nil
	if path != "" {
		d.path, d.format = path, "nextshell"
		a.P.PreviewImport(path, "", d.format)
	}
}

// openExport opens the export dialog for the hosts ids, all of them when empty.
func (a *App) openExport(ids []string) {
	a.openManager()
	d := &a.hosts.records.data
	d.exportOpen, d.exportIDs = true, ids
}

func (a *App) importDialog(c *ui.Context) {
	s := &a.hosts.records.data
	if !s.importOpen {
		return
	}
	t := c.Theme()
	dialog(c, &s.importOpen, func() {
		ui.Column(c).Width(560).Gap(14).Children(func() {
			ui.Text(c, "导入连接").FontSize(17).Bold()
			ui.Form(c, func() {
				formats := []string{"nextshell", "finalshell"}
				i := max(0, indexOf(formats, s.format))
				ui.Field(c, "来源", func() {
					if ui.Segmented(c, &i, "NextShell 1.x", "FinalShell").Label("来源").Changed() {
						s.format = formats[i]
					}
				}).FillWidth()
				if s.format == "finalshell" {
					a.localPath(c, "配置目录", "未选择", &s.path, FilePick{Directory: true, Hidden: true}).Description("FinalShell 的配置目录")
				} else {
					a.localPath(c, "连接文件", "未选择", &s.path, FilePick{}).Description("NextShell 导出的连接文件，也可以直接拖到连接管理器窗口")
				}
				password(c, "解密口令", &s.password).Description("导出时没有加密就留空")
				a.folderPick(c, "导入到目录", &s.folder)
				policies := []string{"skip", "rename-copy", "overwrite"}
				j := max(0, indexOf(policies, s.policy))
				ui.Field(c, "同名主机", func() {
					if ui.Segmented(c, &j, "跳过", "另存副本", "覆盖").Label("同名主机").Changed() {
						s.policy = policies[j]
					}
				}).FillWidth()
			}).FillWidth().Gap(FormGap)
			if p := s.preview; p != nil {
				ui.Column(c).FillWidth().Padding(10).Gap(4).Radius(t.Radius).Border(1, t.Border).Children(func() {
					ui.Text(c, fmt.Sprintf("将导入 %d 个主机", p.Count)).Bold()
					if p.PasswordsUnavailable > 0 {
						muted(c, fmt.Sprintf("%d 个主机的密码无法导入，连接时会询问", p.PasswordsUnavailable))
					}
					if p.KeysNeedRebind > 0 {
						muted(c, fmt.Sprintf("%d 个主机使用的密钥还没有导入：它们保持密钥认证并标红，导入密钥后自动绑定", p.KeysNeedRebind))
					}
					muted(c, strings.Join(p.Names, "、"))
					for _, w := range p.Warnings {
						ui.Text(c, w).FontSize(12).TextColor(t.Warning)
					}
					for _, target := range p.Targets {
						muted(c, "已存在："+target)
					}
				})
			}
			ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "取消").Clicked() {
					s.importOpen = false
				}
				if ui.Button(c, "预览").Disabled(strings.TrimSpace(s.path) == "").Clicked() {
					a.P.PreviewImport(strings.TrimSpace(s.path), s.password, s.format)
					s.password = ""
				}
				if s.preview != nil {
					a.commitImportButton(c)
				}
			})
		})
	})
}
func (a *App) commitImportButton(c *ui.Context) {
	s := &a.hosts.records.data
	if ui.PrimaryButton(c, fmt.Sprintf("导入 %d 个主机", s.preview.Count)).Clicked() {
		run := func() { a.P.CommitImport(s.preview.Token, s.folder, s.policy) }
		if s.policy == "overwrite" && len(s.preview.Targets) > 0 {
			a.confirm("覆盖已有主机", fmt.Sprintf("%d 个已有主机会被覆盖；文件里明确为空的密码会清空已保存的密码。", len(s.preview.Targets)), fmt.Sprintf("覆盖 %d 个主机", len(s.preview.Targets)), run)
		} else {
			run()
		}
	}
}
func (a *App) exportDialog(c *ui.Context) {
	s := &a.hosts.records.data
	if !s.exportOpen {
		return
	}
	dialog(c, &s.exportOpen, func() {
		ui.Column(c).Width(520).Gap(14).Children(func() {
			label := "导出全部主机"
			if len(s.exportIDs) > 0 {
				label = fmt.Sprintf("导出 %d 个主机", len(s.exportIDs))
			}
			ui.Text(c, label).FontSize(17).Bold()
			muted(c, "私钥永远不会导出；使用凭据的主机会带上凭据。在主机磁贴上按住 "+modifierName()+" 多选，可以只导出所选。")
			ui.Form(c, func() {
				a.localPath(c, "保存到", "未选择", &s.exportPath, FilePick{Save: true, DefaultPath: "~/nextshell-hosts.json"}).Description("不会覆盖已有文件")
				toggle(c, "加密并包含密码", &s.encrypt)
				if s.encrypt {
					password(c, "加密口令", &s.exportPassword)
				} else {
					muted(c, "不加密时不包含任何密码")
				}
			}).FillWidth().Gap(FormGap)
			ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "取消").Clicked() {
					s.exportOpen = false
				}
				if ui.PrimaryButton(c, label).Disabled(strings.TrimSpace(s.exportPath) == "").Clicked() {
					var pass *string
					if s.encrypt {
						if s.exportPassword == "" {
							a.fail(fmt.Errorf("加密导出需要口令"))
							return
						}
						p := s.exportPassword
						pass = &p
					}
					a.P.ExportConnections(strings.TrimSpace(s.exportPath), pass, s.exportIDs)
					s.exportPassword = ""
					s.exportOpen = false
				}
			})
		})
	})
}
