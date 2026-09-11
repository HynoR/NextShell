import { z } from "zod";

export const agentErrorSchema = z.object({
  code: z.string(),
  message: z.string()
});

/**
 * Uniform envelope so an agent can branch on `ok` without knowing which failure
 * mode (`not_found` / `forbidden` / `human_intervention` / …) a given tool can
 * produce.
 */
export const outputShape = <T extends z.ZodTypeAny>(data: T) => ({
  ok: z.boolean(),
  data: data.optional(),
  error: agentErrorSchema.optional()
});

/** Every mutating tool takes a live session id straight from `session_list`. */
export const sessionIdInputDescription = "Live session id returned by session_list";

export const READ_ONLY_ANNOTATIONS = {
  readOnlyHint: true,
  destructiveHint: false,
  idempotentHint: true,
  openWorldHint: true
} as const;

export const DESTRUCTIVE_ANNOTATIONS = {
  readOnlyHint: false,
  destructiveHint: true,
  idempotentHint: false,
  openWorldHint: true
} as const;

export const sessionInfoSchema = z.object({
  id: z.string(),
  connectionId: z.string().nullable(),
  connectionName: z.string().nullable(),
  host: z.string().nullable(),
  title: z.string(),
  status: z.string(),
  type: z.string(),
  createdAt: z.string(),
  cwd: z.string().nullable(),
  lastCommand: z.string().nullable()
});

export const savedCommandSchema = z.object({
  id: z.string(),
  name: z.string(),
  description: z.string().optional(),
  group: z.string(),
  command: z.string(),
  appendCr: z.boolean().optional(),
  createdAt: z.string(),
  updatedAt: z.string()
});

export const agentToolDefinitions = {
  session_list: {
    title: "列出会话",
    description:
      "List the terminal tabs the user currently has open in NextShell — the only sessions an agent may touch. Each entry carries the session id, connection name, host and status. cwd is the directory the shell last reported via OSC 7 and is trustworthy even for background tabs; it is null only when the session never reported one (no shell integration). Pass a session id as exec's target; a background exec inherits that cwd.",
    inputSchema: {},
    outputSchema: outputShape(
      z.object({
        sessions: z.array(sessionInfoSchema),
        truncated: z.boolean()
      })
    ),
    annotations: READ_ONLY_ANNOTATIONS
  },
  session_history: {
    title: "读取会话命令记录",
    description:
      "Read OSC-tracked commands, exit codes and bounded raw output for one live session. Sessions without shell integration report limited capability instead of guessed command text.",
    inputSchema: {
      target: z.string().min(1).describe(sessionIdInputDescription),
      limit: z.number().int().min(1).max(200).optional(),
      stripAnsi: z.boolean().optional()
    },
    outputSchema: outputShape(
      z.object({
        sessionId: z.string(),
        integrationAvailable: z.boolean(),
        entries: z.array(
          z.object({
            command: z.string().nullable(),
            exitCode: z.number().nullable(),
            startedAt: z.string(),
            finishedAt: z.string().nullable(),
            output: z.string(),
            truncated: z.boolean()
          })
        ),
        truncated: z.boolean()
      })
    ),
    annotations: READ_ONLY_ANNOTATIONS
  },
  session_read: {
    title: "读取会话屏幕",
    description:
      "Read the rendered screen of a live session, including background tabs. This is a real terminal emulation, so a full-screen program (top, htop, vim, an interactive installer) comes back as the frame a human would see rather than a pile of cursor-addressing escapes. Use `screen` for the current viewport and `scrollback` to include what scrolled off; for command output with exit codes prefer session_history.",
    inputSchema: {
      target: z.string().min(1).describe(sessionIdInputDescription),
      mode: z
        .enum(["screen", "scrollback"])
        .optional()
        .describe("`screen` (default) is the visible viewport; `scrollback` includes history"),
      lines: z
        .number()
        .int()
        .min(1)
        .max(2000)
        .optional()
        .describe("Lines to return, counted back from the bottom"),
      stripAnsi: z
        .boolean()
        .optional()
        .describe("Defaults to true; pass false to keep colour and style sequences")
    },
    outputSchema: outputShape(
      z.object({
        sessionId: z.string(),
        mode: z.enum(["screen", "scrollback"]),
        content: z.string(),
        lines: z.number(),
        cols: z.number(),
        rows: z.number(),
        scrollbackLines: z.number(),
        truncated: z.boolean()
      })
    ),
    annotations: READ_ONLY_ANNOTATIONS
  },
  host_list: {
    title: "查找服务器",
    description:
      "Find saved servers. Without query: the 10 hosts the user connected to most recently. With query: substring match on name, host, tags, group and notes (max 20). openSessions > 0 means a tab is already open — use session_list to get its id instead of session_open. Returns metadata only, never credentials.",
    inputSchema: {
      query: z.string().max(200).optional().describe("Keyword; omit for the recent list")
    },
    outputSchema: outputShape(
      z.object({
        mode: z.enum(["recent", "search"]),
        hosts: z.array(
          z.object({
            id: z.string(),
            name: z.string(),
            host: z.string(),
            port: z.number(),
            username: z.string(),
            groupPath: z.string(),
            tags: z.array(z.string()),
            lastConnectedAt: z.string().nullable(),
            openSessions: z.number()
          })
        ),
        truncated: z.boolean()
      })
    ),
    annotations: READ_ONLY_ANNOTATIONS
  },
  session_open: {
    title: "请求打开服务器",
    description:
      "Ask the user to open a connection: NextShell raises its window and shows an authorization dialog; the tab opens only after the user clicks 授权. Returns status 'opened' with the session id (a host that already has a tab is reused without a dialog), or status 'pending' with a requestId when the user has not answered within this call — call again with the same requestId to keep waiting (the dialog stays up for 5 minutes). A 'denied' error means the user did not authorize it: stop and ask the user to confirm in NextShell before retrying.",
    inputSchema: {
      target: z.string().min(1).optional().describe("Connection id from host_list"),
      reason: z.string().max(300).optional().describe("Shown to the user in the dialog"),
      requestId: z.string().optional().describe("Continue waiting on an earlier pending request")
    },
    outputSchema: outputShape(
      z.object({
        status: z.enum(["opened", "pending"]),
        sessionId: z.string().optional(),
        requestId: z.string().optional(),
        reused: z.boolean().optional()
      })
    ),
    annotations: {
      readOnlyHint: false,
      destructiveHint: false,
      idempotentHint: false,
      openWorldHint: true
    }
  },
  command_search: {
    title: "检索命令库",
    description:
      "Search the user's saved quick-command library (local and workspace entries), so the agent can reuse commands the user already curated instead of inventing them.",
    inputSchema: {
      query: z.string().optional().describe("Substring filter over the command text and name"),
      limit: z.number().int().min(1).max(200).optional()
    },
    outputSchema: outputShape(
      z.object({
        matches: z.array(
          z.object({
            id: z.string(),
            command: z.string(),
            name: z.string().nullable(),
            group: z.string().nullable(),
            appendCr: z.boolean(),
            scope: z.enum(["local", "workspace"]),
            workspaceId: z.string().optional(),
            workspaceName: z.string().optional(),
            lastUsedAt: z.string().nullable()
          })
        ),
        total: z.number(),
        truncated: z.boolean()
      })
    ),
    annotations: READ_ONLY_ANNOTATIONS
  },
  command_save: {
    title: "保存快速命令",
    description:
      "Save or update a quick command in the user's library, exactly like the in-app edit dialog. Pass an id (from command_search) to update an existing entry, or a workspaceId to write into that workspace's shared list.",
    inputSchema: {
      id: z.string().optional().describe("Existing entry id to update; omit to create"),
      workspaceId: z
        .string()
        .optional()
        .describe("Write into this workspace's command list instead of the local library"),
      name: z.string().min(1).max(200),
      group: z.string().max(200).optional(),
      command: z
        .string()
        .min(1)
        .max(64 * 1024),
      appendCr: z
        .boolean()
        .optional()
        .describe("Run the command immediately when inserted, instead of just pasting it")
    },
    outputSchema: outputShape(
      z.object({
        command: savedCommandSchema,
        workspaceId: z.string().optional()
      })
    ),
    annotations: {
      readOnlyHint: false,
      destructiveHint: false,
      idempotentHint: true,
      openWorldHint: true
    }
  },
  exec: {
    title: "在会话里执行命令",
    description:
      "Run a shell command in a live session. Two things are the USER's settings in NextShell's Agent panel, never your choice: (1) how it runs — foreground types the command into the tab (NextShell comes to the front, the user watches it run in their real shell, the call waits up to 120s for the prompt to return) or background (a fresh SSH channel of the same connection, invisible in the tab, returns stdout/stderr; local shell tabs always run in the foreground) — read `mode` in the response; (2) whether each command needs the user's click first — if it does, the first call returns `{ status: \"pending\", requestId }` while the approval dialog is up: call exec again with ONLY that requestId to keep waiting (the request lives 5 minutes), and a `denied` error means the user refused — stop and ask them, never retry on your own. Foreground `waitTimedOut: true` means no completion mark arrived and the command may still be running in the tab: poll session_read, never re-run it. To answer a prompt shown on screen (y/n), exec the answer text. Commands pass a dangerous-command blacklist (a hit is a hard error) and refuse .env files with consent_required until the user agrees; a foreground call fails with human_intervention when the user has unsubmitted text on their command line — stop and report instead of retrying.",
    inputSchema: {
      target: z.string().min(1).optional().describe(sessionIdInputDescription),
      command: z
        .string()
        .min(1)
        .max(256 * 1024)
        .optional(),
      timeoutSec: z
        .number()
        .int()
        .min(1)
        .max(3600)
        .optional()
        .describe("Background: how long to wait for exit. Foreground: capped at 120s"),
      allowSensitive: z
        .boolean()
        .optional()
        .describe(
          "Set true only after the user explicitly agreed to touch a .env file named in a consent_required error"
        ),
      requestId: z
        .string()
        .optional()
        .describe("Continue waiting on a pending approval; pass nothing else with it")
    },
    outputSchema: outputShape(
      z.union([
        z.object({
          status: z.literal("pending"),
          requestId: z.string()
        }),
        z.object({
          sessionId: z.string(),
          mode: z.enum(["foreground", "background"]),
          command: z.string(),
          exitCode: z.number().nullable(),
          output: z.string(),
          stderr: z.string().nullable(),
          waitTimedOut: z.boolean(),
          actualCwd: z.string().nullable()
        })
      ])
    ),
    annotations: DESTRUCTIVE_ANNOTATIONS
  },
  session_send_signal: {
    title: "向会话发送控制信号",
    description:
      "Send a control character to a live session: interrupt (Ctrl-C), eof (Ctrl-D), suspend (Ctrl-Z) or quit (Ctrl-\\). Use interrupt to stop something you started that is taking too long.",
    inputSchema: {
      target: z.string().min(1).describe(sessionIdInputDescription),
      signal: z.enum(["interrupt", "eof", "suspend", "quit"])
    },
    outputSchema: outputShape(
      z.object({
        sessionId: z.string(),
        signal: z.enum(["interrupt", "eof", "suspend", "quit"])
      })
    ),
    annotations: DESTRUCTIVE_ANNOTATIONS
  },
  session_focus: {
    title: "聚焦终端标签",
    description:
      "Bring NextShell's window forward and switch to this session's tab. Use it to hand something back to the user — a prompt that needs their judgement, or a result worth watching live.",
    inputSchema: { target: z.string().min(1).describe(sessionIdInputDescription) },
    outputSchema: outputShape(z.object({ sessionId: z.string() })),
    annotations: {
      readOnlyHint: false,
      destructiveHint: false,
      idempotentHint: true,
      openWorldHint: false
    }
  }
} as const;

export const AGENT_INSTRUCTIONS =
  "NextShell hands the agent the terminal tabs the user has already opened. session_list shows open tabs; host_list finds saved hosts (only the 10 most recent without a query); session_open asks the user to authorize opening one in NextShell — a denied result means stop and ask the user. exec is the only way to run commands; whether it runs in the foreground (typed into the tab, visible) or the background (separate channel, invisible), and whether each command first needs the user's click in NextShell, are the user's settings, not parameters — read `mode` in the response, and on `{ status: \"pending\", requestId }` call exec again with only that requestId; `denied` means stop and ask the user. Commands pass a dangerous-command blacklist, refuse to touch .env files with consent_required until the user agrees (then pass allowSensitive: true), and a foreground exec fails with human_intervention when the user has unsubmitted text on their command line — stop and report when that happens.";
