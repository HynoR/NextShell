// Package automationcli is mygo-automation, the command line client of
// plugins/automation's socket made for agents: compact snapshots of a
// window's elements with short refs, actions on them, and diffs of what
// the window shows once it settled. The command lives in
// plugins/automation/cmd/mygo-automation, which embeds its SKILL.md; the
// package is here so that internal/e2e can drive a real app with it.
package automationcli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Options are what the command takes besides its arguments.
type Options struct {
	// Skill is the SKILL.md that skills prints.
	Skill []byte
	// Dir keeps the sessions' files; empty, it is mygo/automation in the
	// user's cache directory.
	Dir string
}

const usage = `usage: mygo-automation <command> [arguments] [--session name]

Run "mygo-automation skills" for the guide: commands, selectors, refs, --settle.

  open [--socket path]       attach a session to an app (default: $MYGO_AUTOMATION)
  close                      end the session (the app keeps running)
  windows | window <w>       list windows | pick the one commands target
  snapshot [-i] [--full] [--json]
  diff                       what changed since the last snapshot or diff
  press | focus <sel>        fill <sel> <text>     act <sel> <action> [args]
  click <sel>|<x> <y>        pointer move|down|up <sel>|<x> <y>
  wheel <sel>|<x> <y> <dx> <dy>
  type <text>                key <key>...          menu <label>...
  eval <script>|-            wait [<sel>] [--text s] [--gone]
  screenshot <file.png>      skills                version
`

// defaultTimeout is how long --settle and wait wait unless --timeout says.
const defaultTimeout = 10 * time.Second

// Run runs a command line, without the program's name, and returns the
// exit status: 0, 1 when the command failed, 2 when it was used wrong.
func Run(argv []string, stdin io.Reader, stdout, stderr io.Writer, opts Options) int {
	err := run(argv, stdin, stdout, stderr, opts)
	if err == nil {
		return 0
	}
	var ue *usageError
	if errors.As(err, &ue) {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	fmt.Fprintf(stderr, "error: %v\n", err)
	return 1
}

// command is a command of the CLI.
type command struct {
	flags flagSpec
	// noSession marks the commands that run without an open session.
	noSession bool
	run       func(*cmd) error
}

// The flags commands share.
var (
	windowFlag = flagSpec{"window": true}
	actFlags   = flagSpec{"window": true, "settle": false, "timeout": true}
)

func with(specs ...flagSpec) flagSpec {
	out := flagSpec{}
	for _, s := range specs {
		for k, v := range s {
			out[k] = v
		}
	}
	return out
}

var commands map[string]*command

// Commands returns the names of the commands, sorted.
func Commands() []string {
	return slices.Sorted(maps.Keys(commands))
}

func init() {
	pointerFlags := flagSpec{"button": true, "clicks": true, "mods": true}
	commands = map[string]*command{
		"open":       {flags: flagSpec{"socket": true}, noSession: true, run: (*cmd).open},
		"close":      {noSession: true, run: (*cmd).close},
		"windows":    {flags: flagSpec{"json": false}, run: (*cmd).windows},
		"window":     {run: (*cmd).window},
		"snapshot":   {flags: with(windowFlag, flagSpec{"interactive": false, "full": false, "json": false}), run: (*cmd).snapshot},
		"diff":       {flags: windowFlag, run: (*cmd).diff},
		"press":      {flags: actFlags, run: func(c *cmd) error { return c.action("press", "press <sel>") }},
		"focus":      {flags: actFlags, run: func(c *cmd) error { return c.action("focus", "focus <sel>") }},
		"fill":       {flags: actFlags, run: func(c *cmd) error { return c.action("setValue", "fill <sel> <text>") }},
		"act":        {flags: actFlags, run: (*cmd).act},
		"click":      {flags: with(actFlags, pointerFlags), run: (*cmd).click},
		"pointer":    {flags: with(actFlags, pointerFlags), run: (*cmd).pointer},
		"wheel":      {flags: with(actFlags, flagSpec{"precise": false, "mods": true}), run: (*cmd).wheel},
		"type":       {flags: actFlags, run: (*cmd).typeText},
		"key":        {flags: with(actFlags, flagSpec{"down": false, "up": false}), run: (*cmd).key},
		"menu":       {flags: actFlags, run: (*cmd).menu},
		"eval":       {flags: with(windowFlag, flagSpec{"json": false}), run: (*cmd).eval},
		"wait":       {flags: with(actFlags, flagSpec{"text": true, "gone": false}), run: (*cmd).wait},
		"screenshot": {flags: windowFlag, run: (*cmd).screenshot},
		"skills":     {noSession: true, run: func(c *cmd) error { _, err := c.stdout.Write(c.opts.Skill); return err }},
		"version":    {noSession: true, run: func(c *cmd) error { fmt.Fprintln(c.stdout, Version()); return nil }},
	}
}

// cmd is a command running.
type cmd struct {
	stdin          io.Reader
	stdout, stderr io.Writer
	opts           Options
	a              *args
	s              *session
	c              *client
	timeout        time.Duration
}

func run(argv []string, stdin io.Reader, stdout, stderr io.Writer, opts Options) error {
	if len(argv) == 0 || argv[0] == "help" || argv[0] == "-h" || argv[0] == "--help" {
		io.WriteString(stdout, usage)
		if len(argv) == 0 {
			return usagef("no command")
		}
		return nil
	}
	name := argv[0]
	cm, ok := commands[name]
	if !ok {
		return usagef("no command %q: run mygo-automation help", name)
	}
	a, err := parseArgs(argv[1:], with(cm.flags, flagSpec{"session": true}))
	if err != nil {
		return err
	}
	c := &cmd{stdin: stdin, stdout: stdout, stderr: stderr, opts: opts, a: a, timeout: defaultTimeout}
	if v, ok := a.vals["timeout"]; ok {
		if c.timeout, err = parseTimeout(v); err != nil {
			return err
		}
	}
	if cm.noSession {
		return cm.run(c)
	}
	if c.s, err = c.loadSession(); err != nil {
		return err
	}
	var deadline time.Time
	if c.timeout > 0 {
		// A request the app never answers, as from a main thread that
		// hangs, fails rather than holding the agent forever.
		deadline = time.Now().Add(c.timeout + 30*time.Second)
	}
	if c.c, err = dial(c.s.Socket, deadline); err != nil {
		return err
	}
	defer c.c.Close()
	return cm.run(c)
}

// parseTimeout reads a duration, as 5s or 500ms; a bare number is in
// milliseconds, as the protocol's.
func parseTimeout(v string) (time.Duration, error) {
	if ms, err := strconv.Atoi(v); err == nil && ms >= 0 {
		return time.Duration(ms) * time.Millisecond, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < 0 {
		return 0, usagef("--timeout %s: write a duration, as 5s or 500ms (0 waits forever)", v)
	}
	return d, nil
}

// Version is the version of MyGo the command was built from: the
// module's version when go install got it, as v0.8.0, the version an
// app's go.mod requires when installed from the app's module, or (devel).
func Version() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "(devel)"
	}
	mods := append([]*debug.Module{&bi.Main}, bi.Deps...)
	for _, m := range mods {
		if m.Path != "github.com/egoist/mygo" || m.Version == "" {
			continue
		}
		if m.Replace != nil {
			return m.Version + " (replaced by " + m.Replace.Path + ")"
		}
		return m.Version
	}
	return "(devel)"
}

// sessionName is the session's name: --session, else
// $MYGO_AUTOMATION_SESSION, else default.
func (c *cmd) sessionName() string {
	if v, ok := c.a.vals["session"]; ok {
		return v
	}
	if v := os.Getenv("MYGO_AUTOMATION_SESSION"); v != "" {
		return v
	}
	return "default"
}

func (c *cmd) dir() (string, error) {
	if c.opts.Dir != "" {
		return c.opts.Dir, nil
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cache, "mygo", "automation"), nil
}

func (c *cmd) loadSession() (*session, error) {
	dir, err := c.dir()
	if err != nil {
		return nil, err
	}
	return loadSession(dir, c.sessionName())
}

func (c *cmd) ok() error {
	_, err := io.WriteString(c.stdout, "ok\n")
	return err
}

// open attaches a session to the app listening on the socket.
func (c *cmd) open() error {
	if err := c.a.want(0, 0, "open [--socket path] [--session name]"); err != nil {
		return err
	}
	path := c.a.vals["socket"]
	if path == "" {
		path = os.Getenv("MYGO_AUTOMATION")
	}
	if path == "" {
		return usagef("open needs --socket <path>: the path the app's MYGO_AUTOMATION names")
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	dir, err := c.dir()
	if err != nil {
		return err
	}
	name := c.sessionName()
	file, err := sessionPath(dir, name)
	if err != nil {
		return usagef("%v", err)
	}
	cl, err := dial(path, time.Now().Add(time.Minute))
	if err != nil {
		return err
	}
	defer cl.Close()
	var res struct{ Windows []windowInfo }
	if err := cl.call("windows", nil, &res); err != nil {
		return err
	}
	s := &session{Socket: path, Next: 1, name: name, path: file}
	if old, err := loadSession(dir, name); err == nil {
		// Opening again forgets what windows showed, and its refs go
		// stale rather than come back for other elements.
		s.Next = max(old.Next, 1)
	}
	if err := s.save(); err != nil {
		return err
	}
	fmt.Fprintf(c.stdout, "opened %s (session %s)\n", path, name)
	for _, w := range res.Windows {
		fmt.Fprintln(c.stdout, windowLine(w, false))
	}
	return nil
}

// close ends the session, leaving the app running.
func (c *cmd) close() error {
	if err := c.a.want(0, 0, "close [--session name]"); err != nil {
		return err
	}
	dir, err := c.dir()
	if err != nil {
		return err
	}
	name := c.sessionName()
	path, err := sessionPath(dir, name)
	if err != nil {
		return usagef("%v", err)
	}
	if err := os.Remove(path); errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(c.stdout, "no session %s\n", name)
		return nil
	} else if err != nil {
		return err
	}
	fmt.Fprintf(c.stdout, "closed session %s\n", name)
	return nil
}

func (c *cmd) windows() error {
	if err := c.a.want(0, 0, "windows [--json]"); err != nil {
		return err
	}
	raw, err := c.c.callRaw("windows", nil)
	if err != nil {
		return err
	}
	if c.a.bools["json"] {
		return c.printJSON(raw)
	}
	var res struct{ Windows []windowInfo }
	if err := json.Unmarshal(raw, &res); err != nil {
		return err
	}
	if len(res.Windows) == 0 {
		fmt.Fprintln(c.stdout, "(no windows)")
	}
	for _, w := range res.Windows {
		fmt.Fprintln(c.stdout, windowLine(w, w.ID == c.s.Window))
	}
	return nil
}

// window picks the window that commands target unless they name one.
func (c *cmd) window() error {
	if err := c.a.want(1, 1, "window <w1|auto>"); err != nil {
		return err
	}
	id, err := parseWindow(c.a.pos[0])
	if err != nil {
		return err
	}
	var res struct{ Windows []windowInfo }
	if err := c.c.call("windows", nil, &res); err != nil {
		return err
	}
	if id == 0 {
		c.s.Window = 0
		fmt.Fprintln(c.stdout, "commands target the focused window")
		return c.s.save()
	}
	for _, w := range res.Windows {
		if w.ID == id {
			c.s.Window = id
			fmt.Fprintln(c.stdout, windowLine(w, true))
			return c.s.save()
		}
	}
	return &protoError{"no_window", fmt.Sprintf("no window %d: run windows", id)}
}

// parseWindow reads a window, as w2 or 2; auto or 0 is the focused one.
func parseWindow(s string) (int, error) {
	if s == "auto" || s == "focused" {
		return 0, nil
	}
	n, err := strconv.Atoi(strings.TrimPrefix(s, "w"))
	if err != nil || n < 0 {
		return 0, usagef("window %q: write w1, 1 or auto", s)
	}
	return n, nil
}

// targetWindow is the window a command without a ref targets: --window,
// else the session's, else 0 for the focused one.
func (c *cmd) targetWindow() (int, error) {
	if v, ok := c.a.vals["window"]; ok {
		return parseWindow(v)
	}
	return c.s.Window, nil
}

func (c *cmd) printJSON(raw json.RawMessage) error {
	var b bytes.Buffer
	if err := json.Compact(&b, raw); err != nil {
		return err
	}
	b.WriteByte('\n')
	_, err := c.stdout.Write(b.Bytes())
	return err
}

// tree reads the elements of a window, saying how to read a page instead.
func (c *cmd) tree(window int) (*tree, json.RawMessage, error) {
	raw, err := c.c.callRaw("tree", map[string]any{"window": window})
	if err != nil {
		var pe *protoError
		if errors.As(err, &pe) && pe.Code == "unsupported" {
			what, _, _ := strings.Cut(pe.Message, ":")
			return nil, nil, &protoError{pe.Code, what + ", which has no elements here: read it with eval"}
		}
		if errors.As(err, &pe) && pe.Code == "no_window" && window != 0 {
			c.s.forget(window)
			c.s.save()
		}
		return nil, nil, err
	}
	t := &tree{}
	if err := json.Unmarshal(raw, t); err != nil {
		return nil, nil, err
	}
	return t, raw, nil
}

func (c *cmd) snapshot() error {
	if err := c.a.want(0, 0, "snapshot [-i] [--full] [--json] [--window w]"); err != nil {
		return err
	}
	window, err := c.targetWindow()
	if err != nil {
		return err
	}
	t, raw, err := c.tree(window)
	if err != nil {
		return err
	}
	c.s.observe(t)
	if err := c.s.save(); err != nil {
		return err
	}
	if c.a.bools["json"] {
		return c.printJSON(raw)
	}
	writeSnapshot(c.stdout, c.s.Seen[strconv.Itoa(t.Window)], t.Announcements, c.a.bools["interactive"], c.a.bools["full"])
	return nil
}

func (c *cmd) diff() error {
	if err := c.a.want(0, 0, "diff [--window w]"); err != nil {
		return err
	}
	window, err := c.targetWindow()
	if err != nil {
		return err
	}
	return c.printDiff(window)
}

// printDiff prints what changed in the window since the last snapshot or
// diff, and takes what it shows now.
func (c *cmd) printDiff(window int) error {
	t, _, err := c.tree(window)
	if err != nil {
		var pe *protoError
		if errors.As(err, &pe) && pe.Code == "unsupported" {
			// A page settled; it has no elements to compare.
			fmt.Fprintln(c.stdout, "ok (a web page: read it with eval)")
			return nil
		}
		return err
	}
	before := c.s.observe(t)
	if err := c.s.save(); err != nil {
		return err
	}
	lines := diff(before, c.s.Seen[strconv.Itoa(t.Window)], 1)
	if len(lines) == 0 {
		fmt.Fprintln(c.stdout, "(no changes)")
	}
	for _, l := range lines {
		fmt.Fprintln(c.stdout, l)
	}
	writeAnnouncements(c.stdout, t.Announcements)
	return nil
}

// done ends an action on window: with --settle, it waits for the window
// to settle and prints the diff; otherwise it prints ok.
func (c *cmd) done(window int) error {
	if !c.a.bools["settle"] {
		return c.ok()
	}
	if err := c.idle(window); err != nil {
		var pe *protoError
		if !errors.As(err, &pe) || pe.Code != "timeout" {
			return err
		}
		fmt.Fprintf(c.stderr, "warning: the window did not settle in %v; the diff may miss what comes after\n", c.timeout)
	}
	return c.printDiff(window)
}

// idle waits for the window to settle, for --timeout.
func (c *cmd) idle(window int) error {
	return c.c.call("idle", map[string]any{"window": window, "timeout": c.timeout.Milliseconds()}, nil)
}

// target is an element a selector names.
type target struct {
	window int
	id     string
	// ref is the selector when it is a ref, for errors.
	ref string
}

// resolve finds the element a selector names: @e7 (a ref), key=<key>
// (the app's key) or id=<digits> (an element ID, as --json shows).
func (c *cmd) resolve(sel string) (target, error) {
	if n, ok := parseRef(sel); ok {
		w, id, err := c.s.lookup(n)
		if err != nil {
			return target{}, err
		}
		return target{w, id, ref(n)}, nil
	}
	window, err := c.targetWindow()
	if err != nil {
		return target{}, err
	}
	if id, ok := strings.CutPrefix(sel, "id="); ok {
		if _, err := strconv.ParseUint(id, 10, 64); err != nil {
			return target{}, usagef("selector %q: an element ID is a string of digits", sel)
		}
		return target{window: window, id: id}, nil
	}
	k, ok := strings.CutPrefix(sel, "key=")
	if !ok {
		return target{}, usagef("selector %q: write a ref (@e7), key=<key> or id=<digits>", sel)
	}
	want := keyJSON(k)
	t, _, err := c.tree(window)
	if err != nil {
		return target{}, err
	}
	var found []string
	for _, n := range t.Nodes {
		var got bytes.Buffer
		if len(n.Key) > 0 && json.Compact(&got, n.Key) == nil && bytes.Equal(got.Bytes(), want) {
			found = append(found, n.ID)
		}
	}
	switch len(found) {
	case 0:
		return target{}, &protoError{"no_element", fmt.Sprintf("window %d shows no element with the key %s", t.Window, want)}
	case 1:
		return target{window: t.Window, id: found[0]}, nil
	}
	return target{}, &protoError{"invalid_request", fmt.Sprintf("%d elements of window %d have the key %s: use a ref", len(found), t.Window, want)}
}

// gone turns the app's answer that a ref's element or window is gone
// into a stale ref.
func gone(t target, err error) error {
	var pe *protoError
	if t.ref == "" || !errors.As(err, &pe) {
		return err
	}
	switch pe.Code {
	case "no_element":
		return &staleError{t.ref, fmt.Sprintf("window %d no longer shows its element", t.window)}
	case "no_window":
		return &staleError{t.ref, fmt.Sprintf("its window %d closed", t.window)}
	}
	return err
}

// actOn performs an action of assistive technology on an element.
func (c *cmd) actOn(t target, action string, extra map[string]any) error {
	p := map[string]any{"window": t.window, "id": t.id, "action": action}
	for k, v := range extra {
		p[k] = v
	}
	if err := c.c.call("act", p, nil); err != nil {
		return gone(t, err)
	}
	return c.done(t.window)
}

// action runs press, focus and fill.
func (c *cmd) action(action, use string) error {
	n := 1
	if action == "setValue" {
		n = 2
	}
	if err := c.a.want(n, n, use+" [--settle]"); err != nil {
		return err
	}
	t, err := c.resolve(c.a.pos[0])
	if err != nil {
		return err
	}
	var extra map[string]any
	if action == "setValue" {
		extra = map[string]any{"text": c.a.pos[1]}
	}
	return c.actOn(t, action, extra)
}

// actions are the protocol's actions, which act takes.
var actions = []string{"press", "focus", "increment", "decrement", "setValue", "insertText", "selectText", "scrollIntoView", "expand", "collapse"}

func (c *cmd) act() error {
	const use = "act <sel> <action> [<text> | <from> <to>] [--settle]"
	if err := c.a.want(2, 4, use); err != nil {
		return err
	}
	action := c.a.pos[1]
	if !slices.Contains(actions, action) {
		return usagef("no action %q: %s", action, strings.Join(actions, ", "))
	}
	extra := map[string]any{}
	switch rest := c.a.pos[2:]; {
	case action == "setValue" || action == "insertText":
		if len(rest) != 1 {
			return usagef("usage: mygo-automation act <sel> %s <text>", action)
		}
		extra["text"] = rest[0]
	case action == "selectText":
		if len(rest) != 2 {
			return usagef("usage: mygo-automation act <sel> selectText <from> <to>")
		}
		for i, k := range []string{"from", "to"} {
			n, err := strconv.Atoi(rest[i])
			if err != nil || n < 0 {
				return usagef("selectText takes rune offsets, not %q", rest[i])
			}
			extra[k] = n
		}
	case len(rest) > 0:
		return usagef("action %s takes no arguments", action)
	}
	t, err := c.resolve(c.a.pos[0])
	if err != nil {
		return err
	}
	return c.actOn(t, action, extra)
}

// point reads where a pointer goes: an element's center, from pos[0], or
// x and y, from pos[0:2]. It returns the window, the point, and how many
// arguments it read.
func (c *cmd) point(pos []string) (window int, x, y float64, used int, t target, err error) {
	if len(pos) >= 2 {
		if x, err1 := strconv.ParseFloat(pos[0], 64); err1 == nil {
			y, err2 := strconv.ParseFloat(pos[1], 64)
			if err2 != nil {
				return 0, 0, 0, 0, t, usagef("y %q is no number", pos[1])
			}
			window, err = c.targetWindow()
			return window, x, y, 2, t, err
		}
	}
	if len(pos) == 0 {
		return 0, 0, 0, 0, t, usagef("a pointer needs a selector or x and y")
	}
	if t, err = c.resolve(pos[0]); err != nil {
		return 0, 0, 0, 0, t, err
	}
	tr, _, err := c.tree(t.window)
	if err != nil {
		return 0, 0, 0, 0, t, gone(t, err)
	}
	for _, n := range tr.Nodes {
		if n.ID == t.id {
			b := n.Bounds
			return tr.Window, b.X + b.Width/2, b.Y + b.Height/2, 1, t, nil
		}
	}
	return 0, 0, 0, 0, t, gone(t, &protoError{"no_element", fmt.Sprintf("window %d shows no element %s", tr.Window, t.id)})
}

// pointerEvent makes an event of the pointer's flags.
func (c *cmd) pointerEvent(typ string, x, y float64) (inputEvent, error) {
	ev := inputEvent{Type: typ, X: x, Y: y, Button: c.a.vals["button"]}
	if v, ok := c.a.vals["clicks"]; ok {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return ev, usagef("--clicks %q: 1 or more", v)
		}
		ev.Clicks = n
	}
	if v := c.a.vals["mods"]; v != "" {
		ev.Mods = strings.Split(v, ",")
	}
	return ev, nil
}

// input sends events to a window, then ends as actions do.
func (c *cmd) input(window int, t target, evs ...inputEvent) error {
	if err := c.c.call("input", map[string]any{"window": window, "events": evs}, nil); err != nil {
		return gone(t, err)
	}
	return c.done(window)
}

func (c *cmd) click() error {
	const use = "click <sel> | <x> <y> [--button left|right|middle] [--clicks n] [--mods shift,ctrl,alt,super,cmdOrCtrl] [--settle]"
	if err := c.a.want(1, 2, use); err != nil {
		return err
	}
	window, x, y, used, t, err := c.point(c.a.pos)
	if err != nil {
		return err
	}
	if used != len(c.a.pos) {
		return usagef("usage: mygo-automation %s", use)
	}
	ev, err := c.pointerEvent("click", x, y)
	if err != nil {
		return err
	}
	return c.input(window, t, ev)
}

func (c *cmd) pointer() error {
	const use = "pointer move|down|up <sel> | <x> <y> [--button b] [--clicks n] [--mods m] [--settle]"
	if err := c.a.want(2, 3, use); err != nil {
		return err
	}
	typ, ok := map[string]string{"move": "pointerMove", "down": "pointerDown", "up": "pointerUp"}[c.a.pos[0]]
	if !ok {
		return usagef("usage: mygo-automation %s", use)
	}
	window, x, y, used, t, err := c.point(c.a.pos[1:])
	if err != nil {
		return err
	}
	if used != len(c.a.pos)-1 {
		return usagef("usage: mygo-automation %s", use)
	}
	ev, err := c.pointerEvent(typ, x, y)
	if err != nil {
		return err
	}
	return c.input(window, t, ev)
}

func (c *cmd) wheel() error {
	const use = "wheel <sel> <dx> <dy> | <x> <y> <dx> <dy> [--precise] [--mods m] [--settle]"
	if err := c.a.want(3, 4, use); err != nil {
		return err
	}
	at := c.a.pos[:len(c.a.pos)-2]
	window, x, y, used, t, err := c.point(at)
	if err != nil {
		return err
	}
	if used != len(at) {
		return usagef("usage: mygo-automation %s", use)
	}
	var d [2]float64
	for i, s := range c.a.pos[len(at):] {
		if d[i], err = strconv.ParseFloat(s, 64); err != nil {
			return usagef("wheel distance %q is no number", s)
		}
	}
	ev := inputEvent{Type: "wheel", X: x, Y: y, DX: d[0], DY: d[1], Precise: c.a.bools["precise"]}
	if v := c.a.vals["mods"]; v != "" {
		ev.Mods = strings.Split(v, ",")
	}
	return c.input(window, t, ev)
}

func (c *cmd) typeText() error {
	if err := c.a.want(1, 1, "type <text> [--settle]"); err != nil {
		return err
	}
	window, err := c.targetWindow()
	if err != nil {
		return err
	}
	if c.a.pos[0] == "" {
		return usagef("type needs text")
	}
	return c.input(window, target{}, inputEvent{Type: "text", Text: c.a.pos[0]})
}

func (c *cmd) key() error {
	if err := c.a.want(1, -1, "key <key>... [--down|--up] [--settle], as key Enter or key CmdOrCtrl+A Backspace"); err != nil {
		return err
	}
	typ := "key"
	switch {
	case c.a.bools["down"] && c.a.bools["up"]:
		return usagef("key takes --down or --up, not both")
	case c.a.bools["down"]:
		typ = "keyDown"
	case c.a.bools["up"]:
		typ = "keyUp"
	}
	window, err := c.targetWindow()
	if err != nil {
		return err
	}
	evs := make([]inputEvent, len(c.a.pos))
	for i, k := range c.a.pos {
		evs[i] = inputEvent{Type: typ, Key: k}
	}
	return c.input(window, target{}, evs...)
}

func (c *cmd) menu() error {
	if err := c.a.want(1, -1, "menu <label>... [--settle], as menu File \"New Window\""); err != nil {
		return err
	}
	window, err := c.targetWindow()
	if err != nil {
		return err
	}
	if err := c.c.call("menu", map[string]any{"window": window, "path": c.a.pos}, nil); err != nil {
		return err
	}
	return c.done(window)
}

func (c *cmd) eval() error {
	if err := c.a.want(1, 1, "eval <script> | - [--json] [--window w]"); err != nil {
		return err
	}
	script := c.a.pos[0]
	if script == "-" {
		b, err := io.ReadAll(c.stdin)
		if err != nil {
			return err
		}
		script = string(b)
	}
	window, err := c.targetWindow()
	if err != nil {
		return err
	}
	raw, err := c.c.callRaw("eval", map[string]any{"window": window, "script": script})
	if err != nil {
		var pe *protoError
		if errors.As(err, &pe) && pe.Code == "unsupported" {
			what, _, _ := strings.Cut(pe.Message, ":")
			return &protoError{pe.Code, what + ": read it with snapshot (scripts reach only windows showing a web page, not web views inside native UI)"}
		}
		return err
	}
	var res struct{ Value json.RawMessage }
	if err := json.Unmarshal(raw, &res); err != nil {
		return err
	}
	if len(res.Value) == 0 {
		res.Value = json.RawMessage("null")
	}
	var s string
	if !c.a.bools["json"] && json.Unmarshal(res.Value, &s) == nil {
		_, err := fmt.Fprintln(c.stdout, s)
		return err
	}
	return c.printJSON(res.Value)
}

// pollEvery is how often wait reads the window again while what it
// waits for has not come.
const pollEvery = 50 * time.Millisecond

func (c *cmd) wait() error {
	if err := c.a.want(0, 1, "wait [<sel>] [--text s] [--gone] [--timeout d] [--settle]"); err != nil {
		return err
	}
	text, hasText := c.a.vals["text"]
	if !hasText && len(c.a.pos) == 0 {
		if c.a.bools["gone"] {
			return usagef("wait --gone needs a selector or --text")
		}
		window, err := c.targetWindow()
		if err != nil {
			return err
		}
		if err := c.idle(window); err != nil {
			return err
		}
		return c.done(window)
	}
	if hasText && len(c.a.pos) > 0 {
		return usagef("wait takes a selector or --text, not both")
	}
	window, err := c.targetWindow()
	if err != nil {
		return err
	}
	// match tells whether the window shows what is waited for.
	var match func(n *node) bool
	var what string
	if hasText {
		what = "text " + strconv.Quote(text)
		match = func(n *node) bool { return strings.Contains(n.Label, text) || strings.Contains(n.Value, text) }
	} else {
		sel := c.a.pos[0]
		what = sel
		if n, ok := parseRef(sel); ok {
			w, id, err := c.s.lookup(n)
			if err != nil {
				return err
			}
			window = w
			match = func(n *node) bool { return n.ID == id }
		} else if id, ok := strings.CutPrefix(sel, "id="); ok {
			match = func(n *node) bool { return n.ID == id }
		} else if k, ok := strings.CutPrefix(sel, "key="); ok {
			want := keyJSON(k)
			match = func(n *node) bool {
				var got bytes.Buffer
				return len(n.Key) > 0 && json.Compact(&got, n.Key) == nil && bytes.Equal(got.Bytes(), want)
			}
		} else {
			return usagef("selector %q: write a ref (@e7), key=<key> or id=<digits>", sel)
		}
	}
	want := !c.a.bools["gone"]
	start := time.Now()
	for {
		// Read the condition even while the window keeps drawing.
		// Only --settle waits for idle, after the condition is met.
		t, _, err := c.tree(window)
		if err != nil {
			return err
		}
		found := slices.ContainsFunc(t.Nodes, func(n node) bool { return match(&n) })
		if found == want {
			return c.done(window)
		}
		if c.timeout > 0 && time.Since(start) > c.timeout {
			state := "does not show"
			if !want {
				state = "still shows"
			}
			return &protoError{"timeout", fmt.Sprintf("window %d %s %s after %v", t.Window, state, what, c.timeout)}
		}
		time.Sleep(pollEvery)
	}
}

func (c *cmd) screenshot() error {
	if err := c.a.want(1, 1, "screenshot <file.png> [--window w]"); err != nil {
		return err
	}
	window, err := c.targetWindow()
	if err != nil {
		return err
	}
	var res struct {
		PNG           []byte
		Width, Height int
	}
	if err := c.c.call("capture", map[string]any{"window": window}, &res); err != nil {
		return err
	}
	path := c.a.pos[0]
	if err := os.WriteFile(path, res.PNG, 0o644); err != nil {
		return err
	}
	_, err = fmt.Fprintf(c.stdout, "%s %dx%d\n", path, res.Width, res.Height)
	return err
}
