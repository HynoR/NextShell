package appui_test

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/model"
)

// gridSession opens a session whose terminal records what is typed into it.
func gridSession(t *testing.T, p *fakePlatform, tt *ui.Tester, id, title string, c model.Connection, text string) (*appui.Session, *captureConn) {
	t.Helper()
	conn := &captureConn{closed: make(chan struct{})}
	term, e := terminal.New(terminal.Options{Conn: conn, NoBlink: true})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { term.Close() })
	term.Feed([]byte(text))
	s := &appui.Session{ID: id, Title: title, Terminal: term, Connection: c, Local: c.Host == ""}
	p.events = append(p.events, appui.Event{Session: s})
	tt.Frame()
	return s, conn
}

// gridFixture is the app with three sessions: a, b and a local shell, c showing.
func gridFixture(t *testing.T) (*appui.App, *fakePlatform, *ui.Tester, []*captureConn) {
	t.Helper()
	a, p, tt := fixture(t)
	var conns []*captureConn
	for _, v := range []struct{ id, title, text string }{{"A", "tab-A", "alpha$ "}, {"B", "tab-B", "bravo$ "}, {"C", "tab-C", "charlie$ "}} {
		c := model.Connection{}
		if v.id != "C" {
			c = remoteHost(v.id)
		}
		_, conn := gridSession(t, p, tt, v.id, v.title, c, v.text)
		conns = append(conns, conn)
	}
	return a, p, tt, conns
}

func openGrid(t *testing.T, tt *ui.Tester) {
	t.Helper()
	click(t, tt, "监视网格")
	tt.Advance(gridWait)
}

const gridWait = 600 * time.Millisecond

func tileOf(t *testing.T, tt *ui.Tester, title string) ui.Rect {
	t.Helper()
	r, ok := tt.Find("会话预览 " + title)
	if !ok {
		t.Fatal("no tile for", title, tt.Texts())
	}
	return r
}

// The button in the tab strip and the palette's action both show the grid, with a tile for each session,
// and either leaves it again.
func TestGridToggle(t *testing.T) {
	a, _, tt, _ := gridFixture(t)
	if a.GridOn() || tt.HasText("会话预览 tab-A") {
		t.Fatal("the grid shows without being asked")
	}
	openGrid(t, tt)
	if !a.GridOn() {
		t.Fatal("the button did not show the grid")
	}
	for _, title := range []string{"tab-A", "tab-B", "tab-C"} {
		tileOf(t, tt, title)
	}
	if tt.HasText("Terminal") && len(a.GridMirrors()) != 3 {
		t.Fatal("a mirror for each session", len(a.GridMirrors()))
	}
	click(t, tt, "监视网格")
	if a.GridOn() || tt.HasText("会话预览 tab-A") {
		t.Fatal("the button did not leave the grid")
	}
	// The palette's action.
	tt.Key(appMod(), ui.KeyK)
	tt.Type("监视网格")
	tt.Key(0, ui.KeyEnter)
	tt.Advance(gridWait)
	if !a.GridOn() {
		t.Fatal("the palette did not show the grid", tt.Texts())
	}
	tt.Key(appMod(), ui.KeyK)
	tt.Type("监视网格")
	tt.Key(0, ui.KeyEnter)
	tt.Advance(gridWait)
	if a.GridOn() {
		t.Fatal("the palette did not leave the grid")
	}
}

// With no session there is nothing to show: no button, and a grid showing when the last session closes ends.
func TestGridNeedsSessions(t *testing.T) {
	a, p, tt := fixture(t)
	if tt.HasText("监视网格") {
		t.Fatal("a button with no sessions")
	}
	gridSession(t, p, tt, "A", "tab-A", remoteHost("A"), "$ ")
	openGrid(t, tt)
	if !a.GridOn() {
		t.Fatal("not shown")
	}
	p.events = append(p.events, appui.Event{CloseID: "A"})
	tt.Frame()
	if a.GridOn() || len(a.GridMirrors()) != 0 {
		t.Fatal("the grid stayed with no session")
	}
}

// Clicking a tile, its picture too, goes to that session and leaves the grid.
func TestGridClickActivates(t *testing.T) {
	for _, where := range []string{"heading", "text", "picture"} {
		t.Run(where, func(t *testing.T) {
			a, _, tt, _ := gridFixture(t)
			if a.Active != 2 {
				t.Fatal("setup: active", a.Active)
			}
			openGrid(t, tt)
			r := tileOf(t, tt, "tab-B")
			y := r.Y + 14
			switch where {
			case "text":
				y = r.Y + 28 + 6 // over the picture's first line
			case "picture":
				y = r.Y + r.H - 20
			}
			tt.ClickAt(r.X+r.W/2, y)
			if a.GridOn() || a.Active != 1 {
				t.Fatalf("grid %v, active %d, want the second tab", a.GridOn(), a.Active)
			}
			if tt.HasText("会话预览 tab-A") {
				t.Fatal("tiles still show")
			}
		})
	}
}

// A tab of the strip, a click on the active one too, goes back to the sessions.
func TestGridTabStripLeaves(t *testing.T) {
	a, _, tt, _ := gridFixture(t)
	openGrid(t, tt)
	click(t, tt, "tab-A")
	if a.GridOn() || a.Active != 0 {
		t.Fatal("a tab did not leave the grid", a.GridOn(), a.Active)
	}
	openGrid(t, tt)
	click(t, tt, "tab-A") // the one that was showing
	if a.GridOn() || a.Active != 0 {
		t.Fatal("the active tab did not leave the grid", a.GridOn(), a.Active)
	}
}

// Esc leaves the grid and keeps the tab that was showing; with the palette up it closes the palette only.
func TestGridEscape(t *testing.T) {
	a, _, tt, _ := gridFixture(t)
	a.Active = 1
	tt.Frame()
	openGrid(t, tt)
	tt.Key(appMod(), ui.KeyK)
	tt.Key(0, ui.KeyEscape)
	if !a.GridOn() {
		t.Fatal("Esc in the palette left the grid too")
	}
	tt.Key(0, ui.KeyEscape)
	if a.GridOn() || a.Active != 1 {
		t.Fatal("Esc did not leave the grid on the tab that showed", a.GridOn(), a.Active)
	}
}

// Nothing typed while the grid shows reaches any session; once it ends, typing does.
func TestGridNoInputReachesSessions(t *testing.T) {
	a, _, tt, conns := gridFixture(t)
	tt.Type("x")
	waitFor(t, func() bool { return conns[2].text() == "x" }, "the terminal takes keys before the grid")
	openGrid(t, tt)
	tt.Type("hello")
	tt.Key(ui.Ctrl, ui.KeyC)
	tt.Key(ui.Ctrl, ui.KeyD)
	tt.Key(0, ui.KeyBackspace)
	tt.Key(0, ui.KeyUp)
	tt.Key(ui.Shift, ui.KeyTab)
	tt.Key(ui.Cmd, ui.KeyL)
	tt.Key(ui.Cmd|ui.Shift, ui.KeyF)
	tt.Type("world")
	time.Sleep(150 * time.Millisecond) // the input queues write on their own
	for i, c := range conns {
		want := ""
		if i == 2 {
			want = "x"
		}
		if got := c.text(); got != want {
			t.Fatalf("session %d got %q while the grid showed", i, got)
		}
	}
	if !a.GridOn() {
		t.Fatal("the keys left the grid")
	}
	// Back at the session, none of those keys left anything waiting (the command bar or the find bar
	// taking the focus), and typing reaches the session.
	tt.Key(0, ui.KeyEscape)
	tt.Type("ok")
	waitFor(t, func() bool { return conns[2].text() == "xok" }, "typing after the grid")
	if tt.HasText("查找终端内容") && !tt.HasText("输入命令") {
		t.Fatal("the find bar opened")
	}
	// A click on a tile is no input either, and its session gets the keys after it.
	openGrid(t, tt)
	r := tileOf(t, tt, "tab-A")
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	if a.GridOn() || conns[0].text() != "" {
		t.Fatalf("the click typed into its session: %q", conns[0].text())
	}
	tt.Type("hi")
	waitFor(t, func() bool { return conns[0].text() == "hi" }, "typing after the click")
}

// Leaving the grid closes every mirror and picture, whichever way it goes, and leaves no goroutine.
func TestGridClosesMirrorsAndLeavesNoGoroutines(t *testing.T) {
	a, _, tt, _ := gridFixture(t)
	tt.Advance(time.Second)
	base := runtime.NumGoroutine()
	for round, leave := range []func(){
		func() { click(t, tt, "监视网格") },
		func() { tt.Key(0, ui.KeyEscape) },
		func() { click(t, tt, "tab-B") },
		func() { tt.Key(ui.Cmd, ui.Key1) },
	} {
		openGrid(t, tt)
		mirrors := a.GridMirrors()
		if len(mirrors) != 3 {
			t.Fatal("mirrors", len(mirrors))
		}
		pictures := map[string]*terminal.Terminal{}
		for id := range mirrors {
			pictures[id] = a.GridPicture(id)
		}
		if during := runtime.NumGoroutine(); during <= base {
			t.Fatalf("round %d: %d goroutines during the grid, %d before: nothing to leak?", round, during, base)
		}
		leave()
		if a.GridOn() {
			t.Fatalf("round %d: did not leave", round)
		}
		for _, s := range a.Tabs {
			s.Terminal.Feed([]byte("more output\r\n")) // an open mirror would have a frame of it
		}
		for id, m := range mirrors {
			if f, due := m.Frame(); f != nil || due {
				t.Fatalf("round %d: the mirror of %s still frames", round, id)
			}
			select {
			case <-pictures[id].Done():
			case <-time.After(time.Second):
				t.Fatalf("round %d: the picture of %s is open", round, id)
			}
		}
		if len(a.GridMirrors()) != 0 {
			t.Fatal("tiles kept")
		}
	}
	waitFor(t, func() bool { return runtime.NumGoroutine() <= base }, fmt.Sprintf("goroutines back to %d (now %d)", base, runtime.NumGoroutine()))
}

func waitFor(t *testing.T, ok func() bool, what string) {
	t.Helper()
	until := time.Now().Add(3 * time.Second)
	for !ok() {
		if time.Now().After(until) {
			t.Fatal("timed out:", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// The pictures show what the sessions show, and follow it at a modest rate: a frame every 250 ms, not
// one for each frame of the window, and none while the window is idle.
func TestGridPicturesFollowSessionsAtAModestRate(t *testing.T) {
	a, _, tt, _ := gridFixture(t)
	openGrid(t, tt)
	for id, want := range map[string]string{"A": "alpha$", "B": "bravo$", "C": "charlie$"} {
		if got := a.GridPicture(id).Text(); !strings.Contains(got, want) {
			t.Fatalf("picture of %s shows %q", id, got)
		}
	}
	// Output scrolls the session far past its screen: its picture, which keeps no scrollback, shows the screen.
	s := a.Tabs[0]
	var b strings.Builder
	for i := 0; i < 300; i++ {
		fmt.Fprintf(&b, "\x1b[3%dmline %03d\x1b[0m\r\n", i%7, i)
	}
	s.Terminal.Feed([]byte(b.String() + "last$ "))
	tt.Advance(gridWait)
	pic := a.GridPicture("A")
	cols, rows := s.Terminal.Size()
	if pc, pr := pic.Size(); pc != cols || pr != rows {
		t.Fatalf("picture %dx%d, session %dx%d", pc, pr, cols, rows)
	}
	if got, want := screenOf(pic.Text(), rows), screenOf(s.Terminal.Text(), rows); got != want || !strings.Contains(got, "last$") {
		t.Fatalf("picture shows\n%s\nsession shows\n%s", got, want)
	}
	// Rate: two seconds is about eight frames of each session, with output arriving all the while.
	before := a.GridPumps()
	for i := 0; i < 40; i++ {
		s.Terminal.Feed([]byte(fmt.Sprintf("tick %d\r\n", i)))
		tt.Advance(50 * time.Millisecond)
	}
	if n := (a.GridPumps() - before) / 3; n < 6 || n > 10 {
		t.Fatalf("%d frames of a session in 2 s, want about 8", n)
	}
	// The picture caught up with the last of it.
	if got := a.GridPicture("A").Text(); !strings.Contains(got, "tick 39") {
		t.Fatalf("picture behind: %q", got)
	}
	// Idle, the window asks for a frame only at the slow tick.
	before = a.GridPumps()
	tt.Advance(time.Second)
	if n := (a.GridPumps() - before) / 3; n > 5 {
		t.Fatalf("%d frames of a session in 1 s of nothing happening", n)
	}
}

// screenOf is the last rows lines of text, without trailing blanks.
func screenOf(text string, rows int) string {
	lines := strings.Split(strings.TrimRight(text, "\n "), "\n")
	if len(lines) > rows {
		lines = lines[len(lines)-rows:]
	}
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	return strings.Join(lines, "\n")
}

// A session that ended shows its last screen, dimmed, and says 已断开; the others say nothing of the kind.
func TestGridShowsDisconnectedSessions(t *testing.T) {
	a, p, tt := fixture(t)
	gridSession(t, p, tt, "A", "tab-A", remoteHost("A"), "alpha$ ")
	gone, conn := gridSession(t, p, tt, "B", "tab-B", remoteHost("B"), "last words$ ")
	live, _ := gridSession(t, p, tt, "C", "tab-C", remoteHost("C"), "charlie$ ")
	_ = live
	a.Active = 0
	tt.Frame()
	conn.Close()
	<-gone.Terminal.Done()
	tt.Frame()
	openGrid(t, tt)
	marks := 0
	for _, s := range tt.Texts() {
		if s == "已断开" {
			marks++
		}
	}
	if marks != 1 { // the mark over its picture
		t.Fatalf("已断开 shows %d times: %q", marks, tt.Texts())
	}
	// Its last screen is still there.
	if got := a.GridPicture("B").Text(); !strings.Contains(got, "last words$") {
		t.Fatalf("the picture lost the last screen: %q", got)
	}
}

// A session's own view draws everything again after the grid: the mirrors took the record of what
// changed, and without a fix the view would keep rows from before. Compared with an app that never
// showed the grid.
func TestGridLeavesTheSessionViewIntact(t *testing.T) {
	run := func(grid bool) *ui.Tester {
		a, p, tt := fixture(t)
		s, _ := gridSession(t, p, tt, "A", "tab-A", remoteHost("A"), "one\r\ntwo\r\nthree\r\n$ ")
		other, _ := gridSession(t, p, tt, "B", "tab-B", remoteHost("B"), "bravo\r\n$ ")
		a.Active = 0
		tt.Frame()
		if grid {
			openGrid(t, tt)
		}
		s.Terminal.Feed([]byte("\x1b[2;1HTWO CHANGED\x1b[5;1Hnew row\r\n$ "))
		other.Terminal.Feed([]byte("\x1b[1;1HBRAVO CHANGED"))
		if grid {
			tt.Advance(gridWait)
			click(t, tt, "监视网格")
			if a.GridOn() {
				t.Fatal("still on")
			}
		} else {
			tt.Frame()
		}
		tt.Frame()
		// Both sessions' views are drawn, as when their tabs are clicked.
		click(t, tt, "tab-B")
		click(t, tt, "tab-A")
		return tt
	}
	with, without := run(true), run(false)
	r, ok := with.Find("Terminal")
	if !ok {
		t.Fatal("no terminal")
	}
	if n := differ(with, without, r); n != 0 {
		t.Fatalf("%d pixels of the terminal differ from an app that never showed the grid", n)
	}
}

// differ counts the pixels in r that differ between two tests' last frames.
func differ(a, b *ui.Tester, r ui.Rect) int {
	ia, ib := a.Image(), b.Image()
	n := 0
	for y := int(r.Y); y < int(r.Y+r.H); y++ {
		for x := int(r.X); x < int(r.X+r.W); x++ {
			if ia.At(x, y) != ib.At(x, y) {
				n++
			}
		}
	}
	return n
}

// A frame that has no time left for every session's first frame (a big scrollback makes it slow) leaves the
// others to the frames after it: all of them come, one after the other.
func TestGridFillsInOverFrames(t *testing.T) {
	defer appui.SetGridBudget(0)()
	a, _, tt, _ := gridFixture(t)
	openGrid(t, tt)
	for id, want := range map[string]string{"A": "alpha$", "B": "bravo$", "C": "charlie$"} {
		if pic := a.GridPicture(id); pic == nil || !strings.Contains(pic.Text(), want) {
			t.Fatalf("picture of %s is missing", id)
		}
	}
}

// From one session to a dozen the tiles fit the area without scrolling and the pictures shrink only as
// sessions are added; beyond that the tiles stop at a least size and the grid scrolls.
func TestGridPlanSizesTiles(t *testing.T) {
	cell := appui.GridCell()
	nat := [2]float32{100*cell[0] + 1, 30*cell[1] + 1}
	const w, h = 960, 680
	prev := float32(2)
	for n := 1; n <= 30; n++ {
		natural := make([][2]float32, n)
		for i := range natural {
			natural[i] = nat
		}
		cols, tw, th := appui.GridPlan(natural, w, h)
		rows := (n + cols - 1) / cols
		if cols < 1 || cols > n {
			t.Fatalf("%d sessions in %d columns", n, cols)
		}
		if got := float32(cols)*tw + float32(cols-1)*12; got > w+.01 {
			t.Fatalf("%d sessions: %d columns of %.0f are %.0f wide, the area %d", n, cols, tw, got, w)
		}
		if th < 120 || (cols > 1 && tw < 200) {
			t.Fatalf("%d sessions: tiles %.0f x %.0f are below the least size", n, tw, th)
		}
		if n <= 12 && float32(rows)*th+float32(rows-1)*12 > h+.01 {
			t.Fatalf("%d sessions: %d rows of %.0f do not fit %d", n, rows, th, h)
		}
		k := appui.GridScale(nat, tw, th)
		if k > prev+1e-4 && n <= 12 {
			t.Fatalf("%d sessions show larger (%.2f) than %d did (%.2f)", n, k, n-1, prev)
		}
		prev = k
		if n == 1 && (cols != 1 || tw != w) {
			t.Fatalf("one session is %d columns of %.0f", cols, tw)
		}
		t.Logf("%2d sessions: %d columns, tiles %3.0f x %3.0f, pictures at %.2f", n, cols, tw, th, k)
	}
}

// Esc with a dialog up closes the dialog; the grid stays for the next Esc.
func TestGridEscapeInADialog(t *testing.T) {
	a, _, tt, _ := gridFixture(t)
	openGrid(t, tt)
	click(t, tt, "关闭标签 tab-A")
	has(t, tt, "关闭 tab-A？")
	tt.Key(0, ui.KeyEscape)
	if tt.HasText("关闭 tab-A？") || !a.GridOn() {
		t.Fatalf("dialog up %v, grid %v after Esc", tt.HasText("关闭 tab-A？"), a.GridOn())
	}
	tt.Key(0, ui.KeyEscape)
	if a.GridOn() {
		t.Fatal("the second Esc did not leave the grid")
	}
}

// Closing a session from the strip while the grid shows takes its tile away; a new one gets a tile.
func TestGridFollowsTheTabs(t *testing.T) {
	a, p, tt, _ := gridFixture(t)
	openGrid(t, tt)
	click(t, tt, "关闭标签 tab-C") // a local shell: closes at once
	if tt.HasText("会话预览 tab-C") || len(a.GridMirrors()) != 2 || !a.GridOn() {
		t.Fatal("the tile of a closed session stays", len(a.GridMirrors()))
	}
	gridSession(t, p, tt, "D", "tab-D", remoteHost("D"), "delta$ ")
	if a.GridOn() {
		t.Fatal("a new session shows in the grid, whose tab is not the one showing")
	}
}

// Tab goes from tile to tile and no picture takes the focus on the way (a terminal there would take Esc,
// and the keys, though it could send them nowhere).
func TestGridPicturesTakeNoFocus(t *testing.T) {
	a, _, tt, conns := gridFixture(t)
	openGrid(t, tt)
	tt.Key(ui.Shift, ui.KeyTab) // from the active tile (the last) back to the one before
	if !tt.Focused("会话预览 tab-B") {
		t.Fatal("Shift+Tab did not go to the previous tile")
	}
	tt.Type("typed")
	tt.Key(0, ui.KeyEscape)
	if a.GridOn() {
		t.Fatal("Esc did not leave the grid from the tile")
	}
	time.Sleep(100 * time.Millisecond)
	for i, c := range conns {
		if c.text() != "" {
			t.Fatalf("session %d got %q", i, c.text())
		}
	}
}

// Enter on the focused tile goes to its session.
func TestGridEnterOnATile(t *testing.T) {
	a, _, tt, _ := gridFixture(t)
	openGrid(t, tt)
	tt.Key(ui.Shift, ui.KeyTab)
	tt.Key(0, ui.KeyEnter)
	if a.GridOn() || a.Active != 1 {
		t.Fatalf("grid %v, active %d: Enter did not go to the tile's session", a.GridOn(), a.Active)
	}
}

// The pictures are transparent terminals like all the others, so the tile under them paints the terminal's
// ground: without a picture behind the window, the theme's own background, not the window's.
func TestGridTilesPaintTheTerminalGround(t *testing.T) {
	a, p, tt, _ := gridFixture(t)
	_ = a
	for _, dark := range []bool{false, true} {
		tt.SetDark(dark)
		tt.Frame()
		openGrid(t, tt)
		r := tileOf(t, tt, "tab-A")
		light, darkTheme := appui.TerminalThemes(p.Preferences())
		want := light.Background
		if dark {
			want = darkTheme.Background
		}
		got := tt.Image().At(int(r.X+r.W-12), int(r.Y+r.H-12)) // the blank corner under the picture
		gr, gg, gb, _ := got.RGBA()
		if uint8(gr>>8) != want.R || uint8(gg>>8) != want.G || uint8(gb>>8) != want.B {
			t.Fatalf("dark=%v: tile ground is %v, the terminal theme's background %v", dark, got, want)
		}
		click(t, tt, "监视网格")
	}
}
