import { randomUUID } from "node:crypto";
import type { WebContents } from "electron";
import type {
  ConnectionProfile,
  MonitorSnapshot,
  NetworkConnection,
  NetworkSnapshot,
  ProcessDetailSnapshot,
  ProcessSnapshot,
  SystemInfoSnapshot,
} from "../../../../../packages/core/src/index";
import {
  SshConnection,
  type SshConnectOptions,
} from "../../../../../packages/ssh/src/index";
import type {
  DebugLogEntry,
} from "../../../../../packages/shared/src/index";
import type { CachedConnectionRepository } from "../../../../../packages/storage/src/index";
import {
  SystemMonitorController,
  type MonitorSelectionState,
  type ProbeExecutionLog,
} from "./monitor/system-monitor-controller";
import {
  ProcessMonitorController,
  type ProcessProbeExecutionLog,
} from "./monitor/process-monitor-controller";
import {
  firstNonEmptyLine,
  parseProcessDetailPrimary,
} from "./monitor/process-probe-parser";
import {
  NetworkMonitorController,
  type NetworkProbeExecutionLog,
  type NetworkTool,
} from "./monitor/network-monitor-controller";
import {
  parseCpuInfo,
  parseFilesystemEntries,
  parseMeminfoTotals,
  parseNetworkInterfaceTotals,
  parseOsReleaseName,
} from "./system-info-parser";
import {
  MONITOR_SYSTEM_INFO_KERNEL_NAME_COMMAND,
  MONITOR_NETWORK_INTERVAL_MS,
  MONITOR_PROCESS_INTERVAL_MS,
  ADHOC_IDLE_TIMEOUT_MS,
  MONITOR_MAX_CONSECUTIVE_FAILURES,
  MONITOR_COMMAND_TIMEOUT_MS,
  normalizeError,
  parseUptimeSeconds,
  parseCompoundOutput,
  buildSystemInfoCommand,
} from "./container-utils";
import type {
  ActiveSession,
  AdhocSessionRuntime,
  MonitorState,
  SystemMonitorRuntime,
  ProcessMonitorRuntime,
  NetworkMonitorRuntime,
} from "./container-types";
import { MonitorBackoff } from "./monitor/monitor-runner";
import { logger } from "../logger";

// Hidden monitor SSH re-establishment backs off from the FIRST failure so a
// dead network (e.g. right after OS sleep) doesn't trigger a reconnect storm.
const HIDDEN_CONNECT_BACKOFF_BASE_MS = 5_000;
const HIDDEN_CONNECT_BACKOFF_MAX_MS = 300_000; // cap at 5 minutes
const HIDDEN_MONITOR_TAGS = ["SystemMonitor", "ProcessMonitor", "NetworkMonitor"] as const;

// ─── Options ────────────────────────────────────────────────────────────────

export interface MonitorServiceOptions {
  connections: CachedConnectionRepository;
  getConnectionOrThrow: (id: string) => ConnectionProfile;
  resolveConnectOptions: (profile: ConnectionProfile) => Promise<SshConnectOptions>;
  activeSessions: Map<string, ActiveSession>;
  appendAuditLogIfEnabled: (payload: {
    action: string;
    level: "info" | "warn" | "error";
    connectionId?: string;
    message: string;
    metadata?: Record<string, unknown>;
  }) => void;
  debugSenders: Set<WebContents>;
  emitDebugLog: (entry: DebugLogEntry) => void;
  /** Direct guarded webContents.send of a snapshot payload (no ack protocol). */
  emitSystemSnapshot: (sender: WebContents, snapshot: MonitorSnapshot) => void;
  emitProcessSnapshot: (sender: WebContents, snapshot: ProcessSnapshot) => void;
  emitNetworkSnapshot: (sender: WebContents, snapshot: NetworkSnapshot) => void;
}

// ─── Service ────────────────────────────────────────────────────────────────

export class MonitorService {
  // ─── Maps ───────────────────────────────────────────────────────────────
  private readonly systemMonitorRuntimes = new Map<string, SystemMonitorRuntime>();
  private readonly systemMonitorConnections = new Map<string, SshConnection>();
  private readonly systemMonitorConnectionPromises = new Map<string, Promise<SshConnection>>();
  private readonly cancelledSystemMonitorConnections = new Set<string>();

  private readonly processMonitorRuntimes = new Map<string, ProcessMonitorRuntime>();
  private readonly processMonitorPromises = new Map<string, Promise<ProcessMonitorRuntime>>();
  private readonly processMonitorConnections = new Map<string, SshConnection>();
  private readonly processMonitorConnectionPromises = new Map<string, Promise<SshConnection>>();
  private readonly cancelledProcessMonitorConnections = new Set<string>();

  private readonly networkMonitorRuntimes = new Map<string, NetworkMonitorRuntime>();
  private readonly networkMonitorPromises = new Map<string, Promise<NetworkMonitorRuntime>>();
  private readonly networkMonitorConnections = new Map<string, SshConnection>();
  private readonly networkMonitorConnectionPromises = new Map<string, Promise<SshConnection>>();
  private readonly cancelledNetworkMonitorConnections = new Set<string>();

  private readonly adhocSessionRuntimes = new Map<string, AdhocSessionRuntime>();
  private readonly adhocSessionPromises = new Map<string, Promise<AdhocSessionRuntime>>();

  readonly monitorStates = new Map<string, MonitorState>();
  private readonly networkToolCache = new Map<string, NetworkTool>();

  /** Per hidden-connection ("<tag>:<connectionId>") reconnect backoff. */
  private readonly hiddenConnectionBackoffs = new Map<string, MonitorBackoff>();
  /** Guards against registering onClose twice on the same SshConnection object. */
  private readonly closeListenerRegistered = new WeakSet<SshConnection>();

  // ─── Injected dependencies ──────────────────────────────────────────────
  private readonly connections: CachedConnectionRepository;
  private readonly getConnectionOrThrow: (id: string) => ConnectionProfile;
  private readonly resolveConnectOptions: (profile: ConnectionProfile) => Promise<SshConnectOptions>;
  private readonly activeSessions: Map<string, ActiveSession>;
  private readonly appendAuditLogIfEnabled: MonitorServiceOptions["appendAuditLogIfEnabled"];
  private readonly debugSenders: Set<WebContents>;
  private readonly emitDebugLog: (entry: DebugLogEntry) => void;
  private readonly emitSystemSnapshot: (sender: WebContents, snapshot: MonitorSnapshot) => void;
  private readonly emitProcessSnapshot: (sender: WebContents, snapshot: ProcessSnapshot) => void;
  private readonly emitNetworkSnapshot: (sender: WebContents, snapshot: NetworkSnapshot) => void;

  constructor(options: MonitorServiceOptions) {
    this.connections = options.connections;
    this.getConnectionOrThrow = options.getConnectionOrThrow;
    this.resolveConnectOptions = options.resolveConnectOptions;
    this.activeSessions = options.activeSessions;
    this.appendAuditLogIfEnabled = options.appendAuditLogIfEnabled;
    this.debugSenders = options.debugSenders;
    this.emitDebugLog = options.emitDebugLog;
    this.emitSystemSnapshot = options.emitSystemSnapshot;
    this.emitProcessSnapshot = options.emitProcessSnapshot;
    this.emitNetworkSnapshot = options.emitNetworkSnapshot;
  }

  // ─── Guard helpers ────────────────────────────────────────────────────────

  private assertMonitorEnabled(connectionId: string): ConnectionProfile {
    const profile = this.getConnectionOrThrow(connectionId);
    if (!profile.monitorSession) {
      throw new Error("当前连接未启用 Monitor Session，请在连接配置中开启后重试。");
    }
    return profile;
  }

  private assertVisibleTerminalAlive(connectionId: string): void {
    if (!this.hasVisibleTerminalAlive(connectionId)) {
      throw new Error("请先连接 SSH 终端以启动 Monitor Session。");
    }
  }

  private isSenderAlive(sender: WebContents | undefined): boolean {
    return Boolean(sender && !sender.isDestroyed() && !sender.isCrashed());
  }

  private hasVisibleTerminalAlive(connectionId: string): boolean {
    return Array.from(this.activeSessions.values()).some((session) => {
      return (
        session.kind === "remote" &&
        session.connectionId === connectionId &&
        session.descriptor.type === "terminal" &&
        session.descriptor.status === "connected"
      );
    });
  }

  // ─── Session ① System Monitor: dispose ──────────────────────────────────

  async disposeSystemMonitorRuntime(connectionId: string): Promise<void> {
    const runtime = this.systemMonitorRuntimes.get(connectionId);
    if (runtime) {
      runtime.disposed = true;
      await runtime.controller.stop();
      this.systemMonitorRuntimes.delete(connectionId);
    }
    await this.closeSystemMonitorConnection(connectionId);
  }

  // ─── Session ② Process Monitor: dispose ─────────────────────────────────

  async disposeProcessMonitorRuntime(connectionId: string): Promise<void> {
    const runtime = this.processMonitorRuntimes.get(connectionId);
    if (runtime) {
      runtime.disposed = true;
      await runtime.controller.stop();
      this.processMonitorRuntimes.delete(connectionId);
    }
    await this.closeProcessMonitorConnection(connectionId);
    this.processMonitorPromises.delete(connectionId);
  }

  // ─── Session ③ Network Monitor: dispose ─────────────────────────────────

  async disposeNetworkMonitorRuntime(connectionId: string): Promise<void> {
    const runtime = this.networkMonitorRuntimes.get(connectionId);
    if (runtime) {
      runtime.disposed = true;
      await runtime.controller.stop();
      this.networkMonitorRuntimes.delete(connectionId);
    }
    await this.closeNetworkMonitorConnection(connectionId);
    this.networkMonitorPromises.delete(connectionId);
  }

  // ─── Session ④ Ad-hoc: dispose ──────────────────────────────────────────

  async disposeAdhocSession(connectionId: string): Promise<void> {
    const runtime = this.adhocSessionRuntimes.get(connectionId);
    if (runtime) {
      runtime.disposed = true;
      if (runtime.idleTimer) {
        clearTimeout(runtime.idleTimer);
        runtime.idleTimer = undefined;
      }
      this.adhocSessionRuntimes.delete(connectionId);

      try { await runtime.connection.close(); } catch (error) {
        logger.warn("[AdhocSession] failed to close connection", { connectionId, reason: normalizeError(error) });
      }
    }
    this.adhocSessionPromises.delete(connectionId);
  }

  /** Return all connection IDs that have any active monitor/adhoc state. */
  getAllConnectionIds(): string[] {
    const ids = new Set<string>();
    for (const id of this.systemMonitorRuntimes.keys()) ids.add(id);
    for (const id of this.systemMonitorConnections.keys()) ids.add(id);
    for (const id of this.processMonitorRuntimes.keys()) ids.add(id);
    for (const id of this.processMonitorConnections.keys()) ids.add(id);
    for (const id of this.networkMonitorRuntimes.keys()) ids.add(id);
    for (const id of this.networkMonitorConnections.keys()) ids.add(id);
    for (const id of this.adhocSessionRuntimes.keys()) ids.add(id);
    return Array.from(ids);
  }

  // ─── Dispose all hidden sessions for a connection ───────────────────────

  async disposeAllMonitorSessions(connectionId: string): Promise<void> {
    await Promise.all([
      this.disposeSystemMonitorRuntime(connectionId),
      this.disposeProcessMonitorRuntime(connectionId),
      this.disposeNetworkMonitorRuntime(connectionId),
      this.disposeAdhocSession(connectionId)
    ]);
    this.monitorStates.delete(connectionId);
    this.networkToolCache.delete(connectionId);
    for (const tag of HIDDEN_MONITOR_TAGS) {
      this.hiddenConnectionBackoffs.delete(`${tag}:${connectionId}`);
    }
  }

  // ─── Pause / resume all monitor polling (OS suspend, window hidden) ─────

  /** Stop the poll timers of all active monitor controllers without tearing down runtimes. */
  pauseAll(): void {
    for (const runtime of this.systemMonitorRuntimes.values()) runtime.controller.pause();
    for (const runtime of this.processMonitorRuntimes.values()) runtime.controller.pause();
    for (const runtime of this.networkMonitorRuntimes.values()) runtime.controller.pause();
  }

  /** Restart poll timers stopped by pauseAll(); idempotent. */
  resumeAll(): void {
    // Network conditions have changed (wake / window shown) — retry promptly.
    for (const backoff of this.hiddenConnectionBackoffs.values()) backoff.reset();
    for (const runtime of this.systemMonitorRuntimes.values()) runtime.controller.resume();
    for (const runtime of this.processMonitorRuntimes.values()) runtime.controller.resume();
    for (const runtime of this.networkMonitorRuntimes.values()) runtime.controller.resume();
  }

  /** Clear suspension on all monitors for a connection (call on terminal reconnect). */
  clearMonitorSuspension(connectionId: string): void {
    this.systemMonitorRuntimes.get(connectionId)?.controller.clearSuspension();
    this.processMonitorRuntimes.get(connectionId)?.controller.clearSuspension();
    this.networkMonitorRuntimes.get(connectionId)?.controller.clearSuspension();
    for (const tag of HIDDEN_MONITOR_TAGS) {
      this.hiddenConnectionBackoffs.get(`${tag}:${connectionId}`)?.reset();
    }
  }

  // ─── Generic hidden SSH connection factory ──────────────────────────────

  private getHiddenConnectionBackoff(connectionId: string, tag: string): MonitorBackoff {
    const key = `${tag}:${connectionId}`;
    let backoff = this.hiddenConnectionBackoffs.get(key);
    if (!backoff) {
      backoff = new MonitorBackoff(
        HIDDEN_CONNECT_BACKOFF_BASE_MS,
        HIDDEN_CONNECT_BACKOFF_MAX_MS,
        Number.POSITIVE_INFINITY,
      );
      this.hiddenConnectionBackoffs.set(key, backoff);
    }
    return backoff;
  }

  private async establishHiddenConnection(
    connectionId: string,
    tag: string,
    options?: { backoff?: boolean }
  ): Promise<SshConnection> {
    const backoff = (options?.backoff ?? true)
      ? this.getHiddenConnectionBackoff(connectionId, tag)
      : undefined;
    if (backoff?.isActive()) {
      throw new Error(
        `${tag} 重连退避中，${Math.ceil(backoff.remainingMs() / 1000)}s 后重试`
      );
    }

    const profile = this.assertMonitorEnabled(connectionId);
    logger.info(`[${tag}] connecting hidden SSH`, { connectionId, host: profile.host, port: profile.port });
    try {
      const ssh = await SshConnection.connect(await this.resolveConnectOptions(profile));
      backoff?.reset();
      logger.info(`[${tag}] hidden SSH connected`, { connectionId });
      return ssh;
    } catch (error) {
      if (backoff) {
        const delayMs = backoff.apply();
        logger.warn(`[${tag}] hidden SSH connect failed, backing off`, {
          connectionId,
          delayMs,
          reason: normalizeError(error),
        });
      }
      throw error;
    }
  }

  /** Register onClose exactly once per SshConnection object (ssh2 gateway has no listener removal). */
  private registerCloseListenerOnce(connection: SshConnection, handler: () => void): void {
    if (this.closeListenerRegistered.has(connection)) {
      return;
    }
    this.closeListenerRegistered.add(connection);
    connection.onClose(handler);
  }

  // ─── System Monitor connection ──────────────────────────────────────────

  private async closeSystemMonitorConnection(connectionId: string): Promise<void> {
    this.cancelledSystemMonitorConnections.add(connectionId);
    const existing = this.systemMonitorConnections.get(connectionId);
    this.systemMonitorConnections.delete(connectionId);
    this.systemMonitorConnectionPromises.delete(connectionId);
    if (!existing) {
      return;
    }

    try {
      await existing.close();
    } catch (error) {
      logger.warn("[SystemMonitor] failed to close connection", {
        connectionId,
        reason: normalizeError(error),
      });
    }
  }

  private async ensureSystemMonitorConnection(connectionId: string): Promise<SshConnection> {
    this.cancelledSystemMonitorConnections.delete(connectionId);
    const existing = this.systemMonitorConnections.get(connectionId);
    if (existing) {
      return existing;
    }

    const pending = this.systemMonitorConnectionPromises.get(connectionId);
    if (pending) {
      return pending;
    }

    const promise = (async () => {
      const connection = await this.establishHiddenConnection(connectionId, "SystemMonitor");
      if (this.cancelledSystemMonitorConnections.has(connectionId)) {
        this.cancelledSystemMonitorConnections.delete(connectionId);
        try { await connection.close(); } catch { /* ignore */ }
        throw new Error("SystemMonitor connection discarded");
      }

      this.systemMonitorConnections.set(connectionId, connection);
      this.registerCloseListenerOnce(connection, () => {
        const wasActive = this.systemMonitorConnections.get(connectionId) === connection;
        if (wasActive) {
          this.systemMonitorConnections.delete(connectionId);
          logger.warn("[SystemMonitor] hidden SSH disconnected unexpectedly", { connectionId });
        }
      });
      return connection;
    })();

    this.systemMonitorConnectionPromises.set(connectionId, promise);
    try {
      return await promise;
    } finally {
      if (this.systemMonitorConnectionPromises.get(connectionId) === promise) {
        this.systemMonitorConnectionPromises.delete(connectionId);
      }
    }
  }

  // ─── Process Monitor connection ─────────────────────────────────────────

  private async closeProcessMonitorConnection(connectionId: string): Promise<void> {
    this.cancelledProcessMonitorConnections.add(connectionId);
    const existing = this.processMonitorConnections.get(connectionId);
    this.processMonitorConnections.delete(connectionId);
    this.processMonitorConnectionPromises.delete(connectionId);
    if (!existing) {
      return;
    }

    try {
      await existing.close();
    } catch (error) {
      logger.warn("[ProcessMonitor] failed to close connection", {
        connectionId,
        reason: normalizeError(error),
      });
    }
  }

  private async ensureProcessMonitorConnection(connectionId: string): Promise<SshConnection> {
    this.cancelledProcessMonitorConnections.delete(connectionId);
    const existing = this.processMonitorConnections.get(connectionId);
    if (existing) {
      return existing;
    }

    const pending = this.processMonitorConnectionPromises.get(connectionId);
    if (pending) {
      return pending;
    }

    const promise = (async () => {
      const connection = await this.establishHiddenConnection(connectionId, "ProcessMonitor");
      if (this.cancelledProcessMonitorConnections.has(connectionId)) {
        this.cancelledProcessMonitorConnections.delete(connectionId);
        try { await connection.close(); } catch { /* ignore */ }
        throw new Error("ProcessMonitor connection discarded");
      }

      this.processMonitorConnections.set(connectionId, connection);
      this.registerCloseListenerOnce(connection, () => {
        const wasActive = this.processMonitorConnections.get(connectionId) === connection;
        if (wasActive) {
          this.processMonitorConnections.delete(connectionId);
          logger.warn("[ProcessMonitor] hidden SSH disconnected unexpectedly", { connectionId });
        }
      });
      return connection;
    })();

    this.processMonitorConnectionPromises.set(connectionId, promise);
    try {
      return await promise;
    } finally {
      if (this.processMonitorConnectionPromises.get(connectionId) === promise) {
        this.processMonitorConnectionPromises.delete(connectionId);
      }
    }
  }

  // ─── Network Monitor connection ─────────────────────────────────────────

  private async closeNetworkMonitorConnection(connectionId: string): Promise<void> {
    this.cancelledNetworkMonitorConnections.add(connectionId);
    const existing = this.networkMonitorConnections.get(connectionId);
    this.networkMonitorConnections.delete(connectionId);
    this.networkMonitorConnectionPromises.delete(connectionId);
    if (!existing) {
      return;
    }

    try {
      await existing.close();
    } catch (error) {
      logger.warn("[NetworkMonitor] failed to close connection", {
        connectionId,
        reason: normalizeError(error),
      });
    }
  }

  private async ensureNetworkMonitorConnection(connectionId: string): Promise<SshConnection> {
    this.cancelledNetworkMonitorConnections.delete(connectionId);
    const existing = this.networkMonitorConnections.get(connectionId);
    if (existing) {
      return existing;
    }

    const pending = this.networkMonitorConnectionPromises.get(connectionId);
    if (pending) {
      return pending;
    }

    const promise = (async () => {
      const connection = await this.establishHiddenConnection(connectionId, "NetworkMonitor");
      if (this.cancelledNetworkMonitorConnections.has(connectionId)) {
        this.cancelledNetworkMonitorConnections.delete(connectionId);
        try { await connection.close(); } catch { /* ignore */ }
        throw new Error("NetworkMonitor connection discarded");
      }

      this.networkMonitorConnections.set(connectionId, connection);
      this.registerCloseListenerOnce(connection, () => {
        const wasActive = this.networkMonitorConnections.get(connectionId) === connection;
        if (wasActive) {
          this.networkMonitorConnections.delete(connectionId);
          logger.warn("[NetworkMonitor] hidden SSH disconnected unexpectedly", { connectionId });
        }
      });
      return connection;
    })();

    this.networkMonitorConnectionPromises.set(connectionId, promise);
    try {
      return await promise;
    } finally {
      if (this.networkMonitorConnectionPromises.get(connectionId) === promise) {
        this.networkMonitorConnectionPromises.delete(connectionId);
      }
    }
  }

  // ─── Session ① System Monitor: ensure ───────────────────────────────────

  async ensureSystemMonitorRuntime(connectionId: string): Promise<SystemMonitorRuntime> {
    const existing = this.systemMonitorRuntimes.get(connectionId);
    if (existing && !existing.disposed) {
      return existing;
    }

    let runtime: SystemMonitorRuntime;

    const onProbeExecution = (entry: ProbeExecutionLog) => {
      if (this.debugSenders.size > 0) {
        this.emitDebugLog({
          id: randomUUID(),
          timestamp: Date.now(),
          connectionId,
          command: entry.command,
          stdout: entry.stdout.slice(0, 4096),
          exitCode: entry.exitCode,
          durationMs: entry.durationMs,
          ok: entry.ok,
          error: entry.error,
        });
      }

      if (!entry.ok && entry.exitCode >= 0) {
        logger.debug("[SystemMonitor] command non-zero exit", {
          connectionId,
          command: entry.command,
          exitCode: entry.exitCode,
          output: entry.stdout.slice(0, 200),
        });
      }
    };

    const controller = new SystemMonitorController({
      connectionId,
      getConnection: () => this.ensureSystemMonitorConnection(connectionId),
      closeConnection: () => this.closeSystemMonitorConnection(connectionId),
      isVisibleTerminalAlive: () => this.hasVisibleTerminalAlive(connectionId),
      isReceiverAlive: () => this.isSenderAlive(runtime.sender),
      emitSnapshot: (snapshot) => {
        if (this.isSenderAlive(runtime.sender) && runtime.sender) {
          this.emitSystemSnapshot(runtime.sender, snapshot);
        }
      },
      readSelection: () => this.monitorStates.get(connectionId),
      writeSelection: (state: MonitorSelectionState) => {
        const previous = this.monitorStates.get(connectionId);
        this.monitorStates.set(connectionId, { ...previous, ...state });
      },
      logger,
      onProbeExecution,
    });

    runtime = {
      disposed: false,
      controller,
      sender: undefined,
    };

    this.systemMonitorRuntimes.set(connectionId, runtime);
    logger.info("[SystemMonitor] runtime ready", { connectionId });
    return runtime;
  }

  // ─── Session ② Process Monitor: ensure ──────────────────────────────────

  private async ensureProcessMonitorRuntime(connectionId: string): Promise<ProcessMonitorRuntime> {
    const existing = this.processMonitorRuntimes.get(connectionId);
    if (existing && !existing.disposed) {
      return existing;
    }

    const pending = this.processMonitorPromises.get(connectionId);
    if (pending) {
      return pending;
    }

    const promise = (async () => {
      let runtime: ProcessMonitorRuntime;
      const onProbeExecution = (entry: ProcessProbeExecutionLog) => {
        if (this.debugSenders.size > 0) {
          this.emitDebugLog({
            id: randomUUID(),
            timestamp: Date.now(),
            connectionId,
            command: entry.command,
            stdout: entry.stdout.slice(0, 4096),
            exitCode: entry.exitCode,
            durationMs: entry.durationMs,
            ok: entry.ok,
            error: entry.error,
          });
        }
      };

      const controller = new ProcessMonitorController({
        connectionId,
        getConnection: () => this.ensureProcessMonitorConnection(connectionId),
        closeConnection: () => this.closeProcessMonitorConnection(connectionId),
        isVisibleTerminalAlive: () => this.hasVisibleTerminalAlive(connectionId),
        isReceiverAlive: () => this.isSenderAlive(runtime.sender),
        emitSnapshot: (snapshot) => {
          if (this.isSenderAlive(runtime.sender) && runtime.sender) {
            this.emitProcessSnapshot(runtime.sender, snapshot);
          }
        },
        logger,
        onProbeExecution,
        timing: {
          pollIntervalMs: MONITOR_PROCESS_INTERVAL_MS,
          execTimeoutMs: MONITOR_COMMAND_TIMEOUT_MS,
          maxConsecutiveFailures: MONITOR_MAX_CONSECUTIVE_FAILURES,
        },
      });

      runtime = {
        controller,
        sender: undefined,
        disposed: false,
      };

      this.processMonitorRuntimes.set(connectionId, runtime);

      if (!this.hasVisibleTerminalAlive(connectionId)) {
        await this.disposeProcessMonitorRuntime(connectionId);
        throw new Error("可见 SSH 会话已关闭，Process Monitor 启动取消。");
      }

      logger.info("[ProcessMonitor] runtime ready", { connectionId });
      return runtime;
    })();

    this.processMonitorPromises.set(connectionId, promise);
    try {
      return await promise;
    } catch (error) {
      await this.disposeProcessMonitorRuntime(connectionId);
      throw error;
    } finally {
      this.processMonitorPromises.delete(connectionId);
    }
  }

  // ─── Session ③ Network Monitor: ensure ──────────────────────────────────

  private async ensureNetworkMonitorRuntime(connectionId: string): Promise<NetworkMonitorRuntime> {
    const existing = this.networkMonitorRuntimes.get(connectionId);
    if (existing && !existing.disposed) {
      return existing;
    }

    const pending = this.networkMonitorPromises.get(connectionId);
    if (pending) {
      return pending;
    }

    const promise = (async () => {
      let runtime: NetworkMonitorRuntime;
      const onProbeExecution = (entry: NetworkProbeExecutionLog) => {
        if (this.debugSenders.size > 0) {
          this.emitDebugLog({
            id: randomUUID(),
            timestamp: Date.now(),
            connectionId,
            command: entry.command,
            stdout: entry.stdout.slice(0, 4096),
            exitCode: entry.exitCode,
            durationMs: entry.durationMs,
            ok: entry.ok,
            error: entry.error,
          });
        }
      };

      const controller = new NetworkMonitorController({
        connectionId,
        getConnection: () => this.ensureNetworkMonitorConnection(connectionId),
        closeConnection: () => this.closeNetworkMonitorConnection(connectionId),
        isVisibleTerminalAlive: () => this.hasVisibleTerminalAlive(connectionId),
        isReceiverAlive: () => this.isSenderAlive(runtime.sender),
        emitSnapshot: (snapshot) => {
          if (this.isSenderAlive(runtime.sender) && runtime.sender) {
            this.emitNetworkSnapshot(runtime.sender, snapshot);
          }
        },
        readToolCache: () => this.networkToolCache.get(connectionId),
        writeToolCache: (tool) => {
          if (tool) {
            this.networkToolCache.set(connectionId, tool);
          } else {
            this.networkToolCache.delete(connectionId);
          }
        },
        logger,
        onProbeExecution,
        timing: {
          pollIntervalMs: MONITOR_NETWORK_INTERVAL_MS,
          execTimeoutMs: MONITOR_COMMAND_TIMEOUT_MS,
          maxConsecutiveFailures: MONITOR_MAX_CONSECUTIVE_FAILURES,
        },
      });

      runtime = {
        controller,
        sender: undefined,
        disposed: false,
      };

      this.networkMonitorRuntimes.set(connectionId, runtime);

      if (!this.hasVisibleTerminalAlive(connectionId)) {
        await this.disposeNetworkMonitorRuntime(connectionId);
        throw new Error("可见 SSH 会话已关闭，Network Monitor 启动取消。");
      }

      logger.info("[NetworkMonitor] runtime ready", { connectionId });
      return runtime;
    })();

    this.networkMonitorPromises.set(connectionId, promise);
    try {
      return await promise;
    } catch (error) {
      await this.disposeNetworkMonitorRuntime(connectionId);
      throw error;
    } finally {
      this.networkMonitorPromises.delete(connectionId);
    }
  }

  // ─── Session ④ Ad-hoc: ensure ──────────────────────────────────────────

  private resetAdhocIdleTimer(connectionId: string, runtime: AdhocSessionRuntime): void {
    if (runtime.idleTimer) {
      clearTimeout(runtime.idleTimer);
    }
    runtime.lastUsedAt = Date.now();
    runtime.idleTimer = setTimeout(() => {
      logger.info("[AdhocSession] idle timeout, disposing", { connectionId });
      void this.disposeAdhocSession(connectionId);
    }, ADHOC_IDLE_TIMEOUT_MS);
  }

  private async ensureAdhocSession(connectionId: string): Promise<AdhocSessionRuntime> {
    const existing = this.adhocSessionRuntimes.get(connectionId);
    if (existing && !existing.disposed) {
      this.resetAdhocIdleTimer(connectionId, existing);
      return existing;
    }

    const pending = this.adhocSessionPromises.get(connectionId);
    if (pending) {
      return pending;
    }

    const promise = (async () => {
      const connection = await this.establishHiddenConnection(connectionId, "AdhocSession", { backoff: false });

      const runtime: AdhocSessionRuntime = {
        connection,
        lastUsedAt: Date.now(),
        disposed: false
      };

      connection.onClose(() => {
        if (runtime.disposed) return;
        runtime.disposed = true;
        if (runtime.idleTimer) { clearTimeout(runtime.idleTimer); runtime.idleTimer = undefined; }
        this.adhocSessionRuntimes.delete(connectionId);
        this.adhocSessionPromises.delete(connectionId);
        logger.info("[AdhocSession] hidden SSH disconnected", { connectionId });
      });

      this.adhocSessionRuntimes.set(connectionId, runtime);
      this.resetAdhocIdleTimer(connectionId, runtime);

      logger.info("[AdhocSession] runtime ready", { connectionId });
      return runtime;
    })();

    this.adhocSessionPromises.set(connectionId, promise);
    try {
      return await promise;
    } catch (error) {
      await this.disposeAdhocSession(connectionId);
      throw error;
    } finally {
      this.adhocSessionPromises.delete(connectionId);
    }
  }

  // ─── Public API: System Monitor ─────────────────────────────────────────

  async startSystemMonitor(
    connectionId: string,
    sender: WebContents
  ): Promise<{ ok: true }> {
    this.assertMonitorEnabled(connectionId);
    this.assertVisibleTerminalAlive(connectionId);
    const runtime = await this.ensureSystemMonitorRuntime(connectionId);
    runtime.sender = sender;
    return runtime.controller.start();
  }

  stopSystemMonitor(connectionId: string): { ok: true } {
    const runtime = this.systemMonitorRuntimes.get(connectionId);
    if (runtime) {
      runtime.sender = undefined;
      void runtime.controller.stop();
    }
    return { ok: true };
  }

  async selectSystemNetworkInterface(
    connectionId: string,
    networkInterface: string
  ): Promise<{ ok: true }> {
    this.assertMonitorEnabled(connectionId);
    this.assertVisibleTerminalAlive(connectionId);
    const runtime = await this.ensureSystemMonitorRuntime(connectionId);
    return runtime.controller.selectNetworkInterface(networkInterface);
  }

  // ─── Public API: System Info (ad-hoc) ───────────────────────────────────

  private async assertSystemInfoLinuxHost(connectionId: string): Promise<void> {
    // Use ad-hoc session for one-off checks
    const adhoc = await this.ensureAdhocSession(connectionId);
    const result = await adhoc.connection.exec(MONITOR_SYSTEM_INFO_KERNEL_NAME_COMMAND);
    const platform = result.stdout.trim().split(/\s+/)[0] ?? "";
    if (platform !== "Linux") {
      throw new Error("系统信息标签页当前仅支持 Linux 主机");
    }
  }

  async getSystemInfoSnapshot(connectionId: string): Promise<SystemInfoSnapshot> {
    this.assertMonitorEnabled(connectionId);
    this.assertVisibleTerminalAlive(connectionId);
    await this.assertSystemInfoLinuxHost(connectionId);

    // Use ad-hoc session with compound command (9 commands → 1 exec)
    const adhoc = await this.ensureAdhocSession(connectionId);
    const compoundCmd = buildSystemInfoCommand();
    const result = await adhoc.connection.exec(compoundCmd);
    const sections = parseCompoundOutput(result.stdout);

    const memInfoRaw = sections.get("MEMINFO") ?? "";
    const totals = parseMeminfoTotals(memInfoRaw);
    return {
      connectionId,
      hostname: (sections.get("HOSTNAME") ?? "").trim() || "unknown",
      osName: parseOsReleaseName(sections.get("OSRELEASE") ?? ""),
      kernelName: (sections.get("KERNELNAME") ?? "").trim() || "Linux",
      kernelVersion: (sections.get("KERNELVER") ?? "").trim() || "unknown",
      architecture: (sections.get("ARCH") ?? "").trim() || "unknown",
      cpu: parseCpuInfo(sections.get("CPUINFO") ?? ""),
      memoryTotalKb: totals.memoryTotalKb,
      swapTotalKb: totals.swapTotalKb,
      networkInterfaces: parseNetworkInterfaceTotals(sections.get("NETDEV") ?? ""),
      filesystems: parseFilesystemEntries(sections.get("FILESYSTEMS") ?? ""),
      uptimeSeconds: parseUptimeSeconds(sections.get("UPTIME") ?? ""),
      capturedAt: new Date().toISOString()
    };
  }

  // ─── Public API: Process Monitor ────────────────────────────────────────

  async startProcessMonitor(
    connectionId: string,
    sender: WebContents
  ): Promise<{ ok: true }> {
    this.assertMonitorEnabled(connectionId);
    this.assertVisibleTerminalAlive(connectionId);

    const runtime = await this.ensureProcessMonitorRuntime(connectionId);
    runtime.sender = sender;
    return runtime.controller.start();
  }

  stopProcessMonitor(connectionId: string): { ok: true } {
    const runtime = this.processMonitorRuntimes.get(connectionId);
    if (runtime) {
      runtime.sender = undefined;
      void runtime.controller.stop();
    }
    return { ok: true };
  }

  async getProcessDetail(
    connectionId: string,
    pid: number
  ): Promise<ProcessDetailSnapshot> {
    this.assertMonitorEnabled(connectionId);
    this.assertVisibleTerminalAlive(connectionId);

    // Use ad-hoc session for on-demand detail queries
    const adhoc = await this.ensureAdhocSession(connectionId);

    const normalizedPid = Math.trunc(pid);
    if (normalizedPid < 1) {
      throw new Error("无效进程 PID");
    }

    const primaryCommand =
      `ps -p ${normalizedPid} -o pid=,ppid=,user=,state=,%cpu=,%mem=,rss=,etime=,comm=`;
    const argsCommand = `ps -p ${normalizedPid} -o args=`;

    const primary = await adhoc.connection.exec(primaryCommand);
    if (primary.exitCode !== 0) {
      throw new Error("进程不存在或已结束");
    }

    const parsed = parseProcessDetailPrimary(connectionId, primary.stdout);
    if (!parsed) {
      throw new Error("进程不存在或已结束");
    }

    const args = await adhoc.connection.exec(argsCommand);
    const commandLine = args.exitCode === 0
      ? firstNonEmptyLine(args.stdout) ?? parsed.command
      : parsed.command;

    return {
      ...parsed,
      commandLine,
      capturedAt: new Date().toISOString()
    };
  }

  async killRemoteProcess(
    connectionId: string,
    pid: number,
    signal: "SIGTERM" | "SIGKILL"
  ): Promise<{ ok: true }> {
    this.assertMonitorEnabled(connectionId);
    this.assertVisibleTerminalAlive(connectionId);

    // Use ad-hoc session for kill commands
    const adhoc = await this.ensureAdhocSession(connectionId);
    if (signal !== "SIGTERM" && signal !== "SIGKILL") {
      throw new Error("Invalid signal");
    }
    const result = await adhoc.connection.exec(`kill -${signal} ${pid} 2>&1`);
    if (result.exitCode !== 0) {
      throw new Error(`kill 失败 (exit ${result.exitCode}): ${result.stdout.trim() || "unknown error"}`);
    }
    this.appendAuditLogIfEnabled({
      action: "monitor.process_kill",
      level: "warn",
      connectionId,
      message: `Sent ${signal} to PID ${pid}`,
      metadata: { pid, signal }
    });
    return { ok: true };
  }

  // ─── Public API: Network Monitor ────────────────────────────────────────

  async startNetworkMonitor(
    connectionId: string,
    sender: WebContents
  ): Promise<{ ok: true }> {
    this.assertMonitorEnabled(connectionId);
    this.assertVisibleTerminalAlive(connectionId);

    const runtime = await this.ensureNetworkMonitorRuntime(connectionId);
    runtime.sender = sender;
    return runtime.controller.start();
  }

  stopNetworkMonitor(connectionId: string): { ok: true } {
    const runtime = this.networkMonitorRuntimes.get(connectionId);
    if (runtime) {
      runtime.sender = undefined;
      void runtime.controller.stop();
    }
    return { ok: true };
  }

  async getNetworkConnections(
    connectionId: string,
    port: number
  ): Promise<NetworkConnection[]> {
    this.assertMonitorEnabled(connectionId);
    this.assertVisibleTerminalAlive(connectionId);

    const runtime = await this.ensureNetworkMonitorRuntime(connectionId);
    return runtime.controller.getConnectionsByPort(port);
  }
}
