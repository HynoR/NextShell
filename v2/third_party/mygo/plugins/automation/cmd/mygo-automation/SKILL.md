---
name: mygo-automation
description: Drive a running MyGo app from the shell. Read the elements of its native UI as compact text with short refs, press, fill, click, type, send keys and wheel events, choose application menu items, evaluate scripts in windows showing a web page, wait for the UI to settle and read what changed, save screenshots.
---

# mygo-automation

`mygo-automation` drives a running MyGo app through the socket of its
automation plugin (`github.com/egoist/mygo/plugins/automation`). It is
the guide of the version you run, as `mygo-automation version` tells. If
the app's MyGo is another version, install the matching one (see
"Installing").

The loop is: **snapshot → act with `--settle` → read the diff → act
again**. Diffs carry the refs you act on next, so a new snapshot is
seldom needed.

```sh
mygo-automation open --socket /tmp/app.sock
mygo-automation snapshot -i
# @e2 [button] "Add" key=add
# @e3 [textField] "Name" key=name
mygo-automation press @e2 --settle
# - @e1 [text] "Count: 0"
# + @e1 [text] "Count: 1"
# = @e2 [button] "Add" key=add
mygo-automation fill @e3 "Ada" --settle
mygo-automation screenshot /tmp/app.png
mygo-automation close
```

## The app

The app must link the plugin and be started with `MYGO_AUTOMATION`, the
path of the socket it listens on. A file of the app's, kept out of its
release by a build tag:

```go
//go:build e2e

package main

import _ "github.com/egoist/mygo/plugins/automation"
```

```sh
go build -tags e2e -o /tmp/app . && MYGO_AUTOMATION=/tmp/app.sock /tmp/app &
```

Any way of starting it works if the variable reaches it; `mygo dev`
passes its environment to the app. Builds without the inspector
(`-tags mygo_noinspector`, release builds of `mygo build` without
`-debug`) leave the plugin out: they say so on standard error and never
listen. The socket is the user's alone. Pick a path in a directory only
the user can write to; nothing else is needed to connect, and there is
no discovery: the path is the one you gave `MYGO_AUTOMATION`.

## Sessions

- `open [--socket <path>]` attaches a session to the app and lists its
  windows. Without `--socket` it takes `$MYGO_AUTOMATION`. Opening again
  keeps the session's name but forgets what it saw: every ref given
  before goes stale.
- `close` ends the session and forgets its refs. **The app keeps
  running.** To quit it, choose its menu item (`menu <App> Quit`, when
  it has one) or end its process yourself.
- Every command takes `--session <name>` (default:
  `$MYGO_AUTOMATION_SESSION`, else `default`), letters, digits, `.`,
  `_` and `-`. Sessions keep their socket, chosen window and refs apart,
  in files of their own in the user's cache directory
  (`mygo/automation/<name>.json`). Give each agent, or each app, a session
  of its own; run one command at a time in a session.

## Reading the UI

`snapshot` prints the elements of a window of native UI, one per line, a
child indented under the element shown above it, in tree order:

```
@e1 [dialog] "Settings"
  @e2 [checkBox] "Dark mode" key=dark checked focused
  @e3 [textField] "Name" value="Ada" key=name
  @e4 [slider] "Volume" value=0.25
@e5 [button] "Save" disabled
announced "Saved"
```

A line is the element's ref, `[role]`, its label in quotes, then what
it has of `value=`, `placeholder=` (when it has no label), `key=` (the
app's own name for it), its states, `current=` (what it is the current
one of, as the last crumb of a breadcrumb trail is the `page`),
`autocomplete=` (what a field's suggestions complete as you type: `list`
for a popup of them, `inline` for the text completed in place, or
`both`), `haspopup=`, `roledescription=` and `focused`. Empty fields are left
out, and so is `focusable`, which every control is. `webview` marks a web
view inside native UI: its page has no elements here. `announced` lines
are what the app announced to screen readers. Labels and values longer
than 120 characters are cut, ending in `…" (N more)`.
`keyshortcuts=` (after `key=`) is the keys the app says press or focus
the element, as screen readers read them (`aria-keyshortcuts`): `"⌘K"`
on macOS, `"Ctrl+K"` elsewhere, several separated by spaces.

- `-i` (`--interactive`) keeps controls and elements with actions:
  buttons, fields, check boxes, links, list items you can press, and so
  on. Elements left out still get refs, and diffs show them.
- `--full` adds placeholders, descriptions, ranges (`range=0..1
  step=0.1`), text selections (`selection=3,3`, in characters), levels,
  positions in sets (`item=2/5`), the size of a table or a grid and
  the place of a row or a cell in it as the app gave them
  (`rowcount=10001 colcount=5`, `rowindex=4122 colindex=2`), the rows
  and columns a cell covers (`colspan=2`), actions, `bounds=x,y,wxh` and the full
  element ID (`id=…`), and writes long text whole.
- `--json` prints the plugin's answer as it is (IDs, keys, bounds, all
  fields) on one line. It still takes the window's elements for later
  diffs and gives refs, which it does not print.
- `--window <w>` reads another window (see "Windows and pages").

Roles are `group`, `text`, `heading`, `button`, `link`, `checkBox`,
`radio`, `switch`, `slider`, `progress`, `meter`, `stepper`,
`textField`, `comboBox`, `popUpButton`, `menuButton`, `toggleButton`,
`image`, `list`, `listItem`, `table`, `grid`, `treeGrid`, `row`, `cell`, `columnHeader`,
`tree`, `treeItem`, `tabList`, `tab`, `scroll`, `dialog`, `alertDialog`,
`popup`, `tooltip`, `splitter`, `status`, `toolbar`, `radioGroup`,
`disclosure`, `colorWell`, `menu`, `menuBar`, `menuItem`,
`menuItemCheckBox`, `menuItemRadio`, `region`, `log`, `timer`, `alert`,
`separator`, `rowHeader`, `search`, `article`, `navigation`, `deletion`
and `insertion`. States are `disabled`, `checked`,
`mixed`, `selected`, `expanded`, `expandable`, `multiline`, `password`,
`readOnly`, `selectable`, `multiselectable`, `offscreen`, `segment`,
`search`, `invalid`, `sortAscending`, `sortDescending`, `sortOther`,
`vertical`, `horizontal` and `busy`. `current=` is `true`, `page`, `step`, `location`,
`date` or `time`; `haspopup=` the kind of popup the element opens
(`menu`, `listbox`, `tree`, `grid` or `dialog`), and `roledescription=`
what the app calls its role.
Only what assistive technology reads is there: rows and columns that
only lay out others have no line.

## Selectors

Commands that act on an element take one of:

| Selector | Names |
| --- | --- |
| `@e7` (or `e7`) | the element a snapshot or diff gave that ref, in its window |
| `key=<key>` | the one element of the window whose key the app set (`Element.Key`, `Context.Key`) |
| `id=<digits>` | the element with that ID, as `--full` and `--json` show |

Write keys as lines show them: `key=add`, `key=7` (a number), `key=true`
(a boolean), `key='"7"'` or `key='"two words"'` (strings that need JSON's
quotes, quoted again for the shell). A key two elements share is refused:
use a ref. Prefer keys when the app gives them; they last across runs.

## Refs

- Refs come from `snapshot`, `diff` and `--settle`. Use the refs of the
  latest output for a window.
- An element keeps its ref while the window shows it: after `press @e2
  --settle`, `@e2` still names the same button, printed or not.
- New elements get new numbers. A ref is never given twice in a session,
  so it can't come to name another element.
- A ref goes **stale** once a snapshot or diff of its window shows its
  element gone, or the app answers that it is gone. A stale ref is
  refused, never retargeted:
  `error: stale ref @e4: its element is gone; use a ref from the latest snapshot or diff (run snapshot)`.
  An element that comes back gets a new ref.
- A ref carries its window: commands on it go there, whatever `window`
  or `--window` says.
- `open` again, or `close`, ends every ref of the session.

## Acting

| Command | Does |
| --- | --- |
| `press <sel>` | presses, as assistive technology does: clicks a button, toggles a check box, opens a menu button |
| `focus <sel>` | gives the element the keyboard focus |
| `fill <sel> <text>` | replaces a text field's text (`""` empties it) |
| `act <sel> <action> [args]` | any action: `press`, `focus`, `increment`, `decrement`, `setValue <text>`, `insertText <text>` (at the caret), `selectText <from> <to>` (characters), `scrollIntoView`, `expand`, `collapse` |
| `click <sel>` or `click <x> <y>` | a pointer click at the element's center, or at a point; `--button left\|right\|middle`, `--clicks 2` for a double click, `--mods shift,ctrl,alt,super,cmdOrCtrl` |
| `pointer move\|down\|up <sel>\|<x> <y>` | one pointer event, with the same flags, for drags and hovers |
| `wheel <sel>\|<x> <y> <dx> <dy>` | scrolls by `dx`, `dy` DIPs (negative `dy` scrolls up); `--precise` as a touchpad, `--mods` |
| `type <text>` | types text into the focused element, as the keyboard does |
| `key <key>...` | presses and releases keys in order, written as menu accelerators: `Enter`, `Escape`, `Tab`, `Shift+Tab`, `Backspace`, `Delete`, `Up`, `PageDown`, `Home`, `F2`, `a`, `CmdOrCtrl+A`, `CmdOrCtrl+Shift+Z`; `--down` or `--up` sends only the press or the release |
| `menu <label>...` | chooses an item of the application menu by the labels along its path: `menu File "New Window"` |

Points are in DIPs, from the top left corner of the window's content, as
`bounds=` shows. Prefer `press` and `fill` to `click` and `type`: they
need no coordinates and no focus. A key types no text: send text with
`type`. A modifier alone (`key Shift`) is no key. Without `--settle`, an
action prints `ok` once the app took it, which may be before it drew.

`press`, `focus`, `fill`, `act`, `click`, `pointer`, `wheel`, `type`,
`key`, `menu` and `wait` take `--window <w>` (for coordinates, keys,
text and menus), `--settle` and `--timeout <d>`.

## Settle and diff

`--settle` waits until the window has drawn every frame it asked for (the
plugin's `idle`), for `--timeout` (default `10s`; `500ms`, a number of
milliseconds, or `0` to wait as long as it takes), then prints what
changed since the last snapshot or diff of that window, and takes what it
shows now for the next diff:

```
= @e2 [button] "Add" key=add
- @e3 [textField] "Name" key=name
+ @e3 [textField] "Name" value="Ada" key=name focused
+ @e4 [text] "Hello, Ada"
- @e9 [dialog] "Unsaved changes"
```

- `+` an element that came, with its new ref, or an element as it is now.
- `-` an element that went, with its now stale ref, or an element as it
  was before: a changed element shows as `-` then `+`, with one ref.
- `=` an unchanged element next to a change, for context, with its ref.
- `(no changes)` when nothing changed. Moves and bounds are not changes.

A window that never settles, as one with a spinner, warns on standard
error after `--timeout` and prints the diff anyway. `diff` prints the
same without acting or waiting. An action on a ref diffs the ref's
window. One that names no window (`type`, `key`, `menu`, points) while
the session targets the focused window diffs the window focused once it
settled: a window it opened comes as `+` lines.

## Waiting

- `wait` waits for the window to settle (`--timeout`, as above).
- `wait <sel>` waits until the window shows the element; `wait --text
  <s>` until an element's label or value contains `s` (case sensitive).
  These check the condition while the window keeps drawing; `--settle`
  waits for it to settle after the condition is met.
- `--gone` waits until it no longer shows it, as a dialog closing.
- It prints `ok`, or with `--settle` the diff. After `--timeout` it fails
  with `timeout`. Never sleep: wait for what you expect.

## Windows and pages

- `windows` lists the windows: `w1 [content] "Title" focused`; `[content]`
  shows native UI, `[page]` a web page, with its `url=`; `hidden` and
  `selected` mark windows not shown and the one the session targets.
  `--json` prints the plugin's answer.
- `window <w>` makes commands target window `w` (`w2` or `2`) unless they
  name one with `--window`; `window auto` goes back to the focused window,
  the default, else the first.
- Native UI windows have elements: `snapshot`, selectors, `press`, input.
- Page windows have none: `snapshot` and input fail with `unsupported`.
  Read and drive them with `eval <script>`, which prints a string as it
  is and other values as JSON (`--json`: always JSON); `eval -` reads the
  script from standard input. An expression's value is printed, promises
  awaited, or what statements `return`.
- A web view inside native UI is one `webview` element: neither
  `snapshot` nor `eval` reaches its page.

## Screenshots

`screenshot <file.png> [--window <w>]` saves the window as a PNG file and
prints `<file> <width>x<height>` in device pixels. It never prints the
image. Take one only when text does not tell you enough.

## Output and errors

Output goes to standard output; errors go to standard error as
`error: <message>`, with exit status 1, or 2 for a command line used
wrong. Errors from the app start with its code:

| Error | Means | Do |
| --- | --- | --- |
| `no session "<name>"` | no `open` for this session | `open --socket <path>` |
| `cannot reach the app at <path>` | nothing listens: the app quit, never set `MYGO_AUTOMATION`, or lacks the plugin | start the app, then `open` |
| `stale ref @eN` | the ref's element or window is gone | `snapshot`, use a ref it gives |
| `no_window` | no window has that ID, or the app has none | `windows` |
| `no_element` | the window shows no such element or key | `snapshot` |
| `unsupported` | the window cannot do it, as `snapshot` of a page or `eval` in native UI | use the other one |
| `timeout` | `wait` did not see what it waited for, or the window never settled | look at a `snapshot`; raise `--timeout` |
| `invalid_request` | a value the app can't use, as an unknown key name | fix the command |
| `failed` | the app could not do it, as a script that threw, or a menu item it lacks | read the message |

## Limits

- Context menus, and the menus of the Dock and of tray icons, can't be
  chosen: don't open a context menu with `click --button right`.
- Settling waits for frames asked for, not for timers the app set
  (`Context.After`) or work it does in the background: use `wait <sel>`
  or `wait --text` for those.
- On Linux and Windows, `menu` chooses from the window's menu bar, which
  shows the application menu.
- The command does not start or quit the app.

## Installing

```sh
# In the app's module directory: the version of MyGo its go.mod requires.
go install github.com/egoist/mygo/plugins/automation/cmd/mygo-automation
# Anywhere else: name that version, which this prints in the app's module.
go list -m github.com/egoist/mygo
go install github.com/egoist/mygo/plugins/automation/cmd/mygo-automation@vX.Y.Z
```

`version` prints the version of MyGo the command was built from (as
`v0.8.0`; `(devel)` or a pseudo-version for a build from a checkout), and
`skills` prints this guide. `go install` puts `mygo-automation` in
`$(go env GOBIN)`, else
`$(go env GOPATH)/bin`. Upgrading the app's MyGo does not upgrade the
installed command: install it again, and this guide comes with it.
