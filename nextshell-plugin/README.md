# NextShell plugin for Claude Code

Drive the servers you already opened in a **running NextShell 2 desktop app**. The agent never
sees a credential: it borrows your authenticated tabs over a loopback MCP endpoint, and every
change is approved in the app (or auto-approved for a time you choose).

## Setup (once)

1. In NextShell: 设置 → Agent → 开启 MCP. The app writes a private `mcp.json` (port and token)
   into its config directory and removes it when MCP is turned off or the app quits.
2. Install the plugin. It starts the MCP bridge (`nextshell-mcp`, the `nextshell-mcp-*.tgz` attached
   to each NextShell 2 release; it is not on npm), which finds the port and token in that file by
   itself, so there is nothing to copy or paste:

   ```
   /plugin marketplace add HynoR/NextShell
   /plugin install nextshell
   ```

   Without the plugin, add the same bridge by hand (Node.js 24+, no token or port needed):

   ```sh
   claude mcp add --transport stdio nextshell -- npx --allow-remote=root -y https://github.com/HynoR/NextShell/releases/download/v2.0.0-rc1/nextshell-mcp-0.2.0.tgz
   ```

   The URL is the one of the release you installed. npm 12 refuses to install from a URL unless
   `--allow-remote=root` permits it; that allows only the URL on this command line.

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

| Path                        | What it is                                                                                 |
| --------------------------- | ------------------------------------------------------------------------------------------ |
| `.mcp.json`                 | Versioned stdio entry; no token, no port                                                   |
| `skills/nextshell/SKILL.md` | Workflow: `session_list` → `exec` / `session_send_text` → `session_read`, approvals, tiers |

## Trust model

- Tools operate on saved hosts and open tabs. Opening a connection and running anything that
  changes state needs your approval in NextShell, unless you enabled auto-approve.
- Credentials never cross the MCP boundary: the agent gets session ids and redacted output.
- The endpoint listens on 127.0.0.1 only and requires the bearer token. The token file is readable by
  your user only (mode 0600); anything running as you can read it, like any other program you run.
- Commands fall into three tiers. **Forbidden** ones never run, even under auto-approve (wiping disks
  or `/` or home, power off, `kill -9 -1`, overwriting `/etc/passwd`, your own blacklist patterns).
  **Risky** ones (`sudo`, `rm -r`, piping into a shell, writes to `/etc`, package and service
  changes, …) and anything touching credential files (`.env*`, ssh keys, `.aws/credentials`, …) or the
  environment always need a manual click, even under auto-approve. Everything else follows your
  approval or auto-approve setting. The pending list in 设置 → Agent shows the tier, the agent's
  reason, the host or tab name and the full command.
- The policy guards against **accidents**, not against a determined or prompt-injected agent: a shell
  offers endless ways around any classifier. 自动批准不是安全沙箱：只在你信任当前 Agent 任务时开启。
- Agents do not interrupt you: the first change per app run raises the window once with a notice;
  afterwards no raising, no tab switching, no keyboard focus. Tabs an agent opens stay in the
  background with an Agent badge; `session_focus` is the explicit hand-over (at most one raise per
  minute). An agent cannot type into a tab you pressed a key in during the last 3 seconds. The status
  bar shows 「Agent 活动中」 for two minutes after any change and the count of pending approvals.
