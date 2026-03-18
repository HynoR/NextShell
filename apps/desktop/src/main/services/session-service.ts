import os from "node:os";
import { randomUUID } from "node:crypto";
import type { WebContents } from "electron";
import { spawn as spawnPty } from "node-pty";
import type {
  ConnectionProfile,
  CommandExecutionResult,
  SessionDescriptor,
  SessionStatus,
} from "@nextshell/core";
import type { SshConnection } from "@nextshell/ssh";
import type {
  SessionAuthOverrideInput,
  SessionOpenInput,
  SessionStatusEvent,
  StreamDeliveryAckInput,
} from "@nextshell/shared";
import { AUTH_REQUIRED_PREFIX, IPCChannel } from "@nextshell/shared";
import type { CachedConnectionRepository } from "@nextshell/storage";
import type { ActiveSession, ActiveRemoteSession, SystemMonitorRuntime } from "./container-types";
import { normalizeError, toAuthRequiredReason, decodeTerminalData, encodeTerminalData } from "./container-utils";
import { createRemoteOsc7BootstrapPlan, resolveOsc7ShellFamily } from "./terminal-osc7-bootstrap";
import { resolveLocalShellLaunch } from "./local-shell";
import type { createOrderedBytesDispatcher, LatestOnlyDispatcher, LatestOnlyAckInput } from "./ipc-stream-dispatcher";
import { logger } from "../logger";

export interface SessionServiceOptions {
  connections: CachedConnectionRepository;
  activeSessions: Map<string, ActiveSession>;
  getConnectionOrThrow: (id: string) => ConnectionProfile;
  ensureConnection: (connectionId: string, authOverride?: SessionAuthOverrideInput) => Promise<SshConnection>;
  closeConnectionIfIdle: (connectionId: string) => Promise<void>;
  appendAuditLogIfEnabled: (payload: {
    action: string;
    level: "info" | "warn" | "error";
    connectionId?: string;
    message: string;
    metadata?: Record<string, unknown>;
  }) => void;
  sendSessionStatus: (sender: WebContents, payload: SessionStatusEvent) => void;
  sessionDataDispatcher: ReturnType<typeof createOrderedBytesDispatcher>;
  systemMonitorDispatcher: Pick<LatestOnlyDispatcher<unknown>, "ack" | "clear">;
  processMonitorDispatcher: Pick<LatestOnlyDispatcher<unknown>, "ack" | "clear">;
  networkMonitorDispatcher: Pick<LatestOnlyDispatcher<unknown>, "ack" | "clear">;
  ensureSystemMonitorRuntime: (connectionId: string) => Promise<SystemMonitorRuntime>;
  warmupSftp: (connectionId: string, connection: SshConnection) => Promise<string | undefined>;
  persistAuthOverride: (connectionId: string, authOverride: SessionAuthOverrideInput) => Promise<string | undefined>;
}

interface SessionExecOptions {
  signal?: AbortSignal;
  timeoutMs?: number;
}

interface SessionVisibleFilter {
  wrappedCommand: string | null;
  startSentinel: string;
  endPrefix: string;
  endSuffix: string;
  buffer: string;
  suppressNextNewline: boolean;
}

export class SessionService {
  private readonly connections: CachedConnectionRepository;
  private readonly activeSessions: Map<string, ActiveSession>;
  private readonly getConnectionOrThrow: (id: string) => ConnectionProfile;
  private readonly ensureConnection: (connectionId: string, authOverride?: SessionAuthOverrideInput) => Promise<SshConnection>;
  private readonly closeConnectionIfIdle: (connectionId: string) => Promise<void>;
  private readonly appendAuditLogIfEnabled: SessionServiceOptions["appendAuditLogIfEnabled"];
  private readonly sendSessionStatus: (sender: WebContents, payload: SessionStatusEvent) => void;
  private readonly sessionDataDispatcher: ReturnType<typeof createOrderedBytesDispatcher>;
  private readonly systemMonitorDispatcher: Pick<LatestOnlyDispatcher<unknown>, "ack" | "clear">;
  private readonly processMonitorDispatcher: Pick<LatestOnlyDispatcher<unknown>, "ack" | "clear">;
  private readonly networkMonitorDispatcher: Pick<LatestOnlyDispatcher<unknown>, "ack" | "clear">;
  private readonly ensureSystemMonitorRuntime: (connectionId: string) => Promise<SystemMonitorRuntime>;
  private readonly warmupSftp: (connectionId: string, connection: SshConnection) => Promise<string | undefined>;
  private readonly persistAuthOverride: (connectionId: string, authOverride: SessionAuthOverrideInput) => Promise<string | undefined>;
  private readonly sessionOutputListeners = new Map<string, Set<(chunk: string) => void>>();
  private readonly sessionCloseListeners = new Map<string, Set<(reason?: string) => void>>();
  private readonly sessionVisibleFilters = new Map<string, SessionVisibleFilter>();
  private readonly sessionExecLocks = new Set<string>();

  constructor(options: SessionServiceOptions) {
    this.connections = options.connections;
    this.activeSessions = options.activeSessions;
    this.getConnectionOrThrow = options.getConnectionOrThrow;
    this.ensureConnection = options.ensureConnection;
    this.closeConnectionIfIdle = options.closeConnectionIfIdle;
    this.appendAuditLogIfEnabled = options.appendAuditLogIfEnabled;
    this.sendSessionStatus = options.sendSessionStatus;
    this.sessionDataDispatcher = options.sessionDataDispatcher;
    this.systemMonitorDispatcher = options.systemMonitorDispatcher;
    this.processMonitorDispatcher = options.processMonitorDispatcher;
    this.networkMonitorDispatcher = options.networkMonitorDispatcher;
    this.ensureSystemMonitorRuntime = options.ensureSystemMonitorRuntime;
    this.warmupSftp = options.warmupSftp;
    this.persistAuthOverride = options.persistAuthOverride;
  }

  // ─── Public API ──────────────────────────────────────────────────────────

  async openSession(
    input: SessionOpenInput,
    sender: WebContents,
  ): Promise<SessionDescriptor> {
    if (input.target === "local") {
      return this.openLocalSession(sender, input.sessionId);
    }

    return this.openRemoteSession(input.connectionId, sender, input.sessionId, input.authOverride);
  }

  async openRemoteSession(
    connectionId: string,
    sender: WebContents,
    sessionId?: string,
    authOverride?: SessionAuthOverrideInput,
  ): Promise<SessionDescriptor> {
    const profile = this.getConnectionOrThrow(connectionId);
    const descriptorId = sessionId ?? randomUUID();
    if (this.activeSessions.has(descriptorId)) {
      throw new Error("Session id already exists");
    }
    const descriptor: SessionDescriptor = {
      id: descriptorId,
      target: "remote",
      connectionId,
      title: `${profile.name}@${profile.host}`,
      status: "connecting",
      type: "terminal",
      createdAt: new Date().toISOString(),
      reconnectable: true,
    };

    this.sendSessionStatus(sender, {
      sessionId: descriptor.id,
      status: "connecting",
    });

    try {
      const connection = await this.ensureConnection(connectionId, authOverride);
      let shellPath: string | undefined;
      let osc7ShellFamily: ReturnType<typeof resolveOsc7ShellFamily> = undefined;
      if (profile.monitorSession) {
        try {
          const shellProbe = await connection.exec('printf \'%s\' "${SHELL:-}"');
          shellPath = shellProbe.stdout.trim() || undefined;
          osc7ShellFamily = resolveOsc7ShellFamily(shellPath);
        } catch {
          shellPath = undefined;
          osc7ShellFamily = undefined;
        }
      }
      const osc7Bootstrap = createRemoteOsc7BootstrapPlan(
        Boolean(profile.monitorSession),
        profile.host,
        osc7ShellFamily,
        shellPath,
      );
      const shell = osc7Bootstrap.enabled && osc7Bootstrap.launchCommand
        ? await connection.openExecChannel(osc7Bootstrap.launchCommand, {
            cols: 140,
            rows: 40,
            term: "xterm-256color",
          })
        : await connection.openShell({
            cols: 140,
            rows: 40,
            term: "xterm-256color",
          });

      const now = new Date().toISOString();
      this.connections.save({
        ...profile,
        lastConnectedAt: now,
        updatedAt: now,
      });

      descriptor.status = "connected";

      this.activeSessions.set(descriptor.id, {
        kind: "remote",
        descriptor,
        channel: shell,
        sender,
        connectionId,
        terminalEncoding: profile.terminalEncoding,
        backspaceMode: profile.backspaceMode,
        deleteMode: profile.deleteMode,
      });

      shell.on("data", (chunk: Buffer | string) => {
        const active = this.activeSessions.get(descriptor.id);
        if (!active) {
          return;
        }
        const decodedChunk = decodeTerminalData(chunk, active.terminalEncoding);
        this.emitSessionOutput(descriptor.id, decodedChunk);
        const visibleChunk = this.transformVisibleSessionOutput(descriptor.id, decodedChunk);
        if (!visibleChunk) {
          return;
        }

        this.sessionDataDispatcher.push({
          streamId: descriptor.id,
          sender: active.sender,
          chunk: visibleChunk,
          onPause: () => shell.pause(),
          onResume: () => shell.resume(),
        });
      });

      shell.stderr.on("data", (chunk: Buffer | string) => {
        const active = this.activeSessions.get(descriptor.id);
        if (!active) {
          return;
        }
        const decodedChunk = decodeTerminalData(chunk, active.terminalEncoding);
        this.emitSessionOutput(descriptor.id, decodedChunk);
        const visibleChunk = this.transformVisibleSessionOutput(descriptor.id, decodedChunk);
        if (!visibleChunk) {
          return;
        }
        this.sessionDataDispatcher.push({
          streamId: descriptor.id,
          sender: active.sender,
          chunk: visibleChunk,
          onPause: () => shell.pause(),
          onResume: () => shell.resume(),
        });
      });

      shell.on("close", () => {
        shell.removeAllListeners();
        shell.stderr.removeAllListeners();
        this.finalizeRemoteSession(descriptor.id, "disconnected");
      });

      shell.on("error", (error: unknown) => {
        shell.removeAllListeners();
        shell.stderr.removeAllListeners();
        this.finalizeRemoteSession(descriptor.id, "failed", normalizeError(error));
      });

      let connectedReason = await this.warmupSftp(connectionId, connection);
      if (authOverride) {
        const persistWarning = await this.persistAuthOverride(connectionId, authOverride);
        if (persistWarning) {
          connectedReason = connectedReason
            ? `${connectedReason}；${persistWarning}`
            : persistWarning;
        }
      }

      if (profile.monitorSession) {
        try {
          await this.ensureSystemMonitorRuntime(connectionId);
        } catch (error) {
          const monitorReason = `Monitor Session 后台连接初始化失败：${normalizeError(error)}`;
          connectedReason = connectedReason
            ? `${connectedReason}；${monitorReason}`
            : monitorReason;
          logger.warn("[MonitorSession] failed to bootstrap runtime after terminal open", {
            connectionId,
            reason: normalizeError(error),
          });
        }
      }

      this.sendSessionStatus(sender, {
        sessionId: descriptor.id,
        status: "connected",
        reason: connectedReason,
      });

      this.appendAuditLogIfEnabled({
        action: "session.open",
        level: "info",
        connectionId,
        message: "SSH session opened",
        metadata: {
          sessionId: descriptor.id,
        },
      });

      return descriptor;
    } catch (error) {
      const rawReason = normalizeError(error);
      const authReason = toAuthRequiredReason(rawReason);
      const reason = authReason ? `${AUTH_REQUIRED_PREFIX}${authReason}` : rawReason;
      logger.error("[Session] failed to open", {
        connectionId,
        reason,
      });
      if (!authReason) {
        this.sendSessionStatus(sender, {
          sessionId: descriptor.id,
          status: "failed",
          reason,
        });
      }
      this.appendAuditLogIfEnabled({
        action: "session.open_failed",
        level: "error",
        connectionId,
        message: "SSH session failed to open",
        metadata: {
          reason,
          authRequired: Boolean(authReason),
        },
      });
      throw new Error(reason);
    }
  }

  async openLocalSession(
    sender: WebContents,
    sessionId?: string,
  ): Promise<SessionDescriptor> {
    const descriptorId = sessionId ?? randomUUID();
    if (this.activeSessions.has(descriptorId)) {
      throw new Error("Session id already exists");
    }

    const prefs = this.connections.getAppPreferences();
    const shellLaunch = resolveLocalShellLaunch(prefs.terminal.localShell, process.platform);
    const descriptor: SessionDescriptor = {
      id: descriptorId,
      target: "local",
      title: `本地终端 · ${shellLaunch.label}`,
      status: "connecting",
      type: "terminal",
      createdAt: new Date().toISOString(),
      reconnectable: true,
    };

    this.sendSessionStatus(sender, {
      sessionId: descriptor.id,
      status: "connecting",
    });

    try {
      const localShellEnv = Object.fromEntries(
        Object.entries(process.env).filter((entry): entry is [string, string] => typeof entry[1] === "string"),
      );
      const pty = spawnPty(shellLaunch.command, shellLaunch.args, {
        name: "xterm-256color",
        cols: 140,
        rows: 40,
        cwd: os.homedir(),
        env: localShellEnv,
      });

      descriptor.status = "connected";
      this.activeSessions.set(descriptor.id, {
        kind: "local",
        descriptor,
        pty,
        sender,
        terminalEncoding: "utf-8",
      });

      pty.onData((chunk) => {
        const active = this.activeSessions.get(descriptor.id);
        if (!active || active.kind !== "local") {
          return;
        }
        this.emitSessionOutput(descriptor.id, chunk);
        const visibleChunk = this.transformVisibleSessionOutput(descriptor.id, chunk);
        if (!visibleChunk) {
          return;
        }

        this.sessionDataDispatcher.push({
          streamId: descriptor.id,
          sender: active.sender,
          chunk: visibleChunk,
          onPause: () => pty.pause(),
          onResume: () => pty.resume(),
        });
      });

      pty.onExit(({ exitCode, signal }) => {
        const reasonParts: string[] = [];
        if (typeof exitCode === "number") {
          reasonParts.push(`exit ${exitCode}`);
        }
        if (typeof signal === "number") {
          reasonParts.push(`signal ${signal}`);
        }
        this.finalizeLocalSession(
          descriptor.id,
          "disconnected",
          reasonParts.length > 0 ? reasonParts.join(", ") : undefined,
        );
      });

      this.sendSessionStatus(sender, {
        sessionId: descriptor.id,
        status: "connected",
      });

      this.appendAuditLogIfEnabled({
        action: "session.local_open",
        level: "info",
        message: "Local terminal session opened",
        metadata: {
          sessionId: descriptor.id,
          shell: shellLaunch.command,
        },
      });

      return descriptor;
    } catch (error) {
      const reason = error instanceof Error ? error.message : "Failed to open local shell";
      logger.error("[Session] failed to open local terminal", {
        sessionId: descriptor.id,
        reason,
      });
      this.sendSessionStatus(sender, {
        sessionId: descriptor.id,
        status: "failed",
        reason,
      });
      this.appendAuditLogIfEnabled({
        action: "session.local_open_failed",
        level: "error",
        message: "Local terminal session failed to open",
        metadata: {
          sessionId: descriptor.id,
          reason,
        },
      });
      throw new Error(reason);
    }
  }

  writeSession(sessionId: string, data: string): { ok: true } {
    const active = this.activeSessions.get(sessionId);
    if (!active) {
      throw new Error("Session not found");
    }

    if (active.kind === "local") {
      active.pty.write(data);
      return { ok: true };
    }

    const buffer = encodeTerminalData(data, active.terminalEncoding);
    active.channel.write(buffer);
    return { ok: true };
  }

  async execCommandInSession(
    sessionId: string,
    command: string,
    options?: SessionExecOptions,
  ): Promise<CommandExecutionResult> {
    const active = this.activeSessions.get(sessionId);
    if (!active) {
      throw new Error("Session not found");
    }
    if (active.kind !== "remote") {
      throw new Error("AI 仅支持在远端终端会话中执行命令");
    }
    if (this.sessionExecLocks.has(sessionId)) {
      throw new Error("当前终端正在执行其他 AI 命令，请稍后再试");
    }

    this.sessionExecLocks.add(sessionId);

    const markerId = randomUUID().replace(/-/g, "");
    const startSentinel = this.buildSessionExecSentinel(`NEXTSHELL_AI_START_${markerId}`);
    const endPrefix = this.buildSessionExecSentinel(`NEXTSHELL_AI_END_${markerId}:`);
    const endSuffix = "\u001b\\";
    const wrappedCommand = [
      `printf '%b' '${this.encodeShellBytes(startSentinel)}'`,
      `eval -- ${this.escapeShellArg(command)}`,
      "__ns_ai_exit=$?",
      `printf '%b%s%b' '${this.encodeShellBytes(endPrefix)}' "$__ns_ai_exit" '${this.encodeShellBytes(endSuffix)}'`,
    ].join("; ");
    const chunks: string[] = [];

    return await new Promise<CommandExecutionResult>((resolve, reject) => {
      let settled = false;
      let timeoutId: ReturnType<typeof setTimeout> | undefined;

      const cleanup = (): void => {
        if (timeoutId) clearTimeout(timeoutId);
        this.removeSessionOutputListener(sessionId, onChunk);
        this.removeSessionCloseListener(sessionId, onSessionClose);
        options?.signal?.removeEventListener("abort", onAbort);
        this.flushSessionVisibleFilter(sessionId);
        this.sessionVisibleFilters.delete(sessionId);
        this.sessionExecLocks.delete(sessionId);
      };

      const finalize = (
        callback: () => void
      ): void => {
        if (settled) return;
        settled = true;
        cleanup();
        callback();
      };

      const extractCommandResult = (): CommandExecutionResult | undefined => {
        const rawOutput = chunks.join("");
        const startIndex = rawOutput.indexOf(startSentinel);
        if (startIndex < 0) {
          return undefined;
        }
        const payloadStart = startIndex + startSentinel.length;
        const endIndex = rawOutput.indexOf(endPrefix, payloadStart);
        if (endIndex < 0) {
          return undefined;
        }
        const exitStart = endIndex + endPrefix.length;
        const exitEnd = rawOutput.indexOf(endSuffix, exitStart);
        if (exitEnd < 0) {
          return undefined;
        }

        const payload = rawOutput.slice(payloadStart, endIndex);
        const exitCodeRaw = rawOutput.slice(exitStart, exitEnd).trim();
        const parsedExitCode = Number.parseInt(exitCodeRaw, 10);
        const normalizedOutput = payload
          .replace(/^\r?\n/, "")
          .replace(/\r?\n$/, "");

        return {
          connectionId: active.connectionId,
          command,
          stdout: normalizedOutput,
          stderr: "",
          exitCode: Number.isFinite(parsedExitCode) ? parsedExitCode : 1,
          executedAt: new Date().toISOString(),
        };
      };

      const onChunk = (chunk: string): void => {
        chunks.push(chunk);
        const result = extractCommandResult();
        if (result) {
          finalize(() => resolve(result));
        }
      };

      const onAbort = (): void => {
        this.interruptSession(sessionId);
        finalize(() => reject(options?.signal?.reason ?? new DOMException("Aborted", "AbortError")));
      };

      const onSessionClose = (reason?: string): void => {
        finalize(() => reject(new Error(reason ?? "终端会话已关闭，无法继续执行 AI 命令")));
      };

      this.addSessionOutputListener(sessionId, onChunk);
      this.addSessionCloseListener(sessionId, onSessionClose);
      this.sessionVisibleFilters.set(sessionId, {
        wrappedCommand,
        startSentinel,
        endPrefix,
        endSuffix,
        buffer: "",
        suppressNextNewline: false,
      });
      this.emitVisibleSessionCommand(sessionId, command);

      if (options?.signal?.aborted) {
        onAbort();
        return;
      }

      options?.signal?.addEventListener("abort", onAbort, { once: true });

      if (Number.isFinite(options?.timeoutMs) && (options?.timeoutMs ?? 0) > 0) {
        timeoutId = setTimeout(() => {
          this.interruptSession(sessionId);
          finalize(() => reject(new Error("远端命令执行超时")));
        }, options?.timeoutMs);
      }

      try {
        this.writeSession(sessionId, `${wrappedCommand}\r`);
      } catch (error) {
        finalize(() => reject(error));
      }
    });
  }

  resizeSession(
    sessionId: string,
    cols: number,
    rows: number,
  ): { ok: true } {
    const active = this.activeSessions.get(sessionId);
    if (!active) {
      // Session may have already disconnected; silently ignore resize requests
      return { ok: true };
    }

    if (active.kind === "local") {
      active.pty.resize(cols, rows);
      return { ok: true };
    }

    active.channel.setWindow(rows, cols, 0, 0);
    return { ok: true };
  }

  async closeSession(sessionId: string): Promise<{ ok: true }> {
    const active = this.activeSessions.get(sessionId);
    if (!active) {
      return { ok: true };
    }

    logger.info("[Session] closing", {
      sessionId,
      connectionId: active.kind === "remote" ? active.connectionId : undefined,
      target: active.descriptor.target,
    });
    this.sessionDataDispatcher.clear(sessionId);
    if (active.kind === "local") {
      active.pty.kill();
      this.emitSessionClosed(sessionId, "终端会话已关闭");
      this.activeSessions.delete(sessionId);
      this.sessionExecLocks.delete(sessionId);
      this.sessionCloseListeners.delete(sessionId);
      this.sessionVisibleFilters.delete(sessionId);
      this.sessionOutputListeners.delete(sessionId);
      this.sendSessionStatus(active.sender, {
        sessionId,
        status: "disconnected",
      });

      this.appendAuditLogIfEnabled({
        action: "session.local_close",
        level: "info",
        message: "Local terminal session closed",
        metadata: { sessionId },
      });
      return { ok: true };
    }

    active.channel.removeAllListeners();
    if (active.channel.stderr) {
      active.channel.stderr.removeAllListeners();
    }
    active.channel.end();
    this.emitSessionClosed(sessionId, "终端会话已关闭");
    this.activeSessions.delete(sessionId);
    this.sessionExecLocks.delete(sessionId);
    this.sessionCloseListeners.delete(sessionId);
    this.sessionVisibleFilters.delete(sessionId);
    this.sessionOutputListeners.delete(sessionId);
    this.sendSessionStatus(active.sender, {
      sessionId,
      status: "disconnected",
    });

    this.appendAuditLogIfEnabled({
      action: "session.close",
      level: "info",
      connectionId: active.connectionId,
      message: "SSH session closed",
      metadata: { sessionId },
    });

    await this.closeConnectionIfIdle(active.connectionId);
    return { ok: true };
  }

  ackStreamDelivery(input: StreamDeliveryAckInput): { ok: true } {
    switch (input.streamKind) {
      case "session":
        this.sessionDataDispatcher.ack({
          streamId: input.streamId,
          deliveryId: input.deliveryId,
          consumedBytes: input.consumedBytes,
        });
        break;
      case "monitor-system":
        this.systemMonitorDispatcher.ack({
          streamId: input.streamId,
          deliveryId: input.deliveryId,
        });
        break;
      case "monitor-process":
        this.processMonitorDispatcher.ack({
          streamId: input.streamId,
          deliveryId: input.deliveryId,
        });
        break;
      case "monitor-network":
        this.networkMonitorDispatcher.ack({
          streamId: input.streamId,
          deliveryId: input.deliveryId,
        });
        break;
    }

    return { ok: true };
  }

  // ─── Internal Cleanup ────────────────────────────────────────────────────

  finalizeRemoteSession(
    sessionId: string,
    status: Extract<SessionStatus, "disconnected" | "failed">,
    reason?: string,
  ): void {
    const active = this.activeSessions.get(sessionId);
    if (!active) {
      return;
    }

    active.descriptor.status = status;
    this.emitSessionClosed(sessionId, reason);
    this.sessionDataDispatcher.closeWhenDrained(sessionId, () => {
      const drained = this.activeSessions.get(sessionId);
      if (!drained || drained.kind !== "remote") {
        return;
      }

      this.activeSessions.delete(sessionId);
      this.sessionExecLocks.delete(sessionId);
      this.sessionCloseListeners.delete(sessionId);
      this.sessionVisibleFilters.delete(sessionId);
      this.sessionOutputListeners.delete(sessionId);
      drained.descriptor.status = status;
      this.sendSessionStatus(drained.sender, { sessionId, status, reason });
      void this.closeConnectionIfIdle(drained.connectionId);
    });
  }

  finalizeLocalSession(
    sessionId: string,
    status: Extract<SessionStatus, "disconnected" | "failed">,
    reason?: string,
  ): void {
    const active = this.activeSessions.get(sessionId);
    if (!active || active.kind !== "local") {
      return;
    }

    active.descriptor.status = status;
    this.emitSessionClosed(sessionId, reason);
    this.sessionDataDispatcher.closeWhenDrained(sessionId, () => {
      const drained = this.activeSessions.get(sessionId);
      if (!drained || drained.kind !== "local") {
        return;
      }

      this.activeSessions.delete(sessionId);
      this.sessionExecLocks.delete(sessionId);
      this.sessionCloseListeners.delete(sessionId);
      this.sessionVisibleFilters.delete(sessionId);
      this.sessionOutputListeners.delete(sessionId);
      drained.descriptor.status = status;
      this.sendSessionStatus(drained.sender, { sessionId, status, reason });
      this.appendAuditLogIfEnabled({
        action: "session.local_close",
        level: status === "failed" ? "error" : "info",
        message: "Local terminal session closed",
        metadata: { sessionId, reason },
      });
    });
  }

  private addSessionOutputListener(sessionId: string, listener: (chunk: string) => void): void {
    const listeners = this.sessionOutputListeners.get(sessionId) ?? new Set<(chunk: string) => void>();
    listeners.add(listener);
    this.sessionOutputListeners.set(sessionId, listeners);
  }

  private removeSessionOutputListener(sessionId: string, listener: (chunk: string) => void): void {
    const listeners = this.sessionOutputListeners.get(sessionId);
    if (!listeners) return;
    listeners.delete(listener);
    if (listeners.size === 0) {
      this.sessionOutputListeners.delete(sessionId);
    }
  }

  private emitSessionOutput(sessionId: string, chunk: string): void {
    const listeners = this.sessionOutputListeners.get(sessionId);
    if (!listeners || listeners.size === 0) return;
    for (const listener of listeners) {
      listener(chunk);
    }
  }

  private addSessionCloseListener(sessionId: string, listener: (reason?: string) => void): void {
    const listeners = this.sessionCloseListeners.get(sessionId) ?? new Set<(reason?: string) => void>();
    listeners.add(listener);
    this.sessionCloseListeners.set(sessionId, listeners);
  }

  private removeSessionCloseListener(sessionId: string, listener: (reason?: string) => void): void {
    const listeners = this.sessionCloseListeners.get(sessionId);
    if (!listeners) return;
    listeners.delete(listener);
    if (listeners.size === 0) {
      this.sessionCloseListeners.delete(sessionId);
    }
  }

  private emitSessionClosed(sessionId: string, reason?: string): void {
    const listeners = this.sessionCloseListeners.get(sessionId);
    if (!listeners || listeners.size === 0) return;
    for (const listener of listeners) {
      listener(reason);
    }
  }

  private emitVisibleSessionCommand(sessionId: string, command: string): void {
    this.pushSessionVisibleChunk(sessionId, `${command}\r\n`);
  }

  private flushSessionVisibleFilter(sessionId: string): void {
    const filter = this.sessionVisibleFilters.get(sessionId);
    if (!filter) {
      return;
    }
    const visibleChunk = this.transformVisibleSessionOutput(sessionId, "", { flushAll: true });
    if (visibleChunk) {
      this.pushSessionVisibleChunk(sessionId, visibleChunk);
    }
  }

  private pushSessionVisibleChunk(sessionId: string, chunk: string): void {
    if (!chunk) {
      return;
    }
    const active = this.activeSessions.get(sessionId);
    if (!active) {
      return;
    }
    this.sessionDataDispatcher.push({
      streamId: sessionId,
      sender: active.sender,
      chunk,
      onPause: () => undefined,
      onResume: () => undefined,
    });
  }

  private transformVisibleSessionOutput(
    sessionId: string,
    chunk: string,
    options?: { flushAll?: boolean },
  ): string {
    const filter = this.sessionVisibleFilters.get(sessionId);
    if (!filter) {
      return chunk;
    }

    filter.buffer += chunk;
    let output = filter.buffer;

    if (filter.wrappedCommand) {
      const wrappedIndex = output.indexOf(filter.wrappedCommand);
      if (wrappedIndex >= 0) {
        output = output.slice(0, wrappedIndex) + output.slice(wrappedIndex + filter.wrappedCommand.length);
        filter.wrappedCommand = null;
        filter.suppressNextNewline = true;
      }
    }

    if (filter.suppressNextNewline) {
      if (output.startsWith("\r\n")) {
        output = output.slice(2);
        filter.suppressNextNewline = false;
      } else if (output.startsWith("\n") || output.startsWith("\r")) {
        output = output.slice(1);
        filter.suppressNextNewline = false;
      }
    }

    output = output.split(filter.startSentinel).join("");

    while (true) {
      const startIndex = output.indexOf(filter.endPrefix);
      if (startIndex < 0) {
        break;
      }
      const endIndex = output.indexOf(filter.endSuffix, startIndex + filter.endPrefix.length);
      if (endIndex < 0) {
        break;
      }
      output = output.slice(0, startIndex) + output.slice(endIndex + filter.endSuffix.length);
    }

    if (options?.flushAll) {
      if (filter.wrappedCommand) {
        output = "";
        filter.wrappedCommand = null;
      }
      if (filter.suppressNextNewline) {
        output = output.replace(/^(?:\r\n|\n|\r)/, "");
        filter.suppressNextNewline = false;
      }
      filter.buffer = "";
      return output;
    }

    const tailLength = this.getSessionVisibleFilterTailLength(filter);
    if (output.length <= tailLength) {
      filter.buffer = output;
      return "";
    }

    const visibleChunk = output.slice(0, output.length - tailLength);
    filter.buffer = output.slice(output.length - tailLength);
    return visibleChunk;
  }

  private getSessionVisibleFilterTailLength(filter: SessionVisibleFilter): number {
    const echoTail = filter.wrappedCommand ? filter.wrappedCommand.length : 0;
    const startTail = filter.startSentinel.length;
    const endTail = filter.endPrefix.length + filter.endSuffix.length + 8;
    return Math.max(echoTail, startTail, endTail);
  }

  private buildSessionExecSentinel(marker: string): string {
    return `\u001bP${marker}\u001b\\`;
  }

  private encodeShellBytes(value: string): string {
    return Array.from(value)
      .map((char) => `\\${char.charCodeAt(0).toString(8).padStart(3, "0")}`)
      .join("");
  }

  private interruptSession(sessionId: string): void {
    try {
      this.writeSession(sessionId, "\u0003");
    } catch {
      // 会话已关闭时无需重复抛错，后续由关闭回调收敛 Promise。
    }
  }

  private escapeShellArg(arg: string): string {
    return `'${arg.replace(/'/g, "'\\''")}'`;
  }
}
