import type { WebContents } from "electron";
import type {
  AppPreferences,
  ConnectionProfile,
  TerminalEncoding
} from "../../../../../packages/core/src/index";
import type { SshConnection, SshConnectOptions } from "../../../../../packages/ssh/src/index";
import type {
  DebugLogEntry,
  SessionAuthOverrideInput,
  SessionStatusEvent,
  SftpTransferStatusEvent,
  SettingsUpdateInput
} from "../../../../../packages/shared/src/index";
import type { EncryptedSecretVault } from "../../../../../packages/security/src/index";
import type {
  CachedConnectionRepository,
  CachedSshKeyRepository,
  CachedProxyRepository
} from "../../../../../packages/storage/src/index";
import type { RemoteEditManager } from "./remote-edit-manager";
import type { ActiveSession, MonitorState } from "./container-types";
import type { createOrderedBytesDispatcher } from "./ipc-stream-dispatcher";
import type { NetworkTool } from "./monitor/network-monitor-controller";

/**
 * ServiceContext is the shared dependency contract passed to all sub-services.
 * It provides access to repositories, security, connection pool infrastructure,
 * cross-cutting concerns, and stream dispatchers.
 */
export interface ServiceContext {
  // ─── Options ────────────────────────────────────────────────────────────
  dataDir: string;

  // ─── Repositories ───────────────────────────────────────────────────────
  connections: CachedConnectionRepository;
  sshKeyRepo: CachedSshKeyRepository;
  proxyRepo: CachedProxyRepository;

  // ─── Security ───────────────────────────────────────────────────────────
  vault: EncryptedSecretVault;

  // ─── Services ───────────────────────────────────────────────────────────
  remoteEditManager: RemoteEditManager;

  // ─── Connection Pool ────────────────────────────────────────────────────
  activeConnections: Map<string, SshConnection>;
  connectionPromises: Map<string, Promise<SshConnection>>;
  activeSessions: Map<string, ActiveSession>;

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

  // ─── Preferences ────────────────────────────────────────────────────────
  getAppPreferences: () => AppPreferences;
  saveAppPreferencesPatch: (patch: SettingsUpdateInput) => AppPreferences;

  // ─── IPC Helpers ────────────────────────────────────────────────────────
  broadcastToAllWindows: (channel: string, payload: unknown) => void;
  sendSessionStatus: (sender: WebContents, payload: SessionStatusEvent) => void;
  sendTransferStatus: (sender: WebContents | undefined, payload: SftpTransferStatusEvent) => void;

  // ─── Stream Dispatchers ─────────────────────────────────────────────────
  // Only the ordered terminal byte stream is dispatched with the ack
  // protocol; monitor snapshots are sent directly via webContents.send.
  sessionDataDispatcher: ReturnType<typeof createOrderedBytesDispatcher>;

  // ─── Debug Logging ──────────────────────────────────────────────────────
  debugSenders: Set<WebContents>;
  emitDebugLog: (entry: DebugLogEntry) => void;
}
