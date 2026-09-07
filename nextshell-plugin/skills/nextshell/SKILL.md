---
name: nextshell
description: Operate servers through the tabs the user already opened in the NextShell desktop app (MCP tools session_list / session_read / exec / session_send_keys / session_history). Use when the user says things like "看一下 xx 服务器", "在那台机器上跑一下", "帮我查下 web-1 的磁盘", or names a host they have open in NextShell.
---

# NextShell

NextShell lends you the user's **already-open, already-authenticated** terminal tabs.
You never see a password or key; you only get session ids and command output.

## Workflow

1. `session_list` — the only discovery entry. If the host the user means is not listed,
   ask them to open it in NextShell first. Never try to connect on your own.
2. Prefer `exec` (target = session id) for anything that is a plain command: it runs on
   that tab's connection, inherits its cwd, and returns stdout/stderr/exit code.
3. Use `session_read` to see what is on screen, `session_history` for recent commands with
   exit codes and output.
4. `session_send_keys` only when the state lives in that shell (a TUI, a sudo prompt, an
   entered venv or `docker exec`). Use `waitForPrompt` to get the exit code back.
5. `session_focus` to bring the tab to the front when the user should look at it.

## Rules

- A `human_intervention` error means the user typed into that tab after your last
  operation. Stop, report what you were doing, and wait for instructions — do not retry.
- A blacklist error is final; there is no "allow once". Tell the user what was blocked.
- If tools fail with a connection error, NextShell is not running or Agent access is off:
  设置 → Agent 接入 → 启用.

## Not available on purpose

No host enumeration, no opening connections, no file transfer, no local filesystem.
Use `exec` with `cat`/`sed`/`tar` for file work on the remote side.
