import fs from "node:fs";
import path from "node:path";
import { app, BrowserWindow, clipboard, dialog, Notification, shell } from "electron";
import type { WebContents } from "electron";
import type {
  ConnectionProfile,
  MonitorSnapshot,
  NetworkSnapshot,
  ProcessSnapshot
} from "../../../../../packages/core/src/index";
import {
  DEFAULT_MAX_CHANNELS_PER_CONNECTION,
  SshConnection,
  type SshConnectOptions
} from "../../../../../packages/ssh/src/index";
import { IPCChannel, AUTH_REQUIRED_PREFIX } from "../../../../../packages/shared/src/index";
import type {
  DebugLogEntry,
  SessionAuthOverrideInput,
  SessionStatusEvent,
  SftpTransferStatusEvent
} from "../../../../../packages/shared/src/index";
import {
  EncryptedSecretVault,
  KeytarPasswordCache
} from "../../../../../packages/security/src/index";
import {
  SQLiteConnectionRepository,
  CachedConnectionRepository,
  SQLiteSshKeyRepository,
  CachedSshKeyRepository,
  SQLiteConnectionFolderRepository,
  SQLiteProxyRepository,
  CachedProxyRepository
} from "../../../../../packages/storage/src/index";
import { DeviceKeyProvider } from "./device-key-provider";
import { RemoteEditManager } from "./remote-edit-manager";
import { logger } from "../logger";
import { createOrderedBytesDispatcher } from "./ipc-stream-dispatcher";
import { normalizeError } from "./container-utils";
import type { ActiveSession } from "./container-types";

// ─── Sub-services ──────────────────────────────────────────────────────────
import { PreferencesDialogService } from "./preferences-dialog-service";
import { TerminalIntegrationService } from "./terminal-integration-service";
import { NetworkToolService } from "./network-tool-service";
import { CommandService } from "./command-service";
import { ConnectionService } from "./connection-service";
import { ConnectionFolderService } from "./connection-folder-service";
import { ImportExportService } from "./import-export-service";
import { CloudSyncManager } from "./cloud-sync-manager";
import { ResourceOperationsService } from "./resource-operations-service";
import { MonitorService } from "./monitor-service";
import { SftpService } from "./sftp-service";
import { SessionService } from "./session-service";
import { forgetShellIntegrationInstalls } from "./terminal-shell-integration";
import { createAgentMcpService, type AgentRemoteFileStat, type AgentSessionInfo } from "./mcp";
import { AgentPromptBroker } from "./mcp/confirm";
import { OscTapRegistry } from "./mcp/osc-tap";
import { ScreenMirrorRegistry } from "./mcp/screen-mirror";
import { AgentTransferTracker } from "./mcp/transfers";

const cloudSyncWorkspacePasswordRef = (workspaceId: string): string =>
  `secret://cloud-sync-ws-${workspaceId}`;

/** SQLite data lives here; the MCP discovery file deliberately does not. */
const STORAGE_DIRECTORY_NAME = "storage";

// Re-export for consumers (index.ts, register.ts)
export type { ServiceContainer, CreateServiceContainerOptions } from "./container-types";

export const createServiceContainer = async (
  options: import("./container-types").CreateServiceContainerOptions
): Promise<import("./container-types").ServiceContainer> => {
  const dataDir = path.join(options.userDataDir, STORAGE_DIRECTORY_NAME);
  fs.mkdirSync(dataDir, { recursive: true });
  const dbPath = path.join(dataDir, "nextshell.db");

  const rawRepo = new SQLiteConnectionRepository(dbPath);
  const connections = new CachedConnectionRepository(rawRepo);
  connections.seedIfEmpty([]);

  const sshKeyRepo = new CachedSshKeyRepository(new SQLiteSshKeyRepository(rawRepo.getDb()));
  const proxyRepo = new CachedProxyRepository(new SQLiteProxyRepository(rawRepo.getDb()));
  const folderRepo = new SQLiteConnectionFolderRepository(rawRepo.getDb());

  // ─── Device Key ──────────────────────────────────────────────────────────
  // The device key encrypts every stored credential and is authoritative in
  // SQLite. A legacy keychain item is drained once when the database is empty.
  const deviceKeyStore = new KeytarPasswordCache("NextShell", "device-key");
  // Resolved lazily: reading it eagerly would prompt for keychain authorization
  // on every launch, including sessions that never open a stored credential.
  const deviceKeyProvider = new DeviceKeyProvider({
    store: deviceKeyStore,
    db: {
      getLegacy: () => connections.getDeviceKey(),
      saveLegacy: (key) => connections.saveDeviceKey(key)
    }
  });

  const vault = new EncryptedSecretVault(connections.getSecretStore(), () =>
    deviceKeyProvider.get()
  );

  const broadcastToAllWindows = (channel: string, payload: unknown): void => {
    for (const window of BrowserWindow.getAllWindows()) {
      if (!window.isDestroyed()) window.webContents.send(channel, payload);
    }
  };
  const agentPromptBroker = new AgentPromptBroker({
    send: (request) => {
      const target = BrowserWindow.getFocusedWindow() ?? BrowserWindow.getAllWindows()[0];
      if (!target || target.isDestroyed()) throw new Error("No renderer is available for prompt");
      target.webContents.send(IPCChannel.AgentPromptRequest, request);
    }
  });
  /**
   * Declared here because `sendTransferStatus` consults it on every progress
   * event to tell agent-owned tasks apart from user-owned ones.
   */
  const agentTransfers = new AgentTransferTracker();

  // ─── Shared State ────────────────────────────────────────────────────────
  const activeSessions = new Map<string, ActiveSession>();
  const oscTaps = new OscTapRegistry();
  const screenMirrors = new ScreenMirrorRegistry();

  /**
   * Last system-monitor snapshot per connection. MonitorService only pushes;
   * the agent gateway needs a pull, and must never be able to start a monitor
   * session of its own.
   */
  const latestMonitorSnapshots = new Map<string, MonitorSnapshot>();

  // ─── Connection Pool State ───────────────────────────────────────────────
  // One connection profile is backed by *several* ssh2 clients. Every shell,
  // exec and SFTP channel consumes one of the server's session slots (OpenSSH
  // defaults to MaxSessions=10), so a single client cannot back more than a
  // handful of terminals: past the limit shell()/sftp() fail silently and the
  // tab stays blank. Clients are filled up to CLIENT_CHANNEL_BUDGET channels,
  // then another client is dialled.
  const CLIENT_CHANNEL_BUDGET = DEFAULT_MAX_CHANNELS_PER_CONNECTION;
  const connectionPool = new Map<string, SshConnection[]>();
  /** Serializes connect attempts per connection id, auth overrides included. */
  const connectQueues = new Map<string, Promise<SshConnection>>();
  /**
   * Sessions that have begun opening but are not registered in
   * `activeSessions` yet. Without this, closing the last tab while another one
   * is still connecting would tear down the client the new tab is about to use.
   */
  const connectionRefCounts = new Map<string, number>();

  // ─── IPC Helpers ─────────────────────────────────────────────────────────
  const sendSessionStatus = (sender: WebContents, payload: SessionStatusEvent): void => {
    if (!sender.isDestroyed()) sender.send(IPCChannel.SessionStatus, payload);
  };

  /**
   * User-initiated transfers report back to the window that started them.
   * Agent-initiated ones have no sender, so they fan out to every window
   * instead — otherwise the transfer the agent kicked off would be invisible in
   * the GUI queue, which is the only place the user can watch or cancel it.
   */
  const sendTransferStatus = (
    sender: WebContents | undefined,
    payload: SftpTransferStatusEvent
  ): void => {
    if (payload.taskId && agentTransfers.get(payload.taskId)) {
      agentTransfers.applyProgress(payload);
      broadcastToAllWindows(IPCChannel.SftpTransferStatus, {
        ...payload,
        origin: "agent" as const
      });
      return;
    }
    if (!sender || sender.isDestroyed()) return;
    sender.send(IPCChannel.SftpTransferStatus, payload);
  };

  // ─── Stream Dispatchers ──────────────────────────────────────────────────
  const sessionDataDispatcher = createOrderedBytesDispatcher({
    channel: IPCChannel.SessionData,
    flushIntervalMs: 16,
    targetChunkBytes: 64 * 1024,
    highWaterBytes: 512 * 1024,
    lowWaterBytes: 256 * 1024,
    buildPayload: ({ streamId, deliveryId, chunk, byteLength }) => ({
      // byteLength comes from the dispatcher's single measurement of the
      // frame — the same value used for in-flight accounting, so the
      // renderer's verbatim ack always drains the stream.
      sessionId: streamId,
      data: chunk,
      deliveryId,
      byteLength
    })
  });

  // Monitor snapshots are low-rate (≤1Hz) and sent directly — no delivery-id/ack
  // handshake (that protocol remains exclusively for the high-throughput ordered
  // terminal byte stream above). A blocked-but-alive renderer can therefore queue
  // snapshots in the IPC channel at poll rate; that window is bounded by the
  // main process's "unresponsive" auto-reload and the hide/suspend poll pausing.
  const createMonitorSnapshotEmitter =
    <TSnapshot>(channel: string) =>
    (sender: WebContents | undefined, snapshot: TSnapshot): void => {
      if (sender && !sender.isDestroyed() && !sender.isCrashed()) {
        sender.send(channel, snapshot);
      }
    };

  const emitSystemMonitorSnapshot = createMonitorSnapshotEmitter<MonitorSnapshot>(
    IPCChannel.MonitorSystemData
  );
  const emitProcessMonitorSnapshot = createMonitorSnapshotEmitter<ProcessSnapshot>(
    IPCChannel.MonitorProcessData
  );
  const emitNetworkMonitorSnapshot = createMonitorSnapshotEmitter<NetworkSnapshot>(
    IPCChannel.MonitorNetworkData
  );

  // ─── Connection Pool ─────────────────────────────────────────────────────
  const remoteEditManager = new RemoteEditManager({ getConnection: ensureConnection });

  const getConnectionOrThrow = (id: string): ConnectionProfile => {
    const connection = connections.getById(id);
    if (!connection) throw new Error("Connection not found");
    return connection;
  };

  const resolveConnectOptions = async (
    profile: ConnectionProfile,
    authOverride?: SessionAuthOverrideInput
  ): Promise<SshConnectOptions> => {
    let proxy: SshConnectOptions["proxy"];
    if (profile.proxyId) {
      const proxyProfile = proxyRepo.getById(profile.proxyId);
      if (!proxyProfile)
        throw new Error("Referenced proxy profile not found. Please update the connection.");
      const proxySecret = proxyProfile.credentialRef
        ? await vault.readCredential(proxyProfile.credentialRef)
        : undefined;
      proxy = {
        type: proxyProfile.proxyType,
        host: proxyProfile.host,
        port: proxyProfile.port,
        username: proxyProfile.username,
        password:
          proxyProfile.proxyType === "socks5" && proxyProfile.username ? proxySecret : undefined
      };
      if (!proxy.host || proxy.port <= 0)
        throw new Error("Proxy host and port are required when proxy is enabled.");
    }

    const username = authOverride?.username?.trim() || profile.username.trim();
    if (!username) throw new Error("SSH username is required.");

    const prefs = connections.getAppPreferences();
    const keepAliveEnabled = profile.keepAliveEnabled ?? prefs.ssh.keepAliveEnabled;
    const intervalCandidate = profile.keepAliveIntervalSec ?? prefs.ssh.keepAliveIntervalSec;
    const keepAliveIntervalSec =
      Number.isInteger(intervalCandidate) && intervalCandidate >= 5 && intervalCandidate <= 600
        ? intervalCandidate
        : prefs.ssh.keepAliveIntervalSec;
    const keepaliveInterval = keepAliveEnabled ? keepAliveIntervalSec * 1000 : 0;

    const base: Omit<SshConnectOptions, "authType"> = {
      host: profile.host,
      port: profile.port,
      username,
      hostFingerprint: profile.hostFingerprint,
      strictHostKeyChecking: profile.strictHostKeyChecking,
      proxy,
      keepaliveInterval
    };

    const secret = profile.credentialRef
      ? await vault.readCredential(profile.credentialRef)
      : undefined;
    const effectiveAuthType = authOverride?.authType ?? profile.authType;
    const isPasswordStyleAuth =
      effectiveAuthType === "password" || effectiveAuthType === "interactive";

    if (isPasswordStyleAuth) {
      const password =
        authOverride?.authType === "password" || authOverride?.authType === "interactive"
          ? authOverride.password
          : profile.authType === "password" || profile.authType === "interactive"
            ? secret
            : undefined;
      if (!password) {
        throw new Error(
          effectiveAuthType === "interactive"
            ? "Interactive auth requires password"
            : "Password credential is missing. Please provide password."
        );
      }
      return { ...base, authType: effectiveAuthType, password };
    }

    if (effectiveAuthType === "privateKey") {
      const effectiveKeyId = authOverride?.sshKeyId ?? profile.sshKeyId;
      let privateKey: string | undefined;
      let passphrase: string | undefined;
      if (authOverride?.privateKeyContent) {
        privateKey = authOverride.privateKeyContent;
        passphrase = authOverride.passphrase;
      } else if (effectiveKeyId) {
        const keyProfile = sshKeyRepo.getById(effectiveKeyId);
        if (!keyProfile)
          throw new Error("Referenced SSH key not found. Please update the connection.");
        privateKey = await vault.readCredential(keyProfile.keyContentRef);
        if (keyProfile.passphraseRef)
          passphrase = await vault.readCredential(keyProfile.passphraseRef);
        if (authOverride?.passphrase) passphrase = authOverride.passphrase;
      }
      if (!privateKey)
        throw new Error("Private key auth requires an SSH key. Please select a key.");
      return { ...base, authType: "privateKey", privateKey, passphrase };
    }

    return { ...base, authType: "agent" };
  };

  // TOFU: pin the server host-key fingerprint on the first successful connect so
  // any later key change is detected and rejected (see ssh hostVerifier).
  const pinHostFingerprint = (connectionId: string, fingerprint: string): void => {
    try {
      const latest = connections.getById(connectionId);
      if (!latest || latest.hostFingerprint?.trim()) return;
      connections.save({
        ...latest,
        hostFingerprint: fingerprint,
        updatedAt: new Date().toISOString()
      });
      logger.info("[Security] pinned host fingerprint (TOFU)", { connectionId, fingerprint });
    } catch (error) {
      logger.warn("[Security] failed to pin host fingerprint", {
        connectionId,
        error: normalizeError(error)
      });
    }
  };

  const establishConnection = async (
    connectionId: string,
    profile: ConnectionProfile,
    authOverride?: SessionAuthOverrideInput
  ): Promise<SshConnection> => {
    logger.info("[SSH] connecting", { connectionId, host: profile.host, port: profile.port });
    const connectOptions = await resolveConnectOptions(profile, authOverride);
    let observedFingerprint: string | undefined;
    const ssh = await SshConnection.connect({
      ...connectOptions,
      onHostFingerprint: (fingerprint) => {
        observedFingerprint = fingerprint;
      }
    });
    if (observedFingerprint && !profile.hostFingerprint?.trim()) {
      pinHostFingerprint(connectionId, observedFingerprint);
    }
    ssh.onClose(() => {
      evictClient(connectionId, ssh, "closed");
    });
    // Without this listener a post-handshake client error lands on
    // `uncaughtException` and the dead client stays in the pool forever.
    ssh.onError((error) => {
      logger.warn("[SSH] client error", { connectionId, error: normalizeError(error) });
      evictClient(connectionId, ssh, "error");
      void ssh.close().catch(() => undefined);
    });

    const pooled = connectionPool.get(connectionId);
    if (pooled) pooled.push(ssh);
    else connectionPool.set(connectionId, [ssh]);
    logger.info("[SSH] connected", {
      connectionId,
      clients: connectionPool.get(connectionId)?.length ?? 1
    });
    return ssh;
  };

  /**
   * Remove one client from the pool. The identity check matters: a client that
   * is no longer a pool member (already evicted, or superseded by a reconnect)
   * must not tear down the live pool entry, and remote-edit sessions belong to
   * the connection rather than to one client, so they are only cleaned up when
   * the last client of that connection is gone.
   */
  function evictClient(connectionId: string, ssh: SshConnection, reason: string): void {
    const clients = connectionPool.get(connectionId);
    const index = clients ? clients.indexOf(ssh) : -1;
    if (!clients || index < 0) return;
    clients.splice(index, 1);
    const isLastClient = clients.length === 0;
    if (isLastClient) connectionPool.delete(connectionId);
    logger.info("[SSH] client left the pool", {
      connectionId,
      reason,
      remainingClients: clients.length
    });
    if (isLastClient) {
      latestMonitorSnapshots.delete(connectionId);
      void remoteEditManager.cleanupByConnectionId(connectionId);
      // Reconnects must re-probe/re-install: the remote cache dir may be gone.
      forgetShellIntegrationInstalls(connectionId);
    }
  }

  /** Live pool members, pruning clients that died without their close handler
   *  having run yet. */
  const listPooledClients = (connectionId: string): SshConnection[] => {
    const clients = connectionPool.get(connectionId);
    if (!clients) return [];
    for (let index = clients.length - 1; index >= 0; index -= 1) {
      if (!clients[index]!.isAlive) clients.splice(index, 1);
    }
    if (clients.length === 0) {
      connectionPool.delete(connectionId);
      return [];
    }
    return clients;
  };

  /**
   * First pool member with spare channel budget. First-fit rather than
   * least-loaded so the shared SFTP channel and remote-edit stay on the
   * earliest client instead of being duplicated across the pool.
   */
  const pickAvailableClient = (connectionId: string): SshConnection | undefined =>
    listPooledClients(connectionId).find((client) =>
      client.hasChannelCapacity(CLIENT_CHANNEL_BUDGET)
    );

  /**
   * Serialize connect attempts per connection id. The authOverride path used to
   * skip the dedupe entirely, so concurrent retries dialled several clients and
   * silently overwrote each other's pool entry, orphaning the losers.
   */
  const enqueueConnect = (
    connectionId: string,
    authOverride?: SessionAuthOverrideInput
  ): Promise<SshConnection> => {
    const run = async (): Promise<SshConnection> => {
      // Re-check: the attempt ahead of us may have produced a client with
      // spare budget, so a burst of tabs shares a single handshake.
      const reusable = pickAvailableClient(connectionId);
      if (reusable) return reusable;
      const profile = getConnectionOrThrow(connectionId);
      return establishConnection(connectionId, profile, authOverride);
    };

    const previous = connectQueues.get(connectionId);
    const next = previous ? previous.then(run, run) : run();
    connectQueues.set(connectionId, next);
    void next
      .catch(() => undefined)
      .finally(() => {
        if (connectQueues.get(connectionId) === next) connectQueues.delete(connectionId);
      });
    return next;
  };

  function ensureConnection(
    connectionId: string,
    authOverride?: SessionAuthOverrideInput
  ): Promise<SshConnection> {
    const reusable = pickAvailableClient(connectionId);
    if (reusable) return Promise.resolve(reusable);
    return enqueueConnect(connectionId, authOverride);
  }

  /**
   * Terminal variant of `ensureConnection`: it also reserves a channel slot on
   * the chosen client, synchronously with the pick. Tabs opened in the same
   * tick would otherwise all measure the same pre-open load and pile onto one
   * client, which is exactly how MaxSessions gets exhausted.
   */
  const acquireTerminalConnection = async (
    connectionId: string,
    authOverride?: SessionAuthOverrideInput
  ): Promise<{ connection: SshConnection; release: () => void }> => {
    const reusable = pickAvailableClient(connectionId);
    if (reusable) return { connection: reusable, release: reusable.reserveChannel() };
    const connection = await enqueueConnect(connectionId, authOverride);
    return { connection, release: connection.reserveChannel() };
  };

  /**
   * Explicit reference held for the whole "session is opening" window, which
   * starts before the session lands in `activeSessions`.
   */
  const retainConnection = (connectionId: string): (() => void) => {
    connectionRefCounts.set(connectionId, (connectionRefCounts.get(connectionId) ?? 0) + 1);
    let released = false;
    return () => {
      if (released) return;
      released = true;
      const remaining = (connectionRefCounts.get(connectionId) ?? 1) - 1;
      if (remaining > 0) connectionRefCounts.set(connectionId, remaining);
      else connectionRefCounts.delete(connectionId);
    };
  };

  const isConnectionRetained = (connectionId: string): boolean =>
    (connectionRefCounts.get(connectionId) ?? 0) > 0;

  /**
   * Nothing wants this connection: no handshake in flight *and* no live
   * session. Both halves matter — a session that finishes opening drops its
   * ref-count and lands in `activeSessions` in the same breath, so checking
   * only one of them leaves a window where the connection looks idle while a
   * terminal is using it.
   */
  const isConnectionIdle = (connectionId: string): boolean =>
    !isConnectionRetained(connectionId) &&
    !Array.from(activeSessions.values()).some(
      (s) => s.kind === "remote" && s.connectionId === connectionId
    );

  const closeConnectionIfIdle = async (connectionId: string): Promise<void> => {
    if (!isConnectionIdle(connectionId)) return;
    // Monitor teardown races each hidden client's close against a 2s timeout,
    // so this await is long enough for a whole session open to complete inside
    // it: re-check the *full* idleness condition afterwards, not just the ref
    // count, or the tab that just opened gets its shell closed underneath it.
    await monitorSvc.disposeAllMonitorSessions(connectionId);
    if (!isConnectionIdle(connectionId)) return;
    const clients = connectionPool.get(connectionId);
    if (!clients || clients.length === 0) return;
    connectionPool.delete(connectionId);
    await remoteEditManager.cleanupByConnectionId(connectionId);
    await Promise.all(clients.map((client) => client.close()));
  };

  const hasVisibleTerminalAlive = (connectionId: string): boolean =>
    Array.from(activeSessions.values()).some(
      (s) =>
        s.kind === "remote" &&
        s.connectionId === connectionId &&
        s.descriptor.type === "terminal" &&
        s.descriptor.status === "connected"
    );

  const assertMonitorEnabled = (connectionId: string): ConnectionProfile => {
    const profile = getConnectionOrThrow(connectionId);
    if (!profile.monitorSession)
      throw new Error("当前连接未启用 Monitor Session，请在连接配置中开启后重试。");
    return profile;
  };

  const assertVisibleTerminalAlive = (connectionId: string): void => {
    if (!hasVisibleTerminalAlive(connectionId))
      throw new Error("请先连接 SSH 终端以启动 Monitor Session。");
  };

  const establishHiddenConnection = async (
    connectionId: string,
    tag: string
  ): Promise<SshConnection> => {
    const profile = assertMonitorEnabled(connectionId);
    logger.info(`[${tag}] connecting hidden SSH`, {
      connectionId,
      host: profile.host,
      port: profile.port
    });
    const ssh = await SshConnection.connect(await resolveConnectOptions(profile));
    logger.info(`[${tag}] hidden SSH connected`, { connectionId });
    return ssh;
  };

  // ─── Sub-Service Instantiation ───────────────────────────────────────────
  const prefsSvc = new PreferencesDialogService({
    connections
  });

  const terminalIntegrationSvc = new TerminalIntegrationService();

  const networkToolSvc = new NetworkToolService({ connections });

  const monitorSvc = new MonitorService({
    connections,
    getConnectionOrThrow,
    resolveConnectOptions: (profile) => resolveConnectOptions(profile),
    activeSessions,
    debugSenders: prefsSvc.debugSenders,
    emitDebugLog: (entry) => prefsSvc.emitDebugLog(entry),
    emitSystemSnapshot: (sender, snapshot) => {
      latestMonitorSnapshots.set(snapshot.connectionId, snapshot);
      emitSystemMonitorSnapshot(sender, snapshot);
    },
    emitProcessSnapshot: emitProcessMonitorSnapshot,
    emitNetworkSnapshot: emitNetworkMonitorSnapshot
  });

  const sftpSvc = new SftpService({
    getConnectionOrThrow,
    ensureConnection,
    remoteEditManager,
    sendTransferStatus
  });

  const connectionSvc = new ConnectionService({
    connectionFolders: folderRepo,
    connections,
    sshKeyRepo,
    proxyRepo,
    vault,
    activeSessions,
    disposeAllMonitorSessions: (id) => monitorSvc.disposeAllMonitorSessions(id),
    closeConnectionIfIdle,
    remoteEditManager,
    monitorStates: monitorSvc.monitorStates,
    getCloudSyncManager: () => cloudSyncManager,
    sendSessionStatus
  });

  let cloudSyncManager: CloudSyncManager | undefined;

  const commandSvc = new CommandService({
    connections,
    getConnectionOrThrow,
    ensureConnection,
    listWorkspaces: () => connections.listCloudSyncWorkspaces(),
    markWorkspaceCommandsDirty: (workspaceId) => {
      cloudSyncManager?.markWorkspaceCommandsDirty(workspaceId);
    }
  });

  const importExportSvc = new ImportExportService({
    connections,
    sshKeyRepo,
    connectionFolders: folderRepo,
    vault,
    upsertConnection: (input) => connectionSvc.upsertConnection(input)
  });

  const sessionSvc = new SessionService({
    connections,
    activeSessions,
    getConnectionOrThrow,
    acquireTerminalConnection,
    retainConnection,
    closeConnectionIfIdle,
    sendSessionStatus,
    sessionDataDispatcher,
    ensureSystemMonitorRuntime: (id) => monitorSvc.ensureSystemMonitorRuntime(id),
    clearMonitorSuspension: (id) => monitorSvc.clearMonitorSuspension(id),
    warmupSftp: (id, conn) => sftpSvc.warmupSftp(id, conn),
    persistAuthOverride: (id, override) =>
      connectionSvc.persistSuccessfulAuthOverride(id, override),
    // Both agent-facing layers hang off this one tap, and both are gated on the
    // host being agent-visible: an unauthorized host costs nothing at all. They
    // stay separate parsers on purpose — OscTap owns command boundaries and the
    // raw bytes each command produced, which a terminal grid cannot reconstruct,
    // while the mirror owns the rendered frame, which raw bytes cannot express.
    tapAgentSessionData: (sessionId, connectionId, data) => {
      if ((connections.getById(connectionId)?.agentAccess ?? "off") === "off") {
        oscTaps.dispose(sessionId);
        screenMirrors.dispose(sessionId);
        return;
      }
      oscTaps.feed(sessionId, data);
      screenMirrors.write(sessionId, data);
    },
    disposeAgentSessionData: (sessionId) => {
      oscTaps.dispose(sessionId);
      screenMirrors.dispose(sessionId);
    },
    onSessionResized: (sessionId, cols, rows) => screenMirrors.resize(sessionId, cols, rows)
  });

  // Cloud Sync Manager
  cloudSyncManager = new CloudSyncManager({
    listConnections: () => connections.list({}),
    saveConnection: (conn) => connections.save(conn),
    removeConnection: (id) => connections.remove(id),
    listSshKeys: () => sshKeyRepo.list(),
    saveSshKey: (key) => sshKeyRepo.save(key),
    removeSshKey: (id) => sshKeyRepo.remove(id),
    listProxies: () => proxyRepo.list(),
    saveProxy: (proxy) => proxyRepo.save(proxy),
    removeProxy: (id) => proxyRepo.remove(id),
    readCredential: async (ref) => {
      try {
        return await vault.readCredential(ref);
      } catch {
        return undefined;
      }
    },
    storeCredential: (name, secret) => vault.storeCredential(name, secret),
    deleteCredential: (ref) => vault.deleteCredential(ref),
    listWorkspaces: () => connections.listCloudSyncWorkspaces(),
    saveWorkspace: (ws) => connections.saveCloudSyncWorkspace(ws),
    removeWorkspace: (id) => connections.removeCloudSyncWorkspace(id),
    getWorkspaceRepoLocalState: (wId) => connections.getWorkspaceRepoLocalState(wId),
    saveWorkspaceRepoLocalState: (state) => connections.saveWorkspaceRepoLocalState(state),
    listWorkspaceCommands: (wId) => connections.listWorkspaceCommands(wId),
    replaceWorkspaceCommands: (wId, commands) =>
      connections.replaceWorkspaceCommands(wId, commands),
    saveRecycleBinEntry: (e) => connections.saveRecycleBinEntry(e),
    storeWorkspacePassword: async (wId, pwd) => {
      await vault.storeCredential(`cloud-sync-ws-${wId}`, pwd);
    },
    getWorkspacePassword: async (wId) => {
      try {
        return await vault.readCredential(cloudSyncWorkspacePasswordRef(wId));
      } catch {
        return undefined;
      }
    },
    deleteWorkspacePassword: async (wId) => {
      await vault.deleteCredential(cloudSyncWorkspacePasswordRef(wId)).catch(() => {});
    },
    getJsonSetting: (key) => connections.getJsonSetting(key),
    saveJsonSetting: (key, value) => connections.saveJsonSetting(key, value),
    broadcastStatus: (status) => broadcastToAllWindows(IPCChannel.CloudSyncStatusEvent, status),
    broadcastApplied: (wId) =>
      broadcastToAllWindows(IPCChannel.CloudSyncAppliedEvent, { workspaceId: wId })
  });
  cloudSyncManager.initialize();

  // 目录写操作要跟着重投影子孙连接的 groupPath(云同步线协议/MCP schema/导出文件都读它),
  // 所以 IPC 走这层服务而不是裸仓储;只需要投影的其他服务继续直接用 folderRepo。
  const folderSvc = new ConnectionFolderService({
    folders: folderRepo,
    connections,
    listCloudWorkspaces: () => cloudSyncManager?.listWorkspaces() ?? []
  });

  // Resource Operations Service
  const resourceOpsSvc = new ResourceOperationsService({
    connections,
    sshKeyRepo,
    proxyRepo,
    connectionFolders: folderRepo,
    vault,
    cloudSyncManager,
    saveRecycleBinEntry: (e) => connections.saveRecycleBinEntry(e),
    listRecycleBinEntries: () => connections.listRecycleBinEntries(),
    removeRecycleBinEntry: (id) => connections.removeRecycleBinEntry(id)
  });

  // ─── Agent (MCP) Endpoint ────────────────────────────────────────────────
  // Every dependency below is read-only by construction: the gateway is handed
  // no writer, no vault handle and no connect path, so a compromised MCP client
  // cannot reach a credential even if it reaches the gateway.
  const agentMcpSvc = createAgentMcpService({
    userDataDir: options.userDataDir,
    appVersion: app.getVersion(),
    listConnections: () => connections.list({}),
    isConnectionOnline: (connectionId) => listPooledClients(connectionId).length > 0,
    listSessions: () =>
      Array.from(activeSessions.values()).map<AgentSessionInfo>((session) => {
        // getSummary, not get: this runs for every session on every gateway
        // call that touches session state, and a full snapshot clones the whole
        // command history just to read two fields off it.
        const tap = oscTaps.getSummary(session.descriptor.id);
        return {
          id: session.descriptor.id,
          // Local shells have no connection and are therefore invisible to agents.
          connectionId: session.kind === "remote" ? session.connectionId : null,
          title: session.descriptor.title,
          status: session.descriptor.status,
          type: session.descriptor.type,
          createdAt: session.descriptor.createdAt,
          cwd: tap?.cwd ?? null,
          lastCommand: tap?.lastCommand ?? null
        };
      }),
    getMonitorSnapshot: async (connectionId) => latestMonitorSnapshots.get(connectionId) ?? null,
    listRemoteFiles: (connectionId, remotePath) =>
      sftpSvc.listRemoteFiles(connectionId, remotePath),
    statRemoteFile: async (connectionId, remotePath) => {
      const connection = await ensureConnection(connectionId);
      const stats = await connection.stat(remotePath);
      // Anything that is not a plain file, directory or symlink must report
      // "other": calling a character device a regular file is exactly what
      // would let an agent ask for /dev/zero.
      const type: AgentRemoteFileStat["type"] = stats.isDirectory()
        ? "directory"
        : stats.isSymbolicLink()
          ? "link"
          : stats.isFile()
            ? "file"
            : "other";
      return {
        path: remotePath,
        type,
        size: stats.size,
        permissions: (stats.mode & 0o777).toString(8).padStart(4, "0"),
        uid: stats.uid,
        gid: stats.gid,
        modifiedAt: new Date(stats.mtime * 1000).toISOString(),
        accessedAt: new Date(stats.atime * 1000).toISOString()
      };
    },
    readRemoteFile: async (connectionId, remotePath, maxBytes, signal) => {
      const connection = await ensureConnection(connectionId);
      // Bounded inside the SFTP stream, not after the fact: a stat-based check
      // cannot protect against procfs files that report size 0.
      const content = await connection.readFileContent(remotePath, { maxBytes, signal });
      return {
        bytes: content.subarray(0, maxBytes),
        truncated: content.byteLength > maxBytes
      };
    },
    // Deliberately no listCommandHistory: shell history has no connection id,
    // so it cannot be scoped to the hosts the user granted.
    listSavedCommands: (query) => connections.listSavedCommands(query),
    readSessionScreen: async (sessionId, options) =>
      (await screenMirrors.get(sessionId)?.read(options)) ?? null,
    writeSession: (sessionId, data) => {
      sessionSvc.writeSession(sessionId, data, "agent");
    },
    lastUserInputAt: (sessionId) => sessionSvc.lastUserInputAt(sessionId),
    waitForCommandCompletion: async (sessionId, timeoutMs) => {
      const entry = await oscTaps.waitForCommandCompletion(sessionId, timeoutMs);
      if (!entry) return null;
      return {
        command: entry.command,
        exitCode: entry.exitCode,
        output: entry.output,
        truncated: entry.truncated
      };
    },
    openSession: async (connectionId) => {
      // Bound to a real window: an agent-opened tab must be one the user can
      // see and close, not an invisible channel.
      const target = BrowserWindow.getFocusedWindow() ?? BrowserWindow.getAllWindows()[0];
      if (!target || target.isDestroyed()) {
        throw new Error("No NextShell window is available to host the session");
      }
      const descriptor = await sessionSvc.openSession(
        { target: "remote", connectionId },
        target.webContents
      );
      return { id: descriptor.id, title: descriptor.title, status: descriptor.status };
    },
    closeSession: async (sessionId) => {
      await sessionSvc.closeSession(sessionId);
    },
    focusSession: (sessionId) => {
      const target = BrowserWindow.getFocusedWindow() ?? BrowserWindow.getAllWindows()[0];
      if (!target || target.isDestroyed()) return;
      if (target.isMinimized()) target.restore();
      target.show();
      target.focus();
      target.webContents.send(IPCChannel.AgentSessionFocusEvent, { sessionId });
    },
    setSessionAgentControlled: (sessionId, clientName) =>
      broadcastToAllWindows(IPCChannel.AgentSessionControlEvent, {
        sessionId,
        clientName,
        controlled: true
      }),
    clearSessionAgentControlled: (sessionId) =>
      broadcastToAllWindows(IPCChannel.AgentSessionControlEvent, {
        sessionId,
        clientName: null,
        controlled: false
      }),
    getSessionHistory: (sessionId) => {
      const snapshot = oscTaps.get(sessionId);
      if (!snapshot) return null;
      return {
        integrationAvailable:
          snapshot.lastCommand !== null || snapshot.history.some((entry) => entry.command !== null),
        entries: snapshot.history.map((entry) => ({
          command: entry.command,
          exitCode: entry.exitCode,
          startedAt: entry.startedAt,
          finishedAt: entry.endedAt,
          output: entry.output,
          truncated: entry.truncated
        }))
      };
    },
    execCommand: (connectionId, command, options) =>
      commandSvc.execCommand(connectionId, command, options),
    writeRemoteFile: async (connectionId, remotePath, content) => {
      const connection = await ensureConnection(connectionId);
      await connection.writeFileContent(remotePath, content);
    },
    makeRemoteDirectory: async (connectionId, remotePath) => {
      await sftpSvc.createRemoteDirectory(connectionId, remotePath);
    },
    renameRemotePath: async (connectionId, fromPath, toPath) => {
      await sftpSvc.renameRemoteFile(connectionId, fromPath, toPath);
    },
    deleteRemotePath: async (connectionId, remotePath, type) => {
      await sftpSvc.deleteRemoteFile(connectionId, remotePath, type);
    },
    statLocalPath: (localPath) => {
      try {
        // lstat, not stat: a symlink must be reported as what it is so the
        // path policy decides, rather than being silently followed here.
        const stats = fs.lstatSync(localPath);
        return {
          type: stats.isDirectory() ? "directory" : stats.isFile() ? "file" : "other",
          size: stats.size
        };
      } catch {
        return null;
      }
    },
    localPathContext: () => ({
      homeDir: app.getPath("home"),
      appDataDir: options.userDataDir,
      allowedRoots: prefsSvc.getAppPreferences().agent.allowedLocalRoots
    }),
    startUpload: ({ clientId, connectionId, localPath, remotePath, packed }) =>
      agentTransfers.start({
        clientId,
        connectionId,
        direction: "upload",
        localPath,
        remotePath,
        packed,
        // A directory goes over as one tar.gz and is unpacked remotely, which
        // is the whole reason routing an agent through NextShell beats letting
        // it drive scp itself.
        run: (taskId) => {
          const release = retainConnection(connectionId);
          const done = packed
            ? sftpSvc.uploadRemotePacked(
                connectionId,
                [localPath],
                remotePath,
                undefined,
                undefined,
                taskId
              )
            : sftpSvc.uploadRemoteFile(connectionId, localPath, remotePath, undefined, taskId);
          return done.finally(() => {
            release();
            void closeConnectionIfIdle(connectionId).catch(() => undefined);
          });
        }
      }),
    startDownload: ({ clientId, connectionId, remotePath, localPath }) =>
      agentTransfers.start({
        clientId,
        connectionId,
        direction: "download",
        localPath,
        remotePath,
        packed: false,
        run: (taskId) => {
          const release = retainConnection(connectionId);
          return sftpSvc
            .downloadRemoteFile(connectionId, remotePath, localPath, undefined, taskId)
            .finally(() => {
              release();
              void closeConnectionIfIdle(connectionId).catch(() => undefined);
            });
        }
      }),
    getTransfer: (taskId, clientId) => agentTransfers.getForClient(taskId, clientId),
    cancelTransfer: (taskId) => sftpSvc.cancelTransfer(taskId).cancelled,
    runningTransferCount: (clientId) => agentTransfers.runningCountForClient(clientId),
    retainConnection,
    closeConnectionIfIdle,
    promptUser: (request) => agentPromptBroker.request(request),
    respondToPrompt: (response) => {
      agentPromptBroker.respond(response);
    },
    notifyUser: (title, body) => {
      if (Notification.isSupported()) new Notification({ title, body }).show();
    },
    emitActivity: (event) => broadcastToAllWindows(IPCChannel.AgentActivityEvent, event),
    getPreferences: () => prefsSvc.getAppPreferences(),
    tokenStore: {
      read: () => connections.getJsonSetting<string>("agent.mcp.token") ?? null,
      write: (value) => connections.saveJsonSetting("agent.mcp.token", value)
    },
    // Packaged: build/electron-builder.yml copies apps/mcp-bridge/dist here as
    // an extraResource. Dev: the workspace build output. The bridge is not on
    // npm, so `npx @nextshell/mcp-bridge` would simply 404 for the user.
    resolveBridgeEntry: () => {
      const candidate = app.isPackaged
        ? path.join(process.resourcesPath, "mcp-bridge", "index.js")
        : path.resolve(app.getAppPath(), "..", "mcp-bridge", "dist", "index.js");
      return fs.existsSync(candidate) ? candidate : null;
    },
    writeClipboard: (text) => clipboard.writeText(text),
    openExternal: (url) => shell.openExternal(url),
    chooseSavePath: async ({ title, defaultFileName }) => {
      const focused = BrowserWindow.getFocusedWindow();
      const options = {
        title,
        defaultPath: path.join(app.getPath("downloads"), defaultFileName),
        filters: [{ name: "MCP Bundle", extensions: ["mcpb"] }]
      };
      const result = focused
        ? await dialog.showSaveDialog(focused, options)
        : await dialog.showSaveDialog(options);
      return result.canceled || !result.filePath ? null : result.filePath;
    },
    logger: {
      info: (message, meta) => logger.info(`[Agent] ${message}`, meta),
      warn: (message, meta) => logger.warn(`[Agent] ${message}`, meta),
      error: (message, meta) => logger.error(`[Agent] ${message}`, meta)
    }
  });

  // ─── Dispose ─────────────────────────────────────────────────────────────
  const dispose = async (): Promise<void> => {
    connections.flush();
    prefsSvc.dispose();

    // First: stop accepting agent traffic, and unlink the socket plus the
    // discovery file before the rest of the teardown can stall.
    await agentMcpSvc.dispose().catch((error) => {
      logger.warn("[Agent] failed to dispose the MCP endpoint", normalizeError(error));
    });
    agentPromptBroker.dispose();
    oscTaps.disposeAll();
    screenMirrors.disposeAll();

    const allMonitorIds = monitorSvc.getAllConnectionIds();
    await Promise.all(allMonitorIds.map((id) => monitorSvc.disposeAllMonitorSessions(id)));

    await remoteEditManager.dispose();
    networkToolSvc.tracerouteStop();
    cloudSyncManager.dispose();

    const sessionIds = Array.from(activeSessions.keys());
    await Promise.all(sessionIds.map((id) => sessionSvc.closeSession(id)));

    const sshConnections = Array.from(connectionPool.values()).flat();
    connectionPool.clear();
    connectQueues.clear();
    connectionRefCounts.clear();
    await Promise.all(sshConnections.map((c) => c.close()));

    connections.close();
  };

  // ─── Public API ──────────────────────────────────────────────────────────
  // Sub-services are exposed directly; only genuinely composed orchestration
  // (multi-service flows or container-internal state) stays as methods.
  return {
    // Sub-services
    connections: connectionSvc,
    importExport: importExportSvc,
    sessions: sessionSvc,
    monitors: monitorSvc,
    commands: commandSvc,
    sftp: sftpSvc,
    networkTools: networkToolSvc,
    preferences: prefsSvc,
    terminalIntegration: terminalIntegrationSvc,
    cloudSync: cloudSyncManager,
    resourceOps: resourceOpsSvc,
    agentMcp: agentMcpSvc,
    connectionFolders: folderSvc,

    // Orchestration
    removeConnection: async (id) => {
      // 1. Snapshot to recycle bin + DB remove + push tombstone + delete credentials
      await resourceOpsSvc.deleteConnection({ id });
      // 2. Clean up runtime state (sessions, monitors, SSH connections)
      await connectionSvc.removeConnectionRecord(id);
      return { ok: true as const };
    },

    // Recycle bin listing/clearing sits on the container because it is backed
    // by the connection repository, which is container-internal.
    recycleBinList: () => connections.listRecycleBinEntries(),
    recycleBinClear: () => ({ ok: true as const, deleted: connections.clearRecycleBin() }),

    pauseMonitors: () => monitorSvc.pauseAll(),
    resumeMonitors: () => monitorSvc.resumeAll(),
    getAppPreferences: () => prefsSvc.getAppPreferences(),

    dispose
  };
};
