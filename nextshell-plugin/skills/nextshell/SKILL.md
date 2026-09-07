---
name: nextshell
description: Operate servers through the tabs the user already opened in the NextShell desktop app (MCP tools session_list / session_read / exec / session_send_keys / session_history). Use when the user says things like "看一下 xx 服务器", "在那台机器上跑一下", "帮我查下 web-1 的磁盘", or names a host they have open in NextShell.
---

# NextShell

NextShell lends you the user's **already-open, already-authenticated** terminal tabs.
You never see a password or key; you only get session ids and command output.

## Workflow

1. `session_list` — the tabs already open. If the host the user means is not there,
   `host_list` (no query = their 10 most recent hosts; add a keyword to search) and then
   `session_open` with its id: NextShell raises a dialog and the tab opens only after the
   user clicks 授权. `pending` → call again with the same `requestId`. `denied` → stop and
   ask the user to confirm in NextShell; never retry on your own.
2. **Default: `session_send_keys`** with `submit: true` and `waitForPrompt: true`. The
   command and its output appear in the user's tab in real time, as if they typed it.
   If `waitTimedOut` comes back (no shell integration), `session_read` the screen.
3. `session_read` to see what is on screen, `session_history` for recent commands with
   exit codes and output.
4. `exec` **only when the user explicitly asks** for background or quiet execution
   ("在后台执行 xxx", "不打扰我执行 xxx"). It runs out of band, invisible in the tab, and
   returns stdout/stderr/exit code. Not available for local shell tabs.
5. `session_focus` to bring the tab to the front when the user should look at it.

## Rules

- A `human_intervention` error means the user typed into that tab after your last
  operation. Stop, report what you were doing, and wait for instructions — do not retry.
- A blacklist error is final; there is no "allow once". Tell the user what was blocked.
- If tools fail with a connection error, NextShell is not running or Agent access is off:
  设置 → Agent 接入 → 启用.

## Not available on purpose

No unrestricted host enumeration, no connecting without the user's click, no file
transfer, no local filesystem.
Do file work on the remote side with `cat`/`sed`/`tar` through the terminal.
