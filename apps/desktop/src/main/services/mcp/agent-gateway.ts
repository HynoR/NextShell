import {
  pickRecentConnections,
  searchConnections,
  type AppPreferences,
  type ConnectionProfile,
  type SavedCommand,
  type ScopedCommandItem,
  type SessionStatus,
  type SessionType
} from "@nextshell/core";
import type {
  AgentActivityEvent,
  AgentOpenRequestEvent,
  AgentOpenRespondInput
} from "@nextshell/shared";
import { matchDangerousCommand, matchSensitiveFile } from "@nextshell/terminal";

import { AgentOpenBroker } from "./open-broker";
import type { ScreenReadOptions, ScreenReadResult } from "./screen-mirror";

// ─── Client identity ────────────────────────────────────────────────────────

/**
 * The endpoint listens on 127.0.0.1 with no token: any local process is a
 * trusted client, and the only identity is what `initialize` reports.
 */
export interface AgentClientIdentity {
  /** MCP session id. */
  id: string;
  name: string | null;
  version: string | null;
  transport: "http";
}

// ─── Result envelope ────────────────────────────────────────────────────────

export type AgentErrorCode =
  | "invalid_argument"
  | "not_found"
  | "forbidden"
  | "timeout"
  | "unavailable"
  | "human_intervention"
  | "consent_required"
  | "denied"
  | "internal";

export interface AgentToolError {
  code: AgentErrorCode;
  /**
   * Agent-facing text. Never carries a raw exception message, stack, host
   * connection string or key path — those are never exposed to the agent.
   */
  message: string;
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

export interface AgentSessionListEntry extends AgentSessionInfo {
  /** 连接名与主机地址，供 agent 向用户报告它在操作哪台服务器。 */
  connectionName: string | null;
  host: string | null;
}

export interface AgentSessionListPayload {
  sessions: AgentSessionListEntry[];
  truncated: boolean;
}

export interface AgentHostEntry {
  id: string;
  name: string;
  host: string;
  port: number;
  username: string;
  groupPath: string;
  tags: string[];
  lastConnectedAt: string | null;
  /** Tabs already open on this host; > 0 means session_list has it. */
  openSessions: number;
}

export interface AgentHostListPayload {
  mode: "recent" | "search";
  hosts: AgentHostEntry[];
  truncated: boolean;
}

export interface AgentOpenSessionPayload {
  status: "opened" | "pending";
  sessionId?: string;
  requestId?: string;
  reused?: boolean;
}

export interface AgentCommandMatch {
  id: string;
  command: string;
  name: string | null;
  group: string | null;
  appendCr: boolean;
  scope: "local" | "workspace";
  workspaceId?: string;
  workspaceName?: string;
  lastUsedAt: string | null;
}

export interface AgentCommandSearchPayload {
  matches: AgentCommandMatch[];
  total: number;
  truncated: boolean;
}

export interface AgentSaveCommandInput {
  id?: string;
  workspaceId?: string;
  name: string;
  group?: string;
  command: string;
  appendCr?: boolean;
}

export interface AgentCommandSavePayload {
  command: SavedCommand;
  workspaceId?: string;
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

export type AgentControlSignal = "interrupt" | "eof" | "suspend" | "quit";

export type AgentExecMode = AppPreferences["agent"]["execMode"];

export interface AgentExecResult {
  sessionId: string;
  /** How the user's NextShell ran it; the agent never chooses. */
  mode: AgentExecMode;
  command: string;
  /** `null` when unknown: foreground without shell integration, or the wait timed out. */
  exitCode: number | null;
  /** Foreground: what the tab showed until the prompt returned. Background: stdout. */
  output: string;
  /** Background only. */
  stderr: string | null;
  /** Foreground only: no OSC 133 `D` arrived in time; the command may still be running in the tab. */
  waitTimedOut: boolean;
  /** Background only. */
  actualCwd: string | null;
}

/** `pending`: the approval dialog is up in NextShell; call exec again with only `requestId`. */
export type AgentExecPayload = AgentExecResult | { status: "pending"; requestId: string };

/** Everything an approved exec needs, pinned when the dialog went up. */
interface PinnedExec {
  session: AgentSessionInfo;
  connectionId: string | undefined;
  command: string;
  mode: AgentExecMode;
  timeoutSec: number | undefined;
  backgroundTimeoutMs: number;
}

// ─── Dependencies ───────────────────────────────────────────────────────────

/**
 * Narrow view of the main-process services the gateway needs. Kept as an
 * interface (rather than `ServiceContainer`) so the agent surface cannot reach
 * anything it was not explicitly handed — in particular no credential store,
 * no connect path and no way to open sessions of its own.
 */
export interface AgentGatewayDeps {
  /** Read-only connection metadata for the session_list name/host join. */
  listConnections: () => ConnectionProfile[];
  listSessions: () => AgentSessionInfo[];
  /** Local + workspace quick commands (the Stage 6 model). */
  listScopedCommands: () => ScopedCommandItem[];
  /** Upsert semantics identical to the quick-command edit dialog. */
  saveCommand: (input: AgentSaveCommandInput) => SavedCommand;
  getSessionHistory: (sessionId: string) => {
    integrationAvailable: boolean;
    entries: AgentSessionHistoryEntry[];
  } | null;
  /** `null` when the session is not mirrored (ScreenMirror capacity exhausted). */
  readSessionScreen: (
    sessionId: string,
    options: ScreenReadOptions
  ) => Promise<ScreenReadResult | null>;
  writeSession: (sessionId: string, data: string) => void;
  /**
   * Unsubmitted text on the shell's input line: `""` when clean, `null` when
   * the session has no prompt mark to judge by (no shell integration).
   */
  pendingInput: (sessionId: string) => Promise<string | null>;
  /** Epoch millis of the last real keystroke in that session, or `null`; the fallback when `pendingInput` is `null`. */
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
  /** Brings the tab to the front and raises the window. */
  focusSession: (sessionId: string) => void;
  /** Tells the GUI which session an agent is driving, so the tab can be badged. */
  setSessionAgentControlled: (sessionId: string, clientName: string | null) => void;
  clearSessionAgentControlled: (sessionId: string) => void;
  /** Runs a command on an exec channel of the connection backing `connectionId`. */
  execCommand: (
    connectionId: string,
    command: string,
    options: { cwd?: string; signal: AbortSignal }
  ) => Promise<{
    stdout: string;
    stderr: string;
    exitCode: number;
    executedAt: string;
    cwd?: string;
  }>;
  retainConnection: (connectionId: string) => () => void;
  closeConnectionIfIdle: (connectionId: string) => Promise<void>;
  emitActivity: (event: AgentActivityEvent) => void;
  /** Raises the window and shows the `session_open` authorization dialog. */
  requestOpenSession: (request: AgentOpenRequestEvent) => void;
  getPreferences: () => AppPreferences;
  now?: () => number;
  /** Test seams: how long one session_open call waits, and the request's total life. */
  openWaitMs?: number;
  openRequestTimeoutMs?: number;
}

// ─── Helpers ────────────────────────────────────────────────────────────────

const AGENT_REDACTED = "«redacted»";
const ANSI_ESCAPE_PATTERN =
  /[][[\]()#;?]*(?:(?:(?:[a-zA-Z\d]*(?:;[-a-zA-Z\d\x2f#&.:=?%@~_]+)*)?)|(?:(?:\d{1,4}(?:[;:]\d{0,4})*)?[\dA-PR-TZcf-nq-uy=><~]))/g;

const MAX_LIST_ITEMS = 500;
/** Ceiling on how long a foreground `exec` waits for the prompt to return. */
const MAX_WAIT_FOR_PROMPT_MS = 120_000;
/** Recent-host list size when host_list gets no query. */
const RECENT_HOSTS = 10;
const MAX_HOST_SEARCH = 20;
/** One session_open call waits this long before reporting `pending` (under the ~60s harness ceiling). */
const OPEN_WAIT_MS = 50_000;
/** Slack between a foreground wait deadline and the enclosing call timeout. */
const WAIT_FOR_PROMPT_HEADROOM_MS = 5_000;

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
  {
    re: /permission denied|eacces|eperm|access denied/i,
    code: "forbidden",
    message: "The remote host denied access"
  },
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
const normalizeRemotePath = (input: string): AgentToolResult<string> => {
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

// ─── Gateway ────────────────────────────────────────────────────────────────

/**
 * The single policy surface for MCP tool calls. Tools never touch the service
 * container directly: every call goes through `execute`, which applies the
 * kill switch, a timeout, output redaction and activity events.
 *
 * There are deliberately no approval dialogs, no rate limits and no per-host
 * concurrency budgets here. The endpoint is loopback-only, the agent only ever
 * borrows sessions the user already opened, and the preset-plus-user blacklist
 * is the only command filter.
 */
export class AgentGateway {
  private readonly deps: AgentGatewayDeps;
  /**
   * Fallback baseline for human-intervention detection on sessions without
   * shell integration: the moment of this agent's own last foreground `exec`.
   * Human keystrokes stamped later than this mean the person took the keyboard
   * back. Sessions with a prompt mark use the screen instead.
   */
  private readonly agentTouchedAt = new Map<string, number>();
  private activitySequence = 0;
  /**
   * The global breaker. Once tripped every tool call fails immediately, without
   * a dialog, until the user re-arms it — the point of a kill switch is that it
   * does not negotiate.
   */
  private halted = false;
  private readonly openBroker: AgentOpenBroker;
  /** exec requests awaiting the user's click, keyed by the broker's request id. */
  private readonly pendingExec = new Map<string, PinnedExec>();

  constructor(deps: AgentGatewayDeps) {
    this.deps = deps;
    this.openBroker = new AgentOpenBroker({
      send: (request) => deps.requestOpenSession(request),
      timeoutMs: deps.openRequestTimeoutMs,
      now: deps.now
    });
  }

  get isHalted(): boolean {
    return this.halted;
  }

  /** Trips or re-arms the breaker. */
  setHalted(halted: boolean): void {
    this.halted = halted;
  }

  /**
   * Every open tab is agent-visible by construction: the user opening the tab
   * is the grant. Local shells are included; they only lack an exec channel.
   */
  private openSessions(): AgentSessionInfo[] {
    return this.deps.listSessions();
  }

  // ─── Call plumbing ────────────────────────────────────────────────────────

  private now(): number {
    return this.deps.now?.() ?? Date.now();
  }

  private callTimeoutMs(): number {
    const seconds = this.deps.getPreferences().agent.execTimeoutSec;
    return clampInt(seconds, 60, 1, 3600) * 1000;
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
      // Activity rendering is best-effort; the call itself remains authoritative.
    }
  }

  /**
   * Kill switch → timeout → error sanitization → activity.
   *
   * The task is handed an `AbortSignal` that fires on timeout: without it a
   * timed-out call would only stop the caller waiting while the underlying
   * SSH work kept consuming memory in the background.
   */
  private async execute<T>(
    client: AgentClientIdentity,
    tool: string,
    params: Record<string, unknown>,
    task: (signal: AbortSignal) => Promise<T>,
    options: {
      connectionId?: string;
      requestedTimeoutMs?: number;
    } = {}
  ): Promise<AgentToolResult<T>> {
    const { connectionId, requestedTimeoutMs } = options;
    const activityId = `${client.id}:${++this.activitySequence}`;
    const modeTag =
      params.mode === "background" ? "[后台] " : params.mode === "foreground" ? "[前台] " : "";
    const commandSummary =
      typeof params.command === "string" ? `${modeTag}${redactText(params.command)}` : undefined;
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

    let timer: ReturnType<typeof setTimeout> | undefined;
    const controller = new AbortController();
    try {
      const timeoutMs = requestedTimeoutMs ?? this.callTimeoutMs();
      const timeout = new Promise<never>((_, reject) => {
        timer = setTimeout(() => {
          controller.abort();
          reject(new Error("ETIMEDOUT: agent tool call timed out"));
        }, timeoutMs);
      });
      const data = await Promise.race([task(controller.signal), timeout]);
      const settled = data as { exitCode?: unknown; waitTimedOut?: unknown } | null;
      const exitCode = typeof settled?.exitCode === "number" ? String(settled.exitCode) : null;
      // A foreground wait that ran out is not a completion: the panel must not
      // show "完成" for a command that is still running in the tab.
      const unsettled = settled?.waitTimedOut === true;
      const awaitingUser = (settled as { status?: unknown } | null)?.status === "pending";
      this.emitActivity(
        client,
        activityId,
        tool,
        unsettled || awaitingUser ? "unsettled" : "succeeded",
        connectionId,
        awaitingUser
          ? `${commandSummary ?? tool} → 等待用户授权`
          : unsettled
            ? `${commandSummary ?? tool} → 等待超时，仍在标签页中运行`
            : commandSummary && exitCode !== null
              ? `${commandSummary} → exit ${exitCode}`
              : "ok"
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
    }
  }

  /** Rejection before the work starts — a bad argument, an unknown session id. */
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
    const activityId = `${client.id}:${++this.activitySequence}`;
    this.emitActivity(client, activityId, tool, "failed", connectionId, error.code);
    return { ok: false, error };
  }

  // ─── Shared policy gates ──────────────────────────────────────────────────

  /**
   * The only command filter: the preset dangerous list plus the user's own
   * `agent.blacklist` entries (substring, or regular expression when the entry
   * parses as one). A hit is a hard error — there is no "allow once" path.
   */
  private blacklistHit(command: string): string | null {
    const preset = matchDangerousCommand(command);
    if (preset) return preset;
    for (const entry of this.deps.getPreferences().agent.blacklist) {
      const pattern = entry.trim();
      if (!pattern) continue;
      if (command.includes(pattern)) {
        return `matched the user blacklist: ${pattern}`;
      }
      try {
        if (new RegExp(pattern).test(command)) {
          return `matched the user blacklist: ${pattern}`;
        }
      } catch {
        // Not a valid regular expression; the substring check above already ran.
      }
    }
    return null;
  }

  /**
   * `.env` files need explicit user consent: without `allowSensitive` the call
   * fails with `consent_required` so the agent has to ask. The flag is
   * honour-system by design — the harness's own tool approval is where the
   * user actually says yes, and the flag shows up there as a parameter.
   */
  private consentRequired(
    command: string,
    allowSensitive: boolean | undefined
  ): AgentToolError | null {
    if (allowSensitive) return null;
    const file = matchSensitiveFile(command);
    if (!file) return null;
    return {
      code: "consent_required",
      message: `This command touches ${file}, which may hold secrets. Ask the user first; retry with allowSensitive: true only after they explicitly agree.`
    };
  }

  /**
   * Resolves a session id for a takeover tool: the session must exist and be
   * connected. `connectionId` is null for a local shell.
   */
  private resolveLiveSession(
    sessionId: string
  ):
    | { ok: true; session: AgentSessionInfo; connectionId: string | undefined }
    | { ok: false; error: AgentToolError } {
    const session = this.openSessions().find((candidate) => candidate.id === sessionId);
    if (!session) {
      this.agentTouchedAt.delete(sessionId);
      return {
        ok: false,
        error: {
          code: "not_found",
          message: "No active session matches that id; call session_list first"
        }
      };
    }
    if (session.status !== "connected") {
      return {
        ok: false,
        error: { code: "unavailable", message: `Session is ${session.status}, not connected` }
      };
    }
    return { ok: true, session, connectionId: session.connectionId ?? undefined };
  }

  /**
   * Human intervention is an error, not a pause: the agent is expected to stop
   * and report. The judge is the screen: text the user typed after the prompt
   * and has not submitted is theirs, and typing over it is the one thing a
   * foreground `exec` must never do. A clean line — or a running command the
   * agent may need to answer — passes. Without a prompt mark the keystroke
   * timestamp is the fallback: a keystroke since the agent's last call fails
   * this one, once, and this call becomes the new baseline.
   */
  private async checkHumanIntervention(sessionId: string): Promise<AgentToolError | null> {
    const pending = await this.deps.pendingInput(sessionId);
    if (pending !== null) {
      if (pending === "") return null;
      return {
        code: "human_intervention",
        message: `The user has unsubmitted text on the command line (${JSON.stringify(
          redactText(pending).slice(0, 40)
        )}); stop and report to the user instead of typing over it`
      };
    }
    const now = this.now();
    const baseline = this.agentTouchedAt.get(sessionId);
    this.agentTouchedAt.set(sessionId, now);
    if (baseline === undefined) {
      // First takeover establishes the baseline; nothing to compare against.
      return null;
    }
    const lastUser = this.deps.lastUserInputAt(sessionId);
    if (lastUser === null || lastUser <= baseline) {
      return null;
    }
    return {
      code: "human_intervention",
      message:
        "Detected manual keyboard input in this tab after the agent's last operation; stop and report to the user instead of continuing"
    };
  }

  // ─── Host discovery and opening ───────────────────────────────────────────

  /** Renderer answer to an open request; `false` when the request is gone. */
  respondOpen(response: AgentOpenRespondInput): boolean {
    return this.openBroker.respond(response);
  }

  dispose(): void {
    this.openBroker.dispose();
    this.pendingExec.clear();
  }

  async hostList(
    client: AgentClientIdentity,
    input: { query?: string }
  ): Promise<AgentToolResult<AgentHostListPayload>> {
    const query = input.query?.trim() ?? "";
    return this.execute(client, "host_list", { query }, async () => {
      const connections = this.deps.listConnections();
      const openByConnection = new Map<string, number>();
      for (const session of this.deps.listSessions()) {
        if (session.connectionId && session.status === "connected") {
          openByConnection.set(
            session.connectionId,
            (openByConnection.get(session.connectionId) ?? 0) + 1
          );
        }
      }
      const toEntry = (c: ConnectionProfile): AgentHostEntry => ({
        id: c.id,
        name: c.name,
        host: c.host,
        port: c.port,
        username: c.username,
        groupPath: c.groupPath,
        tags: [...c.tags],
        lastConnectedAt: c.lastConnectedAt ?? null,
        openSessions: openByConnection.get(c.id) ?? 0
      });
      if (!query) {
        return {
          mode: "recent",
          hosts: pickRecentConnections(connections, RECENT_HOSTS).map(toEntry),
          truncated: false
        };
      }
      const matches = searchConnections(connections, query, MAX_HOST_SEARCH + 1);
      return {
        mode: "search",
        hosts: matches.slice(0, MAX_HOST_SEARCH).map(toEntry),
        truncated: matches.length > MAX_HOST_SEARCH
      };
    });
  }

  /**
   * Opening a host is the one action that widens the agent's reach, so it is
   * the one action that asks the user in NextShell. A host that already has a
   * connected tab is reused without a dialog.
   */
  async openSession(
    client: AgentClientIdentity,
    input: { target?: string; reason?: string; requestId?: string }
  ): Promise<AgentToolResult<AgentOpenSessionPayload>> {
    const params = { target: input.target, reason: input.reason, requestId: input.requestId };
    const waitMs = this.deps.openWaitMs ?? OPEN_WAIT_MS;

    const settle = async (requestId: string): Promise<AgentOpenSessionPayload> => {
      const outcome = await this.openBroker.wait(requestId, waitMs);
      if (outcome === null) {
        throw new AgentToolFailure({
          code: "not_found",
          message:
            "No pending open request matches that requestId; it was answered, expired, or never existed"
        });
      }
      if (outcome === "pending") {
        return { status: "pending", requestId };
      }
      if (!outcome.approved) {
        throw new AgentToolFailure({
          code: "denied",
          message:
            "The user did not authorize opening this host in NextShell (declined or no answer within 5 minutes). Stop and ask the user to confirm there before retrying."
        });
      }
      if (!outcome.sessionId) {
        throw new AgentToolFailure({
          code: "unavailable",
          message: "The user authorized the connection but it failed to open; check with the user"
        });
      }
      return { status: "opened", sessionId: outcome.sessionId };
    };

    if (input.requestId) {
      const requestId = input.requestId;
      return this.execute(client, "session_open", params, () => settle(requestId), {
        requestedTimeoutMs: waitMs + WAIT_FOR_PROMPT_HEADROOM_MS
      });
    }

    if (!input.target) {
      return this.failed(client, "session_open", params, {
        code: "invalid_argument",
        message: "target (a connection id from host_list) or requestId is required"
      });
    }
    const connection = this.deps.listConnections().find((c) => c.id === input.target);
    if (!connection) {
      return this.failed(client, "session_open", params, {
        code: "not_found",
        message: "No saved connection matches that id; call host_list first"
      });
    }
    const existing = this.deps
      .listSessions()
      .find((s) => s.connectionId === connection.id && s.status === "connected");
    if (existing) {
      return this.execute(
        client,
        "session_open",
        params,
        async () => ({ status: "opened" as const, sessionId: existing.id, reused: true }),
        { connectionId: connection.id }
      );
    }
    return this.execute(
      client,
      "session_open",
      params,
      async () => {
        const requestId = this.openBroker.create({
          kind: "open",
          clientName: client.name,
          connectionId: connection.id,
          connectionName: connection.name,
          host: connection.host,
          reason: input.reason?.trim() ? input.reason.trim().slice(0, 300) : null
        });
        return settle(requestId);
      },
      { connectionId: connection.id, requestedTimeoutMs: waitMs + WAIT_FOR_PROMPT_HEADROOM_MS }
    );
  }

  // ─── Session discovery and reads ──────────────────────────────────────────

  async listSessions(
    client: AgentClientIdentity
  ): Promise<AgentToolResult<AgentSessionListPayload>> {
    return this.execute(client, "session_list", {}, async () => {
      const connectionsById = new Map(
        this.deps.listConnections().map((connection) => [connection.id, connection])
      );
      const sessions = this.openSessions().map<AgentSessionListEntry>((session) => {
        const connection = session.connectionId
          ? connectionsById.get(session.connectionId)
          : undefined;
        return {
          ...session,
          connectionName: connection?.name ?? null,
          host: connection?.host ?? null
        };
      });
      const capped = sessions.slice(0, MAX_LIST_ITEMS);
      return { sessions: capped, truncated: capped.length < sessions.length };
    });
  }

  async sessionHistory(
    client: AgentClientIdentity,
    input: { target: string; limit?: number; stripAnsi?: boolean }
  ): Promise<AgentToolResult<AgentSessionHistoryPayload>> {
    const params = { target: input.target, limit: input.limit, stripAnsi: input.stripAnsi };
    const session = this.openSessions().find((candidate) => candidate.id === input.target);
    if (!session) {
      return this.failed(client, "session_history", params, {
        code: "not_found",
        message: "No active session matches that id; call session_list first"
      });
    }
    const limit = clampInt(input.limit, 50, 1, MAX_LIST_ITEMS);
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
          output: redactText(
            input.stripAnsi ? entry.output.replace(ANSI_ESCAPE_PATTERN, "") : entry.output
          )
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
    const session = this.openSessions().find((candidate) => candidate.id === input.target);
    if (!session) {
      return this.failed(client, "session_read", params, {
        code: "not_found",
        message: "No active session matches that id; call session_list first"
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

  // ─── Quick command library ────────────────────────────────────────────────

  /**
   * The saved command library — the entries the user deliberately curated
   * in-app, local and workspace-scoped. Every command line is redacted before
   * it crosses the MCP boundary.
   */
  async searchCommands(
    client: AgentClientIdentity,
    input: { query?: string; limit?: number }
  ): Promise<AgentToolResult<AgentCommandSearchPayload>> {
    const limit = clampInt(input.limit, 30, 1, MAX_LIST_ITEMS);
    const query = input.query?.trim().toLowerCase() ?? "";
    return this.execute(client, "command_search", { query, limit }, async () => {
      const matches: AgentCommandMatch[] = [];
      for (const item of this.deps.listScopedCommands()) {
        if (
          query &&
          !item.command.toLowerCase().includes(query) &&
          !item.name.toLowerCase().includes(query)
        ) {
          continue;
        }
        matches.push({
          id: item.id,
          command: redactText(item.command),
          name: item.name,
          group: item.group,
          appendCr: item.appendCr ?? false,
          scope: item.scope,
          ...(item.workspaceId ? { workspaceId: item.workspaceId } : {}),
          ...(item.workspaceName ? { workspaceName: item.workspaceName } : {}),
          lastUsedAt: item.updatedAt
        });
      }

      const capped = matches.slice(0, limit);
      return {
        matches: capped,
        total: matches.length,
        truncated: capped.length < matches.length
      };
    });
  }

  /** Saves or updates a quick command, exactly like the in-app edit dialog. */
  async saveCommand(
    client: AgentClientIdentity,
    input: AgentSaveCommandInput
  ): Promise<AgentToolResult<AgentCommandSavePayload>> {
    const params = {
      name: input.name,
      group: input.group,
      command: input.command,
      workspaceId: input.workspaceId
    };
    if (!input.name.trim() || !input.command.trim()) {
      return this.failed(client, "command_save", params, {
        code: "invalid_argument",
        message: "name and command must not be empty"
      });
    }
    return this.execute(client, "command_save", params, async () => {
      const saved = this.deps.saveCommand({
        ...(input.id ? { id: input.id } : {}),
        ...(input.workspaceId ? { workspaceId: input.workspaceId } : {}),
        name: input.name.trim(),
        ...(input.group?.trim() ? { group: input.group.trim() } : {}),
        command: input.command.trim(),
        ...(input.appendCr === undefined ? {} : { appendCr: input.appendCr })
      });
      return {
        command: saved,
        ...(input.workspaceId ? { workspaceId: input.workspaceId } : {})
      };
    });
  }

  // ─── exec on a borrowed session ───────────────────────────────────────────

  /**
   * The one way to run a command. Two things about it are the user's choice in
   * NextShell, never the agent's: *how* it runs (`agent.execMode` — foreground
   * types it into the tab, raises the window and waits for OSC 133 `D`;
   * background uses a fresh exec channel; a local shell always runs in the
   * foreground) and *whether it runs at all* (`agent.execApproval` — `permission`
   * puts every command in front of the user first, `auto`, the default, trusts
   * the harness's own approval). The blacklist and the `.env` consent gate apply in every
   * combination; only the foreground path shares a keyboard with a human and
   * therefore checks for an unsubmitted line of theirs before typing.
   */
  async execCommand(
    client: AgentClientIdentity,
    input: {
      target?: string;
      command?: string;
      timeoutSec?: number;
      allowSensitive?: boolean;
      requestId?: string;
    }
  ): Promise<AgentToolResult<AgentExecPayload>> {
    const waitMs = this.deps.openWaitMs ?? OPEN_WAIT_MS;

    // Continuing to wait on a dialog that is already up: the command and its
    // session were pinned when the request was created, nothing is re-read.
    if (input.requestId) {
      const requestId = input.requestId;
      const pinned = this.pendingExec.get(requestId);
      const params = { requestId, command: pinned?.command, mode: pinned?.mode };
      return this.execute(
        client,
        "exec",
        params,
        async () => {
          const outcome = await this.openBroker.wait(requestId, waitMs);
          if (outcome === null || !pinned) {
            this.pendingExec.delete(requestId);
            throw new AgentToolFailure({
              code: "not_found",
              message:
                "No pending exec request matches that requestId; it was answered, expired, or never existed"
            });
          }
          if (outcome === "pending") return { status: "pending" as const, requestId };
          this.pendingExec.delete(requestId);
          if (!outcome.approved) throw AgentGateway.execDenied();
          return this.runExec(client, pinned);
        },
        {
          connectionId: pinned?.connectionId,
          requestedTimeoutMs: waitMs + MAX_WAIT_FOR_PROMPT_MS + WAIT_FOR_PROMPT_HEADROOM_MS
        }
      );
    }

    const command = (input.command ?? "").trim();
    const preferences = this.deps.getPreferences().agent;
    const live = input.target
      ? this.resolveLiveSession(input.target)
      : ({
          ok: false,
          error: { code: "invalid_argument", message: "target and command are required" }
        } as const);
    const mode: AgentExecMode =
      preferences.execMode === "background" && live.ok && live.connectionId
        ? "background"
        : "foreground";
    const params = {
      target: input.target,
      command,
      mode,
      timeoutSec: input.timeoutSec,
      allowSensitive: input.allowSensitive ?? false
    };
    if (!command) {
      return this.failed(client, "exec", params, {
        code: "invalid_argument",
        message: "command must not be empty"
      });
    }
    if (!live.ok) {
      return this.failed(client, "exec", params, live.error);
    }
    const blocked = this.blacklistHit(command);
    if (blocked) {
      return this.failed(
        client,
        "exec",
        params,
        { code: "forbidden", message: `Blocked by the command blacklist: ${blocked}` },
        live.connectionId
      );
    }
    const consent = this.consentRequired(command, input.allowSensitive);
    if (consent) {
      return this.failed(client, "exec", params, consent, live.connectionId);
    }

    const { session, connectionId } = live;
    const plan: PinnedExec = {
      session,
      connectionId,
      command,
      mode,
      timeoutSec: input.timeoutSec,
      // Background runs need the seconds; resolved here so a later approval
      // does not pick up a preference the user changed in the meantime.
      backgroundTimeoutMs: clampInt(input.timeoutSec, preferences.execTimeoutSec, 1, 3600) * 1000
    };
    const runBudgetMs =
      mode === "background"
        ? input.timeoutSec === undefined
          ? this.callTimeoutMs()
          : plan.backgroundTimeoutMs
        : Math.min(clampInt(input.timeoutSec, 120, 1, 3600) * 1000, MAX_WAIT_FOR_PROMPT_MS) +
          WAIT_FOR_PROMPT_HEADROOM_MS;

    if (preferences.execApproval === "permission") {
      const connection = connectionId
        ? this.deps.listConnections().find((c) => c.id === connectionId)
        : undefined;
      return this.execute(
        client,
        "exec",
        params,
        async () => {
          const requestId = this.openBroker.create({
            kind: "exec",
            clientName: client.name,
            connectionId: session.id,
            connectionName: session.title,
            host: connection?.host ?? "本地终端",
            reason: null,
            command,
            mode
          });
          this.pendingExec.set(requestId, plan);
          const outcome = await this.openBroker.wait(requestId, waitMs);
          if (outcome === "pending") return { status: "pending" as const, requestId };
          this.pendingExec.delete(requestId);
          if (outcome === null || !outcome.approved) throw AgentGateway.execDenied();
          return this.runExec(client, plan);
        },
        { connectionId, requestedTimeoutMs: waitMs + runBudgetMs + WAIT_FOR_PROMPT_HEADROOM_MS }
      );
    }

    return this.execute(client, "exec", params, () => this.runExec(client, plan), {
      connectionId,
      requestedTimeoutMs: runBudgetMs
    });
  }

  private static execDenied(): AgentToolFailure {
    return new AgentToolFailure({
      code: "denied",
      message:
        "The user did not approve running this command in NextShell (declined or no answer within 5 minutes). Stop and ask the user before retrying."
    });
  }

  /** The command itself, after every gate has passed. */
  private async runExec(client: AgentClientIdentity, plan: PinnedExec): Promise<AgentExecResult> {
    const { session, connectionId, command, mode } = plan;
    if (mode === "background" && connectionId) {
      // Inherits the cwd the shell reported via OSC 7.
      const cwd = session.cwd ?? undefined;
      const controller = new AbortController();
      const abortTimer = setTimeout(() => controller.abort(), plan.backgroundTimeoutMs);
      const release = this.deps.retainConnection(connectionId);
      try {
        const result = await this.deps.execCommand(connectionId, command, {
          ...(cwd ? { cwd } : {}),
          signal: controller.signal
        });
        return {
          sessionId: session.id,
          mode,
          command: redactText(command),
          exitCode: result.exitCode,
          output: redactText(result.stdout),
          stderr: redactText(result.stderr),
          waitTimedOut: false,
          actualCwd: result.cwd ?? cwd ?? null
        };
      } finally {
        clearTimeout(abortTimer);
        release();
        await this.deps.closeConnectionIfIdle(connectionId).catch(() => undefined);
      }
    }

    const intervention = await this.checkHumanIntervention(session.id);
    if (intervention) throw new AgentToolFailure(intervention);
    // The foreground wait is the tool's promised 120s unless the agent asks for
    // less; `execTimeoutSec` governs only the background channel. Kept under the
    // call ceiling so a slow command surfaces as `waitTimedOut: true`, not as a
    // `timeout` error.
    const waitMs = Math.min(
      clampInt(plan.timeoutSec, MAX_WAIT_FOR_PROMPT_MS / 1000, 1, 3600) * 1000,
      MAX_WAIT_FOR_PROMPT_MS
    );
    // Subscribed before the write so a fast command cannot complete in the
    // gap between injecting and starting to listen.
    const completion = this.deps.waitForCommandCompletion(session.id, waitMs);
    // Foreground means the user watches: bring the tab forward first.
    this.deps.focusSession(session.id);
    this.deps.setSessionAgentControlled(session.id, client.name);
    try {
      this.deps.writeSession(session.id, `${command}\r`);
      const settled = await completion;
      return {
        sessionId: session.id,
        mode,
        command: redactText(command),
        exitCode: settled?.exitCode ?? null,
        output: settled ? redactText(settled.output) : "",
        stderr: null,
        waitTimedOut: settled === null,
        actualCwd: null
      };
    } finally {
      // In a finally: a badge that survives a failed write would tell the
      // user an agent is still driving a terminal it never reached.
      this.deps.clearSessionAgentControlled(session.id);
    }
  }

  /** Control characters, named rather than raw so the agent cannot smuggle bytes. */
  private static readonly CONTROL_BYTES: Record<
    AgentControlSignal,
    { byte: string; label: string }
  > = {
    interrupt: { byte: "", label: "Ctrl-C（中断当前前台进程）" },
    eof: { byte: "", label: "Ctrl-D（发送 EOF，可能会结束 shell）" },
    suspend: { byte: "", label: "Ctrl-Z（挂起当前前台进程）" },
    quit: { byte: "", label: "Ctrl-\\（退出并转储核心）" }
  };

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

    const { session, connectionId } = live;
    return this.execute(
      client,
      "session_send_signal",
      params,
      async () => {
        this.deps.writeSession(session.id, control.byte);
        return { sessionId: session.id, signal: input.signal };
      },
      { connectionId }
    );
  }

  /** Read-only for the host; it only moves the user's own window. */
  async focusSession(
    client: AgentClientIdentity,
    input: { target: string }
  ): Promise<AgentToolResult<{ sessionId: string }>> {
    const params = { target: input.target };
    const session = this.openSessions().find((candidate) => candidate.id === input.target);
    if (!session) {
      return this.failed(client, "session_focus", params, {
        code: "not_found",
        message: "No active session matches that id; call session_list first"
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
}
