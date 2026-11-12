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
	fmt.Sscan(v, &l.leftHidden, &l.rightOpen, &l.benchHidden, &l.split, &l.bench)
	l.split = min(max(l.split, 20), 90)
	l.bench = min(max(l.bench, 0), len(benchTabs)-1)
	l.saved = l.String()
}
func (l *layoutState) String() string {
	return fmt.Sprintf("%t %t %t %.0f %d", l.leftHidden, l.rightOpen, l.benchHidden, l.split, l.bench)
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
	if len(a.Tabs) > 0 {
		a.tabBar(c)
	}
	if a.current() == nil { // no tabs, or only background tabs an agent opened
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
	s := a.current()
	ui.Column(c).Fill().MinWidth(0).MinHeight(0).Children(func() {
		if a.tabs.mru.Active() {
			a.mruOverlay(c)
		}
		term := terminal.View(c.Key(s.ID+"terminal"), s.Terminal).Grow(1).FillWidth().AutoFocus().Bind(&a.tabs.term)
		if a.P.Exited(s.ID) && term.Shortcut(0, ui.KeyEnter) {
			a.reconnect(s)
		}
	})
}

// sectionLabel heads a section of a side column, small and letter-spaced.
func sectionLabel(c *ui.Context, text string) ui.Element {
	return ui.Text(c, text).FontSize(11).Bold().LetterSpacing(1).TextColor(c.Theme().TextMuted).SingleLine()
}
