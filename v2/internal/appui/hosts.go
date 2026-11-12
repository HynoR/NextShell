package appui

import (
	"fmt"
	"hash/fnv"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/model"
)

type hostsState struct {
	selected           string // the host whose card or form the manager shows
	filter             string
	connecting, failed map[string]bool
	recent             []string

	page   int    // the manager's page: managerHosts, managerKeys, managerProxies or managerCredentials
	folder string // the folder tree's choice: "" for all hosts, recentFolder, or a folder ID
	grid   ui.GridState
	chosen int
	picked ui.Selection[string]

	editor  hostEditor
	folders folderEditor
	batch   batchAuth
	retry   authRetry
	records recordsState
}

const (
	managerHosts = iota
	managerKeys
	managerProxies
	managerCredentials
)

// recentFolder is the folder tree's 最近连接.
const recentFolder = "\x00recent"

// hostEditor is the 新建 / 编辑主机 form in the manager's right column.
type hostEditor struct {
	open                       bool
	edit                       model.Connection
	port, tags, password, err  string
	keepalive                  string
	keepaliveMode              string
	replaceSecret, inheritOrig bool
	more                       [4]bool     // the open sections: 安全, 网络, 终端, 标签与备注
	enter                      bool        // Enter was pressed in a field this frame
	orig                       *hostEditor // the form as it first showed, to tell whether it changed
}

// form is what the user can change in the editor, without its own bookkeeping.
func (e hostEditor) form() hostEditor {
	e.open, e.err, e.more, e.enter, e.orig = false, "", [4]bool{}, false, nil
	return e
}

// dirty reports whether the open form holds edits that are not saved.
func (e *hostEditor) dirty() bool {
	return e.open && e.orig != nil && !reflect.DeepEqual(e.form(), *e.orig)
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

// batchAuth binds a credential, an authentication or a proxy to several hosts at once.
type batchAuth struct {
	open                         bool
	title                        string
	ids                          []string
	credential, auth, key, proxy string
	pass                         string
}

// keep is a batch field's default: leave each host's value alone.
const keep = "\x00keep"

// authRetry is the dialog after a host's login failed: another user, method, key or credential, saved
// to the host before the next attempt.
type authRetry struct {
	open     bool
	host     model.Connection
	message  string
	password string
}

func (h *hostsState) init() {
	h.connecting = map[string]bool{}
	h.failed = map[string]bool{}
	h.chosen = -1
	h.records.init()
}

func (a *App) connect(cc model.Connection) {
	if cc.ID != "" {
		a.hosts.connecting[cc.ID] = true
		delete(a.hosts.failed, cc.ID)
		a.hosts.recent = append([]string{cc.ID}, remove(a.hosts.recent, cc.ID)...)
		a.hosts.recent = a.hosts.recent[:min(len(a.hosts.recent), 20)]
		a.report(a.P.SaveUIState("recent", strings.Join(a.hosts.recent, "\n")), "")
	}
	a.P.Connect(cc)
	if a.view == managerWindow { // the session opens in the main window
		a.raiseMain()
	}
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
func (a *App) credential(id string) (model.Credential, bool) {
	for _, c := range a.Data.Credentials {
		if c.ID == id {
			return c, true
		}
	}
	return model.Credential{}, false
}
func (a *App) key(id string) (model.Key, bool) {
	for _, k := range a.Data.Keys {
		if k.ID == id {
			return k, true
		}
	}
	return model.Key{}, false
}

// hostColors are the colors a host can be given, named by hostColorNames. A host without one gets one
// of them by its id, the same across runs.
var hostColors = []string{"#ef4444", "#f97316", "#eab308", "#22c55e", "#14b8a6", "#3b82f6", "#a855f7", "#ec4899"}
var hostColorNames = []string{"红", "橙", "黄", "绿", "青", "蓝", "紫", "粉"}

// hostColor is the bar that marks cc on its tab and tile.
func hostColor(cc model.Connection) ui.Color {
	if cc.Color != "" {
		return ui.Hex(cc.Color)
	}
	h := fnv.New32a()
	h.Write([]byte(orDefault(cc.ID, cc.User+"@"+cc.Address())))
	return ui.Hex(hostColors[h.Sum32()%uint32(len(hostColors))])
}

// hostColor is hostColor of the host as saved now: a tab's copy is from when it connected.
func (a *App) hostColor(cc model.Connection) ui.Color {
	if h, ok := a.host(cc.ID); ok && cc.ID != "" {
		cc = h
	}
	return hostColor(cc)
}

// resolved is cc with the user and authentication it connects with: its credential's, if it uses one.
func (a *App) resolved(cc model.Connection) model.Connection {
	if cr, ok := a.credential(cc.CredentialID); ok {
		return cc.WithCredential(cr)
	}
	return cc
}

// login is "user@host:port" as cc connects.
func (a *App) login(cc model.Connection) string { return a.resolved(cc).User + "@" + cc.Address() }

// missingKey reports a key-auth host without a key it can use: the red mark of its tile.
func (a *App) missingKey(cc model.Connection) bool {
	r := a.resolved(cc)
	if r.Auth != "key" {
		return false
	}
	_, ok := a.key(r.KeyID)
	return !ok
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

func (a *App) matches(c model.Connection) bool {
	q := strings.ToLower(strings.TrimSpace(a.hosts.filter))
	return q == "" || strings.Contains(strings.ToLower(c.Name+" "+c.Host+" "+a.resolved(c).User+" "+c.Notes+" "+strings.Join(c.Tags, " ")+" "+a.folderPath(c.FolderID)), q)
}

func (a *App) hostMenu(item ui.Element, cc model.Connection) {
	item.ContextMenu(func(m *ui.Menu) {
		if m.Item("连接").Chosen() {
			a.later(func() { a.connect(cc) })
		}
		if m.Item("连接并打开 SFTP").Chosen() {
			a.later(func() { a.connect(cc); a.showBench(benchFiles) })
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
		if m.Item("绑定认证…").Chosen() {
			a.later(func() { a.openBatch("「"+cc.Name+"」", []string{cc.ID}, "") })
		}
		if m.Item("复制地址").Chosen() {
			a.P.CopyText(a.login(cc))
		}
		m.Separator()
		if m.Item("导出…").Chosen() {
			a.later(func() { a.openExport([]string{cc.ID}) })
		}
		if a.Cloud && m.Item("复制到云工作区…").Chosen() {
			a.later(func() { a.settings.selectHosts(cc.ID); a.openSettings("云同步") })
		}
		m.Separator()
		if m.Item("移入回收站").Danger().Chosen() {
			a.later(func() { a.trashHosts([]model.Connection{cc}) })
		}
	})
}

// hostDrag is a host dragged onto a folder of the tree.
type hostDrag string

func (a *App) hostDropTarget(c *ui.Context, e ui.Element, folder string) {
	if _, over := ui.DragOver[hostDrag](e); over {
		e.Background(c.Theme().SurfaceHover)
	}
	if id, ok := ui.Drop[hostDrag](e); ok {
		if cc, found := a.host(string(id)); found && cc.FolderID != folder {
			a.moveHost(cc, folder)
		}
	}
}

// folderSiblings is f's parent's folders in order, and f's place among them.
func (a *App) folderSiblings(f model.Folder) ([]model.Folder, int) {
	var out []model.Folder
	for _, v := range a.Data.Folders {
		if v.ParentID == f.ParentID {
			out = append(out, v)
		}
	}
	return out, slices.IndexFunc(out, func(v model.Folder) bool { return v.ID == f.ID })
}

// shiftFolder moves f up (-1) or down (+1) among its siblings.
func (a *App) shiftFolder(f model.Folder, by int) {
	siblings, i := a.folderSiblings(f)
	if i < 0 || i+by < 0 || i+by >= len(siblings) {
		return
	}
	siblings[i], siblings[i+by] = siblings[i+by], siblings[i]
	for n, v := range siblings {
		if v.SortIndex != n {
			v.SortIndex = n
			if e := a.P.SaveFolder(v); e != nil {
				a.fail(e)
				break
			}
		}
	}
	a.reload()
}

func (a *App) moveHost(cc model.Connection, folder string) {
	cc.FolderID = folder
	a.report(a.P.SaveConnection(cc, nil), "")
	a.reload()
}
func (a *App) trashHosts(cs []model.Connection) {
	var done []model.Connection
	for _, v := range cs {
		v.Deleted = true
		if e := a.P.SaveConnection(v, nil); e != nil {
			a.fail(e)
			break
		}
		a.hosts.picked.Remove(v.ID)
		done = append(done, v)
	}
	a.reload()
	if len(done) == 0 {
		return
	}
	title := "已移入回收站：" + done[0].Name
	if len(done) > 1 {
		title = fmt.Sprintf("已将 %d 个主机移入回收站", len(done))
	}
	a.addToast(ui.Toast{Title: title, Action: "撤销", OnAction: func() {
		for _, v := range done {
			v.Deleted = false
			if e := a.P.SaveConnection(v, nil); e != nil {
				a.fail(e)
				break
			}
		}
		a.reload()
	}})
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
		siblings, i := a.folderSiblings(f)
		if m.Item("上移").Disabled(i <= 0).Chosen() {
			a.later(func() { a.shiftFolder(f, -1) })
		}
		if m.Item("下移").Disabled(i >= len(siblings)-1).Chosen() {
			a.later(func() { a.shiftFolder(f, 1) })
		}
		if m.Item("批量绑定认证…").Chosen() {
			a.later(func() {
				a.openBatch("「"+a.folderPath(f.ID)+"」及子目录", hostIDs(a.folderHosts(f.ID)), "")
			})
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
func hostIDs(cs []model.Connection) []string {
	ids := make([]string, len(cs))
	for i, c := range cs {
		ids[i] = c.ID
	}
	return ids
}

// Host editor.

// newHost and editHost open the form in the connection manager, after asking about the edits of the
// form that is open. A new host starts with the authentication the last saved one used.
func (a *App) newHost(folder string) {
	c := model.Connection{Auth: "password", Encoding: "utf-8", Port: 22, FolderID: folder, Monitor: true}
	if a.P != nil {
		last := strings.Split(a.P.UIState("lastAuth"), "\n")
		if len(last) == 3 && contains(authKinds, last[0]) {
			c.Auth = last[0]
			if _, ok := a.key(last[1]); ok {
				c.KeyID = last[1]
			}
			if _, ok := a.credential(last[2]); ok {
				c.CredentialID = last[2]
			}
			if c.Auth == "key" && c.KeyID == "" {
				c.Auth = "password"
			}
		}
	}
	a.showHostForm(hostEditor{open: true, edit: c, port: "22", keepaliveMode: keepaliveGlobal, keepalive: strconv.Itoa(model.DefaultKeepaliveSeconds), inheritOrig: true}, "")
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
	a.showHostForm(e, c.ID)
}

// showHostForm shows e in the manager's host column, selecting host if there is one; with unsaved edits in
// the form that is open, it first asks whether to drop them.
func (a *App) showHostForm(e hostEditor, host string) {
	a.openManager()
	a.hosts.page = managerHosts
	a.confirmDiscard(func() {
		h := &a.hosts
		if host != "" { // the tile of the host being edited is the chosen one, so choosing another is a change
			h.selected = host
			h.picked.Clear()
			if h.chosen = slices.IndexFunc(a.shownHosts(), func(cc model.Connection) bool { return cc.ID == host }); h.chosen >= 0 {
				h.picked.Add(host)
			}
		}
		h.editor = e
	})
}

// confirmDiscard runs next at once, or, when the host form holds edits that are not saved, after the user
// agrees to drop them. The question shows in the manager, where the edits are.
func (a *App) confirmDiscard(next func()) {
	e := &a.hosts.editor
	if !e.dirty() {
		next()
		return
	}
	name := orDefault(strings.TrimSpace(e.edit.Name), "新主机")
	a.confirmWith("放弃未保存的修改？", "「"+name+"」的修改还没有保存，放弃后无法恢复。", "放弃", "继续编辑", next)
	a.dialogs.pendingIn = managerWindow
}

// cancelHostEdit closes the host form, unless the user wants to keep editing.
func (a *App) cancelHostEdit() {
	a.confirmDiscard(func() { a.hosts.editor.open = false })
}

// ManagerMayClose guards closing the connection manager window: with unsaved edits it asks in the window
// and returns false; the host closes the window again through HideManager once they are dropped.
func (a *App) ManagerMayClose() bool {
	e := &a.hosts.editor
	if !e.dirty() {
		e.open = false
		return true
	}
	a.confirmDiscard(func() {
		e.open = false
		if a.HideManager != nil {
			a.HideManager()
		}
	})
	if a.wakeManager != nil {
		a.wakeManager()
	}
	return false
}

// bindAuth clears the key of a host that no longer logs in with one, and ends the wait of an
// imported host for its key once it has one.
func bindAuth(cc *model.Connection) {
	switch {
	case cc.Auth != "key":
		cc.KeyID = ""
	case cc.KeyID != "":
		cc.BindKey(cc.KeyID)
	}
}

// draft turns the form's fields into a connection.
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
	bindAuth(&cc)
	return cc, cc.Validate()
}

// saveHost saves the form's host and reports whether it did.
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
	a.report(a.P.SaveUIState("lastAuth", cc.Auth+"\n"+cc.KeyID+"\n"+cc.CredentialID), "")
	e.password = ""
	e.open = false
	a.hosts.selected = cc.ID
	a.reload()
	a.toast("已保存主机：" + cc.Name)
	return cc, true
}

// hostForm is the host editor, in the order of the 1.x form: the address, then who logs in and how,
// then the folded sections. All its fields are in one form, so they share one column of labels and their
// inputs start at the same edge; the column is as wide as the widest label showing, so no label is longer
// than four characters, and it keeps its width as the sections open. Its buttons are hostFooter's.
func (a *App) hostForm(c *ui.Context) {
	e := &a.hosts.editor
	t := c.Theme()
	e.enter = false
	title := "新建主机"
	if e.edit.ID != "" {
		title = "编辑主机"
	}
	ui.Text(c, title).FontSize(17).Bold()
	ui.Form(c, func() {
		ui.Field(c, "名称", func() {
			if ui.TextInput(c, &e.edit.Name).Label("名称").Placeholder("prod-web-01").AutoFocus().Grow(1).Submitted() {
				e.enter = true
			}
		}).FillWidth()
		field(c, "主机", &e.edit.Host, &e.enter)
		field(c, "端口", &e.port, &e.enter)
		a.credentialPick(c, &e.edit.CredentialID)
		if cr, ok := a.credential(e.edit.CredentialID); ok {
			ui.Field(c, "用户名", func() { ui.Text(c, cr.User).TextColor(t.TextMuted) }).FillWidth()
			ui.Field(c, "认证", func() { ui.Text(c, authLabel(a, cr.Auth, cr.KeyID)).TextColor(t.TextMuted) }).Description("来自凭据，修改凭据即对所有使用它的主机生效").FillWidth()
		} else {
			field(c, "用户名", &e.edit.User, &e.enter)
			a.authFields(c, e)
		}
		a.folderPick(c, "目录", &e.edit.FolderID)
		disclosure(c, "安全", &e.more[0], func() {
			ui.Field(c, "已知主机", func() { ui.Switch(c, &e.edit.UseKnownHosts).Label("已知主机") }).Description("同时使用 ~/.ssh/known_hosts 校验").FillWidth()
			field(c, "主机指纹", &e.edit.Fingerprint, &e.enter).Description("可选，固定后只信任这个指纹，如 SHA256:…")
		})
		disclosure(c, "网络", &e.more[1], func() {
			a.proxyPick(c, &e.edit.ProxyID, "")
			ui.Field(c, "心跳", func() {
				if ui.Select(c, &e.keepaliveMode, []string{keepaliveGlobal, keepaliveCustom, keepaliveOff}).Label("心跳").Grow(1).Changed() {
					e.inheritOrig = false
				}
			}).FillWidth()
			if e.keepaliveMode == keepaliveCustom {
				field(c, "间隔", &e.keepalive, &e.enter).Description("单位秒，1–3600")
			}
		})
		disclosure(c, "终端", &e.more[2], func() {
			choice(c, "字符编码", &e.edit.Encoding, []string{"utf-8", "gb18030", "gbk", "big5", "shift_jis"})
			pick(c, "退格键", "退格（^H）", &e.edit.Backspace, []string{"ascii-delete"}, []string{"删除（^?）"})
			pick(c, "删除键", "删除（ESC [3~）", &e.edit.DeleteMode, []string{"ascii-delete", "ascii-backspace"}, []string{"删除（^?）", "退格（^H）"})
			toggle(c, "监控状态", &e.edit.Monitor)
		})
		disclosure(c, "标签与备注", &e.more[3], func() {
			field(c, "标签", &e.tags, &e.enter).Description("用逗号分隔")
			toggle(c, "收藏", &e.edit.Favorite)
			swatches(c, "颜色", &e.edit.Color, append([]string{""}, hostColors...), append([]string{"自动"}, hostColorNames...), append([]string{""}, hostColors...))
			ui.Field(c, "备注", func() { ui.TextArea(c, &e.edit.Notes).Label("备注").Height(60).Grow(1) }).FillWidth()
		})
	}).FillWidth().Gap(FormGap)
	if e.orig == nil { // what the controls made of the values on their first frame is what the form opened with
		form := e.form()
		e.orig = &form
	}
}

// hostFooter is the form's buttons, below the scrolling fields: 保存 is also ⌘S and Enter in a field.
func (a *App) hostFooter(c *ui.Context) {
	e := &a.hosts.editor
	t := c.Theme()
	ui.Column(c).FillWidth().Padding(10, 18).Gap(8).BorderColor(t.Border).BorderWidth(1, 0, 0, 0).Children(func() {
		if e.err != "" {
			ui.Text(c, e.err).FontSize(12).TextColor(t.Danger).FillWidth()
		}
		ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Clicked() {
				a.cancelHostEdit()
			}
			save := ui.Button(c, "保存").KeyShortcuts(ui.Cmd, ui.KeyS).Clicked()
			if save || e.enter || c.Shortcut(ui.Cmd, ui.KeyS) {
				a.saveHost()
			}
			if ui.PrimaryButton(c, "保存并连接").Clicked() {
				if cc, ok := a.saveHost(); ok {
					a.connect(cc)
				}
			}
		})
	})
}

// disclosure is ui.Collapsible with its panel at the left edge of the form around it, not indented, so
// the fields inside share the form's label column and their inputs line up with those above it.
func disclosure(c *ui.Context, title string, open *bool, fn func()) {
	t := c.Theme()
	ui.Column(c.Key("section-" + title)).FillWidth().Children(func() { // keyed: the fields above come and go
		p := ui.CollapsibleBase(c, open)
		tr := p.Trigger.AlignSelf(ui.Start).AlignItems(ui.Center).Gap(4).Padding(4, 8, 4, 2).Radius(t.Radius)
		if tr.Hovered() {
			tr.Background(t.SurfaceHover)
		}
		tr.Children(func() {
			icon := "chevron-right"
			if *open {
				icon = "chevron-down"
			}
			ui.Icon(c, appIcons[icon]).FontSize(14).TextColor(t.TextMuted)
			ui.Text(c, title).SingleLine()
		})
		p.Panel(fn).Padding(FormGap, 0, 0, 0).Gap(FormGap)
	})
}

var authKinds = []string{"password", "key", "agent", "keyboard"}

func authKind(c *ui.Context, auth *string) {
	names := []string{"密码", "密钥", "SSH Agent", "键盘交互"}
	chosen := names[max(0, indexOf(authKinds, *auth))]
	choice(c, "认证方式", &chosen, names)
	*auth = authKinds[indexOf(names, chosen)]
}

// authLabel names an authentication, with its key.
func authLabel(a *App, auth, keyID string) string {
	s := authNames[auth]
	if auth == "key" {
		if k, ok := a.key(keyID); ok {
			s += " · " + k.Name
		} else {
			s += " · 缺少密钥"
		}
	}
	return s
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

// keyPick chooses a key, with 新建 / 生成 beside it: the key made there is chosen.
func (a *App) keyPick(c *ui.Context, id *string) {
	var ids, names []string
	for _, k := range a.Data.Keys {
		ids = append(ids, k.ID)
		names = append(names, k.Name)
	}
	ui.Field(c, "密钥", func() {
		ui.Row(c).Grow(1).MinWidth(0).Gap(6).AlignItems(ui.Center).Children(func() {
			pickInline(c, "密钥", "（未选择）", id, ids, names)
			if ui.Button(c, "新建 / 生成…").Clicked() {
				a.openKeyDialog(id)
			}
		})
	}).FillWidth()
}

// proxyPick chooses a proxy, with 新建代理 beside it; keepLabel, when set, names a first choice that
// leaves each host's proxy alone.
func (a *App) proxyPick(c *ui.Context, id *string, keepLabel string) {
	ids, names := []string{}, []string{}
	for _, p := range a.Data.Proxies {
		ids = append(ids, p.ID)
		names = append(names, p.Name)
	}
	ui.Field(c, "代理", func() {
		ui.Row(c).Grow(1).MinWidth(0).Gap(6).AlignItems(ui.Center).Children(func() {
			if keepLabel == "" {
				pickInline(c, "代理", "不使用代理", id, ids, names)
			} else {
				keepChoice(id, func(v *string) {
					pickInline(c, "代理", keepLabel, v, append([]string{none}, ids...), append([]string{"不使用代理"}, names...))
				})
			}
			if ui.Button(c, "新建代理…").Clicked() {
				a.openProxyDialog(model.Proxy{Kind: "socks5"}, id)
			}
		})
	}).FillWidth()
}

// none stands for an empty ID in a choice whose empty value is keep.
const none = "\x00none"

// keepChoice runs fn on *id with keep as the empty choice, and "" as none.
func keepChoice(id *string, fn func(v *string)) {
	v := *id
	switch v {
	case keep:
		v = ""
	case "":
		v = none
	}
	fn(&v)
	switch v {
	case "":
		*id = keep
	case none:
		*id = ""
	default:
		*id = v
	}
}

// credentialPick chooses the credential a host uses, or none, with 新建凭据 beside it.
func (a *App) credentialPick(c *ui.Context, id *string) {
	var ids, names []string
	for _, cr := range a.Data.Credentials {
		ids = append(ids, cr.ID)
		names = append(names, cr.Name)
	}
	ui.Field(c, "使用凭据", func() {
		ui.Row(c).Grow(1).MinWidth(0).Gap(6).AlignItems(ui.Center).Children(func() {
			pickInline(c, "使用凭据", "不使用凭据", id, ids, names)
			if ui.Button(c, "新建凭据…").Clicked() {
				a.openCredentialDialog(model.Credential{Auth: "password"}, id)
			}
		})
	}).FillWidth()
}

// pickInline is pick's Select alone, for a field that has a button beside it.
func pickInline(c *ui.Context, label, none string, id *string, ids, names []string) {
	chosen := none
	opts := []string{none}
	for i, v := range ids {
		n := names[i]
		if contains(opts, n) {
			n += " [" + v[:min(6, len(v))] + "]"
		}
		opts = append(opts, n)
		if *id == v {
			chosen = n
		}
	}
	ui.Select(c, &chosen, opts).Label(label).Grow(1).MinWidth(0)
	*id = ""
	for i, n := range opts[1:] {
		if n == chosen {
			*id = ids[i]
		}
	}
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

func (a *App) editFolder(f model.Folder) {
	a.openManager()
	a.hosts.page = managerHosts
	a.hosts.folders = folderEditor{open: true, folder: f}
}
func (a *App) folderDialog(c *ui.Context) {
	e := &a.hosts.folders
	if !e.open {
		return
	}
	enter := false
	dialog(c, &e.open, func() {
		ui.Column(c).Width(400).Gap(16).Children(func() {
			title := "新建目录"
			if e.folder.ID != "" {
				title = "编辑目录"
			}
			ui.Text(c, title).FontSize(17).Bold()
			ui.Form(c, func() {
				ui.Field(c, "名称", func() { enter = ui.TextInput(c, &e.folder.Name).Label("目录名称").AutoFocus().Grow(1).Submitted() }).FillWidth()
				a.folderPick(c, "上级目录", &e.folder.ParentID)
			}).FillWidth().Gap(FormGap)
			if e.err != "" {
				ui.Text(c, e.err).TextColor(c.Theme().Danger)
			}
			ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "取消").Clicked() {
					e.open = false
				}
				if ui.PrimaryButton(c, "保存目录").Clicked() || enter || c.Shortcut(ui.Cmd, ui.KeyS) {
					a.saveFolder()
				}
			})
		})
	})
}
func (a *App) saveFolder() {
	e := &a.hosts.folders
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

// Batch binding.

// openBatch opens the batch dialog for the hosts ids, described by title; auth preselects a method.
func (a *App) openBatch(title string, ids []string, auth string) {
	a.openManager()
	if auth == "" {
		auth = keep
	}
	a.hosts.batch = batchAuth{open: true, title: title, ids: ids, credential: keep, auth: auth, proxy: keep}
}

// batched is cc after the batch dialog's changes: a credential, else an authentication of its own.
func (b *batchAuth) batched(cc model.Connection) model.Connection {
	if b.credential != keep {
		cc.CredentialID = b.credential
	}
	if b.auth != keep && (b.credential == keep || b.credential == "") {
		cc.Auth, cc.KeyID = b.auth, b.key
		bindAuth(&cc)
	}
	if b.proxy != keep {
		cc.ProxyID = b.proxy
	}
	return cc
}

// summary describes how cc logs in, for the batch preview.
func (a *App) summary(cc model.Connection) string {
	if cr, ok := a.credential(cc.CredentialID); ok {
		return "凭据 " + cr.Name
	}
	return authLabel(a, cc.Auth, cc.KeyID)
}

func (a *App) batchDialog(c *ui.Context) {
	b := &a.hosts.batch
	if !b.open {
		return
	}
	var targets []model.Connection
	for _, id := range b.ids {
		if cc, ok := a.host(id); ok && !cc.Deleted {
			targets = append(targets, cc)
		}
	}
	t := c.Theme()
	dialog(c, &b.open, func() {
		ui.Column(c).Width(560).Gap(14).Children(func() {
			ui.Text(c, "批量绑定认证").FontSize(17).Bold()
			muted(c, fmt.Sprintf("将应用到%s的 %d 个主机", b.title, len(targets)))
			ui.Form(c, func() {
				ids, names := []string{none}, []string{"不使用凭据"}
				for _, cr := range a.Data.Credentials {
					ids = append(ids, cr.ID)
					names = append(names, cr.Name)
				}
				keepChoice(&b.credential, func(v *string) { pick(c, "使用凭据", "保持不变", v, ids, names) })
				if b.credential == keep || b.credential == "" {
					kinds := append([]string{keep}, authKinds...)
					i := max(0, indexOf(kinds, b.auth))
					ui.Field(c, "认证方式", func() {
						if ui.Segmented(c, &i, "保持不变", "密码", "密钥", "SSH Agent", "键盘交互").FillEqually().Label("认证方式").Changed() {
							b.auth = kinds[i]
						}
					}).FillWidth()
					switch b.auth {
					case "password":
						password(c, "密码", &b.pass).Description("留空则保留各主机已保存的密码")
					case "key":
						a.keyPick(c, &b.key)
					}
				}
				a.proxyPick(c, &b.proxy, "保持不变")
			}).FillWidth().Gap(FormGap)
			muted(c, "预览")
			ui.Scroll(c).FillWidth().MaxHeight(180).Children(func() {
				ui.Column(c).FillWidth().Gap(2).Children(func() {
					for _, cc := range targets {
						after := b.batched(cc)
						change := a.summary(cc)
						if now := a.summary(after); now != change {
							change += " → " + now
						}
						if after.ProxyID != cc.ProxyID {
							change += "，代理 → " + orDefault(a.proxyName(after.ProxyID), "不使用")
						}
						ui.Row(c.Key("batch-" + cc.ID)).FillWidth().Gap(8).Children(func() {
							ui.Text(c, cc.Name).FontSize(12).Width(140).SingleLine()
							ui.Text(c, change).FontSize(12).TextColor(t.TextMuted).SingleLine().Grow(1).MinWidth(0)
						})
					}
				})
			})
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
func (a *App) proxyName(id string) string {
	for _, p := range a.Data.Proxies {
		if p.ID == id {
			return p.Name
		}
	}
	return ""
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
	if b.auth == "key" && b.key == "" && (b.credential == keep || b.credential == "") {
		a.fail(fmt.Errorf("请选择密钥"))
		return
	}
	for _, cc := range targets {
		cc = b.batched(cc)
		var secret *string
		if b.auth == "password" && b.pass != "" && (b.credential == keep || b.credential == "") {
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
func (a *App) viaProxy(id string) bool {
	h, ok := a.host(id)
	return ok && h.ProxyID != ""
}

// Authentication retry.

// retryAuth opens the retry dialog for host id after its login failed with message.
func (a *App) retryAuth(id, message string) {
	if cc, ok := a.host(id); ok && !cc.Deleted {
		a.hosts.retry = authRetry{open: true, host: cc, message: message}
	}
}

func (a *App) retryDialog(c *ui.Context) {
	r := &a.hosts.retry
	if !r.open {
		return
	}
	t := c.Theme()
	dialog(c, &r.open, func() {
		ui.Column(c).Width(480).Gap(14).Children(func() {
			ui.Text(c, "登录失败："+r.host.Name).FontSize(17).Bold()
			title, _ := humanize(r.message)
			muted(c, title+"。可以换一个用户、认证方式或密钥再试，修改会保存到这个主机。")
			ui.Form(c, func() {
				a.credentialPick(c, &r.host.CredentialID)
				if cr, ok := a.credential(r.host.CredentialID); ok {
					ui.Field(c, "用户名", func() { ui.Text(c, cr.User).TextColor(t.TextMuted) }).FillWidth()
					ui.Field(c, "认证", func() { ui.Text(c, authLabel(a, cr.Auth, cr.KeyID)).TextColor(t.TextMuted) }).FillWidth()
					return
				}
				field(c, "用户名", &r.host.User)
				authKind(c, &r.host.Auth)
				switch r.host.Auth {
				case "password":
					password(c, "密码", &r.password).Description("留空则在连接时询问")
				case "key":
					a.keyPick(c, &r.host.KeyID)
				}
			}).FillWidth().Gap(FormGap)
			ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "取消").Clicked() {
					r.open = false
				}
				if ui.PrimaryButton(c, "保存并重试").Clicked() {
					a.saveRetry()
				}
			})
		})
	})
}
func (a *App) saveRetry() {
	r := &a.hosts.retry
	cc := r.host
	bindAuth(&cc)
	var secret *string
	if cc.CredentialID == "" && cc.Auth == "password" && r.password != "" {
		secret = &r.password
	}
	if e := a.P.SaveConnection(cc, secret); e != nil {
		a.fail(e)
		return
	}
	r.password = ""
	r.open = false
	a.reload()
	a.connect(cc)
}
