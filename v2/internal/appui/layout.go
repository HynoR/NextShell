package appui

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// layoutState is the main window's regions: the left column, the centre column's
// split between the terminal and the bottom workbench, and the activity column.
type layoutState struct {
	leftHidden, rightOpen, benchHidden bool
	monOpen                            bool    // the left column's 系统监控 section shows its content
	split                              float32 // the terminal's share of the centre column, in percent
	bench                              int     // benchFiles, benchSnippets, benchSysinfo
	saved                              string  // the layout as last saved
	copied                             time.Time
}

const (
	benchFiles = iota
	benchSnippets
	benchSysinfo
)

var benchTabs = []string{"SFTP", "命令库", "系统信息"}

const (
	leftWidth  = 240
	rightWidth = 300
	stripWidth = 40 // a collapsed side column
)

// load reads the layout UIState("layout") saved; what is missing keeps its default.
func (l *layoutState) load(v string) {
	l.split = 68
	folded := false
	fmt.Sscan(v, &l.leftHidden, &l.rightOpen, &l.benchHidden, &l.split, &l.bench, &folded)
	l.monOpen = !folded
	l.split = min(max(l.split, 20), 90)
	l.bench = min(max(l.bench, 0), len(benchTabs)-1)
	l.saved = l.String()
}
func (l *layoutState) String() string {
	return fmt.Sprintf("%t %t %t %.0f %d %t", l.leftHidden, l.rightOpen, l.benchHidden, l.split, l.bench, !l.monOpen)
}

// saveLayout keeps the layout for the next run once it changed. A drag of the
// split saves at each whole percent.
func (a *App) saveLayout() {
	if v := a.layout.String(); v != a.layout.saved {
		a.layout.saved = v
		a.report(a.P.SaveUIState("layout", v), "")
	}
}

// showBench opens the bottom workbench on tab i.
func (a *App) showBench(i int) {
	a.layout.bench = i
	a.layout.benchHidden = false
}

func (a *App) mainWindow(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().Background(t.Background).Children(func() {
		a.titlebar(c)
		ui.Row(c).FillWidth().Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
			a.leftColumn(c)
			ui.Column(c).Grow(1).MinWidth(0).FillHeight().Children(func() { a.centreColumn(c) })
			a.activityColumn(c)
		})
	})
}

// centreColumn is the tabs above the terminal and the workbench, or the welcome page.
func (a *App) centreColumn(c *ui.Context) {
	pending := a.pendingTabs()
	if len(a.Tabs) > 0 || len(pending) > 0 || len(a.documentsIn(false)) > 0 {
		a.tabBar(c)
	}
	if a.current() == nil { // no tabs, or only background tabs an agent opened
		if d := a.activeEditor(); d != nil { // an unsaved document whose host has no session left
			ui.Column(c).Grow(1).FillWidth().MinHeight(0).Children(func() { a.editorTab(c, d) })
			return
		}
		if len(a.Tabs) == 0 && len(pending) > 0 {
			a.connectingView(c, pending)
			return
		}
		ui.Column(c).Grow(1).FillWidth().MinHeight(0).Children(func() { a.welcomeView(c) })
		return
	}
	if a.layout.benchHidden {
		ui.Column(c).Grow(1).FillWidth().MinHeight(0).Children(func() { a.terminalArea(c) })
		a.benchStrip(c)
		return
	}
	ui.SplitVerticalPercent(c, &a.layout.split, func() { a.terminalArea(c) }, func() { a.workbench(c) }).SplitReset(68).Grow(1).FillWidth().MinHeight(0)
}

func (a *App) terminalArea(c *ui.Context) {
	if d := a.activeEditor(); d != nil {
		a.editorTab(c, d)
		return
	}
	if tool := a.activeTool(); tool != nil {
		a.toolView(c, *tool)
		return
	}
	s := a.current()
	ui.Column(c).Fill().MinWidth(0).MinHeight(0).Children(func() {
		if a.tabs.mru.Active() {
			a.mruOverlay(c)
		}
		term := terminal.View(c.Key(s.ID+"terminal"), s.Terminal).Grow(1).FillWidth().AutoFocus().Bind(&a.tabs.term)
		if a.P.Exited(s.ID) && term.Shortcut(0, ui.KeyEnter) {
			a.reconnect(s)
		}
		if id := s.Connection.ID; id != "" && a.hosts.connecting[id] && a.P.Exited(s.ID) && term.Shortcut(0, ui.KeyEscape) {
			a.cancelConnect(id)
		}
		a.endedBanner(c, s)
		if a.cmdbar.finding {
			a.findBar(c, s)
		}
		a.commandBar(c, s)
	})
}

// endedBanner is the one line under the terminal that says the session has ended, with the way back.
// The left column's session card says it too, so the banner shows only while that column is folded.
func (a *App) endedBanner(c *ui.Context, s *Session) {
	if !a.layout.leftHidden || !a.P.Exited(s.ID) {
		return
	}
	if label, _ := a.sessionStatus(c, s); label == "连接中" {
		return
	}
	t := c.Theme()
	text, action := "连接已断开", "重新连接"
	if s.Local {
		text, action = "终端已退出", "重新打开"
	}
	ui.Row(c).FillWidth().Height(32).Padding(0, 12).Gap(10).AlignItems(ui.Center).Background(t.Warning.Alpha(.12)).BorderColor(t.Border).BorderWidth(1, 0, 0, 0).Label(text).Children(func() {
		ui.Icon(c, appIcons["refresh"]).FontSize(13).TextColor(t.Warning)
		ui.Text(c, text).FontSize(13).Bold().TextColor(t.Warning).SingleLine()
		muted(c, "按回车键"+action).SingleLine()
		ui.Spacer(c).Grow(1)
		if ui.Button(c, action).Clicked() {
			a.later(func() { a.reconnect(s) })
		}
	})
}

// sectionLabel heads a section of a side column, small and letter-spaced.
func sectionLabel(c *ui.Context, text string) ui.Element {
	return ui.Text(c, text).FontSize(11).Bold().LetterSpacing(1).TextColor(c.Theme().TextMuted).SingleLine()
}
