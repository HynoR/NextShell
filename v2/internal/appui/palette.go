package appui

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/model"
)

type paletteState struct {
	open            bool
	query, previous string
	selected        int
	scroll          bool // the keys moved the selection: show it
}

func (p *paletteState) show() {
	p.open = true
	p.query = ""
	p.selected = 0
}

// paletteItem is one result; run gets the modifiers held with Enter.
type paletteItem struct {
	group, title, detail string
	run                  func(mods ui.Modifiers)
}

// fuzzy reports whether every rune of q appears in s in order, ignoring case.
func fuzzy(s, q string) bool {
	s = strings.ToLower(s)
	for _, r := range strings.ToLower(q) {
		if unicode.IsSpace(r) {
			continue
		}
		i := strings.IndexRune(s, r)
		if i < 0 {
			return false
		}
		s = s[i+len(string(r)):]
	}
	return true
}

func (a *App) paletteItems() []paletteItem {
	q := strings.TrimSpace(a.palette.query)
	var out []paletteItem
	add := func(group, title, detail, haystack string, run func(ui.Modifiers)) {
		if q == "" || fuzzy(title+" "+haystack, q) {
			out = append(out, paletteItem{group, title, detail, run})
		}
	}
	if strings.Contains(q, "@") {
		if cc, e := model.QuickConnect(q); e == nil {
			out = append(out, paletteItem{"连接", "连接 " + q, "密码会在随后的对话框里询问", func(ui.Modifiers) { a.connect(cc) }})
		}
	}
	// An open tab comes before the host it is a session of, so Enter switches to it instead of opening another.
	for i, s := range a.Tabs {
		add("标签", s.Title, a.tabTooltip(s), s.Connection.Name+" "+s.Connection.Host, func(ui.Modifiers) { a.activateTab(i) })
	}
	limit := 0
	for _, cc := range a.hosts.recentHosts(a, len(a.Data.Connections)) {
		if limit >= 30 {
			break
		}
		before := len(out)
		add("主机", cc.Name, a.login(cc), cc.Host+" "+a.resolved(cc).User+" "+strings.Join(cc.Tags, " ")+" "+a.folderPath(cc.FolderID), func(mods ui.Modifiers) {
			a.connect(cc)
			if mods&ui.Cmd != 0 {
				a.showBench(benchFiles)
			}
		})
		if len(out) > before {
			limit++
		}
	}
	for _, v := range a.Data.Commands {
		add("命令片段", v.Name, firstLine(v.Text), v.Group+" "+v.Description+" "+v.Text, func(mods ui.Modifiers) { a.runSnippet(v, mods&ui.Alt != 0) })
	}
	actions := []struct {
		title string
		run   func()
	}{
		{"新建主机", func() { a.newHost("") }},
		{"新建目录", func() { a.editFolder(model.Folder{}) }},
		{"打开本地终端", a.P.Local},
		{"连接管理器", a.openManager},
		{"打开设置", func() { a.openSettings("") }},
		{"切换深浅色", a.toggleAppearance},
		{"导入连接", func() { a.openImport("") }},
		{"导出连接", func() { a.openExport(nil) }},
		{"密钥管理", func() { a.openManagerPage(managerKeys) }},
		{"代理管理", func() { a.openManagerPage(managerProxies) }},
		{"凭据管理", func() { a.openManagerPage(managerCredentials) }},
		{"Agent 与 MCP", func() { a.openSettings("Agent") }},
		{"回收站", func() { a.openSettings("回收站") }},
		{"网络工具（nexttrace 路径）", func() { a.openSettings("网络工具") }},
		{"发送命令…", func() { a.snippets.commandOpen = true }},
		{"新建命令片段", func() { a.editSnippet(model.Command{Group: "默认"}) }},
		{"显示 / 隐藏左栏", func() { a.layout.leftHidden = !a.layout.leftHidden }},
		{"显示 / 隐藏底部工作区", func() { a.layout.benchHidden = !a.layout.benchHidden }},
		{"显示 / 隐藏活动栏", func() { a.layout.rightOpen = !a.layout.rightOpen }},
		{"SFTP", func() { a.showBench(benchFiles) }},
		{"命令库", func() { a.showBench(benchSnippets) }},
		{"系统信息", func() { a.showBench(benchSysinfo) }},
		{"传输队列", func() { a.layout.rightOpen = true }},
	}
	if a.Cloud {
		actions = append(actions, struct {
			title string
			run   func()
		}{"云同步", func() { a.openSettings("同步") }})
	}
	if len(a.editor.documents) > 0 {
		actions = append(actions, struct {
			title string
			run   func()
		}{"远程编辑器", func() { a.editor.open = true }})
	}
	if s := a.current(); s != nil {
		actions = append(actions, []struct {
			title string
			run   func()
		}{
			{"复制会话", func() { a.P.CloneSession(s) }},
			{"重新连接", func() { a.reconnect(s) }},
			{"重命名标签", func() { a.startRename(s) }},
			{"关闭标签", func() { a.closeTabID(s.ID) }},
			{"清屏", func() { s.Terminal.ClearScreen() }},
			{"清除滚动缓冲", func() { s.Terminal.ClearScrollback() }},
		}...)
		if !s.Local && a.Prefs.ShellIntegration != "off" {
			for _, shell := range []string{"bash", "zsh"} {
				actions = append(actions, struct {
					title string
					run   func()
				}{"插入目录跟踪脚本（" + shell + "）", func() { a.insertShellHook(s, shell) }})
			}
		}
	}
	for _, v := range actions {
		add("操作", v.title, actionKeys[v.title], "", func(ui.Modifiers) { v.run() })
	}
	return out
}

// actionKeys are the shortcuts the palette shows beside the actions that have one. The letters take appMod
// (⌘ on macOS, Ctrl+Shift elsewhere: a plain Ctrl+letter belongs to the shell); the comma takes ⌘ or Ctrl.
var actionKeys = map[string]string{
	"打开本地终端":       appShortcutLabel("T"),
	"连接管理器":        appShortcutLabel("O"),
	"打开设置":         shortcutLabel(","),
	"显示 / 隐藏左栏":    appShortcutLabel("B"),
	"显示 / 隐藏底部工作区": appShortcutLabel("J"),
}

func appShortcutLabel(key string) string {
	if appMod == ui.Cmd {
		return shortcutLabel(key)
	}
	return "Ctrl+Shift+" + key
}

func (a *App) toggleAppearance() {
	p := a.Prefs
	if p.Appearance == "深色" {
		p.Appearance = "浅色"
	} else {
		p.Appearance = "深色"
	}
	a.savePreferences(p)
}

// insertShellHook types the directory hook into the session for the user to review and run.
func (a *App) insertShellHook(s *Session, shell string) {
	command, e := a.P.ShellCommand(shell)
	if e != nil {
		a.fail(e)
		return
	}
	a.report(a.P.Send(s.ID, strings.TrimSpace(command)), "已插入脚本，检查后按回车执行；文件面板即可跟随终端目录")
	a.tabs.term.Focus()
}

func (a *App) paletteView(c *ui.Context, anchor ui.Element) {
	p := &a.palette
	if !p.open {
		return
	}
	t := c.Theme()
	if p.query != p.previous {
		p.previous, p.selected = p.query, 0
	}
	items := a.paletteItems()
	p.selected = max(0, min(p.selected, len(items)-1))
	run := func(i int, mods ui.Modifiers) {
		if i < 0 || i >= len(items) {
			return
		}
		p.open = false
		item := items[i]
		a.later(func() { item.run(mods) })
	}
	ui.PopoverBase(c, anchor, &p.open, func(panel ui.Element) {
		panel.AttachTo(anchor, ui.AnchorBottomEnd, ui.AnchorTopEnd).Margin(6, 0, 0, 0).Width(560).Padding(8).Gap(6).Radius(10).Background(t.Surface).Border(1, t.Border).Shadow(0, 12, 32, 0, ui.RGBA(0, 0, 0, .3))
		ui.TextInput(c, &p.query).Label("命令面板").Placeholder("搜索主机、标签、片段和操作，或输入 user@host").FillWidth().Height(32).AutoFocus().OnTextKey(func(k ui.TextKey) bool {
			switch k.Key {
			case ui.KeyDown:
				p.selected, p.scroll = min(p.selected+1, len(items)-1), true
			case ui.KeyUp:
				p.selected, p.scroll = max(p.selected-1, 0), true
			case ui.KeyEnter:
				run(p.selected, k.Mods)
			case ui.KeyEscape:
				p.open = false
			default:
				return false
			}
			return true
		})
		ui.Scroll(c).MaxHeight(420).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Gap(1).Children(func() {
				if len(items) == 0 {
					muted(c, "没有结果").Padding(8)
				}
				group := ""
				for i, it := range items {
					if it.group != group {
						group = it.group
						muted(c, group).Padding(6, 8, 2, 8)
					}
					row := ui.ButtonBase(c.Key(fmt.Sprint("palette-", i))).Label(it.title).FillWidth().Padding(6, 8).Gap(8).Radius(6).AlignItems(ui.Center)
					if i == p.selected {
						row.Background(t.Accent.Alpha(.15))
						if p.scroll {
							row.ScrollIntoView()
							p.scroll = false
						}
					} else if row.Hovered() {
						row.Background(t.SurfaceHover)
					}
					row.Children(func() {
						ui.Text(c, it.title).SingleLine().Grow(1).MinWidth(0)
						if it.detail != "" {
							ui.Text(c, it.detail).FontSize(12).TextColor(t.TextMuted).SingleLine().MaxWidth(240)
						}
					})
					if row.Clicked() {
						run(i, row.ClickModifiers())
					}
				}
			})
		})
		muted(c, "回车执行 · "+shortcutLabel("回车")+" 连接并打开 SFTP · Alt+回车 片段发送到全部会话").Padding(2, 4)
	})
}
