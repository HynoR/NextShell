package appui

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/model"
)

// settingsState backs the settings window. Preferences apply as they change:
// the text fields are parsed every frame and saved once valid.
type settingsState struct {
	section                                 string
	draft                                   model.Preferences
	size, lineHeight, scrollback, keepalive string
	err                                     string
	hosts                                   map[string]bool // chosen for export and cloud copy
	trash                                   map[string]bool // chosen in the recycle bin

	cloud cloudState
	agent agentState
}

// settingsSections follow the 1.x settings.
var settingsSections = []struct{ id, icon string }{
	{"窗口行为", "panel-left"}, {"文件传输", "transfer"}, {"远端编辑", "edit"}, {"终端", "terminal"},
	{"网络工具", "diagnostics"}, {"云同步", "cloud"}, {"回收站", "trash"}, {"Agent", "agent"}, {"关于", "info"},
}

func (s *settingsState) load(v model.Preferences) {
	s.draft = v
	s.size = strconv.Itoa(v.FontSize)
	s.lineHeight = strconv.FormatFloat(float64(v.LineHeight), 'f', -1, 32)
	s.scrollback = strconv.Itoa(v.ScrollbackMiB)
	s.keepalive = strconv.Itoa(v.KeepaliveSeconds)
	if s.section == "" {
		s.section = "窗口行为"
	}
	if s.hosts == nil {
		s.hosts = map[string]bool{}
		s.cloud.endpoint = "http://127.0.0.1:41888"
		s.cloud.name = "我的工作区"
	}
}
func (s *settingsState) parse() (model.Preferences, error) {
	p := s.draft
	var err error
	if p.FontSize, err = strconv.Atoi(strings.TrimSpace(s.size)); err != nil {
		return p, fmt.Errorf("字号须为整数")
	}
	line, err := strconv.ParseFloat(strings.TrimSpace(s.lineHeight), 32)
	if err != nil {
		return p, fmt.Errorf("行高须为数字")
	}
	p.LineHeight = float32(line)
	if p.ScrollbackMiB, err = strconv.Atoi(strings.TrimSpace(s.scrollback)); err != nil {
		return p, fmt.Errorf("滚动缓冲须为整数")
	}
	if p.KeepaliveSeconds, err = strconv.Atoi(strings.TrimSpace(s.keepalive)); err != nil {
		return p, fmt.Errorf("心跳间隔须为整数")
	}
	if p.ScrollbackMiB < 1 || p.ScrollbackMiB > 256 {
		return p, fmt.Errorf("滚动缓冲须为 1–256 MiB")
	}
	if p.KeepaliveSeconds < 5 || p.KeepaliveSeconds > 600 {
		return p, fmt.Errorf("心跳间隔须为 5–600 秒")
	}
	return p, p.Validate()
}
func (s *settingsState) selectHosts(ids ...string) {
	s.hosts = map[string]bool{}
	for _, id := range ids {
		s.hosts[id] = true
	}
}
func (s *settingsState) handle(a *App, e Event) {
	if e.Import != nil {
		d := &a.hosts.records.data
		d.preview, d.importOpen = e.Import, true
		a.openManager()
	}
	if strings.HasPrefix(e.Message, "已导入 ") {
		a.hosts.records.data.preview, a.hosts.records.data.importOpen = nil, false
	}
	if e.WorkspaceID != "" {
		s.cloud.workspace = e.WorkspaceID
		s.cloud.confirmRemote = false
	}
}

// TerminalThemes uses official terminal palettes and preserves ANSI colors.
func TerminalThemes(v model.Preferences) (*terminal.Theme, *terminal.Theme) {
	if v.TerminalTheme == "跟随外观" {
		return terminalLight, terminalDark
	}
	bg, fg := v.Background, v.Foreground
	switch v.TerminalTheme {
	case "Default":
		bg, fg = "#000000", "#d8eaff"
	case "Dracula":
		bg, fg = "#282a36", "#f8f8f2"
	case "Solarized Dark":
		bg, fg = "#002b36", "#93a1a1"
	case "Gruvbox Dark":
		bg, fg = "#282828", "#ebdbb2"
	case "Nord":
		bg, fg = "#2e3440", "#d8dee9"
	}
	t := *terminalDark
	t.Background = ui.Hex(bg)
	t.Foreground = ui.Hex(fg)
	t.Cursor = t.Foreground
	t.CursorText = t.Background
	return &t, &t
}

func (a *App) savePreferences(p model.Preferences) {
	if e := a.P.SavePreferences(p); e != nil {
		a.fail(e)
		return
	}
	a.Prefs = p
	a.settings.draft = p
	l, d := TerminalThemes(p)
	for _, s := range a.Tabs {
		s.Terminal.SetTheme(l, d)
	}
	for _, wake := range []func(){a.wakeMain, a.wakeManager, a.wakeEditor} { // the main color and appearance apply at once
		if wake != nil {
			wake()
		}
	}
}

// SettingsView builds the settings window.
func (a *App) SettingsView(c *ui.Context) {
	a.view = settingsWindow
	defer func() { a.view = mainWindow }()
	a.theme(c)
	a.wakeSettings = c.Services().Invalidate
	t := c.Theme()
	s := &a.settings
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Width(180).FillHeight().Padding(8).Background(t.Surface).BorderColor(t.Border).BorderWidth(0, 1, 0, 0).Children(func() {
			ui.Sidebar(c.Key("settings-nav"), &s.section, func() {
				for _, v := range settingsSections {
					if v.id == "云同步" && !a.Cloud {
						continue
					}
					ui.SidebarItem(c, v.id, appIcons[v.icon], v.id)
				}
			}).Label("设置分类").Grow(1).FillWidth()
		})
		ui.Scroll(c.Key("settings-" + s.section)).Grow(1).FillHeight().Children(func() {
			ui.Column(c).FillWidth().MaxWidth(680).Padding(24, 28).Gap(16).Children(func() {
				ui.Text(c, s.section).FontSize(20).Bold()
				ui.Form(c, func() { a.settingsSection(c) }).FillWidth().Gap(FormGap)
			})
		})
	})
	if p, err := s.parse(); err != nil {
		s.err = err.Error()
	} else {
		s.err = ""
		if p != a.Prefs {
			a.savePreferences(p)
		}
	}
	a.confirmView(c, settingsWindow)
	a.detailView(c, settingsWindow)
	a.runActions(c)
	a.flushToasts(c, settingsWindow)
	if a.wakeMain != nil {
		a.wakeMain()
	}
}

func (a *App) settingsSection(c *ui.Context) {
	switch a.settings.section {
	case "窗口行为":
		a.windowSettings(c)
	case "文件传输":
		a.transferSettings(c)
	case "远端编辑":
		a.editSettings(c)
	case "终端":
		a.terminalSettings(c)
	case "云同步":
		a.cloudSettings(c)
	case "Agent":
		a.agentSettings(c)
	case "回收站":
		a.trashSettings(c)
	case "网络工具":
		a.diagnosticSettings(c)
	default:
		a.aboutSettings(c)
	}
}

// group is a titled block of a settings page.
// The page is one form, so the labels of all groups line up.
func group(c *ui.Context, title string, fn func()) {
	if title != "" {
		ui.Text(c.Key("group-"+title), title).Bold().FontSize(13).Margin(8, 0, 0, 0)
	}
	fn()
}

// actions lays out buttons at the end of a row.
func actions(c *ui.Context, fn func()) {
	ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(fn)
}

func (a *App) windowSettings(c *ui.Context) {
	v := &a.settings.draft
	group(c, "外观", func() {
		modes := []string{"跟随系统", "浅色", "深色"}
		i := max(0, indexOf(modes, v.Appearance))
		ui.Field(c, "外观", func() {
			if ui.Segmented(c, &i, modes...).Label("外观").Changed() {
				v.Appearance = modes[i]
			}
		}).FillWidth()
		var ids, names, colors []string
		for _, p := range accentPresets {
			ids, names, colors = append(ids, p.id), append(names, p.name), append(colors, p.light[0])
			if c.Theme().Dark {
				colors[len(colors)-1] = p.dark[0]
			}
		}
		chosen := accentFor(v.Accent).id
		swatches(c, "主色", &chosen, ids, names, colors)
		if chosen != accentFor(v.Accent).id {
			v.Accent = chosen
		}
	})
	group(c, "窗口", func() {
		confirm := !v.NoCloseConfirm
		if toggle(c, "关闭窗口时确认", &confirm) {
			v.NoCloseConfirm = !confirm
		}
		tabConfirm := !v.NoTabConfirm
		if toggle(c, "关闭标签时确认", &tabConfirm) {
			v.NoTabConfirm = !tabConfirm
		}
		muted(c, "关闭仍在使用的连接前询问；本地终端和已断开的标签直接关闭")
		toggle(c, "启动时恢复标签", &v.RestoreTabs)
		muted(c, "重新打开上次退出时的主机和本地终端；有未保存的远程文件时总会确认")
		toggle(c, "左栏默认折叠", &v.FoldLeft)
		toggle(c, "底部工作区默认折叠", &v.FoldBench)
		muted(c, "每次启动时折叠；不勾选时保持上次退出时的样子")
	})
	if a.settings.err != "" {
		ui.Text(c, a.settings.err).TextColor(c.Theme().Danger)
	}
	actions(c, func() {
		if ui.Button(c, "恢复默认设置…").Clicked() {
			a.confirmDanger("恢复默认设置", "所有设置都会恢复为默认值；主机和密钥不受影响。", "恢复默认", func() { a.settings.load(model.DefaultPreferences()) })
		}
	})
}

func (a *App) transferSettings(c *ui.Context) {
	v := &a.settings.draft
	group(c, "", func() {
		a.localPath(c, "默认下载目录", "", &v.DownloadDir, FilePick{Directory: true})
		field(c, "默认上传目录", &v.UploadDir).Description("远程服务器上的目录，文件面板的「前往默认上传目录」使用")
	})
}

func (a *App) editSettings(c *ui.Context) {
	group(c, "", func() {
		a.localPath(c, "外部编辑器", "系统文本编辑器", &a.settings.draft.EditorCommand, FilePick{DefaultPath: programDir(false)}).Description("保存后自动上传，状态显示在右侧「实时编辑」")
	})
}

func (a *App) terminalSettings(c *ui.Context) {
	s := &a.settings
	v := &s.draft
	group(c, "字体", func() {
		field(c, "字体", &v.FontFamily).Description("留空使用系统等宽字体")
		field(c, "字号", &s.size).Description("10–24")
		field(c, "行高", &s.lineHeight).Description("1–2")
	})
	group(c, "配色", func() {
		choice(c, "配色方案", &v.TerminalTheme, []string{"跟随外观", "Default", "Dracula", "Solarized Dark", "Gruvbox Dark", "Nord", "自定义"})
		if v.TerminalTheme == "自定义" {
			colorField(c, "背景色", &v.Background)
			colorField(c, "前景色", &v.Foreground)
		}
	})
	group(c, "光标与缓冲", func() {
		cursors := []string{"block", "bar", "underline"}
		i := max(0, indexOf(cursors, v.Cursor))
		ui.Field(c, "光标样式", func() {
			if ui.Segmented(c, &i, "方块", "竖线", "下划线").Label("光标样式").Changed() {
				v.Cursor = cursors[i]
			}
		}).Description("新会话生效").FillWidth()
		toggle(c, "光标闪烁", &v.CursorBlink)
		field(c, "滚动缓冲（MiB）", &s.scrollback).Description("1–256，新会话生效")
	})
	group(c, "行为", func() {
		ui.Field(c, "本地终端标签跟随程序标题", func() { ui.Switch(c, &v.TitleUpdates).Label("本地终端标签跟随程序标题") }).Description("只影响本地终端。SSH 标签始终显示连接名称，程序设置的标题在标签的悬停提示和左栏会话卡里；手动重命名优先").FillWidth()
		toggle(c, "响铃与通知", &v.Notifications)
		toggle(c, "打开链接前确认", &v.HyperlinkConfirm)
		toggle(c, "允许服务器写入剪贴板", &v.ClipboardWrite)
		toggle(c, "服务器读取剪贴板时询问", &v.ClipboardReadAsk)
		muted(c, "剪贴板选项在新会话生效")
	})
	group(c, "Shell 集成", func() {
		modes := []string{"auto", "", "off"}
		i := max(0, indexOf(modes, v.ShellIntegration))
		ui.Field(c, "目录跟踪", func() {
			if ui.Segmented(c, &i, "自动", "手动", "关闭").Label("目录跟踪").Changed() {
				v.ShellIntegration = modes[i]
			}
		}).Description([]string{
			"会在服务器上安装一小段目录跟踪脚本，新的 bash / zsh 会话自动加载；文件面板可跟随终端目录。其他 shell 不受影响",
			"默认。不写服务器文件；需要时在命令面板执行「插入目录跟踪脚本」，只对当前会话生效",
			"不注入，命令面板里也不提供",
		}[i]).FillWidth()
	})
	a.sshSettings(c)
}

// colorField edits a #rrggbb preference with a color well.
func colorField(c *ui.Context, label string, hex *string) {
	color := ui.Hex(*hex)
	ui.Field(c, label, func() {
		if ui.ColorWell(c, &color).Label(label).Changed() {
			*hex = fmt.Sprintf("#%02x%02x%02x", color.R, color.G, color.B)
		}
	}).FillWidth()
}

// sshSettings are the terminal page's connection groups.
func (a *App) sshSettings(c *ui.Context) {
	s := &a.settings
	v := &s.draft
	group(c, "SSH 心跳", func() {
		toggle(c, "默认发送心跳", &v.KeepaliveEnabled)
		field(c, "默认间隔（秒）", &s.keepalive).Description("5–600；主机设为「跟随全局设置」时使用，新会话生效")
	})
	group(c, "密钥、代理与凭据", func() {
		muted(c, "密钥、代理、凭据和连接的导入导出都在连接管理器里，可以在编辑主机时直接新建。")
		actions(c, func() {
			for _, v := range []struct {
				label string
				page  int
			}{{"管理密钥", managerKeys}, {"管理代理", managerProxies}, {"管理凭据", managerCredentials}} {
				if ui.Button(c, v.label).Clicked() {
					a.openManagerPage(v.page)
				}
			}
			if ui.Button(c, "导入导出连接").Clicked() {
				a.openImport("")
			}
		})
	})
	group(c, "本地终端", func() {
		a.localPath(c, "本地 Shell", "系统默认", &v.LocalShell, FilePick{DefaultPath: programDir(true), Hidden: true})
	})
	a.hostKeySettings(c)
	if s.err != "" {
		ui.Text(c, s.err).TextColor(c.Theme().Danger)
	}
}

// programDir is where the settings' program pickers open: where this OS keeps shells or applications.
func programDir(shell bool) string {
	switch {
	case runtime.GOOS == "windows" && shell:
		return `C:\Windows\System32` // a starting point for the panel, not a lookup
	case runtime.GOOS == "windows":
		return `C:\Program Files`
	case shell:
		return "/bin"
	case runtime.GOOS == "darwin":
		return "/Applications"
	}
	return "/usr/bin"
}

const releasesURL = "https://github.com/HynoR/NextShell/releases"

func (a *App) aboutSettings(c *ui.Context) {
	group(c, "", func() {
		ui.Text(c, "NextShell 2").FontSize(16).Bold()
		muted(c, "原生 SSH 终端、文件与监控")
	})
	group(c, "应用", func() {
		ui.Field(c, "版本", func() { ui.Text(c, orDefault(a.Version, "开发版")).Selectable() }).FillWidth()
		if a.ProfilePath != "" {
			ui.Field(c, "配置文件", func() { ui.Text(c, a.ProfilePath).Font("monospace").FontSize(12).Selectable().Grow(1).MinWidth(0) }).Description("主机、设置等数据保存在这个文件里").FillWidth()
		}
		ui.Field(c, "发布页", func() {
			ui.Row(c).Grow(1).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, releasesURL).Font("monospace").FontSize(12).Selectable().Grow(1).MinWidth(0)
				if ui.Button(c, "打开发布页").Clicked() {
					a.report(a.P.OpenURL(releasesURL), "")
				}
			})
		}).Description("新版本发布在这里，NextShell 不会自动检查更新").FillWidth()
	})
	group(c, "最近操作", func() {
		if len(a.logs) == 0 {
			muted(c, "暂无记录（不记录密码和终端内容）")
		}
		for i := len(a.logs) - 1; i >= 0; i-- {
			ui.Text(c.Key(fmt.Sprint("log-", i)), a.logs[i]).FontSize(12).Selectable()
		}
	})
}

// hostKeySettings lists the trusted server keys; forgetting one makes the
// next connection ask again.
func (a *App) hostKeySettings(c *ui.Context) {
	t := c.Theme()
	keys, e := a.P.HostKeys()
	group(c, "已信任的主机密钥", func() {
		if e != nil {
			ui.Text(c, e.Error()).TextColor(t.Danger)
		} else if len(keys) == 0 {
			muted(c, "还没有信任过的主机。首次连接时核对指纹后会记在这里。")
		}
		for _, k := range keys {
			ui.Row(c.Key("hostkey-"+k.Address)).FillWidth().Padding(8).Gap(8).AlignItems(ui.Center).Radius(t.Radius).Border(1, t.Border).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Children(func() {
					ui.Text(c, k.Address).Bold().SingleLine()
					ui.Text(c, k.Fingerprint).Font("monospace").FontSize(11).TextColor(t.TextMuted).SingleLine().Selectable()
				})
				if ghost(c, "移除", "").Label("移除 " + k.Address).Clicked() {
					a.confirmDanger("移除主机密钥", k.Address+"\n下次连接时会重新显示指纹并询问是否信任。", "移除密钥", func() {
						a.report(a.P.ForgetHostKey(k.Address), "已移除 "+k.Address+" 的主机密钥")
					})
				}
			})
		}
	})
}
