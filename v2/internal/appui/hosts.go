package appui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/model"
)

type hostsState struct {
	selected           string
	filter             string
	filterOpen         bool
	collapsed          map[string]bool
	connecting, failed map[string]bool
	recent             []string

	editor hostEditor
	folder folderEditor
	batch  batchAuth
}

// hostEditor is the 新建 / 编辑主机 dialog.
type hostEditor struct {
	open                       bool
	section                    int
	edit                       model.Connection
	port, tags, password, err  string
	keepalive                  string
	keepaliveMode              string
	replaceSecret, inheritOrig bool
}

const (
	keepaliveGlobal = "跟随全局设置"
	keepaliveCustom = "自定义间隔"
	keepaliveOff    = "关闭"
)

type folderEditor struct {
	open   bool
	folder model.Folder
	err    string
}

// batchAuth sets the authentication of every host in a folder and its subfolders.
type batchAuth struct {
	open                   bool
	folder                 string
	auth, key, proxy, pass string
}

func (h *hostsState) init() {
	h.collapsed = map[string]bool{}
	h.connecting = map[string]bool{}
	h.failed = map[string]bool{}
}

func (a *App) connect(cc model.Connection) {
	if cc.ID != "" {
		a.hosts.connecting[cc.ID] = true
		delete(a.hosts.failed, cc.ID)
		a.hosts.recent = append([]string{cc.ID}, remove(a.hosts.recent, cc.ID)...)
	}
	a.P.Connect(cc)
}
func remove(ss []string, s string) []string {
	out := ss[:0:0]
	for _, v := range ss {
		if v != s {
			out = append(out, v)
		}
	}
	return out
}

// recentHosts returns the hosts connected to in this run, then favorites, then the rest.
func (h *hostsState) recentHosts(a *App, n int) []model.Connection {
	var out []model.Connection
	seen := map[string]bool{}
	add := func(c model.Connection) {
		if len(out) < n && !c.Deleted && !seen[c.ID] {
			seen[c.ID] = true
			out = append(out, c)
		}
	}
	for _, id := range h.recent {
		if c, ok := a.host(id); ok {
			add(c)
		}
	}
	for _, c := range a.Data.Connections {
		if c.Favorite {
			add(c)
		}
	}
	for _, c := range a.Data.Connections {
		add(c)
	}
	return out
}
func (a *App) host(id string) (model.Connection, bool) {
	for _, c := range a.Data.Connections {
		if c.ID == id {
			return c, true
		}
	}
	return model.Connection{}, false
}

// hostStatus is the color of a host's dot, or false for none.
func (a *App) hostStatus(c *ui.Context, id string) (ui.Color, bool) {
	t := c.Theme()
	if a.hosts.connecting[id] {
		return t.Warning, true
	}
	if a.hosts.failed[id] {
		return t.Danger, true
	}
	seen := false
	for _, s := range a.Tabs {
		if s.Connection.ID == id {
			if !a.P.Exited(s.ID) {
				return t.Success, true
			}
			seen = true
		}
	}
	return t.TextMuted, seen
}

func (a *App) sidebar(c *ui.Context) {
	t := c.Theme()
	h := &a.hosts
	ui.Column(c).Fill().MinWidth(0).Background(t.Surface).Children(func() {
		ui.Row(c).FillWidth().Height(36).Padding(0, 6, 0, 12).Gap(2).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "主机").Bold().Grow(1)
			iconButton(c, "新建", "plus").PopoverMenuButton(func(m *ui.Menu) {
				if m.Item("新建主机…").Chosen() {
					a.later(func() { a.newHost("") })
				}
				if m.Item("新建目录…").Chosen() {
					a.later(func() { a.editFolder(model.Folder{}) })
				}
			})
			if iconButton(c, "过滤主机", "search").Clicked() {
				h.filterOpen = !h.filterOpen
				if !h.filterOpen {
					h.filter = ""
				}
			}
		})
		if h.filterOpen {
			ui.SearchField(c, &h.filter).Label("过滤主机").Placeholder("名称、地址或标签").AutoFocus().Margin(0, 8, 6, 8)
		}
		if len(a.Data.Connections) == 0 {
			ui.Column(c).FillWidth().Padding(12).Gap(8).Children(func() {
				muted(c, "还没有主机")
				if ui.Button(c, "新建主机").Clicked() {
					a.newHost("")
				}
			})
		}
		list := ui.Sidebar(c.Key("hosts"), &h.selected, func() {
			if strings.TrimSpace(h.filter) != "" {
				for _, cc := range a.Data.Connections {
					if !cc.Deleted && a.matches(cc) {
						a.hostItem(c, cc, "filter")
					}
				}
				return
			}
			var favorites []model.Connection
			for _, cc := range a.Data.Connections {
				if cc.Favorite && !cc.Deleted {
					favorites = append(favorites, cc)
				}
			}
			if len(favorites) > 0 {
				open := !h.collapsed["★"]
				ui.SidebarSection(c.Key("favorites"), "★ 收藏", &open, func() {
					for _, cc := range favorites {
						a.hostItem(c, cc, "fav")
					}
				})
				h.collapsed["★"] = !open
			}
			a.hostTree(c, "", 0)
		}).Label("主机列表").Grow(1).FillWidth()
		if list.Shortcut(0, ui.KeyEnter) {
			if cc, ok := a.host(h.selected); ok {
				a.connect(cc)
			}
		}
		ui.Row(c).FillWidth().Padding(6, 8).BorderColor(t.Border).BorderWidth(1, 0, 0, 0).Children(func() {
			if ghost(c, "本地终端", "terminal").Grow(1).Clicked() {
				a.P.Local()
			}
		})
	})
}

func (a *App) hostTree(c *ui.Context, parent string, depth int) {
	if depth > 30 {
		return
	}
	var direct []model.Connection
	sections := len(a.Data.Folders) > 0
	for _, cc := range a.Data.Connections {
		if cc.FolderID == parent && !cc.Deleted {
			direct = append(direct, cc)
		}
		sections = sections || cc.Favorite && !cc.Deleted
	}
	hosts := func() {
		for _, cc := range direct {
			a.hostItem(c, cc, "tree")
		}
	}
	// Beside sections, hosts outside folders get a section of their own.
	if parent == "" && len(direct) > 0 && sections {
		open := !a.hosts.collapsed[""]
		ui.SidebarSection(c.Key("ungrouped"), "未分组", &open, hosts)
		a.hosts.collapsed[""] = !open
	} else {
		hosts()
	}
	for _, f := range a.Data.Folders {
		if f.ParentID != parent {
			continue
		}
		open := !a.hosts.collapsed[f.ID]
		section := ui.SidebarSection(c.Key(f.ID+"folder"), fmt.Sprintf("%s  %d", f.Name, a.folderCount(f.ID)), &open, func() { a.hostTree(c, f.ID, depth+1) })
		if depth > 0 {
			section.PaddingStart(12) // nests inside the parent's section, so it adds up per depth
		}
		a.folderMenu(section, f)
		a.hosts.collapsed[f.ID] = !open
	}
}
func (a *App) folderCount(id string) int {
	ids := a.folderTree(id)
	n := 0
	for _, cc := range a.Data.Connections {
		if ids[cc.FolderID] && !cc.Deleted {
			n++
		}
	}
	return n
}

// folderTree returns root and every folder under it.
func (a *App) folderTree(root string) map[string]bool {
	folders := map[string]bool{root: true}
	for range a.Data.Folders {
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
	return folders
}

func (a *App) hostItem(c *ui.Context, cc model.Connection, where string) {
	item := ui.SidebarItem(c.Key(where+cc.ID), cc.ID, appIcons["server"], cc.Name).Tooltip(cc.User + "@" + cc.Address())
	a.hostMenu(item, cc)
	if color, ok := a.hostStatus(c, cc.ID); ok {
		item.Children(func() { dot(c, color) })
	}
	if item.DoubleClicked() {
		a.connect(cc)
	}
}

func (a *App) matches(c model.Connection) bool {
	q := strings.ToLower(strings.TrimSpace(a.hosts.filter))
	return q == "" || strings.Contains(strings.ToLower(c.Name+" "+c.Host+" "+c.User+" "+c.Notes+" "+strings.Join(c.Tags, " ")+" "+a.folderPath(c.FolderID)), q)
}

func (a *App) hostMenu(item ui.Element, cc model.Connection) {
	item.ContextMenu(func(m *ui.Menu) {
		if m.Item("连接").Chosen() {
			a.later(func() { a.connect(cc) })
		}
		if m.Item("连接并打开文件面板").Chosen() {
			a.later(func() { a.connect(cc); a.showPanel(panelFiles) })
		}
		m.Separator()
		if m.Item("编辑…").Chosen() {
			a.later(func() { a.editHost(cc) })
		}
		if m.Item("复制主机").Chosen() {
			a.later(func() {
				v, e := a.P.DuplicateConnection(cc.ID)
				a.report(e, "")
				if e == nil {
					a.reload()
					a.editHost(v)
				}
			})
		}
		if m.Item("收藏").Checked(cc.Favorite).Chosen() {
			a.later(func() {
				cc.Favorite = !cc.Favorite
				a.report(a.P.SaveConnection(cc, nil), "")
				a.reload()
			})
		}
		m.Submenu("移动到", func(m *ui.Menu) {
			if m.Item("根目录").Radio(cc.FolderID == "").Chosen() {
				a.later(func() { a.moveHost(cc, "") })
			}
			for _, f := range a.Data.Folders {
				if m.Item(a.folderPath(f.ID)).ID(f.ID).Radio(cc.FolderID == f.ID).Chosen() {
					a.later(func() { a.moveHost(cc, f.ID) })
				}
			}
		})
		if m.Item("复制地址").Chosen() {
			a.P.CopyText(cc.User + "@" + cc.Address())
		}
		m.Separator()
		if m.Item("导出…").Chosen() {
			a.later(func() { a.settings.selectHosts(cc.ID); a.openSettings("导入导出") })
		}
		if m.Item("复制到云工作区…").Chosen() {
			a.later(func() { a.settings.selectHosts(cc.ID); a.openSettings("同步") })
		}
		m.Separator()
		if m.Item("移入回收站").Danger().Chosen() {
			a.later(func() { a.trashHosts([]model.Connection{cc}) })
		}
	})
}
func (a *App) moveHost(cc model.Connection, folder string) {
	cc.FolderID = folder
	a.report(a.P.SaveConnection(cc, nil), "")
	a.reload()
}
func (a *App) trashHosts(cs []model.Connection) {
	for _, v := range cs {
		v.Deleted = true
		if e := a.P.SaveConnection(v, nil); e != nil {
			a.fail(e)
			break
		}
	}
	a.reload()
	if len(cs) == 1 {
		restore := cs[0]
		a.toasts = append(a.toasts, ui.Toast{Title: "已移入回收站：" + restore.Name, Action: "撤销", OnAction: func() {
			restore.Deleted = false
			a.report(a.P.SaveConnection(restore, nil), "")
			a.reload()
		}})
	}
}

func (a *App) folderMenu(e ui.Element, f model.Folder) {
	e.ContextMenu(func(m *ui.Menu) {
		if m.Item("在此新建主机…").Chosen() {
			a.later(func() { a.newHost(f.ID) })
		}
		if m.Item("新建子目录…").Chosen() {
			a.later(func() { a.editFolder(model.Folder{ParentID: f.ID}) })
		}
		if m.Item("重命名 / 移动…").Chosen() {
			a.later(func() { a.editFolder(f) })
		}
		if m.Item("批量设置认证…").Chosen() {
			a.later(func() { a.hosts.batch = batchAuth{open: true, folder: f.ID, auth: "password"} })
		}
		m.Separator()
		if m.Item("删除目录").Danger().Chosen() {
			a.later(func() {
				a.confirm("删除目录「"+f.Name+"」", "只能删除空目录。", "删除目录", func() {
					a.report(a.P.Delete("folder", f.ID), "已删除目录")
					a.reload()
				})
			})
		}
	})
}

// Host editor.

func (a *App) newHost(folder string) {
	a.hosts.editor = hostEditor{open: true, edit: model.Connection{Auth: "password", Encoding: "utf-8", Port: 22, FolderID: folder}, port: "22", keepaliveMode: keepaliveGlobal, keepalive: strconv.Itoa(model.DefaultKeepaliveSeconds), inheritOrig: true}
}
func (a *App) editHost(c model.Connection) {
	e := hostEditor{open: true, edit: c, port: strconv.Itoa(c.Port), tags: strings.Join(c.Tags, ", "), keepalive: strconv.Itoa(c.EffectiveKeepalive())}
	switch {
	case c.KeepaliveEnabled == nil && c.Keepalive == 0:
		e.keepaliveMode = keepaliveGlobal
		e.inheritOrig = true
	case c.EffectiveKeepalive() == 0:
		e.keepaliveMode = keepaliveOff
		e.keepalive = strconv.Itoa(model.DefaultKeepaliveSeconds)
	default:
		e.keepaliveMode = keepaliveCustom
	}
	a.hosts.editor = e
}

// draft turns the dialog's fields into a connection.
func (e *hostEditor) draft() (model.Connection, error) {
	cc := e.edit
	cc.Tags = nil
	seen := map[string]bool{}
	for _, tag := range strings.Split(e.tags, ",") {
		if tag = strings.TrimSpace(tag); tag != "" && !seen[tag] {
			cc.Tags = append(cc.Tags, tag)
			seen[tag] = true
		}
	}
	var err error
	if cc.Port, err = strconv.Atoi(strings.TrimSpace(e.port)); err != nil {
		return cc, fmt.Errorf("端口须为数字")
	}
	switch e.keepaliveMode {
	case keepaliveGlobal:
		if !e.inheritOrig {
			cc.KeepaliveEnabled, cc.KeepaliveIntervalSec, cc.Keepalive = nil, nil, 0
		}
	case keepaliveOff:
		off, zero := false, 0
		cc.KeepaliveEnabled, cc.KeepaliveIntervalSec, cc.Keepalive = &off, &zero, 0
	default:
		n, err := strconv.Atoi(strings.TrimSpace(e.keepalive))
		if err != nil || n < 1 || n > 3600 {
			return cc, fmt.Errorf("心跳间隔须为 1–3600 秒")
		}
		on := true
		cc.KeepaliveEnabled, cc.KeepaliveIntervalSec, cc.Keepalive = &on, &n, n
	}
	if cc.Auth != "key" {
		cc.KeyID = ""
	}
	return cc, cc.Validate()
}

// saveHost saves the dialog's host and reports whether it did.
func (a *App) saveHost() (model.Connection, bool) {
	e := &a.hosts.editor
	cc, err := e.draft()
	if err != nil {
		e.err = err.Error()
		return cc, false
	}
	fresh := cc.ID == ""
	if fresh {
		cc.ID = a.P.NewID()
	}
	var secret *string
	if cc.Auth == "password" && (fresh || e.replaceSecret) {
		secret = &e.password
	}
	if err := a.P.SaveConnection(cc, secret); err != nil {
		e.err = err.Error()
		return cc, false
	}
	e.password = ""
	e.open = false
	a.reload()
	a.toast("已保存主机：" + cc.Name)
	return cc, true
}

func (a *App) hostDialog(c *ui.Context) {
	e := &a.hosts.editor
	if !e.open {
		return
	}
	t := c.Theme()
	dialog(c, &e.open, func() {
		ui.Column(c).Width(560).Gap(16).Children(func() {
			title := "新建主机"
			if e.edit.ID != "" {
				title = "编辑主机"
			}
			ui.Text(c, title).FontSize(17).Bold()
			ui.Row(c).FillWidth().Gap(16).MinHeight(300).AlignItems(ui.Start).Children(func() {
				ui.Column(c).Width(88).Gap(2).FocusGroup(ui.Vertical).Children(func() {
					for i, label := range []string{"常规", "认证", "高级"} {
						b := ui.ButtonBase(c).Label(label).Padding(6, 10).Radius(t.Radius).FillWidth()
						if i == e.section {
							b.Background(t.Accent.Alpha(.15)).TextColor(t.Accent)
						} else if b.Hovered() {
							b.Background(t.SurfaceHover)
						}
						b.Children(func() { ui.Text(c, label) })
						if b.Clicked() {
							e.section = i
						}
					}
				})
				ui.Form(c, func() {
					switch e.section {
					case 0:
						ui.Field(c, "名称", func() { ui.TextInput(c, &e.edit.Name).Label("名称").Placeholder("prod-web-01").AutoFocus().Grow(1) }).FillWidth()
						field(c, "主机", &e.edit.Host)
						field(c, "端口", &e.port)
						field(c, "用户名", &e.edit.User)
						a.folderPick(c, "目录", &e.edit.FolderID)
						field(c, "标签", &e.tags).Description("用逗号分隔")
					case 1:
						a.authFields(c, e)
					default:
						a.advancedFields(c, e)
					}
				}).Grow(1).Gap(FormGap)
			})
			if e.err != "" {
				ui.Text(c, e.err).TextColor(t.Danger)
			}
			ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "取消").Clicked() {
					e.open = false
				}
				if ui.Button(c, "保存").Clicked() {
					a.saveHost()
				}
				if ui.PrimaryButton(c, "保存并连接").Clicked() {
					if cc, ok := a.saveHost(); ok {
						a.connect(cc)
					}
				}
			})
		})
	})
}

var authKinds = []string{"password", "key", "agent", "keyboard"}

func authKind(c *ui.Context, auth *string) {
	kind := max(0, indexOf(authKinds, *auth))
	ui.Field(c, "方式", func() {
		if ui.Segmented(c, &kind, "密码", "密钥", "SSH Agent", "键盘交互").FillEqually().Label("认证方式").Changed() {
			*auth = authKinds[kind]
		}
	}).FillWidth()
}

func (a *App) authFields(c *ui.Context, e *hostEditor) {
	authKind(c, &e.edit.Auth)
	switch e.edit.Auth {
	case "password":
		if e.edit.ID != "" && !e.replaceSecret {
			ui.Field(c, "密码", func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					muted(c, "已保存")
					if ui.Button(c, "修改").Clicked() {
						e.replaceSecret = true
					}
				})
			}).FillWidth()
		} else {
			password(c, "密码", &e.password).Description("留空则在连接时询问")
		}
	case "key":
		a.keyPick(c, &e.edit.KeyID)
	case "agent":
		muted(c, "使用系统 SSH Agent 中的密钥")
	case "keyboard":
		muted(c, "连接时在对话框里回答服务器的问题")
	}
}

// keyPick chooses a key; its last two entries open the key settings.
func (a *App) keyPick(c *ui.Context, id *string) {
	const importKey, generateKey = "导入密钥…", "生成新密钥…"
	opts := []string{"（未选择）"}
	chosen := opts[0]
	for _, k := range a.Data.Keys {
		opts = append(opts, k.Name)
		if k.ID == *id {
			chosen = k.Name
		}
	}
	opts = append(opts, importKey, generateKey)
	choice(c, "密钥", &chosen, opts)
	switch chosen {
	case importKey, generateKey:
		a.later(func() { a.openSettings("密钥") })
		return
	}
	*id = ""
	for _, k := range a.Data.Keys {
		if k.Name == chosen {
			*id = k.ID
		}
	}
}

func (a *App) advancedFields(c *ui.Context, e *hostEditor) {
	var ids, names []string
	for _, p := range a.Data.Proxies {
		ids = append(ids, p.ID)
		names = append(names, p.Name)
	}
	pick(c, "代理", "不使用代理", &e.edit.ProxyID, ids, names)
	choice(c, "字符编码", &e.edit.Encoding, []string{"utf-8", "gb18030", "gbk", "big5", "shift_jis"})
	ui.Field(c, "心跳", func() {
		if ui.Select(c, &e.keepaliveMode, []string{keepaliveGlobal, keepaliveCustom, keepaliveOff}).Label("心跳").Grow(1).Changed() {
			e.inheritOrig = false
		}
	}).FillWidth()
	if e.keepaliveMode == keepaliveCustom {
		field(c, "间隔（秒）", &e.keepalive)
	}
	toggle(c, "使用 ~/.ssh/known_hosts", &e.edit.UseKnownHosts)
	field(c, "固定主机指纹", &e.edit.Fingerprint).Description("可选，如 SHA256:…")
	ui.Field(c, "备注", func() { ui.TextArea(c, &e.edit.Notes).Label("备注").Height(60).Grow(1) }).FillWidth()
}
func indexOf(ss []string, s string) int {
	for i, v := range ss {
		if v == s {
			return i
		}
	}
	return -1
}

// Folder dialog.

func (a *App) editFolder(f model.Folder) { a.hosts.folder = folderEditor{open: true, folder: f} }
func (a *App) folderDialog(c *ui.Context) {
	e := &a.hosts.folder
	if !e.open {
		return
	}
	dialog(c, &e.open, func() {
		ui.Column(c).Width(400).Gap(16).Children(func() {
			title := "新建目录"
			if e.folder.ID != "" {
				title = "编辑目录"
			}
			ui.Text(c, title).FontSize(17).Bold()
			ui.Form(c, func() {
				ui.Field(c, "名称", func() { ui.TextInput(c, &e.folder.Name).Label("目录名称").AutoFocus().Grow(1) }).FillWidth()
				a.folderPick(c, "上级目录", &e.folder.ParentID)
			}).FillWidth().Gap(FormGap)
			if e.err != "" {
				ui.Text(c, e.err).TextColor(c.Theme().Danger)
			}
			ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "取消").Clicked() {
					e.open = false
				}
				if ui.PrimaryButton(c, "保存目录").Clicked() {
					a.saveFolder()
				}
			})
		})
	})
}
func (a *App) saveFolder() {
	e := &a.hosts.folder
	f := e.folder
	f.Name = strings.TrimSpace(f.Name)
	if f.Name == "" {
		e.err = "名称不能为空"
		return
	}
	if f.ID != "" && a.folderTree(f.ID)[f.ParentID] {
		e.err = "不能移动到自己的子目录"
		return
	}
	if f.ID == "" {
		f.ID = a.P.NewID()
	}
	if err := a.P.SaveFolder(f); err != nil {
		e.err = err.Error()
		return
	}
	e.open = false
	a.reload()
}

// Batch authentication dialog.

func (a *App) batchDialog(c *ui.Context) {
	b := &a.hosts.batch
	if !b.open {
		return
	}
	targets := a.folderHosts(b.folder)
	dialog(c, &b.open, func() {
		ui.Column(c).Width(460).Gap(16).Children(func() {
			ui.Text(c, "批量设置认证").FontSize(17).Bold()
			muted(c, fmt.Sprintf("将应用到「%s」及子目录中的 %d 个主机", a.folderPath(b.folder), len(targets)))
			ui.Form(c, func() {
				authKind(c, &b.auth)
				switch b.auth {
				case "password":
					password(c, "密码", &b.pass).Description("留空则保留各主机已保存的密码")
				case "key":
					a.keyPick(c, &b.key)
				}
				var ids, names []string
				for _, p := range a.Data.Proxies {
					ids = append(ids, p.ID)
					names = append(names, p.Name)
				}
				pick(c, "代理", "不使用代理", &b.proxy, ids, names)
			}).FillWidth().Gap(FormGap)
			ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "取消").Clicked() {
					b.open = false
				}
				if ui.PrimaryButton(c, fmt.Sprintf("应用到 %d 个主机", len(targets))).Disabled(len(targets) == 0).Clicked() {
					a.applyBatch(targets)
				}
			})
		})
	})
}
func (a *App) folderHosts(folder string) []model.Connection {
	ids := a.folderTree(folder)
	var out []model.Connection
	for _, cc := range a.Data.Connections {
		if ids[cc.FolderID] && !cc.Deleted {
			out = append(out, cc)
		}
	}
	return out
}
func (a *App) applyBatch(targets []model.Connection) {
	b := &a.hosts.batch
	if b.auth == "key" && b.key == "" {
		a.fail(fmt.Errorf("请选择密钥"))
		return
	}
	for _, cc := range targets {
		cc.Auth, cc.ProxyID = b.auth, b.proxy
		cc.KeyID = ""
		if b.auth == "key" {
			cc.KeyID = b.key
		}
		var secret *string
		if b.auth == "password" && b.pass != "" {
			secret = &b.pass
		}
		if e := a.P.SaveConnection(cc, secret); e != nil {
			a.fail(e)
			return
		}
	}
	b.pass = ""
	b.open = false
	a.reload()
	a.toast(fmt.Sprintf("已更新 %d 个主机", len(targets)))
}
