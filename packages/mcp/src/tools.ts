import { z } from "zod";

/**
 * Tool list of NextShell 2 (v2/internal/mcpendpoint Tools()), kept static so stdio clients can
 * discover tools while the app is closed. The app validates every call itself and replies with
 * `{ ok, data?, error? }`; the bridge passes results through untouched.
 */
const target = z.string().min(1).max(128).describe("Live session id returned by session_list");
// Tools that take a requestId are polled with that id alone, so their other fields are optional.
const requestId = z
  .string()
  .optional()
  .describe(
    "Continue an earlier { status: 'pending' } request: pass ONLY the requestId (the request lives 2 minutes)"
  );
const readOnly = {
  readOnlyHint: true,
  destructiveHint: false,
  idempotentHint: true,
  openWorldHint: true
} as const;
const mutating = {
  readOnlyHint: false,
  destructiveHint: true,
  idempotentHint: false,
  openWorldHint: true
} as const;

export const toolDefinitions = {
  host_list: {
    title: "查找服务器",
    description:
      "Find saved servers by substring of name, host or tags (max 20). Returns metadata only, never credentials. Use a host id as session_open's target.",
    inputSchema: {
      query: z.string().max(200).optional(),
      limit: z.number().int().min(1).max(200).optional()
    },
    annotations: readOnly
  },
  session_list: {
    title: "列出会话",
    description:
      "List the terminal tabs open in NextShell: session id, title, cwd, whether it is a local shell, and whether it is still connected. Pass a connected remote session id as target to the other tools.",
    inputSchema: {},
    annotations: readOnly
  },
  session_read: {
    title: "读取会话屏幕",
    description:
      "Read the visible screen of a live session (secrets are redacted). `lines` counts back from the bottom (default 200, max 2000). Only mode `screen` exists.",
    inputSchema: {
      target,
      lines: z.number().int().min(1).max(2000).optional(),
      mode: z.enum(["screen"]).optional()
    },
    annotations: readOnly
  },
  session_history: {
    title: "读取会话命令记录",
    description:
      "Per-command history of a session. Currently reports integrationAvailable:false with no entries; use session_read instead.",
    inputSchema: { target, limit: z.number().int().min(1).max(200).optional() },
    annotations: readOnly
  },
  command_search: {
    title: "检索命令库",
    description:
      "Search the user's saved command library by substring of name or command, so you can reuse commands the user curated.",
    inputSchema: {
      query: z.string().max(200).optional(),
      limit: z.number().int().min(1).max(200).optional()
    },
    annotations: readOnly
  },
  session_open: {
    title: "请求打开服务器",
    description:
      "Open a connection to a saved host (id from host_list). A host that already has a connected tab is reused. Needs the user's approval in NextShell unless they turned on auto-approve: the first call may return { status: 'pending', requestId }; call again with only that requestId. A `denied` error means the user refused: stop and ask them, never retry on your own.",
    inputSchema: {
      target: z.string().min(1).max(128).optional().describe("Connection id from host_list"),
      reason: z.string().max(300).optional().describe("Shown to the user in the approval prompt"),
      requestId
    },
    annotations: mutating
  },
  command_save: {
    title: "保存快速命令",
    description:
      "Save or update a command in the user's library (pass an id from command_search to update). Needs approval like exec.",
    inputSchema: {
      id: z.string().optional(),
      name: z.string().min(1).max(256).optional(),
      command: z.string().min(1).max(8192).optional(),
      group: z.string().max(256).optional(),
      workspaceId: z.string().optional(),
      appendCr: z.boolean().optional(),
      requestId
    },
    annotations: mutating
  },
  exec: {
    title: "在会话里执行命令",
    description:
      "Run a shell command on the REMOTE host of a connected session over a separate channel (invisible in the tab; local shell tabs are not supported). Returns output, stderr, exitCode and actualCwd, with secrets redacted. Needs the user's approval in NextShell unless they turned on auto-approve: the first call may return { status: 'pending', requestId } while the prompt is up; call exec again with ONLY that requestId. `denied` means the user refused: stop and ask them, never retry on your own. Dangerous or user-blacklisted commands fail with `forbidden`, and commands touching .env files fail with `consent_required` until the user agrees (then pass allowSensitive: true; those always need a manual click). `timeoutSec` max 120 (default 30).",
    inputSchema: {
      target: target.optional(),
      command: z.string().min(1).max(8192).optional(),
      timeoutSec: z.number().int().min(1).max(120).optional(),
      allowSensitive: z.boolean().optional(),
      requestId
    },
    annotations: mutating
  },
  session_send_signal: {
    title: "向会话发送控制信号",
    description:
      "Send a control character to a live session: interrupt (Ctrl-C), eof (Ctrl-D), suspend (Ctrl-Z) or quit (Ctrl-\\). Needs approval like exec.",
    inputSchema: {
      target: target.optional(),
      signal: z.enum(["interrupt", "eof", "suspend", "quit"]).optional(),
      requestId
    },
    annotations: mutating
  },
  session_focus: {
    title: "聚焦终端标签",
    description:
      "Bring a session's tab to the front in NextShell, e.g. to hand something back to the user. Needs approval like exec.",
    inputSchema: { target: target.optional(), requestId },
    annotations: mutating
  }
} as const;

export const INSTRUCTIONS =
  "NextShell lends you the terminal tabs the user already opened; you never see credentials. session_list shows open tabs; host_list finds saved hosts and session_open opens one. exec runs a command on the remote host of a connected tab. Every change (exec, session_open, command_save, session_send_signal, session_focus) needs the user's approval in NextShell unless they enabled auto-approve for a while; on { status: 'pending', requestId } call the same tool again with only that requestId. `denied` means stop and ask the user; `forbidden` is final; `consent_required` means a .env file is involved: ask the user first. Dangerous commands are always blocked. Outputs are redacted.";
