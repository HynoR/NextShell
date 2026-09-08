---
name: nextshell
description: Operate servers through the tabs the user already opened in the NextShell desktop app (MCP tools session_list / session_read / exec / session_history / host_list / session_open). Use when the user says things like "看一下 xx 服务器", "在那台机器上跑一下", "帮我查下 web-1 的磁盘", or names a host they have open in NextShell.
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
2. `exec` — the only way to run a command. **How** it runs is the user's switch in
   NextShell's Agent panel, not your decision and not a parameter:
   - foreground (default): typed into the tab, NextShell comes to the front, the user
     watches it run in their real shell; the call waits up to 120s for the prompt to return.
   - background: a fresh SSH channel of the same connection, invisible in the tab,
     returns stdout/stderr. Local shell tabs always run in the foreground.
     Read `mode` in the response to know which happened. `waitTimedOut: true` means the
     command may still be running in the tab: `session_read` until it finishes, never re-run.
     To answer a y/n prompt on screen, `exec` the answer text.
   - **Permission mode** is the user's other switch. On Auto (default) commands run at once.
     On Permission every command first shows a dialog in NextShell and the call returns
     `{ status: "pending", requestId }`; call `exec` again with **only** that `requestId` to
     keep waiting (5 minutes total). `denied` means the user refused: stop and ask them,
     never retry on your own.
3. `session_read` to see what is on screen, `session_history` for recent commands with
   exit codes and output.
4. `session_send_signal` (Ctrl-C / Ctrl-D / Ctrl-Z) to stop something you started;
   `session_focus` to bring a tab to the front when the user should look at it.

## Rules

- A `human_intervention` error means the user has an unsubmitted command on that tab's
  command line. Stop, report what you were doing, and wait for instructions — do not retry.
- A blacklist error is final; there is no "allow once". Tell the user what was blocked.
- `denied` means the user declined a command in NextShell's dialog (or let it time out).
  Do not resend it; ask what they want instead.
- `consent_required` means the command touches a `.env` file: ask the user, and only after
  they agree retry with `allowSensitive: true`.
- If tools fail with a connection error, NextShell is not running or Agent access is off:
  设置 → Agent 接入 → 启用.

## Not available on purpose

No unrestricted host enumeration, no connecting without the user's click, no raw keystroke
injection, no file transfer, no local filesystem.
Do file work on the remote side with `cat`/`sed`/`tar` through the terminal.
