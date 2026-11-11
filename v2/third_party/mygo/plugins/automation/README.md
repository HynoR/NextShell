# automation

Drive a running [MyGo](https://github.com/egoist/mygo) app from another
process, as end-to-end test runners and agents do: list its windows, read
the elements of their native UI with the app's keys, act on them, send
pointer, wheel, key and text input, choose menu items, evaluate scripts in
pages, capture windows, and wait for them to settle. One protocol on
macOS, Linux and Windows: newline-delimited JSON over a Unix domain
socket that only the user can open.

```go
//go:build e2e

package main

import _ "github.com/egoist/mygo/plugins/automation"
```

```sh
MYGO_AUTOMATION=/tmp/app.sock ./app
echo '{"id": 1, "method": "windows"}' | socat - UNIX-CONNECT:/tmp/app.sock
```

Agents drive it with `mygo-automation`, a command line client with
compact snapshots, short refs and diffs once the UI settled, whose guide
it prints:

```sh
go install github.com/egoist/mygo/plugins/automation/cmd/mygo-automation
mygo-automation skills
```

It listens only when `MYGO_AUTOMATION` is set, and builds without the
inspector (`-tags mygo_noinspector`, the release builds of `mygo build`)
leave it out. See [the documentation](../../docs/plugins/automation.md).
