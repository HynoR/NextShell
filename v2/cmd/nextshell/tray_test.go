package main

import (
	"errors"
	"testing"

	"github.com/egoist/mygo"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/desktop"
	"nextshell.dev/v2/internal/model"
)

// fakeIcon records what the host does to its tray icon.
type fakeIcon struct {
	calls     []string
	menus     []*mygo.Menu
	click     func()
	right     func()
	destroyed bool
}

func (f *fakeIcon) SetMenu(m *mygo.Menu) {
	f.menus = append(f.menus, m)
	if m == nil {
		f.calls = append(f.calls, "remove menu")
	} else {
		f.calls = append(f.calls, "set menu")
	}
}
func (f *fakeIcon) PopUpMenu()                    { f.calls = append(f.calls, "pop up") }
func (f *fakeIcon) OnClick(fn func()) func()      { f.click = fn; return func() {} }
func (f *fakeIcon) OnRightClick(fn func()) func() { f.right = fn; return func() {} }
func (f *fakeIcon) Destroy()                      { f.destroyed = true }

type trayFixture struct {
	tray  *tray
	app   *appui.App
	icons []*fakeIcon
	shown int
	quits int
}

// newTrayFixture is the app on an in-memory profile with the hosts called names.
func newTrayFixture(t *testing.T, names ...string) *trayFixture {
	t.Helper()
	p, e := desktop.Open(":memory:")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(p.Shutdown)
	for _, name := range names {
		c := model.Connection{ID: p.NewID(), Name: name, Host: name + ".example", Port: 22, User: "root", Auth: "password", Encoding: "utf-8", Monitor: true}
		if e := p.SaveConnection(c, nil); e != nil {
			t.Fatal(e)
		}
	}
	f := &trayFixture{app: appui.New(p)}
	f.app.TrayAvailable = true
	p.SetQuit(func() { f.quits++ })
	f.tray = &tray{app: f.app, terminating: func() bool { return false }, show: func() { f.shown++ }, create: func() (trayIcon, error) {
		icon := &fakeIcon{}
		f.icons = append(f.icons, icon)
		return icon, nil
	}}
	return f
}

func (f *trayFixture) icon(t *testing.T) *fakeIcon {
	t.Helper()
	if len(f.icons) == 0 {
		t.Fatal("no tray icon was made")
	}
	return f.icons[len(f.icons)-1]
}

func TestTrayExistsOnlyWhilePreferencesNeedIt(t *testing.T) {
	f := newTrayFixture(t)
	f.tray.Sync()
	if len(f.icons) != 0 || f.tray.Hides(true) {
		t.Fatal("an icon with both options off")
	}
	f.app.Prefs.CloseToTray = true
	f.tray.Sync()
	f.tray.Sync()
	if len(f.icons) != 1 || !f.tray.Hides(true) || f.tray.Hides(false) {
		t.Fatal("icons made", len(f.icons))
	}
	f.app.Prefs.CloseToTray, f.app.Prefs.MinimizeToTray = false, true // either option keeps the same icon
	f.tray.Sync()
	if len(f.icons) != 1 || f.icons[0].destroyed {
		t.Fatal("the icon was replaced")
	}
	f.app.Prefs.MinimizeToTray = false
	f.tray.Sync()
	if !f.icons[0].destroyed || f.tray.Hides(true) {
		t.Fatal("the icon stayed with both options off")
	}
	f.app.Prefs.MinimizeToTray = true
	f.tray.Sync()
	if len(f.icons) != 2 {
		t.Fatal("not made again", len(f.icons))
	}
}

// Where the system has no tray the options go and the window never hides: nothing could bring it back.
func TestNoTrayMeansNoOptionsAndNoHiding(t *testing.T) {
	f := newTrayFixture(t)
	f.tray.create = func() (trayIcon, error) { return nil, errors.New("tray icons need libayatana-appindicator3") }
	f.app.Prefs.CloseToTray, f.app.Prefs.MinimizeToTray = true, true
	f.tray.Sync()
	if f.app.TrayAvailable || f.tray.Hides(true) || f.tray.closeHides() {
		t.Fatal("hides without an icon")
	}
	calls := 0
	f.tray.create = func() (trayIcon, error) { calls++; return &fakeIcon{}, nil }
	f.tray.Sync()
	if calls != 0 {
		t.Fatal("tried again after the system said no")
	}

	// macOS and Windows have a tray; elsewhere it is tried once, and the probe leaves nothing behind.
	g := newTrayFixture(t)
	g.tray.Probe("darwin")
	g.tray.Probe("windows")
	if len(g.icons) != 0 {
		t.Fatal("probed a system that always has a tray")
	}
	g.tray.Probe("linux")
	if len(g.icons) != 1 || !g.icons[0].destroyed || !g.app.TrayAvailable {
		t.Fatal("probe", len(g.icons))
	}
	h := newTrayFixture(t)
	h.tray.create = func() (trayIcon, error) { return nil, errors.New("unsupported") }
	h.tray.Probe("freebsd")
	if h.app.TrayAvailable {
		t.Fatal("tray offered where none exists")
	}
}

func TestClickingTheIconShowsTheWindow(t *testing.T) {
	f := newTrayFixture(t)
	f.app.Prefs.CloseToTray = true
	f.tray.Sync()
	f.icon(t).click()
	if f.shown != 1 {
		t.Fatal("shown", f.shown)
	}
}

func labels(m *mygo.Menu) []string {
	var out []string
	for _, it := range m.Items() {
		if it.Type == mygo.MenuItemSeparator {
			out = append(out, "-")
		} else {
			out = append(out, it.Label)
		}
	}
	return out
}

func TestTrayMenuListsWindowRecentHostsTerminalAndQuit(t *testing.T) {
	want := func(got []string, label ...string) {
		t.Helper()
		if len(got) != len(label) {
			t.Fatalf("menu %q, want %q", got, label)
		}
		for i := range got {
			if got[i] != label[i] {
				t.Fatalf("menu %q, want %q", got, label)
			}
		}
	}
	want(labels(newTrayFixture(t).tray.menu()), "显示窗口", "-", "新建本地终端", "-", "退出")

	f := newTrayFixture(t, "alpha", "beta")
	m := f.tray.menu()
	want(labels(m), "显示窗口", "-", "最近连接", "alpha", "beta", "-", "新建本地终端", "-", "退出")
	items := m.Items()
	if !items[2].Disabled || items[3].Subtitle != "root@alpha.example:22" {
		t.Fatal("heading or login", items[2].Disabled, items[3].Subtitle)
	}
	// Every item brings the window forward first: what it asks for is drawn there.
	for _, it := range []*mygo.MenuItem{items[0], items[3], items[6]} {
		before := f.shown
		it.Click(it, nil)
		if f.shown != before+1 {
			t.Fatalf("%q did not show the window", it.Label)
		}
	}
}

// Quitting from the tray shows the window with the question when sessions are open, and quits when none is.
func TestTrayQuitUsesTheQuitGuard(t *testing.T) {
	f := newTrayFixture(t)
	quit := f.tray.menu().Items()[4]
	quit.Click(quit, nil)
	if f.quits != 1 || f.shown != 1 {
		t.Fatal("nothing open: quits", f.quits, "shown", f.shown)
	}
	f.app.Tabs = append(f.app.Tabs, &appui.Session{ID: "one", Title: "one"})
	quit.Click(quit, nil)
	if f.quits != 1 || f.shown != 2 || !f.app.ConfirmExit {
		t.Fatal("a session is open: quits", f.quits, "shown", f.shown, "asked", f.app.ConfirmExit)
	}
}

func TestMenuOfTheIconIsPoppedUpFreshOrKeptForThePanel(t *testing.T) {
	defer func(old bool) { menuIsStatic = old }(menuIsStatic)

	menuIsStatic = false
	f := newTrayFixture(t)
	f.app.Prefs.CloseToTray = true
	f.tray.Sync()
	icon := f.icon(t)
	if len(icon.calls) != 0 || icon.right == nil {
		t.Fatal("a menu is attached, so clicks would never reach the window:", icon.calls)
	}
	icon.right()
	if got := icon.calls; len(got) != 3 || got[0] != "set menu" || got[1] != "pop up" || got[2] != "remove menu" || len(icon.menus) != 2 || icon.menus[0] == nil || icon.menus[1] != nil {
		t.Fatal("right click", got)
	}

	menuIsStatic = true
	g := newTrayFixture(t)
	g.app.Prefs.CloseToTray = true
	g.tray.Sync()
	panel := g.icon(t)
	if len(panel.calls) != 1 || panel.calls[0] != "set menu" || panel.right != nil {
		t.Fatal("panel icon", panel.calls)
	}
	g.tray.Refresh()
	if len(panel.calls) != 2 {
		t.Fatal("the panel was not given the new menu", panel.calls)
	}
}

// Closing hides the window only with the option on and an icon to bring it back, never while quitting or
// when the system is ending the process.
func TestClosingHidesOnlyWhenNothingIsQuitting(t *testing.T) {
	f := newTrayFixture(t)
	terminating := false
	f.tray.terminating = func() bool { return terminating }
	if f.tray.closeHides() {
		t.Fatal("hides with the option off")
	}
	f.app.Prefs.CloseToTray = true
	if f.tray.closeHides() {
		t.Fatal("hides without an icon")
	}
	f.tray.Sync()
	if !f.tray.closeHides() {
		t.Fatal("does not hide")
	}
	f.tray.quitting.Store(true) // ⌘Q and the tray's 退出 close the windows too
	if f.tray.closeHides() {
		t.Fatal("hides while quitting")
	}
	f.tray.quitting.Store(false) // the quit was cancelled by the confirmation
	if !f.tray.closeHides() {
		t.Fatal("still quitting after the quit was cancelled")
	}
	terminating = true
	if f.tray.closeHides() {
		t.Fatal("hides while the system ends the process")
	}
	// Only minimize to tray on: closing is not affected.
	terminating = false
	f.app.Prefs.CloseToTray, f.app.Prefs.MinimizeToTray = false, true
	if f.tray.closeHides() || !f.tray.Hides(f.app.Prefs.MinimizeToTray) {
		t.Fatal("the two options mix up")
	}
}

// The single-instance lock is taken only when a tray option may hide the window, and not for a trial
// profile, which runs beside the real one.
func TestSingleInstanceLockOnlyForTheRealProfileWithATrayOption(t *testing.T) {
	prefs := func(minimize, closing bool) model.Preferences {
		p := model.DefaultPreferences()
		p.MinimizeToTray, p.CloseToTray = minimize, closing
		return p
	}
	for _, c := range []struct {
		what  string
		prefs model.Preferences
		db    string
		want  bool
	}{
		{"both options off", prefs(false, false), "", false},
		{"close to tray", prefs(false, true), "", true},
		{"minimize to tray", prefs(true, false), "", true},
		{"both", prefs(true, true), "", true},
		{"trial profile", prefs(true, true), "/tmp/trial.db", false},
		{"trial profile, options off", prefs(false, false), "/tmp/trial.db", false},
	} {
		if got := wantsSingleInstance(c.prefs, options{db: c.db}); got != c.want {
			t.Errorf("%s: %v, want %v", c.what, got, c.want)
		}
	}
}

// A later launch shows the window of the one that holds the lock, as the tray's 显示窗口 does, on the main
// thread: the window made after the handler was registered is the one shown.
func TestSecondLaunchShowsTheWindow(t *testing.T) {
	f := newTrayFixture(t)
	ran := 0
	handler := f.tray.secondInstance(func(fn func()) { ran++; fn() })
	handler([]string{"--some-arg"}, "/somewhere")
	if ran != 1 || f.shown != 1 {
		t.Fatal("ran", ran, "shown", f.shown)
	}
	late := 0
	f.tray.show = func() { late++ } // the main window is made after the handler is registered
	handler(nil, "")
	if late != 1 {
		t.Fatal("the window made later was not shown", late)
	}
}

// A tray option turned on in settings needs the single-instance lock too: it is taken then, once, and the
// window may hide only after that.
func TestOptionTurnedOnLaterTakesTheLockBeforeTheWindowCanHide(t *testing.T) {
	f := newTrayFixture(t)
	locks := 0
	f.tray.shared, f.tray.lock = true, func() bool { locks++; return true }
	f.tray.Sync() // both options off: nothing to protect yet
	if locks != 0 || len(f.icons) != 0 {
		t.Fatal("lock taken with both options off", locks)
	}
	f.app.Prefs.CloseToTray = true
	f.tray.Sync()
	f.tray.Sync()
	f.app.Prefs.MinimizeToTray = true
	f.tray.Sync()
	if locks != 1 || len(f.icons) != 1 || !f.tray.closeHides() {
		t.Fatal("locks", locks, "icons", len(f.icons), "hides", f.tray.closeHides())
	}
	// Off and on again: the lock is still held, and mygo is not asked again.
	f.app.Prefs.CloseToTray, f.app.Prefs.MinimizeToTray = false, false
	f.tray.Sync()
	f.app.Prefs.CloseToTray = true
	f.tray.Sync()
	if locks != 1 || len(f.icons) != 2 || !f.tray.closeHides() {
		t.Fatal("locks", locks, "icons", len(f.icons))
	}
	// A run that took the lock at startup does not ask again when the option is already on.
	g := newTrayFixture(t)
	asked := 0
	g.tray.shared, g.tray.lock = true, func() bool { asked++; return true }
	g.app.Prefs.CloseToTray = true
	if !g.tray.takeLock() || asked != 1 {
		t.Fatal("startup lock", asked)
	}
	g.tray.Sync()
	if asked != 1 || len(g.icons) != 1 {
		t.Fatal("asked again", asked)
	}
}

// Where the lock cannot be had (another instance holds it) the window never hides in this run: no icon, the
// settings say the option waits for a restart, and the lock is tried again when the option is next turned on.
func TestWithoutTheLockTheWindowStaysVisible(t *testing.T) {
	f := newTrayFixture(t)
	locks, held := 0, false
	f.tray.shared, f.tray.lock = true, func() bool { locks++; return held }
	f.app.Prefs.CloseToTray = true
	f.tray.Sync()
	if locks != 1 || len(f.icons) != 0 || f.tray.closeHides() || f.tray.Hides(true) {
		t.Fatal("locks", locks, "icons", len(f.icons), "hides", f.tray.closeHides())
	}
	if !f.app.TrayAvailable {
		t.Fatal("the option disappeared instead of waiting for a restart")
	}
	f.tray.Sync()
	if locks != 2 || len(f.icons) != 0 {
		t.Fatal("not tried again", locks)
	}
	held = true
	f.tray.Sync()
	if len(f.icons) != 1 || !f.tray.closeHides() {
		t.Fatal("the lock came free but the icon did not")
	}
}

// A trial profile (--db) runs beside the real one and never asks for the lock.
func TestTrialProfileHidesWithoutTheLock(t *testing.T) {
	f := newTrayFixture(t)
	f.tray.lock = func() bool { t.Fatal("a trial profile took the lock"); return false }
	f.app.Prefs.CloseToTray = true
	f.tray.Sync()
	if len(f.icons) != 1 || !f.tray.closeHides() {
		t.Fatal("trial profile cannot hide", len(f.icons))
	}
}
