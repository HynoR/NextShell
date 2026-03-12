import type { WebContents } from "electron";
import type {
  AppPreferences,
  ConnectionProfile,
  TerminalEncoding,
} from "../../../../../packages/core/src/index";
import type {
  SshConnection,
  SshConnectOptions,
} from "../../../../../packages/ssh/src/index";
import type {
  DebugLogEntry,
  SessionAuthOverrideInput,
  SessionStatusEvent,
  SftpTransferStatusEvent,
  SettingsUpdateInput,
} from "../../../../../packages/shared/src/index";
import type { EncryptedSecretVault, KeytarPasswordCache } from "../../../../../packages/security/src/index";
import type {
  CachedConnectionRepository,
  CachedSshKeyRepository,
  CachedProxyRepository,
} from "../../../../../packages/storage/src/index";
import type { BackupService } from "./backup-service";
import type { RemoteEditManager } from "./remote-edit-manager";
import type {
  ActiveSession,
  AdhocSessionRuntime,
  MonitorState,
  SystemMonitorRuntime,
  ProcessMonitorRuntime,
  NetworkMonitorRuntime,
} from "./container-types";
import type { createLatestOnlyDispatcher, createOrderedBytesDispatcher } from "./ipc-stream-dispatcher";
import type { NetworkTool } from "./monitor/network-monitor-controller";

/**
 * ServiceContext is the shared dependency contract passed to all sub-services.
 * It provides access to repositories, security, connection pool infrastructure,
 * cross-cutting concerns, and stream dispatchers.
 */
export interface ServiceContext {
  // ─── Options ────────────────────────────────────────────────────────────
  dataDir: string;
  keytarServiceName: string;

  // ─── Repositories ───────────────────────────────────────────────────────
  connections: CachedConnectionRepository;
  sshKeyRepo: CachedSshKeyRepository;
  proxyRepo: CachedProxyRepository;

  // ─── Security ───────────────────────────────────────────────────────────
  vault: EncryptedSecretVault;
  keytarCache: KeytarPasswordCache;
  getMasterPassword: () => string | undefined;
  setMasterPassword: (password: string | undefined) => void;

  // ─── Services ───────────────────────────────────────────────────────────
  backupService: BackupService;
  remoteEditManager: RemoteEditManager;

  // ─── Connection Pool ────────────────────────────────────────────────────
  activeConnections: Map<string, SshConnection>;
  connectionPromises: Map<string, Promise<SshConnection>>;
  activeSessions: Map<string, ActiveSession>;

  // ─── Monitor State Maps ─────────────────────────────────────────────────
  systemMonitorRuntimes: Map<string, SystemMonitorRuntime>;
  systemMonitorConnections: Map<string, SshConnection>;
  systemMonitorConnectionPromises: Map<string, Promise<SshConnection>>;
  cancelledSystemMonitorConnections: Set<string>;
  processMonitorRuntimes: Map<string, ProcessMonitorRuntime>;
  processMonitorPromises: Map<string, Promise<ProcessMonitorRuntime>>;
  processMonitorConnections: Map<string, SshConnection>;
  processMonitorConnectionPromises: Map<string, Promise<SshConnection>>;
  cancelledProcessMonitorConnections: Set<string>;
  networkMonitorRuntimes: Map<string, NetworkMonitorRuntime>;
  networkMonitorPromises: Map<string, Promise<NetworkMonitorRuntime>>;
  networkMonitorConnections: Map<string, SshConnection>;
  networkMonitorConnectionPromises: Map<string, Promise<SshConnection>>;
  cancelledNetworkMonitorConnections: Set<string>;
  adhocSessionRuntimes: Map<string, AdhocSessionRuntime>;
  adhocSessionPromises: Map<string, Promise<AdhocSessionRuntime>>;
  monitorStates: Map<string, MonitorState>;
  networkToolCache: Map<string, NetworkTool>;

  // ─── Core Connection Helpers ────────────────────────────────────────────
  getConnectionOrThrow: (id: string) => ConnectionProfile;
  resolveConnectOptions: (
    profile: ConnectionProfile,
    authOverride?: SessionAuthOverrideInput
  ) => Promise<SshConnectOptions>;
  ensureConnection: (
    connectionId: string,
    authOverride?: SessionAuthOverrideInput
  ) => Promise<SshConnection>;
  closeConnectionIfIdle: (connectionId: string) => Promise<void>;
  hasVisibleTerminalAlive: (connectionId: string) => boolean;
  assertMonitorEnabled: (connectionId: string) => ConnectionProfile;
  assertVisibleTerminalAlive: (connectionId: string) => void;
  establishHiddenConnection: (connectionId: string, tag: string) => Promise<SshConnection>;

  // ─── Audit / Preferences ────────────────────────────────────────────────
  auditEnabledForSession: boolean;
  appendAuditLogIfEnabled: (payload: {
    action: string;
    level: "info" | "warn" | "error";
    connectionId?: string;
    message: string;
    metadata?: Record<string, unknown>;
  }) => void;
  getAppPreferences: () => AppPreferences;
  saveAppPreferencesPatch: (
    patch: SettingsUpdateInput,
    options?: { reconfigureCloudSync?: boolean }
  ) => AppPreferences;

  // ─── IPC Helpers ────────────────────────────────────────────────────────
  broadcastToAllWindows: (channel: string, payload: unknown) => void;
  sendSessionStatus: (sender: WebContents, payload: SessionStatusEvent) => void;
  sendTransferStatus: (
    sender: WebContents | undefined,
    payload: SftpTransferStatusEvent
  ) => void;

  // ─── Stream Dispatchers ─────────────────────────────────────────────────
  sessionDataDispatcher: ReturnType<typeof createOrderedBytesDispatcher>;
  systemMonitorDispatcher: ReturnType<typeof createLatestOnlyDispatcher>;
  processMonitorDispatcher: ReturnType<typeof createLatestOnlyDispatcher>;
  networkMonitorDispatcher: ReturnType<typeof createLatestOnlyDispatcher>;

  // ─── Debug Logging ──────────────────────────────────────────────────────
  debugSenders: Set<WebContents>;
  emitDebugLog: (entry: DebugLogEntry) => void;

  // ─── Audit Purge ────────────────────────────────────────────────────────
  auditPurgeTimer: ReturnType<typeof setInterval> | undefined;

  // ─── Try recall master password ─────────────────────────────────────────
  tryRecallMasterPassword: () => Promise<void>;
}
