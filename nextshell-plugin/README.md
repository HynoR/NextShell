# NextShell plugin for Claude Code

Operate your servers through a **running NextShell desktop app**. The agent never sees a
credential — no passwords, no private keys, no device key, no tokens pasted into config files.
It talks to the app over a local, OS-permission-protected endpoint; NextShell resolves hosts,
enforces per-host authorization, asks the human when policy requires it, and shows every action
live in its GUI.

## Install

```
/plugin marketplace add HynoR/nextshell-plugin
/plugin install nextshell
```

Then, inside NextShell: 设置 → Agent 接入 → 启用, and grant hosts one by one
(连接管理 → 编辑连接 → 属性 → Agent 授权).

Zero further configuration: no token to paste, no port to pin. The bundled bridge discovers the
app's endpoint file automatically; if NextShell is not running, tools return a clear
"NextShell 未运行" error instead of failing the session.

## What's inside

| Path                           | What it is                                                                                                             |
| ------------------------------ | ---------------------------------------------------------------------------------------------------------------------- |
| `bin/bridge.mjs`               | Zero-credential stdio ↔ NextShell bridge (self-contained, answers `initialize`/`tools/list` even when the app is down) |
| `skills/remote-triage`         | "看一下 xx 服务器" — snapshot first, read-only drill-down                                                              |
| `skills/deploy-upload`         | "把打包文件传到 /opt/app" — verify → upload via transfer queue → verify → unpack                                       |
| `skills/disk-forensics`        | "哪个文件夹占用最大" — du drill-down anchored on the user's session cwd                                                |
| `commands/nextshell-triage.md` | `/nextshell:nextshell-triage <host>`                                                                                   |
| `agents/sre-operator.md`       | Subagent restricted to the read-only tool tier                                                                         |
| `skills/nextshell-cli`         | MCP-free access: a self-contained CLI + SKILL.md for agents that speak shell but not MCP                               |

## No MCP? Use the skill instead

Any agent that can run shell commands and read locally installed skills (Claude Code,
Copilot, Cursor, custom harnesses) can skip MCP entirely: copy `skills/nextshell-cli/`
into the agent's skills directory (e.g. `~/.claude/skills/`). The skill wraps
`scripts/nextshell-cli.mjs`, a single-file Node CLI that discovers the running app's
endpoint the same way the bridge does:

```bash
node skills/nextshell-cli/scripts/nextshell-cli.mjs status
node skills/nextshell-cli/scripts/nextshell-cli.mjs exec --target web-1 --command uptime
```

Same trust model as MCP: zero credentials in the CLI, per-host authorization and all
confirmations enforced by NextShell itself. The trade-off is coarser client-side
permissioning — everything is a `Bash(node …)` call, so per-tool allowlists don't apply.

## Permission allowlist examples

Installed via this plugin, tools are named `mcp__plugin_nextshell_nextshell__<tool>`:

```json
{
  "permissions": {
    "allow": [
      "mcp__plugin_nextshell_nextshell__host_list",
      "mcp__plugin_nextshell_nextshell__monitor_snapshot",
      "mcp__plugin_nextshell_nextshell__session_list"
    ]
  }
}
```

Added directly with `claude mcp add nextshell …` (NextShell 设置页的「生成并复制接入配置」),
the prefix is `mcp__nextshell__<tool>` instead.

Read-only tools carry `readOnlyHint: true` annotations, so clients that honour annotations
auto-approve them. Everything else is gated by NextShell's own in-app confirmations —
the plugin cannot weaken those.

## Maintainers: refreshing the bridge

`bin/bridge.mjs` is the build output of `apps/mcp-bridge` in the NextShell repo:

```
pnpm --filter @nextshell/mcp-bridge run build
cp apps/mcp-bridge/dist/index.js nextshell-plugin/bin/bridge.mjs
pnpm --filter @nextshell/mcp-bridge run build:cli
cp apps/mcp-bridge/dist/cli.js nextshell-plugin/skills/nextshell-cli/scripts/nextshell-cli.mjs
```

Re-copy them whenever the bridge changes; the static tool manifest they answer
`tools/list` / `tools` with lives inside the bundles.
