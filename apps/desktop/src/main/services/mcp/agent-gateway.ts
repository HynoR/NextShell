import { basename } from "node:path";

import type {
  AgentAccessLevel,
  AppPreferences,
  ConnectionProfile,
  MonitorSnapshot,
  RemoteFileEntry,
  SavedCommand,
  SessionStatus,
  SessionType
} from "@nextshell/core";
import type {
  AgentActivityEvent,
  AgentPromptRequest,
  AgentPromptResponse,
  SessionAuthOverrideInput
} from "@nextshell/shared";
import { classifyCommandRisk, type CommandRiskAssessment } from "@nextshell/terminal";

import {
  evaluateLocalPath,
  type LocalPathIntent,
  type LocalPathPolicyContext
} from "./local-path-policy";
import type { ScreenReadOptions, ScreenReadResult } from "./screen-mirror";
import type { AgentTransferSnapshot } from "./transfers";
import {
  ConnectionTargetAmbiguousError,
  ConnectionTargetNotFoundError,
  buildServerSummary,
  listServerSummaries,
  resolveConnectionTarget,
  searchServerSummaries,
  type ServerSummary
} from "./target-resolver";

// ─── Client identity ────────────────────────────────────────────────────────

export type AgentTransportKind = "socket" | "tcp";

export interface AgentClientIdentity {
  /** MCP session id. */
  id: string;
  name: string | null;
  version: string | null;
  transport: AgentTransportKind;
  /**
   * Rate-limit bucket. Must stay stable across MCP sessions of the same client:
   * keying the budget on the session id would let any caller reset it by
   * re-sending `initialize`.
   */
  rateKey: string;
}

// ─── Result envelope ────────────────────────────────────────────────────────

export type AgentErrorCode =
  | "invalid_argument"
  | "not_found"
  | "ambiguous"
  | "forbidden"
  | "too_large"
  | "rate_limited"
  | "busy"
  | "timeout"
  | "unavailable"
  | "internal";

export interface AgentToolError {
  code: AgentErrorCode;
  /**
   * Agent-facing text. Never carries a raw exception message, stack, host
   * connection string or key path — those are never exposed to the agent.
   */
  message: string;
  /** Populated for `ambiguous`, so the agent can ask the user to pick. */
  candidates?: ServerSummary[];
}

export type AgentToolResult<T> = { ok: true; data: T } | { ok: false; error: AgentToolError };

/** Lets a task inside `execute` fail with an already-classified agent error. */
export class AgentToolFailure extends Error {
  readonly toolError: AgentToolError;

  constructor(toolError: AgentToolError) {
    super(toolError.code);
    this.name = "AgentToolFailure";
    this.toolError = toolError;
  }
}

// ─── Agent-facing payloads ──────────────────────────────────────────────────

/**
 * Host metadata handed to MCP clients verbatim. Every credential-bearing field
 * of `ConnectionProfile` (`credentialRef`, `sshKeyId`, `sshKeyResourceId`,
 * `proxyId`, `hostFingerprint`, `notes`) is deliberately absent.
 */
export interface AgentHostInfo {
  nameId: string;
  name: string;
  host: string;
  port: number;
  user: string;
  groupPath: string;
  tags: string[];
  favorite: boolean;
  access: Exclude<AgentAccessLevel, "off">;
  connected: boolean;
  activeSessions: number;
  lastConnectedAt: string | null;
}

export interface AgentSessionInfo {
  id: string;
  connectionId: string | null;
  title: string;
  status: SessionStatus;
  type: SessionType;
  createdAt: string;
  /** Tracked by OscTap from OSC 7; `null` when the shell never reported one. */
  cwd: string | null;
  lastCommand: string | null;
}

export interface AgentHostDetail extends AgentHostInfo {
  sessions: AgentSessionInfo[];
  monitor: MonitorSnapshot | null;
}

export interface AgentFileEntry {
  name: string;
  path: string;
  type: RemoteFileEntry["type"];
  size: number;
  permissions: string;
  owner: string;
  group: string;
  modifiedAt: string;
}

export interface AgentRemoteFileStat {
  path: string;
  type: "file" | "directory" | "link" | "other";
  size: number;
  permissions: string;
  uid: number;
  gid: number;
  modifiedAt: string;
  accessedAt: string;
}

export interface AgentRemoteFileChunk {
  bytes: Buffer;
  /** True when the source was longer than the requested byte budget. */
  truncated: boolean;
}

export interface AgentHostListPayload {
  hosts: AgentHostInfo[];
  total: number;
  truncated: boolean;
}

export interface AgentSessionListPayload {
  sessions: AgentSessionInfo[];
  truncated: boolean;
}

export interface AgentFileListPayload {
  path: string;
  entries: AgentFileEntry[];
  total: number;
  truncated: boolean;
}

export interface AgentFileReadPayload {
  path: string;
  encoding: "utf-8" | "base64";
  content: string;
  bytes: number;
  size: number;
  truncated: boolean;
}

export interface AgentCommandMatch {
  command: string;
  source: "library";
  name: string | null;
  group: string | null;
  lastUsedAt: string | null;
}

export interface AgentCommandSearchPayload {
  matches: AgentCommandMatch[];
  total: number;
  truncated: boolean;
  /**
   * Only the user's curated command library is reachable. The global shell
   * history is not a source here: it is unattributed (no connection id, so
   * entries typed against unauthorized hosts and local shells cannot be
   * filtered out) and routinely carries inline credentials.
   */
  source: "library";
}

export interface AgentSessionHistoryEntry {
  command: string | null;
  exitCode: number | null;
  startedAt: string;
  finishedAt: string | null;
  output: string;
  truncated: boolean;
}

export interface AgentSessionHistoryPayload {
  sessionId: string;
  integrationAvailable: boolean;
  entries: AgentSessionHistoryEntry[];
  truncated: boolean;
}

export interface AgentSessionScreenPayload extends ScreenReadResult {
  sessionId: string;
}

export interface AgentSendKeysPayload {
  sessionId: string;
  bytes: number;
  submitted: boolean;
  /**
   * Populated only when `waitForPrompt` was requested *and* the remote reported
   * an OSC 133 `D` mark. `null` means the wait timed out or the remote has no
   * shell integration — never a guessed result.
   */
  completed: {
    command: string | null;
    exitCode: number | null;
    output: string;
    truncated: boolean;
  } | null;
  /** True when `waitForPrompt` was asked for but no completion mark arrived. */
  waitTimedOut: boolean;
}

export interface AgentSessionOpenPayload {
  sessionId: string;
  connectionId: string;
  title: string;
  status: SessionStatus;
}

export type AgentControlSignal = "interrupt" | "eof" | "suspend" | "quit";

export interface AgentExecPayload {
  connectionId: string;
  command: string;
  stdout: string;
  stderr: string;
  exitCode: number;
  actualCwd: string | null;
  executedAt: string;
  risk: CommandRiskAssessment;
}

export interface AgentWritePayload {
  path: string;
  bytes: number;
}

export interface AgentMutationPayload {
  path: string;
}

export interface AgentRenamePayload {
  from: string;
  to: string;
}

export interface AgentLocalFileStat {
  type: "file" | "directory" | "other";
  size: number;
}

// ─── Dependencies ───────────────────────────────────────────────────────────

/**
 * Narrow view of the main-process services the gateway needs. Kept as an
 * interface (rather than `ServiceContainer`) so the authorization surface
 * cannot reach anything it was not explicitly handed.
 */
export interface AgentGatewayDeps {
  listConnections: () => ConnectionProfile[];
  isConnectionOnline: (connectionId: string) => boolean;
  listSessions: () => AgentSessionInfo[];
  getMonitorSnapshot: (connectionId: string) => Promise<MonitorSnapshot | null>;
  listRemoteFiles: (connectionId: string, remotePath: string) => Promise<RemoteFileEntry[]>;
  statRemoteFile: (connectionId: string, remotePath: string) => Promise<AgentRemoteFileStat>;
  readRemoteFile: (
    connectionId: string,
    remotePath: string,
    maxBytes: number,
    signal: AbortSignal
  ) => Promise<AgentRemoteFileChunk>;
  listSavedCommands: (query: { keyword?: string; group?: string }) => SavedCommand[];
  getSessionHistory: (sessionId: string) => {
    integrationAvailable: boolean;
    entries: AgentSessionHistoryEntry[];
  } | null;
  /** `null` when the session is not mirrored (ScreenMirror capacity or a host the agent cannot reach). */
  readSessionScreen: (
    sessionId: string,
    options: ScreenReadOptions
  ) => Promise<ScreenReadResult | null>;
  // ── Tier 3: PTY takeover ──
  writeSession: (sessionId: string, data: string) => void;
  /** Epoch millis of the last real keystroke in that session, or `null`. */
  lastUserInputAt: (sessionId: string) => number | null;
  /** Resolves on the next OSC 133 `D`; `null` on timeout or missing integration. */
  waitForCommandCompletion: (
    sessionId: string,
    timeoutMs: number
  ) => Promise<{
    command: string | null;
    exitCode: number | null;
    output: string;
    truncated: boolean;
  } | null>;
  openSession: (connectionId: string) => Promise<{
    id: string;
    title: string;
    status: SessionStatus;
  }>;
  closeSession: (sessionId: string) => Promise<void>;
  /** Brings the tab to the front and raises the window. */
  focusSession: (sessionId: string) => void;
  /** Tells the GUI which sessions an agent is driving, so tabs can be badged. */
  setSessionAgentControlled: (sessionId: string, clientName: string | null) => void;
  clearSessionAgentControlled: (sessionId: string) => void;
  execCommand: (
    connectionId: string,
    command: string,
    options: { cwd?: string; signal: AbortSignal; authOverride?: SessionAuthOverrideInput }
  ) => Promise<{
    stdout: string;
    stderr: string;
    exitCode: number;
    executedAt: string;
    cwd?: string;
  }>;
  // ── Tier 2: remote mutation ──
  writeRemoteFile: (connectionId: string, remotePath: string, content: Buffer) => Promise<void>;
  makeRemoteDirectory: (connectionId: string, remotePath: string) => Promise<void>;
  renameRemotePath: (connectionId: string, fromPath: string, toPath: string) => Promise<void>;
  deleteRemotePath: (
    connectionId: string,
    remotePath: string,
    type: "file" | "directory" | "link"
  ) => Promise<void>;
  // ── Tier 2: transfers ──
  /** `null` when the path does not exist. Never throws. */
  statLocalPath: (localPath: string) => AgentLocalFileStat | null;
  /** Local-path policy context; read per call so preference edits take effect live. */
  localPathContext: () => Omit<LocalPathPolicyContext, "realpath">;
  startUpload: (input: {
    clientId: string;
    connectionId: string;
    localPath: string;
    remotePath: string;
    packed: boolean;
  }) => AgentTransferSnapshot;
  startDownload: (input: {
    clientId: string;
    connectionId: string;
    remotePath: string;
    localPath: string;
  }) => AgentTransferSnapshot;
  getTransfer: (taskId: string, clientId: string) => AgentTransferSnapshot | undefined;
  cancelTransfer: (taskId: string) => boolean;
  runningTransferCount: (clientId: string) => number;
  retainConnection: (connectionId: string) => () => void;
  closeConnectionIfIdle: (connectionId: string) => Promise<void>;
  promptUser: (request: Omit<AgentPromptRequest, "id">) => Promise<AgentPromptResponse>;
  notifyUser: (title: string, message: string) => void;
  emitActivity: (event: AgentActivityEvent) => void;
  getPreferences: () => AppPreferences;
  now?: () => number;
}

export interface AgentGatewayLimits {
  /** Sliding-window call budget per MCP client. */
  callsPerMinute: number;
  /** Concurrent in-flight calls against a single host. */
  perHostConcurrency: number;
  /** Ceiling applied on top of `preferences.agent.execTimeoutSec`. */
  maxCallTimeoutMs: number;
  maxListItems: number;
  maxFileBytes: number;
  /** Inline `file_write` payload ceiling; anything larger belongs in a transfer. */
  maxWriteBytes: number;
  /** Concurrent transfers one client may have in flight. */
  maxConcurrentTransfers: number;
  /**
   * How recently a human keystroke blocks agent injection into the same PTY.
   * The agent stands down; it does not race the person at the keyboard.
   */
  userInputPreemptionMs: number;
  /** Ceiling on `session_send_keys` `waitForPrompt`. */
  maxWaitForPromptMs: number;
}

export const DEFAULT_AGENT_GATEWAY_LIMITS: AgentGatewayLimits = {
  callsPerMinute: 60,
  perHostConcurrency: 4,
  maxCallTimeoutMs: 120_000,
  maxListItems: 500,
  maxFileBytes: 256 * 1024,
  maxWriteBytes: 1024 * 1024,
  maxConcurrentTransfers: 4,
  userInputPreemptionMs: 3_000,
  maxWaitForPromptMs: 120_000
};

export interface AgentGatewayOptions {
  limits?: Partial<AgentGatewayLimits>;
}

export type AgentAccessRequirement = "read" | "write";

export interface AgentResolvedTarget {
  connection: ConnectionProfile;
  summary: ServerSummary;
  level: Exclude<AgentAccessLevel, "off">;
  sessionId?: string;
}

// ─── Helpers ────────────────────────────────────────────────────────────────

const AGENT_REDACTED = "«redacted»";
const RATE_WINDOW_MS = 60_000;
/** Slack between a `waitForPrompt` deadline and the enclosing call timeout. */
const WAIT_FOR_PROMPT_HEADROOM_MS = 5_000;
const BINARY_SNIFF_BYTES = 4096;
const ANSI_ESCAPE_PATTERN = /[\u001b\u009b][[\]()#;?]*(?:(?:(?:[a-zA-Z\d]*(?:;[-a-zA-Z\d\x2f#&.:=?%@~_]+)*)?\u0007)|(?:(?:\d{1,4}(?:[;:]\d{0,4})*)?[\dA-PR-TZcf-nq-uy=><~]))/g;

const accessLevelOf = (connection: ConnectionProfile): AgentAccessLevel =>
  connection.agentAccess ?? "off";

const isAgentVisible = (connection: ConnectionProfile): boolean =>
  accessLevelOf(connection) !== "off";

/** Keep command text safe before returning it through the agent endpoint. */
const COMMAND_SECRET_RULES: Array<{ re: RegExp; replace: string }> = [
  {
    re: /-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z0-9 ]*PRIVATE KEY-----/g,
    replace: AGENT_REDACTED
  },
  { re: /(bearer\s+)[A-Za-z0-9._\-+/=]+/gi, replace: `$1${AGENT_REDACTED}` },
  {
    re: /\b(pass(?:word|wd)?|pwd|token|secret|api[_-]?key|access[_-]?key|passphrase|auth)\b(\s*[=:]\s*)("?)([^\s"']+)\3/gi,
    replace: `$1$2${AGENT_REDACTED}`
  },
  { re: /(--(?:password|token)[=\s])[^\s'"]+/gi, replace: `$1${AGENT_REDACTED}` },
  { re: /(^|\s)(-[pa])[^\s'"]+/g, replace: `$1$2${AGENT_REDACTED}` },
  // FOO_PASSWORD=secret / AWS_SECRET_ACCESS_KEY=secret / api-key="secret"
  {
    re: /([A-Za-z0-9_.-]*(?:pass(?:word|wd)?|pwd|secret|token|api[_-]?key|access[_-]?key|passphrase|credential|auth)[A-Za-z0-9_.-]*\s*=\s*)("[^"]*"|'[^']*'|[^\s]+)/gi,
    replace: `$1${AGENT_REDACTED}`
  },
  // scheme://user:password@host
  { re: /([a-z][a-z0-9+.-]*:\/\/[^\s:/@]+:)[^\s@/]+(@)/gi, replace: `$1${AGENT_REDACTED}$2` },
  // curl -u user:password — only the `user:pass` form; a bare `-u user` prompts.
  {
    re: /((?:^|\s)(?:-u|--user)[=\s]+["']?[^\s:"']+:)[^\s"']+/g,
    replace: `$1${AGENT_REDACTED}`
  },
  // -p'secret' / -a"secret": quoted value glued to the flag.
  { re: /((?:^|\s)-[pa])("[^"]*"|'[^']*')/g, replace: `$1${AGENT_REDACTED}` },
  {
    re: /((?:^|\s)--(?:password|passwd|token|secret|passphrase)[=\s]+)("[^"]*"|'[^']*'|[^\s]+)/gi,
    replace: `$1${AGENT_REDACTED}`
  },
  // sshpass -p secret as a separate argument. Scoped to sshpass on purpose:
  // a blanket `-p <value>` rule would also redact `docker run -p 8080:80`.
  {
    re: /(\bsshpass\s+(?:-\S+\s+)*?-p[=\s]+)("[^"]*"|'[^']*'|[^\s]+)/gi,
    replace: `$1${AGENT_REDACTED}`
  },
  // echo 'secret' | sudo -S …  — the piped-in value is a password by construction
  {
    re: /((?:^|\s)echo\s+)("[^"]*"|'[^']*'|[^\s|]+)(\s*\|\s*(?:\S*\s+)*sudo\s+(?:-\S+\s+)*-S\b)/gi,
    replace: `$1${AGENT_REDACTED}$3`
  }
];

const redactText = (value: string): string => {
  let out = value;
  for (const rule of COMMAND_SECRET_RULES) {
    out = out.replace(rule.re, rule.replace);
  }
  return out;
};

const clampInt = (
  value: number | undefined,
  fallback: number,
  min: number,
  max: number
): number => {
  if (typeof value !== "number" || !Number.isFinite(value)) {
    return fallback;
  }
  return Math.min(max, Math.max(min, Math.trunc(value)));
};

const ERROR_PATTERNS: Array<{ re: RegExp; code: AgentErrorCode; message: string }> = [
  { re: /no such file|enoent|does not exist/i, code: "not_found", message: "Path does not exist" },
  {
    re: /permission denied|eacces|eperm|access denied/i,
    code: "forbidden",
    message: "The remote host denied access to that path"
  },
  { re: /eisdir|is a directory/i, code: "invalid_argument", message: "Path is a directory" },
  { re: /enotdir|not a directory/i, code: "invalid_argument", message: "Path is not a directory" },
  {
    re: /etimedout|timed ?out/i,
    code: "timeout",
    message: "The remote host did not respond in time"
  },
  {
    re: /econnrefused|econnreset|ehostunreach|not connected|connection closed|channel open failure/i,
    code: "unavailable",
    message: "The host connection is unavailable"
  }
];

/**
 * Exception text is an exfiltration surface (it carries paths, hosts and
 * occasionally credentials), so nothing from the original error reaches the
 * agent: only a code and a fixed sentence chosen by pattern match.
 */
const classifyError = (error: unknown): AgentToolError => {
  const raw = error instanceof Error ? `${error.name}: ${error.message}` : String(error);
  for (const pattern of ERROR_PATTERNS) {
    if (pattern.re.test(raw)) {
      return { code: pattern.code, message: pattern.message };
    }
  }
  return { code: "internal", message: "The operation failed" };
};

/**
 * Absolute POSIX paths only: relative paths would silently resolve against a
 * server-side default the agent cannot see.
 */
export const normalizeRemotePath = (input: string): AgentToolResult<string> => {
  const trimmed = input.trim();
  if (!trimmed) {
    return { ok: false, error: { code: "invalid_argument", message: "path must not be empty" } };
  }
  if (trimmed.includes("\0")) {
    return {
      ok: false,
      error: { code: "invalid_argument", message: "path must not contain NUL bytes" }
    };
  }
  if (!trimmed.startsWith("/")) {
    return {
      ok: false,
      error: {
        code: "invalid_argument",
        message: "path must be absolute (start with /); ~ and relative paths are not accepted"
      }
    };
  }

  const resolved: string[] = [];
  for (const segment of trimmed.split("/")) {
    if (!segment || segment === ".") {
      continue;
    }
    if (segment === "..") {
      resolved.pop();
      continue;
    }
    resolved.push(segment);
  }
  return { ok: true, data: `/${resolved.join("/")}` };
};

const CONTROL_KEY_NAMES: Record<string, string> = {
  "\u0003": "<Ctrl-C>",
  "\u0004": "<Ctrl-D>",
  "\u001a": "<Ctrl-Z>",
  "\u001c": "<Ctrl-\\>",
  "\r": "<Enter>",
  "\n": "<LF>",
  "\t": "<Tab>",
  "\u001b": "<Esc>",
  "\u007f": "<Backspace>"
};

/**
 * Renders injected keystrokes for the confirmation dialog. Control bytes are
 * named rather than shown: an invisible `` in the middle of an otherwise
 * innocent-looking line is exactly what the dialog exists to expose, and a raw
 * escape sequence in a dialog is unreadable anyway.
 */
export const describeInjectedKeys = (text: string): string => {
  if (text.length === 0) return "（空输入）";
  let out = "";
  for (const char of text) {
    const named = CONTROL_KEY_NAMES[char];
    if (named) {
      out += named;
      continue;
    }
    const code = char.codePointAt(0) ?? 0;
    out += code < 0x20 || code === 0x7f ? `<0x${code.toString(16).padStart(2, "0")}>` : char;
  }
  return out;
};

const looksBinary = (bytes: Buffer): boolean => {
  const window = bytes.subarray(0, BINARY_SNIFF_BYTES);
  for (const byte of window) {
    if (byte === 0) {
      return true;
    }
  }
  return false;
};

// ─── Gateway ────────────────────────────────────────────────────────────────

/**
 * The single authorization surface for MCP tool calls. Tools never touch the
 * service container directly: every call goes through `execute`, which applies
 * host authorization, rate limiting, per-host concurrency, a timeout, output
 * truncation and activity events.
 */
export class AgentGateway {
  private readonly deps: AgentGatewayDeps;
  private readonly limits: AgentGatewayLimits;
  private readonly callWindows = new Map<string, number[]>();
  private readonly hostInflight = new Map<string, number>();
  private readonly rememberedApprovals = new Map<string, Set<string>>();
  private readonly approvedClients = new Set<string>();
  /**
   * Clients the user turned away. Without this a denied client re-prompts on
   * every single tool call, which floods the user with modal dialogs and trains
   * them to click "allow" — the exact failure mode the gate exists to prevent.
   */
  private readonly deniedClients = new Set<string>();
  /**
   * Sessions the agent opened itself. Their tab badge is persistent — the
   * session belongs to the agent for its whole life — whereas injecting into a
   * session the *user* opened only badges it for the duration of the write.
   */
  private readonly agentOwnedSessions = new Set<string>();
  private readonly pendingClientApprovals = new Map<string, Promise<boolean>>();
  private activitySequence = 0;
  /**
   * The global breaker. Once tripped every tool call fails immediately, without
   * a dialog, until the user re-arms it — the point of a kill switch is that it
   * does not negotiate.
   */
  private halted = false;

  constructor(deps: AgentGatewayDeps, options: AgentGatewayOptions = {}) {
    this.deps = deps;
    this.limits = { ...DEFAULT_AGENT_GATEWAY_LIMITS, ...options.limits };
  }

  get gatewayLimits(): AgentGatewayLimits {
    return this.limits;
  }

  get isHalted(): boolean {
    return this.halted;
  }

  /**
   * Trips or re-arms the breaker. Halting also forgets every remembered
   * approval: a user who hits stop is withdrawing consent, and "always allow
   * this command" must not survive that.
   */
  setHalted(halted: boolean): void {
    this.halted = halted;
    if (!halted) return;
    this.rememberedApprovals.clear();
    // Nothing is agent-driven any more, so no tab should still claim it is.
    for (const sessionId of this.agentOwnedSessions) {
      try {
        this.deps.clearSessionAgentControlled(sessionId);
      } catch {
        // Badge updates are best-effort; the breaker itself must not fail.
      }
    }
    this.agentOwnedSessions.clear();
  }

  /**
   * Drops buckets that have fully aged out. Deliberately not keyed on client
   * disconnect: a client that reconnects must inherit its remaining budget,
   * otherwise reconnecting is a free rate-limit reset.
   */
  pruneRateLimits(): void {
    const now = this.now();
    for (const [key, window] of this.callWindows) {
      const live = window.filter((timestamp) => now - timestamp < RATE_WINDOW_MS);
      if (live.length === 0) {
        this.callWindows.delete(key);
      } else {
        this.callWindows.set(key, live);
      }
    }
  }

  pruneClientSessions(activeSessionIds: ReadonlySet<string>): void {
    for (const id of this.approvedClients) {
      if (!activeSessionIds.has(id)) this.approvedClients.delete(id);
    }
    for (const id of this.deniedClients) {
      if (!activeSessionIds.has(id)) this.deniedClients.delete(id);
    }
    for (const id of this.rememberedApprovals.keys()) {
      if (!activeSessionIds.has(id)) this.rememberedApprovals.delete(id);
    }
  }

  /**
   * Connections the agent is allowed to know about at all. `off` (including a
   * missing field) means the host does not exist as far as MCP is concerned.
   */
  private authorizedConnections(): ConnectionProfile[] {
    return this.deps.listConnections().filter(isAgentVisible);
  }

  /**
   * Unauthorized hosts resolve to `not_found`, never to `forbidden`: the agent
   * must not be able to tell "exists but not granted" from "does not exist".
   */
  resolveTarget(
    target: string,
    requirement: AgentAccessRequirement
  ): AgentToolResult<AgentResolvedTarget> {
    const session = this.authorizedSessions().find((candidate) => candidate.id === target);
    const effectiveTarget = session?.connectionId ?? target;
    let resolved;
    try {
      resolved = resolveConnectionTarget(this.authorizedConnections(), effectiveTarget);
    } catch (error) {
      if (error instanceof ConnectionTargetAmbiguousError) {
        return {
          ok: false,
          error: {
            code: "ambiguous",
            message: `"${target}" matches ${error.candidates.length} hosts; pass one of the candidate nameId values`,
            candidates: error.candidates
          }
        };
      }
      if (error instanceof ConnectionTargetNotFoundError) {
        return {
          ok: false,
          error: {
            code: "not_found",
            message: `No host available to the agent matches "${target}"`
          }
        };
      }
      return { ok: false, error: classifyError(error) };
    }

    const level = accessLevelOf(resolved.connection);
    if (level === "off") {
      return {
        ok: false,
        error: { code: "not_found", message: `No host available to the agent matches "${target}"` }
      };
    }
    if (requirement === "write" && level !== "full") {
      return {
        ok: false,
        error: {
          code: "forbidden",
          message: `Host "${resolved.summary.name}" is granted read-only access to the agent`
        }
      };
    }
    return {
      ok: true,
      data: {
        connection: resolved.connection,
        summary: resolved.summary,
        level,
        ...(session ? { sessionId: session.id } : {})
      }
    };
  }

  // ─── Call plumbing ────────────────────────────────────────────────────────

  private now(): number {
    return this.deps.now?.() ?? Date.now();
  }

  private checkRateLimit(clientId: string): AgentToolError | null {
    const now = this.now();
    const window = (this.callWindows.get(clientId) ?? []).filter(
      (timestamp) => now - timestamp < RATE_WINDOW_MS
    );
    if (window.length >= this.limits.callsPerMinute) {
      this.callWindows.set(clientId, window);
      return {
        code: "rate_limited",
        message: `Rate limit reached (${this.limits.callsPerMinute} calls/minute); retry shortly`
      };
    }
    window.push(now);
    this.callWindows.set(clientId, window);
    return null;
  }

  private callTimeoutMs(): number {
    const seconds = this.deps.getPreferences().agent.execTimeoutSec;
    const millis = clampInt(seconds, 60, 1, 3600) * 1000;
    return Math.min(millis, this.limits.maxCallTimeoutMs);
  }

  private emitActivity(
    client: AgentClientIdentity,
    id: string,
    tool: string,
    status: AgentActivityEvent["status"],
    connectionId?: string,
    result?: string
  ): void {
    try {
      this.deps.emitActivity({
        id,
        clientName: client.name,
        tool,
        status,
        ...(connectionId ? { connectionId } : {}),
        summary: result ? `${tool}: ${result}` : tool,
        createdAt: new Date(this.now()).toISOString()
      });
    } catch {
      // Activity rendering is best-effort; authorization remains authoritative.
    }
  }

  private ensureClientApproved(client: AgentClientIdentity): Promise<boolean> {
    if (this.approvedClients.has(client.id)) return Promise.resolve(true);
    if (this.deniedClients.has(client.id)) return Promise.resolve(false);
    const pending = this.pendingClientApprovals.get(client.id);
    if (pending) return pending;
    const approval = this.deps
      .promptUser({
        kind: "confirm",
        title: "新的 Agent 客户端请求接入",
        message: `${client.name ?? "未知客户端"} (${client.transport}) 请求使用 NextShell 的 Agent 能力。`,
        details: `客户端版本：${client.version ?? "未知"}\n会话标识：${client.id}`
      })
      .then((response) => {
        const approved = !response.canceled && response.value === "approved";
        if (approved) this.approvedClients.add(client.id);
        else this.deniedClients.add(client.id);
        return approved;
      })
      .catch(() => false)
      .finally(() => this.pendingClientApprovals.delete(client.id));
    this.pendingClientApprovals.set(client.id, approval);
    return approval;
  }

  /**
   * Rate limit → client approval → per-host concurrency → preflight → timeout →
   * error sanitization → activity.
   *
   * Rate limiting deliberately runs *first*, ahead of anything that can open a
   * dialog: every user-facing prompt this call may raise (client approval,
   * command confirmation) has to be paid for out of the client's call budget,
   * otherwise a misbehaving client can bury the user under modal dialogs for
   * free. `preflight` runs after the budget and the approval gate and is
   * deliberately outside the call timeout — a prompt waits on a human and
   * carries its own timeout.
   *
   * The task is handed an `AbortSignal` that fires on timeout: without it a
   * timed-out call would only stop the caller waiting while the underlying
   * SSH/SFTP work kept consuming memory in the background.
   */
  private async execute<T>(
    client: AgentClientIdentity,
    tool: string,
    params: Record<string, unknown>,
    task: (signal: AbortSignal) => Promise<T>,
    options: {
      connectionId?: string;
      requestedTimeoutMs?: number;
      preflight?: () => Promise<void>;
    } = {}
  ): Promise<AgentToolResult<T>> {
    const { connectionId, requestedTimeoutMs, preflight } = options;
    const activityId = `${client.id}:${++this.activitySequence}`;
    const commandSummary = typeof params.command === "string" ? redactText(params.command) : undefined;
    this.emitActivity(client, activityId, tool, "running", connectionId, commandSummary);

    if (this.halted) {
      const error: AgentToolError = {
        code: "forbidden",
        message:
          "Agent access is halted from NextShell's Agent panel. Ask the user to resume it before retrying."
      };
      this.emitActivity(client, activityId, tool, "failed", connectionId, "halted");
      return { ok: false, error };
    }

    const rateError = this.checkRateLimit(client.rateKey);
    if (rateError) {
      this.emitActivity(client, activityId, tool, "failed", connectionId, rateError.code);
      return { ok: false, error: rateError };
    }

    if (!(await this.ensureClientApproved(client))) {
      const error: AgentToolError = {
        code: "forbidden",
        message: "The user did not approve this MCP client"
      };
      this.emitActivity(
        client,
        activityId,
        tool,
        "failed",
        connectionId,
        commandSummary ? `${commandSummary} → ${error.code}` : error.code
      );
      return { ok: false, error };
    }

    if (connectionId) {
      const inflight = this.hostInflight.get(connectionId) ?? 0;
      if (inflight >= this.limits.perHostConcurrency) {
        const error: AgentToolError = {
          code: "busy",
          message: `Too many concurrent calls against this host (limit ${this.limits.perHostConcurrency})`
        };
        this.emitActivity(client, activityId, tool, "failed", connectionId, error.code);
        return { ok: false, error };
      }
      this.hostInflight.set(connectionId, inflight + 1);
    }

    let timer: ReturnType<typeof setTimeout> | undefined;
    const controller = new AbortController();
    try {
      if (preflight) {
        await preflight();
      }
      const timeoutMs = Math.min(
        requestedTimeoutMs ?? this.callTimeoutMs(),
        this.limits.maxCallTimeoutMs
      );
      const timeout = new Promise<never>((_, reject) => {
        timer = setTimeout(() => {
          controller.abort();
          reject(new Error("ETIMEDOUT: agent tool call timed out"));
        }, timeoutMs);
      });
      const data = await Promise.race([task(controller.signal), timeout]);
      const exitCode =
        typeof data === "object" && data !== null && "exitCode" in data
          ? String((data as { exitCode: unknown }).exitCode)
          : null;
      this.emitActivity(
        client,
        activityId,
        tool,
        "succeeded",
        connectionId,
        commandSummary && exitCode !== null ? `${commandSummary} → exit ${exitCode}` : "ok"
      );
      return { ok: true, data };
    } catch (error) {
      const toolError = error instanceof AgentToolFailure ? error.toolError : classifyError(error);
      this.emitActivity(
        client,
        activityId,
        tool,
        "failed",
        connectionId,
        commandSummary ? `${commandSummary} → ${toolError.code}` : toolError.code
      );
      return { ok: false, error: toolError };
    } finally {
      if (timer) {
        clearTimeout(timer);
      }
      if (connectionId) {
        const inflight = this.hostInflight.get(connectionId) ?? 1;
        if (inflight <= 1) {
          this.hostInflight.delete(connectionId);
        } else {
          this.hostInflight.set(connectionId, inflight - 1);
        }
      }
    }
  }

  /**
   * Rejection before the work starts — a bad argument, an unauthorized target,
   * a local path the policy refuses. It still charges the call budget: these
   * paths answer questions ("does this host exist", "is this directory
   * off limits"), so leaving them free would make them an unmetered probe.
   */
  private failed<T>(
    client: AgentClientIdentity,
    tool: string,
    params: Record<string, unknown>,
    error: AgentToolError,
    connectionId?: string
  ): AgentToolResult<T> {
    if (this.halted) {
      const halted: AgentToolError = {
        code: "forbidden",
        message:
          "Agent access is halted from NextShell's Agent panel. Ask the user to resume it before retrying."
      };
      return { ok: false, error: halted };
    }
    const reported = this.checkRateLimit(client.rateKey) ?? error;
    return { ok: false, error: reported };
  }

  // ─── Host views ───────────────────────────────────────────────────────────

  private toHostInfo(connection: ConnectionProfile, sessions: AgentSessionInfo[]): AgentHostInfo {
    const summary = buildServerSummary(connection);
    const level = accessLevelOf(connection);
    const active = sessions.filter(
      (session) => session.connectionId === connection.id && session.status !== "disconnected"
    ).length;
    return {
      nameId: summary.nameId,
      name: summary.name,
      host: summary.host,
      port: summary.port,
      user: connection.username,
      groupPath: summary.groupPath,
      tags: summary.tags,
      favorite: summary.favorite,
      access: level === "full" ? "full" : "readonly",
      connected: this.safeOnline(connection.id),
      activeSessions: active,
      lastConnectedAt: connection.lastConnectedAt ?? null
    };
  }

  private safeOnline(connectionId: string): boolean {
    try {
      return this.deps.isConnectionOnline(connectionId);
    } catch {
      return false;
    }
  }

  private authorizedSessions(): AgentSessionInfo[] {
    const allowed = new Set(this.authorizedConnections().map((connection) => connection.id));
    return this.deps
      .listSessions()
      .filter((session) => session.connectionId !== null && allowed.has(session.connectionId));
  }

  // ─── Tier 0 tool surface ──────────────────────────────────────────────────

  async listHosts(
    client: AgentClientIdentity,
    input: { query?: string; limit?: number }
  ): Promise<AgentToolResult<AgentHostListPayload>> {
    const limit = clampInt(input.limit, 50, 1, this.limits.maxListItems);
    return this.execute(client, "host_list", { query: input.query ?? "", limit }, async () => {
      const connections = this.authorizedConnections();
      const sessions = this.authorizedSessions();
      const query = input.query?.trim() ?? "";
      const summaries = query
        ? searchServerSummaries(connections, query, connections.length)
        : listServerSummaries(connections);
      const byNameId = new Map(
        connections.map((connection) => [buildServerSummary(connection).nameId, connection])
      );
      const hosts = summaries
        .slice(0, limit)
        .map((summary) => byNameId.get(summary.nameId))
        .filter((connection): connection is ConnectionProfile => connection !== undefined)
        .map((connection) => this.toHostInfo(connection, sessions));
      return { hosts, total: summaries.length, truncated: summaries.length > hosts.length };
    });
  }

  async describeHost(
    client: AgentClientIdentity,
    input: { target: string }
  ): Promise<AgentToolResult<AgentHostDetail>> {
    const resolved = this.resolveTarget(input.target, "read");
    if (!resolved.ok) {
      return this.failed(client, "host_describe", { target: input.target }, resolved.error);
    }
    const connection = resolved.data.connection;
    return this.execute(
      client,
      "host_describe",
      { target: input.target },
      async () => {
        const sessions = this.authorizedSessions();
        const info = this.toHostInfo(connection, sessions);
        let monitor: MonitorSnapshot | null;
        try {
          monitor = await this.deps.getMonitorSnapshot(connection.id);
        } catch {
          monitor = null;
        }
        return {
          ...info,
          sessions: sessions.filter((session) => session.connectionId === connection.id),
          monitor
        };
      },
      { connectionId: connection.id }
    );
  }

  async listSessions(
    client: AgentClientIdentity,
    input: { target?: string }
  ): Promise<AgentToolResult<AgentSessionListPayload>> {
    let connectionId: string | undefined;
    if (input.target) {
      const resolved = this.resolveTarget(input.target, "read");
      if (!resolved.ok) {
        return this.failed(client, "session_list", { target: input.target }, resolved.error);
      }
      connectionId = resolved.data.connection.id;
    }

    return this.execute(
      client,
      "session_list",
      { target: input.target ?? "" },
      async () => {
        const sessions = this.authorizedSessions().filter(
          (session) => !connectionId || session.connectionId === connectionId
        );
        const capped = sessions.slice(0, this.limits.maxListItems);
        return { sessions: capped, truncated: capped.length < sessions.length };
      },
      { connectionId }
    );
  }

  async sessionHistory(
    client: AgentClientIdentity,
    input: { target: string; limit?: number; stripAnsi?: boolean }
  ): Promise<AgentToolResult<AgentSessionHistoryPayload>> {
    const params = { target: input.target, limit: input.limit, stripAnsi: input.stripAnsi };
    const session = this.authorizedSessions().find((candidate) => candidate.id === input.target);
    if (!session) {
      return this.failed(client, "session_history", params, {
        code: "not_found",
        message: "No active authorized session matches that id; call session_list first"
      });
    }
    const limit = clampInt(input.limit, 50, 1, this.limits.maxListItems);
    return this.execute(
      client,
      "session_history",
      params,
      async () => {
        const snapshot = this.deps.getSessionHistory(session.id);
        if (!snapshot) {
          throw new AgentToolFailure({
            code: "unavailable",
            message: "Session integration data is not available"
          });
        }
        const entries = snapshot.entries.slice(-limit).map((entry) => ({
          ...entry,
          command: entry.command === null ? null : redactText(entry.command),
          output: redactText(input.stripAnsi ? entry.output.replace(ANSI_ESCAPE_PATTERN, "") : entry.output)
        }));
        return {
          sessionId: session.id,
          integrationAvailable: snapshot.integrationAvailable,
          entries,
          truncated: snapshot.entries.length > entries.length
        };
      },
      { connectionId: session.connectionId ?? undefined }
    );
  }

  /**
   * The rendered screen, not the raw byte log. Reading a `top` or `vim` session
   * from raw bytes gives cursor-addressing sequences; only the emulator mirror
   * collapses those into the frame a human sees.
   */
  async readSessionScreen(
    client: AgentClientIdentity,
    input: { target: string; mode?: "screen" | "scrollback"; lines?: number; stripAnsi?: boolean }
  ): Promise<AgentToolResult<AgentSessionScreenPayload>> {
    const params = {
      target: input.target,
      mode: input.mode ?? "screen",
      lines: input.lines,
      stripAnsi: input.stripAnsi
    };
    const session = this.authorizedSessions().find((candidate) => candidate.id === input.target);
    if (!session) {
      return this.failed(client, "session_read", params, {
        code: "not_found",
        message: "No active authorized session matches that id; call session_list first"
      });
    }

    return this.execute(
      client,
      "session_read",
      params,
      async () => {
        const screen = await this.deps.readSessionScreen(session.id, {
          ...(input.mode ? { mode: input.mode } : {}),
          ...(input.lines === undefined ? {} : { lines: input.lines }),
          ...(input.stripAnsi === undefined ? {} : { stripAnsi: input.stripAnsi })
        });
        if (!screen) {
          throw new AgentToolFailure({
            code: "unavailable",
            message:
              "This session is not being mirrored yet; it has produced no output since NextShell started tracking it, or the screen-mirror capacity is exhausted"
          });
        }
        return { sessionId: session.id, ...screen, content: redactText(screen.content) };
      },
      { connectionId: session.connectionId ?? undefined }
    );
  }

  async listFiles(
    client: AgentClientIdentity,
    input: { target: string; path: string; limit?: number }
  ): Promise<AgentToolResult<AgentFileListPayload>> {
    const params = { target: input.target, path: input.path };
    const resolved = this.resolveTarget(input.target, "read");
    if (!resolved.ok) {
      return this.failed(client, "file_list", params, resolved.error);
    }
    const normalized = normalizeRemotePath(input.path);
    if (!normalized.ok) {
      return this.failed(
        client,
        "file_list",
        params,
        normalized.error,
        resolved.data.connection.id
      );
    }

    const connectionId = resolved.data.connection.id;
    const limit = clampInt(input.limit, this.limits.maxListItems, 1, this.limits.maxListItems);
    return this.execute(
      client,
      "file_list",
      params,
      async () => {
        const entries = await this.deps.listRemoteFiles(connectionId, normalized.data);
        const capped = entries.slice(0, limit).map((entry) => ({
          name: entry.name,
          path: entry.path,
          type: entry.type,
          size: entry.size,
          permissions: entry.permissions,
          owner: entry.owner,
          group: entry.group,
          modifiedAt: entry.modifiedAt
        }));
        return {
          path: normalized.data,
          entries: capped,
          total: entries.length,
          truncated: capped.length < entries.length
        };
      },
      { connectionId }
    );
  }

  async statFile(
    client: AgentClientIdentity,
    input: { target: string; path: string }
  ): Promise<AgentToolResult<AgentRemoteFileStat>> {
    const params = { target: input.target, path: input.path };
    const resolved = this.resolveTarget(input.target, "read");
    if (!resolved.ok) {
      return this.failed(client, "file_stat", params, resolved.error);
    }
    const normalized = normalizeRemotePath(input.path);
    if (!normalized.ok) {
      return this.failed(
        client,
        "file_stat",
        params,
        normalized.error,
        resolved.data.connection.id
      );
    }

    const connectionId = resolved.data.connection.id;
    return this.execute(
      client,
      "file_stat",
      params,
      () => this.deps.statRemoteFile(connectionId, normalized.data),
      { connectionId }
    );
  }

  /**
   * The stat is an early-exit courtesy only — `st_size` lies for procfs/sysfs
   * and can change between the stat and the read, so the real ceiling is the
   * byte budget `readRemoteFile` enforces while streaming.
   */
  async readFile(
    client: AgentClientIdentity,
    input: { target: string; path: string; maxBytes?: number }
  ): Promise<AgentToolResult<AgentFileReadPayload>> {
    const params = { target: input.target, path: input.path };
    const resolved = this.resolveTarget(input.target, "read");
    if (!resolved.ok) {
      return this.failed(client, "file_read", params, resolved.error);
    }
    const normalized = normalizeRemotePath(input.path);
    if (!normalized.ok) {
      return this.failed(
        client,
        "file_read",
        params,
        normalized.error,
        resolved.data.connection.id
      );
    }

    const connectionId = resolved.data.connection.id;
    const budget = clampInt(input.maxBytes, this.limits.maxFileBytes, 1, this.limits.maxFileBytes);
    return this.execute(
      client,
      "file_read",
      params,
      async (signal) => {
        const stat = await this.deps.statRemoteFile(connectionId, normalized.data);
        if (stat.type === "directory") {
          throw new AgentToolFailure({
            code: "invalid_argument",
            message: "Path is a directory; use file_list instead"
          });
        }
        if (stat.type === "other") {
          throw new AgentToolFailure({
            code: "invalid_argument",
            message: "Path is not a regular file (device, socket or fifo)"
          });
        }
        if (stat.size > this.limits.maxFileBytes) {
          throw new AgentToolFailure({
            code: "too_large",
            message: `File is ${stat.size} bytes; the agent read limit is ${this.limits.maxFileBytes} bytes`
          });
        }

        const chunk = await this.deps.readRemoteFile(
          connectionId,
          normalized.data,
          budget,
          signal
        );
        const bytes = chunk.bytes.subarray(0, budget);
        const truncated = chunk.truncated || chunk.bytes.byteLength > budget;
        const binary = looksBinary(bytes);
        return {
          path: normalized.data,
          encoding: binary ? ("base64" as const) : ("utf-8" as const),
          content: binary ? bytes.toString("base64") : bytes.toString("utf8"),
          bytes: bytes.byteLength,
          size: stat.size,
          truncated
        };
      },
      { connectionId }
    );
  }

  async monitorSnapshot(
    client: AgentClientIdentity,
    input: { target: string }
  ): Promise<AgentToolResult<MonitorSnapshot>> {
    const params = { target: input.target };
    const resolved = this.resolveTarget(input.target, "read");
    if (!resolved.ok) {
      return this.failed(client, "monitor_snapshot", params, resolved.error);
    }
    const connectionId = resolved.data.connection.id;
    return this.execute(
      client,
      "monitor_snapshot",
      params,
      async () => {
        const snapshot = await this.deps.getMonitorSnapshot(connectionId);
        if (!snapshot) {
          throw new AgentToolFailure({
            code: "unavailable",
            message: "No monitor snapshot is available for this host yet"
          });
        }
        return snapshot;
      },
      { connectionId }
    );
  }

  /**
   * Only the saved command library — the entries the user deliberately curated
   * in-app. The global shell history is deliberately unreachable: it has no
   * connection id (see `CommandHistoryEntry`), so commands typed against
   * unauthorized hosts and local shells cannot be told apart from authorized
   * ones, and its lines regularly carry inline credentials. Entries naming an
   * unauthorized host are still dropped and every line is redacted.
   */
  async searchCommands(
    client: AgentClientIdentity,
    input: { query?: string; limit?: number }
  ): Promise<AgentToolResult<AgentCommandSearchPayload>> {
    const limit = clampInt(input.limit, 30, 1, this.limits.maxListItems);
    const query = input.query?.trim().toLowerCase() ?? "";
    return this.execute(client, "command_search", { query, limit }, async () => {
      const forbiddenTokens = this.deps
        .listConnections()
        .filter((connection) => !isAgentVisible(connection))
        .flatMap((connection) => [connection.host, connection.name])
        .map((token) => token.trim().toLowerCase())
        .filter((token) => token.length >= 3);

      const mentionsForbiddenHost = (command: string): boolean => {
        const lowered = command.toLowerCase();
        return forbiddenTokens.some((token) => lowered.includes(token));
      };

      const matches: AgentCommandMatch[] = [];
      for (const item of this.deps.listSavedCommands(query ? { keyword: query } : {})) {
        if (mentionsForbiddenHost(item.command)) {
          continue;
        }
        matches.push({
          command: redactText(item.command),
          source: "library",
          name: item.name,
          group: item.group,
          lastUsedAt: item.updatedAt
        });
      }

      const capped = matches.slice(0, limit);
      return {
        matches: capped,
        total: matches.length,
        truncated: capped.length < matches.length,
        source: "library" as const
      };
    });
  }

  async execCommand(
    client: AgentClientIdentity,
    input: { target: string; command: string; cwd?: string; timeoutSec?: number }
  ): Promise<AgentToolResult<AgentExecPayload>> {
    const command = input.command.trim();
    const params = { target: input.target, command, cwd: input.cwd, timeoutSec: input.timeoutSec };
    if (!command) {
      return this.failed(client, "exec", params, {
        code: "invalid_argument",
        message: "command must not be empty"
      });
    }
    const risk = classifyCommandRisk(command);
    const requiresWriteAccess = risk.level !== "readonly" || risk.hasSudo;
    const resolved = this.resolveTarget(input.target, requiresWriteAccess ? "write" : "read");
    if (!resolved.ok) {
      return this.failed(client, "exec", params, resolved.error);
    }

    let requestedCwd: string | undefined;
    if (input.cwd !== undefined) {
      const normalized = normalizeRemotePath(input.cwd);
      if (!normalized.ok) {
        return this.failed(client, "exec", params, normalized.error, resolved.data.connection.id);
      }
      requestedCwd = normalized.data;
    } else if (resolved.data.sessionId) {
      requestedCwd = this.authorizedSessions().find(
        (session) => session.id === resolved.data.sessionId
      )?.cwd ?? undefined;
    }

    const connection = resolved.data.connection;
    if (!this.safeOnline(connection.id) && !connection.hostFingerprint?.trim()) {
      return this.failed(client, "exec", params, {
        code: "unavailable",
        message:
          "This host has no pinned host key. Open it in NextShell once so the user can establish trust before an agent connects."
      }, connection.id);
    }

    const approvalKey = `${connection.id}\0${command}`;
    const preferences = this.deps.getPreferences().agent;
    /**
     * Raised inside `execute`'s preflight, not here: the dialog is only reached
     * after the client has paid a call-budget slot and passed the approval gate,
     * so a runaway client cannot spend the user's attention for free.
     */
    const confirmCommand = async (): Promise<void> => {
      const remembered = this.rememberedApprovals.get(client.id)?.has(approvalKey) ?? false;
      const needsConfirmation =
        !remembered &&
        (risk.level === "dangerous" ||
          risk.hasSudo ||
          (risk.level === "unknown" && preferences.confirmUnknownCommands));
      if (!needsConfirmation) return;
      const response = await this.deps.promptUser({
        kind: "confirm",
        title: risk.level === "dangerous" ? "Agent 请求执行危险命令" : "Agent 请求执行命令",
        message: `${client.name ?? "未知客户端"} 请求在 ${connection.name} 上执行：`,
        details: `${command}${requestedCwd ? `\n\n工作目录：${requestedCwd}` : ""}\n\n风险判定：${risk.reason}`,
        allowRemember: true
      });
      if (response.canceled || response.value !== "approved") {
        throw new AgentToolFailure({
          code: "forbidden",
          message: "The user denied the command"
        });
      }
      if (response.rememberForSession) {
        const approvals = this.rememberedApprovals.get(client.id) ?? new Set<string>();
        approvals.add(approvalKey);
        this.rememberedApprovals.set(client.id, approvals);
      }
    };

    const requestedTimeoutMs =
      input.timeoutSec === undefined
        ? undefined
        : clampInt(input.timeoutSec, preferences.execTimeoutSec, 1, 3600) * 1000;
    return this.execute(
      client,
      "exec",
      params,
      async (signal) => {
        const release = this.deps.retainConnection(connection.id);
        try {
          const execute = (authOverride?: SessionAuthOverrideInput) =>
            this.deps.execCommand(connection.id, command, {
              ...(requestedCwd ? { cwd: requestedCwd } : {}),
              signal,
              ...(authOverride ? { authOverride } : {})
            });
          let result;
          try {
            result = await execute();
          } catch (error) {
            const reason = error instanceof Error ? error.message.toLowerCase() : String(error);
            const canRetryInteractive =
              (connection.authType === "password" || connection.authType === "interactive") &&
              /auth|password|permission denied|userauth/.test(reason);
            if (!canRetryInteractive) throw error;
            const response = await this.deps.promptUser({
              kind: "text",
              title: "SSH 需要交互认证",
              message: `请输入 ${connection.name} 的一次性验证码或密码。该值不会保存，也不会返回给 Agent。`,
              sensitive: true,
              placeholder: "验证码或密码"
            });
            if (response.canceled || !response.value) {
              throw new AgentToolFailure({
                code: "forbidden",
                message: "The user canceled interactive authentication"
              });
            }
            const interactiveAuthType: "password" | "interactive" =
              connection.authType === "interactive" ? "interactive" : "password";
            result = await execute({
              username: connection.username,
              authType: interactiveAuthType,
              password: response.value
            });
          }
          return {
            connectionId: connection.id,
            command: redactText(command),
            stdout: redactText(result.stdout),
            stderr: redactText(result.stderr),
            exitCode: result.exitCode,
            actualCwd: result.cwd ?? requestedCwd ?? null,
            executedAt: result.executedAt,
            risk
          };
        } finally {
          release();
          await this.deps.closeConnectionIfIdle(connection.id).catch(() => undefined);
        }
      },
      { connectionId: connection.id, requestedTimeoutMs, preflight: confirmCommand }
    );
  }

  // ─── Tier 2: remote mutation ──────────────────────────────────────────────

  /**
   * Shared confirmation for every host-mutating tool. Gated by
   * `preferences.agent.confirmWrites` — turning it off is the documented
   * "brave mode" and is the user's call, not the agent's.
   */
  private confirmWrite(
    client: AgentClientIdentity,
    connection: ConnectionProfile,
    title: string,
    details: string
  ): () => Promise<void> {
    const approvalKey = `${connection.id}\0${title}\0${details}`;
    return async () => {
      if (!this.deps.getPreferences().agent.confirmWrites) return;
      if (this.rememberedApprovals.get(client.id)?.has(approvalKey)) return;
      const response = await this.deps.promptUser({
        kind: "confirm",
        title,
        message: `${client.name ?? "未知客户端"} 请求修改 ${connection.name}：`,
        details,
        allowRemember: true
      });
      if (response.canceled || response.value !== "approved") {
        throw new AgentToolFailure({
          code: "forbidden",
          message: "The user denied this write operation"
        });
      }
      if (response.rememberForSession) {
        const approvals = this.rememberedApprovals.get(client.id) ?? new Set<string>();
        approvals.add(approvalKey);
        this.rememberedApprovals.set(client.id, approvals);
      }
    };
  }

  /** Resolves the target for a write and normalizes one remote path in one step. */
  private prepareWrite(
    client: AgentClientIdentity,
    tool: string,
    params: Record<string, unknown>,
    target: string,
    remotePath: string
  ):
    | { ok: true; connection: ConnectionProfile; path: string }
    | { ok: false; failure: AgentToolResult<never> } {
    const resolved = this.resolveTarget(target, "write");
    if (!resolved.ok) {
      return { ok: false, failure: this.failed(client, tool, params, resolved.error) };
    }
    const normalized = normalizeRemotePath(remotePath);
    if (!normalized.ok) {
      return {
        ok: false,
        failure: this.failed(
          client,
          tool,
          params,
          normalized.error,
          resolved.data.connection.id
        )
      };
    }
    return { ok: true, connection: resolved.data.connection, path: normalized.data };
  }

  async writeFile(
    client: AgentClientIdentity,
    input: { target: string; path: string; content: string; encoding?: "utf-8" | "base64" }
  ): Promise<AgentToolResult<AgentWritePayload>> {
    const params = { target: input.target, path: input.path, encoding: input.encoding ?? "utf-8" };
    const prepared = this.prepareWrite(client, "file_write", params, input.target, input.path);
    if (!prepared.ok) return prepared.failure;

    let content: Buffer;
    try {
      content = Buffer.from(input.content, input.encoding === "base64" ? "base64" : "utf8");
    } catch {
      return this.failed(client, "file_write", params, {
        code: "invalid_argument",
        message: "content is not valid for the requested encoding"
      });
    }
    if (content.byteLength > this.limits.maxWriteBytes) {
      return this.failed(
        client,
        "file_write",
        params,
        {
          code: "too_large",
          message: `Inline writes are limited to ${this.limits.maxWriteBytes} bytes; use transfer_upload for anything larger`
        },
        prepared.connection.id
      );
    }

    const { connection, path: remotePath } = prepared;
    return this.execute(
      client,
      "file_write",
      params,
      async () => {
        await this.deps.writeRemoteFile(connection.id, remotePath, content);
        return { path: remotePath, bytes: content.byteLength };
      },
      {
        connectionId: connection.id,
        preflight: this.confirmWrite(
          client,
          connection,
          "Agent 请求写入远端文件",
          `写入 ${remotePath}\n大小：${content.byteLength} 字节`
        )
      }
    );
  }

  async makeDirectory(
    client: AgentClientIdentity,
    input: { target: string; path: string }
  ): Promise<AgentToolResult<AgentMutationPayload>> {
    const params = { target: input.target, path: input.path };
    const prepared = this.prepareWrite(client, "file_mkdir", params, input.target, input.path);
    if (!prepared.ok) return prepared.failure;

    const { connection, path: remotePath } = prepared;
    return this.execute(
      client,
      "file_mkdir",
      params,
      async () => {
        await this.deps.makeRemoteDirectory(connection.id, remotePath);
        return { path: remotePath };
      },
      {
        connectionId: connection.id,
        preflight: this.confirmWrite(
          client,
          connection,
          "Agent 请求创建远端目录",
          `创建目录 ${remotePath}（含缺失的父级）`
        )
      }
    );
  }

  async renamePath(
    client: AgentClientIdentity,
    input: { target: string; from: string; to: string }
  ): Promise<AgentToolResult<AgentRenamePayload>> {
    const params = { target: input.target, from: input.from, to: input.to };
    const prepared = this.prepareWrite(client, "file_rename", params, input.target, input.from);
    if (!prepared.ok) return prepared.failure;
    const destination = normalizeRemotePath(input.to);
    if (!destination.ok) {
      return this.failed(
        client,
        "file_rename",
        params,
        destination.error,
        prepared.connection.id
      );
    }

    const { connection, path: fromPath } = prepared;
    const toPath = destination.data;
    return this.execute(
      client,
      "file_rename",
      params,
      async () => {
        await this.deps.renameRemotePath(connection.id, fromPath, toPath);
        return { from: fromPath, to: toPath };
      },
      {
        connectionId: connection.id,
        preflight: this.confirmWrite(
          client,
          connection,
          "Agent 请求重命名远端路径",
          `${fromPath}\n→ ${toPath}`
        )
      }
    );
  }

  /**
   * Unlike the other writes, deletion always asks. `confirmWrites` is a
   * convenience switch for the operations a mistake can be undone from; an
   * `rm -rf` over SFTP is not one of them.
   */
  async deletePath(
    client: AgentClientIdentity,
    input: { target: string; path: string; type: "file" | "directory" | "link" }
  ): Promise<AgentToolResult<AgentMutationPayload>> {
    const params = { target: input.target, path: input.path, type: input.type };
    const prepared = this.prepareWrite(client, "file_delete", params, input.target, input.path);
    if (!prepared.ok) return prepared.failure;

    const { connection, path: remotePath } = prepared;
    if (remotePath === "/") {
      return this.failed(
        client,
        "file_delete",
        params,
        { code: "forbidden", message: "Refusing to delete the filesystem root" },
        connection.id
      );
    }

    return this.execute(
      client,
      "file_delete",
      params,
      async () => {
        await this.deps.deleteRemotePath(connection.id, remotePath, input.type);
        return { path: remotePath };
      },
      {
        connectionId: connection.id,
        preflight: async () => {
          const response = await this.deps.promptUser({
            kind: "confirm",
            title: "Agent 请求删除远端路径",
            message: `${client.name ?? "未知客户端"} 请求在 ${connection.name} 上删除：`,
            details:
              input.type === "directory"
                ? `${remotePath}\n\n这是一个目录，其中的全部内容都会被递归删除，且不可恢复。`
                : `${remotePath}\n\n删除后不可恢复。`
          });
          if (response.canceled || response.value !== "approved") {
            throw new AgentToolFailure({
              code: "forbidden",
              message: "The user denied the deletion"
            });
          }
        }
      }
    );
  }

  // ─── Tier 2: transfers ────────────────────────────────────────────────────

  /**
   * The single choke point for agent-supplied paths on this machine. Denials
   * are reported to the agent with the human-readable reason on purpose: the
   * agent needs to understand it hit a policy wall rather than a missing file,
   * and the reason names only the rule, never the contents.
   */
  private checkLocalPath(
    localPath: string,
    intent: LocalPathIntent
  ): { ok: true; resolved: string } | { ok: false; error: AgentToolError } {
    const decision = evaluateLocalPath(localPath, intent, this.deps.localPathContext());
    if (!decision.allowed) {
      return { ok: false, error: { code: "forbidden", message: decision.reason } };
    }
    return { ok: true, resolved: decision.resolved };
  }

  private transferBudgetError(client: AgentClientIdentity): AgentToolError | null {
    if (this.deps.runningTransferCount(client.id) < this.limits.maxConcurrentTransfers) {
      return null;
    }
    return {
      code: "busy",
      message: `At most ${this.limits.maxConcurrentTransfers} transfers may run at once; poll transfer_status and retry`
    };
  }

  async uploadTransfer(
    client: AgentClientIdentity,
    input: { target: string; localPath: string; remotePath: string }
  ): Promise<AgentToolResult<AgentTransferSnapshot>> {
    const params = {
      target: input.target,
      localPath: input.localPath,
      remotePath: input.remotePath
    };
    const prepared = this.prepareWrite(
      client,
      "transfer_upload",
      params,
      input.target,
      input.remotePath
    );
    if (!prepared.ok) return prepared.failure;
    const { connection, path: remotePath } = prepared;

    const local = this.checkLocalPath(input.localPath, "read");
    if (!local.ok) {
      return this.failed(client, "transfer_upload", params, local.error, connection.id);
    }
    const stat = this.deps.statLocalPath(local.resolved);
    if (!stat) {
      return this.failed(
        client,
        "transfer_upload",
        params,
        { code: "not_found", message: "The local path does not exist" },
        connection.id
      );
    }
    if (stat.type === "other") {
      return this.failed(
        client,
        "transfer_upload",
        params,
        {
          code: "invalid_argument",
          message: "The local path is neither a regular file nor a directory"
        },
        connection.id
      );
    }
    const budgetError = this.transferBudgetError(client);
    if (budgetError) {
      return this.failed(client, "transfer_upload", params, budgetError, connection.id);
    }

    const packed = stat.type === "directory";
    return this.execute(
      client,
      "transfer_upload",
      { ...params, resolvedLocalPath: local.resolved, packed },
      async () => {
        // A single file addressed at an existing remote directory lands *in* it,
        // the way `scp` behaves. Resolved here rather than made the agent's
        // problem: `remotePath: "/opt/app"` is what an agent naturally writes,
        // and the alternative is an opaque SFTP failure. A packed upload always
        // takes a directory, so it needs no adjustment.
        let destination = remotePath;
        if (!packed) {
          const remoteStat = await this.deps
            .statRemoteFile(connection.id, remotePath)
            .catch(() => null);
          if (remoteStat?.type === "directory") {
            destination = `${remotePath === "/" ? "" : remotePath}/${basename(local.resolved)}`;
          }
        }
        return this.deps.startUpload({
          clientId: client.id,
          connectionId: connection.id,
          localPath: local.resolved,
          remotePath: destination,
          packed
        });
      },
      {
        connectionId: connection.id,
        preflight: this.confirmTransfer(client, {
          title: "Agent 请求上传本机文件",
          connectionName: connection.name,
          // The full local path is the last human check against exfiltration,
          // so it is shown verbatim and never elided.
          details: [
            `本机路径：${local.resolved}`,
            packed ? "类型：目录（将打包为 tar.gz 后在远端解包）" : `大小：${stat.size} 字节`,
            `目标主机：${connection.name}`,
            `远端路径：${remotePath}`
          ].join("\n"),
          approvalKey: `upload\0${local.resolved}\0${connection.id}\0${remotePath}`
        })
      }
    );
  }

  async downloadTransfer(
    client: AgentClientIdentity,
    input: { target: string; remotePath: string; localPath: string }
  ): Promise<AgentToolResult<AgentTransferSnapshot>> {
    const params = {
      target: input.target,
      remotePath: input.remotePath,
      localPath: input.localPath
    };
    // Reading the host is a read; the risk lives on this side of the wire.
    const resolved = this.resolveTarget(input.target, "read");
    if (!resolved.ok) {
      return this.failed(client, "transfer_download", params, resolved.error);
    }
    const connection = resolved.data.connection;
    const normalized = normalizeRemotePath(input.remotePath);
    if (!normalized.ok) {
      return this.failed(client, "transfer_download", params, normalized.error, connection.id);
    }

    const local = this.checkLocalPath(input.localPath, "write");
    if (!local.ok) {
      return this.failed(client, "transfer_download", params, local.error, connection.id);
    }
    if (this.deps.statLocalPath(local.resolved)?.type === "directory") {
      return this.failed(
        client,
        "transfer_download",
        params,
        {
          code: "invalid_argument",
          message: "localPath must be the destination file path, not an existing directory"
        },
        connection.id
      );
    }
    const budgetError = this.transferBudgetError(client);
    if (budgetError) {
      return this.failed(client, "transfer_download", params, budgetError, connection.id);
    }

    const remotePath = normalized.data;
    return this.execute(
      client,
      "transfer_download",
      { ...params, resolvedLocalPath: local.resolved },
      async () =>
        this.deps.startDownload({
          clientId: client.id,
          connectionId: connection.id,
          remotePath,
          localPath: local.resolved
        }),
      {
        connectionId: connection.id,
        preflight: this.confirmTransfer(client, {
          title: "Agent 请求下载到本机",
          connectionName: connection.name,
          details: [
            `来源主机：${connection.name}`,
            `远端路径：${remotePath}`,
            `写入本机路径：${local.resolved}`,
            "若该文件已存在，将被覆盖。"
          ].join("\n"),
          approvalKey: `download\0${connection.id}\0${remotePath}\0${local.resolved}`
        })
      }
    );
  }

  /**
   * Transfers always ask, regardless of `confirmWrites`: the local path is the
   * one thing no remote-side authorization can vouch for, and §7.3 makes the
   * dialog (with the full path) the last line of defence.
   */
  private confirmTransfer(
    client: AgentClientIdentity,
    input: { title: string; connectionName: string; details: string; approvalKey: string }
  ): () => Promise<void> {
    return async () => {
      if (this.rememberedApprovals.get(client.id)?.has(input.approvalKey)) return;
      const response = await this.deps.promptUser({
        kind: "confirm",
        title: input.title,
        message: `${client.name ?? "未知客户端"} 请求传输文件：`,
        details: input.details,
        allowRemember: true
      });
      if (response.canceled || response.value !== "approved") {
        throw new AgentToolFailure({
          code: "forbidden",
          message: "The user denied the transfer"
        });
      }
      if (response.rememberForSession) {
        const approvals = this.rememberedApprovals.get(client.id) ?? new Set<string>();
        approvals.add(input.approvalKey);
        this.rememberedApprovals.set(client.id, approvals);
      }
    };
  }

  async transferStatus(
    client: AgentClientIdentity,
    input: { taskId: string }
  ): Promise<AgentToolResult<AgentTransferSnapshot>> {
    const params = { taskId: input.taskId };
    return this.execute(client, "transfer_status", params, async () => {
      const snapshot = this.deps.getTransfer(input.taskId, client.id);
      if (!snapshot) {
        throw new AgentToolFailure({
          code: "not_found",
          message: "No transfer with that id belongs to this client"
        });
      }
      return snapshot;
    });
  }

  async cancelTransfer(
    client: AgentClientIdentity,
    input: { taskId: string }
  ): Promise<AgentToolResult<{ taskId: string; cancelRequested: boolean }>> {
    const params = { taskId: input.taskId };
    return this.execute(client, "transfer_cancel", params, async () => {
      const snapshot = this.deps.getTransfer(input.taskId, client.id);
      if (!snapshot) {
        throw new AgentToolFailure({
          code: "not_found",
          message: "No transfer with that id belongs to this client"
        });
      }
      return {
        taskId: input.taskId,
        cancelRequested: this.deps.cancelTransfer(input.taskId)
      };
    });
  }

  // ─── Tier 3: session control and takeover ─────────────────────────────────

  /** Control characters, named rather than raw so the agent cannot smuggle bytes. */
  private static readonly CONTROL_BYTES: Record<
    AgentControlSignal,
    { byte: string; label: string }
  > = {
    interrupt: { byte: "\u0003", label: "Ctrl-C（中断当前前台进程）" },
    eof: { byte: "\u0004", label: "Ctrl-D（发送 EOF，可能会结束 shell）" },
    suspend: { byte: "\u001a", label: "Ctrl-Z（挂起当前前台进程）" },
    quit: { byte: "\u001c", label: "Ctrl-\\（退出并转储核心）" }
  };


  /**
   * Resolves a session id for a takeover tool: the session must exist, be live,
   * be on a host granted `full` access, and not be one a human is typing into
   * right now.
   */
  private resolveLiveSession(
    sessionId: string
  ):
    | { ok: true; session: AgentSessionInfo; connection: ConnectionProfile }
    | { ok: false; error: AgentToolError } {
    const session = this.authorizedSessions().find((candidate) => candidate.id === sessionId);
    if (!session || !session.connectionId) {
      return {
        ok: false,
        error: {
          code: "not_found",
          message: "No active authorized session matches that id; call session_list first"
        }
      };
    }
    if (session.status !== "connected") {
      return {
        ok: false,
        error: { code: "unavailable", message: `Session is ${session.status}, not connected` }
      };
    }
    const resolved = this.resolveTarget(session.connectionId, "write");
    if (!resolved.ok) {
      return { ok: false, error: resolved.error };
    }
    return { ok: true, session, connection: resolved.data.connection };
  }

  /**
   * The person at the keyboard wins. Injecting into a PTY someone is actively
   * typing in interleaves two input streams into one line editor, which at best
   * garbles the command and at worst runs a spliced one.
   */
  /** Forgets sessions the user closed behind the agent's back. */
  private pruneOwnedSessions(): void {
    if (this.agentOwnedSessions.size === 0) return;
    const live = new Set(this.deps.listSessions().map((session) => session.id));
    for (const sessionId of this.agentOwnedSessions) {
      if (!live.has(sessionId)) this.agentOwnedSessions.delete(sessionId);
    }
  }

  private userIsTyping(sessionId: string): boolean {
    const last = this.deps.lastUserInputAt(sessionId);
    if (last === null) return false;
    return this.now() - last < this.limits.userInputPreemptionMs;
  }

  /**
   * Injection into a live PTY always asks, regardless of `confirmWrites`. It is
   * the one tool that runs inside the user's own shell — inheriting their cwd,
   * their sudo timestamp, their virtualenv — so the command-risk classifier
   * cannot bound what it does, and `exec` exists precisely so this stays rare.
   */
  async sendKeys(
    client: AgentClientIdentity,
    input: {
      target: string;
      text: string;
      submit?: boolean;
      waitForPrompt?: boolean;
      timeoutSec?: number;
    }
  ): Promise<AgentToolResult<AgentSendKeysPayload>> {
    const params = {
      target: input.target,
      command: input.text,
      submit: input.submit ?? false,
      waitForPrompt: input.waitForPrompt ?? false
    };
    if (input.text.length === 0 && !input.submit) {
      return this.failed(client, "session_send_keys", params, {
        code: "invalid_argument",
        message: "text must not be empty unless submit is true"
      });
    }
    const live = this.resolveLiveSession(input.target);
    if (!live.ok) {
      return this.failed(client, "session_send_keys", params, live.error);
    }
    if (this.userIsTyping(live.session.id)) {
      return this.failed(
        client,
        "session_send_keys",
        params,
        {
          code: "busy",
          message: "The user is typing in this session right now; retry shortly or use exec"
        },
        live.connection.id
      );
    }

    const { session, connection } = live;
    const risk = classifyCommandRisk(input.text);
    const payload = input.submit ? `${input.text}\r` : input.text;
    // Kept strictly under the call ceiling: if the wait could outlive the call,
    // a slow command would surface as a `timeout` error instead of the honest
    // `waitTimedOut: true` the tool promises.
    const waitMs = Math.min(
      clampInt(input.timeoutSec, 30, 1, 3600) * 1000,
      this.limits.maxWaitForPromptMs,
      Math.max(1_000, this.limits.maxCallTimeoutMs - WAIT_FOR_PROMPT_HEADROOM_MS)
    );
    const approvalKey = `send_keys\0${session.id}\0${payload}`;

    return this.execute(
      client,
      "session_send_keys",
      params,
      async () => {
        // Subscribed before the write so a fast command cannot complete in the
        // gap between injecting and starting to listen.
        const completion = input.waitForPrompt
          ? this.deps.waitForCommandCompletion(session.id, waitMs)
          : null;
        // A session the agent owns is badged for its whole life; a session the
        // user owns only while the agent is actually driving it.
        const owned = this.agentOwnedSessions.has(session.id);
        if (!owned) this.deps.setSessionAgentControlled(session.id, client.name);
        try {
          this.deps.writeSession(session.id, payload);
          const settled = completion ? await completion : null;
          return {
            sessionId: session.id,
            bytes: Buffer.byteLength(payload, "utf8"),
            submitted: input.submit ?? false,
            completed: settled
              ? {
                  command: settled.command === null ? null : redactText(settled.command),
                  exitCode: settled.exitCode,
                  output: redactText(settled.output),
                  truncated: settled.truncated
                }
              : null,
            waitTimedOut: Boolean(input.waitForPrompt) && settled === null
          };
        } finally {
          // In a finally: a badge that survives a failed write would tell the
          // user an agent is still driving a terminal it never reached.
          if (!owned) this.deps.clearSessionAgentControlled(session.id);
        }
      },
      {
        connectionId: connection.id,
        // The wait is the point of the call, so it gets the whole budget.
        requestedTimeoutMs: input.waitForPrompt
          ? waitMs + WAIT_FOR_PROMPT_HEADROOM_MS
          : undefined,
        preflight: async () => {
          if (this.rememberedApprovals.get(client.id)?.has(approvalKey)) return;
          const response = await this.deps.promptUser({
            kind: "confirm",
            title: "Agent 请求向终端注入输入",
            message: `${client.name ?? "未知客户端"} 请求向 ${connection.name} 的会话「${session.title}」注入输入：`,
            details: [
              describeInjectedKeys(input.text),
              input.submit ? "并按下回车执行" : "不按回车（仅输入）",
              `风险判定：${risk.reason}`,
              "该输入会在你自己的 shell 里执行，继承当前目录、环境与 sudo 状态。"
            ].join("\n"),
            allowRemember: true
          });
          if (response.canceled || response.value !== "approved") {
            throw new AgentToolFailure({
              code: "forbidden",
              message: "The user denied the injection"
            });
          }
          if (response.rememberForSession) {
            const approvals = this.rememberedApprovals.get(client.id) ?? new Set<string>();
            approvals.add(approvalKey);
            this.rememberedApprovals.set(client.id, approvals);
          }
        }
      }
    );
  }

  async sendSignal(
    client: AgentClientIdentity,
    input: { target: string; signal: AgentControlSignal }
  ): Promise<AgentToolResult<{ sessionId: string; signal: AgentControlSignal }>> {
    const params = { target: input.target, signal: input.signal };
    const control = AgentGateway.CONTROL_BYTES[input.signal];
    if (!control) {
      return this.failed(client, "session_send_signal", params, {
        code: "invalid_argument",
        message: "signal must be one of interrupt, eof, suspend, quit"
      });
    }
    const live = this.resolveLiveSession(input.target);
    if (!live.ok) {
      return this.failed(client, "session_send_signal", params, live.error);
    }

    const { session, connection } = live;
    return this.execute(
      client,
      "session_send_signal",
      params,
      async () => {
        this.deps.writeSession(session.id, control.byte);
        return { sessionId: session.id, signal: input.signal };
      },
      {
        connectionId: connection.id,
        preflight: async () => {
          // Interrupting is how a human stops a runaway agent command, so it is
          // the one control byte that does not need its own dialog.
          if (input.signal === "interrupt") return;
          const response = await this.deps.promptUser({
            kind: "confirm",
            title: "Agent 请求向终端发送控制信号",
            message: `${client.name ?? "未知客户端"} 请求向会话「${session.title}」发送：`,
            details: control.label
          });
          if (response.canceled || response.value !== "approved") {
            throw new AgentToolFailure({
              code: "forbidden",
              message: "The user denied the control signal"
            });
          }
        }
      }
    );
  }

  /** Opens a real, visible GUI tab — not a hidden channel the user cannot see. */
  async openSession(
    client: AgentClientIdentity,
    input: { target: string }
  ): Promise<AgentToolResult<AgentSessionOpenPayload>> {
    const params = { target: input.target };
    const resolved = this.resolveTarget(input.target, "write");
    if (!resolved.ok) {
      return this.failed(client, "session_open", params, resolved.error);
    }
    const connection = resolved.data.connection;
    if (!this.safeOnline(connection.id) && !connection.hostFingerprint?.trim()) {
      return this.failed(
        client,
        "session_open",
        params,
        {
          code: "unavailable",
          message:
            "This host has no pinned host key. Open it in NextShell once so the user can establish trust before an agent connects."
        },
        connection.id
      );
    }

    return this.execute(
      client,
      "session_open",
      params,
      async () => {
        const opened = await this.deps.openSession(connection.id);
        this.pruneOwnedSessions();
        this.agentOwnedSessions.add(opened.id);
        this.deps.setSessionAgentControlled(opened.id, client.name);
        return {
          sessionId: opened.id,
          connectionId: connection.id,
          title: opened.title,
          status: opened.status
        };
      },
      {
        connectionId: connection.id,
        preflight: async () => {
          const response = await this.deps.promptUser({
            kind: "confirm",
            title: "Agent 请求打开终端标签",
            message: `${client.name ?? "未知客户端"} 请求在 ${connection.name} 上打开一个新的终端会话。`,
            details: "该标签会真实出现在 NextShell 里并标记为 Agent 控制中，你随时可以关闭它。"
          });
          if (response.canceled || response.value !== "approved") {
            throw new AgentToolFailure({
              code: "forbidden",
              message: "The user denied opening a session"
            });
          }
        }
      }
    );
  }

  async closeSession(
    client: AgentClientIdentity,
    input: { target: string }
  ): Promise<AgentToolResult<{ sessionId: string }>> {
    const params = { target: input.target };
    const live = this.resolveLiveSession(input.target);
    if (!live.ok) {
      return this.failed(client, "session_close", params, live.error);
    }

    const { session, connection } = live;
    return this.execute(
      client,
      "session_close",
      params,
      async () => {
        await this.deps.closeSession(session.id);
        this.agentOwnedSessions.delete(session.id);
        this.deps.clearSessionAgentControlled(session.id);
        return { sessionId: session.id };
      },
      { connectionId: connection.id }
    );
  }

  /** Read-only for the host; it only moves the user's own window. */
  async focusSession(
    client: AgentClientIdentity,
    input: { target: string }
  ): Promise<AgentToolResult<{ sessionId: string }>> {
    const params = { target: input.target };
    const session = this.authorizedSessions().find((candidate) => candidate.id === input.target);
    if (!session) {
      return this.failed(client, "session_focus", params, {
        code: "not_found",
        message: "No active authorized session matches that id; call session_list first"
      });
    }
    return this.execute(
      client,
      "session_focus",
      params,
      async () => {
        this.deps.focusSession(session.id);
        return { sessionId: session.id };
      },
      { connectionId: session.connectionId ?? undefined }
    );
  }

  async askUser(
    client: AgentClientIdentity,
    input: {
      question: string;
      choices?: string[];
      allowText?: boolean;
      sensitive?: boolean;
    }
  ): Promise<AgentToolResult<{ canceled: boolean; answer: string | null }>> {
    const question = input.question.trim();
    if (!question) {
      return this.failed(client, "ask_user", {}, {
        code: "invalid_argument",
        message: "question must not be empty"
      });
    }
    return this.execute(client, "ask_user", { question, choices: input.choices }, async () => {
      const choices = input.choices?.map((choice) => choice.trim()).filter(Boolean);
      const response = await this.deps.promptUser({
        kind: choices && choices.length > 0 ? "select" : input.allowText ? "text" : "confirm",
        title: "Agent 向你提问",
        message: question,
        ...(choices && choices.length > 0 ? { choices } : {}),
        ...(input.sensitive ? { sensitive: true } : {})
      });
      return { canceled: response.canceled, answer: response.value ?? null };
    });
  }

  async notifyUser(
    client: AgentClientIdentity,
    input: { title: string; message: string }
  ): Promise<AgentToolResult<{ delivered: true }>> {
    const title = input.title.trim();
    const message = input.message.trim();
    if (!title || !message) {
      return this.failed(client, "notify_user", {}, {
        code: "invalid_argument",
        message: "title and message must not be empty"
      });
    }
    return this.execute(client, "notify_user", { title, message }, async () => {
      this.deps.notifyUser(title, message);
      return { delivered: true as const };
    });
  }
}
