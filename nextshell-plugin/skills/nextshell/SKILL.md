---
name: nextshell
description: Operate servers through the NextShell 2 desktop app (MCP tools host_list / session_open / session_list / exec / session_read / command_search / command_save). Use when the user says things like "看一下 xx 服务器", "在那台机器上跑一下", "帮我查下 web-1 的磁盘", or names a host saved in NextShell.
---

# NextShell

NextShell lends you the user's **saved hosts and already-authenticated terminal tabs**.
You never see a password or key; you only get session ids and redacted output.

## Workflow

1. `session_list` — tabs already open (`connected: true`, `local: false` ones can run `exec`).
   If the host the user means is not there, `host_list` with a keyword, then `session_open`
   with its id. A host that already has a connected tab is reused.
2. `exec` — runs a command on the **remote** host of a session over a separate channel (not typed
   into the tab; local shell tabs are not supported). Returns `output`, `stderr`, `exitCode`,
   `actualCwd`. `timeoutSec` max 120.
3. `session_read` to see what is on a tab's screen; `session_send_signal` (Ctrl-C / Ctrl-D /
   Ctrl-Z) to stop something; `session_focus` to bring a tab forward for the user;
   `command_search` / `command_save` for the user's saved command library.

## Approvals

Every change (`exec`, `session_open`, `command_save`, `session_send_signal`, `session_focus`)
needs the user's approval in NextShell, unless they turned on 自动批准 for a while (then calls run
at once and return their result directly).

- The first call may return `{ status: "pending", requestId }`. Call the **same tool** again with
  **only** that `requestId` to keep waiting (a request lives 2 minutes); repeat while it is `pending`.
- `denied`: the user declined or it expired. Stop and ask them; never resend it on your own.

## Rules

- `forbidden` is final: the command is dangerous or on the user's blacklist, or access is halted
  (the user pressed 紧急停止). There is no "allow once". Tell the user what was blocked.
- `consent_required`: the command touches a `.env` file. Ask the user, and only after they agree
  retry with `allowSensitive: true`; this always needs a manual click, even under auto-approve.
- `token_missing`: ask the user to turn on 设置 → Agent → 开启 MCP in NextShell. `unauthorized`:
  the token was rotated or points at another profile; ask them to check the same page. Then try
  again: the bridge re-reads the token each time. Do not retry in a loop.
- `app_unavailable`: NextShell is not running or MCP is off. Ask the user to start it.
- `connection_lost`: the command may already have run. Inspect the terminal first; never replay a
  command or an old `requestId` automatically.
- `busy`: the session is running another agent call; wait and retry later.
- `incompatible_version`: the app and the `@nextshell/mcp` package differ; ask the user to update.

## Not available on purpose

No unrestricted host enumeration (max 20 results), no connecting without approval, no raw
keystroke injection, no file transfer, no local filesystem, no `exec` on local shells.
Do file work on the remote side with `cat`/`sed`/`tar` through `exec`.
