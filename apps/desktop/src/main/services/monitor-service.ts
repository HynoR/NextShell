import { randomUUID } from "node:crypto";
import type { WebContents } from "electron";
import type {
  ConnectionProfile,
  MonitorSnapshot,
  NetworkConnection,
  NetworkSnapshot,
  ProcessDetailSnapshot,
  ProcessSnapshot,
  SystemInfoSnapshot
} from "../../../../../packages/core/src/index";
import { SshConnection, type SshConnectOptions } from "../../../../../packages/ssh/src/index";
import type { DebugLogEntry } from "../../../../../packages/shared/src/index";
import type { CachedConnectionRepository } from "../../../../../packages/storage/src/index";
import {
  SystemMonitorController,
  type MonitorSelectionState,
  type ProbeExecutionLog
} from "./monitor/system-monitor-controller";
import {
  ProcessMonitorController,
  type ProcessProbeExecutionLog
} from "./monitor/process-monitor-controller";
import { firstNonEmptyLine, parseProcessDetailPrimary } from "./monitor/process-probe-parser";
import {
  NetworkMonitorController,
  type NetworkProbeExecutionLog,
  type NetworkTool
} from "./monitor/network-monitor-controller";
import {
  parseCpuInfo,
  parseFilesystemEntries,
  parseMeminfoTotals,
  parseNetworkInterfaceTotals,
  parseOsReleaseName
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
  buildSystemInfoCommand
} from "./container-utils";
import type {
  ActiveSession,
  AdhocSessionRuntime,
  MonitorState,
  SystemMonitorRuntime,
  ProcessMonitorRuntime,
  NetworkMonitorRuntime
} from "./container-types";
import { MonitorBackoff } from "./monitor/monitor-runner";
import {
  LEGACY_MONITOR_SUBSCRIBER_ID,
  MonitorSubscriberRegistry
} from "./monitor/monitor-subscribers";
import { logger } from "../logger";

// Hidden monitor SSH re-establishment backs off from the FIRST failure so a
// dead network (e.g. right after OS sleep) doesn't trigger a reconnect storm.
const HIDDEN_CONNECT_BACKOFF_BASE_MS = 5_000;
const HIDDEN_CONNECT_BACKOFF_MAX_MS = 300_000; // cap at 5 minutes
const HIDDEN_MONITOR_TAGS = ["SystemMonitor", "ProcessMonitor", "NetworkMonitor"] as const;

/**
 * Grace period between a system monitor losing its last subscriber and the
 * hidden SSH session actually being torn down.
 *
 * The sidebar monitor follows the *active connection*, so ordinary tab flipping
 * (A → B → A within seconds) unsubscribes A and re-subscribes it right after.
 * Stopping on the spot closed A's hidden SSH connection, and coming back had to
 * redial, re-handshake and re-warm the probe baselines — seconds of an empty
 * panel for a detour the user experienced as instant. Keeping the session warm
 * for one human detour costs ~1 probe/s against a host we are still connected to
 * anyway; anything longer is a real "left this host" and gets stopped.
 *
 * This is a *demand* delay only. Teardown paths (visible terminal gone,
 * connection removed, renderer destroyed/reloaded, app shutdown, legacy
 * connection-level stop) must never linger: they cancel a pending timer and stop
 * immediately.
 */
const SYSTEM_MONITOR_LINGER_MS = 30_000;

/**
 * One in-flight hidden-SSH establish attempt.
 *
 * Cancellation is bound to the attempt object instead of the connection id, so
 * a close that happens while attempt N is dialing can never kill attempt N+1
 * that a different subscriber started right afterwards.
 */
interface HiddenConnectAttempt {
  cancelled: boolean;
}

// ─── Options ────────────────────────────────────────────────────────────────

export interface MonitorServiceOptions {
  connections: CachedConnectionRepository;
  getConnectionOrThrow: (id: string) => ConnectionProfile;
  resolveConnectOptions: (profile: ConnectionProfile) => Promise<SshConnectOptions>;
  activeSessions: Map<string, ActiveSession>;
  debugSenders: Set<WebContents>;
  emitDebugLog: (entry: DebugLogEntry) => void;
  /** Direct guarded webContents.send of a snapshot payload (no ack protocol). */
  emitSystemSnapshot: (sender: WebContents, snapshot: MonitorSnapshot) => void;
  emitProcessSnapshot: (sender: WebContents, snapshot: ProcessSnapshot) => void;
  emitNetworkSnapshot: (sender: WebContents, snapshot: NetworkSnapshot) => void;
  /** Override for `SYSTEM_MONITOR_LINGER_MS` (tests only). */
  systemMonitorLingerMs?: number;
}

// ─── Service ────────────────────────────────────────────────────────────────

export class MonitorService {
  // ─── Maps ───────────────────────────────────────────────────────────────
  private readonly systemMonitorRuntimes = new Map<string, SystemMonitorRuntime>();
  private readonly systemMonitorConnections = new Map<string, SshConnection>();
  private readonly systemMonitorConnectionPromises = new Map<string, Promise<SshConnection>>();
  private readonly systemMonitorConnectAttempts = new Map<string, HiddenConnectAttempt>();

  private readonly processMonitorRuntimes = new Map<string, ProcessMonitorRuntime>();
  private readonly processMonitorPromises = new Map<string, Promise<ProcessMonitorRuntime>>();
  private readonly processMonitorConnections = new Map<string, SshConnection>();
  private readonly processMonitorConnectionPromises = new Map<string, Promise<SshConnection>>();
  private readonly processMonitorConnectAttempts = new Map<string, HiddenConnectAttempt>();

  private readonly networkMonitorRuntimes = new Map<string, NetworkMonitorRuntime>();
  private readonly networkMonitorPromises = new Map<string, Promise<NetworkMonitorRuntime>>();
  private readonly networkMonitorConnections = new Map<string, SshConnection>();
  private readonly networkMonitorConnectionPromises = new Map<string, Promise<SshConnection>>();
  private readonly networkMonitorConnectAttempts = new Map<string, HiddenConnectAttempt>();

  private readonly adhocSessionRuntimes = new Map<string, AdhocSessionRuntime>();
  private readonly adhocSessionPromises = new Map<string, Promise<AdhocSessionRuntime>>();

  // ─── Subscribers ──────────────────────────────────────────────────────────
  // Runtime/hidden connection stay pooled per connection; the *demand* is
  // reference counted per subscriber (= renderer session id), so closing one
  // pane no longer kills the monitors of the other tabs on the same host.
  private readonly systemMonitorSubscribers = new MonitorSubscriberRegistry<WebContents>();
  /**
   * Connections whose system monitor lost its last subscriber and is waiting out
   * `systemMonitorLingerMs` before the hidden SSH session is really closed. A
   * pending entry also counts as "demand" for the controller, which is what keeps
   * its poll loop from stopping itself while nobody is listening.
   */
  private readonly systemMonitorLingerTimers = new Map<string, ReturnType<typeof setTimeout>>();
  private readonly processMonitorSubscribers = new MonitorSubscriberRegistry<WebContents>();
  private readonly networkMonitorSubscribers = new MonitorSubscriberRegistry<WebContents>();
  /** Renderers already hooked for reload/destroy purges (see `watchSender`). */
  private readonly watchedSenders = new WeakSet<WebContents>();

  /** Serializes start/stop per "<kind>:<connectionId>" so a stop can never
   * interleave with a concurrent start of the same monitor. */
  private readonly monitorOpChains = new Map<string, Promise<void>>();

  readonly monitorStates = new Map<string, MonitorState>();
  // NOTE: selection state (network interface) and the network tool cache stay
  // per-connection on purpose — every session of a host shares one hidden SSH
  // connection and one controller, so a per-session selection is not
  // representable without one hidden connection per session.
  private readonly networkToolCache = new Map<string, NetworkTool>();

  /** Per hidden-connection ("<tag>:<connectionId>") reconnect backoff. */
  private readonly hiddenConnectionBackoffs = new Map<string, MonitorBackoff>();
  /** Guards against registering onClose twice on the same SshConnection object. */
  private readonly closeListenerRegistered = new WeakSet<SshConnection>();

  // ─── Injected dependencies ──────────────────────────────────────────────
  private readonly connections: CachedConnectionRepository;
  private readonly getConnectionOrThrow: (id: string) => ConnectionProfile;
  private readonly resolveConnectOptions: (
    profile: ConnectionProfile
  ) => Promise<SshConnectOptions>;
  private readonly activeSessions: Map<string, ActiveSession>;
  private readonly debugSenders: Set<WebContents>;
  private readonly emitDebugLog: (entry: DebugLogEntry) => void;
  private readonly emitSystemSnapshot: (sender: WebContents, snapshot: MonitorSnapshot) => void;
  private readonly emitProcessSnapshot: (sender: WebContents, snapshot: ProcessSnapshot) => void;
  private readonly emitNetworkSnapshot: (sender: WebContents, snapshot: NetworkSnapshot) => void;
  private readonly systemMonitorLingerMs: number;

  constructor(options: MonitorServiceOptions) {
    this.connections = options.connections;
    this.getConnectionOrThrow = options.getConnectionOrThrow;
    this.resolveConnectOptions = options.resolveConnectOptions;
    this.activeSessions = options.activeSessions;
    this.debugSenders = options.debugSenders;
    this.emitDebugLog = options.emitDebugLog;
    this.emitSystemSnapshot = options.emitSystemSnapshot;
    this.emitProcessSnapshot = options.emitProcessSnapshot;
    this.emitNetworkSnapshot = options.emitNetworkSnapshot;
    this.systemMonitorLingerMs = options.systemMonitorLingerMs ?? SYSTEM_MONITOR_LINGER_MS;
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

  /**
   * Watch a renderer so its subscriptions die with its page.
   *
   * `webContents.reload()` — which main performs by itself on `unresponsive` /
   * `render-process-gone` — keeps the same `WebContents` object, so the old
   * page's subscriber ids stay registered while nothing will ever send their
   * `stop`. They would then keep `remove()` from ever reporting the monitor as
   * idle, pinning a hidden SSH connection and its ~1Hz probes for the lifetime
   * of the process. Liveness polling cannot see this: the sender is alive.
   */
  private watchSender(sender: WebContents): void {
    if (this.watchedSenders.has(sender)) {
      return;
    }
    this.watchedSenders.add(sender);

    const purge = (): void => {
      void this.purgeSender(sender).catch(() => undefined);
    };

    sender.once("destroyed", purge);
    sender.on("did-start-navigation", (details) => {
      // Same-document navigations (hash changes, history API) keep the page —
      // and its subscriptions — alive.
      if (details.isMainFrame && !details.isSameDocument) {
        purge();
      }
    });
  }

  /** Drop every subscription of one renderer and stop what it kept alive. */
  private async purgeSender(sender: WebContents): Promise<void> {
    const systemIdle = this.systemMonitorSubscribers.removeSender(sender);
    const processIdle = this.processMonitorSubscribers.removeSender(sender);
    const networkIdle = this.networkMonitorSubscribers.removeSender(sender);

    // A page that is gone can never come back to a lingering monitor, so its
    // grace period is void: sweep those connections here too — `removeSender`
    // cannot report them, they have had no subscribers since the switch away.
    const systemToStop = Array.from(new Set([...systemIdle, ...this.lingeringSystemMonitorIds()]));

    // Each hard stop goes through the per-monitor chain, so a start racing the
    // reload is serialized against it instead of being torn down half-way.
    await Promise.all([
      ...systemToStop.map((connectionId) =>
        this.runExclusive(`system:${connectionId}`, async () => {
          if (this.systemMonitorSubscribers.count(connectionId) > 0) return;
          this.cancelSystemMonitorLinger(connectionId);
          await this.hardStopSystemMonitor(connectionId);
        })
      ),
      ...processIdle.map((connectionId) =>
        this.runExclusive(`process:${connectionId}`, async () => {
          if (this.processMonitorSubscribers.count(connectionId) > 0) return;
          await this.hardStopProcessMonitor(connectionId);
        })
      ),
      ...networkIdle.map((connectionId) =>
        this.runExclusive(`network:${connectionId}`, async () => {
          if (this.networkMonitorSubscribers.count(connectionId) > 0) return;
          await this.hardStopNetworkMonitor(connectionId);
        })
      )
    ]);
  }

  /** Live, de-duplicated renderers of a monitor; prunes dead subscribers. */
  private liveSubscriberSenders(
    registry: MonitorSubscriberRegistry<WebContents>,
    connectionId: string
  ): WebContents[] {
    return registry.pruneDead(connectionId, (sender) => this.isSenderAlive(sender));
  }

  /** Run `task` after every previously queued task for the same key. */
  private runExclusive<T>(key: string, task: () => Promise<T>): Promise<T> {
    const previous = this.monitorOpChains.get(key) ?? Promise.resolve();
    const run = previous.then(task, task);
    const guard = run.then(
      () => undefined,
      () => undefined
    );
    this.monitorOpChains.set(key, guard);
    void guard.then(() => {
      if (this.monitorOpChains.get(key) === guard) {
        this.monitorOpChains.delete(key);
      }
    });
    return run;
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
    // Teardown, not a demand change: kill the grace period with the runtime.
    this.cancelSystemMonitorLinger(connectionId);
    this.systemMonitorSubscribers.clear(connectionId);
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
    this.processMonitorSubscribers.clear(connectionId);
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
    this.networkMonitorSubscribers.clear(connectionId);
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

      try {
        await runtime.connection.close();
      } catch (error) {
        logger.warn("[AdhocSession] failed to close connection", {
          connectionId,
          reason: normalizeError(error)
        });
      }
    }
    this.adhocSessionPromises.delete(connectionId);
  }

  /** Return all connection IDs that have any active monitor/adhoc state. */
  getAllConnectionIds(): string[] {
    const ids = new Set<string>();
    for (const id of this.systemMonitorLingerTimers.keys()) ids.add(id);
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
        Number.POSITIVE_INFINITY
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
    const backoff =
      (options?.backoff ?? true) ? this.getHiddenConnectionBackoff(connectionId, tag) : undefined;
    if (backoff?.isActive()) {
      throw new Error(`${tag} 重连退避中，${Math.ceil(backoff.remainingMs() / 1000)}s 后重试`);
    }

    const profile = this.assertMonitorEnabled(connectionId);
    logger.info(`[${tag}] connecting hidden SSH`, {
      connectionId,
      host: profile.host,
      port: profile.port
    });
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
          reason: normalizeError(error)
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
    // Cancel only the attempt that is dialing right now; an attempt started
    // after this close must not inherit a stale cancel flag (#6).
    const attempt = this.systemMonitorConnectAttempts.get(connectionId);
    if (attempt) {
      attempt.cancelled = true;
      this.systemMonitorConnectAttempts.delete(connectionId);
    }
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
        reason: normalizeError(error)
      });
    }
  }

  private async ensureSystemMonitorConnection(connectionId: string): Promise<SshConnection> {
    const existing = this.systemMonitorConnections.get(connectionId);
    if (existing) {
      return existing;
    }

    const pending = this.systemMonitorConnectionPromises.get(connectionId);
    if (pending) {
      return pending;
    }

    const attempt: HiddenConnectAttempt = { cancelled: false };
    const promise = (async () => {
      const connection = await this.establishHiddenConnection(connectionId, "SystemMonitor");
      if (attempt.cancelled) {
        try {
          await connection.close();
        } catch {
          /* ignore */
        }
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
    this.systemMonitorConnectAttempts.set(connectionId, attempt);
    try {
      return await promise;
    } finally {
      if (this.systemMonitorConnectionPromises.get(connectionId) === promise) {
        this.systemMonitorConnectionPromises.delete(connectionId);
      }
      if (this.systemMonitorConnectAttempts.get(connectionId) === attempt) {
        this.systemMonitorConnectAttempts.delete(connectionId);
      }
    }
  }

  // ─── Process Monitor connection ─────────────────────────────────────────

  private async closeProcessMonitorConnection(connectionId: string): Promise<void> {
    // See closeSystemMonitorConnection: cancel binds to the in-flight attempt.
    const attempt = this.processMonitorConnectAttempts.get(connectionId);
    if (attempt) {
      attempt.cancelled = true;
      this.processMonitorConnectAttempts.delete(connectionId);
    }
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
        reason: normalizeError(error)
      });
    }
  }

  private async ensureProcessMonitorConnection(connectionId: string): Promise<SshConnection> {
    const existing = this.processMonitorConnections.get(connectionId);
    if (existing) {
      return existing;
    }

    const pending = this.processMonitorConnectionPromises.get(connectionId);
    if (pending) {
      return pending;
    }

    const attempt: HiddenConnectAttempt = { cancelled: false };
    const promise = (async () => {
      const connection = await this.establishHiddenConnection(connectionId, "ProcessMonitor");
      if (attempt.cancelled) {
        try {
          await connection.close();
        } catch {
          /* ignore */
        }
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
    this.processMonitorConnectAttempts.set(connectionId, attempt);
    try {
      return await promise;
    } finally {
      if (this.processMonitorConnectionPromises.get(connectionId) === promise) {
        this.processMonitorConnectionPromises.delete(connectionId);
      }
      if (this.processMonitorConnectAttempts.get(connectionId) === attempt) {
        this.processMonitorConnectAttempts.delete(connectionId);
      }
    }
  }

  // ─── Network Monitor connection ─────────────────────────────────────────

  private async closeNetworkMonitorConnection(connectionId: string): Promise<void> {
    // See closeSystemMonitorConnection: cancel binds to the in-flight attempt.
    const attempt = this.networkMonitorConnectAttempts.get(connectionId);
    if (attempt) {
      attempt.cancelled = true;
      this.networkMonitorConnectAttempts.delete(connectionId);
    }
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
        reason: normalizeError(error)
      });
    }
  }

  private async ensureNetworkMonitorConnection(connectionId: string): Promise<SshConnection> {
    const existing = this.networkMonitorConnections.get(connectionId);
    if (existing) {
      return existing;
    }

    const pending = this.networkMonitorConnectionPromises.get(connectionId);
    if (pending) {
      return pending;
    }

    const attempt: HiddenConnectAttempt = { cancelled: false };
    const promise = (async () => {
      const connection = await this.establishHiddenConnection(connectionId, "NetworkMonitor");
      if (attempt.cancelled) {
        try {
          await connection.close();
        } catch {
          /* ignore */
        }
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
    this.networkMonitorConnectAttempts.set(connectionId, attempt);
    try {
      return await promise;
    } finally {
      if (this.networkMonitorConnectionPromises.get(connectionId) === promise) {
        this.networkMonitorConnectionPromises.delete(connectionId);
      }
      if (this.networkMonitorConnectAttempts.get(connectionId) === attempt) {
        this.networkMonitorConnectAttempts.delete(connectionId);
      }
    }
  }

  // ─── Session ① System Monitor: ensure ───────────────────────────────────

  async ensureSystemMonitorRuntime(connectionId: string): Promise<SystemMonitorRuntime> {
    const existing = this.systemMonitorRuntimes.get(connectionId);
    if (existing && !existing.disposed) {
      return existing;
    }

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
          error: entry.error
        });
      }

      if (!entry.ok && entry.exitCode >= 0) {
        logger.debug("[SystemMonitor] command non-zero exit", {
          connectionId,
          command: entry.command,
          exitCode: entry.exitCode,
          output: entry.stdout.slice(0, 200)
        });
      }
    };

    const controller = new SystemMonitorController({
      connectionId,
      getConnection: () => this.ensureSystemMonitorConnection(connectionId),
      closeConnection: () => this.closeSystemMonitorConnection(connectionId),
      isVisibleTerminalAlive: () => this.hasVisibleTerminalAlive(connectionId),
      isReceiverAlive: () => this.hasSystemMonitorDemand(connectionId),
      emitSnapshot: (snapshot) => {
        for (const sender of this.liveSubscriberSenders(
          this.systemMonitorSubscribers,
          connectionId
        )) {
          this.emitSystemSnapshot(sender, snapshot);
        }
      },
      readSelection: () => this.monitorStates.get(connectionId),
      writeSelection: (state: MonitorSelectionState) => {
        const previous = this.monitorStates.get(connectionId);
        this.monitorStates.set(connectionId, { ...previous, ...state });
      },
      logger,
      onProbeExecution
    });

    // `sender` is intentionally left unset: receivers are tracked in
    // systemMonitorSubscribers (one runtime, many subscribers).
    const runtime: SystemMonitorRuntime = {
      disposed: false,
      controller,
      sender: undefined
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
            error: entry.error
          });
        }
      };

      const controller = new ProcessMonitorController({
        connectionId,
        getConnection: () => this.ensureProcessMonitorConnection(connectionId),
        closeConnection: () => this.closeProcessMonitorConnection(connectionId),
        isVisibleTerminalAlive: () => this.hasVisibleTerminalAlive(connectionId),
        isReceiverAlive: () =>
          this.liveSubscriberSenders(this.processMonitorSubscribers, connectionId).length > 0,
        emitSnapshot: (snapshot) => {
          for (const sender of this.liveSubscriberSenders(
            this.processMonitorSubscribers,
            connectionId
          )) {
            this.emitProcessSnapshot(sender, snapshot);
          }
        },
        logger,
        onProbeExecution,
        timing: {
          pollIntervalMs: MONITOR_PROCESS_INTERVAL_MS,
          execTimeoutMs: MONITOR_COMMAND_TIMEOUT_MS,
          maxConsecutiveFailures: MONITOR_MAX_CONSECUTIVE_FAILURES
        }
      });

      // `sender` unused: receivers live in processMonitorSubscribers.
      const runtime: ProcessMonitorRuntime = {
        controller,
        sender: undefined,
        disposed: false
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
            error: entry.error
          });
        }
      };

      const controller = new NetworkMonitorController({
        connectionId,
        getConnection: () => this.ensureNetworkMonitorConnection(connectionId),
        closeConnection: () => this.closeNetworkMonitorConnection(connectionId),
        isVisibleTerminalAlive: () => this.hasVisibleTerminalAlive(connectionId),
        isReceiverAlive: () =>
          this.liveSubscriberSenders(this.networkMonitorSubscribers, connectionId).length > 0,
        emitSnapshot: (snapshot) => {
          for (const sender of this.liveSubscriberSenders(
            this.networkMonitorSubscribers,
            connectionId
          )) {
            this.emitNetworkSnapshot(sender, snapshot);
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
          maxConsecutiveFailures: MONITOR_MAX_CONSECUTIVE_FAILURES
        }
      });

      // `sender` unused: receivers live in networkMonitorSubscribers.
      const runtime: NetworkMonitorRuntime = {
        controller,
        sender: undefined,
        disposed: false
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
      const connection = await this.establishHiddenConnection(connectionId, "AdhocSession", {
        backoff: false
      });

      const runtime: AdhocSessionRuntime = {
        connection,
        lastUsedAt: Date.now(),
        disposed: false
      };

      connection.onClose(() => {
        if (runtime.disposed) return;
        runtime.disposed = true;
        if (runtime.idleTimer) {
          clearTimeout(runtime.idleTimer);
          runtime.idleTimer = undefined;
        }
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

  /**
   * @param subscriberId renderer session id; omitted by legacy callers, which
   * then share the single connection-level subscriber slot.
   */
  async startSystemMonitor(
    connectionId: string,
    sender: WebContents,
    subscriberId?: string
  ): Promise<{ ok: true }> {
    this.assertMonitorEnabled(connectionId);
    this.assertVisibleTerminalAlive(connectionId);
    const id = subscriberId ?? LEGACY_MONITOR_SUBSCRIBER_ID;
    return this.runExclusive(`system:${connectionId}`, async () => {
      // Demand is back: disarm the pending teardown before touching the runtime,
      // so a monitor that is still RUNNING from the linger window is reused as-is
      // (controller.start() is a no-op then — no second SSH dial).
      this.cancelSystemMonitorLinger(connectionId);
      // Register before starting so the controller never sees "no receiver".
      this.watchSender(sender);
      this.systemMonitorSubscribers.add(connectionId, id, sender);
      try {
        const runtime = await this.ensureSystemMonitorRuntime(connectionId);
        return await runtime.controller.start();
      } catch (error) {
        if (this.systemMonitorSubscribers.remove(connectionId, id)) {
          this.cancelSystemMonitorLinger(connectionId);
          await this.hardStopSystemMonitor(connectionId);
        }
        throw error;
      }
    });
  }

  /**
   * @param subscriberId renderer session id. When omitted the call keeps the
   * legacy connection-level semantics and drops every subscriber — that variant
   * is a teardown, so it never lingers.
   */
  async stopSystemMonitor(connectionId: string, subscriberId?: string): Promise<{ ok: true }> {
    return this.runExclusive(`system:${connectionId}`, async () => {
      if (!subscriberId) {
        this.systemMonitorSubscribers.clear(connectionId);
        this.cancelSystemMonitorLinger(connectionId);
        await this.hardStopSystemMonitor(connectionId);
        return { ok: true } as const;
      }

      const idle = this.systemMonitorSubscribers.remove(connectionId, subscriberId);
      if (!idle) {
        return { ok: true } as const;
      }

      // Lingering only pays off when there is a warm runtime to keep *and* the
      // host still has a visible terminal — a re-subscribe could not start the
      // monitor without one, so there would be nothing to come back to.
      const runtime = this.systemMonitorRuntimes.get(connectionId);
      if (!runtime || runtime.disposed || !this.hasVisibleTerminalAlive(connectionId)) {
        this.cancelSystemMonitorLinger(connectionId);
        await this.hardStopSystemMonitor(connectionId);
        return { ok: true } as const;
      }

      this.scheduleSystemMonitorLinger(connectionId);
      return { ok: true } as const;
    });
  }

  /**
   * Does anything still want this system monitor's snapshots?
   *
   * A pending linger timer counts: the controller stops itself as soon as it
   * sees no receiver, which would close the hidden SSH connection one poll tick
   * into the grace period and defeat the whole point. Snapshots produced while
   * lingering are emitted to the (empty) subscriber list, i.e. dropped.
   */
  private hasSystemMonitorDemand(connectionId: string): boolean {
    if (this.liveSubscriberSenders(this.systemMonitorSubscribers, connectionId).length > 0) {
      return true;
    }
    return this.systemMonitorLingerTimers.has(connectionId);
  }

  /**
   * Arm the delayed hard stop for an idle system monitor.
   *
   * Only the *timer* is created here (inside the caller's `runExclusive` turn);
   * the stop itself re-enters `runExclusive("system:<id>")` when the timer fires,
   * so it can never interleave with a start. The fire-time re-check of the
   * subscriber count is what makes a lost cancellation race harmless: whoever
   * subscribed meanwhile is already registered by the time our turn runs.
   */
  private scheduleSystemMonitorLinger(connectionId: string): void {
    this.cancelSystemMonitorLinger(connectionId);

    const timer = setTimeout(() => {
      if (this.systemMonitorLingerTimers.get(connectionId) !== timer) {
        return;
      }
      this.systemMonitorLingerTimers.delete(connectionId);

      void this.runExclusive(`system:${connectionId}`, async () => {
        // Someone came back inside the window, or a newer linger owns the
        // connection now — either way this timer is no longer in charge.
        if (this.systemMonitorSubscribers.count(connectionId) > 0) {
          return;
        }
        if (this.systemMonitorLingerTimers.has(connectionId)) {
          return;
        }
        logger.info("[SystemMonitor] linger expired, stopping idle monitor", { connectionId });
        await this.hardStopSystemMonitor(connectionId);
      }).catch((error) => {
        logger.warn("[SystemMonitor] delayed idle stop failed", {
          connectionId,
          reason: normalizeError(error)
        });
      });
    }, this.systemMonitorLingerMs);

    this.systemMonitorLingerTimers.set(connectionId, timer);
  }

  /** Disarm a pending linger; every teardown path must call this. */
  private cancelSystemMonitorLinger(connectionId: string): void {
    const timer = this.systemMonitorLingerTimers.get(connectionId);
    if (!timer) {
      return;
    }
    clearTimeout(timer);
    this.systemMonitorLingerTimers.delete(connectionId);
  }

  /** Connections currently waiting out their linger window (no subscribers). */
  private lingeringSystemMonitorIds(): string[] {
    return Array.from(this.systemMonitorLingerTimers.keys());
  }

  /** Really stop the controller and close the hidden SSH connection. */
  private async hardStopSystemMonitor(connectionId: string): Promise<void> {
    const runtime = this.systemMonitorRuntimes.get(connectionId);
    if (runtime) {
      runtime.sender = undefined;
      await runtime.controller.stop();
    }
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

  /** @param subscriberId renderer session id (see startSystemMonitor). */
  async startProcessMonitor(
    connectionId: string,
    sender: WebContents,
    subscriberId?: string
  ): Promise<{ ok: true }> {
    this.assertMonitorEnabled(connectionId);
    this.assertVisibleTerminalAlive(connectionId);

    const id = subscriberId ?? LEGACY_MONITOR_SUBSCRIBER_ID;
    return this.runExclusive(`process:${connectionId}`, async () => {
      this.watchSender(sender);
      this.processMonitorSubscribers.add(connectionId, id, sender);
      try {
        const runtime = await this.ensureProcessMonitorRuntime(connectionId);
        return await runtime.controller.start();
      } catch (error) {
        if (this.processMonitorSubscribers.remove(connectionId, id)) {
          await this.hardStopProcessMonitor(connectionId);
        }
        throw error;
      }
    });
  }

  /** @param subscriberId renderer session id (see stopSystemMonitor). */
  async stopProcessMonitor(connectionId: string, subscriberId?: string): Promise<{ ok: true }> {
    return this.runExclusive(`process:${connectionId}`, async () => {
      let idle: boolean;
      if (subscriberId) {
        idle = this.processMonitorSubscribers.remove(connectionId, subscriberId);
      } else {
        this.processMonitorSubscribers.clear(connectionId);
        idle = true;
      }
      if (!idle) {
        return { ok: true } as const;
      }
      await this.hardStopProcessMonitor(connectionId);
      return { ok: true } as const;
    });
  }

  private async hardStopProcessMonitor(connectionId: string): Promise<void> {
    const runtime = this.processMonitorRuntimes.get(connectionId);
    if (runtime) {
      runtime.sender = undefined;
      await runtime.controller.stop();
    }
  }

  async getProcessDetail(connectionId: string, pid: number): Promise<ProcessDetailSnapshot> {
    this.assertMonitorEnabled(connectionId);
    this.assertVisibleTerminalAlive(connectionId);

    // Use ad-hoc session for on-demand detail queries
    const adhoc = await this.ensureAdhocSession(connectionId);

    const normalizedPid = Math.trunc(pid);
    if (normalizedPid < 1) {
      throw new Error("无效进程 PID");
    }

    const primaryCommand = `ps -p ${normalizedPid} -o pid=,ppid=,user=,state=,%cpu=,%mem=,rss=,etime=,comm=`;
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
    const commandLine =
      args.exitCode === 0 ? (firstNonEmptyLine(args.stdout) ?? parsed.command) : parsed.command;

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
      throw new Error(
        `kill 失败 (exit ${result.exitCode}): ${result.stdout.trim() || "unknown error"}`
      );
    }
    return { ok: true };
  }

  // ─── Public API: Network Monitor ────────────────────────────────────────

  /** @param subscriberId renderer session id (see startSystemMonitor). */
  async startNetworkMonitor(
    connectionId: string,
    sender: WebContents,
    subscriberId?: string
  ): Promise<{ ok: true }> {
    this.assertMonitorEnabled(connectionId);
    this.assertVisibleTerminalAlive(connectionId);

    const id = subscriberId ?? LEGACY_MONITOR_SUBSCRIBER_ID;
    return this.runExclusive(`network:${connectionId}`, async () => {
      this.watchSender(sender);
      this.networkMonitorSubscribers.add(connectionId, id, sender);
      try {
        const runtime = await this.ensureNetworkMonitorRuntime(connectionId);
        return await runtime.controller.start();
      } catch (error) {
        if (this.networkMonitorSubscribers.remove(connectionId, id)) {
          await this.hardStopNetworkMonitor(connectionId);
        }
        throw error;
      }
    });
  }

  /** @param subscriberId renderer session id (see stopSystemMonitor). */
  async stopNetworkMonitor(connectionId: string, subscriberId?: string): Promise<{ ok: true }> {
    return this.runExclusive(`network:${connectionId}`, async () => {
      let idle: boolean;
      if (subscriberId) {
        idle = this.networkMonitorSubscribers.remove(connectionId, subscriberId);
      } else {
        this.networkMonitorSubscribers.clear(connectionId);
        idle = true;
      }
      if (!idle) {
        return { ok: true } as const;
      }
      await this.hardStopNetworkMonitor(connectionId);
      return { ok: true } as const;
    });
  }

  private async hardStopNetworkMonitor(connectionId: string): Promise<void> {
    const runtime = this.networkMonitorRuntimes.get(connectionId);
    if (runtime) {
      runtime.sender = undefined;
      await runtime.controller.stop();
    }
  }

  async getNetworkConnections(connectionId: string, port: number): Promise<NetworkConnection[]> {
    this.assertMonitorEnabled(connectionId);
    this.assertVisibleTerminalAlive(connectionId);

    const runtime = await this.ensureNetworkMonitorRuntime(connectionId);
    return runtime.controller.getConnectionsByPort(port);
  }
}
