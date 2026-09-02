import type {
  AppPreferences,
  ConnectionProfile,
  SavedCommand,
  ScopedCommandItem,
  SessionStatus,
  SessionType
} from "@nextshell/core";
import type { AgentActivityEvent } from "@nextshell/shared";
import { matchDangerousCommand } from "@nextshell/terminal";

import type { ScreenReadOptions, ScreenReadResult } from "./screen-mirror";

// ─── Client identity ────────────────────────────────────────────────────────

/**
 * The endpoint only ever listens on a Unix socket / named pipe (0600), so the
 * OS file permissions are the client authorization — there is no token and no
 * approval handshake to identify beyond what `initialize` reports.
 */
export interface AgentClientIdentity {
  /** MCP session id. */
  id: string;
  name: string | null;
  version: string | null;
  transport: "socket";
}

// ─── Result envelope ────────────────────────────────────────────────────────

export type AgentErrorCode =
  | "invalid_argument"
  | "not_found"
  | "forbidden"
  | "timeout"
  | "unavailable"
  | "human_intervention"
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

export type AgentControlSignal = "interrupt" | "eof" | "suspend" | "quit";

export interface AgentExecPayload {
  sessionId: string;
  connectionId: string;
  command: string;
  stdout: string;
  stderr: string;
  exitCode: number;
  actualCwd: string | null;
  executedAt: string;
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
  getPreferences: () => AppPreferences;
  now?: () => number;
}

// ─── Helpers ────────────────────────────────────────────────────────────────

const AGENT_REDACTED = "«redacted»";
const ANSI_ESCAPE_PATTERN =
  /[][[\]()#;?]*(?:(?:(?:[a-zA-Z\d]*(?:;[-a-zA-Z\d\x2f#&.:=?%@~_]+)*)?)|(?:(?:\d{1,4}(?:[;:]\d{0,4})*)?[\dA-PR-TZcf-nq-uy=><~]))/g;

const MAX_LIST_ITEMS = 500;
/** Ceiling on `session_send_keys` `waitForPrompt`. */
const MAX_WAIT_FOR_PROMPT_MS = 120_000;
/** Slack between a `waitForPrompt` deadline and the enclosing call timeout. */
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
 * concurrency budgets here. The endpoint is a 0600 Unix socket (OS-level
 * authorization), the agent only ever borrows sessions the user already
 * opened, and the preset-plus-user blacklist is the only command filter.
 */
export class AgentGateway {
  private readonly deps: AgentGatewayDeps;
  /**
   * Per-session baseline for human-intervention detection: the moment of this
   * agent's own last `send_keys`/`exec` call. Human keystrokes stamped later
   * than this mean the person took the keyboard back.
   */
  private readonly agentTouchedAt = new Map<string, number>();
  private activitySequence = 0;
  /**
   * The global breaker. Once tripped every tool call fails immediately, without
   * a dialog, until the user re-arms it — the point of a kill switch is that it
   * does not negotiate.
   */
  private halted = false;

  constructor(deps: AgentGatewayDeps) {
    this.deps = deps;
  }

  get isHalted(): boolean {
    return this.halted;
  }

  /** Trips or re-arms the breaker. */
  setHalted(halted: boolean): void {
    this.halted = halted;
  }

  /**
   * Every open remote tab is agent-visible by construction: the user opening
   * the tab is the grant. Local shells have no connection and stay invisible.
   */
  private openRemoteSessions(): AgentSessionInfo[] {
    return this.deps.listSessions().filter((session) => session.connectionId !== null);
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
    const commandSummary =
      typeof params.command === "string" ? redactText(params.command) : undefined;
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
   * Resolves a session id for a takeover tool: the session must exist, be a live
   * remote tab and be connected.
   */
  private resolveLiveSession(
    sessionId: string
  ):
    | { ok: true; session: AgentSessionInfo; connectionId: string }
    | { ok: false; error: AgentToolError } {
    const session = this.openRemoteSessions().find((candidate) => candidate.id === sessionId);
    if (!session || !session.connectionId) {
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
    return { ok: true, session, connectionId: session.connectionId };
  }

  /**
   * Human intervention is an error, not a pause: when a real keystroke lands in
   * the tab after this agent's last operation, the call fails with
   * `human_intervention` and the agent is expected to stop and report. The
   * error is one-shot — this call becomes the new baseline, so the next call
   * proceeds unless the human typed again.
   */
  private checkHumanIntervention(sessionId: string): AgentToolError | null {
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

  // ─── Session discovery and reads ──────────────────────────────────────────

  async listSessions(
    client: AgentClientIdentity
  ): Promise<AgentToolResult<AgentSessionListPayload>> {
    return this.execute(client, "session_list", {}, async () => {
      const connectionsById = new Map(
        this.deps.listConnections().map((connection) => [connection.id, connection])
      );
      const sessions = this.openRemoteSessions().map<AgentSessionListEntry>((session) => {
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
    const session = this.openRemoteSessions().find((candidate) => candidate.id === input.target);
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
    const session = this.openRemoteSessions().find((candidate) => candidate.id === input.target);
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
   * Runs a command on a fresh exec channel of the connection backing an open
   * session — the agent borrows the user's already-authenticated connection;
   * it can never dial one itself.
   */
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
    const live = this.resolveLiveSession(input.target);
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
    const intervention = this.checkHumanIntervention(live.session.id);
    if (intervention) {
      return this.failed(client, "exec", params, intervention, live.connectionId);
    }

    let requestedCwd: string | undefined;
    if (input.cwd !== undefined) {
      const normalized = normalizeRemotePath(input.cwd);
      if (!normalized.ok) {
        return this.failed(client, "exec", params, normalized.error, live.connectionId);
      }
      requestedCwd = normalized.data;
    } else {
      // Inherit the cwd the shell reported via OSC 7.
      requestedCwd = live.session.cwd ?? undefined;
    }

    const preferences = this.deps.getPreferences().agent;
    const requestedTimeoutMs =
      input.timeoutSec === undefined
        ? undefined
        : clampInt(input.timeoutSec, preferences.execTimeoutSec, 1, 3600) * 1000;
    const { session, connectionId } = live;
    return this.execute(
      client,
      "exec",
      params,
      async (signal) => {
        const release = this.deps.retainConnection(connectionId);
        try {
          const result = await this.deps.execCommand(connectionId, command, {
            ...(requestedCwd ? { cwd: requestedCwd } : {}),
            signal
          });
          return {
            sessionId: session.id,
            connectionId,
            command: redactText(command),
            stdout: redactText(result.stdout),
            stderr: redactText(result.stderr),
            exitCode: result.exitCode,
            actualCwd: result.cwd ?? requestedCwd ?? null,
            executedAt: result.executedAt
          };
        } finally {
          release();
          await this.deps.closeConnectionIfIdle(connectionId).catch(() => undefined);
        }
      },
      { connectionId, requestedTimeoutMs }
    );
  }

  // ─── PTY takeover ─────────────────────────────────────────────────────────

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

  /**
   * Types into the PTY the user is looking at. The blacklist runs on the
   * injected text, and a human keystroke since the agent's last operation
   * fails the call with `human_intervention` instead of waiting the person out.
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
    if (input.text.length > 0) {
      const blocked = this.blacklistHit(input.text);
      if (blocked) {
        return this.failed(
          client,
          "session_send_keys",
          params,
          { code: "forbidden", message: `Blocked by the command blacklist: ${blocked}` },
          live.connectionId
        );
      }
    }
    const intervention = this.checkHumanIntervention(live.session.id);
    if (intervention) {
      return this.failed(client, "session_send_keys", params, intervention, live.connectionId);
    }

    const { session, connectionId } = live;
    const payload = input.submit ? `${input.text}\r` : input.text;
    // Kept strictly under the call ceiling: if the wait could outlive the call,
    // a slow command would surface as a `timeout` error instead of the honest
    // `waitTimedOut: true` the tool promises.
    const waitMs = Math.min(clampInt(input.timeoutSec, 30, 1, 3600) * 1000, MAX_WAIT_FOR_PROMPT_MS);

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
        this.deps.setSessionAgentControlled(session.id, client.name);
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
          this.deps.clearSessionAgentControlled(session.id);
        }
      },
      {
        connectionId,
        // The wait is the point of the call, so it gets the whole budget.
        requestedTimeoutMs: input.waitForPrompt ? waitMs + WAIT_FOR_PROMPT_HEADROOM_MS : undefined
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
    const session = this.openRemoteSessions().find((candidate) => candidate.id === input.target);
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
