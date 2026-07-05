
import fs from "node:fs";
import path from "node:path";
import { BrowserWindow } from "electron";
import type { WebContents } from "electron";
import type {
  ConnectionProfile,
  MonitorSnapshot,
  NetworkSnapshot,
  ProcessSnapshot,
} from "../../../../../packages/core/src/index";
import {
  SshConnection,
  type SshConnectOptions,
} from "../../../../../packages/ssh/src/index";
import {
  IPCChannel,
  AUTH_REQUIRED_PREFIX,
} from "../../../../../packages/shared/src/index";
import type {
  DebugLogEntry,
  SessionAuthOverrideInput,
  SessionStatusEvent,
  SftpTransferStatusEvent,
} from "../../../../../packages/shared/src/index";
import {
  EncryptedSecretVault,
  KeytarPasswordCache,
  resolveDeviceKey,
  verifyMasterPassword,
} from "../../../../../packages/security/src/index";
import {
  SQLiteConnectionRepository,
  CachedConnectionRepository,
  SQLiteSshKeyRepository,
  CachedSshKeyRepository,
  SQLiteProxyRepository,
  CachedProxyRepository,
} from "../../../../../packages/storage/src/index";
import { RemoteEditManager } from "./remote-edit-manager";
import { BackupService, applyPendingRestore } from "./backup-service";
import { resolveAuditRuntime } from "./audit-runtime";
import { logger } from "../logger";
import { createOrderedBytesDispatcher } from "./ipc-stream-dispatcher";
import { normalizeError } from "./container-utils";
import type { ActiveSession } from "./container-types";

// ─── Sub-services ──────────────────────────────────────────────────────────
import { PreferencesDialogService } from "./preferences-dialog-service";
import { NetworkToolService } from "./network-tool-service";
import { CommandService } from "./command-service";
import { BackupPasswordService } from "./backup-password-service";
import { ConnectionService } from "./connection-service";
import { ImportExportService } from "./import-export-service";
import { CloudSyncManager } from "./cloud-sync-manager";
import { ResourceOperationsService } from "./resource-operations-service";
import { MonitorService } from "./monitor-service";
import { SftpService } from "./sftp-service";
import { SessionService } from "./session-service";

const cloudSyncWorkspacePasswordRef = (workspaceId: string): string =>
  `secret://cloud-sync-ws-${workspaceId}`;

// Re-export for consumers (index.ts, register.ts)
export type { ServiceContainer, CreateServiceContainerOptions } from "./container-types";

export const createServiceContainer = async (
  options: import("./container-types").CreateServiceContainerOptions
): Promise<import("./container-types").ServiceContainer> => {
  fs.mkdirSync(options.dataDir, { recursive: true });
  const dbPath = path.join(options.dataDir, "nextshell.db");

  applyPendingRestore(options.dataDir, dbPath);

  const rawRepo = new SQLiteConnectionRepository(dbPath);
  const connections = new CachedConnectionRepository(rawRepo);
  connections.seedIfEmpty([]);

  const sshKeyRepo = new CachedSshKeyRepository(new SQLiteSshKeyRepository(rawRepo.getDb()));
  const proxyRepo = new CachedProxyRepository(new SQLiteProxyRepository(rawRepo.getDb()));

  // ─── Device Key ──────────────────────────────────────────────────────────
  // The device key encrypts every stored credential. Keep it OUT of the SQLite
  // database (where the ciphertext also lives) by storing it in the OS keychain
  // via keytar — otherwise copying nextshell.db yields both key and ciphertext.
  // Fall back to DB storage only when the keychain is unavailable.
  const deviceKeyStore = new KeytarPasswordCache(options.keytarServiceName ?? "NextShell", "device-key");
  const deviceKey = await resolveDeviceKey(deviceKeyStore, {
    getLegacy: () => connections.getDeviceKey(),
    saveLegacy: (key) => connections.saveDeviceKey(key),
    clearLegacy: () => connections.clearDeviceKey(),
  });
  if (deviceKey.storedIn === "keychain") {
    logger.info(
      deviceKey.migratedFromDatabase
        ? "[Security] migrated device key from database to system keychain"
        : "[Security] device key resolved from system keychain",
    );
  } else {
    logger.warn("[Security] system keychain unavailable; device key stored in local database (degraded)");
  }

  const vault = new EncryptedSecretVault(connections.getSecretStore(), Buffer.from(deviceKey.deviceKeyHex, "hex"));

  // ─── Master Password ────────────────────────────────────────────────────
  const keytarServiceName = options.keytarServiceName ?? "NextShell";
  const keytarCache = new KeytarPasswordCache(keytarServiceName);
  let masterPassword: string | undefined;

  const tryRecallMasterPassword = async (): Promise<void> => {
    if (masterPassword) return;
    const meta = connections.getMasterKeyMeta();
    if (!meta) return;
    const cached = await keytarCache.recall();
    if (!cached) return;
    if (await verifyMasterPassword(cached, meta)) {
      masterPassword = cached;
      logger.info("[Security] recalled master password from keytar");
    }
  };
  void tryRecallMasterPassword();

  const backupService = new BackupService({
    dataDir: options.dataDir,
    repo: connections,
    getMasterPassword: () => masterPassword,
  });

  // ─── Audit ───────────────────────────────────────────────────────────────
  const auditEnabledForSession = connections.getAppPreferences().audit.enabled;
  const auditRuntime = resolveAuditRuntime(connections.getAppPreferences().audit);
  const appendAuditLogDirect = connections.appendAuditLog.bind(connections);

  const appendAuditLogIfEnabled = (payload: {
    action: string;
    level: "info" | "warn" | "error";
    connectionId?: string;
    message: string;
    metadata?: Record<string, unknown>;
  }): void => {
    if (!auditEnabledForSession) return;
    appendAuditLogDirect(payload);
  };

  const broadcastToAllWindows = (channel: string, payload: unknown): void => {
    for (const window of BrowserWindow.getAllWindows()) {
      if (!window.isDestroyed()) window.webContents.send(channel, payload);
    }
  };

  // Audit purge
  const purgeExpiredAuditLogs = (allowWhenDisabled = false): void => {
    try {
      if (!auditEnabledForSession && !allowWhenDisabled) return;
      const prefs = connections.getAppPreferences();
      const days = prefs.audit.retentionDays;
      if (days > 0) {
        const deleted = connections.purgeExpiredAuditLogs(days);
        if (deleted > 0) logger.info(`[Audit] purged ${deleted} expired audit log(s) (retention=${days}d)`);
      }
    } catch (error) {
      logger.warn("[Audit] failed to purge expired logs", error);
    }
  };

  if (auditRuntime.runStartupPurge) {
    const prefs = connections.getAppPreferences();
    if (prefs.audit.retentionDays > 0) purgeExpiredAuditLogs(true);
  }
  const auditPurgeTimer = auditRuntime.runPeriodicPurge
    ? setInterval(purgeExpiredAuditLogs, 6 * 3600_000)
    : undefined;

  // ─── Shared State ────────────────────────────────────────────────────────
  const activeSessions = new Map<string, ActiveSession>();
  const activeConnections = new Map<string, SshConnection>();
  const connectionPromises = new Map<string, Promise<SshConnection>>();

  // ─── IPC Helpers ─────────────────────────────────────────────────────────
  const sendSessionStatus = (sender: WebContents, payload: SessionStatusEvent): void => {
    if (!sender.isDestroyed()) sender.send(IPCChannel.SessionStatus, payload);
  };

  const sendTransferStatus = (sender: WebContents | undefined, payload: SftpTransferStatusEvent): void => {
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
      sessionId: streamId, data: chunk, deliveryId, byteLength,
    }),
  });

  // Monitor snapshots are low-rate (≤1Hz) and sent directly — no delivery-id/ack
  // handshake (that protocol remains exclusively for the high-throughput ordered
  // terminal byte stream above). A blocked-but-alive renderer can therefore queue
  // snapshots in the IPC channel at poll rate; that window is bounded by the
  // main process's "unresponsive" auto-reload and the hide/suspend poll pausing.
  const createMonitorSnapshotEmitter = <TSnapshot>(channel: string) =>
    (sender: WebContents | undefined, snapshot: TSnapshot): void => {
      if (sender && !sender.isDestroyed() && !sender.isCrashed()) {
        sender.send(channel, snapshot);
      }
    };

  const emitSystemMonitorSnapshot = createMonitorSnapshotEmitter<MonitorSnapshot>(IPCChannel.MonitorSystemData);
  const emitProcessMonitorSnapshot = createMonitorSnapshotEmitter<ProcessSnapshot>(IPCChannel.MonitorProcessData);
  const emitNetworkMonitorSnapshot = createMonitorSnapshotEmitter<NetworkSnapshot>(IPCChannel.MonitorNetworkData);

  // ─── Connection Pool ─────────────────────────────────────────────────────
  const remoteEditManager = new RemoteEditManager({ getConnection: ensureConnection });

  const getConnectionOrThrow = (id: string): ConnectionProfile => {
    const connection = connections.getById(id);
    if (!connection) throw new Error("Connection not found");
    return connection;
  };

  const resolveConnectOptions = async (
    profile: ConnectionProfile,
    authOverride?: SessionAuthOverrideInput,
  ): Promise<SshConnectOptions> => {
    let proxy: SshConnectOptions["proxy"];
    if (profile.proxyId) {
      const proxyProfile = proxyRepo.getById(profile.proxyId);
      if (!proxyProfile) throw new Error("Referenced proxy profile not found. Please update the connection.");
      const proxySecret = proxyProfile.credentialRef ? await vault.readCredential(proxyProfile.credentialRef) : undefined;
      proxy = {
        type: proxyProfile.proxyType, host: proxyProfile.host, port: proxyProfile.port,
        username: proxyProfile.username,
        password: proxyProfile.proxyType === "socks5" && proxyProfile.username ? proxySecret : undefined,
      };
      if (!proxy.host || proxy.port <= 0) throw new Error("Proxy host and port are required when proxy is enabled.");
    }

    const username = authOverride?.username?.trim() || profile.username.trim();
    if (!username) throw new Error("SSH username is required.");

    const prefs = connections.getAppPreferences();
    const keepAliveEnabled = profile.keepAliveEnabled ?? prefs.ssh.keepAliveEnabled;
    const intervalCandidate = profile.keepAliveIntervalSec ?? prefs.ssh.keepAliveIntervalSec;
    const keepAliveIntervalSec =
      Number.isInteger(intervalCandidate) && intervalCandidate >= 5 && intervalCandidate <= 600
        ? intervalCandidate : prefs.ssh.keepAliveIntervalSec;
    const keepaliveInterval = keepAliveEnabled ? keepAliveIntervalSec * 1000 : 0;

    const base: Omit<SshConnectOptions, "authType"> = {
      host: profile.host, port: profile.port, username,
      hostFingerprint: profile.hostFingerprint,
      strictHostKeyChecking: profile.strictHostKeyChecking,
      proxy, keepaliveInterval,
    };

    const secret = profile.credentialRef ? await vault.readCredential(profile.credentialRef) : undefined;
    const effectiveAuthType = authOverride?.authType ?? profile.authType;
    const isPasswordStyleAuth = effectiveAuthType === "password" || effectiveAuthType === "interactive";

    if (isPasswordStyleAuth) {
      const password =
        authOverride?.authType === "password" || authOverride?.authType === "interactive"
          ? authOverride.password
          : profile.authType === "password" || profile.authType === "interactive" ? secret : undefined;
      if (!password) {
        throw new Error(effectiveAuthType === "interactive"
          ? "Interactive auth requires password"
          : "Password credential is missing. Please provide password.");
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
        if (!keyProfile) throw new Error("Referenced SSH key not found. Please update the connection.");
        privateKey = await vault.readCredential(keyProfile.keyContentRef);
        if (keyProfile.passphraseRef) passphrase = await vault.readCredential(keyProfile.passphraseRef);
        if (authOverride?.passphrase) passphrase = authOverride.passphrase;
      }
      if (!privateKey) throw new Error("Private key auth requires an SSH key. Please select a key.");
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
      connections.save({ ...latest, hostFingerprint: fingerprint, updatedAt: new Date().toISOString() });
      appendAuditLogIfEnabled({
        action: "connection.host_fingerprint_pinned",
        level: "info",
        connectionId,
        message: "Pinned host key fingerprint on first connect (TOFU)",
        metadata: { fingerprint },
      });
      logger.info("[Security] pinned host fingerprint (TOFU)", { connectionId, fingerprint });
    } catch (error) {
      logger.warn("[Security] failed to pin host fingerprint", { connectionId, error: normalizeError(error) });
    }
  };

  const establishConnection = async (
    connectionId: string, profile: ConnectionProfile, authOverride?: SessionAuthOverrideInput,
  ): Promise<SshConnection> => {
    logger.info("[SSH] connecting", { connectionId, host: profile.host, port: profile.port });
    const connectOptions = await resolveConnectOptions(profile, authOverride);
    let observedFingerprint: string | undefined;
    const ssh = await SshConnection.connect({
      ...connectOptions,
      onHostFingerprint: (fingerprint) => { observedFingerprint = fingerprint; },
    });
    if (observedFingerprint && !profile.hostFingerprint?.trim()) {
      pinHostFingerprint(connectionId, observedFingerprint);
    }
    ssh.onClose(() => {
      activeConnections.delete(connectionId);
      void remoteEditManager.cleanupByConnectionId(connectionId);
      logger.info("[SSH] disconnected", { connectionId });
    });
    activeConnections.set(connectionId, ssh);
    logger.info("[SSH] connected", { connectionId });
    return ssh;
  };

  function ensureConnection(connectionId: string, authOverride?: SessionAuthOverrideInput): Promise<SshConnection> {
    const existing = activeConnections.get(connectionId);
    if (existing) return Promise.resolve(existing);
    if (authOverride) {
      const profile = getConnectionOrThrow(connectionId);
      return establishConnection(connectionId, profile, authOverride);
    }
    const pending = connectionPromises.get(connectionId);
    if (pending) return pending;
    const profile = getConnectionOrThrow(connectionId);
    const promise = establishConnection(connectionId, profile);
    connectionPromises.set(connectionId, promise);
    return promise.finally(() => { connectionPromises.delete(connectionId); });
  }

  const closeConnectionIfIdle = async (connectionId: string): Promise<void> => {
    const stillUsed = Array.from(activeSessions.values()).some(
      (s) => s.kind === "remote" && s.connectionId === connectionId,
    );
    if (stillUsed) return;
    await monitorSvc.disposeAllMonitorSessions(connectionId);
    const client = activeConnections.get(connectionId);
    if (!client) return;
    await remoteEditManager.cleanupByConnectionId(connectionId);
    activeConnections.delete(connectionId);
    await client.close();
  };

  const hasVisibleTerminalAlive = (connectionId: string): boolean =>
    Array.from(activeSessions.values()).some(
      (s) => s.kind === "remote" && s.connectionId === connectionId
        && s.descriptor.type === "terminal" && s.descriptor.status === "connected",
    );

  const assertMonitorEnabled = (connectionId: string): ConnectionProfile => {
    const profile = getConnectionOrThrow(connectionId);
    if (!profile.monitorSession) throw new Error("当前连接未启用 Monitor Session，请在连接配置中开启后重试。");
    return profile;
  };

  const assertVisibleTerminalAlive = (connectionId: string): void => {
    if (!hasVisibleTerminalAlive(connectionId)) throw new Error("请先连接 SSH 终端以启动 Monitor Session。");
  };

  const establishHiddenConnection = async (connectionId: string, tag: string): Promise<SshConnection> => {
    const profile = assertMonitorEnabled(connectionId);
    logger.info(`[${tag}] connecting hidden SSH`, { connectionId, host: profile.host, port: profile.port });
    const ssh = await SshConnection.connect(await resolveConnectOptions(profile));
    logger.info(`[${tag}] hidden SSH connected`, { connectionId });
    return ssh;
  };

  // ─── Sub-Service Instantiation ───────────────────────────────────────────
  const prefsSvc = new PreferencesDialogService({
    connections,
    auditEnabledForSession,
  });

  const networkToolSvc = new NetworkToolService({ connections });

  const monitorSvc = new MonitorService({
    connections,
    getConnectionOrThrow,
    resolveConnectOptions: (profile) => resolveConnectOptions(profile),
    activeSessions,
    appendAuditLogIfEnabled,
    debugSenders: prefsSvc.debugSenders,
    emitDebugLog: (entry) => prefsSvc.emitDebugLog(entry),
    emitSystemSnapshot: emitSystemMonitorSnapshot,
    emitProcessSnapshot: emitProcessMonitorSnapshot,
    emitNetworkSnapshot: emitNetworkMonitorSnapshot,
  });

  const sftpSvc = new SftpService({
    getConnectionOrThrow,
    ensureConnection,
    remoteEditManager,
    appendAuditLogIfEnabled,
    sendTransferStatus,
  });

  const connectionSvc = new ConnectionService({
    connections, sshKeyRepo, proxyRepo, vault,
    activeSessions,
    disposeAllMonitorSessions: (id) => monitorSvc.disposeAllMonitorSessions(id),
    closeConnectionIfIdle,
    remoteEditManager,
    monitorStates: monitorSvc.monitorStates,
    getCloudSyncManager: () => cloudSyncManager,
    appendAuditLogIfEnabled,
    sendSessionStatus,
  });

  const backupPasswordSvc = new BackupPasswordService({
    connections, vault, keytarCache, backupService,
    getMasterPassword: () => masterPassword,
    setMasterPassword: (p) => { masterPassword = p; },
    tryRecallMasterPassword,
    appendAuditLogIfEnabled,
  });

  let cloudSyncManager: CloudSyncManager | undefined;

  const commandSvc = new CommandService({
    connections,
    getConnectionOrThrow,
    ensureConnection,
    listWorkspaces: () => connections.listCloudSyncWorkspaces(),
    markWorkspaceCommandsDirty: (workspaceId) => {
      cloudSyncManager?.markWorkspaceCommandsDirty(workspaceId);
    },
    appendAuditLogIfEnabled,
  });

  const importExportSvc = new ImportExportService({
    connections, vault,
    upsertConnection: (input) => connectionSvc.upsertConnection(input),
    appendAuditLogIfEnabled,
  });

  const sessionSvc = new SessionService({
    connections, activeSessions,
    getConnectionOrThrow,
    ensureConnection,
    closeConnectionIfIdle,
    appendAuditLogIfEnabled,
    sendSessionStatus,
    sessionDataDispatcher,
    ensureSystemMonitorRuntime: (id) => monitorSvc.ensureSystemMonitorRuntime(id),
    clearMonitorSuspension: (id) => monitorSvc.clearMonitorSuspension(id),
    warmupSftp: (id, conn) => sftpSvc.warmupSftp(id, conn),
    persistAuthOverride: (id, override) => connectionSvc.persistSuccessfulAuthOverride(id, override),
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
      try { return await vault.readCredential(ref); } catch { return undefined; }
    },
    storeCredential: (name, secret) => vault.storeCredential(name, secret),
    deleteCredential: (ref) => vault.deleteCredential(ref),
    listWorkspaces: () => connections.listCloudSyncWorkspaces(),
    saveWorkspace: (ws) => connections.saveCloudSyncWorkspace(ws),
    removeWorkspace: (id) => connections.removeCloudSyncWorkspace(id),
    getWorkspaceRepoLocalState: (wId) => connections.getWorkspaceRepoLocalState(wId),
    saveWorkspaceRepoLocalState: (state) => connections.saveWorkspaceRepoLocalState(state),
    listWorkspaceRepoConflicts: (wId) => connections.listWorkspaceRepoConflicts(wId),
    saveWorkspaceRepoConflict: (conflict) => connections.saveWorkspaceRepoConflict(conflict),
    removeWorkspaceRepoConflict: (wId, resourceType, resourceId) =>
      connections.removeWorkspaceRepoConflict(wId, resourceType, resourceId),
    clearWorkspaceRepoConflicts: (wId) => connections.clearWorkspaceRepoConflicts(wId),
    listWorkspaceCommands: (wId) => connections.listWorkspaceCommands(wId),
    replaceWorkspaceCommands: (wId, commands) => connections.replaceWorkspaceCommands(wId, commands),
    getWorkspaceCommandsVersion: (wId) => connections.getWorkspaceCommandsVersion(wId),
    saveWorkspaceCommandsVersion: (wId, version) => connections.saveWorkspaceCommandsVersion(wId, version),
    saveRecycleBinEntry: (e) => connections.saveRecycleBinEntry(e),
    listRecycleBinEntries: () => connections.listRecycleBinEntries(),
    removeRecycleBinEntry: (id) => connections.removeRecycleBinEntry(id),
    storeWorkspacePassword: async (wId, pwd) => {
      await vault.storeCredential(`cloud-sync-ws-${wId}`, pwd);
    },
    getWorkspacePassword: async (wId) => {
      try { return await vault.readCredential(cloudSyncWorkspacePasswordRef(wId)); } catch { return undefined; }
    },
    deleteWorkspacePassword: async (wId) => {
      await vault.deleteCredential(cloudSyncWorkspacePasswordRef(wId)).catch(() => {});
    },
    getJsonSetting: (key) => connections.getJsonSetting(key),
    saveJsonSetting: (key, value) => connections.saveJsonSetting(key, value),
    broadcastStatus: (status) => broadcastToAllWindows(IPCChannel.CloudSyncStatusEvent, status),
    broadcastApplied: (wId) => broadcastToAllWindows(IPCChannel.CloudSyncAppliedEvent, { workspaceId: wId }),
  });
  cloudSyncManager.initialize();

  // Resource Operations Service
  const resourceOpsSvc = new ResourceOperationsService({
    connections,
    sshKeyRepo,
    proxyRepo,
    vault,
    cloudSyncManager,
    saveRecycleBinEntry: (e) => connections.saveRecycleBinEntry(e),
    listRecycleBinEntries: () => connections.listRecycleBinEntries(),
    removeRecycleBinEntry: (id) => connections.removeRecycleBinEntry(id),
    appendAuditLog: (payload) => appendAuditLogIfEnabled(payload),
  });

  // ─── Dispose ─────────────────────────────────────────────────────────────
  const dispose = async (): Promise<void> => {
    connections.flush();
    if (auditPurgeTimer) clearInterval(auditPurgeTimer);
    prefsSvc.dispose();

    const allMonitorIds = monitorSvc.getAllConnectionIds();
    await Promise.all(allMonitorIds.map((id) => monitorSvc.disposeAllMonitorSessions(id)));

    await remoteEditManager.dispose();
    networkToolSvc.tracerouteStop();
    cloudSyncManager.dispose();

    const sessionIds = Array.from(activeSessions.keys());
    await Promise.all(sessionIds.map((id) => sessionSvc.closeSession(id)));

    const sshConnections = Array.from(activeConnections.values());
    activeConnections.clear();
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
    backupPassword: backupPasswordSvc,
    networkTools: networkToolSvc,
    preferences: prefsSvc,
    cloudSync: cloudSyncManager,
    resourceOps: resourceOpsSvc,

    // Orchestration
    removeConnection: async (id) => {
      // 1. Snapshot to recycle bin + DB remove + push tombstone + delete credentials
      await resourceOpsSvc.deleteConnection({ id });
      // 2. Clean up runtime state (sessions, monitors, SSH connections)
      await connectionSvc.removeConnectionRecord(id, { skipAudit: true });
      return { ok: true as const };
    },

    // Recycle bin listing/clearing sits on the container because it is backed
    // by the connection repository, which is container-internal.
    recycleBinList: () => connections.listRecycleBinEntries(),
    recycleBinClear: () => ({ ok: true as const, deleted: connections.clearRecycleBin() }),

    pauseMonitors: () => monitorSvc.pauseAll(),
    resumeMonitors: () => monitorSvc.resumeAll(),
    getAppPreferences: () => prefsSvc.getAppPreferences(),

    dispose,
  };
};
