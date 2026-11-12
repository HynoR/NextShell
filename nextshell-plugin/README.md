# NextShell plugin for Claude Code

Drive the servers you already opened in a **running NextShell 2 desktop app**. The agent never
sees a credential: it borrows your authenticated tabs over a loopback MCP endpoint, and every
change is approved in the app (or auto-approved for a time you choose).

## Setup (once)

1. In NextShell: 设置 → Agent → 开启 MCP. The app writes a private `mcp.json` (port and token)
   into its config directory and removes it when MCP is turned off or the app quits.
2. Install the plugin. It starts `@nextshell/mcp`, which finds the port and token in that file
   by itself, so there is nothing to copy or paste:

   ```
   /plugin marketplace add HynoR/NextShell
   /plugin install nextshell
   ```

   Without the plugin, add the same bridge by hand (Node.js 24+, no token or port needed):

   ```sh
   claude mcp add --transport stdio nextshell -- npx -y @nextshell/mcp@0.2.0
   ```

3. Open the server tab in NextShell, then tell your agent what to do. Tool calls that change
   something show an approval prompt in NextShell; in 设置 → Agent you can turn on 自动批准
   for 15 分钟 / 1 小时 / 直到退出 (a status indicator stays visible and it is off after every
   restart). 紧急停止 turns it off again.

The agent can start before NextShell does: tools are listed offline, and a call made once MCP is
on connects without restarting anything. If you rotate the token in the app, the next call picks
up the new one.

### Custom profile or token source

`@nextshell/mcp` reads the token from, in order: `--token`, the `NEXTSHELL_MCP_TOKEN` environment
variable, `--config <path>`, then the default `mcp.json` of your OS. An app started with
`--db <path>` writes `mcp.json` next to that database; add `--config <dir>/mcp.json` to the plugin's
`args` (or set `NEXTSHELL_MCP_TOKEN`). Details: `packages/mcp/README.md`.

Direct HTTP also works for clients that can set headers: `http://127.0.0.1:41777/mcp` with
`Authorization: Bearer <token from mcp.json>`. Prefer the bridge, which does that for you.

## What's inside

| Path                        | What it is                                                    |
| --------------------------- | ------------------------------------------------------------- |
| `.mcp.json`                 | Versioned stdio entry; no token, no port                      |
| `skills/nextshell/SKILL.md` | Workflow: `session_list` → `exec` → `session_read`, approvals |

## Trust model

- Tools operate on saved hosts and open tabs. Opening a connection and running anything that
  changes state needs your approval in NextShell, unless you enabled auto-approve.
- Credentials never cross the MCP boundary: the agent gets session ids and redacted output.
- The endpoint listens on 127.0.0.1 only and requires the bearer token. The token file is readable by
  your user only (mode 0600); anything running as you can read it, like any other program you run.
- Obvious destructive commands (`rm -rf /`, `mkfs`, `dd` to a device, fork bombs, …) and your own
  blacklist patterns are refused outright, even under auto-approve; commands touching `.env`
  files always need a manual click.
