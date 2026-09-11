# NextShell plugin for Claude Code

Drive the servers you already opened in a **running NextShell desktop app**. The agent never
sees a credential: it borrows your authenticated tabs over a loopback MCP endpoint and every
action shows up live in the NextShell GUI.

## Setup (once)

1. In NextShell: 设置 → Agent 接入 → 启用. The endpoint listens on
   `http://127.0.0.1:41777/mcp` (port adjustable in the same page).
2. Choose **stdio or HTTP** (both are supported; use one per harness):

   **stdio — offline discovery**, requires Node.js 24+ and network for the first download.
   The harness can initialize and list tools while NextShell is closed. Actual tool calls
   require the app to be running with Agent access enabled. Once the app opens, the next
   call reconnects without restarting the harness. After package version 0.1.0 is published:

   ```json
   {
     "mcpServers": {
       "nextshell": {
         "command": "npx",
         "args": ["-y", "@nextshell/mcp@0.1.0", "--port", "41777"]
       }
     }
   }
   ```

   ```sh
   claude mcp add --transport stdio nextshell -- npx -y @nextshell/mcp@0.1.0 --port 41777
   ```

   The Claude Code plugin installs this stdio configuration plus the skill:

   ```
   /plugin marketplace add HynoR/NextShell
   /plugin install nextshell
   ```

   **HTTP — direct connection**, no Node.js needed. NextShell must already be running:

   ```json
   { "mcpServers": { "nextshell": { "type": "http", "url": "http://127.0.0.1:41777/mcp" } } }
   ```

   ```sh
   claude mcp add --transport http nextshell http://127.0.0.1:41777/mcp
   ```

   Stdio-only clients should use the stdio configuration. Both methods use the configured
   port (41777 by default); the settings page generates either configuration for that port.

3. Any harness that installs skills from a folder: 设置 → Agent 接入 shows the path of a
   ready-made `nextshell/SKILL.md`; copy that folder into your skills directory
   (`~/.claude/skills/`, `~/.codex/skills/`, `.agents/skills/`, …).
4. Open the server tab in NextShell, then tell your agent what to do. The 前台 / 后台 switch
   in the Agent panel decides whether the agent's commands are typed into your tab or run
   silently on a separate channel; the 权限模式 switch next to it decides whether each command
   runs at once (Auto, the default — you trust the agent and your harness's own approval) or
   first needs your click (Permission).

## What's inside

| Path                        | What it is                                                          |
| --------------------------- | ------------------------------------------------------------------- |
| `.mcp.json`                 | Versioned stdio MCP entry; HTTP remains available as an alternative |
| `skills/nextshell/SKILL.md` | Workflow: `session_list` → `exec` → `session_read` / history        |

## Trust model

- Tools operate on open tabs. `host_list` finds saved hosts; `session_open` requires user authorization to open a new tab.
- Credentials never cross the MCP boundary — the agent gets session ids and output.
- If you have an unsubmitted command on the line of a tab the agent is driving, its next
  foreground call fails with `human_intervention` and it must stop and report.
- Obvious destructive commands (`rm -rf /`, `mkfs`, `dd` to a device, fork bombs, …) are
  refused outright; add your own patterns in 设置 → Agent 接入 → 命令黑名单.
- The endpoint is loopback-only with no token: any local process is trusted, like any other
  program you run. Turn the switch off to stop listening instantly.
