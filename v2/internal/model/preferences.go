package model

import (
	"fmt"
	"math"
	"regexp"
	"strings"
)

// Preferences are local UI/runtime policy, never cloud resource/key material.
type Preferences struct {
	HyperlinkConfirm bool    `json:"hyperlinkConfirm"`
	Appearance       string  `json:"appearance"`
	Accent           string  `json:"accent"` // the main color preset; "" is the default
	FontFamily       string  `json:"fontFamily"`
	FontSize         int     `json:"fontSize"`
	LineHeight       float32 `json:"lineHeight"`
	TerminalTheme    string  `json:"terminalTheme"`
	Background       string  `json:"background"`
	Foreground       string  `json:"foreground"`
	Cursor           string  `json:"cursor"`
	CursorBlink      bool    `json:"cursorBlink"`
	ScrollbackMiB    int     `json:"scrollbackMiB"`
	ClipboardWrite   bool    `json:"clipboardWrite"`
	ClipboardReadAsk bool    `json:"clipboardReadAsk"`
	TitleUpdates     bool    `json:"titleUpdates"`
	Notifications    bool    `json:"notifications"`
	LocalShell       string  `json:"localShell"`
	UploadDir        string  `json:"uploadDir"`
	DownloadDir      string  `json:"downloadDir"`
	EditorCommand    string  `json:"editorCommand"`
	KeepaliveEnabled bool    `json:"keepaliveEnabled"`
	KeepaliveSeconds int     `json:"keepaliveSeconds"`
	NexttracePath    string  `json:"nexttracePath"`
	NoCloseConfirm   bool    `json:"noCloseConfirm"`   // zero value: confirm closing with open tabs
	NoTabConfirm     bool    `json:"noTabConfirm"`     // zero value: confirm closing a connected tab
	RestoreTabs      bool    `json:"restoreTabs"`      // reopen last run's hosts and local shells
	FoldLeft         bool    `json:"foldLeft"`         // start with the left column folded
	FoldBench        bool    `json:"foldBench"`        // start with the bottom workbench folded
	ShellIntegration string  `json:"shellIntegration"` // "" manual, "auto" or "off"
}

// HostKey is a trusted server key fingerprint.
type HostKey struct{ Address, Fingerprint string }

func DefaultPreferences() Preferences {
	return Preferences{HyperlinkConfirm: true, Appearance: "跟随系统", FontSize: 14, LineHeight: 1.2, TerminalTheme: "跟随外观", Background: "#0b1220", Foreground: "#d8e3f0", Cursor: "block", CursorBlink: true, ScrollbackMiB: 10, TitleUpdates: true, Notifications: true, UploadDir: "~", DownloadDir: "~/Downloads", KeepaliveEnabled: true, KeepaliveSeconds: 15}
}

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func (p Preferences) Validate() error {
	if p.Appearance != "跟随系统" && p.Appearance != "浅色" && p.Appearance != "深色" {
		return fmt.Errorf("无效外观")
	}
	if p.FontSize < 10 || p.FontSize > 24 || math.IsNaN(float64(p.LineHeight)) || p.LineHeight < 1 || p.LineHeight > 2 {
		return fmt.Errorf("字号需为10..24，行高需为1..2")
	}
	switch p.TerminalTheme {
	case "跟随外观", "Default", "Dracula", "Solarized Dark", "Gruvbox Dark", "Nord", "自定义":
	default:
		return fmt.Errorf("无效终端配色")
	}
	if !hexColor.MatchString(p.Background) || !hexColor.MatchString(p.Foreground) {
		return fmt.Errorf("颜色格式需为 #RRGGBB")
	}
	if p.Cursor != "block" && p.Cursor != "bar" && p.Cursor != "underline" {
		return fmt.Errorf("无效光标")
	}
	if p.ScrollbackMiB < 1 || p.ScrollbackMiB > 256 {
		return fmt.Errorf("滚动缓冲需为1..256 MiB（不是行数）")
	}
	if p.KeepaliveSeconds < 5 || p.KeepaliveSeconds > 600 {
		return fmt.Errorf("心跳间隔需为5..600秒")
	}
	for _, s := range []string{p.LocalShell, p.EditorCommand, p.NexttracePath, p.UploadDir, p.DownloadDir, p.FontFamily} {
		if strings.ContainsAny(s, "\x00\r\n") {
			return fmt.Errorf("路径和字体不得含控制字符")
		}
	}
	if p.ShellIntegration != "" && p.ShellIntegration != "auto" && p.ShellIntegration != "off" {
		return fmt.Errorf("无效 Shell 集成模式")
	}
	if p.UploadDir == "" || p.DownloadDir == "" {
		return fmt.Errorf("默认目录不得为空")
	}
	return nil
}
