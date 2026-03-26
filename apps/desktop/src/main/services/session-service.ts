import os from "node:os";
import { randomUUID } from "node:crypto";
import type { WebContents } from "electron";
import { spawn as spawnPty } from "node-pty";
import type {
  ConnectionProfile,
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
  clearMonitorSuspension: (connectionId: string) => void;
  warmupSftp: (connectionId: string, connection: SshConnection) => Promise<string | undefined>;
  persistAuthOverride: (connectionId: string, authOverride: SessionAuthOverrideInput) => Promise<string | undefined>;
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
  private readonly clearMonitorSuspension: (connectionId: string) => void;
  private readonly warmupSftp: (connectionId: string, connection: SshConnection) => Promise<string | undefined>;
  private readonly persistAuthOverride: (connectionId: string, authOverride: SessionAuthOverrideInput) => Promise<string | undefined>;

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
    this.clearMonitorSuspension = options.clearMonitorSuspension;
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

        this.sessionDataDispatcher.push({
          streamId: descriptor.id,
          sender: active.sender,
          chunk: decodeTerminalData(chunk, active.terminalEncoding),
          onPause: () => shell.pause(),
          onResume: () => shell.resume(),
        });
      });

      shell.stderr.on("data", (chunk: Buffer | string) => {
        const active = this.activeSessions.get(descriptor.id);
        if (!active) {
          return;
        }
        this.sessionDataDispatcher.push({
          streamId: descriptor.id,
          sender: active.sender,
          chunk: decodeTerminalData(chunk, active.terminalEncoding),
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
        this.clearMonitorSuspension(connectionId);
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

        this.sessionDataDispatcher.push({
          streamId: descriptor.id,
          sender: active.sender,
          chunk,
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
      this.activeSessions.delete(sessionId);
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
    this.activeSessions.delete(sessionId);
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
    this.sessionDataDispatcher.closeWhenDrained(sessionId, () => {
      const drained = this.activeSessions.get(sessionId);
      if (!drained || drained.kind !== "remote") {
        return;
      }

      this.activeSessions.delete(sessionId);
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
    this.sessionDataDispatcher.closeWhenDrained(sessionId, () => {
      const drained = this.activeSessions.get(sessionId);
      if (!drained || drained.kind !== "local") {
        return;
      }

      this.activeSessions.delete(sessionId);
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
}
