import os from "node:os";
import { randomUUID } from "node:crypto";
import type { WebContents } from "electron";
import { spawn as spawnPty } from "node-pty";
import type { ConnectionProfile, SessionDescriptor, SessionStatus } from "@nextshell/core";
import type { SshConnection } from "@nextshell/ssh";
import type {
  SessionAuthOverrideInput,
  SessionOpenInput,
  SessionStatusEvent,
  StreamDeliveryAckInput
} from "@nextshell/shared";
import { AUTH_REQUIRED_PREFIX, IPCChannel } from "@nextshell/shared";
import type { CachedConnectionRepository } from "@nextshell/storage";
import type { ActiveSession, ActiveRemoteSession } from "./container-types";
import {
  normalizeError,
  toAuthRequiredReason,
  decodeTerminalData,
  encodeTerminalData
} from "./container-utils";
import { prepareShellIntegrationLaunch } from "./terminal-shell-integration";
import { resolveLocalShellLaunch } from "./local-shell";
import type { createOrderedBytesDispatcher } from "./ipc-stream-dispatcher";
import { logger } from "../logger";

export interface SessionServiceOptions {
  connections: CachedConnectionRepository;
  activeSessions: Map<string, ActiveSession>;
  getConnectionOrThrow: (id: string) => ConnectionProfile;
  /**
   * Hands out a pooled SSH client that still has channel budget left, together
   * with a reservation for the shell this session is about to open. The
   * reservation must be released once the shell exists (or the open failed).
   */
  acquireTerminalConnection: (
    connectionId: string,
    authOverride?: SessionAuthOverrideInput
  ) => Promise<{ connection: SshConnection; release: () => void }>;
  /**
   * Marks the connection as in use for the whole "session is opening" window —
   * `activeSessions` only learns about the session once the shell is up, so
   * without this a concurrently closing last tab would close the client out
   * from under it.
   */
  retainConnection: (connectionId: string) => () => void;
  closeConnectionIfIdle: (connectionId: string) => Promise<void>;
  sendSessionStatus: (sender: WebContents, payload: SessionStatusEvent) => void;
  sessionDataDispatcher: ReturnType<typeof createOrderedBytesDispatcher>;
  warmupSftp: (connectionId: string, connection: SshConnection) => Promise<string | undefined>;
  persistAuthOverride: (
    connectionId: string,
    authOverride: SessionAuthOverrideInput
  ) => Promise<string | undefined>;
  tapAgentSessionData: (sessionId: string, connectionId: string, data: string) => void;
  disposeAgentSessionData: (sessionId: string) => void;
  /** Keeps the agent-facing screen mirror the same size as the user's terminal. */
  onSessionResized: (sessionId: string, cols: number, rows: number) => void;
}

export class SessionService {
  private readonly connections: CachedConnectionRepository;
  private readonly activeSessions: Map<string, ActiveSession>;
  private readonly getConnectionOrThrow: (id: string) => ConnectionProfile;
  private readonly acquireTerminalConnection: SessionServiceOptions["acquireTerminalConnection"];
  private readonly retainConnection: (connectionId: string) => () => void;
  private readonly closeConnectionIfIdle: (connectionId: string) => Promise<void>;
  private readonly sendSessionStatus: (sender: WebContents, payload: SessionStatusEvent) => void;
  private readonly sessionDataDispatcher: ReturnType<typeof createOrderedBytesDispatcher>;
  private readonly warmupSftp: (
    connectionId: string,
    connection: SshConnection
  ) => Promise<string | undefined>;
  private readonly persistAuthOverride: (
    connectionId: string,
    authOverride: SessionAuthOverrideInput
  ) => Promise<string | undefined>;
  private readonly tapAgentSessionData: SessionServiceOptions["tapAgentSessionData"];
  private readonly disposeAgentSessionData: SessionServiceOptions["disposeAgentSessionData"];
  private readonly onSessionResized: SessionServiceOptions["onSessionResized"];
  /** Last real keystroke per session; drives agent-injection preemption. */
  private readonly userInputAt = new Map<string, number>();

  constructor(options: SessionServiceOptions) {
    this.connections = options.connections;
    this.activeSessions = options.activeSessions;
    this.getConnectionOrThrow = options.getConnectionOrThrow;
    this.acquireTerminalConnection = options.acquireTerminalConnection;
    this.retainConnection = options.retainConnection;
    this.closeConnectionIfIdle = options.closeConnectionIfIdle;
    this.sendSessionStatus = options.sendSessionStatus;
    this.sessionDataDispatcher = options.sessionDataDispatcher;
    this.warmupSftp = options.warmupSftp;
    this.persistAuthOverride = options.persistAuthOverride;
    this.tapAgentSessionData = options.tapAgentSessionData;
    this.disposeAgentSessionData = (sessionId) => {
      this.userInputAt.delete(sessionId);
      options.disposeAgentSessionData(sessionId);
    };
    this.onSessionResized = options.onSessionResized;
  }

  // ─── Public API ──────────────────────────────────────────────────────────

  async openSession(input: SessionOpenInput, sender: WebContents): Promise<SessionDescriptor> {
    if (input.target === "local") {
      return this.openLocalSession(sender, input.sessionId);
    }

    return this.openRemoteSession(input.connectionId, sender, input.sessionId, input.authOverride);
  }

  async openRemoteSession(
    connectionId: string,
    sender: WebContents,
    sessionId?: string,
    authOverride?: SessionAuthOverrideInput
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
      reconnectable: true
    };

    this.sendSessionStatus(sender, {
      sessionId: descriptor.id,
      status: "connecting"
    });

    // Held for the whole open window: the session only becomes visible in
    // activeSessions once its shell is up, so until then nothing else can tell
    // that this connection is still needed — closing another tab meanwhile
    // used to take the shared client down with it.
    const releaseConnectionRef = this.retainConnection(connectionId);
    let releaseChannelSlot: (() => void) | undefined;

    try {
      const lease = await this.acquireTerminalConnection(connectionId, authOverride);
      const connection = lease.connection;
      releaseChannelSlot = lease.release;
      // Shell integration ("auto" mode only): activate at shell startup by
      // opening the terminal as an exec+PTY request that launches the login
      // shell with a bootstrap file — never by typing into the shell's stdin
      // (no history pollution, no echoed line, no race with TUIs/prompts).
      // The probe + install round trips run once per connection and are
      // cached; every failure falls back to a plain shell with no integration.
      const integrationLaunchCommand =
        this.connections.getAppPreferences().terminal.shellIntegration === "auto"
          ? await prepareShellIntegrationLaunch({
              connection,
              connectionId,
              log: (message, metadata) =>
                logger.info(message, { sessionId: descriptor.id, connectionId, ...metadata })
            })
          : undefined;
      const shellWindow = { cols: 140, rows: 40, term: "xterm-256color" } as const;
      const shell = integrationLaunchCommand
        ? await connection
            .openExecChannel(integrationLaunchCommand, shellWindow)
            .catch((error: unknown) => {
              // e.g. sshd with exec requests restricted: degrade to a plain
              // shell rather than failing the session over an enhancement.
              logger.warn("[ShellIntegration] exec+PTY launch failed; opening plain shell", {
                connectionId,
                reason: normalizeError(error)
              });
              return connection.openShell(shellWindow);
            })
        : await connection.openShell(shellWindow);
      // The real channel now holds the budget slot the lease was standing in for.
      releaseChannelSlot();

      const now = new Date().toISOString();
      this.connections.save({
        ...profile,
        lastConnectedAt: now,
        updatedAt: now
      });

      descriptor.status = "connected";

      this.activeSessions.set(descriptor.id, {
        kind: "remote",
        descriptor,
        channel: shell,
        connection,
        sender,
        connectionId,
        terminalEncoding: profile.terminalEncoding,
        backspaceMode: profile.backspaceMode,
        deleteMode: profile.deleteMode
      });

      shell.on("data", (chunk: Buffer | string) => {
        const active = this.activeSessions.get(descriptor.id);
        if (!active || active.kind !== "remote") {
          return;
        }

        const decoded = decodeTerminalData(chunk, active.terminalEncoding);
        this.tapAgentSessionData(descriptor.id, active.connectionId, decoded);
        this.sessionDataDispatcher.push({
          streamId: descriptor.id,
          sender: active.sender,
          chunk: decoded,
          onPause: () => shell.pause(),
          onResume: () => shell.resume()
        });
      });

      shell.stderr.on("data", (chunk: Buffer | string) => {
        const active = this.activeSessions.get(descriptor.id);
        if (!active || active.kind !== "remote") {
          return;
        }
        const decoded = decodeTerminalData(chunk, active.terminalEncoding);
        this.tapAgentSessionData(descriptor.id, active.connectionId, decoded);
        this.sessionDataDispatcher.push({
          streamId: descriptor.id,
          sender: active.sender,
          chunk: decoded,
          onPause: () => shell.pause(),
          onResume: () => shell.resume()
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

      this.sendSessionStatus(sender, {
        sessionId: descriptor.id,
        status: "connected",
        reason: connectedReason
      });

      return descriptor;
    } catch (error) {
      const rawReason = normalizeError(error);
      const authReason = toAuthRequiredReason(rawReason);
      const reason = authReason ? `${AUTH_REQUIRED_PREFIX}${authReason}` : rawReason;
      logger.error("[Session] failed to open", {
        connectionId,
        reason
      });
      if (!authReason) {
        this.sendSessionStatus(sender, {
          sessionId: descriptor.id,
          status: "failed",
          reason
        });
      }
      throw new Error(reason);
    } finally {
      releaseChannelSlot?.();
      releaseConnectionRef();
      // If this open failed, another tab's close may have been skipped because
      // of the reference we were holding — re-run the idle check so a client
      // nobody uses any more is not left behind.
      if (!this.activeSessions.has(descriptor.id)) {
        void this.closeConnectionIfIdle(connectionId);
      }
    }
  }

  async openLocalSession(sender: WebContents, sessionId?: string): Promise<SessionDescriptor> {
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
      reconnectable: true
    };

    this.sendSessionStatus(sender, {
      sessionId: descriptor.id,
      status: "connecting"
    });

    try {
      const localShellEnv = Object.fromEntries(
        Object.entries(process.env).filter(
          (entry): entry is [string, string] => typeof entry[1] === "string"
        )
      );
      const pty = spawnPty(shellLaunch.command, shellLaunch.args, {
        name: "xterm-256color",
        cols: 140,
        rows: 40,
        cwd: os.homedir(),
        env: localShellEnv
      });

      descriptor.status = "connected";
      this.activeSessions.set(descriptor.id, {
        kind: "local",
        descriptor,
        pty,
        sender,
        terminalEncoding: "utf-8"
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
          onResume: () => pty.resume()
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
          reasonParts.length > 0 ? reasonParts.join(", ") : undefined
        );
      });

      this.sendSessionStatus(sender, {
        sessionId: descriptor.id,
        status: "connected"
      });

      return descriptor;
    } catch (error) {
      const reason = error instanceof Error ? error.message : "Failed to open local shell";
      logger.error("[Session] failed to open local terminal", {
        sessionId: descriptor.id,
        reason
      });
      this.sendSessionStatus(sender, {
        sessionId: descriptor.id,
        status: "failed",
        reason
      });
      throw new Error(reason);
    }
  }

  /**
   * `origin` is what makes agent injection safe to allow at all: a real
   * keystroke stamps the session, and {@link lastUserInputAt} lets the agent
   * gateway stand down while a human is actively typing into the same PTY.
   * Anything the renderer sends is a human or a protocol reply on their behalf;
   * "agent" only ever originates inside the main process.
   */
  writeSession(
    sessionId: string,
    data: string,
    origin: "user" | "protocol" | "agent" = "user"
  ): { ok: true } {
    const active = this.activeSessions.get(sessionId);
    if (!active) {
      throw new Error("Session not found");
    }

    if (origin === "user") {
      this.userInputAt.set(sessionId, Date.now());
    }

    if (active.kind === "local") {
      active.pty.write(data);
      return { ok: true };
    }

    const buffer = encodeTerminalData(data, active.terminalEncoding);
    active.channel.write(buffer);
    return { ok: true };
  }

  /** Epoch millis of the last real keystroke, or `null` if there has been none. */
  lastUserInputAt(sessionId: string): number | null {
    return this.userInputAt.get(sessionId) ?? null;
  }

  resizeSession(sessionId: string, cols: number, rows: number): { ok: true } {
    const active = this.activeSessions.get(sessionId);
    if (!active) {
      // Session may have already disconnected; silently ignore resize requests
      return { ok: true };
    }

    // The agent-facing mirror has to track the same geometry, or the frame it
    // reports back wraps differently from the one the user is looking at.
    this.onSessionResized(sessionId, cols, rows);

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
      target: active.descriptor.target
    });
    this.sessionDataDispatcher.clear(sessionId);
    this.disposeAgentSessionData(sessionId);
    if (active.kind === "local") {
      active.pty.kill();
      this.activeSessions.delete(sessionId);
      this.disposeAgentSessionData(sessionId);
      this.sendSessionStatus(active.sender, {
        sessionId,
        status: "disconnected"
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
      status: "disconnected"
    });

    await this.closeConnectionIfIdle(active.connectionId);
    return { ok: true };
  }

  ackStreamDelivery(input: StreamDeliveryAckInput): { ok: true } {
    // Only the ordered terminal byte stream uses the ack protocol; monitor
    // snapshots are sent directly without delivery tracking. Acks are batched
    // by the renderer: deliveryId is the highest id processed so far and
    // consumedBytes is the byte delta since its previous ack.
    this.sessionDataDispatcher.ack({
      streamId: input.streamId,
      deliveryId: input.deliveryId,
      consumedBytes: input.consumedBytes
    });

    return { ok: true };
  }

  // ─── Internal Cleanup ────────────────────────────────────────────────────

  finalizeRemoteSession(
    sessionId: string,
    status: Extract<SessionStatus, "disconnected" | "failed">,
    reason?: string
  ): void {
    const active = this.activeSessions.get(sessionId);
    if (!active) {
      return;
    }

    active.descriptor.status = status;
    // closeWhenDrained is guaranteed to invoke the callback exactly once:
    // on drain (acks), when the sender is destroyed, on ack-stall timeout, or
    // at the dispatcher's hard drain deadline. A hung renderer that stops
    // acking therefore cannot leak this session or its SSH connection.
    this.sessionDataDispatcher.closeWhenDrained(sessionId, () => {
      const drained = this.activeSessions.get(sessionId);
      if (!drained || drained.kind !== "remote") {
        return;
      }

      this.activeSessions.delete(sessionId);
      this.disposeAgentSessionData(sessionId);
      drained.descriptor.status = status;
      this.sendSessionStatus(drained.sender, { sessionId, status, reason });
      void this.closeConnectionIfIdle(drained.connectionId);
    });
  }

  finalizeLocalSession(
    sessionId: string,
    status: Extract<SessionStatus, "disconnected" | "failed">,
    reason?: string
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
    });
  }
}
