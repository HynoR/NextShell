# @nextshell/mcp

stdio bridge from MCP clients (Claude Code, Codex, …) to the local **NextShell 2** app.
The app keeps its own policy: every change needs approval in the app (unless the user turned
on auto-approve for a while), dangerous commands are blocked, output is redacted.

## Setup

1. In NextShell: 设置 → Agent → 开启 MCP. While MCP is on, the app writes
   `mcp.json` (`{"port":41777,"token":"…"}`, mode 0600) to its config directory and removes
   it when MCP is turned off or the app exits.
2. Add the bridge to your client. No token or port belongs in the config:

```json
{
  "mcpServers": {
    "nextshell": { "command": "npx", "args": ["-y", "@nextshell/mcp@0.2.0"] }
  }
}
```

```sh
claude mcp add --transport stdio nextshell -- npx -y @nextshell/mcp@0.2.0
```

Requires Node.js 24+. The harness can start the bridge, initialize and list tools while
NextShell is closed; only a tool call contacts the app, and it re-reads the token every time it
reconnects, so starting the app later or rotating the token needs no restart.

## Token and port

The bridge sends `Authorization: Bearer <token>` on every request. Token source, first match wins:

1. `--token <token>` (visible to other local users in the process list; prefer the next two)
2. `NEXTSHELL_MCP_TOKEN`
3. `--config <path>`: a file in the `mcp.json` format
4. the default `mcp.json` of the OS (same directory the app uses, `NextShell-v2/mcp.json` under):
   - macOS: `~/Library/Application Support`
   - Linux: `$XDG_CONFIG_HOME` (absolute) or `~/.config`
   - Windows: `%AppData%`

Port: `--port <n>` if given, else the `port` in the file that was read, else 41777. The host is
always 127.0.0.1; redirects are refused so the token never leaves the machine. The token is never
logged or put in an error message.

An app started with a different profile (`--db <path>`) writes `mcp.json` next to that database;
point the bridge at it with `--config <dir>/mcp.json`.

## Errors

Tool failures come back as `{ ok: false, error: { code, message } }`:

| code                   | meaning                                                                      |
| ---------------------- | ---------------------------------------------------------------------------- |
| `token_missing`        | no token found; enable MCP in NextShell 设置 → Agent or pass a token source  |
| `unauthorized`         | the app refused the token (rotated?); nothing ran, the next call re-reads it |
| `app_unavailable`      | app not running or MCP off; the next call reconnects                         |
| `connection_lost`      | response lost, the command may have run: inspect the terminal, do not retry  |
| `incompatible_version` | the app lacks a tool this package expects: update both                       |

App-side codes (`denied`, `forbidden`, `consent_required`, `busy`, `disconnected`, `auth_failed`,
`host_key`, `refused`, `unreachable`, `user_typing`, `invalid_argument`, `cwd_missing`, …) are passed through unchanged, with a short
redacted cause in `message`. A call that returns `{ status: "pending", requestId }` is waiting for the
user: call the same tool again with the same arguments (or only that `requestId`). `session_open` can
also answer `{ status: "user_action_required", reason }` while a dialog waits for the user. The bridge
never replays a call.

## Tools

`host_list`, `session_list`, `session_read` (`sinceCursor`, `waitFor`), `session_history`,
`command_search`, `session_open`, `command_save`, `exec`, `session_send_text`, `session_send_signal`,
`session_focus`, `session_close`, `agent_status`, `request_cancel`. The list, descriptions and JSON
Schemas come from the app itself: `src/tools.generated.json` is generated from the app's tool
metadata (`v2/internal/agent/tools.go`) and a Go test fails when it is stale. After changing a tool:

```sh
cd v2 && NEXTSHELL_UPDATE_TOOLS=1 go test ./internal/mcpendpoint -run Snapshot
```

The bridge serves that list while the app is closed and does not validate arguments itself.

## Development

From the repository root:

```sh
pnpm --filter @nextshell/mcp run build
pnpm exec vitest run packages/mcp/src
pnpm --filter @nextshell/mcp run typecheck
pnpm --filter @nextshell/mcp run smoke:pack   # needs npm registry access
```

Tests run the bridge against `src/fake-app.ts`, a loopback stand-in for the app's endpoint that
enforces the bearer token. To try the real app, build it with `-tags nextshell_devtools` and run
`nextshell --agent-test --db <tmp>/t.db [--import hosts.json]`: MCP and auto-approve are on from the
start, the path of `mcp.json` is printed on stderr (`--config <that path>`), and `--import` loads a
`nextshell-connections` v1 JSON (plain) as saved hosts.

The package is prepared locally and is not published automatically. Keep `package.json`, `MCP_PACKAGE_SPEC`
(`packages/shared/src/mcp-config.ts`) and `nextshell-plugin/.mcp.json` on the same version; a test checks it.
