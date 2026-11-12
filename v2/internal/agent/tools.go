package agent

// ToolSpec describes one MCP tool. The app serves these as tools/list; the stdio bridge's offline
// list (packages/mcp/src/tools.generated.json) is generated from them and a test keeps the two equal.
type ToolSpec struct {
	Name, Title, Description string
	Schema                   map[string]any // JSON Schema of the arguments
	ReadOnly, Idempotent     bool
	Gated                    bool // changes something: needs the user's approval, or auto-approve
}

// Instructions is what clients get at initialize; the bridge serves the same text offline.
const Instructions = "NextShell lends you the terminal tabs the user already opened; you never see credentials. " +
	"session_list shows open tabs; host_list finds saved hosts and session_open opens one. exec runs a command on the remote host of a connected tab; " +
	"session_send_text types into the tab itself and session_read (with waitFor/sinceCursor) reads what came back. " +
	"Every change needs the user's approval in NextShell unless they enabled auto-approve for a while; on { status: 'pending', requestId } call the same tool again with the same arguments (or only the requestId). " +
	"Risky commands (privilege escalation, recursive deletes, piping into a shell, package or service changes, writes to system directories) are ALWAYS a manual approval, even under auto-approve; " +
	"`denied` means stop and ask the user; `forbidden` is final; `consent_required` means a credential file or the environment is involved: ask the user first. " +
	"session_open answers { status: 'user_action_required' } while the user must answer a host-key, password or one-time-code dialog in the app: tell the user and call session_open again later. " +
	"The policy guards against accidents, not against a determined or prompt-injected agent (a shell has endless ways around any classifier): auto-approve is not a sandbox, so never rephrase a refused or manually-approved command to get past its tier, ask the user instead. " +
	"After the first action NextShell does not raise windows or switch tabs for you; session_focus is the explicit hand-over to the user. Outputs are redacted. agent_status shows your pending requests."

func str(desc string, max int) map[string]any {
	return map[string]any{"type": "string", "description": desc, "maxLength": max}
}
func name(desc string, max int) map[string]any {
	s := str(desc, max)
	s["minLength"] = 1
	return s
}
func integer(desc string, min, max int) map[string]any {
	return map[string]any{"type": "integer", "description": desc, "minimum": min, "maximum": max}
}
func boolean(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}
func enum(desc string, values ...string) map[string]any {
	return map[string]any{"type": "string", "description": desc, "enum": values}
}
func object(required []string, props map[string]any) map[string]any {
	s := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

const requestIDHelp = "Optional shortcut to continue an earlier { status: 'pending' } request (it lives 2 minutes): pass the requestId instead of repeating the arguments. Repeating the identical call also continues it."

var (
	targetProp  = name("Live session id returned by session_list (or session_open).", 128)
	requestProp = str(requestIDHelp, 128)
)

var specs = []ToolSpec{
	{
		Name: "host_list", ReadOnly: true, Idempotent: true,
		Title:       "查找服务器",
		Description: "Find saved servers by substring of name, host or tags (case-insensitive; matched against the redacted text). Returns metadata only, never credentials, plus `total` matches, sorted by name (case-insensitive); page with `limit` (1-200, default 50) and `offset`. Use a host id as session_open's target.",
		Schema: object(nil, map[string]any{
			"query":  str("Substring to look for; empty lists everything.", 200),
			"limit":  integer("Page size, 1-200 (default 50).", 1, 200),
			"offset": integer("Skip this many matches (default 0).", 0, 1000000),
		}),
	},
	{
		Name: "session_list", ReadOnly: true, Idempotent: true,
		Title:       "列出会话",
		Description: "List the terminal tabs open in NextShell: session id, title, cwd, whether it is a local shell, and whether it is still connected. Pass a connected session id as target to the other tools. A session that is no longer connected answers `disconnected` there.",
		Schema:      object(nil, map[string]any{}),
	},
	{
		Name: "session_read", ReadOnly: true, Idempotent: true,
		Title: "读取会话屏幕",
		Description: "Read a live session. Without options it returns the visible screen (secrets redacted; `lines` counts back from the bottom, default 200, max 2000) and a `cursor`. " +
			"With `sinceCursor` it returns only the output received after that cursor (escape sequences removed, redacted), so you can follow a command typed with session_send_text. " +
			"With `waitFor` it blocks until the text appears in that new output (or timeoutSec passes, max 60), then returns `matched` and `timedOut`; without sinceCursor it waits for output arriving from now on. " +
			"The pattern is matched against the redacted text. `waitFor.on: \"screen\"` waits for text on the rendered screen instead (polled about ten times a second): use it for full-screen programs such as htop, vim or less, whose output stream is cursor movement. Local shell tabs have no output stream: waitFor polls the screen and sinceCursor is unavailable.",
		Schema: object([]string{"target"}, map[string]any{
			"target": targetProp,
			"lines":  integer("Lines counted back from the bottom (default 200).", 1, 2000),
			"mode":   enum("Only `screen` exists; incremental reads are selected by sinceCursor/waitFor.", "screen"),
			"sinceCursor": map[string]any{"type": "string", "description": "Opaque cursor from an earlier session_read or session_send_text response; return what arrived after it.",
				"pattern": "^[0-9]{1,18}$", "maxLength": 18},
			"waitFor": map[string]any{
				"type": "object", "description": "Wait until this appears in the new output.", "additionalProperties": false, "required": []string{"pattern"},
				"properties": map[string]any{
					"pattern":    name("A substring, or a Go (RE2) regular expression when it starts with `re:` (^ and $ match at line boundaries). Max 200 characters.", 200),
					"timeoutSec": integer("How long to wait, 1-60 (default 30).", 1, 60),
					"on":         enum("`stream` (default): the new output after the cursor. `screen`: the rendered screen, polled about ten times a second, for full-screen programs (htop, vim, less) whose output is cursor movement and not text; sinceCursor is ignored.", "stream", "screen"),
				},
			},
		}),
	},
	{
		Name: "session_history", ReadOnly: true, Idempotent: true,
		Title:       "读取会话命令记录",
		Description: "Per-command history of a session. Currently reports integrationAvailable:false with no entries (it needs OSC 133 prompt marks); use session_read instead.",
		Schema:      object([]string{"target"}, map[string]any{"target": targetProp, "limit": integer("Maximum entries, 1-200.", 1, 200)}),
	},
	{
		Name: "command_search", ReadOnly: true, Idempotent: true,
		Title:       "检索命令库",
		Description: "Search the user's saved command library by substring of name or command (matched against the redacted text), so you can reuse commands the user curated. Returns `total`; page with `limit` (1-200, default 50) and `offset`.",
		Schema: object(nil, map[string]any{
			"query":  str("Substring to look for; empty lists everything.", 200),
			"limit":  integer("Page size, 1-200 (default 50).", 1, 200),
			"offset": integer("Skip this many matches (default 0).", 0, 1000000),
		}),
	},
	{
		Name: "session_open", Gated: true,
		Title: "请求打开服务器",
		Description: "Open a connection to a saved host (id from host_list) and return the new session id. A host that already has a connected tab is reused. The tab opens in the background: it does not switch tabs or take the keyboard. " +
			"Needs the user's approval in NextShell unless they turned on auto-approve: the first call may return { status: 'pending', requestId }; call again with the same arguments. A `denied` error means the user refused: stop and ask them, never retry on your own. " +
			"If the connection needs a decision from the user (trust a host key, type a password or one-time code) the call returns at once with { status: 'user_action_required', reason: 'host_key'|'host_key_changed'|'password'|'otp' } while the dialog stays open in the app (host_key_changed: the server's key differs from the trusted one; password with savedPasswordRejected: true: the saved password was rejected): tell the user, then call session_open again with the same target; it joins the pending connection. " +
			"Failures say why: host_key, auth_failed, refused (also a server that drops the handshake, e.g. over its connection limit), unreachable, timeout. At most 6 connections per server handshake at once; further opens wait their turn.",
		Schema: object([]string{"target"}, map[string]any{
			"target":    name("Connection id from host_list.", 128),
			"reason":    str("Shown to the user in the approval prompt.", 300),
			"requestId": requestProp,
		}),
	},
	{
		Name: "command_save", Gated: true,
		Title: "保存快速命令",
		Description: "Save a command in the user's library, or update one: pass an `id` from command_search and only the fields you want to change (the others are kept). An unknown id or workspaceId answers `not_found`. " +
			"Without an id, name and command are required. The command text is judged like exec's: a forbidden command is refused (`forbidden`), a risky one is always a manual approval even under auto-approve. Needs approval like exec.",
		Schema: object(nil, map[string]any{
			"id":          str("Command id from command_search; update that command, merging only the fields provided.", 128),
			"name":        name("Display name (required for a new command).", 256),
			"command":     name("The command text (required for a new command).", 8192),
			"group":       str("Group name; an empty string clears it.", 256),
			"workspaceId": str("Cloud workspace to save into (new commands only); omit for the local library.", 128),
			"appendCr":    boolean("Press Enter after inserting the command."),
			"reason":      str("Shown to the user in the approval prompt.", 300),
			"requestId":   requestProp,
		}),
	},
	{
		Name: "exec", Gated: true,
		Title: "在会话里执行命令",
		Description: "Run a shell command on the REMOTE host of a connected session over a separate channel (invisible in the tab; local shell tabs are not supported). Returns output, stderr, exitCode and actualCwd, with secrets redacted; one exec at a time per session (`busy` otherwise). " +
			"The command runs as a whole in the tab's directory when the shell reports it (actualCwd; empty when the tab does not track its directory, then it starts in the login directory); if that directory is gone nothing runs and the answer is `cwd_missing`. " +
			"If the connection drops while it runs you get `disconnected` and the command may or may not have finished; closing the session kills it. A client that just drops its HTTP connection does not stop the command: use notifications/cancelled or a short `timeoutSec`. " +
			"Needs the user's approval in NextShell unless they turned on auto-approve: the first call may return { status: 'pending', requestId } while the prompt is up; call exec again with the same arguments. `denied` means the user refused: stop and ask them, never retry on your own. " +
			"Three tiers: forbidden commands (wiping disks or /, power off, kill everything or init, overwriting /etc/passwd, your user's blacklist) fail with `forbidden` and never run; " +
			"risky commands (sudo/su, rm -r, piping or here-stringing into a shell or interpreter, eval, writes to /etc /boot /usr /var/lib, package or service changes, firewall, userdel/passwd, ssh with a remote command, signalling sshd or init, nc -e, docker run --privileged or with / mounted) are ALWAYS a manual approval even under auto-approve (status `pending`, with `tier` and `why`); " +
			"commands touching credential files (.env*, ssh keys, *.pem/*.key/*.p12, .aws/credentials, .netrc, shell history, ~/.config/gcloud, /proc/*/environ, …) or dumping the environment (env, printenv, set, compgen -e) fail with `consent_required` until the user agrees; then pass allowSensitive: true (also a manual approval). " +
			"This is a guard against accidents, not a sandbox: do not rephrase a command to get around its tier. `timeoutSec` max 120 (default 30).",
		Schema: object([]string{"target", "command"}, map[string]any{
			"target":         targetProp,
			"command":        name("Shell command to run on the remote host.", 8192),
			"timeoutSec":     integer("Seconds before the command is cancelled, 1-120 (default 30).", 1, 120),
			"mode":           enum("Only `background` exists: a separate channel, invisible in the tab. Interactive input goes through session_send_text.", "background"),
			"allowSensitive": boolean("Set only after the user agreed that credential files or the environment may be touched."),
			"reason":         str("Shown to the user in the approval prompt.", 300),
			"requestId":      requestProp,
		}),
	},
	{
		Name: "session_send_text", Gated: true,
		Title: "向会话输入文本",
		Description: "Type into the terminal tab itself, like a user would: `text` (up to 4 KiB; the escapes \\r \\n \\t \\e \\xHH and \\\\ are interpreted, so use \\\\ for a literal backslash) and/or named `keys` (Enter, Up, Down, Left, Right, Esc, Tab, Backspace, Delete, F1-F12, PageUp, PageDown, Home, End, Ctrl-a to Ctrl-z), typed text first and keys after. " +
			"To mix them in order pass `input` instead, e.g. [{\"key\":\"Ctrl-b\"}, \"%\"] for tmux's split. " +
			"Use it for interactive programs, sudo password prompts you must not see, REPLs and tmux; use exec for ordinary commands. " +
			"Every complete line (ended by Enter) is classified like an exec command: forbidden lines are refused with `forbidden`, risky lines are always a manual approval, credential files need allowSensitive. At a shell prompt a line edited with history, completion, cursor or any other readline key cannot be inspected and counts as risky (Ctrl-O, Ctrl-J, Ctrl-M and Alt-Enter run the line like Enter). On the alternate screen (tmux, vim, htop) control keys and Esc belong to the program and are not held against you, while lines ended by Enter are still judged. " +
			"The response carries the `cursor` from before the keystrokes: pass it as session_read's sinceCursor (with waitFor) to read the result. If the user pressed a key in that tab within the last 3 seconds the call fails with `user_typing`; retry later.",
		Schema: object([]string{"target"}, map[string]any{
			"target": targetProp,
			"text":   str("Text to type. Include \\r (or a real newline) to press Enter.", 4096),
			"keys":   map[string]any{"type": "array", "description": "Named keys typed after `text`, in order.", "maxItems": 64, "items": map[string]any{"type": "string", "maxLength": 16}},
			"input": map[string]any{"type": "array", "maxItems": 64, "description": "Ordered steps instead of `text` and `keys`: strings (typed with the same escapes as `text`) and { key } items, so [{\"key\":\"Ctrl-b\"}, \"%\"] is tmux's split in one call.",
				"items": map[string]any{"oneOf": []map[string]any{
					str("Text to type.", 4096),
					object([]string{"key"}, map[string]any{"key": name("A named key: Enter, Esc, Tab, Backspace, Delete, Up, Down, Left, Right, Home, End, PageUp, PageDown, F1-F12, Ctrl-a to Ctrl-z.", 16)}),
				}}},
			"allowSensitive": boolean("Set only after the user agreed that credential files or the environment may be touched."),
			"reason":         str("Shown to the user in the approval prompt.", 300),
			"requestId":      requestProp,
		}),
	},
	{
		Name: "session_send_signal", Gated: true,
		Title:       "向会话发送控制信号",
		Description: "Send a control character to a live session: interrupt (Ctrl-C), eof (Ctrl-D), suspend (Ctrl-Z) or quit (Ctrl-\\). Needs approval like exec. Fails with `user_typing` while the user is typing in that tab.",
		Schema: object([]string{"target", "signal"}, map[string]any{
			"target":    targetProp,
			"signal":    enum("Which control character.", "interrupt", "eof", "suspend", "quit"),
			"reason":    str("Shown to the user in the approval prompt.", 300),
			"requestId": requestProp,
		}),
	},
	{
		Name: "session_focus", Gated: true,
		Title: "聚焦终端标签",
		Description: "Hand a session over to the user: select its tab in NextShell and, at most once per 60 seconds, raise the window. Use it when you need the user to look at or type into the tab (a password, a prompt, a result). " +
			"Other tools never switch tabs or take the keyboard. Needs approval like exec.",
		Schema: object([]string{"target"}, map[string]any{
			"target":    targetProp,
			"reason":    str("Shown to the user in the approval prompt.", 300),
			"requestId": requestProp,
		}),
	},
	{
		Name:        "session_close",
		Gated:       true,
		Title:       "关闭会话标签",
		Description: "Close a session's tab and disconnect it (also works on a tab whose connection already dropped); an exec still running there is killed first. Needs approval like exec. Closing a session that is already gone answers `alreadyClosed`.",
		Schema: object([]string{"target"}, map[string]any{
			"target":    targetProp,
			"reason":    str("Shown to the user in the approval prompt.", 300),
			"requestId": requestProp,
		}),
	},
	{
		Name: "agent_status", ReadOnly: true, Idempotent: true,
		Title:       "查看 Agent 状态",
		Description: "Show whether MCP is enabled and halted (emergency stop), whether auto-approve is on and until when, and this client's own pending approval requests (requestId, tool, summary, tier, age). Works while halted.",
		Schema:      object(nil, map[string]any{}),
	},
	{
		Name:        "request_cancel",
		Title:       "取消待批准请求",
		Description: "Withdraw one of your own pending approval requests (id from a { status: 'pending' } reply or agent_status) so the user is not asked. Unknown or other clients' ids answer `not_found`.",
		Schema:      object([]string{"requestId"}, map[string]any{"requestId": name("The pending request to withdraw.", 128)}),
	},
}

// Specs returns the tool definitions in listing order.
func Specs() []ToolSpec { return append([]ToolSpec{}, specs...) }

func spec(tool string) *ToolSpec {
	for i := range specs {
		if specs[i].Name == tool {
			return &specs[i]
		}
	}
	return nil
}
