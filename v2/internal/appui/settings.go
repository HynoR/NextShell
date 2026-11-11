package appui

import (
	"fmt"
	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/model"
	"strconv"
)

type settingsState struct {
	Draft                                            model.Preferences
	Size, LineHeight, Scrollback, Keepalive, Section string
}

func (s *settingsState) load(v model.Preferences) {
	s.Draft = v
	s.Size = strconv.Itoa(v.FontSize)
	s.LineHeight = strconv.FormatFloat(float64(v.LineHeight), 'f', 1, 32)
	s.Scrollback = strconv.Itoa(v.ScrollbackMiB)
	s.Keepalive = strconv.Itoa(v.KeepaliveSeconds)
	if s.Section == "" {
		s.Section = "终端"
	}
}
func (s *settingsState) parse() (model.Preferences, error) {
	p := s.Draft
	var err error
	if p.FontSize, err = strconv.Atoi(s.Size); err != nil {
		return p, fmt.Errorf("字号必须为整数")
	}
	line, e := strconv.ParseFloat(s.LineHeight, 32)
	if e != nil {
		return p, fmt.Errorf("行高必须为数字")
	}
	p.LineHeight = float32(line)
	if p.ScrollbackMiB, err = strconv.Atoi(s.Scrollback); err != nil {
		return p, fmt.Errorf("滚动缓冲必须为整数")
	}
	if p.KeepaliveSeconds, err = strconv.Atoi(s.Keepalive); err != nil {
		return p, fmt.Errorf("keepalive必须为整数")
	}
	return p, p.Validate()
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
		a.status(e)
		return
	}
	a.Prefs = p
	a.Appearance = p.Appearance
	a.Settings.load(p)
	a.Phase2.ExternalProgram = p.EditorCommand
	l, d := TerminalThemes(p)
	for _, s := range a.Tabs {
		s.Terminal.SetTheme(l, d)
	}
	a.status(nil)
}
func (a *App) settingsView(c *ui.Context) {
	s := &a.Settings
	v := &s.Draft
	ui.Scroll(c).Key("page-settings").Fill().Padding(PageInset).Children(func() {
		ui.Column(c).FillWidth().MaxWidth(1000).Padding(PanelInset).Radius(PanelRadius).Background(c.Theme().Surface).Gap(FormGap).Children(func() {
			ui.Text(c, "设置").FontSize(26).Bold()
			choice(c, "设置分类", &s.Section, []string{"终端", "连接与文件", "兼容性与未实现"})
			if s.Section == "终端" {
				choice(c, "设置外观", &v.Appearance, []string{"跟随系统", "浅色", "深色"})
				field(c, "终端字体（空为系统等宽/CJK）", &v.FontFamily)
				field(c, "终端字号（10..24）", &s.Size)
				field(c, "终端行高（1..2）", &s.LineHeight)
				choice(c, "终端配色", &v.TerminalTheme, []string{"跟随外观", "Default", "Dracula", "Solarized Dark", "Gruvbox Dark", "Nord", "自定义"})
				field(c, "终端背景色 #RRGGBB", &v.Background)
				field(c, "终端前景色 #RRGGBB", &v.Foreground)
				choice(c, "终端光标", &v.Cursor, []string{"block", "bar", "underline"})
				ui.Checkbox(c, &v.CursorBlink, "光标闪烁")
				field(c, "滚动缓冲 MiB（1..256）", &s.Scrollback)
				ui.Checkbox(c, &v.TitleUpdates, "允许 OSC 标题更新（手动名字优先）")
				ui.Checkbox(c, &v.Notifications, "终端响铃与应用内通知")
				ui.Checkbox(c, &v.ClipboardWrite, "允许远端程序写入剪贴板（OSC 52）")
				ui.Checkbox(c, &v.ClipboardReadAsk, "远端读取剪贴板时逐次询问（不自动批准）")
				ui.Text(c, "字体/颜色立即生效；光标、缓冲、OSC剪贴板策略仅新建/重连会话生效。默认拒绝OSC剪贴板读写，比v1更保守。").TextColor(c.Theme().TextMuted)
			} else if s.Section == "连接与文件" {
				field(c, "本地 shell 可执行文件（空为系统默认；不含参数）", &v.LocalShell)
				ui.Checkbox(c, &v.KeepaliveEnabled, "启用全局 SSH keepalive")
				field(c, "全局 keepalive 秒（5..600）", &s.Keepalive)
				ui.Text(c, "仅未指定覆盖值的新连接采用全局策略；已连接会话不被重连。").TextColor(c.Theme().TextMuted)
				field(c, "默认上传远端目录", &v.UploadDir)
				field(c, "默认下载本地目录", &v.DownloadDir)
				field(c, "默认外部编辑器可执行文件", &v.EditorCommand)
				field(c, "nexttrace 可执行文件（用户自行安装）", &v.NexttracePath)
				ui.Text(c, "目录通过文件页的“默认上传目录/默认下载位置”显式填入；绝不静默覆盖现有路径。外部编辑仍需手动启动。").TextColor(c.Theme().TextMuted)
			} else {
				ui.Text(c, "以下1.x设置有对应说明入口，但尚无可工作的等价功能；不会保存无效开关。").Bold()
				for _, x := range []string{"背景图 / 透明度 / see-through / WebGL：xterm专属材质未移植", "链接打开前确认：尚未接入官方OpenLink确认流程", "Shell集成 auto/manual/off：目前仅手工安装OSC7脚本；OSC133等待上游", "窗口关闭确认/默认侧栏折叠/默认工作台折叠：当前固定行为", "连接管理弹窗尺寸/列宽：v2为主页面与Split，不复刻旧弹窗", "外部编辑器自动默认模式：目前需显式启动", "Agent端口/exec超时/foreground/approval：使用v2独立更严格安全流程，见Agent页", "nexttrace PoW provider：不调用外部API；仅用户安装的CLI", "托盘/自动更新/多窗口：alex阶段4负责，本轮不动", "旧数据库/keytar迁移、旧云工作区、XOR：按既定决策不做"} {
					ui.Text(c, x).TextColor(c.Theme().TextMuted)
				}
			}
			ui.Row(c).Gap(8).Children(func() {
				if ui.PrimaryButton(c, "保存设置").Clicked() {
					p, e := s.parse()
					if e != nil {
						a.status(e)
					} else {
						a.savePreferences(p)
					}
				}
				if ui.Button(c, "恢复默认（未保存）").Clicked() {
					s.load(model.DefaultPreferences())
				}
			})
		})
	})
}
