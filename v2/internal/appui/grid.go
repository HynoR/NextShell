package appui

import (
	"fmt"
	"io"
	"math"
	"sync"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// 监视网格: every open session as a small, live, read-only picture of its terminal, side by side.
//
// A picture is a second terminal that shows what a terminal.Mirror of the session's own terminal frames
// for it, cell for cell, scaled down with a transform. A Mirror is for a terminal no view shows: its
// frames take the terminal's record of what changed, which the view reads too. So the grid replaces the
// whole area under the tabs while it is up (no session's own view is built), and when it ends, closeGrid
// gives each session's terminal a theme of its own again, which makes its view draw every row anew.

func init() {
	appIcons["grid"] = iconSVG(`<rect x="3.5" y="3.5" width="7" height="7" rx="1.5"/><rect x="13.5" y="3.5" width="7" height="7" rx="1.5"/><rect x="3.5" y="13.5" width="7" height="7" rx="1.5"/><rect x="13.5" y="13.5" width="7" height="7" rx="1.5"/>`)
}

const (
	// gridEvery is how often the pictures catch up with their sessions: a few times a second is
	// plenty to watch a log scroll, and costs a frame of each terminal only when it changed.
	gridEvery = 250 * time.Millisecond
	gridGap   = 12
	gridHead  = 28  // a tile's heading: host colour, status dot, title
	gridMinW  = 200 // the least a tile goes down to before the grid scrolls
	gridMinH  = 120
	gridPad   = 4 // between a tile's border and its picture
)

type gridState struct {
	on    bool
	key   string // the tab that showed when the grid opened: another one showing ends the grid
	tiles map[string]*gridTile
	theme string  // the terminal theme the pictures have
	sig   gridSig // what the last frame laid the grid out for
	// pumps counts the frames taken from sessions, for tests.
	pumps int
}

// gridSig is what the area the grid has depends on.
type gridSig struct {
	w, h        float32
	left, right bool
}

// gridTile is the picture of one session.
type gridTile struct {
	src        *terminal.Terminal
	mirror     *terminal.Mirror
	view       *terminal.Terminal // the picture: a terminal fed the mirror's frames, which nothing can type into
	cols, rows int
	at         time.Time // when it was brought up to date
}

// tileConn is the picture terminal's connection: nothing comes in but frames, and nothing it would send
// (keys, answers to a program's questions) goes anywhere.
type tileConn struct {
	done chan struct{}
	once sync.Once
}

func (c *tileConn) Read([]byte) (int, error)    { <-c.done; return 0, io.EOF }
func (c *tileConn) Write(p []byte) (int, error) { return len(p), nil }
func (c *tileConn) Close() error                { c.once.Do(func() { close(c.done) }); return nil }

// Resize is what the view tells the connection: the picture keeps the session's size, which Resize sets.
func (c *tileConn) Resize(cols, rows int) error { return nil }

func (a *App) newTile(s *Session) *gridTile {
	m, e := s.Terminal.NewMirror()
	if e != nil {
		return nil
	}
	light, dark := TerminalThemes(a.Prefs)
	v, e := terminal.New(terminal.Options{
		Conn:         &tileConn{done: make(chan struct{})},
		ResizeByConn: true, // the size is the session's, not what the tile fits
		Scrollback:   -1,
		Padding:      terminal.Padding{X: .01, Y: .01}, // the zero Padding means the default one
		Theme:        light, DarkTheme: dark,
		NoClipboardWrite: true, // what a program copies must not reach the clipboard from a picture
		ClipboardRead:    terminal.ClipboardDeny,
		NoBlink:          true,
		ImageLimit:       16 << 20, // a picture is small: a program's images count little
	})
	if e != nil {
		m.Close()
		return nil
	}
	return &gridTile{src: s.Terminal, mirror: m, view: v}
}

func (t *gridTile) close() {
	t.mirror.Close()
	t.view.Close()
}

// pump brings the picture up to what the session shows.
func (t *gridTile) pump() {
	if cols, rows := t.src.Size(); cols != t.cols || rows != t.rows {
		t.view.Resize(cols, rows)
		t.cols, t.rows = cols, rows
	}
	if f, _ := t.mirror.Frame(); len(f) > 0 {
		t.view.Feed(f)
	}
}

// toggleGrid shows the grid, or goes back to the tab that showed.
func (a *App) toggleGrid() {
	if a.grid.on {
		a.closeGrid()
	} else if len(a.Tabs) > 0 {
		a.grid = gridState{on: true, key: a.activeTabKey(), tiles: map[string]*gridTile{}}
	}
}

// closeGrid ends the grid: the mirrors and the pictures are closed, and every session's terminal is made
// to draw all of its rows when its view shows again, as the mirrors took the record of which changed.
func (a *App) closeGrid() {
	if !a.grid.on {
		return
	}
	for _, t := range a.grid.tiles {
		t.close()
	}
	light, dark := TerminalThemes(a.Prefs)
	for _, s := range a.Tabs {
		l, d := *light, *dark // new themes: the view applies a theme it has not applied, and draws everything then
		s.Terminal.SetTheme(&l, &d)
	}
	a.grid = gridState{}
}

// gridRebase keeps the grid when the tab that showed was closed: the grid is not what that tab was.
func (a *App) gridRebase() {
	if a.grid.on {
		a.grid.key = a.activeTabKey()
	}
}

// gridShown says whether the grid is what the centre shows: another tab showing, or no session left,
// ends it.
func (a *App) gridShown() bool {
	if a.grid.on && (len(a.Tabs) == 0 || a.activeTabKey() != a.grid.key) {
		a.closeGrid()
	}
	return a.grid.on
}

// gridButton is the tab strip's switch for the grid.
func (a *App) gridButton(c *ui.Context) {
	if len(a.Tabs) == 0 {
		return
	}
	t := c.Theme()
	tip := "监视网格：同屏预览所有会话"
	b := ui.ButtonBase(c).Label("监视网格").Tooltip(tip).Size(28, 28).Radius(t.Radius).Center().TextColor(t.TextMuted)
	switch {
	case a.grid.on:
		b.Background(t.Accent.Alpha(.16)).TextColor(t.Accent)
	case b.Hovered():
		b.Background(t.SurfaceHover).TextColor(t.Text)
	}
	b.Children(func() { ui.Icon(c, appIcons["grid"]).FontSize(16) })
	if b.Clicked() {
		a.later(a.toggleGrid)
	}
}

// gridCell is the size of a cell of the pictures' terminals, in DIPs: the 13 pt monospace font's,
// rounded up to whole pixels as a terminal rounds its cells (at a scale above 1 it is a little less).
var gridCell = sync.OnceValue(func() [2]float32 {
	f := ui.Font{Family: "monospace", Size: 13}
	adv := float32(0)
	for _, g := range ui.Shape(" !\"#$%&'()*+,-./0123456789:;<=>?@ABCDEFGHIJKLMNOPQRSTUVWXYZ[\\]^_`abcdefghijklmnopqrstuvwxyz{|}~", f) {
		adv = max(adv, g.Advance)
	}
	m := f.Metrics()
	return [2]float32{float32(math.Ceil(float64(adv - .05))), float32(math.Ceil(float64(m.Ascent + m.Descent + m.LineGap - .05)))}
})

// gridPlan picks how many columns n tiles go in so that the pictures come out the largest they can
// in an area w by h, and the size of a tile. natural holds each picture's size at scale 1.
func gridPlan(natural [][2]float32, w, h float32) (cols int, tw, th float32) {
	n := len(natural)
	best := float32(-1)
	for c := 1; c <= n; c++ {
		tileW := (w - float32(c-1)*gridGap) / float32(c)
		if c > 1 && tileW < gridMinW {
			break
		}
		rows := (n + c - 1) / c
		availH := max((h-float32(rows-1)*gridGap)/float32(rows), gridMinH)
		sum, tallest := float32(0), float32(0)
		for _, nat := range natural {
			k := gridScale(nat, tileW, availH)
			sum += k
			tallest = max(tallest, nat[1]*k)
		}
		if score := sum / float32(n); score >= best-1e-4 { // a tie goes to more columns: fewer rows to scroll
			// A tile is no taller than its picture needs.
			best, cols, tw, th = score, c, tileW, min(max(gridHead+2+2*gridPad+tallest, gridMinH), availH)
		}
	}
	return
}

// gridScale is how much a picture of natural size is scaled to fit a tile of tw by th: whole, never larger.
func gridScale(natural [2]float32, tw, th float32) float32 {
	return max(min(1, (tw-2-2*gridPad)/natural[0], (th-gridHead-2-2*gridPad)/natural[1]), .01)
}

// gridView is the grid: one tile for each session, in the order of the tabs.
func (a *App) gridView(c *ui.Context) {
	g := &a.grid
	a.gridSync(c)
	t := c.Theme()
	root := ui.Column(c.Key("grid")).Grow(1).FillWidth().MinWidth(0).MinHeight(0).Background(t.Background).Label("监视网格")
	// The area is what the last frame measured. The first frame has none, and a frame that follows a change
	// of the window or its columns has the old one: they estimate it from the window, and ask for another.
	area := root.LayoutBounds()
	w, h := c.Size()
	if sig := (gridSig{w, h, a.layout.leftHidden, a.layout.rightOpen}); sig != g.sig || area.W <= 0 || area.H <= 0 {
		g.sig = sig
		left, right := float32(leftWidth), float32(stripWidth)
		if a.layout.leftHidden {
			left = stripWidth
		}
		if a.layout.rightOpen {
			right = rightWidth
		}
		area = ui.Rect{W: w - left - right, H: h - max(c.TitleBar().Height, 44) - 38}
		c.Invalidate()
	}
	cell := gridCell()
	natural := make([][2]float32, len(a.Tabs))
	for i, s := range a.Tabs {
		cols, rows := s.Terminal.Size()
		if tl := g.tiles[s.ID]; tl != nil && tl.cols > 0 {
			cols, rows = tl.cols, tl.rows
		}
		natural[i] = [2]float32{float32(cols)*cell[0] + 1, float32(rows)*cell[1] + 1}
	}
	cols, tw, th := gridPlan(natural, area.W-2*gridGap, area.H-2*gridGap)
	// Esc leaves the grid, unless something above it takes the key.
	if c.Shortcut(0, ui.KeyEscape) {
		a.later(a.closeGrid)
	}
	root.Children(func() {
		ui.Scroll(c).Grow(1).FillWidth().MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Padding(gridGap).Gap(gridGap).Children(func() {
				for r := 0; r*cols < len(a.Tabs); r++ {
					ui.Row(c.Key(fmt.Sprint("grid-row-", r))).Gap(gridGap).Children(func() {
						for i := r * cols; i < min((r+1)*cols, len(a.Tabs)); i++ {
							a.gridTileView(c, i, natural[i], tw, th)
						}
					})
				}
			})
		})
	})
}

// gridBudget is how long one frame spends on taking frames from sessions before it leaves the rest for the
// next: a session's first frame goes through all of its scrollback, which takes ~20 ms at the default
// 10 MiB and ~0.5 s at 256 MiB, so a grid of many sessions fills in over a few frames instead of
// freezing the window for the sum.
//
// ponytail: all on the window's thread. A session's first frame is O(its scrollback) inside Mirror, so a
// 256 MiB scrollback still holds one frame for ~0.5 s; the fix is upstream (a Mirror that starts from the
// screen), not a goroutine here, since the tab strip reads each terminal's title under the same lock.
var gridBudget = 24 * time.Millisecond

// gridSync keeps the pictures to the sessions, and brings each up to date every gridEvery.
func (a *App) gridSync(c *ui.Context) {
	g := &a.grid
	live := map[string]bool{}
	for _, s := range a.Tabs {
		live[s.ID] = true
		if tl := g.tiles[s.ID]; tl != nil && tl.src != s.Terminal { // the session was replaced
			tl.close()
			delete(g.tiles, s.ID)
		}
	}
	for id, tl := range g.tiles {
		if !live[id] {
			tl.close()
			delete(g.tiles, id)
		}
	}
	p := a.Prefs
	if key := p.TerminalTheme + p.Background + p.Foreground; key != g.theme {
		g.theme = key
		light, dark := TerminalThemes(p)
		for _, tl := range g.tiles {
			tl.view.SetTheme(light, dark)
		}
	}
	now, start, taken, more := c.Now(), time.Now(), 0, false
	for _, s := range a.Tabs {
		tl := g.tiles[s.ID]
		if tl != nil && now.Sub(tl.at) < gridEvery {
			continue
		}
		if taken > 0 && time.Since(start) > gridBudget { // at least one session is taken each frame
			more = true
			continue
		}
		if tl == nil {
			if tl = a.newTile(s); tl == nil {
				continue
			}
			g.tiles[s.ID] = tl
		}
		tl.at = now
		tl.pump()
		g.pumps++
		taken++
	}
	if more {
		c.Invalidate()
	}
	c.After(gridEvery)
}

// gridTileView is one tile: the session's heading, and its picture. Clicking it goes to the session.
func (a *App) gridTileView(c *ui.Context, i int, natural [2]float32, tw, th float32) {
	s := a.Tabs[i]
	t := c.Theme()
	title := a.tabTitle(s)
	label, status := a.sessionStatus(c, s)
	ended := a.P.Exited(s.ID)
	light, dark := TerminalThemes(a.Prefs)
	screen := light.Background
	if t.Dark {
		screen = dark.Background
	}
	card := ui.ButtonBase(c.Key("grid-tile-"+s.ID)).Label("会话预览 "+title).Size(tw, th).Shrink(0).Radius(t.Radius).Clip().Border(1, t.Border).Background(screen)
	switch {
	case i == a.Active:
		card.Border(2, t.Accent)
		card.AutoFocus()
	case card.Hovered():
		card.Border(1, t.TextMuted)
	}
	card.Children(func() {
		ui.Column(c).Fill().Children(func() {
			ui.Row(c).FillWidth().Height(gridHead).Shrink(0).Padding(0, 10).Gap(8).AlignItems(ui.Center).Background(t.Surface).BorderColor(t.Border).BorderWidth(0, 0, 1, 0).Children(func() {
				if !s.Local {
					ui.Box(c).Size(3, 14).Radius(1.5).Background(a.hostColor(s.Connection))
				}
				ui.Box(c).Size(6, 6).Radius(3).Background(status).Label("状态 " + label)
				ui.Text(c, title).FontSize(13).SingleLine().Grow(1).MinWidth(0).TextColor(t.Text)
			})
			ui.Box(c).Grow(1).FillWidth().MinHeight(0).Clip().Children(func() {
				tl := a.grid.tiles[s.ID]
				if tl == nil {
					return
				}
				iw, ih := tw-2, th-gridHead-2
				k := gridScale(natural, tw, th)
				// Centred across the tile, from its top, and drawn by the scale alone: the picture keeps its
				// own size for layout, which the box around it clips.
				// The picture takes no pointer and no Tab: no key reaches it (it could send none anywhere).
				terminal.View(c.Key("grid-view-"+s.ID), tl.view).Absolute().Left(max((iw-natural[0]*k)/2, gridPad)).Top(gridPad).Size(natural[0], natural[1]).Scale(k, k).TransformOrigin(0, 0).PassThrough().TabStop(false)
				if ended {
					// The last screen dimmed, and what became of the session.
					ui.Column(c).Absolute().Left(0).Top(0).Size(iw, ih).Center().Background(screen.Alpha(.6)).PassThrough().Children(func() {
						ui.Text(c, label).FontSize(13).Bold().SingleLine().Padding(4, 12).Radius(12).Background(t.Surface).Border(1, t.Border).TextColor(t.Text)
					})
				}
			})
		})
	})
	if card.Clicked() {
		id := s.ID
		a.later(func() { a.selectTabID(id) })
	}
}
