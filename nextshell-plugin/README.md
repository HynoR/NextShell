# NextShell plugin for Claude Code

Drive the servers you already opened in a **running NextShell desktop app**. The agent never
sees a credential: it borrows your authenticated tabs over a loopback MCP endpoint and every
action shows up live in the NextShell GUI.

## Setup (once)

1. In NextShell: 设置 → Agent 接入 → 启用. The endpoint listens on
   `http://127.0.0.1:41777/mcp` (port adjustable in the same page).
2. Point your harness at it. Any of:

   **Claude Code plugin** (MCP config + skill in one go):

   ```
   /plugin marketplace add HynoR/NextShell
   /plugin install nextshell
   ```

   **Claude Code, no plugin**:

   ```
   claude mcp add --transport http nextshell http://127.0.0.1:41777/mcp
   ```

   **Cursor / Windsurf / Codex / Gemini CLI / anything that reads `mcp.json`** — this file
   is exactly `.mcp.json` in this directory:

   ```json
   { "mcpServers": { "nextshell": { "type": "http", "url": "http://127.0.0.1:41777/mcp" } } }
   ```

   **Claude Desktop** (config file only speaks stdio):

   ```json
   {
     "mcpServers": {
       "nextshell": { "command": "npx", "args": ["mcp-remote", "http://127.0.0.1:41777/mcp"] }
     }
   }
   ```

3. Open the server tab in NextShell, then tell your agent what to do.

## What's inside

| Path                        | What it is                                                        |
| --------------------------- | ----------------------------------------------------------------- |
| `.mcp.json`                 | The one-line MCP server entry (loopback HTTP, no bridge process)  |
| `skills/nextshell/SKILL.md` | Workflow: `session_list` → `exec` / `session_send_keys` → history |

## Trust model

- Tools only reach tabs you opened; there is no host list and no connect tool.
- Credentials never cross the MCP boundary — the agent gets session ids and output.
- If you type into a tab the agent is driving, its next call fails with
  `human_intervention` and it must stop and report.
- Obvious destructive commands (`rm -rf /`, `mkfs`, `dd` to a device, fork bombs, …) are
  refused outright; add your own patterns in 设置 → Agent 接入 → 命令黑名单.
- The endpoint is loopback-only with no token: any local process is trusted, like any other
  program you run. Turn the switch off to stop listening instantly.
