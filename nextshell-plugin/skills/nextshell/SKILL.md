---
name: nextshell
description: Operate servers through the NextShell 2 desktop app (MCP tools host_list / session_open / session_list / exec / session_send_text / session_read / session_close / command_search / command_save). Use when the user says things like "看一下 xx 服务器", "在那台机器上跑一下", "帮我查下 web-1 的磁盘", or names a host saved in NextShell.
---

# NextShell

NextShell lends you the user's **saved hosts and already-authenticated terminal tabs**.
You never see a password or key; you only get session ids and redacted output.

## Workflow

1. `session_list` — tabs already open (`connected: true`, `local: false` ones can run `exec`).
   If the host the user means is not there, `host_list` with a keyword (paged: `limit` 1-200,
   `offset`, `total`), then `session_open` with its id. A host that already has a connected tab
   is reused. The new tab opens in the **background**.
2. `exec` — runs a command on the **remote** host of a session over a separate channel (not typed
   into the tab; local shell tabs are not supported). Returns `output`, `stderr`, `exitCode`,
   `actualCwd`. `timeoutSec` max 120. One exec at a time per session (`busy`); everything else
   (`session_read`, `session_send_signal`, `session_focus`, …) works while one runs.
3. `session_send_text` — type into the tab itself: interactive programs, REPLs, tmux, prompts.
   `text` (escapes `\r \n \t \e \xHH`, `\\` for a backslash, max 4 KiB) and/or `keys`
   (`Enter, Up, Down, Left, Right, Esc, Tab, Backspace, Delete, F1-F12, PageUp, PageDown, Home,
End, Ctrl-a … Ctrl-z`). The reply carries a `cursor`; read the result with
   `session_read { target, sinceCursor, waitFor: { pattern, timeoutSec } }` instead of polling.
   `pattern` is a substring or `re:<regex>`; `waitFor` blocks up to 60 s and returns
   `matched` / `timedOut`. A plain `session_read` returns the visible screen and a `cursor`.
4. `session_send_signal` (Ctrl-C / Ctrl-D / Ctrl-Z) stops things; `session_close` closes a tab;
   `session_focus` hands a tab to the user; `command_search` / `command_save` use the user's
   command library (`command_save` with an `id` updates only the fields you pass).
5. `agent_status` shows whether MCP is halted, whether auto-approve is on, and your own pending
   requests; `request_cancel { requestId }` withdraws one.

## Approvals and the three tiers

Every change (`exec`, `session_open`, `session_send_text`, `session_send_signal`, `session_focus`,
`session_close`, `command_save`) needs the user's approval in NextShell, unless they turned on
自动批准 for a while (then most calls run at once and return their result directly).

- The first call may return `{ status: "pending", requestId }`. Call the **same tool again** with the
  **same arguments** (or only the `requestId`) to keep waiting; a request lives 2 minutes.
- `denied`: the user declined or it expired. Stop and ask them; never resend it on your own.

Commands fall into three tiers; `exec` and every complete line typed with `session_send_text` are judged:

1. **forbidden** — never runs: wiping disks or `/` or `~`, `rm -rf` of home, `find / -delete`, power
   off / reboot / `init 0`, `kill -9 -1`, overwriting `/etc/passwd` or `/etc/shadow`, `chmod -R` on
   `/`, `mkfs`, the user's blacklist. Fails with `forbidden`; there is no "allow once".
2. **risky** — always a **manual** approval, even under auto-approve (`status: "pending"` with
   `tier: "risky"` and `why`): `sudo`/`su`, `rm -r`, piping into a shell or interpreter
   (`curl … | sh`, `eval`, `bash <(…)`), writes to `/etc /boot /usr /var/lib`, package managers
   (`apt install` …), `systemctl stop|disable|mask`, firewall changes, `userdel`/`passwd`. Tell
   the user it is waiting for them.
3. everything else — auto-approvable.

`consent_required`: the command touches a credential file (`.env*`, ssh keys, `.aws/credentials`,
`.netrc`, `.kube/config`, …) or dumps the environment (`env`, `printenv`, `set`). Ask the user, and
only after they agree retry with `allowSensitive: true`; that is also a manual approval.

## Not interrupting the user

NextShell is the user's daily tool. The **first** change you make in a run raises the window once and
shows "Agent 正在操作 NextShell". After that you never raise it, switch tabs or take the keyboard:
tabs you open appear in the background with an Agent badge. Use `session_focus` only when you
**need the user** to look at or type into a tab (a password prompt, a result); it selects the tab and
raises the window at most once a minute. `session_send_text` and `session_send_signal` fail with
`user_typing` while the user pressed a key in that tab in the last 3 seconds; wait and retry later,
do not loop.

## session_open needing the user

If the connection needs a decision (trust a host key, type a password or one-time code),
`session_open` returns **at once** with `{ status: "user_action_required", reason: "host_key" |
"password" | "otp" }` while the dialog stays open in NextShell. Tell the user what to do, wait,
then call `session_open` again with the same target: it joins the connection in progress.

## Errors

- `forbidden`: forbidden command, or access halted (the user pressed 紧急停止). Tell the user.
- `disconnected`: the session dropped or was closed (its id stays recognisable for a while); open it
  again. `not_found`: no such session, host, request or command.
- `auth_failed`, `host_key` (not trusted or changed), `refused`, `unreachable`, `timeout`: why a
  `session_open` failed; the message carries a short cause. Do not retry in a loop.
- `busy`: another exec is running on this session; wait and retry later.
- `token_missing`: ask the user to turn on 设置 → Agent → 开启 MCP in NextShell. `unauthorized`:
  the token was rotated or points at another profile. `app_unavailable`: NextShell is not running or
  MCP is off. `incompatible_version`: the app and `@nextshell/mcp` differ; ask the user to update.
- `connection_lost`: the call may already have run. Inspect the terminal first; never replay a
  command or an old `requestId` automatically.

## Not available on purpose

No credentials, no file transfer, no local filesystem, no `exec` on local shells, no way around
the tiers. Do file work on the remote side with `cat`/`sed`/`tar` through `exec`.
