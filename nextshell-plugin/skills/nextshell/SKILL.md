---
name: nextshell
description: Operate servers through the NextShell 2 desktop app (MCP tools host_list / session_open / session_list / exec / session_send_text / session_read / session_close / command_search / command_save). Use when the user says things like "看一下 xx 服务器", "在那台机器上跑一下", "帮我查下 web-1 的磁盘", or names a host saved in NextShell.
---

# NextShell

NextShell lends you the user's **saved hosts and already-authenticated terminal tabs**.
You never see a password or key; you only get session ids and redacted output.

## Workflow

1. `session_list` — tabs already open (`connected: true`, `local: false` ones can run `exec`).
   If the host the user means is not there, `host_list` with a keyword (sorted by name, paged: `limit` 1-200,
   `offset`, `total`), then `session_open` with its id. A host that already has a connected tab
   is reused. The new tab opens in the **background**.
2. `exec` — runs a command on the **remote** host of a session over a separate channel (not typed
   into the tab; local shell tabs are not supported). Returns `output`, `stderr`, `exitCode`,
   `actualCwd`. `timeoutSec` 1-120 (default 30). One exec at a time per session (`busy`); everything
   else (`session_read`, `session_send_signal`, `session_focus`, …) works while one runs. The command
   runs as a whole in the tab's current directory when the shell reports it (`actualCwd`; empty
   without directory tracking, then it starts in the login directory); if that directory is gone
   nothing runs and you get `cwd_missing` with the path. If the connection drops mid-command you get
   `disconnected` and the command may or may not have finished; closing the session kills it. If
   your client merely drops its HTTP connection the command keeps running until it ends or
   `timeoutSec` passes: cancel with `notifications/cancelled`, or pick a short `timeoutSec`.
3. `session_send_text` — type into the tab itself: interactive programs, REPLs, tmux, prompts.
   `text` (escapes `\r \n \t \e \xHH`, `\\` for a backslash, max 4 KiB) and/or `keys`
   (`Enter, Up, Down, Left, Right, Esc, Tab, Backspace, Delete, F1-F12, PageUp, PageDown, Home,
End, Ctrl-a … Ctrl-z`); `text` is typed first, then the keys. To interleave them use `input`
   instead: an ordered array of strings and `{ "key": "Ctrl-b" }` items, e.g.
   `[{"key":"Ctrl-b"}, "%"]` is tmux's split in one call. The reply carries a `cursor`; read the
   result with `session_read { target, sinceCursor, waitFor: { pattern, timeoutSec } }` instead of
   polling. `pattern` is a substring or `re:<regex>`; `waitFor` blocks up to 60 s and returns
   `matched` / `timedOut`. A full-screen program (htop, vim, less) draws with cursor movement, so its
   output stream holds no readable text: add `on: "screen"` to `waitFor` to match the rendered
   screen instead (polled ten times a second). A plain `session_read` returns the visible screen and
   a `cursor`.
4. `session_send_signal` (Ctrl-C / Ctrl-D / Ctrl-Z) stops things; `session_close` closes a tab;
   `session_focus` hands a tab to the user; `command_search` / `command_save` use the user's
   command library (`command_save` with an `id` updates only the fields you pass).
5. `agent_status` shows whether MCP is halted, whether auto-approve is on, and your own pending
   requests; `request_cancel { requestId }` withdraws one.

## Approvals and the three tiers

The command policy guards against **accidents**, not against a determined or prompt-injected agent:
a shell has endless ways around any classifier. 自动批准不是安全沙箱：只在你信任当前 Agent 任务时开启。
So never rephrase a command to slip past its tier; if a tier gets in your way, ask the user.

Every change (`exec`, `session_open`, `session_send_text`, `session_send_signal`, `session_focus`,
`session_close`, `command_save`) needs the user's approval in NextShell, unless they turned on
自动批准 for a while (then most calls run at once and return their result directly).

- The first call may return `{ status: "pending", requestId }`. Call the **same tool again** with the
  **same arguments** (or only the `requestId`) to keep waiting; a request lives 2 minutes.
- `denied`: the user declined or it expired. Stop and ask them; never resend it on your own.

Commands fall into three tiers; `exec` and every complete line typed with `session_send_text` are judged:

1. **forbidden** — never runs: wiping disks or `/` or `~`, `rm -rf` of home, `find / -delete`, power
   off / reboot / `init 0`, `kill -9 -1`, overwriting `/etc/passwd` or `/etc/shadow`, `chmod -R` on
   `/`, `mkfs`, `kill -9 1` / `kill -TERM 1`, the user's blacklist. Fails with `forbidden`; there is no
   "allow once".
2. **risky** — always a **manual** approval, even under auto-approve (`status: "pending"` with
   `tier: "risky"` and `why`): `sudo`/`su`, `rm -r`, piping into a shell or interpreter
   (`curl … | sh`, `bash <<< '…'`, `eval`, `bash <(…)`), writes to `/etc /boot /usr /var/lib`,
   package managers (`apt install` …), `systemctl stop|disable|mask`, firewall changes,
   `userdel`/`passwd`, `ssh host <command>` (any remote command), `kill`/`pkill` of `sshd` or init,
   `nc -e`, `docker run --privileged` or with `/` mounted. Tell the user it is waiting for them.
3. everything else — auto-approvable.

`consent_required`: the command touches a credential file (`.env*`, ssh keys, `*.pem`/`*.key`/`*.p12`,
`.aws/credentials`, `.netrc`, `.kube/config`, shell history, `~/.config/gcloud`, `/proc/*/environ`, …)
or dumps the environment (`env`, `printenv`, `set`, `compgen -e`). Ask the user, and only after they
agree retry with `allowSensitive: true`; that is also a manual approval.

`command_save` judges the command text the same way: a forbidden command is refused (`forbidden`), a
risky one is a manual approval even under auto-approve.

Typed lines (`session_send_text`) are judged when Enter ends them. At a shell prompt, a line edited
with history, completion, cursor or other readline keys cannot be inspected and counts as risky;
`Ctrl-O`, `Ctrl-J`, `Ctrl-M` and `Alt-Enter` run the line like Enter. On the alternate screen
(tmux, vim, htop) control keys and Esc belong to the program and are not held against you; lines
ended by Enter are still judged.

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
"host_key_changed" | "password" | "otp" }` while the dialog stays open in NextShell. Tell the user
what to do, wait, then call `session_open` again with the same target: it joins the connection in
progress. `host_key_changed` means the server now presents a different key than the one trusted
before: say so plainly, it may be a reinstall or an attack. `password` with
`savedPasswordRejected: true` means the password saved for the host was rejected. At most six
connections per server handshake at once; opening many tabs to one server simply queues.

## Errors

- `forbidden`: forbidden command, or access halted (the user pressed 紧急停止). Tell the user.
- `disconnected`: the session dropped or was closed (its id stays recognisable for a while); open it
  again. From `exec` it also means the connection was lost mid-command: check what happened first. `not_found`: no such session, host, request or command.
- `auth_failed`, `host_key` (not trusted or changed), `refused` (also a server that drops the handshake,
  e.g. over its connection limit), `unreachable`, `timeout`: why a `session_open` failed; the message
  carries a short cause. Do not retry in a loop.
- `invalid_argument`: the message names the field and its allowed range (`timeoutSec` 1-120,
  `waitFor.timeoutSec` 1-60, `limit` 1-200, `offset` ≥ 0, …); unknown fields and enum values are refused.
- `cwd_missing`: the tab's directory no longer exists; nothing ran.
- `busy`: another exec is running on this session; wait and retry later.
- `token_missing`: ask the user to turn on 设置 → Agent → 开启 MCP in NextShell. `unauthorized`:
  the token was rotated or points at another profile. `app_unavailable`: NextShell is not running or
  MCP is off. `incompatible_version`: the app and `@nextshell/mcp` differ; ask the user to update.
- `connection_lost`: the call may already have run. Inspect the terminal first; never replay a
  command or an old `requestId` automatically.

## Not available on purpose

No credentials, no file transfer, no local filesystem, no `exec` on local shells, no way around
the tiers. Do file work on the remote side with `cat`/`sed`/`tar` through `exec`.
